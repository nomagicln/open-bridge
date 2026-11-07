# OpenBridge 软件工程诊断报告

> **诊断对象**：`github.com/nomagicln/open-bridge`（Go 1.25.5；OpenAPI → 语义 CLI + MCP server 运行时引擎）
> **诊断基线**：工作树 `dd18374`（分支 `fix/install-and-args`），`git status` 干净
> **代码规模**：非测试 Go **18,420 LOC** / 测试 **17,527 LOC**；81 个 `.go` 文件、15 个包
> **诊断方式**：Team Lead + 4 名独立领域评审员并行；静态阅读 + 依赖图 + 黑盒实测（mock HTTP server / MCP JSON-RPC / 真实 CLI e2e）+ `-race` + 覆盖率 + 微基准
> **证据来源**：Lead 亲自复现标记 ✅；代码路径确认标记 🔍；团队实测数据标记 📊；无实测的推断标记 ⚠️
> **交付物**：本文件为汇总报告；4 份领域详报（共 4,933 行 / 144 条原始发现）位于 `.diagnosis/`
> **约束遵守**：全程未修改任何源码、配置、`.golangci.yml`、`go.mod`，未执行 git 写操作；临时产物均在 `/tmp`

---

## 0. 执行摘要

### 0.1 总评

**一句话结论：这是一个"能跑、启动很快、MCP 有亮点"的项目，但它当前的状态是"承诺 > 实现"——若干对外承诺的核心能力（认证、输出格式、profile、跨进程缓存、TLS、安全控制）在代码里存在、在文档里宣称、但在运行时失效或从未接线；而唯一能拦住这些问题的质量门禁（lint / 集成测试 / 覆盖率 / 安全检查 / DCO）恰好也全部失效。**

综合评分（10 分制，以"可交付给真实用户、可持续演进"为标准）：

| 维度 | 评分 | 结论 | 最高严重度 |
| --- | :---: | --- | --- |
| 架构与代码设计 | **C+** | 依赖 DAG 无环、局部设计有亮点；但组合根在 `init()`、双 MCP 启动路径、`pkg/config` 上帝包（10.4k 行 / 243 导出）、约 4.5k 行死代码 | P0 |
| 功能正确性 | **D** | 4 条用户可直接触发的功能缺陷：认证全链路失效、`--json/--yaml/--output` 静默无效、`--profile` 静默无效、`ob <app>` 直连路由不可达 | P0 |
| 安全 | **D** | 3 条已确认凭据外泄路径；keyring 无系统后端时固定口令回退；TLS 配置零接线；`gosec` 被禁用；`govulncheck`/CodeQL/Dependabot 全无 | P1（已确认漏洞） |
| 性能 | **C** | 启动 6.4–6.9ms（远优于 100ms 声明）；但产品路径每条命令重解析 spec（4.1MB → 119–134ms，14.4MB → 0.49–0.52s/命令），持久化缓存是空壳，大响应内存放大 7.8× | P1 |
| 代码风格与可维护性 | **B−** | gofmt/goimports 全绿、导出注释 94.9%、命名一致性较好；但错误分类靠字符串、0 个导出 sentinel、`%w` 包装率 54%、复杂度热点靠 `//nolint` 放行、lint 工具链不可运行 | P1 |
| 测试 | **D+** | 535 个 Test / 17.5k 行测试，但覆盖率 55.3%，`pkg/router` 与 `pkg/tui/install` 0%、`cmd/ob` 19%；集成测试 6 个文件仅 1 个带 tag，且带 tag 的组合**编译失败**；0 个 Benchmark；7 个 property test 全部在 `-short`/CI 中跳过 | P1 |
| CI / 工程流程 | **C−** | 3 OS 矩阵 + `-race` + GoReleaser 是亮点；但 lint 用 `latest` 不可复现、无覆盖率门禁、无安全扫描、integration tag 测试从不运行、DCO 42 提交 37 个缺 sign-off、`make install` 必失败 | P1 |
| 文档与实现一致性 | **C−** | `ob version`、表格式默认输出、"Zero Code Generation"、"Cold start <100ms" 等宣称与实现不符；安装向导宣称的 TLS/安全能力实际未接线 | P2 |

**总体评级：C（可运行的原型，尚不具备生产交付质量）。**
当前功能面窄（单进程、单命令），大量问题"暂时不爆"；但一旦按 README 承诺补齐多 app 服务、离线/私有 spec、TLS、重试、真正的跨进程缓存，现有结构会成组失效。

### 0.2 最高优先级问题（P0 / P1 汇总，去重后 16 条）

| # | 严重度 | 问题 | 关键证据 | 验证 |
| :---: | :---: | --- | --- | :---: |
| D-01 | **P0** | **CLI 认证全链路失效**：`InjectAuth` 传空 appName，keyring key 永不命中，且错误被静默吞掉并伪装成"无需认证" | `pkg/cli/handler.go:133`、`pkg/request/builder.go:107-118`、`pkg/credential/keyring.go:316-318` | 🔍✅ |
| D-02 | **P1** | **凭据外泄链 A**：query 型 API key（或用户直接传入的敏感 query 参数）被网络错误信息完整回显 | `pkg/request/builder.go:141-150` + `pkg/cli/errors.go:116-155` | 🔍 |
| D-03 | **P1** | **凭据外泄链 B**：`--generate` 四种语言生成器输出未掩码 URL，`ProtectSensitiveInfo` 对 query 参数失效 | `pkg/codegen/curl.go:37-42`、`go.go`、`nodejs.go`、`python.go` | 🔍 |
| D-04 | **P1** | **凭据外泄链 C**：`ob info -o json/yaml` 输出未掩码 Header（text 模式才掩码） | `cmd/ob/main.go:610-632` vs `:719-733` | 🔍 |
| D-05 | **P1** | **持久化 spec 缓存是空实现**：`LoadSpecWithPersistentCache` 忽略 `appName` 直接同步解析；`Save/LoadParsedSpec` 生产零调用；4.1MB spec 每条命令 119ms / 109MB / 1.24M allocs | `pkg/spec/parser.go:835-839`、`pkg/config/spec_cache.go:694,739` | 📊✅ |
| D-06 | **P1** | **远端 spec 每条命令重复下载**：install 时写盘缓存，运行时读路径从不读缓存；install 本身还下载两次 | `pkg/spec/parser.go:264-295`、`pkg/config/install.go:378-416` | 📊 |
| D-07 | **P1** | **`--json` / `--yaml` / `--output` 静默无效**：CLI flag 在格式化前被从参数表删除，输出恒为 YAML；文档宣称"Table 默认"，实际默认 YAML | `pkg/cli/handler.go:289-315,335-354,358-369` | ✅ |
| D-08 | **P1** | **`--profile` 静默无效**：`getProfile` 只用默认 profile，未知 profile 不报错；`profile.Headers`/`QueryParams` 在 CLI 完全不生效（MCP 生效） | `pkg/cli/handler.go:476-493`、`pkg/mcp/handler.go:142` | ✅ |
| D-09 | **P1** | **MCP 安全控制不执行**：`require_confirm`、`max_requests_per_minute`、`dangerous_operation_patterns`、`hybrid_search` 仅有配置结构，无运行时逻辑；实测 limit=1 时连续 3 次调用全部执行、POST 无确认 | `pkg/config/config.go:228-315` | ✅ |
| D-10 | **P1** | **HTTP 全部无超时**：CLI/MCP/progressive 三处 `&http.Client{}`；`Profile.Timeout` 是死字段；实测服务端挂起时 CLI 永久挂起 | `pkg/cli/handler.go:45`、`pkg/mcp/handler.go:32`、`pkg/mcp/progressive.go:80` | 📊✅ |
| D-11 | **P1** | **`ob <app> ...` 直连路由不可达**：cobra `legacyArgs` 在 RunE 前拦截 unknown subcommand；install 帮助文本却指导用户这样用 | `cmd/ob/main.go:126-137,361-366` | ✅ |
| D-12 | **P1** | **keyring 静默回退 + 硬编码口令**：无系统后端时回退 FileBackend，口令固定为常量 `"openbridge"`；`detectBackend()` 在 darwin 一律报告 keychain（误导用户） | `pkg/credential/keyring.go:191-198,286-301` | 🔍 |
| D-13 | **P1** | **集成测试基础设施失效**：6 个集成测试文件仅 1 个带 `//go:build integration`；`go vet -tags=integration ./...` 因函数重名**编译失败**；CI 从不运行 tagged 测试 | 见 §7.3 | ✅ |
| D-14 | **P1** | **lint "宪法"不可执行**：本机 v1.64.8 报配置错误（v2 格式），v2.8.0 与 Go 1.27 export data 不兼容 panic；Makefile 与 CI 用 `latest`，三处版本漂移 | `.golangci.yml`、`Makefile:30`、`ci.yml:105-109` | ✅ |
| D-15 | **P1** | **`ob list`/`ob info` 解析所有 app 的 spec**：3 app（含 4.1MB）`ob list` p50 166.9ms（空 HOME 8.99ms）；install 时已算过这些数据 | `pkg/config/install.go:898-925`、`cmd/ob/main.go:474-501` | 📊🔍 |
| D-16 | **P1** | **测试/流程门禁失效**：覆盖率 55.3%、`pkg/router`/`pkg/tui` 0%、298/988 函数 0%；`pkg/spec` 零 property test；DCO 42 提交 37 个缺 sign-off | `go tool cover`；`git log` | ✅ |

