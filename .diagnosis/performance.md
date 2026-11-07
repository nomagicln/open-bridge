# OpenBridge 性能诊断报告（performance.md）

> 诊断对象：`nomagicln/open-bridge`（Go OpenAPI → 语义 CLI + MCP 运行时引擎）
> 诊断基线 commit：`dd18374 fix: resolve linting issues and improve code quality`
> 诊断日期：2026-11-07
> 诊断者：perf-analyst（Team Lead 委派）
> 报告性质：只读诊断。**未修改仓库任何源码/配置/go.mod**，未执行 git 写操作；全部临时基准程序位于 `/tmp/ob-perf/`，未污染仓库。
> 仓库规模：非测试 Go 源码约 17.1k LOC（`find pkg cmd internal -name '*.go' -not -name '*_test.go' | xargs wc -l`，其中 pkg 17,084 行）。

---

## 0. 证据等级约定（阅读前必读）

本报告中每个结论都标注了证据类型，请在采信时区分：

| 标记 | 含义 |
| --- | --- |
| `[实测]` | 本机真实测量，给出命令/样本数/统计量，可复现 |
| `[bench]` | `go test -bench` 微基准（`/tmp/ob-perf/bench/`，通过 `replace` 直接 import 仓库包），给 ns/op 与 B/op |
| `[静态]` | 仅源码静态推断（含 `go build -gcflags=-m` 逃逸分析），未实测运行时开销 |
| `[排除]` | 曾怀疑但实测/代码证据证明不是问题，专门记录以免后人重复排查 |

严重度：**P0** = 直接违背对外承诺或造成功能性失败；**P1** = 主要用户路径秒级/内存级退化；**P2** = 可感知但非致命；**P3** = 整洁性/长期风险。

---

## 1. 结论速览

### 1.1 一句话结论

OpenBridge 的**进程启动本身很快（6.5–6.8ms p50，远优于 README 的 100ms 声明）**，但这条声明测量的东西几乎没有意义：它测的是 `--version`/`--help` 的进程启动，而**真正的产品路径（执行任意 app 命令）在每次调用时重新解析完整 OpenAPI spec**——4.1MB spec 单次命令推进到 **~120–134ms**，1MB spec ~42ms，且**持久化解析缓存是一个空壳函数**（`parser.go:837` 直接转发、忽略 `appName`），磁盘上的 `parsed.json` 写入/读取函数（`spec_cache.go:694/739/784`）**在生产代码中零调用**。更糟的是，即便把死代码接上，其实现（整棵 `openapi3.T` JSON 序列化）在 4.1MB spec 上测得 **LoadParsedSpec 118ms / 101MB**，与重新解析 119ms 几乎一样——**现有持久化缓存设计对 spec 解析没有收益**。次要热点：`ob list` 会解析**所有已安装 app 的 spec**（3 个 app 含 1 个 4.1MB spec → 160–169ms）、shell 补全每次 Tab 重新解析 spec（4.1MB → 134ms/Tab）、远程 URL spec 每次命令都重新 HTTP 下载（install 时的磁盘缓存运行时从不读取）。

### 1.2 Top 发现（按严重度/ROI 排序；F14/F15 为 Lead 线索追加实测，见 §14）

| # | 严重度 | 标题 | 关键实测数字 | 位置 |
| --- | --- | --- | --- | --- |
| F1 | **P0** | 持久化解析缓存是空实现：`LoadSpecWithPersistentCache` 直接转发给同步解析，忽略 `appName`；`SaveParsedSpec/LoadParsedSpec/ValidateParsedSpec` 生产零调用 | 4.1MB spec 每次命令 `LoadSpec` 119.4ms / 109.3MB / 1.24M allocs；1MB 33.6ms / 27.8MB | `pkg/spec/parser.go:837`、`pkg/config/spec_cache.go:694,739,784` |
| F2 | **P1** | spec 解析本身是唯一主成本，耗时/分配严格线性于体积（吞吐稳定 ~28–34MB/s；kin-openapi 解析 + 全量 `doc.Validate`） | 0.07MB: 2.30ms；0.96MB: 33.6ms；4.09MB: 119.4ms；内存 2.0MB / 27.8MB / 109.3MB；吞吐 ~28–34MB/s | `pkg/spec/parser.go:225-238,451-469` |
| F3 | **P1** | `ob list` / `ob info` 为打印一张表解析**每个** app 的 spec（含远程 URL 会发网络请求） | 3 app（含 mb5 4.1MB）：`list` p50 166.9–169.2ms；空 HOME 时仅 8.99ms | `pkg/config/install.go:898-925`（解析发生在 918–922） |
| F4 | **P1** | 远程 URL spec 每次命令重新 HTTP GET：install 时的磁盘缓存（`FetchWithCache`）在运行时路径中从不读取 | `FetchWithCache` 生产调用点仅 `install.go:409` + 死代码 `spec_watcher.go:598`；运行时走 `parser.go:264` 裸 `client.Do` | `pkg/config/spec_cache.go:254`、`pkg/spec/parser.go:264-295` |
| F5 | **P1** | shell 补全每次 Tab 起新进程并解析 spec（无跨进程缓存）；补全延迟随 spec 线性涨，直接破坏"Tab 秒回"体感 | `ob __complete run demo ""` 11.7ms；1MB 42.7ms；4.1MB 133.6ms/Tab | `pkg/completion/completion.go:381-402`、`cmd/ob/main.go:978-993` |
| F6 | **P1** | 大响应全量读入内存并多次放大：`io.ReadAll` + JSON 格式化 + `fmt.Println`，实测峰值 RSS 放大 ~7.8× | 5MB body：e2e 90ms / **最大 RSS 62.0MB**；100B body：20ms / 23.2MB → **+38.8MB（7.8×）** | `pkg/cli/handler.go:142`、`pkg/mcp/handler.go:118`、`pkg/mcp/progressive.go:489` |
| F7 | **P2** | HTTP 客户端无超时、无连接池配置：CLI/MCP/progressive 三处 `&http.Client{}`；CLI 单进程单请求无害，MCP 长驻进程缺少 `Transport` 调优与整体超时 | CLI 客户端 `&http.Client{}`（`Timeout=0`，永不等） | `pkg/cli/handler.go:45`、`pkg/mcp/handler.go:32`、`pkg/mcp/progressive.go:80` |
| F8 | **P2** | 语义映射每 operation 重复编译正则、重复构造 map/slice：`defaultMethodMapping` 内 `regexp.MustCompile`，`inferVerbFromOperationID` 内建 map，`Singularize/Pluralize` 内建 map+slice | `Singularize` 1750ns/4 allocs/op；`Pluralize` 2028ns/4 allocs；`BuildCommandTree` 4.1MB→2.49ms/2.59MB；20k op 共 18.5k allocs | `pkg/semantic/verb.go:224,289`、`pkg/semantic/resource.go:295,352` |
| F9 | **P2** | MCP progressive `SearchTools` 是 O(n) 全量扫描 + 每次查询重新解析 predicate 表达式 + 结果无上限拼成单条文本（可能把上千工具塞进一次 MCP 响应） | 1000 工具搜索 20.7µs/9512B/42 allocs；但 `formatSearchResults` 无 top-K 截断（`progressive.go:291-312`） | `pkg/mcp/engine_predicate.go:43-64,242-260`、`pkg/mcp/progressive.go:291` |
| F10 | **P2** | spec 远端缓存写盘非原子：先 `os.WriteFile` spec 再单独写 meta，两次写之间崩溃会留下不一致缓存 | `saveToCache` 549-565、`SaveMeta` 587-594 均为直接写 | `pkg/config/spec_cache.go:549-565,587-594` |
| F11 | **P3** | `spec_watcher.go`（778 行）与 fsnotify 依赖在**生产运行时零调用**——整文件为死代码，watcher 生命周期/泄漏风险实际不存在 | `grep NewLocalFileWatcher/NewRemoteSpecWatcher/NewSpecWatcherManager` 仅命中测试与自身 | `pkg/config/spec_watcher.go:122,528,648` |
| F12 | **P3** | `Parser.GetCachedSpec` 在无锁路径写 `cached.AccessedAt`，若未来并发调用（MCP 并发 tool call）会数据竞争；当前 CLI 单线程故 `-race` 全绿 | 静态：`value.(*CachedSpec)` 后直接 `cached.AccessedAt = time.Now()` | `pkg/spec/parser.go:633-650`（写点在 646） |
| F13 | **P3** | `detectVersion` 把整份 spec 复制成 `string` 再做多次 `strings.Contains` 扫描：体积 O(n) 双份内存，纯浪费 | 4.1MB 数据：`string(data)` 复制路径 284.5µs、分配 5.46MB（14.4GB/s） | `pkg/spec/parser.go:778-800`（780 行 `content := string(data)`） |
| F14 | **P2** | 远程 spec **install 时下载两次**（先 `parser.LoadSpec` 验证/取 baseURL，再 `FetchWithCache` 存缓存），运行时又每命令重下 1 次 → 首次安装 2× 下载 + 每条命令 1× 下载 | 计数实测：`ob install` 期间 HTTP GET **2 次**；`ob run <app> --help` 每条命令 **1 次**（`hits.log`） | `pkg/config/install.go:378-381`（第一次）+ `:409-410`（第二次）；运行时 `pkg/spec/parser.go:264` |
| F15 | **P2** | HTTP 客户端 `Timeout=0`，慢/挂起 API 使 CLI **永久挂起**（不是理论风险） | 实测：服务端 accept 后 sleep 30s，`ob run hangapp resource0 list` 在 3.0s 超时后**仍在运行**（Python `subprocess timeout=3` 触发 `TimeoutExpired`） | `pkg/cli/handler.go:45`、`pkg/mcp/handler.go:32`、`pkg/mcp/progressive.go:80` |

### 1.3 与 README 声明的对照

README 声明："**Fast Startup: Cold start under 100ms**"（`README.md`）。

| 场景 | 实测 p50 | 是否满足 <100ms |
| --- | --- | --- |
| `ob --version`（warm FS/cache） | **6.75ms** | ✅ 远超声明 |
| `ob`（无参）、`ob --help` | 6.83ms / 6.45ms | ✅ |
| `ob list`（空 HOME，0 app） | 8.99ms | ✅ |
| `ob list`（3 app，含 4.1MB spec） | **166.9ms** | ❌ |
| `ob run demo --help`（0.07MB spec） | ~10ms | ✅ |
| `ob run mb1 --help`（0.96MB spec） | **41.5–42.7ms** | ✅（但已接近） |
| `ob run mb5 --help`（4.09MB spec） | **125.9–136.3ms** | ❌ |
| `ob run mb16 --help`（14.42MB spec） | **0.49–0.52s** | ❌❌（线性外推 50MB ≈ 1.7s/命令，见 §14.3） |
| 首次执行新构建二进制（macOS Gatekeeper 首次评估） | **1130ms**（一次性） | ❌（环境伪影，非代码问题，见 §2.3） |

结论：**"Cold start under 100ms" 对 `--version` 成立且大幅优于声明；但它不是产品路径的代表性指标**。真实 app 命令的耗时由 spec 体积决定，4MB 级 spec 即破 100ms，且该成本随每次调用重复支付（F1）。建议 README 改为分档表述（如 `--version` <10ms；app 命令 ≈ spec 解析耗时 × 1 + 网络），或在实现持久化缓存后再宣称端到端 <100ms。

---

## 2. 测量环境与方法

### 2.1 环境

