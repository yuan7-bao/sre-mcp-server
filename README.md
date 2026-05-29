<div align="center">

# 🔭 SRE-MCP-Server

**让 Claude 直接查询 Prometheus / Grafana / Loki，用自然语言做 SRE 监控与故障诊断。**

基于 [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) 构建，Go 语言实现。

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![MCP](https://img.shields.io/badge/MCP-2024--11--05-blueviolet)](https://modelcontextprotocol.io/)
[![License](https://img.shields.io/badge/License-MIT-green)](LICENSE)
[![Prometheus](https://img.shields.io/badge/Prometheus-E6522C?logo=prometheus&logoColor=white)](https://prometheus.io/)
[![Grafana](https://img.shields.io/badge/Grafana-F46800?logo=grafana&logoColor=white)](https://grafana.com/)

</div>

---

## 这是什么？

一个 MCP Server，把 SRE 日常用的监控系统暴露给大语言模型（Claude），实现：

- 🗣️ **自然语言查监控** — "当前 CPU 使用率多少？" → 自动转 PromQL 查询
- 📊 **智能异常检测** — 基于 Z-score / 3-sigma / IQR 的时序异常识别
- 🔗 **信号关联分析** — Metrics 异常 + Error Logs 自动做时间窗口关联
- 🎯 **智能根因推荐** — 输入症状描述，自动跑多维诊断，按置信度排序
- 📈 **容量趋势预测** — 线性回归预测资源何时打爆阈值

## 架构

```
┌─────────────────────────────────────────────────┐
│           Claude Desktop / Claude Code           │
│                 (MCP Host)                       │
└────────────────────┬────────────────────────────┘
                     │ JSON-RPC 2.0 (stdio)
                     ▼
┌─────────────────────────────────────────────────┐
│              SRE-MCP-Server (Go)                 │
│                                                  │
│  ┌────────────┐ ┌──────────┐ ┌───────────────┐  │
│  │ Prometheus │ │ Grafana  │ │     Loki      │  │
│  │   Tools    │ │  Tools   │ │    Tools      │  │
│  │  (6 个)    │ │ (4 个)   │ │   (4 个)      │  │
│  └─────┬──────┘ └────┬─────┘ └──────┬────────┘  │
│        │             │              │            │
│  ┌─────┴─────────────┴──────────────┴─────────┐  │
│  │          AI Analyzer (4 个)                 │  │
│  │  异常检测 · 信号关联 · 根因分析 · 容量预测    │  │
│  └────────────────────────────────────────────┘  │
└────────────────────────────────────────────────┘
         │              │              │
    Prometheus       Grafana         Loki
     :9090           :3000          :3100
```

## 18 个 MCP Tools

### Prometheus 查询（6 个）
| Tool | 功能 |
|------|------|
| `promql_instant` | 即时查询 — 获取指标当前值 |
| `promql_range` | 范围查询 — 获取时间序列数据 |
| `prom_targets` | 查看所有监控目标及健康状态 |
| `prom_alerts` | 获取当前触发的告警 |
| `prom_metadata` | 查询指标元数据（类型/含义） |
| `prom_label_values` | 枚举标签值（发现服务/实例） |

### Grafana 看板（4 个）
| Tool | 功能 |
|------|------|
| `grafana_dashboards` | 列出所有 Dashboard |
| `grafana_dashboard_detail` | 获取 Dashboard 面板与查询详情 |
| `grafana_annotations` | 查询事件标注（部署/变更/事故） |
| `grafana_alerts` | Grafana 告警规则与状态 |

### Loki 日志（4 个）
| Tool | 功能 |
|------|------|
| `loki_query` | LogQL 日志查询 |
| `loki_labels` | 日志标签枚举 |
| `loki_series` | 日志流发现 |
| `loki_stats` | 日志量统计 |

### AI 智能分析（4 个）
| Tool | 功能 |
|------|------|
| `analyze_anomaly` | 时序异常检测（Z-score / 3-sigma / IQR） |
| `correlate_signals` | Metrics-Logs 跨信号关联分析 |
| `rca_suggest` | 智能根因推荐（自动多维诊断） |
| `capacity_forecast` | 容量趋势预测（线性回归） |

## 快速开始

### 前置条件

- Go 1.21+
- Docker & Docker Compose
- [Claude Desktop](https://claude.ai/download)

### 1. 启动监控栈

```bash
git clone https://github.com/3490165738/sre-mcp-server.git
cd sre-mcp-server
docker compose -f configs/docker-compose.yml up -d
```

验证服务：
- Prometheus: http://localhost:9090
- Grafana: http://localhost:3000 (admin/admin)
- Loki: http://localhost:3100/ready

### 2. 编译

```bash
go mod tidy
go build -o sre-mcp-server.exe ./cmd/   # Windows
go build -o sre-mcp-server ./cmd/       # macOS / Linux
```

### 3. 接入 Claude Desktop

编辑 Claude Desktop 配置（Settings → Developer → Edit Config）：

```json
{
  "mcpServers": {
    "sre-monitor": {
      "command": "C:\\path\\to\\sre-mcp-server.exe",
      "args": [
        "--prometheus-url", "http://localhost:9090",
        "--grafana-url", "http://localhost:3000",
        "--grafana-api-key", "",
        "--loki-url", "http://localhost:3100"
      ]
    }
  }
}
```

重启 Claude Desktop，开始对话：

```
💬 "当前有哪些监控目标？"
💬 "查一下过去1小时的 HTTP 请求量趋势"  
💬 "分析一下 order-service 为什么延迟升高了"
💬 "预测磁盘什么时候会满"
```

### 4. 模拟故障（可选）

```bash
chmod +x scripts/load-generator.sh
./scripts/load-generator.sh spike    # 流量尖峰
./scripts/load-generator.sh errors   # 错误注入
./scripts/load-generator.sh chaos    # 混合故障
```

## 项目结构

```
sre-mcp-server/
├── cmd/
│   └── main.go                  # 入口：参数解析、Tool 注册、启动 MCP
├── internal/
│   ├── server/
│   │   └── mcp.go               # MCP Server 核心（JSON-RPC 2.0）
│   ├── tools/
│   │   ├── prometheus.go        # Prometheus HTTP API 封装
│   │   ├── grafana.go           # Grafana API 封装
│   │   └── loki.go              # Loki API 封装
│   └── analyzer/
│       ├── anomaly.go           # 异常检测（Z-score / 3-sigma / IQR）
│       ├── correlator.go        # Metrics-Logs 信号关联
│       ├── rca.go               # 智能根因分析
│       └── forecast.go          # 容量预测（线性回归）
├── configs/
│   ├── docker-compose.yml       # 一键启动监控栈
│   ├── prometheus.yml           # Prometheus 采集配置
│   └── alerting-rules.yml       # 告警规则
├── scripts/
│   └── load-generator.sh        # 故障模拟脚本
├── go.mod
└── README.md
```

## 技术选型

| 组件 | 选型 | 理由 |
|------|------|------|
| 语言 | Go | SRE 领域主流语言，Prometheus/K8s 生态原生 |
| 协议 | MCP (JSON-RPC 2.0) | Anthropic 开放标准，跨 LLM 通用 |
| 指标 | Prometheus | CNCF 毕业项目，行业事实标准 |
| 看板 | Grafana | 可观测性可视化事实标准 |
| 日志 | Loki | 轻量级，与 Grafana 生态深度集成 |
| 异常检测 | Z-score / IQR | 经典统计方法，可解释性强 |
| 预测 | 线性回归 | 简单有效，适合容量趋势场景 |

## 后续规划

- [ ] 接入 Jaeger 实现 Traces 链路追踪（可观测性第三支柱）
- [ ] 基于 OpenTelemetry 统一信号采集
- [ ] 添加 K8s API 工具（Pod 状态 / 事件 / 日志）
- [ ] Streamable HTTP 传输模式支持远程部署
- [ ] 更多异常检测算法（EWMA / 动态阈值）
- [ ] Web UI 管理面板

## License

MIT
