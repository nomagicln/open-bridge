# CLI 参考

本文跟随 `main`，明确标注 **Unreleased（未发布）** 行为。`@latest` 当前安装 **v0.1.3**；升级已有集成前请阅读[升级指南](/zh/guide/upgrading)。

## ob 命令

| 命令 | 描述 |
| --- | --- |
| `ob install <name> --spec <path-or-url>` | 将 API 安装为 CLI 应用 |
| `ob uninstall <name> [--remove-shim=false]` | 移除 app 配置，默认同时移除 shim |
| `ob list [--details]` | 列出已安装应用，`--details` / `-d` 显示详细信息 |
| `ob info <name> [-o text\|json\|yaml]` | 查看 app 配置，默认 `text` |
| `ob run <name> [args...]` | 运行已安装应用 |
| `ob <name> [args...]` | 直接运行已安装应用 |
| `ob completion [bash\|zsh\|fish]` | 生成 shell 补全 |
| `ob --version` | 显示构建版本 |
| `ob --help` | 显示管理命令帮助 |

`ob info -o yaml/json` 用于查看配置，会遮蔽部分敏感 header。它不是已删除的 profile 导出格式，也不是凭据备份；分享前应检查输出。

### 安装选项

交互式安装**默认关闭**。必须提供 `--spec`，或显式启用向导：

```bash
ob install myapi --spec ./openapi.yaml
ob install myapi --spec https://api.example.com/openapi.json --base-url https://api.example.com
ob install myapi -i
```

| 参数 | 默认值 | 含义 |
| --- | --- | --- |
| `--spec`、`-s` | 空 | 本地 OpenAPI 路径或远端 URL；非交互模式必填 |
| `--base-url` | 从规范推断 | 覆盖 API 请求 base URL |
| `--description` | 空 | 应用描述 |
| `--auth` | 空 | 认证类型：`none`、`bearer`、`api_key`、`basic` |
| `--shim` | `true` | 创建命令快捷入口；`--shim=false` 禁用 |
| `--force`、`-f` | `false` | 覆盖现有 app 配置 |
| `--interactive`、`-i` | `false` | 启动交互式安装向导 |

`--auth` 选择认证策略，不接受 secret。凭据通过向导/凭据后端配置。将安装输出的 shim 所在目录加入 `PATH`，或直接使用两种 `ob` 入口。仅在确实需要覆盖配置时使用 `--force`。

## App 命令

语法为 `<app> <resource> <verb> [flags]`。资源与参数名来自已安装规范；下例假设规范提供相应的 user 操作。

```bash
ob run myapi --help
ob myapi users --help
ob myapi users list
ob myapi users get --user_id 123
# shim 已安装且位于 PATH 时：
myapi users list
```

URL 子资源会扁平化为复合资源名。仓库 Todo 示例的当前映射如下：

| API 操作 | App 命令 |
| --- | --- |
| `POST /todos` | `todo todos create --title "Task"` |
| `POST /todos/batch` | `todo todos-batch batch --items '[{"title":"Task"}]'` |
| `DELETE /todos/batch` | `todo todos-batch delete --ids '[1,2]'` |
| `GET /todos/stats` | `todo todos-stats get` |

用 app help 确认名称。Todo 示例由源码推导，本轮实际 smoke test 状态见[架构审计](/maintenance/architecture-audit)。

### 运行时选项

| App 参数 | 含义 |
| --- | --- |
| `--profile`、`-p` | 选择已配置的 profile |
| `--output`、`-o` | API 响应格式：`json` 或 `yaml`，默认 `yaml` |
| `--json`、`--yaml` | 响应格式快捷选项 |
| `--generate`、`-g` | 生成请求片段，不发送 API 请求 |
| `--generate-output`、`-O` | 将生成代码写入文件，默认 stdout |
| `--mcp` | 以 MCP 服务模式启动 app |
| `--transport` | MCP 传输：`stdio`（默认）或 `sse` |
| `--port` | SSE 端口，默认 `8080` |

带值选项支持 `--name value` 和 `--name=value`；profile/output/生成选项的短参数也支持所列的独立值形式。运行时参数应位于首个 `--` 之前。

**Unreleased** 的直接入口、`run` 和 shim 共用运行路径，并遵循：

- 根级显式 `--profile` / `--output` / `--mcp` 为 app 提供默认值，应用级显式选项优先。
- 同一作用域的重复值取最后一次。在应用输出选择中，显式 `--output` 无论顺序均优先于 `--json`/`--yaml`；没有 `--output` 时最后一个快捷选项生效。
- 首个 `--` 结束运行时解析，后续为请求参数，不会启动 MCP 或选择运行时 profile/output。
- 必需的选项值缺失时返回错误。

例如，以下 Unreleased 调用选择 `prod` 与 YAML：

```bash
ob --profile dev --output json run myapi users list --profile prod --output yaml
```

### 请求参数

无分隔符时，按操作 schema 自动分类请求参数：

```bash
ob myapi users get --user_id 123
```

`--` 之后使用显式位置前缀（`--path:`、`--query:`、`--header:`、`--body:`）：

