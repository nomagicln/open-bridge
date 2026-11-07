# OpenBridge 测试与工程流程诊断报告（含安全审查）

> 任务：task-4 — 测试与工程流程诊断（覆盖率/测试质量/AGENTS.md 合规/CI/Makefile/安全/发布/文档/可观测性）
> 评审对象：`/Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge`
> 修订：`dd18374 fix: resolve linting issues and improve code quality`（分支 `fix/install-and-args`）
> 评审环境：macOS arm64，本机 Go `go1.27.3`，`golangci-lint v1.64.8`，`goreleaser v2.13.3`，`node v24.22.2 / npm 11.19.1`
> 评审方式：只读静态审查 + 只读/只写 `/tmp` 的实测命令；除本报告外未修改任何文件；未执行 git 写操作
> 产出：仅本文件 `.diagnosis/process.md`

---

## 0. 执行摘要

### 0.1 总体结论

| 维度 | 结论 | 最高严重度 |
| --- | --- | --- |
| 覆盖率 | 总量 **55.4%（-short）/ 56.5%（full）**；988 个函数中 **298 个 0.0%**；`pkg/tui/install`（603 语句/72 函数）与 `pkg/router`（86 语句/12 函数）为 **0%**；`cmd/ob` 19.0%、`pkg/cli` 31.5% | P1 |
| 测试质量 | 数量充足（535 个 Test 函数、17,527 行）但**结构性偏斜**：`t.Parallel` 全仓 0 处、30 处 `t.Skip`、13 处 `time.Sleep`；7 个 property test 在 AGENTS.md 指定的 `-short` 门禁下**全部跳过**；`pkg/spec`（parser/formatter）**没有任何 property test** | P1 |
| AGENTS.md 合规 | **5 个 `*_integration_test.go` 只有 1 个带 build tag**；唯一带 tag 的文件与被 tag 排除的文件存在**重名函数 → `-tags=integration` 直接编译失败**；孤儿测试包 `internal/integration`；`pkg/router`/`pkg/tui/install` 无测试 | P1 |
| CI | 无 `govulncheck`/CodeQL/Dependabot/许可证检查；`golangci-lint version: latest`；coverage 仅 ubuntu 上传且无门禁；integration tag 测试从未在 CI 运行；手动 `actions/cache` 路径对 macOS/Windows 无效；Node 20 已 EOL | P2 |
| Makefile | **`make install` 在本机直接失败**（GOPATH 环境变量为空）；`GOLANGCI_LINT_VERSION=latest`；`fmt-fix` 盲目 `gofmt -r` 全树重写；`docs` 目标仍依赖已淘汰的 `godoc`；`clean` 不完整 | P2 |
| 安全 | **已确认 3 条凭据外泄路径**（网络错误回显 query 中的 API key、codegen 输出未掩码 URL、`ob info -o json/yaml` 未掩码 Header）；keyring 在无系统后端时用**硬编码口令**落盘；`gosec` 被禁用；TLS 配置字段**完全未接线**（无 `crypto/tls` 引用）；config/cache 权限 0644 | **P1（已确认漏洞）** |
| 发布/仓库卫生 | `goreleaser check` 通过但**无签名/SBOM/公证**；`before: go mod tidy` 非密闭；README “Zero Code Generation” 与 `pkg/codegen` + `--generate` 矛盾；`ob version` 文档存在但命令不存在；`package.json` 的 `main: index.js` 不存在、无 `engines` | P2 |
| 可观测性 | 生产代码**零日志框架**（0 处 `log`/`slog`），无 `--verbose`/debug 开关，`User-Agent` 硬编码 `OpenBridge/1.0` 不含真实版本，排障只能靠 stderr 文本 | P2 |

### 0.2 Top 5 发现（按风险排序）

| # | 严重度 | 标题 | 关键证据 |
| --- | --- | --- | --- |
| 1 | **P1（已确认漏洞）** | 查询参数形式注入的 API key 会明文出现在网络错误信息、codegen 输出、`ob info -o json/yaml` 三处 | `pkg/request/builder.go:141-150` → `pkg/cli/errors.go:120-128,151`；`pkg/codegen/curl.go:41`（另 3 个生成器同）；`cmd/ob/main.go:615-632` |
| 2 | **P1（已确认）** | `go vet -tags=integration ./...` 编译失败，AGENTS.md §5.1 规定的集成测试命令不可用 | `pkg/credential/keyring_test.go:219` 与 `keyring_integration_test.go:219` 重名；`TestWithPassBackend` 同 |
| 3 | **P1（已确认）** | 4/5 个 `*_integration_test.go` 缺 `//go:build integration`，在 `-short` 与 CI 中作为单测运行；AGENTS.md §3.3 形同虚设 | `internal/integration/*.go:1-2`、`pkg/cli/errors_integration_test.go:1`、`pkg/config/install_integration_test.go:1`、`pkg/request/builder_integration_test.go:1` |
| 4 | **P1（已确认）** | keyring 无系统后端时静默回退到 FileBackend，并用**硬编码字符串 "openbridge"** 作为加密口令 | `pkg/credential/keyring.go:191-197`（`keyring.FixedStringPrompt(ServiceName)`）、`:168-186`；`detectBackend()` `:286-301` 还会误报为 keychain |
| 5 | **P1（已确认）** | 覆盖率 55.4% 且 30% 函数从未执行：`pkg/tui/install`（1347 行/0%）、`pkg/router`（0%）、`cmd/ob`（19%）——恰是安全掩码函数 `isSensitiveHeader`/`maskValue` 也是 0% | `go tool cover -func=/tmp/cover.out`；`cmd/ob/main.go:767-783` 0% |

### 0.3 一句话结论

> **测试“量”够，“质”与“门禁”不够**：17.5k 行测试没有换来对关键路径（TUI、router、CLI 输出/掩码、keyring 真实后端）的覆盖，AGENTS.md 里写的隔离约定有一半没有落地，而 CI 会把这些问题全部隐藏（integration 标签只在本地“应该”跑、覆盖率无门禁、lint 版本漂移、无漏洞扫描）。安全上有一条**真实的凭据外泄链**值得优先修。

---

## 1. 基线复核与前提更正

任务书给出的基线大部分成立，但有 2 处判定需纠正（❌）、2 处需补充口径（⚠️），后续分析以此为准。

| 任务书前提 | 实测 | 结论 |
| --- | --- | --- |
| `go build ./...`、`go vet ./...` 通过 | `go vet ./...` → 退出码 0（`VET EXIT=0`） | ✅ 成立（仅在无 tag 的默认构建下） |
| `go test -short ./...` 全绿 | 全部包 `ok`，退出码 0；但 `internal/integration` 显示 `coverage: [no statements]` 且耗时 2.4s | ⚠️ 成立但**假象**：该集成包没有 build tag，`-short` 下仍在跑 |
| `pkg/router`、`pkg/tui/install` 无测试文件 | 两个包均无 `*_test.go`，coverage 0.0% | ✅ 成立 |
| `go.mod` go 1.25.5，CI 矩阵 go 1.25，本机 1.27.3 | `go.mod:3` `go 1.25.5`；`ci.yml:57` `["1.25"]`；本机 `go1.27.3` | ✅ 成立（矩阵只保证 1.25 最新补丁，不保证最小 1.25.5） |
| 仓库根 `coverage.out`/`coverage.html` 未 tracked、可能过时 | `git status --ignored` 显示两者为 `!!`（ignored）；仓库内 `coverage.out` 总计 **56.6%**，本次实测 full **56.5%**、short **55.4%** | ✅ 未 tracked；属旧产物（Jan 25），数值与当前树**接近但不一致**，不应作为任何结论依据 |
| **“<50% 的关键包：credential/config/mcp/request”** | credential **58.4%**（-short）/ 71.8%（full）、config **73.8%/74.8%**、mcp **78.0%**、request **85.1%** | ❌ **前提不成立**。实际 <50% 的是 `cmd/ob` **19.0%**、`pkg/cli` **31.5%**；credential 的 58.4% 是因为 `-short` 跳过 keyring 测试 |
| “race 只在 make test-coverage 中有 -race” | `Makefile:92,96,100` 三个 test 目标均带 `-race`；CI `ci.yml:82` 调用的正是 `make test-coverage` | ⚠️ 部分更正：**CI 的三个 OS 都跑了 -race**，不存在“CI 缺 race”问题；反过来本地 `go test -short ./...` 不带 race |
| integration 测试是否真依赖外部 | `internal/integration/*.go` 没有任何网络调用，`real_api_test.go` 只用本地 testdata/内联 JSON | ✅ 结论：**不是“真 API 测试”**，名字有误导性（见 §4.4） |

### 1.1 本轮实测命令清单（可复现）

```bash
# 覆盖率（-short）
go test -short -count=1 -coverprofile=/tmp/cover.out -covermode=atomic ./...
go tool cover -func=/tmp/cover.out | tail -30

# 覆盖率（full，含 property/keyring 非 CI 测试）
go test -count=1 -coverprofile=/tmp/cover_full.out -covermode=atomic ./...

# AGENTS.md 集成测试命令（关键：失败）
go test -tags=integration ./pkg/credential
go vet -tags=integration ./...

# 工程流程
go vet ./...
make install                       # 预期失败，见 §6
golangci-lint run ./pkg/config     # 预期失败，见 §6
goreleaser check                   # 通过
npm audit --omit=dev               # 0 vulnerabilities
go tool cover -func=coverage.out   # 仓库旧产物 = 56.6%
```

---

## 2. 维度 1：覆盖率实测与分析

### 2.1 总量

| 指标 | 数值 |
| --- | --- |
| Go 语句总数（被统计包） | **6,614** |
| 覆盖语句数（full） | 3,734 → **56.5%** |
| short 模式总量 | **55.4%** |
| 函数总数 | **988** |
| 0.0% 函数数 | **298（30.2%）** |
| 源码行（非测试） | 18,420 |
| 测试行 | 17,527（+385 行 `test_helpers.go`） |
| 测试/源码行比 | 0.95 : 1 |

> 说明：默认 `go test` 的覆盖率**只统计被测包自身**，`internal/integration` 作为独立包语句为 0，因此它**不会**抬高其它包的覆盖率。本报告 55.4%/56.5% 即为“单测口径”，不含任何跨包覆盖率（未使用 `-coverpkg`）。

### 2.2 各包覆盖率（-short / full）

| 包 | 语句 | -short | full | 0% 函数 | 备注 |
| --- | ---: | ---: | ---: | ---: | --- |
| `pkg/request` | 296 | 85.1% | 85.1% | 3 | 最好，但 `builder_integration_test.go` 未打 tag |
| `pkg/semantic` | 325 | 85.5% | 85.5% | 3 | 较好 |
| `pkg/mcp` | 624 | 78.0% | 78.0% | 14 | `server_factory.go`/`shared.go` 无测试 |
| `pkg/spec` | 369 | 78.0% | 78.0% | 6 | **parser/formatter 无 property test** |
| `pkg/config` | 1,785 | 73.8% | 74.8% | 21 | 体量最大，尚可 |
| `pkg/completion` | 181 | 72.9% | 72.9% | 2 | 仅 1 个测试文件复用 `internal/testutil` |
| `pkg/codegen` | 307 | 70.7% | 70.7% | 14 | 4 个生成器文件无同名测试 |
| `pkg/credential` | 397 | **58.4%** | **71.8%** | 11 | **-short 与 CI 中 keyring 路径被跳过** |
| `pkg/cli` | 1,191 | **31.5%** | 31.5% | **71/116** | 用户面最大的包之一 |
| `cmd/ob` | 410 | **19.0%** | 19.0% | **39/50** | CLI 胶水层几乎未测 |
| `internal/proptest` | 13 | 53.8% | 53.8% | 6 | 测试基础设施自身 |
| `internal/testutil` | 27 | **0.0%** | **0.0%** | 10 | 只被 1 个测试文件使用 |
| `pkg/router` | 86 | **0.0%** | **0.0%** | **12/12** | 无测试文件 |
| `pkg/tui/install` | 603 | **0.0%** | **0.0%** | **72/72** | 1347 行，无测试文件 |
| `internal/integration` | 0 | — | — | — | 无源文件的孤儿测试包 |

### 2.3 0% 函数分布（按包）

```text
72  pkg/tui/install        ← 整个包
71  pkg/cli                ← 主要是 help/演示/输出渲染
39  cmd/ob                 ← 所有 install/uninstall/list/info 的 print*/show* 函数
21  pkg/config
14  pkg/mcp
14  pkg/codegen
12  pkg/router             ← 整个包
11  pkg/credential
10  internal/testutil
 6  pkg/spec
 6  internal/proptest
 3  pkg/semantic
 3  pkg/request
 2  pkg/completion
```

### 2.4 覆盖率真实性与关键盲区

