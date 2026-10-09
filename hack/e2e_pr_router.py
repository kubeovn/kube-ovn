#!/usr/bin/env python3
"""Forward GitHub events to the workflows at an exact pull request HEAD."""

import argparse
import base64
import io
import json
import os
import re
import subprocess
import zipfile
from pathlib import Path

import e2e_control as e2eControl
import e2e_selector as e2eSelector


ROUTER_PATH = ".github/workflows/x86-e2e-pr-router.yaml"
WORKFLOWS = {"x86-e2e-dispatcher.yaml", "x86-e2e-gate.yaml", "build-x86-image.yaml"}
SOURCE_EVENTS = {"issue_comment", "pull_request_target", "push"}
PR_ACTIONS = {"opened", "reopened", "synchronize", "labeled", "unlabeled", "closed"}


class GitHub:
    def api(self, path, *, method="GET", payload=None, raw=False):
        args = ["gh", "api", "--method", method, path]
        data = None
        if payload is not None:
            args.extend(["--input", "-"])
            data = json.dumps(payload).encode()
        result = subprocess.run(args, input=data, capture_output=True, check=True)
        if raw:
            return result.stdout
        return json.loads(result.stdout) if result.stdout.strip() else None


def candidateWorkflowRef(prNumber, headSHA):
    if isinstance(prNumber, bool) or not isinstance(prNumber, int) or prNumber <= 0:
        raise ValueError("invalid pull request number")
    if not isinstance(headSHA, str) or not re.fullmatch(r"[0-9a-f]{40}", headSHA):
        raise ValueError("invalid candidate HEAD")
    return f"x86-e2e/pr-{prNumber}-control-{headSHA}"


def livePullRequest(github, repository, prNumber, headSHA, *, allowClosed=False):
    candidateWorkflowRef(prNumber, headSHA)
    pull = github.api(f"repos/{repository}/pulls/{prNumber}")
    if pull.get("number") != prNumber or pull.get("head", {}).get("sha") != headSHA:
        raise ValueError("pull request HEAD changed before routing")
    if pull.get("base", {}).get("repo", {}).get("full_name") != repository:
        raise ValueError("pull request targets a different repository")
    baseRef = pull.get("base", {}).get("ref", "")
    if baseRef != "master" and not re.fullmatch(r"release-[A-Za-z0-9._-]+", baseRef):
        raise ValueError("unsupported pull request base branch")
    if pull.get("state") != "open" and not (allowClosed and pull.get("state") == "closed"):
        raise ValueError("pull request is not open")
    return pull


def dispatch(repository, prNumber, headSHA, workflow, inputs, *, github=None, allowClosed=False):
    github = github or GitHub()
    if workflow not in WORKFLOWS:
        raise ValueError("unsupported candidate workflow")
    if not isinstance(inputs, dict) or any(not isinstance(k, str) or not isinstance(v, str)
                                           for k, v in inputs.items()):
        raise ValueError("workflow inputs must map names to strings")
    ref = candidateWorkflowRef(prNumber, headSHA)
    if (inputs.get("headSHA") != headSHA
            or ("sourceHeadSHA" in inputs and inputs["sourceHeadSHA"] != headSHA)
            or ("prNumber" in inputs and inputs["prNumber"] != str(prNumber))):
        raise ValueError("workflow inputs do not match the candidate pull request identity")
    payload = {"ref": ref, "inputs": inputs}
    # Include JSON escaping of the nested source event in the dispatch size limit.
    if len(json.dumps(payload).encode()) > 65535:
        raise ValueError("workflow dispatch payload exceeds the size limit")
    livePullRequest(github, repository, prNumber, headSHA, allowClosed=allowClosed)
    endpoint = f"repos/{repository}/git/ref/heads/{ref}"
    try:
        existing = github.api(endpoint)
    except subprocess.CalledProcessError as error:
        # Authentication/network failures must not be mistaken for a missing ref.
        if b"HTTP 404" not in (error.stderr or b""):
            raise
        existing = None
    if existing is None:
        try:
            github.api(f"repos/{repository}/git/refs", method="POST",
                       payload={"ref": f"refs/heads/{ref}", "sha": headSHA})
        except subprocess.CalledProcessError as error:
            if b"HTTP 422" not in (error.stderr or b""):
                raise
            existing = github.api(endpoint)
            if existing.get("object", {}).get("sha") != headSHA:
                raise ValueError("candidate workflow ref points at an unexpected revision") from error
    elif existing.get("object", {}).get("sha") != headSHA:
        raise ValueError("candidate workflow ref points at an unexpected revision")
    # Revalidate after ref creation; a synchronize event can race the API calls.
    livePullRequest(github, repository, prNumber, headSHA, allowClosed=allowClosed)
    github.api(f"repos/{repository}/actions/workflows/{workflow}/dispatches", method="POST",
               payload=payload)
    return ref


