Phase 4 — 完整可观测与安全（第 13-16 周）

| 模块 | 交付物 |
|------|--------|
| OpenTelemetry 集成 | Trace + Span 全链路，兼容 Jaeger/Tempo |
| Prometheus 指标 | Token、延迟、工具成功率、A2A 任务状态 |
| StateInspector | 时间线查询 + Checkpoint Diff + Replay |
| 安全边界 | ToolAuthZ 权限矩阵 + InputSanitizer + OutputGuard |
| A2A 认证 | Bearer Token + OAuth2 Scope 控制 |
| EvalHarness 完整版 | 场景回归测试 + Token 消耗基准 |

**验收标准：** 全量 EvalScenario 通过，端到端 Trace 可视化，安全渗透测试通过。

# FluxGraph — Phase 4 任务拆分

> **Phase 4 目标：** 将 FluxGraph 从"能用"升级到"敢用于生产"。建立完整的可观测性体系（链路追踪 / 指标 / 状态回溯）、多层安全防护（工具鉴权 / 输入清洗 / 输出审核）和完整的评估测试基础设施。验收标准是全量 EvalScenario 绿灯、端到端 Trace 在 Jaeger 可视化、安全渗透测试通过。

---

## 模块 27：OpenTelemetry 全链路追踪

**OTel SDK 集成基础：**
1. SDK 初始化
   - 在框架启动时初始化 TracerProvider
   - 支持通过配置注入 ExporterEndpoint（Jaeger / Tempo / OTLP 均可）
   - 支持采样率配置（全量采样 / 概率采样 / 按 TenantID 差异化采样）
   - Global Propagator 设置（W3C TraceContext + Baggage）

2. 框架级 Tracer 封装
   - 提供 `FluxGraphTracer` 封装，隔离框架内部对 OTel SDK 的直接依赖
   - 所有 Span 创建统一通过此封装，便于未来切换观测后端

**`OtelTracingHook` 实现（对接 Phase 2 Hook 机制）：**
1. Node 级 Span 管理
   - HookBeforeNode：创建子 Span（名称格式：`node.{nodeID}`），从 Context.SpanID 派生
   - 将 TenantID、SessionID、NodeID、StepCount 作为 Span Attributes 写入
   - HookAfterNode：设置 Span Status（Ok / Error），记录耗时，结束 Span

2. Tool 级 Span 管理
   - HookBeforeTool：创建 Tool 调用子 Span（名称格式：`tool.{toolName}`）
   - 记录工具输入参数摘要（注意脱敏，不记录敏感字段）
   - HookAfterTool：记录执行结果摘要、成功/失败状态，结束 Span

3. LLM 调用 Span 管理
   - 在 LLMProvider 实现层（OpenAIProvider / AnthropicProvider）内嵌 Span 创建
   - 记录 Attributes：model 名称、input_tokens、output_tokens、finish_reason
   - 流式调用时：Span 在首个 TokenDelta 到达时开始，DeltaDone 到达时结束

4. A2A 跨服务链路传播
   - A2AClient 发请求时：将当前 Span 的 TraceContext 注入 HTTP Header（W3C traceparent）
   - A2AServer 收请求时：从 Header 提取 TraceContext，创建子 Span 继承父链路
   - 实现跨多个 FluxGraph 实例、跨不同 Agent 框架的完整链路追踪

**Span 属性标准化：**
1. 强制属性集（所有 Span 必须包含）
   - `fluxgraph.tenant_id`、`fluxgraph.session_id`、`fluxgraph.trace_id`
   - `fluxgraph.node_id`、`fluxgraph.step_count`

2. 敏感数据处理策略
   - Messages 内容不写入 Span（防止 PII 泄露到观测系统）
   - 只记录内容长度（字符数）、消息数量摘要信息

3. 错误 Span 标准化
   - AgentError 发生时：在 HookOnError 中记录 error.type / error.message
   - 熔断触发时：在 CircuitBreaker Span 中记录熔断状态变更事件

---

## 模块 28：Prometheus 指标体系

**`MetricsCollector` 接口实现：**
1. Prometheus 注册表初始化
   - 创建独立的 Registry（不污染全局默认注册表），便于测试隔离
   - 暴露 `/metrics` HTTP 端点（Prometheus Scrape 入口）