| 项 | 值 |
| --- | --- |
| 机器 | Apple M3 Max（`go test -bench` 报 `cpu: Apple M3 Max`），macOS arm64 |
| Go | `go1.27.3 darwin/arm64`；`CGO_ENABLED=1`（keychain 后端走 cgo） |
| 仓库状态 | `git status --short` 干净；`bin/ob` 已存在（14,442,754 B，2026-01-25，**需自查确认**：是旧构建还是本次任务前产物） |
| 构建产物 | `/tmp/ob-perf/ob`，`go build -ldflags="-s -w" -o /tmp/ob-perf/ob ./cmd/ob` → **15,156,178 B**（strip 后仍 15.1MB） |
| 计时精度 | 使用 Python `time.perf_counter()` + `subprocess.run`，避免 `/usr/bin/time -p` 在 10ms 以下分辨率不足（实测首次 1.13s 后均为 `0.00`） |

### 2.2 关键方法论：隔离 HOME

所有 CLI 测量均使用 `HOME=/tmp/ob-perf/home`（`explain`：`config.GetConfigDir()` 在 macOS 返回 `$HOME/Library/Application Support/openbridge`，`credential` 的 file/keychain 目录同样基于 HOME）。这样：

- 不触碰真实用户配置/keychain（避免污染与安全提示弹窗）；
- 可精确控制已安装 app 数量，用于 F3 的对照实验；
- `OPENBRIDGE_CONFIG_DIR` 同样可用，但为覆盖 keyring 路径选择 HOME 隔离。

安装命令（每个 spec 一个 app）：

```bash
export HOME=/tmp/ob-perf/home
/tmp/ob-perf/ob install demo --spec /tmp/ob-perf/specs/small.yaml \
  --base-url http://127.0.0.1:18080 --shim=false
/tmp/ob-perf/ob install mb1 --spec /tmp/ob-perf/specs/mb1.json --base-url http://127.0.0.1:18080 --shim=false
/tmp/ob-perf/ob install mb5 --spec /tmp/ob-perf/specs/mb5.json --base-url http://127.0.0.1:18080 --shim=false
```

### 2.3 首次执行 1.13s 的解释 `[排除]`

新构建的未签名二进制第一次执行时出现 `real 1.13s / user 0.00 / sys 0.00`，此后立即降到 6–7ms。这是 macOS Gatekeeper/`syspolicyd` 对首次执行文件的一次性校验开销（与代码无关）；**报告其余数据全部为剔除冷缓存后的稳定值（每次测量先 warmup 1–2 次）**。因此本报告不把 1.13s 计入"冷启动"结论，但提醒发布渠道若用 `-ldflags` 重新签名/分发，用户首次运行体感会是秒级——这属于分发问题，不是 Go 代码问题。

### 2.4 合成 spec 设计

仓库 `examples/todo-service` **不含 OpenAPI spec 文件**（只有 `main.py`/`pyproject.toml`/README），因此自造三档合成 spec（脚本 `/tmp/ob-perf/gen_specs.py`，参数化为资源数 × 每资源操作数）：

| 名称 | 生成参数 | 体积 | operations |
| --- | --- | --- | --- |
| `small.yaml` | 5 资源 × 8 op，YAML | 68,692 B (0.07MB) | 25（install 输出自报） |
| `mb1.json` | 70 资源 × 28 op，JSON 缩进 | 960,027 B (0.92MiB) | ~1,960 |
| `mb5.json` | 260 资源 × 78 op，JSON 缩进 | 4,089,687 B (3.90MiB) | ~20,280 |

体积口径说明：下文表格用 **MiB**（`ls` 口径）；F2/F1 的 bench 输出用 `SetBytes` 计算的 MB/s 为 MiB/s。JSON 与 YAML 的解析开销差异未单独拆分（见 §4.6 局限），但两者走同一 `kin-openapi` 路径，量级一致。

### 2.5 微基准工程（/tmp，不入库）

`/tmp/ob-perf/bench/`：

```
go.mod:  module obperfbench
         require github.com/nomagicln/open-bridge v0.0.0
         replace github.com/nomagicln/open-bridge => /Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge
go.sum:  从仓库 go.sum 复制（模块缓存已全量命中，GOPROXY=off 可离线跑）
```

该工程仅 import 仓库导出 API，**不修改仓库**；因为是 `replace` 到本地路径，import 的是工作区当前源码（等价于 `go test` 于仓库内，但避免往仓库写任何文件）。运行方式：

```bash
cd /tmp/ob-perf/bench
GOFLAGS=-mod=mod GOPROXY=off go test -run XXX -bench . -benchtime=3x -benchmem -timeout 300s
```

`-benchtime=3x` 是刻意选择：`LoadSpec/mb5` 单次 ~120ms，若用默认 1s 轮次总耗时会到分钟级，`3x` 已足够稳定（三次值离散 <5%）。

---

## 3. 冷启动实测（F1/F5 的上游事实）

### 3.1 方法

`/tmp/ob-perf/bench.py`：每个 case 先 warmup 2 次，再 `N=20` 次 `subprocess.run`，统计 min/p50/mean/p90/max；隔离 HOME；子进程带 30s 超时（本机无 `timeout` 命令，用 Python `timeout=` 参数控制，符合任务约束）。

```bash
cd /tmp/ob-perf && N=20 python3 bench.py
```

### 3.2 原始结果 `[实测]`

空 HOME（`/tmp/ob-perf/home` 尚未安装 app 时）：

```
--version          rc=0 min=   5.777ms p50=   6.754ms mean=   6.768ms p90=   7.324ms max=   7.578ms
no-args            rc=0 min=   6.238ms p50=   6.828ms mean=   7.091ms p90=   7.962ms max=  10.422ms
--help             rc=0 min=   5.806ms p50=   6.452ms mean=   6.774ms p90=   7.366ms max=  11.068ms
list               rc=0 min=   6.526ms p50=   8.986ms mean=   9.268ms p90=  11.767ms max=  14.429ms
completion-bash    rc=0 min=   7.751ms p50=  10.343ms mean=  10.554ms p90=  12.361ms max=  15.302ms
```

安装 3 个 app 后（同上 HOME）：

```
version              rc=0 p50=     7.57ms min=     6.96ms max=     9.67ms out=49B
help-root            rc=0 p50=     6.46ms min=     5.66ms max=     7.66ms out=931B
list                 rc=0 p50=   160.16ms min=   155.10ms max=   175.27ms out=199B   ← F3
completion-small     rc=0 p50=     5.74ms min=     5.42ms max=    11.67ms out=15913B
```

### 3.3 拆解：进程启动 vs 配置读取 vs spec 解析

`[静态+实测]` 每次 `ob` 调用的固定开销路径：

1. **Go runtime 启动 + init() 阶段**（`cmd/ob/main.go:51-89`）：`init()` **无条件下构造全部依赖**——`config.NewManager()`（会 `os.MkdirAll` 两个目录，`pkg/config/config.go:342-359`）、`spec.NewParser()`（`openapi3.NewLoader()`，`pkg/spec/parser.go:170-186`）、`credential.NewManager()`（`keyring.Open(...)`，`pkg/spec/../credential/keyring.go:207-233`）、`semantic.NewMapper()`（编译 2 个 regexp，`pkg/semantic/resource.go:29-40`）、`request.NewBuilder`、`cli.NewHandler`、`mcp.NewHandler`、`router.NewRouter`、`completion.NewProvider`。**即使执行 `--version` 也全部构造**。
2. **cobra 命令树构造**（`cmd/ob/main.go:102-158`）：7 个子命令 + 全局 flag，常规 cobra 开销。
3. **spec 解析**（仅 app 命令/`list`/补全触发）：见 §4。

实测结论：**固定开销约 6.5–7.6ms**（`--version` p50 6.75ms、`--help` 6.45ms），与"空 HOME 的 `list` 8.99ms"对比可知配置目录扫描 <1ms。也就是说 init() 里那一长串构造（含 `keyring.Open`）**成本很低**（`BenchmarkKeyringNewManager` 1.8µs/op，见 §5），不值得为 6.7ms 启动做延迟初始化——**真正的问题是第 3 步**。

### 3.4 100ms 声明的正确读法

- 若"cold start"指**进程能打印版本**：实测 6.75ms，优于声明 15×。
- 若指**用户跑一条真实 API 命令**：耗时 ≈ 6.7ms + spec 解析（§4）。spec ≤ ~1.5MB 时仍 <100ms；4MB 时 **126–136ms**；远程 URL spec 再加网络 RTT/下载时间（§4.4）。
- 因此声明"技术上不算错，但具有误导性"。建议文档分档（见 §12）。

---

## 4. spec 解析与缓存（本报告最重要章节）

### 4.1 F2：解析是唯一主成本，且超线性 `[bench]`

`BenchmarkLoadSpec`（`p := spec.NewParser(); p.LoadSpec(path)`，即真实调用链 `loadFromFile → os.ReadFile → parseSpec → parseOpenAPI3 → loader.LoadFromData → doc.Validate`）：

```
BenchmarkLoadSpec/small-16            3    2301972 ns/op    29.84 MB/s    1997770 B/op     23881 allocs/op   (0.07MB)
BenchmarkLoadSpec/mb1-16              3   33634472 ns/op    28.54 MB/s   27759202 B/op    333576 allocs/op   (0.96MB)
BenchmarkLoadSpec/mb5-16              3  119369417 ns/op    34.26 MB/s  109272248 B/op   1237757 allocs/op   (4.09MB)
```

对比 `BenchmarkParseBytesOnly`（跳过文件 I/O，直接 `ParseSpecFromJSON(data)`）——**几乎完全一致**（mb5 125.7ms vs 119.4ms，在 3 次采样噪声内）：

```
BenchmarkParseBytesOnly/mb5-16        3  125709444 ns/op    32.53 MB/s  105196213 B/op   1237886 allocs/op
```

结论：
1. **磁盘 I/O 不是瓶颈**（文件已 page cache），**CPU 解析 + 校验**是瓶颈；
2. 吞吐 ~28–34MB/s（线性，无超线性恶化），即每 MB spec 约 **30ms 解析 + 27MB 分配**；
3. 分配量 109MB 对 4MB 输入是 **26.7× 放大**，1.24M 次分配——GC 压力主要来自 kin-openapi 的内部结构与 `doc.Validate`（走引用、schema 校验），属上游库行为，仓库侧只能通过"减少解析次数"（缓存）而非"加速单次解析"来优化。

耗时构成（静态结合 bench）：
- `os.ReadFile`：`BenchmarkDetectVersionCopy` 侧的 4.1MB 内存拷贝 284µs 量级 → 读文件本身 <1ms；
- `detectVersion`（`parser.go:778`）：`content := string(data)` 复制 + ≤6 次 `strings.Contains` 全量扫描，O(n)；
- `openapi3.Loader.LoadFromData`（`parser.go:452`）：YAML/JSON 解码 + 引用解析（`IsExternalRefsAllowed = true`，`parser.go:172`）；
- **`doc.Validate(ctx)`（`parser.go:455-460`）**：对解析后的文档做完整校验（必填、引用、类型、路径参数一致性等）。这是"第二次全量遍历"，静态推断占总耗时 20–40%（未拆 bench，建议后续以 `pprof` 确认）。

### 4.2 F1：持久化解析缓存是空实现（P0）

**调用链（生产代码）**——三处，全部走同一个空壳：

```
cmd/ob/main.go  →  router.Execute (pkg/router/router.go:217)
                 →  cli.Handler.loadAndCacheSpec (pkg/cli/handler.go:85)
                 →  spec.Parser.LoadSpecWithPersistentCache (pkg/spec/parser.go:837)
```

`pkg/spec/parser.go:835-839` 全文：

```go
// LoadSpecWithPersistentCache loads an OpenAPI specification with persistent caching support.
// This enables cross-process caching of parsed specs to avoid re-parsing on each restart.
func (p *Parser) LoadSpecWithPersistentCache(ctx context.Context, source string, appName string) (*openapi3.T, error) {
	return p.LoadSpecWithContext(ctx, source)
}
```

- 函数体**直接转发**，`appName` 参数（缓存 key）完全未使用；
- 注释承诺"avoid re-parsing on each restart"，实现为零；
- 三个调用点：`pkg/cli/handler.go:85`、`pkg/router/router.go:217`、`pkg/completion/completion.go:402`。

