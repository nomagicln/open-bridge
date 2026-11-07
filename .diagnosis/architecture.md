# OpenBridge 架构与代码设计诊断报告

| 项 | 值 |
| --- | --- |
| 项目 | `github.com/nomagicln/open-bridge`（OpenAPI → 语义 CLI + MCP 运行时引擎，Go） |
| 诊断基线 | 工作树 `dd18374`（`fix: resolve linting issues and improve code quality`） |
| 代码规模 | 非测试 Go **18,420 行** / 测试 **17,527 行**；79 个 `.go` 文件、15 个包 |
| 诊断范围 | 组合根/DI、包边界、全局状态、接口设计、错误模型、职责膨胀、扩展机制、数据模型、生命周期、文档漂移 |
| 约束 | 只读诊断；未修改任何源码/配置。唯一写入 `.diagnosis/architecture.md` |
| 方法 | 静态阅读 + `go list` 依赖图 + 黑盒实测（局部 mock HTTP server / 启动计时 / race）+ 交叉验证 Lead 线索 |
| 基线复验 | `go build ./...` 通过（产物成功）；`go vet ./...` 通过；`go test -race -short ./pkg/{mcp,config,semantic,spec}` 全绿 |

> 状态标注：**【已确认】** = 代码引用或命令实测直接证明；**【推断】** = 由代码结构推导、当前测试/运行路径未触发，但改动后极易显现。
> 严重度：**P0**（阻断/安全/核心功能失效）、**P1**（高，影响可靠性或架构可持续性）、**P2**（中，明确技术债）、**P3**（低，风格/可维护性）。

---

## 0. 执行摘要

### 0.1 总评

这是一个**"单进程、单命令、自上而下线性执行"**的 CLI 工具，被按"可复用的多接口运行时引擎"来组织目录，但两者之间的张力没有收敛：

- **优点真实存在**：依赖 DAG 无环（`go list` 可证）；`spec`/`semantic` 包内聚且测试密度高（535 个 Test、7 个 Property Test）；配置写入使用 tmp+rename 原子替换；MCP 的 progressive disclosure 三 meta-tool 设计思路清晰；`options` 模式（`ManagerOption`/`ParserOption`）已在局部使用。
- **主要问题不是"代码写得脏"，而是"承诺 > 实现"**：持久化 spec 缓存只写不读、CLI 认证传空 appName 导致 keyring 永远查不到、`Profile.TLSConfig/Timeout/RetryConfig` 等大量配置字段运行期零接线、`SpecWatcher`/`ProfileManager`/`export` 等约 4.5k 行"能力"在生产路径零调用、文档承诺的 `ob version` / `--json` / `ob <app>` 直连均不工作。
- **架构形态问题**：组合根被 `cmd/ob/main.go` 的 `init()` 全局变量与 `pkg/router` 两处重复实现；`pkg/config` 成为 10.4k 行的"上帝包"；`pkg/cli` 同时承担协议解析、HTTP 执行、错误美化、帮助渲染、代码生成；`pkg/` 导出 733 个符号却没有 API 稳定性策略。
- **风险判断**：当前功能面窄（单进程一条命令），所以不少问题"暂时不爆"；但一旦加入"多 app 同进程服务"、"离线/私有 spec"、"TLS"、"重试"、"真正的跨进程缓存"——这些正是 README 承诺的能力——现有结构会成组失效。

### 0.2 关键发现 Top 12

| # | ID | 严重度 | 标题 | 核心证据 |
| --- | --- | --- | --- | --- |
| 1 | F-01 | **P0** | CLI 认证整体失效：`InjectAuth` 传空 appName，keyring 键永不命中且错误被静默吞掉 | `pkg/cli/handler.go:133`、`pkg/request/builder.go:107-118`、`pkg/credential/keyring.go:316` |
| 2 | F-38 | **P1** | "跨进程持久化 spec 缓存"是空壳：每次 CLI 调用都重新下载 spec；install 时缓存写入后无人读取 | `pkg/spec/parser.go:835-839`、`pkg/config/install.go:407-416`、实测访问日志 |
| 3 | F-02 | **P1** | 组合根在 `init()`：全局可变依赖 + 凭据失败静默降级 + 无法测试 | `cmd/ob/main.go:39-89`、`main_test.go:460-469` |
| 4 | F-05 | **P1** | `pkg/config` 上帝包（领域模型 + IO + install/shim + TUI 提示 + cache + watcher + export/profile） | `pkg/config` 10,448 行 / 243 导出符号；`install.go:662-849` |
| 5 | F-06 | **P1** | 双组合根/双 MCP 启动路径：`main.go` 与 `router` 各一份且行为不一致 | `cmd/ob/main.go:901-951` vs `pkg/router/router.go:214-250` |
| 6 | F-22 | **P1** | 错误模型名不副实：838 行 `errors.go` 主要是文案渲染 + 字符串嗅探分类；全仓仅 1 个 sentinel error | `pkg/cli/errors.go:47-89`、`pkg/mcp/shared.go:13` |
| 7 | F-39 | **P1** | 配置面 ≠ 执行面：TLS/Timeout/Retry/RequireConfirm/限流/QueryParams/SpecFetchAuth 等全部未接线 | `config.go:79,90-143,210-315`；`crypto/tls` 全仓 0 引用；F-39 正文矩阵 |
| 8 | F-48 | **P1** | `ob <app> ...` 直连路由不可达（cobra legacyArgs 拦截），与帮助文本矛盾；路由依赖 argv[0] 文件名 | `cmd/ob/main.go:126-137`、`pkg/router/router.go:74-96`；实测 `ob demo pets list` |
| 9 | F-41 | **P1** | HTTP 客户端全部无超时；`Profile.Timeout` 是"只读不生效"的死字段 | `pkg/cli/handler.go:45`、`pkg/mcp/handler.go:32`、`pkg/mcp/progressive.go:80` |
| 10 | F-18 | **P2** | 接口设计反向：只定义 4 个接口、组件间全用具体类型；`ToolSearchEngine` 单实现伪接口；导出面 733 符号 | `pkg/mcp/engine.go:40-71`；`go list`/grep 统计 |
| 11 | F-42 | **P2** | 死代码/未接线 ≈4.5k 行：SpecWatcher 778 行、SpecCacheManager 部分、ProfileManager 581 行、export 680 行无生产消费者 | `pkg/config/spec_watcher.go:647`、`pkg/config/profile.go`、`pkg/config/export.go` |
| 12 | F-46/F-47 | **P2** | 文档漂移：`ob version` 不存在、`--json/--yaml` 被静默忽略、"Zero Code Generation"与 `pkg/codegen` 并存 | `docs/reference/cli.md:18`、`pkg/cli/handler.go:289-315,358-380` |

### 0.3 度量基线

| 指标 | 值 | 采集方式 |
| --- | --- | --- |
| 非测试源码行 | 18,420 | `find . -name '*.go' ! -name '*_test.go' \| xargs wc -l` |
| 测试源码行 | 17,527（测试/源码 ≈ 0.95） | 同上，`*_test.go` |
| `pkg/` 导出符号 | **733**（`config` 243、`credential` 114、`spec` 104、`mcp` 82、`semantic` 65、`cli` 54、`request` 28、`codegen` 26） | `grep -rhoE '^(func\|type\|var\|const) [A-Z]...'` |
| 非测试接口定义 | **4** 个（`config.SpecWatcher`、`spec.UnmarshalStrategy`、`mcp.ToolSearchEngine`、`codegen.Generator`） | `grep '^type .* interface'` |
| Sentinel error | **1** 个（`pkg/mcp/shared.go:13` `errAppConfigNotSet`） | `grep 'var Err[A-Z]\|var err.*= errors.New'` |
| 库代码 `panic()` | **3** 处（见 F-24） | `grep -rn 'panic('` |
| 显式丢弃错误 `_ =` | **46 处**（非测试） | `grep -rn '_ = '` |
| `context.Background()` 硬编码 | **10 处**（非测试） | `grep -rn 'context.Background()'` |
| `Test*` 函数 / Property Test | 535 / 7 | `grep -c '^func Test'` |
| 带 `//go:build integration` 的集成测试文件 | 1 / 4（AGENTS.md 3.1 要求全部带） | `grep -rln 'go:build integration'` |
| 启动耗时（本机 macOS，warm） | `--version` 中位 **6.2ms**（Lead 测 6.9ms）；首次执行 770ms（Gatekeeper/page-in） | Python subprocess ×20 |
| 无 Benchmark | 全仓 `func Benchmark` = 0 | grep |

### 0.4 与任务书/Lead 线索的核对结论

| 前提 | 核对结果 |
| --- | --- |
| "pkg/config 承担领域模型+IO+缓存+watch+export+install" | **成立**，且更严重：还承担 TUI 提示词与 shell/PATH 逻辑（F-05） |
| "pkg/router 同时依赖 cli/config/mcp/spec 是否违反依赖方向" | 部分成立：方向本身是"顶层编排"，不算倒置；真正问题是 **router 是第二个组合根**，且 `handleOBCommand` 为事实不可达分支（F-06、F-07） |
| "全局可变状态：codegen registry、semantic 全局 slice、config 单例" | **需修正**：`pkg/config` **没有单例**；单例存在于 `cmd/ob` 包级变量。semantic 的全局 slice 确实导出且可变（F-15、F-16） |
| "progressive.go 用 panic 而不是 error" | **成立**，2 处编程错误 panic，见 F-24 |
| Lead 线索 1：CLI `InjectAuth` 空 appName | **已独立验证成立，且升级为 P0**（F-01） |
| Lead 线索 2：配置面/执行面矩阵 | **成立**，本文补全为完整矩阵（F-39） |
| Lead 线索 3：`LoadSpecWithPersistentCache` 空壳、SpecWatcher 零实例化 | **成立**，另发现 install 阶段同一 spec 下载两次（F-38） |
| Lead 线索 4：`ob <app>` 路由不可达、argv[0] 依赖 | **成立**；实测 `ob demo pets list` → `unknown command "demo"`（F-48） |
| Lead 线索 5：`handleOBCommand` 不可达 | **基本成立**：仅在 `__debug_` 命名约定下可达（`router.go:85-91`），生产路径等价死代码（F-48；相关：F-47） |

---

## 1. 诊断方法与复现环境

### 1.1 方法

1. **静态**：逐文件阅读核心路径（`cmd/ob`、`pkg/{router,cli,config,spec,mcp,semantic,request,codegen,completion,tui}`），并对每个结论记录 `file:line`。
2. **依赖图**：用 `go list -deps` 生成包级依赖矩阵（附录 A），据此判断分层与扇出。
3. **黑盒**：构建 `/tmp/ob` 二进制，用本地 mock HTTP server 统计 spec 拉取次数；用 Python 计时器测启动；用 `ob <app>` / `--output json` / `ob version` 验证文档承诺。
4. **交叉验证**：对 Lead 提供的 5 条线索全部独立复核（§0.4），只保留我在代码/实测中能复现的部分。
5. **并发**：`go test -race -short` 覆盖 `mcp/config/semantic/spec` 四包，结果全绿——因此并发类结论明确标注为"当前不可触发/推断"，避免把风险写成已发生的 bug。

### 1.2 复现命令清单

```bash
# 0) 基线
go build ./... && go vet ./... && go test -short ./...

# 1) 依赖矩阵（附录 A）
go list -deps ./pkg/router | grep open-bridge

# 2) 缓存空壳 + 每次命令重复拉 spec（F-38）
#    见附录 C 的 mock server 脚本；实测计数：
#    install: 2 次 GET /openapi.json
#    ob run demo pets list  #1: +2（spec + API）
#    ob run demo pets list  #2: +2
#    ob run demo --help:     +1
wc -l /tmp/obsrv/access.log

# 3) CLI 认证键不匹配（F-01）
grep -n 'InjectAuth' pkg/cli/handler.go pkg/mcp/handler.go pkg/mcp/progressive.go
grep -n 'func buildKey' -A3 pkg/credential/keyring.go   # ob__<app>__<profile>
# CLI 传入 "" => key "ob____<profile>"，install 写入的是 "ob__<app>__<profile>"

# 4) 文档命令实测
/tmp/ob version                 # => unknown command "version" for "ob"（exit 1）
/tmp/ob run demo pets list --output json   # 与默认输出完全一致（--json/--yaml 同）
/tmp/ob demo pets list          # => unknown command "demo" for "ob"

# 5) 启动计时
python3 - <<'PY'
import subprocess,time,statistics
ts=[]
for _ in range(20):
    t=time.perf_counter(); subprocess.run(["/tmp/ob","--version"],capture_output=True); ts.append((time.perf_counter()-t)*1000)
print(statistics.median(ts[1:]), min(ts[1:]), max(ts[1:]))
PY

# 6) 并发（当前无 race 报告）
go test -race -short ./pkg/mcp ./pkg/config ./pkg/semantic ./pkg/spec

# 7) 未接线字段（F-07）
grep -rn 'crypto/tls' --include='*.go' .            # 0 行
grep -rn 'RequireConfirm\|MaxRequestsPerMinute\|DangerousOperationPatterns\|HybridSearch' --include='*.go' pkg cmd  # 仅 config.go 结构定义
grep -rn 'BuildSpecFetchOptions\|QueryParams' --include='*.go' pkg cmd | grep -v _test  # 仅 config/export/profile
```

### 1.3 阅读顺序说明

本报告按任务书 10 个维度组织（§2–§11），发现编号 `F-01..F-54` 全局唯一。每条发现都给出：严重度、状态（已确认/推断）、证据位置、验证方式、重构建议、工作量（S ≤ 0.5d，M ≤ 2d，L ≥ 1w）。§12 给出按优先级合并后的行动清单与"只做三件事"。

---

## 2. 组合根与依赖注入（维度 1）

### F-01 CLI 认证路径传空 appName，keyring 键永不命中且错误被静默吞掉 —— **P0【已确认】**

| 项 | 内容 |
| --- | --- |
| 位置 | `pkg/cli/handler.go:133`；放大点 `pkg/request/builder.go:107-118`；键构造 `pkg/credential/keyring.go:312-317` |
| 状态 | 已确认（静态推导 + 与 MCP 路径对照 + 键格式可证伪） |

**证据链**（三段拼起来就是完整 bug）：

```go
// pkg/cli/handler.go:122-139 —— CLI 执行路径
req, err := h.reqBuilder.BuildRequest(op.Method, op.Path, profile.BaseURL, params, opSpec.Parameters, requestBody)
...
if err := h.reqBuilder.InjectAuth(req, "", profile.Name, &profile.Auth); err != nil {   // ← 空 appName
    return nil, fmt.Errorf("failed to inject auth: %w", err)
}

// pkg/request/builder.go:107-118
func (b *Builder) InjectAuth(req *http.Request, appName, profileName string, authConfig *config.AuthConfig) error {
    if b.credMgr == nil { return nil }
    cred, err := b.credMgr.GetCredential(appName, profileName)
    if err != nil {
        return nil // No credential, skip auth      ← 任何错误都被吞掉
    }
    return b.injectAuthCredentials(req, authConfig, cred)
}

// pkg/credential/keyring.go:312-317 / 382-388
func buildKey(appName, profileName string) string { return fmt.Sprintf("%s__%s__%s", KeyringPrefix, appName, profileName) }
// CLI 传 ""        => "ob____default"
// install 写入     => "ob__demo__default"   (cmd/ob/main.go:231 StoreCredential(appName, "default", cred))
```

