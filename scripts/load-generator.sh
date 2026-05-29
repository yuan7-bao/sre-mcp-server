#!/bin/bash
# load-generator.sh — 模拟不同类型的负载，用于测试 MCP Server 的异常检测和根因分析能力
#
# Usage:
#   ./scripts/load-generator.sh normal     # 正常流量
#   ./scripts/load-generator.sh spike      # 流量尖峰
#   ./scripts/load-generator.sh errors     # 模拟 5xx 错误
#   ./scripts/load-generator.sh slow       # 模拟延迟升高
#   ./scripts/load-generator.sh chaos      # 混合故障

set -e

DEMO_APP_URL="${DEMO_APP_URL:-http://localhost:8081}"
MODE="${1:-normal}"

echo "🔧 Load Generator — Mode: $MODE"
echo "   Target: $DEMO_APP_URL"
echo ""

# 正常流量
generate_normal() {
    echo "📊 Generating normal traffic (5 req/s)..."
    while true; do
        for i in $(seq 1 5); do
            curl -s -o /dev/null -w "%{http_code}" "$DEMO_APP_URL/metrics" &
        done
        wait
        sleep 1
    done
}

# 流量尖峰 — 用于测试异常检测
generate_spike() {
    echo "📈 Generating traffic spike..."
    echo "   Phase 1: Normal (30s)"
    for t in $(seq 1 30); do
        for i in $(seq 1 3); do
            curl -s -o /dev/null "$DEMO_APP_URL/" &
        done
        wait
        sleep 1
    done
    
    echo "   Phase 2: SPIKE (60s, 50 req/s)"
    for t in $(seq 1 60); do
        for i in $(seq 1 50); do
            curl -s -o /dev/null "$DEMO_APP_URL/" &
        done
        wait
        sleep 1
    done
    
    echo "   Phase 3: Recovery (30s)"
    for t in $(seq 1 30); do
        for i in $(seq 1 3); do
            curl -s -o /dev/null "$DEMO_APP_URL/" &
        done
        wait
        sleep 1
    done
    echo "✅ Spike simulation complete"
}

# 模拟错误 — 用于测试告警和关联分析
generate_errors() {
    echo "💥 Generating error traffic..."
    while true; do
        # Normal requests
        for i in $(seq 1 3); do
            curl -s -o /dev/null "$DEMO_APP_URL/" &
        done
        # Requests to non-existent endpoints (404/500)
        for i in $(seq 1 5); do
            curl -s -o /dev/null "$DEMO_APP_URL/nonexistent-$(shuf -i 1-100 -n 1)" &
            curl -s -o /dev/null "$DEMO_APP_URL/error" &
        done
        wait
        sleep 1
    done
}

# 模拟高延迟 — 用于测试 P99 延迟告警
generate_slow() {
    echo "🐢 Generating slow requests..."
    while true; do
        for i in $(seq 1 10); do
            # Add artificial delay via timeout
            curl -s -o /dev/null --max-time 5 "$DEMO_APP_URL/" &
        done
        wait
        sleep 0.5
    done
}

# CPU stress — 用于测试资源告警和容量预测
generate_cpu_stress() {
    echo "🔥 Generating CPU stress..."
    # Use dd or yes to burn CPU
    for i in $(seq 1 $(nproc)); do
        yes > /dev/null &
    done
    echo "   Running for 60 seconds..."
    sleep 60
    kill $(jobs -p) 2>/dev/null
    echo "✅ CPU stress complete"
}

# 混合故障场景
generate_chaos() {
    echo "🌪️ Chaos mode — cycling through different failure patterns"
    echo ""
    
    echo "Phase 1/4: Normal baseline (20s)"
    timeout 20 bash -c 'while true; do curl -s -o /dev/null http://localhost:8081/; sleep 0.5; done' || true
    
    echo "Phase 2/4: Error burst (20s)"  
    timeout 20 bash -c 'while true; do curl -s -o /dev/null http://localhost:8081/error; sleep 0.1; done' || true
    
    echo "Phase 3/4: Traffic spike (20s)"
    timeout 20 bash -c 'while true; do for i in $(seq 1 30); do curl -s -o /dev/null http://localhost:8081/ & done; wait; sleep 1; done' || true
    
    echo "Phase 4/4: Recovery (20s)"
    timeout 20 bash -c 'while true; do curl -s -o /dev/null http://localhost:8081/; sleep 1; done' || true
    
    echo "✅ Chaos simulation complete"
}

case "$MODE" in
    normal)  generate_normal ;;
    spike)   generate_spike ;;
    errors)  generate_errors ;;
    slow)    generate_slow ;;
    cpu)     generate_cpu_stress ;;
    chaos)   generate_chaos ;;
    *)
        echo "Unknown mode: $MODE"
        echo "Usage: $0 {normal|spike|errors|slow|cpu|chaos}"
        exit 1
        ;;
esac
