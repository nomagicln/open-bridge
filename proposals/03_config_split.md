# 提案 03：`pkg/config` 拆分（批次 5 预案）

| 项 | 值 |
| --- | --- |
| 状态 | 设计提案，待批次 5 评审/排期 |
| 基线 | HEAD `654b363`（批次 3 完成后）；**测量快照 = 2026-11-07T21:32Z 工作树**（W1 在改 `pkg/cli`/`pkg/request`，W3 在改 `pkg/config`：`export.go` 已删除、`config.go` 已从 934 行裁剪到 778 行、`test_helpers.go` 78→46 行；W3 尚未提交） |
| 上游 | `FIX-PLAN-BATCH4.md`（明确"批次 5 才做 `pkg/config` 整体拆分"）、`ENGINEERING-DIAGNOSIS.md` D-05/§4.2、`.diagnosis/architecture.md` F-05 |
| 方法 | AST 盘点（`/tmp/config-split/inventory.go`）+ 生产/测试引用矩阵（`matrix.py`）+ `go list -deps` + 逐文件人工核读；不修改任何源码 |
| 结论 | 推荐三段式：**行为先走（S1 安装流程 → internal/install）、再收口（S2 TLS 校验、S3 删 profile 死 API）、最后内化 TUI（S4）**；`pkg/config` 最终约 800 行非测试代码 / 39 个导出符号，依赖降为叶子（stdlib+yaml） |

> 口径说明：本文"导出符号"= AST 顶层导出声明（函数/类型/方法/常量），当前实测 **93**；诊断报告中"243"是按行 grep（含结构体字段）得到的旧口径，两者不可直接比较。W3 已删除的 `export.go`（680 行/20 导出）**不计入**本文现状。

---

## 1. 现状盘点（工作树快照）

### 1.1 文件职责 / 行数 / 导出符号

| 文件 | 非测试 Loc | 导出数 | 职责 | 归属判断 |
| --- | ---: | ---: | --- | --- |
| `config.go` | 778 | 39 | 领域模型（`AppConfig/Profile/AuthConfig/TLSConfig/SafetyConfig/RetryConfig/OAuth2Config/HybridSearchSettings/Duration`）+ `Manager` CRUD/校验/原子写 + 错误类型 + 配置目录 | **保留在 `pkg/config`** |
| `install.go` | 959 | 13 | 安装编排（`InstallApp/doInstall`）、shim/PATH（`CreateShim/RemoveShim/ShimExists/IsShimDirInPath/GetPathInstructions`）、交互 prompt、安装 DTO（`InstallOptions/InstallResult/InstalledAppInfo`）、`UpdateApp/GetInstalledAppInfo` | **迁移 `internal/install`** |
| `path_utils.go` | 261 | 10 | TLS 证书/私钥/CA 路径校验、通用路径校验（`ValidateAndResolvePath`） | **迁移 `internal/install`**（唯一生产消费者是 TUI，见 1.3） |
| `profile.go` | 581 | 31 | `ProfileManager` + `ProfileInfo/ProfileOptions/ProfileExistsError`：profile 增删改查封装 | **删除**（生产零消费者，仅 `internal/integration` 测试 4 处使用） |
| `test_helpers.go` | 46 | 0 | `assertNonexistentAppError`（config/install 测试用）、`assertUnixShimSymlink`（install 测试用） | **拆分**：前者留 config，后者随 install 走 |
| 合计 | **2,625** | **93** | 不含测试 2,979 行 | 目标：config ~800，其余迁出/删除 |

测试文件（迁移/删除时必须成对处理，AGENTS.md §3.1）：

| 测试文件 | Loc | 配对源 | 去向 |
| --- | ---: | --- | --- |
| `config_test.go` | 596 | `config.go` | 留 `pkg/config` |
| `config_property_test.go` | 104 | `config.go` | 留 `pkg/config` |
| `install_test.go` | 1,048 | `install.go` | → `internal/install` |
| `install_integration_test.go`（`//go:build integration`） | 265 | `install.go` | → `internal/install` |
| `path_utils_test.go` | 272 | `path_utils.go` | → `internal/install` |
| `profile_test.go` | 694 | `profile.go` | 随 `profile.go` 删除 |

