# OpenBridge 代码风格与可维护性诊断报告

| 项目 | 值 |
|---|---|
| 仓库 | `github.com/nomagicln/open-bridge`（本地路径 `/Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge`） |
| 评审基线 | 工作区 HEAD `dd18374`（*fix: resolve linting issues and improve code quality*），`git status` 干净 |
| 代码规模 | 源码 **18,420** LOC / 测试 **17,527** LOC；Go 文件 81 个（非测试 42、测试 39） |
| 评审范围 | 11 个风格/可维护性维度 + lint 宪法（`.golangci.yml`）符合度 + AGENTS.md 符合度 |
| 评审者 | style-auditor（Agent Teams，独立于实现） |
| 报告日期 | 2026-11-07 |
| 报告路径 | `.diagnosis/style.md` |
| 硬约束 | 未修改任何源码/配置/`.golangci.yml`/`go.mod`；未执行 git 写操作；未运行任何 `-w/--fix`；仅写本文件 |

> 阅读提示：本报告中的每个发现都有 `file:line` 证据；`P0/P1/P2/P3` 分别表示"阻断/严重/一般/建议"。
> 本报告把"**工具不可用/配置不兼容**"本身作为发现 F-01/F-02 记录，全部结论均标注了其置信来源（工具实测 / 等效替代 / 人工静态分析）。

---

## 0. 执行摘要

### 0.1 一句话结论

**代码格式化层面近乎满分（gofmt/goimports 全绿、vet 全绿、导出注释 94.9%），但"工具链不可复现 + 配置与代码不自洽 + 错误语义靠字符串 + 复杂度热点靠 `//nolint` 放行"这四件事，使 `.golangci.yml` 这份"宪法"目前更接近于"装饰"而非"门禁"。**

### 0.2 维度评分卡

| # | 维度 | 结论 | 最严重发现 | 评级 |
|---|---|---|---|---|
| 1 | 格式与导入 | gofmt=0、goimports=0、gofumpt=20 文件 | F-32 | A- |
| 2 | 复杂度热点 | 10 个函数超 funlen；2 个热点靠 `//nolint` 放行；阈值偏宽 | F-03/F-04/F-25 | C+ |
| 3 | 重复代码 | dupl 9 组（2 组非测试文件未抑制）；另有 4 类人工近似块 | F-05/F-10 | C+ |
| 4 | 命名 | receiver 100% 一致；42 个 `Get*`；`baseUrlInput`、`GetFd`、`TestParameters` | F-26 | B- |
| 5 | 注释与文档 | 导出注释 94.9%；但包注释复制 7 份、`nolint` 占位 package doc | F-19/F-20 | B |
| 6 | 错误处理风格 | **无 sentinel error**、8 处字符串匹配分类、22 处 `%v` 丢链、115 处裸 `return err` | F-07/F-08/F-09 | D+ |
| 7 | 魔法值与 stringly-typed | `mnd` 被禁用；auth/format/scheme 全靠字符串散落 | F-14/F-15 | C |
| 8 | 死代码与过度导出 | 195 个导出函数仅本包使用；internal/ 覆盖不足 | F-16 | C |
| 9 | go.mod 依赖卫生 | ghodss/yaml 已归档 + yaml.v2/v3 双栈；go directive 1.25.5 与 CI 漂移 | F-18 | C+ |
| 10 | AGENTS.md 符合度 | §4.1 错误包装、§3.3 build tag、§4.2 接口化、§3.2 property test 均违反 | F-08/F-10/F-21/F-22 | D+ |
| 11 | 一致性 | 巨型文件、输出风格分裂、context 不传递 | F-12/F-17/F-29 | C |

### 0.3 发现总表（按严重度排序）

| ID | 严重度 | 维度 | 一句话 | 主要证据 |
|---|---|---|---|---|
| F-01 | **P1** | 工具链 | 本机 golangci-lint v1/v2 均无法运行，宪法从未被真实验证 | `golangci-lint run` 报配置错误；v2.8.0 panic |
| F-02 | **P1** | 工具链 | lint 版本三处漂移（Makefile/CI 均 `latest` + 本地 Go 1.27） | `Makefile:30,137-140`；`.github/workflows/ci.yml:105-109` |
| F-03 | **P1** | 复杂度 | 最复杂两个函数用 `// nolint` 直接豁免，`nolintlint` 未启用 | `pkg/cli/args.go:313-314,523-524` |
| F-04 | **P1** | 复杂度 | 若 funlen(50/40) 生效，至少 6 个未抑制函数报错 | `cmd/ob/main.go:102`、`pkg/semantic/resource.go:295,354` 等 |
| F-05 | **P1** | 重复 | dupl(70) 在非测试文件命中 2 组且无 `//nolint` | `pkg/config/test_helpers.go:53` vs `pkg/semantic/test_helpers.go:12` |
| F-06 | **P1** | 复杂度 | revive `max-control-nesting: 3` 将命中 ≥7 个 nest≧4 函数 | `pkg/completion/completion.go:218`（nest=5）等 |
| F-07 | **P1** | 错误 | 错误分类依赖 `strings.Contains(err.Error(), ...)` 字符串匹配 | `pkg/cli/errors.go:71-84`、`pkg/spec/parser.go:461,479` |
| F-08 | **P1** | 错误 | 全仓 0 个导出 sentinel error，直接违反 AGENTS §4.1 | `pkg/mcp/shared.go:13`（唯一 unexported） |
| F-09 | **P1** | 错误 | `%w` 包装率 54%，22 处 `%v` 丢链 + 115 处裸 `return err` | `pkg/mcp/progressive.go:317-491`、`pkg/config/*` |
| F-10 | **P1** | 测试 | 3/4 集成测试缺 `//go:build integration`；2 个孤儿测试文件 | `pkg/config/install_integration_test.go:1` 等 |
| F-11 | **P1** | 测试 | 1347 行 TUI 状态机、892 行 CLI handler 零单元测试 | `pkg/tui/install/model.go`、`pkg/cli/handler.go` |
| F-12 | P2 | 组织 | 巨型文件/包内职责混杂（config 包 5059 行 8 文件） | `pkg/config/config.go:1-924`、`cmd/ob/main.go:1-1057` |
| F-13 | P2 | 组织 | 5 个 `test_helpers.go` 中 4 个 import `testing`，被编入生产包 | `pkg/config/test_helpers.go:4-9` 等 |
| F-14 | P2 | 魔法值 | `mnd` 关闭：权限位 0644×6、30s 超时×6、sleep 50ms 等无常量 | `pkg/config/spec_cache.go:551,557`、`pkg/config/config.go:843` |
| F-15 | P2 | stringly | auth 类型/输出格式/URL scheme 全字符串散落，无枚举常量 | `pkg/spec/parser.go:45,375-379`、`pkg/tui/install/model.go:229,876-926` |
| F-16 | P2 | 导出面 | 195 个导出函数仓库内仅本包使用（config/credential 最重） | 见 §9 表 |
| F-17 | P2 | Context | 库代码 10 处新建 `context.Background()`，公开 API 无 ctx 参数 | `pkg/cli/handler.go:84`、`pkg/spec/parser.go:191,534,590,831` |
| F-18 | P2 | 依赖 | ghodss/yaml 已归档且拖入 yaml.v2，与 yaml.v3 并存；go 1.25.5 漂移 | `go.mod:3,12,17,19` |
| F-19 | P2 | 注释 | 24 个导出符号缺注释；`revive.exported`/`package-comments` 未启用 | `pkg/credential/security.go:214,322` 等 |
| F-20 | P2 | 注释 | package 注释在 7 个 config 文件整段复制；`//nolint` 占据 package doc | `pkg/config/*.go:1`、`pkg/mcp/test_helpers.go:1` |
| F-21 | P2 | AGENTS | §4.2"依赖定义为接口"普遍未执行，无 mock | `pkg/cli/handler.go:31-37` |
| F-22 | P2 | AGENTS | §3.2 parser/formatter/codegen 无 property test | `pkg/spec/parser.go`、`pkg/spec/format.go`、`pkg/codegen` |
| F-23 | P2 | 组织 | TUI 单文件 40+ Step 常量 + 巨型 Update switch | `pkg/tui/install/model.go:20-38,285,1347` |
| F-24 | P3 | lint 配置 | `.golangci.yml` 4 条排除规则全部陈旧（被豁免函数复杂度仅 5-7） | `.golangci.yml:95-116` |
| F-25 | P3 | 阈值 | gocyclo/gocognit=25 偏宽，放过 cyclo19/cognit19 的"亚热点" | §3.3 |
| F-26 | P3 | 命名 | 42 个导出 `Get*`；`GetFd`、`baseUrlInput`、`TestParameters`；import 别名大小写不一 | §5 |
| F-27 | P3 | 错误 | 8 处错误字符串大写开头（revive error-strings 会对齐） | `pkg/credential/security.go:76,190` 等 |
| F-28 | P3 | 注释 | 中英混杂 1 处；`//nolint` 与 `// nolint` 混用；1 处 nolint 无理由 | `pkg/codegen/nodejs.go:12`、`pkg/tui/install/model.go:605` |
| F-29 | P3 | 一致性 | 输出风格四套并存（fmt.Printf×188 / lipgloss / 模板 / emoji） | `cmd/ob/main.go`、`pkg/tui/install`、`pkg/mcp` |
| F-30 | P3 | 测试命名 | 集成测试命名/前缀不统一；`real_api_test.go` 不符合 `*_integration_test.go` 约定 | `internal/integration/real_api_test.go`、`pkg/request/builder_integration_test.go:1` |
| F-31 | P3 | 组织 | `internal/integration` 是"仅测试"包（无任何非测试 .go 文件） | `internal/integration/` |
| F-32 | P3 | 格式 | 20 个文件未通过 gofumpt（仓库未启用 gofumpt formatter） | 见 §2 |
| F-33 | P3 | 治理 | 无 TODO/FIXME/XXX 标记（优点），但也无技术债追踪；`issues: {}` 空配置 | `grep -rn TODO` 无结果；`.golangci.yml:14` |
| F-34 | P3 | 工程卫生 | Go 仓库根同时存在 npm/vitepress 工程（package.json + node_modules），Makefile 无 Node 目标 | `package.json:1-29` |

---

## 1. 方法与工具链实测（含"工具不可用"发现）

### 1.1 环境矩阵

| 组件 | 实测值 | 仓库期望值（配置/CI） | 偏差 |
|---|---|---|---|
| 本机 Go | `go1.27.3 darwin/arm64` | `go.mod:3` 写 `go 1.25.5`；CI `go-version: "1.25"` | ⚠️ 本地比模块声明的工具链高 2 个 minor |
| 本机 golangci-lint | `v1.64.8`（built with go1.26.1） | `.golangci.yml` 是 **v2 格式**（`version: "2"`）；Makefile 装 v2 | ❌ v1 不认 v2 配置 |
| 备用 golangci-lint | `golangci-lint-v2 2.8.0`（built with go1.25.5） | Makefile `latest`；CI action v9 `version: latest` | ⚠️ 与本地 Go 1.27.3 元数据不兼容 |
| staticcheck | `2025.1`（built with go1.24?） | 未在 lint 配置中 | ❌ 无法读取 go1.27 export data |
| gofmt / goimports | 可用，结果全绿 | `.golangci.yml:formatters: goimports` | ✅ |
| gofumpt | 已安装，20 文件可改善 | 未启用 | ⚠️ |
| gocyclo / gocognit / dupl / errcheck / misspell | 本报告期间临时 `go install`（模块缓存可下载），可用 | 对应 linter 均已在配置启用 | ✅ 等效替代 |
| maintidx | 安装成功但**运行失败**（`package "github.com/leanovate/gopter" without types`） | `.golangci.yml:86-87` 启用 | ❌ 未验证 |

### 1.2 命令执行记录（真实输出）

| 命令 | 结果 | 说明 |
|---|---|---|
| `golangci-lint run ./...`（v1.64.8） | ❌ `Error: can't load config: sort-results should be 'true' to use sort-order` | v1 的 schema 与 v2 配置不兼容 |
| `golangci-lint-v2 run ./...`（2.8.0） | ❌ panic：`file requires newer Go version go1.27 (application built with go1.25)`（exit 2） | 二进制内置 go/types 版本 1.25 < 本机 go1.27.3 报告的文件版本 |
| `golangci-lint-v2 config verify` | ❌ exit 3：拉取 `https://golangci-lint.run/jsonschema/golangci.v2.8.jsonschema.json` 超时 | 配置 schema 校验依赖外网，离线/慢网下不可验证 |
| `go vet ./...` | ✅ 无输出（exit 0） | 唯一可用的官方静态检查 |
| `gofmt -l .` | ✅ 0 个文件 | |
| `goimports -l .` | ✅ 0 个文件 | |
| `gofumpt -l <all>` | ⚠️ 20 个文件 | 见 §2 |
| `staticcheck ./...` | ❌ 7 条 `internal error in importing ... export data version 5 is greater than maximum supported version 2` | 工具自身旧于本机 Go |
| `errcheck ./...` | ✅ 无输出 | |
| `misspell -locale US <go files>` | ✅ 无输出（代码与 README/AGENTS/docs 均无拼写问题） | |
| `gocyclo -over 15` | ✅ 5 个函数 >15（最高 25） | 见 §3 |
| `gocognit -over 15` | ✅ 8 个函数 >15（最高 35） | 见 §3 |
| `dupl -t 70` | ✅ 9 组克隆（7 组在 `_test.go`，2 组在非测试文件） | 见 §4 |
| `maintidx ./...` | ❌ `internal error: package "github.com/leanovate/gopter" without types was imported` | 无法验证 `.golangci.yml` 的 maintidx<15 |

### 1.3 发现 F-01［P1］：本机 lint 完全不可用，"宪法"从未被真实验证

**证据（原样）**

```text
$ golangci-lint run ./...
Error: can't load config: sort-results should be 'true' to use sort-order
Failed executing command with error: can't load config: sort-results should be 'true' to use sort-order

$ golangci-lint-v2 run ./...
panic: file requires newer Go version go1.27 (application built with go1.25) [recovered, repanicked]
    ...
    github.com/golangci/golangci-lint/v2/pkg/goanalysis.(*loadingPackage).loadFromSource(...)
[exit 2]

$ golangci-lint-v2 config verify
The command is terminated due to an error: [.golangci.yml] validate:
compile schema: failing loading "https://golangci-lint.run/jsonschema/golangci.v2.8.jsonschema.json":
Get "https://golangci-lint.run/jsonschema/golangci.v2.8.jsonschema.json": context deadline exceeded
[exit 3]
```

**影响**

1. AGENTS.md §1.1 把 `.golangci.yml` 定为"supreme law"，§5.1 要求提交前 `make lint-fix` 必须通过；但**本机根本加载不了配置**——"法"不可执行。
2. 没有可信的 lint 基线，团队无法区分"已修复"与"从未被检查"。最近一次提交标题为 *fix: resolve linting issues and improve code quality*，但在当前环境无法佐证该结论。
3. 结论置信度：F-01/F-02 是**实测事实**；F-03～F-06 是基于"配置语义 + 等效工具"的**推断性结论**，已在下文逐条注明推断链。

### 1.4 发现 F-02［P1］：lint 版本三处漂移，CI 不可复现

