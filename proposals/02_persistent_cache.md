# 提案 02：OpenBridge 持久化派生索引（Persistent Derived Index）

| 项 | 值 |
| --- | --- |
| 状态 | Spike 完成，待批次 2 评审 |
| 关联 | W5 / 契约 C-B5；诊断 `D-05`、performance.md `F1/F2/F5`；批次 1 W1（远端内容缓存，C-B1） |
| 测量快照 | 仓库 HEAD `954a28e` + 批次 1 进行中的工作树（W1 已改 `pkg/spec/parser.go` 远端 URL 路径；**本地文件解析路径未变**） |
| 测量环境 | `go1.27.3 darwin/arm64`，16 vCPU；`github.com/getkin/kin-openapi v0.133.0` |
| 基准工程 | `/tmp/cache-spike/`（`go.mod` 用 `replace` 指向仓库，只 import 仓库导出 API，不改仓库源码） |
| 原始数据 | `/tmp/cache-spike/results.txt`、`/tmp/cache-spike/results2.txt`（本报告数字均来自这两份输出） |
| 结论 | **推荐 b) 派生索引（gob DTO）分层落地**；否决 a) 完整 `openapi3.T` 作为"解析加速器"（JSON 往返 ≈ 重解析，gob 往返会静默丢数据）；c) 解析微优化收益 ≤5%，仅作附带清理 |

---

## 1. 问题陈述

当前每个 CLI 进程都会重新解析一次 OpenAPI spec：

```
ob <app> <res> <verb>        → cli.Handler.loadAndCacheSpec
ob <app> --help              → showAppHelp → spec.Parser.LoadSpec
shell completion（每次 Tab）  → completion.Provider.loadSpec
ob run <app> ... --mcp       → router.startMCPServer
```

- `pkg/spec/parser.go:835-839` 的 `LoadSpecWithPersistentCache` 是空壳（忽略 `appName`，直接转发 `LoadSpec`）；
- 唯一的持久化"解析缓存"实现 `config.SpecCacheManager.SaveParsedSpec/LoadParsedSpec`（整棵 `openapi3.T` JSON 落盘/读回）在生产代码**零调用**；
- performance.md F1/F2 已测：4.09MB spec 每条命令 119.4ms / 109.3MB / 1.24M allocs；而死代码 `LoadParsedSpec` 118.1ms / 101.6MB ≈ 重解析，**"接上开关"也不会更快**。

因此本 spike 要回答：**要不要缓存、缓存什么、怎么缓存**，并给出可落地的键/失效/原子写/接入点设计。目标不是让单次解析更快（kin-openapi 内部成本不可控），而是**把"每次都要解析"变成"每次只读一个小的派生结构"**。

---

## 2. 实验设计

### 2.1 合成 spec（脚本 `/tmp/cache-spike/gen_specs.py`）

| 名称 | 文件体积 | operations | paths | schemas | 说明 |
| --- | --- | --- | --- | --- | --- |
| `small_007mb` | 77,073 B（0.07MB） | 50 | 10 | 1 | 小档，含 3 params/op、requestBody、`$ref` response |
| `mb1` | 1,064,956 B（1MB） | 700 | 140 | 1 | 中档，同结构线性放大 |
| `mb4` | 4,202,366 B（4MB） | 2,600 | 520 | 65 | 大档，260 资源 × 2 paths × 5 methods + 64 个 padding schema |
| `mb4_dense`（补充压力档） | 6,672,262 B（6.7MB） | 8,320 | 4,160 | 1 | 高 operation 密度，用于验证"解析成本 ∝ operation 数" |

> 与 performance.md 的 4.09MB/20,280 ops 合成 spec 结构不同（本 spike 的 operation 更重：每 op 3 个参数 + requestBody + response `$ref`），所以**本报告的绝对 ms 不能与 F1/F2 的 119ms 直接对齐**；报告只使用本 spike 内部同一 spec 的方案间对比，并在 §3.1 用"同一 spec 基线"做归一化。另：W1 已修的"远端 URL 每次重下"不在本 spike 范围（本 spike 全部用本地文件，聚焦解析/派生）。

### 2.2 方法

- 基准工程 `/tmp/cache-spike/main.go` 用 `testing.Benchmark` 编程式跑基准（`ns/op`、`B/op`、`allocs/op`），大对象用 `-benchtime` 默认 1s 自动校准（4MB 档约 4–8 次迭代）；另用 `stageProfile`（7 轮交错采样取中位数）单独拆分 `read / detectVersion / LoadFromData / Validate / noExternalRefs` 各阶段占比，避免不同 `Benchmark` 之间的 GC 偏差。
- 运行方式（模块缓存已命中，可离线）：

