# OpenBridge AI Coding Instructions

## 核心约束

### 架构原则

- **零代码生成**: 所有行为由 OpenAPI Spec 动态驱动，禁止为特定 API 生成静态代码
- **依赖注入**: 所有 Handler 通过构造函数注入依赖，禁止全局状态（`init()` 中的全局变量仅用于 main.go 启动）
- **单向数据流**: `OpenAPI Spec → Parser → Mapper → Handler → Builder → HTTP Request`

### 代码风格

- 遵循 `golangci-lint` 默认规则，运行 `make lint-fix` 检查
- 使用 `goimports` 格式化导入（`make fmt`）
- 错误处理: 使用 `fmt.Errorf("context: %w", err)` 包装错误，保留错误链
- 导出函数必须有 GoDoc 注释

### 测试要求

- **测试驱动**: 新功能必须先写测试，覆盖核心路径
- **三层测试**:
  - 单元测试 `*_test.go`: 使用 `testify/assert`
  - 集成测试 `*_integration_test.go`: 启动真实 HTTP server
  - 属性测试: 使用 `internal/proptest/` + `gopter`，至少 100 次迭代
- 运行: `make test`（含 race 检测）

### 安全约束

- **禁止日志输出凭据**: 使用 `credential.Manager` 管理敏感信息
- **系统密钥环**: 凭据存储于 Keychain/Secret Service，不写入配置文件

## 提交前检查（强制）

**每次提交代码前必须执行：**

```bash
make fmt        # 格式化代码（含 interface{} → any 转换）
make lint-fix   # 自动修复 lint 问题
make test       # 运行全部测试（含 race 检测）
```

**一键检查脚本：**

```bash
make fmt && make lint-fix && make test
```

**PR 审查清单：**

- ✅ 代码已通过 `make lint-fix`
- ✅ 测试覆盖核心功能（单元/集成/属性测试）
- ✅ 所有测试通过 `make test`
- ✅ GoDoc 注释完整（导出函数）
- ✅ 错误处理使用 `%w` 包装

## 关键扩展点

| 场景              | 修改位置                                             |
| ----------------- | ---------------------------------------------------- |
| 新增 CLI 动词映射 | `pkg/semantic/verb.go` → `defaultPathPatternRules()` |
| 新增认证类型      | `pkg/request/builder.go` → `InjectAuth()`            |
| 自定义 MCP 工具   | `pkg/mcp/handler.go` → `BuildMCPTools()`             |
| 新增配置字段      | `pkg/config/config.go` → 相应 struct                 |

## OpenAPI 扩展字段

在 OpenAPI spec 中使用以下扩展覆盖默认语义映射:

```yaml
x-cli-resource: custom-name # 强制资源名
x-cli-verb: trigger # 强制动词（替代 HTTP method 推断）
```

## 开发命令

```bash
make build          # 构建 bin/ob
make test           # 单元+集成测试（race 检测）
make lint-fix       # 代码检查 + 自动修复
make fmt-fix        # 格式化代码（含现代化语法）
```