### 1.2 包内依赖与隐式全局状态

- **包内唯一非 stdlib 依赖**：`install.go` → `pkg/spec`（`spec.NewParser/LoadSpec/GetSpecInfo`），进而 `spec → semantic`。`config.go`/`profile.go`/`path_utils.go` 仅 stdlib + `yaml.v3`。
- **因此 `pkg/config` 目前不是叶子**：`go list -deps ./pkg/config` = `{semantic, spec, config}`。把 `install.go`+`path_utils.go` 迁出后，config 立即变成叶子（仅 stdlib + yaml.v3），这是本次拆分**最明确的架构收益**。
- **隐式全局状态**：只有 2 个不可变包级变量（`config.go:627` `appNameRegex`、`:630` `reservedAppNames`），无单例、无懒初始化、无 init()。`Manager` 是显式构造（`NewManager`/`WithConfigDir`），批次 3 后由 `internal/app.Deps` 单实例持有。**拆分不会引入新的全局状态**。

### 1.3 被引用矩阵（生产代码，按包去重）

直接 import `pkg/config` 的包（7 个生产 + 1 个测试）：

```
internal/app, pkg/cli, pkg/completion, pkg/mcp, pkg/request, pkg/router, pkg/tui/install
+ internal/integration（仅测试）
```

传递依赖 config 的包：`cmd/ob, internal/app, pkg/{cli,completion,mcp,request,router,tui/install}`（共 8 个）。

**生产代码引用的 config 符号（全部，共 16 个）**：

| 符号 | 引用包数 | 包 | 归属决策 |
| --- | ---: | --- | --- |
| `Profile` | 5 | app, cli, mcp, request, router | 留 config（跨包共享模型） |
| `AppConfig` | 4 | app, cli, mcp, router | 留 config |
| `Manager` | 4 | app, cli, completion, router | 留 config |
| `RetryConfig` | 3 | cli, mcp, request | 留 config |
| `InstallOptions` | 2 | app, **tui/install** | 迁 `internal/install`（S4 后 TUI 同步） |
| `AppNotFoundError` | 1 | cli（`errors.As`） | 留 config |
| `AuthConfig` | 1 | request | 留 config |
| `TLSConfig` | 1 | request | 留 config |
| `SafetyConfig` | 1 | mcp | 留 config |
| `InstallResult` / `InstalledAppInfo` | 1 | app | 迁 `internal/install` |
| `NewManager` | 1 | app | 留 config |
| `DefaultProgressiveThreshold` | 1 | tui/install | 迁 `internal/install`（S2/S4） |
| `ValidateTLSCACertFile` / `ValidateTLSCertFile` / `ValidateTLSKeyFile` | 1 | tui/install | 迁 `internal/install`（S2） |

**仅测试引用的符号**（不影响生产迁移）：`Duration`（cli/mcp/request 测试）、`NewAppConfig`/`NewProfile`（router 测试）、`ProfileOptions`/`WithConfigDir`（integration/completion 测试）。

**`Manager` 方法的外部使用（生产）**：

| 包 | 使用的方法 |
| --- | --- |
| `internal/app` | `AppExists, ConfigDir, GetAppConfig, GetProfile, ListApps, SaveAppConfig` + **安装类**: `InstallApp, UninstallApp, GetInstalledAppInfo` |
| `pkg/completion` | `GetAppConfig, ListApps` |
| `pkg/mcp` | `GetProfile` |
| `pkg/router` | `GetAppConfig, GetProfile` |

→ 安装类方法只有 `internal/app` 3 个调用点（`commands.go:203/283/324,343`），拆分影响面极小；其余 4 个包只依赖"领域模型 + 读配置"。

