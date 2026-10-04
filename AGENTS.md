# AGENTS.md

This file provides guidance for coding agents (Claude Code, Codex, Cursor and
others) working on this project.

## Project Overview

**tagctl** is a cloud resource tag compliance auditing, remediation planning, and enforcement CLI tool. It scans cloud resources (AWS and Kubernetes; `gcp` and `azure` are reserved config keys), evaluates them against tag policies defined in YAML, generates remediation plans, and can apply fixes.

## Tech Stack

- **Language**: Go 1.26.6+ (see `go.mod`); `.devcontainer/` pins the Go 1.26
  image (`GOTOOLCHAIN=local`), bump its tag with a new Go minor in `go.mod`
- **CLI Framework**: Cobra + Viper
- **Cloud SDK**: aws-sdk-go-v2 (AWS), client-go (Kubernetes)
- **Config**: YAML decoded strictly by yaml.v3; Viper only locates the file
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
│   ├── apply_interactive.go # apply --interactive: per-resource review
│   ├── evaluate.go       # Evaluate command (Prowler integration)
│   ├── diff.go           # Compliance drift between two scans
│   ├── normalize.go      # Tag value drift detection
│   ├── terraform.go      # Terraform plan/state checking
│   ├── cost.go           # Cost attribution reporting
│   ├── gate.go           # CI gate flags shared by scan/evaluate/terraform
│   ├── exitcode.go       # ExitCode: 0 success, 1 gate failed, 2 other error
│   ├── init.go           # Config scaffolding (--template, --list-templates)
│   ├── templates/        # Embedded tagctl.yaml templates, one per --template name
│   ├── validate.go       # Config validation
│   ├── output.go         # Table/JSON/CSV/HTML output
│   ├── format.go         # -o validation per command (outputFormatFor)
│   ├── paths.go          # output/ directory and file naming
│   ├── progress.go       # Progress reporting
│   ├── color.go          # paletteFor: ANSI codes per stream
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
│   ├── markdown.go       # Markdown summary for CI job summaries
│   ├── gate.go           # Compliance thresholds
│   ├── findings.go       # findingMessage helper, tool name
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
│   │                     # pagination.go the page-token loop for operations
│   │                     # without an SDK paginator, auth.go the AssumeRole
│   │                     # provider, cost.go the Cost Explorer client for
│   │                     # tagctl cost
│   └── k8s/              # Kubernetes resources
└── types/
    ├── resource.go       # Cloud-agnostic Resource type
    ├── violation.go      # FindingStatus, ViolationReason, Violation, Finding
    ├── scan.go           # ScanResult, AccountStats, TagStats
    ├── plan.go           # Plan, TagChange, PlanSummary
    ├── diff.go           # DiffResult, TagDelta, AccountDelta
    ├── normalize.go      # NormalizeResult, ValueCluster
    └── cost.go           # CostReport, TagCost, CostTrend (periods, projection)
test/
├── testdata/             # Config and fixture files
└── testutil/             # Shared test helpers
permissions/aws/          # IAM policies (JSON, source of truth) and the
                          # TagctlScan / TagctlApply CloudFormation roles
                          # rendered from them by scripts/render-iam-templates.py
                          # (CodeBuild tagging and the protected tag key deny
                          # are opt-in apply template parameters)
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
make fmt                # golangci-lint fmt (gofmt + goimports)
make check              # secrets + fmt + vet + build + test-short

