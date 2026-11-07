# OpenBridge 批次 3 修复交付报告

> 上游：[`FIX-PLAN-BATCH3.md`](./FIX-PLAN-BATCH3.md)、[`FIXES-BATCH2.md`](./FIXES-BATCH2.md) §5、[`ENGINEERING-DIAGNOSIS.md`](./ENGINEERING-DIAGNOSIS.md)（F-02/F-18）
> 基线：批次 2 提交 `c2b103f`（已推送）
> 方式：Agent Teams 3 路并行（W1 索引 P3 / W2 组合根 / W3+W4 卫生与依赖）+ Lead 走线、补接线（W1c）与终验
> 状态：**全部完成**；全部门禁通过；DCO 签名提交并推送

---

## 1. 结论摘要

| 项 | 批次 2 后 | 批次 3 后 |
| --- | :--- | :--- |
| 命令级 `--help`（`ob <app> <res> <verb> --help`） | 仍解析完整 spec（120ms 级） | 走派生索引 **11.0ms warm**（Lead e2e：116.6→11.0ms，10.6×；W1 基准 148.6→11.5ms） |
| flag 名 / 枚举值补全 | 解析 spec（135/83ms 级）且枚举值未接入 shell 协议 | 索引路径 **9.7ms / 10.1ms**；枚举值补全已接入 `__complete`（Lead e2e 11.1ms） |
| 索引体积 | 2.61MiB（20k ops，67%） | **516KB（12.6%）**，**−81%**，满足 ≤30% 目标 |
| 组合根 | `cmd/ob/main.go` 1,145 行、`init()` 全局可变依赖 9 个 | **main.go 37 行**；`internal/app.Deps` 显式构造，无全局依赖/init |
| 依赖 | `ghodss/yaml`（已归档）+ `yaml.v2` 间接依赖 | 改用 `yaml.v3`；`go mod tidy -diff` 干净；构建图无 ghodss/yaml.v2 |
| 工程门禁 | codecov floor 58%、无 SECURITY/CODEOWNERS/editorconfig | floor **70%**/patch 75%、SECURITY.md/CODEOWNERS/.editorconfig、actionlint、docs PR 预检、`make bench` |
| 覆盖率 | 71.9% | **73.7%** |
| 测试 | 670 | **696** |

## 2. 工作流与交付

### W1 派生索引 P3（perf-analyst）—— 命令级 help / flag 补全 / 裁剪

- **DTO v2**（`DerivedIndexSchemaVersion` 1→2）：`DerivedOperation` 增加参数元数据（`Name/In/Required/Type/Types/Enum/Description/Example…`）与请求体字段元数据（`Name/Required/Type/Enum/…`）、`BodyRequired/BodyMediaType/HasBody`。
- **Description 裁剪**：按 rune 裁剪 ≤200 字符并记录 `DescriptionTruncated`；新增 `FindOperation/CommandSpec/CommandHelpIndexable/FlagNames/EnumValues`。
- **命令级 help**：走索引重建 `openapi3.Operation`，复用同一 formatter 渲染；冷/热输出**逐字节一致**；不可索引场景自动回退解析。
- **flag 名/枚举值补全**：索引优先、缺失回退解析。
- **W1c 补接线（Lead e2e 发现后）**：`internal/app` 的 `completeRunArgs` 新增 `completeFlagValue`——`--flag <TAB>` 返回枚举值、`--flag=prefix <TAB>` 返回 `--flag=<value>` 候选、其他场景保持原行为，首次补全即生成索引。

**基准（真实二进制，隔离 HOME）**：

| 场景（4.09MB / 1,300 ops；Lead e2e 1.13MiB / 1,800 ops） | cold | warm | 加速 |
| --- | ---: | ---: | ---: |
| 命令级 `--help`（W1） | 148.6ms | **11.5ms** | 12.9× |
| 命令级 `--help`（Lead e2e） | 116.6ms | **11.0ms** | 10.6× |
| flag 名补全 | 135.5ms | **9.7ms** | 14.0× |
| flag 枚举值补全 | 83.4ms | **10.1ms** | 8.3× |

索引体积（写入 `tmp+fsync+rename`、0600、CRC/版本自愈）：**2,739,667B → 516,705B（−81%）**；长描述合成测试 19.4%。

