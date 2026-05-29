package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PrometheusClient wraps the Prometheus HTTP API.
type PrometheusClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewPrometheusClient(baseURL string) *PrometheusClient {
	return &PrometheusClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (p *PrometheusClient) get(path string, params url.Values) ([]byte, error) {
	u := fmt.Sprintf("%s%s", p.baseURL, path)
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	resp, err := p.httpClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body failed: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Prometheus returned %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// InstantQuery executes an instant PromQL query.
func (p *PrometheusClient) InstantQuery(ctx context.Context, params map[string]any) (string, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query parameter is required")
	}

	vals := url.Values{"query": {query}, "time": {time.Now().Format(time.RFC3339)}}
	body, err := p.get("/api/v1/query", vals)
	if err != nil {
		return "", err
	}

	// Parse and format for readability
	var result map[string]any
	json.Unmarshal(body, &result)
	return formatPromResult(result, "instant", query)
}

// RangeQuery executes a range PromQL query.
func (p *PrometheusClient) RangeQuery(ctx context.Context, params map[string]any) (string, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query parameter is required")
	}

	duration := getStringParam(params, "duration", "1h")
	step := getStringParam(params, "step", "1m")

	dur, err := parseDuration(duration)
	if err != nil {
		return "", fmt.Errorf("invalid duration %q: %w", duration, err)
	}

	end := time.Now()
	start := end.Add(-dur)

	vals := url.Values{
		"query": {query},
		"start": {start.Format(time.RFC3339)},
		"end":   {end.Format(time.RFC3339)},
		"step":  {step},
	}

	body, err := p.get("/api/v1/query_range", vals)
	if err != nil {
		return "", err
	}

	var result map[string]any
	json.Unmarshal(body, &result)
	return formatPromResult(result, "range", query)
}

// Targets returns all scrape targets and their status.
func (p *PrometheusClient) Targets(ctx context.Context, params map[string]any) (string, error) {
	body, err := p.get("/api/v1/targets", url.Values{})
	if err != nil {
		return "", err
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)

	data, _ := raw["data"].(map[string]any)
	activeTargets, _ := data["activeTargets"].([]any)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Prometheus Targets (%d active)\n\n", len(activeTargets)))

	upCount, downCount := 0, 0
	for _, t := range activeTargets {
		target, _ := t.(map[string]any)
		health, _ := target["health"].(string)
		labels, _ := target["labels"].(map[string]any)
		job, _ := labels["job"].(string)
		instance, _ := labels["instance"].(string)

		status := "🟢"
		if health != "up" {
			status = "🔴"
			downCount++
		} else {
			upCount++
		}
		sb.WriteString(fmt.Sprintf("%s %s (%s) — %s\n", status, job, instance, health))
	}

	sb.WriteString(fmt.Sprintf("\n**Summary**: %d up, %d down\n", upCount, downCount))
	return sb.String(), nil
}