```bash
ob myapi users get --output json -- --path:user_id=123 --query:expand=details
```

Unreleased 有效参数处理包含路径级继承参数，并按 `(in, name)` 由操作级覆盖。Raw JSON 按实际请求体校验，错误在发送 API 请求前返回。兼容性影响见[升级指南](/zh/guide/upgrading#unreleased-运行时行为变化)。

## 输出与代码生成

API 响应默认 YAML，可选 `--json`、`--yaml`、`--output json|yaml`；当前没有 API 响应表格格式。`ob list`、`ob info` 等管理命令有各自的显示格式。

```bash
ob myapi users list --json
ob myapi users list --output yaml
ob myapi users list --generate curl
ob myapi users list --generate nodejs --generate-output request.js
ob myapi users list -g go -O request.go
ob myapi users list --generate python
```

支持 **`curl`**、**`nodejs`**、**`go`**、**`python`** 四种生成格式，不执行 API 请求。设置 profile 的 `protect_sensitive_info: true` 可将生成代码中的凭据替换为占位符，默认并未启用保护。提交或分享前检查生成结果。

## MCP 传输与安全

```bash
ob run myapi --mcp --transport stdio --profile prod
ob myapi --mcp --transport sse --port 8080
# shim 形式：
myapi --mcp
```

标准模式暴露 API 工具；启用 `safety.progressive_disclosure: true` 后改为暴露三个元工具。其名称按应用的 PascalCase 名称加前缀，即 `Search<App>Tools`、`Load<App>Tool`、`Invoke<App>Tool`；例如应用 `petstore` 对应 `SearchPetstoreTools`、`LoadPetstoreTool`、`InvokePetstoreTool`。确切名称请从 MCP `tools/list` 响应读取。使用 `MethodIs("GET") && PathContains("/users")` 等 predicate 查询时，设置 `safety.search_engine: predicate`。

profile 安全配置示例：

```yaml
profiles:
  default:
    safety:
      read_only_mode: true
      allowed_operations: [listPets]
      denied_operations: [deletePet]
      require_confirm: [POST, PUT, DELETE]
      max_requests_per_minute: 30
      dangerous_operation_patterns:
        - "POST /admin"
        - "DELETE /pets"
      progressive_disclosure: true
      search_engine: predicate
```

- 只读模式仅暴露 GET；允许/拒绝列表使用 operation ID。
- 需要确认的调用必须包含 `_confirm: true`，确认不会被记忆。
- 按进程限流；危险操作正则匹配 `METHOD path` 与 tool ID，即使已确认仍拒绝匹配项。
- Unreleased 目录先过滤，再对剩余同名工具以 method/path/operationId 诊断报错。无歧义名称保持不变。使用现有规则支持的简洁、唯一 `operationId` 并重新发现工具；没有新的命名扩展。
- Unreleased MCP 校验参数，应用与 CLI 相同的 profile header/query 覆盖，将取消传递到 HTTP/重试等待；SSE 随服务 context 关闭。

## 配置与缓存

`OPENBRIDGE_CONFIG_DIR` 覆盖完整配置目录，否则使用：

| 平台 | 目录 |
| --- | --- |
| macOS | `~/Library/Application Support/openbridge/` |
| Linux/Unix | `$XDG_CONFIG_HOME/openbridge/`，未设置时 `~/.config/openbridge/` |
| Windows | `%APPDATA%\openbridge\`，未设置时回退到用户的 `AppData\Roaming` |

app 配置保存在 `apps/<app>.yaml`。profile 可控制 HTTP 行为：

```yaml
profiles:
  prod:
    name: prod
    base_url: https://api.example.com
    headers:
      X-Environment: prod
    query_params:
      tenant: example
    timeout: 30s
    tls:
      ca_file: /etc/ssl/private-ca.pem
      cert_file: /etc/ssl/client.pem
      key_file: /etc/ssl/client-key.pem
      server_name: api.example.com
      min_version: "1.2"
      insecure_skip_verify: false
    retry:
      max_retries: 3
      initial_delay: 200ms
      max_delay: 2s
      retryable_status_codes: [429, 503]
    protect_sensitive_info: true
```

Timeout 默认 `30s`。仅重试受支持的幂等方法（`GET`、`HEAD`、`PUT`、`DELETE`、`OPTIONS`），请求体会重放。注入凭据后，profile header/query 覆盖同名请求键，其余值保留；Unreleased MCP 使用相同顺序。不要将凭据直接写入配置 header/query。

`spec-cache` 存远端规范与重新验证元数据，删除也会失去离线回退，直至重新获取。`index-cache` 存版本化派生数据，会自动重建，包括 Unreleased schema **4 → 5** 变更。App YAML 与凭据不是缓存，应保留。有效性键与回退见[架构参考](/zh/reference/architecture#缓存与版本)。

## OpenAPI 扩展

现有 CLI 映射扩展可自定义资源与动词：

```yaml
paths:
  /server/reboot:
    post:
      x-cli-verb: trigger
      x-cli-resource: server
```

这些是 CLI 映射选项，不构成新的 MCP 工具命名契约。修改后检查已安装规范的帮助与工具发现结果。