**指标定义（四类核心指标）：**

1. Token 消耗指标
   - `fluxgraph_llm_input_tokens_total`：Counter，按 tenant_id / model 分组
   - `fluxgraph_llm_output_tokens_total`：Counter，按 tenant_id / model 分组
   - `fluxgraph_llm_cost_estimate_usd_total`：Counter，按 model 价格估算累计费用

2. 延迟指标
   - `fluxgraph_node_duration_seconds`：Histogram，按 node_id / tenant_id 分组，Bucket 覆盖 10ms 到 30s
   - `fluxgraph_llm_latency_seconds`：Histogram，按 model / provider 分组
   - `fluxgraph_tool_duration_seconds`：Histogram，按 tool_name 分组
   - `fluxgraph_task_total_duration_seconds`：Histogram，完整 Task 从创建到终态的耗时

3. 工具执行指标
   - `fluxgraph_tool_calls_total`：Counter，按 tool_name / status（success/error/timeout）分组
   - `fluxgraph_tool_concurrent_executions`：Gauge，当前并发执行中的工具数量
   - `fluxgraph_tool_auth_denied_total`：Counter，权限拒绝次数，按 tenant_id / tool_name 分组

4. A2A 任务指标
   - `fluxgraph_a2a_tasks_total`：Counter，按 status（completed/failed/canceled）分组
   - `fluxgraph_a2a_remote_calls_total`：Counter，作为 Client 发起的委托次数，按 remote_agent 分组
   - `fluxgraph_a2a_webhook_delivery_total`：Counter，WebHook 推送次数，按 status 分组

5. 系统健康指标
   - `fluxgraph_circuit_breaker_state`：Gauge，0=closed / 1=half-open / 2=open，按 target 分组
   - `fluxgraph_active_sessions`：Gauge，当前活跃 Session 数，按 tenant_id 分组
   - `fluxgraph_engine_max_steps_exceeded_total`：Counter，触发安全阀次数

**Hook 集成：**
1. `PrometheusMetricHook` 实现
   - 对接 Phase 2 的 Hook 机制，在各 HookPoint 写入对应指标
   - 与 OtelTracingHook 同级注册，互不干扰

**Grafana Dashboard 配置：**
1. 提供预置 Dashboard JSON 文件（导入即用）
   - 包含 Token 消耗趋势、P99 延迟、工具成功率、熔断状态四个核心 Panel
   - 支持按 TenantID / NodeID 下钻过滤

---

## 模块 29：`StateInspector` — 状态时间旅行

**时间线查询：**
1. `GetTimeline` 实现
   - 从 MemoryStore.ListCheckpoints 获取所有检查点元数据
   - 按时间排序，构建 `[]StateSnapshot`（每条包含 CheckpointID、时间、NodeID、Status、StepCount）
   - 支持时间范围过滤和分页

2. 时间线可视化数据格式
   - 提供将时间线转换为 Mermaid 甘特图格式的工具方法（供调试工具消费）

**Checkpoint Diff：**
1. `DiffCheckpoints` 实现
   - 加载 cpA 和 cpB 两个检查点的 AgentState
   - 对 Messages 列表做 diff（新增 / 删除的消息）
   - 对 Variables Map 做 diff（变更的 key-value）
   - 对 Artifacts 列表做 diff（新增的产出物）
   - 输出结构化 `StateDiff` 对象

2. Diff 输出格式
   - 支持文本格式输出（命令行调试）
   - 支持 JSON 格式输出（供外部工具消费）

**状态回放（Replay）：**
1. `ReplayFrom` 实现
   - 从指定 CheckpointID 加载历史 AgentState
   - 创建一个独立的 ReplayEngine（隔离的 MockLLMProvider + MockToolRegistry）
   - 注入历史状态，驱动 Engine 从该检查点继续执行
   - 返回重放后的最终 AgentState 供对比分析

2. ReplayEngine 隔离设计
   - 重放时默认使用 Mock 实现，不触发真实 LLM 调用和工具副作用
   - 可配置为使用真实实现（"热重放"，用于验证修复后的行为）

3. Replay 模式选项
   - Step-by-Step 模式：每执行一个 Node 暂停，等待外部确认后继续（交互式调试）
   - Full-Auto 模式：一次性跑完，输出最终状态和完整 Trace

