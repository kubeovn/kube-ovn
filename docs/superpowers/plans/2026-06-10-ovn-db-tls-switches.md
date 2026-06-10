# OVN DB TLS Cert Switch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 引入 `--enable-ovn-db-tls-cert` 开关（默认 false），把 OVN DB TLS 证书管理（CA 初始化 + CSR signer）从 `ENABLE_SSL` 中解耦，并收敛 controller 内散落的 `os.Getenv(util.EnvSSLEnabled)`。

**Architecture:** `Configuration` 新增 `EnableSSL`（ParseFlags 读一次 env）和 `EnableOVNDBTLSCert`（pflag）两个字段；CA 初始化与 CSR handler 注册 gate 在两者同时为 true；SSL 关闭时开关无效打 warning。Chart 在 `values.yaml` 的 `func` 段暴露。

**Tech Stack:** Go pflag、client-go fake clientset、Helm chart。

**Scope note:** 本 plan 只覆盖 controller 开关接入。daemon 侧证书申请/轮转（`--enable-ovn-db-tls-cert-rotation`）和 start-db.sh bootstrap 接入是独立子系统，各自单独出 plan。

**前置状态:** 工作区有未提交的 review 修复（pki.go PKCS#8、signer SAN 校验、ovsdb client ServerName 等），Task 1 先把它们入库。

---

### Task 1: 提交已有的 review 修复

**Files:**
- 已修改（工作区）: `pkg/controller/pki.go`, `pkg/controller/pki_test.go`, `pkg/controller/signer.go`, `pkg/controller/signer_test.go`, `pkg/ovsdb/client/client.go`, `pkg/ovsdb/client/client_test.go`, `pkg/controller/controller.go`

- [ ] **Step 1: 还原 controller.go 中的临时改动**

之前调试时把 CSR handler 条件临时改成了 `config.EnableOVNIPSec || os.Getenv(util.EnvSSLEnabled) == "true"`（controller.go:1053 附近）。本 plan Task 3 会用 config 字段重做这个 gate，先还原为原始条件避免和修复混在一个 commit：

```go
	if config.EnableOVNIPSec {
		if _, err = csrInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueAddCsr,
			UpdateFunc: controller.enqueueUpdateCsr,
			// no need to add delete func for csr
		}); err != nil {
			util.LogFatalAndExit(err, "failed to add csr event handler")
		}
	}
```

（即删掉 `|| os.Getenv(util.EnvSSLEnabled) == "true"` 和上面的注释行；`os` import 如因此不再使用则一并移除——注意 controller.go 其他位置仍在用 `os.Getenv`，大概率保留。）

- [ ] **Step 2: 运行受影响包的测试**

```bash
go build ./pkg/controller/ ./pkg/ovsdb/client/
go test ./pkg/controller/ -count=1 -run 'TestGenerateCACertificate|TestGeneratedCAIsConsumableBySigner|TestEnsureOVNCASecret|TestCSRProfileDetection|TestNewCertificateTemplateForProfile|TestValidateCSRForProfile'
go test ./pkg/ovsdb/client/ -count=1
```

Expected: 全部 PASS。

- [ ] **Step 3: 分两个 commit 提交**

```bash
git add pkg/controller/pki.go pkg/controller/pki_test.go
git commit -s -m "fix(pki): generate PKCS#8 CA key consumable by signer

- marshal CA private key as PKCS#8 to match decodePrivateKey
- tolerate IsAlreadyExists on concurrent secret creation
- add signer round-trip test for generated CA"

git add pkg/controller/signer.go pkg/controller/signer_test.go pkg/ovsdb/client/client.go pkg/ovsdb/client/client_test.go pkg/controller/controller.go
git commit -s -m "fix(signer): validate CSR SANs per profile and fix ovsdb tls config

- reject server CSR SANs outside ovn-nb/ovn-sb services and node IPs
- reject client/ipsec CSRs carrying SANs
- leave tls.Config ServerName empty so each HA endpoint verifies correctly
- warn when ovn db tls client files are only partially present"
```

---

### Task 2: Configuration 新增 EnableSSL 和 EnableOVNDBTLSCert

**Files:**
- Modify: `pkg/controller/config.go`（struct ~line 100，pflag ~line 207，赋值 ~line 315）
- Create: `pkg/controller/config_switch_test.go`

- [ ] **Step 1: 写失败的测试**

`pkg/controller/config.go` 的 `ParseFlags` 会操作全局 pflag/klog，不适合直接在单测里调用。把 env→bool 的解析抽成独立函数来测。创建 `pkg/controller/config_switch_test.go`：

```go
package controller

import (
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestParseEnableSSLFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{name: "enabled", env: "true", want: true},
		{name: "disabled", env: "false", want: false},
		{name: "unset", env: "", want: false},
		{name: "garbage", env: "yes", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(util.EnvSSLEnabled, tt.env)
			if got := parseEnableSSLFromEnv(); got != tt.want {
				t.Fatalf("parseEnableSSLFromEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./pkg/controller/ -count=1 -run TestParseEnableSSLFromEnv
```

