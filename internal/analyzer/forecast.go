package analyzer

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/3490165738/sre-mcp-server/internal/tools"
)

type Forecaster struct {
	prom *tools.PrometheusClient
}

func NewForecaster(prom *tools.PrometheusClient) *Forecaster {
	return &Forecaster{prom: prom}
}

func (f *Forecaster) Forecast(ctx context.Context, params map[string]any) (string, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query parameter is required")
	}

	threshold := 90.0
	if t, ok := params["threshold"].(float64); ok {
		threshold = t
	}
	lookback := getStr(params, "lookback", "7d")

	// Fetch historical data
	points, err := f.prom.GetRangeData(query, lookback, "15m")
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	if len(points) < 20 {
		return "Insufficient data points for forecasting (need at least 20). Try a longer lookback window.", nil
	}

	// Prepare data for linear regression
	// x = time offset in hours from first point, y = metric value
	t0 := points[0].Timestamp
	xs := make([]float64, len(points))
	ys := make([]float64, len(points))
	for i, p := range points {
		xs[i] = p.Timestamp.Sub(t0).Hours()
		ys[i] = p.Value
	}

	// Linear regression: y = slope * x + intercept
	slope, intercept, r2 := linearRegression(xs, ys)

	// Current value
	currentHours := points[len(points)-1].Timestamp.Sub(t0).Hours()
	currentValue := ys[len(ys)-1]

	// Predict when threshold will be hit
	var hitTime time.Time
	var hoursUntilHit float64
	willHit := false

	if slope > 0 && currentValue < threshold {
		// threshold = slope * x + intercept => x = (threshold - intercept) / slope
		hitHours := (threshold - intercept) / slope
		hoursFromNow := hitHours - currentHours
		if hoursFromNow > 0 {
			hitTime = time.Now().Add(time.Duration(hoursFromNow * float64(time.Hour)))
			hoursUntilHit = hoursFromNow
			willHit = true
		}
	}

	// Forecast values at key future points
	type forecastPoint struct {
		label string
		hours float64
	}
	futurePoints := []forecastPoint{
		{"1 hour", 1},
		{"6 hours", 6},
		{"24 hours", 24},
		{"3 days", 72},
		{"7 days", 168},
	}

	// Build report
	var sb strings.Builder
	sb.WriteString("## 📈 Capacity Forecast Report\n\n")
	sb.WriteString(fmt.Sprintf("**Query**: `%s`\n", query))
	sb.WriteString(fmt.Sprintf("**Lookback**: %s | **Threshold**: %.1f\n", lookback, threshold))
	sb.WriteString(fmt.Sprintf("**Data points**: %d\n\n", len(points)))

	sb.WriteString("### Current Status\n")
	sb.WriteString(fmt.Sprintf("- Current value: **%.2f**\n", currentValue))
	sb.WriteString(fmt.Sprintf("- Threshold: **%.1f**\n", threshold))
	remainPct := ((threshold - currentValue) / threshold) * 100
	if remainPct > 0 {
		sb.WriteString(fmt.Sprintf("- Remaining headroom: **%.1f%%**\n", remainPct))
	}
	sb.WriteString("\n")

	sb.WriteString("### Trend Analysis (Linear Regression)\n")
	sb.WriteString(fmt.Sprintf("- Slope: %.6f per hour (%.4f per day)\n", slope, slope*24))
	sb.WriteString(fmt.Sprintf("- R² (goodness of fit): %.4f", r2))
	if r2 < 0.5 {
		sb.WriteString(" ⚠️ Low confidence — data has high variance\n")
	} else if r2 < 0.8 {
		sb.WriteString(" ⚡ Moderate confidence\n")
	} else {
		sb.WriteString(" ✅ High confidence\n")
	}

	trendDir := "📈 **Increasing**"
	if slope < 0 {
		trendDir = "📉 **Decreasing**"
	} else if math.Abs(slope) < 0.001 {
		trendDir = "➡️ **Stable**"
	}
	sb.WriteString(fmt.Sprintf("- Trend: %s (%.4f/hour)\n\n", trendDir, slope))

	// Threshold prediction
	sb.WriteString("### ⏰ Threshold Prediction\n\n")
	if willHit {
		urgency := "🟢"
		if hoursUntilHit < 24 {
			urgency = "🔴"
		} else if hoursUntilHit < 72 {
			urgency = "🟡"
		}
		sb.WriteString(fmt.Sprintf("%s **Predicted to hit %.1f in %.1f hours (%.1f days)**\n", urgency, threshold, hoursUntilHit, hoursUntilHit/24))
		sb.WriteString(fmt.Sprintf("   Estimated time: **%s**\n\n", hitTime.Format("2006-01-02 15:04")))
	} else if slope <= 0 {
		sb.WriteString("✅ Trend is flat or decreasing — threshold will not be hit at current rate.\n\n")
	} else if currentValue >= threshold {
		sb.WriteString("🔴 **Already exceeded threshold!** Immediate action required.\n\n")
	}

	// Future predictions
	sb.WriteString("### Predicted Values\n\n")
	sb.WriteString("| Timeframe | Predicted Value | Status |\n")
	sb.WriteString("|-----------|----------------|--------|\n")
	for _, fp := range futurePoints {
		futureHours := currentHours + fp.hours
		predicted := slope*futureHours + intercept
		status := "✅ OK"
		if predicted >= threshold {
			status = "🔴 OVER"
		} else if predicted >= threshold*0.9 {
			status = "⚠️ WARNING"
		}
		sb.WriteString(fmt.Sprintf("| +%s | %.2f | %s |\n", fp.label, predicted, status))
	}
	sb.WriteString("\n")

	// Recommendations
	sb.WriteString("### 💡 Recommendations\n\n")
	if currentValue >= threshold {
		sb.WriteString("1. **CRITICAL**: Already over threshold — scale immediately or clean up resources\n")
		sb.WriteString("2. Investigate top consumers with more granular queries\n")
		sb.WriteString("3. Set up PagerDuty/On-call alert if not already configured\n")
	} else if willHit && hoursUntilHit < 24 {
		sb.WriteString("1. **URGENT**: Less than 24 hours until threshold — plan scaling now\n")
		sb.WriteString("2. Check for any runaway processes or data growth anomalies\n")
		sb.WriteString("3. Consider horizontal scaling or resource limit increases\n")
	} else if willHit && hoursUntilHit < 168 {
		sb.WriteString("1. Schedule capacity expansion within this week\n")
		sb.WriteString("2. Review if growth rate is expected or anomalous\n")
		sb.WriteString("3. Consider implementing auto-scaling (HPA/KEDA) if not already\n")
	} else {
		sb.WriteString("1. Capacity looks healthy — continue routine monitoring\n")
		sb.WriteString("2. Re-run this forecast weekly as part of capacity planning\n")
	}

	return sb.String(), nil
}

// linearRegression computes y = slope*x + intercept via ordinary least squares.
// Returns slope, intercept, and R² (coefficient of determination).
func linearRegression(xs, ys []float64) (slope, intercept, r2 float64) {
	n := float64(len(xs))
	if n < 2 {
		return 0, 0, 0
	}

	var sumX, sumY, sumXY, sumX2 float64
	for i := range xs {
		sumX += xs[i]
		sumY += ys[i]
		sumXY += xs[i] * ys[i]
		sumX2 += xs[i] * xs[i]
	}

	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0, sumY / n, 0
	}

	slope = (n*sumXY - sumX*sumY) / denom
	intercept = (sumY - slope*sumX) / n

	// R²
	meanY := sumY / n
	var ssTot, ssRes float64
	for i := range xs {
		predicted := slope*xs[i] + intercept
		ssRes += (ys[i] - predicted) * (ys[i] - predicted)
		ssTot += (ys[i] - meanY) * (ys[i] - meanY)
	}
	if ssTot == 0 {
		r2 = 1
	} else {
		r2 = 1 - ssRes/ssTot
	}

	return slope, intercept, r2
}
