# OpenBridge 批次 0 修复交付报告

> 上游：[`FIX-PLAN.md`](./FIX-PLAN.md)（批次 0 / A0-1…A0-8）、[`ENGINEERING-DIAGNOSIS.md`](./ENGINEERING-DIAGNOSIS.md)
> 基线：`dd18374`；修复方式：Agent Teams 5 路并行（4 修复 + 1 独立验收），Lead 汇总与终验
> 状态：**批次 0 全部完成（A0-1…A0-8），全部门禁通过**；未提交 git（工作树交付，见 §7）
> 交付日期：2026-11-07

---

## 1. 结论摘要

| 项 | 修复前（`dd18374`） | 修复后（工作树） |
| --- | --- | :--- |
| CLI 认证（D-01） | 全链路失效（空 appName + 静默吞错） | ✅ 恢复：真实 `appName` + keyring 错误上抛；e2e 断言收到 `Authorization: Bearer …` |
| 凭据外泄（D-02/03/04） | 3 条路径 | ✅ 全部封堵：网络错误 URL、4 种 codegen 输出、`ob info -o json/yaml` |
| `--json/--yaml/--output/-o`（D-07） | 静默无效，恒输出 YAML | ✅ 生效；优先级 `--output > --json/--yaml > 默认 yaml` |
| `--profile/-p` 与 profile 配置（D-08） | 静默忽略 | ✅ 选择/校验生效；Headers/QueryParams 进入 CLI 请求；未知 profile 报错并列出可用项 |
| `ob <app> …` 直连（D-11） | 不可达（cobra legacyArgs） | ✅ `ob <app> [--help|--mcp|<resource> <verb>]` 全部可达 |
| 集成测试（D-13） | 1/6 带 tag，`go vet -tags=integration` 编译失败 | ✅ 6/6 带 tag；重名消除；tagged 全套可运行；CI 新增 3 步门禁 |
| lint（D-14） | 本机不可运行、版本三处漂移 | ✅ pin `v2.15.0`（Makefile + CI）；全仓 **0 issues**（基线 1 条已修） |
| HTTP 超时（D-10） | CLI/MCP 三处无超时 | ✅ 统一 30s |
| keyring 回退（D-12） | 固定口令 `"openbridge"` 静默落盘 | ✅ 无系统后端 + 无口令 → 明确报错；`OPENBRIDGE_KEYRING_PASSWORD` 显式启用文件后端；`Backend()` 反映真实后端 |
| 文档（A0-8） | `ob version`/Table 默认/Zero Code-Gen/100ms 等漂移 | ✅ 中英 10 个文档文件同步；VitePress 构建通过 |

**量化**：
- 变更：**36 个已跟踪文件，+1119 / −178 行**；新增 4 个测试文件 + 3 份文档/计划。
- 新增测试：**51 个 Test 函数**（20 个加入既有文件 + 31 个在新增测试文件），全部 TDD 先红后绿。
- 覆盖率：**55.3% → 59.2%**（`go test -short -coverprofile` 全仓口径）。
- 门禁：build / vet(双 tag) / short / `-race` / integration-tags / lint / gofmt / goimports **全部通过**；独立 e2e **22/22 PASS**（Lead）+ **10/10 PASS**（独立验收）。

## 2. 与诊断发现的对应关系

