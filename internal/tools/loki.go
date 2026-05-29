package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type LokiClient struct {
	baseURL    string
	httpClient *PrometheusClient // reuse HTTP helper
}

func NewLokiClient(baseURL string) *LokiClient {
	return &LokiClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: NewPrometheusClient(baseURL), // reuse get() method
	}
}

func (l *LokiClient) Query(ctx context.Context, params map[string]any) (string, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query parameter is required")
	}

	duration := getStringParam(params, "duration", "1h")
	limit := 100
	if lim, ok := params["limit"].(float64); ok {
		limit = int(lim)
	}

	dur, _ := parseDuration(duration)
	end := time.Now()
	start := end.Add(-dur)

	vals := url.Values{
		"query":     {query},
		"start":     {fmt.Sprintf("%d", start.UnixNano())},
		"end":       {fmt.Sprintf("%d", end.UnixNano())},
		"limit":     {fmt.Sprintf("%d", limit)},
		"direction": {"backward"},
	}

	body, err := l.httpClient.get("/loki/api/v1/query_range", vals)
	if err != nil {
		return "", err
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)

	data, _ := raw["data"].(map[string]any)
	results, _ := data["result"].([]any)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Loki Logs: `%s` (last %s)\n\n", query, duration))

	totalLines := 0
	for _, r := range results {
		result, _ := r.(map[string]any)
		stream, _ := result["stream"].(map[string]any)
		values, _ := result["values"].([]any)
		totalLines += len(values)

		// Stream labels
		var labels []string
		for k, v := range stream {
			labels = append(labels, fmt.Sprintf("%s=%q", k, v))
		}
		sb.WriteString(fmt.Sprintf("### Stream: {%s}\n", strings.Join(labels, ", ")))

		// Log lines (show up to 20 per stream)
		shown := 0
		for _, v := range values {
			pair, _ := v.([]any)
			if len(pair) == 2 {
				tsNano, _ := pair[0].(string)
				line, _ := pair[1].(string)
				ts := parseNanoTimestamp(tsNano)
				sb.WriteString(fmt.Sprintf("[%s] %s\n", ts.Format("15:04:05"), truncate(line, 200)))
				shown++
				if shown >= 20 {
					sb.WriteString(fmt.Sprintf("... (%d more lines in this stream)\n", len(values)-20))
					break
				}
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("**Total**: %d streams, %d log lines\n", len(results), totalLines))
	return sb.String(), nil
}

func (l *LokiClient) Labels(ctx context.Context, params map[string]any) (string, error) {
	body, err := l.httpClient.get("/loki/api/v1/labels", url.Values{})
	if err != nil {
		return "", err
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	data, _ := raw["data"].([]any)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Loki Labels (%d)\n\n", len(data)))
	for _, v := range data {
		sb.WriteString(fmt.Sprintf("- %v\n", v))
	}
	return sb.String(), nil
}

func (l *LokiClient) Series(ctx context.Context, params map[string]any) (string, error) {
	match, _ := params["match"].(string)
	if match == "" {
		return "", fmt.Errorf("match parameter is required")
	}

	end := time.Now()
	start := end.Add(-1 * time.Hour)

	vals := url.Values{
		"match[]": {match},
		"start":   {fmt.Sprintf("%d", start.UnixNano())},
		"end":     {fmt.Sprintf("%d", end.UnixNano())},
	}

	body, err := l.httpClient.get("/loki/api/v1/series", vals)
	if err != nil {
		return "", err
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	data, _ := raw["data"].([]any)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Loki Series matching `%s` (%d found)\n\n", match, len(data)))

	for i, s := range data {
		series, _ := s.(map[string]any)
		var labels []string
		for k, v := range series {
			labels = append(labels, fmt.Sprintf("%s=%q", k, v))
		}
		sb.WriteString(fmt.Sprintf("%d. {%s}\n", i+1, strings.Join(labels, ", ")))
		if i >= 19 {
			sb.WriteString(fmt.Sprintf("... and %d more\n", len(data)-20))
			break
		}
	}
	return sb.String(), nil
}

func (l *LokiClient) Stats(ctx context.Context, params map[string]any) (string, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query parameter is required")
	}
	duration := getStringParam(params, "duration", "1h")

	// Use log volume query via metric query
	volQuery := fmt.Sprintf("sum(count_over_time(%s [1m]))", query)
	dur, _ := parseDuration(duration)
	end := time.Now()
	start := end.Add(-dur)

	vals := url.Values{
		"query": {volQuery},
		"start": {fmt.Sprintf("%d", start.UnixNano())},
		"end":   {fmt.Sprintf("%d", end.UnixNano())},
		"step":  {"60"},
	}

	body, err := l.httpClient.get("/loki/api/v1/query_range", vals)
	if err != nil {
		return "", err
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Log Volume Stats: `%s` (last %s)\n\n", query, duration))

	data, _ := raw["data"].(map[string]any)
	results, _ := data["result"].([]any)

	for _, r := range results {
		result, _ := r.(map[string]any)
		values, _ := result["values"].([]any)
		total := 0.0
		for _, v := range values {
			pair, _ := v.([]any)
			if len(pair) == 2 {
				total += parseFloat(fmt.Sprintf("%v", pair[1]))
			}
		}
		sb.WriteString(fmt.Sprintf("Total log lines: %.0f\n", total))
		if len(values) > 0 {
			sb.WriteString(fmt.Sprintf("Average per minute: %.1f\n", total/float64(len(values))))
		}
	}
	return sb.String(), nil
}

// QueryLogs is a helper for analyzer modules.
func (l *LokiClient) QueryLogs(query, duration string, limit int) ([]LogEntry, error) {
	dur, _ := parseDuration(duration)
	end := time.Now()
	start := end.Add(-dur)

	vals := url.Values{
		"query":     {query},
		"start":     {fmt.Sprintf("%d", start.UnixNano())},
		"end":       {fmt.Sprintf("%d", end.UnixNano())},
		"limit":     {fmt.Sprintf("%d", limit)},
		"direction": {"backward"},
	}

	body, err := l.httpClient.get("/loki/api/v1/query_range", vals)
	if err != nil {
		return nil, err
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	data, _ := raw["data"].(map[string]any)
	results, _ := data["result"].([]any)

	var entries []LogEntry
	for _, r := range results {
		result, _ := r.(map[string]any)
		stream, _ := result["stream"].(map[string]any)
		values, _ := result["values"].([]any)
		for _, v := range values {
			pair, _ := v.([]any)
			if len(pair) == 2 {
				tsStr, _ := pair[0].(string)
				line, _ := pair[1].(string)
				entries = append(entries, LogEntry{
					Timestamp: parseNanoTimestamp(tsStr),
					Line:      line,
					Labels:    stream,
				})
			}
		}
	}
	return entries, nil
}

type LogEntry struct {
	Timestamp time.Time
	Line      string
	Labels    map[string]any
}

// --- helpers ---

func parseNanoTimestamp(s string) time.Time {
	var ns int64
	fmt.Sscanf(s, "%d", &ns)
	return time.Unix(0, ns)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
