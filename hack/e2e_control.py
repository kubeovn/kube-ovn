#!/usr/bin/env python3

import argparse
import hashlib
import json
import re
from pathlib import Path

import e2e_selector as e2eSelector


groupPattern = r"[a-z0-9]+(?:-[a-z0-9]+)*"


def canonicalRequestedGroups(requestedGroups, full):
    return [] if full else sorted(set(requestedGroups))


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
    baseSHA = pullRequest.get("base", {}).get("sha")
    if not re.fullmatch(r"[0-9a-f]{40}", baseSHA or ""):
        return rejectedDecision(command, "pull request base revision is invalid")
    prNumber = pullRequest.get("number")
    approvalGeneration = event.get("comment", {}).get("id")
    if not isinstance(approvalGeneration, int) or approvalGeneration <= 0:
        return rejectedDecision(command, "comment identifier is invalid")
    payload = {
        "repository": event.get("repository", {}).get("full_name"),
        "prNumber": prNumber,
        "headSHA": headSHA,
        "baseSHA": baseSHA,
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
        "baseSHA": baseSHA,
        "approvalGeneration": approvalGeneration,
        "catalogRevision": catalogRevision,
        "requestKey": requestKey,
    }


def executorRequestKey(request):
    payload = {
        "prNumber": request["prNumber"],
        "headSHA": request["headSHA"],
        "baseSHA": request["baseSHA"],
        "action": "dispatch",
        "catalogRevision": request["catalogRevision"],
        "requestedGroups": canonicalRequestedGroups(
            request["requestedGroups"], request["full"]
        ),
        "full": request["full"],
    }
    return hashlib.sha256(
        json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
    ).hexdigest()


def mergeApprovedRequests(decision, priorRequests):
    merged = dict(decision)
    groups = set(decision["requestedGroups"])
    full = decision["full"]
    approvalGeneration = decision.get("approvalGeneration", 0)
    for request in priorRequests:
        if (
            request.get("headSHA") != decision["headSHA"]
            or request.get("baseSHA") != decision["baseSHA"]
            or request.get("catalogRevision") != decision["catalogRevision"]
        ):
            continue
        groups.update(request.get("requestedGroups", []))
        full = full or request.get("full", False)
        approvalGeneration = max(
            approvalGeneration,
            request.get("approvalGeneration", 0),
        )
    merged["requestedGroups"] = canonicalRequestedGroups(groups, full)
    merged["full"] = full
    merged["approvalGeneration"] = approvalGeneration
    merged["requestKey"] = executorRequestKey(merged)
    return merged


def approvedRequest(pullRequest, catalog, commentPages):
    seed = {
        "prNumber": pullRequest["number"],
        "headSHA": pullRequest["head"]["sha"],
        "baseRef": pullRequest["base"]["ref"],
        "baseSHA": pullRequest["base"]["sha"],
        "approvalGeneration": 0,
        "catalogRevision": e2eSelector.catalogRevision(catalog),
        "requestedGroups": [],
        "full": False,
    }
    approvals = []
    for page in commentPages:
        for comment in page:
            user = comment.get("user", {})
            if user.get("login") != "github-actions[bot]" or user.get("type") != "Bot":
                continue
            try:
                marker = parseRequestMarker(comment.get("body", ""))
            except ValueError:
                continue
            if (
                marker is not None
                and marker["headSHA"] == seed["headSHA"]
                and marker["baseSHA"] == seed["baseSHA"]
                and marker["catalogRevision"] == seed["catalogRevision"]
            ):
                approvals.append(marker)
    if not approvals:
        return None
    return mergeApprovedRequests(seed, approvals)


def renderRequestMarker(request):
    payload = {
        "headSHA": request["headSHA"],
        "baseSHA": request["baseSHA"],
        "approvalGeneration": request["approvalGeneration"],
        "catalogRevision": request["catalogRevision"],
        "requestedGroups": canonicalRequestedGroups(
            request["requestedGroups"], request["full"]
        ),
        "full": request["full"],
    }
    return "<!-- x86-e2e-request " + json.dumps(
        payload, sort_keys=True, separators=(",", ":")
    ) + " -->"


def renderApprovalIntent(request):
    payload = {
        "headSHA": request["headSHA"],
        "baseSHA": request["baseSHA"],
        "approvalGeneration": request["approvalGeneration"],
        "catalogRevision": request["catalogRevision"],
        "requestedGroups": canonicalRequestedGroups(
            request["requestedGroups"], request["full"]
        ),
        "full": request["full"],
    }
    return "<!-- x86-e2e-intent " + json.dumps(
        payload, sort_keys=True, separators=(",", ":")
    ) + " -->"


def parseApprovalIntent(body):
    match = re.search(r"<!-- x86-e2e-intent (\{.*?\}) -->", body)
    if not match:
        return None
    return validateRequestMarker(json.loads(match.group(1)))


def approvalIntents(pullRequest, catalog, commentPages):
    headSHA = pullRequest["head"]["sha"]
    baseSHA = pullRequest["base"]["sha"]
    catalogRevision = e2eSelector.catalogRevision(catalog)
    intents = []
    for page in commentPages:
        for comment in page:
            user = comment.get("user", {})
            if user.get("login") != "github-actions[bot]" or user.get("type") != "Bot":
                continue
            try:
                intent = parseApprovalIntent(comment.get("body", ""))
            except ValueError:
                continue
            if (
                intent is not None
                and intent["headSHA"] == headSHA
                and intent["baseSHA"] == baseSHA
                and intent["catalogRevision"] == catalogRevision
            ):
                intents.append(intent)
    return intents


