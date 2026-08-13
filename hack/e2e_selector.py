#!/usr/bin/env python3

import argparse
import fnmatch
import hashlib
import itertools
import json
import re
from pathlib import Path


infrastructureJobs = {
    "build-kube-ovn-base",
    "build-kube-ovn-dpdk-base",
    "build-kube-ovn",
    "build-vpc-nat-gateway",
    "build-e2e-binaries",
    "netpol-path-filter",
    "e2e-selection-shadow",
    "push",
}


def loadCatalog(path):
    with Path(path).open(encoding="utf-8") as stream:
        catalog = json.load(stream)
    validateCatalog(catalog)
    return catalog


def validateCatalog(catalog):
    if catalog.get("schemaVersion") != 1:
        raise ValueError("unsupported catalog schemaVersion")
    groups = catalog.get("groups")
    if not isinstance(groups, dict) or not groups:
        raise ValueError("catalog groups must be a non-empty object")
    knownJobs = set()
    for name, group in groups.items():
        if not re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", name):
            raise ValueError(f"invalid group name {name!r}")
        for job in group.get("jobs", []):
            jobId = job.get("id")
            if not jobId or jobId in knownJobs:
                raise ValueError(f"missing or duplicate job id {jobId!r}")
            knownJobs.add(jobId)
    expanded = expandAll(catalog)
    identities = {matrixIdentity(entry) for entry in expanded}
    if not expanded or len(expanded) != len(identities):
        raise ValueError("catalog must define unique x86 E2E runner jobs")
    if len(expanded) != catalog.get("expectedRunnerJobs"):
        raise ValueError("catalog runner count does not match expectedRunnerJobs")
    for smoke in catalog.get("smoke", []):
        if matrixIdentity(smoke) not in identities:
            raise ValueError(f"smoke entry is not present in the full matrix: {smoke}")
    for rule in catalog.get("pathRules", []):
        unknown = set(rule.get("groups", [])) - groups.keys()
        if unknown:
            raise ValueError(f"path rule contains unknown groups: {sorted(unknown)}")
        if not rule.get("owner") or not rule.get("reason"):
            raise ValueError("every path rule requires owner and reason")


def workflowTestJobs(workflow):
    jobs = {
        match.group(1)
        for match in re.finditer(r"^  ([a-z0-9][a-z0-9-]+):\s*$", workflow, re.MULTILINE)
    }
    return jobs - infrastructureJobs


def yamlScalar(value):
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
        return value[1:-1]
    if value.isdigit():
        return int(value)
    return value


def workflowJobBlocks(workflow):
    matches = list(re.finditer(r"^  ([a-z0-9][a-z0-9-]+):\s*$", workflow, re.MULTILINE))
    return {
        match.group(1): workflow[match.end() : matches[index + 1].start() if index + 1 < len(matches) else None]
        for index, match in enumerate(matches)
    }


def workflowJobMatrix(jobId, block):
    lines = block.splitlines()
    try:
        start = lines.index("      matrix:") + 1
    except ValueError:
        return [{"job": jobId}]
    matrixLines = []
    for line in lines[start:]:
        if line.strip() and len(line) - len(line.lstrip()) <= 6:
            break
        matrixLines.append(line)
    if any(line == "        include:" for line in matrixLines):
        entries = []
        current = None
        for line in matrixLines:
            match = re.fullmatch(r"          - ([a-z0-9-]+): (.+)", line)
            if match:
                current = {match.group(1): yamlScalar(match.group(2))}
                entries.append(current)
                continue
            match = re.fullmatch(r"            ([a-z0-9-]+): (.+)", line)
            if match and current is not None:
                current[match.group(1)] = yamlScalar(match.group(2))
        return [{"job": jobId, **entry} for entry in entries]
    matrix = {}
    currentKey = None
    for line in matrixLines:
        match = re.fullmatch(r"        ([a-z0-9-]+):", line)
        if match:
            currentKey = match.group(1)
            matrix[currentKey] = []
            continue
        match = re.fullmatch(r"          - (.+)", line)
        if match and currentKey is not None:
            matrix[currentKey].append(yamlScalar(match.group(1)))
    return expandJob({"id": jobId, "matrix": matrix})


