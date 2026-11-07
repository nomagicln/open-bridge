# 批次 0 独立验收报告（verification-batch0）

> **验收结论：批次 0 全部 10 个场景 PASS（10/10），0 FAIL，0 N/A。建议 ACCEPT。**
>
> 验收对象：OpenBridge 批次 0 热修（`FIX-PLAN.md` §1 A0-1…A0-8）
> 验收者：perf-analyst（独立验证者，未参与修复、未修改仓库源码/配置、未执行 git 写操作）
> 基线版本：`dd18374627c7896229b10b21106c9dee00f30a65`（`git archive HEAD` 只读快照）
> 验收版本：HEAD `dd18374627c7` + 工作树 `tree_hash=36c417575a6c0396`（冻结态；dirty 43 个文件）
>
> 基线运行：2026-11-07T21:24:29+0100　|　最终运行：2026-11-07T21:27:05+0100
> 验收 harness：`/tmp/verify_batch0/verify.py`（sha256[:16]=`9ecd33690e106d20`），二进制 sha256[:16]=`c24a9c53aab99397`

---

## 1. 结论摘要

| 指标 | 修复前基线 | 最终验收 |
| --- | :---: | :---: |
| 场景 PASS | 2 | **10** |
| 场景 FAIL | 8 | **0** |
| 场景 N/A | 0 | **0** |
| 子项 PASS（含 INFO/N-A 不计） | 26 | **50** |
| 子项 FAIL | 19 | **0** |

| # | 场景 | 验收关注点 | 修复前 | 修复后 |
| :---: | --- | --- | :---: | :---: |
| S1 | 构建与冷启动 <100ms | `go build ./cmd/ob` 成功；`--version` p50 < 100ms（README 冷启动声明）… | ✅ PASS | ✅ PASS |
| S2 | 输出格式 --json / --yaml / --output/-o | `ob run <app> pets list --json` 输出合法 JSON；`--yaml` 输出 YAML；`--output j… | ❌ FAIL | ✅ PASS |
| S3 | --profile 生效 / 未知 profile 报错 | `--profile prod` 请求打到 prod base_url（两个 mock 端口区分）；profile Headers/Quer… | ❌ FAIL | ✅ PASS |
| S4 | `ob <app> ...` 直连路由 | `ob <app> <res> <verb>`、`ob <app> --help`、`ob <app> --mcp` 均可达（A0-4）… | ❌ FAIL | ✅ PASS |
| S5 | 网络错误不回显 query 中的 secret | 模拟网络错误（不可达端口）时，stdout/stderr 不得出现 query 中的 secret（CLI 参数路径 + keyring q… | ❌ FAIL | ✅ PASS |
| S6 | `ob info -o json/yaml` 掩码 Header | `ob info <app> -o json` / `-o yaml` 中 Authorization、X-API-Key 等敏感 head… | ❌ FAIL | ✅ PASS |
| S7 | keyring 文件后端 + Authorization e2e | `OPENBRIDGE_KEYRING_PASSWORD` 显式启用文件后端；pre-seed 的 bearer 凭据在 `ob run` … | ❌ FAIL | ✅ PASS |
| S8 | `--generate curl` 不泄露 query secret | `protect_sensitive_info: true` 时生成的 curl 不得包含 query 中的 secret（D-03）；he… | ❌ FAIL | ✅ PASS |
| S9 | MCP stdio JSON-RPC 冒烟 | `initialize` 返回 serverInfo；`tools/list` 返回非空工具数组（tools 名/描述/schema 完整）… | ✅ PASS | ✅ PASS |
| S10 | 质量门禁 `go vet -tags=integration` 与 `go test -short` | `go vet ./...`、`go vet -tags=integration ./...` 无输出；`go test -short ./… | ❌ FAIL | ✅ PASS |

**修复前 8 个 FAIL 全部转 PASS**：S2（输出格式）、S3（profile）、S4（直连路由）、S5（错误回显 secret）、S6（info 掩码）、S7（认证全链路）、S8（codegen URL 掩码）、S10（integration tag 编译门禁）；S1（冷启动）与 S9（MCP 冒烟）为回归项，修复前后均 PASS。

### 1.1 与 Lead 侧独立结果的交叉核对

Lead 提供的独立参考（未参与本 harness 实现）：/tmp/lead_verify.py（22 项断言） → **22/22 PASS**；全仓 golangci-lint v2.15.0 0 issues；gofmt / goimports / vet / vet -tags 均干净；工作树相对 HEAD 共 43 个变更文件（36 modified + 7 new test/report 文件）。

| 核对项 | Lead 侧 | 本报告（独立 harness） | 一致性 |
| --- | --- | --- | :---: |
| 端到端功能 | 22/22 断言 PASS | 10/10 场景 PASS（50 个 PASS 子项） | ✅ 一致 |
| 构建 | 干净 | `go build ./...` rc=0、`go build ./cmd/ob` 22726466B / 0.26s | ✅ 一致 |
| vet / vet -tags | 干净 | `go vet ./...` + `go vet -tags=integration ./...` rc=0 | ✅ 一致 |
| 单元测试 | 干净 | `go test -short ./...` rc=0；冷缓存复核 `-count=1` 3.75s、13 包全部 ok | ✅ 一致 |
| gofmt | 干净 | `gofmt -l pkg cmd internal` 无输出 | ✅ 一致 |
| lint | golangci-lint v2.15.0 0 issues | 未重复执行（本任务 S10 未含 lint；以 Lead 结果为准） | ➖ 未交叉 |
| 工作树规模 | 43 变更文件（36M+7 new） | `git status --porcelain` = 43 行（36 M + 7 ??） | ✅ 一致 |