def renderRerunMarker(runId, runAttempt, headSHA, baseSHA):
    payload = {
        "runId": int(runId),
        "runAttempt": int(runAttempt),
        "headSHA": headSHA,
        "baseSHA": baseSHA,
    }
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":"))
    return f"<!-- x86-e2e-rerun {encoded} -->"


def parseRerunMarker(body):
    match = re.search(r"<!-- x86-e2e-rerun (\{.*?\}) -->", body)
    if not match:
        return None
    marker = json.loads(match.group(1))
    if not isinstance(marker.get("runId"), int) or marker["runId"] <= 0:
        raise ValueError("invalid rerun marker run identifier")
    if not isinstance(marker.get("runAttempt"), int) or marker["runAttempt"] <= 1:
        raise ValueError("invalid rerun marker attempt")
    if not re.fullmatch(r"[0-9a-f]{40}", marker.get("headSHA") or ""):
        raise ValueError("invalid rerun marker head revision")
    if not re.fullmatch(r"[0-9a-f]{40}", marker.get("baseSHA") or ""):
        raise ValueError("invalid rerun marker base revision")
    return marker


def hasAuthorizedRerun(commentPages, runId, runAttempt, headSHA, baseSHA):
    expected = {
        "runId": int(runId),
        "runAttempt": int(runAttempt),
        "headSHA": headSHA,
        "baseSHA": baseSHA,
    }
    for page in commentPages:
        for comment in page:
            user = comment.get("user", {})
            if user.get("login") != "github-actions[bot]" or user.get("type") != "Bot":
                continue
            try:
                marker = parseRerunMarker(comment.get("body", ""))
            except (ValueError, json.JSONDecodeError):
                continue
            if marker == expected:
                return True
    return False


def isAuthorizedRunAttempt(run, commentPages, headSHA, baseSHA):
    runAttempt = run.get("run_attempt")
    if not isinstance(runAttempt, int) or runAttempt <= 0:
        return False
    if runAttempt == 1:
        return True
    if run.get("triggering_actor", {}).get("login") != "github-actions[bot]":
        return False
    return hasAuthorizedRerun(
        commentPages,
        run["id"],
        runAttempt,
        headSHA,
        baseSHA,
    )


def validateRequestMarker(request):
    headSHA = request.get("headSHA")
    baseSHA = request.get("baseSHA")
    approvalGeneration = request.get("approvalGeneration")
    catalogRevision = request.get("catalogRevision")
    groups = request.get("requestedGroups")
    full = request.get("full")
    if not re.fullmatch(r"[0-9a-f]{40}", headSHA or ""):
        raise ValueError("invalid request marker HEAD")
    if not re.fullmatch(r"[0-9a-f]{40}", baseSHA or ""):
        raise ValueError("invalid request marker base revision")
    if not isinstance(approvalGeneration, int) or approvalGeneration <= 0:
        raise ValueError("invalid request marker approval generation")
    if not re.fullmatch(r"[0-9a-f]{64}", catalogRevision or ""):
        raise ValueError("invalid request marker catalog revision")
    if not isinstance(groups, list) or any(
        not isinstance(group, str) or not re.fullmatch(groupPattern, group)
        for group in groups
    ):
        raise ValueError("invalid request marker groups")
    if not isinstance(full, bool):
        raise ValueError("invalid request marker full value")
    return {
        "headSHA": headSHA,
        "baseSHA": baseSHA,
        "approvalGeneration": approvalGeneration,
        "catalogRevision": catalogRevision,
        "requestedGroups": canonicalRequestedGroups(groups, full),
        "full": full,
    }


def parseRequestMarker(body):
    match = re.search(r"<!-- x86-e2e-request (\{[^\n]*\}) -->", body)
    if not match:
        return None
    return validateRequestMarker(json.loads(match.group(1)))


def evaluateGate(
    plan,
    currentHeadSHA,
    runConclusion,
    approved,
    expectedCatalogRevision=None,
    executedPlan=None,
    trustedExecution=True,
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
    if not trustedExecution:
        return {
            "update": True,
            "conclusion": "action_required",
            "summary": (
                "The pull request workflow is untrusted for the fixed gate; "
                "start the trusted executor with `/test e2e`."
            ),
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
        rf"approval=([1-9][0-9]*) generation=([1-9][0-9]*) "
        rf"groups=({groupPattern}(?:,{groupPattern})*|-) full=([01])",
        name,
    )
    if not match:
        raise ValueError("invalid x86 E2E executor run name")
    groups = [] if match.group(5) == "-" else sorted(set(match.group(5).split(",")))
    return {
        "prNumber": int(match.group(1)),
        "headSHA": match.group(2),
        "approvalGeneration": int(match.group(3)),
        "dispatchGeneration": int(match.group(4)),
        "requestedGroups": groups,
        "full": match.group(6) == "1",
    }


def executorHeadBranch(request):
    return (
        f"x86-e2e/pr-{request['prNumber']}-"
        f"a-{request['approvalGeneration']}-d-{request['dispatchGeneration']}"
    )


def latestExecutorRun(
    runs,
    prNumber,
    headSHA,
    baseRef,
    catalogRevision=None,
    workflowSHA=None,
):
    if baseRef != "master" and not re.fullmatch(
        r"release-[A-Za-z0-9._-]+", baseRef or ""
    ):
        return None
    for run in runs:
        if (
            run.get("path") != ".github/workflows/build-x86-image.yaml"
            or run.get("actor", {}).get("login") != "github-actions[bot]"
            or (workflowSHA is not None and run.get("head_sha") != workflowSHA)
        ):
            continue
        try:
            metadata = parseExecutorRunName(run.get("display_title") or "")
        except ValueError:
            continue
        if (
            run.get("head_branch") == executorHeadBranch(metadata)
            and metadata["prNumber"] == prNumber
            and metadata["headSHA"] == headSHA
        ):
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
