# OpenBridge 批次 1 修复交付报告

> 上游：[`FIX-PLAN-BATCH1.md`](./FIX-PLAN-BATCH1.md)、[`ENGINEERING-DIAGNOSIS.md`](./ENGINEERING-DIAGNOSIS.md) §9 批次 1、[`FIXES-BATCH0.md`](./FIXES-BATCH0.md) §7
> 基线：批次 0 提交 `954a28e`
> 方式：Agent Teams 5 路并行（W1–W5）+ Lead 走线（W6）/依赖升级/终验；TDD 先红后绿
> 状态：**全部完成**（W1–W6 + 依赖安全升级）；全部门禁通过；DCO 签名提交
> 日期：2026-11-07

---

## 1. 结论摘要

| 项 | 批次 0 后 | 批次 1 后 |
| --- | :--- | :--- |
| 远端 spec | install 下载 2 次；每条命令重新下载 | install 1 次；首次命令缓存，之后本地命中；过期条件请求、304 复用、离线 stale 回退 |
| `ob list` / `ob info` | 解析每个 app 的 spec（4.1MB 场景 ~167ms） | 读配置中的 spec 元数据，**~6.9ms**（约 24×） |
| profile HTTP | `timeout`/`tls`/`retry` 字段存在但零接线 | 全部接线：per-profile 超时、CA/mTLS/SNI/TLS 版本、幂等重试 + 退避 + body 重放 |
| MCP 安全 | `require_confirm`/限流/危险模式仅有配置结构 | 三项**真实执行**（Handler + Progressive 双路径），且启动时硬校验正则配置 |
| CI 门禁 | 无覆盖率/漏洞/供应链检查 | codecov 阈值、**govulncheck 阻塞门禁**、Dependabot、CodeQL、`make check` |
| 依赖安全 | govulncheck 实测 **7 个代码可达漏洞** | kin-openapi `v0.133`→`v0.149`、go-sdk `v1.2.0`→`v1.4.1`；**0 可达漏洞** |
| 覆盖率 | 59.2% | **61.7%** |
| 测试 | 584 个 Test | **655 个 Test**（+71） |
| 持久化派生索引 | 无设计 | 完成 spike：`proposals/02_persistent_cache.md`（缓存方案实测 10–18% 体积，help 4MB 269ms→8.5ms 预估） |

## 2. 工作流与交付

### W1 性能缓存（perf-analyst）—— D-06 / D-15

**远端 spec 持久化缓存**（`pkg/spec/persistent_cache.go` + `parser.go`）：
- `spec.WithRemoteCache(dir string) ParserOption`（空 dir 禁用；`pkg/spec` 不依赖 `pkg/config`）。
- 行为：新鲜命中零网络；过期 → `If-None-Match`/`If-Modified-Since` 条件请求；304 刷新元数据复用；非 304 更新；网络错误回退 stale；无缓存返回原错误。
- 原子写：内容 + 元数据分别 `tmp(pid)+rename`，文件 `0600`；损坏/版本不匹配按 miss 自愈。
- 接线（W6）：`cmd/ob/main.go` 的 `init()` 中 `spec.NewParser(spec.WithRemoteCache(filepath.Join(configMgr.ConfigDir(), "spec-cache")))`。

**install 去重**：删除 install 阶段第二次 `FetchWithCache`（运行时首次加载会写缓存）；实测 remote install GET = 1。

**`ob list`/`ob info` 去解析**：`AppConfig` 新增 `SpecTitle`/`SpecVersion`（yaml `spec_title`/`spec_version`），install 写入；`GetInstalledAppInfo` 元数据优先，旧配置缺字段才回退解析；`ob info` 文本输出新增 `API: <title> (v<version>)`。实测删除 spec 源文件后 `ob list`/`ob info` 仍正常。

### W2 profile HTTP 接线（style-auditor）—— D-08 / D-10

新增 `pkg/request/client.go`（纯标准库）：
- `NewHTTPClient(profile *config.Profile) (*http.Client, error)`：Timeout 默认 30s / profile 覆盖；TLS（`insecure_skip_verify`/`ca_file`/`cert_file`+`key_file`/`server_name`/`min_version`）；`http.DefaultTransport.Clone()` 后改 TLS；非法配置 `%w` 报错。
- `DoWithRetry(client, req, retry config.RetryConfig) (*http.Response, error)`：仅 GET/HEAD/PUT/DELETE/OPTIONS；网络错误（排除 context cancel）或 `retryable_status_codes`；`initial_delay` 指数退避（默认 200ms，上限 2s）；body 可重放；`MaxRetries<=0` 单次。
- CLI `executeAPIRequest` 改走上述 client + retry；`Handler.httpClient` 保留注入/兜底。

### W3 MCP 安全控制（sec-fixer）—— D-09