### 0.3 关键度量基线（本机 macOS arm64 / Go 1.27.3 / M3 Max，warm）

| 指标 | 实测值 | 采集方式 |
| --- | --- | --- |
| `ob --version` 冷启动 | **6.4–6.9ms**（p50） | Python `perf_counter` ×15–20；远优于 README 的 <100ms |
| `ob`（无参）/ `ob --help` | 6.63ms / 6.40ms | 同上 |
| `ob run <app>`（0.07MB spec） | 11.2ms | e2e 实测 |
| `ob run <app> --help`（4.09MB spec） | **119–134ms** | perf 微基准 + e2e |
| `ob run <app>`（14.42MB spec） | **0.49–0.52s / 命令** | perf 实测 |
| `ob list`（3 app，含 4.1MB spec） | **166.9ms** | perf 实测（空 HOME 8.99ms） |
| shell 补全（4.1MB spec） | **133.6ms / Tab** | perf 实测 |
| 5MB 响应峰值 RSS | **62.0MB**（基线 23.2MB，**7.8×**） | perf `/usr/bin/time -l` |
| 单元测试覆盖率 | **55.3%（-short）/ 56.5%（full）** | Lead 复跑 + process 实测 |
| Test 函数 / Property test / Benchmark | **535 / 7 / 0** | grep 统计 |
| 集成测试文件带 tag 比例 | **1 / 6** | 逐文件检查 |
| `golangci-lint` | **无法运行**（v1 配置不兼容；v2 与 Go 1.27 不兼容） | 实测 |
| `gofmt -l` / `goimports -l` | **0 / 0** | 实测 |
| `go vet` / `go test -race -short` | **通过 / 全绿** | 实测 |
| `go mod tidy -diff` / `goreleaser check` | **干净 / 通过** | 实测 |
| `pkg/` 导出符号 / 接口定义 | **733 / 4** | grep 统计 |
| DCO 合规 | **42 提交仅 5–6 个带 sign-off（37 缺失）** | 实测 |

### 0.4 团队分工与详报索引

| 领域 | 评审员 | 详报 | 原始发现数 | 分级 |
| --- | --- | --- | :---: | --- |
| 架构与代码设计 | arch-reviewer | `.diagnosis/architecture.md`（1,192 行） | 53 | P0×1 / P1×9 / P2×29 / P3×12 |
| 性能 | perf-analyst | `.diagnosis/performance.md`（960 行） | 15 | P0×1 / P1×5 / P2×6 / P3×3 |
| 代码风格与可维护性 | style-auditor | `.diagnosis/style.md`（1,499 行） | 34 | P1×10 / P2×12 / P3×12 |
| 测试 / 流程 / 安全 | process-auditor | `.diagnosis/process.md`（1,282 行） | 42 | P1×9 / P2×26 / P3×7 |
| 汇总、交叉验证与基线 | Lead（本文件） | `ENGINEERING-DIAGNOSIS.md` | 去重后约 60 条 | P0×1 / P1×15 / P2×28 / P3×16 |

> 下文只展开"跨领域、高价值"的发现；每个领域的完整清单请查对应详报。

---

## 1. 先看好的方面（避免误读为全盘否定）

诊断的公正性要求先确认优点，这些优点应保留：

1. **启动性能真的很好**：`--version` 6.4–6.9ms，远优于 README 的 100ms 声明；`init()` 虽有问题但未造成启动灾难。
2. **依赖图无环、核心包干净**：`go list -deps` 证明无循环依赖；`pkg/spec`、`pkg/semantic`、`pkg/codegen`、`pkg/credential` 零内部依赖，是潜在的可独立复用单元。
3. **格式化与基础静态检查全绿**：`gofmt`/`goimports` 0 处漂移，`go vet` 通过，`errcheck`/`misspell` 通过，`go mod tidy -diff` 干净。
4. **MCP 是真正的亮点**：stdio 模式 JSON-RPC 握手、工具列表、progressive disclosure 三 meta-tool 全部实测可用；`read_only_mode` 在 progressive 搜索中确实过滤掉 POST（实测 `MethodIs("POST")` 返回 0 条）。
5. **设计意图正确**：凭据放 keyring 而非配置文件、配置写入用 tmp+rename 原子替换、错误信息做了人类可读排版、`ManagerOption`/`ParserOption` 的 options 模式。
6. **测试数量可观**：535 个 Test、17.5k 行测试代码、7 个 property test（config/export/credential），`-race` 全绿。
7. **发布工程有基础**：GoReleaser v2 配置通过校验、三平台构建、三 OS CI 矩阵、DCO 检查脚本、依赖版本 pin 的 `go.sum` 完整。
8. **没有 TODO/FIXME 遗留**：全仓 0 处 TODO/FIXME/XXX；导出符号注释覆盖 94.9%。

问题不在于"没做事"，而在于**做完的功能没有验证闭环，没做完的功能被写成已完成**。

---

## 2. 功能正确性缺陷（用户可直接触发）

> 以下 4 条均经 Lead 用隔离 `HOME` + 本地 mock server 端到端复现（✅），源码路径已定位（🔍）。

### 2.1 [P0] D-01 CLI 认证全链路失效

**现象**：安装时输入/存储了 Bearer/API Key/Basic 凭据，运行时 CLI **从不发送认证头**；由于错误被静默吞掉，用户完全无感。

**证据链**：

```go
// pkg/cli/handler.go:133 —— CLI 路径把 appName 传成空串（appName 参数在函数签名里是 "_ string"）
if err := h.reqBuilder.InjectAuth(req, "", profile.Name, &profile.Auth); err != nil {
```

```go
// pkg/request/builder.go:107-118 —— 查不到凭据时直接 return nil，吞掉错误
cred, err := b.credMgr.GetCredential(appName, profileName)
if err != nil {
    return nil // No credential, skip auth
}
```

```go
// pkg/credential/keyring.go:316-318 —— key 格式 ob__<app>__<profile>
func buildKey(appName, profileName string) string {
    return fmt.Sprintf("%s__%s__%s", KeyringPrefix, appName, profileName)
}
```

因此 CLI 查询的是 `ob____default`，而安装写入的是 `ob__petapi__default`，**永远不命中**。
MCP 路径反而是正确的：`pkg/mcp/handler.go:138`、`pkg/mcp/progressive.go:475` 传的都是 `h.appConfig.Name`。
`git log -S` 显示该缺陷由 `9546058 refactor: comprehensive code simplification` 引入，属于重构回归；现有测试全部在 `builder_test.go` 里直接给 `InjectAuth` 传正确 appName，**没有覆盖 CLI handler → builder 的集成路径**。

**影响**：本产品"Secure Credentials"卖点对 CLI 完全失效；所有需要认证的 API 在 CLI 模式下不可用；且属于**安全降级**（用户以为已认证，实际匿名请求）。

**修复（S，约 0.5 天）**：`cli/handler.go:133` 传入真实 `appName`（调用点已具备）；`InjectAuth` 区分 `CredentialNotFound`（静默）与后端错误（上抛/提示）；补一条 e2e 回归测试。

### 2.2 [P1] D-07 输出格式开关静默无效

**实测**：`ob run petapi pets list --json`、`--yaml`、`--output json`、`-o json` 全部输出 YAML；未知 flag `--badflag`、拼写错误 `--lim` 被静默忽略（不报错也不生效）。

**根因**（`pkg/cli/handler.go`）：

```go
// :335-354
generateFormat, generateOutput, cleanParams := extractCLIFlags(params)
...
return h.formatAndPrintOutput(body, cleanParams)   // ← cleanParams 里 output/json/yaml 已被删除

// :304-313 extractCLIFlags 把 "output"/"json"/"yaml" 从 map 中剔除
// :358-369 determineOutputFormat 因此永远走 default → "table" → FormatOutput 再 default → yaml
```

