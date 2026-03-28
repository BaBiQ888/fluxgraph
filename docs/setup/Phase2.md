### Phase 2 — 生产能力（第 5-8 周）

| 模块 | 交付物 |
|------|--------|
| LLMProvider 实现 | `OpenAIProvider`（含流式），`FallbackChainProvider` |
| ToolRegistry 并发执行 | goroutine + WaitGroup 并发工具调用 |
| Planner 接口 | + 内置 `ReActPlanner` 实现 |
| Redis/Postgres MemoryStore | 持久化 + 检查点 |
| Hook 机制 | + `TokenCounterHook`, `LatencyMetricHook` |
| 类型化错误恢复 | 重试 + 熔断 + 状态回滚 |

**验收标准：** 能处理 100 并发 Session，Redis 检查点落盘，熔断正确触发。

# FluxGraph — Phase 2 任务拆分

> **Phase 2 目标：** 将 Phase 1 的骨架升级为可承载真实生产流量的系统。引入真实 LLM、持久化存储、并发工具调用、Planner 推理、生命周期 Hook 和完整错误恢复机制。

---

## 模块 8：`OpenAIProvider` — 真实 LLM 接入

**HTTP 客户端基础层：**
1. 客户端初始化
   - 读取 API Key、Base URL、超时配置（支持环境变量注入）
   - 构建可复用的 HTTP 连接池（控制最大连接数、Keep-Alive）
   - 支持代理配置（企业内网场景）

2. 请求构建
   - 将 AgentState.Messages 转换为 OpenAI Chat 格式
   - 将 ToolRegistry.ListTools() 转换为 `tools` 字段（JSON Schema）
   - 注入 System Message（从 AgentState.Variables 中读取预设 Prompt）

3. 响应解析
   - 解析 `finish_reason`（stop / tool_calls / length）
   - 将 choices[0].message 转换为内部 Message 结构
   - 解析 usage 字段填充 TokenUsage

**同步生成实现（`Generate`）：**
1. 发送 POST 请求，等待完整响应
2. HTTP 错误码映射到 AgentError 类型
   - 429 → ErrRetriable（触发限流重试）
   - 400 → ErrFatal（参数错误）
   - 500/503 → ErrRetriable（服务端临时故障）
3. 响应内容解析并封装为 LLMResponse 返回

**流式生成实现（`StreamGenerate`）：**
1. 发送请求时设置 `stream: true`
2. 读取 SSE 格式的响应流（逐行解析 `data: {...}`）
3. 解析增量字段（delta.content / delta.tool_calls）
4. 将每个增量转换为 TokenDelta 发送到 channel
5. 检测到 `[DONE]` 标记时发送 DeltaDone 并关闭 channel
6. 处理流中断（网络断开）：关闭 channel 并携带错误信号

**`AnthropicProvider`（Claude 系列）：**
1. Anthropic API 的消息格式与 OpenAI 差异适配
   - System Message 单独字段（非 messages 数组内）
   - Tool 格式差异处理（input_schema vs parameters）
2. 流式事件格式适配
   - Anthropic 使用不同的 SSE 事件类型（content_block_delta 等）
3. Token 统计字段映射

**`FallbackChainProvider`（主备切换）：**
1. 链式配置
   - 接受有序 []LLMProvider 列表，按顺序尝试
   - 每个 Provider 可独立设置最大重试次数
2. Fallback 触发条件
   - 当前 Provider 返回 ErrRetriable 且重试次数耗尽时，切换下一个
   - ErrFatal 时不触发 Fallback，直接向上透传
3. 状态记录
   - 记录每次 Fallback 事件（哪个 Provider 失败、切换到哪个），供 Hook 消费

---

## 模块 9：`ToolRegistry` 并发执行实现

**注册中心实现：**
1. 线程安全的工具存储
   - 使用读写锁保护工具 Map，支持运行时动态注册
   - 工具名碰撞检测：重复注册时返回错误（不允许静默覆盖）

2. 权限矩阵存储
   - 实现 TenantID → 允许工具名集合 的关联结构
   - 提供 GrantPermission / RevokePermission 方法动态调整

3. 工具定义导出
   - ListTools() 将所有已注册工具的 InputSchema 序列化为 LLM 所需格式
   - 支持按 TenantID 过滤，只返回当前租户有权限的工具列表

