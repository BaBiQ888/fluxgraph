# 大模型组件与工具沙盒 (Models & Tools Sandbox)

在掌握了 `Node` 与 `Edge` 的工作流后，怎样才能让节点真正拥有“智力”和“双手”呢？这就依赖于 FluxGraph 的 `providers` 和 `tools` 模块。

## 1. 大模型接入层 (Providers)

FluxGraph 原生内置了统一的 `interfaces.LLM` 接口标准，将各类大模型厂商复杂的 HTTP 调用封装成了极简的黑盒体验。你可以在图的 `Node` 内部毫不费力地调用它们来完成一整个阶段的思考。

### OpenAI 与 Anthropic 原生支持

你可以在初始化图组件时传入你喜欢的模型：

```go
package main

import (
    "github.com/BaBiQ888/fluxgraph/providers"
)

// 加载 OpenAI 提供者
openaiProvider := providers.NewOpenAIProvider("sk-your-openai-key", "gpt-4o")

// 或者你更喜欢 Claude：
claudeProvider := providers.NewAnthropicProvider("sk-ant-your-key", "claude-3-5-sonnet")
```

最重要的是，它们输出的全部是标准化格式，也就是它们都接受并在最终返回我们之前提到的 `AgentState` 级别的纯 `Message`。在节点 `Node.Process` 里，你只需调用 `provider.Generate(ctx, state.Messages(), tools)`。

### 高级用法：模型 Fallback 容灾链 (Resilience)

在真正的微服务生产环境里，单点依赖 OpenAI 是极度危险的（例如遇到高并发限流或者服务崩溃宕机）。FluxGraph 是企业级的，这也正是区别于传统个人 Python 项目的地方。

借助自带的 Fallback 守护方案组合，你可以串联它们：

```go
// 优先使用最高质量但脆弱的 OpenAI
primary := providers.NewOpenAIProvider("xxx", "gpt-4o")

// 一旦上面的接口 502/超时，退避到底座更稳的 Claude 上
secondary := providers.NewAnthropicProvider("xxx", "claude-3-5-sonnet")

// 这就是具有自我恢复能力的包装 Provider
resilientProvider := providers.NewFallbackProvider(primary, secondary)
```
当你将这个 `resilientProvider` 交给任何你的业务节点去 Generate 推理大模型时，它甚至可以自动帮你消化那漫长尴尬的重试与降级期间错误。你的业务代码对此完美无感知。

## 2. 工具注册与执行沙盒 (Tools & Sandbox)

如何让语言模型知道我们可以查询天气或者提交 SQL 入库？

### 工具定义的契约

你需要实现 `interfaces.Tool`。你的业务服务里面有多少外部依赖的接口，你都可以包裹成工具提供给大模型：

```go
type WeatherTool struct {}

func (t *WeatherTool) Name() string { return "get_weather" }

func (t *WeatherTool) Description() string { 
    return "获取某个城市的当前天气情况。参数需要是城市名称" 
}

func (t *WeatherTool) Schema() string {
    return `{"type": "object", "properties": {"city": {"type": "string"}}}`
}

func (t *WeatherTool) Execute(ctx context.Context, args []byte) (string, error) {
    // 放入你的任何原生 Go 闭包或者 API HTTP 请求！
    return "杭州今天晴，气温 25°C", nil
}
```

### 注入工具库并分配给大模型

FluxGraph 拥有全局的 `tools.Registry` 注册器：

```go
repo := tools.NewRegistry()
repo.Register(&WeatherTool{})
repo.Register(&SearchMemoryTool{}) // 内置在记忆存储层的向量化工具
```

然后，在你的某个负责和模型沟通的 `Node` 中：

```go
// 这句话就像施了魔法，大模型收到全量天气和参数并主动停摆要求你执行
state, toolCallRecords, _ := openaiProvider.Generate(ctx, state.Messages(), repo.GetAllTools())
```

### 沙盒安全网 (`tools/sandbox`)

执行并不只意味着通过。如果模型产生了幻觉执行了你的数据库 `DELETE` 命令呢？如果在服务器内部运行恶意 Python 呢？
这也是 FluxGraph 优异于其他框架的地方，所有的 Tool 都可以交由安全层进行沙盒级别的限制调用与隔离，保障生产服务进程不会陷入内核危机。

> [!CAUTION]
> 工具调用非常强大。但在没有人类授权通过的情况下赋予模型 `DeleteDB` 等级别的能力非常危险。后续我们在第五章中会展示如何设置安全栏（**Guard & Sanitizer**）来屏蔽恶意请求。

---

现在，你已经彻底搞懂了这个“操作系统”的脑力（大图引擎与模型）以及四肢（Tools 沙盒）。

我们要准备进入深水区了：图框架的绝对进阶——第三阶段 [高级用法：持久化、重试与图流式执行](06_advanced_execution.md)。
