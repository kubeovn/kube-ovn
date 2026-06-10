# OVN DB TLS Daemon Certificate Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** daemon（kube-ovn-cni / ovs-ovn）侧实现 OVN DB TLS client/server 证书的申请和半生命周期自动轮转，受 `--enable-ovn-db-tls-cert-rotation` 开关控制。

**Architecture:** 新增 `pkg/daemon/ovndbtls.go`，复用 IPSec 的 CSR 创建/等待签发/验证写盘模式（`getSignedCert`、`storeCertificate`），但不复用 IPSec 的 key 目录/OVS 配置/strongswan 逻辑。daemon Controller 新增 workqueue 驱动轮转循环。证书文件写到 `/var/run/tls/` 下与脚本约定的路径一致。

**Tech Stack:** Go, crypto/x509, client-go CSR API, workqueue, pflag。

**前置:** controller 侧 `--enable-ovn-db-tls-cert` 开关已实现（前一个 plan）。本 plan 是 daemon/组件侧的消费者。

**scope note:** 本 plan 不涉及 start-db.sh / start-ovs.sh 脚本改造和 chart 证书目录挂载——那是独立的"脚本接入" plan。

---

## 文件结构

```
pkg/daemon/
├── config.go                # 修改：新增 EnableOVNDBTLSCertRotation 字段和 pflag
├── controller.go            # 修改：注册 informer、启动 worker
├── ovndbtls.go              # 新增：证书申请、验证、写盘、轮转循环
└── ovndbtls_test.go         # 新增：单元测试

dist/images/
└── (不变，脚本接入 plan 负责)
```

---

### Task 1: daemon config 新增轮转开关

**Files:**
- Modify: `pkg/daemon/config.go`（struct ~line 74，pflag ~line 139，赋值 ~line 204）
- Create: `pkg/daemon/ovndbtls_config_test.go`

- [ ] **Step 1: 写失败测试**

创建 `pkg/daemon/ovndbtls_config_test.go`：

```go
package daemon

import (
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestParseEnableSSLDaemon(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{name: "true", env: "true", want: true},
		{name: "false", env: "false", want: false},
		{name: "empty", env: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(util.EnvSSLEnabled, tt.env)
			if got := parseEnableSSLFromEnv(); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./pkg/daemon/ -count=1 -run TestParseEnableSSLDaemon
```

Expected: FAIL, `undefined: parseEnableSSLFromEnv`。

- [ ] **Step 3: 实现**

(a) `config.go` struct `Configuration` 在 `IPSecCertDuration int` 之后加：

```go
	EnableSSL                    bool
	EnableOVNDBTLSCertRotation   bool
```

(b) `config.go` pflag 区在 `argOVNIPSecCertDuration` 之后加：

```go
		argEnableOVNDBTLSCertRotation = pflag.Bool("enable-ovn-db-tls-cert-rotation", false, "Whether to enable automatic certificate rotation for OVN DB TLS, requires ENABLE_SSL=true")
```

(c) `config.go` 在 `ParseFlags` 函数之前新增：

```go
func parseEnableSSLFromEnv() bool {
	return os.Getenv(util.EnvSSLEnabled) == "true"
}
```

（config.go 已 import `os` 和 `util`。）

(d) struct 赋值处加：

```go
		EnableSSL:                    parseEnableSSLFromEnv(),
		EnableOVNDBTLSCertRotation:   *argEnableOVNDBTLSCertRotation,
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go build ./pkg/daemon/ && go test ./pkg/daemon/ -count=1 -run TestParseEnableSSLDaemon
```

- [ ] **Step 5: Commit**

```bash
git add pkg/daemon/config.go pkg/daemon/ovndbtls_config_test.go
git commit -s -m "feat(daemon): add enable-ovn-db-tls-cert-rotation flag and EnableSSL config"
```

---

### Task 2: ovndbtls 核心逻辑——证书申请、验证、写盘

