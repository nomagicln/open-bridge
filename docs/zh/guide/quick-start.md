# 快速开始

本文跟随 `main`（**Unreleased，未发布**）。最新已发布标签为 **v0.1.3**，`@latest` 当前安装该版本。已有安装请先阅读[升级指南](/zh/guide/upgrading)。本地 Todo 服务教程见仓库 [QUICKSTART.md](https://github.com/nomagicln/open-bridge/blob/main/QUICKSTART.md)。

## 1. 安装 API

提供 app 名称及本地路径或远端 URL：

```bash
ob install myapi --spec ./openapi.yaml
# 需要时覆盖服务器地址：
ob install myapi --spec ./openapi.yaml --base-url http://localhost:8080
```

这是两种安装方式，任选其一。替换已有配置需要 `--force`，仅在确实需要覆盖时使用。

交互式安装**默认关闭**。不使用 `-i` 时必须提供 `--spec`。如需向导，执行：

```bash
ob install myapi -i
```

向导可配置规范源、base URL、认证、TLS、MCP 安全/渐进披露和生成片段的敏感信息保护。凭据存放于凭据后端；适配已有 profile 时保留 TLS 与安全控制。完整[安装参数](/zh/reference/cli#安装选项)包括 `--auth`、`--description`、`--shim`。

## 2. 探索与运行命令

用 app help 发现规范中的资源、动词和参数名：

```bash
ob run myapi --help
ob myapi --help
```

若规范提供 user 操作，命令可能如下：

```bash
ob myapi users list
ob run myapi users create --name "John"
```

如果安装时创建 shim，将输出的 shim 目录加入 `PATH` 后即可运行 `myapi users list`。嵌套 URL 会成为扁平资源；仓库 Todo 示例的 `/todos/batch` 对应 `todos-batch`，`/todos/stats` 对应 `todos-stats`。

选择 profile/输出格式，或生成片段而不发送 API 请求：

```bash
ob myapi users list --profile prod --output json
ob myapi users list --generate curl
ob myapi users list --generate python -O request.py
```

运行时选项必须位于首个 `--` 之前。之后的请求参数使用 `--path:user_id=123`、`--query:page=1` 等显式前缀。优先级和受支持的生成格式见 [CLI 参考](/zh/reference/cli#运行时选项)。

## 3. 连接 MCP 客户端

以 stdio 启动所选 app/profile：

```bash
ob run myapi --mcp --transport stdio --profile prod
```

支持 `mcpServers` 配置的客户端可使用二进制绝对路径：

```json
{
  "mcpServers": {
    "myapi": {
      "command": "/absolute/path/to/ob",
      "args": ["run", "myapi", "--mcp", "--transport", "stdio", "--profile", "prod"]
    }
  }
}
```

profile 必须已存在。Shim 也可通过 `myapi --mcp` 启动。SSE 使用 `ob myapi --mcp --transport sse --port 8080`；默认传输为 stdio。

标准模式直接暴露 API 工具。启用渐进披露时，在所选 profile 中设置：

```yaml
safety:
  progressive_disclosure: true
  search_engine: predicate
```

渐进模式暴露三个元工具，名称为 `Search<App>Tools`、`Load<App>Tool`、`Invoke<App>Tool`，其中 `<App>` 为应用的 PascalCase 名称；例如应用 `petstore` 对应 `SearchPetstoreTools`。确切名称请从 MCP `tools/list` 响应读取。Predicate 引擎支持 `MethodIs("GET") && PathContains("/users")`、`HasTag("admin")` 及逻辑 `&&`、`||`、`!`。只读、允许/拒绝列表、确认、限流、危险操作模式分别生效，参见 [MCP 安全](/zh/reference/cli#mcp-传输与安全)。

## 配置与后续阅读

`OPENBRIDGE_CONFIG_DIR` 覆盖完整配置目录。默认位置：

- macOS：`~/Library/Application Support/openbridge/`
- Linux/Unix：`$XDG_CONFIG_HOME/openbridge/`，未设置时 `~/.config/openbridge/`
- Windows：`%APPDATA%\openbridge\`

用 `ob list --details` 和 `ob info myapi -o yaml` 查看已安装应用。App 文件位于 `apps/<app>.yaml`，凭据使用配置的后端。远端 `spec-cache` 提供重新验证/离线回退，版本化 `index-cache` 自动重建。升级时保留 app 配置与凭据。

继续阅读 [CLI 参考](/zh/reference/cli)、[架构参考](/zh/reference/architecture)与[升级指南](/zh/guide/upgrading)。Unreleased 实现与验证状态见[架构审计](/maintenance/architecture-audit)。
