# FluxGraph 产品化与生产级架构需求 (Product Requirements)

这份文档是对 FluxGraph 第 0-6 层核心技术原语之上的**产品级和生产级**架构增强需求汇总。
此前的系统虽然在调度引擎和图执行模型上具备基础能力，但在开发者使用、生产部署和多租户运维上存在空白，这 10 个维度将作为框架商用/大规模生产部署的必经之路。

## 优先级评估表

| 优先级 | 补充项 | 原因 |
|---|---|---|
| 🔴 必须（影响上线） | 分布式高可用 + 幂等性 + 数据隐私合规 | 缺这几块，多实例部署下会有严重问题 |
| 🟠 高优（影响使用） | 开发者体验（CLI + 文档）+ 成本配额 | 没有好 DX 框架无法推广；没有配额生产环境失控 |
| 🟡 中优（影响完整性） | 上下文窗口管理 + HITL 完整闭环 + 版本灰度 | 影响 Agent 质量和运营效率 |
| 🟢 长期规划 | 插件生态 + 数据飞轮 + 多语言 SDK | 生态建设，非框架核心但决定天花板 |

## 1. 开发者体验 (DX)
- **CLI 工具 (`fluxgraph`)**
  - `init` - 生成工程骨架和配置模板
  - `tool add` - 注册生成 Tool 接口
  - `session inspect <id>` - 命令行查看事件时间线
  - `replay <id>` - 本地回放调试
  - `eval run` - 触发评估测试
- **接入层 SDK**
  - 原生 Go SDK
  - 通过 A2A 封装的 Python / TS SDK
- **本地调试 Playground**
  - 本地 Web UI 展示执行路径、Token 和单步调试，支持手动注入状态变量
- **完善的文档**
  - Quickstart, API Reference, ADR 架构决策, Troubleshooting

## 2. 上下文窗口管理策略
- **热区（直接上下文）**：近期 N 轮 Message 完整呈现给 LLM
- **温区（中期摘要）**：超出热区的打包为 SystemMessage 形式
- **冷区（长期归档）**：转入向量数据库以 RAG 方式召回
- **触发机制**：Token 比例 (70%)、轮数、时间衰减
- **质量与保护**：对压缩前后做 Eval；`pinned: true` 标记防止核心 Message 被误压

## 3. 成本控制与配额管理
- **租户配额 (Tenant Quota)**：日/月 Token 上限设置（Redis 缓存 + PG 持久化）
- **触发与降级**：
  - 80% 触发内部事件告警警告
  - 100% 触发 `QuotaExceededError` 熔断或等待排队
- **Token 到 USD 的费用映射**
  - 导出指标 `fluxgraph_cost_estimate_usd_total` 供 Grafana 展示
  - 计费差异化与多租户隔离

## 4. 版本管理与灰度发布
- **构成维度的序列化**：System Prompt、Tools、Graph Node、LLM 型号等 YAML 快照
- **多版本生命周期**：存储在 `agent_versions` 的记录
- **灰度策略与回滚**：
  - 基于租户 ID 灰度 / 随机流量 (5%) 灰度
  - 支持 EvalHarness 准入测试作为门禁，配置级别秒级回滚

## 5. 幂等性与消息去重
- **A2A 重试防重叠**：接收方根据客户端提供的唯一 `messageId` 在 Redis (TTL=24h) 进行判重。若命中则复用上次 Task，不重新派发。
- **任务幂等**：如 CancelTask、配置 Webhook 等同一意图多次操作始终返回终态。

## 6. Human-in-the-Loop (HITL) 完整闭环
- **审批信号拓展**：结合 EventBus 向第三方通讯系统（企业微信、Slack）下发富文本卡片告警。
- **操作闭环**：除了支持挂起恢复外，增加审批 Web UI、参数修改、批准或拒绝。
- **超时降级规则**：24H 不批转拒绝/取消。
- **差异化条件流转**：结合工具调用类型的风险打分机制筛选强制审批场景。

## 7. 数据隐私与合规
- **数据主权**：支持 GDPR 标准清理所有关联日志 (`DELETE /tenants/{tenantID}/data`)
- **驻留隔离**：按区域拆分 Memory/Task 持久化环境 (Region Isolation)
- **合规审查**：Message 脱敏、禁止 Trace 抓取内容文本、Webhook 与密钥打码

## 8. 分布式高可用设计
- **无状态核心**：引擎不持有状态，完全靠 MemoryStore (Redis/PG) 托管执行流。
- **基于哈希路由**：同一 Session 路由到同一节点以复用内存在网缓存。宕机容错恢复。
- **分布式锁**：基于 Redis 防止 Session 快照并发写锁、超时后实例接管。
- **系统面选举**：Leader 选举执行配额清算、账本同步等全局低频调度作业。

## 9. 生态与插件体系
- **松耦合机制**：Tools、Nodes 组件基于 Go 的 `init()` 及 `import _` 包引入自我注册机制 (ToolRegistry)
- **隔离安全**：插件执行时的 Panic 兜底 (Recover)，基于 Tenant 范围隔离权限
- **远期**：标准化三方插件

## 10. 端到端数据飞轮
- **评价收集**：为客户端提供质量标记接口 `POST /tasks/{taskID}/feedback`
- **自动样本集**：高质量任务自动采纳正向样本；纠偏或差评提标负判定。
- **EvalScenario 泛化**：动态依据生产高频场景固化成 Eval Harness 用例，供后续回归门禁。
