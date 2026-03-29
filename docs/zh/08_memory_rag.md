# 记忆系统：极速热层与冷域 RAG (Memory Systems)

如果要问是什么功能让 FluxGraph 在市面上脱颖而出，那必是它的“双层持久化与向量记忆架构”。

传统的框架要么不支持记忆（断开就忘），要么全靠你自己在代码外写一块数据库增删改查。如果用户的会话积攒了三年的记录，把所有对话丢进 LLM 那立刻会导致 Token 爆炸并报 Context Length Exceeded。

FluxGraph 为你的微服务出厂标配了一套双层记忆引擎，做到了真正的“懂得多、记性好、还不占内存”。

## 1. 原理：分治架构设计

你需要在使用框架前拉起 `Redis` 和 `PostgreSQL`：
- **热记忆区 (RedisStore)** 缓存最近 N 周期的最热状态上下文。由于纯内存在毫秒级响应，它用于抵抗高并发的“同一局势/Session”内的连环问答；
- **冷记忆区 (PostgresStore + pgvector)** 后台通过特定的 `memory/eventbus` 在每个高价值节点完结时，异步将 `Message` 中的语义段落丢给 OpenAI Embedding 模型向量化，最后埋进带 `vector` 扩展的 Postgres 数据表中。

## 2. 热层：Redis 速率与上下文维护

在系统入口或者启动图引擎前，声明接入 Redis：

```go
import "github.com/BaBiQ888/fluxgraph/storage"

redisClient := storage.NewRedisDriver("localhost:6379", "")
hotStore := storage.NewRedisStore(redisClient)

fluxEngine.EnableMemory(hotStore)
```

现在，你的 `AgentState` 在大图执行过程中被赋予了极高吞吐的会话存储。它极其适合例如聊天机器人的中间频繁打断点，也可以用作微服务防刷限流器。

## 3. 冷层与原生 RAG 集成 (Postgres + pgvector)

当你的 LLM 想回答“上个月你和我敲定了什么需求吗？”的时候，依靠图节点自带的上下文是无法实现的（因为老早因为 Token 限额截断了被刷掉了）。

这时候，FluxGraph 的 `postgres_store.go` 就发威了：

### 配置与初始化
首先你需要在框架接入一个负责做 Embedding 降维抽取的模型（通常是 OpenAI 的 `text-embedding-v3`）：

```go
import "github.com/BaBiQ888/fluxgraph/providers"

embedder := providers.NewOpenAIEmbeddingProvider("sk-your-key", "text-embedding-3-small")
pgDriver := storage.NewPGXDriver("postgres://user:pass@localhost:5432/fluxgraph?sslmode=disable")

coldStore := storage.NewPostgresStore(pgDriver, embedder)
```

### 将记忆赋予 Agent 的大脑工具箱
你并不需要去写各种奇怪的数据抓取节点去打断原来的逻辑流。框架在 `tools` 包里内置了一个极其优雅的 `SearchMemoryTool` 原生工具：

```go
// 初始化一个搜索“深层古老记录”的自带函数工具
searchTool := tools.NewSearchMemoryTool(coldStore)

// 注入给图节点的工具集合！
toolRegistry.Register(searchTool)
```

**魔法发生了！**
当用户在对话中问起古老话题，图里面驱动的 LLM 会突然意识到自己的上下文中丢失了这块逻辑，它会**自主地选择调用 `search_memory` 这个工具函数**。

它自己组织用户的疑问文本传入工具，而该工具底层会调用你刚配置的 `OpenAIEmbedding` 将该意图转为向量 `[0.1, 0.44... -0.5]`，直接下推打进 Postgres 的 `pgvector` 层，进行 `余弦相似度` 或者 `L2` 临近查询，并把最相关的几条几个月前的话术结果原路返还给模型。

> [!NOTE]
> 这个动作浑然一体：**语义感知 -> 发现记忆盲区 -> 自动触发工具 -> 向量召回 (RAG) -> LLM 合成回答**。这一切，你只需要在声明图中花 3 行代码挂靠工具即可，核心 RAG 护城河我们全都为你打通了。

---
现在你的 Agent 已经无所不知了！
接下来是冲刺阶段，让我们把它安全、平稳地通过测试用例和风控护栏，投放入真实的世界：进入第五阶段 [测试与可观测性生产指南](09_testing_eval.md)。
