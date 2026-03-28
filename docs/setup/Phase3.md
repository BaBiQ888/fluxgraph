### Phase 3 — A2A 协议层（第 9-12 周）

| 模块 | 交付物 |
|------|--------|
| AgentCard 自动生成 | 启动时扫描注册的 Skill/Tool 自动构建 |
| A2AServer | JSON-RPC + HTTP/REST 双协议绑定 |
| 流式 SSE 推送 | `StreamRouter` + SSE 端点 |
| Push Notification | WebHook 注册 + 事件推送 |
| A2AClient | Discover + SendTask + StreamTask |
| A2ADelegateNode | 跨 Agent 任务委托 |
| Multi-Turn Manager | contextId 管理 + 跨 Task 上下文继承 |

**验收标准：** FluxGraph 实例能与 Google ADK / LangGraph 等主流框架互相发现并委托任务。

# FluxGraph — Phase 3 任务拆分

> **Phase 3 目标：** 为 FluxGraph 装配完整的 A2A 协议层，使其能作为 **A2A Server** 对外暴露标准化能力，同时作为 **A2A Client** 调用其他 Agent，并支持多轮会话、流式推送和 WebHook 通知。验收标准是与 Google ADK / LangGraph 等主流框架完成互通。

---

## 模块 16：`AgentCard` — 能力声明书

**元数据定义：**
1. AgentCard 核心字段设计
   - name、description、version（语义化版本）、url（A2A 服务端点）
   - 协议版本字段（`protocolVersion: "1.0"`）

2. `AgentCapabilities` 字段设计
   - streaming（是否支持 SSE 流式）
   - pushNotifications（是否支持 WebHook）
   - extendedAgentCard（是否提供认证后的详细 Card）
   - 两个 FluxGraph 扩展字段：stateTimeTravel（检查点回溯）、multiTenancy

3. `AgentSkill` 字段设计
   - id、name、description（自然语言描述，供发现方 LLM 理解）
   - inputModes / outputModes 枚举（text / file / structured_data）
   - tags（用于分类检索）

4. `SecuritySchemes` 字段设计
   - 支持声明 BearerToken / OAuth2 / APIKey 三种方案
   - 每种方案包含验证端点信息

**自动生成机制：**
1. 工具扫描
   - 启动时遍历 ToolRegistry 中所有已注册工具
   - 将每个 Tool 的 Name / Description / InputSchema 映射为一个 AgentSkill

2. 能力检测
   - 检测 Engine 配置中是否启用了流式 EventBus → 自动设置 streaming: true
   - 检测是否配置了 WebHook 服务 → 自动设置 pushNotifications: true

3. 对外暴露端点
   - 在 `/.well-known/agent.json` 路径自动注册 HTTP GET 处理器
   - 响应内容为序列化后的 AgentCard JSON

4. Extended AgentCard 管理
   - 公开 Card 只暴露部分 Skill（无需认证即可发现）
   - 认证后的 Extended Card 暴露完整 Skill 列表和更详细的参数 Schema
   - 两份 Card 在启动时分别构建并缓存

**版本管理：**
1. Card 缓存失效策略
   - 新工具注册或配置变更时主动刷新 Card 缓存
   - 对外暴露 `agentCard.version` 字段，客户端可据此判断是否需要重新拉取

---

## 模块 17：Task 生命周期管理

**Task 数据结构：**
1. Task 核心字段
   - id（Server 生成，全局唯一）、contextId（多轮会话归组标识）
   - status（TaskStatus 对象）、history（[]Message）
   - artifacts（[]Artifact）、metadata

2. TaskStatus 字段
   - state 枚举（submitted / working / input_required / auth_required / completed / failed / canceled / rejected）
   - timestamp（状态变更时间）、message（状态附加说明）

3. TaskState 枚举完整定义
   - 对齐 A2A Spec § 4.1.3 完整枚举
   - 建立 TaskState → AgentState.Status 的双向映射关系

**Task 存储层：**
1. TaskStore 接口定义
   - Create(task)、GetByID(taskID)、UpdateStatus(taskID, status)
   - AppendArtifact(taskID, artifact)、AppendMessage(taskID, message)
   - ListByContextID(contextID)、ListByTenantID(tenantID, filter)

2. RedisTaskStore 实现
   - 使用 Hash 存储 Task 元数据，List 存储 history 和 artifacts
   - 键名格式：`fluxgraph:{tenantID}:task:{taskID}`
   - 任务状态变更时同步更新 Sorted Set 索引（按更新时间排序，支持 ListTasks 分页）

