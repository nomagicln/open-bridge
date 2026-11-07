# OpenBridge 修复计划 —— 批次 5（config 拆分 S1–S3、P4 嵌套 schema、app 拆分）

> 上游：[`FIXES-BATCH4.md`](./FIXES-BATCH4.md) §5、[`proposals/03_config_split.md`](./proposals/03_config_split.md)、[`proposals/02_persistent_cache.md`](./proposals/02_persistent_cache.md)
> 基线：批次 4 提交 `0e92d7f`（已推送，36 个提交全部 DCO 签名）；工作树干净
> 原则：TDD、契约先行、文件级写作用域隔离、不修改 `.golangci.yml`、不新增第三方依赖、DCO 签名提交

---

## 1. 范围与负责人

| 工作流 | 内容 | 对应 | 负责人 | 写作用域 |
| --- | --- | --- | --- | --- |
| **W1 config 拆分 S1–S3** | 按 proposals/03：安装流程/shim/PATH/TLS 校验迁入 `internal/install`；删除 `profile.go` 死 API（~1,275 行含测试）；更新 internal/app、pkg/tui、pkg/router 等 import；保持 install/uninstall/-i/shim/TUI 行为等价 | F-05 / proposals/03 | arch-reviewer | `pkg/config/install.go`、`path_utils.go`、`profile.go`、`config.go`、`install_test.go`、`install_integration_test.go`、`path_utils_test.go`、`profile_test.go`、`test_helpers.go`、新增 `internal/install/*`、`internal/app/*`、`pkg/tui/install/*`（仅 import/类型） |
| **W2 P4 嵌套 schema** | 派生索引 DTO 增加递归 schema 元数据（object/array-of-object/oneOf/allOf 等），执行 fast path 支持嵌套请求体；不可支持安全回退；请求/输出与解析路径逐字节一致；基准与索引体积影响 | D-05 / proposals/02 P4 | perf-analyst | `pkg/spec/derived_index*.go`、`pkg/cli/handler.go`、`pkg/cli/handler_test.go` |
| **W3 app 拆分与补全** | W1 完成后：`internal/app/commands.go`（1,050 行）按 install/query/mcp 拆文件；`--flag value<TAB>` 空格形式补全；internal/install 新包覆盖率补齐 | 工程卫生 / 批次 3–4 风险 | process-auditor（W1 后启动） | `internal/app/*`、`pkg/completion/*`、`internal/install/*_test.go` |
| **W4 汇总/走线** | 契约、终验、文档、DCO 提交/推送、交付报告 | — | Lead | `FIX-PLAN-BATCH5.md`、`FIXES-BATCH5.md`、docs、`ENGINEERING-DIAGNOSIS.md`（附录 D 更新） |

## 2. 契约

### C-F1（W1）config 拆分 S1–S3
- 目标（按 proposals/03）：
  - `pkg/config` 仅保留领域模型（AppConfig/Profile/AuthConfig/TLSConfig/SafetyConfig/RetryConfig/Duration 等）+ Manager 读写（Get/Save/Delete/List/AppExists/ConfigDir/AppsDir）+ 校验（app name/配置校验）；
  - 新增 `internal/install`：`Options`/`Result`/`Installer`（或等价 API）、安装/卸载/shim/PATH 逻辑、TLS 路径校验（来自 path_utils.go 的 ValidateTLS*），依赖 `pkg/config` + `pkg/spec`；
  - 删除 `pkg/config/profile.go` 与 profile_test.go（生产 0 调用；`ProfileOptions`/`ProfileManager`/`GetProfileNames`/`SelectProfileForApp` 等）；
  - 更新 `internal/app`（Deps 增 Installer 装配；commands 使用 internal/install）、`pkg/tui/install`（`config.InstallOptions` → `install.Options` 等）、`pkg/router`/其余 import。
- 约束：**行为与 CLI/TUI 输出零变化**（install/-i/uninstall/shim/TUI happy path）；允许 breaking 导出 API（仓库 v0.1.x，proposals/03 建议 v0.2.0），在报告中列出 breaking 清单。
- 测试迁移：`install_test.go`/`install_integration_test.go`/`path_utils_test.go` 随实现移动并按 AGENTS 配对；config 保留测试不动。
- 验收：`go build ./...`；全仓 `go test -short`、`go test -tags=integration`；隔离 HOME e2e：ob install（含 remote spec）/ob uninstall/ob list/ob info/shim 调用/-i TUI 步骤（用已有 TUI 测试与手工脚本）；`go list -deps ./pkg/config` **不再包含 pkg/spec**（proposals/03 S1 验收断言）。

### C-F2（W2）P4 嵌套 schema
- DTO v4：递归 `DerivedSchema`（Type/Types/Format/Enum/Required/Properties/Items/AdditionalProperties/OneOf/AnyOf/AllOf/Ref 状态），参数与 body 复用；
- `ExecutionIndexable` 判定扩展：object（嵌套）/array of object/array of array/oneOf（单层）等可索引；含 unresolved `$ref`、allOf 合并复杂、additionalProperties 动态键、非 JSON media type → 回退；
- fast path 重建 `openapi3.Operation` 时递归恢复 schema；请求体序列化/校验/错误文本与解析路径一致；
- 索引体积影响报告（目标增长可控，如 ≤2× v3）；benchmark 4MB spec 命令 fast path；
- 不可支持时安全回退，报告覆盖比例。

## 3. 验收矩阵

| 检查 | 标准 |
| --- | --- |
| build/gofmt/goimports/vet（双 tag）/short/race/integration | 全绿 |
| lint / govulncheck / tidy / actionlint / docs | 0 / 0 / 空 / 通过 / 通过 |
| 覆盖率 | ≥75.2% |
| e2e | 批次 0–4 回归 + 批次 5 新增（install 拆分等价、嵌套 fast path） |
| 依赖断言 | `go list -deps ./pkg/config` 不含 pkg/spec |
| 行为等价 | install/shim/TUI 输出与批次 4 基线一致；fast path 请求/输出一致 |

## 4. 执行顺序

1. W1 + W2 并行（写作用域分离）
2. W1 完成后启动 W3（internal/app 拆分 + 空格形式补全 + 新包覆盖）
3. Lead：终验、e2e 回归、文档、DCO 提交/推送

---

## 5. 执行结果（2026-11-07，批次 5 完成）

| 工作流 | 状态 | 关键证据 |
| --- | :---: | --- |
| W1 config 拆分 S1–S3 | ✅ | pkg/config 2,625→808 行/93→41 导出/**叶子包**；internal/install 建立（1,278 非测试行）；profile 死 API 删除 1,275 行；25/25 行为等价 |
| W2 P4 嵌套 schema | ✅ | 索引 v4；嵌套 object/array-of-object/array-of-array/oneOf 可索引；可索引率 44%→89%；索引体积 1.08–1.13×；4MB 命令 warm 12.9ms |
| W3 app 拆分与补全 | ✅ | commands.go 1,050 行→4 领域文件（53 decl 1:1）；`--flag value<TAB>` 补全；internal/install 覆盖 68.8%→82.1% |
| 全量门禁 | ✅ | build/vet 双 tag/short(15)/race(15)/integration(16)/lint(0)/govulncheck(0)/tidy/actionlint/docs；覆盖率 **75.9%** |
| e2e 回归 | ✅ | 批次 5 24/24、批次 4 22/22、批次 3 19/19、批次 2 20/20、批次 1 18/18、批次 0 22/22 |

交付汇总见 [`FIXES-BATCH5.md`](./FIXES-BATCH5.md)。批次 6 候选见该报告 §5 / 诊断附录 D.3。