**(a) 覆盖率数字本身可信，但是“偏科”的覆盖率。**
- 高分集中在 `request/semantic/mcp/config`（这四处测试投入最大，测试/源码比 2.20/0.76/0.79/1.07）。
- 低分集中在**用户真正看到的出口**：`cmd/ob` 19%（`main.go` 1057 行，50 个函数中 39 个 0%）、`pkg/cli` 31.5%（116 个函数中 71 个 0%）。

**(b) 0% 的函数包含安全相关逻辑，这是最值得警惕的“假安全”。**
- `cmd/ob/main.go:767-776` `isSensitiveHeader`、`:779-783` `maskValue`、`:759-763` `valueOrNone` 全部 **0.0%**。也就是说**掩码逻辑本身没有任何测试**，其正确性完全靠人眼（见 §7.3，实际也确实存在掩码缺口）。
- `pkg/cli/handler.go:35-96`（Handler 初始化相关）多个入口函数 0%。
- `pkg/router` 12 个函数 0%：路由是 `ob <app> <resource> <verb>` 与 `ob run` 的分流核心，所有 shim 调用都要经过它。
- `pkg/tui/install` 72 个函数 0%：`ob install` 的交互式向导（用户最常用路径之一）完全没有测试，`-short` 与 full 都是 0%。

**(c) `-short` 掩盖了下滑。**
`pkg/credential` 从 full 的 71.8% 掉到 short 的 58.4%：13.4 个百分点完全来自 keyring 集成测试，而这些测试有 **两重跳过**：

```go
// pkg/credential/keyring_test.go:317-329
if testing.Short() { t.Skip("skipping integration test in short mode") }
if isCI()          { t.Skip("skipping keyring test in CI environment") }
m, err := NewManager()
if err != nil     { t.Skipf("keyring not available: %v", err) }
```

`isCI()`（`keyring_test.go:523-532`）检查 `CI/GITHUB_ACTIONS/TRAVIS/CIRCLECI/JENKINS_URL` —— **在 GitHub Actions 上恒为 true**。因此凭据存取（本产品“Secure Credentials”卖点）在 CI 中**永远不执行**；即便在 macOS runner 上 Keychain 可用也一样跳过。

**(d) 覆盖率没有门禁。**
- CI `ci.yml:84-91` 上传 codecov，但 `fail_ci_if_error: false`，且**没有** threshold/`codecov.yml`。
- 质量下滑不会被任何流程拦住：本分支（HEAD~2 起）“code simplification” 重构后覆盖率仅 ~56%，无人察觉。

**(e) 大量“可测但没测”的功能性死区。**
`ob list`/`ob info`/`ob uninstall` 的全部 `print*`/`show*` 函数 0%：这些函数是纯格式化逻辑，用 `t.TempDir()+bytes.Buffer` 即可测，成本极低，却完全空白。对比 `pkg/spec/format_test.go`（398 行）说明团队有能力写这类测试，只是 CLI 出口层没有跟上。

---

## 3. 维度 2：测试质量

### 3.1 量化画像

| 指标 | 数值 | 评价 |
| --- | ---: | --- |
| 测试文件数 | 36 | — |
| 顶层 `func Test*` | **535** | 数量充足 |
| `t.Run` 子测试 | 208 | 表驱动比例偏低 |
| `assert.*` | 493 | — |
| `require.*` | 400 | require:assert ≈ 45:55，总体合理 |
| stdlib `t.Errorf/Fatalf` | **728 / 380** | 与 testify **两套风格混用** |
| `t.Helper()` | 11 | 公共 helper 标记不足 |
| `t.Parallel()` | **0** | 全仓零并行 |
| `t.Skip*` | **30** | 见 §3.3 |
| `time.Sleep` | **13** | 见 §3.4 |
| `TestMain` | 0 | 无全局 setup/teardown |
| `Benchmark*/Fuzz*` | **0** | “<100ms 冷启动”无基准测试守护 |
| `//nolint` | 14 | 均带理由注释，符合 AGENTS.md §1.1 ✅ |

### 3.2 断言强度与风格

**(a) 断言库混用（P3）。**
20/36 个文件完全不用 testify，用 stdlib；16 个文件用 testify。示例：
- 纯 stdlib：`pkg/spec/parser_test.go`（130 处 `t.Errorf/Fatalf`）、`pkg/config/config_test.go`（83）、`pkg/cli/args_test.go`（67）、`pkg/semantic/*`、`pkg/codegen/codegen_test.go`。
- 纯 testify：`pkg/config/spec_watcher_test.go`（107 require + 68 assert）、`pkg/mcp/registry_test.go`、`pkg/credential/security_test.go`。

影响：错误信息格式、失败粒度（`require` 立即终止 vs `assert` 继续）不一致；新人难以判断项目标准；`require` 在 keyring 测试中大量使用（24 处）使得“跳过 vs 失败”的判定依赖 helper 实现。

**(b) 断言强度总体合格，不是“空断言测试”。**
抽查未见“只调用不校验”的测试。自动化扫描出的 13 个“无断言”函数经人工核对**全部是委托给 helper** 的薄封装，例如：

```go
// pkg/spec/parser_test.go:984
func TestLoadSwagger20Spec(t *testing.T) {
    verifySwaggerSpecLoading(t, "swagger20.json", "Swagger 2.0 Pet Store")   // helper 内 t.Fatalf
}
// pkg/request/builder_test.go:936
func TestValidateParams_RequiredParameters(t *testing.T) { ... runValidateParamsTests(t, b, tests) }
// pkg/config/export_test.go:574
func TestIsCredentialHeader(t *testing.T) { ... runCredentialDetectorTests(t, "isCredentialHeader", tests, isCredentialHeader) }
```

但这也带来一个反向问题：**helper 是“隐式断言点”，测试函数本身可读性差**，且 helper 无统一约定（5 个包各有 `test_helpers.go`，命名/用途/签名不同，`t.Helper()` 仅打了 11 处）。

**(c) 表驱动程度不均。**
- 好：`pkg/request/builder_test.go`（15 个表 + 20 个 `t.Run`）、`pkg/credential/security_test.go`（10 表 + 32 子测试）、`pkg/spec/format_test.go`（7 表）。
- 差：`pkg/config/spec_watcher_test.go` 1001 行/48 个 Test 函数、`t.Run` 0 个、表 0 个——大量重复 `time.Sleep` 的串行用例；`pkg/cli/args_test.go` 28 个 Test 函数、0 个 `t.Run`。
- `pkg/spec/parser_test.go` 1868 行/67 个 Test 函数是最大的单文件，但只有 2 处 `t.Run`。

**(d) 错误分支覆盖。**
- 有专门错误分支测试：`pkg/cli/errors_test.go`（8 个 Test，覆盖 HTTP 4xx/5xx、网络错误、AppNotFound 等）、`pkg/cli/errors_integration_test.go`（7 个）。
- 缺口：`pkg/cli/handler.go` 的 71 个 0% 函数里包含大量错误分支（`printAndWrapError` 之外的分支），`ExecuteCommand` 主流程也基本未覆盖。
- 部分错误被吞掉且无测试：生产代码 `_ = ` 丢弃错误 20 处（`install.go:306,550`、`spec_watcher.go` 等），测试没有守护“错误不应被吞”。

### 3.3 `t.Skip` 画像（30 处，全部列出归类）

| 原因 | 数量 | 位置 | 问题 |
| --- | ---: | --- | --- |
| `testing.Short()` 跳过 property test | 4 | `pkg/config/config_property_test.go:18`、`export_property_test.go:18,92`、`pkg/credential/keyring_property_test.go:30,111,163`（共 6 处含 credential） | AGENTS.md §5.1 推荐的本地门禁正是 `go test -v -short ./...` → **property test 在推荐流程中永不运行** |
| `testing.Short()` 跳过 keyring 集成 | 5 | `keyring_test.go:318,343,399,436,478` | 同左 |
| `isCI()` 跳过 keyring | 5 | `keyring_test.go:323,347,403,440,482` | **CI 中凭据路径 0 覆盖** |
| keyring 环境不可用 | 5 | `keyring_test.go:328,352,408,445,487` | 在无桌面 Linux 上静默通过 |
| 无法写 home 目录 | 1 | `pkg/config/path_utils_test.go:139` | 环境相关 |
| integration 测试 short 跳过 | 2 | `pkg/config/install_integration_test.go:15,226` | 依赖外部 `ob` 二进制/进程 |

**结论（P2）**：30 处 `t.Skip` 中有 **21 处直接或间接导致“本 RFC 期望验证的行为在 CI 不执行”**。CI 绿 ≠ 功能可用。特别是：`isCI()` 的跳过策略让三平台 CI 都无法发现 keyring 回归。

### 3.4 Flaky 风险

13 处 `time.Sleep`：

| 文件 | 行 | 睡眠时长 | 风险 |
| --- | --- | --- | --- |
| `pkg/config/spec_watcher_test.go` | 177,243,275,746,778,806,833,866,904,937,987 | 10ms/100ms × | fsnotify 事件到达时间依赖机器负载；Windows/CI 上 100ms 容易不足 → 偶发失败 |
| `pkg/spec/parser_test.go` | 160,561 | 10ms/100ms | 同上（文件 mtime/缓存失效场景） |

实测：`go test -race -count=5 -short ./pkg/config -run 'TestSpecWatcher|TestWatcher'` 连续 5 次通过（2.6s），但该筛选只命中部分用例；在 3 个 OS 的 CI 上 5 次中偶发一次的成本远高于本地。建议改为 `require.Eventually`（轮询 + 超时）而非固定 sleep。

其他 flaky 面：
- `pkg/config/install_integration_test.go:66,146,236` 依赖 `os.Getenv("OB_BINARY")`/外部进程（需看具体），在没有先 `make build` 的环境直接失败——CI 未构建二进制时是否稳定存疑。
- 多处测试写真实用户目录（见 §3.7），并发/残留会造成顺序依赖。
- 0 处 `t.Parallel` 意味着**问题被掩盖而非消除**：串行执行降低了资源竞争，但 CI 时长与隔离问题仍存在。

### 3.5 Property-based testing 质量（AGENTS.md §3.2 专项）

**(a) 覆盖范围不达标（P1）。**
- 全仓仅 7 个 `TestProperty*`：`pkg/config`（3）、`pkg/credential`（3）、`internal/proptest`（1 个自检）。
- **`pkg/spec`（parser.go 839 行 + format.go 序列化/格式化）0 个 property test**。这正是 AGENTS.md §3.2 点名要求的“Parsers and formatters / Serialization”。`pkg/spec` 有 78% 覆盖率，但全部是示例驱动（example-based）测试，不覆盖 `Decode(Encode(x)) == x` 这类不变量。
- 命名规范 ✅：7 个函数均以 `TestProperty` 开头。

**(b) 现有 property test 有效性存疑（P2）——“任何错误都 return true”。**

```go
// pkg/config/config_property_test.go:45-71
m, err := NewManager(WithConfigDir(tmpDir))
if err != nil {
    t.Logf("Failed to create manager: %v", err)
    return true                       // ← 属性测试空转
}
...
err = m.SaveAppConfig(original)
if err != nil {
    t.Logf("Save failed for app=%s: %v", appName, err)
    return true                       // ← 失败被当成“通过”
}
loaded, err := m.GetAppConfig(appName)
if err != nil { ... return true }     // ← 同上
...
if !matches { t.Logf(...) }           // ← 不一致也只是 log？注意此处最终 return matches
return matches
```

`pkg/config/export_property_test.go:50-60` 同样在 save/export 失败时 `return true`。实际效果：只要被测代码**永远返回 error**，property 测试仍会通过——这与 property test 的目的相反。加之默认 `gopter` 100 次迭代 + 每次创建临时目录做磁盘 I/O，运行成本不低（`pkg/config` 3.3s），收益却不可靠。

**(c) 生成器覆盖单薄（P3）。**
`internal/proptest` 只有 10 个薄封装 generator（`AlphaString/Identifier/IntRange/SliceOf/MapOf/OneConstOf/Bool/AppName/ProfileName`），`pkg/config` 的 property 测试还额外用 `gen.AlphaString().SuchThat(len>3)` 手工过滤，未使用 `AnyString` 等；`internal/testutil` 的 `MockServer` 未被 property test 使用。

### 3.6 Mock 策略 vs AGENTS.md §4.2

- AGENTS.md 要求“优先标准工具（如 mockery），除非接口简单”。
- 实际情况：**没有 mock 生成框架，也没有 mock 文件**。`pkg/config/test_helpers.go`（78 行）与 `pkg/request/test_helpers.go`（102 行）是手写 fake/构造器；`internal/testutil/testutil.go`（208 行）提供 `MockServer`（httptest 封装）。
- 判断：**对该项目可接受**——依赖多为 `httptest.Server` 与真实文件系统，不需要 mock 框架。但存在两个不一致：`internal/testutil` 只有 1 个消费者（`pkg/completion/completion_test.go:8`），其余包各自手写 httptest，重复实现；`internal/testutil` 自身 0% 覆盖，实际上成了“半废弃”代码。**建议（P3）**：要么统一复用并加测试，要么删除以降低维护面。

### 3.7 测试隔离：真实用户目录污染（P2）