| 位置 | 内容 | 问题 |
|---|---|---|
| `Makefile:30` | `GOLANGCI_LINT_VERSION ?= latest` | `go install ...@latest`：任何人/任何时间安装到不同版本；`.golangci.yml` 已使用 v2 语法，`latest` 若解析到 v1 系将直接失败（本机即如此） |
| `Makefile:137-140` | `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)` | 路径固定 v2，但版本 `latest` 仍不可复现 |
| `.github/workflows/ci.yml:105-109` | `golangci/golangci-lint-action@v9` + `version: latest` | CI 随上游更新而漂移；v9 默认按 `latest` 拉取二进制，lint 结果随日期变化 |
| `.github/workflows/ci.yml:103` | `go-version: "1.25"` | 与 `go.mod:3` 的 `go 1.25.5` patch 版本不同步（setup-go 会解析到 1.25.x 最新 patch，通常 ≥5，但无显式约束） |
| 本地环境 | Go 1.27.3 + golangci-v2(built go1.25.5) | 二者组合 panic（见 F-01） |

**建议**：`Makefile` 固定 `GOLANGCI_LINT_VERSION ?= v2.8.0`（或当前发布版本），CI 显式 `version: v2.8.0`；本地通过 `GOTOOLCHAIN=go1.25.5` 或 go1.25 工具链运行 lint；在 CI 增加 `go version` 与 `golangci-lint version` 打印便于排障。（只建议，不改配置文件。）

### 1.5 等效替代检查覆盖矩阵

| 维度 | 首选工具（不可用） | 替代手段 | 置信度 |
|---|---|---|---|
| 格式化 | golangci goimports formatter | `gofmt -l`、`goimports -l`、`gofumpt -l` | 高 |
| 静态正确性 | staticcheck / govet | `go vet ./...`（即 govet） + `go build ./...` | 高（govet 部分） |
| 复杂度 | gocyclo/gocognit/maintidx（配置内） | 独立安装 gocyclo/gocognit；maintidx 失败→自写 AST 分析（行数/语句数/嵌套深度） | 高（gocyclo/gocognit）/中（funlen 行数按 AST 口径复算） |
| 重复 | dupl（配置内） | 独立 dupl `-t 70` + 人工比对 | 高 |
| 错误处理 | errcheck/errorlint/invalid-err | `errcheck ./...`（绿）+ 人工 grep `%w/%v`、`return err`、`strings.Contains(err` | 中高 |
| 拼写 | misspell | `misspell` 独立运行（绿） | 高 |
| 文档 | revive exported（未启用） | 自写 AST 注释覆盖统计 | 高 |
| 死代码/未使用 | unused（配置内） | 自写"导出符号跨包引用"统计（非完全等价） | 中 |
| 嵌套 | revive max-control-nesting（配置内） | 自写 AST 最大控制嵌套深度（注意与 revive 计数口径可能有 ±1 差异） | 中高 |
| 维护指数 | maintidx | ❌ 未能替代（gopter 无类型信息导致失败），只做定性判断 | 低 |

---

## 2. 维度一：格式与导入

### 2.1 实测结果

| 检查 | 命令 | 结果 |
|---|---|---|
| gofmt | `gofmt -l .`（排除 node_modules） | **0** 个文件 |
| goimports | `goimports -l .` | **0** 个文件 |
| gofumpt | `gofumpt -l <42 个源文件+测试>` | **20** 个文件（F-32） |
| 构建 | `go build ./...` | ✅ |
| vet | `go vet ./...` | ✅ |

### 2.2 导入分组与排序

抽查 `pkg/cli/handler.go:5-21`：

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/codegen"
	...
	"gopkg.in/yaml.v3"
)
```

结论：**标准库 / 第三方 / 内部包三段式分组正确，组内字典序正确，无 dot-import，无空导入**（`blank-imports` revive 规则启用，且无违规）。这是本仓库最干净的一维。

### 2.3 发现 F-32［P3］：gofumpt 未启用，20 个文件不符合 gofumpt 风格

实测 gofumpt 命中的 20 个文件（示例，非完整列表）：

```text
cmd/ob/main_test.go
internal/integration/integration_test.go
internal/testutil/testutil.go
pkg/credential/security_test.go
pkg/config/profile_test.go
pkg/config/config.go
pkg/config/install_test.go
pkg/config/export.go
pkg/config/spec_cache_test.go
pkg/config/install_integration_test.go
pkg/config/export_test.go
pkg/config/spec_cache.go
pkg/config/spec_watcher_test.go
pkg/config/install.go
pkg/spec/parser_test.go
pkg/completion/completion_test.go
pkg/cli/handler.go
pkg/codegen/codegen_test.go
pkg/request/builder_test.go
pkg/request/builder_integration_test.go
```

说明：gofmt/goimports 是"合法 Go"的下限；gofumpt 是更严格的超集（多余空行、import 分组细化、简单语句合并等）。`.golangci.yml:120-122` 只启用 goimports，因此这 20 个文件的漂移不会被 CI 捕获。**建议**（不改文件）：在 `formatters.enable` 中追加 `gofumpt`，先跑一次 `gofumpt -w .`（由负责人在独立提交中执行，不属本报告权限）。

### 2.4 导入别名一致性

| 文件:行 | 别名 | 风格 |
|---|---|---|
| `cmd/ob/main.go:16` | `tea "github.com/charmbracelet/bubbletea"` | 小写短名，符合惯例 |
| `cmd/ob/main.go:26` | `installTui "github.com/nomagicln/open-bridge/pkg/tui/install"` | camelCase |
| `pkg/tui/install/model.go:10` | `tea "github.com/charmbracelet/bubbletea"` | 一致 |
| `pkg/spec/format.go:11` | `ghodssyaml "github.com/ghodss/yaml"` | 全小写 |
| `pkg/router/router.go:13` | `mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"` | 全小写 |

**结论**：仓库仅 5 处显式别名，**无冲突别名、无重复别名**；风格上 `installTui`（驼峰）与 `ghodssyaml`/`mcpsdk`（全小写）不一致（F-26 的一部分，P3）。若考虑 Go 命名惯例，`installTui` 应写作 `installTUI` 或 `installtui`——但这属于"别名风格"，不影响正确性。

---

## 3. 维度二：复杂度热点

### 3.1 指标口径

- **gocyclo**：独立工具（与 `.golangci.yml:82-83` 的 `min-complexity: 25` 同一算法）。
- **gocognit**：独立工具（与 `.golangci.yml:84-85` 的 `min-complexity: 25` 同一算法，注意 gocognit 递归调用可能被低估，工具已提示）。
- **行数/语句数**：自写 AST 统计（`lines = End.Line - Pos.Line + 1`；statements 为常见语句节点计数）。
- **嵌套深度**：自写 AST 统计"if/for/range/switch/typeswitch/select 的最大嵌套层数"（与 revive `max-control-nesting` 口径接近，可能 ±1）。
- **是否被抑制**：人工核对函数上方/同行是否有 `//nolint`。

### 3.2 Top 15 复杂度热点（非测试代码）

| # | file:line | 函数 | 行数 | 语句 | gocyclo | gocognit | 嵌套 | 配置下状态 | 风险 |
|---|---|---|---|---|---|---|---|---|---|
| 1 | `pkg/cli/args.go:524` | `parseValueWithContext` | **93** | 62 | **25** | **26** | 3 | 被 `// nolint:gocognit,funlen` 抑制（:523） | 🔴 类型解析核心，最难测 |
| 2 | `pkg/cli/args.go:314` | `parseRequestParamsAuto` | **82** | 66 | 19 | **35** | 3 | 被 `// nolint:gocognit,gocyclo,funlen` 抑制（:313） | 🔴 参数自动分类主入口 |
| 3 | `pkg/mcp/test_helpers.go:59` | `createTestOpenAPISpec` | **89** | 9 | <10 | <10 | 1 | 被 `//nolint:funlen` 抑制（:58） | 🟡 巨型测试数据字面量 |
| 4 | `pkg/credential/security.go:373` | `looksLikeCredential` | 46 | 28 | **25** | **25** | 2 | 阈值恰为 25，不报（>25 才报） | 🟠 安全判定逻辑，边界不可测 |
| 5 | `pkg/cli/handler.go:844` | `(*Handler).showParameterValidationError` | 49 | 37 | ~21 | **25** | **4** | gocognit 不报（=25）；revive 嵌套 4 会被规则命中 | 🟠 嵌套 4 层 |
| 6 | `cmd/ob/main.go:102` | `Execute` | **58** | 22 | <10 | <10 | 1 | **funlen 未抑制（body=56>50）** | 🔴 入口分发 |
| 7 | `pkg/semantic/resource.go:295` | `Singularize` | **57** | 17 | 5 | <10 | 2 | **funlen 未抑制（55>50）** | 🟠 不规则复数表 |
| 8 | `pkg/mcp/progressive.go:143` | `buildSearchToolDefinition` | **55** | 16 | 5 | <10 | 1 | **funlen 未抑制（53>50）** | 🟠 巨型模板字符串 |
| 9 | `pkg/semantic/resource.go:354` | `Pluralize` | **54** | 17 | 13 | <10 | 2 | **funlen 未抑制（52>50）** | 🟠 同上 |
| 10 | `pkg/config/install.go:372` | `(*Manager).doInstall` | **54** | 36 | 9 | <10 | 2 | **funlen 未抑制（52>50）** | 🟠 安装主流程 |
| 11 | `pkg/cli/args.go:620` | `validateBodyParam` | **53** | 38 | ~15 | 14 | 2 | **funlen 未抑制（51>50）** | 🟠 参数校验 |
| 12 | `pkg/config/install.go:563` | `(*Manager).CreateShim` | **53** | 34 | 8 | <10 | 2 | **funlen 未抑制（51>50）** | 🟠 平台分支 |
| 13 | `cmd/ob/main.go:902` | `startMCPServer` | 50 | 38 | 7 | <10 | 1 | body=48，funlen 不报 | 🟡 |
| 14 | `pkg/tui/install/model.go:285` | `(Model).handleStepUpdate` | 42 | 22 | 19 | <10 | 2 | `//nolint:funlen // Step routing requires many cases`（:285） | 🟡 有理由，合规 |
| 15 | `pkg/tui/install/model.go:354` | `(Model).renderStepContent` | ~39 | — | 19 | <10 | 2 | 无抑制（未超阈值） | 🟡 |

**补充（gocognit 10–19 的"亚热点"，配置阈值 25 全部放过）**：

| file:line | 函数 | gocognit | 备注 |
|---|---|---|---|
| `pkg/completion/completion.go:218` | `collectBodyPropertyFlags` | 19 | **嵌套 5 层**（revive max-control-nesting=3 会命中） |
| `pkg/tui/install/model.go:593` | `(Model).updateBaseURL` | 18 | URL 校验 + TLS 状态 |
| `pkg/mcp/progressive.go:334` | `formatToolParameters` | 17 | |
| `pkg/cli/args.go:273` | `classifyParamInSpec` | 16 | 嵌套 4 |
| `pkg/config/install.go:851` | `(*Manager).UpdateApp` | 15 | |
| `pkg/cli/args.go:488` | `BuildNestedBody` | 15 | 嵌套 4 |
| `pkg/cli/errors.go:52` | `(*ErrorFormatter).FormatErrorWithContext` | 13 | 内部走字符串匹配（见 F-07） |
| `pkg/completion/completion.go:301` | `findParameterEnumValues` | 12 | 嵌套 4 |
| `pkg/mcp/registry.go:177` | `addParametersToSchema` | 13 | 嵌套 4 |
| `pkg/cli/args.go:879` | `setNestedValue` | 12 | 嵌套 3 |

### 3.3 阈值分析：25 是否过宽？

`.golangci.yml` 的阈值：`gocyclo 25`、`gocognit 25`、`maintidx <15`、`dupl 70`、`funlen 50/40`。逐项评估：

1. **gocyclo=25 过宽**（行业常用 10–15；golangci 默认 30 已是"宽松档"）。本仓库实际存在的 19（`parseRequestParamsAuto`）、19（`renderStepContent`/`handleStepUpdate`）函数全部合法通过——它们的可读性明显低于阈值设定所暗示的水平。
2. **gocognit=25 过宽**：认知复杂度的告警档常用 15。`collectBodyPropertyFlags`=19、`updateBaseURL`=18、`formatToolParameters`=17 均被放过；更矛盾的是**最严重的 35 被 `//nolint` 豁免**（F-03），即"阈值没拦住、nolint 又放行"。
3. **funlen 50/40 相对严格**，但当前有 6 个未抑制函数将触发（F-04）——说明配置的严格度与代码现状不匹配。
4. **maintidx under 15** 比默认（20）严格，但本机**无法运行**（工具报 gopter 无类型），实际是"设置了但从未验证"。
5. **阈值组合的结构性问题**：`gocognit` 与 `gocyclo` 都设为 25，但 `parseRequestParamsAuto` 的 gocyclo 只有 19、gocognit 35——说明该项目的主要问题是**深度嵌套的认知负担**而不是分支数量；只调 gocyclo 会漏掉，gocognit 又被 nolint 绕过。**建议**：把 gocognit 阈值降到 20、gocyclo 降到 20，并对存量用"基线文件 + 只减不增"策略（见 §13）。

### 3.4 发现 F-03［P1］：复杂度豁免依赖 `// nolint`，且 nolintlint 未启用

```go
// pkg/cli/args.go:311-314
// parseRequestParamsAuto handles auto-classification based on OpenAPI spec (Mode B)
// nolint:gocognit,gocyclo,funlen // Complex logic for parameter classification is necessary
func parseRequestParamsAuto(args []string, params *RequestParams, opSpec *openapi3.Operation) (*RequestParams, error) {

// pkg/cli/args.go:521-524
// parseValueWithContext is an internal helper for parsing values in context of a parameter's schema.
// Supports type-aware parsing for proper JSON serialization.
// If schema is nil, performs generic parsing.
// nolint:gocognit,funlen // Type-aware parsing requires handling multiple cases
func parseValueWithContext(value string, schema *openapi3.Schema) (any, error) {
```

问题：

1. 这两处是**全仓最复杂、最核心、最缺测试的参数解析函数**（`pkg/cli/args.go` 923 行，测试只有 `args_test.go` 的间接覆盖），却恰好被豁免——lint 通过 ≠ 代码可维护。
2. `.golangci.yml:20-54` **没有启用 `nolintlint`**：谁都可以加 `//nolint`，且不会有人检查"理由是否为空、指令是否过期、是否只是写了一个 linter 名"。AGENTS §1.1 要求"`//nolint` 必须带理由"，但该约束**没有任何自动化执行手段**。
3. nolint 的写法不统一（`// nolint:` vs `//nolint:`，见 F-28），且其中一处**无理由**（`pkg/tui/install/model.go:605`，见 §11）。

### 3.5 发现 F-04［P1］：funlen 与代码现状不自洽（6 处未抑制）

按 funlen 的口径（函数体行数 = `End.Line - Pos.Line - 1`；statements 计数）对非测试代码全量扫描：

| 函数 | body 行数 | statements | 是否有 nolint | 结论 |
|---|---|---|---|---|
| `pkg/cli/args.go:524 parseValueWithContext` | 91 | 62 | ✅ `// nolint:...funlen` | 抑制 |
| `pkg/mcp/test_helpers.go:59 createTestOpenAPISpec` | 87 | 9 | ✅ `//nolint:funlen` | 抑制 |
| `pkg/cli/args.go:314 parseRequestParamsAuto` | 80 | 66 | ✅ `// nolint:...funlen` | 抑制 |
| `cmd/ob/main.go:102 Execute` | 56 | 22 | ❌ | **会报错** |
| `pkg/semantic/resource.go:295 Singularize` | 55 | 17 | ❌ | **会报错** |
| `pkg/mcp/progressive.go:143 buildSearchToolDefinition` | 53 | 16 | ❌ | **会报错** |
| `pkg/semantic/resource.go:354 Pluralize` | 52 | 17 | ❌ | **会报错** |
| `pkg/config/install.go:372 doInstall` | 52 | 36 | ❌ | **会报错** |
| `pkg/cli/args.go:620 validateBodyParam` | 51 | 38 | ❌ | **会报错** |
| `pkg/config/install.go:563 CreateShim` | 51 | 34 | ❌ | **会报错** |