| 诊断 ID | 描述 | 修复内容 | 主要文件 |
| :---: | --- | --- | --- |
| D-01 (P0) | CLI 认证空 appName + 静默吞错 | 调用点传真实 `appName`；`InjectAuth` 契约 C1（none/空 auth 不查 keyring、NotFound 静默、后端错误 `%w` 上抛） | `pkg/cli/handler.go`、`pkg/request/builder.go` |
| D-02 | 网络错误回显 query secret | `redactURL`：清 userinfo、query 值→`REDACTED`（保留 key 名）、解析失败 fail-closed；两处打印均脱敏 | `pkg/cli/errors.go` |
| D-03 | codegen URL 未掩码 | `maskURL` + `isSensitiveQueryParam`；curl/go/nodejs/python 四生成器全部接入；`MaskSecrets=false` 不误伤 | `pkg/codegen/*.go` |
| D-04 | `ob info -o json/yaml` 未掩码 Header | `redactAppConfig` 深拷贝掩码 `Profiles[*].Headers` 与 `SpecFetchHeaders`，不改原配置 | `cmd/ob/main.go` |
| D-07 | 输出格式开关无效 | `parseCLIOptions`（长/短、`=`/空格、`--` 之后不解析）+ 格式传递修复 | `pkg/cli/handler.go` |
| D-08 | `--profile` 与 profile 级配置无效 | `getProfile(appConfig, name)` 校验；`applyProfileToRequest` 应用 Headers/QueryParams（保留既有 query/userinfo） | `pkg/cli/handler.go` |
| D-10 | HTTP 无超时 | CLI `Handler.httpClient`、MCP `NewHandler`/`NewProgressiveHandler` 默认 client 统一 `Timeout: 30s` | `pkg/cli/handler.go`、`pkg/mcp/handler.go`、`pkg/mcp/progressive.go` |
| D-11 | `ob <app>` 路由不可达 | `rootCmd.Args=ArbitraryArgs` + `SetInterspersed(false)`；`Router.ExecuteApp`；删除 `handleOBCommand`/`__debug_` 死分支 | `cmd/ob/main.go`、`pkg/router/router.go` |
| D-12 | keyring 固定口令回退 | 契约 C5：系统后端优先、file 显式 opt-in、真实后端上报；`handleRemoveError` 幂等修复 | `pkg/credential/keyring.go` |
| D-13 | integration 基建失效 | 6 文件补 build tag；重命名 2 个冲突测试；CI 增 `go vet`(双 tag)+tagged test；Makefile 增 `vet`/`test-integration` | `pkg/**/*_integration_test.go`、`internal/integration/*`、`ci.yml`、`Makefile` |
| D-14 | lint 不可复现 | Makefile/CI pin `v2.15.0`；修复基线唯一 staticcheck QF1012 与新增 funlen；全仓 0 issue | `Makefile`、`ci.yml`、`pkg/mcp/progressive.go`、`cmd/ob/main.go` |
| A0-8 | 文档漂移 | `ob version`→`ob --version`；默认输出 YAML；No-SDK-Code-Gen 措辞；冷启动表述；keyring 说明；TLS 现状标注 | `README.md`、`QUICKSTART.md`、`docs/**`（zh/en 共 10 文件） |

## 3. 每个修复的关键实现

### 3.1 认证（D-01）

```go
// pkg/cli/handler.go
func (h *Handler) buildRequest(appName string, ...) (*http.Request, error) {
    ...
    if err := h.reqBuilder.InjectAuth(req, appName, profile.Name, &profile.Auth); err != nil {
        return nil, fmt.Errorf("failed to inject auth: %w", err)
    }
    applyProfileToRequest(req, profile)
```

```go
// pkg/request/builder.go —— 契约 C1
if b.credMgr == nil { return nil }
if authConfig == nil || authConfig.Type == "" || strings.EqualFold(authConfig.Type, "none") {
    return nil // 不触碰 keyring
}
cred, err := b.credMgr.GetCredential(appName, profileName)
if err != nil {
    var notFound *credential.CredentialNotFoundError
    if errors.As(err, &notFound) { return nil } // 未配置凭据是合法状态
    return fmt.Errorf("failed to load credential for %s/%s: %w", appName, profileName, err)
}
```
附带：`credentialProvider` 内部接口使 builder 可用 fake 测试（不改变 `NewBuilder` 签名）。

### 3.2 脱敏（D-02/03/04）

- `redactURL`：`userinfo` 清除、query 值统一 `REDACTED`（保留参数名）、解析失败删 query（fail-closed）。
- `maskURL`：仅掩码 token 类参数名（api_key/token/secret/password/…），占位符 `<YOUR_API_KEY>`，四种生成器覆盖。
- `redactAppConfig`：深拷贝 + 复用 `isSensitiveHeader`/`maskValue`，text 路径不变、磁盘配置不被改写。

