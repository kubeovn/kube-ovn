#!/usr/bin/env python3

import argparse
import hashlib
import json
import re
from pathlib import Path

import e2e_selector as e2eSelector


groupPattern = r"[a-z0-9]+(?:-[a-z0-9]+)*"


def parseCommand(body):
    body = body.strip()
    if not body.startswith("/test e2e") and body != "/retest e2e-failed":
        return None
    if body == "/test e2e":
        return {"action": "dispatch", "requestedGroups": [], "full": False}
    if body == "/test e2e-all":
        return {"action": "dispatch", "requestedGroups": [], "full": True}
    if body == "/retest e2e-failed":
        return {"action": "rerun-failed", "requestedGroups": [], "full": False}
    match = re.fullmatch(rf"/test e2e ({groupPattern}(?:,{groupPattern})*)", body)
    if not match:
        raise ValueError("invalid E2E command")
    return {
        "action": "dispatch",
        "requestedGroups": sorted(set(match.group(1).split(","))),
        "full": False,
    }


def rejectedDecision(command, reason):
    return {
        "accepted": False,
        "action": command["action"],
        "requestedGroups": command["requestedGroups"],
        "full": command["full"],
        "reason": reason,
    }


def decideDispatch(
    event,
    permission,
    pullRequest,
    confirmedHeadSHA,
    knownGroups,
    catalogRevision,
):
    body = event.get("comment", {}).get("body", "")
    try:
        command = parseCommand(body)
    except ValueError as error:
        return rejectedDecision(
            {"action": "reject", "requestedGroups": [], "full": False},
            str(error),
        )
    if command is None:
        return None
    unknownGroups = sorted(set(command["requestedGroups"]) - set(knownGroups))
    if unknownGroups:
        return rejectedDecision(
            command,
            f"unknown requested E2E group: {unknownGroups[0]}",
        )
    if event.get("action") != "created" or "pull_request" not in event.get("issue", {}):
        return rejectedDecision(command, "comment is not a newly created pull request comment")
    if pullRequest.get("state") != "open":
        return rejectedDecision(command, "pull request is not open")
    if permission not in {"admin", "maintain", "write"}:
        return rejectedDecision(command, "repository write permission is required")
    headSHA = pullRequest.get("head", {}).get("sha")
    if not re.fullmatch(r"[0-9a-f]{40}", headSHA or ""):
        return rejectedDecision(command, "pull request HEAD is invalid")
    if confirmedHeadSHA != headSHA:
        return rejectedDecision(command, "pull request HEAD changed; comment again")
    baseRef = pullRequest.get("base", {}).get("ref")
    if baseRef != "master" and not re.fullmatch(r"release-[A-Za-z0-9._-]+", baseRef or ""):
        return rejectedDecision(command, "pull request base branch is not supported")
    prNumber = pullRequest.get("number")
    payload = {
        "repository": event.get("repository", {}).get("full_name"),
        "prNumber": prNumber,
        "headSHA": headSHA,
        "action": command["action"],
        "requestedGroups": command["requestedGroups"],
        "full": command["full"],
        "catalogRevision": catalogRevision,
    }
    requestKey = hashlib.sha256(
        json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
    ).hexdigest()
    return {
        **command,
        "accepted": True,
        "reason": "",
        "prNumber": prNumber,
        "headSHA": headSHA,
        "baseRef": baseRef,
        "catalogRevision": catalogRevision,
        "requestKey": requestKey,
    }


def executorRequestKey(request):
    payload = {
        "prNumber": request["prNumber"],
        "headSHA": request["headSHA"],
        "action": "dispatch",
        "catalogRevision": request["catalogRevision"],
        "requestedGroups": request["requestedGroups"],
        "full": request["full"],
    }
    return hashlib.sha256(
        json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
    ).hexdigest()


def mergeApprovedRequests(decision, priorRequests):
    merged = dict(decision)
    groups = set(decision["requestedGroups"])
    full = decision["full"]
    for request in priorRequests:
        if request.get("headSHA") != decision["headSHA"]:
            continue
        groups.update(request.get("requestedGroups", []))
        full = full or request.get("full", False)
    merged["requestedGroups"] = sorted(groups)
    merged["full"] = full
    merged["requestKey"] = executorRequestKey(merged)
    return merged


def renderRequestMarker(request):
    payload = {
        "headSHA": request["headSHA"],
        "requestedGroups": request["requestedGroups"],
        "full": request["full"],
    }
    return "<!-- x86-e2e-request " + json.dumps(
        payload, sort_keys=True, separators=(",", ":")
    ) + " -->"