**影响**：脚本化/CI 消费 JSON 的能力失效；文档（README、`docs/reference/cli.md`）宣称"Table 输出（默认）"与实际（YAML 默认、开关无效）双重不符。

**修复（S）**：把输出格式从 `params` 中单独解析并传入 `formatAndPrintOutput`；对未知 flag 报错。

### 2.3 [P1] D-08 `--profile` 与 profile 级配置静默失效

- `h.getProfile(appConfig)`（`pkg/cli/handler.go:476-493`）**只读 `appConfig.DefaultProfile`**，完全忽略 `--profile`；不存在的 profile 也不报错。实测：default 指向 58123、prod 指向 58124，`--profile prod` 仍然请求 58123。
- `profile.Headers` 只在 MCP 生效（`pkg/mcp/handler.go:142`、`progressive.go:479`），CLI 请求里没有；实测服务端记录中 CLI 无 `X-Profile-Header`，MCP 有。
- `profile.QueryParams`、`profile.TLSConfig`、`profile.Timeout`、`profile.RetryConfig` 在运行期**全部零接线**（全仓 `crypto/tls` 引用数 = 0）。
- `Profile.BuildSpecFetchOptions`（`pkg/config/config.go:124`）在生产代码中零调用。

**影响**：文档承诺的 profile 切换（dev/prod）、私有 spec 拉取认证、超时/重试/TLS 全是空头支票；MCP 与 CLI 行为不一致。

**修复（M）**：计划一个统一的 `request.Profile` 应用层（headers/query/auth/timeout/TLS/retry 一次装配），CLI 与 MCP 共用；`--profile` 进入解析并校验存在性。

### 2.4 [P1] D-11 `ob <app> ...` 直连路由不可达

install 帮助明确写：

```text
ob myapp get-user --id=123        # cmd/ob/main.go:361-366
```

但实测 `ob petapi` 返回 `unknown command "petapi" for "ob"`。原因：`cmd/ob/main.go:126-137` 的 `RunE` fallback（"Handle unknown subcommands as app names"）**永远不会被调用**——cobra 对"有子命令的根命令"在 `ValidateArgs` 阶段就用 `legacyArgs` 拒绝了未知子命令。

同时 `pkg/router/router.go:118-123` 的 `handleOBCommand()` 返回 "ob commands should be handled by CLI framework"，是第二条不可达分支。

**修复（S）**：设置 `rootCmd.Args = cobra.ArbitraryArgs` 或改用 `FParseErrWhitelist`/自定义 `Find`；删除 `handleOBCommand`；二选一后同步帮助文本与文档。

### 2.5 [P1] D-09 MCP 安全控制"只配置、不执行"

`SafetyConfig` 的字段执行情况实测矩阵：

| 字段 | 声称 | 实际 | 证据 |
| --- | --- | --- | --- |
| `read_only_mode` | 只读 | ✅ 生效（progressive 搜索/工具构建均过滤） | `pkg/mcp/handler.go:270-276`、`registry.go:112-118` |
| `allowed_operations` / `denied_operations` | 白/黑名单 | ✅ 生效 | `pkg/mcp/handler.go:284-303` |
| `require_confirm` | POST 需确认 | ❌ 无任何执行逻辑；实测 POST 直接执行 | grep 0 命中 |
| `max_requests_per_minute` | 限流 | ❌ 无执行逻辑；实测 limit=1 连续 3 次请求全部执行 | grep 0 命中 |
| `dangerous_operation_patterns` | 危险操作拦截 | ❌ 无执行逻辑 | grep 0 命中 |
| `hybrid_search`（含 Embedder/ONNX/向量） | 混合检索 | ❌ 仅为配置结构（`config.go:277-299`），无实现 | grep 0 命中 |

**影响**：`docs/guide/quick-start.md` 的安装向导把 "Read-Only Mode" 等作为安全卖点呈现；用户启用后以为自己被保护，实际 `require_confirm`/限流未生效。这是**安全承诺失效**，不是单纯缺功能。

**修复（S/M）**：实现（MCP 层拦截 + 确认回执）或在文档中明确标注"未实现"并把字段从 schema 移除；同时给 SafetyConfig 增加矩阵测试（配置字段 ↔ 执行断言）。

### 2.6 [P1] D-05 + D-06 持久化缓存空壳、远端 spec 每命令重下

```go
// pkg/spec/parser.go:835-839
// LoadSpecWithPersistentCache loads an OpenAPI specification with persistent caching support.
// This enables cross-process caching of parsed specs to avoid re-parsing on each restart.
func (p *Parser) LoadSpecWithPersistentCache(ctx context.Context, source string, appName string) (*openapi3.T, error) {
    return p.LoadSpecWithContext(ctx, source)   // ← appName 被忽略，持久化缓存不存在
}
```

- `SaveParsedSpec` / `LoadParsedSpec` / `ValidateParsedSpec`（`pkg/config/spec_cache.go:694/739/784`）**生产代码零调用**（仅测试）。
- 远端 URL spec：install 时 `FetchWithCache` 写盘，但运行期 `parser.loadFromURL`（`:264-295`）每次直接 `client.Do`，**从不读盘**；实测 install 阶段 2 次 GET（第一次 `LoadSpec`、第二次 `FetchWithCache`），之后每条命令再 1 次 GET。
- 本地 spec：每条命令重新 `kin-openapi` 解析 + 全量 `doc.Validate`。实测 4.09MB spec = **119ms / 109MB / 1.24M allocs per command**。

**关键反直觉结论（perf 实测）**：即便把现有死代码接上也没有收益——`LoadParsedSpec` 采用"整棵 `openapi3.T` JSON 往返"，4MB spec 实测 **118ms / 101MB**，与重新解析（119ms）等价。**正确的方向是缓存下游派生产物（语义命令树的 gob 索引），而不是缓存 AST。**

### 2.7 其他功能正确性问题（P2，详见详报）

- `ob version` 被 README、QUICKSTART、docs 三处文档提及，但命令不存在（只有 `--version`）。
- `Profile.Version`（配置版本/迁移字段）只写不读，无任何迁移逻辑（`pkg/config/config.go:38-39,478-480`）。
- 配置/缓存文件写 0644、目录 0755（`config.go:332,455`、`spec_cache.go:551-593`），且 tmp 用固定 `.tmp` 名、无 `fsync`。
- 路由依赖二进制文件名 `os.Args[0]`（`main.go:103-110`、`router.go:74-96`）：`ob` 被重命名/通过其他 symlink 调用即行为反转（实测 `ob-diag --version` 被当成 app 名）。
- `--generate` 代码生成在未认证时生成"无认证"的 curl（与 D-01 叠加），且 URL 不掩码（D-03）。
- MCP SSE transport 监听 `:port`（全接口、无认证、无 Origin 校验，实测 `*:58125` LISTEN），属 opt-in 风险。

---

## 3. 安全诊断

> 完整 42 条见 `.diagnosis/process.md` §7；本节只列高价值项，并区分"已确认漏洞"与"加固建议"。

### 3.1 [P1｜已确认] 凭据外泄三路径（D-02 / D-03 / D-04）

**路径 A：网络错误回显完整 URL（含 query 中的 API key）**

```go
// pkg/request/builder.go:141-150：query 型 API key 被写进 URL
q.Set(authConfig.KeyName, token)
req.URL.RawQuery = q.Encode()

// pkg/cli/errors.go:120-128,151-153：网络错误时原样打印 url.Error.URL
targetURL = e.URL
fmt.Fprintf(&sb, "Failed to connect to: %s\n", targetURL)
...
fmt.Fprintf(&sb, "  - Try accessing %s in a browser\n", targetURL)
```

触发条件：`auth.location == "query"` + 任何网络错误（DNS/超时/TLS）。key 会进入终端 scrollback、CI 日志、issue 截图。

> 精度说明：由于 D-01 导致 CLI 当前根本不注入 keyring 凭据，今天最直接的触发面是"用户在首个位置参数里直接传入的敏感 query 参数"（如 `--api_key sk-...`）——它同样会进入 URL 并被回显；**一旦 D-01 修复，keyring 中的 query 型凭据也会命中同一路径，且因为认证恢复而更常见。** 因此这两项应同时修复。

**路径 B：`--generate` 四种生成器 URL 未掩码**

`pkg/codegen/curl.go:37-42` 直接 `req.URL.String()`；`go.go:42,115`、`nodejs.go:42`、`python.go:105` 同样。掩码逻辑只覆盖 Header（`codegen.go:146-171`）与 Body（`maskBody`），**URL 完全绕过**。用户勾选 `ProtectSensitiveInfo`（安装向导文案："Mask API keys in code generated for the AI context"）后仍会明文输出。