**Inspector HTTP API（调试端点）：**
1. `GET /debug/sessions/{sessionId}/timeline` → 返回状态时间线
2. `GET /debug/checkpoints/{cpA}/diff/{cpB}` → 返回 Checkpoint Diff
3. `POST /debug/checkpoints/{checkpointId}/replay` → 触发重放，返回重放结果
4. 调试端点安全隔离
   - 在生产环境通过配置默认禁用（`debug.enabled: false`）
   - 启用时需要额外的 admin scope Token

---

## 模块 30：`ToolAuthZ` — 工具权限沙箱

**权限矩阵设计：**
1. 数据结构
   - 二维权限矩阵：TenantID → 允许的 ToolName 集合
   - 支持通配符规则（如 `tenant-A` 允许所有 `read:*` 前缀的工具）
   - 支持 Deny List（黑名单优先于白名单）

2. 权限规则存储
   - 规则持久化到 PostgreSQL 的 `tool_permissions` 表
   - 启动时全量加载到内存（带读写锁），支持运行时热更新

3. 权限管理 API
   - `POST /admin/permissions` → 新增权限规则
   - `DELETE /admin/permissions/{ruleId}` → 撤销规则
   - `GET /admin/permissions/{tenantId}` → 查询某租户的完整权限集

**鉴权执行层：**
1. 前置鉴权（并发执行分发前）
   - ToolRegistry.ExecuteConcurrent 在分发 goroutine 前统一校验所有 ToolCall
   - 无权限的调用直接生成 `PermissionDenied` ToolResult，不进入执行队列

2. 鉴权缓存
   - 每次鉴权结果缓存（TenantID + ToolName → bool），TTL 30 秒
   - 规则热更新时主动清除相关缓存条目

3. 鉴权审计
   - 每次鉴权（无论通过或拒绝）写入审计日志（AuditLogHook 对接）
   - 拒绝事件同步更新 `fluxgraph_tool_auth_denied_total` 指标

---

## 模块 31：`InputSanitizer` — 输入清洗

**注入点设计：**
1. 外部数据入场点识别
   - A2AServer 收到用户 Message 时（注入前清洗）
   - Tool 执行结果写回 AgentState 时（工具输出也是外部数据）
   - MemoryStore.Load 后 AgentState 写入 Engine 前（防止持久化的数据被污染）

**Prompt Injection 检测：**
1. 基于规则的静态检测
   - 维护 Injection 特征词库（如"忽略上面的指令"、"你现在是..."等高风险模式）
   - 支持多语言特征词（中文 / 英文 / 混合）
   - 匹配到特征词时：标记该 Part 为可疑，并写入告警日志

2. 结构化数据处理
   - JSON / XML 格式的外部数据在注入 SystemMessage 前进行转义
   - 防止外部数据中的特殊字符破坏 Prompt 结构

3. 长度限制控制
   - 单条 Message Part 的最大字符数限制（可按 TenantID 配置）
   - 超出限制时截断并追加告警标记

**清洗策略配置：**
1. 支持三种模式：Log-Only（只记录不阻断）、Sanitize（去除可疑内容）、Block（直接拒绝请求）
2. 按 TenantID 差异化配置（企业客户 vs 个人用户不同安全等级）

---

## 模块 32：`OutputGuard` — 输出内容审核

**审核时机：**
1. LLMNode 生成最终回复后，写入 AgentState.Messages 之前
2. Task 状态变为 completed 之前对 Artifacts 内容审核

**本地规则引擎（内置）：**
1. 关键词过滤
   - 维护敏感词词库（支持按 TenantID 加载自定义词库）
   - 命中时执行配置的动作（脱敏替换 / 拦截 / 人工审核队列）

2. 结构化数据泄露检测
   - 正则检测 LLM 输出中是否包含身份证号 / 信用卡号 / 手机号等敏感格式
   - 命中时自动脱敏（替换为 `***`）

3. PII 检测
   - 识别并掩码常见 PII 格式

**外部审核服务对接（`OutputGuard` 接口）：**
1. 接口定义允许注入外部实现（内部风控系统 / 第三方内容安全 API）
2. 外部调用超时时的降级策略：配置是否允许超时降级放行（fail-open vs fail-close）