### 3.3 CLI 选项与 profile（D-07/D-08）

- 新增 `cliOptions{outputFormat, profileName, generateFormat, generateOutput}` 与 `parseCLIOptions`；`extractCLIFlags` 继续从 API 参数中剥离 CLI 专属键（含 `profile`）。
- 输出优先级：`--output/-o` > `--json`/`--yaml` > 默认 `yaml`。
- `getProfile` 未知 profile → 报错并列出可用项；`applyProfileToRequest` 在认证注入后叠加 Headers/QueryParams。

### 3.4 路由（D-11）

- `rootCmd.Args = cobra.ArbitraryArgs`；`SetInterspersed(false)` 使 app 级 flag 不被 root 吞掉。
- `Router.ExecuteApp(appName, args)` 显式分发（配置校验 → MCP 判定 → CLI）；`Execute` 仅供 argv[0] shim 模式。
- 删除 `handleOBCommand`、`__debug_` 死分支；`Execute()` 拆为 `newRootCmd()`/`runRootCommand()`（同时消掉 funlen）。

### 3.5 keyring（D-12）

- 移除 `keyring.FixedStringPrompt(ServiceName)`；
- `backendCandidates`：默认仅系统后端；`WithFileBackend`/`WithAllowedBackends(file)`/`OPENBRIDGE_KEYRING_PASSWORD` 三选一显式启用文件后端；
- `openKeyring` 逐个尝试并返回真实 `BackendType`（不再按平台猜测）；`ErrNoSecureKeyring`/`ErrKeyringPasswordRequired` 为导出 sentinel。
- `handleRemoveError` 对 `keyring.ErrKeyNotFound` 与 `fs.ErrNotExist` 均幂等（tagged 测试暴露的既有缺陷）。

### 3.6 门禁（D-13/D-14）

- `Makefile`：`GOLANGCI_LINT_VERSION ?= v2.15.0`；`GOBIN` 由 `go env` 解析（`make install` 不再依赖空 `$GOPATH`）；新增 `vet`（双 tag）、`test-integration`；`.PHONY`/help 同步。
- CI：lint job 增 `go mod download`、`go vet ./...`、`go vet -tags=integration ./...`；test job 增 ubuntu 上的 `go test -tags=integration ./...`；lint 固定 `v2.15.0`。
- 集成测试：6 个文件补 `//go:build integration`；`TestIntegrationGetPlatformInfo`/`TestIntegrationWithPassBackend` 消除重名。

## 4. 验收证据（Lead 最终执行）

| # | 检查 | 命令 | 结果 |
| :---: | --- | --- | --- |
| 1 | 构建 | `go build ./...` | ✅ exit 0 |
| 2 | 格式 | `gofmt -l pkg cmd internal` / `goimports -l pkg cmd internal` | ✅ 0 文件 |
| 3 | vet | `go vet ./...`、`go vet -tags=integration ./...` | ✅ 均 exit 0 |
| 4 | 单测 | `go test -short -count=1 ./...` | ✅ 12 包全绿 |
| 5 | Race | `go test -race -short -count=1 ./...` | ✅ 12 包全绿 |
| 6 | 集成测试 | `go test -tags=integration -count=1 ./...` | ✅ 12 包全绿（含 `internal/integration`、`pkg/credential`） |
| 7 | lint | `/tmp/gobin2/golangci-lint run ./...`（v2.15.0） | ✅ **0 issues**（基线 1 条） |
| 8 | 覆盖率 | `go test -short -coverprofile` | ✅ **59.2%**（修复前 55.3%） |
| 9 | 文档构建 | `npm run docs:build` | ✅ 通过（见 §6 环境说明） |
| 10 | Lead e2e harness | `/tmp/lead_verify.py`（22 断言：构建/格式/profile/路由/脱敏/认证/MCP/vet） | ✅ **22/22 PASS** |
| 11 | 独立验收 | perf-analyst `/tmp/verify_batch0/verify.py`（10 场景，修复前 2/10） | ✅ **10/10 PASS，0 FAIL，建议 ACCEPT**；报告 `.diagnosis/verification-batch0.md`（820 行，冻结树 `tree_hash=36c417575a6c0396`） |

