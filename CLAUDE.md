# CLAUDE.md

This file provides guidance for Claude Code when working on this project.

## Project Overview

**tagctl** is a cloud resource tag compliance auditing, remediation planning, and enforcement CLI tool. It scans cloud resources (AWS today; a Kubernetes provider exists but is not wired into the CLI), evaluates them against tag policies defined in YAML, generates remediation plans, and can apply fixes.

## Tech Stack

- **Language**: Go 1.26+ (see `go.mod`)
- **CLI Framework**: Cobra + Viper
- **Cloud SDK**: aws-sdk-go-v2 (AWS), client-go (Kubernetes)
- **Config**: YAML via Viper
- **Testing**: standard `testing` package
- **Linting**: golangci-lint (`.golangci.yml`)

## Project Structure

```
cmd/tagctl/main.go        # Entry point
internal/
├── cli/
│   ├── root.go           # Root command, global flags, version
│   ├── scan.go           # Scan command
│   ├── plan.go           # Plan command
│   ├── apply.go          # Apply command
│   ├── evaluate.go       # Evaluate command (Prowler integration)
│   ├── diff.go           # Compliance drift between two scans
│   ├── normalize.go      # Tag value drift detection
│   ├── terraform.go      # Terraform plan/state checking
│   ├── cost.go           # Cost attribution reporting
│   ├── gate.go           # CI gate flags shared by scan/evaluate/terraform
│   ├── init.go           # Config scaffolding
│   ├── validate.go       # Config validation
│   ├── output.go         # Table/JSON/CSV/HTML output
│   ├── paths.go          # output/ directory and file naming
│   ├── progress.go       # Progress reporting
│   ├── auth.go           # AWS auth flags shared by scan/apply/cost
│   └── providers.go      # Provider initialization
├── config/config.go      # YAML config parsing and validation
├── engine/
│   ├── scanner.go        # Resource discovery coordinator
│   ├── evaluator.go      # Tag policy evaluation
│   ├── planner.go        # Remediation plan generation
│   ├── applier.go        # Tag application
│   ├── differ.go         # Drift between two scans
│   └── normalizer.go     # Tag value clustering
├── report/               # Report writers (CI formats and the HTML report)
│   ├── sarif.go          # SARIF 2.1.0 for GitHub code scanning
│   ├── junit.go          # JUnit XML
│   ├── ocsf.go           # OCSF 1.4 Compliance Finding events (class 2003)
│   ├── gate.go           # Compliance thresholds
│   ├── findings.go       # failedFindings helper
│   ├── html.go           # HTML report view model (templates/scan.html)
│   └── templates/        # Embedded html/template files
├── terraform/parse.go    # terraform show -json parsing
├── log/log.go            # Leveled logging (error, info, debug)
├── provider/
│   ├── provider.go       # Provider interface
│   ├── aws/              # One file per service or service group: ec2
│   │                     # (+snapshots), ec2_extra, rds, rds_extra (Aurora,
│   │                     # DocumentDB, Neptune), lambda, s3, sns, sqs, elbv2,
│   │                     # elb_classic, autoscaling, dynamodb, ecs, eks,
│   │                     # elasticache, efs, ecr, kms, kinesis, logs,
│   │                     # apigateway, data, app, security (Access Analyzer,
│   │                     # ACM PCA, CloudTrail, Config, DS, FMS, GuardDuty,
│   │                     # Network Firewall, Roles Anywhere, WAF), devtools
│   │                     # (Amplify, AppSync, Bedrock, CodeArtifact,
│   │                     # CodeCommit, CodePipeline, Service Catalog,
│   │                     # Well-Architected), data_extra (Athena, DMS, Data
│   │                     # Pipeline, DataSync, EMR, Glacier, MemoryDB, MQ,
│   │                     # SES, Storage Gateway, Transfer), compute_extra
│   │                     # (AppStream, Batch, Direct Connect, DLM, DRS,
│   │                     # Lightsail, SSM, Incident Manager, WorkSpaces),
│   │                     # global + global_extra (Route 53, CloudFront, IAM,
│   │                     # Shield, WAF global, Global Accelerator); tags.go is
│   │                     # the bulk tag layer, resource.go the resource/tag
│   │                     # helpers, concurrent.go the fan-out/client cache,
│   │                     # auth.go the AssumeRole provider, cost.go the Cost
│   │                     # Explorer client for tagctl cost
│   └── k8s/              # Kubernetes resources
└── types/
    ├── resource.go       # Cloud-agnostic Resource type
    ├── violation.go      # FindingStatus, ViolationReason, Violation, Finding
    ├── scan.go           # ScanResult, AccountStats, TagStats
    ├── plan.go           # Plan, TagChange, PlanSummary
    ├── diff.go           # DiffResult, TagDelta, AccountDelta
    ├── normalize.go      # NormalizeResult, ValueCluster
    └── cost.go           # CostReport, TagCost
test/
├── testdata/             # Config and fixture files
└── testutil/             # Shared test helpers
permissions/aws/          # IAM policies (JSON, source of truth) and the
                          # TagctlScan / TagctlApply CloudFormation roles
                          # rendered from them by scripts/render-iam-templates.py
```

