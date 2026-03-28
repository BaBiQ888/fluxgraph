<img width="2816" height="1536" alt="Gemini_Generated_Image_foak6mfoak6mfoak" src="https://github.com/user-attachments/assets/97f0b29c-90fa-4056-9df4-319b58a9cd12" />

# FluxGraph 🌌

[🇬🇧 English](README.md) | [🇨🇳 简体中文](README_zh.md)

FluxGraph 是一个生产级、高并发的 Golang AI 智能体（Agent）开发框架。它采用状态机图（State Machine Graph）编排范式，相当于智能体服务的“微型操作系统”。开箱即用地提供了状态流转、可回溯记忆、可观测性以及原生的智能体间（A2A, Agent-to-Agent）通信协议。

---

## 📖 什么是 FluxGraph？ (What is it?)

FluxGraph 并不是另一个仅仅封装大语言模型（LLM）API 的请求库。它是一个**面向生产环境的 Agent 编排与托管系统**。
它将构建智能体的过程抽象为在“图（Graph）”上的节点流转（Nodes & Edges）。在 FluxGraph 中，每一个决策步骤、工具调用或大模型请求都是一个节点，状态（State）在节点之间传递。这种设计使得即使是再复杂的 Multi-Agent 架构或具有多重条件分支的 RAG 流程，也可以被拆解为可理解、可测试的独立单元。

## 🤔 为什么要做这件事情？ (Why build FluxGraph?)

如今开源社区有着大量优秀的 Python Agent 框架（如 LangGraph, AutoGen 等），但在许多企业级核心业务（尤其是高并发、低延迟的微服务后台）中，使用 Go 语言生态有着不可替代的性能优势与易维护性。
我们构建 FluxGraph 主要是为了解决企业在将 Agent 从“实验室（Demo）”推向“生产线（Production）”时面临的痛点：

1. **复杂业务流的失控**：单纯的 Prompt 难以稳定控制复杂的条件流转。我们需要确定性的状态机来兜底逻辑流程。
2. **记忆碎片化与遗忘**：长时间运行的 Agent 容易导致 Context 窗口超载或业务断连，缺乏一个针对生产的“长短期”混合记忆方案。
3. **监控和安全性盲区**：许多框架只管发起对话，却对内部的 Tool Call 动作不具备工业级拦截、限流与审计能力。
4. **孤岛式的 Agent**：Agent 之间缺乏一种标准的高效通信协议协同作业，通常只能通过繁琐的独立 API 胶水代码相互调用。

## 💡 FluxGraph 能够帮助到什么？ (How it helps)

使用 FluxGraph，您可以轻松做到：

- **极速构建高并发 Agent 服务**：利用 Go 原生的协程模型以及 FluxGraph 底层优化的异步引擎处理海量请求。
- **让大模型更稳定地执行长任务**：将庞大的流程式任务通过 Graph 拆分为小的状态节点（Nodes），有效避免 LLM “出戏” 或偏离目标。
- **构建会成长的 Agent**：通过其独有的双层记忆系统（Redis 并发管理 + pgvector 长效语素召回），随着时间的推移，您的 Agent 能记住长达数月前的核心交互信息，并支持 RAG 增强。
- **建立超级微服务（Swarm 网络）**：原生的 A2A 网关能直接将你的单体智能体转变为可通过 HTTP 或 gRPC 被其他 Agent 调用的云原生微服务。

---

## 🚀 核心特性

- **图结构编排引擎**：将复杂的 Agent 工作流定义为有向图结构（`graph` 和 `engine` 模块），轻松管理执行流、确定性跳转和工具使用循环。
- **双层分离记忆系统**：
  - **热层 (Redis)**：毫秒级响应的高速短期多轮会话追踪和任务队列处理。
  - **冷层 (PostgreSQL + pgvector)**：用于持久化存储全量流水的向量数据库，原生集成系统级的检索增强（RAG）组件。
- **标准 A2A 微服务通信**：内置 gRPC 及 HTTP 服务端，生来支持标准化的不同网络/进程系统下 Agent 间的高效协同作业。
- **企业级生产力护航**：
  - **可观测性**：全面集成 OpenTelemetry（OTel）链路追踪与 Prometheus 全局指标透出。
  - **安全性**：具备字段级的全局审计日志（Audit Log）及可扩展的输出拦截守卫（Output GuardHook）。
  - **高可用能力**：内置熔断器（Circuit Breaker）和备用模型降级链，当首选 LLM 不可挂载时可瞬间回退至备用节点安全降级。

---

## 🏗️ 架构解析

整个项目被解耦为具有极低相互依赖性的模块：