### 4.1 独立验收逐项结果（perf-analyst，与 Lead 交叉一致）

| 场景 | 修复前基线（`dd18374` 只读快照） | 修复后（冻结工作树） | 关键证据 |
| :---: | :---: | :---: | --- |
| S1 构建/冷启动 | PASS | PASS | `--version` p50 6.8ms |
| S2 输出格式 | FAIL | **PASS** | `--json/--yaml/--output/-o` 生效且数据一致，默认 YAML |
| S3 `--profile` | FAIL | **PASS** | prod 端口命中 1 次、default 0 次；Headers/QueryParams 生效；未知 profile rc=1 并列出可用项 |
| S4 直连路由 | FAIL | **PASS** | `ob <app> pets list` / `--help` / `--mcp` 全可达 |
| S5 错误回显 secret | FAIL | **PASS** | 两条路径均 `api_key=REDACTED`，canary 0 次 |
| S6 info 掩码 | FAIL | **PASS** | json/yaml 中 `Be******_7`、`XA****_6`，非敏感头保留 |
| S7 认证全链路 | FAIL | **PASS** | `Backend()=file`；mock 收到 `Authorization: Bearer AUTH_TOKEN_CANARY_7` |
| S8 codegen 脱敏 | FAIL | **PASS** | query/Authorization/X-Api-Key 均 `<YOUR_API_KEY>` |
| S9 MCP 冒烟 | PASS | PASS | serverInfo=petapi，3 tools，inputSchema 完整 |
| S10 质量门禁 | FAIL | **PASS** | build/vet/双 tag vet/test-short/gofmt 全绿；`-count=1` 冷缓存全绿 |

独立验收残留观察（不影响 ACCEPT）：
- **R1**：未知 flag 仍静默忽略（修复前后一致，属 D-07 的次要项，列入批次 1）。
- **R2**：S1 仅测进程启动（产品路径 spec 解析成本属批次 1 性能项）。
- **R3**：独立验收只做 tagged 套件编译（`go vet -tags=integration`）；**运行**由 Lead 汇总执行，已全绿（§4 第 6 行）。
- **R4**：profile 的 TLS/Retry 接线属批次 1。

### 4.2 Lead e2e 关键实测输出

```
PASS  2b --json output          | [ { "id": 1, "name": "rex" } ]
PASS  2d --output json          | [ { "id": 1, "name": "rex" } ]
PASS  3a --profile prod routes  | prod_hits=1
PASS  3b profile header sent    | from-prod
PASS  3c unknown profile errors | profile "nope" not found; available profiles: default, prod
PASS  4a ob <app> direct        | - id: 1  name: rex
PASS  5 network error redaction | Failed to connect to: http://127.0.0.1:58201/pets?api_key=REDACTED
PASS  6 info -o json masks headers
PASS  7c Authorization injected | Bearer LEAD_E2E_BEARER_TOKEN
PASS  8 codegen masks query secret | ...pets?api_key=<YOUR_API_KEY> -H 'Authorization: Bearer <YOUR_API_KEY>'
PASS  9 MCP tools/list          | tools=3
```

## 5. 行为变更与兼容性说明（需 reviewer 注意）

| 变更 | 影响 | 兼容性 |
| --- | --- | --- |
| `InjectAuth` 后端错误由"静默跳过"变为 `%w` 上抛 | 凭据后端故障会显式报错（不再静默匿名请求） | 期望行为修正；MCP 调用方会感知 |
| 无系统 keyring 且未设口令时 `NewManager` 返回错误 | 无桌面后端的 Linux/容器默认不可用凭据 | **有意**：移除固定口令回退；设 `OPENBRIDGE_KEYRING_PASSWORD` 启用加密文件后端；`cmd/ob` 以 warning 继续（无凭据） |
| `WithFileBackend(dir, nil)` 且无 env 口令 → `ErrKeyringPasswordRequired` | 显式文件后端必须提供口令 | 现有 `WithFileBackend(dir, pwFn)` 用法不变 |
| `--profile` 不再作为请求参数透传；未知 profile 报错 | 依赖"静默忽略"的脚本会显式失败 | 期望行为修正 |
| 默认输出格式仍为 YAML，但 `--json/--yaml/--output/-o` 现在生效 | 显式开关行为变化 | 文档已改为"YAML（默认）" |
| `ob <app> …` 直连形式现在可用 | 新增能力；shim/`ob run` 行为不变 | 向后兼容 |
| 集成测试文件默认不再参与 `go test ./...`（需 `-tags=integration`） | 默认套件变快；CI 已加 tagged 步骤 | 符合 AGENTS.md §3.3 |
| lint 固定 v2.15.0 | 本地需 `make install-lint`（会覆盖旧 v1 二进制） | CI 可复现 |

