# OpenBridge 批次 2 修复交付报告

> 上游：[`FIX-PLAN-BATCH2.md`](./FIX-PLAN-BATCH2.md)、[`proposals/02_persistent_cache.md`](./proposals/02_persistent_cache.md)、[`FIXES-BATCH1.md`](./FIXES-BATCH1.md) §5
> 基线：批次 1 提交 `f31af20`（已推送）
> 方式：Agent Teams 4 路并行（W1–W4，其中 W4 含 bug 修复 W4b）+ Lead 走线/终验
> 状态：**全部完成**；全部门禁通过；DCO 签名提交；已推送

---

## 1. 结论摘要

| 项 | 批次 1 后 | 批次 2 后 |
| --- | :--- | :--- |
| 大 spec 的 `ob <app> --help` | 每条命令解析 spec（1.93MiB→2500 ops 冷启动 258ms） | 首次建索引后**热路径 18.2ms**（14.2×）；4.09MB 基准 11.3ms |
| shell 补全 | 每次 Tab 解析 spec（145ms 级） | 走派生索引 **~9.7ms**（同一 1.93MiB spec 实测；4MB 基准 11.4ms） |
| 死代码 | `spec_watcher.go`/`spec_cache.go` 共 ~1.6k 行无生产调用 + `fsnotify` 依赖 | **删除 4 文件 / −3260 行**；`fsnotify` 从 go.mod 与构建图移除 |
| MCP HTTP | 固定 30s client，profile TLS/retry 不生效 | per-profile **TLS/超时/重试**全部生效（CA 生效/无 CA 失败/非法 CA isError 但进程存活/503 重试） |
| TUI 安装向导 | 1,347 行 0% 覆盖；且发现 3 个真实 bug | **99.2% 覆盖**；3 个 bug 修复并有回归测试 |
| 覆盖率 | 61.7% | **71.9%** |
| 测试 | 655 个 Test | **670 个 Test**（新增 81 个，同时删除死代码的旧测试） |
| 规范化 | 帮助输出顺序随 map 迭代变化 | 排序输出，**冷/热帮助文本字节一致** |

## 2. 工作流与交付

### W1 持久化派生索引（perf-analyst）—— D-05

**P1 存储层** `pkg/spec/derived_index.go`（新，631 行）：
- API：`spec.WithIndexCache(dir string)`、`(*spec.Parser).LoadDerivedIndex(ctx, source, appName)`、`(*DerivedIndex).CommandTree()`。
- **显式 gob DTO**（资源/动词/子资源 + method/path/operationId/summary/description/tags），不缓存 `openapi3.T`（提案已实测：JSON 往返仅 −9% 且 `$ref` 丢失；直接 gob 会静默丢 `openapi3.Paths`）。
- 格式：`magic "OBDIDX01" | formatVersion | keyLen | payloadLen | crc32c | keyJSON | gob payload`；tmp+fsync+chmod 0600+rename 原子写；损坏/CRC/版本不匹配 → 删除并按 miss 重建。
- 键：本地 `abs_path+size+mtimeNanos+ParserVersion+IndexSchemaVersion`；远端 `URL+content SHA256+版本戳`。
- `ParserVersion = kin-openapi 版本 + semantic.DerivedRulesVersion + spec.DerivedIndexSchemaVersion`。

**P2 接线**：
- CLI help 三路径（app/resource/command-list）优先走索引，缺失/失效回退完整解析，帮助文本/退出码不变；
- completion 的 4 个命令补全方法走索引；
- 帮助渲染改为排序输出（内容不变，冷/热输出字节一致）。

**基准（真实二进制，隔离 HOME）**：

| spec | 场景 | cold p50 | warm p50 | 加速 |
| --- | --- | ---: | ---: | ---: |
| 4.09MB / 20,280 ops | `ob <app> --help` | 134.8ms | **11.3ms** | 11.9× |
| 4.09MB / 20,280 ops | `ob __complete` | 145.2ms | **11.4ms** | 12.7× |
| 4.2MB / 2,600 ops | `--help` | 265.6ms | **10.4ms** | 25.5× |
| 1.93MiB / 2,500 ops（Lead e2e） | `--help` | 257.9ms | **18.2ms** | 14.2× |

