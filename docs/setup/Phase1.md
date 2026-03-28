Phase 1 — 地基（第 1-4 周）

| 模块 | 交付物 |
|------|--------|
| Layer 0 核心原语 | `Context`, `Message`, `Part`, `AgentState`, `Artifact` 定义 + 单元测试 |
| Layer 1 接口定义 | 所有 Interface 定义，无实现 |
| InMemory 实现 | `InMemoryStore`, `InMemoryEventBus`, `MockLLMProvider` |
| GraphBuilder | 声明式 API + 静态/条件边 |
| Engine 基础版 | 同步 EventLoop，无 Hook，无错误恢复 |

**验收标准：** 能跑通一个本地 `LLMNode → ToolExecutorNode → RouterNode` 的完整 ReAct 循环。

# FluxGraph — Phase 1 任务拆分

---

## 模块 1：项目工程骨架

**目录结构设计：**
1. 规划顶层包结构（核心原语包、接口包、引擎包、实现包、测试包）
   - 按照分层架构划定各层代码边界
   - 确保接口层与实现层物理隔离，避免反向依赖

2. 依赖管理初始化
   - 初始化模块管理文件（go.mod 或 pyproject.toml，依选型）
   - 确定测试框架、断言库等基础依赖
   - 配置 lint 与格式化工具（Golangci-lint / Ruff 等）

3. CI 基础配置
   - 配置本地 pre-commit hook（格式检查、静态分析）
   - 编写 Makefile / taskfile 定义常用命令（build、test、lint）

4. 版本与变更日志规范
   - 确定语义化版本策略
   - 建立 CHANGELOG 模板

---

## 模块 2：Layer 0 — 核心原语

**`Context`（执行上下文）：**
1. 字段定义
   - 包含 TraceID、SpanID、TenantID、SessionID、Deadline、Metadata
   - TenantID 设计为非可选字段，构造时强制传入
2. 构造函数设计
   - 提供带 Deadline 的标准构造方法
   - 提供支持父子继承的派生方法（子 Context 继承父 TenantID）
3. 取消信号机制
   - 封装标准库的 cancel 函数，对外只暴露 Cancel() 方法
   - 超时到达时自动触发取消信号

**`Part`（内容最小单元）：**
1. 类型枚举定义
   - 枚举所有 Part 类型：Text、File、StructuredData、ToolCall、ToolResult
2. FileRef 设计
   - 只存 URI 引用，不存 raw bytes，避免内存膨胀
   - 包含 MIME Type 字段，用于 A2A 能力协商
3. ToolCall/ToolResult 字段设计
   - ToolCall 包含：调用 ID、工具名、参数 Map
   - ToolResult 包含：对应的调用 ID、结果内容、执行状态

**`Message`（标准消息结构）：**
1. Role 枚举定义
   - 覆盖 User、Assistant、System、Tool 四种角色
2. Message 字段设计
   - 包含 ID、Role、Parts 列表、时间戳、Metadata
   - 包含 ContextID 和 TaskID（预留 A2A 协议绑定字段，Phase 1 可为空）
3. Parts 校验规则
   - 不同 Role 允许携带的 Part 类型进行约束（如 Tool Role 只能携带 ToolResult）

**`Artifact`（产出物）：**
1. 字段设计
   - 包含 ID、Name、Parts 列表、Metadata
   - 设计 AppendPart / Replace 方法用于流式追加场景
2. 与 Message 的职责边界
   - 明确规则：过程通信用 Message，任务输出用 Artifact

**`AgentState`（全局状态对象）：**
1. 核心字段设计
   - Messages 列表、Variables 业务变量 Map、Artifacts 列表
   - 执行元数据：StepCount、RetryCount、LastNodeID、Status 枚举
   - 检查点引用 CheckpointID、A2A 绑定字段（TaskID、ContextID）
2. Status 枚举定义
   - 完整枚举：Running、Paused、WaitingHuman、Completed、Failed
3. 不可变快照设计
   - 每次状态更新返回新对象，不原地修改（函数式风格）
   - 提供 `With*` 系列方法（如 WithVariable、WithMessage）便于链式更新
4. Variables 类型安全封装
   - 提供类型断言辅助方法，避免直接 `any` 取值报错

---

## 模块 3：Layer 1 — 接口定义层（无任何实现）

**`LLMProvider` 接口：**
1. 同步生成方法签名
   - 入参：Context + AgentState；出参：标准 Response 结构 + error
2. 流式生成方法签名
   - 出参为 TokenDelta channel，调用方负责消费
3. `LLMResponse` 结构定义
   - 包含 Message、TokenUsage（输入/输出 token 分别统计）、FinishReason