```bash
cd /tmp/cache-spike
GOFLAGS=-mod=mod GOPROXY=off go build -o spike .
GOFLAGS=-mod=mod GOPROXY=off ./spike specs/small_007mb.json specs/mb1.json specs/mb4.json specs/mb4_dense.json
```

- 噪声：单次 `testing.Benchmark` 的重活（4MB parse）多次运行波动约 ±5–15%；跨方案比较的方向性不受影响（差距是 10×–200× 级）。`B/op` 用于判断 GC 压力，不进收益结论。

---

## 3. 实测结果

### 3.1 基线：重解析成本（同一 spec 基准）

| spec | bytes | ops | `LoadSpec` ms/op | B/op | allocs/op | 阶段剖面 `loadValidate` 中位数 |
| --- | --- | --- | ---: | ---: | ---: | ---: |
| small_007mb | 77,073 | 50 | **6.14** | 3.60 MB | 58,699 | 5.14 ms |
| mb1 | 1,064,956 | 700 | **71.67** | 48.95 MB | 807,316 | 67.66 ms |
| mb4 | 4,202,366 | 2,600 | **256.16** | 190.46 MB | 3,121,474 | 264.59 ms |
| mb4_dense | 6,672,262 | 8,320 | **442.14** | 350.18 MB | 5,493,367 | 445.20 ms |

结论与 F2 一致：**耗时/分配与 operation 数近似线性，分配放大 ~25–45×（相对于文件体积）**。4MB 档每次命令 ≈ 256ms + 进程启动 ≈ 6.5ms，即约 262ms 是"重复解析税"。

### 3.2 方案 a：缓存完整 `openapi3.T`

#### a-1 JSON（= 现有 `SaveParsedSpec/LoadParsedSpec` 的做法）

| spec | MarshalIndent ms | 落盘体积 | Unmarshal ms | 现有 `SaveParsedSpec` ms | 现有 `LoadParsedSpec` ms | Load / 重解析 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| small | 1.18 | 72,402 B（93.9% 源） | 5.46 | 1.49 | 4.76 | 0.78× |
| mb1 | 16.08 | 999,835 B（93.9%） | 63.48 | 16.80 | 62.67 | 0.87× |
| mb4 | 57.58 | 3,960,545 B（94.2%） | 232.09 | 59.72 | **232.72** | **0.91×** |
| mb4_dense | 101.20 | 6,414,342 B（96.1%） | 386.16 | 102.23 | **388.70** | **0.88×** |

**否决理由 1（速度）**：JSON 往返只节省 9–13%，与 F1 的 118ms≈119ms 结论一致。原因：`json.Unmarshal` 仍要重建同等规模的 map/slice/引用，成本与 `kin-openapi` 解析同阶。

**否决理由 2（正确性）**：`json.Unmarshal` 不会重新解析 `$ref`。实测 **`$ref` 全部未解析**：

| spec | 请求/响应 body `$ref` 总数 | roundtrip 后 Value==nil | 未解析率 |
| --- | ---: | ---: | ---: |
| small | 80 | 80 | 100% |
| mb4 | 4,160 | 4,160 | 100% |

即："把 `parsed.json` 读回来"得到的 doc 不能直接用于执行路径（`request.Builder` 需要参数/请求体 schema，response 解析也需要 resolved ref）。要恢复引用只能再用 `loader.LoadFromData` 重解析——那就等于回到 3.1 的基线。

> `pkg/config` 中 `SaveParsedSpec/LoadParsedSpec/ValidateParsedSpec` 及 `SpecCacheMeta.ParsedSpecPath/ParsedAt` 应作为**死代码删除**（批次 2 清理项），不要接到运行时。

#### a-2 gob（对完整 `openapi3.T` 直接 gob）

| spec | gob 体积 | Encode ms | Decode ms | decode 后可用性 | 说明 |
| --- | ---: | ---: | ---: | --- | --- |
| small | 4,834 B | 0.05 | 0.12 | ❌ 空命令树，params=0 | `Paths` 丢失 |
| mb1 | 4,832 B | 0.05 | 0.12 | ❌ 空命令树 | 同上 |
| mb4 | 155,169 B | 0.62 | 0.69 | ❌ 空命令树，params=0 | 同上 |
| mb4_dense | 4,719 B | 0.05 | 0.11 | ❌ 空命令树 | 同上 |

