# FluxGraph 🌌

[🇨🇳 简体中文](README_zh.md) | [🇬🇧 English](README.md)

FluxGraph is a production-grade, high-concurrency Go framework for building state-machine based AI Agents. It serves as an operating system layer for your agents, providing built-in orchestration, resilient memory, observability, and Agent-to-Agent (A2A) communication protocols out-of-the-box.

## Core Features 🚀

- **Graph-Based Orchestration**: Define your agent workflows as a directed state graph (`graph` & `engine`). Manage execution flow, deterministic transitions, and tool-use cycles easily.
- **Dual-Tier Memory System**: 
  - **Hot Layer**: Redis-based ephemeral storage for high-speed multi-turn session tracking and task queueing.
  - **Cold Layer**: PostgreSQL + `pgvector` for permanent, semantically-searchable historical context (RAG integrated into the standard ToolRegistry).
- **A2A Protocol Integration**: Seamlessly expose your Agent as an independent microservice over standard HTTP and gRPC protocols, allowing multiple FluxGraph agents to collaborate natively.
- **Enterprise-Ready Plugs**: 
  - **Observability**: OpenTelemetry (OTel) distributed tracing and Prometheus metrics hooks.
  - **Security**: Granular Audit Logs and an Output Guard hook to sanitize and monitor LLM responses.
  - **Resiliency**: Circuit breakers and fallback chains embedded into the LLM `providers` (OpenAI & Anthropic).

## Getting Started 🛠️

### Prerequisites
- Go 1.22+
- Docker & Docker-Compose (for standing up Redis & pgvector)

### Installation

```bash
git clone https://github.com/BaBiQ888/fluxgraph.git
cd fluxgraph
go mod download
```

### Starting Infrastructure

FluxGraph requires Redis and Postgres with the pgvector extension. A `docker-compose.yml` is provided.

```bash
# Start Postgres (pgvector), Redis, Jaeger, and Prometheus services
make docker-up

# Notice: Database tables will be automatically initialized via the mounted ./migrations scripts
```

### Running Your First Agent Server

Make sure to configure your environment variables (refer to `.env.example`), then run:

```bash
cp .env.example .env
make run
```

## Architecture Layers 🏗️

- **`/core`**: Base primitives mimicking production LLM usage (`Message`, `Part`, `AgentState`).
- **`/engine` & `/graph`**: Execution engine responsible for routing context from one `Node` interface to the next.
- **`/providers`**: Unified, resettable bindings to underlying LLMs.
- **`/storage`**: Implementations of the `MemoryStore` interface.
- **`/tools`**: Tool execution bindings, dynamically injectable into the LLM context.
- **`/a2a`**: Agent-to-Agent protocol over gRPC and proper HTTP bindings.

## Contributing
We welcome contributions! Please open an issue or submit a pull request if you'd like to improve FluxGraph.

## License

MIT