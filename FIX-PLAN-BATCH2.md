# OpenBridge 修复计划 —— 批次 2（派生索引、死代码清理、MCP 客户端、测试覆盖）

> 上游：[`FIXES-BATCH1.md`](./FIXES-BATCH1.md) §5、[`proposals/02_persistent_cache.md`](./proposals/02_persistent_cache.md)
> 基线：批次 1 最新提交 `f31af20`（已推送 origin）；工作树干净
> 原则：TDD、契约先行、文件级写作用域隔离、不修改 `.golangci.yml`、不新增第三方依赖、DCO 签名提交
> 本轮**不做**（批次 3）：`init()` 组合根重构、`pkg/config` 拆分、错误模型全量 sentinel 化、派生索引 P3/P4（命令级 help、执行路径）

---

## 1. 范围与负责人

| 工作流 | 内容 | 对应 | 负责人 | 写作用域 |
| --- | --- | --- | --- | --- |
| **W1 派生索引** | P1 存储层（gob DTO、键/失效/原子写/损坏自愈、版本戳）+ P2 接线（app help 与 shell completion 走索引，不再解析完整 spec） | proposals/02；D-05 | perf-analyst | `pkg/spec/derived_index*.go`（新）、`pkg/spec/parser.go`、`pkg/spec/parser_test.go`、`pkg/semantic/*`（DerivedRulesVersion）、`pkg/completion/*`、`pkg/cli/handler.go`（仅 help 路径）、`pkg/cli/handler_test.go`（仅 help 测试） |
| **W2 死代码清理** | 删除已被证伪/无生产调用的 `pkg/config/spec_watcher.go`（778 行）、`spec_cache.go`（816 行）及其测试；移除 `fsnotify` 依赖引用（go.mod 由 Lead tidy）；确保 build/test 全绿 | 诊断 F-42/性能 F11/提案 §3.2 | process-auditor | `pkg/config/spec_watcher.go`、`pkg/config/spec_watcher_test.go`、`pkg/config/spec_cache.go`、`pkg/config/spec_cache_test.go`（均为删除），必要时删除引用点 |
| **W3 MCP 客户端接线** | MCP Handler / ProgressiveHandler 使用 `request.NewHTTPClient(profile)` + `DoWithRetry`（per-profile TLS/超时/重试），保留现有 30s 兜底；按 profile 缓存 client | FIXES-BATCH1 §5（MCP TLS 接线） | sec-fixer | `pkg/mcp/handler.go`、`pkg/mcp/handler_test.go`、`pkg/mcp/progressive.go`、`pkg/mcp/progressive_test.go`、新增 `pkg/mcp/client_test.go`（如需） |
| **W4 测试覆盖** | `pkg/tui/install` 状态机测试（Update/View 纯函数化覆盖，目标 ≥50%）、`internal/testutil` 复用/清理；不追求 UI 像素级断言 | 覆盖率盲区（tui 0%） | arch-reviewer | `pkg/tui/install/*_test.go`（新）、`pkg/tui/install/test_helpers.go`（如需，新）、`internal/testutil/*`（仅测试工具） |
| **W5 汇总/走线** | 派生索引缓存构造接线、文档、`go mod tidy`（移除 fsnotify）、终验、DCO 提交、交付报告 | — | Lead | `cmd/ob/main.go`、`go.mod`/`go.sum`、`FIX-PLAN-BATCH2.md`、`FIXES-BATCH2.md`、docs 相关 |

## 2. 契约