索引体积：2.61MiB（20k ops）/ 0.45MiB（2.6k ops）；存储层 bench：cold miss 149.7ms、warm hit 3.19ms、CommandTree 0.83ms、encode 3.53ms。

### W2 死代码清理（process-auditor）—— 架构 F-42 / 性能 F11

- 删除 `pkg/config/spec_cache.go`（816 行）、`spec_cache_test.go`（665）、`spec_watcher.go`（778）、`spec_watcher_test.go`（1001），**共 −3260 行**。
- 删除前 grep 确认生产引用为零；`fsnotify` 仅被 `spec_watcher.go` import，删除后从 go.mod/构建图移除（Lead `go mod tidy`）。
- 唯一连带调整：`config.BuildSpecFetchOptions` 返回类型改为 `spec.SpecFetchOptions`（字段一致，无行为变化）。

### W3 MCP per-profile 客户端（sec-fixer）—— FIXES-BATCH1 §5

- `NewHandler(nil)` / Progressive 不再固定单一 client：按 profile 懒构造并缓存（mutex；显式注入优先）。
- Tool 调用统一走 `request.NewHTTPClient(profile)` + `request.DoWithRetry(client, req, profile.RetryConfig)`。
- 非法 TLS 配置 → MCP `isError`（含原因），进程存活可继续响应；无配置时仍 30s 兜底。
- 14 个新测试 + 真实 `ob run app --mcp` stdio e2e 13/13 PASS。

### W4 TUI 测试与 bug 修复（arch-reviewer）—— 覆盖率盲区

- 新增 3 个测试文件（35 Test / 56 subtest）：`model_test.go`、`render_test.go`、`test_helpers.go`。
- 覆盖：初始化/步骤前进后退、spec/BaseURL/TLS 输入与校验、auth 类型、MCP 选项、确认/取消、错误态、窗口变化、全部 18 个 Step 的 `View()` 渲染。
- 覆盖率 **0% → 99.2%**，`-race` PASS，无 TTY/真实 bubbletea program。
- **发现并修复 3 个真实 bug（W4b）**：
  - **B2（P1）**：`updateOptionSelection` value receiver 导致 shim/search-engine 选择被丢弃 → TUI 无法选择 "No shim"；改为值接收 + 写回 setter，回归测试锁定。
  - **B3（P2）**：`determineNextMCPStep` 丢弃 SearchEngine 自动赋值 → 单引擎场景最终为空；改为返回 `(Step, string)` 并持久化。
  - **B1（P2）**：空输入 Enter 把 Placeholder 文案当真实值 → "必填"校验分支不可达；改为空输入保留空串并走校验。
- 覆盖率进一步提升至 **99.2%**（要求 ≥50%）。

### W5 Lead 走线

- `cmd/ob/main.go`：`spec.NewParser(WithRemoteCache(...), WithIndexCache(<config-dir>/index-cache))`。
- `go mod tidy` 移除 `fsnotify`（保持 govulncheck 0 可达）。
- 文档同步（中英）：README/introduction/quick-start/reference（新增 Caching 章节）/index。
- 全量门禁、回归与提交。

## 3. 验收证据（Lead 最终执行）

| # | 检查 | 结果 |
| :---: | --- | --- |
| 1 | `go build ./...` | ✅ |
| 2 | `gofmt -l` / `goimports -l` | ✅ 0 |
| 3 | `go vet ./...` + `go vet -tags=integration ./...` | ✅ |
| 4 | `go test -short -count=1 ./...` | ✅ 13 包 |
| 5 | `go test -race -short -count=1 ./...` | ✅ 13 包 |
| 6 | `go test -tags=integration -count=1 ./...` | ✅ 14 包 |
| 7 | `golangci-lint v2.15.0 run ./...` | ✅ **0 issues** |
| 8 | `govulncheck ./...` | ✅ **0 可达漏洞** |
| 9 | 覆盖率 | ✅ **71.9%**（批次 1：61.7%） |
| 10 | `pkg/tui/install` 覆盖率 | ✅ **99.2%**（原 0%） |
| 11 | `npm run docs:build` | ✅ |
| 12 | 批次 2 e2e（`/tmp/lead_verify_b2.py`，20 断言） | ✅ **20/20 PASS** |
| 13 | 批次 1 回归（18 断言） | ✅ **18/18 PASS** |
| 14 | 批次 0 回归（22 断言） | ✅ **22/22 PASS** |