**并发执行核心：**
1. 任务分发
   - 接收 []ToolCall，对每个 ToolCall 创建独立 goroutine
   - 每个 goroutine 持有从父 Context 派生的子 Context（继承 Deadline）

2. 结果汇聚
   - WaitGroup 等待所有 goroutine 完成
   - 使用带索引的结果 slice 保持原始调用顺序（不使用 channel 收集，避免乱序）

3. 超时与局部失败处理
   - 任意单个工具超时，只取消该 goroutine 的子 Context，不影响其他并发工具
   - 汇聚时记录每个 ToolResult 的执行状态（成功/超时/错误）

4. 鉴权前置校验
   - 在分发 goroutine 之前统一校验所有工具的权限
   - 无权限的工具调用直接生成 PermissionDenied 的 ToolResult，不进入执行队列

**内置工具实现（开发调试用）：**
1. `EchoTool`
   - 原样返回输入内容，用于测试工具调用链路
2. `SleepTool`
   - 模拟耗时工具，用于验证并发执行与超时机制

---

## 模块 10：`ReActPlanner` — 内置规划器实现

**Plan 数据结构完善：**
1. PlanStep 状态字段
   - 增加 Status 枚举（Pending / Running / Completed / Failed / Skipped）
   - 增加 ActualNodeID（实际执行的节点，允许与预设不同）

2. Plan 执行追踪
   - 在 AgentState.Variables 中维护当前 Plan 引用
   - 每次 Node 执行完成后更新对应 PlanStep 状态

**ReAct 规划逻辑：**
1. 初始化 Plan（`Plan` 阶段）
   - 接收高层目标，构造首个 PlanStep（通常为 LLMNode 思考步骤）
   - 暂不生成完整计划，采用单步生成策略（Think → Act → Observe）

2. 动态追加步骤（`Act` 阶段）
   - 解析上一轮 LLM 输出，若包含 ToolCall，追加 ToolExecutorNode 步骤
   - 工具结果返回后，追加下一个 LLMNode 步骤继续推理

3. 计划修订（`Revise` 方法）
   - 接收当前已完成步骤的 Observe 结果
   - 判断目标是否已达成（检查 AgentState 中的完成信号）
   - 未达成则追加新的 Think 步骤，触发下一轮 ReAct 循环

4. 终止条件判断
   - LLM 输出无 ToolCall → 判定为最终回复，生成 Terminal 步骤
   - 超过最大轮次 → 强制生成 Failed 终止步骤

---

## 模块 11：`RedisMemoryStore` — 高性能持久化

**连接管理：**
1. 连接池初始化
   - 配置最大连接数、空闲超时、连接重试策略
   - 支持 Redis Sentinel / Cluster 模式配置

2. 键名设计
   - 所有键以 `fluxgraph:{tenantID}:{sessionID}` 为前缀，强制租户隔离
   - 设计键的 TTL 策略（会话过期自动清理）

**Save / Load 实现：**
1. 序列化策略
   - AgentState 序列化为 JSON 存入 Redis String
   - 大型 Messages 列表使用 Redis List 分块存储（超过阈值时分页）

2. 原子性保障
   - Save 使用 MULTI/EXEC 事务，确保 State 和 CheckpointMeta 同步写入

3. 并发读写保护
   - 使用 Redis 乐观锁（WATCH + MULTI）防止并发写冲突
   - 冲突时重试，超过重试次数返回 ErrStateCorrupted

**检查点实现：**
1. 检查点创建
   - 每次 Save 生成唯一 CheckpointID（时间戳 + 序列号）
   - 在 Sorted Set 中存储 sessionID → CheckpointID 的有序索引（Score 为时间戳）

2. 检查点查询
   - ListCheckpoints 按时间倒序返回分页元数据列表
   - LoadCheckpoint 按 CheckpointID 直接命中

3. 检查点清理策略
   - 保留最近 N 个检查点，超出时自动删除最老的（ZREMRANGEBYRANK）
   - N 值通过配置注入

**AppendMessages 优化：**
1. 增量追加
   - 使用 Redis List 的 RPUSH 命令追加新消息，不重写整体 State
