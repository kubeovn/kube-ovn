# OVN DB TLS Script and Chart Integration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 start-db.sh / start-ovs.sh / start-controller.sh 从共享 `kube-ovn-tls`（`/var/run/tls/{cacert,cert,key}`）切换到分离的 server/client 证书路径（`server.crt/key`、`client.crt/key`、`ca.crt`），保留 legacy fallback 以支持升级过渡。

**Architecture:** shell 脚本按文件存在性选择路径——新三件（`client.crt` + `client.key` + `ca.crt`）齐全时使用新路径（严格校验），否则回退 legacy 共享证书（`InsecureSkipVerify`）。Chart 新增 `ovn-db-tls-ca` Secret 挂载和 `ENABLE_OVN_DB_TLS_CERT_ROTATION` 暴露。

**Tech Stack:** Bash, Helm YAML。

**前置:** daemon cert plan（ovndbtls.go）已实现，证书会写到 `/var/run/tls/` 的新路径。

---

## 文件结构

```
dist/images/
├── start-db.sh          # 修改：server cert 路径 + client cert（northd）路径
├── start-ovs.sh         # 修改：client cert 路径
├── start-controller.sh  # 修改：client cert 路径

charts/kube-ovn/
├── values.yaml          # 修改：新增 ENABLE_OVN_DB_TLS_CERT_ROTATION
└── templates/
    ├── central-deploy.yaml    # 修改：新增 ovn-db-tls-ca volume + 新证书路径
    ├── controller-deploy.yaml # 修改：新增 ovn-db-tls-ca volume
    └── ovs-ovn-ds.yaml        # 修改：新增 ovn-db-tls-ca volume + rotation flag
```

---

### Task 1: start-db.sh 接入新证书路径

**Files:**
- Modify: `dist/images/start-db.sh`

这是改动量最大的文件（~580 行）。核心变化：OVN DB server 和 northd 使用分离的证书路径。

- [ ] **Step 1: 新增证书路径选择函数**

在文件头部（`ENABLE_SSL` 读取之后）加一个辅助函数：

```bash
# Choose TLS certificate paths: if the new DB TLS files exist use them,
# otherwise fall back to the legacy kube-ovn-tls shared certs.
choose_tls_paths() {
  if [[ -f /var/run/tls/server.crt && -f /var/run/tls/server.key && -f /var/run/tls/ca.crt ]]; then
    TLS_SERVER_KEY=/var/run/tls/server.key
    TLS_SERVER_CERT=/var/run/tls/server.crt
    TLS_SERVER_CA=/var/run/tls/ca.crt
    TLS_CLIENT_KEY=/var/run/tls/client.key
    TLS_CLIENT_CERT=/var/run/tls/client.crt
    TLS_CLIENT_CA=/var/run/tls/ca.crt
    TLS_NORTHD_KEY=/var/run/tls/client.key
    TLS_NORTHD_CERT=/var/run/tls/client.crt
    TLS_NORTHD_CA=/var/run/tls/ca.crt
  else
    TLS_SERVER_KEY=/var/run/tls/key
    TLS_SERVER_CERT=/var/run/tls/cert
    TLS_SERVER_CA=/var/run/tls/cacert
    TLS_CLIENT_KEY=/var/run/tls/key
    TLS_CLIENT_CERT=/var/run/tls/cert
    TLS_CLIENT_CA=/var/run/tls/cacert
    TLS_NORTHD_KEY=/var/run/tls/key
    TLS_NORTHD_CERT=/var/run/tls/cert
    TLS_NORTHD_CA=/var/run/tls/cacert
  fi
}
```

在 `ENABLE_SSL=true` 的分支内调用 `choose_tls_paths`。

- [ ] **Step 2: 替换 start OVN DB 的 SSL 参数**

找到 `ovn-ctl` 启动 NB/SB DB 的位置（搜索 `ovn-nb-db-ssl-key`），将：

```bash
--ovn-nb-db-ssl-key=/var/run/tls/key \
--ovn-nb-db-ssl-cert=/var/run/tls/cert \
--ovn-nb-db-ssl-ca-cert=/var/run/tls/cacert \
--ovn-sb-db-ssl-key=/var/run/tls/key \
--ovn-sb-db-ssl-cert=/var/run/tls/cert \
--ovn-sb-db-ssl-ca-cert=/var/run/tls/cacert \
```

替换为使用变量：

