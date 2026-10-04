#!/usr/bin/env python3
"""Render permissions/aws/tagctl-*-role.yaml from the JSON policy files."""
import json
import pathlib

ROOT = pathlib.Path(__file__).resolve().parent.parent / "permissions" / "aws"


def load(name):
    return json.loads((ROOT / name).read_text())


def policy_yaml(doc, indent):
    pad = " " * indent
    out = [f'{pad}Version: "2012-10-17"', f"{pad}Statement:"]
    for st in doc["Statement"]:
        out.append(f"{pad}  - Sid: {st['Sid']}")
        out.append(f"{pad}    Effect: {st['Effect']}")
        out.append(f"{pad}    Action:")
        out.extend(f"{pad}      - {a}" for a in st["Action"])
        if isinstance(st["Resource"], str):
            out.append(f'{pad}    Resource: "{st["Resource"]}"')
        else:
            out.append(f"{pad}    Resource:")
            out.extend(f'{pad}      - "{r}"' for r in st["Resource"])
    return "\n".join(out)


def policy_entry(name, source, condition):
    doc = load(source)
    if condition is None:
        return f"        - PolicyName: {name}\n          PolicyDocument:\n{policy_yaml(doc, 12)}\n"
    return (
        f"        - Fn::If:\n"
        f"            - {condition}\n"
        f"            - PolicyName: {name}\n"
        f"              PolicyDocument:\n{policy_yaml(doc, 16)}\n"
        f"            - {{Ref: AWS::NoValue}}\n"
    )


HEADER = """AWSTemplateFormatVersion: "2010-09-09"
Description: >-
  {description}

Metadata:
  AWS::CloudFormation::Interface:
    ParameterGroups:
      - Label:
          default: Who can assume the role
        Parameters:
          - TrustedPrincipalArn
          - ExternalId
          - RequireMFA
          - OrgId
      - Label:
          default: Role settings
        Parameters:
          - RoleName
          - MaxSessionDuration
          - PermissionsBoundaryArn
{extra_groups}
Parameters:
  TrustedPrincipalArn:
    Type: String
    Description: >-
      Principal allowed to assume the role: an account root
      (arn:aws:iam::111111111111:root), an IAM role or user, or an Identity
      Center permission-set role. Use the account root to trust the whole
      account and let IAM policies there decide who assumes it.
    AllowedPattern: "^arn:aws[a-z-]*:iam::[0-9]{{12}}:.+$"
    ConstraintDescription: must be an IAM principal ARN
  ExternalId:
    Type: String
    Description: >-
      Optional. When set, callers must send it as sts:ExternalId; put the same
      value in external_id in tagctl.yaml. Recommended when the principal lives
      in another account.
    Default: ""
    NoEcho: true
  RequireMFA:
    Type: String
    Description: >-
      Require an MFA-authenticated session to assume the role. Set to true for
      roles assumed by people; keep false for CI, where no MFA context exists.
    Default: "{mfa_default}"
    AllowedValues: ["true", "false"]
  OrgId:
    Type: String
    Description: >-
      Optional. AWS Organizations id. When set, the caller must also belong to
      this organization (aws:PrincipalOrgID), on top of matching
      TrustedPrincipalArn.
    Default: ""
    AllowedPattern: "^(o-[a-z0-9]{{10,32}})?$"
    ConstraintDescription: must be empty or an organization id such as o-a1b2c3d4e5
  RoleName:
    Type: String
    Description: Name of the role. Referenced from tagctl.yaml as role_arn.
    Default: {role_name}
    AllowedPattern: "^[A-Za-z0-9+=,.@_-]{{1,64}}$"
  MaxSessionDuration:
    Type: Number
    Description: >-
      Upper bound, in seconds, for session_duration in tagctl.yaml. A full
      scan of a large account can run past one hour.
    Default: {session_default}
    MinValue: 3600
    MaxValue: 43200
  PermissionsBoundaryArn:
    Type: String
    Description: Optional permissions boundary to attach to the role.
    Default: ""
{extra_parameters}
Conditions:
  HasExternalId:
    Fn::Not:
      - Fn::Equals: [{{Ref: ExternalId}}, ""]
  MFARequired:
    Fn::Equals: [{{Ref: RequireMFA}}, "true"]
  HasOrgId:
    Fn::Not:
      - Fn::Equals: [{{Ref: OrgId}}, ""]
  HasTrustConditions:
    Fn::Or: [{{Condition: HasExternalId}}, {{Condition: MFARequired}}, {{Condition: HasOrgId}}]
  HasPermissionsBoundary:
    Fn::Not:
      - Fn::Equals: [{{Ref: PermissionsBoundaryArn}}, ""]
{extra_conditions}
Resources:
  Role:
    Type: AWS::IAM::Role
    Properties:
      RoleName: {{Ref: RoleName}}
      Description: {role_description}
      MaxSessionDuration: {{Ref: MaxSessionDuration}}
      PermissionsBoundary:
        Fn::If:
          - HasPermissionsBoundary
          - {{Ref: PermissionsBoundaryArn}}
          - {{Ref: AWS::NoValue}}
      AssumeRolePolicyDocument:
        Version: "2012-10-17"
        Statement:
          - Sid: TagctlAssumeRole
            Effect: Allow
            Principal:
              AWS: {{Ref: TrustedPrincipalArn}}
            Action: sts:AssumeRole
            Condition:
              Fn::If:
                - HasTrustConditions
                - StringEquals:
                    Fn::If:
                      - HasExternalId
                      - Fn::If:
                          - HasOrgId
                          - sts:ExternalId: {{Ref: ExternalId}}
                            aws:PrincipalOrgID: {{Ref: OrgId}}
                          - sts:ExternalId: {{Ref: ExternalId}}
                      - Fn::If:
                          - HasOrgId
                          - aws:PrincipalOrgID: {{Ref: OrgId}}
                          - {{Ref: AWS::NoValue}}
                  Bool:
                    Fn::If:
                      - MFARequired
                      - aws:MultiFactorAuthPresent: "true"
                      - {{Ref: AWS::NoValue}}
                - {{Ref: AWS::NoValue}}
      Tags:
        - Key: managed-by
          Value: tagctl
{extra_role_properties}      Policies:
"""