2. 滑动窗口裁剪
   - 消息数量超过上下文窗口阈值时，自动触发 LTRIM 裁剪旧消息
   - 裁剪前将被移除的消息归档（写入冷存储键，供长期 RAG 使用）

---

## 模块 12：`PostgresMemoryStore` — 强一致性持久化

**数据库 Schema 设计：**
1. sessions 表
   - 主键 session_id、tenant_id、created_at、last_active_at、status
2. agent_states 表
   - 关联 session_id、state_json（完整状态快照）、version（乐观锁版本号）
3. checkpoints 表
   - checkpoint_id、session_id、created_at、state_json、node_id（在哪个节点时创建）
4. messages 表
   - 独立存储每条 Message，支持高效追加与分页查询
   - 包含 role、content_json、tenant_id、session_id、created_at

**基础操作实现：**
1. Save
   - 使用 upsert（INSERT ON CONFLICT UPDATE）+版本号校验，防止并发写覆盖
2. Load
   - 读取最新版本的 agent_states 记录
3. LoadCheckpoint
   - 按 checkpoint_id 精确查询
4. ListCheckpoints
   - 按 created_at 倒序，支持 limit/offset 分页

**连接池与事务：**
1. 使用连接池管理数据库连接（最大连接数、获取超时配置）
2. Save 和 AppendMessages 在同一事务中执行，保证一致性

---

## 模块 13：`Hook` 机制完整实现

**Hook 注册与调度：**
1. 引擎集成
   - Engine 维护 []LifecycleHook 列表，按注册顺序顺序调用
   - Hook 执行失败不中断主流程（隔离 Hook 侧的 panic）

2. Hook 上下文数据
   - HookMeta 结构包含：NodeID、执行耗时、当前 StepCount、错误信息（若有）

**`TokenCounterHook` 实现：**
1. 数据收集
   - 在 HookAfterNode 中读取 LLMResponse.TokenUsage
   - 按 TenantID + ModelName 维度累计 InputTokens / OutputTokens
2. 统计输出
   - 提供 GetSummary() 方法输出当前会话的 Token 消耗汇总
   - Phase 5 中接入 Prometheus MetricsCollector

**`LatencyMetricHook` 实现：**
1. 计时逻辑
   - HookBeforeNode 时记录开始时间戳
   - HookAfterNode 时计算耗时，按 NodeID 分类记录
2. 统计输出
   - 记录最大、最小、平均耗时
   - Phase 5 中上报 Prometheus Histogram

**`AuditLogHook` 实现：**
1. 审计事件生成
   - 在关键 HookPoint（BeforeNode、AfterTool、OnError）写入结构化审计日志
   - 字段：时间戳、TenantID、SessionID、操作类型、操作对象、结果
2. 日志输出适配
   - 支持写入本地文件、标准输出、或发往外部日志服务（通过接口适配）

**`ContextWindowGuardHook` 实现：**
1. Token 预估
   - 在 HookBeforeNode 时估算当前 Messages 总 Token 数（字符数 / 4 近似估算）
2. 主动压缩触发
   - 超过配置阈值（如模型上限的 80%）时，触发摘要压缩流程
   - 调用 LLMProvider 对旧消息生成摘要，替换原始消息列表
   - 压缩后更新 AgentState.Messages，继续原有 Node 执行

---

## 模块 14：类型化错误恢复体系

**AgentError 类型体系：**
1. 错误结构定义
   - Category（Retriable / Fatal / HumanNeeded / StateCorrupted）
   - NodeID（发生错误的节点）、Cause（原始 error）
   - RetryAfter（可选，来自限流响应头）

2. 错误构造辅助函数
   - NewRetriableError / NewFatalError / NewHumanNeededError 等
   - 支持从 HTTP 状态码自动推断 Category

**`RetryPolicy` 实现：**
1. 指数退避算法
   - 基础等待时间、最大等待时间、退避系数均可配置
   - 加入随机 Jitter（± 20%），防止雪崩重试

2. 重试条件判断
   - 只对 ErrRetriable 生效
   - 超过最大重试次数后降级为 ErrFatal

3. Context 超时感知
   - 每次等待前检查 Context.Deadline，即将超时则中止重试