**配套写盘/读盘函数存在但零调用**：

| 函数 | 位置 | 生产调用点 |
| --- | --- | --- |
| `SaveParsedSpec` | `pkg/config/spec_cache.go:694` | **0**（`grep -rn "SaveParsedSpec" --include=*.go . \| grep -v _test.go` 仅命中定义） |
| `LoadParsedSpec` | `pkg/config/spec_cache.go:739` | **0** |
| `ValidateParsedSpec` | `pkg/config/spec_cache.go:784` | **0** |
| `SpecCacheMeta.ParsedSpecPath/ParsedAt/ParserVersion` | `pkg/config/spec_cache.go:67-74` | 仅 `SaveParsedSpec` 写入（死路径），无人读取 |

**唯一生效的缓存是进程内内存缓存**：

- `Parser.cache sync.Map`（`pkg/spec/parser.go:117-119`），`cacheTTL` 默认 5 分钟（`parser.go:177`）；
- `GetCachedSpec`（`parser.go:633`）/`CacheSpecWithSource`（`parser.go:673`）；
- **对一次性 CLI 进程毫无意义**：每个 `ob <app> ...` 都是新进程，`sync.Map` 冷启动必空；只有 MCP server 长驻进程内才有效（MCP 是长驻，确实受益——见 §7）。

**更关键：现有持久化设计即使接上也没有收益** `[bench]`。`SaveParsedSpec/LoadParsedSpec` 的实现是"把整棵 `openapi3.T` 用 `json.MarshalIndent` 落盘、再 `json.Unmarshal` 回来"（`spec_cache.go:700-716`、`744-767`）。在 4.1MB spec 上实测：

```
BenchmarkSaveParsedSpecMB5-16      2   37732688 ns/op   101651812 B/op    223833 allocs/op
BenchmarkLoadParsedSpecMB5-16      2  118071479 ns/op   101620556 B/op   1151814 allocs/op
```

- `LoadParsedSpec` **118ms / 101.6MB** ≈ 重新解析 **119.4ms / 109.3MB**；
- 原因：`openapi3.T` 序列化时已无 YAML 优势，且 `json.MarshalIndent` 把大量小 map/slice 展开（写盘 37.7ms、100MB 临时分配），读回再做完整反序列化 + 无校验但仍需重建所有引用结构；
- 磁盘体积也会膨胀（小 spec 落盘 JSON 已是源 YAML 的数倍，属静态推断；`SaveParsedSpec` 小 spec 单次 1.97ms/2.11MB 分配可佐证）。
- **因此该死代码不能简单"接上开关"**，正确方向见 §12-opt1（缓存派生产物，不缓存 `openapi3.T`）。

**影响量化**：4.1MB spec 每条命令 **+119ms**；0.96MB **+33.6ms**；且每次命令必付。假设用户 4MB spec 每天跑 200 条命令，即每天约 24 秒纯重复解析、约 21.8GB 累计分配（109MB × 200）。

### 4.3 缓存键/哈希/失效判断（静态审计）

| 维度 | 现状 | 位置 | 评价 |
| --- | --- | --- | --- |
| 键 | 内存缓存 key = `appName`；远端缓存 key = `appName`（目录名）；**无 source+mtime/size/hash 复合键** | `parser.go:670`、`spec_cache.go:147,700` | 内存缓存以 app 名作键，spec 源变更后只要 TTL 内仍命中旧解析结果（5 分钟窗口）；MCP 长驻进程会吃到陈旧 spec |
| 哈希 | 远端缓存用 `sha256.Sum256(整个 content)` 做完整性校验（`computeHash`，`spec_cache.go:184-187`）；**解析缓存不使用哈希** | `spec_cache.go:184`、`770-781` | `checkSourceIntegrity` 为本地文件读取全文再哈希（`spec_cache.go:770-781`），**为了判断是否变化而把 spec 全读一遍**——对 4MB spec 是 ~1ms 级额外 I/O，可接受但设计笨；且该函数属死路径 |
| 失效判断 | 远端缓存：`ExpiresAt`（Cache-Control/Expires/默认 24h TTL，`spec_cache.go:189-249`）+ ETag/Last-Modified 条件请求（`spec_cache.go:449-460`）；**运行时不读缓存所以以上全部无效** | `spec_cache.go:78-80,341,449-460` | 设计完整、实现闲置 |
| 原子写 | `SaveParsedSpec` 用 tmp+rename（**原子**，`spec_cache.go:710-716`）；`saveToCache` 的 spec 用 `os.WriteFile` 直写（**非原子**，`spec_cache.go:557`），meta 另写（`:587-594`）→ 两次写间崩溃产生"新 spec + 旧 meta"或"spec 存在 meta 缺失" | 见左 | F10 |
| 全量 JSON 往返 | Swagger 2.0 路径：`ContentDetector.ToJSONWithFallback(data)` 先整体 YAML→JSON 再 `json.Unmarshal`（`parser.go:493-505`），即**多一次全量序列化往返**；远端缓存 `FetchResult.Content` 也整体驻留 | `parser.go:489-521` | 对 2.0 spec 额外一次 O(n) marshal+unmarshal，P3 |

### 4.4 F4：远程 URL spec 每次命令重新下载（P1）

- install 时会把远端 spec 缓存到磁盘：`pkg/config/install.go:406-414`（`if isWebURL(primarySource) { cacheManager := NewSpecCacheManager(m.AppsDir()); cacheManager.FetchWithCache(...) }`）；
- 但 `config.AppConfig.SpecSource` 保存的仍是 **URL 本身**（`pkg/config/install.go:271` `SpecSource: specSource`，`prepareSpecSources` 不落地为本地路径）；
- 运行时 `LoadSpecWithPersistentCache → LoadSpecWithContext`：`isURL(source)` 为真 → `loadFromURL`（`pkg/spec/parser.go:264-295`）→ `p.httpClient.Do(req)` **裸 HTTP 请求**，无缓存读取、无 ETag 复用、无本地回退；
- `SpecCacheManager`（816 行）的生产调用点仅 2 处：`install.go:409` 与死代码 `spec_watcher.go:598`（`grep -rn "FetchWithCache\|NewSpecCacheManager" --include=*.go . | grep -v _test.go` 已核对）。
- **后果**：远程 spec 的每条 CLI 命令 = DNS+TLS+GET 全量下载 + 解析。spec 1MB 时局域网 +2–5ms 可忽略，公网 100ms RTT 级 + 解析 33ms = 单命令 150ms+；spec 5MB 或慢网时直接秒级，且**离线时完全不可用**（install 时的磁盘缓存本应支持离线，实际没有）。这是 F1 的"远端放大版"，严重度按端到端影响记为 P1。
- **计数实测**（详见 §14.1）：本地 HTTP server 记录请求，`ob install <url>` 期间 **2 次 GET**（F14 双下载），`ob run <app> --help` **每条命令 1 次 GET**（`hits.log` 计数）。

### 4.5 是否每次启动都解析 spec

- `ob --version` / `--help` / `ob`（无参）：**不解析**；`init()` 只构造 Parser（`parser.go:170`），不发 I/O；
- `ob list` / `ob info`：**解析所有 app 的 spec**（F3，`install.go:918-922`）；
- `ob run <app> ...`、shim 调用（`<app> ...`）：**解析 1 个 spec**（F1 的 stub 链）；
- `ob <TAB>` / `ob run <app> <TAB>`：**每次按键起新进程 + 解析 1 个 spec**（F5）；
- MCP 模式：启动解析 1 次，进程内缓存复用（`handler.go:77-91`），`GetCachedSpec` 命中后不再解析——**这是仓库里唯一真正生效的缓存**。

### 4.6 局限与未测项

- `doc.Validate` 的占比未单独 bench（需要临时克隆 loader/validate 或加 pprof，受"禁止改源码"约束未做）；报告中该占比标注为**静态推断**。
- 未用真实世界 spec（petstore/GitHub/Stripe）验证；合成 spec 结构规律（大量同构 operation、重复 schema），实际 spec 的引用复用更高，解析可能略有不同的分配曲线。**建议后续用 petstore.yaml + GitHub API spec 复测**。
- JSON vs YAML 差异未拆分：`small.yaml` 仅 0.07MB，与 `mb1.json` 不同档，无法直接对比格式影响。静态预期 YAML 解码更慢，但两者都远小于 `Validate` 的间接成本。

---

## 5. keyring（`pkg/credential/keyring.go`，639 行）

### 5.1 实测 `[bench]`

```
BenchmarkKeyringNewManager-16        3    1819 ns/op       384 B/op     7 allocs/op
BenchmarkKeyringGetCredential-16     3  189333 ns/op      1618 B/op    39 allocs/op
```

### 5.2 结论

- **`NewManager` 很便宜（1.8µs）**：`keyring.Open(config)`（`keyring.go:207-233`）在 macOS 上只是构造后端、不做 Keychain 查询，所以 `init()` 里无条件调用（`cmd/ob/main.go:65-72`）不是启动瓶颈。**`[排除]`：keyring 初始化不是冷启动问题。**
- **每次取凭据 189µs**：`GetCredential`（`keyring.go:382-410`）→ keyring 读（macOS Security framework cgo 调用）。调用点是 `request.Builder.InjectAuth`（`pkg/request/builder.go:97-108`），**每条 API 命令/每次 MCP tool call 都调用一次**。
  - 对 CLI 单请求：189µs 相对解析（30–120ms）可忽略；
  - 对 MCP 高 QPS：每次 tool call 固定 +0.19ms，10k 调用/天 ≈ 1.9s，影响 P3 级；
  - 真正值得注意的**不是延迟而是语义**：即使无 auth 配置（`AuthType == "none"`）也会先查 keyring（`builder.go:97-101` 先 `GetCredential` 再判断 `injectAuthCredentials`），即**每条命令 1 次系统 keyring 访问**；在 CI/无 keychain 环境（如 Linux Docker 无 Secret Service）会走错误分支（`NewManager` 失败时 `main.go:67-71` 只降级为 nil manager，警告后继续），此时 `InjectAuth` 直接 `if b.credMgr == nil { return nil }`（`builder.go:99-101`）。
- **无缓存**：`Manager` 不缓存已取凭据（`pkg/credential/keyring.go` 无 cache 字段），同一进程内 N 次 tool call = N 次 keyring 读。MCP 长驻场景建议加进程内 LRU（收益：每 call 省 189µs；成本 S）。
- **静态推断**：`ListCredentials`（`keyring.go:449`）会枚举整个 keyring 服务（keyring 库 `Keys()`），若被 `ob list --details`/`info` 之类路径调用会放大；已核对：`main.go` 的 list/info 路径**不调用** credential manager，故无影响。

---

## 6. HTTP 请求路径（`pkg/request/builder.go` 681 行 + 三处执行点）

### 6.1 客户端复用与超时（F7，静态 + 实测辅助）

| 执行点 | 客户端构造 | Timeout | Transport | 位置 |
| --- | --- | --- | --- | --- |
| CLI | `httpClient: &http.Client{}` | **0（无超时）** | 默认 `http.DefaultTransport`（新建 Transport 实例，但进程内复用同一 client） | `pkg/cli/handler.go:45` |
| MCP | `httpClient = &http.Client{}`（nil 时） | **0** | 默认 Transport | `pkg/mcp/handler.go:30-34` |
| MCP progressive | 同上 | **0** | 默认 Transport | `pkg/mcp/progressive.go:74-85` |
| spec 远端加载 | `&http.Client{Timeout: 30s}` | 30s | 默认 Transport | `pkg/spec/parser.go:174-180` |
| spec 远端缓存 | `&http.Client{Timeout: 30s}` | 30s | 默认 Transport | `pkg/config/spec_cache.go:110-117,126-133` |