**路径 C：`ob info -o json/yaml` 未掩码 Header**

`cmd/ob/main.go:610-632` 直接 `json.MarshalIndent(cfg)` / `yaml.Marshal(cfg)`；只有 text 路径（`:719-733`）调用 `isSensitiveHeader`/`maskValue`。而这两个掩码函数本身 **0% 测试覆盖**。

**修复（S，1–3 天）**：统一 `redactURL`（清除 query 值与 userinfo）+ `redactConfig`；4 个生成器统一走掩码；每处配 1 个测试。这是投入产出比最高的安全修复。

### 3.2 [P1｜已确认] keyring 回退使用硬编码口令（D-12）

```go
// pkg/credential/keyring.go:191-198
func getPasswordFunc(cfg *managerConfig) func(string) (string, error) {
    if cfg.filePasswordFn != nil { return cfg.filePasswordFn }
    return keyring.FixedStringPrompt(ServiceName)   // 固定口令 "openbridge"
}
```

- 无桌面后端（Linux headless/CI/容器）时静默回退 FileBackend，用可被源码得知的固定口令加密——等同混淆，不是安全存储。
- `detectBackend()`（`:286-301`）按平台"猜"：darwin 一律返回 `BackendKeychain`，即使实际用的是 FileBackend；`ob install` 的 "Credentials stored securely in keychain" 可能撒谎。
- CI 中 keyring 测试被 `isCI()` 恒跳过（`keyring_test.go:523-532`），凭据路径在自动化里**永不执行**。

**修复（S）**：默认禁止 FileBackend（除非显式 `--allow-file-backend` 且口令来自 env/prompt）；`Backend()` 返回真实后端；CI 增加 file-backend 测试（隔离 HOME）。

### 3.3 [P1｜已确认] TLS 配置全链路未接线

- 全仓 `crypto/tls` 引用 **0 处**；`TLSConfig`（`config.go:210-230`）只在配置结构、install 合并、export/print 中出现，从不构造 `tls.Config`。
- 因此 `insecure_skip_verify`、自定义 CA、mTLS 客户端证书、`min_version` **全部无效**；对自签证书/企业内网 API 直接不可用（只能报 TLS 错误）。
- 与 D-10 叠加：没有任何超时，TLS 握手失败也无法快速失败（依赖系统 TCP 超时）。

### 3.4 [P2｜已确认] 权限、gosec 与供应链

- 配置/缓存文件 **0644**、目录 **0755**（`config.go:332,455`；`spec_cache.go:551,593,696-710`）；`tmpPath = configPath + ".tmp"` 固定名，多进程并发写有竞态；无 `fsync`。
- `.golangci.yml:43-45` 显式 `disable: gosec`（注释"enable later after baseline cleanup"）；CI 无 `govulncheck`、CodeQL、Dependabot、license 检查。
- DCO：42 个提交中 37 个缺 `Signed-off-by`（AGENTS.md §5.2 要求全部 DCO），CI 的 DCO job 只覆盖 push/PR 的增量范围，历史违规未被发现。
- shim：Unix 用 symlink（appName 已由正则校验，无命令注入）；Windows `.cmd` 用已引号包裹的 `obPath` + 校验过的 appName，**未发现注入**，但 `install`/`uninstall` 的 `CreateShim`/`RemoveShim` 内部不重新校验 appName（加固建议）。

---

## 4. 架构与代码设计

> 完整 53 条见 `.diagnosis/architecture.md`；本节给结构性结论。

### 4.1 组合根错位：`init()` + 全局变量 + 双启动路径

```go
// cmd/ob/main.go:38-89
var (
    configMgr *config.Manager
    specParser *spec.Parser
    credMgr *credential.Manager
    ...
)
func init() {
    configMgr, err = config.NewManager()   // 失败 os.Exit(1)
    ...
    credMgr, err = credential.NewManager() // 失败仅 Warning
    ...
}
```

问题：
- 包级可变依赖 + `init()` 中做 I/O（keyring、目录创建），导致**单测无法构造隔离环境**（`main_test.go:460-469` 只能临时替换全局变量）；`internal/integration` 测试会真实打开 keyring、创建用户目录。
- 失败策略分裂：config 失败 `os.Exit`、credential 失败静默降级，没有统一错误出口。
- **两套 MCP 启动实现**：`cmd/ob/main.go:901-951`（`startMCPServer`）与 `pkg/router/router.go:214-250`（`startMCPServer`）各写一份，已经在 server version、缓存使用、调用顺序上漂移（例如 main 走 `mcpHandler` 直连、router 走 progressive 分支；版本号一处 "1.0" 一处缺失）。
- `pkg/router` 名为路由，实为"第二个 main"：它 import `cli+mcp+config+spec`，内含 MCP 启动、参数解析、profile 探测，同时保留不可达的 `handleOBCommand`。

**重构建议（M，2–3 天）**：`internal/app.Deps` + `app.Run(ctx, argv)` 显式构造；`main.go` 收敛为 `main()` + `Execute()`；router 退化为纯函数 `Resolve(argv) (mode, appName, args, error)`；合并两套 MCP 启动。

### 4.2 `pkg/config` 上帝包

- **10,448 行 / 243 个导出符号**，同时承担：领域模型（AppConfig/Profile）、配置 IO、安装流程、shell shim/PATH、TUI 交互提示词、spec 下载缓存、文件 watcher、profile 导出/导入。
- 其中约 **4.5k 行在生产路径零调用**（arch 详报 F-42）：
  - `spec_watcher.go` 778 行 + `fsnotify` 依赖：`NewSpecWatcherManager`/`NewLocalFileWatcher`/`NewRemoteSpecWatcher` 生产零实例化；
  - `profile.go` 581 行的 `ProfileManager` 部分方法无消费者；
  - `export.go` 680 行的 `ExportProfileWithOptions`/`ImportProfile*` 没有 CLI 命令暴露；
  - `EnvironmentCredentialSource`、`Profile.BuildSpecFetchOptions`、`SpecCacheManager.SaveParsedSpec/LoadParsedSpec` 均无生产调用。
- `config → spec` 是唯一"领域 → 解析"跨层依赖，也是未来循环依赖的风险点。

### 4.3 `pkg/cli` 职责过载与错误模型

- `cli/` 3,000+ 行：参数语法（`args.go` 923）、命令执行 + HTTP（`handler.go` 892）、错误排版（`errors.go` 838）。`errors.go` 中约 750 行是**人类可读文案渲染**，真正的错误分类只有约 20 行，且分类方式是 `strings.Contains(err.Error(), ...)`（`errors.go:70-86`；同类问题见 `spec/parser.go:461,479`、`credential/security.go:299`）。
- **全仓 0 个导出 sentinel error**（唯一一个 `errAppConfigNotSet` 在 `mcp/shared.go:13` 且未导出），与 AGENTS.md §4.1 "Define strict sentinel errors" 直接冲突；因此无法用 `errors.Is/As` 做稳定判定，只能嗅探字符串。
- `PrintedError.Error()` 返回空字符串（`errors.go:16-36`），用"空消息"表示"已打印过"，破坏 Go 错误语义（`errors.Is` 依赖 `Unwrap` 才侥幸可用），也让日志/遥测拿不到错误内容。
- 库代码 3 处 `panic`（`mcp/progressive.go:118,121`、`codegen/codegen.go:63`）用于编程错误校验，在可被外部输入触发的路径上应改为 error。
- 46 处显式丢弃错误 `_ =`（非测试）。

### 4.4 接口、导出面与扩展机制

- 全仓仅 **4 个接口**（`config.SpecWatcher`、`spec.UnmarshalStrategy`、`mcp.ToolSearchEngine`、`codegen.Generator`），而组件之间全部以具体指针类型互连（`*config.Manager`、`*spec.Parser` 等）；AGENTS.md §4.2 "依赖定义为接口"未落地，导致几乎无法 mock（现有测试大量依赖真实文件系统/keyring）。
- `pkg/` 导出 **733 个符号**，无 API 稳定性/版本化策略；仓库是应用而非库，`pkg` 的公开 API 面过大（arch 统计 195 个导出函数仅本包使用）。
- `codegen` 用 `init()` 注册全局 registry（`codegen.go:40-66`），但 `register` 未导出、工厂不可注入，实际是"伪扩展点"（当前只有 4 个固定生成器）。
- `semantic` 把 `StandardVerbs`/`ActionVerbs` 作为导出可变 slice（`verb.go:459-506`），可被调用方修改，语义映射有全局副作用风险。
- MCP tool 命名（`tool_naming.go`）与 CLI 语义命名（`semantic/verb.go`）是两套独立启发式规则，同名概念可能得到不同名字。

