# Repository Guidelines

## Project Structure & Module Organization

- `cmd/` entrypoints for CNI plugin, controller, daemon, and helpers; binaries land in `dist/images/` via Make targets.
- `pkg/` shared Go libraries; `fastpath/` and `versions/` hold data-plane helpers and release metadata.
- `charts/` Helm chart, `yamls/` and top-level `*-sa.yaml` manifest examples; `docs/` product docs.
- `hack/` CI/dev scripts; `makefiles/` split build/test logic; `test/` contains `unittest`, `e2e`, `performance`, and fixtures.

## Build, Test, and Development Commands

- `make build-go` – tidy modules and compile Go binaries for linux/amd64 into `dist/images/`.
- `make lint` – run `golangci-lint` plus Go “modernize”; auto-fixes when not in CI.
- `make ut` – run unit tests: Ginkgo suites in `test/unittest` and `go test` with coverage for `pkg`.

## General Coding Guidlines

- Every time after editing code. MUST run `make lint` to detect and fix potential lint issues.
- When modifying code, try to clean up any related code logic that is no longer needed.
- Follow `CODE_STYLE.md`: camelCase identifiers, keep functions short (~100 lines), return/log errors instead of discarding, and prefer `if err := ...; err != nil` patterns.

## Adding a New Feature

- Plan first: clarify any uncertainties and confirm the approach before making changes.
- Add unit tests to cover the new feature.
- When adding end-to-end (e2e) tests for the new feature, use `f.SkipVersionPriorTo` to ensure they run only on supported branches.

## API Changes (CRD Updates)

When modifying CRD types in `pkg/apis/kubeovn/v1/`, update the following CRD definition files to keep them in sync:

- `dist/images/install.sh` – embedded CRD definitions for install script
- `kube-ovn-crd.yaml` – standalone CRD manifest in project root
- `charts/kube-ovn/templates/kube-ovn-crd.yaml` – Helm chart v1 CRD template
- `charts/kube-ovn-v2/crds/kube-ovn-crd.yaml` – Helm chart v2 CRD definitions

## Fixing a Bug

- Analyze the issue first and identify the root cause. Confirm the analysis before making edits.
- Check if the same bug pattern exists elsewhere in the codebase.