**差异声明**：两侧独立实现、独立断言，结果无冲突。本报告的差异价值在于：
① 用真实 `install → run → mock server` 全链路验证行为语义（而非仅函数级断言）；② 显式用 `OPENBRIDGE_KEYRING_PASSWORD` + 文件后端验证 D-12 与 D-01 的组合路径；③ 用 `git archive HEAD` 只读快照固化"修复前"对照，且每次运行自动校验收验过程未改动仓库（`repo_unchanged_by_verifier=true`）。

---

## 2. 验收方法与环境

### 2.1 原则

- **独立**：验证者不参与修复，不修改仓库源码/配置，不执行 git 写操作（无 commit/stash/worktree）；
- **可复现**：所有场景由 `/tmp/verify_batch0/verify.py` 自动化执行，输出原始 JSON/MD 与 mock 请求日志；
- **隔离**：`HOME=/tmp/verify_batch0/home`、`OPENBRIDGE_CONFIG_DIR=<HOME>/Library/Application Support/openbridge`，不触碰真实用户配置与 macOS Keychain（文件后端 + `OPENBRIDGE_KEYRING_PASSWORD`）；
- **修复前对照**：基线来自 `git archive HEAD` 的只读快照（`/tmp/verify_batch0/pristine`，HEAD `dd18374`），同一 harness、同一端口/凭据/canary 断言；修复后跑冻结工作树；
- **零污染自检**：每次运行比较运行前后 `git status --porcelain`，结果写入 `repo_unchanged_by_verifier`。

### 2.2 环境

| 项 | 值 |
| --- | --- |
| 仓库 | `/Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge` |
| 平台 | macOS arm64（Apple M3 Max），Go 1.27.3，`CGO_ENABLED=1` |
| 被测二进制 | `/tmp/verify_batch0/bin/ob`（22726466 B，sha256[:16]=`c24a9c53aab99397`；**必须命名为 `ob`**，否则 `cmd/ob/main.go` 进入 shim 分支） |
| 验收版本标识 | HEAD `dd18374627c7`，worktree `tree_hash=36c417575a6c0396`（`git diff` + untracked 清单的 sha256） |
| Mock API | `mock.py`：单进程双端口 `127.0.0.1:58131`（default）/`:58132`（prod），逐请求记录 JSONL；不可达端口 `:58139` 用于制造网络错误 |
| spec | `specs/pets.yaml`（OpenAPI 3.0.3，3 个 operation，含 query `api_key` 参数） |
| keyring | 文件后端，`OPENBRIDGE_KEYRING_PASSWORD=batch0-verify-passphrase`；`bin/keyseed`（Go module，`replace` 到被测仓库）预存凭据 |

### 2.3 harness 组成（`/tmp/verify_batch0/`）

| 文件 | 作用 |
| --- | --- |
| `verify.py` | 主验收脚本：构建、隔离 HOME、安装 4 个 app、patch profile、10 场景断言、JSON/MD 结果 |
| `mock.py` | 双端口 mock HTTP server，记录 method/path/headers/body 到 `requests.jsonl` |
| `specs/pets.yaml` | 手工构造 OpenAPI spec |
| `keyseed/`（Go module, replace） | `keyseed` 预存凭据（强制文件后端，规避 macOS Keychain 弹窗/挂起）；`yamlcheck` 用 yaml.v3 校验 YAML（宿主 Python 无 pyyaml） |
| `pristine/` | `git archive HEAD` 只读快照（修复前基线） |
| `results_baseline.json` / `results_final.json` | 逐子项原始证据 |
| `evidence_snippets.txt` | 关键命令的真实输出摘录（本报告 §4） |

### 2.4 被测 app 布局

| app | profile | base_url | 认证 | 用途 |
| --- | --- | --- | --- | --- |
| `petapi` | default | `:58131` | none | S2/S3/S9 基础功能 |
| `petapi` | prod | `:58132` | none；Headers `X-Profile-Header: from-prod`，QueryParams `profile_q=prod-val` | S3 profile |
| `petapi` | dead | `:58139`（不可达） | none | S3 备用 |
| `petauth` | default | `:58131` | bearer（keyring 预存 `AUTH_TOKEN_CANARY_7`）；Headers 含 `Authorization`/`X-API-Key`；`protect_sensitive_info: true` | S6/S7/S8 |
| `petdead` | default | `:58139`（不可达） | none | S5 CLI 参数 secret 回显 |
| `petdeadq` | default | `:58139`（不可达） | api_key @ query（keyring 预存 `LEAK_TOKEN_CANARY_5`） | S5 keyring query 凭据回显 |

> 说明：app 配置通过 `ob install` 生成后，在**隔离 HOME 内**用 `write_config` 覆写为测试 fixture（安装命令无 `--protect-sensitive-info` flag）。该操作只改 `/tmp` 下的测试配置，不涉及仓库。

---

## 3. 逐场景验收明细

### S1 构建与冷启动 <100ms

**验收目标**：`go build ./cmd/ob` 成功；`--version` p50 < 100ms（README 冷启动声明）

**通过标准**：二进制可构建；10 次 `--version` p50<100ms；退出码 0

**修复前**：✅ PASS　|　**修复后**：✅ PASS

<details open><summary>修复后子项（验收版本 <code>dd18374627c7 + worktree</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| go build ./cmd/ob | PASS | `binary 22726466 bytes, 0.25s` |
| --version exit code | PASS | `rc=0` |
| --version output mentions version | PASS | `ob version dev (commit: unknown, built: unknown)` |
| --version p50 < 100ms (measured 6.8ms) | PASS | `min=6.4ms p50=6.8ms max=8.0ms n=10` |

</details>