**根因（已确认）**：`kin-openapi v0.133` 的 `openapi3.Paths` 内部是**未导出 map**，gob 只编码导出字段且**不报错**——于是编码"成功"、解码"成功"，但路径全部丢失（4.8KB 的"完整 spec"就是证据）。这是一个**静默数据损坏**陷阱：任何"对第三方结构体直接 gob"的方案都必须先验证 roundtrip 后的可用性，不能只看 `err == nil`。

> 因此 a) 两条路都否决：JSON 慢且引用丢失；gob 快但丢数据（且第三方库升级/字段可见性变化随时再坏）。

### 3.3 方案 b：缓存派生数据（推荐方向）

派生数据两种形态都实测：

- **`b-tree`**：直接 gob `*semantic.CommandTree`（`pkg/semantic/mapper.go:25-49`）。它只有导出字段（`RootResources/Name/Description/Operations/SubResources/Parent`），未导出的 `verbSet` 在构建完成后不再使用，`Parent` 当前恒为 nil，因此可 gob；但**依赖"未导出字段恰好无用"这一隐式前提**，不推荐长期使用。
- **`b-index`**：显式 DTO `CachedIndex` = 扁平资源表 + 操作目录（`resource/verb/method/path/operationId/summary` + 精简参数 `{name,in,required,type,format,enum}` + `hasBody`），全部导出字段、带 `SchemaVersion`。这是推荐形态。

| spec | b-tree gob 体积 | b-tree Encode/Decode ms | b-index gob 体积 | b-index Encode/Decode ms | b-index Build ms |
| --- | ---: | ---: | ---: | ---: | ---: |
| small | 9,215 B（12.0% 源） | 0.018 / 0.031 | 10,030 B（13.0%） | 0.030 / 0.044 | 0.015 |
| mb1 | 125,661 B（11.8%） | 0.181 / 0.267 | 133,872 B（12.6%） | 0.449 / 0.434 | 0.226 |
| mb4 | 469,533 B（11.2%） | 0.616 / 0.915 | 499,404 B（11.9%） | 1.049 / 1.356 | 0.992 |
| mb4_dense | 852,063 B（12.8%） | 2.280 / 3.928 | 1,168,466 B（17.5%） | 2.307 / 3.617 | 5.529 |

解码后的可用性已验证：`b-tree` 解码得 `resources/ops` 完整、可遍历；`b-index` 解码得 8,320 ops / 16,640 params 可遍历。分配量对比：mb4 `b-index` 解码 2.49MB B/op vs 基线解析 190.46MB B/op（**76× 更少**）。

#### 帮助/补全端到端（冷解析 vs 温缓存）

| spec | `help.cold`（read+parse+tree+walk） | `help.warmTreeFile`（read+decode+walk） | `help.warmIndexFile` | 加速比（index） |
| --- | ---: | ---: | ---: | ---: |
| small | 5.08 ms | 0.045 ms | 0.059 ms | **86×** |
| mb1 | 70.84 ms | 0.354 ms | 0.489 ms | **145×** |
| mb4 | 262.73 ms | 0.974 ms | 1.464 ms | **179×** |
| mb4_dense | 467.65 ms | 4.258 ms | 3.773 ms | **124×** |

> `warm*File` 已包含从磁盘读 cache 文件 + gob 解码 + 遍历渲染的成本；mb4_dense 的 index 文件（1.17MB）读+解码约 3.8ms，仍比解析快两个数量级。

#### 完整执行路径（对照 a-JSON 是否值得）

| spec | `exec.cold`（parse+tree+lookup） | `exec.warmFullTJSON`（read+unmarshal+tree+lookup） | 节省 |
| --- | ---: | ---: | ---: |
| small | 5.24 ms | 4.93 ms | 6% |
| mb1 | 67.48 ms | 60.81 ms | 10% |
| mb4 | 268.67 ms | 240.47 ms | 10.5% |
| mb4_dense | 463.49 ms | 418.45 ms | 9.7% |

→ 完整 T 的 JSON 缓存对执行路径也只有 ~10% 收益，**不值得为它维护磁盘格式与失效逻辑**。执行路径的真正解法见 §7（阶段 P4：派生索引扩展为 execution view，需 handler/request-builder 改造）。

