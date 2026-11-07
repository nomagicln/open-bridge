# OpenBridge 修复计划 —— 批次 4（错误模型、执行路径索引 POC、config 死 API 清理）

> 上游：[`FIXES-BATCH3.md`](./FIXES-BATCH3.md) §5、[`ENGINEERING-DIAGNOSIS.md`](./ENGINEERING-DIAGNOSIS.md) F-22/F-23
> 基线：DCO 修复后的分支 `654b363`（32 个提交全部 Signed-off-by，已推送）；工作树干净
> 原则：TDD、契约先行、文件级写作用域隔离、不修改 `.golangci.yml`（失效项已清理）、不新增第三方依赖、DCO 签名提交
> 本轮**不做**（批次 5）：`pkg/config` 整体拆分、派生索引 P4 的复杂 schema 支持、ghodss/yaml.v2 module graph 旧链路升级

---

## 1. 范围与负责人

| 工作流 | 内容 | 对应 | 负责人 | 写作用域 |
| --- | --- | --- | --- | --- |
| **W1 错误模型 typed/sentinel 化** | 用 `errors.As` 类型分类替换 `pkg/cli/errors.go` 的字符串嗅探；新增 typed errors（unknown resource/verb、missing parameter、parameter validation、spec load）；`PrintedError.Error()` 返回真实文本；更新 producer 与测试 | F-22/F-23 | style-auditor | `pkg/cli/errors.go`、`pkg/cli/errors_test.go`、`pkg/cli/errtypes.go`（新）、`pkg/cli/handler.go`、`pkg/cli/handler_test.go`、`pkg/request/builder.go`、`pkg/request/builder_test.go`、`internal/app/commands.go`（仅 spec-load 包装） |
| **W2 执行路径索引 POC（P4）** | 对"可索引 operation"用派生索引构建请求、跳过完整 spec 解析；不可索引安全回退；输出与解析路径一致；端到端基准 | D-05 / proposals/02 | perf-analyst（W1 完成后启动） | `pkg/spec/derived_index*.go`、`pkg/cli/handler.go`、`pkg/cli/handler_test.go` |
| **W3 pkg/config 死 API 清理** | 删除无生产消费者的 `export.go`（profile 导出/导入，680 行）及 tests、`Profile.BuildSpecFetchOptions`（生产零调用）及测试；确认引用后最小调整 | 架构 F-42 / 风格 F-18 | process-auditor | `pkg/config/export.go`、`pkg/config/export_test.go`、`pkg/config/export_property_test.go`、`pkg/config/config.go`、`pkg/config/config_test.go`、`pkg/config/profile.go`（仅确需时） |
| **W4 汇总/走线** | DCO 已修复；批次 4 契约/终验/文档/提交/推送 | — | Lead | `FIX-PLAN-BATCH4.md`、`FIXES-BATCH4.md`、`ENGINEERING-DIAGNOSIS.md`（状态附录） |

## 2. 契约

### C-E1（W1）错误模型
- 新增 `pkg/cli/errtypes.go`（或 errors.go 内）：
```go
type UnknownResourceError struct{ Resource string }
type UnknownVerbError struct{ Resource, Verb string }
type SpecLoadError struct{ Source string; Err error }   // Error()/Unwrap()
```
- `pkg/request/builder.go` 新增：
```go
type MissingParameterError struct{ Name string }
type ParameterValidationError struct{ Parameter, Reason string }
```
（`checkRequiredParams`/`checkRequiredBodyFields`/`validateEnumValue`/`validateParameterType` 返回 typed error；保留原有可读消息，`Error()` 文本不变，避免用户可见变化）
- `pkg/cli/errors.go::FormatErrorWithContext`：删除 5 处 `strings.Contains` 分类，改为 `errors.As` 链（保留 `url.Error`/`net.OpError`/`config.AppNotFoundError` 现有分支）；各 `format*` 函数改为从 typed error 字段取值，不再从字符串 `Split` 解析。
- `PrintedError.Error()` 返回底层错误文本（`main` 的"已打印不重复打印"逻辑不变）；不得造成双重打印（测试锁定）。
- producer 迁移：`pkg/cli/handler.go` 的 `resolveOperation`（unknown resource/verb）、spec-load 包装（handler/router/internal/app）；确保所有用户可见错误文本**逐字不变**（用 e2e 对照）。
- 测试：每种 typed error 的 `errors.As` 分类；用户可见文本快照；`PrintedError` 语义；混合包装（`%w`）场景。

