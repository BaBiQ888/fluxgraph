# 人类环中控制与分布式 A2A 代理子图 (Interrupts & Distributed Subgraphs)

在传统的 Agent 教程里，你可能是把整个链条写死：大模型生成代码，保存，然后执行。但在企业网关和风控环节呢？
如果不加入人为干预或者分布式的审查隔离，没有人敢在生产发版。

FluxGraph 为高级架构师留下了最可怕的控制武器。

## 1. 中止、审查与人类在环 (Human in the loop / Interrupts)

通过大图引擎自带的 `engine.Hooks`，你可以让图在进入任意一个高危 `Node`（例如负责转账的沙盒节点）之前强制阻断！

```go
engine.RegisterHook(&engine.BeforeNodeHook{
    TargetNodeID: "exec_transfer_money_node",
    Handler: func(ctx context.Context, state *core.AgentState) error {
        // 在该方法里，阻断引擎的自动轮转。
        // 将此任务的记录持久化扔给人工审查面板，然后显式抛出一个 InterruptError
        return engine.NewInterruptError("Waiting for human approval")
    },
})
```

图引擎在遇到中断信号时并不会丢失状态，而是直接冷冻该图执行实例并封存当前的一切上下文变量（配合我们前一章的 `Persistence` 层）。

**人类环重返！**

人类风控员审核修改了用户的输入后，使用 `fluxEngine.Resume(ctx, updatedState)`，该节点就会像什么事也没发生过一样，拿着新的带有授权签名的上下文接着走完转账图流转。

## 2. 跨语言的大型集群“子图” —— A2A 机制

如果你看过 LangGraph 的“子图 (Subgraphs)”，你可能觉得不过就是把另一个图的代码作为当前图的一个节点 `NewNode()` 塞进去。

**这格局太小了！** 在 FluxGraph 宇宙中，我们提供了震惊业界的微服务抽象。它是业界首个在通信协议底层就做好了互通准备的 Agent 框架 —— ** Agent-to-Agent (A2A) 模块**。

你不再受限于在一个单一的 Go 进程里吃光机器资源。你的“图”甚至可以调用部署在隔壁 Kubernetes 集群里的 Python 容器（需使用兼容同宗 gRPC `.proto` 约定的客户端）。

### 代理节点 (Delegate Node)

在图中的表现，对于主调引擎而言，这依旧是一个普通的 `Node`。只不过你放入的类叫做 `DelegateNode`：

```go
package main

import "github.com/BaBiQ888/fluxgraph/a2a"

// 将远端服务器上挂载的某个大图业务，无缝代理成一个本地节点！
remoteWorkerNode := a2a.NewDelegateNode(
    "remote_data_cleaner", // 本地注册叫什么名字
    "grpc://10.0.0.8:9090", // 隔壁跑着另一个子集群 Agent
)

// 当做普通 Node 插到本地图上
g.AddNode(remoteWorkerNode)
```

当你这个微服务走到这个节点时，由于 `DelegateNode` 内置了 A2A gRPC Client 客户端。它会连同当前的 `AgentState` 进行高效轻量的 protobuf 序列化，跨过内网打给那个远端的 Agent。

远端的微服务在自己的图中进行思考完、沙盒代码跑完以后，**返回修改好的增量 AgentState 到这里**，并在此引擎唤醒流转。

> [!IMPORTANT]
> 这是 FluxGraph 设计中最精妙的宏图。它使得你写的一个一个小型图逻辑，最后能像蜂群 (`Swarm`) 或者微服务集群一样编织在一起，极速扩展，甚至解除了多语言系统团队孤岛。

我们在接下来的部分：[08. 高速热层与冷域知识记忆库 (Memory & RAG)](08_memory_rag.md) 里，继续探讨为什么你能给这群集群赋予惊人的上下文历史搜寻能力。