**可证实的副作用：** `cmd/ob` 在 `init()` 中初始化全局依赖：

```go
// cmd/ob/main.go:52-90（节选）
func init() {
    configMgr, err = config.NewManager()      // ← 读写真实平台配置目录
    if err != nil { fmt.Fprintf(os.Stderr, ...); os.Exit(1) }
    credMgr, err = credential.NewManager()    // ← 打开真实系统 keyring
    ...
}
```

`go test ./cmd/ob` 会 **import 该包并执行 `init()`**，因此：
1. 每个测试二进制都会在真实 `~/Library/Application Support/openbridge`（或 `~/.config/openbridge`）创建目录；
2. 会打开真实 keyring（macOS 上可能触发 Keychain 授权弹窗）；
3. 若 `NewManager` 失败会 `os.Exit(1)`，**在 init 中终止整个测试进程**，表现为“测试未运行就失败”，极难排查。

**现场证据：** 本机真实配置目录中存在测试命名的残留：

```text
~/Library/Application Support/openbridge/apps/
  harbor.yaml      (2026-01-25 23:13, spec=~/Downloads/swagger.yaml —— 开发者真实使用)
  testapp.yaml     (2026-01-25 22:08, spec=/tmp/test_spec.yaml —— 测试命名)
  testbody.yaml    (2026-01-25 22:39, spec=/tmp/test_body_spec.yaml —— 测试命名)
drwxr-xr-x  apps/  harbor/  testbody/   # 权限 0755
drwx------  keyring/                  # 权限 0700（keyring 库创建，较正确）
```

`testapp`/`testbody` 与测试代码中的命名完全一致（`cmd/ob/main_test.go:437`、`pkg/config/install_test.go`），`/tmp/test_spec.yaml` 在仓库中**不存在**，说明曾有一次非隔离运行把测试数据写进了真实配置目录。`TestUpdateAPIKeyName` 虽然用了 `t.TempDir()`，但它替换的是全局 `configMgr`，一旦顺序/并发变化就会互相影响：

```go
// cmd/ob/main_test.go:470-480
originalConfigMgr := configMgr
defer func() { configMgr = originalConfigMgr }()
configMgr = mgr   // ← 全局可变状态
```

同类问题：`internal/integration/integration_test.go:55` 直接 `credential.NewManager()`（无 `WithFileBackend`/临时目录），在开发者机器上会访问**真实 keyring**；在 CI 上被 `isCI()` 跳过（但该文件本身没有 build tag，在本地 `-short` 仍会打开 keyring）。

**建议（P2）**：`init()` 不应做 I/O；改为 `main()` 内的显式装配，测试通过 `TestMain` + `OPENBRIDGE_CONFIG_DIR`/`WithConfigDir` 注入；或在 `init()` 检测 `testing.Testing()` 并跳过。

### 3.8 单个包的抽样评价

**`pkg/config`（73.8%，5389 行测试）**
- 优点：体量大、错误路径覆盖较全（`spec_watcher` 107 require）、有 property test（但见 §3.5b）。
- 缺点：`spec_watcher_test.go` 1001 行靠 `time.Sleep` 串行；`install_test.go` 841 行全用 stdlib 断言；`config_test.go:34,64` 直接 `os.Setenv/os.Unsetenv` 而非 `t.Setenv`（泄漏风险，`defer Unsetenv` 在 parallel 下不安全——目前没有 parallel 才没炸）。

**`pkg/cli`（31.5%，1352 行测试）**
- `args_test.go` 450 行 28 个 Test，覆盖参数解析主路径，但 **0 个 `t.Run`、0 个表驱动**，每个用例重复 setup。
- `errors_test.go`/`errors_integration_test.go` 质量较好（表驱动、断言结构化输出）。
- `handler.go` 892 行、71 个函数 0%：`ExecuteCommand`、帮助渲染、输出格式分支没有测试；这是 CLI 的核心。

**`pkg/spec`（78.0%，2266 行测试）**
- 测试量最大（2.01:1），Swagger 2.0/OpenAPI 3/JSON/YAML 都有覆盖。
- 但：无 property test；`parser_test.go:990,1421` 反向依赖 `../../internal/integration/testdata/*`（单测依赖另一包的测试资产，跨包耦合，P3）；存在 `time.Sleep` 2 处。

**`pkg/mcp`（78.0%，1729 行测试）**
- `registry_test.go`/`progressive_test.go`/`engine_predicate_test.go` 断言密度高（30~45 assert），但 `server_factory.go`（3 函数）与 `shared.go`（1 函数）完全无测试。
- 进步：`handler_test.go` 用 httptest 端到端覆盖 MCP 调用链。

### 3.9 没有的东西（缺口清单）

- 0 个 `Benchmark*`：README 宣称“Cold start under 100ms”（`README.md:26`），实测**首次执行 610ms**、热启动 5.4–6.8ms（`python3` 计时 5 次取中位），无基准测试守护，性能回归不可见。
- 0 个 `Fuzz*`：parser 处理不可信 OpenAPI 输入，fuzz 是低成本高收益。
- 0 个 `TestMain`：无全局资源回收/环境隔离。
- 0 个 `t.Cleanup` 之外的全局清理：`os.Setenv` 风格残留。
- 无“测试运行时间”监控：CI 未设 timeout（`ci.yml` test job 无 `timeout-minutes`）。

---

## 4. 维度 3：AGENTS.md 合规

### 4.1 集成测试 build tag（§3.3）——严重违规

| 文件 | 是否 `//go:build integration` | 结果 |
| --- | --- | --- |
| `internal/integration/integration_test.go` | ❌ 无（首行是 package 注释） | 在 `-short`、CI 中执行 |
| `internal/integration/real_api_test.go` | ❌ 无 | 同上 |
| `pkg/cli/errors_integration_test.go` | ❌ 无 | 同上（430 行） |
| `pkg/config/install_integration_test.go` | ❌ 无 | 同上（263 行），内部还有 `testing.Short()` 手工跳过 |
| `pkg/request/builder_integration_test.go` | ❌ 无 | 同上（219 行） |
| `pkg/credential/keyring_integration_test.go` | ✅ `//go:build integration`（第 1 行） | **唯一合规**，但见 4.2 |

**证据（内部集成包头部）：**

```go
// internal/integration/integration_test.go:1-2
// Package integration provides end-to-end integration tests for OpenBridge.
package integration            // ← 没有 build tag
```

**后果：**
1. `-short` 下仍会执行（`coverage: [no statements]` 且耗时 2.4s 即证据）；
2. CI 三平台都会跑这些“集成测试”，违反 AGENTS.md “Integration Test Isolation”；
3. `make test-short`/`go test -short ./...` 的语义与文档不符；
4. 由于它们实际只依赖本地文件与 httptest，**技术上并不需要隔离**——正确做法是把其中真正的单元场景合并/重命名，而不是留一个名不副实的 `*_integration_test.go`。

### 4.2 带 tag 的集成测试**编译失败**（最严重）

```text
$ go test -tags=integration ./pkg/credential
pkg/credential/keyring_integration_test.go:261:6: other declaration of TestWithPassBackend
FAIL	github.com/nomagicln/open-bridge/pkg/credential [build failed]

$ go vet -tags=integration ./...
vet: pkg/credential/keyring_test.go:219:6: TestGetPlatformInfo redeclared in this block
```

**根因：** 同名 `func TestXXX` 同时存在于未打 tag 的 `keyring_test.go` 与打了 tag 的 `keyring_integration_test.go`：

```bash
$ grep -oE '^func (Test[A-Za-z0-9_]+)' pkg/credential/*_test.go | sort | uniq -c | awk '$1>1'
      2 func TestGetPlatformInfo
      2 func TestWithPassBackend
```

**影响：**
- AGENTS.md §5.1 第 3 步 “Integration Tests (Optional but Recommended): `go test -v -tags=integration ./...`” **完全不可执行**；
- 由于 CI 从不跑 `-tags=integration`（`ci.yml:82` 只调 `make test-coverage`），这个损坏**长期不可见**；
- 修复方式：删除未打 tag 文件中的重复定义（把真正的集成场景留在 tagged 文件），或反向打 tag。

### 4.3 测试文件与源文件严格配对（§3.1）

AGENTS.md 要求“Strict Name Matching: foo.go ↔ foo_test.go”和“No Orphan Tests”。

| 违规类型 | 文件 | 说明 |
| --- | --- | --- |
| **孤儿测试（无对应源文件）** | `internal/integration/integration_test.go`（762 行）、`real_api_test.go`（258 行） | 包内无任何非测试 `.go` 文件 → 整个包只存在于测试，构成“测试包孤儿” |
| **无测试的源文件** | `pkg/router/router.go`（250 行，12 函数） | 核心路由，0% |
| | `pkg/tui/install/model.go`（1347 行，72 函数） | 安装向导，0% |
| | `pkg/mcp/server_factory.go`（48 行）、`pkg/mcp/shared.go`（85 行） | 0% |
| | `pkg/codegen/curl.go`、`go.go`、`nodejs.go`、`python.go` | 仅由 `codegen_test.go` 间接覆盖（违反严格配对，但测试存在） |
| | `internal/proptest/generators.go`、`internal/testutil/testutil.go` | 基础设施，无对应测试（可接受但 testutil 是 0%） |
| **命名不符** | `cmd/ob/main_test.go`（源文件 `main.go`，可接受）；`internal/integration/*`（`_test.go` 不以 `_integration_test.go` 结尾却叫 integration） | — |

补充：`pkg/semantic/mapper_test.go` **是合法配对**（`mapper.go` 存在），任务书提到的“不匹配”实际不成立；真正的不匹配是上表。

### 4.4 Property testing 规则（§3.2）

| 规则 | 状态 | 证据 |
| --- | --- | --- |
| Parser 需要 property test | ❌ | `pkg/spec` 0 个 `TestProperty*` |
| Formatter 需要 property test | ❌ | `pkg/spec/format.go` 无 property test |
| Serialization/Deserialization | ⚠️ 部分 | 仅 config 的 YAML round-trip（`config_property_test.go`），且见 §3.5b 空转 |
| 命名 `TestProperty*` | ✅ | 7/7 符合 |
| 使用 gopter | ✅ | `go.mod:17 github.com/leanovate/gopter v0.2.11` |

### 4.5 其他规则

| 规则 | 状态 | 证据 |
| --- | --- | --- |
| `//nolint` 必须带理由 | ✅ | 14 处全部带注释（如 `pkg/tui/install/model.go:285` `//nolint:funlen // Step routing requires many cases`） |
| 错误包装 `%w` | ⚠️ 基本符合 | `pkg/cli/handler.go:87,130,134` 均 `%w`；但有 20 处 `_ =` 丢弃错误（如 `pkg/config/install.go:306,550`） |
| Sentinel errors | ✅ | `pkg/config/config.go:805-830` 定义 `AppNotFoundError`/`ProfileNotFoundError` 等 |
| Mock 优先标准工具 | ⚠️ 手写（可接受，见 §3.6） |
| 提交 DCO sign-off | ❌ | 见 §5.8：最近 40 个提交 35 个缺 `Signed-off-by` |
| 文档同步 | ❌ | 见 §8.3：README/QUICKSTART/docs 与实现存在 4 处不一致 |
| TDD 三步协议 | 无法验证 | 历史提交粒度大（“comprehensive code simplification”），无 red/green 痕迹可查 |

---

## 5. 维度 4：CI 审查（`.github/workflows/`）

### 5.1 `ci.yml` 全量问题清单

| # | 行 | 问题 | 严重度 |
| --- | --- | --- | --- |
| C-1 | 57 | Go 矩阵 `["1.25"]` 只锁定 minor，无法保证测试到 `go.mod` 要求的 **1.25.5**；也无 `go-version-file: go.mod` | P3 |
| C-2 | 106-109 | `golangci/golangci-lint-action@v9` + `version: latest` → **不可复现**；构建工具链（Go 1.25）与 lint 二进制版本随上游漂移；`.golangci.yml` 的 `run.timeout: 3m` 与 `args: --timeout=5m` 不一致 | **P2** |
| C-3 | 82 | 测试只跑 `make test-coverage`（= `go test -v -race -coverprofile ...`），**从不跑 `-tags=integration`** → §4.2 的编译失败永远发现不了 | **P1** |
| C-4 | 84-91 | coverage 仅在 `ubuntu-latest` 上传；`fail_ci_if_error: false`；**无覆盖率阈值**；未上传 `coverage.html`/未做 PR 注释 | P2 |
| C-5 | 全文件 | 无 `govulncheck`、无 CodeQL workflow、无 Dependabot/Renovate、无 license/header 检查、无 `goreleaser check`、无 `go vet` 独立步骤（依赖 golangci 的 govet） | **P2** |
| C-6 | 68-76 | 手写 `actions/cache` 缓存 `~/.cache/go-build`：**macOS 实际路径是 `~/Library/Caches/go-build`，Windows 是 `%LOCALAPPDATA%\go-build`** → 该缓存只在 Linux 生效；且 `setup-go@v5` 默认已带缓存，重复 | P3 |
| C-7 | 111-136 | `build` job 只在 ubuntu；`./bin/ob --version` 冒烟只在 linux；无 `go build ./...` 全平台验证（靠 test 隐式编译） | P3 |
| C-8 | 138-163 | `release` 只在 tag、`needs: [build]`，但**不做 `goreleaser check`**、不验证产物 checksum、无 provenance/签名 | P2 |
| C-9 | 20,61,64,98,117,147,157 | actions 仅 pin 到 major tag（`@v4`/`@v5`/`@v9`），未 pin SHA → 供应链风险（`tj-actions/changed-files` 类事件） | P3 |
| C-10 | 全文件 | **无 `concurrency`** → 同一分支连续 push 会并发跑全部 job（浪费 3 OS × race 的机时） | P3 |
| C-11 | 11-12 | 顶层 `permissions: contents: read` ✅ 最小化；`release` job 单独提权 `contents: write` ✅ 正确 | ✅ 无问题 |
| C-12 | 92 | lint job 无缓存（未用 setup-go cache），慢 | P3 |
| C-13 | 51-91 | test job 无 `timeout-minutes`；3 OS × `-race` 全量测试（含固定 sleep 用例）有超时/抖动风险 | P3 |

