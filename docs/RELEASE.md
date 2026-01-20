# Release 配置说明

## ✅ 已完成的多平台支持

### 支持的平台和架构

| OS      | amd64 | arm64 |
|---------|-------|-------|
| Linux   | ✅    | ✅    |
| macOS   | ✅    | ✅    |
| Windows | ✅    | ❌    |

**注意**: Windows ARM64 暂时不支持，因为生态成熟度较低。

### 构建产物

Release 时会自动生成：

- `ob_vX.Y.Z_Linux_x86_64.tar.gz`
- `ob_vX.Y.Z_Linux_arm64.tar.gz`
- `ob_vX.Y.Z_Darwin_x86_64.tar.gz` (Intel Mac)
- `ob_vX.Y.Z_Darwin_arm64.tar.gz` (Apple Silicon)
- `ob_vX.Y.Z_Windows_x86_64.zip`
- `checksums.txt` (SHA256 校验和)

### 自动化 Changelog

GoReleaser 会根据 commit message 自动分类：

- `feat:` → New Features
- `fix:` → Bug Fixes
- `perf:` → Performance Improvements
- `refactor:` → Refactors
- `build(deps):` → Dependencies

## 🚀 如何发布

### 1. 打标签

```bash
# 创建版本标签
git tag -a v0.1.0 -m "Release v0.1.0"

# 推送标签（触发 release workflow）
git push origin v0.1.0
```

### 2. GitHub Actions 自动执行

- 运行全部测试（3个平台）
- 运行 lint 检查
- 构建所有平台二进制
- 生成 changelog
- 创建 GitHub Release

### 3. 本地测试（可选）

```bash
# 安装 GoReleaser
brew install goreleaser  # macOS
# 或
go install github.com/goreleaser/goreleaser/v2@latest

# 本地测试（不会推送）
goreleaser release --snapshot --clean
```

## 📦 未来扩展（已预留配置）

### Homebrew Tap

取消 `.goreleaser.yml` 中 `brews` 部分的注释，需要：

1. 创建 `nomagicln/homebrew-tap` 仓库
2. 添加 `HOMEBREW_TAP_TOKEN` 到 GitHub Secrets

安装方式：

```bash
brew tap nomagicln/tap
brew install ob
```

### Snapcraft (Linux)

取消 `snapcrafts` 部分注释，用户可通过：

```bash
snap install ob
```

## 🔍 验证配置

```bash
# 验证 .goreleaser.yml 语法
goreleaser check

# 构建当前快照（不发布）
goreleaser build --snapshot --clean
```

## 📝 CI/CD 流程

```
Push Tag v0.1.0
    ↓
GitHub Actions (ci.yml)
    ↓
1. Test (ubuntu/macos/windows) ✓
    ↓
2. Lint (ubuntu) ✓
    ↓
3. Build (artifact) ✓
    ↓
4. Release (GoReleaser) ✓
   ├─ Build: linux/darwin/windows (amd64/arm64)
   ├─ Package: tar.gz / zip
   ├─ Checksum: SHA256
   ├─ Changelog: Auto-generate
   └─ Upload: GitHub Releases
```

## ⚙️ 关键特性

1. **零停机更新**: 每次 release 完全替换前一版本
2. **安全构建**: CGO_ENABLED=0，静态链接无依赖
3. **版本信息**: 自动注入 version/commit/date
4. **校验和**: 所有文件自动生成 SHA256 校验
5. **自动归档**: 包含 LICENSE、README、QUICKSTART

## 🐛 常见问题

### Q: 如何回滚发布？

A: GitHub Releases 可以删除 tag 和 release，然后重新打标签。

### Q: 支持预发布版本吗？

A: 支持，标签格式如 `v0.1.0-beta.1` 会自动标记为 prerelease。

### Q: 如何自定义 changelog？

A: 手动编辑 GitHub Release 描述，GoReleaser 生成的是初始版本。
