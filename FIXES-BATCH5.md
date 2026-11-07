# OpenBridge 批次 5 修复交付报告

> 上游：[`FIX-PLAN-BATCH5.md`](./FIX-PLAN-BATCH5.md)、[`FIXES-BATCH4.md`](./FIXES-BATCH4.md) §5、[`proposals/03_config_split.md`](./proposals/03_config_split.md)、[`proposals/02_persistent_cache.md`](./proposals/02_persistent_cache.md)
> 基线：批次 4 提交 `0e92d7f`（已推送，36 个提交全部 DCO 签名）
> 方式：Agent Teams 3 路（W1 config 拆分 / W2 P4 嵌套 schema / W3 app 拆分与补全）+ Lead 走线/终验
> 状态：**全部完成**；全部门禁通过；DCO 签名提交并推送

---

## 1. 结论摘要

| 项 | 批次 4 后 | 批次 5 后 |
| --- | :--- | :--- |
| `pkg/config` | 非测试 2,625 行 / AST 导出 93 / 依赖 `pkg/spec` | **808 行 / 41 导出 / 叶子包**（`go list -deps ./pkg/config` 仅自身）；`-69%` / `-52%` |
| 安装逻辑 | 与配置模型混在 `pkg/config` | 新增 `internal/install`（Installer/TLS/shim/PATH，非测试 1,278 行）；`pkg/config/profile.go` 死 API 删除（1,275 行含测试） |
| 命令执行 fast path | 仅标量参数/body + string 数组 | **嵌套 object / array-of-object / array-of-array / 单层 oneOf/anyOf**；嵌套 fixture 可索引率 **44%→89%** |
| 索引体积 | v3 | v4：4.09MB spec 索引 **519KB→559KB（1.08×）**；4.2MB 1.13×；均 ≤2× |
| 执行延迟（4.09MB） | v3 warm 11.4ms | v4 warm **12.9ms**（cold 132.1ms，10.3×） |
| `internal/app/commands.go` | 1,050 行单文件 | 拆为 **4 个领域文件**（install 286 / list 389 / run 187 / completion 179），原 53 个 decl 1:1 保留 |
| 补全 | `--flag <TAB>`、`--flag=prefix` | 新增 **`--flag value<TAB>` 空格形式**（返回裸值以适配 bash/zsh） |
| `internal/install` 覆盖率 | 新包 | **68.8% → 82.1%**（目标 ≥75%） |
| 覆盖率 | 75.2% | **75.9%**（含新包，未倒退） |
| lint 配置 | 遗留 `pkg/config/install.go` gocyclo 豁免（路径失效） | 删除失效豁免，全仓仍 **0 issues** |

## 2. 工作流与交付

### W1 `pkg/config` 拆分 S1–S3（arch-reviewer）—— 架构 F-05

- **新包 `internal/install`**：`Installer`/`New` + `InstallApp/UninstallApp/UpdateApp/GetInstalledAppInfo/CreateShim/RemoveShim/ShimExists/IsShimDirInPath/GetPathInstructions`；`InstallOptions/InstallResult/InstalledAppInfo/DefaultProgressiveThreshold`；`tlsassets.go`（`ValidateTLS*`、路径校验）。依赖 `pkg/config` + `pkg/spec`。
- **`pkg/config` 收敛**：仅领域模型（AppConfig/Profile/Auth/TLS/Safety/Retry/Duration）+ Manager 读写/校验 + `GetConfigDir`；新增 `Manager.AppConfigPath`、`ValidateAppName` 薄访问器；**808 行 / 41 导出 / 叶子包**。
- **删除** `pkg/config/profile.go` + `profile_test.go`（ProfileManager 家族，生产 0 调用，1,275 行含测试）。
- 测试按 AGENTS 配对迁移：`install_test↔install.go`、`tlsassets_test↔tlsassets.go`、integration tag 保留、helper 拆包。
- **行为等价**：基线 vs 当前 25/25 场景 stdout/stderr/exit code 完全一致（help/version/list/install 本地与远端/uninstall/info text-json-yaml/shim/completion/remote GET 计数）。
- **Breaking 清单**（建议 v0.2.0 + CHANGELOG 迁移表）：
  - 安装类 13 个导出 → `internal/install`（内部包，不承诺公开）；
  - TLS/路径校验 10 个导出 → `internal/install`；
  - ProfileManager 家族 31 个导出 → 删除（改用既有 `AppConfig.AddProfile/GetProfile/DeleteProfile/SetDefaultProfile`）。

