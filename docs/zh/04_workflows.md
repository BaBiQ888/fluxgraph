# 工作流与状态机制 (Workflows & Mechanisms)

在上一章里，我们介绍了单节点是如何触发与结束的。这一章，我们将深入核心图引擎 (`graph` 与 `engine` 包)，展示 FluxGraph 是如何优雅地在多节点之间进行条件路由与数据交换的。

## 1. 节点的注册与编排 (Nodes)

任何需要被大图调度的执行单元都必须实现 `interfaces.Node` 接口约束：

```go
type Node interface {
    ID() string
    Process(ctx context.Context, state *core.AgentState) (*NodeResult, error)
}
```

这里 `NodeResult` 决定了该节点结束后携带的是什么状态，供下一个节点使用。

你可以像搭积木一样添加无数个业务独立的节点：

```go
g := graph.NewGraph()
g.AddNode(&FetchWeatherNode{})
g.AddNode(&LLMAnalyzerNode{})
g.AddNode(&StoreDBNode{})
```

## 2. 边与条件路由 (Edges & Conditional Edges)

没有边约束的节点孤岛是没有意义的。FluxGraph 提供了两种连接方式。

### 线性直连 (Linear Edge)

如果你想让 `FetchWeatherNode` 结束完后，**必须毫无条件地**流转给 `LLMAnalyzerNode`：

```go
// 这行代码建立了一条强一致性的有向无环边
g.AddEdge("fetch_weather_id", "llm_analyzer_id")
```

### 条件判定边 (Conditional Edge)

这也是驱动大语言模型复杂规划逻辑的核心！假设 `LLMAnalyzerNode` 返回了一段 `State`，如果是成功我们走向 `StoreDBNode` 并结束；如果是失败我们需要退回 `FetchWeatherNode` 重新抓取。

你需要实现一个断言函数：向图注入 `AddConditionalEdge`：

```go
g.AddConditionalEdge("llm_analyzer_id", func(ctx context.Context, state *core.AgentState) (string, error) {
    // 读取 LLM 的最终决断记录
    lastMsg := state.LatestMessage()
    if strings.Contains(lastMsg.Parts[0].Text, "ERROR") {
        // 返回下一个必须去往的节点 ID 定向
        return "fetch_weather_id", nil 
    }
    // 成功则去往入库节点
    return "store_db_id", nil
})
```

这就是大模型“循环思考”框架 `ReAct` 工作流能够一直执行直到产生合格结果的最核心运转秘密。无论多少次重新拉起，只要条件函数指向了前序 Node 的 `ID`，图引擎就会携带更新后的全局 `State` 把状态交还给节点重新再运算。

## 3. 终结与起始守门人

不要忘记告诉引擎从图的哪个点开始切入，以及到哪里算大图的边缘停顿。

```go
g.SetEntrypoint("fetch_weather_id")
```

引擎在启动时必定顺着这里进入。
而退出的原则也非常简单，FluxGraph **没有显式的 `EndEdge`**。
在 FluxGraph 引擎里，**如果当前节点运行结束且没有任何后置边（直连或条件）匹配到下一个去向节点，那么当前图任务就此“终结”**，最终的计算结果 State 就会被抛出：

```go
finalState, err := fluxEngine.Run(ctx, ...)
```

## 4. Hook（生命周期钩子）

真正应对生产的框架允许你在流水线流经的过程旁路切入逻辑。通过 `engine.Hooks`，你能够在每走过一个边缘前获取最高控制权，用于打点日志或者甚至挂起流程！这是这套有向流里极度强大的高级特性：我们在稍后的“可观测性”或者“中止与人类在环”等篇幅中会用大量代码案例论述它的重要性。

---

如果这一整套逻辑让你想起了你在纸上画产品逻辑流程图的思考方式，那就对了！将大模型的每一个 prompt 请求封装在这样的框图闭环内，你的微服务就不会再“失控”了。

下一章我们将讲解：用真实强大的 LLM 和沙盒去填充节点内部 —— **[大模型组件与工具沙盒编排](05_providers_tools.md)**。