<details><summary>修复前子项（基线 <code>dd18374627c7</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| go build ./cmd/ob | PASS | `binary 22674258 bytes, 0.16s` |
| --version exit code | PASS | `rc=0` |
| --version output mentions version | PASS | `ob version dev (commit: unknown, built: unknown)` |
| --version p50 < 100ms (measured 7.7ms) | PASS | `min=6.1ms p50=7.7ms max=8.9ms n=10` |

</details>

**涉及代码（修复点）**：FIX-PLAN A0-8（文档表述）不涉及代码；本项在修复前后均应 PASS，用于确认修复未引入启动回归。

**复现命令**：

```bash
cd /tmp/verify_batch0
python3 verify.py --label final --only S1
```

---

### S2 输出格式 --json / --yaml / --output/-o

**验收目标**：`ob run <app> pets list --json` 输出合法 JSON；`--yaml` 输出 YAML；`--output json`/`-o json` 与 `--json` 数据一致；默认 YAML（契约 C2）

**通过标准**：4 种格式开关全部生效且数据一致；默认输出 YAML；`--yaml` 不是 JSON

**修复前**：❌ FAIL　|　**修复后**：✅ PASS

<details open><summary>修复后子项（验收版本 <code>dd18374627c7 + worktree</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| default run succeeds | PASS | `8.4ms, 60B` |
| --json emits valid JSON | PASS | `rc=0 keys=['pets']` |
| --output json emits valid JSON | PASS | `rc=0` |
| -o json emits valid JSON | PASS | `rc=0` |
| --json == --output json (data) | PASS | `equal=True` |
| --json == -o json (data) | PASS | `equal=True` |
| --yaml emits valid YAML | PASS | `rc=0 type=dict` |
| --yaml == --json (data) | PASS | `equal=True` |
| --yaml is YAML not JSON | PASS | `stdout is not JSON` |
| INFO unknown flag --badflag behaviour (informational) | INFO | `rc=0 ignored=True` |
| default output is YAML | PASS | `type=dict` |

</details>

<details><summary>修复前子项（基线 <code>dd18374627c7</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| default run succeeds | PASS | `0ms, 60B` |
| --json emits valid JSON | FAIL | `rc=0 parse-error=Expecting value: line 1 column 1 (char 0)` |
| --output json emits valid JSON | FAIL | `rc=0 parse-error=Expecting value: line 1 column 1 (char 0)` |
| -o json emits valid JSON | FAIL | `rc=0 parse-error=Expecting value: line 1 column 1 (char 0)` |
| --yaml emits valid YAML | PASS | `rc=0 type=dict` |
| --yaml is YAML not JSON | PASS | `stdout is not JSON` |
| INFO unknown flag --badflag behaviour (informational) | INFO | `rc=0 ignored=True` |
| default output is YAML | PASS | `type=dict` |

</details>

**涉及代码（修复点）**：契约 C2 / task-6（style-auditor）：`pkg/cli/handler.go` 新增 `parseCLIOptions`，输出格式在 `extractCLIFlags` 之前解析并显式传入 `formatAndPrintOutput`。

**复现命令**：

```bash
export HOME=/tmp/verify_batch0/home OPENBRIDGE_CONFIG_DIR="/tmp/verify_batch0/home/Library/Application Support/openbridge"
/tmp/verify_batch0/bin/ob run petapi pets list --json | python3 -m json.tool
/tmp/verify_batch0/bin/ob run petapi pets list --output json | python3 -m json.tool
/tmp/verify_batch0/bin/ob run petapi pets list --yaml
```

---

### S3 --profile 生效 / 未知 profile 报错

**验收目标**：`--profile prod` 请求打到 prod base_url（两个 mock 端口区分）；profile Headers/QueryParams 进入请求；未知 profile 非零退出并指明

**通过标准**：prod 端口收到 1 次请求、default 端口 0 次；X-Profile-Header/query 参数出现；未知 profile rc≠0 且错误含 profile 名与可用列表

**修复前**：❌ FAIL　|　**修复后**：✅ PASS

<details open><summary>修复后子项（验收版本 <code>dd18374627c7 + worktree</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| --profile prod run succeeds | PASS | `8.6ms` |
| --profile prod hits prod port | PASS | `prod_requests=1 default_requests=0` |
| --profile prod does NOT hit default port | PASS | `default_requests=0` |
| profile Headers applied (X-Profile-Header) | PASS | `got='from-prod'` |
| profile QueryParams applied (profile_q) | PASS | `got=['prod-val']` |
| unknown profile exits non-zero | PASS | `rc=1` |
| unknown profile error names profile | PASS | `profile "nope" not found; available profiles: dead, default, prod` |

</details>

<details><summary>修复前子项（基线 <code>dd18374627c7</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| --profile prod run succeeds | PASS | `0ms` |
| --profile prod hits prod port | FAIL | `prod_requests=0 default_requests=1` |
| --profile prod does NOT hit default port | FAIL | `default_requests=1` |
| unknown profile exits non-zero | FAIL | `rc=0` |
| unknown profile error names profile | FAIL | `pets:   - id: "1"     name: rex   - id: "2"     name: milo` |

</details>

**涉及代码（修复点）**：契约 C2 / task-6：`Handler.getProfile(appConfig, name)` 校验存在性；CLI 请求应用 `profile.Headers` 与 `profile.QueryParams`。

**复现命令**：

```bash
export HOME=/tmp/verify_batch0/home OPENBRIDGE_CONFIG_DIR="/tmp/verify_batch0/home/Library/Application Support/openbridge"
/tmp/verify_batch0/bin/ob run petapi pets list --profile prod --json
/tmp/verify_batch0/bin/ob run petapi pets list --profile nope; echo rc=$?
```

---

### S4 `ob <app> ...` 直连路由

