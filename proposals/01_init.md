# 产品需求文档 (PRD): OpenBridge - 通用 API 运行时引擎

| 文档版本 | V1.0 |
| --- | --- |
| **状态** | 草稿 (Draft) |
| **作者** | Gemini (Representative) |
| **日期** | 2026-01-19 |
| **核心概念** | OpenAPI Runtime, Kubectl-style CLI, MCP Server |

---

## 1. 产品概述 (Executive Summary)

**OpenBridge** 是一个基于 OpenAPI 规范的通用运行时引擎。它旨在解决 API 工具链割裂的问题，通过读取标准的 OpenAPI 定义文件，无需编写额外代码，即可动态生成两套交互界面：

1. **面向人类：** 具备 `kubectl` 风格、语义化操作、支持独立命令入口的命令行工具（CLI）。
2. **面向 AI：** 符合 Model Context Protocol (MCP) 标准的服务端，使 AI Agent 能直接调用 API 作为工具。

**产品愿景：** "One Spec, Dual Interface."（一份定义，人机两用。）

---

## 2. 问题陈述与背景 (Problem Statement)

当前 API 使用端存在显著痛点：

1. **CLI 体验差：** `curl` 过于底层，参数构造繁琐；生成的 SDK CLI 往往生硬（仅是 API 的 1:1 映射），缺乏业务语义（如 `kubectl` 般的流畅感）。
2. **AI 接入成本高：** 为了让 AI (如 ChatGPT/Claude) 调用内部 API，开发者需要专门编写 Plugin 代码或 Function Calling 适配层，维护成本高。
3. **入口不统一：** 不同的微服务通常需要下载不同的 CLI 工具，或者使用复杂的通用工具（如 Postman），缺乏统一且轻量的命令行入口。

---

## 3. 产品目标 (Goals)

1. **零代码生成 (Zero Code-Gen)：** 核心引擎为通用二进制，所有行为由 OpenAPI Spec + 配置文件 动态决定。
2. **语义化 CLI 体验：** 自动或通过简单配置，将 HTTP 动作映射为 `create`, `get`, `delete`, `apply` 等语义化动词。
3. **原生 AI 支持：** 内置 MCP Server，一键让 API 变为 AI 可用的 Skill。
4. **应用化分发：** 支持将配置注册为系统级独立命令（如 `myapp`），而非 `bridge --app myapp`。

---

## 4. 核心功能需求 (Functional Requirements)

### 4.1. 模块一：应用配置与独立入口 (The App Shim)

**需求描述：**
用户可以通过配置文件定义一个 "App"，系统应在 OS 层面提供该 App 的快捷入口。

**功能细则：**

* **配置加载：** 支持加载本地 `openapi.yaml` 或远程 URL。
* **命令注册 (Alias/Shim)：**
* 提供 `bridge install <app-name> --spec <path>` 命令。
* 在用户的 `$PATH` 中创建名为 `<app-name>` 的轻量级 Shim（垫片脚本或软链）。
* **效果：** 用户在终端直接输入 `myapp` 即可调用，而非 `bridge run myapp`。


* **多环境支持：** 每个 App 可配置多个 Profile（如 `dev`, `prod`），通过 `myapp --profile prod` 切换。

### 4.2. 模块二：语义化命令行引擎 (Semantic CLI Engine)

**需求描述：**
将 OpenAPI 的 RESTful 结构转换为“资源-动词”结构。

**映射逻辑 (Smart Mapping)：**

1. **资源识别：** 从 URL Path 中提取最后一段名词作为资源名（如 `/api/v1/users` -> `users`）。
2. **动词推断 (Heuristic Strategy)：**
* `POST` -> 默认映射为 `create`。
* `GET (list)` -> 默认映射为 `list` 或 `get`。
* `GET (item)` -> 默认映射为 `describe` 或 `get`。
* `PUT/PATCH` -> 默认映射为 `apply` 或 `update`。
* `DELETE` -> 默认映射为 `delete`。


3. **人工干预 (Overrides)：**
* 支持在 OpenAPI 文件中使用扩展字段 `x-cli-verb` 和 `x-cli-resource` 强制覆盖默认逻辑。
* *示例：* `POST /server/reboot` 默认可能是 `create reboot`（不通顺），通过配置改为 `trigger reboot` 或 `reboot server`。



