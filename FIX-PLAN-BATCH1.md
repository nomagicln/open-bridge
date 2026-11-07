# OpenBridge 修复计划 —— 批次 1（性能、配置接线、MCP 安全、门禁）

> 上游：[`ENGINEERING-DIAGNOSIS.md`](./ENGINEERING-DIAGNOSIS.md) §9 批次 1、[`FIXES-BATCH0.md`](./FIXES-BATCH0.md) §7
> 基线：批次 0 已提交（最新提交 `954a28e`），工作树干净
> 原则：TDD（先红后绿）、契约先行、文件级写作用域隔离、不修改 `.golangci.yml`/`go.mod` 依赖、DCO 签名提交
> 本轮**不做**：`init()` 组合根重构、`pkg/config` 拆分、错误模型全量 sentinel 化（批次 2；其中持久化派生索引先做 spike）

---

## 1. 范围与负责人

| 工作流 | 内容 | 对应诊断 | 负责人 | 写作用域 |
| --- | --- | --- | --- | --- |
| **W1 性能/缓存** | 远端 spec 持久化缓存读路径（ETag/条件请求/离线 stale 回退）；install 去掉第二次下载；`ob list`/`ob info` 读配置中的 SpecInfo 不解析 spec | D-06、D-15 | perf-analyst | `pkg/spec/parser.go`、`pkg/spec/parser_test.go`、`pkg/config/spec_cache.go`、`pkg/config/spec_cache_test.go`、`pkg/config/config.go`、`pkg/config/install.go`、`pkg/config/install_test.go`、`pkg/config/config_test.go`、新增 `pkg/spec/persistent_cache.go`（如需） |
| **W2 配置接线** | `request.NewHTTPClient(profile)`：Timeout/TLS（CA/mTLS/skip/SNI/MinVersion）；重试（可重放 body、幂等/可重试状态码、退避）；CLI 请求走新 client + 重试 | D-08、D-10 | style-auditor | `pkg/request/client.go`（新）、`pkg/request/client_test.go`（新）、`pkg/cli/handler.go`、`pkg/cli/handler_test.go` |
| **W3 MCP 安全控制** | `require_confirm`（两阶段确认参数）、`max_requests_per_minute`（令牌桶）、`dangerous_operation_patterns`（正则拦截）；覆盖 Handler 与 ProgressiveHandler 两条路径 | D-09 | sec-fixer | `pkg/mcp/safety.go`（新）、`pkg/mcp/safety_test.go`（新）、`pkg/mcp/handler.go`、`pkg/mcp/handler_test.go`、`pkg/mcp/registry.go`、`pkg/mcp/registry_test.go`、`pkg/mcp/progressive.go`、`pkg/mcp/progressive_test.go` |
| **W4 CI 门禁** | `codecov.yml` 覆盖率阈值（以当前 59.2% 为 floor）；`govulncheck` 步骤；`dependabot.yml`（gomod/actions/npm）；CodeQL workflow；Makefile `check` 聚合目标 | D-16 | process-auditor | `.github/workflows/ci.yml`、`.github/workflows/codeql.yml`（新）、`.github/dependabot.yml`（新）、`codecov.yml`（新）、`Makefile` |
| **W5 缓存 spike** | 持久化派生索引（命令树 / 操作目录）可行性：gob 编码/解码、体积、失效键设计、与完整 `openapi3.T` 解析的成本对比；产出设计提案 + 实测数据 | D-05（为批次 2 铺路） | arch-reviewer | `proposals/02_persistent_cache.md`（新）+ `/tmp` 基准（不改源码） |
| **W6 汇总/走线** | `pkg/spec` 缓存构造接线（`cmd/ob/main.go`，一行级）、文档同步、终验、DCO 提交、交付报告 | — | Lead | `cmd/ob/main.go`、`FIX-PLAN-BATCH1.md`、`FIXES-BATCH1.md`、docs 相关 |

## 2. 契约（跨任务先对齐）

### C-B1（W1）远端 spec 持久化缓存
- 位置：`pkg/spec` 内部实现（不引入 `spec → config` 依赖；缓存目录通过 `ParserOption` 传入）。
- 建议 API：
  ```go
  func WithRemoteCache(dir string) ParserOption      // dir 为空 = 禁用
  ```