**影响**：
- 所有依赖凭证的类型（`bearer`/`api_key`/`basic`）在 **CLI 模式下 100% 不生效**，请求以未认证发出，服务端返回 401/403 时用户只看到 HTTP 错误，而不提示"凭证未找到"。
- 与 MCP 路径形成对照：`mcp/handler.go:138`、`mcp/progressive.go:475` 传 `h.appConfig.Name`，说明作者本意是传 app 名——这是重构（`9546058 refactor: comprehensive code simplification`）时被参数化后漏传的典型 DI 事故。
- **安全含义**：凭证静默降级 + `InjectAuth` 吞错，等于"配置了 auth 却永远不发送"，且没有任何日志。用户会误以为已认证（安装输出还会打印 `Credentials stored securely`，`cmd/ob/main.go:234`）。

**验证**：`grep -rn 'InjectAuth' pkg cmd`；对比三处调用参数。可加一条回归测试：以 file backend 建 `credMgr`，`StoreCredential("demo","default",...)` 后走 `ExecuteCommand`，断言请求头含 `Authorization`（当前必失败）。

**修复建议**（S）：`InjectAuth` 的签名改为 `InjectAuth(req, appName, profileName, cfg)` 本来是对的，问题在调用点；同时把 `InjectAuth` 改为返回 `ErrCredentialNotFound` 让调用方决定是否降级（例如 `--no-auth`），并在 CLI 层对 401 响应给出针对性提示。**不要**再把 "no credential" 与 "credential backend failure" 混为 `nil`。

### F-02 组合根在 `init()`：全局依赖、I/O 副作用、失败策略不一致 —— **P1【已确认】**

| 项 | 内容 |
| --- | --- |
| 位置 | `cmd/ob/main.go:38-89`（全局变量 + `init()`），`cmd/ob/main_test.go:460-469` |
| 状态 | 已确认 |

```go
// cmd/ob/main.go:38-49
var (
    configMgr *config.Manager; specParser *spec.Parser; credMgr *credential.Manager
    mapper *semantic.Mapper; reqBuilder *request.Builder; cliHandler *cli.Handler
    mcpHandler *mcp.Handler; appRouter *router.Router; completionHelper *completion.Provider
)
// cmd/ob/main.go:51-89
func init() {
    configMgr, err = config.NewManager()          // 失败 => os.Exit(1)，无法测试
    if err != nil { ...; os.Exit(1) }
    specParser = spec.NewParser()
    credMgr, err = credential.NewManager()        // 失败 => 仅 warning，继续
    if err != nil { fmt.Fprintf(os.Stderr, "Warning: credential manager unavailable: %v\n", err) }
    reqBuilder = request.NewBuilder(credMgr)      // 可能持有 nil credMgr
    cliHandler = cli.NewHandler(specParser, mapper, reqBuilder, configMgr)
    mcpHandler = mcp.NewHandler(mapper, reqBuilder, nil)   // nil httpClient 语义
    appRouter = router.NewRouter(cliHandler, mcpHandler, configMgr, specParser)
    completionHelper = completion.NewProvider(configMgr, specParser, mapper)
}
```

**具体问题**：

1. **失败策略自相矛盾**：config 失败 `os.Exit(1)`（在 `init()` 里！），credential 失败只 warning；`main()` 的 `Execute()` 错误处理（`main.go:91-99`）完全感知不到。三种失败等级（fatal / degrade / silent）没有统一出口。
2. **I/O 发生在 init**：`config.NewManager` 会 `MkdirAll`（`config.go:350-357`），`credential.NewManager` 调 `keyring.Open`（`keyring.go:221`）。实测 `ob --help` / `ob completion bash` 都会创建 `~/Library/Application Support/openbridge/apps`（我用 `HOME=/tmp/...` 实测复现）。shell 补全每次 Tab 都会触发这两个 I/O——虽然当前 6ms 可接受，但违背"补全应极轻"的常识，且 keyring 在 Linux/D-Bus 不可用时的降级路径不可控。
3. **不可测试**：`main_test.go:460-469` 的注释直接承认了这一点：

```go
// We need to temporarily replace the global configMgr
originalConfigMgr := configMgr
defer func() { configMgr = originalConfigMgr }()
configMgr = mgr
```

   测试只能覆盖 `createCredentialFromParams`/`mergeExistingConfig`/`parseMCPServerArgs` 等纯函数（见 `main_test.go` 10 个 Test 名单），**`Execute()`、`runInstallCmd`、`runCommand`、`startMCPServer` 无法测试**。
4. **全局单例的隐含前提**：`mcpHandler` 是进程级单例，`router.startMCPServer` 与 `main.startMCPServer` 都对它调用 `SetSpec/SetAppConfig`（`handler.go:42-51`）。单 app 单进程时可接受；一旦"一个 ob 进程服务多个 app"（MCP 多租户/`sse` 场景），就是数据竞争 + 串台。

**修复建议**（M）：把 `init()` 改成显式 `main()` 内的 `newAppContext() (*AppDeps, error)`：

```go
type AppDeps struct {
    Config   *config.Manager
    Parser   *spec.Parser
    Creds    credential.Manager   // 接口，或 nil-object
    // per-command 构造：Handler/Router 不再进全局
}
func newAppDeps() (*AppDeps, error)   // 统一错误策略，可注入 fake
func run(ctx context.Context, args []string, deps *AppDeps) error   // 可测
```

`main()` 只负责 `os.Exit`。`config.NewManager` 的目录创建改到第一次写时（`ensureDir` 已存在，可复用），读路径不产生副作用。

### F-03 依赖全部是具体类型，无消费方接口，mock 只能靠真实文件系统 —— **P2【已确认】**

- `cli.NewHandler(specParser, mapper, reqBuilder, configMgr)`（`pkg/cli/handler.go:35-51`）、`mcp.NewHandler`、`completion.NewProvider`、`router.NewRouter` 的字段类型全是 `*config.Manager`、`*spec.Parser` 等具体指针。
- 全仓非测试接口仅 4 个（§0.3），且**没有一个是"消费方为自身测试而定义"**的——与 AGENTS.md §4.2 "Always define dependencies as interfaces" 直接冲突。
- 直接后果：所有 handler 测试必须落真实 `config.Manager`（temp dir）和真实 `spec.Parser`（http server），于是测试规模膨胀（`builder_test.go` 1506 行、`spec/parser_test.go` 1868 行）且慢。
- **建议**（M）：在 `pkg/cli`/`pkg/mcp` 内部定义最小接口（`SpecLoader`、`CredentialStore`、`ConfigStore`），实现方在 `pkg/config`/`pkg/credential`；接口放消费方而非实现方。这是"可测试性"性价比最高的一步。

### F-04 构造顺序隐式、nil 语义重载 —— **P3【已确认】**

- `reqBuilder = request.NewBuilder(credMgr)` 在 `credMgr` 可能为 nil 时构造（`main.go:76`）；`Builder.InjectAuth` 里 `if b.credMgr == nil { return nil }`（`builder.go:108-110`）——nil 既是"没有 keyring"也是"跳过 auth"，与 F-01 的吞错叠加。
- `mcp.NewHandler(mapper, reqBuilder, nil)`（`main.go:82`）与 `mcp.NewProgressiveHandler(reqBuilder, nil, engineType)`（`main.go:926`/`router.go:195-199`）的 `nil` 表示"用默认 http.Client"；而 `NewProgressiveHandler` 内部 `if httpClient == nil { httpClient = &http.Client{} }`（`progressive.go:79-81`）。三个 Handler 各自 new 一个无超时 client（F-09）。
- **建议**（S）：统一 `deps` 结构体 + 构造函数返回 error；nil 不允许出现在构造函数参数里（用 options 的默认值替代）。

---

## 3. 包边界与分层（维度 2）

### 3.1 现状：依赖矩阵（`go list -deps`，2026-11 基线）

| 包 | 直接（含传递）依赖 open-bridge 包 |
| --- | --- |
| `cmd/ob` | codegen, spec, config, credential, request, semantic, cli, completion, mcp, router, tui/install |
| `pkg/router` | cli, config, mcp, spec（传递：codegen, credential, request, semantic） |
| `pkg/cli` | codegen, spec, config, credential, request, semantic |
| `pkg/mcp` | spec, config, credential, request, semantic |
| `pkg/completion` | spec, config, semantic |
| `pkg/request` | spec, config, credential |
| `pkg/config` | spec |
| `pkg/tui/install` | spec, config |
| `pkg/spec` / `pkg/semantic` / `pkg/codegen` / `pkg/credential` | 无内部依赖（叶子） |

**正面结论（已确认）**：当前 DAG 无循环，`spec`/`semantic`/`codegen`/`credential` 是干净叶子。这是必须保留的结构资产。

**风险点（推断）**：
- `config → spec` 是唯一"领域包依赖解析器"的边；`spec` 目前是叶子，但一旦 `spec` 需要读配置（例如 cache 目录），就会形成 **config ↔ spec 循环**。`LoadSpecWithPersistentCache(ctx, source, appName)` 的 `appName` 参数（`parser.go:837`）正是这种"解析器想知道 app 配置"的破口，目前它被忽略（F-02），等于循环压力被暂时掩盖。
- `router` 对 `cli`+`mcp` 的双依赖是"编排层"的合理代价，但它同时持有 `config.Manager`（用于 `GetAppConfig`）与 `spec.Parser`（用于加载 spec），说明 **router 已经越权承担了"应用装配 + 执行"两件事**（F-05）。

### F-05 `pkg/config` 上帝包：九类职责糅在 10,448 行/243 个导出符号里 —— **P1【已确认】**

| 职责 | 代表文件:行 | 说明 |
| --- | --- | --- |
| 领域模型 | `config.go:18-303`（`AppConfig/Profile/AuthConfig/TLSConfig/SafetyConfig/RetryConfig`） | 合理 |
| 持久化/IO | `config.go:331-494`（`NewManager/GetAppConfig/SaveAppConfig/writeConfigAtomically`） | 合理 |
| 安装编排 | `install.go:345-427`（`InstallApp/doInstall`，含 spec 加载） | 越界：安装 = CLI 用例 |
| Shim/Shell/PATH | `install.go:563-755`（`CreateShim/IsShimDirInPath/GetPathInstructions/getDefaultShimDir`） | 越界：OS 集成 |
| 交互式提示（TUI 文本） | `install.go:774-849`（`promptString/promptYesNo/promptChoice`），`promptForMissingInfo:500` | 越界：表现层 |
| 远端 spec 缓存 | `spec_cache.go`（816 行，HTTP + ETag + 原子写） | 越界：应属 `pkg/spec` 或独立 `internal/specstore` |
| 文件监听 | `spec_watcher.go`（778 行，fsnotify + 轮询 + 远端刷新，**生产零实例化**） | 越界且死代码 |
| 导入/导出 | `export.go`（680 行，V1/V2 schema + 校验）、`profile.go`（581 行，`ProfileManager`） | 越界且无 CLI 入口（F-24） |
| 路径策略 | `path_utils.go`（261 行，TLS 文件校验） | 可接受 |

副作用：
- 任何使用 `config.AppConfig` 类型的地方都被迫链接 fsnotify / HTTP 客户端 / os 交互代码（`go list -deps` 可见 `pkg/cli` 传递依赖 fsnotify）。
- 测试文件 `spec_watcher_test.go` 1001 行 + `spec_cache_test.go` 665 行，占 pkg/config 测试的 ~46%，维护的是**死代码**。
- `public API 面` 243 个符号中至少 1/3 无语义归属。

**拆分建议**（L，可渐进）：
1. `pkg/config` 只留 `AppConfig`/`Profile`/`Manager`（读/写/校验）≈ 1.2k 行。
2. `pkg/install`（或 `internal/install`）：`InstallApp/uninstall/shim/prompts`，依赖 `config`+`spec`。
3. `pkg/speccache`（或 `internal/specstore`）：`SpecCacheManager` + `FetchWithCache`。
4. `pkg/spec`：加 `Watcher`（若将来真要用）或 **直接删除 `spec_watcher.go`**（F-42 建议删除）。
5. `pkg/config/export.go`+`profile.go`：若 `ob profile export/import` 不是产品需求，删除；若是，落到 `internal/commands` 并从 public 面移除。

### F-06 双组合根 / 双 MCP 启动路径，且两份实现已经漂移 —— **P1【已确认】**

`ob run <app> ... --mcp` 走 `cmd/ob/main.go:901-951`；shim 模式（二进制名 = app 名）走 `pkg/router/router.go:214-250`。两者做同一件事但已经不一致：

| 行为 | `main.startMCPServer`（main.go:902） | `router.startMCPServer`（router.go:215） | 差异后果 |
| --- | --- | --- | --- |
| spec 加载 | `specParser.LoadSpec`（**无缓存参数**，`main.go:903`） | `LoadSpecWithPersistentCache(ctx, source, appName)`（`router.go:217`） | 两条路径未来接缓存时行为不同 |
| MCP server 版本号 | `mcp.NewServerFactory(appConfig.Name, version)`（构建版本，`main.go:915`） | `mcp.NewServerFactory(appConfig.Name, "1.0")`（`router.go:229`） | 客户端看到的 server version 不同 |
| Progressive 构造 | `mcp.NewProgressiveHandler(reqBuilder, nil, engineType)`（`main.go:926`） | `mcp.NewProgressiveHandler(r.mcpHandler.GetRequestBuilder(), nil, engineType)`（`router.go:195-199`） | 前者依赖全局，后者依赖 handler getter |
| SetAppConfig/SetSpec 顺序 | `SetSpec` → `SetAppConfig`（`main.go:934-937`，progressive 未用 `SetAppConfig` 的 panic 风险） | `SetAppConfig` → `SetSpec`（`router.go:204-205`） | 若 `SetSpec` 失败，前者已 panic 前置条件满足，后者不满足 |
| 参数解析 | `parseMCPServerArgs`（`main.go:869-899`） | `mcpOptions.parseTransport/parsePort`（`router.go:133-176`） | 两套手写 arg parser，行为细节会漂移 |
| 错误信息 | `failed to parse search engine type`（main.go:923） | `invalid search engine type`（router.go:191） | 文案不一致 |

**修复方向**（M）：只保留一个 `pkg/mcpapp.NewServer(ctx, appConfig, profileName, opts, deps)`；`main` 与 `router` 都调用它。router 退化为"根据 argv 计算 (appName, args, mode) 的纯函数"，不持有 `configMgr`/`specParser`。