**`profile.go` / `path_utils.go` 的生产引用**：

- `ProfileManager`/`NewProfileManager`：生产 **0**；仅 `internal/integration/integration_test.go:341,395` 两处（4 行）。
- `path_utils.go`：生产消费者仅 `pkg/tui/install/model.go:668,714,721` 三个 TLS 校验 + `DefaultProgressiveThreshold`（install.go）；`config` 包内无其他使用者。
- `install.go` 的 shim/PATH 函数：外部生产引用 **0**（全部经 `InstallApp/UninstallApp` 内部调用）。

### 1.4 依赖方向（拆分前）

```
pkg/cli ─┐
pkg/mcp ─┤
pkg/request ─┼─▶ pkg/config ─▶ pkg/spec ─▶ pkg/semantic
pkg/router ─┤
pkg/completion ─┤
internal/app ─┘
pkg/tui/install ─▶ pkg/config   （仅 5 个符号）
```

### 1.5 测试与校验文件耦合

- `install_integration_test.go` 依赖 `//go:build integration`（AGENTS §3.1 要求保留）；它使用 `config.Manager.InstallApp` 9 次。
- `internal/integration/integration_test.go` 使用 `config.InstallOptions`（9 处 `InstallApp`）、`ProfileOptions/NewProfileManager`（4 处）、`WithConfigDir`；拆分 Step 1/3/4 会改动该测试文件约 40–60 行。
- `pkg/tui/install`：`model.go` 引用 `config.InstallOptions`（~20 处字段）、`DefaultProgressiveThreshold`、3 个 TLS 校验函数；测试同。
- `pkg/config/test_helpers.go` 两个 helper 被不同包测试使用（见 1.1），拆分时必须分别落地，否则触发 "orphan/未定义 helper"。

---

## 2. 目标结构

### 2.1 目标包布局（推荐方案）

```
pkg/config/                 # 领域模型 + 持久化（叶子：stdlib + yaml.v3）
  config.go                 # AppConfig/Profile/Auth/TLS/Safety/Retry/Duration + Manager CRUD/校验/原子写
  config_test.go
  config_property_test.go
  test_helpers.go           # 仅 assertNonexistentAppError（供本包测试）

internal/install/           # 安装用例 + OS 集成 + 交互提示（依赖 config + spec）
  install.go                # Installer{cfg, parser}; Install/Uninstall/Update/Info + 编排
  shim.go                   # CreateShim/RemoveShim/ShimExists/IsShimDirInPath/GetPathInstructions/getOBBinaryPath
  prompt.go                 # promptString/YesNo/Choice + promptForMissingInfo（Interactive 遗留能力，可标 Deprecated）
  tlsassets.go              # 由 path_utils.go 迁入（TLS 证书/CA/私钥校验）
  options.go                # InstallOptions/InstallResult/InstalledAppInfo/DefaultProgressiveThreshold（S4 后）
  *_test.go                 # install_test/install_integration/path_utils_test + helper

internal/tui/install/       # 由 pkg/tui/install 迁入（S4a；~2,764 行含测试）
  model.go / *_test.go / test_helpers.go

internal/app/               # 组合根（批次 3 已就位）
  Deps 增加 Installer *install.Installer；New() 构造；commands 调 d.Installer.*
```

### 2.2 各包职责与依赖方向

| 包 | 职责 | 允许依赖 | 导出面目标 |
| --- | --- | --- | --- |
| `pkg/config` | 领域模型 + 配置存储（CRUD/校验/原子写/配置目录） | stdlib + yaml.v3（**叶子**） | 39 → 最终 39（仅模型+Manager+错误） |
| `internal/install` | 安装/卸载/更新/信息、shim/PATH、TLS 资产校验、安装 DTO、prompt | `pkg/config`、`pkg/spec`、stdlib | ~18（Installer 方法 + 少量函数/类型） |
| `internal/tui/install` | 安装向导 UI 状态机 | `internal/install`、`pkg/spec`、bubbletea | 3（`NewModel/Model/Result` 现状不变） |
| `internal/app` | 组合根/命令编排/渲染 | 上述 + cli/mcp/router/completion/request/semantic | `Deps/New/Run` |
| cli / mcp / request / router / completion | 只依赖领域模型与 `Manager` | `pkg/config`（模型） | 不变 |