APPLY_GROUPS = """      - Label:
          default: What the role may tag
        Parameters:
          - AllowCodeBuildTagging
          - AllowTagRemoval
          - ProtectedTagKeys
"""

APPLY_PARAMETERS = """  AllowCodeBuildTagging:
    Type: String
    Description: >-
      Grant codebuild:UpdateProject. CodeBuild has no tag-only action, and this
      one can also replace a project's buildspec and service role, so it is off
      by default and tagctl apply reports CodeBuild changes as failed.
    Default: "false"
    AllowedValues: ["true", "false"]
  AllowTagRemoval:
    Type: String
    Description: >-
      Grant the untag actions, so tagctl apply can perform the removals of
      rules.rename and policy.forbidden. Off by default: without it apply
      still adds and updates tags and reports every removal as failed.
    Default: "false"
    AllowedValues: ["true", "false"]
  ProtectedTagKeys:
    Type: String
    Description: >-
      Optional. Comma-separated tag keys, wildcards allowed and no spaces
      around the commas, that the role may never add, change or remove, such as
      the keys your ABAC policies check. Adds a Deny on aws:TagKeys.
    Default: ""
"""

APPLY_CONDITIONS = """  CodeBuildTagging:
    Fn::Equals: [{Ref: AllowCodeBuildTagging}, "true"]
  TagRemoval:
    Fn::Equals: [{Ref: AllowTagRemoval}, "true"]
  HasProtectedTagKeys:
    Fn::Not:
      - Fn::Equals: [{Ref: ProtectedTagKeys}, ""]
"""