**Windows 测试真实性（P3，需 CI 日志核实）：**
- Windows job 直接 `make test-coverage`（`ci.yml:79` 用 `make deps`，`:82` 用 `make`）→ **依赖 windows runner 自带 GNU make**（GitHub 镜像是否有 make 随镜像变化，属外部依赖）；
- `pkg/config` 的 shim 测试有 `runtime.GOOS` 分支（`install_test.go:406,645,671,698,727,830`），Unix symlink 断言只在非 Windows 执行（`test_helpers.go:29-50`）——设计正确；
- 但 `pkg/tui/install` 无测试、keyring 测试被 `isCI()` 全跳 → **Windows 上的真实差异（.cmd shim 内容、%* 参数转发、路径）只由 `install_test.go` 的少数用例覆盖**，且没有跨平台 golden file 校验。

### 5.2 integration tag 测试在 CI 的“双向缺失”

- **应排除却没排除**：4 个未打 tag 的 `*_integration_test.go` 在 CI 中当单测跑（见 §4.1）；
- **应运行却没运行**：唯一打了 tag 的 `keyring_integration_test.go` 在 CI 中既不编译也不运行（且它本身编译失败）；
- **结果**：CI 的“Test”绿灯对集成行为**零信息量**。

### 5.3 DCO 脚本边界（`ci.yml:15-49`）

脚本逻辑：

```bash
for commit in $(git rev-list "$range"); do
  if ! git log -1 --pretty=%B "$commit" | grep -qi "^Signed-off-by: "; then ... fail
```

问题：

| # | 边界 | 影响 |
| --- | --- | --- |
| DCO-1 | `grep "^Signed-off-by: "` 只要求**任意一行**以该前缀开头，**不校验签名与作者身份一致** → 任意捏造的 sign-off 可通过 | 弱执行，DCO 名存实亡 |
| DCO-2 | `git rev-list range` **包含 merge commit**；GitHub “Merge pull request” 生成的 merge commit 无 sign-off → DCO 失败；仓库确实存在 merge commit（`8842f4f Merge pull request #2`） | 对 merge 工作流不友好 |
| DCO-3 | 新分支/新 tag push 时 `github.event.before` 全 0（`ci.yml:31-35`）→ range 退化为**单个 sha**，只检查 head，漏检同一 push 里其余提交 | 可绕过 |
| DCO-4 | 缺 sign-off 的提交来自 bot 时无法豁免（若将来启用 Dependabot，会直接红） | 与 C-5 的依赖自动化冲突 |
| DCO-5 | 无 `dco` 相关 action 的标准实现（如 `tim-actions/dco`），自研脚本无测试 | 维护风险 |

**已确认的合规现状（P1）：**

```text
最近 40 个提交中 35 个缺 Signed-off-by（含 HEAD dd18374、2f3f669）
仅 5 个带 sign-off 的多为 Claude/AI 生成的合并提交
```

AGENTS.md §5.2 写明 “All commits MUST include DCO sign-off … All commits without sign-off will be rejected by the DCO check”，但历史大量违反 → 说明 **DCO check 要么长期失败、要么被绕过**；无论哪种，规则未被工程化落地。注意：本报告不通过 GitHub API 核实 Actions 运行历史（无网络访问），结论基于脚本逻辑 + 提交历史。

### 5.4 `deploy-docs.yml`

| # | 行 | 问题 | 严重度 |
| --- | --- | --- | --- |
| DOC-1 | 36 | `node-version: 20`：Node 20 已于 **2026-04-30 进入 EOL**（本环境日期 2026-11），应升 22/24 | P2 |
| DOC-2 | 34-37 | `setup-node@v4` + `cache: npm` 依赖 `package-lock.json` ✅ 存在 | ✅ |
| DOC-3 | 8 | `paths` 只含 docs/package.json/workflow，**不含源码或 README** → README 改动的站点内容不会重建（README 不在 docs 站点内则无妨） | P3 |
| DOC-4 | 3-10 | **只在 push to main 触发**，无 `pull_request` → 文档构建失败只能在合并后发现（没有 PR 预检） | P2 |
| DOC-5 | 12-15 | 权限声明在 workflow 级（`pages: write`/`id-token: write`），只有一个 job 尚可，但应按 job 收敛 | P3 |
| DOC-6 | 21-58 | 无 markdown 链接检查、无 Algolia 索引校验（构建时 secrets 缺失则静默降级？） | P3 |
| DOC-7 | 33-46 | `npm ci` 后未缓存 VitePress 构建产物；构建无 `timeout-minutes` | P3 |
| DOC-8 | 6-10 | 未包含 `AGENTS.md`、`QUICKSTART.md` 等文档源（若站点不渲染则不适用） | P3 |

### 5.5 CI 与本地工具链的割裂（P2）

- CI 用 golangci-lint **v2 系**（action v9，配置 `version: "2"`，`.golangci.yml:1`）；
- 本机 PATH 中的 `golangci-lint v1.64.8` 执行 `make lint` 直接失败：

```text
$ golangci-lint run ./pkg/config
Error: can't load config: sort-results should be 'true' to use sort-order
```

即 `make lint` 的可用性取决于开发者机器上装的是哪个大版本；`Makefile:139` 的 `go install .../v2/cmd/golangci-lint@latest` 虽指向 v2，但 `latest` 不可复现，且不会覆盖 PATH 中已有的 v1。**结论**：本地 lint 失败并不是噪声，而是“版本未 pin + 不校验”的确定性后果。

---

## 6. 维度 5：Makefile

### 6.1 `make install` 实测失败（P2，已确认）

```make
# Makefile:170-176
install: build
	@if [ -z "$(GOPATH)" ]; then \
		echo "Error: GOPATH is not set"; \
		exit 1; \
	fi
	cp $(OUTPUT_DIR)/$(BINARY_NAME) $(GOPATH)/bin/$(BINARY_NAME)
```

实测：

```text
$ echo "GOPATH env: '${GOPATH:-<empty>}'"
GOPATH env: '<empty>'
$ go env GOPATH
/Users/fangzhiheng/go

$ make build        # 成功
$ make install
Error: GOPATH is not set
make: *** [install] Error 1
```

三个问题：
1. **使用 make 变量 `$(GOPATH)` 而不是 `go env GOPATH`**：现代 Go 不要求导出 GOPATH，绝大多数开发者环境里该 env 为空 → 目标必然失败；
2. 若 GOPATH 被设置成 `/`（极端），`make -n` 显示会执行 `cp bin/ob /bin/ob`；
3. 未 `mkdir -p $(GOPATH)/bin`，首次安装可能失败。

正确写法：`GOBIN := $(shell go env GOBIN)` → fallback `$(shell go env GOPATH)/bin`，并 `install -d`。

### 6.2 版本不可复现（P2）

```make
# Makefile:30
GOLANGCI_LINT_VERSION ?= latest
```

- `install-lint`（`:137-140`）用 `@latest` 安装 v2 主版本，但 lint 行为随上游变化；
- CI 同样 `version: latest`（`ci.yml:108`）；
- 结果：同一次 PR 在两个月后重跑可能因 lint 规则升级而失败（或相反，本该报的错不再报）。

### 6.3 `fmt-fix` 的语义风险（P3，已确认命令存在风险）

```make
# Makefile:119-130
fmt-fix:
	@gofmt -w -r 'interface{} -> any' .
	@gofmt -w -r 'a[b:len(a)] -> a[b:]' .
	@gofmt -s -w .
	@goimports -w .
```

风险：
1. `gofmt -r 'interface{} -> any'` 是** AST 级别全局重写**：若任何包/局部作用域声明了名为 `any` 的类型/变量（Go 1.18 前常见兼容写法），重写后语义改变甚至编译失败；
2. `a[b:len(a)] -> a[b:]` 仅对切片成立；对数组指针、字符串等方式在部分上下文语义等价，但 `-r` 模式不区分类型，存在误改；
3. **没有 `-l`/diff 预检 + 没有 CI 校验**：`lint-fix`/`fmt-fix` 都是“直接改文件”，开发者再 `git diff` 目检；与 AGENTS.md 的“最小改动”原则相悖；
4. `gofmt -w .` 会递归处理仓库下**所有** `.go`（当前无 vendor/node_modules Go 文件，属幸运），未限定 `./cmd ./pkg ./internal`。

### 6.4 `docs` 目标使用已淘汰的 godoc（P3）

```make
# Makefile:187-190
docs:
	$(call check_tool,godoc,Install with: go install golang.org/x/tools/cmd/godoc@latest)
	godoc -http=:6060
```

- `godoc` 自 Go 1.13 起不再随发行版分发；x/tools 中的命令长期冻结，官方替代是 `pkgsite`（`golang.org/x/pkgsite/cmd/pkgsite`）；
- `check-tools`（`:149`）仍在检查 `godoc`，本机实测 `NOT FOUND`；
- 由于项目真正的文档是 VitePress（`package.json:scripts.docs:*`），该 Makefile 目标已无用户价值，建议删除或改为 `npm run docs:dev`。

### 6.5 `test-coverage` 在仓库根生成产物（P3）

```make
# Makefile:99-102
test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
```

- 产物落到仓库根（`coverage.out` 328KB、`coverage.html` 808KB 目前存在于工作区，均被 `.gitignore` 忽略）；
- 依赖“开发者不会误提交”，且 `-coverprofile` 路径不可配置；
- CI 上传 `./coverage.out` 依赖同一路径（`ci.yml:89`）——耦合可接受，但建议 `-coverprofile=$(OUTPUT_DIR)/coverage.out` 或 `/tmp`。

### 6.6 其他 Makefile 问题

| # | 位置 | 问题 | 严重度 |
| --- | --- | --- | --- |
| M-1 | `:197-199` `clean` | 只删 `bin/`、`coverage.out/html`；不删 `node_modules/`、`docs/.vitepress/dist|cache`、`examples/todo-service/.venv|__pycache__`、`bin/`（已删）→ “clean”名不副实 | P3 |
| M-2 | 全文件 | 无 `vet` 目标、无 `test-integration` 目标（`-tags=integration`）、无 `check`（fmt/lint/test 一键）→ 与 AGENTS.md §5.1 的检查清单不对应 | P2 |
| M-3 | `:91-96` | `test` 与 `test-short` 都带 `-race`，但 AGENTS.md 推荐 `go test -v -short ./...`（不带 race）→ 文档与 Makefile 不一致 | P3 |
| M-4 | `:7-10` | `VERSION` 用 `git describe --dirty`，CI 构建的二进制版本含 `-dirty`（如本机 `v0.1.3-4-gdd18374`）；`make build` 每次重编，`install` 依赖它 | P3 |
| M-5 | `:205-240` `help` | 未列出新增变量/目标（如 `test-coverage` 无说明之外的 `test-integration`）；`check-tools` 未包含 `goreleaser`/`npm`/`uv` 等实际依赖 | P3 |
| M-6 | `:156-158` `deps` | `go mod download && go mod verify` ✅ 良好（CI 使用它） | ✅ |
| M-7 | `:143-149` `check-tools` | 检查 `godoc`（已淘汰，见 6.4），不检查真正需要的 `goimports` 版本 | P3 |

---

## 7. 维度 6：安全审查（逐条标注“已确认 / 加固建议”）

> 严重度定义：P0 = 可直接远程利用或默认配置下数据丢失；P1 = 可导致凭据泄露/权限提升/功能不可用且路径明确；P2 = 需特定条件但后果重要，或纵深防御缺口；P3 = 加固建议/低影响。
> 本维度优先识别“可复现的凭据外泄链”，所有代码引用均已核对行号。

### 7.1 凭据处理总体架构

