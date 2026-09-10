# IAM roles for tagctl

Two roles, one per intent. Deploy the one you need in every account tagctl
will touch and point `role_arn` in `tagctl.yaml` at it.

| Role | Template | Policies | Use it for |
|------|----------|----------|------------|
| `TagctlScan` | [`tagctl-scan-role.yaml`](tagctl-scan-role.yaml) | `TagctlScan` | `scan`, `plan`, `diff`, `normalize`, `terraform`, `cost`. Read-only: it cannot write a tag. Safe to hand to a scheduled job or an auditor. |
| `TagctlApply` | [`tagctl-apply-role.yaml`](tagctl-apply-role.yaml) | `TagctlScan` + `TagctlApply` | `apply`. Everything the scan role can do plus the tag write actions. Assume it deliberately, ideally with MFA or an external id. |

`tagctl-scan-policy.json` and `tagctl-apply-policy.json` are the policy
documents on their own, for Terraform (`file()`), the console or an existing
role. The templates embed them verbatim; `go test ./internal/provider/aws/`
fails when a copy drifts or when the provider calls an API the policies do
not allow.

## Deploy

```bash
# read-only, trusted by the whole account
aws cloudformation deploy \
  --template-file tagctl-scan-role.yaml \
  --stack-name tagctl-scan-role \
  --capabilities CAPABILITY_NAMED_IAM \
  --parameter-overrides TrustedPrincipalArn=arn:aws:iam::111111111111:root

# read-write, trusted by one role in another account, MFA required
aws cloudformation deploy \
  --template-file tagctl-apply-role.yaml \
  --stack-name tagctl-apply-role \
  --capabilities CAPABILITY_NAMED_IAM \
  --parameter-overrides \
    TrustedPrincipalArn=arn:aws:iam::222222222222:role/platform-admin \
    ExternalId=4f2c9a \
    RequireMFA=true
```

The `TagctlConfig` output is the account entry to paste under `clouds.aws`.

## Parameters

| Parameter | Default | Notes |
|-----------|---------|-------|
| `TrustedPrincipalArn` | required | Account root, IAM role/user or Identity Center permission-set role. |
| `ExternalId` | empty | Enforced as `sts:ExternalId` when set; mirror it in `external_id`. |
| `RequireMFA` | scan `false`, apply `true` | Adds `aws:MultiFactorAuthPresent`. Keep `false` for CI. |
| `RoleName` | `TagctlScan` / `TagctlApply` | |
| `MaxSessionDuration` | `3600` | Ceiling for `session_duration`; raise it for large accounts. |
| `PermissionsBoundaryArn` | empty | |

Every `Resource` is `"*"`: the policies are organised by action, not by ARN.
Scope them to ARN patterns in production if your account layout allows it.
