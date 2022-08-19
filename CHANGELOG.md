# Changelog

## v1.10.5 (2022-08-10)

 * [88531d50](https://github.com/kubeovn/kube-ovn/commit/88531d501c4a08d13ec48f80ec324c70105316c6) set release v1.10.5
 * [97031bdd](https://github.com/kubeovn/kube-ovn/commit/97031bdd6b49fdf2252d7f5f10aa891fd94ca197) prepare for release v1.10.5
 * [4a34c5dd](https://github.com/kubeovn/kube-ovn/commit/4a34c5dd47bd719c9e1fa4a893bf767eeacf1c7c) delete htb qos when releated annotation is deleted (#1788)
 * [66643ba3](https://github.com/kubeovn/kube-ovn/commit/66643ba3aa6851fa5865e483b71f06fd50a36da9) perf: fix memory leak
 * [84aba41f](https://github.com/kubeovn/kube-ovn/commit/84aba41f4bc9d12145bb7dde34a8f91e24aa699b) perf: disable mlockall to reduce memory usage
 * [35533738](https://github.com/kubeovn/kube-ovn/commit/35533738e1b86cbdacdaa7d9457f323f3d42ed35) fix iptables for services with external traffic policy set to Local (#1773)
 * [32ee00b6](https://github.com/kubeovn/kube-ovn/commit/32ee00b6190767efac36e5d40f639ef94fe6121b) perf: reduce metrics labels (#1784)
 * [93e74c60](https://github.com/kubeovn/kube-ovn/commit/93e74c6092ceb8c13e9b9eb4dd75572a6b4ebeda) northd: remove lookup_arp_ip actions (#1780)
 * [6c7f45ef](https://github.com/kubeovn/kube-ovn/commit/6c7f45efd19c049d99712ed872c9624245f64a04) fix install error
 * [86173506](https://github.com/kubeovn/kube-ovn/commit/86173506d7cd164b08e50b791908ccd86e697cac) fix:can not delete pod with sriov vf (#1654)
 * [dc77ceb3](https://github.com/kubeovn/kube-ovn/commit/dc77ceb385c82755253a665831038e753f3945f6) dpdk-v2 ，--with-hybrid-dpdk 修改 Dockerfile.base-dpdk 解决 编译安装 ovs-dpdk 正常运行 (#1754)
 * [7a1795e6](https://github.com/kubeovn/kube-ovn/commit/7a1795e61e7d360ad77a2687e065d924df87dc60) dpdk-v2 ，--with-hybrid-dpdk qemu 创建 sock 权限问题 (#1739)
 * [0541ce98](https://github.com/kubeovn/kube-ovn/commit/0541ce98da448b6372e44b2fb9e554db9c62ecf6) feature: support exchange link names of OVS bridge and provider nic in underlay networks (#1736)
 * [4617d7f7](https://github.com/kubeovn/kube-ovn/commit/4617d7f7a31e119e168a546d48015a313fd8a84d) support kubernetes v1.24 (#1761)
 * [29f3d6ed](https://github.com/kubeovn/kube-ovn/commit/29f3d6edd6780dcb1a69f04304921186447c93eb) use leases for leader election (#1529)
 * [f02df1a8](https://github.com/kubeovn/kube-ovn/commit/f02df1a82d6004ab8532453b1752d0e14d855380) fix iptables for service traffic when external traffic policy set to local (#1728)
 * [7f256965](https://github.com/kubeovn/kube-ovn/commit/7f256965bf0ec0598c818dcb5053d878e60c9a2b) set sysctl variables on cni server startup (#1758)
 * [47e39fbf](https://github.com/kubeovn/kube-ovn/commit/47e39fbf5befd59e1f8254b0bbb97bab1f9abf2d) fix: add omitempty to subnet spec
 * [c9ac0cdf](https://github.com/kubeovn/kube-ovn/commit/c9ac0cdf96270c7c9bfe5f45b320010b0d6198a3) perf: replace jemalloc to reduce memory usage
 * [7ffa99e3](https://github.com/kubeovn/kube-ovn/commit/7ffa99e37280f02e92488653500bc9b79354c990) avoid patch interface deletion & recreation during restart (#1741)
 * [8fa4ca49](https://github.com/kubeovn/kube-ovn/commit/8fa4ca49705f35c613a28e48f436696441463ee9) only support IPv4 snat in vpc-nat-gw when internal subnet is dual (#1747)
 * [a46b36d9](https://github.com/kubeovn/kube-ovn/commit/a46b36d98687c359c4d3224e1106b6b528389de0) enqueue subnets after vpc update (#1722)
 * [1bf5dc44](https://github.com/kubeovn/kube-ovn/commit/1bf5dc44f89b7699ec23e0dcc54db56d802e919b) do not add subnet not processed by kube-ovn to vpc (#1735)
 * [66d8be9f](https://github.com/kubeovn/kube-ovn/commit/66d8be9f1dd6226d58ec743d5076ced665a02802) dpdk-v2 ，--with-hybrid-dpdk qemu 创建 sock 权限问题 (#1739)
 * [e9c27c60](https://github.com/kubeovn/kube-ovn/commit/e9c27c60556c4a115df0b06996919d3ca8ec5517) fix: If pod has snat or eip, also need delete staticRoute when delete pod. (#1731)
 * [7841f082](https://github.com/kubeovn/kube-ovn/commit/7841f082151a058d2f54db3cb537f5cdfc143a0e) optimize lrp create for subnet in vpc (#1712)
 * [994885c8](https://github.com/kubeovn/kube-ovn/commit/994885c808177ab74e7d813c509763bc047899f6) fix: cancel delete staticroute when it's used by NatRule (#1733)
 * [f9a84588](https://github.com/kubeovn/kube-ovn/commit/f9a84588e6c147a4d4e252920b2cf064629ed1dd) fix: wrong info when update subnet from dual to ipv4 or ipv6. (#1726)
 * [77988f21](https://github.com/kubeovn/kube-ovn/commit/77988f21f3f5a7155908ed8f2d3a384baad7e808) fix overlay MTU in vxlan/stt tunnels (#1693)

### Contributors

 * Mengxin Liu
 * hzma
 * long.wang
 * xujunjie-cover
 * zhouhui-Corigine
 * 张祖建

## v1.10.4 (2022-07-18)

 * [1e4a1959](https://github.com/kubeovn/kube-ovn/commit/1e4a195992020c422a3f6edf82e06a2277e00ca7) set release 1.10.4
 * [0bbcb389](https://github.com/kubeovn/kube-ovn/commit/0bbcb3898fb5b590637d78b4e5b68f528637ca97) prepare for release 1.10.4
 * [fb76c58e](https://github.com/kubeovn/kube-ovn/commit/fb76c58e51894cb18f720ada9f3c58257745e285) fix: response has no gw when create nic without default route (#1703)
 * [55b3d508](https://github.com/kubeovn/kube-ovn/commit/55b3d508392276c5104500ee52f7537ea8111548) ignore ovsdb-server/compact error: not storing a duplicate snapshot
 * [b6084777](https://github.com/kubeovn/kube-ovn/commit/b6084777c279e3e031405dd0e91bb9d6b0c90a31) Get latest vpc data from apiserver instead of cache (#1684)
 * [f447a1d5](https://github.com/kubeovn/kube-ovn/commit/f447a1d519d7c61c61c85f82dd485fe03126f0fc) update priority range in htb qos (#1688)
 * [bdfdc178](https://github.com/kubeovn/kube-ovn/commit/bdfdc178174abd3e3f4e40eb5e2f56a28086ae98) fix: clean vip eip snat dant fip in cleanup.sh (#1690)
 * [460f930c](https://github.com/kubeovn/kube-ovn/commit/460f930cfb429997213a16376caa175d159a5655) add upgrade-ovs script (#1681)

### Contributors

 * Mengxin Liu
 * Wang Bo
 * bobz965
 * hzma
 * xujunjie-cover
 * zhangzujian

## v1.10.3 (2022-07-13)

 * [f24ed686](https://github.com/kubeovn/kube-ovn/commit/f24ed6862f870481f6ad823401e6437c1781478c) set release 1.10.3
 * [02d68f7f](https://github.com/kubeovn/kube-ovn/commit/02d68f7fb5036a00c1de3424a80dd9113b12a75a) prepare for release 1.10.3
 * [2c989340](https://github.com/kubeovn/kube-ovn/commit/2c989340b834b34341af061e3f690a44101ced29) fix: change ovn-ic static route to policy (#1670)
 * [1596c9ef](https://github.com/kubeovn/kube-ovn/commit/1596c9ef00ce7505af460978042b1e18d21795a5) fix: Do not Recreate Logical_Router_Port when Vpc recreated (#1570)
 * [db4f5ad0](https://github.com/kubeovn/kube-ovn/commit/db4f5ad0644a65dfefaf3655351150913926dbfa) Delete pod if subnet of the pod's owner(sts/vm) updated (#1678)
 * [c41897a0](https://github.com/kubeovn/kube-ovn/commit/c41897a00a1011b35efae358232cc4d8bb7bfbb5) do not snat packets only for subnets with distributed gateway when external traffic policy is set to local (#1616)
 * [8190df3b](https://github.com/kubeovn/kube-ovn/commit/8190df3b330da01613d676fc768094c7f60c15c7) security: disable pprof by default (#1672)
 * [761ddcbc](https://github.com/kubeovn/kube-ovn/commit/761ddcbc62586e2cb74064f0bf18973fca3c8094) bgp: consolidate service check and use service const (#1674)
 * [5cffa97d](https://github.com/kubeovn/kube-ovn/commit/5cffa97d2708f9113b43bea05cf3cb95f7f92509) fix bgp: sync service cache (#1673)
 * [874785bf](https://github.com/kubeovn/kube-ovn/commit/874785bfbcf7c686f2064871fe5226bd719db857) fix iptables for direct routing (#1578)
 * [f3886af7](https://github.com/kubeovn/kube-ovn/commit/f3886af7b30a6253bed5d88bf1addbad4d2a78ac) fix libovsdb (#1664)
 * [662dfa64](https://github.com/kubeovn/kube-ovn/commit/662dfa649897728744d8d5dcb8c8bd3bdfb1fc95) mount modules for auto load ip6tables moudles (#1665)
 * [1efaeb00](https://github.com/kubeovn/kube-ovn/commit/1efaeb000deaed7c824c83265229fb58e4dbbddd) ignore pod not scheduled when reconcile subnet (#1666)
 * [4409f6c9](https://github.com/kubeovn/kube-ovn/commit/4409f6c9f051cde843e30df4bd5e29678d7ae9de) fix ovs-ovn not running on newly added nodes (#1661)
 * [b5025a6a](https://github.com/kubeovn/kube-ovn/commit/b5025a6a7f1dbdc39a6a3f7738bad635b4a8c032) fix get security group name by external_ids (#1663)
 * [4afbaf31](https://github.com/kubeovn/kube-ovn/commit/4afbaf31d8514e85d184d307e35cfc9c91291bf0) add policy route when add subnet (#1655)

### Contributors

 * Mengxin Liu
 * Money Liu
 * Wang Bo
 * gugu
 * hzma
 * lut777
 * wangyd1988
 * 刘睿华
 * 张祖建

## v1.10.2 (2022-06-28)

 * [b1a17c4a](https://github.com/kubeovn/kube-ovn/commit/b1a17c4add0a817fb05340f3fc1777e57a305de4) set for release 1.10.2
 * [4d229555](https://github.com/kubeovn/kube-ovn/commit/4d229555325ca3ac8561e815acfc26dff952aa9d) fix: no need routed when use v1.multus-cni.io/default-network (#1652)
 * [40391a03](https://github.com/kubeovn/kube-ovn/commit/40391a0384b7666ec06b82d8c3a00ecff2517fcc) prepare for release 1.10.2
 * [7c4dfe72](https://github.com/kubeovn/kube-ovn/commit/7c4dfe72192458c381781923ee399473a3727ebc) fix: subnet failed when create without protocol
 * [4b063242](https://github.com/kubeovn/kube-ovn/commit/4b063242b513d1cd82f06bc65bba66da23a8e41c) set ether dst addr for dnat on logical switch (#1512)
 * [20222e4f](https://github.com/kubeovn/kube-ovn/commit/20222e4f5db74782cf49336cfd31882b847cdd1f) CI: delete resources in order to avoid a long time waiting for subnet deletions. (#1643)
 * [35e29e16](https://github.com/kubeovn/kube-ovn/commit/35e29e162524ddb58e5e721ded06cfbb9329b1c7) ci: fix golangci-lint (#1639)
 * [4661b76e](https://github.com/kubeovn/kube-ovn/commit/4661b76eaeeb28aea6a1ab853929f49117befc21) fix: cleanup should ignore patch failed (#1626)
 * [73a53ba7](https://github.com/kubeovn/kube-ovn/commit/73a53ba74fbd3ee4dadc6b6c4730ccafe2f06808) fix no interface report to multus cni, missing in k8s.v1.cni.cncf.io/network[s]-status (#1636)
 * [fe5e020e](https://github.com/kubeovn/kube-ovn/commit/fe5e020eb9251658f7c30ba07d4687125ede8078) Update install.sh (#1645)
 * [bd7ff533](https://github.com/kubeovn/kube-ovn/commit/bd7ff5338c55ac01d790ecacc75b7e83c4fd1b22) set networkpolicy log default to false (#1633)
 * [83c9e845](https://github.com/kubeovn/kube-ovn/commit/83c9e84560d5e789e1408334b05b210e711cca3b) update policy route when join subnet cidr changed (#1638)
 * [bcf057d1](https://github.com/kubeovn/kube-ovn/commit/bcf057d16d73f8639854856e3694217f826bba34) ci: update trivy options (#1637)
 * [f93a5273](https://github.com/kubeovn/kube-ovn/commit/f93a52737cdd793610cdb09ef472e4b63da3a6ae) increase initial delay of ovs-ovn liveness probe (#1634)
 * [1a55ce12](https://github.com/kubeovn/kube-ovn/commit/1a55ce126a38600ab4ed26c8a9d468bbeeb4c7e4) wait ovn-central pods running before delete ovs-ovn pods (#1627)
 * [f8a266d6](https://github.com/kubeovn/kube-ovn/commit/f8a266d69587e6c961917f1ec57fe1f71f29f3f4) get dbstatus for all ovn-central pod (#1619)
 * [bc838d5a](https://github.com/kubeovn/kube-ovn/commit/bc838d5a607275c33622d8122646acd622a5bb70) delete "allow" policy route on subnet deletion (#1628)

### Contributors

 * Mengxin Liu
 * ShaPoHun
 * halfcrazy
 * hzma
 * xujunjie-cover
 * zhangzujian
 * 张祖建

## v1.10.1 (2022-06-19)

 * [4935fa6a](https://github.com/kubeovn/kube-ovn/commit/4935fa6adc8a0088b173603e819cec274996ed29) monitor dns in cilium e2e (#1597)
 * [3dc29041](https://github.com/kubeovn/kube-ovn/commit/3dc290413f89d0a51fc0f6549f4ae115e6fd9320) prepare for release 1.10.1
 * [e459688e](https://github.com/kubeovn/kube-ovn/commit/e459688e03f628741901f442a589e1afb79abfc8) ci: build amd64 images without avx512 (#1584)
 * [d7144681](https://github.com/kubeovn/kube-ovn/commit/d71446817c63ab573ef7fc359ff90ffd68bef526) update ovs health check, delete connection to ovn sb db (#1588)
 * [cfbe55e0](https://github.com/kubeovn/kube-ovn/commit/cfbe55e028bcd273ed16d9c6b64203cc86b27059) fix: all cluster pod will be in podadd queue (#1587)
 * [08ba4215](https://github.com/kubeovn/kube-ovn/commit/08ba4215b6986b5d2a7f928dd9460eee1adf31a5) fix pod could not be ready (#1562)
 * [c453b7ac](https://github.com/kubeovn/kube-ovn/commit/c453b7ac2f4720ab32f44e10a19d0e1accb8a91f) fix: delete pod panic when delete vm or statefulset. (#1565)
 * [77044e3d](https://github.com/kubeovn/kube-ovn/commit/77044e3da2d3c4abf71047971beb9b348fc2e611) fix: clean CRDs introduced by new vpc-nat-gateway (#1563)
 * [e35f90f1](https://github.com/kubeovn/kube-ovn/commit/e35f90f1b6d6156a1e743c1b8281fc0b51206fce) do not gc vm pod lsp when vm still exists (#1558)
 * [adabd853](https://github.com/kubeovn/kube-ovn/commit/adabd853fdc6f8791a1a96e379e32ea91d692d30) do not delete static routes on controller startup (#1560)
 * [4348e58f](https://github.com/kubeovn/kube-ovn/commit/4348e58f0240e26315af13942b0042b0cf8e8bb4) replace ovn-nbctl daemon with libovsdb in frequent operations (#1544)
 * [4cacb4b9](https://github.com/kubeovn/kube-ovn/commit/4cacb4b989192e047b219f2bace7c1351501e8c4) fix exec cmd in vpc nat gateway (#1556)
 * [0ed681af](https://github.com/kubeovn/kube-ovn/commit/0ed681afa3ab73125ca6dec88f14180161b1c734) CNI: do not return route if nic is not eth0 (#1555)
 * [96f232d4](https://github.com/kubeovn/kube-ovn/commit/96f232d4626bfdf47a5583979a0ac69677f95e3d) do not nat packets for incoming traffic when service externalTrafficPolicy is Local
 * [bbb8a697](https://github.com/kubeovn/kube-ovn/commit/bbb8a6971dbb48a0ea3e445ac51be95a13523faa) exit kube-ovn-controller on stopped leading (#1536)
 * [4b0bd69e](https://github.com/kubeovn/kube-ovn/commit/4b0bd69e35284c7d30603eb05922b7631571e401) tmp cancel cilium external svc test (#1531)

### Contributors

 * hzma
 * lut777
 * xujunjie-cover
 * zhangzujian
 * 刘睿华
 * 张祖建

## v1.10.0 (2022-05-15)

 * [16d28f75](https://github.com/kubeovn/kube-ovn/commit/16d28f755b22704427c297918c01119955ed6e6d) release 1.10.0
 * [bcdb3388](https://github.com/kubeovn/kube-ovn/commit/bcdb338864fd35bf43110a97e8515cd0373d64d3) use inc-engine/recompute instead of deprecated recompute (#1528)
 * [12094766](https://github.com/kubeovn/kube-ovn/commit/1209476696394494982d64ce294580e1751b51fd) update kind to v0.13.0 (#1530)
 * [673138f2](https://github.com/kubeovn/kube-ovn/commit/673138f284a26b26535ce14450a6192c3cd77077) move dumb-init from base images to kube-ovn image (#1527)
 * [ad6826d9](https://github.com/kubeovn/kube-ovn/commit/ad6826d9f1a4b883e1881870d3a535144fa5b286) fix installing dumb-init in arm64 image (#1525)
 * [4eebabc1](https://github.com/kubeovn/kube-ovn/commit/4eebabc1e18a69a80412dc98991a8290f6e89a4f) optimize ovs request in cni (#1518)
 * [7a3f73d5](https://github.com/kubeovn/kube-ovn/commit/7a3f73d566e358bf9ba328d6f290927ccd5369b7) optimize node port-group check (#1514)
 * [b7c01d43](https://github.com/kubeovn/kube-ovn/commit/b7c01d438e92af1a9eeccd90a2ebb55d9462c4b9) logic optimization (#1521)
 * [65ee71b4](https://github.com/kubeovn/kube-ovn/commit/65ee71b4ba1f3a77d146ed43aa27cd60371f69af) fix defunct ovn-nbctl daemon (#1523)
 * [ebe00370](https://github.com/kubeovn/kube-ovn/commit/ebe00370173585ca2428968bda978796e30132e5) fix arm image (#1524)
 * [354d6c3e](https://github.com/kubeovn/kube-ovn/commit/354d6c3ef8592e3d7506c3e9cea3be0ca1559bdc) fix: keep vm's and statefulset's ips when user specified subnet (#1520)
 * [6021e528](https://github.com/kubeovn/kube-ovn/commit/6021e5288e28caaf506da3906fb48dda1337b0c8) feature: add doc for tunning packages (#1513)
 * [8e72f2e1](https://github.com/kubeovn/kube-ovn/commit/8e72f2e1ff7f0cac1a2984e7fc9b40e54bc77a7a) add document for windows support (#1515)
 * [d7ef43b3](https://github.com/kubeovn/kube-ovn/commit/d7ef43b3e8e0916877b19aa4b351c06adf718102) reduce ovs-ovn restart downtime (#1516)
 * [7b8aa124](https://github.com/kubeovn/kube-ovn/commit/7b8aa12410c986eed7d5e41aea969abff81dabf1) finish basic windows support (#1463)
 * [ecc8268f](https://github.com/kubeovn/kube-ovn/commit/ecc8268fe25706962ce1b33eb73c65f342339f2b) refactor logical router routes (#1500)
 * [51603624](https://github.com/kubeovn/kube-ovn/commit/51603624190f0271b73979ed13a0436faa4fb58e) add netem qos when create pod (#1510)
 * [5158dd9d](https://github.com/kubeovn/kube-ovn/commit/5158dd9d2d96d2e19f8826b403f1c4a6d5299ce6) handle the case of error node cidr (#1509)
 * [1285b039](https://github.com/kubeovn/kube-ovn/commit/1285b03983a8add91e8442a1fef4211691df0594) fix: ovs trace flow always ends with controller action (#1508)
 * [69428690](https://github.com/kubeovn/kube-ovn/commit/694286902e595ee61f39b1ba78c94944a82e6a7c) add qos e2e test (#1505)
 * [f214ee20](https://github.com/kubeovn/kube-ovn/commit/f214ee202e02591b7ed23320839b203a162dbf4b) optimize IPAM initialization (#1498)
 * [367d6b74](https://github.com/kubeovn/kube-ovn/commit/367d6b74612c5ce12bb5ecea0accb0dc2ef5dcdf) test: fix flaky test (#1506)
 * [79ad4fcf](https://github.com/kubeovn/kube-ovn/commit/79ad4fcf44066a8be2809bb6f3991fba84b972a1) docs: update README.md
 * [85d09ccd](https://github.com/kubeovn/kube-ovn/commit/85d09ccd05dd74e437bd5cbd937ac3ce36262c0c) synchronize yamls with installation script (#1504)
 * [63dc5219](https://github.com/kubeovn/kube-ovn/commit/63dc5219cbfc40d09ddb4d5c7737f27a424e4dc0) feature: svc of multiple clusters (#1491)
 * [011eacf6](https://github.com/kubeovn/kube-ovn/commit/011eacf63b901c93a8c8b65cc7d7bbc42a616d78) use OVS branch-2.17 (#1495)
 * [afc9ef62](https://github.com/kubeovn/kube-ovn/commit/afc9ef62295711399f576ec7d79b43d39fef9723) Update USERS.md (#1496)
 * [b057404b](https://github.com/kubeovn/kube-ovn/commit/b057404baa655afba6db3ce57244dcb1c2f8f142) update document for mellanox hardware offload (#1494)
 * [fb3c3e6e](https://github.com/kubeovn/kube-ovn/commit/fb3c3e6e8a7e26c848f2ccac786c0a4ec78f29ad) Feature iptables eip nats splits (#1437)
 * [0c95402e](https://github.com/kubeovn/kube-ovn/commit/0c95402edd68fc95bf4c973bed49a6ce1f274254) Update USERS.md (#1493)
 * [08a7d5b6](https://github.com/kubeovn/kube-ovn/commit/08a7d5b61ed59973e193dd86adc35ff3d08613d4) update github actions (#1489)
 * [ad28dca0](https://github.com/kubeovn/kube-ovn/commit/ad28dca06fef8b1f31bc448caea4e4566070c50a) update USER.md (#1492)
 * [0db63226](https://github.com/kubeovn/kube-ovn/commit/0db63226817ff865570c203ddb3c57ca66b610fc) fix: add empty chassis check in ovn db (#1484)
 * [d631f8f8](https://github.com/kubeovn/kube-ovn/commit/d631f8f8b8838846184eb678dc2c934377697258) feat: lsp forwarding external Layer-2 packets (#1487)
 * [d4d700ec](https://github.com/kubeovn/kube-ovn/commit/d4d700ecdfb020a4bbb12851a5023edc36c5dbc6) base: add back kubectl (#1485)
 * [59e4ae73](https://github.com/kubeovn/kube-ovn/commit/59e4ae73879a1d6cfa95905df66d5cbb02a6fab8) delete ipam record when gc lsp (#1483)
 * [73405b2a](https://github.com/kubeovn/kube-ovn/commit/73405b2ad2577ae5ec42521b4c827e91954ee4fd) fix: wrong vpc-nat-gateway arm image (#1482)
 * [881622d4](https://github.com/kubeovn/kube-ovn/commit/881622d47dbe61340d99614620464d421c7613cc) fix pod annotation may override by patch (#1480)
 * [e772ee95](https://github.com/kubeovn/kube-ovn/commit/e772ee95ecdc96e8c3c6fc5fbb35d54a3d4671f5) add acl doc (#1476)
 * [6ef72e75](https://github.com/kubeovn/kube-ovn/commit/6ef72e75db2309c1090fc58306e400cc938fff47) fix: workqueue_depth should show count not rate (#1478)
 * [5ba5c526](https://github.com/kubeovn/kube-ovn/commit/5ba5c5264c28e9c59e5d67977588163af0a073be) add delete ovs pods after restore nb db (#1474)
 * [73f9d15f](https://github.com/kubeovn/kube-ovn/commit/73f9d15fcc1bb7a32a1e137a3c26deffffa5fbde) delete monitor noexecute toleration (#1473)
 * [abaebea4](https://github.com/kubeovn/kube-ovn/commit/abaebea4790d7b9490eb5fa8a962fc4dd3302031) add env-check (#1464)
 * [1d6d4653](https://github.com/kubeovn/kube-ovn/commit/1d6d46532690f8e85e6726939233ab9a65c413a1) Support kubevirt vm live migrate for pod static ip (#1468)
 * [54cab3aa](https://github.com/kubeovn/kube-ovn/commit/54cab3aa2f0bd2c5ca28fe883ff50afbc8ee802a) fix routes for packets from Pods to other nodes
 * [ba8c5937](https://github.com/kubeovn/kube-ovn/commit/ba8c5937e8e4205a5f19768d739472033866666e) add manual compile method for ubuntu20.04 (#1461)
 * [7848d71f](https://github.com/kubeovn/kube-ovn/commit/7848d71fbf415d1b43a0ecd4cdd8cef760efcb9d) append metrics (#1465)
 * [4f0b1976](https://github.com/kubeovn/kube-ovn/commit/4f0b197663d3582f0a1861591f262adf6b31e880) Annotation network_type always is geneve
 * [6ddba02a](https://github.com/kubeovn/kube-ovn/commit/6ddba02af015ee74a3d6d195dcde0efd3eee3081) masquerade packets from Pods to service IP
 * [3d18b8d3](https://github.com/kubeovn/kube-ovn/commit/3d18b8d3f3322a50d36358e328c15df3338e2dad) update OVS and OVN for windows
 * [39cdfc5c](https://github.com/kubeovn/kube-ovn/commit/39cdfc5c0df09d2e6114b03730ba77209e95f426) windows support for cni server
 * [75d8f4de](https://github.com/kubeovn/kube-ovn/commit/75d8f4de335dd7e259192b8c0aca7e9bdeae924f) add kube-ovn-controller switch for EIP and SNAT
 * [8ac3e0c0](https://github.com/kubeovn/kube-ovn/commit/8ac3e0c019645a138ed6e79dd3988fc69d587589) docs: add USERS.md (#1454)
 * [8c214bc9](https://github.com/kubeovn/kube-ovn/commit/8c214bc91129481a69c4d815d8b832183d9165ec) update topology pic
 * [cd5c591c](https://github.com/kubeovn/kube-ovn/commit/cd5c591cd521628dfe7aba4a3bd503b689977ed3) feature: add sb/nb db check bash script (#1441)
 * [fc5f7190](https://github.com/kubeovn/kube-ovn/commit/fc5f7190ae5c9363422abba098c0746d44c4a632) add routed check in circulation (#1446)
 * [aa756519](https://github.com/kubeovn/kube-ovn/commit/aa756519386eabb2225c2d669c706f14e8bbf6c1) modify init ipam by ip crd only for sts pod (#1448)
 * [3a5ead6d](https://github.com/kubeovn/kube-ovn/commit/3a5ead6d751b9a0cbd3626270691dfb6acc0c46d) base: refactor ovn/ovs build (#1444)
 * [43051166](https://github.com/kubeovn/kube-ovn/commit/43051166883c0f8ade64add8b1049267fc4b578b) log: show the reason if get gw node failed (#1443)
 * [8f1e85ae](https://github.com/kubeovn/kube-ovn/commit/8f1e85ae6fd4f2f89319edbee91c9e42eadb57c7) add doc for #1358 (#1440)
 * [0c0a0308](https://github.com/kubeovn/kube-ovn/commit/0c0a03081965e9642136a54a0e5f67158d5016ab) prepare windows support for cni server
 * [88b07498](https://github.com/kubeovn/kube-ovn/commit/88b0749846bb1ca49480fee75b6661313e4dc69d) modify webhook img to independent image (#1442)
 * [3dbfa4de](https://github.com/kubeovn/kube-ovn/commit/3dbfa4de2899ef8d219085e07a3ab96f1c5e2b09) update alpine to fix CVE-2022-1271
 * [03af744f](https://github.com/kubeovn/kube-ovn/commit/03af744f11e2b8686d774ae11c35752fec7085d2) fix adding key to delete Pod queue
 * [0ea24dcf](https://github.com/kubeovn/kube-ovn/commit/0ea24dcf234502e0ca5d7104a7fe6549183a2137) fix IPAM initialization
 * [b26a06e7](https://github.com/kubeovn/kube-ovn/commit/b26a06e7aacd9790008bc6b2a0d6c54042f51ecb) temporary cancel the external2cluater  e2e test for cilium (#1428)
 * [94bc2087](https://github.com/kubeovn/kube-ovn/commit/94bc20878860979aa3d4aaad1cbc0222a212e9a4) ignore all link local unicast addresses/routes
 * [9be57346](https://github.com/kubeovn/kube-ovn/commit/9be57346b2388214adfe45c74703ba561418a825) fix error handling for netlink.AddrDel
 * [87164cc9](https://github.com/kubeovn/kube-ovn/commit/87164cc9531926eda12e52adfec5b2595ae04114) replace pod name when create ip crd (#1425)
 * [e7c69ba5](https://github.com/kubeovn/kube-ovn/commit/e7c69ba58d7cc16e68eec872c71e2f493e6474e0) add webhook vaildate the vpc resource whether can be deleted. (#1423)
 * [c9a58886](https://github.com/kubeovn/kube-ovn/commit/c9a58886a818ac14d85cad42b722c9ae5535d11c) We are looking forward to your PR! (#1422)
 * [743ce241](https://github.com/kubeovn/kube-ovn/commit/743ce241e1497245d1b70791c87e76940415b19a) support alloc static ip from any subnet after ns supports multi subnets (#1417)
 * [d3f6431f](https://github.com/kubeovn/kube-ovn/commit/d3f6431f234b3310b4cc0f9604c36415ab404288) fix provider-networks status
 * [48e0c4ed](https://github.com/kubeovn/kube-ovn/commit/48e0c4ed78d701f63d8f6fd2e6439086df387116) build ovs/ovn for windows in ci
 * [3b4ac99a](https://github.com/kubeovn/kube-ovn/commit/3b4ac99ac9dc2ee11b44502ddd59808f12603a54) cilium e2e: deploy k8s without kube-proxy
 * [902315ed](https://github.com/kubeovn/kube-ovn/commit/902315ed50a9699ae52ba6ec715eb500666861c8) windows support for CNI
 * [f2baa2f7](https://github.com/kubeovn/kube-ovn/commit/f2baa2f7fd634f8a1da4eae2d0d5e550f75fee90) add simple e2e for multus integration
 * [e3693436](https://github.com/kubeovn/kube-ovn/commit/e3693436c7972455452f525f66fb068115189306) update e2e testing
 * [60bf81a3](https://github.com/kubeovn/kube-ovn/commit/60bf81a35ecee8a0a5405d5dd39a040e0685ff39) recover ips CR on IPAM initialization
 * [8e1cd468](https://github.com/kubeovn/kube-ovn/commit/8e1cd4687a00f17321c9dfe5870dca60b558354b) docs: update ROADMAP.md and MAINTAINERS
 * [19ecaeee](https://github.com/kubeovn/kube-ovn/commit/19ecaeee27d3052802195ba4a85900bd5be99664) create ip crd in kube-ovn-controller (#1413)
 * [25abbce7](https://github.com/kubeovn/kube-ovn/commit/25abbce7d83d37ef755e059c161fc84888a41088) add condition for triggering the deletion of redundant chassises in sbdb (#1411)
 * [a378fad2](https://github.com/kubeovn/kube-ovn/commit/a378fad2469da2916d254227bf9a0e682bcbb78f) fix: do not recreate port for terminating pods (#1409)
 * [9587ad41](https://github.com/kubeovn/kube-ovn/commit/9587ad41a96f319f2dbfad17c8df8a6da2f7e21c) update cni version to 1.0
 * [df83c5fb](https://github.com/kubeovn/kube-ovn/commit/df83c5fb7bf3d9376b9ba3ce1fa22e6e44b61ce9) update underlay environment requirements
 * [ff695aa3](https://github.com/kubeovn/kube-ovn/commit/ff695aa36a7011b475f33107a104226f2ca38b95) avoid frequent ipset update
 * [f475736c](https://github.com/kubeovn/kube-ovn/commit/f475736c6f90b753d4a673e4829a87c80fab596a) add reset for kube-ovn-monitor metrics (#1403)
 * [87d6839d](https://github.com/kubeovn/kube-ovn/commit/87d6839dda10a5f921d307171c6dec0cb9702607) fix: The underlay physical gateway config by external-gw-addr when use snat&eip (#1400)
 * [d36a0d8d](https://github.com/kubeovn/kube-ovn/commit/d36a0d8d74ebe568ce55d3f4c21bae7b6f5a9283) add custom acls for subnet (#1395)
 * [3206a7a2](https://github.com/kubeovn/kube-ovn/commit/3206a7a2ae88a2e03e61f888b92aa433da7c8564) check the cidr format whether is correct (#1396)
 * [a33d519b](https://github.com/kubeovn/kube-ovn/commit/a33d519b24f66cba7b92ddde1408a0bda2a284ce) optimize docs due to frequently asked question. (#1393)
 * [7bd25c63](https://github.com/kubeovn/kube-ovn/commit/7bd25c639118fc8bcc5f679986c94ac0e7e75cd9) adding IP Protocol enumeration to CRD can reduce the kube-ovn Controller judgment logic (#1391)
 * [dcc7971a](https://github.com/kubeovn/kube-ovn/commit/dcc7971ae09bc22e46dca4895e12fc50007879ea) change the wechat qcode
 * [677690d5](https://github.com/kubeovn/kube-ovn/commit/677690d51f3e4ecbf8868e752a64f3f356c8eb47) append vm deletion check (#1390)
 * [0d663ebe](https://github.com/kubeovn/kube-ovn/commit/0d663ebe67d8b206be4137fe9bb629b3f9ebd354) We should handle the case where the subnet protocol is handled (#1373)
 * [7289e87c](https://github.com/kubeovn/kube-ovn/commit/7289e87c8ab380c7842906ddfd8e5fc0082c22ce) VIP is decoupled from port security (#1389)
 * [12907270](https://github.com/kubeovn/kube-ovn/commit/12907270bda18366bf591403ed4a8ebde4d69a0f) chore: reduce image size (#1388)
 * [5e108fe8](https://github.com/kubeovn/kube-ovn/commit/5e108fe873eed45f12746f63b473b55b808f523c) docs: update the maintainer and roadmap (#1387)
 * [fe7cbe1b](https://github.com/kubeovn/kube-ovn/commit/fe7cbe1ba2ffed4adacfceb9368d4302d2e0c233) ci: update kind and k8s
 * [ea60cdf7](https://github.com/kubeovn/kube-ovn/commit/ea60cdf712e064356990bc908501e58959077a44) fix external egress gateway
 * [22cb15c5](https://github.com/kubeovn/kube-ovn/commit/22cb15c513ba94aa25597cbce8b0a396d70a0980) add missing link scope routes in vpc-nat-gateway
 * [5571619d](https://github.com/kubeovn/kube-ovn/commit/5571619da26fe3a45660037444d91a9016a7cb63) update nodeips for restore cmd in ko plugin
 * [33180a1c](https://github.com/kubeovn/kube-ovn/commit/33180a1c7648500f16dfe55b22bbc7776f4e5115) increase memory limit of ovn-central
 * [aa24894e](https://github.com/kubeovn/kube-ovn/commit/aa24894ea3b35fc7de50213344c001506b1bc7f8) fix range loop
 * [1f24d64d](https://github.com/kubeovn/kube-ovn/commit/1f24d64d942e0655417f7d4be16d4e5dee98b7c0) fix probe error
 * [c621853a](https://github.com/kubeovn/kube-ovn/commit/c621853abfd2de8a0050c59d940f38b253287cb0) update script to add restore plugin cmd
 * [dd4a5e0d](https://github.com/kubeovn/kube-ovn/commit/dd4a5e0d62a284d347f986cffec2478c577cae2a) support dpdk (#1317)
 * [8ad9e838](https://github.com/kubeovn/kube-ovn/commit/8ad9e8382b513b02d117be09301f1a38bddf18b6) Use camel case instead of snake case
 * [9f3426ee](https://github.com/kubeovn/kube-ovn/commit/9f3426ee82611edc991ed814a1c8cfd24d35e14e) add detail error when failed to create resource
 * [44dae1f7](https://github.com/kubeovn/kube-ovn/commit/44dae1f704ed049126a07e85f53c9a54ddb8ef9e) add restore process for ovn nb db
 * [c4bb2454](https://github.com/kubeovn/kube-ovn/commit/c4bb24543a4b612661e80248d9cd562ee4dbb1c1) add reset porocess for ovs interface metrics
 * [8e8da195](https://github.com/kubeovn/kube-ovn/commit/8e8da19585cc83ac6f67a4d4841c272c790d3727) fix SNAT/PR on Pod startup
 * [e9a4bd5c](https://github.com/kubeovn/kube-ovn/commit/e9a4bd5c79823c6e2e67b13a221326da7d95bb51) optimize kube-ovn-monitor yaml
 * [b11ffa31](https://github.com/kubeovn/kube-ovn/commit/b11ffa31c6f00616af8f70a4c62d2b7b4dc7d289) Update subnet.go
 * [0b43fc80](https://github.com/kubeovn/kube-ovn/commit/0b43fc8042de74cb11bbce8a0823cc048f8449c6) feat: add webhook to check subnet deletion.
 * [21837784](https://github.com/kubeovn/kube-ovn/commit/218377849857dbc53e5023ea658afe2e71deacf6) modify ipam v6 release ip problem
 * [1264684c](https://github.com/kubeovn/kube-ovn/commit/1264684c69ddaea9e72e4a1cf2f57e50714e0013) skip ping gateway for pods during live migration
 * [0da84f83](https://github.com/kubeovn/kube-ovn/commit/0da84f83f481db3fd3d597750ef0c891cd6b6c25) don't check conflict for migration pod with only static mac
 * [89aa2413](https://github.com/kubeovn/kube-ovn/commit/89aa2413d9f6f6d4a9c19b9c01416363361a3dd4) add service cidr when init kubeadm
 * [bfcb0331](https://github.com/kubeovn/kube-ovn/commit/bfcb0331eca84c37b5345247d1372fea0669a8ca) docs: add provide and ns spec for multus crd
 * [4f987b10](https://github.com/kubeovn/kube-ovn/commit/4f987b10a203fd28e774cb87e1304e5943d184b8) update flag parse in webhook
 * [7354d0c3](https://github.com/kubeovn/kube-ovn/commit/7354d0c3005092660c6074a1ac75e0297f9d320f) fix usage of ovn commands
 * [ffd5c844](https://github.com/kubeovn/kube-ovn/commit/ffd5c844854efc6b18f01e9a64ad872609260f63) add check for pod update process
 * [fe7a6e03](https://github.com/kubeovn/kube-ovn/commit/fe7a6e030947a874dd747b269450ff3682666804) log: rotate all logs in kube-ovn-cni and add compress
 * [024d1684](https://github.com/kubeovn/kube-ovn/commit/024d1684b7e2ce9328621b516c56e14755930f3d) keep ip for kubevirt pod
 * [8c0b358d](https://github.com/kubeovn/kube-ovn/commit/8c0b358d08c61e26c913e60695420c5549378280) docs: add integration with Corigine OVS offload
 * [07c53120](https://github.com/kubeovn/kube-ovn/commit/07c531208c806542457a94219ebc78b9c1f6d16f) fix OVS bridge with bond port in mode 6
 * [baeb3af4](https://github.com/kubeovn/kube-ovn/commit/baeb3af415464b9f53bf865f6fc65d49b0e0e4b3) fix: continue of deletion for del pod failed when can't found vpc or subnet (#1335)
 * [8e204be4](https://github.com/kubeovn/kube-ovn/commit/8e204be4759804dd90bf89c1e403fa83154f136f) feat: support DHCP
 * [8393f322](https://github.com/kubeovn/kube-ovn/commit/8393f322f4ff5feccaa40d11d11112e59af50cf3) Fix usage of ovn commands
 * [bb7b5e56](https://github.com/kubeovn/kube-ovn/commit/bb7b5e56b0c37a5437c4617e82caf9e8734bc09d) resync provider network status periodically
 * [62642ea8](https://github.com/kubeovn/kube-ovn/commit/62642ea8efc56294e7a350e70dc8e58de9e9bc28) Revert "resync provider network status periodically"
 * [6ba89e8c](https://github.com/kubeovn/kube-ovn/commit/6ba89e8c0ea9ebad9917932979d0feeab9e075a6) use const instead the string
 * [d8ba8d03](https://github.com/kubeovn/kube-ovn/commit/d8ba8d038ec1db6bdd04c06ae51f2964c4674799) when update gateway info, we should append old to new deploy
 * [cc124556](https://github.com/kubeovn/kube-ovn/commit/cc124556d9b0b264531fb24358ae45008e56aef6) resync provider network status periodically
 * [c53b28b1](https://github.com/kubeovn/kube-ovn/commit/c53b28b1e5d8a25ecc4e966343e6f28ca7dacee9) fix underlay subnet in custom VPC
 * [c4a807b1](https://github.com/kubeovn/kube-ovn/commit/c4a807b1d4c2b7df9852b3f1e74c93365ef6ebaa) fix ips update
 * [3269bad9](https://github.com/kubeovn/kube-ovn/commit/3269bad932a630415addb233eefc888d2760a9ba) kube-ovn CNI配置文件名字可配置 (#1318)
 * [491abaa8](https://github.com/kubeovn/kube-ovn/commit/491abaa88e2b8c88b6e81967886ad69df33f32ab) delete the logic of repeated enqueueing
 * [31c0b075](https://github.com/kubeovn/kube-ovn/commit/31c0b07597e059e4bb2f4e67ae1a8dd3ef44e4ff) add log to file, update upgrade script
 * [61c5ebb8](https://github.com/kubeovn/kube-ovn/commit/61c5ebb8399be1323fcdcea6e7c7c8e2b2797bc7) Temporarily comment out the compile and upload of the centos8 compile container.
 * [aef6595f](https://github.com/kubeovn/kube-ovn/commit/aef6595f58cba068e5f61ae0b1f29f15f9e4fbb3) Revert "Temporarily comment out the compile and upload of the centos8 compile…"
 * [79a26873](https://github.com/kubeovn/kube-ovn/commit/79a26873882398f33e44a7d9a8926e02438b16e7) Temporarily comment out the compile and upload of the centos8 compile container.
 * [1fd27d7c](https://github.com/kubeovn/kube-ovn/commit/1fd27d7c036a9d06681c5bea4105f66ae2cc747e) feat: add webhook for subnet update validation
 * [6ab8e369](https://github.com/kubeovn/kube-ovn/commit/6ab8e36980f02baa86164a2aa3f971f3e885a2c1) optimized decision logic
 * [af0baa0c](https://github.com/kubeovn/kube-ovn/commit/af0baa0ca66e5bcc7143dfd747b88098f2db4f03) Use camel case instead of snake case
 * [b6764e0b](https://github.com/kubeovn/kube-ovn/commit/b6764e0bc6f5c9effad18a689a275f5894732cda) append add cidr and excludeIps annotation for namespace
 * [a34bb353](https://github.com/kubeovn/kube-ovn/commit/a34bb353881285a897b31469bbd8faab0a40a3e1) feat: vpc peering connection
 * [9c5556c8](https://github.com/kubeovn/kube-ovn/commit/9c5556c80ba9bf5dfb70c1e7c6bf331539cdea3e) Remove excess code
 * [273eb844](https://github.com/kubeovn/kube-ovn/commit/273eb844be70a7332ad2f6422ee0c521c4765ec6) chore: show install options when installing (#1293)
 * [d5e342c0](https://github.com/kubeovn/kube-ovn/commit/d5e342c068a743b4a940bf983d0a36b41786616c) feat: update provider network via node annotation
 * [e9c9b1ce](https://github.com/kubeovn/kube-ovn/commit/e9c9b1cef55107e5fd0f6af75ad68d1d77c8cf4c) add container compile and insmod
 * [a90b06a8](https://github.com/kubeovn/kube-ovn/commit/a90b06a8d8f8077ca15e0a6d767cde35d489c303) add policy route for centralized subnet
 * [2a39f793](https://github.com/kubeovn/kube-ovn/commit/2a39f793b6674548628e075ee3a3972d1b1b069a) fix: replace ecmp dp_hash with hash by src_ip (#1289)
 * [0fd564e4](https://github.com/kubeovn/kube-ovn/commit/0fd564e400452193a9a299a318b728efe3aad828) Use go to rerimplement ovn-is-leader.sh (#1243)
 * [432c4070](https://github.com/kubeovn/kube-ovn/commit/432c4070e966ba3a22b59fae2a6417603f071815) fix: only log matched svc with np (#1287)
 * [cb1a698a](https://github.com/kubeovn/kube-ovn/commit/cb1a698a254c2d2b1f53fe0fa9d68d1cb2b82790) feat: Replace command health check with k8s tcpSocket check (#1251)
 * [b220f0c6](https://github.com/kubeovn/kube-ovn/commit/b220f0c6ee0652f9b677ecd2c4bafea60a9b8162) add 'virtual' port for vip (#1278)
 * [36c43c48](https://github.com/kubeovn/kube-ovn/commit/36c43c48653cf3782f03fe373b253e24f6e96ec2) skip the missing of kube-dns (#1286)
 * [dad0ef62](https://github.com/kubeovn/kube-ovn/commit/dad0ef62615fda516ac1ccab615aa9b16c9b9657) fix: check if taint exists before un-taint
 * [9365a62d](https://github.com/kubeovn/kube-ovn/commit/9365a62d39dabd3d3aba802d39482d5fbede103e) add policy route for distributed subnet in default vpc
 * [a5ca73c8](https://github.com/kubeovn/kube-ovn/commit/a5ca73c8a88519265a90f1b23be0e69b2bdcc102) ci: add retry to fix flaky test
 * [4fdca714](https://github.com/kubeovn/kube-ovn/commit/4fdca714654b9265b8e20549693b49bdbb0d0087) set up tunnel correctly in hybrid mode
 * [7f8f322b](https://github.com/kubeovn/kube-ovn/commit/7f8f322bac7740c9092695a76540b22609cd2563) check static route conflict
 * [e7bf87b8](https://github.com/kubeovn/kube-ovn/commit/e7bf87b89f2ebb235246c2a03acb636b31d8e833) fix: https://github.com/kubeovn/kube-ovn/issues/1271#issue-1108813998
 * [017e5125](https://github.com/kubeovn/kube-ovn/commit/017e5125207a5d276c8c0a6437eec03eb47f1482) transfer IP/route earlier in OVS startup
 * [ee2ccf1b](https://github.com/kubeovn/kube-ovn/commit/ee2ccf1b93193e0cdc7fee64251e68d6e4f135cd) delete unused constant
 * [4022bd57](https://github.com/kubeovn/kube-ovn/commit/4022bd577cbe142a264c8f7544332711c271d95f) add metric for ovn nb/sb db status
 * [fdcc833a](https://github.com/kubeovn/kube-ovn/commit/fdcc833a3e7a1478f1c0eac44cc3668dfd1ac5d1) add gateway check after update subnet
 * [f40e26ad](https://github.com/kubeovn/kube-ovn/commit/f40e26ad78c375f131c0cbe8c7f4c77fd32449fb) we should first see if a condition is not going to be met
 * [3ae628cb](https://github.com/kubeovn/kube-ovn/commit/3ae628cb8bec67852712d2f854afcc918acd53d1) add judge before use slices index
 * [47625c52](https://github.com/kubeovn/kube-ovn/commit/47625c52c1d8262ded65671a1c325aeef2980caf) prevent multiple namespace reconcile
 * [4455c869](https://github.com/kubeovn/kube-ovn/commit/4455c8692e306db226d2779df9bc6a3a74c51839) prevent multiple namespace reconcile
 * [6b60a587](https://github.com/kubeovn/kube-ovn/commit/6b60a5876caacc68273fb858e0f0b408c34858fd) fix: validate statefulset pod by name
 * [fa02cb21](https://github.com/kubeovn/kube-ovn/commit/fa02cb2161b1d7ec8312569d5b84998fbb72aaca) fix golang and base image versions
 * [f210b934](https://github.com/kubeovn/kube-ovn/commit/f210b93403240a13cbe8d2a704ba0338d088dd79) add back centralized subnet active-standby mode
 * [2557c516](https://github.com/kubeovn/kube-ovn/commit/2557c51670b091d950859dbabcf2a660bf8ebb96) support to add multiple subnets for a namespace
 * [c230ed8a](https://github.com/kubeovn/kube-ovn/commit/c230ed8a1b80181e055d6fb5d5e11a329166b79c) prepare for next release
 * [f95a90eb](https://github.com/kubeovn/kube-ovn/commit/f95a90eb3ee579d01069bf610fcd184d70f22c4e) Support only configure static mac_address

### Contributors

 * Cookie Wang
 * Fudankenshin
 * Mengxin Liu
 * Samuel Liu
 * amoy-xuhao
 * bob199x
 * bobz965
 * caohuilong
 * chestack
 * fanriming
 * gongysh2004
 * hackeren
 * halfcrazy
 * hzma
 * jyjiangkai
 * long.wang
 * lut777
 * pengbinbin1
 * wang_yudong
 * wangyd1988
 * xujunjie
 * xujunjie-cover
 * yi.luo
 * zhangzujian
 * 尚墨
 * 张祖建
 * 罗云鹤
 * 范日明

## v1.9.8 (2022-08-10)

 * [686d913c](https://github.com/kubeovn/kube-ovn/commit/686d913c21d56d6f2a5bb2e6446de7fa2a8f5dc9) set release v1.9.8
 * [8de35693](https://github.com/kubeovn/kube-ovn/commit/8de356930fdaebce8136c4f6f033cad8db4815c5) prepare for release v1.9.8
 * [38ee8301](https://github.com/kubeovn/kube-ovn/commit/38ee83014556702604e77071870ca7f06fde0a43) delete htb qos when releated annotation is deleted (#1788)
 * [85bd5f94](https://github.com/kubeovn/kube-ovn/commit/85bd5f94b2c2a8ce81452caffc6c6099e1b5504b) perf: fix memory leak
 * [46c970d6](https://github.com/kubeovn/kube-ovn/commit/46c970d6dcab21e64b62cf13e0e4a285a734a96e) perf: disable mlockall to reduce memory usage
 * [d7fd3793](https://github.com/kubeovn/kube-ovn/commit/d7fd3793e646c9dc5bbcef40a633a7baa61696df) perf: reduce metrics labels (#1784)
 * [d7a9f5e9](https://github.com/kubeovn/kube-ovn/commit/d7a9f5e91c5a44deb9801e4538386512e45da627) feature: support exchange link names of OVS bridge and provider nic in underlay networks (#1736)
 * [b966dd59](https://github.com/kubeovn/kube-ovn/commit/b966dd596c4d8898a267bf33e7e85d2b8144da00) perf: replace jemalloc to reduce memory usage (#1764)
 * [8bb8b173](https://github.com/kubeovn/kube-ovn/commit/8bb8b17355b91456bde7535747c13ae937e0a894) fix: add omitempty to subnet spec (#1765)
 * [fd676437](https://github.com/kubeovn/kube-ovn/commit/fd67643772e7b1e9ea1c5a39f4a7d3356fe853a8) set sysctl variables on cni server startup (#1758)
 * [7c6250f3](https://github.com/kubeovn/kube-ovn/commit/7c6250f3fa69a7843e03791bb4eb268497874d4c) avoid patch interface deletion & recreation during restart (#1741)
 * [a91056a3](https://github.com/kubeovn/kube-ovn/commit/a91056a3caff58c39f93bd87f5b677f5d50ac62a) enqueue subnets after vpc update (#1722)
 * [e895c5ff](https://github.com/kubeovn/kube-ovn/commit/e895c5ff0062d0730b7dfb2abc120d782afa8907) do not add subnet not processed by kube-ovn to vpc (#1735)
 * [f13f3f46](https://github.com/kubeovn/kube-ovn/commit/f13f3f4621e8788caaf18751fb7766cd3ad7d3cd) add logrotate for kube-ovn log (#1740)
 * [70246fb9](https://github.com/kubeovn/kube-ovn/commit/70246fb9ac6ecb7e38e04274e7fc043fd809bd88) fix: If pod has snat or eip, also need delete staticRoute when delete pod. (#1731)
 * [76e3c670](https://github.com/kubeovn/kube-ovn/commit/76e3c670e75ff9cceef38a291b26c70014ff143a) fix iptables for service traffic when external traffic policy set to local(#1725)
 * [cee39213](https://github.com/kubeovn/kube-ovn/commit/cee392133310cb1f404f88613d2c8e3eaa4018aa) optimize lrp create for subnet in vpc (#1712)
 * [21f0b979](https://github.com/kubeovn/kube-ovn/commit/21f0b979c38d18a5ed2abb93216b6fd3341d2d94) fix: cancel delete staticroute when it's used by NatRule (#1733)
 * [4c2d0c86](https://github.com/kubeovn/kube-ovn/commit/4c2d0c86765d6208c033df095b9a18aa3eee19fe) fix: wrong info when update subnet from dual to ipv4 or ipv6. (#1726)
 * [417176ed](https://github.com/kubeovn/kube-ovn/commit/417176ed9bff4061720a3f6d8e86ab78c2bd42b0) fix: new ovn-ic static route method adapted due to old ovn version (#1718)

### Contributors

 * Mengxin Liu
 * hzma
 * lut777
 * xujunjie-cover
 * zhangzujian
 * 张祖建

## v1.9.7 (2022-07-18)

 * [eb412c96](https://github.com/kubeovn/kube-ovn/commit/eb412c96ff98d50ea5fddcef30f089b11d186c51) set release 1.9.7
 * [07bec2a2](https://github.com/kubeovn/kube-ovn/commit/07bec2a203798c54f8585f1d0469eb3fb713a999) prepare for release 1.9.7
 * [a798a8c2](https://github.com/kubeovn/kube-ovn/commit/a798a8c25633e5dbc8ac72a3d90dce8147aa422a) Get latest vpc data from apiserver instead of cache (#1684)
 * [8bc1b169](https://github.com/kubeovn/kube-ovn/commit/8bc1b1697145ed4dc488588c01862c8f20949a90) update priority range in htb qos (#1688)
 * [ef4673d2](https://github.com/kubeovn/kube-ovn/commit/ef4673d204a83dc7d98ace69b55007fbed265d7e) add upgrade-ovs script (#1681)

### Contributors

 * Mengxin Liu
 * Wang Bo
 * hzma

## v1.9.6 (2022-07-13)

 * [6db04118](https://github.com/kubeovn/kube-ovn/commit/6db04118eb5885dfcf3ce9aa0f584c1d5cab84da) set release 1.9.6
 * [885e41f6](https://github.com/kubeovn/kube-ovn/commit/885e41f6ae43084feb7cfd850e7619e4a1ba7911) prepare for release 1.9.6
 * [556a2cf8](https://github.com/kubeovn/kube-ovn/commit/556a2cf83af6f2dffdce61393d128aaa7c190e13) shim: fix diffs of commits
 * [67da728a](https://github.com/kubeovn/kube-ovn/commit/67da728ad6e72ebb7af4d2101b07939dfc7c2465) fix: change ovn-ic static route to policy (#1670)
 * [a7a11f03](https://github.com/kubeovn/kube-ovn/commit/a7a11f0301adc92a4f4d0513bd393c8a5ccded22) fix: Do not Recreate Logical_Router_Port when Vpc recreated (#1570)
 * [e2ab703a](https://github.com/kubeovn/kube-ovn/commit/e2ab703a4bc0fcbdba564275eb7631e08ab4fc38) feat: vpc peering connection
 * [7699a34b](https://github.com/kubeovn/kube-ovn/commit/7699a34bb6b3400227abba6082f855aad7a32e04) Delete pod if subnet of the pod's owner(sts/vm) updated (#1678)
 * [02e8973a](https://github.com/kubeovn/kube-ovn/commit/02e8973a22e153b49b45607010187add66d38962) security: disable pprof by default (#1672)
 * [0242b9c2](https://github.com/kubeovn/kube-ovn/commit/0242b9c2ade5bce9275dd24f58c050fbe2ccbe91) bgp: consolidate service check and use service const (#1674)
 * [3401d933](https://github.com/kubeovn/kube-ovn/commit/3401d933b8ceb1762f4e4675a32ea5bf38a43459) fix bgp: sync service cache (#1673)
 * [f818ca5c](https://github.com/kubeovn/kube-ovn/commit/f818ca5c782c71387e8a7386190a2b4d54f54293) fix libovsdb (#1664)
 * [a11feff7](https://github.com/kubeovn/kube-ovn/commit/a11feff7e3b4f98d83b1149433f4b9c257897c54) mount modules for auto load ip6tables moudles (#1665)
 * [2882cafc](https://github.com/kubeovn/kube-ovn/commit/2882cafc1a0b94176bd3bf3d34813a39272bbcfb) ignore pod not scheduled when reconcile subnet (#1666)
 * [91dfbbf4](https://github.com/kubeovn/kube-ovn/commit/91dfbbf44c50e9f05e0f08e4cebf6e26c589e078) fix get security group name by external_ids (#1663)
 * [e56d581b](https://github.com/kubeovn/kube-ovn/commit/e56d581b8778ad206fe936cd58abbfc008e26ae1) add policy route when add subnet

### Contributors

 * Mengxin Liu
 * Money Liu
 * Wang Bo
 * gugu
 * hzma
 * lut777
 * wangyd1988
 * 刘睿华
 * 张祖建
 * 范日明

## v1.9.5 (2022-06-28)

 * [8a2cc741](https://github.com/kubeovn/kube-ovn/commit/8a2cc7418191fa5268779ac62da2d1d7405236d4) set for release 1.9.5
 * [9935ab54](https://github.com/kubeovn/kube-ovn/commit/9935ab544d44566c827339e2161049907f73ffc1) fix: no need routed when use v1.multus-cni.io/default-network (#1652)
 * [60d33ca9](https://github.com/kubeovn/kube-ovn/commit/60d33ca97749b75980a678762de597a0e4e7b097) prepare for release 1.9.5
 * [a48e64ae](https://github.com/kubeovn/kube-ovn/commit/a48e64ae469e01b7de308667f61dd69f05586954) CI: delete resources in order to avoid a long time waiting for subnet deletions. (#1643)
 * [502a7a00](https://github.com/kubeovn/kube-ovn/commit/502a7a00480de870e3de33dca5517c523835989b) set networkpolicy log default to false (#1633)
 * [0bda2e6f](https://github.com/kubeovn/kube-ovn/commit/0bda2e6f6aceae063ec22a972bec7d00d2764491) update policy route when join subnet cidr changed (#1638)
 * [3cfafe40](https://github.com/kubeovn/kube-ovn/commit/3cfafe40d35cfda70782c857a107918016ce22c6) ci: update trivy options (#1637)
 * [71dba393](https://github.com/kubeovn/kube-ovn/commit/71dba393dd75fbc9726cdcce12fcf5bbb89f1d46) increase initial delay of ovs-ovn liveness probe (#1634)
 * [cf0bbd92](https://github.com/kubeovn/kube-ovn/commit/cf0bbd9212c4438f8144d56f99a4f65a55550c94) wait ovn-central pods running before delete ovs-ovn pods (#1627)
 * [0877c3a7](https://github.com/kubeovn/kube-ovn/commit/0877c3a753bfd3a85c4c4f67b7af3f38de38ed5a) get dbstatus for all ovn-central pod (#1619)
 * [51c409bd](https://github.com/kubeovn/kube-ovn/commit/51c409bdc5e285154e007e085c15e098ee98dc81) fix issues about OVN policy routing
 * [637503b4](https://github.com/kubeovn/kube-ovn/commit/637503b46f9429fef62b81cd6585796ca8255fad) use policy route instead of static route (#1618)

### Contributors

 * hzma
 * xujunjie-cover
 * zhangzujian
 * 张祖建

## v1.9.4 (2022-06-19)

 * [c85ab203](https://github.com/kubeovn/kube-ovn/commit/c85ab20329420f1e494a1d1d5810581102ca3316) ci: disable cilium e2e for release
 * [0a841aa1](https://github.com/kubeovn/kube-ovn/commit/0a841aa10a67b7b33b35fe59a75d106688bc874b) prepare for release 1.9.4
 * [f99f4e81](https://github.com/kubeovn/kube-ovn/commit/f99f4e815f4f886859444043f77f882980a6d722) update ovs health check, delete connection to ovn sb db (#1588)
 * [82d7dd37](https://github.com/kubeovn/kube-ovn/commit/82d7dd37b4a23bb9d7abea63c33a3c473673e0f4) fix: all cluster pod will be in podadd queue (#1587)
 * [3c68cb9b](https://github.com/kubeovn/kube-ovn/commit/3c68cb9bbba5f3d1175a3cbcdd6c08d0e196e49a) fix pod could not be ready (#1562)
 * [f39ff7a8](https://github.com/kubeovn/kube-ovn/commit/f39ff7a8b74aefecd15c67ae1d7e62cdeae27692) fix: delete pod panic when delete vm or statefulset. (#1565)
 * [4c60872f](https://github.com/kubeovn/kube-ovn/commit/4c60872fcbf49cd396d31597677ab3ca8a07e0bc) fix: keep vm's and statefulset's ips when user specified subnet (#1520)
 * [81781a01](https://github.com/kubeovn/kube-ovn/commit/81781a0117a37652e630baef441dc4c9edf0128c) do not gc vm pod lsp when vm still exists (#1558)
 * [4a28c014](https://github.com/kubeovn/kube-ovn/commit/4a28c0149292576ab15550d4a1fce4e2ba24d52f) fix exec cmd in vpc nat gateway (#1556)
 * [67db2bf3](https://github.com/kubeovn/kube-ovn/commit/67db2bf3158e951e003052a5a8a5b1a38b7aa0be) CNI: do not return route if nic is not eth0 (#1555)
 * [d5fce51d](https://github.com/kubeovn/kube-ovn/commit/d5fce51d2ccb2904e9162d5905fde0380f4ae782) exit kube-ovn-controller on stopped leading (#1536)
 * [05a4b4dc](https://github.com/kubeovn/kube-ovn/commit/05a4b4dc1ca7a94259f080fdc8ddb9a46126c045) remove name for default drop acl in networkpolicy (#1522)
 * [6fcc1975](https://github.com/kubeovn/kube-ovn/commit/6fcc19756bb98035082ec51f112c279bfb694f88) tmp cancel cilium external svc test (#1531)
 * [fe3bb3e5](https://github.com/kubeovn/kube-ovn/commit/fe3bb3e53721437740b8895072a2c572b4ae1c16) move dumb-init from base images to kube-ovn image

### Contributors

 * hzma
 * lut777
 * xujunjie-cover
 * 刘睿华
 * 张祖建

## v1.9.3 (2022-05-13)

 * [a2ba0c15](https://github.com/kubeovn/kube-ovn/commit/a2ba0c1503d56110084123591c8ff52f964bcd52) release 1.9.3
 * [0695d31e](https://github.com/kubeovn/kube-ovn/commit/0695d31e2b780f2d874e3e0caf95d89f6346a8c1) fix defunct ovn-nbctl daemon
 * [f8594a29](https://github.com/kubeovn/kube-ovn/commit/f8594a29eb5c7dbdf6887af081cfa32db35c3cb8) optimize ovs request in cni (#1518)
 * [08f2961d](https://github.com/kubeovn/kube-ovn/commit/08f2961d98bd2a48f6b570f54329265f4d12fbff) optimize node port-group check (#1514)
 * [9ec4a430](https://github.com/kubeovn/kube-ovn/commit/9ec4a43019e088b80f7c863345a25e443a4dca80) reduce ovs-ovn restart downtime (#1516)
 * [b55fa987](https://github.com/kubeovn/kube-ovn/commit/b55fa98765d63ced385ca20dbd7b2ee3a479d886) prepare for release 1.9.3
 * [e4ba2e6d](https://github.com/kubeovn/kube-ovn/commit/e4ba2e6ddb6394890e182013d3a848e7957a5262) fix: ovs trace flow always ends with controller action (#1508)
 * [2e681af3](https://github.com/kubeovn/kube-ovn/commit/2e681af36687db2422ca15af52e6e65bd1181275) optimize IPAM initialization
 * [76fe9cef](https://github.com/kubeovn/kube-ovn/commit/76fe9cef23464e70a9399ea4e5031dc3bbe7b6fb) ci: skip some checks
 * [51dc9243](https://github.com/kubeovn/kube-ovn/commit/51dc92431a748a2f2453870c7629a1f6083384d5) delete ipam record and static route when gc lsp (#1490)

### Contributors

 * Mengxin Liu
 * hzma
 * zhangzujian

## v1.9.2 (2022-04-25)

 * [6273d294](https://github.com/kubeovn/kube-ovn/commit/6273d2940a52c89f6722101b19fbb7b4aca988f1) release for v1.9.2
 * [c98322d7](https://github.com/kubeovn/kube-ovn/commit/c98322d7b9413c991af94a46f35750d999b7476e) fix: wrong vpc-nat-gateway arm image (#1482)
 * [bc4f761c](https://github.com/kubeovn/kube-ovn/commit/bc4f761ca57059875b3eb6d155cc0fce93b5563c) add delete ovs pods after restore nb db (#1474)
 * [945f2336](https://github.com/kubeovn/kube-ovn/commit/945f233661bde2b8626763ae1735a313f10c142b) delete monitor noexecute toleration (#1473)
 * [35ecc687](https://github.com/kubeovn/kube-ovn/commit/35ecc687dc6717d9199e199d792b2851db08f908) add env-check (#1464)
 * [1f68e12a](https://github.com/kubeovn/kube-ovn/commit/1f68e12a5ca03def17e64057d945ba796e9de957) append metrics (#1465)
 * [302156bc](https://github.com/kubeovn/kube-ovn/commit/302156bcb05f54a99891a3aac5715154ba78167e) masquerade packets from Pods to service IP
 * [4faa8831](https://github.com/kubeovn/kube-ovn/commit/4faa88311d5988af2604456654a20585d9a9a0ae) add kube-ovn-controller switch for EIP and SNAT
 * [300a1643](https://github.com/kubeovn/kube-ovn/commit/300a16437bcc25630c35f34846654f5de2d1736e) ignore cni cve
 * [75383df3](https://github.com/kubeovn/kube-ovn/commit/75383df313aa5dae97ab8192fcc2aa8305b40dbe) add routed check in circulation (#1446)
 * [c4f5f4d6](https://github.com/kubeovn/kube-ovn/commit/c4f5f4d67b8c195c8c9f01bff9ebe07172db9973) modify init ipam by ip crd only for sts pod (#1448)
 * [135798dc](https://github.com/kubeovn/kube-ovn/commit/135798dcce63b532fcdb40f1eb67f476737dd19f) log: show the reason if get gw node failed (#1443)
 * [9bec51be9](https://github.com/kubeovn/kube-ovn/commit/9bec51be9768f0e8c78204133aff7fb5ca7f90cb) modify webhook img to independent image (#1442)
 * [e1d6dbf6](https://github.com/kubeovn/kube-ovn/commit/e1d6dbf6808755e9cb624485054c711ef61a3d5d) support keep-vm-ip and live-migrate at the same time (#1439)
 * [613b6ae5](https://github.com/kubeovn/kube-ovn/commit/613b6ae54e80dd9361154de99b1a09ea63aec6b8) update alpine to fix CVE-2022-1271
 * [553bedd2](https://github.com/kubeovn/kube-ovn/commit/553bedd2fab1147f1037f71399b08a093873af5a) fix adding key to delete Pod queue
 * [d899cc97](https://github.com/kubeovn/kube-ovn/commit/d899cc97021cdd5d8cbe34fdcfc3124c0e6fc745) fix IPAM initialization
 * [e159443d](https://github.com/kubeovn/kube-ovn/commit/e159443db6bd93ac32163ba6ebe7db3141784052) ignore all link local unicast addresses/routes
 * [06bd4f86](https://github.com/kubeovn/kube-ovn/commit/06bd4f861bb46dc1b3e75722de157dbd7355f5fe) fix error handling for netlink.AddrDel
 * [71e3f119](https://github.com/kubeovn/kube-ovn/commit/71e3f119307c1549c3cf3e834fc542c9eec1adad) replace pod name when create ip crd
 * [8e65f6f6](https://github.com/kubeovn/kube-ovn/commit/8e65f6f608548e12b0f2b29af0c56b8212a47d93) support alloc static ip from any subnet after ns supports multi subnets (#1417)
 * [9bc2f96a](https://github.com/kubeovn/kube-ovn/commit/9bc2f96a80fddbd4fa5e4d6a0cc42b58f73a33fd) fix provider-networks status
 * [269f819a](https://github.com/kubeovn/kube-ovn/commit/269f819a36ae8c73780d54415aa8ad816a3189a4) recover ips CR on IPAM initialization
 * [dc43dc20](https://github.com/kubeovn/kube-ovn/commit/dc43dc20a4354907051859cf7bd00d88108dfb6d) create ip crd in kube-ovn-controller (#1413)
 * [41f8e26b](https://github.com/kubeovn/kube-ovn/commit/41f8e26b791c509220bca9e6bc2bc24eb328afab) add condition for triggering the deletion of redundant chassises in sbdb (#1411)
 * [2aedc6ac](https://github.com/kubeovn/kube-ovn/commit/2aedc6ac39990b82ef09746bf38199037f16188e) fix: do not recreate port for terminating pods (#1409)
 * [d5556404](https://github.com/kubeovn/kube-ovn/commit/d5556404700bab6dbc1979a8b348d5d2f056906b) avoid frequent ipset update
 * [c86ff85e](https://github.com/kubeovn/kube-ovn/commit/c86ff85e81c923e86a021ffb91ed9ee2c37171ce) fix: The underlay physical gateway config by external-gw-addr when use snat&eip (#1400)
 * [deea9ded](https://github.com/kubeovn/kube-ovn/commit/deea9ded6b6df99508a4aa262a8a25ac2ea67cfe) add reset for kube-ovn-monitor metrics (#1403)
 * [899de6ff](https://github.com/kubeovn/kube-ovn/commit/899de6ffc52776405a647de1edd2a07aba5deedc) check the cidr format whether is correct (#1396)
 * [b54364b4](https://github.com/kubeovn/kube-ovn/commit/b54364b469b8d7a177206d98384a117521b8b701) update dockerfile to use v1.9.1 base img
 * [24190501](https://github.com/kubeovn/kube-ovn/commit/2419050109c52b0ceb839fec135acfaf5905cc89) append vm deletion check
 * [1953712a](https://github.com/kubeovn/kube-ovn/commit/1953712a41349abc35301c338a706d5d59338ec8) delete repeat para
 * [7c0348a7](https://github.com/kubeovn/kube-ovn/commit/7c0348a777212ae50bb566f8824cc1325185bdbe) update nodeips for restore cmd in ko plugin
 * [f320ef8f](https://github.com/kubeovn/kube-ovn/commit/f320ef8fa07fca8f2a5e6a68bfec8ebb130d51ca) fix external egress gateway
 * [c3e17d8c](https://github.com/kubeovn/kube-ovn/commit/c3e17d8c0df8f55da405957356b48200f057f255) add missing link scope routes in vpc-nat-gateway
 * [9d9d5878](https://github.com/kubeovn/kube-ovn/commit/9d9d58784d6476866c51c306e27d52c1ab4af253) increase memory limit of ovn-central
 * [c4092113](https://github.com/kubeovn/kube-ovn/commit/c4092113f7650da07cc459fe804308d127453f85) fix range loop
 * [7397db27](https://github.com/kubeovn/kube-ovn/commit/7397db27ba346b6a1c4efed23f4af960c677ba6e) update script to add restore plugin cmd

### Contributors

 * Mengxin Liu
 * hzma
 * lut777
 * wangyd1988
 * xujunjie-cover
 * zhangzujian

## v1.9.1 (2022-03-09)

 * [46eb49ad](https://github.com/kubeovn/kube-ovn/commit/46eb49adca18cae8a352b4b5949a7250c7a1f91a) release update 1.9.1 changelog (#1361)
 * [59594fed](https://github.com/kubeovn/kube-ovn/commit/59594fed8406d5dc75db1d1e9ee671af5ca506b7) add restore process for ovn nb db
 * [de794986](https://github.com/kubeovn/kube-ovn/commit/de794986cc6b67ce565d778ad7a0f09d278b49dd) optimize kube-ovn-monitor yaml
 * [47a16c38](https://github.com/kubeovn/kube-ovn/commit/47a16c38fdb751cd50af6898bcfe4313d8180f8d) add reset porocess for ovs interface metrics
 * [a3618bcd](https://github.com/kubeovn/kube-ovn/commit/a3618bcd8912912f35b89d6663d431d294138ca3) fix SNAT/PR on Pod startup
 * [81247723](https://github.com/kubeovn/kube-ovn/commit/81247723de608dc7948b603461341b8fd26343f9) modify ipam v6 release ip problem
 * [0006902b](https://github.com/kubeovn/kube-ovn/commit/0006902b3ddfd04f8022aa92acd17c9073275663) skip ping gateway for pods during live migration
 * [092db781](https://github.com/kubeovn/kube-ovn/commit/092db781867ad34e3b6f1088f04bdd3c1f7d5a4f) update flag parse in webhook
 * [222a1fb6](https://github.com/kubeovn/kube-ovn/commit/222a1fb638f3d11ef573fae5b02ba0cd41ff69d5) feat: add webhook for subnet update validation
 * [0615254e](https://github.com/kubeovn/kube-ovn/commit/0615254edd4d502c0b1c16a8e42e77ee02d01d94) keep ip for kubevirt pod
 * [87bb7f18](https://github.com/kubeovn/kube-ovn/commit/87bb7f18b145d0bc9f5c8fb9b13710afc77e5a21) add check for pod update process
 * [7886467a](https://github.com/kubeovn/kube-ovn/commit/7886467ab31694b1fc4bf00ac281e22a99262490) fix ips update
 * [ab3f0a6d](https://github.com/kubeovn/kube-ovn/commit/ab3f0a6d2be67907d0fae1d55244292142bbb0d4) append htbqos para in crd yaml
 * [a68a55f9](https://github.com/kubeovn/kube-ovn/commit/a68a55f9762a5463996ac5466d2ffa6b39c8e69c) fix: replace ecmp dp_hash with hash by src_ip (#1289)
 * [dd08ecab](https://github.com/kubeovn/kube-ovn/commit/dd08ecabe370f818cc15680d88149a2ed0ba1d1c) fix OVS bridge with bond port in mode 6
 * [5fd56d1e](https://github.com/kubeovn/kube-ovn/commit/5fd56d1e12f1d3f5fad9151de2fe767a1da935c1) fix: continue of deletion for del pod failed when can't found vpc or subnet (#1335)
 * [0d114958](https://github.com/kubeovn/kube-ovn/commit/0d11495840316233b7a0b78b84ee389257085a7a) Fix usage of ovn commands
 * [621e2b57](https://github.com/kubeovn/kube-ovn/commit/621e2b571eb1fd66db305c85155e5232ac6e7559) resync provider network status periodically
 * [10ac8c3a](https://github.com/kubeovn/kube-ovn/commit/10ac8c3aff2b365fdb114132d044544fd662399b) Revert "resync provider network status periodically"
 * [fadc1316](https://github.com/kubeovn/kube-ovn/commit/fadc13162c8d17cf3ba654dd09469dbe06557ab5) fix statefulset Pod deletion
 * [b74eaccc](https://github.com/kubeovn/kube-ovn/commit/b74eaccc33fb436fbbaffd47f4a6b31c3ebcfde7) resync provider network status periodically
 * [9a0f708f](https://github.com/kubeovn/kube-ovn/commit/9a0f708fdc1f06ac60206c837ccc572129731b88) fix underlay subnet in custom VPC
 * [69b3d72a](https://github.com/kubeovn/kube-ovn/commit/69b3d72a02580dc5e270c3790a02e5be24f0916c) append add cidr and excludeIps annotation for namespace
 * [c63cb106](https://github.com/kubeovn/kube-ovn/commit/c63cb1067df23b3095b2f51ac0f7fc57ca3303d0) support to add multiple subnets for a namespace
 * [3f818b72](https://github.com/kubeovn/kube-ovn/commit/3f818b729c7ddcf7d9f8ce9a63a8caf9ca05dbcd) feat: update provider network via node annotation
 * [57f16570](https://github.com/kubeovn/kube-ovn/commit/57f16570ad32d3f25b08b9f54db09005f0a84841) fix: only log matched svc with np (#1287)
 * [288c5fe9](https://github.com/kubeovn/kube-ovn/commit/288c5fe9e9492df75a33b4faa24b536c03673863) transfer IP/route earlier in OVS startup
 * [4c4390b3](https://github.com/kubeovn/kube-ovn/commit/4c4390b36ebfc056e458d4c473158b29a192f437) add metric for ovn nb/sb db status
 * [92e7b975](https://github.com/kubeovn/kube-ovn/commit/92e7b975a9caffb97484bb8bf7fda8306d18f8be) check static route conflict
 * [67a7d85b](https://github.com/kubeovn/kube-ovn/commit/67a7d85baec9cb90d8840fabc9ea23d7fd8520d6) set up tunnel correctly in hybrid mode
 * [eabed9cc](https://github.com/kubeovn/kube-ovn/commit/eabed9ccdb39191d3144ef4e6e88e570bc014c02) fix clusterrole in ovn-ha.yaml
 * [65b83219](https://github.com/kubeovn/kube-ovn/commit/65b8321962558ff94e6b303b2b7b6d4c2e036b3a) add gateway check after update subnet
 * [f3f8c4dc](https://github.com/kubeovn/kube-ovn/commit/f3f8c4dc17f3f260c61e2c6add90fff9b65fd0db) fix: validate statefulset pod by name
 * [b5544bc3](https://github.com/kubeovn/kube-ovn/commit/b5544bc3cbf8bf614e56070ffa86cf04685b3532) add back centralized subnet active-standby mode

### Contributors

 * Mengxin Liu
 * chestack
 * hzma
 * lut777
 * xujunjie
 * xujunjie-cover
 * zhangzujian

## v1.9.0 (2022-01-12)

 * [e4d48df3](https://github.com/kubeovn/kube-ovn/commit/e4d48df38d6ed16acb77d92d66686df7d40f55ea) prepare for release 1.9.0
 * [c830594d](https://github.com/kubeovn/kube-ovn/commit/c830594dc5b9575531b34eea358bad019d0ff3a5) fix: liveMigration with IPv6
 * [e52b6897](https://github.com/kubeovn/kube-ovn/commit/e52b689764cad9166f6b499b1757b3e76ee4a765) update networkpolicy port process
 * [851ad0ce](https://github.com/kubeovn/kube-ovn/commit/851ad0ce6874fe2ec1dff3ecb8ded3079ce27f18) Add args to configure port ln-ovn-external
 * [5d95d628](https://github.com/kubeovn/kube-ovn/commit/5d95d62857b3a4ffdcabe2b8ae945d48d9ef1249) update check for delete statefulset pod
 * [695f4532](https://github.com/kubeovn/kube-ovn/commit/695f45320200a29948264edac201675892cd8e4d) ignore hostnetwork pod when initipam
 * [4b98d15f](https://github.com/kubeovn/kube-ovn/commit/4b98d15fb07ab23adefae6fb23aee365e15db18a) kubectl-ko: support trace Pods being created
 * [63bc25ea](https://github.com/kubeovn/kube-ovn/commit/63bc25ea84da3adafc16bc9a1467adb6930aa9b1) add dnsutils for base image
 * [6318d004](https://github.com/kubeovn/kube-ovn/commit/6318d004990743c87d88ff7885d01bb1e36fd858) Add new arg to configure ns of ExternalGatewayConfig
 * [71522920](https://github.com/kubeovn/kube-ovn/commit/71522920498015847c12455728fc38d17eaab5b5) update scripts for 1.8.2
 * [960f02c1](https://github.com/kubeovn/kube-ovn/commit/960f02c15bfb2665d9d2589713a6fbbab9958a69) Optimized decision logic
 * [8974f6a3](https://github.com/kubeovn/kube-ovn/commit/8974f6a3712d14d6fd5674b449dd62f903c22f98) add svc cidr in ovs LB for optimization
 * [0192a9ae](https://github.com/kubeovn/kube-ovn/commit/0192a9ae8851b14a81fbf6dad7b4ebb006a4c71e) add doc for gateway pod in default vpc
 * [1f9dc754](https://github.com/kubeovn/kube-ovn/commit/1f9dc754c9fdebe089699a07fec2fc3e76e1dc12) optimize log for node port-group
 * [36d6b00a](https://github.com/kubeovn/kube-ovn/commit/36d6b00a8fae03b1df57116befc3b101a5f348dd) fix iptables rules and service e2e
 * [8dc938d8](https://github.com/kubeovn/kube-ovn/commit/8dc938d83d923ad1ffdcbbf72e559dc9497ddeed) add kubectl-ko to docker image
 * [c4cc8f0d](https://github.com/kubeovn/kube-ovn/commit/c4cc8f0d9b43d9ed4b5f21879263eab1e235cd61) fix: invalid syntax error
 * [a4f4cb49](https://github.com/kubeovn/kube-ovn/commit/a4f4cb490ff257063f859cead802013018517563) fix pod tolerations
 * [8611de82](https://github.com/kubeovn/kube-ovn/commit/8611de8229214b732c858175bc2822a25bfcd02b) modify pod's process of update for use multus cni as default cni
 * [5ab83ba4](https://github.com/kubeovn/kube-ovn/commit/5ab83ba42b41e4dbab912be345a42adc0fdefdd1) fix installation script
 * [09ef9be0](https://github.com/kubeovn/kube-ovn/commit/09ef9be09c3fc1809dc0d09209502cbe156c7682) add log for ecmp route
 * [791b00f4](https://github.com/kubeovn/kube-ovn/commit/791b00f42c660f1afbb0ce1d4714344558774e12) fix: ipv6 traffic still go into ct
 * [55e6a8ca](https://github.com/kubeovn/kube-ovn/commit/55e6a8ca326c73a62c91bfd6622a200d8366d1e7) append check for centralized subnet nat process
 * [58a44fb2](https://github.com/kubeovn/kube-ovn/commit/58a44fb2b7d90086af77095aa20cfcc48e353a36) move chassis judge to the end of node processing
 * [9f0c42fa](https://github.com/kubeovn/kube-ovn/commit/9f0c42fae734db095a12921555eb8ae140bac192) change nbctl args 'wait=sb' to 'no-wait'
 * [6f356705](https://github.com/kubeovn/kube-ovn/commit/6f35670556dc8fa924d0b41198433e8afc78084a) use different ip crd with provider suffix for pod multus nic
 * [f7b595dc](https://github.com/kubeovn/kube-ovn/commit/f7b595dcf67fc2222c387058b88c811e6ed1116d) fix service cidr in dual stack cluster
 * [c510b439](https://github.com/kubeovn/kube-ovn/commit/c510b43972bb8ee217e05856c8e0022f6c93c86b) add healthcheck cmd to probe live and ready
 * [e14bc40c](https://github.com/kubeovn/kube-ovn/commit/e14bc40c512324808deab974d2235fa5a61b5ba1) delete frequently log
 * [bde98e75](https://github.com/kubeovn/kube-ovn/commit/bde98e7571069d50caead0e4b7968d4e2947feb4) support running ovn-ic e2e on macOS
 * [727ea53a](https://github.com/kubeovn/kube-ovn/commit/727ea53a809a1846c6d701cab5bca26b151a9313) pinger: fix getting empty PodIPs
 * [205a0c02](https://github.com/kubeovn/kube-ovn/commit/205a0c021e3be8d8df0931bbbbba29f392fa8220) fix cni deepcopy
 * [650ea6d3](https://github.com/kubeovn/kube-ovn/commit/650ea6d3c5693b9e11f1b016c936b4a95a1b11a4) add cilium e2e
 * [46ba84ee](https://github.com/kubeovn/kube-ovn/commit/46ba84eefebd9fc15e6749c253b4dd4fd8cf1840) filter used qos when delete qos
 * [1de284eb](https://github.com/kubeovn/kube-ovn/commit/1de284ebc8504e53cd4a2ec0388d407731226671) add protocol check when subnet is dual-stack
 * [1f4a247d](https://github.com/kubeovn/kube-ovn/commit/1f4a247ddf8801f32b5b177dd8408a5ea0827a60) lint: make go-lint happy
 * [91f3fa4b](https://github.com/kubeovn/kube-ovn/commit/91f3fa4b9b715faa09ad7e52f547e59bb9b920a3) some fixes
 * [d57bc1d7](https://github.com/kubeovn/kube-ovn/commit/d57bc1d72e66cfa0822cdb73b216af74fe4bb7d7) compatible with OVN 20.06
 * [9116425a](https://github.com/kubeovn/kube-ovn/commit/9116425a42fdb3fb6cc8812d04cc0aa900c0b385) use multus-cni as default cni to assign ip
 * [d18323a4](https://github.com/kubeovn/kube-ovn/commit/d18323a4906626eb2dbb51a5d81085cbc34c86b9) some fixes
 * [668c2125](https://github.com/kubeovn/kube-ovn/commit/668c2125646cc1c2d29893457885dc2d245170d7) perf: jemalloc and ISA optimization
 * [5c08d28d](https://github.com/kubeovn/kube-ovn/commit/5c08d28da0eb7fa6981e71ca65c59f8dedeb42ed) fix: check np switch
 * [36571555](https://github.com/kubeovn/kube-ovn/commit/36571555676739e4e2e6949cd37d13999b5d9175) fix: port security
 * [e713bdf0](https://github.com/kubeovn/kube-ovn/commit/e713bdf0f281415536eeab623385fd534e694d6c) fix nat rule
 * [d8e84cf0](https://github.com/kubeovn/kube-ovn/commit/d8e84cf06fc232ae0e954dfb20e3aa08ce0de30e) When netpol is added to a workload, the workload's POD can be accessed using service
 * [51365b41](https://github.com/kubeovn/kube-ovn/commit/51365b41d1745f45c401943b75589ad1779efaf4) when update subnet's execpt ip,we should filter repeat ip
 * [5aacec59](https://github.com/kubeovn/kube-ovn/commit/5aacec592b8f63c03cf3b2d41b0292a48a084a62) update wechat image
 * [6c8fa978](https://github.com/kubeovn/kube-ovn/commit/6c8fa978b008a75727ce97e1d326d2b5a7c096df) fix: do not reuse released ip after subnet updated
 * [e4648cc8](https://github.com/kubeovn/kube-ovn/commit/e4648cc81c2a8cc9bf11157379ee5e99d5c97e0c) update: update 1.7-1.8 script
 * [b1f8332c](https://github.com/kubeovn/kube-ovn/commit/b1f8332c68770fe1db8a1dae2bdb84ef601bc195) perf: do not send traffic to ct if not designate to svc
 * [178cf7b8](https://github.com/kubeovn/kube-ovn/commit/178cf7b87fbaecd4868620c7fdbd9e5a3fa89145) fix: add back the leader check
 * [7be43c97](https://github.com/kubeovn/kube-ovn/commit/7be43c9748ba4470d7d40c63f39fd52352b0913a) fix port_security
 * [e596c3c4](https://github.com/kubeovn/kube-ovn/commit/e596c3c4b0f5118db38bb308dce9a21bf25ea952) sync live migration vm port
 * [e8b1ff5b](https://github.com/kubeovn/kube-ovn/commit/e8b1ff5b4ec877840c1647c4544b23c0a38ac252) docs: add f5 ces integration docs
 * [7058d568](https://github.com/kubeovn/kube-ovn/commit/7058d5680f9338e3d8ee0dd711a535ab2faba368) update Go modules
 * [84dbb102](https://github.com/kubeovn/kube-ovn/commit/84dbb102b3782abe57f6cd66e51abff9f219ac4e) update delete operation for statefulset pod
 * [e9e2c911](https://github.com/kubeovn/kube-ovn/commit/e9e2c9111e26b5c7b4814104ba94a76949b3f548) chore: update klog to v2 which embed log rotation
 * [fafd5555](https://github.com/kubeovn/kube-ovn/commit/fafd5555284ee8d766109f1a785cf043cd4e1715) fix: add kube-ovn-cni prob timeout
 * [490590a4](https://github.com/kubeovn/kube-ovn/commit/490590a4694b753f9733559492bc8380b4a2680a) append add db compact for nb and sb db
 * [4fb302f5](https://github.com/kubeovn/kube-ovn/commit/4fb302f5ebbed2b363c3bafb9bb6157328641969) deleting all chassises which are not nodes
 * [c49a7404](https://github.com/kubeovn/kube-ovn/commit/c49a740401323cc09d2e09e6d8abc00bbdca827b) add db compact for nb and sb db
 * [3b7ec06c](https://github.com/kubeovn/kube-ovn/commit/3b7ec06c766d59598a7a178f376f85682a4be84d) add vendor param for fix list LR
 * [ae23d3df](https://github.com/kubeovn/kube-ovn/commit/ae23d3dfd500820cb2c5ff79baca58f234f1efb6) fix LB: skip service without cluster IP
 * [df3d3977](https://github.com/kubeovn/kube-ovn/commit/df3d3977b9f19941060d0d7a4af63ef99c4ef494) add webhook with cert-manager issued certificate
 * [2be11269](https://github.com/kubeovn/kube-ovn/commit/2be11269a9646f4cc124b8c6f2178dd4fd289bbe) security: update base ubuntu image
 * [eb364717](https://github.com/kubeovn/kube-ovn/commit/eb3647176ddbed8f61aedb688d6d2c4800e60446) add pod in default vpc to node port-group
 * [ea300d2b](https://github.com/kubeovn/kube-ovn/commit/ea300d2bf179d58f51465c5e767d487e606f7894) fix pinger's compatibility for k8s v1.16
 * [3837b0a2](https://github.com/kubeovn/kube-ovn/commit/3837b0a231659728d51d3ad64570c565d66bfed4) check IPv4 gateway by resolving gateway MAC in underlay subnets
 * [75604b5d](https://github.com/kubeovn/kube-ovn/commit/75604b5d94ef5ff9ada5f5533ebaa157894adf8c) add nodeSelector for vpc-nat-gateway pod
 * [fac6c725](https://github.com/kubeovn/kube-ovn/commit/fac6c725acf646e5f3e5fd3fc3a797402b7a10df) do not send multicast packets to conntrack
 * [c3004bbc](https://github.com/kubeovn/kube-ovn/commit/c3004bbc2aa9c7d020fead19da390144c6d18162) Revert "support to set NB_Global option mcast_privileged"
 * [2802b94d](https://github.com/kubeovn/kube-ovn/commit/2802b94ddb002ee434236e564ce18a6e23350850) add ip address for lsp
 * [28a93927](https://github.com/kubeovn/kube-ovn/commit/28a93927af69358c35116e68a53ebe703c1326ae) fix: no need to set address for ls to lr port
 * [2048007a](https://github.com/kubeovn/kube-ovn/commit/2048007a2c8a0e7fa5f691639782c69ce7e3aae1) add sg acl check when init
 * [b9abee71](https://github.com/kubeovn/kube-ovn/commit/b9abee71542b934f36f6cfb569f46d0bc2f79201) cleanup command flags
 * [54a3b913](https://github.com/kubeovn/kube-ovn/commit/54a3b913ef3c1481318512e3aac3fd81b21bcab8) replace port-group named address-set with port-group since there's no ip set for lsp when create lsp
 * [743502cd](https://github.com/kubeovn/kube-ovn/commit/743502cd744c3c397665b60e81bfdd2817e5beeb) support to set NB_Global option mcast_privileged
 * [a5f0256a](https://github.com/kubeovn/kube-ovn/commit/a5f0256a979041adfead4c7ed47b72dd0b828a72) add networkpolicy support for attachment cni
 * [45f64bfa](https://github.com/kubeovn/kube-ovn/commit/45f64bfaf83ff131f88b0121f8e5951d8b852b5e) add process for pod attachment nic with subnet in default vpc
 * [49e9197e](https://github.com/kubeovn/kube-ovn/commit/49e9197e57db258a9baebcdbc3ba997599e27b6d) fix security group
 * [60e896f8](https://github.com/kubeovn/kube-ovn/commit/60e896f8967e0d9a3f4f0f1fa1c3a73083ca5b14) fix the duplicate call about strings.Split
 * [c9f5f4b4](https://github.com/kubeovn/kube-ovn/commit/c9f5f4b46cf9969816b471fec1fa0fb79fa86933) deepcopy fix steps
 * [e0cb19aa](https://github.com/kubeovn/kube-ovn/commit/e0cb19aa73f95bba16872bd532ef4b6fd0d0a1c2) fix: do not nat route traffic
 * [4e4d95d5](https://github.com/kubeovn/kube-ovn/commit/4e4d95d5f31f5c36af2c8d137e39081b6128ec50) fix: Skip MAC address Settings when PCI addresse is unavailable
 * [adce05c7](https://github.com/kubeovn/kube-ovn/commit/adce05c74ea6e89bee155e15b1ff43a1ab3c170e) add ovn-ic e2e
 * [3b6b5034](https://github.com/kubeovn/kube-ovn/commit/3b6b5034d9900d820e93ec08a3f18c5e3298bc88) other CNI can be used as the default network
 * [841f907b](https://github.com/kubeovn/kube-ovn/commit/841f907b0d0e8b40e9a0d186cafc33152e5b818a) fix: move macvlan binary to host
 * [52ec0af4](https://github.com/kubeovn/kube-ovn/commit/52ec0af4721478bb4d0bc4d5aff09960891ca9a2) Revert "ci: init kind cluster before build finish"
 * [a8599325](https://github.com/kubeovn/kube-ovn/commit/a859932540fca9fc5a9b8d862e43f22cf04291e2) fix ko trace
 * [1dd66a77](https://github.com/kubeovn/kube-ovn/commit/1dd66a770612544342c9af0d0d962f23a818640a) add ovn-ic HA deploy
 * [bc3ce0bb](https://github.com/kubeovn/kube-ovn/commit/bc3ce0bbf11c0b6bcb9d621e0cf5c9d94e1b775a) fix node address set name
 * [cbed2820](https://github.com/kubeovn/kube-ovn/commit/cbed282046713526f15d55ef6bba920051237f64) update cni init image
 * [a648bfc6](https://github.com/kubeovn/kube-ovn/commit/a648bfc6a23f6ced4c5688b1689e772ea2511e64) chore: update kind k8s to 1.22 and remove pre 1.16 support
 * [a1d56e97](https://github.com/kubeovn/kube-ovn/commit/a1d56e9737b256da80da09b3aef83efb28f75945) do not set bridge-nf-call-iptables
 * [738c7612](https://github.com/kubeovn/kube-ovn/commit/738c76126e5f2eea8c57073436ab6330d7b31029) use logical router policy for accessing node
 * [6719ee24](https://github.com/kubeovn/kube-ovn/commit/6719ee242b798352a99d12d26b034f58aa1fe401) ci: init kind cluster before build finish
 * [61817bf4](https://github.com/kubeovn/kube-ovn/commit/61817bf417278b553aeec981c84275efac6a623c) reduce qos query with ovs-vsctl cmd
 * [1776c447](https://github.com/kubeovn/kube-ovn/commit/1776c4471447ec2e34fb373019a2c55ff82e1f1b) fix read-only pointer in vlan and provider-network
 * [329228d4](https://github.com/kubeovn/kube-ovn/commit/329228d4a9b11e45bfe122e44089f1f4f66ca9f9) fix: trace in custom vpc
 * [a9c0a4aa](https://github.com/kubeovn/kube-ovn/commit/a9c0a4aa499da7c12991e7bfaa1047291f4e7ada) fix read-only pointer in vlan and provider-network
 * [62df3416](https://github.com/kubeovn/kube-ovn/commit/62df34162ef72710a3e532d8fb489cbb78ffc594) update docs
 * [a546ba95](https://github.com/kubeovn/kube-ovn/commit/a546ba9545d22bb2d366e94e84e4fb5914964dc5) fix LB in dual stack cluster
 * [eb63f72e](https://github.com/kubeovn/kube-ovn/commit/eb63f72ec1db5cd6a5394f8717bb13514fe965f1) fix: check allocated annotation in update handler
 * [55b8b8ac](https://github.com/kubeovn/kube-ovn/commit/55b8b8ac2d543eb99055451b2327d9fe19478049) support using logical gateway in underlay subnet
 * [ef424d73](https://github.com/kubeovn/kube-ovn/commit/ef424d731a07c791943b587ca0e65014dec439df) docs: optimize cilium integration docs
 * [a09e84d0](https://github.com/kubeovn/kube-ovn/commit/a09e84d0c4ac028ce6de7944a60ff1bca0fed3c3) fix: ensure all kube-ovn components deleted before annotate pods
 * [e7aeb96e](https://github.com/kubeovn/kube-ovn/commit/e7aeb96ec789ca45ac1f10ef1945f01cb17dd26e) fix bug: logical switch ts not ready
 * [dc4e693f](https://github.com/kubeovn/kube-ovn/commit/dc4e693f1f90396a9094ce4e3e0136176a5c4b01) Fix unpopulated CPU charts
 * [003723e5](https://github.com/kubeovn/kube-ovn/commit/003723e5881364a27afb44667c2bfb4359c2c2e7) Revert "get default subnet"
 * [418feb1b](https://github.com/kubeovn/kube-ovn/commit/418feb1ba8ea96c804265b6966f0dba28d81debd) add htbqoses rbac
 * [850e4218](https://github.com/kubeovn/kube-ovn/commit/850e42186e373c1456a8c612a10cf143d248cf67) feat: pod can use multiple nic with the same subnet
 * [5840d509](https://github.com/kubeovn/kube-ovn/commit/5840d5093ff6c98710849ba078b02350b4419a78) add error detail
 * [e6377cae](https://github.com/kubeovn/kube-ovn/commit/e6377caefbe46869f3000fef05affe993aa053ca) add check switch for default subnet's gateway
 * [b5b6c326](https://github.com/kubeovn/kube-ovn/commit/b5b6c32678a696efff4e4b10fb84b40b327b5a7d) get default subnet
 * [fbafca41](https://github.com/kubeovn/kube-ovn/commit/fbafca410eb4f79b0252029d1dee117aef8d1dd9) remove node chassis annotation on cleanup
 * [348eaf36](https://github.com/kubeovn/kube-ovn/commit/348eaf3670720181c1e5a82ca10a1bc02a3c1732) update: add 1.7 to 1.8 update scripts
 * [f934613d](https://github.com/kubeovn/kube-ovn/commit/f934613d36f0e756201df03ac5e1bd1e5cf23f9f) base: add macvlan to help vpc setup
 * [cd1dda1e](https://github.com/kubeovn/kube-ovn/commit/cd1dda1e5449f6debd5a6e95e87bf25067bb825d) fix: delete vpc-nat-gw deployment
 * [50eddac3](https://github.com/kubeovn/kube-ovn/commit/50eddac3e95b97136399647b390dcc614257f7fa) ko: check ovsdb storage status
 * [20670e87](https://github.com/kubeovn/kube-ovn/commit/20670e8772d46e1a9a29714d2e18d24727a655b8) fix cleanup.sh and uninstall.sh
 * [b31c4d19](https://github.com/kubeovn/kube-ovn/commit/b31c4d19f4761a846167d352c4606fd612b1441c) use constant instead a string
 * [86f63f26](https://github.com/kubeovn/kube-ovn/commit/86f63f26dbc319e4b65263029ac140f8e4466ccd) fix: check and load ip_tables module
 * [3bfd82b7](https://github.com/kubeovn/kube-ovn/commit/3bfd82b7e08ca9cf1694649c7dd43d5c4234aa62) fix: multus-cni subnet allocation
 * [e5ed1ace](https://github.com/kubeovn/kube-ovn/commit/e5ed1ace04509669b2331d26fb8797f645275cde) docs: add svg
 * [17ff6c55](https://github.com/kubeovn/kube-ovn/commit/17ff6c55f97c484cd37408db678224b2f3b4d648) chore: update install
 * [ce97b94c](https://github.com/kubeovn/kube-ovn/commit/ce97b94c8e7c4c8f18b3702a1d4b757e7720259f) integrate Cilium into Kube-OVN
 * [fda0c17b](https://github.com/kubeovn/kube-ovn/commit/fda0c17b4066f9d439a85b5ca47f9cc526db614d) fix kubectl-ko diagnose
 * [3f8a2b0e](https://github.com/kubeovn/kube-ovn/commit/3f8a2b0ed4551290e047829e197f99a5750ea46d) change inspection logic from manually adding lsp to just readding pod queue
 * [01ca82f9](https://github.com/kubeovn/kube-ovn/commit/01ca82f9ce006dbae6a418fe1bfe8fa8b139d7f0) fix pinger in dual stack cluster
 * [0ba64dea](https://github.com/kubeovn/kube-ovn/commit/0ba64dea916c1898411962c9fc0567724252b863) add e2e testing for dual stack underlay
 * [7f27a05d](https://github.com/kubeovn/kube-ovn/commit/7f27a05d53690aff302593b5e8a29fea68116df1) fix pinger and monitor in underlay networking
 * [6a56f8bb](https://github.com/kubeovn/kube-ovn/commit/6a56f8bb5694336d6758f4567ca6b0220e256fa0) fix kubectl plugin ko
 * [2c9fe438](https://github.com/kubeovn/kube-ovn/commit/2c9fe438de87f1ef2748224782db96f54247c3dc) adjust the location of the log
 * [86ee933a](https://github.com/kubeovn/kube-ovn/commit/86ee933aa16c747a56cee523092d030afc042bc3) ci: push vpc-nat-gateway
 * [f459ca97](https://github.com/kubeovn/kube-ovn/commit/f459ca975fc3d31edb95d3f703b6ec892c85e320) replace api for get lsp id by name
 * [0a533984](https://github.com/kubeovn/kube-ovn/commit/0a533984f0238c21594f334f3153308a5a61f22d) docs：revise vpc.md
 * [78847899](https://github.com/kubeovn/kube-ovn/commit/788478991bdcf5f5b3a9ad6fe883a0de8e50bf32) grafana: optimize grafana dashboard
 * [168a7c97](https://github.com/kubeovn/kube-ovn/commit/168a7c9768a3660ce2c1c1472721b324ef2c1798) In netpol egress rules, except rule should be set to != and should not be ==
 * [d7edf24b](https://github.com/kubeovn/kube-ovn/commit/d7edf24bf98d8229e4e71827e7b5d0f26c7b295a) ci: add vpc-nat-gateway build
 * [5cd32df8](https://github.com/kubeovn/kube-ovn/commit/5cd32df809a895d0df5b8bae1f0ce4e1d772c31a) Update OVN to version 21.06
 * [dd36d61c](https://github.com/kubeovn/kube-ovn/commit/dd36d61c3b70e20111d8246dcd7eb9b4a770bf05) modify kube-ovn as multus-cni problem
 * [d17f6151](https://github.com/kubeovn/kube-ovn/commit/d17f6151310e7024a2d8a04e89abdff586648832) support to set htb qos priority
 * [c20e0111](https://github.com/kubeovn/kube-ovn/commit/c20e01110e0f2dcc138aac2f13793815fafd5ae2) perf: add fastpath module for 4.x kernel
 * [ff5d3df3](https://github.com/kubeovn/kube-ovn/commit/ff5d3df3f898834709cd73dafbd898e2ceb8b04d) add inspection
 * [3e9f9a99](https://github.com/kubeovn/kube-ovn/commit/3e9f9a99c0bde8fc1857e7a3d73a0a542231d4b5) perf: add stt section and update benchmark
 * [d3842327](https://github.com/kubeovn/kube-ovn/commit/d3842327175880b73a09bfb86a95ddbaf876ebbf) feat: optimize log
 * [4c6c29a3](https://github.com/kubeovn/kube-ovn/commit/4c6c29a36a0300eac7646c590cabe144df915e35) fix: init node with wrong ipamkey and lead conflict
 * [47255a10](https://github.com/kubeovn/kube-ovn/commit/47255a10591ab69ff8566b33ece98875f6f1fff1) fix installation scripts
 * [fd745487](https://github.com/kubeovn/kube-ovn/commit/fd745487040ff067d2b9f161658555201f223857) fix getting LSP UUID by name
 * [1f5719a5](https://github.com/kubeovn/kube-ovn/commit/1f5719a59afc1acf009eb7e3438806c780908bf6) fix StatefulSet down scale
 * [5bccd845](https://github.com/kubeovn/kube-ovn/commit/5bccd8453419cb4477dc925555f686c1bf3a7705) fix vpc policy route
 * [acb82de0](https://github.com/kubeovn/kube-ovn/commit/acb82de0dd34152e537036208e147ec86c5fed6a) docs: update roadmap
 * [87f9b863](https://github.com/kubeovn/kube-ovn/commit/87f9b863ea2e666aa27f1fc99fb6670aff94d6f2) refactor: mute ovn0 ping log and add ping details
 * [a99c4200](https://github.com/kubeovn/kube-ovn/commit/a99c4200cfcd3ef233e81bb778cfc0eac5356c27) fix: wrong link for iptables
 * [52b01c01](https://github.com/kubeovn/kube-ovn/commit/52b01c017bd11c72b0134d7e092e5cb6636590ff) fix IPAM for StatefulSet
 * [51511e63](https://github.com/kubeovn/kube-ovn/commit/51511e633030eb92b6cc29f28338e890ff10f6d7) append externalIds for pod and node when upgrade
 * [391f7014](https://github.com/kubeovn/kube-ovn/commit/391f7014e5c617b8f29723b460e23cf54779c4a5) feature: LoadBalancer for custom VPC
 * [7fd8cf44](https://github.com/kubeovn/kube-ovn/commit/7fd8cf44fb4532fc182652939c0fbed1cf13e8d8) feat: support vip
 * [25f634fb](https://github.com/kubeovn/kube-ovn/commit/25f634fbcbcf54f8f1dfde6f90aed4cd31b65479) fix VPC document
 * [97a5b2a3](https://github.com/kubeovn/kube-ovn/commit/97a5b2a337a511917f600303cf5e0823a4a7b942) fix init ipam
 * [71fcbf12](https://github.com/kubeovn/kube-ovn/commit/71fcbf12153330db0ddae6a1ba8baed628eafcb1) fix: gc lb
 * [2b154b1a](https://github.com/kubeovn/kube-ovn/commit/2b154b1a52fcbb873988fb11900120273a1e4163) Update prometheus.md
 * [1e766f9c](https://github.com/kubeovn/kube-ovn/commit/1e766f9cd76229189a957a78f8cc5d17a3e771cf) feat: support VLAN subnet in VPC
 * [4c013a3e](https://github.com/kubeovn/kube-ovn/commit/4c013a3e186273d01b33cd0812a95925471f7a1a) ci: push dev image to separate repo
 * [39c8a19c](https://github.com/kubeovn/kube-ovn/commit/39c8a19c177808f8c7060c30b2cc903cb53f1ee7) fix: kubeclient timeout
 * [edaf41e0](https://github.com/kubeovn/kube-ovn/commit/edaf41e041ef527abd141a4bbafca6cb445c90fa) fix: serialize pod add/delete order
 * [78a77f79](https://github.com/kubeovn/kube-ovn/commit/78a77f797c43b62086817b5a421789cdedbf6191) perf: increase ovn-nb timeout
 * [5937ccbf](https://github.com/kubeovn/kube-ovn/commit/5937ccbf7aba3d3aee7d5ce49fe7cae1f062a088) fix gc lsp statistic for multiple subnet
 * [c71620ce](https://github.com/kubeovn/kube-ovn/commit/c71620ce129baf8cc2434129ba965440eebb3ed2) fix: re-check ns annotation to avoid annotations lost
 * [d40d5701](https://github.com/kubeovn/kube-ovn/commit/d40d5701522c6ca43e771556848dcd6191337af6) perf: do not diagnose external access
 * [871c1493](https://github.com/kubeovn/kube-ovn/commit/871c1493b251792bb8cbf5ed6c8b9dbd7f4aee9c) feature: vpc support policy route
 * [90b1a2ea](https://github.com/kubeovn/kube-ovn/commit/90b1a2ea4205ebcdcb392760f1af12456543e801) reactor: remove ovn ipam options
 * [7f43f25c](https://github.com/kubeovn/kube-ovn/commit/7f43f25c1cf2f65e0e658894aa4a5312add2f5ec) perf: switch's router port's addresses to "router"
 * [8dbe8f94](https://github.com/kubeovn/kube-ovn/commit/8dbe8f94678a8f5c7ec6259477d7a9e9dd711057) lint: make staticcheck happy
 * [8ad46dad](https://github.com/kubeovn/kube-ovn/commit/8ad46dad4f96855fe413b18a1e3d368b418c5c3f) fix e2e testing
 * [5a126378](https://github.com/kubeovn/kube-ovn/commit/5a126378a3466bb7ce715174c702e0e9454e85b6) prepare for next release
 * [5b70c81d](https://github.com/kubeovn/kube-ovn/commit/5b70c81d3b7707d8cc1efe3632df28822ecf347c) fix variable referrence
 * [42fed929](https://github.com/kubeovn/kube-ovn/commit/42fed929e61bed1fc1e0e654d4b379ab9317d601) fix typos
 * [f59aff27](https://github.com/kubeovn/kube-ovn/commit/f59aff27e39f038eb2a0bca50294f95e072e9a20) refactor: reuse waitNetworkReady to check ovn0 and slightly improve the installation speed
 * [ea723d6d](https://github.com/kubeovn/kube-ovn/commit/ea723d6dc9afed1e246407f8a34499c481d67a6c) fix nat-outgoing/policy-routing on pod startup
 * [2439c86e](https://github.com/kubeovn/kube-ovn/commit/2439c86e6e4945073f023dc51b3ab49a5fa2be19) feat: suport vm live migration

### Contributors

 * Mengxin Liu
 * MengxinLiu
 * azee
 * chestack
 * feixiang43
 * huangjunwei
 * hzma
 * lhalbert
 * liqd
 * luoyunhe
 * lut777
 * pengbinbin1
 * vseeker
 * wang_yudong
 * wangchl01
 * zhangzujian
 * 范日明

## v1.8.9 (2022-07-13)

 * [9050b22d](https://github.com/kubeovn/kube-ovn/commit/9050b22da0750de7f8880f937de42bb6363024c4) set release 1.8.9
 * [c42900d6](https://github.com/kubeovn/kube-ovn/commit/c42900d6350ad90b10c7f091f67d9e3491477ce1) prepare for release 1.8.9
 * [ff928386](https://github.com/kubeovn/kube-ovn/commit/ff928386e1a4a6ea9f3b9d497c94335c1c241849) [PATCH] Delete pod if subnet of the pod's owner(sts/vm) updated (#1678)
 * [f216a2f5](https://github.com/kubeovn/kube-ovn/commit/f216a2f57854d002b759dacbfecfb242fe89b760) security: disable pprof by default (#1672)
 * [a984c913](https://github.com/kubeovn/kube-ovn/commit/a984c913d265597b1c9ca249a91833f0f7eb28dd) update ovs health check, delete connection to ovn sb db (#1588)

### Contributors

 * Mengxin Liu
 * Wang Bo
 * hzma

## v1.8.8 (2022-06-28)

 * [0fbefff5](https://github.com/kubeovn/kube-ovn/commit/0fbefff55b77bc991f97d216b301044af27a01b8) set release 1.8.8
 * [37df8e76](https://github.com/kubeovn/kube-ovn/commit/37df8e76ada0731a7693d0cdd611736f9ab8aa72) prepare for release 1.8.8
 * [bf873330](https://github.com/kubeovn/kube-ovn/commit/bf8733308c0b64fe1444e4d253463896e437864c) CI: delete resources in order to avoid a long time waiting for subnet deletions. (#1643)
 * [de117356](https://github.com/kubeovn/kube-ovn/commit/de117356f0a6e0054b563005e994f11de7ae4a30) add ovn-ic HA deploy
 * [1dcf9a43](https://github.com/kubeovn/kube-ovn/commit/1dcf9a43223c595fe4af8ca22f3c3826de656394) set networkpolicy log default to false

### Contributors

 * hzma
 * lut777
 * 张祖建

## v1.8.7 (2022-06-19)

 * [46987551](https://github.com/kubeovn/kube-ovn/commit/46987551520b9aa5014c1dccfc7c3a6621b96f2a) prepare for release 1.8.7
 * [b6796d09](https://github.com/kubeovn/kube-ovn/commit/b6796d09c46b3dd299c772a1f0c96a49bde16889) cni handler: do not wait routed annotation for net1 (#1586)
 * [f5c3ed3f](https://github.com/kubeovn/kube-ovn/commit/f5c3ed3f71e853df23a16b5c2e0fb049cf2d55c1) fix adding static route after LSP deletion (#1571)
 * [f7ee860b](https://github.com/kubeovn/kube-ovn/commit/f7ee860b2ed0119d3a4b6582b87a8da2942e2918) fix duplicate netns parameter (#1580)
 * [0a3468b1](https://github.com/kubeovn/kube-ovn/commit/0a3468b144fde5828cbbb9e49eed42bd1cdf06a1) do not gc vm pod lsp when vm still exists (#1558)
 * [d453add3](https://github.com/kubeovn/kube-ovn/commit/d453add371feef760c852e8c7d6023136e967d4e) fix exec cmd in vpc nat gateway (#1556)
 * [8303ace0](https://github.com/kubeovn/kube-ovn/commit/8303ace0b44f8164f16d5f6f9708ae5d067117d5) CNI: do not return route if nic is not eth0 (#1555)
 * [bc758245](https://github.com/kubeovn/kube-ovn/commit/bc75824545aace5e0360e98f1440878ddb681ec7) exit kube-ovn-controller on stopped leading (#1536)
 * [c51b09e8](https://github.com/kubeovn/kube-ovn/commit/c51b09e8f5f17fc2241f8624893140aab0492990) remove name for default drop acl in networkpolicy (#1522)
 * [9fe8cfcd](https://github.com/kubeovn/kube-ovn/commit/9fe8cfcd08578a39df5a3ef9a7ed8aaf5b8657e1) move dumb-init from base images to kube-ovn image
 * [2a8a45a1](https://github.com/kubeovn/kube-ovn/commit/2a8a45a16dbc8fe4ba6bce49f075bc875a0152bc) fix defunct ovn-nbctl daemon

### Contributors

 * hzma
 * zhangzujian
 * 张祖建

## v1.8.6 (2022-05-13)

 * [56bf06df](https://github.com/kubeovn/kube-ovn/commit/56bf06df9b7159958b1c439518b7ab666083eea6) release 1.8.6
 * [9e5b2b28](https://github.com/kubeovn/kube-ovn/commit/9e5b2b288713d49f2f96b8f7add9e56ec3f6e033) reduce ovs-ovn restart downtime (#1516)
 * [e4d6cc2f](https://github.com/kubeovn/kube-ovn/commit/e4d6cc2f3b3b579ad64ce72c2deef1056393c038) prepare for release 1.8.6
 * [60aa8913](https://github.com/kubeovn/kube-ovn/commit/60aa89139154b30a27f6912e8dee65849853cca7) fix: ovs trace flow always ends with controller action (#1508)
 * [2a074c6f](https://github.com/kubeovn/kube-ovn/commit/2a074c6f6529f488723cd4fac407e9739f39a0ee) optimize IPAM initialization

### Contributors

 * Mengxin Liu
 * zhangzujian

## v1.8.5 (2022-04-27)

 * [9b96bacf](https://github.com/kubeovn/kube-ovn/commit/9b96bacf49aae35dc6d7bfc6f42ee6d8adceac81) ci: skip some checks
 * [e20cf4a2](https://github.com/kubeovn/kube-ovn/commit/e20cf4a2207a388e08c7cd5b503ee934331fbe96) delete ipam record and static route when gc lsp (#1490)
 * [035f5072](https://github.com/kubeovn/kube-ovn/commit/035f5072c9219be7e8d989fec6eee338150b6321) CVE-2022-27191 (#1479)
 * [e898c96e](https://github.com/kubeovn/kube-ovn/commit/e898c96e667b13d700e55af67057f503ed3ff138) add delete ovs pods after restore nb db (#1474)
 * [89d7471c](https://github.com/kubeovn/kube-ovn/commit/89d7471c77f722d3e28681f6a251fcf40403957b) delete monitor noexecute toleration (#1473)
 * [4b012aa6](https://github.com/kubeovn/kube-ovn/commit/4b012aa6d53b44fa08a59ec2fc73774fb70a27d1) add env-check (#1464)
 * [3d0448b4](https://github.com/kubeovn/kube-ovn/commit/3d0448b4b9469548ebe43f0da0d1fb8677ef66de) append metrics (#1465)
 * [a0e2404c](https://github.com/kubeovn/kube-ovn/commit/a0e2404c9cc634c6579e8c88ded1a2055953900b) add kube-ovn-controller switch for EIP and SNAT
 * [ca2ca1a1](https://github.com/kubeovn/kube-ovn/commit/ca2ca1a1614133b31c190cda102eabd493e64461) add routed check in circulation (#1446)
 * [c9dfa5bb](https://github.com/kubeovn/kube-ovn/commit/c9dfa5bbbcdf6ab0a8245dccc8be2554322b1d0a) modify init ipam by ip crd only for sts pod (#1448)
 * [8b5ce74a](https://github.com/kubeovn/kube-ovn/commit/8b5ce74ad37720b6f0552573e8cdadace791b708) ignore cni cve
 * [22fe8fbe](https://github.com/kubeovn/kube-ovn/commit/22fe8fbe6f6f8cf30b9e5456ab8e6f0cda366d14) log: show the reason if get gw node failed (#1443)
 * [8570e286](https://github.com/kubeovn/kube-ovn/commit/8570e286173f77117e4a84a6d9345280f8e82b4d) update alpine to fix CVE-2022-1271
 * [6aa6b0a9](https://github.com/kubeovn/kube-ovn/commit/6aa6b0a92b4209fe58147df15b03768de798e4e3) fix adding key to delete Pod queue
 * [bf12ea0e](https://github.com/kubeovn/kube-ovn/commit/bf12ea0e0480555e17376890b128d30e16f109d4) fix IPAM initialization
 * [5e005884](https://github.com/kubeovn/kube-ovn/commit/5e0058846712b046a6b8442490223d6588e8b3ab) ignore all link local unicast addresses/routes
 * [63248040](https://github.com/kubeovn/kube-ovn/commit/6324804011e068eeeb9143c53a24b9219efce3d2) fix error handling for netlink.AddrDel
 * [aa7c3b8d](https://github.com/kubeovn/kube-ovn/commit/aa7c3b8def0e6363bee4322c1103bc5493b212f0) replace pod name when create ip crd
 * [f0bb2769](https://github.com/kubeovn/kube-ovn/commit/f0bb2769bd493b327fd9c905502c26a307c2f235) support alloc static ip from any subnet after ns supports multi subnets
 * [7a67a213](https://github.com/kubeovn/kube-ovn/commit/7a67a213d8aea8eb6d90873ec8882fcce292cfed) fix provider-networks status
 * [8529bf8b](https://github.com/kubeovn/kube-ovn/commit/8529bf8b79565b3c268ace4a84601dd6b5940d40) recover ips CR on IPAM initialization

### Contributors

 * Mengxin Liu
 * hzma
 * zhangzujian

## v1.8.4 (2022-03-29)

 * [48eb70a4](https://github.com/kubeovn/kube-ovn/commit/48eb70a4d90f9e6334c3df23919b0afe5b20311b) release update 1.8.4 changelog (#1414)
 * [2fe7fff2](https://github.com/kubeovn/kube-ovn/commit/2fe7fff2a8c5fbe23df621c950299acbe14cd53b) create ip crd in kube-ovn-controller (#1412)
 * [01163c1c](https://github.com/kubeovn/kube-ovn/commit/01163c1c2e331f63c5bf5c38bd1cf542c1a363a8) fix: add condition for triggering the deletion of redundant chassises in sbdb (#1411)
 * [c262bdcf](https://github.com/kubeovn/kube-ovn/commit/c262bdcf0abbcf3528a964f6f4507bbf5f23a979) fix: do not recreate port for terminating pods (#1409)
 * [bf167a60](https://github.com/kubeovn/kube-ovn/commit/bf167a60dc152490aa5b74adedee102799ecd44e) avoid frequent ipset update
 * [b44bbc5d](https://github.com/kubeovn/kube-ovn/commit/b44bbc5d0325c1e70cd7c3d13c56369a71d79f77) fix: The underlay physical gateway config by external-gw-addr when use snat&eip (#1400)
 * [ffdd1967](https://github.com/kubeovn/kube-ovn/commit/ffdd196723f90c2441bb5ab6b406da36e7722018) add reset for kube-ovn-monitor metrics (#1403)
 * [eda71b3c](https://github.com/kubeovn/kube-ovn/commit/eda71b3c54ee8419950a80c43e46bba140c65e21) check the cidr format whether is correct (#1396)
 * [62695032](https://github.com/kubeovn/kube-ovn/commit/626950326ce9f842bb04f93e31914cfbe52c366e) update dockerfile to use v1.8.3 base img
 * [c15afc54](https://github.com/kubeovn/kube-ovn/commit/c15afc542fe50fc72739fa951345a193b6c9d105) append vm deletion check
 * [9faf2a10](https://github.com/kubeovn/kube-ovn/commit/9faf2a101ad87363954a6a847b2b3d93776f4237) update nodeips for restore cmd in ko plugin
 * [621a37f0](https://github.com/kubeovn/kube-ovn/commit/621a37f08754493503025481b7a92731239c76b6) fix external egress gateway
 * [27af3335](https://github.com/kubeovn/kube-ovn/commit/27af3335a6f1b3cb562467c9b3fdc32bd04adb8a) update ip assigned check
 * [4d88bea5](https://github.com/kubeovn/kube-ovn/commit/4d88bea538c5953dec1651d605d998129f2f8c4c) add missing link scope routes in vpc-nat-gateway
 * [bf8026ed](https://github.com/kubeovn/kube-ovn/commit/bf8026ed6482e928d3effb77781480e4c8a7d3a0) increase memory limit of ovn-central
 * [5a52041b](https://github.com/kubeovn/kube-ovn/commit/5a52041b6bc45429171c2c515b9178f0bccfa919) fix range loop

### Contributors

 * hzma
 * lut777
 * wangyd1988
 * xujunjie-cover
 * zhangzujian

## v1.8.3 (2022-03-09)

 * [37937fcf](https://github.com/kubeovn/kube-ovn/commit/37937fcf13e8c646b863696770c119efcba6df7c) release update 1.8.3 changelog (#1360)
 * [014ecc87](https://github.com/kubeovn/kube-ovn/commit/014ecc871f093d3adcf9602fe9629c8925d47f2d) add restore process for ovn nb db
 * [dbf4774d](https://github.com/kubeovn/kube-ovn/commit/dbf4774d6580b5cc4a94fef90006317bb10344f9) optimize kube-ovn-monitor yaml
 * [ce8087d7](https://github.com/kubeovn/kube-ovn/commit/ce8087d75a90399e125c11a762c4e59350494faa) add reset porocess for ovs interface metrics
 * [62938245](https://github.com/kubeovn/kube-ovn/commit/62938245fb3c082575ac02815429901a9db08a45) deepcopy fix steps
 * [118f1299](https://github.com/kubeovn/kube-ovn/commit/118f129910a85e74c084f9f2f8cefb3d79d23bca) fix SNAT/PR on Pod startup
 * [9fa2c792](https://github.com/kubeovn/kube-ovn/commit/9fa2c792ec28ab428befc8aef8fbde2d91a0f369) add check for pod update process
 * [f053f2a2](https://github.com/kubeovn/kube-ovn/commit/f053f2a25f7743eeb10e30ee18ab2aeb75ed037f) fix ips update
 * [fe9532d4](https://github.com/kubeovn/kube-ovn/commit/fe9532d4e66e3625b56a08aec6232d4f21106184) fix cni deepcopy
 * [c76e9b01](https://github.com/kubeovn/kube-ovn/commit/c76e9b01286eb51362dff4342435d4b2fe49330c) fix: replace ecmp dp_hash with hash by src_ip (#1289)
 * [f3922ba9](https://github.com/kubeovn/kube-ovn/commit/f3922ba9c90496ba62ab5a9715d204804848e260) keep ip for kubevirt pod
 * [f6628902](https://github.com/kubeovn/kube-ovn/commit/f66289024e0397e1163e57cc3aac39ef0b956aa9) fix OVS bridge with bond port in mode 6
 * [a421d9f8](https://github.com/kubeovn/kube-ovn/commit/a421d9f8658c95da27975bb2679eaad00dc2fe97) fix: continue of deletion for del pod failed when can't found vpc or subnet (#1335)
 * [cf7f4bd9](https://github.com/kubeovn/kube-ovn/commit/cf7f4bd9f267b4f2550db3b78563f6ab8665ed12) Fix usage of ovn commands
 * [586a0764](https://github.com/kubeovn/kube-ovn/commit/586a0764bd398da9b02da4994ab22364d2f75ca2) ignore cilint
 * [e083a2ba](https://github.com/kubeovn/kube-ovn/commit/e083a2ba061f5ed57b797a5138c4d668da9081b3) resync provider network status periodically
 * [dcb3e82d](https://github.com/kubeovn/kube-ovn/commit/dcb3e82dd96af722df20575a6df06ef2abb6f2f8) Revert "resync provider network status periodically"
 * [18740e5c](https://github.com/kubeovn/kube-ovn/commit/18740e5c9e0fb3214810265afe308b0359ab6f89) fix statefulset Pod deletion
 * [85c15cb4](https://github.com/kubeovn/kube-ovn/commit/85c15cb4efad3c577e2722fc26b254c5c4e4df52) resync provider network status periodically
 * [172c1733](https://github.com/kubeovn/kube-ovn/commit/172c173390ff921240e9f8bee1e654cbd1c4c37a) feat: optimize log
 * [136aedf9](https://github.com/kubeovn/kube-ovn/commit/136aedf9961fa5a513ad1fa91ea3ad3cbd2c5c1c) optimize log for node port-group
 * [0869e621](https://github.com/kubeovn/kube-ovn/commit/0869e621a4ee76d022e96a4bbb61933bc99273b5) append add cidr and excludeIps annotation for namespace
 * [e04eaf7a](https://github.com/kubeovn/kube-ovn/commit/e04eaf7a5d85935c3b41658985f96387c5eb383f) support to add multiple subnets for a namespace
 * [ae201ef5](https://github.com/kubeovn/kube-ovn/commit/ae201ef51dacd165f283b1537ee58a88bdddc3a8) feat: update provider network via node annotation
 * [5cf005e2](https://github.com/kubeovn/kube-ovn/commit/5cf005e249318ba9bf85488c923566ebe3e8d06c) fix: only log matched svc with np (#1287)
 * [6ef52c22](https://github.com/kubeovn/kube-ovn/commit/6ef52c22e346f3d2f810d964ed026916cb518285) transfer IP/route earlier in OVS startup
 * [75157be8](https://github.com/kubeovn/kube-ovn/commit/75157be80e8383532c49c98650ea58cccc21b76f) add metric for ovn nb/sb db status
 * [4b23c84c](https://github.com/kubeovn/kube-ovn/commit/4b23c84c2745039954a6ce40f330357f6efa5dac) check static route conflict
 * [0832f5ef](https://github.com/kubeovn/kube-ovn/commit/0832f5efa6e7c601a695f85621bd1ace664c6604) set up tunnel correctly in hybrid mode
 * [175d54d1](https://github.com/kubeovn/kube-ovn/commit/175d54d1897109e82ccef29b1b7e4ad1280b891f) fix clusterrole in ovn-ha.yaml
 * [457475f2](https://github.com/kubeovn/kube-ovn/commit/457475f2da8485e643e1a25607f297e59ae1d795) add gateway check after update subnet
 * [45787fb7](https://github.com/kubeovn/kube-ovn/commit/45787fb743a55e8e34fb109ddc77d3904b026f29) add back centralized subnet active-standby mode
 * [a737e196](https://github.com/kubeovn/kube-ovn/commit/a737e19662d4a5efc7e528633609aecb84806998) update networkpolicy port process
 * [ff6bf6fa](https://github.com/kubeovn/kube-ovn/commit/ff6bf6fa6dd901459e831a882b0035bc88dbae8a) update check for delete statefulset pod

### Contributors

 * chestack
 * hzma
 * lut777
 * xujunjie-cover
 * zhangzujian

## v1.8.2 (2022-01-05)

 * [5acf9586](https://github.com/kubeovn/kube-ovn/commit/5acf958622bb896a21951ebb6d6eded7bca98d16) release: update 1.8.2 changelog
 * [49b2ae40](https://github.com/kubeovn/kube-ovn/commit/49b2ae40c88f293cc09de6796b8b920358f4e4f9) add log for ecmp route
 * [798d0bb9](https://github.com/kubeovn/kube-ovn/commit/798d0bb97757726077d8a8ff6454aae4ee751e71) fix pod tolerations
 * [c5f4c8e6](https://github.com/kubeovn/kube-ovn/commit/c5f4c8e61920db9a03842b0b535d0c14fb47ee98) fix installation script
 * [270d28e4](https://github.com/kubeovn/kube-ovn/commit/270d28e47c7acd8b258ff27e31700fb851f64feb) append check for centralized subnet nat process
 * [ee691fb5](https://github.com/kubeovn/kube-ovn/commit/ee691fb5118be4f300e14b77e94b2cbb74b80df9) change nbctl args 'wait=sb' to 'no-wait'
 * [c4956ac3](https://github.com/kubeovn/kube-ovn/commit/c4956ac3ea9d606d40d651fd58b69e521760045a) move chassis judge to the end of node processing
 * [636b946a](https://github.com/kubeovn/kube-ovn/commit/636b946af6fe08d0dc9d042f1a6701734a8c0c45) use different ip crd with provider suffix for pod multus nic
 * [a03a858c](https://github.com/kubeovn/kube-ovn/commit/a03a858c167fc55f0b0683cbb90f2da17b36e505) use multus-cni as default cni to assign ip
 * [3205b88e](https://github.com/kubeovn/kube-ovn/commit/3205b88eaf94238c6819760acd1e57b5b96d70f9) fix: do not reuse released ip after subnet updated
 * [7de6afb8](https://github.com/kubeovn/kube-ovn/commit/7de6afb828cf3456c10fdc72cb47526a60dc23bf) delete frequently log
 * [efefc20b](https://github.com/kubeovn/kube-ovn/commit/efefc20b125310bf9362250b1b7aea2b9ea51fea) pinger: fix getting empty PodIPs
 * [d98fab8d](https://github.com/kubeovn/kube-ovn/commit/d98fab8d9b4c9ccc45b282536ae9376ae949a665) add protocol check when subnet is dual-stack
 * [0a48f6a6](https://github.com/kubeovn/kube-ovn/commit/0a48f6a6a38b164a37022ecd921a4abe9b1f1350) filter used qos when delete qos
 * [26f239aa](https://github.com/kubeovn/kube-ovn/commit/26f239aa01cd79a8a681a0e8f730a4033659db96) fix: check np switch
 * [4187a329](https://github.com/kubeovn/kube-ovn/commit/4187a329bde0884ef6586006fe5919c20a6288c2) When netpol is added to a workload, the workload's POD can be accessed using service
 * [e7c50077](https://github.com/kubeovn/kube-ovn/commit/e7c50077549a5f9858ed4ebe8cf618592a39c282) when update subnet's execpt ip,we should filter repeat ip
 * [86020295](https://github.com/kubeovn/kube-ovn/commit/86020295969e90350ec2364232a4ce7a65ecf54c) fix: add back the leader check
 * [dfa1a3a8](https://github.com/kubeovn/kube-ovn/commit/dfa1a3a8ea4a9ac600304ab211438eabf7c97fb7) security: upadate base image
 * [7f1e9354](https://github.com/kubeovn/kube-ovn/commit/7f1e9354d414ef95324a232171f2e61ddc4af654) update delete operation for statefulset pod
 * [17301ee2](https://github.com/kubeovn/kube-ovn/commit/17301ee2fe3a5aa433aee4a37782c39bee3fdd3b) chore: update klog to v2 which embed log rotation
 * [7cfeee1e](https://github.com/kubeovn/kube-ovn/commit/7cfeee1e296547ebeb40e54dae42cab8a45e3a49) fix: add kube-ovn-cni prob timeout
 * [88a92ac9](https://github.com/kubeovn/kube-ovn/commit/88a92ac95357112b5f11a5f02e63875588f7629c) append add db compact for nb and sb db
 * [9496e386](https://github.com/kubeovn/kube-ovn/commit/9496e38634716ef45174b06661ecdcc7e33b28c5) add vendor param for fix list LR
 * [641dcdde](https://github.com/kubeovn/kube-ovn/commit/641dcdde2ec0ac092e1c5cc8df0d988ca4d1d360) deleting all chassises which are not nodes
 * [ad0bc1b7](https://github.com/kubeovn/kube-ovn/commit/ad0bc1b775e7e0840e0a42b3b7d82941d6a1d900) add db compact for nb and sb db
 * [b50da0e1](https://github.com/kubeovn/kube-ovn/commit/b50da0e1921e136dfd942013efef7bfa4cc72eaf) fix pinger's compatibility for k8s v1.16
 * [723ec5c3](https://github.com/kubeovn/kube-ovn/commit/723ec5c3b26449e5a642424f5fcc811e17b32c8c) fix LB: skip service without cluster IP
 * [d412c780](https://github.com/kubeovn/kube-ovn/commit/d412c780510a770cfc7862ffd060caad4597d53b) security: update base ubuntu image
 * [b96b7056](https://github.com/kubeovn/kube-ovn/commit/b96b7056b31cce1a4c3ba8bd0f2fa521e3d35a55) add pod in default vpc to node port-group
 * [e1dfa7b1](https://github.com/kubeovn/kube-ovn/commit/e1dfa7b19de891990607f762447818b3bcafb7ba) add sg acl check when init
 * [c8692dfb](https://github.com/kubeovn/kube-ovn/commit/c8692dfb0cb2ef7e9500ea7ff92f24f06ba019bf) fix: no need to set address for ls to lr port
 * [ef0e3b95](https://github.com/kubeovn/kube-ovn/commit/ef0e3b95a2a796cc7e1108f8abed656add9ea9de) fix ko trace
 * [7231a6f2](https://github.com/kubeovn/kube-ovn/commit/7231a6f2015c2449a2d9969c117e19df765ca675) fix read-only pointer in vlan and provider-network
 * [01e30a42](https://github.com/kubeovn/kube-ovn/commit/01e30a42b19cad6ea555bb293adf1526c5f724f8) fix read-only pointer in vlan and provider-network
 * [72cf31dd](https://github.com/kubeovn/kube-ovn/commit/72cf31dd4fda2c9cc0f1cd10c445a2364d97c597) fix: trace in custom vpc
 * [03639a4a](https://github.com/kubeovn/kube-ovn/commit/03639a4a83db3962fe11415a8ff1464faccc45ec) fix: multus-cni subnet allocation
 * [1857130e](https://github.com/kubeovn/kube-ovn/commit/1857130e2fafe3b9833e36fd1f3098f3c0e519ea) fix LB in dual stack cluster
 * [3773bedf](https://github.com/kubeovn/kube-ovn/commit/3773bedf1c15ca0a27e63d95fd919b025b7640d6) prepare for release 1.8.2
 * [45316125](https://github.com/kubeovn/kube-ovn/commit/45316125c746653935945b2a782dc1bd246dfaa7) fix: check allocated annotation in update handler
 * [79be0cde](https://github.com/kubeovn/kube-ovn/commit/79be0cde96ffb64db06ba37cf5d8e9b4ef01ad5a) fix bug: logical switch ts not ready
 * [e3581cf1](https://github.com/kubeovn/kube-ovn/commit/e3581cf1483d444492fcc2974da74e8a8df47e49) fix: ensure all kube-ovn components deleted before annotate pods
 * [9847a1b6](https://github.com/kubeovn/kube-ovn/commit/9847a1b67f753a07853e257dcede7118a3377c2b) Revert "add check switch for default subnet's gateway"
 * [c106afa6](https://github.com/kubeovn/kube-ovn/commit/c106afa635a3742df2a01dfca18ff5fb83e1f96f) add check switch for default subnet's gateway
 * [bdf5b0e2](https://github.com/kubeovn/kube-ovn/commit/bdf5b0e29d312a9ff42ef52c4dadc77b9bd1cffd) remove node chassis annotation on cleanup
 * [31a5da22](https://github.com/kubeovn/kube-ovn/commit/31a5da222e8ddf3b47b3da8affd468dd9d4d6085) fix: delete vpc-nat-gw deployment
 * [765ede7b](https://github.com/kubeovn/kube-ovn/commit/765ede7bb9feb3f0861910e7acba6002663b63ac) fix: serialize pod add/delete order
 * [78dc1fbf](https://github.com/kubeovn/kube-ovn/commit/78dc1fbf43b45dcebf7dd7bbcde3a6dff348e662) change inspection logic from manually adding lsp to just readding pod queue
 * [986f8b4e](https://github.com/kubeovn/kube-ovn/commit/986f8b4e4f74d954285d862b31ec3de32163db34) add inspection
 * [15ea6ab8](https://github.com/kubeovn/kube-ovn/commit/15ea6ab88217f14ccb2faf516c9982c095386479) fix: check and load ip_tables module
 * [9bb0cfc2](https://github.com/kubeovn/kube-ovn/commit/9bb0cfc242ee2f8887cdab6dcb2615f47da1098e) fix cleanup.sh and uninstall.sh
 * [da422ff9](https://github.com/kubeovn/kube-ovn/commit/da422ff9fb9cd5767a21e59ae9d48287c15d0e44) fix kubectl-ko diagnose
 * [cc8a4da0](https://github.com/kubeovn/kube-ovn/commit/cc8a4da05fda180c00be109f0d396cb4070e6384) fix pinger in dual stack cluster
 * [9364d2a2](https://github.com/kubeovn/kube-ovn/commit/9364d2a2d8392f5fd4710a43f50308093a500bcd) add e2e testing for dual stack underlay
 * [ecf4e011](https://github.com/kubeovn/kube-ovn/commit/ecf4e011c8b8fc446bf32f306fcfbaae1717b542) fix pinger and monitor in underlay networking
 * [91a32d41](https://github.com/kubeovn/kube-ovn/commit/91a32d416c091b710e5d5c5c1cc4cf76ec41145b) fix kubectl plugin ko
 * [259f8d6a](https://github.com/kubeovn/kube-ovn/commit/259f8d6a0834595e2d919b77b91841f1387e6a67) replace api for get lsp id by name
 * [7e775fa6](https://github.com/kubeovn/kube-ovn/commit/7e775fa6a817a73dcfde1f0484eaa39a0dc5992e) In netpol egress rules, except rule should be set to "!=" and should not be "=="
 * [0a09e055](https://github.com/kubeovn/kube-ovn/commit/0a09e0557eac09439d6d3fb531203f47a15eb628) modify kube-ovn as multus-cni problem

### Contributors

 * Mengxin Liu
 * hzma
 * lut777
 * wang_yudong
 * zhangzujian
 * 范日明

## v1.8.1 (2021-10-09)

 * [31f53094](https://github.com/kubeovn/kube-ovn/commit/31f53094f5b85101e19ba2bcba5dc02491759a22) release: prepare for 1.8.1
 * [fa66c5f8](https://github.com/kubeovn/kube-ovn/commit/fa66c5f87df3df62b7c6c199484115b8149f2b76) fix: init node with wrong ipamkey and lead conflict
 * [fa17c3d6](https://github.com/kubeovn/kube-ovn/commit/fa17c3d6325150496addc2cc725483e8c0e6d817) fix installation scripts
 * [c7d050b9](https://github.com/kubeovn/kube-ovn/commit/c7d050b99d63b6469f26acb6c34c28d59c456605) fix getting LSP UUID by name
 * [f0bebbec](https://github.com/kubeovn/kube-ovn/commit/f0bebbec809dbdadaaa2e4f8972bd431f3a91f08) fix StatefulSet down scale
 * [4c189b7f](https://github.com/kubeovn/kube-ovn/commit/4c189b7f8d268f4fd4d9d36c3180924d37eddac7) refactor: mute ovn0 ping log and add ping details
 * [c208cd51](https://github.com/kubeovn/kube-ovn/commit/c208cd5181ef46e3392cba8aa113e4bf9af01736) fix: wrong link for iptables
 * [b4faf60b](https://github.com/kubeovn/kube-ovn/commit/b4faf60b78a55fe98722de36a7642f6742db61d7) fix IPAM for StatefulSet
 * [d0525957](https://github.com/kubeovn/kube-ovn/commit/d05259579e9e929e8eef80d7b7bc97cee124d45b) append externalIds for pod and node when upgrade
 * [34ba16ea](https://github.com/kubeovn/kube-ovn/commit/34ba16eac715f46d2676b53de25feef118a1f3d3) perf: increase ovn-nb timeout
 * [f844a2bc](https://github.com/kubeovn/kube-ovn/commit/f844a2bc30a3b2aeafbcbfaee3e153493948ce1a) fix: re-check ns annotation to avoid annotations lost
 * [f7214195](https://github.com/kubeovn/kube-ovn/commit/f72141953363e19a12f53f902770b90566c46c1d) perf: do not diagnose external access
 * [6232c73b](https://github.com/kubeovn/kube-ovn/commit/6232c73bbddc761c526c31033137e46053306b09) reactor: remove ovn ipam options
 * [651ab41e](https://github.com/kubeovn/kube-ovn/commit/651ab41ed587454be444fc2d51497fec120c120d) perf: switch's router port's addresses to "router"
 * [f5997a87](https://github.com/kubeovn/kube-ovn/commit/f5997a875f805e022b28619d861be7b458accc97) fix gc lsp statistic for multiple subnet
 * [da43e21b](https://github.com/kubeovn/kube-ovn/commit/da43e21b198b2a9cf013786be95b9b706ecf73e7) fix e2e testing
 * [5e3c1507](https://github.com/kubeovn/kube-ovn/commit/5e3c1507371f7117fe5441070ce19e3a2062aec8) fix variable referrence
 * [bc95b5d3](https://github.com/kubeovn/kube-ovn/commit/bc95b5d3a0c048bcfb500a93fec8ed9e88bd7a2c) fix nat-outgoing/policy-routing on pod startup

### Contributors

 * Mengxin Liu
 * hzma
 * lut777
 * zhangzujian

## v1.8.0 (2021-09-08)

 * [7c5fed65](https://github.com/kubeovn/kube-ovn/commit/7c5fed6547c8695056de3f12a5ca7e0754b37d39) fix adding OVN routes in dual stack Kubernetes
 * [80a037ee](https://github.com/kubeovn/kube-ovn/commit/80a037ee557cf4c3ffe60845a52ae8f1f00196f8) release: prepare for 1.8
 * [f59bfb86](https://github.com/kubeovn/kube-ovn/commit/f59bfb864a261e8f0174c8940850389d79ae02a1) add update process and adding label to ls/lsp/lr
 * [e09d99b3](https://github.com/kubeovn/kube-ovn/commit/e09d99b3b4e8e46f7af795fafeaa725bf8bb0d0b) fix: VLAN CIDR conflict check
 * [e6b8341e](https://github.com/kubeovn/kube-ovn/commit/e6b8341e6b9cf337104aff383ef27279faf51bc7) security: update base image
 * [29422965](https://github.com/kubeovn/kube-ovn/commit/29422965e5cf0b9a62143c66205f3f5bf059f7e8) update provider network CRD
 * [25b151c8](https://github.com/kubeovn/kube-ovn/commit/25b151c8587eb63778e0f87664ceb54801797687) fix external-vpc
 * [44a8b4f6](https://github.com/kubeovn/kube-ovn/commit/44a8b4f6912b5869c9eb19ec92d3b340fbb57821) perf: use link alias to filter packet
 * [e9984fe0](https://github.com/kubeovn/kube-ovn/commit/e9984fe0c047f34d0be196b54062a6be2c450504) security: fix CVE-2021-3538
 * [d41c5e9b](https://github.com/kubeovn/kube-ovn/commit/d41c5e9b840e740db398564d5c2d0b303457804d) add print columns for subnet/vpc/vpc-nat-gw crd
 * [730e4f17](https://github.com/kubeovn/kube-ovn/commit/730e4f17165d853055557e3a9b442655aa21fed5) improve support for dual-stack
 * [c148a5ac](https://github.com/kubeovn/kube-ovn/commit/c148a5ac861f27247847df0114956a510a1162e6) initialize ipsets on cni server startup
 * [10613e87](https://github.com/kubeovn/kube-ovn/commit/10613e8727b33e60ba15f2f57869c4884a029c2a) delete residual ovs internal ports
 * [361d4bbe](https://github.com/kubeovn/kube-ovn/commit/361d4bbe026784ea4314ee262c12745ad7e6e982) simplify vlan implement
 * [6fde0a56](https://github.com/kubeovn/kube-ovn/commit/6fde0a56686d543eabc6ba5d5368b5ba38adc768) fix: ovn-northd svc flip flop
 * [b1106056](https://github.com/kubeovn/kube-ovn/commit/b110605650819ee4aadce6c21b22c0cddf852f24) add container run command for runtime containerd
 * [42e212ca](https://github.com/kubeovn/kube-ovn/commit/42e212ca24467174c56e71e9ca497dba6843371c) fix subnet conflict check for node address
 * [3d2c6eb9](https://github.com/kubeovn/kube-ovn/commit/3d2c6eb96e191e3f8346cf97fe086e241bf2f405) feat: read interface in installation from environment
 * [35acf424](https://github.com/kubeovn/kube-ovn/commit/35acf424ee659eee1ee0872afa36bc5925974988) update encap ip by node annotation periodic
 * [13b2080a](https://github.com/kubeovn/kube-ovn/commit/13b2080a03c922e766d11ef8a8739296c0689e5b) fix ipset on pod creation/deletion
 * [f415b1ba](https://github.com/kubeovn/kube-ovn/commit/f415b1ba54836e1750667f40c1c86f529682f6b2) add ready status for provider network
 * [09283849](https://github.com/kubeovn/kube-ovn/commit/0928384955266226aef1119fcd8e90ebf4f34ccb) avoid Pod IP to be the same with node internal IP
 * [70fbbecc](https://github.com/kubeovn/kube-ovn/commit/70fbbecc43aa9db9d015eef4a8fbacd60d47de68) remove subnet's `spec.underlayGateway` field
 * [96b0c118](https://github.com/kubeovn/kube-ovn/commit/96b0c11859661bd6c377ae22add2e163aebe3d1d) add support for custom routes
 * [45aafca2](https://github.com/kubeovn/kube-ovn/commit/45aafca2104a94ab56b3ed03a3e8df67abc462bc) Add missing metadata directive in VpcNatGateway example
 * [0380d64c](https://github.com/kubeovn/kube-ovn/commit/0380d64cea659085aabb1b77dbcc17e3a5a18ac6) use util.hostNameEnv instead KUBE_NODE_NAME
 * [38e04f34](https://github.com/kubeovn/kube-ovn/commit/38e04f34aaadfbef889fae2f31e5df280e5429b8) chore: change wechat image
 * [5df9fdd4](https://github.com/kubeovn/kube-ovn/commit/5df9fdd4c7594af4e02c5100e68e74c95eb94c35) fix typo
 * [4a7dd734](https://github.com/kubeovn/kube-ovn/commit/4a7dd7340dc9e4ae024c4c670c7382d8c2e269ce) perf: add fastpath and tuning guide
 * [3d8cdb6c](https://github.com/kubeovn/kube-ovn/commit/3d8cdb6cb0177b5212b235bf850fe706fe61eb70) update node labels and provider network's status.readyNodes when provider network is not initialized successfully in a node
 * [8596ddc9](https://github.com/kubeovn/kube-ovn/commit/8596ddc901a55a04474a941f06e1a67f6dd8e644) fix issues in underlay networking
 * [7724990d](https://github.com/kubeovn/kube-ovn/commit/7724990dda02f599a2b9bb153e42d034591e6059) add external vpc switch
 * [ffef618d](https://github.com/kubeovn/kube-ovn/commit/ffef618db5aed74330627e6336a5b7f5aeaca525) update versions in docs and yamls
 * [6e8d5c80](https://github.com/kubeovn/kube-ovn/commit/6e8d5c80a809f11f794841ba7881038030548079) update Go to version 1.16
 * [3deb5770](https://github.com/kubeovn/kube-ovn/commit/3deb57708fcf95f0b6a46dfc23c18fb970914341) fix IPv6-related issues
 * [2e4922d5](https://github.com/kubeovn/kube-ovn/commit/2e4922d560da774590694002cb95b8cfa6adad3b) ci: use stable version
 * [dcda11d6](https://github.com/kubeovn/kube-ovn/commit/dcda11d6e19d9508ecd4a23f8b41857a9fe81fc4) fix: bad udp checksum when access nodeport
 * [f12e5ee5](https://github.com/kubeovn/kube-ovn/commit/f12e5ee587e26c1474351b9f49e8889e870d1680) fix port-security, address parameters should be merged into one
 * [f03d4350](https://github.com/kubeovn/kube-ovn/commit/f03d435038bdc8f7eeb131c0bbd3628a4dcc2af6) docs: optimize description
 * [b5b5bdb8](https://github.com/kubeovn/kube-ovn/commit/b5b5bdb89e717670a3f5f10e2c758148a73bdefc) ensure provider nic is up
 * [b5bbed38](https://github.com/kubeovn/kube-ovn/commit/b5bbed38a3f175982f9ba25b14a57cd56e7ba611) fix uninstall.sh
 * [3ba5168c](https://github.com/kubeovn/kube-ovn/commit/3ba5168cec17e251fff40254df057aaa837fdd16) some optimizations
 * [9ae0b3c3](https://github.com/kubeovn/kube-ovn/commit/9ae0b3c351e60539fcdf89cfbff6628e50f76c0a) fix gofmt lint
 * [410d9329](https://github.com/kubeovn/kube-ovn/commit/410d932900eab3184bfddd017c197b189a11a391) fix multi-nic.md
 * [5e9e41ac](https://github.com/kubeovn/kube-ovn/commit/5e9e41ac5555e4521680221b5bb73dd81fc620cc) fix dual stack cluster created by kind
 * [386d6160](https://github.com/kubeovn/kube-ovn/commit/386d6160f2970ef0ea498680f96e881f32b4f0a1) remove external egress gateway from additionalPrinterColumns
 * [70ae50ef](https://github.com/kubeovn/kube-ovn/commit/70ae50efb3ef53458b11a519efdf962c1b04a396) fix default bind socket of cni server
 * [56025ede](https://github.com/kubeovn/kube-ovn/commit/56025edea4628b755b6b6116772be1bc1efa0492) if the string of ip is empty,program will die
 * [9492f63f](https://github.com/kubeovn/kube-ovn/commit/9492f63f6bd590153a17ea30f43509f8208a1e99) if the string of ip  is empty,program will die
 * [324dce2e](https://github.com/kubeovn/kube-ovn/commit/324dce2e3b38f71461fd9a9b372aacb1cd54ca55) fix underlay networking on node reboot
 * [f7077d58](https://github.com/kubeovn/kube-ovn/commit/f7077d58ee08a9864725a5b1962c6e9bb54a6033) add judge before use the index about cidrBlocks and ips
 * [f25b1ae2](https://github.com/kubeovn/kube-ovn/commit/f25b1ae2b544af576842d0bf538b0cf90a9e0d32) add validation check function
 * [bda102a7](https://github.com/kubeovn/kube-ovn/commit/bda102a772483b84903bc4ac7532472e88608a78) docs: add wechat qcode
 * [14ccbeb3](https://github.com/kubeovn/kube-ovn/commit/14ccbeb377578ea18ea69b5c467445f5f08f13e3) feat: security group
 * [992a09d3](https://github.com/kubeovn/kube-ovn/commit/992a09d353a4bdb012aea0a591248b8a523a8f2f) delete subnet AvailableIPs and UsingIPs para
 * [057ade92](https://github.com/kubeovn/kube-ovn/commit/057ade92edbdca16000598f65dd5afe6fc063c89) fix: panic when node has nil annotations
 * [59869daa](https://github.com/kubeovn/kube-ovn/commit/59869daaf1246621c9dad409e0549483bf5a4a35) append pod/exec resource for vpc nat gw
 * [3ed2fe26](https://github.com/kubeovn/kube-ovn/commit/3ed2fe26dfd913155335d2a97414426c8e72ed7e) update comment for SetInterfaceBandwidth
 * [e1caa594](https://github.com/kubeovn/kube-ovn/commit/e1caa5941e55dd14f5e30dee5446900fb5138766) update qos process
 * [80e5e2ba](https://github.com/kubeovn/kube-ovn/commit/80e5e2babbe01cd15863837bb8ee74087bad9c91) fix LoadBalancer in custom VPCs
 * [bb1146ee](https://github.com/kubeovn/kube-ovn/commit/bb1146eeb5887f8f3856e929626ef6b939b9e9bf) Support Pod annotations control port mirroring
 * [4c4b0900](https://github.com/kubeovn/kube-ovn/commit/4c4b09007b5d0ce1cbf8a361c30bcf5591a22dcf) fix docs
 * [a04d964d](https://github.com/kubeovn/kube-ovn/commit/a04d964dcf1ef99a4a8f3ed62449a39a5cecc6f4) externalOvnRouters is ok with 0
 * [9524c93f](https://github.com/kubeovn/kube-ovn/commit/9524c93f3b55a40518f8198a2d91ac369ebd49f4) delete attachment ips
 * [6dd6a51d](https://github.com/kubeovn/kube-ovn/commit/6dd6a51d7d53a9d9caa175108e330e8dff43f93e) fix external_ids:pod_netns
 * [cbe8ae68](https://github.com/kubeovn/kube-ovn/commit/cbe8ae689fe0b8118cabe0e2d4fe683602026d3d) add switch for network policy support
 * [dc56d238](https://github.com/kubeovn/kube-ovn/commit/dc56d23853ff6b0fc38a5f514b029387be0b5787) fix subnet e2e
 * [e3daee83](https://github.com/kubeovn/kube-ovn/commit/e3daee8306ab1d71957f01db5de8890e5d31194c) ignore empty strings when counting lbs
 * [81ce45c2](https://github.com/kubeovn/kube-ovn/commit/81ce45c2e89a31c064eba54eec46cc729129f25c) fix iptables
 * [e9ea6a0f](https://github.com/kubeovn/kube-ovn/commit/e9ea6a0f98220453fc3b36a581cc695445f7a503) fix issue #944
 * [1cb57358](https://github.com/kubeovn/kube-ovn/commit/1cb57358ac0dbd36f7fa9fc6eda71b86f621fd12) fix openstackonkubernetes doc bugs
 * [fcdb0106](https://github.com/kubeovn/kube-ovn/commit/fcdb0106b263afffba370987763d07f7486d3490) add switch for gateway connectivity check
 * [4dc4624f](https://github.com/kubeovn/kube-ovn/commit/4dc4624f9bcf75ca296fc9c84c03590163c31505) fix cleanup.sh
 * [4fb97407](https://github.com/kubeovn/kube-ovn/commit/4fb974071601402230e5d5c2a45491ec9fa8df4c) security: fix CVE-2021-33910
 * [41b6429c](https://github.com/kubeovn/kube-ovn/commit/41b6429c4334bdff5c2c85d17d9775484800958a) delete ecmp route when node is deleted
 * [5bd96ac7](https://github.com/kubeovn/kube-ovn/commit/5bd96ac718927a511f0111c3ceb20350f6a9effb) fix: if nftables not exists do no exit
 * [6c5efbc3](https://github.com/kubeovn/kube-ovn/commit/6c5efbc30e0ad3e7f38b46af6b455b0d8abcb118) update wechat contract method
 * [e449b8ea](https://github.com/kubeovn/kube-ovn/commit/e449b8eaf156c387c9619016ca9f107a1c1353c4) delete overlapped var subnet
 * [2427a4b3](https://github.com/kubeovn/kube-ovn/commit/2427a4b3a05471239f29bceb1b9e6f0444fa5788) add designative nat ip process for centralized subnet
 * [1595eac5](https://github.com/kubeovn/kube-ovn/commit/1595eac56740e388b718548204906cd7b5f9a4dc) fix ipsets
 * [7e24e7d6](https://github.com/kubeovn/kube-ovn/commit/7e24e7d6f78430fee4fd3951adf9bb006ee0f07b) update underlay e2e testing
 * [27c649a5](https://github.com/kubeovn/kube-ovn/commit/27c649a50a40d4c96ed73262bbaaabbc47bc6dcc) match chassis until timeout
 * [df76038a](https://github.com/kubeovn/kube-ovn/commit/df76038a0a0475a269a3829e14875a9e847a6e45) fix CRD provider-networks.kubeovn.io
 * [d1c7a2ee](https://github.com/kubeovn/kube-ovn/commit/d1c7a2ee3664ba1b63220e50cb42034a8408e687) fix: set vf mac
 * [949c28c2](https://github.com/kubeovn/kube-ovn/commit/949c28c25d31322e4f7b910c2395911a571c28a4) update qos ingress_policing_burst
 * [8a05bdc8](https://github.com/kubeovn/kube-ovn/commit/8a05bdc88d51c236b8a20040282a808b51ccada2) add field defaultNetworkType in configmap ovn-config
 * [1810dfc3](https://github.com/kubeovn/kube-ovn/commit/1810dfc32f22e42654fab674ae125927e14c512a) keep subnet's vlan empty if not specified
 * [4e28600d](https://github.com/kubeovn/kube-ovn/commit/4e28600d4a42844832bfb2cb70f30b51dea0b21b) delete ecmp route when node is not ready
 * [d145f575](https://github.com/kubeovn/kube-ovn/commit/d145f5759a3245dcce407cadcd5271034fe9a224) add del learned routes when remove ovnic
 * [6499e585](https://github.com/kubeovn/kube-ovn/commit/6499e5859f92c0ba58f266aa308c795a2c52ba3b) [kubectl-ko] support trace in underlay networking
 * [23d84f0a](https://github.com/kubeovn/kube-ovn/commit/23d84f0a7e8ef9e83777b003986f2e1bbdf11a38) fix subnet available IPs
 * [eced6bac](https://github.com/kubeovn/kube-ovn/commit/eced6bacdcc66cb523a0ed47271061f8ce654056) fix bug for deleting ovn-ic lrp failed
 * [a4abbb2e](https://github.com/kubeovn/kube-ovn/commit/a4abbb2e8af05b04e0b5c0702439b7aa5966b019) add node internal ip into ovn-ic advertise blacklist
 * [2ec0aa74](https://github.com/kubeovn/kube-ovn/commit/2ec0aa7494805e472182b52b0c5fb643899e083a) underlay/vlan network refactoring
 * [ead2c65f](https://github.com/kubeovn/kube-ovn/commit/ead2c65f00263f653dc093abbaf5f2d64710eff3) chore: update ovn to 21.03
 * [651a634d](https://github.com/kubeovn/kube-ovn/commit/651a634d6b78c9d719947fcaee6aa6cda4ec84a0) security: fix CVE-2021-3121
 * [8cff6851](https://github.com/kubeovn/kube-ovn/commit/8cff685191f6911f477d4632d43a409311213da1) list ls with label to avoid listing ts failure
 * [3fd9c7ac](https://github.com/kubeovn/kube-ovn/commit/3fd9c7acd32e1f361ab59b90db3c58a88d70b8dd) Update log error
 * [0fe67258](https://github.com/kubeovn/kube-ovn/commit/0fe67258a9e65eaf46671425592ba73d271cbcb9) delete the process of ip crd delete in cni delete request
 * [9049fc72](https://github.com/kubeovn/kube-ovn/commit/9049fc725eb66761461268fde65a6c0a3d7673af) update networkpolicy process
 * [a5b22a21](https://github.com/kubeovn/kube-ovn/commit/a5b22a21e9ab6a210a5a0e4f24367b4181e55e12) modify func name Additonal to Additional
 * [0cd5dcfe](https://github.com/kubeovn/kube-ovn/commit/0cd5dcfec78517ee738d6620fb5cf3ae18e9eda0) fix uninstall.sh execution in OVS pods
 * [b4ce83a2](https://github.com/kubeovn/kube-ovn/commit/b4ce83a2cf2df2467cd015682ac2812f8b5dabbc) perf: enable tx offload again as upstream already fix it
 * [9ca47b65](https://github.com/kubeovn/kube-ovn/commit/9ca47b6584a99866e7fee94381285e34df9cd1fa) label lr, ls and lsp, and add label filter when gc
 * [37a045a3](https://github.com/kubeovn/kube-ovn/commit/37a045a31d2581a0373538a04a9362cf7cb158b0) security: add go build security options
 * [bdf91846](https://github.com/kubeovn/kube-ovn/commit/bdf91846bd646d084487f7637d79a2ff9ec3ab6a) feat: ko support cluster operations status/kick/backup
 * [efdce464](https://github.com/kubeovn/kube-ovn/commit/efdce464567c2a9705b3f6f70629662a17d48367) docs: update docs about vlan/internal-port/kubeconfig
 * [ced43405](https://github.com/kubeovn/kube-ovn/commit/ced434053d9b5bf280de476b3a93988a3eabb78b) add judge before use slices's index
 * [3d98d762](https://github.com/kubeovn/kube-ovn/commit/3d98d7626ff7db93927b3c25f80531400e1dceff) update kind to version v0.11.1
 * [e1e63cfa](https://github.com/kubeovn/kube-ovn/commit/e1e63cfaff83fea058bfb6ad584c849a427c619a) adapt to vfio-pci driver
 * [205f5712](https://github.com/kubeovn/kube-ovn/commit/205f571245c939820faf27e292c383ecaecb7397) fix IP/route transfer on node reboot
 * [a3cac539](https://github.com/kubeovn/kube-ovn/commit/a3cac539f6bb13d52135641b6132a79098e7d2ca) add master check when a node adding to a cluster and config sb/nb address
 * [b98afeef](https://github.com/kubeovn/kube-ovn/commit/b98afeefcd415ec6bb236b36bf58ff7797ae8035) update installation scripts
 * [2d750cbf](https://github.com/kubeovn/kube-ovn/commit/2d750cbf1226dd5e8e6f3ed8d0dc762a3fa59883) enable hw-offload
 * [64b9abae](https://github.com/kubeovn/kube-ovn/commit/64b9abae6083eb0305cd85d9ff1ddb3631a93cab) do not delete statefulset pod when update pod
 * [4359c198](https://github.com/kubeovn/kube-ovn/commit/4359c1980534d3b6e38a977ac327b07c5da35cbe) fix: node route should filter out 'vpc'
 * [744e6577](https://github.com/kubeovn/kube-ovn/commit/744e6577ba7d705ec94acc9a7a53dfd0c33907c6) feat: lb switch
 * [7ec2f994](https://github.com/kubeovn/kube-ovn/commit/7ec2f994dd06332eef4db6ff4507ef9a0b6929fe) docs: show openstack docs and docker image status
 * [5484387f](https://github.com/kubeovn/kube-ovn/commit/5484387f24f60af534b55c44bfe1e5dde4c6b0fe) fix: clean up gateway chassis list for external gw
 * [acc95f1d](https://github.com/kubeovn/kube-ovn/commit/acc95f1d67332abeb15bb67244af15179111a772) add doc for openstack/kubernetes hybrid deploy
 * [e2973c4f](https://github.com/kubeovn/kube-ovn/commit/e2973c4f04b32528a2769db200e9c9f7c8f56f94) configure OVS internal port after dummy interface
 * [8608b7e5](https://github.com/kubeovn/kube-ovn/commit/8608b7e5ab97273adfca03ec9de05f1144937aec) some fixes in vlan initialization
 * [872340c8](https://github.com/kubeovn/kube-ovn/commit/872340c87cb2e0db84fcd52f9669b99333a1d21f) clean up vpc service
 * [fde89914](https://github.com/kubeovn/kube-ovn/commit/fde89914587de175191517877dbbd33960bff0ba) feat: vpc load balancer
 * [8ed91be4](https://github.com/kubeovn/kube-ovn/commit/8ed91be4a2fcfe969a55721ed18ea419120bbdb6) fix: lsp may lost when server pressure is high
 * [42fbe86e](https://github.com/kubeovn/kube-ovn/commit/42fbe86e7d0d66a3f8e8ebcaa81ef82f2875be58) fix: check crds when controller start
 * [a5fef59b](https://github.com/kubeovn/kube-ovn/commit/a5fef59b57a840b1248b581b3d00959914055fd2) start evpc ph1
 * [31ee8c10](https://github.com/kubeovn/kube-ovn/commit/31ee8c104396b57c3cc0b9963e8d5f462d5aa691) start evpc ph1
 * [44db142e](https://github.com/kubeovn/kube-ovn/commit/44db142e511c7d655eee655d98387dd88701bcb8) ci: retry arm build when failed
 * [96c13985](https://github.com/kubeovn/kube-ovn/commit/96c139851d3ddd4f35f90729a9cc813f0d28526e) update ecmp notes
 * [8c169322](https://github.com/kubeovn/kube-ovn/commit/8c169322fcbd168033cfacaa63a641fb799549c8) add interface name in cni response
 * [aa88e2a2](https://github.com/kubeovn/kube-ovn/commit/aa88e2a21b8749e6531479162aceaf0f174e7cfa) add nicType for offload
 * [eb387428](https://github.com/kubeovn/kube-ovn/commit/eb387428e114bee62591c6850a2d21a5973f8300) 1.Support to specify node nic name 2.Delete extra blank lines
 * [cb8cc645](https://github.com/kubeovn/kube-ovn/commit/cb8cc6454f19b55432a8378402cd1f29ae308522) ignore update pod nic annotation when is not nil
 * [3a4347b9](https://github.com/kubeovn/kube-ovn/commit/3a4347b92b343cc9fbc0bd8f841213b681cca9c9) set default UnderlayGateway to true in vlan mode
 * [a0d78920](https://github.com/kubeovn/kube-ovn/commit/a0d78920fb1aaabd08d38992254be1e7da87bb3c) unify logical entity list funcs (#863)
 * [9e563d84](https://github.com/kubeovn/kube-ovn/commit/9e563d8476fd46c4bd23e0183a0f6f042437c034) ci: remove dpdk ci
 * [e48a0894](https://github.com/kubeovn/kube-ovn/commit/e48a0894e0e82cd672b3de53682b5b8049cc4601) correct vlan e2e testing
 * [f690085d](https://github.com/kubeovn/kube-ovn/commit/f690085d89ea26821dba38175da185ba22f951f1) fix: remove rollout check
 * [2b2df3dc](https://github.com/kubeovn/kube-ovn/commit/2b2df3dc1c925de42b1674c34e56014b0ee0ebf2) adapt internal tcpdump
 * [2531779a](https://github.com/kubeovn/kube-ovn/commit/2531779a464221bf63c309d043de14096697fab3) update docker buildx install method
 * [eef1b0aa](https://github.com/kubeovn/kube-ovn/commit/eef1b0aa88408c80cc5120fa5b3299bd0848b053) fix: remove wait ovn sb
 * [2e59e81c](https://github.com/kubeovn/kube-ovn/commit/2e59e81c9113b217351205e985c264b3acc69a0a) fix: ci issues
 * [df47c489](https://github.com/kubeovn/kube-ovn/commit/df47c489e86305a92485e98f367f5b1d208e5fae) fix: cleanup kube-ovn-monitor resource
 * [598cffdd](https://github.com/kubeovn/kube-ovn/commit/598cffdd7f2ee931fd247e96a0d12426d0a5c62f) fix multi-nic.md
 * [f4b75bd0](https://github.com/kubeovn/kube-ovn/commit/f4b75bd04fee12b8e55896ba7f325ef6974e455d) fix: acl overlay issues
 * [2fe4fe1d](https://github.com/kubeovn/kube-ovn/commit/2fe4fe1d4d4e79c332dea720b85284ae7b8bfd29) ci: split ovn/ovs into base image
 * [db2b7b06](https://github.com/kubeovn/kube-ovn/commit/db2b7b069a1f99a65aeeb69f758a46cd05baddde) add judge before use slices's index
 * [3e259ae9](https://github.com/kubeovn/kube-ovn/commit/3e259ae909877e7e787a2533de325f1d3a24cd16) update version to v1.7 in docs
 * [eb54dc03](https://github.com/kubeovn/kube-ovn/commit/eb54dc037ea75cc58ba01aab97be9d340eeff274) update master version to v1.8.0

### Contributors

 * Mengxin Liu
 * Ruijian Zhang
 * Tobias
 * feixiang43
 * hzma
 * lhalbert
 * lut777
 * pengbinbin
 * pengbinbin1
 * wang_yudong
 * xieyanker
 * xuhao
 * zhang.zujian
 * zhangzujian
 * 范日明
 * 马洪贞

## v1.7.3 (2021-10-09)

 * [6329a275](https://github.com/kubeovn/kube-ovn/commit/6329a2750cf0e21f206d16d5e73dbc3b88cb7607) release: prepare for 1.7.3
 * [a17dd60d](https://github.com/kubeovn/kube-ovn/commit/a17dd60d60c86829c08bd43006bcda4a8ec6ed0c) fix: disable periodically gc
 * [26a355d9](https://github.com/kubeovn/kube-ovn/commit/26a355d9c6026247cc328b07ec1723b670c94022) fix installation scripts
 * [be8b5ea7](https://github.com/kubeovn/kube-ovn/commit/be8b5ea7abd44194021006d3c7158e5202ce69b5) fix StatefulSet down scale
 * [506e95d5](https://github.com/kubeovn/kube-ovn/commit/506e95d5ea70fea7e32ff04659d5995367281000) fix: init node with wrong ipamkey and lead conflict
 * [7fed7ee3](https://github.com/kubeovn/kube-ovn/commit/7fed7ee32fb5d7232d7337d512cfc08365428ad4) refactor: mute ovn0 ping log and add ping details
 * [9110bcef](https://github.com/kubeovn/kube-ovn/commit/9110bcef7c035535cf27f0c93aa655b454453daf) fix: wrong alias for iptables
 * [18053abd](https://github.com/kubeovn/kube-ovn/commit/18053abd2fe0bb04afe12b1258af19b9c40e1b0d) fix: northd probe issues
 * [698d92c6](https://github.com/kubeovn/kube-ovn/commit/698d92c659bfd6517e873fffe289921ed0417850) fix IPAM for StatefulSet
 * [0c1baacb](https://github.com/kubeovn/kube-ovn/commit/0c1baacb2825c42dca94fbdf820555fbbb86a8ed) append externalIds for pod and node when upgrade
 * [905b789f](https://github.com/kubeovn/kube-ovn/commit/905b789fa6b02081ca2abaf1f5602c6004d1c67c) security: update base image
 * [7d86e2c5](https://github.com/kubeovn/kube-ovn/commit/7d86e2c5f7fdb13164851180227d22b5d8099707) fix gc lsp statistic for multiple subnet
 * [6ce5cd8b](https://github.com/kubeovn/kube-ovn/commit/6ce5cd8be21a717405e94ed11b586086363e342a) fix: kubeclient timeout
 * [c3b72cff](https://github.com/kubeovn/kube-ovn/commit/c3b72cff50eff765b11d310661e59ec05e640667) fix: serialize pod add/delete order
 * [530a3dd0](https://github.com/kubeovn/kube-ovn/commit/530a3dd0c175a75d4c8bd59a583944871db3405c) refactor: reuse waitNetworkReady to check ovn0 and slightly improve the installation speed
 * [121c9a41](https://github.com/kubeovn/kube-ovn/commit/121c9a41a1c0b01395e38f7d8a5ed0e62f972564) perf: increase ovn-nb timeout
 * [1f97edcc](https://github.com/kubeovn/kube-ovn/commit/1f97edccdbc004a36b5386acd0ebfbade6d8c855) fix: re-check ns annotation to avoid annotations lost
 * [c79244fc](https://github.com/kubeovn/kube-ovn/commit/c79244fc17640ad62a0f18854c8901438dfb444a) perf: do not diagnose external access
 * [6bc241fc](https://github.com/kubeovn/kube-ovn/commit/6bc241fca0282969b67295977e1bec7b9de5fdf2) reactor: remove ovn ipam options
 * [74ab9aa1](https://github.com/kubeovn/kube-ovn/commit/74ab9aa16ee2f0d61f58af895a862c7f48db787e) perf: switch's router port's addresses to "router"
 * [a5791a01](https://github.com/kubeovn/kube-ovn/commit/a5791a01565a9fda089166fc8a481f60a8bdfa57) fix e2e testing
 * [6505e2e4](https://github.com/kubeovn/kube-ovn/commit/6505e2e413eff0b7f53843292ce7684da09b912b) fix variable referrence
 * [d1f14509](https://github.com/kubeovn/kube-ovn/commit/d1f145098691ebf86b482979639170cc0080b697) fix nat-outgoing/policy-routing on pod startup

### Contributors

 * Mengxin Liu
 * hzma
 * lut777
 * zhangzujian

## v1.7.2 (2021-09-08)

 * [cd650db4](https://github.com/kubeovn/kube-ovn/commit/cd650db44953a94c8a78077da19e4922b7ff4fa5) fix: VLAN CIDR conflict check
 * [4cabb12c](https://github.com/kubeovn/kube-ovn/commit/4cabb12cc4c6a46cc006ab46590e69f5c7a5f628) perf: use link alias to filter packet
 * [af4a1983](https://github.com/kubeovn/kube-ovn/commit/af4a1983253a4fd8f0c958b798398ec34333155e) security: fix CVE-2021-3538
 * [c6daff2a](https://github.com/kubeovn/kube-ovn/commit/c6daff2a7be7512849d139d008766867701823ce) prepare for release v1.7.2
 * [18241707](https://github.com/kubeovn/kube-ovn/commit/18241707f2408e3e99ef6dd3fb4d61b88903b0e8) initialize ipsets on cni server startup
 * [cf32ab1e](https://github.com/kubeovn/kube-ovn/commit/cf32ab1e02b4fdb2386ff71eae97592a8c9547d9) delete residual ovs internal ports
 * [7d94413f](https://github.com/kubeovn/kube-ovn/commit/7d94413f3d6255c8c4ef2a7518ebede58fef085d) fix: ovn-northd svc flip flop
 * [316d141e](https://github.com/kubeovn/kube-ovn/commit/316d141e36e47beaf4f159c7905345faceac6eb7) fix subnet conflict check for node address
 * [d44273e9](https://github.com/kubeovn/kube-ovn/commit/d44273e9edf4ee4777762a26447bb559dbbef2f5) update comment for SetInterfaceBandwidth
 * [06810be2](https://github.com/kubeovn/kube-ovn/commit/06810be2370e1857480d1af659817e60523b7297) update encap ip by node annotation periodic
 * [99ec3d4a](https://github.com/kubeovn/kube-ovn/commit/99ec3d4a8b650733d76ae04ef348b6f3d2301fda) delete subnet AvailableIPs and UsingIPs para
 * [c57c6dbc](https://github.com/kubeovn/kube-ovn/commit/c57c6dbc76cc2ebdbbd4cebf2aca9a34e5fdacfc) fix ipset on pod creation/deletion
 * [ef9dbc5b](https://github.com/kubeovn/kube-ovn/commit/ef9dbc5bb5f39aaf6812c2042f487293378ca067) add ready status for provider network
 * [8906e457](https://github.com/kubeovn/kube-ovn/commit/8906e457cba6ed76976ba20dcd025bb5e30938fa) avoid Pod IP to be the same with node internal IP
 * [85b57239](https://github.com/kubeovn/kube-ovn/commit/85b5723951bf8991355a070633ac2e7d4d665983) update node labels and provider network's status.readyNodes when provider network is not initialized successfully in a node
 * [078c0c8b](https://github.com/kubeovn/kube-ovn/commit/078c0c8be697352b3921878c24a48ab780b6fc5c) fix issues in underlay networking
 * [2919288a](https://github.com/kubeovn/kube-ovn/commit/2919288ad12ddc394176531856663bcf16284fe9) fix IPv6-related issues
 * [aaf56e65](https://github.com/kubeovn/kube-ovn/commit/aaf56e654978295d4dc228602b03867ec9f00caa) ci: use stable version
 * [25609873](https://github.com/kubeovn/kube-ovn/commit/2560987355f067d365416d8ff5327720224dff95) fix: bad udp checksum when access nodeport
 * [78077f34](https://github.com/kubeovn/kube-ovn/commit/78077f34f5cf5641afa9a2b9800a1b45f7b8e2d1) ensure provider nic is up
 * [154f21c3](https://github.com/kubeovn/kube-ovn/commit/154f21c3d7b19cec787cd0d2043a0753b8608ed7) fix uninstall.sh
 * [7a4c5a59](https://github.com/kubeovn/kube-ovn/commit/7a4c5a59d83b9aac0fb8dc6d841473dd3df5f063) fix gofmt lint
 * [169a3256](https://github.com/kubeovn/kube-ovn/commit/169a32568656f940a139fb0b78a1f4ee02992e84) if the string of ip is empty,program will die
 * [1065c8e4](https://github.com/kubeovn/kube-ovn/commit/1065c8e47e46a27250706c6f8dcfa183a5494b65) fix dual stack cluster created by kind
 * [dd756c05](https://github.com/kubeovn/kube-ovn/commit/dd756c0590b1b1e2e2547ead4889aa3bb2da605a) fix default bind socket of cni server
 * [6ebbbbf4](https://github.com/kubeovn/kube-ovn/commit/6ebbbbf4fac32a5e540132014c2166a323aaf123) update kind to v0.11.1
 * [ad2b08ec](https://github.com/kubeovn/kube-ovn/commit/ad2b08ec600416e4608d7661de124b8a54c18897) fix underlay networking on node reboot
 * [2ba31cc1](https://github.com/kubeovn/kube-ovn/commit/2ba31cc11fcb232d9dd27dc1179887a1520319d2) append pod/exec resource for vpc nat gw
 * [7831f803](https://github.com/kubeovn/kube-ovn/commit/7831f80396ac6ec52830f1d1e89eb4e0fe4b3c55) fix: panic when node has nil annotations
 * [554cc044](https://github.com/kubeovn/kube-ovn/commit/554cc0444f727c745e27b56ed30782b138770711) update qos process
 * [a47d9297](https://github.com/kubeovn/kube-ovn/commit/a47d92973893f766e3a76cd4e833ae8fe207fd09) delete attachment ips
 * [b633ab3c](https://github.com/kubeovn/kube-ovn/commit/b633ab3cfa7ef69a6c64b4d26bbdf63f33c9c149) fix external_ids:pod_netns
 * [b3190ef8](https://github.com/kubeovn/kube-ovn/commit/b3190ef881ae573a4288b84a420250fa974a4210) fix subnet e2e
 * [ae3cc954](https://github.com/kubeovn/kube-ovn/commit/ae3cc9546f098f3beb887a20424d33b1542aa5f1) ignore empty strings when counting lbs
 * [a9bee809](https://github.com/kubeovn/kube-ovn/commit/a9bee8098dfb1cfd181e1e559bd4288d1928b403) fix iptables
 * [5cd1b14e](https://github.com/kubeovn/kube-ovn/commit/5cd1b14ed798473f360c9ea4e94ea637e229bc44) fix image version
 * [a93e2dec](https://github.com/kubeovn/kube-ovn/commit/a93e2deceaa72ad0e07f7f27fe4730e3d0393b06) fix cleanup.sh
 * [0e3c1cbc](https://github.com/kubeovn/kube-ovn/commit/0e3c1cbc7f92ae3fb8d29f55d4b16eb6063c64db) security: fix CVE-2021-33910
 * [50da96ae](https://github.com/kubeovn/kube-ovn/commit/50da96aeaff09cfcf37517dd007474da77001ac2) delete ecmp route when node is deleted
 * [851dd303](https://github.com/kubeovn/kube-ovn/commit/851dd3034f31332c13d09bfb8f94912a323d6785) fix: if nftables not exists do no exit
 * [e48c985b](https://github.com/kubeovn/kube-ovn/commit/e48c985b50f88413582ddfaa4a9b3829cfb2c8e9) delete overlapped var subnet
 * [1dfcf6df](https://github.com/kubeovn/kube-ovn/commit/1dfcf6dff75d488022c06522cc3737f25bea2c95) match chassis until timeout
 * [4f09a0d5](https://github.com/kubeovn/kube-ovn/commit/4f09a0d53133cff5b932e2b45afee4800cb95e0a) update qos ingress_policing_burst
 * [a63de27a](https://github.com/kubeovn/kube-ovn/commit/a63de27a6f898518e9399f19a6f8ae1354d68424) fix ipsets
 * [cc51be3d](https://github.com/kubeovn/kube-ovn/commit/cc51be3d18768cb6e03a2ab8bc24fc6f6eadfe1b) update underlay e2e testing
 * [7cd02fef](https://github.com/kubeovn/kube-ovn/commit/7cd02fef501a425e9c531e6fb8f699fc48cbc1c8) fix CRD provider-networks.kubeovn.io

### Contributors

 * Mengxin Liu
 * Ruijian Zhang
 * feixiang43
 * hzma
 * lut777
 * zhangzujian
 * 范日明

## v1.7.1 (2021-07-15)

 * [1b289a22](https://github.com/kubeovn/kube-ovn/commit/1b289a22c791587b0269e8984118db35698542e8) ready for release v1.7.1
 * [795fbdf0](https://github.com/kubeovn/kube-ovn/commit/795fbdf0febb10bfa93aee891688cf44bdb9cb6b) add field defaultNetworkType in configmap ovn-config
 * [dc440c76](https://github.com/kubeovn/kube-ovn/commit/dc440c76e928b8e0d3ef3b170e156d20332334c3) keep subnet's vlan empty if not specified
 * [7b7eef98](https://github.com/kubeovn/kube-ovn/commit/7b7eef98fc0c6d708809d1ff642ad510d4875dd8) update ecmp notes
 * [d26850de](https://github.com/kubeovn/kube-ovn/commit/d26850de6cc8431b484c53e4c6b14db17e44e541) delete ecmp route when node is not ready
 * [72a73fb6](https://github.com/kubeovn/kube-ovn/commit/72a73fb679ee35ed2dbb5ff829e89b9b4909d581) delete the process of ip crd delete in cni delete request
 * [22a296e5](https://github.com/kubeovn/kube-ovn/commit/22a296e590d932d7d98615b6b4114b3dee7f3fb0) fix subnet available IPs
 * [b6076028](https://github.com/kubeovn/kube-ovn/commit/b60760288f79fad7213c54145c3563be95475829) [kubectl-ko] support trace in underlay networking
 * [0b877b96](https://github.com/kubeovn/kube-ovn/commit/0b877b9656867ba4637577996c13f543c9df1ba9) underlay/vlan network refactoring
 * [7c529a18](https://github.com/kubeovn/kube-ovn/commit/7c529a18f089cfc159d482ea4d340352478c3111) adapt internal tcpdump
 * [10481d9b](https://github.com/kubeovn/kube-ovn/commit/10481d9b3b37c8f511fa84dd2066080465eec70f) fix bug for deleting ovn-ic lrp failed
 * [1adb788f](https://github.com/kubeovn/kube-ovn/commit/1adb788feb4102b0a4d739844550d0cf5425b480) add node internal ip into ovn-ic advertise blacklist
 * [f9d542ee](https://github.com/kubeovn/kube-ovn/commit/f9d542ee0aca9b19e40e5bd8a73e10cb2c06d419) security: fix CVE-2021-3121
 * [498c7dd1](https://github.com/kubeovn/kube-ovn/commit/498c7dd1b9f6c3a556094639d2e4bb971d14d953) feat: ko support cluster operations status/kick/backup
 * [d812c746](https://github.com/kubeovn/kube-ovn/commit/d812c746720629f01c143a7c064478393ec717c0) fix uninstall.sh execution in OVS pods
 * [fd512511](https://github.com/kubeovn/kube-ovn/commit/fd51251178985c994779114bc687c980cc404b11) perf: enable tx offload again as upstream already fix it
 * [f41d5742](https://github.com/kubeovn/kube-ovn/commit/f41d57429990103f6fcb34613f28bb7497d02fa5) security: add go build security options
 * [feedaca8](https://github.com/kubeovn/kube-ovn/commit/feedaca88ce7344abf15b2a828ad170cd74e4762) fix IP/route transfer on node reboot
 * [5406d701](https://github.com/kubeovn/kube-ovn/commit/5406d70139946da7178df26c2610dac030170d0f) add master check when a node adding to a cluster and config sb/nb address
 * [136ead43](https://github.com/kubeovn/kube-ovn/commit/136ead4307c55493ac035bd0c19cc70177828a46) do not delete statefulset pod when update pod
 * [1ef87e13](https://github.com/kubeovn/kube-ovn/commit/1ef87e13317ea4f39ea6ff6de3389e00c5f74a40) fix: node route should filter out 'vpc'
 * [0761fe7a](https://github.com/kubeovn/kube-ovn/commit/0761fe7ac17b2256d1ed23d299a9ec9f758c7314) some fixes in vlan initialization
 * [63122eb8](https://github.com/kubeovn/kube-ovn/commit/63122eb805d8f4ffc720e164685a5e04e1dc512b) fix: clean up gateway chassis list for external gw
 * [96e22451](https://github.com/kubeovn/kube-ovn/commit/96e224519b767965cf806fdc32c2f6a3d909e26c) ci: remove dpdk ci
 * [7003890e](https://github.com/kubeovn/kube-ovn/commit/7003890e9c9c1abfd04ce2d5afaebd1b36c95d5d) correct vlan e2e testing
 * [dcdf75a3](https://github.com/kubeovn/kube-ovn/commit/dcdf75a38093154233072125f8b87b8d03586ea6) configure OVS internal port after dummy interface
 * [9b70842a](https://github.com/kubeovn/kube-ovn/commit/9b70842a0ff644f3aac8d0be81e22f658c945eb1) fix: lsp may lost when server pressure is high
 * [1f48f9fd](https://github.com/kubeovn/kube-ovn/commit/1f48f9fd9124e6089ad6c7a061f054a4476a2319) 1.Support to specify node nic name 2.Delete extra blank lines
 * [8c37d4b9](https://github.com/kubeovn/kube-ovn/commit/8c37d4b932268ea6fd2ff5159f6cdb2e378b0f4f) ignore update pod nic annotation when is not nil
 * [00e2e009](https://github.com/kubeovn/kube-ovn/commit/00e2e009eb29f83aa360609b2627485072defa23) set default UnderlayGateway to true in vlan mode
 * [f11cdf94](https://github.com/kubeovn/kube-ovn/commit/f11cdf9421f2ba97a8aabeb23812953b7f46e82d) fix: remove rollout check
 * [2d67471d](https://github.com/kubeovn/kube-ovn/commit/2d67471db6e6a99ddb6c844832dc5f27323eac40) fix: remove wait ovn sb
 * [ba7d6553](https://github.com/kubeovn/kube-ovn/commit/ba7d655329e03042c05e3adbf736a767db16c75e) fix: cleanup kube-ovn-monitor resource
 * [1e1da5a5](https://github.com/kubeovn/kube-ovn/commit/1e1da5a5586c791b1ae4b55c7b47955b92cce4a5) fix: acl overlay issues
 * [00681fb0](https://github.com/kubeovn/kube-ovn/commit/00681fb09c32c5c545dbd011291a36aa87f5d794) update version to v1.7 in docs

### Contributors

 * Mengxin Liu
 * Ruijian Zhang
 * hzma
 * lut777
 * xuhao
 * zhangzujian
 * 范日明
 * 马洪贞

## v1.7.0 (2021-06-03)

 * [907b34d2](https://github.com/kubeovn/kube-ovn/commit/907b34d2b7264436461c3b7d5fc02609e0a446d1) prepare for release v1.7.0
 * [ab727c98](https://github.com/kubeovn/kube-ovn/commit/ab727c98be5dbe848dafc44025e766553508049e) diagnose: check sa related resource
 * [9bd2e9f8](https://github.com/kubeovn/kube-ovn/commit/9bd2e9f87dfe85062020c9ce957cff64e90242ec) fix: do not nat route traffic
 * [3bd14945](https://github.com/kubeovn/kube-ovn/commit/3bd14945b4f2d998e939a1b3bd4d10b3b7535364) fix: release ip addresses even if pods not found
 * [f4794183](https://github.com/kubeovn/kube-ovn/commit/f47941838bd5acf9c1e87fb12cb01a0cff1ac688) fix typo
 * [2a2160d0](https://github.com/kubeovn/kube-ovn/commit/2a2160d0c663b0d19bcd00347fe5239ea785ffa2) docs: add description of custom kubeconfig
 * [3dd99a79](https://github.com/kubeovn/kube-ovn/commit/3dd99a7910830c604d7bef30ff24415214b099d7) fix: add address_set to avoid error message
 * [ba40fd67](https://github.com/kubeovn/kube-ovn/commit/ba40fd67e4a4c7bc6cf99c63549b97e8d65c5c0a) optimize Makefile
 * [cb95f4e6](https://github.com/kubeovn/kube-ovn/commit/cb95f4e6db63a3476e7b11249f6ac25b5e387316) update vlan document
 * [31a96f21](https://github.com/kubeovn/kube-ovn/commit/31a96f219d162705345e5a76c278b79946aada58) add label to avoid deleting other
 * [6cd6b34b](https://github.com/kubeovn/kube-ovn/commit/6cd6b34b0f1e836bc6cdf65532676dcce7e88390) delete unused log
 * [34734010](https://github.com/kubeovn/kube-ovn/commit/347340106c365edecfdca3756b1ea1461715cf05) add ovs internal-port for pod network interface
 * [9e715623](https://github.com/kubeovn/kube-ovn/commit/9e715623deab30b7b72e21f03e001474fe77ed4a) support underlay mode with single nic
 * [d6c96d07](https://github.com/kubeovn/kube-ovn/commit/d6c96d07dd56049ef708f8d156dadc57e3997e92) support underlay mode with single nic
 * [c1d3fc3c](https://github.com/kubeovn/kube-ovn/commit/c1d3fc3cfe51d886e427923caa75b9fd9b4f4df7) fix: add node to pod allow acl
 * [ed49cd49](https://github.com/kubeovn/kube-ovn/commit/ed49cd4978c81982eb0dcabd3db4ef937b7af533) traffic rate for multus nic
 * [1b00190f](https://github.com/kubeovn/kube-ovn/commit/1b00190f3025fa66ef0ba1c3a4b8145eeb9a98d8) add ovs internal-port for pod network interface
 * [775aec6c](https://github.com/kubeovn/kube-ovn/commit/775aec6c6b24cfd9ab6fbcc46943556a1d282810) Add maintainers
 * [59847bc1](https://github.com/kubeovn/kube-ovn/commit/59847bc108cd37dc624da2c7d1332079263f6aea) add e2e tests for external egress gateway
 * [a0006ebf](https://github.com/kubeovn/kube-ovn/commit/a0006ebf01b7a8425a72f8f1f51864585fde01e0) fix e2e testing on macOS
 * [0ff3d6bb](https://github.com/kubeovn/kube-ovn/commit/0ff3d6bb78f6371790ca647cb2febc982210b9cb) ci: fix lint and scan error
 * [33e0ec27](https://github.com/kubeovn/kube-ovn/commit/33e0ec27c302151b8c86fa47d17e13a0184df8d8) fix: check if provider network exists
 * [9e53d4cc](https://github.com/kubeovn/kube-ovn/commit/9e53d4cc8b9382e9ef51471e5bf40250ebea31b0) update subnet document
 * [a2e4fec4](https://github.com/kubeovn/kube-ovn/commit/a2e4fec4f943232b6fe6cac0d23a0e047dca11e3) rename ExternalGateway to ExternalEgressGateway
 * [1ccaec9a](https://github.com/kubeovn/kube-ovn/commit/1ccaec9af8fc86065276ecba48c7871c210ab5d5) fix installation doc
 * [34fb4759](https://github.com/kubeovn/kube-ovn/commit/34fb47594208c9a15b9edeb57668227b86feec48) fix: forward policy to accept
 * [bbbd091f](https://github.com/kubeovn/kube-ovn/commit/bbbd091f6e0819405096ec5fd2f4ae77b5a21487) ci: fix lint error
 * [28cf4cc2](https://github.com/kubeovn/kube-ovn/commit/28cf4cc2d2e69dda06b9ac2a1c61dab9db8925a8) traffic rate for multus nic
 * [0dcf6930](https://github.com/kubeovn/kube-ovn/commit/0dcf69300b864438685db904ce73679691d6d5d3) refactor: optimize service.go and subnet.go
 * [7719fc2a](https://github.com/kubeovn/kube-ovn/commit/7719fc2ad42c542a8188c93cfb7c3cb260c8da2e) Check and Fetch all ValidatePodNetwork errors
 * [123ead48](https://github.com/kubeovn/kube-ovn/commit/123ead48272728ced58c721a86c19e34527d51d0) add judge about nic address
 * [17fe2302](https://github.com/kubeovn/kube-ovn/commit/17fe230273fdac9a979f2c3e523b5940f42e1286) implement new feature: external gateway
 * [01686e3e](https://github.com/kubeovn/kube-ovn/commit/01686e3e77add612cdf1fc4d9fac93547c8c58e1) start_ic should run regardless of ts port
 * [c733c7e4](https://github.com/kubeovn/kube-ovn/commit/c733c7e40498f8eac88ca0d4f19b24d65a4c555e) add judge before use index
 * [ba709afb](https://github.com/kubeovn/kube-ovn/commit/ba709afb5dbb11dc1041fcdf9f0c4e0bcb78ccee) specify ovs ops on diff nodes
 * [07089205](https://github.com/kubeovn/kube-ovn/commit/07089205dc0f1031890f98886cbfb20fd6360050) fix mss rule
 * [4458a4d7](https://github.com/kubeovn/kube-ovn/commit/4458a4d7254010fe36028c46adf3d24c0eaf3e4b) Get node info from listerv1.NodeLister(index)
 * [19a7aed9](https://github.com/kubeovn/kube-ovn/commit/19a7aed98a20bdff6ba6cfa4f04bc541bd7dabc3) Clean up the wrong log
 * [27fe348a](https://github.com/kubeovn/kube-ovn/commit/27fe348a7a7aa0a8e9807ff551754432ad911952) refactor: optimize subnet.go
 * [ddfd06b2](https://github.com/kubeovn/kube-ovn/commit/ddfd06b25e2bc35ca73814c690292f1d807ce521) Optimise the redundancy code
 * [bd55c104](https://github.com/kubeovn/kube-ovn/commit/bd55c104fa532c030a0fad6512e02b2450b7a7f0) Handler the parse config error before used
 * [bd3f13dc](https://github.com/kubeovn/kube-ovn/commit/bd3f13dccd3360efe3c6c9173d5d992b246f0e76) ci: remove 3-master e2e
 * [9e827e7b](https://github.com/kubeovn/kube-ovn/commit/9e827e7b52f5cc2dcb08d9e2210d8ad2c9ced27f) Remove the unnecessary rm command
 * [587bbcdb](https://github.com/kubeovn/kube-ovn/commit/587bbcdbb366e9b7dfae70e314acf11c9b29212d) Use localtime when the kube-ovn installed
 * [a52a38d0](https://github.com/kubeovn/kube-ovn/commit/a52a38d064e1ad83ae98b9255320b8750b02a95a) Fix the different time from container and host
 * [436e788b](https://github.com/kubeovn/kube-ovn/commit/436e788be597d039712fb32333d5b480d7a6da7e) add issue template
 * [5fc3cfb1](https://github.com/kubeovn/kube-ovn/commit/5fc3cfb184568cfc1b20c2b4a881881f0fe98224) add bgp doc
 * [f16fcb9a](https://github.com/kubeovn/kube-ovn/commit/f16fcb9ada97e8060090b786b478c783e210ecaa) support afisafis
 * [d94af379](https://github.com/kubeovn/kube-ovn/commit/d94af379bbc322c91c2b14a61e5dcf002f4565f9) feat: support graceful restart
 * [26a02725](https://github.com/kubeovn/kube-ovn/commit/26a0272527e5ad79652607ca20b2bf0bd7f6f486) fix: del might panic if duplicate delete
 * [41226d86](https://github.com/kubeovn/kube-ovn/commit/41226d86f80d41d3fcacfc22d3e5b3bf9c9c920f) fix: lr-route for eip using nic-ip, and not external gateway addr.
 * [d176dac7](https://github.com/kubeovn/kube-ovn/commit/d176dac7cc42aaa25ec5c1a8f43f595d24920f61) feat: support announce service ip
 * [136571d1](https://github.com/kubeovn/kube-ovn/commit/136571d167152b0d870e3b84d84da87de9734f1a) Fix some minor nits for docs
 * [2781a47b](https://github.com/kubeovn/kube-ovn/commit/2781a47b46895e0c2e3d94514863a87f7ad564e0) add bpg options in bgp.md
 * [1b788902](https://github.com/kubeovn/kube-ovn/commit/1b788902c388d7916dc711e44cf09cb1c09867fe) add Opstk&K8s ic doc
 * [cc843816](https://github.com/kubeovn/kube-ovn/commit/cc84381617906003edfabe7ab9f92b91db3c4c57) add holdtime function
 * [b9e96339](https://github.com/kubeovn/kube-ovn/commit/b9e96339328c7340e297b34d8bf45dac38a45425) fix: do not re-generate ts port
 * [610f132b](https://github.com/kubeovn/kube-ovn/commit/610f132bdbdb81be7b1bc1efc86ccbe132102200) fix: ignore root path doc ci
 * [bd1e0975](https://github.com/kubeovn/kube-ovn/commit/bd1e0975f7dea75566ae7328b1fd3faef514e176) fix: do not gc learned routes
 * [be2048be](https://github.com/kubeovn/kube-ovn/commit/be2048be3e636e7bb0cfc2105254ef3f23a68482) feat: add vxlan in README.md
 * [cbb2ddd4](https://github.com/kubeovn/kube-ovn/commit/cbb2ddd476f2e173f4f3e6dd440cc3da2d58dcb3) fix: get_leader_ip always return fist node ip
 * [03f597ce](https://github.com/kubeovn/kube-ovn/commit/03f597ce47ed434ce5b1a28f5a4d67c464f68711) fix: remove tty error notification
 * [cc353bbc](https://github.com/kubeovn/kube-ovn/commit/cc353bbca9d547ac30e3ca39e816ed8838c999fd) fix ovn nb reconnect
 * [af2709df](https://github.com/kubeovn/kube-ovn/commit/af2709dfa32d79887c8573572a21475684afa2c1) add docs for 'multus ovn network'
 * [ffc20a91](https://github.com/kubeovn/kube-ovn/commit/ffc20a9158cb92cce59e105a0eff294ea940fd48) add vpc nat gateway docs
 * [a1ae937a](https://github.com/kubeovn/kube-ovn/commit/a1ae937ac580bf9c529be99a56d8f45a8859c136) fix: static route for default multus network
 * [0489a72a](https://github.com/kubeovn/kube-ovn/commit/0489a72adf650b8bf91fe1cf8df279e26ca1bafc) feat: support vxlan tunnel
 * [77f65449](https://github.com/kubeovn/kube-ovn/commit/77f65449c1e5a1a5c90a256c10829c2a51ba53d4) append delete ovn-monitor in ovn.yaml
 * [c5ee49e8](https://github.com/kubeovn/kube-ovn/commit/c5ee49e8225f17f83393cd67ca666a33c49b6d32) split ovn-central and ovn-monitor
 * [e0890f72](https://github.com/kubeovn/kube-ovn/commit/e0890f727327afd792aed64521aa5c78a384ef41) Fix mount the systemid path
 * [fc92fbc2](https://github.com/kubeovn/kube-ovn/commit/fc92fbc2eadfd2f83f16aac04ebf32dfe2d0fac7) handle update deployment vpc-nat-gw
 * [686681ef](https://github.com/kubeovn/kube-ovn/commit/686681ef3603519588158a9b5e80f49dbac291b6) refactor: remove function genNatGwDeployment's return error
 * [064c3851](https://github.com/kubeovn/kube-ovn/commit/064c38510219e8fb81c1f0e65352ff15e35e6603) Update crd vpc-nat-gateways.kubeovn.io for pre-1.16
 * [a0dfea1b](https://github.com/kubeovn/kube-ovn/commit/a0dfea1b0f32d9e5931f569409f0d0a2de079577) fix incorrect method for gateway node judgement
 * [86c99c37](https://github.com/kubeovn/kube-ovn/commit/86c99c378bb346cefc4a47005ec16c79150800a8) Fix the 'multus how to use' link
 * [1acb4992](https://github.com/kubeovn/kube-ovn/commit/1acb499213170a1a9d3f3d7c08a5374758455912) fix multi nic
 * [9c5ca0a0](https://github.com/kubeovn/kube-ovn/commit/9c5ca0a04b08b68d3d68c5dadc82e970b2fa1d10) fix duplicate imports
 * [b4750853](https://github.com/kubeovn/kube-ovn/commit/b4750853db9ae994dc7a7599fa6e506885416984) fix: compatible with JSON format
 * [2a2cd27a](https://github.com/kubeovn/kube-ovn/commit/2a2cd27a6a4f41125c4800a85ad87181f5ccb65d) fix: leader may change during startup, use cluster connection to set options.
 * [aad81548](https://github.com/kubeovn/kube-ovn/commit/aad8154848f71dab233f16eec3c160b2b2e8fa5c) fix SNAT on pod startup
 * [388119a7](https://github.com/kubeovn/kube-ovn/commit/388119a75ffe65896203136e5f4b6de22e2d3d23) fix development guide
 * [2efdac9a](https://github.com/kubeovn/kube-ovn/commit/2efdac9a4b3a74b782f4f0531525ed311a0a6a33) fix gofmt
 * [c264bec1](https://github.com/kubeovn/kube-ovn/commit/c264bec18408064ddbfc7322af50c8f2cc688ea5) fix: configure nic failed when ifname empty
 * [763f8bcf](https://github.com/kubeovn/kube-ovn/commit/763f8bcf79b1ce885b72d2b99bd9850fcdf941de) fix: port does not support vlan tag update
 * [a60764ea](https://github.com/kubeovn/kube-ovn/commit/a60764ea98e695d91826057e143840b38d0c7663) fix build dev image
 * [faa7bc6a](https://github.com/kubeovn/kube-ovn/commit/faa7bc6a5bc4e7d4bb2924bc4a4bb0c421437394) support hybrid mode for geneve and vlan
 * [d8472ba7](https://github.com/kubeovn/kube-ovn/commit/d8472ba77be74afae2bf1294d7f2f39265504cf3) remove extra space
 * [f9c836b6](https://github.com/kubeovn/kube-ovn/commit/f9c836b6df232008743f7a2f5dc58a1bc54b8087) fix: compatible with no norhtd svc
 * [bbed09d3](https://github.com/kubeovn/kube-ovn/commit/bbed09d3634002299a2da36789aeb4ee5e329ef8) fix chassis check for node
 * [dfdf5f8b](https://github.com/kubeovn/kube-ovn/commit/dfdf5f8b0fefc78d51e0b80b8b16f586924690a8) optimization for ovn/ovs status metric
 * [9e82ca3d](https://github.com/kubeovn/kube-ovn/commit/9e82ca3d594ce169dc575f0eddf8f09c1918c162) fix: release norhtd lock when power off
 * [1fbfad52](https://github.com/kubeovn/kube-ovn/commit/1fbfad523fd9e95e9470181c76a5315ff291e4f9) add single node e2e
 * [f9ae6258](https://github.com/kubeovn/kube-ovn/commit/f9ae6258e21e87a632bcb19f4acc6cc847612598) fix get pod attachment net
 * [0632e253](https://github.com/kubeovn/kube-ovn/commit/0632e253a182793cd68c9bac841f56c83e0bb51d) support ovn defautl attach net
 * [2c1a8aa6](https://github.com/kubeovn/kube-ovn/commit/2c1a8aa6425e43055d5c3271ac96083a36a1da2f) add network-attachment-definitions clusterRole
 * [808a3a93](https://github.com/kubeovn/kube-ovn/commit/808a3a93b3b4572f207480a5c94b9781939b0689) feat: multus ovn nic
 * [28e14188](https://github.com/kubeovn/kube-ovn/commit/28e14188ae40ced07979f4e99fa231063ff9769d) update node ip when upgrade to dualstack
 * [0265747d](https://github.com/kubeovn/kube-ovn/commit/0265747dc8bbb578eea25bc9f1963bcf2ebb6f5e) add details for prerequisite
 * [3e42f684](https://github.com/kubeovn/kube-ovn/commit/3e42f68421b5e73a204fd08f0cd04d07c1ee11ab) Add Ecmp Static Route for centralized gateway
 * [b72e9d50](https://github.com/kubeovn/kube-ovn/commit/b72e9d50bcfa8547233137a6a94c828172ad0ace) fix: disable offload if geneve port exists
 * [f4e665b9](https://github.com/kubeovn/kube-ovn/commit/f4e665b95f26927ec2aceecf4a709f27de4c03a9) disable offload for genev_sys_6081
 * [acade01b](https://github.com/kubeovn/kube-ovn/commit/acade01b4c41b05332e0c6b3d923fdad1428fa48) refactor: optimize ovn command when error exists
 * [5251c272](https://github.com/kubeovn/kube-ovn/commit/5251c2722310f89573337151d166a83d2e5232a1) add net-attach-def ClusterRole
 * [5126aedd](https://github.com/kubeovn/kube-ovn/commit/5126aedd1c75680354bb7e44ea02fa01dcd6be53) add lsp with external_id
 * [ec7f7425](https://github.com/kubeovn/kube-ovn/commit/ec7f7425b30d7c6cb27ebecb2586cb0d23917671) feat: multus ovn nic
 * [19e23d14](https://github.com/kubeovn/kube-ovn/commit/19e23d1419e04b6efc872fa29238e8e362a74015) fix: check ovn0 status
 * [c02afc00](https://github.com/kubeovn/kube-ovn/commit/c02afc00c9c7835fcc267ff44de6e393f136e3c5) livenessprobe fail if ovn nb/ovn sb not running
 * [983831e0](https://github.com/kubeovn/kube-ovn/commit/983831e0dfeb2c252bcc6d314d0f4d9df597ce9b) fix: disable checksum offload for ovn0 to prevent kernel issue
 * [d9f166b7](https://github.com/kubeovn/kube-ovn/commit/d9f166b7132a63619b305af7eed416915911af73) ignore ip6tabels check for v4 hostIP
 * [680802d6](https://github.com/kubeovn/kube-ovn/commit/680802d618956c48f6815820f2edb50c15b0644e) improve the code style of [import group ordering]
 * [8e38a79d](https://github.com/kubeovn/kube-ovn/commit/8e38a79da6c1c97bedbd5ff0103dae7bfc0ee60e) fix wrong sequence
 * [1e0d77c3](https://github.com/kubeovn/kube-ovn/commit/1e0d77c35f313d4d775963cf8dae2670349e1c92) update arm64 build
 * [638a03ac](https://github.com/kubeovn/kube-ovn/commit/638a03ac4ef1edd42d57825901e9f1035f105e36) fix: restart ovn-controller to force update flows
 * [14784fbb](https://github.com/kubeovn/kube-ovn/commit/14784fbb9c12b2c079319410ecb88cc7296ce209) fix: disable checksum validation
 * [a04dcfb6](https://github.com/kubeovn/kube-ovn/commit/a04dcfb626c6ad85083f40703fab0ea02a7735a8) Use public network effective image
 * [24095d7f](https://github.com/kubeovn/kube-ovn/commit/24095d7fe927376d6bc315cf629303da3594dfc9) update usingips check when update finalizer for subnet
 * [54ef1af2](https://github.com/kubeovn/kube-ovn/commit/54ef1af2afbb85584c16a1981d158372df2fc8fa) fix dependency
 * [717688d6](https://github.com/kubeovn/kube-ovn/commit/717688d66a20e0f13bd77dbd230eb7b7db4c6b61) Update vendor.
 * [496fc4dd](https://github.com/kubeovn/kube-ovn/commit/496fc4dddbf1b863555029905a0f60fe3ed6afb5) trim space the port_binding's output
 * [00fdac83](https://github.com/kubeovn/kube-ovn/commit/00fdac83cdfebeacccba8a8bb6ff21fe5dbaba58) refactor: remove unnecessary config logic
 * [b06dad21](https://github.com/kubeovn/kube-ovn/commit/b06dad21b8c89c68d8d571293a03e8f712e53af2) update maintainers
 * [e5d9584e](https://github.com/kubeovn/kube-ovn/commit/e5d9584e3c5c34f3a167877a8ffdf9698ab12785) docs: deprecated webhook
 * [92cc4ed3](https://github.com/kubeovn/kube-ovn/commit/92cc4ed376b62cdbeb1634bc5b4b2e5c60820be2) fix: add missing ovn-ic binary
 * [c0349e4f](https://github.com/kubeovn/kube-ovn/commit/c0349e4fcb4a5afc278f2a1e72443f98da715e3f) chore: change action name
 * [1a448ecc](https://github.com/kubeovn/kube-ovn/commit/1a448eccce0d65f7cf48e0895f4a012e74ef70ff) chore update artworks
 * [537588c3](https://github.com/kubeovn/kube-ovn/commit/537588c397caec5b1ee5a32eb0150a436e58c9d6) fix: delete chassis_private when delete node
 * [a50fb181](https://github.com/kubeovn/kube-ovn/commit/a50fb1817b7a3b4b8cbcf86e35b11583af64c91a) Add 'kubectl ko trace' command's default namespace
 * [fad9473d](https://github.com/kubeovn/kube-ovn/commit/fad9473d90f360cf797955fa78b90aa3ab57e077) Add 'kubectl ko trace' command's default namespace
 * [77c92ca8](https://github.com/kubeovn/kube-ovn/commit/77c92ca8c5ec5dd78268333a0d141687dceb470c) perf: reclaim heap memory after compaction.
 * [f3df58ae](https://github.com/kubeovn/kube-ovn/commit/f3df58aeafcb85d7107fa0a4974ac3d71f5f7466) remove the old script
 * [b69f389c](https://github.com/kubeovn/kube-ovn/commit/b69f389cf6ea337da30fcc012eae208196f85abd) docs: add CNCF description
 * [08b95e74](https://github.com/kubeovn/kube-ovn/commit/08b95e747bd96d054a2ffd4371a0f0e7328de062) fix: gc not exist node error
 * [9f661461](https://github.com/kubeovn/kube-ovn/commit/9f6614613cedfdbb30c268be96b3c68fc0755bf8) perf: use new option to decrease ovn-sb size
 * [9dc06908](https://github.com/kubeovn/kube-ovn/commit/9dc0690831cdc75d143b2a2eab20f3d6d739507c) fix: return err
 * [8bd44608](https://github.com/kubeovn/kube-ovn/commit/8bd4460832a207a62074cd60e8b8f731fa666b17) docs: add faq section
 * [482e6f71](https://github.com/kubeovn/kube-ovn/commit/482e6f71a078396f51cd29fd223d3cb017e1a65c) add vpc nat gateway Dockerfile
 * [b0e983f0](https://github.com/kubeovn/kube-ovn/commit/b0e983f04521ffa9bf5fcac394331a3e11ab9774) feat: vpc nat gateway
 * [951e31ea](https://github.com/kubeovn/kube-ovn/commit/951e31ea3135c72c0472cdcdb86d8a8864cf8b1a) add node address allocate check when init
 * [215c8f45](https://github.com/kubeovn/kube-ovn/commit/215c8f45bbc3f77ba5928f50c897eea53aa8c2a6) update upgrade for ovn-default and join subnet
 * [a537985d](https://github.com/kubeovn/kube-ovn/commit/a537985d9d5d4dea6208c1104f1800bf8dd281a8) fix: lint error
 * [d0d3e89c](https://github.com/kubeovn/kube-ovn/commit/d0d3e89cc82159383c3e22e0a1fb62f815134c65) fix: add missing ovn-ic-db schema
 * [98651014](https://github.com/kubeovn/kube-ovn/commit/98651014c8994ed08b4de0f9afaf32b9fe8c4384) update subnet ip num calculate
 * [d6bb03bd](https://github.com/kubeovn/kube-ovn/commit/d6bb03bd527f03aa26a8b87d5a809cd1a80a603d) fix: masq traffic to ovn0 from other nodes
 * [0a7024f9](https://github.com/kubeovn/kube-ovn/commit/0a7024f97bba8511a84185474d907a236f71a93a) refactor: reduce duplicated GetNodeInternalIP function
 * [ac294669](https://github.com/kubeovn/kube-ovn/commit/ac294669c7c7e2a24d2af6910494b6b8d175091b) chore: update go version
 * [0e9c717d](https://github.com/kubeovn/kube-ovn/commit/0e9c717d05ebc26a01c2c6c3961a17316c675260) chore: move build dependency from alauda to kubeovn
 * [64fac57a](https://github.com/kubeovn/kube-ovn/commit/64fac57a9c8626c916ff6ffdb103bd5f765ed729) feat: support set default gateway in install script
 * [ca71de3c](https://github.com/kubeovn/kube-ovn/commit/ca71de3cfb8de87413de482c155df9927316bb4b) docs: fix typos
 * [582cb9ce](https://github.com/kubeovn/kube-ovn/commit/582cb9ce6acb323fbe8e8dd19651901a24249b7f) Update install-pre-1.16.sh
 * [62fc20ef](https://github.com/kubeovn/kube-ovn/commit/62fc20efea653fc7463840c746ec7e70013fcf2f) Update install.sh
 * [87859ac1](https://github.com/kubeovn/kube-ovn/commit/87859ac1951acbe9c4331cdeb80e7eb10948c3e8) go import repo change to kubeovn
 * [1152744e](https://github.com/kubeovn/kube-ovn/commit/1152744e9f47626310601a4c63a7af61e3fe6bf6) feat: vpc nat gateway
 * [298138e4](https://github.com/kubeovn/kube-ovn/commit/298138e41d5d36f0400633bc3183f191de0c74cf) Resolving typo.
 * [4701fcb3](https://github.com/kubeovn/kube-ovn/commit/4701fcb3741213c4fbcd6e5e1a151fdcd4c6d3c2) filter repeat exclude ips
 * [e3931f0e](https://github.com/kubeovn/kube-ovn/commit/e3931f0ea926e91c36fb2ac2699cb4e080f87b58) modify ip count for dual
 * [a4ddb360](https://github.com/kubeovn/kube-ovn/commit/a4ddb3604584dd5b558686e663face1cb2a15584) docs: add ARCHITECTURE.MD
 * [9eee6f93](https://github.com/kubeovn/kube-ovn/commit/9eee6f938da6d79aac59171b6f53a108d123bede) refactor: reduce duplicated function
 * [a7b687a0](https://github.com/kubeovn/kube-ovn/commit/a7b687a05fadf8acf7b25d4dd994f96a7aa67a5a) fix: add dpdk pod name
 * [d32b423b](https://github.com/kubeovn/kube-ovn/commit/d32b423b68ffdb8d49394f65c99c9342fd73d699) Update cleanup.sh
 * [9faaff57](https://github.com/kubeovn/kube-ovn/commit/9faaff574f68bf0d763c3a29cfe4b31f26c0b659) Update cleanup.sh
 * [df065f94](https://github.com/kubeovn/kube-ovn/commit/df065f94b4a835e8f6a1dc17d7e73570f4ec80ee) test: add service e2e
 * [60e49f5a](https://github.com/kubeovn/kube-ovn/commit/60e49f5a0ff999a98e9edc733ad6b548bed224ab) modify test problem
 * [2dbcb76f](https://github.com/kubeovn/kube-ovn/commit/2dbcb76fa31cce633a63f18d1ff0a2a592e16bf9) fix: kube-proxy check
 * [512044cb](https://github.com/kubeovn/kube-ovn/commit/512044cb389df7257adca989f604e3a00da0ddd4) ovn-central: set default db addr same with leader node to fix nb and sb error 'bind: Address already in use'
 * [c755ef23](https://github.com/kubeovn/kube-ovn/commit/c755ef232313476a24111621cda522119aec7749) fix: reset ovn0 addr
 * [a168c282](https://github.com/kubeovn/kube-ovn/commit/a168c28264e12dfcbec823c6756c76cb1c22f974) tests: add e2e for ofctl/dpctl/appctl
 * [f6dc58a5](https://github.com/kubeovn/kube-ovn/commit/f6dc58a5dcabd0731db9abe1a5f92af7c396892e) ci: replace image
 * [b1d03370](https://github.com/kubeovn/kube-ovn/commit/b1d03370e61d81a0d72a4f0f2b4917fbdcffbc59) docs: clarify dpdk usage scenario
 * [21d9940b](https://github.com/kubeovn/kube-ovn/commit/21d9940b9d13257699291e516166ee02df1a45d2) ci: update kind version and set timeout
 * [8b833ee5](https://github.com/kubeovn/kube-ovn/commit/8b833ee52ec0139dcd75b55556b8fe9ce51703a0) Update install-pre-1.16.sh
 * [4b6f0eed](https://github.com/kubeovn/kube-ovn/commit/4b6f0eedbed57a64c8b025f06a220f0a9670161a) Update install.sh
 * [f6f88501](https://github.com/kubeovn/kube-ovn/commit/f6f88501f9876f9b95c0549abcc22a43733e18e6) refactor: remove duplicated call
 * [473cdc48](https://github.com/kubeovn/kube-ovn/commit/473cdc48766dea1a152b2c430069014cc4064804) Update kubectl-ko
 * [1ca17686](https://github.com/kubeovn/kube-ovn/commit/1ca17686b9493f2e64b72d7b5e3d85f7e2df08d0) Fix missing square brackets in curl ipv6
 * [136336b2](https://github.com/kubeovn/kube-ovn/commit/136336b2124593ebedd8e5663f164b7ecf9b5981) Modify the health check for kube-proxy port, compatible with ipv6
 * [98a56dec](https://github.com/kubeovn/kube-ovn/commit/98a56dece7c20235cd806ed2cdef50cebb07de24) Update controller.go
 * [c52c067b](https://github.com/kubeovn/kube-ovn/commit/c52c067b946b4aac7eef8cbb3002eb4aeefc9045) Fix: remove IsNotFound when get configmap external gateway
 * [74fa7729](https://github.com/kubeovn/kube-ovn/commit/74fa7729d806b4129edb8604ef2effa0b79c13ca) Fix: check kube-proxy's 10256 port healthz
 * [d594554d](https://github.com/kubeovn/kube-ovn/commit/d594554d73f83d7d5b9e601533c8fb27f225dc47) fix: ip6tables check error
 * [b17f2373](https://github.com/kubeovn/kube-ovn/commit/b17f23732c65807e84a7d520c5c64c6869e3ee8e) Add MAINTAINERS file
 * [2783c134](https://github.com/kubeovn/kube-ovn/commit/2783c134f16a9ea9c172afd30206868025d677ec) add vpcs && vpcs/status clusterRole
 * [31e1226e](https://github.com/kubeovn/kube-ovn/commit/31e1226e01080cdd4ce27ef7979a18ba2736439b) Update install-pre-1.16.sh
 * [f1efaa7f](https://github.com/kubeovn/kube-ovn/commit/f1efaa7f256f0bb936a65b5a1a5b8b38d12d84ff) delete connect to ovsdb for ovn-monitor
 * [f69ae44b](https://github.com/kubeovn/kube-ovn/commit/f69ae44bec65b67e2ab61689de0aa27da7143b41) cni-bin-dir,cni-conf-dir configurable Fix https://github.com/alauda/kube-ovn/issues/655
 * [f5999b3b](https://github.com/kubeovn/kube-ovn/commit/f5999b3b580f129c25c8882f136d6f35d25875c7) Update install.sh
 * [e13448aa](https://github.com/kubeovn/kube-ovn/commit/e13448aac6c524c3ee1bfa42f27b2569418e6a2c) Error: unknown command "ko" for "kubectl"
 * [7d56483a](https://github.com/kubeovn/kube-ovn/commit/7d56483a3e76983a96eff5d894b3ad92df186c37) Fix: wrong split in FindLoadbalancer function
 * [34776b8a](https://github.com/kubeovn/kube-ovn/commit/34776b8a7986225cae89be0b30773201578a84bb) vlan nic support regex
 * [f23093c4](https://github.com/kubeovn/kube-ovn/commit/f23093c4f94194be1416d0aea760d0861c77b9fe) fix underlay gateway flood logs
 * [4a9901aa](https://github.com/kubeovn/kube-ovn/commit/4a9901aa68f95ff8207cb45fb271c4d83095d563) fix: check required module before start
 * [8d4694f8](https://github.com/kubeovn/kube-ovn/commit/8d4694f835e724cac2cb23cfedbf47a1646d2044) docs: add underlay docs
 * [3713b253](https://github.com/kubeovn/kube-ovn/commit/3713b2537f6bea24f4a1d87f64016265d5f5727e) chore: update ovn to 20.12 and ovs to 2.15
 * [1ab87130](https://github.com/kubeovn/kube-ovn/commit/1ab871307bf2c98b692bfff3f7d3e1db314dab34) prepare for next release
 * [a94803d3](https://github.com/kubeovn/kube-ovn/commit/a94803d3208faec094cafb7dfa80c6cb6a286218) fix: make sure northd leader change
 * [03487cf2](https://github.com/kubeovn/kube-ovn/commit/03487cf28ac435c1723b858f03471464a689504d) fix: make sure ovn-central is updated one by one
 * [9d3b78a3](https://github.com/kubeovn/kube-ovn/commit/9d3b78a319fcd623c8df02df1f87a1b7926e205e) fix: restart when init ping failed
 * [6e09c77d](https://github.com/kubeovn/kube-ovn/commit/6e09c77deeba8ff2643f3309b19ee0d109c6f838) fix: increase raft timer to avoid leader flap
 * [87aa15cb](https://github.com/kubeovn/kube-ovn/commit/87aa15cbf9494d68d66d0e1494e7362c7f39aa76) pass golangci-lint
 * [134ea89d](https://github.com/kubeovn/kube-ovn/commit/134ea89d0e196584547171d11c78caecda059cf7) add golangci-lint to github actions
 * [d325e7e0](https://github.com/kubeovn/kube-ovn/commit/d325e7e0b606ca141b05a169ce3ea1f993782006) fix pod terminating not recycle ip when controller not ready
 * [87af4ca9](https://github.com/kubeovn/kube-ovn/commit/87af4ca9ff374fc8ae1bcbe8af84e73532f2e7c3) fix: add new iptable cleanup commands
 * [d287063b](https://github.com/kubeovn/kube-ovn/commit/d287063bf7e9a69fa740a6ea35f007e5878e78b8) modify static gw changed problem
 * [fcf3be19](https://github.com/kubeovn/kube-ovn/commit/fcf3be190e141fcc6099d34bcefd4313ae71ae81) Fix wait pod network ready take long time
 * [0b4e4458](https://github.com/kubeovn/kube-ovn/commit/0b4e4458e51033f096eb229624a36fd18b39ce13) fix: when address is empty, skip route/nat deletion
 * [ed0e9ba2](https://github.com/kubeovn/kube-ovn/commit/ed0e9ba22a7e65fc1bc0fc50c220a9957a59ed44) fix: update ipam cidr when subnet changed
 * [06816efb](https://github.com/kubeovn/kube-ovn/commit/06816efb55507304c44a33b90cf1e140533bc47a) modify test problem for dual-stack upgrade

### Contributors

 * Amye Scavarda Perrin
 * JinLin Fu
 * Mengxin Liu
 * Wan Junjie
 * Yan Wei
 * Yan Zhu
 * caoyingjun
 * chestack
 * cmj
 * danieldin95
 * halfcrazy
 * hzma
 * luoyunhe1
 * lut777
 * pengbinbin1
 * sayicui
 * wangyudong
 * withlin
 * xieyanker
 * zhangzujian
 * 范日明
 * 马洪贞

## v1.6.3 (2021-06-03)

 * [8e28e139](https://github.com/kubeovn/kube-ovn/commit/8e28e139b14c6edc95d7dbd168807ac7b1f6ce19) prepare release for v1.6.3
 * [2818eb86](https://github.com/kubeovn/kube-ovn/commit/2818eb861af3c3cba8f5f1ecfa29e09eb7706910) fix: do not nat route traffic
 * [be20533b](https://github.com/kubeovn/kube-ovn/commit/be20533bafab1cbd2ebff6404fe760ca88b48f44) fix: release ip addresses even if pods not found
 * [1bdff344](https://github.com/kubeovn/kube-ovn/commit/1bdff3443cf716954e026733f482fdcc107a8342) security: fix crypto CVE
 * [f29958db](https://github.com/kubeovn/kube-ovn/commit/f29958dbfac8620786ae6240414c51ae067c3851) fix: add address_set to avoid error message
 * [04fc67f8](https://github.com/kubeovn/kube-ovn/commit/04fc67f801d3721c20ace44e34fae9ae9f3566e3) fix: add node to pod allow acl
 * [91d43e01](https://github.com/kubeovn/kube-ovn/commit/91d43e01ae3ca38d8ae2bec34820182cf771fe2a) Handler the parse config error before used
 * [634f672b](https://github.com/kubeovn/kube-ovn/commit/634f672bf6339be32959285ff03fbc2716afcc5c) fix: del might panic if duplicate delete
 * [7795b519](https://github.com/kubeovn/kube-ovn/commit/7795b519c7e1aab3f6be63f27f99645ce46229af) fix: do not re-generate ts port
 * [37ed257f](https://github.com/kubeovn/kube-ovn/commit/37ed257fb1e7a66495ef3ce90202c53922b562b8) fix: get_leader_ip always return fist node ip
 * [548a5c55](https://github.com/kubeovn/kube-ovn/commit/548a5c556ac6bab504c806a91e60742d26109c56) fix: do not gc learned routes
 * [4e8a7c99](https://github.com/kubeovn/kube-ovn/commit/4e8a7c99871faf286716a0626ccff7e1cf0e6a2d) fix: remove tty error notification
 * [9e060882](https://github.com/kubeovn/kube-ovn/commit/9e060882b7219b904a3d06782c574b28eb1d506b) fix ovn nb reconnect
 * [1b35390f](https://github.com/kubeovn/kube-ovn/commit/1b35390fd1cbd7bbf405346d08ead499828f0b34) perf: reclaim heap memory after compaction.
 * [703174a8](https://github.com/kubeovn/kube-ovn/commit/703174a82a5bc27a48b7c7a2fb9b1e0a811595e1) fix: leader may change during startup, use cluster connection to set options.
 * [14de53e7](https://github.com/kubeovn/kube-ovn/commit/14de53e7357302c8243f7477648e0996997acdf7) fix SNAT on pod startup

### Contributors

 * Mengxin Liu
 * Yan Zhu
 * caoyingjun
 * chestack
 * zhangzujian
 * 马洪贞

## v1.6.2 (2021-04-18)

 * [2f421181](https://github.com/kubeovn/kube-ovn/commit/2f42118173983b94e82023b274850487bd144f05) release 1.6.2
 * [23c9240d](https://github.com/kubeovn/kube-ovn/commit/23c9240dce812ecec1183b6b6b433d8f648cfc61) fix: configure nic failed when ifname empty
 * [6574447f](https://github.com/kubeovn/kube-ovn/commit/6574447f6082538ec1571fd58242b41480b7bb8e) remove extra space
 * [b65d41ad](https://github.com/kubeovn/kube-ovn/commit/b65d41ade4a4bbd42703877b19a43ed98d069946) fix chassis check for node
 * [bec0d0f4](https://github.com/kubeovn/kube-ovn/commit/bec0d0f42a8623ce3d3a855c98edcce6abc4f7e1) fix: compatible with no norhtd svc
 * [ef76fcc0](https://github.com/kubeovn/kube-ovn/commit/ef76fcc06ffe2732106c1dc891c5f2ad44f637c7) fix: release norhtd lock when power off
 * [fefcff27](https://github.com/kubeovn/kube-ovn/commit/fefcff2726e1a3e13f8240b2ffc41df93f4c8fae) fix: disable offload if geneve port exists
 * [a1679923](https://github.com/kubeovn/kube-ovn/commit/a167992376bf4ff282cfe77b26d873083e0e367a) disable offload for genev_sys_6081
 * [12e6b0b1](https://github.com/kubeovn/kube-ovn/commit/12e6b0b182c37d5c2e2fe8cb4ccf4e9fb80ecfd9) rebuild to fix openssl cve
 * [a5862310](https://github.com/kubeovn/kube-ovn/commit/a58623107b68ff912713eab97593d4d0aeba4842) fix: check ovn0 status
 * [03956f1f](https://github.com/kubeovn/kube-ovn/commit/03956f1fc42555d8fa74aa3e563b9d510e01c807) ignore ip6tabels check for v4 hostIP
 * [35f06495](https://github.com/kubeovn/kube-ovn/commit/35f0649537d2d6a79b7f775635c42026b64d735f) livenessprobe fail if ovn nb/ovn sb not running
 * [3f15c923](https://github.com/kubeovn/kube-ovn/commit/3f15c9238da1c2444c8b3e18394c6231f4f1a636) fix: disable checksum offload for ovn0 to prevent kernel issue
 * [54f5102d](https://github.com/kubeovn/kube-ovn/commit/54f5102dd9d08e58baeb3c30b201be9669cd9755) add node address allocate check when init
 * [07bea935](https://github.com/kubeovn/kube-ovn/commit/07bea9354c55c9aa0e9d21059c675d19c36f4b0a) update arm64 build
 * [995022e6](https://github.com/kubeovn/kube-ovn/commit/995022e6a8d00364d7133a079ee6dca902b87446) fix: restart ovn-controller to force update flows
 * [21c312c0](https://github.com/kubeovn/kube-ovn/commit/21c312c01508dcd4b91aef1308864bd3ce46c39b) fix: disable checksum validation
 * [73bb2d83](https://github.com/kubeovn/kube-ovn/commit/73bb2d83b20685743332c5dde638fd802dd8d9cd) update usingips check when update finalizer for subnet

### Contributors

 * Mengxin Liu
 * danieldin95
 * halfcrazy
 * hzma
 * lut777

## v1.6.1 (2021-03-09)

 * [87e11481](https://github.com/kubeovn/kube-ovn/commit/87e114817fced318b64a79f9bf8f82a048447210) fix: add missing ovn-ic binary
 * [dbf53f6e](https://github.com/kubeovn/kube-ovn/commit/dbf53f6e2b9d20bc76d138c46cedf87c2b0918de) release for 1.6.1
 * [2dcd7584](https://github.com/kubeovn/kube-ovn/commit/2dcd7584b1ae7100ddcee2b194c441e4d3b0b86b) fix: delete chassis_private when delete node
 * [f8aeb887](https://github.com/kubeovn/kube-ovn/commit/f8aeb887a007a31045b2c3fce9eb85817d9d9fe7) chore: update ovn to 20.12 ovs to 2.15
 * [35190e1c](https://github.com/kubeovn/kube-ovn/commit/35190e1c197d2896a9491755ee02bc4c096c1bad) refactor: reduce duplicated function
 * [afe9a9f0](https://github.com/kubeovn/kube-ovn/commit/afe9a9f05d60b860dba32e0eb572058b3a0ebcc6) fix: masq traffic to ovn0 from other nodes
 * [96880905](https://github.com/kubeovn/kube-ovn/commit/96880905a68d5a403d710ba8dc000b6ff5338ea6) ovn-central: set default db addr same with leader node to fix nb and sb error 'bind: Address already in use'
 * [cce2bb4d](https://github.com/kubeovn/kube-ovn/commit/cce2bb4d4fce58a2ed09b49d45a72d95fe0f86de) fix: reset ovn0 addr
 * [8152bdf5](https://github.com/kubeovn/kube-ovn/commit/8152bdf5e0615e3238a4763449e41b8d01ff6ebe) Fix: wrong split in FindLoadbalancer function
 * [33b0e186](https://github.com/kubeovn/kube-ovn/commit/33b0e186d3bd8d3ba6452dfcc24574492537eb8f) fix underlay gateway flood logs
 * [9a8e7870](https://github.com/kubeovn/kube-ovn/commit/9a8e7870007aedcc3a4e5b3cf6429af317f9c66a) fix: check required module before start
 * [b70f6103](https://github.com/kubeovn/kube-ovn/commit/b70f6103f3ab308fe940c65de6682662a012570a) fix: make sure northd leader change
 * [ecbd43e2](https://github.com/kubeovn/kube-ovn/commit/ecbd43e2a4205896c7b9d45e63faf5e0a1319c07) fix: restart when init ping failed
 * [4b752988](https://github.com/kubeovn/kube-ovn/commit/4b752988e7960452195380929c1ae9fa3d2555cf) fix pod terminating not recycle ip when controller not ready
 * [0e794679](https://github.com/kubeovn/kube-ovn/commit/0e79467975372a51e19e515977f2dde2797f8184) fix: add new iptable cleanup commands
 * [cf725882](https://github.com/kubeovn/kube-ovn/commit/cf725882c7f450b40e5f82dfc6214d84345f9d8e) Fix wait pod network ready take long time
 * [bbb7edc6](https://github.com/kubeovn/kube-ovn/commit/bbb7edc630544e906104d4132cd4e7a6fcc04394) fix: when address is empty, skip route/nat deletion
 * [7121fa80](https://github.com/kubeovn/kube-ovn/commit/7121fa801fe760520a3161799d7142801e5fc102) fix: update ipam cidr when subnet changed
 * [99d8981f](https://github.com/kubeovn/kube-ovn/commit/99d8981f91b18b6a539ea34866edba736b189528) prepare for 1.6.1
 * [8559014f](https://github.com/kubeovn/kube-ovn/commit/8559014f7950981623c8ad039298debbf08583aa) move build dependency from alauda to kubeovn
 * [9184aa93](https://github.com/kubeovn/kube-ovn/commit/9184aa939d387af3e7996e1536356854ca3a37ff) update upgrade for ovn-default and join subnet
 * [f11c6b3c](https://github.com/kubeovn/kube-ovn/commit/f11c6b3c21b4b24358aba4114f42564ac8375d70) update subnet ip num calculate
 * [e5e6e302](https://github.com/kubeovn/kube-ovn/commit/e5e6e302b40b35b7936897c549e8216f8112d7c3) fix: ip6tables check error
 * [23dcd2a3](https://github.com/kubeovn/kube-ovn/commit/23dcd2a35a5769768340ccadc3fcff63449680bf) delete unused import packet
 * [5ead6b1d](https://github.com/kubeovn/kube-ovn/commit/5ead6b1d4bb120a41cd7ee4ad7f6b665127688b6) filter repeat exclude ips
 * [30217437](https://github.com/kubeovn/kube-ovn/commit/30217437d26769cb82f85eabac07a5e41a4ee9a0) modify ip count for dual
 * [b4560b99](https://github.com/kubeovn/kube-ovn/commit/b4560b99c5df7d351390a5156e45392dc7f4ff7a) modify test problem
 * [b4b55581](https://github.com/kubeovn/kube-ovn/commit/b4b55581b6b827c5b1d84a09d3483d0d827e1082) add vpcs && vpcs/status clusterRole
 * [d6f14147](https://github.com/kubeovn/kube-ovn/commit/d6f14147a7f07ce93c5ead2e953f1f547cded778) delete connect to ovsdb for ovn-monitor
 * [98859f9b](https://github.com/kubeovn/kube-ovn/commit/98859f9b2cc502ae4835ae01ceb0f7be3536bdac) modify static gw changed problem
 * [255e20c6](https://github.com/kubeovn/kube-ovn/commit/255e20c699a7032af6a90eab4213bac834c4b36d) modify test problem for dual-stack upgrade

### Contributors

 * Mengxin Liu
 * Wan Junjie
 * Yan Zhu
 * cmj
 * hzma
 * wangyudong
 * xieyanker

## v1.6.0 (2021-01-04)

 * [d47ccb67](https://github.com/kubeovn/kube-ovn/commit/d47ccb678692e441a774d11477269a4c4e430544) release: 1.6.0
 * [b8f221bf](https://github.com/kubeovn/kube-ovn/commit/b8f221bf7d47b2190acfd716878e1b5aa441a409) docs: add docs for vpc
 * [12cf140b](https://github.com/kubeovn/kube-ovn/commit/12cf140b167755bcb7a29981f5962ff369689694) fix typo
 * [b13cb7bf](https://github.com/kubeovn/kube-ovn/commit/b13cb7bf8f34516a9fe9cf64eb0d56b14644c7d1) ci: update go version to 1.15
 * [7f9eefed](https://github.com/kubeovn/kube-ovn/commit/7f9eefedb1267d9d18059c638b23361d1c198891) Fix: replace the command to run the script via 'sh' with 'bash'
 * [076ab28f](https://github.com/kubeovn/kube-ovn/commit/076ab28f80c46826c5237da406eb18eb38d4bb54) Fix the default mtu parameter's describe
 * [8e608667](https://github.com/kubeovn/kube-ovn/commit/8e6086678b923eb032a8b95d13a9bf214b1f38e8) modify network policy process
 * [171dcff6](https://github.com/kubeovn/kube-ovn/commit/171dcff6dd3b27899e06ae752f8fa34896b159de) upgrade for subnet from single protocol to dual-stack
 * [bbc68577](https://github.com/kubeovn/kube-ovn/commit/bbc68577b91ab26cdec5208c02dc165fe73a8222) add network policy adapt for dual-stack
 * [c01766cf](https://github.com/kubeovn/kube-ovn/commit/c01766cfaef9554b6acbb435d81050951c97a1de) feat: update ovn to 20.09
 * [315831aa](https://github.com/kubeovn/kube-ovn/commit/315831aa0f5baacac396266d009f746565b79db0) docs: prepare docs for 1.6.0 release
 * [a1e7974f](https://github.com/kubeovn/kube-ovn/commit/a1e7974fa950420d5e2520942335f6284f161bdc) perf: add pprof to pinger
 * [627956e9](https://github.com/kubeovn/kube-ovn/commit/627956e95e2c440b4b75709dfdc4e33050209815) doc for dual-stack
 * [02751bf4](https://github.com/kubeovn/kube-ovn/commit/02751bf42d196e6e5542b5284953a554eb83e857) Update the container nic name use the CNI_IFNAME parameter which passed by kubelet
 * [14f36814](https://github.com/kubeovn/kube-ovn/commit/14f36814f7899402f62ef85941ac066b1f2312dc) ci: enable docker experimental feature
 * [9a785fc9](https://github.com/kubeovn/kube-ovn/commit/9a785fc9eeb744ddcc3d8eb4d98cd419ca26910b) ci: build multi arch image
 * [03ff96e6](https://github.com/kubeovn/kube-ovn/commit/03ff96e66b8316348fa50ac7371cc27616464caa) <fix>(np) fix mulit np rule and gateway bug
 * [20f3fcb1](https://github.com/kubeovn/kube-ovn/commit/20f3fcb178c0bf300ad6b792de53cc9aab9218fd) fix start-db.sh echo message
 * [52b39d76](https://github.com/kubeovn/kube-ovn/commit/52b39d764dacece2dec406db485e46181f3bd7d3) fix: iface check error
 * [072870b1](https://github.com/kubeovn/kube-ovn/commit/072870b16e6858d3690b10db975b2f28da7e7b7b) fix: add missing ping due to deb build
 * [efdd3913](https://github.com/kubeovn/kube-ovn/commit/efdd3913b22a2bed4a98b8882897403869fc82aa) fix: find iface by full match first then regex match
 * [f922ef75](https://github.com/kubeovn/kube-ovn/commit/f922ef752883131d5d70abdaa8a2bb4b3235ef32) fix: livenessProb/readinessProb might conflict when run logrotate at same time
 * [f1fe2b2e](https://github.com/kubeovn/kube-ovn/commit/f1fe2b2ead442a58331efe8fb1b47ac7bc4858f6) modify subnet and ip crds
 * [a2d76df7](https://github.com/kubeovn/kube-ovn/commit/a2d76df7c0fea1b64289024a4cf637ef8657e2c2) modify service vip parse error
 * [8aa5d0a4](https://github.com/kubeovn/kube-ovn/commit/8aa5d0a4f978a85a460fe2adcc05b4f6b1a39dd5) update vendor
 * [44381c74](https://github.com/kubeovn/kube-ovn/commit/44381c74e23437b03fdd31ce6b30f0f5a2c29005) update client-go
 * [96c1c100](https://github.com/kubeovn/kube-ovn/commit/96c1c1003535a87166bbf1cf6fbccc9a99a99cc6) fix: np with multiple rules
 * [87e6ded0](https://github.com/kubeovn/kube-ovn/commit/87e6ded0411e1a27bc18b0d807c5eacff028bb58) modify loop error for get metrics
 * [1e2a7477](https://github.com/kubeovn/kube-ovn/commit/1e2a747749b8a702cf59304f00d1549778ff0e34) diagnose: add more diagnose info
 * [aea12bae](https://github.com/kubeovn/kube-ovn/commit/aea12bae5a744b1b6c688ed5343cfab729dcd802) ci: trigger action when yamls change
 * [7bd6bf39](https://github.com/kubeovn/kube-ovn/commit/7bd6bf39f793698e1b429018fb7a1b10cb19e192) fix: ha e2e failed
 * [56774aaf](https://github.com/kubeovn/kube-ovn/commit/56774aafd2d98d4589b2e4fd18ea758b7e6cf66f) fix: allow traffic to gateway
 * [a78c2661](https://github.com/kubeovn/kube-ovn/commit/a78c2661bdc559e6b00521ae7ee62399ac633f05) fix: cni-server default encap ip use right interface ip
 * [7d31e617](https://github.com/kubeovn/kube-ovn/commit/7d31e617a42719e68e338d60a278774e51f5146b) feat: change default build image to ubuntu
 * [e2cd7871](https://github.com/kubeovn/kube-ovn/commit/e2cd7871583f812271089c5002743460246e6242) add build for dualstack
 * [ddda6332](https://github.com/kubeovn/kube-ovn/commit/ddda633204486877d8176476d1bd0470a84c3ecc) feat: distributed eip
 * [a6fef94a](https://github.com/kubeovn/kube-ovn/commit/a6fef94a823be183eb80ca14be4703300e2c5add) Add CNI modify for dualstack
 * [a54bfc28](https://github.com/kubeovn/kube-ovn/commit/a54bfc2840f84d6fcad315a7ba0fd05ded6c7d12) Debian: Add debian docker image support
 * [8a01cb1c](https://github.com/kubeovn/kube-ovn/commit/8a01cb1c211e3becefc46262c0fc78257be47b02) Add adaption for dualstack, part of daemon process.
 * [9738af18](https://github.com/kubeovn/kube-ovn/commit/9738af184b7604013ef166a2fdf6ec1043d624a9) chore: reduce binary size
 * [6483d6e3](https://github.com/kubeovn/kube-ovn/commit/6483d6e330cb4f46a9e260d0d65add82c757ab91) modify build problem
 * [dab50b33](https://github.com/kubeovn/kube-ovn/commit/dab50b33d5c7ac8e51ccb470a257ba4bb3a332fd) Append ip monitor to document
 * [34428819](https://github.com/kubeovn/kube-ovn/commit/344288195ea1a74df7f0b87fd32fed517b59fc89) license: fix felix dir
 * [2ef66568](https://github.com/kubeovn/kube-ovn/commit/2ef6656832d1c091acd33d83472efef7e871c886) feat: support advertise subnet route
 * [ecbd01a6](https://github.com/kubeovn/kube-ovn/commit/ecbd01a65546239c2786e0fe00b611f0c17fbb01) Add IP Num Alert
 * [d64e6931](https://github.com/kubeovn/kube-ovn/commit/d64e693178ce90a3dcec71a2ecfefb40b411ec4a) Add adaption for dualstack, part of controller process.
 * [7246037b](https://github.com/kubeovn/kube-ovn/commit/7246037b16227deb35d4e675d056de519d432228) convert ip to string
 * [2aecb3d9](https://github.com/kubeovn/kube-ovn/commit/2aecb3d9b3890b7d16dad0d17e5cb7d2c80699dd) add pod static ip validate
 * [b58e01b6](https://github.com/kubeovn/kube-ovn/commit/b58e01b6185ccccb02ca722e2a6f25498cbe60a9) chore: add COC and roadmap
 * [7bbdc00f](https://github.com/kubeovn/kube-ovn/commit/7bbdc00f29e9011731d0f72b900afb5d8f77b3eb) fix: move felix to self repo to remove bird license
 * [d2b570cf](https://github.com/kubeovn/kube-ovn/commit/d2b570cfbdd6b62314c4b646538305decaedf60a) Add license scan report and status
 * [86584b95](https://github.com/kubeovn/kube-ovn/commit/86584b95d7ea4498ac61b026b46fa004c459823c) fix: default network
 * [ccea68bf](https://github.com/kubeovn/kube-ovn/commit/ccea68bfa249b8d382c21e7133af4e84c6288d5b) release for 1.5.2
 * [07347501](https://github.com/kubeovn/kube-ovn/commit/07347501aa62d191fb08824bf76cdbe1c7590f58) fix: ovn-ic support ssl
 * [4d8b186a](https://github.com/kubeovn/kube-ovn/commit/4d8b186a35da4fcd0dc6d0f0c5959d292311061b) fix: nat rules can be modified
 * [f535460f](https://github.com/kubeovn/kube-ovn/commit/f535460ff720851bee6586840a786b9d1cd23d1a) fix: remove svc cidr routes
 * [e3082cd7](https://github.com/kubeovn/kube-ovn/commit/e3082cd7727ffd47aeae6af0ea47dabbd397a80e) ci: specify ubuntu version to make github action happy
 * [f6cce9a0](https://github.com/kubeovn/kube-ovn/commit/f6cce9a0afabe91904cd2cd13a9733c877cdd9e3) fix: specify exec container to mute warning message
 * [2215c05f](https://github.com/kubeovn/kube-ovn/commit/2215c05f45f9f42c0fffe57a342d73d01f5da103) feat: remove cluster ip dependency for ovn/ovs components
 * [a9747b31](https://github.com/kubeovn/kube-ovn/commit/a9747b3161f51844cf3a052c6c589c0d4f580e9d) fix: add resources limits to avoid eviction
 * [00571196](https://github.com/kubeovn/kube-ovn/commit/005711961cf3b5cc1a11c06558d1b0b9f21d69ba) fix: vpc static route manage
 * [8deb5d8d](https://github.com/kubeovn/kube-ovn/commit/8deb5d8d05babbe20881f492a775fc86d08b7b8d) fix: validate vpc subnet
 * [256ac6c5](https://github.com/kubeovn/kube-ovn/commit/256ac6c5350c5e8a67657a06872928c86b491363) Fix external-address config description
 * [ccda611a](https://github.com/kubeovn/kube-ovn/commit/ccda611a5c3d8cc196bfeaab1985350f0168b7d9) Fix the problem of confusion between old and new versions of crd
 * [f2f64801](https://github.com/kubeovn/kube-ovn/commit/f2f6480112272ce221bf0bd4da3010633a15a541) fix: ovn-central check if it exits in NODE_IPS
 * [5b973a89](https://github.com/kubeovn/kube-ovn/commit/5b973a89e501dd2e1c27cc0f78ca7267322d2827) fix: check ipv6 requirement before start
 * [86941a8a](https://github.com/kubeovn/kube-ovn/commit/86941a8a87885765ceaafa5d9a07cf100723004e) feat: add ovs/ovn log rotation
 * [ef41733c](https://github.com/kubeovn/kube-ovn/commit/ef41733c558ffd68c7b7ec79c91f93afbef727de) add node ping total count metric
 * [5e6bd911](https://github.com/kubeovn/kube-ovn/commit/5e6bd9112b69ea1928830084961d4b3286381ccd) diagnose: add ovs-vsctl show to diagnose results
 * [7301e992](https://github.com/kubeovn/kube-ovn/commit/7301e992665c6b18f47384ddc1e0fb36c07e8274) fix: nat rules
 * [6026028a](https://github.com/kubeovn/kube-ovn/commit/6026028a48bdffb9252af001e3df110479287fa2) fix: masq other nodes to local pod to avoid nodrport triangle traffic
 * [d41110ec](https://github.com/kubeovn/kube-ovn/commit/d41110ec10d8887c44c707e51dd4b1a8c6823221) Update install.sh to allow dpdk limits configuration (#546)
 * [a128d7fc](https://github.com/kubeovn/kube-ovn/commit/a128d7fcc4a752f7deffa8aeb3d6c26bbf0eb76f) format
 * [b6ad17b5](https://github.com/kubeovn/kube-ovn/commit/b6ad17b537389e64f306d4837538b4c0d0ef0d59) test: e2e uses IPVS cluster by default
 * [f6951cf5](https://github.com/kubeovn/kube-ovn/commit/f6951cf5d98db035df3471364bc396682c87f703) chore: update go version to 1.15
 * [1f703c3d](https://github.com/kubeovn/kube-ovn/commit/1f703c3d3f49f887de951b4fe0b057a44e7f1fe6) fix: tolerate all taints
 * [f8ace73c](https://github.com/kubeovn/kube-ovn/commit/f8ace73c190930b21ce005c8a7ad9a3d4b0ace7d) feature: add vpc static route
 * [f62cb4eb](https://github.com/kubeovn/kube-ovn/commit/f62cb4ebfe52ef3de8f52b2f5c84acaee74e705b) fix: cleanup script error
 * [3bac21f7](https://github.com/kubeovn/kube-ovn/commit/3bac21f7ecaa674eaa386e8d55f010a20aaac101) docs: modify eip config description
 * [1f07d96b](https://github.com/kubeovn/kube-ovn/commit/1f07d96b7081b0496fb339635385f09f069dc6de) security: remove sqlite to mute cve warning
 * [015bc625](https://github.com/kubeovn/kube-ovn/commit/015bc6259b7cb9e32bca94fa3a781371cf6a6c0a) test: add e2e for kubectl-ko
 * [aa86e406](https://github.com/kubeovn/kube-ovn/commit/aa86e406fe17f9cf9c76803514e7b4c373e3bb8b) feat: pinger can return exit code when failed
 * [2cf855ec](https://github.com/kubeovn/kube-ovn/commit/2cf855ecd7b4550fc4a273aeac7c3eb9d6641aed) fix: nat traffic that from host to svc
 * [cbe0ad55](https://github.com/kubeovn/kube-ovn/commit/cbe0ad55f41c47a4d8e5f963802e7051f4561ab6) docs: new feat for disable-ic, regex iface and pod bind subnet
 * [5dbaf2d3](https://github.com/kubeovn/kube-ovn/commit/5dbaf2d3a528e0bf10febcb9877bea7d51bcd003) sync the default subnet of ns by vpc's status
 * [dd2234f4](https://github.com/kubeovn/kube-ovn/commit/dd2234f4e65d8fd31f2cad84aa055d12b3ae46e9) fix: devault vpc lb/dns
 * [32c49c1b](https://github.com/kubeovn/kube-ovn/commit/32c49c1b93842130e60f74dd477aad0aad9c5a30) fix: shutdown vpc workqueue
 * [67076d62](https://github.com/kubeovn/kube-ovn/commit/67076d62f0e3f26af1a864678e660edadfaf5464) fix: subnet CIDRConflict
 * [d5b819b0](https://github.com/kubeovn/kube-ovn/commit/d5b819b03710f149ae4c8d66687e87ea09589827) fix: subnet bind to ns
 * [921190ef](https://github.com/kubeovn/kube-ovn/commit/921190ef8534b0d591e9f89c6eb9ca4b07d2fbc8) feature: add vpc crd
 * [b5ecac95](https://github.com/kubeovn/kube-ovn/commit/b5ecac95cbb27b39250f870dd6e6c885ca7dae79) Release and gc the resources in vpc
 * [15eca9dc](https://github.com/kubeovn/kube-ovn/commit/15eca9dca39aa497cc3670bcb453aff9d020acdc) fix: gc logic router
 * [91fec563](https://github.com/kubeovn/kube-ovn/commit/91fec5631ca0ecbdd3d677282a25e8029169ecad) gc and clean vpc
 * [7a0e28b9](https://github.com/kubeovn/kube-ovn/commit/7a0e28b98bfcbf90ee6766bf89a72d85df51428c) Remove the VPC while removing the default subnet
 * [99217cec](https://github.com/kubeovn/kube-ovn/commit/99217cec142f3e6943280e34a0be85926664ae7c) feature: support custom vpc
 * [9d821bce](https://github.com/kubeovn/kube-ovn/commit/9d821bce095b59b17cee1d86f365fa5032d74fcf) chore: refactor log
 * [240cd800](https://github.com/kubeovn/kube-ovn/commit/240cd800a7545356d723f28c09e3cfdee5d8fe87) feat: iface support regexp
 * [94b6b1b5](https://github.com/kubeovn/kube-ovn/commit/94b6b1b59b089023864a70bf5c55334926a0abdf) feat: support disable interconnection for specific subnet
 * [652190c3](https://github.com/kubeovn/kube-ovn/commit/652190c359fa7bf0f37db317dc4f4680e70c1fb5) modify review problems
 * [7285581a](https://github.com/kubeovn/kube-ovn/commit/7285581a535b3e9207d1a8231fba9e7fa852d4cc) docs: v1.5.1 changelog
 * [47f0acbb](https://github.com/kubeovn/kube-ovn/commit/47f0acbbd9ca240abb2946b6a5d39d5fa271c0bd) perf: accelerate ic and ex gw update
 * [bafac87e](https://github.com/kubeovn/kube-ovn/commit/bafac87ee1c885dfbefeee26db8fc4d5364ef835) fix: missing version date
 * [8ef12007](https://github.com/kubeovn/kube-ovn/commit/8ef12007cc3b77612ba132fef4c0864a8aa92ec6) fix: check multicast and loopback subnet
 * [3b20abb0](https://github.com/kubeovn/kube-ovn/commit/3b20abb0e7770d6cf6722f144b4b307ab5caac82) monitor: refactor grafana dashboard
 * [f9cbaea5](https://github.com/kubeovn/kube-ovn/commit/f9cbaea5b3ea9adc6744064fde8b5841fc51c0d0) docs: do not allow install to namespace other than kube-system
 * [559e2cd8](https://github.com/kubeovn/kube-ovn/commit/559e2cd8cbe25370b6745b90787781d147959a29) update review problems for ovn_monitor
 * [1c356a36](https://github.com/kubeovn/kube-ovn/commit/1c356a3659885d38cf1ac840b6e6d7e237f99967) monitor: add more dashboard
 * [aa7b20d7](https://github.com/kubeovn/kube-ovn/commit/aa7b20d75a0ff848cf99439ac36ce48d840fdb5d) chore: add vendor
 * [97d64f93](https://github.com/kubeovn/kube-ovn/commit/97d64f9342d77463fd7476d9511f4e924303ff8e) Updated Dockerfile.dpdk1911 to use Centos8 and DPDK19.11.4
 * [b4aa989d](https://github.com/kubeovn/kube-ovn/commit/b4aa989da6b93012e0c17410547cdb85970a4331) fix: CodeQL scan warning
 * [a27e1760](https://github.com/kubeovn/kube-ovn/commit/a27e176039549b00d8d914c387f565013f5315d3) fix: ipt wrong order and add cluster route
 * [9eb96dd7](https://github.com/kubeovn/kube-ovn/commit/9eb96dd788d3d5fbbabbadb9a867257e719a8be5) opt: only allow specifies default subnet
 * [0da634e8](https://github.com/kubeovn/kube-ovn/commit/0da634e8fe592d16223257cd67cc5248331a21aa) chore: reduce image size
 * [93bf5423](https://github.com/kubeovn/kube-ovn/commit/93bf5423534d8f775be65b9fd7252d6a05677879) feature: Support for namespace binding multiple subnets
 * [e37159c2](https://github.com/kubeovn/kube-ovn/commit/e37159c23dea126b91aae598ce03c67fcc23935f) docs: fix multi nic subnet options
 * [c35a159b](https://github.com/kubeovn/kube-ovn/commit/c35a159b974e321432e562b94a82ebd324271e7d) docs: add pinger/controller/cni metrics
 * [7f5b4237](https://github.com/kubeovn/kube-ovn/commit/7f5b423742d743ff4a9213df19c685f49c3532f6) fix: add default ssl var for compatibility
 * [59b70696](https://github.com/kubeovn/kube-ovn/commit/59b706964218988a5d6e5fb7623436a3a8a831df) Add monitor doc
 * [bb130cac](https://github.com/kubeovn/kube-ovn/commit/bb130cac2cc0c2cc961f94750a72bc510d7d78fe) fix: ipv6 network format when update subnet
 * [dc62d105](https://github.com/kubeovn/kube-ovn/commit/dc62d10506a7a9f9ff2f2f9d8b6514d14b3d008f) fix: ipv6 len mismatch
 * [6088851d](https://github.com/kubeovn/kube-ovn/commit/6088851d49c5d21f37117a85c8e3a3b69b6a37f1) chore: add version info
 * [88001376](https://github.com/kubeovn/kube-ovn/commit/88001376db58d8640ac67e294b00a25c57613cea) metrics: add ovs client latency metrics
 * [3cafd5f8](https://github.com/kubeovn/kube-ovn/commit/3cafd5f8a5d57c3f77ee9ecd0b0ec4bbcbee09aa) Add OVN/OVS Monitor
 * [89567776](https://github.com/kubeovn/kube-ovn/commit/89567776afcc203a4ed4aac76468d7b84e5968ce) docs: performance test method
 * [0c975e34](https://github.com/kubeovn/kube-ovn/commit/0c975e343bae483882d36b4c8480337fb6c971c8) fix: wrong port porto for udp
 * [f3759b78](https://github.com/kubeovn/kube-ovn/commit/f3759b78e0d186fac611a2637b79317b63d3c7e4) docs: add descriptions of local files
 * [b46acd6c](https://github.com/kubeovn/kube-ovn/commit/b46acd6c38f136ee0ca3e9f53265f239480cab81) ci: add github code scan
 * [2444d51a](https://github.com/kubeovn/kube-ovn/commit/2444d51aefb075356b7b6665c593ffdfb83f19db) fix: do not adv join cidr when enable ovn-ic
 * [292bf4ca](https://github.com/kubeovn/kube-ovn/commit/292bf4caf51e2acba1226d64504a2c16300d0cb2) perf: remove default acl rules
 * [20e82c39](https://github.com/kubeovn/kube-ovn/commit/20e82c39388c7dcefebff4e6bb3ba720b3df5fb2) prepare for next release
 * [9324491c](https://github.com/kubeovn/kube-ovn/commit/9324491cc02ee4e7b798f565f13bf39d52969205) fix: use internal IP when node connect pod
 * [c1870c1a](https://github.com/kubeovn/kube-ovn/commit/c1870c1acda04d567d7aa2c40fa0bd3f0bdbeadb) ci: change to docker buildx action
 * [a1976650](https://github.com/kubeovn/kube-ovn/commit/a1976650c25566fe37bc6274cf2f7c3db95dab47) fix: delete pod when marked with deletionTimestamp
 * [c3c4f1c5](https://github.com/kubeovn/kube-ovn/commit/c3c4f1c5b00b62e7475e5defacac14adbb3bda07) fix: remove not alive pod in pg

### Contributors

 * Emma Kenny
 * Mengxin Liu
 * MengxinLiu
 * Wan Junjie
 * emmakenny
 * feixiang
 * fossabot
 * hzma
 * luoyunhe1
 * wiwen
 * xieyanker
 * 范日明

## v1.5.2 (2020-12-01)

 * [498d74d7](https://github.com/kubeovn/kube-ovn/commit/498d74d7bf79de3f233e5fd43bed07fae651ecb5) release for 1.5.2
 * [271c07bd](https://github.com/kubeovn/kube-ovn/commit/271c07bd9e8cf4d6e8b81b8214f4b0e20a359f39) fix: nat rules can be modified
 * [21a5edbd](https://github.com/kubeovn/kube-ovn/commit/21a5edbd59c7187023433a0e22d30215b4e6a182) fix: add resources limits to avoid eviction
 * [762f1c21](https://github.com/kubeovn/kube-ovn/commit/762f1c21fea3c105bf966df5c599af896208ccfd) ci: specify ubuntu version to make github action happy
 * [bd4019dd](https://github.com/kubeovn/kube-ovn/commit/bd4019ddc77a482e41f8addcff2c713ed8fa531e) fix: remove svc cidr routes
 * [93a89753](https://github.com/kubeovn/kube-ovn/commit/93a8975393e153b6176a5d102e811bcb5eec27cc) Fix the problem of confusion between old and new versions of crd
 * [031f5436](https://github.com/kubeovn/kube-ovn/commit/031f54368a1f0347e649d4c142c4f165f2715505) Fix external-address config description
 * [3371ce4c](https://github.com/kubeovn/kube-ovn/commit/3371ce4c076ad700fe3324dad5dfd22a33f7cce7) fix: ovn-central check if it exits in NODE_IPS
 * [cf4c4127](https://github.com/kubeovn/kube-ovn/commit/cf4c41279974ae28ff6b6f7340e9b66b81a3229b) fix: check ipv6 requirement before start
 * [186d90cd](https://github.com/kubeovn/kube-ovn/commit/186d90cdb6408eff64bdfb6731398d3b29e2ce7f) feat: add ovs/ovn log rotation
 * [b5dfc1c6](https://github.com/kubeovn/kube-ovn/commit/b5dfc1c65148a608fbc14f9fabbee06a43cc6b58) diagnose: add ovs-vsctl show to diagnose results
 * [37cbb713](https://github.com/kubeovn/kube-ovn/commit/37cbb713ed95d3b03b30ebbf75f2c34d8a5067f6) add node ping total count metric
 * [6ed020c2](https://github.com/kubeovn/kube-ovn/commit/6ed020c246352fc2bbc3c735da458c7e14c6e441) fix: tolerate all taints
 * [1a4f48a0](https://github.com/kubeovn/kube-ovn/commit/1a4f48a09844d4c65fecf88b439d027221de041f) chore: update go version to 1.15
 * [e0fc3331](https://github.com/kubeovn/kube-ovn/commit/e0fc3331683614711429e72f6849d605f62084fc) fix: masq other nodes to local pod to avoid nodrport triangle traffic
 * [f6ff2780](https://github.com/kubeovn/kube-ovn/commit/f6ff27805e83e7394d35a8540ab01291163e6db7) Update install.sh to allow dpdk limits configuration (#546)
 * [96636386](https://github.com/kubeovn/kube-ovn/commit/9663638607a23c6aa67c5b7fbda53a127c11b6ce) prepare for 1.5.2
 * [06d8b374](https://github.com/kubeovn/kube-ovn/commit/06d8b374eb621f77ddb25638f32168158c13b0f6) fix: cleanup script error
 * [5ddf72b2](https://github.com/kubeovn/kube-ovn/commit/5ddf72b241cc45039572289a4fdb080569ea81e1) security: remove sqlite to mute cve warning
 * [1fe42677](https://github.com/kubeovn/kube-ovn/commit/1fe42677e55bad2f945cbcb8547f88b70ae4d630) chore: refactor log
 * [0f1b74dc](https://github.com/kubeovn/kube-ovn/commit/0f1b74dc91ff053f760ed51878e6980e7fe26d99) fix: nat traffic that from host to svc
 * [24b97cb0](https://github.com/kubeovn/kube-ovn/commit/24b97cb08ac3eca129c2d968ff79b635b9029e84) feat: iface support regexp

### Contributors

 * Mengxin Liu
 * emmakenny
 * hzma
 * xieyanker

## v1.5.1 (2020-10-26)

 * [bf860e26](https://github.com/kubeovn/kube-ovn/commit/bf860e26eff8f99478c28ee3c9db8eb32ba5f14d) release 1.5.1
 * [cf96d6db](https://github.com/kubeovn/kube-ovn/commit/cf96d6dbdb0989c748c033e99169dbc0b32e5fee) opt: only allow specifies default subnet
 * [99e393ec](https://github.com/kubeovn/kube-ovn/commit/99e393ec64a18e17e1e26c16ce50b74484093e5f) feature: Support for namespace binding multiple subnets
 * [fa4006c0](https://github.com/kubeovn/kube-ovn/commit/fa4006c07eeef58a3c85049a565cb317211fc2cf) perf: accelerate ic and ex gw update
 * [c327535a](https://github.com/kubeovn/kube-ovn/commit/c327535ad8bc4eb597eb2ebdefcd2c6a27f6cf17) fix: check multicast and loopback subnet
 * [d74e2078](https://github.com/kubeovn/kube-ovn/commit/d74e2078870b7b89f7879fa9eb5537fc7ff2fb4e) fix: CodeQL scan warning
 * [df8530a3](https://github.com/kubeovn/kube-ovn/commit/df8530a3caab12a3480a9e3dd2ce636216e1de55) fix: ipt wrong order and add cluster route
 * [33afdd18](https://github.com/kubeovn/kube-ovn/commit/33afdd183004aa3d8436acbc97dbdacc735b6aed) fix: add default ssl var for compatibility
 * [f14155e4](https://github.com/kubeovn/kube-ovn/commit/f14155e460dbeaf4423aafcc1d2305aa8f9c4c23) fix: broken rpm link
 * [a99ecbee](https://github.com/kubeovn/kube-ovn/commit/a99ecbeea0c85239db2a6f8d3aeefe62b8f4f139) fix: ipv6 network format when update subnet
 * [5fbb92b0](https://github.com/kubeovn/kube-ovn/commit/5fbb92b0f6257a62285d45922d456b77e7ac8de6) fix: ipv6 len mismatch
 * [bbda6a80](https://github.com/kubeovn/kube-ovn/commit/bbda6a8069bd1862337b62069f07a6291ef67778) fix: wrong port porto for udp
 * [42b7aa12](https://github.com/kubeovn/kube-ovn/commit/42b7aa121f495c266dfd68e3b6946ef5cf975c20) fix: do not adv join cidr when enable ovn-ic
 * [34952c80](https://github.com/kubeovn/kube-ovn/commit/34952c80970882269a81d6a0e787cbd0d268cba8) perf: remove default acl rules
 * [2ad71107](https://github.com/kubeovn/kube-ovn/commit/2ad7110793deea0f9e3c1fdb4b9f176f02b4d7d7) fix: use internal IP when node connect pod
 * [c42d42f1](https://github.com/kubeovn/kube-ovn/commit/c42d42f1d7366401ded307eb0ff3779c9752b9ca) ci: change to docker buildx action
 * [ba401065](https://github.com/kubeovn/kube-ovn/commit/ba40106519c2ac36db71f336fb5fcdf594ffe49c) fix: delete pod when marked with deletionTimestamp
 * [f8a4e656](https://github.com/kubeovn/kube-ovn/commit/f8a4e6565a758a0c6c5c2dc9f14781d70b2f94a9) fix: remove not alive pod in pg

### Contributors

 * Mengxin Liu
 * 范日明

## v1.5.0 (2020-09-28)

 * [c0a34b84](https://github.com/kubeovn/kube-ovn/commit/c0a34b842eb4cf13121fec2f9f69c579019d6b84) release: prepare for release 1.5.0
 * [95548457](https://github.com/kubeovn/kube-ovn/commit/955484579832c8386a69baf3e82d231b8de7614d) perf: use podLister to optimize k8s calls
 * [6635f930](https://github.com/kubeovn/kube-ovn/commit/6635f9302c33824dc126592e722c7cc4a6a08bb0) chore: enable ssl to default ci tests
 * [5f29fc30](https://github.com/kubeovn/kube-ovn/commit/5f29fc307c78c30d22a248972b072a562a50906e) security: change ovsdb file access to 600
 * [0e0a6887](https://github.com/kubeovn/kube-ovn/commit/0e0a6887ac4c0e550aa7830b86070f18bb7bd0f8) docs: improve hw-offload
 * [a1a215dc](https://github.com/kubeovn/kube-ovn/commit/a1a215dcc72314202879833d4ab68953ea23d04b) feat: support db ssl communication
 * [e7a88c11](https://github.com/kubeovn/kube-ovn/commit/e7a88c1133857eb1d8d3d2f100dd8a9b8f9059fb) diagnose: show nb/sb/node info
 * [090624fd](https://github.com/kubeovn/kube-ovn/commit/090624fd9e00e1a2798fc3ac5671925f8dac82ee) fix: pinger diagnose should use cmd args
 * [fae393e3](https://github.com/kubeovn/kube-ovn/commit/fae393e3b491153a5e49f23eea8168e3982bab51) fix: ipv6 get portmap failed again
 * [b74189fe](https://github.com/kubeovn/kube-ovn/commit/b74189feea76dc7acac069e6d0e85699666410e1) fix: ipv6 get portmap failed
 * [f1c2f995](https://github.com/kubeovn/kube-ovn/commit/f1c2f99528f264941b50d97de0c574db2ba3aa67) fix: delay mv cni conf to when cniserver is ready
 * [98bb7510](https://github.com/kubeovn/kube-ovn/commit/98bb7510cd0c6aef5d02aa7fe7524bf5786b65fe) chore: update kind and kube-ovn-cni updateStrategy
 * [64640421](https://github.com/kubeovn/kube-ovn/commit/646404216812dfff8461a2e6e9c019ed68f86a51) monitor: add cni grafana dashboard
 * [38adc18f](https://github.com/kubeovn/kube-ovn/commit/38adc18f18cb8744b381fc67af62d297e80daa2c) monitor: add more kube-ovn-cni metrics
 * [36e9091d](https://github.com/kubeovn/kube-ovn/commit/36e9091d10ff3c6799629fbb871ed9a69a915058) feat: update pinger dashboard
 * [ab736d8f](https://github.com/kubeovn/kube-ovn/commit/ab736d8f523967ad31a43b5b0f7e566728e20c7d) fix: issues with vlan underlay gateway
 * [2e5f0ecb](https://github.com/kubeovn/kube-ovn/commit/2e5f0ecbe48bcbe6c8fa98d550d2f45ab27592f3) feat: set more metadata to interface external_ids
 * [77c4a5f2](https://github.com/kubeovn/kube-ovn/commit/77c4a5f2932c66d304d2f87332838df7d8953d63) feat: grace stop ovn-controller
 * [ebfc1530](https://github.com/kubeovn/kube-ovn/commit/ebfc1530dfaeab15127ffb0f234c9756a27cd294) refactor: fix bridge-mappings and refactor vlan code
 * [729ed3c7](https://github.com/kubeovn/kube-ovn/commit/729ed3c7a502460aeadfb0cd9785832ad44f797c) fix: allow mirror config update
 * [84bb3c83](https://github.com/kubeovn/kube-ovn/commit/84bb3c8380c28f3b323dca5b0bbc91e8a77ec66e) fix: cleanup v6 iptables and ipset
 * [da493717](https://github.com/kubeovn/kube-ovn/commit/da4937172279e42eda90eb4b8109eec263ba869a) docs: add gateway docs and optimize others
 * [ece4219f](https://github.com/kubeovn/kube-ovn/commit/ece4219f7bc54da03f9678095335b5a8b0a3accb) feat: integrate ovn sfc
 * [2b2e7a9a](https://github.com/kubeovn/kube-ovn/commit/2b2e7a9a788ee7cc11d44f7b6f821aec6f9086dc) feat: support pod snat
 * [7a60b569](https://github.com/kubeovn/kube-ovn/commit/7a60b569c9e2f431eb98c948061cb35e796bfd87) prepare for next release
 * [e9933619](https://github.com/kubeovn/kube-ovn/commit/e9933619e913f4bcd2136168faf2d78f9b007629) fix: ovn-ic-db restart failed
 * [115c1266](https://github.com/kubeovn/kube-ovn/commit/115c126684972af06f2eb0019bc25031004045f3) fix: stop ovn-ic when disabled
 * [e9861444](https://github.com/kubeovn/kube-ovn/commit/e98614444005451184e260e96ef0032ee8f9ae98) fix: use nodeName as chassis hostname

### Contributors

 * Mengxin Liu

## v1.4.0 (2020-09-01)

 * [0f973a5a](https://github.com/kubeovn/kube-ovn/commit/0f973a5ad897f2c6b70eee404772852d911bcce1) prepare for 1.4 release
 * [78ab9b1e](https://github.com/kubeovn/kube-ovn/commit/78ab9b1e2d21baa0c765529a2aebc048017af04b) fix: do not gc learned routes
 * [3ddb9614](https://github.com/kubeovn/kube-ovn/commit/3ddb9614d9e1fee9b08f1eb5de612fc23750336c) chore: add psp
 * [f847e5be](https://github.com/kubeovn/kube-ovn/commit/f847e5be666dfe7a8ff790013f539686aac56b9b) perf: apply udp improvement
 * [a8f0d228](https://github.com/kubeovn/kube-ovn/commit/a8f0d2285e44ff0c035cc149e0ead9c6b2c4d08e) chore: sync pre-1.16 install.sh
 * [0918e9a2](https://github.com/kubeovn/kube-ovn/commit/0918e9a2073c16fb78908b5aed79ae9116e768db) ci: use go 1.15
 * [f43a1027](https://github.com/kubeovn/kube-ovn/commit/f43a102729c28cc97cc9f6e900598f05f90e782f) fix: add prob timeout to wait script finish
 * [c5ca0b1b](https://github.com/kubeovn/kube-ovn/commit/c5ca0b1b70c1d56cf21daec590633210913bdc32) resolve review problem
 * [28d5a8aa](https://github.com/kubeovn/kube-ovn/commit/28d5a8aab4a75e983afb7d80bba026efa99524d0) chore: suppress verbose logs
 * [df54b0d1](https://github.com/kubeovn/kube-ovn/commit/df54b0d130394dc3be8a02932db9db170bde91b7) fix: do not gc ic logical_switch
 * [b9ab4d66](https://github.com/kubeovn/kube-ovn/commit/b9ab4d661f48ded7a9f5bc8bfb93e86ec690f8c1) fix: only gc VIF type logical_switch_port
 * [731fef99](https://github.com/kubeovn/kube-ovn/commit/731fef9924cb97ec618e266bb12078ccb1da38ec) docs: update docs
 * [e9ae40a9](https://github.com/kubeovn/kube-ovn/commit/e9ae40a98c0fa6214db4c1e7741d10c99d8f6884) chore: add back lflow reduction optimization
 * [022c7903](https://github.com/kubeovn/kube-ovn/commit/022c79037ba0adaf880b8ebf560d115885db7036) chore: update ovs to 2.14.0
 * [8e93c054](https://github.com/kubeovn/kube-ovn/commit/8e93c054e252ce89dd0bc5480b64f26a1f248f22) fix: remove duplicated gcLogicalSwitch
 * [c3b7457a](https://github.com/kubeovn/kube-ovn/commit/c3b7457a12728108e399705287c81df53ae577f7) fix: modify src-ip route priority
 * [e0096f9b](https://github.com/kubeovn/kube-ovn/commit/e0096f9b9fc7c269c2da0d2d31fc0522c46da10f) fix: missing session lb to logical switch
 * [6fbcc198](https://github.com/kubeovn/kube-ovn/commit/6fbcc198a1e7360955d8a15f839f406c8574bb32) feat: ovn-ic integration
 * [0ea62c16](https://github.com/kubeovn/kube-ovn/commit/0ea62c16375a890f4d6ea94bd1bf2f761b6a020c) fix:resolve gosec check problem
 * [b2d0393b](https://github.com/kubeovn/kube-ovn/commit/b2d0393b2eb140ecbd0e38a6a96a0b7c79e5dbe4) feat: do not perform masq on external traffic
 * [4e1ad126](https://github.com/kubeovn/kube-ovn/commit/4e1ad1260d1fa40542d2a23ba8f19f5e649aacfe) chore: fix patch failure
 * [a7c460a4](https://github.com/kubeovn/kube-ovn/commit/a7c460a48efe275bf82f6fac2f86f402f0c551c2) fix: subnet acl might conflict if allowSubnets and subnet cidr cover each other
 * [0dd85e46](https://github.com/kubeovn/kube-ovn/commit/0dd85e469e62c56ea67cc6d3c2b4bb83af67def6) feat: acl log drop packets
 * [6d048632](https://github.com/kubeovn/kube-ovn/commit/6d048632fdbbea7c4c1258b80c46b7f021b14e66) chore: remove juju log dependency
 * [9535c26b](https://github.com/kubeovn/kube-ovn/commit/9535c26ba2d65ad2c92e63a32685b6c9d7a9bf93) feat: gw switch from overlay to underlay
 * [4b095580](https://github.com/kubeovn/kube-ovn/commit/4b095580830573e4c09d0debc6c480aeff1ced29) chore: prepare for 1.4 release
 * [c9d07e1d](https://github.com/kubeovn/kube-ovn/commit/c9d07e1d5c3b23584993900807a862d11ffbf038) fix: prevent update failed logs
 * [a98ec5bd](https://github.com/kubeovn/kube-ovn/commit/a98ec5bd19393587991853dde0ff6ac191613eb4) fix: ko use external-ids to find related nic
 * [1cad39ce](https://github.com/kubeovn/kube-ovn/commit/1cad39cef27c303737c122d406ecb0fbd994d68f) fix: forward accept rules

### Contributors

 * Mengxin Liu
 * hzma

## v1.3.0 (2020-07-31)

 * [45d30713](https://github.com/kubeovn/kube-ovn/commit/45d30713d07ffff760127cf70a806bf5fc4446d9) chore: add build date
 * [c9953234](https://github.com/kubeovn/kube-ovn/commit/c995323497a1c9b0369e1b9164c9a83c5fa0a1c2) release: update 1.3.0 docs
 * [34627a66](https://github.com/kubeovn/kube-ovn/commit/34627a66953fceca9e905a53f1b642ba3a6ce687) fix: call appendMssRule function to resolved mss according problem
 * [bb961ae5](https://github.com/kubeovn/kube-ovn/commit/bb961ae5c63c07cc597a54c3bcdbb80bd2b4c002) dpdk: add kmod, pdump and proc-info tools
 * [cf47ee1b](https://github.com/kubeovn/kube-ovn/commit/cf47ee1b91db83551ae50fc3bf7a1d61dfb88236) fix: ci image tags
 * [46768179](https://github.com/kubeovn/kube-ovn/commit/46768179604d756186057d20475f637a239e51c8) chore: optimize dpdk build
 * [5c107687](https://github.com/kubeovn/kube-ovn/commit/5c1076875ef7d8d330eb08311eeb1c8df34cedc5) docs: add hw-offload docs and resolve some issues
 * [e64c6132](https://github.com/kubeovn/kube-ovn/commit/e64c61327d12b66e7c0ea0aa20949b288dcfbec1) fix: if sriov device, do not delete the host nic
 * [f55c3fba](https://github.com/kubeovn/kube-ovn/commit/f55c3fba21ba0f1f513ae834497eed76d623a17f) fix: use keymutex to serialize pod add/delete operation
 * [d438574d](https://github.com/kubeovn/kube-ovn/commit/d438574d43fd8c0d108d54fed16a94bcb0b0f8c3) feat: assign a pod as the gw
 * [1806a572](https://github.com/kubeovn/kube-ovn/commit/1806a57226d87152fead314f3df596510e3a8db9) ci: add arm build to normal ci process
 * [5aed1ef1](https://github.com/kubeovn/kube-ovn/commit/5aed1ef1ca6a28ebc68fc7ca994b0a02a7f3319d) ci: add unfixed cve
 * [19201a36](https://github.com/kubeovn/kube-ovn/commit/19201a36999041309f4eea2cf12adda266d07da4) ci: arm64 build accelerate
 * [63fbc008](https://github.com/kubeovn/kube-ovn/commit/63fbc00869c02ef62a1d861a9f297687bcd7937d) chore: add logs to sriov interface
 * [82140c93](https://github.com/kubeovn/kube-ovn/commit/82140c9313be9e50daf94f8c299674249007c4a1) ci: add ipv6 install e2e
 * [c3814c72](https://github.com/kubeovn/kube-ovn/commit/c3814c725f783aecf869f81f560fe5b463e0a86d) feat: recycle lsp at runtime
 * [3f9d7c92](https://github.com/kubeovn/kube-ovn/commit/3f9d7c928dfcfbebaff28409849bcac0e18bdeff) fix: qos error
 * [e460541d](https://github.com/kubeovn/kube-ovn/commit/e460541dd3fdc938d43b17b2c71e81e5a7d03b8c) fix: variable error
 * [de9493f2](https://github.com/kubeovn/kube-ovn/commit/de9493f2f03e7a365120a7e3a2285bff48f2753d) ci: modify cache usage
 * [1994e5c3](https://github.com/kubeovn/kube-ovn/commit/1994e5c30b1609a891b2f3222e25039e9dd41378) ci: save ci time
 * [5c4d5a3c](https://github.com/kubeovn/kube-ovn/commit/5c4d5a3cfa64479c154ab3379f3f6dc3175ac546) chore: use j2 to render different kind.yaml
 * [d1a184ef](https://github.com/kubeovn/kube-ovn/commit/d1a184ef9901055c79b0ba97dce2dadda4ba20aa) fix: set qlen for ovn0
 * [a2d969e8](https://github.com/kubeovn/kube-ovn/commit/a2d969e8f99f406ca1651142cd9d8a6633e0773d) prepare for 1.3 release
 * [3a018a86](https://github.com/kubeovn/kube-ovn/commit/3a018a86e9dde7308f76a1fb30902184a0150cb9) chore: update build.sh
 * [be7c68f2](https://github.com/kubeovn/kube-ovn/commit/be7c68f23fbdb18e166479608eb2b4d663bce8c3) fix: log error
 * [31723f66](https://github.com/kubeovn/kube-ovn/commit/31723f669e54f662d9d56c9833c7386a05fe768f) chore: check ovn-sb connectivity from ovn-ovs probe
 * [d017f1f2](https://github.com/kubeovn/kube-ovn/commit/d017f1f2d0e6a0a0eef96e8ddcfa8b907a7007f1) fix: available ips calculation issues
 * [309c8080](https://github.com/kubeovn/kube-ovn/commit/309c808005ae3d38923569c9e9d0b39c0a082a66) perf: add hw offload
 * [4b8faede](https://github.com/kubeovn/kube-ovn/commit/4b8faede48168fb21ac39cbb3084a40bd7a3777f) docs: add gateway qos doc
 * [32a9af2b](https://github.com/kubeovn/kube-ovn/commit/32a9af2bde14322acd4a77661566f75e1aebf1a4) ci: remove master taint
 * [3865220d](https://github.com/kubeovn/kube-ovn/commit/3865220df86b2af451642037f9a12c1ad618bf28) chore: update cni dependencies
 * [8e032392](https://github.com/kubeovn/kube-ovn/commit/8e032392d62d5a8c4b9545f66ec78ba214cbb9db) feat: session service
 * [34b7cba7](https://github.com/kubeovn/kube-ovn/commit/34b7cba751209975063c155fac2ecbf919348a6e) Revert "perf: use policy-route to replace src-ip route"
 * [1d13d5c3](https://github.com/kubeovn/kube-ovn/commit/1d13d5c38ab51d4c5829a3a76ce7eaa0552d8ed6) Revert "fix: ipv6 policy route"
 * [65813640](https://github.com/kubeovn/kube-ovn/commit/658136408bc58902645b17ac7b8d56ff07a0c09a) Revert "fix: reset address_set when delete subnet"
 * [e6817a65](https://github.com/kubeovn/kube-ovn/commit/e6817a65163ede10b76e5229896af0c6fafe5e1a) fix: reset address_set when delete subnet
 * [dbc968ca](https://github.com/kubeovn/kube-ovn/commit/dbc968ca57823a5b939f6ae133581bb2c8a7f7f4) test: statefulset without ippool
 * [9440a11f](https://github.com/kubeovn/kube-ovn/commit/9440a11ff09ab344224a0e7c0a97ceaa9c83cdbb) match apps/* statefulset
 * [ca122027](https://github.com/kubeovn/kube-ovn/commit/ca122027b0dcf94b736306a982fc48bd2cec9981) fix: ipv6 policy route
 * [54acd0c3](https://github.com/kubeovn/kube-ovn/commit/54acd0c3576b7a09b97d6ffb34b2ed4da582260a) feat: support gw qos
 * [b8f03248](https://github.com/kubeovn/kube-ovn/commit/b8f032487624e5a189a7e0675a66640316a12673) perf: use policy-route to replace src-ip route
 * [83dc420e](https://github.com/kubeovn/kube-ovn/commit/83dc420e4f8c21a1014de6a98bc7feb6ce0c0f2e) Solve the problem of non-standard statefulset creation mode
 * [32e6d572](https://github.com/kubeovn/kube-ovn/commit/32e6d57282dd679b685d75ba0695244fe25bcf76) fix: arm64 build missing env
 * [c93f0d84](https://github.com/kubeovn/kube-ovn/commit/c93f0d848d99f20d0ecdee178521f545b45cfcef) action: use commit as image tag
 * [732b240c](https://github.com/kubeovn/kube-ovn/commit/732b240c07a1134774e27606b1d40192587eddc8) Add libatomic to docker image
 * [9d5294bb](https://github.com/kubeovn/kube-ovn/commit/9d5294bb96f4a0d19dcc742e9412e7f8631c3c9a) chore: save disk space when building
 * [4b1f5244](https://github.com/kubeovn/kube-ovn/commit/4b1f5244c3ce141a0c74f15893b7615bec996777) chore: change crd form v1beta1 to v1
 * [e6fb0fcb](https://github.com/kubeovn/kube-ovn/commit/e6fb0fcb9947c36d36d4bae1e3e64bca3add3c9c) kubectl-ko: add ovs-tracing info
 * [61aa3ba2](https://github.com/kubeovn/kube-ovn/commit/61aa3ba2b47071de74ce10bd42eff43eb22520eb) pinger: add metrics to resolve external address
 * [ef0f3b27](https://github.com/kubeovn/kube-ovn/commit/ef0f3b279edcc62c520144b9d79f7138d27666d8) chore: update ovn to 20.06
 * [961f5f1a](https://github.com/kubeovn/kube-ovn/commit/961f5f1ab42de8f40c28640565dbb894d53ef79b) update changelog
 * [85f2e0e0](https://github.com/kubeovn/kube-ovn/commit/85f2e0e0f85e8b9c88730c5d38eada3f91c19ab0) fix some version in docs
 * [f989bdd8](https://github.com/kubeovn/kube-ovn/commit/f989bdd8f449ec65381fd7fdfee7ecf95d3195a8) fix: rename variable
 * [990bf983](https://github.com/kubeovn/kube-ovn/commit/990bf9839cc061a1adab0978e9d9e32222ade2ae) fix: minor fix
 * [8d7045b3](https://github.com/kubeovn/kube-ovn/commit/8d7045b3a11c34cad82382b95d273dd6af1a869f) feat: use never used address first to reduce conflict
 * [db2516c2](https://github.com/kubeovn/kube-ovn/commit/db2516c2d899803518880d7036aff4154619b2e9) ci: use tmpfs to accelerate e2e
 * [79272376](https://github.com/kubeovn/kube-ovn/commit/792723761535fb01cf20391af0b0d9f61a26da67) fix: create/delete order might lead ip conflict
 * [b27d7545](https://github.com/kubeovn/kube-ovn/commit/b27d754551ab336961f951540a905de4e4009a54) ci: do not push image when pr
 * [a1f53e67](https://github.com/kubeovn/kube-ovn/commit/a1f53e67d76da37322dac333cf015e0bf07ac615) clean up all white noise
 * [a4f40370](https://github.com/kubeovn/kube-ovn/commit/a4f40370d19c0b2712a8d9fefda388e4b6eb1a84) security: update yum repo
 * [270c825c](https://github.com/kubeovn/kube-ovn/commit/270c825cd599c7ffa52632c7f897744d3722a860) fix node's annottaions overwrited incorrectly
 * [5adc5a44](https://github.com/kubeovn/kube-ovn/commit/5adc5a440a06aec9715c3e1f232b84f4332e8013) Fix typo in multi-nic.md
 * [3ac92a15](https://github.com/kubeovn/kube-ovn/commit/3ac92a157d2bb3015963a9e3304e002cdc184d98) Userspace-CNI updates in dpdk.md
 * [76e72b7e](https://github.com/kubeovn/kube-ovn/commit/76e72b7ea09dab0a6299d9510a50e2b527ca92a2) Remove empty lines from DPDK Dockerfile
 * [9b5c018a](https://github.com/kubeovn/kube-ovn/commit/9b5c018aca9e7968b6e98d595eca1dfe13ed9f5a) security: update loopback to fix CVE
 * [bd1f2acf](https://github.com/kubeovn/kube-ovn/commit/bd1f2acf7c511af1cf670f6119cf7c4ea735368c) Make OVS-DPDK start script more robust
 * [3bfc39f8](https://github.com/kubeovn/kube-ovn/commit/3bfc39f82050e16e37a20a843185ca800fa7940e) Reduce DPDK image size
 * [4917afe9](https://github.com/kubeovn/kube-ovn/commit/4917afe98ed89302c1481d25fb1622d79acf54f1) fix: add back privilege for ipv6
 * [8121afd6](https://github.com/kubeovn/kube-ovn/commit/8121afd6037a3414b2ca1a0ff8f3f004d47695a7) Config support for OVS-DPDK
 * [ad30e687](https://github.com/kubeovn/kube-ovn/commit/ad30e6870a2edc35fd6f624d317fcd5bbbeb2ed3) security: add trivy scan and fix image CVEs
 * [06256a09](https://github.com/kubeovn/kube-ovn/commit/06256a09bce362754b680fa8dc1eba6a93e65830) docs: modify arm build
 * [9d2e64a4](https://github.com/kubeovn/kube-ovn/commit/9d2e64a40bab54394e60f90a2f3e8bee38dd35f2) docs: update development
 * [bd975768](https://github.com/kubeovn/kube-ovn/commit/bd975768146fc27dc7ffcdecb2de2ceef9507482) refactor: use ovs.Exec replace raw command
 * [32024ba8](https://github.com/kubeovn/kube-ovn/commit/32024ba88963a56593cd112717ea433abf6cfc30) chore: add gosec to audit code security
 * [1db9046d](https://github.com/kubeovn/kube-ovn/commit/1db9046d6803245d5ff3a5cbdbaff6e0a7c6794f) prepare for next release
 * [aa72ba6c](https://github.com/kubeovn/kube-ovn/commit/aa72ba6ce070ff7406fec3c0d0bccebdd08508ca) fix: arm build
 * [628f5c5e](https://github.com/kubeovn/kube-ovn/commit/628f5c5e154e0070f9f6de7dccfb3ae9d3f3a796) fix: change version in install.sh

### Contributors

 * Gary
 * Haocheng Liu
 * Mengxin Liu
 * MengxinLiu
 * Patryk Strusiewicz-Surmacki
 * Xiang Dai
 * ckji
 * laik
 * linruichao

## v1.2.1 (2020-06-22)

 * [755f57bc](https://github.com/kubeovn/kube-ovn/commit/755f57bc55977bcfa547f1a27bd816dab0b423fe) release 1.2.1
 * [88b847ca](https://github.com/kubeovn/kube-ovn/commit/88b847ca7b860257eef8f118bbdfa37b7d9c5c48) fix: create/delete order might lead ip conflict
 * [0656f63c](https://github.com/kubeovn/kube-ovn/commit/0656f63c3c52c401a9944f470c2c357059c45b3b) fix node's annottaions overwrited incorrectly
 * [86e20a09](https://github.com/kubeovn/kube-ovn/commit/86e20a097f55acc3ab4d0f7de8ac6e9f9bbd1138) security: update loopback to fix CVE
 * [b1ea8a36](https://github.com/kubeovn/kube-ovn/commit/b1ea8a3642830837cc6f1b0e08cc9b9710210cc5) fix: add back privilege for ipv6
 * [2a877530](https://github.com/kubeovn/kube-ovn/commit/2a8775305d9d303197af40265cecc059ead1a027) fix: arm build
 * [8ec2c159](https://github.com/kubeovn/kube-ovn/commit/8ec2c159a34b52381c432aa96e98f30af50b293c) fix: change version in install.sh

### Contributors

 * Mengxin Liu
 * MengxinLiu
 * ckji

## v1.2.0 (2020-05-30)

 * [280a1bd3](https://github.com/kubeovn/kube-ovn/commit/280a1bd39ea045ece2e8ef05259a023846873076) chore: prepare for release 1.2
 * [4342187d](https://github.com/kubeovn/kube-ovn/commit/4342187d8f72ff18511cbfe7ebb3f70cf357a064) chore: prepare for release 1.2
 * [4a52bb43](https://github.com/kubeovn/kube-ovn/commit/4a52bb43df3eb9fbbd1f56aa43f02085c87363b9) DPDK doc update and small image reduction
 * [b055cc68](https://github.com/kubeovn/kube-ovn/commit/b055cc68af72111431130376ada2494102eb70fe) Add OVS-DPDK support, for issue 104
 * [f7fdd2dc](https://github.com/kubeovn/kube-ovn/commit/f7fdd2dcae463837bc98f51b08cb682372d1762e) fix: pod get deleted between configure nb and patch pod
 * [e13dc5ac](https://github.com/kubeovn/kube-ovn/commit/e13dc5ac0ec194f90302f31a09c37492de4beab4) fix: native vlan and delete subnet issues
 * [44b5a6a7](https://github.com/kubeovn/kube-ovn/commit/44b5a6a7cf53646b14f02524e3060238e0c0fce6) fix: trigger github action when dist dir change
 * [3a2ee051](https://github.com/kubeovn/kube-ovn/commit/3a2ee051cba6d1f55f81abc2e37e6167ad35fedf) fix: update ovn patch
 * [6e1589cc](https://github.com/kubeovn/kube-ovn/commit/6e1589cc50c009b07293bb83f726483586ac59ca) chore: improve log
 * [00f98489](https://github.com/kubeovn/kube-ovn/commit/00f9848902d14c4c1fec406001fd3c115f2bb300) fix: gc lsp for pod that not alive
 * [701e9efd](https://github.com/kubeovn/kube-ovn/commit/701e9efdc3cde047b7336511c1a5f61538c13681) feat: support underlay without vlan encap
 * [83ad499f](https://github.com/kubeovn/kube-ovn/commit/83ad499fdda2f32b7a1b6d0774ceddedf91aa5ac) chore: optimize kube-ovn-cni log
 * [84b6cdcf](https://github.com/kubeovn/kube-ovn/commit/84b6cdcf64e7bd39aede73f87fc0d070869f6dfb) fix: gc node lsp
 * [7aafd944](https://github.com/kubeovn/kube-ovn/commit/7aafd94477588c8d4fc022a7d0d6ce8676e8bd87) chore: remove vagrant
 * [92ccf729](https://github.com/kubeovn/kube-ovn/commit/92ccf729f46adaee18ab00102aef496cfe2a6999) fix: dst route policy might be empty
 * [6c89a046](https://github.com/kubeovn/kube-ovn/commit/6c89a046755837dc93e2c73cc258efc4a675ba4e) feat: in vlan mode if physical gateway exists, no need to create a virtual one
 * [1d5c6958](https://github.com/kubeovn/kube-ovn/commit/1d5c6958aa83e83f4ad4bfa22ed8a58be69900c8) perf: add amd64 compile flags back
 * [b0f0947d](https://github.com/kubeovn/kube-ovn/commit/b0f0947d010b81f486148c293cd22e323e728922) fix: init ipam before gc, other wise routes will be deleted
 * [dbc23c5e](https://github.com/kubeovn/kube-ovn/commit/dbc23c5e99057a547cdd216487771095c14cf60a) fix: patch ovn to lower src-ip route priority to work with ovn-ic
 * [5a763820](https://github.com/kubeovn/kube-ovn/commit/5a7638200612b179414342252cf1d0fd27de78ec) fix: return early if allocation is not ready
 * [b03c3768](https://github.com/kubeovn/kube-ovn/commit/b03c37684ded0dd89df7f3ea84b985aa09459a12) chore: remove networks crd
 * [2853438c](https://github.com/kubeovn/kube-ovn/commit/2853438c4ad3632082ad9cb2d8b40d9325fb20a1) perf: remove more stale lflow
 * [0665f2e8](https://github.com/kubeovn/kube-ovn/commit/0665f2e8fee7ff6b5a5f5d68731b7d15d967c723) ci: run ut and e2e in github action
 * [e71b68c0](https://github.com/kubeovn/kube-ovn/commit/e71b68c01e6045aa291fc60d398c531d44895b43) fix: check svc and endpoint protocol
 * [508eb7a2](https://github.com/kubeovn/kube-ovn/commit/508eb7a28ff3138010ac73808a95c3ca427f9cdb) perf: reduce lflow count
 * [5f8b9b40](https://github.com/kubeovn/kube-ovn/commit/5f8b9b406427d90fb5a3aca097839172aa7c57b1) fix: when podName or namespace contains dot, lsp cannot be deleted correctly
 * [27c72560](https://github.com/kubeovn/kube-ovn/commit/27c72560c6caa58b1a125d020c7af06c19584c73) fix: wrong subnet status
 * [f0b17a69](https://github.com/kubeovn/kube-ovn/commit/f0b17a69b0f1b0945e015232713170c01e75f56b) feat: change pod route when update gateway type
 * [13283daf](https://github.com/kubeovn/kube-ovn/commit/13283daf39c4f1a7231d88f2e5d5e5c0ea72adf1) feat: refactor subnet and allow cidr change
 * [23821d6c](https://github.com/kubeovn/kube-ovn/commit/23821d6cb256ea8a9adaf71d5af389d12e3b8207) fix: use kubectl to avoid tls handshake error
 * [e647cc6c](https://github.com/kubeovn/kube-ovn/commit/e647cc6c05960b77a11781a2de98a51303a89ccd) chore: reduce logs
 * [aef4336d](https://github.com/kubeovn/kube-ovn/commit/aef4336d1c13e67422508f5a504ac3016fb51122) feat: only show error log of kube-ovn-controller
 * [a9ab0bc2](https://github.com/kubeovn/kube-ovn/commit/a9ab0bc2531ca472c9c3f3d0608cd138db9f8bd2) fix: map concurrent panic
 * [2dd13b23](https://github.com/kubeovn/kube-ovn/commit/2dd13b2328a056cf343914f635da56cffb56057b) fix: ipv6 related issues
 * [86c443e7](https://github.com/kubeovn/kube-ovn/commit/86c443e71d72e624518ca71d285e115979cf626a) fix: validate if subnet cidr conflicts with svc ip
 * [eb4cb1b3](https://github.com/kubeovn/kube-ovn/commit/eb4cb1b3a2ef58f566002b982a656f2bab00295d) fix: validate if node address conflict with subnet cidr
 * [7f595ee0](https://github.com/kubeovn/kube-ovn/commit/7f595ee030c6cc2e4a6099d47f592659c0a66eae) feat: github action
 * [1046b572](https://github.com/kubeovn/kube-ovn/commit/1046b5727767a804f2001b9342cabf5211c4e696) fix: wait node annotations ready before handle pods
 * [7a0151cc](https://github.com/kubeovn/kube-ovn/commit/7a0151cc21919d1b89b076c7262169418386d1c7) fix: check ovn-nbctl socket in new dir
 * [0dc76768](https://github.com/kubeovn/kube-ovn/commit/0dc76768bfba4105ecece940e29a00142b02721a) fix: error log found in scale test
 * [04715943](https://github.com/kubeovn/kube-ovn/commit/047159438c7c500b54dc5e5aac2f6c8375605acb) fix: concurrent panic
 * [da14eaeb](https://github.com/kubeovn/kube-ovn/commit/da14eaebbe4950a369db0200e9531f20d79e0024) feat: use bgp to announce pod ip
 * [909b5a00](https://github.com/kubeovn/kube-ovn/commit/909b5a00da781f56123f77add7016afc56a3a8be) release 1.1.1
 * [ab834b5a](https://github.com/kubeovn/kube-ovn/commit/ab834b5a1d817a0dd9f7cd929c5ab6c74d32db6b) fix: labels might be nil
 * [0c0824db](https://github.com/kubeovn/kube-ovn/commit/0c0824db916f5e36e1437fd4acdb347476a2f86a) fix: ping output format
 * [ce27fb31](https://github.com/kubeovn/kube-ovn/commit/ce27fb31c44ce78f78fdcded6020fc1576ff561f) monitor: make graph more sensitive to changes
 * [9b05fccf](https://github.com/kubeovn/kube-ovn/commit/9b05fccf2942a453ff818beb6e7e1a5bee72e074) docs: update vlan docs
 * [d0544d89](https://github.com/kubeovn/kube-ovn/commit/d0544d8969f7ece8fa352f5c81a78ba03b3fbd9b) docs: update docs
 * [28aef840](https://github.com/kubeovn/kube-ovn/commit/28aef840e09a9bbf35883eb621c72f55f438691d) feat: improve install/uninstall
 * [8d853656](https://github.com/kubeovn/kube-ovn/commit/8d853656496dd94d54f5fc681e2c8b18c5c468ef) refactor: refactor cni-server
 * [d99ffff0](https://github.com/kubeovn/kube-ovn/commit/d99ffff0e39dfe71bf22e5de606bb42caa2f8813) refactor: controller refactor
 * [8f1f0135](https://github.com/kubeovn/kube-ovn/commit/8f1f0135dd7188359502db41bcb801b86c051293) feat: modify install.sh for vlan type network
 * [cfe9d276](https://github.com/kubeovn/kube-ovn/commit/cfe9d27614859cf73ea442a4c28d7d88b3e1c213) feat(vlan): vlan network type
 * [edd0ea81](https://github.com/kubeovn/kube-ovn/commit/edd0ea81e935c9d5a76f1a7fb1c6871ea1a49511) feat(vlan): vlan network type
 * [c63accf4](https://github.com/kubeovn/kube-ovn/commit/c63accf42e109537b7e26d5b0dbadcb34e701349) fix: yaml indent and ovn central dir
 * [5bc84d7b](https://github.com/kubeovn/kube-ovn/commit/5bc84d7b4a5b1f825f6b27d6c50e42879846b0c3) docs: chinese wechat info
 * [feaec4dd](https://github.com/kubeovn/kube-ovn/commit/feaec4ddd63d816a22f5b25cc6bf568b2183fd76) fix: fork go-ping and apply patches
 * [58f73b33](https://github.com/kubeovn/kube-ovn/commit/58f73b33f4062e25a730ad2570ac328b2291b2d5) chore: update kind node to 1.18 and ginkgo
 * [d274a979](https://github.com/kubeovn/kube-ovn/commit/d274a979a786847f8eda80e8573083a26da1ef86) docs: add arm build steps
 * [d061fc3c](https://github.com/kubeovn/kube-ovn/commit/d061fc3ca98e7a8f795c04648782b36c7881478e) fix: mount etc/origin/ovn to ovs-ovn
 * [f8d6fd5c](https://github.com/kubeovn/kube-ovn/commit/f8d6fd5c1a922c178a2e43a42d6a211ba7865cd4) add support for multi-arch build
 * [953f5be7](https://github.com/kubeovn/kube-ovn/commit/953f5be7b345a805e6080516fc5ba769a7251299) docs: change the cidr to avoid misunderstanding
 * [5c5b9e08](https://github.com/kubeovn/kube-ovn/commit/5c5b9e08f4733c931e1eb82c433d67c87038987f) feat: diagnose check if dns/kubernetes svc exist
 * [7c6d6784](https://github.com/kubeovn/kube-ovn/commit/7c6d6784f417dd5b9bf71376b30be95d3b5d9c98) OVS local interface table mac_in_use row is lower case, but pod annotation store mac in Upper case.
 * [b53a2153](https://github.com/kubeovn/kube-ovn/commit/b53a2153727a8bd6c0e7a1a1fa89a20cd2bc2fd7) prepare for 1.2
 * [0d60df32](https://github.com/kubeovn/kube-ovn/commit/0d60df326555aa06b1a90299872d0a0b73923e78) fix: separate log for no address and wrong address
 * [a4106b2d](https://github.com/kubeovn/kube-ovn/commit/a4106b2d7eab2346bef84c9afa05d2c35accf291) docs: format docs

### Contributors

 * Gary
 * Mengxin Liu
 * MengxinLiu
 * Yan Zhu
 * fangtian
 * linruichao

## v1.1.1 (2020-04-27)

 * [08f39db7](https://github.com/kubeovn/kube-ovn/commit/08f39db71344c4f48df00597919e66dddd0672d8) release 1.1.1
 * [95d479f5](https://github.com/kubeovn/kube-ovn/commit/95d479f580649426b2d8ab030a2a453b2b18ddbc) fix: labels might be nil
 * [eb9c9fd6](https://github.com/kubeovn/kube-ovn/commit/eb9c9fd64a689236034d98e7c32700ebb12b8456) monitor: make graph more sensitive to changes
 * [7e9b3661](https://github.com/kubeovn/kube-ovn/commit/7e9b36611eb8ae76b69cf27f226bf37286f08491) fix: ping output format
 * [ae53bf57](https://github.com/kubeovn/kube-ovn/commit/ae53bf57b2b151430eff218e78d3c6da9c101f91) fix: yaml indent and ovn central dir
 * [82128d2f](https://github.com/kubeovn/kube-ovn/commit/82128d2f54090f6eb43ad9aaf67b8ad6e4e6d57f) fix: fork go-ping and apply patches
 * [14247939](https://github.com/kubeovn/kube-ovn/commit/142479393e608c15787953c7efaf77c4e53fdef7) fix: mount etc/origin/ovn to ovs-ovn
 * [83f3a920](https://github.com/kubeovn/kube-ovn/commit/83f3a92042c13210e642e9698b290d13dc3c93d3) fix: use legacy iptables

### Contributors

 * MengxinLiu

## v1.1.0 (2020-04-07)

 * [de9b003d](https://github.com/kubeovn/kube-ovn/commit/de9b003da042aab313ee2ae9d4f069231c5df846) release 1.1.0
 * [4511a16b](https://github.com/kubeovn/kube-ovn/commit/4511a16b68cc6919e170e55b357537d57ebb922f) feat: use buildx to reduce image size
 * [370689e7](https://github.com/kubeovn/kube-ovn/commit/370689e79633f05810eea404c15e9831cca6a2e6) test: check host route when add/del a subnet
 * [0df863b6](https://github.com/kubeovn/kube-ovn/commit/0df863b67680b5baebc62b5af72a9bc8bd4f7df0) [DO NOT REVIEW] vendor update: introduce klogr and do some tidy
 * [eeba4c01](https://github.com/kubeovn/kube-ovn/commit/eeba4c01918166017f06f434ca9312cdb3953094) [webhook] init logger for controller-runtime
 * [ae187152](https://github.com/kubeovn/kube-ovn/commit/ae187152abd6004dcaa0958b7fd2128eaf3ce8ab) test: add node test
 * [e1038d22](https://github.com/kubeovn/kube-ovn/commit/e1038d222d35700c71cc5749d46181d0103235c1) fix: acl and qos issues
 * [a4c81ba7](https://github.com/kubeovn/kube-ovn/commit/a4c81ba78999532fc6a11bb48765ad59abd4075c) feat: expose iface in install.sh
 * [b6967f57](https://github.com/kubeovn/kube-ovn/commit/b6967f57bd58407576eef6964e11f0d8ace7227e) fix: remove auto checksums
 * [dbc85075](https://github.com/kubeovn/kube-ovn/commit/dbc85075ab7822b0f7478a5b528792683e339c7a) perf: offload udp checksum if possible
 * [bdb23691](https://github.com/kubeovn/kube-ovn/commit/bdb23691cce02e8c7a1dab88dac5ab3070a3f2ae) release v1.0.1
 * [cdf4de3f](https://github.com/kubeovn/kube-ovn/commit/cdf4de3f905029f8617c8657d7d1768df03840bd) perf: add x86 optimization CFLAGS
 * [131181c2](https://github.com/kubeovn/kube-ovn/commit/131181c248681602eb75cd2722f1b30119e943e4) chore: add scripts to build ovs
 * [2b5dd72b](https://github.com/kubeovn/kube-ovn/commit/2b5dd72b61ef8d877ea43d1ee36079990daaa2bf) fix: lost route when subnet add and is not ready
 * [9032ac84](https://github.com/kubeovn/kube-ovn/commit/9032ac8439422b342c75f684d909607ed8cb5345) fix: ip prefix might be empty
 * [d1654e15](https://github.com/kubeovn/kube-ovn/commit/d1654e152d676598dfb01881b8334711a93da094) chore: reduce image size
 * [464e991e](https://github.com/kubeovn/kube-ovn/commit/464e991ee8e1aac9785264b6d6bab70ea2b075de) chore: modify nodeSelector label to support k8s 1.17
 * [2814a1d5](https://github.com/kubeovn/kube-ovn/commit/2814a1d5f846f5aa6ed240daf9d77956dc499ad5) fix: use ovn-appctl to do recompute
 * [0eaedd99](https://github.com/kubeovn/kube-ovn/commit/0eaedd99fffe3c20b2e3e54c0b13f2749c52ea49) docs: multi nic
 * [dd1923c3](https://github.com/kubeovn/kube-ovn/commit/dd1923c3292c14a2d109b9b3b247481cc3af2a22) feat: ip cr support multi-nic
 * [b2ce6f08](https://github.com/kubeovn/kube-ovn/commit/b2ce6f08e09f14102f5fac90608c530667cae59d) fix: update in svc 1.1.1.1 may del svc 1.1.1.10
 * [20bb7a78](https://github.com/kubeovn/kube-ovn/commit/20bb7a7842b52f5e6a4a801f23cc3b3eedc52997) feat: add cni side logical to support ipam for multi-nic
 * [1319eb5d](https://github.com/kubeovn/kube-ovn/commit/1319eb5d436298cd402d8f2a26faf0ddee13b144) feat: add basic allocation function for multus-cni
 * [8f6997a9](https://github.com/kubeovn/kube-ovn/commit/8f6997a93584b45d5acd036f619268549af43afd) fix: only delete pod that restart policy is Always
 * [3a2de9cd](https://github.com/kubeovn/kube-ovn/commit/3a2de9cdc94eaa2da174a25fd14e0e4e3a44805e) perf: only enqueue updatePod when needed
 * [0f7b9d4c](https://github.com/kubeovn/kube-ovn/commit/0f7b9d4ceef86e75f35ad78bd06119cfb41d0318) fix: add iptables to accept container traffic
 * [bdd021c0](https://github.com/kubeovn/kube-ovn/commit/bdd021c0347c7adb207d4684e3c631ea97830f75) feat: check kube-proxy and coredns in diagnose
 * [502f18cf](https://github.com/kubeovn/kube-ovn/commit/502f18cf28a68fb3a739149480e8d549e87c3421) feat: add label param in install script
 * [5a1cf371](https://github.com/kubeovn/kube-ovn/commit/5a1cf3719832ad306e02062da40eabfbbb0200b5) perf: recycle ip and lsp for pod that in failed or succeeded phase
 * [d1968584](https://github.com/kubeovn/kube-ovn/commit/d1968584156cf77180a0cd5d834e434d937eea63) fix: add inactivity_probe back
 * [417a001b](https://github.com/kubeovn/kube-ovn/commit/417a001b660f5ba8fb04060cddea9b7b06177245) feat: check if crds exist in diagnose
 * [e65a9d09](https://github.com/kubeovn/kube-ovn/commit/e65a9d091d669de597905cea0629d5995bdb26fa) fix: gc static routes
 * [91829d24](https://github.com/kubeovn/kube-ovn/commit/91829d2404672926364b980cd6281fdbcb9a02ec) fix: still delete lsp if pod not in ipam
 * [7d22430d](https://github.com/kubeovn/kube-ovn/commit/7d22430d4b93902f7e74210af8ab629001486f1b) fix: delete chassis from sb when delete node
 * [5f5df34e](https://github.com/kubeovn/kube-ovn/commit/5f5df34e9d721d54bb5408fd1cd460a0919dda7e) fix: missing label selector
 * [9822dba9](https://github.com/kubeovn/kube-ovn/commit/9822dba992442cfa669f944817642b4239bbeb79) feat: add one script installer
 * [479437a3](https://github.com/kubeovn/kube-ovn/commit/479437a35667e228ba605d1c8800867b1294a377) fix: cleanup in offline environment
 * [e707eb96](https://github.com/kubeovn/kube-ovn/commit/e707eb96d95cb28468e04ecd47a7f0a877296c3b) feat: diagnose check ds/deployment status
 * [3c786f57](https://github.com/kubeovn/kube-ovn/commit/3c786f57f77d808f310905c7f184b3d4fad5bc77) refactor: the ipam now has lock itself no need for ippool queue
 * [9211486b](https://github.com/kubeovn/kube-ovn/commit/9211486be0a91aa7ea8f55d4d7020276a0f3b42a) fix: if pod is evicted, recycle address
 * [2546deaf](https://github.com/kubeovn/kube-ovn/commit/2546deaf7fea0721de546946a3e473cb5a109aa9) fix: use uuid to fetch vip
 * [51f06bd6](https://github.com/kubeovn/kube-ovn/commit/51f06bd64ca07a9e958f7da4d32848a3b9495881) refactor ipam
 * [2336dc75](https://github.com/kubeovn/kube-ovn/commit/2336dc75f546b6fa13700d7359284b477dff47c9) release 1.0.0
 * [7d918f56](https://github.com/kubeovn/kube-ovn/commit/7d918f5600468e0ac5f834b768b78e4cc9d429c1) refactor pod controller
 * [866db995](https://github.com/kubeovn/kube-ovn/commit/866db995f89a41f9de3ed9e66fd5ace2f6da6071) merge images into one
 * [8296a9e7](https://github.com/kubeovn/kube-ovn/commit/8296a9e7e4b4deaaae9a28412af961a8f50c96a4) fix:enablebash alias option in Dockerfile CMD scripts
 * [68d87ec2](https://github.com/kubeovn/kube-ovn/commit/68d87ec2327068f4bd5549080d0b9cf2d94dfe50) webhook: use global variables to avoid repeated map constructing
 * [cf2784ad](https://github.com/kubeovn/kube-ovn/commit/cf2784adff77f30237fd1bcf9e66100810a7a313) remove useless fields in webhook.yaml
 * [657b5a29](https://github.com/kubeovn/kube-ovn/commit/657b5a295c9ad7d59677e03620d45262e668eeae) remove leader-election for webhook manager
 * [2bcf0d28](https://github.com/kubeovn/kube-ovn/commit/2bcf0d284f3edf8a041bc4289293c4970a75ffea) feat: update to 20.03.0 ovn

### Contributors

 * Bruce Ma
 * MengxinLiu
 * Your Name

## v1.0.1 (2020-03-31)

 * [706cdfc3](https://github.com/kubeovn/kube-ovn/commit/706cdfc377268b4ebff1cb7cc6cee9ea25727599) release v1.0.1
 * [a51a672a](https://github.com/kubeovn/kube-ovn/commit/a51a672a042645287aabb9d6ae0f850b86b11f14) fix: lost route when subnet add and is not ready
 * [576cf776](https://github.com/kubeovn/kube-ovn/commit/576cf77680c8f3fca375f7662296bd8cd3fc9990) fix: ip prefix might be empty
 * [0e1670bf](https://github.com/kubeovn/kube-ovn/commit/0e1670bf7d110d39048b57b4f30576556d057262) fix: update in svc 1.1.1.1 may del svc 1.1.1.10
 * [63f05e5a](https://github.com/kubeovn/kube-ovn/commit/63f05e5a141f7da01a1c22651da7297943a6dd82) fix: add inactivity_probe back
 * [bad0c43f](https://github.com/kubeovn/kube-ovn/commit/bad0c43f65c818609c00b014e443581082996f7b) fix: use uuid to fetch vip

### Contributors

 * MengxinLiu

## v1.0.0 (2020-02-27)

 * [f40ce553](https://github.com/kubeovn/kube-ovn/commit/f40ce553de4d480782a0e4b1c83e248208a970c3) release 1.0.0
 * [28238794](https://github.com/kubeovn/kube-ovn/commit/282387945fa246f2d2a1f22f0fe377f583d797d8) prepare for 1.0
 * [a036b37b](https://github.com/kubeovn/kube-ovn/commit/a036b37b790ecf545aedd7c94fd4d60b666cc7db) fix: add back missing lsp gc
 * [44d53c24](https://github.com/kubeovn/kube-ovn/commit/44d53c24ae740b8053f4d44c7d3befc66878b033) fix: delete lb if it has no backend
 * [b8498a83](https://github.com/kubeovn/kube-ovn/commit/b8498a8364dac6839049cbd3af58ff0553592448) metrics: expose cni operation metrics
 * [a75f9991](https://github.com/kubeovn/kube-ovn/commit/a75f99917742711661dba4db54f780051607ea32) refactor: refactor server.go
 * [c88221ee](https://github.com/kubeovn/kube-ovn/commit/c88221ee41bfe6b460694b74241c4bda30cde480) fix: disable ovn-nb inactivity_probe
 * [957654f9](https://github.com/kubeovn/kube-ovn/commit/957654f9845aa2a02daaef58dffd132331a457d4) fix: wait for container network ready before cni return
 * [870d20b0](https://github.com/kubeovn/kube-ovn/commit/870d20b0a0c95c8386aaadac2992132ca128ceca) refactor: refactor controller.go
 * [2885419d](https://github.com/kubeovn/kube-ovn/commit/2885419d83c9e03103a6ee01244b798860ccdb66) ovn: pick upstream performance patch
 * [11598739](https://github.com/kubeovn/kube-ovn/commit/1159873975902d7d2cb94b63bc026758f7edd5ce) docs: add the development guide and fix the lint
 * [0be25516](https://github.com/kubeovn/kube-ovn/commit/0be2551616b2cbc9424a85902b7d1b0539b0ef12) docs: add companies using kube-ovn section
 * [d56552b8](https://github.com/kubeovn/kube-ovn/commit/d56552b896ef0060c96cb9711dda9cbfdce07677) docs: add community information
 * [8edd0225](https://github.com/kubeovn/kube-ovn/commit/8edd0225548c4c964d71ef0a60e5c7f8403a2792) fix: alleviate ping lost
 * [632bbc5e](https://github.com/kubeovn/kube-ovn/commit/632bbc5e17565396043d615760845721c48285f5) refactor: refactor ovn-nbctl.go
 * [8aafa415](https://github.com/kubeovn/kube-ovn/commit/8aafa415ef4a882363f7324b177149d8f5ba9f6a) docs: modify the readme
 * [60ce7659](https://github.com/kubeovn/kube-ovn/commit/60ce76592a20c890287d9613eaa0fb2d7772ddfb) fix: pinger percentage error
 * [276a28cf](https://github.com/kubeovn/kube-ovn/commit/276a28cf8af18246d5953a062457469ce500b7e5) fix: add kube-ovn types to default scheme
 * [998a9e63](https://github.com/kubeovn/kube-ovn/commit/998a9e63449158b04c87defe6282ac4717747db4) refactor: cniserver
 * [a5d339b2](https://github.com/kubeovn/kube-ovn/commit/a5d339b2b5132a0ce01dc416e0042204d7c7f239) docs: update docs
 * [dc92afa3](https://github.com/kubeovn/kube-ovn/commit/dc92afa3dcfa9022da7c34526289bc65e8d354fd) fix: add a periodically recompute to ovn-controller to avoid inconsistency
 * [8488ae2a](https://github.com/kubeovn/kube-ovn/commit/8488ae2a65f3361bd554025967294c3bb4faefa4) fix: add timeout to pinger access ovs/ovn
 * [ff1ff145](https://github.com/kubeovn/kube-ovn/commit/ff1ff1457906480c7502b408707777df68676b83) fix: when subnet cidr conflict requeue the subnet
 * [e31a08ec](https://github.com/kubeovn/kube-ovn/commit/e31a08ec3fa67d2ab6ea4281f8a952b57f4d2e3d) fix: add runGateway to wait.Until
 * [18239073](https://github.com/kubeovn/kube-ovn/commit/1823907373f12cb812bc2616cf68b2e4497ed8c8) fix: restart nbctl-daemon if not response
 * [839308e0](https://github.com/kubeovn/kube-ovn/commit/839308e080243fafa1a40d7a3161a3c75f4d402e) feat: display controller log in kubectl-ko diagnose
 * [8e6c3d62](https://github.com/kubeovn/kube-ovn/commit/8e6c3d62d529e4c6e1c9b818b6c37f6f43629230) refactor: separate normal check and ovn specific check
 * [c9783181](https://github.com/kubeovn/kube-ovn/commit/c97831817d6fd962ef8e07c32fcbcb364d4fce2d) fix: do not return not found err
 * [f19e5596](https://github.com/kubeovn/kube-ovn/commit/f19e5596453b5ba97b97693d54add4f4fc4561e2) fix: move components to kube-system ns and add priorityClass
 * [a5d298db](https://github.com/kubeovn/kube-ovn/commit/a5d298dbfb45ae33059ce5e3d5d8ae9b633e0ee8) feat: cniserver check allocated annotation before configure pod network
 * [8f72b7eb](https://github.com/kubeovn/kube-ovn/commit/8f72b7ebb7437a95759aa9b628145b8044c8e4d5) fix: set ovn-openflow-probe-interval
 * [3838a46d](https://github.com/kubeovn/kube-ovn/commit/3838a46d146148bd8b9d7b7c0cc914f984a8ab84) pinger: add port binds check between local ovs and ovn-sb
 * [f8248cec](https://github.com/kubeovn/kube-ovn/commit/f8248cec44caf653c0ade0537c14e5f6f4f34cd2) fix: if cidr block not ends with zero, reformat it
 * [dff1d648](https://github.com/kubeovn/kube-ovn/commit/dff1d64857359919e1405f2dd11f8b3782e68fec) fix: resync iptables
 * [40fab55f](https://github.com/kubeovn/kube-ovn/commit/40fab55f27081b87303dd45cf019195f19cac06d) update version
 * [920053c5](https://github.com/kubeovn/kube-ovn/commit/920053c5dfddf85ea0d81f9c5d1d4303c488a9d5) pinger: add timeout for dns resolve
 * [513d2bd9](https://github.com/kubeovn/kube-ovn/commit/513d2bd9f55a19446a65bb7c8aa14543c5c931be) e2e: add basic framework and tests for e2e

### Contributors

 * Bruce Ma
 * Mengxin Liu
 * MengxinLiu
 * withlin

## v0.10.2 (2020-01-09)

 * [c5f49f24](https://github.com/kubeovn/kube-ovn/commit/c5f49f24cae17ec229076b0ca51fc29abcf0eb89) release 0.10.2
 * [61b7dded](https://github.com/kubeovn/kube-ovn/commit/61b7dded9c974983c891bed6fba4840c3942eddc) fix: add a periodically recompute to ovn-controller to avoid inconsistency
 * [9de9d0b5](https://github.com/kubeovn/kube-ovn/commit/9de9d0b5d131a894a5acb86074298f8bd3813b82) fix: when subnet cidr conflict requeue the subnet
 * [dca15914](https://github.com/kubeovn/kube-ovn/commit/dca1591436e601fb7228cf2bc2097d773730d65f) fix: add runGateway to wait.Until
 * [f16209b4](https://github.com/kubeovn/kube-ovn/commit/f16209b441537a2de63bd0b501e226f00d18b4ed) fix: restart nbctl-daemon if not response

### Contributors

 * Mengxin Liu

## v0.10.1 (2020-01-02)

 * [09e27cea](https://github.com/kubeovn/kube-ovn/commit/09e27cea7e6f34dd0ca76973ab93ccad8d102a5e) release: v0.10.1
 * [fafa5607](https://github.com/kubeovn/kube-ovn/commit/fafa560712816d74515a0764c0f6bf2193e2bae0) fix: do not return not found err
 * [858d3331](https://github.com/kubeovn/kube-ovn/commit/858d3331f6c1a066a7a66929d61db492738f9b54) fix: set ovn-openflow-probe-interval
 * [641d6f86](https://github.com/kubeovn/kube-ovn/commit/641d6f86627d84e013ae5a59c5b0f0a0dc5a54db) pinger: add port binds check between local ovs and ovn-sb
 * [8435a335](https://github.com/kubeovn/kube-ovn/commit/8435a335c81955794053d72230a4473e63091ddd) fix: if cidr block not ends with zero, reformat it
 * [1f5df246](https://github.com/kubeovn/kube-ovn/commit/1f5df246a24357fce175f182a66deab3e634636b) fix: resync iptables

### Contributors

 * Mengxin Liu

## v0.10.0 (2019-12-23)

 * [9747d540](https://github.com/kubeovn/kube-ovn/commit/9747d5405f8c8552a33f89a0f35ffc916af7d11d) docs: update changelog
 * [adf5071e](https://github.com/kubeovn/kube-ovn/commit/adf5071e1dc58dae33fafee7913429e2b93e2cfe) fix: address in ep might be empty
 * [182bb151](https://github.com/kubeovn/kube-ovn/commit/182bb1513017de14327fd681fa940f2965f8287c) fix: cniserver wait ovs ready
 * [518c0a78](https://github.com/kubeovn/kube-ovn/commit/518c0a7817e174a353d3d2bfd9f4d7f3dc633af0) fix: wrong deletion in gc lb and portgroup
 * [2492a166](https://github.com/kubeovn/kube-ovn/commit/2492a166147d008dcaa4fbd4aa71267618c1cdfb) ovn: add memory patch to slow down memory increase
 * [d0bd71fd](https://github.com/kubeovn/kube-ovn/commit/d0bd71fd7acde3e2315491d8491cc21875bb0656) fix: wait default and node logical switch ready
 * [23cad463](https://github.com/kubeovn/kube-ovn/commit/23cad463b9f6fe07a8668dcc1008dba70666cc2b) fix: podSelector in networkpolicy should only consider pods in the same ns
 * [ca5539f0](https://github.com/kubeovn/kube-ovn/commit/ca5539f09cbff947bec69487e5e8e70b377b0400) fix: do not add unallocated pod to port-group
 * [d5ed1ee7](https://github.com/kubeovn/kube-ovn/commit/d5ed1ee73151a67612a87372850e9b7da01a4d66) release 0.10.0
 * [3c62ea29](https://github.com/kubeovn/kube-ovn/commit/3c62ea29e5688a7a8d019e942040c05995e35126) ovn: pick up commit from upstream
 * [4c966c37](https://github.com/kubeovn/kube-ovn/commit/4c966c371713474edecec051151330761c716806) feat: pinger support check an address out of cluster.
 * [f0096078](https://github.com/kubeovn/kube-ovn/commit/f009607895f0a4610fa3ca72c16d7ea8d05a604c) chore: double quote shell variables
 * [83364b52](https://github.com/kubeovn/kube-ovn/commit/83364b52e1a74d9cf3805620599ff581a7a8cbbe) fix: cluster mode db will generate lots listen error log
 * [d9e1cd1c](https://github.com/kubeovn/kube-ovn/commit/d9e1cd1cad29de427bfdeea25aea71092c51a613) fix: gc logical_switch_port form listing pods and nodes
 * [a5dc8bb9](https://github.com/kubeovn/kube-ovn/commit/a5dc8bb9b3a2b00e8e700cf75e923395268fa50f) fix: some init and cleanup bugs
 * [a5eb5e7f](https://github.com/kubeovn/kube-ovn/commit/a5eb5e7f3bb259f53c50fba35b225bd160ee3694) fix: ovn-cluster mode
 * [a6f0dd14](https://github.com/kubeovn/kube-ovn/commit/a6f0dd143b8bc0b47b313b54247a810f3b13b929) feat: exclude_ips can be changed dynamically
 * [d9c59434](https://github.com/kubeovn/kube-ovn/commit/d9c594343a3727d2c15a2f199eb1b0624605bd7d) update ovn to 2.12.0-1
 * [06eceb3b](https://github.com/kubeovn/kube-ovn/commit/06eceb3b31fc389248309746e64664e541f78012) feat: use label to select leader to avoid pod status misleading
 * [aa53c7dd](https://github.com/kubeovn/kube-ovn/commit/aa53c7dd4122dd2e7b1b9d20bd07e6393f2b352e) fix: ip conflict when use ippool
 * [59044330](https://github.com/kubeovn/kube-ovn/commit/59044330170e47c0a3b6f60857de8ac91405fab3) docs: add v0.9.1 changelog
 * [5efbea9f](https://github.com/kubeovn/kube-ovn/commit/5efbea9f31ccd6b47bc3b30b4e5f3a27df2602ea) fix: block subnet deletion when there any ip in use
 * [a1dc8c11](https://github.com/kubeovn/kube-ovn/commit/a1dc8c11fb95b1c60d52ae7ecb39df26932d5a0f) plugin: kubectl plugin now expose ovs-vsctl to each node
 * [d3c6a71c](https://github.com/kubeovn/kube-ovn/commit/d3c6a71c50caa98b6b667ca71b7004c5e0a9b266) fix: nbctl need timeout to avoid hang infinitely
 * [77e58903](https://github.com/kubeovn/kube-ovn/commit/77e58903458c1765a9541bc3e4415e5ee52877fe) perf: as lr-route-add with --may-exist will replace exist route, no need for another delete
 * [d4a51bdc](https://github.com/kubeovn/kube-ovn/commit/d4a51bdc67b4dbafd6a4d3e91c71242d47407250) perf: when controller restart skip pod already create lsp
 * [7617fa79](https://github.com/kubeovn/kube-ovn/commit/7617fa79776ffa04a38fc7c8d8a607f3e44c0ff6) fix: when delete node recycle related ip/route resource
 * [f4e87476](https://github.com/kubeovn/kube-ovn/commit/f4e8747693c1220597616cbbb4653696c45864ed) fix typo in start-ovs.sh
 * [9b88e084](https://github.com/kubeovn/kube-ovn/commit/9b88e08419471e186e29d25dde47e5a53c7ef068) perf: skip evicted pod when enqueueAddPod and enqueueUpdatePod
 * [e4818624](https://github.com/kubeovn/kube-ovn/commit/e481862402cd5af579ce44391a34c1b4f9af44b8) fix: use ep.subset.port.name to infer target port number
 * [0d8ae20c](https://github.com/kubeovn/kube-ovn/commit/0d8ae20c3eb42dfaf115c8ec0b67ba7d720be5f0) fix: if no available address delete pod might failed related to #155
 * [bbd4257d](https://github.com/kubeovn/kube-ovn/commit/bbd4257d0236e69d7f0b480e22161ca10ea4ae3c) kind: support reload kube-ovn component in kind cluster
 * [d0479e90](https://github.com/kubeovn/kube-ovn/commit/d0479e9035aa63d64864182176a8ee293872af02) perf: filter pod in informer list-watch and disable resync
 * [61a7a7b9](https://github.com/kubeovn/kube-ovn/commit/61a7a7b9c12d8ff364366f94eecfdd9b8479e41b) fix: index out of range err when create lsp
 * [623661ef](https://github.com/kubeovn/kube-ovn/commit/623661ef28db2444f1ef6a4242357e59c251e449) prepare for next release
 * [1643c7f0](https://github.com/kubeovn/kube-ovn/commit/1643c7f0da3c472e5265584960ad379adfe9f8bd) kind: support to install kube-ovn in kind
 * [9611599f](https://github.com/kubeovn/kube-ovn/commit/9611599f1d447f911d20b6fad5b21a28de960bee) fix: mount /var/run/netns that kind will use it to store network ns files

### Contributors

 * Mengxin Liu
 * qsyqian

## v0.9.1 (2019-12-02)

 * [5d4714c1](https://github.com/kubeovn/kube-ovn/commit/5d4714c1e042af91a51450784ac00c7518d414bf) release v0.9.1
 * [847ef8b0](https://github.com/kubeovn/kube-ovn/commit/847ef8b088b94041d1c435af66016a4b78fde376) fix: block subnet deletion when there any ip in use
 * [e0fbfea6](https://github.com/kubeovn/kube-ovn/commit/e0fbfea64ccd93c376083c97dca282dfa764d5ec) fix: nbctl need timeout to avoid hang infinitely
 * [dd63c5a4](https://github.com/kubeovn/kube-ovn/commit/dd63c5a41e830d9fbfae7af1ae2d4ea9c3b324e3) fix: when delete node recycle related ip/route resource
 * [4d0ad6c7](https://github.com/kubeovn/kube-ovn/commit/4d0ad6c7bff32bd667c27062ddea48d0dbd929f8) fix typo in start-ovs.sh
 * [646a177c](https://github.com/kubeovn/kube-ovn/commit/646a177ca778103b5c880cb2dc837a46072dcf47) fix: use ep.subset.port.name to infer target port number
 * [9ae58a81](https://github.com/kubeovn/kube-ovn/commit/9ae58a81ae021948457860e805b2d842b494dda5) fix image tag
 * [3b793d4a](https://github.com/kubeovn/kube-ovn/commit/3b793d4ac20493447d47646553dcfa776644075f) fix: mount /var/run/netns that kind will use it to store network ns files
 * [093770dd](https://github.com/kubeovn/kube-ovn/commit/093770dd5d453188fa6a4bcb68ad91727cc78e77) fix: index out of range err when create lsp

### Contributors

 * Mengxin Liu
 * qsyqian

## v0.9.0 (2019-11-22)

 * [53db261a](https://github.com/kubeovn/kube-ovn/commit/53db261a12444f89b3520a1b2528c7f08063ce6b) release: v0.9.0
 * [1984cbe8](https://github.com/kubeovn/kube-ovn/commit/1984cbe8010bc52d49989caa336076b2675110e3) feat: when use nodelocaldns do not nat the address
 * [446999f4](https://github.com/kubeovn/kube-ovn/commit/446999f4cd022e4540f0239a346bd17178f4f9af) docs: add description about relation of cidr and static ip allocation
 * [6f1854f9](https://github.com/kubeovn/kube-ovn/commit/6f1854f9f58cc94cfc74b71eebf89f2770e0509b) Check the short name of kubernetes services which is independant of the cluster domain name.
 * [c6f8efeb](https://github.com/kubeovn/kube-ovn/commit/c6f8efeb059208c766fe3585558c8f8c38439b58) fix: some grafana modification
 * [40144160](https://github.com/kubeovn/kube-ovn/commit/40144160d2926628d27a7766d3332dd7569342f9) fix: add missing cap
 * [7c464d69](https://github.com/kubeovn/kube-ovn/commit/7c464d692f7e3a80c47aa642034645b0ea70e28d) chore: update ovn and other minor fix
 * [ac537152](https://github.com/kubeovn/kube-ovn/commit/ac53715217327651264af87b93e627c9908d39a9) fix re-annotate namespaces when subnet deleted
 * [fe2f2612](https://github.com/kubeovn/kube-ovn/commit/fe2f2612a60164b1bbe049a4c5dbdcef984fc6d6) fix: add ingress_policing_burst to accurate limit ingress bandwidth
 * [20b2c83d](https://github.com/kubeovn/kube-ovn/commit/20b2c83deab57801aaf2fce8b475463cefc991d2) fix: network unreachable when add egress qos for pod
 * [758dbc1c](https://github.com/kubeovn/kube-ovn/commit/758dbc1ce7c14b93bbf613f7385ea2e7f4bbb7c5) fix: err when add egress qos
 * [bdfd351d](https://github.com/kubeovn/kube-ovn/commit/bdfd351d738f849f26b186c398da11c9453080bc) fix: remove privilege=true from long run container
 * [0859da1f](https://github.com/kubeovn/kube-ovn/commit/0859da1fe5a2d0195a9ebd81a2c723e4b3be6d98) perf: optimize pod add
 * [3718851d](https://github.com/kubeovn/kube-ovn/commit/3718851df4087e92cdc2d68d2dd4592413c61742) fix: add keepalive to ovn-controller
 * [6ad98106](https://github.com/kubeovn/kube-ovn/commit/6ad981064336a19c441c4eceff2153066ba53273) feat: add controller metrics
 * [b87ed0ee](https://github.com/kubeovn/kube-ovn/commit/b87ed0eec24dde544770635e318c710a81adb0c7) If pod have not a status.PodIP skip add/del static route
 * [b9108fba](https://github.com/kubeovn/kube-ovn/commit/b9108fba4d0d6f26f7940d55f8c7236c4f5f3858) fix: ippool pod static route might lost during leader election
 * [a2e24de6](https://github.com/kubeovn/kube-ovn/commit/a2e24de62a553e791506a7b8679f72f94e161dea) fix: static route might lost during leader election
 * [8202a188](https://github.com/kubeovn/kube-ovn/commit/8202a188663e674eced3348ec1dac90d423930c2) feat: add grafana config and modify metrics.
 * [cae0ef27](https://github.com/kubeovn/kube-ovn/commit/cae0ef274857ada317cfe5092278c6f1b53e675c) fix: only keep the last iface-id
 * [f3528f23](https://github.com/kubeovn/kube-ovn/commit/f3528f237bcfc07b3b459bdd0a47a15077dd66d2) fix: add missing gc
 * [3791ba29](https://github.com/kubeovn/kube-ovn/commit/3791ba29d0adf66bd4511a3e35efc207cff2659c) fix: gc resource when start controller
 * [f970615b](https://github.com/kubeovn/kube-ovn/commit/f970615bb1a583b10f506a2f70510f45e293a08f) fix: watch will break if timeout is set
 * [ef285b21](https://github.com/kubeovn/kube-ovn/commit/ef285b213de5086980dc70eeb62542370e8e4427) feat: pinger add apiserver check metrics
 * [d33685e6](https://github.com/kubeovn/kube-ovn/commit/d33685e6403aea8838ab4f6ca2ccd04dadd9002e) fix: avoid conflict when init

### Contributors

 * Mengxin Liu
 * QIANSHUANGYANG [钱双洋]
 * Sébastien BERNARD
 * Yan Zhu

## v0.8.0 (2019-10-08)

 * [6b57f61b](https://github.com/kubeovn/kube-ovn/commit/6b57f61b34c18ee5815db7e595bca327967d8f68) release v0.8.0
 * [6ed722f9](https://github.com/kubeovn/kube-ovn/commit/6ed722f9439bd5987f232b6ab584b5c61ceefc33) fix: loss might be negative number
 * [7c0517b5](https://github.com/kubeovn/kube-ovn/commit/7c0517b5120770ff72ff35b45376457ae85084e4) feat: pinger prometheus support
 * [e23bd552](https://github.com/kubeovn/kube-ovn/commit/e23bd552f46bdb76065f869d4130533c670c55a9) feat: support pinger
 * [d837aa12](https://github.com/kubeovn/kube-ovn/commit/d837aa122b9d37728e3db349b1e5e9e1f9580c03) chore: update ovs/ovn
 * [4246cb74](https://github.com/kubeovn/kube-ovn/commit/4246cb74de513fb1267b512e19eedcb2e8b969e4) feat: gateway ha
 * [e27c9e54](https://github.com/kubeovn/kube-ovn/commit/e27c9e54df996c82319f91f8b255706b2ae8781c) chore: remove ovs-ipsec and update go to 1.13
 * [ba3084eb](https://github.com/kubeovn/kube-ovn/commit/ba3084ebc827b3fba3dbac367f92f705151445db) feat: add kubectl plugin
 * [54a465d1](https://github.com/kubeovn/kube-ovn/commit/54a465d161f9351c49e103261c716ba2b3568e68) docs: add comparison
 * [38be68d6](https://github.com/kubeovn/kube-ovn/commit/38be68d6f2a8c50c6116413a3f95d668de43f42e) fix: pod should be accessed from node when acl applied
 * [e62f0ab0](https://github.com/kubeovn/kube-ovn/commit/e62f0ab0412561d8607aca7843b899d2a1aac514) enable portmap by default to support hostport
 * [80de8e58](https://github.com/kubeovn/kube-ovn/commit/80de8e58d53a5e98fdbce89030637535e63b1ea8) feat: add port security to pod port
 * [4849f056](https://github.com/kubeovn/kube-ovn/commit/4849f05621dac0220d6db10c8bd3f52907035ee7) feat: add node switch allocated ip cr
 * [34e8406e](https://github.com/kubeovn/kube-ovn/commit/34e8406ee17ef7d28229887e9fd6557e782cdee8) prepare for next release

### Contributors

 * Mengxin Liu
 * MengxinLiu
 * Yan Zhu

## v0.7.0 (2019-08-21)

 * [933fd8d2](https://github.com/kubeovn/kube-ovn/commit/933fd8d2505cc813f0db18f05d4535ac7d530b15) release: bump v0.7.0
 * [7e2bdf52](https://github.com/kubeovn/kube-ovn/commit/7e2bdf522015a7678f7a05b0f6b7cbba3af5c72d) fix: add default excludeIps and check kern version
 * [31544abb](https://github.com/kubeovn/kube-ovn/commit/31544abb510f615172dc19eaeb96e342b80de222) fix: deal with ipv6 connection str
 * [0f8f2aad](https://github.com/kubeovn/kube-ovn/commit/0f8f2aad7492fa295f680faeb4c65e14b5ed8a2a) fix missing condition when subnet is private
 * [d37da1bc](https://github.com/kubeovn/kube-ovn/commit/d37da1bc48c130221c7f3631a7e7e2d8b4b25948) add subnet status
 * [4a5c5498](https://github.com/kubeovn/kube-ovn/commit/4a5c5498345af803e6ebd16d7bed648134855be1) fix: acl related issues
 * [62a395e6](https://github.com/kubeovn/kube-ovn/commit/62a395e6f242c96f4af81be6f93781bcb7508326) Revert "add subnet status field"
 * [b8f1d9ef](https://github.com/kubeovn/kube-ovn/commit/b8f1d9ef0fef618ee14bb33dd18bce560ac084a5) add missing subnets/status operation permission
 * [6c119ad1](https://github.com/kubeovn/kube-ovn/commit/6c119ad1e2d784ab5b6397e722c8302867b29d38) Update cleanup.sh
 * [b08ece4f](https://github.com/kubeovn/kube-ovn/commit/b08ece4fbca124d97ed83f9c6ac195ae84a4f64e) feat: add exclude_ips annotation to namespace
 * [a2774ed0](https://github.com/kubeovn/kube-ovn/commit/a2774ed0e44a48e17efa0ea0bbb5b32277ea5430) fix: use pg-del to remove pg and acl, check if ports is empty before set pg
 * [422c6dc0](https://github.com/kubeovn/kube-ovn/commit/422c6dc0001108bb4bf394baf8b903ee1baa30ea) add subnet status
 * [fde683ea](https://github.com/kubeovn/kube-ovn/commit/fde683eaf8e8574f6d53867478b955310a248594) feat: add subnet annotation to ns and automatically unbind ns from subnet.
 * [948e1306](https://github.com/kubeovn/kube-ovn/commit/948e130638f8870b7919d58229d1e6efd18d7c5d) docs: add cn docs link
 * [5278e105](https://github.com/kubeovn/kube-ovn/commit/5278e1051139d986e78f04c32a1a95074787f61a) feat: add default values to subnet
 * [ea451a1a](https://github.com/kubeovn/kube-ovn/commit/ea451a1aace30bbf35b93ddc14978c5dcab32322) write back subnet name to ip label
 * [1c7121db](https://github.com/kubeovn/kube-ovn/commit/1c7121dbb9ade6aa0edb362050223cbed2ad8b6d) chore: enable mirror in yaml and modify docs
 * [db9783a3](https://github.com/kubeovn/kube-ovn/commit/db9783a3a5d045814250110d9d111a54277a622e) fix: duplicate import in network_policy.go
 * [8a57747e](https://github.com/kubeovn/kube-ovn/commit/8a57747ef794ea6fb9c570992e0d023e01bc3a76) fix: improve cni-conf name priority
 * [5f1436be](https://github.com/kubeovn/kube-ovn/commit/5f1436bef28b59fcc8a6a2b1b66b6b0a746633c2) fix: wait subnet ready before start worker.
 * [661387ef](https://github.com/kubeovn/kube-ovn/commit/661387efea058278baf503add43d53ef4cc4d03d) fix: check ls exists before handle it
 * [9e05f533](https://github.com/kubeovn/kube-ovn/commit/9e05f53333afd65c6cb122200a3a6d21b1aa8c6d) docs: add more installation tools.
 * [dccb93c7](https://github.com/kubeovn/kube-ovn/commit/dccb93c7b347613dcae28c59a347651d2188582d) docs: add support os and notes.
 * [c6a160b3](https://github.com/kubeovn/kube-ovn/commit/c6a160b3f09df6a20fd48abe9a5ec338fa5792ef) Update subnet.md
 * [31ad00bd](https://github.com/kubeovn/kube-ovn/commit/31ad00bd75a3e1fe4aaa4f7993b56e9ec1b94163) feat: add ip info to ip crd
 * [ad7b5c2f](https://github.com/kubeovn/kube-ovn/commit/ad7b5c2f67d885c8e7ad6c1067d8b5ddc3d663cf) feat: update logo
 * [44c3077c](https://github.com/kubeovn/kube-ovn/commit/44c3077c7b0153944861acf2c20b1363819cb39c) feat: add logo
 * [55d7fd6f](https://github.com/kubeovn/kube-ovn/commit/55d7fd6f791357d666cca3bc24d11e3e96485acf) feat: reserve vport for statefulset pod
 * [7a3c8a6a](https://github.com/kubeovn/kube-ovn/commit/7a3c8a6a8f7be35af6ab07d0363d2484dd4462d5) docs: add crd installation
 * [aa016c1b](https://github.com/kubeovn/kube-ovn/commit/aa016c1b0220f51a3a273acc44bd5c9d48aadde2) fix: modify default header length
 * [85b40690](https://github.com/kubeovn/kube-ovn/commit/85b4069077dcc52600ccdca26bda24e9b5a723f8) fix: do not create exist logical switch
 * [36294366](https://github.com/kubeovn/kube-ovn/commit/3629436602367fcd8e4077a292859da59eb335a5) chore: prepare for next release

### Contributors

 * MengxinLiu
 * Yan Zhu
 * ftiannew
 * halfcrazy
 * shuangyang.qian

## v0.6.0 (2019-07-22)

 * [463d6253](https://github.com/kubeovn/kube-ovn/commit/463d6253a4e5362496d2b9fd5f90d7c39ecf87e4) docs: add crd/ipv6 docs and bump version 0.6.0
 * [103c23af](https://github.com/kubeovn/kube-ovn/commit/103c23af64b08998768770b512b894e609456528) fix build error
 * [9d173ba0](https://github.com/kubeovn/kube-ovn/commit/9d173ba0c28dc84b322e4ea729edf6d7972f9100) feat: support ipv6-only mode
 * [05566017](https://github.com/kubeovn/kube-ovn/commit/05566017cb41850fe1958c0ac392017f6f4bbeab) add webhook docs
 * [766cec9b](https://github.com/kubeovn/kube-ovn/commit/766cec9b8435ae3a2b2f37f1fdd0056c9c582ebf) add admission webhook for static ip
 * [2abeacb4](https://github.com/kubeovn/kube-ovn/commit/2abeacb4a85b7db613dcfe96106ddda3a57b059b) docs: add support platform version
 * [ed7264ea](https://github.com/kubeovn/kube-ovn/commit/ed7264ea91cafe6201ad48559fc9ac7dfed46dea) feat: use subnet crd to manage logical switch
 * [1e5c9f6c](https://github.com/kubeovn/kube-ovn/commit/1e5c9f6cbdc5b120da2a528e99883796281fd5a6) Use k8s hostname, fix #60
 * [87367295](https://github.com/kubeovn/kube-ovn/commit/873672952e945cfc90e9e1cba0630d6551cab644) fix: remove dependency on cluster-admin
 * [e0864a03](https://github.com/kubeovn/kube-ovn/commit/e0864a03d012a311e5ff3ae854d86a7cafabc790) chore: use go mod to replace dep
 * [96ec620d](https://github.com/kubeovn/kube-ovn/commit/96ec620d924568a67d482b003cba61627930c64a) docs: update mirror feature to readme
 * [855d834f](https://github.com/kubeovn/kube-ovn/commit/855d834fa9721ebad93ed26d3fdfb88d8a0a8c39) feat: support traffic mirror
 * [d1c3ea85](https://github.com/kubeovn/kube-ovn/commit/d1c3ea853f935414eb0f34baadce8b377996ff62) prepare for next release

### Contributors

 * MengxinLiu
 * Yan Zhu

## v0.5.0 (2019-06-07)

 * [782e04be](https://github.com/kubeovn/kube-ovn/commit/782e04bee9922016c2332602145f45cd186fc7c3) chore: bump v0.5.0
 * [a27f8339](https://github.com/kubeovn/kube-ovn/commit/a27f8339456a5264611af4dbbe5482e42fdd1dc1) fix: wrong mtu
 * [44707167](https://github.com/kubeovn/kube-ovn/commit/4470716766a6aa3da3ac6453aae2992846301c58) feat: support user define iface and mtu
 * [f8d8e186](https://github.com/kubeovn/kube-ovn/commit/f8d8e186fee449dc7f1549a046566719c19c8047) fix: remove mask field from ip annotation
 * [55090404](https://github.com/kubeovn/kube-ovn/commit/5509040475fe64297c5e68a7e8f45b4dcc61be9e) feat: auto assign gw for controller config and expose more cmd args
 * [48da0fe1](https://github.com/kubeovn/kube-ovn/commit/48da0fe177dab436450650d1e346431be349bd03) feat: add pprof and use it as probe
 * [8984c90b](https://github.com/kubeovn/kube-ovn/commit/8984c90b567750195b72b694d1a230bfdceb1c8a) feat: set kernel args when start cniserver
 * [208a1dfc](https://github.com/kubeovn/kube-ovn/commit/208a1dfc797552d24bc385e2873198e8ae9f851f) feat: support network policy
 * [c8d208fb](https://github.com/kubeovn/kube-ovn/commit/c8d208fbdfdf00e18653f9cba735b35cd03f74fd) prepare for next release

### Contributors

 * MengxinLiu

## v0.4.1 (2019-05-27)

 * [5a2cb093](https://github.com/kubeovn/kube-ovn/commit/5a2cb093c6eddb1c450b74f1c942c153341a40bb) bump version to v0.4.1
 * [f8e8b001](https://github.com/kubeovn/kube-ovn/commit/f8e8b00114af09ca62a8e00eb28623640a73ec87) fix: manual static ip allocation and automatic allocation should use different ip validation
 * [031924d1](https://github.com/kubeovn/kube-ovn/commit/031924d10dbcda61deb217fa4f3efe1b13748f6b) Fix json: cannot unmarshal string into Go value of type request.PodResponse https://github.com/alauda/kube-ovn/issues/33
 * [24259dbf](https://github.com/kubeovn/kube-ovn/commit/24259dbf95af068990e0d1ebf52971a0d928c0d4) fix: use ovsdb-client to get leader info
 * [3541b6cf](https://github.com/kubeovn/kube-ovn/commit/3541b6cf747dc33d7667acb467e5a1a0f86c8dbc) fix: use default-gw as default-exclude-ips and expose args to docs
 * [69c48538](https://github.com/kubeovn/kube-ovn/commit/69c48538074d0e002c444207fbd68995f63b8293) to cleanup all created resources, not only kube-ovn namespace.
 * [9361bb43](https://github.com/kubeovn/kube-ovn/commit/9361bb4383ccbea67c63858150366c439fb92360) prepare for next release

### Contributors

 * MengxinLiu
 * Yan Zhu
 * fanbin

## v0.4.0 (2019-05-16)

 * [509bf4a4](https://github.com/kubeovn/kube-ovn/commit/509bf4a4168f95e99e86ce8d4375967b50b66a64) feat: bump version to 0.4.0
 * [2e414519](https://github.com/kubeovn/kube-ovn/commit/2e414519755441d90e0947a4337bd4b72fbf29d1) feat: support expose pod ip to external network
 * [8992bbe3](https://github.com/kubeovn/kube-ovn/commit/8992bbe3833e1f9ac80b0aa95fafcd7d173d0000) fix: check conflict subnet cidr
 * [0f9d1e4b](https://github.com/kubeovn/kube-ovn/commit/0f9d1e4be28947269394d44747f7bea06ece8911) fix: start informer when controller is leader
 * [71c15d65](https://github.com/kubeovn/kube-ovn/commit/71c15d65b0ed3ce09f0296fa897c8a4c1957445d) feat: validate namespace/pod annotations
 * [89491b57](https://github.com/kubeovn/kube-ovn/commit/89491b5702ac3dbffca10c7b7e38e0fe2165c48a) fix: wait node-gw info ready
 * [0d86393d](https://github.com/kubeovn/kube-ovn/commit/0d86393d298725de08b28401cbf094811eee0e93) fix: use ovn/ovs-ctl to health check
 * [278ccfe5](https://github.com/kubeovn/kube-ovn/commit/278ccfe5e94e208fa2f8e812c01ea4a9fa992070) feat: remove finalizer dependency improve svc performance
 * [8f962673](https://github.com/kubeovn/kube-ovn/commit/8f962673a7ee83bdb9706ebc2548885f4661aca5) fix: reuse node ip and mac annotation
 * [b8f85143](https://github.com/kubeovn/kube-ovn/commit/b8f85143b6571462845fc4487966ea33cbfbcbe6) Add ha for ovn dbs and simplify makefile
 * [3c617451](https://github.com/kubeovn/kube-ovn/commit/3c617451efe78b46acddcf012e00d049bf722171) feat: merge ovn-nbctl request
 * [b5ac7da4](https://github.com/kubeovn/kube-ovn/commit/b5ac7da4a5c9a1f50a1065440abb0660440d2aaf) feat: separate ip pool pod and add parallelism to workers
 * [ce105dff](https://github.com/kubeovn/kube-ovn/commit/ce105dffb1a6634bc414ed33030ee61ef507c106) Mute logrus log for ipset Dont need to change the vendored code.
 * [657470c8](https://github.com/kubeovn/kube-ovn/commit/657470c8f27de71451c81639e64d148b56354829) Fix klog cant use V module The side affect of this commit is glog's V module not work.
 * [5429f51b](https://github.com/kubeovn/kube-ovn/commit/5429f51bea2157f321ada9400463df85db41f342) feat: use ovn macam to allocate mac for static ip pod
 * [5a8958cd](https://github.com/kubeovn/kube-ovn/commit/5a8958cdb18d4907668db16191e98abc1d9b1cba) feat: update ovn to 2.11.1
 * [ca036f9e](https://github.com/kubeovn/kube-ovn/commit/ca036f9e7eb9a65b3641fb98c6c744454880897a) Add vagrantfile
 * [660c0570](https://github.com/kubeovn/kube-ovn/commit/660c0570cf2130b9bddb2d5706af5fc4caad3b41) fix: use tag version yaml url
 * [bc66671c](https://github.com/kubeovn/kube-ovn/commit/bc66671c511f64cdd8c9d76588f8586464434b89) chore: fix go-report golint issues
 * [12a4bec9](https://github.com/kubeovn/kube-ovn/commit/12a4bec93e7b655e439af8e2903e895d62a7b047) ha for kube-ovn-controller
 * [b7d0f599](https://github.com/kubeovn/kube-ovn/commit/b7d0f599e153f2c39cc96af59425c57db3e62ad1) cleanup unused code
 * [756831d7](https://github.com/kubeovn/kube-ovn/commit/756831d7213846d8c9676775a02a6d505c67dfd5) docs: add network topology
 * [c0559487](https://github.com/kubeovn/kube-ovn/commit/c055948732089450464c747842c5914703762d1b) chore: Minor updates to gateway.md
 * [21e34e9f](https://github.com/kubeovn/kube-ovn/commit/21e34e9f217a64ef7bd8c078150d5c2e199542e8) chore: Gateway documentation touch-ups
 * [aa0b2b7c](https://github.com/kubeovn/kube-ovn/commit/aa0b2b7c147c3108c0e1315870a83393efcb6d7a) chore: QoS documentation touch-ups
 * [3ec0098a](https://github.com/kubeovn/kube-ovn/commit/3ec0098a4e0ab10b6924a58bcf18d04538b29ef1) chore: Subnet Isolation documentation touch-ups
 * [524845e9](https://github.com/kubeovn/kube-ovn/commit/524845e93c7197e3b61e50949012e5ab4dd30e14) chore: Static IP documentation touch-up
 * [b510016c](https://github.com/kubeovn/kube-ovn/commit/b510016c2916b91b16930a2c36a6e41122b2c0a1) chore: Subnet documentation touch-ups
 * [524f7d3f](https://github.com/kubeovn/kube-ovn/commit/524f7d3ff3ce02b4f6261c4e33b0137a935b4431) chore: Installation Guide touch-ups
 * [a1995d03](https://github.com/kubeovn/kube-ovn/commit/a1995d0374f47e8ab932fa09ed83fc690ec6bcb0) chore: README touch-up.

### Contributors

 * Kai Chen
 * MengxinLiu
 * Yan Zhu

## v0.3.0 (2019-04-19)

 * [79c0642e](https://github.com/kubeovn/kube-ovn/commit/79c0642eba7b8146269262ce2d011f1c725ef1a3) docs: bump version
 * [cb2f50da](https://github.com/kubeovn/kube-ovn/commit/cb2f50da4639aa02e6bde5acc26874d3a08a47ed) fix: acl rule error
 * [1a6f492a](https://github.com/kubeovn/kube-ovn/commit/1a6f492ad603cdc1ac34992fb502f790d86c9fdb) fix: init node gw before run controller
 * [75c514a1](https://github.com/kubeovn/kube-ovn/commit/75c514a19a633c72068d094eae7fc31d25a5ec25) fix: external dns issues
 * [13068892](https://github.com/kubeovn/kube-ovn/commit/130688927a4282bb8c9c662f09a74bbefca77ef2) feat: use daemon ovn-nbctl to improve performance and cleanup unused dns code
 * [24cda418](https://github.com/kubeovn/kube-ovn/commit/24cda418cd50af8173bff0e3698ddf7780bb1c53) Implement centralized gateway.
 * [890934f4](https://github.com/kubeovn/kube-ovn/commit/890934f473772c26453c49bca93a1fcb57dbc962) chore: migrate from bitbucket to github

### Contributors

 * MengxinLiu
 * Yan Zhu

## v0.2.0 (2019-04-15)

 * [adf655cb](https://github.com/kubeovn/kube-ovn/commit/adf655cb95e57f4b9e9921dd1074dfa517a88fc5) remove dns from ls and bump new version
 * [ca21c6cb](https://github.com/kubeovn/kube-ovn/commit/ca21c6cb1f50fb3b313f1104cadc5c0baf7deb73) make filter table forward chain default accept
 * [cd0ddf10](https://github.com/kubeovn/kube-ovn/commit/cd0ddf10a39cf3e95bcff11dd9b0d37075968891) ipset exclude cluster service ip range
 * [1d753c8e](https://github.com/kubeovn/kube-ovn/commit/1d753c8e1d189bb9a05500365be53618207b0e1c) fix: lb bugs
 * [cb91d984](https://github.com/kubeovn/kube-ovn/commit/cb91d9842e9cf5fdfa9817a4da94c868cf786a6c) read cidr from ns annotation
 * [e9998332](https://github.com/kubeovn/kube-ovn/commit/e9998332991445ed4a9664071af1601fc6b57af6) fix: remove dns table from nodeswitch and remove unused other_config:namespace
 * [049cab2c](https://github.com/kubeovn/kube-ovn/commit/049cab2c114f3fdca4b0b4b18fcd1a3515c2d855) fix pod has no ip
 * [170c3c63](https://github.com/kubeovn/kube-ovn/commit/170c3c63433cb75c8d522dcd7256e09bf059dcd4) Distributed gateway implement
 * [cebb8dfd](https://github.com/kubeovn/kube-ovn/commit/cebb8dfda9f7ee72a3b66d92066a60c9fe0a10ac) fix: clean lost interface.
 * [4367ba07](https://github.com/kubeovn/kube-ovn/commit/4367ba079c268d163b08c282af510ccc4ee0beb0) feat: support subnet isolation
 * [1fe8c916](https://github.com/kubeovn/kube-ovn/commit/1fe8c916dcb6f678fa8dcde28ff570ce63a456c5) feat: support dynamic qos
 * [e04bc093](https://github.com/kubeovn/kube-ovn/commit/e04bc09397a922e60e5b13be7192f80816624d98) fix: ovn restart issues
 * [014f1dcf](https://github.com/kubeovn/kube-ovn/commit/014f1dcf531e8eb6be43a9d6d28e6cc952111f38) fix: ovn restart issues
 * [3e78ddc3](https://github.com/kubeovn/kube-ovn/commit/3e78ddc375a51a96bfb711a96d70912c10dafd60) fix: validate namespace switch annotations
 * [44eafc50](https://github.com/kubeovn/kube-ovn/commit/44eafc505a05bfebea49a7eaf8f09f98f4c7a885) fix lint && add docker build
 * [cb3e01a4](https://github.com/kubeovn/kube-ovn/commit/cb3e01a4ef7c599ac78e40e802fd4a7346001dba) feat: update yaml, add readiness/liveness probe, add pass shell args
 * [004deefd](https://github.com/kubeovn/kube-ovn/commit/004deefd9dd061e73d9a54ee2721afab4ee8ecf2) feat: support qos
 * [d37264e4](https://github.com/kubeovn/kube-ovn/commit/d37264e4547ef51e3bd533aba33525a2121aa0e6) feat: add simple gateway implementation

### Contributors

 * Mengxin Liu
 * MengxinLiu
 * Yan Zhu