**流式输出的审核处理：**
1. 流式场景无法等待完整内容再审核
2. 策略：流式内容实时透传给用户，同时异步审核；审核不通过时发送撤回指令（后置拦截）
3. 高风险 TenantID 可配置为在完整输出生成后再推送（牺牲实时性换取安全性）

---

## 模块 33：`EvalHarness` — 完整评估框架

**EvalScenario 数据格式：**
1. 场景定义结构
   - name、description、tags（用于分类筛选）
   - initialState（起始 AgentState）、goal（任务目标描述）
   - mockLLMResponses（预设回复序列）、mockToolBehaviors（工具预设行为）

2. 断言结构
   - `ExpectedStep`：描述每个 Node 执行后的预期状态变化
   - `FinalAssertion`：接受最终 AgentState 的校验函数
   - `ArtifactAssertion`：对 Artifacts 内容的校验规则

3. 场景文件格式
   - 支持 YAML 格式定义场景（便于非开发者编写测试用例）
   - 支持 Go / Python 代码方式定义（复杂断言逻辑）

**`EvalHarness` 核心实现：**
1. 场景加载
   - 支持从目录批量加载所有 `.eval.yaml` 场景文件
   - 按 tags 过滤运行子集（如只运行 `smoke` 标签的场景）

2. 隔离执行环境
   - 每个场景创建独立的 Engine 实例（互不共享状态）
   - MockLLMProvider / MockToolRegistry / InMemoryStore / InMemoryEventBus 全部独立注入

3. 执行与断言
   - 驱动 Engine.Run 执行完整场景
   - 按 ExpectedStep 顺序校验中间状态
   - 最终执行 FinalAssertion，汇总通过 / 失败详情

4. EvalResult 结构
   - passed（bool）、failedStep（哪一步失败）、finalState（快照）
   - tokenCost（当前场景消耗的模拟 Token 数）、duration（执行耗时）
   - stateTimeline（完整状态时间线，用于失败分析）

**Token 消耗基准测试：**
1. Golden File 机制
   - 首次运行时将 tokenCost 记录为 Golden 值写入文件
   - 后续运行对比当前值与 Golden 值，超出 10% 阈值则标记为 Warning

2. 回归基准报告
   - 生成 Token 消耗对比报告（场景名 / 历史均值 / 当前值 / 变化百分比）

**场景覆盖度报告：**
1. 统计已有 EvalScenario 对各 Node 类型的覆盖情况
2. 识别未被任何场景覆盖的 Node，在报告中标记为待补充

---

## 模块 34：安全审计日志体系

**审计日志设计：**
1. 审计事件类型定义
   - AuthSuccess / AuthFailure（认证结果）
   - ToolAuthGranted / ToolAuthDenied（工具权限决策）
   - TaskCreated / TaskCompleted / TaskCanceled（Task 生命周期）
   - PermissionRuleChanged（权限规则变更）
   - OutputBlocked / OutputSanitized（输出审核结果）
   - DebugEndpointAccessed（调试端点访问）

2. 审计日志字段标准化
   - eventTime、eventType、tenantID、sessionID、actorID（发起方）
   - targetObject（操作对象）、result（success/failure）、detail（附加信息）

3. 日志不可篡改性
   - 每条审计日志追加写入（Append-Only），不支持修改删除
   - 可选：写入独立的 Audit DB（与业务库隔离）

**AuditLogHook 完善（对接 Phase 2）：**
1. 扩展 HookPoint 覆盖：补充 OnOutputGuard、OnAuthDecision 两个新 Point
2. 日志异步写入（不阻塞主流程），使用内部队列 + 后台 goroutine 批量落盘

---

## 模块 35：渗透测试与安全验收

**A2A 协议层测试：**
1. 未认证请求测试
   - 不携带 Token 直接访问各端点，验证均返回 401
2. 越权测试
   - TenantA 的 Token 尝试访问 TenantB 的 Task，验证返回 TaskNotFoundError（不泄露存在性）
3. Scope 越权测试
   - 只有 `agent:read` Scope 的 Token 尝试 SendMessage，验证返回 403