**Files:**
- Create: `pkg/daemon/ovndbtls.go`
- Create: `pkg/daemon/ovndbtls_test.go`

这是最核心的文件。参考 `ipsec.go` 中 `getSignedCert`（创建 CSR → 等待签发）、`storeCertificate`（写盘）、`needNewCert`（检查是否需要新证书）、`untilCertRefresh`（计算下次刷新时间）的形态，但不引入 IPSec 的 OVS/strongswan 依赖。

- [ ] **Step 1: 写失败测试**

创建 `pkg/daemon/ovndbtls_test.go`：

```go
package daemon

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNeedNewOVNDBTLSCert(t *testing.T) {
	dir := t.TempDir()

	t.Run("no cert file", func(t *testing.T) {
		got, err := needNewOVNDBTLSCert(filepath.Join(dir, "missing.crt"), filepath.Join(dir, "missing.key"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Fatal("expected true when cert file does not exist")
		}
	})

	t.Run("valid cert not expired not past half", func(t *testing.T) {
		key, _ := rsa.GenerateKey(rand.Reader, 2048)
		tmpl := &x509.Certificate{
			SerialNumber:          big.NewInt(1),
			NotBefore:             time.Now().Add(-1 * time.Hour),
			NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
			BasicConstraintsValid: true,
		}
		certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

		certPath := filepath.Join(dir, "valid.crt")
		keyPath := filepath.Join(dir, "valid.key")
		os.WriteFile(certPath, certPEM, 0o600)
		os.WriteFile(keyPath, keyPEM, 0o600)

		got, err := needNewOVNDBTLSCert(certPath, keyPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Fatal("expected false for valid cert well before half-life")
		}
	})

	t.Run("cert past half life", func(t *testing.T) {
		key, _ := rsa.GenerateKey(rand.Reader, 2048)
		tmpl := &x509.Certificate{
			SerialNumber:          big.NewInt(2),
			NotBefore:             time.Now().Add(-6 * time.Hour),
			NotAfter:              time.Now().Add(4 * time.Hour),
			BasicConstraintsValid: true,
		}
		certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

		certPath := filepath.Join(dir, "old.crt")
		keyPath := filepath.Join(dir, "old.key")
		os.WriteFile(certPath, certPEM, 0o600)
		os.WriteFile(keyPath, keyPEM, 0o600)

		got, err := needNewOVNDBTLSCert(certPath, keyPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Fatal("expected true for cert past half-life")
		}
	})
}

func TestValidateNewOVNDBTLSCert(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)

	t.Run "valid cert matches key", func(t *testing.T) {
		tmpl := &x509.Certificate{
			SerialNumber:          big.NewInt(1),
			Subject:               pkix.Name{CommonName: "test"},
			NotBefore:             time.Now().Add(-time.Minute),
			NotAfter:              time.Now().Add(time.Hour),
			KeyUsage:              x509.KeyUsageDigitalSignature,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			BasicConstraintsValid: true,
		}
		certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

		if err := validateNewOVNDBTLSCert(certPEM, key, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
```

注意：上面测试中有语法错误（`t.Run "valid cert matches key"` 缺少括号），实现时修正为 `t.Run("valid cert matches key", func(t *testing.T) {`。

- [ ] **Step 2: 运行确认失败**

```bash
go test ./pkg/daemon/ -count=1 -run 'TestNeedNewOVNDBTLSCert|TestValidateNewOVNDBTLSCert'
```

- [ ] **Step 3: 实现 `pkg/daemon/ovndbtls.go`**