# Template-only: the denied keys are a deployment parameter, not a JSON file.
PROTECTED_TAG_KEYS_POLICY = """        - Fn::If:
            - HasProtectedTagKeys
            - PolicyName: TagctlProtectedTagKeys
              PolicyDocument:
                Version: "2012-10-17"
                Statement:
                  - Sid: DenyProtectedTagKeys
                    Effect: Deny
                    Action: "*"
                    Resource: "*"
                    Condition:
                      ForAnyValue:StringLike:
                        aws:TagKeys:
                          Fn::Split: [",", {Ref: ProtectedTagKeys}]
            - {Ref: AWS::NoValue}
"""

# A managed policy: the role's inline policies have no room left for it.
APPLY_ROLE_PROPERTIES = """      ManagedPolicyArns:
        Fn::If:
          - TagRemoval
          - [{Ref: UntagPolicy}]
          - {Ref: AWS::NoValue}
"""

UNTAG_POLICY_FILE = "tagctl-apply-untag-policy.json"

UNTAG_POLICY_RESOURCE = """  UntagPolicy:
    Type: AWS::IAM::ManagedPolicy
    Condition: TagRemoval
    Properties:
      Description: Tag removal for tagctl apply (rules.rename and policy.forbidden)
      PolicyDocument:
"""


def untag_policy_resource():
    return UNTAG_POLICY_RESOURCE + policy_yaml(load(UNTAG_POLICY_FILE), 8) + "\n"


FOOTER = """
Outputs:
  RoleArn:
    Description: Value for role_arn in tagctl.yaml (or --role).
    Value: {Fn::GetAtt: [Role, Arn]}
    Export:
      Name: {Fn::Sub: "${AWS::StackName}-RoleArn"}
  TagctlConfig:
    Description: Account entry for clouds.aws in tagctl.yaml.
    Value:
      Fn::Sub: "- profile: <base profile>\\n  role_arn: ${Role.Arn}"
"""

TEMPLATES = [
    {
        "file": "tagctl-scan-role.yaml",
        "description": (
            "tagctl read-only role. Runs scan, plan, diff, normalize, terraform "
            "and cost against this account. It cannot write a tag."
        ),
        "role_name": "TagctlScan",
        "role_description": "Read-only role for tagctl scans",
        "mfa_default": "false",
        "session_default": 3600,
        "extra_groups": "",
        "extra_parameters": "",
        "extra_conditions": "",
        "extra_role_properties": "",
        "policies": [("TagctlScan", "tagctl-scan-policy.json", None)],
        "extra_policies": "",
        "extra_resources": lambda: "",
    },
    {
        "file": "tagctl-apply-role.yaml",
        "description": (
            "tagctl remediation role. Everything the scan role can do, plus "
            "writing tags on the supported resource types, each action scoped "
            "to the ARNs tagctl tags. Assume it only to run tagctl apply."
        ),
        "role_name": "TagctlApply",
        "role_description": "Read-write role for tagctl apply",
        "mfa_default": "true",
        "session_default": 3600,
        "extra_groups": APPLY_GROUPS,
        "extra_parameters": APPLY_PARAMETERS,
        "extra_conditions": APPLY_CONDITIONS,
        "extra_role_properties": APPLY_ROLE_PROPERTIES,
        "policies": [
            ("TagctlScan", "tagctl-scan-policy.json", None),
            ("TagctlApply", "tagctl-apply-policy.json", None),
            ("TagctlApplyCodeBuild", "tagctl-apply-codebuild-policy.json", "CodeBuildTagging"),
        ],
        "extra_policies": PROTECTED_TAG_KEYS_POLICY,
        "extra_resources": untag_policy_resource,
    },
]

HEADER_FIELDS = (
    "description", "role_name", "role_description", "mfa_default", "session_default",
    "extra_groups", "extra_parameters", "extra_conditions", "extra_role_properties",
)


def main():
    for spec in TEMPLATES:
        body = HEADER.format(**{k: spec[k] for k in HEADER_FIELDS})
        for name, source, condition in spec["policies"]:
            body += policy_entry(name, source, condition)
        body += spec["extra_policies"]
        body += spec["extra_resources"]()
        body += FOOTER
        (ROOT / spec["file"]).write_text(body)
        print(f"wrote permissions/aws/{spec['file']}")


if __name__ == "__main__":
    main()