## Key Commands

```bash
# Development setup
make setup              # Install tools + git hooks

# Build and run
make build              # Build to ./bin/tagctl
./bin/tagctl scan       # Scan and evaluate
./bin/tagctl plan       # Generate remediation plan
./bin/tagctl apply      # Apply plan changes
./bin/tagctl evaluate   # Evaluate resources from JSON (Prowler integration)
./bin/tagctl diff       # Compliance drift between two scans
./bin/tagctl normalize  # Find tag values that are variants of one another
./bin/tagctl terraform  # Check a Terraform plan against the policy
./bin/tagctl cost       # Spend the tags fail to account for
./bin/tagctl init       # Scaffold a tagctl.yaml
./bin/tagctl validate   # Validate the config

# Development
make test               # Run all tests
make test-short         # Tests without the race detector
make coverage           # Tests with coverage report
make lint               # golangci-lint
make vet                # go vet
make fmt                # gofmt
make check              # secrets + fmt + vet + build + test-short

# Run a single package or test
go test -v ./internal/engine/...
go test -v -run TestEvaluate ./internal/engine/
```

## Global Flags

- `-c, --config` — config file (default `./tagctl.yaml`)
- `-o, --output` — output format: `table`, `json`, `csv`
- `-l, --log-level` — `error`, `info`, `debug`

## Configuration File (tagctl.yaml)

See `tagctl.yaml.example`. Structure:

```yaml
clouds:
  aws:
    - profile: default        # AWS profile name (credentials never live here)
      # role_arn: arn:aws:iam::123456789012:role/TagctlScan  # Optional AssumeRole
      regions:                # Optional: empty = all regions
        - us-east-1

policy:
  required:
    - name: environment
      values: [dev, staging, prod]
    - name: owner
      pattern: "^.+@.+$"

rules:
  infer:                      # Infer tags from resource names
    - tag: environment
      from_name:
        - pattern: "-prod-"
          value: prod

  defaults:                   # Set default values
    - resource: "*"
      when:
        "tag:owner": absent
      set:
        owner: platform@company.com

ignore:
  resources:                  # Glob patterns
    - "aws_cloudwatch_*"
  tags:
    managed-by: [terraform]
```

## Architecture Notes

### Data Flow

1. **Scan**: `engine.Scanner` → `provider.Provider.ListResources()` → `[]types.Resource`.
   A provider error keeps the other resources, sets `ScanResult.Partial`/`Errors`
   and is returned joined; `scan` writes its reports, then fails unless
   `--allow-partial`. `plan`, `diff` and `--baseline` warn on a partial scan file
2. **Evaluate**: `engine.Evaluator` → `[]types.Finding` (PASS or FAILED)
3. **Plan**: `engine.Planner` → filters FAILED findings with `missing` reason → `[]types.TagChange`
4. **Apply**: `engine.ValidatePlan` (add/update only) → `engine.Applier` → `provider.Provider.ApplyTags()` → cloud API calls

### Findings Model

Compliance checks produce findings that can be PASS or FAILED:

```go
type FindingStatus string
const (
    StatusPass   FindingStatus = "PASS"
    StatusFailed FindingStatus = "FAILED"
)

type ViolationReason string
const (
    ReasonMissing       ViolationReason = "missing"
    ReasonInvalidValue  ViolationReason = "invalid_value"
    ReasonInvalidFormat ViolationReason = "invalid_format"
    ReasonCompliant     ViolationReason = "compliant"
)
```

