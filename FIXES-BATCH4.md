# OpenBridge 批次 4 修复交付报告

> 上游：[`FIX-PLAN-BATCH4.md`](./FIX-PLAN-BATCH4.md)、[`FIXES-BATCH3.md`](./FIXES-BATCH3.md) §5、[`proposals/02_persistent_cache.md`](./proposals/02_persistent_cache.md)、[`proposals/03_config_split.md`](./proposals/03_config_split.md)
> 基线：DCO 修复后的分支 `654b363`（32 个提交全部 `Signed-off-by`，已推送）
> 方式：Agent Teams 3 路并行（W1 错误模型 / W3 config 死 API / W4 config 拆分提案）+ W2（P4 POC，W1 完成后启动）+ Lead 走线/终验
> 状态：**全部完成**；全部门禁通过；DCO 签名提交并推送

---

## 1. 结论摘要

| 项 | 批次 3 后 | 批次 4 后 |
| --- | :--- | :--- |
| 错误分类 | `pkg/cli/errors.go` 5 处 `strings.Contains` + `Split` 解析消息 | **全部 `errors.As` typed error 分类**；用户可见文本逐字不变（13 条 e2e golden 对比） |
| `PrintedError` | `Error()` 返回空字符串 | 返回底层错误文本；main 不重复打印行为由测试锁定 |
| 命令执行路径（4MB spec + mock API） | 每条命令解析 spec：**147.0ms cold** | 可索引 operation **warm 11.4ms（12.9×）**，跳过完整解析；不可索引安全回退 |
| 本地 spec 被删除 | 命令报错 | 已建索引的可索引命令**仍可执行**（新增离线语义） |
| `pkg/config` | 含 680 行无 CLI 消费者的 profile 导出/导入 API | 删除 **1,934 行**（export/profile API + tests + 连带引用），无行为变化 |
| `pkg/config` 拆分 | 无设计 | `proposals/03_config_split.md`（346 行）：S1–S5 分步计划、实测依赖矩阵、风险与 breaking 策略 |
| DCO | 分支含 2 个历史未签名提交（PR DCO 必红） | **32/32 全部 Signed-off-by**（`git rebase --signoff` + force-with-lease，已推送；本地备份分支保留） |
| 覆盖率 | 73.7% | **75.2%** |
| 测试 | 696 | **683**（净减少来自删除死 API 的 21+ 测试；新增 typed/execution 测试 17+ 个） |

## 2. 工作流与交付

### W1 错误模型 typed 化（style-auditor）—— 架构 F-22/F-23

- 新增 `pkg/cli/errtypes.go`：`UnknownResourceError{Resource}`、`UnknownVerbError{Resource,Verb}`、`SpecLoadError{Source,Err}`（`Error()`/`Unwrap()`，文本保持原可读形式）+ 私有 `displayError`（富文本 + Unwrap typed，保证 CLI 富消息逐字保留）。
- `pkg/request` 新增 `MissingParameterError{Name,BodyField}`、`ParameterValidationError{Parameter,Reason}`；`checkRequiredParams`/`checkRequiredBodyFields`/`validateParameterType`/`validateEnumValue` 全部迁移，`Error()` 文本逐字不变。
- `pkg/cli/errors.go`：**删除 5 处 `strings.Contains`**，改 `errors.As` 链（AppNotFound/url/net → UnknownResource → UnknownVerb → MissingParameter → ParameterValidation → SpecLoad）；`format*` 从 typed 字段取值，不再 `Split` 字符串。
- Producer 迁移：`handler.go` 的 `resolveOperation`/help/exec spec-load、`router.go`、`internal/app/commands.go`。
- **文本等价证据**：实现前用独立程序捕获批次 3 的 7 类 formatter 输出作为 golden；迁移后（含 `%w` wrapped）逐字相等；Lead e2e 对 13 条错误/正常命令与基线二进制 `stdout+stderr+exit code` 全一致。
- `PrintedError.Error()` 返回底层文本，main 的"已打印不重复打印"行为由测试锁定。

### W2 执行路径索引 P4 POC（perf-analyst）—— D-05

