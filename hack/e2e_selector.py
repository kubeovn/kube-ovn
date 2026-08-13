#!/usr/bin/env python3

import argparse
import fnmatch
import hashlib
import html
import itertools
import json
import re
from pathlib import Path


INFRASTRUCTURE_JOBS = {
    "build-kube-ovn-base",
    "build-kube-ovn-dpdk-base",
    "build-kube-ovn",
    "build-vpc-nat-gateway",
    "build-e2e-binaries",
    "netpol-path-filter",
    "e2e-selection-shadow",
    "push",
}


def load_catalog(path):
    with Path(path).open(encoding="utf-8") as stream:
        catalog = json.load(stream)
    validate_catalog(catalog)
    return catalog


def validate_catalog(catalog):
    if catalog.get("schemaVersion") != 1:
        raise ValueError("unsupported catalog schemaVersion")
    groups = catalog.get("groups")
    if not isinstance(groups, dict) or not groups:
        raise ValueError("catalog groups must be a non-empty object")
    known_jobs = set()
    for name, group in groups.items():
        if not re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", name):
            raise ValueError(f"invalid group name {name!r}")
        for job in group.get("jobs", []):
            job_id = job.get("id")
            if not job_id or job_id in known_jobs:
                raise ValueError(f"missing or duplicate job id {job_id!r}")
            known_jobs.add(job_id)
    expanded = expand_all(catalog)
    identities = {matrix_identity(entry) for entry in expanded}
    if not expanded or len(expanded) != len(identities):
        raise ValueError("catalog must define unique x86 E2E runner jobs")
    if len(expanded) != catalog.get("expectedRunnerJobs"):
        raise ValueError("catalog runner count does not match expectedRunnerJobs")
    for smoke in catalog.get("smoke", []):
        if matrix_identity(smoke) not in identities:
            raise ValueError(f"smoke entry is not present in the full matrix: {smoke}")
    for rule in catalog.get("pathRules", []):
        unknown = set(rule.get("groups", [])) - groups.keys()
        if unknown:
            raise ValueError(f"path rule contains unknown groups: {sorted(unknown)}")
        if not rule.get("owner") or not rule.get("reason"):
            raise ValueError("every path rule requires owner and reason")


def workflow_test_jobs(workflow):
    jobs = {
        match.group(1)
        for match in re.finditer(r"^  ([a-z0-9][a-z0-9-]+):\s*$", workflow, re.MULTILINE)
    }
    return jobs - INFRASTRUCTURE_JOBS


def expand_job(job):
    if "include" in job:
        entries = job["include"]
    else:
        matrix = job.get("matrix", {})
        keys = list(matrix)
        values = [matrix[key] for key in keys]
        entries = [dict(zip(keys, combination)) for combination in itertools.product(*values)]
        if not entries:
            entries = [{}]
    return [{"job": job["id"], **entry} for entry in entries]


def expand_groups(catalog, groups):
    return [
        entry
        for group_name in groups
        for job in catalog["groups"][group_name]["jobs"]
        for entry in expand_job(job)
    ]


def expand_all(catalog):
    return expand_groups(catalog, sorted(catalog["groups"]))


def matrix_identity(entry):
    identity = {key: value for key, value in entry.items() if key != "selection"}
    return json.dumps(identity, sort_keys=True, separators=(",", ":"))


def matches(path, pattern):
    return fnmatch.fnmatchcase(path, pattern)


def matched_path_groups(catalog, paths):
    groups = set()
    reasons = []
    classified_paths = set()
    for rule in catalog.get("pathRules", []):
        matched = sorted(
            path for path in paths if any(matches(path, pattern) for pattern in rule["patterns"])
        )
        if not matched:
            continue
        groups.update(rule["groups"])
        classified_paths.update(matched)
        reasons.append(
            {
                "source": "path",
                "groups": sorted(rule["groups"]),
                "paths": matched,
                "owner": rule["owner"],
                "reason": rule["reason"],
            }
        )
    return groups, reasons, classified_paths


def full_reason(catalog, paths, labels, path_groups, classified_paths):
    if catalog.get("forceFull"):
        return "catalog forceFull is enabled"
    if "e2e:full" in labels:
        return "label e2e:full requested the full suite"
    for path in sorted(paths):
        if any(matches(path, pattern) for pattern in catalog.get("fullPaths", [])):
            return f"shared path {path} requires the full suite"
    unknown_production = sorted(
        path
        for path in paths
        if any(path.startswith(prefix) for prefix in catalog.get("productionPrefixes", []))
        and path not in classified_paths
    )
    if unknown_production:
        return f"unclassified production path {unknown_production[0]} requires the full suite"
    threshold = catalog.get("fullThreshold", 3)
    if len(path_groups) >= threshold:
        return f"path selection matched {len(path_groups)} test groups"
    return ""