def compactEvent(eventName, event, actor, runId, runAttempt):
    repository = event.get("repository", {})
    source = {
        "action": event.get("action"),
        "repository": {"full_name": repository.get("full_name"),
                       "default_branch": repository.get("default_branch")},
        "sender": {"login": actor},
        "_router": {"runId": int(runId), "runAttempt": int(runAttempt), "eventName": eventName},
    }
    if eventName == "issue_comment":
        comment = event.get("comment", {})
        issue = event.get("issue", {})
        source["issue"] = {"number": issue.get("number"), "pull_request": issue.get("pull_request")}
        source["comment"] = {"id": comment.get("id"), "body": comment.get("body"),
                             "user": {"login": comment.get("user", {}).get("login")}}
    elif eventName == "pull_request_target":
        pull = event.get("pull_request", {})
        source["pull_request"] = {
            "number": pull.get("number"), "head": {"sha": pull.get("head", {}).get("sha")},
            "base": {"ref": pull.get("base", {}).get("ref"), "sha": pull.get("base", {}).get("sha")},
        }
        if "label" in event:
            source["label"] = {"name": event["label"].get("name")}
    return source


def checkComment(github, repository, source, actor):
    comment = source["comment"]
    if source.get("action") != "created" or not source["issue"].get("pull_request"):
        raise ValueError("source is not a new pull request comment")
    if comment.get("user", {}).get("login") != actor:
        raise ValueError("comment author does not match the source actor")
    live = github.api(f"repos/{repository}/issues/comments/{comment['id']}")
    expectedIssue = f"https://api.github.com/repos/{repository}/issues/{source['issue']['number']}"
    if (live.get("id") != comment.get("id") or live.get("body") != comment.get("body")
            or live.get("user", {}).get("login") != actor or live.get("issue_url") != expectedIssue):
        raise ValueError("source comment changed, was deleted, or belongs to another pull request")
    permission = github.api(f"repos/{repository}/collaborators/{actor}/permission")
    if permission.get("permission") not in {"admin", "maintain", "write"}:
        raise ValueError("repository write permission is required to forward an E2E command")


