# 安装

OpenBridge 可以作为独立二进制文件安装，也可以从源码编译安装。

## 从源码安装

如果您已安装 Go (1.25 或更高版本)：

```bash
go install github.com/nomagicln/open-bridge/cmd/ob@latest
```

## 从二进制文件安装

请从 [Releases 页面](https://github.com/nomagicln/open-bridge/releases) 下载适用于您平台的最新版本。

## Shell 自动补全

OpenBridge 支持 bash, zsh 和 fish 的 Shell 自动补全。它能为命令、资源、参数和枚举值提供建议。

### Bash

```bash
# 在当前会话加载
source <(ob completion bash)

# 永久安装 (Linux)
ob completion bash | sudo tee /etc/bash_completion.d/ob

# 永久安装 (macOS with Homebrew)
ob completion bash > /usr/local/etc/bash_completion.d/ob
```

### Zsh

```bash
# 在当前会话加载
source <(ob completion zsh)

# 永久安装
ob completion zsh > "${fpath[1]}/_ob"
```

### Fish

```bash
# 在当前会话加载
ob completion fish | source

# 永久安装
ob completion fish > ~/.config/fish/completions/ob.fish
```