**推断链与置信度**：`.golangci.yml:90-92` `funlen: lines: 50, statements: 40`；funlen 的计数实现为函数体行数（v0.2.0 源码：对比 `lineLimit`）。因此**若 lint 在版本匹配的环境中真实运行，上述 6 个未抑制函数会被报出**。这有两种可能：(a) CI lint 实际是红的但未被注意；(b) 真实 golangci 版本的 funlen 计数口径与我复算的不同（最多差 1–2 行，`Execute` 56、`Singularize` 55 仍有较大超出）。无论哪种，都说明"配置严格度 vs 代码现状"缺乏基线管理。

### 3.6 发现 F-06［P1］：revive `max-control-nesting: 3` 会命中 ≥7 个函数

`.golangci.yml:52-55`：

```yaml
- name: max-control-nesting
  arguments:
    - 3
```

实测非测试代码中控制嵌套 ≥4 的函数（自写 AST）：

| 嵌套 | file:line | 函数 |
|---|---|---|
| 5 | `pkg/completion/completion.go:218` | `collectBodyPropertyFlags` |
| 4 | `pkg/cli/args.go:488` | `BuildNestedBody` |
| 4 | `pkg/completion/completion.go:301` | `findParameterEnumValues` |
| 4 | `pkg/config/test_helpers.go:67` | `containsAny` |
| 4 | `pkg/mcp/registry.go:177` | `addParametersToSchema` |
| 4 | `pkg/cli/args.go:273` | `classifyParamInSpec` |
| 4 | `pkg/cli/handler.go:844` | `(*Handler).showParameterValidationError` |

revive v1.13.0 中该规则默认值为 5，配置为 3 即"超过 3 层报告"。这 7 处均**没有** `//nolint`，因此同样属于"配置与代码不自洽"的证据。注：revive 对该规则的计数实现与我的 AST 口径可能有 ±1 差异（例如是否把 else-if 计入），置信度中高。

### 3.7 发现 F-25［P3］：复杂度治理缺少"热点清单/基线"

`.golangci.yml` 的 exclusions 只按"函数名 text"排除（`newInstallCmd`/`ExecuteCommand`/`CompleteFlagValues`/`InstallApp`），而不是按"文件+函数+基线"管理。这导致：

- 新增复杂度热点只能靠 `//nolint`（F-03）；
- 存量热点没有"只减不增"的基线；
- 排除项一旦函数改名/拆分即失效，无人清理（见 F-24）。

---

## 4. 维度三：重复代码

### 4.1 dupl 实测（阈值 70，与 `.golangci.yml:88-89` 一致）

| # | 组 | 文件与行 | 类型 | 是否有 nolint |
|---|---|---|---|---|
| D1 | 2 clones | `pkg/mcp/progressive_test.go:173,199` / `:187,213` | 测试 | 文件头 `//nolint:dupl`（:1） |
| D2 | 3 clones | `pkg/mcp/progressive_test.go:173,185` / `:187,199` / `:201,213` | 测试 | 同上 |
| D3 | 2 clones | `pkg/codegen/codegen_test.go:221,233` / `:235,247` | 测试 | 函数级 `//nolint:dupl`（:221,:235） |
| D4 | 2 clones | `pkg/mcp/engine_predicate_test.go:44,60` / `:62,78` | 测试 | 文件头 `//nolint:dupl`（:1） |
| D5 | 4 clones | `pkg/mcp/engine_predicate_test.go:30,42` / `:133,146` / `:180,192` / `:194,206` | 测试 | 同上 |
| D6 | 2 clones | **`pkg/cli/handler.go:686,701`** / **`pkg/codegen/go.go:139,155`** | **生产** | ❌ 无 |
| D7 | 2 clones | **`pkg/config/test_helpers.go:53,64`** / **`pkg/semantic/test_helpers.go:12,23`** | **非测试文件（含 testing）** | ❌ 无 |
| D8 | 2 clones | `pkg/mcp/engine_predicate_test.go:80,96` / `:148,165` | 测试 | 文件头 nolint |
| D9 | 2 clones | `pkg/cli/args_test.go:199,223` / `:227,251` | 测试 | `// nolint:dupl`（:198,:226） |

仅对**非 `_test.go` 文件**跑 dupl，仍有两组（D6、D7）——即 `.golangci.yml` 的 `dupl` 若生效，这两处会报错。

### 4.2 发现 F-05［P1］：D7 是真实重复且无抑制

```go
// pkg/config/test_helpers.go:53
func runCredentialDetectorTests(t *testing.T, label string, cases []credentialTestCase, fn func(string) bool) {
	t.Helper()
	for _, tc := range cases { ... }
}

// pkg/semantic/test_helpers.go:12
func runStringMappingTests(t *testing.T, label string, tests []stringMappingCase, fn func(string) string) {
	t.Helper()
	for _, tc := range tests { ... }
}
```

两个文件都**不是** `_test.go`（因此逃过 `.golangci.yml:97-100` 的测试排除），内容又是"表驱动测试 runner"的近似复制。**这是可直接消掉的重复**：抽到 `internal/testutil`（已存在）用泛型 `RunTableTests[T any]` 即可。同时解决 F-13（非测试文件 import testing）。

### 4.3 D6 是 dupl 的"结构性假阳性"，但揭示了真实设计问题

`pkg/cli/handler.go:686-701`（`writeGlobalFlagsSection`，用 `sb.WriteString` 拼帮助文本）与 `pkg/codegen/go.go:139-155`（`writeExecuteRequest`，用 `buf.WriteString` 拼 **Go 源码**）在 token 序列上高度相似，但语义完全不同。dupl 会命中；**真正的问题是两处都用"手写 WriteString 序列"来生成长文本**：

- `pkg/mcp/progressive.go:143-260` 已经用 `text/template` 生成工具描述（正确做法）；
- `pkg/codegen/*.go` 四个生成器全部用手写字符串拼接（`writeHeaders` 已被迫打 `//nolint:dupl`，见 `nodejs.go:70`、`python.go:47`）；
- `pkg/cli/handler.go` 的帮助文本与 `cmd/ob/main.go` 中 Cobra 的 `Use/Short/Long` 描述**各写一份**，存在漂移风险（如 `--yaml` 的默认值说明分散两处）。

**建议**：codegen 用 `text/template` 重写（与 mcp 一致）；CLI 帮助文本以 Cobra 定义或单一 `help.go` 为唯一来源。

### 4.4 人工发现的近似块（dupl 阈值未覆盖但值得治理）

| # | 位置 | 形态 | 建议 |
|---|---|---|---|
| M1 | `pkg/codegen/nodejs.go:70`、`pkg/codegen/python.go:47`（均已有 `//nolint:dupl`）、`pkg/codegen/go.go`、`curl.go` | 4 个 generator 的 `writeHeaders`/`writeBody` 结构同构 | 模板化 + 公共 header 渲染函数 |
| M2 | `cmd/ob/main.go:602-666` `printAppConfig` / `printAppConfigJSON` / `printAppConfigYAML` / `printAppConfigText` | json/yaml 两兄弟几乎逐行相同 | 合并为 `printAppConfig(format)` + 统一 serializer 选路 |
| M3 | `cmd/ob/main.go:674,689,699,719,734` `printProfileDetails/Auth/TLS/Headers/Safety` | 5 个 `fmt.Printf` 段落函数，缩进/冒号风格重复 | 定义 `printSection(title, indent, [][2]string)` 或统一 formatter |
| M4 | `pkg/cli/errors.go` 的 `formatXxxError` 系列（`formatAppNotFoundError`/`formatNetworkError`/`formatMissingParameterError`…） | 一致的三段式"Error→原因→建议" | 抽 `formatErrorBlock(kind, msg, suggestions)` |
| M5 | `pkg/config/export.go:441-458` 三种格式（yaml/json/legacy）回溯解析 | `if err := X.Unmarshal(data,&v); err == nil && v.Version != ""` 重复 3 次 | 定义 `formatCandidates` 表 |
| M6 | `pkg/cli/test_helpers.go:16 runHTTPErrorCases` vs `pkg/config/test_helpers.go:53` vs `pkg/semantic/test_helpers.go:12` vs `pkg/request/test_helpers.go:59` | 4 套表驱动 runner | 泛型化 |
| M7 | `pkg/config/test_helpers.go:67 containsAny` vs 其他包的 contains 类工具 | 无共享字符串工具包 | 放 `internal/` 或 `slices`/`strings` 标准库 |

---

## 5. 维度四：命名

### 5.1 实测统计

| 检查项 | 结果 | 证据/命令 |
|---|---|---|
| 导出函数 `Get*` 前缀 | **42 个** | `credential/security.go:455,538`、`config/config.go:361,434,856,884`、`mcp/registry.go:273,315,325,334` 等 |
| Getter 风格 | 均为 `GetXxx()` 而非 Go 惯例的 `Xxx()` | `Go` 官方风格建议避免 `Get` 前缀 |
| receiver 命名一致性 | **0 处不一致**（同一类型只有一个 receiver 名） | 自写 AST 扫描（revive `receiver-naming` 亦启用） |
| 缩写大小写 | 全仓 `ID` 138 次、`URL` 279 次、`JSON` 107 次、`HTTP` 74 次、`API` 175 次 → **大写规范执行得好** | grep 计数 |
| 例外（内部标识符） | `baseUrlInput`（13 处，`pkg/tui/install/model.go`）、`toolId`（8 处）、`MyApi`（1 处，测试/示例） | grep |
| 例外（外部契约） | `operationId`/`petId` 等为 OpenAPI/JSON 的 `json:"..."` 标签与 map key，**不属命名违规** | `pkg/mcp/tool_naming.go`、`internal/testutil` 样例 |
| 包名 | `cli`/`codegen`/`config`/`credential`/`mcp`/`request`/`router`/`semantic`/`spec`/`tui/install` | 无 `internal/integration` 重名冲突 |
| 错误变量 | 无导出 `Err*`；唯一 sentinel `errAppConfigNotSet`（`pkg/mcp/shared.go:13`）符合小写惯例 | 见 §7 |
| stutter | `config.Manager`、`credential.Manager` 正常；**未发现** `config.ConfigManager` 类重复 | `go doc` 抽查 |
| `Test*` 命名污染生产代码 | `internal/proptest/proptest.go:11 func TestParameters()` 是**生产函数**而非测试 | 容易被 `grep 'func Test'`/IDE 误认为测试 |

### 5.2 发现 F-26［P3］：命名细节

1. **42 个导出 `Get*`**：Go 惯例是字段/概念名直接作为方法名（`Profile()` 而非 `GetProfile()`），`Get` 前缀通常来自 Java/C# 习惯。此仓库内部一致，但与 Go 生态惯例不一致；且其中大量 `Get*` 是"内部 getter"（见 F-16 过度导出），建议在收敛导出面时一并改为 `Xxx()`。
2. **`GetFd`（`pkg/credential/security.go:455`）**：缩写 `Fd` 大小写/语义均不清晰（应为 `FD`，且它是终端探测用的文件描述符辅助函数）。同文件还有 `GetAuthValue`、`GetPlatformInfo` 等。
3. **`internal/proptest/proptest.go:11 TestParameters`**：生产函数名以 `Test` 开头（虽然不会被 `go test` 当作测试，但会被 IDE/脚本过滤混淆）；其文档注释首行是 "Default: 100 iterations..."，**未以函数名开头**（go doc 规范）。
4. **`baseUrlInput`**（`pkg/tui/install/model.go`，13 处）：应为 `baseURLInput`，与全仓 `URL` 大写惯例冲突。
5. **import 别名风格**：`installTui`（`cmd/ob/main.go:26`）vs `ghodssyaml`/`mcpsdk`（全小写）。建议统一为全小写（`installtui`）或全大写缩写（`installTUI`）。

### 5.3 值得肯定的命名实践

- 复数/单数语义化函数：`Singularize`/`Pluralize`（`pkg/semantic/resource.go:295,354`）。
- 动词映射表命名：`MapVerb`/`IsStandardVerb`/`IsActionVerb`（`pkg/semantic/verb.go:175,469,494`）。
- 错误类型命名：`AppNotFoundError`/`ProfileNotFoundError`/`PathValidationError`/`PrintedError` —— 清晰可读。
- receiver 一致性 100%（多数项目做不到）。

---

## 6. 维度五：注释与文档

### 6.1 统计口径与结果

| 指标 | 数值 | 方法 |
|---|---|---|
| package comment 覆盖 | **15/15 包均有** | 自写 AST：每个 package 至少一个文件有 Package doc |
| package comment 重复 | `config` **7 份**、`semantic` **3 份**、`cli` **2 份**、`credential` **2 份**、`mcp` **2 份**、`spec` **2 份**；`cmd/ob`、`codegen`、`completion`、`request`、`router`、`proptest`、`testutil` 各 1 份 | 见 §6.2 表 |
| 导出 func/type 注释覆盖 | **446/470 = 94.9%** | 自写 AST（函数/类型声明前 3 行内有 `//`） |
| 缺失注释的导出符号 | **24 个**（清单见 §6.3） | |
| 注释行总数 | 3,017 行 | 全仓 `//` 行 |
| 中文注释 | **1 行**（`pkg/codegen/nodejs.go:12`） | 中英混杂 1 处 |
| TODO/FIXME/XXX/HACK | **0 处** | grep 无结果 |
| 过时注释 | 未发现明确"注释与代码矛盾"样例（人工抽查 30 个导出函数 + 5 个包 doc） | 置信度中 |

### 6.2 发现 F-20［P2］：package 注释整段复制 + 文件级注释污染

**证据表（每个包重复的 package doc 所在文件）**：

| 包 | package doc 文本（首行） | 重复文件数 | 文件 |
|---|---|---|---|
| `config` | `Package config provides configuration management for OpenBridge.` | **7** | `config.go:1`、`export.go:1`、`install.go:1`、`path_utils.go:1`、`profile.go:1`、`spec_cache.go:1`、`spec_watcher.go:1` |
| `semantic` | `Package semantic provides semantic mapping from OpenAPI operations to CLI commands.` | 3 | `mapper.go:1`、`resource.go:1`、`verb.go:1` |
| `cli` | `Package cli provides CLI command handling for OpenBridge.` | 2 | `errors.go:1`、`handler.go:1` |
| `credential` | `Package credential provides secure credential management using the system keyring.` | 2 | `keyring.go:1`、`security.go:1` |
| `mcp` | `Package mcp provides MCP (Model Context Protocol) server functionality.` | 2 | `engine.go:1`、`handler.go:1` |
| `spec` | `Package spec provides OpenAPI specification parsing and validation.` | 2 | `format.go:1`、`parser.go:1` |

问题：

1. **维护陷阱**：当包职责变化时，需要同步改 N 份副本；`go doc` 只展示其中一份，其余是"看不见的重复文档"。
2. **文件级说明混入 package doc**：`pkg/config/path_utils.go:1-2`：

```go
// Package config provides configuration management for OpenBridge.
// This file contains path validation utilities for TLS certificates and other files.
package config
```