Expected: FAIL，`undefined: parseEnableSSLFromEnv`。

- [ ] **Step 3: 实现**

`pkg/controller/config.go` 三处修改。

(a) struct 字段，加在 `CertManagerIPSecCert bool` 之后：

```go
	EnableOVNIPSec              bool
	CertManagerIPSecCert        bool
	EnableSSL                   bool
	EnableOVNDBTLSCert          bool
	EnableLiveMigrationOptimize bool
```

(b) pflag 定义，加在 `argCertManagerIPSecCert` 之后：

```go
		argEnableOVNDBTLSCert = pflag.Bool("enable-ovn-db-tls-cert", false, "Whether to enable built-in certificate management (CA and CSR signer) for OVN DB TLS, requires ENABLE_SSL=true")
```

(c) 文件内新增解析函数（放在 `ParseFlags` 之前）：

```go
// parseEnableSSLFromEnv reads ENABLE_SSL once so the rest of the controller
// can rely on Configuration.EnableSSL instead of scattered os.Getenv calls.
func parseEnableSSLFromEnv() bool {
	return os.Getenv(util.EnvSSLEnabled) == "true"
}
```

（`config.go` 已 import `os` 和 `util`，无需新增 import。）

(d) `Configuration` 字面量赋值，加在 `CertManagerIPSecCert` 行后：