- 行为（HTTP(S) spec）：
  1. 命中缓存且 `ExpiresAt` 未过 → 直接读缓存内容解析，不发请求；
  2. 过期 → 发条件请求（`If-None-Match`/`If-Modified-Since`）；304 → 复用缓存；
  3. 非 304 → 更新缓存（元数据 + 内容，均原子写 tmp+rename）；
  4. 网络错误 → 回退 stale 缓存（若有），否则返回原错误；
  5. 本地文件 spec 行为不变。
- 缓存键：URL；元数据含 `URL/ETag/Last-Modified/FetchedAt/ExpiresAt/SHA256`。
- install 流程：删除第二次 `FetchWithCache` 下载（runtime 首次加载会写缓存）；保留 `install` 时解析。
- `ob list/info`：`AppConfig` 新增持久化字段（如 `spec_info: {title, version, operation_count}` 或复用既有 `operation_count` + `spec_title`/`spec_version`），install 时写入；`GetInstalledAppInfo` 优先读配置，字段缺失才回退解析（兼容旧配置）。

### C-B2（W2）HTTP client 与重试
```go
// pkg/request/client.go
func NewHTTPClient(profile *config.Profile) (*http.Client, error)
func DoWithRetry(client *http.Client, req *http.Request, retry config.RetryConfig) (*http.Response, error)
```
- `NewHTTPClient`：`Timeout` 默认 30s（`profile.Timeout.Duration > 0` 时覆盖）；TLS：`InsecureSkipVerify`、`CAFile`、`ClientCert/Key`、`ServerName`、`MinVersion("1.2"/"1.3")`；证书/配置非法返回 `%w` 错误；默认 Transport 复用（`http.DefaultTransport` clone 基础上改 TLS）。
- `DoWithRetry`：仅对幂等语义安全场景重试（GET/HEAD/PUT/DELETE/OPTIONS 或配置允许）；重试条件 = 网络错误（非 context cancel）或 `RetryableStatusCodes`；退避 = 初始 200ms 指数 ×2 上限 2s，总次数 `MaxRetries`；body 必须可重放（缓冲 `GetBody`）；`RetryConfig` 全零 = 不重试。
- CLI handler：`executeAPIRequest` 用 `NewHTTPClient(profile)` + `DoWithRetry`；`Handler.httpClient` 保留作为测试注入/兜底（默认 30s）。
- 注意：**不修改** `pkg/mcp/*`（W3 所有）；MCP 的 profile TLS 接线留批次 2。

### C-B3（W3）MCP 安全控制
- `require_confirm`：语义为"该方法需要显式确认"。
  - Handler/Progressive 在调用前检查：若 method ∈ RequireConfirm 且未确认 → 返回 `isError` 结果：`"operation requires confirmation; re-invoke with _confirm=true"`；
  - 工具 inputSchema 增加可选布尔参数 `_confirm`（不改变既有参数）；`InvokeTool`/`SearchTools` 路径一致；
  - 确认后正常执行（是否记忆？**不做**记忆，逐次确认）。
- `max_requests_per_minute`：进程内令牌桶（`golang.org/x/time/rate` 需新增依赖 → **禁止**；用 `time.Ticker`/互斥计数自实现，窗口 1 分钟滑动或固定窗口均可，文档写清）；超限返回 `isError` 并带剩余等待秒数。
- `dangerous_operation_patterns`：编译为正则（配置校验失败 → 启动报错），匹配 `METHOD path` 或 tool ID；命中 → 拒绝并返回原因。
- 只读/白/黑名单行为不变；`SafetyConfig` 字段语义写进注释 + 测试矩阵（配置→行为）。
- 并发安全：Handler 可能被并发调用，限流/计数需加锁。

### C-B4（W4）CI 门禁
- `codecov.yml`：`coverage.status.project.default.target: auto` + `threshold: 1%`（或 floor 58%）；patch target 70%；`fail_ci_if_error` 保持 false（fork token 问题），阈值由 codecov 状态检查执行。
- CI 增加 `govulncheck`（ubuntu，`go install golang.org/x/vuln/cmd/govulncheck@v1.1.4` 或 `go run`；固定版本）。
- `.github/dependabot.yml`：gomod weekly、github-actions weekly、npm weekly（docs）。
- `.github/workflows/codeql.yml`：Go + Actions，`on: push/pr + schedule`，权限最小化。
- Makefile 新增 `check`（fmt 校验 + vet 双 tag + lint + test-short），并同步 help/.PHONY。

