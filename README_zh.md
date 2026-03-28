# FluxGraph 🌌

FluxGraph 是一个生产级、高并发的 Golang AI 智能体（Agent）开发框架。它采用状态机图编排范式，相当于智能体服务的“微型操作系统”，开箱即用地提供了状态流转、可回溯记忆、可观测性以及原生的智能体间（A2A, Agent-to-Agent）通信协议。

## 核心特性 🚀

- **图结构编排引擎**：将复杂的 Agent 工作流定义为有向图结构（`graph` 和 `engine` 模块），轻松管理执行流、确定性跳转和工具使用循环。
- **双层分离记忆系统**：
  - **热冷分离**：热层使用 Redis 提供高速多轮会话追踪和任务队列处理。
  - **向量记忆**：冷层使用 PostgreSQL 结合 `pgvector` 存储全量对话流水，并原生集成内置的检索增强（RAG）工具，在历史上下文越界时提供高效的语义捞回。
- **Agent 到 Agent (A2A) 通信**：提供开箱即用的 HTTP 与 gRPC 微服务网关。使您的 Agent 生来就是一个标准化的微服务，支持与其他 FluxGraph Agent 构建高并发协作网络（Swarm）。
- **企业级生产力插件**：
  - **可观测性**：全面继承 OpenTelemetry（OTel）链路追踪和 Prometheus 监控指标（Hooks 挂载）。
  - **安全性**：全局审计日志（Audit Log）及用于净化的输出守卫（Output GuardHook）。
  - **高可用**：内建基于断路器的 LLM `providers`（OpenAI & Anthropic），支持模型的高效退避和降级重试。

## 快速开始 🛠️

### 前置依赖
- Go 1.22+
- Docker & Docker-Compose（用于一键拉起依赖组件）

### 环境安装

```bash
git clone https://github.com/FluxGraph/fluxgraph.git
cd fluxgraph
go mod download
```

### 启动基础设施

系统强依赖 Redis 与 带有向量拓展的 Postgres，我们提供了开箱即用的编排文件：

```bash
# 以后台模式启动 Postgres (pgvector), Redis, Jaeger 以及 Prometheus
docker-compose up -d

# 提示：持久层的建表及索引扩展语句将通过 ./migrations 目录被容器自动挂载执行。
```

### 启动 Agent 节点

拷贝 `.env.example` 为 `.env` 并填入您的 `OPENAI_API_KEY` 及其他信息，随后执行：

```bash
go run cmd/fluxgraph/main.go
```

## 架构简述 🏗️

- **`/core`**：系统底层原语声明（包括对话片段 `Message`/`Part` 以及状态树 `AgentState`）。
- **`/engine` & `/graph`**：执行引擎组件，全权管理从当前 `Node` 流转至下一个 `Node` 的中间态。
- **`/providers`**：标准化的大模型供应商接入层，支持多模态及结构化流返回。
- **`/storage`**：抽象的 `MemoryStore` 实现，封装热冷两层的调度逻辑。
- **`/tools`**：执行工具的抽象沙盒及注册表（Registry）。

## 开源协议

MIT