**Prompt Injection 测试：**
1. 构造包含注入特征词的 Message 发送（如携带"忽略系统指令"的用户消息）
2. 验证 InputSanitizer 正确拦截或脱敏，不影响 SystemMessage 结构

**工具权限测试：**
1. TenantA 尝试调用只授权给 TenantB 的工具
2. 验证 PermissionDenied ToolResult 正确返回，且 Tool 代码未被实际执行

**输出审核测试：**
1. 注入返回敏感内容的 MockLLMProvider
2. 验证 OutputGuard 脱敏后的内容才到达客户端

**WebHook 签名验证测试：**
1. 伪造 WebHook 请求（不携带正确签名）发往接收端
2. 验证签名校验拦截，不触发业务逻辑

---

## 模块 36：Phase 4 集成与性能验收测试

**全量 EvalScenario 回归测试：**
1. 运行所有 Phase 1 - 4 过程中累积的 EvalScenario
2. 验证全部绿灯，Token 消耗与 Golden File 差异在 10% 以内

**可观测性端到端验证：**
1. 运行包含 LLMNode + ToolExecutorNode + A2ADelegateNode 的复合场景
2. 在 Jaeger UI 中验证：从用户请求到远端 Agent 返回，全链路 Span 完整串联
3. 在 Grafana 中验证：Token 消耗 / 工具延迟 / A2A 任务状态 指标实时更新

**StateInspector 验收：**
1. 执行一个包含 5+ 个 Node 的完整 Task
2. 通过 Inspector API 获取时间线，验证每个 Node 对应一个 Checkpoint
3. 选取中间检查点做重放，验证重放结果与历史执行一致

**并发与隔离压测：**
1. 50 个并发 Session（混合不同 TenantID）同时运行
2. 验证 Prometheus 中各 TenantID 的 Token 计数相互独立不串扰
3. 验证审计日志中 TenantID 字段全部正确归属

---

## Phase 4 模块依赖顺序

```
模块27（OTel 追踪）
    └── 依赖 Phase 2 Hook 机制 + Phase 3 A2AClient/Server（跨服务传播）

模块28（Prometheus 指标）
    └── 依赖 Phase 2 Hook 机制 + Phase 3 Task 生命周期事件

模块29（StateInspector）
    └── 依赖 Phase 2 Redis/Postgres MemoryStore（检查点存储）

模块30（ToolAuthZ）
    └── 依赖 Phase 1 ToolRegistry + Phase 2 并发执行层

模块31（InputSanitizer）
    └── 依赖 Phase 3 A2AServer（注入点：请求解析后）

模块32（OutputGuard）
    └── 依赖 Phase 2 LLMProvider（注入点：Generate 返回后）
    └── 依赖 Phase 3 StreamRouter（流式场景异步审核）

模块33（EvalHarness）
    └── 依赖 Phase 1 MockLLM/MockTool + 以上全部模块

模块34（审计日志体系）
    └── 依赖 Phase 2 AuditLogHook + 模块30-32（新增审计点）

模块35（渗透测试）
    └── 依赖 模块30-32 全部完成后执行

模块36（集成验收）
    └── 依赖 以上全部模块
```

---

## 四个 Phase 整体里程碑回顾

| Phase | 核心交付 | 完工标志 |
|-------|---------|---------|
| **Phase 1** | 骨架、核心原语、接口定义、基础 Engine | MockLLM 跑通 ReAct 循环 |
| **Phase 2** | 真实 LLM、持久化、并发工具、Hook、错误恢复 | 100 并发 Session 稳定运行 |
| **Phase 3** | A2A 完整协议层、跨框架互通 | 与 ADK / LangGraph 双向委托成功 |
| **Phase 4** | 全链路可观测、安全边界、评估体系 | 渗透测试通过、全量 Eval 绿灯 |

---

> **Phase 4 完工验收标准：**  
> 端到端 Trace 在 Jaeger 完整可视化（跨 A2A Agent 链路）；Grafana Dashboard 实时展示四类核心指标；StateInspector 支持任意历史检查点回放；ToolAuthZ 越权测试 100% 拦截；InputSanitizer 注入检测覆盖主流攻击向量；全量 EvalScenario 绿灯，Token 消耗基准偏差 < 10%；并发渗透测试下租户数据零串扰。
---