# OVN DB TLS 证书生命周期设计

## 背景

Kube-OVN 当前已经支持通过 `ENABLE_SSL=true` 将 OVN NB/SB 数据库连接从 TCP 切换为 SSL：

- `start-db.sh` 会把数据库监听从 `ptcp` 切到 `pssl`。
- `start-ovs.sh` 会把 `ovn-controller` 连接 SB 的地址从 `tcp` 切到 `ssl`。
- `start-controller.sh` 会把 `ssl:` 格式的 NB/SB 地址传给 `kube-ovn-controller`。
- Helm 和 `install.sh` 会生成或复用 `kube-ovn-tls` Secret，并挂载到 `/var/run/tls`。

当前实现的主要问题是：

- `kube-ovn-tls` 是共享证书，服务端和客户端长期共用同一套 `cacert/cert/key`。
- 证书生命周期主要依赖安装阶段创建和手动替换，没有自动续签和轮转。
- 现有 Go OVSDB client 使用 `InsecureSkipVerify: true`，没有严格校验服务端身份。
- 从非 SSL 或旧共享证书方案升级到自动轮转证书方案，需要明确平滑迁移流程。

Kube-OVN 内部已经有一套给 OVN IPSec 使用的证书签发和轮转流程：

- `ovn-ipsec-ca` 保存自签 CA。
- daemon 生成私钥和 CSR。
- daemon 创建 Kubernetes `CertificateSigningRequest`。
- controller 自动 approve，并用 `kubeovn.io/signer` 签发匹配的 CSR。
- daemon 在证书生命周期过半后自动刷新证书。

OVN DB TLS 可以复用这套 CSR/signer/半生命周期刷新机制，但需要独立的 CA、证书用途和校验逻辑，不能直接复用 IPSec 证书。

## 目标

- 保持 `ENABLE_SSL` 作为 OVN DB SSL/TLS 的唯一功能开关。
- 在 `ENABLE_SSL=true` 时，将 OVN DB TLS 从共享证书扩展为 mTLS。
- 由 Kube-OVN 自己管理 OVN DB TLS 证书创建和轮转，不依赖 cert-manager。
- 复用现有 OVN IPSec 的 CSR/signer/半生命周期刷新机制。
- 为 OVN DB server 和各访问组件签发不同用途的证书。
- 支持证书不存在、损坏、非当前 CA 签发、生命周期过半时自动重新申请。
- 保留从现有 `kube-ovn-tls` 共享证书方案平滑升级的路径。
- 保留从 `ENABLE_SSL=false` 升级到 `ENABLE_SSL=true` 的路径。
- 不拆分 OVN NB/SB 访问端口，继续使用现有 6641/6642。

## 非目标

- 不新增 `ENABLE_OVN_DB_TLS` 这类第二个 TLS 开关。
- 不依赖 cert-manager。Kube-OVN 可能需要在 cert-manager 可用前先启动。
- 不使用 Kubernetes 控制面 CA 私钥给 OVN DB TLS 签证书。
- 不直接复用 OVN IPSec 证书或 `ovn-ipsec-ca`。
- 不启用 OVN SB DB `role=ovn-controller`。
- 不实现 OVN SB DB 表/字段级 RBAC。
- 不拆分多个 SB DB listener 或多个端口。

## 配置

继续复用现有配置：

```yaml
networking:
  ENABLE_SSL: false
```

字段语义：

- `ENABLE_SSL=false`：OVN DB 使用 `tcp/ptcp`。
- `ENABLE_SSL=true`：OVN DB 使用 `ssl/pssl`，并启用 OVN DB TLS 证书管理。

第一阶段不新增 `SSL_CERT_SOURCE`、`SSL_SECRET_NAME`、`SSL_STRICT_VERIFY`：

- `SSL_CERT_SOURCE` 暂不需要。证书统一由 kube-ovn 自管理；升级兼容通过旧 `kube-ovn-tls` 迁移路径处理。
- `SSL_SECRET_NAME` 暂不需要。旧共享证书仍固定为 `kube-ovn-tls`，新自动轮转证书使用内部固定资源名。
- `SSL_STRICT_VERIFY` 暂不需要。严格校验是 `ENABLE_SSL=true` 下 mTLS 方案的一部分，不再额外暴露一个容易产生组合状态的开关。

## 证书模型

新增 OVN DB TLS 专用 CA：

