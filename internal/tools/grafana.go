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

type GrafanaClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewGrafanaClient(baseURL, apiKey string) *GrafanaClient {
	return &GrafanaClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (g *GrafanaClient) get(path string, params url.Values) ([]byte, error) {
	u := fmt.Sprintf("%s%s", g.baseURL, path)
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, _ := http.NewRequest("GET", u, nil)
	if g.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+g.apiKey)
	}
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Grafana request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Grafana returned %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (g *GrafanaClient) ListDashboards(ctx context.Context, params map[string]any) (string, error) {
	body, err := g.get("/api/search?type=dash-db", url.Values{})
	if err != nil {
		return "", err
	}

	var dashboards []map[string]any
	json.Unmarshal(body, &dashboards)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Grafana Dashboards (%d)\n\n", len(dashboards)))
	for i, d := range dashboards {
		title, _ := d["title"].(string)
		uid, _ := d["uid"].(string)
		uri, _ := d["uri"].(string)
		tags, _ := d["tags"].([]any)
		tagStr := ""
		for _, t := range tags {
			tagStr += fmt.Sprintf("[%v] ", t)
		}
		sb.WriteString(fmt.Sprintf("%d. **%s** (uid: %s) %s\n   %s\n", i+1, title, uid, tagStr, uri))
	}
	return sb.String(), nil
}

func (g *GrafanaClient) DashboardDetail(ctx context.Context, params map[string]any) (string, error) {
	uid, _ := params["uid"].(string)
	if uid == "" {
		return "", fmt.Errorf("uid parameter is required")
	}

	body, err := g.get(fmt.Sprintf("/api/dashboards/uid/%s", uid), url.Values{})
	if err != nil {
		return "", err
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)

	dashboard, _ := raw["dashboard"].(map[string]any)
	title, _ := dashboard["title"].(string)
	panels, _ := dashboard["panels"].([]any)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Dashboard: %s (uid: %s)\n\n", title, uid))
	sb.WriteString(fmt.Sprintf("Panels: %d\n\n", len(panels)))

	for i, p := range panels {
		panel, _ := p.(map[string]any)
		pTitle, _ := panel["title"].(string)
		pType, _ := panel["type"].(string)
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, pType, pTitle))

		// Extract queries
		if targets, ok := panel["targets"].([]any); ok {
			for _, t := range targets {
				target, _ := t.(map[string]any)
				if expr, ok := target["expr"].(string); ok && expr != "" {
					sb.WriteString(fmt.Sprintf("   Query: `%s`\n", expr))
				}
			}
		}
	}
	return sb.String(), nil
}

func (g *GrafanaClient) Annotations(ctx context.Context, params map[string]any) (string, error) {
	duration := getStringParam(params, "duration", "6h")
	dur, _ := parseDuration(duration)
	from := time.Now().Add(-dur).UnixMilli()
	to := time.Now().UnixMilli()

	vals := url.Values{
		"from": {fmt.Sprintf("%d", from)},
		"to":   {fmt.Sprintf("%d", to)},
	}
	if tags, ok := params["tags"].(string); ok && tags != "" {
		vals.Set("tags", tags)
	}

	body, err := g.get("/api/annotations", vals)
	if err != nil {
		return "", err
	}

	var annotations []map[string]any
	json.Unmarshal(body, &annotations)

	if len(annotations) == 0 {
		return fmt.Sprintf("No annotations found in the last %s.", duration), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Annotations (last %s, %d found)\n\n", duration, len(annotations)))

	for i, a := range annotations {
		text, _ := a["text"].(string)
		tags, _ := a["tags"].([]any)
		created, _ := a["created"].(float64)
		ts := time.UnixMilli(int64(created))

		var tagStrs []string
		for _, t := range tags {
			tagStrs = append(tagStrs, fmt.Sprintf("%v", t))
		}

		sb.WriteString(fmt.Sprintf("%d. [%s] %s", i+1, ts.Format("01/02 15:04"), text))
		if len(tagStrs) > 0 {
			sb.WriteString(fmt.Sprintf(" {%s}", strings.Join(tagStrs, ", ")))
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

func (g *GrafanaClient) AlertRules(ctx context.Context, params map[string]any) (string, error) {
	body, err := g.get("/api/v1/provisioning/alert-rules", url.Values{})
	if err != nil {
		// Fallback to legacy API
		body, err = g.get("/api/ruler/grafana/api/v1/rules", url.Values{})
		if err != nil {
			return "", err
		}
	}

	var rules []map[string]any
	json.Unmarshal(body, &rules)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Grafana Alert Rules (%d)\n\n", len(rules)))

	for i, r := range rules {
		title, _ := r["title"].(string)
		condition, _ := r["condition"].(string)
		sb.WriteString(fmt.Sprintf("%d. %s (condition: %s)\n", i+1, title, condition))
	}
	return sb.String(), nil
}
