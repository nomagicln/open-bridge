# OpenBridge 修复计划 —— 批次 3（索引 P3、组合根、工程卫生、依赖治理）

> 上游：[`FIXES-BATCH2.md`](./FIXES-BATCH2.md) §5、[`ENGINEERING-DIAGNOSIS.md`](./ENGINEERING-DIAGNOSIS.md)（F-02/F-18/F-22）
> 基线：批次 2 最新提交 `c2b103f`（已推送）；工作树干净
> 原则：TDD、契约先行、文件级写作用域隔离、不修改 `.golangci.yml`、不新增第三方依赖、DCO 签名提交
> 本轮**不做**（批次 4）：`pkg/config` 拆分、错误模型 sentinel 化、派生索引 P4（执行路径）、`init()` 之外的 DI 全面接口化

---

## 1. 范围与负责人

| 工作流 | 内容 | 对应 | 负责人 | 写作用域 |
| --- | --- | --- | --- | --- |
| **W1 派生索引 P3 + 裁剪** | 索引 DTO 扩展 operation 参数/请求体元数据（name/in/required/type/enum/body 属性）；命令级 `--help` 与 flag/enum 补全走索引；Description 裁剪（当前占 67% 体积）；DTD schemaVersion+1 | F-18 / FIXES-BATCH2 §5 | perf-analyst | `pkg/spec/derived_index*.go`、`pkg/spec/parser.go`、`pkg/cli/handler.go`、`pkg/cli/handler_test.go`、`pkg/completion/*`、`pkg/cli/args.go`（仅如需） |
| **W2 组合根重构** | 新增 `internal/app`：`Deps` 结构 + `New/ Run`，把 `cmd/ob/main.go` 的 `init()` 全局依赖改为显式构造；main.go 收敛为 main/Execute；命令构建函数改为方法/传入依赖；**行为与 CLI 输出零变化** | 架构 F-02 | arch-reviewer | `cmd/ob/main.go`、`cmd/ob/main_test.go`、新增 `internal/app/*.go`（+测试） |
| **W3 工程卫生与门禁** | codecov floor 70% / patch 75%；新增 SECURITY.md、CODEOWNERS、.editorconfig；package.json 修正（去掉不存在的 main、private/engines）；Node 20→22（deploy-docs）；CI 增加 actionlint 与 docs build 预检；Makefile 增加 `bench` 目标 | F-18 / D-16 / 工程卫生 | process-auditor | `.github/workflows/ci.yml`、`.github/workflows/deploy-docs.yml`、`codecov.yml`、`Makefile`、`package.json`、`SECURITY.md`（新）、`CODEOWNERS`（新）、`.editorconfig`（新） |
| **W4 依赖治理** | `pkg/spec/format.go` 用 `gopkg.in/yaml.v3` 替换归档的 `github.com/ghodss/yaml`（保持 JSON/YAML 语义与测试不变）；更新 go.mod/go.sum，确认 `yaml.v2` 间接依赖消失 | 风格 F-18 | process-auditor（与 W3 同人不同文件，可串行） | `pkg/spec/format.go`、`pkg/spec/format_test.go`、`go.mod`、`go.sum` |
| **W5 汇总/走线** | 组合根与索引接线终验、文档、全量门禁、e2e 回归、DCO 提交、交付报告 | — | Lead | `FIX-PLAN-BATCH3.md`、`FIXES-BATCH3.md`、docs 相关、main.go 最终整合 |

> W3/W4 同属 process-auditor 的不同目录/文件，可串行执行。

## 2. 契约

### C-D1（W1）派生索引 P3
- DTO 扩展（`DerivedIndexSchemaVersion` → 2）：
  - `DerivedOperation` 增加 `Parameters []DerivedParameter{Name,In,Required,Type,Enum}`、`BodyFields []DerivedField{Name,Required,Type}`、`Method/Path/OperationID/Summary/Tags` 已有；
  - Description 裁剪：索引中仅保留最多 200 字符（帮助命令级输出不需要全文），DTO 体积目标 ≤ 源 spec 30%。
- 命令级 help：`showCommandHelp` 等路径优先用索引渲染（参数/枚举/请求体字段），索引缺失/失配回退解析；输出与解析路径**逐字节一致**。
- flag/enum 补全：`completion` 的 flag 值与枚举补全优先走索引，缺失回退解析。
- 验收基准：4.09MB spec 上 `ob <app> <res> <verb> --help` 与 flag 补全前后耗时（目标 ≤20ms 级），索引体积对比（裁剪前 2.61MiB → 目标下降）。
- 不改变帮助文本内容、退出码；不新增依赖。

### C-D2（W2）组合根
- 新增 `internal/app`：
```go
type Deps struct { Config *config.Manager; Parser *spec.Parser; Credential *credential.Manager; Mapper *semantic.Mapper; Builder *request.Builder; CLI *cli.Handler; MCP *mcp.Handler; Router *router.Router; Completion *completion.Provider }
func New() (*Deps, error)   // 显式构造，失败策略与现状一致（config fatal / credential degrade）
func Run(args []string) error
```
- `cmd/ob/main.go`：删除包级依赖变量与 `init()`，`main()` 构造 Deps 后调用 `app.Run`；所有 `new*Cmd` 从全局依赖改为经 `Deps`/app 方法访问；`appRouter`/`configMgr` 等引用全部收敛。
- 保持行为：`ob`/shim/直连/install/list/info/uninstall/completion/MCP 参数与输出不变；`main_test.go` 改为可注入 Deps 的测试（不再依赖全局变量）。
- 验收：`go build ./...` + 全量测试；`ob --help`/`ob <app> --help`/`ob install` 等 e2e 输出与批次 2 一致。