## 6. 环境说明（非仓库缺陷）

- 本机 `PATH` 上的 `golangci-lint` 仍是 v1.64.8，与 v2 配置不兼容；本次验收使用 `go install …@v2.15.0` 生成的 `/tmp/gobin2/golangci-lint`。执行 `make install-lint` 可安装 pin 版本。
- `docs/.vitepress/theme/` 是工作区中一个**未被 git 跟踪的空目录**，会导致 VitePress 误解析 `theme/index` 而构建失败。已将其移至 `/tmp/vp-empty-theme-backup`；干净 clone 不受影响，构建通过。若后续需要自定义主题，应提交实际的 `theme/index.ts`。
- 本机 Go 1.27.3 与旧版 staticcheck/golangci（≤v2.8）不兼容，属工具链问题，非仓库代码问题。

## 7. 未完成项（批次 1 候选，明确不在本轮）

| 项 | 诊断 ID | 说明 |
| --- | --- | --- |
| 真正的持久化 spec 缓存（缓存派生命令树，而非 AST） | D-05 | 性能最大单项；需重新设计 gob 索引 + 失效键 |
| `ob list/info` 不解析 spec、远端 spec 读缓存 + 条件请求 | D-05/D-06/D-15 | 依赖上一项 |
| `Profile.Timeout/TLSConfig/RetryConfig` 运行时接线 | D-08/D-10 | 本轮只加了固定 30s 超时；TLS/Retry 仍未接线 |
| MCP `require_confirm`/限流/危险模式实现 | D-09 | 配置语义已确认；实现需设计确认回执 |
| 未知 flag 静默忽略（R1）、错误模型 sentinel 化、`init()` 组合根撤销、`pkg/config` 拆分 | 架构报告 F-02/F-05/F-22 / 验收 R1 | 结构性重构 |
| 覆盖率门禁（codecov threshold）、govulncheck/CodeQL/Dependabot | D-16 | CI 安全与覆盖率门禁 |

## 8. 回滚方式

工作树未提交，回滚只需丢弃改动：

```bash
# 查看改动
git status --short && git diff --stat
# 全量回滚（保留诊断/计划文档）
git checkout -- .
rm -f pkg/cli/cli_options_test.go pkg/cli/handler_test.go \
      pkg/credential/keyring_fallback_test.go pkg/router/router_test.go
# 恢复被移动的空 theme 目录（可选）
mv /tmp/vp-empty-theme-backup docs/.vitepress/theme
```

## 9. 复现验收

```bash
cd /Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge
go build ./... && go vet ./... && go vet -tags=integration ./...
gofmt -l pkg cmd internal && goimports -l pkg cmd internal
go test -short -count=1 ./...
go test -race -short -count=1 ./...
go test -tags=integration -count=1 ./...
/tmp/gobin2/golangci-lint run ./...        # pin v2.15.0
python3 /tmp/lead_verify.py                # Lead 22 项 e2e
python3 /tmp/verify_batch0/verify.py --label final   # 独立 10 场景
npm run docs:build
```

> 结论：批次 0 的 8 项热修全部落地并通过独立验收；`ENGINEERING-DIAGNOSIS.md` 中 P0 与最高优先级 P1 的"用户可感知 + 安全"缺陷已关闭。建议 review 后以 `git commit -s`（DCO）分批提交，再启动批次 1（缓存/配置接线/安全控制）。
