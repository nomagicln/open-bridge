# 介绍

**OpenBridge** 是一个通用的 API 运行时引擎，它在 OpenAPI 规范和两个截然不同的接口之间架起了一座桥梁：

1. **面向人类**：一个具有资源导向命令的语义化 CLI
2. **面向 AI 智能体**：一个 MCP (Model Context Protocol) 服务器

> "One Spec, Dual Interface." (一份规范，双重接口)

## 为什么选择 OpenBridge?

传统的 API 工具通常需要生成代码或手动编写 CLI 包装器。OpenBridge 采用不同的方法，通过在运行时解释您的 OpenAPI 规范来实现：

- **无需代码生成**：当 API 规范变更时，您无需重新构建 CLI。
- **统一接口**：一个工具同时提供人类友好的 CLI 和机器友好的 MCP 服务器。
- **上下文感知**：专为 AI 智能体时代设计，以 LLM 能够理解和有效使用的方式提供工具。

## 核心特性

- **零代码生成**：所有行为均由 OpenAPI 规范动态驱动
- **语义化 CLI**：资源导向型命令（`myapp user create --name "John"`）
- **Shell 自动补全**：支持命令、资源、参数和枚举值的 Tab 补全
- **MCP 服务器**：通过 Model Context Protocol 原生集成 AI 智能体
- **跨平台**：支持 Linux, macOS 和 Windows
- **安全凭据**：使用系统密钥环（Keychain, Credential Manager, Secret Service）
- **快速启动**：冷启动时间通常低于 100ms
