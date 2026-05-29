package analyzer

import (
	"context"
	"fmt"
	"strings"

	"github.com/3490165738/sre-mcp-server/internal/tools"
)

type RCAEngine struct {
	prom *tools.PrometheusClient
	loki *tools.LokiClient
}

func NewRCAEngine(prom *tools.PrometheusClient, loki *tools.LokiClient) *RCAEngine {
	return &RCAEngine{prom: prom, loki: loki}
}

// Suggest performs automated root cause analysis given a symptom description.
func (r *RCAEngine) Suggest(ctx context.Context, params map[string]any) (string, error) {
	symptom, _ := params["symptom"].(string)
	if symptom == "" {
		return "", fmt.Errorf("symptom parameter is required")
	}
	service := getStr(params, "service", "")
	namespace := getStr(params, "namespace", "")

	var sb strings.Builder
	sb.WriteString("## 🔍 Root Cause Analysis\n\n")
	sb.WriteString(fmt.Sprintf("**Symptom**: %s\n", symptom))
	if service != "" {
		sb.WriteString(fmt.Sprintf("**Service**: %s\n", service))
	}
	sb.WriteString("\n---\n\n")

	// Build service-specific queries
	checks := r.buildChecks(symptom, service, namespace)

	sb.WriteString("### Diagnostic Checks\n\n")

	findings := make([]finding, 0)

	for i, check := range checks {
		sb.WriteString(fmt.Sprintf("**Check %d**: %s\n", i+1, check.name))
		sb.WriteString(fmt.Sprintf("  Query: `%s`\n", check.query))

		// Execute the check
		points, err := r.prom.GetRangeData(check.query, "30m", "1m")
		if err != nil {
			sb.WriteString(fmt.Sprintf("  ❌ Query failed: %v\n\n", err))
			continue
		}

		if len(points) == 0 {
			sb.WriteString("  ⚪ No data\n\n")
			continue
		}

		// Analyze the result
		values := make([]float64, len(points))
		for j, p := range points {
			values[j] = p.Value
		}
		mean, std := meanStd(values)
		_, maxVal := minMax(values)
		latest := values[len(values)-1]

		isAbnormal := false
		severity := "low"
		detail := ""

		// Check against threshold
		if check.threshold > 0 && latest > check.threshold {
			isAbnormal = true
			severity = "high"
			detail = fmt.Sprintf("Current value %.2f exceeds threshold %.2f", latest, check.threshold)
		} else if std > 0 && (latest-mean)/std > 2.5 {
			isAbnormal = true
			severity = "medium"
			detail = fmt.Sprintf("Current value %.2f is %.1f sigma above mean %.2f", latest, (latest-mean)/std, mean)
		}

		if isAbnormal {
			icon := "⚠️"
			if severity == "high" {
				icon = "🔴"
			}
			sb.WriteString(fmt.Sprintf("  %s **ABNORMAL** [%s]: %s\n", icon, severity, detail))
			findings = append(findings, finding{
				name:     check.name,
				category: check.category,
				severity: severity,
				detail:   detail,
				value:    latest,
				max:      maxVal,
			})
		} else {
			sb.WriteString(fmt.Sprintf("  ✅ Normal (current=%.2f, mean=%.2f)\n", latest, mean))
		}
		sb.WriteString("\n")
	}

	// Check related error logs
	if service != "" {
		sb.WriteString("### Error Log Scan\n\n")
		logQuery := fmt.Sprintf("{job=\"%s\"} |= \"error\" or |= \"Error\" or |= \"ERROR\" or |= \"panic\" or |= \"fatal\"", service)
		if namespace != "" {
			logQuery = fmt.Sprintf("{namespace=\"%s\", job=\"%s\"} |= \"error\" or |= \"ERROR\"", namespace, service)
		}

		logs, err := r.loki.QueryLogs(logQuery, "30m", 20)
		if err != nil {
			sb.WriteString(fmt.Sprintf("  Log query failed: %v\n\n", err))
		} else if len(logs) == 0 {
			sb.WriteString("  No error logs found in the last 30 minutes.\n\n")
		} else {
			sb.WriteString(fmt.Sprintf("  Found **%d** error log entries:\n", len(logs)))
			for j, log := range logs {
				if j >= 10 {
					sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(logs)-10))
					break
				}
				sb.WriteString(fmt.Sprintf("  [%s] %s\n", log.Timestamp.Format("15:04:05"), truncateStr(log.Line, 120)))
			}
			sb.WriteString("\n")
		}
	}

	// Generate RCA summary
	sb.WriteString("### 🎯 Probable Root Causes (ranked by likelihood)\n\n")

	if len(findings) == 0 {
		sb.WriteString("No obvious abnormalities detected in standard metrics.\n")
		sb.WriteString("Possible next steps:\n")
		sb.WriteString("1. Check application-level metrics (business SLIs)\n")
		sb.WriteString("2. Review recent deployments or config changes\n")
		sb.WriteString("3. Check external dependencies (databases, third-party APIs)\n")
		sb.WriteString("4. Examine network-level metrics (packet loss, DNS latency)\n")
	} else {
		// Rank findings by severity
		highCount := 0
		for _, f := range findings {
			if f.severity == "high" {
				highCount++
			}
		}

		for i, f := range findings {
			confidence := "Medium"
			if f.severity == "high" {
				confidence = "High"
			}
			sb.WriteString(fmt.Sprintf("%d. **[%s confidence]** %s\n", i+1, confidence, f.name))
			sb.WriteString(fmt.Sprintf("   %s\n", f.detail))
			sb.WriteString(fmt.Sprintf("   Category: %s\n", f.category))

			// Suggest remediation
			remediation := suggestRemediation(f.category, f.severity)
			sb.WriteString(fmt.Sprintf("   Suggested action: %s\n\n", remediation))
		}
	}

	return sb.String(), nil
}