3. Task 与 AgentState 同步机制
   - Engine 每次 Node 执行完成后触发 TaskStore.UpdateStatus
   - Artifact 写入 AgentState 时同步触发 TaskStore.AppendArtifact

**Task CRUD 业务逻辑层：**
1. 创建 Task
   - 生成唯一 TaskID（UUID v4）
   - 若请求携带 contextId 则关联，否则生成新 contextId
   - 初始状态设为 submitted，异步触发 Engine.Run

2. 取消 Task
   - 校验当前 TaskState 是否可取消（非终态才可取消）
   - 向 Engine 发送取消信号（通过 Context.CancelFunc）
   - 更新 TaskState 为 canceled

3. 列举 Task
   - 按 tenantID 过滤，按更新时间倒序，支持 cursor 分页（cursor = 时间戳 + taskID）
   - 支持按 status / contextId / 时间范围过滤

---

## 模块 18：`A2AServer` — 对外协议服务器

**HTTP 服务器基础：**
1. 路由注册
   - `GET /.well-known/agent.json` → AgentCard
   - `POST /` → JSON-RPC 主入口（分发所有操作）
   - `GET /tasks/{taskId}` → HTTP/REST GetTask
   - `POST /tasks/{taskId}/cancel` → HTTP/REST CancelTask
   - `GET /tasks` → HTTP/REST ListTasks
   - `GET /agent/authenticatedExtendedCard` → Extended AgentCard

2. 中间件链
   - 请求解析中间件（JSON-RPC 格式校验）
   - 认证中间件（在全局路由层前置执行）
   - TenantID 注入中间件（从认证结果提取 TenantID 写入 Context）
   - 限流中间件（基于 TenantID 的 RateLimiter）
   - 请求日志中间件（记录 TraceID、耗时、HTTP 状态码）

**JSON-RPC 方法分发：**
1. 方法路由表建立
   - `message/send` → SendMessageHandler
   - `message/stream` → StreamMessageHandler
   - `tasks/get` → GetTaskHandler
   - `tasks/list` → ListTasksHandler
   - `tasks/cancel` → CancelTaskHandler
   - `tasks/subscribe` → SubscribeToTaskHandler
   - `tasks/pushNotificationConfig/create` → CreatePushConfigHandler
   - `tasks/pushNotificationConfig/get` → GetPushConfigHandler
   - `tasks/pushNotificationConfig/list` → ListPushConfigHandler
   - `tasks/pushNotificationConfig/delete` → DeletePushConfigHandler

2. JSON-RPC 协议规范
   - 严格校验 jsonrpc 版本字段（必须为 "2.0"）
   - 请求 id 传递：保证 response.id 与 request.id 一致
   - 错误响应格式：code / message / data 三字段结构

3. A2A 协议版本协商
   - 解析 `A2A-Version` Header（缺省视为 0.3）
   - 不支持的版本返回 VersionNotSupportedError

**`SendMessageHandler` 实现：**
1. 请求解析
   - 解析 SendMessageRequest（message、configuration、taskId、contextId）
   - 校验 message.parts 中的 PartType 是否在 AgentCard 声明的 inputModes 中
2. 执行模式判断
   - returnImmediately: false → 阻塞等待 Task 完成再返回
   - returnImmediately: true → 立即返回 Task（working 状态），后台异步执行
3. 将 A2A Message 转换为 AgentState，调用 Engine.Run
4. 将最终 AgentState 转换回 A2A Task 格式返回

**A2A 专属错误处理：**
1. A2A 错误码映射
   - TaskNotFoundError / TaskNotCancelableError / UnsupportedOperationError 等全部实现
   - 每种错误包含 HTTP 状态码、JSON-RPC 错误码、可读描述三要素
2. 错误信息安全原则
   - 资源不存在与无权限访问统一返回 TaskNotFoundError，防止信息泄露

---

## 模块 19：`A2AAuthenticator` — 认证与授权

**Bearer Token 认证：**
1. Token 颁发
   - 提供 `/auth/token` 端点，接受 ClientID + ClientSecret 换取 JWT
   - JWT Payload 包含 TenantID、Scopes、过期时间
2. Token 校验
   - 中间件层解析 Authorization: Bearer 头
   - 验证签名、过期时间，提取 TenantID 注入 Context

**OAuth2 Scope 控制：**
1. Scope 定义
   - 定义框架级 Scope：`agent:read`（GetTask / ListTasks）、`agent:write`（SendMessage）、`agent:admin`（管理操作）
