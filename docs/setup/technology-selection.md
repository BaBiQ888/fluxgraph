FluxGraph — 完整技术选型

---

## 一、核心语言

| 维度 | 选型 | 理由 |
|------|------|------|
| **主语言** | **Go 1.22+** | goroutine 原生并发完美支撑工具并发执行与 EventLoop；interface 系统天然匹配插拔架构；无 GIL 限制；编译产物单二进制，部署极简 |
| **备选语言** | Python 3.12+（asyncio） | 若团队以 Python 为主，可替换；但 goroutine vs asyncio 在并发工具执行场景下，Go 的心智模型更简单、性能更稳定 |
| **语言版本锁定策略** | go.mod 严格锁定小版本 | 防止 CI 与本地环境偏差 |

---

## 二、HTTP 与 RPC 层

| 组件 | 选型 | 说明 |
|------|------|------|
| **HTTP 路由框架** | `chi` v5 | 极度轻量、100% 兼容 `net/http` 标准接口、中间件链清晰；拒绝使用 Gin（魔法较多、测试复杂） |
| **HTTP 服务器** | 标准库 `net/http` + `chi` | 不引入额外运行时依赖 |
| **gRPC** | `google.golang.org/grpc` v1.6x | 官方标准；A2A 协议 gRPC Binding 必须 |
| **Proto 代码生成** | `protoc` + `protoc-gen-go` + `protoc-gen-go-grpc` | 从官方 `a2a.proto` 直接生成，不手写 |
| **SSE 推送** | 标准库 `net/http` 手写 | SSE 协议极简（text/event-stream），无需引入库；自己控制心跳和重连逻辑更透明 |
| **HTTP Client（A2AClient 用）** | 标准库 `net/http` + 自定义重试封装 | 不引入 `resty` 等三方库，减少依赖树 |
| **请求超时控制** | `context.WithTimeout` 标准库 | 统一通过 FluxGraph Context 传递，不在 HTTP 层单独管理 |

---

## 三、数据存储层

### 3.1 会话状态存储（热层）

| 组件 | 选型 | 说明 |
|------|------|------|
| **Redis 客户端** | `redis/go-redis` v9 | 支持 Cluster / Sentinel / 单机三种模式统一接口；Pipeline、事务、Pub/Sub 完整支持 |
| **Redis 版本要求** | Redis 7.0+ | 使用 Redis Functions 做原子性检查点操作；Redis 7 的 ACL 支持多租户键空间隔离 |
| **Redis 数据结构选型** | Hash（Task 元数据）+ List（Messages / Artifacts）+ Sorted Set（检查点索引）+ String（AgentState 快照） | 按场景选型，不一刀切用 String |

### 3.2 持久化存储（冷层）

| 组件 | 选型 | 说明 |
|------|------|------|
| **关系型数据库** | PostgreSQL 16+ | JSON 支持（JSONB）完美存储 AgentState；pgvector 扩展支持向量检索，不需要额外引入向量数据库 |
| **PG 驱动** | `jackc/pgx` v5 | 纯 Go 实现，性能最优；支持批量查询（batch）和 COPY 协议；拒绝使用 `database/sql` + `lib/pq` 组合 |
| **SQL 查询构建** | `Masterminds/squirrel` | 类型安全的查询构建器，不使用 ORM（GORM 在复杂场景下容易失控） |
| **数据库迁移** | `golang-migrate/migrate` | 纯 SQL 迁移文件，本地和 CI 均可运行；支持 Up / Down 双向 |
| **连接池** | pgx 内置连接池（`pgxpool`） | 不引入额外连接池中间件 |

### 3.3 向量检索（RAG 记忆层）

| 组件 | 选型 | 说明 |
|------|------|------|
| **向量存储** | PostgreSQL + `pgvector` 扩展 | Phase 1-3 不需要独立向量数据库；pgvector 支持余弦相似度、IVFFlat / HNSW 索引 |
| **Embedding 生成** | OpenAI `text-embedding-3-small` API | 1536 维；后续可扩展为本地 Embedding 模型 |
| **备选向量 DB** | Qdrant（当 pgvector 性能瓶颈时） | 纯 Go 客户端，REST API，迁移成本低 |

---

## 四、消息队列 / 事件总线