### 4.5 数据模型与生命周期

- `AppConfig.Version` 注释写"for migrations"，但无读取/迁移逻辑（`config.go:38-39`）。
- `Profile` 是 12 个关注点的拼盘（auth/headers/query/tls/safety/timeout/retry/spec-fetch/protect-sensitive/...），恰好对应 D-08 的"配置面 ≠ 执行面"。
- 全链路无 `context` 传播：10 处 `context.Background()` 硬编码（`cli/handler.go:84`、`spec/parser.go:191,534,590,831` 等）；cobra 用 `Execute()` 而非 `ExecuteContext`；无 `signal.NotifyContext`/优雅关闭。MCP 长驻进程无法被优雅终止（只能杀进程）。
- 安装流程无事务/回滚：Spec 解析成功 → 写配置 → 建 shim → 存 keyring，中途失败会留下"半安装"状态（`install.go:372-427`、`main.go:230-236,286-304`）。

---

## 5. 性能诊断

> 完整 15 条见 `.diagnosis/performance.md`；微基准原始输出与复现脚本见该报告 §13。

### 5.1 启动快，但测的不是产品路径

README 的 **"Cold start under 100ms"** 对 `--version` 成立且远优于声明（6.4–6.9ms）。但 `--version` 不加载 spec；真实 app 命令的耗时由 spec 体积决定，且**每条命令重复支付**：

| 场景 | 实测 p50 | 结论 |
| --- | ---: | --- |
| `ob --version` / `--help` / 无参 | 6.4–6.9ms | ✅ 远优于 100ms |
| `ob list`（0 app，空 HOME） | 8.99ms | ✅ |
| `ob list`（3 app，含 4.1MB spec） | **166.9ms** | ❌ 为打印一张表解析了所有 spec |
| `ob run demo --help`（0.07MB spec） | ~10ms | ✅ |
| `ob run mb1 --help`（0.96MB spec） | **42ms** | ⚠️ 已接近声明上限 |
| `ob run mb5 --help`（4.09MB spec） | **126–136ms** | ❌ 破 100ms |
| `ob run`（14.42MB spec） | **490–520ms/命令** | ❌ 线性/超线性恶化 |
| shell 补全（4.1MB spec） | **134ms/Tab** | ❌ 每次新进程重解析 |
| 5MB 响应命令 | 90ms / **峰值 RSS 62MB**（基线 23.2MB，7.8×） | ❌ 全量 `io.ReadAll` + 重序列化 |

### 5.2 成本结构（微基准）

| 操作 | 耗时 | 分配 |
| --- | ---: | ---: |
| Parse spec 0.07MB | 2.30ms | 2.0MB / 24k allocs |
| Parse spec 0.96MB | 33.6ms | 27.8MB / 334k allocs |
| Parse spec 4.09MB | **119.4ms** | **109.3MB / 1.24M allocs** |
| BuildCommandTree 4.09MB | 2.49ms | 2.6MB / 18.5k allocs |
| LoadParsedSpec（现有"缓存"设计）4.09MB | **118.1ms** | 101.6MB | ← 与重解析等价，说明方案本身无效 |
| Keyring GetCredential | 189µs | — |
| MCP BuildMCPTools（1000 op） | 0.51ms | 非瓶颈 |
| Predicate search（1000 工具） | 20.7µs | 非瓶颈 |

结论：**spec 解析是唯一主成本**（约 28–34MB/s 吞吐），而下游派生产物（命令树/工具注册）很便宜。因此缓存策略应缓存"命令树索引"而非 AST。

### 5.3 其他性能问题

- **响应全量入内存**：`io.ReadAll`（`cli/handler.go:142`、`mcp/handler.go:118`、`progressive.go:489`）+ JSON→any→YAML 重序列化（`handler.go:496-529`）；大响应有 OOM 风险，MCP 侧还会把整个 body 作为单条 `TextContent` 放入上下文。
- **`ob list` / `ob info` 解析 spec**：install 时已经算过 `SpecInfo`/`OperationCount` 并存进配置，运行时却重新解析（`install.go:898-925`）。
- **MCP progressive Search 结果无上限**（`progressive.go:291-312`）：1000 工具时一次查询可能吐出数 MB 文本。
- **语义映射每 operation 重复编译正则/建 map**（`semantic/verb.go:224,289`、`resource.go:295,352`）：`Singularize` 1750ns/4 allocs、`Pluralize` 2028ns；`BuildCommandTree` 对 4MB spec 仅 2.49ms，端到端占比 <2%，属"顺手优化"。
- **`Parser.GetCachedSpec` 无锁写 `AccessedAt`**（`spec/parser.go:633-648`）：当前 CLI 单线程不触发，但 MCP 长驻进程若并发 tool call 即数据竞争；`-race` 测试未覆盖并发调用。
- **watcher 死代码**：`spec_watcher.go` 778 行 + `fsnotify` 依赖生产零调用，其"生命周期/泄漏"风险实际不存在，但维护成本与依赖体积存在。

### 5.4 优化路线（按 ROI）

| 优先级 | 动作 | 预期收益 | 成本 |
| :---: | --- | --- | :---: |
| P0 | 修复认证（D-01）后，重新设计持久化缓存：缓存**派生命令树索引**（gob，含 `(path,size,mtime)` + ParserVersion 失效键），实现 `LoadSpecWithPersistentCache` 真实语义 | 4.1MB spec 单命令 126ms → 目标 20–40ms；Tab 134ms → 10–25ms | M |
| P1 | `ob list`/`ob info` 不解析 spec，改读配置字段 | 3 app 167ms → ~10ms（16×） | S |
| P1 | 远端 spec 运行时读磁盘缓存 + 条件请求 + stale 回退（代码已在 `spec_cache.go` 实现，只是没人调用） | 公网场景省 50–500ms；离线可用；消除每命令重下 | M |
| P1 | 响应流式化/上限：小 body 原路径，大 body 直接 `io.Copy` 或落盘并提示；MCP 截断 | 5MB 响应峰值内存 -60% | M |
| P2 | 统一 `http.Client`（超时来自 `Profile.Timeout`）+ TLS/Retry 接线 | 消除永久挂起；TLS 可用 | S/M |
| P3 | 语义包微优化（正则/map 提为包级、有序 slice 防 map 迭代不确定性） | `BuildCommandTree` -30~60%，端到端 ≤1.5% | S |
| P3 | 删除或接线 watcher/缓存死代码（约 4.5k 行） | 可维护性/依赖体积 | S |

---

## 6. 代码风格与可维护性

> 完整 34 条见 `.diagnosis/style.md`；所有工具原始输出见该报告附录 A。

### 6.1 评分与结论

| 维度 | 结果 | 结论 |
| --- | --- | --- |
| 格式/导入 | `gofmt -l`=0、`goimports -l`=0；`gofumpt -l`=20 文件 | **A−**（未启用 gofumpt） |
| 命名 | receiver 100% 一致；42 个导出 `Get*`；`baseUrlInput`/`GetFd` | **B−** |
| 注释 | 导出符号注释 94.9%；24 个导出符号缺注释；package 注释在 7 个 config 文件整段复制 | **B** |
| 复杂度 | gocyclo>15 共 5 个（最高 25）；gocognit>15 共 8 个（最高 35）；最复杂两个函数用 `//nolint` 豁免 | **C+** |
| 重复 | dupl(70) 9 组，7 组测试、2 组非测试 | **C+** |
| 错误处理 | 0 导出 sentinel；`%w` 包装率 54%（132/244）；`%v` 丢链 3 处起；61 处裸 `return err`；分类靠字符串 | **D+** |
| 魔法值/stringly | `mnd` 被禁用；auth/format/scheme/权限位/超时散落 | **C** |
| 死代码/导出面 | 733 导出符号；195 个导出函数仅本包使用；约 4.5k 行死代码 | **C** |
| 依赖卫生 | `ghodss/yaml` 已归档并拖入 `yaml.v2`，与 `yaml.v3` 双栈；`vulcand/predicate` 单点重依赖；`go 1.25.5` patch 写法与 CI 漂移 | **C+** |
| AGENTS.md | §4.1 错误包装、§4.2 依赖接口化、§3.2 property test、§3.3 build tag 均违反 | **D+** |

### 6.2 lint "宪法"失效（D-14）

三处版本漂移：

