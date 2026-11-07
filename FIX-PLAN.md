# OpenBridge 修复计划 —— 批次 0（热修）

> 上游依据：[`ENGINEERING-DIAGNOSIS.md`](./ENGINEERING-DIAGNOSIS.md) §9 批次 0；详报见 `.diagnosis/`
> 基线：`dd18374`（工作树，修复前 `git status` 仅诊断报告未跟踪）
> 原则：**TDD（先写失败测试→实现→转绿）**、最小改动、不引入新依赖、不修改 `.golangci.yml`、遵守 AGENTS.md 的测试命名与 `%w` 包装规范
> 本轮不做：批次 1 的结构性重构（真实持久化缓存、profile 统一装配层、TLS/Retry 接线、MCP 限流实现、`init()` 组合根重构、`pkg/config` 拆分）——留待下一批

---

## 1. 目标与范围

修复诊断报告中用户可直接触发的 P0/P1 缺陷，并恢复质量门禁的可执行性：

| 编号 | 缺陷 | 诊断 ID | 负责人 | 验收 |
| :---: | --- | --- | --- | --- |
| A0-1 | CLI 认证全链路失效 | D-01 | cli-flags（调用点）+ credential-sec（InjectAuth 语义/keyring） | 单测断言 CLI 请求携带 Authorization；keyring 后端错误不再被吞 |
| A0-2 | 凭据外泄三路径 | D-02/03/04 | credential-sec（错误 URL + codegen）+ routing-info（ob info） | 网络错误不回显 query 值；codegen URL 掩码；info JSON/YAML 掩码 Header |
| A0-3 | `--json/--yaml/--output/-o` 静默无效 | D-07 | cli-flags | `--json` 输出合法 JSON；`--output yaml` 输出 YAML；默认 YAML |
| A0-3b | `--profile/-p` 静默无效、profile Headers/QueryParams 在 CLI 不生效 | D-08（部分） | cli-flags | 指定 profile 生效；不存在的 profile 报错；Headers/QueryParams 进入请求 |
| A0-4 | `ob <app> ...` 直连路由不可达 | D-11 | routing-info | `ob <app> --help` / `ob <app> <res> <verb>` / `ob <app> --mcp` 均可达 |
| A0-5 | 集成测试 tag / 重名 / CI 门禁 / Makefile | D-13/14 | test-infra | `go vet -tags=integration ./...` 通过；CI 增加 vet + integration 步骤；lint 版本 pin；`make install` 可用 |
| A0-6 | HTTP 全部无超时 | D-10 | cli-flags（CLI）+ test-infra（MCP） | 三处 client 均设 Timeout；慢服务不再永久挂起 |
| A0-7 | keyring 静默回退 + 固定口令 | D-12 | credential-sec | 无系统后端且未提供口令时明确报错；支持 `OPENBRIDGE_KEYRING_PASSWORD` 显式启用文件后端 |
| A0-8 | 文档与实现漂移 | 诊断 §7.5 | Lead | `ob version`→`ob --version`、输出默认/选项、Zero-Code-Gen 措辞、冷启动表述 |

## 2. 任务分派与写作用域（避免冲突）

| 任务 | 成员 | 写作用域（advisory） | 不碰 |
| --- | --- | --- | --- |
| task-5 安全/凭据修复 | credential-sec（新） | `pkg/request/builder.go`、`pkg/request/builder_test.go`、`pkg/cli/errors.go`、`pkg/cli/errors_test.go`、`pkg/codegen/*.go`、`pkg/credential/keyring.go`、`pkg/credential/keyring_fallback_test.go`（新） | 不改 `pkg/cli/handler.go`、`cmd/ob/main.go`、`pkg/mcp/*`、`.golangci.yml`、`go.mod` |
| task-6 CLI 语义修复 | style-auditor | `pkg/cli/handler.go`、`pkg/cli/handler_test.go`、`pkg/cli/cli_options_test.go`（新）、`pkg/cli/args.go`（仅在必要时） | 不改 `pkg/cli/errors.go`、`pkg/request/*`、`cmd/ob/main.go` |
| task-7 路由与 info 脱敏 | arch-reviewer | `cmd/ob/main.go`、`cmd/ob/main_test.go`、`pkg/router/router.go`、`pkg/router/router_test.go`（新） | 不改 `pkg/cli/*`、`pkg/config/*` |
| task-8 测试基建/CI/MCP 超时 | process-auditor | `pkg/*/*_integration_test.go`、`internal/integration/*_test.go`、`.github/workflows/ci.yml`、`Makefile`、`pkg/mcp/handler.go`、`pkg/mcp/progressive.go`、`pkg/mcp/handler_test.go` | 不改 `pkg/credential/keyring_test.go`（仅改 `keyring_integration_test.go`）、不改业务逻辑 |
| task-9 独立验收 | perf-analyst | `/tmp/verify_batch0/` + `.diagnosis/verification-batch0.md` | 不修改仓库源码 |
| A0-8 文档 | Lead | `README.md`、`QUICKSTART.md`、`docs/reference/cli.md`、`docs/guide/*.md`、`docs/zh/**`、`FIX-PLAN.md`/`FIXES-BATCH0.md` | 不改 Go 源码 |

