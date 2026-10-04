#!/usr/bin/env python3
"""Check permissions/aws/*-policy.json against the AWS service reference.

Every action must exist, and every scoped ARN pattern must match a resource
type one of the statement's actions of that service accepts. Needs network
access to servicereference.us-east-1.amazonaws.com, the machine-readable
Service Authorization Reference, so it is run by hand (make iam-check) and
not by the tests.
"""
import fnmatch
import json
import pathlib
import re
import sys
import urllib.error
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent / "permissions" / "aws"
REFERENCE = "https://servicereference.us-east-1.amazonaws.com/v1/{0}/{0}.json"

_services = {}


def reference(prefix):
    if prefix not in _services:
        try:
            with urllib.request.urlopen(REFERENCE.format(prefix), timeout=30) as response:
                doc = json.load(response)
        except urllib.error.HTTPError:
            doc = {"Actions": []}
        _services[prefix] = {
            "actions": {a["Name"]: [r["Name"] for r in a.get("Resources", [])] for a in doc["Actions"]},
            "formats": {r["Name"]: [arn_glob(f) for f in r["ARNFormats"]] for r in doc.get("Resources", [])},
        }
    return _services[prefix]


def arn_glob(arn_format):
    """Turn arn:${Partition}:rds:${Region}:${Account}:db:${Name} into a sample aws ARN."""
    return re.sub(r"\$\{[^}]+\}", "x", arn_format).replace("arn:x:", "arn:aws:", 1)


def check(path):
    problems = []
    for statement in json.loads(path.read_text())["Statement"]:
        by_service = {}
        for action in statement["Action"]:
            prefix, name = action.split(":")
            if name not in reference(prefix)["actions"]:
                problems.append(f"{path.name}/{statement['Sid']}: {action} is not in the service reference")
                continue
            by_service.setdefault(prefix, []).append(name)

        resources = statement["Resource"]
        if statement["Effect"] != "Allow" or isinstance(resources, str):
            continue
        for pattern in resources:
            prefix = pattern.split(":")[2]
            ref = reference(prefix)
            accepted = {r for name in by_service.get(prefix, []) for r in ref["actions"][name]}
            if not any(fnmatch.fnmatchcase(sample, pattern) for r in accepted for sample in ref["formats"].get(r, [])):
                problems.append(f"{path.name}/{statement['Sid']}: no action of {prefix} accepts {pattern}")
    return problems


def main():
    files = sorted(ROOT.glob("*-policy.json"))
    problems = [p for f in files for p in check(f)]
    for problem in problems:
        print(problem)
    actions = sum(len(s["Action"]) for f in files for s in json.loads(f.read_text())["Statement"])
    print(f"checked {actions} actions in {len(files)} policies against {len(_services)} services: {len(problems)} problem(s)")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
