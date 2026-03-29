# 可观察性：链路追踪与探针 (Observability)

把一个有 50 个节点、带重试、带 fallback、还能动态抛出子图请求的 Agent 架构发到线上？如果没有完善的监控，一旦用户反馈模型回复慢了，或者请求报错 500 了，你从纯文本的控制台日志里排查能让你抓狂到离职。

FluxGraph 为此内嵌了业界最强的生产级方案：**完全原生的 OpenTelemetry (OTel)** 链路追踪，外加针对 Token 消耗和节点耗时的 Prometheus 监控矩阵。

## 1. 原生全栈 OpenTelemetry (OTel)

FluxGraph 对于每一个 `Node` 的触发、甚至大模型底层请求包发出和回包的瞬间，都已经埋下了 OTel 的标准追踪块 (Spans)。

你唯一需要做的，是在拉起 `Engine` 的 main 函数里加上这几行极其标准化的初始化代码：

```go
import "github.com/BaBiQ888/fluxgraph/observability"

// 这条调用会读取环境变量 OTLP_ENDPOINT 并启动导出器
// 例如直接导出给你的 Jaeger 或 Zipkin 收集后端
shutdown := observability.InitTracer("MyFluxGraphService")
defer shutdown()

// 下面一切照常启动图引擎
// g := ...
// engine := ...
```

当一条请求流经你的框架。你可以在你们企业的 Jaeger 面板上直观的看到一个绝美的瀑布流：
* `Engine Run Start` 
  * `Node: FetchWeather (Duration: 215ms)`
    * `HTTP Request -> weather.api.com (Duration: 200ms)`
  * `Node: LLM_Thinking (Duration: 8000ms)`
    * `Prompt Encoding (Duration: 10ms)`
    * `OpenAI GPT-4o Generation (Duration: 7900ms) - Tags: [Tokens: 520, Prompt: 40]`

哪里出错了，或者哪里耗时了，一目了然！

## 2. Metrics 与 Prometheus：算清每一分账单

你当然需要知道你们组通过 FluxGraph 烧掉了多少 API 费用，或者平均响应并发。
FluxGraph `observability` 模块同样帮你做好了打点：

- `fluxgraph_node_execution_duration_seconds`：各个图节点执行分布直方图。
- `fluxgraph_llm_token_usage_total`：分提供商、分模型的 Token 计费。
- `fluxgraph_engine_failures_total`：重试与引擎奔溃率计数器。

你只需要用 Prometheus 去抓取暴露的指标 `/metrics` 端点即可。代码本身无需任何硬编码的改动。

## 3. FluxGraph Inspector：终端可视化探针

如果你没有 OTel 面板呢？如果是开发期间在本地环境？
FluxGraph 内置了一个专门为终端 CLI 与本地环境打造的：`Inspector` (可视化探针面板)。

```go
import "github.com/BaBiQ888/fluxgraph/observability"

// 将图引擎的指针注入给探针
observability.StartInspector(fluxEngine, 8080)
```

这会在你的机器上直接启动一个微型的网页服务。它会通过 Websocket 画出你构建图的拓扑结构图，并且实时亮起当前引擎跑到哪个节点的光圈。你甚至可以点进去查看节点内流转出去的 `AgentState` 当时的实时长相（参数值）！

## 4. 彻底的黑匣子：全局审计日志 (Audit)

此外，对于金融、医疗等极度严肃且注重合规的场景。大模型看过了什么数据也必须要封存备份可查证。

利用 `security/audit.go`：
```go
engine.EnableAuditLog("/var/log/fluxgraph_audit.json")
```

任何改变 `State` 的操作、所有的请求进出包、乃至 `guard` 拦截下来的恶意语句都会被以结构化 JSON 的形式灌入该审计日志内，方便 Logstash 进行合规审查调取。

---
最后一步，配置也写好了，监控也到位了。让我们来看看企业怎么把这个项目封包上线：[11. 微服务发布指南](11_deployment.md)。