- 设计意图正确：凭据存系统 keyring，配置只存元数据（`pkg/config/config.go:191` 注释明确 “Actual credentials … NEVER in this configuration file”）；
- 实际实现存在两个层面的问题：**keyring 后端回退**（§7.2）与**凭据渲染/输出未统一脱敏**（§7.3–7.5）。

### 7.2 [P1｜已确认] keyring 无系统后端时使用硬编码口令回退到文件后端

```go
// pkg/credential/keyring.go:168-186
config := keyring.Config{
    ServiceName:              ServiceName,
    FileDir:                  fileDir,
    FilePasswordFunc:         passwordFn,      // ← 见下
    ...
}
if len(cfg.allowedBackends) > 0 {
    config.AllowedBackends = cfg.allowedBackends   // ← 默认不限制，允许任意后端
}

// pkg/credential/keyring.go:191-197
func getPasswordFunc(cfg *managerConfig) func(string) (string, error) {
    if cfg.filePasswordFn != nil { return cfg.filePasswordFn }
    return keyring.FixedStringPrompt(ServiceName)   // ← 硬编码口令 "openbridge"
}
```

分析：
- `99designs/keyring` 的 `backendOrder` 为 `[WinCred, Keychain, SecretService, KWallet, KeyCtl, Pass, File]`（`$GOMODCACHE/github.com/99designs/keyring@v1.2.2/keyring.go:36-49`）；当 `AllowedBackends` 为 nil 时取全部候选（同文件 `:59-60`），因此 Linux 无 Secret Service/KWallet（headless、容器、CI、SSH）时会**静默回退到 FileBackend**；`file.go:16-22` 的 opener 直接接受 `FileDir`+`FilePasswordFunc`，不验证强度；
- `FixedStringPrompt(ServiceName)` 会把**常量字符串 "openbridge"** 当作文件加密口令（`file.go:57-70` 的 `unlock()` 调 `passwordFunc` 即得到该常量）——凡能读取该用户文件（同机其它用户若有读权限、备份、同步盘、同用户恶意进程）都可解密，等价于“混淆”而非加密；
- 文件后端本身权限正确（目录 0700 / 文件 0600，`file.go:46,146`），所以问题**不在**文件权限，而在**口令是源码常量**；
- `AllowedBackends` 默认空 → 不限制后端。正确做法：生产默认 `AllowedBackends = {SecretService, Keychain, WinCred, KWallet, Pass}`，并显式要求用户 opt-in FileBackend；
- 关键讽刺：**CI 恰好跳过 keyring 测试**（§3.3），所以这条回退路径从未被任何自动化验证。

**判定：已确认设计缺陷（P1）。** 加固建议：
1. 默认 `AllowedBackends` 排除 FileBackend；
2. 若必须支持文件后端，口令来自 `OPENBRIDGE_KEYRING_PASSPHRASE`/系统 Prompt，禁止 `FixedStringPrompt`；
3. 启动时把实际后端写入 `ob info` 输出并在文件后端时打印醒目警告。

### 7.3 [P1｜已确认漏洞] 凭据外泄链 A：网络错误信息回显 query 中的 API key

**注入点：**

```go
// pkg/request/builder.go:140-150
func injectAPIKey(req *http.Request, authConfig *config.AuthConfig, token string) error {
    if authConfig.Location == "query" {
        q := req.URL.Query()
        q.Set(authConfig.KeyName, token)      // ← API key 进入 URL
        req.URL.RawQuery = q.Encode()
    } else { req.Header.Set(authConfig.KeyName, token) }
    return nil
}
```

**回显点：**

```go
// pkg/cli/handler.go:159-166
resp, err := h.httpClient.Do(req)
if err != nil {
    return nil, h.printAndWrapError(h.errorFormatter.FormatError(err), err)
}

// pkg/cli/errors.go:120-128
var e *url.Error
case errors.As(err, &e):
    targetURL = e.URL
    fmt.Fprintf(&sb, "Failed to connect to: %s\n", targetURL)   // ← 完整 URL（含 ?api_key=…）
...
// pkg/cli/errors.go:151-153
if targetURL != "" {
    fmt.Fprintf(&sb, "  - Try accessing %s in a browser\n", targetURL)   // ← 再打印一次
}
```

`net/http` 返回的 `*url.Error.URL` 是**含 query 的完整 URL**，因此只要 `AuthConfig.Location == "query"`（API key 走 query，很多公有 API 如此）且发生网络错误（DNS/超时/TLS），终端会输出：

```text
Error: Network connectivity issue.
Failed to connect to: https://api.example.com/v1/users?api_key=sk-live-REAL-SECRET
  - Try accessing https://api.example.com/v1/users?api_key=sk-live-REAL-SECRET in a browser
```

**判定：已确认漏洞（P1）。** 这条路径必然触发（错误是正常运维场景），且 key 会进入 shell scrollback、CI 日志、issue 截图。与 `ob info` 里辛辛苦苦做的掩码形成直接矛盾。

**修复（S）：** 在 `formatNetworkError` 及一切对外错误中，用 `redactURL(u)` 抹掉 query 值（保留 key 名）与 userinfo；`builder` 可在请求上附加 `X-…` 脱敏标记。同时补一个针对 `formatNetworkError` 的测试（当前 `pkg/cli/errors_test.go` 只测 AppNotFound/HTTP 状态，未测 URL 脱敏）。

### 7.4 [P1｜已确认漏洞] 凭据外泄链 B：`--generate` 代码生成输出未掩码 URL

```go
// pkg/cli/handler.go:196-204
generator, err := createCodeGenerator(format, profile.ProtectSensitiveInfo)  // MaskSecrets
code, err := generator.Generate(req)

// pkg/codegen/curl.go:38-43
func (g *CurlGenerator) writeURL(buf *bytes.Buffer, req *http.Request) {
    buf.WriteString(" '")
    buf.WriteString(req.URL.String())      // ← 无任何掩码
    buf.WriteString("'")
}
```

同样问题存在于全部 4 个生成器：`nodejs.go:42`、`python.go:105`、`go.go:42,115` 均直接写 `req.URL.String()`。
掩码逻辑只覆盖 Header（`codegen.go:146-171`）与 Body（`maskBody`），**URL 完全绕过**。

**影响：**
- 该功能的产品定位是“把请求生成代码交给 AI/贴到对话”（`pkg/cli/handler.go:693-698` 的帮助文本、`docs/guide/quick-start.md` 的安装向导文案 “Sensitive Info: Mask API keys in code generated for the AI context”）；
- 用户勾选“Sensitive Info”后，query 型 API key 仍会以明文出现在 curl/Python/Go/Node 代码里 → **直接进入 LLM 上下文与日志**；
- `ProtectSensitiveInfo` 的语义被打破（安全承诺不成立）。

**判定：已确认漏洞（P1）。** 修复（S）：URL 生成前统一 `maskQueryValues`（对 `looksLikeToken` 命中的值替换为 `<YOUR_API_KEY>`），并加测试 `TestGenerateDoesNotLeakQueryAPIKey`；同时给 4 个生成器补 URL 用例（目前 `pkg/codegen` 14 个函数 0% 覆盖，4 个生成器文件无同名测试）。

### 7.5 [P2｜已确认] 凭据外泄链 C：`ob info -o json/yaml` 未掩码 Header

```go
// cmd/ob/main.go:602-632
func printAppConfig(cfg *config.AppConfig, format string) error {
    switch format {
    case "json": return printAppConfigJSON(cfg)   // json.MarshalIndent(cfg) —— 全量
    case "yaml": return printAppConfigYAML(cfg)   // yaml.Marshal(cfg)  —— 全量
    default: printAppConfigText(cfg)              // 这里才做 mask
    }
}

// 掩码只存在于文本渲染：
// cmd/ob/main.go:719-731
for k, v := range p.Headers {
    displayValue := v
    if isSensitiveHeader(k) { displayValue = maskValue(v) }
```

`Profile.Headers`（`pkg/config/config.go:67` `Headers map[string]string`）允许用户配置任意 header，其中 `Authorization`/`X-API-Key` 等是常见用法。默认 `ob info`（text）会掩码 ✅，但 `ob info <app> -o json` 或 `-o yaml`（命令自身帮助 `cmd/ob/main.go:573-574` 明确推荐）会**原样输出**。

**判定：已确认（P2，条件触发：用户显式选 json/yaml）。** 修复（S/M）：在 `marshal` 前做 `redactConfig(cfg)`（复制结构体并替换 headers 值）；`isSensitiveHeader`/`maskValue` 也需补测试（当前 0% 覆盖）。

### 7.6 [P2｜已确认] 掩码规则覆盖不足且存在误报

```go
// cmd/ob/main.go:767-776
func isSensitiveHeader(name string) bool {
    sensitive := []string{"authorization", "x-api-key", "api-key", "token", "secret", "password"}
    nameLower := strings.ToLower(name)
    for _, s := range sensitive {
        if strings.Contains(nameLower, s) { return true }
    }
    return false
}

// cmd/ob/main.go:779-783
func maskValue(value string) string {
    if len(value) <= 4 { return "****" }
    return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
}
```

- 漏报（P2）：`Cookie`/`Set-Cookie`、`Proxy-Authorization`、`X-Auth`、`X-Session-Id`、`passwd`、`private-key` 等不在列表 → 文本输出也照样泄露；
- 误报（P3）：子串匹配会把 `X-Token-Count`、`token_expiry` 之类非敏感头掩码，影响可读性；
- 掩码保留首尾各 2 字符（`maskValue`），对短 key 信息量偏高（如 8 位 key 只遮 4 位）；`value[:2]` 对多字节 UTF-8 可能切出半个字符（Go 字符串按字节 → 输出乱码，P3）；
- 同一套规则在 `pkg/codegen` 中另有一份前缀列表（`codegen.go:152-164`），两份规则不一致（codegen 有 `X-Api-Secret` 等，main 没有）→ **建议提取到公共 `internal/redact`**。

**判定：已确认（P2 漏报 / P3 误报）。** 加固建议：改为“redact-by-default + allowlist”或统一使用成熟库；至少为上述 header 补 case（`pkg/cli/errors_test.go` 的测试风格）。

### 7.7 [P2｜已确认] TLS 配置是“死配置”：没有任何 `crypto/tls` 引用

```text
$ grep -rn 'tls\.|crypto/tls' --include='*.go' pkg internal cmd | grep -v _test
(空)
```

- `config.TLSConfig`（`pkg/config/config.go:210-228`：`InsecureSkipVerify/CAFile/CertFile/KeyFile/MinVersion`）被安装向导（`pkg/tui/install/model.go:650-760`）、安装选项（`pkg/config/install.go:70-71,142-151,230-232`）写入并持久化；
- `cmd/ob/main.go:698-715` 会在 `ob info` 中展示；
- **但没有任何 HTTP 客户端使用它**：`pkg/spec/parser.go:176`、`pkg/config/spec_cache.go:113,128`、`pkg/cli/handler.go:45`、`pkg/mcp/handler.go:32`、`pkg/mcp/progressive.go:80` 全部是默认 `http.Client{}`/`&http.Client{Timeout: 30s}`，无自定义 `Transport`。

**判定：已确认功能缺陷（P2）。**
- 安全侧“好消息”：不存在实际的 `InsecureSkipVerify: true` 漏洞（配置被忽略）；
- 功能侧坏消息：**自签名/私有 CA 的 API 无法使用**（用户明确配置 `insecure_skip_verify: true` 也不生效），客户端证书认证（mTLS）同样不可用——而安装向导专门引导用户配置它们；
- 修复（M）：为所有 HTTP 客户端构建共享 `*http.Transport`（从 profile.TLSConfig 派生），并在 `ob info` 中标注实际生效状态；同时补测试（当前 0%）。

### 7.8 [P2｜已确认] 请求无超时（可致挂起）

| 位置 | 代码 | 风险 |
| --- | --- | --- |
| `pkg/cli/handler.go:45` | `httpClient: &http.Client{}` | API 请求**无超时**；`handler.go:161` 的 `Do` 可永久挂起 |
| `pkg/mcp/handler.go:32` | `httpClient = &http.Client{}` | MCP 工具调用可永久挂起 |
| `pkg/mcp/progressive.go:80` | `httpClient = &http.Client{}` | 同上 |

对比：`pkg/spec/parser.go:176-177`、`pkg/config/spec_cache.go:113-114,128-129` 均设置 30s 超时 ✅。而 `Profile.Timeout`（`pkg/config/config.go:843`，默认 30s）**没有传递给 CLI 请求客户端** → 另一个“配置存在但未接线”的问题（与 7.7 同类，P2）。修复：用 `profile.Timeout` 构造 per-request `context.WithTimeout`。

**判定：已确认（P2）。** 这也解释了为什么 `pkg/cli/errors.go` 的网络错误分支缺少测试：没有超时，写超时测试很麻烦。

### 7.9 [P3｜已确认] shim 生成与执行：无命令注入，但边界可加固

```go
// pkg/config/install.go:586-593（Windows）
shimContent = fmt.Sprintf(`@echo off
"%s" run %s %%*
`, obPath, appName)
...
// Unix：os.Symlink(obPath, shimPath)
```