```go
package daemon

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	v1 "k8s.io/api/certificates/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	ovnDBTLSCertPath = util.SslClientCertPath // /var/run/tls/client.crt
	ovnDBTLSKeyPath  = util.SslClientKeyPath  // /var/run/tls/client.key
	ovnDBTLSCAPath   = util.SslCAPath         // /var/run/tls/ca.crt

	ovnDBTLSServerCertPath = util.SslServerCertPath // /var/run/tls/server.crt
	ovnDBTLSServerKeyPath  = util.SslServerKeyPath  // /var/run/tls/server.key
)

// shouldManageOVNDBTLSCert reports whether daemon-side OVN DB TLS cert
// management is active. The rotation switch is meaningless without SSL.
func (c *Controller) shouldManageOVNDBTLSCert() bool {
	if !c.config.EnableOVNDBTLSCertRotation {
		return false
	}
	if !c.config.EnableSSL {
		klog.Warning("enable-ovn-db-tls-cert-rotation requires ENABLE_SSL=true, ignored")
		return false
	}
	return true
}

// needNewOVNDBTLSCert returns true if the cert file does not exist, cannot be
// parsed, is expired, or has passed its half-life.
func needNewOVNDBTLSCert(certPath, keyPath string) (bool, error) {
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("failed to read cert: %w", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("failed to stat key: %w", err)
	}

	block, _ := pem.Decode(certBytes)
	if block == nil {
		return true, nil // corrupt
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true, nil // corrupt
	}
	if time.Now().After(cert.NotAfter) {
		return true, nil // expired
	}

	refreshTime := cert.NotBefore.Add(cert.NotAfter.Sub(cert.NotBefore) / 2)
	return time.Now().After(refreshTime), nil
}

// validateNewOVNDBTLSCert checks that a newly-signed certificate is valid
// before writing it to disk. Returns nil only when all checks pass.
func validateNewOVNDBTLSCert(certPEM []byte, privateKey *rsa.PrivateKey, expectedExtKeyUsage []x509.ExtKeyUsage) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return errors.New("failed to decode certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}
	if time.Now().After(cert.NotAfter) {
		return errors.New("certificate is already expired")
	}
	if !cert.PublicKey.(*rsa.PublicKey).Equal(privateKey.Public()) {
		return errors.New("certificate public key does not match private key")
	}
	// Check ExtKeyUsage: at least one of the expected usages must be present
	if len(expectedExtKeyUsage) > 0 {
		found := false
		for _, expected := range expectedExtKeyUsage {
			for _, actual := range cert.ExtKeyUsage {
				if actual == expected {
					found = true
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("certificate ExtKeyUsage %v does not contain any of %v", cert.ExtKeyUsage, expectedExtKeyUsage)
		}
	}
	return nil
}

// atomicWriteCert writes cert PEM and key to the target paths atomically
// (write to temp file, then rename).
func atomicWriteCert(certPath string, certPEM []byte, keyPath string, key *rsa.PrivateKey) error {
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	if err := atomicWriteFile(certPath, certPEM, 0o600); err != nil {
		return fmt.Errorf("failed to write cert: %w", err)
	}
	if err := atomicWriteFile(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("failed to write key: %w", err)
	}
	return nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// untilOVNDBTLSCertRefresh returns the duration until the certificate's
// half-life point.
func untilOVNDBTLSCertRefresh(certPath string) (time.Duration, error) {
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read certificate: %w", err)
	}
	block, _ := pem.Decode(certBytes)
	if block == nil {
		return 0, errors.New("failed to decode PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return 0, fmt.Errorf("failed to parse certificate: %w", err)
	}
	refreshTime := cert.NotBefore.Add(cert.NotAfter.Sub(cert.NotBefore) / 2)
	return time.Until(refreshTime), nil
}

// requestOVNDBTLSCSR creates a CSR, waits for the controller signer to sign
// it, and returns the signed certificate PEM.
func (c *Controller) requestOVNDBTLSCSR(ctx context.Context, csrName string, key *rsa.PrivateKey, usage v1.KeyUsage) ([]byte, error) {
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSR: %w", err)
	}

	csr := &v1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{Name: csrName},
		Spec: v1.CertificateSigningRequestSpec{
			Request:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}),
			SignerName: util.SignerName,
			Usages:     []v1.KeyUsage{usage},
		},
	}

	if _, err = c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Create(ctx, csr, metav1.CreateOptions{}); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("failed to create CSR: %w", err)
		}
		klog.Infof("CSR %s already exists", csrName)
	}

	defer func() {
		if err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Delete(context.Background(), csrName, metav1.DeleteOptions{}); err != nil {
			klog.Errorf("failed to delete CSR %s: %v", csrName, err)
		}
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for CSR %s: %w", csrName, ctx.Err())
		case <-ticker.C:
			got, err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Get(ctx, csrName, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to get CSR: %w", err)
			}
			if len(got.Status.Certificate) != 0 {
				return got.Status.Certificate, nil
			}
		}
	}
}
```