**验收目标**：`ob <app> <res> <verb>`、`ob <app> --help`、`ob <app> --mcp` 均可达（A0-4）

**通过标准**：三条直连路径 rc=0；help 含资源列表；MCP initialize/tools-list 正常

**修复前**：❌ FAIL　|　**修复后**：✅ PASS

<details open><summary>修复后子项（验收版本 <code>dd18374627c7 + worktree</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| ob <app> <res> <verb> works | PASS | `rc=0 120B` |
| ob <app> --help works | PASS | `rc=0` |
| ob <app> --mcp reachable | PASS | `serverInfo.name='petapi' tools=3 sample=['createPet', 'getPet', 'listPets']` |

</details>

<details><summary>修复前子项（基线 <code>dd18374627c7</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| ob <app> <res> <verb> works | FAIL | `rc=1 0B` |
| ob <app> --help works | FAIL | `rc=1` |
| ob <app> --mcp reachable | FAIL | `initialize failed: None; stderr=unknown command "petapi" for "ob" ` |

</details>

**涉及代码（修复点）**：契约 C3 / task-7（arch-reviewer）：`rootCmd.Args = cobra.ArbitraryArgs` + `RunE` 调 `appRouter.ExecuteApp`；位置 `cmd/ob/main.go`、`pkg/router/router.go`。

**复现命令**：

```bash
export HOME=/tmp/verify_batch0/home OPENBRIDGE_CONFIG_DIR="/tmp/verify_batch0/home/Library/Application Support/openbridge"
/tmp/verify_batch0/bin/ob petapi pets list --json
/tmp/verify_batch0/bin/ob petapi --help
printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"v","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  | /tmp/verify_batch0/bin/ob petapi --mcp 2>/dev/null
```

---

### S5 网络错误不回显 query 中的 secret

**验收目标**：模拟网络错误（不可达端口）时，stdout/stderr 不得出现 query 中的 secret（CLI 参数路径 + keyring query 凭据路径，D-02）

**通过标准**：两路径 rc≠0；canary 字符串不出现；URL 中 query 值显示为 REDACTED

**修复前**：❌ FAIL　|　**修复后**：✅ PASS

<details open><summary>修复后子项（验收版本 <code>dd18374627c7 + worktree</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| network error path exercised (non-zero rc) | PASS | `rc=1` |
| network error does not echo query secret (CLI param) | PASS | `canary_in_output=False` |
| network error path exercised (keyring query auth) | PASS | `rc=1` |
| network error does not echo keyring query token | PASS | `canary_in_output=False` |

</details>

<details><summary>修复前子项（基线 <code>dd18374627c7</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| network error path exercised (non-zero rc) | PASS | `rc=1` |
| network error does not echo query secret (CLI param) | FAIL | `canary_in_output=True` |
| network error path exercised (keyring query auth) | PASS | `rc=1` |
| network error does not echo keyring query token | PASS | `canary_in_output=False` |

</details>

**涉及代码（修复点）**：契约 C1+安全修复 / task-5（credential-sec）：`pkg/cli/errors.go` 在网络错误格式化前统一 `redactURL`（清 query 值/userinfo）。

**复现命令**：

```bash
export HOME=/tmp/verify_batch0/home OPENBRIDGE_CONFIG_DIR="/tmp/verify_batch0/home/Library/Application Support/openbridge"
/tmp/verify_batch0/bin/ob run petdead pets list --api_key SECRET_LEAK_CANARY_5A 2>&1 | grep -c SECRET_LEAK_CANARY_5A   # 期望 0
/tmp/verify_batch0/bin/ob run petdeadq pets list 2>&1 | grep -c LEAK_TOKEN_CANARY_5                                # 期望 0
```

---

### S6 `ob info -o json/yaml` 掩码 Header

**验收目标**：`ob info <app> -o json` / `-o yaml` 中 Authorization、X-API-Key 等敏感 header 的值被掩码；非敏感 header 保留（D-04）

**通过标准**：json/yaml 均可解析；敏感值 canary 不出现（显示为 `Be****..._7` 形式）；非敏感 header 原样保留

**修复前**：❌ FAIL　|　**修复后**：✅ PASS

<details open><summary>修复后子项（验收版本 <code>dd18374627c7 + worktree</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| ob info -o json emits valid JSON | PASS | `1704B` |
| info JSON masks Authorization header | PASS | `canary_present=False` |
| info JSON masks X-API-Key header | PASS | `canary_present=False` |
| info JSON keeps non-sensitive header | PASS | `safe_present=True` |
| ob info -o yaml emits valid YAML | PASS | `506B` |
| info YAML masks Authorization header | PASS | `canary_present=False` |
| info YAML masks X-API-Key header | PASS | `canary_present=False` |

</details>

<details><summary>修复前子项（基线 <code>dd18374627c7</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| ob info -o json emits valid JSON | PASS | `1704B` |
| info JSON masks Authorization header | FAIL | `canary_present=True` |
| info JSON masks X-API-Key header | FAIL | `canary_present=True` |
| info JSON keeps non-sensitive header | PASS | `safe_present=True` |
| ob info -o yaml emits valid YAML | PASS | `506B` |
| info YAML masks Authorization header | FAIL | `canary_present=True` |
| info YAML masks X-API-Key header | FAIL | `canary_present=True` |

</details>

**涉及代码（修复点）**：契约 C3 / task-7：`cmd/ob/main.go` 的 `showAppInfo` 在 json/yaml 序列化前对 `Profiles[*].Headers`/`SpecFetchHeaders` 深拷贝掩码（复用 `isSensitiveHeader`/`maskValue`）。

**复现命令**：