| 场景 | 选型 | 说明 |
|------|------|------|
| **单进程内部** | Go channel（带缓冲） | Phase 1 InMemoryEventBus 的底层实现；零依赖 |
| **多实例扩展** | Redis Pub/Sub（`go-redis`） | Phase 2-3 首选；与 Redis 热层复用同一连接池，运维成本低 |
| **高吞吐企业级** | Apache Kafka（`segmentio/kafka-go`） | Phase 4 可选升级；适用于每秒万级事件的场景；`kafka-go` 是纯 Go 实现，不依赖 cgo |
| **WebHook 异步推送队列** | Redis List（`RPUSH` + 后台消费 goroutine） | 轻量实现 WebHook 可靠推送；Kafka 版本下迁移到 Topic 消费 |

---

## 五、LLM 接入层

| 组件 | 选型 | 说明 |
|------|------|------|
| **OpenAI 兼容协议** | 自研 `OpenAIProvider`（封装 `net/http`） | 不使用官方 SDK，因为需要对流式、重试、熔断进行精细控制 |
| **Anthropic** | 自研 `AnthropicProvider` | 同上 |
| **本地部署模型** | `OllamaProvider`（Ollama HTTP API） | 与 OpenAI 协议高度兼容，适配成本低 |
| **Embedding 模型** | OpenAI API / 本地 `nomic-embed-text`（via Ollama） | 两种通过 `EmbeddingProvider` 接口统一 |

---

## 六、可观测性

### 6.1 链路追踪

| 组件 | 选型 | 说明 |
|------|------|------|
| **OTel SDK** | `go.opentelemetry.io/otel` v1.2x | 官方 Go SDK；稳定 API，不锁定后端 |
| **导出格式** | OTLP/HTTP（首选）+ OTLP/gRPC | 兼容 Jaeger / Tempo / Datadog / New Relic 全部主流后端 |
| **本地追踪后端** | Jaeger v2（`jaegertracing/jaeger`） | Docker Compose 一键启动；支持 OTLP 直接接入 |
| **生产追踪后端** | Grafana Tempo | 与 Grafana 原生集成；对象存储成本低 |
| **传播协议** | W3C TraceContext + Baggage | A2A 跨框架链路传播的标准选择 |

### 6.2 指标

| 组件 | 选型 | 说明 |
|------|------|------|
| **Metrics 客户端** | `prometheus/client_golang` v1.2x | Go 生态地位稳固；支持 Histogram、Counter、Gauge、Summary |
| **指标后端** | Prometheus 2.x | 本地 + 生产通用 |
| **可视化** | Grafana 10+ | 与 Prometheus + Tempo 原生集成；预置 Dashboard JSON 随代码版本管理 |
| **告警** | Grafana Alerting（替代 Alertmanager） | 统一告警入口，减少组件数量 |

### 6.3 日志

| 组件 | 选型 | 说明 |
|------|------|------|
| **结构化日志库** | `rs/zerolog` | 零内存分配的结构化日志；性能优于 `zap` 和 `slog`（标准库） |
| **日志格式** | JSON 结构化（生产）/ Console 彩色（开发） | zerolog 支持两种模式按环境变量切换 |
| **日志级别** | Debug / Info / Warn / Error / Fatal | 通过配置文件控制，生产默认 Info |
| **审计日志后端** | PostgreSQL 审计表（Append-Only）| 与业务库隔离的独立 Schema |

---

## 七、认证与安全

| 组件 | 选型 | 说明 |
|------|------|------|
| **JWT 库** | `golang-jwt/jwt` v5 | 社区维护活跃；支持 HS256 / RS256 / ES256 |
| **密钥管理** | 环境变量注入（开发）/ HashiCorp Vault（生产） | Vault 支持动态密钥轮换，不硬编码密钥 |
| **TLS** | 标准库 `crypto/tls` + Let's Encrypt（`golang/crypto/acme`） | A2A Server 生产环境必须 HTTPS |
| **HMAC 签名（WebHook）** | 标准库 `crypto/hmac` + `crypto/sha256` | WebHook Signature 验证 |
| **输入清洗** | 自研 `InputSanitizer`（规则引擎 + 正则） | 不引入重型 WAF 库，保持轻量 |
| **速率限制** | `golang.org/x/time/rate`（令牌桶） | 标准库扩展，零外部依赖 |
| **密码哈希** | `golang.org/x/crypto/bcrypt` | ClientSecret 存储用，标准库扩展 |