### C-D3（W3）工程卫生
- `codecov.yml`：project floor 70%（threshold 1%）、patch 75%。
- `SECURITY.md`（漏洞报告流程）、`CODEOWNERS`（默认 @nomagicln）、`.editorconfig`（Go/MD/YAML 风格）。
- `package.json`：删除 `main: index.js`、`description/keywords/author` 补全、`"private": true`、`engines.node >= 22`；`deploy-docs.yml` Node 22。
- `Makefile`：新增 `bench`（`go test -run XXX -bench . ./pkg/spec/...` 之类，非阻塞）。
- CI：新增 `actionlint` 步骤（pin 版本）；docs build 预检（PR 且 docs 变更时 `npm ci && npm run docs:build`）；不改现有 lint/test/build 逻辑。
- 验收：workflow YAML 通过 actionlint（本机可 `go run github.com/rhysd/actionlint/cmd/actionlint@...` 或 `brew` 已有则用）；`make -n bench`；`npm ci && npm run docs:build`（模拟 docs job）。

### C-D4（W4）依赖治理
- `pkg/spec/format.go`：`ghodssyaml.Unmarshal`（YAML→JSON→json）替换为 `yaml.v3` 直接解码/转换为等价 JSON 语义；保持 `UnmarshalStrategy` 接口与现有测试/属性测试通过。
- 更新 go.mod/go.sum（移除 `github.com/ghodss/yaml`，确认 `gopkg.in/yaml.v2` 不再需要）；`go mod tidy` 由 Lead 复核或 W4 直接执行（唯一依赖改动者）。
- 验收：`go test ./pkg/spec`（含 property test）；`go mod tidy -diff` 空；`go list -m all | grep ghodss` 无输出。

## 3. 验收矩阵

| 检查 | 命令 | 标准 |
| --- | --- | --- |
| 构建/格式 | `go build ./...`、gofmt/goimports | 0 |
| 静态 | `go vet ./...`、`go vet -tags=integration ./...` | 0 |
| 单测/竞态/集成 | `go test -short`、`go test -race -short`、`go test -tags=integration` | 全绿 |
| Lint | `/tmp/gobin2/golangci-lint run ./...` | 0 issues |
| 漏洞/依赖 | govulncheck；`go mod tidy -diff` | 0 可达；无 diff |
| 覆盖率 | `go test -short -coverprofile` | ≥71.9% |
| 性能 | help/flag 补全前后对比 | ≤20ms 级；索引体积下降 |
| e2e | 批次 0（22）/1（18）/2（20）+ 批次 3 新增 | 全部 PASS |
| 文档/工作流 | `npm run docs:build`；actionlint | 通过 |

## 4. 冲突规避

| 风险 | 缓解 |
| --- | --- |
| W1 与 W2 都涉及 CLI 行为 | W1 只改 help/补全路径；W2 只改 main.go/组合根，不碰 pkg/cli |
| W2 改 main.go 与 W1 flag 补全接线 | 补全经 Parser.LoadDerivedIndex 内部完成，不改 main 接线 |
| W3 与 W4 同人不同文件 | 串行执行，互不影响 |
| W4 与 W1 同包 | W4 仅 `format.go`；W1 不碰 format.go |
| 并行中间态 | 各跑自己包测试；Lead 终验 |

## 5. 执行顺序

1. W1/W2 并行；process-auditor 串行 W3→W4
2. Lead：终验 + e2e 回归 + 文档
3. DCO 分组提交 + 推送 + `FIXES-BATCH3.md`


---

## 6. 执行结果（2026-11-07，批次 3 完成）

| 工作流 | 状态 | 关键证据 |
| --- | :---: | --- |
| W1 派生索引 P3 + 裁剪 | ✅ | 命令级 help 148.6→11.5ms（Lead e2e 116.6→11.0ms）、flag 名 135.5→9.7ms、枚举值 83.4→10.1ms；索引 2.74MB→517KB（−81%）；输出逐字节一致 |
| W1c flag 值补全接线 | ✅ | `--flag <TAB>`/`--flag=prefix` 返回枚举值，8 个新测试；此前只返回 flag 名 |
| W2 组合根重构 | ✅ | main.go 1,145→37 行；internal/app.Deps 显式构造；21/21 场景输出+退出码与基线一致 |
| W3 工程卫生 | ✅ | codecov floor 70%/patch 75%、SECURITY/CODEOWNERS/.editorconfig、actionlint、docs PR 预检、make bench、Node 22 |
| W4 依赖治理 | ✅ | ghodss/yaml→yaml.v3（44 例矩阵等价）、yaml.v2 移除；tidy diff 空；构建图无两者 |
| 全量门禁 | ✅ | build/vet 双 tag/short(14)/race(14)/integration(15)/lint(0)/govulncheck(0)/tidy/actionlint/docs；覆盖率 73.7% |
| e2e 回归 | ✅ | 批次 3 19/19、批次 2 20/20、批次 1 18/18、批次 0 22/22 全部 PASS |

交付汇总见 [`FIXES-BATCH3.md`](./FIXES-BATCH3.md)。批次 4 候选见该报告 §5（派生索引 P4、pkg/config 拆分、错误模型 sentinel 化、ghodss 旧链路升级等）。