def catalog_revision(catalog):
    payload = json.dumps(catalog, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(payload).hexdigest()


def deduplicate_matrix(entries):
    result = []
    seen = {}
    for entry in entries:
        key = matrix_identity(entry)
        if key not in seen:
            seen[key] = len(result)
            result.append(entry)
        elif result[seen[key]]["selection"] != entry["selection"]:
            result[seen[key]]["selection"] = "smoke+group"
    return result


def select(catalog, paths, labels, requested_groups, head_sha):
    paths = sorted(set(paths))
    labels = sorted(set(labels))
    requested_groups = sorted(set(requested_groups))
    known_groups = set(catalog["groups"])
    invalid_requests = set(requested_groups) - known_groups
    if invalid_requests:
        raise ValueError(f"unknown requested group: {sorted(invalid_requests)}")

    label_groups = {label.removeprefix("e2e:") for label in labels if label.startswith("e2e:")}
    label_groups.discard("full")
    invalid_labels = label_groups - known_groups
    if invalid_labels:
        raise ValueError(f"unknown label group: {sorted(invalid_labels)}")

    path_groups, reasons, classified_paths = matched_path_groups(catalog, paths)
    selected_groups = path_groups | label_groups | set(requested_groups)
    reasons.extend(
        {"source": "label", "groups": [group], "reason": f"label e2e:{group}"}
        for group in sorted(label_groups)
    )
    reasons.extend(
        {"source": "request", "groups": [group], "reason": f"requested group {group}"}
        for group in requested_groups
    )
    reason = full_reason(catalog, paths, labels, path_groups, classified_paths)
    full = bool(reason)
    effective_groups = sorted(known_groups if full else selected_groups)
    smoke = [{**entry, "selection": "smoke"} for entry in catalog["smoke"]]
    selected = [{**entry, "selection": "group"} for entry in expand_groups(catalog, effective_groups)]
    matrix = deduplicate_matrix(smoke + selected)
    return {
        "schemaVersion": 1,
        "headSHA": head_sha,
        "selectedGroups": effective_groups,
        "matrix": matrix,
        "reasons": reasons,
        "full": full,
        "fullReason": reason,
        "catalogRevision": catalog_revision(catalog),
    }


def render_summary(plan, changed_paths):
    groups = safe_summary_text(", ".join(plan["selectedGroups"]) or "smoke only")
    lines = [
        "## x86 E2E selection shadow report",
        "",
        "> Shadow mode only: the current x86 E2E jobs still run unchanged.",
        "",
        f"- HEAD: `{plan['headSHA']}`",
        f"- Full suite: `{'yes' if plan['full'] else 'no'}`",
        f"- Selected groups: {groups}",
        f"- Proposed runner jobs: {len(plan['matrix'])}",
        f"- Changed paths: {len(changed_paths)}",
    ]
    if plan["fullReason"]:
        lines.append(f"- Full-suite reason: {safe_summary_text(plan['fullReason'])}")
    if plan["reasons"]:
        lines.extend(["", "### Selection reasons", ""])
        for reason in plan["reasons"]:
            source = safe_summary_text(reason["source"])
            explanation = safe_summary_text(reason["reason"])
            lines.append(f"- `{source}`: {explanation}")
    return "\n".join(lines) + "\n"


def safe_summary_text(value):
    printable = "".join(character if character.isprintable() else " " for character in value)
    return html.escape(printable, quote=False)


def event_labels(path):
    if not path:
        return []
    with Path(path).open(encoding="utf-8") as stream:
        event = json.load(stream)
    return [label["name"] for label in event.get("pull_request", {}).get("labels", [])]


def read_paths(path):
    data = Path(path).read_bytes()
    if b"\0" in data:
        return [item.decode("utf-8", errors="surrogateescape") for item in data.split(b"\0") if item]
    return data.decode("utf-8").splitlines()


def parse_args():
    parser = argparse.ArgumentParser(description="Compute the x86 E2E selection plan")
    parser.add_argument("--catalog", default=".github/e2e-selection.json")
    parser.add_argument("--paths-file", required=True)
    parser.add_argument("--event-file")
    parser.add_argument("--label", action="append", default=[])
    parser.add_argument("--request-group", action="append", default=[])
    parser.add_argument("--head-sha", required=True)
    parser.add_argument("--plan-file", required=True)
    parser.add_argument("--summary-file")
    return parser.parse_args()


def main():
    args = parse_args()
    catalog = load_catalog(args.catalog)
    paths = read_paths(args.paths_file)
    labels = args.label + event_labels(args.event_file)
    plan = select(catalog, paths, labels, args.request_group, args.head_sha)
    Path(args.plan_file).write_text(json.dumps(plan, indent=2) + "\n", encoding="utf-8")
    if args.summary_file:
        Path(args.summary_file).write_text(render_summary(plan, paths), encoding="utf-8")


if __name__ == "__main__":
    main()
