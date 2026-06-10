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

Kube-OVN 内部已经有一套给 OVN IPSec 使用的证书签发和轮转流程：

- `ovn-ipsec-ca` 保存自签 CA。
- daemon 生成私钥和 CSR。
- daemon 创建 Kubernetes `CertificateSigningRequest`。
- controller 自动 approve，并用 `kubeovn.io/signer` 签发匹配的 CSR。
- daemon 在证书生命周期过半后自动刷新证书。

OVN DB TLS 复用这套 CSR/signer/半生命周期刷新机制，但需要独立的 CA、证书用途和校验逻辑，不能直接复用 IPSec 证书。

## 目标

- `ENABLE_SSL` 保持为 OVN DB 协议（tcp/ssl）的唯一开关，语义不变。
- 新增证书管理开关和轮转开关，均默认关闭，存量行为零变化。
- 由 Kube-OVN 自己管理 OVN DB TLS 证书创建和轮转，不依赖 cert-manager。
- 复用现有 OVN IPSec 的 CSR/signer/半生命周期刷新机制。
- 为 OVN DB server 和各访问组件签发不同用途的证书，启用 mTLS 严格校验。
- 轮转失败时不影响在用证书和运行中的进程。

## 非目标

- 不依赖 cert-manager。Kube-OVN 可能需要在 cert-manager 可用前先启动。
- 不使用 Kubernetes 控制面 CA 私钥给 OVN DB TLS 签证书。
- 不直接复用 OVN IPSec 证书或 `ovn-ipsec-ca`。
- 不启用 OVN SB DB `role=ovn-controller`，不实现表/字段级 RBAC。
- 不拆分 OVN NB/SB 访问端口，继续使用现有 6641/6642。
- 第一阶段不提供存量 `ENABLE_SSL=true + kube-ovn-tls` 集群的自动迁移路径
  （存量 SSL 集群几乎不存在；迁移路径留到功能稳定后再设计）。
- 不暴露证书有效期参数。leaf 证书有效期固定 10 年，CA 固定 10 年；
  轮转频率调节的需求出现后再加 `--ovn-db-tls-cert-duration`。

## 配置

三个正交开关：

| 开关 | 控制内容 | 默认 |
|---|---|---|
| `ENABLE_SSL`（env） | OVN DB 协议：tcp/ptcp 或 ssl/pssl | false |
| `--enable-ovn-db-tls-cert`（controller pflag） | 证书管理：CA 初始化 + CSR signer 处理 `ovn-db-tls-*` profile | false |
| `--enable-ovn-db-tls-cert-rotation`（daemon/组件侧 pflag） | 自动轮转：半生命周期重新申请证书 | false |

行为矩阵：

| ENABLE_SSL | tls-cert | tls-cert-rotation | 行为 |
|---|---|---|---|
| false | * | * | 现状 TCP。证书开关无效，开了打 warning 忽略 |
| true | false | * | SSL 走现有 `kube-ovn-tls` 共享证书（legacy 路径），零行为变化 |
| true | true | false | 初始化 `ovn-db-tls-ca`，signer 签发 server/client 证书，mTLS 严格校验；证书只在启动时检查和申请，运行中不动 |
| true | true | true | 在上一行基础上，组件在证书生命周期过半后自动重新申请并滚动自身进程 |

依赖关系在启动时校验：下层开关未开时上层开关无效并打 warning，不报错退出。

命名说明：避开 `cert-manager` 字样，与现有 `--cert-manager-ipsec-cert`
（指 cert-manager 开源项目）区分；本功能是内置 signer 签发。

代码一致性要求：controller 内不再散落 `os.Getenv(util.EnvSSLEnabled)`，
在 `ParseFlags` 读取一次存为 `Configuration.EnableSSL`，所有判断走 config 字段。

## 证书模型

新增 OVN DB TLS 专用 CA，由 kube-ovn-controller 在
`ENABLE_SSL=true && --enable-ovn-db-tls-cert` 时创建：

