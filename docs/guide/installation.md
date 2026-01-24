# Installation

OpenBridge can be installed as a standalone binary or from source.

## From Source

If you have Go installed (1.25 or later):

```bash
go install github.com/nomagicln/open-bridge/cmd/ob@latest
```

## From Binary

Download the latest release for your platform from the [Releases page](https://github.com/nomagicln/open-bridge/releases).

## Shell Auto-Completion

OpenBridge supports shell auto-completion for bash, zsh, and fish shells. This provides suggestions for commands, resources, flags, and enum values.

### Bash

```bash
# Load in current session
source <(ob completion bash)

# Install permanently (Linux)
ob completion bash | sudo tee /etc/bash_completion.d/ob

# Install permanently (macOS with Homebrew)
ob completion bash > /usr/local/etc/bash_completion.d/ob
```

### Zsh

```bash
# Load in current session
source <(ob completion zsh)

# Install permanently
ob completion zsh > "${fpath[1]}/_ob"
```

### Fish

```bash
# Load in current session
ob completion fish | source

# Install permanently
ob completion fish > ~/.config/fish/completions/ob.fish
```