**交互体验：**

* **Tab 自动补全：** 必须支持资源名、参数名的 Shell 补全。
* **输出格式化：** 默认输出表格 (Table) 视图，支持 `--json` 和 `--yaml` 原文输出。

### 4.3. 模块三：AI 接入网关 (MCP Server)

**需求描述：**
OpenBridge 需具备 "Serve Mode"，作为 AI 模型与真实 API 之间的代理。

**功能细则：**

* **MCP 协议实现：** 实现 MCP 的 `list_tools` 和 `call_tool` 接口。
* **工具转换：**
* 将 OpenAPI 的 `summary` 转换为 Tool `description`。
* 将 `schema` 转换为 Tool `input_schema`。


* **安全沙箱：**
* 支持配置“AI 只读模式”（仅允许 GET 请求）。
* 支持通过 Prompts 确认危险操作（如 Delete）。


* **启动方式：** `myapp --mcp` 即可启动该 App 对应的 MCP 服务，供 Claude Desktop 或其他 Agent 连接。

---

## 5. 用户故事 (User Stories)

| 角色 | 场景 | 期望行为 |
| --- | --- | --- |
| **SRE 运维** | 需要紧急重启线上一台服务。 | 不想翻文档找 Curl 参数。直接输入 `ops-cli server reboot --id 101`，系统提示确认后执行成功。 |
| **后端开发** | 刚写完新接口 `POST /invoices`，需要测试。 | 不想打开 Postman。直接输入 `myapp invoices create --amount 100`，立即看到表格化的返回结果。 |
| **AI Agent** | 用户问 Claude：“帮我查下 id 为 5 的用户最近的订单”。 | Claude 发现连接了 `myapp` MCP 服务，自动查找工具 `list_orders`，传入 `user_id=5`，获得数据并总结给用户。 |

---

## 6. 非功能需求 (Non-Functional Requirements)

1. **性能：** CLI 启动时间（冷启动）需小于 100ms。建议使用 Golang 或 Rust 开发。
2. **兼容性：** 必须完全兼容 OpenAPI 3.0 及 3.1 标准。
3. **单二进制文件：** 分发时应只有一个核心 Binary，方便安装。
4. **安全性：** Token/API Key 必须存储在系统级安全区域（如 macOS Keyring, Windows Credential Manager），严禁明文存储在配置文件中。

---

## 7. 架构设计草图 (Architecture Draft)

```text
[ User Terminal ]      [ AI Model / Claude ]
       |                        |
       v (Command)              v (JSON-RPC / MCP)
+--------------------------------------------------+
|               OpenBridge Core Binary             |
+--------------------------------------------------+
|  [ Semantic Parser ] <--> [ MCP Protocol Handler]|
|           ^                         ^            |
|           | (Load & Parse)          |            |
|           v                         v            |
|      [ OpenAPI Spec ]       [ Config / Auth ]    |
+--------------------------------------------------+
            | (Translate to HTTP)
            v
     [ Real Backend API ]

```

---

## 8. 实施路线图 (Roadmap)

* **Phase 1: CLI 原型 (MVP)**
* 实现 OpenAPI 解析。
* 实现基础的 HTTP -> `kubectl` 动词映射。
* 支持 `bridge run --spec ...` 运行。


* **Phase 2: 独立入口与交互增强**
* 实现 `install` 命令生成 Shim。
* 实现 Tab 自动补全。
* 接入系统 Keyring 管理 Token。


* **Phase 3: AI 能力 (MCP)**
* 集成 MCP Server 协议。
* 支持 `myapp --mcp` 模式。


* **Phase 4: 生态扩展**
* 支持 TUI (Terminal UI) 交互模式（类似 `k9s`）。
* 支持插件系统。



---

## 9. 成功指标 (Success Metrics)

* **配置耗时：** 用户从拿到 `openapi.yaml` 到能够执行第一条命令的时间 < 3分钟。
* **映射准确率：** 默认启发式算法对标准 REST API 的动词映射准确率 > 90%。
* **AI 调用成功率：** AI 通过 MCP 理解并正确调用 API 参数的成功率 > 95%。

---