### 3.4 方案 c：解析优化（各阶段占比）

`stageProfile` 7 轮交错中位数（单位 ms）：

| spec | read | detectVersion（复制整串 + 多次 Contains） | LoadFromData（无 Validate） | LoadFromData+Validate | Validate 增量 | 占比（Validate/总） |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| small | 0.086 | 0.026 | 5.06 | 5.14 | **0.08** | 1.6% |
| mb1 | 0.186 | 0.371 | 66.69 | 67.66 | **0.97** | 1.4% |
| mb4 | 0.527 | 1.403 | 257.09 | 264.59 | **7.50** | 2.8% |
| mb4_dense | 0.784 | 2.292 | 423.44 | 445.20 | **21.76** | 4.9% |

- 跳过 `doc.Validate`：**只能省 1.4–4.9%**，但会丢失 spec 合法性早失败与 3.1 `type: null` 兼容 workaround 语义。**不推荐**为性能去掉 Validate（可作为 `--skip-validate` 极客开关，默认开启校验）。
- 禁用外部 refs（`IsExternalRefsAllowed=false`）：本地文件场景**无收益**（mb4 262.65 vs 262.0，噪声内），且会破坏带外部 `$ref` 的 spec。**不采用**。
- `detectVersion` 的 `string(data)` 复制 + 5 次 `strings.Contains`：0.5–1% 总耗时，可改成 `bytes.Contains` 省掉整份复制（F13，P3 顺手清理）。
- 原始 bytes 复用（先读一次再 `ParseSpecFromJSON`）：1MB 省 ~4%（文件 I/O 0.16ms + version 0.37ms），4MB 省 <1%；且需要给 Parser 增加 bytes 入口。**收益不足，不纳入批次 2 主线**。
- **LoadFromData 占 95%+**：仓库侧唯一有效的优化是"不重复解析"（缓存），不是"解析更快"。这与 F1/F2 的结论一致。

---

## 4. 综合对比（0.07/1/4MB 三档）

冷路径基线（重解析，端到端）：0.07MB **5.1ms** / 1MB **70.8ms** / 4MB **262.7ms**（不含 ~6.5ms 进程启动）。
下表"热路径解码"= 只算缓存格式的解码（不含读文件；b 类含命令树遍历）；"相对重解析"= 热路径 / 冷路径。

| 方案 | 热路径解码 0.07MB | 1MB | 4MB | 缓存体积（4MB） | 相对重解析（4MB） | 正确性风险 / 维护成本 |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| a-1 完整 T JSON（unmarshal） | 5.46 ms | 63.48 ms | 232.09 ms | ~3.96MB | 0.91×（**-9%，无意义**） | `$ref` 100% 未解析（需重解析恢复）；已有死代码应删 |
| a-2 完整 T gob（decode） | 0.12 ms（**坏**） | 0.12 ms（**坏**） | 0.69 ms（**坏**） | 0.155MB（缺 Paths） | 表面 0.003×，实际不可用 | **静默丢数据**；不可接受 |
| **b-tree gob（decode+walk）** | 0.031 ms | 0.267 ms | 0.915 ms | 0.47MB | **~0.004×（≈270×）** | 依赖"未导出字段构建后无用"的隐式前提 |
| **b-index gob（decode+walk，推荐）** | **0.044 ms** | **0.434 ms** | **1.356 ms** | **0.50MB** | **~0.005×（≈190×）** | 显式 DTO + SchemaVersion + golden 测试可控 |
| c 解析优化（跳过 Validate/外部 refs 等） | -1.6% | -1.4% | -2.8% | — | 1.0×（**无收益**） | 失去校验语义；收益极低、不采用 |

**推荐：b-index（持久化派生索引 DTO）分两层落地**——先覆盖 help/completion（风险最低、收益最大），执行路径作为下一阶段（需 handler 改造，另行 POC → 见 §7/§8）。

---

## 5. 缓存键与失效策略

### 5.1 键（按 source 类型）

```go
type indexKey struct {
    AppName            string // 隔离 app（同 spec 可被多 app 安装）
    SourceKind         string // "file" | "url"
    Source             string // file: 绝对路径；url: 规范化 URL
    Size               int64  // file: stat size；url: 0
    ModTimeNanos       int64  // file: stat ModTime().UnixNano()；url: 0
    ContentSHA256      string // url: W1 缓存内容哈希（C-B1 已计算）；file: 空（用 size+mtime）
    ParserVersion      string // 见 5.2
    IndexSchemaVersion int    // DTO 结构版本
}
```