### W2 P4 嵌套 schema（perf-analyst）—— D-05

- DTO v4（`DerivedIndexSchemaVersion` 3→4）：递归 `DerivedSchema`（Type/Types/Format/Enum/Required/Properties/Items/AdditionalProperties(+Set/Allowed)/OneOf/AnyOf/AllOf/Ref/Description/Example），参数与 body 统一复用；`openAPISchema()` 递归重建（depth≤16 + visited 环检测）。
- 可索引范围：嵌套 object、array of object、array of array、单层 oneOf/anyOf；安全回退：allOf、additionalProperties 动态键、非 string enum、无 type/union、非 JSON/多 media type、递归/过深、unresolved ref（记录 `ExecutionBlockReason`）。
- **等价证据**：标量、嵌套 object（schema 驱动与 raw `--body`）、array-of-object、array-of-array、oneOf 共 9 类命令的 method/path/query/body/Content-Type 与 stdout **逐字节一致**；缺必填/枚举非法错误文本一致；删 spec 仍可执行；additionalProperties 回退解析。
- **体积/覆盖/基准**：

| 指标 | v3 | v4 | 变化 |
| --- | ---: | ---: | ---: |
| 4.09MB spec 索引 | 519,356B | **559,309B** | 1.08× |
| 4.2MB spec 索引 | 1,088,399B | 1,234,652B | 1.13× |
| 嵌套 fixture 可索引率 | 4/9 (44%) | **8/9 (89%)** | +45pp |
| 4.09MB 命令执行 | cold 132.1ms | **warm 12.9ms** | 10.3× |

### W3 app 拆分与补全（process-auditor）

- `internal/app/commands.go`（1,050 行）拆为 `commands_install.go`(286)/`commands_list.go`(389)/`commands_run.go`(187)/`commands_completion.go`(179)，`app.go` 保留 Deps/New/Run/rootCmd；53 个 decl 1:1 保留，lint 0 issue。
- **`--flag value<TAB>` 空格补全**：`toComplete` 为普通值且 args 末项为 `--flag` → 按前缀过滤枚举值返回裸值（shell 语义正确）；`--status=av` 等号形式返回 `--status=available`；既有行为不变。
- `internal/install` 覆盖率 **68.8%→82.1%**：新增 15 个测试函数（InstallOptions 合并/校验、shim 创建/移除/PATH、TLS 校验、GetInstalledAppInfo、Update/Uninstall 分支等）。

### W4 Lead 走线

- 删除 `.golangci.yml` 中失效的 `pkg/config/install.go` gocyclo 豁免（lint 仍 0 issue）。
- 更新 `ENGINEERING-DIAGNOSIS.md` 附录 D：F-05 从"设计完成待实施"→ S1–S3 已落地；D-05 执行路径 fast path 闭环；指标更新。
- 全量门禁、五批 e2e 回归、提交与推送。

## 3. 验收证据（Lead 最终执行）

| # | 检查 | 结果 |
| :---: | --- | --- |
| 1 | `go build ./...` | ✅ |
| 2 | `gofmt -l` / `goimports -l` | ✅ 0 / 0 |
| 3 | `go vet ./...` + `go vet -tags=integration ./...` | ✅ |
| 4 | `go test -short -count=1 ./...` | ✅ 15 包 |
| 5 | `go test -race -short -count=1 ./...` | ✅ 15 包 |
| 6 | `go test -tags=integration -count=1 ./...` | ✅ 16 包 |
| 7 | `golangci-lint v2.15.0 run ./...` | ✅ **0 issues** |
| 8 | `govulncheck ./...` | ✅ **0 可达漏洞** |
| 9 | `go mod tidy -diff` | ✅ 空 |
| 10 | 覆盖率 | ✅ **75.9%**（批次 4：75.2%，未倒退） |
| 11 | `internal/install` 覆盖率 | ✅ **82.1%** |
| 12 | `go list -deps ./pkg/config` | ✅ 仅 `pkg/config`（叶子） |
| 13 | `npm run docs:build` / actionlint | ✅ |
| 14 | 批次 5 e2e（24 断言） | ✅ **24/24 PASS** |
| 15 | 批次 4/3/2/1/0 回归 | ✅ 22/22、19/19、20/20、18/18、22/22 |