// Alerts returns all currently firing alerts.
func (p *PrometheusClient) Alerts(ctx context.Context, params map[string]any) (string, error) {
	body, err := p.get("/api/v1/alerts", url.Values{})
	if err != nil {
		return "", err
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)

	data, _ := raw["data"].(map[string]any)
	alerts, _ := data["alerts"].([]any)

	if len(alerts) == 0 {
		return "✅ No alerts currently firing.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 🚨 Active Alerts (%d)\n\n", len(alerts)))

	for i, a := range alerts {
		alert, _ := a.(map[string]any)
		labels, _ := alert["labels"].(map[string]any)
		annotations, _ := alert["annotations"].(map[string]any)
		state, _ := alert["state"].(string)
		name, _ := labels["alertname"].(string)
		severity, _ := labels["severity"].(string)
		summary, _ := annotations["summary"].(string)
		activeAt, _ := alert["activeAt"].(string)

		icon := "⚠️"
		if severity == "critical" {
			icon = "🔴"
		}

		sb.WriteString(fmt.Sprintf("%d. %s **%s** [%s] — %s\n", i+1, icon, name, severity, state))
		if summary != "" {
			sb.WriteString(fmt.Sprintf("   Summary: %s\n", summary))
		}
		if activeAt != "" {
			sb.WriteString(fmt.Sprintf("   Active since: %s\n", activeAt))
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// Metadata returns metric metadata.
func (p *PrometheusClient) Metadata(ctx context.Context, params map[string]any) (string, error) {
	metric, _ := params["metric"].(string)
	if metric == "" {
		return "", fmt.Errorf("metric parameter is required")
	}

	vals := url.Values{"metric": {metric}}
	body, err := p.get("/api/v1/metadata", vals)
	if err != nil {
		return "", err
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	data, _ := raw["data"].(map[string]any)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Metric: %s\n\n", metric))

	if entries, ok := data[metric].([]any); ok {
		for _, e := range entries {
			entry, _ := e.(map[string]any)
			sb.WriteString(fmt.Sprintf("- Type: %s\n", entry["type"]))
			sb.WriteString(fmt.Sprintf("- Help: %s\n", entry["help"]))
			if unit, ok := entry["unit"].(string); ok && unit != "" {
				sb.WriteString(fmt.Sprintf("- Unit: %s\n", unit))
			}
		}
	} else {
		sb.WriteString("No metadata found for this metric.\n")
	}
	return sb.String(), nil
}

// LabelValues returns all values for a given label.
func (p *PrometheusClient) LabelValues(ctx context.Context, params map[string]any) (string, error) {
	label, _ := params["label"].(string)
	if label == "" {
		return "", fmt.Errorf("label parameter is required")
	}

	body, err := p.get(fmt.Sprintf("/api/v1/label/%s/values", label), url.Values{})
	if err != nil {
		return "", err
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	data, _ := raw["data"].([]any)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Label values for '%s' (%d values)\n\n", label, len(data)))
	for _, v := range data {
		sb.WriteString(fmt.Sprintf("- %v\n", v))
	}
	return sb.String(), nil
}

// GetRangeData is a helper used by analyzer modules to get raw time series data.
func (p *PrometheusClient) GetRangeData(query, duration, step string) ([]DataPoint, error) {
	dur, err := parseDuration(duration)
	if err != nil {
		return nil, err
	}

	end := time.Now()
	start := end.Add(-dur)

	vals := url.Values{
		"query": {query},
		"start": {start.Format(time.RFC3339)},
		"end":   {end.Format(time.RFC3339)},
		"step":  {step},
	}

	body, err := p.get("/api/v1/query_range", vals)
	if err != nil {
		return nil, err
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)

	data, _ := raw["data"].(map[string]any)
	results, _ := data["result"].([]any)

	var points []DataPoint
	for _, r := range results {
		result, _ := r.(map[string]any)
		values, _ := result["values"].([]any)
		for _, v := range values {
			pair, _ := v.([]any)
			if len(pair) == 2 {
				ts, _ := pair[0].(float64)
				val := parseFloat(fmt.Sprintf("%v", pair[1]))
				points = append(points, DataPoint{
					Timestamp: time.Unix(int64(ts), 0),
					Value:     val,
				})
			}
		}
	}
	return points, nil
}

// DataPoint represents a single time series data point.
type DataPoint struct {
	Timestamp time.Time
	Value     float64
}

// --- helpers ---

func formatPromResult(raw map[string]any, queryType, query string) (string, error) {
	data, _ := raw["data"].(map[string]any)
	resultType, _ := data["resultType"].(string)
	results, _ := data["result"].([]any)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## PromQL: `%s`\nResult type: %s | %d series\n\n", query, resultType, len(results)))

	for i, r := range results {
		result, _ := r.(map[string]any)
		metric, _ := result["metric"].(map[string]any)

		// Format metric labels
		var labelParts []string
		for k, v := range metric {
			labelParts = append(labelParts, fmt.Sprintf("%s=%q", k, v))
		}
		sb.WriteString(fmt.Sprintf("**Series %d**: {%s}\n", i+1, strings.Join(labelParts, ", ")))

		if queryType == "instant" {
			value, _ := result["value"].([]any)
			if len(value) == 2 {
				sb.WriteString(fmt.Sprintf("  Value: %v\n", value[1]))
			}
		} else {
			values, _ := result["values"].([]any)
			sb.WriteString(fmt.Sprintf("  Data points: %d\n", len(values)))
			// Show first and last 3 points
			show := 3
			for j, v := range values {
				if j >= show && j < len(values)-show {
					if j == show {
						sb.WriteString("  ...\n")
					}
					continue
				}
				pair, _ := v.([]any)
				if len(pair) == 2 {
					ts := time.Unix(int64(pair[0].(float64)), 0)
					sb.WriteString(fmt.Sprintf("  [%s] %v\n", ts.Format("15:04:05"), pair[1]))
				}
			}
		}
		sb.WriteString("\n")

		// Limit output to first 10 series
		if i >= 9 {
			sb.WriteString(fmt.Sprintf("... and %d more series (truncated)\n", len(results)-10))
			break
		}
	}
	return sb.String(), nil
}

func getStringParam(params map[string]any, key, defaultVal string) string {
	if v, ok := params[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}

func parseDuration(s string) (time.Duration, error) {
	// Support Prometheus-style durations: 5m, 1h, 6h, 24h, 7d
	if strings.HasSuffix(s, "d") {
		s = strings.TrimSuffix(s, "d")
		days := parseFloat(s)
		return time.Duration(days*24) * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}