| 位置 | 值 | 问题 |
| --- | --- | --- |
| 本机 PATH | `golangci-lint v1.64.8`（built go1.26.1） | v1 无法解析 v2 格式配置：`can't load config: sort-results should be 'true' to use sort-order` |
| 备用 v2.8.0（built go1.25.5） | 运行 panic：`file requires newer Go version` / `export data version 5 > max 2` | 二进制的 x/tools 读不了 Go 1.27.3 产出的 export data |
| `Makefile:30` + `ci.yml:105-109` | 都取 `latest` | 版本不可复现；CI 与本地 Go 版本组合未固定 |

直接后果：**AGENTS.md §5.1 要求提交前运行 `make fmt-fix && make lint-fix`，但 `make lint` 在本机根本无法运行**；仓库自带的 4 条 gocyclo 排除规则（`.golangci.yml:95-116`）所豁免的函数经复核复杂度仅 5–7，说明排除规则已陈旧，lint 配置与代码现状脱节。

**改进（S）**：`.golangci.yml` 保留 v2 格式；Makefile/CI 固定到同一个具体版本（如 `v2.8.0`），并在 README 记录"工具版本需与 Go 版本匹配"；启用 `nolintlint`（当前 14 条 `//nolint` 中至少 1 条无理由）、`goconst`、`usestdlibvars`、`errname`，恢复 `gosec`（baseline 过滤）。

### 6.3 错误处理：与 AGENTS.md 冲突最严重的一项

- **0 个导出 sentinel error**——AGENTS.md §4.1 明确要求"Define strict sentinel errors (e.g., `ErrConfigNotFound`) for state checks in the `var` block"，实际只有 1 个未导出的 `errAppConfigNotSet`，其他全是 `fmt.Errorf`/自定义 struct。
- 错误分类依赖字符串匹配（`cli/errors.go:70-86` 五处 `strings.Contains`）。
- `%w` 包装率 54%（244 处 `fmt.Errorf` 中 132 处带 `%w`），61 处直接 `return err`；`mcp/progressive.go:317-491` 最密集。
- `PrintedError.Error()` 返回 `""`（`cli/errors.go:16-36`）——用空字符串表达"已打印"，使错误在 `slog`/遥测中不可见。
- 46 处 `_ =` 显式丢弃错误（安装、shim、缓存、watcher 路径）。

### 6.4 复杂度热点（style 复核 + gocyclo/gocognit 实测）

| 函数 | 文件:行 | 指标 |
| --- | --- | ---: |
| `parseRequestParamsAuto` | `pkg/cli/args.go:314` | gocognit **35**（`//nolint` 豁免） |
| `parseValueWithContext` | `pkg/cli/args.go:524` | gocyclo **25** / gocognit 26（`//nolint` 豁免） |
| `looksLikeCredential` | `pkg/credential/security.go:373` | gocyclo 25 / gocognit 25 |
| `(Model).renderStepContent` | `pkg/tui/install/model.go:354` | gocyclo 19 |
| `(Model).handleStepUpdate` | `pkg/tui/install/model.go:285` | gocyclo 19（`//nolint:funlen`） |
| `(*Handler).showParameterValidationError` | `pkg/cli/handler.go:844` | gocognit 25 |
| `collectBodyPropertyFlags` | `pkg/completion/completion.go:218` | gocognit 19 / 嵌套 5 |

`.golangci.yml` 的阈值（gocyclo/gocognit=25、funlen 50/40、dupl=70）明显宽于代码现状，仍有 6 个函数实际超过 funlen 却未被抑制——说明 lint 从未在当前基线跑过。

### 6.5 依赖卫生

- `github.com/ghodss/yaml v1.0.0`（2020 年起归档）仍在 `go.mod` 直接依赖，并把 `gopkg.in/yaml.v2` 作为间接依赖拖入，与 `gopkg.in/yaml.v3` 双栈并存。
- `go.mod` 写 `go 1.25.5`（patch 版本），CI 用 `go-version: "1.25"`（解析为最新 1.25.x），两者语义不完全一致。
- 直接依赖 14 个，其中 `charmbracelet/{bubbletea,bubbles,lipgloss}` 仅服务 1,347 行的 TUI；`vulcand/predicate` 仅服务 MCP 搜索 DSL；属于"单点重依赖"，需评估收益。
- `make update-deps` 直接 `go get -u ./...`，无锁定/回归保护。

---

## 7. 测试与工程流程

> 完整 42 条见 `.diagnosis/process.md`。

### 7.1 覆盖率：55.3%，且"偏科"

| 包 | 语句 | -short 覆盖 | 0 覆盖函数数 | 备注 |
| --- | ---: | ---: | ---: | --- |
| `pkg/request` | 296 | 85.1% | 3 | 最好 |
| `pkg/semantic` | 325 | 85.5% | 3 | 好 |
| `pkg/mcp` | 624 | 78.0% | 14 | `server_factory`/`shared` 无测试 |
| `pkg/spec` | 369 | 78.0% | 6 | **parser/formatter 无 property test** |
| `pkg/config` | 1,785 | 73.8% | 21 | 体积最大 |
| `pkg/completion` | 181 | 72.9% | 2 | |
| `pkg/codegen` | 307 | 70.7% | 14 | 4 个生成器无同名测试 |
| `pkg/credential` | 397 | 58.4%（full 71.8%） | 11 | keyring 测试在 `-short`/CI 双跳过 |
| `pkg/cli` | 1,191 | **31.5%** | 71/116 | 用户面最大包之一 |
| `cmd/ob` | 410 | **19.0%** | 39/50 | CLI 胶水层几乎未测 |
| `pkg/router` | 86 | **0.0%** | 12/12 | 无测试文件 |
| `pkg/tui/install` | 603 | **0.0%** | 72/72 | 1,347 行向导，无测试 |
| `internal/integration` | 0 | — | — | 无源文件的孤儿测试包 |

关键盲区：安全掩码函数 `isSensitiveHeader`/`maskValue` **0%**；路由核心 `pkg/router` **0%**；交互式安装 `pkg/tui/install` **0%**。
`-short` 掩盖了下滑：`pkg/credential` 从 full 的 71.8% 掉到 58.4%，因为 keyring 测试同时被 `testing.Short()` 和 `isCI()`（`CI`/`GITHUB_ACTIONS` 恒真）跳过——**"Secure Credentials"卖点在 CI 中从未被执行验证**。

### 7.2 测试质量画像

- 535 个 Test 函数、17,527 行测试代码，但 `t.Parallel` **0 处**、`t.Skip` **30 处**、`time.Sleep` **13 处**（watcher 测试 flaky 风险）、**0 个 Benchmark / Fuzz**。
- property test 7 个，全部 `if testing.Short() { t.Skip }`，而 `make test-short` 与 CI 都带 `-short` 语义 → **property test 在门禁中全部空转**；`pkg/spec`（AGENTS.md §3.2 明确点名的 parser/formatter）**没有 property test**。
- 部分 property test 的错误分支 `return true` 直接跳过断言（`config_property_test.go:45-71`、`export_property_test.go:50-60`）。
- 单测反向依赖 integration testdata（`pkg/spec/parser_test.go:990,1421`）。

### 7.3 AGENTS.md 合规：集成测试隔离失效（D-13）

- 命名含 integration 的测试文件共 6 个：`pkg/{cli,config,credential,request}/*_integration_test.go` + `internal/integration/{integration,real_api}_test.go`。
- **仅 `pkg/credential/keyring_integration_test.go` 带 `//go:build integration`**；其余 5 个在默认构建/`-short`/CI 中作为单元测试运行（`internal/integration` 在 `-short` 下实测运行 1.6–2.4s）。
- 更严重：**带 tag 的组合直接编译失败**——`pkg/credential/keyring_test.go:219` 与 `keyring_integration_test.go:219` 都有 `TestGetPlatformInfo`（另有 `TestWithPassBackend` 重名）：

```text
$ go vet -tags=integration ./...
vet: pkg/credential/keyring_test.go:219:6: TestGetPlatformInfo redeclared in this block
```

即 AGENTS.md §5.1 规定的 `go test -v -tags=integration ./...` **完全不可用**。
- 测试文件与源文件配对违规：`pkg/router`、`pkg/tui/install`、`mcp/server_factory.go`、`mcp/shared.go` 无测试；`internal/integration` 是没有源文件的孤儿包。

### 7.4 CI 与工程流程

**做得好的**：3 OS × Go 1.25 矩阵、`make test-coverage` 带 `-race`、DCO job、GoReleaser v2、docs 自动部署、actions 权限最小化。

**缺口**：

