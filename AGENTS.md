# Datadog Agent - Project Overview for AI coding assistant

## Project Summary
The Datadog Agent is a comprehensive monitoring and observability agent written primarily in Go. It collects metrics, traces, logs, and security events from systems and applications, forwarding them to the Datadog platform. This is the main repository for Agent versions 6 and 7.

## Project Structure

### Core Directories
- `/cmd/` - Entry points for various agent components
  - `agent/` - Main agent binary
  - `cluster-agent/` - Kubernetes cluster agent
  - `dogstatsd/` - StatsD metrics daemon
  - `trace-agent/` - APM trace collection agent
  - `system-probe/` - System-level monitoring (eBPF)
  - `security-agent/` - Security monitoring
  - `process-agent/` - Process monitoring
  - `privateactionrunner/` - Executing actions

- `/pkg/` - Core Go packages and libraries
  - `aggregator/` - Metrics aggregation
  - `collector/` - Check scheduling and execution
  - `config/` - Configuration management
  - `logs/` - Log collection and processing
  - `metrics/` - Metrics types and handling
  - `network/` - Network monitoring
  - `security/` - Security monitoring components
  - `trace/` - APM tracing components

- `/comp/` - Component-based architecture modules
  - `core/` - Core components
  - `metadata/` - Metadata collection
  - `logs/` - Log components
  - `trace/` - Trace components

- `/tasks/` - Python invoke tasks for development
  - Build, test, lint, and deployment automation

- `/rtloader/` - Runtime loader for Python checks

## Development Workflow

> **IMPORTANT**: Always use `dda inv` commands to build and test the agent. Never invoke `go build`, `go test`, or `go run` directly — the `dda inv` wrappers set the correct build tags, environment variables, and module context.

### Common Commands

#### Building

```bash
# install dda on mac OS
brew install --cask dda

# Install development tools
dda inv install-tools

# Build the main agent
dda inv agent.build --build-exclude=systemd

# Build specific components
dda inv dogstatsd.build
dda inv trace-agent.build
dda inv system-probe.build
```

#### Testing
```bash
# Run all tests
dda inv test

# Test specific package
dda inv test --targets=./pkg/aggregator

# Test specific package excluding a test
dda inv test --targets=./pkg/aggregator -e TestFoo

# Run Go linters
dda inv linter.go

# Run all linters
dda inv linter.all
```

#### Running on Linux (dev container)

Some packages require Linux (e.g. `//go:build linux` files). Use the dev container:

```bash
# Run any inv command inside the Linux dev container
dda env dev run -- dda inv -- test --targets=./pkg/some/linux/package
dda env dev run -- dda inv -- linter.go --targets=./pkg/some/linux/package
```

#### Running Locally
```bash
# Create dev config with testing API key
echo "api_key: 0000001" > dev/dist/datadog.yaml

# Run the agent
./bin/agent/agent run -c bin/agent/dist/datadog.yaml
```

### Development Configuration
The development configuration file should be placed at `dev/dist/datadog.yaml`. After building, it gets copied to `bin/agent/dist/datadog.yaml`.

## Key Components

### Check System
- Checks are Python or Go modules that collect metrics
- Located in `cmd/agent/dist/checks/`
- Can be autodiscovered via Kubernetes annotations/labels

### Configuration
- Main config: `datadog.yaml`
- Check configs: `conf.d/<check_name>.d/conf.yaml`
- Supports environment variable overrides with `DD_` prefix

### eBPF-based System Checks
- Checks using eBPF probes require system-probe module running
- Examples: tcp_queue_length, oom_kill, seccomp_tracer
- Module code (system-probe): `pkg/collector/corechecks/ebpf/probe/<check>/`
- Check code (agent): `pkg/collector/corechecks/ebpf/<check>/`
- System-probe modules: `cmd/system-probe/modules/`
- Configuration: Set `<check_name>.enabled: true` in system-probe config
- See `pkg/collector/corechecks/ebpf/AGENTS.md` for detailed structure
- Quick reference: `.cursor/rules/system_probe_modules.mdc` for common patterns and pitfalls

## Testing Strategy

### Unit Tests
- Go tests using standard `go test`
- Python tests using pytest
- Run with `dda inv test --targets=<package>`

### End-to-End Tests
- E2E framework in `test/new-e2e/`

### Linting
- Go: golangci-lint via `dda inv linter.go`
- Python: various linters via `dda inv linter.python`
- YAML: yamllint
- Shell: shellcheck

## Build System

### Invoke Tasks
The project uses Python's Invoke framework with custom tasks. Main task categories:
- `agent.*` - Core agent tasks
- `test` - Testing tasks
- `linter.*` - Linting tasks
- `docker.*` - Docker image tasks
- `release.*` - Release management

