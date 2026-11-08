# 架构参考

OpenBridge 在运行时解释 OpenAPI 规范，提供以资源为中心的 CLI 和 MCP 工具。两种接口共用配置模型、凭据、请求构造和 HTTP 策略。可选的 `--generate` 请求片段生成功能不改变这一运行时模型。

本文跟随 `main`，包含下述 **Unreleased（未发布）** 契约。最新已发布标签为 **v0.1.3**。版本边界见[升级指南](/zh/guide/upgrading)，本轮证据、待办与实际验证结果见[架构审计](/maintenance/architecture-audit)。

## 阅读顺序与权威来源

修改仓库时，依次阅读：

1. [AGENTS.md](https://github.com/nomagicln/open-bridge/blob/main/AGENTS.md) 与 [CONTRIBUTING.md](https://github.com/nomagicln/open-bridge/blob/main/CONTRIBUTING.md)，遵守开发规则；`.golangci.yml` 是 lint 的权威来源。
2. [README](https://github.com/nomagicln/open-bridge/blob/main/README.md)、本文及 [CLI 参考](/zh/reference/cli)，了解当前边界与命令。
3. [升级指南](/zh/guide/upgrading)与 [CHANGELOG](https://github.com/nomagicln/open-bridge/blob/main/CHANGELOG.md)，检查消费方兼容性。
4. **所选 ref** 的源码和测试，确认实际行为；审计记录本轮哪些契约已验证。

已安装的 OpenAPI 文档定义操作、参数位置与 schema；profile 定义环境与安全策略。派生索引只是可重建的规范投影，不是另一份 API 定义。

历史材料各有用途：

| 材料 | 使用方式 |
| --- | --- |
| [原始 PRD](https://github.com/nomagicln/open-bridge/blob/main/proposals/01_init.md) 与 [.kiro 规格](https://github.com/nomagicln/open-bridge/tree/main/.kiro/specs/openbridge) | 产品意图、最初需求与任务。PRD 不是依赖注入 ADR，也不能证明所有目标均已实现。 |
| [持久化索引提案](https://github.com/nomagicln/open-bridge/blob/main/proposals/02_persistent_cache.md) | 解释缓存派生 DTO、保留保守回退的理由。快照耗时及 `/tmp` 脚本是历史证据，不是当前可运行的验收命令。 |
| [配置拆分提案](https://github.com/nomagicln/open-bridge/blob/main/proposals/03_config_split.md) | 设计理由与历史迁移计划。具体移动/删除情况以当前源码和升级表为准。 |
| [工程诊断](https://github.com/nomagicln/open-bridge/blob/main/ENGINEERING-DIAGNOSIS.md)、`FIX*` 报告、`.diagnosis/` | 各自基线的调查结论；不覆盖当前审计，也不意味着旧缺陷仍然存在。 |

原始表格输出、profile 导入/导出目标不会自动成为当前功能。API 响应支持 YAML 和 JSON；管理命令 `ob list` 有独立表格显示。历史 profile 导入/导出 API 当前没有等价公共替代。

## 模块与依赖方向

| 模块 | 职责与边界 |
| --- | --- |
| `cmd/ob` | 进程入口和构建元数据，委托装配与执行。 |
| `internal/app` | 显式组合根（`New`、`Deps.Run`）及 Cobra 管理命令。构造 config、带缓存的 parser、凭据、mapper、request builder、handler、router、installer 和补全组件。 |
| `internal/install` | 仓库内部的安装用例、shim/PATH 与 TLS 资产校验。下游 Go 模块不能导入。 |
| `pkg/config` | 公共 app/profile/auth/TLS/safety/retry 模型、校验、目录解析及 YAML 持久化。不负责解析规范或安装。 |
| `pkg/spec` | OpenAPI 加载/归一化、远端内容缓存、内存解析缓存和派生索引。组合根传入缓存目录/选项，无需依赖 `pkg/config` 选择路径。 |
| `pkg/semantic` | 操作到资源/动词的映射，含扩展覆盖和确定性冲突处理。URL 子资源扁平化为 `todos-batch` 等名称。 |
| `pkg/router` | 选择 app、CLI/MCP 模式以及运行时 profile/选项。 |
| `pkg/cli`、`pkg/completion` | app 执行/帮助/输出及 shell 补全；在受支持时复用 spec/semantic 投影。 |
| `pkg/mcp` | 授权工具目录、schema/分派、标准/渐进暴露、搜索、确认和限流/安全检查。 |
| `pkg/request`、`pkg/credential` | 共用 HTTP 构造/校验/认证/客户端策略，以及凭据后端。profile 保存认证配置，secret 放在凭据后端。 |
| `pkg/codegen`、`pkg/tui` | 可选请求代码片段与交互式安装 UI。位于 `pkg` 并不使 `internal/install` 成为下游安装 API；TUI 与安装 DTO 的耦合仍是需关注的边界。 |
| `internal/integration`、`internal/proptest` | 跨包集成与属性测试支持。生产执行不能依赖 testing、断言或属性测试包。 |

`pkg` 可导入，但 1.0 之前仍应逐 ref 检查源码兼容性。`internal` 是仓库私有实现。安装行为不进入配置模型，CLI/MCP 传输不进入请求/规范模型；保持显式构造装配，避免引入包级服务单例。

## 请求与命令数据流

管理命令（`install`、`uninstall`、`list`、`info`、`completion`）由 `internal/app` 编排。app 请求路径如下：

```text
cmd/ob -> internal/app -> router -> CLI handler / MCP handler
                                      |
                           config/profile + spec/semantic
                                      |
                           request builder + credentials
                                      |
                          profile overrides + HTTP client
```

Unreleased 入口契约统一 `ob <app>`、`ob run <app>`、shim。根级显式选项提供默认值，应用级显式选项优先；首个 `--` 结束运行时选项，同一作用域重复值取最后一次，`--output` 仍优先于 `--json`/`--yaml`。见 [CLI 参考](/zh/reference/cli#运行时选项)。

有效参数按 `(in, name)` 合并 `PathItem.Parameters` 与 `Operation.Parameters`，操作级声明覆盖同一对。CLI help/执行、补全、派生索引和两种 MCP 模式共用合并结果，且不修改共享规范。

校验作用于实际请求体表示，包括 raw JSON。CLI/MCP 先构建请求参数、注入凭据，再应用 profile 的 `headers`/`query_params`；profile 覆盖同名键，其余值保留。HTTP 客户端应用 timeout、TLS、retry；重试仍限制于支持的幂等方法。调用 context 传递到 HTTP 和重试等待，SSE 随服务取消而关闭。这些修复均为 Unreleased，验证状态归审计记录。

## MCP 目录与安全

标准模式直接暴露 API 工具。profile 设置 `safety.progressive_disclosure: true` 后暴露三个元工具 `Search<App>Tools`、`Load<App>Tool`、`Invoke<App>Tool`，其中 `<App>` 为应用的 PascalCase 名称；搜索引擎选择和现有 predicate DSL 与工具身份是不同关注点。

Unreleased 契约在只读/允许/拒绝过滤**之后**构建统一目录。同一发布名称的 schema、元数据和 method/path 绑定到同一个允许操作。调用解析该绑定，不再另行扫描全部规范。若允许集合中的两个操作仍同名，则构建失败，报告 method/path/operationId。失败重建不能发布部分目录或保留过期的加载/搜索结果。

现有无歧义工具名保持不变。解决冲突可使用现有规则支持的简洁、唯一 `operationId`，然后重新发现工具；不引入新命名扩展。调用仍执行确认、限流、危险模式和方法安全检查；命名或传输变化不能绕过这些控制。

## 缓存与版本

应用组合根在配置目录下启用 `spec-cache` 与 `index-cache`。下游直接使用 `spec.Parser` 时自行传入缓存选项和目录。

| 层 | 生命周期与有效性 |
| --- | --- |
| 内存解析缓存 | 随 parser 存活，由 TTL/失效控制进程内复用；不是序列化 `openapi3.T` 的存储。 |
| `<config-dir>/spec-cache` | 远端响应字节与元数据。过期后以 `ETag`/`Last-Modified` 重新验证，远端不可达时提供离线回退。删除后需重新获取源才能恢复该能力。 |
| `<config-dir>/index-cache` | 派生命令/help/补全数据，以及受支持 schema 的执行投影。本地源用绝对路径、大小、修改时间；远端用 URL 和内容哈希。parser/semantic/schema 指纹使不兼容投影失效。不支持的执行 schema 回退到完整解析和请求构造。 |
| `<config-dir>/apps/<app>.yaml` 与凭据后端 | 用户配置与 secret，不是派生缓存；升级时保留。 |

派生索引用于优化规范解释，不是另一套 HTTP 执行器。`LoadSpecWithPersistentCache` 当前转发到普通的 context loader；远端持久化缓存由 parser 选项启用。函数名不代表完整 OpenAPI 对象图会序列化，也不能据此推断它与 `LoadSpecWithContext` 有不同缓存实现。

区分以下版本：

- OpenBridge 发布/构建版本标识二进制或 Go ref。
- app YAML 的 `version` 是配置版本，可选 `spec_title`/`spec_version` 描述 API。
- `DerivedIndexSchemaVersion` 标识索引 DTO 的字段**及语义**。Unreleased 有效参数变化将其从 **4 升到 5**，旧索引自动重建。
- 磁盘容器格式和 parser/semantic 指纹是额外且独立的兼容检查。

缓存迁移无需删除 app YAML 或凭据。让索引自动重建，仅在源可重新获取时删除远端内容；不要通过复制不相关版本/配置环境的缓存来迁移。

## 修改边界时的验证

映射、合并、序列化和校验等纯逻辑使用针对性的单元/属性测试。文件系统、HTTP、进程入口、实际 MCP 分派和服务关闭使用 integration tag 测试。比较冷解析/温索引行为，修改 profile 后重新加载，验证工具真正绑定的 HTTP 操作。

仓库门禁包括格式/lint、`go build ./...`、带/不带 `integration` 的 vet、short/race/integration 测试、`go mod tidy -diff`、生产依赖隔离、最低 Go 版本和 `npm run docs:build`。具体命令与工具版本遵循当前[贡献指南](https://github.com/nomagicln/open-bridge/blob/main/CONTRIBUTING.md)。结果与限制记入审计；本文不宣称未完成工作树已通过门禁。
