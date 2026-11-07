# CLI 参考

## ob 命令

用于管理 OpenBridge 应用程序的全局命令。

| 命令 | 描述 |
|---------|-------------|
| `ob install <name> --spec <path>` | 将 API 安装为 CLI 应用程序 |
| `ob uninstall <name>` | 移除已安装的应用程序 |
| `ob list` | 列出所有已安装的应用程序 |
| `ob run <name> [args...]` | 运行已安装应用程序的命令 |
| `ob completion [bash\|zsh\|fish]` | 生成 Shell 自动补全脚本 |
| `ob --version` | 显示版本信息 |
| `ob help` | 显示帮助 |

## App 命令

已安装应用程序可用的命令。

| 模式 | 示例 |
|---------|---------|
| `<app> <resource> list` | `myapi users list` |
| `<app> <resource> get --id <id>` | `myapi user get --id 123` |
| `<app> <resource> create [flags]` | `myapi user create --name "John"` |
| `<app> <resource> update [flags]` | `myapi user update --id 123 --name "Jane"` |
| `<app> <resource> delete --id <id>` | `myapi user delete --id 123` |

## 输出格式

使用参数控制输出格式：

```bash
# YAML 输出（默认）
myapi users list

# JSON 输出
myapi users list --json

# 显式指定格式
myapi users list --output json
myapi users list --yaml
```

## 缓存

OpenBridge 会在配置目录下保存派生数据：

* `<config-dir>/spec-cache` — 远端 OpenAPI 规范缓存；过期时用 `ETag` / `Last-Modified` 条件重新验证，
  远端不可达时作为离线回退。
* `<config-dir>/index-cache` — 帮助/补全派生索引。本地 spec 以绝对路径 + 大小 + 修改时间作为键，
  远端 spec 以 URL + 内容哈希作为键；索引带版本戳，源或语义规则变更时自动重建。

删除任一目录都是安全的，需要时会自动重建。

## Profile 配置

`<config-dir>/apps/<app>.yaml` 中的每个 profile 可以调整 CLI 的 HTTP 行为：

```yaml
profiles:
  prod:
    base_url: https://api.example.com
    timeout: 30s
    tls:
      ca_file: /etc/ssl/private-ca.pem   # 自定义 CA
      cert_file: /etc/ssl/client.pem     # 可选 mTLS 客户端证书
      key_file: /etc/ssl/client-key.pem
      server_name: api.example.com       # 可选 SNI 覆盖
      min_version: "1.2"                 # "1.2" 或 "1.3"
      insecure_skip_verify: false
    retry:
      max_retries: 3
      initial_delay: 200ms
      max_delay: 2s
      retryable_status_codes: [429, 503]
```

* `timeout` 默认为 `30s`；`tls` 与 `retry` 设置会应用到 CLI 请求。
* 仅对幂等方法（`GET`、`HEAD`、`PUT`、`DELETE`、`OPTIONS`）重试，请求体会自动重放。

## MCP 安全控制

调用 MCP 工具时会执行 `safety` 配置：

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
```

* `require_confirm` 中的方法在调用方传入 `_confirm: true` 之前会被拒绝；确认不会被记忆。
* `max_requests_per_minute` 按进程限流；超限调用会返回剩余等待时间。
* `dangerous_operation_patterns` 是正则，匹配 `METHOD path` 与 tool ID；即使带确认也会被拒绝。
* `read_only_mode`、`allowed_operations`、`denied_operations` 控制工具是否暴露。

## OpenAPI 扩展

在 OpenAPI 规范中使用 `x-cli-*` 扩展来自定义 CLI 行为：

```yaml
paths:
  /server/reboot:
    post:
      x-cli-verb: trigger      # 覆盖默认动词映射
      x-cli-resource: server   # 覆盖资源名称
```