目标依赖图（无环）：

```
internal/app ─▶ internal/install ─▶ pkg/config（叶子）
      │                └──────────▶ pkg/spec ─▶ pkg/semantic
      ├─────────────▶ internal/tui/install ─▶ internal/install
      └─────────────▶ pkg/{cli,mcp,request,router,completion} ─▶ pkg/config
```

### 2.3 类型归属：两种策略（推荐 A→B 分阶段）

| 策略 | 内容 | 风险 | 建议 |
| --- | --- | --- | --- |
| **A（S1 采用）** | 安装**行为**迁 `internal/install`；`InstallOptions/InstallResult/InstalledAppInfo/DefaultProgressiveThreshold` **暂留 `pkg/config`**（TUI/app 的既有 import 不动，最小化并行冲突） | 低；config 仍留 4 个安装 DTO | 批次 5 前半段 |
| **B（S4 收口）** | DTO + `DefaultProgressiveThreshold` 迁 `internal/install`；TUI/app/integration 更新 import；`pkg/config` 只保留领域模型 | 中；公开 API breaking（pre-1.0 可接受），TUI 测试批量改 import | 批次 5 后半段（S4，需先内化 TUI） |

> 不能通过"在 `pkg/config` 里做 type alias/转发方法"保持兼容：`config` 与 `internal/install` 双向引用会形成循环（Go 不允许 parent↔child 包循环）。因此 **breaking 不可避免**，只能选择"何时发生"。
> 兼容替代方案：若必须保持 v0.1.x 公开 API，S1 不删 `Manager.InstallApp` 等方法，而是追加 `Installer` 新路径（双实现会漂移，不推荐）；本提案推荐在 **v0.2.0**（pre-1.0）做一次性 breaking，并在 CHANGELOG 给出迁移表（旧方法→新方法）。

### 2.4 `internal/app` 装配变化

```go
type Deps struct {
    Config     *config.Manager     // 领域读/写（保留）
    Installer  *install.Installer  // 新增：安装用例
    Parser     *spec.Parser
    ... // 其余 7 个字段不变
}

func New() (*Deps, error) {
    configMgr, err := config.NewManager()          // 不变
    ...
    installer := install.New(configMgr)             // parser 由 Installer 内部按需构造（与现状一致）
    return &Deps{Config: configMgr, Installer: installer, ...}, nil
}
```

命令调用点变化（`internal/app/commands.go`，仅 3 处 + 1 处类型）：

```go
// before                                   // after
d.Config.InstallApp(app, opts)              d.Installer.Install(app, opts)
d.Config.UninstallApp(app, removeShim)      d.Installer.Uninstall(app, removeShim)
d.Config.GetInstalledAppInfo(app)           d.Installer.Info(app)
config.InstallResult / InstalledAppInfo     （S1 不变；S4 随 DTO 迁到 install.*）
```

### 2.5 TUI 解耦

- **S2**：TUI 只需把 3 个 `config.ValidateTLS*` 改为 `install.ValidateTLS*`（import `internal/install`），`InstallOptions` 暂不变（策略 A）。
- **S4a**：`git mv pkg/tui/install internal/tui/install`，全仓只有 `internal/app` 引用它（1 处 import），**不动业务代码**；消除 "public 包依赖 internal 包" 的不自然方向（S2 若先做，会出现 `pkg/tui → internal/install`，Go 允许但语义不佳，因此 S4a 应在 S2 前或同批完成）。
- **S4b**：DTO 迁入 `internal/install` 后，TUI 的 `config.` 前缀改为 `install.`（model.go 约 20 处、测试约 40 处，机械替换）。