`This file contains...` 是文件级说明，却与 package doc 连成同一个注释组——`go doc config` 会把它当成包文档的一部分。正确做法是 package doc 只在**一个**文件（如 `doc.go` 或 `config.go`），文件说明放在 package 子句之后或省略。

3. **`//nolint` 占据 package doc 位置**：`pkg/mcp/test_helpers.go:1-2`：

```go
//nolint:dupl // This file contains shared test helpers that define consistent test data.
package mcp
```

Go 的 `ast.CommentGroup.Text()` 会把 `//nolint:...` 当 directive 剥离，导致该文件贡献一个**空 package doc**；若某天它成为包内唯一文件，`go doc` 将显示空白文档。

### 6.3 发现 F-19［P2］：24 个导出符号缺注释

| file:line | 符号 | 备注 |
|---|---|---|
| `pkg/cli/args.go:465` | `ParseValue` | 核心导出的参数解析函数，**无注释** |
| `pkg/cli/errors.go:22` | `(*PrintedError).Error` | 接口方法 |
| `pkg/cli/errors.go:28` | `(*PrintedError).Unwrap` | 接口方法 |
| `pkg/config/config.go:810` | `(*AppNotFoundError).Error` | 接口方法 |
| `pkg/config/config.go:820` | `(*ProfileNotFoundError).Error` | 接口方法 |
| `pkg/config/export.go:410` | `ImportProfileWithOptions` | |
| `pkg/config/profile.go:71` | `CreateProfile` | |
| `pkg/config/profile.go:521` | `(*ProfileNotFoundError).Error`？（以实际为准） | 接口方法 |
| `pkg/config/path_utils.go:22` | `(*PathValidationError).Error` | 接口方法 |
| `pkg/config/path_utils.go:29` | `(*PathValidationError).Unwrap` | 接口方法 |
| `pkg/config/spec_watcher.go:32-35` | `SpecChangeModified/Deleted/Expired/Error` | 导出常量 |
| `pkg/config/spec_watcher.go:97-98` | `WatchModeInotify/Polling` | 导出常量 |
| `pkg/config/spec_watcher.go` | `SpecChange*` 类型方法 | |
| `pkg/credential/keyring.go:207` | `NewManager` | **构造函数无注释** |
| `pkg/credential/keyring.go:479` | `ListAllCredentials` | |
| `pkg/credential/keyring.go:561` | `(*CredentialNotFoundError).Error`（以实际为准） | 接口方法 |
| `pkg/credential/security.go:214` | `Validate` | |
| `pkg/credential/security.go:246` | `Error` | 接口方法 |
| `pkg/credential/security.go:256` | `Unwrap` | 接口方法 |
| `pkg/credential/security.go:322` | `RedactCredential` | 安全相关导出 |
| `pkg/semantic/verb.go:175` | `MapVerb` | |
| `pkg/tui/install/model.go:61` | `type Model` | 导出类型 |
| `pkg/tui/install/model.go:222,268,275,473` | `NewModel/Init/Update/View` | Bubble Tea 接口方法 |
| `pkg/tui/install/model.go:1345` | `Result` | 导出类型 |
| `pkg/tui/install/model.go:20-38` | 20 个 `Step*` 导出常量 | |

说明：`Error()/Unwrap()/Init/Update/View` 等接口方法无注释在 Go 中可接受（golint/revive 的 `exported` 规则通常也豁免实现接口的方法）。真正需要补的是 `ParseValue`、`NewManager`、`ListAllCredentials`、`RedactCredential`、`ImportProfileWithOptions`、`CreateProfile`、`MapVerb`、`NewModel`、`type Model/Result`、以及 20+ 个导出 `Step*` 常量。

**根因**：`.golangci.yml:49-76` 的 revive 规则清单里**没有 `exported`、也没有 `package-comments`**，所以"导出必须有注释"从未被机器约束。

### 6.4 注释质量的正例与反例

**正例（值得保留的实践）**：

- `pkg/tui/install/model.go:1088-1094`：先解释 `checkOverwriteOrFinish` 的语义，再解释"为什么 tea.Cmd 恒为 nil 但仍保留"，最后 `//nolint:unparam // tea.Cmd is kept for Update pattern consistency`——**这是 AGENTS §1.1 要求的"nolint + 理由"的正确样板**。
- `pkg/cli/errors.go:17-20`：`PrintedError` 解释"为什么 Error() 返回空串"（防止双重打印）——注释解释了代码的"反直觉"之处，价值高。
- `pkg/credential/security.go:364`、`:422`、`:431`：安全函数明确写 "This should NEVER be used with actual credentials."、"best-effort due to garbage collection"——安全语义注释到位。

**反例**：

- `pkg/codegen/nodejs.go:12`：全仓唯一中文注释 `// writeHeaderValues通用函数，将HTTP头部写入缓冲区`——中英混杂，且**缺少空格**（`writeHeaderValues通用`），与全仓英文注释风格不一致（F-28）。
- `pkg/tui/install/model.go:605`：`//nolint:staticcheck` **无理由**，且它紧贴的语句是 `m.err = fmt.Errorf("Base URL is required")`，看起来像**追加注释时行号漂移导致抑制了错误位置**（无法确证 staticcheck 实际要抑制什么）——违反 AGENTS §1.1 的 MUST（F-28）。

---

## 7. 维度六：错误处理风格

### 7.1 统计总览（非测试代码）

| 指标 | 数值 | 说明 |
|---|---|---|
| `fmt.Errorf` 总数 | **244** | |
| 使用 `%w` | **132**（**54%**） | AGENTS §4.1 要求"总是包裹" |
| 使用 `%v` 且带 `err` 参数（丢链） | **22** | 清单见 §7.3 |
| `return err` / `return nil, err` 裸返回 | **115** | 清单节选见 §7.2 |
| 导出 sentinel `Err*` | **0** | AGENTS §4.1 明确要求 |
| 非导出 sentinel | 1（`errAppConfigNotSet`，`pkg/mcp/shared.go:13`） | |
| 内联 `errors.New(...)` 直接 `return` | 3 | `pkg/config/spec_watcher.go:139,551,707` |
| `errors.Is/As` 使用 | 18（含测试） | 主要：`pkg/credential/keyring.go:387,406,427`、`pkg/cli/errors.go:35,63-67,126-136` |
| 自定义错误类型 | ≥6 种 | `PrintedError`、`AppNotFoundError`、`ProfileNotFoundError`、`PathValidationError`、`CredentialNotFoundError`、`SpecWatcherError`（以实际为准） |
| 错误字符串大写开头 | 8 处 | 见 §7.4 |
| 错误字符串句号结尾 | 0 处 | ✅ |
| 错误中包含敏感信息 | 未发现直接泄露；credential 包有 `RedactCredential` | ✅（但见 §7.5 的间接风险） |

### 7.2 发现 F-09［P1］：错误包装率仅 54%，115 处裸返回

**裸 `return err` 高频文件**（全部非测试，`grep -rnE 'return (nil, )?err$'` 共 115 处）：

| 文件 | 数量（估计） | 代表行 |
|---|---|---|
| `pkg/config/config.go` | 20+ | `:158,166,436,441,472,484,498,561,597,650,655,688,758,766` |
| `pkg/config/export.go` | 10+ | `:57,418,423,428,580,600,627,660` |
| `pkg/config/install.go` | 25+ | `:188,216,327,346,351,359,374,380…` |
| `pkg/config/spec_cache.go` | 20+ | `:319,346,365,381,399,464…` |
| `pkg/config/spec_watcher.go` | 8 | `:157,161,198,202,…` |
| `pkg/credential/keyring.go` | 8 | `:431,438,442,523,528,547` |
| `pkg/credential/security.go` | 4 | `:480,484,496` |
| `cmd/ob/main.go` | 2 | `:335,818` |

**问题**：AGENTS §4.1 的代码示例明确写 `❌ Bad: return err`，并要求"always wrap errors when passing them up the stack to preserve context"。115 处裸返回意味着错误从 lib 传到 CLI 时丢失操作上下文（哪个 app、哪个 profile、哪个文件）。**这不是"风格偏好"，而是排障能力的实际损失**。

**建议**：优先治理 `pkg/config`（错误冒泡最密集），使用 `fmt.Errorf("load app config %q: %w", appName, err)` 模式；对"已经是最终用户可见错误"的路径（如 `PrintedError`）可保留裸返回但需注释说明。

### 7.3 发现 F-09（续）：22 处 `%v` 丢弃错误链

```text
pkg/mcp/handler.go:105   errorResult("Failed to build request: %v", err)
pkg/mcp/handler.go:114   errorResult("Failed to execute API call: %v", err)
pkg/mcp/handler.go:120   errorResult("Failed to read response body: %v", err)
pkg/mcp/handler.go:158   errorResult("Error unmarshaling arguments: %v", err)
pkg/mcp/handler.go:163   errorResult("Error mapping tool: %v", err)
pkg/mcp/progressive.go:317/323/397/406/424/472/485/491  errorResultProg("...: %v", err)
pkg/config/export.go:278/341   fmt.Sprintf("...: %v", err)
pkg/config/test_helpers.go:34/42   t.Fatalf("...: %v", err)   （测试辅助，影响小）
pkg/request/test_helpers.go:66/70/88/92   （测试辅助，影响小）
internal/testutil/testutil.go:86   t.Fatalf("...: %v", err)   （测试辅助，影响小）
```

**关键点**：`errorResult`/`errorResultProg` 是 MCP 工具结果的构造器，它们把错误转换成 **JSON 文本返回给 AI agent**；用 `%v` 意味着 agent 只看到字符串，**无法结构化判断错误类型**（如"是认证失败还是网络失败"）。`errorlint` 已启用（`.golangci.yml:31`），它的 `errorf` 检查正是针对这类 `%v` → 若 lint 真跑过，这些会被报出（再次佐证 F-01 的"宪法未生效"）。

### 7.4 发现 F-08［P1］：全仓无 sentinel error（AGENTS §4.1 直接违反）

AGENTS §4.1 原文：

> **Sentinel Errors:** Define strict sentinel errors (e.g., `ErrConfigNotFound`) for state checks in the `var` block of the package.

实测：

- `grep -rnE 'Err[A-Z][A-Za-z0-9]*\s*=\s*(errors\.New|fmt\.Errorf)'` → **0 条**；
- 全部 `errors.New` 共 4 处：`pkg/config/spec_watcher.go:139`（`"watcher already running"`）、`:551`（同名）、`:707`（`"manager already running"`）、`pkg/mcp/shared.go:13`（`errAppConfigNotSet`，唯一 var 块 sentinel 但未导出）；
- 错误状态判断改用**自定义类型 + errors.As**（`pkg/credential/keyring.go:387,406,427`、`pkg/config/config.go` 的 `AppNotFoundError`）——类型化本身是对的，但**导出 sentinel 缺失**意味着：
  1. 跨包调用方无法 `errors.Is` 判断特定状态（例如"spec 已过期"）；
  2. 包内 `errors.New` 内联返回的 3 处"状态错误"无法被区分（只能靠字符串或 `err != nil`）；
  3. `spec_watcher.go:139` 与 `:551` 的 `"watcher already running"` **重复创建相同错误**，应提升为 `ErrWatcherAlreadyRunning`。

**建议**（不修改代码）：在各包新增 `var ( ErrXxx = errors.New("...") )` 块（AGENTS 要求的 var 块），至少覆盖：
`pkg/config`: `ErrAppNotFound`、`ErrProfileNotFound`、`ErrAppExists`、`ErrSpecStale`、`ErrWatcherRunning`;
`pkg/spec`: `ErrInvalidSpec`、`ErrUnsupportedVersion`;
`pkg/credential`: `ErrCredentialNotFound`、`ErrKeyringUnavailable`;
`pkg/mcp`: `ErrAppConfigNotSet`（导出现有 `errAppConfigNotSet`）。

### 7.5 发现 F-07［P1］：错误分类依赖字符串匹配（stringly-typed errors）

`pkg/cli/errors.go:52-88`（`FormatErrorWithContext`）：

```go
default:
	errMsg := err.Error()
	if strings.Contains(errMsg, "unknown resource") { ... }
	if strings.Contains(errMsg, "unknown verb") { ... }
	if strings.Contains(errMsg, "required parameter") && strings.Contains(errMsg, "missing") { ... }
	if strings.Contains(errMsg, "parameter validation failed") { ... }
	if strings.Contains(errMsg, "failed to load spec") { ... }
	return fmt.Sprintf("Error: %s", errMsg)
```

同类代码：

| file:line | 代码 | 风险 |
|---|---|---|
| `pkg/cli/errors.go:71` | `strings.Contains(errMsg, "unknown resource")` | 改一句错误文案 → 用户体验路径静默失效 |
| `pkg/cli/errors.go:74` | `"unknown verb"` | 同上 |
| `pkg/cli/errors.go:77` | `"required parameter"` + `"missing"` | 同上 |
| `pkg/cli/errors.go:80` | `"parameter validation failed"` | 同上 |
| `pkg/cli/errors.go:83` | `"failed to load spec"` | 同上 |
| `pkg/spec/parser.go:461,479` | `strings.Contains(err.Error(), `unsupported 'type' value "null"`)` | 依赖 kin-openapi 内部错误文案（依赖升级即失效） |
| `pkg/credential/security.go:299` | `strings.Contains(errStr, "not found") \|\| strings.Contains(errStr, "does not exist")` | 依赖各 keyring backend 错误文案，跨平台最脆弱 |

**这是全仓最严重的"可维护性定时炸弹"**：它把"错误语义"编码进了**人类可读字符串**。AGENTS §4.1 的 sentinel/包装要求正是为了避免这个。修复路径：F-08（定义 sentinel）→ 用 `errors.Is` 替换 `strings.Contains` → 删除 `FormatErrorWithContext` 的 default 字符串分支。

### 7.6 发现 F-27［P3］：错误字符串风格不统一

revive `error-strings`（`.golangci.yml:60-61` 已启用）要求错误串不以大写开头、不以标点结尾。实测大写开头 8 处（0 处标点结尾）：

```text
pkg/credential/security.go:76   "API key cannot be empty"
pkg/credential/security.go:190  "API key cannot be empty"
pkg/config/spec_cache.go:497    "HTTP %d: %s"
pkg/config/path_utils.go:228    "CA certificate: %w"
pkg/tui/install/model.go:606    "Base URL is required"
pkg/spec/parser.go:464,482      "OpenAPI 3.x validation failed: %w"
pkg/mcp/handler.go:177          "OpenAPI spec not loaded"
```

说明：其中多数是**面向最终用户的 UI 文案**而非内部错误（`Base URL is required` 会被直接展示），这类"展示型错误"与"内部错误"应分层：内部错误小写 + `%w`，展示层再做首字母大写。当前混在一起，导致 reopen 判定为违规。建议引入 `cli.UserFacingError` 包装或统一用 `errors.New` 的小写内部错误 + 展示层格式化。

---

## 8. 维度七：魔法值与 stringly-typed

### 8.1 数字字面量（`mnd` 被禁止，见 `.golangci.yml:41-42`）

`.golangci.yml` 对 `mnd` 的禁用理由是：

```yaml
  disable:
    # numeric literals in CLI flags and protocol hints
    - mnd
```

这个理由对**CLI flag 默认值**成立，但仓库同时存在一批"本应是常量"的数字：