- **本地文件**：`path + size + mtimeNanos + ParserVersion + IndexSchemaVersion`。命中前只需一次 `os.Stat`（µs 级，无需读 4MB 内容做哈希）。
- **远端 URL**：交给批次 1 W1 的 `pkg/spec/persistent_cache.go`（`C-B1`：URL/ETag/Last-Modified/FetchedAt/ExpiresAt/SHA256）负责"内容是否变化"；派生索引键用 `ContentSHA256 + ParserVersion + IndexSchemaVersion`，命中时**零网络**。远端 fallback（网络失败用 stale 内容）时，只要内容哈希未变，派生索引仍然有效。
- 键 hash 进文件名，避免"读到别的版本文件"：`<configDir>/apps/<app>/cache/derived/index-<sha256(key)[:16]>.gob`（由 W6 在 `main.go` 构造 Parser 时用 `spec.WithIndexCache(configDir)` 传入；`pkg/spec` 不 import `pkg/config`，避免依赖反向）。

### 5.2 版本（什么时候必须失效）

`ParserVersion` 建议由三部分拼成单一字符串（写进缓存头，不匹配即重建）：

1. `kin-openapi` 版本（从 `go.mod` 固定值生成常量，如 `"kin-openapi/v0.133.0"`）——解析产物结构变化；
2. `semantic` 规则版本（**新增** `semantic.DerivedRulesVersion`，资源/动词启发式、`ActionVerbs`、conflict 策略变化时 bump）——命令树语义变化；
3. `IndexSchemaVersion`（gob DTO 字段/语义变化）。
   - 附：`x-cli-verb`/`x-cli-resource` 等内容变化由 source 的 mtime/内容哈希覆盖，不需要进版本号。

### 5.3 失效判定顺序（命中路径）

```
1. os.Stat(source) fail        → 不读缓存（让上层报错）
2. 读 cache 文件头（magic/version/keyHash）
3. key 不匹配（size/mtime/版本）→ 视为 miss
4. CRC 校验失败 / gob decode 失败 → 删除坏缓存，视为 miss
5. 命中 → 直接用（help/completion）；执行路径仍按原逻辑 LoadSpec
```

所有 miss/损坏路径必须**静默回退到解析**，绝不因为缓存问题让命令失败。

---

## 6. 原子写与并发

### 6.1 单文件格式（避免"索引 + meta 两份写"的撕裂）

```
magic        [8]byte   "OBCACHE\x01"
schemaVer    uint32
keyLen       uint32
payloadLen   uint32
crc32c       uint32   // payload 的 CRC32C
keyJSON      [keyLen]byte
payload      [payloadLen]byte  // gob(CachedIndex)
```

- 写入：`os.CreateTemp(dir, "index-*.tmp")` → `Write` → `f.Sync()` → `f.Close()` → `os.Rename(tmp, final)`（同目录 rename 原子）。
- 读取：`os.ReadFile` → 校验 magic/schemaVer/key/crc → gob decode；任一失败删除并 fallback。
- 好处：单文件原子替换，不存在"新 index + 旧 meta"；比"tmp+rename 两个文件"简单。
- 权限：`0600` 或 `0644`（派生索引只含路径/summary/参数名与类型；**不缓存任何 Header/Token**——认证信息在 keyring/Profile，索引里天然没有）。缓存目录沿用 `apps/<app>/cache/`（与 W1 共存，子目录 `derived/`）。

### 6.2 并发

| 场景 | 处理 |
| --- | --- |
| 多进程同时写同一 index | last-writer-wins；rename 原子；允许重复计算（最多多解析一次）。可选 `flock` 写锁降低 thundering herd（批次 2 可先不做）。 |
| 读时正好被写 | 读端看到旧文件或新文件（rename 原子），不会读到半个文件。 |
| MCP 长驻进程 | 进程内已有 `Parser.cache sync.Map`（5 分钟 TTL），索引只在启动时读一次；不冲突。 |
| 同一 spec 被多个 app 安装 | key 含 `AppName`，各自目录；接受重复缓存（体积 0.5MB/4MB spec，可接受）。 |

---

## 7. 接入点（现状 → 改造）

### 7.1 新增（pkg/spec，不 import config）