### W2 组合根重构（arch-reviewer）—— 架构 F-02

- 新增 `internal/app`：`Deps`（Config/Parser/Credential/Mapper/Builder/CLI/MCP/Router/Completion）、`New()` 显式构造（config fatal、credential degrade warning 与现状一致；Parser 保留远端/索引缓存）、`Deps.Run`/`Run` 封装 shim 判定 + cobra + 直连路由。
- `cmd/ob/main.go` 1,145 → **37 行**（仅 ldflags 变量 + 薄 main）；原 49 个函数迁入 `internal/app/commands.go`，全部转为 `*Deps` 方法。
- 原 16 个 main 行为测试迁移到 `internal/app` 并改为 Deps 注入（删除全局替换 hack）；新增 2 个测试。
- **行为等价**：基线（`c2b103f`）与重构后二进制的 **21/21 场景 stdout+stderr+exit code 完全一致**（含 install/run/info/路由/MCP probe/shim）；Lead 另做 9/9 命令对比一致。

### W3 工程卫生（process-auditor）

- `codecov.yml`：project floor **70%**、patch **75%**（保持 auto/1% 默认）。
- 新增 `SECURITY.md`（支持版本/报告流程/SLA/scope）、`CODEOWNERS`（`* @nomagicln`）、`.editorconfig`。
- `package.json`：删除不存在的 `main: index.js`、`private: true`、keywords/author/`engines.node>=22`；deploy-docs Node 20→22。
- `Makefile`：新增非阻塞 `bench` 目标（pkg/spec + pkg/cli 热路径）。
- CI：新增 `actionlint`（pin v1.7.12）与 docs PR 预检（仅 PR + docs 变更时 `npm ci && npm run docs:build`）。
- `Makefile`/`ci.yml` 既有 lint/test/build/security 逻辑未改。

### W4 依赖治理（process-auditor）

- `pkg/spec/format.go`：用 `gopkg.in/yaml.v3` 解码 + Node 归一化替换归档的 `ghodss/yaml`，兼容 unquoted timestamp（保持字符串）、YAML 1.1 布尔、重复键 last-wins、非字符串键、merge/alias；`UnmarshalStrategy`/`Unmarshal` 语义不变。
- 等价性：**44 例旧实现 vs 新实现矩阵全部 SAME** 后才落码；`format_test.go` 新增 30 个行为等价子测试 + 错误用例。
- `go.mod/go.sum`：移除 `ghodss/yaml v1.0.0` 与 `gopkg.in/yaml.v2 v2.4.0`；`go mod tidy -diff` 干净；`go list -deps -test ./...` 无两者。
- 说明：`go list -m all` 仍列出两者作为 **module graph 遗留节点**（viper/etcd→grpc-gateway 链路），非构建依赖，需升级旧链路才能消失（留批次 4）。

### W5 Lead 走线

- 发现并补齐 W1c 枚举补全接线；清理 `.golangci.yml` 中已失效的 `cmd/ob/main.go` gocyclo 豁免（当前全仓 lint 0 issue，非"消音"）。
- 全量门禁、三批 e2e 回归、提交与推送。

## 3. 验收证据（Lead 最终执行）

| # | 检查 | 结果 |
| :---: | --- | --- |
| 1 | `go build ./...` | ✅ |
| 2 | `gofmt -l` / `goimports -l` | ✅ 0 / 0 |
| 3 | `go vet ./...` + `go vet -tags=integration ./...` | ✅ |
| 4 | `go test -short -count=1 ./...` | ✅ 14 包 |
| 5 | `go test -race -short -count=1 ./...` | ✅ 14 包 |
| 6 | `go test -tags=integration -count=1 ./...` | ✅ 15 包 |
| 7 | `golangci-lint v2.15.0 run ./...` | ✅ **0 issues** |
| 8 | `govulncheck ./...` | ✅ **0 可达漏洞** |
| 9 | `go mod tidy -diff` | ✅ 空 |
| 10 | 覆盖率 | ✅ **73.7%**（批次 2：71.9%） |
| 11 | `npm run docs:build` | ✅ |
| 12 | `actionlint v1.7.12 .github/workflows/*.yml` | ✅ exit 0 |
| 13 | 批次 3 e2e（`/tmp/lead_verify_b3.py`，19 断言） | ✅ **19/19 PASS** |
| 14 | 批次 2 回归（20 断言） | ✅ **20/20 PASS** |
| 15 | 批次 1 回归（18 断言） | ✅ **18/18 PASS** |
| 16 | 批次 0 回归（22 断言） | ✅ **22/22 PASS** |

