#!/usr/bin/env python3

import argparse
import hashlib
import json
import re
from pathlib import Path

import e2e_selector as e2eSelector


groupPattern = r"[a-z0-9]+(?:-[a-z0-9]+)*"
headPattern = r"[0-9a-f]{40}"
noncePattern = r"[0-9a-f]{16}"


def canonicalRequestedGroups(requestedGroups, full):
    return [] if full else sorted(set(requestedGroups))


def requestNonce(prNumber, headSHA, baseSHA, catalogRevision):
    return e2eSelector.requestNonce(prNumber, headSHA, baseSHA, catalogRevision)


def parseCommand(body):
    body = body.strip()
    if not body.startswith("/test e2e") and not body.startswith("/retest e2e-failed"):
        return None
    binding = rf" --head ({headPattern}) --nonce ({noncePattern})"
    match = re.fullmatch(rf"/test e2e{binding}", body)
    if match:
        return {
            "action": "dispatch",
            "requestedGroups": [],
            "full": False,
            "headSHA": match.group(1),
            "nonce": match.group(2),
        }
    match = re.fullmatch(rf"/test e2e-all{binding}", body)
    if match:
        return {
            "action": "dispatch",
            "requestedGroups": [],
            "full": True,
            "headSHA": match.group(1),
            "nonce": match.group(2),
        }
    match = re.fullmatch(rf"/retest e2e-failed{binding}", body)
    if match:
        return {
            "action": "rerun-failed",
            "requestedGroups": [],
            "full": False,
            "headSHA": match.group(1),
            "nonce": match.group(2),
        }
    match = re.fullmatch(
        rf"/test e2e ({groupPattern}(?:,{groupPattern})*){binding}",
        body,
    )
    if match:
        return {
            "action": "dispatch",
            "requestedGroups": sorted(set(match.group(1).split(","))),
            "full": False,
            "headSHA": match.group(2),
            "nonce": match.group(3),
        }
    unbound = {
        "/test e2e": {"action": "dispatch", "requestedGroups": [], "full": False},
        "/test e2e-all": {"action": "dispatch", "requestedGroups": [], "full": True},
        "/retest e2e-failed": {
            "action": "rerun-failed",
            "requestedGroups": [],
            "full": False,
        },
    }
    if body in unbound:
        return {**unbound[body], "headSHA": "", "nonce": ""}
    match = re.fullmatch(rf"/test e2e ({groupPattern}(?:,{groupPattern})*)", body)
    if not match:
        raise ValueError("invalid E2E command")
    return {
        "action": "dispatch",
        "requestedGroups": sorted(set(match.group(1).split(","))),
        "full": False,
        "headSHA": "",
        "nonce": "",
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
    liveComment=None,
    controlledLabels=(),
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
    controlledLabels = set(controlledLabels)
    knownControlledLabels = {"e2e:full"} | {f"e2e:{group}" for group in knownGroups}
    invalidControlledLabels = sorted(controlledLabels - knownControlledLabels)
    if invalidControlledLabels:
        return rejectedDecision(command, "controlled label provenance is invalid")
    controlledGroups = {
        label.removeprefix("e2e:")
        for label in controlledLabels
        if label != "e2e:full"
    }
    command = {
        **command,
        "requestedGroups": canonicalRequestedGroups(
            set(command["requestedGroups"]) | controlledGroups,
            command["full"] or "e2e:full" in controlledLabels,
        ),
        "full": command["full"] or "e2e:full" in controlledLabels,
    }
    eventComment = event.get("comment", {})
    eventUser = eventComment.get("user", {})
    liveUser = liveComment.get("user", {}) if isinstance(liveComment, dict) else {}
    if (
        liveComment is None
        or liveComment.get("id") != eventComment.get("id")
        or liveComment.get("body") != eventComment.get("body")
        or liveUser.get("login") != eventUser.get("login")
    ):
        return rejectedDecision(command, "comment changed or was deleted before dispatch")
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
    expectedNonce = requestNonce(prNumber, headSHA, baseSHA, catalogRevision)
    if command.get("headSHA") or command.get("nonce"):
        if command["headSHA"] != headSHA:
            return rejectedDecision(command, "comment is bound to another pull request HEAD")
        if command["nonce"] != expectedNonce:
            return rejectedDecision(command, "comment binding nonce is stale or invalid")
    command = {**command, "headSHA": headSHA, "nonce": expectedNonce}
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
        "controlledLabels": sorted(controlledLabels),
    }


