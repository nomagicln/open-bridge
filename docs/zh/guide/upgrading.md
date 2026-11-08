# 升级 OpenBridge

最新已发布标签为 **v0.1.3**；`go install github.com/nomagicln/open-bridge/cmd/ob@latest` 当前安装该版本。`main` 及该标签之后的提交包含 **Unreleased（未发布）** 变更。本文的运行时修复尚无对应的发布版本，请固定已验证的标签或提交。

升级前阅读目标 ref 的 [CHANGELOG](https://github.com/nomagicln/open-bridge/blob/main/CHANGELOG.md)、[CLI 参考](/zh/reference/cli)和[架构参考](/zh/reference/architecture)。[架构审计](/maintenance/architecture-audit)记录本轮验证状态；Unreleased 条目本身不代表全部检查通过。

## 确认消费方式与版本

记录 `ob --version`、适用时的 Go 模块版本，以及旧、新 ref 解析出的提交。先找出下游项目自己的命令调用、MCP 工具名、YAML 读写和 Go import，再对照上游差异修改。

| 消费方式 | 升级前检查 |
| --- | --- |
| CLI / 子进程 | 入口形式、profile/output 参数、分隔符、代码片段生成、退出行为和已安装规范的帮助 |
| MCP 客户端 | 传输、profile 安全策略、发现的工具名与 schema、确认流程、取消和代表性调用 |
| 配置 | 配置目录、app/profile 名称、凭据后端、自定义 header/query、TLS 和自定义序列化 |
| Go import | 删除的导出符号、构造函数/选项契约，以及下游自身的构建和测试 |

在 OpenBridge checkout 中执行下列只读命令，并替换为实际使用的 ref：

```bash
git rev-parse v0.1.3
git rev-parse 6378155
git diff v0.1.3..6378155 -- pkg/config
```

下表的证据范围严格为 **`git diff v0.1.3..6378155 -- pkg/config`**，其中完整基线为 `6378155ed1e7c67733534c35ee257d961e9446a3`。表格记录该基线已存在的公开 API 删除。还应检查 `6378155..<你的目标 ref>` 的后续变更；此表不承诺未来所有提交的兼容性，也不依赖不稳定的导出数量统计。

## 从 v0.1.3 迁移 Go 源码

`pkg/config` 当前负责配置模型、校验、目录选择和持久化。仓库的安装实现位于 `internal/install`，下游模块受 Go 的 `internal` 导入规则限制，**不能将它用作公共替代 API**。

| v0.1.3 中已删除的 API 或类别 | 当前方式与限制 |
| --- | --- |
| `Manager.InstallApp`、`UpdateApp`、`UninstallApp`；`InstallOptions`、`InstallResult`、`InstalledAppInfo`、`GetInstalledAppInfo` | 无公共 Go 替代。安装自动化可调用 `ob install <app> --spec <source>`，用 `ob info <app> -o yaml` 或 `ob list --details` 检查，用 `ob uninstall <app>` 移除。`--force` 会覆盖现有配置，仅在确实需要覆盖时使用。若依赖进程内 Go 契约，固定 v0.1.3。 |
| `Manager.CreateShim`、`RemoveShim`、`ShimExists`；`IsShimDirInPath`、`GetPathInstructions` | 无公共 Go shim 管理替代。CLI 自动化可用安装的 `--shim` 和卸载的 `--remove-shim`；否则保留旧标签。 |
| `PathValidationError`、`TLSConfigValidation`、`ValidateAndResolvePath`、`ValidateTLSCertFile`、`ValidateTLSKeyFile`、`ValidateTLSCACertFile`、`ValidateTLSConfigPaths`、`IsValidPath` | 无公共校验 helper 替代。CLI 安装负责自身资产校验；下游 Go 代码保留自己的路径/TLS 校验或固定旧版本，不得导入 `internal/install`。 |
| `ProfileManager`、`Manager.NewProfileManager`；profile 创建/读取/更新/删除/默认值/列表方法；`ProfileOptions`、`ProfileInfo`、`ProfileExistsError` | 用 `Manager.GetAppConfig` 加载，根据需求使用 `NewProfile`、`AppConfig.AddProfile`、`GetProfile`、`DeleteProfile`、`SetDefaultProfile`、`GetDefaultProfile`、`ListProfiles`，再 `Manager.SaveAppConfig`。模型方法并不复现旧接口的所有校验规则与错误类型。见下方写回示例。 |
| `ProfileManager.Reload`、`Save`、`RenameProfile`、`CopyProfile`、`ProfileExists`、`ProfileCount`、`SelectProfile`、`ListProfilesWithInfo`；`Manager.SelectProfileForApp`、`GetProfileNames`；`ProfileSummary` | 重新加载/保存用 `Manager.GetAppConfig`/`SaveAppConfig`，名称列表用 `AppConfig.ListProfiles`，存在性用 `GetProfile`，数量用 `len(app.Profiles)`，显式/默认查找用 `GetProfile`/`GetDefaultProfile`。重命名、复制、选择策略、详细摘要及旧错误/默认语义没有等价公共便捷 API；保留下游自己的需求实现或固定旧标签。 |
| `ProfileManager.SetProfileHeader`、`DeleteProfileHeader`、`SetProfileQueryParam`、`ConfigureSafety`、`ConfigureTLS`、`ConfigureRetry` | 修改 `config.Profile` 对应字段，通过 `AppConfig.AddProfile(*profile)` 写回、校验、保存。保留已有 map 项和安全设置。 |
| `Manager.ExportProfile`、`ImportProfile`、`ProfileExport`；`export.go` 的选项/文件/批量接口，包括 `ExportProfileWithOptions`、`ImportProfileWithOptions`、`ValidateImport`、`ExportAllProfiles`、`ExportAllProfilesAsSingle`、`ImportBulkProfiles`、`ExportProfileToFile`、`ImportProfileFromFile`、`ExportFormat`、`ExportOptions`、`ImportOptions`、`ImportValidationResult`、`ProfileExportV2`、`ExportedProfile`、`ExportedAuth`、`ExportMetadata`、`BulkExport`、`DefaultExportOptions` | 无公共替代，也没有 profile 导入/导出 CLI。`ob info -o yaml/json` 用于查看配置，会遮蔽部分敏感 header；它不是历史交换格式，也不是凭据备份。依赖原有封装格式的流程应保留旧标签。 |
| `SpecCacheManager` 及构造/fetch/refresh/metadata/clear/info 方法；`SpecCacheMeta`、`FetchResult`、`CacheInfo` | 旧缓存管理 API 和布局无直接公共替代。加载规范可用 `pkg/spec.NewParser` 配合 `WithRemoteCache`、按需 `WithIndexCache`，再调用 `LoadSpecWithContext` 或 `LoadSpecWithOptions`。这些提供解析器缓存，并不复现按 app 管理的旧契约；需要旧 API 时固定旧标签。 |
| `config.SpecFetchOptions`、`Profile.BuildSpecFetchOptions` | `pkg/spec.SpecFetchOptions` 和 `Parser.LoadSpecWithOptions` 支持认证获取规范。按需求显式构建 header/auth 选项，并从凭据后端取出 secret；没有公共的 profile 到 options 转换 helper 替代。 |
| `SpecWatcher`、`SpecWatcherManager`、`LocalFileWatcher`、`RemoteSpecWatcher`，以及构造/选项/事件类型（`SpecChangeEvent`、`SpecChangeType`、`SpecChangeHandler`、`WatchMode`、`WithPollInterval`、`WithCheckInterval`、`WithRefreshBefore`）和生命周期/check/handler 方法 | 无公共替代。解析器加载时重新验证缓存，不提供后台监视、回调或旧 watcher 生命周期。需要这些契约时固定旧标签。 |

### 修改 profile 后必须写回

`AppConfig.GetProfile` 返回的是 **profile 值副本的指针**。修改 `BaseURL` 等标量不会更新 `app.Profiles` 中的值。map 字段仍引用原来的底层 map；若需要独立副本，应复制这些 map。编辑后始终写回、保存，并重新加载验证。

下面是下游项目可使用的 helper，仅调用现有公共 API：

```go
package integration

import (
    "fmt"

    "github.com/nomagicln/open-bridge/pkg/config"
)

func updateBaseURL(manager *config.Manager, appName, profileName, baseURL string) error {
    app, err := manager.GetAppConfig(appName)
    if err != nil {
        return fmt.Errorf("load app: %w", err)
    }
    profile, ok := app.GetProfile(profileName)
    if !ok {
        return fmt.Errorf("profile %q does not exist", profileName)
    }
    profile.Name = profileName // YAML profiles may omit the embedded name.
    profile.BaseURL = baseURL
    app.AddProfile(*profile)
    if err := config.ValidateConfig(app); err != nil {
        return fmt.Errorf("validate app: %w", err)
    }
    if err := manager.SaveAppConfig(app); err != nil {
        return fmt.Errorf("save app: %w", err)
    }

    saved, err := manager.GetAppConfig(appName)
    if err != nil {
        return fmt.Errorf("reload app: %w", err)
    }
    actual, ok := saved.GetProfile(profileName)
    if !ok || actual.BaseURL != baseURL {
        return fmt.Errorf("saved profile %q did not retain base URL", profileName)
    }
    return nil
}
```

新建 profile 可用 `app.AddProfile(config.NewProfile(name, baseURL))`。`AddProfile` 可以覆盖同名条目，如需仅创建，应先检查是否存在。删除或重命名时，按下游需求显式确定默认 profile，避免依赖 map 遍历顺序。

## 配置与缓存

已有 app YAML 仍可读取。新增的可选 `spec_title`、`spec_version` 用于持久化 API 元数据，无需重写旧文件。`version` 表示 app 配置版本，`spec_version` 表示 API 版本，两者都不是 OpenBridge 发布版本。

App YAML 与历史 profile 导出格式是不同契约。旧 `ProfileExport` 封装含 `version`、`app_name`、`profile_name`、`profile`、`exported_at`；`ProfileExportV2`/`BulkExport` 有自己的版本化封装与导出选项。不要把导出文件改名为 `apps/<app>.yaml`，也不要将 `ob info` 当成兼容的导入工具。

保留配置目录与凭据后端。`OPENBRIDGE_CONFIG_DIR` 覆盖完整配置位置；Linux/Unix 默认遵循 `XDG_CONFIG_HOME`。若升级后找不到 app，先检查新旧进程是否使用相同目录。

Unreleased 的有效参数修复将派生索引 schema 从 **4 提升到 5**；旧索引在加载时自动重建，无需删除 app 配置或凭据。远端 `spec-cache` 可能是规范唯一的离线副本，只有确认能重新获取源时才删除。参见[缓存生命周期](/zh/reference/architecture#缓存与版本)。

## Unreleased 运行时行为变化

以下为本轮已批准的契约，不属于 v0.1.3 行为。实现与测试状态见[架构审计](/maintenance/architecture-audit)。

- **MCP 工具身份**：标准与渐进模式在只读/允许/拒绝过滤后构建统一授权目录。无歧义名称保持不变；允许集合中仍同名的操作会导致构建失败，并报告 method/path/operationId。可为规范设置现有命名规则支持的简洁、唯一 `operationId`，重新发现工具并更新客户端引用。不引入新命名扩展或自动后缀。
- **有效参数**：路径级与操作级参数按 `(in, name)` 合并，同一对由操作级覆盖。CLI help/执行、补全、派生索引和两种 MCP 模式使用相同结果。
- **请求校验与 profile**：按实际发送的 raw JSON body 校验，非法 JSON 或缺少必填数据在 HTTP 请求前失败。MCP 校验参数并复用 CLI 的覆盖顺序：构建请求参数、注入凭据、应用 profile 的 `headers` 和 `query_params`。同名 header/query 由 profile 覆盖，其余值保留。
- **入口与参数**：`ob <app>`、`ob run <app>`、shim 使用统一运行路径。应用级显式选项优先于根级显式默认值；同一作用域的重复值取最后一次。`--output` 仍优先于 `--json`/`--yaml`。首个 `--` 结束运行时选项解析，后续为请求参数；选项缺值时明确报错。
- **取消**：工具调用 context 传递到 HTTP 请求，重试等待可取消，SSE 随服务 context 结束而关闭。现有幂等重试限制和安全策略继续有效。

受支持参数见 [CLI 参考](/zh/reference/cli)。用隔离配置和测试凭据运行下游自身构建/测试及代表性调用。遇到阻塞应报告，不能通过删除配置、放宽安全策略或静默改变消费方式规避。

## 固定版本或回退

安装之前不可变发布的二进制，或将 Go 模块固定到已验证的标签。例如：

```bash
go install github.com/nomagicln/open-bridge/cmd/ob@v0.1.3
# 下游 Go 模块需要 v0.1.3 的 API 契约时：
go get github.com/nomagicln/open-bridge@v0.1.3
```

保留配置与凭据，再运行该下游项目自己的检查。发布标签保持不可变；回退不删除或移动标签。[README](https://github.com/nomagicln/open-bridge/blob/main/README.md#upgrading-with-an-ai-agent) 提供可复制的下游 Agent 提示词。