```go
		EnableOVNIPSec:                 *argEnableOVNIPSec,
		CertManagerIPSecCert:           *argCertManagerIPSecCert,
		EnableSSL:                      parseEnableSSLFromEnv(),
		EnableOVNDBTLSCert:             *argEnableOVNDBTLSCert,
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go build ./pkg/controller/ && go test ./pkg/controller/ -count=1 -run TestParseEnableSSLFromEnv
```

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add pkg/controller/config.go pkg/controller/config_switch_test.go
git commit -s -m "feat(controller): add enable-ovn-db-tls-cert flag and EnableSSL config field"
```

---

### Task 3: 用 config 字段 gate CA 初始化和 CSR handler

**Files:**
- Modify: `pkg/controller/controller.go`（CSR handler ~line 1052，CA init ~line 1131）
- Modify: `pkg/controller/pki.go`（新增 gate 辅助方法）
- Test: `pkg/controller/pki_test.go`

- [ ] **Step 1: 写失败的测试**

gate 逻辑（含 warning 路径）抽成可测的纯函数。在 `pkg/controller/pki_test.go` 追加：

```go
func TestShouldManageOVNDBTLSCert(t *testing.T) {
	tests := []struct {
		name      string
		enableSSL bool
		enableTLS bool
		want      bool
	}{
		{name: "both enabled", enableSSL: true, enableTLS: true, want: true},
		{name: "ssl only keeps legacy path", enableSSL: true, enableTLS: false, want: false},
		{name: "tls cert without ssl is ignored", enableSSL: false, enableTLS: true, want: false},
		{name: "both disabled", enableSSL: false, enableTLS: false, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Controller{
				config: &Configuration{
					EnableSSL:          tt.enableSSL,
					EnableOVNDBTLSCert: tt.enableTLS,
				},
			}
			if got := c.shouldManageOVNDBTLSCert(); got != tt.want {
				t.Fatalf("shouldManageOVNDBTLSCert() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./pkg/controller/ -count=1 -run TestShouldManageOVNDBTLSCert
```

Expected: FAIL，`undefined: shouldManageOVNDBTLSCert`。

- [ ] **Step 3: 实现 gate 方法**

在 `pkg/controller/pki.go` 的 `InitDefaultOVNDBTLSCA` 之后追加：

```go
// shouldManageOVNDBTLSCert reports whether built-in OVN DB TLS certificate
// management (CA + CSR signer) is active. The cert switch is meaningless
// without SSL, so it is ignored (with a warning at startup) in that case.
func (c *Controller) shouldManageOVNDBTLSCert() bool {
	if !c.config.EnableOVNDBTLSCert {
		return false
	}
	if !c.config.EnableSSL {
		klog.Warning("enable-ovn-db-tls-cert requires ENABLE_SSL=true, ignored")
		return false
	}
	return true
}
```

- [ ] **Step 4: 接入两个 gate 点**

(a) `pkg/controller/controller.go` ~line 1131，替换现有 env 判断：

```go
	if c.shouldManageOVNDBTLSCert() {
		if err := c.InitDefaultOVNDBTLSCA(); err != nil {
			util.LogFatalAndExit(err, "failed to init ovn db tls CA")
		}
	}
```

（原代码为 `if os.Getenv(util.EnvSSLEnabled) == "true" {`，整块条件替换。）

(b) `pkg/controller/controller.go` ~line 1052，CSR handler 注册条件。注意此处 `controller` 是局部变量（非 `c`），沿用局部变量调用：

```go
	// the signer serves both IPSec CSRs and OVN DB TLS CSRs
	if config.EnableOVNIPSec || controller.shouldManageOVNDBTLSCert() {
		if _, err = csrInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueAddCsr,
			UpdateFunc: controller.enqueueUpdateCsr,
			// no need to add delete func for csr
		}); err != nil {
			util.LogFatalAndExit(err, "failed to add csr event handler")
		}
	}
```

(c) 确认 `controller.go` 顶部 import 的 `os` 是否仍被其他代码使用：

```bash
grep -n "os\." pkg/controller/controller.go | grep -v "_test"
```

若仅剩这次删掉的调用则移除 `"os"` import；否则保留。

- [ ] **Step 5: 运行测试确认通过**

```bash
go build ./pkg/controller/ && go test ./pkg/controller/ -count=1 -run 'TestShouldManageOVNDBTLSCert|TestGenerateCACertificate|TestEnsureOVNCASecret'
```

Expected: 全部 PASS。

- [ ] **Step 6: Commit**

```bash
git add pkg/controller/controller.go pkg/controller/pki.go pkg/controller/pki_test.go
git commit -s -m "feat(controller): gate ovn db tls cert management behind explicit switch"
```

---

### Task 4: Chart 暴露开关

**Files:**
- Modify: `charts/kube-ovn/values.yaml`（`func` 段，~line 115 `ENABLE_OVN_IPSEC` 附近）
- Modify: `charts/kube-ovn/templates/controller-deploy.yaml`（args，~line 150 `--enable-ovn-ipsec` 附近）

- [ ] **Step 1: values.yaml 加配置项**

在 `func` 段 `ENABLE_OVN_IPSEC: false` 之后加：

```yaml
  ENABLE_OVN_DB_TLS_CERT: false
```

- [ ] **Step 2: controller-deploy.yaml 传参**

在 `- --enable-ovn-ipsec={{- .Values.func.ENABLE_OVN_IPSEC }}` 之后加：

```yaml
          - --enable-ovn-db-tls-cert={{- .Values.func.ENABLE_OVN_DB_TLS_CERT }}
```

- [ ] **Step 3: 渲染验证**

```bash
helm template test charts/kube-ovn --set func.ENABLE_OVN_DB_TLS_CERT=true 2>/dev/null | grep "enable-ovn-db-tls-cert"
helm template test charts/kube-ovn 2>/dev/null | grep "enable-ovn-db-tls-cert"
```

Expected: 第一条输出 `- --enable-ovn-db-tls-cert=true`；第二条输出 `- --enable-ovn-db-tls-cert=false`。

- [ ] **Step 4: Commit**

```bash
git add charts/kube-ovn/values.yaml charts/kube-ovn/templates/controller-deploy.yaml
git commit -s -m "chore(chart): expose ENABLE_OVN_DB_TLS_CERT switch"
```

---

### Task 5: 全量验证

**Files:** 无新改动。

- [ ] **Step 1: 跑受影响包全量测试**

```bash
go test ./pkg/controller/ ./pkg/ovsdb/client/ -count=1
```

Expected: 全部 PASS（pkg/controller 较慢，约 1-2 分钟）。

- [ ] **Step 2: make lint**

```bash
make lint 2>&1 | tail -15
```

Expected: 仅存在两个预存的 gosec G117 告警（`cmd/frr/frr.go:31`、`pkg/apis/kubeovn/v1/bgp_conf.go:30`，与本改动无关）。若出现新告警则修复后重跑。

- [ ] **Step 3: 检查 diff 卫生**

```bash
git diff --check 546192b0a..HEAD
git log --oneline 546192b0a..HEAD
```

Expected: 无 whitespace 错误；commit 序列为 Task 1 的两个 fix、Task 2-4 各一个。

- [ ] **Step 4: （可选，有 kind 环境时）集群冒烟**

```bash
make build-go && make build-kube-ovn
# kind load 镜像并重启 kube-ovn-controller 后：
kubectl -n kube-system logs -l app=kube-ovn-controller --tail=50 | grep -i "tls"
```

Expected:
- chart 未开 `ENABLE_OVN_DB_TLS_CERT` 时：不再出现 `OVN DB TLS CA secret init successfully`，`ovn-db-tls-ca` secret 不创建（先删掉验证期残留：`kubectl -n kube-system delete secret ovn-db-tls-ca --ignore-not-found`）。
- 开了之后：CA secret 创建，提交 `ovn-db-tls-server-*` CSR 能被签发。

---

## 后续 plan（不在本 plan 内）

- **daemon 证书申请与轮转**：`--enable-ovn-db-tls-cert-rotation` 开关、ovs-ovn client cert、ovn-central server cert、半生命周期刷新与"验证后才重启"不变式（spec「轮转设计」章节）。
- **脚本与 manifest 接入**：start-db.sh / start-ovs.sh 新证书路径、bootstrap CSR 流程、证书目录挂载（spec「OVN 命令形态」「启用与回滚」章节）。
