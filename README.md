# Double Agent

A resilient SSH agent proxy that automatically discovers and connects to active SSH agents on your system. Never lose your SSH agent connection again when switching between terminals, tmux sessions, or SSH connections.

## Features

- **Automatic Agent Discovery**: Finds and connects to active SSH agents on your system
- **Transparent Failover**: Automatically switches to a new agent when the current one becomes unavailable
- **Zero Configuration**: Works out of the box with sensible defaults
- **Fast**: < 1ms latency overhead with intelligent caching
- **Secure**: Sanitizes logs to prevent leaking sensitive information
- **Production Ready**: Comprehensive test coverage and benchmarks

## Installation

### Using Nix (Recommended)

Double Agent is packaged as a Nix flake with Home Manager and NixOS modules.

#### Home Manager

Add to your `flake.nix`:

```nix
{
  inputs = {
    double-agent.url = "github:phinze/double-agent";
  };

  outputs = { self, nixpkgs, home-manager, double-agent, ... }: {
    homeConfigurations.youruser = home-manager.lib.homeManagerConfiguration {
      modules = [
        double-agent.homeManagerModules.default
        {
          services.double-agent = {
            enable = true;
            socketPath = "$HOME/.ssh/agent";  # default
            autoStart = true;                  # default
            shellIntegration = {
              bash = true;  # default
              zsh = true;   # default
              fish = true;  # default
            };
          };
        }
      ];
    };
  };
}
```

#### NixOS System Module

```nix
{
  imports = [ double-agent.nixosModules.default ];
  
  services.double-agent = {
    enable = true;
    # Configure per-user as needed
  };
}
```

#### Direct Package Installation

```bash
nix profile install github:phinze/double-agent
```

### Building from Source

```bash
git clone https://github.com/phinze/double-agent.git
cd double-agent
go build -o double-agent
```

## Usage

### Basic Usage

Start the proxy in the foreground:

```bash
double-agent ~/.ssh/agent
```

Start as a daemon:

```bash
double-agent -d ~/.ssh/agent
```

### Shell Configuration

Export the proxy socket path in your shell:

```bash
export SSH_AUTH_SOCK="$HOME/.ssh/agent"
```

Or add to your shell configuration:

#### Bash/Zsh

```bash
# ~/.bashrc or ~/.zshrc
export DOUBLE_AGENT_SOCKET="$HOME/.ssh/agent"
if [ -S "$DOUBLE_AGENT_SOCKET" ]; then
  export SSH_AUTH_SOCK="$DOUBLE_AGENT_SOCKET"
fi
```

#### Fish

```fish
# ~/.config/fish/config.fish
set -gx DOUBLE_AGENT_SOCKET "$HOME/.ssh/agent"
if test -S "$DOUBLE_AGENT_SOCKET"
  set -gx SSH_AUTH_SOCK "$DOUBLE_AGENT_SOCKET"
end
```

### Command Line Options

```
double-agent [options] <proxy-socket-path>

Options:
  -v, --verbose        Enable verbose logging
  -d, --daemon         Run as daemon (detach from terminal)
  --test-discovery     Test socket discovery and exit
  --health             Check if proxy is healthy and exit
  --version            Show version and exit
  -h, --help           Show help message
```

### Testing and Diagnostics

Test socket discovery to see available SSH agents:

```bash
double-agent --test-discovery
```

Check if the proxy is healthy:

```bash
double-agent --health ~/.ssh/agent
```

## How It Works

1. **Discovery**: Double Agent scans `/tmp/ssh-*/agent.*` for SSH agent sockets owned by the current user
2. **Validation**: Each socket is tested by sending an SSH agent protocol message
3. **Proxying**: Client connections are transparently forwarded to the active agent
4. **Caching**: The active socket is cached for 5 seconds to minimize discovery overhead
5. **Failover**: If the cached socket fails, a new discovery is triggered automatically

## Architecture

```
┌─────────┐     ┌──────────────┐     ┌───────────┐
│ SSH     │────▶│ Double Agent │────▶│ SSH Agent │
│ Client  │     │    Proxy     │     │  Socket   │
└─────────┘     └──────────────┘     └───────────┘
                       │
                       ▼
                 ┌──────────┐
                 │ Discovery│
                 │  & Cache │
                 └──────────┘
```

## Development

### Running Tests

```bash
# Unit tests
go test ./...

# Integration tests
go test -tags=integration ./...

# Benchmarks
go test -bench=. ./...

# Test coverage
go test -cover ./...
```

### Project Structure

```
double-agent/
├── main.go                 # CLI entry point
├── proxy/
│   ├── proxy.go           # Core proxy logic
│   ├── discovery.go       # Socket discovery
│   ├── protocol.go        # SSH agent protocol constants
│   ├── health.go          # Health check implementation
│   └── sanitizer.go       # Log sanitization
├── nix/
│   ├── package.nix        # Nix package definition
│   ├── home-manager.nix   # Home Manager module
│   └── nixos.nix          # NixOS module
└── flake.nix              # Nix flake
```

## Performance

Double Agent is designed for minimal overhead:

- **Latency**: < 1ms overhead (typically 100-400μs)
- **Memory**: ~70KB per connection
- **Caching**: 5-second cache reduces discovery overhead by 99%+
- **Throughput**: Handles 100+ concurrent connections

## Security

- Only connects to sockets owned by the current user
- Sanitizes logs to prevent leaking usernames and SSH fingerprints
- No modification or inspection of SSH agent protocol data
- Transparent proxy - no keys or secrets are stored

## Troubleshooting

### No Active SSH Agent Found

```bash
# Check if you have an SSH agent running
echo $SSH_AUTH_SOCK
ssh-add -l

# Start a new SSH agent if needed
eval $(ssh-agent)
ssh-add ~/.ssh/id_rsa
```

### Proxy Not Starting

```bash
# Check if the socket path exists and has correct permissions
ls -la ~/.ssh/

# Remove stale socket if it exists
rm -f ~/.ssh/agent

# Start with verbose logging
double-agent -v ~/.ssh/agent
```

### Connection Issues

```bash
# Test discovery to see available agents
double-agent --test-discovery

# Check proxy health
double-agent --health ~/.ssh/agent

# Enable verbose logging for debugging
double-agent -v ~/.ssh/agent
```

## Contributing

Contributions are welcome! Please feel free to submit issues and pull requests.

### Development Setup

1. Clone the repository
2. Install Go 1.21 or later
3. Run tests: `go test ./...`
4. Run linter: `golangci-lint run`

## License

Apache License 2.0 - See [LICENSE](LICENSE) file for details.

## Acknowledgments

Double Agent solves the common problem of SSH agent sockets becoming stale when switching between terminal sessions, especially in tmux environments or when using SSH agent forwarding.

## Related Projects

- [ssh-agent](https://www.openssh.com/) - The standard OpenSSH authentication agent
- [gpg-agent](https://gnupg.org/) - GnuPG's agent with SSH support
- [keychain](https://www.funtoo.org/Keychain) - Manages SSH and GPG agents