def validateWorkflow(catalog, workflow):
    blocks = workflowJobBlocks(workflow)
    workflowJobs = workflowTestJobs(workflow)
    catalogJobs = {
        job["id"]
        for group in catalog["groups"].values()
        for job in group["jobs"]
    }
    if workflowJobs != catalogJobs:
        raise ValueError("catalog jobs do not match the x86 workflow")
    workflowEntries = [
        entry
        for jobId in sorted(workflowJobs)
        for entry in workflowJobMatrix(jobId, blocks[jobId])
    ]
    workflowIdentities = {matrixIdentity(entry) for entry in workflowEntries}
    catalogIdentities = {matrixIdentity(entry) for entry in expandAll(catalog)}
    if workflowIdentities != catalogIdentities:
        raise ValueError("catalog matrix does not match the x86 workflow")


def expandJob(job):
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


def expandGroups(catalog, groups):
    return [
        entry
        for groupName in groups
        for job in catalog["groups"][groupName]["jobs"]
        for entry in expandJob(job)
    ]


def expandAll(catalog):
    return expandGroups(catalog, sorted(catalog["groups"]))


def matrixIdentity(entry):
    identity = {key: value for key, value in entry.items() if key != "selection"}
    return json.dumps(identity, sort_keys=True, separators=(",", ":"))


def matches(path, pattern):
    return fnmatch.fnmatchcase(path, pattern)


def matchedPathGroups(catalog, paths):
    groups = set()
    reasons = []
    classifiedPaths = set()
    for rule in catalog.get("pathRules", []):
        matched = sorted(
            path for path in paths if any(matches(path, pattern) for pattern in rule["patterns"])
        )
        if not matched:
            continue
        groups.update(rule["groups"])
        classifiedPaths.update(matched)
        reasons.append(
            {
                "source": "path",
                "groups": sorted(rule["groups"]),
                "paths": matched,
                "owner": rule["owner"],
                "reason": rule["reason"],
            }
        )
    return groups, reasons, classifiedPaths


def fullReason(catalog, paths, labels, pathGroups, classifiedPaths):
    if catalog.get("forceFull"):
        return "catalog forceFull is enabled"
    if "e2e:full" in labels:
        return "label e2e:full requested the full suite"
    for path in sorted(paths):
        if any(matches(path, pattern) for pattern in catalog.get("fullPaths", [])):
            return f"shared path {path} requires the full suite"
    unknownProduction = sorted(
        path
        for path in paths
        if any(path.startswith(prefix) for prefix in catalog.get("productionPrefixes", []))
        and path not in classifiedPaths
    )
    if unknownProduction:
        return f"unclassified production path {unknownProduction[0]} requires the full suite"
    threshold = catalog.get("fullThreshold", 3)
    if len(pathGroups) >= threshold:
        return f"path selection matched {len(pathGroups)} test groups"
    return ""


