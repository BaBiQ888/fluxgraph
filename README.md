<img width="2816" height="1536" alt="Gemini_Generated_Image_foak6mfoak6mfoak" src="https://github.com/user-attachments/assets/74a71e71-e11c-45b2-8f86-9b6af953ca0a" />

# FluxGraph 🌌

[🇨🇳 简体中文](README_zh.md) | [🇬🇧 English](README.md)

FluxGraph is a production-grade, high-concurrency Go framework for building state-machine based AI Agents. It serves as an operating system layer for your agents, providing built-in orchestration, resilient memory, observability, and native Agent-to-Agent (A2A) communication protocols out-of-the-box.

---

## 📖 What is FluxGraph?

FluxGraph is not just another wrapper library around LLM APIs. It is a **production-ready Agent orchestration and hosting system**.
It models the lifecycle of building intelligent agents as a flow on a Graph comprised of Nodes and Edges. In FluxGraph, every decision step, tool invocation, or LLM request acts as a node, with state passing between them. This design ensures that even the most complex Multi-Agent architectures or multi-branch RAG workflows can be decoupled into comprehensible, testable, independent units.

## 🤔 Why build FluxGraph?

While the open-source community boasts excellent Python-based frameworks (such as LangGraph and AutoGen), the Go ecosystem offers unparalleled performance advantages and maintainability for enterprise-level core business logic—especially in high-concurrency, low-latency microservice architectures.
We built FluxGraph to address the pain points companies face when trying to move an Agent out of the "Demo Lab" into a "Production Line":

1. **Loss of Control in Complex Workflows**: Relying solely on prompt engineering often fails to reliably manage complex conditional flows. A deterministic state machine is needed to back up the logic.
2. **Fragmented Memory & Amnesia**: Long-running agents easily overload the Context window or lose operational continuity. There is a lack of "long/short-term" hybrid memory solutions tailored for production.
3. **Blind Spots in Monitoring & Security**: Many frameworks only initiate conversation requests, lacking industrial-grade interception, rate limiting, and auditing capabilities for internal Tool Call actions.
4. **Siloed Agents**: Without a standard, efficient communication protocol, agents can usually only collaborate via cumbersome, custom API glue code.

## 💡 How FluxGraph Helps

With FluxGraph, you can effortlessly achieve the following:

- **Rapidly Build High-Concurrency Agent Services**: Process massive requests using Go's native goroutines combined with FluxGraph's optimized underlying asynchronous engine.
- **Keep LLMs Focused on Long-Running Tasks**: Break down massive workflows into smaller state Nodes within the Graph, effectively preventing the LLM from hallucinating or missing its goals.
- **Build Agents with Growing Context**: Leverage the unique dual-tier memory system (Redis for high-speed concurrency + PostgreSQL `pgvector` for semantic recall) to allow your Agent to remember core interactions from months ago, deeply integrating RAG.
- **Form Super Microservices (Swarm Networks)**: The native A2A gateway instantly turns your standalone agent into a cloud-native microservice that can be called by other Agents over standard HTTP or gRPC.

---

## 🚀 Core Features

- **Graph-Based Orchestration**: Define complex agent workflows as directed graphs (`graph` and `engine` modules). Easily manage execution flow, deterministic state transitions, and tool-use cycles.
- **Dual-Tier Memory System**:
  - **Hot Layer (Redis)**: Millisecond-latency multi-turn session tracking and task queue processing.
  - **Cold Layer (PostgreSQL + pgvector)**: Persistent vector-based store of the entire timeline, seamlessly integrating robust Retrieval-Augmented Generation (RAG).
- **Standard A2A Microservice Communication**: Built-in gRPC and HTTP servers, natively supporting standardized agent-to-agent collaboration across different architectures or languages.
- **Enterprise-Grade Safeguards**:
  - **Observability**: Fully integrated OpenTelemetry (OTel) distributed tracing and global Prometheus metrics.
  - **Security**: Field-level global Audit Logs and extensible Output GuardHooks for sanitizing LLM responses.
  - **High Availability**: Built-in Circuit Breakers and LLM Fallback chains to smoothly degrade to backup models (e.g., Anthropic or local setups) if the primary provider (e.g., OpenAI) fails.

---

## 🏗️ Architecture Layers

The framework is decoupled into highly independent modules:

- **`/core`**: Defines the base domain models and primitives (e.g., `Message`, `Part`, and the purely functional `AgentState`). All packages share this common vocabulary.
- **`/engine` & `/graph`**: The execution orchestrator. Manages execution of `Nodes`, logical branching of `Edges`, and routes the context passing.
- **`/providers`**: A standardized binding layer for accessing various LLM providers, normalizing prompt inputs and streaming responses.
- **`/storage` / `/memory`**：Implementations of the memory stack (handling hybrid separation, vector searching, etc.).
- **`/tools` / `/interfaces`**：Standardized gateway for injecting custom tool capabilities (Tool Registry) so the LLM can see what local functions are available.
- **`/a2a`**：The microservice networking communication component bridging the standalone agent with the outside world.

---

## 💻 How to Use

Developing with FluxGraph feels perfectly natural. You merely define isolated business "Nodes" and assemble them into a directed graph pipeline.

```go
package main

import (
    "context"
    "github.com/FluxGraph/fluxgraph/core"
    "github.com/FluxGraph/fluxgraph/engine"
    "github.com/FluxGraph/fluxgraph/graph"
    "github.com/FluxGraph/fluxgraph/interfaces"
)

// Define your processing Node
type MyBusinessNode struct { id string }
func (n *MyBusinessNode) ID() string { return n.id }
func (n *MyBusinessNode) Process(ctx context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
    // Inject business logic and append resulting context into the state slice
    state = state.WithMessage(core.Message{
        Role: core.RoleAssistant, 
        Parts: []core.Part{{Type: core.PartTypeText, Text: "Task processed successfully!"}},
    })
    return &interfaces.NodeResult{State: state}, nil
}

func main() {
    // 1. Initialize the Graph assembler
    g := graph.NewGraph()
    
    // 2. Wire nodes together
    nodeA := &MyBusinessNode{id: "step_one"}
    g.AddNode(nodeA)
    g.SetEntrypoint("step_one")
    
    // 3. Mount the configured graph into the engine and start
    ctx := context.Background()
    fluxEngine := engine.NewEngine(g)
    _, err := fluxEngine.Run(ctx, core.NewAgentState())
    if err != nil {
        panic(err)
    }
}
```

## 🧩 How to Integrate

Whether you're embedding an intelligent agent into an existing Go backend or creating a standalone swarm node to serve multiple clients, FluxGraph remains exceptionally flexible:

- **Custom Tool Integration (`/tools`)**: Simply implement the `Execute()` and `Schema()` interface methods to wrap your local functions, magically empowering the LLM to run SQL queries, scrape APIs, or send automated emails.
- **Cross-Platform RPC (`/a2a`)**: Boot the `a2a.Server` to attach your graph to an active network endpoint. Because it listens on standard gRPC (and REST fallback), client applications written in Ruby, Python, Java, or another FluxGraph peer can establish high-throughput, low-latency communication over standard Protocol Buffers—perfectly compatible with K8S Load Balancers.
- **Observability (OTel)**: Inject a single line `observability.InitTracer()` at the top of your function, and every node execution or LLM prompt sequence is seamlessly pushed to your company's Jaeger instances for full-stack distributed tracing.

---

## 🛠️ Getting Started

### Prerequisites
- Go 1.22+
- Docker & Docker-Compose (for standing up Redis & pgvector instances easily)

### Installation

```bash
git clone https://github.com/BaBiQ888/fluxgraph.git
cd fluxgraph
go mod download
```

### Starting Infrastructure

FluxGraph natively mandates Redis (for rapid context caching and rate limits) and PostgreSQL with the pgvector extension. A `docker-compose.yml` is provided.

```bash
# Start Postgres (pgvector), Redis, Jaeger, and Prometheus services in the background
make docker-up

# Notice: All necessary SQL dialect extensions and underlying table structures will be automatically migrated.
```

### Running Your First Agent Server

Make sure to configure your environment variables (refer to `.env.example`), setting up your LLM credentials, then run:

```bash
cp .env.example .env
make run
```

---

## 🤝 Contributing
This marks the beginning of a vibrant open-source ecosystem. Whether you identify a Bug, fix a typo in the documentation, or contribute a brand-new Tool integration, we aggressively welcome Issue reports and Pull Requests from the community!

## 📄 License
MIT