- DTO v3（`DerivedIndexSchemaVersion` 2→3）：`ExecutionIndexable` + `ExecutionBlockReason`；执行所需参数/body 元数据沿用 v2 字段；新增 `SemanticOperation()` 与 "源文件删除后按稳定 identity 读取索引"（`loadIdentity`）。
- `pkg/cli/handler.go`：`resolveOperationSpec` 优先 `LoadDerivedIndex + FindOperation`；`ExecutionIndexable` 时由索引重建 `openapi3.Operation` + `semantic.Operation`，**不调用 `LoadSpec`**，随后走现有 Validate/BuildRequest/execute；索引缺失/失配/不可索引 → 原解析路径。
- 可索引边界（安全回退）：标量参数/body 字段 + 字符串数组；嵌套 object、array-of-object、allOf/oneOf/anyOf、无 type、非 JSON/多 media type、additionalProperties、未解析引用、parameter content schema → `ExecutionIndexable=false` 并记录原因。
- **等价证据**：query/path/body（标量+字符串数组）/PATCH body 四类命令 cold(解析) vs warm(索引) 的 method/path/query/body/Content-Type 逐项一致、stdout 一致、无 spec 缓存；缺必填/枚举非法错误文本一致；建索引后删除 spec 文件命令仍成功；不可索引命令按原语义回退。
- **基准（mock API）**：

| spec | cold（解析） | warm（索引） | 加速 |
| --- | ---: | ---: | ---: |
| 1MB / 350 ops | 42.2ms | **10.3ms** | 4.1× |
| 4.09MB / 1,300 ops | 133.3ms | **10.9ms** | 12.3× |
| 真实 `ob run`（4.09MB） | 147.0ms | **11.4ms**（min 10.7） | 12.9× |
| Lead e2e（1.0MiB / 900 ops） | 109.8ms | **12.8ms** | 8.6× |

可索引比例：基准 spec 100%（1MB/350、4.09MB/1300、2.6k ops、enum 840 ops）；混合 fixture 4/5（嵌套 body 回退）。

### W3 `pkg/config` 死 API 清理（process-auditor）—— 架构 F-42

- 删除 `export.go`（680）+ `export_test.go`（646）+ `export_property_test.go`（161）；`config.go` −156（`BuildSpecFetchOptions`/`ExportProfile`/`ProfileExport`/`ImportProfile` 等）；`config_test.go` −189；`test_helpers.go` −32；跨作用域必要清理 `internal/integration/integration_test.go` −70（`TestProfileImportExport`，已获 Lead 批准）。
- **合计 −1,934 行 / 0 新增**；引用 grep（`ExportProfile|ImportProfile|BuildSpecFetchOptions`）为空；行为零变化；全量 short + integration 全绿。
- 报告项（批次 5）：`profile.go` 的 ProfileManager 家族生产 0 调用（约 400 行级），建议连同测试清理（已写入 proposals/03 S3）。

### W4 `pkg/config` 拆分设计提案（arch-reviewer）

`proposals/03_config_split.md`（346 行）：
- 现状实测：config 非测试 2,625 行 / AST 导出 93（config.go 778/39、install.go 959/13、path_utils.go 261/10、profile.go 581/31 且生产 0 引用）；外部生产仅引用 16 个符号；无单例/init，仅 2 个不可变正则/集合变量。
- 推荐结构：`pkg/config` 收敛为领域模型 + Manager（~800 行/39 导出/叶子）；`internal/install` 承接安装/shim/PATH/prompt/TLS 校验/DTO（~1,220 行）；TUI 内化；删 `profile.go`。
- 分步 S0–S5（约 4–5.5 人日 / 5 PR），每步含依赖断言、e2e install/-i/shim、双 tag vet/lint；最大风险为公开 API breaking（建议 v0.2.0 + CHANGELOG 迁移表）。

### W5 Lead 走线

- **DCO 修复**：`git rebase --signoff main` 重写 32 个提交（树内容不变，`git diff` 与备份分支为空），`--force-with-lease` 推送；本地备份分支 `backup/pre-dco-rewrite-20261107` 保留。
- `ENGINEERING-DIAGNOSIS.md` 新增附录 D：P0/P1 发现 → 修复批次映射、指标前后对比、未闭环项。
- 全量门禁、四批 e2e 回归、提交与推送。

## 3. 验收证据（Lead 最终执行）

| # | 检查 | 结果 |
| :---: | --- | --- |
| 1 | `go build ./...` | ✅ |
| 2 | `gofmt -l` / `goimports -l` | ✅ 0 / 0 |
| 3 | `go vet ./...` + `go vet -tags=integration ./...` | ✅ |
| 4 | `go test -short -count=1 ./...` | ✅ 14 包 |
| 5 | `go test -race -short -count=1 ./...` | ✅ 14 包 |
| 6 | `go test -tags=integration -count=1 ./...` | ✅ 15 包 |
| 7 | `golangci-lint v2.15.0 run ./...` | ✅ **0 issues** |
| 8 | `govulncheck ./...` | ✅ **0 可达漏洞** |
| 9 | `go mod tidy -diff` | ✅ 空 |
| 10 | 覆盖率 | ✅ **75.2%**（批次 3：73.7%） |
| 11 | `npm run docs:build` | ✅ |
| 12 | `actionlint v1.7.12` | ✅ exit 0 |
| 13 | 批次 4 e2e（`/tmp/lead_verify_b4.py`，22 断言） | ✅ **22/22 PASS** |
| 14 | 批次 3 回归（19 断言） | ✅ 19/19 PASS |
| 15 | 批次 2 回归（20 断言） | ✅ 20/20 PASS |
| 16 | 批次 1 回归（18 断言） | ✅ 18/18 PASS |
| 17 | 批次 0 回归（22 断言） | ✅ 22/22 PASS |

