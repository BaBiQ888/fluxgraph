# 快速开始

在本章节，我们将引导您快速在本地启动一个 FluxGraph 环境，并以极少的代码行数体验 FluxGraph 引擎（`Engine`）驱动的第一次单节点流转。

## 1. 安装与依赖包配置

由于 FluxGraph 是基于 Go 原生的框架，请确保您的本地系统已安装 Go 1.22 或以上版本。

新建您的业务库或直接切入您的服务目录下，执行：

```bash
# 初始化模块
go mod init my_agent_app

# 下载并绑定依赖树
go get -u github.com/BaBiQ888/fluxgraph@latest
```

> [!TIP]
> 针对更高阶的记忆搜索与并发管理机制，FluxGraph 完全支持接入 **Redis (高速热层)** 与 **PostgreSQL + pgvector (海量冷记忆与 RAG 层)**。不过，由于本篇重点在于入门极简的控制流，第一张基于运行内存运转的“图”完全不需要任何外部中间件组件即可跑通！

## 2. 编写你的第一个图拓扑

FluxGraph 通过高度可复用的“图节点（Nodes）”以及指导流向的边缘关联（Edges）来调度每一个对话回合或者工具下推任务。

让我们仅用 30 几行代码写一个能够实现 "收到任务 -> 处理任务 -> 更新核心全局上下文 (`State`)" 的迷你框架：

请在你的根目录下创建 `main.go` 并粘入：

```go
package main

import (
    "context"
    "fmt"

    "github.com/BaBiQ888/fluxgraph/core"
    "github.com/BaBiQ888/fluxgraph/engine"
    "github.com/BaBiQ888/fluxgraph/graph"
    "github.com/BaBiQ888/fluxgraph/interfaces"
)

// ---------------------------
// 步骤一：定义属于你的业务执行节点
// ---------------------------
type HelloWorldNode struct {}

// 让框架识别该节点的唯一 ID 路由
func (n *HelloWorldNode) ID() string {
    return "hello_world"
}

// 引擎会在路由到该节点时回调此原生函数
func (n *HelloWorldNode) Process(ctx context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
    fmt.Println("🚀 节点已被引擎触发：世界，你好！")
    
    // 我们从 state 中拿数据或者追加数据。这里演示追加更新全局单向状态。
    newState := state.WithMessage(core.Message{
        Role: core.RoleAssistant,
        Parts: []core.Part{{Type: core.PartTypeText, Text: "AI 已经接管这个图节点，并完成了数据结算。"}},
    })
    
    // 将更新后的指针以及错误抛弃上交给引擎
    return &interfaces.NodeResult{State: newState}, nil
}

func main() {
    // ---------------------------
    // 步骤二：实例化拓扑图，并装配你的全量节点网
    // ---------------------------
    g := graph.NewGraph()
    
    // 注入我们刚写的节点
    g.AddNode(&HelloWorldNode{})
    
    // 强制声明第一道“安检门”，因为没有设置边条件，所以它执行完就会把状态返回外界（End 点）
    g.SetEntrypoint("hello_world")
    
    // ---------------------------
    // 步骤三：让沉睡的图纸动起来（挂载执行引擎）
    // ---------------------------
    ctx := context.Background()
    fluxEngine := engine.NewEngine(g)
    
    // 准备一块干净纯洁、没有记忆的白板原语作为起步投喂点
    initialState := core.NewAgentState()
    
    fmt.Println("正在拉起 FluxGraph Engine...")
    
    // Engine 将挂载 context 开始它的自动化流程（如有中断，此处会挂起）
    finalState, err := fluxEngine.Run(ctx, initialState)
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("✅ 最终返回产出成功。\n")
    // 出发了！来看看你通过纯状态驱动记录了什么：
    fmt.Printf("State 记忆痕迹: %+v\n", finalState)
}
```

### 控制台运转效果

你可以终端里执行它：
```console
$ go run main.go
正在拉起 FluxGraph Engine...
🚀 节点已被引擎触发：世界，你好！
✅ 最终返回产出成功。
State 记忆痕迹: &{...详细多模态日志}
```

## 3. 下一步

在此基础架构上，你其实就已经拥有了**一切扩展的基础图纸**！

* 想引入真实发散大模型的脑力？用 FluxGraph 中的 `providers` 实现原生 LLM 节点的调度。
* 想接入爬虫？自己新建一个装配实现 `interfaces.Tool` 约定的工具并通过 `tools.Registry` 注入！

为了彻底理解这背后优雅的架构解耦与设计模式，欢迎继续阅读下一章节：[使用 FluxGraph 思考 (图论原语哲学)](03_concepts.md)。