---

## 3. 分步迁移计划

> 原则：每步一个独立可回滚提交；**同一步内"移动 + 调用点更新 + 测试迁移"一起完成**，绝不保留双实现；每步结束跑本步验收 + 全仓 short/lint。

### S0 基线与骨架（S，0.5d）
- 记录基线：`go list -deps ./pkg/config`、覆盖率、批次 0–4 e2e 结果、`grep 'config\.(Install|Uninstall|...)'`；
- 新建 `internal/install/doc.go`（只有包注释，无代码）与 `proposals/03` 引用；不改行为。
- 验收：`go build ./...`、`go test -short ./...` 与基线一致。

### S1 安装行为迁移：`config.Manager` → `internal/install.Installer`（M，1–1.5d）
- **移动**：`install.go` 中除 `InstallOptions/InstallResult/InstalledAppInfo` 外的全部函数（约 840 行）→ `internal/install/{install.go,shim.go,prompt.go}`；`install_test.go`(1,048)+`install_integration_test.go`(265) 同步 `git mv`；`test_helpers.go` 的 `assertUnixShimSymlink` 拆分到 `internal/install/test_helpers.go`。
- **保留** `pkg/config/install_types.go`：3 个类型（~120 行）+ `DefaultProgressiveThreshold`（避免动 TUI）。
- **API**：新增 `install.New(cfg *config.Manager) *Installer`；方法 `Install/Uninstall/Update/Info/CreateShim/RemoveShim/ShimExists/IsShimDirInPath/GetPathInstructions`；删除 `config.Manager` 上对应方法。
- **调用点**：`internal/app/app.go`（Deps+New）、`internal/app/commands.go`（3 处）、`internal/integration/integration_test.go`（9 处 InstallApp）；`pkg/config` 不再 import `spec`。
- **兼容**：breaking（`Manager.InstallApp` 等移除）；CHANGELOG 迁移表；无 alias（循环约束）。
- **验收**：
  ```bash
  go build ./...
  go test -count=1 ./internal/install ./pkg/config ./internal/app
  go list -deps ./pkg/config | grep -E 'open-bridge/pkg/(spec|semantic)'   # 期望空
  go test -short -count=1 ./...
  # e2e: install / uninstall / list / info / shim / install -i 各跑一遍（隔离 HOME）
  PATH=/tmp/gobin2:$PATH golangci-lint run ./...
  ```
- 行数：新增 ~960（3 文件），删除 config 行为 ~840（类型保留），测试迁移 1,313。

### S2 TLS/路径校验下沉（S，0.5d）
- `path_utils.go`(261)+`path_utils_test.go`(272) → `internal/install/tlsassets.go`+test；
- TUI `model.go` 3 处 + `model_test.go` 调用改 `install.ValidateTLS*`（import `internal/install`）；
- `pkg/config` 去掉 `crypto/x509`/`encoding/pem` 依赖（config 包彻底无 OS 资产校验）。
- 验收：`go test ./pkg/tui/install ./internal/install ./pkg/config`；`go list -deps ./pkg/config` 仅 stdlib+yaml；TUI TLS 用例绿；e2e `ob install -i` 冒烟。
- 行数：移动 533（含测试），config -261。

### S3 删除 `profile.go` 死 API（M，1d）
- 证据：`ProfileManager` 生产 0 引用（仅 integration 测试 4 处）、`pkg/config/profile_test.go` 694 行；`profile.go` 31 个导出符号（`ProfileManager/ProfileInfo/ProfileOptions/...`）。
- 动作：删除 `profile.go`+`profile_test.go`（1,275 行）；`internal/integration/integration_test.go` 的 4 处改为直接编辑 `AppConfig.Profiles` map（约 40 行改动）；保留 `AppConfig.AddProfile/GetProfile/...` 现有模型方法。
- 若产品确认未来要 profile CRUD 能力：改为迁 `internal/profile`（依赖 config），不留在 `pkg/config`；**本轮推荐直接删除**。
- 验收：`grep -rn 'ProfileManager' --include='*.go' pkg cmd internal` 为空；`go test -short ./...` + `go test -tags=integration ./internal/integration`；e2e `info/list` 输出不变。
- 行数：-1,275（含测试），导出 -31。