2. Scope 校验
   - 各 Handler 在执行前校验当前 Token 的 Scope 是否满足操作要求

**Extended AgentCard 访问控制：**
1. 要求请求携带有效 Bearer Token
2. Token 合法时返回完整 AgentCard
3. 非法时返回 401，不返回部分信息

**A2A 对外请求签名（Client 侧）：**
1. 当 FluxGraph 作为 A2A Client 向其他 Agent 发请求时，附加本身的身份凭证
2. 支持在 A2AClient 配置中注入凭证（Token / API Key）

---

## 模块 20：`StreamRouter` + SSE 推送

**`StreamRouter` 核心（Layer 4 实现）：**
1. ToolCallDetector 实现
   - 维护括号深度计数器，检测流式 JSON 是否构成完整的 tool_call 对象
   - 支持多个并发 ToolCall 的边界识别（streaming 中可能有多个并行工具调用）

2. 流事件分类输出
   - TextDelta → StreamEvent{StreamEventText}，直接透传
   - 完整 ToolCall → StreamEvent{StreamEventToolCall}，解析参数
   - LLM 生成完毕 → StreamEvent{StreamEventDone}

3. 背压控制
   - StreamEvent channel 设置缓冲区大小（防止消费者慢导致 goroutine 泄漏）
   - context 取消时及时关闭 channel

**SSE 端点实现（`StreamMessageHandler`）：**
1. SSE 协议规范
   - 响应 Content-Type: text/event-stream
   - 每个事件格式：`data: {json}\n\n`
   - 定期发送心跳事件（防止代理层超时断开）

2. 事件序列保证
   - 首个事件必须是 Task 对象（含 taskId、contextId）
   - 后续为 TaskStatusUpdateEvent 和 TaskArtifactUpdateEvent 交替出现
   - 最后一个事件标记 `final: true`，客户端据此关闭连接

3. 多客户端并发订阅同一 Task
   - 相同 taskId 可以被多个连接同时订阅
   - EventBus 广播单条事件，每个连接各自消费（扇出模式）
   - 某个连接断开不影响其他连接和 Task 生命周期

4. 客户端重连处理
   - 支持 `Last-Event-ID` 请求头（续传）
   - 重连时先推送当前 Task 快照，再订阅后续增量事件

**`SubscribeToTaskHandler`（订阅已有 Task）：**
1. 校验 TaskState 不是终态（终态任务不可订阅）
2. 立即推送一次当前 Task 快照作为首个 SSE 事件
3. 订阅 EventBus 中该 TaskID 的后续事件持续推送

---

## 模块 21：Push Notification（WebHook）

**PushNotificationConfig 数据结构：**
1. 字段设计
   - id（Server 生成）、taskId、webhookUrl、authentication（可选，Header 注入）
   - eventTypes（可选过滤：只推送特定类型事件）

2. Config 存储
   - 与 Task 关联存储在 TaskStore 中
   - 支持一个 Task 绑定多个 WebHook 配置（不同系统监听同一 Task）

**WebHook 推送实现：**
1. 事件触发时机
   - Task 状态变更时（TaskStatusUpdateEvent）
   - 新 Artifact 产出时（TaskArtifactUpdateEvent）

2. 推送执行
   - 对所有关联 Config 并发发送 HTTP POST 请求
   - 请求体格式与 SSE 事件体相同（StreamResponse 对象）
   - 携带配置中的认证 Header（如 `Authorization: Bearer {token}`）

3. 推送可靠性
   - 推送失败时（网络超时 / 4xx / 5xx）触发重试（指数退避，最多 3 次）
   - 超过重试次数后记录失败日志，不阻塞主流程

4. 安全验证（Webhook Signature）
   - 推送时在 Header 中附加 HMAC-SHA256 签名
   - 接收方可验证签名，确认推送来源合法

**PushConfig CRUD Handler 实现：**
1. CreatePushConfigHandler：校验 webhookUrl 合法性 → 存储配置 → 返回含 id 的 Config
2. GetPushConfigHandler：按 configId 查询，校验 TenantID 归属
3. ListPushConfigHandler：按 taskId 返回所有 Config 列表（分页）
4. DeletePushConfigHandler：删除配置，幂等处理（已删除返回成功而非 404）

---

## 模块 22：`A2AClient` — 调用外部 Agent

**Discover 能力发现：**
1. 拉取 AgentCard
   - GET `{agentURL}/.well-known/agent.json`
   - 解析并缓存 AgentCard（TTL 可配置）