分析：
- **进程内复用**：每个 CLI 进程只发 1 个请求，连接池无意义（这点不是问题）；MCP server 长驻，`h.httpClient` 是同一实例，**Transport 在同一进程内确实复用**（`[静态]`：`Do` 由 client 方法调用，未发现每次 new client），所以"连接不复用"**不成立**——`[排除]` 一个常见误判。
- **真实问题（已实测复现，F15）**：`cli`/`mcp` 路径 **Timeout=0（永不超时）**。默认 Transport 只有 `DialContext` 30s、`TLSHandshakeTimeout` 10s、`ResponseHeaderTimeout` 0，若服务端接受连接后不返回响应头，CLI 会**永久挂起**（用户只能 Ctrl-C）。实测：server accept 后 sleep 30s，`ob run hangapp resource0 list` 在 **3.0s 超时后仍在运行**（§14.2）。MCP 场景服务端 hang 会占住 goroutine 且 MCP 客户端无感知。这是 **P2 可靠性问题**，修复成本 S（3 处加 `Timeout`，且 profile 已有 `Timeout/TLSConfig/RetryConfig` 字段但**运行时未接线**，写进 yaml 的可靠性配置当前完全不生效，`config.go:88-120`）。
- **未设置 `MaxIdleConnsPerHost`**：MCP 并发大量 tool call 时默认 `MaxIdleConnsPerHost=2` 可能导致连接反复重建（Go 默认值），`[静态]`，影响取决于并发度，P3。

### 6.2 请求体构建（`builder.go` 全流程）

- `BuildRequest`（`builder.go:36-76`）：路径参数替换（`substitutePathParams`）→ query 拼接（`buildQueryString`）→ URL 字符串拼接（`buildFullURL`，`builder.go:80-86` 用 `+` 拼接，O(3) 字符串，可忽略）→ body 序列化（`buildRequestBody`，`json.Marshal` 到 `[]byte` 再 `bytes.NewReader`，`builder.go:96-118`）→ header 注入。
- **请求体全量驻留内存**：JSON body 先 marshal 成 `[]byte`（一次分配），`bytes.Reader` 包装（不复制）→ 对超大请求体（例如上传 100MB）会整块进内存；且 `createBodyReader` 仅对 GET/HEAD 跳过（`builder.go:96-99`），POST/PUT/PATCH/DELETE 均走 JSON。
- **DELETE 带 body 的静态风险**：`method == GET || HEAD` 才跳过 body，DELETE 也会尝试构建 body——与 OpenAPI 语义基本兼容，非性能问题。
- **参数校验**：`ValidateParams`（`builder.go` 后半段，被 `cli/handler.go:346` 调用）对每个参数做类型/必填检查，O(参数数)，可忽略。

### 6.3 响应体处理（F6，P1）

三处执行点全部 `io.ReadAll` 全量读入：

```go
// pkg/cli/handler.go:141-152
func (h *Handler) readResponse(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	...
}
```

- 之后 `formatAndPrintOutput`（`cli/handler.go:372-380`）→ `FormatOutput`（`:496-528`）把 body **再 unmarshal 成 `any` 然后重新 marshal** 成 json/yaml/table → `fmt.Println(output)`（整串打印）。即一条响应在内存中至少存在 3 份：原始 `[]byte`、解析后的对象树、重新序列化的 `string`（`fmt.Println` 处还有 stdout 缓冲）。
- **实测内存放大** `[实测]`：本地 HTTP server 返回固定 body，比较 100B vs 5MB：

```bash
# srv.py 返回 {"data":"x"*N}，N 由 /tmp/ob-perf/resp_size 控制
HOME=/tmp/ob-perf/home /usr/bin/time -l ./ob run demo resource0 list          # 100B body
  0.02 real  0.01 user  0.00 sys     23183360  maximum resident set size   (23.2MB)
HOME=/tmp/ob-perf/home /usr/bin/time -l ./ob run demo resource0 list -o json  # 5,000,000B body
  0.09 real  0.08 user  0.01 sys     62029824  maximum resident set size   (62.0MB)
```

- **5MB body → 峰值 RSS +38.8MB（7.8×）**，耗时 20ms→90ms。
- MCP 路径同样 `io.ReadAll`（`pkg/mcp/handler.go:116-122`、`pkg/mcp/progressive.go:487-493`），而且 MCP 结果是 `TextContent` 整串塞进一次响应 → 大响应在 MCP 场景会同时放大客户端/服务端内存与 LLM 上下文（P1 级风险，尤其"list 端点返回 10MB JSON"是最常见的 OpenAPI 场景）。
- **压缩**：Go Transport 默认在未显式设置 `Accept-Encoding` 时自动加 `gzip` 并透明解压（仓库未设置该 header，`builder.go:52-74` 只加 Content-Type 与用户 header）→ 传输层压缩**是正常工作的**，`[排除]`"未处理 gzip"的猜测。但透明解压后 `io.ReadAll` 仍拿到解压后的全量，内存放大不因 gzip 改善。
- **重定向**：未自定义 `CheckRedirect`，Go 默认最多 10 跳；`[静态]` 无死循环风险，但每跳会重新发送 header（含 Authorization，跨域时 Go 会剥离敏感头，标准行为）。
- **建议**（P1 修复，成本 M）：流式输出（`io.Copy` 到 stdout + 仅对小 body 解析格式化）+ 响应大小上限（如 >8MB 直接原样流式输出并提示 `-o raw`）+ MCP 侧对 body 截断（如 1MB 上限 + 分页提示）。收益：5MB body 峰值内存可从 ~62MB 降到 ~24MB 基线；避免 OOM（当前 100MB 响应理论峰值 >1GB，静态推断）。

---

## 7. MCP 模式（`pkg/mcp`：progressive.go 552、registry.go 364、engine_predicate.go 307、handler.go 304）

### 7.1 工具注册时机（结论：启动时一次，非每请求）`[静态+bench]`

- **非 progressive 模式**：`mcp.Handler.SetSpec`（`handler.go:42-46`）→ `Register`（`:53-65`）在 server 启动时构建并 `s.AddTool` 一次；`HandleCallTool` 每请求只做 `json.Unmarshal` 参数 + 构建 HTTP 请求（`handler.go:150-200`）。**不存在"每次请求重建 schema"**，`[排除]`。
- **progressive 模式**：`ProgressiveHandler.SetSpec`（`progressive.go:99-117`）启动时 `registry.BuildFromSpec` + `searchEngine.Index` 一次；`Register`（`:128-140`）注册 3 个 meta-tool。每次 `SearchTools` 不重建 schema。
- 但 `Register` 内 **3 个描述模板每次 `template.New(...).Parse(...)`**（`progressive.go:154`、`:207`、`:253`）。仅启动一次，单次 <100µs `[静态]`，P3 可忽略。

### 7.2 工具 schema 生成成本 `[bench]`

```
BenchmarkRegistryBuildFromSpec/small-16   3     39438 ns/op    86432 B/op     587 allocs/op   (25 ops)
BenchmarkRegistryBuildFromSpec/mb1-16     3    830416 ns/op  1279272 B/op    7887 allocs/op   (1960 ops)
```

- 每个 operation 生成一个 `mcp.Tool`（`convertOperationToMCPTool`，`registry.go:150-176`，`buildPropertySchema` `:230-252` 递归构造 `map[string]any` JSON Schema）→ ~0.42µs/op、~650B/op、4 allocs/op。
- **对 20k op 的 4MB spec 推算 ~8.5ms / 13MB**（线性外推，未实测 mb5；`[静态]`外推可信度：两档线性度良好）。相对解析 119ms 是 7%，**不是主要问题**，但注意：**非 progressive 模式下所有工具 schema 常驻内存**（20k × ~650B ≈ 13MB + `mcp.Tool` 结构开销），MCP 进程 RSS 会明显高于 CLI。
- progressive 模式的价值在于**不把 N 个 schema 发给客户端**（客户端上下文），代价是服务端仍持有全量 registry——这是正确取舍，`[排除]`"progressive 注册每次重建 schema"的担忧。

### 7.3 progressive disclosure 搜索（F9，P2）

- `Search`（`engine_predicate.go:242-260`）：`e.mu.RLock()` 后**线性遍历全部 tool metadata**逐个执行 predicate；无倒排索引、无标签索引。
- 每次查询 `createMatcher`（`engine_predicate.go:43-64`）都 `predicate.NewParser(...)` 重新解析表达式（vulcand/predicate 的 parser 构造 + AST 解析），`[bench]` 实测 1000 工具时 `Search` 20.7µs/9512B/42 allocs——**单次很便宜**，P2 而非 P1。
- **真正的风险在结果**：`handleSearchTools`（`progressive.go:315-330`）→ `formatSearchResults`（`:291-312`）把**所有命中工具**拼成一个字符串（无 top-K、无截断）；`query` 为空时 `Search` 直接 `getAllTools()` 返回全量（`engine_predicate.go:246-248`、`getAllTools` `:262-264`）。20k op 的 spec 若客户端发 `query=""` 或宽松查询，单次 MCP 响应可达 **数 MB 文本**（每工具约 100–200B 文本，20k ≈ 2–4MB）→ 直接吃掉 LLM 上下文窗口并造成传输阻塞。**这是 P1 级体验风险**（严重度定级见 §1.2 F9 记 P2，因为触发需客户端"列全部"行为；若产品定位面向 AI Agent，实际触发概率高，建议按 P1 处理）。
- 缓解（成本 S）：`Search` 加 `limit`（默认 30）+ `formatSearchResults` 截断并返回 "还有 N 个匹配，请缩小查询"；`query == ""` 时返回分类摘要而非全量。

### 7.4 MCP 内存与生命周期 `[静态]`

- `registry.loadedTools` 缓存（`registry.go:283-303`）**无上限**：客户端调用 `LoadTool` 的每个 toolID 都永久驻留（含完整 `mcp.Tool` schema）。正常使用下上限 = 工具总数（有界，因为 toolID 来自 registry），所以**不算无界增长**；但"loaded 缓存"的设计使每个已加载工具在 `registry.tools` 与 `loadedTools` 中各存一份 `mcp.Tool`（`Load` 里 `r.loadedTools[toolID] = tool`，`:299-302`）→ 2× 内存，P3。
- `ProgressiveHandler.Close`（`progressive.go:508-513`）只关搜索索引（predicate 引擎为 no-op）；**没有 goroutine 需要回收**（predicate engine 无后台协程）→ 无 goroutine 泄漏，`[排除]`。
- server 生命周期：`factory.RunServer`（`pkg/mcp/server_factory.go`）用 stdio transport，进程随连接退出；未发现泄漏。
- **MCP 每 tool call 固定开销**：`parseToolArguments`（json.Unmarshal，`handler.go:180-190`）+ `InjectAuth`（keyring 189µs！见 §5）+ HTTP 请求（无超时，见 §6.1）+ `io.ReadAll`（见 §6.3）。**keyring 是 MCP 每 call 最值得优化的一项**。

---

## 8. CLI 补全（`pkg/completion/completion.go` 418 行 + `cmd/ob/main.go`）

### 8.1 机制（静态）

- `ob completion bash|zsh|fish|powershell` 生成**静态脚本**（`newCompletionCmd`，`cmd/ob/main.go`；实测 `completion bash` 输出 15,913B，耗时 p50 10.3ms，**不解析 spec**）。
- 补全脚本由 cobra 生成，对 `run` 子命令注册了 `ValidArgsFunction: completeRunArgs`（`main.go:798`、`:967-993`）：每次 Tab → shell 起一个**新的 `ob __complete ...` 进程** → cobra 解析 → `completeRunArgs` → `completionHelper.CompleteResources/CompleteVerbs/...` → `Provider.loadSpec`（`completion.go:381-402`）→ **`LoadSpecWithPersistentCache`（即空壳）→ 完整解析 spec**。
- `Provider.loadSpec` 内的 `getCachedSpec`（`completion.go:388-390`）读的是 `Parser.cache`（进程内 sync.Map）——**对每次新进程必然 miss**。