# Run a single package or test
go test -v ./internal/engine/...
go test -v -run TestEvaluate ./internal/engine/
```

## Global Flags

- `-c, --config` — config file (default `./tagctl.yaml`)
- `-o, --output` — stdout format, case-insensitive, checked by each command with
  `outputFormatFor` (first listed is the default when unset): scan
  `table|json|csv`; cost `table|json|csv`; plan, diff, normalize, terraform `table|json`;
  evaluate `json`; apply, init, validate, version `table`. Anything else exits 2
- `-l, --log-level` — `error`, `info`, `debug`; anything else exits 2

stdout carries only the selected output. Banner, spinners, prompts, warnings and
"wrote X" notices go to stderr (`fmt.Fprint(os.Stderr, ...)`). A gate report on
`-` replaces the command's stdout output; `gateOptions.stdoutFormat` rejects two
reports on `-` or one next to an explicit `-o`

Colour is per stream: `log.UseColor(w)` is the single decision (a terminal and
`NO_COLOR` empty). CLI code takes its codes from `paletteFor(w)`
(`internal/cli/color.go`), whose fields are empty when colour is off; never
write a raw ANSI code. Spinners animate only on a terminal. Values from cloud
data or user files go through `printable()` before reaching a table or a
stderr notice; log lines (`log.write`) and the `Error:` line in `main` go
through `log.Printable`, which keeps line breaks and tabs

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
   `--allow-partial`. `plan`, `diff` and `--baseline` warn on a partial scan file.
   `scan --resource-type <glob>` (`RealScanner.OnlyTypes`) drops non-matching
   types after discovery, before `ignore`: listers do not declare their
   resource types, so no API call is saved
2. **Evaluate**: `engine.Evaluator` → `[]types.Finding` (PASS or FAILED)
3. **Plan**: `engine.Planner` → filters FAILED findings with `missing` reason → `[]types.TagChange`
4. **Apply**: `engine.ValidatePlan` (add/update only) → `engine.Applier` → `provider.Provider.ApplyTags()` → cloud API calls

`apply --interactive` runs `reviewChanges` between loading the plan and the
applier: one prompt per `Resource.Identity()` on stderr, answers read from
`applyInput` (tests replace it and `stdinIsTerminal`). The approved changes
become a filtered copy of the plan, checked again with `engine.ValidatePlan`;
skipped changes are counted apart, never as applied or failed.

### Exit codes

`main` exits with `cli.ExitCode(err)`: 0 on success, 1 when the error is marked
with `gateFailed`, 2 for anything else (Cobra usage errors, partial scans and
failed applies included). `gateOptions.check` marks `--fail-under`/`--fail-on-new`;
`cost --fail-under`, `diff --fail-on-regression` and `normalize --fail-on-drift`
wrap their own. A new gate that returns an unmarked error exits 2.

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

`ScanResult` carries both `Violations` (deprecated, removed in `v1.0.0`) and `Findings`; new code should
use `Findings`. The `violations` JSON key is kept for backwards compatibility. The
evaluator checks each resource once; `Violations` is its FAILED findings in the same
order (`type Violation Finding`). `ScanResult.FailedFindings()` falls back to
`Violations` for scan files written before findings existed; planner, differ,
SARIF and `terraform` read failures through it. An optional tag is checked only
on resources that carry it: an absent one is no finding and not `missing` in
`by_tag`, and its `compliance_percent` is the valid share of its carriers (100
when there are none).
`ScanResult.Resources` (`resources`, omitted when empty) is the inventory
`EvaluateResources` records, one `ResourceRef` (identity, id, arn, type, provider,
account, region) per resource, findings or not. `engine.Diff` takes new/removed
resources from the inventory plus the resources findings name; `diff` warns when
a scan file counts resources but has neither (written before the inventory).

### Providers

- **AWS**: 106 resource types, matching the AWS services Prowler audits (full
  table in `docs/providers/aws.mdx`).
  Discovery always goes through each service's List/Describe API, registered
  in `regionalListers()` (per region) and `globalListers()` (Route 53,
  CloudFront, IAM, Shield, WAF Classic global, WAFv2 CloudFront, Global
  Accelerator; S3 is handled apart because buckets are filtered by region). Tags are read in bulk: `startTagSources()` launches one
  `tag:GetResources` sweep per region before discovery, and listers resolve
  tags with `p.resourceTags(ctx, region, arn, fallback)` (services with their own
  tag API) or `p.bulkTags(ctx, region, arn)` after
  `p.requireBulkTags(ctx, region, label)` (services without one; the service
  is skipped with `log.Error`
  when the sweep is unavailable). Regions come from config; empty `regions`
  means all available regions. A resource whose tags cannot be read is
  skipped with `p.skipResource` (logs and records it), never reported as
  untagged; `requireBulkTags` records a skipped service the same way. A
  not-found answer (`resourceGone`: deleted since it was listed) is dropped
  at debug level (a batch call failing that way is re-read one item at a
  time); a cancelled context records nothing and `discover` returns the
  context error instead. The bulk tag helpers take the scan `ctx` and stop
  waiting for the sweep once it is cancelled.
  `ListResources` returns what it found plus `errors.Join` of every lister
  error (prefixed `account <id>, region <region|global>: list <label>:`) and
  a per-service summary of those skips (prefixed `account <id>:`). `discover`
  runs at most `maxConcurrentListers` (32) listers at once and starts none
  once the context is cancelled (that one context error per account counts
  the listers not started); each per-resource fan-out has its own
  `maxConcurrentAPICalls` (16) pool that takes the scan `ctx` and dispatches
  no further item once it is cancelled (ECS nests two, up to 256 calls on one
  client) and the tag sweeps run outside both, so never take a lister slot
  from inside a lister.
- **AWS auth**: `aws.New` relies on `config.LoadDefaultConfig` (SDK chain);
  `profile` adds `WithSharedConfigProfile`, `role_arn` wraps the credentials in
  `stscreds.NewAssumeRoleProvider` + `aws.NewCredentialsCache`
  (`internal/provider/aws/auth.go`). The region for the initial STS call is the
  SDK's, then the first configured region, then `us-east-1`; scanned regions
  always come from config. `withRetryDefaults` sets adaptive retries with
  `maxRetryAttempts` (7) unless `AWS_RETRY_MODE`/`AWS_MAX_ATTEMPTS` or the
  profile set them. `--profile`/`--role` and friends live in
  `internal/cli/auth.go` and override a config with at most one AWS entry.
- **AWS partitions**: `New` reads the partition from the caller identity ARN
  (`partitionOf`) into `Provider.partition`; empty means `aws` (providers built
  in tests). Build every ARN with `p.buildARN`/`p.ec2ARN`, never a literal
  `arn:aws:`. `p.globalRegion()` (table in `partition.go`, copied from the
  SDK's `partitions.json`) is where the global clients, Cost Explorer, the
  extra bulk sweep (`tagSweepRegions`) and the Tagging API for region-less
  ARNs (`taggingRegion`) go. Global Accelerator is skipped outside `aws`.
  `getResourceType` routes ARNs with `arn.Parse` on the service and resource
  segments, so it is partition-agnostic; a malformed ARN has no route.
- **Kubernetes**: `initProviders()` builds one `k8s.Provider` per
  `clouds.kubernetes` entry through `newKubernetesProvider` (a package var in
  `internal/cli/providers.go`; tests swap it for `k8s.NewWithClients` with
  client-go fake clientsets), so `scan`, `plan` and `apply` run against
  clusters. Labels are the tags. A resource has `Provider` `kubernetes`,
  `Account` the cluster `name` (unique, `config.Validate()` rejects a repeat),
  `Region` the namespace and `ID` `<type>/<namespace>/<name>`. `restConfig`
  resolves credentials: `kubeconfig: in-cluster`, an explicit path, or the
  kubectl loading rules (`KUBECONFIG`, `~/.kube/config`, in-cluster); a
  kubeconfig or context that cannot be loaded fails `initProviders`.
  `ListResources` lists each type once cluster-wide, or once per configured
  namespace (namespaces themselves by `Get`, so namespaced Roles are enough);
  every failing call is returned joined, prefixed `cluster <name>:`, next to
  what was listed, so an unreachable cluster is a partial scan. `ApplyTags`
  runs `validateLabels` (label key and value syntax) before the merge patch and
  fails the resource without an API call: an e-mail is not a valid label
  value. Annotations are not read or written. `resource_types` is checked
  against `config.KubernetesResourceTypes` (the single source; the k8s
  constants alias it). Secrets are opt-in, listed and patched through the
  metadata client so their data is never fetched; every List goes through
  `eachPage` (Limit/Continue).

Each provider implements the `provider.Provider` interface (`Name`,
`ListResources`, `ApplyTags`). `AccountID()` is optional (AWS account id,
Kubernetes cluster name): the applier routes a change to the provider whose
`AccountID()` equals the resource's `Account`, so a provider that can be
configured more than once must implement it.
A provider that also implements `ApplyTagsInRegion` (engine `regionalTagger`)
receives `Resource.Region` from the plan: AWS needs it for EC2 resources,
tagged by bare ID with one `CreateTags` call in that region, and fails without
it instead of probing regions.

Provider status has one table, "Provider Status" in `docs/development.mdx`.
README, CONTRIBUTING, `tagctl.yaml.example`, the `init` templates and the docs
pages introduction, configuration, architecture, credentials, rules, roadmap
and providers/kubernetes summarize it and link there: change them together.

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

Scan and plan commands write to the `output/` directory (see `internal/cli/paths.go`).
`--output-dir` on `scan`, `plan` and `apply` names another one (`outputDirFor`
resolves it and the path helpers take it as an argument; `OutputDir` is only the
default); `scan --no-files` writes no report files. `diff` and `normalize` still
look up the latest scan in the default directory:

- `scan-YYYYMMDD-HHMMSS.json` — full scan results with all findings
- `scan-YYYYMMDD-HHMMSS.csv` — findings CSV
- `scan-YYYYMMDD-HHMMSS.html` — standalone HTML report: headline, per-tag
  coverage chips, resource-type × tag matrix, accounts, a by-tag-value table
  and a findings table with status filter and client-side pager (50 rows by
  default; print ignores the page and shows the whole filter). Rendered by
  `report.WriteHTML` from the embedded `html/template` (auto-escaped; no
  external assets, opens offline). The by-value table (`htmlByValue`) is
  aggregated in Go per policy tag: resources by `Identity()`, compliant = no
  FAILED finding, `(untagged)` row last, opening on `owner`/`team` when the
  policy has one. Each finding row carries `data-g`, the value index per tag,
  so the browser filters by comparing indexes and never re-aggregates
- `plan-YYYYMMDD-HHMMSS.json` — remediation plan

The gate flags (`--sarif`, `--junit`, `--ocsf`) on `scan`/`evaluate`/`terraform`
write extra machine-readable reports (mode 0600, `createReport`) wherever the
flag points. SARIF anchors results to the config file read
(`viper.ConfigFileUsed()`, or `evaluate --policy`) relative to the working
directory, sets `automationDetails.id` `tagctl/<command>/` and marks a partial
scan `invocations[0].executionSuccessful: false`. OCSF emits one
Compliance Finding (class 2003, schema 1.4.0) per finding, passes included, as
NDJSON when the path ends in `.ndjson`/`.jsonl` and as a JSON array otherwise;
`finding_info.uid` (`<resource identity>#<tag>`) and `analytic.uid` are the
same ids SARIF uses, keep them aligned. The full mapping is the contract in
`docs/integrations/ocsf.mdx`; extend it whenever a field changes.