`ScanResult` carries both `Violations` (deprecated) and `Findings`; new code should
use `Findings`. The `violations` JSON key is kept for backwards compatibility.

### Providers

- **AWS**: 106 resource types, matching the AWS services Prowler audits (full
  table in `docs/providers/aws.mdx`).
  Discovery always goes through each service's List/Describe API, registered
  in `regionalListers()` (per region) and `globalListers()` (Route 53,
  CloudFront, IAM, Shield, WAF Classic global, WAFv2 CloudFront, Global
  Accelerator; S3 is handled apart because buckets are filtered by region). Tags are read in bulk: `startTagSources()` launches one
  `tag:GetResources` sweep per region before discovery, and listers resolve
  tags with `p.resourceTags(region, arn, fallback)` (services with their own
  tag API) or `p.bulkTags(region, arn)` after `p.requireBulkTags(region,
  label)` (services without one; the service is skipped with `log.Error`
  when the sweep is unavailable). Regions come from config; empty `regions`
  means all available regions. A resource whose tags cannot be read is
  skipped with `p.skipResource` (logs and records it), never reported as
  untagged; `requireBulkTags` records a skipped service the same way. A
  not-found answer (`resourceGone`: deleted since it was listed) is dropped
  at debug level (a batch call failing that way is re-read one item at a
  time); a cancelled context records nothing and `discover` returns the
  context error instead.
  `ListResources` returns what it found plus `errors.Join` of every lister
  error (prefixed `account <id>, region <region|global>: list <label>:`) and
  a per-service summary of those skips (prefixed `account <id>:`).
- **AWS auth**: `aws.New` relies on `config.LoadDefaultConfig` (SDK chain);
  `profile` adds `WithSharedConfigProfile`, `role_arn` wraps the credentials in
  `stscreds.NewAssumeRoleProvider` + `aws.NewCredentialsCache`
  (`internal/provider/aws/auth.go`). The region for the initial STS call is the
  SDK's, then the first configured region, then `us-east-1`; scanned regions
  always come from config. `--profile`/`--role` and friends live in
  `internal/cli/auth.go` and override a config with at most one AWS entry.
- **Kubernetes**: provider exists under `internal/provider/k8s/` but is **not wired**
  into `initProviders()` yet (commented TODO). A `clouds.kubernetes` entry
  validates but scans nothing. `resource_types` is checked against
  `config.KubernetesResourceTypes` (the single source; the k8s constants alias
  it). Secrets are opt-in, listed and patched through the metadata client so
  their data is never fetched; every List goes through `eachPage`
  (Limit/Continue).

Each provider implements the `provider.Provider` interface (`Name`,
`ListResources`, `ApplyTags`); `AccountID()` is AWS-specific.

## Code Conventions

- Standard Go formatting — run `make fmt` before committing
- Exported identifiers carry doc comments starting with the identifier name
- Errors are wrapped with `fmt.Errorf("...: %w", err)`
- Keep cloud-specific logic inside `internal/provider/<cloud>/`; `internal/engine`
  and `internal/types` stay cloud-agnostic

## Testing

- Tests live next to the code they cover (`foo.go` → `foo_test.go`)
- Shared fixtures and helpers are in `test/testutil/`; test data in `test/testdata/`
- Mock cloud SDK calls — never hit real AWS in tests

## Output Files

Scan and plan commands write to the `output/` directory (see `internal/cli/paths.go`):

- `scan-YYYYMMDD-HHMMSS.json` — full scan results with all findings
- `scan-YYYYMMDD-HHMMSS.csv` — findings CSV
- `scan-YYYYMMDD-HHMMSS.html` — standalone HTML report: headline, per-tag
  coverage chips, resource-type × tag matrix, accounts and a findings table
  with status filter and client-side pager (50 rows by default; print ignores
  the page and shows the whole filter). Rendered by `report.WriteHTML` from the
  embedded `html/template` (auto-escaped; no external assets, opens offline)
- `plan-YYYYMMDD-HHMMSS.json` — remediation plan