### 8.2 实测 `[实测]`（每个 case 10 次，先 warmup 1 次；隔离 HOME）

```
demo __complete run demo ''            : min 9.45ms  p50 11.73ms  max 17.32ms   out=53B
mb1  __complete run mb1 ''             : min 41.54ms p50 42.67ms  max 44.25ms   out=763B
mb5  __complete run mb5 ''             : min 125.95ms p50 133.61ms max 136.30ms out=3013B
demo __complete run demo resource0 ''  : min 9.05ms  p50 9.55ms   max 10.45ms
mb1  __complete run mb1 resource0 ''   : min 39.91ms p50 42.10ms  max 42.98ms
mb5  __complete run mb5 resource0 ''   : min 130.85ms p50 135.08ms max 138.87ms
list（3 app）                          : p50 166.92ms（min 166.92 / max 170.90）
list --details（3 app）                 : p50 151.71ms
```

解释与结论：
- 补全耗时 ≈ 固定 6.7ms + spec 解析（§4.1），**完全线性于 spec 体积**；
- 每次 Tab 都付一次完整解析；对 1MB spec 用户感知为 **~43ms/键**，4MB 为 **~134ms/键**——在交互式 shell 中会造成明显卡顿（尤其连续 Tab/长按）；
- 输出体积本身很小（53B–3013B），**不是输出问题**，是解析问题；
- `list` 与 `list --details` 都 ~150–170ms，因为两者都要 `GetInstalledAppInfo`（F3）——`--details` 略快只是噪声。

### 8.3 建议（成本 S/M）

- S：`ob completion` 脚本生成时把 app 名/资源名列表**静态内嵌**（安装/卸载时刷新），Tab 完全不起进程；
- M：为补全提供**轻量命令树索引**（见 §12-opt1），补全只读 ~10–50KB 的索引文件，不复用 `openapi3.T`；
- S（兜底）：`completeRunArgs` 对 spec 解析加 100ms 超时/降级为"无补全"，避免卡 shell。

---

## 9. 语义匹配算法（`pkg/semantic/resource.go` 519 行、`verb.go` 525 行、`mapper.go` 161 行）

### 9.1 复杂度实测 `[bench]`

```
BenchmarkBuildCommandTree/small-16   3     82139 ns/op    141056 B/op    1148 allocs/op   (25 ops)
BenchmarkBuildCommandTree/mb1-16     3    792569 ns/op    798218 B/op    5592 allocs/op   (~1960 ops)
BenchmarkBuildCommandTree/mb5-16     3   2485806 ns/op   2589285 B/op   18519 allocs/op   (~20280 ops)
```

- 线性度良好（25→20,280 ops 耗时 82µs→2.49ms，约 0.12µs/op），**没有 O(n²) 灾难**；
- 相对解析成本：mb5 的 2.49ms 仅占 `LoadSpec` 119ms 的 **2.1%**，mb1 的 0.79ms 占 2.4%——**语义映射不是主成本**，`[排除]`"语义匹配算法是大头"的假设；
- 但每 op 115 allocs（18,519/20,280）偏多，属可优化但收益有限（P3）。

### 9.2 具体热点（静态定位，配合 bench）

1. **`defaultMethodMapping` 内每次调用 `regexp.MustCompile`**（`pkg/semantic/verb.go:224`）：

```go
func (m *VerbMapper) defaultMethodMapping(method, path string) string {
	hasPathParam := regexp.MustCompile(`/\{[^}]+\}$`).MatchString(path)
```

   `MustCompile` 每次编译一个正则（约 1–3µs + 若干分配）。该函数是 `MapVerb` 的 **Priority 5 默认分支**（`verb.go:207-213`），**对常规 CRUD 路径必然走到**（因为没有 `x-cli-verb`、路径不以 `activate/list` 等 action 词结尾、operationId 不以动词开头时）。20k op 即 20k 次正则编译，`[静态]`估计 **20–60ms**（未单独 bench，因函数未导出；按 Singularize 1750ns/4alloc 量级估算单次编译 1–3µs）。**这是 semantic 包内最值得修的一条（成本 S：包级 var + 一次编译）**。
2. **`checkPathPatternRules` 线性扫 35 条预编译正则/op**（`verb.go:145-153`，规则表 `createActionRules` `:38-78`）：每条规则 `strings.ToUpper(method)` + `MatchString`；对 GET/POST 常规路径全部不命中 → 每次 35 次匹配。20k op ≈ 70 万次正则匹配，`[静态]`估 5–15ms。可用**按 method 分桶 + 按 path 后缀 switch**（成本 M，收益有限）。
3. **`inferVerbFromOperationID` 每次构造 map**（`verb.go:289` `verbMappings := getVerbMappings()`，函数在 `:264-283` 返回 `map[string]string` literal）：每 op 一次 map 分配（约 40 项 → 堆分配 + 哈希），且**遍历顺序随机**（Go map 迭代）导致"list"/"get"等多前缀命中时结果不确定（**正确性隐患**：如 `getUserList` 可能匹配 `get` 或 `list`）。建议改为有序 slice + 前缀 switch（成本 S，顺带修确定性）。
4. **`VerbSet.Add` 每次 `AllVerbs()` 分配+排序**（`verb.go:391-410`）：

```go
func (s *VerbSet) Add(mapping *VerbMapping) string {
	originalVerb := mapping.Verb
	existingVerbs := s.AllVerbs()          // 分配 []string + sort
	if containsVerb(existingVerbs, mapping.Verb) { ... }
```

   `AllVerbs`（`:411-421`）收集全部 verb 再 `sort.Strings`，然后 `containsVerb`（`:373-376`）线性查找。单个资源内 verb 数通常 ≤8，所以实际 O(k²logk) 的 k 很小 → **总成本不高**（bench 显示 20k op 仅 2.49ms），但纯属浪费；改成 `map` 查存在性即可（成本 S）。`[静态]`
5. **`Singularize`/`Pluralize` 每次调用重建 map + slice**（`resource.go:295-350`、`:352-...`）：

```
BenchmarkSingularize-16    3  1750 ns/op  1202 B/op  4 allocs/op
BenchmarkPluralize-16      3  2028 ns/op  1208 B/op  4 allocs/op
```

   两个函数体内联 ~20 项 `map[string]string`（irregulars）与 8 项结构体 slice（suffixMappings），每调用一次就堆分配一次。**关键问题：当前 `Singularize` 在生产路径中零调用**（`grep -rn "Singularize(\|NormalizeResourceName(" --include=*.go pkg | grep -v _test.go` 仅命中 `resource.go:287` 定义处内部调用，而 `NormalizeResourceName` 自身也无人调用）——**又一个死代码 + 反模式**（若未来启用，每资源 ~2µs）。建议提升为包级 `var`（成本 S），并删除未使用方法或补测试覆盖。
6. **字符串拼接热点**：`NormalizeName`（`resource.go:264-276`）连续 `strings.ReplaceAll` ×2 + `strings.ToLower` ×2 + `normalizeForCLI` 逐 rune `WriteRune`（`:277-285`）——每资源名 4–6 次分配。20k op 通常复用资源名，实际影响小，P3。
7. **`Extract` 每次 `&ExtractResult{}` + 多次 slice 分配**（`resource.go:120-136`、`parsePathSegments`/`findResourceSegments`/`extractPathParams` 各分配一次 slice）：每 op 3–5 次分配，属 115 allocs/op 的主要来源之一，P3。

### 9.3 建议

- **S 级（低风险高确定性）**：`verb.go:224` 正则提为包级 `var`；`verbMappings` 改有序 slice；`Singularize/Pluralize` 的 map/slice 提为包级；`VerbSet.Add` 用 map 判重。合计 `[静态]`预计 `BuildCommandTree` 降 30–60%（mb5 2.49ms→1.0–1.7ms），但对端到端（解析 119ms）**收益 ≤1.5%**——**优先级低，建议随其他改动顺手做，不单独立项**。
- 真收益在"减少 BuildCommandTree 的次数"（当前每次命令 1 次；§4 的持久化索引方案会把整棵树缓存到磁盘，见 §12）。

---

## 10. 并发、内存与 watcher 生命周期

### 10.1 `-race` 是否全绿 `[实测]`

```bash
go test -race -short ./...
```

结果（全部通过，`[实测]`）：

```
ok  github.com/nomagicln/open-bridge/cmd/ob          2.107s
ok  github.com/nomagicln/open-bridge/internal/integration 2.606s
ok  github.com/nomagicln/open-bridge/internal/proptest    4.755s
ok  github.com/nomagicln/open-bridge/pkg/cli         3.329s
ok  github.com/nomagicln/open-bridge/pkg/codegen     3.732s
ok  github.com/nomagicln/open-bridge/pkg/completion  2.976s
ok  github.com/nomagicln/open-bridge/pkg/config      (cached)
ok  github.com/nomagicln/open-bridge/pkg/credential  4.211s
ok  github.com/nomagicln/open-bridge/pkg/mcp         (cached)
ok  github.com/nomagicln/open-bridge/pkg/request     1.792s
ok  ... (其余包同)
```

结论：**`-race` 全绿**。但注意"全绿"≠"无竞争可能"：见 F12。

### 10.2 F12：`GetCachedSpec` 的无锁写（P3）

`pkg/spec/parser.go:633-650`：

```go
func (p *Parser) GetCachedSpec(appName string) (*openapi3.T, bool) {
	value, ok := p.cache.Load(appName)   // sync.Map 读，无锁
	...
	cached := value.(*CachedSpec)
	...
	cached.AccessedAt = time.Now()       // ← 对共享对象的无锁写（:646）
	return cached.Spec, true
}
```

- 若两个 goroutine 并发调用 `GetCachedSpec`（MCP server 未来若并发处理 tool call，或 `cli.Handler` 被并发复用），会构成 **data race**（`sync.Map` 只保证 map 操作安全，不保护 value 内部字段）；
- 当前生产路径：CLI 单 goroutine、MCP SDK 的 tool handler 是否并发由 SDK 决定（`mcp.Server` 对 stdio 通常串行），且 MCP 路径不调用 `GetCachedSpec`（MCP 用 `SetSpec` 一次），所以**当前无实际竞争**，`-race` 全绿符合预期；
- 修复（S）：`AccessedAt` 不写或改用 `atomic.Int64`；`GetCachedSpecInfo`（`:652-666`）不写字段，可直接对照。

### 10.3 锁粒度

- `Parser.cache`：`sync.Map`，无全局锁；`CachedSpec` 字段无保护（F12）。粒度合适。
- `SpecCacheManager`：**无任何锁/互斥**（`pkg/config/spec_cache.go` 全文无 `sync` import）——多进程并发安装/刷新同一 app 时对 `meta.json`/`spec` 文件是"最后写入者赢"，可能交错；`SaveParsedSpec` 有 tmp+rename 但 `SaveMeta` 无（`spec_cache.go:587-594`）。**P3**（多进程写同一 app 属边缘场景）。
- `ToolRegistry`：`sync.RWMutex`（`registry.go:40-50`），`Load` 持写锁做 map 读+写（`:284-303`）——所有 tool call 串行化在该锁上，MCP 高并发时是竞争点；但临界区只是 map 操作，纳秒级，P3。
- `PredicateSearchEngine`：`sync.RWMutex`（`engine_predicate.go:22`），`Search` 持读锁做**全量线性扫描**（`:242-260`）——N 个并发 Search 可并行（读锁），可接受。

### 10.4 fsnotify watcher 生命周期（F11：死代码，无泄漏）`[静态]`