| 问题 | 证据 | 影响 |
| --- | --- | --- |
| lint 版本 `latest` | `ci.yml:105-109`、`Makefile:30` | CI 可因新版本发布突然变红/变绿；本地无法复现 |
| 无覆盖率门禁 | `ci.yml:84-91` 上传 codecov 但 `fail_ci_if_error: false`，无 `codecov.yml` | 覆盖率从 56% 继续下滑无人拦截 |
| integration tag 测试从不运行 | `ci.yml:82` 只有 `make test-coverage`（无 `-tags`） | §7.3 的失效永久隐形 |
| 无安全扫描 | 无 `govulncheck`/CodeQL/Dependabot/license 检查 | 依赖 CVE 与漏洞无感知 |
| 无 `go vet` 步骤 | `ci.yml` 全文 | 依赖 `make lint`（已失效） |
| Node 20 已 EOL（docs workflow） | `deploy-docs.yml:36` | 未来构建失败风险 |
| coverage 仅 ubuntu 上传 | `ci.yml:84` | macOS/Windows 覆盖率不可见 |
| DCO 检查范围有限 | `ci.yml:15-49` 只查增量；历史 37/42 缺失 | AGENTS.md §5.2 形同虚设 |

**Makefile**：`make install` 依赖 `$GOPATH` 环境变量（未导出时实测失败，应用 `go env GOPATH`/`go install`）；`GOLANGCI_LINT_VERSION=latest`；`fmt-fix` 用 `gofmt -r 'interface{} -> any'` 等全树重写规则（语义风险）；`docs` 目标仍用已淘汰的 `godoc`；`clean` 不清理 docs dist / node_modules；无 `test-integration`/`vet`/`check` 目标，与 AGENTS.md 检查清单不对应。

### 7.5 文档与实现漂移（抽查）

| 文档宣称 | 实际 | 证据 |
| --- | --- | --- |
| `ob version` 命令 | 不存在（仅 `--version`） | `README.md`、`QUICKSTART.md:28`、`docs/reference/cli.md:18` |
| "Table output (default)" | 默认 YAML，且 `--json/--yaml/--output` 无效 | `pkg/cli/handler.go:358-369,496-529` |
| "Zero Code Generation" | 存在 `pkg/codegen` 与 `--generate`（4 语言） | `README.md:20`、`pkg/cli/handler.go:171-210` |
| 安装向导配置 TLS/敏感信息掩码 | TLS 零接线；掩码不覆盖 URL | `config.go:210-230`、`pkg/codegen/*.go` |
| profile 切换 `--profile prod` | 静默无效 | §2.3 |
| `ob <app> ...` 直连 | 不可达 | §2.4 |
| MCP `require_confirm`/限流 | 未实现 | §2.5 |

此外 `docs/guide/quick-start.md` 的 MCP 配置示例 `"args": ["run", "myapi", "--mcp"]` 实测可用（✅），这一点没有问题。

### 7.6 发布与仓库卫生

- `goreleaser check` 通过；三平台构建配置正确；但**无 checksum 签名、无 SBOM、无 macOS 公证**；`before.hooks: go mod tidy` 非密闭（会改工作区）。
- `.gitignore` 覆盖 `bin/`、`coverage.*`、`node_modules/`、`docs/.vitepress/dist`；`examples/todo-service/.venv`、`__pycache__` 未入库（✅ 无污染）。
- 仓库根存在 `coverage.out`/`coverage.html`（1 月 25 日旧产物，已忽略但可能误导；实测 56.6%，与当前 55.3% 不一致）。
- `package.json` 的 `main: index.js` 不存在、`keywords` 空、无 `engines`、`@types/node@25` 与 docs workflow 的 Node 20 不一致。
- 缺 `SECURITY.md`、`CODEOWNERS`、`.editorconfig`；`docs/.vitepress/dist` 已正则忽略（✅）。
- 可观测性：生产代码 **0 处日志框架**（无 `log`/`slog`），无 `--verbose`/debug 开关，User-Agent 硬编码 `OpenBridge/1.0` 不含真实版本——线上排障只能靠 stderr 文本。

---

## 8. 根因分析：为什么会变成这样

按影响面排序，5 条系统性根因：

1. **质量门禁全部失效，导致"重构带走功能"无人发现。**
   最近一次 "comprehensive code simplification"（`9546058`，33 文件 +2,794/−1,687）直接引入 P0 认证回归；覆盖率 56%、lint 不可运行、integration 测试不可运行、CI 无门禁——四道防线同时缺位。**这不是某个人的失误，是流程没有反馈回路。**

2. **"配置面 > 执行面"：配置模型先于实现生长。**
   `Profile` 12 个关注点、`SafetyConfig` 9 个字段、`TLS/Retry/Timeout/SpecFetchAuth`、`HybridSearch`……配置/文档/TUI 向导都已存在，但约一半字段运行时零接线。功能被"声明"而非"实现"，测试也只测配置结构而不测行为。

3. **组合根与依赖注入缺失，导致"测试隔离"不可能。**
   `init()` 中打开 keyring/创建目录 → 所有测试共享真实环境 → 集成测试只能靠 `isCI()`/`t.Skip` 跳过 → 关键路径（凭据、路由、TUI、CLI 输出）测试覆盖为 0 → 回归无法被发现。这是一个自我强化的恶性循环。

4. **错误模型 stringly-typed，导致"静默失败"成为默认行为。**
   无 sentinel、`strings.Contains` 分类、`return nil // skip auth`、46 处 `_ =`、`PrintedError` 空消息——错误无法被程序区分，只能被人眼从文本里猜。D-01/D-02/D-08 都是同一根因的不同表现。

5. **文档与实现没有同步机制。**
   README/docs/QUICKSTART/安装向导由不同时间点写成，没有 "docs-as-tests"（命令可执行、flag 可验证）或 CI 文档检查；`ob version`、输出格式、Zero Code-Gen、TLS 承诺因此长期漂移。

---

## 9. 行动路线图

> 优先级以"用户可感知的正确性/安全性" > "可持续演进的基础设施" > "性能" > "整洁性" 排序。
> 工作量口径：S ≤ 1 天，M = 2–5 天，L = 1–2 周。

### 批次 0 —— 立即热修（1–3 天，全部 S）

| # | 动作 | 关联 | 验收 |
| :---: | --- | --- | --- |
| A0-1 | **修 CLI 认证**：`cli/handler.go:133` 传真实 `appName`；`InjectAuth` 区分 not-found 与后端错误；补 CLI→builder e2e | D-01 | 本地 mock server 断言收到 `Authorization`/`X-API-Key` |
| A0-2 | **堵凭据外泄**：`redactURL`（网络错误 + 4 生成器）+ `redactConfig`（`ob info -o json/yaml`） | D-02/03/04 | 新增 3 个测试；grep 无未掩码 `req.URL.String()` 输出 |
| A0-3 | **修输出格式**：`--json/--yaml/--output` 正确传递；未知 flag 报错；默认值对齐文档 | D-07 | `--json` 输出合法 JSON；未知 flag 非零退出 |
| A0-4 | **修 `ob <app>` 路由**：`rootCmd.Args = cobra.ArbitraryArgs`；删 `handleOBCommand` | D-11 | `ob <app> --help` 可用 |
| A0-5 | **修集成测试**：删除 2 组重名测试函数；5 个文件补 `//go:build integration`；CI 加 `go vet ./...` + `go test -tags=integration ./...` 两步 | D-13 | `go vet -tags=integration ./...` 通过 |
| A0-6 | **HTTP 超时**：三处 client 设 `Timeout`（先用固定 30s，后续接 `Profile.Timeout`） | D-10 | 挂起服务端测试 5s 内返回超时错误 |
| A0-7 | **keyring 回退**：默认禁用 FileBackend / 固定口令；`Backend()` 返回真实后端 | D-12 | 无后端时明确报错而非静默降级 |
| A0-8 | **文档修正**：`ob version`→`ob --version`；输出默认/D-07 说明；"Zero Code Generation"措辞 | §7.5 | 文档命令逐条可执行 |

### 批次 1 —— 第一个迭代（1–2 周，S/M）

