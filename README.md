<p align="center">
  <pre>
  ████████╗ █████╗  ██████╗  ██████╗████████╗██╗
  ╚══██╔══╝██╔══██╗██╔════╝ ██╔════╝╚══██╔══╝██║
     ██║   ███████║██║  ███╗██║        ██║   ██║
     ██║   ██╔══██║██║   ██║██║        ██║   ██║
     ██║   ██║  ██║╚██████╔╝╚██████╗   ██║   ███████╗
     ╚═╝   ╚═╝  ╚═╝ ╚═════╝  ╚═════╝   ╚═╝   ╚══════╝
  </pre>
</p>

<p align="center">
  <strong>Audit, fix, and enforce cloud resource tags across AWS (GCP and Azure coming soon).</strong>
</p>

<p align="center">
  <a href="https://github.com/unicrons/tagctl/actions/workflows/ci.yml"><img src="https://github.com/unicrons/tagctl/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://goreportcard.com/report/github.com/unicrons/tagctl"><img src="https://goreportcard.com/badge/github.com/unicrons/tagctl" alt="Go Report Card"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="License"></a>
  <a href="https://github.com/unicrons/tagctl/releases"><img src="https://img.shields.io/github/v/release/unicrons/tagctl" alt="Release"></a>
</p>

---

## Why tagctl?

Cloud resources without proper tags are a **FinOps nightmare**. You can't allocate costs, assign ownership, or enforce compliance. Existing tools are either too complex (Cloud Custodian), too limited (AWS Tag Policies), or too expensive (AWS Config).

**tagctl** fills the gap with a simple, focused approach:

```bash
$ tagctl scan    # What's wrong?        → output/scan-TIMESTAMP.json
$ tagctl plan    # How to fix it?       → output/plan-TIMESTAMP.json (uses latest scan)
$ tagctl apply   # Fix it.              → applies changes (uses latest plan)
```

## Key Features

| Feature | Description |
|---------|-------------|
| **AWS Support** | 106 resource types across the same AWS services Prowler audits — from EC2 and S3 to CloudTrail, GuardDuty, WAF, Bedrock, SageMaker, CodePipeline, WorkSpaces and IAM roles (see [docs/providers/aws.mdx](docs/providers/aws.mdx); GCP, Azure coming soon) |
| **Global discovery** | Scans ALL resources across ALL regions in your account by default |
| **Smart inference** | Automatically derive tags from resource names (`web-prod-api` → `environment: prod`) |
| **Defaults** | Fill missing tags with a default value when a condition holds (`tag:owner: absent`) |
| **Safe by default** | Always preview changes with `plan` before `apply` |
| **GitOps ready** | Store config in git, run in CI/CD pipelines |
| **Beautiful reports** | Clear compliance dashboards for management |
| **Drift tracking** | `diff` reports what got worse since a baseline, so CI gates on the change |
| **Value normalization** | Collapses `prod` / `Production` / `PROD`, the drift that silently splits cost reports |
| **Shift left** | `terraform` checks a plan before anything is created, honouring `default_tags` |
| **Cost attribution** | `cost` reports the spend nobody can be billed for, in currency |
| **CI native** | SARIF for GitHub code scanning, JUnit for any CI, with compliance gates |
| **OCSF** | Every finding as an OCSF 1.4 Compliance Finding event, ready for Security Lake or any OCSF-native SIEM |

## Quick Start

```bash
# Install
go install github.com/unicrons/tagctl/cmd/tagctl@latest

# Initialize configuration
tagctl init

# Edit your policy
vim tagctl.yaml

# See what's wrong
tagctl scan

# Generate fix plan
tagctl plan

# Apply fixes
tagctl apply
```

## Example Output

### Scan Report

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
                  Tag Compliance Report
                  2026-02-02 10:51:48
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Overall: 65% compliant █████████████░░░░░░░ (98/150 resources)

By Account:
  ACCOUNT         TOTAL  COMPLIANT  COMPLIANCE
  aws/production  100    85         85% ████████░░
  aws/staging     50     13         26% ██░░░░░░░░

By Required Tag:
  TAG          STATUS  PRESENT  MISSING  INVALID  COMPLIANCE
  environment  PASS    140      10       0        93% █████████░
  cost-center  FAILED  98       52       5        62% ██████░░░░
  owner        FAILED  75       75       10       43% ████░░░░░░

Run 'tagctl plan' to see suggested fixes.

Detailed results saved to:
  • JSON: /path/to/output/scan-20260202-105148.json
  • CSV:  /path/to/output/scan-20260202-105148.csv
  • HTML: /path/to/output/scan-20260202-105148.html
```

### Plan Output

```
Planned changes:

aws_instance (web-prod-api-1)
  + environment:  "prod"     (inferred: name contains '-prod-')

aws_s3_bucket (legacy-data-2019)
  + owner:        "platform-team@company.com"  (default)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Summary: 3 resources will be modified
         5 tags will be added

Run 'tagctl apply' to execute this plan.
```

## Configuration

Create `tagctl.yaml` in your project root:

```yaml
# Cloud accounts to scan
clouds:
  aws:
    - profile: production
      # regions: [us-east-1, eu-west-1]  # Optional: limit to specific regions
      # By default, ALL resources across ALL regions are discovered
    - profile: staging
      regions: [us-east-1]  # Optionally limit regions (also filters S3 buckets)