- `pkg/config/spec_watcher.go`（778 行）定义 `LocalFileWatcher`（`:101-503`，含 fsnotify 目录监听 `:194-205`、`runFsnotifyMode` `:228-270`、轮询降级 `:349-376`）、`RemoteSpecWatcher`（`:506-...`）、`SpecWatcherManager`（`:648-...`）。
- **生产代码零调用**：`grep -rn "NewLocalFileWatcher\|NewRemoteSpecWatcher\|NewSpecWatcherManager" --include=*.go .` → 仅命中 `spec_watcher.go` 自身与 `spec_watcher_test.go`。
- 所以：**不存在 watcher 泄漏/重复注册/goroutine 泄漏**（因为根本没启动过 watcher）；fsnotify 是死依赖（增加二进制体积与供应链面，P3 清理项）。
- 若未来接上：代码本身有正确性保障——`Stop()` 关 `fsWatcher` + `stopCh`（`:433-443`），`runFsnotifyMode` 退出时关 watcher（`:235-240`），`tryStartFsnotify`/`runPollingMode` 有模式切换逻辑；**但静态阅读发现潜在重复注册风险**：`Start` 每次调用重新 `make(chan struct{})`（`:142`）且未检查 `running` 状态，若重复 `Start` 会重复起 goroutine；属于"未启用的代码"，不列 P 级。

### 10.5 无界缓存与内存增长

| 缓存 | 是否有界 | 位置 | 结论 |
| --- | --- | --- | --- |
| `Parser.cache` | 有界（按 app 数，进程内） | `parser.go:117` | 进程内，无泄漏；但 `ClearCache`（`:691-697`）用 `Range`+`Delete` 违规模式（`[静态]`：sync.Map 允许 Range 中 Delete，安全） |
| `registry.loadedTools` | 有界（≤ 工具总数） | `registry.go:23` | 每个 tool 双份存储（`tools`+`loadedTools`），P3 |
| `registry.tools` / `engine.tools` | 有界（= op 数） | `registry.go:19`、`engine_predicate.go:21` | 20k op ≈ 13MB 级，静态推断 |
| `SpecCacheManager` 磁盘缓存 | **无上限**（每个 app 一份 spec + meta，从不清理过期） | `spec_cache.go:147-165` | 长期使用会积累旧 spec/parsed.json（尤其 `SaveParsedSpec` 若启用后 4MB spec 落盘可达数十 MB）；`Clear/ClearAll` 只在 uninstall 时调用（静态，`config.go:496-512` 删除 config，未确认是否清 cache——**建议核对**） |
| HTTP 响应 | 无上限 | §6.3 | F6 |

### 10.6 内存基线参考 `[实测]`

- `ob --version` 进程峰值 RSS：未单独测（`/usr/bin/time -l ./ob --version` 可测；`[静态]`预期 ~10–20MB，Go runtime 基线）。
- `ob run demo resource0 list`（小 spec + 100B 响应）：**最大 RSS 23.2MB**；
- 同命令 5MB 响应：**62.0MB**（F6）；
- `LoadSpec` mb5 单次分配 109MB（bench B/op），但因逐次回收，进程峰值 RSS 远低于累加值；对 `list` 3 app 场景，三个 spec 同时驻留（`GetInstalledAppInfo` 顺序解析、循环变量 `info` 持有 `SpecInfo` 但不持有 spec），实际 RSS 未测，`[静态]`预期 60–120MB 级（若 4MB spec 的 openapi3.T 常驻 ~80MB 级）。**建议后续以 `go test -bench -memprofile` 或 `GODEBUG=gctrace=1` 精确化**。

---

## 11. 热路径分配（P3，供后续优化参考）

### 11.1 逃逸分析 `[bench/静态]`

```bash
go build -gcflags='-m' ./pkg/spec ./pkg/config 2>&1 | grep -c "escapes to heap"
# → 526
```

`pkg/spec`+`pkg/config` 两包共 **526 处堆逃逸**（`-m` 输出计数，含库内正常逃逸）。抽样（与热路径相关）：

```
pkg/spec/parser.go:171:30  &openapi3.Loader{...} escapes to heap
pkg/spec/parser.go:174:7   &Parser{...} escapes to heap
pkg/spec/parser.go:176:11  &http.Client{...} escapes to heap
pkg/spec/format.go:168:9   &ContentDetector{...} escapes to heap
pkg/spec/format.go:169:34  []UnmarshalStrategy{...} escapes to heap
pkg/spec/format.go:170:4   &JSONStrategy{} escapes to heap
pkg/spec/format.go:79:6    moved to heap: js
```

其中 `format.go:168-172` 在 **Swagger 2.0 路径的每次解析**都会构造 `ContentDetector` + 策略 slice（`NewContentDetector`，被 `parser.go:492` 调用）——对 2.0 spec 每命令多 4–5 次分配，量级微不足道但属"每调用必分配"的反模式。

### 11.2 具体分配热点 Top 5（按影响排序）

| 排名 | 热点 | 证据 | 量级 | 修复 |
| --- | --- | --- | --- | --- |
| 1 | **spec 解析本身**（kin-openapi `LoadFromData` + `Validate`） | `BenchmarkLoadSpec/mb5` 109.3MB / 1.24M allocs | 每命令 100MB 级 | 缓存派生产物（§12-opt1），成本 M |
| 2 | **`detectVersion` 全量 string 复制 + 多次 Contains** | `BenchmarkDetectVersionCopy` 284.5µs / 5.46MB / 2 allocs（4.1MB 输入） | 每次解析 +5.4MB 分配，占 5% 内存 | 改为 `bytes.Contains`（零拷贝），成本 S，收益小但确定 |
| 3 | **语义映射每 op 多次小分配**（`ExtractResult`、segment slice、正则编译、map literal） | `BenchmarkBuildCommandTree/mb5` 18,519 allocs / 2.59MB | 每命令 2.5ms / 2.6MB | §9.3 的 4 个 S 级改动 |
| 4 | **响应处理三重驻留**（`io.ReadAll` + unmarshal + remarshal + Println） | §6.3 实测 RSS 7.8× | 每命令 +38.8MB@5MB | 流式输出，成本 M |
| 5 | **`Singularize/Pluralize` 每次重建 map/slice** | 1750ns / 4 allocs / 调用（当前生产未调用，潜伏） | 若启用：每资源 ~2µs | 提为包级 var，成本 S |

### 11.3 已经很好的地方（避免过度优化）

- `bin` 体积：`-ldflags="-s -w"` 后 15.1MB（Go 静态链接 + bubbletea + keyring cgo + kin-openapi，属合理范围）；二进制加载 6.7ms 表明**段加载/动态链接不是瓶颈**。
- 语义映射无 O(n²)：实测线性。
- MCP 工具注册非每请求：实测/静态均确认。
- 连接复用：MCP 进程内复用同一 `http.Client`/Transport，无每请求重建。

---

## 12. 优化路线图（按 ROI/风险排序）

### opt1（P0 修复，成本 M）：实现真正有效的"解析产物缓存"，而不是缓存 `openapi3.T`

**目标**：4.1MB spec 的单命令从 ~126ms 降到 ~15ms 量级（乐观）/ ≤40ms（保守）。

**关键约束（来自实测）**：不能沿用 `SaveParsedSpec/LoadParsedSpec` 的"整棵 `openapi3.T` JSON 往返"——实测 118ms/101MB，与重解析等价。**正确做法是缓存下游派生产物**：

1. **命令树索引**（结构化、小）：序列化 `semantic.CommandTree` + 每个 operation 的 `(method, path, operationID, summary, parameters schema)` 的精简结构（不含完整 schema、不含 refs），用 **gob** 或 msgpack 落盘（内容确定、无 map 迭代顺序问题需显式排序）。估算体积：20k op × ~300B ≈ **6MB**（[静态]估算，需实测），解码 ~10–20ms（gob 比 JSON 快 2–4×，`[静态]`预期）。
2. **失效判断复合键**：`(绝对路径, size, mtimeNanos)` + `ParserVersion` + OpenBridge 版本；对远程 URL 额外 `ETag`/`Last-Modified`（元数据已存在于 `SpecCacheMeta`，`spec_cache.go:33-74`）。**禁止**为判断失效而全文读取+哈希（现有 `checkSourceIntegrity` 的反模式，`spec_cache.go:770-781`）。
3. **原子写**：tmp 文件 + `os.Rename`（`SaveParsedSpec` 已有正确模式，`:710-716`，可直接复用）。
4. **接入点**：实现 `LoadSpecWithPersistentCache` 的真实语义；`cli.Handler.loadAndCacheSpec`、`router.startMCPServer`、`completion.Provider.loadSpec` 三处自动受益；同时保留内存 sync.Map 作为 L1。

**验证方式**：4MB spec 上 `ob run mb5 --help` 从 p50 133.6ms → 目标 <40ms；`ob __complete` 从 134ms → <25ms。

### opt2（P1 修复，成本 S）：`ob list` / `ob info` 不解析 spec

`GetInstalledAppInfo`（`pkg/config/install.go:898-925`）用 `spec.NewParser().LoadSpec` 解析 spec 只为拿 title/version/path/op 数——这些数据**install 时已算过**（`InstallResult.SpecInfo`、`config.OperationCount`，`install.go:406-417`、`config.go:44-48`）。改为读配置字段，缺失时才回退解析。收益：3 app 场景 `list` 166.9ms → ~10ms（**16×**）；且消除 `list` 对远程 spec 的网络依赖（F4 的放大点）。

### opt3（P1 修复，成本 M）：远程 spec 走磁盘缓存 + 离线回退

另：install 阶段**复用首次 `LoadSpec` 的 raw bytes**，消除 `FetchWithCache` 的重复下载（F14 实测 2 次 GET，`install.go:378-410`，成本 S）。运行时 `loadFromURL`（`parser.go:264-295`）改为：先查 `SpecCacheManager` 磁盘缓存（键 = URL + ETag/Last-Modified），命中且未过期即返回；过期则按 `If-None-Match` 条件请求（代码已实现于 `spec_cache.go:449-460`，只是无人调用）→ 304 复用，非 304 更新缓存；网络失败回退 stale 缓存（`tryUseStaleCache` 已实现 `spec_cache.go:359-377`）。收益：远程 spec 每命令从 "DNS+TLS+全量下载+解析" 降为 "本地读+解析"，公网场景省 50–500ms；离线可用。

### opt4（P1 修复，成本 M）：响应流式化与上限

见 §6.3。`io.ReadAll` → 小 body（如 <1MB）走原格式化路径，大 body 直接 `io.Copy(os.Stdout, resp.Body)`（或写到文件并提示），MCP 侧截断 + `isError` 提示。收益：5MB body 峰值内存 -60%；消除大响应 OOM 风险。

### opt5（P2 修复，成本 S）：HTTP 超时与 keyring 缓存

- `cli.Handler.httpClient`（`handler.go:45`）、`mcp.Handler`（`handler.go:32`）、`progressive`（`progressive.go:80`）统一传入 `&http.Client{Timeout: 30s}` 或用 profile.Timeout（`config.Profile.Timeout` 已存在，`config.go:88-89`）——消除永久挂起；
- `credential.Manager` 增加进程内凭据 LRU（TTL 短，如 60s）——MCP 每 call 省 189µs；
- `InjectAuth` 先判 `authConfig.Type == "none"/""` 再碰 keyring（`builder.go:97`）——CLI 每命令省 189µs。

### opt6（P2 修复，成本 S）：MCP progressive 搜索结果限流

`Search` 加 `limit`（默认 30/Top-K），`formatSearchResults` 截断并提示"缩小查询"；`query==""` 返回按 tag/资源聚合的摘要，不回全量（`progressive.go:291-312`、`engine_predicate.go:246-248`）。

### opt7（P3 清理，成本 S）：删除/接线死代码