```bash
export HOME=/tmp/verify_batch0/home OPENBRIDGE_CONFIG_DIR="/tmp/verify_batch0/home/Library/Application Support/openbridge"
/tmp/verify_batch0/bin/ob info petauth -o json | grep -c AUTH_TOKEN_CANARY_7   # 期望 0
/tmp/verify_batch0/bin/ob info petauth -o yaml | grep -c XAPIKEY_CANARY_6     # 期望 0
```

---

### S7 keyring 文件后端 + Authorization e2e

**验收目标**：`OPENBRIDGE_KEYRING_PASSWORD` 显式启用文件后端；pre-seed 的 bearer 凭据在 `ob run` 时被读取并注入 `Authorization: Bearer <token>`（D-01 + D-12）

**通过标准**：`Manager.Backend()==file`；mock server 记录 `Authorization: Bearer AUTH_TOKEN_CANARY_7`；请求路径 /pets

**修复前**：❌ FAIL　|　**修复后**：✅ PASS

<details open><summary>修复后子项（验收版本 <code>dd18374627c7 + worktree</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| keyseed stored bearer credential | PASS | `seed={"backend_default_manager":"file","file_written":true,"keyring_dir":"/tmp/verify_batch0/home/Library/Application Support/openbridge/keyring","stored_token_len":` |
| OPENBRIDGE_KEYRING_PASSWORD selects file backend | PASS | `default_manager_backend='file' (expected 'file'); stderr='seed: probe result: file'` |
| authenticated run succeeds | PASS | `rc=0 10.0ms` |
| request carries Authorization: Bearer <seeded token> | PASS | `authorization='Bearer AUTH_TOKEN_CANARY_7'` |
| request path is /pets | PASS | `path='/pets'` |

</details>

<details><summary>修复前子项（基线 <code>dd18374627c7</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| keyseed stored bearer credential | PASS | `seed={"backend_default_manager":"keychain","file_written":true,"keyring_dir":"/tmp/verify_batch0/home/Library/Application Support/openbridge/keyring","stored_token_l` |
| OPENBRIDGE_KEYRING_PASSWORD selects file backend | FAIL | `default_manager_backend='keychain' (expected 'file'); stderr='seed: probe result: keychain'` |
| authenticated run succeeds | PASS | `rc=0 0ms` |
| request carries Authorization: Bearer <seeded token> | FAIL | `authorization=None` |
| request path is /pets | PASS | `path='/pets'` |

</details>

**涉及代码（修复点）**：契约 C5 / task-5：`pkg/credential/keyring.go` 的 `getPasswordFunc` 读 `OPENBRIDGE_KEYRING_PASSWORD` 并选 FileBackend、`Manager.Backend()` 返回真实后端；契约 C1 / task-6：`pkg/cli/handler.go` 的 `buildRequest` 把真实 `appName` 传给 `InjectAuth`。

**复现命令**：

```bash
# 1) seed（同一 HOME/config_dir/keyring 目录）
OPENBRIDGE_KEYRING_PASSWORD=batch0-verify-passphrase \
  /tmp/verify_batch0/bin/keyseed --app petauth --profile default --type bearer \
  --token AUTH_TOKEN_CANARY_7 --dir "/tmp/verify_batch0/home/Library/Application Support/openbridge/keyring"
# 2) run（同一 env）+ 查 mock 记录
export HOME=/tmp/verify_batch0/home OPENBRIDGE_CONFIG_DIR="/tmp/verify_batch0/home/Library/Application Support/openbridge"
OPENBRIDGE_KEYRING_PASSWORD=batch0-verify-passphrase /tmp/verify_batch0/bin/ob run petauth pets list --json
python3 -c "import json;print([json.loads(l)['headers'].get('Authorization') for l in open('/tmp/verify_batch0/requests.jsonl')])" 
```

---

### S8 `--generate curl` 不泄露 query secret

**验收目标**：`protect_sensitive_info: true` 时生成的 curl 不得包含 query 中的 secret（D-03）；header 凭据同样不泄露

**通过标准**：curl 生成成功；query 值显示 `<YOUR_API_KEY>`；Authorization/X-API-Key 值同样为占位符；非敏感 header 保留

**修复前**：❌ FAIL　|　**修复后**：✅ PASS

<details open><summary>修复后子项（验收版本 <code>dd18374627c7 + worktree</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| --generate curl succeeds | PASS | `rc=0` |
| generated curl does not leak query secret | PASS | `canary_in_output=False` |
| generated curl does not leak bearer header token | PASS | `token_in_output=False` |
| generated output looks like curl | PASS | `curl -X GET 'http://127.0.0.1:58131/pets?api_key=<YOUR_API_KEY>' \   -H 'Authorization: Bearer <YOUR_API_KEY>' \` |

</details>

<details><summary>修复前子项（基线 <code>dd18374627c7</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| --generate curl succeeds | PASS | `rc=0` |
| generated curl does not leak query secret | FAIL | `canary_in_output=True` |
| generated curl does not leak bearer header token | PASS | `token_in_output=False` |
| generated output looks like curl | PASS | `curl -X GET 'http://127.0.0.1:58131/pets?api_key=SECRET_Q_CANARY_8'` |

</details>

**涉及代码（修复点）**：安全修复 / task-5：`pkg/codegen/` 四个生成器统一走 `redactURL`（`codegen.go` 已有 header/body 掩码，缺 URL）。

**复现命令**：

```bash
export HOME=/tmp/verify_batch0/home OPENBRIDGE_CONFIG_DIR="/tmp/verify_batch0/home/Library/Application Support/openbridge"
/tmp/verify_batch0/bin/ob run petauth pets list --api_key SECRET_Q_CANARY_8 --generate curl \
  | grep -c SECRET_Q_CANARY_8   # 期望 0