### F-07 `pkg/router` 分层问题：不是"依赖倒置"，而是"第二个 main + 死分支" —— **P2【已确认】**

- `router.go:118-123`：

```go
// handleOBCommand handles ob-level commands (install, uninstall, list, etc.)
func (r *Router) handleOBCommand() error {
    // This is handled by cobra in main.go
    // This method is a fallback for direct router usage
    return fmt.Errorf("ob commands should be handled by CLI framework")
}
```

  唯一调用点 `router.go:54-56` 的前置条件是 `detectAppName` 返回 `"ob"`；而 `detectAppName` 仅在**二进制名是 `ob`** 或 `__debug_` 约定下返回 `"ob"`（`router.go:81-91`）。`main.Execute` 只在 `binaryName != "ob"` 时调用 router（`main.go:104-110`），因此该分支在生产中不可达（仅 `__debug_ob` 测试命名可达）。**"fallback for direct router usage" 是一句谎话——没有任何 direct usage。**
- `router.go:74-96` 的 `detectAppName` 直接读 `os.Args`（`:88-89` 使用 `os.Args[2]`/`args[3:]`，而其余部分使用传入的 `args`），函数不纯、不可测。
- `pkg/router` 仅 250 行却 import 4 个兄弟包；`handleOBCommand`、`parseMCPOptions`、`registerProgressiveHandler`、`startMCPServer` 实际是"主编排层"逻辑，放在 router 让 router 成为**第二个组合根**。

**建议**（M）：router 只保留 `Resolve(argv []string) (Mode, appName string, appArgs []string, err error)` 纯函数（配表驱动测试）；server 启动移入 `pkg/mcpapp`；删除 `handleOBCommand` 与 `__debug_` 约定，测试用显式 `Resolve([]string{...})` 注入。

### F-08 `pkg/cli` 职责混合 & `pkg/cli`↔`pkg/tui`↔`pkg/config` 边界 —— **P2【已确认】**

`pkg/cli/handler.go`（892 行）一个类型承担：

| 职责 | 证据 |
| --- | --- |
| 命令解析（resource/verb/flags） | `handler.go:318-357`、`parseCLIFlags:417-452` |
| 参数合并与校验委托 | `mergeRequestParams:453-475`、`args.go` 923 行 |
| HTTP 执行 | `executeAPIRequest:155-167`、`httpClient` 字段 |
| 输出格式化 | `FormatOutput:496-529`、`formatAndPrintOutput:372-380` |
| 帮助渲染（app/resource/verb/global） | `showAppHelp:635-664`、`writeExamplesSection:703-750` |
| 错误美化 | `printAndWrapError:115-121` + `errors.go` 838 行 |
| 代码生成 | `createCodeGenerator:171-177`、`generateCode:192-210` |
| spec 加载/内存缓存 | `loadAndCacheSpec:77-93` |

`pkg/tui/install` 与 `pkg/cli` 的边界同样模糊：
- TUI 只 import `config`/`spec`，但它**自己加载 spec**（`tui/install/model.go:1294-1300` `parser.LoadSpec`），而 InstallApp 又会再加载一次（`config/install.go:381-385`）——安装流程在网络层重复 2–3 次（见 F-02 实测：非 TUI 的 install 也 2 次）。
- TUI 使用的表单模型 `config.InstallOptions`（`install.go:27-107`）同时被 CLI flags（`main.go:307-315`）与 TUI 状态共享，属于"跨 UI 共享 DTO"，方向尚可，但 `InstallOptions` 同时含 `Reader io.Reader`/`Writer io.Writer`/`Interactive bool`（表现层关注点）与 `AuthType string`/`AuthParams`（领域）——建议拆分 `InstallRequest`（领域）+ `InstallUI`（port）。
- `pkg/config` 里含英文提示文案（`install.go:470-533`），`pkg/cli`/`pkg/tui` 才是表现层——**文案下沉到领域包**，导致无法本地化/无法在库模式下静默。

**建议**（M）：`pkg/cli` 拆为 `cli`（协议解析，纯函数：`Parse(args, opSpec) → Request`）、`cmdhandler`（执行 + 输出）、`helprender`；`errors.go` 的渲染部分移到 `cli/errfmt`（见 F-25）。

### F-09 无 internal 共享模型：CLI 参数语法、spec 缓存、错误渲染至少各有两份实现 —— **P2【已确认】**

| 概念 | 实现 A | 实现 B | 后果 |
| --- | --- | --- | --- |
| CLI flag 解析 | `pkg/cli/handler.go:417-452`（`parseCLIFlags`，手写 `--k v`/`--k=v`） | `pkg/cli/args.go:37-85`（`SplitArgs` + prefix 语法） | 两套规则共存，文档/测试各自演化 |
| MCP 参数解析 | `cmd/ob/main.go:869-899` | `pkg/router/router.go:133-176` | 见 F-06 |
| spec 内存缓存 | `pkg/spec/parser.go:632-706`（`GetCachedSpec/CacheSpec`） | `pkg/cli/handler.go:77-93` 再包一层；`pkg/completion/completion.go:386-405` 第三处 | 三处 cache 语义（key/TTL/失效）各自维护 |
| 错误呈现 | `pkg/cli/errors.go`（CLI） | `pkg/mcp/handler.go:76-84 errorResult`（MCP 文本） | 无法共享分类（F-25） |

**建议**（M）：在 `internal/cliarg` 收敛"argv → 结构化请求"；`internal/speccache` 收敛缓存；`internal/errkind` 定义错误分类（`KindAuth/KindNetwork/KindSpec/KindUsage`），CLI/MCP 各自渲染。

### F-10 平台路径策略 4 处独立实现 —— **P2【已确认】**

| 路径 | 实现 |
| --- | --- |
| 配置目录 | `pkg/config/config.go:361-405` `GetConfigDir`（darwin/win/linux + `OPENBRIDGE_CONFIG_DIR`） |
| Keyring 目录 | `pkg/credential/keyring.go:260-300`（darwin `Library/Application Support/openbridge/keyring`、win `LOCALAPPDATA`、linux `XDG_DATA_HOME`） |
| Shim 目录 | `pkg/config/install.go:662-683`（`~/.local/bin`，Windows `LOCALAPPDATA/OpenBridge/bin`） |
| Spec cache 目录 | `pkg/config/install.go:409` `NewSpecCacheManager(m.AppsDir())` → `apps/<app>/cache/` |

四份 `switch runtime.GOOS` 没有任何共享测试向量；`docs/README` 只写了一个平台表，实际行为以代码为准（已确认 macOS 一致，其余未验证）。**建议**（S）：`internal/platformdirs` 单点定义 + 表驱动测试（每平台一个期望值），并统一环境变量前缀（当前只有 config 支持 `OPENBRIDGE_CONFIG_DIR`，keyring/shim 不支持覆盖 → 测试与 CI 需要污染真实 HOME，这是我实测中被迫 `HOME=/tmp/...` 的原因）。

### F-11 `pkg/tui/install/model.go` 1347 行 / 72 个方法：巨型状态机 —— **P2【已确认】**

- 单文件包含：18 个 Step 枚举（`model.go:20-38`）、Lipgloss 样式全局变量（`:42-63`）、`Model`（`:65-...`）、72 个方法（含 input 校验、TLS 文件路径解析 `:679-732`、progressive 阈值判断 `:552`、spec 加载 `:1287-1305`）。
- 后果：TUI 逻辑无法单测（Bubble Tea 需 `teatest`）、样式与领域判断混在一起、`Config` 校验逻辑（`ValidateTLSCACertFile` 等）本应在 `pkg/config/path_utils.go`（目前正确位置有实现，TUI 只是调用——这部分尚可）。
- **建议**（M）：按 Step 拆分 `update_*.go`（Bubble Tea 官方惯例），把 Step 枚举与跳转表抽成 `wizard/flow.go` 纯函数，配 flow 表单测。

---

## 4. 全局可变状态与并发安全（维度 3）

### F-12 全局状态清单（已修正任务书前提）—— **P2【已确认】**

| 全局状态 | 位置 | 可变性 | 评估 |
| --- | --- | --- | --- |
| `cmd/ob` 9 个依赖指针 | `cmd/ob/main.go:39-49` | 运行时可被测试替换（`main_test.go:463-469`） | 单例事实存在，见 F-03 |
| `codegen.registry` map | `pkg/codegen/codegen.go:40` | 理论可写（`register` 在 init 中调用） | 见 F-15 |
| `semantic.StandardVerbs`/`ActionVerbs` 导出 slice | `pkg/semantic/verb.go:459-506` | 任何 importer 可 append/改元素 | 见 F-16 |
| `semantic.actionPrefixes` | `pkg/semantic/resource.go:138-157` | 包内未导出，只读 | 安全 |
| `tui` 样式全局 | `pkg/tui/install/model.go:42` | 只读 | 安全 |
| `config.appNameRegex/reservedAppNames` | `pkg/config/config.go:773-780` | 只读 | 安全 |
| `mcp.Handler` 的 `spec/appConfig/profileName` | `pkg/mcp/handler.go:20-27` | `SetSpec/SetAppConfig` 任意时刻可改 | 见 F-17 |
| `mcp.errAppConfigNotSet` | `pkg/mcp/shared.go:13` | 只读 | 全仓唯一 sentinel（F-27 反例） |

**修正**：任务书说"config 单例"——`pkg/config` **没有**包级单例，`Manager` 是显式构造的；单例问题是 `cmd/ob` 的包级变量。`pkg/spec.Parser` 的 `sync.Map` 缓存是实例字段，不是全局。

### F-13 `codegen` 全局 registry + `init()` 注册，且扩展点被 `register` 未导出封死 —— **P3【已确认】**

```go
// pkg/codegen/codegen.go:39-66
var registry = make(map[OutputFormat]GeneratorFactory)
func init() { register(FormatCurl, ...); register(FormatNodeJS, ...); register(FormatGo, ...); register(FormatPython, ...) }
// register registers a new code generator factory for the specified format.
// This function is used during initialization and can be called by extensions
// to register custom generators.                                    ← 注释声称可扩展
func register(format OutputFormat, factory GeneratorFactory) { ... }   ← 实际未导出
```

- 注释与代码矛盾：外部扩展者无法调用 `register`，所以 registry 只是"4 项静态表"，用 `switch` 更简单（见 §8 F-30）。
- 4 个生成器 `curl.go/go.go/nodejs.go/python.go` 每个仅 3–4k 字节，抽象收益低（`Generator` 接口单方法 `Generate(*http.Request) (string,error)`，合理但非必需）。
- 若未来导出 `Register`，当前无锁 map 会有并发写风险；现在"init 后只读"是唯一安全前提，且没有文档化。
- **建议**（S）：要么删除 registry 改 `switch format`（更少 API、不变量更强），要么导出 `Register` + 加 `sync.RWMutex` + 文档化并发契约。**优于**保留现在这种"看起来可扩展但不能扩展"的中间态。

### F-14 `mcp.Handler` / `ProgressiveHandler` 是可变的进程级单例 —— **P2【推断】**

```go
// pkg/mcp/handler.go:20-27
type Handler struct { mapper *semantic.Mapper; requestBuilder *request.Builder; httpClient *http.Client; spec *openapi3.T; appConfig *config.AppConfig; profileName string }
func (h *Handler) SetSpec(spec *openapi3.T)                  { h.spec = spec }          // handler.go:42
func (h *Handler) SetAppConfig(appCfg *config.AppConfig, p string) { h.appConfig = appCfg; h.profileName = p } // :47
```

- 双组合根（F-06）都直接改它；`cmd/ob` 中它是全局单例（`main.go:46,82`）。
- 读取方是 MCP SDK 注册的回调（`Register` 时把方法值交给 server，`handler.go:58-66`），SDK 在调用期间并发执行 tool handler。当前 `SetSpec` 一定发生在 `RunServer` 之前，所以**当前无真实竞争**（race 测试也通过）；但 API 允许"先 Register 后 SetSpec"，且语义上 `SetSpec` 会重建 `spec` 指针，若未来支持热重载（`SpecWatcher` 的设计意图正是此，F-42）立刻变成 race。
- **建议**（M）：Handler 构造后不可变（`NewHandler(spec, appConfig, profile, ...)`）；需要重载时用 `atomic.Pointer[HandlerState]` 或整体替换 handler 实例。

### F-15 `semantic` 导出可变 slice 作为包级"常量" —— **P3【已确认】**

```go
// pkg/semantic/verb.go:459-472
var StandardVerbs = []string{"list","get","create","update","apply","delete"}
var ActionVerbs = []string{...21 项...}
func IsStandardVerb(verb string) bool { return slices.Contains(StandardVerbs, verb) }   // :507
```

任何 importer 可以 `semantic.StandardVerbs[0] = "list-all"` 或 `append`，从而静默改变 CLI 语义与 MCP tool 命名（`tool_naming.go` 使用 `semantic.Mapper`）。应改为：

```go
var StandardVerbs = [...]string{...}   // 或返回副本的 funcs
func IsStandardVerb(v string) bool { return slices.Contains(StandardVerbs[:], v) }
```

**建议**（S）。

### F-16 `spec.Parser` 内存缓存无锁写 `AccessedAt`；跨进程配置读改写无文件锁 —— **P3【推断】**

```go
// pkg/spec/parser.go:632-648
func (p *Parser) GetCachedSpec(appName string) (*openapi3.T, bool) {
    value, ok := p.cache.Load(appName)     // sync.Map 保证 map 安全
    ...
    cached.AccessedAt = time.Now()         // ← 对共享 *CachedSpec 的写，无锁
    return cached.Spec, true
}
```

- `sync.Map` 只保护"值指针"，`CachedSpec.AccessedAt` 是共享可变字段；并发 `GetCachedSpec`（例如未来 TUI/服务模式）会产生 data race。当前每进程只跑一条命令，不可触发。
- `SaveAppConfig`（`config.go:466-494`）是"读文件→改→写"的跨进程 RMW，无 `flock`；两个 `ob install` 并发会丢更新（推断，未实测）。`writeConfigAtomically`（`:453-463`）只保证单次写原子，不保证事务。
- **建议**（S/M）：`AccessedAt` 用 `atomic.Int64`；配置写入引入 `golang.org/x/sys/unix.Flock`（或 `github.com/gofrs/flock`——AGENTS.md 要求不轻引入依赖，可用 `os.OpenFile(..., O_CREATE|O_EXCL)` 临时锁文件方案）并在 `SaveAppConfig` 文档化"不保护并发读改写"。

### F-17 并发验证结果 —— **已确认（阴性）**

```bash
$ go test -race -short ./pkg/mcp ./pkg/config ./pkg/semantic ./pkg/spec
ok  github.com/nomagicln/open-bridge/pkg/mcp       2.222s
ok  github.com/nomagicln/open-bridge/pkg/config    3.070s
ok  github.com/nomagicln/open-bridge/pkg/semantic  3.193s
ok  github.com/nomagicln/open-bridge/pkg/spec      3.270s
```