### 3.1 批次 5 e2e 关键输出

```
PASS A1 install output equivalent / A2 config file equivalent / A3 list,info,run,--help equivalent
PASS A4 remote install GET count | baseline=1 current=1
PASS A6 uninstall equivalent / A7 shim symlink equivalent / A8 shim install output equivalent
PASS B6 nested body equals parse path | cur==base (nested object/array body)
PASS B5 nested offline after spec delete
PASS B4 nested fast path latency | median=13.2ms
PASS C1 space-form flag completion | available
PASS C2 equals-form flag completion | --status=available
PASS C3 internal/install coverage | 82.1%
PASS C4 pkg/config is a leaf
```

## 4. 行为变更与兼容性

| 变更 | 影响 | 兼容性 |
| --- | --- | --- |
| `pkg/config` 安装/TLS/ProfileManager API 移出/删除 | 公开 API breaking（仓库 v0.1.x） | 仓库内生产零残留引用；建议 v0.2.0 + CHANGELOG 迁移表（清单见 W1 报告） |
| `internal/install` 新包 | 内部 API，不承诺公开 | — |
| `pkg/tui/install` import `internal/install` | 同模块合法；public→internal 方向待 S4 内化 TUI 优化 | 行为不变 |
| 派生索引 v4 | 旧索引版本失效首跑重建；索引体积 +8–13% | 自动重建；命令输出/请求不变 |
| 执行 fast path 支持嵌套 schema | 更多 operation 免解析；不支持仍回退 | 输出/请求逐字节一致 |
| `--flag value<TAB>` 补全 | 空格形式返回裸枚举值（适配 shell）；等号形式返回 `--flag=value` | 既有补全行为不变 |
| 删除失效 lint 豁免 | 全仓 lint 仍 0 issue | 纯清理 |

## 5. 遗留项（批次 6 候选）

| 项 | 依据 | 说明 |
| --- | --- | --- |
| proposals/03 S4：TUI 内化 + DTO 归位 | F-05 | `pkg/tui → internal/install` 方向优化；config 已在 S1–S3 后收敛为叶子 |
| P4 剩余 schema 边界 | W2 风险 | additionalProperties 动态键、allOf、非 string enum、深递归 ref 仍回退 |
| 索引体积优化 | W2 风险 | 深共享 `$ref` 内联可能放大；可按需裁剪描述/示例 |
| `pkg/config/test_helpers.go` import `testing` | W1 风险 | 非测试文件进生产包（既有约定），可后续清理 |
| Windows-only shim 分支测试 | W3 风险 | darwin 不可达（覆盖率天花板 ~96%） |
| `internal/app` 覆盖率/命令层测试 | — | 拆分后可继续补 |
| ghodss/yaml.v2 module graph 遗留 | 批次 3 | 需升级 viper/etcd 旧链路 |

## 6. 复现验收

```bash
cd /Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge
go build ./... && go vet ./... && go vet -tags=integration ./...
gofmt -l pkg cmd internal && goimports -l pkg cmd internal
go test -short -count=1 ./... && go test -race -short -count=1 ./...
go test -tags=integration -count=1 ./...
go mod tidy -diff
/tmp/gobin2/golangci-lint run ./... && /tmp/gobin2/govulncheck ./...
go list -deps ./pkg/config | grep open-bridge   # 仅 pkg/config
python3 /tmp/lead_verify_b5.py   # 24 断言（批次 5）
python3 /tmp/lead_verify_b4.py   # 22 断言（批次 4 回归）
python3 /tmp/lead_verify_b3.py   # 19 断言（批次 3 回归）
python3 /tmp/lead_verify_b2.py   # 20 断言（批次 2 回归）
python3 /tmp/lead_verify_b1.py   # 18 断言（批次 1 回归）
python3 /tmp/lead_verify.py      # 22 断言（批次 0 回归）
npm run docs:build
```

> 结论：批次 5 完成 `pkg/config` 拆分 S1–S3（→808 行叶子包 + `internal/install`，删除 ProfileManager 死 API）、执行 fast path 嵌套 schema 扩展（索引 +8–13%、可索引率 44%→89%、4MB 命令 12.9ms）、`internal/app` 拆分与空格形式补全、`internal/install` 覆盖率 82.1%；覆盖率 75.2%→75.9%，五批 e2e 回归全部通过。