def prepare(eventName, event, repository, actor, runId, runAttempt, *, github=None):
    github = github or GitHub()
    if event.get("repository", {}).get("full_name") != repository:
        raise ValueError("source event belongs to another repository")
    if eventName == "push":
        ref = event.get("ref", "")
        if not re.fullmatch(r"refs/heads/release-[A-Za-z0-9._-]+", ref):
            raise ValueError("unsupported base push")
        baseRef, baseSHA = ref.removeprefix("refs/heads/"), event.get("after")
        if github.api(f"repos/{repository}/git/ref/heads/{baseRef}")["object"]["sha"] != baseSHA:
            return []
        routes, page = [], 1
        while True:
            pulls = github.api(f"repos/{repository}/pulls?state=open&base={baseRef}&per_page=100&page={page}")
            for pull in pulls:
                prNumber, headSHA = pull["number"], pull["head"]["sha"]
                livePullRequest(github, repository, prNumber, headSHA)
                source = compactEvent(eventName, event, actor, runId, runAttempt)
                source.update({"ref": ref, "after": baseSHA,
                               "pull_request": {"number": prNumber, "head": {"sha": headSHA}}})
                routes.append({"workflow": "x86-e2e-dispatcher.yaml", "prNumber": prNumber,
                               "headSHA": headSHA, "allowClosed": False, "sourceEvent": source,
                               "inputs": {"prNumber": str(prNumber), "headSHA": headSHA,
                                          "approvalGeneration": "0", "sourceEventName": eventName,
                                          "sourceEvent": json.dumps(source, separators=(",", ":")),
                                          "sourceHeadSHA": headSHA}})
            if len(pulls) < 100:
                return routes
            page += 1
    if eventName in {"issue_comment", "pull_request_target"}:
        source = compactEvent(eventName, event, actor, runId, runAttempt)
        if event.get("sender", {}).get("login") != actor:
            raise ValueError("source event actor does not match the authenticated actor")
        if eventName == "issue_comment":
            body = source["comment"].get("body") or ""
            if not body.startswith(("/test e2e", "/retest e2e-failed")):
                return None
            checkComment(github, repository, source, actor)
            prNumber = source["issue"]["number"]
            pull = github.api(f"repos/{repository}/pulls/{prNumber}")
            headSHA = pull.get("head", {}).get("sha")
        else:
            if source.get("action") not in PR_ACTIONS:
                raise ValueError("unsupported pull request source action")
            prNumber = source["pull_request"]["number"]
            headSHA = source["pull_request"]["head"]["sha"]
        allowClosed = eventName == "pull_request_target" and source["action"] == "closed"
        livePullRequest(github, repository, prNumber, headSHA, allowClosed=allowClosed)
        sourceText = json.dumps(source, separators=(",", ":"))
        # Leave room for the other inputs within GitHub's 65,535-character limit.
        if len(sourceText) > 60000:
            raise ValueError("source event exceeds the workflow input size limit")
        return {"workflow": "x86-e2e-dispatcher.yaml", "prNumber": prNumber,
                "headSHA": headSHA, "allowClosed": allowClosed, "sourceEvent": source,
                "inputs": {"prNumber": str(prNumber), "headSHA": headSHA, "approvalGeneration": "0",
                           "sourceEventName": eventName, "sourceEvent": sourceText, "sourceHeadSHA": headSHA}}
    if eventName != "workflow_run" or event.get("action") != "completed":
        raise ValueError("unsupported source event")
    eventRun = event.get("workflow_run", {})
    run = github.api(f"repos/{repository}/actions/runs/{eventRun['id']}")
    # Ordinary release image builds do not participate in the PR E2E gate.
    if run.get("event") != "workflow_dispatch" or not (run.get("display_title") or "").startswith("x86-e2e pr="):
        return None
    if (run.get("path") != ".github/workflows/build-x86-image.yaml"
            or run.get("event") != "workflow_dispatch"
            or run.get("actor", {}).get("login") != "github-actions[bot]"
            or run.get("status") != "completed"
            or run.get("run_attempt") != eventRun.get("run_attempt")):
        raise ValueError("source executor is not an authenticated completed candidate run")
    metadata = e2eControl.parseExecutorRunName(run.get("display_title") or "")
    headSHA, prNumber = metadata["headSHA"], metadata["prNumber"]
    if (run.get("head_sha") != headSHA
            or run.get("head_branch") != e2eControl.executorHeadBranch(metadata)):
        raise ValueError("source executor did not execute the exact candidate HEAD")
    pull = livePullRequest(github, repository, prNumber, headSHA)
    baseRef = pull["base"]["ref"]
    base = metadata["baseSHA"]
    if not base:
        raise ValueError("source executor is missing its actual base identity")
    content = github.api(f"repos/{repository}/contents/.github/e2e-selection.json?ref={headSHA}")
    catalog = json.loads(base64.b64decode(content["content"]))
    e2eSelector.validateCatalog(catalog)
    inputs = {"prNumber": str(prNumber), "headSHA": headSHA, "baseSHA": base,
              "approvalGeneration": str(metadata["approvalGeneration"]),
              "catalogRevision": e2eSelector.catalogRevision(catalog),
              "requestedGroups": json.dumps(metadata["requestedGroups"]),
              "controlledLabels": json.dumps(metadata["controlledLabels"]),
              "full": str(metadata["full"]).lower(), "recordIntent": "false", "baseRefresh": "false",
              "executorRunId": str(run["id"]), "executorRunAttempt": str(run["run_attempt"]),
              "executorConclusion": ""}
    return {"workflow": "x86-e2e-gate.yaml", "prNumber": prNumber, "headSHA": headSHA,
            "allowClosed": False, "sourceEvent": {"workflow_run": {"id": run["id"]}}, "inputs": inputs}