| 类别 | 字面量 | 出现次数 | 证据（file:line） | 应有形态 |
|---|---|---|---|---|
| 文件权限（文件） | `0644` | 6 | `pkg/config/config.go:455`、`pkg/config/spec_cache.go:557,710`、`pkg/config/install.go`（写 shim/config） | `const filePerm = 0o644` |
| 文件权限（目录/可执行） | `0755` | 5 | `pkg/config/install.go:573,610`、`pkg/config/spec_cache.go:551,696` | `const dirPerm = 0o755` |
| 私有权限 | `0600` | 1 | `pkg/cli/handler.go:184`（生成代码输出文件） | 同上 |
| 默认 HTTP 超时 | `30 * time.Second` | **6** | `pkg/config/config.go:843`、`pkg/config/profile.go:87`、`pkg/config/install.go:286`、`pkg/config/spec_cache.go:114,129`、`pkg/spec/parser.go:177` | `const DefaultHTTPTimeout = 30 * time.Second` |
| 轮询/重试间隔 | `50 * time.Millisecond` | 1 | `pkg/config/spec_watcher.go:323`（`time.Sleep` 等待文件落盘） | 常量 + 注释说明理由 |
| 刷新/防抖间隔 | `200ms`/`500ms` | 4+2 | `pkg/tui/install/model.go:142,171,179,204,880` | `const spinnerInterval = ...` |
| 显示宽度 | `50` / `40` / `37` | 3 | `cmd/ob/main.go:487-488,637`（`strings.Repeat("=", 50)`、表格列宽） | `const tableWidth = 50` |
| 通用端口/占位 | `11434`（Ollama 默认端口）、`8080`（测试服务器）、`12345`（示例） | 各 1 | `pkg/config/config.go:272`、`pkg/config/install.go:215`、`pkg/cli/args.go:878` | 端口常量或注释 |
| 缓存 TTL 默认值 | 5m / 24h | — | `pkg/spec/parser.go:175`（5m）、`pkg/config/spec_cache.go:23`（已定义为 `DefaultCacheTTL` ✅） | 前者应同后者 |

**值得肯定的反例**：`pkg/config/spec_cache.go:23` 已有 `const DefaultCacheTTL = 24 * time.Hour`——说明团队知道该怎么做，只是执行不一致。

**结论（F-14［P2］）**：`mnd` 全量禁用过度；应改为"启用 + ignore 明显无歧义的数字（0/1/2/3、HTTP 状态码等）"，或者至少把权限位/超时/显示宽度提升为常量。当前状态是"配置承诺了可读性（注释说 numeric literals 是 CLI 场景），但 config 核心包被连带豁免"。

### 8.2 stringly-typed API 与枚举缺失（F-15［P2］）

| 语义 | 散落的字符串 | 站点数 | 证据 |
|---|---|---|---|
| 认证类型 | `"bearer"` / `"basic"` / `"api_key"` | 12+ | `pkg/spec/parser.go:45`（列表）、`:375-379`（switch 探测）；`pkg/tui/install/model.go:229`（选项列表）、`:876,885,898,921,923,926`（switch 分支）；`pkg/credential/security.go:48,229,563`（`unsupported auth type: %s`） |
| 输出格式 | `"json"` / `"yaml"` / `"text"` | 8+ | `cmd/ob/main.go:602-612`（`printAppConfig` switch）；`pkg/spec/format.go:25,27`；`pkg/cli/handler.go` 帮助文本写入 `"json, yaml"` |
| URL scheme | `"http"` / `"https"` | 4 | `pkg/spec/parser.go:825`；`pkg/tui/install/model.go:611` |
| 参数位置 | `"path"` / `"query"` / `"header"` / `"body"` | 10+ | `pkg/cli/args.go:301-305`（switch）；`pkg/completion/completion.go`；`pkg/request/builder.go` |
| HTTP 方法 | `"GET"` / `"POST"` / … | 8 | `pkg/semantic/verb.go:68-76`（map 字面量） |
| 代码生成格式 | `"curl"` / `"nodejs"` / `"go"` / `"python"` | — | **已有常量** `pkg/codegen/codegen.go:17-20`（`FormatCurl` 等，但**无注释**，见 F-19） |

**问题本质**：

1. `authType` 在 **3 个包里各自 switch 同一组魔法字符串**：`spec/parser.go` 探测 → `tui/install/model.go` 展示/回填 → `credential/security.go` 构造凭据。任何一方新增类型（如 `"oauth2"`、`"mtls"`）都会漏改，且编译器无法发现。
2. 参数位置字符串 `"path"/"query"/"header"` 既作为 map key、又作为 switch 分支、还可能写入配置 YAML，属于"协议字面量"，应当有 `type ParamLocation string` + 常量；`goconst`/`usestdlibvars` 可自动发现重复。
3. HTTP 方法用字符串 map 可理解（go-openapi 上游 API 即字符串），但可用 `net/http` 的 `http.MethodGet` 等常量（`usestdlibvars` 的标准能力）。

**建议**（不改代码，仅方案）：

```go
// pkg/credential/auth_type.go 或 config 包
type AuthType string
const (
    AuthBearer AuthType = "bearer"
    AuthBasic  AuthType = "basic"
    AuthAPIKey AuthType = "api_key"
)
```

配合 `String()`/`UnmarshalYAML`/`IsValid()`，把 3 处 switch 改为同一个 `AuthType` 的分发，可消除 F-07 中一部分字符串分类（认证失败提示路径）。

---

## 9. 维度八：死代码与过度导出

### 9.1 测量方法

- 全仓导出函数/类型声明 **477** 个（自写 AST）。
- 对每个导出函数统计"**声明包之外**的引用次数"（grep 标识符 + `(`，含测试文件）。若为 0，则判为"仅本包使用"。
- 局限：无法判断是否有**仓库外**消费者（该模块是可执行的 CLI 应用，`pkg/` 未承诺稳定 API；从 README 与文档看没有库用法示例）。因此"过度导出"结论的置信度为中高。

### 9.2 结果：195 个导出函数仅本包使用（F-16［P2］）

按包统计（节选，完整名单可由本报告附录方法复现）：

| 包 | 过度导出数（约） | 典型符号（file:line） |
|---|---|---|
| `pkg/config` | **~60** | `GetConfigDir`（config.go:361）、`AppsDir`（:412）、`ConfigDir`（:407）、`Save`（profile.go:45）、`LoadMeta`（spec_cache.go:572）、`SaveMeta`（:587）、`SaveParsedSpec`（:694）、`ValidateParsedSpec`（:784）、`Refresh`（:624）、`ClearAll`（:603）、`WithPollInterval`（spec_watcher.go:117）等 |
| `pkg/credential` | **~25** | `GetFd`（security.go:455）、`SecureZeroMemory`（:422）、`SecureZeroString`（:431）、`MaskInput`（:501）、`PromptBearerToken`（:53）、`PromptBasicAuth`（:83）、`GetAuthValue`（keyring.go:85）、`IsExpired`（:68）、`NeedsRefresh`（:76）等 |
| `pkg/mcp` | ~15 | `GetTools`（handler.go:67）、`GetRegistry`（progressive.go:516）、`GetSearchEngine`（:521）、`GetMetadata`（registry.go:273）、`GetLoadedTools`（:315）、`GetOperationInfo`（:325）、`MapToolToOperation`（handler.go:175）、`NormalizeOperationID`（tool_naming.go:261） |
| `pkg/spec` | ~12 | `GetHTTPClient`（parser.go:215）、`GetFetchOptions`（:220）、`Merge`（:83）、`GetCacheStats`（:699）、`ToJSONWithFallback`（format.go:260）、`UnmarshalWithFallback`（format.go:244） |
| `pkg/semantic` | ~10 | `AllMappings`（verb.go:421）、`VerbDescription`（:499）、`NormalizeResourceName`（resource.go:279）、`InferResourceFromOperationID`（:478） |
| `pkg/cli` | ~10 | `ParseValue`（args.go:465）、`SplitArgs`（:37）、`BuildNestedBody`（:488）、`FormatError`（errors.go:47）、`FormatUsageHelp`（:316） |
| `pkg/completion` | ~5 | `CompleteFlagValues`（completion.go:339）、`CompleteResourcesForVerb`（:105） |
| `pkg/codegen` | ~3 | `ListFormats`（codegen.go:85） |
| `internal/*` | ~9 | `internal/proptest` 的 `AnyString`/`MapOf`/`SliceOf`、`internal/testutil` 的 `On`/`OnJSON`/`URL` 等（internal 包内"过度导出"影响小，可忽略） |

**为什么这是问题**：

1. **API 面越大，维护成本越高**：195 个"事实内部"函数的签名/行为一旦变更，IDE 重构工具会误以为有跨包契约；新成员也难以分辨"哪些是公共 API"。
2. **测试与文档负担**：导出即有"对外承诺"的隐含语义，但 24 个导出符号连注释都没有（F-19）。
3. **`internal/` 使用不足**：该模块是 CLI 应用，`pkg/cli`、`pkg/router`、`pkg/tui/install`、`pkg/codegen` 在仓库外无消费者，却放在 `pkg/` 而非 `internal/`。真正需要"公开"的只有 `cmd/ob` 的接线与（若有）库用法。

**建议**（按风险从低到高）：

- S：把明显内部实现（`pkg/cli`、`pkg/router`、`pkg/completion`、`pkg/tui/install`）迁到 `internal/`（Go 编译器强制不可外部导入）。
- M：`pkg/config`/`pkg/credential`/`pkg/mcp` 仅导出**真正跨包**的入口（`NewManager`/`InstallApp`/`GetAppConfig` 等），其余改小写；用 `go doc` 生成 API 清单做人工审计。
- L：若项目确实想作为库发布，补齐所有导出符号的文档 + 兼容性测试；否则保持 internal 化。

### 9.3 未使用符号（unused）未能完全验证

`.golangci.yml:37` 启用了 `unused`，但 lint 不可运行。自写分析只发现 1 个"疑似仅接口实现使用"的导出方法（`tui/install.Model.Init`，被 Bubble Tea 接口调用，**不是死代码**）。其余无法可靠判定（如仅测试使用的导出函数、仅 `go doc` 使用的方法对 unused 的豁免逻辑）。**结论：死代码维度本地只能给出"未发现明显死代码"，不能给出"无死代码"的保证**（见附录 B）。

### 9.4 值得肯定

- `internal/proptest`、`internal/testutil`、`internal/integration` 的划分思路正确（虽然 `internal/integration` 只有测试，见 F-31）。
- 生产代码 0 个 `TODO/FIXME/XXX`，无注释掉的死代码块（人工抽查 20 个文件未发现 `/* ... */` 大段注释代码）。

---

## 10. 维度九：go.mod 依赖卫生

### 10.1 依赖全貌

`go.mod`：直接依赖 **14** 个（require 块 1），间接依赖 **47** 个；`go.sum` 72 KB。

| 直接依赖 | 版本 | 仓库内使用点 | 评估 |
|---|---|---|---|
| `github.com/99designs/keyring` | v1.2.2 | `pkg/credential/keyring.go:14` | 核心功能，必要；但引入 8+ 平台相关间接依赖（go-keychain/dbus/jose2go/wincred/percent/godbus…），属 keyring 生态成本 |
| `github.com/charmbracelet/bubbletea` | v1.3.10 | `pkg/tui/install/model.go:10`、`cmd/ob/main.go:16` | TUI 核心，必要 |
| `github.com/charmbracelet/bubbles` | v0.21.0 | 需确认（`grep -rn bubbles` 未在报告所列文件中见到，可能仅 textinput） | ⚠️ 若仅极少数组件使用，值得审计（bubbles 与 bubbletea/lipgloss 版本互相约束） |
| `github.com/charmbracelet/lipgloss` | v1.1.0 | `pkg/tui/install/model.go` | TUI 样式，必要 |
| `github.com/fsnotify/fsnotify` | v1.9.0 | `pkg/config/spec_watcher.go` | spec 热更新，必要 |
| `github.com/getkin/kin-openapi` | v0.133.0 | `pkg/spec/parser.go`、全仓 openapi3 引用 | 核心，必要（体积大，含 jsonpointer/easyjson/marshmallow 等） |
| **`github.com/ghodss/yaml`** | **v1.0.0** | **仅 `pkg/spec/format.go:11`（别名 `ghodssyaml`）** | 🔴 **已归档弃用**，见 10.2 |
| `github.com/leanovate/gopter` | v0.2.11 | `internal/proptest` | 仅测试用；作为 direct 依赖出现在生产依赖清单，结构上可接受（Go 无 dev-dependencies） |
| `github.com/modelcontextprotocol/go-sdk` | v1.2.0 | `pkg/mcp` | MCP 核心，必要 |
| `github.com/spf13/cobra` | v1.10.2 | `cmd/ob/main.go` | CLI 核心，必要 |
| `github.com/stretchr/testify` | v1.11.1 | 测试 | 正常 |
| **`github.com/vulcand/predicate`** | **v1.3.0** | **仅 `pkg/mcp/engine_predicate.go:8`** | 🟡 单文件使用；引入 `gravitational/trace` 等间接依赖；见 10.3 |
| `golang.org/x/term` | v0.39.0 | `pkg/credential/security.go`（终端探测） | 必要 |
| `gopkg.in/yaml.v3` | v3.0.1 | `pkg/config/config.go`、`pkg/cli/args.go:866`、`cmd/ob/main.go:626` 等 6 处 | 必要 |

### 10.2 发现 F-18［P2］：ghodss/yaml 已归档 + yaml.v2/v3 双栈

**事实**：

1. `github.com/ghodss/yaml` v1.0.0 自 2019 年 2 月起**归档**（README 明确 "DEPRECATED: use sigs.k8s.io/yaml instead"），仓库内仅 1 个使用点：

```go
// pkg/spec/format.go:10-11
import (
	...
	ghodssyaml "github.com/ghodss/yaml"
)
```

2. 其作用是 YAML→JSON 转换（因为 `kin-openapi` 与 `encoding/json` 需要 JSON 结构）。
3. `ghodss/yaml` 依赖 `gopkg.in/yaml.v2 v2.4.0`（`go.mod` 间接依赖块末尾），而仓库同时直接使用 `gopkg.in/yaml.v3 v3.0.1`——**同一个二进制里链接了两代 YAML 解析器**：安全补丁需打两份、行为差异（map 键顺序、数字类型、锚点/合并键）可能导致 spec 解析在两条路径上不一致。
4. 依赖树中已出现 `github.com/oasdiff/yaml`、`github.com/oasdiff/yaml3`（kin-openapi 提供 YAML 支持的间接依赖），说明**上游已自带 YAML→JSON 能力**，可先验证是否可直接使用。

**建议**（三选一，按成本排序）：

- S：保留 `ghodss/yaml` 但在 `pkg/spec/format.go` 处加 `//nolint:staticcheck // ...` + 注释说明迁移计划（并确认 staticcheck 是否报 SA1019）。
- M：替换为 `sigs.k8s.io/yaml`（ghodss 的官方继任者，API 兼容，需新增 1 个直接依赖）。
- M/L：使用 `yaml.v3` 解码到 `any` 后做递归 key 归一化，完全删除 ghodss + yaml.v2。**优先验证 kin-openapi 的 `openapi3.Loader` 是否已能直接加载 YAML**，若可以则此文件逻辑可整体删除。

### 10.3 发现 F-18（续）：vulcand/predicate 的"单点重依赖"

```go
// pkg/mcp/engine_predicate.go:8
import (
	"github.com/vulcand/predicate"
)
```

