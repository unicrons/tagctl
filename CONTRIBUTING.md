# Contributing to tagctl

Thank you for your interest in contributing to tagctl! This guide will help you get started.

## Provider Status

AWS is supported (106 resource types). The Kubernetes provider in
`internal/provider/k8s/` is experimental and not wired into the CLI; GCP and
Azure are not started. The status table is in
[docs/development.mdx](docs/development.mdx).

## Code of Conduct

This project follows the [Contributor Covenant 2.1](CODE_OF_CONDUCT.md). By
taking part you agree to uphold it; the Enforcement section says how to report
unacceptable behavior privately.

## Getting Started

### Prerequisites

- Go 1.26.6 or later (the version in `go.mod`)
- Git
- Make
- AWS credentials (for testing with real resources)

### Setup

```bash
# Clone the repository
git clone https://github.com/unicrons/tagctl.git
cd tagctl

# Install development tools and enable pre-commit hooks
make setup

# Run tests
make test

# Build
make build
```

`make setup` installs golangci-lint and trufflehog at the versions CI pins into
`$(go env GOPATH)/bin` and enables the pre-commit hook, which checks the staged
content for secrets, formatting, lint, build and short tests. See
[Development](docs/development.mdx) for the details.

### Dev Container

`.devcontainer/devcontainer.json` provides Go 1.26 and Node.js 22 (for the
Mintlify docs) for VS Code Dev Containers or GitHub Codespaces. It runs
`make tools` when the container is created but not `make hooks`; see
[Development](docs/development.mdx#dev-container) for why and how to enable the
hook.

### Verify Setup

```bash
./bin/tagctl version
./bin/tagctl --help

# Test with mock data
./bin/tagctl scan --mock
```

## Development Workflow

### 1. Create a Branch

```bash
git checkout -b feature/your-feature-name
# or
git checkout -b fix/issue-description
```

### 2. Make Changes

Follow the existing code style and patterns. Key guidelines:

- **Formatting**: Run `make fmt` before committing
- **Linting**: Run `make lint` and fix any issues
- **Testing**: Add tests for new functionality
- **Documentation**: Update docs if behavior changes

### 3. Test Your Changes

```bash
# Run all tests
make test

# Run tests with coverage
make coverage

# Run specific package tests
go test -v ./internal/engine/...

# Run a specific test
go test -v -run TestMockScanner_Scan ./internal/engine/
```

### 4. Commit

Commits follow [Conventional Commits](https://www.conventionalcommits.org):
`type(scope): summary`, subject line of at most 60 characters, imperative mood.
The scope is the area touched (`aws`, `cli`, `engine`, `docs`, `ci`...):

```
feat(aws): discover Lightsail instances

- List instances in the regions Lightsail supports
- Tag them through the Lightsail API, not the Tagging API

Closes #123
```

Types:
- `feat` - New feature
- `fix` - Bug fix
- `docs` - Documentation only
- `test` - Adding tests
- `refactor` - Code refactoring
- `chore` - Maintenance tasks (`chore(merge): take changes` when landing a
  contribution as a merge commit)
- `ci` - Workflow and tooling changes
- `deps` - Dependency bumps (what Dependabot uses)

Release notes are generated from these subjects, so `feat`, `fix` and `docs`
commits are what users will read. CI rejects a PR whose commits or title do
not follow the format (`scripts/check-commits.sh`).

CI runs only the jobs the diff needs. On a pull request, Lint (shellcheck on
`scripts/*.sh` and `.githooks/*`, then golangci-lint) and Test (Linux, which
also compiles every package) run when Go files change or anything the build
and tests read: `go.mod`/`go.sum`, `.golangci.yml`,
`internal/report/templates/`, `testdata/`, `permissions/`,
`docs/providers/aws.mdx`, `Makefile`, `scripts/` and `.githooks/`. Gosec scans
the packages holding the changed Go files (the whole module when only other
inputs changed), Docs runs when `docs/` changes and the secrets scan always
runs. A push to `main` only re-runs Test. A skipped job counts as passed.

### 5. Push and Create PR

```bash
git push origin feature/your-feature-name
```

Then create a Pull Request on GitHub. The PR template
(`.github/pull_request_template.md`) pre-fills the description and checklist
below.

## Project Structure

```
tagctl/
├── cmd/tagctl/           # Entry point
├── internal/
│   ├── cli/              # Commands
│   │   ├── root.go       # Global flags, banner
│   │   ├── scan.go       # Scan command → output/scan-*.json
│   │   ├── plan.go       # Plan command → output/plan-*.json
│   │   ├── apply.go      # Apply command
│   │   ├── output.go     # Table, JSON, CSV, HTML formatters
│   │   ├── evaluate.go   # Evaluate resources from JSON
│   │   ├── diff.go       # Compliance drift between two scans
│   │   ├── normalize.go  # Tag value drift detection
│   │   ├── terraform.go  # Terraform plan/state checking
│   │   ├── cost.go       # Cost attribution reporting
│   │   ├── gate.go       # CI gate flags
│   │   ├── init.go, validate.go, paths.go, progress.go
│   │   ├── auth.go       # AWS auth flags shared by scan/apply/cost
│   │   └── providers.go  # Provider initialization
│   ├── config/           # Configuration parsing and validation
│   ├── engine/           # Core logic
│   │   ├── scanner.go    # Resource discovery
│   │   ├── evaluator.go  # Policy evaluation → Findings (PASS/FAILED)
│   │   ├── planner.go    # Change planning (inference, defaults)
│   │   ├── applier.go    # Tag application
│   │   ├── differ.go     # Drift between two scans
│   │   └── normalizer.go # Tag value clustering
│   ├── report/           # SARIF, JUnit, OCSF, gate, HTML report
│   ├── terraform/        # terraform show -json parsing
│   ├── log/              # Leveled logging (error, info, debug)
│   ├── provider/         # Cloud providers
│   │   ├── provider.go   # Interface
│   │   ├── aws/          # AWS, one file per service group
│   │   └── k8s/          # Kubernetes (not wired yet)
│   └── types/            # Domain types
│       ├── resource.go, violation.go, scan.go, plan.go
│       └── diff.go, normalize.go, cost.go
├── docs/                 # Documentation (Mintlify)
├── permissions/aws/      # IAM policies and the CloudFormation role templates
├── test/
│   ├── testdata/         # Config and fixture files
│   └── testutil/         # Shared test helpers
└── output/               # Generated reports
```

See [Architecture](docs/architecture.mdx) for details.

## Data Flow

tagctl follows a file-based workflow where each command outputs to a file:

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   tagctl scan   │────▶│  tagctl plan    │────▶│  tagctl apply   │
│                 │     │                 │     │                 │
│ output/scan-*.json    │ output/plan-*.json    │ applies changes │
└─────────────────┘     └─────────────────┘     └─────────────────┘
```

- `scan` discovers resources and evaluates against policy → generates **Findings** (PASS/FAILED)
- `plan` reads scan results and generates fix suggestions → uses **inference** and **default** rules
- `apply` reads plan and applies tag changes to cloud resources

`evaluate`, `diff`, `normalize`, `terraform` and `cost` read scan files or
external input instead of the cloud; see [docs/commands.mdx](docs/commands.mdx).

## Adding Features

### Adding a New Command

1. Create `internal/cli/newcmd.go`:

```go
package cli

import "github.com/spf13/cobra"

var newCmd = &cobra.Command{
    Use:   "newcmd",
    Short: "Short description",
    Long:  `Longer description...`,
    RunE:  runNewCmd,
}

func init() {
    newCmd.Flags().String("flag", "", "flag description")
}

func runNewCmd(cmd *cobra.Command, args []string) error {
    // Implementation
    return nil
}
```

2. Register in `root.go`:

```go
func init() {
    // ...
    rootCmd.AddCommand(newCmd)
}
```

3. Add tests in `internal/cli/newcmd_test.go`

4. Document in `docs/commands.mdx`

### Adding a New Provider

We're looking for contributors to help with:
- **Kubernetes** - wire the existing `internal/provider/k8s/` provider into
  `initProviders()`
- **GCP** - Compute Engine, Cloud Storage, GKE
- **Azure** - VMs, Storage Accounts, AKS

[docs/roadmap.mdx](docs/roadmap.mdx) says what is left for each.

A new AWS service is not a new provider: follow
[Adding an AWS Service](docs/contributing/adding-an-aws-service.mdx), which
covers the lister, the tag source, write routing, the pinned tests, the IAM
policies and the docs.

1. Create `internal/provider/newcloud/provider.go`:

```go
package newcloud

import (
    "context"
    "github.com/unicrons/tagctl/internal/types"
)

type Provider struct {
    // client, config, etc.
}

func New(ctx context.Context, cfg Config) (*Provider, error) {
    // Initialize client, validate credentials
    return &Provider{}, nil
}

func (p *Provider) Name() string {
    return "newcloud"
}

func (p *Provider) ListResources(ctx context.Context) ([]types.Resource, error) {
    var resources []types.Resource
    // List resources and populate:
    // - ID, ARN (or equivalent), Name
    // - Type (e.g., "gcp_compute_instance")
    // - Region, Account/Project
    // - Tags/Labels
    return resources, nil
}

func (p *Provider) ApplyTags(ctx context.Context, resourceID string, tags map[string]string) error {
    // Apply tags to resource using cloud API
    return nil
}
```

2. Add config types in `internal/config/config.go`

3. Register provider in `internal/cli/providers.go`

4. Add comprehensive tests (use mock clients)

5. Update documentation in `docs/providers/`

### Adding a Rule Type

1. Add types in `internal/config/config.go`
2. Implement logic in `internal/engine/planner.go`
3. Add tests
4. Document in `docs/rules.mdx`

## Testing Guidelines

### Test File Naming

- `foo.go` → `foo_test.go`
- Same package for unit tests
- `_test` package suffix for black-box tests

### Test Structure

```go
func TestFunctionName(t *testing.T) {
    tests := []struct {
        name string
        // inputs
        // expected outputs
    }{
        {
            name: "descriptive case name",
            // ...
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test logic
        })
    }
}
```

### Coverage

`make coverage` writes `coverage.html`. New code comes with tests that mock the
cloud SDK; nothing in the suite may reach real AWS.

### Key Types to Understand

```go
// Finding represents a compliance check (PASS or FAILED)
type Finding struct {
    Resource Resource      `json:"resource"`
    Tag      string        `json:"tag"`
    Status   FindingStatus `json:"status"`   // "PASS" or "FAILED"
    Reason   ViolationReason `json:"reason"` // "compliant", "missing", "invalid_value", "invalid_format"
    Expected string        `json:"expected,omitempty"`
    Actual   string        `json:"actual,omitempty"`
}

// Violation is deprecated, use Finding instead
type Violation struct {
    Resource Resource        `json:"resource"`
    Tag      string          `json:"tag"`
    Status   FindingStatus   `json:"status"` // Always "FAILED"
    Reason   ViolationReason `json:"reason"`
}
```

## Pull Request Guidelines

### PR Title

Use the same prefixes as commits:

```
feat(azure): add Azure provider support
fix(engine): correct tag inheritance for EBS volumes
docs(configuration): improve configuration examples
```

### PR Description

Include:
- What changes were made
- Why the changes were needed
- How to test the changes
- Related issues (e.g., "Closes #123")

### PR Checklist

- [ ] Tests pass (`make test`)
- [ ] Code is formatted (`make fmt`)
- [ ] Linter passes (`make lint`)
- [ ] Documentation updated (if needed)
- [ ] Commit messages are clear

## Code Style

### Go Style

Follow standard Go conventions:

- `gofmt` formatting
- Effective Go guidelines
- Go Code Review Comments

### Naming

- Interfaces: `Scanner`, `Planner`, `Applier` (noun)
- Implementations: `MockScanner`, `MockPlanner`, `MockApplier`
- Constructors: `NewScanner()`, `NewMockScanner()`

### Error Handling

```go
// Good: wrap errors with context
if err != nil {
    return fmt.Errorf("failed to scan resources: %w", err)
}

// Bad: lose context
if err != nil {
    return err
}
```

### Comments

```go
// Good: explain why, not what
// Security Hub creates hundreds of Config rules per account; they are not
// the user's to tag.

// Bad: explain what (obvious from code)
// Loop through resources
for _, r := range resources {
```

## Getting Help

[Open an issue](https://github.com/unicrons/tagctl/issues/new/choose) with the
form that fits; blank issues are disabled:

- **Questions**: the Question form, after checking the [docs](docs/)
- **Bugs**: the Bug report form, with account ids, ARNs and credentials redacted
- **Features**: the Feature request form
- **Security vulnerabilities**: never a public issue; follow
  [SECURITY.md](SECURITY.md)

## Releases

A release is a tag. Pushing `vX.Y.Z` runs the `Release` workflow, which
builds the binaries with GoReleaser (`.goreleaser.yaml`), attaches them with
their checksums to a GitHub Release and writes the notes from the commit
subjects since the previous tag.

```bash
git tag -a v1.0.0 -m "v1.0.0"
git push origin v1.0.0
```

## Recognition

Contributors are recognized in:
- Release notes
- README.md (for significant contributions)

Thank you for contributing! 🎉
