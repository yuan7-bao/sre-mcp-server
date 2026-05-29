package analyzer

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/zhujiaqii/sre-mcp-server/internal/tools"
)

type AnomalyDetector struct {
	prom *tools.PrometheusClient
}

func NewAnomalyDetector(prom *tools.PrometheusClient) *AnomalyDetector {
	return &AnomalyDetector{prom: prom}
}

func (a *AnomalyDetector) Detect(ctx context.Context, params map[string]any) (string, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query parameter is required")
	}

	duration := getStr(params, "duration", "6h")
	method := getStr(params, "method", "zscore")

	// Fetch time series data
	points, err := a.prom.GetRangeData(query, duration, "1m")
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	if len(points) < 10 {
		return "Insufficient data points for anomaly detection (need at least 10).", nil
	}

	// Extract values
	values := make([]float64, len(points))
	for i, p := range points {
		values[i] = p.Value
	}

	// Calculate statistics
	mean, stddev := meanStd(values)
	min, max := minMax(values)
	median := percentile(values, 50)
	p95 := percentile(values, 95)
	p99 := percentile(values, 99)

	// Detect anomalies
	var anomalies []anomalyPoint
	switch method {
	case "zscore":
		anomalies = detectZScore(points, mean, stddev, 3.0)
	case "sigma3":
		anomalies = detectSigma3(points, mean, stddev)
	case "iqr":
		anomalies = detectIQR(points, values)
	default:
		anomalies = detectZScore(points, mean, stddev, 3.0)
	}

	// Build report
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Anomaly Detection Report\n\n"))
	sb.WriteString(fmt.Sprintf("**Query**: `%s`\n", query))
	sb.WriteString(fmt.Sprintf("**Window**: %s | **Method**: %s | **Data points**: %d\n\n", duration, method, len(points)))

	sb.WriteString("### Statistics\n")
	sb.WriteString(fmt.Sprintf("- Mean: %.4f\n", mean))
	sb.WriteString(fmt.Sprintf("- Std Dev: %.4f\n", stddev))
	sb.WriteString(fmt.Sprintf("- Min: %.4f | Max: %.4f\n", min, max))
	sb.WriteString(fmt.Sprintf("- Median: %.4f | P95: %.4f | P99: %.4f\n\n", median, p95, p99))

	if len(anomalies) == 0 {
		sb.WriteString("### Result: ✅ No anomalies detected\n")
		sb.WriteString("All data points are within normal statistical bounds.\n")
	} else {
		sb.WriteString(fmt.Sprintf("### Result: 🚨 %d anomalies detected\n\n", len(anomalies)))
		for i, a := range anomalies {
			direction := "⬆️ SPIKE"
			if a.value < mean {
				direction = "⬇️ DROP"
			}
			sb.WriteString(fmt.Sprintf("%d. [%s] %s value=%.4f (z-score=%.2f, deviation=%.1f%%)\n",
				i+1, a.timestamp.Format("15:04:05"), direction, a.value, a.zscore, a.deviation))
			if i >= 19 {
				sb.WriteString(fmt.Sprintf("... and %d more anomalies\n", len(anomalies)-20))
				break
			}
		}

		sb.WriteString("\n### Interpretation\n")
		spikes := 0
		drops := 0
		for _, a := range anomalies {
			if a.value > mean {
				spikes++
			} else {
				drops++
			}
		}
		if spikes > drops {
			sb.WriteString("- Primarily **upward spikes** — possible causes: traffic surge, resource leak, cascading failure\n")
		} else if drops > spikes {
			sb.WriteString("- Primarily **downward drops** — possible causes: service outage, network partition, dependency failure\n")
		} else {
			sb.WriteString("- **Mixed anomalies** — possible causes: oscillating behavior, flapping alerts, noisy metric\n")
		}
	}

	return sb.String(), nil
}

type anomalyPoint struct {
	timestamp interface{ Format(string) string }
	value     float64
	zscore    float64
	deviation float64 // percentage deviation from mean
}

func detectZScore(points []tools.DataPoint, mean, std, threshold float64) []anomalyPoint {
	if std == 0 {
		return nil
	}
	var result []anomalyPoint
	for _, p := range points {
		z := (p.Value - mean) / std
		if math.Abs(z) > threshold {
			dev := ((p.Value - mean) / mean) * 100
			result = append(result, anomalyPoint{
				timestamp: p.Timestamp,
				value:     p.Value,
				zscore:    z,
				deviation: dev,
			})
		}
	}
	return result
}

func detectSigma3(points []tools.DataPoint, mean, std float64) []anomalyPoint {
	return detectZScore(points, mean, std, 3.0)
}

func detectIQR(points []tools.DataPoint, values []float64) []anomalyPoint {
	q1 := percentile(values, 25)
	q3 := percentile(values, 75)
	iqr := q3 - q1
	lower := q1 - 1.5*iqr
	upper := q3 + 1.5*iqr

	mean, _ := meanStd(values)

	var result []anomalyPoint
	for _, p := range points {
		if p.Value < lower || p.Value > upper {
			z := 0.0
			if iqr > 0 {
				z = (p.Value - (q1+q3)/2) / iqr
			}
			dev := ((p.Value - mean) / mean) * 100
			result = append(result, anomalyPoint{
				timestamp: p.Timestamp,
				value:     p.Value,
				zscore:    z,
				deviation: dev,
			})
		}
	}
	return result
}

// --- math helpers ---

func meanStd(vals []float64) (float64, float64) {
	n := float64(len(vals))
	if n == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	mean := sum / n

	variance := 0.0
	for _, v := range vals {
		d := v - mean
		variance += d * d
	}
	std := math.Sqrt(variance / n)
	return mean, std
}

func minMax(vals []float64) (float64, float64) {
	if len(vals) == 0 {
		return 0, 0
	}
	min, max := vals[0], vals[0]
	for _, v := range vals {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}

func percentile(vals []float64, p float64) float64 {
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)

	idx := (p / 100) * float64(len(sorted)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper || upper >= len(sorted) {
		return sorted[lower]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

func getStr(params map[string]any, key, def string) string {
	if v, ok := params[key].(string); ok && v != "" {
		return v
	}
	return def
}