---

## 八、错误处理与弹性

| 组件 | 选型 | 说明 |
|------|------|------|
| **熔断器** | `sony/gobreaker` | 轻量、稳定、接口简洁；状态机实现符合框架需求 |
| **重试** | 自研（指数退避 + Jitter） | 需要与 Context.Deadline 感知紧密结合，三方库满足度低 |
| **超时控制** | `context.WithTimeout` 标准库 | 贯穿所有层，不使用框架级全局超时 |

---

## 九、A2A 协议相关

| 组件 | 选型 | 说明 |
|------|------|------|
| **Proto 规范来源** | `a2aproject/A2A` 官方仓库 `spec/a2a.proto` | 唯一权威来源，不手写 |
| **JSON-RPC 处理** | 自研轻量解析器（`encoding/json` 标准库） | JSON-RPC 2.0 规范简单，不引入专用库 |
| **AgentCard 序列化** | `encoding/json` 标准库 | 标准 JSON 输出 |
| **Capability 协商** | 自研（对比 AgentCard.Capabilities 字段） | 逻辑简单，不需要协商库 |
| **协议版本管理** | 语义化版本（`Major.Minor`）存于常量 | 对应 A2A Spec § 3.6 |

---

## 十、配置管理

| 组件 | 选型 | 说明 |
|------|------|------|
| **配置库** | `spf13/viper` | 支持 YAML / TOML / ENV 多源配置，优先级可控 |
| **环境变量加载** | `joho/godotenv` | 本地 `.env` 文件加载，生产不使用 |
| **配置结构验证** | `go-playground/validator` | 启动时强制校验必填配置项，快速失败 |
| **敏感配置** | 环境变量（不写入配置文件）/ Vault 动态注入 | API Key / DB Password 等绝不落盘 |
| **配置文件格式** | YAML（主）| 可读性强，支持多层嵌套 |

---

## 十一、测试工具链

| 组件 | 选型 | 说明 |
|------|------|------|
| **单元测试框架** | 标准库 `testing` + `testify` | `testify/assert` 断言清晰；`testify/suite` 组织测试套件 |
| **Mock 生成** | `vektra/mockery` v2 | 从 Interface 自动生成 Mock 实现；不手写 Mock |
| **集成测试容器** | `testcontainers-go` | 为 Redis / PostgreSQL 按需启动真实容器，测试完自动销毁 |
| **HTTP 测试** | 标准库 `net/http/httptest` | 无需真实端口，测试 Handler 快速 |
| **EvalScenario 格式** | YAML 文件 + Go 代码双模式 | YAML 适合非开发者编写场景；Go 代码适合复杂断言 |
| **代码覆盖率** | `go test -coverprofile` + `gcov` 可视化 | 内置工具，不依赖三方 |
| **性能基准测试** | `go test -bench` 标准库 | 关键路径（并发工具执行 / EventLoop Tick）必须有 Benchmark |
| **模糊测试** | `go test -fuzz` 标准库（Go 1.18+） | 针对 InputSanitizer 和 JSON 解析做模糊测试 |

---

## 十二、代码质量

| 组件 | 选型 | 说明 |
|------|------|------|
| **Lint** | `golangci-lint` v1.5x | 集成 50+ 静态分析器；配置文件版本化管理 |
| **必开 Linter** | `staticcheck` / `errcheck` / `gosec` / `govet` / `unused` | 覆盖安全、错误处理、未使用代码三个维度 |
| **格式化** | `gofmt` + `goimports` | 强制格式化，CI 验证不通过则阻断合并 |
| **依赖安全扫描** | `govulncheck` | Go 官方漏洞扫描工具，扫描已知 CVE |
| **API 兼容性检测** | `apidiff`（go.org/x/exp） | 检测 Interface 变更是否破坏向后兼容 |

---

## 十三、CI / CD

