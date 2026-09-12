# Security Policy

## Supported versions

tagctl has no maintained release branches. Security fixes are made on `main`
and ship in the next release; earlier releases do not receive backports.

| Version | Supported |
| --- | --- |
| `main` and the next release cut from it | Yes |
| Earlier releases | No, upgrade to the latest release |

## Reporting a vulnerability

Do not report vulnerabilities in public issues, pull requests or commit
messages.

Report them through GitHub private vulnerability reporting:
[https://github.com/unicrons/tagctl/security/advisories/new](https://github.com/unicrons/tagctl/security/advisories/new)
(the **Report a vulnerability** button in the repository's Security tab). Only
you and the unicrons maintainers can see the report, and the conversation, the
fix and the advisory stay in that thread until disclosure.

## What to include

- The affected version (`tagctl --version`) or commit.
- The component: a command, the AWS provider, a report writer, or the IAM
  policies and CloudFormation roles under `permissions/aws/`.
- What an attacker can do and what they need first, for example the ability
  to tag a resource, write a plan file or edit `tagctl.yaml`.
- Steps to reproduce or a proof of concept, with private data removed: use
  `123456789012` for account ids and never include real credentials, ARNs or
  scan output from a real account.
- A suggested fix, if you have one.

## What to expect

The maintainers reply in the private report, confirm or decline the issue, and
agree a disclosure date with you. Confirmed issues are fixed on `main` and
published as a GitHub security advisory, crediting you unless you ask not to be
named.

## Out of scope

- Findings tagctl reports about your own cloud resources: those are the
  tool's output, not vulnerabilities in tagctl.
- Vulnerabilities in AWS services or in third-party dependencies that are
  already public. Report them upstream; a pull request bumping the dependency
  is welcome.
