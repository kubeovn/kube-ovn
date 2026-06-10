# OVN DB TLS Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有 `ENABLE_SSL` 基础上实现 OVN DB TLS 专用证书 profile，为后续 mTLS 和自动轮转提供 signer、证书校验和客户端加载能力。

**Architecture:** 先把现有 `kubeovn.io/signer` 从只支持 IPsec CSR 扩展为支持多 profile。第一阶段实现 OVN DB TLS CSR 识别、证书模板、CA Secret 常量和 Go OVSDB client 严格 TLS 配置；脚本和 workload 证书落盘在后续任务接入。

**Tech Stack:** Go, Kubernetes CSR API, crypto/x509, libovsdb, Bash, Helm.

---

### Task 1: 扩展 signer profile 基础能力

**Files:**
- Modify: `pkg/util/const.go`
- Modify: `pkg/controller/signer.go`
- Test: `pkg/controller/signer_test.go`

- [ ] **Step 1: Write failing tests**

Add tests for:

```go
func TestCSRProfileDetection(t *testing.T) {
    // ovn-ipsec-* + UsageIPsecTunnel -> ipsec profile
    // ovn-db-tls-server-* + UsageServerAuth -> ovn db server profile
    // ovn-db-tls-client-* + UsageClientAuth -> ovn db client profile
}
```

- [ ] **Step 2: Run red test**

Run:

```bash
go test ./pkg/controller -run 'TestCSRProfileDetection|TestNewCertificateTemplateForProfile' -count=1
```

Expected: FAIL because OVN DB TLS profile helpers do not exist yet.

- [ ] **Step 3: Implement profile helpers**

Add constants:

```go
DefaultOVNDBTLSCA = "ovn-db-tls-ca"
OVNDBTLSServerCSRPrefix = "ovn-db-tls-server-"
OVNDBTLSClientCSRPrefix = "ovn-db-tls-client-"
```

Add signer profile helpers that return profile type, expected CA Secret, and expected key usages.

- [ ] **Step 4: Run green test**

Run:

```bash
go test ./pkg/controller -run 'TestCSRProfileDetection|TestNewCertificateTemplateForProfile' -count=1
```

Expected: PASS.

### Task 2: 签发 OVN DB TLS server/client 证书

**Files:**
- Modify: `pkg/controller/signer.go`
- Test: `pkg/controller/signer_test.go`

- [ ] **Step 1: Write failing tests**

Add tests for certificate template generation:

```go
func TestNewCertificateTemplateForProfile(t *testing.T) {
    // server profile gets ExtKeyUsageServerAuth and carries CSR DNS/IP SANs
    // client profile gets ExtKeyUsageClientAuth
    // ipsec profile preserves existing behavior
}
```

- [ ] **Step 2: Run red test**

Run:

```bash
go test ./pkg/controller -run TestNewCertificateTemplateForProfile -count=1
```

Expected: FAIL because current `newCertificateTemplate` does not set profile-specific usages.

- [ ] **Step 3: Implement template generation**

Change signer code so `handleAddOrUpdateCsr` resolves a profile before signing and calls:

```go
newCertificateTemplateForProfile(certReq, profile)
```

- [ ] **Step 4: Run green test**

Run:

```bash
go test ./pkg/controller -run TestNewCertificateTemplateForProfile -count=1
```

Expected: PASS.

### Task 3: 新增 OVN DB TLS CA 初始化

**Files:**
- Modify: `pkg/controller/pki.go`
- Test: `pkg/controller/pki_test.go`

- [ ] **Step 1: Write failing tests**

Add fake client tests:

```go
func TestInitDefaultOVNDBTLSCA(t *testing.T) {
    // existing Secret -> no create
    // missing Secret -> creates ovn-db-tls-ca with cacert/cakey
}
```

- [ ] **Step 2: Run red test**

Run:

```bash
go test ./pkg/controller -run TestInitDefaultOVNDBTLSCA -count=1
```

Expected: FAIL because `InitDefaultOVNDBTLSCA` does not exist.

- [ ] **Step 3: Implement CA initializer**

Reuse the existing OVS PKI CA creation path from `InitDefaultOVNIPsecCA`, but create `ovn-db-tls-ca`.

- [ ] **Step 4: Wire initializer**

Call `InitDefaultOVNDBTLSCA` when `ENABLE_SSL=true`.

- [ ] **Step 5: Run green test**

Run:

```bash
go test ./pkg/controller -run TestInitDefaultOVNDBTLSCA -count=1
```

Expected: PASS.

### Task 4: OVSDB Go client strict TLS configuration

**Files:**
- Modify: `pkg/util/const.go`
- Modify: `pkg/ovsdb/client/client.go`
- Test: `pkg/ovsdb/client/client_test.go`

- [ ] **Step 1: Write failing tests**

Add tests for TLS config creation:

```go
func TestNewTLSConfigUsesClientCertificateAndRootCA(t *testing.T) {}
func TestNewTLSConfigSetsServerNameWhenProvided(t *testing.T) {}
```

- [ ] **Step 2: Run red test**

Run:

```bash
go test ./pkg/ovsdb/client -run TestNewTLSConfig -count=1
```

Expected: FAIL because TLS config helper does not exist and current code uses `InsecureSkipVerify`.

- [ ] **Step 3: Implement TLS config helper**

Extract TLS config creation into a helper that loads:

```text
/var/run/tls/client.key
/var/run/tls/client.crt
/var/run/tls/ca.crt
```

and sets `RootCAs`, `Certificates`, and `ServerName` when known.

- [ ] **Step 4: Preserve upgrade compatibility**

If new client cert paths are absent, fall back to old:

```text
/var/run/tls/key
/var/run/tls/cert
/var/run/tls/cacert
```

- [ ] **Step 5: Run green test**

Run:

```bash
go test ./pkg/ovsdb/client -run TestNewTLSConfig -count=1
```

Expected: PASS.

### Task 5: 脚本和 manifest 接入

**Files:**
- Modify: `dist/images/start-db.sh`
- Modify: `dist/images/start-ovs.sh`
- Modify: `dist/images/start-controller.sh`
- Modify: `charts/kube-ovn/templates/*.yaml`

- [ ] **Step 1: Update script paths**

Change server-side OVN DB commands to use `server.key/server.crt/ca.crt`, and client-side commands to use `client.key/client.crt/ca.crt`.

- [ ] **Step 2: Add fallback**

Before using new paths, fallback to old `key/cert/cacert` if new files are absent.

- [ ] **Step 3: Update mounts**

Mount both old `kube-ovn-tls` and new OVN DB TLS material during upgrade.

- [ ] **Step 4: Run shell check through targeted render**

Run Helm template or existing chart test command for `ENABLE_SSL=true`.

### Task 6: Verification

**Files:**
- All modified files

- [ ] **Step 1: Run targeted tests**

```bash
go test ./pkg/controller ./pkg/ovsdb/client -count=1
```

- [ ] **Step 2: Run required lint**

```bash
make lint
```

- [ ] **Step 3: Inspect generated diff**

```bash
git diff --stat
git diff --check
```

Expected: no whitespace errors; no unrelated files changed.