- **`/core`**：定义系统底层的领域模型及原语（例如 `Message`, `Part`, 以及全局只读状态树 `AgentState`），所有模块共享通用词汇。
- **`/engine` & `/graph`**：图引擎心智。管理节点（Nodes）的执行、边（Edges）的逻辑跳转，并承载上下文状态传递。
- **`/providers`**：标准化的统一 LLM 模型接入接口层屏蔽不同厂商品牌格式。
- **`/storage` / `/memory`**：记忆栈与状态缓存的封装层（负责控制热冷分离、向量检索等逻辑）。
- **`/tools` / `/interfaces`**：第三方工具侧的标准化接入网关和接口定义。
- **`/a2a`**：微服务网络通信组件，承担 Agent 对外双向交互。

---

## 💻 如何使用 (How to use)

FluxGraph 的开发体验极为自然，您只需定义好不同的业务处理 "节点"，并将它们组装为图状流水线即可开始工作。

```go
package main

import (
    "context"
    "github.com/BaBiQ888/fluxgraph/core"
    "github.com/BaBiQ888/fluxgraph/engine"
    "github.com/BaBiQ888/fluxgraph/graph"
    "github.com/BaBiQ888/fluxgraph/interfaces"
)

// 定义你的处理节点
type MyBusinessNode struct { id string }
func (n *MyBusinessNode) ID() string { return n.id }
func (n *MyBusinessNode) Process(ctx context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
    // 处理核心业务逻辑后，将新的结果推入状态切片之中
    state = state.WithMessage(core.Message{
        Role: core.RoleAssistant, 
        Parts: []core.Part{{Type: core.PartTypeText, Text: "任务处理完成！"}},
    })
    return &interfaces.NodeResult{State: state}, nil
}

func main() {
    // 1. 初始化图组装器
    g := graph.NewGraph()
    
    // 2. 将编写的大量业务节点连入边缘网络中
    nodeA := &MyBusinessNode{id: "step_one"}
    g.AddNode(nodeA)
    g.SetEntrypoint("step_one")
    
    // 3. 将编排好的 Graph 装载进引擎直接发起启动
    ctx := context.Background()
    fluxEngine := engine.NewEngine(g)
    _, err := fluxEngine.Run(ctx, core.NewAgentState())
    if err != nil {
        panic(err)
    }
}
```

## 🧩 如何集成 (How to integrate)

不论您是希望在现有 Go 服务生态中低成本嵌入智能体，还是将 FluxGraph 项目独立拆分为一个大后端池供跨服务系统调用，它都能保持极高的集成可塑：

- **工具侧定制 (Tools)**：实现接口内极简的 `Execute()` 和 `Schema()` 两方法包装你的本地函数，大语言模型即可自动获得例如“连接 MySQL 检索”、“主动回复 Email邮件”或是“自动发起企业微信推文”的能力。
- **跨平台对接 (A2A)**：启动 `a2a.Server` 挂载引擎，程序在后台启动原生的 gRPC 端点监听端口。您的 Ruby / Python / Java 服务，或是另一个 FluxGraph 网络均可通过 PB (ProtoBuf) 进行高密度低延迟的握手和多轮通讯能力。完美兼容企业内的负载发现生态机制。
- **监控追踪 (OTel)**：只需在核心包启动首部中一行执行 `observability.InitTracer()` 即便自动把每一个调用及大模型的请求节点记录发送到内部现有的 Jaeger 后台上进行全息展现和链路追踪。

---

## 🛠️ 快速开始

### 前置依赖
- Go 1.22+
- Docker & Docker-Compose（用于一键起周边数据库支持的基站环境）

### 环境安装

```bash
git clone https://github.com/BaBiQ888/fluxgraph.git
cd fluxgraph
go mod download
```

### 启动基础设施

本框架默认强制依赖 Redis (用作快热栈表和限流分发) 以及安装好向量映射能力 (pgvector) 的 Postgres 持久结构。

```bash
# 后台拉起并暴露所需的核心支撑层、监控面板和追踪探针
make docker-up

# 注意事项：DB 内所需的标准扩展 Schema 以及 Table Migration 文件会在装载过程中自动执行初始化拉起。
```

### 启动测试 Agent 节点

按照官方定义模板拷贝填写 API KEYs : 

```bash
cp .env.example .env
make run
```

---

## 🤝 参与贡献
这是一个开源生态系统建设的起点，无论您修复了一个 Bug，提交了一篇文案，还是新增了一个工具驱动，我们都非常期待来自社区的拉取请求 (Pull Requests) 与 Issue 支持！

## 📄 开源协议
MIT