4. `TokenDelta` 结构定义
   - 包含 DeltaType（Text / ToolCall / Done）和对应 Content
5. `ModelInfo` 结构定义
   - 用于 AgentCard 自动生成阶段的元数据（名称、最大 Token 数等）

**`Tool` 接口：**
1. 元信息方法签名
   - Name()、Description()、InputSchema()、RequiredPermissions()
2. Execute 方法签名
   - 入参：Context + 参数 Map；出参：字符串结果 + error
3. `ToolInputSchema` 结构定义
   - 对齐 JSON Schema 标准（Type、Properties、Required 字段）

**`ToolRegistry` 接口：**
1. 注册方法签名
   - Register(tool, permissions...) 支持权限标签绑定
2. 查询方法签名
   - GetTool(name)、ListTools()（返回所有工具定义，供 LLM 消费）
3. 并发执行方法签名
   - ExecuteConcurrent(ctx, []ToolCall) 返回 []ToolResult（保持顺序）
4. 鉴权方法签名
   - AuthorizeCall(ctx, toolName) 依据 TenantID 校验权限

**`MemoryStore` 接口：**
1. 基础读写方法签名
   - Save(ctx, sessionID, state)、Load(ctx, sessionID)
2. 检查点方法签名
   - LoadCheckpoint(ctx, checkpointID)、ListCheckpoints(ctx, sessionID)
3. 增量追加方法签名
   - AppendMessages(ctx, sessionID, []Message)（性能优化，不整体 Save）
4. 语义检索方法签名
   - Search(ctx, sessionID, query, topK)（Phase 1 只定义，不实现）

**`Node` 接口：**
1. 标识方法签名
   - ID() 返回节点唯一标识
2. 执行方法签名
   - Process(ctx, state) 返回 NodeResult + error
3. `NodeResult` 结构定义
   - 包含更新后的 State、NextNodes 强制路由列表、Interrupt 中断信号
4. `InterruptSignal` 结构定义
   - 包含 InterruptType 枚举（HumanApproval / InputRequired / ExternalEvent）
   - 包含 Payload 和 ResumeKey（EventBus 唤醒键）

**`Planner` 接口：**
1. Plan 方法签名
   - 入参：Context + 高层目标字符串 + AgentState；出参：Plan + error
2. Revise 方法签名
   - 入参：当前 Plan + AgentState；用于执行中途动态修订计划
3. `Plan` 结构定义
   - Steps 列表、Strategy 枚举（Sequential / Parallel / Adaptive）
4. `PlanStep` 结构定义
   - 包含 ID、Description、映射的 NodeIDs、依赖的前置 StepID 列表

**`EventBus` 接口：**
1. 发布/订阅方法签名
   - Publish(Event)、Subscribe(EventType, handler) 返回 SubscriptionID
   - Unsubscribe(SubscriptionID)
2. `Event` 结构定义
   - 包含 EventType 枚举、SessionID、TaskID、Payload、Timestamp
3. EventType 枚举完整定义
   - AgentPaused、AgentResumed、TaskCompleted、NodeStarted、NodeCompleted、ToolCalled

---

## 模块 4：InMemory 基础实现

**`InMemoryStore`：**
1. 数据结构选型
   - 使用线程安全的 Map（带读写锁）存储 sessionID → AgentState
   - 使用有序列表存储 sessionID → []CheckpointMeta
2. Save / Load 实现
   - Save 时对 AgentState 做深拷贝，防止引用共享导致状态污染
   - Load 时同样返回深拷贝
3. AppendMessages 实现
   - 只追加 Messages 字段，不替换整体 State
4. ListCheckpoints 实现
   - 按时间排序返回所有快照元数据

**`InMemoryEventBus`：**
1. 订阅者管理
   - 使用 Map 存储 EventType → []Handler 列表，支持多订阅者
   - 订阅返回唯一 SubscriptionID，Unsubscribe 时按 ID 精准移除
2. Publish 实现
   - 同步遍历所有匹配的 Handler 并调用
   - Phase 1 允许同步调用，Phase 3 升级为异步

**`MockLLMProvider`：**
1. 预设回复序列设计
   - 内部维护 []LLMResponse 列表，按调用次序顺序返回
   - 超出预设数量时返回可配置的 fallback 回复
2. 流式模拟实现
   - 将预设 Response 拆分为字符级 TokenDelta，通过 channel 逐字发送
3. 调用记录能力
   - 记录每次调用的入参（AgentState 快照），便于测试断言

**`MockToolRegistry`：**
1. 工具预设行为注册
   - 支持按工具名配置 固定返回值 或 自定义行为函数