```go
// pkg/spec/derived_index.go（新）
type AttributeIndex struct { ... }           // 见 §3.3 CachedIndex
func (p *Parser) LoadAttributeIndex(ctx context.Context, appName, source string) (*AttributeIndex, bool, error)
func (p *Parser) SaveAttributeIndex(appName, source string, idx *AttributeIndex) error
func WithIndexCache(dir string) ParserOption
```

### 7.2 现有调用点接线（阶段 P2）

| 现状 | 改造 |
| --- | --- |
| `cli.Handler.showAppHelp` → `h.mapper.BuildCommandTree(specDoc)`（`pkg/cli/handler.go:635-664`） | 新增 `h.loadCommandTree(appName, appConfig)`：先 `super.LoadAttributeIndex`，命中直接渲染；miss → 原路径 + 写缓存 |
| `cli.Handler.showResourceHelp`（`:563-618`） | 同上（只用到 tree 的 resource 子集） |
| `completion.Provider.loadSpec` + `BuildCommandTree`（`pkg/completion/completion.go:386-405`） | 改用 `LoadAttributeIndex`（补全只需要 resource/verb/flag 名，b-index 的 condensed params 足够） |
| `pkg/spec/parser.go:835-839 LoadSpecWithPersistentCache` | **保留原语义**（执行路径仍返回完整 `*openapi3.T`）；仅更新注释去空壳；不要试图让派生索引伪装成 `*openapi3.T` |
| `cmd/ob/main.go` init 的 `spec.NewParser()` | W6 一行改为 `spec.NewParser(spec.WithIndexCache(configDir))` |
| `pkg/config` `SaveParsedSpec/LoadParsedSpec/ValidateParsedSpec` | **批次 2 删除**（死代码 + a-1 已证无收益）；`SpecCacheMeta.ParsedSpecPath/ParsedAt/ParserVersion` 同步清理 |

> 注意 help 的三层需求不同：`<app> --help` / `<app> <resource> --help` 只需命令树；`<app> <res> <verb> --help`（`FormatUsageHelp`，`pkg/cli/errors.go:316+`）需要参数/请求体 schema，属于 P3 范围（b-index 已缓存参数但请求体属性需扩展，或该命令级别仍走解析回退）。

---

## 8. 批次 2 分步计划（含工作量）

| 阶段 | 内容 | 交付/验收 | 工作量 |
| --- | --- | --- | --- |
| **P1 存储层** | `pkg/spec/derived_index.go`：DTO（`SchemaVersion`）、`AttributeIndex` 构建/转换、单文件原子读写、key（§5）、坏缓存回退、`WithIndexCache` | 单测：roundtrip `reflect.DeepEqual`、版本不匹配重建、截断/坏 CRC、并发读；**golden 测试必须断言"解码后可用于渲染，而不只是 err==nil"**（a-2 的教训） | **S**（1–2d） |
| **P2 help/completion 接线** | `cli.Handler.loadCommandTree` + `showAppHelp/showResourceHelp`；`completion.Provider`；W6 在 `main.go` 构造 Parser 时传缓存目录；`pkg/config` 死代码清理 | `go test -short ./...` 全绿；e2e：`ob <app> --help` 在缓存命中时**不调用** `LoadSpec`（可用 fake Parser/stub 计数，或 strace/pprof 佐证）；4MB 档 `--help` 端到端 ≤ 15ms（本机） | **M**（2–3d） |
| **P3 命令级 help** | 扩展 index：请求体顶层属性 schema（type/format/enum/required）、参数默认值；让 `FormatUsageHelp` 可从 index 渲染（或保留解析回退，按收益决定） | 命令级 `--help` 输出与现状逐字对比（golden）；缓存命中率统计 | **M**（2–3d） |
| **P4 执行路径（可选，需 gate）** | 把 P3 的 execution view 交给 `request.Builder`：支持从 index 重建精简 `openapi3.Parameters`/`RequestBody`（或以新 DTO 直接构造请求）；handler 改造；功能开关 + miss 回退 | POC 基准：4MB 档 spec 相关耗时 ≤ 5ms（decode + 重建）、内存 <20MB；e2e 参数/请求体/嵌套对象全等价；失败率 0 | **L**（4–6d） |
| **P5 远端 & 观测** | 与 W1 content cache 对齐（key 用 ContentSHA256）；命中/miss/损坏/回退计数器（`slog`/stderr debug）；文档 | 远端 4MB spec：第二次命令零网络 + 端到端 ≤15ms；`ob list/info` 不触发解析（W1 已覆盖） | **S**（1–2d） |