| # | 动作 | 关联 |
| :---: | --- | --- |
| A1-1 | **实现真实持久化缓存**：缓存命令树索引（gob）+ 复合失效键 + 原子写；`LoadSpecWithPersistentCache` 落地；`ob list`/`info` 改读配置字段；远端 spec 走磁盘缓存 + 条件请求 + stale 回退 | D-05/06/15 |
| A1-2 | **profile 应用层**：headers/query/auth/timeout/TLS/retry 统一装配，CLI 与 MCP 共用；`--profile` 校验 | D-08 |
| A1-3 | **MCP 安全矩阵**：实现 `require_confirm`/限流，或从 schema/文档中移除；补配置↔行为测试 | D-09 |
| A1-4 | **撤销 `init()` 组合根**：`internal/app.Deps` + `app.Run(ctx, argv)`；合并两套 MCP 启动；main.go 收敛到 ~50 行 | §4.1 |
| A1-5 | **错误模型收敛**：导出 sentinel（`ErrAppNotFound`/`ErrCredentialNotFound`/...）；`errors.Is/As` 分类替代字符串嗅探；`PrintedError` 返回真实文本；`signal.NotifyContext` | §4.3 |
| A1-6 | **CI 门禁**：lint 版本 pin（Makefile/CI 同一 v2.x）、`govulncheck`、恢复 `gosec`（baseline）、codecov 阈值（floor 55%）、Dependabot | D-14/16 |
| A1-7 | **响应流式化**：大 body 直接 `io.Copy`/落盘；MCP 结果截断 | §5.3 |

### 批次 2 —— 结构性治理（3–6 周，M/L）

- **拆 `pkg/config`**：领域模型留 `config`；`internal/install`（shim/TUI prompt/安装流程）；`internal/specstore`（cache/watcher 或删除）；导出面 733 → ~250。
- **拆 `cmd/ob`**：`internal/commands`（install/list/info/uninstall/run/completion）+ `internal/render`；`pkg/tui/install` 拆 step 文件并用 `teatest` 覆盖。
- **测试补强**：`pkg/router` 12 函数表驱动、`pkg/tui` 状态机纯函数化、`cmd/ob` 输出函数 buffer 测试、`pkg/spec` parser/formatter property test、安全掩码函数测试；目标覆盖率 ≥70%、`t.Parallel` 渐进引入、删除 `time.Sleep` 改 `require.Eventually`。
- **可观测性**：`internal/log`（slog）+ `--verbose` + 请求耗时/状态 + 脱敏审计；UA 带真实版本。
- **发布供应链**：cosign 签名 + SBOM + macOS 公证；actions pin 到 SHA；`goreleaser before.hooks` 改 `go mod tidy -diff` 校验。
- **文档即测试**：为 README/docs 的命令、flag 建 e2e 冒烟（临时 HOME 跑真实二进制），纳入 CI。
- **删除死代码**：`spec_watcher.go`（778 行）+ `fsnotify`、`export/profile` 未接线的 API、`EnvironmentCredentialSource`、`Save/LoadParsedSpec` 旧方案——每删一项同步删测试，减少维护面。

### 预期效果

| 场景 | 现状 | 批次 0 后 | 批次 1 后 |
| --- | ---: | ---: | ---: |
| CLI 认证 | 静默失效 | ✅ 可用 | ✅ 可用 |
| 凭据外泄 | 3 条路径 | ✅ 已堵 | ✅ 已堵 |
| `--json` / `--profile` | 静默无效 | ✅ 可用 | ✅ 可用 |
| 4.1MB spec 单命令 | 119–134ms | 不变 | ~20–40ms |
| `ob list`（3 app） | 167ms | 不变 | ~10ms |
| 5MB 响应峰值 RSS | 62MB | 不变 | ~24MB |
| 集成测试 | 编译失败 | ✅ 可运行 | ✅ 进 CI 门禁 |
| 覆盖率 | 55.3% | 55.3% | ≥70% 目标 |

---

## 10. 如果只能做三件事

1. **A0-1 + A0-2（1–3 天）：先修认证、再堵凭据外泄。**
   这是唯一一条"用户立刻能感知 + 安全影响真实"的组合：认证失效让产品核心卖点归零，凭据外泄让用户把 key 贴进日志/LLM 上下文。两项都是 S 级改动，且顺带把 `InjectAuth` 的错误语义理顺——修功能的同时收紧最烂的架构接缝。

2. **A0-5 + A1-6（3–5 天）：让"规定的检查"真的能跑起来。**
   集成测试编译失败 + lint 无法运行 + CI 无门禁 = 当前所有问题的放大器。修好这三道门，本报告里一半的回归（D-01、D-13、D-14、D-16）以后不会再悄悄发生。**没有这一步，其余修复会在下一次重构中再次失效。**

3. **A1-1（1 周）：把"解析一次、长期复用"从口号变成实现。**
   这是性能与产品体验的最大单项收益（4.1MB spec 命令 130ms → 20–40ms，`list` 16×），也是 ARCH 报告里"承诺 > 实现"的最典型代表；实现时必须缓存派生索引而非 AST（现有 `SaveParsedSpec` 方案已被实测证伪）。

> 若只能再做一件：**A1-4（撤销 `init()` 组合根 + 合并 MCP 启动）**——这是"半年后这个仓库还能改得动"的分水岭；不做，后续每加一个命令/接口都会复制一份全局装配逻辑。

---

## 附录 A：四份领域详报索引

| 报告 | 内容 | 关键章节 |
| --- | --- | --- |
| `.diagnosis/architecture.md` | 53 条架构/设计发现 | §0.2 Top12、F-01（认证）、F-02（init）、F-05（config 上帝包）、F-06（双组合根）、F-22（错误模型）、F-38/F-39（缓存/配置矩阵）、§12 行动清单 |
| `.diagnosis/performance.md` | 15 条性能发现 + 微基准 | §1 Top、F1（缓存空壳）、F3（list）、F4（远端 spec）、F5（补全）、F6（响应内存）、§12 优化路线、§13 基准原始输出 |
| `.diagnosis/style.md` | 34 条风格/可维护性发现 | §0.2 评分卡、§1（lint 不可用）、§3 复杂度 Top15、§7 错误处理、§10 依赖、§12 建议 lint 规则 |
| `.diagnosis/process.md` | 42 条测试/流程/安全发现 | §0.2 Top5、§2 覆盖率、§4 AGENTS 合规、§5 CI、§6 Makefile、§7 安全（7.3–7.12）、§11–12 行动清单 |

## 附录 B：复现命令（核心）

```bash
cd /Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge

# 基线
go build ./... && go vet ./... && go test -short ./...
go test -race -short ./...

# 覆盖率
go test -short -count=1 -coverprofile=/tmp/cover.out -covermode=atomic ./...
go tool cover -func=/tmp/cover.out | tail -1

# 集成测试（当前失败）
go vet -tags=integration ./...

# lint（当前不可用）
golangci-lint run ./...            # v1：配置不兼容
golangci-lint-v2 run ./...         # v2.8.0：与 Go 1.27 export data 冲突

# 功能 e2e（隔离 HOME，避免污染真实配置）
export HOME=/tmp/ob-diag-home OPENBRIDGE_CONFIG_DIR=/tmp/ob-diag-cfg
go build -o /tmp/ob ./cmd/ob
/tmp/ob install petapi --spec /tmp/spec.yaml --auth none --shim=false
/tmp/ob run petapi pets list --json     # BUG：输出 YAML
/tmp/ob run petapi pets list --profile prod  # BUG：仍用 default
/tmp/ob petapi pets list                # BUG：unknown command "petapi"

# DCO
git log --format='%h %s' | while read h s; do git log -1 --format='%B' $h | grep -q '^Signed-off-by: ' || echo "MISSING $h"; done | wc -l
```

## 附录 C：证据分布与未验证事项

**证据分布**：本报告 60 条去重发现中，✅ 端到端复现 12 条、🔍 源码路径确认 21 条、📊 团队实测 15 条、⚠️ 静态推断 12 条（已在正文逐条标注）。

**未验证/局限**（如实声明）：

1. `golangci-lint` 真实结果无法获得（工具链三重不兼容，见 §6.2）；复杂度/重复等领域用 gocyclo/gocognit/dupl 等效替代，已标注。
2. 未在 Linux/Windows 实机复现；keyring FileBackend 行为基于 99designs/keyring 源码与测试静态推断。
3. 真实世界大型 spec（Stripe/GitHub）未测试，性能数据基于合成 spec（0.07–14.4MB）。
4. `govulncheck` 未运行（本机未安装；`go install` 受网络/工具链限制），依赖 CVE 未评估。
5. 未验证仓库外是否有消费者导入 `pkg/*`，"过度导出"为仓库内视角。
6. `maintidx`/`staticcheck`/`golangci-lint` 因 Go 1.27.3 的 export-data 版本问题无法运行，相关结论均来自替代工具或人工阅读。

---

> **报告完。** 本次诊断未修改仓库任何源码/配置；新增文件仅有本报告与 `.diagnosis/` 下 4 份领域详报。若需要，我可以按 §9 批次 0 直接发起修复 PR（建议从 A0-1/A0-2 开始）。