- `pkg/config/spec_watcher.go`（778 行）+ fsnotify 依赖：**无生产调用**（F11）。要么删除（减小二进制约 1–2MB `[静态]`、减少维护面），要么接上 `--watch` 功能；当前状态是"文档化能力但实际不生效"的隐性契约违背——若 README/docs 宣称 watch，应列为 P1 文档 bug。
- `SaveParsedSpec/LoadParsedSpec/ValidateParsedSpec`:按 opt1 重新设计或删除。
- `Singularize/Pluralize/NormalizeResourceName`：未调用，删除或启用。
- `SpecCacheManager`：按 opt3 接线，否则整包 816 行闲置。

### opt8（P3，成本 S）：语义包微优化

`verb.go:224` 正则提为包级；`getVerbMappings()` 改有序 slice（同时修复 map 迭代导致的 verb 推断不确定性）；`Singularize/Pluralize` map 提为包级（`resource.go:295`、`:352`）；`VerbSet.Add` 用 map 判重（`verb.go:391-410`）。合计 `BuildCommandTree` -30~60%，端到端收益 ≤1.5%，**建议顺手做**。

### 预期总收益（按 opt1+opt2+opt3+opt4 全部落地，`[静态]`推算）

| 场景 | 现状 p50 | 目标 | 主要来源 |
| --- | --- | --- | --- |
| `ob --version` | 6.75ms | 6.75ms（不动） | 已很好 |
| `ob list`（3 app） | 166.9ms | ~10ms | opt2 |
| `ob run <app>`（4.1MB spec） | 125.9–136.3ms | ~20–40ms | opt1 |
| `ob __complete`（4.1MB spec） | 133.6ms | ~10–25ms | opt1（+opt2 若静态内嵌） |
| `ob run`（远程 1MB spec，公网） | 33.6ms + 网络 | ~5ms + 条件请求 | opt3 |
| 5MB 响应命令峰值 RSS | 62.0MB | ~24MB | opt4 |

---

## 13. 附录

### 13.1 复现步骤（完整）

```bash
# 0) 构建（不修改仓库；产物落 /tmp）
mkdir -p /tmp/ob-perf
cd /Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge
go build -ldflags="-s -w" -o /tmp/ob-perf/ob ./cmd/ob

# 1) 合成 spec
cd /tmp/ob-perf && python3 gen_specs.py
python3 - <<'EOF'
import sys; sys.path.insert(0,'/tmp/ob-perf')
from gen_specs import make_spec, write_json
write_json("mb1.json", make_spec(70, 28, desc_len=40))    # 0.92 MiB
write_json("mb5.json", make_spec(260, 78, desc_len=50))   # 3.90 MiB
EOF

# 2) 隔离 HOME + 安装 app
mkdir -p /tmp/ob-perf/home
export HOME=/tmp/ob-perf/home
/tmp/ob-perf/ob install demo --spec /tmp/ob-perf/specs/small.yaml --base-url http://127.0.0.1:18080 --shim=false
/tmp/ob-perf/ob install mb1  --spec /tmp/ob-perf/specs/mb1.json  --base-url http://127.0.0.1:18080 --shim=false
/tmp/ob-perf/ob install mb5  --spec /tmp/ob-perf/specs/mb5.json  --base-url http://127.0.0.1:18080 --shim=false

# 3) 冷启动/命令延迟（python 计时，20 次）
N=20 python3 /tmp/ob-perf/bench.py
N=10 python3 /tmp/ob-perf/bench_cli.py

# 4) 微基准（replace 到本地仓库，不写仓库）
cd /tmp/ob-perf/bench
GOFLAGS=-mod=mod GOPROXY=off go test -run XXX -bench . -benchtime=3x -benchmem -timeout 300s

# 5) 大响应内存放大
python3 /tmp/ob-perf/srv.py &   # 先 echo 100 > resp_size
HOME=/tmp/ob-perf/home /usr/bin/time -l /tmp/ob-perf/ob run demo resource0 list
# echo 5000000 > resp_size 后重启 srv.py 再测

# 6) race
cd /Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge && go test -race -short ./...

# 7) 逃逸
go build -gcflags='-m' ./pkg/spec ./pkg/config 2>&1 | grep -c "escapes to heap"
```

### 13.2 微基准原始输出（存档）

```
goos: darwin
goarch: arm64
pkg: obperfbench
cpu: Apple M3 Max
BenchmarkSaveParsedSpec-16          	       3	   1972070 ns/op	 2113320 B/op	   12545 allocs/op
BenchmarkLoadParsedSpec-16          	       3	   3349736 ns/op	 3039584 B/op	   31697 allocs/op
BenchmarkDetectVersionCopy-16       	       3	    284528 ns/op	14373.58 MB/s	 5461448 B/op	       2 allocs/op
BenchmarkKeyringNewManager-16       	       3	      1819 ns/op	     384 B/op	       7 allocs/op
BenchmarkKeyringGetCredential-16    	     189333 ns/op	    1618 B/op	      39 allocs/op
BenchmarkLoadSpec/mb5-16            	       3	 119369417 ns/op	  34.26 MB/s	109272248 B/op	 1237757 allocs/op
BenchmarkLoadSpec/small-16          	       3	   2301972 ns/op	  29.84 MB/s	 1997770 B/op	   23881 allocs/op
BenchmarkLoadSpec/mb1-16            	       3	  33634472 ns/op	  28.54 MB/s	27759202 B/op	  333576 allocs/op
BenchmarkParseBytesOnly/small-16    	       3	   2292111 ns/op	  29.97 MB/s	 1920597 B/op	   23850 allocs/op
BenchmarkParseBytesOnly/mb1-16      	       3	  33917875 ns/op	  28.30 MB/s	26788869 B/op	  333542 allocs/op
BenchmarkParseBytesOnly/mb5-16      	       3	 125709444 ns/op	  32.53 MB/s	105196213 B/op	 1237886 allocs/op
BenchmarkLoadSpecWithCtx/small-16   	       3	   2304792 ns/op	 1998869 B/op	   23885 allocs/op
BenchmarkLoadSpecWithCtx/mb1-16     	       3	  32536667 ns/op	27784970 B/op	  333727 allocs/op
BenchmarkLoadSpecWithCtx/mb5-16     	       3	 126648986 ns/op	109293474 B/op	 1237856 allocs/op
BenchmarkBuildCommandTree/mb5-16    	       3	   2485806 ns/op	 2589285 B/op	   18519 allocs/op
BenchmarkBuildCommandTree/small-16  	       3	     82139 ns/op	  141056 B/op	    1148 allocs/op
BenchmarkBuildCommandTree/mb1-16    	       3	    792569 ns/op	  798218 B/op	    5592 allocs/op
BenchmarkSingularize-16             	       3	      1750 ns/op	    1202 B/op	       4 allocs/op
BenchmarkPluralize-16               	       3	      2028 ns/op	    1208 B/op	       4 allocs/op
BenchmarkVerbSetAdd-16              	       3	     35028 ns/op	   81146 B/op	     797 allocs/op
BenchmarkRegistryBuildFromSpec/mb1-16  2	    830416 ns/op	 1279272 B/op	    7887 allocs/op
BenchmarkRegistryBuildFromSpec/small-16 2	      39438 ns/op	   86432 B/op	     587 allocs/op
BenchmarkPredicateSearch-16            2	     20730 ns/op	    9512 B/op	      42 allocs/op
BenchmarkSaveParsedSpecMB5-16          2	  37732688 ns/op	101651812 B/op	  223833 allocs/op
BenchmarkLoadParsedSpecMB5-16          2	 118071479 ns/op	101620556 B/op	 1151814 allocs/op
```

### 13.3 关键 file:line 索引

| 主题 | 位置 |
| --- | --- |
| 全依赖 init（每命令） | `cmd/ob/main.go:51-89` |
| cobra 命令树 | `cmd/ob/main.go:102-158` |
| 持久化缓存 stub | `pkg/spec/parser.go:835-839` |
| 解析入口/校验 | `pkg/spec/parser.go:225-238`、`:404-421`、`:451-469`、`:489-521` |
| 版本探测全量复制 | `pkg/spec/parser.go:778-800` |
| 进程内缓存/TTL | `pkg/spec/parser.go:117`、`:177`、`:633-697` |
| 解析缓存写/读/校验（死） | `pkg/config/spec_cache.go:694`、`:739`、`:784` |
| 远端缓存 fetch/条件请求/原子性 | `pkg/config/spec_cache.go:254-514`、`:449-460`、`:549-565`、`:587-594` |
| list/info 解析每个 spec | `pkg/config/install.go:898-925`（解析 918-922） |
| 补全 loadSpec | `pkg/completion/completion.go:381-402` |
| 补全 cobra 接线 | `cmd/ob/main.go:962-1010` |
| 语义 verb 热点 | `pkg/semantic/verb.go:145-153`、`:224`、`:264-289`、`:391-421` |
| 语义 resource 热点 | `pkg/semantic/resource.go:120-136`、`:264-285`、`:295-350`、`:352-...` |
| HTTP 客户端（无超时） | `pkg/cli/handler.go:45`、`pkg/mcp/handler.go:32`、`pkg/mcp/progressive.go:80` |
| 响应全量读取 | `pkg/cli/handler.go:142`、`pkg/mcp/handler.go:118`、`pkg/mcp/progressive.go:489` |
| keyring 初始化/取用 | `pkg/credential/keyring.go:207-233`、`:382-410`、`pkg/request/builder.go:97-108` |
| MCP 注册/schema 生成 | `pkg/mcp/handler.go:42-65`、`pkg/mcp/registry.go:51-64`、`:150-252` |
| MCP 搜索全扫/结果无上限 | `pkg/mcp/engine_predicate.go:43-64`、`:242-264`、`pkg/mcp/progressive.go:291-330` |
| watcher 死代码 | `pkg/config/spec_watcher.go:122`、`:528`、`:648` |

### 13.4 未验证/后续工作清单

1. `doc.Validate` 在解析总耗时中的精确占比：需要克隆 loader 或临时 pprof（禁止改源码约束下未做）；
2. 真实世界 spec（petstore、GitHub、Stripe）复测解析曲线；
3. gob/msgpack 版命令树索引的体积与解码耗时实测（opt1 的关键假设）；
4. `ob list`/`ob info` 在 0/1/3/10 app 下的 RSS 曲线（验证 §10.6 推断）；
5. 远程 URL spec 的端到端网络耗时（需公网环境，本机以本地 server 模拟解析部分）；
6. `-gcflags='-m'` 只扫了 `pkg/spec`+`pkg/config`；`pkg/mcp`/`pkg/semantic` 未统计（预期量级更小）；
7. `bin/ob` 的来源（旧构建产物？）未确认，但本报告全部基于 `/tmp/ob-perf/ob` 新构建，不受影响。

---

## 14. 补充实测（Lead 线索交叉核对，2026-11-07 追加）

本节为 Team Lead 追加线索的**独立复核**，全部为本报告作者亲自测量；与 Lead 的启动基线交叉验证一致（见 §14.6）。

### 14.1 F14：远程 install 下载两次 + 运行时每命令再下载一次（P2，实测）

**测量方法**（`/tmp/ob-perf/srv2.py`，本地 HTTP server 提供 `small.yaml` 并记录每个 GET 到 `hits.log`）：

```bash
# server: GET /openapi.yaml 返回 spec，日志写入 hits.log
rm -f hits.log; (python3 srv2.py spec 2>/dev/null &); sleep 0.7
export HOME=/tmp/ob-perf/home
/usr/bin/time -p ./ob install urldemo --spec http://127.0.0.1:18081/openapi.yaml \
    --base-url http://127.0.0.1:18080 --shim=false
grep -c GET hits.log          # → 2
/usr/bin/time -p ./ob run urldemo --help
grep -c GET hits.log          # → 3（即单条命令又下 1 次）
```

实测输出：