### 3.1 批次 4 e2e 关键输出

```
PASS A equivalent: info noapp / bogusresource / bogusverb / missing required_q / invalid enum /
                  missing body field / unknown profile / badflag / list / cmd-not-exist / spec load error  (13/13)
PASS B removed export.go/export_test.go/export_property_test.go
PASS B no dead API references
PASS C1 fast path faster than parse path | baseline=109.8ms current=12.8ms
PASS C2 output identical
PASS C3 requests identical | requests=8
PASS C4 fast path skips parsing | (spec tampered with same size+mtime, command still succeeds)
```

## 4. 行为变更与兼容性

| 变更 | 影响 | 兼容性 |
| --- | --- | --- |
| 错误分类改 typed error | 用户可见错误文本不变（golden 逐字）；程序可用 `errors.Is/As` 判定 | 内部 API 变化；CLI 输出兼容 |
| `PrintedError.Error()` 返回文本 | 日志/遥测可拿到错误内容；main 仍不重复打印 | CLI 输出不变 |
| 执行路径 fast path | 可索引 operation 免解析；输出/请求字节一致 | 不可索引自动回退；索引版本升级首跑重建 |
| 本地 spec 删除后 indexable 命令仍可执行 | 新增离线语义 | 属增强；不可索引命令仍报错 |
| 删除 `pkg/config` 导出/导入 profile API | 仓库内生产零调用；外部使用者会 breaking | 属死 API 清理；建议 v0.2.0 说明 |
| DCO 历史重写 | 分支提交 hash 变化（内容不变） | 已 force-push；本地备份分支保留 |

## 5. 遗留项（批次 5 候选）

| 项 | 依据 | 说明 |
| --- | --- | --- |
| `pkg/config` 拆分 S1–S5 | proposals/03 | 安装流程→internal/install、TLS 下沉、删 profile 死 API、TUI 内化 |
| 派生索引 P4 扩展 | W2 风险 | 嵌套 object/array-of-object/oneOf 等 schema 目前回退；视真实 API 形态决定是否扩展 |
| 索引 DTO body 字段顺序 | 既有风险 | formatter 与解析路径同样受 map 顺序影响 |
| `--flag value<TAB>` 空格形式补全 | 批次 3 风险 | 当前支持 `--flag <TAB>` 与 `--flag=` |
| `go list -m all` ghodss/yaml.v2 module graph 遗留 | 批次 3 | 需升级 viper/etcd 旧链路 |
| `internal/app/commands.go` 1,050 行 | 批次 3 风险 | 可按 install/query/mcp 拆文件 |
| 覆盖率进一步提升 | — | 当前 75.2%；`internal/app`/`pkg/cli` 命令层仍有空间 |

## 6. 复现验收

```bash
cd /Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge
go build ./... && go vet ./... && go vet -tags=integration ./...
gofmt -l pkg cmd internal && goimports -l pkg cmd internal
go test -short -count=1 ./... && go test -race -short -count=1 ./...
go test -tags=integration -count=1 ./...
go mod tidy -diff
/tmp/gobin2/golangci-lint run ./... && /tmp/gobin2/govulncheck ./...
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/*.yml
python3 /tmp/lead_verify_b4.py   # 22 断言（批次 4）
python3 /tmp/lead_verify_b3.py   # 19 断言（批次 3 回归）
python3 /tmp/lead_verify_b2.py   # 20 断言（批次 2 回归）
python3 /tmp/lead_verify_b1.py   # 18 断言（批次 1 回归）
python3 /tmp/lead_verify.py      # 22 断言（批次 0 回归）
npm run docs:build
```

> 结论：批次 4 完成错误模型 typed 化（用户可见文本零变化）、执行路径索引 P4 POC（4MB spec 命令 147ms→11.4ms，跳过解析并支持离线）、`pkg/config` 死 API 清理（−1,934 行）与拆分设计提案；DCO 历史已重写修复（32/32 签名）；覆盖率 73.7%→75.2%，四批 e2e 回归全部通过。