注意：`rand` 和 `context` 需要 import。`rand` 引用 `crypto/rand`（非 `math/rand`）。文件开头的 import 块里补上。实际上仔细看上面的代码，`x509.CreateCertificateRequest(rand.Reader, ...)` 已经用了 `crypto/rand`，需要确认 import 块包含 `"crypto/rand"`。最终 import 块为：

```go
import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	v1 "k8s.io/api/certificates/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)
```

同时删除未使用的 `"crypto"` import（`crypto.PrivateKey` 没有直接使用，`requestKey` 参数类型是 `*rsa.PrivateKey`）。

- [ ] **Step 4: 运行测试确认通过**

```bash
go build ./pkg/daemon/ && go test ./pkg/daemon/ -count=1 -run 'TestNeedNewOVNDBTLSCert|TestValidateNewOVNDBTLSCert' -v
```

- [ ] **Step 5: Commit**

```bash
git add pkg/daemon/ovndbtls.go pkg/daemon/ovndbtls_test.go
git commit -s -m "feat(daemon): add ovn db tls cert request, validation, and rotation helpers"
```

---

### Task 3: daemon Controller 注册轮转 queue 和 worker

**Files:**
- Modify: `pkg/daemon/controller.go`（struct 新增 queue 字段、Run 函数启动 worker）
- Modify: `pkg/daemon/ovndbtls.go`（新增 SyncOVNDBTLSCerts 入口函数）

- [ ] **Step 1: 在 `controller.go` 的 `Controller` struct 加 queue**

在 `ipsecQueue` 字段之后加：

```go
	ovnDBTLSQueue workqueue.TypedRateLimitingInterface[string]
```

- [ ] **Step 2: 在 `NewController` 中初始化 queue**

在 `ipsecQueue` 初始化行之后加：

```go
		ovnDBTLSQueue:     newTypedRateLimitingQueue[string]("OVNDBTLS", nil),
```

- [ ] **Step 3: 在 `controller.go` 的 Run 函数中启动 worker**

在 `go c.runIPSecWorker(ctx)` 行之后加：

```go
	if c.shouldManageOVNDBTLSCert() {
		go wait.Until(c.runOVNDBTLSWorker, time.Second, ctx.Done())
		c.ovnDBTLSQueue.Add("ovn-db-tls-client") // bootstrap client cert
	}
```

在 `c.ipsecQueue.ShutDown()` 之后加：

```go
	c.ovnDBTLSQueue.ShutDown()
```

在 `controller.go` 中添加 worker 函数（可以放在 `runIPSecWorker` 附近）：

```go
func (c *Controller) runOVNDBTLSWorker() {
	for c.processNextOVNDBTLSWorkItem() {
	}
}

func (c *Controller) processNextOVNDBTLSWorkItem() bool {
	key, shutdown := c.ovnDBTLSQueue.Get()
	if shutdown {
		return false
	}
	defer c.ovnDBTLSQueue.Done(key)

	if err := c.SyncOVNDBTLSCerts(key); err != nil {
		klog.Errorf("error syncing ovn db tls cert %s: %v", key, err)
		c.ovnDBTLSQueue.AddRateLimited(key)
		return true
	}
	c.ovnDBTLSQueue.Forget(key)
	return true
}
```

