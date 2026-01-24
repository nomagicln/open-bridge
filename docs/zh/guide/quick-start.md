# 快速开始

在几分钟内启动并运行 OpenBridge。

> **注意**：有关详细的逐步教程，请参阅仓库中的 [QUICKSTART.md](https://github.com/nomagicln/open-bridge/blob/main/QUICKSTART.md)。

## 1. 安装 API

通过提供 API 名称及其 OpenAPI 规范的路径或 URL 来安装 API：

```bash
ob install myapi --spec ./openapi.yaml
```

### 交互式安装向导

当您运行安装命令时，`ob` 将启动一个交互式向导来配置您的应用程序：

1.  **OpenAPI 规范源**：如果未通过参数提供，系统将要求您输入 OpenAPI 规范的 URL 或本地路径。
2.  **Base URL**：向导将尝试从规范中自动检测。您可以覆盖它以指向不同的环境（例如 `http://localhost:8080` vs `https://api.example.com`）。
3.  **身份验证**：选择您的认证策略：
    *   `None`：公开 API。
    *   `Bearer`：标准 Bearer 令牌 (JWT)。
    *   `API Key`：通过 Header 或 Query 参数传递的密钥。
    *   `Basic`：用户名和密码。
4.  **安全**：如果需要，配置 TLS 设置（如客户端证书）。
5.  **MCP 选项**：
    *   **渐进式披露 (Progressive Disclosure)**：为大型 API 启用此功能，以帮助 AI 智能体高效发现工具。
    *   **只读模式**：限制 AI 仅执行安全的 `GET` 操作。
    *   **敏感信息**：在为 AI 上下文生成的代码中隐藏 API 密钥。

## 2. 运行命令

安装完成后，使用 CLI 与 API 资源进行交互：

```bash
# 列出资源
myapi users list

# 创建资源
myapi user create --name "John"
```

## 3. 配合 AI 使用 (MCP 模式)

OpenBridge 实现了 Model Context Protocol (MCP)，作为 API 和 AI 智能体（如 Claude）之间的桥梁。

### 配置示例 (Claude Desktop)

要在 Claude Desktop 中使用已安装的 API，请将以下内容添加到您的配置文件中：

*   **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
*   **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "myapi": {
      "command": "/path/to/ob",
      "args": ["run", "myapi", "--mcp"]
      // 或者如果您创建了 shim：
      // "command": "myapi",
      // "args": ["--mcp"]
    }
  }
}
```

> **注意**：请将 `/path/to/ob` 替换为您的 `ob` 二进制文件的绝对路径（例如 `/usr/local/bin/ob` 或 `/${HOME}/go/bin/ob`）。

### 手动启动服务器

您也可以在终端中手动测试服务器：

```bash
# 启动 MCP 服务器
myapi --mcp
```

### 渐进式披露

为了高效处理大型 API，OpenBridge 使用 **渐进式披露** 策略。它暴露三个元工具，而不是一次性倾倒所有端点：

1. **`SearchTools`**：使用强大的查询语言查找相关工具。
2. **`LoadTool`**：加载特定工具的完整 Schema。
3. **`InvokeTool`**：执行工具。

#### 搜索语法

- **方法**：`MethodIs("GET")`
- **路径**：`PathContains("/users")`
- **标签**：`HasTag("admin")`
- **逻辑运算**：`&&` (与), `||` (或), `!` (非)

## 配置

配置文件存储在以下位置：

- **macOS**: `~/Library/Application Support/openbridge/`
- **Linux**: `~/.config/openbridge/`
- **Windows**: `%APPDATA%\openbridge\`

### 配置文件 (Profiles)

使用 profile 管理不同的环境（dev, prod）：

```bash
myapi users list --profile prod
```