依赖顺序：P1 → P2 →（P3 → P4 可选）→ P5。P2 是收益/风险最优点，建议批次 2 先做 P1+P2，P4 用独立 POC 决定。

---

## 9. 两种收益估算

### 9.1 只做 help/completion 缓存（P1+P2）

以"进程启动 6.5ms + 配置读取 ~0.5ms + 目标命令成本"估算端到端（本机 warm OS cache）：

| spec | 现状 `--help` 端到端 | 命中索引后 | 节省 |
| --- | ---: | ---: | ---: |
| 0.07MB | ~12.1 ms（5.1+7.0） | ~7.1 ms | **41%** |
| 1MB | ~77.3 ms（70.8+6.5） | ~7.5 ms | **90%** |
| 4MB | ~269.2 ms（262.7+6.5） | ~8.5 ms | **97%** |
| 4MB_dense | ~474 ms | ~11 ms | **98%** |

- shell 补全（每次 Tab 一次进程）：performance.md F5 的 4.1MB→134ms/Tab 可降到 ~8ms/Tab（**~17×**），是用户体感最强的场景（补全按键）。
- 小 spec 收益有限（解析 5ms 已在噪声级），但反正命中路径 <0.1ms，没有副作用。
- 覆盖范围：`<app> --help`、`<app> <res> --help`、所有 shell 补全（resource/verb/flag）；**命令级 `--help` 与执行仍走解析**（P3/P4 之前）。

### 9.2 完整执行路径缓存（P4，估算，未实测）

| 方案 | 4MB 档执行路径 | 可行性 |
| --- | ---: | --- |
| a-1 完整 T JSON | 268.7 → 240.5 ms（**-10%**） | 否决（§3.2） |
| b-index 扩展 execution view（估算） | 268.7 → ~10–12ms（**-96%**） | 需 P4 改造；按当前 index decode 1.36ms × 1.8–3（请求体 schema 扩展）+ 重建/渲染 ~2–4ms 估算 |
| 仅 W1 远端内容缓存 | 去掉下载（公网 100ms 级），解析仍在 | W1 已交付；与 W5 正交、可叠加 |

- 若 P4 的 POC 达不到"4MB 档 decode+重建 ≤5ms 且行为等价"，则**执行路径保持现状**（每次解析），只在 help/completion 用缓存——这不影响 P2 的收益。
- 更现实的折中：执行路径仍解析完整 spec，但利用 P2 的索引做"资源/动词解析"以外的快速失败（如未知 resource/verb 直接报错，不解析 spec），可省"输错命令"场景的全量解析（未实测，逻辑可行）。

---

## 10. 风险与缓解

| 风险 | 证据/影响 | 缓解 |
| --- | --- | --- |
| **gob 静默丢字段** | 本 spike a-2：`openapi3.Paths` 未导出 map 被忽略，编码/解码均无错误但 spec 变空 | DTO 全导出字段；golden roundtrip 必须 `reflect.DeepEqual` + "可渲染/可查询"双断言；禁止直接 gob 第三方结构 |
| gob 兼容性 | 字段重命名/类型变更后旧缓存 decode 报错或语义错 | 文件头 `IndexSchemaVersion` + `ParserVersion`；不匹配即重建；decode 失败删除并按 miss 处理 |
| 语义规则变更导致陈旧命令名 | `semantic.ActionVerbs`/`x-cli-verb` 启发式变化后缓存仍用旧 verb | 引入 `semantic.DerivedRulesVersion` 并纳入 ParserVersion；修改规则必须 bump（写入 CONTRIBUTING 检查项） |
| map 迭代顺序 | gob 不保证 map 顺序；help 渲染当前本就遍历 map（`writeResourcesSection`），顺序不确定是既有行为 | 索引落地时对资源/词典排序（渲染层 sort），顺便让 help 输出确定化（便于 golden test）；不要把"缓存文件字节"当内容哈希 |
| mtime 粒度/编辑器保留 mtime | `size+mtimeNanos` 在极少场景漏判（同尺寸同 mtime 内容变化） | 默认接受（业界常见）；提供 `OPENBRIDGE_CACHE=off` 与 `--no-cache`；必要时对 mtime 未变但"用户显式要求"的场景算内容哈希（4MB ≈ 1–2ms，仅 opt-in） |
| 并发写/读 | 多进程同时启动（补全高并发） | rename 原子 + last-writer-wins；写端可选 flock；读端坏文件即删 |
| 缓存目录权限/膨胀 | 每 app 0.1–1.2MB，可接受；大量 app 时 | 跟随 W1 的缓存清理策略（按 mtime/总量 LRU，批次 2 可先记录目录大小到 `ob info`） |
| 安全 | 索引只含 path/summary/参数名类型（公开信息），不含 Header/Token | 不把 `Profile.Headers/Auth` 写入索引；文件权限 0600；路径可能含本地用户名，与配置目录同级可接受 |
| 远端 spec 一致性 | W1 的 stale fallback + 派生索引哈希 | 派生索引键用**内容哈希**而非 URL+时间，天然对齐 |
| 性能回退（cache miss 时多写一次） | 首次命令 parse 256ms + BuildIndex ~1ms + 写 0.5MB | 可接受（<0.5% 开销）；写入失败静默忽略 |