- **无 shell 注入**：`appName` 先过 `validateAppName`（`pkg/config/config.go:773-801`，`^[a-zA-Z][a-zA-Z0-9_-]*$`，长度 ≤64，保留字黑名单），不允许空格/引号/`&`/`|`/`^`；`obPath` 用双引号包裹；
- 残余风险（P3）：
  1. Windows 批处理中 `%` 会被展开——若 `obPath` 含 `%VAR%` 形式（路径本身可含 `%`），会被 cmd 二次展开；
  2. `CreateShim`/`RemoveShim`/`ShimExists` 是导出方法，**自身不校验 appName**，只依赖调用方；`CreateShim("../../evil", dir)` 会路径遍历（当前所有调用点都先 `validateAppName`，属纵深防御缺口）；
  3. Unix shim 用 symlink 且 `_ = os.Remove(shimPath)` 先删后建（`install.go:598-604`）：若 shimDir 被同机攻击者控制，存在竞态（对本机 CLI 威胁模型较低）；
  4. `getDefaultShimDir()` 对 Unix 统一用 `~/.local/bin`（`install.go:663-685`），若该目录不在 PATH 会提示（已处理）。
- `exec` 唯一使用点（`pkg/config/install.go:758`）是 `exec.LookPath("ob")` —— **不是命令执行**，无注入面。✅

**判定：无已确认漏洞；3 条加固建议（P3）。**

### 7.10 [P2｜已确认] 文件与目录权限过宽

| 路径 | 代码 | 权限 | 说明 |
| --- | --- | --- | --- |
| 应用配置 | `pkg/config/config.go:455` `os.WriteFile(tmpPath, data, 0644)` | 0644 | 含 `Profiles.*.Headers`，可能含 Authorization |
| 配置目录 | `pkg/config/config.go:332` `os.MkdirAll(path, 0755)` | 0755 | 其它用户可列目录 |
| spec 缓存 | `pkg/config/spec_cache.go:557,593,710` | 0644 | 缓存内容可能含安全 scheme |
| 缓存目录 | `pkg/config/spec_cache.go:551,696` | 0755 | — |
| profile 导出 | `pkg/config/export.go:583` | 0644 | 导出文件（设计上不含凭据） |
| 生成的代码 | `pkg/cli/handler.go:184` | **0600** ✅ | 唯一正确使用严格权限的写文件操作 |
| keyring 目录 | 由 keyring 库创建 | 0700 ✅ | 实测 `drwx------` |

**判定：已确认（P2）。** 配置目录与文件应 0700/0600（至少包含 headers 的 app 配置），或文档明确“headers 不要放密钥”。另：`writeConfigAtomically`（`config.go:453-462`）使用**固定 tmp 名** `configPath + ".tmp"` 且无 `fsync`：
- 并发 `ob install` 同一 app 会互相覆盖同一 tmp 文件（最后 rename 者胜，可能丢更新）；
- 崩溃/掉电窗口内可能出现 0 字节配置（rename 前未 fsync）；
- 建议 `os.CreateTemp(dir, ".ob-*")` + `Chmod(0600)` + `Sync()` + rename。

### 7.11 [P2｜已确认] `gosec` 被显式禁用，且无替代的漏洞扫描

```yaml
# .golangci.yml:38-44
  disable:
    - forcetypeassert
    - mnd
    # enable later after baseline cleanup
    - gosec
```

- 全仓无 `govulncheck`（实测 `govulncheck NOT INSTALLED`），CI 无漏洞扫描（§5.1 C-5）；
- 无 Dependabot/Renovate → 依赖 CVE 不会被自动 PR；
- 依赖面不小：`go.mod` 61 个 require 行（15 个直接依赖 + 46 间接），含 `99designs/keyring`、`modelcontextprotocol/go-sdk`、`kin-openapi`、`bubbletea` 等；`npm audit --omit=dev` 显示 **0 vulnerabilities**（但项目无运行时 npm 依赖，该结论仅覆盖 devDependencies）。
- 建议（M）：`make vuln` = `govulncheck ./...` 入 CI；恢复 gosec（先 `--exclude-generated` 或用 baseline）；启用 Dependabot（同时需注意 §5.3 DCO-4 的 DCO 冲突）。

**判定：已确认流程缺口（P2）。**

### 7.12 [P3] 其他安全观察

| # | 项 | 结论 |
| --- | --- | --- |
| H-1 | 敏感数据入日志 | 生产代码几乎不打印请求头；`codegen` 有 Header/Body 掩码（唯独 URL 缺失，见 7.4）。整体**未发现** token 写日志的路径 ✅ |
| H-2 | 错误包装 | 多个错误只返回 `err`（`pkg/config` 中 6 处、`.cli` 中少量），丢失上下文（AGENTS.md §4.1 违规，P3） |
| H-3 | SSRF | `ob install --spec <url>` 会拉取任意 http(s) URL（含内网/云元数据地址）。这是本地 CLI 的显式用户输入，风险可接受，但**无响应体大小上限**（`io.ReadAll` 前未见 `LimitReader`？`parser.go` 有 30s timeout）→ 超大 spec 可耗尽内存（P3 加固建议：`io.LimitReader`，如 32MB） |
| H-4 | 证书/密钥路径校验 | `pkg/config/path_utils.go:118-238` 有 PEM 校验 ✅，但校验后的路径被写入配置却从未用于 TLS（7.7） |
| H-5 | 临时文件竞态 | 仅 `config.go:453`、`spec_cache.go:704` 两处，固定名 + 0644 + 无 fsync（见 7.10） |
| H-6 | panic 于库代码 | `pkg/mcp/progressive.go:118,121`（`panic("appCfg cannot be nil")`）、`pkg/codegen/codegen.go:63` → 若输入触发 nil，CLI 崩溃而非返回错误（P3）。同类：`os.Exit(1)` 在 `cmd/ob/main.go:57` 的 `init()` 中（见 §3.7） |
| H-7 | 版本信息伪造 | `User-Agent: "OpenBridge/1.0"` 硬编码（`pkg/spec/parser.go:317`、`pkg/config/spec_cache.go:468`），与真实版本无关 → 服务端风控/排障失真（P3，见 §9） |

---

## 8. 维度 7：发布与仓库卫生

### 8.1 GoReleaser

`goreleaser check` 实测通过：

```text
$ goreleaser check
  • checking                                  path=.goreleaser.yml
  • 1 configuration file(s) validated
  • thanks for using GoReleaser!
```

配置能力与缺口：

| 项 | 状态 | 证据 |
| --- | --- | --- |
| 多平台构建（linux/darwin/windows × amd64/arm64，忽略 windows/arm64） | ✅ | `.goreleaser.yml:18-40` |
| ldflags 注入 version/commit/date | ✅ | `.goreleaser.yml:20-24`；`Makefile:10` |
| SHA256 checksums | ✅ | `.goreleaser.yml:52-54` |
| **签名（cosign/gpg）** | ❌ 缺失 | 无 `signs:` 段 |
| **SBOM** | ❌ 缺失 | 无 `sboms:` 段 |
| **macOS 公证（notarize）** | ❌ 缺失 | 无 `notarize:` 段 |
| Homebrew/Snap/NFPM/docker | ❌ 注释掉（`.goreleaser.yml:136-160`） | 与 docs/RELEASE.md “Future Extensions” 一致 |
| `before.hooks: go mod tidy` | ⚠️ 非密闭 | `.goreleaser.yml:9`：发布时自动改 `go.mod/go.sum`，若 tidy 产生差异不会失败也不会被审阅；且发布产物内容依赖当时网络 |
| 归档格式（tar.gz+zip 混用） | ⚠️ | `archive.formats: [tar.gz, zip]` 对 linux 也产出 zip（冗余） |
| changelog 分组 | ✅ | `.goreleaser.yml:58-85` 与 Conventional Commits 对齐 |
| `release.draft: false` | ⚠️ | 直接发布，无 draft 审阅；`prerelease: auto` 尚可 |

**判定（P2 加固建议）**：发布产物无签名/无 SBOM/无公证，用户校验链不完整；建议启用 `cosign sign-blob` + `sboms: [syft]` + darwin `notarize`（需要 Apple 凭据），并把 `go mod tidy` 改为 CI 的 `go mod tidy -diff`（校验而非修改）。

### 8.2 README 特性声明 vs 实现一致性（抽查 8 条）

| # | 声明（README） | 实现 | 判定 |
| --- | --- | --- | --- |
| R-1 | `README.md:20` “**Zero Code Generation**: All behavior is dynamically driven by OpenAPI specifications” | 存在完整 `pkg/codegen`（5 文件/768 行，4 种语言生成器），CLI 有 `--generate/-g`、`--generate-output/-O`（`pkg/cli/handler.go:693-698`），安装向导有“Sensitive Info”步骤 | ❌ **矛盾**（P2）。正确表述：“运行时零代码生成，另提供可选代码片段生成” |
| R-2 | `README.md:26` “**Fast Startup**: Cold start under 100ms” | 实测热启动 5.4–6.8ms（`--version`/`list`/`--help` 各 5 次），但**首次执行（冷启动、页未缓存）610ms**；无 benchmark | ⚠️ **未验证**（P3）。字面“cold start”不成立，应为“warm start <10ms”；建议 benchmark 守护 |
| R-3 | `README.md:25` “**Secure Credentials**: Uses system keyring (Keychain, Credential Manager, Secret Service)” | keyring 库支持；但存在 FileBackend 硬编码口令回退（§7.2）且 CI 不测 | ⚠️ **部分成立**（P1，见 7.2） |
| R-4 | `README.md:24` “**Multi-Platform**: Works on Linux, macOS, and Windows” | build 矩阵覆盖；CI 三 OS 跑测试；windows/arm64 明确不支持（`docs/RELEASE.md`） | ✅ 基本成立 |
| R-5 | `README.md` “Shell Auto-Completion … enum values” | `pkg/completion` 实现资源/verb/flag 补全；`CompleteFlagValues` 有 enum 逻辑（`completion.go`）；但 `pkg/completion` 2 个函数 0% 覆盖，`completion.go:135-150` 无测试 | ⚠️ 功能存在，测试薄弱（P3） |
| R-6 | `README.md:150`/`docs/reference/cli.md:14`/`QUICKSTART.md:28` “`ob version`” | 实测 `./bin/ob version` → `unknown command "version" for "ob"`；只有 `--version` | ❌ **文档错误**（P2） |
| R-7 | `docs/guide/quick-start.md` MCP 配置示例 `args: ["run","myapi","--mcp"]` | `ob run <app> --mcp` 语义可用（`run` 子命令 `DisableFlagParsing` + `parseMCPServerArgs`），但文档未说明也可直接 `myapi --mcp` | ⚠️ 可用但易混淆（P3） |
| R-8 | `docs/reference/cli.md:36-42` `--json`/`--yaml` | 实际由 `parseCLIFlags`→`determineOutputFormat`（`pkg/cli/handler.go:358-369`）支持 ✅；补全列表也含（`completion.go:257`） | ✅ 一致（此行原疑为不一致，实测澄清） |

### 8.3 QUICKSTART / docs / README 与命令实测对照（抽查 5 条）

| # | 文档位置 | 文档内容 | 实测 | 判定 |
| --- | --- | --- | --- | --- |
| Q-1 | `QUICKSTART.md:28` | `./bin/ob version` | `unknown command "version"` | ❌ |
| Q-2 | `QUICKSTART.md:20-24` | `make build` 生成 `bin/ob` | 成功 | ✅ |
| Q-3 | `QUICKSTART.md:60` | `./bin/ob install todo --spec http://localhost:8000/openapi.json` | `install` 支持 `--spec`；但交互式提示需要 TTY（CI/管道下行为未验证） | ⚠️ |
| Q-4 | `docs/guide/installation.md` | `go install .../cmd/ob@latest` | `go.mod` 要求 go 1.25.5；`@latest` 在旧 Go 上失败（文档未提示 Go 版本） | ⚠️ P3 |
| Q-5 | `README.md:145-152` Command 表 | 列出 `ob version`、`ob help`、`ob completion` 等 | 表内仅 `ob version` 与实现不符（其余均存在，`ob help` 由 cobra 提供）；与 Q-1 同一处错误 | ❌ |

### 8.4 `package.json` / npm 元数据

```json
// package.json
{
  "name": "open-bridge",
  "version": "1.0.0",
  "main": "index.js",          // ← 文件不存在（P3）
  "keywords": [],              // ← 空（P3）
  "author": "",                // ← 空（P3）
  "license": "Apache-2.0",
  "devDependencies": {
    "@types/node": "^25.0.10", // ← 与 CI Node 20 不匹配（P3）
    "vitepress": "^1.6.4",     // ✅
    "vue": "^3.5.27"           // ✅
  }
  // 无 "engines"（P3）
}
```