- 仅用于 MCP 搜索过滤表达式（`engine_predicate.go` 307 行）。
- 该依赖把 `github.com/gravitational/trace`（错误处理框架）拖入依赖树（`go.mod` 间接依赖中可见），对一个 CLI 来说属于"为一个小功能引入一整个框架族"。
- 它本身并不是坏库（谓词解析与求值质量不错），但**应被隔离**：建议放到 `internal/` 内部包，或在 README/架构文档说明"搜索 DSL 是唯一用途，替换成本 XX"。
- 若未来要裁剪依赖，可评估用 `expr`/`govaluate` 或自写 80 行解析器（搜索 DSL 仅支持少量操作符时）。

### 10.4 发现 F-18（续）：`go 1.25.5` 的 patch 版本写法

`go.mod:3`：

```go
go 1.25.5
```

- **语法合法**（Go 1.21 起 `go` 指令允许 patch 版本，用于 `go` 命令强制 toolchain 下限）。
- 但带来三个可维护性问题：
  1. **与 CI 声明不一致**：`.github/workflows/ci.yml:103` 是 `go-version: "1.25"`（setup-go 会解析到 1.25.x 最新 patch）。若某天 1.25.5 尚未发布/被 revoke，CI 仍可能通过而本地 go.mod 已要求更高版本。
  2. **与本地工具链不一致**：本机 go1.27.3，模块 go 1.25.5；`staticcheck`/`golangci-lint` 的"内置 go/types 版本 vs 本机 Go 版本"错配正是 F-01 的根因之一。
  3. **对使用者不友好**：任何使用 Go 1.25.0–1.25.4 的开发者首次 `go build` 会触发 toolchain 自动下载（需要网络与校验，本报告中 `GOSUMDB=off` 环境下已实际失败，见附录 A.4）。
- **建议**：改为 `go 1.25`（major.minor，最通用）或 `go 1.25.5` + `toolchain go1.25.5` 并在 CI/Makefile 用 `go-version-file: go.mod` 统一（`setup-go` 支持 `go-version-file: go.mod`）。

### 10.5 依赖维护流程缺口（P3，合并入 F-18）

- CI 无 `go mod tidy` 校验步骤（无法保证 go.mod/go.sum 与代码一致；本报告未运行 `go mod tidy -diff` 以避改文件）。
- 无依赖版本升级策略（未在本报告范围内审计 `.github/dependabot.yml` 是否存在；若不存在建议添加）。

---

## 11. 维度十：AGENTS.md 符合度

AGENTS.md 是本仓库的"行为宪法"。下表逐条核对**可静态检查**的条款（TDD 流程等过程性要求标注"不可验证"）。

| AGENTS 条款 | 要求 | 结论 | 证据 |
|---|---|---|---|
| §1.1 Lint 至高无上、不得为消错而改配置 | 不得改 `.golangci.yml` | ⚠️ 无证据显示被篡改，但 `//nolint` 被用作放行手段（F-03） | `pkg/cli/args.go:313,523` |
| §1.1 `//nolint` 必须带理由 | MUST include comment | ❌ 1 处违反；另 `// nolint`（带空格）与 `//nolint` 混用；`nolintlint` 未启用 | `pkg/tui/install/model.go:605`（无理由）；`pkg/cli/args.go:313,523`、`pkg/cli/args_test.go:198,226`（带空格） |
| §2 TDD 三步协议 | 先接口/测试后实现 | ⚠️ **不可验证**：无 CI 门禁或提交历史可证明；但测试分布显示核心交互（TUI/CLI handler）零测试，与协议精神不符 | F-11 |
| §3.1 严格同名配对 `foo.go ↔ foo_test.go` | 每个源文件有同名测试 | ❌ **9 个源文件无同名测试**（见 §11.1） | `find` 脚本 |
| §3.1 不得有孤儿测试 | 每个测试对应源文件 | ❌ `internal/integration/integration_test.go`、`internal/integration/real_api_test.go` 无对应 `integration.go`/`real_api.go` | `internal/integration/` 目录下只有 `_test.go` |
| §3.1 测试类型：Unit/Integration/Property/Helpers 命名 | 命名与用途一致 | ⚠️ 大体符合：`*_property_test.go` 3 个、`*_integration_test.go` 4 个、`test_helpers.go` 5 个；但 `internal/integration/real_api_test.go` 未按 `*_integration_test.go` 命名 | F-30 |
| §3.2 Parser/Formatter/序列化 MUST 用 property test | 使用 gopter | ❌ `pkg/spec/parser.go`（839 行 parser）、`pkg/spec/format.go`（287 行 format）、`pkg/codegen`（768 行生成器）**无 property test**；已有 6 个 `TestProperty*` 仅覆盖 config/export/credential | F-22 |
| §3.2 property test 命名 `TestProperty*` | 全部以 TestProperty 开头 | ✅ 6/6 合规（`pkg/credential/keyring_property_test.go:27,108,159`、`pkg/config/export_property_test.go:15,88`、`pkg/config/config_property_test.go:15`） | grep |
| §3.3 integration 测试必须带 `//go:build integration` | 文件第一行 build tag | ❌ **3/4 违反**：`pkg/config/install_integration_test.go:1`、`pkg/cli/errors_integration_test.go:1`、`pkg/request/builder_integration_test.go:1` 首行即 `package xxx`；仅 `pkg/credential/keyring_integration_test.go:1` 正确 | 见 §11.2 |
| §4.1 总是用 `%w` 包装错误 | 向上传递时包裹 | ❌ 54% `%w`；22 处 `%v` 丢链；115 处裸 `return err` | F-09 |
| §4.1 sentinel error 定义在 var 块 | `ErrXxx` 严格哨兵 | ❌ **0 个导出 sentinel**；仅 1 个未导出 + 3 处内联 `errors.New` | F-08 |
| §4.2 依赖定义为接口、优先生成 mock | 接口化 | ❌ CLI Handler 依赖具体类型（`pkg/cli/handler.go:31-37`：`*spec.Parser`、`*semantic.Mapper`、`*request.Builder`、`*config.Manager`），构造函数 `NewHandler` 同；仓库测试全部直连真实对象，未发现 mockery/mock 生成配置 | F-21 |
| §5.1 提交前 `make fmt-fix && make lint-fix` 必须通过 | 工具可执行 | ❌ 本机不可执行（F-01）；`gofumpt` 漂移 20 文件 | F-01/F-32 |
| §5.2 Conventional Commits + DCO | `type(scope): desc` + `-s` | ✅ 最近提交符合（`fix: ...`、`refactor: ...`）；CI 有 DCO job（`.github/workflows/ci.yml:19-51`） | `git log` |

### 11.1 发现 F-10/F-11（测试组织）：源文件 ↔ 测试对照全表

**无同名测试的源文件（9 个）**：

| 源文件 | 行数 | 风险 |
|---|---|---|
| `pkg/tui/install/model.go` | **1347** | 🔴 交互主流程，零测试 |
| `pkg/cli/handler.go` | **892** | 🔴 命令执行/请求发送核心，仅被 args/errors 测试间接覆盖 |
| `pkg/mcp/server_factory.go` | — | 工厂逻辑无测试 |
| `pkg/mcp/shared.go` | — | `errAppConfigNotSet`/`newProfileNotFoundError` 无直接测试 |
| `pkg/router/router.go` | 250 | 🔴 路由入口无测试 |
| `pkg/codegen/curl.go` | — | 由 `codegen_test.go` 间接覆盖（可接受） |
| `pkg/codegen/go.go` | — | 同上 |
| `pkg/codegen/nodejs.go` | — | 同上 |
| `pkg/codegen/python.go` | — | 同上 |

**孤儿测试（2 个）**：`internal/integration/integration_test.go`、`internal/integration/real_api_test.go`——`internal/integration` 目录下**没有任何非测试 `.go` 文件**（F-31），因此它们"无源可对"，只能算端到端测试集合，应命名为 `*_integration_test.go` 或移动到 `pkg/*/integration_test` 场景。

**测试投入分布不均**（源码/测试 LOC 比）：

| 包 | 源码 LOC | 测试 LOC | 测试/源码 |
|---|---|---|---|
| `pkg/spec` | 1,126 | 2,266 | **2.01×**（覆盖好） |
| `pkg/request` | 783 | 1,725 | **2.20×**（覆盖好） |
| `pkg/config` | 5,059 | 5,389 | 1.07× |
| `pkg/credential` | 1,217 | 1,995 | 1.64× |
| `pkg/mcp` | 2,200 | 1,729 | 0.79× |
| `pkg/cli` | 2,688 | 1,352 | **0.50×**（最重代码、较少测试） |
| `pkg/tui/install` | 1,347 | **0** | **0.00×** |
| `pkg/router` | 250 | **0** | **0.00×** |
| `cmd/ob` | 1,057 | 498 | 0.47× |

### 11.2 发现 F-10（集成测试 build tag 缺失）细节

AGENTS §3.3 要求：

```go
//go:build integration

package mypackage_test
```

实测 4 个 `*_integration_test.go` 的首行：

```text
pkg/credential/keyring_integration_test.go  -> //go:build integration   ✅
pkg/config/install_integration_test.go      -> package config           ❌
pkg/cli/errors_integration_test.go          -> package cli              ❌
pkg/request/builder_integration_test.go     -> package request          ❌
```

后果：

1. 这 3 个文件会在默认 `go test ./...` 中执行（CI 的 `make test-coverage` 也会），与"integration 只在显式 tag 下运行"的约定矛盾。
2. `pkg/config/install_integration_test.go:14-16,225-227` 用 `testing.Short()` 兜底——说明作者**知道**它会在普通测试里跑；`pkg/request/builder_integration_test.go` 直接读写文件系统（`os`/`filepath`），无任何 skip；`pkg/cli/errors_integration_test.go` 纯内存可接受但仍违反命名/门禁约定。
3. 附带违反 §3.1 的 `package mypackage_test` 规范（AGENTS §3.3 示例要求外部测试包），当前 4 个集成测试全部是**内部测试包**（`package config` 等），无法强制"只测导出 API"。

### 11.3 发现 F-21（§4.2 接口化缺失）细节

```go
// pkg/cli/handler.go:31-37
type Handler struct {
	specParser     *spec.Parser
	mapper         *semantic.Mapper
	reqBuilder     *request.Builder
	httpClient     *http.Client
	errorFormatter *ErrorFormatter
	configMgr      *config.Manager
}
```

- AGENTS §4.2："**Interfaces:** Always define dependencies as interfaces, not concrete structs."
- 上述 6 个依赖中 5 个是具体指针类型；`NewHandler`（`handler.go:40-45`）同样接收具体类型。
- 结果：`pkg/cli` 的测试只能通过"构造真实 Parser/Mapper/Builder/Manager"来测 Handler（成本高），而 `errors_test.go`/`args_test.go` 只测了不依赖 Handler 的纯函数——这解释了为什么 892 行的 handler 没有 handler_test.go。
- 同类问题见于 `pkg/mcp`（`ProgressiveHandler`/`Handler` 持有具体 `*Builder`/`*Registry`/`*SearchEngine`）与 `pkg/tui/install.Model`（直接持有 `*config.Manager`）。
- **建议**：为 `Parser`/`Builder`/`Manager`/`Registry` 提取最小接口（`SpecLoader`、`RequestBuilder`、`ConfigStore`、`ToolRegistry`），测试使用轻量 fake；AGENTS 允许"interface is trivial 时手写"，不必引入 mockery。

### 11.4 发现 F-22（§3.2 property test 缺失）细节

已有 property test（3 文件 6 函数）：

| file:line | 函数 | 覆盖对象 |
|---|---|---|
| `pkg/credential/keyring_property_test.go:27` | `TestPropertyCredentialRoundTrip` | Credential 序列化往返 |
| `pkg/credential/keyring_property_test.go:108` | `TestPropertyCredentialDeletion` | 删除不变式 |
| `pkg/credential/keyring_property_test.go:159` | `TestPropertyCredentialIsolation` | 多 app/profile 隔离 |
| `pkg/config/export_property_test.go:15` | `TestPropertyProfileExportExcludesCredentials` | 导出安全不变式 |
| `pkg/config/export_property_test.go:88` | `TestPropertyProfileImportValidation` | 导入校验 |
| `pkg/config/config_property_test.go:15` | `TestPropertyConfigPersistenceRoundTrip` | 配置持久化往返 |

**违反点**：AGENTS §3.2 的 MUST 列表包含 "Parsers and formatters / Serialization/Deserialization logic / Complex algorithms with invariants (e.g., Decode(Encode(x)) == x)"。而：

- `pkg/spec/parser.go`（839 行，OpenAPI 2.0/3.x 检测、YAML/JSON 双格式、缓存合并）**零 property test**；
- `pkg/spec/format.go`（DetectFormat/ToJSON/UnmarshalWithFallback，287 行）**零 property test**；
- `pkg/codegen`（4 个 generator，768 行）**零 property test**（"生成代码可编译"本身就是绝佳 property）。
- 反例价值：`internal/proptest` 已有完备基础设施（`TestParameters`、`AlphaString`、`Identifier`、`MapOf`、`SliceOf`、`IntRange`），**用起来成本很低**——这使缺失更显眼。

**建议**（P2）：补 3 组 property test：
1. `spec/format_property_test.go`：`Unmarshal→ToJSON→Unmarshal` 往返不变式；
2. `spec/parser_property_test.go`：随机合法/非法 version/format 输入不 panic、错误可分类；
3. `codegen/codegen_property_test.go`：生成代码可被 `go/parser`/`node --check`/`python -m py_compile` 解析（或至少 `gofmt` 可格式化），以及对敏感信息永不泄漏的 property（与 `protect_sensitive_info` 联动）。

### 11.5 不可验证条款

| 条款 | 原因 |
|---|---|
| §2 TDD 三步流程 | 过程要求，无法从代码终态验证；仓库无 `.golangci` 之外的 TDD 门禁（如 git hooks），CI 也不检查"测试先于实现" |
| §1.1 不得修改 `.golangci.yml` | 需要 git 历史逐提交审计，超出本次只读评审范围 |
| §5.1 "make fmt-fix && make lint-fix" 全绿 | 工具不可执行（F-01），无法验证 |

---

## 12. 维度十一：一致性

### 12.1 文件组织：包规模与巨型文件

**按包统计**（LOC，自写统计）：

| 包 | 源码 | 测试 | 文件数 | 观察 |
|---|---|---|---|---|
| `pkg/config` | 5,059 | 5,389 | 8 | 8 个文件覆盖 config/profile/install/shim/spec_cache/spec_watcher/export/path——**单一包承担 5 个领域** |
| `pkg/cli` | 2,688 | 1,352 | 4 | args/errors/handler 三个巨型文件 |
| `pkg/mcp` | 2,200 | 1,729 | 8 | 相对均衡 |
| `pkg/credential` | 1,217 | 1,995 | 2 | keyring/security 各 ~600/580 行 |
| `pkg/semantic` | 1,228 | 930 | 3 | 均衡 |
| `pkg/spec` | 1,126 | 2,266 | 2 | parser 839 行偏大 |
| `pkg/request` | 783 | 1,725 | 1+helper | 均衡 |
| `pkg/codegen` | 768 | 293 | 5 | 4 个 generator 分开，好 |
| `pkg/tui/install` | **1,347** | **0** | 1 | 🔴 单文件 |
| `pkg/router` | 250 | **0** | 1 | |
| `cmd/ob` | 1,057 | 498 | 1+test | 🔴 main.go 巨型 |
| `internal/integration` | 0 | 1,020 | 2 test | 仅测试包 |

**Top 10 巨型文件**：