```text
Secret: ovn-db-tls-ca
data:
  cacert   # PEM, 自签 CA, 10 年
  cakey    # PEM, PKCS#8（signer 的 decodePrivateKey 只接受 PKCS#8）
```

该 CA 只用于 OVN DB TLS，不复用 `ovn-ipsec-ca`。

证书分为两类：

```text
server cert:
  给 ovn-central / OVN DB 使用
  ExtendedKeyUsage = serverAuth

client cert:
  给 kube-ovn-controller / ovs-ovn(northd 含在 central 内) 使用
  ExtendedKeyUsage = clientAuth
```

server cert 的 SAN 允许范围（signer 强制校验，超出即拒签）：

- `ovn-nb` / `ovn-sb`
- `ovn-nb.<namespace>` / `ovn-sb.<namespace>`
- `ovn-nb.<namespace>.svc[.<cluster-domain>]` / `ovn-sb.<namespace>.svc[.<cluster-domain>]`
- 节点 InternalIP、loopback（hostNetwork/raft 模式实际使用的地址）

第一阶段不支持 VIP 或外部 endpoint 的 SAN。

client cert 不允许携带任何 SAN，身份通过 CN 表达：

- `kube-ovn-controller`
- `ovs-ovn:<nodeName>`

monitor / pinger / ic-controller 复用同一套 Go OVSDB client，
机制打通后二阶段接入。

第一阶段只在 TLS 握手阶段校验证书是否合法，不基于 CN 做授权。

## 鉴权语义

本方案实现的是 mTLS 连接层证书鉴权：

- OVN DB 校验 client cert：由 `ovn-db-tls-ca` 签发、在有效期内、允许 clientAuth。
- client 校验 server cert：由 `ovn-db-tls-ca` 签发、在有效期内、允许 serverAuth、SAN 匹配连接地址。

解决的问题：未持有本集群 kube-ovn 签发的合法 client cert，不能建立 OVN DB TLS 连接。

不解决的问题：已通过 mTLS 连接后，不区分不同组件能读写哪些 OVN DB 表或字段
（OVN SB DB role/RBAC 第一阶段不做）。

## CSR profile 与 signer 校验

复用 `kubeovn.io/signer`，按 CSR 名称前缀 + usage 识别 profile：

| profile | CSR 名称 | usage | CA |
|---|---|---|---|
| ipsec（现有） | `ovn-ipsec-<node>` | ipsec tunnel | ovn-ipsec-ca |
| ovn-db-tls-server | `ovn-db-tls-server-<name>` | server auth | ovn-db-tls-ca |
| ovn-db-tls-client | `ovn-db-tls-client-<name>` | client auth | ovn-db-tls-ca |

signer 在签发前校验 CSR 内容（防止持有 CSR create 权限即可伪造身份）：

- server profile：DNS SAN 限于上述 service 名集合；IP SAN 限于节点 InternalIP + loopback。
- client profile：禁止任何 SAN。
- ipsec profile：禁止 IP SAN（保持现有行为）。

校验失败写 `CSRValidationFailure` condition，不签发，不降级 controller 状态。

证书模板按 profile 设置 `ExtKeyUsage`；`IPAddresses` 仅 server profile 透传。

## 签发流程

组件侧（bootstrap 与轮转共用同一段逻辑）：

```text
组件启动（或轮转触发）
  → 检查本地证书：不存在 / PEM 损坏 / 不在有效期内 → 需要申请
  → 生成 private key + CSR，创建 Kubernetes CSR（signerName=kubeovn.io/signer）
  → 等待 controller approve + 签发
  → 验证签发结果（见轮转安全设计）
  → 原子写盘
  → 启动（或重启）对应 OVN/OVS 进程
```

controller signer 侧：

```text
watch CSR → 识别 profile → 校验 usage/SAN → auto-approve
  → 用对应 CA 签发 → 写入 status.certificate
```