**`CircuitBreaker` 实现（针对 LLMProvider 和 Tool）：**
1. 三态状态机
   - Closed（正常）→ Open（熔断）→ HalfOpen（探测恢复）
2. 状态转换规则
   - 滑动时间窗口内失败率超阈值 → Closed 转 Open
   - Open 持续 N 秒后允许单个探测请求 → 转 HalfOpen
   - 探测成功 → 转回 Closed；失败 → 重置 Open 计时
3. 熔断时的降级响应
   - LLMProvider 熔断时：返回预设的降级消息（"系统繁忙，请稍后重试"）
   - Tool 熔断时：返回 ToolResult 错误，引擎继续执行后续节点

**`RateLimiter` 实现：**
1. 令牌桶算法
   - 按 TenantID 维度独立限流（多租户公平隔离）
   - 配置每秒令牌数和桶容量
2. 等待策略
   - 可配置等待模式（阻塞等待 / 超时返回 ErrRetriable）

**`TypedErrorHandler` 集成进 Engine：**
1. 错误分发
   - Engine 捕获 Node.Process() 的 error 后交给 TypedErrorHandler
   - 根据 Category 分别调用 RetryPolicy / CircuitBreaker / Interrupt / Rollback

2. 状态回滚
   - ErrStateCorrupted 时从 MemoryStore.LoadCheckpoint 恢复
   - 回滚后将 State.Status 置为 Running，从检查点所在节点的后继继续

3. HumanNeeded 转 Interrupt
   - ErrHumanNeeded 转换为 InterruptSignal{HumanApproval}，经由 Engine 挂起流程

---

## 模块 15：Phase 2 集成测试

**真实 LLM 冒烟测试：**
1. 配置真实 API Key 的 E2E 测试（标记为 integration test，CI 中按需触发）
2. 验证同步 / 流式两种生成路径均可走通完整 ReAct 循环

**并发工具调用测试：**
1. 注册 3-5 个 MockTool，每个模拟不同耗时
2. 验证是否并发执行（总耗时 ≈ 最慢工具耗时，而非累加）
3. 验证结果顺序与调用顺序一致

**熔断重试测试：**
1. 注入一个前 N 次必定失败的 MockLLMProvider
2. 验证 RetryPolicy 指数退避行为
3. 验证 CircuitBreaker 在失败率超阈值后触发，探测请求正确发出

**持久化测试：**
1. Redis / Postgres Store 的 Save → 进程"重启" → Load 验证状态完整性
2. 检查点写入后 LoadCheckpoint 正确恢复到历史节点状态
3. 并发 Save 时乐观锁冲突处理验证

**Hook 测试：**
1. 验证 TokenCounterHook 在 LLMNode 执行后正确累计 token 数
2. 验证 ContextWindowGuardHook 在消息超限时触发压缩，压缩后 Messages 数量减少
3. Hook 内部 panic 不影响主流程继续执行

---

## Phase 2 模块依赖顺序

```
模块8（真实 LLMProvider）
    └── 依赖 Phase 1 的 LLMProvider 接口

模块9（ToolRegistry 并发执行）
    └── 依赖 Phase 1 的 Tool / ToolRegistry 接口

模块10（ReActPlanner）
    └── 依赖 模块8（LLMProvider）+ 模块9（ToolRegistry）+ Phase 1 Engine

模块11（RedisMemoryStore）
    └── 依赖 Phase 1 的 MemoryStore 接口

模块12（PostgresMemoryStore）
    └── 依赖 Phase 1 的 MemoryStore 接口（可与模块11并行开发）

模块13（Hook 机制）
    └── 依赖 Phase 1 Engine（注入 Hook 调度点）
    └── 依赖 模块8（TokenCounterHook 需要 TokenUsage）

模块14（错误恢复体系）
    └── 依赖 Phase 1 Engine（错误处理集成点）
    └── 依赖 模块11/12（状态回滚需持久化）

模块15（集成测试）
    └── 依赖 以上全部模块
```

---

> **Phase 2 完工验收标准：**  
> 使用真实 OpenAI API，在 Redis 持久化下，能稳定处理 100 并发 Session 的 ReAct 循环；工具并发执行时间不超过最慢工具的 1.2 倍；人为注入限流错误后熔断正确触发；会话中断后 Resume 状态完整恢复。