```text
1347  pkg/tui/install/model.go
1057  cmd/ob/main.go
 941  pkg/config/install.go
 924  pkg/config/config.go
 923  pkg/cli/args.go
 892  pkg/cli/handler.go
 838  pkg/cli/errors.go
 839  pkg/spec/parser.go
 816  pkg/config/spec_cache.go
 778  pkg/config/spec_watcher.go
```

### 12.2 发现 F-12［P2］：巨型文件与职责混杂

| 文件 | 混杂的职责 | 拆分建议 |
|---|---|---|
| `pkg/tui/install/model.go`（1347） | 状态机 + 渲染 + 输入校验 + TLS 证书校验 + 历史记录 + auth 流程 | `model.go`（状态机）+ `steps/*.go`（每步 handler）+ `render/*.go` + `validate.go` |
| `cmd/ob/main.go`（1057） | Cobra 命令构造（`new*Cmd` 5 个）+ 打印（`print*` 22 个）+ TUI 启动 + MCP server 启动 + shell completion（6 个 `complete*`） | `cmd/ob/commands/`（5 个命令各一文件）+ `cmd/ob/output/`（打印）+ `cmd/ob/completion.go` |
| `pkg/config/install.go`（941） | 安装流程 + shim 创建（Windows/POSIX）+ PATH 操作 + TUI 交互回调 + 包管理器提示 | `install.go`（流程）+ `shim_unix.go`/`shim_windows.go`（build tag 分平台）+ `path_env.go` |
| `pkg/config/config.go`（924） | 配置类型 + Manager + 校验 + 导入兼容（legacy）+ 错误类型 + 默认值 | `types.go` + `manager.go` + `validate.go` + `legacy.go` + `errors.go` |
| `pkg/cli/args.go`（923） | 参数拆分 + 自动分类 + 类型解析 + body 嵌套 + 校验 + 字面量解析 | `args/split.go` + `args/classify.go` + `args/parse.go` + `args/body.go` |
| `pkg/cli/errors.go`（838） | 错误类型 + 错误格式化（多种）+ 网络错误 + 帮助/示例生成 + 相似度建议 | `errors/format.go` + `errors/help.go` + `errors/suggest.go` |
| `pkg/spec/parser.go`（839） | 加载（file/url/embedded）+ 版本检测 + 校验 + 缓存读写 + 变体策略 | `load.go` + `validate.go` + `cache.go` |

**判断依据**：单文件 >700 行本身不是罪，但这些文件的函数列表显示"3–5 个互不依赖的关注点"（如 `main.go` 的 22 个 print 与 6 个 complete 与 5 个 newCmd 无耦合），拆分成本低、收益明确。

### 12.3 发现 F-23［P2］：TUI 单文件状态机

`pkg/tui/install/model.go`：

- 第 20–38 行：**20 个导出 `Step*` 常量**（`StepSpecInput` … `StepDone`），构成隐式状态机；
- 第 222 行 `NewModel`（45 行）、第 275 行 `Update`（Bubble Tea 主更新，内含巨型 switch）、第 285 行 `handleStepUpdate`（42 行，`//nolint:funlen`）、第 354 行 `renderStepContent`、第 473 行 `View`；
- 第 593–1100 行：大量 `updateXxx`（baseURL/TLS/auth/spinner/…）与 `checkXxx`；
- 第 1312 行 `renderInput`、第 1345 行 `Result`。
- **0 个测试**（F-11）。

**风险**：状态机转移（step → step）没有可验证的迁移表；任何"下一步"改动只能靠人工点击 TUI 验证。**建议**：把 `(step, msg) → (step', cmd)` 抽成纯函数表（不依赖 tea），用表驱动测试覆盖；渲染层用 `teatest`/golden 文件。

### 12.4 发现 F-17［P2］：Context 未传递（10 处库内 `context.Background()`）

| file:line | 上下文 |
|---|---|
| `pkg/cli/handler.go:84` | `loadSpec`：持久化缓存加载 |
| `pkg/spec/parser.go:191` | `LoadSpec`（内部新建 ctx） |
| `pkg/spec/parser.go:534` | 校验路径 |
| `pkg/spec/parser.go:590` | 校验路径 |
| `pkg/spec/parser.go:831` | `ParseSpecFromJSON` |
| `pkg/completion/completion.go:401` | 补全时的 spec 加载 |
| `pkg/router/router.go:216` | MCP server 启动 |
| `pkg/router/router.go:249` | MCP server 启动（另一路径） |
| `cmd/ob/main.go:950` | `factory.RunServer(context.Background(), ...)` |
| `pkg/config/spec_watcher.go:710` | `context.WithCancel(context.Background())`（watcher 生命周期，可接受） |

**问题**：公开 API `Parser.LoadSpec`、`LoadSpecWithPersistentCache`、`Builder.Build`、MCP handler 均**没有 `ctx context.Context` 参数**，导致：

1. CLI 用户 `Ctrl+C` 或超时无法取消 HTTP 请求（`net/http` 请求若以 `http.NewRequest` 而非 `NewRequestWithContext` 创建，则无法取消——建议一并审计 `pkg/request/builder.go`）；
2. 无法在测试中注入 deadline 验证超时行为；
3. `context.Background()` 在库代码内部创建是 Go 反模式（库不应拥有生命周期决策权）。

**建议（M）**：为 `Parser`/`Builder`/`Handler` 的导出入口添加 `ctx context.Context` 首参（AGENTS 未明说，但 Go 惯例），内部传递；`spec_watcher` 的 Background 保留（其生命周期由 `Start/Stop` 管理）并在注释中说明。

### 12.5 发现 F-29［P3］：输出风格四套并存

| 风格 | 出现处 | 证据 |
|---|---|---|
| `fmt.Printf/Println` + emoji | `cmd/ob/main.go`（**188 处** `fmt.Print*`/`fmt.Fprint*`） | `main.go:286-303`、`:443-457`、`:635-672`（含 `📋/👤/📝/✓`） |
| `lipgloss` 样式 | `pkg/tui/install` | `model.go`（View/样式） |
| `text/template` | MCP 工具描述 | `pkg/mcp/progressive.go:143,243,334` |
| `strings.Builder` + 手写帮助 | CLI 帮助/错误建议 | `pkg/cli/handler.go:686-701`、`pkg/cli/errors.go` 各 `formatXxxError` |

**问题**：同一条用户路径（`ob list`）的输出格式在 `cmd/ob/main.go`，而 `ob <app> <verb>` 的帮助在 `pkg/cli/handler.go`，两者对 `--yaml` 默认值、emoji、缩进的约定不一致；**无法统一快照测试**。建议：CLI 输出统一到 `cmd/ob/output` 包（纯函数 + golden test），TUI 保持 lipgloss，MCP 保持模板。

### 12.6 发现 F-30［P3］/F-31［P3］：测试命名与 test-only 包

- `internal/integration/integration_test.go`、`real_api_test.go`：均以 `// Package integration provides end-to-end integration tests for OpenBridge.` 开头，但**首行不是 build tag**，且目录下无源码 → 既不满足 AGENTS §3.1（无源可对），也不满足 §3.3（无 build tag）。
- `pkg/request/builder_integration_test.go` 内部测试函数命名为 `TestIntegration_NestedObjectsAndArrays`（下划线风格，非 Go 惯例的 `TestXxx`）——集成测试可以例外，但与 AGENTS §3.2 的 `TestProperty*` 命名哲学不一致（建议统一为 `TestBuilderIntegration_...`）。
- `pkg/config/install_integration_test.go` 用 `testing.Short()` 而非 build tag（另一种门禁）——与 `pkg/credential` 的 build tag 方式并存，团队需要二选一。

### 12.7 发现 F-33［P3］/F-34［P3］：治理与工程卫生

- **F-33**：全仓 0 个 `TODO/FIXME/XXX/HACK`（优点），但 `.golangci.yml:14` `issues: {}` 空配置会按默认 `max-issues-per-linter: 50`/`max-same-issues: 3` 截断 lint 输出；建议把本报告 F-xx 转为可跟踪 issues。
- **F-34**：仓库根同时存在 Go 工程与 vitepress（`package.json:1-29`、`node_modules/`，`.gitignore:36`），Makefile 无 Node 目标 → 环境噪声；建议补 `docs-install`/`docs-build`。

### 12.8 发现 F-24［P3］：`.golangci.yml` 陈旧排除规则

`.golangci.yml:102-116` 共 4 条 `exclusions.rules`，逐条核对目标函数**实际复杂度**（gocyclo）：

| 配置项 | 目标 | 实际 gocyclo | 结论 |
|---|---|---|---|
| `.golangci.yml:102-105` | `cmd/ob/main.go` + text `newInstallCmd` | **<10**（未进入 top 200，`main.go:353`） | 陈旧（远低于 25，永不触发） |
| `.golangci.yml:106-108` | `pkg/cli/handler.go` + text `ExecuteCommand` | **7**（`handler.go:318`） | 陈旧 |
| `.golangci.yml:109-111` | `pkg/completion/completion.go` + text `CompleteFlagValues` | **<10**（未进入 top 200，`completion.go:339`） | 陈旧 |
| `.golangci.yml:112-114` | `pkg/config/install.go` + text `InstallApp` | **7**（方法 `(*Manager).InstallApp`，`install.go:345`） | 陈旧（且该 text 会同时匹配方法，语义模糊） |

**危害**：排除规则本应记录"已知无法降低的复杂度"，但 4 条全部指向实际简单函数——说明这些规则是"历史上为某个版本写的"再未复核。移除以恢复 lint 敏感性，或改写成文件级注释说明。**同时注意**：`.golangci.yml:97-100` 对 `_test.go` 排除了 gocyclo/gocognit/funlen/unparam，但**没有排除 dupl**——而 7 组 dupl 全部在测试文件里，靠 8 处 `//nolint:dupl` 手动抑制。建议把 dupl 也加入测试排除（或接受现有 nolint 策略但统一写法）。

---

## 13. 建议的 `.golangci.yml` 增量规则（仅建议，未修改文件）

> 依据 AGENTS §1.1，`.golangci.yml` 是"宪法"，任何修改需人工批准。以下仅为**建议草案**，供 Lead/维护者评估；本评审未修改该文件。

### 13.1 建议启用的 linter（与当前问题一一对应）

```yaml
linters:
  enable:
    # 1) nolint 治理（F-03/F-28）：强制理由、禁止无效指令、禁止空白指令
    - nolintlint
    # 2) 重复字符串常量（F-15）：“bearer”/“json”/“api_key” 等
    - goconst
    # 3) 标准库字面量（F-15）：http 方法/scheme/状态码
    - usestdlibvars
    # 4) 错误命名与 nil 错误（F-08/F-09）
    - errname
    - nilerr
    # 5) context 传递（F-17）
    - contextcheck
    - containedctx
    # 6) 常见 Go 坏味道（F-29 等）
    - gocritic
    # 7) 安全（凭据/密钥仓库必须）：先以基线方式启用
    - gosec
```

**逐条理由**：

| Linter | 解决的问题 | 预期新增告警（估算） |
|---|---|---|
| `nolintlint` | `//nolint` 无理由/无 linter 名/未使用 | 1 处无理由（model.go:605）+ 风格统一 |
| `goconst` | `"bearer"` 等字符串重复 ≥3 次 | ≥6 组（F-15） |
| `usestdlibvars` | `"GET"/"POST"/"http"/"https"` 用标准库常量 | ≥12 处 |
| `errname` | sentinel 名 `Err*` / 错误类型 `XxxError` | 0（当前类型命名已正确），防止回退 |
| `nilerr` | `if err != nil { return nil }` 类逻辑错误 | 待测（本地无法运行） |
| `contextcheck` | 上下文未传递 | ≥10 处（F-17） |
| `containedctx` | struct 里存 context | 待测 |
| `gocritic` | `singleCaseSwitch`/`ifElseChain`/`appendAssign` 等 | 待测（建议先用 `disable-all` + 白名单） |
| `gosec` | 凭据/文件权限/弱随机/命令注入 | 凭据包高价值；建议启用后对存量加 `//nolint:gosec // 理由` 或 exclusions 基线 |

### 13.2 建议调整的阈值与设置

```yaml
linters:
  settings:
    # 复杂度（F-03/F-25）：存量已高，建议“只减不增”而非一步到位
    gocyclo:
      min-complexity: 20        # 25 -> 20（对存量用 exclusions 白名单）
    gocognit:
      min-complexity: 20        # 25 -> 20
    maintidx:
      under: 15                 # 保持（默认 20），但需修好可运行性
    funlen:
      lines: 50                 # 保持，先把 6 处未抑制违规处理掉（F-04）
      statements: 40
    dupl:
      threshold: 70             # 保持；建议对 *_test.go 启用排除（当前 7 组靠 nolint）
    revive:
      rules:
        # 新增：导出符号必须有注释（F-19）、包必须有 package comment（F-20）
        - name: exported
        - name: package-comments
        - name: error-naming        # var ErrXxx / type XxxError（F-08）
        - name: context-as-argument # ctx 必须是第一个参数（F-17）
        - name: context-keys-type
        - name: unused-receiver
        # 现有 max-control-nesting 从 3 调整为 4（先消除 7 处 nest=4 还是收紧，
        # 取决于团队选择；若维持 3，需处理 §3.6 的 7 个函数）
```

### 13.3 建议的 exclusions / issues 设置

```yaml
issues:
  # 不要截断输出（F-33）
  max-issues-per-linter: 0
  max-same-issues: 0

linters:
  exclusions:
    rules:
      # 删除 4 条陈旧规则（F-24）
      # - path: cmd/ob/main.go ... newInstallCmd
      # - path: pkg/cli/handler.go ... ExecuteCommand
      # - path: pkg/completion/completion.go ... CompleteFlagValues
      # - path: pkg/config/install.go ... InstallApp
      #
      # 可选：测试文件整体排除 dupl（当前 7 组测试克隆均靠 nolint）
      - path: _test\.go
        linters:
          - dupl
      #
      # 可选：对 test_helpers.go 的 funlen/dupl 豁免（需先治理 F-13）
      - path: test_helpers\.go
        linters:
          - funlen
          - dupl
```

### 13.4 建议的 formatters 与 CI 门禁

```yaml
formatters:
  enable:
    - goimports
    - gofumpt      # 新增：当前 20 个文件漂移（F-32）
```

CI 建议（`.github/workflows/ci.yml`，仅建议）：

```yaml
# 1) 固定版本，避免 latest 漂移（F-02）
- uses: golangci/golangci-lint-action@v9
  with:
    version: v2.8.0          # 与 Makefile 一致
    args: --timeout=5m
# 2) 固定 Go 版本与 go.mod 对齐
- uses: actions/setup-go@v5
  with:
    go-version-file: go.mod
# 3) 把 lint 结果落盘为 artifact，便于离线复核
```

---

## 14. 优先级行动清单（S/M/L 工作量）

> S ≈ 0.5 天以内（单文件/配置级）；M ≈ 1–3 天；L ≈ 1 周以上（跨包重构）。