### 3.1 批次 3 e2e 关键输出

```
PASS A3 command help warm uses index | cold=116.6ms warm=11.0ms speedup=10.6x
PASS A4 command help output stable
PASS A5 help shows params/enums
PASS A6 index trimmed | index=0.55MiB ratio=49%
PASS A7 flag enum completion | 11.1ms out=['active', 'archived']
PASS A8 index path matches parse path
PASS B equivalent: --help / --version / list / info / run <app> --help / <app> --help / bogus-command / --badflag / --json   (9/9)
```

## 4. 行为变更与兼容性

| 变更 | 影响 | 兼容性 |
| --- | --- | --- |
| 命令级 help/flag 补全走索引 | 冷启动首次写索引，之后约 10ms | 输出逐字节一致；不可索引场景回退解析 |
| 索引 DTO v2 | 旧索引文件版本不匹配自动删除重建 | 无需手动清理 |
| flag 值补全接入 shell 协议 | `--flag <TAB>` / `--flag=prefix <TAB>` 生效 | 新增能力；其他补全场景不变 |
| `cmd/ob/main.go` → `internal/app` | 对用户无影响（ldflags 路径保持 `-X main.version`） | 21/21 行为等价 |
| ghodss/yaml → yaml.v3 | YAML→JSON 语义保持一致（44 例矩阵 + 30 子测试） | 无用户可见变化 |
| lint 配置删除失效豁免 | 全仓 lint 仍 0 issue | 非消音，纯清理 |

## 5. 遗留项（批次 4 候选）

| 项 | 依据 | 说明 |
| --- | --- | --- |
| 派生索引 P4（执行路径 index） | proposals/02 | 需 handler/request-builder 改造，L，POC gate |
| `pkg/config` 拆分（10.4k 行 / 243 导出） | 架构 F-05 | 组合根已就绪，下一步可拆 install/shim/TUI prompt |
| 错误模型 sentinel 化 + `PrintedError` 语义 | 架构 F-22/F-23 | 字符串嗅探仍在 `pkg/cli/errors.go` |
| `go list -m all` 遗留 ghodss/yaml.v2 module graph 节点 | W4 风险 | 需升级 viper/etcd 旧链路 |
| 索引 DTO 可选 body 属性顺序 | W1 风险 | formatter 与解析路径同样受 map 顺序影响 |
| `internal/app/commands.go` 1,050 行 | W2 风险 | 可按 install/query/mcp 再拆文件 |
| shell 补全 `--flag value<TAB>`（空格形式） | W1c 风险 | 当前只支持 `--flag <TAB>` 与 `--flag=` |

## 6. 复现验收

```bash
cd /Users/fangzhiheng/Projects/github.com/nomagicln/open-bridge
go build ./... && go vet ./... && go vet -tags=integration ./...
gofmt -l pkg cmd internal && goimports -l pkg cmd internal
go test -short -count=1 ./... && go test -race -short -count=1 ./...
go test -tags=integration -count=1 ./...
go mod tidy -diff
/tmp/gobin2/golangci-lint run ./... && /tmp/gobin2/govulncheck ./...
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/*.yml
python3 /tmp/lead_verify_b3.py   # 19 断言（批次 3）
python3 /tmp/lead_verify_b2.py   # 20 断言（批次 2 回归）
python3 /tmp/lead_verify_b1.py   # 18 断言（批次 1 回归）
python3 /tmp/lead_verify.py      # 22 断言（批次 0 回归）
npm run docs:build
```

> 结论：批次 3 完成派生索引 P3（命令级 help/flag 枚举补全 ~10ms、索引体积 −81%）、组合根重构（main.go 1,145→37 行、行为 21/21 等价）、工程卫生与门禁加固、ghodss/yaml 依赖替换；覆盖率 71.9%→73.7%，全部门禁与三批 e2e 回归通过。
