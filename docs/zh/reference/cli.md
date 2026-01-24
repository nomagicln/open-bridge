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
| `ob version` | 显示版本信息 |
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
# 表格输出 (默认)
myapi users list

# JSON 输出
myapi users list --json

# YAML 输出
myapi users list --yaml
```

## OpenAPI 扩展

在 OpenAPI 规范中使用 `x-cli-*` 扩展来自定义 CLI 行为：

```yaml
paths:
  /server/reboot:
    post:
      x-cli-verb: trigger      # 覆盖默认动词映射
      x-cli-resource: server   # 覆盖资源名称
```