## 3. 接口契约（跨任务必须先对齐）

### C1 `request.Builder.InjectAuth`（credential-sec 实现，cli-flags 调用）
签名保持不变：
```go
func (b *Builder) InjectAuth(req *http.Request, appName, profileName string, authConfig *config.AuthConfig) error
```
语义：
1. `b.credMgr == nil` → `nil`；
2. `authConfig == nil` 或 `authConfig.Type` 为 `""`/`"none"` → `nil`（**不触碰 keyring**）；
3. `GetCredential` 返回 `*credential.CredentialNotFoundError` → `nil`（未配置凭据是合法状态，保持静默）；
4. 其他错误 → `fmt.Errorf("failed to load credential for %s/%s: %w", appName, profileName, err)`；
5. 成功 → 按 auth 类型注入。

### C2 CLI 选项解析（cli-flags）
- 新增（包内未导出）：
```go
type cliOptions struct { outputFormat, profileName, generateFormat, generateOutput string }
func parseCLIOptions(args []string) cliOptions   // 支持 --output/-o、--json、--yaml、--profile/-p、--generate/-g、--generate-output/-O
func (h *Handler) getProfile(appConfig *config.AppConfig, name string) (*config.Profile, error) // name 为空→DefaultProfile；不存在→报错并列出可用
```
- 输出格式取值优先级：`--output` > `--json`/`--yaml` > 默认 `"yaml"`；"table" 不再作为隐式默认（文档同步为 YAML）。
- `Handler` 增加 `stdout io.Writer`（默认 `os.Stdout`）以便测试。
- CLI 请求在 `BuildRequest` 后应用 `profile.Headers` 与 `profile.QueryParams`（MCP 已有 Headers 逻辑，保持行为一致）。
- HTTP client：`&http.Client{Timeout: 30 * time.Second}`。
- `buildRequest`/`generateCode`/`executeAPIRequest` 使用真实 `appName` 调用 `InjectAuth`。

### C3 路由（routing-info）
```go
// pkg/router/router.go
func (r *Router) ExecuteApp(appName string, args []string) error // 显式 app 分发：加载配置→MCP 判定→CLI
```
- `cmd/ob/main.go`：`rootCmd.Args = cobra.ArbitraryArgs`；`RunE` 在 `configMgr.AppExists(args[0])` 时调用 `appRouter.ExecuteApp(args[0], args[1:])`，否则报 "unknown command or app"。
- 删除 `Router.handleOBCommand` 与不可达分支；保留 shim 模式 `Execute`（基于 argv[0]）。
- `ob info -o json/yaml` 前对 `AppConfig.Profiles[*].Headers` / `SpecFetchHeaders` 做深拷贝掩码（复用 `isSensitiveHeader`/`maskValue`）。
- 新增 `pkg/router/router_test.go`（首次为该包建立测试）。

### C4 测试基建（test-infra）
- 为 `pkg/cli/errors_integration_test.go`、`pkg/config/install_integration_test.go`、`pkg/request/builder_integration_test.go`、`internal/integration/{integration,real_api}_test.go` 顶部加 `//go:build integration`。
- 重命名 `pkg/credential/keyring_integration_test.go` 与 `pkg/credential/keyring_test.go` 冲突的函数（如 `TestGetPlatformInfo`→`TestIntegrationGetPlatformInfo`、`TestWithPassBackend`→`TestIntegrationWithPassBackend`），只改 integration 文件。
- `.github/workflows/ci.yml`：新增 `go vet ./...`、`go vet -tags=integration ./...`、`go test -tags=integration ./...`；`golangci-lint` 固定 `v2.8.0`；保留 3 OS 矩阵。
- `Makefile`：`GOLANGCI_LINT_VERSION ?= v2.8.0`；`install` 改用 `go install`/`go env GOPATH`；新增 `vet`、`test-integration` 目标；同步 `help`。
- MCP：`pkg/mcp/handler.go`、`pkg/mcp/progressive.go` 的 `http.Client` 统一 `Timeout: 30s`（与 CLI/parser 一致）。

