# 🔭 SRE-MCP-Server

> 一个连接 Prometheus / Grafana / Loki 的 MCP Server，让 Claude 直接查询监控指标、分析告警、智能定位根因。

## 项目定位

这不是一个玩具 demo —— 它是一个**可以写进简历的 AIOps 实战项目**，面向字节跳动 SRE 岗位的技术面试。

## 架构总览

```
┌─────────────────────────────────────────────────────────┐
│                    Claude Desktop / Code                 │
│                     (MCP Host / Client)                  │
└──────────────────────┬──────────────────────────────────┘
                       │ JSON-RPC 2.0 (stdio / Streamable HTTP)
                       ▼
┌─────────────────────────────────────────────────────────┐
│                   SRE-MCP-Server (Go)                    │
│                                                          │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌─────────┐ │
│  │Prometheus│  │  Grafana  │  │   Loki   │  │   AI    │ │
│  │  Tools   │  │  Tools   │  │  Tools   │  │Analyzer │ │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬────┘ │
│       │              │              │              │      │
└───────┼──────────────┼──────────────┼──────────────┼──────┘
        ▼              ▼              ▼              ▼
  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐
  │Prometheus│  │ Grafana  │  │   Loki   │  │  本地    │
  │  :9090   │  │  :3000   │  │  :3100   │  │ 异常检测 │
  └──────────┘  └──────────┘  └──────────┘  └──────────┘
```

## 暴露的 MCP Tools（18 个）

### Prometheus 指标查询（6 个）
| Tool | 功能 |
|------|------|
| `promql_instant` | 即时查询（当前值） |
| `promql_range` | 范围查询（时间序列） |
| `prom_targets` | 查看监控目标状态 |
| `prom_alerts` | 获取当前告警 |
| `prom_metadata` | 指标元数据查询 |
| `prom_label_values` | 标签值枚举 |

### Grafana Dashboard（4 个）
| Tool | 功能 |
|------|------|
| `grafana_dashboards` | 列出所有 Dashboard |
| `grafana_dashboard_detail` | 获取 Dashboard 配置 |
| `grafana_annotations` | 查询事件标注 |
| `grafana_alerts` | Grafana 告警规则状态 |

### Loki 日志查询（4 个）
| Tool | 功能 |
|------|------|
| `loki_query` | LogQL 日志查询 |
| `loki_labels` | 日志标签枚举 |
| `loki_series` | 日志流发现 |
| `loki_tail` | 实时日志尾随 |

### AI 智能分析（4 个）
| Tool | 功能 |
|------|------|
| `analyze_anomaly` | 时序异常检测（3-sigma / Z-score） |
| `correlate_signals` | Metrics-Logs-Traces 关联分析 |
| `rca_suggest` | 智能根因推荐 |
| `capacity_forecast` | 容量趋势预测（线性回归） |

## 快速开始

### 1. 前置条件

```bash
# 需要 Go 1.21+, Docker, Docker Compose
go version   # >= 1.21
docker --version
```

### 2. 一键启动监控栈

```bash
# 启动 Prometheus + Grafana + Loki + Node Exporter
docker compose -f configs/docker-compose.yml up -d

# 验证
curl http://localhost:9090/-/healthy     # Prometheus
curl http://localhost:3000/api/health     # Grafana
curl http://localhost:3100/ready          # Loki
```

### 3. 编译 & 运行 MCP Server

```bash
go build -o sre-mcp-server ./cmd/
./sre-mcp-server --prometheus-url http://localhost:9090 \
                 --grafana-url http://localhost:3000 \
                 --grafana-api-key YOUR_KEY \
                 --loki-url http://localhost:3100
```

### 4. 接入 Claude Desktop

编辑 `~/Library/Application Support/Claude/claude_desktop_config.json`（macOS）
或 `%APPDATA%\Claude\claude_desktop_config.json`（Windows）：

```json
{
  "mcpServers": {
    "sre-monitor": {
      "command": "/path/to/sre-mcp-server",
      "args": [
        "--prometheus-url", "http://localhost:9090",
        "--grafana-url", "http://localhost:3000",
        "--grafana-api-key", "YOUR_GRAFANA_API_KEY",
        "--loki-url", "http://localhost:3100"
      ]
    }
  }
}
```

重启 Claude Desktop，你就能直接对 Claude 说：

- "当前有哪些告警在触发？"
- "查一下过去1小时 CPU 使用率最高的5个节点"
- "分析一下 order-service 的延迟为什么升高了"
- "预测一下磁盘空间什么时候会满"

## 项目结构

```
sre-mcp-server/
├── cmd/
│   └── main.go              # 入口：解析参数、注册 tools、启动 MCP
├── internal/
│   ├── server/
│   │   └── mcp.go           # MCP Server 核心（JSON-RPC handler）
│   ├── tools/
│   │   ├── prometheus.go    # Prometheus 查询工具
│   │   ├── grafana.go       # Grafana API 工具
│   │   └── loki.go          # Loki 日志工具
│   └── analyzer/
│       ├── anomaly.go       # 异常检测算法
│       ├── correlator.go    # 信号关联
│       ├── rca.go           # 根因分析
│       └── forecast.go      # 容量预测
├── configs/
│   ├── docker-compose.yml   # 一键启动监控栈
│   ├── prometheus.yml       # Prometheus 配置
│   └── alerting-rules.yml   # 告警规则
├── scripts/
│   └── load-generator.sh    # 模拟负载（用于演示/测试）
├── go.mod
├── go.sum
└── README.md
```

## 简历怎么写

```
SRE 智能监控 MCP Server                              个人项目 · 2026.xx
• 基于 MCP（Model Context Protocol）开发 Go 语言 SRE 监控服务，
  暴露 18 个 MCP Tools 连接 Prometheus / Grafana / Loki，实现
  LLM 对监控数据的自然语言查询与智能分析
• 实现时序异常检测（3-sigma / Z-score）、Metrics-Logs 关联分析、
  基于拓扑的智能根因推荐、线性回归容量预测等 AIOps 能力
• 支持 stdio 与 Streamable HTTP 双传输模式，集成 Claude Desktop / 
  Claude Code，实际部署于 K8s 集群可观测性场景
```

## License

MIT
