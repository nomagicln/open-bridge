# Release Configuration

## ✅ Multi-Platform Support

### Supported Platforms and Architectures

| OS      | amd64 | arm64 |
|---------|-------|-------|
| Linux   | ✅    | ✅    |
| macOS   | ✅    | ✅    |
| Windows | ✅    | ❌    |

**Note**: Windows ARM64 is not currently supported due to limited ecosystem maturity.

### Build Artifacts

The following artifacts are automatically generated on release:

- `ob_vX.Y.Z_Linux_x86_64.tar.gz`
- `ob_vX.Y.Z_Linux_arm64.tar.gz`
- `ob_vX.Y.Z_Darwin_x86_64.tar.gz` (Intel Mac)
- `ob_vX.Y.Z_Darwin_arm64.tar.gz` (Apple Silicon)
- `ob_vX.Y.Z_Windows_x86_64.zip`
- `checksums.txt` (SHA256 checksums)

### Automated Changelog

GoReleaser automatically categorizes commits based on their prefixes:

- `feat:` → New Features
- `fix:` → Bug Fixes
- `perf:` → Performance Improvements
- `refactor:` → Refactors
- `build(deps):` → Dependencies

## 🚀 How to Release

### 1. Create a Tag

```bash
# Create a version tag
git tag -a v0.1.0 -m "Release v0.1.0"

# Push the tag (triggers release workflow)
git push origin v0.1.0
```

### 2. GitHub Actions Workflow

The workflow automatically:

- Runs the full test suite (across 3 platforms)
- Runs lint checks
- Builds binaries for all platforms
- Generates the changelog
- Creates the GitHub Release

### 3. Local Testing (Optional)

```bash
# Install GoReleaser
brew install goreleaser  # macOS
# or
go install github.com/goreleaser/goreleaser/v2@latest

# Test locally (dry run, nothing is published)
goreleaser release --snapshot --clean
```

## 📦 Future Extensions (Pre-configured)

### Homebrew Tap

Uncomment the `brews` section in `.goreleaser.yml`. Prerequisites:

1. Create a `nomagicln/homebrew-tap` repository
2. Add `HOMEBREW_TAP_TOKEN` to GitHub Secrets

Installation:

```bash
brew tap nomagicln/tap
brew install ob
```

### Snapcraft (Linux)

Uncomment the `snapcrafts` section. Users can then install via:

```bash
snap install ob
```

## 🔍 Validate Configuration

```bash
# Validate .goreleaser.yml syntax
goreleaser check

# Build a snapshot (without publishing)
goreleaser build --snapshot --clean
```

## 📝 CI/CD Pipeline

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

## ⚙️ Key Features

1. **Zero-Downtime Updates**: Each release fully replaces the previous version
2. **Secure Builds**: CGO_ENABLED=0, statically linked with no external dependencies
3. **Version Metadata**: Automatically injects version/commit/date
4. **Checksums**: SHA256 checksums generated for all artifacts
5. **Auto-Archiving**: Includes LICENSE, README, and QUICKSTART

## 🐛 FAQ

### Q: How do I roll back a release?

A: Delete the tag and release from GitHub Releases, then re-tag.

### Q: Are pre-release versions supported?

A: Yes. Tags like `v0.1.0-beta.1` are automatically marked as prerelease.

### Q: How do I customize the changelog?

A: Manually edit the GitHub Release description. GoReleaser generates an initial version.