### 3.1 批次 2 e2e 关键输出

```
PASS A0 big spec generated        | size=1.93MiB
PASS A2 cold help builds index    | cold=257.9ms
PASS A3 warm help uses index      | cold=257.9ms warm=18.2ms speedup=14.2x
PASS A4 index cache files written | files=1
PASS A5 help content correct      | output stable
PASS A6 completion uses index     | completion=9.7ms rc=0
PASS B1 removed spec_cache.go / spec_watcher.go ...
PASS B2 fsnotify not in build graph
PASS B3 fsnotify removed from go.mod
PASS C1 MCP profile CA works
PASS C2 MCP without CA fails
PASS C3 invalid CA isError
PASS C4 server survives bad config
PASS C5 MCP retry on 503          | requests=3
PASS D1 TUI coverage >=50%        | coverage=99.2%
```

## 4. 行为变更与兼容性

| 变更 | 影响 | 兼容性 |
| --- | --- | --- |
| 派生索引缓存（`<config-dir>/index-cache`） | 帮助/补全热路径不再解析 spec | 新目录；删除后自动重建；索引失效自动回退解析 |
| 帮助输出排序 | 资源/动词顺序确定、冷/热一致 | 文本内容不变，仅顺序稳定 |
| `spec_watcher`/`spec_cache` 包内导出 API 删除 | 仓库内无消费者 | 属于死代码清理；如需 watch 能力应作为显式新功能重做 |
| MCP client 从 profile 构造并缓存 | profile TLS/超时/重试生效 | 显式注入 client 仍优先；非法配置返回 isError 而非启动失败 |
| TUI shim 可选 "No" | 修复后用户可真正禁用 shim | 修复前行为（始终建 shim）属 bug |
| TUI 空必填输入现在报校验错误 | 不再静默使用 Placeholder 文案 | 修复前是错误路径不可达 |

## 5. 遗留项（批次 3 候选）

| 项 | 依据 | 说明 |
| --- | --- | --- |
| 派生索引 P3（命令级 `--help`、flag/enum 补全） | proposals/02 | 当前仍解析 spec；预计再降 100ms 级 |
| 派生索引 P4（执行路径 index，POC gate） | proposals/02 | 需 handler/request-builder 改造；L |
| `init()` 组合根、`pkg/config` 拆分 | 架构 F-02/F-05 | 结构性治理 |
| 错误模型 sentinel 化 + `PrintedError` 语义 | 架构 F-22/F-23 | 字符串嗅探仍在 |
| 索引裁剪（Description 占 67% 体积） | W1 风险 | 高 op 数 spec 可减小索引 |
| MCP client 缓存热更新（profile 名作 key） | W3 风险 | 启动期静态配置假设 |
| `ghodss/yaml` 归档依赖、`BuildSpecFetchOptions` 保留 API | 风格 F-18 / W2 交接 | 批次 3 清理 |

## 6. 复现验收

```bash
cd /Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge
go build ./... && go vet ./... && go vet -tags=integration ./...
gofmt -l pkg cmd internal && goimports -l pkg cmd internal
go test -short -count=1 ./... && go test -race -short -count=1 ./...
go test -tags=integration -count=1 ./...
/tmp/gobin2/golangci-lint run ./... && /tmp/gobin2/govulncheck ./...
python3 /tmp/lead_verify_b2.py   # 20 断言（批次 2）
python3 /tmp/lead_verify_b1.py   # 18 断言（批次 1 回归）
python3 /tmp/lead_verify.py      # 22 断言（批次 0 回归）
npm run docs:build
```

> 结论：批次 2 完成派生索引（help/completion 热路径 11–18ms）、1.8k 行死代码与 fsnotify 依赖清理、MCP per-profile TLS/超时/重试接线、TUI 0%→99.2% 覆盖并修复 3 个真实 bug；覆盖率 61.7%→71.9%，全部门禁与三批 e2e 回归通过。