bootstrap 约束：`ENABLE_SSL=true` 时 `ovn-central` 必须先有 server cert
才能启动 `pssl` 监听，因此 signer 只依赖 Kubernetes API 和 `ovn-db-tls-ca`，
不依赖 OVN DB 已可用。

## 轮转设计

轮转由 `--enable-ovn-db-tls-cert-rotation` 控制，默认关闭。
触发条件：证书生命周期超过一半（10 年期 → 5 年触发，余量极大）。

核心不变式：**进程重启只发生在新证书完整验证通过之后；
签发链路任何一环失败，在用证书和运行中的进程不受影响。**

```text
半生命周期到达
  → 生成新 key + CSR，等待签发      【失败：日志 + event + 重试，旧证书继续用】
  → 验证新证书：
      - PEM 可解析
      - 由当前 ovn-db-tls-ca 签发
      - 在有效期内、ExtKeyUsage 正确
      - 与新 key 匹配               【任一不过：丢弃，旧证书继续用，下轮重试】
  → 原子写盘（临时文件 + rename）
  → 重启对应进程
```

可观测性：轮转失败除日志外，向对应 Pod 发送 Kubernetes event，
便于发现"轮转持续失败但尚未影响服务"的状态。

证书更新后按重启处理，不强求无感 reload：

- `ovn-central` server cert 更新后滚动 OVN DB 相关进程。
- `ovs-ovn` client cert 更新后重启 `ovn-controller`。
- `kube-ovn-controller` client cert 更新后重启自身。

轮转关闭时的手动轮转路径（与 bootstrap 共用代码，无额外风险面）：
删除组件本地证书文件后滚动 Pod，组件启动时检测证书不存在自动重新申请。

## 证书文件路径

```text
/var/run/tls/server.crt|server.key   # ovn-central
/var/run/tls/client.crt|client.key   # 各客户端组件
/var/run/tls/ca.crt                  # 共用 CA
```

旧共享证书路径 `/var/run/tls/{cacert,cert,key}` 保留为 legacy fallback：
Go OVSDB client 按文件存在性选择——新三件齐全则严格校验，
否则回退 legacy（保留 `InsecureSkipVerify`，部分存在时打 warning）。

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

Go OVSDB client 严格校验时 `ServerName` 留空：
libovsdb 对地址列表中每个 endpoint 复用同一个 `tls.Config`，
`tls.Dialer` 在 `ServerName` 为空时按实际拨号地址逐连接派生，
多副本 HA 下对每个节点分别校验。显式固定 ServerName 会锁死在
第一个 endpoint 上，禁止这样做。

## 启用与回滚

新装集群启用（第一阶段唯一支持的开启路径）：

1. 安装时设置 `ENABLE_SSL=true` + `--enable-ovn-db-tls-cert`。
2. controller 创建 `ovn-db-tls-ca`；`ovn-central` 启动流程先申请 server cert
   再启动 `pssl` 监听；客户端组件申请 client cert 后以 `ssl:` 连接。
3. 验证后可再开启 `--enable-ovn-db-tls-cert-rotation`。

回滚：关闭对应开关并滚动组件即可。

- 关 rotation：停止自动轮转，已签发证书继续有效。
- 关 tls-cert：组件回退 legacy `kube-ovn-tls` 共享证书
  （Go client 文件存在性 fallback 已覆盖），SSL 连接不中断。
- 关 ENABLE_SSL：回到明文 TCP，仅作紧急手段。

## 代码改动范围

Controller：

- `Configuration` 新增 `EnableSSL`（收敛 env 读取）和 `EnableOVNDBTLSCert`。
- CA 初始化与 CSR handler 注册 gate 在
  `config.EnableSSL && config.EnableOVNDBTLSCert`
  （CSR handler 与 IPSec 共用：`EnableOVNIPSec || (EnableSSL && EnableOVNDBTLSCert)`）。
- signer 扩展 `ovn-db-tls-server/client` profile 与 SAN 校验。

Daemon 和组件启动：