`--summary` (same commands) writes `report.WriteMarkdown`: headline, per-tag
table, top failing resource types (distinct `Identity()`) and the first 25
failed findings. Every cell goes through `markdownCell`, since names and tag
values are account-controlled. `GITHUB_STEP_SUMMARY` is never read: the user
passes it to the flag.

## Common Patterns

### Adding a New AWS Service

The canonical guide, with snippets, is
`docs/contributing/adding-an-aws-service.mdx`; change it with the pattern.
Checklist:

1. Client: `regionalClient(p, region, svc.NewFromConfig)` (global services
   pass `p.globalRegion()`); `Provider` has no per-service client fields or
   getters
2. Lister: `list<Service>` resolves the client, `list<Service>From` takes a
   narrow API interface; `p.resource`/`p.bulkResource`, `tagsToMap`;
   `notSubscribed(err, "<the service's own code>")` is zero resources at
   debug level; page with the SDK paginator or `paginate`
3. Tags: inline, `p.resourceTags(ctx, region, arn, fallback)` inside
   `forEachConcurrently(ctx, ...)`, or `p.requireBulkTags(ctx, ...)` +
   `p.bulkTags(ctx, ...)`. ARNs the API does not return: `p.buildARN` /
   `p.ec2ARN`, never a literal `arn:aws:`. Types read
   through the sweep go in `bulkTagFilterGroups` (a missing filter silently
   reads as untagged). Tag-read error: `p.skipResource`; add the not-found
   error type to `resourceGone`