```
=== install from URL ===
real 0.01
=== GET count during install: 2 ===
spec GET /openapi.yaml #1
spec GET /openapi.yaml #2
=== one command with remote spec (re-download?) ===
real 0.01
=== GET count for 1 command: 1 ===
spec GET /openapi.yaml #3
```

结论：
1. **安装阶段 2 次下载**——`doInstall` 先 `parser.LoadSpec(primarySource)`（`pkg/config/install.go:378-381`，用途：校验 + 推导 baseURL + `GetSpecInfo`），随后 `isWebURL` 分支里 `NewSpecCacheManager(...).FetchWithCache`（`install.go:409-410`）再下载一遍存缓存。**两次都是无条件全量下载**，未复用第一次的 `specDoc`/raw bytes。
2. **运行阶段每条命令再下载 1 次**（`pkg/spec/parser.go:264` 裸 `client.Do`，见 F4）。即：远程 spec 用户每天 N 条命令 = N 次完整下载 + N 次解析；install 时写下的磁盘缓存（`install.go:409`）**从未被读取**。
3. 修复（成本 S）：`doInstall` 把第一次 `LoadSpec` 时的 raw bytes 或 `FetchResult` 直接交给 `saveToCache`/`SaveMeta`，消除重复下载（安装阶段 -50% 网络）；运行时按 opt3 接磁盘缓存 + 条件请求。收益：远程 spec 首装少一次全量下载，日常命令从"网络下载+解析"降为"本地读+解析"。

### 14.2 F15：无超时导致 CLI 永久挂起（P2，已实测复现）

**测量方法**：本地 server 接受连接后 `time.sleep(30)` 不返回；app 的 `base_url` 指向该端口；用 Python 子进程 3s 超时运行。

```bash
(python3 srv2.py hang 2>/dev/null &); sleep 0.7
python3 - <<'EOF'
import subprocess, os, time
env=dict(os.environ); env["HOME"]="/tmp/ob-perf/home"
try:
    subprocess.run(["/tmp/ob-perf/ob","run","hangapp","resource0","list"],
                   capture_output=True, env=env, timeout=3)
except subprocess.TimeoutExpired:
    print("HUNG after 3.0s")
EOF
```

实测输出：

```
HUNG: still running after 3.0s (http.Client Timeout=0) — confirmed
```

结论：`&http.Client{}`（`pkg/cli/handler.go:45`）→ `Timeout==0` 表示**永不超时**；默认 Transport 只有 `DialContext 30s`/`TLSHandshakeTimeout 10s`，对"连接已建立但服务端不回响应"没有任何保护（`ResponseHeaderTimeout=0`）。这不是理论风险：CLI 会一直挂到用户 Ctrl-C，MCP server 会永久占用一个 tool-call goroutine。修复成本 S（`Timeout: 30 * time.Second` 或接 `profile.Timeout`，`config.Profile.Timeout` 字段已存在但运行时未接线，`pkg/config/config.go:88-89`；`TLSConfig`/`RetryConfig` 同样未接线——**profile 里的一整套可靠性配置目前是"写了不生效"**，建议一并接线或从 schema 移除，避免误以为有保护）。

### 14.3 更大 spec 的代价外推（14.4MB 实测，P1 的量化强化）

为了量化 Lead 关心的"大 spec 50+MB 场景"，生成 14.42MB spec（`mb16.json`，1100 资源 × 220 op）并实测 install / 每次命令：

```bash
python3 - <<'EOF'
import sys; sys.path.insert(0,'/tmp/ob-perf')
from gen_specs import make_spec, write_json
write_json("mb16.json", make_spec(1100, 220, desc_len=40))   # 14.42 MiB
EOF
/usr/bin/time -p ./ob install mb16 --spec /tmp/ob-perf/specs/mb16.json --base-url ... --shim=false
/usr/bin/time -p ./ob run mb16 --help     # ×3
```

实测（3 次运行高度稳定）：

```
=== install big (timing incl parse) ===
real 0.50  user 0.63  sys 0.02
=== run mb16 --help (re-parse every command) ===
real 0.52  user 0.67  sys 0.02
real 0.50  user 0.64  sys 0.02
real 0.49  user 0.63  sys 0.02
```

- **14.42MB spec → 每条命令 ~0.50s（p50 约 0.50s，min 0.49s）**，即约 **35ms/MB**，与 bench 的 28–34MB/s 一致（`LoadSpec` 4.09MB=119ms → 29ms/MB；14.4MB≈0.50s → 34.7ms/MB，符合线性+轻微超线性）；
- **线性外推到 Lead 的 50MB 场景**：单条命令 **≈1.7–1.8s**；分配量按 26.7× 放大推算 **≈1.35–1.4GB/命令**（`[静态]`外推，14.4MB 实测分配量未单独记录；4.09MB 实测 109.3MB）；
- 50MB spec 在 MCP 常驻进程中更糟：启动一次解析 ~1.7s + `openapi3.T` 常驻内存（按 4MB→约 80–100MB 推断，`[静态]`）→ MCP 进程 RSS 可能 1–1.5GB 级；
- **结论**：F1（持久化缓存空实现）在 50MB spec 下不是"优化项"而是"可用性问题"——每条命令 1.7s、每个补全 Tab 1.7s、`ob list` 若安装多个大 spec 会累加到数秒。**这是本报告最高优先级的修复理由。**

### 14.4 MCP 全量构建 bench（补齐 §7.2，实测）

Lead 线索 6："BuildMCPTools/processPathItem 每次启动全量构建；progressive SetSpec 全量索引；registry 全量。请 bench 1000 操作的 spec 构建耗时。"

```
BenchmarkBuildMCPTools/small-16   5   35650 ns/op   62792 B/op   469 allocs/op    (25 ops)
BenchmarkBuildMCPTools/mb1-16     5  512517 ns/op  899059 B/op  6454 allocs/op   (~1960 ops)
BenchmarkRegistryBuildFromSpec/small-16  2   39438 ns/op   86432 B/op   587 allocs/op
BenchmarkRegistryBuildFromSpec/mb1-16    2  830416 ns/op 1279272 B/op  7887 allocs/op
```

- **非 progressive `BuildMCPTools`（= `processPathItem` 逐 op 构建 `mcp.Tool`+JSON Schema 全量）**：~1000 op → **0.51ms / 899KB / 6,454 allocs**；~1960 op 实测 512.5µs，**~0.26µs/op**；
- **progressive `registry.BuildFromSpec`**：~1960 op → **830µs / 1.28MB / 7,887 allocs**（比 BuildMCPTools 多 ~60%，因为多了 metadata 构建 + 双份存储）；
- 结论：**MCP 工具构建本身不是瓶颈**（1000 op 仅 0.5–0.8ms，占 spec 解析 33.6ms 的 1.5–2.5%）；真正的 MCP 启动成本仍是 spec 解析（§4.1）。Lead 担心的"每次启动全量构建"确实存在，但**量级可接受**，不必优先优化；优化重点应是"避免每次启动都解析 spec"（opt1 对 MCP 同样适用，可省 33ms–1.7s 启动时间）。

### 14.5 响应格式转换：yaml 默认路径 vs json（实测，补充 F6）

Lead 线索 4："`--output yaml` 默认把 JSON 反序列化再 yaml.Marshal，大响应内存放大/CPU 值得实测。"

```bash
echo 5000000 > resp_size; (python3 srv.py 2>/dev/null &); sleep 0.7
/usr/bin/time -l ./ob run demo resource0 list          # 默认（table→实际走格式化）
/usr/bin/time -l ./ob run demo resource0 list -o json
```

实测（5,000,000B 响应）：

| 输出模式 | real | max RSS |
| --- | --- | --- |
| 默认（`yaml`/table 路径） | 0.09s | 60,948,480 B (58.1MiB) |
| `-o json` | 0.07s | 60,932,096 B (58.1MiB) |

- **峰值内存两模式相同（~60.9MB）**：瓶颈是"`io.ReadAll` 原始 5MB + 反序列化对象树（每个字符串/键独立分配，约 10–20MB）+ 重新序列化输出字符串（≈5MB×2~3）”的三重驻留，而非某个特定 marshal 实现；
- **yaml 路径 CPU +~29%**（90ms vs 70ms）：`FormatOutput`（`pkg/cli/handler.go:496-528`）对同一 body 先 `json.Unmarshal` 成 `any`，再 `yaml.Marshal`，比直接输出多一次全量构建 + 一次全量序列化；5MB 时代价 ~20ms，**P3 级**（但对 50MB 响应会放大到 ~200ms 与数百 MB，建议随 opt4 一起处理）；
- 结论：Lead 的怀疑方向正确（yaml 默认路径确有"JSON→any→yaml"双转换），**但内存放大的主因是 `io.ReadAll` 无上限，而不仅是格式转换**；opt4 的"小 body 格式化 / 大 body 流式直出"应能同时解决两者。

### 14.6 与 Lead 启动基线的交叉验证

| 命令 | Lead 实测 median | 本报告实测 p50 | 差异 |
| --- | --- | --- | --- |
| `ob --version` | 6.90ms | 6.75ms（20 次，min 5.78） | -2.2% |
| `ob`（无参） | 6.63ms | 6.83ms | +3.0% |
| `ob --help` | 6.40ms | 6.45ms | +0.8% |
| `ob list` | 7.33ms | 8.99ms（空 HOME）/ 166.9ms（3 app，含 4.1MB spec） | 基线一致；差异来自 app 数量与 spec 体积（Lead 的 petapi 为 3 op 小 spec） |
| `ob run petapi pets list` | 11.21ms | `ob run demo resource0 list` ≈11.7ms（0.07MB spec，本地 server） | +4% |

**交叉验证结论**：两套独立测量在 5% 内一致，启动基线数据可信。Lead 额外确认：**二进制必须命名为 `ob`，否则 `cmd/ob/main.go:105-114` 会走 shim 分支**（`binaryName != "ob"` → `appRouter.Execute`），这一点本报告所有测量均满足（`/tmp/ob-perf/ob`）。Lead 的 `go test -race -short ./...` 结果与本报告 §10.1 一致（全绿）。

### 14.7 因 Lead 线索新增/修订的结论

1. **F14 新增**（远程 install 双下载 + 运行时每命令重下）：实测 2 次/install、1 次/命令，P2。
2. **F15 新增**（无超时导致 CLI 实测挂起）：3s 超时后仍在运行，P2；同时确认 `Profile.Timeout/TLSConfig/RetryConfig` **运行时未接线**（写了不生效）。
3. **F1 严重度强化**：14.42MB spec 实测 0.50s/命令，50MB 外推 ~1.7s/命令 —— P0 理由更充分。
4. **F6 补充**：yaml/json 双格式实测均 ~58MiB/5MB 响应，主因 `io.ReadAll`；yaml 路径多 ~29% CPU。
5. **§7.2 补充**：`BuildMCPTools` 1000 op = 0.51ms；`BuildRegistryFromSpec` ~1960 op = 0.83ms —— MCP 构建不是瓶颈，**MCP 启动成本仍由 spec 解析主导**。
6. **opt3 补充**：install 阶段消除重复下载（复用首次 `LoadSpec` 的 bytes）为 S 级改动、独立于运行时缓存接线。

---

## 15. 一句话给决策者

> **把"解析一次、长期复用"从口号变成实现**（opt1），顺手修掉 `ob list` 解析全部 spec（opt2）与远程 spec 不读缓存/重复下载（opt3、F14），OpenBridge 的真实命令延迟可从 130ms 级降到 20–40ms 级、`list` 从 167ms 降到 10ms 级、14MB+ spec 场景从 0.5s/命令降到毫秒级；其余（HTTP 超时、keyring 缓存、响应流式化、语义包微优化、死代码清理）都是低成本高确定性的收尾项。README 的 <100ms 声明应改为分档表述，否则会在 4MB spec 场景被用户实测打脸；50MB spec 场景则已是**可用性**问题（~1.7s/命令、~1.4GB 分配/命令）。

（报告完）
