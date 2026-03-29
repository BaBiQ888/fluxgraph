# 高阶执行：持久化、重试容错与流式传输

在企业级应用中，脚本跑得对不对只是第一步，**“跑挂了能不能恢复”、“中间耗时太长能不能保存进度”** 才是决定框架能否上生产的关键。

FluxGraph 为高级工程师提供了极其健壮的底层机制保障。

## 1. 持久化 (Persistence) 与持久执行

传统的 LLM 脚本挂了就全丢了。而在 FluxGraph 里，每一次节点 `Process()` 吐出的 `State` 都可以被自动推入后端的 `TaskStore` 中打上检查点 (Checkpoint)。

这叫做 **断点持久执行**：

```go
// 通过 Redis 或 Postgres 实例化一套存储
storage := storage.NewTaskStore(pgDriver)

// 通知引擎：我不仅要你跑大图，还要你一步一步记在冷库里
fluxEngine.EnablePersistence(storage)

// 当机器重启、或者容器重新拉偏后，我们可以根据某个具体的 TaskID（对应线程 ID）无缝恢复
recoveredState, err := fluxEngine.Resume(ctx, "task-abc-1234")
```

任何中断的流只要恢复，引擎就会根据 `TaskStore` 里保存的最后一个经过的 `NodeID` 及最近的 `State` 快照，无缝将图再拉起。这就是“时间旅行”技术的底层原理。

## 2. 局部容错退避重试 (Resilience)

图跑着跑着，API 偶发超时了怎么办？如果不加保护整个大流程会功亏一篑。
FluxGraph 引擎支持在节点级抛出特定的 `error`，促使图产生原生的**重试退避流**：

```go
func (n *FragileNode) Process(ctx context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
    result, err := RequestFragileAPI()
    if err != nil {
        // 返回引擎约定的重试错，触发外部挂载的 Resilience 重试算法（如指数退避）
        return nil, core.NewRetryableError(err, 3) // 允许最多重试 3 次
    }
}
```

此外，你还能使用上章节提到的 `providers.NewFallbackProvider`，确保连模型本身崩溃时都能被无感兜底。这极大简化了业务代码中满天飞的 `try...catch...sleep` 噪音。

## 3. 图节点事件流与大模型流式传输 (Streaming)

FluxGraph 对于追求极致前端交互体验的项目来说极其友好。

### 图执行状态流

当你通过 `Engine` 驱动长周期执行时（长达数分钟的思考图），你不用一直傻等着最终那个 `finalState` 同步返回。

目前内置的高级 A2A 通信协议或 `eventbus`，早已将其实现为了可订阅的**基于通道或者 SSE 推送的事件流**：

只要引擎在一个 Node 上处理完毕，它就会通过内部 EventBus 下发一个局部增量的状态更新。前端用户可以实时看到界面上闪过：“正在获取天气... -> 获取成功... -> 模型推演预测中...”。

### 模型响应流式的多级映射机制

更进一步的流式概念，是 LLM 生成文本过程中的 Token 流。
由于 `AgentState.Message.Parts` 被严格界定为了纯函数化的值，FluxGraph 底层的 Provider 在处理大段文案流时，利用了协程与缓冲池结构 `(engine/hooks.go)`，支持在一整个步骤落库之前，将缓冲池内的差异切片快速通过回调挂钩推离节点外。这样无论前端业务再复杂，拿到的依然是一根干净、原生的数据管道流。

以上便奠定了最基础的长运行服务护城河。在下一个极其重磅的高级篇章 [07. 中止控制与分布式 A2A 代理子图](07_interrupts_subgraphs.md) 中，我们将揭晓 FluxGraph 是**如何让不同机器甚至不能共存内存里的程序也能进行子图嵌套互通的巨大秘密。**