**结论**：当前测试路径下无 race 报告；因此 §4 中并发结论均为"设计定时炸弹"级（推断），而不是已发生缺陷。这也是为什么本文把 F-14/F-16 定为 P2/P3 而非 P0。

---

## 5. 接口设计与 API 表面（维度 4）

### F-18 接口悖论：只定义 4 个接口，其中 3 个是"伪接口/无人消费" —— **P2【已确认】**

| 接口 | 位置 | 实现数 | 消费方 | 判定 |
| --- | --- | --- | --- | --- |
| `codegen.Generator` | `codegen.go:30-34` | 4 | `cli/handler.go:196` | 唯一有真实多实现的接口（合理） |
| `mcp.ToolSearchEngine` | `mcp/engine.go:40-71` | 1（`PredicateSearchEngine`） | `mcp/progressive.go` | **伪接口**：7 个方法的"未来 SQL/Vector"从未存在；`NewSearchEngine` 的 switch 只有一个分支（`engine.go:73-82`），`ParseSearchEngineType` 只接受 `"predicate"`（`:94-101`），`NewSearchEngineWithConfig` 标注 Deprecated 仍导出（`:85-91`） |
| `config.SpecWatcher` | `spec_watcher.go:42-52` | 2 | **无生产消费方** | 死代码接口 |
| `spec.UnmarshalStrategy` | `spec/format.go:31` | ？ | 包内 | 局部合理 |

`ToolSearchEngine` 的 7 个方法（Search/GetDescription/GetQueryExample/GetBestPractices/GetExamples/Index/Close）本质是"一个实现的行为集合"，接口化只增加了动态分发和一个"unknown engine type"错误分支。**建议**（S）：只保留 predicate 引擎，删除接口与 `SearchEngineType` 抽象；`SafetyConfig.SearchEngine` 改为 `bool ProgressiveSearchEnabled` 或删除——当前配置任何非 "predicate" 值都直接报错（`config.go:231-263` 的字段存在，但 `ParseSearchEngineType` 只认 predicate）。

### F-19 过度导出与"无版本化策略"：`pkg/` 733 个导出符号 —— **P2【已确认】**

- 量化：`pkg/config` 243、`credential` 114、`spec` 104、`mcp` 82、`semantic` 65、`cli` 54、`request` 28、`codegen` 26。
- 其中相当比例是"实现细节被迫公开"：`SpecCacheMeta`、`FetchResult`、`CacheInfo`、`ProfileExportV2`、`ExportedAuth`、`ExportMetadata`、`ImportValidationResult`、`BulkExport`、`PathValidationError`、`TLSConfigValidation`、`watcherBase` 的派生类型……
- `pkg/` 语义在 Go 里是"可被外部导入的稳定 API"，而本项目是**单一 CLI 二进制**（`cmd/ob` 是唯一 main，模块无其他进口商）。也就是说 733 个符号中绝大多数**本可以是 internal**。
- 无 API 版本策略：无 `CHANGELOG`、无 `internal/` 迁移计划、`Version` 字段不参与兼容判断（F-39）。
- **建议**（M）：以 `internal/` 为默认、`pkg/` 只保留 4 个"稳定契约"（`config.AppConfig`/`Profile`、`spec.Parser`、`mcp` server 入口、`semantic` 查询），并在 `docs/RELEASE.md` 或根 `VERSIONING.md` 写明 breaking change 规则。若短期不迁移，至少在包注释标注 `// Stability: experimental`。

### F-20 stringly-typed 领域概念：authType / outputFormat / engineType / search 语法 —— **P2【已确认】**