def executorRequestKey(request):
    if request.get("automatic", False):
        return ""
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
    merged["controlledLabels"] = sorted(
        set(decision.get("controlledLabels", []))
        | {
            label
            for request in priorRequests
            for label in request.get("controlledLabels", [])
        }
    )
    merged["requestKey"] = executorRequestKey(merged)
    return merged


def approvedRequest(pullRequest, catalog, commentPages):
    currentControlledLabels = trustedControlledLabels(
        catalog,
        commentPages,
        [label.get("name", "") for label in pullRequest.get("labels", [])],
    )
    seed = {
        "prNumber": pullRequest["number"],
        "headSHA": pullRequest["head"]["sha"],
        "baseRef": pullRequest["base"]["ref"],
        "baseSHA": pullRequest["base"]["sha"],
        "approvalGeneration": 0,
        "catalogRevision": e2eSelector.catalogRevision(catalog),
        "requestedGroups": [],
        "full": False,
        "controlledLabels": currentControlledLabels,
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
                and marker.get("controlledLabels", []) == currentControlledLabels
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
    controlledLabels = sorted(set(request.get("controlledLabels", [])))
    if controlledLabels:
        payload["controlledLabels"] = controlledLabels
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
    controlledLabels = sorted(set(request.get("controlledLabels", [])))
    if controlledLabels:
        payload["controlledLabels"] = controlledLabels
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
    currentControlledLabels = trustedControlledLabels(
        catalog,
        commentPages,
        [label.get("name", "") for label in pullRequest.get("labels", [])],
    )
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
                and intent.get("controlledLabels", []) == currentControlledLabels
            ):
                intents.append(intent)
    return intents


def renderControlledLabelMarker(label, present):
    payload = {"label": label, "present": bool(present)}
    return "<!-- x86-e2e-label " + json.dumps(
        payload, sort_keys=True, separators=(",", ":")
    ) + " -->"


def parseControlledLabelMarker(body):
    match = re.search(r"<!-- x86-e2e-label (\{[^\n]*\}) -->", body)
    if not match:
        return None
    marker = json.loads(match.group(1))
    label = marker.get("label")
    if not isinstance(label, str) or not re.fullmatch(
        rf"e2e:(?:full|{groupPattern})", label
    ):
        raise ValueError("invalid controlled label marker")
    if not isinstance(marker.get("present"), bool):
        raise ValueError("invalid controlled label state")
    return {"label": label, "present": marker["present"]}