def parseRequestMarker(body):
    match = re.search(r"<!-- x86-e2e-request (\{[^\n]*\}) -->", body)
    if not match:
        return None
    request = json.loads(match.group(1))
    headSHA = request.get("headSHA")
    groups = request.get("requestedGroups")
    full = request.get("full")
    if not re.fullmatch(r"[0-9a-f]{40}", headSHA or ""):
        raise ValueError("invalid request marker HEAD")
    if not isinstance(groups, list) or any(
        not isinstance(group, str) or not re.fullmatch(groupPattern, group)
        for group in groups
    ):
        raise ValueError("invalid request marker groups")
    if not isinstance(full, bool):
        raise ValueError("invalid request marker full value")
    return {"headSHA": headSHA, "requestedGroups": sorted(set(groups)), "full": full}


def evaluateGate(
    plan,
    currentHeadSHA,
    runConclusion,
    approved,
    expectedCatalogRevision=None,
    executedPlan=None,
):
    if plan.get("headSHA") != currentHeadSHA:
        return {
            "update": False,
            "conclusion": "",
            "summary": "",
            "reason": "workflow result belongs to a stale pull request HEAD",
        }
    if (
        expectedCatalogRevision is not None
        and plan.get("catalogRevision") != expectedCatalogRevision
    ):
        return {
            "update": True,
            "conclusion": "failure",
            "summary": "The executor and gate used incompatible E2E catalog revisions.",
            "reason": "",
        }
    if runConclusion != "success":
        return {
            "update": True,
            "conclusion": "failure",
            "summary": f"The x86 E2E workflow concluded with {runConclusion}.",
            "reason": "",
        }
    if executedPlan is None:
        return {
            "update": True,
            "conclusion": "failure",
            "summary": "The successful x86 E2E workflow did not publish its SelectionPlan.",
            "reason": "",
        }
    if executedPlan != plan:
        return {
            "update": True,
            "conclusion": "action_required",
            "summary": "The x86 E2E selection plan changed; start a new execution for the current plan.",
            "reason": "",
        }
    if plan.get("approvalRequired") and not approved:
        return {
            "update": True,
            "conclusion": "action_required",
            "summary": "Deferred x86 E2E groups are waiting for `/test e2e`.",
            "reason": "",
        }
    return {
        "update": True,
        "conclusion": "success",
        "summary": "All x86 E2E coverage required for this HEAD succeeded.",
        "reason": "",
    }


def parseExecutorRunName(name):
    match = re.fullmatch(
        rf"x86-e2e pr=([1-9][0-9]*) head=([0-9a-f]{{40}}) "
        rf"request=([0-9a-f]{{64}}) catalog=([0-9a-f]{{64}}) "
        rf"groups=({groupPattern}(?:,{groupPattern})*|-) full=([01])",
        name,
    )
    if not match:
        raise ValueError("invalid x86 E2E executor run name")
    groups = [] if match.group(5) == "-" else sorted(set(match.group(5).split(",")))
    metadata = {
        "prNumber": int(match.group(1)),
        "headSHA": match.group(2),
        "requestKey": match.group(3),
        "catalogRevision": match.group(4),
        "requestedGroups": groups,
        "full": match.group(6) == "1",
    }
    if metadata["requestKey"] != executorRequestKey(metadata):
        raise ValueError("executor request key does not match run metadata")
    return metadata


def latestExecutorRun(runs, prNumber, headSHA, baseRef):
    for run in runs:
        if (
            run.get("path") != ".github/workflows/build-x86-image.yaml"
            or run.get("actor", {}).get("login") != "github-actions[bot]"
            or run.get("head_branch") != baseRef
        ):
            continue
        try:
            metadata = parseExecutorRunName(run.get("display_title") or "")
        except ValueError:
            continue
        if metadata["prNumber"] == prNumber and metadata["headSHA"] == headSHA:
            return run
    return None


def parseArgs():
    parser = argparse.ArgumentParser(description="Control comment-gated x86 E2E workflows")
    subparsers = parser.add_subparsers(dest="command", required=True)
    dispatch = subparsers.add_parser("dispatch")
    dispatch.add_argument("--event-file", dest="eventFile", required=True)
    dispatch.add_argument("--catalog", default=".github/e2e-selection.json")
    dispatch.add_argument("--permission", required=True)
    dispatch.add_argument("--pull-request-file", dest="pullRequestFile", required=True)
    dispatch.add_argument("--confirmed-head-sha", dest="confirmedHeadSHA", required=True)
    dispatch.add_argument("--decision-file", dest="decisionFile", required=True)
    return parser.parse_args()


def main():
    args = parseArgs()
    if args.command == "dispatch":
        event = json.loads(Path(args.eventFile).read_text(encoding="utf-8"))
        catalog = json.loads(Path(args.catalog).read_text(encoding="utf-8"))
        pullRequest = json.loads(Path(args.pullRequestFile).read_text(encoding="utf-8"))
        decision = decideDispatch(
            event,
            args.permission,
            pullRequest,
            args.confirmedHeadSHA,
            set(catalog["groups"]),
            e2eSelector.catalogRevision(catalog),
        )
        Path(args.decisionFile).write_text(
            json.dumps(decision, indent=2) + "\n",
            encoding="utf-8",
        )


if __name__ == "__main__":
    main()