```

---

### S9 MCP stdio JSON-RPC 冒烟

**验收目标**：`initialize` 返回 serverInfo；`tools/list` 返回非空工具数组（tools 名/描述/schema 完整）

**通过标准**：serverInfo.name=petapi；tools 数量>0 且每个含 name/description/inputSchema

**修复前**：✅ PASS　|　**修复后**：✅ PASS

<details open><summary>修复后子项（验收版本 <code>dd18374627c7 + worktree</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| MCP initialize + tools/list (ob run <app> --mcp) | PASS | `serverInfo.name='petapi' tools=3 sample=['createPet', 'getPet', 'listPets']` |

</details>

<details><summary>修复前子项（基线 <code>dd18374627c7</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| MCP initialize + tools/list (ob run <app> --mcp) | PASS | `serverInfo.name='petapi' tools=3 sample=['createPet', 'getPet', 'listPets']` |

</details>

**涉及代码（修复点）**：无代码变更要求（回归项）；task-8 给 MCP client 增加 Timeout，不应影响 stdio 协议行为。

**复现命令**：

```bash
export HOME=/tmp/verify_batch0/home OPENBRIDGE_CONFIG_DIR="/tmp/verify_batch0/home/Library/Application Support/openbridge"
printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"v","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  | /tmp/verify_batch0/bin/ob run petapi --mcp 2>/dev/null
```

---

### S10 质量门禁 `go vet -tags=integration` 与 `go test -short`

**验收目标**：`go vet ./...`、`go vet -tags=integration ./...` 无输出；`go test -short ./...` 全绿；gofmt 无漂移（A0-5）

**通过标准**：5 项全部 rc=0；gofmt 无未格式化文件

**修复前**：❌ FAIL　|　**修复后**：✅ PASS

<details open><summary>修复后子项（验收版本 <code>dd18374627c7 + worktree</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| go build ./... (1.6s) | PASS | `` |
| go vet ./... (0.3s) | PASS | `` |
| go vet -tags=integration ./... (0.3s) | PASS | `` |
| go test -short ./... (0.3s) | PASS | `` |
| gofmt -l pkg cmd internal (informational) | PASS | `unformatted=[]` |

</details>

<details><summary>修复前子项（基线 <code>dd18374627c7</code>）</summary>

| 子项 | 结果 | 证据 |
| --- | --- | --- |
| go build ./... (1.4s) | PASS | `` |
| go vet ./... (0.2s) | PASS | `` |
| go vet -tags=integration ./... (0.3s) | FAIL | `# github.com/nomagicln/open-bridge/pkg/credential # [github.com/nomagicln/open-bridge/pkg/credential] vet: pkg/credential/keyring_test.go:219:6: TestGetPlatformInfo redeclared in this block` |
| go test -short ./... (0.3s) | PASS | `` |
| gofmt -l pkg cmd internal (informational) | PASS | `unformatted=[]` |

</details>

**涉及代码（修复点）**：契约 C4 / task-8（process-auditor）：4 个 `*_integration_test.go` 补 `//go:build integration`；重命名 `pkg/credential/keyring_integration_test.go` 中与 `keyring_test.go` 冲突的函数。

**复现命令**：

```bash
cd /Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge
go build ./... && go vet ./... && go vet -tags=integration ./... && go test -short ./... && gofmt -l pkg cmd internal
```

---

## 4. 关键证据摘录（真实命令输出）

> 以下输出来自冻结工作树的已构建二进制（同一隔离 HOME/keyring），与 `results_final.json` 的机器断言相互印证。

### A. S2: `ob run petapi pets list --json`（合法 JSON）

{
  "pets": [
    {
      "id": "1",
      "name": "rex"
    },
    {
      "id": "2",
      "name": "milo"
    }
  ]
}

### B. S2: `ob run petapi pets list --yaml`（合法 YAML）

pets:
  - id: "1"
    name: rex
  - id: "2"
    name: milo

### C. S2: `ob run petapi pets list`（无 flag，默认 YAML）

pets:
  - id: "1"
    name: rex
  - id: "2"
    name: milo

### D. S3: 未知 profile 报错（非零退出并列出可用 profile）

profile "nope" not found; available profiles: dead, default, prod
exit=1

### E. S4: 直连路由 `ob petapi pets list --json`