| # | 优先级 | 工作量 | 任务 | 关联发现 | 验收标准 |
|---|---|---|---|---|---|
| 1 | **P0/P1** | S | 固定并跑通 lint：`Makefile` 与 CI 钉死 `golangci-lint v2.8.0`；CI 用 `go-version-file: go.mod`；本地用 go1.25 工具链 | F-01/F-02 | CI 与本地 `make lint` 输出一致且可复现 |
| 2 | **P1** | S | 修复 F-04：对 6 个未抑制 funlen 函数拆分或补 `//nolint:funlen // 理由`（优先拆分 `Execute`/`Singularize`/`Pluralize`） | F-04 | lint 在 funlen 上 clean 或每条 nolint 有理由 |
| 3 | **P1** | S | 修 F-06：处理 7 处 nest≥4（把 `max-control-nesting` 调回 4 或重构 `collectBodyPropertyFlags`） | F-06 | revive 该规则 clean |
| 4 | **P1** | S | 修 F-05：把 2 组 dupl 克隆合并（`test_helpers` 泛型化；codegen 模板化） | F-05 | `dupl -t 70` 在非测试文件 0 克隆 |
| 5 | **P1** | S | 修 F-10：给 3 个 `*_integration_test.go` 加 `//go:build integration`；`internal/integration` 测试重命名为 `*_integration_test.go` | F-10 | `go test -short ./...` 不跑集成；tag 构建通过 |
| 6 | **P1** | M | 错误语义化：定义各包 sentinel errors（var 块），用 `errors.Is/As` 替换 8 处字符串匹配 | F-07/F-08 | `grep 'strings.Contains(err'` = 0；新增 10+ sentinel |
| 7 | **P1** | M | 错误包装：115 处裸 `return err` 中至少 config 包全部补 `%w` 上下文；22 处 `%v` 改 `%w` | F-09 | `%w` 包装率 >85%；errorlint clean |
| 8 | **P1** | M | 为 F-03 两个热点函数补测试（表驱动覆盖 auto 分类与类型解析的边界），再决定是否拆函数 | F-03/F-11 | 覆盖率 ≥70%，gocognit 下降或 nolint 保留但理由充分 |
| 9 | **P2** | S | 删除 `.golangci.yml` 4 条陈旧排除；`issues: max-issues-per-linter: 0` | F-24/F-33 | 配置与目标函数一致 |
| 10 | **P2** | S | 启用 `nolintlint` + `goconst` + `usestdlibvars`，先修低风险项 | F-03/F-15/F-28 | 新规则 0 告警或全部 nolint 有理由 |
| 11 | **P2** | S | 统一 auth/format/param 常量（`AuthType`/`OutputFormat`/`ParamLocation`） | F-15 | 魔法字符串出现次数降为 0 |
| 12 | **P2** | S | 补 24 个缺失导出注释；package doc 收敛到每包 1 处（+ `doc.go`） | F-19/F-20 | `go doc` 无空注释；grep package comment = 1/包 |
| 13 | **P2** | S | 删除/迁移 test_helpers 中的 testing 依赖（用 `export_test.go` 或 `internal/testutil`） | F-13 | 生产包依赖树不含 testing 相关 helper |
| 14 | **P2** | M | ghodss/yaml 替换（sigs.k8s.io/yaml 或 yaml.v3 归一化），移除 yaml.v2 | F-18 | `go mod why gopkg.in/yaml.v2` 为空 |
| 15 | **P2** | M | 依赖接口化：为 Parser/Builder/Manager/Registry 提取小接口并用于 CLI/MCP 构造 | F-21 | handler 可用 fake 测试 |
| 16 | **P2** | M | 补 property test：spec format/parser + codegen 生成可编译性 | F-22 | 3 个新 `TestProperty*` 通过 |
| 17 | **P2** | M | 拆分 `pkg/cli/args.go`（923 行）与 `pkg/cli/errors.go`（838 行） | F-12 | 每文件 <500 行，函数职责单一 |
| 18 | **P2** | L | 拆分 `pkg/tui/install/model.go`（1347 行）为 step 包 + 补状态机测试 | F-12/F-23 | model.go <400 行；step 迁移表测试覆盖 |
| 19 | **P2** | L | 为 `pkg/cli/handler.go`、`pkg/router/router.go` 补单元测试（依赖接口化后成本下降） | F-11/F-21 | 两包覆盖率 ≥60% |
| 20 | **P3** | S | 命名治理：`Get*` 逐步改 `Xxx()`；`baseUrlInput`→`baseURLInput`；`TestParameters` 更名 | F-26 | 全仓命名规范文档 + lint（revive exported） |
| 21 | **P3** | S | 启用 `gofumpt` formatter 并一次性格式化 20 个文件 | F-32 | `gofumpt -l` = 0 |
| 22 | **P3** | S | 统一输出层（cmd/ob/output）与错误字符串大小写策略 | F-27/F-29 | golden 测试通过 |
| 23 | **P3** | S | Makefile 增加 docs 目标、README 说明 Node 非必需 | F-34 | 新成员无需 Node 即可 `make test` |
| 24 | **P3** | M | 对 exports 做审计：内部包迁 `internal/`；对外 API 清单 + 文档 | F-16 | `go doc` API 清单评审通过 |

---

## 15. 如果只做三件事

**第一件：让"宪法"真正可执行（F-01/F-02 + F-04/F-05/F-06/F-24）。**
当前状态是"配置很严格、但从未运行"——比"配置宽松"更危险，因为它制造了虚假的安全感。固定 golangci-lint 版本、CI 与本地一致、清除 4 条陈旧排除、处理 6 处 funlen/2 组 dupl/7 处嵌套，让 lint 第一次真正绿灯。预计 **S 工作量**，但回报最大。

**第二件：把错误从"字符串"变成"类型"（F-07/F-08/F-09）。**
0 个 sentinel + 8 处 `strings.Contains(err.Error())` + 115 处裸 `return err`，意味着"改一句错误文案就可能让用户看到 "Error: unknown" 而不是友好提示"。定义 `Err*` var 块、用 `errors.Is/As` 替换字符串分类、给 config 包补 `%w` 上下文。这是**唯一一项会同时改善用户体验、可测试性、可观测性**的重构。预计 **M 工作量**。

**第三件：把最复杂/最没测试的两个地方变"可测"（F-03/F-11 + F-12/F-18）。**
`pkg/cli/args.go` 的 `parseRequestParamsAuto`（gocognit 35、82 行）与 `parseValueWithContext`（93 行）是全 CLI 的解析核心，恰好被 `//nolint` 豁免且无直接测试；`pkg/tui/install/model.go` 1347 行零测试。先给它们写表驱动测试/抽纯函数，再谈拆分。预计 **M–L**，但这决定了未来每次改动是否安全。

---

## 附录 A：原始命令输出（证据留档）

### A.1 golangci-lint v1.64.8（本机默认）

```text
$ golangci-lint --version
golangci-lint has version v1.64.8 built with go1.26.1 from (unknown, modified: ?,
mod sum: "h1:y5TdeVidMtBGG32zgSC7ZXTFNHrsJkDnpO4ItB3Am+I=") on (unknown)

$ golangci-lint run ./...
Error: can't load config: sort-results should be 'true' to use sort-order
Failed executing command with error: can't load config: sort-results should be 'true' to use sort-order
```

### A.2 golangci-lint v2.8.0（本机备份二进制）

```text
$ golangci-lint-v2 --version
golangci-lint has version 2.8.0 built with go1.25.5 from (unknown, modified: ?,
mod sum: "h1:wJnr3hJWY3eVzOUcfwbDc2qbi2RDEpvLmQeNFaPSNYA=") on (unknown)

$ golangci-lint-v2 run ./...
panic: file requires newer Go version go1.27 (application built with go1.25) [recovered, repanicked]
    go/types.(*Checker).handleBailout ...
    github.com/golangci/golangci-lint/v2/pkg/goanalysis.(*loadingPackage).convertError ...
    github.com/golangci/golangci-lint/v2/pkg/goanalysis.(*loadingPackage).loadFromSource(...)
    ...exit 2
```

### A.3 golangci-lint-v2 config verify（配置 schema 校验）

```text
$ golangci-lint-v2 config verify
The command is terminated due to an error: [.golangci.yml] validate:
compile schema: failing loading "https://golangci-lint.run/jsonschema/golangci.v2.8.jsonschema.json":
Get "https://golangci-lint.run/jsonschema/golangci.v2.8.jsonschema.json":
context deadline exceeded (Client.Timeout exceeded while awaiting headers)
[exit 3]
```

### A.4 尝试用 GOTOOLCHAIN 固定 go1.25.5（Lead 补充证据）

```text
$ GOTOOLCHAIN=go1.25.5 golangci-lint run ./...
level=error msg="Running error: context loading failed: failed to load packages:
 go: downloading go1.25.5 (darwin/arm64); verifying module:
 checksum database disabled by GOSUMDB=off"
[exit 4]
```

> Lead 另在 `/tmp/gobin/golangci-lint` 用本机 Go 1.27.3 构建 v2.8.0，仍因 `x/tools` 无法解码 Go 1.27 export data 失败（typecheck 2 issues）。**本轮评审无法获得任何 golangci-lint 真实结果**。

### A.5 staticcheck

```text
$ staticcheck ./...
-: internal error in importing "github.com/clipperhouse/stringish"
   (cannot decode ..., export data version 5 is greater than maximum supported version 2) (compile)
-: internal error in importing "internal/byteorder" ... (compile)
-: internal error in importing "internal/cpu" ... (compile)
-: internal error in importing "internal/goarch" ... (compile)
-: internal error in importing "math/bits" ... (compile)
-: internal error in importing "unicode" ... (compile)
-: internal error in importing "unicode/utf8" ... (compile)
```

### A.6 go vet / gofmt / goimports / gofumpt / errcheck / misspell

结果为：`go vet`、`gofmt -l`、`goimports -l`、`errcheck`、`misspell` 均**无输出（通过）**；`gofumpt -l` 命中 20 个文件（清单见 §2.3）。

### A.7 gocyclo（−over 15）

```text
$ gocyclo -over 15 <非测试 .go>
25  credential looksLikeCredential              pkg/credential/security.go:373
25  cli        parseValueWithContext            pkg/cli/args.go:524
19  install    (Model).renderStepContent        pkg/tui/install/model.go:354
19  install    (Model).handleStepUpdate         pkg/tui/install/model.go:285
19  cli        parseRequestParamsAuto           pkg/cli/args.go:314
```

### A.8 gocognit（−over 15）

```text
$ gocognit -over 15 <非测试 .go>
35  cli        parseRequestParamsAuto                 pkg/cli/args.go:314
26  cli        parseValueWithContext                  pkg/cli/args.go:524
25  credential looksLikeCredential                    pkg/credential/security.go:373
25  cli        (*Handler).showParameterValidationError pkg/cli/handler.go:844
19  completion collectBodyPropertyFlags               pkg/completion/completion.go:218
18  install    (Model).updateBaseURL                  pkg/tui/install/model.go:593
17  mcp        formatToolParameters                   pkg/mcp/progressive.go:334
16  cli        classifyParamInSpec                    pkg/cli/args.go:273
[stderr] gocognit: note: some calls could not be resolved from the source alone;
         recursion may be under-counted.
```

### A.9 dupl（阈值 70，全仓）

```text
$ dupl -t 70 $(find . -name '*.go' -not -path './node_modules/*')
found 2 clones:
  ./pkg/mcp/progressive_test.go:173,199
  ./pkg/mcp/progressive_test.go:187,213
found 3 clones:
  ./pkg/mcp/progressive_test.go:173,185
  ./pkg/mcp/progressive_test.go:187,199
  ./pkg/mcp/progressive_test.go:201,213
found 2 clones:
  ./pkg/codegen/codegen_test.go:221,233
  ./pkg/codegen/codegen_test.go:235,247
found 2 clones:
  ./pkg/mcp/engine_predicate_test.go:44,60
  ./pkg/mcp/engine_predicate_test.go:62,78
found 4 clones:
  ./pkg/mcp/engine_predicate_test.go:30,42
  ./pkg/mcp/engine_predicate_test.go:133,146
  ./pkg/mcp/engine_predicate_test.go:180,192
  ./pkg/mcp/engine_predicate_test.go:194,206
found 2 clones:
  ./pkg/cli/handler.go:686,701
  ./pkg/codegen/go.go:139,155
found 2 clones:
  ./pkg/config/test_helpers.go:53,64
  ./pkg/semantic/test_helpers.go:12,23
found 2 clones:
  ./pkg/mcp/engine_predicate_test.go:80,96
  ./pkg/mcp/engine_predicate_test.go:148,165
found 2 clones:
  ./pkg/cli/args_test.go:199,223
  ./pkg/cli/args_test.go:227,251

Found total 9 clone groups.
```

---

## 附录 B：统计口径、局限与未验证项

### B.1 统计口径

| 指标 | 口径 |
|---|---|
| 源码/测试 LOC | `wc -l` 直接累加（空行计入） |
| 函数行数 | AST `End.Line - Pos.Line + 1`（含签名与闭括号） |
| funlen 估算 | body 行数 = `End.Line - Pos.Line - 1`（funlen v0.2.0 的实现口径）；估算误差 ±1–2 行 |
| 语句数 | AST 常见语句节点计数（Block/Expr/Assign/Return/Decl/If/For/Range/Switch/TypeSwitch/Select/Go/Defer/Send/IncDec/Branch/Labeled） |
| 嵌套深度 | if/for/range/switch/typeswitch/select 的最大层数（else-if 按 `else` 内的 if 计） |
| 导出注释覆盖 | 声明前 3 行内存在 `//` 注释即计入（对接口实现方法不区分） |
| 过度导出 | 导出函数标识符在**声明包以外**的 grep 计数为 0 |
| 错误包装率 | `%w` 计数 / `fmt.Errorf` 总数（同一调用只计一次） |

### B.2 未验证项（如实声明）

| 项目 | 原因 | 影响 |
|---|---|---|
| golangci-lint 真实结果 | v1 配置不兼容；v2 与 go1.27 export data 不兼容；toolchain 下载失败（附录 A.4） | F-03～F-06/F-24 为"配置语义 + 等效工具"推断，非 lint 实测 |
| staticcheck | 工具旧于本机 Go（export data version 5 > 2） | 未验证 SA/ST/QF 类问题 |
| maintidx | 运行错误（gopter 无类型信息） | 维护指数维度未量化，仅定性 |
| `unused`/`ineffassign` | 无独立工具，且 lint 不可运行 | 死代码只做导出面分析，非精确 |
| 测试执行 | 未运行 `go test`（本任务只读且时间有限；运行测试会产生缓存文件） | 本报告不评价测试通过率/覆盖率数值 |
| 外部消费者 | 无法确认 `pkg/*` 是否被仓库外项目导入 | "过度导出"结论为仓库内视角 |
| git 历史审计 | 只读 `git log -3`，未逐提交审计 `.golangci.yml` 变更 | §1.1"不得改配置"未做历史结论 |
| 依赖 CVE | 未运行 `govulncheck` | 安全性另见安全评审/performance 报告 |

---

## 附录 C：与其它评审报告的边界

本报告只回答"**代码风格与可维护性**"，以下内容属于其它评审员职责，本文仅提供线索，不展开：

| 主题 | 归属报告 | 本文相关线索 |
|---|---|---|
| 性能（内存拷贝、字符串拼接、HTTP client 复用、缓存策略） | performance 报告 | `strings.Builder`/模板化、`spec_cache`、`http.Client` 复用 |
| 架构（包依赖方向、接口设计、MCP 协议分层） | architecture 报告 | F-16/F-17/F-21 的接口化与 internal/ 化建议 |
| 流程/工程（CI、发布、覆盖率门禁、DCO、文档同步） | process 报告 | F-01/F-02/F-18/F-33/F-34 的 CI 与依赖流程部分 |
| 安全（凭据泄漏、gosec、日志敏感信息） | 安全相关评审 | F-27 的展示型错误、`RedactCredential` 正例、`gosec` 被禁用 |

> **本报告未修改任何文件（除本文件外），未执行 git 写操作，未运行任何 `-w/--fix` 命令。**
> 报告结束。

