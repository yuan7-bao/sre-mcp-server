package analyzer

import (
	"context"
	"fmt"
	"strings"

	"github.com/3490165738/sre-mcp-server/internal/tools"
)

type Correlator struct {
	prom *tools.PrometheusClient
	loki *tools.LokiClient
}

func NewCorrelator(prom *tools.PrometheusClient, loki *tools.LokiClient) *Correlator {
	return &Correlator{prom: prom, loki: loki}
}

func (c *Correlator) Correlate(ctx context.Context, params map[string]any) (string, error) {
	metricQuery, _ := params["metric_query"].(string)
	logQuery, _ := params["log_query"].(string)
	duration := getStr(params, "duration", "1h")

	if metricQuery == "" || logQuery == "" {
		return "", fmt.Errorf("both metric_query and log_query are required")
	}

	// 1. Fetch metric data and detect anomalies
	points, err := c.prom.GetRangeData(metricQuery, duration, "1m")
	if err != nil {
		return "", fmt.Errorf("metric query failed: %w", err)
	}

	values := make([]float64, len(points))
	for i, p := range points {
		values[i] = p.Value
	}
	mean, std := meanStd(values)
	anomalies := detectZScore(points, mean, std, 2.5) // slightly lower threshold for correlation

	// 2. Fetch log data
	logs, err := c.loki.QueryLogs(logQuery, duration, 200)
	if err != nil {
		return "", fmt.Errorf("log query failed: %w", err)
	}

	// 3. Correlate: find log entries near anomaly timestamps (±2 min window)
	var sb strings.Builder
	sb.WriteString("## Signal Correlation Report\n\n")
	sb.WriteString(fmt.Sprintf("**Metric**: `%s`\n", metricQuery))
	sb.WriteString(fmt.Sprintf("**Logs**: `%s`\n", logQuery))
	sb.WriteString(fmt.Sprintf("**Window**: %s\n\n", duration))

	sb.WriteString(fmt.Sprintf("### Metric Analysis\n"))
	sb.WriteString(fmt.Sprintf("- Data points: %d | Mean: %.4f | StdDev: %.4f\n", len(points), mean, std))
	sb.WriteString(fmt.Sprintf("- Anomalies detected: %d\n\n", len(anomalies)))

	sb.WriteString(fmt.Sprintf("### Log Analysis\n"))
	sb.WriteString(fmt.Sprintf("- Total log entries: %d\n\n", len(logs)))

	if len(anomalies) == 0 {
		sb.WriteString("### Correlation: ✅ No metric anomalies to correlate\n")
		sb.WriteString("The metric appears stable. Log entries may be routine.\n")
		return sb.String(), nil
	}

	sb.WriteString("### Correlated Events\n\n")
	correlations := 0
	for i, a := range anomalies {
		if i >= 10 {
			break
		}
		// Find logs within ±2 minutes of anomaly
		var nearbyLogs []tools.LogEntry
		for _, log := range logs {
			// Simple time proximity check
			logTime := log.Timestamp
			anomTime := points[0].Timestamp // placeholder
			for _, p := range points {
				if p.Value == a.value {
					anomTime = p.Timestamp
					break
				}
			}

			timeDiff := logTime.Sub(anomTime)
			if timeDiff > -120e9 && timeDiff < 120e9 { // ±2 minutes in nanoseconds
				nearbyLogs = append(nearbyLogs, log)
			}
		}

		if len(nearbyLogs) > 0 {
			correlations++
			direction := "⬆️ SPIKE"
			if a.value < mean {
				direction = "⬇️ DROP"
			}
			sb.WriteString(fmt.Sprintf("**Anomaly %d**: %s at value=%.4f (z=%.2f)\n", i+1, direction, a.value, a.zscore))
			sb.WriteString("Nearby logs:\n")
			for j, log := range nearbyLogs {
				if j >= 5 {
					sb.WriteString(fmt.Sprintf("  ... and %d more logs\n", len(nearbyLogs)-5))
					break
				}
				sb.WriteString(fmt.Sprintf("  [%s] %s\n", log.Timestamp.Format("15:04:05"), truncateStr(log.Line, 150)))
			}
			sb.WriteString("\n")
		}
	}

	if correlations == 0 {
		sb.WriteString("No temporal correlations found between metric anomalies and log events.\n")
		sb.WriteString("This could mean the issue is not reflected in these specific logs.\n")
	} else {
		sb.WriteString(fmt.Sprintf("\n**Summary**: %d of %d anomalies have correlated log events.\n", correlations, len(anomalies)))
	}

	return sb.String(), nil
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
