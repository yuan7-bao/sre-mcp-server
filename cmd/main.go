package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/3490165738/sre-mcp-server/internal/server"
	"github.com/3490165738/sre-mcp-server/internal/tools"
	"github.com/3490165738/sre-mcp-server/internal/analyzer"
)

var (
	prometheusURL string
	grafanaURL    string
	grafanaAPIKey string
	lokiURL       string
	transport     string
	httpAddr      string
)

func init() {
	flag.StringVar(&prometheusURL, "prometheus-url", envOrDefault("PROMETHEUS_URL", "http://localhost:9090"), "Prometheus server URL")
	flag.StringVar(&grafanaURL, "grafana-url", envOrDefault("GRAFANA_URL", "http://localhost:3000"), "Grafana server URL")
	flag.StringVar(&grafanaAPIKey, "grafana-api-key", envOrDefault("GRAFANA_API_KEY", ""), "Grafana API key")
	flag.StringVar(&lokiURL, "loki-url", envOrDefault("LOKI_URL", "http://localhost:3100"), "Loki server URL")
	flag.StringVar(&transport, "transport", envOrDefault("MCP_TRANSPORT", "stdio"), "Transport mode: stdio or http")
	flag.StringVar(&httpAddr, "http-addr", envOrDefault("MCP_HTTP_ADDR", ":8080"), "HTTP listen address (when transport=http)")
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func main() {
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("Starting SRE MCP Server...")
	log.Printf("  Prometheus: %s", prometheusURL)
	log.Printf("  Grafana:    %s", grafanaURL)
	log.Printf("  Loki:       %s", lokiURL)
	log.Printf("  Transport:  %s", transport)

	// Initialize clients
	promClient := tools.NewPrometheusClient(prometheusURL)
	grafanaClient := tools.NewGrafanaClient(grafanaURL, grafanaAPIKey)
	lokiClient := tools.NewLokiClient(lokiURL)
	anomalyDetector := analyzer.NewAnomalyDetector(promClient)
	correlator := analyzer.NewCorrelator(promClient, lokiClient)
	rcaEngine := analyzer.NewRCAEngine(promClient, lokiClient)
	forecaster := analyzer.NewForecaster(promClient)

	// Create MCP server and register all tools
	srv := server.NewMCPServer("sre-mcp-server", "0.1.0")

	// -- Prometheus Tools --
	srv.RegisterTool(server.ToolDef{
		Name:        "promql_instant",
		Description: "Execute an instant PromQL query against Prometheus. Returns the current value of metrics. Use this for checking the current state of any metric (CPU usage, memory, request rate, error rate, etc).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "PromQL expression, e.g. 'up', 'rate(http_requests_total[5m])', 'node_cpu_seconds_total'"},
			},
			"required": []string{"query"},
		},
		Handler: promClient.InstantQuery,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "promql_range",
		Description: "Execute a range PromQL query over a time window. Returns time series data. Use this for trend analysis, viewing metric history, or comparing values over time.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":    map[string]any{"type": "string", "description": "PromQL expression"},
				"duration": map[string]any{"type": "string", "description": "Time range, e.g. '1h', '6h', '24h', '7d'", "default": "1h"},
				"step":     map[string]any{"type": "string", "description": "Query resolution step, e.g. '15s', '1m', '5m'", "default": "1m"},
			},
			"required": []string{"query"},
		},
		Handler: promClient.RangeQuery,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "prom_targets",
		Description: "List all Prometheus scrape targets and their health status (up/down). Use this to check which services are being monitored and if any targets are unreachable.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     promClient.Targets,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "prom_alerts",
		Description: "Get all currently firing alerts from Prometheus. Returns alert name, labels, severity, and duration. This is the first tool to use when investigating an incident.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     promClient.Alerts,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "prom_metadata",
		Description: "Get metadata (type, help text, unit) for a specific metric name. Use this to understand what a metric measures before querying it.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"metric": map[string]any{"type": "string", "description": "Metric name, e.g. 'http_requests_total'"},
			},
			"required": []string{"metric"},
		},
		Handler: promClient.Metadata,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "prom_label_values",
		Description: "Get all possible values for a specific Prometheus label. Useful for discovering available services, instances, job names, namespaces, etc.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"label": map[string]any{"type": "string", "description": "Label name, e.g. 'job', 'instance', 'namespace', 'service'"},
			},
			"required": []string{"label"},
		},
		Handler: promClient.LabelValues,
	})

	// -- Grafana Tools --
	srv.RegisterTool(server.ToolDef{
		Name:        "grafana_dashboards",
		Description: "List all Grafana dashboards with their titles, UIDs, and tags. Use this to discover what dashboards exist and find the right one to inspect.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     grafanaClient.ListDashboards,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "grafana_dashboard_detail",
		Description: "Get full configuration of a specific Grafana dashboard including all panels, queries, and thresholds. Use this to understand what metrics a dashboard monitors.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"uid": map[string]any{"type": "string", "description": "Dashboard UID (get from grafana_dashboards)"},
			},
			"required": []string{"uid"},
		},
		Handler: grafanaClient.DashboardDetail,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "grafana_annotations",
		Description: "Query Grafana annotations/events within a time range. Annotations mark deployments, incidents, config changes — useful for correlating with metric anomalies.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"duration": map[string]any{"type": "string", "description": "Time range to look back, e.g. '1h', '24h'", "default": "6h"},
				"tags":     map[string]any{"type": "string", "description": "Filter by tags (comma-separated), e.g. 'deploy,incident'"},
			},
		},
		Handler: grafanaClient.Annotations,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "grafana_alerts",
		Description: "Get Grafana alerting rules and their current states (firing, normal, pending, error). Shows alert conditions and configured thresholds.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     grafanaClient.AlertRules,
	})

	// -- Loki Tools --
	srv.RegisterTool(server.ToolDef{
		Name:        "loki_query",
		Description: "Query logs from Loki using LogQL. Returns log lines matching the query. Essential for investigating errors, exceptions, and service behavior during incidents.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":    map[string]any{"type": "string", "description": "LogQL expression, e.g. '{job=\"api-server\"} |= \"error\"', '{namespace=\"prod\"} | json | status >= 500'"},
				"duration": map[string]any{"type": "string", "description": "Time range, e.g. '15m', '1h', '6h'", "default": "1h"},
				"limit":    map[string]any{"type": "integer", "description": "Max number of log lines to return", "default": 100},
			},
			"required": []string{"query"},
		},
		Handler: lokiClient.Query,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "loki_labels",
		Description: "List all available log labels in Loki. Use this to discover what log sources exist (jobs, namespaces, containers, pods).",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     lokiClient.Labels,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "loki_series",
		Description: "Discover log streams matching a label selector. Returns unique label combinations. Use this to find specific log sources before querying.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"match": map[string]any{"type": "string", "description": "Label selector, e.g. '{job=\"api-server\"}', '{namespace=\"production\"}'"},
			},
			"required": []string{"match"},
		},
		Handler: lokiClient.Series,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "loki_stats",
		Description: "Get log volume statistics for a label selector. Shows how many log lines and bytes are being produced — useful for identifying noisy services.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":    map[string]any{"type": "string", "description": "LogQL selector, e.g. '{namespace=\"prod\"}'"},
				"duration": map[string]any{"type": "string", "description": "Time range", "default": "1h"},
			},
			"required": []string{"query"},
		},
		Handler: lokiClient.Stats,
	})

	// -- AI Analyzer Tools --
	srv.RegisterTool(server.ToolDef{
		Name:        "analyze_anomaly",
		Description: "Detect anomalies in a Prometheus metric using statistical methods (3-sigma, Z-score, IQR). Automatically identifies unusual spikes, drops, or trend changes. Use this when you suspect something is wrong but don't know the exact threshold.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":    map[string]any{"type": "string", "description": "PromQL query for the metric to analyze"},
				"duration": map[string]any{"type": "string", "description": "Analysis window", "default": "6h"},
				"method":   map[string]any{"type": "string", "description": "Detection method: 'zscore', 'sigma3', 'iqr'", "default": "zscore"},
			},
			"required": []string{"query"},
		},
		Handler: anomalyDetector.Detect,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "correlate_signals",
		Description: "Correlate metrics anomalies with log events and deployment annotations in the same time window. Finds connections between metric spikes and error logs / config changes / deployments. The key tool for incident investigation.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"metric_query": map[string]any{"type": "string", "description": "PromQL query for the metric showing issues"},
				"log_query":    map[string]any{"type": "string", "description": "LogQL query for related logs, e.g. '{job=\"api\"} |= \"error\"'"},
				"duration":     map[string]any{"type": "string", "description": "Time window to correlate", "default": "1h"},
			},
			"required": []string{"metric_query", "log_query"},
		},
		Handler: correlator.Correlate,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "rca_suggest",
		Description: "Given a symptom (e.g. 'high latency on order-service'), automatically query related metrics, logs, and recent changes, then suggest probable root causes ranked by likelihood. This is the most powerful diagnostic tool — it chains multiple queries together.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"symptom":   map[string]any{"type": "string", "description": "Description of the problem, e.g. 'high error rate on payment service', 'node-3 CPU spike'"},
				"service":   map[string]any{"type": "string", "description": "Affected service name (optional, improves accuracy)"},
				"namespace": map[string]any{"type": "string", "description": "K8s namespace (optional)"},
			},
			"required": []string{"symptom"},
		},
		Handler: rcaEngine.Suggest,
	})

	srv.RegisterTool(server.ToolDef{
		Name:        "capacity_forecast",
		Description: "Predict when a resource will hit a critical threshold using linear regression on historical data. Answers questions like 'when will disk space run out?' or 'when will we need to scale up?'",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":     map[string]any{"type": "string", "description": "PromQL for the resource metric (e.g. disk usage percentage, memory usage)"},
				"threshold": map[string]any{"type": "number", "description": "Critical threshold value (e.g. 90 for 90% disk usage)", "default": 90},
				"lookback":  map[string]any{"type": "string", "description": "Historical data to base prediction on", "default": "7d"},
			},
			"required": []string{"query"},
		},
		Handler: forecaster.Forecast,
	})

	// Start server
	log.Printf("Registered %d MCP tools", srv.ToolCount())

	var err error
	switch transport {
	case "stdio":
		log.Println("Starting in stdio mode...")
		err = srv.ServeStdio()
	case "http":
		log.Printf("Starting HTTP server on %s...", httpAddr)
		err = srv.ServeHTTP(httpAddr)
	default:
		log.Fatalf("Unknown transport: %s (use 'stdio' or 'http')", transport)
	}

	if err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