### C-E2（W2）执行路径索引
- DTO v3：为 operation 增加执行所需最小 schema 信息（参数 required/type/enum，body required fields/type；仅简单标量/字符串数组可索引），新增 `ExecutionIndexable bool` + 不可索引原因。
- `pkg/cli/handler.go` 执行路径：`LoadSpec` 前先 `LoadDerivedIndex`；`ExecutionIndexable` 时用索引重建 `openapi3.Operation`（复用 W1 批次 3 的 help 重建逻辑）→ 走现有 Validate/BuildRequest/execute；否则回退完整解析。
- 输出、错误文本、退出码与解析路径一致；HTTP mock 断言请求 method/path/query/body 一致。
- 基准：4MB spec 上 `ob <app> <res> <verb>`（mock API）cold/warm 对比；给出 skipped spec parse 的耗时收益（目标命令总耗时下降 50%+）。
- POC 若遇 schema 复杂度边界，允许 `ExecutionIndexable=false` 安全回退，并在报告列出覆盖范围。

### C-E3（W3）config 死 API 清理
- 删除前确认：`ExportProfile*`/`ImportProfile*`/`BuildSpecFetchOptions` 在 pkg/cmd/internal 生产代码零调用（排除将被删除文件；测试文件随之删除）。
- 删除 `export.go`/`export_test.go`/`export_property_test.go`；删除 `config.go` 中 `ExportProfile`/`ImportProfile` 与 `BuildSpecFetchOptions` 及其 `config_test.go` 测试；保留 `Profile` 结构与 `export.go` 中被其他文件使用的类型（如 `ExportedProfile` 若仅测试使用则一并删除）。
- 若 `profile.go` 中的 `ProfileManager` 相关函数无生产消费者，列入报告但**本轮不删**（避免扩大范围）。
- 验收：`go build ./...`、`go test ./pkg/config`、全仓 test-short；`grep -rn 'ExportProfile\|ImportProfile\|BuildSpecFetchOptions' --include='*.go' pkg cmd internal` 仅剩注释/文档或为空。

## 3. 验收矩阵

| 检查 | 标准 |
| --- | --- |
| build/gofmt/goimports/vet（双 tag）/short/race/integration | 全绿 |
| lint / govulncheck / tidy diff / actionlint / docs build | 0 issues / 0 可达 / 空 / 通过 / 通过 |
| 覆盖率 | ≥73.7% |
| 错误文本 | 与批次 3 二进制逐字一致（typed 化不改变用户可见输出） |
| 执行路径 POC | 可索引场景输出/请求与解析路径一致；基准数字 |
| e2e | 批次 0（22）/1（18）/2（20）/3（19）+ 批次 4 新增 |

## 4. 执行顺序

1. W1 + W3 并行（互不冲突）
2. W1 完成后启动 W2（P4 POC，复用 handler.go 写作用域）
3. Lead：终验、e2e 回归、诊断状态附录、DCO 提交/推送

---

## 5. 执行结果（2026-11-07，批次 4 完成）

| 工作流 | 状态 | 关键证据 |
| --- | :---: | --- |
| W1 错误模型 typed 化 | ✅ | 5 处 strings.Contains 全部替换 errors.As；13/13 错误/正常命令文本与基线逐字一致；PrintedError 返回真实文本 |
| W2 执行路径索引 P4 POC | ✅ | 4.09MB spec 命令 147.0→11.4ms（12.9×）；1MB 42.2→10.3ms；删 spec 后 indexable 命令仍可执行；不可索引安全回退 |
| W3 config 死 API 清理 | ✅ | 删除 1,934 行 / 0 新增；引用 grep 为空；short+integration 全绿 |
| W4 config 拆分提案 | ✅ | proposals/03_config_split.md（346 行，S0–S5 分步 + 实测依赖矩阵） |
| DCO 修复 | ✅ | rebase --signoff 重写 32 提交（树不变），force-with-lease 推送；32/32 签名 |
| 全量门禁 | ✅ | build/vet 双 tag/short(14)/race(14)/integration(15)/lint(0)/govulncheck(0)/tidy/actionlint/docs；覆盖率 75.2% |
| e2e 回归 | ✅ | 批次 4 22/22、批次 3 19/19、批次 2 20/20、批次 1 18/18、批次 0 22/22 |

交付汇总见 [`FIXES-BATCH4.md`](./FIXES-BATCH4.md)。批次 5 候选见该报告 §5 / 诊断附录 D.3。