2. Extended Card 获取
   - 若 AgentCard.capabilities.extendedAgentCard 为 true，且本地持有认证凭证
   - 自动请求 `/agent/authenticatedExtendedCard` 并用结果替换缓存

3. Card 缓存管理
   - 按 agentURL 缓存，版本字段变更时主动失效
   - 支持手动 Refresh（强制重新拉取）

**Negotiate 能力协商：**
1. 校验必要能力
   - 调用方指定所需能力（如 streaming: true）
   - 与远端 AgentCard.capabilities 对比，不满足时返回错误（而非静默降级）

2. 协议版本协商
   - 发送请求时附加 `A2A-Version: 1.0` Header
   - 远端返回 VersionNotSupportedError 时，尝试降级版本重试

**`SendTask` 同步调用：**
1. 将内部 Message 结构序列化为 A2A SendMessageRequest
2. 发送 JSON-RPC `message/send` 请求
3. 根据 returnImmediately 配置决定：
   - false → 阻塞等待直到 Task 终态，轮询使用 GetTask
   - true → 立即获取 Task，外部调用者负责后续轮询
4. 将远端 Task.artifacts 转换为内部 Artifact 结构返回

**`StreamTask` 流式调用：**
1. 发送 `message/stream` 请求，建立 SSE 连接
2. 解析 SSE 事件流（text/event-stream 格式）
3. 将 TaskStatusUpdateEvent / TaskArtifactUpdateEvent 转换为内部 StreamEvent
4. 通过 channel 返回给调用方实时消费
5. 连接断开时自动重连（携带 Last-Event-ID，从断点续传）

**`ContinueTask` 多轮交互：**
1. 在已有 Task 上继续发消息（携带 taskId + contextId）
2. 适用于 input_required 状态的 Task（提供所需输入后继续）
3. 校验：提供 taskId 时必须与 contextId 匹配，不匹配返回错误

**`PollTask` 状态轮询：**
1. 定时调用 `tasks/get` 检查 Task 状态
2. 轮询间隔采用指数退避（避免频繁轮询）
3. Task 达到终态时停止轮询并返回结果

**`RegisterWebhook`：**
1. 调用远端 `tasks/pushNotificationConfig/create`
2. 本地记录 configId，用于后续删除

---

## 模块 23：`A2ADelegateNode` — 跨 Agent 任务委托

**节点核心逻辑：**
1. 配置项
   - remoteAgentURL（委托目标地址）
   - skillID（可选，指定目标 Agent 的特定 Skill）
   - preferStreaming（是否优先使用流式调用）
   - timeout（整体委托超时）

2. 执行前准备
   - 调用 A2AClient.Discover 获取远端 AgentCard（优先读缓存）
   - 调用 A2AClient.Negotiate 验证所需能力
   - 将 AgentState.Messages 转换为委托消息（可配置：全量传递 or 只传最后 N 条）

3. 委托执行分支
   - 远端支持 streaming 且 preferStreaming = true → StreamTask
     - 实时将 TaskArtifactUpdateEvent 追加到本地 AgentState.Artifacts
   - 否则 → SendTask（阻塞等待终态）

4. 结果整合
   - 远端 Task.artifacts → 合并写入本地 AgentState.Artifacts
   - 远端 Task 的最终 Message → 追加至 AgentState.Messages（Role = assistant）
   - 远端 Task.status.state 映射为 NodeResult 的状态信号

5. 委托失败处理
   - 远端返回 failed / rejected → 构造 ErrFatal，交给 TypedErrorHandler
   - 网络超时 → 构造 ErrRetriable，触发重试
   - 超时重试次数耗尽 → 向上层返回委托失败信息

---

## 模块 24：Multi-Turn 多轮会话管理

**`A2AConversationManager` 实现：**
1. ContextID 注册表
   - 维护 contextId → []TaskID 的关联（有序，按创建时间）
   - 提供 RegisterTask / GetTasksByContext 方法

2. 上下文继承策略接口
   - 定义 ContextInheritancePolicy 接口
   - 内置 `LastNTasksPolicy`：只继承最近 N 个 Task 的 artifacts 和 summary

**ContextID 生命周期：**
1. 新请求无 contextId → 生成新 contextId，绑定到 Task，响应中返回
2. 新请求携带 contextId → 关联到已有 context，继承历史上下文
3. contextId 与 taskId 不匹配时 → 返回参数校验错误（A2A Spec 强制要求）