- `main: index.js` 指向不存在的文件（实测 `ls index.js` → No such file），但本项目不是 npm 包，`main` 无实际用途 → 建议删除或改 `private: true`；
- 无 `engines`：CI 用 Node 20（`deploy-docs.yml:36`），`@types/node@25` 面向 Node 25 → 类型声明与运行时不匹配；
- `npm audit --omit=dev` → **0 vulnerabilities**（无生产 npm 依赖）；
- “两套依赖安全”：Go 依赖走 `go.sum`（无审计流水线）；npm 依赖只有 3 个 devDependencies（有 `package-lock.json` v3）✅。

### 8.5 仓库卫生

| 项 | 状态 | 证据 |
| --- | --- | --- |
| `bin/`（15MB 二进制） | 未 tracked ✅ | `git status --ignored` 显示 `!! bin/` |
| `coverage.out`(328K)/`coverage.html`(808K) | 未 tracked ✅，但**存在且过时** | `git status --ignored`；仓库版本 56.6% vs 实测 56.5% |
| `node_modules/`(93MB) | 未 tracked ✅ | `.gitignore:31` |
| `docs/.vitepress/dist`(2MB) | 未 tracked ✅ | `.gitignore:47` |
| `examples/todo-service/.venv`(14MB)、`__pycache__`(36K) | 未 tracked ✅（由 `examples/todo-service/.gitignore` 覆盖） | 顶层 `.gitignore` **没有** `.venv/`/`__pycache__/`，靠嵌套文件；若在其它位置建 venv 会出现在 `git status`（P3） |
| `.gitignore` 覆盖度 | 基本完整：`bin/`、coverage、node_modules、`docs/.vitepress/dist|cache`、IDE、OS；缺 `dist/`（通用）、`.venv/`、`__pycache__/`、`*.test`、`.env`（P3） |
| tracked 文件总数 | 132（79 `.go`、28 `.md`、5 `.yml`、5 `.json`…）；无大文件/二进制 ✅ | `git ls-files` |
| 安全/治理文件 | ❌ 无 `SECURITY.md`、无 `CODEOWNERS`、无 `.editorconfig` | `ls` 显示不存在（P3） |
| `proposals/`、`.kiro/`、`.claude/`、`.cursor/` | `.claude`、`.vscode` 被 ignore（实测 `!!`）；`.kiro/specs/openbridge/{design,requirements,tasks}.md` **已 tracked（3 个 md）**，属内部设计文档，建议确认是否应公开；`proposals/01_init.md` tracked | 可接受但需决策 |
| 许可证 | Apache-2.0 + NOTICE ✅ | `LICENSE`/`NOTICE`；无 CI 校验依赖许可证兼容性（P3） |

---

## 9. 维度 8：可观测性 / 运维

### 9.1 现状

| 能力 | 现状 | 证据 |
| --- | --- | --- |
| 日志框架 | **无**：`grep -rn 'log\.|log/slog|slog\.'` 在生产代码中 0 命中 | `pkg/ internal/ cmd/` 全量 grep |
| 日志级别/开关 | 无 `--verbose`/`--debug`/`OPENBRIDGE_DEBUG` | grep 0 命中 |
| 结构化错误 | 只有面向人类的彩色/分段文本（`pkg/cli/errors.go` 838 行） | — |
| 请求追踪 | 无 request-id、无 `httptrace`、无耗时统计 | `pkg/cli/handler.go:161` 直接 `Do` |
| 版本信息 | 仅在 `cmd/ob/main.go:35-39` 的 `version/commit/date`（ldflags 注入）；`User-Agent` 硬编码 `OpenBridge/1.0`（`pkg/spec/parser.go:317`、`pkg/config/spec_cache.go:468`） | — |
| 遥测 | 无（无 telemetry 依赖） | — |
| 退出码 | `main()` 统一 `os.Exit(1)`（`cmd/ob/main.go:91-98`），无细分退出码 | — |
| 错误丢弃 | 20 处 `_ =`（含 `install.go:306,550` 的警告路径） | grep |
| 性能指标 | 无（无 benchmark、无 pprof） | — |

### 9.2 影响评估（P2）

- **用户侧排障**：网络错误只给“Network connectivity issue + URL + 建议”（且 URL 可能泄露 key，§7.3），没有 `--verbose` 可看到实际请求方法/URL/耗时/重试；MCP 模式下服务端错误无法定位。
- **维护者侧排障**：CI 失败时无结构化日志可检索；`ob info` 展示的 TLS 配置是“死配置”（§7.7），会误导用户以为已生效。
- **安全审计**：无审计日志（谁在何时调用了哪个 API/用哪个 profile），对“AI 代理以用户身份操作生产 API”的产品定位是明显缺口（可观测性 + 合规双重问题）。
- **版本归因**：`User-Agent` 固定为 `OpenBridge/1.0`，服务端无法区分客户端版本；`--version` 输出格式（`ob version <ver> (commit: …, built: …)`）也无机器可读选项。

### 9.3 建议（M/L）

1. `internal/log` 薄封装（`slog` + `--verbose/-v` + `OPENBRIDGE_LOG`），默认 WARN 到 stderr，`--verbose` 打 URL（**脱敏后**）/状态码/耗时；
2. 请求层加 `X-OpenBridge-Version`/`X-Request-Id`（或 UA 注入真实版本）；
3. MCP 模式加审计日志（内存/文件可选），记录 tool 名、operationId、结果状态；
4. 增加 `ob doctor`（检查 keyring 后端、TLS 生效、配置权限、PATH shim），把 §7 的隐性状态显式化。

---

## 10. 发现汇总表

> 共 42 条，其中 P1 9 条、P2 26 条、P3 7 条；安全类标注“已确认”或“加固建议”。

| ID | 严重度 | 类别 | 标题 | 证据 |
| --- | --- | --- | --- | --- |
| F-01 | **P1** | 安全·已确认 | query 注入的 API key 在网络错误中明文回显 | `pkg/request/builder.go:141-150` + `pkg/cli/errors.go:120-128,151` |
| F-02 | **P1** | 安全·已确认 | codegen 输出未掩码 URL（MaskSecrets 失效） | `pkg/codegen/curl.go:41`；`nodejs.go:42`、`python.go:105`、`go.go:42,115` |
| F-03 | **P1** | 安全·已确认 | keyring 回退 FileBackend + 硬编码口令 | `pkg/credential/keyring.go:191-197,168-186`；`detectBackend:286-301` |
| F-04 | **P1** | 测试 | `-tags=integration` 编译失败，AGENTS §5.1 命令不可用 | `pkg/credential/keyring_test.go:219,261` vs `keyring_integration_test.go:219,261` |
| F-05 | **P1** | 测试 | 4/5 集成测试文件缺 build tag | `internal/integration/*.go:1`、`pkg/cli/errors_integration_test.go:1`、`pkg/config/install_integration_test.go:1`、`pkg/request/builder_integration_test.go:1` |
| F-06 | **P1** | 覆盖率 | 总覆盖 55.4%/56.5%，298/988 函数 0%，tui/router 0% | §2.2/§2.3 |
| F-07 | **P1** | AGENTS | property test 未覆盖 parser/formatter（`pkg/spec` 0 个） | §4.4 |
| F-08 | **P1** | 流程 | DCO 规则未落地：40 提交 35 缺 sign-off | §5.3 |
| F-09 | **P1** | CI | CI 从不运行 `-tags=integration`，F-04/F-05 永久隐形 | `ci.yml:82` |
| F-10 | P2 | 安全·已确认 | `ob info -o json/yaml` 未掩码 Header | `cmd/ob/main.go:602-632` vs `:719-731` |
| F-11 | P2 | 安全·已确认 | 掩码规则漏 Cookie/Proxy-Authorization 等 | `cmd/ob/main.go:767-776` |
| F-12 | P2 | 安全·已确认 | TLS 配置全链路未接线（无 crypto/tls） | `pkg/config/config.go:208-228`；grep 0 命中 |
| F-13 | P2 | 安全·已确认 | CLI/MCP 请求无超时；Profile.Timeout 未生效 | `pkg/cli/handler.go:45`、`pkg/mcp/handler.go:32`、`pkg/mcp/progressive.go:80` |
| F-14 | P2 | 安全·已确认 | config/cache 0644、目录 0755；tmp 固定名、无 fsync | `pkg/config/config.go:332,455`、`spec_cache.go:551-593,696-710` |
| F-15 | P2 | 安全·已确认 | gosec 禁用、无 govulncheck/Dependabot | `.golangci.yml:44`；CI 无扫描 |
| F-16 | P2 | 测试 | 30 处 t.Skip；keyring 在 CI 恒跳过；property 在 -short 全跳过 | §3.3 |
| F-17 | P2 | 测试 | property test 空转（错误即 return true） | `pkg/config/config_property_test.go:45-71`、`export_property_test.go:50-60` |
| F-18 | P2 | 测试 | 13 处 time.Sleep 的 watcher 用例 flaky 风险 | `pkg/config/spec_watcher_test.go:177,243,275,746…987` |
| F-19 | P2 | 测试 | 测试隔离破坏：init() 打开真实 keyring/创建真实目录 | `cmd/ob/main.go:52-90`、`internal/integration/integration_test.go:55` |
| F-20 | P2 | 测试 | 0 个 t.Parallel；0 Benchmark/Fuzz | §3.1 |
| F-21 | P2 | 测试 | `cmd/ob` 19%、`pkg/cli` 31.5%：CLI 出口层未测 | §2.2 |
| F-22 | P2 | 测试 | `internal/testutil` 仅 1 消费者且 0% 覆盖 | `pkg/completion/completion_test.go:8` |
| F-23 | P2 | AGENTS | 文件配对违规（router/tui/ mcp server_factory/shared 无测试；internal/integration 孤儿包） | §4.3 |
| F-24 | P2 | CI | golangci-lint `latest` + 本地 v1 与 v2 配置冲突 | `ci.yml:106-109`、`Makefile:30` |
| F-25 | P2 | CI | 无 vuln/CodeQL/dependabot/license 检查；coverage 无门禁 | `ci.yml:84-91` |
| F-26 | P2 | CI | coverage 只 ubuntu、fail_ci_if_error:false；无 PR 注释 | `ci.yml:84-91` |
| F-27 | P2 | CI | 手动 actions/cache 路径对 macOS/Windows 无效；setup-go 已内置 | `ci.yml:68-76` |
| F-28 | P2 | CI | Node 20 已 EOL；docs workflow 无 PR 预检 | `deploy-docs.yml:3-10,36` |
| F-29 | P2 | Makefile | `make install` 因 GOPATH 为空必失败 | `Makefile:170-176` + 实测 |
| F-30 | P2 | Makefile | `GOLANGCI_LINT_VERSION=latest` 不可复现 | `Makefile:30` |
| F-31 | P2 | Makefile | 无 vet/test-integration/check 目标，与 AGENTS 检查清单不对应 | `Makefile:1-2,91-102` |
| F-32 | P2 | 发布 | goreleaser 无签名/SBOM/公证；`before: go mod tidy` 非密闭 | `.goreleaser.yml:9,52-54` |
| F-33 | P2 | 文档 | README “Zero Code Generation” 与 pkg/codegen 矛盾 | `README.md:20`、`pkg/cli/handler.go:693` |
| F-34 | P2 | 文档 | `ob version` 文档化但命令不存在 | `README.md:150`、`docs/reference/cli.md:14`、`QUICKSTART.md:28` |
| F-35 | P2 | 可观测性 | 零日志/无 verbose/UA 硬编码；审计缺失 | §9 |
| F-36 | P3 | Makefile | `fmt-fix` 的 gofmt -r 语义风险 | `Makefile:124,126` |
| F-37 | P3 | Makefile | `docs` 仍用 godoc（已淘汰）；check-tools 检查它 | `Makefile:149,187-190` |
| F-38 | P3 | Makefile | `clean` 不完整 | `Makefile:197-199` |
| F-39 | P3 | 仓库 | package.json main 不存在、keywords 空、无 engines、@types/node 25 vs Node 20 | `package.json:5,19` |
| F-40 | P3 | 仓库 | 缺 SECURITY.md/CODEOWNERS/.editorconfig；.gitignore 缺通用 .venv/dist | §8.5 |
| F-41 | P3 | 安全·加固建议 | shim appName 未在导出方法内部校验；Windows % 展开 | `pkg/config/install.go:563-620` |
| F-42 | P3 | 测试 | 单测反向依赖 integration testdata；`real_api_test.go` 名不副实 | `pkg/spec/parser_test.go:990,1421` |

---

## 11. 优先级行动清单

### S（≤3 天，立即修复；全部为低成本高收益）