def trustedControlledLabels(catalog, commentPages, liveLabels):
    knownLabels = {"e2e:full"} | {f"e2e:{group}" for group in catalog["groups"]}
    live = set(liveLabels)
    latest = {}
    for page in commentPages:
        for comment in page:
            user = comment.get("user", {})
            commentId = comment.get("id")
            if (
                user.get("login") != "github-actions[bot]"
                or user.get("type") != "Bot"
                or not isinstance(commentId, int)
                or commentId <= 0
            ):
                continue
            try:
                marker = parseControlledLabelMarker(comment.get("body", ""))
            except (ValueError, json.JSONDecodeError):
                continue
            if marker is None or marker["label"] not in knownLabels:
                continue
            prior = latest.get(marker["label"])
            if prior is None or commentId > prior[0]:
                latest[marker["label"]] = (commentId, marker["present"])
    return sorted(
        label
        for label, (_, present) in latest.items()
        if present and label in live
    )


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
    controlledLabels = request.get("controlledLabels", [])
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
    if not isinstance(controlledLabels, list) or any(
        not isinstance(label, str)
        or not re.fullmatch(rf"e2e:(?:full|{groupPattern})", label)
        for label in controlledLabels
    ):
        raise ValueError("invalid request marker controlled labels")
    parsed = {
        "headSHA": headSHA,
        "baseSHA": baseSHA,
        "approvalGeneration": approvalGeneration,
        "catalogRevision": catalogRevision,
        "requestedGroups": canonicalRequestedGroups(groups, full),
        "full": full,
    }
    if "controlledLabels" in request:
        parsed["controlledLabels"] = sorted(set(controlledLabels))
    return parsed


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
        rf"(?:mode=(automatic|approved) )?"
        rf"groups=({groupPattern}(?:,{groupPattern})*|-) "
        rf"(?:labels=(e2e:(?:full|{groupPattern})(?:,e2e:(?:full|{groupPattern}))*|-) )?"
        rf"full=([01])",
        name,
    )
    if not match:
        raise ValueError("invalid x86 E2E executor run name")
    groups = [] if match.group(6) == "-" else sorted(set(match.group(6).split(",")))
    labelsValue = match.group(7) or "-"
    controlledLabels = (
        [] if labelsValue == "-" else sorted(set(labelsValue.split(",")))
    )
    return {
        "prNumber": int(match.group(1)),
        "headSHA": match.group(2),
        "approvalGeneration": int(match.group(3)),
        "dispatchGeneration": int(match.group(4)),
        "automatic": match.group(5) == "automatic",
        "requestedGroups": groups,
        "controlledLabels": controlledLabels,
        "full": match.group(8) == "1",
    }


def executorHeadBranch(request):
    return (
        f"x86-e2e/pr-{request['prNumber']}-"
        f"a-{request['approvalGeneration']}-d-{request['dispatchGeneration']}"
    )


def isolatedExecutorRefSHA(payload):
    if payload is None:
        return ""
    if isinstance(payload, (bytes, bytearray)):
        payload = payload.decode()
    if isinstance(payload, str):
        text = payload.strip()
        if not text:
            return ""
        try:
            payload = json.loads(text)
        except json.JSONDecodeError:
            return ""
    if not isinstance(payload, dict):
        return ""
    obj = payload.get("object")
    sha = obj.get("sha") if isinstance(obj, dict) else None
    if isinstance(sha, str) and re.fullmatch(headPattern, sha):
        return sha
    return ""


def isolatedExecutorRefAction(payload, expectedSHA):
    if not re.fullmatch(headPattern, expectedSHA or ""):
        raise ValueError("invalid expected isolated executor revision")
    sha = isolatedExecutorRefSHA(payload)
    if not sha:
        return "create"
    if sha != expectedSHA:
        return "reject"
    return "reuse"


def isApprovedGateReservation(check, prNumber, headSHA):
    if not isinstance(check, dict):
        return False
    if not re.fullmatch(headPattern, headSHA or ""):
        raise ValueError("invalid approved gate HEAD")
    summary = ""
    output = check.get("output")
    if isinstance(output, dict):
        summary = output.get("summary") or ""
    return (
        check.get("name") == "x86-e2e / required-gate"
        and check.get("external_id") == f"x86-e2e-pr-{int(prNumber)}-{headSHA}"
        and check.get("status") == "completed"
        and check.get("conclusion") == "action_required"
        and "waiting for its trusted executor" in summary
    )


def isTrustedExecutorRef(refName, baseRef, request):
    return refName == baseRef or refName == executorHeadBranch(request)


def workflowJobTitle(block):
    for line in block.splitlines():
        stripped = line.strip()
        if stripped.startswith("name:"):
            return stripped.split(":", 1)[1].strip()
    return ""


def selectedExecutorJobs(jobs, selectedTitles):
    matched = []
    seen = set()
    for job in jobs:
        name = job.get("name") or ""
        for title in selectedTitles:
            prefix = title.split("${{", 1)[0].rstrip()
            if name == title or (prefix and name.startswith(prefix)):
                jobId = job.get("id")
                if jobId in seen:
                    break
                seen.add(jobId)
                matched.append(job)
                break
    return matched