| 组件 | 选型 | 说明 |
|------|------|------|
| **CI 平台** | GitHub Actions | 与代码仓库原生集成；矩阵测试（多 Go 版本）开箱即用 |
| **CI 流水线阶段** | lint → unit test → integration test → build → eval harness → security scan | 严格顺序，任一失败阻断后续 |
| **制品仓库** | GitHub Container Registry（GHCR） | Docker 镜像免费托管 |
| **Release 自动化** | `goreleaser` | 多平台二进制编译 + Changelog 生成 + GitHub Release 一键发布 |
| **依赖更新** | Dependabot（GitHub 内置） | 自动提 PR 更新依赖版本，减少人工维护 |

---

## 十四、本地开发环境

| 组件 | 选型 | 说明 |
|------|------|------|
| **容器编排** | Docker Compose v2 | 一键启动 Redis + PostgreSQL + Jaeger + Prometheus + Grafana |
| **热重载** | `cosmtrek/air` | 代码变更自动重编译，开发效率高 |
| **数据库 GUI** | `TablePlus`（Mac）/ `DBeaver` | PostgreSQL 可视化，辅助调试 |
| **Redis GUI** | `RedisInsight`（官方工具） | 查看 AgentState / Task 存储结构 |
| **Makefile 任务** | GNU Make | 统一 `make dev` / `make test` / `make lint` / `make migrate` 入口 |
| **环境变量管理** | `.env.example` 版本化，`.env` 本地绝不提交 | `godotenv` 加载 |

---

## 十五、部署与运维

| 组件 | 选型 | 说明 |
|------|------|------|
| **容器化** | Docker（多阶段构建）| 最终镜像基于 `distroless/static`，体积 < 20MB，无 Shell 攻击面 |
| **编排** | Kubernetes 1.28+ | 生产标准 |
| **包管理** | Helm v3 | FluxGraph 提供官方 Helm Chart |
| **服务网格（可选）** | Istio / Linkerd | mTLS、流量管理；Phase 4 后按需引入 |
| **密钥注入** | Kubernetes Secrets + External Secrets Operator（对接 Vault） | 不在容器镜像中存任何密钥 |
| **健康检查端点** | `GET /health/live`（存活）/ `GET /health/ready`（就绪）| 标准 Kubernetes Probe 接口 |
| **优雅关闭** | `os.Signal` + `Server.Shutdown(ctx)` | 等待进行中的 Session 完成后再退出，最长等待时间可配置 |

---

## 十六、选型决策矩阵总览

```
核心语言       Go 1.22+
├── HTTP       chi v5 + net/http
├── gRPC       google.golang.org/grpc
├── 热存储      Redis 7 (go-redis v9)
├── 冷存储      PostgreSQL 16 (pgx v5 + squirrel)
├── 向量检索    pgvector (Phase 1-3) → Qdrant (按需升级)
├── 事件总线    channel → Redis Pub/Sub → Kafka (分阶段升级)
│
├── 可观测性
│   ├── Tracing   OTel SDK → OTLP → Jaeger/Tempo
│   ├── Metrics   prometheus/client_golang → Grafana
│   └── Logging   zerolog (JSON结构化)
│
├── 安全
│   ├── JWT        golang-jwt v5
│   ├── 熔断       sony/gobreaker
│   ├── 限流       golang.org/x/time/rate
│   └── 密钥       Vault (生产)
│
├── 测试
│   ├── 单元测试   testing + testify
│   ├── Mock       mockery v2
│   ├── 集成测试   testcontainers-go
│   └── Eval       自研 EvalHarness
│
├── 质量
│   ├── Lint       golangci-lint
│   └── 漏洞       govulncheck
│
└── 基础设施
    ├── CI         GitHub Actions
    ├── 容器       Docker (distroless)
    ├── 编排       Kubernetes + Helm
    └── 本地       Docker Compose + air
```

---

**三条核心选型原则：**

1. **标准库优先** — 能用标准库解决的绝不引入三方库（HTTP、Context、Crypto、JSON），保持依赖树干净
2. **分阶段升级** — EventBus 从 channel 升到 Redis 再到 Kafka；存储从 InMemory 升到 Redis+PG，每次升级只换实现不换接口
3. **不绑定厂商** — 可观测性通过 OTLP 标准协议导出（可接 Jaeger / Datadog / New Relic 任意一家）；LLM 通过 `LLMProvider` 接口换底座零成本