### S4 TUI 内化 + DTO 归位（M，1–1.5d，推荐但可降级为 follow-up）
- **S4a**：`git mv pkg/tui/install internal/tui/install`（model.go 1,370 + 测试/helper 1,394 = 2,764 行）；`internal/app/commands.go` import 改 1 处；`pkg/tui` 目录删除（无其他引用）。
- **S4b**：`InstallOptions/InstallResult/InstalledAppInfo/DefaultProgressiveThreshold` 迁 `internal/install/options.go`；更新 `internal/app`（~20 处）、`internal/tui/install`（~60 处含测试）、`internal/integration`（~10 处）import。
- 验收：
  ```bash
  grep -rn 'config\.Install\|config\.DefaultProgressiveThreshold' --include='*.go' pkg cmd internal  # 期望空
  go list -deps ./pkg/config | grep -E 'open-bridge'   # 期望空（叶子）
  go test -count=1 ./internal/... ./pkg/config
  go test -short -count=1 ./... && go vet ./... && go vet -tags=integration ./...
  # e2e: help/install/-i/run/list/info/uninstall/shim
  ```
- 行数：移动 ~130（类型/常量）+ TUI 2,764；config 非测试降至 ~800。

### S5 门禁/文档收尾（S，0.5d）
- `.golangci.yml`：`pkg/config/install\.go` 的 gocyclo 豁免路径改 `internal/install/install\.go`（**批次 5 允许改，本提案不动配置**）；检查 `dupl/funlen` 在移动后仍 0 issue；
- 文档：`docs/` 架构说明、`ENGINEERING-DIAGNOSIS.md` F-05 状态标注、CHANGELOG（v0.2.0 breaking 迁移表）、`FIXES-BATCH5.md`；
- 覆盖率：`pkg/config` 分拆后测试减少，使用全仓覆盖率门槛（当前 ≥73.7%）或按 `-coverpkg` 汇总，不按单包硬卡。
- 验收：lint 0、build/vet（双 tag）/short/race/integration 全绿、文档构建通过。

**总计**：约 **4–5.5 人日**（S0 .5 + S1 1.5 + S2 .5 + S3 1 + S4 1.5 + S5 .5），可拆成 5 个 PR；S1/S3 可并行（不同文件），S2/S4 依赖 S1。

---

## 4. 明确不建议拆分/改动的部分

1. **领域模型不拆**：`AppConfig/Profile/AuthConfig/TLSConfig/SafetyConfig/RetryConfig/OAuth2Config/HybridSearchSettings/Duration` 被 5 个包跨包共享（`Profile` 83 处、`AppConfig` 59 处、`SafetyConfig` 52 处引用），保持 `pkg/config` 单一模型包；不要按类型再拆 `config/auth`、`config/tls`（会造成 import 网状与迁移成本）。
2. **`Manager` 读写保持**：`GetAppConfig/SaveAppConfig/ListApps/AppExists/ConfigDir/AppsDir/校验` 是稳定领域能力；`WithConfigDir` 被测试/集成使用，保留。
3. **错误类型保留**：`AppNotFoundError`/`ProfileNotFoundError` 被 `pkg/cli` 用 `errors.As` 分类（批次 4 typed error 依赖），不迁 internal。
4. **`Duration` 自定义 YAML 编解码保留**（配置格式兼容）。
5. **不在本批拆 `pkg/cli`、`pkg/router`、`pkg/spec`**：与本提案正交，避免一次 PR 过大。
6. **不为了"零 breaking"引入双实现/转发层**（会漂移且形成循环）；接受 v0.2.0 一次性迁移。

---

## 5. 风险与回滚

