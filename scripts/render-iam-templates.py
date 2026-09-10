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
        for a in st["Action"]:
            out.append(f"{pad}      - {a}")
        out.append(f'{pad}    Resource: "{st["Resource"]}"')
    return "\n".join(out)


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
      - Label:
          default: Role settings
        Parameters:
          - RoleName
          - MaxSessionDuration
          - PermissionsBoundaryArn

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

Conditions:
  HasExternalId:
    Fn::Not:
      - Fn::Equals: [{{Ref: ExternalId}}, ""]
  MFARequired:
    Fn::Equals: [{{Ref: RequireMFA}}, "true"]
  HasTrustConditions:
    Fn::Or: [{{Condition: HasExternalId}}, {{Condition: MFARequired}}]
  HasPermissionsBoundary:
    Fn::Not:
      - Fn::Equals: [{{Ref: PermissionsBoundaryArn}}, ""]

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
                      - sts:ExternalId: {{Ref: ExternalId}}
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
      Policies:
"""

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
        "policies": [("TagctlScan", "tagctl-scan-policy.json")],
    },
    {
        "file": "tagctl-apply-role.yaml",
        "description": (
            "tagctl remediation role. Everything the scan role can do, plus "
            "writing tags on the 106 supported resource types. Assume it only "
            "to run tagctl apply."
        ),
        "role_name": "TagctlApply",
        "role_description": "Read-write role for tagctl apply",
        "mfa_default": "true",
        "session_default": 3600,
        "policies": [
            ("TagctlScan", "tagctl-scan-policy.json"),
            ("TagctlApply", "tagctl-apply-policy.json"),
        ],
    },
]


def main():
    for spec in TEMPLATES:
        body = HEADER.format(**{k: v for k, v in spec.items() if k not in ("file", "policies")})
        for name, source in spec["policies"]:
            body += f"        - PolicyName: {name}\n          PolicyDocument:\n"
            body += policy_yaml(load(source), 12) + "\n"
        body += FOOTER
        (ROOT / spec["file"]).write_text(body)
        print(f"wrote permissions/aws/{spec['file']}")


if __name__ == "__main__":
    main()