The gate flags (`--sarif`, `--junit`, `--ocsf`) on `scan`/`evaluate`/`terraform`
write extra machine-readable reports wherever the flag points. OCSF emits one
Compliance Finding (class 2003, schema 1.4.0) per finding, passes included;
`finding_info.uid` (`<resource identity>#<tag>`) and `analytic.uid` are the
same ids SARIF uses, keep them aligned. The full mapping is the contract in
`docs/integrations/ocsf.mdx`; extend it whenever a field changes.

## Common Patterns

### Adding a New AWS Service

1. `go get` the SDK module. Resolve the client with
   `regionalClient(p, region, svc.NewFromConfig)` (generic cache in
   `p.clients`); the typed `<svc>Clients` maps and `get<Service>Client` getters
   are the older pattern, do not add more
2. Add a `list<Service>` method in the file of its service group under
   `internal/provider/aws/` (or a new file). Split it in two: `list<Service>`
   resolves the regional client and delegates to `list<Service>From`, which
   takes a narrow API interface so tests can inject a mock. Build resources
   with `p.resource(...)` (tags known) or `p.bulkResource(...)` (bulk source);
   convert SDK tag slices with `tagsToMap`. A "service not set up" answer
   (`notSubscribed(err)`) is zero resources at debug level, not an error
3. Pick the tag source: inline tags from the Describe call when the API returns
   them; otherwise `p.resourceTags(region, arn, fallback)` when the service has
   a per-resource tag API (wrap the fallback in `forEachConcurrently`), or
   `p.requireBulkTags` + `p.bulkTags` when it does not. Any type read through
   the bulk source must also be added to `bulkTagFilterGroups` in `tags.go`
   (ARN notation, `service:type`), otherwise the sweep never returns its tags.
   On a tag-read error skip the resource with `p.skipResource`, do not report
   it as untagged; add the tag call's not-found error type to `resourceGone`
4. Add an `apply<Service>Tags` method only if the service has a tag write API
   of its own; any other ARN is routed to `applyTagsViaTaggingAPI` by
   `getResourceType` (`tagging_api`)
5. Register the lister in `regionalListers()` (or `globalListers()`) in `provider.go`
6. Route the resource type in `tagAppliers()` and `resourceTypePrefixes`.
   Resources are tagged by ARN unless their type is in `idAddressedTypes`
   (`internal/engine/applier.go`); only add a type there when its tag API
   takes a bare ID
7. Extend `TestRegionalListers`/`TestGlobalListers`,
   `TestGetResourceType_AllSupportedServices` and `TestTaggingIdentifier`,
   which pin the supported service set
8. Add the read actions to `permissions/aws/tagctl-scan-policy.json` and the
   write action to `tagctl-apply-policy.json`, run `make iam-templates` to
   re-render the CloudFormation roles, and paste the same JSON into
   `docs/providers/aws.mdx`. `TestPermissionPolicies_*` pins the three copies
   together and fails when a lister calls an API no policy allows
9. Add the resource to the table in `docs/providers/aws.mdx` and bump the
   count in README, `docs/introduction.mdx`, `docs/configuration.mdx`,
   `docs/development.mdx` and `docs/architecture.mdx`

### Adding a New CLI Command

1. Create `internal/cli/newcmd.go` with a `cobra.Command`
2. Register it with `rootCmd.AddCommand(newCmd)` in `root.go`
3. Add `internal/cli/newcmd_test.go`
4. Document it in `docs/commands.mdx`

### Adding a New Tag Rule Type

1. Add the struct in `internal/config/config.go`
2. Add parsing and validation for it
3. Add processing in `internal/engine/planner.go`

## Linting