| # | 风险 | 影响 | 缓解 |
| --- | --- | --- | --- |
| R1 | **公开 API breaking**（`Manager.InstallApp` 等与 `Install*` 类型迁移；仓库有 `v0.1.x` tag） | 外部导入者编译失败 | pre-1.0 可接受：v0.2.0 + CHANGELOG 迁移表；S1/S4 分两步，让外部用户有过渡；不提供 alias（循环） |
| R2 | **循环依赖**：`config ↔ internal/install` 一旦出现即编译失败，且 alias 方案会制造它 | 拆不动/返工 | 铁律：`pkg/config` 绝不 import `internal/*`；每步用 `go list -deps ./pkg/config` 断言（应为 stdlib+yaml）；CI 增加依赖方向检查 |
| R3 | **TUI 方向问题**：S2 会让 `pkg/tui`（public）import `internal/install` | 语义不佳但合法 | 先 S4a 内化 TUI，再做 S2/S4b；或 S2/S4 合并执行 |
| R4 | **测试配对/命名**（AGENTS §3.1）：移动 `install.go` 必须同移 `install_test.go`/`install_integration_test.go`；`test_helpers.go` 两份 helper 拆开 | 孤儿测试/helper 未定义/检查失败 | 用 `git mv` 保历史；每步跑 `go vet -tags=integration ./...`；按文件对照表（附录 B）逐项删除 |
| R5 | **lint 路径豁免失效**：`.golangci.yml` 的 `pkg/config/install\.go` gocyclo 豁免在移动后指向不存在路径 | lint 可能新报 gocyclo | S5 更新豁免路径（批次 5 允许）；S1 提交前本地跑 lint 确认 |
| R6 | **并行冲突**：W3 正在改 `config.go/export`（快照中已删 export.go），W1 在改 cli/request | 合并冲突/中间态 | S1 在 W3 合并后开始；文件级写作用域：`pkg/config/*` vs `internal/install/*` vs `internal/app`；S1 期间不碰 `pkg/cli` |
| R7 | **覆盖率重新分布**：`pkg/config` 测试大量迁出，单包覆盖率骤降 | codecov/门禁红 | 改用全仓覆盖率门槛（≥73.7%）+ `-coverpkg=./...` 汇总；不按单包硬卡；S1 内补 installer 的少量测试 |
| R8 | **e2e 行为回归**（install/uninstall/shim/info/`-i` TUI） | 用户可见变化 | 每步跑批次 0–4 e2e 场景（22/18/20/19）+ 新增 install 冒烟；对比隔离 HOME 输出 |
| R9 | **隐藏调用点**：`internal/integration` 测试、`pkg/tui` 测试仍用 `config.InstallOptions` | 编译失败 | 每步前置 grep：`grep -rn 'config\.\(Install\|Uninstall\|UpdateApp\|GetInstalledAppInfo\|CreateShim\|RemoveShim\|ShimExists\|IsShimDirInPath\|GetPathInstructions\)' --include='*.go' pkg cmd internal`，为 0 才删方法 |
| R10 | **`spec` 依赖从 config 移除**影响其他包传递依赖（cli/mcp 等本已直接 import spec） | 理论无行为变化 | 仅依赖图变化；`go mod tidy` diff 应为空；观察 `go build`/二进制体积无意外 |

**回滚策略**：每步独立 commit（S5 提到 5 个 PR），回滚 = revert 该提交；因**不保留双实现**，回滚后调用点回到旧 `Manager` 方法即可，无状态漂移。`git mv` 保历史，回滚不会丢测试文件。

---

## 6. 批次 5 验收矩阵（整批完成时）

