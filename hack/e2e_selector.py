#!/usr/bin/env python3

import argparse
import collections
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
    "e2e-selection",
    "e2e-executor-result",
    "push",
}
expectedX86RunnerJobs = 82
mandatorySmoke = [
    {"job": "kube-ovn-conformance-e2e", "ip-family": "ipv4", "mode": "overlay"},
    {"job": "kube-ovn-conformance-e2e", "ip-family": "ipv4", "mode": "underlay"},
    {"job": "k8s-conformance-e2e", "ip-family": "ipv4", "mode": "overlay"},
]
dynamicWorkflowMatrices = {
    "k8s-conformance-e2e": {
        "output": "k8sConformanceMatrix",
        "matrix": {"ip-family": ["ipv4", "ipv6", "dual"], "mode": ["overlay", "underlay"]},
    },
    "kube-ovn-conformance-e2e": {
        "output": "kubeOvnConformanceMatrix",
        "matrix": {"ip-family": ["ipv4", "ipv6", "dual"], "mode": ["overlay", "underlay"]},
    },
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
        if not isinstance(group, dict) or not isinstance(group.get("jobs"), list):
            raise ValueError(f"catalog group {name!r} must define a jobs array")
        for job in group["jobs"]:
            if not isinstance(job, dict):
                raise ValueError(f"catalog group {name!r} contains an invalid job")
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
    smokeEntries = catalog.get("smoke")
    if smokeEntries != mandatorySmoke:
        raise ValueError("catalog smoke must contain the mandatory three runner jobs")
    for smoke in smokeEntries:
        if matrixIdentity(smoke) not in identities:
            raise ValueError(f"smoke entry is not present in the full matrix: {smoke}")
    for rule in catalog.get("pathRules", []):
        unknown = set(rule.get("groups", [])) - groups.keys()
        if unknown:
            raise ValueError(f"path rule contains unknown groups: {sorted(unknown)}")
        if not rule.get("owner") or not rule.get("reason"):
            raise ValueError("every path rule requires owner and reason")


def workflowJobsSection(workflow):
    match = re.search(r"^jobs:\s*$", workflow, re.MULTILINE)
    if match is None:
        raise ValueError("workflow does not define jobs")
    return workflow[match.end() :]


def workflowTestJobs(workflow):
    jobs = {
        match.group(1)
        for match in re.finditer(
            r"^  ([a-z0-9][a-z0-9-]+):\s*$",
            workflowJobsSection(workflow),
            re.MULTILINE,
        )
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
    jobsSection = workflowJobsSection(workflow)
    matches = list(
        re.finditer(r"^  ([a-z0-9][a-z0-9-]+):\s*$", jobsSection, re.MULTILINE)
    )
    return {
        match.group(1): jobsSection[
            match.end() : matches[index + 1].start()
            if index + 1 < len(matches)
            else None
        ]
        for index, match in enumerate(matches)
    }


def workflowJobMatrix(jobId, block):
    lines = block.splitlines()
    dynamic = dynamicWorkflowMatrices.get(jobId)
    if dynamic is not None:
        expression = (
            "      matrix: ${{ fromJSON(needs.e2e-selection.outputs."
            + dynamic["output"]
            + ") }}"
        )
        if expression in lines:
            return expandJob({"id": jobId, "matrix": dynamic["matrix"]})
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
        match = re.fullmatch(r"        ([a-z0-9-]+): \[(.*)\]", line)
        if match:
            currentKey = None
            matrix[match.group(1)] = [
                yamlScalar(value) for value in match.group(2).split(",") if value.strip()
            ]
            continue
        match = re.fullmatch(r"        ([a-z0-9-]+):", line)
        if match:
            currentKey = match.group(1)
            matrix[currentKey] = []
            continue
        match = re.fullmatch(r"          - (.+)", line)
        if match and currentKey is not None:
            matrix[currentKey].append(yamlScalar(match.group(1)))
    return expandJob({"id": jobId, "matrix": matrix})


def expandWorkflow(workflow):
    blocks = workflowJobBlocks(workflow)
    jobs = workflowTestJobs(workflow)
    return [
        entry
        for jobId in sorted(jobs)
        for entry in workflowJobMatrix(jobId, blocks[jobId])
    ]


def validateWorkflow(catalog, workflow):
    workflowJobs = workflowTestJobs(workflow)
    catalogJobs = {
        job["id"]
        for group in catalog["groups"].values()
        for job in group["jobs"]
    }
    if workflowJobs != catalogJobs:
        raise ValueError("catalog jobs do not match the x86 workflow")
    workflowEntries = expandWorkflow(workflow)
    workflowIdentities = collections.Counter(matrixIdentity(entry) for entry in workflowEntries)
    catalogIdentities = collections.Counter(matrixIdentity(entry) for entry in expandAll(catalog))
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


def jobMatrix(plan, jobId):
    entries = [
        {
            key: value
            for key, value in entry.items()
            if key not in {"job", "selection"}
        }
        for entry in plan["matrix"]
        if entry["job"] == jobId
    ]
    if not entries:
        raise ValueError(f"selection plan does not contain job {jobId!r}")
    return {"include": entries}


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


def select(catalog, paths, labels, requestedGroups, headSHA, forcedFullReason=""):
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
    reason = forcedFullReason or fullReason(
        catalog,
        paths,
        labels,
        pathGroups,
        classifiedPaths,
    )
    full = bool(reason)
    recommendedGroups = sorted(pathGroups | labelGroups) if not full else []
    automaticGroups = sorted(knownGroups) if full else []
    effectiveGroups = sorted(knownGroups if full else selectedGroups)
    smoke = [{**entry, "selection": "smoke"} for entry in catalog["smoke"]]
    selected = [{**entry, "selection": "group"} for entry in expandGroups(catalog, effectiveGroups)]
    matrix = deduplicateMatrix(smoke + selected)
    return {
        "schemaVersion": 1,
        "headSHA": headSHA,
        "automaticGroups": automaticGroups,
        "recommendedGroups": recommendedGroups,
        "requestedGroups": requestedGroups,
        "selectedGroups": effectiveGroups,
        "matrix": matrix,
        "reasons": reasons,
        "full": full,
        "approvalRequired": bool(recommendedGroups),
        "fullReason": reason,
        "catalogRevision": catalogRevision(catalog),
    }


def renderSummary(plan, changedPaths):
    groups = safeSummaryText(", ".join(plan["selectedGroups"]) or "smoke only")
    recommended = safeSummaryText(", ".join(plan["recommendedGroups"]) or "none")
    requested = safeSummaryText(", ".join(plan["requestedGroups"]) or "none")
    automatic = (
        f"full suite ({len(plan['matrix'])} runner jobs)"
        if plan["full"]
        else f"mandatory smoke ({len(mandatorySmoke)} runner jobs)"
    )
    lines = [
        "## x86 E2E selection shadow report",
        "",
        "> Shadow mode only: the current x86 E2E jobs still run unchanged.",
        "",
        f"- HEAD: `{plan['headSHA']}`",
        f"- Full suite: `{'yes' if plan['full'] else 'no'}`",
        f"- Automatic coverage: {safeSummaryText(automatic)}",
        f"- Recommended deferred groups: {recommended}",
        f"- Requested groups: {requested}",
        f"- Approval required: `{'yes' if plan['approvalRequired'] else 'no'}`",
        f"- Selected groups: {groups}",
        f"- Proposed runner jobs: {len(plan['matrix'])}",
        f"- Changed paths: {len(changedPaths)}",
    ]
    if plan["fullReason"]:
        lines.append(f"- Full-suite reason: {safeSummaryText(plan['fullReason'])}")
    if plan["approvalRequired"]:
        commandGroups = safeSummaryText(",".join(plan["recommendedGroups"]))
        lines.extend(
            [
                "- Waiting for: `/test e2e`",
                f"- Alternative: `/test e2e {commandGroups}`",
            ]
        )
    if plan["reasons"]:
        lines.extend(["", "### Selection reasons", ""])
        for reason in plan["reasons"]:
            source = safeSummaryText(reason["source"])
            explanation = safeSummaryText(reason["reason"])
            lines.append(f"- `{source}`: {explanation}")
    return "\n".join(lines) + "\n"


def fallbackPlan(headSHA, error, workflow, source):
    reason = f"{source} error requires the full suite: {error}"
    matrix = [
        {**entry, "selection": "group"}
        for entry in expandWorkflow(workflow)
    ]
    identities = {matrixIdentity(entry) for entry in matrix}
    if len(matrix) != expectedX86RunnerJobs or len(identities) != len(matrix):
        raise ValueError(
            "workflow fallback matrix must contain exactly "
            f"{expectedX86RunnerJobs} unique x86 E2E runner jobs"
        )
    return {
        "schemaVersion": 1,
        "headSHA": headSHA,
        "automaticGroups": [],
        "recommendedGroups": [],
        "requestedGroups": [],
        "selectedGroups": [],
        "matrix": matrix,
        "reasons": [{"source": source, "groups": [], "reason": reason}],
        "full": True,
        "approvalRequired": False,
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
    pullRequest = event.get("pull_request") if isinstance(event, dict) else None
    labels = pullRequest.get("labels") if isinstance(pullRequest, dict) else None
    if not isinstance(labels, list):
        raise ValueError("event pull_request labels must be an array")
    if any(not isinstance(label, dict) or not isinstance(label.get("name"), str) for label in labels):
        raise ValueError("event labels must contain string names")
    return [label["name"] for label in labels]


def changedPathsFromNameStatus(data):
    if not data:
        return []
    if not data.endswith(b"\0"):
        raise ValueError("git name-status output must be NUL terminated")
    fields = data[:-1].split(b"\0")
    paths = []
    index = 0
    while index < len(fields):
        status = fields[index]
        index += 1
        if not re.fullmatch(rb"[ACDMRT](?:[0-9]+)?", status):
            raise ValueError("invalid git name-status record")
        pathCount = 2 if status.startswith((b"R", b"C")) else 1
        if index + pathCount > len(fields):
            raise ValueError("incomplete git name-status record")
        paths.extend(
            field.decode("utf-8", errors="surrogateescape")
            for field in fields[index : index + pathCount]
        )
        index += pathCount
    return paths


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
    parser.add_argument("--request-groups-json", dest="requestGroupsJSON", default="[]")
    parser.add_argument("--head-sha", dest="headSHA", required=True)
    parser.add_argument("--force-full-reason", dest="forceFullReason", default="")
    parser.add_argument("--plan-file", dest="planFile", required=True)
    parser.add_argument("--summary-file", dest="summaryFile")
    return parser.parse_args()


def main():
    args = parseArgs()
    paths = []
    try:
        paths = readPaths(args.pathsFile)
    except Exception as error:
        workflow = Path(args.workflow).read_text(encoding="utf-8")
        plan = fallbackPlan(args.headSHA, error, workflow, "selection")
        writePlan(plan, paths, args.planFile, args.summaryFile)
        return
    try:
        catalog = loadCatalog(args.catalog)
    except Exception as error:
        workflow = Path(args.workflow).read_text(encoding="utf-8")
        plan = fallbackPlan(args.headSHA, error, workflow, "catalog")
    else:
        try:
            labels = args.label + eventLabels(args.eventFile)
            requestedGroups = json.loads(args.requestGroupsJSON)
            if not isinstance(requestedGroups, list) or any(
                not isinstance(group, str) for group in requestedGroups
            ):
                raise ValueError("request groups JSON must be an array of strings")
            plan = select(
                catalog,
                paths,
                labels,
                args.requestGroup + requestedGroups,
                args.headSHA,
                args.forceFullReason,
            )
        except Exception as error:
            workflow = Path(args.workflow).read_text(encoding="utf-8")
            plan = fallbackPlan(args.headSHA, error, workflow, "selection")
    writePlan(plan, paths, args.planFile, args.summaryFile)


def writePlan(plan, paths, planFile, summaryFile):
    Path(planFile).write_text(json.dumps(plan, indent=2) + "\n", encoding="utf-8")
    if summaryFile:
        Path(summaryFile).write_text(renderSummary(plan, paths), encoding="utf-8")


if __name__ == "__main__":
    main()