type check struct {
	name      string
	query     string
	threshold float64
	category  string
}

type finding struct {
	name     string
	category string
	severity string
	detail   string
	value    float64
	max      float64
}

func (r *RCAEngine) buildChecks(symptom, service, namespace string) []check {
	checks := []check{
		// Resource utilization
		{name: "CPU Usage", query: "100 - (avg(rate(node_cpu_seconds_total{mode=\"idle\"}[5m])) * 100)", threshold: 85, category: "resource"},
		{name: "Memory Usage", query: "(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100", threshold: 90, category: "resource"},
		{name: "Disk Usage", query: "(1 - node_filesystem_avail_bytes{fstype!~\"tmpfs|overlay\"} / node_filesystem_size_bytes{fstype!~\"tmpfs|overlay\"}) * 100", threshold: 85, category: "resource"},

		// Network
		{name: "Network Errors", query: "rate(node_network_receive_errs_total[5m]) + rate(node_network_transmit_errs_total[5m])", threshold: 10, category: "network"},

		// K8s Pod status
		{name: "Pod Restarts (last 30m)", query: "sum(increase(kube_pod_container_status_restarts_total[30m]))", threshold: 5, category: "application"},
		{name: "Pending Pods", query: "sum(kube_pod_status_phase{phase=\"Pending\"})", threshold: 1, category: "scheduling"},
		{name: "OOMKilled Containers", query: "sum(kube_pod_container_status_last_terminated_reason{reason=\"OOMKilled\"})", threshold: 0, category: "resource"},
	}

	// Add service-specific checks
	if service != "" {
		svcFilter := fmt.Sprintf("job=\"%s\"", service)
		if namespace != "" {
			svcFilter = fmt.Sprintf("namespace=\"%s\", job=\"%s\"", namespace, service)
		}

		checks = append(checks,
			check{name: fmt.Sprintf("%s Error Rate", service), query: fmt.Sprintf("sum(rate(http_requests_total{%s, status=~\"5..\"}[5m])) / sum(rate(http_requests_total{%s}[5m])) * 100", svcFilter, svcFilter), threshold: 5, category: "application"},
			check{name: fmt.Sprintf("%s Latency P99", service), query: fmt.Sprintf("histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{%s}[5m])) by (le))", svcFilter), threshold: 0, category: "performance"},
			check{name: fmt.Sprintf("%s Request Rate", service), query: fmt.Sprintf("sum(rate(http_requests_total{%s}[5m]))", svcFilter), threshold: 0, category: "traffic"},
		)
	}

	// Add symptom-specific checks
	symptomLower := strings.ToLower(symptom)
	if strings.Contains(symptomLower, "latency") || strings.Contains(symptomLower, "slow") || strings.Contains(symptomLower, "延迟") {
		checks = append(checks,
			check{name: "DNS Latency", query: "avg(dns_lookup_duration_seconds)", threshold: 0.1, category: "network"},
			check{name: "Redis Latency", query: "redis_commands_duration_seconds_total", threshold: 0, category: "dependency"},
		)
	}
	if strings.Contains(symptomLower, "memory") || strings.Contains(symptomLower, "oom") || strings.Contains(symptomLower, "内存") {
		checks = append(checks,
			check{name: "Container Memory vs Limit", query: "sum(container_memory_working_set_bytes) / sum(kube_pod_container_resource_limits{resource=\"memory\"}) * 100", threshold: 90, category: "resource"},
		)
	}

	return checks
}

func suggestRemediation(category, severity string) string {
	switch category {
	case "resource":
		if severity == "high" {
			return "Immediate: scale horizontally (increase replicas) or vertically (increase resource limits). Check for memory leaks or CPU-bound loops."
		}
		return "Monitor closely. Consider adjusting resource requests/limits or enabling HPA."
	case "network":
		return "Check network policies, DNS resolution, and upstream proxy health. Run tcpdump if needed."
	case "application":
		return "Check recent deployments (git log). Review error logs for stack traces. Consider rollback if correlated with a deploy."
	case "scheduling":
		return "Check node resource availability. Review PDB constraints and pod affinity rules."
	case "performance":
		return "Profile the service (pprof for Go, async-profiler for Java). Check database query performance and connection pool saturation."
	case "traffic":
		return "Verify if traffic spike is organic or attack. Check rate limiting and circuit breaker configurations."
	case "dependency":
		return "Check health of downstream dependencies (databases, caches, external APIs). Verify connection pool status."
	default:
		return "Investigate further with targeted queries."
	}
}