### C-C1（W1）派生索引
```go
// pkg/spec（新文件）
type DerivedIndex struct { /* gob DTO：版本戳 + 源键 + 资源/动词/子资源/操作摘要 */ }

// ParserOption：索引缓存目录（空=禁用）
func WithIndexCache(dir string) ParserOption

// 从 spec 构建并落盘（或命中读回）；供 app help/completion 使用
func (p *Parser) LoadDerivedIndex(ctx context.Context, source, appName string) (*DerivedIndex, error)
```
- **DTO 必须显式定义且 gob 可编码**（不得直接 gob `openapi3.T` / `semantic.CommandTree` 中可能含不可导出字段的结构；转换函数负责映射）。
- 内容：命令树（资源/动词/子资源/operation 引用）+ app help 所需操作摘要（method/path/operationId/summary/tags），不含完整 schema。
- 缓存键/失效：
  - 本地：`abs_path + size + mtimeNanos + ParserVersion + IndexSchemaVersion`；
  - 远端：复用 W1（批次 1）远端缓存的 `SHA256` + `URL + ParserVersion + IndexSchemaVersion`；
  - `ParserVersion` 至少包含 kin-openapi 版本 + `semantic.DerivedRulesVersion`（新增常量，规则变更时手工+1）+ DTO `IndexSchemaVersion`。
- 文件格式：`magic + formatVersion + CRC32C + gob payload`，tmp+fsync+rename 原子写，0600；损坏/版本不匹配 → 删除并按 miss 回退（重建）。
- 接线：
  - `pkg/cli/handler.go` 的 app help（`showAppHelp` 及资源/动词列表渲染）优先用索引；索引不可用时回退现有 spec 解析。
  - `pkg/completion` 的 `CompleteVerbs`/`CompleteResources` 等命令补全走索引；flag/enum 补全可继续回退 spec（P3 再优化）。
  - 不改变帮助文本内容与退出码。
- 验收基准：4MB 合成 spec 上 `ob <app> --help` 与 completion 的耗时对比（前/后），目标 help 冷路径 ≤ 15ms 级、无 spec 解析。

### C-C2（W2）死代码清理
- 删除前确认：`grep -rn 'SpecCacheManager\|NewSpecCacheManager\|SpecWatcherManager\|LocalFileWatcher\|RemoteSpecWatcher' --include='*.go' | grep -v _test` 仅剩被删文件与测试；若有生产引用先报告 Lead。
- 删除 4 个文件（含测试），若 `pkg/config` 其他文件引用了被删类型（如 `SpecFetchOptions` 保留在 spec 包），做最小调整。
- 不做 `go.mod` 修改（Lead 执行 `go mod tidy` 移除 `fsnotify`）。
- 验收：`go build ./...`、`go vet ./...`、`go test -short ./...` 全绿；给出删除行数统计。

### C-C3（W3）MCP 客户端
- `NewHandler(..., httpClient *http.Client)` 保持签名；当传入 nil 时，per-call 根据 profile 构造 client（`httpClientForProfile(profile)`）并缓存（`sync.Map`/mutex，key=profile 名称或配置哈希）；
- 标准路径 `executeTool` 与 progressive `InvokeTool` 使用 `request.NewHTTPClient(profile)` + `request.DoWithRetry`；
- 失败构造 client（非法 TLS 配置）→ 返回 MCP `isError`，不 panic；
- 调用方显式注入的 `httpClient`（测试）优先，不被覆盖；
- 不改变 SafetyConfig 行为与工具 schema。
- 验收：现有 MCP 测试全绿 + 新增 TLS/超时/重试 e2e 测试（httptest TLS 自签 + 503 重试）。

### C-C4（W4）TUI 测试
- 以 `pkg/tui/install/model.go` 的 `Model.Update`/`View` 纯函数为入口，构造按键序列（`tea.KeyMsg`）覆盖：步骤前进/后退、auth 选择、spec 输入校验、TLS 输入、确认/取消、错误态；
- 目标：`pkg/tui/install` 覆盖率从 0% 提升到 ≥50%（`go test -cover ./pkg/tui/install`）；
- 若需要可测试性小重构（如注入 reader/writer 已存在），优先测试而非改业务逻辑；改动文件仅限 `pkg/tui/install` 测试文件与 `internal/testutil`。