---

## 11. 结论

1. **完整 `openapi3.T` 不能作为解析加速缓存**：a-1 JSON 往返 232.7ms ≈ 重解析 256.2ms（-9%），且 `$ref` 100% 未解析；a-2 gob 会静默损坏（Paths 丢失）。现有 `SaveParsedSpec/LoadParsedSpec` 应删除。
2. **派生索引（b-index gob DTO）是正确方向**：4MB spec 缓存 0.50MB（11.9%），decode 1.36ms；help 端到端 262.7ms → 1.5ms（**179×**），端到端（含启动）~269ms → ~8.5ms（**97%**）。
3. **解析微优化（c）不值得作为主方案**：Validate 只占 1.4–4.9%，外部 refs/bytes 复用/version 检测合计 <2%；唯一值得顺手做的是 `detectVersion` 去掉 `string(data)` 复制（P3）。
4. **落地路径**：P1 存储层（S）→ P2 help/completion 接线（M）→ P3 命令级 help（M）→ P4 执行路径（L，gate）→ P5 远端与观测（S）。建议批次 2 先交付 P1+P2，用真实 e2e 数字验证"4MB `--help` ≤15ms、缓存命中零解析"后再决定 P3/P4。

---

## 附录 A：复现与原始数据

- 工程：`/tmp/cache-spike/`（`main.go`、`gen_specs.py`、`specs/*.json`、`go.mod` + `replace`）
- 基准命令：`cd /tmp/cache-spike && GOFLAGS=-mod=mod GOPROXY=off ./spike specs/small_007mb.json specs/mb1.json specs/mb4.json specs/mb4_dense.json`
- 原始输出：`/tmp/cache-spike/results.txt`（全量基准）、`/tmp/cache-spike/results2.txt`（$ref 未解析诊断）
- 未测项（局限）：
  - 仅合成 spec（非真实 petstore/GitHub/Stripe），解析成本 ∝ operation 数/参数数；真实 spec 的 `$ref` 复用更高，解析分配曲线可能不同；建议 P2 后用 1 个真实大 spec 复测。
  - 仅 JSON 格式源（YAML 走同一 kin-openapi 路径，预期同量级；未单独拆分）。
  - gob 压缩（flate/zstd）未测：index 0.5MB 已足够小，暂不需要；若需进一步缩小可评估。
  - msgpack 未测：契约不允许新增依赖，且 gob 指标已满足（decode 1.36ms）；若未来要跨语言消费再评估。
  - P4 execution view 的 decode/体积为**估算**（未实现），需 POC 验证。

## 附录 B：测量噪声与口径

- `ns/op` 来自 `testing.Benchmark` 默认校准（大对象 4–8 次迭代）；`4MB LoadSpec` 跨运行观测 250–268ms（±7%），报告中"冷/热对比"使用同一轮数据，比例结论稳定。
- `B/op`/`allocs/op` 包含 GC 开销，只用于比较量级（如 190.46MB vs 2.49MB），不作为时间结论。
- `stageProfile` 为 7 轮交错采样中位数，专门用于占比拆分（Validate 增量等）；其绝对值与 `Benchmark` 口径可能差 ±5%，报告只取占比。
- 测量期间 W1 正在修改 `pkg/spec/parser.go` 的**远端 URL** 路径；本地文件 `loadFromFile → parseSpec` 路径未变，故本次本地 spec 数据不受影响。批次 1 合并后建议用同一工程重跑一次做回归（预计无差异）。