```text
Secret: ovn-db-tls-ca
data:
  cacert
  cakey
```

该 CA 只用于 OVN DB TLS，不复用 `ovn-ipsec-ca`。

证书分为两类：

```text
server cert:
  给 ovn-central / OVN DB 使用
  ExtendedKeyUsage = serverAuth

client cert:
  给 kube-ovn-controller / ovs-ovn / monitor / pinger 使用
  ExtendedKeyUsage = clientAuth
```

server cert 的 SAN 至少覆盖：

- `ovn-nb`
- `ovn-sb`
- `ovn-nb.<namespace>`
- `ovn-sb.<namespace>`
- `ovn-nb.<namespace>.svc`
- `ovn-sb.<namespace>.svc`
- 配置的 OVN DB VIP 或外部 endpoint
- hostNetwork、raft 或多副本模式实际会使用到的 pod IP 或 node IP

client cert 的 CN 建议表达组件身份：

- `kube-ovn-controller`
- `ovs-ovn:<nodeName>`
- `kube-ovn-monitor`
- `kube-ovn-pinger:<nodeName>`
- `kube-ovn-ic-controller`

第一阶段只在 TLS 握手阶段校验证书是否合法，不基于 CN 做 OVN DB 表/字段级授权。

## 鉴权语义

本方案实现的是 mTLS 连接层证书鉴权：

```text
client 连接 OVN DB
  |
  | 带 client cert
  v
OVN DB 校验:
  - client cert 是否由 ovn-db-tls-ca 签发
  - client cert 是否过期
  - client cert 是否允许 clientAuth
```

同时 client 校验 OVN DB server cert：

```text
client 校验:
  - server cert 是否由 ovn-db-tls-ca 签发
  - server cert 是否过期
  - server cert 是否允许 serverAuth
  - server cert SAN 是否匹配连接地址
```

该方案解决的问题是：

```text
未持有本集群 kube-ovn 签发的合法 client cert，不能建立 OVN DB TLS 连接。
```

该方案不解决的问题是：

```text
已经通过 mTLS 连接后，不区分不同组件能读写哪些 OVN DB 表或字段。
```

OVN SB DB `role/RBAC` 属于表/字段级授权，第一阶段不做。

## 签发和轮转流程

复用现有 OVN IPSec 的流程形态，但新增 OVN DB TLS 专用 profile。

组件侧流程：

```text
组件启动
  |
  | 检查本地证书
  v
证书不存在 / 损坏 / 非当前 CA 签发 / 生命周期过半
  |
  | 生成 private key + CSR
  v
创建 Kubernetes CSR
  |
  | signerName = kubeovn.io/signer
  | profile = ovn-db-tls
  v
等待 kube-ovn-controller 签发
  |
  v
写入本地证书文件
  |
  v
启动或重启对应 OVN/OVS 进程
```

controller signer 流程：

```text
watch CSR
  |
  | 识别 ovn-db-tls profile
  v
校验 CSR 名称、组件身份、node 是否存在、usage、SAN
  |
  v
approve CSR
  |
  v
使用 ovn-db-tls-ca 签发证书
  |
  v
写入 CSR status.certificate
```

触发重新申请的条件：

- 证书文件不存在。
- 私钥文件不存在。
- 证书 PEM 损坏或无法解析。
- 证书不在有效期内。
- 证书不是当前 `ovn-db-tls-ca` 签发。
- 证书生命周期已经超过一半。
- server cert 的 SAN 不覆盖当前连接地址。

第一阶段允许证书更新后重启对应进程，不强求无感 reload：

- `ovn-central` server cert 更新后重启 `ovn-northd` / OVN DB 相关进程或滚动 `ovn-central`。
- `ovs-ovn` client cert 更新后重启 `ovn-controller`。
- `kube-ovn-controller`、`monitor`、`pinger` client cert 更新后重启自身进程或 Pod。

## OVN 命令形态

OVN DB server 使用 server 证书和 CA：

```bash
/usr/share/ovn/scripts/ovn-ctl \
  --ovn-nb-db-ssl-key=/var/run/tls/server.key \
  --ovn-nb-db-ssl-cert=/var/run/tls/server.crt \
  --ovn-nb-db-ssl-ca-cert=/var/run/tls/ca.crt \
  --ovn-sb-db-ssl-key=/var/run/tls/server.key \
  --ovn-sb-db-ssl-cert=/var/run/tls/server.crt \
  --ovn-sb-db-ssl-ca-cert=/var/run/tls/ca.crt \
  --ovn-northd-ssl-key=/var/run/tls/client.key \
  --ovn-northd-ssl-cert=/var/run/tls/client.crt \
  --ovn-northd-ssl-ca-cert=/var/run/tls/ca.crt \
  restart_northd
```