4. Writes: `tagging_api` (`applyTagsViaTaggingAPI`) by default;
   `apply<Service>Tags` (region from `regionForARN(arn)`, never a configured
   region) + `tagAppliers` + `arnServiceRoutes` only when the Tagging
   API cannot tag the type; `idAddressedTypes` + `ec2IDPrefixes` only for
   bare-ID tag APIs, which get the plan's region through `regionTagAppliers`
   (built once from `ec2IDPrefixes`, plus S3)
5. Register in `regionalListers()` (`provider.go`) or `globalListers()`
6. Pinned tests: `TestRegionalListers`/`TestGlobalListers`,
   `TestGetResourceType_AllSupportedServices`, `TestTaggingIdentifier`
7. IAM: read actions in `tagctl-scan-policy.json`, write action in
   `tagctl-apply-policy.json` on the Service Authorization Reference ARN
   pattern (`"*"` only via `unscopedWriteActions`), `make iam-templates`, same
   JSON in `docs/providers/aws.mdx`. `TestPermissionPolicies_*` pins the
   copies, API coverage and the 10,240-character inline limit
8. Docs: table and "How tags are read" in `docs/providers/aws.mdx`; bump the
   count in README, CONTRIBUTING, this file, introduction, configuration,
   development, architecture

### Adding a New CLI Command

1. Create `internal/cli/newcmd.go` with a `cobra.Command`
2. Register it with `rootCmd.AddCommand(newCmd)` in `root.go`
3. Add `internal/cli/newcmd_test.go`
4. Document it in `docs/commands.mdx`

### Adding an init Template

1. Add `internal/cli/templates/<name>.yaml`, a complete config using only
   keys `config.Load` accepts