2. 调用记录能力
   - 记录每次 Execute 的入参，便于验证工具是否被正确调用
3. 鉴权模拟
   - 可配置指定工具返回权限拒绝，用于安全边界测试

---

## 模块 5：GraphBuilder

**节点管理：**
1. 节点注册
   - AddNode(id, Node) 方法，ID 重复时报错
   - 维护内部节点 Map，按 ID 快速查找
2. 入口/出口节点设置
   - SetEntry(nodeID) 指定图的起始节点
   - SetTerminal(nodeID...) 支持多个终止节点

**边的定义：**
1. 静态边
   - AddEdge(fromID, toID) 完成后无条件跳转
   - 记录为邻接表结构
2. 条件边
   - AddConditionalEdge(fromID, routerFunc) 路由函数接收 AgentState 返回 nextNodeID
   - 允许路由函数返回多个候选值（用于并行分支，Phase 后续实现）
3. 自环检测
   - 条件边允许回到自身或祖先节点（支持 ReAct 循环）
   - 通过最大步骤数（MaxSteps）在引擎层防止无限循环

**图验证（Build 阶段）：**
1. 完整性校验
   - 检查所有边引用的 NodeID 是否已注册
   - 校验 Entry 节点已设置
2. 可达性校验
   - 警告（非阻断）存在不可达节点（从 Entry 出发无法访问到的节点）
3. 图快照（不可变）
   - Build() 后返回只读 Graph 对象，防止运行时修改

---

## 模块 6：Engine 基础版（同步 EventLoop）

**路由决策：**
1. 静态路由解析
   - 根据当前 `LastNodeID` 查找邻接表，获取 nextNodeID
2. 条件路由解析
   - 若边为条件边，执行路由函数，传入当前 AgentState 获取 nextNodeID
3. NodeResult 强制路由优先级
   - 若 `NodeResult.NextNodes` 非空，优先使用节点自身指定的后继，覆盖图的路由

**EventLoop 主循环：**
1. 循环退出条件
   - 当前 Status 为 Completed / Failed
   - nextNodeID 为空（无后继节点）
   - StepCount 达到 MaxSteps 上限（返回 ErrMaxStepsExceeded）
2. 节点执行调用
   - 调用 `node.Process(ctx, state)`，更新 State 和 StepCount
3. 中断信号处理
   - 检查 `NodeResult.Interrupt` 是否非 nil
   - 若有中断信号，将 Status 设为 Paused，调用 MemoryStore.Save，退出循环

**Resume 恢复机制：**
1. 从 MemoryStore 加载挂起状态
   - 按 SessionID 取出 Paused 状态的 AgentState
2. 注入恢复数据
   - 将外部输入（人工审批结果 / Webhook 回调）写入 Variables
3. 重置状态并继续循环
   - 将 Status 改回 Running，从 LastNodeID 的后继节点继续执行

---

## 模块 7：单元测试

**核心原语测试：**
1. Context 测试
   - 验证 Deadline 触发 Cancel 信号
   - 验证子 Context 继承父 TenantID
2. AgentState 不可变性测试
   - 验证 `With*` 方法返回新对象，原对象不受影响
3. Message Role 合法性测试
   - 验证非法 Part 类型组合被正确拒绝

**GraphBuilder 测试：**
1. 正常图构建验证
2. 非法边引用应报错，不可达节点应发出警告
3. Entry 未设置时 Build 应失败

**Engine EventLoop 测试：**
1. 线性图（A→B→C）完整执行，验证最终 AgentState 正确
2. 条件边路由验证（根据 State 变量走不同分支）
3. MaxSteps 触发验证
4. Interrupt 挂起后 Resume 验证（State 正确持久化并恢复）

**MockLLMProvider / MockTool 测试：**
1. 预设序列按顺序消费验证
2. 流式输出 TokenDelta 顺序性验证
3. 工具调用记录被正确追踪

---

## Phase 1 模块依赖顺序

```
模块1（骨架）
    └── 模块2（核心原语）
            └── 模块3（接口定义）
                    ├── 模块4（InMemory 实现）
                    │       └── 模块6（Engine）
                    └── 模块5（GraphBuilder）
                                └── 模块6（Engine）
                                            └── 模块7（测试）
```

> **Phase 1 完工验收标准：** 用 `MockLLMProvider` + `MockToolRegistry` + `InMemoryStore`，能跑通一个 `LLMNode → ToolExecutorNode → RouterNode → LLMNode（回环）→ TerminalNode` 的完整 ReAct 循环，且所有单元测试绿灯。