NB/SB DB 继续使用现有端口：

```bash
ovn-nbctl ... set-connection pssl:6641:[::]
ovn-sbctl ... set-connection pssl:6642:[::]
```

`ovn-controller` 使用 client 证书：

```bash
/usr/share/ovn/scripts/ovn-ctl \
  --ovn-controller-ssl-key=/var/run/tls/client.key \
  --ovn-controller-ssl-cert=/var/run/tls/client.crt \
  --ovn-controller-ssl-ca-cert=/var/run/tls/ca.crt \
  restart_controller
```

Go OVSDB client 加载：

```text
/var/run/tls/client.key
/var/run/tls/client.crt
/var/run/tls/ca.crt
```

并移除 `InsecureSkipVerify: true`，改为严格校验 server cert 和 SAN。

## Bootstrap 流程

`ENABLE_SSL=true` 时，`ovn-central` 在启动 `pssl` 监听前需要 server cert。因此 bootstrap 不能依赖一个必须先连上 OVN DB 才能工作的流程。

处理原则：

1. `ovn-db-tls-ca` 由安装器或 controller 在 Kubernetes API 可用时创建。
2. `ovn-central` 在启动 OVN DB 前先确保 server cert 已存在。
3. 如果 server cert 不存在，`ovn-central` 侧的启动脚本或 init 流程先走 CSR 签发。
4. server cert 准备完成后再启动 `pssl` listener。
5. 其他组件再申请 client cert，并通过 `ssl:` 连接 OVN DB。

为了避免 bootstrap 循环，OVN DB TLS 的 signer 只能依赖 Kubernetes API 和 `ovn-db-tls-ca`，不能依赖 OVN DB 已经可用。

## 升级策略

### 从 `ENABLE_SSL=false` 升级

推荐分阶段迁移：

1. 升级 Kube-OVN 镜像和 manifests，保持 `ENABLE_SSL=false`。
2. 创建 `ovn-db-tls-ca`。
3. 预生成或等待 `ovn-central` 申请 server cert。
4. 等待各客户端组件申请 client cert。
5. 将 `ENABLE_SSL` 切为 `true`。
6. 先滚动 `ovn-central`，确认 NB/SB DB 开放 `pssl:6641/6642`。
7. 再滚动 `kube-ovn-controller`、`ovs-ovn`、`monitor`、`pinger`。
8. 验证所有连接地址为 `ssl:`，并确认无明文 `tcp:` 连接残留。

如果当前 OVN 和部署模式支持临时同时保留 `ptcp` 和 `pssl`，可以优先使用双监听降低切换窗口。若不支持，则使用有序 rollout 和 readiness gate。

### 从现有 `ENABLE_SSL=true + kube-ovn-tls` 升级

现有集群已经依赖 `kube-ovn-tls` 共享证书。升级时需要兼容旧证书，避免一次切换导致所有客户端同时断连。

推荐流程：

1. 升级 Kube-OVN 镜像和 manifests，先继续挂载和使用旧 `kube-ovn-tls`。
2. 创建 `ovn-db-tls-ca`。
3. 让 `ovn-central` 申请新的 server cert。
4. 让各客户端申请新的 client cert。
5. 在同一 Pod 内保留旧证书和新证书两个路径。
6. 先切换 `ovn-central` 到新 server cert 和新 CA。
7. 再滚动客户端组件到新 client cert。
8. 所有组件确认使用新证书后，停止依赖旧 `kube-ovn-tls`。

如果 CA 需要从旧 `kube-ovn-tls` CA 切到新 `ovn-db-tls-ca`，建议使用 CA bundle 过渡：

1. `ca.crt` 临时包含旧 CA + 新 CA。
2. 滚动所有服务端和客户端。
3. 将 leaf cert 切换为新 CA 签发的证书。
4. 再次滚动所有服务端和客户端。
5. 从 `ca.crt` 移除旧 CA。
6. 最后滚动所有服务端和客户端。

### 回滚策略

如果启用新证书后连接异常：