**Input-Required 多轮交互：**
1. Engine 遇到 InterruptSignal{InputRequired} → Task 状态变为 input_required
2. Client 发送携带 taskId 的新 Message → ConversationManager 识别为同一 Task 的续传
3. 将新 Message 注入 AgentState，调用 Engine.Resume 恢复执行

**跨 Task 上下文摘要：**
1. 每个 Task 完成时，将 artifacts 和关键 Messages 做摘要
2. 摘要写入 ContextStore（按 contextId 归组）
3. 同一 contextId 下新 Task 启动时，从 ContextStore 读取历史摘要注入 System Message

---

## 模块 25：gRPC 协议绑定（可选实现）

**Proto 文件生成：**
1. 对齐 A2A Spec `a2a.proto`（官方规范文件）
2. 不手写 proto 定义，从官方规范直接拉取并使用代码生成工具生成 Go / Python stub

**gRPC Server 实现：**
1. 将 JSON-RPC Handler 的业务逻辑复用（不重复实现）
2. gRPC Handler 只做协议格式转换 + 调用共用业务层

**流式 RPC 实现：**
1. `StreamMessage` → Server-side streaming RPC
2. `SubscribeToTask` → Server-side streaming RPC
3. gRPC 的流式事件与 SSE 事件体共用同一构建逻辑

---

## 模块 26：Phase 3 跨框架互通测试

**与 Google ADK 互通测试：**
1. FluxGraph 作为 Server，ADK 作为 Client 发起委托
   - 验证 AgentCard 被正确解析
   - 验证 Task 生命周期事件被正确接收
2. FluxGraph 作为 Client，委托任务到 ADK Agent
   - 验证 A2AClient.Discover 正确读取 ADK 的 AgentCard
   - 验证 StreamTask 中实时收到 ADK 的 artifact 更新

**与 LangGraph 互通测试：**
1. 重复上述两个方向的测试
2. 验证 Multi-Turn（contextId 传递）在跨框架场景正常工作

**Push Notification 端到端测试：**
1. 启动本地 Webhook Receiver（简单 HTTP Server）
2. 创建 Task 并注册 WebHook
3. 验证 Task 状态变更时 Webhook 被正确触发，签名验证通过

**Multi-Turn 场景测试：**
1. 创建 Task A（contextId = ctx-1）完成
2. 创建 Task B（携带 contextId = ctx-1）
3. 验证 Task B 的 System Message 中包含 Task A 的历史摘要

**能力协商负向测试：**
1. 向不支持 streaming 的 Agent 发起流式请求 → 验证返回 UnsupportedOperationError
2. Token 过期时请求 Extended AgentCard → 验证返回 401
3. contextId 与 taskId 不匹配的请求 → 验证被正确拒绝

---

## Phase 3 模块依赖顺序

```
模块16（AgentCard 自动生成）
    └── 依赖 Phase 1 ToolRegistry + Phase 2 LLMProvider（读取模型信息）

模块17（Task 生命周期管理）
    └── 依赖 Phase 1 AgentState + Phase 2 RedisMemoryStore

模块18（A2AServer）
    └── 依赖 模块16（AgentCard）+ 模块17（Task 管理）+ Phase 2 Engine

模块19（A2AAuthenticator）
    └── 依赖 模块18（注入认证中间件）

模块20（StreamRouter + SSE）
    └── 依赖 模块17（Task 事件）+ Phase 1 EventBus

模块21（Push Notification）
    └── 依赖 模块17（Task 事件）+ 模块20（事件构建逻辑复用）

模块22（A2AClient）
    └── 独立模块，依赖模块16（AgentCard 解析）

模块23（A2ADelegateNode）
    └── 依赖 模块22（A2AClient）+ Phase 1 Node 接口

模块24（Multi-Turn 会话管理）
    └── 依赖 模块17（Task 存储）+ 模块18（请求解析）

模块25（gRPC 绑定）
    └── 依赖 模块18（复用业务层）

模块26（互通测试）
    └── 依赖 以上全部模块
```

---

> **Phase 3 完工验收标准：**  
> FluxGraph 实例能被 Google ADK 通过标准 A2A 协议发现并委托任务；能通过 A2AClient 委托任务到 LangGraph Agent 并实时接收流式 artifact 更新；Multi-Turn 场景下跨 Task 上下文继承正常；WebHook 在 Task 状态变更后 1 秒内送达，签名验证通过；所有 A2A Spec 强制要求的错误码均有对应实现。