| # | 行动 | 对应 | 验收 |
| --- | --- | --- | --- |
| S-1 | 统一 URL 脱敏：`errors.formatNetworkError` + 4 个 codegen 生成器 + `ob info -o json/yaml` 输出前 redact | F-01/02/10 | 新增 `TestFormatNetworkErrorRedactsQuery`、`TestGenerateRedactsQueryAPIKey`、`TestInfoJSONMasksHeaders`；grep 证明无直接打印 `req.URL.String()`（未掩码） |
| S-2 | 修复 `-tags=integration` 编译：删除 `keyring_test.go` 与 `keyring_integration_test.go` 的 2 组重名函数 | F-04 | `go vet -tags=integration ./... && go test -tags=integration ./pkg/credential` 通过 |
| S-3 | 给 4 个未打 tag 的 `*_integration_test.go` 加 `//go:build integration`（或按内容改名合并为单测） | F-05 | `go list -f '{{.GoFiles}}'` / `go vet -tags=integration ./...` 验证；`go test -short ./...` 不再执行 `internal/integration` |
| S-4 | CI 增加 `go vet ./...` 与 `go test -tags=integration ./...` 步骤 | F-09 | workflow 出现两步；故意破坏 tag 时能红 |
| S-5 | `make install` 改用 `go env GOBIN`/`go env GOPATH` + `install -d` | F-29 | `make install` 在未导出 GOPATH 的机器成功 |
| S-6 | 默认禁用 keyring FileBackend；若必须启用，口令来自 env/prompt，禁止 `FixedStringPrompt`；`ob info` 显示真实后端 | F-03 | 新增单测：无桌面后端时 `NewManager` 返回错误而非静默使用固定口令 |
| S-7 | 修 `ob version` 文档（改 `ob --version`）并为 CLI 输出函数补最小测试（`list/info` 用 buffer） | F-34、F-21 | 文档测试命令可执行；`cmd/ob` 覆盖率 ≥35% |
| S-8 | `golangci-lint` 版本 pin（Makefile 与 CI 统一到同一 v2.x），本地 `make lint` 可在干净环境复现 | F-24 | `make lint` 在 CI 与本地同版本通过 |

### M（1–2 周）

| # | 行动 | 对应 |
| --- | --- | --- |
| M-1 | 为 `pkg/spec`（parser + format）补 property test：`Decode(Encode(x))==x`、YAML/JSON 等价、URL/路径解析不变量；同时修掉现有 property test 的 `return true` 空转 | F-07/17 |
| M-2 | 给 `pkg/tui/install` 至少补 model 状态机单测（Update/View 纯函数化，1347 行无需真实 TTY），目标 ≥50% | F-06/23 |
| M-3 | 给 `pkg/router` 补 12 个函数的表驱动测试（路由分流/未知参数/多 profile） | F-06/23 |
| M-4 | TLS/TImeout 接入：共享 `http.Transport`（CA/cert/skip、MinVersion）+ `profile.Timeout` 生效，并补 httptest TLS 测试 | F-12/13 |
| M-5 | CI 安全流水线：`govulncheck` + restore `gosec`（baseline）+ Dependabot（处理 DCO bot 豁免） + `goreleaser check` | F-15/25/32 |
| M-6 | 覆盖率门禁：`codecov.yml` 设 project/patch 阈值（如 project 55%、patch 70%），`fail_ci_if_error: true`；coverage 上传三 OS 中至少 ubuntu 强制 | F-06/26 |
| M-7 | 消除 `time.Sleep`：改用 `require.Eventually`/`wait.Poll`；为 watcher 测试加 `-race -count=10` 的 CI 稳定性任务（或单独 nightly） | F-18 |
| M-8 | DCO：改用 `tim-actions/dco`（校验 signer==author）；对 merge/bot 提交显式豁免；在 CONTRIBUTING.md 强调 `-s`，并用 pre-commit hook 降低违规率 | F-08 |
| M-9 | 配置/缓存权限收紧 0700/0600 + `os.CreateTemp` + `Sync`；`ob info` 文档标注 headers 不要放密钥 | F-14 |
| M-10 | README/QUICKSTART/docs 三处同步：Zero Codegen 表述、`ob version`、MCP 启动方式、`--generate` 文档化 | F-33/34 |
| M-11 | 发布签名与 SBOM：cosign + syft + （可选）notarize；`before.hooks` 的 `go mod tidy` 改为 `-diff` 校验 | F-32 |
| M-12 | 测试隔离：`init()` 去 I/O；测试统一 `t.Setenv("OPENBRIDGE_CONFIG_DIR", t.TempDir())` + `TestMain`；`t.Parallel` 在无共享状态的包逐步引入 | F-19/20 |

### L（1–2 月，体系化改进）

| # | 行动 | 对应 |
| --- | --- | --- |
| L-1 | 覆盖率提升到 70%：以 `cmd/ob`/`pkg/cli`/`tui`/`router` 为重点（当前合计 ~1,790 未覆盖语句的 60%） | F-06/21 |
| L-2 | 可观测性体系：`internal/log`（slog）+ `--verbose` + 请求耗时/状态 + 脱敏审计日志 + `ob doctor` | F-35 |
| L-3 | 安全左移：`Semgrep/CodeQL` + `gosec` 全量 + secret scanning（若托管在 GitHub 启用 push protection） + 依赖许可证检查 | F-15/25 |
| L-4 | 供应链：actions pin 到 SHA（renovate 自动升级）、发布 provenance（SLSA）、checksums 签名、Homebrew/Snap 渠道 | F-32、C-9 |
| L-5 | 基准测试与性能门禁：`BenchmarkStartup`/`BenchmarkParse`，把 “<100ms” 变成可执行断言 | R-2 |
| L-6 | 测试架构：统一断言库（testify）与 helper 规范（`t.Helper`）、消除 5 套重复 `test_helpers.go`、删除或补齐 `internal/testutil` | F-20/22 |

---

## 12. 如果只做三件事

### 第一件：堵住凭据外泄链（S-1，1–3 天，收益/成本比最高）

一条**已确认可利用**的链路：query 型 API key → 被 `injectAPIKey` 写进 URL → ①网络错误 `url.Error.URL` 原样打印（`pkg/cli/errors.go:120-128`）；②`--generate` 四种语言生成器的 URL 未掩码（`pkg/codegen/*.go`）；③`ob info -o json/yaml` 未掩码 Header（`cmd/ob/main.go:602-632`）。修复只需一个 `redactURL` + 一个 `redactConfig`，各配 1 个测试即可。

**为什么第一**：这是产品“Secure Credentials / 面向 AI 的敏感信息掩码”卖点的直接反例；泄露目标包括终端 scrollback、CI 日志、issue 截图、LLM 上下文。先堵住，再谈其它。

### 第二件：让“规定的测试”真的能跑（S-2 + S-3 + S-4，2–4 天）

当前 `go vet -tags=integration ./...` **编译失败**（F-04）；4/5 个集成测试文件**没打 tag**（F-05）；CI **从不跑 tagged 测试**（F-09）。三者叠加的结果：AGENTS.md §3.3/§5.1 的测试隔离约定完全失效，而 CI 绿灯给出虚假安全感。修复成本极低（删 2 组重名函数、加 4 个 tag 行、CI 加 2 个 step），却能让“集成测试”从口号变成可执行。

**为什么第二**：这是所有测试质量问题的“放大器”——没有这条流水线，后续 M-1/M-2/M-3 写的测试同样可能不被执行。

### 第三件：给 CI 装上“安全与覆盖率的最小门禁”（M-5 + M-6，1–2 周）

- `govulncheck ./...` + 恢复 `gosec`（baseline）+ Dependabot（注意 DCO bot 豁免）；
- `codecov.yml` 阈值 + `fail_ci_if_error: true`，把当前 55.4% 设为 floor，禁止倒退；
- golangci-lint 版本 pin（消除 v1/v2 漂移）；
- 顺带把 `make install`、`ob version` 两条“确定性坏”的开发者体验修掉。

**为什么第三**：前两件是“修现状”，第三件是“防复发”。没有门禁，下一次重构（如本分支的 “comprehensive code simplification”）会再次把覆盖率与安全性带走。

---

## 13. 附录

### 13.1 环境与工具版本

```text
OS:        macOS arm64 (darwin)
Go:        go1.27.3 darwin/arm64（go.mod 要求 1.25.5，CI 矩阵 1.25）
golangci-lint: v1.64.8 (built with go1.26.1) —— 与仓库 v2 配置不兼容（见 §5.5）
goreleaser:    v2.13.3
staticcheck:   2025.1.1 (0.6.1)
govulncheck:   NOT INSTALLED
gosec:         NOT INSTALLED
godoc:         NOT INSTALLED
node/npm:      v24.22.2 / 11.19.1
```

### 13.2 关键原始输出

**(1) 覆盖率（-short）**

```text
cmd/ob                  coverage: 19.0%
internal/integration    coverage: [no statements]     ← 无 build tag，仍在运行
internal/proptest       coverage: 53.8%
internal/testutil       coverage: 0.0%
pkg/cli                 coverage: 31.5%
pkg/codegen             coverage: 70.7%
pkg/completion          coverage: 72.9%
pkg/config              coverage: 73.8%
pkg/credential          coverage: 58.4%
pkg/mcp                 coverage: 78.0%
pkg/request             coverage: 85.1%
pkg/router              coverage: 0.0%
pkg/semantic            coverage: 85.5%
pkg/spec                coverage: 78.0%
pkg/tui/install         coverage: 0.0%
total:                  (statements) 55.4%
```

**(2) 覆盖率（full）**：`total: 56.5%`；credential 71.8%、config 74.8%（其余同 -short）。

**(3) `-tags=integration` 编译失败**

```text
$ go test -tags=integration ./pkg/credential
pkg/credential/keyring_integration_test.go:261:6: other declaration of TestWithPassBackend
FAIL	github.com/nomagicln/open-bridge/pkg/credential [build failed]

$ go vet -tags=integration ./...
vet: pkg/credential/keyring_test.go:219:6: TestGetPlatformInfo redeclared in this block
```

**(4) `make install`**

```text
$ make install
CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=v0.1.3-4-gdd18374 ..." -o bin/ob ./cmd/ob
Error: GOPATH is not set
make: *** [install] Error 1
```

**(5) `go vet ./...`**：退出码 0，无输出。

**(6) `golangci-lint` 本地冲突**

```text
$ golangci-lint run ./pkg/config
Error: can't load config: sort-results should be 'true' to use sort-order
```

**(7) `goreleaser check`**

```text
  • checking                                  path=.goreleaser.yml
  • 1 configuration file(s) validated
  • thanks for using GoReleaser!
```

**(8) `npm audit --omit=dev`**：`found 0 vulnerabilities`。

**(9) DCO 现状**

```text
最近 40 个提交中 35 个缺 Signed-off-by（HEAD dd18374、2f3f669 均缺）
仓库共 1 个 merge commit（8842f4f）——无 sign-off，落在 DCO 检查范围
```

**(10) 启动耗时实测**

```text
./bin/ob --version   min=5.9ms  med=6.2ms  max=6.8ms   (5 次)
./bin/ob list        min=6.1ms  med=6.4ms  max=6.5ms
./bin/ob --help      min=5.4ms  med=5.8ms  max=6.6ms
首次（冷启动、page cache 空）：real 0.61s
```

**(11) 真实配置目录残留（测试隔离证据）**

```text
~/Library/Application Support/openbridge/apps/
  harbor.yaml    spec_source: ~/Downloads/swagger.yaml   (2026-01-25 23:13)
  testapp.yaml   spec_source: /tmp/test_spec.yaml        (2026-01-25 22:08)
  testbody.yaml  spec_source: /tmp/test_body_spec.yaml   (2026-01-25 22:39)
目录权限：drwxr-xr-x (0755)；配置文件：-rw-r--r-- (0644)
```

### 13.3 方法与局限

- **只读原则**：未修改任何源码/配置；未 `go mod tidy`/`gofmt -w`/`lint --fix`；只在 `/tmp` 生成覆盖率文件（`/tmp/cover.out`、`/tmp/cover_full.out`、`/tmp/cover_unit.out`）。`make install` 失败前有 `make build`，会覆盖 gitignored 的 `bin/ob`（已存在，内容为构建产物），不影响仓库状态（`git status` 干净）。
- **未做的验证（环境限制）**：
  - 无 GitHub API/网络访问，无法核实 Actions 运行历史、CodeQL 是否在 GitHub 端启用（历史提交 `69d8ff7 "Potential fix for code scanning alert no. 4"` 提示曾有过 CodeQL，但仓库内无配置）；
  - `govulncheck` 未安装，未做依赖 CVE 扫描（仅记录缺失）；
  - 无 Windows 运行环境，Windows CI 行为（make 可用性、%* 转发、keyring 跳过）为“需 CI 日志核实”的推断；
  - 无可用真实 API/keyring 交互（安全评估基于代码路径，未执行真实凭据请求）。
- **严重度口径**：P1 指“路径明确、后果为凭据泄露/功能不可用/规则形同虚设”，P2 指“需特定条件或纵深防御缺口”，P3 指“加固建议/低影响”。所有安全结论均标注“已确认”或“加固建议”。
- **行号说明**：行号基于当前工作区 `dd18374`；若后续提交改动，请以内容为准。

---

*Report generated by process-auditor for task-4. 仅交付本文件；未改动仓库任何其它文件。*