| 检查 | 命令 | 标准 |
| --- | --- | --- |
| 构建 | `go build ./...` | 0 |
| 格式/静态 | `gofmt -l pkg cmd internal`、`goimports -l`、`go vet ./...`、`go vet -tags=integration ./...` | 空 |
| 单测 | `go test -short -count=1 ./...` | 全绿 |
| Race | `go test -race -short -count=1 ./...` | 全绿 |
| 集成 | `go test -tags=integration -count=1 ./...` | 全绿 |
| Lint | `PATH=/tmp/gobin2:$PATH golangci-lint run ./...` | 0 issues |
| 依赖方向 | `go list -deps ./pkg/config \| grep -E 'open-bridge'` | 空（config 叶子） |
| 导出面 | AST 盘点 | `pkg/config` 39 exports；`internal/install` ≤20；profile 31 exports 归零 |
| e2e | 批次 0–4 场景 + install/-i/shim | 输出与基线逐字一致 |
| 文档 | `npm run docs:build` | 通过 |

## 附录 A：测量方法与复现

- AST 盘点：`go run /tmp/config-split/inventory.go pkg/config`（顶层导出声明 + 每文件 LOC）。
- 引用矩阵：`/tmp/config-split/matrix.py`（生产/测试分离；仅统计 import `pkg/config` 的文件中的 `config.<Symbol>`；Manager 方法按 `\.<Method>(` 统计）。
- 依赖：`go list -deps ./pkg/config`、`go list -f '{{.ImportPath}}|{{join .Deps " "}}' ./...`。
- 测试耦合：`grep -rn 'config\.' pkg/tui/install internal/integration pkg/config --include='*.go'`。
- 快照说明：所有数字基于 2026-11-07T21:32Z 工作树；W3 合并后 `export.go` 及 807 行测试正式删除（本文已不计），`config.go` 行数可能继续小幅下降，不改变拆分结论。

## 附录 B：文件级迁移对照表

| 当前 | 目标 | 动作 | 行数 |
| --- | --- | --- | ---: |
| `pkg/config/install.go`（类型部分 :27-116、:947+） | `pkg/config/install_types.go`（S1）→ `internal/install/options.go`（S4b） | 拆分/移动 | ~120 |
| `pkg/config/install.go`（其余 840 行） | `internal/install/{install,shim,prompt}.go` | `git mv`+拆分 | ~840 |
| `pkg/config/path_utils.go` | `internal/install/tlsassets.go` | `git mv` | 261 |
| `pkg/config/profile.go` | — | 删除 | 581 |
| `pkg/config/install_test.go` | `internal/install/install_test.go` | `git mv` | 1,048 |
| `pkg/config/install_integration_test.go` | `internal/install/install_integration_test.go` | `git mv` | 265 |
| `pkg/config/path_utils_test.go` | `internal/install/tlsassets_test.go` | `git mv` | 272 |
| `pkg/config/profile_test.go` | — | 删除 | 694 |
| `pkg/config/test_helpers.go` | 拆为 config（`assertNonexistentAppError`）+ internal/install（`assertUnixShimSymlink`） | 拆分 | 46 |
| `pkg/tui/install/*`（S4a） | `internal/tui/install/*` | `git mv` | 2,764 |

## 附录 C：import 影响面清单

| 位置 | 受影响内容 | 预计改动 |
| --- | --- | --- |
| `internal/app/app.go` | Deps 增 `Installer`；import `internal/install` | ~15 行 |
| `internal/app/commands.go` | 3 处安装调用 + `InstallResult/InstalledAppInfo` 类型（S4b 后再加 import） | ~10 行 |
| `internal/integration/integration_test.go` | `InstallApp` 9 处、`ProfileManager` 4 处、`ProfileOptions/WithConfigDir` | ~50 行 |
| `pkg/tui/install/model.go` + tests | `config.ValidateTLS*`/`DefaultProgressiveThreshold`（S2）；DTO（S4b）；包路径（S4a） | ~60 行 |
| `pkg/{cli,mcp,request,router,completion}` | 无（仅模型/Manager） | 0 |
| `cmd/ob/main.go` | 无（只 import internal/app） | 0 |
| `.golangci.yml` | 1 条 gocyclo 豁免路径（S5） | 1 行 |