def validateSource(eventName, event, sourceEventName, source, prNumber, headSHA, repository, *, github=None):
    github = github or GitHub()
    if not sourceEventName:
        if eventName != "workflow_dispatch" or os.environ.get("GITHUB_ACTOR") != "github-actions[bot]":
            raise ValueError("candidate dispatcher must originate from the router or a bot callback")
        livePullRequest(github, repository, prNumber, headSHA)
        return event
    if (eventName != "workflow_dispatch" or os.environ.get("GITHUB_ACTOR") != "github-actions[bot]"
            or sourceEventName not in SOURCE_EVENTS):
        raise ValueError("replayed source events require an authenticated router dispatch")
    provenance = source.get("_router", {})
    runId, attempt = provenance.get("runId"), provenance.get("runAttempt")
    if not isinstance(runId, int) or runId <= 0 or not isinstance(attempt, int) or attempt <= 0:
        raise ValueError("source event lacks router run provenance")
    run = github.api(f"repos/{repository}/actions/runs/{runId}")
    actor = source.get("sender", {}).get("login")
    if (run.get("path") != ROUTER_PATH or run.get("event") != sourceEventName
            or run.get("actor", {}).get("login") != actor or run.get("run_attempt") != attempt
            or provenance.get("eventName") != sourceEventName
            or source.get("repository", {}).get("full_name") != repository):
        raise ValueError("source event does not match its authenticated router run")
    artifacts = github.api(f"repos/{repository}/actions/runs/{runId}/artifacts?per_page=100")["artifacts"]
    matches = [item for item in artifacts if item.get("name") == f"x86-e2e-router-event-{attempt}"
               and not item.get("expired")]
    if len(matches) != 1:
        raise ValueError("canonical router event artifact is missing or ambiguous")
    data = github.api(f"repos/{repository}/actions/artifacts/{matches[0]['id']}/zip", raw=True)
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        entry = archive.getinfo("source-events.json")
        if entry.file_size > 8 * 1024 * 1024:
            raise ValueError("canonical router event exceeds the size limit")
        canonical = json.loads(archive.read(entry))
    if not isinstance(canonical, list) or canonical.count(source) != 1:
        raise ValueError("source event differs from the canonical router event")
    if sourceEventName == "issue_comment":
        if source.get("issue", {}).get("number") != prNumber:
            raise ValueError("source comment refers to another pull request")
        checkComment(github, repository, source, actor)
    elif sourceEventName == "push":
        pull = livePullRequest(github, repository, prNumber, headSHA)
        baseRef = pull["base"]["ref"]
        if (source.get("pull_request", {}).get("number") != prNumber
                or source.get("pull_request", {}).get("head", {}).get("sha") != headSHA
                or source.get("ref") != f"refs/heads/{baseRef}"
                or not baseRef.startswith("release-")
                or github.api(f"repos/{repository}/git/ref/heads/{baseRef}")["object"]["sha"] != source.get("after")):
            raise ValueError("source base push is stale or refers to another pull request")
    elif (source.get("pull_request", {}).get("number") != prNumber
          or source.get("pull_request", {}).get("head", {}).get("sha") != headSHA
          or source.get("action") not in PR_ACTIONS):
        raise ValueError("source pull request identity is invalid")
    livePullRequest(github, repository, prNumber, headSHA,
                    allowClosed=sourceEventName == "pull_request_target" and source.get("action") == "closed")
    return source


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    send = commands.add_parser("dispatch")
    send.add_argument("--pr-number", type=int, required=True)
    send.add_argument("--head-sha", required=True)
    send.add_argument("--workflow", choices=sorted(WORKFLOWS), required=True)
    send.add_argument("--inputs-file", type=Path, required=True)
    send.add_argument("--allow-closed", action="store_true")
    prep = commands.add_parser("prepare")
    prep.add_argument("--event-file", type=Path, required=True)
    prep.add_argument("--event-name", required=True)
    prep.add_argument("--output-dir", type=Path, required=True)
    batch = commands.add_parser("dispatch-prepared")
    batch.add_argument("--routes-file", type=Path, required=True)
    materialize = commands.add_parser("materialize-source")
    materialize.add_argument("--output-file", type=Path, required=True)
    check = commands.add_parser("validate-source")
    check.add_argument("--output-file", type=Path, required=True)
    args = parser.parse_args()
    repository = os.environ["GITHUB_REPOSITORY"]
    if args.command == "materialize-source":
        event = (json.loads(os.environ["SOURCE_EVENT"]) if os.environ.get("SOURCE_EVENT_NAME")
                 else json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text()))
        args.output_file.write_text(json.dumps(event))
    elif args.command == "dispatch":
        print(dispatch(repository, args.pr_number, args.head_sha, args.workflow,
                       json.loads(args.inputs_file.read_text()), allowClosed=args.allow_closed))
    elif args.command == "dispatch-prepared":
        for routed in json.loads(args.routes_file.read_text()):
            print(dispatch(repository, routed["prNumber"], routed["headSHA"], routed["workflow"],
                           routed["inputs"], allowClosed=routed["allowClosed"]))
    elif args.command == "prepare":
        routed = prepare(args.event_name, json.loads(args.event_file.read_text()), repository,
                         os.environ["GITHUB_ACTOR"], os.environ["GITHUB_RUN_ID"], os.environ["GITHUB_RUN_ATTEMPT"])
        routes = routed if isinstance(routed, list) else ([routed] if routed else [])
        with Path(os.environ["GITHUB_OUTPUT"]).open("a") as output:
            output.write(f"route={'true' if routes else 'false'}\n")
            if routes:
                args.output_dir.mkdir(parents=True, exist_ok=True)
                (args.output_dir / "source-events.json").write_text(json.dumps([item["sourceEvent"] for item in routes]))
                (args.output_dir / "routes.json").write_text(json.dumps(routes))
    else:
        if os.environ["GITHUB_SHA"] != os.environ["CANDIDATE_HEAD"]:
            raise ValueError("dispatcher workflow did not execute the exact candidate HEAD")
        sourceName = os.environ.get("SOURCE_EVENT_NAME", "")
        source = json.loads(os.environ.get("SOURCE_EVENT", "") or "{}")
        event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text())
        canonical = validateSource(os.environ["GITHUB_EVENT_NAME"], event, sourceName, source,
                                   int(os.environ.get("PR_NUMBER") or "0"), os.environ["CANDIDATE_HEAD"], repository)
        args.output_file.write_text(json.dumps(canonical))


if __name__ == "__main__":
    main()