def selectedExecutorJobsAreTerminal(jobs, selectedTitles):
    if not selectedTitles:
        return True
    matched = selectedExecutorJobs(jobs, selectedTitles)
    seenTitles = set()
    for job in matched:
        name = job.get("name") or ""
        for title in selectedTitles:
            prefix = title.split("${{", 1)[0].rstrip()
            if name == title or (prefix and name.startswith(prefix)):
                seenTitles.add(title)
                break
    if seenTitles != set(selectedTitles):
        return False
    return all(
        job.get("status") == "completed" or bool(job.get("conclusion"))
        for job in matched
    )


def prCheckRunFromExecutorJob(job, headSHA, prNumber):
    if not re.fullmatch(headPattern, headSHA or ""):
        raise ValueError("invalid pull request HEAD for E2E check")
    name = job.get("name") or ""
    if not name:
        raise ValueError("executor job is missing a name")
    conclusion = job.get("conclusion")
    completed = job.get("status") == "completed" or bool(conclusion)
    payload = {
        "name": name,
        "head_sha": headSHA,
        "status": "completed" if completed else "in_progress",
        "external_id": f"x86-e2e-pr-{int(prNumber)}-{headSHA}-{name}"[:255],
        "details_url": job.get("html_url") or "",
        "output": {
            "title": name,
            "summary": (
                f"Trusted x86 E2E result for pull request HEAD `{headSHA}`."
            ),
        },
    }
    if completed:
        payload["conclusion"] = conclusion or "cancelled"
    return payload


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
    matchingRuns = []
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
            matchingRuns.append((metadata, run))
    if not matchingRuns:
        return None
    # An automatic run is a coverage probe and must never supersede a durable
    # approved request that is already in flight for the same HEAD.
    for metadata, run in matchingRuns:
        if not metadata["automatic"]:
            return run
    return matchingRuns[0][1]


def parseArgs():
    parser = argparse.ArgumentParser(description="Control comment-gated x86 E2E workflows")
    subparsers = parser.add_subparsers(dest="command", required=True)
    dispatch = subparsers.add_parser("dispatch")
    dispatch.add_argument("--event-file", dest="eventFile", required=True)
    dispatch.add_argument("--catalog", default=".github/e2e-selection.json")
    dispatch.add_argument("--permission", required=True)
    dispatch.add_argument("--pull-request-file", dest="pullRequestFile", required=True)
    dispatch.add_argument("--confirmed-head-sha", dest="confirmedHeadSHA", required=True)
    dispatch.add_argument("--live-comment-file", dest="liveCommentFile", required=True)
    dispatch.add_argument(
        "--controlled-labels-file",
        dest="controlledLabelsFile",
        required=True,
    )
    dispatch.add_argument("--decision-file", dest="decisionFile", required=True)
    return parser.parse_args()


def main():
    args = parseArgs()
    if args.command == "dispatch":
        event = json.loads(Path(args.eventFile).read_text(encoding="utf-8"))
        catalog = json.loads(Path(args.catalog).read_text(encoding="utf-8"))
        pullRequest = json.loads(Path(args.pullRequestFile).read_text(encoding="utf-8"))
        liveComment = json.loads(Path(args.liveCommentFile).read_text(encoding="utf-8"))
        controlledLabels = json.loads(
            Path(args.controlledLabelsFile).read_text(encoding="utf-8")
        )
        if not isinstance(controlledLabels, list) or any(
            not isinstance(label, str) for label in controlledLabels
        ):
            raise SystemExit("controlled labels must be a JSON array of strings")
        decision = decideDispatch(
            event,
            args.permission,
            pullRequest,
            args.confirmedHeadSHA,
            set(catalog["groups"]),
            e2eSelector.catalogRevision(catalog),
            liveComment,
            controlledLabels,
        )
        Path(args.decisionFile).write_text(
            json.dumps(decision, indent=2) + "\n",
            encoding="utf-8",
        )


if __name__ == "__main__":
    main()