### C5 keyring（credential-sec）
- 默认**不再**使用固定口令回退：`getPasswordFunc` 无显式配置时返回错误（说明如何配置）。
- 支持显式文件后端：`OPENBRIDGE_KEYRING_PASSWORD` 存在时使用 `FileBackend`（口令来自环境变量），供 headless/CI/e2e 使用；后端目录沿用现有 `getDefaultKeyringDir()`。
- `Manager.Backend()` 必须反映真实后端（系统 keychain / file），不得按平台猜测。
- 现有 `WithFileBackend`/`WithPassBackend` 选项行为保持兼容。

## 4. 验收矩阵（Lead 最终执行）

| 检查 | 命令 | 通过标准 |
| --- | --- | --- |
| 编译 | `go build ./...` | 退出码 0 |
| 格式 | `gofmt -l pkg cmd internal`、`goimports -l pkg cmd internal` | 无输出 |
| 静态检查 | `go vet ./...`、`go vet -tags=integration ./...` | 无输出 |
| 单元测试 | `go test -short ./...` | 全绿 |
| Race | `go test -race -short ./...` | 全绿 |
| 集成编译+运行 | `go test -tags=integration ./pkg/... ./internal/...` | 通过或按设计 skip（缺后端） |
| e2e | `/tmp/verify_batch0/verify.py`（独立验收任务产出） | 全部 PASS |
| 手工 | 隔离 HOME 下 install/run/info/route/MCP 冒烟 | 见诊断报告 §2 场景 |
| 文档 | README/QUICKSTART/docs 命令逐条可执行 | 无失效命令 |

## 5. 风险与回滚

| 风险 | 缓解 |
| --- | --- |
| 并行改同一 Go 包导致中间态编译失败 | 文件级写作用域隔离；各成员只跑自己包的单测；Lead 最终统一验证 |
| 给 integration 测试加 tag 后默认套件"变绿但少测" | CI 新增 `-tags=integration` 步骤；`go vet -tags=integration` 纳入门禁 |
| keyring 行为变更影响现有安装流程 | 保留 `WithFileBackend` 兼容；系统后端可用时行为不变；新增 fallback 测试 |
| 路由修复与 cobra 行为冲突 | 只改 `rootCmd.Args` + RunE，不重写命令树；`pkg/router/router_test.go` 覆盖 |
| 凭据脱敏误伤正常 URL 显示 | 只掩码 query 值（保留 key 名）与 userinfo；错误信息保留主机/路径 |
| 输出格式修复破坏脚本兼容 | 默认仍是 YAML（与现状一致），仅让显式开关生效 |

## 6. 执行顺序

1. 并行：task-5/6/7/8（代码）+ Lead 文档（A0-8）
2. 独立验收：task-9 在代码就绪后跑 e2e + 回归
3. Lead 汇总修复：全量测试、`gofmt/goimports/vet`、审阅 diff、补漏
4. 产出 `FIXES-BATCH0.md`（变更清单 + 测试证据 + 遗留项）


---

## 7. 执行结果（2026-11-07，批次 0 完成）

| 项 | 状态 | 证据 |
| --- | :---: | --- |
| A0-1 CLI 认证 | ✅ | 独立验收 S7 PASS；`pkg/request/builder_test.go`、`pkg/cli/handler_test.go` |
| A0-2 凭据脱敏（三路径） | ✅ | 独立验收 S5/S6/S8 PASS；`pkg/cli/errors_test.go`、`pkg/codegen/codegen_test.go`、`cmd/ob/main_test.go` |
| A0-3 输出格式 | ✅ | 独立验收 S2 PASS；`pkg/cli/cli_options_test.go` |
| A0-3b profile 选择/Headers | ✅ | 独立验收 S3 PASS |
| A0-4 `ob <app>` 直连路由 | ✅ | 独立验收 S4 PASS；`pkg/router/router_test.go` |
| A0-5 集成测试/CI/Makefile | ✅ | `go vet -tags=integration`、`go test -tags=integration ./...` 全绿；CI/Makefile diff |
| A0-6 HTTP 超时（CLI+MCP） | ✅ | `handler_test.go` 断言；`go test ./pkg/mcp` |
| A0-7 keyring 回退安全 | ✅ | `pkg/credential/keyring_fallback_test.go`；独立验收 S7（Backend=file） |
| A0-8 文档同步 | ✅ | 10 文件；`npm run docs:build` 通过 |
| Lead 终验 | ✅ | 36 files / +1119−178；51 新增测试；覆盖率 55.3%→59.2%；lint 0 issues；e2e 22/22 |
| 独立验收 | ✅ | 10/10 PASS，建议 ACCEPT（`.diagnosis/verification-batch0.md`） |

交付汇总见 [`FIXES-BATCH0.md`](./FIXES-BATCH0.md)。批次 1 候选（持久化缓存、profile TLS/Timeout/Retry 接线、MCP 安全控制、错误模型/组合根重构、覆盖率与安全门禁）见该报告 §7。
