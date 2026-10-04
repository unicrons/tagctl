# IAM roles for tagctl

Two roles, one per intent. Deploy the one you need in every account tagctl
will touch and point `role_arn` in `tagctl.yaml` at it.

| Role | Template | Policies | Use it for |
|------|----------|----------|------------|
| `TagctlScan` | [`tagctl-scan-role.yaml`](tagctl-scan-role.yaml) | `TagctlScan` | `scan`, `plan`, `diff`, `normalize`, `terraform`, `cost`. Read-only: it cannot write a tag. Safe to hand to a scheduled job or an auditor. |
| `TagctlApply` | [`tagctl-apply-role.yaml`](tagctl-apply-role.yaml) | `TagctlScan` + `TagctlApply`, plus `TagctlApplyCodeBuild` and `TagctlProtectedTagKeys` when enabled | `apply`. Everything the scan role can do plus the tag write actions. Assume it deliberately, ideally with MFA or an external id. |

`tagctl-scan-policy.json`, `tagctl-apply-policy.json` and
`tagctl-apply-codebuild-policy.json` are the policy documents on their own, for
Terraform (`file()`), the console or an existing role. The templates embed
them verbatim; `go test ./internal/provider/aws/` fails when a copy drifts or
when the provider calls an API the policies do not allow.

## Deploy

```bash
# read-only, trusted by every principal of the account allowed to assume it,
# as long as it belongs to the organization
aws cloudformation deploy \
  --template-file tagctl-scan-role.yaml \
  --stack-name tagctl-scan-role \
  --capabilities CAPABILITY_NAMED_IAM \
  --parameter-overrides \
    TrustedPrincipalArn=arn:aws:iam::123456789012:root \
    OrgId=o-a1b2c3d4e5

# read-write, trusted by one role in another account, MFA required
aws cloudformation deploy \
  --template-file tagctl-apply-role.yaml \
  --stack-name tagctl-apply-role \
  --capabilities CAPABILITY_NAMED_IAM \
  --parameter-overrides \
    TrustedPrincipalArn=arn:aws:iam::222222222222:role/platform-admin \
    ExternalId=4f2c9a \
    RequireMFA=true \
    ProtectedTagKeys=access-team,access-level
```

The `TagctlConfig` output is the account entry to paste under `clouds.aws`.

## Who can assume the role

`TrustedPrincipalArn` becomes the `Principal` of the role's trust policy:

```json
{
  "Effect": "Allow",
  "Principal": { "AWS": "arn:aws:iam::123456789012:root" },
  "Action": "sts:AssumeRole"
}
```

An account root ARN does not mean the root user. It delegates the decision to
that account: every IAM user and role in `123456789012` whose identity policy
allows `sts:AssumeRole` on this role can assume it, administrators included.
That is acceptable for the read-only scan role inside an account you control;
for anything else, and always for `TagctlApply`, name one role or user
(`arn:aws:iam::123456789012:role/platform-admin`).

Narrow either form with the conditions the templates offer: `OrgId`
(`aws:PrincipalOrgID`, the caller must belong to your organization),
`ExternalId` and `RequireMFA`.

## Parameters

| Parameter | Default | Notes |
|-----------|---------|-------|
| `TrustedPrincipalArn` | required | IAM role/user, Identity Center permission-set role or account root. See [Who can assume the role](#who-can-assume-the-role). |
| `ExternalId` | empty | Enforced as `sts:ExternalId` when set; mirror it in `external_id`. |
| `RequireMFA` | scan `false`, apply `true` | Adds `aws:MultiFactorAuthPresent`. Keep `false` for CI. |
| `OrgId` | empty | Adds `aws:PrincipalOrgID`: the caller must also belong to this organization. It narrows `TrustedPrincipalArn`, it does not replace it. |
| `RoleName` | `TagctlScan` / `TagctlApply` | |
| `MaxSessionDuration` | `3600` | Ceiling for `session_duration`; raise it for large accounts. |
| `PermissionsBoundaryArn` | empty | |
| `AllowCodeBuildTagging` | `false` (apply only) | Attaches `TagctlApplyCodeBuild`. See [CodeBuild](#codebuild). |
| `ProtectedTagKeys` | empty (apply only) | Comma-separated tag keys, wildcards allowed, no spaces around the commas. Attaches `TagctlProtectedTagKeys`. See [Protected tag keys](#protected-tag-keys). |

## Scope

The scan policy stays on `"*"`: it only reads. `ce:GetCostAndUsage` in it
serves `tagctl cost` and nothing else.

Every write action in the apply policy is limited to the ARN patterns of the
resource types tagctl tags, taken from the
[Service Authorization Reference](https://docs.aws.amazon.com/service-authorization/latest/reference/reference_policies_actions-resources-contextkeys.html).
API Gateway tagging (REST and HTTP/WebSocket APIs) maps to `apigateway:PUT`,
`POST` and `PATCH` in the reference; the policy grants `PUT` and `POST` on
`/tags/*` only, so they cannot create, update or import an API. The reference
lists the `Tags` resource type under `PUT` but not `POST`, so HTTP API tagging
through the `POST` scope is unverified. `tag:TagResources` and
`workspaces:CreateTags` stay on `"*"` because the reference lists no resource
type for them; `tag:TagResources` still needs the scoped action of the service
that owns the resource. The ARNs use the `aws` partition: replace `arn:aws:`
in AWS China or GovCloud.

Service Catalog portfolios may not be taggable: the Resource Groups supported
resources table marks `AWS::ServiceCatalog::Portfolio` as not taggable through
Tag Editor, and the reference maps `servicecatalog:TagResource` only to
AppRegistry applications and attribute groups, which tagctl never tags, so the
policy does not grant it. Portfolio tags change through
`servicecatalog:UpdatePortfolio`, which the policy does not grant either; which
action `tag:TagResources` checks for a portfolio is not documented.

IAM caps the inline policies of a role at 10,240 characters in total,
whitespace excluded. Scan, apply and CodeBuild policies use about 9,150, which
leaves about 900 for the `ProtectedTagKeys` list.

### CodeBuild

CodeBuild has no tag-only IAM action: project tags are written through
`codebuild:UpdateProject`, which can also replace a project's buildspec and
service role. It is off by default and `tagctl apply` reports CodeBuild changes
as failed until `AllowCodeBuildTagging=true`.

### Protected tag keys

`ProtectedTagKeys` adds a Deny on every request whose `aws:TagKeys` match one
of the keys (`ForAnyValue:StringLike`). Use it for the keys your ABAC policies
check. It only applies to actions whose Service Authorization Reference entry
lists `aws:TagKeys`: `s3:PutBucketTagging`, `route53:ChangeTagsForResource`
and `glacier:AddTagsToVault` do not, so bucket, hosted zone and vault tags
are not protected by it. `StringLike` is case-sensitive, but tag keys on IAM
users and roles are not: `iam:TagRole` with `Access-Team` overwrites
`access-team` without matching `ProtectedTagKeys=access-team`. List the case
variants, or use `?` for letters whose case may vary (`?ccess-?eam`).