新增 `pkg/mcp/safety.go` + 接线（Handler / ProgressiveHandler / Registry）：
- **require_confirm**：method ∈ 配置时首次调用返回 `isError`：`operation requires confirmation; re-invoke with _confirm=true`；所有工具 schema 增加可选 `_confirm`；确认后执行且不记忆；`_confirm` 从参数中剥离、不转发给 API。
- **max_requests_per_minute**：标准库固定 1 分钟窗口 + mutex（并发安全）；超限返回 `isError` + 剩余等待秒数；SearchTools/LoadTool 不占额度。
- **dangerous_operation_patterns**：启动/SetSpec 编译正则（非法配置报错）；匹配 `METHOD path` 与 tool ID；命中拒绝且 `_confirm` 不可绕过。
- **启动硬校验（W6）**：`cmd/ob/main.go` 与 `pkg/router/router.go` 两条 MCP 启动路径都调用 `mcp.ValidateSafetyConfig`，非法正则启动即失败。

### W4 CI 门禁（process-auditor）—— D-16

- `codecov.yml`：project `target: auto` + `threshold: 1%` + floor 58%；patch 70%；ignore 文档/示例。
- `ci.yml` 新增 `security` job：govulncheck（固定 v1.1.4）；依赖升级后由 `continue-on-error` **翻转为阻塞**。
- `.github/dependabot.yml`（gomod/actions/npm weekly）。
- `.github/workflows/codeql.yml`（Go + Actions，push/PR/schedule，最小权限）。
- `Makefile` 新增 `check` 聚合目标（gofmt → goimports → vet 双 tag → lint → test-short）。

### W5 持久化派生索引 spike（arch-reviewer）—— 为 D-05 铺路

`proposals/02_persistent_cache.md`（374 行）+ `/tmp/cache-spike` 实测：
- **否决 a) 缓存完整 `openapi3.T`**：JSON 往返 4MB unmarshal 232ms ≈ 重解析 256ms（仅 -9%），且 4,160/4,160 个 body `$ref` 回读后未解析；gob 对第三方结构体会**静默丢字段**（`openapi3.Paths` 未导出 map → 空命令树）。
- **推荐 b) 显式 gob DTO 派生索引**：0.07/1/4MB 索引体积 10KB/134KB/499KB（源 12–18%），decode+walk 0.044/0.43/1.36ms；4MB help 269ms→约 8.5ms（-97%），补全 Tab 134ms→约 8ms。
- 否决 c) 解析微优化（Validate 仅占 1.4–4.9%）。
- 批次 2 路线：P1 存储层(S) → P2 help/completion(M) → P3 命令级 help(M) → P4 执行路径(L, POC gate) → P5 远端/观测(S)。

### W6 Lead 走线 + 依赖安全升级

- 解析器缓存接线（上一节）、MCP 安全配置启动硬校验、`ob info` 元数据展示（含测试）。
- **依赖安全**：`kin-openapi v0.133.0 → v0.149.0`、`modelcontextprotocol/go-sdk v1.2.0 → v1.4.1`（govulncheck 实测 7 个可达漏洞 → **0**）；CI govulncheck 门禁转为阻塞。
- 文档同步（中英）：README / introduction / quick-start / reference（Profile 配置 + MCP 安全控制）/ index。

## 3. 验收证据（Lead 最终执行）

| # | 检查 | 结果 |
| :---: | --- | --- |
| 1 | `go build ./...` | ✅ |
| 2 | `gofmt -l pkg cmd internal` / `goimports -l` | ✅ 0 |
| 3 | `go vet ./...` + `go vet -tags=integration ./...` | ✅ |
| 4 | `go test -short -count=1 ./...` | ✅ 12 包 |
| 5 | `go test -race -short -count=1 ./...` | ✅ 12 包 |
| 6 | `go test -tags=integration -count=1 ./...` | ✅ 13 包（含 `internal/integration`） |
| 7 | `/tmp/gobin2/golangci-lint run ./...`（v2.15.0） | ✅ **0 issues** |
| 8 | `/tmp/gobin2/govulncheck ./...` | ✅ **0 可达漏洞** |
| 9 | 覆盖率 `go test -short -coverprofile` | ✅ **61.7%**（批次 0 为 59.2%） |
| 10 | `npm run docs:build` | ✅ |
| 11 | 批次 1 e2e `/tmp/lead_verify_b1.py`（18 断言） | ✅ **18/18 PASS** |
| 12 | 批次 0 回归 `/tmp/lead_verify.py`（22 断言） | ✅ **22/22 PASS**（无回归） |

### 3.1 批次 1 e2e 关键实测