### C-C5（W5）Lead 走线
- `cmd/ob/main.go` 的 `spec.NewParser(...)` 增加 `spec.WithIndexCache(<config-dir>/index-cache)`；
- `go mod tidy` 移除 fsnotify（W2 删除后）；govulncheck 保持 0 可达；
- 文档：派生索引/缓存目录说明；
- 全量门禁 + e2e（批次 0/1 回归 + 批次 2 新增场景）+ DCO 分组提交 + `FIXES-BATCH2.md`。

## 3. 验收矩阵

| 检查 | 命令 | 标准 |
| --- | --- | --- |
| 构建 | `go build ./...` | 0 |
| 格式 | `gofmt -l` / `goimports -l` | 0 |
| 静态 | `go vet ./...`、`go vet -tags=integration ./...` | 0 |
| 单测/竞态 | `go test -short -count=1 ./...`、`go test -race -short -count=1 ./...` | 全绿 |
| 集成 | `go test -tags=integration -count=1 ./...` | 全绿 |
| Lint | `/tmp/gobin2/golangci-lint run ./...` | 0 issues |
| 漏洞 | `/tmp/gobin2/govulncheck ./...` | 0 可达 |
| 覆盖率 | `go test -short -coverprofile` + `pkg/tui/install` 覆盖 | ≥61.7%，TUI ≥50% |
| 性能 | 4MB spec help/completion 前后对比 | help ≤15ms 级、completion 明显下降 |
| 文档 | `npm run docs:build` | 通过 |
| e2e | 批次 0（22）、批次 1（18）、批次 2 新增（索引/清理/MCP） | 全部 PASS |

## 4. 冲突规避

| 风险 | 缓解 |
| --- | --- |
| W1 与 W5 都改 `cmd/ob/main.go` | W1 不改 main；W5 只加构造参数 |
| W1 与 W4 都改 `pkg/cli/handler*.go` | W4 不碰 pkg/cli（仅 TUI 测试）；W1 独占 help 路径 |
| W2 删除文件与 W1 spec 改动 | 不同包（config vs spec），无交集 |
| W3 与 W2 都改 go.mod | 都不改 go.mod；Lead 统一 tidy |
| 并行中间态 | 各跑自己包测试；Lead 终验 |

## 5. 执行顺序

1. 并行 W1–W4
2. Lead：W5 接线 + go mod tidy + docs
3. 终验（含 e2e 回归与新场景）
4. DCO 分组提交 + `FIXES-BATCH2.md`


---

## 6. 执行结果（2026-11-07，批次 2 完成）

| 工作流 | 状态 | 关键证据 |
| --- | :---: | --- |
| W1 派生索引 P1+P2 | ✅ | help 热路径 4.09MB 11.3ms（11.9×）、2.6k ops 10.4ms（25.5×）、补全 145→11.4ms；索引原子写+CRC+版本戳；e2e A2–A6 PASS |
| W2 死代码清理 | ✅ | 删除 4 文件 −3260 行；fsnotify 从 go.mod/构建图移除；全量测试绿 |
| W3 MCP per-profile 客户端 | ✅ | CA/无 CA/非法 CA/503 重试全部 e2e PASS；14 新测试 |
| W4 TUI 覆盖 + W4b bug 修复 | ✅ | 0%→99.2%；修复 shim 选择丢失（P1）/SearchEngine 丢失/placeholder 当值；回归测试锁定 |
| W5 Lead 走线 | ✅ | index-cache 接线、go mod tidy、文档、终验、提交 |
| 全量门禁 | ✅ | build/vet 双 tag/short/race/integration/lint(0)/govulncheck(0)/docs；覆盖率 71.9%；e2e 20/20 + 批次 1 回归 18/18 + 批次 0 回归 22/22 |

交付汇总见 [`FIXES-BATCH2.md`](./FIXES-BATCH2.md)。批次 3 候选见该报告 §5（派生索引 P3/P4、组合根/config 拆分、错误模型、索引裁剪、依赖治理）。