```bash
--ovn-nb-db-ssl-key=$TLS_SERVER_KEY \
--ovn-nb-db-ssl-cert=$TLS_SERVER_CERT \
--ovn-nb-db-ssl-ca-cert=$TLS_SERVER_CA \
--ovn-sb-db-ssl-key=$TLS_SERVER_KEY \
--ovn-sb-db-ssl-cert=$TLS_SERVER_CERT \
--ovn-sb-db-ssl-ca-cert=$TLS_SERVER_CA \
```

同样替换 northd 的 SSL 参数：

```bash
--ovn-northd-ssl-key=$TLS_NORTHD_KEY \
--ovn-northd-ssl-cert=$TLS_NORTHD_CERT \
--ovn-northd-ssl-ca-cert=$TLS_NORTHD_CA \
```

- [ ] **Step 3: shellcheck 验证**

```bash
shellcheck -x dist/images/start-db.sh 2>&1 | head -20
```

如有 warning 且与本次改动相关则修复；预存的 warning 可忽略。

- [ ] **Step 4: Commit**

```bash
git add dist/images/start-db.sh
git commit -s -m "feat(start-db): use separate server/client cert paths for ovn db tls"
```

---

### Task 2: start-ovs.sh 和 start-controller.sh 接入新证书路径

**Files:**
- Modify: `dist/images/start-ovs.sh`（~159 行）
- Modify: `dist/images/start-controller.sh`（~41 行）

- [ ] **Step 1: start-ovs.sh**

(a) 在 `ENABLE_SSL` 判断块内，替换 `ovn-controller` SSL 参数（搜索 `ovn-controller-ssl-key`）：

原：
```bash
/usr/share/ovn/scripts/ovn-ctl --ovn-controller-ssl-key=/var/run/tls/key --ovn-controller-ssl-cert=/var/run/tls/cert --ovn-controller-ssl-ca-cert=/var/run/tls/cacert ...
```

替换为（使用同样的 `choose_tls_paths` 逻辑，或直接内联判断）：

```bash
# Use new client cert paths if available, otherwise legacy
OVS_SSL_KEY=/var/run/tls/key
OVS_SSL_CERT=/var/run/tls/cert
OVS_SSL_CA=/var/run/tls/cacert
if [[ -f /var/run/tls/client.crt && -f /var/run/tls/client.key && -f /var/run/tls/ca.crt ]]; then
  OVS_SSL_KEY=/var/run/tls/client.key
  OVS_SSL_CERT=/var/run/tls/client.crt
  OVS_SSL_CA=/var/run/tls/ca.crt
fi
```

然后 ovn-ctl 命令使用 `${OVS_SSL_KEY}` 等变量。

(b) 同样替换 `ovn-controller` 的 `ssl:` 连接地址（如果脚本里有构造 ssl: URL 的地方，它们已经通过 `ENABLE_SSL` 控制，不需要改路径，只需要确认它们仍然工作）。

- [ ] **Step 2: start-controller.sh**

这个脚本较短，主要设置 `OVN_NB_ADDR` 和 `OVN_SB_ADDR`。SSL 地址格式（`ssl:host:port`）不变，证书加载由 Go binary 的 `pkg/ovsdb/client` 通过文件存在性自动选择，脚本不需要改证书路径。

确认脚本内容——如果它只传地址不传证书路径，则不需要改动，跳过 commit。

- [ ] **Step 3: Commit（如有改动）**

```bash
git add dist/images/start-ovs.sh
git commit -s -m "feat(start-ovs): use separate client cert paths for ovn db tls"
```

如果 start-controller.sh 无需改动则只提交 start-ovs.sh。

---

### Task 3: Chart 挂载新证书和暴露 rotation 开关

**Files:**
- Modify: `charts/kube-ovn/values.yaml`
- Modify: `charts/kube-ovn/templates/central-deploy.yaml`
- Modify: `charts/kube-ovn/templates/controller-deploy.yaml`
- Modify: `charts/kube-ovn/templates/ovs-ovn-ds.yaml`

- [ ] **Step 1: values.yaml 新增 rotation 开关**

在 `func` 段 `ENABLE_OVN_DB_TLS_CERT: false` 之后加：

```yaml
  ENABLE_OVN_DB_TLS_CERT_ROTATION: false
```

- [ ] **Step 2: central-deploy.yaml 新增 CA volume**