2. List it in `configTemplates` (`internal/cli/init.go`);
   `TestConfigTemplates_MatchEmbeddedFiles` fails on a file that is not listed
   and `TestInit_EveryTemplateLoadsAndValidates` on one `validate` rejects
3. Add it to the templates table in `docs/commands.mdx`

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

## GitHub Action

`action.yml` at the repo root is a composite action (`uses: unicrons/tagctl@<ref>`):
`actions/setup-go`, `go install ...@<version>` (default: the action ref; a
`uses: ./` checkout has no ref and builds from `github.action_path`), then
tagctl with the `args` input. Inputs reach the scripts through `env:` only,
never `${{ inputs.* }}` inside `run:`. `.github/workflows/action-test.yml` runs
it from the checkout when `action.yml` changes; the contract is
`docs/integrations/github-action.mdx`.

## Git Hooks

The pre-commit hook runs on a copy of `git write-tree` (`<git dir>/tagctl-pre-commit`;
intent-to-add paths are left out), never the working tree: the staged
`.githooks/check-secrets.sh` on the staged files (any
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
   cannot be read is skipped with an error, never reported as untagged. Apply
   tags a bucket by ARN through `tag:TagResources` in the bucket's region
   (from the plan, else `GetBucketLocation`), never with a read followed by
   `PutBucketTagging`, which replaces the whole tag set

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
   with no identity) and writing `aws:` as a mapping instead of a list (still
   accepted: `AWSAccounts.UnmarshalYAML` lifts it into a one-element list).
   `config.Validate()` rejects the first; `tagctl validate` warns about the second

7. **Config loading is strict and runs on every command**: Viper only locates
   the file (`--config` or discovery); `config.Load` decodes it with yaml.v3
   `KnownFields(true)` (unknown keys fail with their line) and runs
   `config.Validate()`, for `loadConfig()` and `evaluate --policy` alike. Never
   decode it with `viper.Unmarshal`: Viper lowercases map keys, so mixed-case
   tag names in `when`, `set` and `ignore.tags` silently stop matching.
   Validation does not require a cloud provider (scan has demo mode, evaluate
   reads JSON); it checks the account shape (no static keys, role options only
   with `role_arn`, unknown account fields rejected), region codes, regex and
   glob (`path.Match`) patterns, tag names defined twice, `rules.defaults`
   (`resource`, non-empty `set`, `when` keys `tag:<name>` with `absent` or an
   exact value; the planner fails closed on anything else) and Kubernetes
   clusters (unique `name`; unknown or repeated `resource_types` rejected)

8. **Bulk tags are eventually consistent**: `tag:GetResources` can lag a few
   minutes behind `tagctl apply`. Per-resource tag APIs are read-after-write,
   so a fresh scan may disagree between services for a short while

9. **Bulk tags only cover the configured regions plus the partition's global
   region** (`us-east-1` in `aws`): the global listers read from that sweep,
   which `startTagSources` adds even when the region is not configured

10. **The bulk sweep must stay filtered**: an unfiltered `GetResources` in
   a real development account returned 33k tagged ARNs (337 sequential pages, 32 s)
   while the audited set was 485. `bulkTagFilterGroups` restricts it to the
   audited types and runs the groups in parallel; a wrong filter string makes
   the whole region fail with `InvalidParameterException` (loud, then fallback)

11. **Prowler parity is the coverage target**: the service list mirrors
    `prowler/providers/aws/services` (90 services); the seven with nothing to
    tag per account are deliberately absent (account, inspector2, macie,
    organizations, resourceexplorer2, securityhub, trustedadvisor). Global
    Accelerator lives only in us-west-2 and Lightsail only in the regions
    `lightsailEndpoints` returns (`GetRegions` once per provider, commercial
    partition only; `lightsailFallbackRegions` and one `log.Error` when the
    call fails); both are addressed by their own tag API, not the Tagging API.
    `idAddressedTypes` in the applier is the short list of types tagged by ID
    (EC2 family); everything else, S3 buckets included, is tagged by ARN. An
    identifier that is neither an ARN nor an EC2 ID has no route and
    `ApplyTags` fails with `unknown resource type`

12. **Cost Explorer bills per request**: `cost` runs one `GetCostAndUsage`
    query per tag (plus its pages). `--trend` switches that query to `DAILY`
    and buckets the days in `types.CostPeriods` (daily up to 14 days, weekly
    beyond; periods align to the window end, a leading remainder is `Partial`
    and excluded from `CostChange`/`CostProjection`). Never add a call per
    period. `cost` has no mock mode; `costExplorerAPI` is mocked in tests