```
PASS A2 install fetches spec once            | gets=1
PASS A3 first run fills cache                | first=1
PASS A3b reruns use local cache              | second=0
PASS A5 offline cache fallback               | help works after server stopped
PASS B2 ob info metadata                     | API: Pet API (v7.8.9)（spec 源文件已删除）
PASS C1 self-signed rejected by default      | tls: failed to verify certificate
PASS C2 custom CA accepted                   | - id: 9 name: tls
PASS D1 profile timeout                      | elapsed=0.01s
PASS D2 retry on 503                         | requests=3
PASS E1 confirm required                     | operation requires confirmation; re-invoke with _confirm=true
PASS E2 confirmed call executes              | {"id":3}
PASS E3 confirmation not remembered          | rejected again
PASS E4 rate limit enforced                  | rate limit exceeded ... retry after 60 seconds
PASS E5 dangerous pattern blocked            | operation blocked by dangerous_operation_patterns
```

独立验收对照：批次 1 的 e2e 由 Lead 直接执行（18 断言）并叠加批次 0 独立 harness 回归（22 断言）；W1/W2/W3/W4 各自另有包级 `-race` 与手测证据（见各任务摘要）。

## 4. 行为变更与兼容性说明

| 变更 | 影响 | 兼容性 |
| --- | --- | --- |
| 远端 spec 缓存默认启用（`<config-dir>/spec-cache`） | 首次命令写缓存；之后本地命中；过期条件请求 | 新目录，0600 文件；删除目录即回退原行为 |
| remote install 不再预写缓存 | 安装后首次命令会下载一次（符合预期） | install 输出不变 |
| `AppConfig` 新增 `spec_title`/`spec_version` 字段 | 旧配置缺字段时回退解析一次并正常显示 | 向后兼容 |
| CLI 请求按 profile 应用 TLS/超时/重试 | 之前被忽略的 profile 字段现在生效 | 期望行为修正；无配置时 30s 超时 |
| MCP 工具 schema 增加可选 `_confirm` | AI 客户端可传；不传保持原行为 | 向后兼容（可选参数） |
| `require_confirm` 方法首次调用被拒绝 | 之前直接执行 | 安全语义修正（仅启用该配置时） |
| 限流为进程内固定窗口 | 多进程不共享额度 | 文档已说明 |
| 非法 `dangerous_operation_patterns` 启动即失败 | 之前静默忽略 | 快速失败 |
| 依赖升级 kin-openapi / go-sdk | API 兼容，全量测试通过 | 补丁级安全升级 |
| govulncheck CI 门禁阻塞 | 有可达漏洞时 CI 红 | 期望治理 |

## 5. 遗留项（批次 2 候选）

| 项 | 依据 | 说明 |
| --- | --- | --- |
| 持久化派生索引 P1–P2（help/completion） | `proposals/02_persistent_cache.md` | 预计 4MB help 269ms→~8.5ms；先做存储层+help/completion 接线 |
| 执行路径派生索引 P4（L，POC gate） | 同上 | 需 handler/request-builder 改造，预计 -96% 但复杂度高 |
| 删除 `config.SpecCacheManager.SaveParsedSpec/LoadParsedSpec` 死代码 | 提案 §3.2 | JSON 往返已被实测证伪 |
| MCP 限流跨进程/持久化（如需） | W3 风险 | 当前进程内固定窗口 |
| `init()` 组合根、`pkg/config` 拆分、错误模型 sentinel 化 | 架构诊断 F-02/F-05/F-22 | 批次 2 结构治理 |
| profile TLS/retry 接入 MCP 请求客户端 | W2 范围外 | CLI 已接线；MCP 仍固定 30s client |
| `ghodss/yaml` 归档依赖、`go 1.25.5` 与 CI 漂移 | 风格诊断 F-18 | 依赖治理 |

## 6. 回滚与复现

```bash
# 回滚（未提交前）
git checkout -- .
git clean -fd pkg/spec/persistent_cache.go pkg/spec/persistent_cache_test.go \
  pkg/mcp/safety.go pkg/mcp/safety_test.go pkg/request/client.go pkg/request/client_test.go \
  .github/dependabot.yml .github/workflows/codeql.yml codecov.yml proposals/02_persistent_cache.md

# 复现验收
go build ./... && go vet ./... && go vet -tags=integration ./...
gofmt -l pkg cmd internal && goimports -l pkg cmd internal
go test -short -count=1 ./... && go test -race -short -count=1 ./...
go test -tags=integration -count=1 ./...
/tmp/gobin2/golangci-lint run ./... && /tmp/gobin2/govulncheck ./...
python3 /tmp/lead_verify_b1.py   # 18 断言
python3 /tmp/lead_verify.py      # 22 断言（批次 0 回归）
npm run docs:build
```

> 结论：批次 1 完成了"远端 spec 缓存、list/info 去解析、profile HTTP 接线、MCP 安全控制、CI 门禁、依赖漏洞修复"六项落地，并交付了持久化派生索引的实测设计与批次 2 路线；全部门禁与端到端场景通过，无批次 0 回归。