`.golangci.yml` is v2 format and CI pins golangci-lint `v2.13.2` through
`golangci-lint-action` (`.github/workflows/ci.yml`). Reproduce CI with the same
version; a v1 binary cannot read the config, and a binary built with an older
Go than `go.mod` targets refuses to run:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
```

`make tools` runs that install (`GOLANGCI_LINT_VERSION` in the `Makefile`) and
fetches trufflehog `TRUFFLEHOG_VERSION` through its checksum-verifying install
script: trufflehog's `go.mod` has `replace` directives, so `go install` fails.

Running with `--no-config` is not a substitute: the default linter set omits
`prealloc`, `goconst` and others the repo enables, so it reports clean on code
CI will reject.

## Releases

Pushing a `v*` tag runs `.github/workflows/release.yml`: GoReleaser
(`.goreleaser.yaml`) builds linux/darwin/windows × amd64/arm64 with `-s -w`
and the `main.version`/`main.buildTime` ldflags, attaches archives and
`checksums.txt` to the GitHub Release and groups the notes by conventional
commit type. Commit subjects are `type(scope): summary`, at most 60 characters.

## Git Hooks

The pre-commit hook runs on a copy of the index (`<git dir>/tagctl-pre-commit`),
never the working tree: `.githooks/check-secrets.sh` on the staged files (any
trufflehog finding fails; `.trufflehog-ignore` holds path regexes), `gofmt` on
staged Go files, `golangci-lint --new-from-patch` with the staged diff (whole
tree on a root commit; `go vet` when the binary is missing), `go build
-buildvcs=false` (the copy sits inside `.git`) and `go test -short`. Tools
resolve from `$(go env GOPATH)/bin` first, where `make tools` installs the
versions CI pins.

Install with: `make hooks`

## Known Gotchas

1. **S3 is global**: buckets are listed once through `ListBuckets` (paginated, which
   also returns each bucket's region), then filtered by the configured `regions`
   before their tags are read with the right regional client. A bucket whose tags
   cannot be read is skipped with an error, never reported as untagged

2. **Empty regions config**: `regions: []` means discover all available regions
   via the EC2 API

3. **Findings vs Violations**: the codebase is migrating to "findings" terminology.
   A Finding can be PASS or FAILED, while a Violation is always FAILED. The
   `Violations` field and `violations` JSON key remain for backwards compatibility

4. **HTML report**: shows all findings (PASS and FAILED) with status chips, not
   just failures

5. **Resource identity is `Resource.Identity()`, never `ID`**: the ARN, or
   `provider/account/region/id` with empty parts dropped. AWS repeats names
   across regions (Control Tower log groups and lambdas), and keying by bare ID
   once produced 4 phantom compliant resources in a scan with 0. The evaluator
   (`CompliantCount`), differ, planner summary, applier grouping, normalizer,
   HTML matrix, SARIF fingerprint and OCSF `finding_info.uid` all key by it;
   anything new that counts, groups or compares resources must too

6. **YAML list formatting**: `clouds.aws` is a list of accounts. Two common
   mistakes: putting `regions` as a separate list item (creates a second account
   with no identity) and writing `aws:` as a mapping instead of a list (works by
   accident: the decoder lifts it into a one-element list). `config.Validate()`
   rejects the first; `tagctl validate` warns about the second

7. **Config validation runs on every command**: `loadConfig()` calls
   `config.Validate()`. It does not require a cloud provider (scan has demo mode,
   evaluate reads JSON); it checks the account shape (no static keys, role
   options only with `role_arn`, unknown fields rejected), region codes, regex
   patterns and Kubernetes `resource_types` (unknown or repeated types rejected)

8. **Bulk tags are eventually consistent**: `tag:GetResources` can lag a few
   minutes behind `tagctl apply`. Per-resource tag APIs are read-after-write,
   so a fresh scan may disagree between services for a short while

9. **Bulk tags only cover the configured regions plus `us-east-1`**: the
   global listers read from the `us-east-1` sweep, which `startTagSources`
   adds even when that region is not configured

10. **The bulk sweep must stay filtered**: an unfiltered `GetResources` in
   a real development account returned 33k tagged ARNs (337 sequential pages, 32 s)
   while the audited set was 485. `bulkTagFilterGroups` restricts it to the
   audited types and runs the groups in parallel; a wrong filter string makes
   the whole region fail with `InvalidParameterException` (loud, then fallback)

11. **Prowler parity is the coverage target**: the service list mirrors
    `prowler/providers/aws/services` (90 services); the seven with nothing to
    tag per account are deliberately absent (account, inspector2, macie,
    organizations, resourceexplorer2, securityhub, trustedadvisor). Global
    Accelerator lives only in us-west-2 and Lightsail only in
    `lightsailRegions`; both are addressed by their own tag API, not the
    Tagging API. `idAddressedTypes` in the applier is the short list of types
    tagged by ID (EC2 family, S3); everything else is tagged by ARN