- 复用 IPSec 的证书检查、CSR 创建、等待签发逻辑。
- 新增 `--enable-ovn-db-tls-cert-rotation`，半生命周期刷新 + 验证后重启。
- 为 `ovs-ovn` 生成 node client cert；为 `ovn-central` 生成 server cert。

OVSDB Go client：

- 按文件存在性加载 client cert/key/CA，严格校验，ServerName 留空。
- legacy 路径保留 `InsecureSkipVerify` fallback。

Helm / manifests / 脚本：

- `values.yaml` 暴露两个新开关，controller/daemon 模板传参。
- `start-db.sh` / `start-ovs.sh` 接入新证书路径，旧路径 fallback。
- 挂载新证书目录。

## 测试范围

单元测试：

- CSR profile 识别；usage/SAN 校验（伪造 DNS SAN、非节点 IP、client 越权 SAN 拒签）。
- server/client cert 模板生成（ExtKeyUsage、IPAddresses 按 profile）。
- CA 生成产物可被 signer 的 decode 函数闭环消费（PKCS#8）。
- 开关组合：SSL 关时证书开关无效；证书开关关时不初始化 CA。
- 轮转：半生命周期判断；新证书验证失败时不写盘不重启。
- Go OVSDB client：严格/legacy 路径选择，ServerName 为空。

e2e 测试（用 `f.SkipVersionPriorTo` 限定版本，需要 SSL 环境）：

- `ENABLE_SSL=true + tls-cert` 全新安装，组件证书签发、mTLS 连接正常。
- 合法 server CSR 自动 approve + 签发，SAN/ExtKeyUsage 正确。
- 伪造 SAN 的 CSR 出现 `CSRValidationFailure` 且不签发。
- 非 kube-ovn CA 签发的 client cert 连接 OVN DB 失败。
- 删除组件证书文件后滚动 Pod，自动重新申请（手动轮转路径）。
- rotation 开启时：构造半生命周期已过的证书，验证自动重签并恢复连接；
  signer 不可用时验证旧证书继续工作、进程不重启。

## 风险和约束

- bootstrap 不能依赖 OVN DB 已可用，只能依赖 Kubernetes API 和 CA Secret。
- server cert SAN 覆盖不完整会导致严格校验后连接失败；
  第一阶段 SAN 白名单不含 VIP/外部 endpoint，使用此类地址的部署暂不支持严格校验。
- 轮转的安全性依赖"验证后才重启"不变式，实现时该路径必须有单测覆盖。
- CA 轮转（10 年）不在本设计范围内，到期前需要单独设计 bundle 过渡。
- `start-db.sh` 在启动 DB 前走 CSR 签发需要容器内有 K8s API 客户端能力，
  是脚本接入阶段改动量最大的点。

## 阶段划分

第一阶段（本设计）：

1. 三开关体系：`ENABLE_SSL` / `--enable-ovn-db-tls-cert` / `--enable-ovn-db-tls-cert-rotation`，后两者默认 false。
2. `ovn-db-tls-ca` + signer profile + SAN 校验。
3. `ovn-central` server cert、`kube-ovn-controller` / `ovs-ovn` client cert 自动签发。
4. mTLS 严格校验 + legacy fallback。
5. 半生命周期自动轮转（验证后重启，失败不影响在用证书）。

第二阶段（不在本设计内）：

- monitor / pinger / ic-controller 接入 client cert。
- 存量 `kube-ovn-tls` 集群迁移路径（CA bundle 过渡）。
- 开关默认值翻转评估；`--ovn-db-tls-cert-duration` 参数。
- VIP / 外部 endpoint SAN 支持。

一句话总结：

```text
Kube-OVN 在 ENABLE_SSL（协议）之上新增 --enable-ovn-db-tls-cert（证书管理）
和 --enable-ovn-db-tls-cert-rotation（自动轮转）两个默认关闭的开关，
复用 IPSec 的 CSR/signer 机制为 OVN DB 签发 server/client 证书实现 mTLS；
轮转遵循"新证书验证通过后才重启进程"的不变式，任何失败都停留在旧证书上。
```