def catalogRevision(catalog):
    payload = json.dumps(catalog, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(payload).hexdigest()


def deduplicateMatrix(entries):
    result = []
    seen = {}
    for entry in entries:
        key = matrixIdentity(entry)
        if key not in seen:
            seen[key] = len(result)
            result.append(entry)
        elif result[seen[key]]["selection"] != entry["selection"]:
            result[seen[key]]["selection"] = "smoke+group"
    return result


def select(catalog, paths, labels, requestedGroups, headSHA):
    paths = sorted(set(paths))
    labels = sorted(set(labels))
    requestedGroups = sorted(set(requestedGroups))
    knownGroups = set(catalog["groups"])
    invalidRequests = set(requestedGroups) - knownGroups
    if invalidRequests:
        raise ValueError(f"unknown requested group: {sorted(invalidRequests)}")

    labelGroups = {label.removeprefix("e2e:") for label in labels if label.startswith("e2e:")}
    labelGroups.discard("full")
    invalidLabels = labelGroups - knownGroups
    if invalidLabels:
        raise ValueError(f"unknown label group: {sorted(invalidLabels)}")

    pathGroups, reasons, classifiedPaths = matchedPathGroups(catalog, paths)
    selectedGroups = pathGroups | labelGroups | set(requestedGroups)
    reasons.extend(
        {"source": "label", "groups": [group], "reason": f"label e2e:{group}"}
        for group in sorted(labelGroups)
    )
    reasons.extend(
        {"source": "request", "groups": [group], "reason": f"requested group {group}"}
        for group in requestedGroups
    )
    reason = fullReason(catalog, paths, labels, pathGroups, classifiedPaths)
    full = bool(reason)
    effectiveGroups = sorted(knownGroups if full else selectedGroups)
    smoke = [{**entry, "selection": "smoke"} for entry in catalog["smoke"]]
    selected = [{**entry, "selection": "group"} for entry in expandGroups(catalog, effectiveGroups)]
    matrix = deduplicateMatrix(smoke + selected)
    return {
        "schemaVersion": 1,
        "headSHA": headSHA,
        "selectedGroups": effectiveGroups,
        "matrix": matrix,
        "reasons": reasons,
        "full": full,
        "fullReason": reason,
        "catalogRevision": catalogRevision(catalog),
    }


def renderSummary(plan, changedPaths):
    groups = safeSummaryText(", ".join(plan["selectedGroups"]) or "smoke only")
    lines = [
        "## x86 E2E selection shadow report",
        "",
        "> Shadow mode only: the current x86 E2E jobs still run unchanged.",
        "",
        f"- HEAD: `{plan['headSHA']}`",
        f"- Full suite: `{'yes' if plan['full'] else 'no'}`",
        f"- Selected groups: {groups}",
        f"- Proposed runner jobs: {len(plan['matrix'])}",
        f"- Changed paths: {len(changedPaths)}",
    ]
    if plan["fullReason"]:
        lines.append(f"- Full-suite reason: {safeSummaryText(plan['fullReason'])}")
    if plan["reasons"]:
        lines.extend(["", "### Selection reasons", ""])
        for reason in plan["reasons"]:
            source = safeSummaryText(reason["source"])
            explanation = safeSummaryText(reason["reason"])
            lines.append(f"- `{source}`: {explanation}")
    return "\n".join(lines) + "\n"


def fallbackPlan(headSHA, error, workflow):
    reason = f"catalog error requires the full suite: {error}"
    blocks = workflowJobBlocks(workflow)
    jobs = workflowTestJobs(workflow)
    matrix = [
        {**entry, "selection": "group"}
        for jobId in sorted(jobs)
        for entry in workflowJobMatrix(jobId, blocks[jobId])
    ]
    return {
        "schemaVersion": 1,
        "headSHA": headSHA,
        "selectedGroups": [],
        "matrix": matrix,
        "reasons": [{"source": "catalog", "groups": [], "reason": reason}],
        "full": True,
        "fullReason": reason,
        "catalogRevision": "",
    }


def safeSummaryText(value):
    printable = "".join(character if character.isprintable() else " " for character in value)
    return "".join(
        character
        if character.isalnum() or character in " /_-"
        else f"&#{ord(character)};"
        for character in printable
    )


def eventLabels(path):
    if not path:
        return []
    with Path(path).open(encoding="utf-8") as stream:
        event = json.load(stream)
    return [label["name"] for label in event.get("pull_request", {}).get("labels", [])]


def readPaths(path):
    data = Path(path).read_bytes()
    if b"\0" in data:
        return [item.decode("utf-8", errors="surrogateescape") for item in data.split(b"\0") if item]
    return data.decode("utf-8").splitlines()


def parseArgs():
    parser = argparse.ArgumentParser(description="Compute the x86 E2E selection plan")
    parser.add_argument("--catalog", default=".github/e2e-selection.json")
    parser.add_argument("--workflow", default=".github/workflows/build-x86-image.yaml")
    parser.add_argument("--paths-file", dest="pathsFile", required=True)
    parser.add_argument("--event-file", dest="eventFile")
    parser.add_argument("--label", action="append", default=[])
    parser.add_argument("--request-group", dest="requestGroup", action="append", default=[])
    parser.add_argument("--head-sha", dest="headSHA", required=True)
    parser.add_argument("--plan-file", dest="planFile", required=True)
    parser.add_argument("--summary-file", dest="summaryFile")
    return parser.parse_args()


def main():
    args = parseArgs()
    paths = readPaths(args.pathsFile)
    try:
        catalog = loadCatalog(args.catalog)
    except (OSError, UnicodeError, json.JSONDecodeError, ValueError) as error:
        workflow = Path(args.workflow).read_text(encoding="utf-8")
        plan = fallbackPlan(args.headSHA, error, workflow)
    else:
        labels = args.label + eventLabels(args.eventFile)
        plan = select(catalog, paths, labels, args.requestGroup, args.headSHA)
    Path(args.planFile).write_text(json.dumps(plan, indent=2) + "\n", encoding="utf-8")
    if args.summaryFile:
        Path(args.summaryFile).write_text(renderSummary(plan, paths), encoding="utf-8")


if __name__ == "__main__":
    main()