{
  "pets": [
    {
      "id": "1",
exit=0

### F. S4: 直连路由 `ob petapi --help`

Usage: petapi <resource> <verb> [flags]

Description: Pet Verify API

Available Resources:
  pets

Common Verbs:
  create, list, get, update, delete, apply

### G. S5: 网络错误不回显 CLI 参数中的 query secret（已 REDACTED）

Error: Network connectivity issue.

Failed to connect to: http://127.0.0.1:58139/pets?api_key=REDACTED
Reason: dial tcp 127.0.0.1:58139: connect: connection refused

Troubleshooting:
  - Check your internet connection
  - Verify the API endpoint is accessible

### G2. S5: 网络错误不回显 keyring query 凭据（已 REDACTED）

Error: Network connectivity issue.

Failed to connect to: http://127.0.0.1:58139/pets?api_key=REDACTED
Reason: dial tcp 127.0.0.1:58139: connect: connection refused

Troubleshooting:
  - Check your internet connection
  - Verify the API endpoint is accessible

### H. S6: `ob info petauth -o json` 中敏感 Header 被掩码

{
  "Authorization": "Be**********************_7",
  "X-API-Key": "XA************_6",
  "X-Safe-Header": "safe-value"
}

### H2. S6: `ob info petauth -o yaml` 中敏感 Header 被掩码

headers:
            Authorization: Be**********************_7
            X-API-Key: XA************_6
            X-Safe-Header: safe-value

### I. S7: mock server 记录到认证请求（Authorization 来自文件后端 keyring）

{
  "port": 58131,
  "method": "GET",
  "path": "/pets",
  "Authorization": "Bearer AUTH_TOKEN_CANARY_7",
  "User-Agent": "Go-http-client/1.1"
}

### J. S8: `--generate curl` 输出中 URL/Header 凭据被掩码

curl -X GET 'http://127.0.0.1:58131/pets?api_key=<YOUR_API_KEY>' \
  -H 'Authorization: Bearer <YOUR_API_KEY>' \
  -H 'X-Api-Key: <YOUR_API_KEY>' \
  -H 'X-Safe-Header: safe-value'

### K. S9: MCP stdio initialize + tools/list 响应

initialize.response = {"serverInfo": {"name": "petapi", "version": "dev"}, "protocolVersion": "2025-06-18", "capabilities": ["logging", "tools"]}
tools/list.count = 3
tools/list.names = ["createPet", "getPet", "listPets"]
tools[0].keys = ["description", "inputSchema", "name"]
tools[0].inputSchema = {"properties": {"id": {"type": "string"}, "name": {"type": "string"}}, "required": ["id", "name"], "type": "object"}

### L. S1: `ob --version`（启动 <100ms）

ob version dev (commit: unknown, built: unknown)

### M. S10: 质量门禁计时结果

hot-cache run（verify.py 内，go test 缓存命中）:
- go build ./... (1.6s): PASS
- go vet ./... (0.3s): PASS
- go vet -tags=integration ./... (0.3s): PASS
- go test -short ./... (0.3s): PASS（缓存）
- gofmt -l pkg cmd internal (informational): PASS

cold-cache 复核（验收后手工执行，-count=1）:
$ go test -short -count=1 ./...      # 3.75s wall，13 个包全部 ok，2 个包无测试文件
ok  github.com/nomagicln/open-bridge/cmd/ob            0.695s
ok  github.com/nomagicln/open-bridge/internal/proptest   2.967s
ok  github.com/nomagicln/open-bridge/pkg/cli            2.595s
ok  github.com/nomagicln/open-bridge/pkg/codegen        0.416s
ok  github.com/nomagicln/open-bridge/pkg/completion     1.589s
ok  github.com/nomagicln/open-bridge/pkg/config         2.742s
ok  github.com/nomagicln/open-bridge/pkg/credential     0.905s
ok  github.com/nomagicln/open-bridge/pkg/mcp            1.844s
ok  github.com/nomagicln/open-bridge/pkg/request        1.139s
ok  github.com/nomagicln/open-bridge/pkg/router         2.323s
ok  github.com/nomagicln/open-bridge/pkg/semantic       2.049s
ok  github.com/nomagicln/open-bridge/pkg/spec           2.823s
$ go vet -tags=integration ./...     # rc=0，无输出

### N. 构建版本标识（HEAD / worktree hash / binary hash / harness hash）

git HEAD (base): dd18374627c7896229b10b21106c9dee00f30a65
worktree tree_hash: 36c417575a6c0396
dirty files: 43
binary: /tmp/verify_batch0/bin/ob 22726466 bytes sha256[:16]=c24a9c53aab99397
verifier sha256[:16]: 9ecd33690e106d20

---

## 5. 覆盖矩阵（task-9 十场景 ↔ 诊断 ID ↔ FIX-PLAN 任务）

| 场景 | 诊断 ID | FIX-PLAN 项 | 负责人 | 修复前 → 修复后 |
| :---: | --- | --- | --- | :---: |
| S1 | README 冷启动声明 | A0-8（文档） | Lead | PASS → PASS |
| S2 | D-07 | A0-3 | cli-flags / style-auditor | FAIL → **PASS** |
| S3 | D-08（部分） | A0-3b | cli-flags / style-auditor | FAIL → **PASS** |
| S4 | D-11 | A0-4 | routing-info / arch-reviewer | FAIL → **PASS** |
| S5 | D-02 | A0-2 | credential-sec | FAIL → **PASS** |
| S6 | D-04 | A0-2 | routing-info / arch-reviewer | FAIL → **PASS** |
| S7 | D-01 + D-12 | A0-1 / A0-7 | credential-sec + cli-flags | FAIL → **PASS** |
| S8 | D-03 | A0-2 | credential-sec | FAIL → **PASS** |
| S9 | 回归（MCP 协议） | A0-6 副作用面 | test-infra / process-auditor | PASS → PASS |
| S10 | D-13 / D-14 | A0-5 | test-infra / process-auditor | FAIL → **PASS** |

---

## 6. 残留观察与范围说明（不影响 ACCEPT）

| 编号 | 观察 | 级别 | 说明 |
| :---: | --- | --- | --- |
| R1 | 未知 flag 仍被静默忽略 | INFO | `ob run petapi pets list --badflag` 退出码 0 且行为不变（INFO 子项，修复前后一致）。FIX-PLAN A0-3 的验收范围只覆盖 `--json/--yaml/--output/-o`；诊断 D-07 把"未知 flag 静默忽略"列为次要症状，本批次未要求修复，建议列入批次 1（严格 flag 校验 + 拼写建议）。 |
| R2 | S1 的 <100ms 只覆盖进程启动 | 范围说明 | 本场景按 task-9 要求只测 `--version` 的进程启动（p50 6.5ms），不含 spec 解析；大 spec 的命令级成本属批次 1（性能报告 F1/F2）范围。 |
| R3 | S10 未运行 `-tags=integration` 测试套件 | 范围说明 | task-9 S10 只要求记录 `go vet -tags=integration ./...` 与 `go test -short ./...` 结果；`go test -tags=integration ./...` 由 Lead 在汇总阶段执行（FIX-PLAN §4 矩阵），本报告记录其可编译性。 |
| R4 | S3 只覆盖 profile 选择与 Headers/QueryParams | 范围说明 | profile 级 `TLSConfig`/`RetryConfig` 接线属批次 1（FIX-PLAN 明确本轮不做），故未纳入 S3 断言；HTTP client 30s 超时（A0-6）由静态检查+代码审阅覆盖，本 harness 不做慢服务计时（会拖长验收时间）。 |

---

## 7. 复现指南（完整）

```bash
# 0) 依赖：macOS/Linux + Go 1.25+；无需网络（mock server 全本地回环）

# 1) 修复前基线（只读快照，不修改仓库、不执行 git 写操作）
mkdir -p /tmp/verify_batch0/pristine
cd /Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge
git archive HEAD | tar -x -C /tmp/verify_batch0/pristine
(git rev-parse HEAD) > /tmp/verify_batch0/pristine/.snapshot_head
python3 /tmp/verify_batch0/verify.py --label baseline --repo /tmp/verify_batch0/pristine

# 2) 最终验收（冻结工作树）
python3 /tmp/verify_batch0/verify.py --label final

# 3) 单场景复跑（如 S7 认证链路）
python3 /tmp/verify_batch0/verify.py --label final --only S7

# 4) 生成报告
python3 /tmp/verify_batch0/make_report.py

# 5) 结果与证据
cat /tmp/verify_batch0/results_final.json
cat /tmp/verify_batch0/requests.jsonl     # mock 收到的每个请求
cat /tmp/verify_batch0/evidence_snippets.txt
```

### 7.1 断言不通过时的排查建议

- **S1 超时/慢**：确认二进制名为 `ob`（`os.Args[0]` 决定 shim 分支）；确认机器无 Gatekeeper 首次评估（首次运行 ~1s 属环境伪影）。
- **S2/S3 失败**：确认 `ob run <app> ...` 使用的是安装后的 app 配置（`ob list` 可见）；`--profile prod` 的 base_url 必须是 `:58132`。
- **S7 失败**：确认 `keyseed` 与 `ob` 使用**同一** `HOME`/`OPENBRIDGE_CONFIG_DIR`/`OPENBRIDGE_KEYRING_PASSWORD`；`keyseed` 输出 `backend_default_manager` 应为 `file`（修复前为 `keychain`，可作为 D-12 的判定信号）。
- **S9 失败**：MCP stdio 是行分隔 JSON；必须按 `initialize → notifications/initialized → tools/list` 顺序发送，且保持 stdin 打开（管道一次性喂入后立即 EOF 会导致 `server is closing: EOF`）。
- **S10 失败**：integration tag 相关的 vet 失败多为测试文件缺 `//go:build integration` 或重名函数；按 `go vet -tags=integration ./...` 的首条错误定位。

---

## 8. 原始证据索引

| 文件 | 内容 |
| --- | --- |
| `/tmp/verify_batch0/results_baseline.json` | 修复前基线逐子项 status/evidence（HEAD `dd18374`） |
| `/tmp/verify_batch0/results_final.json` | 最终验收逐子项 status/evidence（含 tree_hash/binary hash） |
| `/tmp/verify_batch0/results_*.md` | 脚本自动生成的结果汇总表 |
| `/tmp/verify_batch0/requests.jsonl` | mock server 收到的每个请求（含 Authorization 断言依据） |
| `/tmp/verify_batch0/evidence_snippets.txt` | §4 的关键命令输出原文 |
| `/tmp/verify_batch0/verify.py` | 验收脚本（全部断言） |
| `/tmp/verify_batch0/make_report.py` | 本报告生成脚本 |
| `/tmp/verify_batch0/run_baseline.log` / `run_final.log` | 运行日志 |
| `/tmp/verify_batch0/mcp_evidence.txt` | MCP initialize/tools-list 原始响应 |

### 8.1 验收过程未改动仓库的证明

- `results_final.json.repo.repo_unchanged_by_verifier = True`；
- 运行前后 `git status --porcelain` 均为 43 行（43 个文件，为批次 0 修复本身的变更，非验证者引入）；
- 验证者唯一写入仓库的文件是本报告 `.diagnosis/verification-batch0.md`（task-9 指派的交付物）。

---

## 9. 最终结论

**批次 0 热修通过独立验收：10/10 场景 PASS，修复前 8 个 FAIL 全部转 PASS，无回归、无新增风险。**

- 安全类（D-01/D-02/D-03/D-04/D-12）五条用户可触发缺陷均已在真实 e2e 链路上验证修复：
  凭据成功注入 `Authorization`、网络错误只显示 `api_key=REDACTED`、`info`/`--generate curl` 输出掩码、
  `OPENBRIDGE_KEYRING_PASSWORD` 正确选择文件后端且 `Backend()` 反映真实后端；
- CLI 语义类（D-07/D-08/D-11）修复后行为与 FIX-PLAN 契约 C2/C3 一致，错误信息可操作（未知 profile 列出可用项）；
- 质量门禁类（D-13/D-14/A0-5）`go vet -tags=integration`、`go test -short`、`gofmt` 全部干净；
- 回归项 S1（`--version` p50 见 §3 S1 实测值，本机 6.8ms）、S9（MCP stdio 3 tools）保持正常。

**建议**：ACCEPT 并进入 Lead 汇总（`FIXES-BATCH0.md`）。批次 1 跟进 R1（未知 flag 静默忽略）与性能类（`ENGINEERING-DIAGNOSIS.md` §2.6 / `.diagnosis/performance.md` F1/F2：持久化解析缓存空壳、大 spec 每命令重解析）。

（报告结束）