# Tag policy - what tags are required?
policy:
  required:
    - name: environment
      values: [dev, staging, prod]
    - name: cost-center
      pattern: "^[A-Z]{2,4}-\\d{3,6}$"
    - name: owner
      pattern: "^.+@company\\.com$"

# Auto-fix rules
rules:
  # Infer tags from resource names
  infer:
    - tag: environment
      from_name:
        - pattern: "-prod-"
          value: prod
        - pattern: "-staging-|-stg-"
          value: staging

  # Default values when tag is missing
  defaults:
    - resource: "*"
      when:
        tag:owner: absent
      set:
        owner: platform-team@company.com

# Skip certain resources
ignore:
  resources:
    - "aws_cloudwatch_*"
    - "aws_iam_*"
```

## Commands

| Command | Description |
|---------|-------------|
| `tagctl init` | Generate a configuration template |
| `tagctl validate` | Validate your configuration file |
| `tagctl scan` | Audit resources and report compliance → generates `scan-*.json` |
| `tagctl plan` | Generate a fix plan from scan results → generates `plan-*.json` |
| `tagctl apply` | Apply the planned changes from plan file |
| `tagctl evaluate` | Evaluate resources from external JSON (for Prowler integration) |
| `tagctl diff` | Compare two scans and report compliance drift |
| `tagctl normalize` | Find tag values that are variants of one another (`prod` / `Production` / `PROD`) |
| `tagctl terraform` | Check a Terraform plan or state against the policy, before apply |
| `tagctl cost` | Report how much spend your tags fail to account for |
| `tagctl version` | Show version information |

### Workflow

Each command passes data to the next via JSON files in the `output/` directory:

```bash
# 1. Scan resources and save results
tagctl scan                              # → output/scan-20260202-143052.json

# 2. Generate plan from scan results
tagctl plan                              # Uses latest scan automatically
tagctl plan --scan output/scan-*.json    # Or specify a scan file

# 3. Apply changes from plan
tagctl apply                             # Uses latest plan automatically
tagctl apply --plan output/plan-*.json   # Or specify a plan file
```

### Global Flags

```
-c, --config string      Config file path (default: ./tagctl.yaml)
-o, --output string      Output format: table, json, csv (default: table)
-l, --log-level string   Log level: error, info, debug (default: error)
-h, --help               Show help
```

### Scan Flags

```
--region strings   AWS region(s) to scan (default: all available regions)
--profile, -p      AWS profile (default: SDK credential chain)
--role string      IAM role ARN to assume (--external-id, --session-duration, --mfa-serial)
--verbose          Show all violations
--mock             Use mock data for demonstration
```

## Output Files

Every scan automatically generates reports in the `output/` directory:

```
output/
├── scan-20260202-105148.json   # Full results in JSON format
├── scan-20260202-105148.csv    # Violations in CSV format
└── scan-20260202-105148.html   # Visual HTML report
```

| Format | Description |
|--------|-------------|
| **JSON** | Complete scan results with all metadata, suitable for CI/CD pipelines |
| **CSV** | Violations only, importable into Excel/Google Sheets |
| **HTML** | Standalone visual report: per-tag coverage, a resource-type × tag matrix and filterable findings. Opens offline, shareable with management |

## Documentation

| Document | Description |
|----------|-------------|
| [Getting Started](docs/getting-started.mdx) | Installation and first steps |
| [Configuration](docs/configuration.mdx) | Complete configuration reference |
| [Commands](docs/commands.mdx) | Detailed command documentation |
| [Rules](docs/rules.mdx) | Inference and defaults |
| [Architecture](docs/architecture.mdx) | How tagctl works internally |

## Installation

### From Source

```bash
go install github.com/unicrons/tagctl/cmd/tagctl@latest
```

### Build from Repository

```bash
git clone https://github.com/unicrons/tagctl.git
cd tagctl
make build
./bin/tagctl --help
```

### Releases

Pre-built binaries for Linux, macOS and Windows (amd64 and arm64) are attached
to every [GitHub Release](https://github.com/unicrons/tagctl/releases), with a
`checksums.txt` to verify them.

## Development

```bash
# Run tests
make test

# Run tests with coverage
make coverage

# Build binary
make build

# Format code
make fmt

# Run linter
make lint

# Run all checks
make all
```

## Roadmap

- [x] Core CLI structure
- [x] Mock scanner/planner/applier
- [x] AWS provider implementation (106 resource types, bulk tag reads through the Resource Groups Tagging API)
- [x] Real tag scanning
- [x] Real tag application
- [x] Multi-region auto-discovery
- [x] Global resource discovery (all regions, no filtering)
- [x] Verbose logging (error, info, debug)
- [x] Multi-format output (JSON, CSV, HTML)
- [x] Compliance findings (PASS/FAILED status)
- [x] File-based workflow (scan → plan → apply)
- [x] Resource ARN in all outputs
- [x] Compliance drift between scans (`diff`)
- [x] CI gates with SARIF and JUnit output
- [x] OCSF Compliance Finding output (`--ocsf`)
- [x] Tag value normalization (`normalize`)
- [x] Terraform plan and state checking (`terraform`)
- [x] Cost attribution via Cost Explorer (`cost`)
- [ ] Kubernetes provider (coming soon)
- [ ] GCP provider (coming soon)
- [ ] Azure provider (coming soon)
- [ ] Web dashboard

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details.

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

---

<p align="center">
  Made with ❤️ for the FinOps community
</p>