ovn-central 需要：
1. 保留现有的 `kube-ovn-tls` volume（legacy fallback）
2. 新增 `ovn-db-tls-ca` Secret volume（提供 CA 给 daemon 申请证书时读取）
3. 新增 emptyDir 或与 kube-ovn-tls 共享 `/var/run/tls`（daemon 写入新证书文件后脚本读取）

最简做法：将现有 `/var/run/tls` 挂载改为读写（如果当前是 readonly 则需要改），daemon 的 ovndbtls 代码直接往这个目录写新文件。

检查当前 central-deploy.yaml 中 `/var/run/tls` 的挂载方式：
- 如果是从 `kube-ovn-tls` Secret 挂载且 readonly，则需要改为 emptyDir + initContainer 从 Secret 复制文件，这样 daemon 才能往同一目录写新文件。
- 如果已经是可写的，直接加 CA secret 挂载。

**推荐方案**：添加一个 initContainer 将 `kube-ovn-tls` 的文件复制到 emptyDir `/var/run/tls`，然后主容器挂载这个 emptyDir。这样 daemon 可以在运行时往同一目录写入新证书文件。

```yaml
# 新增 initContainer
initContainers:
- name: copy-tls
  image: {{ .Values.images.kubeOvn }}
  command: ['sh', '-c', 'cp -L /tls-source/* /var/run/tls/ 2>/dev/null || true']
  volumeMounts:
  - name: kube-ovn-tls-source
    mountPath: /tls-source
  - name: tls-dir
    mountPath: /var/run/tls

# volumes 新增
- name: kube-ovn-tls-source
  secret:
    secretName: kube-ovn-tls
    optional: true
- name: tls-dir
  emptyDir: {}
```

主容器 volumeMounts 改为挂载 `tls-dir`（emptyDir）。

**注意**：这个改动需要仔细对照现有 YAML 结构，确保不破坏非 SSL 模式下的行为。如果改动过于侵入，第一阶段可以简化为只在 `ENABLE_OVN_DB_TLS_CERT` 为 true 时才使用 emptyDir（通过 Helm 条件渲染）。

- [ ] **Step 3: controller-deploy.yaml**

kube-ovn-controller 的 `/var/run/tls` 同样需要可写（daemon 模式的 ovndbtls 代码需要往里写 client cert）。如果 controller 也需要运行时写证书，同样的 emptyDir + initContainer 模式。

但 kube-ovn-controller 的 TLS 配置是由 Go binary 的 `pkg/ovsdb/client` 通过文件存在性选择路径——controller binary 本身不运行 daemon 的 ovndbtls 代码。controller 的 client cert 需要由单独的机制申请（可能是一个 sidecar 或 initContainer 调 CSR API）。

**第一阶段简化**：controller 保持从 `kube-ovn-tls` Secret 挂载（legacy 路径），新证书路径暂时只给 ovs-ovn 和 ovn-central 使用。Controller 的 client cert 签发由后续任务完成。

- [ ] **Step 4: ovs-ovn-ds.yaml 新增 rotation flag 和 CA**

在 ovs-ovn daemonset 中：
(a) args 加 `--enable-ovn-db-tls-cert-rotation={{ .Values.func.ENABLE_OVN_DB_TLS_CERT_ROTATION }}`
(b) 确保 `/var/run/tls` 挂载为可写（ovs-ovn daemon 的 ovndbtls 代码需要往里写 client cert）

- [ ] **Step 5: Commit**

```bash
git add charts/kube-ovn/
git commit -s -m "chore(chart): mount ovn db tls volumes and expose rotation switch"
```

---

### Task 4: 验证

- [ ] **Step 1: Helm 模板渲染**

```bash
helm template test charts/kube-ovn --set networking.ENABLE_SSL=true --set func.ENABLE_OVN_DB_TLS_CERT=true --set func.ENABLE_OVN_DB_TLS_CERT_ROTATION=true 2>/dev/null | grep -E "enable-ovn-db-tls|tls-dir|kube-ovn-tls-source" | head -20
```

确认：
- controller 有 `--enable-ovn-db-tls-cert=true`
- ovs-ovn 有 `--enable-ovn-db-tls-cert-rotation=true`
- ovn-central 有新的 volume 结构

- [ ] **Step 2: 非 SSL 模式渲染不退化**

```bash
helm template test charts/kube-ovn 2>/dev/null | grep -c "enable-ovn-db-tls"
```

确认默认值都是 false。

- [ ] **Step 3: shellcheck 两个改过的脚本**

```bash
shellcheck -x dist/images/start-db.sh dist/images/start-ovs.sh 2>&1 | head -20
```