| 概念 | 字符串在何处被 switch/比较 |
| --- | --- |
| authType (`"bearer"/"api_key"/"basic"`) | `cmd/ob/main.go:241-257`（createCredentialFromParams）、`pkg/request/builder.go:122-131`（injectAuthCredentials）、`pkg/spec/parser.go:374-...`（applyAuthentication）、`pkg/credential/security.go`（环境变量来源）、`pkg/config/config.go:106-116`（SpecFetchAuth）、TUI `model.go` |
| output format (`"json"/"yaml"/"table"`；codegen `"curl"/"go"/"python"/"nodejs"`） | `pkg/cli/handler.go:496-529`、`pkg/cli/errors.go`、`docs/reference/cli.md`、`codegen.go:16-21` |
| search engine (`"predicate"`) | `pkg/mcp/engine.go:94-101`、`pkg/router/router.go:187-192`、`cmd/ob/main.go:953-959` |
| shim/spec source 类型（URL vs 文件 vs 本地目录） | `pkg/config/install.go:457-464 isWebURL`、`spec/parser.go:198 isURL`、`config.go:429 normalizeSpecSource` |

**建议**（M）：`credential.AuthType`/`codegen.OutputFormat` 已是 typed，但**校验点分散**；抽 `credential.ParseAuthType(string) (AuthType, error)` 与 `codegen.ParseFormat`，在所有入口（CLI flag、TUI、config 加载、MCP 参数）统一调用一次，之后内部只用 typed 值。这同时修复 F-01 的"同一 auth 语义 6 处解析"根因之一。

### F-21 nil/boolean 参数重载语义 —— **P3【已确认】**

- `NewHandler(mapper, reqBuilder, nil)`：第三个参数是 httpClient，nil = 默认（`main.go:82`）。
- `NewProgressiveHandler(reqBuilder, nil, engineType)`：nil = 默认 client（`progressive.go:79-81`）。
- `mcp.NewSearchEngineWithConfig(engineType, _ any)`（`engine.go:85-91`）：`any` 参数完全忽略。
- `config.InstallOptions` 有 6+ 个 bool（`CreateShim/Force/Interactive/ProtectSensitiveInfo/...`，`install.go:27-107`），`buildSafetyConfig(opts)`（`install.go:238-263`）由 bool 推导 SafetyConfig；布尔组合语义需要读实现才能理解。
- **建议**（S）：options struct + 明确默认值；删除 `any` 参数和 Deprecated 的 `NewSearchEngineWithConfig`。

---

## 6. 错误模型（维度 5）

### F-22 `pkg/cli/errors.go` 838 行：名为错误体系，实为"错误文案渲染器 + 字符串嗅探分类器" —— **P1【已确认】**

| 结构 | 行数/占比 | 内容 |
| --- | --- | --- |
| 错误包装（`PrintedError`） | `:16-36`（21 行） | 唯一"错误模型" |
| 分类分发 | `:47-89`（43 行） | **先 `errors.As` 3 种标准库/领域错误，其余全部 `strings.Contains(errMsg, "unknown resource"/"unknown verb"/"required parameter"...)** |
| 文案渲染 | `:91-838`（约 750 行） | `formatAppNotFoundError`、`formatNetworkError`、`formatUnknown*`、`FormatHTTPError`、`FormatUsageHelp`（含参数表、示例生成）、`formatExampleWithBody`、`SuggestSimilarApps`+Levenshtein（`:752-838`） |

```go
// pkg/cli/errors.go:69-87 —— 分类靠字符串匹配
default:
    errMsg := err.Error()
    if strings.Contains(errMsg, "unknown resource") { return f.formatUnknownResourceError(errMsg) }
    if strings.Contains(errMsg, "unknown verb")    { return f.formatUnknownVerbError(errMsg) }
    if strings.Contains(errMsg, "required parameter") && strings.Contains(errMsg, "missing") { ... }
    if strings.Contains(errMsg, "parameter validation failed") { ... }
    if strings.Contains(errMsg, "failed to load spec") { ... }
    return fmt.Sprintf("Error: %s", errMsg)
```

**问题**：
1. **脆弱耦合**：`pkg/cli` 的显示逻辑依赖 `pkg/semantic`/`pkg/request` 错误文案的**子串**。任何文案微调（"unknown resource" → "unknown resource type"）都会让帮助信息静默失效，且编译器无法发现。我 grep 了这些字符串的生产者：`semantic`/`cli` 内部 `fmt.Errorf` 生成，无类型契约。
2. **分层污染**：错误渲染 import `net/http`、`openapi3`、`pkg/config`，并生成 CLI 示例——这是 **presentation**，却放在 `pkg/cli` 顶层，MCP 无法复用（MCP 自己写 `errorResult`，`mcp/handler.go:76-84`）。
3. **"错误分类"缺失**：没有 `Kind`（usage/auth/network/spec/internal），`errors.Is` 无法判断，重试/退出码/遥测都做不了。全仓非测试 sentinel 只有 1 个（`mcp/shared.go:13`），与 AGENTS.md §4.1 "Define strict sentinel errors for state checks" 明确冲突。
4. 838 行中约 90% 是格式化，真正"错误系统"约 60 行 → **名实不符**，让 review 与测试成本被高估。

**重构建议**（M）：
- `internal/errkind`：定义 `Kind` + `Wrap(kind, err)` + `KindOf(err)` + 关键 sentinel（`ErrAppNotFound`、`ErrCredentialMissing`、`ErrSpecLoad`、`ErrUnsupportedFormat`）；生产端在**产生错误处**打标签，而不是显示端嗅探。
- `pkg/cli/errfmt`（或 `internal/errfmt`）：只做 `Format(err, FormatOptions) string`，把 `SuggestSimilarApps/Levenshtein`、`FormatUsageHelp`、HTTP 状态解释分别拆文件；`FormatUsageHelp` 的示例生成（约 400 行）可移到 `pkg/cli/helprender`。
- MCP 复用同一 `FormatOptions` 的纯文本版本。

### F-23 `PrintedError.Error()` 返回空字符串，破坏 Go 错误语义 —— **P2【已确认】**

```go
// pkg/cli/errors.go:16-30
type PrintedError struct{ Err error }
func (e *PrintedError) Error() string {
    // Return empty string since the error has already been printed.
    return ""        // ← 任何 %w/%v/日志/测试断言看到空串
}
func (e *PrintedError) Unwrap() error { return e.Err }
```

- `main.go:93-97` 依赖 `cli.IsPrintedError` 抑制二次打印——这是"控制流用错误类型"的合理技巧，但 `Error() == ""` 会让：
  - `fmt.Errorf("...: %w", printedErr)` 产生 `"...: "` 的空尾；
  - 日志/`slog`/测试失败信息丢失上下文（`errors.Unwrap` 仍可取到，但打印不出来）；
  - `errors.Is(err, someErr)` 可用，但 `err.Error()` 不可读。
- **建议**（S）：`Error()` 返回原始错误文本，`IsPrintedError`/`main` 的抑制逻辑保持；若担心重复打印，用 `fmt.Fprintf(os.Stderr, ...)` 的单一出口 + 一个 `printed bool` 标志在 context 中，而不是污染错误对象。

### F-24 panic 在库代码中 —— **P2【已确认】**

| 位置 | 内容 | 评估 |
| --- | --- | --- |
| `pkg/mcp/progressive.go:116-125` | `SetAppConfig` 对 nil / 空 Name `panic` | 公开 API 的编程错误校验，**从签名（无 error 返回）看是唯一选择**，但违反"库代码不 panic"惯例；调用方 `router.go:204`/`main.go:937` 无法 recover；且两处调用顺序不同（F-06） |
| `pkg/codegen/codegen.go:61-66` | `register(nil)` panic | init 内部调用，等价程序员断言，可接受；若导出 `Register` 则必须改 error |

**建议**（S）：`SetAppConfig(appCfg, profile) error`，`NewProgressiveHandler` 直接接收 appConfig/profile，**消除两段式初始化**（构造后再 Set 的顺序依赖是 F-06 漂移的根因）。

### F-25 46 处显式丢弃错误 + 若干上下文丢失点 —— **P2【已确认】**

Top 丢失点：

| 位置 | 问题 | 后果 |
| --- | --- | --- |
| `pkg/request/builder.go:112-115` | 所有 `GetCredential` 错误 `return nil` | **F-01 的放大器**：keyring 故障/键不存在/未初始化 全部静默 |
| `pkg/router/router.go:59-62` | `appConfig, err := GetAppConfig(appName); if err != nil { return fmt.Errorf("app not found: %s ...") }` | 用泛化错误**替换**原始 err（丢弃 `%w`）；权限错误/损坏 YAML 都被报成"app not found"，误导用户 |
| `cmd/ob/main.go:66-70` | credential manager 失败仅 warning | 后续所有 auth 静默失效（F-01 同路径） |
| `cmd/ob/main.go:230-236` | `StoreCredential` 失败仅 `fmt.Printf("Warning: ...")` 且成功时也打印 | 安装"成功"但凭证可能没存；退出码 0 |
| `pkg/config/install.go:407-416` | 远程 spec 缓存失败仅 warning | 可接受（降级），但应区分"缓存失败"与"spec 不可用" |
| `pkg/config/install.go:550`（`UninstallApp` 中） | `_ = m.RemoveShim(...)`? 见 `install.go:535-561` 的 `fmt.Fprintf` warning | uninstall 部分成功 |
| `pkg/mcp/progressive.go:508-514` | `Close()` 返回 error，但 `router.go:206,211` 用 `_ =` | 资源泄漏不可见 |
| 全局 | 46 处 `_ =`（含 `defer func(){ _ = resp.Body.Close() }()` 这类合理用法） | 需要区分"合理丢弃"与"隐性吞错"，建议 lint 枚举 |

**建议**（M）：`errorlint` 已在 `.golangci.yml` 启用，但 `.golangci.yml` 的 `issues: {}` 无豁免说明；对 `_ =` 增加 `errcheck` 的 `check-blank: true`，并对确实要忽略的加 `//nolint:errcheck // reason`（AGENTS.md 要求）。同时 `BuildRequest/InjectAuth` 的"无凭证"与"后端错误"必须拆开。

---

## 7. 职责膨胀与复杂度（维度 6）

### F-26 `cmd/ob/main.go` 1057 行混合 15+ 职责，且不可测试 —— **P1【已确认】**

功能清单（全部在 main.go）：

| # | 职责 | 行区间 |
| --- | --- | --- |
| 1 | 构建信息 ldflags 变量 | 31-36 |
| 2 | 依赖组合根（init） | 38-89 |
| 3 | 进程入口 / shim 模式判定 | 91-110 |
| 4 | cobra root + 全局 flags + 未知子命令兜底 | 112-159 |
| 5 | 交互式安装编排（调用 TUI） | 161-192 |
| 6 | install opts 与既有 config 合并 | 194-212 |
| 7 | 凭证存储 + API key name 更新 | 214-283 |
| 8 | install 结果打印 | 285-304 |
| 9 | install 子命令 + flags + 执行 | 306-387 |
| 10 | uninstall 子命令 + 执行 + not-found 展示 | 389-440 |
| 11 | list 子命令 + 表格/详情渲染 | 442-554 |
| 12 | info 子命令 + text/json/yaml 三种渲染 + 敏感 header 脱敏 | 556-784 |
| 13 | run 子命令 + 执行模式分派 | 786-860 |
| 14 | MCP 参数解析 + 启动（第二份实现，F-06） | 862-959 |
| 15 | shell completion 子命令 + 自定义补全函数 | 961-1057 |

**测试性证据**：`cmd/ob/main_test.go` 仅 10 个 Test，全部覆盖纯函数（`TestCreateCredentialFromParams`、`TestMergeExistingConfig`、`TestParseMCPServerArgs`、`TestParseArgValue`、`TestNew*Cmd` 常量检查、`TestUpdateAPIKeyName` 用全局替换技巧）。`Execute()`/`runInstallCmd`/`runCommand`/`startMCPServer`/`listApps`/`showAppInfo` 零覆盖。

**拆分建议**（L，但可分两步）：
1. **Step 1（M）**：`cmd/ob/main.go` 只留 `main()` + `Execute()`（~50 行）。新建 `internal/commands/`（`install.go/uninstall.go/list.go/info.go/run.go/completion.go`，每个 ~100-150 行），签名统一 `func New(deps *AppDeps) *cobra.Command`。
2. **Step 2（M）**：`internal/render/`（table/json/yaml/text 渲染，当前 main.go:442-784 约 340 行）；`internal/app`（AppDeps 构造 + `Run(ctx, argv) error`）。
   - 收益：命令可单测（注入 fake config/creds）、`Execute` 可 e2e（`Run(ctx, []string{"list"})`）；同时消除 F-03 的全局单例。

### F-27 `config/install.go` 941 行：把 CLI 用例、OS 集成、交互提示塞进领域包 —— **P2【已确认】**

- 已列于 F-05 表格。补充量化：941 行中约 260 行是交互 prompt（`:774-849`）+ shim/PATH（`:563-755`）+ 提示文案（`:500-533`）；真正"安装"编排约 120 行（`:345-427`）。
- `InstallApp` 是 `Manager` 方法但只被 `cmd/ob` 调用（`main.go:341`）——说明它是用例而非领域操作。
- 附带逻辑缺陷：shim 二进制解析**优先 `exec.LookPath("ob")`**（`install.go:756-770`），实测 `demo -> /usr/local/bin/ob`（另一个 md5 不同的旧构建），而不是当前运行的 `/tmp/ob`——**shim 指向 PATH 中的旧版本**，可能产生"升级后 shim 仍跑旧逻辑"的经典事故。这是 P2【已确认】的具体缺陷。

**建议**（M）：迁移到 `internal/install`；`getOBBinaryPath` 改为 `os.Executable()` 优先（PATH 查找仅作后备，并在 shim 目标与当前二进制不一致时告警）。

### F-28 `pkg/cli/args.go` 923 行 + `handler.go` 892 行 + `errors.go` 838 行 —— **P2【已确认】**

三个文件合计 2653 行，承担"argv 语法 + 参数分类 + 类型转换 + 帮助 + 错误文案"。`args.go` 中 30+ 函数（`SplitArgs`、`parseRequestParamsWithPrefix`、`parseRequestParamsAuto`、`processBodyMultiValues`、`BuildNestedBody`、`setNestedValue`、`parseFileReference`……），`handler.go` 中 30+ 方法。`gocyclo`/`gocognit` 在 `.golangci.yml` 中已被豁免 3 个函数（`ExecuteCommand`、`CompleteFlagValues`、`InstallApp`、`newInstallCmd`），说明复杂度已到"需要豁免"的程度。

**建议**（M，可先做）：加"纯函数化"边界——`args.go` 的解析入口统一返回 `(RequestParams, error)` 且不依赖 `openapi3.Operation` 之外的上下文；`handler.go` 按"解析/执行/输出/帮助"四文件拆。

### F-29 `pkg/tui/install/model.go` 已见 F-11 —— 不重复计分。

---

## 8. 扩展机制设计（维度 7）

### F-30 codegen registry vs switch：当前是"伪扩展点" —— **P3【已确认】**（详见 F-13）

**结论**：4 个内置生成器固定注册、`register` 未导出、无并发保护、注释承诺扩展。要么退化为 switch（推荐），要么补齐可扩展契约（`Register` + 锁 + `Generator` 文档 + 失败返回 error 而非 panic）。

### F-31 MCP progressive disclosure 状态机设计 —— **P2【已确认】**

设计意图（`progressive.go:58-71`）：暴露 3 个 meta-tool，`SearchTools` 检索 → `LoadTool` 加载 schema → `InvokeTool` 执行；`registry.loadedTools` 记录"已加载"状态（`registry.go:24,315-340`），`Invoke` 前强制"必须已加载"（`progressive.go:431-437`）。

**优点**：解决大 API 上下文膨胀的需求真实、meta-tool 分离清晰、`ToolMetadata` 摘要够用。

**问题**：

| 问题 | 证据 | 影响 |
| --- | --- | --- |
| 会话状态存在**进程级 registry** 里，没有 session 概念 | `registry.go:24` `loadedTools` 单 map；`ProgressiveHandler` 每 server 一个实例（`router.go:195`） | 单 client 可接受；**多 client/多会话共享一个 server 时状态串台**（MCP streamable-http 场景） |
| meta-tool 名称模板化但常量固定 | `progressive.go:20-24` 定义 `MetaToolSearchTools="SearchTools"`，但 `:45/:48` 模板生成 `Load{{.AppName}}Tool`，`Register` 里用 `h.formatAppName()`（`:143-199`） | 常量与真实 tool 名不一致；客户端如果按常量硬编码会找不到工具；测试与实现两套命名 |
| 步骤顺序强耦合（Load 后才能 Invoke） | `progressive.go:431-437` | 对 LLM 是额外一步失败点；`SearchTools` 返回的 schema 已含参数摘要，强制 Load 有教学价值但也放大 token/调用错误 |
| 每次 Search 重新创建 predicate parser | `engine_predicate.go:43-56`（`createMatcher` 每次 `predicate.NewParser`） | 每次搜索解析 DSL，O(queries) 语法树构建；工具量大时是热路径 |
| 状态机不可观测 | 无 metrics/log | 无法知道 LLM 是否跳过 Load |

**建议**（M）：引入 `Session` 结构（按 MCP session id 隔离 `loadedTools`）；meta-tool 名用统一函数 `MetalToolName(appName)` 并同时用于常量/模板；predicate parser 在 engine 构造时创建一次（`:24-31`），查询只 `Parse`。

### F-32 predicate DSL（`vulcand/predicate`）引入第三方 DSL 的收益/风险 —— **P2【已确认 + 权衡建议】**

```go
// pkg/mcp/engine_predicate.go:43-56
parser, err := predicate.NewParser(predicate.Def{Functions: e.createPredicateFunctions(), Operators: e.createPredicateOperators()})
pred, err := parser.Parse(query)
...
fn, ok := pred.(toolPredicate)     // 类型断言，失败返回 error "predicate must evaluate to boolean, got %T"
```

| 维度 | 评估 |
| --- | --- |
| 收益 | 表达力强（AND/OR/NOT + 11 个字段匹配函数），README 教 LLM 写 `MethodIs("GET") && PathContains("/users")`；避免自造 DSL |
| 风险 1 | **仅 1 个引擎**：抽象（`ToolSearchEngine` 7 方法）与 `SearchEngineType` 都为"未来 SQL/Vector"预留，但配置只接受 "predicate"（`engine.go:94-101`）——第三方依赖换来的只是"一个可配置语法" |
| 风险 2 | 语法错误直接作为工具错误返回给 LLM（`createMatcher` 返回 error → `handleSearchTools` 应转为 `CallToolResult`），LLM 需要"学会"该 DSL；不如简单的 `{query: string, method: "GET"}` 结构化参数对 LLM 友好 |
| 风险 3 | DSL 语义靠 11 个函数实现且大小写/子串匹配（`strings.ToLower`）；无字段白名单校验的代价是每个函数都要 nil/空处理 |
| 风险 4 | 依赖树：`vulcand/predicate` → `gravitational/trace`（go.mod 间接依赖），为一个内部搜索功能引入 trace 包，收益比低 |
| 结论 | 若产品核心是"LLM 易用"，**结构化搜索参数**（`method`,`path_contains`,`tags`,`limit`）比 DSL 更稳、更省 token、无解析错误；DSL 可以作为高级覆盖（`expr` 字段）保留，但不应是唯一入口。若保留 DSL，建议 parser 复用 + 结构化快捷方式双入口 |

**建议**（M）：`SearchTools` 入参改为结构化（JSON schema 由 SDK 校验）+ 可选 `expr`；删除 `ToolSearchEngine`/`SearchEngineType` 抽象；`predicate.NewParser` 提升到构造期。

### F-33 tool 命名策略：启发式、双实现、与 CLI 命名不共享 —— **P3【已确认】**

```go
// pkg/mcp/tool_naming.go:16-38
func GenerateToolName(method, path string, op *openapi3.Operation) string {
    if op.OperationID != "" && !hasRedundancy(op.OperationID) { return op.OperationID }
    if op.OperationID != "" && isFastAPIPattern(op.OperationID) { ... extractMeaningfulName ... }
    mapper := semantic.NewMapper()                       // ← 每次调用 new mapper
    resource := mapper.ExtractResource(path, op)
    verb := mapper.MapVerb(method, path, op)
    return fmt.Sprintf("%s_%s", resource, verb)
}
// pkg/mcp/tool_naming.go:40-56
func hasRedundancy(operationID string) bool {
    ...
    if hasDuplicate { return true }
    return isFastAPIPattern(operationID)   // ← 与上一个检查重复，FastAPI 模式一定走第二个分支
}
```

- 逻辑冗余：`hasRedundancy` 已含 `isFastAPIPattern`，于是 `GenerateToolName` 第一个 `if` 对 FastAPI ID 永远 false，第二个 `if` 才生效——可读性问题（非 bug，因为两个分支都会产出改进名）。
- 每次调用 `semantic.NewMapper()` 分配默认映射表；`BuildMCPTools` 对每个 operation 调用一次（`registry.go` 中 `convertOperationToTool`），大 spec（数千 operation）下是重复分配。建议 `Mapper` 在 handler 构造时注入。
- MCP tool 名（`todos_list`）与 CLI 命令（`todos list`）由**不同代码路径**生成（前者 `GenerateToolName`，后者 `semantic.BuildCommandTree`），但文档声称"matches the CLI command structure"（`tool_naming.go:12-14`）。共享 `semantic.Mapper` 但不共享生成函数 → 长期漂移风险。
- **建议**（S）：`ToolNamer` 结构体持有 mapper；把"name gen"下沉到 `semantic`（`semantic.CommandName(resource, verb)`），CLI 与 MCP 共用。

### F-34 `x-cli-verb` / `x-cli-resource` 扩展点：无 schema 校验、无文档、覆盖不完整 —— **P2【已确认】**

- 实现：`semantic/verb.go:128-142 checkExtensionVerb`、`semantic/resource.go:63-91 extractFromExtension`。
- 文档仅 `docs/reference/cli.md:44-54` 给出示例，**没有**：字段优先级说明（extension > pattern > operationId > method default？）、冲突处理、无效值的错误提示、与 MCP tool 名的一致性。
- `README.md` 完全没提扩展点；`QUICKSTART` 也没提。用户无法从主文档得知如何对抗启发式误判（例如 `POST /orders/{id}/cancel` 被映射成 `cancel` 还是 `orders_cancel`）。
- **建议**（S）：在 `docs/reference/openapi-extensions.md` 定义 `x-cli-verb`、`x-cli-resource`、`x-mcp-tool-name`、`x-mcp-description`（当前 MCP tool description 直接取 `operation.Summary`/`Description`？见 `registry.go:convertOperationToTool`），并加扩展字段的校验 + 单测。

---

## 9. 数据模型与兼容性（维度 8）

### F-35 `AppConfig.Version` 只写不读：没有迁移机制 —— **P2【已确认】**

```go
// pkg/config/config.go:38-39
// Version is the application configuration version (for migrations).
Version string `yaml:"version,omitempty"`
// pkg/config/config.go:478-480 (SaveAppConfig)
if config.Version == "" { config.Version = "1.0" }
```

- 全仓 `config.Version` 的读路径：**不存在**（仅 `export.go:441-468` 对 *export 格式*做 v1/v2 嗅探，与 AppConfig.Version 无关）。安装写 `"1.0"`（`install.go:275`、`config.go:832`），export 写 `"2.0"`（`export.go:69`）——两套版本号语义混用。
- 后果：字段重命名/语义变化时无法自动迁移；`importProfileLegacy`（`config.go:680-723`）是唯一的"迁移"，但只服务 import。
- **建议**（M）：定义 `const AppConfigSchemaVersion = 2`；`GetAppConfig` 读取时 `if v != current { migrate(data, v) ; save }`；给 `Duration`（已有自定义 YAML 编解码）、`TLSConfig` 等写迁移单测。同时在 cfg 顶层加 `schema_version` 独立字段（`Version` 现在语义是"app 版本"还是"配置版本"含糊）。

### F-36 `Profile` 是 12 个关注点的拼盘，且含明文凭证载体 —— **P2【已确认 + 安全】**

`Profile`（`config.go:56-103`）同时含：`BaseURL`、`Auth`、`Headers`（可含 `Authorization`！）、`QueryParams`、`TLSConfig`、`SafetyConfig`、`Timeout`、`RetryConfig`、`Description`、`IsDefault`、`SpecFetchAuth`、`SpecFetchHeaders`、`ProtectSensitiveInfo`。

- **安全**：`Profile.Headers` 用于 MCP 每次请求注入（`mcp/handler.go:142`），但存 YAML 明文（`writeConfigAtomically` 0644，`config.go:453-461`）；`main.go:726-729` 打印时做脱敏，说明团队知道它可能含密钥，但**存储不脱敏**（keyring 只存 `Credential`，不存 headers）。建议：`Headers` 中敏感值支持 `${env:VAR}` / keyring 引用，或至少文件权限 0600。
- 关注点混杂导致 API 矩阵（F-39）出现大量"配置了但没接线"的字段：`SpecFetchAuth`/`BuildSpecFetchOptions`（`config.go:122-143`）生产零调用；`QueryParams` 运行期零使用；`TLSConfig`/`Timeout`/`RetryConfig` 未接线。
- **建议**（L）：拆 `RequestPolicy`（Headers/QueryParams/Timeout/Retry/TLS）+ `Auth` + `Safety`；每个字段在 `docs/reference/config.md` 标注"是否已接线"（当前 README 把未接线字段当功能宣传）。

### F-37 语义映射硬编码：4 份重复清单 + 英语形态学启发式 —— **P2【已确认】**

| 清单 | 位置 | 条目数 |
| --- | --- | --- |
| `createActionRules`（path regex → verb） | `semantic/verb.go:39-80` | 36 |
| `ActionVerbs` | `semantic/verb.go:474-505` | 21（与上表高度重叠） |
| `actionPrefixes`（operationId 前缀） | `semantic/resource.go:138-157` | 51 |
| `VerbDescription` map | `semantic/verb.go:519-547` | 20（缺失 archive/publish/sync 等，回退 "Perform x operation"） |
| `defaultMethodMapping`（method+path 启发式） | `semantic/verb.go:222-255` | switch |
| 单复数规则（`Singularize/Pluralize`） | `semantic/resource.go:295-408` | 英语规则硬编码（`-ies/-es/-s`、不规则表？） |

- 后果：新增一个动作动词（如 "finalize"）需要改 3–4 处；`VerbDescription` 与 `ActionVerbs` 不一致已发生（ActionVerbs 有 archive，VerbDescription 没有）。
- 无用户可配置映射：`VerbMapper.CustomMappings`/`PathPatternRules` 是导出字段（`verb.go:15-22`），但**没有配置入口**（config 无 `verb_mappings` 字段，`InstallOptions` 也无），只有 `x-cli-verb` 能覆盖单条 path。
- **建议**（M）：单一 `semantic.Catalog`（`type Verb struct{ Name, Description string; DefaultFor []MethodPattern }`），registry 化；`ActionVerbs`/`VerbDescription`/rules 从同一 catalog 生成；在 `AppConfig` 增加 `verb_overrides` 支持用户修正启发式误判。

### F-38 spec cache 失效模型：设计完整但没有读路径（write-only cache）—— **P1【已确认】**

**设计**（`pkg/config/spec_cache.go`）：`SpecCacheMeta{URL, ETag, LastModified, CachedAt, ExpiresAt}`（`:29-80`）、`parseExpiresAt` 支持 `Cache-Control: max-age` / `Expires` / 默认 24h（`:206-231`）、`FetchWithCache` 写 `apps/<app>/cache/{spec.json,meta.json,parsed.json}`（`:551-593`、`:696-716`）、`GetCachedSpecInfo` 等读取 API 齐全。

**现实**：
- 唯一写入口是 `install.go:407-416`（远程 spec 安装时预缓存一次）；**零读取**——`SpecCacheManager` 的 `LoadCachedSpec/FetchWithCache` 的读分支只被它自己调用。
- `loadFromURL`（`spec/parser.go:264-294`）每次直接 `p.client.Do`，从不查 cache。
- `LoadSpecWithPersistentCache`（`parser.go:835-839`）注释声称 "enables cross-process caching ... avoid re-parsing on each restart"，函数体 `return p.LoadSpecWithContext(ctx, source)`——**参数 appName 完全丢弃**。
- **实测**（附录 C）：install = 2 次 GET（LoadSpec 1 次 + FetchWithCache 1 次，同一 URL 下两次），之后每条命令 = 1 次 spec + 1 次 API；`--help` 也 1 次 spec。缓存文件明明存在（`apps/demo/cache/spec.json`）却无人使用。

**影响（P1）**：
1. **离线不可用**：README 未承诺离线，但 install 后无法离线使用；MCP server 每次启动重新拉 spec。
2. **延迟/失败面**：每个 CLI 动作都暴露在网络故障下；`ob demo --help` 也会失败（帮助本可离线）。
3. **速率/隐私**：对远端 spec 服务器产生不必要流量（安装时还双倍）。
4. 代码 816 行 + 665 行测试维护的是"写缓存"，收益为零。

**修复建议**（M）：在 `pkg/spec.Parser` 增加 `LoadSpecWithPersistentCache` 的真实实现：`cacheKey=source`，读 `apps/<app>/cache/meta.json` 判断 `ExpiresAt`/ETag → 命中则读 `spec.json` 反序列化（无需网络）；未命中走条件请求（`If-None-Match`/`If-Modified-Since`）→ 304 复用、200 覆写。或者把 cache 移到 `pkg/spec` 内部（消除 config→spec 反向边隐患）。install 阶段删掉第二次 `FetchWithCache`（LoadSpec 已下载）。

### F-39 配置面 vs 执行面矩阵（Lead 线索 2 的完整版）—— **P1【已确认】**

| 配置项 | 配置结构 | 运行时是否生效 | 证据 |
| --- | --- | --- | --- |
| `Profile.BaseURL` | `config.go:61` | ✅ CLI/MCP | `request/builder.go:47` |
| `Profile.Auth.Type/KeyName/Location` | `config.go:173-194` | ⚠️ MCP 生效；**CLI 因 F-01 必然失效** | `builder.go:107-118` vs `cli/handler.go:133` |
| `Profile.Headers` | `config.go:67` | ⚠️ **仅 MCP**（`mcp/handler.go:142`）；CLI 不注入 | 全仓 grep |
| `Profile.QueryParams` | `config.go:70` | ❌ 运行期无人使用（仅 config/export/profile） | grep 仅 config 包内 |
| `Profile.TLSConfig`（CA/ClientCert/SkipVerify） | `config.go:210-230` | ❌ 全仓 **0 处 `crypto/tls` import**；没有任何 `tls.Config` 构造 | `grep -rn 'crypto/tls'` = 空 |
| `Profile.Timeout` | `config.go:79`、`Duration` | ❌ 从未用于 `http.Client.Timeout`/`context.WithTimeout`；仅打印（`main.go:683`）与 import/export | F-09 |
| `Profile.RetryConfig` | `config.go:300-315` | ❌ 运行期零引用（仅 config/export/profile） | grep |
| `Profile.SpecFetchAuth`/`SpecFetchHeaders`+`BuildSpecFetchOptions` | `config.go:90-143` | ❌ `BuildSpecFetchOptions` 生产零调用 → 私有 spec 无法认证拉取 | grep |
| `Profile.ProtectSensitiveInfo` | `config.go:99-102` | ✅ 仅 `--generate` 代码输出脱敏（`cli/handler.go:198`） | grep |
| `Safety.ReadOnlyMode` | `config.go:232-240` | ✅ MCP（`mcp/handler.go:275`、`registry.go`） | grep |
| `Safety.ProgressiveDisclosure` | 同上 | ✅ MCP（`router.go:232`、`main.go:919`） | grep |
| `Safety.SearchEngine` | 同上 | ⚠️ 只接受 `"predicate"`；其他值直接报错 | `mcp/engine.go:94-101` |
| `Safety.AllowedOperations/DeniedOperations` | 同上 | ✅ MCP（`mcp/handler.go:291-300`、`registry.go:260-270`） | grep |
| `Safety.RequireConfirm` | `config.go:241-242` | ❌ 零引用 | grep 仅 config.go 定义 |
| `Safety.MaxRequestsPerMinute` | `config.go:244-245` | ❌ 零引用 | 同上 |
| `Safety.DangerousOperationPatterns` | `config.go:247-248` | ❌ 零引用 | 同上 |
| `Safety.HybridSearch` | `config.go:259-263` | ❌ 零引用 | 同上 |
| `SpecCache.*`（TTL/ETag） | `spec_cache.go` | ❌ write-only（F-38） | 实测 |
| `AppConfig.OperationCount` | `config.go:50-52` | ⚠️ 仅 TUI 用于推荐 progressive（`tui:552`）；MCP 不读 | grep |

**结论**：对外承诺的"安全/企业"配置面（TLS、超时、重试、确认、限流、危险操作、私有 spec 认证）**在实现层几乎全部是 YAML 占位符**。这不是单点 bug，而是"配置模型先行、执行层未跟进"的系统性漂移（P1）。

**建议**（L）：建立"配置字段 → 执行点"矩阵测试（本文表格即雏形）：每个字段必须在 e2e/集成测试中被断言生效（例如 TLS：起 httptest TLS server + 自签 CA，断言请求成功；Timeout：`httptest` 慢响应，断言超时错误）。未接线的字段在 `docs/reference/config.md` 标 `not implemented` 或从文档移除。

---

## 10. 生命周期与资源管理（维度 9）

### F-40 全链路无 `context` 传播、无信号处理/优雅关闭 —— **P2【已确认】**

- `context.Background()` 硬编码 10 处（§0.3），包括：`cli/handler.go:84`（spec 加载）、`router.go:216,249`（MCP run）、`main.go:950`（MCP run）、`spec/parser.go:191,534,590,831`、`completion.go:401`。
- `cobra.Execute()` 未用 `ExecuteContext`（`main.go:158`）→ Ctrl-C 依赖默认信号 termination；MCP server（stdio/http）没有 `Shutdown` 流程；`defer cleanup()`（`router.go:237`）在进程被杀时不会执行。
- 3 个 `http.Client` 无 Timeout（F-09），`&http.Client{}` 默认 `Timeout: 0` = 无限等待；一旦服务端挂起，CLI 会永久卡住，且 Ctrl-C 只能杀进程。
- **建议**（M）：`main()` 建 `signal.NotifyContext`，`ExecuteContext` 传递；所有 HTTP 调用用 `http.NewRequestWithContext`；client 统一从 `httpclient.New(timeout)` 注入（同时接线 `Profile.Timeout`，修 F-39）。

### F-41 无超时 HTTP 客户端 + 未接线的 `Timeout` 配置 —— **P1【已确认】**

（与 F-07/F-39 交叉引用，单列因为它是"用户可感知的挂起"风险）

| 位置 | 代码 |
| --- | --- |
| `pkg/cli/handler.go:45` | `httpClient: &http.Client{}` |
| `pkg/mcp/handler.go:32` | `httpClient = &http.Client{}` |
| `pkg/mcp/progressive.go:80` | `httpClient = &http.Client{}` |

对照：`pkg/spec/parser.go:176-178` 和 `pkg/config/spec_cache.go:113-117` 都有 30s 超时——**只有 spec 拉取有超时，API 请求反而没有**。而用户配置的 `Profile.Timeout` 是死字段（F-39）。

**建议**（S）：`profileTimeout := profile.Timeout.Duration; if profileTimeout <= 0 { profileTimeout = 30*time.Second }`，构造 `&http.Client{Timeout: profileTimeout}`；TLS 用同 profile 的 `TLSConfig` 构造 `Transport`（顺手修 F-39 的 TLS）。

### F-42 spec watcher 的生命周期设计与死代码问题 —— **P2【已确认（死代码）+ 推断（若启用则泄漏）】**

- `pkg/config/spec_watcher.go` 778 行：`SpecWatcher` 接口、`LocalFileWatcher`（fsnotify + 轮询双模 + 文件 hash 去抖 + `handleFsEvent`）、`RemoteSpecWatcher`（定时检查 + cache manager）、`SpecWatcherManager`（`:638-720`，多 watcher + 全局 handler + ctx/cancel）。
- **生产零实例化**：`grep -rn 'NewSpecWatcherManager\|NewLocalFileWatcher\|NewRemoteSpecWatcher'` 在 cmd/pkg（非 test）无调用。唯一使用者是它自己的测试（1001 行）。
- 若未来启用，存在生命周期问题（推断）：
  - `Start`（`:706-720`）注册 watcher 后返回；`Stop` 只 `cancel` 与 `Close`，没有 `wg.Wait()` 等 goroutine 退出（`watchLoop` 是 `go func`），存在"Stop 返回后 goroutine 仍触碰已关闭 fsnotify"的可能；
  - `NewLocalFileWatcher` 的 `fsWatcher` 在 `tryStartFsnotify` 失败后为 nil，`Stop` 里 `if w.fsWatcher != nil` 判断了，但轮询 goroutine 的退出条件依赖 ctx——基本正确，但没有测试覆盖 Stop→Start 重入。
- **结论**：这是"设计很完整、接线为零"的典型（与 F-38 同构）。**建议**（S）：删除 `spec_watcher.go`（以及 fsnotify 依赖、1001 行测试），等有真实需求（`ob watch` 命令或 MCP 热重载）再按 F-14 的不可变 handler 设计重做。如果保留，必须接一条端到端测试（改本地 spec → CLI/MCP 反映新定义）。

### F-43 缓存写入原子性与权限 —— **P2【已确认】**

| 写入点 | 原子性 | 权限 | 备注 |
| --- | --- | --- | --- |
| `config.writeConfigAtomically` | ✅ tmp+rename（`config.go:453-463`） | **0644** | 无 fsync；同目录 tmp 命名 `path+".tmp"` 无 pid/随机后缀 → 并发写会互踩（推断） |
| `spec_cache` 原始/元数据写入 | ❌ 直接 `os.WriteFile(specPath, ...)`、`os.WriteFile(metaPath, ...)`（`spec_cache.go:551-593`） | 0644 | 写一半崩溃 → 损坏缓存（无 reader 所以暂时无害，修 F-38 后必须改原子） |
| `spec_cache` parsed 写入 | ✅ tmp+rename（`:696-716`） | 0644 | 同 tmp 命名风险 |
| shim 创建 | ✅ symlink 原子替换（`install.go:563-617`，`os.Remove`+`os.Symlink`） | 0755 | 有 remove-then-create 窗口（推断可接受） |

**建议**（S/M）：统一 `internal/atomicfile.Write(path, data, perm)`（tmp 带随机后缀 + `os.Chmod` + `Sync` + rename）；配置文件权限改 **0600**（含 `Profile.Headers` 潜在密钥，F-36）。

### F-44 安装流程的部分成功状态与回滚缺失 —— **P2【已确认】**

`doInstall`（`install.go:372-427`）顺序：`SaveAppConfig`（成功）→ `FetchWithCache`（warning）→ `createShimIfRequested`（warning，`install.go:299-318`：`fmt.Fprintf` 警告不返回 error）→ 返回 `InstallResult`。调用方 `runInstallCmd`（`main.go:341-349`）随后 `storeInstallCredentials`（`main.go:230-236`，失败仅 warning）→ 打印 "✓ Successfully installed"。

- 结果：**config 已写、shim 失败、凭证未存** 的最坏状态下 CLI 仍打印"成功"并 exit 0。用户随后执行 shim 得到 "app not found" 或未认证请求（叠加 F-01）。
- **建议**（M）：返回 `InstallResult{Warnings []error, Err error}`；对关键步骤分级：config 失败 = 失败；shim/凭证失败 = 失败但可配置 `--soft`；输出中明确 `Installed with warnings`，退出码非 0。或真正回滚（删除 config、shim）。

### F-45 TUI 与 CLI 共享模型但重复加载 spec —— **P3【已确认】**

见 F-08：TUI `model.go:1287-1305` 自己 `spec.NewParser().LoadSpec()` 获取标题/描述用于交互预览；`InstallApp` 内 `doInstall` 再 `LoadSpec`；远程 spec 还要 `FetchWithCache` 再下载一次。交互式安装远程 spec = 3 次网络往返（推断；非 TUI 实测为 2 次）。
**建议**（S）：`InstallOptions` 传入已加载的 `*openapi3.T`/`SpecInfo`（由调用方加载一次），`doInstall` 接受可选已加载 spec。

---

## 11. 设计与文档漂移（维度 10）

### F-46 文档命令 `ob version` 不存在 —— **P2【已确认】**

- `docs/reference/cli.md:18` 与 `QUICKSTART.md` 写 `ob version` / `./bin/ob version`。
- 实现：root cobra 只设置了 `Version` 字段（`main.go:122`），cobra 只注册 `--version` flag；没有 `newVersionCmd()`。实测：

```
$ /tmp/ob version
unknown command "version" for "ob"     (exit 1)
$ /tmp/ob --version
ob version dev (commit: unknown, built: unknown)
```

- **修复**（S）：加 `newVersionCmd()`（输出同 `--version`），或文档改为 `ob --version`。建议加文档校验（见 F-52）。

### F-47 文档承诺 `--json` / `--yaml`，实际被静默忽略 —— **P2【已确认】**

- 文档：`README.md:172-175`、`docs/reference/cli.md:38-41`（`myapi users list --json` / `--yaml`）。
- 实现：`parseAndMergeParams` → `extractCLIFlags`（`cli/handler.go:289-315`）**把 `output/json/yaml` 从 params 中删掉**，然后 `formatAndPrintOutput(body, cleanParams)`（`:372-380`）→ `determineOutputFormat(cleanParams)`（`:358-370`）永远拿不到这些键 → 永远返回 `"table"`。

```go
// cli/handler.go:306-313
case "generate", "generate-output", "output", "json", "yaml":
    continue          // ← 丢弃 output/json/yaml
...
// :318-355 ExecuteCommand
generateFormat, generateOutput, cleanParams := extractCLIFlags(params)
...
return h.formatAndPrintOutput(body, cleanParams)   // ← 收不到 output
```

- 实测（附录 C）：默认、`--output json`、`--json` 三种输出**完全一致**。
- 另：root 全局 flag `-o/--output`（`main.go:146`）在 `ob run`（`DisableFlagParsing: true`，`main.go:799`）路径下被当普通参数传给 handler——同样被丢弃；在 shim 路径下根本没有 cobra 解析。
- **修复**（S）：`extractCLIFlags` 返回 `outputFormat`（而不是丢弃）；`ParseRequestParams` 需保证这些不是 API 参数（已有 `extractCLIFlags` 白名单逻辑，改成保留）——并补 e2e 测试（起 mock server 断言 stdout 是 JSON）。

### F-48 `ob <app> ...` 直连路由不可达，且与帮助文本矛盾 —— **P1【已确认】**

- 预期（`main.go:361-366` install 的 Long text 与 README）：

```
After installation, you can call API operations using either:
  - Automatic parameter classification (recommended):
    ob myapp get-user --id=123
```

- 实现（`main.go:126-137`）试图用 root `RunE` 做 fallback：

```go
RunE: func(cmd *cobra.Command, args []string) error {
    if len(args) == 0 { return cmd.Help() }
    appName := args[0]
    if configMgr.AppExists(appName) { return appRouter.Execute(...) }
    return fmt.Errorf("unknown command or app: %s\n...", appName)
},
```

  但 cobra 对"有子命令的 root"使用 `legacyArgs` 校验：当第一个参数不是已知子命令且不是 flag 时，**在 `RunE` 之前**返回 `unknown command %q for %q`。于是 fallback 永不执行。
- 实测（已安装 `demo` app）：

```
$ /tmp/ob demo pets list
unknown command "demo" for "ob"
$ /tmp/ob run demo pets list
（正常输出）
```

- 影响：**README/帮助宣称的 `<app>` 直连（非 shim 场景）不可用**；用户必须 `ob run` 或 shim。而 shim 路径又依赖二进制名（F-49）。
- **修复**（S/M）：给 root 设置 `Args: cobra.ArbitraryArgs`（覆盖 legacyArgs），在 `RunE` 内做 app 分发；或 `rootCmd.SetArgs` 前手动分流 `os.Args[1]`（在 `Execute()` 里先查 `configMgr.AppExists(os.Args[1])` → 直接 `appRouter.Execute`）。二者都要补 e2e。

### F-49 路由依赖二进制文件名 `os.Args[0]`：rename/symlink 即行为反转 —— **P2【已确认】**

- `main.Execute`：`binaryName != "ob"` → 视为 shim 模式（`main.go:103-110`）；`router.detectAppName` 再次 basename 判断（`router.go:74-96`），并保留 `__debug_` 前缀约定（`:85-91`）。
- 实测：把二进制复制为 `/tmp/ob-arch` 后：

```
$ /tmp/ob-arch --version
app not found: ob-arch (use 'ob' install to add it)   # 不打印版本，直接按 app 找配置
```

  即 **改名 `ob` → `openbridge` 就完全失效**，且错误信息误导（提示安装一个叫 ob-arch 的 app）。包管理器（Homebrew 的 `ob@1.2`、`versioned symlink`、CI 的 `ob-linux-amd64`）会直接触发。
- 另有 `getOBBinaryPath` 的 PATH 优先问题（F-27）：shim 可能指向 PATH 中的其他 ob 版本。
- **建议**（M）：显式模式参数：`exec.Command` 无法感知？可改为环境变量 `OPENBRIDGE_APP_NAME`（shim 脚本设置）优先 + 文件名兜底；或 `ob --app <name>` 显式模式；删除 `__debug_` 约定（测试用 `Resolve(argv)` 注入，F-07）。错误信息应提示"本二进制以 shim 模式运行，未找到 app X；若你是直接运行 ob，请命名为 ob 或使用 `ob run`"。

### F-50 "Zero Code Generation" 与 `pkg/codegen` 并存的措辞问题 —— **P3【已确认】**

- `README.md:20` / `docs/index.md:21` / `docs/guide/introduction.md:20`：`**Zero Code Generation**: All behavior is dynamically driven by OpenAPI specifications`。
- 实际：`pkg/codegen`（4 语言生成器）+ `--generate` 功能（`cli/handler.go:171-210`）存在。
- 语义可以自洽（"运行 API 不需要代码生成"，而 `--generate` 是导出代码片段辅助调试），但字面矛盾，且 `--generate`/`--generate-output` **完全没写进 README**（只在 code 注释与 `cli/errors.go` 的 usage 里出现）。建议改措辞为 "No code generation required to run APIs; optional `--generate` exports curl/Go/Python/Node examples"，并补文档。

### F-51 "Cold start <100ms"：实测达标，但无 Benchmark 守护 + 首启例外 —— **P3【已确认（数字）+ 推断（风险）】**

- 实测 warm：`--version` 中位 6.2ms、`list` 5.4ms、`completion bash` 5.4ms（附录 C）。**达标**。
- 但：① 首次执行 770ms（macOS Gatekeeper 校验 + page-in，我这台机器复现）；② 无 `func Benchmark`（全仓 0），CI 无启动耗时检查 → 任何 init 加重（F-02 的 I/O、keyring 探测）都无回归守护；③ keyring 在 Linux 无 D-Bus 时的降级路径未测，可能显著变慢（推断）。
- **建议**（S）：加 `BenchmarkStartup`/`cmd/ob` e2e 计时门禁（如 `go test -run TestStartup -timeout`），或在 CI 用 `hyperfine` 阈值；文档注明"warm start on developer hardware"。

### F-52 AGENTS.md 自身协议被违反（可作为治理改进点）—— **P3【已确认】**

| AGENTS.md 条款 | 违反证据 |
| --- | --- |
| §4.1 "Define strict sentinel errors ... for state checks" | 全仓非测试仅 1 个 sentinel（`mcp/shared.go:13`）；状态判断靠字符串（F-22） |
| §3.1 所有 `*_integration_test.go` 必须 `//go:build integration` | 4 个中仅 1 个带 tag（`credential/keyring_integration_test.go`）；`config/install_integration_test.go`、`cli/errors_integration_test.go`、`request/builder_integration_test.go` 缺 tag → 默认测试运行（其中 install 测试会碰真实 FS/shim，可能依赖 `ob` 在 PATH——与我发现的 `getOBBinaryPath` 行为叠加） |
| §4.2 "Always define dependencies as interfaces" | 见 F-03/F-18 |
| §2 TDD 流程 | 无法从仓库判断；但 `cmd` 层全局变量使测试先行困难（F-26） |
| §5.1 提交前 `make fmt-fix && make lint-fix` | lint 工具链可复现性问题（Lead 已确认 golangci v2 配置与 Go 1.27 export data 不兼容） |
| `.golangci.yml` 关闭 `gosec`（"enable later after baseline cleanup"） | 对"凭证/密钥"相关项目，gosec 关闭是安全债 |

**建议**（S）：把"sentinel error 命名规范""integration tag 检查""arch 依赖规则"写成 CI 脚本（`test -z $(grep -L ...)`），而不是只写在 AGENTS.md 里。项目已有 `.github/workflows/ci.yml`，加 3 个 grep 级检查成本极低。

### F-53 proposals/01_init.md 的历史命名与当前实现不一致 —— **P3【已确认】**

- `proposals/01_init.md:41-46` 使用 `bridge install <app-name>`、`bridge --app myapp`；当前产品命令是 `ob`，且 app 直连模式实际不可达（F-48）。proposals 是历史文档，但放在仓库根目录且未标注"已废弃"，会误导新贡献者。
- **建议**（S）：文件头加 `> Status: Superseded by docs/guide/*`，或移入 `docs/archive/`。

---

## 12. 优先级行动清单

### 12.1 建议排期（按 ROI 排序，含关联发现）

| # | 行动 | 关联 | 工作量 | 预估收益 |
| --- | --- | --- | --- | --- |
| A1 | **修复 CLI 认证**：`cli/handler.go:133` 传 `appName`；`InjectAuth` 区分 `ErrCredentialNotFound` 与后端错误并在 CLI 输出提示；补 keyring file-backend 的 e2e 回归测试 | F-01、F-25 | **S**（0.5d） | 恢复 bearer/api_key/basic 全部功能；消除静默安全降级 |
| A2 | **让持久化 spec cache 真正生效**：实现 `LoadSpecWithPersistentCache` 读路径（先读 meta→命中读 spec.json；未命中条件请求）；install 去重（删 `FetchWithCache` 或复用已下载内容）；`--help` 允许缓存读取 | F-38 | **M**（1.5d） | 离线可用、每命令去掉一次网络 RTT、启动稳定性；并为 config→spec 解耦 |
| A3 | **撤销 `init()` 组合根**：`internal/app.AppDeps` + `run(ctx, argv)`；`main.go` 只剩 `main/Execute`；统一失败策略（config fatal / creds degrade 用 nil-object + 提示） | F-02、F-03 | **M**（2d） | 可测试性（cmd 层从 10 个纯函数测试 → e2e）、消除全局单例 |
| A4 | **合并 MCP 启动路径**：`pkg/mcpapp.NewServer(...)`；`router` 退化为纯 `Resolve`；删除 `handleOBCommand`、`__debug_`；修 server version/缓存/顺序不一致 | F-06、F-07、F-14 | **M**（2d） | 消除双实现漂移与不可达分支；为未来多 app 化铺路 |
| A5 | **拆 `pkg/config`**：至少先拆出 `internal/install`（含 shim/prompt）与删除 `spec_watcher.go`；`spec_cache` 迁 `pkg/spec`（或 `internal/specstore`） | F-05、F-42、F-19 | **L**（1w，可分 PR） | 10.4k 行→~5k；删除 ~1.8k 死代码/测试；打断 config↔spec 风险 |
| A6 | **错误模型收敛**：`internal/errkind` + sentinel；`errors.go` 拆渲染；`PrintedError.Error()` 返回真实文本；MCP 复用分类 | F-22、F-23、F-24、F-25 | **M**（2d） | 消除字符串嗅探；退出码/重试/遥测可行；符合 AGENTS |
| A7 | **HTTP 生命周期**：统一 client 注入（timeout 来自 `Profile.Timeout`）、TLS 接线（`crypto/tls`）、`signal.NotifyContext` + `ExecuteContext` | F-40、F-41、F-39 | **M**（1.5d） | 消除挂起风险；让 TLS/Timeout 配置名副其实 |
| A8 | **文档-实现对齐**：`ob version`、`--json/--yaml`、`ob <app>`、`--generate` 四个断点修或改文档；加文档命令的 e2e 冒烟 | F-46、F-47、F-48、F-50 | **S**（1d） | 用户信任；减少 issue |
| A9 | **路由模式显式化**：`Resolve(argv)` 纯函数 + `OPENBRIDGE_APP_NAME`（shim 设置）替代 argv[0] 猜测；`getOBBinaryPath` 用 `os.Executable` | F-48、F-49、F-27 | **S/M** | 消除改名失效与 shim 指向旧二进制 |
| A10 | **导出面治理**：`pkg/` → `internal/` 迁移计划；删除 export/profile/watcher 等死 API 或落地 CLI 命令；`codegen`/`semantic` 的可变全局收紧 | F-42、F-05、F-19、F-18、F-20 | **M** | 733→~250 导出符号；API 契约可声明 |
| A11 | **配置接线矩阵 + 矩阵测试**：F-39 矩阵中每个字段要么接线（TLS/Timeout/Retry/RequireConfirm/限流/私有 spec），要么文档标未实现；删除 `SearchEngine`/`HybridSearch` 等单值抽象 | F-39、F-07、F-18、F-32 | **L**（1w） | "企业配置"名实相符 |
| A12 | **cmd 层拆分**：`internal/commands` + `internal/render`；`main.go` 1057 → ~50 行 | F-26、F-27、F-28 | **M**（2d） | 命令可单测；主文件可 review |
| B1 | 测试架构：修 3 个缺失 integration tag；加 `BenchmarkStartup`；给 cmd/router 加 e2e（`os/exec` 跑真实二进制，配 temp HOME） | F-52、F-51、F-26 | **S/M** | 防止本文问题回归 |
| B2 | TUI 拆分：step 分文件 + flow 纯函数 + teatest | F-11、F-29 | **M** | TUI 可测 |
| B3 | semantic 单一 catalog + verb description 补齐；tool naming 共享 | F-37、F-33、F-34 | **M** | 语义一致、可配置 |
| B4 | atomicfile 统一 + 配置 0600 + 并发写锁/文档 | F-43、F-36、F-16 | **S** | 数据完整性与凭证安全 |
| B5 | `x-cli-*`/`x-mcp-*` 扩展规范文档 + 校验 | F-34 | **S** | 用户可纠偏启发式 |
| C1 | 删除 `SearchEngineType`/`NewSearchEngineWithConfig`/`hybrid` 配置；progressive session 隔离；predicate parser 复用；结构化搜索参数 | F-18、F-31、F-32 | **M** | MCP 更稳更省 token |
| C2 | 版本迁移：`AppConfig.Version` 读取 + migrate 钩子 | F-35 | **M** | 可持续演进 |

### 12.2 "如果只做三件事"

1. **A1 + A2：修认证 + 让缓存生效**（约 2 天）。这两个是"用户立刻能感知"的正确性缺陷：认证全挂、每条命令多一次网络往返且离线不可用。它们的修复也顺带把 `InjectAuth` 的错误语义、spec 加载的缓存层级理顺——即 **修功能的同时收紧两条最烂的架构接缝**。
2. **A3 + A4：撤 init 组合根 + 合并 MCP 启动路径**（约 3–4 天）。这是"继续加功能前必须付的税"：不做的话，后续每加一个命令/接口都会复制一份全局装配与启动逻辑（F-06 已经是第一次复制）。做完后 `cmd` 可 e2e 测试，router 变成可单测纯函数。
3. **A6 + A7：错误分类 + HTTP 生命周期**（约 3 天）。它们是所有"静默失败/挂起/配置无效"问题的共同根：`InjectAuth` 吞错、router 覆盖原始错误、46 处 `_ =`、无超时、无 context——统一 errkind + 统一 client + `NotifyContext` 之后，F-25 的大部分点自然消失，F-39 的矩阵也只剩"显式未实现"。

> 若只能做一件：**A1**（认证是 P0，且修复面极小）。若目标是"半年后这个仓库还能改得动"：**A3+A4**。

---

## 附录 A：包依赖矩阵（`go list -deps ./pkg/X`，仅列 open-bridge 包）

```
cmd/ob            -> tui/install, router, completion, cli, mcp, config, spec, credential, request, semantic, codegen
pkg/router        -> cli, mcp, config, spec            (+传递: codegen, credential, request, semantic)
pkg/cli           -> codegen, config, credential, request, semantic, spec
pkg/mcp           -> config, credential, request, semantic, spec
pkg/completion    -> config, semantic, spec
pkg/request       -> config, credential, spec
pkg/config        -> spec
pkg/tui/install   -> config, spec
pkg/spec          -> (无)
pkg/semantic      -> (无)
pkg/codegen       -> (无)
pkg/credential    -> (无)
```

结论：DAG 无环（✅ 保留）；`router` 扇出 4、`cmd/ob` 扇出 11、`config→spec` 是唯一跨"领域→解析"边（未来循环风险点，见 F-04/F-38）。

## 附录 B：证据索引（按发现编号）

| ID | 严重度 | 状态 | 关键 file:line |
| --- | --- | --- | --- |
| F-01 | P0 | 已确认 | `cli/handler.go:133`；`request/builder.go:107-118`；`credential/keyring.go:312-317,382-391`；`cmd/ob/main.go:230-236` |
| F-02 | P1 | 已确认 | `cmd/ob/main.go:38-89`；`main_test.go:460-469`；`config/config.go:336-358`；`credential/keyring.go:207-233` |
| F-03 | P2 | 已确认 | `cli/handler.go:25-51`；`mcp/handler.go:30-40`；`grep '^type .* interface'` = 4 |
| F-04 | P3 | 已确认 | `builder.go:108-110`；`main.go:76,82,926`；`router.go:195-199`；`progressive.go:79-81` |
| F-05 | P1 | 已确认 | `config.go:18-303,331-494`；`install.go:345-427,563-849`；`spec_cache.go`；`spec_watcher.go`；`export.go`；`profile.go` |
| F-06 | P1 | 已确认 | `cmd/ob/main.go:901-951` vs `router.go:214-250`；`main.go:915` vs `router.go:229`；`main.go:934-937` vs `router.go:204-205` |
| F-07 | P2 | 已确认 | `router.go:54-56,74-96,118-123` |
| F-08 | P2 | 已确认 | `cli/handler.go:318-357,417-452,496-529,635-664`；`tui/install/model.go:1287-1305`；`config/install.go:27-107,774-849` |
| F-09 | P2 | 已确认 | `cli/handler.go:417-452`；`args.go:37-85`；`spec/parser.go:632-706`；`completion.go:386-405` |
| F-10 | P2 | 已确认 | `config.go:361-405`；`keyring.go:260-300`；`install.go:662-683` |
| F-11 | P2 | 已确认 | `tui/install/model.go:20-38,42-63,65+,1287-1305`（1347 行/72 方法） |
| F-12 | P2 | 已确认 | `codegen.go:40`；`semantic/verb.go:459-506`；`mcp/handler.go:20-27`；`main.go:39-49` |
| F-13 | P3 | 已确认 | `codegen/codegen.go:39-66` |
| F-14 | P2 | 推断 | `mcp/handler.go:20-27,42-51`；`router.go:241-243`；`main.go:943-945` |
| F-15 | P3 | 已确认 | `semantic/verb.go:459-506`；`tool_naming.go:34-38` |
| F-16 | P3 | 推断 | `spec/parser.go:632-648`；`config.go:453-494` |
| F-17 | — | 已确认（阴性） | `go test -race -short ./pkg/{mcp,config,semantic,spec}` 全绿 |
| F-18 | P2 | 已确认 | `mcp/engine.go:40-101`；`codegen.go:30-34`；`spec_watcher.go:42-52` |
| F-19 | P2 | 已确认 | 733 导出符号统计（§0.3）；`config/export.go:16-133` 等 |
| F-20 | P2 | 已确认 | `main.go:241-257`；`builder.go:121-132`；`spec/parser.go:374-...`；`cli/handler.go:496-529`；`mcp/engine.go:94-101` |
| F-21 | P3 | 已确认 | `main.go:82,926`；`engine.go:85-91`；`install.go:27-107,238-263` |
| F-22 | P1 | 已确认 | `cli/errors.go:47-89`（字符串嗅探 70-86）；`:91-838`（渲染 750 行）；`:752-838`（Levenshtein） |
| F-23 | P2 | 已确认 | `cli/errors.go:16-36`；`main.go:91-99` |
| F-24 | P2 | 已确认 | `progressive.go:116-125`；`codegen.go:61-66` |
| F-25 | P2 | 已确认 | `builder.go:112-115`；`router.go:59-62`；`main.go:66-70,230-236`；`install.go:407-416`；`progressive.go:508-514`；46 处 `_ =` |
| F-26 | P1 | 已确认 | `cmd/ob/main.go` 全文（1057 行，职责表 §7）；`main_test.go`（10 个纯函数 Test） |
| F-27 | P2 | 已确认 | `install.go:345-427,500-533,563-755,756-770`；实测 shim → `/usr/local/bin/ob`（旧二进制） |
| F-28 | P2 | 已确认 | `cli/args.go`（923）、`cli/handler.go`（892）、`cli/errors.go`（838）；`.golangci.yml` gocyclo 豁免 |
| F-29 | — | 已见 F-11/F-26 |
| F-30 | P3 | 已确认 | 同 F-13 |
| F-31 | P2 | 已确认 | `progressive.go:20-24,45-56,128-140,431-437,508-514`；`registry.go:24,315-340`；`engine_predicate.go:43-56` |
| F-32 | P2 | 已确认 | `engine_predicate.go:43-56,66-90`；`engine.go:40-101`；`go.mod:23 vulcand/predicate` |
| F-33 | P3 | 已确认 | `mcp/tool_naming.go:9-62,120+`；`registry.go` convertOperationToTool |
| F-34 | P2 | 已确认 | `semantic/verb.go:128-142`；`semantic/resource.go:63-91`；`docs/reference/cli.md:44-54` |
| F-35 | P2 | 已确认 | `config.go:38-39,478-480,680-723`；`install.go:275`；`export.go:69` |
| F-36 | P2 | 已确认 | `config.go:56-103,453-461`；`mcp/handler.go:142`；`main.go:726-729` |
| F-37 | P2 | 已确认 | `semantic/verb.go:39-80,222-255,459-547`；`semantic/resource.go:138-157,295-408` |
| F-38 | P1 | 已确认 | `spec/parser.go:835-839,264-294`；`config/spec_cache.go:29-80,206-231,551-593`；`install.go:407-416`；实测访问日志 |
| F-39 | P1 | 已确认 | F-39 矩阵各行证据；`grep crypto/tls` = 0；`config.go:79,90-143,210-315` |
| F-40 | P2 | 已确认 | 10 处 `context.Background()`；`main.go:158`（cobra.Execute 非 Context）；`router.go:249` |
| F-41 | P1 | 已确认 | `cli/handler.go:45`；`mcp/handler.go:32`；`progressive.go:80`；对照 `parser.go:176-178` 有 30s |
| F-42 | P2 | 已确认（死）+推断（启用后泄漏） | `spec_watcher.go:103-720`；生产零实例化（grep）；`spec_watcher_test.go` 1001 行 |
| F-43 | P2 | 已确认 | `config.go:453-463`（0644/无 fsync）；`spec_cache.go:551-593`（非原子）；`:696-716` |
| F-44 | P2 | 已确认 | `install.go:372-427,299-318`；`main.go:230-236,286-304` |
| F-45 | P3 | 已确认 | `tui/install/model.go:1287-1305`；`config/install.go:381-385,407-416` |
| F-46 | P2 | 已确认 | `docs/reference/cli.md:18`、`QUICKSTART.md:28` vs `main.go:122`；实测 |
| F-47 | P2 | 已确认 | `README.md:172-175`、`docs/reference/cli.md:38-41`；`cli/handler.go:289-315,358-380`；实测 |
| F-48 | P1 | 已确认 | `main.go:126-137,361-366`；实测 `ob demo pets list`；cobra legacyArgs |
| F-49 | P2 | 已确认 | `main.go:103-110`；`router.go:74-96`；实测 `ob-arch --version`；`install.go:756-770` |
| F-50 | P3 | 已确认 | `README.md:20`、`docs/index.md:21`、`docs/guide/introduction.md:20` vs `pkg/codegen/*`、`cli/handler.go:171-210` |
| F-51 | P3 | 已确认 | 计时实测（附录 C）；`grep 'func Benchmark'` = 0 |
| F-52 | P3 | 已确认 | AGENTS.md §3.1/§4.1/§4.2 vs 3 个缺 tag 的集成测试、1 个 sentinel、无接口；`.golangci.yml` gosec disable |
| F-53 | P3 | 已确认 | `proposals/01_init.md:41-46` |

## 附录 C：复现脚本

### C.1 spec 重复拉取计数（F-38 核心证据）

```bash
set -e
rm -rf /tmp/obsrv && mkdir -p /tmp/obsrv
cat > /tmp/obsrv/openapi.json <<'EOF'
{"openapi":"3.0.0","info":{"title":"Demo","version":"1.0.0"},
 "servers":[{"url":"http://127.0.0.1:18089"}],
 "paths":{"/pets":{"get":{"operationId":"list_pets","responses":{"200":{"description":"ok"}}}}}}
EOF
cat > /tmp/obsrv/count_server.py <<'EOF'
import http.server, socketserver
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        with open('/tmp/obsrv/access.log','a') as f: f.write(self.path+"\n")
        body=open('/tmp/obsrv/openapi.json','rb').read()
        self.send_response(200); self.send_header('Content-Type','application/json')
        self.send_header('Content-Length',str(len(body))); self.end_headers(); self.wfile.write(body)
    def log_message(self,*a): pass
with socketserver.TCPServer(("127.0.0.1",18089),H) as s: s.serve_forever()
EOF
: > /tmp/obsrv/access.log
python3 /tmp/obsrv/count_server.py &   # 后台
go build -o /tmp/ob ./cmd/ob
export HOME=/tmp/ob-spec-home && rm -rf $HOME && mkdir -p $HOME
/tmp/ob install demo --spec http://127.0.0.1:18089/openapi.json >/dev/null 2>&1
echo "after install: $(wc -l < /tmp/obsrv/access.log)"          # => 2（LoadSpec + FetchWithCache）
/tmp/ob run demo pets list >/dev/null 2>&1; echo "run1: $(wc -l < /tmp/obsrv/access.log)"   # => 4
/tmp/ob run demo pets list >/dev/null 2>&1; echo "run2: $(wc -l < /tmp/obsrv/access.log)"   # => 6
/tmp/ob run demo --help >/dev/null 2>&1;     echo "help: $(wc -l < /tmp/obsrv/access.log)"  # => 7
find "$HOME/Library/Application Support/openbridge/apps/demo" -type f   # cache/spec.json 存在但无人读取
```

### C.2 启动计时（F-51）

```bash
python3 - <<'PY'
import subprocess,time,statistics
ts=[]
for _ in range(20):
    t=time.perf_counter(); subprocess.run(["/tmp/ob","--version"],capture_output=True)
    ts.append((time.perf_counter()-t)*1000)
print("median %.1fms min %.1fms max %.1fms" % (statistics.median(ts[1:]), min(ts[1:]), max(ts[1:])))
PY
# 本机：median 6.2ms / min 5.8 / max 7.3（首次 770ms，macOS 校验）
```

### C.3 文档断点（F-46/F-47/F-48）

```bash
export HOME=/tmp/ob-spec-home          # 已装 demo（C.1）
/tmp/ob version                        # unknown command "version" for "ob"
/tmp/ob demo pets list                 # unknown command "demo" for "ob"
/tmp/ob run demo pets list --json | head -2     # 与默认输出一致（--json 被丢弃）
/tmp/ob run demo pets list --output json | head -2
```

### C.4 并发/依赖/接口统计

```bash
go test -race -short ./pkg/mcp ./pkg/config ./pkg/semantic ./pkg/spec
go list -deps ./pkg/router | grep open-bridge
grep -rhoE '^(func|type|var|const) [A-Z][A-Za-z0-9_]*' --include='*.go' pkg | wc -l   # 733
grep -rn '^type .* interface' --include='*.go' pkg cmd internal | grep -v _test      # 4
grep -rn 'var Err[A-Z]\|var err.*= errors.New' --include='*.go' pkg cmd | grep -v _test  # 1
grep -rn 'crypto/tls' --include='*.go' . | wc -l                                      # 0
```

## 附录 D：值得保留的设计（避免重构时误伤）

1. **DAG 无环 + 叶子包干净**（附录 A）：`spec`/`semantic`/`codegen`/`credential` 无内部依赖，是后续 `internal/` 化的良好基础。
2. **`ManagerOption`/`ParserOption` 模式**（`config.go:321-335`、`spec/parser.go:145-185`）：已是标准 Go options，继续用，只是调用点从 `init()` 移出。
3. **原子写 `writeConfigAtomically`**（`config.go:453-463`）：思路正确，只需补 fsync/权限/唯一 tmp 名（F-43）。
4. **MCP progressive disclosure 的产品直觉**（`progressive.go`）：三 meta-tool 对大 API 是对的；需要修的是 session 隔离与命名/DSL 细节（F-31/F-32），不是方向。
5. **测试密度**（17.5k 行/535 Test/7 Property）：`semantic`、`spec`、`request` 的测试是资产；`internal/proptest` + gopter 属性测试（`config_property_test.go`、`export_property_test.go`）应扩展到 `Duration`/`AuthConfig`/`SpecCacheMeta`。
6. **错误 UX 的用心**（`errors.go` 的 `FormatUsageHelp`/Levenshtein 推荐）：用户价值真实，只是放错层、用错机制；拆到 `internal/errfmt` + 类型化分类后可保留全部 UX。
7. **`x-cli-*` 扩展思路**（`semantic/verb.go:128-142`）：用 spec 扩展对抗启发式误判是正确方向，缺的是文档与校验（F-34）。

---

*报告结束。全文 53 条编号发现（F-01…F-53；其中 F-29 为合并说明不计级）。评级分布：**P0×1、P1×9、P2×29、P3×12**，另有 F-17 阴性验证不计级；含推断成分的 4 条为 F-14、F-16、F-42、F-51。所有 P0/P1 均给出可复现命令或直接代码引用。*