- [ ] **Step 4: 在 `ovndbtls.go` 新增 `SyncOVNDBTLSCerts`**

在文件末尾追加：

```go
// SyncOVNDBTLSCerts checks whether a new certificate is needed, requests one
// from the controller signer, validates it, writes it to disk, and schedules
// the next rotation check. On any failure, the old certificate stays in place.
func (c *Controller) SyncOVNDBTLSCerts(key string) error {
	var certPath, keyPath string
	var usage v1.KeyUsage
	var expectedExtKeyUsage []x509.ExtKeyUsage

	switch key {
	case "ovn-db-tls-client":
		certPath = ovnDBTLSCertPath
		keyPath = ovnDBTLSKeyPath
		usage = v1.UsageClientAuth
		expectedExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	default:
		return fmt.Errorf("unknown ovn db tls key: %s", key)
	}

	needsRenewal, err := needNewOVNDBTLSCert(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("check cert: %w", err)
	}
	if !needsRenewal {
		klog.V(4).Infof("ovn db tls cert %s still valid, skipping", key)
	} else {
		klog.Infof("requesting new ovn db tls cert for %s", key)
		newKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return fmt.Errorf("generate key: %w", err)
		}

		csrName := fmt.Sprintf("ovn-db-tls-client-%s", c.config.NodeName)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()

		certPEM, err := c.requestOVNDBTLSCSR(ctx, csrName, newKey, usage)
		if err != nil {
			return fmt.Errorf("request cert: %w", err)
		}

		if err := validateNewOVNDBTLSCert(certPEM, newKey, expectedExtKeyUsage); err != nil {
			return fmt.Errorf("validate new cert (discarding): %w", err)
		}

		if err := atomicWriteCert(certPath, certPEM, keyPath, newKey); err != nil {
			return fmt.Errorf("write cert: %w", err)
		}
		klog.Infof("ovn db tls cert %s written successfully", key)
	}

	// Schedule next check at half-life
	untilRefresh, err := untilOVNDBTLSCertRefresh(certPath)
	if err != nil {
		klog.Errorf("calculating cert refresh time for %s: %v", key, err)
		untilRefresh = 5 * time.Minute // fallback: retry soon
	}
	c.ovnDBTLSQueue.AddAfter(key, untilRefresh)
	return nil
}
```

- [ ] **Step 5: 编译确认**

```bash
go build ./pkg/daemon/
```

- [ ] **Step 6: Commit**

```bash
git add pkg/daemon/controller.go pkg/daemon/ovndbtls.go
git commit -s -m "feat(daemon): register ovn db tls cert rotation queue and worker"
```

---

### Task 4: 全量测试

- [ ] **Step 1: 运行 daemon 包测试**

```bash
go test ./pkg/daemon/ -count=1 -v -run 'TestNeedNewOVNDBTLSCert|TestValidateNewOVNDBTLSCert|TestParseEnableSSLDaemon'
```

Expected: 全部 PASS。

- [ ] **Step 2: 编译完整 daemon binary**

```bash
go build ./cmd/daemon/
```

Expected: 无错误。

- [ ] **Step 3: Commit（如有 lint fix）**

如果编译或测试触发了小修复，一起 commit。否则跳过。

---

## 后续 plan

- **脚本接入**：start-db.sh / start-ovs.sh 使用新证书路径（server.crt/key, client.crt/key, ca.crt），保留 legacy fallback；chart 挂载新证书目录。
- **server cert**：当前只实现了 client cert 轮转。server cert（ovn-central 侧）需要 start-db.sh bootstrap 或独立 Go helper 在 DB 启动前申请——这是脚本接入 plan 的一部分。
- **chart 暴露 `ENABLE_OVN_DB_TLS_CERT_ROTATION`**：在 values.yaml 和 daemonset template 中传参。