1. 保留旧 `kube-ovn-tls` Secret。
2. 将证书路径或挂载切回旧 `/var/run/tls/{key,cert,cacert}`。
3. 滚动 `ovn-central` 和客户端组件。
4. 必要时将 `ENABLE_SSL` 临时切回 `false`，但该操作会短暂回到明文 TCP，应仅作为紧急回滚手段。

## 代码改动范围

Helm 和 manifests：

- 保留 `ENABLE_SSL` 作为唯一开关。
- 保留旧 `kube-ovn-tls` 作为升级兼容输入。
- 新增 OVN DB TLS CA Secret 和证书文件挂载。
- 为 `ovn-central`、`ovs-ovn`、`kube-ovn-controller`、`monitor`、`pinger` 挂载对应 server/client 证书路径。

Controller：

- 扩展现有 `kubeovn.io/signer`，增加 OVN DB TLS profile。
- 新增 `ovn-db-tls-ca` 创建和读取逻辑。
- 新增 server/client CSR 校验逻辑。
- 签发证书时设置正确的 `ExtKeyUsage`、SAN、有效期。

Daemon 和组件启动：

- 复用 IPsec 的证书检查、CSR 创建、等待签发、半生命周期刷新逻辑。
- 为 `ovs-ovn` 生成 node client cert。
- 为 `ovn-central` 生成 server cert 和 northd client cert。
- 证书更新后重启或 reload 对应进程。

OVSDB Go client：

- 支持加载 client cert、client key、CA。
- 移除 `InsecureSkipVerify: true`。
- 设置 `ServerName` 或等价校验逻辑，确保 server cert SAN 覆盖连接地址。

脚本：

- 继续用 `ENABLE_SSL` 作为协议切换开关。
- 将共享 `/var/run/tls/key|cert|cacert` 调整为 server/client 明确路径。
- 保留旧路径兼容升级，最终迁移到新路径。

## 测试范围

单元测试：

- CSR profile 识别。
- CSR 名称、usage、CN、SAN 校验。
- server cert 和 client cert 模板生成。
- 证书生命周期过半刷新判断。
- 非当前 CA 签发证书触发重新申请。
- Go OVSDB client 严格校验证书和 SAN。

集成或 e2e 测试：

- `ENABLE_SSL=true` 的全新安装。
- `ENABLE_SSL=false` 升级到 `true`。
- 现有 `ENABLE_SSL=true + kube-ovn-tls` 升级到自动轮转证书。
- client cert 过期或被删除后自动重新申请。
- server cert SAN 不匹配时连接失败。
- 非 kube-ovn CA 签发的 client cert 连接 OVN DB 失败。
- 证书生命周期过半后自动刷新并恢复连接。

## 风险和约束

- bootstrap 不能依赖 OVN DB 已可用，只能依赖 Kubernetes API 和 CA Secret。
- server cert SAN 覆盖不完整会导致严格校验后连接失败。
- CA 轮转比 leaf cert 轮转更敏感，需要 bundle 过渡。
- 证书更新后 OVN/OVS 进程是否支持无感 reload 需要验证；第一阶段按重启处理。
- 旧 `kube-ovn-tls` 到新证书路径的迁移需要兼容一段时间。
- 不做 OVN SB DB role/RBAC，因此本方案是连接层证书鉴权，不是表/字段级授权。

## 第一阶段实现范围

第一阶段建议实现：

1. 保持 `ENABLE_SSL` 为唯一协议开关。
2. 新增 `ovn-db-tls-ca`，不复用 `ovn-ipsec-ca`。
3. 复用 IPsec CSR/signer/半生命周期刷新机制。
4. 支持 OVN DB server cert 自动签发和刷新。
5. 支持各组件 client cert 自动签发和刷新。
6. 启用 mTLS，客户端和服务端都严格校验证书。
7. 从旧 `kube-ovn-tls` 共享证书方案平滑升级。
8. 不启用 OVN SB DB `role/RBAC`，不拆端口。

一句话总结：

```text
Kube-OVN 在现有 ENABLE_SSL 基础上，将固定共享 kube-ovn-tls 升级为由 kube-ovn 自签 signer 管理的 OVN DB TLS 证书体系；复用 IPsec 的 CSR/signer/半生命周期刷新机制，实现 server/client 证书自动签发、mTLS 校验和自动轮转。第一阶段不做 OVN SB DB role/RBAC。
```