### Build Tags
Go build tags control feature inclusion, some examples are:
- `kubeapiserver` - Kubernetes API server support
- `containerd` - containerd support
- `docker` - Docker support
- `ebpf` - eBPF support
- `python` - Python check support
- and MANY more, refer to ./tasks/build_tags.py for a full reference.

## Important Files

### Configuration
- `datadog.yaml` - Main agent configuration
- `modules.yml` - Go module definitions
- `release.json` - Release version information
- `.gitlab-ci.yml` - CI/CD pipeline configuration

### Documentation
- `/docs/` - Internal documentation
- `/docs/dev/` - Developer guides
- `README.md` - Project overview
- `CONTRIBUTING.md` - Contribution guidelines

## CI/CD Pipeline

### GitLab CI
- Primary CI system
- Defined in `.gitlab-ci.yml` and `.gitlab/` directory
- Runs tests, builds, and deployments

### GitHub Actions
- Secondary CI for specific workflows
- Tests about the pull-request settings or repository configuration
- Release automation workflows

## Security Considerations

### Sensitive Data
- Never commit API keys or secrets
- Use secret backend for credentials

## Module System
The project uses Go modules with multiple sub-modules.
TODO: Describe specific strategies for managing modules, including any invoke
tasks.

## Platform Support
- **Linux**: Full support (amd64, arm64)
- **Windows**: Full support (Server 2016+, Windows 10+)
- **macOS**: Supported
- **AIX**: No support in this codebase
- **Container**: Docker, Kubernetes, ECS, containerd, and more

## Best Practices

1. **Always run linters before committing**: `dda inv linter.go`
2. **Always test your changes**: `dda inv test --targets=<your_package>`
3. **Follow Go conventions**: Use gofmt, follow project structure
4. **Update documentation**: Keep docs in sync with code changes
6. **Check for security implications**: Review security-sensitive changes carefully

## Troubleshooting Development Issues

### Common Build Issues
- **Missing tools**: Run `dda inv install-tools`
- **CMake errors**: Remove `dda inv rtloader.clean`

### Testing Issues
- **Flaky tests**: Check `flakes.yaml` for known issues
- **Coverage issues**: Use `--coverage` flag

<!-- BEGIN BEADS INTEGRATION -->
## Issue Tracking with bd (beads)

**IMPORTANT**: This project uses **bd (beads)** for ALL issue tracking. Do NOT use markdown TODOs, task lists, or other tracking methods.

### Why bd?

- Dependency-aware: Track blockers and relationships between issues
- Git-friendly: Dolt-powered version control with native sync
- Agent-optimized: JSON output, ready work detection, discovered-from links
- Prevents duplicate tracking systems and confusion

### Quick Start

**Check for ready work:**

```bash
bd ready --json
```

**Create new issues:**

```bash
bd create "Issue title" --description="Detailed context" -t bug|feature|task -p 0-4 --json
bd create "Issue title" --description="What this issue is about" -p 1 --deps discovered-from:bd-123 --json
```

**Claim and update:**

```bash
bd update <id> --claim --json
bd update bd-42 --priority 1 --json
```

**Complete work:**

```bash
bd close bd-42 --reason "Completed" --json
```

### Issue Types

- `bug` - Something broken
- `feature` - New functionality
- `task` - Work item (tests, docs, refactoring)
- `epic` - Large feature with subtasks
- `chore` - Maintenance (dependencies, tooling)

### Priorities

- `0` - Critical (security, data loss, broken builds)
- `1` - High (major features, important bugs)
- `2` - Medium (default, nice-to-have)
- `3` - Low (polish, optimization)
- `4` - Backlog (future ideas)

### Workflow for AI Agents

1. **Check ready work**: `bd ready` shows unblocked issues
2. **Claim your task atomically**: `bd update <id> --claim`
3. **Work on it**: Implement, test, document
4. **Discover new work?** Create linked issue:
   - `bd create "Found bug" --description="Details about what was found" -p 1 --deps discovered-from:<parent-id>`
5. **Complete**: `bd close <id> --reason "Done"`

### Auto-Sync

bd automatically syncs via Dolt:

- Each write auto-commits to Dolt history
- Use `bd dolt push`/`bd dolt pull` for remote sync
- No manual export/import needed!

### Important Rules

- ✅ Use bd for ALL task tracking
- ✅ Always use `--json` flag for programmatic use
- ✅ Link discovered work with `discovered-from` dependencies
- ✅ Check `bd ready` before asking "what should I work on?"
- ❌ Do NOT create markdown TODO lists
- ❌ Do NOT use external issue trackers
- ❌ Do NOT duplicate tracking systems

For more details, see README.md and docs/QUICKSTART.md.

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

<!-- END BEADS INTEGRATION -->