### C-B5（W5）缓存 spike 交付
- `proposals/02_persistent_cache.md`：候选方案对比（a. 缓存完整 `openapi3.T` JSON/gob；b. 缓存派生命令树 + 操作目录；c. 解析优化如跳过 validate）；实测数据（编码/解码耗时、体积、失效策略）；推荐方案 + 批次 2 实施步骤 + 风险。
- 基准放 `/tmp/cache-spike/`，不修改仓库源码。

## 3. 验收矩阵（Lead 最终执行）

| 检查 | 命令 | 标准 |
| --- | --- | --- |
| 构建 | `go build ./...` | 0 |
| 格式 | `gofmt -l pkg cmd internal` / `goimports -l` | 0 |
| 静态 | `go vet ./...`、`go vet -tags=integration ./...` | 0 |
| 单测 | `go test -short -count=1 ./...` | 全绿 |
| Race | `go test -race -short -count=1 ./...` | 全绿 |
| 集成 | `go test -tags=integration -count=1 ./...` | 全绿 |
| Lint | `/tmp/gobin2/golangci-lint run ./...` | 0 issues |
| e2e（新增） | 远端 spec 缓存命中计数、TLS 自签测试、Retry 计数、MCP 安全三项 | PASS |
| 覆盖率 | `go test -short -coverprofile` | ≥ 59.2%（不倒退） |
| 文档 | `npm run docs:build` | 通过 |
| 提交 | `git commit -s` 分组 | 全部 DCO 签名 |

## 4. 冲突规避

| 风险 | 缓解 |
| --- | --- |
| W1 与 W6 都改 `cmd/ob/main.go` | W1 不碰 main；W6 只做一行构造接线 |
| W2 与 W3 都碰 `pkg/mcp` | W2 仅改 CLI 路径，MCP 留批次 2；W3 独占 `pkg/mcp/*` |
| W1 与 W2 都碰 `pkg/cli/handler.go` | W1 不碰 handler；parser 缓存构造在 main 完成 |
| 并行中间态编译失败 | 各成员只跑自己包；Lead 最终统一验证 |
| 新依赖 | 禁止新增第三方依赖（限流/正则/编码用标准库） |

## 5. 执行顺序

1. 并行：W1/W2/W3/W4/W5（5 路）
2. Lead：W6 接线（等 W1 契约就绪）+ 文档
3. 独立验收：沿用 `/tmp/lead_verify.py` + 新增批次 1 场景（Lead 或复用验证 harness）
4. 全量门禁 + DCO 分组提交 + `FIXES-BATCH1.md`


---

## 6. 执行结果（2026-11-07，批次 1 完成）

| 工作流 | 状态 | 关键证据 |
| --- | :---: | --- |
| W1 远端 spec 缓存 + install 去重 + list/info 去解析 | ✅ | e2e A1–A5/B1–B2：install GET=1、首跑缓存、重跑 0 请求、离线可用、删 spec 后 list/info 正常；`ob list` ~6.9ms |
| W2 profile Timeout/TLS/Retry 接线 | ✅ | e2e C1/C2/D1/D2：自签拒绝、自定义 CA 通过、300ms 超时、503 重试 3 次成功；新增 22 测试 |
| W3 MCP 安全控制 | ✅ | e2e E1–E5：确认/不记忆/限流/危险模式全部生效；28 个新增/子测试 |
| W4 CI 门禁 | ✅ | codecov.yml、govulncheck（阻塞）、Dependabot、CodeQL、`make check`；`make check` 本地通过 |
| W5 派生索引 spike | ✅ | `proposals/02_persistent_cache.md`（374 行）：b) gob DTO 推荐；4MB help 269ms→~8.5ms 预估 |
| W6 走线 + 依赖安全升级 | ✅ | parser 缓存接线、MCP 安全启动硬校验、info 元数据展示；kin-openapi/go-sdk 升级后 govulncheck 0 可达漏洞 |
| 全量门禁 | ✅ | build/vet 双 tag/short/race/integration/lint(0)/govulncheck(0)/docs build；覆盖率 61.7%；e2e 18/18 + 批次 0 回归 22/22 |

交付汇总见 [`FIXES-BATCH1.md`](./FIXES-BATCH1.md)。批次 2 候选（持久化派生索引 P1–P2、执行路径 P4、死代码清理、`init()`/config 拆分、MCP TLS 接线）见该报告 §5。
