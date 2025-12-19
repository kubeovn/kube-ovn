# Changelog

## v1.14.22 (2025-12-19)

 * [a5ec31143](https://github.com/kubeovn/kube-ovn/commit/a5ec31143a103abbbb0f0e438185b71678ad07b3) release v1.14.22
 * [c6e41bd81](https://github.com/kubeovn/kube-ovn/commit/c6e41bd8145d25921c3565e03ccc89462e9b3186) libovsdb: set inactivity timeout only when passed in value is greater than zero (#6078)
 * [0bd0b3f34](https://github.com/kubeovn/kube-ovn/commit/0bd0b3f34bce5aef18f82f4cf0b2874debbfafd1) bump k8s to v1.32.11 (#6063)
 * [1857e3d44](https://github.com/kubeovn/kube-ovn/commit/1857e3d4447f6fa802996070fc4ddcd5f797cd9b) prepare for next release

### Contributors

 * Mengxin Liu
 * 张祖建

## v1.14.21 (2025-12-19)

 * [0be1d9ea1](https://github.com/kubeovn/kube-ovn/commit/0be1d9ea1e21f4b89778d0de2be660898203d351) release v1.14.21
 * [ec6279d2c](https://github.com/kubeovn/kube-ovn/commit/ec6279d2c04b82cb271f8522a1b3e3991d4d4dee) fix occasional migration failures caused by timing issues. (#6066)
 * [19153d07f](https://github.com/kubeovn/kube-ovn/commit/19153d07fa8be6711ec71cdddc2ec95057b96fde) bump libovsdb to v0.8.1 (#5596)
 * [490404903](https://github.com/kubeovn/kube-ovn/commit/490404903fe11a57120df0b322bc79b1d06935ee) ovsdb: exit clustered ovsdb server if multiple raft leaders found (#6065)
 * [74fd118e7](https://github.com/kubeovn/kube-ovn/commit/74fd118e777730f27f32968c33bdce3332d20d43) fix(deps): update module libovsdb (#6046)
 * [291048db4](https://github.com/kubeovn/kube-ovn/commit/291048db4b83777df483f1421672498bd7f97ba4) 跟踪日志发现在eip绑定qos以及修改qos的过程中，有v4ip为空的情况也会执行nat网关的tc规则，v4ip为空规则解析错误，排除该场景。 (#6055)
 * [248d48135](https://github.com/kubeovn/kube-ovn/commit/248d4813582f8f0d3ba57653e45b1cd29bc5f291) lint: skip generated files (#6049)
 * [1739a3172](https://github.com/kubeovn/kube-ovn/commit/1739a3172585a26af39f772c5641a8d5cdb1c3f2) prepare for next release

### Contributors

 * Mengxin Liu
 * changluyi
 * mengyu0829
 * 张祖建

## v1.14.20 (2025-12-15)

 * [fd2489a8a](https://github.com/kubeovn/kube-ovn/commit/fd2489a8a79c1d90cabfa54a0f6e2582cb8f6762) release v1.14.20
 * [321b5dfd2](https://github.com/kubeovn/kube-ovn/commit/321b5dfd2df621bfc79692e8a9f15ce9444c2b90) cni: disable ipv6 RA (#6045)
 * [958ed8619](https://github.com/kubeovn/kube-ovn/commit/958ed86191c459144756b1e4764b6bc6b3b1bb7d) fix: double quoting causing nxdomain (#6039)
 * [a009a60fc](https://github.com/kubeovn/kube-ovn/commit/a009a60fc68b12931d0f014f37b10b5facffc54e) prepare for next release

### Contributors

 * Bryan Lee
 * Mengxin Liu
 * 张祖建

## v1.14.19 (2025-12-10)

 * [06e6fe026](https://github.com/kubeovn/kube-ovn/commit/06e6fe02647617ca4fa38e12153a8adada497f13) release v1.14.19
 * [f2aba5935](https://github.com/kubeovn/kube-ovn/commit/f2aba59356c08814250c2461533dd5bdc42284d0) Fix empty subnet and err try (#5963)
 * [db005248f](https://github.com/kubeovn/kube-ovn/commit/db005248f5b2990db01cb622996dd77c39fb7598) remove no need code
 * [145f1a5bf](https://github.com/kubeovn/kube-ovn/commit/145f1a5bf963339d5e5082fe56bb7b98bb3b036f) add auto create vlan sub interface (#5966)
 * [96dae3766](https://github.com/kubeovn/kube-ovn/commit/96dae37661002d5856c5ca085432c333e6df19be) fix(deps): update module github.com/containernetworking/plugins to v1.9.0 [security] (#6027)
 * [d1dd363c6](https://github.com/kubeovn/kube-ovn/commit/d1dd363c6be03700e51a004a1b706b40910bce24) chore(deps): update dependency cni-plugin to v1.9.0 (#6024)
 * [6d4bb4948](https://github.com/kubeovn/kube-ovn/commit/6d4bb4948c56f7be2ba4684f9bdbd2a916618ce5) enable_ipforward for vpc_nat_gateway
 * [320001864](https://github.com/kubeovn/kube-ovn/commit/320001864b7695b4dac61573353485a3a2a76afd) ovn process skip non-ovn subnet (#6018)
 * [0c9931de9](https://github.com/kubeovn/kube-ovn/commit/0c9931de9f23e0140cc6f2b6d169d543999756e3) fix(deps): update module golang.org/x/tools to v0.40.0 (#6016)
 * [36373b54b](https://github.com/kubeovn/kube-ovn/commit/36373b54b8f312dc82d3b9d48ca3b9e488be8700) fix(deps): update golang (#6015)
 * [69d6aeda2](https://github.com/kubeovn/kube-ovn/commit/69d6aeda2f20052855e11b78aa21d2661529a411) make sure ip in subnet using range (#6005)
 * [de5b4bac2](https://github.com/kubeovn/kube-ovn/commit/de5b4bac2ded286865330ae1486c0072dcd91d1a) 修复vip无法正常固定ip (#5865)
 * [446acc67d](https://github.com/kubeovn/kube-ovn/commit/446acc67d6d9472ff2b1bbeb1520e75db280b34a) prepare for next release

### Contributors

 * Mengxin Liu
 * changluyi
 * clyi
 * lfpython
 * renovate[bot]
 * zbb88888

## v1.14.18 (2025-12-05)

 * [56c22a057](https://github.com/kubeovn/kube-ovn/commit/56c22a057bb2faada066f26b3a22f0188849957a) release v1.14.18
 * [1a5ea437b](https://github.com/kubeovn/kube-ovn/commit/1a5ea437b2175aa0506bbd538d8b7bb42ef49628) Revert "gc interface if it's not exist in podLister (#5789)"
 * [9d5233980](https://github.com/kubeovn/kube-ovn/commit/9d5233980f60efefecf8cd4dcd869730bfcfc6f0) prepare for next release

### Contributors

 * Mengxin Liu

## v1.14.17 (2025-12-03)

 * [6fc038614](https://github.com/kubeovn/kube-ovn/commit/6fc0386147972d167a191b115d3ed38530031162) release v1.14.17
 * [b051d3d49](https://github.com/kubeovn/kube-ovn/commit/b051d3d499ab727ea602265c9762e4e475e57618) chore(deps): update dependency go to v1.25.5 (#5982)
 * [98ce1f01e](https://github.com/kubeovn/kube-ovn/commit/98ce1f01ebc019b18c62283ac4c985ebe4f389f4) vpc egress gateway: add support for specifying tolerations (#5978)
 * [a74387c64](https://github.com/kubeovn/kube-ovn/commit/a74387c6450844ca48d5be08a9ca57a1bab7f191) prepare for next release

### Contributors

 * Mengxin Liu
 * renovate[bot]
 * 张祖建

## v1.14.16 (2025-12-02)

 * [58ae02b81](https://github.com/kubeovn/kube-ovn/commit/58ae02b81e3cc1d00318282acc5a2861e53b1e05) release v1.14.16
 * [8ee4c16b1](https://github.com/kubeovn/kube-ovn/commit/8ee4c16b142ab5d9fd3a54f415e6d7948a66f4c6) security: only approve validated CSR (#5972)
 * [afea2bcd9](https://github.com/kubeovn/kube-ovn/commit/afea2bcd9a99660107472a119801539b5dae0bbe) security: disable hostPID for pinger and cni-server (#5973)
 * [f17902641](https://github.com/kubeovn/kube-ovn/commit/f17902641c4f605a13859d974aef98d804e27870) cni-server: reduce redundant logging (#5975)
 * [8886b913b](https://github.com/kubeovn/kube-ovn/commit/8886b913bca1cd8efb693909ce7e6ebbe0e6666f) gc interface if it's not exist in podLister (#5789)
 * [82e188fbf](https://github.com/kubeovn/kube-ovn/commit/82e188fbf81595ccce7898c7ea212cf7785096b3) security: set sticky bits on world-writable directories (#5971)
 * [0c27dbf45](https://github.com/kubeovn/kube-ovn/commit/0c27dbf455f52ad645ad5642da17429840f2eb1b) security: use security context to enable the docker/default seccomp profile in pod definitions (#5970)
 * [e6d1cf482](https://github.com/kubeovn/kube-ovn/commit/e6d1cf4822cc92746184e3a9e129e4ff3bd749c0) speaker: fix missing log directory (#5959)
 * [04c9a1d9e](https://github.com/kubeovn/kube-ovn/commit/04c9a1d9ef42192ab8c13deeef06fc4e0838df30) fix: correct V6Eip field assignment in OvnDnatRule status (#5957)
 * [5185e2363](https://github.com/kubeovn/kube-ovn/commit/5185e2363a42aafdc07ee4bbfb04e702b098bb1f) e2e: do not change join cidr and service cidr for ovn-ic tests (#5951)
 * [6bb183a55](https://github.com/kubeovn/kube-ovn/commit/6bb183a555128bec214075419619ded8ec9a9d66) cni-server: configure sysctl parameters on demand (#5950)
 * [98a66562f](https://github.com/kubeovn/kube-ovn/commit/98a66562f9abeee62bca593ab7bfba9d994a13c6) feat(install): split kube-ovn-pinger, keep version in the head, reload multus,  enable control not del existing pod (#5909)
 * [226794b68](https://github.com/kubeovn/kube-ovn/commit/226794b68f5d74efe5a54f7e5c534280c97be0b7) fix: handle missing NAD during pod deletion (#5929)
 * [d5b0c5e82](https://github.com/kubeovn/kube-ovn/commit/d5b0c5e82143b51f3986dfb73e88d9b7f7b6412c) fix: use the same cni dir as the mount (#5928)
 * [bcd6c9cbf](https://github.com/kubeovn/kube-ovn/commit/bcd6c9cbf871726278a7be3122f68b0556e25fda) fix: correct error log message in getDBStatus function (#5934)
 * [b7c5d198d](https://github.com/kubeovn/kube-ovn/commit/b7c5d198d963471d9409faa4aa0b12ccb86091d6) metrics: split subnet CIDR block for dual-stack subnets (#5931)
 * [ee172480c](https://github.com/kubeovn/kube-ovn/commit/ee172480c6c340cc57908c72480d1c39182761d2) security: fix C-0190 (#5927)
 * [a90889056](https://github.com/kubeovn/kube-ovn/commit/a90889056000f43f46cd010ef4972443be613e45) get pods just before using and add some log (#5922)
 * [48b78ff84](https://github.com/kubeovn/kube-ovn/commit/48b78ff84cc9d1aad4da3d764f22546ef4cf98c3) optimize log output (#5916)
 * [f0f2d8a84](https://github.com/kubeovn/kube-ovn/commit/f0f2d8a849f6203947eb63db91fc198a70e33e41) prepare for next release

### Contributors

 * Bryan Lee
 * Congqi Zhao
 * DiMalovanyy
 * Hargrove Wang
 * Mengxin Liu
 * zbb88888
 * zhangzujian
 * 张祖建

## v1.14.15 (2025-11-17)

 * [b31418851](https://github.com/kubeovn/kube-ovn/commit/b31418851ec95e89d1e39f2b37c960ec23be75aa) release v1.14.15
 * [f039dcf97](https://github.com/kubeovn/kube-ovn/commit/f039dcf9774353d6ec5b33a557914289f7e453ac) cni-server: set cni config file permission to 600 (#5906)
 * [2818ee850](https://github.com/kubeovn/kube-ovn/commit/2818ee8507d8c2ca768119afe21d3c1cbb47f4ef) fix go cache key and restore keys
 * [896b19166](https://github.com/kubeovn/kube-ovn/commit/896b19166aee72835a58418ffc26fd6d9a211349) ci: bump actions/download-artifact to v6
 * [15a9cbf20](https://github.com/kubeovn/kube-ovn/commit/15a9cbf2061f5bbdce616497422bbac160b652ab) ci: bump actions/upload-artifact to v5
 * [5bf265580](https://github.com/kubeovn/kube-ovn/commit/5bf26558036c07b942d69d0e68f6841eb4dfdf45) ci: bump actions/setup-go to v6
 * [0e226312d](https://github.com/kubeovn/kube-ovn/commit/0e226312d3622b7367b976ee14ab4f5ed00a2daa) ci: bump actions/checkout to v5
 * [e8dbfd054](https://github.com/kubeovn/kube-ovn/commit/e8dbfd0546ff134223bd8af16979181a274505bb) fix: nat rule finalizer (#5806)
 * [b41fc7dd0](https://github.com/kubeovn/kube-ovn/commit/b41fc7dd012961d7b351dabeefd14f8fa46c32e5) prepare for next release

### Contributors

 * Mengxin Liu
 * zbb88888
 * zhangzujian
 * 张祖建

## v1.14.14 (2025-11-13)

 * [46eb7fe04](https://github.com/kubeovn/kube-ovn/commit/46eb7fe04e8ce87a210114b2777fe9342202609d) release v1.14.14
 * [f67115d54](https://github.com/kubeovn/kube-ovn/commit/f67115d5433384f688f9d13acab841aad44006af) fix(deps): update module golang.org/x/tools to v0.39.0 (#5904)
 * [30fae6ec1](https://github.com/kubeovn/kube-ovn/commit/30fae6ec161a205eadd92a90471dfbb7af12201a) fix(deps): update kubernetes to v1.32.10 (#5902)
 * [8cd2132d0](https://github.com/kubeovn/kube-ovn/commit/8cd2132d09432004c7ad615e54b70a84f2d3faf7) cni-server: detect if dbus is available (#5896)
 * [1de7fb3f4](https://github.com/kubeovn/kube-ovn/commit/1de7fb3f48fad64adc2d2a5313542769ae3a9e00)  update chart-v2 release script
 * [45e8b52bf](https://github.com/kubeovn/kube-ovn/commit/45e8b52bfbb687a319bf5488d8af1c4dcab6e26c) fix(deps): update golang (#5895)
 * [9b80b9c61](https://github.com/kubeovn/kube-ovn/commit/9b80b9c6100a424d35bef779b03c2d2a8e4a8267) server: add hostname and pod IPs into TLS certificate SAN (#5888)
 * [5b1dd6d30](https://github.com/kubeovn/kube-ovn/commit/5b1dd6d301647183a49a155dcea79a5bb1203761) fix version trim
 * [b2878dd4a](https://github.com/kubeovn/kube-ovn/commit/b2878dd4ad094cd06901fda9f33b2987267c1849) rollback permission
 * [263a8f373](https://github.com/kubeovn/kube-ovn/commit/263a8f373c6bf0165997b5b4716526bbdc43ecb2) prepare for next release

### Contributors

 * Mengxin Liu
 * renovate[bot]
 * 张祖建

## v1.14.13 (2025-11-11)

 * [76520efa5](https://github.com/kubeovn/kube-ovn/commit/76520efa519a407b634e4a59cedd40d1bb7c2561) release v1.14.13
 * [ffd57ec66](https://github.com/kubeovn/kube-ovn/commit/ffd57ec66e4e43d9994b1d8bb51274d0bf2cf3ce) fix metallb underlay lflow rule is deleted unexpected (#5723)
 * [11debb4d7](https://github.com/kubeovn/kube-ovn/commit/11debb4d7e6d2a2683c951d2076c1f76c8f50b66) fix: loadbalancerservice value (#5884)
 * [52a26737a](https://github.com/kubeovn/kube-ovn/commit/52a26737a703a95b9e34f661c0fc3d3a0a049791) fix(deps): update golang (#5880)
 * [25504091e](https://github.com/kubeovn/kube-ovn/commit/25504091efc3b14bbe630022ed75260256149fca) fix migrate failed (#5873)
 * [daa0d3133](https://github.com/kubeovn/kube-ovn/commit/daa0d3133e8d45db5b7038b279505e249903908f) return empty nodeips during chart rendering dry-run (#5760)
 * [2c7de8ac9](https://github.com/kubeovn/kube-ovn/commit/2c7de8ac9a0d11cf32eae1e5bf416d39a4b3c87e) update containerd
 * [872a0ece7](https://github.com/kubeovn/kube-ovn/commit/872a0ece71a53dbfc82dcdc267ecb030ec4366ee) fix release permission
 * [8463c9057](https://github.com/kubeovn/kube-ovn/commit/8463c9057e02f3491b1491eac1dfdf17471838c3) chore(deps): update dependency go to v1.25.4 (#5871)
 * [b8a4e1ec6](https://github.com/kubeovn/kube-ovn/commit/b8a4e1ec6f60b0b1f6d459dc3837f4a551081cd2) prepare for next release

### Contributors

 * Abhishek Pandey
 * Bryan Lee
 * Mengxin Liu
 * changluyi
 * renovate[bot]

## v1.14.12 (2025-11-05)

 * [a40eacfa8](https://github.com/kubeovn/kube-ovn/commit/a40eacfa84c25255215b34ab7c73e3ab9ad82452) release v1.14.12
 * [0fdb2f198](https://github.com/kubeovn/kube-ovn/commit/0fdb2f19803fae02aa17565977d555e005a87ec3) fix talos e2e failure on ipv4 (#5812)
 * [39c91c7f7](https://github.com/kubeovn/kube-ovn/commit/39c91c7f7ec1f5b7a75908d227502332100e0d80) Update the kubeovn_deny_all security group after the virtual machine … (#5831)
 * [13ce09f3c](https://github.com/kubeovn/kube-ovn/commit/13ce09f3c7de3cc017b438fd191aa4088b54f1e1) Add OCI registry support for Helm charts (#5837)
 * [77b60b90d](https://github.com/kubeovn/kube-ovn/commit/77b60b90d1953fb91950defba73a3511b74ffa61) clean migrate  state when migrate is done (#5833)
 * [57823ccbd](https://github.com/kubeovn/kube-ovn/commit/57823ccbd9ca05c955b72a1157264991f48ebabc) modify route priority for /32 src route
 * [7bcb504a0](https://github.com/kubeovn/kube-ovn/commit/7bcb504a0aaf6cc799e96159bb30b60994fcc4dc) chore(deps): update dependency go to v1.25.3 (#5793)
 * [9459b6797](https://github.com/kubeovn/kube-ovn/commit/9459b6797ca5981799f2f75a35b4cee5e07ff45d) prepare for next release

### Contributors

 * Copilot
 * Mengxin Liu
 * changluyi
 * narutoqq
 * renovate[bot]

## v1.14.11 (2025-10-11)

 * [879c670ea](https://github.com/kubeovn/kube-ovn/commit/879c670ea5f5bf9cccefd9b03f64307193c2122f) release v1.14.11
 * [f9c8743da](https://github.com/kubeovn/kube-ovn/commit/f9c8743da113fa2b58988bc12c9b3c9bc1d8b2fb) fix sometimes e2e metallb err (#5761)
 * [7be12912a](https://github.com/kubeovn/kube-ovn/commit/7be12912a92734696a04961d73b58ce3e380747d) bump k8s to v1.32.9 (#5780)
 * [fc113898e](https://github.com/kubeovn/kube-ovn/commit/fc113898e1a22ba363917a20aeeec452a4183cdd) change the route offset to increase the priority of dst routes (#5781)
 * [16df110ef](https://github.com/kubeovn/kube-ovn/commit/16df110ef996b5562896b56b1a3e35e72cadd457) fix(deps): update golang (#5774)
 * [8921d8864](https://github.com/kubeovn/kube-ovn/commit/8921d886444f8749d41df5d130f5a3b96dc652c0) prepare for next release

### Contributors

 * Mengxin Liu
 * changluyi
 * renovate[bot]
 * 张祖建

## v1.14.10 (2025-09-29)

 * [3b2bee7d0](https://github.com/kubeovn/kube-ovn/commit/3b2bee7d02ff49c381d743ac6d6ae1f59ea990d8) release v1.14.10
 * [8cb0a7875](https://github.com/kubeovn/kube-ovn/commit/8cb0a7875c07189ce1a5fd4107983dda93ff8d31) Increase timeout
 * [144b772ae](https://github.com/kubeovn/kube-ovn/commit/144b772ae7f780412471ea4338261a354a145b5a) fix gabage u2o openflow (#5757)
 * [74ca5981f](https://github.com/kubeovn/kube-ovn/commit/74ca5981fbbf63b6d4eedae01a0fe4591c62dca1) prepare for next release

### Contributors

 * Mengxin Liu
 * changluyi

## v1.14.9 (2025-09-23)

 * [238d0ef8b](https://github.com/kubeovn/kube-ovn/commit/238d0ef8b6dd49992f389bafea4ac378b617f287) release v1.14.9
 * [244e28268](https://github.com/kubeovn/kube-ovn/commit/244e2826835623c8fbde8ba91b406acf435da9e4) check ovn0 mac should be set expected (#5749)
 * [c85a490db](https://github.com/kubeovn/kube-ovn/commit/c85a490db05758d5e743915cd95353bbcf625de4) prepare for next release

### Contributors

 * Mengxin Liu
 * changluyi

## v1.14.8 (2025-09-22)

 * [12c136b8e](https://github.com/kubeovn/kube-ovn/commit/12c136b8e1ea149e7b73fa64d83a8b7b1412676b) release v1.14.8
 * [75a1eac11](https://github.com/kubeovn/kube-ovn/commit/75a1eac11164b6e782903bea698728403094a793) controller: fix lsp not deleted in gc (#5735)
 * [e33b6d386](https://github.com/kubeovn/kube-ovn/commit/e33b6d3869a45ede9d0b4d00a83e6eb5d3546718) prepare for next release

### Contributors

 * Mengxin Liu
 * 张祖建

## v1.14.7 (2025-09-18)

 * [7f2af0a97](https://github.com/kubeovn/kube-ovn/commit/7f2af0a97f0d0c8fa0ad8f2f1dac4284a98d726d) release v1.14.7
 * [47f0b30f9](https://github.com/kubeovn/kube-ovn/commit/47f0b30f9e2f10ac3a587daf424f1a825cfc5217) ipam: fix allocating from ippool (#5731)
 * [42f70e9d9](https://github.com/kubeovn/kube-ovn/commit/42f70e9d93c33116fc80d127d22f5b06a4d66cd8) controller: fix ip allocation from IPPools (#5729)
 * [31c620ca7](https://github.com/kubeovn/kube-ovn/commit/31c620ca74a661f9095841cd16a8c06fce5735f6) use arp for ipve network ready check (#5716)
 * [b5dc7251d](https://github.com/kubeovn/kube-ovn/commit/b5dc7251d32b24b8b83d3f0c72b41279a65eee91) fix(deps): update module golang.org/x/tools to v0.37.0 (#5714)
 * [96b08d582](https://github.com/kubeovn/kube-ovn/commit/96b08d582959ce79799fccebb50ee602bf16c5b8) gc ip by different types (#5702)
 * [8c5e18b52](https://github.com/kubeovn/kube-ovn/commit/8c5e18b529cbeac32b4971ff1786715bbee6e5f9) fix(deps): update module golang.org/x/net to v0.44.0 (#5705)
 * [0343e64b6](https://github.com/kubeovn/kube-ovn/commit/0343e64b6e04c44ac9bcef29b8b539d2be126ad0) prepare for next release

### Contributors

 * Mengxin Liu
 * renovate[bot]
 * 张祖建

## v1.14.6 (2025-09-09)

 * [bd38cc13f](https://github.com/kubeovn/kube-ovn/commit/bd38cc13f86ad2de670c073624e065e7060354b0) release v1.14.6
 * [5858c490c](https://github.com/kubeovn/kube-ovn/commit/5858c490c936157ffafe5571189aadc550ac91ee) veg: fix init failure when the a cidr applied is a sub network of the internal subnet (#5685)
 * [e3e6aea2d](https://github.com/kubeovn/kube-ovn/commit/e3e6aea2dd29890b4e5225b288b379477635c382) controller: fix marking ip resource as reserved incorrectly (#5698)
 * [bbe4f790d](https://github.com/kubeovn/kube-ovn/commit/bbe4f790d5ae64476c34200899f2f616b236d6bb) bump k8s to v1.32.8 (#5696)
 * [d455ee86d](https://github.com/kubeovn/kube-ovn/commit/d455ee86d967464a50b287bfde5a2da5da417e6e) fix(deps): update golang (#5695)
 * [41e2c37af](https://github.com/kubeovn/kube-ovn/commit/41e2c37afa66f3cba171e4d86900666cf315cc9e) fix(deps): update golang (#5692)
 * [a2dba9a5a](https://github.com/kubeovn/kube-ovn/commit/a2dba9a5af206fc9c73777d45f98720a35faf476) crd: fix unrecognized format int32/int64 (#5671)
 * [ce1bd7b2a](https://github.com/kubeovn/kube-ovn/commit/ce1bd7b2ab3089e3d5e37aa7b347248d35d65005) chore(deps): update dependency go to v1.25.1 (#5675)
 * [cdedd5ad2](https://github.com/kubeovn/kube-ovn/commit/cdedd5ad29304fba1f1a4056aad7c7a0b1fd25ef) ipam: fix ippool using/free ips after subnet update (#5668)
 * [1152eac13](https://github.com/kubeovn/kube-ovn/commit/1152eac13561a3c244816add10c346d54f3aae44) release address when acquire a new static address (#5658)
 * [4b79814c4](https://github.com/kubeovn/kube-ovn/commit/4b79814c48f1653bfcbe435f31edb568fb0f473c) fix vpc-dns annotations update revision (#5655)
 * [d9a99348f](https://github.com/kubeovn/kube-ovn/commit/d9a99348fc38619826a535657350c6b46055bf2a) handle delete final state unknown object in enqueue handler (#5649)
 * [40489e222](https://github.com/kubeovn/kube-ovn/commit/40489e2224c32501fca683b4a0f8b6d6b8d839f9) Initialize annotations immediately before assignment in setVpcDNSRoute (#5648)
 * [4eac3c5c5](https://github.com/kubeovn/kube-ovn/commit/4eac3c5c5c74d6baf6b771a7ed77a9b7a39fec5b) fix bug for issuse #5597: when slr update, the same vip will be delete too (#5646)
 * [c2dc2adaa](https://github.com/kubeovn/kube-ovn/commit/c2dc2adaa8ada66b344e6ed74be5e4d940c3158a)  fix bug for issuse #5597 (#5616)
 * [ca0ad1687](https://github.com/kubeovn/kube-ovn/commit/ca0ad16877e36779d587d4dd9f0421008837ecbe) fix ovn ipsec when restart cni (#5603)
 * [03ba1faf4](https://github.com/kubeovn/kube-ovn/commit/03ba1faf4611fd9e3f116bbd72f9c5a8fa8a1d54) fix static mac pod conflict with gateway mac (#5623)
 * [cfd7e53d7](https://github.com/kubeovn/kube-ovn/commit/cfd7e53d731148ff91e07ee8ef81a1e2b6fa125d) feat(vpcnatgw): send gratuitous arp for nexthops at natgw initialization (#5607)
 * [653e033e4](https://github.com/kubeovn/kube-ovn/commit/653e033e4a88289e02ae879dd864273f55c04a3a) modernize waitGroup
 * [9825bfa0f](https://github.com/kubeovn/kube-ovn/commit/9825bfa0fe76ca071946d517c71883ebe87863fd) chore(deps): update dependency go to v1.25.0
 * [6ec1c7c1f](https://github.com/kubeovn/kube-ovn/commit/6ec1c7c1f08ec2eaa57e01124c524bfae164cf8b) ci: ignore goveralls failure (#5606)
 * [00bb73ffe](https://github.com/kubeovn/kube-ovn/commit/00bb73ffe9c68d3468fa238626a1c4f7d34fa154) reduce probability of maps conncurrent iteration and write (#5585)
 * [4f1bdd7da](https://github.com/kubeovn/kube-ovn/commit/4f1bdd7daf3464165e299303d563d1473ebe7642) remove lsp when gw nodes change (#5591)
 * [2aee1e3b0](https://github.com/kubeovn/kube-ovn/commit/2aee1e3b09beb1be90e97727dd1cc2fb50bbdb22) prepare for next release

### Contributors

 * Mengxin Liu
 * SKALA NETWORKS
 * changluyi
 * pengpeng wu
 * renovate[bot]
 * xieyanker
 * zhangzujian
 * 张祖建

## v1.14.5 (2025-08-11)

 * [3ca45970f](https://github.com/kubeovn/kube-ovn/commit/3ca45970fa80976cb4587041a3146ed51be52619) release v1.14.5
 * [e855bcfef](https://github.com/kubeovn/kube-ovn/commit/e855bcfef6bf22d46f424b87e1b8f495122121e1) ci: rebuild gobgp for arm64 (#5583)
 * [da23a342c](https://github.com/kubeovn/kube-ovn/commit/da23a342cbf09a4a751f72cd97cf8994383a08e3) chore(deps): update dependency go to v1.24.6 (#5581)
 * [ff4ce47f1](https://github.com/kubeovn/kube-ovn/commit/ff4ce47f139ad7edc175befaf85e821e72ffb97c) fix(deps): update golang (#5574)
 * [8bc76be74](https://github.com/kubeovn/kube-ovn/commit/8bc76be744d7a1f368fe5ecca81dd62737311d03) vm static ip validate should use vm name (#5569)
 * [c61e4ec57](https://github.com/kubeovn/kube-ovn/commit/c61e4ec57b277203dbff281496c3c3cb43c1033c) chore(deps): update dependency go to v1.24.6 (#5564)
 * [42bbf139d](https://github.com/kubeovn/kube-ovn/commit/42bbf139d147d4ece924f44af5f572e6b480b23f) Fix the problem that if available ip is 0 but there is a value in excludeIPs, the fixed ip is used as the ip in excludeIPs but the error noAddressAvaliable is still reported. (#5565)
 * [b0f333c80](https://github.com/kubeovn/kube-ovn/commit/b0f333c80d9da02e3db23e1e84dbdbf751fc79ba) Do not garbage collect IPs of stopped VMs using non-default multus networks (#5557)
 * [63b8dcd36](https://github.com/kubeovn/kube-ovn/commit/63b8dcd36d1a5871289553893415cf7a0e16b89a) skip mv if config file already exists (#5558)
 * [574c59057](https://github.com/kubeovn/kube-ovn/commit/574c59057d5dc9d4d6ec188829474f6a106f466f) skip nad without config (#5560)
 * [ae1b53f72](https://github.com/kubeovn/kube-ovn/commit/ae1b53f729af54beb5db47e9d3609d17f113735e) tolerate duplicate ACLs (#5552)
 * [c26027037](https://github.com/kubeovn/kube-ovn/commit/c2602703735ce9d28b67f18ef48d7b9ac49d09b2) fix(helm): static master ips wouldn't work anymore (#5554)
 * [dbc6a9e91](https://github.com/kubeovn/kube-ovn/commit/dbc6a9e91f925c50c11e68b55499971565602a8a) disable external sync by default
 * [917e7510e](https://github.com/kubeovn/kube-ovn/commit/917e7510e529c184409befb7035e60480506c4a7) do not handle update external vpcs
 * [ca18d733c](https://github.com/kubeovn/kube-ovn/commit/ca18d733c7857865101b64a679ed0ad10961683c) fix nat-gateway arping (#5546)
 * [c1ff836ac](https://github.com/kubeovn/kube-ovn/commit/c1ff836ac7880c711f9327a23f75a58f33d52c18) fix vpcNatGw status cannot be updated (#5547)
 * [1094f8fd4](https://github.com/kubeovn/kube-ovn/commit/1094f8fd49e0ee1814e5a655e8b7424d5c7160ef) Handle missing files gracefully in metric functions by returning early on os.IsNotExist errors (#5529)
 * [5b68f08d7](https://github.com/kubeovn/kube-ovn/commit/5b68f08d726aee672f79bee35e81a88c13887cc2) CVE fix
 * [283a293fd](https://github.com/kubeovn/kube-ovn/commit/283a293fd631c9ed57209ee60ab2fa02cf61fcdc) check underlay nic exist before config external bridge (#5520)
 * [0dae9b010](https://github.com/kubeovn/kube-ovn/commit/0dae9b010011e9c014b1368c34696e3b6d579f9e) delay timer is incorrect (#5499)
 * [1ea9d09a4](https://github.com/kubeovn/kube-ovn/commit/1ea9d09a4197191956d5479805663efc67686995) prepare for next release

### Contributors

 * Mengxin Liu
 * SKALA NETWORKS
 * andrewlee1089
 * changluyi
 * renovate[bot]
 * xieyanker
 * zbb88888
 * 张祖建

## v1.14.4 (2025-07-23)

 * [839f68c34](https://github.com/kubeovn/kube-ovn/commit/839f68c34aaae682eb21d44484edd098641042f2) release v1.14.4
 * [282158756](https://github.com/kubeovn/kube-ovn/commit/282158756de99cf838365ce8e4a1c5aa6a0da8c7) fix NB Global not updated after OVN IC is disabled (#5511)
 * [a6950498a](https://github.com/kubeovn/kube-ovn/commit/a6950498a899ccdcfbd9fe160446af4c2c66e86a) fix concurrent map race (#5510)
 * [1e7988b26](https://github.com/kubeovn/kube-ovn/commit/1e7988b2680cd18582f4acef5fcd937591ffb4c0) fix cleanup stuck (#5505)
 * [c075fb20b](https://github.com/kubeovn/kube-ovn/commit/c075fb20b17b1216330fa08c141d946af0dc436d) panic install if labeled nodes are not found (#5504)
 * [31cf1670d](https://github.com/kubeovn/kube-ovn/commit/31cf1670df82527a4097550df7256f625d42702b) fix: ip/vpc are not deleted by helm uninstall (#5501)
 * [f1f059bff](https://github.com/kubeovn/kube-ovn/commit/f1f059bffc359a7b7a93c12b54d487ce49b0dd1f) chart: fix node selector (#5500)
 * [64b027d1d](https://github.com/kubeovn/kube-ovn/commit/64b027d1dec4e638829614159b2187ee1b6a438a) prepare for next release

### Contributors

 * Mengxin Liu
 * cmdy
 * 张祖建

## v1.14.3 (2025-07-18)

 * [328e984c5](https://github.com/kubeovn/kube-ovn/commit/328e984c591e4e79c240944ea030bea9009a1a85) release v1.14.3
 * [305d0c23b](https://github.com/kubeovn/kube-ovn/commit/305d0c23b1ce7daced682a2c23951e53cac4484d) fix version (#5495)
 * [a05ad7a8c](https://github.com/kubeovn/kube-ovn/commit/a05ad7a8ca5891014b9e591f51587f31fef921fe) metrics: fix pinger_inconsistent_port_binding (#5496)
 * [987abfb8a](https://github.com/kubeovn/kube-ovn/commit/987abfb8a32ab13929171b98bfdd9483b39bc43a) fix: only addOrUpdateSubnetQueue if the GatewayType is distributed instead of if it's not centralized (#5493)
 * [f9c72712f](https://github.com/kubeovn/kube-ovn/commit/f9c72712f923dc10a579866e0b0ab91de13dec26) bump k8s to v1.32.7 (#5492)
 * [6cde3a346](https://github.com/kubeovn/kube-ovn/commit/6cde3a34622aa81f3ccfb0665b53ad0b3e4e8880) fix missing rbac rule for sa ovn-ovs (#5488)
 * [bf6702777](https://github.com/kubeovn/kube-ovn/commit/bf67027775fa31e3dc78f39397a5e7eebbf5051d) chart version equal to 1.0 (#5417)
 * [b73f4616f](https://github.com/kubeovn/kube-ovn/commit/b73f4616f539221068a04ee98e7a0dba06911517) add release image cleanup
 * [9175edc27](https://github.com/kubeovn/kube-ovn/commit/9175edc276d711f12caa977cb7823c85a4d7a265) fix docs release
 * [18e08ef19](https://github.com/kubeovn/kube-ovn/commit/18e08ef1922c8d3586c2fa9216cc9ad1950e376a) prepare for next release

### Contributors

 * Joachim Hill-Grannec
 * Mengxin Liu
 * changluyi
 * 张祖建

## v1.14.2 (2025-07-14)

 * [f22e6ce93](https://github.com/kubeovn/kube-ovn/commit/f22e6ce93a636b1651344114a80c102a03f74e56) release v1.14.2
 * [be4e75777](https://github.com/kubeovn/kube-ovn/commit/be4e7577773d558732eb0fc85dd3804b82a9875a) controller: fix setting LR options on initialization (#5444)
 * [d00729aa9](https://github.com/kubeovn/kube-ovn/commit/d00729aa95c00362c3573ab1322445b41f26bc77) kubectl-ko: collect information about ipsec and xfrm (#5472)
 * [549c17cc0](https://github.com/kubeovn/kube-ovn/commit/549c17cc091c4d59826885ca558dbaf3f26e1544) fix(deps): update module golang.org/x/tools to v0.35.0 (#5478)
 * [131bebf7c](https://github.com/kubeovn/kube-ovn/commit/131bebf7c8464ef201437a747586f6086ce664c0) allow different provider use same vlan (#5471)
 * [2bafe4c0b](https://github.com/kubeovn/kube-ovn/commit/2bafe4c0b441b47f0369a3518680dd4c917b82d4) fix(deps): update module golang.org/x/net to v0.42.0 (#5469)
 * [4c0a115a9](https://github.com/kubeovn/kube-ovn/commit/4c0a115a9a7e913e288648ef9e8b8ed3ffa591d4) chore(deps): update module golang.org/x/term to v0.33.0 (#5462)
 * [a42245ab4](https://github.com/kubeovn/kube-ovn/commit/a42245ab462c936bfb6d1c095d58879bc2dd1468) chore(deps): update module golang.org/x/text to v0.27.0 (#5458)
 * [918e2a47b](https://github.com/kubeovn/kube-ovn/commit/918e2a47b6060b5d50f6dbe4d2d6467f1ad07a2c) fix(deps): update module golang.org/x/sys to v0.34.0 (#5460)
 * [d7446cc87](https://github.com/kubeovn/kube-ovn/commit/d7446cc8793b83a7d8829332fcc45c0d72f1bd6c) fix(deps): update module golang.org/x/mod to v0.26.0 (#5459)
 * [7db024adc](https://github.com/kubeovn/kube-ovn/commit/7db024adcdf1783e2f1084486dcae67be0ad96f5) chore(deps): update module golang.org/x/sync to v0.16.0 (#5456)
 * [b85e80bb0](https://github.com/kubeovn/kube-ovn/commit/b85e80bb017d2c4fc62db9871aa284b661c75b28) chore(deps): update dependency go to v1.24.5 (master) (#5438)
 * [661cfb526](https://github.com/kubeovn/kube-ovn/commit/661cfb526b26f392fdde172fa590697060e3a66a) replace Endpoint with EndpointSlice (#5437)
 * [87828c62d](https://github.com/kubeovn/kube-ovn/commit/87828c62d561e4495e7c70c0be09311f8c6a0a85) ci: increase timeout of Multus-CNI conformance e2e (#5429)
 * [b161305e1](https://github.com/kubeovn/kube-ovn/commit/b161305e137ca82f9cbbe0198062e96c9ddb66b2) chore(deps): update dependency go to v1.24.5 (#5439)
 * [e78ebeb75](https://github.com/kubeovn/kube-ovn/commit/e78ebeb75b268bba91d096458ca50ac613d2a356) prepare for next release

### Contributors

 * Mengxin Liu
 * renovate[bot]
 * zbb88888
 * 张祖建

## v1.14.1 (2025-07-04)

 * [f2b180e60](https://github.com/kubeovn/kube-ovn/commit/f2b180e608e0a8d241f56d67c4ca8e05ad959c45) release v1.14.1
 * [d24fc268e](https://github.com/kubeovn/kube-ovn/commit/d24fc268ee09bb57be3dbcda290f7db68119aa64) fix setting LR option always_learn_from_arp_request (#5426)
 * [5ea4d1456](https://github.com/kubeovn/kube-ovn/commit/5ea4d14566553150f3b22add8d77db4e30579799) kubectl-ko: replace endpoint with endpointslice (#5425)
 * [018a55927](https://github.com/kubeovn/kube-ovn/commit/018a55927ff09185d51db8c3c45c8f301d89e7ee) controller: set always_learn_from_arp_request to false only when LR is not connected to external network (#5419)
 * [fb859bbf6](https://github.com/kubeovn/kube-ovn/commit/fb859bbf653689d0115b942d6fea78b64cccb1cf) fix parsing resolv.conf when systemd-resolved is running on the host (#5423)
 * [ee0c70951](https://github.com/kubeovn/kube-ovn/commit/ee0c709513d188053f7dc693a16b9d7db9370c38) remove unused e2e tests
 * [737c301a8](https://github.com/kubeovn/kube-ovn/commit/737c301a845d82a1c152ce391728888f3f2e790a) cni-server: fix inserting and deleting iptables rules (#5421)
 * [30c8c5835](https://github.com/kubeovn/kube-ovn/commit/30c8c5835206148b7a8c55043403bf71ece5d56f) add manual release
 * [eae798768](https://github.com/kubeovn/kube-ovn/commit/eae798768adb72e1b9a0292eff2d7a4de8ccd72f) upgrade ovn chart name (#5416)
 * [f35452203](https://github.com/kubeovn/kube-ovn/commit/f35452203eb48869389f420d63dccc7450b0f7a4) mock: generate code using go tool command (#5409)
 * [528fb73ce](https://github.com/kubeovn/kube-ovn/commit/528fb73ced4e6ecab02b0d6297dd2a6e53adcede) fix matching loopback addresses (#5402)
 * [7cfc3bc89](https://github.com/kubeovn/kube-ovn/commit/7cfc3bc89b5ecdbc4c6d6426f8c370223b7d2f90) ci: deploy ipv6 talos cluster without ipv4 addresses (#5401)
 * [eb930603a](https://github.com/kubeovn/kube-ovn/commit/eb930603a7275b8f06f5110d4761a1c8cad3dfdf) fix(services): SLRs could not target VM endpoints (#5397)
 * [14d3fbb07](https://github.com/kubeovn/kube-ovn/commit/14d3fbb07db960996452b108b06baeb16b53638b) del and add acls in one transaction (#5394)
 * [d0fab29b9](https://github.com/kubeovn/kube-ovn/commit/d0fab29b92d6208406aabfa0f5b3bd4b84ee4366) prepare for next release

### Contributors

 * Mengxin Liu
 * SKALA NETWORKS
 * changluyi
 * zhangzujian
 * 张祖建

## v1.14.0 (2025-06-25)

 * [85a45ce30](https://github.com/kubeovn/kube-ovn/commit/85a45ce306585ce24e2ee23a752df850a81c034c) release vm ip when vm is stopped then deleted (#5390)
 * [904ac9afd](https://github.com/kubeovn/kube-ovn/commit/904ac9afde8e00ce23815b208072fd8fe7a2e719) wrong code (#5391)
 * [8194e60ff](https://github.com/kubeovn/kube-ovn/commit/8194e60ff6a4146c8d4a797329773f714372b2b3) vpc egress gateway: fix matching vpc (#5389)
 * [936150dd2](https://github.com/kubeovn/kube-ovn/commit/936150dd2f03c737037d9a25a1acf6788161128a) fix migrate down time (#5388)
 * [61f577ff3](https://github.com/kubeovn/kube-ovn/commit/61f577ff313094db4dc83d4501c2d6aa5492ff9d) fix duplicate acls because of parentkey (#5357)
 * [94c514e73](https://github.com/kubeovn/kube-ovn/commit/94c514e73bd688299124918b523c5a0d1549ad95) fix(slr): support switchlbrules targeting subnets with non-default providers (#5376)
 * [a2db6b178](https://github.com/kubeovn/kube-ovn/commit/a2db6b1784df4a1e126f178042b87091ac76724a) u2o keep src flow should consider multi-vlan on the same provider (#5385)
 * [ea758a79d](https://github.com/kubeovn/kube-ovn/commit/ea758a79dfffd9f8c707fa6778b61b1957f592e2) bump k8s to v1.32.6 (#5383)
 * [c386c7cdf](https://github.com/kubeovn/kube-ovn/commit/c386c7cdffbb65596525f5df1c4ccbac246f37f3) base: fix login shell of user sync (#5384)
 * [afdec4c35](https://github.com/kubeovn/kube-ovn/commit/afdec4c358172125a76a57a676d106fc635c86fb) fix(slr): deleting old entries from ipmapping to avoid clogging loadbalancer (#5380)
 * [5a66b6626](https://github.com/kubeovn/kube-ovn/commit/5a66b6626f2cf01203e5575f9767349ff5a923c7) feature: perform duplicate address detection if ipv6 address has a dadfailed flag (#5156)
 * [22ba1c035](https://github.com/kubeovn/kube-ovn/commit/22ba1c035359c3e710d6e9d70fe4c928ede65ff2) base: set login shell to /usr/sbin/nologin (#5382)
 * [f58127989](https://github.com/kubeovn/kube-ovn/commit/f58127989303570e385bf91a530806ad3ee8dd2a) chore(deps): update dependency cert-manager to v1.18.1 (#5373)
 * [7fb6efd88](https://github.com/kubeovn/kube-ovn/commit/7fb6efd88624eb8073b31c473a9ce49d1997c87c) fix(deps): update module github.com/containerd/containerd/v2 to v2.1.3 (#5381)
 * [79a799646](https://github.com/kubeovn/kube-ovn/commit/79a799646785ff265be2b64247e545d8d4a65e16) chore(deps): update dependency cilium to v1.17.5 (#5379)
 * [28ea8670b](https://github.com/kubeovn/kube-ovn/commit/28ea8670b810c95876b3e7e5154d64f43e84485f) ci: increase multus-cni resource limits (#5378)
 * [46c8045eb](https://github.com/kubeovn/kube-ovn/commit/46c8045eb782e0f17bd5cae8f1b3899f7a8637ea) fix(slr): switchlbrule doesn't support multi-homed/ipv6-first pods (#5375)
 * [016f52a01](https://github.com/kubeovn/kube-ovn/commit/016f52a01094b5d000429a8783ed3806dc77f253) vpc egress gateway: drop traffic if no nexthop is available (#5370)
 * [5e4e20aaf](https://github.com/kubeovn/kube-ovn/commit/5e4e20aaf8d2a75c31f31d6b670a1781b85c49a5) base: remove unused packages (#5368)
 * [501b4e8e1](https://github.com/kubeovn/kube-ovn/commit/501b4e8e1828ac58dacc5713fe638ea7c892e211) fix ip cr not delete with pod in the case of subnet not exist (#5364)
 * [2aeea8629](https://github.com/kubeovn/kube-ovn/commit/2aeea8629eafa13cfd65734c48f8bd98452b25c1) u2o keep src mac (#5192)
 * [9d398faaa](https://github.com/kubeovn/kube-ovn/commit/9d398faaac50a9ddab1c75e2d570b725d13caad0) fix: add validation to prevent subnet and VPC with the same name (#5371)
 * [574b33a2f](https://github.com/kubeovn/kube-ovn/commit/574b33a2ff31f3c883eeefc76dda034824423a06) chore(deps): update dependency helm to v3.18.3 (#5366)
 * [2865d3db8](https://github.com/kubeovn/kube-ovn/commit/2865d3db83affbbef0f3484a34d26e145fdda400) fix modernize lint (#5363)
 * [06540ebad](https://github.com/kubeovn/kube-ovn/commit/06540ebadd01bd257e890bb0b993147da9d5475b) chore(deps): update dependency gosec to v2.22.5 (#5362)
 * [29c25ab0c](https://github.com/kubeovn/kube-ovn/commit/29c25ab0cbd1af24f750b7d11ba2c6cab8a9117a) docs: updated CHANGELOG.md (#5361)
 * [33ff4cd73](https://github.com/kubeovn/kube-ovn/commit/33ff4cd73d9313ece91b1990ded038373510fb2a) fix(deps): update module github.com/containerd/containerd/v2 to v2.1.2 (#5354)
 * [1fceea23e](https://github.com/kubeovn/kube-ovn/commit/1fceea23ede605698a589f679b91cd28c348b25b) fix(slr): address family and familypolicy isn't correct (#5349)
 * [49bbed8d3](https://github.com/kubeovn/kube-ovn/commit/49bbed8d320e937bb23ceeab1a5315f74a86ee2d) controller: migrate acl tier after upgrade (#5351)
 * [82ef65ff9](https://github.com/kubeovn/kube-ovn/commit/82ef65ff99c9a993e5cf39011fc122017e0d9d8b) fix vpc egress gateway not applied to new pods (#5348)
 * [c40c6e38b](https://github.com/kubeovn/kube-ovn/commit/c40c6e38b78001da68dbb9c394120c565940be55) use group to reduce noisies
 * [cab9fa964](https://github.com/kubeovn/kube-ovn/commit/cab9fa964c7c7b0b693e0b5037e19d6376d7171e) add renovate label and ignore windows upgrade
 * [ddaf03eee](https://github.com/kubeovn/kube-ovn/commit/ddaf03eee7744a565cf5a4ee603ff3e22c91cef2) add return for enqueueUpdateIP error (#5350)
 * [1b4f2f558](https://github.com/kubeovn/kube-ovn/commit/1b4f2f5587935233d2dfc20133d65de7ef2e87bc) fix(deps): update module kubevirt.io/client-go to v1.5.2 (#5340)
 * [557f68652](https://github.com/kubeovn/kube-ovn/commit/557f686528c53346d5ec7bffb49c44b0665a7e8e) chore(deps): update dependency kubevirt to v1.5.2 (#5338)
 * [f1c5e4631](https://github.com/kubeovn/kube-ovn/commit/f1c5e4631d5be89a983b96741d77747928e65f10) docs: updated CHANGELOG.md (#5347)
 * [241398a1e](https://github.com/kubeovn/kube-ovn/commit/241398a1e0939defed0d59019da4822c8c7464f8) fix sts/vm lsp in incorrect port groups after rescheduled to another node (#5344)
 * [d4ddd23a9](https://github.com/kubeovn/kube-ovn/commit/d4ddd23a9d37a7cc00ed0b00355307247ad7a016) fix(deps): update module kubevirt.io/api to v1.5.2 (#5339)
 * [e30a970b2](https://github.com/kubeovn/kube-ovn/commit/e30a970b2869d8372bfe94e6c749589264c52b41) chore(deps): update dependency cert-manager to v1.18.0 (#5341)
 * [9ab91ada8](https://github.com/kubeovn/kube-ovn/commit/9ab91ada88dc8a4407cfdb5bbc0658d183445acd) fix(deps): update kubernetes packages to v0.33.1 (#5329)
 * [a5940a234](https://github.com/kubeovn/kube-ovn/commit/a5940a23477fbac8b3ea4ce991cdf2692fc36f66) base: build openssl fips module from deb source (#5331)
 * [4d6274bcd](https://github.com/kubeovn/kube-ovn/commit/4d6274bcdee5c2fe86d1d31b16d71d3de84dcee4) fix: vm has multi nic in the same subnet, but release all (#5336)
 * [cfcce171e](https://github.com/kubeovn/kube-ovn/commit/cfcce171e2a8b6e9b755877ab639d1184aada419) docs: updated CHANGELOG.md (#5335)
 * [3dc7a64ce](https://github.com/kubeovn/kube-ovn/commit/3dc7a64ce136251770b546e8b4d7399743abe5ad) merge dad check to 1.12 (#5334)
 * [bbd62e894](https://github.com/kubeovn/kube-ovn/commit/bbd62e89488ab180068a47e4380c523883f9d4de) fix fips not really replace (#5278)
 * [581425bc9](https://github.com/kubeovn/kube-ovn/commit/581425bc9186339b1e3644f186283eff0344aac5) netpol: fix missing ACL name (#5281)
 * [7624feb07](https://github.com/kubeovn/kube-ovn/commit/7624feb07ec7440dc0541445c44cea153480a493) fix: kubectl-ko: properly get the number of nodes (#5327)
 * [97ff89de2](https://github.com/kubeovn/kube-ovn/commit/97ff89de284a33f9ff941ace243ffb8c25b12787) fix(deps): update module gopkg.in/k8snetworkplumbingwg/multus-cni.v4 to v4.2.1 (#5330)
 * [dcdf899ea](https://github.com/kubeovn/kube-ovn/commit/dcdf899ea6e75672deb172a377ab4f572fd14a7b) chore(deps): update dependency multus to v4.2.1 (#5328)
 * [7ecf5339e](https://github.com/kubeovn/kube-ovn/commit/7ecf5339e5d82453a14ca0b34fef522e90f24a60) chore(deps): update dependency ubuntu to v24 (#5315)
 * [21286537a](https://github.com/kubeovn/kube-ovn/commit/21286537aa287edfcd885d24a7d908c9890fd7ae) chore(deps): update dependency metallb to v0.15.2 (#5313)
 * [a7e439f2c](https://github.com/kubeovn/kube-ovn/commit/a7e439f2ce39a5a2d1d06f28b08e061e58cb2d77) docs: updated CHANGELOG.md (#5325)
 * [eb395857f](https://github.com/kubeovn/kube-ovn/commit/eb395857fc9e04de3b6456abbfa522ef0f6b4ce5) bump go to 1.24.4 (#5321)
 * [c09b19d0c](https://github.com/kubeovn/kube-ovn/commit/c09b19d0cb033d734377e0c21d64fc26bca09a5d) fix(deps): update module go.universe.tf/metallb to v0.15.2 (#5314)
 * [3fb0dcc56](https://github.com/kubeovn/kube-ovn/commit/3fb0dcc569156c73f83a9d168fc6d1f479c07c1d) [BUG] Request Latency 99th Quantile series labels are not unique (#5320)
 * [96f05583b](https://github.com/kubeovn/kube-ovn/commit/96f05583b066e90b6a8cfce1ebf51731ecd57571) chore: add gomodTidy to postUpdateOptions
 * [59619e990](https://github.com/kubeovn/kube-ovn/commit/59619e99099044a73ca8cb5e6d5b8ef643435a19) chore: release branch automate security updates (#5311)
 * [7ad7db246](https://github.com/kubeovn/kube-ovn/commit/7ad7db2468b3ae473209e463ce6c099156870b0d) chore: remove Dependabot configuration file
 * [7e6728ce3](https://github.com/kubeovn/kube-ovn/commit/7e6728ce35dbcd7733be806ece498faa7f8bffe4) tls: rotate self-signed key/certificate (#5303)
 * [fe5161e1c](https://github.com/kubeovn/kube-ovn/commit/fe5161e1c402e20ee1ee34af429b3a918b2c103e) chore(deps): update dependency metallb to v0.15.0 (#5308)
 * [cc4c701d1](https://github.com/kubeovn/kube-ovn/commit/cc4c701d105d934966e54f358f3abe14abba554b) chore(deps): update aquasecurity/trivy-action action to v0.31.0 (#5307)
 * [3a96257a9](https://github.com/kubeovn/kube-ovn/commit/3a96257a936223e8728f80df9a810d38211e94a5) ovn fip spec distributed and support v4:v6 v6:v4 (#5283)
 * [82b73fece](https://github.com/kubeovn/kube-ovn/commit/82b73fece2476e474c18b86365ae97c55968e71f) fix podcidr route still use join ip as src ip (#5287)
 * [04d429faf](https://github.com/kubeovn/kube-ovn/commit/04d429faff27854130058b2c3b443e65b9e455d0) chore(deps): update dependency helm to v3.18.2 (#5305)
 * [bd7a40e7d](https://github.com/kubeovn/kube-ovn/commit/bd7a40e7d18ac81d691efabc9f8ff84a51859903) feat(controller): add database health check (#5294)
 * [f14f44113](https://github.com/kubeovn/kube-ovn/commit/f14f441135c87ed1d320b11ffd95124f8ce7a6f4) fix(deps): update module github.com/puzpuzpuz/xsync/v3 to v4 (#5302)
 * [05e9fbf19](https://github.com/kubeovn/kube-ovn/commit/05e9fbf19e24a314659894cc5ab54a58413c3e44) fix(deps): update module github.com/containerd/containerd to v2 (#5301)
 * [1244a4ff4](https://github.com/kubeovn/kube-ovn/commit/1244a4ff46a8d39d4f1de6194d9bb7f4bdf63dbb) fix(deps): update module github.com/cenkalti/backoff/v4 to v5 (#5300)
 * [ddf54e4e7](https://github.com/kubeovn/kube-ovn/commit/ddf54e4e740af971fc9396863f74917d47790de3) tls: add options to set tls min/max version and cipher suites (#5289)
 * [b6934cf08](https://github.com/kubeovn/kube-ovn/commit/b6934cf088485c63dfe7ef93852611e332a50c16) build(deps): bump github.com/docker/docker (#5295)
 * [baaf5d5f0](https://github.com/kubeovn/kube-ovn/commit/baaf5d5f064bb64c1dde3b9f6bd7471568ac1885) chore(deps): update dependency kind to v0.29.0 (#5297)
 * [e4d9a05d6](https://github.com/kubeovn/kube-ovn/commit/e4d9a05d68990e54a6eca5815fd4d7dab263eb6d) chore(deps): update dependency kwork to v0.7.0 (#5293)
 * [ba1ba78d7](https://github.com/kubeovn/kube-ovn/commit/ba1ba78d70dccf4d433c219bad7281810b6a8936) chore(deps): update dependency helm to v3.18.1 (#5292)
 * [4266f1875](https://github.com/kubeovn/kube-ovn/commit/4266f187539ab7efea63105df213cd5cfdedeefb) chore: Configure Renovate (#5253)
 * [2668ef876](https://github.com/kubeovn/kube-ovn/commit/2668ef876d9ac891b7b2b7f6748c879a657ce535) build(deps): bump github.com/docker/docker (#5290)
 * [9cbafeec8](https://github.com/kubeovn/kube-ovn/commit/9cbafeec86548dfab85fa438ac9ef466f5d10777) fix(bgpspeaker): nat gateway with embedded bgp speaker would crash (#5285)
 * [8583f3ccb](https://github.com/kubeovn/kube-ovn/commit/8583f3ccb189e1643e17ddade112838256e626e4) tls: specify cipher suites (#5288)
 * [6b0cef8d0](https://github.com/kubeovn/kube-ovn/commit/6b0cef8d041650ff5cf904c4514a68d4127353df) build(deps): bump github.com/go-logr/logr from 1.4.2 to 1.4.3 (#5286)
 * [c43c7a77e](https://github.com/kubeovn/kube-ovn/commit/c43c7a77e5c0c5249f04c835bff4511b4954c9c7) fix vlan check conflict and recover it  (#5282)
 * [5db981389](https://github.com/kubeovn/kube-ovn/commit/5db981389b04dbb80ef30ad708cdd92dacbc5b2f) base: add CAP_NET_ADMIN to traceroute (#5275)
 * [c8a6597c5](https://github.com/kubeovn/kube-ovn/commit/c8a6597c5544c3c2ee403bdfdefde28b594759f8) fix port name (#5274)
 * [e75c500ed](https://github.com/kubeovn/kube-ovn/commit/e75c500ed52cc7b61f82ab80d94bf511bb145b6e) any vpc can use any external subnet (#5268)
 * [543fd4583](https://github.com/kubeovn/kube-ovn/commit/543fd458302986d15d9ee589106d214d19767ab5) check vlan conflict (#5233)
 * [58bc11094](https://github.com/kubeovn/kube-ovn/commit/58bc1109447818193add3fcd35d41769f54ec885) docs: updated CHANGELOG.md (#5276)
 * [af90ec12c](https://github.com/kubeovn/kube-ovn/commit/af90ec12c44bbd9137f873bacdb1d14a77e4eb86) build(deps): bump google.golang.org/grpc from 1.72.1 to 1.72.2 (#5273)
 * [03ec76987](https://github.com/kubeovn/kube-ovn/commit/03ec76987351fb9be452a2ee05b2318a86c71c4c) some fixes for vpc egress gateway (#5271)
 * [f88c2df8f](https://github.com/kubeovn/kube-ovn/commit/f88c2df8f9b60a43bc89f6f1e04de3c76dc74707) base: update ovn patches (#5264)
 * [186ea9be4](https://github.com/kubeovn/kube-ovn/commit/186ea9be4ab9dbd27d7c8c4aa6a013df51e1379d) feature: traffic policy (#5263)
 * [4f060e2a5](https://github.com/kubeovn/kube-ovn/commit/4f060e2a5430c8b383a6e93bab3e05041d96b3cb) vpc egreess gateway: select workloads by namespace selector and pod selector (#5260)
 * [224d30cc6](https://github.com/kubeovn/kube-ovn/commit/224d30cc6ba5b787d2592762f89c6c716e854b39) chore(vpc-nat-gw): refactor before adding HA gw (#5197)
 * [5645113be](https://github.com/kubeovn/kube-ovn/commit/5645113beab86de6ecb0bc291f3232bcaba636b4) Feature/x requirements log permission (#5238) (#5258)
 * [eaee89f48](https://github.com/kubeovn/kube-ovn/commit/eaee89f48f918e14373a935b1981d1c67534ab61) fix Node access underlay pod failed when applying  network policy (#5256)
 * [32310fb7f](https://github.com/kubeovn/kube-ovn/commit/32310fb7fcaeb1f5a02ba2697e4bfd05087df49c) bump k8s to v1.32.5 (#5251)
 * [3f0edd93c](https://github.com/kubeovn/kube-ovn/commit/3f0edd93c3c110bb3a74c4906e50079ea6cea06c) cleanup: delete additional ConfigMaps and RoleBindings in cleanup script (#5255)
 * [33be6d513](https://github.com/kubeovn/kube-ovn/commit/33be6d51367a362518b7d819634beca7582904d3) fix: put iptables nat prerouting rule right before kube-proxy inserted one (#5232)
 * [f04a58b22](https://github.com/kubeovn/kube-ovn/commit/f04a58b22779cb1571772cd908ee3b50a59b1e14) fix handle pod elapsed time (#5250)
 * [073a5d7dd](https://github.com/kubeovn/kube-ovn/commit/073a5d7dde1cd5a6c56b1a0c054d735b089a095b) build(deps): bump google.golang.org/grpc from 1.72.0 to 1.72.1 (#5249)
 * [3bf5b1dd8](https://github.com/kubeovn/kube-ovn/commit/3bf5b1dd8c6dc4336b998fbb8953a3b1e8f0ca7d) Fix fisp arm (#5244)
 * [c4b013d87](https://github.com/kubeovn/kube-ovn/commit/c4b013d8786c26b50c630e59e708a8d9dd625e27) Update upgrade-ovs.sh to use POD_NAMESPACE variable for fetching update strategy (#5243)
 * [11b05dc22](https://github.com/kubeovn/kube-ovn/commit/11b05dc2220f95078ffd2fc1f51b2f621f1dfd80) fix slbr missing vip when svc is deleted
 * [8413ae0e9](https://github.com/kubeovn/kube-ovn/commit/8413ae0e91022ad6abcd6f1e723be649dcf19470) Refactor controller to use EndpointSlices instead of Endpoints
 * [3fede2ae0](https://github.com/kubeovn/kube-ovn/commit/3fede2ae074dc32179d91b541ede0d3b29837809) Fix fisp arm (#5241)
 * [aa2a32975](https://github.com/kubeovn/kube-ovn/commit/aa2a32975e04b9217ced84ba1aa629a8d9efb81f) Revert "fix arm (#5239)" (#5240)
 * [8bf693d8e](https://github.com/kubeovn/kube-ovn/commit/8bf693d8e2177ec81102f2284efea1888c7c301d) fix arm (#5239)
 * [a8a91f2d5](https://github.com/kubeovn/kube-ovn/commit/a8a91f2d5ca2aa7c0f4dc42d70c9c998a8f98234) complile openssl 3.5.0 to support fips (#5217)
 * [f2ccacd65](https://github.com/kubeovn/kube-ovn/commit/f2ccacd6571297bd132b24363e14798db6bdcb9a) docs: updated CHANGELOG.md (#5236)
 * [86bd74096](https://github.com/kubeovn/kube-ovn/commit/86bd74096764afbcb085652948abf34e1e491734) vpc egress gateway: set minimum replicas to zero (#5235)
 * [c7365c7d9](https://github.com/kubeovn/kube-ovn/commit/c7365c7d9982052161ca0c586b944c66eb2cb595) build(deps): bump github.com/vishvananda/netlink (#5234)
 * [3ae4be4c2](https://github.com/kubeovn/kube-ovn/commit/3ae4be4c27a1511f7eb68cf2546fc4765d1330e0) log show handle pod elapsed time (#5218)
 * [04c6845d0](https://github.com/kubeovn/kube-ovn/commit/04c6845d07d4fd46d8b13cfb60da7a6f434be696) Deleting a FIP sets the referenced EIP to be ready (#5141)
 * [7fcde6cab](https://github.com/kubeovn/kube-ovn/commit/7fcde6cab069bfd9dd09535e263f2a04e1f52bbe) simple vip (#5229)
 * [143781ea9](https://github.com/kubeovn/kube-ovn/commit/143781ea9a151109cb86aa740c5df75ff2be2bdd) We've seen instances of a VPC being left with a now-deleted subnet on (#5228)
 * [a7a3ef04f](https://github.com/kubeovn/kube-ovn/commit/a7a3ef04f94ebf8791bcefa5b01edca89aed005f) build(deps): bump github.com/osrg/gobgp/v3 from 3.36.0 to 3.37.0 (#5231)
 * [f29c6c8d5](https://github.com/kubeovn/kube-ovn/commit/f29c6c8d5ecaa95235b886482b1e15268636872e) ci: fix cilium chaining with underlay networking (#5226)
 * [fb758c7a3](https://github.com/kubeovn/kube-ovn/commit/fb758c7a3d455c1cff38f32c25dfb8ebb0760eba) build(deps): bump github.com/Microsoft/hcsshim from 0.12.9 to 0.13.0 (#5225)
 * [a86a25682](https://github.com/kubeovn/kube-ovn/commit/a86a256827d8a9c3b0e0f665267445de5c8e93ae) bump go to 1.24.3 (#5214)
 * [01e127ccb](https://github.com/kubeovn/kube-ovn/commit/01e127ccbe0d597ff4fcdc15f90eda64d32f9cbf) build(deps): bump golang.org/x/tools from 0.32.0 to 0.33.0 (#5212)
 * [986cfb645](https://github.com/kubeovn/kube-ovn/commit/986cfb645e91f6b8b182a0fbe408b853a91ba9f2) base: use local patch files (#5201)
 * [509ec599e](https://github.com/kubeovn/kube-ovn/commit/509ec599e9fe3ad7182cbd41bf59ae5d7b2b009c) vpc egress gateway: fix invalid route destination (#5202)
 * [8d75e5c3d](https://github.com/kubeovn/kube-ovn/commit/8d75e5c3d3fad91c2a8f634389f8af7e4aa1850e) build(deps): bump golang.org/x/sys from 0.32.0 to 0.33.0 (#5204)
 * [636aebc1a](https://github.com/kubeovn/kube-ovn/commit/636aebc1acb044b35d016278ca4d7da12612b317) fix (#5200)
 * [e624949db](https://github.com/kubeovn/kube-ovn/commit/e624949db863e90a0d276eea00a1174834e46407) fix nil pointer (#5199)
 * [8aaebc312](https://github.com/kubeovn/kube-ovn/commit/8aaebc31228f4bf8ce98a1433cdead3c1dac8200) build(deps): bump go.uber.org/mock from 0.5.1 to 0.5.2 (#5198)
 * [41cd03166](https://github.com/kubeovn/kube-ovn/commit/41cd031667768fe773a2cd980fc4adda4b592280) fix ip clean (#5193)
 * [8f16fc789](https://github.com/kubeovn/kube-ovn/commit/8f16fc789df5be6488c557a2163edf1a13c5af7a) fix installation script and chart for dpdk (#5194)
 * [056d7944f](https://github.com/kubeovn/kube-ovn/commit/056d7944ff78b63b8106626ac2d2db4f65c60059) some fixes for chart v2 (#5196)
 * [ce2065964](https://github.com/kubeovn/kube-ovn/commit/ce2065964ac2d7a5c2dad96205c9b35905cc3afb) feat(helm): new chart design (#4437)
 * [5ca36a36d](https://github.com/kubeovn/kube-ovn/commit/5ca36a36d49fe44fbd0e7de4826e2e901426c806) feat(helm): new chart design (#4437)
 * [05ac943f4](https://github.com/kubeovn/kube-ovn/commit/05ac943f4d06fd32d46559274c17d8a572bb5fd3) base: bump cni plugins to v1.7.1 (#5191)
 * [2f288ee10](https://github.com/kubeovn/kube-ovn/commit/2f288ee108190f76610fa22acf0183678e8f9e5b) build(deps): bump github.com/containernetworking/plugins (#5190)
 * [f87a51fb7](https://github.com/kubeovn/kube-ovn/commit/f87a51fb7139e3262d8f7636b1192d9142ae9c3b) Makefile: set major/minor version number for the master branch (#5186)
 * [43c6ee802](https://github.com/kubeovn/kube-ovn/commit/43c6ee80237b2aa86edbad8d4596ed5f15dde385) fix metallb e2e (#5183)
 * [d105d35f3](https://github.com/kubeovn/kube-ovn/commit/d105d35f3c17fbe392d3df65ea81d87649d36490) docs: updated CHANGELOG.md (#5181)
 * [b6158dae3](https://github.com/kubeovn/kube-ovn/commit/b6158dae3362e54f07d507e128b83800df2154e3) bump k8s to v1.32.4 (#5178)
 * [42233288a](https://github.com/kubeovn/kube-ovn/commit/42233288a168a8c2fe4999a68f231c7ecaf75a46) Makefile: fix installing dev version on talos (#5179)
 * [821234a02](https://github.com/kubeovn/kube-ovn/commit/821234a02237a1147fa5886c24882f9b420d3071) ci: ignore cni-server restarts caused by join network check failure (#5177)
 * [ab3d11ae7](https://github.com/kubeovn/kube-ovn/commit/ab3d11ae77c08b6fc354c7a8264bccf892d0d808) base: update ovn patch (#5175)
 * [6011b90ba](https://github.com/kubeovn/kube-ovn/commit/6011b90ba467775d7beb78c0517048eb654c9944) fix: clean up garbage lsp (#5172)
 * [05b7d839b](https://github.com/kubeovn/kube-ovn/commit/05b7d839b14cae3af41fca927282b2cb275d2514) build(deps): bump google.golang.org/grpc from 1.71.1 to 1.72.0 (#5171)
 * [8b26247fc](https://github.com/kubeovn/kube-ovn/commit/8b26247fca3c2c2d6d7970f03723773df0910081) docs: updated CHANGELOG.md (#5168)
 * [48db4e499](https://github.com/kubeovn/kube-ovn/commit/48db4e4997a19119372f91930687a7125f960862) controller: ensure ovn route policy is reconciled after node is initialized (#5166)
 * [3177e93bd](https://github.com/kubeovn/kube-ovn/commit/3177e93bd631e18811577a493a7a546bc09aa7f9) modernize: simplify code by using modern constructs (#5163)
 * [9987f5517](https://github.com/kubeovn/kube-ovn/commit/9987f551732db35e030a2cecd305fd09959fddfb) build(deps): bump github.com/docker/docker (#5165)
 * [ad810d6a8](https://github.com/kubeovn/kube-ovn/commit/ad810d6a8a2d93512456ad14876c9845283eb9d7) docs: updated CHANGELOG.md (#5164)
 * [449cc932d](https://github.com/kubeovn/kube-ovn/commit/449cc932d9064f28e4d94c91fc8ccdee51fad5bb) fix vpc nat gateway to correctly use subnet mapped attachment network (#5158)
 * [262b66867](https://github.com/kubeovn/kube-ovn/commit/262b668675104fcfce7cbbfb1b6a3a326f36a610) build(deps): bump github.com/docker/docker (#5160)
 * [91f264e7e](https://github.com/kubeovn/kube-ovn/commit/91f264e7e2f2cb1e7c6c635e7ed78ea2f487f997) support k8s host vm vip type (#5148)
 * [7e584096f](https://github.com/kubeovn/kube-ovn/commit/7e584096f411c6282d3599d762d8a9d220136bbc) pinger: add liveness/readiness probes (#5155)
 * [eb17163bb](https://github.com/kubeovn/kube-ovn/commit/eb17163bb41ae85fc9af215ea3e3808b6a8d5786) add dad e2e (#5146)
 * [80382fd1e](https://github.com/kubeovn/kube-ovn/commit/80382fd1ec161040833ec4514627854453863305) chart: fix ovs ipsec keys host path (#5137)
 * [3fa839c74](https://github.com/kubeovn/kube-ovn/commit/3fa839c74d80862120296fd9ede1586cc878f22f) build(deps): bump kernel.org/pub/linux/libs/security/libcap/cap (#5152)
 * [432426cdd](https://github.com/kubeovn/kube-ovn/commit/432426cdd7c1ba3aa8c2b75dbcabdf7c342d6248) ci: add tests for underlay installation on Talos (#5147)
 * [9204d85fd](https://github.com/kubeovn/kube-ovn/commit/9204d85fdfa3123f27c9cbd4e1d562b17e60f3a1) ci: bump golangci-lint to v2.1.1 (#5151)
 * [745615025](https://github.com/kubeovn/kube-ovn/commit/745615025befeec918d0f47d8009f633ff43ece1) ci: fix kvm/libvirt installation (#5150)
 * [8407e834f](https://github.com/kubeovn/kube-ovn/commit/8407e834fc4cd25a21b0f2151e83320194129645) Add Finalizer to FIP before programming FIP into the VPC NT Gateway (#5142)
 * [6b601709b](https://github.com/kubeovn/kube-ovn/commit/6b601709b50f9444b0fda01a35a354414d5d9625) Support /32 tunnel-source (#5144)
 * [7c8c730d8](https://github.com/kubeovn/kube-ovn/commit/7c8c730d8e24f53c965a80da30379cdde746a07e) ci: add installation test for Talos Linux (#5109)
 * [490ecc037](https://github.com/kubeovn/kube-ovn/commit/490ecc037f43805d32b278650230ec7957d38271) fix dbus/NetworkManager connection in Talos (#5140)
 * [db40fc449](https://github.com/kubeovn/kube-ovn/commit/db40fc44954f3628e5ac0ba6d261c323bcc5aa11) chart: fix local bin directory host path (#5136)
 * [b39caaccb](https://github.com/kubeovn/kube-ovn/commit/b39caaccbe4891f53330b584b1abf0471644f3de) base: update ovn patches (#5139)
 * [fbe9957c3](https://github.com/kubeovn/kube-ovn/commit/fbe9957c3f61ef537e79371a36f69585eca7282b) build(deps): bump github.com/prometheus/client_golang (#5133)
 * [1cc23652f](https://github.com/kubeovn/kube-ovn/commit/1cc23652f5e079b6ab7cf6234c31fcb2b3977b52) build(deps): bump go.uber.org/mock from 0.5.0 to 0.5.1 (#5134)
 * [17bd506d7](https://github.com/kubeovn/kube-ovn/commit/17bd506d7aa1bb829afe71999c7c701a3b8987b6) build(deps): bump github.com/prometheus-community/pro-bing (#5135)
 * [c78348c02](https://github.com/kubeovn/kube-ovn/commit/c78348c029e2c606f4f15159dae56ca225ae853a) build(deps): bump golang.org/x/tools from v0.31.0 to v0.32.0 (#5130)
 * [93483a59c](https://github.com/kubeovn/kube-ovn/commit/93483a59c4df468d3540f5171df35d9e6c7419f8) build(deps): bump github.com/onsi/ginkgo/v2 from 2.23.3 to 2.23.4 (#5128)
 * [f78099773](https://github.com/kubeovn/kube-ovn/commit/f78099773ef829077986fa3e14ab850775a22f3c) build(deps): bump github.com/containernetworking/cni from 1.2.3 to 1.3.0 (#5127)
 * [68c800172](https://github.com/kubeovn/kube-ovn/commit/68c800172ff5628187d09949863d199b8242823c) docs: updated CHANGELOG.md (#5123)
 * [645ac944e](https://github.com/kubeovn/kube-ovn/commit/645ac944e9ca97565a1ac61cbb5a7741d17df8b7) Introduce Swisscom to USERS.md (#5120)
 * [a38b004b3](https://github.com/kubeovn/kube-ovn/commit/a38b004b31ce0a1a3f9a6e224771f4bd5e614b15) gc: consider whether the sts pod is alive during lsp gc (#5122)
 * [ded57da84](https://github.com/kubeovn/kube-ovn/commit/ded57da84a608a476198356c1d3819406b6f5df5) build(deps): bump github.com/osrg/gobgp/v3 from 3.35.0 to 3.36.0 (#5121)
 * [26fa3c019](https://github.com/kubeovn/kube-ovn/commit/26fa3c019b7660ec3de1e6f80b21a0b78685cb0f) build(deps): bump github.com/onsi/gomega from 1.36.3 to 1.37.0 (#5119)
 * [3fe43f2c0](https://github.com/kubeovn/kube-ovn/commit/3fe43f2c0d034fcff19874af0c875179fe09495f) bump go to 1.24.2 (#5117)
 * [ca13d2894](https://github.com/kubeovn/kube-ovn/commit/ca13d2894352091906f11e59aaf7d155c52dadc0) build(deps): bump google.golang.org/grpc from 1.71.0 to 1.71.1 (#5116)
 * [d55a933a2](https://github.com/kubeovn/kube-ovn/commit/d55a933a2bb1e56046b23cbde55ed308b6d86578) ovn lb select the local chassis's backend prefer (#4894)
 * [c3e2307e4](https://github.com/kubeovn/kube-ovn/commit/c3e2307e40f84297c1aed02ed74737bf5b008a68) docs: updated CHANGELOG.md (#5113)
 * [0d227994e](https://github.com/kubeovn/kube-ovn/commit/0d227994e88dd07cec55816206f497fad31c076c) base: update ovs patches (#5111)
 * [c7a6a71ef](https://github.com/kubeovn/kube-ovn/commit/c7a6a71ef7148e1a6836da4f423a793f0396c32a) e2e: fix kubectl-ko trace test (#5108)
 * [6f88e1915](https://github.com/kubeovn/kube-ovn/commit/6f88e1915b1e5ae1baefce567850cf3ecfb1f735) feat(controller): skip appending VM LSPs if default Multus network is present (#5106)
 * [9fc5197fe](https://github.com/kubeovn/kube-ovn/commit/9fc5197fe9a011abea74c4447a4bd46dca2c5ece) build(deps): bump github.com/docker/docker (#5104)
 * [695db1531](https://github.com/kubeovn/kube-ovn/commit/695db15319e09cb9e4582b79da904e507bea0dc1) build(deps): bump github.com/onsi/gomega from 1.36.2 to 1.36.3 (#5103)
 * [0a6e5e38a](https://github.com/kubeovn/kube-ovn/commit/0a6e5e38a616348e66e27f1e30811bb91d5008d4) build(deps): bump github.com/rs/zerolog from 1.33.0 to 1.34.0 (#5101)
 * [25e392585](https://github.com/kubeovn/kube-ovn/commit/25e39258589b923503cf86c2e332a19bc9145f2d) build(deps): bump github.com/onsi/ginkgo/v2 from 2.23.2 to 2.23.3 (#5102)
 * [30451fe85](https://github.com/kubeovn/kube-ovn/commit/30451fe858cd617ad6a95144c4b4b78df8719fed) build(deps): bump github.com/docker/docker (#5100)
 * [51067fd2a](https://github.com/kubeovn/kube-ovn/commit/51067fd2a5390e6582e7ba1ef8aa90264c5c7d07) bump golangci-lint to v2.0.1 (#5097)
 * [af6c3f6ea](https://github.com/kubeovn/kube-ovn/commit/af6c3f6eaaafc31271c15a7d0988e8154ef7a443) build(deps): bump gopkg.in/k8snetworkplumbingwg/multus-cni.v4 (#5096)
 * [339df023f](https://github.com/kubeovn/kube-ovn/commit/339df023fbe9ae04c3d0b0fa13d49ef604874f8e) build(deps): bump github.com/onsi/ginkgo/v2 from 2.23.1 to 2.23.2 (#5094)
 * [5e624cf18](https://github.com/kubeovn/kube-ovn/commit/5e624cf18b6af088e3da8d99145958019a1d4076) build(deps): bump github.com/docker/docker (#5092)
 * [7b829a4ce](https://github.com/kubeovn/kube-ovn/commit/7b829a4ce4f909c3ef5ce76f7825bee35f0abfd9) build(deps): bump github.com/onsi/ginkgo/v2 from 2.23.0 to 2.23.1 (#5091)
 * [3310f23dd](https://github.com/kubeovn/kube-ovn/commit/3310f23dde1374e3941512ab7916f64163fcedc2) fix: egress network policy not work, when no pod hit matchlabel (#5089)
 * [c64143504](https://github.com/kubeovn/kube-ovn/commit/c64143504e3d944946224a12f308b9b53d03fecc) docs: updated CHANGELOG.md (#5086)
 * [7560a577d](https://github.com/kubeovn/kube-ovn/commit/7560a577db9bd3b7e0a968fb021a45d4c8824fe1) build(deps): bump github.com/containerd/containerd from 1.7.26 to 1.7.27 (#5085)
 * [fc14f7959](https://github.com/kubeovn/kube-ovn/commit/fc14f79598cd6987c3c1a1533ff8e8d5597536d2) build(deps): bump aquasecurity/trivy-action from 0.29.0 to 0.30.0 (#5081)
 * [ba9bcece1](https://github.com/kubeovn/kube-ovn/commit/ba9bcece1c06af831b36fbd9879e4dfe7a119dc0) bind to pod ips when env variable ENABLE_BIND_LOCAL_IP is set to true (#5049)
 * [6f4100d7f](https://github.com/kubeovn/kube-ovn/commit/6f4100d7f79cfa09b3a59f6e8f224a082b6ba75b) cni-server: check ipv6 dadfailed flag (#5042)
 * [b1c5f2565](https://github.com/kubeovn/kube-ovn/commit/b1c5f2565206d6b974f41020919d4884657d6868) [fix] When the Nat-gw pod container restarts unexpectedly, trigger nat-gw statefulset restart to restore the nat-gw pod configuration (#5072)
 * [3f261807e](https://github.com/kubeovn/kube-ovn/commit/3f261807edcbb6652975b783c305663d79429315) bump kubevirt to v1.5.0 (#5080)
 * [d650cfb6f](https://github.com/kubeovn/kube-ovn/commit/d650cfb6f1a5e98136e8890524e0de1f33471663) docs: updated CHANGELOG.md (#5079)
 * [f98582420](https://github.com/kubeovn/kube-ovn/commit/f98582420289fbde1041ec2e81338eb5c330e93d) bump go to 1.24.1 (#5076)
 * [56b54c7c2](https://github.com/kubeovn/kube-ovn/commit/56b54c7c253859bd7af5aec767200e439bc900a5) build(deps): bump golang.org/x/net from 0.35.0 to 0.36.0 in /test/anp (#5075)
 * [0f4fde3ba](https://github.com/kubeovn/kube-ovn/commit/0f4fde3baa19e766ce87690fa8367355cdb249bc) build(deps): bump k8s.io/kubernetes in the k8s-io group (#5074)
 * [7b11e93f9](https://github.com/kubeovn/kube-ovn/commit/7b11e93f9df2d185aebc078b40d6da328ef82e4f) feat: Improve subnet and VPC finalizer handling (#5071)
 * [58e83ce0f](https://github.com/kubeovn/kube-ovn/commit/58e83ce0f327b40a0f252ec3e07d4a761a23b020) feat: Make Kube-OVN namespace configurable with default value (#5069)
 * [d8a4a0590](https://github.com/kubeovn/kube-ovn/commit/d8a4a0590d68255de6d6fcdae134576bd1ac73a6) build(deps): bump sigs.k8s.io/controller-runtime from 0.20.2 to 0.20.3 (#5067)
 * [c26810950](https://github.com/kubeovn/kube-ovn/commit/c268109500f4a68bdf1ea64607f38efdce7830f9) Fix #5028: Orphaned subnets which reference a non-existent VPC cause new namespaces to never get correct annotations. (#5031)
 * [902d37c6f](https://github.com/kubeovn/kube-ovn/commit/902d37c6f34f012a2e131fa3a575b790d0198983) remove genev_sys_6081 when uninstall kube-ovn (#5066)
 * [b303fa119](https://github.com/kubeovn/kube-ovn/commit/b303fa119bd3f57783c97db78a18ca92498ddb69) add copy of the kubevirt informer from upstream (#5065)
 * [c5f5c5e0c](https://github.com/kubeovn/kube-ovn/commit/c5f5c5e0c7f7fbdec4ecdfd9715ce341c250d185) build(deps): bump golang.org/x/tools from 0.30.0 to 0.31.0 (#5059)
 * [bfef0c8a1](https://github.com/kubeovn/kube-ovn/commit/bfef0c8a18cf27dfd1caf03a475f3fa7b49861ba) build(deps): bump github.com/onsi/ginkgo/v2 from 2.22.2 to 2.23.0 (#5060)
 * [dad4cdfba](https://github.com/kubeovn/kube-ovn/commit/dad4cdfbae32d1b33fc4f130b6ee6ad5987b9acb) build(deps): bump golang.org/x/time from 0.10.0 to 0.11.0 (#5054)
 * [bb66e12f1](https://github.com/kubeovn/kube-ovn/commit/bb66e12f1cbfc9befb2de5d63d89871efe6095a2) build(deps): bump golang.org/x/sys from 0.30.0 to 0.31.0 (#5055)
 * [fb2027b28](https://github.com/kubeovn/kube-ovn/commit/fb2027b28baf8034d7a6257d4b53582354deb441) build(deps): bump google.golang.org/grpc from 1.70.0 to 1.71.0 (#5053)
 * [08c48a353](https://github.com/kubeovn/kube-ovn/commit/08c48a353628d3d72f69eea690a4c005a1e6c31f) build(deps): bump golang.org/x/mod from 0.23.0 to 0.24.0 (#5056)
 * [44a8f2045](https://github.com/kubeovn/kube-ovn/commit/44a8f20450c7b845d011b665a6d7afd4120a3f75) build(deps): bump kernel.org/pub/linux/libs/security/libcap/cap (#5052)
 * [7212f8574](https://github.com/kubeovn/kube-ovn/commit/7212f857427b8257b74e7a72073b12771e239bbb) build(deps): bump github.com/prometheus/client_golang (#5051)
 * [6dfea7466](https://github.com/kubeovn/kube-ovn/commit/6dfea74666c7ad81de39dedb1dc2bd2e3192324b) simple vip lable update and then update subnet status (#5036)
 * [fae0f6388](https://github.com/kubeovn/kube-ovn/commit/fae0f6388582ea7c451d5a2fa494ae1ebaee96c1) build(deps): bump github.com/osrg/gobgp/v3 from 3.34.0 to 3.35.0 (#5047)
 * [599c0c3d3](https://github.com/kubeovn/kube-ovn/commit/599c0c3d3319d3e51a13e96639695b863c512d5e) kubectl-ko: fix conntrack state (#5038)
 * [103ddc6f3](https://github.com/kubeovn/kube-ovn/commit/103ddc6f39bef62682eb7d1fbe1746c1eef58e91) build(deps): bump github.com/containerd/containerd from 1.7.25 to 1.7.26 (#5041)
 * [340c48a1c](https://github.com/kubeovn/kube-ovn/commit/340c48a1c8d2c52732fd795ef5b7e87113857239) build(deps): bump github.com/docker/docker (#5033)
 * [4a990cca2](https://github.com/kubeovn/kube-ovn/commit/4a990cca20ea460d26a93ed2958687d4592c4604) build(deps): bump github.com/emicklei/go-restful/v3 (#5032)
 * [aa4cdbea8](https://github.com/kubeovn/kube-ovn/commit/aa4cdbea8dfef15b2f879f21c3fe191bac0fd1a2) fix rerun the completed vim migration (#5020) (#5026)
 * [94c41b1da](https://github.com/kubeovn/kube-ovn/commit/94c41b1da256fcc1832798d169ee888eca3c7497) bfd: fix bfdd-control setting mintx/minrx (#5021)
 * [4005606ea](https://github.com/kubeovn/kube-ovn/commit/4005606ea768e600a1b59b40628aed6fc48613b8) feat(GC): Add check for GC disabled (#5005)
 * [87feaf5e2](https://github.com/kubeovn/kube-ovn/commit/87feaf5e252595c2e95b32826fd914746ae0d03b) build(deps): bump github.com/containernetworking/plugins from v1.6.0 to v1.6.2 (#5017)
 * [3c0d84902](https://github.com/kubeovn/kube-ovn/commit/3c0d849024edd02520f12404191b4422f0959772) fix logging in ovn lr/ls gc (#5014)
 * [91b969818](https://github.com/kubeovn/kube-ovn/commit/91b96981817d5d8d14c99659a0e1dfdc29766761) build(deps): bump github.com/prometheus/client_golang (#5016)
 * [a46d69506](https://github.com/kubeovn/kube-ovn/commit/a46d69506ea307f533d24bc70c0fca5fa7b5ef28) docs: updated CHANGELOG.md (#5012)
 * [1cf92b4c2](https://github.com/kubeovn/kube-ovn/commit/1cf92b4c2766fcb69257c1d4ee7c21c69c33dffc) build(deps): bump azure/setup-helm from 4.2.0 to 4.3.0 (#5011)
 * [7213dc4e3](https://github.com/kubeovn/kube-ovn/commit/7213dc4e3fc61f37befd18cc8ad0d0934848cd62) build(deps): bump github.com/puzpuzpuz/xsync/v3 from 3.5.0 to 3.5.1 (#5001)
 * [c10c6e99a](https://github.com/kubeovn/kube-ovn/commit/c10c6e99a3e1886f6762507c8c6f3408ee51d460) docs: updated CHANGELOG.md (#5000)
 * [a3e4ce730](https://github.com/kubeovn/kube-ovn/commit/a3e4ce730a92a54d3e84957dddfc71074cd51ad7) docs: updated CHANGELOG.md (#4999)
 * [0b45b667a](https://github.com/kubeovn/kube-ovn/commit/0b45b667a2dfeec159d1bb6a19ae60398e1c1666) bump k8s to v1.32.2 (#4992)
 * [f9904d26a](https://github.com/kubeovn/kube-ovn/commit/f9904d26a3b2c7304d70f573353d90acd2dc58e8) controller: respect named ports of restartable init containers (#4994)
 * [d4eb19d16](https://github.com/kubeovn/kube-ovn/commit/d4eb19d16220bd91013cf0b89c2acb931b783cff) build(deps): bump sigs.k8s.io/controller-runtime from 0.20.1 to 0.20.2 (#4993)
 * [48eec14ea](https://github.com/kubeovn/kube-ovn/commit/48eec14ea9c1ccfd8b83398c5093d99f868c2302) build(deps): bump k8s.io/kubernetes in the k8s-io group (#4990)
 * [b44ea855f](https://github.com/kubeovn/kube-ovn/commit/b44ea855f4f7d765da3ff0b117b5311e748da7c2) bump go to 1.24.0 (#4986)
 * [7a36465e3](https://github.com/kubeovn/kube-ovn/commit/7a36465e33a1da8d5d695643d6d5a27bcddf7693) iptables: fix subnet metrics rules (#4979)
 * [d7714f991](https://github.com/kubeovn/kube-ovn/commit/d7714f991d5dbcc5ff940ea60bac6943b4902910) fix superfluous response.WriteHeader (#4980)
 * [31601001c](https://github.com/kubeovn/kube-ovn/commit/31601001c985ef18af770cc54f6057d33d69eae4) e2e: skip some test cases for versions prior to v1.14 in cilium chaining (#4988)
 * [f909a3a7d](https://github.com/kubeovn/kube-ovn/commit/f909a3a7d15ff8789fe6b279653da1d3cfefebfd) remove twitter account
 * [7d7d2ddfd](https://github.com/kubeovn/kube-ovn/commit/7d7d2ddfd3fa463214f8dbd0751dcedd6fe9de81) fix security groups changed when vm is shut down (#4976)
 * [501231a03](https://github.com/kubeovn/kube-ovn/commit/501231a03b2995084c1cf0a3165cd4e64c33bd7f) bump golang.org/x/tools from v0.29.0 to v0.30.0 (#4983)
 * [89a220693](https://github.com/kubeovn/kube-ovn/commit/89a2206935eef5b27756e89a468a329c8e92eeab) ci: bump cilium to v1.17.0 (#4982)
 * [0407520b8](https://github.com/kubeovn/kube-ovn/commit/0407520b814b001a6c0eb239904944f11d148170) e2e: fix ip conflict (#4977)
 * [8beaeab1d](https://github.com/kubeovn/kube-ovn/commit/8beaeab1d6ac91b21711d2fbb7109d4662ab8eef) Revert "refactor: remove duplicated iptables subnet forward rules (#4860)" (#4978)
 * [242b04b72](https://github.com/kubeovn/kube-ovn/commit/242b04b727e60b39cf0690e1290a77cfd8c5d2e9) refactor: remove duplicated iptables subnet forward rules (#4860)
 * [57b373373](https://github.com/kubeovn/kube-ovn/commit/57b373373d3f0dd734cb6646b990c6424141afd9) use httpGet as liveness/readiness probe method (#4945)
 * [7f3031366](https://github.com/kubeovn/kube-ovn/commit/7f30313666ce03edcbc5d0e95b41f2c992db1bce) build(deps): bump google.golang.org/protobuf from 1.36.4 to 1.36.5 (#4970)
 * [30e97c7d6](https://github.com/kubeovn/kube-ovn/commit/30e97c7d619f29bf4ca40a236e202f4882bf600f) bump github.com/hashicorp/yamux from v0.1.1 to v0.1.2 (#4968)
 * [2a1239ffa](https://github.com/kubeovn/kube-ovn/commit/2a1239ffacf3abb914b2fefd3d4af73573e44238) controller: consider StatefulSet's start ordinal (#4967)
 * [61ae78798](https://github.com/kubeovn/kube-ovn/commit/61ae787989cee408c4ec48f95aa4603e25a92a89) bump go to 1.23.6 (#4962)
 * [48ea5af47](https://github.com/kubeovn/kube-ovn/commit/48ea5af479fceb8b52ad8f41ae3b68b5c14b2094) build(deps): bump golang.org/x/time from 0.9.0 to 0.10.0 (#4958)
 * [f5292fd21](https://github.com/kubeovn/kube-ovn/commit/f5292fd21fc512b6dc15d255ab2521bc78e6c2c5) build(deps): bump golang.org/x/sys from 0.29.0 to 0.30.0 (#4959)
 * [497c2e74b](https://github.com/kubeovn/kube-ovn/commit/497c2e74bf19ff51e25fa548d417e2b4b997f701) build(deps): bump github.com/osrg/gobgp/v3 from 3.33.0 to 3.34.0 (#4956)
 * [209b461ea](https://github.com/kubeovn/kube-ovn/commit/209b461eacf6e570cb780a5e90c6a985f76c7786) build(deps): bump github.com/prometheus-community/pro-bing (#4957)
 * [e65a7def2](https://github.com/kubeovn/kube-ovn/commit/e65a7def2a3117d0276ffbd3c2655815a5aff3a0) build(deps): bump github.com/brianvoe/gofakeit/v7 from 7.1.2 to 7.2.1 (#4954)
 * [2074e695e](https://github.com/kubeovn/kube-ovn/commit/2074e695e74ffa6a905e1925aacc2b48d0c5ea5a) build(deps): bump github.com/spf13/pflag from 1.0.5 to 1.0.6 (#4953)
 * [b92359055](https://github.com/kubeovn/kube-ovn/commit/b9235905561bef4d229baf8e438379b1bd552607) build(deps): bump github.com/evanphx/json-patch/v5 from 5.9.10 to 5.9.11 (#4951)
 * [e5a3c4d6c](https://github.com/kubeovn/kube-ovn/commit/e5a3c4d6c277fff2d3628cd7685043d1cdd60090) build(deps): bump github.com/evanphx/json-patch/v5 from 5.9.0 to 5.9.10 (#4949)
 * [b7786ad18](https://github.com/kubeovn/kube-ovn/commit/b7786ad187b756ec479000ac66038ed40ae45c0a) build(deps): bump github.com/golang/glog from 1.2.3 to 1.2.4 (#4950)
 * [a60e21b3d](https://github.com/kubeovn/kube-ovn/commit/a60e21b3d43722c532847167c250102627e6ca9d) build(deps): bump github.com/puzpuzpuz/xsync/v3 from 3.4.1 to 3.5.0 (#4948)
 * [de7ea70eb](https://github.com/kubeovn/kube-ovn/commit/de7ea70ebb5b9125cf7475909adf397173b6a813) build(deps): bump github.com/docker/docker (#4944)
 * [34ece0464](https://github.com/kubeovn/kube-ovn/commit/34ece0464deea380c48b5b6daa0b19b52cf49011) build(deps): bump google.golang.org/protobuf from 1.36.3 to 1.36.4 (#4947)
 * [6c059cef6](https://github.com/kubeovn/kube-ovn/commit/6c059cef6a110a47ee6076841e049de06229cbcc) build(deps): bump google.golang.org/grpc from 1.69.4 to 1.70.0 (#4946)
 * [29e989c94](https://github.com/kubeovn/kube-ovn/commit/29e989c9451b1859a2f51d1b691fd269bf50871e) build(deps): bump sigs.k8s.io/controller-runtime from 0.20.0 to 0.20.1 (#4943)
 * [903c532ff](https://github.com/kubeovn/kube-ovn/commit/903c532ff96f9a9b8055857c6b60cc6f4a9ec5d6) docs(cloudnull): Add Rackspace as a user (#4941)
 * [ead6e6b4e](https://github.com/kubeovn/kube-ovn/commit/ead6e6b4e61ed522cf89be67394b3d73bf504592) build(deps): bump github.com/prometheus-community/pro-bing (#4940)
 * [fe30a54cc](https://github.com/kubeovn/kube-ovn/commit/fe30a54cc2b7221fd3a0e56d0cf1fe3a37774ae0) build(deps): bump helm/chart-releaser-action from 1.6.0 to 1.7.0 (#4938)
 * [2f2b722eb](https://github.com/kubeovn/kube-ovn/commit/2f2b722ebae4a08241852798b91415ee6f84c426) build(deps): bump github.com/puzpuzpuz/xsync/v3 from 3.4.0 to 3.4.1 (#4937)
 * [6b9c9008a](https://github.com/kubeovn/kube-ovn/commit/6b9c9008af3cfdc87a51516610be53aabb727c45) ci: build arm64 images on arm64 hosted runners (#4936)
 * [aeecbd4e3](https://github.com/kubeovn/kube-ovn/commit/aeecbd4e3ac8589bb8fbfaae609e50672178a93a) bump go to 1.23.5 (#4935)
 * [e84536bdd](https://github.com/kubeovn/kube-ovn/commit/e84536bdd8616b97b511913c81980b9c72f004e2) controller: fix gateway nodes check (#4912)
 * [5c9058aec](https://github.com/kubeovn/kube-ovn/commit/5c9058aec4a3c2de6a6e7088fc838584ec6429f5) build(deps): bump sigs.k8s.io/controller-runtime from 0.19.4 to 0.20.0 (#4932)
 * [c0113468f](https://github.com/kubeovn/kube-ovn/commit/c0113468fa15f664a2d6583badaad14ed9debd0f) bump k8s to v1.32.1 (#4930)
 * [773b790dd](https://github.com/kubeovn/kube-ovn/commit/773b790dd6fe1da4c9f8caf995f36780f138df29) fix log (#4928)
 * [ccff9b09c](https://github.com/kubeovn/kube-ovn/commit/ccff9b09c4ee25897b90f8f7e5b7ae30447b8443) make sure gw pod exist before eip creation (#4924)
 * [fba416ade](https://github.com/kubeovn/kube-ovn/commit/fba416adea8edf669305e4330ad63ae756df390f) build(deps): bump google.golang.org/protobuf from 1.36.2 to 1.36.3 (#4927)
 * [af6a1b377](https://github.com/kubeovn/kube-ovn/commit/af6a1b37783905010990df38c95be4364a11b654) [performance] in large-scale clusters, init node router policy too slow (#4895)
 * [756cfd94b](https://github.com/kubeovn/kube-ovn/commit/756cfd94bc12c998faa70e2ec408e719d49adba2) build(deps): bump github.com/docker/docker (#4926)
 * [27f0754c5](https://github.com/kubeovn/kube-ovn/commit/27f0754c57c0612106f06352b3d356519d3449f0) remove dup func and fix ut (#4921)
 * [4dd28f558](https://github.com/kubeovn/kube-ovn/commit/4dd28f5588cca5cafaef31f0611f24f57a328839) build(deps): bump google.golang.org/grpc from 1.69.2 to 1.69.4 (#4923)
 * [d2608530d](https://github.com/kubeovn/kube-ovn/commit/d2608530d2778141c625d5d5bf4e7f3aa0fdfa93) controller: check condition NodeNetworkUnavailable when determining whether node is ready (#4917)
 * [f1d91c9bb](https://github.com/kubeovn/kube-ovn/commit/f1d91c9bb18ad46f8fd3e4e23c91f2dee54b895f) replace reflect.DeepEqual with slices.Equal and maps.Equal (#4918)
 * [e250afa56](https://github.com/kubeovn/kube-ovn/commit/e250afa56ff5d22019a8d7fa07ae7aa026ee3d9b) cni-server: set node NetworkUnavailable condition after join subnet gateway check (#4915)
 * [a7fff996b](https://github.com/kubeovn/kube-ovn/commit/a7fff996b5d0c66b6ea4428d0251844aad4a7d08) build(deps): bump sigs.k8s.io/controller-runtime from 0.19.3 to 0.19.4 (#4911)
 * [8bb726fb3](https://github.com/kubeovn/kube-ovn/commit/8bb726fb3b50c91d227f2d48c0ce98b345af5573) e2e: do not check mac annotation for versions prior to v1.13 (#4910)
 * [d5f620510](https://github.com/kubeovn/kube-ovn/commit/d5f6205107e378c0e487ec69c50cd2d0f461b6e0) ipam: check subnet's available ipv6 address count (#4903)
 * [eb3930813](https://github.com/kubeovn/kube-ovn/commit/eb3930813d4eb179b3fd780407bcc3f0aabf0c1c) base: bump cni plugins to v1.6.2 (#4904)
 * [672af54db](https://github.com/kubeovn/kube-ovn/commit/672af54dbe8a50aa0189b9b3a33c3203651b6ea6) build(deps): bump golang.org/x/tools from 0.28.0 to 0.29.0 (#4905)
 * [bf81d1523](https://github.com/kubeovn/kube-ovn/commit/bf81d1523d746c7851ce53846161764906de5a7b) build(deps): bump google.golang.org/protobuf from 1.36.1 to 1.36.2 (#4907)
 * [604413604](https://github.com/kubeovn/kube-ovn/commit/6044136041af2c8a527faa0e96e7f2e9ca721242) Increase code specification and readability in vxlan nic name and qos (#4896)
 * [d66af7ca5](https://github.com/kubeovn/kube-ovn/commit/d66af7ca5499d7c23affcf5b7007c0268ab3e560) build(deps): bump golang.org/x/time from 0.8.0 to 0.9.0 (#4901)
 * [cbb62ef56](https://github.com/kubeovn/kube-ovn/commit/cbb62ef5613846d5f6ac4d75af7c0aacb48db455) build(deps): bump golang.org/x/sys from 0.28.0 to 0.29.0 (#4900)
 * [44fcf08c9](https://github.com/kubeovn/kube-ovn/commit/44fcf08c90b2900d5701f035381572dd5c621f96) fix: kube-ovn-controller cannot be ready when ENABLE_METRICS is false (#4886)
 * [4d3dd9c7d](https://github.com/kubeovn/kube-ovn/commit/4d3dd9c7db1a73221a7da828a03ba449d08aecf4) controller: generate stable annotations for pod routes (#4889)
 * [ed8bea32f](https://github.com/kubeovn/kube-ovn/commit/ed8bea32fd8ae3846dbcb9c6ded86fcb10658394) e2e: fix duplicate random cidr (#4893)
 * [ac7cf401e](https://github.com/kubeovn/kube-ovn/commit/ac7cf401e2f414bacaf14acdf2b91f261a20bb82) build(deps): bump github.com/osrg/gobgp/v3 from 3.32.0 to 3.33.0 (#4891)
 * [3c6eeaf57](https://github.com/kubeovn/kube-ovn/commit/3c6eeaf57fd927e915161ff63603b501abcc637f) build(deps): bump github.com/onsi/ginkgo/v2 from 2.22.1 to 2.22.2 (#4890)
 * [48f309cd8](https://github.com/kubeovn/kube-ovn/commit/48f309cd8b834a185f539c8527751dc84ed33e54) update readme
 * [010b701f8](https://github.com/kubeovn/kube-ovn/commit/010b701f8e616ab5b778151af418349c1651f538) Support multiple IPPools in the namespace (#4777)
 * [5eb316b03](https://github.com/kubeovn/kube-ovn/commit/5eb316b036be3af8e809635d91f83c49abc7dc43) rename Makefile.e2e to e2e.mk (#4885)
 * [d10251fd7](https://github.com/kubeovn/kube-ovn/commit/d10251fd741033c7983fab2e3e5ac60a11be62b2) ipam: use ip provided by nad annotation when providing IPAM for other CNI plugins (#4883)
 * [bad1bdb3b](https://github.com/kubeovn/kube-ovn/commit/bad1bdb3b34075f0d9603915bdbd94f5906b4fc2) bump go modules used by ANP e2e tests (#4882)
 * [ee6f907be](https://github.com/kubeovn/kube-ovn/commit/ee6f907beb1914f1807840261b24b8d4290e83bf) add nodeSelector for lb-svc (#4793)
 * [38df57725](https://github.com/kubeovn/kube-ovn/commit/38df577256e3d6dab80a86b9179bcc39ff11f5ff) fix golangci-lint (#4880)
 * [c2bc18227](https://github.com/kubeovn/kube-ovn/commit/c2bc18227b1f8b3b0af439c52fbc33a3720602d6) remove unused function (#4877)
 * [9b0072304](https://github.com/kubeovn/kube-ovn/commit/9b00723041cef25d14ed0d1cc5b59cb01798b849) should be able to use mac and ip provided by k8s.v1.cni.cncf.io/networks annotation fix e2e version (#4878)
 * [93759b921](https://github.com/kubeovn/kube-ovn/commit/93759b921408fd52ddcf66e5d17a2b7b4399b3fa) pod should use mac and ips provider by multus firstly (#4800)
 * [79acd891d](https://github.com/kubeovn/kube-ovn/commit/79acd891dd97f8bd504d7bde2fba84c22eccec0b) build(deps): bump github.com/onsi/gomega from 1.36.1 to 1.36.2 (#4871)
 * [e04c2480e](https://github.com/kubeovn/kube-ovn/commit/e04c2480edc24af358c2a969f6b04eed387f72c8) skip node local dns ip conntrack when set acl (#4825)
 * [9aa076bb4](https://github.com/kubeovn/kube-ovn/commit/9aa076bb403108f3da4c54257012d8c29fdeb3b2) Add default subnet in custom vpc to the beginning of the list (#4826)
 * [cec47b529](https://github.com/kubeovn/kube-ovn/commit/cec47b5290207f8f943d68fd11a43f8e0ed515c8) keep dockerfile variable the same as download-go-deps.sh (#4863)
 * [bf5240087](https://github.com/kubeovn/kube-ovn/commit/bf5240087f37e697b427830f424ab38e38d8ed70) fix(controller/subnet): controller crashes on subnets if gateway is unspecified and netpol are disabled (#4848)
 * [8ecbae8b4](https://github.com/kubeovn/kube-ovn/commit/8ecbae8b4d36e33cd1ff3cf646ad6ab70cf7ac0b) clean up legacy iptables rules only when iptables/ip6_tables is loaded (#4855)
 * [b110300fa](https://github.com/kubeovn/kube-ovn/commit/b110300fa9c1769fb4f998b9f13efb9b4128a7bd) build(deps): bump helm/kind-action from 1.11.0 to 1.12.0 (#4868)
 * [454817da3](https://github.com/kubeovn/kube-ovn/commit/454817da3b78038c69310aa48c3c57316b67540f) build(deps): bump google.golang.org/protobuf from 1.36.0 to 1.36.1 (#4869)
 * [3245c91cf](https://github.com/kubeovn/kube-ovn/commit/3245c91cf34b0a7eed3484c74339852a214977cf) build(deps): bump github.com/onsi/ginkgo/v2 from 2.22.0 to 2.22.1 (#4861)
 * [6478007b7](https://github.com/kubeovn/kube-ovn/commit/6478007b742c76f4c831a4f933391d1dd0c871b2) fix: err is always nil (#4857)
 * [3e78b22da](https://github.com/kubeovn/kube-ovn/commit/3e78b22da960aebac76990ca2b5b229b690d508b) deps: bump golang.org/x/net to v0.33.0 (#4851)
 * [87016253a](https://github.com/kubeovn/kube-ovn/commit/87016253abcca997a3787b3156f0479ff5104566) build(deps): bump github.com/docker/docker (#4850)
 * [1caca2c56](https://github.com/kubeovn/kube-ovn/commit/1caca2c56df18013682adfd2f89497c569cb8532) fix gateway node check for centralized ecmp subnets (#4847)
 * [0a8adbf4c](https://github.com/kubeovn/kube-ovn/commit/0a8adbf4c9cdf517d9d45fa96a95f15b6069a8d9) remove unused function (#4821)
 * [96ad24588](https://github.com/kubeovn/kube-ovn/commit/96ad2458880a39993677e8166e2e7786dc0149bc) use cache.MetaObjectToName() to get namespaced name (#4842)
 * [a88306ac0](https://github.com/kubeovn/kube-ovn/commit/a88306ac099ccc5d69ae4419acdad757ac87fff5) use JSON merge patch to update labels/annotations (#4838)
 * [64acd012c](https://github.com/kubeovn/kube-ovn/commit/64acd012c1f0cc80a735daab1c4d85b488fe2bf9) fix getting subnet cidr by protocol (#4844)
 * [4875f23c6](https://github.com/kubeovn/kube-ovn/commit/4875f23c6be6e17b1650fee0ed839d2e37567b99) ci: wait for kubevirt crd to be created before creating CR (#4839)
 * [bcf771332](https://github.com/kubeovn/kube-ovn/commit/bcf771332a6dd1a4b712a359bf59414d52c71acd) build(deps): bump google.golang.org/protobuf from 1.35.2 to 1.36.0 (#4846)
 * [517f0e488](https://github.com/kubeovn/kube-ovn/commit/517f0e488e26c0f8a44539cd3ebc784f5374fe4d) e2e: add test case for specifying mac address when nad plugin is macvlan (#4836)
 * [9291e1f17](https://github.com/kubeovn/kube-ovn/commit/9291e1f17d17c3725f109e7c8c7b9a07295972f8) build(deps): bump helm/kind-action from 1.10.0 to 1.11.0 (#4837)
 * [6c109a9ef](https://github.com/kubeovn/kube-ovn/commit/6c109a9efe565a2f5116e58c2e3fd8f4816d614a) refactor: remove redundant policy route addition in node handling (#4835)
 * [77650e593](https://github.com/kubeovn/kube-ovn/commit/77650e5931c708dc39bba1792cbe93d9eb368613) fix  some normative issues 24.12.16 (#4833)
 * [ab4824e3a](https://github.com/kubeovn/kube-ovn/commit/ab4824e3a796747635bbc1135fccee0b9598f29e) docs: updated CHANGELOG.md (#4832)
 * [b2148a224](https://github.com/kubeovn/kube-ovn/commit/b2148a22492d8db7a360896e6504c89ad15b6049) docs: updated CHANGELOG.md (#4831)
 * [d8064d85e](https://github.com/kubeovn/kube-ovn/commit/d8064d85e9c285b766cf6d13a6ad07ef31901e44) build(deps): bump google.golang.org/grpc from 1.68.1 to 1.69.0 (#4830)
 * [bd3593da6](https://github.com/kubeovn/kube-ovn/commit/bd3593da679afee8e05e9fc99b5c6407a186bb2d) api: add scale subresource for vpc-egress-gateway (#4829)
 * [514a2b643](https://github.com/kubeovn/kube-ovn/commit/514a2b643e26b29e4102a1fe1b3a38938be082d1) build(deps): bump k8s.io/kubernetes from 1.31.4 to 1.32.0 (#4827)
 * [e2e6dd77c](https://github.com/kubeovn/kube-ovn/commit/e2e6dd77c8e7b8f858e4c77351298b03f627644e) cni: do not exit if the sysctl variable does not exist or can not be set (#4828)
 * [386290793](https://github.com/kubeovn/kube-ovn/commit/38629079338cd643470e3a059778b5cb88bebd36) add anp/banp unittests (#4774)
 * [7006e9cb8](https://github.com/kubeovn/kube-ovn/commit/7006e9cb8af3b0889d3b1c44474d93ba54cb2fa5) fix(helm): add get on crd for ovn-cr (#4816)
 * [a3d301c00](https://github.com/kubeovn/kube-ovn/commit/a3d301c0003442fc30fcb5ca22f3b411a28a74fa) build(deps): bump github.com/brianvoe/gofakeit/v7 from 7.0.0 to 7.1.2 (#4818)
 * [b5c977863](https://github.com/kubeovn/kube-ovn/commit/b5c97786350a65d5d1462686ccb33d1e3a04bfd9) docs: updated CHANGELOG.md (#4815)
 * [1218d0b20](https://github.com/kubeovn/kube-ovn/commit/1218d0b20b3dff0366e6b82c046a44afa702f280) add some unittests (#4790)
 * [35fcdc1fb](https://github.com/kubeovn/kube-ovn/commit/35fcdc1fbe606802ed32f282285bfaac0fa25f43) bump k8s to v1.31.4 (#4813)
 * [922b5578e](https://github.com/kubeovn/kube-ovn/commit/922b5578e6422ea8903327e2421b793b8af1a918) build(deps): bump github.com/docker/docker (#4812)
 * [57107d297](https://github.com/kubeovn/kube-ovn/commit/57107d297c7506d2946489ab0ae747e8341f27a4) build(deps): bump github.com/onsi/gomega from 1.36.0 to 1.36.1 (#4811)
 * [ac81ac29f](https://github.com/kubeovn/kube-ovn/commit/ac81ac29f9027fc23f069eb255ccdf20f91b59e7) fix issue 4803: The two names should have a containment relationship (#4807)
 * [bc6379ba5](https://github.com/kubeovn/kube-ovn/commit/bc6379ba57205cb365aee7f6be8193ee975417d1) ci: fix names of artifacts uploaded by vpc egress agteway e2e test (#4799)
 * [1844fe7d4](https://github.com/kubeovn/kube-ovn/commit/1844fe7d4c9925af5e3812f56c38002b81284091) base: fix underlay network break during upgrade from v1.12 (#4797)
 * [69b727140](https://github.com/kubeovn/kube-ovn/commit/69b727140ee1eeaf1c7eda702b0cb2ee398da7d8) build(deps): bump gopkg.in/k8snetworkplumbingwg/multus-cni.v4 (#4806)
 * [322fb8e90](https://github.com/kubeovn/kube-ovn/commit/322fb8e900c94d332e29e6e338b3cef72d9250d2) auto detect kubevirt install (#4791)
 * [6b6bf2d8b](https://github.com/kubeovn/kube-ovn/commit/6b6bf2d8bdd044db8fe0e1a1b5fd966b9f6177d7) build(deps): bump google.golang.org/grpc from 1.68.0 to 1.68.1 (#4796)
 * [6016e30c9](https://github.com/kubeovn/kube-ovn/commit/6016e30c993b4adc79df529ad8e6c660cf389684) build(deps): bump golang.org/x/sys from 0.27.0 to 0.28.0 (#4792)
 * [86ad84bd5](https://github.com/kubeovn/kube-ovn/commit/86ad84bd552a3c1a02f8421fe6a43d8a240db6b9) ci: report unittest coverage (#4783)
 * [6ee3b3636](https://github.com/kubeovn/kube-ovn/commit/6ee3b363685542b64d932b610fbe75c42d74bc73) bump go to 1.23.4 (#4781)
 * [f73fbaceb](https://github.com/kubeovn/kube-ovn/commit/f73fbaceb84cecf35d0c3df28b55f6ee18c86622) build(deps): bump github.com/osrg/gobgp/v3 from 3.31.0 to 3.32.0 (#4787)
 * [bd0bd0323](https://github.com/kubeovn/kube-ovn/commit/bd0bd0323d4d45eee2d8b7bd2466ac7a58ae92f7) build(deps): bump sigs.k8s.io/controller-runtime from 0.19.2 to 0.19.3 (#4788)
 * [e642eef8f](https://github.com/kubeovn/kube-ovn/commit/e642eef8f2ef595b6b5ba726d37b37a7365788b0) ci: bump kubevirt to v1.4.0 (#4782)
 * [e34d5624a](https://github.com/kubeovn/kube-ovn/commit/e34d5624a7135e916c11d1476adb33c2cb70bfc7) feature: VPC Egress Gateway (#4692)
 * [ae0fc894d](https://github.com/kubeovn/kube-ovn/commit/ae0fc894d4163bdef89dad393ed0ff482222f418) build(deps): bump kernel.org/pub/linux/libs/security/libcap/cap (#4779)
 * [7b633257b](https://github.com/kubeovn/kube-ovn/commit/7b633257b895fd45383a5f27563c639cabefca15) add kubevirt live migration optimize (#4773)
 * [a80490a4e](https://github.com/kubeovn/kube-ovn/commit/a80490a4e2ea673debe37d8dc572b885254b6998) build(deps): bump github.com/stretchr/testify from 1.9.0 to 1.10.0 (#4765)
 * [6df02a803](https://github.com/kubeovn/kube-ovn/commit/6df02a803ac4012061b9a823c8ad2e8b23251c09) build(deps): bump github.com/onsi/gomega from 1.35.1 to 1.36.0 (#4766)
 * [5c827f18e](https://github.com/kubeovn/kube-ovn/commit/5c827f18e575d3c0f3262d7833f40dfd17affdd0) docs: updated CHANGELOG.md (#4763)
 * [95ceeffac](https://github.com/kubeovn/kube-ovn/commit/95ceeffaca14ff6f342681bedb9c5a6d281fe162) remove capability SYS_MODULE (#4744)
 * [35f181ba7](https://github.com/kubeovn/kube-ovn/commit/35f181ba74bec18090cfb9f87d1cc2f66c637bc0) add observed generation into condition (#4751)
 * [6c55c613c](https://github.com/kubeovn/kube-ovn/commit/6c55c613c28f618570497a643400097b0c8f8fde) build(deps): bump k8s from 1.31.2 to 1.31.3 (#4754)
 * [623039c09](https://github.com/kubeovn/kube-ovn/commit/623039c09a0d273041c7ad5f50f82c8f25f90c28) ci: add release-1.13 to workflows (#4746)
 * [f6e2a253b](https://github.com/kubeovn/kube-ovn/commit/f6e2a253b9fbf8ffed28eb00edc5a36700e59def) build(deps): bump sigs.k8s.io/controller-runtime from 0.19.1 to 0.19.2 (#4757)
 * [be8439cd6](https://github.com/kubeovn/kube-ovn/commit/be8439cd626e4d1adfd95dcbb080f86bb1b3ddc6) build(deps): bump github.com/prometheus-community/pro-bing (#4758)
 * [06ac37d42](https://github.com/kubeovn/kube-ovn/commit/06ac37d42cfe14d5ab21a0847333ead9f6239662) build(deps): bump github.com/onsi/ginkgo/v2 from 2.21.0 to 2.22.0 (#4755)
 * [c3ae8059a](https://github.com/kubeovn/kube-ovn/commit/c3ae8059a865faf62e115f5a3e1ad4aaeb9a34df) split type definitions into separate files (#4750)
 * [17606a126](https://github.com/kubeovn/kube-ovn/commit/17606a1265572da83d44e8d5d0b3aa5b651fac7f) build(deps): bump aquasecurity/trivy-action from 0.28.0 to 0.29.0 (#4752)
 * [671ad64f5](https://github.com/kubeovn/kube-ovn/commit/671ad64f5654843c38865418d906b693042e8eec) add not found err check for lb-svc (#4748)
 * [6e158d6c8](https://github.com/kubeovn/kube-ovn/commit/6e158d6c8cd290272689db800a1a8db8ba6d7a92) update release script (#4749)
 * [623ab01ba](https://github.com/kubeovn/kube-ovn/commit/623ab01ba6d80cbdb24b6e23fed312393d083d00) vpc: add support for dedicated BFD LRP (#4717)
 * [bf9bea1e3](https://github.com/kubeovn/kube-ovn/commit/bf9bea1e3d659fa81779f6aa260ed17a1716a884) docs: updated CHANGELOG.md (#4743)
 * [6481c4a4e](https://github.com/kubeovn/kube-ovn/commit/6481c4a4eda95534e2e08f72937daf10673b0a3a) update release scripts
 * [3d92364b6](https://github.com/kubeovn/kube-ovn/commit/3d92364b67f07f0ea1f2dd1dd0ab9633cdf7ccad) prepare for next release

### Contributors

 * Congqi Zhao
 * Johann Schley
 * Karol Szwaj
 * Kevin Carter
 * Mengxin Liu
 * QEDQCD
 * Robin Lee
 * SKALA NETWORKS
 * Zespre Chang
 * andrewlee1089
 * bobz965
 * changluyi
 * cmdy
 * coldzerofear
 * dependabot[bot]
 * dolibali
 * github-actions[bot]
 * hzma
 * jimyag
 * netdever
 * renovate[bot]
 * xiaoyie
 * zbb88888
 * 张祖建

## v1.13.15 (2025-08-21)

 * [f859bbada](https://github.com/kubeovn/kube-ovn/commit/f859bbada8cdea21406366e02c93fbf48ab68ff0) release v1.13.15
 * [50e9d0c55](https://github.com/kubeovn/kube-ovn/commit/50e9d0c5564ed3fefeb2d4f1f639be17d367e7aa) fix ovn ipsec when restart cni (#5603)
 * [496e9a065](https://github.com/kubeovn/kube-ovn/commit/496e9a065cf4516c681fd8ce8e4422408321be53) fix static mac pod conflict with gateway mac (#5623)
 * [51c2e4e2a](https://github.com/kubeovn/kube-ovn/commit/51c2e4e2abfe7324f010a8c946cb20e9009ca3bd) fix lint issues
 * [0a5655f3d](https://github.com/kubeovn/kube-ovn/commit/0a5655f3db67f3a55db7d2290df47fd70a786a2f) chore(deps): update dependency go to v1.25.0
 * [f309aff9f](https://github.com/kubeovn/kube-ovn/commit/f309aff9fe74904b43ec93980afb313bffb7c985) remove lsp when gw nodes change (#5591)
 * [106e03219](https://github.com/kubeovn/kube-ovn/commit/106e03219498dc9cb5cd3241b2a6a105712fe2f5) chore(deps): update dependency go to v1.24.6 (#5580)
 * [2f63f5084](https://github.com/kubeovn/kube-ovn/commit/2f63f5084a58bd86671f501ce70b34e7814a8a4d) fix(deps): update golang (#5573)
 * [05c3d27b7](https://github.com/kubeovn/kube-ovn/commit/05c3d27b79522217e8c56ae7ff3caf6079bf58ba) vm static ip validate should use vm name (#5569)
 * [5f8f8c48c](https://github.com/kubeovn/kube-ovn/commit/5f8f8c48c5a56c9605bbcddc947b008cf16fbc33) Fix the problem that if available ip is 0 but there is a value in excludeIPs, the fixed ip is used as the ip in excludeIPs but the error noAddressAvaliable is still reported (#5567)
 * [8f4fb8239](https://github.com/kubeovn/kube-ovn/commit/8f4fb823978abd641899c8eb554ed8bb17cb5a79) skip mv if config file already exists (#5558)
 * [e22ba5df2](https://github.com/kubeovn/kube-ovn/commit/e22ba5df267b048fabbf4abd69e1eb0ca8dc6dd9) skip nad without config (#5560)
 * [017b3c55c](https://github.com/kubeovn/kube-ovn/commit/017b3c55c7a51cf5df247bd7dff2818a808d6566) tolerate duplicate ACLs (#5552)
 * [5157edb98](https://github.com/kubeovn/kube-ovn/commit/5157edb98110087fb8565ad38bf494a9ae963135) do not handle update external vpcs
 * [57815e403](https://github.com/kubeovn/kube-ovn/commit/57815e403ae2f92c4a18383bdeab973cf2de289a) chore: add verbose guard to endpoint update (#5549)
 * [49b93a904](https://github.com/kubeovn/kube-ovn/commit/49b93a9041a95d64ea16308e5a84d828b62dea80) fix vpcNatGw status cannot be updated (#5547)
 * [329eb9511](https://github.com/kubeovn/kube-ovn/commit/329eb9511964b0ed24c0a193ef3826834124854b) fix nat-gateway arping (#5546)
 * [fe53bb088](https://github.com/kubeovn/kube-ovn/commit/fe53bb088d86797d51d4e56ac6d96edd6ce35aa6) CVE fix
 * [c013f2668](https://github.com/kubeovn/kube-ovn/commit/c013f26688c229e7c11c44779eba81ab5d5d1194) check underlay nic exist before config external bridge (#5520)
 * [bf37d409a](https://github.com/kubeovn/kube-ovn/commit/bf37d409a664e97a28fb924110e606ca6f16e1a3) fix NB Global not updated after OVN IC is disabled (#5511)
 * [631158fe0](https://github.com/kubeovn/kube-ovn/commit/631158fe08d522d0e8746fab8a001d55204c85bd) metrics: fix pinger_inconsistent_port_binding (#5496)
 * [40819c07d](https://github.com/kubeovn/kube-ovn/commit/40819c07df861691d48d9cf9f10813bc6080c85f) fix: only addOrUpdateSubnetQueue if the GatewayType is distributed instead of if it's not centralized (#5493)
 * [ecb62e10c](https://github.com/kubeovn/kube-ovn/commit/ecb62e10c170532296fc7a27fecdc5a5d21a5c15) controller: fix setting LR options on initialization (#5444)
 * [8579165ec](https://github.com/kubeovn/kube-ovn/commit/8579165eca97d758797d545635e7755ec23a3a6e) kubectl-ko: collect information about ipsec and xfrm (#5472)
 * [384ad0b6c](https://github.com/kubeovn/kube-ovn/commit/384ad0b6cc59327c803469e7979aa351fd897da6) chore(deps): update module golang.org/x/tools to v0.35.0 (#5477)
 * [7e9304e35](https://github.com/kubeovn/kube-ovn/commit/7e9304e35404bcded0f8f245c314338a360678f8) chore(deps): update module golang.org/x/net to v0.42.0 (#5468)
 * [4908becee](https://github.com/kubeovn/kube-ovn/commit/4908beceefce9d31c2bbcb9b2b04350e47fc148d) chore(deps): update module golang.org/x/term to v0.33.0 (#5452)
 * [244f37324](https://github.com/kubeovn/kube-ovn/commit/244f37324cc8cacdbd301402e1dff6afc14766e5) chore(deps): update module golang.org/x/text to v0.27.0 (#5453)
 * [26ed85ac3](https://github.com/kubeovn/kube-ovn/commit/26ed85ac32598608805fb684b28534576eb43bc4) fix(deps): update module golang.org/x/mod to v0.26.0 (#5454)
 * [bc5ce5ea7](https://github.com/kubeovn/kube-ovn/commit/bc5ce5ea7ae050eb0530578db706dd676336bc95) chore(deps): update module golang.org/x/sync to v0.16.0 (#5451)
 * [d01a17ec2](https://github.com/kubeovn/kube-ovn/commit/d01a17ec244cd12996d2ef6dc32554af8ae3a481) bump go to 1.23.11 (#5441)
 * [c5c29d1c2](https://github.com/kubeovn/kube-ovn/commit/c5c29d1c273efb97ade595f94a0e6e0eed7ccd80) bump github.com/go-viper/mapstructure/v2 to v2.3.0
 * [71e168ddd](https://github.com/kubeovn/kube-ovn/commit/71e168ddd72a42c63f1d545d498da435875c41ff) fix setting LR option always_learn_from_arp_request (#5426)
 * [3e0531d63](https://github.com/kubeovn/kube-ovn/commit/3e0531d63991a49e526f08158039217f6fdef749) chore(deps): update golang.org/x/exp digest to b7579e2 (#5413)
 * [5e71c3c5c](https://github.com/kubeovn/kube-ovn/commit/5e71c3c5c77a05d1e69da54f5ea59a2b6d583f76) controller: set always_learn_from_arp_request to false only when LR is not connected to external network (#5419)
 * [3e3187b50](https://github.com/kubeovn/kube-ovn/commit/3e3187b50a2f405f2a35ff264362c4710ff3e662) fix parsing resolv.conf when systemd-resolved is running on the host (#5423)
 * [323ce7e3f](https://github.com/kubeovn/kube-ovn/commit/323ce7e3f31f237b8d89f46d8dda7d34fd6234fa) cni-server: fix inserting and deleting iptables rules (#5421)
 * [ae247edd1](https://github.com/kubeovn/kube-ovn/commit/ae247edd1a0ffc33264e308aa015de01716776e5) ci: deploy ipv6 talos cluster without ipv4 addresses (#5401)
 * [663a68a2a](https://github.com/kubeovn/kube-ovn/commit/663a68a2a2585433a156718ecba557dddd5c82a5) del and add acls in one transaction (#5394)
 * [46d145e25](https://github.com/kubeovn/kube-ovn/commit/46d145e252de6e9f193623394bc20b85b5e01ce8) wrong code (#5391)
 * [01bfde741](https://github.com/kubeovn/kube-ovn/commit/01bfde741f24987c12d332baf7cd72670ec1b496) fix migrate down time (#5388)
 * [12eb90b15](https://github.com/kubeovn/kube-ovn/commit/12eb90b15b477b772735d92208b9bfeeecda4986) fix duplicate acls because of parentkey (#5357)
 * [83fbcae79](https://github.com/kubeovn/kube-ovn/commit/83fbcae79c01b3904fca9221fc0b1e0628e6c736) fix(slr): deleting old entries from ipmapping to avoid clogging loadbalancer (#5380)
 * [871886f27](https://github.com/kubeovn/kube-ovn/commit/871886f27b773c7097993404c888c12e477d3665) fix(slr): switchlbrule doesn't support multi-homed/ipv6-first pods (#5375)
 * [b19557edc](https://github.com/kubeovn/kube-ovn/commit/b19557edc4db36d6bedfad733088949049ca3d36) fix ip cr not delete with pod in the case of subnet not exist (#5364)
 * [6e0214aa2](https://github.com/kubeovn/kube-ovn/commit/6e0214aa2cd7af8022146703022f3adef20a68e9) prepare for next release

### Contributors

 * Joachim Hill-Grannec
 * Kevin Carter
 * Mengxin Liu
 * SKALA NETWORKS
 * changluyi
 * renovate[bot]
 * xieyanker
 * zbb88888
 * zhangzujian
 * 张祖建

## v1.13.14 (2025-06-16)

 * [91d775e92](https://github.com/kubeovn/kube-ovn/commit/91d775e9245ff0efcedc0292d018b9c6f494e9a1) release v1.13.14
 * [d046a4fbe](https://github.com/kubeovn/kube-ovn/commit/d046a4fbe78c74a2304309026714b3d55f0e490d) fix undefined strings.SplitSeq
 * [fb38751f1](https://github.com/kubeovn/kube-ovn/commit/fb38751f1d561b8dcb413579616669d85c14aca8) fix(slr): address family and familypolicy isn't correct (#5349)
 * [558148287](https://github.com/kubeovn/kube-ovn/commit/558148287325a9981ee64cdabbef6017f8b94e0d) controller: migrate acl tier after upgrade (#5351)
 * [dcbdf8867](https://github.com/kubeovn/kube-ovn/commit/dcbdf8867b4f4182a54635b2edb053bc830eae30) fix sts/vm lsp in incorrect port groups after rescheduled to another node (#5345)
 * [e24165271](https://github.com/kubeovn/kube-ovn/commit/e241652718c690512c50fb961bed3dba13b8c264) fix: vm has multi nic in the same subnet, but release all (#5336)
 * [b5efc5122](https://github.com/kubeovn/kube-ovn/commit/b5efc5122bf3a93b32d20c3369877c750eadf4be) fix missing package import
 * [d82a69399](https://github.com/kubeovn/kube-ovn/commit/d82a693997ebd3b5c92e3cc95870a64b5dddda1b) netpol: fix missing ACL name (#5281)
 * [d0fdd5e67](https://github.com/kubeovn/kube-ovn/commit/d0fdd5e67b9f68dd8bec6cd4e1cff8c19bfb2f86) fix: kubectl-ko: properly get the number of nodes (#5327)
 * [960d9aaa1](https://github.com/kubeovn/kube-ovn/commit/960d9aaa1d750f1efff8603a541600380e0d5ccc) bump go to 1.23.10 (#5322)
 * [b64b866ce](https://github.com/kubeovn/kube-ovn/commit/b64b866cedd211685253ad570dd36f0e055fcbe8) fix podcidr route still use join ip as src ip (#5287)
 * [fc9a5e923](https://github.com/kubeovn/kube-ovn/commit/fc9a5e9235ffd28849dd6c58ae492469c6d8308e) prepare for next release

### Contributors

 * Mengxin Liu
 * Robin Lee
 * SKALA NETWORKS
 * changluyi
 * zbb88888
 * zhangzujian
 * 张祖建

## v1.13.13 (2025-05-27)

 * [21635236f](https://github.com/kubeovn/kube-ovn/commit/21635236f969b5635149ac03fea8cef293b78a91) release v1.13.13
 * [df88d972b](https://github.com/kubeovn/kube-ovn/commit/df88d972b04ac07474900a3575ba0c3440b26c97) bump k8s to v1.31.9 (#5265)
 * [72edcebd3](https://github.com/kubeovn/kube-ovn/commit/72edcebd359652645500a6024692b5e266233bcd) skip cve for no upstream fixes
 * [edca939ea](https://github.com/kubeovn/kube-ovn/commit/edca939ea9cf67a1146677e1ad66b2f4ac222edf) base: update ovn patches (#5264)
 * [83825f277](https://github.com/kubeovn/kube-ovn/commit/83825f27734c90ca9bd87eeaa10e09e698c8f2cd) cleanup: delete additional ConfigMaps and RoleBindings in cleanup script (#5255)
 * [36f1ce3b3](https://github.com/kubeovn/kube-ovn/commit/36f1ce3b3023a19165516ac5ceafc1c377555485) fix: put iptables nat prerouting rule right before kube-proxy inserted one (#5232)
 * [91f1f5726](https://github.com/kubeovn/kube-ovn/commit/91f1f572659b07e171cad6b62ff79507a712ca98) Update upgrade-ovs.sh to use POD_NAMESPACE variable for fetching update strategy (#5243)
 * [4d0a0b479](https://github.com/kubeovn/kube-ovn/commit/4d0a0b479d261bf5c806c3759bd55296146038bb) prepare for next release

### Contributors

 * Mengxin Liu
 * Zespre Chang
 * jimyag
 * 张祖建

## v1.13.12 (2025-05-14)

 * [0796f1955](https://github.com/kubeovn/kube-ovn/commit/0796f19553d6a62c504123ba95673f911e89bbf9) release v1.13.12
 * [009a14b9b](https://github.com/kubeovn/kube-ovn/commit/009a14b9beecf2a3673bd8f56a89d2868796f616) Deleting a FIP sets the referenced EIP to be ready (#5141)
 * [c6563edf6](https://github.com/kubeovn/kube-ovn/commit/c6563edf65c5909640b41cf7ccd7af05fb99d08b) We've seen instances of a VPC being left with a now-deleted subnet on (#5228)
 * [783a0c1da](https://github.com/kubeovn/kube-ovn/commit/783a0c1dab99a00f91b1d4c02206c03adbfa0c3f) ci: fix cilium chaining with underlay networking (#5226)
 * [c697a0ab0](https://github.com/kubeovn/kube-ovn/commit/c697a0ab083be2a73c4bde8bd957c6e5d0ddbaf0) bump go to 1.23.9 (#5215)
 * [0f7e84a0c](https://github.com/kubeovn/kube-ovn/commit/0f7e84a0c43dcdb127304d71054a39329c6733ca) build(deps): bump golang.org/x/sys from 0.32.0 to 0.33.0 (#5204)
 * [4300f7726](https://github.com/kubeovn/kube-ovn/commit/4300f7726bbc9a85b4aa2adff589ec91bead0130) build(deps): bump go.uber.org/mock from 0.5.1 to 0.5.2 (#5198)
 * [36b84b58c](https://github.com/kubeovn/kube-ovn/commit/36b84b58c256755bd5c6762f6c57baa64cc36983) base: use local patch files (#5206)
 * [26ed8a131](https://github.com/kubeovn/kube-ovn/commit/26ed8a13110c5141797992ac785931c833a7eb1a) fix ip clean (#5193)
 * [6d149712e](https://github.com/kubeovn/kube-ovn/commit/6d149712e34cf92ab1bf95da0cfa8ebd9f0afe6c) prepare for next release

### Contributors

 * Mengxin Liu
 * andrewlee1089
 * dependabot[bot]
 * zbb88888
 * 张祖建

## v1.13.11 (2025-04-24)

 * [147139f54](https://github.com/kubeovn/kube-ovn/commit/147139f54ef9bf739b834037e692223babe75a96) release v1.13.11
 * [a5775069e](https://github.com/kubeovn/kube-ovn/commit/a5775069e94afc944c97670f77a83fe060554078) Makefile: fix installing dev version on talos (#5179)
 * [d65e05601](https://github.com/kubeovn/kube-ovn/commit/d65e05601bdebd2f0f79f461f616dcffade8e01d) ci: ignore cni-server restarts caused by join network check failure (#5177)
 * [5349262c6](https://github.com/kubeovn/kube-ovn/commit/5349262c68e9b0926d746bd9a5c153d1b4f77310) ci: add tests for underlay installation on Talos (#5147)
 * [1be8e788a](https://github.com/kubeovn/kube-ovn/commit/1be8e788a4c1bc3ef766a74ccc889159fe9b3513) ci: fix kvm/libvirt installation (#5150)
 * [7f1cdd275](https://github.com/kubeovn/kube-ovn/commit/7f1cdd275eb71a973899633c04cbf03855d43da2) ci: add installation test for Talos Linux (#5109)
 * [38597cd96](https://github.com/kubeovn/kube-ovn/commit/38597cd96f25607d6aa7494ba0a75ecf25cfa76f) bump k8s to v1.31.8 (#5176)
 * [c71c4459a](https://github.com/kubeovn/kube-ovn/commit/c71c4459ad9d39267b28f9fd0cdd9e519f46121d) base: update ovn patch (#5175)
 * [807ec7d74](https://github.com/kubeovn/kube-ovn/commit/807ec7d7403b7d668f3449b03b6574f0d6841e7c) remove capability SYS_MODULE (#4744)
 * [8e97319b8](https://github.com/kubeovn/kube-ovn/commit/8e97319b87640a8afb22b50a19de4f6931573648) fix: clean up garbage lsp (#5172)
 * [e1e0a6b1c](https://github.com/kubeovn/kube-ovn/commit/e1e0a6b1c072d84ffb73b4d5737b8e8bfc5a1a8a) prepare for next release

### Contributors

 * Mengxin Liu
 * changluyi
 * zhangzujian
 * 张祖建

## v1.13.10 (2025-04-21)

 * [421eb1ccd](https://github.com/kubeovn/kube-ovn/commit/421eb1ccd048433e78ed159104351a346d6c968c) release v1.13.10
 * [84c796490](https://github.com/kubeovn/kube-ovn/commit/84c7964905cde010fc23bbe8c366fecc75d6cc2b) controller: ensure ovn route policy is reconciled after node is initialized (#5166)
 * [d913b72ef](https://github.com/kubeovn/kube-ovn/commit/d913b72ef0038480ffe620253e99b11d4a278174) prepare for next release

### Contributors

 * Mengxin Liu
 * 张祖建

## v1.13.9 (2025-04-18)

 * [4afa60ad8](https://github.com/kubeovn/kube-ovn/commit/4afa60ad8260e7d029c6505af953e13ad667ba36) release v1.13.9
 * [765125723](https://github.com/kubeovn/kube-ovn/commit/76512572373927573247f886527a8707db76402c) chart: fix ovs ipsec keys host path (#5137)
 * [f614eaa40](https://github.com/kubeovn/kube-ovn/commit/f614eaa409e2a3e4d6d9ad4e4007c1d699da8246) Add Finalizer to FIP before programming FIP into the VPC NT Gateway (#5142)
 * [c3ded819c](https://github.com/kubeovn/kube-ovn/commit/c3ded819c2fddd737abe5e01a24d50febc866b01) Support multiple IPPools in the namespace (#4777)
 * [29aa6bee7](https://github.com/kubeovn/kube-ovn/commit/29aa6bee7d4544bb5ce97daee7711edfadbed582) fix dbus/NetworkManager connection in Talos (#5140)
 * [588937f3d](https://github.com/kubeovn/kube-ovn/commit/588937f3daacbf0f601acbc46333d6ff52386516) bump go to 1.23.8 (#5138)
 * [6bdf5a5e5](https://github.com/kubeovn/kube-ovn/commit/6bdf5a5e5bddb07db119703c67cc0f223cda68b5) chart: fix local bin directory host path (#5136)
 * [51c8ad2cc](https://github.com/kubeovn/kube-ovn/commit/51c8ad2cc4075316b6fce5e9c7ec025bfcd6d747) base: update ovn patches (#5139)
 * [8174aaf6c](https://github.com/kubeovn/kube-ovn/commit/8174aaf6c082c05993b9a460d55a5e167b430206) when dad state not ready should return err (#5129)
 * [4d62df4e8](https://github.com/kubeovn/kube-ovn/commit/4d62df4e8336c31cde1f8ea6af6a4d5c15944a0d) prepare for next release

### Contributors

 * Karol Szwaj
 * Mengxin Liu
 * andrewlee1089
 * changluyi
 * 张祖建

## v1.13.8 (2025-04-06)

 * [caed342ae](https://github.com/kubeovn/kube-ovn/commit/caed342aeb255c0d9dda9557efd84e14b4fd2823) release v1.13.8
 * [a5ee66061](https://github.com/kubeovn/kube-ovn/commit/a5ee66061a612d47455d7c9d87ee867ea51aedf4) Fix: IPv6 Link-Local Address Used as Source IP During Pod Initialization (#5124)
 * [d06112fe4](https://github.com/kubeovn/kube-ovn/commit/d06112fe485e3e98807a432a7339397606967a8b) prepare for next release

### Contributors

 * Mengxin Liu
 * changluyi

## v1.13.7 (2025-04-04)

 * [4ee4e2e84](https://github.com/kubeovn/kube-ovn/commit/4ee4e2e8473b95fef7c674f576be2e962825bea6) release v1.13.7
 * [1af3b7396](https://github.com/kubeovn/kube-ovn/commit/1af3b7396ce56932732d296f6491222119a8147b) gc: consider whether the sts pod is alive during lsp gc (#5122)
 * [feea276fa](https://github.com/kubeovn/kube-ovn/commit/feea276fa31229fde4bc468fea8901654d177a7b) prepare for next release

### Contributors

 * Mengxin Liu
 * 张祖建

## v1.13.6 (2025-04-01)

 * [fb521c708](https://github.com/kubeovn/kube-ovn/commit/fb521c7081fb109c9808074e38faee22e225391b) release v1.13.6
 * [0480e8db9](https://github.com/kubeovn/kube-ovn/commit/0480e8db9b2c579f0f280b10f878d15969a25488) base: update ovs patches (#5111)
 * [3ec1a4c93](https://github.com/kubeovn/kube-ovn/commit/3ec1a4c9320717c236c06b406a06162beb926d4e) feat(controller): skip appending VM LSPs if default Multus network is present (#5106)
 * [62bb1e5aa](https://github.com/kubeovn/kube-ovn/commit/62bb1e5aa3d079bb709f6a4df559f4d8a0b518b7) fix: egress network policy not work, when no pod hit matchlabel (#5088)
 * [caedd0027](https://github.com/kubeovn/kube-ovn/commit/caedd0027c80eea887e05743fe5e8e7a3bb5d3c8) prepare for next release

### Contributors

 * Mengxin Liu
 * changluyi
 * 张祖建

## v1.13.5 (2025-03-19)

 * [d3f2e8fd5](https://github.com/kubeovn/kube-ovn/commit/d3f2e8fd5bce24139bd228951479d165edb1662b) release v1.13.5
 * [c8275136f](https://github.com/kubeovn/kube-ovn/commit/c8275136f77a833ba8c2275edd25cb0aff243c05) build(deps): bump github.com/containerd/containerd from 1.7.26 to 1.7.27 (#5085)
 * [17d80d58c](https://github.com/kubeovn/kube-ovn/commit/17d80d58c4c4052f7eab8b7a20a68f1910d33690) build(deps): bump aquasecurity/trivy-action from 0.29.0 to 0.30.0 (#5081)
 * [c248c0da2](https://github.com/kubeovn/kube-ovn/commit/c248c0da272ac18683bc2d556419b417bdbf514f) bind to pod ips when env variable ENABLE_BIND_LOCAL_IP is set to true (#5049)
 * [217788c8f](https://github.com/kubeovn/kube-ovn/commit/217788c8f1a97c2921c1b3023bea91874cd24146) prepare for next release

### Contributors

 * Mengxin Liu
 * dependabot[bot]
 * 张祖建

## v1.13.4 (2025-03-13)

 * [0284f481a](https://github.com/kubeovn/kube-ovn/commit/0284f481a32e6952dd4aa334f65a0ee9548d711e) release v1.13.4
 * [959c0b6c7](https://github.com/kubeovn/kube-ovn/commit/959c0b6c71404a6c4e9789b63a2244ab9c0f6cdc) bump go to 1.23.7 (#5077)
 * [3e7b7849c](https://github.com/kubeovn/kube-ovn/commit/3e7b7849cf6ccd806871b24fb64efef74ec35c0f) feat: Enhance finalizer handling for VPC and subnet
 * [3105f7882](https://github.com/kubeovn/kube-ovn/commit/3105f788277de2e4eb3b464f7b44980f694c92e8) feat: Make Kube-OVN namespace configurable with default value (#5069)
 * [79b70fd47](https://github.com/kubeovn/kube-ovn/commit/79b70fd4729678ea39bd20ceeefe0f9e4e87d93a) Fix #5028: Orphaned subnets which reference a non-existent VPC cause new namespaces to never get correct annotations. (#5031)
 * [a5a76ac80](https://github.com/kubeovn/kube-ovn/commit/a5a76ac80d1dfcc05b4521cceb258c7b87b10577) remove genev_sys_6081 when uninstall kube-ovn (#5066)
 * [28e2290b6](https://github.com/kubeovn/kube-ovn/commit/28e2290b6f489d86699e6cbdb3431e016b46fd46) simple vip lable update and then update subnet status (#5036)
 * [20e307940](https://github.com/kubeovn/kube-ovn/commit/20e307940d00170c9e9e650c12e267ea0ecf7fb1) kubectl-ko: fix conntrack state (#5038)
 * [de7a53101](https://github.com/kubeovn/kube-ovn/commit/de7a531012417e4c070bfdbca7bd8248870cd738) fix rerun the completed vim migration (#5020)
 * [24dae762d](https://github.com/kubeovn/kube-ovn/commit/24dae762d61d8e8d42370f342d13d71209fefb5b) feat(GC): Add check for GC disabled (#5005)
 * [1b04dbde9](https://github.com/kubeovn/kube-ovn/commit/1b04dbde9f584d86c3aa2e930f80d12456190071) ci: bump aquasecurity/trivy-action to 0.29.0
 * [1a6249be4](https://github.com/kubeovn/kube-ovn/commit/1a6249be4bf988734263a529a50a2e8616d016e9) prepare for next release

### Contributors

 * Kevin Carter
 * Mengxin Liu
 * andrewlee1089
 * changluyi
 * zbb88888
 * zhangzujian
 * 张祖建

## v1.13.3 (2025-02-17)

 * [4a10a7571](https://github.com/kubeovn/kube-ovn/commit/4a10a7571f8c0c44067b427e01c0e8da3174694b) release v1.13.3
 * [e18e91075](https://github.com/kubeovn/kube-ovn/commit/e18e91075c06346434d4412c4f9b757707671b0b) bump k8s to v1.31.6 (#4996)
 * [c56d8db7d](https://github.com/kubeovn/kube-ovn/commit/c56d8db7d4b4309c16462da5c0415176fa18ddc2) fix superfluous response.WriteHeader (#4980)
 * [79c9e473e](https://github.com/kubeovn/kube-ovn/commit/79c9e473e0c5ce4542e213ff17352912ede4d4dc) bump dependencies
 * [460602f50](https://github.com/kubeovn/kube-ovn/commit/460602f501d3c74132a1e9585882d8ef4c62317f) use httpGet as liveness/readiness probe method (#4945)
 * [b70b16cdf](https://github.com/kubeovn/kube-ovn/commit/b70b16cdfe5d76f624cd5a684113231af942d878) fix: kube-ovn-controller cannot be ready when ENABLE_METRICS is false (#4886)
 * [a6123dd74](https://github.com/kubeovn/kube-ovn/commit/a6123dd7421010caf7fe1e8f62aa0e2c26613148) controller: consider StatefulSet's start ordinal (#4967)
 * [dbfc8a5fa](https://github.com/kubeovn/kube-ovn/commit/dbfc8a5fa191fc8b7ba717afa1a721e708d6e280) bump go to 1.23.6 (#4963)
 * [3ec00b3f1](https://github.com/kubeovn/kube-ovn/commit/3ec00b3f10eab538c22e94ee4096505c4744095b) ci: build arm64 images on arm64 hosted runners (#4936)
 * [4a3f420d8](https://github.com/kubeovn/kube-ovn/commit/4a3f420d8ac37647301c2e543e77e883de0ef9be) fix log (#4928)
 * [bacc10f9e](https://github.com/kubeovn/kube-ovn/commit/bacc10f9e1d9c42ca045754160e4c64218cbac44) make sure gw pod exist before eip creation (#4924)
 * [1b9a4b906](https://github.com/kubeovn/kube-ovn/commit/1b9a4b906f23bb1f0694d2b81f8d601f1ccc586a) controller: check condition NodeNetworkUnavailable when determining whether node is ready (#4917)
 * [e90637aab](https://github.com/kubeovn/kube-ovn/commit/e90637aaba969d5dabb59129fdc61e0b78eca297) cni-server: set node NetworkUnavailable condition after join subnet gateway check (#4915)
 * [73c364a60](https://github.com/kubeovn/kube-ovn/commit/73c364a60945b1f1d9eba5af744c0fccbf44948f) base: bump cni plugins to v1.6.2 (#4904)
 * [331683b76](https://github.com/kubeovn/kube-ovn/commit/331683b76d5958360bd93aefda2f321890c4bb0d) ipam: check subnet's available ipv6 address count (#4903)
 * [81f72d099](https://github.com/kubeovn/kube-ovn/commit/81f72d099825f536042b8a3724230bb64ee89c67) remove enable-live-migration-optimize templately (#4892)
 * [5d787e262](https://github.com/kubeovn/kube-ovn/commit/5d787e262640088b7c4838149361e2e258a31eee) ipam: use ip provided by nad annotation when providing IPAM for other CNI plugins (#4883)
 * [7981d08c9](https://github.com/kubeovn/kube-ovn/commit/7981d08c927e85b4e5aed7d741abd3e3867f30fd) fix(helm): add get on crd for ovn-cr (#4816)
 * [610745cf5](https://github.com/kubeovn/kube-ovn/commit/610745cf59f5ab36d84e24a8f34c78789f524349) keep dockerfile variable the same as download-go-deps.sh (#4863)
 * [c72b725d6](https://github.com/kubeovn/kube-ovn/commit/c72b725d624a2beacc1c20b3dd378071a1715d78) fix go version
 * [2c4a7596b](https://github.com/kubeovn/kube-ovn/commit/2c4a7596b48be01157bf89f6ddc657cb64823451) fix security (#4879)
 * [c78590afc](https://github.com/kubeovn/kube-ovn/commit/c78590afc73646c30cdb3ed8853d56d63f4f06f0) auto detect kubevirt install (#4791) (#4876)
 * [1f518d9f2](https://github.com/kubeovn/kube-ovn/commit/1f518d9f2617d18e495697e2d8b0e72d2d8af41f) pod should use mac and ips provider by multus firstly (#4800) (#4875)
 * [e2ecef195](https://github.com/kubeovn/kube-ovn/commit/e2ecef195aa2a0e54dfc92a7f2020a95779a94c9) add kubevirt live migration optimize (#4773) (#4874)
 * [7bc30dbd6](https://github.com/kubeovn/kube-ovn/commit/7bc30dbd61c9c39d2cfb403533d58c91eeafb496) fix(controller/subnet): controller crashes on subnets if gateway is unspecified and netpol are disabled (#4848)
 * [2cc515e72](https://github.com/kubeovn/kube-ovn/commit/2cc515e721ff3709a7ba74c0a01c426d3b628bcb) clean up legacy iptables rules only when iptables/ip6_tables is loaded (#4855)
 * [d53efc637](https://github.com/kubeovn/kube-ovn/commit/d53efc6377b151e97293f9004367d8d7c70df7d8) bump go to 1.23.4 (#4852)
 * [19683fa4f](https://github.com/kubeovn/kube-ovn/commit/19683fa4fb0b1c71885060b7a6b27629391f7bfb) fix gateway node check for centralized ecmp subnets (#4847)
 * [cdc639d31](https://github.com/kubeovn/kube-ovn/commit/cdc639d316633034ff2f8334c29b2103b36e75f6) use JSON merge patch to update labels/annotations (#4838)
 * [ae9763933](https://github.com/kubeovn/kube-ovn/commit/ae9763933cb415c3a1b71c72738a755822ce2afc) fix getting subnet cidr by protocol (#4844)
 * [ec216d216](https://github.com/kubeovn/kube-ovn/commit/ec216d21656f956a38defcfc66c6dc0dbf6248ca) ci: wait for kubevirt crd to be created before creating CR (#4839)
 * [85749c4f7](https://github.com/kubeovn/kube-ovn/commit/85749c4f7cd9b3141ec5dc779ac60b299e8f9bd2) build(deps): bump helm/kind-action from 1.10.0 to 1.11.0 (#4837)
 * [5335f1095](https://github.com/kubeovn/kube-ovn/commit/5335f1095d11bc74acde4f01640ec9ebe458b458) refactor: remove redundant policy route addition in node handling (#4835)
 * [c47150435](https://github.com/kubeovn/kube-ovn/commit/c47150435d062135330e481fccd0644736727096) prepare for next release

### Contributors

 * Congqi Zhao
 * Mengxin Liu
 * SKALA NETWORKS
 * bobz965
 * changluyi
 * zbb88888
 * zhangzujian
 * 张祖建

## v1.13.2 (2024-12-16)

 * [229b0b74e](https://github.com/kubeovn/kube-ovn/commit/229b0b74e0039c34c2e61942ce3ad76bd2b00e37) release v1.13.2
 * [221b1a3c7](https://github.com/kubeovn/kube-ovn/commit/221b1a3c7a817f3a184aebcf77b9ce8c7b4970e5) cni: do not exit if the sysctl variable does not exist or can not be set (#4828)
 * [e4654105e](https://github.com/kubeovn/kube-ovn/commit/e4654105efad75d4ea03932626cd3f42cf45700a) skip node local dns ip conntrack when set acl (#4824)
 * [fe75a8497](https://github.com/kubeovn/kube-ovn/commit/fe75a849730904cedb59d12cce9f179b146aab3d) prepare for next release

### Contributors

 * changluyi
 * 张祖建

## v1.13.1 (2024-12-11)

 * [75dc01ae1](https://github.com/kubeovn/kube-ovn/commit/75dc01ae133d9eb92d0101aed50a7eb72b31d114) release v1.13.1
 * [a0f5d4903](https://github.com/kubeovn/kube-ovn/commit/a0f5d4903c0a5e7f3f56f1afee3383d5630768c1) build(deps): bump github.com/docker/docker (#4812)
 * [67f5a1ebc](https://github.com/kubeovn/kube-ovn/commit/67f5a1ebc96b4e9422299b42995b126be5ffe36e) fix issue 4803: The two names should have a containment relationship (#4807)
 * [ad46a78d9](https://github.com/kubeovn/kube-ovn/commit/ad46a78d908d3fbce5a1c134c6b93539983f37e8) build(deps): bump gopkg.in/k8snetworkplumbingwg/multus-cni.v4 (#4806)
 * [cfecb7a2d](https://github.com/kubeovn/kube-ovn/commit/cfecb7a2d17df4464d793c4904e5a4090014c070) base: fix underlay network break during upgrade from v1.12 (#4797)
 * [3620365b6](https://github.com/kubeovn/kube-ovn/commit/3620365b637c557169db76dbb54bca61e9eb218f) build(deps): bump k8s from 1.31.2 to 1.31.3 (#4754)
 * [7488e0b97](https://github.com/kubeovn/kube-ovn/commit/7488e0b97f3740d7e427fb2f1260325a57c645d1) remove e2e test cases (#4745)
 * [cfb7186bc](https://github.com/kubeovn/kube-ovn/commit/cfb7186bcf87970e4c362fff384856037eaa633f) update release script (#4749)
 * [6139c51c9](https://github.com/kubeovn/kube-ovn/commit/6139c51c915f0801fb10153ce81e920a4141c0a7) add not found err check for lb-svc (#4748)
 * [c8dd1aeb6](https://github.com/kubeovn/kube-ovn/commit/c8dd1aeb60745e3af76c6a178f6515e04e3df078) prepare for next release

### Contributors

 * QEDQCD
 * dependabot[bot]
 * hzma
 * 张祖建

## v1.13.0 (2024-11-18)

 * [ae4ce3770](https://github.com/kubeovn/kube-ovn/commit/ae4ce3770c71de6c2c9ba456d11bf30b651f83ab) add loop check for tunnel nic (#4736)
 * [1bf233b3c](https://github.com/kubeovn/kube-ovn/commit/1bf233b3c1ab9cb1eddae1be3ba1230000124ac7) fix vm ip lost and show more log about ip delete (#4737)
 * [b92366b0b](https://github.com/kubeovn/kube-ovn/commit/b92366b0b4d92cd5583ae7076dd95bec95a667f4) add adapt with nodes and networks for anp/banp egress rules (#4731)
 * [5fe31c998](https://github.com/kubeovn/kube-ovn/commit/5fe31c998cacee0f598177093887f208023a3faa) ut: fix duplicate lsp name (#4734)
 * [11933d11c](https://github.com/kubeovn/kube-ovn/commit/11933d11c750cf89db4d6e017447eb7cfe820590) build(deps): bump google.golang.org/protobuf from 1.35.1 to 1.35.2 (#4733)
 * [a9e73db72](https://github.com/kubeovn/kube-ovn/commit/a9e73db72a0ebdb88c1cdeb0e9dac23261abd5cd) kube-ovn-cni will panic if cidr is invalid (#4729)
 * [e9a6c3697](https://github.com/kubeovn/kube-ovn/commit/e9a6c3697fb70daadd25a33655f5799185ef8617) build(deps): bump kubevirt.io/api from 1.3.1 to 1.4.0 (#4732)
 * [393db6ab1](https://github.com/kubeovn/kube-ovn/commit/393db6ab15be4d3ddd39219526c6f2029da85bdb) lb-svc is compatible with dual stack subnets (#4724)
 * [ccb85730f](https://github.com/kubeovn/kube-ovn/commit/ccb85730fdd0d1239c42d9855a40b11c73b56269) [bugfix] Optimize gc method at  port group and node (#4722)
 * [92a0d954d](https://github.com/kubeovn/kube-ovn/commit/92a0d954d6633fdc6657392585e6bec2a3eda511) The eip is not cleaned after eip is deleted (#4718) (#4719)
 * [e806b5336](https://github.com/kubeovn/kube-ovn/commit/e806b5336b54ca594471b520e7b16904e78473a7) base: add ovs-sandbox (#4712)
 * [427ea72db](https://github.com/kubeovn/kube-ovn/commit/427ea72db65b7261a3d3ea2300325ef2f6742017) build(deps): bump kernel.org/pub/linux/libs/security/libcap/cap (#4725)
 * [f50da5390](https://github.com/kubeovn/kube-ovn/commit/f50da53908ee61a3b205f17420a1ebd629e10839) ci: bump golangci-lint to v1.62.0 (#4723)
 * [5c54e01c4](https://github.com/kubeovn/kube-ovn/commit/5c54e01c447ce63d1643c7f8e360b40244a1cde4) base: update ovn patches (#4721)
 * [2edbf3fe0](https://github.com/kubeovn/kube-ovn/commit/2edbf3fe0a6050c0f69daf7f67bb46ca54ba7864) ci: add connectivity e2e test (#4658)
 * [505495756](https://github.com/kubeovn/kube-ovn/commit/5054957568ad442279666e0cebca47427a9c9de3) ci: bump kind to v0.25.0 (#4720)
 * [9e55f4ae9](https://github.com/kubeovn/kube-ovn/commit/9e55f4ae9faab6ca4bc9797ce679d2ee0529b1c5) bump go to 1.23.3 (#4716)
 * [ad7138bce](https://github.com/kubeovn/kube-ovn/commit/ad7138bcee1a5b6f4cdf9dd056d7fc734bb9bb70) use metav1.LabelSelectorAsSelector() to convert label selector (#4709)
 * [9c4a239e9](https://github.com/kubeovn/kube-ovn/commit/9c4a239e97701349cb04891a264d6434895b5dd3) minor fixes in vpc pod probe e2e test (#4682)
 * [d60e328ed](https://github.com/kubeovn/kube-ovn/commit/d60e328edb0c9b4aeb227911e6a47cc35e95297d) Fix ovn nat e2e (#4715)
 * [f14836aac](https://github.com/kubeovn/kube-ovn/commit/f14836aacbbe7e1f353d5a0e648ac8ac0d95d4f7) base: bump gobgp to v3.31.0 (#4708)
 * [262961170](https://github.com/kubeovn/kube-ovn/commit/2629611708a0dda0a9c8ecdf21e125e8bea1a4d3) add ut in pkg/ovs 24.11.7 (#4713)
 * [aaa67e613](https://github.com/kubeovn/kube-ovn/commit/aaa67e613aea0976b0f080ed7fdcedd50d1f6c1f) ut: add unit test for pkg/pvs (#4711)
 * [e2f5c69dc](https://github.com/kubeovn/kube-ovn/commit/e2f5c69dc1046b9343366fe0621e6e75da6a5fcd) ut: fix duplicate LSP name (#4710)
 * [d5e00c246](https://github.com/kubeovn/kube-ovn/commit/d5e00c246361955048f3ad3ba4b0f1ed392d7279) add ut for ovn-nb-logical_router_policy.go (#4706)
 * [d81bb1365](https://github.com/kubeovn/kube-ovn/commit/d81bb13654c743a8aa3630df4c5a572314da7db7) feat(natgw): extended nexthop support to advertise ipv4 NRLI over ipv6 sessions (#4704)
 * [1dc93da82](https://github.com/kubeovn/kube-ovn/commit/1dc93da8270b5ac8950054c738d4464b74647736) ci: re-add ovn vpc nat gw e2e to push needs (#4703)
 * [74b5c495a](https://github.com/kubeovn/kube-ovn/commit/74b5c495ada57fd1fa1a2c6cf9d1a0a01f9ecf60) ut: add unit test (#4702)
 * [5734a2765](https://github.com/kubeovn/kube-ovn/commit/5734a2765da80125fbab5055876665e1c2e75cb3) Make NAT gateway generation prefix a variable (#4557)
 * [042bdff2b](https://github.com/kubeovn/kube-ovn/commit/042bdff2ba073e475834024549825bcf8ea11d46) build(deps): bump github.com/osrg/gobgp/v3 from 3.30.0 to 3.31.0 (#4700)
 * [63d715864](https://github.com/kubeovn/kube-ovn/commit/63d71586414381e4b62627b9bcadce5f4c84e409) ut: fix use of closed network connection (#4698)
 * [d71f0a2f5](https://github.com/kubeovn/kube-ovn/commit/d71f0a2f52cdd3a356767805bd2dbe4c25acacc0) fix concurrent issue in ovn-nb_test.go (#4697)
 * [5db327096](https://github.com/kubeovn/kube-ovn/commit/5db327096035a7c5bc335e2bc928d1ea326366bb) add ut for ovn-nb-logical_router_port_test.go (#4696)
 * [6842df3c9](https://github.com/kubeovn/kube-ovn/commit/6842df3c940832f68be421c924de94d6b9e86408) add process for lb-svc ports update (#4660)
 * [451a463f4](https://github.com/kubeovn/kube-ovn/commit/451a463f4912b33e617ff074962e074b3a56fa8f) add WaitUntil check for ns (#4695)
 * [e4d1d3836](https://github.com/kubeovn/kube-ovn/commit/e4d1d3836b14ee45e50efb5319f4b79f5a3667fa) add ut for ovn-nb-logical_router_policy (#4693)
 * [1f0eb6f93](https://github.com/kubeovn/kube-ovn/commit/1f0eb6f93f4dc4d6edb1968f56dfa79faa270b88) ut: add unit test for ovn-nb-logical_router.go (#4689)
 * [b057e4ce6](https://github.com/kubeovn/kube-ovn/commit/b057e4ce6d3d29cb71c11656f51da29380ad0261) build(deps): bump github.com/onsi/gomega from 1.35.0 to 1.35.1 (#4688)
 * [63492716d](https://github.com/kubeovn/kube-ovn/commit/63492716d834a0658a2c0e6f0dbc806c318c7f6b) add fail nb client ut case in ovn-nb-logical_switch_port_test.go (#4685)
 * [f7f7fa877](https://github.com/kubeovn/kube-ovn/commit/f7f7fa877ff6fa6b6596894836915cf76fa9a963) base: compile OpenBFDD locally (#4683)
 * [ef4bb5a63](https://github.com/kubeovn/kube-ovn/commit/ef4bb5a631413377edbdadb7bf09c09cee4476fa) fix: cache eip should not update (#4673)
 * [4c8a49b4c](https://github.com/kubeovn/kube-ovn/commit/4c8a49b4c1db433d87edaeeb7373528ceda9cc97) build(deps): bump github.com/onsi/gomega from 1.34.2 to 1.35.0 (#4678)
 * [d90ae7c94](https://github.com/kubeovn/kube-ovn/commit/d90ae7c94d8b0d982a4d53caa592cdcb90974b52) Revert "build(deps): bump github.com/Microsoft/hcsshim from 0.12.7 to 0.12.9 …" (#4681)
 * [768880820](https://github.com/kubeovn/kube-ovn/commit/768880820e7c135756e34bde81d765e1f7ed329c) build(deps): bump github.com/Microsoft/hcsshim from 0.12.7 to 0.12.9 (#4677)
 * [bb398bf2b](https://github.com/kubeovn/kube-ovn/commit/bb398bf2bdc9cd417ebf1d65ea534690abee5cb8) build(deps): bump github.com/onsi/ginkgo/v2 from 2.20.2 to 2.21.0 (#4679)
 * [45c54b6c5](https://github.com/kubeovn/kube-ovn/commit/45c54b6c5da803d04e38471dae7416a3f8bdb7a8) add ut for subnet.go and ovn-nb-acl.go (#4674)
 * [f966ecc30](https://github.com/kubeovn/kube-ovn/commit/f966ecc306ab2c13ce57ae8dd519d5fc18a0e39d) add e2e for subnet with namespaceSelector (#4665)
 * [3d92683af](https://github.com/kubeovn/kube-ovn/commit/3d92683af5de2587c222d156a437e8e3d8d9744c) ut: add ovs and  ovn nb and cidr check (#4671)
 * [535048dcc](https://github.com/kubeovn/kube-ovn/commit/535048dcc3a759bc6f268756503cf4ba283d9ccf) add unit for ovn-nb-logical_router_route.go (#4672)
 * [bc2eb2b80](https://github.com/kubeovn/kube-ovn/commit/bc2eb2b80f2acb0d1d23cab3d57174ab9dbd42ed) build(deps): bump kernel.org/pub/linux/libs/security/libcap/cap (#4667)
 * [406c3794b](https://github.com/kubeovn/kube-ovn/commit/406c3794b732edf8b67e32aeb6e2eb78d1cd0be2) add unit test for ovn-nb-nat.go (#4664)
 * [a6e5276e1](https://github.com/kubeovn/kube-ovn/commit/a6e5276e111c790f09ada33e38f504bc5575f672) ut: add ovn nat (#4650)
 * [44cd6e5c2](https://github.com/kubeovn/kube-ovn/commit/44cd6e5c2ff111e6f75ac4ace8df129e70a8e087) add unit test for net.go (#4659)
 * [8e9f9479e](https://github.com/kubeovn/kube-ovn/commit/8e9f9479e610be1c0df4b44189c732de4c5dc275) add ut for ovn-nb-logical_router_port (#4651)
 * [1ffd88dad](https://github.com/kubeovn/kube-ovn/commit/1ffd88dad4584a9736da61ddf182d78f735d6411) base: fix ovn patch for setting ether dst addr after dnat (#4662)
 * [ab2c21293](https://github.com/kubeovn/kube-ovn/commit/ab2c21293ac5972e4047a32589f2b157dd7b8cb8) build(deps): bump sigs.k8s.io/controller-runtime from 0.19.0 to 0.19.1 (#4661)
 * [eb51ad211](https://github.com/kubeovn/kube-ovn/commit/eb51ad2111ea4f1d8b2c7d65b99a29e50cd46c46) bump k8s to v1.31.2 (#4654)
 * [62ab30a7d](https://github.com/kubeovn/kube-ovn/commit/62ab30a7d416100e4004c565d34d983f16ff9bb2) makefile: remove target kind-install-debug (#4656)
 * [92f6800bf](https://github.com/kubeovn/kube-ovn/commit/92f6800bf07a533dbdb0d86b2fa2aa95fb82e4af) build(deps): bump k8s.io/kubernetes in the k8s-io group (#4653)
 * [5cfb6b4c4](https://github.com/kubeovn/kube-ovn/commit/5cfb6b4c46854fee5fb8ef11e43f589dce70022d) ut: add logical_switch and port (#4644)
 * [0706c41ae](https://github.com/kubeovn/kube-ovn/commit/0706c41aea938336ff8a0e2d3c25a153e83c7ace) ut: add unit for ovn-nb-load_balancer (#4646)
 * [7003bca4f](https://github.com/kubeovn/kube-ovn/commit/7003bca4fafbd4b82f0c4ade14294887dd8d21ea) ci: ignore ovn vpc-nat-gw conformance e2e test (#4649)
 * [a7a8a9235](https://github.com/kubeovn/kube-ovn/commit/a7a8a9235566398f37f1512fb9266a303c6168fe) fix: bfd should use multi arch (#4647)
 * [c8288e89b](https://github.com/kubeovn/kube-ovn/commit/c8288e89b0e6960c235e1093bfeea56f529e29be) fix sometimes restart kube-ovn-cni ,ipsec not start (#4641)
 * [a9cd9a1a8](https://github.com/kubeovn/kube-ovn/commit/a9cd9a1a807535a8625c7b0237437edd47040d3e) fix: udp bad checksum on VXLAN interface (#4639)
 * [c414e6b96](https://github.com/kubeovn/kube-ovn/commit/c414e6b967a3b985365f2e546b2afbd51d258f0c) add e2e test for accessing nodeport when ovs/ovn component is down (#4642)
 * [d16bb52da](https://github.com/kubeovn/kube-ovn/commit/d16bb52da711da83222d404ca538186a16070f2b) add ovs vsctl , gw chassis, lb healthcheck ut (#4611)
 * [2ac7e2c0f](https://github.com/kubeovn/kube-ovn/commit/2ac7e2c0f589793ea0530b13a28ca4ddc6678afc) ut: fail fast (#4630)
 * [d02cffc28](https://github.com/kubeovn/kube-ovn/commit/d02cffc287780840a8c80c7aeaf97b1a13cb5ab4) use ovn-appctl for ovn components (#4638)
 * [79c13434b](https://github.com/kubeovn/kube-ovn/commit/79c13434bbdd9e805bdeef3e15a381195bd7872a) fix 'invalid memory address or nil pointer dereference' for namespace (#4636)
 * [7665b2b68](https://github.com/kubeovn/kube-ovn/commit/7665b2b6867fec44f88ce290bc612d76741a80f4) chart: fix missing CRD properties (#4637)
 * [ad7c89059](https://github.com/kubeovn/kube-ovn/commit/ad7c8905952be59e563a85f100703b4188c274ec) fix: CreateGatewayACL creates duplicate ndACL (#4592)
 * [aa1e67958](https://github.com/kubeovn/kube-ovn/commit/aa1e679586100754f48f07c27abb43c487549948) Fix accidental logical switch name conflict in ovn-nb-logical_switch_port_test.go (#4635)
 * [03aacacff](https://github.com/kubeovn/kube-ovn/commit/03aacacff0d8265d52a8cb3f10b88b80c55d7bc0) Add ut for ovn-nb-logical_switch_port.go date 24.10.16pm (#4625)
 * [c52c8c0cc](https://github.com/kubeovn/kube-ovn/commit/c52c8c0cc0267a46bcbc7d8be37cbde549e094c6) bump go.uber.org/mock to v0.5.0 (#4632)
 * [e0e8083f1](https://github.com/kubeovn/kube-ovn/commit/e0e8083f160c626cd4f30beddc21bb1031def1a1) docs: updated CHANGELOG.md (#4634)
 * [c9afcafbe](https://github.com/kubeovn/kube-ovn/commit/c9afcafbe4ad5e433cbd5313cdc9e1a1ebf191be) docs: updated CHANGELOG.md (#4633)
 * [ff6cc124a](https://github.com/kubeovn/kube-ovn/commit/ff6cc124adb0a413dd3e716ba6dfd9b7aa9c73dd) Refactor network policy matching logic (#4626)
 * [18f6ac918](https://github.com/kubeovn/kube-ovn/commit/18f6ac918447d968a331592afc86a18d474e1f92) team device not set unmanage (#4629)
 * [642973dbb](https://github.com/kubeovn/kube-ovn/commit/642973dbb3ca9e9da0a866e997db5468ff0aeab3) fix: once ovn-encap-csum is set false,it can't be reset to true (#4623)
 * [1c19fc70b](https://github.com/kubeovn/kube-ovn/commit/1c19fc70bcb69d0da105dba8dabb684caaca570f) build(deps): bump aquasecurity/trivy-action from 0.27.0 to 0.28.0 (#4628)
 * [d1af97bdd](https://github.com/kubeovn/kube-ovn/commit/d1af97bddea7c75aa4216033aa9c0b68fc789bba) bump go to 1.23.2 (#4412)
 * [0dbe49e68](https://github.com/kubeovn/kube-ovn/commit/0dbe49e6830200592e6a1acd53e1163efdef8277) ovn-ic: do not restart ovn-controller (#4624)
 * [77abe0c04](https://github.com/kubeovn/kube-ovn/commit/77abe0c048866c398f6baaf49bed76f75cf0b1eb) fix: ut dial tcp longer (#4614)
 * [1f227958d](https://github.com/kubeovn/kube-ovn/commit/1f227958deba7239ddd71ac41de4fa9e7ea63c3d) fix permission issue in kube-ovn installation script and chart (#4613)
 * [ea80cf25f](https://github.com/kubeovn/kube-ovn/commit/ea80cf25ffb2bab78991dd911a04cc57b3a8ff95) docs: updated CHANGELOG.md (#4620)
 * [5df224b16](https://github.com/kubeovn/kube-ovn/commit/5df224b16fee9cca8b06e01c8367efd915b35305) docs: updated CHANGELOG.md (#4619)
 * [aa8dce0bf](https://github.com/kubeovn/kube-ovn/commit/aa8dce0bff383a0b7f93f7f3aa0202aaee0c4893) build(deps): bump github.com/prometheus/client_golang (#4617)
 * [f9a5a3b97](https://github.com/kubeovn/kube-ovn/commit/f9a5a3b974098b7500e6c249fff6ec523304ca13) build(deps): bump github.com/k8snetworkplumbingwg/network-attachment-definition-client (#4618)
 * [63df5fe7d](https://github.com/kubeovn/kube-ovn/commit/63df5fe7d62cf07101145ea760291ed93beb9561) fix: ut (#4609)
 * [5cb13da09](https://github.com/kubeovn/kube-ovn/commit/5cb13da0958ec58e54bf67222df519df0ec6b571) fix memory overflow, add mac_binding related options to router (#4603)
 * [a6f13a67e](https://github.com/kubeovn/kube-ovn/commit/a6f13a67e25bdd86ff52351a8c06bc80ec7511e8) libovsdb: improve performance for listing logical router policies (#4599)
 * [08d8a546f](https://github.com/kubeovn/kube-ovn/commit/08d8a546fea22758685106b65d375d7eeb58c0d5) Add unit test in pkg/ovs/ovn-nb-logical_switch_port.go (#4602)
 * [071eb8e72](https://github.com/kubeovn/kube-ovn/commit/071eb8e7299ee1e8590e56590b606528d2c5490f) fix: sb ut failed (#4606)
 * [27379a662](https://github.com/kubeovn/kube-ovn/commit/27379a6625deca02cec448b5e34440f2b08d7bfb) vsctl ut (#4601)
 * [e8a9db09d](https://github.com/kubeovn/kube-ovn/commit/e8a9db09d5be04bd98283df8e65789080b737d36) fix logging (#4600)
 * [1251e7827](https://github.com/kubeovn/kube-ovn/commit/1251e78275d4894a3c85678e94847cbc118932dd) fix: dns overwrites dhcp when enabling dhcp and setting dhcp options at the same time (#4597)
 * [42ebd7cdd](https://github.com/kubeovn/kube-ovn/commit/42ebd7cdd406f88c34c6da606a8272a6a958a8ec) build(deps): bump aquasecurity/trivy-action from 0.26.0 to 0.27.0 (#4598)
 * [e91a7426f](https://github.com/kubeovn/kube-ovn/commit/e91a7426f713a09d0f4c4624afa76373e3a7f830) add namespaceSelector field in subnet (#4520)
 * [a30d88d03](https://github.com/kubeovn/kube-ovn/commit/a30d88d0307edff1ae2c37a6f61a102cd6bf7e78) chart: fix value of env OVN_DB_IPS (#4595)
 * [a9ca636d1](https://github.com/kubeovn/kube-ovn/commit/a9ca636d135a2ee3e49180153e75b6bac7e1e362) cni: always enable ipv6 (#4435)
 * [2e169a3f0](https://github.com/kubeovn/kube-ovn/commit/2e169a3f0e1c97c0edccf1e2f0e12e74688d70cd) ut: add ovn ic (#4593)
 * [8a3f2ea5a](https://github.com/kubeovn/kube-ovn/commit/8a3f2ea5a90c9815e74c9aad1ecff5126d86695f) chart: add missing sa imagePullSecrets (#4594)
 * [39067cd07](https://github.com/kubeovn/kube-ovn/commit/39067cd0775c7f122e3d53f73e0c4af94df1f271) add: fail case ut (#4589)
 * [4f80f33ce](https://github.com/kubeovn/kube-ovn/commit/4f80f33ce2a6c5b2197b68134390c5876f3fbe47) build(deps): bump aquasecurity/trivy-action from 0.25.0 to 0.26.0 (#4590)
 * [73a7ec69a](https://github.com/kubeovn/kube-ovn/commit/73a7ec69ac9a57d8a836aac660478a29aaee0f5f) build(deps): bump google.golang.org/protobuf from 1.34.2 to 1.35.1 (#4588)
 * [4cd5b0e5c](https://github.com/kubeovn/kube-ovn/commit/4cd5b0e5ca56c156154ab2d9472043e0eba57344) build(deps): bump aquasecurity/trivy-action from 0.24.0 to 0.25.0 (#4587)
 * [bd766ce4a](https://github.com/kubeovn/kube-ovn/commit/bd766ce4a8864b720f5919ccea817162b7c027ff) bump go to 1.22.8 (#4584)
 * [004877f00](https://github.com/kubeovn/kube-ovn/commit/004877f00eee4ffa5e195d55cc261aeb9578f8b2) Fix error log not printed nearby in util package (#4574)
 * [1d19ec090](https://github.com/kubeovn/kube-ovn/commit/1d19ec0904b9ff28686024d0c0a54e12f0fbe703) Fix error log not printed nearby in ipam package (#4585)
 * [e00511de3](https://github.com/kubeovn/kube-ovn/commit/e00511de3bc843b1f0a91511f0a81ff53a55b794) gc for security group (#4559)
 * [b2830a5d5](https://github.com/kubeovn/kube-ovn/commit/b2830a5d55643945cf78046315dcd6548db3990f) fix: test_list_chassis_with_no_entries (#4586)
 * [a676e630f](https://github.com/kubeovn/kube-ovn/commit/a676e630fbcda65ff95d207c9c56164863e80c75) add values for OVSDB_CON_TIMEOUT and OVSDB_INACTIVITY_TIMEOUT (#4568)
 * [edd2ccf9a](https://github.com/kubeovn/kube-ovn/commit/edd2ccf9a2d7cd6f72218cc91c6cd25061e9c125) Add genev_sys_6081 and vxlan_sys_4789 to cilium devices (#4575)
 * [4d5cdc7cd](https://github.com/kubeovn/kube-ovn/commit/4d5cdc7cd86720b654f1e55bad3e0f918c97fce2) fix slice init length (#4579)
 * [03e5c2d1a](https://github.com/kubeovn/kube-ovn/commit/03e5c2d1a95c9dbc816a0a1ba453ba3f6f52e3c1) ci: bump cilium, multus-cni, cert-manager and submariner (#4572)
 * [6f3d36bc0](https://github.com/kubeovn/kube-ovn/commit/6f3d36bc0d10dbfe1c8ac8fc3b349ccc2e323df5) ci: set trivy db repository to public.ecr.aws/aquasecurity/trivy-db:2 (#4570)
 * [38298e458](https://github.com/kubeovn/kube-ovn/commit/38298e45897d1f59282f7968a93e2e274e2b62a7) build(deps): bump github.com/osrg/gobgp/v3 from 3.29.0 to 3.30.0 (#4576)
 * [57cf0fdec](https://github.com/kubeovn/kube-ovn/commit/57cf0fdecff41ba4a3ab842aa0d13687e6580682) build(deps): bump golang.org/x/sys from 0.25.0 to 0.26.0 (#4581)
 * [1865f8dd6](https://github.com/kubeovn/kube-ovn/commit/1865f8dd61703db6c94f1915389d750a2c0e8404) build(deps): bump golang.org/x/time from 0.6.0 to 0.7.0 (#4580)
 * [18f4dbace](https://github.com/kubeovn/kube-ovn/commit/18f4dbace97779dccb3ec844700b6f5d715f85e7) build(deps): bump gopkg.in/k8snetworkplumbingwg/multus-cni.v4 (#4583)
 * [5d5480edf](https://github.com/kubeovn/kube-ovn/commit/5d5480edf2d2942b061a924518b82c2a9240be6e) build(deps): bump google.golang.org/grpc from 1.67.0 to 1.67.1 (#4577)
 * [519370471](https://github.com/kubeovn/kube-ovn/commit/519370471e9ca44184d65240e1130fbf32f374d4) Check network mask when init subnet (#4494)
 * [e90a58a6b](https://github.com/kubeovn/kube-ovn/commit/e90a58a6bc7071e4ee1626ad44cf9aef92adac0a) Fix error log not printed nearby in ovs package (#4562)
 * [3d12fa70d](https://github.com/kubeovn/kube-ovn/commit/3d12fa70da3b45ee4c741f4ebeda0e77c501338b) build(deps): bump github.com/Microsoft/hcsshim from 0.12.6 to 0.12.7 (#4561)
 * [62b80ca62](https://github.com/kubeovn/kube-ovn/commit/62b80ca62e373fe3b10ac9247868602f94fe3ad1) docs: updated CHANGELOG.md (#4571)
 * [49cba10a7](https://github.com/kubeovn/kube-ovn/commit/49cba10a77c18d9d67abcd1505a649ba0ceebdf8) ut: ovn sb suite (#4549)
 * [84203a086](https://github.com/kubeovn/kube-ovn/commit/84203a0861d59204288413fc3655acf5846f72c5) fix ovn0 add ipv6 local-link address (#4547)
 * [856dc10a1](https://github.com/kubeovn/kube-ovn/commit/856dc10a14ba8dc0219b98e0aa7c999614dd0175) build(deps): bump github.com/docker/docker (#4556)
 * [2f4a6c247](https://github.com/kubeovn/kube-ovn/commit/2f4a6c247f590327a511ef01e726c33d0e0454b2) docs: updated CHANGELOG.md (#4555)
 * [d38fef188](https://github.com/kubeovn/kube-ovn/commit/d38fef1881d04ee6af7db82b9d880a5ffb4663bd) fix kube-ovn-cni capabilities (#4550)
 * [514bc77de](https://github.com/kubeovn/kube-ovn/commit/514bc77de78fc09166f3ce89c3a68e10e9f385e1) docs: updated CHANGELOG.md (#4548)
 * [5a70de9c7](https://github.com/kubeovn/kube-ovn/commit/5a70de9c7c24dceefd45b8548047f0b8eccf2dad) allow user to set vxlan_sys_4789 tx off (#4543)
 * [2baa5bda7](https://github.com/kubeovn/kube-ovn/commit/2baa5bda791e2a9e0fddd5687e9b69f090b4fb97) add utils ut (#4544)
 * [73ce0ec8e](https://github.com/kubeovn/kube-ovn/commit/73ce0ec8ed20128319d182af9c3bd9d64849f814) build(deps): bump github.com/docker/docker (#4541)
 * [d38914a49](https://github.com/kubeovn/kube-ovn/commit/d38914a49f9dd883caadec78e445fa3d938beaa8) build(deps): bump google.golang.org/grpc from 1.66.2 to 1.67.0 (#4542)
 * [996da8152](https://github.com/kubeovn/kube-ovn/commit/996da8152ce9b97cc117e3213bbb2abc24d11d47) add arp ut (#4535)
 * [b90491266](https://github.com/kubeovn/kube-ovn/commit/b90491266d8e5df4296839f0088ccf09978f7eec) base: rebuild go binary deps from source (#4524)
 * [8ba16af2d](https://github.com/kubeovn/kube-ovn/commit/8ba16af2d3172ec7c266f27e0e6cc05cd2328814) fix speaker metrics (#4523)
 * [412f79904](https://github.com/kubeovn/kube-ovn/commit/412f79904de9d666aa55995f89bf6e99f88b0d89) [fix]: delete the NIC regardless of whether the Pod was found or not. (#4500)
 * [2fa78ab8d](https://github.com/kubeovn/kube-ovn/commit/2fa78ab8df74159277c7add9d48013c2ad9cfc11) add ut for ip.go (#4536)
 * [b406ce91d](https://github.com/kubeovn/kube-ovn/commit/b406ce91dd48eba075bc8e337df47482d7f94463) docs: updated CHANGELOG.md (#4534)
 * [94749490a](https://github.com/kubeovn/kube-ovn/commit/94749490a7ee2f3e7a56ad1eeda8478b4ad7256f) bump multus to v4.1.1 (#4532)
 * [5769b072b](https://github.com/kubeovn/kube-ovn/commit/5769b072b24b3da88619a4470c45a07a3f6ad6ac) check whether subnet exists before deleting vpc (#4533)
 * [a2b22ec2b](https://github.com/kubeovn/kube-ovn/commit/a2b22ec2b6277c1a40b6f6e63ba30f15dfb750ad) fix bugs and enhance unit test coverage for all functions in pkg/util/validator.go (#4505)
 * [640f6835e](https://github.com/kubeovn/kube-ovn/commit/640f6835e9f52f4ac8232e353a36c8a16369f503) build(deps): bump github.com/prometheus/client_golang (#4531)
 * [7f3c719ca](https://github.com/kubeovn/kube-ovn/commit/7f3c719cada9877655cbf2d10214594747317ff6) ut:add ut for func DialTCP and DialAPIServer in apk/util/k8s.go (#4515)
 * [c06857477](https://github.com/kubeovn/kube-ovn/commit/c06857477180a095d5ea1044808d9be07f040eb5) build(deps): bump gopkg.in/k8snetworkplumbingwg/multus-cni.v4 (#4529)
 * [217e4860b](https://github.com/kubeovn/kube-ovn/commit/217e4860bf1bc444e78e63f75a439e1660a0d8b7) add ut (#4525)
 * [4b06bd07b](https://github.com/kubeovn/kube-ovn/commit/4b06bd07b2971f7deff8bed7465bf4bb28197f33) add pod patch and exec ut (#4521)
 * [e2697fc91](https://github.com/kubeovn/kube-ovn/commit/e2697fc91a8a06cd240abca97ddc0e397a0d101a) Utils net ut (#4514)
 * [56f786977](https://github.com/kubeovn/kube-ovn/commit/56f786977f710c7c02780a26c2be4c8c4526b05f) add ginkgo ut coverage (#4510)
 * [a3c6676ab](https://github.com/kubeovn/kube-ovn/commit/a3c6676ab8e13f13ec55e9d9ad34f65c95fa69bd) fix mistake to announce ipv4 (#4497)
 * [544f2addc](https://github.com/kubeovn/kube-ovn/commit/544f2addcbcfeadc5e74ea7e5162f0d1097c260e) ut: add unit for dhcp_options logical_switch and ovn-nb_global (#4508)
 * [d032f05dd](https://github.com/kubeovn/kube-ovn/commit/d032f05dd3331c77e7862665163174c6301bf548) build(deps): bump github.com/k8snetworkplumbingwg/network-attachment-definition-client from 1.7.2 to 1.7.3 (#4522)
 * [f95e06c9a](https://github.com/kubeovn/kube-ovn/commit/f95e06c9a6d4f1640ee9c1c6603554812c090a5d) fix: upgrade submodule ginkgo (#4509)
 * [f3061ef67](https://github.com/kubeovn/kube-ovn/commit/f3061ef67cb9a3786852f20237d69909b9cfef51) fix: mac arm64 run netlink ut (#4511)
 * [bf75a65ee](https://github.com/kubeovn/kube-ovn/commit/bf75a65eedc5aa75eecaa679a2f85bafee91d6e0) bump k8s to v1.31.1 (#4512)
 * [14484fd35](https://github.com/kubeovn/kube-ovn/commit/14484fd35dfd74098dbe5750a2745e701f0ced48) build(deps): bump google.golang.org/grpc from 1.66.1 to 1.66.2 (#4517)
 * [6bf158781](https://github.com/kubeovn/kube-ovn/commit/6bf1587816e4f862075c9a451a5a9f79c52ff8fb) build(deps): bump github.com/k8snetworkplumbingwg/network-attachment-definition-client (#4518)
 * [2b24b2b4c](https://github.com/kubeovn/kube-ovn/commit/2b24b2b4c6b642fab05da3a53dad904005ffad7f) fix mcast e2e error (#4503)
 * [979513112](https://github.com/kubeovn/kube-ovn/commit/979513112c86b3c1f1f97ffc34f45da2b0bde8c4) fix: err log (#4507)
 * [486cde80a](https://github.com/kubeovn/kube-ovn/commit/486cde80a7357fb95de078cc556156c0359bdfbb) make: add ut cover (#4490)
 * [3e843e81d](https://github.com/kubeovn/kube-ovn/commit/3e843e81dcb60d8da15569934f9e4dcc7f54978d) build(deps): bump github.com/containerd/containerd from 1.7.21 to 1.7.22 (#4501)
 * [f16258302](https://github.com/kubeovn/kube-ovn/commit/f16258302203b32e0fadac462a7fd2fca12e218b) build(deps): bump google.golang.org/grpc from 1.66.0 to 1.66.1 (#4502)
 * [1cf7e5086](https://github.com/kubeovn/kube-ovn/commit/1cf7e50866230091fe158d9e8ac9b17185a2e67f) fix: gobgp CVE (#4498)
 * [dbcbc056a](https://github.com/kubeovn/kube-ovn/commit/dbcbc056a17854dc877db7c7917fb6f245563211) add mcast querier ip for multicast (#4375)
 * [b0bb154f4](https://github.com/kubeovn/kube-ovn/commit/b0bb154f438ac5b8686ac0cb34114f52e1d6360a) bump golangci-lint to v1.61.0 (#4496)
 * [0542c92ad](https://github.com/kubeovn/kube-ovn/commit/0542c92addedfd33420ec531cca0c92503e437bf) build(deps): bump github.com/docker/docker (#4495)
 * [3653e74dc](https://github.com/kubeovn/kube-ovn/commit/3653e74dc69338eea23287556279d7c9eb34358e) fix: support ptp networks with ipv4 /31 netmask and ipv6 /127 netmask  (#4425)
 * [5391b8015](https://github.com/kubeovn/kube-ovn/commit/5391b80157444e73cdecd12a3694595303326c5f) tproxy: support named port (#4487)
 * [2d1152262](https://github.com/kubeovn/kube-ovn/commit/2d11522625639c4a526865e30d0b99e83ef24932) fix: ipam ut name conflicts (#4489)
 * [baaaed0b5](https://github.com/kubeovn/kube-ovn/commit/baaaed0b570c12df90561d4e9aaf04f324e93412) add gobgp client cmd (#4460)
 * [70c7b979c](https://github.com/kubeovn/kube-ovn/commit/70c7b979c7a07776fe1eaafda3a6d8b2b898b3ee) ut: add unit test for bfd acl and address_set (#4461)
 * [827c50f23](https://github.com/kubeovn/kube-ovn/commit/827c50f23d95c0d3cfabd652da6cbacf06c25230) Add ipam (#4451)
 * [2214d2702](https://github.com/kubeovn/kube-ovn/commit/2214d270239ac63e1a1d5e343df4e4f7346b7fdc) Add ipam ut (#4455)
 * [6d4011f9a](https://github.com/kubeovn/kube-ovn/commit/6d4011f9ad1e5a7ba85f5c04bd80d6cbcdb3460a) bump go to 1.22.7 (#4482)
 * [d76bf6db6](https://github.com/kubeovn/kube-ovn/commit/d76bf6db68cc609a5cb3c6e8e4eeb3cebf2a0333) fix 解决部署遇到的不成功问题 (#4484)
 * [b9c3c2137](https://github.com/kubeovn/kube-ovn/commit/b9c3c21378356cc7c714dcf7c63dcde59549d74a) fix: arping reply may duplicate (#4477)
 * [fd5ce3169](https://github.com/kubeovn/kube-ovn/commit/fd5ce316947114cb963b804d3f45a20387c02eec) ci: bump cilium to v1.16.1 (#4337)
 * [ff2300bfb](https://github.com/kubeovn/kube-ovn/commit/ff2300bfbd340666f43d4f06f77212811fa363e8) fix bug: dpdk场景下添加删除ProviderNetwork失败 (#4466)
 * [1104627f4](https://github.com/kubeovn/kube-ovn/commit/1104627f44386f7959f56c17ea3059002771ab09) build(deps): bump github.com/prometheus/client_golang (#4480)
 * [1b46c42a1](https://github.com/kubeovn/kube-ovn/commit/1b46c42a13c719f5ba6e11c54ce6f706494c6438) build(deps): bump golang.org/x/mod from 0.20.0 to 0.21.0 (#4481)
 * [33272f110](https://github.com/kubeovn/kube-ovn/commit/33272f110457a7fc2407cc63036fc96d2a5ffccc) fix: kubectl-ko using kube-ovn-cni pod for nsenter (#4478)
 * [b3075e316](https://github.com/kubeovn/kube-ovn/commit/b3075e316319a9ebff3816bd2d0fbdd6dfdae852) remove incorrect error logging (#4473)
 * [c55e97aef](https://github.com/kubeovn/kube-ovn/commit/c55e97aefb2651a9065e16c1f636e1281585d2f5) ci: bump golangci-lint to v1.60.3 (#4474)
 * [4e5cc7bb9](https://github.com/kubeovn/kube-ovn/commit/4e5cc7bb94fb37d1313ea80fa7e45f016ac60572) build(deps): bump golang.org/x/sys from 0.24.0 to 0.25.0 (#4476)
 * [e99013eca](https://github.com/kubeovn/kube-ovn/commit/e99013ecaf6af0344ad6aa95b72f51d3e2bbf0e4) add anp/banp e2e case (#4347)
 * [be408f895](https://github.com/kubeovn/kube-ovn/commit/be408f895deb3e5e0a453423380f912abb189d89) when using vf, set pod_nic_type to sriov (#4463)
 * [31311598b](https://github.com/kubeovn/kube-ovn/commit/31311598b6a3bb3c99771b48f8e81acf110a779a) fix: create and parse coredns template (#4445)
 * [938ddada8](https://github.com/kubeovn/kube-ovn/commit/938ddada8e14a2fb55a8c1ed01b1b5269e945b0e) kubectl-ko trace using ovs-ovn instead of kube-ovn-cni (#4471)
 * [2cfbc91d8](https://github.com/kubeovn/kube-ovn/commit/2cfbc91d8716eea11850caf3e09b821e3ca62b81) build(deps): bump github.com/opencontainers/runc from 1.1.13 to 1.1.14 (#4469)
 * [286c34419](https://github.com/kubeovn/kube-ovn/commit/286c3441998fd65bb7d77a19cb7d6d786d51c249) build(deps): bump peter-evans/create-pull-request from 6 to 7 (#4467)
 * [ee6e590d0](https://github.com/kubeovn/kube-ovn/commit/ee6e590d0b34c7c8ba6eeb6cd3416d6f98fbb8f4) build(deps): bump github.com/onsi/gomega from 1.34.1 to 1.34.2 (#4458)
 * [50016edf3](https://github.com/kubeovn/kube-ovn/commit/50016edf3b34874532973e5dcb9e588f3c20b44f) build(deps): bump github.com/onsi/ginkgo/v2 from 2.20.1 to 2.20.2 (#4459)
 * [6ecb05e5a](https://github.com/kubeovn/kube-ovn/commit/6ecb05e5a94a826b75c1592c602c64fcdffdd5c7) docs: updated CHANGELOG.md (#4452)
 * [80df67b38](https://github.com/kubeovn/kube-ovn/commit/80df67b38fef49f3b2ae05959ddab1360a50c35f) metrics: do not export information if a subnet is not validated (#4444)
 * [365d17dbc](https://github.com/kubeovn/kube-ovn/commit/365d17dbc3109bd645185291cd1d35974859fdc9) add ut (#4448)
 * [07ec2a337](https://github.com/kubeovn/kube-ovn/commit/07ec2a337f650ba605f21c1c5715edd6278f4ea0) build(deps): bump google.golang.org/grpc from 1.65.0 to 1.66.0 (#4449)
 * [0df9de1f9](https://github.com/kubeovn/kube-ovn/commit/0df9de1f91dc2f75d94f1ec998308f74a1396e8d) build(deps): bump github.com/docker/docker (#4450)
 * [add87ee6e](https://github.com/kubeovn/kube-ovn/commit/add87ee6e8fbe29e75aaa425ca66fc487a181c44) ut: add unit test for ovn.go and ovs/util.go (#4440)
 * [72cbfa5e5](https://github.com/kubeovn/kube-ovn/commit/72cbfa5e51440ebeaa863fb8e38a75b4fe5f65d5) Add ipam ut (#4442)
 * [509a660f8](https://github.com/kubeovn/kube-ovn/commit/509a660f8ee7ab4664ff3bde92f180ca3ea753fd) build(deps): bump github.com/containerd/containerd from 1.7.20 to 1.7.21 (#4443)
 * [2e17cb4dc](https://github.com/kubeovn/kube-ovn/commit/2e17cb4dc542a95616bf8bb1f113a0bd76f044e0) fix: init default vpc (#4429)
 * [f7da30552](https://github.com/kubeovn/kube-ovn/commit/f7da30552ad35d398709d4db1ea4926830810f27) build(deps): bump github.com/vishvananda/netlink from 1.2.1 to 1.3.0 (#4438)
 * [aa08ad652](https://github.com/kubeovn/kube-ovn/commit/aa08ad652a500034fb845627b59baad250907c13) build(deps): bump github.com/prometheus/client_golang (#4439)
 * [b197ad34e](https://github.com/kubeovn/kube-ovn/commit/b197ad34e20bac6ce0e38110f2ed40ded63c3a19) build(deps): bump kubevirt.io/api from 1.3.0 to 1.3.1 (#4431)
 * [6f669c705](https://github.com/kubeovn/kube-ovn/commit/6f669c7050487b354618df733adcdf291b3c53db) feat: allow setting default subnet for custom vpc (#4171)
 * [c1a367b52](https://github.com/kubeovn/kube-ovn/commit/c1a367b52910039b792700559ffdc150322fe539) Add ipam ut (#4424)
 * [b21c96c3c](https://github.com/kubeovn/kube-ovn/commit/b21c96c3c3e8a8a8a682580c6b985beb8425a162) bump github.com/vishvananda/netlink to v1.2.1 (#4434)
 * [748aaf4bd](https://github.com/kubeovn/kube-ovn/commit/748aaf4bd7990c9be4f4aeab83c7220d6d70ed65) bump kubevirt to v1.3.1 (#4433)
 * [41a23b88b](https://github.com/kubeovn/kube-ovn/commit/41a23b88b3c1ccb9f7caf1459fd6d6b1712b8f7d) add unit test for ovn-nb-port_group (#4427)
 * [f93c39336](https://github.com/kubeovn/kube-ovn/commit/f93c3933678c6ddb88c9d50f19a4c22b808ff7ad) add unit test for NewAnpACLMatch (#4386)
 * [6355a337a](https://github.com/kubeovn/kube-ovn/commit/6355a337a58e6d0c4b0d1686da1ca872f412b9c2) build(deps): bump github.com/onsi/ginkgo/v2 from 2.20.0 to 2.20.1 (#4432)
 * [5d5b17236](https://github.com/kubeovn/kube-ovn/commit/5d5b17236504d0e6ad263ff01e46be0ac4abfe90) vpc-nat-gateway: use iptables-legacy for centos 7 (#4428)
 * [6bfbfe815](https://github.com/kubeovn/kube-ovn/commit/6bfbfe815532287a2b8f0f11f09bc4d74d0d5c77) feat(bgp): logger & enhance nadProvider config (#4352)
 * [46b8f569c](https://github.com/kubeovn/kube-ovn/commit/46b8f569c09571e95abeabcf5df4f4e526a804a8) netpol: add allow acl rules for u2o logical gateway (#4420)
 * [226601a64](https://github.com/kubeovn/kube-ovn/commit/226601a647af667377b6999457f3cd8b772e43fe) build(deps): bump github.com/prometheus/client_golang (#4423)
 * [7bb3cb588](https://github.com/kubeovn/kube-ovn/commit/7bb3cb588d33ccd61302bdd8b110a5a4ba2d7f75) build(deps): bump github.com/Microsoft/hcsshim from 0.12.5 to 0.12.6 (#4422)
 * [e5eba5f10](https://github.com/kubeovn/kube-ovn/commit/e5eba5f105db7f037e02776d801734f9fe3100bb) vpc-nat-gateway: do not add routes for underlay subnets (#4416)
 * [0ec244071](https://github.com/kubeovn/kube-ovn/commit/0ec244071cc6916ffa5f5353fc4b0d134e6fec5d) Makefile: simplify underlay u2o installation (#4419)
 * [b077d2f46](https://github.com/kubeovn/kube-ovn/commit/b077d2f46f87b39570be0c0654c8414aac367c32) bump kind to v0.24.0 (#4411)
 * [9e2cc200b](https://github.com/kubeovn/kube-ovn/commit/9e2cc200bad2ee107cc9d06dd52e9ef3b7098fb9) bump k8s to v1.31.0 (#4403)
 * [1c1e1a520](https://github.com/kubeovn/kube-ovn/commit/1c1e1a520a95a9e51b91f0d604498bd3f7c74fe7) simplicify if debug (#4402)
 * [9c80fc8cc](https://github.com/kubeovn/kube-ovn/commit/9c80fc8ccc14b744d0cc7ed7b9e4fe7358841110) fix kube-ovn-cni run fail on docker (#4409)
 * [8a8c06c74](https://github.com/kubeovn/kube-ovn/commit/8a8c06c747a1220e8f56ed397a6cb87c5bb79786) auto fix lint issues in local dev (#4385)
 * [25473b58a](https://github.com/kubeovn/kube-ovn/commit/25473b58a96d66824db396207b51bcf549256579) ci: bump golangci-lint to v1.60.1 (#4407)
 * [a02733fa4](https://github.com/kubeovn/kube-ovn/commit/a02733fa49ba32d17960e0798fbaae0352b41171) docs: updated CHANGELOG.md (#4401)
 * [ac1cc2c6b](https://github.com/kubeovn/kube-ovn/commit/ac1cc2c6bc95e05e0c848d3a838aaafb0fff806d) build(deps): bump github.com/docker/docker (#4400)
 * [71acfadb4](https://github.com/kubeovn/kube-ovn/commit/71acfadb49c845e20ff895e70ec4185c71693eca) build(deps): bump sigs.k8s.io/controller-runtime from 0.18.4 to 0.18.5 (#4398)
 * [06542452d](https://github.com/kubeovn/kube-ovn/commit/06542452db03e6094b14bca6c263299624478a2c) fix: uppercase ip address is not recommended (#4372)
 * [c9c2d3c61](https://github.com/kubeovn/kube-ovn/commit/c9c2d3c610638056a26ac7b71acc4632ccdd21e6) increase health probe timeout (#4397)
 * [0cf8c0f4e](https://github.com/kubeovn/kube-ovn/commit/0cf8c0f4ee761a628ebafe0f389404f1bdee881c) Support yusur smartnic (#4393)
 * [6c48efe58](https://github.com/kubeovn/kube-ovn/commit/6c48efe586639349fbdc100ffff64010e10ef202) add log (#4390)
 * [966b42ef1](https://github.com/kubeovn/kube-ovn/commit/966b42ef1bcae935cdecffcf93ee85ce35249463) cni-server: do not set sysctl variables (#4395)
 * [881644c5a](https://github.com/kubeovn/kube-ovn/commit/881644c5acbf744c84ddb8f497eaf8d74bfa0344) ci: fix git diff (#4388)
 * [db820a377](https://github.com/kubeovn/kube-ovn/commit/db820a37768c8733e90236307dfeb88faf2bc1d5) ipsec: fix chart installation failure (#4387)
 * [cbe4e37ce](https://github.com/kubeovn/kube-ovn/commit/cbe4e37cef588602a529a08384a084774e08f58b) change cidr to avoid conflict (#4392)
 * [5fc1c7717](https://github.com/kubeovn/kube-ovn/commit/5fc1c7717adfc19b39cb42e920f1bb0625a93d9a) docs: updated CHANGELOG.md (#4391)
 * [ef40eb019](https://github.com/kubeovn/kube-ovn/commit/ef40eb019b77fef3b1605512553b83e15f700ba1) increase the default probe interval for large cluster (#4389)
 * [33d7a282d](https://github.com/kubeovn/kube-ovn/commit/33d7a282d9ad5c46a62330a932bea626a9fd4401) serve pprof in metrics server if metrics server listens on 0.0.0.0 (#4384)
 * [f17033e56](https://github.com/kubeovn/kube-ovn/commit/f17033e5646164547477a06a488ec3d49dc6a867) fix EOF during TLS handshake caused by health check (#4381)
 * [ae88ad811](https://github.com/kubeovn/kube-ovn/commit/ae88ad8113e1fd799dd12b2791791775a5b78864) ut: add unit test for SGLostACL (#4378)
 * [502a90efe](https://github.com/kubeovn/kube-ovn/commit/502a90efeed87537e8f440b42dc6ca3ef1903309) refactor ipsec function (#4334)
 * [b32eb6ea6](https://github.com/kubeovn/kube-ovn/commit/b32eb6ea620554a0a042c1cfe3900c93f226f977) replace protocol check in netpol update (#4359)
 * [610ac19ea](https://github.com/kubeovn/kube-ovn/commit/610ac19eab12e50027c21ec4955313ed175b321d) fix controller-runtime logger not set (#4380)
 * [c3b3041a1](https://github.com/kubeovn/kube-ovn/commit/c3b3041a129cba1832ab487c033e747efa967a16) build(deps): bump golang.org/x/sys from 0.23.0 to 0.24.0 (#4383)
 * [5baabfb5f](https://github.com/kubeovn/kube-ovn/commit/5baabfb5f2e05e64f92d5ac405aea368d91f0166) fix: should delete subnet before vpc (#4370)
 * [96e4aed19](https://github.com/kubeovn/kube-ovn/commit/96e4aed19cab227db8bf559a4c1d180105fba708) fix: use route replace to avoid confilcts (#4371)
 * [818649cbb](https://github.com/kubeovn/kube-ovn/commit/818649cbbaf590805f38283c8fec405619f3f57f) Fix DISABLE_MODULES_MANAGEMENT (#4365)
 * [0faf8a038](https://github.com/kubeovn/kube-ovn/commit/0faf8a0387bde757f2fdb75665e7c330305ad7d8) fix: correct the priority of ACL associated with security group (#4366)
 * [ef4c017e2](https://github.com/kubeovn/kube-ovn/commit/ef4c017e2f655dc68cfd49a6c48012b62f60b02b) bump multus to v4.1.0 (#4377)
 * [33399c2dc](https://github.com/kubeovn/kube-ovn/commit/33399c2dc328983794837f311569181aff0df396) bump go to 1.22.6 (#4373)
 * [d50778168](https://github.com/kubeovn/kube-ovn/commit/d5077816805ab18e1c574faa9ad041b394973643) build(deps): bump golang.org/x/mod from 0.19.0 to 0.20.0 (#4367)
 * [9c4c6e0ce](https://github.com/kubeovn/kube-ovn/commit/9c4c6e0ce0d73cec74afda3ff09f2aced84a9aed) build(deps): bump golang.org/x/sys from 0.22.0 to 0.23.0 (#4368)
 * [631ff2ff1](https://github.com/kubeovn/kube-ovn/commit/631ff2ff1fe16c2b8d0de410efdf31fb9bb796af) build(deps): bump golang.org/x/time from 0.5.0 to 0.6.0 (#4369)
 * [ed16ce5a2](https://github.com/kubeovn/kube-ovn/commit/ed16ce5a2b8fb4288c88d87fefb174530796a007) vpc dns: remove unused variable (#4335)
 * [5181842ef](https://github.com/kubeovn/kube-ovn/commit/5181842ef55cfc228ea27e7ccfe440ec734fca88) build(deps): bump github.com/osrg/gobgp/v3 from 3.28.0 to 3.29.0 (#4362)
 * [55865b05a](https://github.com/kubeovn/kube-ovn/commit/55865b05ab7cf074e04c340d1c00aac946e8b111) build(deps): bump github.com/k8snetworkplumbingwg/network-attachment-definition-client (#4361)
 * [204451948](https://github.com/kubeovn/kube-ovn/commit/204451948d3f1f46dcac85ab18e4d74e6dc6ed08) fix lr-lb dnat with multiple distributed gateway ports (#4351)
 * [e4c67ca93](https://github.com/kubeovn/kube-ovn/commit/e4c67ca93351daab935d0b7efa1d9cd3495cad23) fix: load-balancer for dnat cleaned after restarting kube-ovn-controller (#4357)
 * [df2d066b4](https://github.com/kubeovn/kube-ovn/commit/df2d066b4487879afab308966060b29e9816b2d8) build(deps): bump github.com/onsi/gomega from 1.34.0 to 1.34.1 (#4350)
 * [386732c68](https://github.com/kubeovn/kube-ovn/commit/386732c68d469d9d8fc95677b2091806dc93ec6a) cni-server: disable udp-fragmentation-offload (#4342)
 * [fa429125c](https://github.com/kubeovn/kube-ovn/commit/fa429125cbc87df486a8face28e63eab49152b25) reduce image size by merging layers (#4346)
 * [b3b775e91](https://github.com/kubeovn/kube-ovn/commit/b3b775e911a5875ce85831ff915a7b2030352b04) reduce image sice by merging webhook binary into kube-ovn-cmd (#4345)
 * [6261a3ed3](https://github.com/kubeovn/kube-ovn/commit/6261a3ed3d2f4717ae09f5fd3b3d23019bf3c355) move initDefaultVpc into initOVN (#4344)
 * [be2ddb920](https://github.com/kubeovn/kube-ovn/commit/be2ddb920163c1d63aeb7233c50dd3229f0b3bbb) refactor eip and snat in default vpc with lr policy (#4343)
 * [28844842d](https://github.com/kubeovn/kube-ovn/commit/28844842d4f10375e9303c3d7fe263aa67c9423a) Add BGP capabilities to the NAT GW (#4285)
 * [cd3da988d](https://github.com/kubeovn/kube-ovn/commit/cd3da988d00b74b8d59b7212eb47a6f5aca0969b) build(deps): bump github.com/onsi/ginkgo/v2 from 2.19.0 to 2.19.1 (#4340)
 * [4ce36b654](https://github.com/kubeovn/kube-ovn/commit/4ce36b654d2f5c21f110cc871b2d397d7e0fb166) build(deps): bump github.com/onsi/gomega from 1.33.1 to 1.34.0 (#4341)
 * [41260c8c0](https://github.com/kubeovn/kube-ovn/commit/41260c8c040f6a10350c2fdfac9f6261112b1e5e) add acl action log annotation for netpol/anp/banp (#4338)
 * [006f74e80](https://github.com/kubeovn/kube-ovn/commit/006f74e80215e1c5a513ba9fd6c8e0bd7694b11d) Add ut (#4296)
 * [22a78ea80](https://github.com/kubeovn/kube-ovn/commit/22a78ea80d5166ec791457f32ef3b3b2e30bc982) build(deps): bump github.com/prometheus-community/pro-bing (#4339)
 * [9316e17cd](https://github.com/kubeovn/kube-ovn/commit/9316e17cd4917b11680d9314edb7bd46582ebf94) security: run as non-root user (#4326)
 * [d80de4599](https://github.com/kubeovn/kube-ovn/commit/d80de4599f5e2ce081117e25e51c8a5173763faf) build(deps): bump github.com/docker/docker (#4336)
 * [60040887a](https://github.com/kubeovn/kube-ovn/commit/60040887a6209452a50a9386462392eb6377c9d8) crd: fix missing fields (#4333)
 * [af95247e4](https://github.com/kubeovn/kube-ovn/commit/af95247e4608713daadec6028ecc3a0bc4670da9) pinger: fix process not terminated on sigkill (#4329)
 * [05984fc94](https://github.com/kubeovn/kube-ovn/commit/05984fc949f046bcf6e11948e61191fbd81ba166) fix: scripts (#4291)
 * [9229c4551](https://github.com/kubeovn/kube-ovn/commit/9229c45515fb32da63b56b14ac1aca21733a30de) build(deps): bump github.com/containernetworking/cni from 1.2.2 to 1.2.3 (#4328)
 * [4c5ec03e7](https://github.com/kubeovn/kube-ovn/commit/4c5ec03e7f5d524470c5291a947accec03adabc3) build(deps): bump github.com/docker/docker (#4327)
 * [1ba9e7c9e](https://github.com/kubeovn/kube-ovn/commit/1ba9e7c9ebb3f4b466e08b53b3b809358e3f432e) add new linter (#4322)
 * [d009a27f1](https://github.com/kubeovn/kube-ovn/commit/d009a27f185646ede2d80bc617598cd4796a88c6) add support for interface activation strategy (#4294)
 * [a21de7eaf](https://github.com/kubeovn/kube-ovn/commit/a21de7eaf42c872fd76bdf2997f3c0ba2f123759) ci: bump submariner to 0.18.0 (#4287)
 * [be7fd1741](https://github.com/kubeovn/kube-ovn/commit/be7fd17412bfb756208df00c8fa1df0082d69904) fix dialing https server (#4324)
 * [7537eb937](https://github.com/kubeovn/kube-ovn/commit/7537eb9376c39430f88e92c947ddbb8c98796801) refactor metrics (#4313)
 * [d9e761cbf](https://github.com/kubeovn/kube-ovn/commit/d9e761cbf422167b48f8fb2ca8dcf2f49a4b5d32) metrics: fix missing rbac for sa ovn (#4312)
 * [6824a58e2](https://github.com/kubeovn/kube-ovn/commit/6824a58e27b89a3ccc6dd4a3b1eae3d1000919a4) bump kubevirt to v1.3.0 (#4311)
 * [35fcef891](https://github.com/kubeovn/kube-ovn/commit/35fcef8912b92b111278300ac01bd8c8c0a09da9) update update-codegen-docker.sh (#4319)
 * [b2aec9258](https://github.com/kubeovn/kube-ovn/commit/b2aec9258804e0fba83ec9a31e97b0663b3d9084) ci: add timeout for executing cleanup.sh (#4321)
 * [0606d63b2](https://github.com/kubeovn/kube-ovn/commit/0606d63b2174232af7cbe6125635ecd3fd6e834c) e2e: skip north gateway test for versions prior to v1.12 (#4320)
 * [3a0a9104e](https://github.com/kubeovn/kube-ovn/commit/3a0a9104ed52cc4293eb85c39d522c2448639d5e) metrics: add support for secure serving (#4297)
 * [77d6f9985](https://github.com/kubeovn/kube-ovn/commit/77d6f9985ee98b2e962c38aaf084e94ec603ebc7) bump k8s to v1.30.3 (#4305)
 * [087e3323b](https://github.com/kubeovn/kube-ovn/commit/087e3323b9e7a90593c87da0fc12696585edc220) build(deps): bump k8s.io/kubernetes in the k8s-io group (#4309)
 * [109fd0a89](https://github.com/kubeovn/kube-ovn/commit/109fd0a892475e8662e8dfde171d4bf51e8da4f3) build(deps): bump github.com/moby/sys/mountinfo from 0.7.1 to 0.7.2 (#4310)
 * [2159ff5ef](https://github.com/kubeovn/kube-ovn/commit/2159ff5efe2511276a301cf3bf1db5b68cdce7ef) fix map concurrent read and write crash (#4302)
 * [26c04b1bf](https://github.com/kubeovn/kube-ovn/commit/26c04b1bfd7111ef1e26492369b05f3e9e71dd99) security: run as unprivileged (#3040)
 * [9e928d639](https://github.com/kubeovn/kube-ovn/commit/9e928d639265792c4f049d9aadddc1cd671812c6) add anp/banp code (#4290)
 * [1f58f75dc](https://github.com/kubeovn/kube-ovn/commit/1f58f75dcb28c34ba8c787a6bf9132490600d4ec) fix e2e
 * [83fba9ea3](https://github.com/kubeovn/kube-ovn/commit/83fba9ea38549ec8933054f6d9c1edb63dae23d5) Use route policy to reimplement northGateway (#4289)
 * [4f0d43d30](https://github.com/kubeovn/kube-ovn/commit/4f0d43d308adacd0e78b048b2beab77f71801f50) build(deps): bump github.com/Microsoft/hcsshim from 0.12.4 to 0.12.5 (#4299)
 * [e3222733d](https://github.com/kubeovn/kube-ovn/commit/e3222733df840c06e5c0684ae786f0087fba8947) base: bump ubuntu to 24.04 (#4293)
 * [ee78b5c3b](https://github.com/kubeovn/kube-ovn/commit/ee78b5c3b0e722c3f8ea35efd5bae3d7fae8a72c) underlay: set trunks of host nic port (#4282)
 * [b17975854](https://github.com/kubeovn/kube-ovn/commit/b17975854204aa916bd00dc57b7d9717b556cdda) do not set gatewayType for non ovn subnet and custom vpc subnet (#4283)
 * [8a9f4c1b1](https://github.com/kubeovn/kube-ovn/commit/8a9f4c1b1cec24dec94cef5ed3462da4eeecb3e3) fix using route table name as vpc name (#4284)
 * [f21c9c308](https://github.com/kubeovn/kube-ovn/commit/f21c9c308df1b065db0e2529d62dbf5c91859c6e) fix ovn lb not updated due to service update failure (#4280)
 * [78fe1f920](https://github.com/kubeovn/kube-ovn/commit/78fe1f920b920492412c7e7820cc840b307e1936) vpc-nat-gw: configure routes for subnets by pod annotations (#4278)
 * [e043e970f](https://github.com/kubeovn/kube-ovn/commit/e043e970faa860ce4d98e4c12a2efa20e6a44c5b) docs: updated CHANGELOG.md (#4276)
 * [e5029d569](https://github.com/kubeovn/kube-ovn/commit/e5029d569f481397222ae62a4033fde5488a2083) fix: set dhcp gateway to U2OInterconnectionIP when enabling dhcp and u2o (#4228)
 * [45a16a713](https://github.com/kubeovn/kube-ovn/commit/45a16a7137243cf818b0ddbb603c7c19400b1fdc) ci: run go mod tidy before building kubectl (#4274)
 * [51839ce3e](https://github.com/kubeovn/kube-ovn/commit/51839ce3e970b721f9009673fa3326bfa7185083) build(deps): bump aquasecurity/trivy-action from 0.23.0 to 0.24.0 (#4275)
 * [bc61393b2](https://github.com/kubeovn/kube-ovn/commit/bc61393b2e1475768a77bc639cf87061d3e0dce3) enable nat gw by default (#4273)
 * [e56a4cd13](https://github.com/kubeovn/kube-ovn/commit/e56a4cd13ca84dbb8d09f84c4e90012035f92f88) klog: set log file max size to 200MB (#4272)
 * [e74e6cad3](https://github.com/kubeovn/kube-ovn/commit/e74e6cad3fa337e0edd921b13365e437fc3bebe3) logrotate: set file size limit to 100M (#4271)
 * [0900ace50](https://github.com/kubeovn/kube-ovn/commit/0900ace50892a10305f76b92c13bae01f06ac89c) remove unused environment variable LOG_ROTATE (#4270)
 * [6b5356512](https://github.com/kubeovn/kube-ovn/commit/6b5356512b758c7c11ab75645eabc40497a02ab4) fix e2e (#4269)
 * [6ed2463ed](https://github.com/kubeovn/kube-ovn/commit/6ed2463ed10e47a295ceb2bb29a4b79729a34d72) ci: disable cgo when building kubelet and cni plugins (#4268)
 * [7006d9dd6](https://github.com/kubeovn/kube-ovn/commit/7006d9dd6efaf0bd2b0a95112e25df219bd5eb15) when router is deleted return success for static route deletion (#4266)
 * [cfd2bd70a](https://github.com/kubeovn/kube-ovn/commit/cfd2bd70a1faa1b49b1ed3899f68d29a38f504a2) ipam: fix ip not released for non-ovn subnets (#4265)
 * [e25bf15f7](https://github.com/kubeovn/kube-ovn/commit/e25bf15f7441f49e88dd247cb33feeeb53bc4c86) fix vpcnatgw image is not synced (#4264)
 * [5f8cef9d5](https://github.com/kubeovn/kube-ovn/commit/5f8cef9d5523dd3f0c309ead96944901bdfea80f) cni-server: break gateway check after pod is terminated/missing (#4248)
 * [c7310a606](https://github.com/kubeovn/kube-ovn/commit/c7310a606133776525e0b73867c0574ee375801f) do not create iptables rule for setting tcp mss (#4260)
 * [989d854cf](https://github.com/kubeovn/kube-ovn/commit/989d854cf60aa8c160971a1403f6fb87062b2776) fix invalid subnet not sync route (#4258)
 * [e4cf12180](https://github.com/kubeovn/kube-ovn/commit/e4cf12180487a2bfd1935a3799fab2c32bda36f4) do not allocate mac for non-ovn subnets (#4242)
 * [929e3362b](https://github.com/kubeovn/kube-ovn/commit/929e3362bac99e9c58452fd82921db120806d27f) refactor lb svc e2e (#4236)
 * [7de95baa6](https://github.com/kubeovn/kube-ovn/commit/7de95baa6fc2f3fbbb7eb61372d9ac5504b7f027) build(deps): bump golang.org/x/sys from 0.21.0 to 0.22.0 (#4257)
 * [082b1488f](https://github.com/kubeovn/kube-ovn/commit/082b1488f064d86a45bdfe81863cfbe2f0a4c001) build(deps): bump golang.org/x/mod from 0.18.0 to 0.19.0 (#4256)
 * [877862820](https://github.com/kubeovn/kube-ovn/commit/8778628206119c0ec4fb84fe144b30dd1f0a3bca) build kubectl and cni plugins from source if vuln found in the base image (#4253)
 * [607818c9c](https://github.com/kubeovn/kube-ovn/commit/607818c9c43d04186c29e34bee22bd2970701a3c) add retry check u2o openflow filter rule (#4255)
 * [11c4c7640](https://github.com/kubeovn/kube-ovn/commit/11c4c7640c09aeebe0aa6323f73f902c09f997e3) e2e case "should drop ARP/ND request from localnet port to LRP" (#4251)
 * [2b71a56ac](https://github.com/kubeovn/kube-ovn/commit/2b71a56ac78b1836df8b3af245751a435a33a332) check both sts name and UID when handling pod deletion (#4238)
 * [41755bfc0](https://github.com/kubeovn/kube-ovn/commit/41755bfc035068522ebb55773b9a14b2fde09ec1) do not exit if chassis is not found (#4246)
 * [d0e147603](https://github.com/kubeovn/kube-ovn/commit/d0e147603345dc9e1e33e077a1641a990bf27b63) build(deps): bump github.com/osrg/gobgp/v3 from 3.27.0 to 3.28.0 (#4244)
 * [09dd08fd7](https://github.com/kubeovn/kube-ovn/commit/09dd08fd7498a4efe793f9cb4926ee83c75feb8c) build(deps): bump github.com/docker/docker (#4245)
 * [d9b5e4f61](https://github.com/kubeovn/kube-ovn/commit/d9b5e4f61e218622a2352da09b3c0a99418e8d15) force deleted subnet left ip, can only be handled by human (#4157)
 * [b614b1bc2](https://github.com/kubeovn/kube-ovn/commit/b614b1bc21f34b53100de0d83825889fc6aa3745) vpc-nat-gateway: configure initial routes by pod annotations (#4240)
 * [751fa71f5](https://github.com/kubeovn/kube-ovn/commit/751fa71f50e5cb9afe99682f1164c6bd5165b273) vpc-dns: set route to apiserver by pod annotation (#4239)
 * [627e2fdbb](https://github.com/kubeovn/kube-ovn/commit/627e2fdbbb61be4eb55a19c785f54050a7c3aa58) update node labels/annotations by json merge patch (#4230)
 * [d857ffbbf](https://github.com/kubeovn/kube-ovn/commit/d857ffbbff3f26e3ca9098ab8fe97539a431107b) vpc-nat-gateway: print messgae to stderr (#4237)
 * [9bcee4116](https://github.com/kubeovn/kube-ovn/commit/9bcee4116e2901584a8ea7b586ae6f6e27ef3955) fix: init if nat gw exists (#4232)
 * [cc9ca11b5](https://github.com/kubeovn/kube-ovn/commit/cc9ca11b58449f5b5c294b637b29c8ccbdb0567f) lb svc: fix dead lock (#4235)
 * [06fd80b45](https://github.com/kubeovn/kube-ovn/commit/06fd80b4538721bb502190d10f176d1eb7d5da32) ci: dump/upload events on e2e test failure (#4231)
 * [cbad90e1b](https://github.com/kubeovn/kube-ovn/commit/cbad90e1b330b44d4dd4acbc7b37b887d1082bda) ci: collect and upload audit logs on e2e failure (#4227)
 * [eebe7038b](https://github.com/kubeovn/kube-ovn/commit/eebe7038b89e73de59d9aa2b5548edc335e90596) docs: updated CHANGELOG.md (#4234)
 * [3e894e23b](https://github.com/kubeovn/kube-ovn/commit/3e894e23b7070eca6ef652dc67fa82e28c727533) build(deps): bump github.com/docker/docker (#4233)
 * [fa8b8ef2c](https://github.com/kubeovn/kube-ovn/commit/fa8b8ef2cb24fd75c120214e9afb0c7a9be4af8f) e2e: retry traceroute (#4225)
 * [735868470](https://github.com/kubeovn/kube-ovn/commit/73586847099ee68d8a0772be14fd1b0857356bb3) fix ipv6 service ip not added to ovn lb vips due to pod cache not synced (#4223)
 * [816a4bfad](https://github.com/kubeovn/kube-ovn/commit/816a4bfad0c58e80be0cdcae003014ecbc7271dc) build(deps): bump github.com/containernetworking/cni from 1.2.1 to 1.2.2 (#4222)
 * [c81e91c9f](https://github.com/kubeovn/kube-ovn/commit/c81e91c9fbc12141af585698d33e29907f170318) build(deps): bump github.com/docker/docker (#4221)
 * [be0cf5387](https://github.com/kubeovn/kube-ovn/commit/be0cf53871407faabc1f976298e041da0e2ccb95) e2e: exec ovn-nbctl via kubectl exec svc/ovn-nb (#4218)
 * [42cb4daa3](https://github.com/kubeovn/kube-ovn/commit/42cb4daa3596ac1c6822a07f1acf6e973d614fde) e2e: add curl timeouts (#4216)
 * [81f1927f3](https://github.com/kubeovn/kube-ovn/commit/81f1927f3ee14a2cf6c4335b9114abd92a3501be) e2e: fix logging (#4220)
 * [c5f03705d](https://github.com/kubeovn/kube-ovn/commit/c5f03705df56cf546e64762cd4386ae4b395d7a9) pinger: fix warning logs (#4213)
 * [2e80b7efe](https://github.com/kubeovn/kube-ovn/commit/2e80b7efe6e4f8b2433b4bce184765c43b0c4bc8) delete unused scripts (#4214)
 * [80b1d62bb](https://github.com/kubeovn/kube-ovn/commit/80b1d62bb7fb79e15f04290c6e2aa17cbe05a9ab) base: clean ipset deb files (#4215)
 * [be5277aec](https://github.com/kubeovn/kube-ovn/commit/be5277aec1c56491bb707ffa3545c5778bc98ed9) ic: ensure db file is fixed (#4211)
 * [acf78727b](https://github.com/kubeovn/kube-ovn/commit/acf78727b30d70162d162c1a53b82fc127eab0c9) fix getting service cluster ips (#4206)
 * [a11abec4e](https://github.com/kubeovn/kube-ovn/commit/a11abec4e4d5c38d32e43b867f6077de2ca26c88) add unit test for ip_range (#4125)
 * [ccff4ed94](https://github.com/kubeovn/kube-ovn/commit/ccff4ed942a71516e5218cfbcd69b94409f9ccc9) fix: nil pointer when subnet is not ready (#4190)
 * [6dde684a9](https://github.com/kubeovn/kube-ovn/commit/6dde684a91ad171842846ab4aaa08ee2dabd85b5) e2e: fix getting ipv4 address from link (#4177)
 * [bbf29e7b3](https://github.com/kubeovn/kube-ovn/commit/bbf29e7b3ff5b8321a48466b94b838d7771a4c54) docs: updated CHANGELOG.md (#4203)
 * [f9e53dc59](https://github.com/kubeovn/kube-ovn/commit/f9e53dc598d874abca205e0898080c122be9df0b) remove unused annotation (#4191)
 * [f63172d12](https://github.com/kubeovn/kube-ovn/commit/f63172d12d4046227c86f3a3579aa10366c78c8e) add e2e test cases for nat outgoing routes (#4187)
 * [2942337fa](https://github.com/kubeovn/kube-ovn/commit/2942337fa0ef9b38eec721a752520efc2da47adf) e2e: add ginkgo.GinkgoHelper() (#4192)
 * [e1ddaafb8](https://github.com/kubeovn/kube-ovn/commit/e1ddaafb803104737fae1f71730138456da7390e) build(deps): bump k8s.io/klog/v2 in the k8s-io group (#4202)
 * [183b2e606](https://github.com/kubeovn/kube-ovn/commit/183b2e606eec377d1a49e2ac6dfc9be1eccbe993) fix vm not running after changing the subnet (#4199)
 * [9684aee46](https://github.com/kubeovn/kube-ovn/commit/9684aee46d059c4458c406956d8b5e507655c074) pinger: reset interface_rx_multicast_packets (#4198)
 * [5c6029e58](https://github.com/kubeovn/kube-ovn/commit/5c6029e58bf6869cbc1f649dc8251d1e41aecac5) fix kube-ovn-cni crash for newly added nodes , due to old legacy event in deleteNodeQueue (#4197)
 * [e16f3809a](https://github.com/kubeovn/kube-ovn/commit/e16f3809a90d4702b4a50c0b1700f4ccca14753c) build(deps): bump k8s.io/klog/v2 from 2.120.1 to 2.130.0 (#4184)
 * [6b73daeb8](https://github.com/kubeovn/kube-ovn/commit/6b73daeb8ca2e0ff97dacc6a270911b27764ce60) build(deps): bump github.com/containernetworking/cni from 1.2.0 to 1.2.1 (#4189)
 * [ee65995a9](https://github.com/kubeovn/kube-ovn/commit/ee65995a947646c14a4a0a7b7cee054e3565f36f) build(deps): bump github.com/containernetworking/plugins (#4188)
 * [5566029f6](https://github.com/kubeovn/kube-ovn/commit/5566029f683ad63e9712a8d7d746cf357ca7310f) base: bump cni plugins to v1.5.1 (#4185)
 * [b57964cab](https://github.com/kubeovn/kube-ovn/commit/b57964cabc6523fb07a8104072a74921dc46f4e1) fix u2o arp/nd e2e
 * [e0bcc2967](https://github.com/kubeovn/kube-ovn/commit/e0bcc2967422bcb5d9b95dc43ee7c666d52e12d9) update ovn upgrade porcess (#4132)
 * [7fe3f6e6c](https://github.com/kubeovn/kube-ovn/commit/7fe3f6e6c81e7f4659b7cb0ffb37692e51ed6d92) add helper function PodIPs() (#4178)
 * [2f3c5d48b](https://github.com/kubeovn/kube-ovn/commit/2f3c5d48bc5a96275ac80ad5d2290b34ab55557d) northd: skip arp/nd request for lrp addresses from localnet ports (#4158)
 * [bbbb0d509](https://github.com/kubeovn/kube-ovn/commit/bbbb0d509c69d8b3578cb1df6a25d918d4f2fca6) ci: check pod crashes on installation/e2e failure (#4160)
 * [3e933249e](https://github.com/kubeovn/kube-ovn/commit/3e933249eb4a79ae46296c1fe838b01a5ede6c9c) fix change vm subnet e2e (#4114)
 * [f4efa2c86](https://github.com/kubeovn/kube-ovn/commit/f4efa2c861b6dea14ada63fdcf1ce04698a56fbb) fix ipam init (#4155)
 * [a639831a3](https://github.com/kubeovn/kube-ovn/commit/a639831a38ba8467ba26cd08b3b2ed576f3217f4) use IsZero() to check whether the DeletionTimestamp is nil (#4176)
 * [8562d70d9](https://github.com/kubeovn/kube-ovn/commit/8562d70d95de9242f6569befa87172358f6f6f04) ci: push amd64 legacy image only for branches >= v1.13 (#4179)
 * [3917b8b5f](https://github.com/kubeovn/kube-ovn/commit/3917b8b5f81a8baadc3f8855edf3bac2b7fcbd0e) ut add ip range split (#4115)
 * [e6445d816](https://github.com/kubeovn/kube-ovn/commit/e6445d816a9b03b2ab6c48aac8a4826d0008f241) fix reconcile routes (#4168)
 * [b722ffb15](https://github.com/kubeovn/kube-ovn/commit/b722ffb15a859b4f35d9bbf224f658e0b6e743e5) add support for amd64 legacy cpu (#4170)
 * [0edbf4e90](https://github.com/kubeovn/kube-ovn/commit/0edbf4e909c9961736487bd77e222a549012cbeb) build(deps): bump github.com/docker/docker from 26.1.4+incompatible to 27.0.0+incompatible (#4173)
 * [10871e85a](https://github.com/kubeovn/kube-ovn/commit/10871e85abbda5d48fa47fb76c3b73a2567d3c3a) docs: updated CHANGELOG.md (#4175)
 * [49c9ff1d8](https://github.com/kubeovn/kube-ovn/commit/49c9ff1d890fd525c066192c70fb1c3f87b42c9c) e2e: fix retrieving docker network information (#4169)
 * [6ac97fbb2](https://github.com/kubeovn/kube-ovn/commit/6ac97fbb21b1aaa58a11262038f09d69c4406aab) docs: updated CHANGELOG.md (#4166)
 * [3da2fa298](https://github.com/kubeovn/kube-ovn/commit/3da2fa29880f1370638275bd3a46037ccba12bf5) fix lr policy recreation after kube-ovn-controller restarts (#4123)
 * [b6b789081](https://github.com/kubeovn/kube-ovn/commit/b6b78908169765f3a8599d76abfe9bcd8e954d72) bump k8s to v1.30.2 (#4164)
 * [5132e77ee](https://github.com/kubeovn/kube-ovn/commit/5132e77ee9534047ee8ef86fa51375490f7d8cc5) base: bump kubectl to v1.30.2 (#4163)
 * [ccd5132d0](https://github.com/kubeovn/kube-ovn/commit/ccd5132d088ed54e73d4958f1615e99ee91261f9) ci: fix retrieving docker network subnet/gateway (#4161)
 * [98447feb6](https://github.com/kubeovn/kube-ovn/commit/98447feb6f63a78e4cb3bc4925978d1eeaa33af6) update ovn patches (#4143)
 * [9c3ff7d36](https://github.com/kubeovn/kube-ovn/commit/9c3ff7d368556699eb7ff0ee33ee18d4bb973207) fix u2o e2e in 1.9 (#4154)
 * [351ba96f7](https://github.com/kubeovn/kube-ovn/commit/351ba96f7901088d188c352ad5e85421a94eb11f) speaker: add option to set node IP to override pod IP (#4140)
 * [37b7c46ad](https://github.com/kubeovn/kube-ovn/commit/37b7c46ad5d83aa8a4ca94260ab227d3feb4ee0e) Drop u2o arp request (#4135)
 * [f5b1d2abc](https://github.com/kubeovn/kube-ovn/commit/f5b1d2abcda157f447a98d715902b2a0a6e544ec) add ovn0 default route (#4127)
 * [5ed1c9f66](https://github.com/kubeovn/kube-ovn/commit/5ed1c9f669f5c0494915df55ec9e136e68e4dcd6) build(deps): bump google.golang.org/protobuf from 1.34.1 to 1.34.2 (#4146)
 * [51d46cdf1](https://github.com/kubeovn/kube-ovn/commit/51d46cdf187676607d5f021c3e1e43bf4c6cfb21) ovn: add support for conditionally skipping conntrack (#4131)
 * [815bfc280](https://github.com/kubeovn/kube-ovn/commit/815bfc2807fe13109d43d782a4c5e3628bf831c4) Ipam ut (#4112)
 * [50b0b8252](https://github.com/kubeovn/kube-ovn/commit/50b0b8252de6e181dc0567a53bcfb0064975b87a) build(deps): bump github.com/Microsoft/hcsshim from 0.12.3 to 0.12.4 (#4141)
 * [7220a5ee2](https://github.com/kubeovn/kube-ovn/commit/7220a5ee29a84ccfcd84eb34f8fbd48f6f2cab87) allow ip change name in migration (#4133)
 * [d6b41942a](https://github.com/kubeovn/kube-ovn/commit/d6b41942a393aa4b88f8a14fbbeaafa69061d641) distinguish-portSecurity-with-security-group (#4134)
 * [442cb0d5b](https://github.com/kubeovn/kube-ovn/commit/442cb0d5b91f8d40683b75736736e8740daea9c3) build(deps): bump github.com/docker/docker (#4136)
 * [5253c69e0](https://github.com/kubeovn/kube-ovn/commit/5253c69e0830b2eb64aa81b67dc629b84fa2c6dc) build(deps): bump sigs.k8s.io/controller-runtime from 0.18.3 to 0.18.4 (#4137)
 * [426b5d0a6](https://github.com/kubeovn/kube-ovn/commit/426b5d0a633e5a5e2c15492b04f5b059f1921515) fix ipam subnet concurrent map iteration and map write (#4126)
 * [ab8665191](https://github.com/kubeovn/kube-ovn/commit/ab8665191aa51aa4197985501bfd257729e637d0) trivy: ignore unfixed CVEs (#4129)
 * [9650f03b5](https://github.com/kubeovn/kube-ovn/commit/9650f03b5bdf04964aa6a391fef0a2b42f89a181) bump ovn to v24.03 - part 1 (#4083)
 * [7acdbab5a](https://github.com/kubeovn/kube-ovn/commit/7acdbab5a6e1681e90df5a0a29a749085de8c559) bump go to 1.22.4 (#4121)
 * [120bc8346](https://github.com/kubeovn/kube-ovn/commit/120bc83466fc29fe5f9ad41e9f3b2fe52953d7f1) build(deps): bump golang.org/x/sys from 0.20.0 to 0.21.0 (#4118)
 * [a222b682b](https://github.com/kubeovn/kube-ovn/commit/a222b682b818e6f94c2815d5d4d81deff26ed221) Enable inactivity check on ovndb connection (#4006)
 * [51880ef57](https://github.com/kubeovn/kube-ovn/commit/51880ef57ae87c8dde0b010ff68128cdf81b855c) build(deps): bump golang.org/x/mod from 0.17.0 to 0.18.0 (#4120)
 * [9b309190a](https://github.com/kubeovn/kube-ovn/commit/9b309190a1751a075ac74c515ebe77c51ea9286b) build(deps): bump github.com/osrg/gobgp/v3 from 3.26.0 to 3.27.0 (#4119)
 * [78f268fee](https://github.com/kubeovn/kube-ovn/commit/78f268fee444914d5e02f54cb818ad3d266f6efb) fix e2e timeout (#4116)
 * [96190f4cd](https://github.com/kubeovn/kube-ovn/commit/96190f4cd02fb6abed6b4201806d637ef2f6c57b) fix: IP Add/Sub overflow or underflow (#4111)
 * [bc0308e40](https://github.com/kubeovn/kube-ovn/commit/bc0308e40338ec43f3cab6598d9d0d98bf208e6a) delete unused dpdk dockerfile (#4113)
 * [cf7c98262](https://github.com/kubeovn/kube-ovn/commit/cf7c98262f383afe6756e9af10dfd09b7208e151) ovn vpc nat gw e2e only run after 1.12 (#4110)
 * [d62aace22](https://github.com/kubeovn/kube-ovn/commit/d62aace2284c1d8680fcd6b8c7cafa9daf9070fe) u2o: fix ip cr's mac field does not match with lrp mac (#4107)
 * [db821d221](https://github.com/kubeovn/kube-ovn/commit/db821d2210f3921af250897c930147d1c672d779) Makefile: clean generate ssl files and ko logs (#4106)
 * [d5f90bf4a](https://github.com/kubeovn/kube-ovn/commit/d5f90bf4a76e302c79ea90421d6ce5cb2c567aa5) add unit test for util (#4060)
 * [e67d5f12b](https://github.com/kubeovn/kube-ovn/commit/e67d5f12b750ca4c04b632c3dabd94b047d1f1e5) add reinit process for lb-svc pod (#4092)
 * [bcc96b141](https://github.com/kubeovn/kube-ovn/commit/bcc96b1413a39e64dc16a4e69f5eb1f0eb54b5cb) build(deps): bump github.com/emicklei/go-restful/v3 (#4103)
 * [55dbfbb8a](https://github.com/kubeovn/kube-ovn/commit/55dbfbb8acbef4b3b7992ac97b8d1251559c2f42) Fix change subnet (#4088)
 * [5fe053eb8](https://github.com/kubeovn/kube-ovn/commit/5fe053eb8b434622fe395206366309fb99c4c3d3) e2e: remove unused variables (#4098)
 * [1947da9e0](https://github.com/kubeovn/kube-ovn/commit/1947da9e09e5ea46c706342f2e812c762c5f02d7) Makefile: run kubectl-ko script when collecting logs (#4100)
 * [8270d802c](https://github.com/kubeovn/kube-ovn/commit/8270d802c362db4bfd86a97916ee524467227e57) e2e: bump images (#4101)
 * [2eab0593a](https://github.com/kubeovn/kube-ovn/commit/2eab0593ace8765957dd7480bc1da7428ed7bb40) install.sh: waiting for deleted kube-ovn-pinger to disapper (#4096)
 * [368d19043](https://github.com/kubeovn/kube-ovn/commit/368d190430532a66110e0ae0c9fe130b9fb7a973) fix mac conflict (#4095)
 * [9c981274b](https://github.com/kubeovn/kube-ovn/commit/9c981274b50fe1221a9ddc5bc7eac094f6807795) fix: add ip_reserved label for vip (#4093)
 * [202923114](https://github.com/kubeovn/kube-ovn/commit/2029231144785b9dfbdde0ace9ba981901d48625) build(deps): bump github.com/onsi/ginkgo/v2 from 2.18.0 to 2.19.0 (#4085)
 * [afe5446c4](https://github.com/kubeovn/kube-ovn/commit/afe5446c42d03cf3da3483bd60e988b3b43b2f40) build(deps): bump sigs.k8s.io/controller-runtime from 0.18.2 to 0.18.3 (#4084)
 * [e1310e170](https://github.com/kubeovn/kube-ovn/commit/e1310e1705096d80e7ea78d8d47fae6c728e56d8) install.sh: wait for all kube-ovn-pinger pods to be ready (#4082)
 * [13564d9be](https://github.com/kubeovn/kube-ovn/commit/13564d9bebcb106f09ab41ad4ba8cb9f235bd688) ci: print all the previous logs for restarted pods (#4081)
 * [9f67d358c](https://github.com/kubeovn/kube-ovn/commit/9f67d358caa98a09f4daaf75734870e040e2f86f) opt: replace ovn-sbctl with ovsdb-client (#4075)
 * [aebe363ee](https://github.com/kubeovn/kube-ovn/commit/aebe363eea382d351cd37d6dccf16afdc18bc21a) ovs: get controllerrevision with option --ignore-not-found (#4058)
 * [7148bcc36](https://github.com/kubeovn/kube-ovn/commit/7148bcc36b3d91555d5441813e9979ba73c35f38) fix exit on error (#4080)
 * [4bf54e35e](https://github.com/kubeovn/kube-ovn/commit/4bf54e35e38eea37e94b4e84352caf3be92d9ecd) delete lease on cleanup (#4079)
 * [f1daf5c86](https://github.com/kubeovn/kube-ovn/commit/f1daf5c86b7a7214dd8e0267d887d7edb7475b02) base: aoivd unnecessary env variables (#4070)
 * [796d5f089](https://github.com/kubeovn/kube-ovn/commit/796d5f089347243db21e30243a7ac032c3fbe132) ci: downgrade node image to v1.29.2 (#4069)
 * [23f963200](https://github.com/kubeovn/kube-ovn/commit/23f9632002afac8a4c2abb0819aed49f96691bf8) fix crypto/rand: argument to Int is <= 0 (#4077)
 * [e31784afa](https://github.com/kubeovn/kube-ovn/commit/e31784afa1c099aa2d665f1494fcbb0467a0a652) docs: updated CHANGELOG.md (#4072)
 * [2a3cdbd36](https://github.com/kubeovn/kube-ovn/commit/2a3cdbd361a1ecf1442d2659f1fd5abdff39c3cd) fix: gateway should not be network address and broadcast address (#4043)
 * [08e3c56ea](https://github.com/kubeovn/kube-ovn/commit/08e3c56ead51540c440bf9383be4bb2e7cafba51) support ctr to generate ssl certs (#4068)
 * [66f61445a](https://github.com/kubeovn/kube-ovn/commit/66f61445aeb0eb10a8e384d4a9d2f9eb4b2d510d) e2e: skip SCTP connectivity tests for versions prior to 1.12 (#4065)
 * [d76545b8d](https://github.com/kubeovn/kube-ovn/commit/d76545b8d26280f8137c441cddec857c146740c7) fix: should update subnet status after change vm subnet (#4061)
 * [3a7ee3431](https://github.com/kubeovn/kube-ovn/commit/3a7ee3431627b8945c3067a240da2df39bcf672e) bump ginkgo to v2.18.0 (#4062)
 * [1903bd8ce](https://github.com/kubeovn/kube-ovn/commit/1903bd8cef57a7b1cdea395ed43335b5a747fc8e) ci: fix scheduled e2e (#4057)
 * [72180104d](https://github.com/kubeovn/kube-ovn/commit/72180104df098b90e4778da1bec44bd625f459b5) ci: build e2e binaries and free disk space on necessary (#4059)
 * [f00ff138f](https://github.com/kubeovn/kube-ovn/commit/f00ff138fab75c466555fc303ba95704de0ace4e) crd: add subnet name pattern (#4054)
 * [bf1cb76cd](https://github.com/kubeovn/kube-ovn/commit/bf1cb76cd9dc4e592b5513d3c14e1832a8c32c8d) optimize code (#4049)
 * [d30c1e7be](https://github.com/kubeovn/kube-ovn/commit/d30c1e7be5af8b0b0fcba0ed86247d60868b501e) add err log (#4046)
 * [f98308791](https://github.com/kubeovn/kube-ovn/commit/f98308791273f8dc2d0a798c8e01ed4f566f68f7) build(deps): bump github.com/containernetworking/plugins (#4055)
 * [94ce0acc5](https://github.com/kubeovn/kube-ovn/commit/94ce0acc5f536abe995efd47b8d6d4b6f7064c2c) docs: updated CHANGELOG.md (#4050)
 * [bb46f5713](https://github.com/kubeovn/kube-ovn/commit/bb46f5713fbcd66fa16fe772d8d2df775b7ff3b7) wait for all pods to be deleted before deleting serviceaccount/clusterrole/clusterrolebinding (#4035)
 * [96cd668e8](https://github.com/kubeovn/kube-ovn/commit/96cd668e8d425ee6b54e97c430fcab748a455358) fix: add route for underlay subnet which enables u2o and disables LB (#4039)
 * [e907ec51e](https://github.com/kubeovn/kube-ovn/commit/e907ec51e249a799dfe144177019671e1b69cb76) add pure arm64 build target (#4044)
 * [a0c2a4344](https://github.com/kubeovn/kube-ovn/commit/a0c2a4344b8f211c502dc1e1c161d94bb778a51b) log deleting iptables rule (#4031)
 * [87c007413](https://github.com/kubeovn/kube-ovn/commit/87c007413dc17d6bd7af2568090b9b37d968eaad) build(deps): bump github.com/docker/docker (#4037)
 * [ef3cef030](https://github.com/kubeovn/kube-ovn/commit/ef3cef03058610caf393a3d4f774ec0e0f04ff66) uninstall.sh: delete OVN-POSTROUTING rule in mangle table (#4034)
 * [4995dcc05](https://github.com/kubeovn/kube-ovn/commit/4995dcc05d9123378f1c7555c6350a8ea819e751) cleanup.sh: remove sa/clusterrole/clusterrolebinding (#4024)
 * [e0aa56628](https://github.com/kubeovn/kube-ovn/commit/e0aa566287fb0353871fce0a85f79edcc51582e2) do not use exec for start scripts with trap quit EXIT (#4025)
 * [3015e5dab](https://github.com/kubeovn/kube-ovn/commit/3015e5dab42bdf6d4fbabfcec2e5c95ca2c034be) bump k8s to 1.30.1 (#4028)
 * [2f92a6fc6](https://github.com/kubeovn/kube-ovn/commit/2f92a6fc6fe4542e31ea904a5c9a966567193e9f) fix node annotations not updated when initializing the default provider-network (#4030)
 * [2245d6404](https://github.com/kubeovn/kube-ovn/commit/2245d6404dcf2762d10b4f4489fcc50148eab8a6) set mac in U2OInterconnection ip resources (#4008)
 * [fa66889b1](https://github.com/kubeovn/kube-ovn/commit/fa66889b1154076c466933c9f0f2ed313c7fbf62) rollback residual link and port (#4012)
 * [51602bd8c](https://github.com/kubeovn/kube-ovn/commit/51602bd8cc6695ef3392bd56cca4f287536ac879) build(deps): bump google.golang.org/grpc from 1.63.2 to 1.64.0 (#4027)
 * [a1815db05](https://github.com/kubeovn/kube-ovn/commit/a1815db0537482a53e9d430467b8f5a5cd8cba37) fix add ip eip trigger subnet status count ip (#4023)
 * [85e28bc1e](https://github.com/kubeovn/kube-ovn/commit/85e28bc1e6e2b40de5b5aff6df951f4111783cc0) bump gosec to 2.20.0 (#4021)
 * [9236508a8](https://github.com/kubeovn/kube-ovn/commit/9236508a8ea145a0f788c71328d468ace080772f) remove unused yamls (#4022)
 * [178299a32](https://github.com/kubeovn/kube-ovn/commit/178299a3248f935c5a7683ab98d6390b8536e874) reconcile iptable to the first position after iptables -t nat -F (#3995)
 * [6924c4e6a](https://github.com/kubeovn/kube-ovn/commit/6924c4e6aac50f3f06f5b01b4d4956a7af82d8b3) fix container args (#4020)
 * [6b048eb49](https://github.com/kubeovn/kube-ovn/commit/6b048eb497bdb3b7692874bee3005899d2a3f88f) Append new DHCP options to existing options (#3997)
 * [18a477418](https://github.com/kubeovn/kube-ovn/commit/18a477418b7e57548078beb69aeac5cbbf54d8c1) Make dhcpOptions can accept multiple addresses (#4003)
 * [5994860b6](https://github.com/kubeovn/kube-ovn/commit/5994860b61742c53b72e5bfe16e6a0c743df600d) ci: bump k8s to v1.30.0 (#4019)
 * [b8d77e242](https://github.com/kubeovn/kube-ovn/commit/b8d77e24222630f73af7403bb09bbfd59a6f54c1) fix lsp not updated correctly when logical switch is changed (#4015)
 * [a57d04115](https://github.com/kubeovn/kube-ovn/commit/a57d0411566c517f2031ebb04c9facd9a353519d) base: set entrypoint to  dumb-init (#4018)
 * [67848da17](https://github.com/kubeovn/kube-ovn/commit/67848da173514561221325b95146c0ed26c6f718) fix: u2o dual stack uses the corresponding svc ip (#4013)
 * [35e37ad7e](https://github.com/kubeovn/kube-ovn/commit/35e37ad7eef6ef9ef2f2bba4113ef84ab927a98c) fix policy route not deleted after subnet is deleted (#4016)
 * [4d7ec749a](https://github.com/kubeovn/kube-ovn/commit/4d7ec749a6e7878fcd84a1b0dc9340aa6945d281) fix: Resolved the hidden issue with zombie processes (#4004)
 * [4d8c39199](https://github.com/kubeovn/kube-ovn/commit/4d8c39199cb1ebe7d59500f2a611bc9db1094593) docs: updated CHANGELOG.md (#4017)
 * [64452939c](https://github.com/kubeovn/kube-ovn/commit/64452939c9f4e56281c5db3bb31a1ad533b06108) fix lsp not updating addresses (#4011)
 * [41cb044c9](https://github.com/kubeovn/kube-ovn/commit/41cb044c9c15de6bc3919f03e6a358eda822c085) simplify file reading (#4010)
 * [aaf5736a8](https://github.com/kubeovn/kube-ovn/commit/aaf5736a8a3de45bc24861da07622a4044d46fb4) fix: close file (#4007)
 * [9d19242c6](https://github.com/kubeovn/kube-ovn/commit/9d19242c6e9fce48e2bd1a45b48db4a8573a95ce) fix controller-runtime logger not set (#4005)
 * [3b73df6e8](https://github.com/kubeovn/kube-ovn/commit/3b73df6e80e2b345d418768c272eb9678e979b06) update meeting info
 * [e2dee3e42](https://github.com/kubeovn/kube-ovn/commit/e2dee3e4278ebe87d600874e10bd7626e1e27f50) fix node gc (#3992)
 * [060231bf7](https://github.com/kubeovn/kube-ovn/commit/060231bf750b2bd686e77b213ee544cd5ceb1b66) build(deps): bump github.com/docker/docker (#4000)
 * [dd4de4ac1](https://github.com/kubeovn/kube-ovn/commit/dd4de4ac1b3ea6a00afa64c16700d2c061aa8b43) e2e: fix skip cases for k8s network conformance tests (#3994)
 * [5bcdf2878](https://github.com/kubeovn/kube-ovn/commit/5bcdf2878ee6e95c8a04d2a3adad1f90cb29d152) Disable pod use NodeSwitch subnet (#3991)
 * [8c548754c](https://github.com/kubeovn/kube-ovn/commit/8c548754c92d9f088ac34b621aa650652133df07) update roadmap
 * [0068ce3e7](https://github.com/kubeovn/kube-ovn/commit/0068ce3e7cf0d073dff85a1253e4542e43678f27) bump go to 1.22.3 (#3989)
 * [b64f9fe59](https://github.com/kubeovn/kube-ovn/commit/b64f9fe591817c1db46bc2fd690e52b2c3646435) subnet print vlan as vpc (#3983)
 * [5f6483871](https://github.com/kubeovn/kube-ovn/commit/5f64838711ec0c537e18db802693ce2328609362) Revert "build(deps): bump github.com/kubeovn/felix (#3984)" (#3988)
 * [e3c729cd5](https://github.com/kubeovn/kube-ovn/commit/e3c729cd5cdaba1e028e72f3e6885cf0bbed3d64) build(deps): bump github.com/onsi/ginkgo/v2 from 2.17.2 to 2.17.3 (#3985)
 * [3c82e51c9](https://github.com/kubeovn/kube-ovn/commit/3c82e51c9e193e048d7837942b1507a74715301e) build(deps): bump golangci/golangci-lint-action from 5 to 6 (#3986)
 * [33bda4f28](https://github.com/kubeovn/kube-ovn/commit/33bda4f286516cf15b4286447f4b91ade032db5d) build(deps): bump github.com/kubeovn/felix (#3984)
 * [63d0aade6](https://github.com/kubeovn/kube-ovn/commit/63d0aade6c9f201a6f3efec71b844e84334f8ec8) bump k8s to v1.30.0 (#3965)
 * [776bd8373](https://github.com/kubeovn/kube-ovn/commit/776bd83733ac1d84cf221fa29a012c491b2c5a40) remove vendor name info (#3978)
 * [81b40c30e](https://github.com/kubeovn/kube-ovn/commit/81b40c30ef51f14871b44c453c26e5437b60c3c5) support ovn eip nats dualstack (#3822)
 * [02f876fb9](https://github.com/kubeovn/kube-ovn/commit/02f876fb986d8f5ef47239c71572b8f81eb38aa4) docs: updated CHANGELOG.md (#3982)
 * [828402dff](https://github.com/kubeovn/kube-ovn/commit/828402dff10cffe24446fc774c964b8f4fe1bcb9) build(deps): bump google.golang.org/protobuf from 1.34.0 to 1.34.1 (#3981)
 * [6656a1fed](https://github.com/kubeovn/kube-ovn/commit/6656a1fed4cf5a671d843ef4ef8f31faba0bb6a6) build(deps): bump golang.org/x/sys from 0.19.0 to 0.20.0 (#3980)
 * [5fa123a44](https://github.com/kubeovn/kube-ovn/commit/5fa123a4482818f9dc751bd96baddcf28a8e041c) ipam: fix IPRangeList clone (#3979)
 * [8a38cfab8](https://github.com/kubeovn/kube-ovn/commit/8a38cfab81d12ee7a6596694c755737374fcebd6) build(deps): bump github.com/docker/docker (#3974)
 * [ef799c77b](https://github.com/kubeovn/kube-ovn/commit/ef799c77b4fae4d851b0bf233e9f32fdb6e4e665) chart: fix kubeVersion to allow for patterns that match sub versions (#3975)
 * [afdedc6a7](https://github.com/kubeovn/kube-ovn/commit/afdedc6a72102b10c7a0f6769aebe182fc13dea7) build(deps): bump github.com/osrg/gobgp/v3 from 3.25.0 to 3.26.0 (#3973)
 * [1ea18d8c9](https://github.com/kubeovn/kube-ovn/commit/1ea18d8c9da4ea03047dd9211da95713a97d4428) build(deps): bump github.com/onsi/gomega from 1.33.0 to 1.33.1 (#3972)
 * [b50a7ec26](https://github.com/kubeovn/kube-ovn/commit/b50a7ec26ec21c6995e75cb6a567e46f628e7952) build(deps): bump google.golang.org/protobuf from 1.33.0 to 1.34.0 (#3971)
 * [50b101b08](https://github.com/kubeovn/kube-ovn/commit/50b101b08b5bbc3ea1361cea78eda9db0c5199c1) docs: updated CHANGELOG.md (#3969)
 * [2e8556d50](https://github.com/kubeovn/kube-ovn/commit/2e8556d5050f3a84e1b608e6cd73035ff8480625) change subnet check using ip location to avoid ipam init err (#3962)
 * [fa2770bf2](https://github.com/kubeovn/kube-ovn/commit/fa2770bf2f136d06d45a2620fe727811376e2fd3) fix subnet acl with same net allow (#3961)
 * [dd9a25dbf](https://github.com/kubeovn/kube-ovn/commit/dd9a25dbf9cf4fc5f34a60c112ba42aebc6ccf2c) add patch permission for cni clusterrole (#3959)
 * [18d1a1558](https://github.com/kubeovn/kube-ovn/commit/18d1a1558080a8a23c48cd5a53d5e8f722152015) use jinjanator instead of j2cli (#3957)
 * [7cddf7f7d](https://github.com/kubeovn/kube-ovn/commit/7cddf7f7d08458e0b83de3184acf5171c70f0ee6) fix index out of range (#3958)
 * [4c5061961](https://github.com/kubeovn/kube-ovn/commit/4c50619616e5e51d613cb01f3cb0388ae94ed35c) Fix finalizer (#3955)
 * [77f011c09](https://github.com/kubeovn/kube-ovn/commit/77f011c0968f080bd5458aec6fc536fbd9050855) build(deps): bump golangci/golangci-lint-action from 4 to 5 (#3952)
 * [e56a6367d](https://github.com/kubeovn/kube-ovn/commit/e56a6367d7ac7178660fd342b30c5d4ac452441b) fix nil pointer dereference (#3951)
 * [7e0afbd72](https://github.com/kubeovn/kube-ovn/commit/7e0afbd729caa3587b768854bc04e2ac43f79c9e) build(deps): bump helm/kind-action from 1.9.0 to 1.10.0 (#3949)
 * [c0a08ab3a](https://github.com/kubeovn/kube-ovn/commit/c0a08ab3a8e47bc3a49b648ed4a7c67713a6696b) build(deps): bump github.com/docker/docker (#3948)
 * [609559bb6](https://github.com/kubeovn/kube-ovn/commit/609559bb6f901dd837c55a845f3dcc1fcc2285f0) rename templates
 * [ff83a0e16](https://github.com/kubeovn/kube-ovn/commit/ff83a0e16236ca0ac60d2243896632d409d941e4) add issue template config
 * [f99cd2b36](https://github.com/kubeovn/kube-ovn/commit/f99cd2b3695fc6c3088d285d96142ea35fba30ea) modify issue templates
 * [a44335253](https://github.com/kubeovn/kube-ovn/commit/a44335253ff4caba20c30b17cea4724374045e54) check kube ovn podrestarts (#3925)
 * [015564d34](https://github.com/kubeovn/kube-ovn/commit/015564d3499ad1422a6e24606eca147d827810c3) docs: updated CHANGELOG.md (#3946)
 * [152d54da3](https://github.com/kubeovn/kube-ovn/commit/152d54da35f8a5e161cc2920dda2d5d6e018f63f) fix: lower camel case (#3942)
 * [740e01f4b](https://github.com/kubeovn/kube-ovn/commit/740e01f4bfe94eda561446b560b6887f1159b50c) build(deps): bump github.com/Microsoft/hcsshim from 0.12.2 to 0.12.3 (#3944)
 * [cd9df5d26](https://github.com/kubeovn/kube-ovn/commit/cd9df5d26352e4f335dd51f4e991300359f2f3fd) drop both IPv4 and IPv6 traffic in networkpolicy drop acl (#3940)
 * [cb6f22992](https://github.com/kubeovn/kube-ovn/commit/cb6f22992a544cc964b1ebe5d3424ca15efa954a) build(deps): bump github.com/onsi/gomega from 1.32.0 to 1.33.0 (#3938)
 * [7ffb791ae](https://github.com/kubeovn/kube-ovn/commit/7ffb791aeeb6dd443047bd8fca16f5b960021a48) add metric for subnet info (#3932)
 * [ce5c01704](https://github.com/kubeovn/kube-ovn/commit/ce5c01704e15edf48b4ab8c8c4a73168e7558973) build(deps): bump github.com/docker/docker (#3935)
 * [05b2d5b58](https://github.com/kubeovn/kube-ovn/commit/05b2d5b58c68693cbeb73a4ccbcb8a7e281162b1) cni-server: set sysctl variables only when the env variables are passed in (#3929)
 * [6b92ab638](https://github.com/kubeovn/kube-ovn/commit/6b92ab638d3c27fb5998a51a62d473b0c48a41bb) ovn: check whether db file is fixed (#3928)
 * [b1d87f172](https://github.com/kubeovn/kube-ovn/commit/b1d87f172b8ad216fae6720fa5f961c380354d12) bump k8s to v1.29.4 (#3927)
 * [a5bf8612e](https://github.com/kubeovn/kube-ovn/commit/a5bf8612e28d110c981f7844ece5df9349e90986) append filepath when compare cni config (#3923)
 * [f6534cf24](https://github.com/kubeovn/kube-ovn/commit/f6534cf2419324cbb1c8732c4ec6e06e9b1243b6) build(deps): bump azure/setup-helm from 4.1.0 to 4.2.0 (#3926)
 * [1d693287b](https://github.com/kubeovn/kube-ovn/commit/1d693287b90133387844a804370092939e35f757) cni: add mtu in add result (#3922)
 * [8cc1a0f73](https://github.com/kubeovn/kube-ovn/commit/8cc1a0f730beadfea0db4fc8f6429596cfb1ed65) build(deps): bump kubevirt.io/api from 1.1.1 to 1.2.0 (#3918)
 * [23c8df489](https://github.com/kubeovn/kube-ovn/commit/23c8df489ebf133f2a6dcc56706db5ea802a39e6) re calculate subnet using ips while inconsistency detected (#3920)
 * [f536d4805](https://github.com/kubeovn/kube-ovn/commit/f536d4805939d879bd3d6b16c951fba9c2653323) update ovn monitor (#3903)
 * [f81256213](https://github.com/kubeovn/kube-ovn/commit/f81256213594c62e54c254f6c64896d3af9bdccd) add monitor for sysctl para (#3913)
 * [98580d68e](https://github.com/kubeovn/kube-ovn/commit/98580d68e876977623ebdfa557c939ac3f1ef008) update form link
 * [44466906d](https://github.com/kubeovn/kube-ovn/commit/44466906dd917cefc9a889579a5fa1111b5a0dbb) build(deps): bump github.com/containernetworking/cni (#3917)
 * [fe157d3df](https://github.com/kubeovn/kube-ovn/commit/fe157d3df185c693304402f9176ccee764b01405) refactor kubevirt vm e2e (#3914)
 * [062a55881](https://github.com/kubeovn/kube-ovn/commit/062a5588162ed0a3938c49c90382c96f54ca785e) fix subnet provider validation (#3915)
 * [748aac73a](https://github.com/kubeovn/kube-ovn/commit/748aac73a9ef231e8218c7ff764081171f36ebae) bump go toolchain to go1.22.2 (#3905)
 * [f5aef5a7d](https://github.com/kubeovn/kube-ovn/commit/f5aef5a7d1547154b57e0b1caad50e03281901cd) build(deps): bump github.com/k8snetworkplumbingwg/network-attachment-definition-client from 1.6.0 to 1.7.0 (#3906)
 * [52fed946b](https://github.com/kubeovn/kube-ovn/commit/52fed946bc26733747ac7f7d8107fab740f909c3) build(deps): bump github.com/docker/docker (#3907)
 * [85ac74124](https://github.com/kubeovn/kube-ovn/commit/85ac74124e889b571187bce0c358ba2ddc32a9cf) support specifying routes when providing IPAM for other CNI plugins (#3904)
 * [e0fffa6b5](https://github.com/kubeovn/kube-ovn/commit/e0fffa6b53b3d61b384700b50d0faa5ef1a7cebb) fix br-external not init because of  no permission after ovn-nat-gw configmap created (#3902)
 * [a6e9ef461](https://github.com/kubeovn/kube-ovn/commit/a6e9ef461b3a353294d3d6ab549cafa7f1ea2d22) build(deps): bump sigs.k8s.io/controller-runtime from 0.17.2 to 0.17.3 (#3901)
 * [629c9df59](https://github.com/kubeovn/kube-ovn/commit/629c9df59ec4df94108d99a58ea59166b870433a) build(deps): bump google.golang.org/grpc from 1.63.0 to 1.63.2 (#3900)
 * [859e109b5](https://github.com/kubeovn/kube-ovn/commit/859e109b522b125c45dd7b0b1f6b1cb9be3574eb) Fix init sg (#3890)
 * [2fa5df222](https://github.com/kubeovn/kube-ovn/commit/2fa5df222b927fcc6f0a6a1319818db5cd364e30) distinguish portSecurity with security group (#3862)
 * [28aa65d18](https://github.com/kubeovn/kube-ovn/commit/28aa65d18fb1e9fa44d1a4faee1eca130110d7ce) skip conntrack when access node dns ip (#3894)
 * [019bacd36](https://github.com/kubeovn/kube-ovn/commit/019bacd361e0d54ee65acf025328b68a46a75236) build(deps): bump github.com/osrg/gobgp/v3 from 3.24.0 to 3.25.0 (#3897)
 * [f983f5b49](https://github.com/kubeovn/kube-ovn/commit/f983f5b497ffc2a3476242c9340ef6a9f696c262) build(deps): bump golang.org/x/sys from 0.18.0 to 0.19.0 (#3896)
 * [b292e01f8](https://github.com/kubeovn/kube-ovn/commit/b292e01f861530ebde0bf875bf9c014ade227613) build(deps): bump golang.org/x/mod from 0.16.0 to 0.17.0 (#3895)
 * [9207cbab2](https://github.com/kubeovn/kube-ovn/commit/9207cbab29f0dcd62cd41a9bb66f0655ac3b8484) build(deps): bump google.golang.org/grpc from 1.62.1 to 1.63.0 (#3898)
 * [b7a2c0a83](https://github.com/kubeovn/kube-ovn/commit/b7a2c0a83d16ab4885a720faaaf96f7046637d2f) chart: fix ovs-ovn update strategy (#3887)
 * [a48c597eb](https://github.com/kubeovn/kube-ovn/commit/a48c597eb610e6d499aa56ca65abaf070f38962d) fix ic e2e (#3831)
 * [f8dee83c1](https://github.com/kubeovn/kube-ovn/commit/f8dee83c1af8adbacd2f18a93a7b713dbf6db3ec) build(deps): bump github.com/Microsoft/hcsshim from 0.12.1 to 0.12.2 (#3891)
 * [64f6f2ba6](https://github.com/kubeovn/kube-ovn/commit/64f6f2ba6cf670e56c124af0bb931f6be6feeb64) Fix index out of range in controller security_group (#3845)
 * [8d17662fe](https://github.com/kubeovn/kube-ovn/commit/8d17662fe74e137c0135d3bafc5b56e5cbfa04a3) fix: ipam invalid memory address or nil pointer dereference (#3889)
 * [d954b6358](https://github.com/kubeovn/kube-ovn/commit/d954b63580e7e71599cfe740ed7857b1a960cac6) add tracepath (#3884)
 * [e1a8a39bd](https://github.com/kubeovn/kube-ovn/commit/e1a8a39bd7e5c66bcc4a9de8f521e5ec3a078cd4) change northd probe interval to 5s (#3885)
 * [31968e810](https://github.com/kubeovn/kube-ovn/commit/31968e8100f1364a4928b210bf9d0bb6573eba18) docs: updated CHANGELOG.md (#3883)
 * [57f08ad94](https://github.com/kubeovn/kube-ovn/commit/57f08ad94f662cfe01bb21812cc3662fc4cbbf51) ovn: reduce down time during upgrading from version 21.06 (#3881)
 * [3328b9012](https://github.com/kubeovn/kube-ovn/commit/3328b901273d3103cacf0b5049ba46d42d08b941) compile binaries with debug symbols for debug images (#3871)
 * [b9116899c](https://github.com/kubeovn/kube-ovn/commit/b9116899c47c13a65451e7c7476551b0db6892ce) ovn: update patch for skipping ct (#3879)
 * [74fafdb62](https://github.com/kubeovn/kube-ovn/commit/74fafdb624987afaf29d2a76deb212b8933a6c14) docs: updated CHANGELOG.md (#3877)
 * [f1bbab62a](https://github.com/kubeovn/kube-ovn/commit/f1bbab62a8b80692c3a04fc6f37ede7331800ce5) build(deps): bump github.com/cenkalti/backoff/v4 from 4.2.1 to 4.3.0 (#3875)
 * [40614b5a3](https://github.com/kubeovn/kube-ovn/commit/40614b5a3da72a748b622a72e2aea27c8cf56775) ci: fix memory leak reporting caused by ovn-controller crashes (#3873)
 * [982b13688](https://github.com/kubeovn/kube-ovn/commit/982b13688bfbca9fc4e46c5ba9d5333499b01453) fix reference to nil pointer (#3872)
 * [1173594e2](https://github.com/kubeovn/kube-ovn/commit/1173594e2887bc66cffa7eeceb6d30a55d619a78) build(deps): bump github.com/onsi/ginkgo/v2 from 2.17.0 to 2.17.1 (#3866)
 * [485d07f6f](https://github.com/kubeovn/kube-ovn/commit/485d07f6f5fc8b4137ad3fecdfe84434e6c5e265) build(deps): bump github.com/Microsoft/hcsshim from 0.12.0 to 0.12.1 (#3867)
 * [b48c519f7](https://github.com/kubeovn/kube-ovn/commit/b48c519f73a839713c92b8d2bbe01a8e8fd18743) fix: when give ipv4 cidr ipv6 gateway, the gateway will extend infinitely (#3865)
 * [fbec723a4](https://github.com/kubeovn/kube-ovn/commit/fbec723a4c989540332cd1530a583f7ff7afced4) docs: updated CHANGELOG.md (#3864)
 * [54c95e484](https://github.com/kubeovn/kube-ovn/commit/54c95e484d6852748b014d0f158135f4a2bc5ffa) exclude vip as encap ip (#3859)
 * [2f48cc7f5](https://github.com/kubeovn/kube-ovn/commit/2f48cc7f58794f0bf63ba4f41b004b5cf201ba47) docs: updated CHANGELOG.md (#3858)
 * [05c67d87c](https://github.com/kubeovn/kube-ovn/commit/05c67d87cf74535387607c256285f74508ed5bdf) fix: aap get wrong lsp when using multus (#3827)
 * [cef6f0e92](https://github.com/kubeovn/kube-ovn/commit/cef6f0e92dd957000391a1f2eac0590cf3454df2) chart: fix missing ENABLE_IC (#3851)
 * [a4d5c06b9](https://github.com/kubeovn/kube-ovn/commit/a4d5c06b93394a570e1deefd18960225d234a250) delete unused mount volumes (#3838)
 * [d3e477f44](https://github.com/kubeovn/kube-ovn/commit/d3e477f44a8815690215e9f3303d6d6987deae41) bump k8s to v1.29.3 (#3833)
 * [07ac371a2](https://github.com/kubeovn/kube-ovn/commit/07ac371a2239eb91ba7db3963aae07d0c370e120) iptables: reject access to invalid service port only for TCP (#3843)
 * [eac020818](https://github.com/kubeovn/kube-ovn/commit/eac020818f998e7b6ca4cd7ecadfde7fe63677d0) ci: bump helm to v3.14.3 (#3829)
 * [d019ee273](https://github.com/kubeovn/kube-ovn/commit/d019ee27317faf368491a4440eaf600bf59a0cdd) build(deps): bump github.com/docker/docker (#3854)
 * [c5c121d53](https://github.com/kubeovn/kube-ovn/commit/c5c121d53e4905910ec773f1a8a67a162a48b59e) build(deps): bump github.com/docker/docker (#3849)
 * [b9810c80e](https://github.com/kubeovn/kube-ovn/commit/b9810c80eaecac182f6ea9167cbdbb46a89f4d09) u2o: add policy route for u2o when disabling lb (#3813)
 * [18d88d93e](https://github.com/kubeovn/kube-ovn/commit/18d88d93ec662a6274b47769b9b486ae45ab88c1) bump github.com/onsi/ginkgo/v2 to v2.17.0 (#3837)
 * [aaf1464f2](https://github.com/kubeovn/kube-ovn/commit/aaf1464f2e3b8508e96aff23a04413b693a1bb06) fix ip is a substring in node name (#3832)
 * [b28663b58](https://github.com/kubeovn/kube-ovn/commit/b28663b58cba265591d8de0e1eabb4fb495b6582) base: bump cni plugins and kubectl (#3826)
 * [b65941147](https://github.com/kubeovn/kube-ovn/commit/b65941147949a2af90fd1ec61f4fa83bf30d84ad) add ovs flow num dashboard (#3821)
 * [73e1c42d1](https://github.com/kubeovn/kube-ovn/commit/73e1c42d175db04c92d3c44dae800dbb7d03f0e6) build(deps): bump github.com/containernetworking/plugins (#3825)
 * [ad025ff1a](https://github.com/kubeovn/kube-ovn/commit/ad025ff1a663538882e7e85f7eb36aea62f35c59) build(deps): bump github.com/emicklei/go-restful/v3 (#3824)
 * [d29d9b36f](https://github.com/kubeovn/kube-ovn/commit/d29d9b36f1f97432f4d64b6936f7eb8f46717da3) build(deps): bump github.com/prometheus-community/pro-bing (#3820)
 * [be6cc86d9](https://github.com/kubeovn/kube-ovn/commit/be6cc86d9543f821e76bb823c2c2764fad567d13) fix cmd inject (#3809)
 * [a2977e47f](https://github.com/kubeovn/kube-ovn/commit/a2977e47fe74f2e4b14a7b06bf678baa98261c8c) kubectl-ko: fix subnet diagnose failure (#3808)
 * [f745776e1](https://github.com/kubeovn/kube-ovn/commit/f745776e1bceb55d9a3cd7e2771f14c0fe9fe1de) Makefile: fix clab bgp configuration (#3818)
 * [0314e82d6](https://github.com/kubeovn/kube-ovn/commit/0314e82d68db54b5f1ad24799d61cf7bbc257d2b) Makefile: fix cilium installation (#3817)
 * [a8dc9ab95](https://github.com/kubeovn/kube-ovn/commit/a8dc9ab953cb94a80bfeb1ba4f5c708f86e16301) add sts e2e case for pr (#3800)
 * [9e3fdabf3](https://github.com/kubeovn/kube-ovn/commit/9e3fdabf39eaed64a0e6771e635b643a94aef0ea) build(deps): bump github.com/docker/docker (#3811)
 * [61b6b5e40](https://github.com/kubeovn/kube-ovn/commit/61b6b5e403ddf8f9f2d700e1d08a836aab786a11) pinger: do not setup metrics if disabled (#3806)
 * [4f37bbcab](https://github.com/kubeovn/kube-ovn/commit/4f37bbcabbecff1370f031fba040be602b38e4a4) remove unused environment variables (#3804)
 * [4a9a1f821](https://github.com/kubeovn/kube-ovn/commit/4a9a1f8214223f273850bb53b19c82407de23621) Fix the failure to enable multi-network card traffic mirroring for newly created pods (#3805)
 * [a4c022809](https://github.com/kubeovn/kube-ovn/commit/a4c022809a2c53719ac553f021b1af174d4efada) build(deps): bump google.golang.org/protobuf from 1.32.0 to 1.33.0 (#3802)
 * [307c05dd7](https://github.com/kubeovn/kube-ovn/commit/307c05dd73b9bf1e29009d3a0a467e3f8949f154) build(deps): bump google.golang.org/grpc from 1.62.0 to 1.62.1 (#3803)
 * [b0678af05](https://github.com/kubeovn/kube-ovn/commit/b0678af053de33b39e703746fef4684a06c811c6) build(deps): bump github.com/onsi/ginkgo/v2 from 2.15.0 to 2.16.0 (#3797)
 * [7dda62d6f](https://github.com/kubeovn/kube-ovn/commit/7dda62d6fa8c763fbd382ca94bd72429519cf9fc) build(deps): bump github.com/Microsoft/hcsshim from 0.11.4 to 0.12.0 (#3796)
 * [116c24caa](https://github.com/kubeovn/kube-ovn/commit/116c24caace0e78620d78f80fd73055165cbc5c1) VM live migrate (#3767)
 * [5afcc3ec6](https://github.com/kubeovn/kube-ovn/commit/5afcc3ec69824ee6f5019dcb619fd6ac2e42aebb) refactor fastpath (#3794)
 * [7b937cb6b](https://github.com/kubeovn/kube-ovn/commit/7b937cb6b4f648836ad4b558236b9f50ca0adcc1) add missing metallb-cr.yaml.j2
 * [3ee81e23a](https://github.com/kubeovn/kube-ovn/commit/3ee81e23a3cf607751c4a28b3d6aa357408e0efd) Makefile: add target kind-install-metallb (#3795)
 * [e08e3a2c1](https://github.com/kubeovn/kube-ovn/commit/e08e3a2c1cafe9a7f21132c0c6281df988b0c9fb) fix: go toolchain config (#3790)
 * [31e0c8d51](https://github.com/kubeovn/kube-ovn/commit/31e0c8d51f220a638b48806f7b8b49513008aea6) build(deps): bump golang.org/x/sys from 0.17.0 to 0.18.0 (#3792)
 * [0951b36ae](https://github.com/kubeovn/kube-ovn/commit/0951b36aecea1cf412fd81358f7908dad3a63b77) build(deps): bump golang.org/x/mod from 0.15.0 to 0.16.0 (#3793)
 * [498a62de6](https://github.com/kubeovn/kube-ovn/commit/498a62de6ff8d73dfe191a5cb77a33f4f0a8c446) build(deps): bump azure/setup-helm from 4.0.0 to 4.1.0 (#3791)
 * [47ea57b72](https://github.com/kubeovn/kube-ovn/commit/47ea57b72666dd928aaa2850cdc110b11a0c86c7) Refactor build-go targets to use -trimpath flag (#3789)
 * [3e8329f7f](https://github.com/kubeovn/kube-ovn/commit/3e8329f7f61211993aa53f0cc973927df6707fd9) fix incorrect variable assignment (#3787)
 * [e264ed841](https://github.com/kubeovn/kube-ovn/commit/e264ed8414ea99dc76e198df23a201b0f9ac845e) Makefile: bump deepflow to v6.4 (#3783)
 * [b2a6b3e97](https://github.com/kubeovn/kube-ovn/commit/b2a6b3e977179751d03aea0a414fe50e928a8e8f) bump github.com/osrg/gobgp/v3 to v3.24.0 (#3785)
 * [25ebadc4e](https://github.com/kubeovn/kube-ovn/commit/25ebadc4e981536dcbd8a4cd4c0cebc463cfd113) build(deps): bump github.com/stretchr/testify from 1.8.4 to 1.9.0 (#3784)
 * [78234970e](https://github.com/kubeovn/kube-ovn/commit/78234970e8981501817aca3deb6e6b8da5c7afc9) docs: updated CHANGELOG.md (#3782)
 * [83ae77ff5](https://github.com/kubeovn/kube-ovn/commit/83ae77ff526d0891ce1cb3eb18d076fee3e02a5b) docs: updated CHANGELOG.md (#3779)
 * [da6b52213](https://github.com/kubeovn/kube-ovn/commit/da6b522133d61c4c28c77176856acc84c3b1a35f) fix sts pod's logical switch port do not update externa_id vendor and ls (#3777)
 * [aa6e36d0d](https://github.com/kubeovn/kube-ovn/commit/aa6e36d0d45a5bbb2d34e54f7c05baf73ea8a9a9) optimize nat gw (#3775)
 * [6037ee83a](https://github.com/kubeovn/kube-ovn/commit/6037ee83a0b4478e542097030f7289c84e4eca53) update files generated by k8s codegen (#3771)
 * [e85263fee](https://github.com/kubeovn/kube-ovn/commit/e85263fee0122d8f3ec25aba78875406e0e75d61) fix dot imports (#3773)
 * [ebb0f347c](https://github.com/kubeovn/kube-ovn/commit/ebb0f347c08bf3fe96811b0e0c9528b1a1de894c) fix crd (#3772)
 * [eeda6dcd4](https://github.com/kubeovn/kube-ovn/commit/eeda6dcd4b948ebec842932b7335e385faf94812) build(deps): bump github.com/emicklei/go-restful/v3 (#3768)
 * [6280d42e8](https://github.com/kubeovn/kube-ovn/commit/6280d42e87b7bad45bcf204d8fb8800d403ae5fc) docs: updated CHANGELOG.md (#3766)
 * [6e292a89b](https://github.com/kubeovn/kube-ovn/commit/6e292a89bd31602eee60785948f52bb58fc2d6e4) refactor ovn clusterrole (#3755)
 * [924d99192](https://github.com/kubeovn/kube-ovn/commit/924d9919236b4da2e3f308110f1a1917337dbc1f) e2e: fix subnet finalizers validation (#3765)
 * [5520490cd](https://github.com/kubeovn/kube-ovn/commit/5520490cd6c3503891bcfe6c7e4902d98bd304d9) kube-ovn-controller: fix finalizers migration (#3764)
 * [461d353c2](https://github.com/kubeovn/kube-ovn/commit/461d353c2587bafea4de0d04824a66fd0ab05662) remove trailing spaces (#3751)
 * [2a804103a](https://github.com/kubeovn/kube-ovn/commit/2a804103a109f3e1f82e0d9831fb9a0536fd8f01) if startOVNIC firstly, and setazName secondly, the ovn-ic-db may sync the old azname (#3759)
 * [21e821e7c](https://github.com/kubeovn/kube-ovn/commit/21e821e7cbcbc97dcc930e15c7e17429b5ea29a0) Add missing commas, correction of spelling errors (#3761)
 * [608d032da](https://github.com/kubeovn/kube-ovn/commit/608d032da9af37957bbf2cdda360171e42f30b6f) kube-ovn-controller: optimize finalizers migration (#3754)
 * [26c0affa0](https://github.com/kubeovn/kube-ovn/commit/26c0affa00c1ee88d08f46c40d34ddcb53dc0c9d) build(deps): bump google.golang.org/grpc from 1.61.1 to 1.62.0 (#3757)
 * [aa7d0d4c6](https://github.com/kubeovn/kube-ovn/commit/aa7d0d4c6884fd92b4c868077d054178ff8a1ca0) build(deps): bump github.com/parnurzeal/gorequest from 0.2.16 to 0.3.0 (#3756)
 * [d631f9de1](https://github.com/kubeovn/kube-ovn/commit/d631f9de114abe5aa696f194d45c1c568f77d7c3) log near err (#3753)
 * [d739b4121](https://github.com/kubeovn/kube-ovn/commit/d739b41217c44c123ca3c809212a0687e1f00c9b) fix: should use domain-qualified finalizer name (#3748)
 * [0405b92eb](https://github.com/kubeovn/kube-ovn/commit/0405b92eb6faa9177d51d4c6805e02eaada72481) Fix slr (#3746)
 * [fd3ae1d1c](https://github.com/kubeovn/kube-ovn/commit/fd3ae1d1c478afe5e67e0aa159bd9cd384d6819f) add helper script for release (#3752)
 * [26d7a9c45](https://github.com/kubeovn/kube-ovn/commit/26d7a9c4525fba43d5a9dbad2974a876f0047cee) docs: updated CHANGELOG.md (#3750)
 * [4594a2540](https://github.com/kubeovn/kube-ovn/commit/4594a2540881d69c34ff945c2cd8a51d1b1aac80) ovn-ic-server recover wait longer time (#3749)
 * [882c64ead](https://github.com/kubeovn/kube-ovn/commit/882c64ead05121d49690d8fe714896105520abdc) fix kubevirt vm e2e (#3747)
 * [71fa9c48c](https://github.com/kubeovn/kube-ovn/commit/71fa9c48c1da7b2320248ee6bc73c0c901d89e25) ci: bump azure/setup-helm to v4.0.0 (#3743)
 * [63f2c9964](https://github.com/kubeovn/kube-ovn/commit/63f2c9964b3e08c393ea4bb08b52f9544032dfc5) base: install libmnl0 instead of libmnl-dev (#3745)
 * [835e787ba](https://github.com/kubeovn/kube-ovn/commit/835e787ba071d53ecc6c3ed7bbc6d2d7af5d38c7) ci: collect ko logs for all kind clusters (#3744)
 * [e2ec8f118](https://github.com/kubeovn/kube-ovn/commit/e2ec8f118ebc663fa2d6c174a31c9eb090c91d5c) ci: fix ovn ic log file name (#3742)
 * [2217162d2](https://github.com/kubeovn/kube-ovn/commit/2217162d2b248bdf8552496f6c6fc0e21d072bb8) ci: bump cilium to v1.15.1 (#3735)
 * [b0f0bb71c](https://github.com/kubeovn/kube-ovn/commit/b0f0bb71cd72c017d32aaa1af779f9bf1e845464) remove invalid ovs build option (#3733)
 * [f95ca28fb](https://github.com/kubeovn/kube-ovn/commit/f95ca28fb0754e57477933f49cbc0cd3530a8211) add directory to charts (#3734)
 * [457e6bafb](https://github.com/kubeovn/kube-ovn/commit/457e6bafbbf23786ff02bad0899db47bfffcebce) bump k8s to v1.29.2 (#3725)
 * [8228a1e21](https://github.com/kubeovn/kube-ovn/commit/8228a1e21fe42d569439bcd6eaacf908966eafda) dpdk: remove unnecessary ovn patch (#3736)
 * [cc935bd79](https://github.com/kubeovn/kube-ovn/commit/cc935bd792eaaea49a9fa5a261778b322d83d9c7) remove util.UniqString() and util.ContainsString() (#3732)
 * [9abe9217f](https://github.com/kubeovn/kube-ovn/commit/9abe9217f5df24cc25756f28e1611754f7a09d96) fix kubevirt e2e (#3729)
 * [d265d7844](https://github.com/kubeovn/kube-ovn/commit/d265d7844d8a217d04b699cc18c3b871bc5726af) bump libovsdb (#3731)
 * [b98d6cee4](https://github.com/kubeovn/kube-ovn/commit/b98d6cee4c9c4adf23d3fd4aee1c01814a4c6eec) Makefile: bump kwok to v0.5.1 (#3730)
 * [7891205a3](https://github.com/kubeovn/kube-ovn/commit/7891205a3d2426f617eeef2716ef8a11b7f92e1f) fix typo (#3727)
 * [a7e70db93](https://github.com/kubeovn/kube-ovn/commit/a7e70db93fccd9bbedfd19b0a40f936d2397bb2d) update chart relase action workflow (#3728)
 * [3552db384](https://github.com/kubeovn/kube-ovn/commit/3552db38497ee97838b4c737ea515d2c218c422d) Fix: Resolve issue with skipped execution of sg annotations (#3700)
 * [28781c373](https://github.com/kubeovn/kube-ovn/commit/28781c37344d301ad331e803d4fed631838fded6) remove copilot4pr as the experiment ended
 * [8334fb3ac](https://github.com/kubeovn/kube-ovn/commit/8334fb3acf831b09e8ada81486d74e6c85844536) install.sh: wait ovn-ic-controller to be ready (#3704)
 * [704168dec](https://github.com/kubeovn/kube-ovn/commit/704168dec3032e48a5703871944a80be4b1acc12) fix: all gw nodes (#3724)
 * [40230ef74](https://github.com/kubeovn/kube-ovn/commit/40230ef74db8f89a154c599fe1c24cb94fc50e06) bump go to v1.22 (#3718)
 * [2a0a0be1f](https://github.com/kubeovn/kube-ovn/commit/2a0a0be1f545b5f88179ab71b8a748d41d248dbb) fix lint errors (#3719)
 * [01999c632](https://github.com/kubeovn/kube-ovn/commit/01999c6323838271a6184c6fd5b3a70f6dcf05cb) ovn: remove unnecessary patch (#3720)
 * [77db8f1b7](https://github.com/kubeovn/kube-ovn/commit/77db8f1b7edabb8613562eafd5c7103d04103c12) Add Ænix to the Adopters list (#3711)
 * [0e44d5e7a](https://github.com/kubeovn/kube-ovn/commit/0e44d5e7a2eb890591d88cfcfd705ba4563db20d) build(deps): bump golangci/golangci-lint-action from 3 to 4 (#3712)
 * [64ee14c52](https://github.com/kubeovn/kube-ovn/commit/64ee14c521235698191e46001ea754ec365a4f6e) build(deps): bump helm/kind-action from 1.8.0 to 1.9.0 (#3713)
 * [d7ad7f2d1](https://github.com/kubeovn/kube-ovn/commit/d7ad7f2d16d35a26fd18ff8010467c933b8d9fcd) build(deps): bump github.com/docker/docker (#3705)
 * [2e2fa7daa](https://github.com/kubeovn/kube-ovn/commit/2e2fa7daaa37cd3f07b08dbba26bb80b7a2b4be1) build(deps): bump golang.org/x/mod from 0.14.0 to 0.15.0 (#3706)
 * [a28af4aa1](https://github.com/kubeovn/kube-ovn/commit/a28af4aa1459812c3a194805dd267f7270526a93) fix: ip delete use wrong key, ip delete only once (#3698)
 * [d3ef8922a](https://github.com/kubeovn/kube-ovn/commit/d3ef8922aa27f44b9ba762c6d345f0d4ea6414fb) fix typo
 * [54f72e32c](https://github.com/kubeovn/kube-ovn/commit/54f72e32c0c28f5d0f3dd41362e7d1e89dc36c65) ci: bump kind to v0.21.0 (#3694)
 * [b6b1fccc3](https://github.com/kubeovn/kube-ovn/commit/b6b1fccc3a988ede4fb8e2b96f5d7d1ad6c4f1fb) update chart release workflow (#3691)
 * [36eba82e0](https://github.com/kubeovn/kube-ovn/commit/36eba82e0316b38faa865ff5d4b33c141860ac79) remove unused (#3696)
 * [a487f6108](https://github.com/kubeovn/kube-ovn/commit/a487f61081f4034b4a739a8a7d63e75f57053d82) kube-ovn-controller: remove unused codes (#3692)
 * [f2be2d58f](https://github.com/kubeovn/kube-ovn/commit/f2be2d58f3ce834e9140482e65624154ef2d7bb3) fix: security group base acl direction (#3690)
 * [0270012a7](https://github.com/kubeovn/kube-ovn/commit/0270012a704ab656f565092e67fc5567f82bf42f) remove fip controller (#3684)
 * [3716edd4a](https://github.com/kubeovn/kube-ovn/commit/3716edd4a08db4699567f5ebc511a04d41a92e3b) build(deps): bump github.com/osrg/gobgp/v3 from 3.22.0 to 3.23.0 (#3688)
 * [24acf0463](https://github.com/kubeovn/kube-ovn/commit/24acf046310079a11c7b4a7b7eb82ecf5218f972) build(deps): bump github.com/docker/docker (#3689)
 * [028e7ab36](https://github.com/kubeovn/kube-ovn/commit/028e7ab3602a33a06ae04dc810b92e13f0f63f73) build(deps): bump peter-evans/create-pull-request from 5 to 6 (#3685)
 * [c797ccd14](https://github.com/kubeovn/kube-ovn/commit/c797ccd1450f11312b6780ab0e1962ec5c7aefa4) build(deps): bump github.com/opencontainers/runc from 1.1.10 to 1.1.12 (#3686)
 * [0d6b4adda](https://github.com/kubeovn/kube-ovn/commit/0d6b4adda75c4a52961914e215d61cb80e974f2e) Compatible with controller deployment methods before kube-ovn 1.11.16 (#3677)
 * [6db8c840b](https://github.com/kubeovn/kube-ovn/commit/6db8c840b159e0d710487ff1c0b80ed7118eba8e) Support ip creation (#3667)
 * [38db6c34d](https://github.com/kubeovn/kube-ovn/commit/38db6c34d889f90968054e98ceae93fda4377498) set after genev_sys_6081 started (#3680)
 * [a79551c01](https://github.com/kubeovn/kube-ovn/commit/a79551c016201ca138a607e750ee0eb7133f3d61) ovn: add nb option version_compatibility (#3671)
 * [18f8f8ce4](https://github.com/kubeovn/kube-ovn/commit/18f8f8ce4ee71d5e58eafeea22856baaedf992bc) Makefile: fix install/upgrade chart (#3678)
 * [b0e813aa7](https://github.com/kubeovn/kube-ovn/commit/b0e813aa7856a2a0947d02a03cd3606f84aa7529) build(deps): bump github.com/evanphx/json-patch/v5 from 5.8.1 to 5.9.0 (#3676)
 * [f11820f8c](https://github.com/kubeovn/kube-ovn/commit/f11820f8cbc6baf6be77882fe9afbd1c0fc84527) ovn: do not send direct traffic between lports to conntrack (#3663)
 * [8f2cef01b](https://github.com/kubeovn/kube-ovn/commit/8f2cef01b6d6b37dff1d00175f722664ef9cd894) ovn-ic-ecmp refactor (#3632)
 * [ab4f1d861](https://github.com/kubeovn/kube-ovn/commit/ab4f1d8611236f407944e20dd084df458a565b56) fix getting vip with an empty name (#3673)
 * [5239bda19](https://github.com/kubeovn/kube-ovn/commit/5239bda19438b3b324d04ee8880614642289d61f) build(deps): bump dorny/paths-filter from 2 to 3 (#3672)
 * [0c82109ce](https://github.com/kubeovn/kube-ovn/commit/0c82109cefbca4f0aa5e92218957487b8fc1ad09) build(deps): bump github.com/docker/docker (#3670)
 * [c11ce4d58](https://github.com/kubeovn/kube-ovn/commit/c11ce4d5837d0eccc315ac5bcbbe3d9da5256184) replace github.com/golang/mock with go.uber.org/mock/mockgen (#3664)
 * [82986a64b](https://github.com/kubeovn/kube-ovn/commit/82986a64b14db83c0de920e789595601700a9930) build(deps): bump google.golang.org/grpc from 1.60.1 to 1.61.0 (#3669)
 * [01aabdb65](https://github.com/kubeovn/kube-ovn/commit/01aabdb650d9f46e67eb3d4681b4b70e0308ea05) fix 409 (#3662)
 * [0667fc009](https://github.com/kubeovn/kube-ovn/commit/0667fc009ef01c0605df6215c2c505e4b0ab717d) fix nil pointer (#3661)
 * [0e70364a1](https://github.com/kubeovn/kube-ovn/commit/0e70364a10bd4a86de1eb6a5d5ad62c3952c9443) docs: updated CHANGELOG.md (#3659)
 * [d4a815d83](https://github.com/kubeovn/kube-ovn/commit/d4a815d8369530360f652be2532f6701ad0786e4) chart: fix parsing image tag when the image url contains a port (#3644)
 * [b2dead02e](https://github.com/kubeovn/kube-ovn/commit/b2dead02e1992f61debe315fe0ef0b62c5d9567a) ovs: reduce cpu utilization (#3650)
 * [42e91567c](https://github.com/kubeovn/kube-ovn/commit/42e91567c336cd0edfb4637a0515caec47b33879) remove static route for custom vpc (#3647)
 * [d92e929e3](https://github.com/kubeovn/kube-ovn/commit/d92e929e3c293e70993cdab506fac52842f326c7) bump go modules (#3654)
 * [3104a6f0a](https://github.com/kubeovn/kube-ovn/commit/3104a6f0a366e8cc3a758c1c78880f886aa0e594) kube-ovn-monitor and kube-ovn-pinger export pprof path (#3649)
 * [8838b08d2](https://github.com/kubeovn/kube-ovn/commit/8838b08d227615eff7dc9829465908cd43d0cf89) build(deps): bump github.com/onsi/gomega from 1.31.0 to 1.31.1 (#3652)
 * [ea9cec2a9](https://github.com/kubeovn/kube-ovn/commit/ea9cec2a9f2c7956fd62b86566719c47ce22e8a0) build(deps): bump github.com/docker/docker (#3651)
 * [a69e6a3bc](https://github.com/kubeovn/kube-ovn/commit/a69e6a3bc46d36e8b056e3b2b6ce5bcb6868f9c9) build(deps): bump github.com/onsi/gomega from 1.30.0 to 1.31.0 (#3641)
 * [6da2734f0](https://github.com/kubeovn/kube-ovn/commit/6da2734f068b24ef79d3ab28ff1d2cc4e365b759) build(deps): bump actions/cache from 3 to 4 (#3643)
 * [44e248404](https://github.com/kubeovn/kube-ovn/commit/44e248404cfe374f94e655cd77a32f6ea509db3f) build(deps): bump the k8s-io group with 1 update (#3638)
 * [9f262fc32](https://github.com/kubeovn/kube-ovn/commit/9f262fc3252489ab796cc8d11eddc749bec794fb) build(deps): bump github.com/onsi/ginkgo/v2 from 2.14.0 to 2.15.0 (#3642)
 * [5f9ba94f5](https://github.com/kubeovn/kube-ovn/commit/5f9ba94f59ffc79ef667c51b0de9935a047e1e59) build(deps): bump github.com/evanphx/json-patch/v5 from 5.8.0 to 5.8.1 (#3633)
 * [f13ec5ae7](https://github.com/kubeovn/kube-ovn/commit/f13ec5ae7357cacf84c528891401722e81f94ac3) e2e for vip dual stack with security group (#3630)
 * [d5685ff78](https://github.com/kubeovn/kube-ovn/commit/d5685ff7831cd11b03a6dbbf1c07f927bc3f5463) SYSCTL_IPV4_IP_NO_PMTU_DISC change default to 0 (#3596)
 * [e1643a0c0](https://github.com/kubeovn/kube-ovn/commit/e1643a0c0cd72e57cd0949f26a9f9c0128a24045) remove duplicated function (#3629)
 * [790d9cb15](https://github.com/kubeovn/kube-ovn/commit/790d9cb159afd8e3f0cf7d7145e5f4b7d981f86e) build(deps): bump github.com/evanphx/json-patch/v5 from 5.7.0 to 5.8.0 (#3628)
 * [d487d711d](https://github.com/kubeovn/kube-ovn/commit/d487d711db6e9ff4c21d782c6e7e6f5e6c2bc2bc) fix: static one ip in dualstack subnet (#3625)
 * [45b00710c](https://github.com/kubeovn/kube-ovn/commit/45b00710cfc9e2b77e42bc297ab15908ab7dfa12) support vip dual stack (#3617)
 * [8707ff750](https://github.com/kubeovn/kube-ovn/commit/8707ff750fb244024a595eacce473d9a0f59f365) build(deps): bump github.com/onsi/ginkgo/v2 from 2.13.2 to 2.14.0 (#3624)
 * [bf7eb65d0](https://github.com/kubeovn/kube-ovn/commit/bf7eb65d0f46769f46adee43f3a4c8e992b7744f) build(deps): bump k8s.io/klog/v2 from 2.110.1 to 2.120.0 (#3622)
 * [bafdeae81](https://github.com/kubeovn/kube-ovn/commit/bafdeae81c3bd8de38c1efb8bbaa6a6dc7f84216) fix: empty  loop (#3615)
 * [2641a5932](https://github.com/kubeovn/kube-ovn/commit/2641a593225ef80f8afcc5f50a29cf0b335fe2ca) chart: fix ovs-ovn upgrade (#3613)
 * [524fa50c9](https://github.com/kubeovn/kube-ovn/commit/524fa50c9e153b36d3492b89179c21a6f28339ce) build(deps): bump github.com/emicklei/go-restful/v3 (#3619)
 * [9a77310e0](https://github.com/kubeovn/kube-ovn/commit/9a77310e0f6fc3ae2a5bb4f5f9f3d4604598d41c) fix: subnet status usingIp not consistent with availableIPRange and usingIPRange (#3608)
 * [cb28bed29](https://github.com/kubeovn/kube-ovn/commit/cb28bed294c6ec7aa1c89c78e49045cc34f5542f) build(deps): bump golang.org/x/sys from 0.15.0 to 0.16.0 (#3607)
 * [7c6c9127b](https://github.com/kubeovn/kube-ovn/commit/7c6c9127b49a8e8bb590b38721487a050149446a) build(deps): bump github.com/emicklei/go-restful/v3 (#3606)
 * [032314131](https://github.com/kubeovn/kube-ovn/commit/0323141312aca21bc5d174f323b40635ca9f8e46) ci: install kube-ovn with helm chart (#3599)
 * [83ca99d39](https://github.com/kubeovn/kube-ovn/commit/83ca99d3909173b61458cdd9de2320f685b1f906) ovs: increase cpu limit to 2 cores (#3530)
 * [5589e8ed8](https://github.com/kubeovn/kube-ovn/commit/5589e8ed870f333d70e3e371cc6efa4debf0e18e) update policy route when subnet cidr is changed (#3587)
 * [5d9a24a4d](https://github.com/kubeovn/kube-ovn/commit/5d9a24a4ded4f078dabc6ae926e2426cac0e8879) update ipset to v7.17 (#3601)
 * [1cd058f77](https://github.com/kubeovn/kube-ovn/commit/1cd058f7788199208415c3d318efcc9a41254813) fix ko (#3600)
 * [d4d2cbd22](https://github.com/kubeovn/kube-ovn/commit/d4d2cbd229d7de96c8d5c359fdf59b41b4076a4d) build(deps): bump github.com/osrg/gobgp/v3 from 3.21.0 to 3.22.0 (#3603)
 * [f7bd2520f](https://github.com/kubeovn/kube-ovn/commit/f7bd2520f34b6c6c236302f299f38c09519a55f0) add: MASTER_NODES_LABEL (#3597)
 * [d8e51e225](https://github.com/kubeovn/kube-ovn/commit/d8e51e225851afc7c7a2e9ed8f18d6378514c72a) chart: generate SSL certificates (#3598)
 * [37d832b37](https://github.com/kubeovn/kube-ovn/commit/37d832b37f7ab96017170f9848e0dbd63e314abb) fix: lrp type eip no finalizer (#3584)
 * [f2e494cae](https://github.com/kubeovn/kube-ovn/commit/f2e494caea9632c153e02360916e789ed97d450b) fix: ovn eip type (#3590)
 * [a91bc8ed0](https://github.com/kubeovn/kube-ovn/commit/a91bc8ed01af4e84042bd243363a21e15a6f4bfa) do not count ips in excludeIPs as available and using IPs (#3582)
 * [0052a409c](https://github.com/kubeovn/kube-ovn/commit/0052a409c374553021058a1c8214bb2200256df5) chart: support Talos Linux (#3593)
 * [09e2a0d5d](https://github.com/kubeovn/kube-ovn/commit/09e2a0d5db411f107f64129530c0e0bced4fc51e) chart: Autodiscover master nodes (#3591)
 * [897ff9f2b](https://github.com/kubeovn/kube-ovn/commit/897ff9f2badbfc40ffd685dd4411b3cb698e8eec) bump k8s to v1.28.5 (#3594)
 * [b3ea8b9c4](https://github.com/kubeovn/kube-ovn/commit/b3ea8b9c430029771d9eab75d706e2d5f4c1e9ad) fix static build for CNI-plugin (#3585)
 * [80cb018ee](https://github.com/kubeovn/kube-ovn/commit/80cb018ee59679e1b428110a600b67fbc7745a62) fix: duplicate ip deletion (#3589)
 * [f60bb3d5b](https://github.com/kubeovn/kube-ovn/commit/f60bb3d5b8ef09c50f199c8718a3876232897cee) fix security issue (#3588)
 * [44654a985](https://github.com/kubeovn/kube-ovn/commit/44654a985ffbcad0ebc5706899a04626c20e1dbc) chart: fix specifying namespace (#3592)
 * [b823c9bb9](https://github.com/kubeovn/kube-ovn/commit/b823c9bb9da15b9485a4bcb7dac289f902ab7342) build(deps): bump github.com/prometheus/client_golang (#3586)
 * [b0df19270](https://github.com/kubeovn/kube-ovn/commit/b0df19270aec959185665393224d93ed743269ff) ovn0 ipv6 addr gen mode set 0 (#3580)
 * [f9b3aa49a](https://github.com/kubeovn/kube-ovn/commit/f9b3aa49a46c10b89f566fe090e492b5ac4dd279) fix: G307 (#3575)
 * [fea6165eb](https://github.com/kubeovn/kube-ovn/commit/fea6165eb4d23ebd79c5996c979a0ee8dc5dc5ad) add bfd check from lrp to  underlay gw (#3441)
 * [f7ded7dff](https://github.com/kubeovn/kube-ovn/commit/f7ded7dff2a69ad22527e4bfef497a7c18c3d4a1) fix: add err log (#3572)
 * [71007d8d1](https://github.com/kubeovn/kube-ovn/commit/71007d8d17b3cbfc466b11de1d15a1a89f8c995a) build(deps): bump google.golang.org/protobuf from 1.31.0 to 1.32.0 (#3571)
 * [c6fefd4c1](https://github.com/kubeovn/kube-ovn/commit/c6fefd4c18b18e48d4c34d4b0b09af4cb6793873) fix: calculate usings before removed finalizer (#3570)
 * [ef35f62f0](https://github.com/kubeovn/kube-ovn/commit/ef35f62f04990d9ea2f817e6aa80cb3d48b3a8dd) fix speaker log (#3487)
 * [a106e7fe0](https://github.com/kubeovn/kube-ovn/commit/a106e7fe0690185513dd0eaa339cce01ed6bbe6b) fix: cleanup.sh (#3569)
 * [a4c2237c4](https://github.com/kubeovn/kube-ovn/commit/a4c2237c4b1a2f1225d8f9c70e755e202604098c) Makefile: fix kwok installation (#3561)
 * [69c3d6c74](https://github.com/kubeovn/kube-ovn/commit/69c3d6c74b1a9699fce767386d79421377c2daee) fix u2o infinity recycle (#3563)
 * [f6c83bad4](https://github.com/kubeovn/kube-ovn/commit/f6c83bad45f54e03482874c37434e87a3f869c50) roll back to v3 due to massive request timeout https://github.com/actions/download-artifact/issues/249
 * [82e11ecc0](https://github.com/kubeovn/kube-ovn/commit/82e11ecc0086d46aba79b6bc50c41a787bee500a) Fix 1.12 ipam deletion (#3554)
 * [72e90fd16](https://github.com/kubeovn/kube-ovn/commit/72e90fd16e9f9460f47cb06502e97f377d36e268) htbqos is not used anymore (#3559)
 * [51dcf7f79](https://github.com/kubeovn/kube-ovn/commit/51dcf7f79e6a37433cc8872a4f48f772ad16f27b) delete lsp and ipam with ip (#3540)
 * [67a83bf6a](https://github.com/kubeovn/kube-ovn/commit/67a83bf6a57010d1917e2527c8ac0d475e1400eb) skip subnet mtu e2e prior to v1.9 (#3557)
 * [a4545549d](https://github.com/kubeovn/kube-ovn/commit/a4545549de679c8d79b0038db24385a6bb8c7864) add np prefix to networkpolicy name when networkpolicy's name starts with number (#3551)
 * [4850f0a7e](https://github.com/kubeovn/kube-ovn/commit/4850f0a7ed5cd048cad33acdfb9feeb214b86843) do not calculate subnet.spec.excludeIPs as availableIPs (#3550)
 * [794a52a81](https://github.com/kubeovn/kube-ovn/commit/794a52a8130639bfa89883c64c836896bca21790) Fix: centos8 reached EOL and ovn 21.03 tarball unmatch with ovs 2.15 tarball issue (#3546)
 * [b09d249ba](https://github.com/kubeovn/kube-ovn/commit/b09d249baaece56dda23dfbee20a99ed9ba38dde) build(deps): bump google.golang.org/grpc from 1.60.0 to 1.60.1 (#3547)
 * [8081f437c](https://github.com/kubeovn/kube-ovn/commit/8081f437c777eb3dfe2c60f11ab78894d6fd77cb) keep ovs cpu:mem 1:1 and show pod status in kube-system namespace before diagnose (#3537)
 * [2f76694f8](https://github.com/kubeovn/kube-ovn/commit/2f76694f8d4d53d6189236267c6575a6602bf32d) fix ovn ic not clean lsp and lrp when az name contains "-" (#3542)
 * [65cf9ce47](https://github.com/kubeovn/kube-ovn/commit/65cf9ce472e7434a3d6b69e0ca71560a444cb3cd) fix: apply changes to the latest version (#3514)
 * [4e64005c2](https://github.com/kubeovn/kube-ovn/commit/4e64005c24e63e203346dba884a129b4e4c72b53) build(deps): bump golang.org/x/crypto from 0.16.0 to 0.17.0 (#3544)
 * [1712eda5a](https://github.com/kubeovn/kube-ovn/commit/1712eda5a8d752fb2c36e960141c9c4c5200b083) Revert "ovn-central: check raft inconsistency from nb/sb logs (#3532)"
 * [f25e20daa](https://github.com/kubeovn/kube-ovn/commit/f25e20daa7da04cd72519adc4049e67cfcdb47c8) SgPorts should be updated when pod deleted (#3516)
 * [44705bb89](https://github.com/kubeovn/kube-ovn/commit/44705bb898b9b1b334b235f79cba42956b406e96) bump go modules (#3505)
 * [2c7acdbb6](https://github.com/kubeovn/kube-ovn/commit/2c7acdbb6aa6273f6cab3d59a95f97353dfa4cac) ovn-central: check raft inconsistency from nb/sb logs (#3532)
 * [2c3eacb72](https://github.com/kubeovn/kube-ovn/commit/2c3eacb72b8faced363316768129a0798533b5f3) build(deps): bump actions/download-artifact from 3 to 4 (#3535)
 * [f07636b22](https://github.com/kubeovn/kube-ovn/commit/f07636b22fdb0086297ac387e6a95d976969cdcf) build(deps): bump actions/upload-artifact from 3 to 4 (#3534)
 * [a8ee70b32](https://github.com/kubeovn/kube-ovn/commit/a8ee70b32bd3d35d76307110ef2618ce915fa4b7) fix chassis gc (#3525)
 * [508535a43](https://github.com/kubeovn/kube-ovn/commit/508535a438c92d2021b5f88a9f0a4dc48a574e6b) Makefile: fix clab bgp cleanup (#3524)
 * [b0e254dea](https://github.com/kubeovn/kube-ovn/commit/b0e254deaee4ef4b373be1a253c9ec655400109c) docs: updated CHANGELOG.md (#3523)
 * [dbb946989](https://github.com/kubeovn/kube-ovn/commit/dbb9469893359762b19d3e07753acf9e9a17133c) build(deps): bump github/codeql-action from 2 to 3 (#3522)
 * [a1020196a](https://github.com/kubeovn/kube-ovn/commit/a1020196af71bb41f19083ea38d84d58ce83a4af) build(deps): bump google.golang.org/grpc from 1.59.0 to 1.60.0 (#3517)
 * [f49d93b0a](https://github.com/kubeovn/kube-ovn/commit/f49d93b0a9eb0b229d0bfdea7dd8ab531b5abf44) netpol: do not create unused address set for services (#3208)
 * [1f9dbf8d4](https://github.com/kubeovn/kube-ovn/commit/1f9dbf8d4f73b389eaa40b0b63dfe2bf849ef5c3) fix: gc keep lb for dnat after restart controller (#3508)
 * [869a9bc3c](https://github.com/kubeovn/kube-ovn/commit/869a9bc3c6718e92d6daecf748fb34af29386cad) ci: fix ovn-vpc-nat-gw e2e failed (#3503)
 * [7e285581a](https://github.com/kubeovn/kube-ovn/commit/7e285581aed29bf61a8b67cf4f00a43a421d9472) cni-server: set sysctl variable net.ipv4.ip_no_pmtu_disc to 1 by default (#3504)
 * [079e6c8dc](https://github.com/kubeovn/kube-ovn/commit/079e6c8dceaed82684cf8af4172018adab162b4b) build(deps): bump actions/stale from 8 to 9 (#3507)
 * [a999b009f](https://github.com/kubeovn/kube-ovn/commit/a999b009f31cccb8d1f698a800df33ea4489805e) fix: skip unnecessary del port request (#3501)
 * [23419b8e1](https://github.com/kubeovn/kube-ovn/commit/23419b8e1ddcd7d149817479ff3652a3b125161b) update image
 * [d243606a2](https://github.com/kubeovn/kube-ovn/commit/d243606a268947aa5b431f6672b6de2f4ac434a3) fix: duplicate gw nodes (#3500)
 * [7e5085020](https://github.com/kubeovn/kube-ovn/commit/7e508502067e273ae7740bb7c1828f603575d4ae) build(deps): bump actions/setup-go from 4 to 5 (#3499)
 * [260cca8de](https://github.com/kubeovn/kube-ovn/commit/260cca8de5b384c03fe1cd1ce44baf172ed2c8be) fix typo (#3494)
 * [6a939d928](https://github.com/kubeovn/kube-ovn/commit/6a939d928adc6a26ecc956b644cb339a3e6d4b70) fix: lost gc lsp in previous pr (#3493)
 * [874da5bd9](https://github.com/kubeovn/kube-ovn/commit/874da5bd9ca0ed610707357d24fbe4173bd55f8e) delete String() function (#3488)
 * [487bcd036](https://github.com/kubeovn/kube-ovn/commit/487bcd0365c07986e05856c4e008412e135a34ce) build(deps): bump github.com/containernetworking/plugins (#3489)
 * [be730193b](https://github.com/kubeovn/kube-ovn/commit/be730193ba423b7ba68f586d05318b6bc04bd237) e2e aap for security group (#3459)
 * [984f227f5](https://github.com/kubeovn/kube-ovn/commit/984f227f525712296c81bcf6ebe7c283e2a44183) iptables drop invalid rst (#3484)
 * [7436dc4de](https://github.com/kubeovn/kube-ovn/commit/7436dc4de1e570fd245b957466e080d5072499e2) add part of e2es for release-1.12-mc (#3478)
 * [f7ff76b8d](https://github.com/kubeovn/kube-ovn/commit/f7ff76b8d9d3442338250a9c8f2781f248b3a95f) make vpc_nat_gateway redo eip fip snat dnat iptables rules after k8s cluster reboot (#3267)
 * [8628642e0](https://github.com/kubeovn/kube-ovn/commit/8628642e0a03b1df01bea25fafd3da0f605a354a) fix: github action cannot ping 114 (#3486)
 * [39e920141](https://github.com/kubeovn/kube-ovn/commit/39e920141e6f8b01c421946143e5c05c4892b858) fix: check chassis before creation (#3482)
 * [fc7697704](https://github.com/kubeovn/kube-ovn/commit/fc7697704cea33412ba36ffcd4e87cb7f9e1faac) build(deps): bump github.com/osrg/gobgp/v3 from 3.20.0 to 3.21.0 (#3481)
 * [19269984a](https://github.com/kubeovn/kube-ovn/commit/19269984a6e8f9d5cc7ca4bfd06596b2d995920e) fix ovn eip not calculated (#3477)
 * [91dea3587](https://github.com/kubeovn/kube-ovn/commit/91dea35873a549455f8c43f805889361ba4c75d0) fix: calculate subnet before handle finalizer (#3469)
 * [f25700396](https://github.com/kubeovn/kube-ovn/commit/f25700396973f5287e1e603c760b0cbcb1ec2732) fix: ipam clean all pod nic ip address and mac even if just delete a nic (#3453)
 * [c6fe51733](https://github.com/kubeovn/kube-ovn/commit/c6fe51733b3891271ba9224910e83ee9504f1e2c) schedule kube-ovn-controller on the kube-ovn-master node (#3479)
 * [6233434a8](https://github.com/kubeovn/kube-ovn/commit/6233434a8f5a14e6f6ce2e29411840b9fb670d39) delete vm's lsp and release ipam.ip (#3476)
 * [ab0cf2214](https://github.com/kubeovn/kube-ovn/commit/ab0cf221482c85bcc604c79409c2bd34ef75a191) build(deps): bump github.com/onsi/ginkgo/v2 from 2.13.1 to 2.13.2 (#3475)
 * [45cee5780](https://github.com/kubeovn/kube-ovn/commit/45cee57808f15c603e0419f1b101c9141644170f) ci: remove branch release-1.12-cmss
 * [b1de14c0f](https://github.com/kubeovn/kube-ovn/commit/b1de14c0fcbf2d08e6021e98d23eef4e840e2cf7) fix: add static route for custom vpc when create subnet with (#3462)
 * [f536b24a1](https://github.com/kubeovn/kube-ovn/commit/f536b24a1419b5e9fd1c895bd812621b74c525f7) build(deps): bump golang.org/x/time from 0.4.0 to 0.5.0 (#3463)
 * [3351db34e](https://github.com/kubeovn/kube-ovn/commit/3351db34eaeac862809b7c9a23ed66afbf4618a7) build(deps): bump golang.org/x/sys (#3464)
 * [07798be1e](https://github.com/kubeovn/kube-ovn/commit/07798be1ebc9231e49acd67d8b87774c26b27c26) kube-ovn-cni: fix pinger result when timeout is reached (#3457)
 * [e334b60c3](https://github.com/kubeovn/kube-ovn/commit/e334b60c3df21b961147d55fb6523604b88c7f92) ovs-healthcheck: ignore error when log file does not exist (#3456)
 * [f8435b319](https://github.com/kubeovn/kube-ovn/commit/f8435b319980e9e287e97f2154f8bdb567ec3e01) ipam: fix duplicate allocation after cidr expansion (#3455)
 * [be68461ff](https://github.com/kubeovn/kube-ovn/commit/be68461ffa6ff7ab2d3142f81b2481a04d5fa497) feat:  support dpdk interface hotplug for kubevirt (#3438)
 * [d970e528f](https://github.com/kubeovn/kube-ovn/commit/d970e528fda0f87b724558b15d07c95e07cb8350) fix e2e install failed (#3450)
 * [ea14a7161](https://github.com/kubeovn/kube-ovn/commit/ea14a71615d41c02462e689a8a5b754cd1166b89) ci: fix scheduled workflow runs of building base images (#3449)
 * [b6a931fc2](https://github.com/kubeovn/kube-ovn/commit/b6a931fc2dc0c5cb1f1829ef43ae29c2d25d0114) readd assigned ip addresses to ipam when subnet has been changed (#3447)
 * [54949dbc2](https://github.com/kubeovn/kube-ovn/commit/54949dbc219ed6db541b8187370bd98f8a27efe7) base: fix missing CFLAGS -fPIC for arm64 (#3428)
 * [14f136fe3](https://github.com/kubeovn/kube-ovn/commit/14f136fe33228479b3a93457807ae6c61af9ab16) support allow address pairs for SecurityGroup (#3442)
 * [0c4fc68fc](https://github.com/kubeovn/kube-ovn/commit/0c4fc68fcd159b9d8919e2a21f9dd00b48f6dbdb) ci: support specifying branch (#3444)
 * [4622372ee](https://github.com/kubeovn/kube-ovn/commit/4622372ee8da97c279f6adc048178963845fbc71) OvnEip using default external subnet when not set (#3445)
 * [64a5cf523](https://github.com/kubeovn/kube-ovn/commit/64a5cf523de88d1dee28ddb476a82120c9201df2) ci: fix deepflow installation (#3443)
 * [c48863d82](https://github.com/kubeovn/kube-ovn/commit/c48863d82db6ab6f8ed8d25c3741a59bbfe5f034) fix: release VF assigned to the Pod back to the host network namespace (#3440)
 * [05a9a162c](https://github.com/kubeovn/kube-ovn/commit/05a9a162c0751a3274727065b009e5dc6c161155) fix:  multus network status not find dpdk interface name (#3432)
 * [2ba4c78a1](https://github.com/kubeovn/kube-ovn/commit/2ba4c78a1866ce184ce87611aeaf3f18901e9269) e2e for extra external subnets (#3435)
 * [68e96e77d](https://github.com/kubeovn/kube-ovn/commit/68e96e77d5206c0a42ceeb1f72469262b2f77993) Correcting spelling errors in word static (#3434)
 * [cffb893cf](https://github.com/kubeovn/kube-ovn/commit/cffb893cf22069077ed00ed6a75cae43ffb9515c) ci: fix missing environment variables (#3430)
 * [024b95e8f](https://github.com/kubeovn/kube-ovn/commit/024b95e8fd39c0111b4dc77e892698f66c6e1971) e2e: fix VIP test failure in v1.12
 * [02b60df1b](https://github.com/kubeovn/kube-ovn/commit/02b60df1baacc4e2a5b7b7825e83b89c4e35495b) base: fix dpdk build failure (#3426)
 * [38fefbe7a](https://github.com/kubeovn/kube-ovn/commit/38fefbe7a0aa2ac712c7d42617e7da2eeb578480) bump k8s to v1.28.4 (#3423)
 * [f52f2ef52](https://github.com/kubeovn/kube-ovn/commit/f52f2ef52215dced0e1691196a51bd1a78c8d3c7) base: fix ovn build failure (#3340)
 * [43f9b64e0](https://github.com/kubeovn/kube-ovn/commit/43f9b64e0707f6439107d2896bccfad5decdc3a5) ci: fix scheduled base image build workflow (#3415)
 * [965360e32](https://github.com/kubeovn/kube-ovn/commit/965360e32ac0e4929115e1504b8865fc38245753) fix:  lsp dhcp options set failed when subnet dhcp option is enabled (#3422)
 * [62b0e21e3](https://github.com/kubeovn/kube-ovn/commit/62b0e21e3f050416b40f9d5302838e1db75cbc04) chart: move CRD into directory crds (#3420)
 * [f60ad1fd2](https://github.com/kubeovn/kube-ovn/commit/f60ad1fd269dbadcd47daae337b6c3486ae5fd1e) trivy: ignore CVE-2023-5528
 * [4452eeaaa](https://github.com/kubeovn/kube-ovn/commit/4452eeaaa268cab474f5e8a7a3c74b9c4b0adbe1) base: fix ovn-northd/ovn-controller not creating pidfile in arm64 (#3413)
 * [e2e3b4f97](https://github.com/kubeovn/kube-ovn/commit/e2e3b4f975068d5690e2939b8082547975119353) fix kube-ovn-monitor probe (#3409)
 * [f7e216096](https://github.com/kubeovn/kube-ovn/commit/f7e2160962a91a9b2cbf70ab61588bf64ba01dae) ci: fix dpdk jobs (#3405)
 * [bbdf6c6f9](https://github.com/kubeovn/kube-ovn/commit/bbdf6c6f9b8a73b0f315bf3a8f5f7329e08c537e) support ovn ic ecmp (#3348)
 * [57b7d92fb](https://github.com/kubeovn/kube-ovn/commit/57b7d92fb5513c987c2988814ab86184572f1917) e2e for allowed address pair (#3394)
 * [df98a0a89](https://github.com/kubeovn/kube-ovn/commit/df98a0a89a904d0493914488fa5a7b06fb9fbd7f) ci: free disk space for all x86 jobs (#3406)
 * [819700b71](https://github.com/kubeovn/kube-ovn/commit/819700b7162360f505beb9838a655a3afc47bc6b) build(deps): bump k8s.io/klog/v2 from 2.100.1 to 2.110.1 (#3353)
 * [34cb63229](https://github.com/kubeovn/kube-ovn/commit/34cb63229d6b2b6f79d2b1306c92adfb65ab69ae) ci: free disk space (#3404)
 * [dd73d833a](https://github.com/kubeovn/kube-ovn/commit/dd73d833ac9ed442ee27d0a27c201a7cf2bdc2ee) build(deps): bump go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc from 0.45.0 to 0.46.0 (#3402)
 * [ca5ebcd71](https://github.com/kubeovn/kube-ovn/commit/ca5ebcd71c858fbbf0695c146135aaffd78351f7) build(deps): bump github.com/onsi/ginkgo/v2 from 2.13.0 to 2.13.1 (#3401)
 * [77785601a](https://github.com/kubeovn/kube-ovn/commit/77785601a5d52f10fb47804d2e803fec6c02ed14) fix: wrong usage about DeepEqual (#3396)
 * [8d3248936](https://github.com/kubeovn/kube-ovn/commit/8d3248936382998c694a4a919fbc1705b05472a1) build(deps): bump github.com/onsi/gomega from 1.29.0 to 1.30.0 (#3398)
 * [7d22425bb](https://github.com/kubeovn/kube-ovn/commit/7d22425bba829f7af8cb08f6dce340980307c04b) subnet support config mtu (#3367)
 * [a76445ed4](https://github.com/kubeovn/kube-ovn/commit/a76445ed4e0d0e1c9b7007f91ba16189a1538bc9) fix dualStack network checkgw raise panic (#3392)
 * [182c075a4](https://github.com/kubeovn/kube-ovn/commit/182c075a49e230426092796ef63091db888ad9e4) fix dpdk workflow (#3384)
 * [35b103b51](https://github.com/kubeovn/kube-ovn/commit/35b103b51167069faf6bdf256d6bfb5f59384718) support aap through vip crd using selector (#3359)
 * [08d8321e0](https://github.com/kubeovn/kube-ovn/commit/08d8321e0c65a440a48cf0569d75a8484e260204) fix:  gc delete multus ip cr and lsp setting when enable keep vm ip (#3378)
 * [a6b7045d3](https://github.com/kubeovn/kube-ovn/commit/a6b7045d3a7fe9e3291be7c493e469596ee6b541) add kube-ovn-controller nodeAffinity prefer not on ic gateway (#3376)
 * [ebbe6d95f](https://github.com/kubeovn/kube-ovn/commit/ebbe6d95f624072f85f67b98801935a31ff0ff37) Add clean ic az db (#3372)
 * [c5e877372](https://github.com/kubeovn/kube-ovn/commit/c5e87737223637e0a3e7a57f7fdc9184c8c9ea7c) build(deps): bump github.com/moby/sys/mountinfo from 0.6.2 to 0.7.1 (#3389)
 * [75bbeb202](https://github.com/kubeovn/kube-ovn/commit/75bbeb2024923bf35147e5584b9993a7bee3aea8) fix: externalID map should not include external_ids (#3385)
 * [92b386037](https://github.com/kubeovn/kube-ovn/commit/92b3860379deb841d913403f2a6c32758ea75f2e) fix dependency issues
 * [c3fd09d28](https://github.com/kubeovn/kube-ovn/commit/c3fd09d2835d70dc6eb06d5b883a139acea2b377) build(deps): bump golang.org/x/mod from 0.13.0 to 0.14.0 (#3380)
 * [4d9d8b36b](https://github.com/kubeovn/kube-ovn/commit/4d9d8b36b360f914639daf5969d303d708b801c9) build(deps): bump golang.org/x/sys from 0.13.0 to 0.14.0 (#3381)
 * [9604c8193](https://github.com/kubeovn/kube-ovn/commit/9604c8193738880ac1409d2bc56634484b7cad98) build(deps): bump golang.org/x/time from 0.3.0 to 0.4.0 (#3383)
 * [b8e63dba8](https://github.com/kubeovn/kube-ovn/commit/b8e63dba86eb1e9670bf6da827903e2c05e1ee66) docs: updated CHANGELOG.md (#3379)
 * [5ae3d25da](https://github.com/kubeovn/kube-ovn/commit/5ae3d25da24cf7c5c0ecbfe0b378d9b57983a460) kube-ovn-dpdk building need its dpdk base img (#3371)
 * [cc1f6adb4](https://github.com/kubeovn/kube-ovn/commit/cc1f6adb48cdbce33748f61367e772ae6fe690ba) delete check for existing ip cr (#3361)
 * [3bb2b3078](https://github.com/kubeovn/kube-ovn/commit/3bb2b3078fce72046ac45881361bb8a8c6fcf59b) fix IP residue after changing subnet of vm in some scenarios (#3370)
 * [20125e8fe](https://github.com/kubeovn/kube-ovn/commit/20125e8fe9fb2d10bc5dd0fb26fcbf6082527ef3) fix start-ovs-dpdk-v2.sh syntax error (#3373)
 * [b1d2d4dc2](https://github.com/kubeovn/kube-ovn/commit/b1d2d4dc2324261af8b05283a81845189c6b8e5d) build(deps): bump github.com/Microsoft/hcsshim from 0.11.2 to 0.11.4 (#3374)
 * [435fda5a9](https://github.com/kubeovn/kube-ovn/commit/435fda5a949257805e6c5adeb56809a77ff5bee4) build(deps): bump helm/chart-releaser-action from 1.5.0 to 1.6.0 (#3375)
 * [99ad03e90](https://github.com/kubeovn/kube-ovn/commit/99ad03e90c2f1cb6654b2be45439b1d82334d2bf) Add cmss work flow (#3358)
 * [2b200e8fa](https://github.com/kubeovn/kube-ovn/commit/2b200e8fac725126e647f21f0a35b757a7c439c0) kube-ovn-controller: fix memory growth caused by unused workqueue (#3366)
 * [31ae1fd57](https://github.com/kubeovn/kube-ovn/commit/31ae1fd578412b774559499ac6abe59a9f9de4eb) build(deps): bump github.com/osrg/gobgp/v3 from 3.19.0 to 3.20.0 (#3362)
 * [6b82297c7](https://github.com/kubeovn/kube-ovn/commit/6b82297c702989b7fb352ef84842c270208d340e) add e2e case for netpol (#3355)
 * [91994f925](https://github.com/kubeovn/kube-ovn/commit/91994f925c4aab5978a6034380402d9cd9448fba) build(deps): bump actions/stale from 7 to 8 (#3352)
 * [66f365c8e](https://github.com/kubeovn/kube-ovn/commit/66f365c8ed03a810daf2f0d20ec09936b6ec85d0) Signed-off-by: changluyi <clyi@alauda.io> (#3349)
 * [ce9975bd4](https://github.com/kubeovn/kube-ovn/commit/ce9975bd4cffcc44babb802c465aef4d34043cfe) Allow same subnet traffic (#3344)
 * [0a98e42e2](https://github.com/kubeovn/kube-ovn/commit/0a98e42e241aa674c2e3202a1b5cb0812f7a7569) add stale bot
 * [68747f904](https://github.com/kubeovn/kube-ovn/commit/68747f90440790ecdb88d5e12ef07de5046bba94) add compact switch (#3337)
 * [ac770718f](https://github.com/kubeovn/kube-ovn/commit/ac770718fe6540503aa28b9ed9e6b60eb0a50bb0) Add Layer 2 forwarding for subnet ports again (#3300)
 * [7484a2b76](https://github.com/kubeovn/kube-ovn/commit/7484a2b76d4010a3d099fe3199e2c7a129ebf1f8) build(deps): bump github.com/docker/docker (#3346)
 * [6db0ce87e](https://github.com/kubeovn/kube-ovn/commit/6db0ce87ebdcb157bc3ad868b4cadeda81248c84) fix lr-lb dnat with multiple distributed gateway ports (#3345)
 * [b95f196ab](https://github.com/kubeovn/kube-ovn/commit/b95f196ab4e52c3e2e76da09f5aa99bd187a3bbb) ci: bump various versions (#3339)
 * [920db9a84](https://github.com/kubeovn/kube-ovn/commit/920db9a84ef06b5aa650dc126faa989fb322f9b5) build(deps): bump github.com/onsi/gomega from 1.28.1 to 1.29.0 (#3343)
 * [f68a3da15](https://github.com/kubeovn/kube-ovn/commit/f68a3da15a6ea04a63f5b61699c32ff315457333) feat:  dpdk-22.11.1 support by kube-ovn (#3332)
 * [42572d2f7](https://github.com/kubeovn/kube-ovn/commit/42572d2f73ed6f129cc0a79bd1597c48ecbc64e3) docs: updated CHANGELOG.md (#3334)
 * [352b161ae](https://github.com/kubeovn/kube-ovn/commit/352b161ae651e9e2325eb4ff5a57c6d0366da16a) bump k8s to v1.28.3 (#3324)
 * [d974d89b4](https://github.com/kubeovn/kube-ovn/commit/d974d89b45b3d44122d5f53f74cb52a6232b41cf) profile: fix fd leak (#3328)
 * [1606f3c8a](https://github.com/kubeovn/kube-ovn/commit/1606f3c8a14a5ff797b966782dca20e98b873ace) Nat reuse router port external ip (#3313)
 * [9a2375393](https://github.com/kubeovn/kube-ovn/commit/9a23753934cc7b11263f12ea1345c6707a76e442) build(deps): bump github.com/onsi/gomega from 1.28.0 to 1.28.1 (#3326)
 * [9a1e662c7](https://github.com/kubeovn/kube-ovn/commit/9a1e662c74af5f903bece162c5b2fab194087683) kube-ovn-controller: fix ovn ic log directory not mounted to hostpath (#3322)
 * [4d74b584e](https://github.com/kubeovn/kube-ovn/commit/4d74b584e93521029d3cd0d23a79e1e69e3fd656) fix golang lint error (#3323)
 * [cc6bd8835](https://github.com/kubeovn/kube-ovn/commit/cc6bd8835edfd697b13df8c6063bf870c6cdd365) build(deps): bump sigs.k8s.io/controller-runtime from 0.16.2 to 0.16.3 (#3315)
 * [7db25b22f](https://github.com/kubeovn/kube-ovn/commit/7db25b22f326f017f1b1a5052e2e3cb13af68276) build(deps): bump the k8s-io group with 1 update (#3314)
 * [3fe3ec665](https://github.com/kubeovn/kube-ovn/commit/3fe3ec665cae5dccd5dfd8dfa156083c3cd58ef0) add load balancer health check (#3216)
 * [4b37fa9b4](https://github.com/kubeovn/kube-ovn/commit/4b37fa9b4aaa674a8fe5cd17c6cc3911e185ddec) build(deps): bump google.golang.org/grpc from 1.58.3 to 1.59.0 (#3310)
 * [2c30a2183](https://github.com/kubeovn/kube-ovn/commit/2c30a2183d6c2f221b208694e26feae9a10eb5a2) build(deps): bump github.com/Microsoft/hcsshim from 0.11.1 to 0.11.2 (#3309)
 * [a94eade9c](https://github.com/kubeovn/kube-ovn/commit/a94eade9c59229f9ec7552f193f8e9fdc150efc1) support vpc configuration of multiple external network segments through label and crd (#3264)
 * [e6086d281](https://github.com/kubeovn/kube-ovn/commit/e6086d281a2aec252683f0b8a1243654703c309c) sync subnet to vpc while switching between custom VPC and default VPC (#3218)
 * [99fb1899b](https://github.com/kubeovn/kube-ovn/commit/99fb1899b4f8ca000d32036538070f6ca73f0aae) security: ignore kubectl cve (#3305)
 * [fd9f3a19b](https://github.com/kubeovn/kube-ovn/commit/fd9f3a19bba7509c747e2532181ba5816006e459) Don't enqueue VPC update when DeletionTimestamp is zero (#3302)
 * [60ef1ad1f](https://github.com/kubeovn/kube-ovn/commit/60ef1ad1f31d2974cb9462670b0ad26b1015be6f) Revert update base image to ubuntu:23.10 (#3289) (#3299)
 * [ce8012093](https://github.com/kubeovn/kube-ovn/commit/ce801209307643525ab3e021c697a3fbdb30b6a5) add base rules for allowing vrrp packets (#3293)
 * [9f93981d6](https://github.com/kubeovn/kube-ovn/commit/9f93981d676813a6d10bfc9b96cf290f8931066d) build(deps): bump google.golang.org/grpc from 1.58.2 to 1.58.3 (#3295)
 * [01df39645](https://github.com/kubeovn/kube-ovn/commit/01df3964521c7a1fb028944d6468f6b67d6ced0b) build(deps): bump golang.org/x/net from 0.16.0 to 0.17.0 (#3296)
 * [b6192f37a](https://github.com/kubeovn/kube-ovn/commit/b6192f37a15d5a35c0c472c301688dced23ea20f) add concurrency limiter to ovs-vsctl (#3288)
 * [286e6340d](https://github.com/kubeovn/kube-ovn/commit/286e6340dcd5da3a01b9b5f67d1daa61b94c1c4e) update base image to ubuntu:23.10 (#3289)
 * [e5af67e75](https://github.com/kubeovn/kube-ovn/commit/e5af67e752dbdbfe969ee3ae7e9034566dda5786) build(deps): bump github.com/onsi/ginkgo/v2 from 2.12.1 to 2.13.0 (#3290)
 * [33bc9a626](https://github.com/kubeovn/kube-ovn/commit/33bc9a626759673ea9b2ffc3aa1fb737636bf008) support custom vpc dns its deployment replicas (#3286)
 * [f9edd6692](https://github.com/kubeovn/kube-ovn/commit/f9edd66926466c83a0a4ff42a18946e414e724e7) webhook: fix ip validation when pod is annotated with an ippool name (#3284)
 * [4af196059](https://github.com/kubeovn/kube-ovn/commit/4af196059283b296b79b434df9d8771eef66199d) webhook: use dedicated port for health probe (#3285)
 * [51722079c](https://github.com/kubeovn/kube-ovn/commit/51722079c9c0abe3f3571b005f352456088d894f) bgp: support multiple peer addresses (#3283)
 * [c493a95bf](https://github.com/kubeovn/kube-ovn/commit/c493a95bf6491ce1cfbe5df81b67063db2087e8b) bgp: support announce policy (#3282)
 * [177d6e1cf](https://github.com/kubeovn/kube-ovn/commit/177d6e1cf901006388c916271f233fed4438033e) bump go modules (#3279)
 * [7ec458958](https://github.com/kubeovn/kube-ovn/commit/7ec4589589d2c57774ae3e136ee1d6114e8229a8) dump cpu/mem profile into file on signal SIGUSR1/SIGUSR2 (#3262)
 * [3f0755d71](https://github.com/kubeovn/kube-ovn/commit/3f0755d710b9b94a3e109d10616c8da15b32c5f7) ovs: load kernel module ip_tables only when it exists (#3281)
 * [5707dd03e](https://github.com/kubeovn/kube-ovn/commit/5707dd03e792b02e4fc2dc48708c8234e1413ff5) kubectl-ko: fix help message (#3277)
 * [ce2c72216](https://github.com/kubeovn/kube-ovn/commit/ce2c722164a9ee3799c5ec931a81a7c771dc7101) update directory name in charts readme (#3276)
 * [7ae309337](https://github.com/kubeovn/kube-ovn/commit/7ae309337afd7fd0c84a1e858402344431ca5040) fix ovn build failure (#3275)
 * [71491211b](https://github.com/kubeovn/kube-ovn/commit/71491211b6f90aec3a5b32b1233ac4c3b70cf8ed) build(deps): bump golang.org/x/mod from 0.12.0 to 0.13.0 (#3272)
 * [9076c3163](https://github.com/kubeovn/kube-ovn/commit/9076c3163dfc4a614cfa4a98d89d66785898229e) build(deps): bump golang.org/x/sys from 0.12.0 to 0.13.0 (#3271)
 * [dac982d1a](https://github.com/kubeovn/kube-ovn/commit/dac982d1a4b5fb4c493113968794790eba2ebddd) build(deps): bump github.com/osrg/gobgp/v3 from 3.18.0 to 3.19.0 (#3270)
 * [6ee71b52b](https://github.com/kubeovn/kube-ovn/commit/6ee71b52b8c2ba04ae1607901777d77faa5dbf8d) build(deps): bump github.com/onsi/gomega from 1.27.10 to 1.28.0 (#3268)
 * [cb46bfdd8](https://github.com/kubeovn/kube-ovn/commit/cb46bfdd8c44e82d9e47ef1094535a82dee24ce6) build(deps): bump github.com/prometheus/client_golang (#3266)
 * [aee41bd68](https://github.com/kubeovn/kube-ovn/commit/aee41bd687c53e7a7c0023e5477e128ca7477101) bump k8s to v1.28.2 (#3159)
 * [f20343297](https://github.com/kubeovn/kube-ovn/commit/f2034329793283457c5ac856fa7b051ff225f488) pinger: increase packet send interval (#3259)
 * [55f254e43](https://github.com/kubeovn/kube-ovn/commit/55f254e43e0058cbcd2fb90f2afac3cae6a24e1a) add init container in vpc-nat-gateway statefulset for init (#3254)
 * [4394bad9e](https://github.com/kubeovn/kube-ovn/commit/4394bad9e5f370ea69e06cc1ab5490d2abf6b81e) lrp should use chassis name instead of uuid (#3258)
 * [4e176a6ae](https://github.com/kubeovn/kube-ovn/commit/4e176a6aeb41ce116586df76a6fad0d4d9f3c7f6) docs: updated CHANGELOG.md (#3252)
 * [e25bd2ed7](https://github.com/kubeovn/kube-ovn/commit/e25bd2ed74c21b1091fe14b8c163ed781cf5d7bd) fix: for existing nic, no need to set the port type to internal (#3243)
 * [0ceb92408](https://github.com/kubeovn/kube-ovn/commit/0ceb924083307117bb00f5429c8827bc74e80a2c) adjust vip prints as ip (#3248)
 * [ff9b57b1c](https://github.com/kubeovn/kube-ovn/commit/ff9b57b1c44209e19c9177609f223acc5ed08361) add dpdk probe (#3151)
 * [b0836845f](https://github.com/kubeovn/kube-ovn/commit/b0836845ffd2fccad0dbe2e38c66826b9cae2893) add wrk in kube-ovn:test (#3233)
 * [6cde93263](https://github.com/kubeovn/kube-ovn/commit/6cde9326357709f8a1cff7d68202605234ced2b1) build(deps): bump google.golang.org/grpc from 1.58.1 to 1.58.2 (#3251)
 * [f016bb37a](https://github.com/kubeovn/kube-ovn/commit/f016bb37a5cf46d02129ff300ccf46c2e5927eef) build(deps): bump github.com/Microsoft/hcsshim from 0.11.0 to 0.11.1 (#3245)
 * [53c291966](https://github.com/kubeovn/kube-ovn/commit/53c2919666a9122bbd6e619a3345774dd7c0ad96) update policy route nexthops para (#3224)
 * [9a408a50a](https://github.com/kubeovn/kube-ovn/commit/9a408a50afc095583a814766b280188a1740ba55) build(deps): bump the k8s-io group with 1 update (#3242)
 * [233e46a25](https://github.com/kubeovn/kube-ovn/commit/233e46a2585b5a20d37c4b40f99072d6a608feb6) fix goproxy Denial of Service vulnerability (#3240)
 * [28127e229](https://github.com/kubeovn/kube-ovn/commit/28127e2290a33ef7d0bef55780f56af32820dce3) build(deps): bump github.com/cyphar/filepath-securejoin (#3239)
 * [2e6a0c9a6](https://github.com/kubeovn/kube-ovn/commit/2e6a0c9a6cca65c71d7bce1ed737ce9051f7365a) build(deps): bump github.com/onsi/ginkgo/v2 from 2.11.0 to 2.12.1 (#3237)
 * [c0b968ea1](https://github.com/kubeovn/kube-ovn/commit/c0b968ea13e1679f1b5fa30ff6d6862a02ff58d3) build(deps): bump github.com/docker/docker (#3234)
 * [5f1d3fe66](https://github.com/kubeovn/kube-ovn/commit/5f1d3fe66c6e59b745d19803b7dd802c7a5ff0a9) build(deps): bump github.com/evanphx/json-patch/v5 from 5.6.0 to 5.7.0 (#3235)
 * [f26f0c357](https://github.com/kubeovn/kube-ovn/commit/f26f0c3572df1da73a961d085d7da4f2ffc8047f) build(deps): bump github.com/osrg/gobgp/v3 from 3.17.0 to 3.18.0 (#3238)
 * [6d5fd3739](https://github.com/kubeovn/kube-ovn/commit/6d5fd3739d01a476c84b1900cb640535fab75b1c) update dependabot
 * [423e099b6](https://github.com/kubeovn/kube-ovn/commit/423e099b64dad206083338e33f230cac91178b36) build(deps): bump google.golang.org/grpc from 1.57.0 to 1.58.1 (#3229)
 * [ae1e0ef99](https://github.com/kubeovn/kube-ovn/commit/ae1e0ef99cb028c4bf21b94717eb94b621244f95) build(deps): bump github.com/Microsoft/hcsshim from 0.10.0 to 0.11.0 (#3228)
 * [ef5b250cd](https://github.com/kubeovn/kube-ovn/commit/ef5b250cd219b1e44658e46f6a7cb459e716a252) build(deps): bump golang.org/x/sys from 0.11.0 to 0.12.0 (#3232)
 * [4a0a7393f](https://github.com/kubeovn/kube-ovn/commit/4a0a7393f89c72afe582c032c246c252517982d8) fix typos (#3215)
 * [eb747615a](https://github.com/kubeovn/kube-ovn/commit/eb747615a8e94a0e7c101081a213e8d9990ca462) chart: remove subnet finalizers before subnets are deleted (#3213)
 * [494f47581](https://github.com/kubeovn/kube-ovn/commit/494f475811f7d0ae0472b3d0e078cd5f03d49e53) build(deps): bump docker/setup-buildx-action from 2 to 3 (#3206)
 * [b04cb3cc7](https://github.com/kubeovn/kube-ovn/commit/b04cb3cc7078a784b5277c1d91d8007254bc4b2a) add special handling for the route policy of the default VPC (#3194)
 * [a17fb7d74](https://github.com/kubeovn/kube-ovn/commit/a17fb7d74c3cd67fcd14614e4497cd1f4f1ef3ff) build(deps): bump docker/setup-qemu-action from 2 to 3 (#3207)
 * [8a0ec964e](https://github.com/kubeovn/kube-ovn/commit/8a0ec964e1158a447bd09a55e07de8a5bb295203) fix add static route to wrong table of ovn (#3195)
 * [4e4f27f29](https://github.com/kubeovn/kube-ovn/commit/4e4f27f2922e9e756e02c88d46da33b760c7164b) kubectl-ko: add new command ovn-trace for tracing ovn lflows only (#3202)
 * [2cba74b39](https://github.com/kubeovn/kube-ovn/commit/2cba74b3914aff8ee582f520b95c4e809585a42d) netpol: fix duplicate default drop acl (#3197)
 * [d766711fb](https://github.com/kubeovn/kube-ovn/commit/d766711fb82b4a24dd081d2ea8a211cd5219ccc5) add log to help find conflict ip owner (#3191)
 * [11408af46](https://github.com/kubeovn/kube-ovn/commit/11408af4660835c8776b29e80890bee0974be543) suuport user custom log location (#3186)
 * [8a3cf0349](https://github.com/kubeovn/kube-ovn/commit/8a3cf034961207f55fe128e4de70cdbcf92de6bf) add golang lint (#3154)
 * [371d7c3c4](https://github.com/kubeovn/kube-ovn/commit/371d7c3c40c471f143060c48d8fa8d4a55d1d844) underlay: fix ip/route tranfer when the nic is managed by NetworkManager (#3184)
 * [8490ae67d](https://github.com/kubeovn/kube-ovn/commit/8490ae67dea0fb5dbad6c5bee380a29fe1926ade) ci: wait for terminating ovs-ovn pod to disappear (#3160)
 * [d8b06ecbe](https://github.com/kubeovn/kube-ovn/commit/d8b06ecbe8cf558b5db788159de8898c830d51b4) fix ovn build (#3166)
 * [089175485](https://github.com/kubeovn/kube-ovn/commit/089175485190c2f9b2a7ab3c61e3005251600a66) build(deps): bump actions/checkout from 3 to 4 (#3183)
 * [f71246f80](https://github.com/kubeovn/kube-ovn/commit/f71246f80d85b403f39df08add7f87aea0168a35) chart: fix ovs-ovn upgrade (#3164)
 * [0505ca338](https://github.com/kubeovn/kube-ovn/commit/0505ca3384929c6b0820abd8961dfc177fbe048b) subnet: fix deleting lr policy on node deletion (#3176)
 * [093730364](https://github.com/kubeovn/kube-ovn/commit/093730364c69acf908dac636905b53ab9941a46e) Add tolerations to pinger daemonSet to allow schedule on not ready nodes. (#3173)
 * [58efd1daf](https://github.com/kubeovn/kube-ovn/commit/58efd1daf2f70af3557d29a7558c06886f6a3b03) ci/test: bump various versions (#3162)
 * [5172b62b7](https://github.com/kubeovn/kube-ovn/commit/5172b62b79ac1e64c633c676de46bfdaa89d68e1) kubectl-ko: get ovn db leaders only on necessary (#3158)
 * [a5c8510cd](https://github.com/kubeovn/kube-ovn/commit/a5c8510cd72633cc1e7163c2d227c679f63ec3a0) underlay: fix NetworkManager operation (#3147)
 * [3a67ea0d5](https://github.com/kubeovn/kube-ovn/commit/3a67ea0d5bd9429b91f130a7f1a581eaf5313a28) enable set --ovn-northd-n-threads (#3150)
 * [c753d4e44](https://github.com/kubeovn/kube-ovn/commit/c753d4e4483c5d94fd36cf5eb970e61ca1676856) build(deps): bump github.com/emicklei/go-restful/v3 (#3156)
 * [4d4577122](https://github.com/kubeovn/kube-ovn/commit/4d457712260ac92d5fcf7e2cc5314d5bbe1fdb55) Fix max unavailable (#3149)
 * [46cee0374](https://github.com/kubeovn/kube-ovn/commit/46cee0374a80e3b9e431f64b4277e0827c0e335d) base: remove ovn patch for skipping ct (#3141)
 * [bcbae34eb](https://github.com/kubeovn/kube-ovn/commit/bcbae34ebaeecbadb91b020da8214a3d5b069ebc) sbctl chassis operation  replace with libovsdb (#3119)
 * [a10d94aeb](https://github.com/kubeovn/kube-ovn/commit/a10d94aeb387c622d4629081b694af4a62f5e23f) Enable set probe (#3145)
 * [d6e5aba00](https://github.com/kubeovn/kube-ovn/commit/d6e5aba009990dc71aee7cc18c1bdf7520073ac0) support recreate a backup pod with full annotation (#3144)
 * [2f6f67515](https://github.com/kubeovn/kube-ovn/commit/2f6f67515dab6f2aa1c657c87e4af0e6ffce5614) fix ovn nat not clean (#3139)
 * [d0063e3cc](https://github.com/kubeovn/kube-ovn/commit/d0063e3cca6597544f17fc3d930bc881d1263000) add mulicast snoop switch (#3129)
 * [e541a5797](https://github.com/kubeovn/kube-ovn/commit/e541a5797aeb32ffa41efe3bbefc68c07da98b80) ovn: do not send direct traffic between lports to conntrack (#3131)
 * [563bca688](https://github.com/kubeovn/kube-ovn/commit/563bca6881e041df1d2a2d36b8efbe44d3fe5d5a) bump go version to 1.21 (#3137)
 * [bbb0e102d](https://github.com/kubeovn/kube-ovn/commit/bbb0e102d202b6766c6a01366cac6afde5c3c207) delete append externalIds process in initIPAM (#3134)
 * [01c5d362a](https://github.com/kubeovn/kube-ovn/commit/01c5d362ad494f49fb4830da923ee2ddc7fca4b1) add probe (#3133)
 * [51ceddb66](https://github.com/kubeovn/kube-ovn/commit/51ceddb66362fd35c78c575b24a80c0afbb91ec0) ci: fix version comparison (#3127)
 * [795e026ad](https://github.com/kubeovn/kube-ovn/commit/795e026ad14f1ed6cf11cce30016c3828c5e4aea) fix ci (#3125)
 * [294e5f996](https://github.com/kubeovn/kube-ovn/commit/294e5f9969557a6b041ecae20c814c183f8ea555) move unnecessary init process after startWorkers (#3124)
 * [1a720192d](https://github.com/kubeovn/kube-ovn/commit/1a720192ddbf3639bd5371a4ffbd604405da3cb3) add e2e test for ovn db recover (#3118)
 * [99f37b398](https://github.com/kubeovn/kube-ovn/commit/99f37b3989ad3ab0446db58c79d567d9a8e4d8ee) prepare for next release

### Contributors

 * Amin Mohammadian
 * Andrei Kvapil
 * Anton Patsev
 * Congqi Zhao
 * Guangyu Suo
 * Joachim Hill-Grannec
 * Karol Szwaj
 * KillMaster9
 * Longchuanzheng
 * Mengxin Liu
 * Navid
 * Qinghao Huang
 * SKALA NETWORKS
 * SkalaNetworks
 * Tobias
 * Zhao Congqi
 * bobz965
 * bogdan-cehash
 * changluyi
 * cmdy
 * coldzerofear
 * cui fliter
 * dependabot[bot]
 * dolibali
 * fanriming
 * github-actions[bot]
 * guangwu
 * guofen
 * hzma
 * lanyujie
 * liyh-yusur
 * pengbinbin1
 * singchia
 * smartVan
 * wanglei
 * wangwangyusur
 * wangwangyusur288
 * wenwenxiong
 * wujixin
 * xieyanker
 * xujunjie-cover
 * zcq98
 * zhangzujian
 * 夜微澜
 * 张祖建
 * 袁又袁

## v1.12.35 (2025-08-20)

 * [112588fc3](https://github.com/kubeovn/kube-ovn/commit/112588fc35bd53ea50e005d9d9ed88275ac88582) release v1.12.35
 * [828445a4e](https://github.com/kubeovn/kube-ovn/commit/828445a4e5a2ff410f2cfa8354d373b45a517f35) dump golang 1.23.12 (#5630)
 * [47f8c01dd](https://github.com/kubeovn/kube-ovn/commit/47f8c01dd2f753dfedef799b8c4572d215312b5b) fix static mac pod conflict with gateway mac (#5623)
 * [2898de112](https://github.com/kubeovn/kube-ovn/commit/2898de112fad3dc30ef74f18882e89538b05690d) Fix the problem that if available ip is 0 but there is a value in excludeIPs, the fixed ip is used as the ip in excludeIPs but the error noAddressAvaliable is still reported (#5568)
 * [9c2d11580](https://github.com/kubeovn/kube-ovn/commit/9c2d115802e8cd304c0c06be8ecd4e6d31d58326) check underlay nic exist before config external bridge (#5520)
 * [91c2578b9](https://github.com/kubeovn/kube-ovn/commit/91c2578b946084a97e053e09b8c2167d89c0fda3) bump github.com/go-viper/mapstructure/v2 to v2.3.0
 * [a1377c821](https://github.com/kubeovn/kube-ovn/commit/a1377c821ebba035633248986461634186097221) fix setting LR option always_learn_from_arp_request (#5426)
 * [0b4f2a380](https://github.com/kubeovn/kube-ovn/commit/0b4f2a380e3b6fa4a0578b9cb50a15def941bf47) controller: set always_learn_from_arp_request to false only when LR is not connected to external network (#5419)
 * [f54fc8aaa](https://github.com/kubeovn/kube-ovn/commit/f54fc8aaa109a59cab21d142394a0d7575744251) fix parsing resolv.conf when systemd-resolved is running on the host (#5423)
 * [78fb865eb](https://github.com/kubeovn/kube-ovn/commit/78fb865ebb673c189bbc89d43d365fe3e01c751d) fix duplicate acls because of parentkey
 * [c56cfd8a7](https://github.com/kubeovn/kube-ovn/commit/c56cfd8a71721d6a719c5356e581d36a0ef5f928) fix(slr): deleting old entries from ipmapping to avoid clogging loadbalancer (#5380)
 * [985e294f0](https://github.com/kubeovn/kube-ovn/commit/985e294f09b1cf9ef6a6733db6cedcd221b5fb0a) fix(slr): switchlbrule doesn't support multi-homed/ipv6-first pods (#5375)
 * [4205ff744](https://github.com/kubeovn/kube-ovn/commit/4205ff744561c5b14469dfa8fefb288b139a7788) prepare for next release

### Contributors

 * Mengxin Liu
 * SKALA NETWORKS
 * changluyi
 * clyi
 * zhangzujian
 * 张祖建

## v1.12.34 (2025-06-11)

 * [2e5315080](https://github.com/kubeovn/kube-ovn/commit/2e5315080603750409d2114108c46967057b124b) release v1.12.34
 * [b1cb20a7f](https://github.com/kubeovn/kube-ovn/commit/b1cb20a7fc7dda9a4270740df066106de445f37b) fix sts/vm lsp in incorrect port groups after rescheduled to another node (#5346)
 * [189ce7525](https://github.com/kubeovn/kube-ovn/commit/189ce7525f2293f9496ec2822c453c06db2a9f00) prepare for next release

### Contributors

 * Mengxin Liu
 * 张祖建

## v1.12.33 (2025-06-09)

 * [2e5ef48cd](https://github.com/kubeovn/kube-ovn/commit/2e5ef48cde8c085068c3adfe0342e018e68dbef1) release v1.12.33
 * [25ad657a3](https://github.com/kubeovn/kube-ovn/commit/25ad657a372e56a0163ed174dc43d6f0a53553b0) add dad check (#5333)
 * [0594fa43e](https://github.com/kubeovn/kube-ovn/commit/0594fa43e8852bba2fff3db9568d1adb32ac4e64) netpol: fix missing ACL name (#5281)
 * [5b4cc8ac9](https://github.com/kubeovn/kube-ovn/commit/5b4cc8ac9e69a0bafea58617de92abba79b8ce08) fix: kubectl-ko: properly get the number of nodes (#5327)
 * [c505d25ca](https://github.com/kubeovn/kube-ovn/commit/c505d25ca6a1043cdb90c8dca290843698e8cb8a) prepare for next release

### Contributors

 * Mengxin Liu
 * Robin Lee
 * changluyi
 * 张祖建

## v1.12.32 (2025-06-06)

 * [fb2c1d8f4](https://github.com/kubeovn/kube-ovn/commit/fb2c1d8f4eb0d6d288ddf78f2f5065f007edf0cb) release v1.12.32
 * [9cc2a4875](https://github.com/kubeovn/kube-ovn/commit/9cc2a48756ea21e24e29f339102664370e1b98f4) bump go to 1.23.10 (#5323)
 * [8ff160589](https://github.com/kubeovn/kube-ovn/commit/8ff1605897b4ddebfc82711c14ab7ce8d8e85c78) fix podcidr route still use join ip as src ip (#5287)
 * [d73f752f7](https://github.com/kubeovn/kube-ovn/commit/d73f752f7aafa0f1fa53c0b15df677acdd15ad9f) ci: fix cilium chaining with underlay networking (#5226)
 * [54021bf79](https://github.com/kubeovn/kube-ovn/commit/54021bf79b607bc32e797a208b586a9aa728957a) base: use local patch files (#5207)
 * [2bf56792d](https://github.com/kubeovn/kube-ovn/commit/2bf56792d52dd354a5d08819ca3f4da911ae4dda) bump k8s to v1.30.12 (#5185)
 * [9f27024ee](https://github.com/kubeovn/kube-ovn/commit/9f27024ee6a72f26f9589cd1c1d249470dd26cbd) controller: ensure ovn route policy is reconciled after node is initialized (#5166)
 * [e416f32ef](https://github.com/kubeovn/kube-ovn/commit/e416f32ef63139bcd60cdd040894cc396f39b9c5) ci: bump ubuntu to 24.04 (#5153)
 * [f64f3ea17](https://github.com/kubeovn/kube-ovn/commit/f64f3ea17107e09e6d5e19a6a1a1fe0c97b738e8) fix dbus/NetworkManager connection in Talos (#5140)
 * [fe45fa8e3](https://github.com/kubeovn/kube-ovn/commit/fe45fa8e30f014d0177fde742320dfd75e04cc4e) bump go to 1.23.7 (#5105)
 * [0d0d6b832](https://github.com/kubeovn/kube-ovn/commit/0d0d6b83293004e089f2e7d4bd6603fbd0694958) fix: egress network policy not work, when no pod hit matchlabel
 * [d9e1d9e1d](https://github.com/kubeovn/kube-ovn/commit/d9e1d9e1dc8c1149e001b52c9e9b3ac647018ec9) [fix] When the Nat-gw pod container restarts unexpectedly, trigger nat-gw statefulset restart to restore the nat-gw pod configuration (#5070)
 * [8c2049b34](https://github.com/kubeovn/kube-ovn/commit/8c2049b34040decb4521fd94b3c41c82c2e91fa2) kubectl-ko: fix conntrack state (#5038)
 * [8e183b1d6](https://github.com/kubeovn/kube-ovn/commit/8e183b1d6674c1f5fe3c242f3880dcf44c759e60) ci: remove legacy network policy e2e (#5035)
 * [449fec2ba](https://github.com/kubeovn/kube-ovn/commit/449fec2ba3f32e6552fec12f31753a916f106b19) feat(GC): Add check for GC disabled (#5005)
 * [432a98a83](https://github.com/kubeovn/kube-ovn/commit/432a98a831da1837379a939abf61101b86ba22b5) bump k8s to 1.30.10 (#5019)
 * [8d33b59ea](https://github.com/kubeovn/kube-ovn/commit/8d33b59eaa7c7774df2f31d6723cd4c1174c8c29) ci: bump aquasecurity/trivy-action to 0.29.0
 * [ad2538e19](https://github.com/kubeovn/kube-ovn/commit/ad2538e190afdcdeeca3b370f26d782b43cd85aa) prepare for next release

### Contributors

 * Kevin Carter
 * Mengxin Liu
 * changluyi
 * clyi
 * xiaoyie
 * zhangzujian
 * 张祖建

## v1.12.31 (2025-02-17)

 * [89c3acf1e](https://github.com/kubeovn/kube-ovn/commit/89c3acf1ef2f78f84175a7f47ef538012212ba13) release v1.12.31
 * [cd0d99ebe](https://github.com/kubeovn/kube-ovn/commit/cd0d99ebe94e715aa0b27ba29479d26393dc2683) fix superfluous response.WriteHeader (#4980)
 * [dc641799a](https://github.com/kubeovn/kube-ovn/commit/dc641799aa7d000f93312678a750a8f323f3aceb) use httpGet as liveness/readiness probe method (#4945)
 * [97dc1210f](https://github.com/kubeovn/kube-ovn/commit/97dc1210fb888b7f04d9df02de258e13edcf5357) fix: kube-ovn-cni always dump-flows br-provider per period (#4969)
 * [1ad46bed6](https://github.com/kubeovn/kube-ovn/commit/1ad46bed64ec333a45b58928952b4ef99a386e36) controller: consider StatefulSet's start ordinal (#4967)
 * [5b859819f](https://github.com/kubeovn/kube-ovn/commit/5b859819fc3649e18ec595815f41de42245badca) bump go to 1.22.12 (#4965)
 * [11def018a](https://github.com/kubeovn/kube-ovn/commit/11def018a1bdb6d145f26658bb99cbe06e0f7a75) ci: build arm64 images on arm64 hosted runners (#4936)
 * [138da35ac](https://github.com/kubeovn/kube-ovn/commit/138da35ac8b960da9286308eb5f2214aca5f0ff5) fix log (#4928)
 * [3518f6b80](https://github.com/kubeovn/kube-ovn/commit/3518f6b804adbd9a30c10567aee64b144b4df6e9) controller: check condition NodeNetworkUnavailable when determining whether node is ready (#4917)
 * [7ef9ebaae](https://github.com/kubeovn/kube-ovn/commit/7ef9ebaaec74c5a20a98688e52902c104a65cd11) cni-server: set node NetworkUnavailable condition after join subnet gateway check (#4915)
 * [b8ba5d84d](https://github.com/kubeovn/kube-ovn/commit/b8ba5d84d12c92d0189f10f91bd37a2e467203ec) ipam: check subnet's available ipv6 address count (#4903)
 * [26224c58d](https://github.com/kubeovn/kube-ovn/commit/26224c58dfc884f86c965990ea37c5e69144ff74) fix(controller/subnet): controller crashes on subnets if gateway is unspecified and netpol are disabled (#4848)
 * [67c9af8c1](https://github.com/kubeovn/kube-ovn/commit/67c9af8c125f77ed2c7f6ff354d8d4ac4ac1f1e5) bump go to 1.22.10 (#4853)
 * [b030cbc4d](https://github.com/kubeovn/kube-ovn/commit/b030cbc4dc7bb91d43a34908957e7337beb5ecaf) use JSON merge patch to update labels/annotations (#4838)
 * [e19f48c08](https://github.com/kubeovn/kube-ovn/commit/e19f48c0826f0cc236c8d7092a8f4297ae7660ca) fix getting subnet cidr by protocol (#4844)
 * [5e8288be0](https://github.com/kubeovn/kube-ovn/commit/5e8288be0765a69f45b8fd8d475533ed3f8d5df8) ci: wait for kubevirt crd to be created before creating CR (#4839)
 * [16955b0b0](https://github.com/kubeovn/kube-ovn/commit/16955b0b0156046b61466d07d39c2e3d739bdaae) build(deps): bump helm/kind-action from 1.10.0 to 1.11.0 (#4837)
 * [b76c0441c](https://github.com/kubeovn/kube-ovn/commit/b76c0441cd75f59410823af3663d902ac6c952f3) refactor: remove redundant policy route addition in node handling (#4835)
 * [3b1357316](https://github.com/kubeovn/kube-ovn/commit/3b1357316bc22dde38bff6519282dc0151bed72d) prepare for next release

### Contributors

 * Mengxin Liu
 * SKALA NETWORKS
 * changluyi
 * zbb88888
 * zhangzujian
 * 张祖建

## v1.12.30 (2024-12-16)

 * [4344a4671](https://github.com/kubeovn/kube-ovn/commit/4344a46714a1e21a879d1842eb09afb300edddb8) release v1.12.30
 * [f2c7937a7](https://github.com/kubeovn/kube-ovn/commit/f2c7937a7b4227bbaa72aa77c5036c3d6aa1517b) skip node local dns ip conntrack when acl is set: (#4810)
 * [249b3edc0](https://github.com/kubeovn/kube-ovn/commit/249b3edc00a9b9cfbaf1d4192611cf8f1abe0106) bump k8s to v1.30.7 (#4771)
 * [86794c287](https://github.com/kubeovn/kube-ovn/commit/86794c287c5141762e6ff4dbfe2e6e337ca06abd) bump dpdk base image to ubuntu 24.04 (#4770)
 * [9dc2ab28f](https://github.com/kubeovn/kube-ovn/commit/9dc2ab28f0642a981078cad7cd9e2e0c155ff972) prepare for next release

### Contributors

 * changluyi
 * 张祖建

## v1.12.29 (2024-11-25)

 * [803bceeb4](https://github.com/kubeovn/kube-ovn/commit/803bceeb43434012225be4de8f7bccf8516c5208) release v1.12.29
 * [e1c59860f](https://github.com/kubeovn/kube-ovn/commit/e1c59860fb9663300c4897073ab0699c41b395c6) skip conntrack when access node dns ip (#3894) (#4762)
 * [576dfd427](https://github.com/kubeovn/kube-ovn/commit/576dfd427a9d4ce052b61732c3739be704887b4d) add not found err check for lb-svc (#4748)
 * [b23c8da43](https://github.com/kubeovn/kube-ovn/commit/b23c8da43aff75d3cce8aa7c703d8d6bb5a2e488) [bugfix] Optimize gc method at  port group and node (#4722)
 * [b929a11d0](https://github.com/kubeovn/kube-ovn/commit/b929a11d09fa920a7c792e1e8393b06692d28f56) Revert "[bugfix] Unable to correctly gc port group (#4694)"
 * [714afd4d9](https://github.com/kubeovn/kube-ovn/commit/714afd4d959179f7cfcfd5d4aeb7bbbc28e7a2a8) kube-ovn-cni will panic if cidr is invalid (#4729)
 * [e12731ddb](https://github.com/kubeovn/kube-ovn/commit/e12731ddb33a2b69456779c76149246ea0d1cf84) fix build errors
 * [c3e8fb477](https://github.com/kubeovn/kube-ovn/commit/c3e8fb477067f272516da1ef585b03b60121862d) The eip is not cleaned after eip is deleted (#4718) (#4719)
 * [c328da4b6](https://github.com/kubeovn/kube-ovn/commit/c328da4b67a258ea846c3ee6ad22a64d9a1ef749) [bugfix] Unable to correctly gc port group (#4694)
 * [ee560e8d4](https://github.com/kubeovn/kube-ovn/commit/ee560e8d41fbb61c7b24f61262729818f0e39fab) VM live migrate (#3767)
 * [42f99a717](https://github.com/kubeovn/kube-ovn/commit/42f99a71787d572e84bfa97f8cac4699dd43cd4e) add process for lb-svc ports update (#4676)
 * [57c823967](https://github.com/kubeovn/kube-ovn/commit/57c823967f6549694432cfa356bbe20fcfbbb4f7) [bugfix] When add_eip, send a GARP for eip from net1 (#4701)
 * [4ee107a85](https://github.com/kubeovn/kube-ovn/commit/4ee107a85449e22051feb1c23999bff816294130) fix: udp bad checksum on VXLAN interface (#4639)
 * [4ba576c31](https://github.com/kubeovn/kube-ovn/commit/4ba576c31aff3cbc0bc6583705d8b1937b9a2e21) prepare for next release

### Contributors

 * Congqi Zhao
 * Guangyu Suo
 * Mengxin Liu
 * bobz965
 * changluyi
 * cmdy
 * hzma
 * xiaoyie

## v1.12.28 (2024-10-18)

 * [368e6effb](https://github.com/kubeovn/kube-ovn/commit/368e6effb26839fb25cbef724e9704ff92f44226) release v1.12.28
 * [aa7f680d8](https://github.com/kubeovn/kube-ovn/commit/aa7f680d803c0c609881ae68c8a218413654e48d) Refactor network policy matching logic (#4626)
 * [603be7210](https://github.com/kubeovn/kube-ovn/commit/603be72104f076d9c3cd79879dce677b5b00164c) team device not set unmanage (#4627)
 * [cd741a73b](https://github.com/kubeovn/kube-ovn/commit/cd741a73be66a4699fb2fd8b8180dfb1d6a1f404) prepare for next release

### Contributors

 * Mengxin Liu
 * changluyi

## v1.12.27 (2024-10-16)

 * [748f8189c](https://github.com/kubeovn/kube-ovn/commit/748f8189c996f58126a0002e23631effa89124b3) release v1.12.27
 * [6da2b51c8](https://github.com/kubeovn/kube-ovn/commit/6da2b51c835ca7e94cdae752f2cd4592aaf5a383) fix memory overflow, add mac_binding related options to router (#4603) (#4608)
 * [207fcfbc8](https://github.com/kubeovn/kube-ovn/commit/207fcfbc8b13e0cd94cb2e22d9f70f250e69cfa9) chart: add missing sa imagePullSecrets (#4594)
 * [e782ba081](https://github.com/kubeovn/kube-ovn/commit/e782ba08108639f7ec093a7f45c78ab8ec26e8ee) bump go to 1.22.8 (#4584)
 * [cbae706c3](https://github.com/kubeovn/kube-ovn/commit/cbae706c3ab1fc632edc854ea685f73472042ee8) Add genev_sys_6081 and vxlan_sys_4789 to cilium devices (#4575)
 * [1f7ca9d8f](https://github.com/kubeovn/kube-ovn/commit/1f7ca9d8feee3c402378a6b0022210c42387cf61) ci: set trivy db repository to public.ecr.aws/aquasecurity/trivy-db:2 (#4570)
 * [6ba85efb6](https://github.com/kubeovn/kube-ovn/commit/6ba85efb680a066bffb6a13c74018f058340a7f3) prepare for next release

### Contributors

 * Andrei Kvapil
 * changluyi
 * 张祖建

## v1.12.26 (2024-09-29)

 * [6b486523f](https://github.com/kubeovn/kube-ovn/commit/6b486523f83eca2b49801e925b8f4bdfcda83f96) release v1.12.26
 * [0078be3b9](https://github.com/kubeovn/kube-ovn/commit/0078be3b958f492cc8ce5f1d8b563cfc8abb332a) fix para input error
 * [f4a0c780f](https://github.com/kubeovn/kube-ovn/commit/f4a0c780f745c55449065d97f6654f7f5b822ba2) add values for OVSDB_CON_TIMEOUT and OVSDB_INACTIVITY_TIMEOUT (#4567)
 * [58bbdf50b](https://github.com/kubeovn/kube-ovn/commit/58bbdf50b5e4263408a57f5cf6e2c6baf1db4dbc) prepare for next release

### Contributors

 * changluyi
 * clyi

## v1.12.25 (2024-09-23)

 * [c23f44b93](https://github.com/kubeovn/kube-ovn/commit/c23f44b93c04082a91b2fdf3a1e47cb3ba591f64) release v1.12.25
 * [3130464a6](https://github.com/kubeovn/kube-ovn/commit/3130464a64adbc490fa17fa01f81c5f77848a7e0) fix (#4553)
 * [ff0e85b70](https://github.com/kubeovn/kube-ovn/commit/ff0e85b70f772bc853c09612d63820b3015a5105) fix only ipv6 mode should add ovn0 locallink ipv6 address (#4552)
 * [1f8b19166](https://github.com/kubeovn/kube-ovn/commit/1f8b191664e2f1d86bfdc1ff2c387ba101abd4f0) prepare for next release

### Contributors

 * changluyi

## v1.12.24 (2024-09-23)

 * [bc0a6f3c1](https://github.com/kubeovn/kube-ovn/commit/bc0a6f3c162c5783f443dda6c6d47de91abcb5e0) release v1.12.24
 * [55c9e0653](https://github.com/kubeovn/kube-ovn/commit/55c9e065305a5f7014c5389f89991cbdb6208a51) add ovn0 ipv6 local link address when ipv6 mode (#4545)
 * [1fe9e7c1a](https://github.com/kubeovn/kube-ovn/commit/1fe9e7c1a39037e740031b67536fe440412dcaae) base: rebuild go binary deps from source (#4524)
 * [a6617223d](https://github.com/kubeovn/kube-ovn/commit/a6617223dba7da9b1e00bcafe3f7f03d68af191c) prepare for next release

### Contributors

 * changluyi
 * zhangzujian

## v1.12.23 (2024-09-18)

 * [9f890e764](https://github.com/kubeovn/kube-ovn/commit/9f890e76474cda2690a941405bd3a705cced0c89) release v1.12.23
 * [65312b283](https://github.com/kubeovn/kube-ovn/commit/65312b2836c721e88874d1f9e757c4792583c950) bump multus to v4.1.1 (#4532)
 * [e98cc1974](https://github.com/kubeovn/kube-ovn/commit/e98cc19744b81e273e5723b5db29fd130eb84724) bump k8s to v1.30.5 (#4513)
 * [2e1887d9c](https://github.com/kubeovn/kube-ovn/commit/2e1887d9c62029bf5c78357deb12dbd4e2559df0) bump go to 1.22.7 (#4482)
 * [78b727980](https://github.com/kubeovn/kube-ovn/commit/78b72798070f4c624e0607a9bac5ae0ba5bb7e57) update Makefile (#4472)
 * [822019410](https://github.com/kubeovn/kube-ovn/commit/822019410073320a07b5de48cab9ecacf7110309) tproxy: support named port (#4487)
 * [723e8b3cb](https://github.com/kubeovn/kube-ovn/commit/723e8b3cb6c02234c1cefd46a3a9e4763fc7784b) fix: arping reply may duplicate (#4477)
 * [cbbe10ab3](https://github.com/kubeovn/kube-ovn/commit/cbbe10ab3190d596423007dff9d78142832b9ca6) Enable inactivity check on ovndb connection (#4006)
 * [0d8c1a9e3](https://github.com/kubeovn/kube-ovn/commit/0d8c1a9e334b00da588fa43df70f079059fdeb3f) support vip dual stack (#3617)
 * [2960014ef](https://github.com/kubeovn/kube-ovn/commit/2960014ef504eb8ecbdbcc166d79ba680dec6830) metrics: do not export information if a subnet is not validated (#4444)
 * [810d6dbc5](https://github.com/kubeovn/kube-ovn/commit/810d6dbc5a6d8b4662a89886a1bbc9d56d0cc9b7) prepare for next release

### Contributors

 * Mengxin Liu
 * fanriming
 * zhangzujian
 * 张祖建

## v1.12.22 (2024-08-29)

 * [618701059](https://github.com/kubeovn/kube-ovn/commit/618701059e376430a280c1403aebf1628d6bfae6) release v1.12.22
 * [820d95d04](https://github.com/kubeovn/kube-ovn/commit/820d95d04d9ccbbc016169d29df5506276a86f9d) vpc-nat-gateway: use iptables-legacy for centos 7 (#4428)
 * [b2b55a1c9](https://github.com/kubeovn/kube-ovn/commit/b2b55a1c941808fe775fa9433a1149fa02e25eca) netpol: add allow acl rules for u2o logical gateway (#4420)
 * [1ecbd5912](https://github.com/kubeovn/kube-ovn/commit/1ecbd59121d7526ca71bc315870016b83157ac93) Makefile: simplify underlay u2o installation (#4419)
 * [b2e6a7a51](https://github.com/kubeovn/kube-ovn/commit/b2e6a7a51171e73ffd04449abc1edbec26b498f0) vpc-nat-gateway: do not add routes for underlay subnets (#4416)
 * [c03dfd2ea](https://github.com/kubeovn/kube-ovn/commit/c03dfd2eaceaa52461c4032b34ac0265856daf89) cni-server: fix failure in ipv6/dual clusters running in docker (#4417)
 * [786eb0ab2](https://github.com/kubeovn/kube-ovn/commit/786eb0ab2a375527310cbcc349a33dc34320cb04) add acl log annotation (#4414)
 * [6d0666469](https://github.com/kubeovn/kube-ovn/commit/6d0666469d5a82a7b4e65e7c934c908a475ff0b2) bump k8s to v1.30.4 (#4410)
 * [1827b650e](https://github.com/kubeovn/kube-ovn/commit/1827b650edd49e8222a62ca00fa20d3f7febea30) fix kube-ovn-cni run fail on docker (#4406)
 * [ebd55782a](https://github.com/kubeovn/kube-ovn/commit/ebd55782a05227e450b4423c49faa64b7b896cda) prepare for next release

### Contributors

 * changluyi
 * hzma
 * zhangzujian
 * 张祖建

## v1.12.21 (2024-08-14)

 * [40ef8d6bc](https://github.com/kubeovn/kube-ovn/commit/40ef8d6bcd2edd4d8e52630e41787c70ebf1c0ba) release v1.12.21
 * [d4692dfe5](https://github.com/kubeovn/kube-ovn/commit/d4692dfe591053c6dec5c18573786d0ea00c778f) increase health probe timeout (#4397)
 * [909991a3f](https://github.com/kubeovn/kube-ovn/commit/909991a3feaaedbfa31159389a668a16d572ac85) cni-server: do not set sysctl variables (#4395)
 * [912177f3c](https://github.com/kubeovn/kube-ovn/commit/912177f3c52b5a088dec71a414334aa359217749) fix ovs patch
 * [cf95de51f](https://github.com/kubeovn/kube-ovn/commit/cf95de51f51518f4077faf339bb09c8877858ff7) prepare for next release

### Contributors

 * 张祖建

## v1.12.20 (2024-08-13)

 * [918652d60](https://github.com/kubeovn/kube-ovn/commit/918652d6044ba686ed59d71991a41ca007b13d63) release v1.12.20
 * [b867a33f3](https://github.com/kubeovn/kube-ovn/commit/b867a33f34964425a0b741fd3b85efa3c872cc46) increase the default probe interval for large cluster
 * [1562b2281](https://github.com/kubeovn/kube-ovn/commit/1562b228101ead4caba0eda31d0c18ed3c8416d4) install.sh: add option SECURE_SERVING
 * [87e756598](https://github.com/kubeovn/kube-ovn/commit/87e756598835ae7e40d646d7e82fc8fe64250b39) fix EOF during TLS handshake caused by health check (#4381)
 * [39980fb0e](https://github.com/kubeovn/kube-ovn/commit/39980fb0eba7f64d0c4e25fcf961581651715f2c) fix controller-runtime logger not set (#4380)
 * [fa2316a9c](https://github.com/kubeovn/kube-ovn/commit/fa2316a9c0a3b1c7c5d82b030e9a13fb4a686207) bump go to 1.22.6 (#4373)
 * [fe218b09f](https://github.com/kubeovn/kube-ovn/commit/fe218b09f6cfd92c94f59c204306b87635ec2ec4) replace protocol check in netpol update (#4356)
 * [3133267c7](https://github.com/kubeovn/kube-ovn/commit/3133267c7087afdcc0c2ddca10e9e2bd14daa45a) build(deps): bump github.com/prometheus-community/pro-bing (#4339)
 * [541d5f450](https://github.com/kubeovn/kube-ovn/commit/541d5f4508d0914229f6f901276e2404396c318d) cni-server: disable udp-fragmentation-offload (#4342)
 * [258408ec6](https://github.com/kubeovn/kube-ovn/commit/258408ec606fa1e6cdcebec11206d11a788b85b2) build(deps): bump github.com/onsi/gomega from 1.34.0 to 1.34.1 (#4350)
 * [a9784c7b6](https://github.com/kubeovn/kube-ovn/commit/a9784c7b6b59e823f9859c169f701666431c5153) build(deps): bump github.com/onsi/ginkgo/v2 from 2.19.0 to 2.19.1 (#4340)
 * [7c5031071](https://github.com/kubeovn/kube-ovn/commit/7c5031071e3d932096d4f67e3d663df2156c8cb5) build(deps): bump github.com/onsi/gomega from 1.33.1 to 1.34.0 (#4341)
 * [642c2fc71](https://github.com/kubeovn/kube-ovn/commit/642c2fc7110f846db3daa89c35b4e7b0d012c8f9) build(deps): bump github.com/docker/docker (#4336)
 * [756c47a7c](https://github.com/kubeovn/kube-ovn/commit/756c47a7c0ad72631306119029fa91c72ccd1489) build(deps): bump github.com/containernetworking/cni from 1.2.2 to 1.2.3 (#4328)
 * [341ff27ff](https://github.com/kubeovn/kube-ovn/commit/341ff27ffec7a7267181ad19c38a84de820b7c39) build(deps): bump github.com/docker/docker (#4327)
 * [2e76fe9c1](https://github.com/kubeovn/kube-ovn/commit/2e76fe9c1bc98592a1eac1a20d6489a04cc65042) pinger: fix process not terminated on sigkill (#4329)
 * [7719ce574](https://github.com/kubeovn/kube-ovn/commit/7719ce5746134d7ef518975039a6d8cc8dfa4c2f) fix: scripts (#4291)
 * [4e39bd857](https://github.com/kubeovn/kube-ovn/commit/4e39bd85781de4fde4954feabc8d37bbc7292b1f) fix dialing https server (#4324)
 * [8cd578c68](https://github.com/kubeovn/kube-ovn/commit/8cd578c683ff35ccc94c5b2b564add3bd778a08b) refactor metrics (#4313)
 * [e0d1dbdbf](https://github.com/kubeovn/kube-ovn/commit/e0d1dbdbfce08bbc869ab32ee3f080590c928929) bump k8s to v1.30.3 (#4317)
 * [68797a103](https://github.com/kubeovn/kube-ovn/commit/68797a10328a5646700f7dafc76e8912d71061f1) metrics: fix missing rbac for sa ovn (#4312)
 * [10781730c](https://github.com/kubeovn/kube-ovn/commit/10781730cbc9b06325fe9a4655d1782f621ecb30) metrics: add support for secure serving (#4297)
 * [b57721129](https://github.com/kubeovn/kube-ovn/commit/b57721129731cf7be0bf604159ff1b917b228a8a) security: run as unprivileged (#3040)
 * [3e52a0ee6](https://github.com/kubeovn/kube-ovn/commit/3e52a0ee6489b7fec0221821183430d8e749a8ba) bump k8s to v1.27.16 (#4306)
 * [b88ed8d21](https://github.com/kubeovn/kube-ovn/commit/b88ed8d21a4f7bac6646e194c13fdf8637dada92) fix map concurrent read and write crash (#4302)
 * [2ded787af](https://github.com/kubeovn/kube-ovn/commit/2ded787af86caf1cd60b9250b55681eabc50087a) use route policy to reimplement northGateway
 * [69befe33e](https://github.com/kubeovn/kube-ovn/commit/69befe33ed200a1d133f632ded5b6c5b207d1aba) underlay: set trunks of host nic port (#4282)
 * [79cf20988](https://github.com/kubeovn/kube-ovn/commit/79cf20988f45ba1bb645337c79686c4d48919359) fix using route table name as vpc name
 * [bf5a554ec](https://github.com/kubeovn/kube-ovn/commit/bf5a554eca05944a0a96faa932582d1bcd9f1789) fix: set dhcp gateway to U2OInterconnectionIP when enabling dhcp and u2o (#4228)
 * [54aa76f00](https://github.com/kubeovn/kube-ovn/commit/54aa76f00266c51ae969a5355af1165857c1f267) fix ovn lb not updated due to service update failure (#4280)
 * [e4b4142c7](https://github.com/kubeovn/kube-ovn/commit/e4b4142c745342faa7f7b855dc0d7b0bfb808e09) build(deps): bump aquasecurity/trivy-action from 0.23.0 to 0.24.0 (#4275)
 * [0691a8943](https://github.com/kubeovn/kube-ovn/commit/0691a89436af6a3c41483544207928047efd7a46) prepare for next release

### Contributors

 * Mengxin Liu
 * Zhao Congqi
 * dependabot[bot]
 * hzma
 * zhangzujian
 * 张祖建

## v1.12.19 (2024-07-10)

 * [93095c8c9](https://github.com/kubeovn/kube-ovn/commit/93095c8c932bbcaa92d428900f29ff4af02b682f) release v1.12.19
 * [cbddeea4d](https://github.com/kubeovn/kube-ovn/commit/cbddeea4d309887de1f9a5afae4a9333a22aedcd) update .trivyignore
 * [d8b4a77c9](https://github.com/kubeovn/kube-ovn/commit/d8b4a77c957fb7f3a9aaf601ae57b58e0f27a127) ci: run go mod tidy before building kubectl (#4274)
 * [1dc25c44c](https://github.com/kubeovn/kube-ovn/commit/1dc25c44c5742d4e045346a5438184481e321861) ci: disable cgo when building kubelet and cni plugins (#4268)
 * [a66dbff18](https://github.com/kubeovn/kube-ovn/commit/a66dbff185b93a1ec69f65b7601af8e012a17ce0) build kubectl and cni plugins from source if vuln found in the base image (#4253)
 * [39cfac4fd](https://github.com/kubeovn/kube-ovn/commit/39cfac4fdd9342403bf60e9f096ddf8f21b10c10) ignore cve CVE-2024-24791
 * [4877acde7](https://github.com/kubeovn/kube-ovn/commit/4877acde748f3240893991ed9a67aab9fdd8b6e0) bump go version
 * [1b16809cf](https://github.com/kubeovn/kube-ovn/commit/1b16809cfcdd7c852ab4f5c8a415f7375882d7a0) enable nat gw by default (#4273)
 * [1e53391e4](https://github.com/kubeovn/kube-ovn/commit/1e53391e48d2ba6360e18c56855724e57eb61461) ipam: fix ip not released for non-ovn subnets (#4265)
 * [f151e32e3](https://github.com/kubeovn/kube-ovn/commit/f151e32e378ea268458998e570e52edbaf32806e) klog: set log file max size to 200MB (#4272)
 * [c2e5a0610](https://github.com/kubeovn/kube-ovn/commit/c2e5a06104b34bd8d7e66e502d6b9d3e9886fc29) logrotate: set file size limit to 100M (#4271)
 * [05eb0e166](https://github.com/kubeovn/kube-ovn/commit/05eb0e1661e81e49f48369f38898935094d08dec) remove unused environment variable LOG_ROTATE (#4270)
 * [331006f7c](https://github.com/kubeovn/kube-ovn/commit/331006f7c64bcbaaa95eac71a4d1ffc596cc95ea) when router is deleted return success for static route deletion (#4266)
 * [b16adb60b](https://github.com/kubeovn/kube-ovn/commit/b16adb60b30fdf529b94d4cfc15b59c9b6242574) fix vpcnatgw image is not synced (#4264)
 * [f56934673](https://github.com/kubeovn/kube-ovn/commit/f56934673e789435e87fa2bb0b3d3bb4d81d78ba) build(deps): bump golang.org/x/sys from 0.21.0 to 0.22.0 (#4257)
 * [38db3cf8b](https://github.com/kubeovn/kube-ovn/commit/38db3cf8b797e24125cdd624c566ac84299fd971) build(deps): bump golang.org/x/mod from 0.18.0 to 0.19.0 (#4256)
 * [d47451394](https://github.com/kubeovn/kube-ovn/commit/d474513941e2215fc6557dc8758d6b0cc66a2d08) build(deps): bump github.com/osrg/gobgp/v3 from 3.27.0 to 3.28.0 (#4244)
 * [5d16919c0](https://github.com/kubeovn/kube-ovn/commit/5d16919c0a66e829b0a3b8a732d7c10cb4f9631b) do not create iptables rule for setting tcp mss (#4260)
 * [c12c57227](https://github.com/kubeovn/kube-ovn/commit/c12c5722704d804106322ba2150ab5f814fe67ba) fix invalid subnet not sync route (#4262)
 * [7743d6660](https://github.com/kubeovn/kube-ovn/commit/7743d6660a644244702106c6f2172aa142b99690) update node name label for ip resources (#4242)
 * [c69f9d432](https://github.com/kubeovn/kube-ovn/commit/c69f9d432e22916e58c60b00a2f98ecb5506c33a) lb svc: update svc status after configuring nat rules (#4235)
 * [d7b160d5c](https://github.com/kubeovn/kube-ovn/commit/d7b160d5cedd38bed35de65e84c3fd219185f046) vpc-nat-gateway: print messgae to stderr (#4237)
 * [217ea26ab](https://github.com/kubeovn/kube-ovn/commit/217ea26abcdb418317c8b737c5d5adf0257d0b0d) check both sts name and UID when handling pod deletion (#4238)
 * [35563ed5d](https://github.com/kubeovn/kube-ovn/commit/35563ed5d340db9253c654c62b829149d95f23ff) do not exit if chassis is not found (#4246)
 * [d4002b81a](https://github.com/kubeovn/kube-ovn/commit/d4002b81aa4a545b69c1ffdc99934137decb1a89) ci: do not compile fastpath kernel module for centos (#4247)
 * [03ce5a328](https://github.com/kubeovn/kube-ovn/commit/03ce5a328732e3e20d6916cdb46bd58702751778) update node labels/annotations by json merge patch (#4230)
 * [c893f524c](https://github.com/kubeovn/kube-ovn/commit/c893f524c396ef599e5b2621c0fc1833ce4cee22) fix ipv6 service ip not added to ovn lb vips due to pod cache not synced (#4223)
 * [6c467fa69](https://github.com/kubeovn/kube-ovn/commit/6c467fa696fb3101a37bdc90db584b9e2e69b260) ic: ensure db file is fixed (#4211)
 * [194e61558](https://github.com/kubeovn/kube-ovn/commit/194e615580e42c2d52aadf058c5a3a71b803a9bf) base: clean ipset deb files (#4215)
 * [b1b7d3b11](https://github.com/kubeovn/kube-ovn/commit/b1b7d3b119a92085c3e70f87d86e9189f648588a) fix getting service cluster ips (#4206)
 * [8761e3c68](https://github.com/kubeovn/kube-ovn/commit/8761e3c687c03fb2b187ae67bc28adf32ad47bac) fix: nil pointer when subnet is not ready (#4190)
 * [8708c7223](https://github.com/kubeovn/kube-ovn/commit/8708c7223dbbc520747b9b019b1f019cb809f7bd) base: add traceroute
 * [289c55260](https://github.com/kubeovn/kube-ovn/commit/289c55260224cbea703d1485b576e210d39ae0dc) prepare for next release

### Contributors

 * Mengxin Liu
 * Zhao Congqi
 * changluyi
 * dependabot[bot]
 * zhangzujian
 * 张祖建

## v1.12.18 (2024-06-21)

 * [8186a8a22](https://github.com/kubeovn/kube-ovn/commit/8186a8a22044e8277731fadfd0a9bb0cdfc8c70a) release v1.12.18
 * [65641d31d](https://github.com/kubeovn/kube-ovn/commit/65641d31daa25e85d46f40d6a5e7f8a36afd4cc8) fix vm not running after changing the subnet (#4199)
 * [10cd0ce6a](https://github.com/kubeovn/kube-ovn/commit/10cd0ce6ad70d735f640920742f44c7b5653291e) pinger: reset interface_rx_multicast_packets (#4198)
 * [05e2ccb51](https://github.com/kubeovn/kube-ovn/commit/05e2ccb5127ef745183312b0c8089a1949853e8d) fix kube-ovn-cni crash for newly added nodes , due to old legacy event in deleteNodeQueue (#4194)
 * [ff0a7c535](https://github.com/kubeovn/kube-ovn/commit/ff0a7c535b8f632e4804bb0b0082c0dca3a767ae) base: bump cni plugins to v1.5.1 (#4185)
 * [4a662e221](https://github.com/kubeovn/kube-ovn/commit/4a662e221ad714a7d9cd1529d52a18a71dfd7a2e) ci: check pod crashes on installation/e2e failure (#4160)
 * [b7bf4926a](https://github.com/kubeovn/kube-ovn/commit/b7bf4926a7676c6a1f93fac7196ff4ab92624438) base: bump kubectl to v1.30.2 (#4163)
 * [342a2ab87](https://github.com/kubeovn/kube-ovn/commit/342a2ab877940069c326224ffa0ea201cc4e8419) fix reconcile routes (#4168)
 * [8965ecce5](https://github.com/kubeovn/kube-ovn/commit/8965ecce5f0f439c9963e28aed4a5e2099ced4d5) ci: fix retrieving docker network subnet/gateway (#4161)
 * [63b336915](https://github.com/kubeovn/kube-ovn/commit/63b3369153ad3229c52b8b44be242356e330e970) prepare for next release

### Contributors

 * changluyi
 * 张祖建

## v1.12.17 (2024-06-13)

 * [2b643c97d](https://github.com/kubeovn/kube-ovn/commit/2b643c97dc40b1a7293bb2a80a49208b6d35dc0f) release v1.12.17
 * [19f91543c](https://github.com/kubeovn/kube-ovn/commit/19f91543c1a3f996b80b8c24893ecfb720f85180) Drop u2o arp request 1.12 (#4150)
 * [e84ab121e](https://github.com/kubeovn/kube-ovn/commit/e84ab121e5d6b0fb3797a8107a732b252fe0c0ff) add ovn0 default route (#4127)
 * [acab364f1](https://github.com/kubeovn/kube-ovn/commit/acab364f1dcb854917ae3aaead2ba2acabb014b8) distinguish-portSecurity-with-security-group (#4134)
 * [e53db6edd](https://github.com/kubeovn/kube-ovn/commit/e53db6edd650f6538bb0437c12e618bd48f69bd7) fix ipam subnet concurrent map iteration and map write (#4126)
 * [9e8c5eebf](https://github.com/kubeovn/kube-ovn/commit/9e8c5eebf831f77fc8197aef6b6304d445eeef9a) trivy: ignore unfixed CVEs (#4129)
 * [f266b4eb5](https://github.com/kubeovn/kube-ovn/commit/f266b4eb507c4baae7137e9bf7f40ad5da0172b6) bump go to 1.22.4 (#4121)
 * [9077beb57](https://github.com/kubeovn/kube-ovn/commit/9077beb575fe219c34ba1c35a08832f6f35dc873) build(deps): bump golang.org/x/sys from 0.20.0 to 0.21.0 (#4118)
 * [627c42131](https://github.com/kubeovn/kube-ovn/commit/627c42131a8b4b82c609c4c11f29fdc46998639a) build(deps): bump github.com/osrg/gobgp/v3 from 3.26.0 to 3.27.0 (#4119)
 * [b496e60e4](https://github.com/kubeovn/kube-ovn/commit/b496e60e4f99a77d57c17286ba79dc8400e3e5da) fix: IP Add/Sub overflow or underflow (#4111)
 * [7b4ff3abd](https://github.com/kubeovn/kube-ovn/commit/7b4ff3abda108fb8ee7ef0b7c7d0474acd2887a0) add enable multicast snoop to 1.12 (#4105)
 * [2286ffa54](https://github.com/kubeovn/kube-ovn/commit/2286ffa54ed61c3f0a657f0cd2014d14bab3b9e5) fix: remove change vm subnet directly (#4102)
 * [b4f54daf7](https://github.com/kubeovn/kube-ovn/commit/b4f54daf7e3ff02de112f35b1e5c184837df2a1b) Makefile: run kubectl-ko script when collecting logs (#4100)
 * [64d1e1cdb](https://github.com/kubeovn/kube-ovn/commit/64d1e1cdbbfe275992b181deea1c49c0e4fa7124) install.sh: waiting for deleted kube-ovn-pinger to disapper (#4096)
 * [cde29e90b](https://github.com/kubeovn/kube-ovn/commit/cde29e90b87365015c0404f36e41d44c3151a37f) fix mac conflict (#4095)
 * [4fe44ab3a](https://github.com/kubeovn/kube-ovn/commit/4fe44ab3ace3b96e9c30e7090b6e7891b0591c42) fix: add static route for custom vpc when create subnet with (#3462)
 * [2d667f5bd](https://github.com/kubeovn/kube-ovn/commit/2d667f5bde52472305d5e590bfc4be55ecd1f8c8) fix: add ip_reserved label for vip (#4093)
 * [a874aaf7f](https://github.com/kubeovn/kube-ovn/commit/a874aaf7f659da431d94f9135dedcf06c2c1d33b) install.sh: wait for all kube-ovn-pinger pods to be ready (#4082)
 * [d2e3bf651](https://github.com/kubeovn/kube-ovn/commit/d2e3bf6512bc1a1f0f4dd269c28156aadbf2b930) ci: print all the previous logs for restarted pods (#4081)
 * [f271a54df](https://github.com/kubeovn/kube-ovn/commit/f271a54df53de66174cff005e82020e12ef77e6c) opt: replace ovn-sbctl with ovsdb-client (#4075)
 * [2653759af](https://github.com/kubeovn/kube-ovn/commit/2653759afc16cae7525706ae01d049569b50c852) ovs: get controllerrevision with option --ignore-not-found (#4058)
 * [e0908bfb4](https://github.com/kubeovn/kube-ovn/commit/e0908bfb4c879b567e1a94379f3457a2c503724c) fix exit on error (#4080)
 * [6d7ed5d07](https://github.com/kubeovn/kube-ovn/commit/6d7ed5d0799fba50491905276ede5a050768387b) delete lease on cleanup (#4079)
 * [fcde92c1f](https://github.com/kubeovn/kube-ovn/commit/fcde92c1f78a17c471136e96c6c8dc2c64d0c8ce) base: aoivd unnecessary env variables (#4070)
 * [38fcd9e1f](https://github.com/kubeovn/kube-ovn/commit/38fcd9e1f1895b3653e792b3cc7a99944565f9fa) ci: downgrade node image to v1.29.2 (#4069)
 * [b1237f9a6](https://github.com/kubeovn/kube-ovn/commit/b1237f9a64ac230896f23ae03ca4531208f08a9a) fix crypto/rand: argument to Int is <= 0 (#4077)
 * [e82c42782](https://github.com/kubeovn/kube-ovn/commit/e82c42782b80676d18eac56c1addea7ea1fe1f90) fix backport (#4066)
 * [1ba289cb2](https://github.com/kubeovn/kube-ovn/commit/1ba289cb214c6d03af859eb47627656a445fccb8) fix: should update subnet status after change vm subnet (#4061)
 * [de09d72b4](https://github.com/kubeovn/kube-ovn/commit/de09d72b410e6b2878424024b4cd3b4dc01dd1bb) ci: build e2e binaries and free disk space on necessary (#4059)
 * [94c22af43](https://github.com/kubeovn/kube-ovn/commit/94c22af4332d21d9d9cb19fa0aed1bbc62b0e11e) crd: add subnet name pattern (#4054)
 * [f69625d12](https://github.com/kubeovn/kube-ovn/commit/f69625d123afa2633bc67115325fc1e26e45e2e5) fix assignment to entry in nil map (#3925)
 * [623c62873](https://github.com/kubeovn/kube-ovn/commit/623c62873c3d8227829edccb0aad0821b3d48103) add release cleanup
 * [81b8a9de5](https://github.com/kubeovn/kube-ovn/commit/81b8a9de5035ff604eeebf5b0c720629687d9e4d) prepare for next release

### Contributors

 * Mengxin Liu
 * Zhao Congqi
 * bobz965
 * changluyi
 * dependabot[bot]
 * fanriming
 * hzma
 * xujunjie-cover
 * zhangzujian
 * 张祖建

## v1.12.16 (2024-05-20)

 * [dff82c4f3](https://github.com/kubeovn/kube-ovn/commit/dff82c4f3c1d94cc7fbc874541a98b5bb5cead22) release v1.12.16
 * [3957427d7](https://github.com/kubeovn/kube-ovn/commit/3957427d71e12599766b85bc40c604ca55bc95fa) wait for all pods to be deleted before deleting serviceaccount/clusterrole/clusterrolebinding (#4035)
 * [b77279882](https://github.com/kubeovn/kube-ovn/commit/b77279882098824528991f9acce04a8032b16be5) uninstall.sh: delete OVN-POSTROUTING rule in mangle table (#4034)
 * [3cb629146](https://github.com/kubeovn/kube-ovn/commit/3cb629146b49b2c50e34ea5705fd3e65a55bf095) cleanup.sh: remove sa/clusterrole/clusterrolebinding (#4024)
 * [70d33bef6](https://github.com/kubeovn/kube-ovn/commit/70d33bef63532ee8ea1d4a6c31e54fcffe9ea3ce) do not use exec for start scripts with trap quit EXIT (#4025)
 * [93e313774](https://github.com/kubeovn/kube-ovn/commit/93e313774c7163d95c0a65511c29308fd37b2dd4) bump k8s to 1.27.14 (#4029)
 * [85009d5e3](https://github.com/kubeovn/kube-ovn/commit/85009d5e3da2e7aa4d3c0d5682253800bdc7298d) fix node annotations not updated when initializing the default provider-network (#4030)
 * [4de2e0944](https://github.com/kubeovn/kube-ovn/commit/4de2e09447eaaefc1e627eaadd47dfbf32d89029) fix container args (#4020)
 * [2e700adc0](https://github.com/kubeovn/kube-ovn/commit/2e700adc02d6f1e4d0819e943921df9b6763290e) fix lsp not updated correctly when logical switch is changed (#4015)
 * [edee1e544](https://github.com/kubeovn/kube-ovn/commit/edee1e5446a20032d32441a836f14af2951cc838) base: set entrypoint to  dumb-init (#4018)
 * [03834d807](https://github.com/kubeovn/kube-ovn/commit/03834d807b033d50eceae390dd41352b8e55ad54) fix: Resolved the hidden issue with zombie processes (#4004)
 * [fcfc09951](https://github.com/kubeovn/kube-ovn/commit/fcfc0995180c49c19b3b084391200241c008b721) simplify file reading (#4010)
 * [d8c8f8ac5](https://github.com/kubeovn/kube-ovn/commit/d8c8f8ac5c98066c7dd3f52046085a13dc2c7b4a) prepare for next release

### Contributors

 * fanriming
 * 张祖建

## v1.12.15 (2024-05-13)

 * [ad0849135](https://github.com/kubeovn/kube-ovn/commit/ad08491359768c16d4ec5a790db7a0875d3e86b7) release v1.12.15
 * [505c041d2](https://github.com/kubeovn/kube-ovn/commit/505c041d261babc54aeb1206069bdbd42f61bba8) fix lsp not updating addresses (#4011)
 * [8d4738c45](https://github.com/kubeovn/kube-ovn/commit/8d4738c45c38df56386caeb4cf6910168c81ea32) bump gosec to 2.19.0
 * [be8def373](https://github.com/kubeovn/kube-ovn/commit/be8def3735c3cab7a3dd048614ffc58f4307d400) fix: close file (#4007)
 * [b47f2cefa](https://github.com/kubeovn/kube-ovn/commit/b47f2cefa99aebc1890d44e212c6f0f37b1f4706) fix node gc (#3992)
 * [2b5be24b8](https://github.com/kubeovn/kube-ovn/commit/2b5be24b8c9ed529f78005b4cc3e7dc255d69c52) bump go to 1.22.3 (#3989)
 * [6df85a023](https://github.com/kubeovn/kube-ovn/commit/6df85a023dae6dc9a7eea7ed2cca14997050a6f6) build(deps): bump google.golang.org/protobuf from 1.34.0 to 1.34.1 (#3981)
 * [ad9ee0883](https://github.com/kubeovn/kube-ovn/commit/ad9ee0883ff9627dfb16e7575f9ae26f4a7fa83d) build(deps): bump golang.org/x/sys from 0.19.0 to 0.20.0 (#3980)
 * [e77634dfa](https://github.com/kubeovn/kube-ovn/commit/e77634dfa97b7b2af79b2c7dc6ecda52380587eb) prepare for next release

### Contributors

 * dependabot[bot]
 * guangwu
 * zhangzujian
 * 张祖建

## v1.12.14 (2024-05-07)

 * [8461d3811](https://github.com/kubeovn/kube-ovn/commit/8461d38111e133d78de2b4198b6ac5319738d625) release v1.12.14
 * [99fda1eff](https://github.com/kubeovn/kube-ovn/commit/99fda1effbdf20f6dc0cf12b93b0a15c6e94a5bf) ignore CVEs in CNI plugins
 * [70871e720](https://github.com/kubeovn/kube-ovn/commit/70871e720dda180bfb35590fd32934ac5c0db4ad) ipam: fix IPRangeList clone (#3979)
 * [12b85e500](https://github.com/kubeovn/kube-ovn/commit/12b85e500ec0fe8e5a06559cf14f9f5e65f2c7d9) remove unused e2e test cases (#3968)
 * [fd2874a9e](https://github.com/kubeovn/kube-ovn/commit/fd2874a9e3bf2bf1b38e037690a729ab227a7be3) chart: fix kubeVersion to allow for patterns that match sub versions (#3975)
 * [4b2d5cbaa](https://github.com/kubeovn/kube-ovn/commit/4b2d5cbaa6ed7ccffa50b807534a398ce0a16525) prepare for next release

### Contributors

 * Joachim Hill-Grannec
 * zhangzujian
 * 张祖建

## v1.12.13 (2024-04-30)

 * [ec3c9282c](https://github.com/kubeovn/kube-ovn/commit/ec3c9282cfe5fdd400b045d3efe39cded184fca0) release v1.12.13
 * [e89577995](https://github.com/kubeovn/kube-ovn/commit/e89577995f15ac5c7d72822b112aa25dd905fea5) bump k8s to v1.27.13 (#3963)
 * [8e962a02a](https://github.com/kubeovn/kube-ovn/commit/8e962a02af7f7aaae4814b63687af9239f318ef2) fix subnet check ip in using to avoid ipam init (#3964)
 * [954df535d](https://github.com/kubeovn/kube-ovn/commit/954df535dbddd49cfb154a65dab78a181fd610d3) add patch permission for cni clusterrole
 * [db7ea0c69](https://github.com/kubeovn/kube-ovn/commit/db7ea0c69e9b40a33a32eebab5b62e9a4c7b46e2) fix index out of range (#3958)
 * [bfe803347](https://github.com/kubeovn/kube-ovn/commit/bfe8033473dd799fe6bd2694de38b5a16ad9fce6) fix nil pointer dereference (#3951)
 * [72106744c](https://github.com/kubeovn/kube-ovn/commit/72106744c59a9f4623354baaaf94baa08b2502ab) fix: lower camel case (#3942)
 * [5fccb52e4](https://github.com/kubeovn/kube-ovn/commit/5fccb52e4042c7c981d8d368393a1ceeeb402bd9) prepare for next release

### Contributors

 * Zhao Congqi
 * bobz965
 * 张祖建
 * 马洪贞

## v1.12.12 (2024-04-23)

 * [0363e783f](https://github.com/kubeovn/kube-ovn/commit/0363e783f66fdd1f53ec23b8c247fb1191a19e4a) release v1.12.12
 * [dcca4e549](https://github.com/kubeovn/kube-ovn/commit/dcca4e549309e67ba2ba8400c070e00de5b87e59) drop both IPv4 and IPv6 traffic in networkpolicy drop acl (#3940)
 * [4ff20a51f](https://github.com/kubeovn/kube-ovn/commit/4ff20a51f89cebbcca8b87e3de3b4ef9e23cf120) update net package version (#3936)
 * [bae875607](https://github.com/kubeovn/kube-ovn/commit/bae875607f619c63059d9fd7a5e002973c419d58) add metric for subnet info (#3932)
 * [166d9a433](https://github.com/kubeovn/kube-ovn/commit/166d9a433b388e2c9a6e1a4ac92218596caa5903) cni-server: set sysctl variables only when the env variables are passed in (#3929)
 * [86be0f678](https://github.com/kubeovn/kube-ovn/commit/86be0f6783cd630a36a8935344b8f6082defa770) ovn: check whether db file is fixed (#3928)
 * [021b99628](https://github.com/kubeovn/kube-ovn/commit/021b996285dcfd67d15043e5ce25ae89fe806d1a) fix backport (#3924)
 * [4ed15b961](https://github.com/kubeovn/kube-ovn/commit/4ed15b961cc9d52f0dc6442301d48cd8af23e17d) append filepath when compare cni config (#3923)
 * [589329223](https://github.com/kubeovn/kube-ovn/commit/5893292232518f1f78fd7986b9bf8e98b025595a) re calculate subnet using ips while inconsistency detected (#3920)
 * [698c93468](https://github.com/kubeovn/kube-ovn/commit/698c9346826feca5aa361055a76dc94bec5b62e8) update ovn monitor (#3903)
 * [c91d5b667](https://github.com/kubeovn/kube-ovn/commit/c91d5b667bf6cd05660566ec838c5ce0c69d7c36) add monitor for sysctl para (#3913)
 * [36025b576](https://github.com/kubeovn/kube-ovn/commit/36025b576b188eff5a893eba1edb23b344d54351) refactor kubevirt vm e2e (#3914)
 * [b32f607db](https://github.com/kubeovn/kube-ovn/commit/b32f607db7e0a7bef29c965ba35af0e4dbb75ad2) support specifying routes when providing IPAM for other CNI plugins (#3904)
 * [96a569576](https://github.com/kubeovn/kube-ovn/commit/96a5695765c7e8daae2c3fb5813987623800f4fb) Fix init sg (#3890)
 * [abd735c61](https://github.com/kubeovn/kube-ovn/commit/abd735c6153c7239d9bc3f49e572ff2b223b1724) distinguish portSecurity with security group (#3862)
 * [d8650a66d](https://github.com/kubeovn/kube-ovn/commit/d8650a66d10ded318300660e9e3ec19c816cfece) fix br-external not init because of  no permission after ovn-nat-gw configmap created (#3902)
 * [63fa192fd](https://github.com/kubeovn/kube-ovn/commit/63fa192fd68c06ae57eaaf24cbdc95fe0af46448) build(deps): bump google.golang.org/grpc from 1.63.0 to 1.63.2 (#3900)
 * [f8bac0733](https://github.com/kubeovn/kube-ovn/commit/f8bac073322664a852b7391d7ab377ae5d2570e5) build(deps): bump github.com/osrg/gobgp/v3 from 3.24.0 to 3.25.0 (#3897)
 * [11eae635b](https://github.com/kubeovn/kube-ovn/commit/11eae635b4210deb2bccdf9858d478034ab434ce) build(deps): bump golang.org/x/sys from 0.18.0 to 0.19.0 (#3896)
 * [2a339a658](https://github.com/kubeovn/kube-ovn/commit/2a339a6586dcb3d16726222a0e1765ec092d99ee) build(deps): bump golang.org/x/mod from 0.16.0 to 0.17.0 (#3895)
 * [696d9f47f](https://github.com/kubeovn/kube-ovn/commit/696d9f47f18a60ba3f2a99b43da74ab0c1deb9ff) build(deps): bump google.golang.org/grpc from 1.62.1 to 1.63.0 (#3898)
 * [beb224519](https://github.com/kubeovn/kube-ovn/commit/beb224519a7ec27edb0b851dffaed831ab9a0cfe) build(deps): bump github.com/Microsoft/hcsshim from 0.12.1 to 0.12.2 (#3891)
 * [c4be969c4](https://github.com/kubeovn/kube-ovn/commit/c4be969c4763f4d708b55f18f1d1326831a11616) build(deps): bump github.com/cenkalti/backoff/v4 from 4.2.1 to 4.3.0 (#3875)
 * [f8655fb65](https://github.com/kubeovn/kube-ovn/commit/f8655fb655ebdad0beacb831d171a264656c44bd) chart: fix ovs-ovn update strategy (#3887)
 * [b43ac43cb](https://github.com/kubeovn/kube-ovn/commit/b43ac43cb9a646c0a122e5d75c1fc8abf626eb1d) Fix index out of range in controller security_group (#3845)
 * [6609c81af](https://github.com/kubeovn/kube-ovn/commit/6609c81af4d9fe7fc25052c58fc00e39abc75fc0) fix: ipam invalid memory address or nil pointer dereference (#3889)
 * [c207ecae8](https://github.com/kubeovn/kube-ovn/commit/c207ecae8f9cb76d27a0ef19e5963e0701ae5b61) add tracepath (#3884)
 * [054f1298d](https://github.com/kubeovn/kube-ovn/commit/054f1298de926a6cc726ff1685bb2472679ece85) prepare for next release

### Contributors

 * Longchuanzheng
 * Zhao Congqi
 * bobz965
 * dependabot[bot]
 * hzma
 * zhangzujian
 * 张祖建

## v1.12.11 (2024-03-28)

 * [66df5d63f](https://github.com/kubeovn/kube-ovn/commit/66df5d63f62c33f4271f2f4ea44c5d928610a83c) release v1.12.11
 * [db92bb99e](https://github.com/kubeovn/kube-ovn/commit/db92bb99e1fc312240e97bc9aed158d2b861320e) change northd probe interval to 5s (#3882)
 * [d4f8ebe46](https://github.com/kubeovn/kube-ovn/commit/d4f8ebe46864516ced47229832f0c85b9066768c) fix go fmt
 * [64cd8d3a6](https://github.com/kubeovn/kube-ovn/commit/64cd8d3a6e7472ab923ee93d73627e219392f964) ovn: reduce down time during upgrading from version 21.06 (#3881)
 * [b347841af](https://github.com/kubeovn/kube-ovn/commit/b347841af7d4eb07a8f23f8d8197fcc2d728e485) compile binaries with debug symbols for debug images (#3871)
 * [94041b19e](https://github.com/kubeovn/kube-ovn/commit/94041b19efbd7825601b50e57ba48c1d06fcb0be) ovn: update patch for skipping ct (#3879)
 * [9b183d2e0](https://github.com/kubeovn/kube-ovn/commit/9b183d2e003048ef46b54b719ed226804fefb2df) ci: fix memory leak reporting caused by ovn-controller crashes (#3873)
 * [720e5f62d](https://github.com/kubeovn/kube-ovn/commit/720e5f62d18c8253ef2d9df8ee1977a5216ca153) Update dependencies in go.mod file
 * [ccefae1de](https://github.com/kubeovn/kube-ovn/commit/ccefae1de9ca40260cfe366f590270853bbcf022) prepare for next release

### Contributors

 * changluyi
 * zhangzujian
 * 张祖建

## v1.12.10 (2024-03-25)

 * [4a6888d5d](https://github.com/kubeovn/kube-ovn/commit/4a6888d5dacc16211f6b6702dce5ccc9bbcf4183) release v1.12.10
 * [55c314eff](https://github.com/kubeovn/kube-ovn/commit/55c314eff24e17f5eb2d73c772fb10a9dc61b1fb) fix: when give ipv4 cidr ipv6 gateway, the gateway will extend infinitely (#3860)
 * [577066859](https://github.com/kubeovn/kube-ovn/commit/5770668599905a43614bc7d083b19bb91836ff12) prepare for next release

### Contributors

 * changluyi

## v1.12.9 (2024-03-22)

 * [ee424ae45](https://github.com/kubeovn/kube-ovn/commit/ee424ae45dddbfe8506a0364a4a41979a044faeb) release v1.12.9
 * [2a956e0be](https://github.com/kubeovn/kube-ovn/commit/2a956e0be09cb12952fe85aa9f298721161a81d2) chart: fix missing ENABLE_IC (#3851)
 * [7d634ba23](https://github.com/kubeovn/kube-ovn/commit/7d634ba23126c7b0a19e18fe9a98526a4c7efe29) fix: cleanup.sh (#3569)
 * [350eabbea](https://github.com/kubeovn/kube-ovn/commit/350eabbeacc88eadf9173e19bba4e825b42e9d5b) exclude vip as encap ip (#3855)
 * [3cd21ed6b](https://github.com/kubeovn/kube-ovn/commit/3cd21ed6bd8a32073fc5bc686d3ef79f381be24d) fix: duplicate ip deletion (#3589)
 * [9ba51333a](https://github.com/kubeovn/kube-ovn/commit/9ba51333a1fab06149199a357d9db5f3ecae30e6) iptables: reject access to invalid service port only for TCP (#3843)
 * [19e6a99be](https://github.com/kubeovn/kube-ovn/commit/19e6a99bef98faef4ef7cb24b2e3241712ab0cb7) Fix 1.12 ipam deletion (#3554)
 * [78c41df44](https://github.com/kubeovn/kube-ovn/commit/78c41df44d741124f3e17c5d64ad0bc92c71b00b) delete lsp and ipam with ip (#3540)
 * [7bd3a6d03](https://github.com/kubeovn/kube-ovn/commit/7bd3a6d03134c1e51fda64124b286812597f9760) update protobuf module (#3841)
 * [4c19787fd](https://github.com/kubeovn/kube-ovn/commit/4c19787fdf080d81df7c856e3181ac30056a514b) kubectl-ko: fix subnet diagnose failure (#3808)
 * [efe08095d](https://github.com/kubeovn/kube-ovn/commit/efe08095db552f3ae2f09aa3184b5d391da69416) pinger: do not setup metrics if disabled (#3806)
 * [52e6b3e2a](https://github.com/kubeovn/kube-ovn/commit/52e6b3e2a72a2c0caa6e53cf2c13e2fe6540d750) Fix the failure to enable multi-network card traffic mirroring for newly created pods (#3805)
 * [a83262279](https://github.com/kubeovn/kube-ovn/commit/a83262279740e9d762e111cd1ca32abc43a1fada) Makefile: add target kind-install-metallb (#3795)
 * [ca937e58c](https://github.com/kubeovn/kube-ovn/commit/ca937e58cda6e377b7254788b58ccb61baf51e1d) build(deps): bump golang.org/x/sys from 0.17.0 to 0.18.0 (#3792)
 * [a3ee69a32](https://github.com/kubeovn/kube-ovn/commit/a3ee69a32fc692b49355f32cf452c8b036ac54cd) build(deps): bump golang.org/x/mod from 0.15.0 to 0.16.0 (#3793)
 * [6c53861a1](https://github.com/kubeovn/kube-ovn/commit/6c53861a16b43ce65e89584dddf57f0b3f891a28) Refactor build-go targets to use the -trimpath flag
 * [442a28054](https://github.com/kubeovn/kube-ovn/commit/442a28054e9ae84a3b729c97b73e6b3b31bff124) fix incorrect variable assignment (#3787)
 * [08a915a13](https://github.com/kubeovn/kube-ovn/commit/08a915a136748857c0c885b51753e40cb0620071) prepare for next release

### Contributors

 * bobz965
 * changluyi
 * dependabot[bot]
 * hzma
 * xieyanker
 * zhangzujian
 * 张祖建

## v1.12.8 (2024-02-29)

 * [f8e434c6d](https://github.com/kubeovn/kube-ovn/commit/f8e434c6df93377093b72992910de1f09dcf37bc) release v1.12.8
 * [8d8f9496d](https://github.com/kubeovn/kube-ovn/commit/8d8f9496dd7e0069664aba090b3e15ffe26e641e) update release.sh
 * [1e0db6d1e](https://github.com/kubeovn/kube-ovn/commit/1e0db6d1ed9b55f852308bacf995f4ba37610d3f) prepare for next release

### Contributors


## v1.12.7 (2024-02-28)

 * [147384727](https://github.com/kubeovn/kube-ovn/commit/1473847276e95ff5d4b6578d2959abafd8e9a9a6) release v1.12.7
 * [16b563e20](https://github.com/kubeovn/kube-ovn/commit/16b563e208563061892d78ee751861bd93567fa7) fix sts pod's logical switch port do not update externa_id vendor and ls (#3778)
 * [078a03f26](https://github.com/kubeovn/kube-ovn/commit/078a03f26ca7301ae206afcb820ab5a97fc58848) update dependence
 * [f9f2aaeb8](https://github.com/kubeovn/kube-ovn/commit/f9f2aaeb80ab20925429103bd8df6290bcd6dae7) update release.sh
 * [29157d4dd](https://github.com/kubeovn/kube-ovn/commit/29157d4ddb480cddd54d8d36fa9e80ce6b0e563c) update release.sh
 * [b5f7a6348](https://github.com/kubeovn/kube-ovn/commit/b5f7a6348bdf29a1edd6a7c2962f6feece89f4fa) refactor ovn clusterrole (#3755)
 * [76ea1c12f](https://github.com/kubeovn/kube-ovn/commit/76ea1c12f36197fb93ac77a73f6e771f35c0a335) prepare for next release

### Contributors

 * changluyi
 * hzma

## v1.12.6 (2024-02-26)

 * [a395104de](https://github.com/kubeovn/kube-ovn/commit/a395104dee67e9ed973a1afe005fd4d55f4691a9) release v1.12.6
 * [df6a84aaf](https://github.com/kubeovn/kube-ovn/commit/df6a84aaf3bfd48b8a42adbde5095e8ba736703e) if startOVNIC firstly, and setazName secondly, the ovn-ic-db may sync the old azname (#3759) (#3763)
 * [a90654522](https://github.com/kubeovn/kube-ovn/commit/a9065452236818173f6cbcaf84ec00c371298f10) modify chart.yaml version
 * [0107f9dd6](https://github.com/kubeovn/kube-ovn/commit/0107f9dd6294231d35232c9c414c8cd0870c7af3) add action for build base
 * [f537002cf](https://github.com/kubeovn/kube-ovn/commit/f537002cf51f2dd055643c5611147d3ae65dd441) prepare for next release

### Contributors

 * changluyi

## v1.12.5 (2024-02-21)

 * [b45c8339a](https://github.com/kubeovn/kube-ovn/commit/b45c8339ab370c3c68d532ebbe475781d982251c) release v1.12.5
 * [aeeb3c7b5](https://github.com/kubeovn/kube-ovn/commit/aeeb3c7b562e312e108db7bf46b463cba34f76aa) ci: bump github actions
 * [71e5da7b1](https://github.com/kubeovn/kube-ovn/commit/71e5da7b13a1e5adc2ca90e2cacaf45ca8497b0a) ci: bump azure/setup-helm to v4.0.0 (#3743)
 * [1d544554c](https://github.com/kubeovn/kube-ovn/commit/1d544554c231eb4e86ed28056d720e2bb4fee8bd) base: install libmnl0 instead of libmnl-dev (#3745)
 * [669ed067e](https://github.com/kubeovn/kube-ovn/commit/669ed067e0374344a2b86a8eacb8ad14b3ca0cf3) ci: collect ko logs for all kind clusters (#3744)
 * [b9224483d](https://github.com/kubeovn/kube-ovn/commit/b9224483defc22663368bad0ac6e00ec596f524c) ci: fix ovn ic log file name (#3742)
 * [5e5fd728d](https://github.com/kubeovn/kube-ovn/commit/5e5fd728d06d7b3f4c7dabc853f3cae10c45d267) ci: bump kind and node image
 * [cc8c9ab66](https://github.com/kubeovn/kube-ovn/commit/cc8c9ab664b19adbcd4ef95a6f23468c393b7e9d) update chart relase action workflow (#3728,#3734,#3691) (#3738)
 * [b3d8fca7d](https://github.com/kubeovn/kube-ovn/commit/b3d8fca7d21e88708fedbb0095ccf7d229714ef8) remove invalid ovs build option (#3733)
 * [2aa3311e9](https://github.com/kubeovn/kube-ovn/commit/2aa3311e92ba54d4285c904016db89877243b361) dpdk: remove unnecessary ovn patch (#3736)
 * [8e0e4b17f](https://github.com/kubeovn/kube-ovn/commit/8e0e4b17fd08891a5bd3dd98a6d7c9855ef5cbe2) Fix: Resolve issue with skipped execution of sg annotations (#3700)
 * [ae895e2e3](https://github.com/kubeovn/kube-ovn/commit/ae895e2e3b458a2d09325d903685f45ea3e342cf) fix: all gw nodes (#3723)
 * [bffef867b](https://github.com/kubeovn/kube-ovn/commit/bffef867bec988e882543983559346fbc95e57b9) ovn: remove unnecessary patch (#3720)
 * [764580fa8](https://github.com/kubeovn/kube-ovn/commit/764580fa86453d51aeff81c2eb9892dcb3811854) ci: fix artifact upload
 * [c71e3949b](https://github.com/kubeovn/kube-ovn/commit/c71e3949b1cba6fdd253f8d320f57d6e31ed361e) bump k8s to v1.27.10 (#3693)
 * [f9c80e127](https://github.com/kubeovn/kube-ovn/commit/f9c80e12735f1db13e17fdd979118c5bcf9a3ec4) fix backport (#3697)
 * [50b6506d6](https://github.com/kubeovn/kube-ovn/commit/50b6506d6350cecdd30692be2bda093821063d12) remove unused (#3696)
 * [a0e3b6798](https://github.com/kubeovn/kube-ovn/commit/a0e3b67986458081ece100ed91f232419b720060) kube-ovn-controller: remove unused codes (#3692)
 * [8da2239c8](https://github.com/kubeovn/kube-ovn/commit/8da2239c8689bff681f59a9885e9d05cb0046f32) remove fip controller (#3684)
 * [51a3c1e40](https://github.com/kubeovn/kube-ovn/commit/51a3c1e40867f42e349b6bdd704c98340945139d) build(deps): bump github.com/osrg/gobgp/v3 from 3.22.0 to 3.23.0 (#3688)
 * [6d1cbd3c9](https://github.com/kubeovn/kube-ovn/commit/6d1cbd3c9634e46501bfd55888caf571ef2bd655) Compatible with controller deployment methods before kube-ovn 1.11.16 (#3677)
 * [ea918989b](https://github.com/kubeovn/kube-ovn/commit/ea918989b5e907cbfafbcdd054e9ab37a95c2da7) set after genev_sys_6081 started (#3680)
 * [c16b634e5](https://github.com/kubeovn/kube-ovn/commit/c16b634e5396c116ae2dcee8233180315f016fe8) ovn: add nb option version_compatibility (#3671)
 * [295871187](https://github.com/kubeovn/kube-ovn/commit/295871187c68448e93ea904e7f2fad1a0659e5f0) Makefile: fix install/upgrade chart (#3678)
 * [822df3752](https://github.com/kubeovn/kube-ovn/commit/822df3752e9a37a4de8d955156a5a3007bd25e98) ovn: do not send direct traffic between lports to conntrack (#3663)
 * [130f06cb7](https://github.com/kubeovn/kube-ovn/commit/130f06cb73c6d21c89a651b10b5b94925d62cc25) ovn-ic-ecmp refactor 1.12 (#3637)
 * [8c75820aa](https://github.com/kubeovn/kube-ovn/commit/8c75820aa4b5cb582db95976fb233e2d92c32d00) build(deps): bump google.golang.org/grpc from 1.60.1 to 1.61.0 (#3669)
 * [0da94b589](https://github.com/kubeovn/kube-ovn/commit/0da94b5893a21bb9a2143167fb0491b2b175a781) fix 409 (#3662)
 * [b2f7da5fc](https://github.com/kubeovn/kube-ovn/commit/b2f7da5fc24bb50ae3358e05c065b35eda9c5e54) fix nil pointer (#3661)
 * [51e32914a](https://github.com/kubeovn/kube-ovn/commit/51e32914a870dd76d984c8132ac462dfc66139f2) chart: fix parsing image tag when the image url contains a port (#3644)
 * [a9d896ab4](https://github.com/kubeovn/kube-ovn/commit/a9d896ab4164fdbd41b1cde2fff98be6263c2d7b) ovs: reduce cpu utilization (#3650)
 * [2f7fdb29a](https://github.com/kubeovn/kube-ovn/commit/2f7fdb29a6412130f2ad08a7bd71ab7ed58deab9) kube-ovn-monitor and kube-ovn-pinger export pprof path (#3657)
 * [62568275e](https://github.com/kubeovn/kube-ovn/commit/62568275e92657fbb9c2edf179cbc813296bc97c) build(deps): bump github.com/onsi/gomega from 1.30.0 to 1.31.0 (#3641)
 * [53e712d2e](https://github.com/kubeovn/kube-ovn/commit/53e712d2e63edb81595afcad04eb9535962d1edd) build(deps): bump actions/cache from 3 to 4 (#3643)
 * [4dbe93429](https://github.com/kubeovn/kube-ovn/commit/4dbe93429fb97ab36a5c5e905bb07e9c10cdb856) build(deps): bump github.com/onsi/ginkgo/v2 from 2.14.0 to 2.15.0 (#3642)
 * [141ceaaa3](https://github.com/kubeovn/kube-ovn/commit/141ceaaa381ca49fa848e10b18c58e60a961eb90) SYSCTL_IPV4_IP_NO_PMTU_DISC set default to 0
 * [754b172c9](https://github.com/kubeovn/kube-ovn/commit/754b172c93678eb69863a9922580403d61eba18b) build(deps): bump github.com/evanphx/json-patch/v5 from 5.7.0 to 5.8.0 (#3628)
 * [fc988842a](https://github.com/kubeovn/kube-ovn/commit/fc988842abd33f55462e26fb8054c13f878c1a31) chart: fix ovs-ovn upgrade (#3613)
 * [216c92927](https://github.com/kubeovn/kube-ovn/commit/216c92927a32ce1c3709d66f0a4c3227c8463680) build(deps): bump github.com/emicklei/go-restful/v3 (#3619)
 * [74c5b8a03](https://github.com/kubeovn/kube-ovn/commit/74c5b8a03881af867a4ed9e22b537daea77ce36e) build(deps): bump github.com/emicklei/go-restful/v3 (#3606)
 * [91be66f1e](https://github.com/kubeovn/kube-ovn/commit/91be66f1ee16d8adb52e2f09e2944468abd2a364) update policy route when subnet cidr is changed (#3587)
 * [c3f6e3c24](https://github.com/kubeovn/kube-ovn/commit/c3f6e3c245d949a7c781c7f60c33114087741e92) update ipset to v7.17 (#3601)
 * [05d0334e9](https://github.com/kubeovn/kube-ovn/commit/05d0334e965d22dca9abd0ee85726f58dbb7c919) ovs: increase cpu limit to 2 cores (#3530)
 * [8f4220c61](https://github.com/kubeovn/kube-ovn/commit/8f4220c613f5ec6ef4aaa525468b5b892c9b7ba5) build(deps): bump github.com/osrg/gobgp/v3 from 3.21.0 to 3.22.0 (#3603)
 * [4b859054f](https://github.com/kubeovn/kube-ovn/commit/4b859054fc7f29f6a4730fc9b20ba9b3305a853a) do not count ips in excludeIPs as available and using IPs (#3582)
 * [f4311ab07](https://github.com/kubeovn/kube-ovn/commit/f4311ab07f5785a6a7a2a1131f404a89bcd80006) fix security issue (#3588)
 * [9f74ee32d](https://github.com/kubeovn/kube-ovn/commit/9f74ee32d18cfebee61b1e7e17ec39cebbe04cc4) ovn0 ipv6 addr gen mode set 0 (#3579)
 * [958aab96f](https://github.com/kubeovn/kube-ovn/commit/958aab96fb1e3daa4e2f60412ab32ea673690949) fix: add err log (#3572)
 * [ad3f674a2](https://github.com/kubeovn/kube-ovn/commit/ad3f674a289c2a73aecb2c67c54cf9b7f6c258fe) build(deps): bump google.golang.org/protobuf from 1.31.0 to 1.32.0 (#3571)
 * [acb9e97fa](https://github.com/kubeovn/kube-ovn/commit/acb9e97fa6a38f9c89193ca25a51e0c2f6ccd01c) Makefile: fix kwok installation (#3561)
 * [db2aeeba2](https://github.com/kubeovn/kube-ovn/commit/db2aeeba2a0673d770213a89043b3505cc753a5d) fix u2o infinity recycle
 * [32604cf13](https://github.com/kubeovn/kube-ovn/commit/32604cf135c9b2fb1a06f3db4c154d082efcdc80) do not calculate subnet.spec.excludeIPs as availableIPs (#3550)
 * [999dc618c](https://github.com/kubeovn/kube-ovn/commit/999dc618c77a11bec314013ded0c0985ccd4a271) add np prefix to networkpolicy name when networkpolicy's name starts with number (#3551)
 * [2249e24e9](https://github.com/kubeovn/kube-ovn/commit/2249e24e900687525ac6ed5712e7d3084789990a) fix: apply changes to the latest version (#3514)
 * [1b27c414c](https://github.com/kubeovn/kube-ovn/commit/1b27c414c37fa8382a3a2ae10a26dfa89f393096) fix ovn ic not clean lsp and lrp when az name contains "-" (#3541)
 * [81ef8ff44](https://github.com/kubeovn/kube-ovn/commit/81ef8ff44e703fbcb21f784290ff9901ea6e0f6e) build(deps): bump golang.org/x/crypto from 0.16.0 to 0.17.0 (#3544)
 * [cd28464f4](https://github.com/kubeovn/kube-ovn/commit/cd28464f4ded3c50462b2fdae722945f7bde1752) Revert "ovn-central: check raft inconsistency from nb/sb logs (#3532)"
 * [e88e5b2bb](https://github.com/kubeovn/kube-ovn/commit/e88e5b2bbb4f253352570eb4ebcfcbc972bb2194) ovn-central: check raft inconsistency from nb/sb logs (#3532)
 * [2a499b424](https://github.com/kubeovn/kube-ovn/commit/2a499b424f19af0ae7246f989c0a21f1773949ab) fix chassis gc (#3525)
 * [f07aef034](https://github.com/kubeovn/kube-ovn/commit/f07aef0341ffb5d8fd3d173b69e116d452292c04) prepare for next release

### Contributors

 * Changlu Yi
 * Qinghao Huang
 * Zhao Congqi
 * bobz965
 * changluyi
 * dependabot[bot]
 * hzma
 * zhangzujian
 * 张祖建

## v1.12.4 (2023-12-14)

 * [366e79957](https://github.com/kubeovn/kube-ovn/commit/366e799579d6c485d329dd32ddbb1aba684a47f0) set release v1.12.4
 * [4207a45a1](https://github.com/kubeovn/kube-ovn/commit/4207a45a12faee3a9eb4bec515ecca2d58617d06) cni-server: set sysctl variable net.ipv4.ip_no_pmtu_disc to 1 by default (#3504)
 * [943a1d99a](https://github.com/kubeovn/kube-ovn/commit/943a1d99aa236bbb2244adba2560dfcc9ae9a96d) fix: duplicate gw nodes (#3500)
 * [35c7eaf52](https://github.com/kubeovn/kube-ovn/commit/35c7eaf52fbb7740954d2059f8fe19e6e1f7f455) add drop invalid rst 1.12 (#3490)
 * [60966143a](https://github.com/kubeovn/kube-ovn/commit/60966143a255191653e76a6e5e623522e9a79d5c) delete String() function (#3488)
 * [d4adddeee](https://github.com/kubeovn/kube-ovn/commit/d4adddeee9dadaee062c4b807fa758d7b2003013) fix: lost gc lsp in previous pr (#3493)
 * [1d89ea2ad](https://github.com/kubeovn/kube-ovn/commit/1d89ea2ad8b2d953b28569d2783bf2b57a942d13) fix: ipam clean all pod nic ip address and mac even if just delete a nic (#3453)
 * [3903f9b06](https://github.com/kubeovn/kube-ovn/commit/3903f9b06a85ed422eddfede88e21ef3c65a729f) fix: check chassis before creation (#3482)
 * [621fcad72](https://github.com/kubeovn/kube-ovn/commit/621fcad72f1a9cd09c88230515fa754e98f43632) build(deps): bump github.com/osrg/gobgp/v3 from 3.20.0 to 3.21.0 (#3481)
 * [cdd435381](https://github.com/kubeovn/kube-ovn/commit/cdd4353818b3c5b16e245fc90ee9e19584f8a69c) fix ovn eip not calculated (#3477)
 * [1476c0a12](https://github.com/kubeovn/kube-ovn/commit/1476c0a128398f472a7a3a5cc2b93093cde4ccef) fix: calculate subnet before handle finalizer (#3469)
 * [739825f11](https://github.com/kubeovn/kube-ovn/commit/739825f114702831a5f6246604f646f57d4793a2) schedule kube-ovn-controller on the kube-ovn-master node (#3479)
 * [768dd5fb0](https://github.com/kubeovn/kube-ovn/commit/768dd5fb054d0e0be0c7d631487d097275189528) delete vm's lsp and release ipam.ip (#3476)
 * [03a82bae1](https://github.com/kubeovn/kube-ovn/commit/03a82bae15ac9cfabdaca025b707c1a89a7cef53) build(deps): bump github.com/onsi/ginkgo/v2 from 2.13.1 to 2.13.2 (#3475)
 * [051499908](https://github.com/kubeovn/kube-ovn/commit/05149990837582f41c5540fe58db3723df06987f) build(deps): bump golang.org/x/time from 0.4.0 to 0.5.0 (#3463)
 * [a45620d8a](https://github.com/kubeovn/kube-ovn/commit/a45620d8ae6e83fd6d7dcfdf2829571cce80a12f) build(deps): bump golang.org/x/sys (#3464)
 * [0fb4f1960](https://github.com/kubeovn/kube-ovn/commit/0fb4f1960af16039013c50e49bc7bc1ba69dda3d) kube-ovn-cni: fix pinger result when timeout is reached (#3457)
 * [df2a9c36b](https://github.com/kubeovn/kube-ovn/commit/df2a9c36ba89a33fcf681fd24cbc4d84f17adaeb) ovs-healthcheck: ignore error when log file does not exist (#3456)
 * [7dc00aad2](https://github.com/kubeovn/kube-ovn/commit/7dc00aad2f8cf351922b50cf4535779b8fbec4bd) ipam: fix duplicate allocation after cidr expansion (#3455)
 * [23a4b7336](https://github.com/kubeovn/kube-ovn/commit/23a4b7336cee173ba42f2d3da535506903bf9e3a) fix e2e install failed
 * [72444c11e](https://github.com/kubeovn/kube-ovn/commit/72444c11e6889507c982849d42ec4c609c5026e2) readd assigned ip addresses to ipam when subnet has been changed (#3448)
 * [03ed9daf6](https://github.com/kubeovn/kube-ovn/commit/03ed9daf61ea2db9f3b14e703510600fc5c0e80a) base: fix missing CFLAGS -fPIC for arm64 (#3428)
 * [2f3923c6f](https://github.com/kubeovn/kube-ovn/commit/2f3923c6f035a184dd96078800f37ebb676da147) fix:  multus network status not find dpdk interface name (#3432)
 * [5069a03d2](https://github.com/kubeovn/kube-ovn/commit/5069a03d2836de331b4acd80e0b73df57b6d6fd4) bump k8s to v1.27.8 (#3425)
 * [ca41d0f44](https://github.com/kubeovn/kube-ovn/commit/ca41d0f44be2c5d238586439b1e6990c3a985062) ci: fix missing environment variables (#3430)
 * [c736912bc](https://github.com/kubeovn/kube-ovn/commit/c736912bcab601fe370906fcd4fea7a7211321e9) base: fix dpdk build failure (#3426)
 * [45e7f1fc8](https://github.com/kubeovn/kube-ovn/commit/45e7f1fc8190c0e60d8317d2dd30efc4d24a85ae) base: fix ovn build failure (#3340)
 * [d6adccad3](https://github.com/kubeovn/kube-ovn/commit/d6adccad3ac8127fc53ef2fd6271315d35ed5d84) fix:  lsp dhcp options set failed when subnet dhcp option is enabled (#3422)
 * [6ee914ccb](https://github.com/kubeovn/kube-ovn/commit/6ee914ccbcf08f63de0d6be8482c41fbd26fb5e6) trivy: ignore CVE-2023-5528
 * [7f5e68c5a](https://github.com/kubeovn/kube-ovn/commit/7f5e68c5ad05b18450bfa2c0c11cb9d478ad9acc) update policy route nexthops para
 * [a54fcfed7](https://github.com/kubeovn/kube-ovn/commit/a54fcfed74c745e4840f84df3605df56f8ccbb16) ci: fix dpdk jobs (#3405)
 * [0fe59db40](https://github.com/kubeovn/kube-ovn/commit/0fe59db404039193d219e956eeff74b9226bb7c1) ci: free disk space for all x86 jobs (#3406)
 * [56b314510](https://github.com/kubeovn/kube-ovn/commit/56b3145104186445098d70a6df08a9a0541b05eb) ci: free disk space (#3404)
 * [b0759192b](https://github.com/kubeovn/kube-ovn/commit/b0759192b9bccfa5a8273a304c3bbb38faf55888) fix dpdk workflow (#3384)
 * [459e2d6d2](https://github.com/kubeovn/kube-ovn/commit/459e2d6d2c9065f184dae8eb3f0dfc4de5c83932) base: fix ovn-northd/ovn-controller not creating pidfile in arm64 (#3413)
 * [984c0358b](https://github.com/kubeovn/kube-ovn/commit/984c0358bb9b151626d59bc2280c9c2377700674) support ovn ic ecmp (#3348) (#3410)
 * [12625f509](https://github.com/kubeovn/kube-ovn/commit/12625f509f36d66aa4f4a6d1bd3845fb263fb5e8) fix kube-ovn-monitor probe (#3409)
 * [a3c7fb4b8](https://github.com/kubeovn/kube-ovn/commit/a3c7fb4b8a203fdd3df9e46d921768d2b8303630) fix: wrong usage about DeepEqual (#3396)
 * [6d6eef434](https://github.com/kubeovn/kube-ovn/commit/6d6eef4347d9b8557ae3e3663b07e6115a952ed0) subnet support config mtu (#3367)
 * [005b92bd6](https://github.com/kubeovn/kube-ovn/commit/005b92bd6a2812a07852785b38e6b16baf3683a6) fix dualStack network checkgw raise panic (#3392)
 * [9f08f4a24](https://github.com/kubeovn/kube-ovn/commit/9f08f4a24f26236890d199f41b416697a1e6dc84) feat:  dpdk-22.11.1 support by kube-ovn (#3388)
 * [196133114](https://github.com/kubeovn/kube-ovn/commit/1961331149fca3c7132fdf69cb25c48ecb46e316) fix:  gc delete multus ip cr and lsp setting when enable keep vm ip (#3378)
 * [a37ee6bf1](https://github.com/kubeovn/kube-ovn/commit/a37ee6bf1f2188d0a5e6b9431891f1f4dc49a9ba) add kube-ovn-controller nodeAffinity prefer not on ic gateway
 * [50c5341cd](https://github.com/kubeovn/kube-ovn/commit/50c5341cd4bffac25f7ab3e4eb8a0f722554f35e) fix: externalID map should not include external_ids (#3385)
 * [c670082d3](https://github.com/kubeovn/kube-ovn/commit/c670082d387ddefefd4c3020cabf5e90574fec47) build(deps): bump github.com/moby/sys/mountinfo from 0.6.2 to 0.7.0
 * [d271d9e0d](https://github.com/kubeovn/kube-ovn/commit/d271d9e0d70f9d0abadc4870bb4e6a55089efaf0) build(deps): bump golang.org/x/mod from 0.13.0 to 0.14.0 (#3380)
 * [f49f84e8f](https://github.com/kubeovn/kube-ovn/commit/f49f84e8f6296d575dd3fedf9ae3a745561091ce) build(deps): bump golang.org/x/time from 0.3.0 to 0.4.0 (#3383)
 * [dd2af541a](https://github.com/kubeovn/kube-ovn/commit/dd2af541a9014e56712146e8a223b09e51877583) build(deps): bump golang.org/x/time from 0.3.0 to 0.4.0 (#3383)
 * [d683487f8](https://github.com/kubeovn/kube-ovn/commit/d683487f82222a8b6ce1192de890db08feb3ff16) prepare for next release

### Contributors

 * Changlu Yi
 * bobz965
 * changluyi
 * dependabot[bot]
 * hzma
 * pengbinbin1
 * wujixin
 * xujunjie-cover
 * zhangzujian
 * 张祖建
 * 袁又袁
 * 马洪贞

## v1.12.3 (2023-11-06)

 * [b64d7e9b1](https://github.com/kubeovn/kube-ovn/commit/b64d7e9b1e0422b397a7e7b30dfbd8c45d7cafc8) set release for 1.12.3
 * [a9cbe0279](https://github.com/kubeovn/kube-ovn/commit/a9cbe0279482b9317d56055e9de1a7867268c8f5) kube-ovn-dpdk building need its dpdk base img (#3371)
 * [b0efd5a9c](https://github.com/kubeovn/kube-ovn/commit/b0efd5a9c16d3ad41ef1ee40178d5c7f11f892e4) delete check for existing ip cr (#3361)
 * [366e64110](https://github.com/kubeovn/kube-ovn/commit/366e6411041d540b13fb6dc3df91f9532bdb7c00) fix IP residue after changing subnet of vm in some scenarios (#3370)
 * [f593a046c](https://github.com/kubeovn/kube-ovn/commit/f593a046c6ffa5d5a9d7ceda9ee7d0a7195f7dfb) sync acp chart (#3364)
 * [def751692](https://github.com/kubeovn/kube-ovn/commit/def7516926b4d3d8781016569c06dabfaef876f5) kube-ovn-controller: fix memory growth caused by unused workqueue
 * [3023336df](https://github.com/kubeovn/kube-ovn/commit/3023336dffa6a56b87edbe4bae2185b88adc3028) build(deps): bump github.com/osrg/gobgp/v3 from 3.19.0 to 3.20.0 (#3362)
 * [c7ffd1f56](https://github.com/kubeovn/kube-ovn/commit/c7ffd1f56682519efec6bc7f5b5d4b5c8202055b) fix access svc ip failed, when acl is on (#3350)
 * [5da749647](https://github.com/kubeovn/kube-ovn/commit/5da7496476a8a336b6ee18b7bc54036045704370) Add Layer 2 forwarding for subnet ports again (#3300)
 * [31e77fbf3](https://github.com/kubeovn/kube-ovn/commit/31e77fbf3c757413b76d21e861dddba59268a061) add compact for release-1.12 (#3342)
 * [d771eb8ce](https://github.com/kubeovn/kube-ovn/commit/d771eb8ce222e50511a3b93fe61e92b28c69c272) prepare for next release

### Contributors

 * Tobias
 * bobz965
 * changluyi
 * dependabot[bot]
 * hzma
 * 张祖建
 * 袁又袁

## v1.12.2 (2023-10-24)

 * [23a6299c1](https://github.com/kubeovn/kube-ovn/commit/23a6299c1ad15b8a52a34b4ac70e4295c6903116) set release 1.12.2
 * [b4abb34a7](https://github.com/kubeovn/kube-ovn/commit/b4abb34a7c9a6ed050683aa55081c6275f93366d) Nat reuse router port external ip (#3313)
 * [a0228ef94](https://github.com/kubeovn/kube-ovn/commit/a0228ef94324bd92de08c9011bfaa4cb3b93343d) dump cpu/mem profile into file on signal SIGUSR1/SIGUSR2 (#3262)
 * [5226abf61](https://github.com/kubeovn/kube-ovn/commit/5226abf61fd2336d57db27d69428bdbb0a0eb12f) kube-ovn-controller: fix ovn ic log directory not mounted to hostpath (#3322)
 * [c7e6fc344](https://github.com/kubeovn/kube-ovn/commit/c7e6fc3443c73972dd026fd2e7b85b4e37cf567f) fix golang lint error (#3323)
 * [f779892f7](https://github.com/kubeovn/kube-ovn/commit/f779892f71a5f2bd36b44dee784a665cc3a2f4fd) update go version
 * [f2eac645b](https://github.com/kubeovn/kube-ovn/commit/f2eac645bd342039f2e43ba06c758159b0972e97) fix build error
 * [9375e5924](https://github.com/kubeovn/kube-ovn/commit/9375e5924f07a5a1ae3502b04c4d679ab18a2507) add type assertion for ip crd (#3311)
 * [127a87a97](https://github.com/kubeovn/kube-ovn/commit/127a87a970d706a227ab520ec63f0ba66d356ac9) add load balancer health check (#3216)
 * [e01a85364](https://github.com/kubeovn/kube-ovn/commit/e01a853644bd8ef8ec4763015509cf5b90e99554) build(deps): bump google.golang.org/grpc from 1.58.3 to 1.59.0 (#3310)
 * [c4e364173](https://github.com/kubeovn/kube-ovn/commit/c4e3641730cd2a68c411d4d03789495dfbffd48b) build(deps): bump github.com/Microsoft/hcsshim from 0.11.1 to 0.11.2 (#3309)
 * [418d45f14](https://github.com/kubeovn/kube-ovn/commit/418d45f14a6c17ebeaf097deea28c35719cfcc10) support vpc configuration of multiple external network segments through label and crd (#3264)
 * [51980378e](https://github.com/kubeovn/kube-ovn/commit/51980378eec2d00c9696df0dedf020a92d94ddf4) sync subnet to vpc while switching between custom VPC and default VPC (#3218)
 * [529c9f4e9](https://github.com/kubeovn/kube-ovn/commit/529c9f4e95d71bbcb8780d647fd9f001051ed3c0) security: ignore kubectl cve (#3305)
 * [29eef2778](https://github.com/kubeovn/kube-ovn/commit/29eef2778c1542e3e460051b4c19d30e9057c4b0) Don't enqueue VPC update when DeletionTimestamp is zero (#3302)
 * [408c6e9ee](https://github.com/kubeovn/kube-ovn/commit/408c6e9ee902f3278c39bab6e9a3473b9c6c26cc) Revert "update base image to ubuntu:23.10 (#3289)"
 * [671d55db7](https://github.com/kubeovn/kube-ovn/commit/671d55db7b4c205dd5bc897b66bf215f9541c85d) add base rules for allowing vrrp packets (#3293)
 * [c6b2cbdde](https://github.com/kubeovn/kube-ovn/commit/c6b2cbdde31c4d45c5f08c9035c54af74ddf47a6) build(deps): bump google.golang.org/grpc from 1.58.2 to 1.58.3 (#3295)
 * [c674f4896](https://github.com/kubeovn/kube-ovn/commit/c674f4896fccf22d7d505e46acc017c0891b4ee6) build(deps): bump golang.org/x/net from 0.16.0 to 0.17.0 (#3296)
 * [af35954f1](https://github.com/kubeovn/kube-ovn/commit/af35954f1fce0cb4f015da50919b98fac88917f2) webhook: fix ip validation when pod is annotated with an ippool name (#3284)
 * [94ecdf7a7](https://github.com/kubeovn/kube-ovn/commit/94ecdf7a725435bb40de9aec08447e5974e81fac) webhook: use dedicated port for health probe (#3285)
 * [9cb875a1a](https://github.com/kubeovn/kube-ovn/commit/9cb875a1a71b1d291f8524172925d5a5668db41d) add concurrency limiter to ovs-vsctl (#3288)
 * [47c4d7257](https://github.com/kubeovn/kube-ovn/commit/47c4d7257a459f618186e711c942f10fb3ef925c) update base image to ubuntu:23.10 (#3289)
 * [b94667dec](https://github.com/kubeovn/kube-ovn/commit/b94667dec72795f7eb16a61cff060dc2e06499fe) support custom vpc dns its deployment replicas (#3286)
 * [fa7eecf9d](https://github.com/kubeovn/kube-ovn/commit/fa7eecf9dbd3b916d8784217a36ae4f7b0dc7d44) ovs: load kernel module ip_tables only when it exists (#3281)
 * [de5860e00](https://github.com/kubeovn/kube-ovn/commit/de5860e0095e83cda87140f014ba79a945e5d757) update directory name in charts readme (#3276)
 * [481d372eb](https://github.com/kubeovn/kube-ovn/commit/481d372eb5194a045fafa443f5ada9f25d0f7c7d) fix ovn build failure (#3275)
 * [92654f4e8](https://github.com/kubeovn/kube-ovn/commit/92654f4e8c0ce84392c5b35e6f5da0727300ab42) build(deps): bump golang.org/x/sys from 0.12.0 to 0.13.0 (#3271)
 * [1036b0a8e](https://github.com/kubeovn/kube-ovn/commit/1036b0a8ece8bfdb3feb18b32bd4f4b579519d62) build(deps): bump golang.org/x/sys from 0.12.0 to 0.13.0 (#3271)
 * [3cb084edf](https://github.com/kubeovn/kube-ovn/commit/3cb084edf62f7142f547b5757a3402c1878a88c2) build(deps): bump github.com/prometheus/client_golang (#3266)
 * [8aaca988f](https://github.com/kubeovn/kube-ovn/commit/8aaca988f6d57ae88338d394ec7f964610ab5bac) build(deps): bump github.com/prometheus/client_golang (#3266)
 * [e7a91d0a2](https://github.com/kubeovn/kube-ovn/commit/e7a91d0a253fd1ae6c215fa89f72a98a16be65fc) prepare for the next release
 * [9b03b4adc](https://github.com/kubeovn/kube-ovn/commit/9b03b4adc4644a5d113d7f84535b73f0b3ea62c0) pinger: increase packet send interval (#3259)
 * [70a13529b](https://github.com/kubeovn/kube-ovn/commit/70a13529b80265fce2e42aa1b0e6934c7ff544e3) add init container in vpc-nat-gateway statefulset for init (#3254)
 * [1156c03d6](https://github.com/kubeovn/kube-ovn/commit/1156c03d61cace1fc2c46d30821078b868e8f2e2) lrp should use chassis name instead of uuid (#3258)

### Contributors

 * Tobias
 * bobz965
 * dependabot[bot]
 * hzma
 * wenwenxiong
 * zcq98
 * 夜微澜
 * 张祖建
 * 袁又袁
 * 马洪贞

## v1.12.1 (2023-09-25)

 * [e945a1066](https://github.com/kubeovn/kube-ovn/commit/e945a1066363e86e4848eba72d77c5904728e0c8) set release for v1.12.1
 * [f9adc20ae](https://github.com/kubeovn/kube-ovn/commit/f9adc20aefcd94bace3b0847f227b609e24bf557) fix: for existing nic, no need to set the port type to internal (#3243)
 * [e19b5b507](https://github.com/kubeovn/kube-ovn/commit/e19b5b507f17231011377a533461a9c0e4460584) adjust vip prints as ip (#3248)
 * [7d3dc0375](https://github.com/kubeovn/kube-ovn/commit/7d3dc0375e9794f6cb7d848d99aee0ec3418c225) add dpdk probe (#3151)
 * [223cc614d](https://github.com/kubeovn/kube-ovn/commit/223cc614d8271c372307ef682d4e8d171f356abb) build(deps): bump google.golang.org/grpc from 1.58.1 to 1.58.2 (#3251)
 * [66a2f59bc](https://github.com/kubeovn/kube-ovn/commit/66a2f59bca0297aa2bcd442e5d2ca51638a5ef75) build(deps): bump github.com/Microsoft/hcsshim from 0.11.0 to 0.11.1 (#3245)
 * [e5f8671b6](https://github.com/kubeovn/kube-ovn/commit/e5f8671b6e7effef4b08ba2f2840e99edcd6b0db) update kubectl to v1.28.2
 * [8e27e2044](https://github.com/kubeovn/kube-ovn/commit/8e27e20448597bb713135ee4469000ab6f0a0ff4) fix goproxy Denial of Service vulnerability (#3240)
 * [444b31721](https://github.com/kubeovn/kube-ovn/commit/444b31721d4c8e87b70513bc7f8eb7804645f98d) build(deps): bump github.com/cyphar/filepath-securejoin (#3239)
 * [55edc1b6d](https://github.com/kubeovn/kube-ovn/commit/55edc1b6debea29353a560ed8cde1ebd24816fc3) build(deps): bump github.com/onsi/ginkgo/v2 from 2.11.0 to 2.12.1 (#3237)
 * [525e3b2d7](https://github.com/kubeovn/kube-ovn/commit/525e3b2d7fbfc9e56f45b28b5c2cdd4a1088e1d4) build(deps): bump github.com/docker/docker (#3234)
 * [de3a300d4](https://github.com/kubeovn/kube-ovn/commit/de3a300d43cdcef9281dac3a798e67c81aa4b94e) build(deps): bump github.com/evanphx/json-patch/v5 from 5.6.0 to 5.7.0 (#3235)
 * [019217c4d](https://github.com/kubeovn/kube-ovn/commit/019217c4d9f8a503e2e8915aabff76f3bee6cb6a) build(deps): bump github.com/osrg/gobgp/v3 from 3.17.0 to 3.18.0 (#3238)
 * [62aabb6e0](https://github.com/kubeovn/kube-ovn/commit/62aabb6e01b29cee7cbe1c7b55669790ec677cda) build(deps): bump google.golang.org/grpc from 1.57.0 to 1.58.1
 * [72b82d068](https://github.com/kubeovn/kube-ovn/commit/72b82d0681c51e29041bba612ed7ed9a98421f35) build(deps): bump github.com/Microsoft/hcsshim from 0.10.0 to 0.11.0 (#3228)
 * [20b8ca58a](https://github.com/kubeovn/kube-ovn/commit/20b8ca58a784422b850f63bd9d8bba083e6cabe8) build(deps): bump golang.org/x/sys from 0.11.0 to 0.12.0 (#3232)
 * [8817e24da](https://github.com/kubeovn/kube-ovn/commit/8817e24daa15728a0a43e0b2ac57961298f3b433) chart: remove subnet finalizers before subnets are deleted (#3213)
 * [47e80feac](https://github.com/kubeovn/kube-ovn/commit/47e80feace8c9022946dbb65d07155a1e4f8b519) kubectl-ko: add new command ovn-trace for tracing ovn lflows only (#3202)
 * [0bb52d91e](https://github.com/kubeovn/kube-ovn/commit/0bb52d91e395fc86d05dcd9a90e54e1869ac321b) fix conflict after cherry-pick
 * [87779e18f](https://github.com/kubeovn/kube-ovn/commit/87779e18fcb1a1fa800fbe41116b6ef6dd7e97f5) add golang lint (#3154)
 * [545d64d8b](https://github.com/kubeovn/kube-ovn/commit/545d64d8b53e414b5d6452739edfcdb4bc25c96f) add special handling for the route policy of the default VPC (#3194)
 * [3603584c4](https://github.com/kubeovn/kube-ovn/commit/3603584c452dc9c05e4d73189296019d892ea560) fix add static route to wrong table of ovn (#3195)
 * [012e00302](https://github.com/kubeovn/kube-ovn/commit/012e0030260c443cf4bf8c27a37591d794fe278b) netpol: fix duplicate default drop acl (#3197)
 * [1c13e40aa](https://github.com/kubeovn/kube-ovn/commit/1c13e40aa0d3fc23c4768a89706d3fce095d18a7) add log to help find conflict ip owner (#3191)
 * [7f3b1e8b6](https://github.com/kubeovn/kube-ovn/commit/7f3b1e8b68cb04ec28425eeaa9ba26f9263aa947) suuport user custom log location (#3186)
 * [188c252d7](https://github.com/kubeovn/kube-ovn/commit/188c252d767656900af00b8e44b7daac8afc0875) enable set --ovn-northd-n-threads (#3150)
 * [c5d4221f4](https://github.com/kubeovn/kube-ovn/commit/c5d4221f49a348cfec025170bab7c31608e32727) Fix max unavailable (#3149)
 * [5d0721105](https://github.com/kubeovn/kube-ovn/commit/5d07211058cc9578d01bbecbe2542737ea5be7b9) add probe (#3133)
 * [0aa39e827](https://github.com/kubeovn/kube-ovn/commit/0aa39e82789d811233c086d1bc042560c39657d7) underlay: fix ip/route tranfer when the nic is managed by NetworkManager (#3184)
 * [47f475b0d](https://github.com/kubeovn/kube-ovn/commit/47f475b0d4263ed92305e88fe0aa0adfb6c857cd) ci: wait for terminating ovs-ovn pod to disappear (#3160)
 * [01123f8d7](https://github.com/kubeovn/kube-ovn/commit/01123f8d76a14bec8161f3f90d1306e855455d33) fix ovn build (#3166)
 * [f3b646053](https://github.com/kubeovn/kube-ovn/commit/f3b646053361002526e6ae4fb4ad3bf3f37cbab6) chart: fix ovs-ovn upgrade (#3164)
 * [ff1635581](https://github.com/kubeovn/kube-ovn/commit/ff1635581baf8e0632cc668ebb52c2a6ef80dd9e) subnet: fix deleting lr policy on node deletion (#3176)
 * [7339b5a5c](https://github.com/kubeovn/kube-ovn/commit/7339b5a5c2f817330e79b440d42302ebdfe2467a) ci/test: bump various versions (#3162)
 * [094d13692](https://github.com/kubeovn/kube-ovn/commit/094d136922204faebff11cfb568c2e51639e9b47) kubectl-ko: get ovn db leaders only on necessary (#3158)
 * [1dad23d99](https://github.com/kubeovn/kube-ovn/commit/1dad23d99c13ffef71a65a14f0faf9013cfd9703) underlay: fix NetworkManager operation (#3147)
 * [0e4909a2c](https://github.com/kubeovn/kube-ovn/commit/0e4909a2c143b94563107e46ba5e8feb333bc84c) Revert "enable set --ovn-northd-n-threads"
 * [9fd7ef5e1](https://github.com/kubeovn/kube-ovn/commit/9fd7ef5e1d1225d9809cea768dd45af8a26f6c14) enable set --ovn-northd-n-threads
 * [2e820fd8a](https://github.com/kubeovn/kube-ovn/commit/2e820fd8a3ae90bd30d1833c5a12e57ce8cb9df8) sbctl chassis operation  replace with libovsdb (#3119)
 * [00bfa4bd8](https://github.com/kubeovn/kube-ovn/commit/00bfa4bd8dc071b37cfa08e22ff21a68d69964ab) base: remove ovn patch for skipping ct (#3141)
 * [377d56dc9](https://github.com/kubeovn/kube-ovn/commit/377d56dc9d11a02ebf7d5e52a316347d603a7c3f) Enable set probe (#3145)
 * [a7af8973a](https://github.com/kubeovn/kube-ovn/commit/a7af8973a4f1a38c2654a1c04c3357423f11f130) support recreate a backup pod with full annotation (#3144)
 * [515bdb795](https://github.com/kubeovn/kube-ovn/commit/515bdb7952b0f3729e098bb55a55e5d9e47e6d81) fix ovn nat not clean (#3139)
 * [f225c66da](https://github.com/kubeovn/kube-ovn/commit/f225c66da128344007235f99119458e4ca4ca809) ovn: do not send direct traffic between lports to conntrack (#3131)
 * [e5c62d964](https://github.com/kubeovn/kube-ovn/commit/e5c62d964f69a6efdac2ca377dc0cfaa55fd0061) delete append externalIds process in initIPAM (#3134)
 * [bd4d99bee](https://github.com/kubeovn/kube-ovn/commit/bd4d99bee65f221ae9f2b996032722ee3cabe3c2) add e2e test for ovn db recover (#3118)
 * [74f69b279](https://github.com/kubeovn/kube-ovn/commit/74f69b279e9b4e87699628d2d9bf62114a8f9996) bump version number
 * [e1a1b78b0](https://github.com/kubeovn/kube-ovn/commit/e1a1b78b0c3dc6314a44ff69d9bfc9ff7cc4dc7f) docs: updated CHANGELOG.md (#3122)

### Contributors

 * bobz965
 * changluyi
 * dependabot[bot]
 * github-actions[bot]
 * hzma
 * 夜微澜
 * 张祖建
 * 马洪贞

## v1.12.0 (2023-08-08)

 * [15861418b](https://github.com/kubeovn/kube-ovn/commit/15861418b017b9573d71d970882073c08aa0a391) update changelog
 * [6cf53101f](https://github.com/kubeovn/kube-ovn/commit/6cf53101f620caafd7d08b424e1408e967adaca3) build(deps): bump sigs.k8s.io/controller-runtime from 0.15.0 to 0.15.1 (#3120)
 * [cd1202ca4](https://github.com/kubeovn/kube-ovn/commit/cd1202ca400a64bb5c3d68784eeb23586444d405) ovn: fix corrupted database file on start (#3112)
 * [02f8c630b](https://github.com/kubeovn/kube-ovn/commit/02f8c630b076e9a97cd8788714813a5c1858f634) some fixes in e2e (#3116)
 * [d8fa8395e](https://github.com/kubeovn/kube-ovn/commit/d8fa8395e4150bb6c31ec0dce2b447d0b8461c6d) controller: fix vpc update (#3117)
 * [b5b25ffd0](https://github.com/kubeovn/kube-ovn/commit/b5b25ffd086eed2d2f72cc32b3a5684cf002e6ea) increase event burst size (#3115)
 * [c8031f6ef](https://github.com/kubeovn/kube-ovn/commit/c8031f6ef60d25095a7a54b532aa6f0871cc39c0) build(deps): bump golang.org/x/sys from 0.10.0 to 0.11.0 (#3114)
 * [6ba997d20](https://github.com/kubeovn/kube-ovn/commit/6ba997d20b32555e02906a635ef00ce980cbb52b) 简化 ovn eip 类型 (#3107)
 * [a0c5e3895](https://github.com/kubeovn/kube-ovn/commit/a0c5e3895907bc9100a31d2d7d7039b2eed7a664) fix u2o policy route allocate too many openflows cause oom (#3099)
 * [a9fdbf92e](https://github.com/kubeovn/kube-ovn/commit/a9fdbf92ea09a2cfff184ecae4c72c632f0d213b) Fix relevant annotations are not deleted in hotnoplug nic process (#3108)
 * [3c6d6bc05](https://github.com/kubeovn/kube-ovn/commit/3c6d6bc051bc9a88dd1c55bec14685d42e99cd11) ovn: delete the db file if the node with new empty db file cannot join cluster for more than 120s (#3101)
 * [914bf6130](https://github.com/kubeovn/kube-ovn/commit/914bf6130e4f65bc58458e9dd61d72aa495f85f8) get all chassis once (#3103)
 * [42e0574c2](https://github.com/kubeovn/kube-ovn/commit/42e0574c2eb820ea8b1265dd633687ab92de5941) distinguish nat ip for central subnet with ecmp and active-standby (#3100)
 * [a27ce4c33](https://github.com/kubeovn/kube-ovn/commit/a27ce4c334db6d49d6c70dc8375667b5b9ead019) build(deps): bump github.com/osrg/gobgp/v3 from 3.16.0 to 3.17.0 (#3105)
 * [68dc1c380](https://github.com/kubeovn/kube-ovn/commit/68dc1c38023e4967e9644c89e72e63c605df8591) add log near err (#3098)
 * [c6c472a0e](https://github.com/kubeovn/kube-ovn/commit/c6c472a0e295dfd4189df9fd8cd47a69827fdb04) iptables: reject access to invalid service port when kube-proxy works in IPVS mode (#3059)
 * [f8835ef56](https://github.com/kubeovn/kube-ovn/commit/f8835ef5674af736136713e8867af9a59f173ff7) Ovn nat 1 (#3095)
 * [5704dae0c](https://github.com/kubeovn/kube-ovn/commit/5704dae0c7be8a86d70cd81f03602e7e5942b703) skip ok pod (#3090)
 * [18580edff](https://github.com/kubeovn/kube-ovn/commit/18580edffa990a65b61e3e49d356e0a4c603721e) ipam: return error for invalid ip range (#3088)
 * [a7e7a83d1](https://github.com/kubeovn/kube-ovn/commit/a7e7a83d12542f23a42e8f48ceef4a7c6b8fd9c7) some fixes in e2e (#3094)
 * [882187431](https://github.com/kubeovn/kube-ovn/commit/88218743189db426fd6d97272a670762b71ba1ae) bug_fix if only one port bind to the sg, then unbind the port to the sg ,it will not enforce in port_group (#3092)
 * [4c1161e92](https://github.com/kubeovn/kube-ovn/commit/4c1161e92f5d2ee2aecc4b4f88a4f9fc49a1e7fe) fix .status.default when initializing the default vpc (#3086)
 * [fa91428bc](https://github.com/kubeovn/kube-ovn/commit/fa91428bc2882e9d9dd82b78f6eb3dcc91500cd4) fix repeate set chassis (#3083)
 * [68a798f47](https://github.com/kubeovn/kube-ovn/commit/68a798f47aa4a9051a41dd7020d11ab1eda34910) build(deps): bump google.golang.org/grpc from 1.56.2 to 1.57.0 (#3085)
 * [da1648ccd](https://github.com/kubeovn/kube-ovn/commit/da1648ccd64c3436669d643e2d7a1b474d1d7cd9) fix go fmt
 * [88b5912fc](https://github.com/kubeovn/kube-ovn/commit/88b5912fce998957704fb689340911c213a8e902) fix kube-ovn-speaker log (#3081)
 * [89544c352](https://github.com/kubeovn/kube-ovn/commit/89544c35233fadb34ce8e0236d712ccbd6127b81) remove FOSSA status card
 * [ac648680e](https://github.com/kubeovn/kube-ovn/commit/ac648680ebc6b2655903bae5cb50c09ae90b0c12) cni-server: fix ovn mappings for vpc nat gateway (#3075)
 * [0fe9dcb27](https://github.com/kubeovn/kube-ovn/commit/0fe9dcb276e5dd9f7f0ca31b568cb40d3aabc49a) fix kube-ovn-speaker (#3076)
 * [827a5a273](https://github.com/kubeovn/kube-ovn/commit/827a5a2730f969477b60ae466e5aa4ca175073c1) build(deps): bump github.com/Microsoft/hcsshim from 0.9.10 to 0.10.0 (#3079)
 * [38cd9203a](https://github.com/kubeovn/kube-ovn/commit/38cd9203a56bd65f3a596775fa0a4f3ca75173e7) ovn client: fix sb chassis existence check (#3072)
 * [038ff7def](https://github.com/kubeovn/kube-ovn/commit/038ff7defef82eac3ab35fae55f034e0279d9e6c) e2e: fix switch lb rule test (#3071)
 * [e14ebbd58](https://github.com/kubeovn/kube-ovn/commit/e14ebbd58af2ef0599b6bfaa3dd171d6cc0388d0) bump github.com/docker/docker to v24.0.5 (#3073)
 * [90c830574](https://github.com/kubeovn/kube-ovn/commit/90c830574c9f7f6786201e4fb3ce2951accff7e9) iptables: add --random-fully to SNAT rules (#3066)
 * [1350397ec](https://github.com/kubeovn/kube-ovn/commit/1350397ec97c51f356be168f89f2fe49fd5511ab) update lint tmeout
 * [ff6d03d0e](https://github.com/kubeovn/kube-ovn/commit/ff6d03d0ec22260490a234b55bd775243a9c1f9d) build(deps): bump github.com/onsi/gomega from 1.27.9 to 1.27.10 (#3069)
 * [76b01496f](https://github.com/kubeovn/kube-ovn/commit/76b01496f3d5d4e03ee54b31a0ac1bf94ed3b6fa) bump k8s to v1.27.4 (#3063)
 * [d8e59ab61](https://github.com/kubeovn/kube-ovn/commit/d8e59ab619ec097a25ef0fbe26122ede73d4fd91) e2e: do not import pkg/daemon (#3055)
 * [20a6526c4](https://github.com/kubeovn/kube-ovn/commit/20a6526c407b5dbd69330c119681efb2a66903ca) build(deps): bump github.com/onsi/gomega from 1.27.8 to 1.27.9 (#3065)
 * [af335ba81](https://github.com/kubeovn/kube-ovn/commit/af335ba81a9789fde3fa9b7a4a41146ba00fd114) build(deps): bump github.com/Microsoft/hcsshim from 0.9.9 to 0.9.10 (#3061)
 * [976a32b01](https://github.com/kubeovn/kube-ovn/commit/976a32b01a84824f615211ad20ae94833c5b576b) ci: fix multus installation (#3062)
 * [0d1599ffd](https://github.com/kubeovn/kube-ovn/commit/0d1599ffdcca1428647f63e44db396aabf8081e2) add srl connectivity test (#3056)
 * [42f35a358](https://github.com/kubeovn/kube-ovn/commit/42f35a3588e90986668b0100d99424862d1c6ada) ipam: fix ippool with single dual-stack address (#3054)
 * [2ba3b8e54](https://github.com/kubeovn/kube-ovn/commit/2ba3b8e54cd06bc3b66c029d31ba9a8744fd4f61) controller: skip VIP gc if LB not found (#3048)
 * [52232b5e3](https://github.com/kubeovn/kube-ovn/commit/52232b5e33f8bd678d516602d8ef81d364c9757b) keep vm vip when enableKeepVmIP is true (#3053)
 * [ed58b2103](https://github.com/kubeovn/kube-ovn/commit/ed58b210302269f2d09b27087001b74fe50cd131) cni: reduce memory usage (#3047)
 * [3be1e84c2](https://github.com/kubeovn/kube-ovn/commit/3be1e84c2142dd89412d13a4896b40a7f91c0296) set genev_sys_6081 tx checksum off (#3045)
 * [4e761156c](https://github.com/kubeovn/kube-ovn/commit/4e761156c1ede9e44e85dbf2a03aca4f816a8712) fix vpc lb init (#3046)
 * [f4f80415a](https://github.com/kubeovn/kube-ovn/commit/f4f80415a06fa73d030e1becfb1a9d479eafdca6) custom vpc pod support tcp http probe with tproxy method (#3024)
 * [494209d3c](https://github.com/kubeovn/kube-ovn/commit/494209d3cf3e9177c2d311d6b7c063e67810393a) change log (#3042)
 * [b40c35b8c](https://github.com/kubeovn/kube-ovn/commit/b40c35b8c4076622886f63c6dae344764cc2a05d) Makefile: add deepflow and kwok installation (#3036)
 * [5a0686b27](https://github.com/kubeovn/kube-ovn/commit/5a0686b2700070d04fd31d50ffea1e3367202b50) windows: fix ovn patches (#3035)
 * [e3b7439d4](https://github.com/kubeovn/kube-ovn/commit/e3b7439d40c41b88525d87fb66a7cf1c03d2881a) ci: pin go version to 1.20.5 (#3034)
 * [97a3e1bc9](https://github.com/kubeovn/kube-ovn/commit/97a3e1bc9b192a76a11e338b609876d2d8c77165) static ip in exclude-ips can be allocated normally when subnet's availableIPs is 0 (#3031)
 * [9d88e4970](https://github.com/kubeovn/kube-ovn/commit/9d88e4970718cc377914470d9059a63c548ed900) pinger: use fully qualified domain name (#3032)
 * [f3833f91b](https://github.com/kubeovn/kube-ovn/commit/f3833f91b42d46692f618ec0b3f4976e95b85739) feat:  suport kubevirt nic hotplug (#3013)
 * [62f332897](https://github.com/kubeovn/kube-ovn/commit/62f332897a26f014031f7dbab1a8566f6d28fd22) fix lrp eip not clean (#3026)
 * [047af4a21](https://github.com/kubeovn/kube-ovn/commit/047af4a21607125b43b15a6f8ea6b213b931b1bf) build(deps): bump helm/kind-action from 1.7.0 to 1.8.0 (#3029)
 * [e01e616ee](https://github.com/kubeovn/kube-ovn/commit/e01e616ee0be12c54ab1ac54b03df9f002c55456) update maintainer
 * [ea9c1f1e5](https://github.com/kubeovn/kube-ovn/commit/ea9c1f1e5dad1d4342d851d37315631b696d6048) uninstall.sh: fix ipset name (#3028)
 * [9e6dc6366](https://github.com/kubeovn/kube-ovn/commit/9e6dc6366091c443bba60a0bc46fbedda29b0636) build(deps): bump github.com/docker/docker (#3027)
 * [3dd7f4ab7](https://github.com/kubeovn/kube-ovn/commit/3dd7f4ab788797831444148c4485869f61aca5b7) replace ovn legacy client with libovsdb (#3018)
 * [c5bfdb464](https://github.com/kubeovn/kube-ovn/commit/c5bfdb4645a7e2899c8b38ae51d01ff5c87ce2b2) install.sh: fix duplicate resources apply (#3023)
 * [2e4fb05cf](https://github.com/kubeovn/kube-ovn/commit/2e4fb05cf015751439115426df38ca1d7bad06f0) build(deps): bump github.com/docker/docker (#3019)
 * [aefaef5ab](https://github.com/kubeovn/kube-ovn/commit/aefaef5abeb6d6120fc5948e3112532cfa305e54) build(deps): bump google.golang.org/grpc from 1.56.1 to 1.56.2 (#3020)
 * [1f1fb82e4](https://github.com/kubeovn/kube-ovn/commit/1f1fb82e4fcaa4dd22d65d54843c79f2e26b09bc) ovn: fix cluster connections when SSL is enabled (#3001)
 * [18560c964](https://github.com/kubeovn/kube-ovn/commit/18560c964caf4b4a7f007f812c76f8a885665e34) cleanup.sh: wait for provier-networks to be deleted before deleting kube-ovn-cni (#3006)
 * [9304ae5a4](https://github.com/kubeovn/kube-ovn/commit/9304ae5a460f8dbff0e785aa703adef899de4109) kube-ovn-controller: fix workqueue metrics (#3011)
 * [d4153885e](https://github.com/kubeovn/kube-ovn/commit/d4153885e06248b42dbbab9c4b1e8aaf17dd4b0e) ci: fix go cache key (#3015)
 * [5a5f66ebf](https://github.com/kubeovn/kube-ovn/commit/5a5f66ebf55f38a0c4dfedf2de753dc6fe4300af) fix vlan subnet use logical gw can not access outside cluster node (#3007)
 * [18fd55ddf](https://github.com/kubeovn/kube-ovn/commit/18fd55ddf4a45c350a2561a4b2976999cc5b86c4) build(deps): bump github.com/prometheus-community/pro-bing (#3016)
 * [269f460d7](https://github.com/kubeovn/kube-ovn/commit/269f460d72167ba5018a94e95788ccac9588ba37) fix vpc already delete while delete policy route (#3005)
 * [e744d76ea](https://github.com/kubeovn/kube-ovn/commit/e744d76eac8d366b836abd0d7a39c4d5cbd127c2) make compatible with simplicified enable-eip-snat-cm (#3009)
 * [2a6525300](https://github.com/kubeovn/kube-ovn/commit/2a6525300c996afa2cdede47fc6f36398115244c) build(deps): bump golang.org/x/sys from 0.9.0 to 0.10.0 (#3012)
 * [d5f89bce7](https://github.com/kubeovn/kube-ovn/commit/d5f89bce73841b76993c43a94820e9c04fd0fe52) subnet: fix nat outgoing policy rule (#3003)
 * [8358a91e8](https://github.com/kubeovn/kube-ovn/commit/8358a91e8408c133adf20cfc4a889931a890184a) build(deps): bump github.com/osrg/gobgp/v3 from 3.15.0 to 3.16.0 (#3010)
 * [fe924e9f4](https://github.com/kubeovn/kube-ovn/commit/fe924e9f49cf00e419d943fa8453319d4d87d0ad) fix subnet finalizer (#3004)
 * [12366937b](https://github.com/kubeovn/kube-ovn/commit/12366937b1177496b2469d8e87bc67abe4daa4b2) chart: fix readOnly in volumes (#3002)
 * [d5462b10c](https://github.com/kubeovn/kube-ovn/commit/d5462b10c9d26dc99e287b5cc111e5c4f94cc57b) libovsdb: various bug fixes (#2998)
 * [af04530e9](https://github.com/kubeovn/kube-ovn/commit/af04530e9aa4db98ef25ac4f0a681dfcb3be0fd4) choose subnet by pod's annotation in networkpolicy (#2987)
 * [5c455499b](https://github.com/kubeovn/kube-ovn/commit/5c455499b35d91f2c976520927bc70822f727a65) IPPool: fix missing support for CIDR (#2982)
 * [f2d063a85](https://github.com/kubeovn/kube-ovn/commit/f2d063a853938a81c5be8ab3ce762a6b4e35d99e) kubectl ko performance enhance (#2975)
 * [d5d196e7c](https://github.com/kubeovn/kube-ovn/commit/d5d196e7c1fdc39ab50ce0fd87c5c5340f00d5d3) fix deleting old sb chassis for a re-added node (#2989)
 * [30cd09e6e](https://github.com/kubeovn/kube-ovn/commit/30cd09e6e792ca0cf648614155fae423379c9644) add e2e for new ippool feature (#2981)
 * [5fdf1f9ee](https://github.com/kubeovn/kube-ovn/commit/5fdf1f9ee08955d1c29b1079c3cbb47c62ffbd8a) underlay: fix NetworkManager syncer for virtual interfaces (#2988)
 * [1bb51239c](https://github.com/kubeovn/kube-ovn/commit/1bb51239cf8e26055cf230315edb0e09f7bc7792) underlay: does not set a device managed to no if it has VLAN managed by NM (#2986)
 * [3793e9937](https://github.com/kubeovn/kube-ovn/commit/3793e99370206a410743e1c96839e11ad0647b4f) build(deps): bump google.golang.org/protobuf from 1.30.0 to 1.31.0 (#2985)
 * [6a5bfe46d](https://github.com/kubeovn/kube-ovn/commit/6a5bfe46dd6b2e9124e005386191672e4b82f3e6) support helm install  hybrid_dpdk ovs-ovn (#2980)
 * [dc40a8cb6](https://github.com/kubeovn/kube-ovn/commit/dc40a8cb6df0d3fbba171069c1484e3c87ae9157) add unittest for IPAM (#2977)
 * [daa436d3c](https://github.com/kubeovn/kube-ovn/commit/daa436d3c1d4ea099faff713be0affb34ff121cb) IPAM: fix subnet mutex not released when static IP is out of range (#2979)
 * [65fd8a4aa](https://github.com/kubeovn/kube-ovn/commit/65fd8a4aa4d596363131ba710a50bfbc052e4071) fix initialization check of vpc nat gateway configuration (#2978)
 * [e558702de](https://github.com/kubeovn/kube-ovn/commit/e558702de597f99eb0591cb8e2bf31fd084cfd0b) refactor: make qos test cases parallel (#2957)
 * [27685a173](https://github.com/kubeovn/kube-ovn/commit/27685a173a7f537e4ffd1539a3a655c8354ee59d) IPAM: add support for ippool (#2958)
 * [41b4f266d](https://github.com/kubeovn/kube-ovn/commit/41b4f266dc565538b1711e7dd876cfd34c8ab894) build(deps): bump google.golang.org/grpc from 1.56.0 to 1.56.1 (#2974)
 * [57b01b4ad](https://github.com/kubeovn/kube-ovn/commit/57b01b4ad210d1c3845867529c211ba1b2960e2b) ovn ic support dual (#2970)
 * [7a14cf219](https://github.com/kubeovn/kube-ovn/commit/7a14cf21958a50e9c5b40fa1fe836294b64f7bee) base: fix ovn patches (#2971)
 * [a5194e668](https://github.com/kubeovn/kube-ovn/commit/a5194e6682c9b2603b7857024193d4ea59efec87) build(deps): bump github.com/onsi/ginkgo/v2 from 2.10.0 to 2.11.0 (#2968)
 * [a5e63c720](https://github.com/kubeovn/kube-ovn/commit/a5e63c7209c61491179d7b272e87e37592f0b3f4) add detail comment (#2969)
 * [13256fab7](https://github.com/kubeovn/kube-ovn/commit/13256fab7002db60de4f2df0c89bb986ef4f9dc8) 1. add host multicast perf (#2965)
 * [33b6df120](https://github.com/kubeovn/kube-ovn/commit/33b6df1202ce53c02a8707895113f22beae2b1ca) cni-server: reconcile ovn0 routes periodically (#2963)
 * [e4f682675](https://github.com/kubeovn/kube-ovn/commit/e4f682675319eb80289bbf68a008619278ff13f6) uninstall.sh: flush and delete iptables chain OVN-MASQUERADE (#2961)
 * [9fbebd340](https://github.com/kubeovn/kube-ovn/commit/9fbebd3406cacf4f10c3afbfc55b25b407e84f90) fix e2e failed (#2960)
 * [5de251721](https://github.com/kubeovn/kube-ovn/commit/5de251721684e01cf50511c9ea59faa4a0e6d60c) u2o specify u2oip from v1.9 (#2934)
 * [30ea6d6c4](https://github.com/kubeovn/kube-ovn/commit/30ea6d6c4679b1b206aec0a9e8bb55b1b25fe226) underlay: sync NetworkManager IP config to OVS bridge (#2949)
 * [27a2f300c](https://github.com/kubeovn/kube-ovn/commit/27a2f300c918fccc588728e6fe07b02253b4f5ca) chore: USERS.md (#2955)
 * [1c29580e9](https://github.com/kubeovn/kube-ovn/commit/1c29580e9ee1e52943956cd96ed746adcf51a07e) bump k8s version to v1.27.3 (#2953)
 * [c0730acb7](https://github.com/kubeovn/kube-ovn/commit/c0730acb7c998c8257714c8dd9253628d36bb92a) ci: fix build-base strategy (#2950)
 * [f52b15091](https://github.com/kubeovn/kube-ovn/commit/f52b15091d21a18a0e1928c55788f035f206edf7) e2e: add qos policy test cases (#2924)
 * [d8739d29b](https://github.com/kubeovn/kube-ovn/commit/d8739d29bdead176da5d4594fdebc7a3f836ec53) typo (#2952)
 * [de9c96036](https://github.com/kubeovn/kube-ovn/commit/de9c9603679bcb2957becaca1f78e54c3a006168) build(deps): bump google.golang.org/grpc from 1.55.0 to 1.56.0 (#2951)
 * [f4b3c0fe1](https://github.com/kubeovn/kube-ovn/commit/f4b3c0fe190676237d4ca3c6b484e4be6e91c783) build(deps): bump github.com/prometheus/client_golang (#2948)
 * [e13c2005d](https://github.com/kubeovn/kube-ovn/commit/e13c2005d65a8c7040fd873a640968daaf37c304) Revert "nm not managed only in the change provide nic name case (#2754)" (#2944)
 * [765dc8d7f](https://github.com/kubeovn/kube-ovn/commit/765dc8d7f74560e70d7b41c40576a4150b1fbffa) add permision for test-server.sh (#2942)
 * [a9d0b4be5](https://github.com/kubeovn/kube-ovn/commit/a9d0b4be58d631d5871d7df122d7fa27d8bc134a) Kubectl ko diagnose perf (#2915)
 * [8f414f724](https://github.com/kubeovn/kube-ovn/commit/8f414f724911c00d4722c7ff7dc0e127540301cd) build(deps): bump golang.org/x/sys from 0.8.0 to 0.9.0 (#2940)
 * [88f706e41](https://github.com/kubeovn/kube-ovn/commit/88f706e412dc0392224f9ef47a34e05a08061142) controller: fix DHCP MTU when the default network mode is underlay (#2941)
 * [ea56b5603](https://github.com/kubeovn/kube-ovn/commit/ea56b56034b2a3e9a5de150765e6e98f02c3b686) e2e: fix u2o case (#2931)
 * [c1c716f18](https://github.com/kubeovn/kube-ovn/commit/c1c716f1834c8b174732136c438153cfe5ecc235) add err log to help find conflict ip owner (#2939)
 * [1f27076f7](https://github.com/kubeovn/kube-ovn/commit/1f27076f7c8bb1b6b1452dab4bd00438a5258d85) support set the mtu of  dhcpv4_options (#2930)
 * [f1d2011a9](https://github.com/kubeovn/kube-ovn/commit/f1d2011a9b95c4c94bb9993b967af4355df13084) modify lb-svc dnat port error (#2927)
 * [d7edac78a](https://github.com/kubeovn/kube-ovn/commit/d7edac78a115e6ec8505f3729e344c48845cd885) fix race condition in gateway check logs (#2928)
 * [fc7c16ae7](https://github.com/kubeovn/kube-ovn/commit/fc7c16ae7d006e6a951eb051cba3af4461598296) add subnet.spec.u2oInterconnectionIP (#2921)
 * [6105d57b4](https://github.com/kubeovn/kube-ovn/commit/6105d57b404555ad2a1345a6139221ed4d40cfe3) disable ai review
 * [8773ea3d4](https://github.com/kubeovn/kube-ovn/commit/8773ea3d460351eacadf5e6d70dba8cd815e48be) e2e: fix waiting deployment to be restarted (#2909)
 * [56927913e](https://github.com/kubeovn/kube-ovn/commit/56927913e2b227e1c5850cb6b3d83e1cbb0e4c4c) make conformance with underlay  pn vlan subnet has no gw (#2908)
 * [0356a63fe](https://github.com/kubeovn/kube-ovn/commit/0356a63fe092bb7a33bdabb4d92016d2076b7c49) fix: natgw init check command not work (#2923)
 * [3a8e13ee3](https://github.com/kubeovn/kube-ovn/commit/3a8e13ee3b137906d0a7430e88017ef772a1cced) fix issue 2916 (#2917)
 * [517d3791a](https://github.com/kubeovn/kube-ovn/commit/517d3791a9363dd3635ca37b40cbd0cdd2906931) add sync map to fix cocurrent write (#2918)
 * [dff950b18](https://github.com/kubeovn/kube-ovn/commit/dff950b18c790820354f1b22c52aeda5d4cf8929) cni-server: clear iptables mark before doing masquerade (#2919)
 * [d043a2d2e](https://github.com/kubeovn/kube-ovn/commit/d043a2d2edfb18a874f5c73b2354c2e8dd40d539) build(deps): bump github.com/onsi/ginkgo/v2 from 2.9.7 to 2.10.0 (#2913)
 * [525b0b76d](https://github.com/kubeovn/kube-ovn/commit/525b0b76d4ddad8d248bda0b845ecbc21fb944a1) build(deps): bump github.com/onsi/gomega from 1.27.7 to 1.27.8 (#2914)
 * [3616d3de0](https://github.com/kubeovn/kube-ovn/commit/3616d3de045d9a812a89c43d6af7c18a83fe11c3) For eip created without spec.V4ip this field (#2912)
 * [ace0b970b](https://github.com/kubeovn/kube-ovn/commit/ace0b970b22c42a342b46d94d3bdd613dcb8e45f) match outgoing interface when perform snat (#2911)
 * [d61a2ad64](https://github.com/kubeovn/kube-ovn/commit/d61a2ad64d8285ef3c68956facb047b8288b5529) libovsdb: ignore not found error when listing objects with a filter (#2900)
 * [78f923a9f](https://github.com/kubeovn/kube-ovn/commit/78f923a9f21301705a202580ecdd4090a2c02513) build(deps): bump github.com/sirupsen/logrus from 1.9.2 to 1.9.3 (#2903)
 * [0e27e0caa](https://github.com/kubeovn/kube-ovn/commit/0e27e0caa26767e38b395baefe65b6e0dcf044a1) build(deps): bump github.com/osrg/gobgp/v3 from 3.14.0 to 3.15.0 (#2904)
 * [fd92c2a87](https://github.com/kubeovn/kube-ovn/commit/fd92c2a87ec6979f306104d7bafb4ecf0643862e) fix base build
 * [668287af8](https://github.com/kubeovn/kube-ovn/commit/668287af888224a48438de9315b011d1010f465d) fix build base ci
 * [a17461409](https://github.com/kubeovn/kube-ovn/commit/a1746140969f2fbe7d57b0f8a6fe0aff307fe5f0) fix build base ci
 * [2f52d9297](https://github.com/kubeovn/kube-ovn/commit/2f52d9297eef625e9a85fabd8a06642e13d99763) refactor IPAM (#2896)
 * [db51370fd](https://github.com/kubeovn/kube-ovn/commit/db51370fdc4ef8ee5a61732893878208b74a926c) add e2e u2o vpc version check (#2901)
 * [6acecb604](https://github.com/kubeovn/kube-ovn/commit/6acecb6046b8ee929582c69831593f34f476bfff) kube-ovn-controller: fix subnet update (#2882)
 * [35aa8b403](https://github.com/kubeovn/kube-ovn/commit/35aa8b4035800e620f7a699c1759bf8a09cdeb22) Supporting user-defined kubelet directory (#2893)
 * [3883a744c](https://github.com/kubeovn/kube-ovn/commit/3883a744c7f799b4c2f40abd260d4ff74854a96a) ci: use latest golangci-lint
 * [efe3ee34f](https://github.com/kubeovn/kube-ovn/commit/efe3ee34fc4ab30b01aa4349502f774ce6165bb1) underlay: do not delete patch ports created by ovn-controller (#2851)
 * [04c64f0ae](https://github.com/kubeovn/kube-ovn/commit/04c64f0aee60ed2d988460fe2986db03c61c7b61) update pr-review
 * [aa1ffaa3a](https://github.com/kubeovn/kube-ovn/commit/aa1ffaa3a5fc349c623e7e51ab9d0c5e1655ee3c) auto build base for release branches
 * [fe4eec778](https://github.com/kubeovn/kube-ovn/commit/fe4eec7783655cc6da120a06eecf6bad194205d2) Add natoutgoing policy rules (#2883)
 * [bbe04e89d](https://github.com/kubeovn/kube-ovn/commit/bbe04e89dac9b69c452f95c3a036b9b94bae19ba) pin golangci-lint version
 * [0c5f90553](https://github.com/kubeovn/kube-ovn/commit/0c5f90553f65aa083fe2cf0853ad8a4d54113c36) skip case 'connect to NodePort service with external traffic policy set to Local from other nodes' (#2895)
 * [93f027f3e](https://github.com/kubeovn/kube-ovn/commit/93f027f3e602b56db6934e1e344a3f8363a61110) refactor subnet gateway (#2872)
 * [33c526237](https://github.com/kubeovn/kube-ovn/commit/33c52623700d0284a5701ee4b4f9446a3fef00bc) update webhook check (#2878)
 * [a123be78e](https://github.com/kubeovn/kube-ovn/commit/a123be78eb0f37ee686d41fa1a3ea8d67eff93ce) skip pr-review as run out openai quota
 * [5c2c94873](https://github.com/kubeovn/kube-ovn/commit/5c2c948731b8cba961c95fa3f29d115bc8b1565e) skip kubectl cve
 * [403c2dcd7](https://github.com/kubeovn/kube-ovn/commit/403c2dcd7a70cc46d36dcf9dff6763b4d9701417) build(deps): bump github.com/onsi/ginkgo/v2 from 2.9.5 to 2.9.7 (#2890)
 * [589d0b6f8](https://github.com/kubeovn/kube-ovn/commit/589d0b6f8221953d9d3a4f749d6f4a6113cb831e) e2e: multiple external network (#2884)
 * [79521c318](https://github.com/kubeovn/kube-ovn/commit/79521c3185b988892f0e2cc7aea360b8c59ba9af) build(deps): bump github.com/stretchr/testify from 1.8.3 to 1.8.4 (#2885)
 * [71253fe5f](https://github.com/kubeovn/kube-ovn/commit/71253fe5fa925aec2d280f76f5aab4307e5829a1) fix vip str format (#2879)
 * [6b5345ff2](https://github.com/kubeovn/kube-ovn/commit/6b5345ff2aae3a6c6072593d444674bb0752909b) ci: fix valgrind result analysis (#2853)
 * [7c80a1358](https://github.com/kubeovn/kube-ovn/commit/7c80a13580d845c3d7b43366156f19902e185e12) ovs: fix memory leak in qos (#2871)
 * [9f39621a8](https://github.com/kubeovn/kube-ovn/commit/9f39621a8d57627e4b45cba5058d91b79e7c61e7) feat: vpc nat gw e2e (#2866)
 * [e68b983b1](https://github.com/kubeovn/kube-ovn/commit/e68b983b1041f01dcdafa72184c8725fbab20ce6) build(deps): bump github.com/docker/docker (#2875)
 * [056b4cf83](https://github.com/kubeovn/kube-ovn/commit/056b4cf83366a021da6fae13611b6969ef1fe40f) fix gc nil pointer (#2858)
 * [32b85219c](https://github.com/kubeovn/kube-ovn/commit/32b85219c3b55d33041551ac7d7bc2803a372f63) bump k8s to v1.27.2 (#2861)
 * [a80b3754c](https://github.com/kubeovn/kube-ovn/commit/a80b3754c39e02e01f42fe8c92eeba68a7ab29a7) add e2e test for slr (#2841)
 * [20b20366d](https://github.com/kubeovn/kube-ovn/commit/20b20366da888c15aa25730cb5eff205c5b63843) Move docs to new website (#2862)
 * [24d9dfeef](https://github.com/kubeovn/kube-ovn/commit/24d9dfeef0ceaf805326226b762ffa5407154b99) build(deps): bump gopkg.in/k8snetworkplumbingwg/multus-cni.v4 (#2860)
 * [83a47a734](https://github.com/kubeovn/kube-ovn/commit/83a47a734ff0f92f741a644b2424de73573716ed) update dependabot
 * [d6202bc7a](https://github.com/kubeovn/kube-ovn/commit/d6202bc7addf94abb9b89e42d060dbd770b42e66) refactor clusterrole for kube-ovn (#2833)
 * [b1c77ad73](https://github.com/kubeovn/kube-ovn/commit/b1c77ad737b3e2f94d92b97c13400b2b7863f164) some fixes in CI/e2e (#2856)
 * [a94fb0b21](https://github.com/kubeovn/kube-ovn/commit/a94fb0b2177a1cfc7f5e34f18c68dfaf240f51df) manage ovn bfd with libovsdb (#2812)
 * [d9a038ce9](https://github.com/kubeovn/kube-ovn/commit/d9a038ce9d15f39ffad938e3a8905c9ac42bd9e6) update the volumeMounts premission (#2852)
 * [d642f5b58](https://github.com/kubeovn/kube-ovn/commit/d642f5b583ba8225714c5c6533e3bb80a17b989d) fix vip lsp not clean (#2848)
 * [a1cf2b39b](https://github.com/kubeovn/kube-ovn/commit/a1cf2b39b894ceb09b36858563d2c1ba9ff814cf) U2o support custom vpc (#2831)
 * [2068d879c](https://github.com/kubeovn/kube-ovn/commit/2068d879c26f52b729f61e7e67cc89b7e2bfe309) kubectl-ko: fix trace when u2oInterconnection is enabled (#2836)
 * [6ee56d089](https://github.com/kubeovn/kube-ovn/commit/6ee56d089e477057223cd0ce1bd28f82ddcb721e) ci: detect ovs/ovn memory leak (#2839)
 * [776567739](https://github.com/kubeovn/kube-ovn/commit/776567739b40b057a5c244c4d3b192978d442f15) iptables: always do SNAT for access from other nodes to nodeport with external traffic policy set to Local (#2844)
 * [175fb262e](https://github.com/kubeovn/kube-ovn/commit/175fb262e9dc2a39879134e7304665f5ce3b71e2) fix underlay access to node through ovn0 (#2842)
 * [98392b3a1](https://github.com/kubeovn/kube-ovn/commit/98392b3a11fda41cb1229320dc9dcc45e1dd7a65) build(deps): bump github.com/docker/docker (#2843)
 * [da944a3ea](https://github.com/kubeovn/kube-ovn/commit/da944a3eabbc7144a991dbc2b5005acd296e1903) adapt vpc dns in master (#2822)
 * [c7b7a0a5f](https://github.com/kubeovn/kube-ovn/commit/c7b7a0a5f21f56d8849e8363381ebf8378c6c2f5) bump go dependencies (#2820)
 * [94d7cc867](https://github.com/kubeovn/kube-ovn/commit/94d7cc867208e1f2b4d555d57ae56e23fb2bd7b6) fix MTU when subnet is using logical gateway (#2834)
 * [486c61aef](https://github.com/kubeovn/kube-ovn/commit/486c61aefe221ca11f0d192470fd50f948d33623) refactor image builds (#2818)
 * [a7fd9ddf9](https://github.com/kubeovn/kube-ovn/commit/a7fd9ddf97480832480de202463bb0dbb13b5596) build(deps): bump github.com/stretchr/testify from 1.8.2 to 1.8.3 (#2832)
 * [667f5a7c6](https://github.com/kubeovn/kube-ovn/commit/667f5a7c6ef7c9858f6b082d7e5b8104363e2f25) build(deps): bump github.com/onsi/gomega from 1.27.6 to 1.27.7 (#2830)
 * [853abd9d9](https://github.com/kubeovn/kube-ovn/commit/853abd9d9d4270d46b5ee0456523f087970c68ba) vip support create arp proxy logical switch port (#2817)
 * [46bdd01ab](https://github.com/kubeovn/kube-ovn/commit/46bdd01abb0eeed0addd2b8656404235b72a6d91) build(deps): bump github.com/sirupsen/logrus from 1.9.0 to 1.9.2 (#2828)
 * [e988089ee](https://github.com/kubeovn/kube-ovn/commit/e988089ee95618a7915f25bea633887108174eae) build(deps): bump github.com/docker/docker (#2827)
 * [3b8c9edc5](https://github.com/kubeovn/kube-ovn/commit/3b8c9edc55e083468ed98217c1b688ce926ec2ff) add route for service ip range when init vpc-nat-gw (#2821)
 * [4f015f6d3](https://github.com/kubeovn/kube-ovn/commit/4f015f6d36f3d4a0a4b21cd5d971165cbff48f30) do not allocate MAC address when kube-ovn is called as an IPAM plugin (#2816)
 * [a30daea4f](https://github.com/kubeovn/kube-ovn/commit/a30daea4f0a571d0db4e1950a8744cf8476255d8) Iptables nat support share eip (#2805)
 * [0466edce6](https://github.com/kubeovn/kube-ovn/commit/0466edce660cb9ed041ac91e4ae96644bf0089fe) fix typos (#2815)
 * [fca6c9d51](https://github.com/kubeovn/kube-ovn/commit/fca6c9d51151864acff266a03807ab1f3b884ff1) fix some typos (#2814)
 * [630104d55](https://github.com/kubeovn/kube-ovn/commit/630104d55a570b99e98952217cec2d7766de3bd4) add iperf to test group multicast (#2796)
 * [2ba3846b0](https://github.com/kubeovn/kube-ovn/commit/2ba3846b097ec30e4b4bf257c12b4b038480197a) add available check for northd enpoint (#2799)
 * [253358ea8](https://github.com/kubeovn/kube-ovn/commit/253358ea83e4f3f01c2ec826a809f42743e58ea6) manage ovn lr static route with libovsdb (#2804)
 * [781b47d91](https://github.com/kubeovn/kube-ovn/commit/781b47d916c3f1bcff9b4f27f10f8b2bd81714a1) add support of user-defined endpoints to SwitchLBRule (#2777)
 * [74221a6e4](https://github.com/kubeovn/kube-ovn/commit/74221a6e48179528a88b4b24496d375745b78cb2) e2e: fix test container not removed (#2800)
 * [6ddd03bf2](https://github.com/kubeovn/kube-ovn/commit/6ddd03bf2f62ab0c99bb68cf05d903ba3c9466d2) manage ovn lr policy with libovsdb (#2788)
 * [7350db5fa](https://github.com/kubeovn/kube-ovn/commit/7350db5fad4d6a1f185e0fb2a52d5f61f324a29b) build(deps): bump github.com/docker/distribution (#2797)
 * [8f43028a2](https://github.com/kubeovn/kube-ovn/commit/8f43028a24040af1d4d6e65ec528f6d5b9e5d2f3) fix handedeletePod repeat 4 times (#2789)
 * [c8af3dd36](https://github.com/kubeovn/kube-ovn/commit/c8af3dd366d79ab6f9e6fc9c4ad3eb6261e6b604) fix cleanup order (#2792)
 * [b9542ad35](https://github.com/kubeovn/kube-ovn/commit/b9542ad3509d92af8ca0989f37adedac37303587) fix missing main route table for the default vpc (#2785)
 * [1511573dc](https://github.com/kubeovn/kube-ovn/commit/1511573dc4a05a04289f7b6a287c350a1c57399f) add ovn DVR fip e2e (#2780)
 * [0127e10a2](https://github.com/kubeovn/kube-ovn/commit/0127e10a20c53fb665a327cbeb8990bad2f8e023) build(deps): bump github.com/containernetworking/plugins (#2784)
 * [100227bee](https://github.com/kubeovn/kube-ovn/commit/100227bee623dadbc9a9ff3f65992e7353b5cd43) add key lock for more resources (#2781)
 * [16db5082d](https://github.com/kubeovn/kube-ovn/commit/16db5082d54cdde6a1f793578d7d953fb620128e) bump cni plugins to v1.3.0 (#2786)
 * [08e2e66f2](https://github.com/kubeovn/kube-ovn/commit/08e2e66f2e9db182a40b55a8f31fbb561a4dd99f) replace util.DefaultVpc with c.config.ClusterRouter (#2782)
 * [e1154acf6](https://github.com/kubeovn/kube-ovn/commit/e1154acf679fb5bd58f9dbdfac4b84ae1649a995) fix static route recreation after kube-ovn-controller restarts (#2778)
 * [e7190e6ae](https://github.com/kubeovn/kube-ovn/commit/e7190e6ae383db35bffccc153dfe86041ec783b7) clean up code about static routes (#2779)
 * [b1a339b77](https://github.com/kubeovn/kube-ovn/commit/b1a339b77b53ac4acc89c1b170a5396707cc8930) Reorder cleanup step by put subnet and vpc to the last to avoid conflict (#2776)
 * [a2b789cca](https://github.com/kubeovn/kube-ovn/commit/a2b789cca2a8b2106b60d9782e122e989450aff3) optimize kube-ovn-controller logic (#2771)
 * [3b2b07166](https://github.com/kubeovn/kube-ovn/commit/3b2b07166e99bf99f47b2f469f36f486c63551e1) use rate limiting queue with delaying for pod deletion events (#2774)
 * [04e4d2582](https://github.com/kubeovn/kube-ovn/commit/04e4d2582f9ae39df50b100b337ed057c66df571) fix underlay subnet kubectl ko trace error (#2773)
 * [9b1de4816](https://github.com/kubeovn/kube-ovn/commit/9b1de48169218d37d52941f8bf8280ff63f8aea1) feat: natgw qos (#2753)
 * [11b171e14](https://github.com/kubeovn/kube-ovn/commit/11b171e146f0a5672e8e9064a2f60820dc715cac) build(deps): bump github.com/docker/docker (#2770)
 * [62d8122c8](https://github.com/kubeovn/kube-ovn/commit/62d8122c828ead3344c3e4faf91ad7b527bddb1d) fix ip statistics in subnet status (#2769)
 * [d3d01762c](https://github.com/kubeovn/kube-ovn/commit/d3d01762c1640d4e6469e88ee5d9c6a16b07fb45) informer: wait for cache sync before adding event handlers (#2768)
 * [a23dd865a](https://github.com/kubeovn/kube-ovn/commit/a23dd865aa7eec08e315d025850cdc231478639b) build(deps): bump github.com/scylladb/go-set (#2766)
 * [e2bf60f76](https://github.com/kubeovn/kube-ovn/commit/e2bf60f767e9ce3e55f3f250f0479e2725bea64d) support disable arp check ip conflict in vlan provider network (#2760)
 * [c55cbd6e0](https://github.com/kubeovn/kube-ovn/commit/c55cbd6e0d1da7df3e69e5ac18b3f195edd7d3a6) replace string map with string set (#2765)
 * [99be9cb0e](https://github.com/kubeovn/kube-ovn/commit/99be9cb0effc22c1edf0e2ace3ba181279bf2bc1) cni-server: wait ovs-vswitchd to be running (#2759)
 * [1933ed872](https://github.com/kubeovn/kube-ovn/commit/1933ed872c37b386a0c60cd7caa745b51b39464a) kubectl-ko: support trace for pod with host network (#2761)
 * [bf1a3d7c4](https://github.com/kubeovn/kube-ovn/commit/bf1a3d7c424927c97543c46e207f650ba8069809) libovsdb: fix potential duplicate addresses (#2763)
 * [5585d447a](https://github.com/kubeovn/kube-ovn/commit/5585d447a874f40ed3c4dcd458c929efa2a8d784) ci: run kube-ovn e2e for underlay (#2762)
 * [cf1748c64](https://github.com/kubeovn/kube-ovn/commit/cf1748c64fdcf4d95ea4663191d42a1fe8e1cfc4) kubectl-ko: fix pod tracing in underlay (#2757)
 * [6db99d531](https://github.com/kubeovn/kube-ovn/commit/6db99d531f77e9373bffe13c71213ac3ebd4ba82) When Subnet spec.vpc is updated, the status in VPC should also be updated. (#2756)
 * [328a8911d](https://github.com/kubeovn/kube-ovn/commit/328a8911d557cb5deb5c901f10a3630dd728b273) ovn-nbctl: remove unused functions (#2755)
 * [86a07a30e](https://github.com/kubeovn/kube-ovn/commit/86a07a30eeafc2621fd33105218bcbc81251d83d) add route table option in static route for subnet (#2748)
 * [f6414ce18](https://github.com/kubeovn/kube-ovn/commit/f6414ce18e82ed140e636ee847b184ae0d8f1683) replace acl/address_set function call with ovnClient (#2648)
 * [c77f36818](https://github.com/kubeovn/kube-ovn/commit/c77f368187b5be35fe27a97ef60a09375e8ca226) nm not managed only in the change provide nic name case (#2754)
 * [cc1be3ee7](https://github.com/kubeovn/kube-ovn/commit/cc1be3ee79c1e85564515be6ef0f998ad45c53ad) support node local dns cache (#2733)
 * [d7fa2a491](https://github.com/kubeovn/kube-ovn/commit/d7fa2a491e3d1cffff20aa4d7ffb79ac924cb1a0) build(deps): bump google.golang.org/grpc from 1.54.0 to 1.55.0 (#2752)
 * [faff1e627](https://github.com/kubeovn/kube-ovn/commit/faff1e627c94edfbf722633e9c64a17702236a08) build(deps): bump golang.org/x/sys from 0.7.0 to 0.8.0 (#2751)
 * [bdd201b16](https://github.com/kubeovn/kube-ovn/commit/bdd201b16b24436ffa8947db5e8c8a222ba9564f) update eip qos procees, replace qosLabelEIP with natLabelEip (#2736)
 * [d1711acd2](https://github.com/kubeovn/kube-ovn/commit/d1711acd2d27b74815b2ef22a985c64b3f63d58f) refresh nat gw image before using it (#2743)
 * [353df49ac](https://github.com/kubeovn/kube-ovn/commit/353df49ac64134a483b1c258e8d64a886ad8820c) build(deps): bump github.com/prometheus/client_golang (#2745)
 * [91400eccc](https://github.com/kubeovn/kube-ovn/commit/91400eccc6269de60ea7a1553e97e0dce4f8701d) Using full repo name to avoid short-name error in podman (#2746)
 * [fa404a061](https://github.com/kubeovn/kube-ovn/commit/fa404a0614ac1bb1ad582818f2f364a8cddbe891) build(deps): bump github.com/osrg/gobgp/v3 from 3.13.0 to 3.14.0 (#2738)
 * [7eed8341a](https://github.com/kubeovn/kube-ovn/commit/7eed8341a7c96f182374d4e165df82c0c11a7225) add policy route when use old active gateway node for centralized subnet (#2722)
 * [66615b6d8](https://github.com/kubeovn/kube-ovn/commit/66615b6d86d50864650dd7e2f3dd988c3cd3933c) feat: support for multiple external network (#2725)
 * [f8328bdbf](https://github.com/kubeovn/kube-ovn/commit/f8328bdbf282b975999d964567e5d858f3a0e0ad) build(deps): bump github.com/docker/docker (#2732)
 * [6198f691b](https://github.com/kubeovn/kube-ovn/commit/6198f691b5102f51becbe21cdba397a28c8d2801) build(deps): bump github.com/Microsoft/hcsshim from 0.9.8 to 0.9.9 (#2731)
 * [2a015e5c4](https://github.com/kubeovn/kube-ovn/commit/2a015e5c461ef8178ecc24c2419949fee32fb00c) base: remove patch for fixing ofpbuf memory leak (#2715)
 * [a01f9606d](https://github.com/kubeovn/kube-ovn/commit/a01f9606da5121ae1fa1d4fee721ab6376bae5f7) fix recover db failed using method in (#2711)
 * [a6d2a53cc](https://github.com/kubeovn/kube-ovn/commit/a6d2a53cc53080b2e1324e13291e4858cc1db4f9) refactor: improve performance by using cache (#2713)
 * [7dbfd2be0](https://github.com/kubeovn/kube-ovn/commit/7dbfd2be0bc1ee06b4f52baa9871ef0e6b2367f0) For dualstack and ipv6 the default ipv6 range should be same with the ipv4 cidr. (#2708)
 * [15780bfbb](https://github.com/kubeovn/kube-ovn/commit/15780bfbb611bfef95c6f2ccfcc9858a4adf9bbd) feat: support dynamically changing qos for EIP (#2671)
 * [d865b48d1](https://github.com/kubeovn/kube-ovn/commit/d865b48d1ea8ae19e141c49799bff8f8487f3264) base: refactor dockerfile (#2696)
 * [53bfcf447](https://github.com/kubeovn/kube-ovn/commit/53bfcf447140f6c02b406ca666466875b5fbb567) kubectl-ko: add support for tracing nodes (#2697)
 * [f5fee4c93](https://github.com/kubeovn/kube-ovn/commit/f5fee4c936d3e398c08c791093f671a1b0c19c50) cni-server: do not perform ipv4 conflict detection during VM live migration (#2693)
 * [942b87d13](https://github.com/kubeovn/kube-ovn/commit/942b87d13a21c60666ae1075f0bfaba95e28e9e3) fix: iptables nat gw e2e not clean sts eth0 net1 ip (#2698)
 * [236574c75](https://github.com/kubeovn/kube-ovn/commit/236574c750edad001626c02170c21a280e50ca2c) Add random fully when nat (#2681)
 * [9e3f70c1e](https://github.com/kubeovn/kube-ovn/commit/9e3f70c1e2a69d1dbd5b924b00196b09d3592b12) replace StrategicMergePatchType with MergePatchType (#2694)
 * [b59bfd331](https://github.com/kubeovn/kube-ovn/commit/b59bfd331dff52b1fbde28894d28f97e6d6d2a1d) ci: fix scheduled vpc nat gateway e2e (#2692)
 * [d469235f9](https://github.com/kubeovn/kube-ovn/commit/d469235f967a0d519d3d9cbc4e710bc042eb00aa) ovn-controller: do not send GARP on localnet for Kube-OVN ports (#2690)
 * [7db85edd0](https://github.com/kubeovn/kube-ovn/commit/7db85edd06e472f9629cb4a0a6a76029115929e5) netpol: fix enqueueing network policy after LSP creation (#2687)
 * [aba724431](https://github.com/kubeovn/kube-ovn/commit/aba724431824034c8b712d03e3988d93e603523e) add tcp mem collector (#2683)
 * [07a6d4cae](https://github.com/kubeovn/kube-ovn/commit/07a6d4cae2b9854e99c55b702a6bdf406bb99ce8) fix manifest yamls (#2689)
 * [1d6a0fe4b](https://github.com/kubeovn/kube-ovn/commit/1d6a0fe4b4546e44030748df1e2959f9c2662324) attach node name label in ip cr (#2680)
 * [233dc61ec](https://github.com/kubeovn/kube-ovn/commit/233dc61ec23e17daac29bb8b2820048eaeef8c56) adapt ippool annotation (#2678)
 * [095dca268](https://github.com/kubeovn/kube-ovn/commit/095dca2682ae0e745eee4e6f0801c113818263b2) netpol: fix packet drop casued by incorrect address set deletion (#2677)
 * [3dc36c8c3](https://github.com/kubeovn/kube-ovn/commit/3dc36c8c3898aad19d15c005df0b3534ce17254c) fix kubectl ko using ovn-central pod that not in a good status (#2676)
 * [9c5523f7f](https://github.com/kubeovn/kube-ovn/commit/9c5523f7fb9c37e11bb9636ceb6c9f4e9d928183) add nat gw e2e (#2639)
 * [a9993dac3](https://github.com/kubeovn/kube-ovn/commit/a9993dac38dc1dc0d92065ae8149312e4d662ab7) add workflows for release chart (#2672)
 * [4399963e7](https://github.com/kubeovn/kube-ovn/commit/4399963e796744136f0dc96318bd0bddeaf3157e) build(deps): bump github.com/Microsoft/go-winio from 0.6.0 to 0.6.1 (#2663)
 * [d6b0c28df](https://github.com/kubeovn/kube-ovn/commit/d6b0c28df328afc01f7dd2f0a7d933f295f2048c) remove auto update k8s and cadvisor
 * [b57f36ff3](https://github.com/kubeovn/kube-ovn/commit/b57f36ff3a64db751a8bdc5376ca8940da57f3de) build(deps): bump k8s.io/sample-controller from 0.26.3 to 0.26.4 (#2675)
 * [a33adde2f](https://github.com/kubeovn/kube-ovn/commit/a33adde2f2291cd82704c1a41b05fcb868dc7f5b) ignore k8s major and minor dependencies as they always break build.
 * [68f813e0f](https://github.com/kubeovn/kube-ovn/commit/68f813e0fb5617cd524c7ecb87be9bc080b796cf) rename charts (#2667)
 * [933d76e3f](https://github.com/kubeovn/kube-ovn/commit/933d76e3fa419c343e586cff16c2bc11903cf1e3) ipam update condition refactor (#2651)
 * [05e725160](https://github.com/kubeovn/kube-ovn/commit/05e725160535b4e92efd442f69949d965806b639) fix LSP existence check (#2657)
 * [f84343e84](https://github.com/kubeovn/kube-ovn/commit/f84343e84b1bcd7ac0abf874c29ee90ea716dd0d) fix network policy issues (#2652)
 * [148f1bf4a](https://github.com/kubeovn/kube-ovn/commit/148f1bf4a5ca19bfba1af6e627ecc864e029406a) Resolve SetLoadBalancerAffinityTimeout not being effective (#2647)
 * [0b5fc5d36](https://github.com/kubeovn/kube-ovn/commit/0b5fc5d36ac7c52e38150934c1f91c03f12ee804) broadcast free arp when pod is setup (#2638)
 * [dc31cbd20](https://github.com/kubeovn/kube-ovn/commit/dc31cbd2038fa4ca4947d465bc59040f62500676) delete sync user (#2629)
 * [7e872fbe5](https://github.com/kubeovn/kube-ovn/commit/7e872fbe53c7420f0a90a10c23a6f92033be098b) fix: eip qos (#2632)
 * [ddf28fc2f](https://github.com/kubeovn/kube-ovn/commit/ddf28fc2f020bffda95a9d206e42769b6888eaeb) fix: make webhook port configurable. (#2631)
 * [c53d58dac](https://github.com/kubeovn/kube-ovn/commit/c53d58dac3d240205add1de6e0f6d5d89a9842c1) support ovn ipsec  (#2616)
 * [53bf75d21](https://github.com/kubeovn/kube-ovn/commit/53bf75d218a6c0cf994ee49adcdca8dc4d67baa0) feat: add support for EIP QoS (#2550)
 * [1fc5d8534](https://github.com/kubeovn/kube-ovn/commit/1fc5d85346a38593803fb0c7d8c344e4b5d6c4ed) libovsdb: fix race condition in OVN LB operations (#2625)
 * [cfff2db3d](https://github.com/kubeovn/kube-ovn/commit/cfff2db3ded9a5f5c3bce5fc7e4d19e116fe68f8) fix IPAM allocation caused by incorrect pod annotations patch (#2624)
 * [3e67e8935](https://github.com/kubeovn/kube-ovn/commit/3e67e89354ba62269bceaac08869bc2e4717f4f3) ci: deploy multus in thick mode (#2628)
 * [1caaea2ab](https://github.com/kubeovn/kube-ovn/commit/1caaea2ab5b04a5d8573da5b0dfb1a41ea2dd601) libovsdb: use monitor_cond as the monitor method (#2627)
 * [c0ab8351c](https://github.com/kubeovn/kube-ovn/commit/c0ab8351c9f5776553f7f10772531ba162b0e173) ci: fix multus installation (#2622)
 * [84a910b02](https://github.com/kubeovn/kube-ovn/commit/84a910b0235ccecf24950222dfacb64fdfd3de37) ovs: fix dpif-netlink ofpbuf memory leak (#2620)
 * [42a86869e](https://github.com/kubeovn/kube-ovn/commit/42a86869e3fc3d12682f395c65c9b6a38a9a85de) Optimized tolerations code in vpc-nat-gw (#2613)
 * [1e8e38288](https://github.com/kubeovn/kube-ovn/commit/1e8e38288c9accd275ab65168ec82d82962c0606) replace port_group function call with ovnClient (#2608)
 * [9b577403b](https://github.com/kubeovn/kube-ovn/commit/9b577403ba071c267d606affb9fcdd438b841e67) reduce test binary size and add missing webhook build (#2610)
 * [949eb8b73](https://github.com/kubeovn/kube-ovn/commit/949eb8b73f5eb4d2a560ff9cae9cc75875fcb807) fix: ovneip print column and finalizer (#2593)
 * [5babe8e69](https://github.com/kubeovn/kube-ovn/commit/5babe8e69fd31c114e844fc1da9a07ef96e2a9d8) add affinity to vpc-nat-gw (#2609)
 * [6bf15d4a4](https://github.com/kubeovn/kube-ovn/commit/6bf15d4a4cb3edc890501daf090ad4095b804e8e) ci: fix multus installation (#2604)
 * [8629d6340](https://github.com/kubeovn/kube-ovn/commit/8629d6340d7f100c2f0795c180ad342db94f61f8) update .gitignore (#2600)
 * [254598fbd](https://github.com/kubeovn/kube-ovn/commit/254598fbdca25920a5f72ade1e60722344f8a57b) bump go modules (#2603)
 * [602b16052](https://github.com/kubeovn/kube-ovn/commit/602b16052ff139bd2bf32fbafe91164730e2b4cd) build(deps): bump peter-evans/create-pull-request from 4 to 5 (#2606)
 * [787616f1c](https://github.com/kubeovn/kube-ovn/commit/787616f1c2fde385b47ceca9f8f622b27287901e) build(deps): bump github.com/docker/docker (#2605)
 * [62b8761d0](https://github.com/kubeovn/kube-ovn/commit/62b8761d0031ed73279f92db1b7ee4d0f8861046) build(deps): bump golang.org/x/sys from 0.6.0 to 0.7.0 (#2607)
 * [d2523f466](https://github.com/kubeovn/kube-ovn/commit/d2523f466a258a14e03766ec2b2c7a2204c0daaf) cut invalid OVN_NB_DAEMON to make log more readable (#2601)
 * [4c7ddc68d](https://github.com/kubeovn/kube-ovn/commit/4c7ddc68d952eedd44c58730ec8bf7cf514a3a30) unittest: fix length assertion (#2597)
 * [7ba428d71](https://github.com/kubeovn/kube-ovn/commit/7ba428d7101cc2a25e196aa1c3e76a33ee65bf33) use copilot to generate pr content
 * [1a474fd98](https://github.com/kubeovn/kube-ovn/commit/1a474fd988345e87ce5170d81c9570fc5df4f133) replace lb function call with ovnClient (#2598)
 * [a73deb47a](https://github.com/kubeovn/kube-ovn/commit/a73deb47a0b6799a9030912fbc58b6d8bc53f6af) build(deps): bump github.com/osrg/gobgp/v3 from 3.12.0 to 3.13.0 (#2596)
 * [2fb1f95a9](https://github.com/kubeovn/kube-ovn/commit/2fb1f95a9aa960f3991872b40f771bbfe9f551ce) Merge handleAddPod with handleUpdatePod. (#2563)
 * [9399c1e1f](https://github.com/kubeovn/kube-ovn/commit/9399c1e1f64e2ae9df9004e0135d154f43201c4c) fix log (#2586)
 * [da323a52d](https://github.com/kubeovn/kube-ovn/commit/da323a52d103ee1f6cc6c95bfa37c7bb02df9d29) fix: ovn snat and fip delete (#2584)
 * [048e9315f](https://github.com/kubeovn/kube-ovn/commit/048e9315fa58c1d2347aabba74e05c4633b28b0b) underlay: get address/route before setting nm managed to no (#2592)
 * [5d036cd56](https://github.com/kubeovn/kube-ovn/commit/5d036cd560fa0671f13e72572592d182278b6682) update chart description (#2582)
 * [6d50bdc36](https://github.com/kubeovn/kube-ovn/commit/6d50bdc36167737ebb655e8359aa004030dfde83) iptables: use the same mode with kube-proxy (#2535)
 * [09477984f](https://github.com/kubeovn/kube-ovn/commit/09477984f6f4ad8007a611035de12c6b96eacd6b) ci: bump kind image to v1.26.3 (#2581)
 * [5b7bdccb0](https://github.com/kubeovn/kube-ovn/commit/5b7bdccb0099d1dff46b68999acbef97c4838d75) fix: invalid memory address (#2585)
 * [cba9c16e6](https://github.com/kubeovn/kube-ovn/commit/cba9c16e652b8a4a1e398f12f9c0ac43633cfe1e) kubectl ko change solution to collect logs to path kubectl-ko-log (#2575)
 * [bb268618f](https://github.com/kubeovn/kube-ovn/commit/bb268618fa0485a817147f50c78d843d7b9e0b59) if one item is removed, do not requeue (#2578)
 * [5aad7c535](https://github.com/kubeovn/kube-ovn/commit/5aad7c535da3c4bf87d3b7199d8ca15595e76ac1) build(deps): bump github.com/onsi/gomega from 1.27.5 to 1.27.6 (#2579)
 * [a9d66220d](https://github.com/kubeovn/kube-ovn/commit/a9d66220da1624d637d1d36235689111f3d7c396) fix vpc dns when ovn-default is dualstack (#2576)
 * [279717ca7](https://github.com/kubeovn/kube-ovn/commit/279717ca7d1a94d0a32974693be8226402556539) move the vpc-nat generic configurations into one single ConfigMap (#2574)
 * [887df215a](https://github.com/kubeovn/kube-ovn/commit/887df215a42a676782505895322a557ade855615) feat: add ovn dnat (#2565)
 * [02a868734](https://github.com/kubeovn/kube-ovn/commit/02a868734adb662381bb3a6b0241cd7d64d2257d) Fix kubectl ko log loss when restart deployment or ds (#2531)
 * [1d1f5fabb](https://github.com/kubeovn/kube-ovn/commit/1d1f5fabbeccf8ce3523266b95d3780704f7da84) add wait until (#2569)
 * [c0e843fd4](https://github.com/kubeovn/kube-ovn/commit/c0e843fd42ff2eaffe0af0770056f9b8b5638089) do no review dependency update
 * [a7ccd1ae0](https://github.com/kubeovn/kube-ovn/commit/a7ccd1ae0c73fe45ec1f1559f141714845787096) build(deps): bump github.com/opencontainers/runc from 1.1.4 to 1.1.5 (#2572)
 * [5dce9cd2c](https://github.com/kubeovn/kube-ovn/commit/5dce9cd2c80fbd6b80093988216fd1672155ddc6) move ipam.subnet.mutex to caller (#2571)
 * [9fba0b548](https://github.com/kubeovn/kube-ovn/commit/9fba0b548c5489ec183f9740c945ec7d7cec0aaf) build(deps): bump sigs.k8s.io/controller-runtime from 0.14.5 to 0.14.6 (#2568)
 * [3f7997b31](https://github.com/kubeovn/kube-ovn/commit/3f7997b319083e1f0b87a1739804849f299a6c2a) fix: memory leak in IPAM caused by leftover map keys (#2566)
 * [1e9f35299](https://github.com/kubeovn/kube-ovn/commit/1e9f3529980c6ab37f68dc00f5d812cc8e102d55) build(deps): bump github.com/docker/docker (#2567)
 * [8e03e97b8](https://github.com/kubeovn/kube-ovn/commit/8e03e97b84101c331d097236b22f444750850a18) fix ovn-bridge-mappings deletion (#2564)
 * [e19620b0a](https://github.com/kubeovn/kube-ovn/commit/e19620b0aad1abbb61b5046789aa6c406d590f5e) fix lrp deletion after upgrade (#2548)
 * [ed9283489](https://github.com/kubeovn/kube-ovn/commit/ed92834898238c7e9ee2696e5255d348311a6864) fix gw label for vpc update field (#2562)
 * [642fa92a2](https://github.com/kubeovn/kube-ovn/commit/642fa92a2420305db232e1d8db16f984ad630d29) update CRD in helm chart (#2560)
 * [1a41369d6](https://github.com/kubeovn/kube-ovn/commit/1a41369d64482dff27cff5acc87d0aa4015c3302) fix CRD indent in install.sh (#2559)
 * [f955143f7](https://github.com/kubeovn/kube-ovn/commit/f955143f7cad8cfc29ed5923117e138c9f326335) fix update snat rules not effect correctly (#2554)
 * [fd6ec3d8a](https://github.com/kubeovn/kube-ovn/commit/fd6ec3d8af336b13a11b1fe8edab04d90d224b74) fix go mod list (#2556)
 * [b4e7e2e87](https://github.com/kubeovn/kube-ovn/commit/b4e7e2e87983a0ded5f796de5cc8f9e6bb396da1) do not set device unmanaged if NetworkManager is not running (#2549)
 * [fe1b4ac62](https://github.com/kubeovn/kube-ovn/commit/fe1b4ac6242fe9ef5ea871d61de06b22e6ea7c0c) update review bot
 * [f9eb0ca41](https://github.com/kubeovn/kube-ovn/commit/f9eb0ca41ecc0d31bbdeadf970cc01e91ba69db7) build(deps): bump github.com/onsi/gomega from 1.27.4 to 1.27.5 (#2551)
 * [955cf0ffb](https://github.com/kubeovn/kube-ovn/commit/955cf0ffba719f5bf9fc7a4184ebcb1ef4c3ef93) underlay: fix network manager operation (#2546)
 * [b8fc9d9a0](https://github.com/kubeovn/kube-ovn/commit/b8fc9d9a05bc4bd438f677bb644075b73f3c4aef) controller: fix apiserver connection timeout on startup (#2545)
 * [2ae8a9afc](https://github.com/kubeovn/kube-ovn/commit/2ae8a9afc1de5e9951f5715914747a8e0b468ef4) fix update fip rules not effect correctly (#2540)
 * [98dc2f251](https://github.com/kubeovn/kube-ovn/commit/98dc2f251aafccf7c2791e88f3b626e6b41b3292) fix lsp deletion failure when external-ids:ls is empty (#2544)
 * [6b9cdd33a](https://github.com/kubeovn/kube-ovn/commit/6b9cdd33a1edf44a6f579eb4c895f9d829f96e41) sync parameters to charts from install script (#2526)
 * [8c49fc012](https://github.com/kubeovn/kube-ovn/commit/8c49fc012f8506243acfcfdb8307c890d08f6576) underlay: delete altname after renaming the link (#2539)
 * [2a81f4043](https://github.com/kubeovn/kube-ovn/commit/2a81f40433e6b8c31eaff1d6789994fd3f1221f1) failed to delete ovn-fip or ovn-snat (#2534)
 * [17807e555](https://github.com/kubeovn/kube-ovn/commit/17807e5553039141716543ba40cd7f8974dd573a) fix encap_ip will be lost when we restart the ovs-dpdk node (#2543)
 * [829e74c2f](https://github.com/kubeovn/kube-ovn/commit/829e74c2fce41510628b745fa8d09c3a804a49a0) fix service fail (#2537)
 * [bd91f8b86](https://github.com/kubeovn/kube-ovn/commit/bd91f8b86c6ce1bd000692b91cb04da916770936) Add speaker param check (#2538)
 * [7e6feabe8](https://github.com/kubeovn/kube-ovn/commit/7e6feabe89ea87b4f95c1e376886cabd56024225) feat: support nic-hotplug to a running pod. (#2521)
 * [bbe1f3e88](https://github.com/kubeovn/kube-ovn/commit/bbe1f3e884d5d8cb8b27215412e0d60cfa6ccf5c) build(deps): bump google.golang.org/grpc from 1.53.0 to 1.54.0 (#2541)
 * [ae51a6561](https://github.com/kubeovn/kube-ovn/commit/ae51a6561121195f7a6341885714514f4dc3e8a1) fix update dnat rules not effect correctly (#2518)
 * [569b576ac](https://github.com/kubeovn/kube-ovn/commit/569b576ac4e4c42890721909996071147ca0fcd9) underlay: fix link name exchange (#2516)
 * [e97109590](https://github.com/kubeovn/kube-ovn/commit/e97109590322bcc1d035e53d9406965138960dc3) add vip to webhook e2e (#2525)
 * [30d30bfea](https://github.com/kubeovn/kube-ovn/commit/30d30bfead7ab67242b00faa63bf62ced4f83710) fix submariner e2e (#2519)
 * [9eda4859f](https://github.com/kubeovn/kube-ovn/commit/9eda4859fda67897b429034441f59e365b6723d6) fix lsp gc after upgrade (#2513)
 * [0b8964c9e](https://github.com/kubeovn/kube-ovn/commit/0b8964c9e0a6106c3ad05e9d0ae8677cc5b9da26) fix: ovn-fip creation failure due to an excessively long label (#2529)
 * [cc8a11d72](https://github.com/kubeovn/kube-ovn/commit/cc8a11d72021fab878b1be9302f0c46186ca5dc8) add sleep (#2523)
 * [416cc7727](https://github.com/kubeovn/kube-ovn/commit/416cc7727ff85ec01c1a4260d857ced651a4730c) when restart deployment kube-ovn-controller the kubectl ko log loss (#2508)
 * [e7085dec7](https://github.com/kubeovn/kube-ovn/commit/e7085dec79de60b83f25a85a9fa7a385ba192a72) optimize e2e framework (#2492)
 * [4b59bdfc3](https://github.com/kubeovn/kube-ovn/commit/4b59bdfc362e63e0a0ef9ce35819f63b48492ee4) fix ovs patches (#2506)
 * [1138c2cff](https://github.com/kubeovn/kube-ovn/commit/1138c2cff7233be4b39ed3265923cb2236d75021) fix subnet iprange not correct (#2505)
 * [0ebb67853](https://github.com/kubeovn/kube-ovn/commit/0ebb67853164682e5a7fc719c4bab325d4682730) bump k8s to v1.26.3 (#2514)
 * [6fb799232](https://github.com/kubeovn/kube-ovn/commit/6fb7992326a8a844b0081fc2c3f8cfde596b3abd) add kubevirt multus nic lsp before gc process (#2504)
 * [3fc6d8e34](https://github.com/kubeovn/kube-ovn/commit/3fc6d8e3495f9032f119773df02d672eafc3e417) update slack link
 * [46d9edbd2](https://github.com/kubeovn/kube-ovn/commit/46d9edbd28dbfa5f942c420e097f3870ed2305d3) docs: updated CHANGELOG.md (#2515)
 * [36329d54c](https://github.com/kubeovn/kube-ovn/commit/36329d54cb15a40885173526093e2631d29b9633) optimize ovs upgrade script (#2512)
 * [f8aabdf58](https://github.com/kubeovn/kube-ovn/commit/f8aabdf583b80d7bef228265ee3f26a666cb975e) ci: change to pull_request_target
 * [089d8cd20](https://github.com/kubeovn/kube-ovn/commit/089d8cd20b0ac734ddfcb2f60d1666411b9f400d) ci: add openai to review the code (#2511)
 * [ee5e59a94](https://github.com/kubeovn/kube-ovn/commit/ee5e59a9436e85982aeb3fbe14040577a2f39302) add support of user-defined image name for vpc-dns (#2502)
 * [20e702226](https://github.com/kubeovn/kube-ovn/commit/20e702226357cd15474e43ec14c8adee67c34e25) build(deps): bump google.golang.org/protobuf from 1.29.1 to 1.30.0 (#2500)
 * [b6913c522](https://github.com/kubeovn/kube-ovn/commit/b6913c522c32749ef0559a2c61e6ad6840a30d9f) build(deps): bump github.com/Microsoft/hcsshim from 0.9.7 to 0.9.8 (#2499)
 * [443dd58bf](https://github.com/kubeovn/kube-ovn/commit/443dd58bf8fb51005cfacf9ec4f9f78a274cceef) replace lr/ls/lrp/lsp function call with ovnClient (#2477)
 * [599ed2349](https://github.com/kubeovn/kube-ovn/commit/599ed2349ec37b3765e1aa90f49e9f3eacda3847) ci: fix go cache (#2498)
 * [0606f90df](https://github.com/kubeovn/kube-ovn/commit/0606f90df816fac12c81caaa9357a2f2822eceb3) add skip (#2491)
 * [1aff1c4f5](https://github.com/kubeovn/kube-ovn/commit/1aff1c4f5a9ba282e0aae1edef8a85182fab4792) ensure address label is correct before deleting it (#2487)
 * [e9dd28929](https://github.com/kubeovn/kube-ovn/commit/e9dd28929a529d8cb73cea5bb7742b4c56c085e4) fix scheduled submariner e2e (#2469)
 * [c66a93ac7](https://github.com/kubeovn/kube-ovn/commit/c66a93ac71e44a30cf92ddaed7076f540e7f98e2) build(deps): bump actions/setup-go from 3 to 4 (#2490)
 * [a8aede741](https://github.com/kubeovn/kube-ovn/commit/a8aede741c5c08763853a08e87101ab2f1f764a6) build(deps): bump github.com/onsi/gomega from 1.27.3 to 1.27.4 (#2489)
 * [70a220a01](https://github.com/kubeovn/kube-ovn/commit/70a220a013c12f20559898f6f0b352dc56fc8769) add some sleep wait iptables clean (#2488)
 * [0b8e53463](https://github.com/kubeovn/kube-ovn/commit/0b8e534639c7b504e2b2a203de44a930141b8cde) Add kubectl ko log (#2451)
 * [c3620cd09](https://github.com/kubeovn/kube-ovn/commit/c3620cd09bcc43c874506476198b4f0371bc620c) fix: gw configmap may not exist (#2484)
 * [a31235b11](https://github.com/kubeovn/kube-ovn/commit/a31235b114bafa1a08d655d942ac52aea4317d3e) fix ovs qos e2e for versions prior to v1.12 (#2483)
 * [1470c10d3](https://github.com/kubeovn/kube-ovn/commit/1470c10d30aea08f44b16733bdce5b9c27083536) add node to addNodeQueue if required annations are missing (#2481)
 * [47a705577](https://github.com/kubeovn/kube-ovn/commit/47a705577c2cd3c08a5443a1d6f36d78c0f4748f) Add jitter support to netem qos parameters (#2476)
 * [b15fc51bb](https://github.com/kubeovn/kube-ovn/commit/b15fc51bbbdc20dbcfe61b76ce0856efa4f81ea5) build(deps): bump google.golang.org/protobuf from 1.29.0 to 1.29.1 (#2480)
 * [d1cd3dddf](https://github.com/kubeovn/kube-ovn/commit/d1cd3dddfee6ff61e748eefbf987d1334f3364cd) fix ovs-ovn startup/restart (#2467)
 * [b26784f1d](https://github.com/kubeovn/kube-ovn/commit/b26784f1db1b5bdb230148f704b9e4cd78e66d67) fix changging the stopped vm's subnets, the vm cann't start normally (#2463)
 * [7e2e437d5](https://github.com/kubeovn/kube-ovn/commit/7e2e437d57341a80b157eddb0f7b6f1a13a0f11c) build(deps): bump github.com/onsi/gomega from 1.27.2 to 1.27.3 (#2475)
 * [5b07ccbbc](https://github.com/kubeovn/kube-ovn/commit/5b07ccbbc97504d4598ee4a0310b9eda4a32ed5b) when we delete the pod，it's no need to update the sgs assign to pod (#2465)
 * [3fd564b77](https://github.com/kubeovn/kube-ovn/commit/3fd564b77208d9f0cb45a74e00ef0c4c0ab50017) fix libovsdb issues (#2462)
 * [0689a7293](https://github.com/kubeovn/kube-ovn/commit/0689a7293d6c993ef15db83d6c7a89f2bb36ac7f) fix ips CR not found due to etcd error (#2472)
 * [e368a20ed](https://github.com/kubeovn/kube-ovn/commit/e368a20ed1d0fa87a5992216a5228de85070576c) wait for subnet lb (#2471)
 * [0ecd9aff9](https://github.com/kubeovn/kube-ovn/commit/0ecd9aff989696c45b1b9faf57694843090072a0) chore: update base periodically to resolve security issues. (#2470)
 * [5387acf40](https://github.com/kubeovn/kube-ovn/commit/5387acf40925bca7efba3d0f6571b4724bec330d) do not delete external switch if it is created by provider network vlan subnet (#2449)
 * [282706d67](https://github.com/kubeovn/kube-ovn/commit/282706d67fa5d98a468dff48811fb21e939cdda1) add upgrade compatibility (#2468)
 * [482167a92](https://github.com/kubeovn/kube-ovn/commit/482167a921b5670a56493efbfa8f39f708a7dff5) ci: fix ovn-ic installation (#2456)
 * [2bce5080f](https://github.com/kubeovn/kube-ovn/commit/2bce5080f355eaa2dbd5ea1fdc43eaad4c993f2d) Fixed：Prevents grep from prematurely exiting the shell script if it cannot find a pattern (#2466)
 * [4d850e017](https://github.com/kubeovn/kube-ovn/commit/4d850e017c61ef8e515a45df221c026e0047a5f4) add install for webhook (#2460)
 * [f17b4348b](https://github.com/kubeovn/kube-ovn/commit/f17b4348bdbce1890faf2a17b392110fe9d5af7e) e2e add some debug info and sleep (#2439)
 * [8df83cb14](https://github.com/kubeovn/kube-ovn/commit/8df83cb14469203ed1b27b46cefc2edea0fac968) do not set subnet's vlan empty on failure (#2445)
 * [7ae8db6cf](https://github.com/kubeovn/kube-ovn/commit/7ae8db6cf553797067628381050f69aba5b04b5a) wait subnet lb clear in set subnet EnableLb to false e2e (#2450)
 * [674cc2907](https://github.com/kubeovn/kube-ovn/commit/674cc2907dbfeced1c2fdefcd0009c71dd8e8c71) build(deps): bump github.com/emicklei/go-restful/v3 (#2458)
 * [e4c089abe](https://github.com/kubeovn/kube-ovn/commit/e4c089abe1cdcd7ecb63015a5f3340fbafb40da4) ci(Mergify): configuration update (#2457)
 * [0444a2b20](https://github.com/kubeovn/kube-ovn/commit/0444a2b20c1c89df7e4fcb94db1f521e3c395233) kube-ovn-speaker support IPv6/Dual (#2455)
 * [790c7cc29](https://github.com/kubeovn/kube-ovn/commit/790c7cc296aea61d5e6b8a70ad65d628c0455414) replace nb_global function call with ovnClient (#2454)
 * [0d129742f](https://github.com/kubeovn/kube-ovn/commit/0d129742ff3e31f51dfd962e3eae915f1878cd32) build(deps): bump google.golang.org/protobuf from 1.28.1 to 1.29.0 (#2452)
 * [b399cca68](https://github.com/kubeovn/kube-ovn/commit/b399cca68ddab0a2f331b614d14fb8af8ce0b396) fix parsing logical router static routes (#2443)
 * [9df323d7e](https://github.com/kubeovn/kube-ovn/commit/9df323d7ed65a781a7838cc5b2b4c010fb123962) base: fix ovn patches (#2444)
 * [3259b9120](https://github.com/kubeovn/kube-ovn/commit/3259b9120bfa26742245369e1bbad11d472535d7) prepare for libovsdb replacement (#1978)
 * [d71a314d5](https://github.com/kubeovn/kube-ovn/commit/d71a314d5dbd5199d134e4ef42ab6c4cbee312ad) support auto change external bridge (#2437)
 * [f4bef89e2](https://github.com/kubeovn/kube-ovn/commit/f4bef89e20859d4daab6264780c0c0b8186fe1f7) fix ovn-speaker router bug (#2433)
 * [497260efe](https://github.com/kubeovn/kube-ovn/commit/497260efe4abfa2ceef95f9954b26dec57c9f7a3) ovs: change update strategy to RollingUpdate (#2422)
 * [c84479be2](https://github.com/kubeovn/kube-ovn/commit/c84479be2e0ad190fd4cfca4da1fcc25ffc6b914) add kubevirt install (#2430)
 * [e9017e2ab](https://github.com/kubeovn/kube-ovn/commit/e9017e2ab558fb03132ff2a67c0a48b1673fc46e) e2e: wait for subnet to meet specified condition (#2431)
 * [810f7b994](https://github.com/kubeovn/kube-ovn/commit/810f7b994fb937d35534c2452f60e21caa3f4b9c) delete all  invalid ovn lb  strategy and prevent invalid multiple endpoint reconsile (#2419)
 * [25fef7cce](https://github.com/kubeovn/kube-ovn/commit/25fef7cceea1a715369da5e97a61489a4e6099c4) add sumbarier case (#2416)
 * [a99ceb20d](https://github.com/kubeovn/kube-ovn/commit/a99ceb20dd67ed5ea52556f7f3b5adc15b6e8cd7) iptables-rules upgrade compatible (#2429)
 * [570338473](https://github.com/kubeovn/kube-ovn/commit/570338473c3a48e51b0d9d1d2155e7b0724b09bd) add log (#2423)
 * [824f2e0ab](https://github.com/kubeovn/kube-ovn/commit/824f2e0ab9475ee40570659c3717e3026eaf478c) check subnet gateway after wait (#2428)
 * [86c01e6ba](https://github.com/kubeovn/kube-ovn/commit/86c01e6ba6c924393fb551f350733911c6f2de6f) fix chart install/upgrade e2e (#2426)
 * [322eab3b9](https://github.com/kubeovn/kube-ovn/commit/322eab3b927ea2cc47310fe056ef039bd976a866) ci: fix cilium chaining e2e (#2391)
 * [79367647b](https://github.com/kubeovn/kube-ovn/commit/79367647b84607ca531d03084fb19efc4552e614) build(deps): bump golang.org/x/sys from 0.5.0 to 0.6.0 (#2427)
 * [dc5148bb8](https://github.com/kubeovn/kube-ovn/commit/dc5148bb86f66ce9574152f0eacaf3f79714c182) resolve e2e error in v1.12.0 (#2425)
 * [541b641f4](https://github.com/kubeovn/kube-ovn/commit/541b641f431d1c57932c05b36df079e846d7e0a0) update test server and test results (#2421)
 * [980507053](https://github.com/kubeovn/kube-ovn/commit/9805070534fc8496ca296b114f28029263d221e0) Modify the pod scheduling of vpcdns (#2420)
 * [83ab70ffb](https://github.com/kubeovn/kube-ovn/commit/83ab70ffb61241ef7b6467927de56ee96daf1334) e2e: double parallel test nodes in ci (#2411)
 * [fd3bee6ee](https://github.com/kubeovn/kube-ovn/commit/fd3bee6ee22780d1d5739ce2b7ec156daf6c938d) fix scheduled e2e (#2417)
 * [5cd8649bc](https://github.com/kubeovn/kube-ovn/commit/5cd8649bc6eefdccc255955f11a47de75b74d252) build(deps): bump sigs.k8s.io/controller-runtime from 0.14.4 to 0.14.5 (#2415)
 * [68d2ebfa7](https://github.com/kubeovn/kube-ovn/commit/68d2ebfa77f78f662ed29a41b87d21949a87ec04) build(deps): bump github.com/osrg/gobgp/v3 from 3.11.0 to 3.12.0 (#2414)
 * [8f6c21ce1](https://github.com/kubeovn/kube-ovn/commit/8f6c21ce18d06866acd3191238c6314c23842139) build(deps): bump k8s.io/klog/v2 from 2.90.0 to 2.90.1 (#2413)
 * [d837d978e](https://github.com/kubeovn/kube-ovn/commit/d837d978e951aad90d696052e843f811fd8adfe1) bump go modules (#2408)
 * [8fbc5dd17](https://github.com/kubeovn/kube-ovn/commit/8fbc5dd17dee8e08a732e85a9290e38defd58c07) e2e: fix random conflict in parallel processes (#2410)
 * [cedcbbc84](https://github.com/kubeovn/kube-ovn/commit/cedcbbc84efd5e14dacdb5726276b6e99059a51e) fix_base_sg_rule (#2401)
 * [4a28cfb3d](https://github.com/kubeovn/kube-ovn/commit/4a28cfb3df675b82a3dcd0c56d3062d2a7617770) build(deps): bump k8s.io/sample-controller from 0.26.1 to 0.26.2 (#2403)
 * [d30935e0c](https://github.com/kubeovn/kube-ovn/commit/d30935e0c2f2f48222afef44fd2253d2325545af) build(deps): bump github.com/onsi/gomega from 1.27.1 to 1.27.2 (#2396)
 * [645908f66](https://github.com/kubeovn/kube-ovn/commit/645908f66fc276660c455a94d7956487dc15f52a) Support bfd management (#2382)
 * [b1a09bafc](https://github.com/kubeovn/kube-ovn/commit/b1a09bafc7b3df1fa0a3e0c06425f6e0679eacad) remove unused param (#2393)
 * [d24455196](https://github.com/kubeovn/kube-ovn/commit/d244551962ea359dfc1055f2496958ce30519f17) update ipv6 security-group remote group name (#2389)
 * [db435dcc3](https://github.com/kubeovn/kube-ovn/commit/db435dcc320acadd1d578a98876a2bae7451c143) Fix routeregexp ipv6 (#2395)
 * [8a63d2801](https://github.com/kubeovn/kube-ovn/commit/8a63d2801411c82ab6d98c6ff05a7f615c6fb78e) ci: fix ref name check (#2390)
 * [42e6a3022](https://github.com/kubeovn/kube-ovn/commit/42e6a302267a3457fbb037212dae7292d38fbe10) add support of user-defined kubelet directory (#2388)
 * [282644e92](https://github.com/kubeovn/kube-ovn/commit/282644e929dfeb340df6aab3b9277ce6fcd6f8d1) support 1.11 (#2387)
 * [2d1c12529](https://github.com/kubeovn/kube-ovn/commit/2d1c12529ee194102d4e62a8639400299f34a772) ci: skip netpol e2e automatically for push events (#2379)
 * [109704d0a](https://github.com/kubeovn/kube-ovn/commit/109704d0a654749436935f312facebc2db4817f6) ci: make path filter more accurate (#2381)
 * [770224375](https://github.com/kubeovn/kube-ovn/commit/77022437575d112f81002955a681509815dcff4f) build(deps): bump github.com/stretchr/testify from 1.8.1 to 1.8.2 (#2386)
 * [9737c3902](https://github.com/kubeovn/kube-ovn/commit/9737c3902fc6e13893fe35bbd5292502f9f6b423) Fix comment format (#2383)
 * [01e558052](https://github.com/kubeovn/kube-ovn/commit/01e558052ee004e5e854e9b5f2b8c3e51b147bdd) fix: ovs-ovn should reboot now (#2297)
 * [5e0c305f6](https://github.com/kubeovn/kube-ovn/commit/5e0c305f6cd1cdd5db17e14c4a11c2c85db92b90) fix service dual stack add/del cluster ips not change ovn nb (#2367)
 * [ff8361165](https://github.com/kubeovn/kube-ovn/commit/ff83611654b5af8e0a4fd525b82c3f6c512dbaee) ci: fix path filter for windows build (#2378)
 * [4f3f4e741](https://github.com/kubeovn/kube-ovn/commit/4f3f4e741219c4f0969f2ff008e264dd8053562b) e2e: run specs in parallel (#2375)
 * [ffbb15248](https://github.com/kubeovn/kube-ovn/commit/ffbb15248045d512f1947a965cdd1c957c3392f5) add base sg rules for ports (#2365)
 * [db9f9272a](https://github.com/kubeovn/kube-ovn/commit/db9f9272af57f21f62914f7665d6b65bc24a805e) accelerate cleanup (#2376)
 * [50df652c2](https://github.com/kubeovn/kube-ovn/commit/50df652c26c7e95428ff80efb0bc432f963ff86d) update ovnnb model (#2371)
 * [f68044bce](https://github.com/kubeovn/kube-ovn/commit/f68044bce43cca4359e9e3c046c13cf03d854260) docs: updated CHANGELOG.md (#2373)
 * [8a1814a83](https://github.com/kubeovn/kube-ovn/commit/8a1814a83e652272249a0a14d5aa2456d913a02f) fix changelog workflow (#2372)
 * [a1a528b75](https://github.com/kubeovn/kube-ovn/commit/a1a528b75978417d55c1f6431712a6bff9375c23) build(deps): bump github.com/Microsoft/hcsshim from 0.9.6 to 0.9.7 (#2370)
 * [ee53dfe17](https://github.com/kubeovn/kube-ovn/commit/ee53dfe179aeb344b9964485a802c4ee734c22db) Add gateway monitor metrics and event (#2345)
 * [c061ae182](https://github.com/kubeovn/kube-ovn/commit/c061ae182e6cf98783a20876bf1a451fed8ceb35) ci: fix default branch test (#2369)
 * [4a0829a71](https://github.com/kubeovn/kube-ovn/commit/4a0829a71ef442bdf42b9aad5a74a26ce7d30fd5) fix github actions workflows (#2363)
 * [62834eb12](https://github.com/kubeovn/kube-ovn/commit/62834eb1286c213ec0ec10293cadd8617522b58b) Fixed iptables creation failure due to an excessively long label (#2366)
 * [c5d8ebacc](https://github.com/kubeovn/kube-ovn/commit/c5d8ebacc6c4f468609802e6e570cc19744df5ec) use existing node switch cidr instead of the configured one (#2359)
 * [092aa0830](https://github.com/kubeovn/kube-ovn/commit/092aa083074fced40771da6135b10fb1e1090bd0) Do not wait pod deletion one by one to accelerate install (#2360)
 * [1974f8b11](https://github.com/kubeovn/kube-ovn/commit/1974f8b1169e60e5b3cb5b7be789b8ac939eb951) Change log level (#2362)
 * [13f345da4](https://github.com/kubeovn/kube-ovn/commit/13f345da485ee22c2f61be80307189c14a1a68e6) change log level (#2356)
 * [5bd51760d](https://github.com/kubeovn/kube-ovn/commit/5bd51760d19e5cf662d14e88ad14efa0dd04fae3) build(deps): bump github.com/onsi/gomega from 1.27.0 to 1.27.1 (#2357)
 * [3b466d2de](https://github.com/kubeovn/kube-ovn/commit/3b466d2de935101d5df42b564038a9f7c5c03cc7) simplify github actions workflows (#2338)
 * [8fe8bc58c](https://github.com/kubeovn/kube-ovn/commit/8fe8bc58cf008330a2fa9e5bb9e96092d4ee0622) update go version to v1.20 (#2312)
 * [90f504c76](https://github.com/kubeovn/kube-ovn/commit/90f504c760d620b9b23918cb4f80975bda7ce5c1) build(deps): bump golang.org/x/net from 0.6.0 to 0.7.0 (#2353)
 * [a9753c34b](https://github.com/kubeovn/kube-ovn/commit/a9753c34b435ec73f08b0b1f50759b9fe6e91d66) build(deps): bump github.com/onsi/gomega from 1.26.0 to 1.27.0 (#2349)
 * [6e21a93e9](https://github.com/kubeovn/kube-ovn/commit/6e21a93e965b385006388fe918a4fdacef969ee9) chore: no need to wait 30 seconds before kube-ovn-cni get ready. (#2339)
 * [f8b97e72f](https://github.com/kubeovn/kube-ovn/commit/f8b97e72f550fc2af2cb661ae2cd665a108b42bf) do not remove link local route on ovn0 (#2341)
 * [79584c43b](https://github.com/kubeovn/kube-ovn/commit/79584c43b8d10c31550332855272dbfcfa109fcc) fix encap ip when the tunnel interface has multiple addresses (#2340)
 * [156d59767](https://github.com/kubeovn/kube-ovn/commit/156d5976745d95709776be0eb1401e7b8aaf91d9) fix legacy network policy err (#2313)
 * [9c51bd9eb](https://github.com/kubeovn/kube-ovn/commit/9c51bd9ebcb16cb60e6941e93cb2016d6bae9bcb) enqueue endpoint when handling service add event (#2337)
 * [cdf549976](https://github.com/kubeovn/kube-ovn/commit/cdf54997653b20e207dc3c4bad9c9e91e9057fff) Add neighbor-address format check for kube-ovn-speaker (#2335)
 * [b0b46948e](https://github.com/kubeovn/kube-ovn/commit/b0b46948e69f493a34df1806b96197370919b12c) add ovnext0 inside ns on gw node for ecmp static route with bfd (#2237)
 * [4ca994bf8](https://github.com/kubeovn/kube-ovn/commit/4ca994bf85670da0f7fdc839cd3c150fc9ec609b) OVN LB: add support for SCTP protocol (#2331)
 * [ea14e91fa](https://github.com/kubeovn/kube-ovn/commit/ea14e91fa79385f3401c55593735c7cf98914668) fix getting service backends in dual-stack clusters (#2323)
 * [937d3ced4](https://github.com/kubeovn/kube-ovn/commit/937d3ced4a55e4ecc0aa70743abdc1b79ab91692) e2e: skip case of switching session affinity (#2328)
 * [eb2b36a52](https://github.com/kubeovn/kube-ovn/commit/eb2b36a52898bf6b8cbdd7f952be905d38b5ff1b) fix k8s networking dns e2e (#2325)
 * [1c97f58a3](https://github.com/kubeovn/kube-ovn/commit/1c97f58a327ff2e13105ae1e2aba0c913c30e49d) Add the bgp router-id format check (#2316)
 * [f7f2375fa](https://github.com/kubeovn/kube-ovn/commit/f7f2375fa83ac91e6c49d62da686d7e773e82f2a) perform the gateway check but ignore the result when the annotation of subnet is ‘disableGatewayCheck=true’ to make sure of the first network packet (#2290)
 * [0bd7c7e59](https://github.com/kubeovn/kube-ovn/commit/0bd7c7e598761ed06f877cbe5d0cb0a46ad07f60) perf: use empty struct to reduce memory usage (#2327)
 * [b2eaea006](https://github.com/kubeovn/kube-ovn/commit/b2eaea0064abd74e9a997984d743e085f9567868) split netpol cases (#2322)
 * [40b5890ae](https://github.com/kubeovn/kube-ovn/commit/40b5890aefc14376b5e18b471cbeccef94ee88e4) feat: support default service session stickiness timeout (#2311)
 * [83685b5a9](https://github.com/kubeovn/kube-ovn/commit/83685b5a9b4d5b98c69d01848104e5a4f51d4c55) feat: configure routes via pod annotation (#2307)
 * [c8d443ef4](https://github.com/kubeovn/kube-ovn/commit/c8d443ef46044fda8690e833ed508e10a24281b6) build(deps): bump github.com/docker/docker (#2320)
 * [4e2fe3103](https://github.com/kubeovn/kube-ovn/commit/4e2fe3103c709f5f78f669cdc1a151f78dbc21c2) e2e: do not test versions prior to 1.11 for ovn-ic update (#2319)
 * [0d2aa03cd](https://github.com/kubeovn/kube-ovn/commit/0d2aa03cd6ae536e0bf3e1bbe8d5f2ae5a06fe74) ovndb: use Local_Config to configure listen addresses (#2299)
 * [87bacf5ff](https://github.com/kubeovn/kube-ovn/commit/87bacf5ff55ebc3b0fec551ce4a8f957a43c5568) chore: improve the list style in Markdown (#2315)
 * [8c1edc807](https://github.com/kubeovn/kube-ovn/commit/8c1edc807299f5e40f0ccd09b70aecf745045b84) fix egress node and gateway acl should apply after lb. (#2310)
 * [22cc93370](https://github.com/kubeovn/kube-ovn/commit/22cc933708b09c8c5048f649715ea4ea3753e705) fix kube-ovn-controller crash on startup (#2305)
 * [b6eb7ce21](https://github.com/kubeovn/kube-ovn/commit/b6eb7ce219651a6737bf5428d74c0b8f5ecc19ae) build(deps): bump google.golang.org/grpc from 1.52.3 to 1.53.0 (#2308)
 * [5ca2a5c89](https://github.com/kubeovn/kube-ovn/commit/5ca2a5c89c7fa8d60b7abfc790656209421a8555) build(deps): bump golang.org/x/sys from 0.4.0 to 0.5.0 (#2309)
 * [eb31a1783](https://github.com/kubeovn/kube-ovn/commit/eb31a17833c033b30f26b6b78596d5894387239b) ignore e2e for subnet enableEcmp before v1.12.0 (#2306)
 * [f81c43a12](https://github.com/kubeovn/kube-ovn/commit/f81c43a122b9a5328abca11c700c51297896aff5) fix u2o code err (#2300)
 * [993fefaab](https://github.com/kubeovn/kube-ovn/commit/993fefaab49d048c0b110346d1b361abf1e11018) set join subnet.spec.enableLb to nil (#2304)
 * [d1d109727](https://github.com/kubeovn/kube-ovn/commit/d1d109727641cda6c5b2d0bdff91342ecd515aeb) fix image tag in helm chart (#2302)
 * [77cf5e9b2](https://github.com/kubeovn/kube-ovn/commit/77cf5e9b22031d31ab56c8414ea3e2112c3bc77a) update trivy deprecated arg and the ignored CVE. (#2296)
 * [9b85bbac8](https://github.com/kubeovn/kube-ovn/commit/9b85bbac8668b2d1aac16cbcb90e2efe2837811b) move enableEcmp to subnet (#2284)
 * [87eacf593](https://github.com/kubeovn/kube-ovn/commit/87eacf59362bbaf3ef323bd55652a6442c117593) build(deps): bump sigs.k8s.io/controller-runtime from 0.14.3 to 0.14.4 (#2301)
 * [971add05a](https://github.com/kubeovn/kube-ovn/commit/971add05a436600ef48b2d16d1eb18b6527463b9) fix gosec ci installation (#2295)
 * [ac72f7717](https://github.com/kubeovn/kube-ovn/commit/ac72f7717927022eb0fa288423d6394a4bc74163) delete htb qos priority (#2288)
 * [36da29cb9](https://github.com/kubeovn/kube-ovn/commit/36da29cb96cad0eccdbd65608057cf7fb908037b) build(deps): bump sigs.k8s.io/controller-runtime from 0.14.2 to 0.14.3 (#2292)
 * [ea1df9641](https://github.com/kubeovn/kube-ovn/commit/ea1df9641066a2f6102c566d76ae7553af3d8ae7) ovn northd: fix connection inactivity probe (#2286)
 * [54984d67a](https://github.com/kubeovn/kube-ovn/commit/54984d67a20404ec1b8ce14efb6f6d834711c292) fix ct new config error (#2289)
 * [3f0a50088](https://github.com/kubeovn/kube-ovn/commit/3f0a500885ebadd1184924457f82e927effaee6f) fix wrong network interface name in gateway check (#2282)
 * [74a7da88a](https://github.com/kubeovn/kube-ovn/commit/74a7da88aebbad8302e07b650d56df83973db3c1) build(deps): bump github.com/docker/docker (#2287)
 * [20e576991](https://github.com/kubeovn/kube-ovn/commit/20e5769912d0f0a9ee2fd758d5d9b9db6819573f) Improve webhook (#2278)
 * [f0d915136](https://github.com/kubeovn/kube-ovn/commit/f0d915136b96bc14d5c646818da30d88eb44fb41) add named port support (#2273)
 * [9985ee5c6](https://github.com/kubeovn/kube-ovn/commit/9985ee5c6b95bc248fc141ce4e508511d91cd881) fix access from node to overlay pods when network policy ingress exists (#2279)
 * [2b3834009](https://github.com/kubeovn/kube-ovn/commit/2b38340091bbb64c8c622c06bcddcf08a5473053) move enableLb to subnet (#2276)
 * [5712485d7](https://github.com/kubeovn/kube-ovn/commit/5712485d7e94af72c95ec3716d374f4b63a76f83) build(deps): bump github.com/osrg/gobgp/v3 from 3.10.0 to 3.11.0 (#2280)
 * [805f83ea0](https://github.com/kubeovn/kube-ovn/commit/805f83ea026d71fa4104fa21a53132251f0d3a70) add V4/V6UsingIPRange and V4/V6AvailableIPRange in subnet status (#2268)
 * [0c74034d0](https://github.com/kubeovn/kube-ovn/commit/0c74034d056b0f90fc577dc3cb0079f147f0eca4) skip u2o test case before 1.9 (#2274)
 * [eddf18d86](https://github.com/kubeovn/kube-ovn/commit/eddf18d8601a116b15d502f59a06d81fffd6b6e4) fix network break on kube-ovn-cni startup (#2272)
 * [26c506d89](https://github.com/kubeovn/kube-ovn/commit/26c506d89b309dfa6cd7b3e18fb4a96f26bd6df8) bump go modules (#2267)
 * [e10d076ef](https://github.com/kubeovn/kube-ovn/commit/e10d076ef8a004ee4a5af83c3e58df3b084dd5b9) fix setting mtu for ovs internal port (#2247)
 * [155768a3e](https://github.com/kubeovn/kube-ovn/commit/155768a3e4d4215542af4896acbd59f6636cc067) bump ovs/ovn versions (#2254)
 * [281242ef3](https://github.com/kubeovn/kube-ovn/commit/281242ef3deed8d617aa469f6fe84e3ac5de9678) use node ip instead of ovn0 ip when accessing overlay pod/svc from host network (#2243)
 * [ea8523c66](https://github.com/kubeovn/kube-ovn/commit/ea8523c6667bd04cebcb31f3b45970459aa147d8) build(deps): bump google.golang.org/grpc from 1.52.1 to 1.52.3 (#2265)
 * [cb20b12e3](https://github.com/kubeovn/kube-ovn/commit/cb20b12e36d45b36fbdbaf215ce97164a77b5042) build(deps): bump google.golang.org/grpc from 1.52.0 to 1.52.1 (#2264)
 * [69546ffb8](https://github.com/kubeovn/kube-ovn/commit/69546ffb819729f87bce228e9fc9b67114d55324) build(deps): bump k8s.io/klog/v2 from 2.80.1 to 2.90.0 (#2262)
 * [4d9177f7d](https://github.com/kubeovn/kube-ovn/commit/4d9177f7dc5975de9898387f8c280cc901ae52e7) build(deps): bump github.com/onsi/gomega from 1.25.0 to 1.26.0 (#2263)
 * [cc4bfd544](https://github.com/kubeovn/kube-ovn/commit/cc4bfd5445f749da36cb84d97e3202a85b96f87d) build(deps): bump k8s.io/sample-controller from 0.26.0 to 0.26.1 (#2260)
 * [8a6ac128b](https://github.com/kubeovn/kube-ovn/commit/8a6ac128b60609638ce37edf57c5b0f75f2fc9c6) build(deps): bump github.com/docker/docker (#2259)
 * [b33086f74](https://github.com/kubeovn/kube-ovn/commit/b33086f74cda2a3e39968bd613248ca29527ed3a) egress networkpolicy acl add option apply-after-lb (#2251)
 * [625a6854b](https://github.com/kubeovn/kube-ovn/commit/625a6854bc891f101abee55316c7d9ac546991a8) ovn db: add support for listening on pod ip (#2235)
 * [6969dcd80](https://github.com/kubeovn/kube-ovn/commit/6969dcd80d7472a305d489ad7161bc081274038f) update cni plugin to 1.2.0 (#2255)
 * [1f9957099](https://github.com/kubeovn/kube-ovn/commit/1f99570994822c7f4ade37a637fda6a936847629) build(deps): bump github.com/onsi/gomega from 1.24.2 to 1.25.0 (#2257)
 * [486e8ee25](https://github.com/kubeovn/kube-ovn/commit/486e8ee254dd2c33de63da318d044305615c865b) clean up legacy u2o implement (#2248)
 * [5e684e9d8](https://github.com/kubeovn/kube-ovn/commit/5e684e9d82ff759b93f9da848017d73625b2aee6) eip status状态切换缓慢 (#2256)
 * [1049d2459](https://github.com/kubeovn/kube-ovn/commit/1049d2459cf63a0a47573bea28207ce42f8d5eb6) build(deps): bump github.com/containernetworking/plugins (#2253)
 * [9092956fc](https://github.com/kubeovn/kube-ovn/commit/9092956fc0571f5dc9fe3a79b03cc3d4bb56cc71) fix vip create (#2245)
 * [dc731efdc](https://github.com/kubeovn/kube-ovn/commit/dc731efdc2e9f82bc9d870a70c44df851c6f4eec) improve webhook functions for vpc and subnet (#2241)
 * [dfb1cc2b8](https://github.com/kubeovn/kube-ovn/commit/dfb1cc2b8821aa7f2dfb1f71083310e964fd3be9) fix syntax errors (#2240)
 * [e65498020](https://github.com/kubeovn/kube-ovn/commit/e65498020c2c9548f423bc11a01e3a89a65eb0d8) add release-1.11 to scheduled e2e (#2238)
 * [6adf82678](https://github.com/kubeovn/kube-ovn/commit/6adf82678fd97c021d248481d177945bb4230dfe) fix webhook (#2236)
 * [3f5bd39ba](https://github.com/kubeovn/kube-ovn/commit/3f5bd39ba7e97fe8f77107e7fe9284ab705978d3) fix: ovnic del old AZ after establish the new as name (#2229)
 * [b0c17afdc](https://github.com/kubeovn/kube-ovn/commit/b0c17afdce512c2a878d36721a33afb755ebb4e3) prepare for next release
 * [91db26f1a](https://github.com/kubeovn/kube-ovn/commit/91db26f1a108ab3a8fcadb841635f44d89cf3237) build(deps): bump google.golang.org/grpc from 1.51.0 to 1.52.0 (#2234)

### Contributors

 * Alex Jones
 * Daviddcc
 * KillMaster9
 * Longchuanzheng
 * Miika Petäjäniemi
 * Nico Wang
 * Rick
 * bobz965
 * changluyi
 * dependabot[bot]
 * fsl
 * github-actions[bot]
 * gugu
 * hzma
 * jeffy
 * jizhixiang
 * lanyujie
 * liuzhen21
 * lut777
 * mingo
 * qiutingjun
 * shane
 * wangyd1988
 * wujixin
 * xujunjie-cover
 * 夜微澜
 * 张祖建
 * 袁又袁

## v1.11.24 (2025-08-20)

 * [d9154f1e1](https://github.com/kubeovn/kube-ovn/commit/d9154f1e145d71617eb77be2790dd7f9c3a1edf7) release v1.11.24
 * [3056aa7fc](https://github.com/kubeovn/kube-ovn/commit/3056aa7fc2d63ac8c52bd4854dd2596b4b7fc160) fix cve (#5631)
 * [6f029865e](https://github.com/kubeovn/kube-ovn/commit/6f029865ef7371ff819368d150fa9f6a5702b309) prepare for next release

### Contributors

 * Mengxin Liu
 * changluyi

## v1.11.23 (2025-08-20)

 * [513a68d91](https://github.com/kubeovn/kube-ovn/commit/513a68d912553e8306735d82964f0d48e2fb1f40) release v1.11.23
 * [cfe30482e](https://github.com/kubeovn/kube-ovn/commit/cfe30482ee90f08e7d8dd4fff2ad238cd36c9d21) Fix mac conflict 1.11 (#5622)
 * [b1cae7830](https://github.com/kubeovn/kube-ovn/commit/b1cae7830ffd9c93f68be92ee6b9e5c598ffd407) Fix the problem that if available ip is 0 but there is a value in excludeIPs, the fixed ip is used as the ip in excludeIPs but the error noAddressAvaliable is still reported (#5570)
 * [e41cae0a0](https://github.com/kubeovn/kube-ovn/commit/e41cae0a0a5e3863ddd142f0b00813f818a21f88) check underlay nic exist before config external bridge (#5520)
 * [182ce19da](https://github.com/kubeovn/kube-ovn/commit/182ce19da72c46fcf0a842af9a759a5980479e81) Revert "fix dpdk base tag (#5211)"
 * [325a42cdc](https://github.com/kubeovn/kube-ovn/commit/325a42cdcef8ef3dbece6d9448511e607ef8dd4a) fix dpdk base tag (#5211)
 * [6e1782f1e](https://github.com/kubeovn/kube-ovn/commit/6e1782f1ecb69a649b826ae0c3b0c82b7eaf01e1) base: use local patch files (#5208)
 * [460015bae](https://github.com/kubeovn/kube-ovn/commit/460015baef88d6a105869f07bbf8e91d5b3fac2e) fix underlay subnet kubectl ko trace error (#2773)
 * [2539427eb](https://github.com/kubeovn/kube-ovn/commit/2539427eb1d59d4ca9b1e1d85dba90ebc7f938e5) kubectl-ko: fix conntrack state (#5038)
 * [b3fb9a050](https://github.com/kubeovn/kube-ovn/commit/b3fb9a0501f5da0831fd31a18a0c5ca00cc3489f) ci: remove legacy network policy e2e (#5037)
 * [28c8af1c2](https://github.com/kubeovn/kube-ovn/commit/28c8af1c212955aca497ef6d4bbac6ced8d67189) ci: bump aquasecurity/trivy-action to 0.29.0
 * [70c8965f8](https://github.com/kubeovn/kube-ovn/commit/70c8965f8d539ef5932ecbeab3b8046b5a7b62d6) prepare for next release

### Contributors

 * Mengxin Liu
 * changluyi
 * zhangzujian
 * 张祖建

## v1.11.22 (2025-02-19)

 * [f8079e29e](https://github.com/kubeovn/kube-ovn/commit/f8079e29eaac69de8fb04a894d5cb0497f071aa4) release v1.11.22
 * [b4e666add](https://github.com/kubeovn/kube-ovn/commit/b4e666add8ba9fa85e2d453040f31433177f0673) fix cve (#5006)
 * [c0c9fe58e](https://github.com/kubeovn/kube-ovn/commit/c0c9fe58e6757febbe904441c63197e4fb7f9e94) bump go to 1.22.12 (#5008)
 * [7add545c8](https://github.com/kubeovn/kube-ovn/commit/7add545c8beec55c4e49b137f92a72666a410df8) fix cilium chaining e2e test failure
 * [974a23ec5](https://github.com/kubeovn/kube-ovn/commit/974a23ec51b97adfadd4e10b925148a0c7da98f3) ignore trivy (#5004)
 * [b455c7797](https://github.com/kubeovn/kube-ovn/commit/b455c7797662ae929eb1a03a3a6921a900fe226d) fix: kube-ovn-cni always dump-flows br-provider per period (#4971)
 * [3fc588c93](https://github.com/kubeovn/kube-ovn/commit/3fc588c93ceb03f92e44d5876751dc72ec5b364b) controller: consider StatefulSet's start ordinal (#4967)
 * [60f3dcbec](https://github.com/kubeovn/kube-ovn/commit/60f3dcbec88fbecc5c5e55446e0c98f4de1bc4c5) ci: build arm64 base images on arm64 hosted runners (#4936)
 * [a8fde894a](https://github.com/kubeovn/kube-ovn/commit/a8fde894aeaee5d106dd61ef709d24f9ae595086) ci: build arm64 images on arm64 hosted runners (#4936)
 * [6b8e67c09](https://github.com/kubeovn/kube-ovn/commit/6b8e67c093f9e65c79179d8e0ab60e7f46ba35d0) controller: check condition NodeNetworkUnavailable when determining whether node is ready (#4917)
 * [cc9f56645](https://github.com/kubeovn/kube-ovn/commit/cc9f56645c791defcdfadb5bf04c4a2ba6cc23ad) cni-server: set node NetworkUnavailable condition after join subnet gateway check (#4915)
 * [4cf6035b7](https://github.com/kubeovn/kube-ovn/commit/4cf6035b71331c9fb5f1f8d18e0b54c31d794a66) ipam: check subnet's available ipv6 address count (#4903)
 * [8a21336d4](https://github.com/kubeovn/kube-ovn/commit/8a21336d4221909fd556ce87bce3c4e270132f2b) bump go to 1.22.10 (#4854)
 * [be1f189db](https://github.com/kubeovn/kube-ovn/commit/be1f189db5ead8809b64b87ca06a70e4c79c1258) add release scripts
 * [a604b5931](https://github.com/kubeovn/kube-ovn/commit/a604b5931f889bee269bace7b774ffc2be2af604) prepare for next release

### Contributors

 * changluyi
 * zhangzujian
 * 张祖建

## v1.11.21 (2024-10-18)

 * [e6a8501d5](https://github.com/kubeovn/kube-ovn/commit/e6a8501d5c12f5c6a101bc7f223af5bb52c68b54) release v1.11.21
 * [633fbbeb0](https://github.com/kubeovn/kube-ovn/commit/633fbbeb0772cf62aaf8cc6c0b84092ef60e50cb) team device not set unmanage
 * [dca168d80](https://github.com/kubeovn/kube-ovn/commit/dca168d802e0fb197312f1e6bfba86c50c069b2d) prepare for next release

### Contributors

 * clyi

## v1.11.20 (2024-10-16)

 * [0a1194ad4](https://github.com/kubeovn/kube-ovn/commit/0a1194ad43d34e96f35ca874fcc01a6a3b68803f) release v1.11.20
 * [4e7fb345e](https://github.com/kubeovn/kube-ovn/commit/4e7fb345ef3d59004a50b2292c3c1428a0bfa7e9) fix memory overflow, add mac_binding related options to router (#4610)
 * [2c84c60f0](https://github.com/kubeovn/kube-ovn/commit/2c84c60f09186ca126e3084224493713163f461a) bump go to 1.22.8 (#4584)
 * [1cbbf8b87](https://github.com/kubeovn/kube-ovn/commit/1cbbf8b8772f3b1d527a8b84561d58d10fae2444) ci: set trivy db repository to public.ecr.aws/aquasecurity/trivy-db:2 (#4570)
 * [c5a08fa9c](https://github.com/kubeovn/kube-ovn/commit/c5a08fa9cfc4fc3eda7238140febdd121efa9b43) base: rebuild go binary deps from source (#4524)
 * [778578460](https://github.com/kubeovn/kube-ovn/commit/778578460499a2bb63106cc7984c58ece9eaedb9) bump kubectl to v1.30.5
 * [9ce2a3e35](https://github.com/kubeovn/kube-ovn/commit/9ce2a3e3574db880d5a1b34f5934e211cdf80ab7) bump go to 1.22.7 (#4482)
 * [8ae11f5a0](https://github.com/kubeovn/kube-ovn/commit/8ae11f5a0836cdfb9aac96120bfda8e0518fba85) netpol: add allow acl rules for u2o logical gateway (#4420)
 * [27d4fc3fd](https://github.com/kubeovn/kube-ovn/commit/27d4fc3fda750f93f8d91b17c77f03917d2f6f16) Makefile: simplify underlay u2o installation (#4419)
 * [305b2e88c](https://github.com/kubeovn/kube-ovn/commit/305b2e88c26c62a6eb55ae1e3db3c26047ec693f) bump go to 1.22.6
 * [a4041c31a](https://github.com/kubeovn/kube-ovn/commit/a4041c31a5da5e20a26211ebf50c172d9b6ac4db) replace protocol check in netpol update (#4358)
 * [9e5b64dac](https://github.com/kubeovn/kube-ovn/commit/9e5b64dac42dccdf915babf70f8f8c875955c4ad) fix: empty /var/run/netns (#4360)
 * [9056aa03a](https://github.com/kubeovn/kube-ovn/commit/9056aa03a428a0b4ac53fca3718d6004018c1db5) cni-server: disable udp-fragmentation-offload (#4342)
 * [a012bb249](https://github.com/kubeovn/kube-ovn/commit/a012bb2490b8e1ac7f0f4fc59a633eb6725b3ccb) bump k8s to v1.27.16 (#4307)
 * [4aa87ead4](https://github.com/kubeovn/kube-ovn/commit/4aa87ead49afcfd3ca416130efac13a13ed8e8a1) underlay: set trunks of host nic port (#4282)
 * [677aaf6f7](https://github.com/kubeovn/kube-ovn/commit/677aaf6f7dc8c26ed443dc9841f315adb9a8d0ff) fix ovn lb not updated due to service update failure (#4280)
 * [3d4353397](https://github.com/kubeovn/kube-ovn/commit/3d43533970096aaaeb28af41ec5e49c5286811ec) build(deps): bump aquasecurity/trivy-action from 0.23.0 to 0.24.0 (#4275)
 * [99d884724](https://github.com/kubeovn/kube-ovn/commit/99d884724721666fcde3f3c275504770455c922e) bump k8s to v1.27.15
 * [b3a52a0bf](https://github.com/kubeovn/kube-ovn/commit/b3a52a0bfb0cc6045a35ed70a86885d97c64f71a) ci: run go mod tidy before building kubectl (#4274)
 * [271a79efa](https://github.com/kubeovn/kube-ovn/commit/271a79efaf0eedf4ac52095a6b58dffe0e46c88a) ci: disable cgo when building kubelet and cni plugins (#4268)
 * [57c458935](https://github.com/kubeovn/kube-ovn/commit/57c4589350ccfd9de17e4f0e82ca3e71c857f199) build kubectl and cni plugins from source if vuln found in the base image (#4253)
 * [49d053d6f](https://github.com/kubeovn/kube-ovn/commit/49d053d6fda7642587f176abaae8f60306e89685) klog: set log file max size to 200MB (#4272)
 * [aea0ad24f](https://github.com/kubeovn/kube-ovn/commit/aea0ad24f37b82ee553854059c3a21c61107f131) logrotate: set file size limit to 100M (#4271)
 * [bfe6fb841](https://github.com/kubeovn/kube-ovn/commit/bfe6fb8415d839457300f729cd9955bd6ccbcdc6) remove unused environment variable LOG_ROTATE (#4270)
 * [2d0591c00](https://github.com/kubeovn/kube-ovn/commit/2d0591c0099fb9587b975c057741993f11550e12) fix invalid subnet not sync route (#4263)
 * [5da304bac](https://github.com/kubeovn/kube-ovn/commit/5da304bac006c97e74afa58e560c07b1d860fdea) fix missing env variable in lb svc e2e
 * [8af31b4f7](https://github.com/kubeovn/kube-ovn/commit/8af31b4f7e9c16c79e0ea24cf59582b1163393d5) lb svc: update svc status after configuring nat rules (#4235)
 * [e0926b018](https://github.com/kubeovn/kube-ovn/commit/e0926b018e09728575c6b6c9097645b9fe5a5168) vpc-nat-gateway: print messgae to stderr (#4237)
 * [d9f5f668c](https://github.com/kubeovn/kube-ovn/commit/d9f5f668c28eba53db64a2ca516c71ee9b54b564) ci: do not compile fastpath kernel module for centos (#4247)
 * [c327632e0](https://github.com/kubeovn/kube-ovn/commit/c327632e02a9acb724e6a86265b9a16d4d02efc7) prepare for next release

### Contributors

 * bobz965
 * changluyi
 * dependabot[bot]
 * hzma
 * zhangzujian
 * 张祖建

## v1.11.19 (2024-06-28)

 * [a53ad6633](https://github.com/kubeovn/kube-ovn/commit/a53ad663383bfff1a48f115cc11ea19a1b92014d) prepare for next release
 * [6001c921e](https://github.com/kubeovn/kube-ovn/commit/6001c921eaecb56555099913d033239d2d133357) fix ipv6 service ip not added to ovn lb vips due to pod cache not synced (#4223)
 * [8067217b3](https://github.com/kubeovn/kube-ovn/commit/8067217b38a05978b5bcc603292bc4fec2e50d85) support to set nic bandwidth and mirror when pod is annotated with DefaultNetworkAnnotation (#4208)
 * [054263b44](https://github.com/kubeovn/kube-ovn/commit/054263b44ba17b3af93652bfab9edb8f1067fdf3) fix getting service cluster ips (#4206)
 * [34dea54ab](https://github.com/kubeovn/kube-ovn/commit/34dea54abe2f6e7805919b111f0320ade0dd9987) base: add traceroute
 * [2d3cee6cc](https://github.com/kubeovn/kube-ovn/commit/2d3cee6cce7b2932cb55409b874eece4f35f9f41) pinger: reset interface_rx_multicast_packets (#4198)
 * [60b298496](https://github.com/kubeovn/kube-ovn/commit/60b298496b6586324a3fac35459ead566c565063) base: bump kubectl to v1.30.2 (#4163)
 * [a984e4554](https://github.com/kubeovn/kube-ovn/commit/a984e45549e4683f37be1727f9dbe392ba528395) base: bump cni plugins to v1.5.1 (#4185)
 * [15952c63c](https://github.com/kubeovn/kube-ovn/commit/15952c63c763a05473472a093df4fcc718c4d547) fix reconcile routes (#4168)
 * [d3a8296cd](https://github.com/kubeovn/kube-ovn/commit/d3a8296cd371912a3b73f8ff34334664fd36eba4) ci: bump actions
 * [2dcd74596](https://github.com/kubeovn/kube-ovn/commit/2dcd74596d1d29469280e8876ba47fca93e1c2d2) replace util.DefaultVpc with c.config.ClusterRouter (#2782)
 * [76c827b27](https://github.com/kubeovn/kube-ovn/commit/76c827b27bdb48c6ed403444e74afd4e80ebab6f) prepare for next release

### Contributors

 * yunsilicon
 * zhangzujian
 * 张祖建

## v1.11.18 (2024-06-14)

 * [6ee9cb668](https://github.com/kubeovn/kube-ovn/commit/6ee9cb668d7fdf1e9f51e609c244545cbbe00899) set release for 1.11.18
 * [9f8d19ed0](https://github.com/kubeovn/kube-ovn/commit/9f8d19ed0f6787147378882c79ac9c2a6020f15d) ci: fix retrieving docker network subnet/gateway (#4161)
 * [312cca60f](https://github.com/kubeovn/kube-ovn/commit/312cca60fa52e9ab6023dda7a4440359fca0a4ca) Drop u2o arp request 1.11 (#4151)
 * [00f646ac9](https://github.com/kubeovn/kube-ovn/commit/00f646ac98661d8c19708e338ae6bfd0d6b5b985) add ovn0 default route (#4127)
 * [a30e50463](https://github.com/kubeovn/kube-ovn/commit/a30e504633ebc6375561489df800a15c10aaea0c) trivy: ignore unfixed CVEs (#4129)
 * [094f311d1](https://github.com/kubeovn/kube-ovn/commit/094f311d147f1a471b23aa96f672ca9b215378b1) bump go to 1.22.4 (#4121)
 * [48e97f42c](https://github.com/kubeovn/kube-ovn/commit/48e97f42cd0914b0127e4c3233bbb9b4c12462fd) Makefile: run kubectl-ko script when collecting logs (#4100)
 * [f3411a426](https://github.com/kubeovn/kube-ovn/commit/f3411a42636efb9d5a4a84d98e55a399e6539a4d) fix: ip can not change spec (#4091)
 * [2e59f0f72](https://github.com/kubeovn/kube-ovn/commit/2e59f0f7211be808ab7cc07b6ee87ba81e6f1a57) remove unused func and parameter (#4090)
 * [22d6961d1](https://github.com/kubeovn/kube-ovn/commit/22d6961d12b579482e175b7e1045c58cc3388166) fix windows build
 * [d2fb5bfd8](https://github.com/kubeovn/kube-ovn/commit/d2fb5bfd8118d57a8bc26b6071e3bc59c3f1e064) Add support for Yunsilicon NIC (#4074)
 * [3bf87d746](https://github.com/kubeovn/kube-ovn/commit/3bf87d74644700f32b89cc8859f1aa40303b92e6) prepare for next release

### Contributors

 * bobz965
 * changluyi
 * hzma
 * yunsilicon
 * zhangzujian
 * 张祖建

## v1.11.17 (2024-05-23)

 * [c8e258357](https://github.com/kubeovn/kube-ovn/commit/c8e258357afa1ba523d7c45f8cc642c28c6bbf34) set release for 1.11.17
 * [ad3ddaf41](https://github.com/kubeovn/kube-ovn/commit/ad3ddaf4175ace47acd5c8e74dca58c2f7f0a796) fix node gc (#4040)
 * [7bec212be](https://github.com/kubeovn/kube-ovn/commit/7bec212be89d7a8fe0ba4ed978ca27e6bc51e12b) fix: should update subnet status after change vm subnet (#4061)
 * [e252ca7f1](https://github.com/kubeovn/kube-ovn/commit/e252ca7f193e46baaf59c80ec8ffd51280f8346e) ci: build e2e binaries and free disk space on necessary (#4059)
 * [497bc6648](https://github.com/kubeovn/kube-ovn/commit/497bc66487e43fc4d86df1bfe699ec645e5dec3f) crd: add subnet name pattern (#4054)
 * [3d0f073f7](https://github.com/kubeovn/kube-ovn/commit/3d0f073f78e3c8b43084e3b3054116851f665b54) remove Makefile.e2e
 * [5448eb885](https://github.com/kubeovn/kube-ovn/commit/5448eb885b607aeb808bca42ebe939abe3c8666f) bump k8s to 1.27.14 (#4029)
 * [e73086549](https://github.com/kubeovn/kube-ovn/commit/e730865495dce2c66c0fed3899c42792cefd5745) fix node annotations not updated when initializing the default provider-network (#4030)
 * [64accc673](https://github.com/kubeovn/kube-ovn/commit/64accc673677e2d9c8cc0c7238bddbde8ce3f191) bump gosec to 2.19.0
 * [ba3536669](https://github.com/kubeovn/kube-ovn/commit/ba35366696be2bcb2fb2aaf182428e7e85ee72fc) fix: close file (#4007)
 * [818702117](https://github.com/kubeovn/kube-ovn/commit/8187021175e1be2f9b180a9070654ebbee9a76b1) build(deps): bump actions/cache from 3 to 4
 * [04968067a](https://github.com/kubeovn/kube-ovn/commit/04968067ae7cb514f44d745fa0bcdaa2f799cdcd) bump go to 1.22.3 (#3989)
 * [43e664d28](https://github.com/kubeovn/kube-ovn/commit/43e664d283b1b934eef8acf43f578000c3307cd8) bump k8s to v1.27.13 (#3966)
 * [621026cbd](https://github.com/kubeovn/kube-ovn/commit/621026cbdf65ada02f8612e05741e37fe097edba) fix index out of range (#3958)
 * [bfc03b936](https://github.com/kubeovn/kube-ovn/commit/bfc03b936d82c2cde4f57921a9310993983c0202) fix nil pointer dereference (#3951)
 * [14553780a](https://github.com/kubeovn/kube-ovn/commit/14553780a0b37fc67a71d6ddc7b781429a427230) cni-server: set sysctl variables only when the env variables are passed in (#3929)
 * [cdd65b54c](https://github.com/kubeovn/kube-ovn/commit/cdd65b54c015d40a3277c4973d1ade840a08002a) ovn: check whether db file is fixed (#3928)
 * [da69a41aa](https://github.com/kubeovn/kube-ovn/commit/da69a41aa724bd3bbf9087bcbd91f653adf2f3b8) 1.11 distinguish portSecurity with security group (#3863)
 * [9c45f462c](https://github.com/kubeovn/kube-ovn/commit/9c45f462c436a76dcbecc02ce5433695f57dc9be) add tracepath (#3884)
 * [fe2797cd0](https://github.com/kubeovn/kube-ovn/commit/fe2797cd064e26be91adf9d643499b175521daad) ovn: update patch for skipping ct (#3879)
 * [8885cd233](https://github.com/kubeovn/kube-ovn/commit/8885cd2330d1d0437c2df47b0b9fd75d1042b02b) prepare for next release

### Contributors

 * bobz965
 * guangwu
 * zhangzujian
 * 张祖建

## v1.11.16 (2024-03-27)

 * [6b9e393cf](https://github.com/kubeovn/kube-ovn/commit/6b9e393cf1f23a2cc9761e1bde781cb20d8d1f2b) set release 1.11.16
 * [f28babdae](https://github.com/kubeovn/kube-ovn/commit/f28babdae3a09e638b30bd0c0816daad847ef92e) fix cves
 * [bc4cf1aea](https://github.com/kubeovn/kube-ovn/commit/bc4cf1aea61911dbf8370d248f3d229443de9e4d) ci: fix memory leak reporting caused by ovn-controller crashes (#3873)
 * [e7fc6eac6](https://github.com/kubeovn/kube-ovn/commit/e7fc6eac6f682776e88052c9e079e49e4b3dc8f9) Fix the failure to enable multi-network card traffic mirroring for newly created pods (#3805)
 * [680ca67c6](https://github.com/kubeovn/kube-ovn/commit/680ca67c6da8e035a69795da439e0fe37af9113e) fix incorrect variable assignment (#3787)
 * [576866b99](https://github.com/kubeovn/kube-ovn/commit/576866b99036c224969e41fd6bff0f1e601216b9) if startOVNIC firstly, and setazName secondly, the ovn-ic-db may sync the old azname (#3762)
 * [bf36c9117](https://github.com/kubeovn/kube-ovn/commit/bf36c9117f3a6a9f69febbad9d06aaa0c16188d9) ci: cleanup disk space
 * [fbbe6e732](https://github.com/kubeovn/kube-ovn/commit/fbbe6e732678879cd0ea23045784b1aff782cca9) Log near err (#3739)
 * [08fa8214f](https://github.com/kubeovn/kube-ovn/commit/08fa8214fd24477f42f27b60ea7eba68a29b1c59) ip trigger subnet delete (#3703)
 * [65bb2b75e](https://github.com/kubeovn/kube-ovn/commit/65bb2b75ee375ba2af31183c89b921cdb405ec04) fix some ip can not allocate after released (#3699)
 * [1794ab877](https://github.com/kubeovn/kube-ovn/commit/1794ab8779951a80c6137b7029fd88625389c92b) Compatible with controller deployment methods before kube-ovn 1.11.16
 * [d5d4caa47](https://github.com/kubeovn/kube-ovn/commit/d5d4caa475b4159548f02e46e2a74566938ec8f6) ovn: do not send direct traffic between lports to conntrack (#3663)
 * [6f29efd96](https://github.com/kubeovn/kube-ovn/commit/6f29efd965425c052cec631af968f2467ad467ff) sync master change to 1.11 (#3674)
 * [0e98a62db](https://github.com/kubeovn/kube-ovn/commit/0e98a62db22ec09064d5d0b3f8513d86d8e534d7) delete cm ovn-ic-config cause crash 1.11 (#3665)
 * [c0fa3db18](https://github.com/kubeovn/kube-ovn/commit/c0fa3db18056c50a2cd6c64d7d7c8f290aa4b9ca) prepare for next release

### Contributors

 * Changlu Yi
 * bobz965
 * changluyi
 * xieyanker
 * zhangzujian
 * 张祖建

## v1.11.15 (2024-01-22)

 * [94bdf05d4](https://github.com/kubeovn/kube-ovn/commit/94bdf05d4555764b2d1e1f697eb47feb14cf7d08) set for release 1.11.15
 * [b783389ea](https://github.com/kubeovn/kube-ovn/commit/b783389ea7d651b164e7aea3d2249ee9736983bf) refactor start-ic-db.sh (#3645)
 * [84c052017](https://github.com/kubeovn/kube-ovn/commit/84c052017be51f7e63f34ebe7ef9dfda152ee9ff) kube-ovn-monitor and kube-ovn-pinger export pprof path (#3656)
 * [edf23a757](https://github.com/kubeovn/kube-ovn/commit/edf23a7574e510deb18ee76bb976fcfd2b57a0c5) SYSCTL_IPV4_IP_NO_PMTU_DISC set default to 0
 * [7b863fbf3](https://github.com/kubeovn/kube-ovn/commit/7b863fbf30b8816b53623a21bf8880b57485f67c) Ovn ic ecmp enhance 1.11 (#3609)
 * [7a5c4aa6d](https://github.com/kubeovn/kube-ovn/commit/7a5c4aa6d566bbe239d520ea0467e6af4fbab621) fix: subnet can not delete even if no ip in using (#3621)
 * [3fec12e5f](https://github.com/kubeovn/kube-ovn/commit/3fec12e5f454b242acc7872604e619bb09e8d4ca) fix: static spectify one ip (#3614)
 * [6e80f351c](https://github.com/kubeovn/kube-ovn/commit/6e80f351c0940dac50068af4cd5f7487e6bec629) do not calculate subnet.spec.excludeIPs as availableIPs (#3612)
 * [d4967031d](https://github.com/kubeovn/kube-ovn/commit/d4967031d1a4e55c690a8421fed37bc3110436d1) update policy route when subnet cidr changed (#3611)
 * [6fad3a17d](https://github.com/kubeovn/kube-ovn/commit/6fad3a17db27d2569b6551fe25450e84c502e24c) prepare for next release

### Contributors

 * Changlu Yi
 * bobz965
 * changluyi
 * hzma

## v1.11.14 (2024-01-08)

 * [576301137](https://github.com/kubeovn/kube-ovn/commit/576301137576bc96710a451ca1d584038dc4dc7e) set release 1.11.14
 * [a8f93e76c](https://github.com/kubeovn/kube-ovn/commit/a8f93e76c4e20589542dbb3cac31ba8a91942124) ovs: increase cpu limit to 2 cores (#3530)
 * [9d4ea55c2](https://github.com/kubeovn/kube-ovn/commit/9d4ea55c2b6fa6ec615619b529f184494b65ce04) fix u2o infinity recycle (#3567)
 * [ad3e36926](https://github.com/kubeovn/kube-ovn/commit/ad3e3692697a593381c933dbdd3d8f762954d707) 1.11 ip delete lsp (#3562)
 * [e27aced6f](https://github.com/kubeovn/kube-ovn/commit/e27aced6f282ff5b897a9159b9b5385a0ec9b05d) fix ipam deletion (#3549)
 * [40db90115](https://github.com/kubeovn/kube-ovn/commit/40db90115072fce69373220f534d0ff89976b1a1) Revert "ovn-central: check raft inconsistency from nb/sb logs (#3532)"
 * [d30a4cf7b](https://github.com/kubeovn/kube-ovn/commit/d30a4cf7b16be4875af390c5509d0214c56c3a54) fix IP residue after changing subnet of vm in some scenarios (#3370)
 * [407a26f21](https://github.com/kubeovn/kube-ovn/commit/407a26f2148bc1d31ca391eca1dc85e166127cd8) ovn-central: check raft inconsistency from nb/sb logs (#3532)
 * [ca9e530f7](https://github.com/kubeovn/kube-ovn/commit/ca9e530f7b507f138e7c6adc754df9577b9328c8) prepare for next release

### Contributors

 * bobz965
 * changluyi
 * zhangzujian
 * 张祖建
 * 袁又袁

## v1.11.13 (2023-12-14)

 * [dc24eee77](https://github.com/kubeovn/kube-ovn/commit/dc24eee7714baa541a4a91ab5ede9d3289908bbd) set release v1.11.13
 * [226180e9e](https://github.com/kubeovn/kube-ovn/commit/226180e9ea6edbe88ca554aae410a6fd63f3868f) cni-server: set sysctl variable net.ipv4.ip_no_pmtu_disc to 1 by default (#3504)
 * [57c6a8228](https://github.com/kubeovn/kube-ovn/commit/57c6a822883522c37ce41d65fb6fa4906186221b) typo (#3495)
 * [aed1be8ea](https://github.com/kubeovn/kube-ovn/commit/aed1be8ea8122cadfac69a2a85843bbe5c4a7504) add iptables drop invalid rst (#3491)
 * [97bfd4c29](https://github.com/kubeovn/kube-ovn/commit/97bfd4c29d65c548d818b760f8c8187d68363b85) delete vm's lsp and release ipam.ip (#3476)
 * [a5063c807](https://github.com/kubeovn/kube-ovn/commit/a5063c80775c8d106c5c61ed8db0f55883fddd9e) fix: ipam clean all pod nic ip address and mac even if just delete a nic (#3451)
 * [1dd86ea2a](https://github.com/kubeovn/kube-ovn/commit/1dd86ea2af58edb6adb32dded1f1051216e234cd) ovs-healthcheck: ignore error when log file does not exist (#3456)
 * [a92c08a93](https://github.com/kubeovn/kube-ovn/commit/a92c08a93ff64f50949764ecacd6958641a219cb) prepare for the next release

### Contributors

 * bobz965
 * changluyi
 * zhangzujian
 * 张祖建
 * 袁又袁

## v1.11.12 (2023-11-17)

 * [7d83392fc](https://github.com/kubeovn/kube-ovn/commit/7d83392fc6488cfd5bd2a9f2d96b52472014f6ca) set allow-related for gatewayACL and NodePgACL (#3433)
 * [5e9c9f9ce](https://github.com/kubeovn/kube-ovn/commit/5e9c9f9ce7347f854c843f3591a14ed9d48a3718) bump k8s to v1.26.11 (#3427)
 * [ec584f442](https://github.com/kubeovn/kube-ovn/commit/ec584f442266afa9f1de9dbcea92fd5ce22dcbe2) base: fix ovn build failure (#3340)
 * [aac2ae266](https://github.com/kubeovn/kube-ovn/commit/aac2ae266cf85fe0400a8bde1bd8d2185b1a3e9c) optimize ecmp policy route (#3421)
 * [81e0ec304](https://github.com/kubeovn/kube-ovn/commit/81e0ec304ab81613e8bcdb9f60dca8a9989152c1) trivy: ignore CVE-2023-5528
 * [96b62d734](https://github.com/kubeovn/kube-ovn/commit/96b62d734bfa82c8e818c8f1a2f8a96de2dabc89) mtu merge failed (#3417)
 * [c1331e743](https://github.com/kubeovn/kube-ovn/commit/c1331e7438e53741d37d18d8ad4075392142834d) fix helm (#3412)
 * [1c946022d](https://github.com/kubeovn/kube-ovn/commit/1c946022d7effd9facfbde7dce1e0c4c16055af1) Supporting user-defined kubelet directory (#2893)
 * [67adfd998](https://github.com/kubeovn/kube-ovn/commit/67adfd998458662ea6e23370201867a0bb9f08b1) add mtu config to release-1.11
 * [a8960432a](https://github.com/kubeovn/kube-ovn/commit/a8960432a62152edba62ea27ab48592a4ffef684) fix Vulnerability (#3391)
 * [0c74c4e36](https://github.com/kubeovn/kube-ovn/commit/0c74c4e366919e409f8f35e582d0d6851d9e9082) add kube-ovn-controller nodeAffinity prefer not on ic gateway
 * [070452b0b](https://github.com/kubeovn/kube-ovn/commit/070452b0b1226eaa4b55079f7f433f4dc4fcb5a2) Revert "upgrade ovs-ovn pod by generation version instead of chart version (#1960)" (#3387)
 * [6e0711f3b](https://github.com/kubeovn/kube-ovn/commit/6e0711f3b0a67a9a130fb126dc720996e75b2ff0) delete check for existing ip cr (#3361)
 * [dabe5993f](https://github.com/kubeovn/kube-ovn/commit/dabe5993f4acef4da7abb0353c9a5bb0b8f3e832) kube-ovn-controller: fix memory growth caused by unused workqueue
 * [3adf7195e](https://github.com/kubeovn/kube-ovn/commit/3adf7195e771273c8e6144771ce6cbb428979bd2) add compact switch release-1.11 (#3338)
 * [49061ce14](https://github.com/kubeovn/kube-ovn/commit/49061ce1466c124f9d5d311b050bad2cba897cf1) add switch for compact (#3336)
 * [1fe0a9998](https://github.com/kubeovn/kube-ovn/commit/1fe0a9998b1120546ff918d944e58906bb4cb912) base: fix ovn build failure (#3327)
 * [614bdc02d](https://github.com/kubeovn/kube-ovn/commit/614bdc02df261d6f10d916e0189eb6a3e96f0f2e) kube-ovn-controller: fix ovn ic log directory not mounted to hostpath (#3322)
 * [a0e68a40c](https://github.com/kubeovn/kube-ovn/commit/a0e68a40c4ab7f2f1ab772eb03b5fd11bee5e965) fix golang lint error (#3323)
 * [df53f9f2c](https://github.com/kubeovn/kube-ovn/commit/df53f9f2c35f957792e08190f33a0f7865921ec6) add type assertion for ip crd (#3311)
 * [126f87154](https://github.com/kubeovn/kube-ovn/commit/126f871548e74cb83f1f03c5c7308f79df4d8b27) update go net version to v0.17.0 (#3312)
 * [4115d87d8](https://github.com/kubeovn/kube-ovn/commit/4115d87d8e9b0dc27c90c3c362afa3dd14023213) security: ignore kubectl cve (#3305)
 * [682f191d1](https://github.com/kubeovn/kube-ovn/commit/682f191d18a8652c793fcd608d307f44db5cbd23) Revert "update base image to ubuntu:23.10 (#3289)"
 * [995b3fc43](https://github.com/kubeovn/kube-ovn/commit/995b3fc43a030362eab6941d7637fed9544fe401) update base image to ubuntu:23.10 (#3289)
 * [a932658fc](https://github.com/kubeovn/kube-ovn/commit/a932658fc981c4e862196b0ed1bf4c788147de94) ovs: load kernel module ip_tables only when it exists (#3281)
 * [3858fbefb](https://github.com/kubeovn/kube-ovn/commit/3858fbefb4ccbf4066794bced94896e7ed5cc370) pinger: increase packet send interval (#3259)

### Contributors

 * Changlu Yi
 * bobz965
 * changluyi
 * hzma
 * zhangzujian
 * 张祖建
 * 袁又袁
 * 马洪贞

## v1.11.11 (2023-09-25)

 * [a98b4b107](https://github.com/kubeovn/kube-ovn/commit/a98b4b1072f6d6a0bec1777ea0e9be1047e7988d) set for release 1.11.11
 * [67010837c](https://github.com/kubeovn/kube-ovn/commit/67010837c60d5905fc65b34da7f4db2ad889bf36) fix: for existing nic, no need to set the port type to internal (#3243)
 * [b183e577e](https://github.com/kubeovn/kube-ovn/commit/b183e577e874e27234b16dfc312c20659d50aeaa) undo delete perl cmd to update release-1.11 image
 * [634a99514](https://github.com/kubeovn/kube-ovn/commit/634a9951466531d33afe2c22103daeeca3ba1aa3) update kubectl and delete perl (#3223)
 * [dabea2d96](https://github.com/kubeovn/kube-ovn/commit/dabea2d96905a490a624f2c80147b6ffdbe5f834) fix vpc-peer dualstack bug (#3204)
 * [0eb4f7948](https://github.com/kubeovn/kube-ovn/commit/0eb4f7948024b0f92c6b340a73cd3f02a8b3ae8e) fix ipam random get (#3200)
 * [a71201a48](https://github.com/kubeovn/kube-ovn/commit/a71201a48cde54e92bf91c4d12de212cb314a880) fix G101
 * [58ebea1d9](https://github.com/kubeovn/kube-ovn/commit/58ebea1d97c2b33b0cfa1abca61f7444e2a2a904) add err log to help find conflict ip owner (#2939)
 * [8246b8b72](https://github.com/kubeovn/kube-ovn/commit/8246b8b72c0498128448a4f386395821020c60ea) underlay: fix ip/route tranfer when the nic is managed by NetworkManager (#3184)
 * [febb78f82](https://github.com/kubeovn/kube-ovn/commit/febb78f82ba185cc79d032777ce6cce2f9d2691a) fix ovn build (#3166)
 * [0e12c017e](https://github.com/kubeovn/kube-ovn/commit/0e12c017e3a8ae77c3f1c1612e4b6736642890eb) chart: fix ovs-ovn upgrade (#3164)
 * [cb80f8d90](https://github.com/kubeovn/kube-ovn/commit/cb80f8d908bdde91226b727329d8d453380317ba) subnet: fix deleting lr policy on node deletion (#3178)
 * [eb9bcd583](https://github.com/kubeovn/kube-ovn/commit/eb9bcd583118d4a24ae16d11d9b17bd46031faad) delete append externalIds process in initIPAM (#3134)
 * [3ee977a29](https://github.com/kubeovn/kube-ovn/commit/3ee977a29b961b090bf94f65b3d4e5496c9d1be8) move unnecessary init process after startWorkers (#3124)
 * [0857593ce](https://github.com/kubeovn/kube-ovn/commit/0857593ce0ad06b0aae332c63e322eaffe845ec8) underlay: fix NetworkManager operation (#3147)
 * [132660e83](https://github.com/kubeovn/kube-ovn/commit/132660e834adaa6b445901197253da001a269f28) base: remove ovn patch for skipping ct (#3140)
 * [6177d38fd](https://github.com/kubeovn/kube-ovn/commit/6177d38fd4c1b4e7d09d884bb35f0830986221dc) delete append externalIds process in initIPAM (#3134)
 * [0b305d982](https://github.com/kubeovn/kube-ovn/commit/0b305d982a3f2faf518bafef3c1c27a56328298e) prepare for the next release
 * [fe4cf9e3b](https://github.com/kubeovn/kube-ovn/commit/fe4cf9e3b2a7018e254940e58c69419162c14e37) add e2e test for ovn db recover (#3118)
 * [896268556](https://github.com/kubeovn/kube-ovn/commit/896268556de0cc8e95f38e9a363125b7c34eaf42) update install.sh

### Contributors

 * bobz965
 * hzma
 * 张祖建
 * 袁又袁
 * 马洪贞

## v1.11.10 (2023-08-07)

 * [7f111db34](https://github.com/kubeovn/kube-ovn/commit/7f111db348957ca3a782d5b5f463ffb939146466) ovn: fix corrupted database file on start (#3112)
 * [a11d7e92d](https://github.com/kubeovn/kube-ovn/commit/a11d7e92d9d564de299618f81e882e4c5e499a38) update version to v1.11.10
 * [4b56b637d](https://github.com/kubeovn/kube-ovn/commit/4b56b637d2a8ece4701aa961127ef29250410123) fix u2o policy route generate too many flow tables cause oom
 * [935fa9279](https://github.com/kubeovn/kube-ovn/commit/935fa9279e88e3a564ced9e61bace6d23babbfb5) distinguish nat ip for central subnet with ecmp and active-standby (#3100)
 * [460655c22](https://github.com/kubeovn/kube-ovn/commit/460655c22e164e6b2dc096e6cb6fdd8ccf5957aa) bug_fix if only one port bind to the sg, then unbind the port to the sg ,it will not enforce in port_group (#3092)
 * [2bd75f0a1](https://github.com/kubeovn/kube-ovn/commit/2bd75f0a1f1c2dd5940a04128a42d68186bfc82d) Revert "fix sg"
 * [0400c4545](https://github.com/kubeovn/kube-ovn/commit/0400c4545579d224db85d8448c4c179c391e54e9) fix sg
 * [aca169d24](https://github.com/kubeovn/kube-ovn/commit/aca169d240bf6a59e68c071c43d9cf1429425d80) fix .status.default when initializing the default vpc (#3086)
 * [b2b19014b](https://github.com/kubeovn/kube-ovn/commit/b2b19014bbcacd060e989478812882f39fd633b1) cni-server: fix ovn mappings for vpc nat gateway (#3075)
 * [da86070e4](https://github.com/kubeovn/kube-ovn/commit/da86070e4eae090c3679855aff5d515da279294a) ovn client: fix sb chassis existence check (#3072)
 * [998e857df](https://github.com/kubeovn/kube-ovn/commit/998e857df340597c04754802f7b045e0b29caaf8) ci: do not pin go version (#3073)
 * [8597b9028](https://github.com/kubeovn/kube-ovn/commit/8597b9028a4b4140bb0bb27f25321dbad9647f59) ci: fix multus installation (#3062)
 * [b17174455](https://github.com/kubeovn/kube-ovn/commit/b171744553c0f05b8267ca9f22dd7cc9dc66e02c) ipam: fix ippool with single dual-stack address (#3054)
 * [c1a8d92a3](https://github.com/kubeovn/kube-ovn/commit/c1a8d92a38a4c1931f5f1277f9bd685dc89bd1dc) fix vpc lb init (#3046)
 * [03f94a527](https://github.com/kubeovn/kube-ovn/commit/03f94a5279759d506a9d8397f6704d127e46aca0) Revert "prepare for next release"
 * [28a4888d9](https://github.com/kubeovn/kube-ovn/commit/28a4888d9eb9fc2ef5352b9c1bf7f43afdd829f5) set genev_sys_6081 tx checksum off (#3045)
 * [6b0cc730c](https://github.com/kubeovn/kube-ovn/commit/6b0cc730c9af992ff22598ef6194ff59eb2e1a35) prepare for next release
 * [8ce77f850](https://github.com/kubeovn/kube-ovn/commit/8ce77f8508a4482d9b3582637cb465e736afadaf) fix ifname start with pod (#3038)
 * [a29d00cc1](https://github.com/kubeovn/kube-ovn/commit/a29d00cc178b7132e15422aa0f64ce2164c8aae5) static ip in exclude-ips can be allocated normally when subnet's availableIPs is 0 #3031
 * [58eef01e4](https://github.com/kubeovn/kube-ovn/commit/58eef01e45298ae109ebee4e5b97f28535dffd99) ci: pin go version to 1.20.5 (#3034)
 * [0f3d599bd](https://github.com/kubeovn/kube-ovn/commit/0f3d599bdfce9a4f5ed37c46429499e4e80d93e6) pinger: use fully qualified domain name (#3032)
 * [5400c37d7](https://github.com/kubeovn/kube-ovn/commit/5400c37d7fff4c3491f6d669f29c13328540e906) uninstall.sh: fix ipset name (#3028)
 * [c79184159](https://github.com/kubeovn/kube-ovn/commit/c791841595c04a30ae050454100fe5d45845afd8) kube-ovn-controller: fix workqueue metrics (#3011)
 * [54a0b1a69](https://github.com/kubeovn/kube-ovn/commit/54a0b1a69330eb60a28d8744d42ecdcac682f3cc) fix subnet finalizer (#3004)
 * [f3be5d12c](https://github.com/kubeovn/kube-ovn/commit/f3be5d12c493eb9630482797734f0d9d4d663550) choose subnet by pod's annotation in networkpolicy (#2987)
 * [2279f621d](https://github.com/kubeovn/kube-ovn/commit/2279f621d360df30f0de103f9e8f09f2bec43c1b) kubectl ko performance enhance (#2975) (#2994)
 * [572a2e851](https://github.com/kubeovn/kube-ovn/commit/572a2e851d9321be31881e8ebc0138f2d4aa27cd) fix deleting old sb chassis for a re-added node (#2989)
 * [abda1560c](https://github.com/kubeovn/kube-ovn/commit/abda1560ce89cd51fca2662dacd2c24242695ce6) underlay: fix NetworkManager syncer for virtual interfaces (#2988)
 * [ec17b7358](https://github.com/kubeovn/kube-ovn/commit/ec17b7358e7f304681b8e71b9e9a30434109dd89) underlay: does not set a device managed to no if it has VLAN managed by NM (#2986)
 * [7db9ff12a](https://github.com/kubeovn/kube-ovn/commit/7db9ff12a4b5dfe86d838484adaf79ee08fccc2d) bump k8s version to v1.26.6 (#2973)
 * [cc7768baa](https://github.com/kubeovn/kube-ovn/commit/cc7768baa2104ec4126e4de8f99dc6a44041ad93) base: fix ovn patches (#2972)
 * [7829b8737](https://github.com/kubeovn/kube-ovn/commit/7829b87377abae9893898f35e707b841d7eb0622) add detail comment
 * [c84a9748b](https://github.com/kubeovn/kube-ovn/commit/c84a9748b998a2eb3febce1394f9bd5f36da14d6) Kubectl ko diagnose perf release 1.11 (#2967)
 * [6325c83eb](https://github.com/kubeovn/kube-ovn/commit/6325c83eb44f95a2b6f6390692b6ae7eb39b199f) cni-server: reconcile ovn0 routes periodically (#2963)
 * [6ea123a7b](https://github.com/kubeovn/kube-ovn/commit/6ea123a7b1ec862a19def20a3964838c5d050ae3) uninstall.sh: flush and delete iptables chain OVN-MASQUERADE (#2961)
 * [738c40785](https://github.com/kubeovn/kube-ovn/commit/738c40785605557b7f2b03860ff8da0addf82303) underlay: sync NetworkManager IP config to OVS bridge (#2949)
 * [d9bab2e23](https://github.com/kubeovn/kube-ovn/commit/d9bab2e234dae8ae15d24773700c75396453b4cd) typo (#2952)
 * [b931b5bfd](https://github.com/kubeovn/kube-ovn/commit/b931b5bfd42792b0ebb8ba15f7a1c4448bea352c) Revert "base: fix ovn build failure (#2926)"
 * [d15874e03](https://github.com/kubeovn/kube-ovn/commit/d15874e03a165fef2e7f37f647584c86652c7715) Revert "nm not managed only in the change provide nic name case (#2754)" (#2944)
 * [168863cbc](https://github.com/kubeovn/kube-ovn/commit/168863cbc7d316a95119dbf028aac15cd2aaab0d) kubectl ko perf on release-1.11 (#2945)
 * [ea5f81a73](https://github.com/kubeovn/kube-ovn/commit/ea5f81a735ae1cb385d428ab4500c832f5a5f439) controller: fix DHCP MTU when the default network mode is underlay (#2941)
 * [6d883dc98](https://github.com/kubeovn/kube-ovn/commit/6d883dc986d2f950ac563557f378253cfeb66830) support set the mtu of  dhcpv4_options (#2930)
 * [effc11156](https://github.com/kubeovn/kube-ovn/commit/effc11156a089053bf3649ecb3c0947a617d8824) u2o support specify u2o ip on release-1.11 (#2937)
 * [94859807c](https://github.com/kubeovn/kube-ovn/commit/94859807c3106f1c48651de5892f830bb3128b11) modify lb-svc dnat port error (#2927)

### Contributors

 * bobz965
 * changluyi
 * hzma
 * yichanglu
 * 张祖建
 * 袁又袁
 * 马洪贞

## v1.11.8 (2023-06-12)

 * [d15f003a1](https://github.com/kubeovn/kube-ovn/commit/d15f003a109aebcdf06fec2dd1c45ceb3561286a) prepare for next release
 * [3796d1efb](https://github.com/kubeovn/kube-ovn/commit/3796d1efb813b85e54c2982116ca4f76e909762c) base: fix ovn build failure (#2926)
 * [98748f6e9](https://github.com/kubeovn/kube-ovn/commit/98748f6e9555cdffb57f14c6e7d7f9f26b6dab91) bump version number to v1.11.8
 * [0a0d254dd](https://github.com/kubeovn/kube-ovn/commit/0a0d254dd68bd23712eb6d155f3ceb966f3175f2) fix encap_ip will be lost when we restart the ovs-dpdk node (#2543)
 * [919c8eebf](https://github.com/kubeovn/kube-ovn/commit/919c8eebf937834bbe234e58474dc42aff2eb28c) cni-server: clear iptables mark before doing masquerade (#2919)
 * [91b383b0a](https://github.com/kubeovn/kube-ovn/commit/91b383b0ac1cd78c1523569fc1a6505a954d9f11) For eip created without spec.V4ip this field (#2912)
 * [b8523fc6a](https://github.com/kubeovn/kube-ovn/commit/b8523fc6a917f8ccd0d8de75b5f9e67fcbbf6c10) match outgoing interface when perform snat (#2911)

### Contributors

 * 张祖建
 * 袁又袁

## v1.11.7 (2023-06-05)

 * [0b27996b1](https://github.com/kubeovn/kube-ovn/commit/0b27996b1dd4c58f2f8d6b53a094f5bbc8656ca8) prepare for release 1.11.7
 * [b6b024587](https://github.com/kubeovn/kube-ovn/commit/b6b024587fe7b4c302fc6b4cb0a448f909316e23) underlay: do not delete patch ports created by ovn-controller (#2851)
 * [bed822993](https://github.com/kubeovn/kube-ovn/commit/bed822993b2c6ded0a7b11ec29e6510a123b904c) fix gc report error #2886
 * [42a5656c8](https://github.com/kubeovn/kube-ovn/commit/42a5656c8f3590132a4412cc8d26a5691ce661fd) add support of user-defined kubelet directory (#2388)
 * [4d1b12a8b](https://github.com/kubeovn/kube-ovn/commit/4d1b12a8b4637606af6f7a111c396dad71f4a64f) ci: fix valgrind result analysis (#2853)
 * [e1b79191c](https://github.com/kubeovn/kube-ovn/commit/e1b79191cc2c2f93fdfef4624a628fe5cd07ce3f) ovs: fix memory leak in qos (#2871)
 * [50cc00d0b](https://github.com/kubeovn/kube-ovn/commit/50cc00d0b8d696100c6345d6e93056bed8df7af8) prepare for next release

### Contributors

 * zhangzujian
 * 夜微澜
 * 张祖建
 * 马洪贞

## v1.11.6 (2023-05-25)

 * [f071a9740](https://github.com/kubeovn/kube-ovn/commit/f071a974045b9fe9c92d6e0c89f7677e726ca091) prepare for next release
 * [94644e129](https://github.com/kubeovn/kube-ovn/commit/94644e12939dd5878dc3cbabf251ed2eb12abde2) u2o support custom vpc release 1.11 (#2849)
 * [30f4cc303](https://github.com/kubeovn/kube-ovn/commit/30f4cc3030c3879e0fd0b7dd26379b00311279ba) kubectl-ko: fix trace when u2oInterconnection is enabled (#2836)
 * [e50687af3](https://github.com/kubeovn/kube-ovn/commit/e50687af3a4752320f7681e840037eb6fcf3a4c0) ci: detect ovs/ovn memory leak (#2839)
 * [767e102ac](https://github.com/kubeovn/kube-ovn/commit/767e102ac547bd1e6836e7a79feb4259d51667ce) fix underlay access to node through ovn0 (#2846)
 * [ae226e33c](https://github.com/kubeovn/kube-ovn/commit/ae226e33c4a33d698e5b129cf9b4e5614cb636b6) iptables: always do SNAT for access from other nodes to nodeport with external traffic policy set to Local (#2844)
 * [ef78fee1d](https://github.com/kubeovn/kube-ovn/commit/ef78fee1d54b65d4c5030baf7f3c26eba5ae068e) delete user tss (#2838)
 * [4dd164acd](https://github.com/kubeovn/kube-ovn/commit/4dd164acd252fd46fb36076d88ad0d439f211d0f) ci: fix no-avx512 image build
 * [f4033e738](https://github.com/kubeovn/kube-ovn/commit/f4033e738e685012c5506d6e74476f34c7176a1d) ci: fix kube-ovn-base build
 * [ea9547700](https://github.com/kubeovn/kube-ovn/commit/ea9547700dab638d0d1d4a52f4ce5feda0433132) refactor image builds (#2818)
 * [ddfedfa17](https://github.com/kubeovn/kube-ovn/commit/ddfedfa17924b243c694f3dee17a4286f1c3656a) fix MTU when subnet is using logical gateway (#2834)
 * [1346b0e7e](https://github.com/kubeovn/kube-ovn/commit/1346b0e7ec7ce94287fcc22c68274ea475a828f7) update vpc dns env value
 * [5d8b106a0](https://github.com/kubeovn/kube-ovn/commit/5d8b106a0096d4b104579f95eaad05009074bcb9) add route for service ip range when init vpc-nat-gw (#2821)
 * [cd4ff4f6e](https://github.com/kubeovn/kube-ovn/commit/cd4ff4f6e3dac0ade08da8a13dd1b07faf3af2ab) fix cleanup order (#2792)
 * [94e7463e8](https://github.com/kubeovn/kube-ovn/commit/94e7463e8aad160df64853003dd8117103d98c68) add available check for northd enpoint
 * [f7a80c900](https://github.com/kubeovn/kube-ovn/commit/f7a80c9001abece8492b4576427e034a234d52c3) update release note

### Contributors

 * changluyi
 * hzma
 * zhangzujian
 * 张祖建
 * 马洪贞

## v1.11.5 (2023-05-10)

 * [632bad30f](https://github.com/kubeovn/kube-ovn/commit/632bad30f4f93661c6cbb9625514d63faa58a929) prepare for release 1.11.5
 * [bc4637c04](https://github.com/kubeovn/kube-ovn/commit/bc4637c0479b95bdac615d7cbccf5159a9ae6a29) reorder the deletion to avoid dependency conflict
 * [a8539c579](https://github.com/kubeovn/kube-ovn/commit/a8539c579ae171e65e7777169b1d55181d4665ad) fix ip statistics in subnet status (#2769)
 * [655d5ff26](https://github.com/kubeovn/kube-ovn/commit/655d5ff265627fba55f82f713d6661298c7b57eb) support disable arp check ip conflict in vlan provider network (#2760)
 * [a5720d6f6](https://github.com/kubeovn/kube-ovn/commit/a5720d6f63cdb8eb2a31bd4f55b9a372e0417eec) cni-server: wait ovs-vswitchd to be running (#2759)
 * [8a4e97b54](https://github.com/kubeovn/kube-ovn/commit/8a4e97b54bf35b5bed68c5992ccbf052e28f8494) ci: run kube-ovn e2e for underlay (#2762)
 * [87c686831](https://github.com/kubeovn/kube-ovn/commit/87c686831248f24b486b27cd8958c003e2e5c6be) iptables: use the same mode with kube-proxy (#2758)
 * [944f30491](https://github.com/kubeovn/kube-ovn/commit/944f30491f679d6357b0b9f183919f33dd88873f) nm not managed only in the change provide nic name case (#2754)
 * [a55db01d8](https://github.com/kubeovn/kube-ovn/commit/a55db01d8122d7154dfd7665942bb6ed78bbb4a9) update policy route when change from ecmp to active-standby (#2717)
 * [ba6398247](https://github.com/kubeovn/kube-ovn/commit/ba63982478976e4bd7c61543100b50fee815dac8) fix recover db failed using offical doc (#2718)
 * [81b60ac84](https://github.com/kubeovn/kube-ovn/commit/81b60ac84ef589c0d3ca3615f431e780839225f5) fix_base_sg_rule (#2401)
 * [e80879c5e](https://github.com/kubeovn/kube-ovn/commit/e80879c5e80e5120487df83eeae8905b14c53631) add base sg rules for ports (#2365)
 * [f90aa3981](https://github.com/kubeovn/kube-ovn/commit/f90aa3981d0967bdfd1b74fed1024d9eb56d1efd) bump base images
 * [abaee01a4](https://github.com/kubeovn/kube-ovn/commit/abaee01a4a1b36aa3ed23f4c131d894df6ef8c9e) base: remove patch for fixing ofpbuf memory leak (#2715)
 * [2e800bf24](https://github.com/kubeovn/kube-ovn/commit/2e800bf244e5b0e93a45f842a0e357bfec5a4db9) prepare for release 1.11.4
 * [22367be61](https://github.com/kubeovn/kube-ovn/commit/22367be6166eb45f5f1c1889895193fedcba8eb4) cni-server: do not perform ipv4 conflict detection during VM live migration (#2693)
 * [49dfd39ea](https://github.com/kubeovn/kube-ovn/commit/49dfd39eaa6e63b049a10f6929e8ed1b2c652858) fix can not clean the last abandoned snat table (#2701)
 * [6ec1982a6](https://github.com/kubeovn/kube-ovn/commit/6ec1982a69ccb5e006b875093312a420703b2aa8) replace StrategicMergePatchType with MergePatchType (#2694)
 * [320f5670e](https://github.com/kubeovn/kube-ovn/commit/320f5670e042a9238a089a4cd2b3899ae4f56e0d) fix build error by partially revert 951f89c5
 * [d4eabab03](https://github.com/kubeovn/kube-ovn/commit/d4eabab03ad28eec2915da4c616a139274bae9b0) ovn-controller: do not send GARP on localnet for Kube-OVN ports (#2690)
 * [951f89c54](https://github.com/kubeovn/kube-ovn/commit/951f89c54913747606251bf24acbe344cdc6b3e1) adapt ippool annotation (#2678)
 * [96e8be6d6](https://github.com/kubeovn/kube-ovn/commit/96e8be6d6f4d99f9a967fdf15aad28a627b4efa0) netpol: fix packet drop casued by incorrect address set deletion (#2677)
 * [6b95cecde](https://github.com/kubeovn/kube-ovn/commit/6b95cecded7936cf36d5bc6fbb50ba5f3f6c19af) fix pg set port fail when lsp is already deleted (#2658)
 * [5ad2bafe0](https://github.com/kubeovn/kube-ovn/commit/5ad2bafe07d611a55059dcd15163fa889ce0020e) add subnetstatus lock for handleAddOrUpdateSubnet (#2669)
 * [f314ab582](https://github.com/kubeovn/kube-ovn/commit/f314ab582f1e1eb75287305a8534b80bdb052a9c) broadcase free arp when pod setup
 * [e29fdc962](https://github.com/kubeovn/kube-ovn/commit/e29fdc962d3a5d8f1a5e01f998a9baf938c9e1b1) delete sync user (#2629)
 * [621423f7d](https://github.com/kubeovn/kube-ovn/commit/621423f7d5dede2f335094a0972282cb26cfaedf) Add ipsec package to image release 1.11 (#2618)
 * [9c80381ba](https://github.com/kubeovn/kube-ovn/commit/9c80381ba44e78d05f3ea8b73c155e2c04e3c496) ci: deploy multus in thick mode (#2628)
 * [2731e8e30](https://github.com/kubeovn/kube-ovn/commit/2731e8e3082480dc022b8d4ed5b666df910072c8) libovsdb: use monitor_cond as the monitor method (#2627)
 * [71a8ffe36](https://github.com/kubeovn/kube-ovn/commit/71a8ffe36facf8693d5ce28f7bb8f60cdf3d1f33) ci: fix multus installation (#2622)
 * [786fea901](https://github.com/kubeovn/kube-ovn/commit/786fea9010862f9daca54fec7de0653860240924) ovs: fix dpif-netlink ofpbuf memory leak (#2620)
 * [d9647b4d4](https://github.com/kubeovn/kube-ovn/commit/d9647b4d446f1841cc633b66a1f0dc5a42f7c471) update Dockerfile.debug
 * [5b099ed27](https://github.com/kubeovn/kube-ovn/commit/5b099ed27272accd2e351be368fc8c442394c81d) ci: fix multus installation (#2604)
 * [fdc2301bd](https://github.com/kubeovn/kube-ovn/commit/fdc2301bdd4a1f12c36221adcde57890c049f0f5) cut invalid OVN_NB_DAEMON to make log more readable (#2601)
 * [02b1e140b](https://github.com/kubeovn/kube-ovn/commit/02b1e140b606261ba3faf215c85734fe05ce1ba8) unittest: fix length assertion (#2597)
 * [209246bd9](https://github.com/kubeovn/kube-ovn/commit/209246bd9fd8ef491166afe5507fa9d29d526610) bump base image
 * [d2f1a8012](https://github.com/kubeovn/kube-ovn/commit/d2f1a8012bfbfc12511cca9642e8763ac9303f50) security: remove CVE-2022-29526 from .trivyignore
 * [7a69233f8](https://github.com/kubeovn/kube-ovn/commit/7a69233f8cbd7c390b0962077b527816c9b1e55d) base: fix CVE-2022-3294 (#2594)
 * [ea46479db](https://github.com/kubeovn/kube-ovn/commit/ea46479db33deb6bcbc7098f0be3b893a8ef11ac) underlay: get address/route before setting nm managed to no (#2592)
 * [d67d40d39](https://github.com/kubeovn/kube-ovn/commit/d67d40d3918518ba4b48521988b9bbd1c21f5a31) base: fix ovs patches (#2590)
 * [ed14bc2d0](https://github.com/kubeovn/kube-ovn/commit/ed14bc2d062be74a210392869154ae5aabd130b6) ci: bump kind image to v1.26.3 (#2581)
 * [9c01b1bd4](https://github.com/kubeovn/kube-ovn/commit/9c01b1bd4c4d12dc9113c4bb9058bb86d7119a7f) move ipam.subnet.mutex to caller (#2571)
 * [fb70f9393](https://github.com/kubeovn/kube-ovn/commit/fb70f9393513908ee1b7267d24452ec2b9300b1c) fix: memory leak in IPAM caused by leftover map keys (#2566)
 * [f4f990b38](https://github.com/kubeovn/kube-ovn/commit/f4f990b3812a6318eed4dd13622851c6a45d4ce7) fix ovn-bridge-mappings deletion (#2564)
 * [e4242a01e](https://github.com/kubeovn/kube-ovn/commit/e4242a01ee72c4156fb9c25254a741bda98e96ee) fix go mod list (#2556)
 * [4c08bfe09](https://github.com/kubeovn/kube-ovn/commit/4c08bfe0908e3d78275c6ff705dc695e784dd300) do not set device unmanaged if NetworkManager is not running (#2549)
 * [39c99c6ea](https://github.com/kubeovn/kube-ovn/commit/39c99c6eaae1dc9e6b70a8f5ade65e089e45ce2f) fix update dnat rules not effect correctly (#2518)
 * [7eb7ed6ed](https://github.com/kubeovn/kube-ovn/commit/7eb7ed6ed04724396c3c7d490f69c5eb9bfe36e8) underlay: fix network manager operation (#2546)
 * [8f67a3245](https://github.com/kubeovn/kube-ovn/commit/8f67a3245910051926c309c4e31b5863938c78a5) controller: fix apiserver connection timeout on startup (#2545)
 * [4b8654db8](https://github.com/kubeovn/kube-ovn/commit/4b8654db81737fe54779bc9222156caf241e57f2) underlay: delete altname after renaming the link (#2539)
 * [8d0d56ecb](https://github.com/kubeovn/kube-ovn/commit/8d0d56ecba2b49829bc3976269105935f873c349) underlay: fix link name exchange (#2516)
 * [f22535e36](https://github.com/kubeovn/kube-ovn/commit/f22535e36530a379b2889d6818867af2a6d6b5fa) fix changging the stopped vm's subnets, the vm cann't start normally (#2463)
 * [5bd71ba8b](https://github.com/kubeovn/kube-ovn/commit/5bd71ba8bf54d49a21f5ab08c141917ed0dbeea0) add kubevirt multus nic lsp before gc process (#2504)
 * [d9ccaf7b3](https://github.com/kubeovn/kube-ovn/commit/d9ccaf7b39d1446f691b775d93a07facca1df88c) update for release v1.11.3

### Contributors

 * bobz965
 * changluyi
 * hzma
 * yichanglu
 * zhangzujian
 * 夜微澜
 * 张祖建
 * 袁又袁
 * 马洪贞

## v1.11.3 (2023-03-18)

 * [9fe900fce](https://github.com/kubeovn/kube-ovn/commit/9fe900fce63ce684f6d8f440ac7a8154b0cccc69) prepare for release v1.11.3
 * [d70bf21f9](https://github.com/kubeovn/kube-ovn/commit/d70bf21f9a1bb3b5153f8fb5b88bfbc83523f896) ensure address label is correct before deleting it (#2487)
 * [df493a8bd](https://github.com/kubeovn/kube-ovn/commit/df493a8bde433ad88609d871255199d1d6aae835) add node to addNodeQueue if required annations are missing (#2481)
 * [b4145855d](https://github.com/kubeovn/kube-ovn/commit/b4145855dcb8d4a1ab832cbd44aef7d12e87ad74) fix ips CR not found due to etcd error (#2472)
 * [63afc1f6c](https://github.com/kubeovn/kube-ovn/commit/63afc1f6c0f441c3cc1ac59657af4380e1db4831) ci: fix ovn-ic installation (#2456)
 * [f790d5a12](https://github.com/kubeovn/kube-ovn/commit/f790d5a12680adf8c78b4ea76a1b54c772a2478f) do not set subnet's vlan empty on failure (#2445)
 * [0ff516bbd](https://github.com/kubeovn/kube-ovn/commit/0ff516bbddd2a01ed485e33fa0ca235ba7af08b6) change cni version from v1.1.1 to v1.2.0
 * [b0935b7c7](https://github.com/kubeovn/kube-ovn/commit/b0935b7c7f2f146b4bac3eec298aba6d34d7988b) fix ovn-speaker router bug (#2433)
 * [7f6ba2b1a](https://github.com/kubeovn/kube-ovn/commit/7f6ba2b1a7670e11109f2a0a1f84342c6a6f9809) fix chart install/upgrade e2e (#2426)
 * [e0fe08c5f](https://github.com/kubeovn/kube-ovn/commit/e0fe08c5f5bfe32c3d3eab4fd8bdc7f792f66495) ci: fix cilium chaining e2e (#2391)
 * [365e8f47f](https://github.com/kubeovn/kube-ovn/commit/365e8f47f1fbc51f1a4ea715b433053b8e201a95) Modify the pod scheduling of vpcdns (#2420)
 * [13c7319f0](https://github.com/kubeovn/kube-ovn/commit/13c7319f0d3b509e90ced918f0de51a8812d11be) fix: python package issues
 * [7100e157f](https://github.com/kubeovn/kube-ovn/commit/7100e157f7af9574497603bdae4f4c7853fe3cee) update ipv6 security-group remote group name (#2389)
 * [909c1b6b1](https://github.com/kubeovn/kube-ovn/commit/909c1b6b12c7f56ad2a66c0bf7cad452ab46cc7b) Fix routeregexp ipv6 (#2395)
 * [20cdc9d8f](https://github.com/kubeovn/kube-ovn/commit/20cdc9d8fe265499698d4d1470e607596d863456) ci: fix ref name check (#2390)
 * [af25e6adc](https://github.com/kubeovn/kube-ovn/commit/af25e6adc03ce52f0237adf6264ccf86ba3ffb14) bump base images
 * [064df2513](https://github.com/kubeovn/kube-ovn/commit/064df2513b5a67f1da286aaa893e309a794ec989) ci: skip netpol e2e automatically for push events (#2379)
 * [d5005b74a](https://github.com/kubeovn/kube-ovn/commit/d5005b74a8f49ae1238462f00ac25d60b3299d12) ci: make path filter more accurate (#2381)
 * [0f308f344](https://github.com/kubeovn/kube-ovn/commit/0f308f3442fdb5303c5d17a8849b4aa45fba3372) fix service dual stack add/del cluster ips not change ovn nb
 * [4a70baefa](https://github.com/kubeovn/kube-ovn/commit/4a70baefaa9cd760dc384e955062fab17b7baf3c) ci: fix path filter for windows build (#2378)
 * [37662226d](https://github.com/kubeovn/kube-ovn/commit/37662226d2ba62b1083eba1b5eadc948a2b124f2) e2e: run specs in parallel (#2375)

### Contributors

 * Daviddcc
 * KillMaster9
 * changluyi
 * hzma
 * jeffy
 * yichanglu
 * zhangzujian
 * 张祖建

## v1.11.2 (2023-02-22)

 * [67fd6efb8](https://github.com/kubeovn/kube-ovn/commit/67fd6efb8dd37dc640b8f430fb9e33b023bd1238) fix CVE-2022-41723
 * [354485b38](https://github.com/kubeovn/kube-ovn/commit/354485b3887c690cf741a31653e85680d1d3c28f) bump base images
 * [5b58c8f89](https://github.com/kubeovn/kube-ovn/commit/5b58c8f8915b7dadff898246873a559692b7ef96) fix: ovs-ovn should reboot now (#2298)
 * [eae134e93](https://github.com/kubeovn/kube-ovn/commit/eae134e933a5b3b7e007f171c2f3544a30283756) ci: fix default branch test (#2369)
 * [ef6c1cd6b](https://github.com/kubeovn/kube-ovn/commit/ef6c1cd6b124cd863213fb17458e56cc0f56fd93) fix github actions workflows (#2363)
 * [8bb647daf](https://github.com/kubeovn/kube-ovn/commit/8bb647daf71918dd1179e1a306a384d83bbfc47f) simplify github actions workflows (#2338)
 * [8e8417ccc](https://github.com/kubeovn/kube-ovn/commit/8e8417cccef1af5b3b2ce7bedf5f7312095f85f4) Fixed iptables creation failure due to an excessively long label (#2366)
 * [50059147f](https://github.com/kubeovn/kube-ovn/commit/50059147f44abc335c5389c7d48319aab2a206f3) Improve webhook (#2278)
 * [0f8c04e98](https://github.com/kubeovn/kube-ovn/commit/0f8c04e986ddd11f797065d00d2341affdd865bf) eip status状态切换缓慢 (#2256)
 * [5603a98f2](https://github.com/kubeovn/kube-ovn/commit/5603a98f2420771927305bae80657e8a284503a7) fix vip create (#2245)
 * [8fc8e0cee](https://github.com/kubeovn/kube-ovn/commit/8fc8e0ceeba3a766c2ef4e798d75cde02d8f8186) improve webhook functions for vpc and subnet (#2241)
 * [9cc91bbb8](https://github.com/kubeovn/kube-ovn/commit/9cc91bbb86a594a48d71261b755301369dc5272f) fix webhook (#2236)
 * [3b8da6adf](https://github.com/kubeovn/kube-ovn/commit/3b8da6adff7b47f3bfb075efa1ee6a34ea8b141c) use existing node switch cidr instead of the configured one (#2359)
 * [87b8bdec9](https://github.com/kubeovn/kube-ovn/commit/87b8bdec981eaa529ebf028299ebfaf3a5087199) Release 1.11 merge netpol (#2361)
 * [578b39219](https://github.com/kubeovn/kube-ovn/commit/578b392191b60cd491de4f348dc8b17bdf0f3a27) Release 1.11 merge netpol (#2355)
 * [14a8b9bb7](https://github.com/kubeovn/kube-ovn/commit/14a8b9bb72d8a9028c9521fd65c568ade7b8dbb5) prepare for 1.11.2
 * [965207213](https://github.com/kubeovn/kube-ovn/commit/965207213ba47457d7c5199686ea1ae4d7991dde) do not remove link local route on ovn0 (#2341)
 * [f83af7443](https://github.com/kubeovn/kube-ovn/commit/f83af7443d00ff5cf5e4ac72d5e79afe153eaea2) fix encap ip when the tunnel interface has multiple addresses (#2340)
 * [746e5d0a8](https://github.com/kubeovn/kube-ovn/commit/746e5d0a8f7c2bf4352e141273d6701c4113acca) enqueue endpoint when handling service add event (#2337)
 * [3e9d928b3](https://github.com/kubeovn/kube-ovn/commit/3e9d928b3cebd84e6555786b59bba101d50b152c) Add neighbor-address format check for kube-ovn-speaker (#2335)
 * [f7156c9dc](https://github.com/kubeovn/kube-ovn/commit/f7156c9dc5b58c77664bdfd978ec43c1ab1755d7) OVN LB: add support for SCTP protocol (#2331)
 * [354fd4008](https://github.com/kubeovn/kube-ovn/commit/354fd40084a4be0d32d3b0622b57c72557ea1804) fix getting service backends in dual-stack clusters (#2323)
 * [1fe492d57](https://github.com/kubeovn/kube-ovn/commit/1fe492d57b4c4f6f4609d24bcc35943dc1a194f0) fix github actions workflow
 * [0133c48f7](https://github.com/kubeovn/kube-ovn/commit/0133c48f715c3c945b98cfa26f7993171a4701c0) perform the gateway check but ignore the result when the annotation of subnet is ‘disableGatewayCheck=true’ to make sure of the first network packet (#2290)
 * [a5cce7443](https://github.com/kubeovn/kube-ovn/commit/a5cce74437bae07a0256687b7bf2ae5fd1fc1135) Add the bgp router-id format check (#2316)

### Contributors

 * KillMaster9
 * changluyi
 * jeffy
 * lut777
 * qiutingjun
 * zhangzujian
 * 张祖建

## v1.11.1 (2023-02-09)

 * [1008299a7](https://github.com/kubeovn/kube-ovn/commit/1008299a7e7d9636b8d0dd282f9c9accedb21906) prepare for release v1.11.1
 * [3c0f64bcf](https://github.com/kubeovn/kube-ovn/commit/3c0f64bcf4fb7224dceee90c1cbc1a51103eb390) fix: ovnic del old AZ after establish the new as name (#2229)
 * [57f2c17d3](https://github.com/kubeovn/kube-ovn/commit/57f2c17d3c1d8584efbb4ad1544f0931df7d6d1c) fix u2o code err
 * [cd3c333f6](https://github.com/kubeovn/kube-ovn/commit/cd3c333f69cf7d09e2ccc33efeba1177a3e0a839) fix kube-ovn-controller crash on startup (#2305)
 * [8c4b917f1](https://github.com/kubeovn/kube-ovn/commit/8c4b917f1e5224628259d1e46f8a1bd47bdc4dd6) fix Makefile
 * [cdcd9a9ca](https://github.com/kubeovn/kube-ovn/commit/cdcd9a9caf78af2dabafa6fb2453aaca631d10ba) delete htb qos priority (#2288)
 * [602ee37d2](https://github.com/kubeovn/kube-ovn/commit/602ee37d2e160123bd93e95e30dbee04afd64f2d) fix gosec ci installation (#2295)
 * [b367218b5](https://github.com/kubeovn/kube-ovn/commit/b367218b59018ff2a45a33d84ece57710a1dd4bb) ovn northd: fix connection inactivity probe (#2286)
 * [b90b552ad](https://github.com/kubeovn/kube-ovn/commit/b90b552adb5fb69cf66c52ef9cbe1fffb5e35611) fix ct new config error
 * [a6663031b](https://github.com/kubeovn/kube-ovn/commit/a6663031bc201420a4516637a030fd612a2a6a2e) fix network break on kube-ovn-cni startup (#2272)
 * [22ff73531](https://github.com/kubeovn/kube-ovn/commit/22ff73531add6fa9f9f958eb0bc38e35b6b5f2e7) fix setting mtu for ovs internal port (#2247)
 * [4f957c6a2](https://github.com/kubeovn/kube-ovn/commit/4f957c6a2de9f4fc89114f6b0cd4d917c7557b61) fix gosec installation
 * [5ed45f38d](https://github.com/kubeovn/kube-ovn/commit/5ed45f38d44e5bac6f67cfced7ab8dc862251a4c) fix ovn patches
 * [1eedbb165](https://github.com/kubeovn/kube-ovn/commit/1eedbb1659a1cae30feaa1473261367aa419eba9) ovn db: add support for listening on pod ip (#2235)
 * [996faa1f3](https://github.com/kubeovn/kube-ovn/commit/996faa1f3f369a7e375c478c2dfe4c6cdab566aa) Revert "prepare for next release"
 * [0bf23975b](https://github.com/kubeovn/kube-ovn/commit/0bf23975b71dd809e1c7a14779a73ce8f7bb96a0) prepare for next release

### Contributors

 * changluyi
 * lut777
 * zhangzujian
 * 张祖建
 * 马洪贞

## v1.11.0 (2023-01-09)

 * [a49d18198](https://github.com/kubeovn/kube-ovn/commit/a49d18198e03afe290d73e6b2969200f090e6455) Update CHANGELOG.md for v1.11.0
 * [59bc50f73](https://github.com/kubeovn/kube-ovn/commit/59bc50f734bd8b3fa7ead27ab22ddf1574e77c1c) feat: add helm upgrade e2e (#2222)
 * [25f897378](https://github.com/kubeovn/kube-ovn/commit/25f89737827c66666b9317b5b4392b9071dcc251) fix: now route with connected/static will all be sync (#2231)
 * [c2467d219](https://github.com/kubeovn/kube-ovn/commit/c2467d219d6bfcf082a2f19aa9978d8bc5a8818f) add enable-metrics arg to disable metrics (#2232)
 * [67024ec5f](https://github.com/kubeovn/kube-ovn/commit/67024ec5f0cdaa2a9b910de593a452c5ccb481da) add u2o test case (#2203)
 * [f5d801109](https://github.com/kubeovn/kube-ovn/commit/f5d8011098ef0a22513b53adb2779929d0b7d978) add more args to break test server
 * [f5b9eef8f](https://github.com/kubeovn/kube-ovn/commit/f5b9eef8f3dc7759a678af477ed6585b2be5c234) add release-1.8/1.9/1.10 to scheduled e2e (#2224)
 * [ab5a2c825](https://github.com/kubeovn/kube-ovn/commit/ab5a2c825acb5d20ae65b417814d85adcafdbf1e) cni-server: fix waiting for routed annotation (#2225)
 * [6fd9ea0cb](https://github.com/kubeovn/kube-ovn/commit/6fd9ea0cb390671080361f136039117620e2f6f2) build(deps): bump golang.org/x/sys from 0.3.0 to 0.4.0 (#2223)
 * [cbde65e24](https://github.com/kubeovn/kube-ovn/commit/cbde65e24c2b7a74b6a7e1b0ffd832c5800958ac) feature: detect ipv4 address conflict in underlay (#2208)
 * [64d6f24f5](https://github.com/kubeovn/kube-ovn/commit/64d6f24f5ed4296cafccf6c2a2c3fec159a150d8) fix git ref name in e2e (#2218)
 * [b0cd45c63](https://github.com/kubeovn/kube-ovn/commit/b0cd45c63886c7f38b2ca9fdbabac0d19ddb27c3) fix e2e for v1.8 (#2216)
 * [5843892b0](https://github.com/kubeovn/kube-ovn/commit/5843892b007b1554be0970e616cabfcfb88abd9d) some fixes for e2e testing (#2207)
 * [b6a11789c](https://github.com/kubeovn/kube-ovn/commit/b6a11789c3ae5196a597d71decbf952cbc7688e8) build(deps): bump github.com/osrg/gobgp/v3 from 3.9.0 to 3.10.0 (#2209)
 * [4f08d9413](https://github.com/kubeovn/kube-ovn/commit/4f08d941306c3fd1087ef4ae537eaf0c9ed35fe3) distinguish ippool process for dualstack and normal ippool situation (#2204)
 * [098a8212d](https://github.com/kubeovn/kube-ovn/commit/098a8212dae2c3698c77b249f8e279cb90955e51) u2o feature (#2189)
 * [c0d76fd8b](https://github.com/kubeovn/kube-ovn/commit/c0d76fd8b494a28c68581d54fcce5eb8a2e18e4c) ovn nb and sb can't bind lan ip in ssl (#2200)
 * [1489b65cb](https://github.com/kubeovn/kube-ovn/commit/1489b65cb15f7792434351bb01dc60d29807da3a) build(deps): bump sigs.k8s.io/controller-runtime from 0.14.0 to 0.14.1 (#2199)
 * [16002a282](https://github.com/kubeovn/kube-ovn/commit/16002a282a9e77059611dfa52641ff0119a91ca6) local ip bind to service (#2195)
 * [1407eba24](https://github.com/kubeovn/kube-ovn/commit/1407eba24b9c78231e371dcf4f0f36e729dce5b5) refactor e2e testing (#2078)
 * [86fab667b](https://github.com/kubeovn/kube-ovn/commit/86fab667b414a5ed470ae8ad7ae9e64020844ac9) fix: ovs gc just for pod if (#2187)
 * [1a43c6dee](https://github.com/kubeovn/kube-ovn/commit/1a43c6deed0b00e40b48cc39ff093e781c31314e) update docs link in install.sh (#2196)
 * [02feb9a91](https://github.com/kubeovn/kube-ovn/commit/02feb9a91543c31f72aaa31134008442c5eaa95d) fix lr policy for default subnet with logical gateway enabled (#2177)
 * [3e129fe1b](https://github.com/kubeovn/kube-ovn/commit/3e129fe1b51a51b61ee6fb5213fb97674eb31ee3) sync delete pod process from release-1.9 (#2190)
 * [b6e507065](https://github.com/kubeovn/kube-ovn/commit/b6e507065cf408ac508f4801dab6654ade39302f) fix: update helm 1.11.0 (#2182)
 * [3fb825c8f](https://github.com/kubeovn/kube-ovn/commit/3fb825c8f723c06f4ad20033f9c6dee1cb506146) reserve pod eip static route when update vpc (#2185)
 * [159fd9f02](https://github.com/kubeovn/kube-ovn/commit/159fd9f0290d32bd1dc4d86a6877492c8401066f) ignore conflict check for pod ip crd (#2188)
 * [4d6ad644f](https://github.com/kubeovn/kube-ovn/commit/4d6ad644f8772936afe202169db84aec8f2ff74a) remove unused subnet status fields (#2178)
 * [484fe97ab](https://github.com/kubeovn/kube-ovn/commit/484fe97ab3e214055d8152d518a1d92f082f2116) fix:react leader elect (#2167)
 * [c914fe78d](https://github.com/kubeovn/kube-ovn/commit/c914fe78de5860868ad560c4e631c44d2f7d8be0) fix base/windows build (#2172)
 * [6a8fc2f3b](https://github.com/kubeovn/kube-ovn/commit/6a8fc2f3b5e3e51c2e2074108388ed06bb2c7b11) add metric interface_rx_multicast_packets (#2156)
 * [2b5e28ff9](https://github.com/kubeovn/kube-ovn/commit/2b5e28ff9735df0829eae63c5d947b43dd64f2a3) build(deps): bump github.com/onsi/gomega from 1.24.1 to 1.24.2 (#2168)
 * [0992f36f8](https://github.com/kubeovn/kube-ovn/commit/0992f36f89743e4bc2f06b4290ddcd7f8098f691) update wechat link
 * [d45a04407](https://github.com/kubeovn/kube-ovn/commit/d45a04407532dce7e9e85087835f5e05e961b1c4) build(deps): bump github.com/Microsoft/hcsshim from 0.9.5 to 0.9.6 (#2161)
 * [adecee76f](https://github.com/kubeovn/kube-ovn/commit/adecee76fb680cd24a30358affc28224053136ad) ci: refactor previous push multi arch (#2164)
 * [5e4955c91](https://github.com/kubeovn/kube-ovn/commit/5e4955c918026b2d21abaf5fbeecb8cfdcb56280) security: we should check all the vulnerabilities that can be fixed (#2163)
 * [502a25bfb](https://github.com/kubeovn/kube-ovn/commit/502a25bfb7c014e86801d8232d3a45e0d8d198f3) An error occurred when netpol was added in double-stack mode (#2160)
 * [dbbbddc16](https://github.com/kubeovn/kube-ovn/commit/dbbbddc166e5d09f815cea1fe909fa30dc1bf600) add process for delete networkpolicy start with number (#2157)
 * [26f407fc0](https://github.com/kubeovn/kube-ovn/commit/26f407fc073c9436e322d36212aabeeda1f09d54) security remove private key (#2159)
 * [57457bd46](https://github.com/kubeovn/kube-ovn/commit/57457bd46d7216e897c1816d412023edcb07c488) add scheduled e2e testing (#2144)
 * [5444126aa](https://github.com/kubeovn/kube-ovn/commit/5444126aad775a9779880f06967b491e862412f5) northd: fix race condition in health check (#2154)
 * [755a46a69](https://github.com/kubeovn/kube-ovn/commit/755a46a6981984112e7580d449631f466fce643e) add check for subnet cidr (#2153)
 * [c627468ab](https://github.com/kubeovn/kube-ovn/commit/c627468abbca5544f43b3af3f3e367f9727cd56b) delete nc cmd in image (#2148)
 * [207a52cdf](https://github.com/kubeovn/kube-ovn/commit/207a52cdfbff68572e1d26f64e59e8a4cef09299) bump k8s to v1.26 (#2152)
 * [a4a8b5ad6](https://github.com/kubeovn/kube-ovn/commit/a4a8b5ad619d0e55d799d6c93f77a8228f6c152a) add benchmark test for ipam (#2123)
 * [4b1e78c22](https://github.com/kubeovn/kube-ovn/commit/4b1e78c22839d1cb04a930cc250879eb1469c2ff) update: add YuDong Wang into MAINTAINERS (#2147)
 * [39ee1e7cc](https://github.com/kubeovn/kube-ovn/commit/39ee1e7cc4513c19c73dfdc028cd17927e8b4102) build(deps): bump k8s.io/sample-controller from 0.25.4 to 0.25.5 (#2146)
 * [7aa9bdbcc](https://github.com/kubeovn/kube-ovn/commit/7aa9bdbcc59417e46861f7e497815360fc0b504a) delete nc in base image (#2141)
 * [aab79cb8e](https://github.com/kubeovn/kube-ovn/commit/aab79cb8e1e95b8cccc640de9ac9cac491e9942b) update go modules (#2142)
 * [fa32177d2](https://github.com/kubeovn/kube-ovn/commit/fa32177d242849242469c0f6f69716aa78edfc62) delete ip crd base on podName (#2143)
 * [4072eb762](https://github.com/kubeovn/kube-ovn/commit/4072eb76269c8ddf409874e704d5d89a5b0d50a0) fix vpc spec external not true after init external gw (#2140)
 * [51907e025](https://github.com/kubeovn/kube-ovn/commit/51907e0254e9a7243c7f330dad2f256c65eb007b) refactor ipam unit test (#2126)
 * [ad56e98fb](https://github.com/kubeovn/kube-ovn/commit/ad56e98fb5375faa2fdec66b7cf4de65165b52ef) build(deps): bump github.com/k8snetworkplumbingwg/network-attachment-definition-client (#2139)
 * [012ab59e1](https://github.com/kubeovn/kube-ovn/commit/012ab59e164dc743d047023c22b0efca72401d90) some optimization for provider network status update (#2135)
 * [c410d8b49](https://github.com/kubeovn/kube-ovn/commit/c410d8b498b9ccf49152c6528be905b9a6069e37) simplify iptables eip nat (#2137)
 * [ef4e75554](https://github.com/kubeovn/kube-ovn/commit/ef4e75554d7a1495ca35fb3a95ec89cf57cf5459) kind: support to specify api server address/port (#2134)
 * [9bbf5e439](https://github.com/kubeovn/kube-ovn/commit/9bbf5e4399df6442708d110e0b63abcae3dfeea9) kubectl-ko: fix registry/version (#2133)
 * [2156ef0d7](https://github.com/kubeovn/kube-ovn/commit/2156ef0d7ec42004f5930b2512a4ad4a1e0efa3d) check if subnet cidr is correct (#2136)
 * [f58c88fc2](https://github.com/kubeovn/kube-ovn/commit/f58c88fc244ef7aaa2b28358b813488f6b5e1204)  fix: sometimes alloc ipv6 address failed sometimes ipam.GetStaticAddress return NoAvailableAddress (#2132)
 * [27d22b7fb](https://github.com/kubeovn/kube-ovn/commit/27d22b7fb24b2e7eca178014ef18abdf4abed6a3) fix: delete static route should consider dualstack (#2130)
 * [9b38bf7f6](https://github.com/kubeovn/kube-ovn/commit/9b38bf7f632a84be334b5b4f5cf4d799d1d93e2f) build(deps): bump github.com/osrg/gobgp/v3 from 3.8.0 to 3.9.0 (#2121)
 * [f9f63cae4](https://github.com/kubeovn/kube-ovn/commit/f9f63cae481ddc1c76a2ac09a576616a4e22c613) build(deps): bump github.com/Wifx/gonetworkmanager from 0.4.0 to 0.5.0 (#2122)
 * [67b4dc1b9](https://github.com/kubeovn/kube-ovn/commit/67b4dc1b9933005cd64d30fcc2b052386f564fe7) build(deps): bump golang.org/x/time from 0.2.0 to 0.3.0 (#2120)
 * [78584b7cb](https://github.com/kubeovn/kube-ovn/commit/78584b7cb1b45973fb658fe2e5f36de97aeb4a6a) fix: vlan gw clean in 2 scene (#2117)
 * [b8e15e197](https://github.com/kubeovn/kube-ovn/commit/b8e15e1977a6e39afc3dd407120af9c5797c3c6b) optimize provider network (#2099)
 * [66e96b8e7](https://github.com/kubeovn/kube-ovn/commit/66e96b8e75d5b7b0a0a773a7c525e711fdb9c4fc) build(deps): bump golang.org/x/sys from 0.2.0 to 0.3.0 (#2119)
 * [625e31732](https://github.com/kubeovn/kube-ovn/commit/625e317323734b8179b9641df3316b149b3a3029) fix removing default static route in default vpc (#2116)
 * [141c4c355](https://github.com/kubeovn/kube-ovn/commit/141c4c3556e4753ea89c9fcb565e5ccffe557111) fix: eip deletion (#2118)
 * [86f75c835](https://github.com/kubeovn/kube-ovn/commit/86f75c835de89b86b9cc23c6267834b498c9411e) fix: ecmp route keep delete and recreate  (#2083)
 * [15fd547b3](https://github.com/kubeovn/kube-ovn/commit/15fd547b30ab4e09915130fccdef5e7f9cd7502a) fix policy route for subnets with logical gateway (#2108)
 * [c7549d41c](https://github.com/kubeovn/kube-ovn/commit/c7549d41c048ee5e1ebe68506432d1bfe3315789) build(deps): bump github.com/emicklei/go-restful/v3 from 3.9.0 to 3.10.1 (#2113)
 * [c42dae317](https://github.com/kubeovn/kube-ovn/commit/c42dae3176efc64604226bd608f6b0c6a4eb6108) refactor function name isIPAssignedToPod to isIPAssignedToOtherPod (#2096)
 * [c52f384ee](https://github.com/kubeovn/kube-ovn/commit/c52f384ee92a457307b87bdf5617f981b3d4aec0) build(deps): bump github.com/onsi/gomega from 1.24.0 to 1.24.1 (#2111)
 * [fc80d5922](https://github.com/kubeovn/kube-ovn/commit/fc80d592282c47220c7a26a0e5a52de211fdfb2b) fix: logical gw underlay gw subnet not clean (#2114)
 * [5862b0205](https://github.com/kubeovn/kube-ovn/commit/5862b0205bc229a3f3717690ffc7e9c3ffb79251) build(deps): bump github.com/osrg/gobgp/v3 from 3.6.0 to 3.8.0 (#2110)
 * [4b4bdb3c3](https://github.com/kubeovn/kube-ovn/commit/4b4bdb3c3d58a9c81aa25777fac7a905558fa827) build(deps): bump sigs.k8s.io/controller-runtime from 0.12.3 to 0.13.1 (#2109)
 * [684d1c757](https://github.com/kubeovn/kube-ovn/commit/684d1c757bfa67cc3ce20cb913f389049351d384) fix go mod (#2107)
 * [8ac8cc4e0](https://github.com/kubeovn/kube-ovn/commit/8ac8cc4e0535e814cb5e155b912e11e7a612689d) build(deps): bump github.com/onsi/ginkgo/v2 from 2.3.1 to 2.5.1 (#2103)
 * [12f2f404d](https://github.com/kubeovn/kube-ovn/commit/12f2f404d0fdd492eeac2860812793936bcce72a) build(deps): bump k8s.io/sample-controller from 0.24.4 to 0.25.4 (#2101)
 * [5caec7039](https://github.com/kubeovn/kube-ovn/commit/5caec7039d9b81c3f2e47342a415833dd733078a) build(deps): bump github.com/Microsoft/go-winio from 0.5.2 to 0.6.0 (#2104)
 * [e2eae04cb](https://github.com/kubeovn/kube-ovn/commit/e2eae04cba360a20d84d5ca07a30c5928e8936d7) build(deps): bump google.golang.org/grpc from 1.49.0 to 1.51.0 (#2102)
 * [8f4bf43ae](https://github.com/kubeovn/kube-ovn/commit/8f4bf43ae6ce347ba63b69ed40351294d56ed225) build(deps): bump github.com/Microsoft/hcsshim from 0.9.4 to 0.9.5 (#2100)
 * [47fe3eef8](https://github.com/kubeovn/kube-ovn/commit/47fe3eef820b4e0b8e5547abac39a8b29b221fb8) Create dependabot.yml
 * [5bed4af14](https://github.com/kubeovn/kube-ovn/commit/5bed4af148a13e3aa479cf0b62723a6a84bb7416) replace klog.Fatalf with klog.ErrorS and klog.FlushAndExit (#2093)
 * [2652bcfec](https://github.com/kubeovn/kube-ovn/commit/2652bcfec145a821df9b5677a758ea422dd4824a) fix: slow vip finalizer operation (#2092)
 * [4486e7fe7](https://github.com/kubeovn/kube-ovn/commit/4486e7fe74cb325e1e67d5303a8b4676eb317ea6) ko-trace: support ARP request/reply (#2046)
 * [a659f2e53](https://github.com/kubeovn/kube-ovn/commit/a659f2e53eaeec9783dd8fa5c6b32cc5eea99e31) fix: cni response missing sandbox field (#2089)
 * [88500fa5a](https://github.com/kubeovn/kube-ovn/commit/88500fa5ad96b9ac89180130339634310df43c6c) check if externalIds map is nil when add node as gw for centralized subnet (#2088)
 * [47d3872c1](https://github.com/kubeovn/kube-ovn/commit/47d3872c108f6b4c3c13cbf5ff9e4c7a8eb3eb9b) fix: del createIPS (#2087)
 * [d16163852](https://github.com/kubeovn/kube-ovn/commit/d161638529c7ffe15f0f875074a888c71f32b0e7) fix: add opts for ips del (#2079)
 * [4da9e4e5e](https://github.com/kubeovn/kube-ovn/commit/4da9e4e5e3bb28bcc1b7209c371335b21ba8ce6d) fix ovs bridge not deleted cause by port link not found (#2084)
 * [7344578e2](https://github.com/kubeovn/kube-ovn/commit/7344578e2105b4a0a3010c1439482079eaef1d23) fix libovsdb issues (#2070)
 * [9c292c006](https://github.com/kubeovn/kube-ovn/commit/9c292c006946f02d628161ac524f96c797065e3b) ipset: fix unknown ipset data attribute from kernel (#2086)
 * [def110817](https://github.com/kubeovn/kube-ovn/commit/def110817a7bdfb2842db851afba72a013f60fb7) fix: vpc lrp reset after restart kube-ovn-controller (#2074)
 * [0c668432d](https://github.com/kubeovn/kube-ovn/commit/0c668432dc926f48de9cf3f9617c4a20a7d63dd6) fix: add del bash for redundant ips (#2063)
 * [1c334c8d5](https://github.com/kubeovn/kube-ovn/commit/1c334c8d580addb749380dc521b2bb7095011fb5) refactor: add unknown config to logic switch port (#2067)
 * [419c385bb](https://github.com/kubeovn/kube-ovn/commit/419c385bb7e23939ca2b3daf4a319399118e699a) ovs-dpdk supports adding bond for multi-NICs (#2064)
 * [aef4cd3a6](https://github.com/kubeovn/kube-ovn/commit/aef4cd3a6324bfe4a65c02deb1d4331ab3aba9ed) fix OVN LS/LB gc (#2069)
 * [8aa724eba](https://github.com/kubeovn/kube-ovn/commit/8aa724ebae2583898cc8da6d4b05bf8b7e57c3de) fix: vip ipam not recover all (#2071)
 * [514b76664](https://github.com/kubeovn/kube-ovn/commit/514b7666431db4288094318b686449512fa97a47) bug-fix: make kind-reload invalid (#2068)
 * [657dbf60c](https://github.com/kubeovn/kube-ovn/commit/657dbf60cb5e05a617c5d87f55e3245afa1b45a4) remove no need params svcasname (#2057)
 * [1fcfbc423](https://github.com/kubeovn/kube-ovn/commit/1fcfbc423b015efb425a282e7d97758ba6498b93) Fix:hybrid-dpdk with vxlan tunnel mode，The OVS node does not create a VXLAN tunnel to the OVS-DPDK node (#2065)
 * [a7ed4429f](https://github.com/kubeovn/kube-ovn/commit/a7ed4429fde5a728f294ccd4f84b1b35561db054) update ipv6 address for vpc peer (#2060)
 * [db4fd6290](https://github.com/kubeovn/kube-ovn/commit/db4fd629032d6e4406ce3e017178f7321a601265) perf: reduce controller init time (#2054)
 * [34f426177](https://github.com/kubeovn/kube-ovn/commit/34f4261772f898a5a985f45e3df63789c881138e) reflactor note (#2053)
 * [b22e66ad7](https://github.com/kubeovn/kube-ovn/commit/b22e66ad7daaf32f9212db6a73a89ff2ef5a67f9) fix: replace replace with add to override existing route (#2052)
 * [c08741582](https://github.com/kubeovn/kube-ovn/commit/c08741582c82009f2fb7d94efe7ab49681c5980c) refactor Makefile (#1901)
 * [ea22c1ac0](https://github.com/kubeovn/kube-ovn/commit/ea22c1ac00b2f7b8c46b9bac49fbef49c878f1e3) pass klog verbosity to libovsdb (#2048)
 * [8a29023d2](https://github.com/kubeovn/kube-ovn/commit/8a29023d2b532be4df51a097227cdc05a449b5ee) ovs: fix reaching resubmit limit in underlay (#2038)
 * [db796b436](https://github.com/kubeovn/kube-ovn/commit/db796b4368bb75f1d95fa3a12c2164c1242716b2) sync crd yamls (#2040)
 * [f8f5a4c33](https://github.com/kubeovn/kube-ovn/commit/f8f5a4c33ae2049844ad413327286bbee89d330e) add helm and e2e test (#2020)
 * [5f40c2221](https://github.com/kubeovn/kube-ovn/commit/5f40c22210ce03ef38ec8fa7bfb4557c1449fae0) fix: add unitest (#2030)
 * [686110517](https://github.com/kubeovn/kube-ovn/commit/686110517e03ad63496127addebd9ea5b059c042) fix: pod not add finalizer after add iptables fip (#2041)
 * [75da1603a](https://github.com/kubeovn/kube-ovn/commit/75da1603afb8a51dd4fbb3c996eb2d5454d5b094) feat: ovn eip snat fip (#2029)
 * [730756056](https://github.com/kubeovn/kube-ovn/commit/73075605622d5f8558104ad380793fc82da2c74c) fix: vpc and vpc nat gw not clean (#2032)
 * [79a5ef340](https://github.com/kubeovn/kube-ovn/commit/79a5ef3405b6f5e9314db8716eba391355c9e2a5) update CHANGELOG.md
 * [e7fb20896](https://github.com/kubeovn/kube-ovn/commit/e7fb208965cff4344203c592255a6eb3186f0e80) fix pinger namespace error (#2034)
 * [4abd912f8](https://github.com/kubeovn/kube-ovn/commit/4abd912f875fd2c664b7e7ac69573089bc98d582) iptables: avoid duplicate logging (#2028)
 * [3fb645c9b](https://github.com/kubeovn/kube-ovn/commit/3fb645c9b9b16d16a473a4f7b9c2f3949881ec10) fix: gateway route should stay still when node is pingable (#2011)
 * [92b9c8c3c](https://github.com/kubeovn/kube-ovn/commit/92b9c8c3c9aadc4d1aeab3040b2c74e44725c8c7) update np name with character prefix (#2024)
 * [394978550](https://github.com/kubeovn/kube-ovn/commit/3949785505572474bf4750b622fadfda964c6885) bump kind and node image versions (#2023)
 * [56992c867](https://github.com/kubeovn/kube-ovn/commit/56992c867fb4b5ea47544ffb99ec56eb86253c4d) fix ovn nb/sb health check (#2019)
 * [bf93c458e](https://github.com/kubeovn/kube-ovn/commit/bf93c458ef3d66ebb17ffec3a39e7e3f515a72b8) fix ovs fdb for the local bridge port (#2014)
 * [578301543](https://github.com/kubeovn/kube-ovn/commit/578301543024e989e50f992524220a2f63bdb826) fix go version
 * [ad7cfe87f](https://github.com/kubeovn/kube-ovn/commit/ad7cfe87f5166dffdb097fcaa2ef769bd692b70f) perf: add debug info for perf trace (#2017)
 * [16a958363](https://github.com/kubeovn/kube-ovn/commit/16a9583639ddb0f1a7a602d838493163a518f8d6) fix: not append finalizer (#2012)
 * [688fd5e21](https://github.com/kubeovn/kube-ovn/commit/688fd5e2164651499582c1c79aea09955c51bbca) do not need to delete pg when update networkpolicy (#1959)
 * [c4d8a2f3e](https://github.com/kubeovn/kube-ovn/commit/c4d8a2f3ea26cd0bd16bddf53b2990fae9c39440) test: add test-server to collect packet lost during upgrade (#2010)
 * [f89908e79](https://github.com/kubeovn/kube-ovn/commit/f89908e7941b8edf656503f310aaac04bfd60870) support create iptables fip and eip automatically if pod enable fip  (#1993)
 * [ab80fd88b](https://github.com/kubeovn/kube-ovn/commit/ab80fd88b90935d9745210655fd5422b8a070c55) ci: upgrade deprecated actions (#2004)
 * [de5ef5116](https://github.com/kubeovn/kube-ovn/commit/de5ef5116c2f4388d9bbab32e537923776b54f43) fix: make ip deletion the same as creation (#2002)
 * [bfcc952c3](https://github.com/kubeovn/kube-ovn/commit/bfcc952c32acbfbd5018a0bac51e66bc7c27295e) fix: Add support for Mellanox NIC (#1999)
 * [f4c977f14](https://github.com/kubeovn/kube-ovn/commit/f4c977f1443b15ebe1da99ebb23e40fb813cab88) fix: nat gw not enqueue its resources (#1996)
 * [32f65f81d](https://github.com/kubeovn/kube-ovn/commit/32f65f81d46db912dd5f19ee877eb3aff6d9fcd9) fix: delete fiprule failed at first time (#1998)
 * [eaa936b38](https://github.com/kubeovn/kube-ovn/commit/eaa936b389104733395d87541fde62fab0cc0390) fix typo (#1994)
 * [dd3790ac1](https://github.com/kubeovn/kube-ovn/commit/dd3790ac1da2af5c09c65ed1fecb80c0d5a00a33) feat: now interface for containerd could be inspected (#1987)
 * [fee5bfd3d](https://github.com/kubeovn/kube-ovn/commit/fee5bfd3db394c65c96f3edb04810616e55aaf0f) fix: snat conntrack race (#1985)
 * [e1f7d72c1](https://github.com/kubeovn/kube-ovn/commit/e1f7d72c15b61b083590672745c79b5d8a2d903a) add check of write to ovn sb db for ovn-controller (#1989)
 * [892aa759d](https://github.com/kubeovn/kube-ovn/commit/892aa759d475586d6915278d0cd9343ca4c67c76) fix grep matching device in routes (#1986)
 * [113f62f6f](https://github.com/kubeovn/kube-ovn/commit/113f62f6fc332776c7aeee0b535ebb408adc04ee) delete pod after TerminationGracePeriodSeconds (#1984)
 * [87996f754](https://github.com/kubeovn/kube-ovn/commit/87996f75465e8c8ba1d7d5a7d5d908ec11e0033f) ovs: fix waiting flows in underlay networking (#1983)
 * [eea788869](https://github.com/kubeovn/kube-ovn/commit/eea7888699e3dae8790b0c931be61ab07094ed0e) feature: support default vpc use nat gw pod as cust vpc (#1979)
 * [3d2c7a59a](https://github.com/kubeovn/kube-ovn/commit/3d2c7a59a8cd2eff4f901fd18996a305c0523287) ovn db: recover automatically on startup if db corruption is detected (#1980)
 * [9ff3b9c05](https://github.com/kubeovn/kube-ovn/commit/9ff3b9c05983b7b88b8fb54a526ee197bd0326cf) fix: modify src route priority (#1973)
 * [57c75c1e2](https://github.com/kubeovn/kube-ovn/commit/57c75c1e2c328391014b8d8e690d79addc560dd4) upgrade ovs-ovn pod by generation version instead of chart version (#1960)
 * [b9e98e524](https://github.com/kubeovn/kube-ovn/commit/b9e98e52473a6bdec2cea27bfdf18f309f81943b) avoid concurrent subnet status update (#1976)
 * [ea854d460](https://github.com/kubeovn/kube-ovn/commit/ea854d460a106f6fc53ea34d429d2076530afaf0) fix metrics name (#1977)
 * [15f676f64](https://github.com/kubeovn/kube-ovn/commit/15f676f64b62c40e55ab756a7610830b595d740c) add vm pod to ipam by ip when initIPAM (#1974)
 * [afe06d819](https://github.com/kubeovn/kube-ovn/commit/afe06d819d159a2bde66343c58354979b37dece2) validate nbctl socket path in start-controller.sh (#1971)
 * [3796a5828](https://github.com/kubeovn/kube-ovn/commit/3796a5828f4f95205cdd745748fae2bf2c464b88) skip CVE-2022-3358 (#1972)
 * [80aab2ea0](https://github.com/kubeovn/kube-ovn/commit/80aab2ea046a62619c9a8956a42ba7526aeed516) fix version mismatch between the Ginkgo CLI and the imported package (#1967)
 * [b7863bdb1](https://github.com/kubeovn/kube-ovn/commit/b7863bdb1c6cee1d732930a8cd27389d3c018ca2) ovs: fix mac learning in environments with hairpin enabled (#1943)
 * [95939ca4d](https://github.com/kubeovn/kube-ovn/commit/95939ca4d9a652b151ea57320d7e67800f80a921) fix: add  default deny acl (#1935)
 * [de3d65c01](https://github.com/kubeovn/kube-ovn/commit/de3d65c01a1a4430590fe083e90494ce17425ecd) Fix registry for ovn-central container in install.sh (#1951)
 * [c8d22d2cf](https://github.com/kubeovn/kube-ovn/commit/c8d22d2cf9c0644035d6d5a0cf6b454ac5f59a3e) ovs: add fdb update logging (#1941)
 * [f1f6642b9](https://github.com/kubeovn/kube-ovn/commit/f1f6642b939202968abb427dae7ec05ca8498e1e) add chart version check when upgrade ovs-ovn pod (#1942)
 * [73fde2cef](https://github.com/kubeovn/kube-ovn/commit/73fde2cefd8c02d4bd440a582cfab633ac6cb575) fix underlay e2e testing (#1929)
 * [38956b6ca](https://github.com/kubeovn/kube-ovn/commit/38956b6cad4ec99a330d5abb3d16d520f76f802d) set leader flag when get leader (#1939)
 * [af6973fe3](https://github.com/kubeovn/kube-ovn/commit/af6973fe362cb9f6a47ca6b4f0726754c46247cf) set ovsdb-server vlog level to avoid warnings caused by ovs-vsctl (#1937)
 * [a3292078d](https://github.com/kubeovn/kube-ovn/commit/a3292078df22377509c8d480acde5934cc611a3f) fix: UpdateNatRule will error when logicalIP, externalIP is different protocol; replace : to \\: when IPv6 in ovs cli.
 * [76541ef12](https://github.com/kubeovn/kube-ovn/commit/76541ef12fb4b0dbc62060efd0fea5ffa08c0211) fix: noAllowLiveMigration port can't sync vips (#7)
 * [474206be5](https://github.com/kubeovn/kube-ovn/commit/474206be5e37c5271345f1e07638f673e4d40025) fix: add pod not update vip virtual port
 * [596741bcd](https://github.com/kubeovn/kube-ovn/commit/596741bcd31f8e04c374df6b2e08def8930773f0) fix: delete chassis (#1927)
 * [395a35549](https://github.com/kubeovn/kube-ovn/commit/395a35549a287aaabaad950c5ba3e9c5a5261bd5) fix: pod mistaken ls label (#1925)
 * [797100edf](https://github.com/kubeovn/kube-ovn/commit/797100edf42b2731a31fb2268ab82888a8077946) ignore pod without lsp when add pod to port-group (#1924)
 * [1a49e738f](https://github.com/kubeovn/kube-ovn/commit/1a49e738f214d96722181f69f0c0e9fc29bca0e4) add network partition check in ovn probes (#1923)
 * [16c0ed9fd](https://github.com/kubeovn/kube-ovn/commit/16c0ed9fdf5550f6a089a86cb7d873a60f190a2c) fix: fip unbind can't take effect immediately when conntrack record exists (#1922)
 * [606e6f62d](https://github.com/kubeovn/kube-ovn/commit/606e6f62d0b79d1fd5d30a37c0ae1fab45122b0c) No need to change deivceID to sriov_netdevice. (#1904)
 * [76dd9afa7](https://github.com/kubeovn/kube-ovn/commit/76dd9afa79e1c9c719f13dd815cd9e39b84b1917) update ns annotation when subnet cidr changed (#1921)
 * [8d1ce4206](https://github.com/kubeovn/kube-ovn/commit/8d1ce42066f076c98086446ef751c7f3923e35e7) fix EIP/SNAT on dynamic Pod annotation (#1918)
 * [4882c354e](https://github.com/kubeovn/kube-ovn/commit/4882c354e70513e568bc5029e2a59901e9e16498) fix: eip and nat crd can delete even if nat gw pod deleted and ipatab… (#1917)
 * [d8886d133](https://github.com/kubeovn/kube-ovn/commit/d8886d1334152e836b2e0129916d42a3663b541a) fix missing crd (#1909)
 * [8d2991e37](https://github.com/kubeovn/kube-ovn/commit/8d2991e371da857583d8a17a5858440e84e8b09a) Nat gw support toleration (#1907)
 * [b3bbe1d4f](https://github.com/kubeovn/kube-ovn/commit/b3bbe1d4f3fc8a0a5f5bc5222d097eca1e8e0174) Update USERS.md (#1908)
 * [dbe4ebb31](https://github.com/kubeovn/kube-ovn/commit/dbe4ebb31496d959768dd6d451939b11f831856a) fix typo (#1897)
 * [8d15497a8](https://github.com/kubeovn/kube-ovn/commit/8d15497a8f66e2acf4ea64edd4c3ff91db22ef13) fix: Make the /sys directory in ovs-ovn-dpdk pod writable (#1899)
 * [5befab467](https://github.com/kubeovn/kube-ovn/commit/5befab467404775e1f10948ce0979df792b19ac2) fix: failed to add eip (#1898)
 * [7fae28aec](https://github.com/kubeovn/kube-ovn/commit/7fae28aecf7119f8ed3f777defac7294f699be4c) fix: gatewaynode might be null (#1896)
 * [57edfe41d](https://github.com/kubeovn/kube-ovn/commit/57edfe41d82ef00d5f58e3e2755f2a5f300f3dd7) ci: increase golangci-lint timeout (#1894)
 * [07f12c2df](https://github.com/kubeovn/kube-ovn/commit/07f12c2df1ce79b18143ae9a9e47de65685c319e) update Go to version 1.19 (#1892)
 * [e5878ff9f](https://github.com/kubeovn/kube-ovn/commit/e5878ff9f00887d37a1fb51dc9f5b14ab38e6f87) fix: api rollback (#1895)
 * [82db50fe7](https://github.com/kubeovn/kube-ovn/commit/82db50fe71bf6e9d40bd9bf033615ee939675297) ci: use concurrency to ensure that only a single workflow (#1850)
 * [83b867ab5](https://github.com/kubeovn/kube-ovn/commit/83b867ab54a9e4d6165d0c71bd510cac7d2feabe) kubectl-ko: turn off pipefail for ovn leader check (#1891)
 * [959a64dca](https://github.com/kubeovn/kube-ovn/commit/959a64dcab1822fb7b206f3b55ec26492ee7e80a) kubectl-ko: fix trace for KubeVirt VM (#1802)
 * [10fd33304](https://github.com/kubeovn/kube-ovn/commit/10fd33304ff3e04b27f2ccc72b50207c900035e4) fix duplicate logs for leader election (#1886)
 * [13ebb855d](https://github.com/kubeovn/kube-ovn/commit/13ebb855dd8494af6e63ce9eda4bc0f25cec4453) fix setting ether dst addr for dnat (#1881)
 * [b14850356](https://github.com/kubeovn/kube-ovn/commit/b1485035635ea2812a45e5d4e6ec54fe9864b590) change the prtocol string to const (#1887)
 * [7e56931f3](https://github.com/kubeovn/kube-ovn/commit/7e56931f36df58281cdf887529c6cd99e18c8aa3) refactor iptables rules (#1868)
 * [14898dd3c](https://github.com/kubeovn/kube-ovn/commit/14898dd3c70025c149a00dcdda293add16bf1a9d) cni should handler unmont volume, when delete pod. (#1873)
 * [031ed0317](https://github.com/kubeovn/kube-ovn/commit/031ed0317e595c80dce6cd4eb3a27487041e0543) delete and recreate netem qos when update process (#1872)
 * [dedd5aaab](https://github.com/kubeovn/kube-ovn/commit/dedd5aaabd1e95daf8a20ba73b0dac26384e346c) feat: check configuration (#1832)
 * [97b41127e](https://github.com/kubeovn/kube-ovn/commit/97b41127e14c9b9df5ae930f73f3ded81f49bd25) security: conform to gosec G114 (#1860)
 * [2dfb6e72a](https://github.com/kubeovn/kube-ovn/commit/2dfb6e72ace6ccf7fb6e78857cdf22954b1946ec) update CHANGELOG.md
 * [b6450b2f2](https://github.com/kubeovn/kube-ovn/commit/b6450b2f27fc80b27c61116085053f26cd7cdccd) e2e: add timeout for waiting resources to be ready (#1871)
 * [e1656752a](https://github.com/kubeovn/kube-ovn/commit/e1656752a12e43674dbef73056384f1a2995eecc) upgrade to Ginkgo v2 (#1861)
 * [0adecb0cb](https://github.com/kubeovn/kube-ovn/commit/0adecb0cb7217e77210b6afcf8b7bab530ae98b5) feat: reduce downtime by increasing arp cache timeout
 * [c24f678b8](https://github.com/kubeovn/kube-ovn/commit/c24f678b8b63907af3b5961674376229da38b56e) feat: reduce wait time by counting the flow num.
 * [05611aa75](https://github.com/kubeovn/kube-ovn/commit/05611aa758de93adba97b09c539438f790cd908f) fix: missing stop_ovn_daemon args
 * [6ab837c4e](https://github.com/kubeovn/kube-ovn/commit/6ab837c4e70da188e4133690177eb1d4b21ec5cf) fix: nat gw pod should set default gw to net1 so that to access public (#1864)
 * [b08765dc0](https://github.com/kubeovn/kube-ovn/commit/b08765dc092210d7dc72cb3b284df071dd2e5ebd) delete log severity for drop acl when update networkpolicy (#1863)
 * [e13c4ef13](https://github.com/kubeovn/kube-ovn/commit/e13c4ef13df25f092afd5a3bf0484931013444bf) ovs: fix log file descriptor leak in monitor process (#1855)
 * [8a235e9e8](https://github.com/kubeovn/kube-ovn/commit/8a235e9e8dc84adbd89aaca978c30ce37328c47b) fix: dnat port not use whole words to check (#1854)
 * [34e02ebb5](https://github.com/kubeovn/kube-ovn/commit/34e02ebb545908fd59e879fac15f91dfafa855d9) fix ovs-ovn logging (#1848)
 * [2a2a32f99](https://github.com/kubeovn/kube-ovn/commit/2a2a32f99ea591a59cbb737852870b47c72e84ac) fix ovn dhcp not work with ovs-dpdk (#1853)
 * [44e412509](https://github.com/kubeovn/kube-ovn/commit/44e4125093672f0923efd10069d85c90a9e5383d) docs: Update USERS.md (#1851)
 * [51f491f27](https://github.com/kubeovn/kube-ovn/commit/51f491f272914a5afa70c068cdf65bf1433320c6) fix: multus macvlan ipvlan use kube-ovn ipam，but  ip not inited in init-ipam (#1843)
 * [8ef6c01cf](https://github.com/kubeovn/kube-ovn/commit/8ef6c01cfb47bdc81c8e3f51a2b4e7420181e36c) fix underlay e2e (#1828)
 * [c33276fd5](https://github.com/kubeovn/kube-ovn/commit/c33276fd5ec83243b79417a0ad796c8473fdc48a) fix arping error log (#1841)
 * [69cf5ca59](https://github.com/kubeovn/kube-ovn/commit/69cf5ca5991945cbc8c015c1c562c88bdbaf73be) ko: fix kube-proxy check (#1842)
 * [5012ff3ef](https://github.com/kubeovn/kube-ovn/commit/5012ff3efc34b759c9852ee9b45e6de3979e8add) base: use patch from OVN upstream (#1844)
 * [07773cb08](https://github.com/kubeovn/kube-ovn/commit/07773cb08212cc6d5cc2556301317c5400fed977) ci: switch environment to ubuntu-20.04 (#1838)
 * [eded55166](https://github.com/kubeovn/kube-ovn/commit/eded55166f6d13c47e2c3563ba10c0dfed4eb739) ci: split image builds to speed up jobs (#1807)
 * [656bd46ca](https://github.com/kubeovn/kube-ovn/commit/656bd46ca595bfc480f24ca575bd564709596291) ci: update Go cache to speed up jobs (#1829)
 * [57d74bff3](https://github.com/kubeovn/kube-ovn/commit/57d74bff3bc847db36473ca2f8ce64cfc1a27b87) windows: fix ovs/ovn versions and patches (#1830)
 * [babd80219](https://github.com/kubeovn/kube-ovn/commit/babd802194c97d877d1479236c722c4bbc33a861) 修改 ovs-ovn-dpdk 容器镜像编译打包，解决容器中 ovs 运行不正常：无法添加物理网卡，无法创建 vhostuserclient port 问题 (#1831)
 * [0ed5c924c](https://github.com/kubeovn/kube-ovn/commit/0ed5c924c5bd8a78ff91f2ffb4a134be7b69b76b) support adding routes in underlay Pods for access to overlay Pods (#1762)
 * [013549ab9](https://github.com/kubeovn/kube-ovn/commit/013549ab94ee5e4e74d0f6c9f88af81812d2bc70) update centralized subnet gateway ready patch operation (#1827)
 * [9937ef876](https://github.com/kubeovn/kube-ovn/commit/9937ef8764e24a89b7c39ad8855315f3e7ca0521) remove pod security policy (#1822)
 * [725957a7b](https://github.com/kubeovn/kube-ovn/commit/725957a7ba9c46a415b0cc21da7d90d9c2c003cb) fix duplicate log for tunnel interface decision (#1823)
 * [2e64133ce](https://github.com/kubeovn/kube-ovn/commit/2e64133ce186763d4c5c1f09566a2811da9cd606) update ovs/ovn version to fix hardware offload (#1821)
 * [9c87d9d13](https://github.com/kubeovn/kube-ovn/commit/9c87d9d136f86a5834a9408f763d374ee542a7d8) fix: use full longest word to match full ip about dnat (#1825)
 * [385064a0c](https://github.com/kubeovn/kube-ovn/commit/385064a0ca1e2927ee71c76afb97e8e1c9d0627a) update centralize subnet gatewayNode until gw is ready (#1814)
 * [4944db750](https://github.com/kubeovn/kube-ovn/commit/4944db750ec6981406a34485254a66e63d03adde) initialize IPAM from IP CR with empty PodType for sts Pods (#1812)
 * [d41c043d8](https://github.com/kubeovn/kube-ovn/commit/d41c043d825cd85990f36e5f0bc9587030bd82db) feat: add editable ovn-ic (#1795)
 * [ddcdfb9ee](https://github.com/kubeovn/kube-ovn/commit/ddcdfb9eedbaca142160be947e70aaefb5932849) kubectl-ko: fix missing env-check (#1804)
 * [8b2df5880](https://github.com/kubeovn/kube-ovn/commit/8b2df588095dac7db6838b6bd5abb348d72125bb) kubectl-ko: fix destination mac (#1801)
 * [e8816e964](https://github.com/kubeovn/kube-ovn/commit/e8816e964a6cb7794973b3b9e0ccc833367bc3e8) fix cilium e2e (#1759)
 * [bc3804151](https://github.com/kubeovn/kube-ovn/commit/bc380415163b972a7bfa4f505e472b1c0ebd376e) abort kube-ovn-controller on leader change (#1797)
 * [26e77eadf](https://github.com/kubeovn/kube-ovn/commit/26e77eadfb93e49185769999f57edf734ddc2d18) avoid invalid ovn-nbctl daemon socket path (#1799)
 * [e7064062b](https://github.com/kubeovn/kube-ovn/commit/e7064062b7eb319c37361bb9f27a5866b6de9f41) update CHANGELOG.md
 * [70f1b141c](https://github.com/kubeovn/kube-ovn/commit/70f1b141ca7f5c58e64a46c9dd8f89a0e1d82df8) Perf/memleak (#1791)
 * [225da25e7](https://github.com/kubeovn/kube-ovn/commit/225da25e746b6890e708c17891b291327dabac01) delete htb qos when releated annotation is deleted (#1788)
 * [8db05a1fa](https://github.com/kubeovn/kube-ovn/commit/8db05a1fa600c468b395020d1bf27589a9fde126) Fix nag gw gc (#1783)
 * [277f6f697](https://github.com/kubeovn/kube-ovn/commit/277f6f697708cfad21625f3dad25eac09134f54c) fix iptables for services with external traffic policy set to Local (#1773)
 * [42812b92e](https://github.com/kubeovn/kube-ovn/commit/42812b92ef1766de0d5d80db3935231d277be052) perf: reduce metrics labels (#1784)
 * [9ffb9d229](https://github.com/kubeovn/kube-ovn/commit/9ffb9d229e8e4cbbe5055965c8a649522228bb36) northd: remove lookup_arp_ip actions (#1780)
 * [9efd4bb2f](https://github.com/kubeovn/kube-ovn/commit/9efd4bb2f88760b1fb16f53a248efe85ef888620) fix: 5ms is too short for eip and nats creation (#1781)
 * [80425b7cf](https://github.com/kubeovn/kube-ovn/commit/80425b7cf8649f35486aa5eaee27069c55512fe7) Lb-svc supports custom VPCs (#1779)
 * [cd00ddb64](https://github.com/kubeovn/kube-ovn/commit/cd00ddb642a799fb896f756d3bb56fc06b7bd3b1) fix ovnic e2e (#1763)
 * [51bf142fc](https://github.com/kubeovn/kube-ovn/commit/51bf142fcadd934be2f9b648185db1cf0b0eece6) fix iptables for service traffic when external traffic policy set to local (#1728)
 * [916600f6d](https://github.com/kubeovn/kube-ovn/commit/916600f6dd94236373807f0f553697c42d9acdd2) set sysctl variables on cni server startup (#1758)
 * [c10c71184](https://github.com/kubeovn/kube-ovn/commit/c10c71184f5afbfc53f36017d411c89cd2f05971) fix: add omitempty to subnet spec (#1765)
 * [2dd46e693](https://github.com/kubeovn/kube-ovn/commit/2dd46e6931a726e1eec85c3234d0e7a81adf9deb) perf: replace jemalloc to reduce memory usage (#1764)
 * [35513157e](https://github.com/kubeovn/kube-ovn/commit/35513157e44b6e9de65f12b29a444ecadef65c70) avoid patch interface deletion & recreation during restart (#1741)
 * [e7ce68bbf](https://github.com/kubeovn/kube-ovn/commit/e7ce68bbf710f0780e82f01d98b9c93b21755983) feature: support exchange link names of OVS bridge and provider nic in underlay networks (#1736)
 * [5254731f3](https://github.com/kubeovn/kube-ovn/commit/5254731f3e86e24aa764d3e09226b44f91431f03) dpdk-v2 ，--with-hybrid-dpdk 修改 Dockerfile.base-dpdk 解决 编译安装 ovs-dpdk 正常运行 (#1754)
 * [3717c6cb6](https://github.com/kubeovn/kube-ovn/commit/3717c6cb620a9e9e2cec97b2a95831ad321982e0) fix: Adjust order for Log and output err when get NatRule faild. (#1751)
 * [f7358337d](https://github.com/kubeovn/kube-ovn/commit/f7358337d4d0ff8643bf2221efabfb4f6068bd1f) only support IPv4 snat in vpc-nat-gw when internal subnet is dual (#1747)
 * [99c53d296](https://github.com/kubeovn/kube-ovn/commit/99c53d2965317a9968df15dcb3151a7587bad81a) update README.md
 * [7c4293ebd](https://github.com/kubeovn/kube-ovn/commit/7c4293ebd29ce00ae9d4dcff6aed963c08c58539) docs: update USERS.md (#1743)
 * [a8ed4bce6](https://github.com/kubeovn/kube-ovn/commit/a8ed4bce6a8acceabf6565edabc056ff69f7ef7a) style: import group ordering. (#1742)
 * [bcafb10c8](https://github.com/kubeovn/kube-ovn/commit/bcafb10c89c02a52107e2a909960821ace7b2b46) enqueue subnets after vpc update (#1722)
 * [3af851624](https://github.com/kubeovn/kube-ovn/commit/3af8516249d45d002ba941447e133bb592a5c122) do not add subnet not processed by kube-ovn to vpc (#1735)
 * [7ac7b5927](https://github.com/kubeovn/kube-ovn/commit/7ac7b59272ed4d42df79d1b7dbc9a98bbd96b9c5) dpdk-v2 ，--with-hybrid-dpdk qemu 创建 sock 权限问题 (#1739)
 * [96683cb13](https://github.com/kubeovn/kube-ovn/commit/96683cb1397584a675ed47ae2abe7301156888d9) fix: const extGw may expired, after subnet updated, so use ipam subne… (#1730)
 * [861cf05d4](https://github.com/kubeovn/kube-ovn/commit/861cf05d4bcd33bdebcf5abd39ded6925b9f0110) fix service not working when a node's IPv6 address is before the IPv4 address (#1724)
 * [daaddba2f](https://github.com/kubeovn/kube-ovn/commit/daaddba2f6e16de31a1755dad59a3b85fa0c41f9) update pr template
 * [ce40f7ed0](https://github.com/kubeovn/kube-ovn/commit/ce40f7ed000da1ddfbb75e297eaec7dd9f48b99c) fix: If pod has snat or eip, also need delete staticRoute when delete pod. (#1731)
 * [3ff586ecd](https://github.com/kubeovn/kube-ovn/commit/3ff586ecd80fb984b2361711b4b69856f0ca3360) optimize lrp create for subnet in vpc (#1712)
 * [f582a11bd](https://github.com/kubeovn/kube-ovn/commit/f582a11bd2b0d79c16a1356e47ac27b1717b759c) fix: cancel delete staticroute when it's used by NatRule (#1733)
 * [3c7588bf4](https://github.com/kubeovn/kube-ovn/commit/3c7588bf44008b92ea7313f100b8d6f3dad1f0e1) fix: wrong info when update subnet from dual to ipv4 or ipv6. (#1726)
 * [d1b291ed6](https://github.com/kubeovn/kube-ovn/commit/d1b291ed659b079b0b7fd5e39f8cb7b60f4a43db) change: add newline at end of file (#1717)
 * [26260c917](https://github.com/kubeovn/kube-ovn/commit/26260c91722026c2cdb11f8ea79c4eb15f9672cc) add kernel prerequisite for Rocky Linux 8.6 (#1713)
 * [2fd8e41ee](https://github.com/kubeovn/kube-ovn/commit/2fd8e41eea42945151892aaf94780ed5524ecd4a) Add CODE_STYLE.md (#1711)
 * [3b9111f90](https://github.com/kubeovn/kube-ovn/commit/3b9111f90476bf9cd2fbc436f0716b783bb042c1) Change system-cluster-critical to system-node-critical to prevent pods of DaemonSet type from being (#1709)
 * [e8877f1d7](https://github.com/kubeovn/kube-ovn/commit/e8877f1d7e9c7bc1009ebfe10748c0b70ba568e1) Develop custom vpc-dns (#1662)
 * [ac01a603e](https://github.com/kubeovn/kube-ovn/commit/ac01a603e65eb2655ef8bb61b1c9ac0b91cfcd23) fix CVE-2022-30065 (#1710)
 * [4b13888d0](https://github.com/kubeovn/kube-ovn/commit/4b13888d053d94cd869cd7f115e0cd3ee0233775) fix: add and set ENABLE_KEEP_VM_IP=true to keep vm ip (#1702)
 * [fe18db301](https://github.com/kubeovn/kube-ovn/commit/fe18db301f0281e28c27bd7b6ecd3f427c115337) update CHANGELOG.md
 * [1ab550563](https://github.com/kubeovn/kube-ovn/commit/1ab5505631a53634f1810fd8fa2d100e9797518c) fix overlay MTU in vxlan/stt tunnels (#1693)
 * [c9d9923ef](https://github.com/kubeovn/kube-ovn/commit/c9d9923ef83c1179f54d1d3fbf60f70cdd53065a) fix: response has no gw when create nic without default route (#1703)
 * [b52655fc7](https://github.com/kubeovn/kube-ovn/commit/b52655fc7c202707b881248f2263e7a4a580dbe4) add note in install.sh for install --with-hybrid-dpdk(dpdk-v2). (#1699)
 * [4530a435c](https://github.com/kubeovn/kube-ovn/commit/4530a435c9acad95c0cf8bb5d8dd353bc01dfe02) ignore ovsdb-server/compact error: not storing a duplicate snapshot (#1691)
 * [c4e91cbdf](https://github.com/kubeovn/kube-ovn/commit/c4e91cbdfbaa4c15168a71060bdc7655b7c89671) Get latest vpc data from apiserver instead of cache (#1684)
 * [45bc2f7e9](https://github.com/kubeovn/kube-ovn/commit/45bc2f7e9bfda18b4303b09e9099f2e96f486d48) support kubernetes v1.24 (#1553)
 * [48d914e79](https://github.com/kubeovn/kube-ovn/commit/48d914e7961a3dda8679222ac7addc09b0aec437) update priority range in htb qos (#1688)
 * [41bdcd05c](https://github.com/kubeovn/kube-ovn/commit/41bdcd05c6d95e73d4679490f767c88aec50763b) fix: clean vip eip snat dant fip in cleanup.sh (#1690)
 * [c2cea8850](https://github.com/kubeovn/kube-ovn/commit/c2cea8850d2b9cfd7c136f68120bc139c0a54fa3) update: add Bingbing Zhang into MAINTAINERS (#1687)
 * [a38dbb5d4](https://github.com/kubeovn/kube-ovn/commit/a38dbb5d4d214dd58b89e1eb83fee1dbdcb7aa13) fix: move away words that is considered offensive after k8s v1.20.0 (#1682)
 * [76edad080](https://github.com/kubeovn/kube-ovn/commit/76edad0800b72b2fe3d4bd3eb342ac2123c76f03) update CHANGELOG.md
 * [e4575c882](https://github.com/kubeovn/kube-ovn/commit/e4575c882d34d3d32f0c6c1c607358d8a37d8d9c) add upgrade-ovs script (#1681)
 * [b3c32210d](https://github.com/kubeovn/kube-ovn/commit/b3c32210d5936c45a8c3443b68168ec77cd52ae4) fix: change ovn-ic static route to policy (#1670)
 * [db3b9f7f1](https://github.com/kubeovn/kube-ovn/commit/db3b9f7f1f23f1f6aa7e71dbb4355703d596aa90) Delete pod if subnet of the pod's owner(sts/vm) updated (#1678)
 * [533859fea](https://github.com/kubeovn/kube-ovn/commit/533859fea55b7c62ce46c609f2183ee06d4bf08b) Develop switch lb rule (#1656)
 * [8cb6e36d5](https://github.com/kubeovn/kube-ovn/commit/8cb6e36d5eca4eb6ae1603093ebb1ec76fb181f4) do not snat packets only for subnets with distributed gateway when external traffic policy is set to local (#1616)
 * [86ecfd2d7](https://github.com/kubeovn/kube-ovn/commit/86ecfd2d7586030d0cfb378d4cc4e682a5752163) refactor: extract external routes from eip func, make it the same as … (#1671)
 * [62ddfda6d](https://github.com/kubeovn/kube-ovn/commit/62ddfda6dbeeb708b21f14231203674672ad0052) add loadbalancer service (#1611)
 * [b174b4f45](https://github.com/kubeovn/kube-ovn/commit/b174b4f455ea4a73da38c06d0d2b5963b1bf39f4) bgp: consolidate service check and use service const (#1674)
 * [24786f486](https://github.com/kubeovn/kube-ovn/commit/24786f486ce92efd733fb29e0653b79c820d9d03) security: disable pprof by default (#1672)
 * [3191d8c88](https://github.com/kubeovn/kube-ovn/commit/3191d8c8817a28ead49be803b8f7ac6164cb4f3c) fix bgp: sync service cache (#1673)
 * [f12266827](https://github.com/kubeovn/kube-ovn/commit/f12266827277518de2480e5df22a3dcb6c0b5c14) fix iptables for direct routing (#1578)
 * [2b615f33a](https://github.com/kubeovn/kube-ovn/commit/2b615f33a7f9da403c9a83564b5a8719e28a0b60) feature: support pod use static vip (#1650)
 * [d69024850](https://github.com/kubeovn/kube-ovn/commit/d690248504b7481e1ee7096df6ab24b9e5fb5cb6)  fix: kubectl-ko does't work when ovn-nb, ovn-sb and ovn-northd master slave Switchover (#1669)
 * [b4f890108](https://github.com/kubeovn/kube-ovn/commit/b4f8901089845bb6a63472efe790901ba7991bc0) mount modules for auto load ip6tables moudles (#1665)
 * [72fccd7e2](https://github.com/kubeovn/kube-ovn/commit/72fccd7e23b3745912ae182aa35066dfdad508c2) update docs links
 * [bcec46a3c](https://github.com/kubeovn/kube-ovn/commit/bcec46a3c7c21342831b966d7c81869ae483400b) fix: subnet failed when create without protocol (#1653)
 * [eddce7597](https://github.com/kubeovn/kube-ovn/commit/eddce7597410dabea5c37ebda2399c439c8e64e3) ignore pod not scheduled when reconcile subnet (#1666)
 * [a158f8d6b](https://github.com/kubeovn/kube-ovn/commit/a158f8d6bca4536ba98617c7f92fbc4add756a29) fix libovsdb (#1664)
 * [200c53176](https://github.com/kubeovn/kube-ovn/commit/200c53176770ea0e3a11f3a052a2d7baac68dcbc) fix ovs-ovn not running on newly added nodes (#1661)
 * [4616285c6](https://github.com/kubeovn/kube-ovn/commit/4616285c6161be8790c4af6bbaa9b73645ea6ad8) fix get security group name by external_ids (#1663)
 * [993ae20c8](https://github.com/kubeovn/kube-ovn/commit/993ae20c8c6c5a85be9a09a00a3917e4c439ce2c) fix:can not delete pod with sriov vf (#1654)
 * [9a180f59e](https://github.com/kubeovn/kube-ovn/commit/9a180f59e0fb976931e9e24cfeb6042f962e64bc) add policy route when add subnet (#1655)
 * [a93f211fd](https://github.com/kubeovn/kube-ovn/commit/a93f211fd48677029bac54c83a56446cabaadb66) update CHANGELOG.md
 * [cf1c2017f](https://github.com/kubeovn/kube-ovn/commit/cf1c2017f759ce5eea9fbfdd66750647d3a01f47) fix: no need routed when use v1.multus-cni.io/default-network (#1652)
 * [bcfbe8c6a](https://github.com/kubeovn/kube-ovn/commit/bcfbe8c6a31f0c4db7fc26ea7704e894ca3878b6) docs: add GOVERNANCE.md and SECURITY.md
 * [c2b9eeb4e](https://github.com/kubeovn/kube-ovn/commit/c2b9eeb4ea7b40776b4082d80f7ada210119ac32) fix: should go on check ip after occupied ip (#1649)
 * [9d0cefb5b](https://github.com/kubeovn/kube-ovn/commit/9d0cefb5b3205275a2dbc40fd1c31aed3ebbd829) set ether dst addr for dnat on logical switch (#1512)
 * [a9d5e50d5](https://github.com/kubeovn/kube-ovn/commit/a9d5e50d59f7c63681b170e756dd3fbcdf8d9444) docs: update README.md
 * [d71d1f696](https://github.com/kubeovn/kube-ovn/commit/d71d1f696423c31adc9de0dc4956862df459250d) CI: delete resources in order to avoid a long time waiting for subnet deletions. (#1643)
 * [fe1ebe060](https://github.com/kubeovn/kube-ovn/commit/fe1ebe060a38c2c4400d50bd067854a0230e7c72) ci: fix golangci-lint (#1639)
 * [89514c9c8](https://github.com/kubeovn/kube-ovn/commit/89514c9c834ac86f8a56f183bef70e6425e4aef5) Update install.sh (#1645)
 * [4c8c0a395](https://github.com/kubeovn/kube-ovn/commit/4c8c0a395e05c40a304e8aa99d260206d075dc77) fix: make sure pod annotation switch is the first choice to allocate ip,  and fix vpc nat sts not delete (#1640)
 * [ae8bc1b4c](https://github.com/kubeovn/kube-ovn/commit/ae8bc1b4ca7874e2341e4f0f8eaa4adc587f9c25) docs: update docs link
 * [0f91a61e5](https://github.com/kubeovn/kube-ovn/commit/0f91a61e52d182d455452d5a93ecc4f1e7d6f71c) set networkpolicy log default to false (#1633)
 * [5c857350b](https://github.com/kubeovn/kube-ovn/commit/5c857350b35e3bc92d0b9a804a697ad4fe45ab96) update policy route when join subnet cidr changed (#1638)
 * [9ac8797b3](https://github.com/kubeovn/kube-ovn/commit/9ac8797b327ff64b8f7b9c88c407f424985bf087) fix: diskfull may lead to wrong raft status for ovs db (#1635)
 * [011eba28e](https://github.com/kubeovn/kube-ovn/commit/011eba28ec7ad0760336a65dbf83e7137b209eed) ci: update trivy options (#1637)
 * [3d82780ea](https://github.com/kubeovn/kube-ovn/commit/3d82780ea10f329b14c2e5f6de086697c4fa1b4d) fix no interface report to multus cni, missing in k8s.v1.cni.cncf.io/network[s]-status (#1636)
 * [c953b8f37](https://github.com/kubeovn/kube-ovn/commit/c953b8f37a29f152808ac6ccd6e2a26b709af434) change vp gw pod workload from deployment to statefulset (#1630)
 * [6238695f7](https://github.com/kubeovn/kube-ovn/commit/6238695f74692d1a2fad56485053542e1319a678) increase initial delay of ovs-ovn liveness probe (#1634)
 * [de99e8260](https://github.com/kubeovn/kube-ovn/commit/de99e8260b8f487888299c5d6f366e8541344ad7) fix: cleanup should ignore patch failed (#1626)
 * [8c946a4c0](https://github.com/kubeovn/kube-ovn/commit/8c946a4c01c2addaa472cee6576fe9f4e9a0a889) delete "allow" policy route on subnet deletion (#1628)
 * [75ece0e06](https://github.com/kubeovn/kube-ovn/commit/75ece0e06a719c79440d3b87fe65a8955e0b9453) wait ovn-central pods running before delete ovs-ovn pods (#1627)
 * [f55e32b3e](https://github.com/kubeovn/kube-ovn/commit/f55e32b3eb4f6bdf9cd030bfd2a63011fe19b65d) vip, eip support ipv6 vip count (#1624)
 * [b458bf78c](https://github.com/kubeovn/kube-ovn/commit/b458bf78c733a9089c70c40b5b675e6991c46977) ci: auto changelog now (#1625)
 * [fce2cf172](https://github.com/kubeovn/kube-ovn/commit/fce2cf172f456b21551f4e3fa4110d87a40c3ec6) get dbstatus for all ovn-central pod (#1619)
 * [be66af4be](https://github.com/kubeovn/kube-ovn/commit/be66af4be1fb011ef6da6b8ed71847e08794ed3e) refactor: use ConfigMap resourceVersion to check if ovn-vpc-nat-gw-config changed (#1617)
 * [c54cafa5d](https://github.com/kubeovn/kube-ovn/commit/c54cafa5d4a89ab89c6b2b25500e0a8cc212235e) fix controller exit before process pod update event (#1621)
 * [0ac7e7b98](https://github.com/kubeovn/kube-ovn/commit/0ac7e7b98c9b18cb466957e34c8f28ca9675f19b) docs: update ROADMAP.md
 * [d6915bd64](https://github.com/kubeovn/kube-ovn/commit/d6915bd643b41349b38ef8b7352a9f66e7e3460e) fix acl log name too long (exceed 63) (#1612)
 * [0b840398c](https://github.com/kubeovn/kube-ovn/commit/0b840398c7371aa53a1c05cb0da5f53248fe50dd) docs: Add High-level design of ovn-spekaer (#1609)
 * [7ebf36ddb](https://github.com/kubeovn/kube-ovn/commit/7ebf36ddbe9858a6483225f44b51ebc5dac20fe9) docs: Fix allowed subnets (#1610)
 * [b2f65cd13](https://github.com/kubeovn/kube-ovn/commit/b2f65cd132a07328853f24507f24ecf37dafd617) add cni log Prevent "for loop time" approximately health check time (#1606)
 * [71af95319](https://github.com/kubeovn/kube-ovn/commit/71af95319bffd86e4844c24b76769239efaee3f4) docs:Add Usage of ovn-speaker for passivemode and ebgp-multihop (#1605)
 * [aa456623b](https://github.com/kubeovn/kube-ovn/commit/aa456623b645e5cb75250c4c537da2c3982df246) update static ip docs (#1607)
 * [1400d5b5c](https://github.com/kubeovn/kube-ovn/commit/1400d5b5cf2cfc7a6ff05d9b90225c5506123b6e) Modify the next hop calculation method for kube-ovn-speaker (#1604)
 * [bc86ec86a](https://github.com/kubeovn/kube-ovn/commit/bc86ec86a966d3786424c2ad45637e534f3b382c) fix static ip error in dual stack (#1598)
 * [7fdb4bc68](https://github.com/kubeovn/kube-ovn/commit/7fdb4bc68f207468035e4ef38bff091f561e5869) ci: build amd64 images without avx512 (#1584)
 * [b7453796e](https://github.com/kubeovn/kube-ovn/commit/b7453796e9933f51df23c32bfe60f50b675bc7b5) Add ebgp-multihop function for kube-ovn-speaker (#1601)
 * [6dd2f0ae4](https://github.com/kubeovn/kube-ovn/commit/6dd2f0ae44e4485ca4e2a9bb32502f88de347a1b) monitor dns in cilium e2e (#1597)
 * [75cfe4147](https://github.com/kubeovn/kube-ovn/commit/75cfe4147b40f525a8f3087f3e3a3459bb79937d) Add passivemode for kube-ovn-speaker (#1600)
 * [05bddc6ae](https://github.com/kubeovn/kube-ovn/commit/05bddc6aeed819fa7bc9d2ac202d038d4e457c15) Bump github.com/emicklei/go-restful/v3 from 3.7.4 to 3.8.0 (#1599)
 * [5d54ade92](https://github.com/kubeovn/kube-ovn/commit/5d54ade92bca16eddc1d84c699d5f02a38f0c0cc) docs: fix the kind name (#1593)
 * [4053d46b2](https://github.com/kubeovn/kube-ovn/commit/4053d46b2d9c00edcdc873ad7f4384145149d1bb) Support CNI VESION command (#1596)
 * [34dc29d6b](https://github.com/kubeovn/kube-ovn/commit/34dc29d6b40923370a1725340593ef7530cd0162) update ovs health check, delete connection to ovn sb db (#1588)
 * [d891a84b5](https://github.com/kubeovn/kube-ovn/commit/d891a84b5fee73df91c41a4c10526bcaddd0983c) fix ovn-ic doc err (#1590)
 * [5507440b7](https://github.com/kubeovn/kube-ovn/commit/5507440b79602cf5aa233c441ec3fe4629064982) fix: all cluster pod will be in podadd queue (#1587)
 * [990e291e7](https://github.com/kubeovn/kube-ovn/commit/990e291e7c6f34fdffc9d6f21d2f2531c9be8a26) feat: add args for gc/inspect interval (#1572)
 * [ea2686bf1](https://github.com/kubeovn/kube-ovn/commit/ea2686bf1b0d5dc9429b8808d2b23ccc18886a98) fix: Do not Recreate Logical_Router_Port when Vpc recreated (#1570)
 * [c651a2fce](https://github.com/kubeovn/kube-ovn/commit/c651a2fce98f39581157416445ecb7b0e2a3810f) optimized initialization and gc for the chassis (#1511)
 * [f5d5b0beb](https://github.com/kubeovn/kube-ovn/commit/f5d5b0bebdda54461d046d6fdcbdaa021a78c0fc) fix pod could not be ready (#1562)
 * [337f6e05a](https://github.com/kubeovn/kube-ovn/commit/337f6e05a6b12277eafd86689a7d6604a9cc44ae) Fix incorrect usage info of 'argExternalGatewayNet' (#1567)
 * [d5535f2ba](https://github.com/kubeovn/kube-ovn/commit/d5535f2ba43c0e9a40998aa9e76876fd63a6215a) fix: delete pod panic when delete vm or statefulset. (#1565)
 * [a83305061](https://github.com/kubeovn/kube-ovn/commit/a83305061a91d2729b5251fc4779578d2844408b) fix: clean CRDs introduced by new vpc-nat-gateway (#1563)
 * [fb1f59b10](https://github.com/kubeovn/kube-ovn/commit/fb1f59b107b43ab5583f4c7cfa453ff2d17459e3) do not gc vm pod lsp when vm still exists (#1558) (#1561)
 * [a4c2ef3a9](https://github.com/kubeovn/kube-ovn/commit/a4c2ef3a9d264d3f11a171d7e7599c862968c8e4) do not delete static routes on controller startup (#1560)
 * [a13cd1e29](https://github.com/kubeovn/kube-ovn/commit/a13cd1e29eea78b0d01a8000a5fe40176a8a21e3) update alpine to v3.16 (#1559)
 * [8b9138ae0](https://github.com/kubeovn/kube-ovn/commit/8b9138ae0e27c348117c18ed8e39a0e0345cb515) fix VPC document (#1554)
 * [308e9ecd0](https://github.com/kubeovn/kube-ovn/commit/308e9ecd0d3130d076aa219a0cb0f66fc1dc0033) replace ovn-nbctl daemon with libovsdb in frequent operations (#1544)
 * [434837746](https://github.com/kubeovn/kube-ovn/commit/4348377461487f286c2bfc8a2c0e6e72bd517ba4) fix exec cmd in vpc nat gateway (#1556)
 * [5c17eeaf6](https://github.com/kubeovn/kube-ovn/commit/5c17eeaf68344d43648d8058d09e4c783c8e66bf) CNI: do not return route if nic is not eth0 (#1555)
 * [ed8ed00eb](https://github.com/kubeovn/kube-ovn/commit/ed8ed00ebfeac0bd68e79b2daf15cdc51e1dcc28) do not nat packets for incoming traffic when (#1552)
 * [8f8734bdb](https://github.com/kubeovn/kube-ovn/commit/8f8734bdbd0b53365d85d0ceb2ae2ab728af20fe) add kubeovn 1.9.2 charts (#1539)
 * [d0d9f4eae](https://github.com/kubeovn/kube-ovn/commit/d0d9f4eae467e2b63ae21bbdbf0beef6b5bec73f) fix: opt kubectl-ko install solution (#1550)
 * [9d950f672](https://github.com/kubeovn/kube-ovn/commit/9d950f67247c82862620055d12c12779c6061c28) always set mac address to sriov vf (#1551)
 * [4f1e81219](https://github.com/kubeovn/kube-ovn/commit/4f1e81219f844de841fb0f3dd069c0f9827046ef) use leases for leader election (#1529)
 * [ae56306b4](https://github.com/kubeovn/kube-ovn/commit/ae56306b4e5bf6e40469dc30b485771283abb9ae) fix: fix db-check bug (#1541)
 * [f6b24444b](https://github.com/kubeovn/kube-ovn/commit/f6b24444bb7384057ebbf486a9a232ae55d3d2ff) bump version to v1.11.0 (#1545)
 * [24791f450](https://github.com/kubeovn/kube-ovn/commit/24791f450dbdef54261d1b436a796d15a5258352) exit kube-ovn-controller on stopped leading (#1536)
 * [39e5f0a12](https://github.com/kubeovn/kube-ovn/commit/39e5f0a12219526e6011a636915c5876dc000853) fix: update check script for restart ovs-ovn after rebuild ovsdb (#1534)
 * [c84a14c65](https://github.com/kubeovn/kube-ovn/commit/c84a14c652afdf75f78087d7d1be1e5e582d5368) tmp cancel cilium external svc test (#1531)
 * [5d173010d](https://github.com/kubeovn/kube-ovn/commit/5d173010d8707effffdae3fd2d9ddd25a382249c) remove name for default drop acl in networkpolicy (#1522)

### Contributors

 * Alex Jones
 * Chris
 * Kaihang Zhang
 * KillMaster9
 * Mengxin Liu
 * Money Liu
 * Noah
 * ShaPoHun
 * Usman Malik
 * Wang Bo
 * Xiaobo Liu
 * bobz965
 * carezkh
 * changluyi
 * dependabot[bot]
 * fanriming
 * gugu
 * halfcrazy
 * huangsq
 * hzma
 * jeffy
 * long.wang
 * lut777
 * pengbinbin1
 * runzhliu
 * shane
 * wangyd1988
 * xujunjie-cover
 * zhouhui-Corigine
 * 刘睿华
 * 尚墨
 * 张祖建
 * 袁又袁

## v1.10.10 (2023-03-18)

 * [0c5fd63b2](https://github.com/kubeovn/kube-ovn/commit/0c5fd63b2d7edb7f0b3ced4d65ac908da8dc812e) prepare for release v1.10.10
 * [3631e4e42](https://github.com/kubeovn/kube-ovn/commit/3631e4e420fea3a4bb9657f0777c1b6da48429ff) ensure address label is correct before deleting it (#2487)
 * [5ffc237af](https://github.com/kubeovn/kube-ovn/commit/5ffc237af63a7f5283c5a946ce576a1d00103d29) add node to addNodeQueue if required annations are missing (#2481)
 * [2db927efd](https://github.com/kubeovn/kube-ovn/commit/2db927efdf9c8a52cc31c83b4147376bb0aa6bd5) remove unused subnet status fields (#2482)
 * [8d08c629a](https://github.com/kubeovn/kube-ovn/commit/8d08c629a53eb09c4e635f418cbc4733018171af) fix ips CR not found due to etcd error (#2472)
 * [ec7a3dd57](https://github.com/kubeovn/kube-ovn/commit/ec7a3dd572cddd375e486aa8581ae8510eeb13a9) ci: fix ovn-ic installation (#2456)
 * [b4383543d](https://github.com/kubeovn/kube-ovn/commit/b4383543d24584deb650abacba9e6f9a08d25bbf) do not set subnet's vlan empty on failure (#2445)
 * [1c21d6e79](https://github.com/kubeovn/kube-ovn/commit/1c21d6e795b5553010f8d702a3d57dc720f2879f) fix: missing import netlink
 * [a4228c392](https://github.com/kubeovn/kube-ovn/commit/a4228c3922748bd843bdd3d9ad8241ce432b4140) change cni version from v1.1.1 to v1.2.0 (#2435)
 * [9e363e41f](https://github.com/kubeovn/kube-ovn/commit/9e363e41f4690c3af88eafdba210a3ccdda69475) fix ovn-speaker router bug (#2433)
 * [458308656](https://github.com/kubeovn/kube-ovn/commit/458308656d05d44015b668789471c3149d94dc9e) fix ovn-ic e2e
 * [a1f59628f](https://github.com/kubeovn/kube-ovn/commit/a1f59628f74f7eaf5d4899c15b1e467934aae995) ci: fix cilium chaining e2e (#2391)
 * [8a584a7c4](https://github.com/kubeovn/kube-ovn/commit/8a584a7c47f55d31a2af5be6e2c921b10cc18c5c) fix: python package issues
 * [8ec90f578](https://github.com/kubeovn/kube-ovn/commit/8ec90f578322b3f91b77bfae0b8f5771c174c094) update ipv6 security-group remote group name (#2389)
 * [489d24530](https://github.com/kubeovn/kube-ovn/commit/489d24530b41aa93342b4926509f6c31d7a59ef2) Fix routeregexp ipv6 (#2395)
 * [8f045d753](https://github.com/kubeovn/kube-ovn/commit/8f045d7539464219cbb6d5b1b07b4460f74318d1) ci: fix ref name check (#2390)
 * [aec96bf2e](https://github.com/kubeovn/kube-ovn/commit/aec96bf2edd3745414bcf5266abdb7032cd6808f) bump base images
 * [6321f170f](https://github.com/kubeovn/kube-ovn/commit/6321f170f9ccd6adc4a8180b67128e63b673e7e7) ci: skip netpol e2e automatically for push events (#2379)
 * [b8ad11775](https://github.com/kubeovn/kube-ovn/commit/b8ad117757daae38e06fe8aeb6ebf2ba756cdaba) ci: make path filter more accurate (#2381)
 * [1bc6c8145](https://github.com/kubeovn/kube-ovn/commit/1bc6c8145b3302891965796f220efba630eba2d8) ci: fix path filter for windows build (#2378)
 * [5c6f394ff](https://github.com/kubeovn/kube-ovn/commit/5c6f394fff397d62a67b7fb61536bdede969bab9) e2e: run specs in parallel (#2375)
 * [437f8dfa0](https://github.com/kubeovn/kube-ovn/commit/437f8dfa05faab49e11dce96553ecbe5da176b7c) fix CVE-2022-41723
 * [edf176200](https://github.com/kubeovn/kube-ovn/commit/edf1762001e79efa7574770c8096de6972db9f9d) ci: fix default branch test (#2369)
 * [74f492e92](https://github.com/kubeovn/kube-ovn/commit/74f492e92a957c0f8d95924faacf4602371760d9) fix github actions workflows (#2363)
 * [9554adfb5](https://github.com/kubeovn/kube-ovn/commit/9554adfb5df3c2250ca62bfd19cf051364de2464) simplify github actions workflows (#2338)
 * [b62f472e2](https://github.com/kubeovn/kube-ovn/commit/b62f472e202eb459b07dc19758693924ffd347b9) use existing node switch cidr instead of the configured one (#2359)
 * [902b9a35c](https://github.com/kubeovn/kube-ovn/commit/902b9a35cf2261f04ae0a55f390fe425a0a6e245) do not remove link local route on ovn0 (#2341)
 * [e8f32ac64](https://github.com/kubeovn/kube-ovn/commit/e8f32ac64b66ecdaab889cc96c690063f3dce099) fix encap ip when the tunnel interface has multiple addresses (#2340)
 * [c0c9c71e6](https://github.com/kubeovn/kube-ovn/commit/c0c9c71e67bf78aa2bb6e9fe0c827a24a29308b9) enqueue endpoint when handling service add event (#2337)
 * [fe42367ae](https://github.com/kubeovn/kube-ovn/commit/fe42367ae897e66ac631b4cdd4eccc787ea82c0c) fix getting service backends in dual-stack clusters (#2323)
 * [33e6e41fe](https://github.com/kubeovn/kube-ovn/commit/33e6e41fe0ac22ae5eff2455a379978246dc2f7c) fix github actions workflow
 * [b2d7f735e](https://github.com/kubeovn/kube-ovn/commit/b2d7f735eed9f4fba65d5e19a477009c95ab2ca7) prepare for release v1.10.9
 * [68b34c91c](https://github.com/kubeovn/kube-ovn/commit/68b34c91c19baccc1a0e0009a964e5e61c2bbf44) fix u2o code err
 * [138fc5f11](https://github.com/kubeovn/kube-ovn/commit/138fc5f11b7127baaa73cb2a3a0ab80b8ad78e08) fix kube-ovn-controller crash on startup (#2305)
 * [50b0c8662](https://github.com/kubeovn/kube-ovn/commit/50b0c86625a4cb17214354db8c52fca8b1936e4f) fix gosec ci installation (#2295)
 * [50cc03e96](https://github.com/kubeovn/kube-ovn/commit/50cc03e963314682b6fbc88e41d3b6586fed8ce5) ovn northd: fix connection inactivity probe (#2286)
 * [1ba9977a0](https://github.com/kubeovn/kube-ovn/commit/1ba9977a0843126baeb8677fa9bf0b9308eebf0c) fix ct new config error
 * [ed53f3040](https://github.com/kubeovn/kube-ovn/commit/ed53f3040bf68933effa7dfe15e64a3dc1a320d9) fix network break on kube-ovn-cni startup (#2272)
 * [e70839b3a](https://github.com/kubeovn/kube-ovn/commit/e70839b3a52f01c312272bbe836cef2240b35ebd) fix setting mtu for ovs internal port (#2247)
 * [9195dbd32](https://github.com/kubeovn/kube-ovn/commit/9195dbd323c912203aafe150884e4dc4b86fb07b) fix gosec installation
 * [2a32c9a47](https://github.com/kubeovn/kube-ovn/commit/2a32c9a47183b422259825fe3ea3059d0fe8923b) bump base image version
 * [8a5326273](https://github.com/kubeovn/kube-ovn/commit/8a5326273c3d8ffb479a14ff6ab006ba4ea1fb89) fix ovn patches
 * [2a4b98053](https://github.com/kubeovn/kube-ovn/commit/2a4b98053e5ee3a0741b51c0d0ef93631f517aac) ovn db: add support for listening on pod ip (#2235)
 * [0d88edd62](https://github.com/kubeovn/kube-ovn/commit/0d88edd6227595f3f0f9e973a92e8502c6f87639) add enable-metrics arg to disable metrics (#2232)
 * [41120b2fb](https://github.com/kubeovn/kube-ovn/commit/41120b2fbcceea6152b00d116559f7cea1a64283) fix not building no-avx512 image (#2228)
 * [4320301e1](https://github.com/kubeovn/kube-ovn/commit/4320301e11c802c76e031d647c39703bd50e3b6b) u2o feature merge to 1.10 (#2227)
 * [c92af9b90](https://github.com/kubeovn/kube-ovn/commit/c92af9b90572a2311f12246d8f61490f947a1368) fix windows build
 * [05801fab4](https://github.com/kubeovn/kube-ovn/commit/05801fab4d454d8cee13d401ff3d1ecfd014807c) add release-1.8/1.9/1.10 to scheduled e2e (#2224)
 * [267e4aff8](https://github.com/kubeovn/kube-ovn/commit/267e4aff8315d8343126942445667da4372e8193) cni-server: fix waiting for routed annotation (#2225)
 * [6a9b2d8a7](https://github.com/kubeovn/kube-ovn/commit/6a9b2d8a7dc5211f482214d48d38ade3714603f7) release-1.10: refactor e2e (#2213)
 * [b2901e8ef](https://github.com/kubeovn/kube-ovn/commit/b2901e8ef2c50544d9f6d6c18c50f523bda746b9) feature: detect ipv4 address conflict in underlay (#2208)
 * [172e1733f](https://github.com/kubeovn/kube-ovn/commit/172e1733fe7e702e2416e2f558673c0de6e43861) set release v1.10.8

### Contributors

 * Daviddcc
 * KillMaster9
 * changluyi
 * hzma
 * zhangzujian
 * 张祖建

## v1.10.8 (2023-01-03)

 * [d009416a1](https://github.com/kubeovn/kube-ovn/commit/d009416a155aaee638d835f3f0212d2c2ed5801f) prepare for release 1.10.8
 * [b5b734292](https://github.com/kubeovn/kube-ovn/commit/b5b7342925a1ab0d700c81d45520ca84c72344ca) ci: add publish action
 * [f44c82b14](https://github.com/kubeovn/kube-ovn/commit/f44c82b145cdcf37dee14f7380b435ebc226cb8a) ovn nb and sb can't bind lan ip in ssl merge to 1.10 (#2202)
 * [7dba66c4c](https://github.com/kubeovn/kube-ovn/commit/7dba66c4cc67da3ad0573aa616f7945acf3588bc) bind local ip release 1.10 (#2198)
 * [2cad0351c](https://github.com/kubeovn/kube-ovn/commit/2cad0351c605947da64170cb146f9404f909a4df) fix: ovs gc just for pod if (#2187)
 * [498706c5c](https://github.com/kubeovn/kube-ovn/commit/498706c5c468dce110777226e2ecfbd28c0d07c9) update docs link in install.sh (#2196)
 * [ea0b77c54](https://github.com/kubeovn/kube-ovn/commit/ea0b77c54eea86fe9fbcb66a33cc7b473360e5d2) fix lr policy for default subnet with logical gateway enabled (#2177)
 * [b9085d541](https://github.com/kubeovn/kube-ovn/commit/b9085d54142b2ccc0b1adea04aae3bcda0aa40cb) sync delete pod process from release-1.9
 * [33da2052e](https://github.com/kubeovn/kube-ovn/commit/33da2052ecd4be828ec32a1132470b7541ce92db) reserve pod eip static route when update vpc (#2185)
 * [9bcb20333](https://github.com/kubeovn/kube-ovn/commit/9bcb20333614205cff93a15af563c4865a9f5a3f) ignore conflict check for pod ip crd (#2188)
 * [a6e512ae6](https://github.com/kubeovn/kube-ovn/commit/a6e512ae63abc05cf8bd58054ced242473e74e69) fix base/windows build (#2172)
 * [48b44cf67](https://github.com/kubeovn/kube-ovn/commit/48b44cf67ebf68835dcb3e7a6913eedf12a81e63) add metric interface_rx_multicast_packets (#2156)
 * [4b15aa110](https://github.com/kubeovn/kube-ovn/commit/4b15aa1103d376eb2d95f488dfe72a85f0930f2e) An error occurred when netpol was added in double-stack mode (#2160)
 * [0c4a9f1ce](https://github.com/kubeovn/kube-ovn/commit/0c4a9f1ce44bd939bbdd29a914654400fcf63216) add process for delete networkpolicy start with number (#2157)
 * [0ef78a105](https://github.com/kubeovn/kube-ovn/commit/0ef78a1053941b6311b7fc54cdcfdc1a9f7df87d) northd: fix race condition in health check (#2154)
 * [d06f17b8b](https://github.com/kubeovn/kube-ovn/commit/d06f17b8b4d601ef01f33aa123b2dc0e025b785b) add check for subnet cidr (#2153)
 * [7aa6ca369](https://github.com/kubeovn/kube-ovn/commit/7aa6ca369c123080df84c0ae85b0f29d0f7bae85) delete nc cmd in image (#2148)
 * [6182cce5c](https://github.com/kubeovn/kube-ovn/commit/6182cce5cfff78defe3867e24d12d4541b01fe26) delete ip crd base on podName (#2143)
 * [69ff0eedf](https://github.com/kubeovn/kube-ovn/commit/69ff0eedf7ac2fb16c6719542a88c1f4ecbb5c9a) some optimization for provider network status update (#2135)
 * [5c661d4f4](https://github.com/kubeovn/kube-ovn/commit/5c661d4f40929ee5e779cccb46b70df4b1282c8d) kind: support to specify api server address/port (#2134)
 * [e91bfedf4](https://github.com/kubeovn/kube-ovn/commit/e91bfedf40b002396ec1b86bb4a3a2800c6b9f0d) kubectl-ko: fix registry/version (#2133)
 * [c16394bbd](https://github.com/kubeovn/kube-ovn/commit/c16394bbdb438c639b3ed9468282c038888de864)  fix: sometimes alloc ipv6 address failed sometimes ipam.GetStaticAddress return NoAvailableAddress
 * [e37f63ae5](https://github.com/kubeovn/kube-ovn/commit/e37f63ae54307f54d856881ac57f6e8eb65a36e2) fix: delete static route should consider dualstack (#2130)
 * [d17c4dddf](https://github.com/kubeovn/kube-ovn/commit/d17c4dddf2079a1095ee85abd27e0ba0f366bd91) optimize provider network (#2099)
 * [3f8687bc1](https://github.com/kubeovn/kube-ovn/commit/3f8687bc12eda0644dacf6dd00e5a6e787cd8a70) fix removing default static route in default vpc (#2116)
 * [9ef032b69](https://github.com/kubeovn/kube-ovn/commit/9ef032b693ae9a1cd88be7dcfb85208408af015a) fix: cni response missing sandbox field (#2089)
 * [20130696a](https://github.com/kubeovn/kube-ovn/commit/20130696a59f9baaf8f2bb37a1ab2c493995d688) fix: eip deletion (#2118)
 * [9d1e526d9](https://github.com/kubeovn/kube-ovn/commit/9d1e526d9f546c214263ce8249be413c50ac2ec5) fix policy route for subnets with logical gateway (#2108)
 * [3a8bb12ca](https://github.com/kubeovn/kube-ovn/commit/3a8bb12ca4b76d6782fbf4624c3aefcb80c32e84) replace klog.Fatalf with klog.ErrorS and klog.FlushAndExit (#2093)
 * [c0e6b57c8](https://github.com/kubeovn/kube-ovn/commit/c0e6b57c8a0601c92667926d2dee4272b3de16f6) fix: del createIPS (#2087)
 * [d76976cf1](https://github.com/kubeovn/kube-ovn/commit/d76976cf14607e8ff7864382c4bff93b01d32762) check if externalIds map is nil when add node as gw for centralized subnet (#2088)
 * [7d2e8eaa3](https://github.com/kubeovn/kube-ovn/commit/7d2e8eaa30f97674adeb654efd4b472852a3a1a5) fix ovs bridge not deleted cause by port link not found (#2084)
 * [22abb8a69](https://github.com/kubeovn/kube-ovn/commit/22abb8a69c28d77983bf8130dab4cb6c32af6611) fix libovsdb issues (#2070)
 * [d916d7b8e](https://github.com/kubeovn/kube-ovn/commit/d916d7b8eb20ce857095ff2109ef69f4d7c1093a) ipset: fix unknown ipset data attribute from kernel (#2086)
 * [8e068b26e](https://github.com/kubeovn/kube-ovn/commit/8e068b26eb3a432459ccca4a4729bc7a87b1f8a1) reflactor: add unkown config to lsp
 * [0af7ac205](https://github.com/kubeovn/kube-ovn/commit/0af7ac2057bc9abf9b937ab223d1574776bf8faf) fix OVN LS/LB gc (#2069)
 * [edc2e6455](https://github.com/kubeovn/kube-ovn/commit/edc2e6455b6f313874941c7f55f7b8ab7a69753a) Fix:hybrid-dpdk with vxlan tunnel mode，The OVS node does not create a VXLAN tunnel to the OVS-DPDK node (#2065)
 * [e3e79a74d](https://github.com/kubeovn/kube-ovn/commit/e3e79a74d1843089059583f95e9410aac25b78c3) update ipv6 address for vpc peer (#2060)
 * [15e544f35](https://github.com/kubeovn/kube-ovn/commit/15e544f35486376a9914f80892a2eafeb2c1e453) perf: reduce controller init time (#2054)
 * [8b06f3f56](https://github.com/kubeovn/kube-ovn/commit/8b06f3f5696f04c8eed903ddccf7fcb69c568c12) fix: replace replace with add to override existing route (#2052)
 * [fa3c8c9a9](https://github.com/kubeovn/kube-ovn/commit/fa3c8c9a98431c945c3a6f8136351aeebb071ce5) pass klog verbosity to libovsdb (#2048)
 * [70240ff35](https://github.com/kubeovn/kube-ovn/commit/70240ff35e2cc01aa7c0441dda43456faf7d613a) use the latest base image
 * [97494c73a](https://github.com/kubeovn/kube-ovn/commit/97494c73ab5bdeea142c1a989587c11cbe22da4e) ovs: fix reaching resubmit limit in underlay (#2038)
 * [f69ad3812](https://github.com/kubeovn/kube-ovn/commit/f69ad38127657a8ab7a24f0772c0b539b3768887) fix: vpc and vpc nat gw not clean (#2032)
 * [791d92447](https://github.com/kubeovn/kube-ovn/commit/791d92447a306514424da4da768965981e2724e1) fix: install the latest version (#2036)

### Contributors

 * Mengxin Liu
 * bobz965
 * changluyi
 * fanriming
 * hzma
 * lut777
 * wangyd1988
 * zhangzujian
 * 张祖建
 * 袁又袁
 * 马洪贞

## v1.10.7 (2022-11-11)

 * [6c2ff6ab8](https://github.com/kubeovn/kube-ovn/commit/6c2ff6ab8f4973b95a351bbaeaa67cffd0dc9116) set release for 1.10.7
 * [0b47ca3d6](https://github.com/kubeovn/kube-ovn/commit/0b47ca3d6d040f706a04a0c22c213518c95251d2) fix: Add support for Mellanox NIC (#1999)
 * [b2cd4df17](https://github.com/kubeovn/kube-ovn/commit/b2cd4df1713917c9483c070e504a1ef19071523d) fix pinger namespace error (#2034)
 * [7e2c3be72](https://github.com/kubeovn/kube-ovn/commit/7e2c3be72bfb6456a0fe89d874bb0b8a9da7de1f) increase action timeout
 * [51dbde5ef](https://github.com/kubeovn/kube-ovn/commit/51dbde5ef674fd68dba6e5201194aaa10be347b8) prepare release for 1.10.7
 * [2cab58da1](https://github.com/kubeovn/kube-ovn/commit/2cab58da16ec72ae952730171ddae31327580eb2) fix: gateway route should stay still when node is pingable (#2011)
 * [f2bdb8eac](https://github.com/kubeovn/kube-ovn/commit/f2bdb8eac81f822b10df80827cc4aa66410df1d9) iptables: avoid duplicate logging (#2028)
 * [d895b7664](https://github.com/kubeovn/kube-ovn/commit/d895b76644a73bc89078316311e6838c89be4ea0) update np name with character prefix (#2024)
 * [3267b0f51](https://github.com/kubeovn/kube-ovn/commit/3267b0f51c21b08d574f0cb290f5bb74e8a9843c) bump kind and node image versions (#2023)
 * [5db54e30b](https://github.com/kubeovn/kube-ovn/commit/5db54e30bff6954d9c6e8c717e035b5408d2cbbb) fix ovn nb/sb health check (#2019)
 * [0633625b4](https://github.com/kubeovn/kube-ovn/commit/0633625b4bb9146d7c264b4848c6e6d08dc1005c) fix ovs fdb for the local bridge port (#2014)
 * [cf1ffcb20](https://github.com/kubeovn/kube-ovn/commit/cf1ffcb203454621f4d90b11a8a34185f885c768) do not need to delete pg when update networkpolicy (#1959)
 * [381882c24](https://github.com/kubeovn/kube-ovn/commit/381882c248256d1a744e42e3595e22390f1cf358) ci: upgrade deprecated actions (#2004)
 * [071bebc64](https://github.com/kubeovn/kube-ovn/commit/071bebc64744046a5831bb3d92e25ac39947a6c4) fix: make ip deletion the same as creation (#2002)
 * [1bf5fa966](https://github.com/kubeovn/kube-ovn/commit/1bf5fa966326b0bbc18ff245ec7d3e8439ee57b9) fix: delete fiprule failed at first time (#1998)
 * [9e51caaa5](https://github.com/kubeovn/kube-ovn/commit/9e51caaa59e00b0d58a13d892e9bcf2c5881b94f) add check of write to ovn sb db for ovn-controller (#1989)
 * [ce6536a48](https://github.com/kubeovn/kube-ovn/commit/ce6536a48d6bea6d03ae1ade9158fc369516a266) fix grep matching device in routes (#1986)
 * [145663168](https://github.com/kubeovn/kube-ovn/commit/145663168a827cc24b2f283979b25dcc3a8b6952) delete pod after TerminationGracePeriodSeconds (#1984)
 * [20ed648d0](https://github.com/kubeovn/kube-ovn/commit/20ed648d043e843a393dd52b98c979756c84cc82) ovs: fix waiting flows in underlay networking (#1983)
 * [8c9232cef](https://github.com/kubeovn/kube-ovn/commit/8c9232cef6d86af649b15238582f4e64c78aeb9d) feature: support default vpc use nat gw pod as cust vpc (#1979)
 * [e7f3fb560](https://github.com/kubeovn/kube-ovn/commit/e7f3fb5602bcdbebe1654ed852371d22dd52d9ff) ovn db: recover automatically on startup if db corruption is detected (#1980)
 * [e430042f8](https://github.com/kubeovn/kube-ovn/commit/e430042f82c1434802680fc40bf17d01dd1f30e3) fix: modify src route priority (#1973)
 * [a62e0740a](https://github.com/kubeovn/kube-ovn/commit/a62e0740a777bb3512f96aa512b3c6c30945eff7) fix CVE-2022-32149
 * [d433f2579](https://github.com/kubeovn/kube-ovn/commit/d433f257937c95bb9f97cb0b786d8257ea48b91b) avoid concurrent subnet status update (#1976)
 * [9e249b343](https://github.com/kubeovn/kube-ovn/commit/9e249b3436c4e3acac76a094f57891923d0a5592) upgrade ovs-ovn pod by generation version instead of chart version (#1960)
 * [916ae9184](https://github.com/kubeovn/kube-ovn/commit/916ae91845573715cf53c22b2fa9c291431a7059) fix metrics name (#1977)
 * [f56bb0b01](https://github.com/kubeovn/kube-ovn/commit/f56bb0b015cb709f1c038b1813dbc8f4bfffbf93) add vm pod to ipam by ip when initIPAM (#1974)
 * [ffa04989d](https://github.com/kubeovn/kube-ovn/commit/ffa04989d02aeadf86988debc95037bcb8aa3b69) validate nbctl socket path in start-controller.sh
 * [21b4b3f84](https://github.com/kubeovn/kube-ovn/commit/21b4b3f8468a0af3d3c2280bab1543729e78f0bd) skip CVE-2022-3358 (#1972)
 * [3f8369507](https://github.com/kubeovn/kube-ovn/commit/3f83695071b287fb19f54ceaf5cdaf320225f7f5) use latest base image
 * [2a1074e41](https://github.com/kubeovn/kube-ovn/commit/2a1074e4159501e81822a39e1c53816ba7d5c53a) fix: add  default deny acl (#1935)
 * [aa7160333](https://github.com/kubeovn/kube-ovn/commit/aa716033320f285a6c1ef4373eed006b4d2da792) ovs: fix mac learning in environments with hairpin enabled (#1943)
 * [77c27d4b3](https://github.com/kubeovn/kube-ovn/commit/77c27d4b3c775795f8a9a688af976a6d81ce79c2) Fix registry for ovn-central container in install.sh (#1951)
 * [1f1e3c287](https://github.com/kubeovn/kube-ovn/commit/1f1e3c287eb278417a43ca024df65b673fde519a) ovs: add fdb update logging (#1941)
 * [eeaf796de](https://github.com/kubeovn/kube-ovn/commit/eeaf796de9d49133679b31dfbb4081680961bb4f) add chart version check when upgrade ovs-ovn pod
 * [b0907efc4](https://github.com/kubeovn/kube-ovn/commit/b0907efc47e001592f62bfbd6adb63db65df0ddc) fix underlay e2e testing (#1929)
 * [4a80a4857](https://github.com/kubeovn/kube-ovn/commit/4a80a4857dc3ecdc88d533a5682d7f92ef925786) set leader flag when get leader
 * [5ef11cb45](https://github.com/kubeovn/kube-ovn/commit/5ef11cb4588bebac3b1e3aa099dbd265a46afba3) set ovsdb-server vlog level to avoid warnings caused by ovs-vsctl (#1937)
 * [122041c1b](https://github.com/kubeovn/kube-ovn/commit/122041c1bfe018c28fd10196ac364ff8e4961c8a) fix: pod mistaken ls label (#1925)
 * [8996131ac](https://github.com/kubeovn/kube-ovn/commit/8996131ac76637ed748495d958143a95a992ff82) ignore pod without lsp when add pod to port-group
 * [ee1c306ad](https://github.com/kubeovn/kube-ovn/commit/ee1c306ad471d359fb78ce78433f0f3d432ef6b0) add network partition check in ovn probes
 * [efa8f60d5](https://github.com/kubeovn/kube-ovn/commit/efa8f60d5bfde85cb23fb670c85a63f01b124d34) update ns annotation when subnet cidr changed (#1921)
 * [3e00aa542](https://github.com/kubeovn/kube-ovn/commit/3e00aa54235049151033179dec1a00151963a091) fix CVE-2022-27664
 * [98f7bc08a](https://github.com/kubeovn/kube-ovn/commit/98f7bc08abb4f083a1653c231189ef830e6e9e9b) fix EIP/SNAT on dynamic Pod annotation (#1918)
 * [bcaf1e7c5](https://github.com/kubeovn/kube-ovn/commit/bcaf1e7c52b343cfdba40a9bc75179839035ea58) fix: eip and nat crd can delete even if nat gw pod deleted and ipatab… (#1917)
 * [95ebe009a](https://github.com/kubeovn/kube-ovn/commit/95ebe009a04724b23c223c4975f4c7616899528a) fix: failed to add eip (#1898)
 * [5e06b3671](https://github.com/kubeovn/kube-ovn/commit/5e06b36711e154a6288426f607c7709dc61a3c88) ci: increase golangci-lint timeout (#1894)
 * [72a260748](https://github.com/kubeovn/kube-ovn/commit/72a260748fe6991eb5cccba5b8170eef3ed2b033) fix: gatewaynode might be null (#1896)
 * [5f5e85f64](https://github.com/kubeovn/kube-ovn/commit/5f5e85f64b8a6981aad4633868dc6b9364494796) fix: api rollback
 * [63eb2551b](https://github.com/kubeovn/kube-ovn/commit/63eb2551bc0230884dfe4a07f4820452fe554620) fix: diskfull may lead to wrong raft status for ovs db (#1635)
 * [2bc4f03e1](https://github.com/kubeovn/kube-ovn/commit/2bc4f03e16a004211afb0ef077424e48bcc22b37) kubectl-ko: turn off pipefail for ovn leader check (#1891)
 * [ec0f1e4ff](https://github.com/kubeovn/kube-ovn/commit/ec0f1e4ff5acc4ee712973b9ec3e418dd9c7d4a4) update dpdk base image
 * [503807e34](https://github.com/kubeovn/kube-ovn/commit/503807e34fc8210764fe80394e4885b753d2aa06) kubectl-ko: fix trace for KubeVirt VM (#1802)
 * [f961605a5](https://github.com/kubeovn/kube-ovn/commit/f961605a519dfad80a36a1714fa0bfb11eab4026) fix duplicate logs for leader election (#1886)
 * [88473e630](https://github.com/kubeovn/kube-ovn/commit/88473e630acc146b47bebfb3f68ecae328ca34b4) fix setting ether dst addr for dnat (#1881)
 * [704c179e9](https://github.com/kubeovn/kube-ovn/commit/704c179e90d29ba6507dac443897890fb9414f88) refactor iptables rules (#1868)
 * [7f399adfc](https://github.com/kubeovn/kube-ovn/commit/7f399adfc1b28a4079b12c03f3d65e2f437a6092) cni should handler unmont volume, when delete pod. (#1873)
 * [3e54d9dd6](https://github.com/kubeovn/kube-ovn/commit/3e54d9dd6434030d8d9fda05a03f0fe426421151) delete and recreate netem qos when update process (#1872)
 * [e52d3476d](https://github.com/kubeovn/kube-ovn/commit/e52d3476d2ec68cfc539aed9c2c23c143156973d) feat: check configuration (#1832)
 * [e92c85fa0](https://github.com/kubeovn/kube-ovn/commit/e92c85fa016533a1dfdd9e4f2ccd3e6f372de171) fix: nat gw pod should set default gw to net1 so that to access public (#1864)

### Contributors

 * Kaihang Zhang
 * Mengxin Liu
 * Noah
 * bobz965
 * hzma
 * jeffy
 * long.wang
 * lut777
 * runzhliu
 * shane
 * zhangzujian
 * 张祖建
 * 马洪贞

## v1.10.6 (2022-08-30)

 * [0b9f0c1f5](https://github.com/kubeovn/kube-ovn/commit/0b9f0c1f577cddffe25d3aacf3f41c75ca4cb875) set release 1.10.6
 * [1510905c3](https://github.com/kubeovn/kube-ovn/commit/1510905c3a5910f34583824e083e6717db825f67) feat: reduce downtime by increasing arp cache timeout
 * [2b05fd4cc](https://github.com/kubeovn/kube-ovn/commit/2b05fd4cc009f70a8613e38ad298cec242ba9894) feat: reduce wait time by counting the flow num.
 * [e5378927a](https://github.com/kubeovn/kube-ovn/commit/e5378927a6fbf31ebcc511a36da249fe845bf07f) fix: missing stop_ovn_daemon args
 * [709ede035](https://github.com/kubeovn/kube-ovn/commit/709ede035131ac00f33e295bcf673193e86bcbad) delete log severity for drop acl when update networkpolicy
 * [c1e5be72e](https://github.com/kubeovn/kube-ovn/commit/c1e5be72e608caf15fc62517a89769404f76cda1) refactor: extract external routes from eip func, make it the same as … (#1671)
 * [7bcf578e6](https://github.com/kubeovn/kube-ovn/commit/7bcf578e6fd26bd7493b1fda540cc507d6a2eaab) prepare release for 1.10.6
 * [ed237f9ba](https://github.com/kubeovn/kube-ovn/commit/ed237f9ba8cdcc49f398a4644b25c4e4d382a48f) ovs: fix log file descriptor leak in monitor process (#1855)
 * [e16667c36](https://github.com/kubeovn/kube-ovn/commit/e16667c36e9097c1edd9d0433f5aa36cb9ef2469) fix ovs-ovn logging (#1848)
 * [a83ec4753](https://github.com/kubeovn/kube-ovn/commit/a83ec475348ccbe0104291b2bf0e44fde64b595a) fix: dnat port not use whole words to check (#1854)
 * [e3b410236](https://github.com/kubeovn/kube-ovn/commit/e3b4102360e58958c3486efa5a493e1bc0455b5a) fix ovn dhcp not work with ovs-dpdk (#1853)
 * [237e3189e](https://github.com/kubeovn/kube-ovn/commit/237e3189e4bfa232eb12652c3f71ecbf6016f629) update base image
 * [05b27f2de](https://github.com/kubeovn/kube-ovn/commit/05b27f2de3762494b77a3b06d72bc46758ec30c7) fix: add and set ENABLE_KEEP_VM_IP=true to keep vm ip (#1702)
 * [a4030de5c](https://github.com/kubeovn/kube-ovn/commit/a4030de5c8fca82eb439954decdf25cd6f05eebd) fix: multus macvlan ipvlan use kube-ovn ipam，but  ip not inited in init-ipam (#1843)
 * [80053001f](https://github.com/kubeovn/kube-ovn/commit/80053001fbe5d4427c02b68224e0fa4cf2509fe3) fix underlay e2e (#1828)
 * [1a3a16941](https://github.com/kubeovn/kube-ovn/commit/1a3a1694166516e4cbd86d2c12c642cabbc3e5db) fix arping error log (#1841)
 * [9447b8590](https://github.com/kubeovn/kube-ovn/commit/9447b8590421dbd50b5fc3138880fa9d4698e5c1) ko: fix kube-proxy check (#1842)
 * [774b8d467](https://github.com/kubeovn/kube-ovn/commit/774b8d46723fb74272100386f470521b77a006ec) base: use patch from OVN upstream (#1844)
 * [17d0f5af5](https://github.com/kubeovn/kube-ovn/commit/17d0f5af569cafe31bcdcdab6b0b083a8ef2f2e0) ci: switch environment to ubuntu-20.04 (#1838)
 * [9f0d324a1](https://github.com/kubeovn/kube-ovn/commit/9f0d324a17a6b1219dc75b64cb57903ce4145f60) 修改 ovs-ovn-dpdk 容器镜像编译打包，解决容器中 ovs 运行不正常：无法添加物理网卡，无法创建 vhostuserclient port 问题 (#1831)
 * [8c533548e](https://github.com/kubeovn/kube-ovn/commit/8c533548ec8105ae9e7885138d51aa6f897286ad) windows: fix ovs/ovn versions and patches (#1830)
 * [d24c51313](https://github.com/kubeovn/kube-ovn/commit/d24c5131324ef1139aa61bedbe65d98fe30fc870) update centralized subnet gateway ready patch operation (#1827)
 * [02a4caf18](https://github.com/kubeovn/kube-ovn/commit/02a4caf18c499c63baea9c336402efd9c5e58be4) fix duplicate log for tunnel interface decision (#1823)
 * [b25f58f5a](https://github.com/kubeovn/kube-ovn/commit/b25f58f5a23ed9f81fca08938f3007620d650dbc) update ovs/ovn version to fix hardware offload (#1821)
 * [842d6a347](https://github.com/kubeovn/kube-ovn/commit/842d6a347f97ad59b3f861a171bbdc2d1811b8e5) fix: use full longest word to match full ip about dnat (#1825)
 * [f12fe0eac](https://github.com/kubeovn/kube-ovn/commit/f12fe0eace1b2fddf434a152ef7c9d49d6b14cd2) update centralize subnet gatewayNode until gw is ready (#1814)
 * [b9c591f97](https://github.com/kubeovn/kube-ovn/commit/b9c591f97ad8adb2872c53ae8df2edf7b9d05294) initialize IPAM from IP CR with empty PodType for sts Pods (#1812)
 * [e57021fc6](https://github.com/kubeovn/kube-ovn/commit/e57021fc64e29157a26aea4a0c95a5f15344bc21) kubectl-ko: fix missing env-check (#1804)
 * [4c2481123](https://github.com/kubeovn/kube-ovn/commit/4c2481123436c3898736a023a0ac7aad923ecbd6) kubectl-ko: fix destination mac (#1801)
 * [c21c57d1f](https://github.com/kubeovn/kube-ovn/commit/c21c57d1fc9534746bd48dc916a8ae0654bd5139) abort kube-ovn-controller on leader change (#1797)
 * [d2939e9ee](https://github.com/kubeovn/kube-ovn/commit/d2939e9ee73ff05083d095b001d754382645acf6) avoid invalid ovn-nbctl daemon socket path (#1799)
 * [aa7b9c8f7](https://github.com/kubeovn/kube-ovn/commit/aa7b9c8f7f56d989887209eb542b066c8367430d) update vpc-nat-gateway base
 * [7674b85fe](https://github.com/kubeovn/kube-ovn/commit/7674b85fefacab832b4b9c62f6b81c4af364521d) fix: warning for empty chassis fixed (#1787)

### Contributors

 * bobz965
 * hzma
 * long.wang
 * lut777
 * zhangzujian
 * 张祖建

## v1.10.5 (2022-08-10)

 * [88531d501](https://github.com/kubeovn/kube-ovn/commit/88531d501c4a08d13ec48f80ec324c70105316c6) set release v1.10.5
 * [97031bdd6](https://github.com/kubeovn/kube-ovn/commit/97031bdd6b49fdf2252d7f5f10aa891fd94ca197) prepare for release v1.10.5
 * [4a34c5dd4](https://github.com/kubeovn/kube-ovn/commit/4a34c5dd47bd719c9e1fa4a893bf767eeacf1c7c) delete htb qos when releated annotation is deleted (#1788)
 * [66643ba3a](https://github.com/kubeovn/kube-ovn/commit/66643ba3aa6851fa5865e483b71f06fd50a36da9) perf: fix memory leak
 * [84aba41f4](https://github.com/kubeovn/kube-ovn/commit/84aba41f4bc9d12145bb7dde34a8f91e24aa699b) perf: disable mlockall to reduce memory usage
 * [35533738e](https://github.com/kubeovn/kube-ovn/commit/35533738e1b86cbdacdaa7d9457f323f3d42ed35) fix iptables for services with external traffic policy set to Local (#1773)
 * [32ee00b61](https://github.com/kubeovn/kube-ovn/commit/32ee00b6190767efac36e5d40f639ef94fe6121b) perf: reduce metrics labels (#1784)
 * [93e74c609](https://github.com/kubeovn/kube-ovn/commit/93e74c6092ceb8c13e9b9eb4dd75572a6b4ebeda) northd: remove lookup_arp_ip actions (#1780)
 * [6c7f45efd](https://github.com/kubeovn/kube-ovn/commit/6c7f45efd19c049d99712ed872c9624245f64a04) fix install error
 * [86173506d](https://github.com/kubeovn/kube-ovn/commit/86173506d7cd164b08e50b791908ccd86e697cac) fix:can not delete pod with sriov vf (#1654)
 * [dc77ceb38](https://github.com/kubeovn/kube-ovn/commit/dc77ceb385c82755253a665831038e753f3945f6) dpdk-v2 ，--with-hybrid-dpdk 修改 Dockerfile.base-dpdk 解决 编译安装 ovs-dpdk 正常运行 (#1754)
 * [7a1795e61](https://github.com/kubeovn/kube-ovn/commit/7a1795e61e7d360ad77a2687e065d924df87dc60) dpdk-v2 ，--with-hybrid-dpdk qemu 创建 sock 权限问题 (#1739)
 * [0541ce98d](https://github.com/kubeovn/kube-ovn/commit/0541ce98da448b6372e44b2fb9e554db9c62ecf6) feature: support exchange link names of OVS bridge and provider nic in underlay networks (#1736)
 * [4617d7f7a](https://github.com/kubeovn/kube-ovn/commit/4617d7f7a31e119e168a546d48015a313fd8a84d) support kubernetes v1.24 (#1761)
 * [29f3d6edd](https://github.com/kubeovn/kube-ovn/commit/29f3d6edd6780dcb1a69f04304921186447c93eb) use leases for leader election (#1529)
 * [f02df1a82](https://github.com/kubeovn/kube-ovn/commit/f02df1a82d6004ab8532453b1752d0e14d855380) fix iptables for service traffic when external traffic policy set to local (#1728)
 * [7f256965b](https://github.com/kubeovn/kube-ovn/commit/7f256965bf0ec0598c818dcb5053d878e60c9a2b) set sysctl variables on cni server startup (#1758)
 * [47e39fbf5](https://github.com/kubeovn/kube-ovn/commit/47e39fbf5befd59e1f8254b0bbb97bab1f9abf2d) fix: add omitempty to subnet spec
 * [c9ac0cdf9](https://github.com/kubeovn/kube-ovn/commit/c9ac0cdf96270c7c9bfe5f45b320010b0d6198a3) perf: replace jemalloc to reduce memory usage
 * [7ffa99e37](https://github.com/kubeovn/kube-ovn/commit/7ffa99e37280f02e92488653500bc9b79354c990) avoid patch interface deletion & recreation during restart (#1741)
 * [8fa4ca497](https://github.com/kubeovn/kube-ovn/commit/8fa4ca49705f35c613a28e48f436696441463ee9) only support IPv4 snat in vpc-nat-gw when internal subnet is dual (#1747)
 * [a46b36d98](https://github.com/kubeovn/kube-ovn/commit/a46b36d98687c359c4d3224e1106b6b528389de0) enqueue subnets after vpc update (#1722)
 * [1bf5dc44f](https://github.com/kubeovn/kube-ovn/commit/1bf5dc44f89b7699ec23e0dcc54db56d802e919b) do not add subnet not processed by kube-ovn to vpc (#1735)
 * [66d8be9f1](https://github.com/kubeovn/kube-ovn/commit/66d8be9f1dd6226d58ec743d5076ced665a02802) dpdk-v2 ，--with-hybrid-dpdk qemu 创建 sock 权限问题 (#1739)
 * [e9c27c605](https://github.com/kubeovn/kube-ovn/commit/e9c27c60556c4a115df0b06996919d3ca8ec5517) fix: If pod has snat or eip, also need delete staticRoute when delete pod. (#1731)
 * [7841f0821](https://github.com/kubeovn/kube-ovn/commit/7841f082151a058d2f54db3cb537f5cdfc143a0e) optimize lrp create for subnet in vpc (#1712)
 * [994885c80](https://github.com/kubeovn/kube-ovn/commit/994885c808177ab74e7d813c509763bc047899f6) fix: cancel delete staticroute when it's used by NatRule (#1733)
 * [f9a84588e](https://github.com/kubeovn/kube-ovn/commit/f9a84588e6c147a4d4e252920b2cf064629ed1dd) fix: wrong info when update subnet from dual to ipv4 or ipv6. (#1726)
 * [77988f21f](https://github.com/kubeovn/kube-ovn/commit/77988f21f3f5a7155908ed8f2d3a384baad7e808) fix overlay MTU in vxlan/stt tunnels (#1693)

### Contributors

 * Mengxin Liu
 * hzma
 * long.wang
 * xujunjie-cover
 * zhouhui-Corigine
 * 张祖建

## v1.10.4 (2022-07-18)

 * [1e4a19599](https://github.com/kubeovn/kube-ovn/commit/1e4a195992020c422a3f6edf82e06a2277e00ca7) set release 1.10.4
 * [0bbcb3898](https://github.com/kubeovn/kube-ovn/commit/0bbcb3898fb5b590637d78b4e5b68f528637ca97) prepare for release 1.10.4
 * [fb76c58e5](https://github.com/kubeovn/kube-ovn/commit/fb76c58e51894cb18f720ada9f3c58257745e285) fix: response has no gw when create nic without default route (#1703)
 * [55b3d5083](https://github.com/kubeovn/kube-ovn/commit/55b3d508392276c5104500ee52f7537ea8111548) ignore ovsdb-server/compact error: not storing a duplicate snapshot
 * [b6084777c](https://github.com/kubeovn/kube-ovn/commit/b6084777c279e3e031405dd0e91bb9d6b0c90a31) Get latest vpc data from apiserver instead of cache (#1684)
 * [f447a1d51](https://github.com/kubeovn/kube-ovn/commit/f447a1d519d7c61c61c85f82dd485fe03126f0fc) update priority range in htb qos (#1688)
 * [bdfdc1781](https://github.com/kubeovn/kube-ovn/commit/bdfdc178174abd3e3f4e40eb5e2f56a28086ae98) fix: clean vip eip snat dant fip in cleanup.sh (#1690)
 * [460f930cf](https://github.com/kubeovn/kube-ovn/commit/460f930cfb429997213a16376caa175d159a5655) add upgrade-ovs script (#1681)

### Contributors

 * Mengxin Liu
 * Wang Bo
 * bobz965
 * hzma
 * xujunjie-cover
 * zhangzujian

## v1.10.3 (2022-07-13)

 * [f24ed6862](https://github.com/kubeovn/kube-ovn/commit/f24ed6862f870481f6ad823401e6437c1781478c) set release 1.10.3
 * [02d68f7fb](https://github.com/kubeovn/kube-ovn/commit/02d68f7fb5036a00c1de3424a80dd9113b12a75a) prepare for release 1.10.3
 * [2c989340b](https://github.com/kubeovn/kube-ovn/commit/2c989340b834b34341af061e3f690a44101ced29) fix: change ovn-ic static route to policy (#1670)
 * [1596c9ef0](https://github.com/kubeovn/kube-ovn/commit/1596c9ef00ce7505af460978042b1e18d21795a5) fix: Do not Recreate Logical_Router_Port when Vpc recreated (#1570)
 * [db4f5ad06](https://github.com/kubeovn/kube-ovn/commit/db4f5ad0644a65dfefaf3655351150913926dbfa) Delete pod if subnet of the pod's owner(sts/vm) updated (#1678)
 * [c41897a00](https://github.com/kubeovn/kube-ovn/commit/c41897a00a1011b35efae358232cc4d8bb7bfbb5) do not snat packets only for subnets with distributed gateway when external traffic policy is set to local (#1616)
 * [8190df3b3](https://github.com/kubeovn/kube-ovn/commit/8190df3b330da01613d676fc768094c7f60c15c7) security: disable pprof by default (#1672)
 * [761ddcbc6](https://github.com/kubeovn/kube-ovn/commit/761ddcbc62586e2cb74064f0bf18973fca3c8094) bgp: consolidate service check and use service const (#1674)
 * [5cffa97d2](https://github.com/kubeovn/kube-ovn/commit/5cffa97d2708f9113b43bea05cf3cb95f7f92509) fix bgp: sync service cache (#1673)
 * [874785bfb](https://github.com/kubeovn/kube-ovn/commit/874785bfbcf7c686f2064871fe5226bd719db857) fix iptables for direct routing (#1578)
 * [f3886af7b](https://github.com/kubeovn/kube-ovn/commit/f3886af7b30a6253bed5d88bf1addbad4d2a78ac) fix libovsdb (#1664)
 * [662dfa649](https://github.com/kubeovn/kube-ovn/commit/662dfa649897728744d8d5dcb8c8bd3bdfb1fc95) mount modules for auto load ip6tables moudles (#1665)
 * [1efaeb000](https://github.com/kubeovn/kube-ovn/commit/1efaeb000deaed7c824c83265229fb58e4dbbddd) ignore pod not scheduled when reconcile subnet (#1666)
 * [4409f6c9f](https://github.com/kubeovn/kube-ovn/commit/4409f6c9f051cde843e30df4bd5e29678d7ae9de) fix ovs-ovn not running on newly added nodes (#1661)
 * [b5025a6a7](https://github.com/kubeovn/kube-ovn/commit/b5025a6a7f1dbdc39a6a3f7738bad635b4a8c032) fix get security group name by external_ids (#1663)
 * [4afbaf31d](https://github.com/kubeovn/kube-ovn/commit/4afbaf31d8514e85d184d307e35cfc9c91291bf0) add policy route when add subnet (#1655)

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

 * [b1a17c4ad](https://github.com/kubeovn/kube-ovn/commit/b1a17c4add0a817fb05340f3fc1777e57a305de4) set for release 1.10.2
 * [4d2295553](https://github.com/kubeovn/kube-ovn/commit/4d229555325ca3ac8561e815acfc26dff952aa9d) fix: no need routed when use v1.multus-cni.io/default-network (#1652)
 * [40391a038](https://github.com/kubeovn/kube-ovn/commit/40391a0384b7666ec06b82d8c3a00ecff2517fcc) prepare for release 1.10.2
 * [7c4dfe721](https://github.com/kubeovn/kube-ovn/commit/7c4dfe72192458c381781923ee399473a3727ebc) fix: subnet failed when create without protocol
 * [4b063242b](https://github.com/kubeovn/kube-ovn/commit/4b063242b513d1cd82f06bc65bba66da23a8e41c) set ether dst addr for dnat on logical switch (#1512)
 * [20222e4f5](https://github.com/kubeovn/kube-ovn/commit/20222e4f5db74782cf49336cfd31882b847cdd1f) CI: delete resources in order to avoid a long time waiting for subnet deletions. (#1643)
 * [35e29e162](https://github.com/kubeovn/kube-ovn/commit/35e29e162524ddb58e5e721ded06cfbb9329b1c7) ci: fix golangci-lint (#1639)
 * [4661b76ea](https://github.com/kubeovn/kube-ovn/commit/4661b76eaeeb28aea6a1ab853929f49117befc21) fix: cleanup should ignore patch failed (#1626)
 * [73a53ba74](https://github.com/kubeovn/kube-ovn/commit/73a53ba74fbd3ee4dadc6b6c4730ccafe2f06808) fix no interface report to multus cni, missing in k8s.v1.cni.cncf.io/network[s]-status (#1636)
 * [fe5e020eb](https://github.com/kubeovn/kube-ovn/commit/fe5e020eb9251658f7c30ba07d4687125ede8078) Update install.sh (#1645)
 * [bd7ff5338](https://github.com/kubeovn/kube-ovn/commit/bd7ff5338c55ac01d790ecacc75b7e83c4fd1b22) set networkpolicy log default to false (#1633)
 * [83c9e8456](https://github.com/kubeovn/kube-ovn/commit/83c9e84560d5e789e1408334b05b210e711cca3b) update policy route when join subnet cidr changed (#1638)
 * [bcf057d16](https://github.com/kubeovn/kube-ovn/commit/bcf057d16d73f8639854856e3694217f826bba34) ci: update trivy options (#1637)
 * [f93a52737](https://github.com/kubeovn/kube-ovn/commit/f93a52737cdd793610cdb09ef472e4b63da3a6ae) increase initial delay of ovs-ovn liveness probe (#1634)
 * [1a55ce126](https://github.com/kubeovn/kube-ovn/commit/1a55ce126a38600ab4ed26c8a9d468bbeeb4c7e4) wait ovn-central pods running before delete ovs-ovn pods (#1627)
 * [f8a266d69](https://github.com/kubeovn/kube-ovn/commit/f8a266d69587e6c961917f1ec57fe1f71f29f3f4) get dbstatus for all ovn-central pod (#1619)
 * [bc838d5a6](https://github.com/kubeovn/kube-ovn/commit/bc838d5a607275c33622d8122646acd622a5bb70) delete "allow" policy route on subnet deletion (#1628)

### Contributors

 * Mengxin Liu
 * ShaPoHun
 * halfcrazy
 * hzma
 * xujunjie-cover
 * zhangzujian
 * 张祖建

## v1.10.1 (2022-06-19)

 * [4935fa6ad](https://github.com/kubeovn/kube-ovn/commit/4935fa6adc8a0088b173603e819cec274996ed29) monitor dns in cilium e2e (#1597)
 * [3dc290413](https://github.com/kubeovn/kube-ovn/commit/3dc290413f89d0a51fc0f6549f4ae115e6fd9320) prepare for release 1.10.1
 * [e459688e0](https://github.com/kubeovn/kube-ovn/commit/e459688e03f628741901f442a589e1afb79abfc8) ci: build amd64 images without avx512 (#1584)
 * [d71446817](https://github.com/kubeovn/kube-ovn/commit/d71446817c63ab573ef7fc359ff90ffd68bef526) update ovs health check, delete connection to ovn sb db (#1588)
 * [cfbe55e02](https://github.com/kubeovn/kube-ovn/commit/cfbe55e028bcd273ed16d9c6b64203cc86b27059) fix: all cluster pod will be in podadd queue (#1587)
 * [08ba4215b](https://github.com/kubeovn/kube-ovn/commit/08ba4215b6986b5d2a7f928dd9460eee1adf31a5) fix pod could not be ready (#1562)
 * [c453b7ac2](https://github.com/kubeovn/kube-ovn/commit/c453b7ac2f4720ab32f44e10a19d0e1accb8a91f) fix: delete pod panic when delete vm or statefulset. (#1565)
 * [77044e3da](https://github.com/kubeovn/kube-ovn/commit/77044e3da2d3c4abf71047971beb9b348fc2e611) fix: clean CRDs introduced by new vpc-nat-gateway (#1563)
 * [e35f90f1b](https://github.com/kubeovn/kube-ovn/commit/e35f90f1b6d6156a1e743c1b8281fc0b51206fce) do not gc vm pod lsp when vm still exists (#1558)
 * [adabd853f](https://github.com/kubeovn/kube-ovn/commit/adabd853fdc6f8791a1a96e379e32ea91d692d30) do not delete static routes on controller startup (#1560)
 * [4348e58f0](https://github.com/kubeovn/kube-ovn/commit/4348e58f0240e26315af13942b0042b0cf8e8bb4) replace ovn-nbctl daemon with libovsdb in frequent operations (#1544)
 * [4cacb4b98](https://github.com/kubeovn/kube-ovn/commit/4cacb4b989192e047b219f2bace7c1351501e8c4) fix exec cmd in vpc nat gateway (#1556)
 * [0ed681afa](https://github.com/kubeovn/kube-ovn/commit/0ed681afa3ab73125ca6dec88f14180161b1c734) CNI: do not return route if nic is not eth0 (#1555)
 * [96f232d46](https://github.com/kubeovn/kube-ovn/commit/96f232d4626bfdf47a5583979a0ac69677f95e3d) do not nat packets for incoming traffic when service externalTrafficPolicy is Local
 * [bbb8a6971](https://github.com/kubeovn/kube-ovn/commit/bbb8a6971dbb48a0ea3e445ac51be95a13523faa) exit kube-ovn-controller on stopped leading (#1536)
 * [4b0bd69e3](https://github.com/kubeovn/kube-ovn/commit/4b0bd69e35284c7d30603eb05922b7631571e401) tmp cancel cilium external svc test (#1531)

### Contributors

 * hzma
 * lut777
 * xujunjie-cover
 * zhangzujian
 * 刘睿华
 * 张祖建

## v1.10.0 (2022-05-15)

 * [16d28f755](https://github.com/kubeovn/kube-ovn/commit/16d28f755b22704427c297918c01119955ed6e6d) release 1.10.0
 * [bcdb33886](https://github.com/kubeovn/kube-ovn/commit/bcdb338864fd35bf43110a97e8515cd0373d64d3) use inc-engine/recompute instead of deprecated recompute (#1528)
 * [120947669](https://github.com/kubeovn/kube-ovn/commit/1209476696394494982d64ce294580e1751b51fd) update kind to v0.13.0 (#1530)
 * [673138f28](https://github.com/kubeovn/kube-ovn/commit/673138f284a26b26535ce14450a6192c3cd77077) move dumb-init from base images to kube-ovn image (#1527)
 * [ad6826d9f](https://github.com/kubeovn/kube-ovn/commit/ad6826d9f1a4b883e1881870d3a535144fa5b286) fix installing dumb-init in arm64 image (#1525)
 * [4eebabc1e](https://github.com/kubeovn/kube-ovn/commit/4eebabc1e18a69a80412dc98991a8290f6e89a4f) optimize ovs request in cni (#1518)
 * [7a3f73d56](https://github.com/kubeovn/kube-ovn/commit/7a3f73d566e358bf9ba328d6f290927ccd5369b7) optimize node port-group check (#1514)
 * [b7c01d438](https://github.com/kubeovn/kube-ovn/commit/b7c01d438e92af1a9eeccd90a2ebb55d9462c4b9) logic optimization (#1521)
 * [65ee71b4b](https://github.com/kubeovn/kube-ovn/commit/65ee71b4ba1f3a77d146ed43aa27cd60371f69af) fix defunct ovn-nbctl daemon (#1523)
 * [ebe003701](https://github.com/kubeovn/kube-ovn/commit/ebe00370173585ca2428968bda978796e30132e5) fix arm image (#1524)
 * [354d6c3ef](https://github.com/kubeovn/kube-ovn/commit/354d6c3ef8592e3d7506c3e9cea3be0ca1559bdc) fix: keep vm's and statefulset's ips when user specified subnet (#1520)
 * [6021e5288](https://github.com/kubeovn/kube-ovn/commit/6021e5288e28caaf506da3906fb48dda1337b0c8) feature: add doc for tunning packages (#1513)
 * [8e72f2e1f](https://github.com/kubeovn/kube-ovn/commit/8e72f2e1ff7f0cac1a2984e7fc9b40e54bc77a7a) add document for windows support (#1515)
 * [d7ef43b3e](https://github.com/kubeovn/kube-ovn/commit/d7ef43b3e8e0916877b19aa4b351c06adf718102) reduce ovs-ovn restart downtime (#1516)
 * [7b8aa1241](https://github.com/kubeovn/kube-ovn/commit/7b8aa12410c986eed7d5e41aea969abff81dabf1) finish basic windows support (#1463)
 * [ecc8268fe](https://github.com/kubeovn/kube-ovn/commit/ecc8268fe25706962ce1b33eb73c65f342339f2b) refactor logical router routes (#1500)
 * [516036241](https://github.com/kubeovn/kube-ovn/commit/51603624190f0271b73979ed13a0436faa4fb58e) add netem qos when create pod (#1510)
 * [5158dd9d2](https://github.com/kubeovn/kube-ovn/commit/5158dd9d2d96d2e19f8826b403f1c4a6d5299ce6) handle the case of error node cidr (#1509)
 * [1285b0398](https://github.com/kubeovn/kube-ovn/commit/1285b03983a8add91e8442a1fef4211691df0594) fix: ovs trace flow always ends with controller action (#1508)
 * [694286902](https://github.com/kubeovn/kube-ovn/commit/694286902e595ee61f39b1ba78c94944a82e6a7c) add qos e2e test (#1505)
 * [f214ee202](https://github.com/kubeovn/kube-ovn/commit/f214ee202e02591b7ed23320839b203a162dbf4b) optimize IPAM initialization (#1498)
 * [367d6b746](https://github.com/kubeovn/kube-ovn/commit/367d6b74612c5ce12bb5ecea0accb0dc2ef5dcdf) test: fix flaky test (#1506)
 * [79ad4fcf4](https://github.com/kubeovn/kube-ovn/commit/79ad4fcf44066a8be2809bb6f3991fba84b972a1) docs: update README.md
 * [85d09ccd0](https://github.com/kubeovn/kube-ovn/commit/85d09ccd05dd74e437bd5cbd937ac3ce36262c0c) synchronize yamls with installation script (#1504)
 * [63dc5219c](https://github.com/kubeovn/kube-ovn/commit/63dc5219cbfc40d09ddb4d5c7737f27a424e4dc0) feature: svc of multiple clusters (#1491)
 * [011eacf63](https://github.com/kubeovn/kube-ovn/commit/011eacf63b901c93a8c8b65cc7d7bbc42a616d78) use OVS branch-2.17 (#1495)
 * [afc9ef622](https://github.com/kubeovn/kube-ovn/commit/afc9ef62295711399f576ec7d79b43d39fef9723) Update USERS.md (#1496)
 * [b057404ba](https://github.com/kubeovn/kube-ovn/commit/b057404baa655afba6db3ce57244dcb1c2f8f142) update document for mellanox hardware offload (#1494)
 * [fb3c3e6e8](https://github.com/kubeovn/kube-ovn/commit/fb3c3e6e8a7e26c848f2ccac786c0a4ec78f29ad) Feature iptables eip nats splits (#1437)
 * [0c95402ed](https://github.com/kubeovn/kube-ovn/commit/0c95402edd68fc95bf4c973bed49a6ce1f274254) Update USERS.md (#1493)
 * [08a7d5b61](https://github.com/kubeovn/kube-ovn/commit/08a7d5b61ed59973e193dd86adc35ff3d08613d4) update github actions (#1489)
 * [ad28dca06](https://github.com/kubeovn/kube-ovn/commit/ad28dca06fef8b1f31bc448caea4e4566070c50a) update USER.md (#1492)
 * [0db632268](https://github.com/kubeovn/kube-ovn/commit/0db63226817ff865570c203ddb3c57ca66b610fc) fix: add empty chassis check in ovn db (#1484)
 * [d631f8f8b](https://github.com/kubeovn/kube-ovn/commit/d631f8f8b8838846184eb678dc2c934377697258) feat: lsp forwarding external Layer-2 packets (#1487)
 * [d4d700ecd](https://github.com/kubeovn/kube-ovn/commit/d4d700ecdfb020a4bbb12851a5023edc36c5dbc6) base: add back kubectl (#1485)
 * [59e4ae738](https://github.com/kubeovn/kube-ovn/commit/59e4ae73879a1d6cfa95905df66d5cbb02a6fab8) delete ipam record when gc lsp (#1483)
 * [73405b2ad](https://github.com/kubeovn/kube-ovn/commit/73405b2ad2577ae5ec42521b4c827e91954ee4fd) fix: wrong vpc-nat-gateway arm image (#1482)
 * [881622d47](https://github.com/kubeovn/kube-ovn/commit/881622d47dbe61340d99614620464d421c7613cc) fix pod annotation may override by patch (#1480)
 * [e772ee95e](https://github.com/kubeovn/kube-ovn/commit/e772ee95ecdc96e8c3c6fc5fbb35d54a3d4671f5) add acl doc (#1476)
 * [6ef72e75d](https://github.com/kubeovn/kube-ovn/commit/6ef72e75db2309c1090fc58306e400cc938fff47) fix: workqueue_depth should show count not rate (#1478)
 * [5ba5c5264](https://github.com/kubeovn/kube-ovn/commit/5ba5c5264c28e9c59e5d67977588163af0a073be) add delete ovs pods after restore nb db (#1474)
 * [73f9d15fc](https://github.com/kubeovn/kube-ovn/commit/73f9d15fcc1bb7a32a1e137a3c26deffffa5fbde) delete monitor noexecute toleration (#1473)
 * [abaebea47](https://github.com/kubeovn/kube-ovn/commit/abaebea4790d7b9490eb5fa8a962fc4dd3302031) add env-check (#1464)
 * [1d6d46532](https://github.com/kubeovn/kube-ovn/commit/1d6d46532690f8e85e6726939233ab9a65c413a1) Support kubevirt vm live migrate for pod static ip (#1468)
 * [54cab3aa2](https://github.com/kubeovn/kube-ovn/commit/54cab3aa2f0bd2c5ca28fe883ff50afbc8ee802a) fix routes for packets from Pods to other nodes
 * [ba8c5937e](https://github.com/kubeovn/kube-ovn/commit/ba8c5937e8e4205a5f19768d739472033866666e) add manual compile method for ubuntu20.04 (#1461)
 * [7848d71fb](https://github.com/kubeovn/kube-ovn/commit/7848d71fbf415d1b43a0ecd4cdd8cef760efcb9d) append metrics (#1465)
 * [4f0b19766](https://github.com/kubeovn/kube-ovn/commit/4f0b197663d3582f0a1861591f262adf6b31e880) Annotation network_type always is geneve
 * [6ddba02af](https://github.com/kubeovn/kube-ovn/commit/6ddba02af015ee74a3d6d195dcde0efd3eee3081) masquerade packets from Pods to service IP
 * [3d18b8d3f](https://github.com/kubeovn/kube-ovn/commit/3d18b8d3f3322a50d36358e328c15df3338e2dad) update OVS and OVN for windows
 * [39cdfc5c0](https://github.com/kubeovn/kube-ovn/commit/39cdfc5c0df09d2e6114b03730ba77209e95f426) windows support for cni server
 * [75d8f4de3](https://github.com/kubeovn/kube-ovn/commit/75d8f4de335dd7e259192b8c0aca7e9bdeae924f) add kube-ovn-controller switch for EIP and SNAT
 * [8ac3e0c01](https://github.com/kubeovn/kube-ovn/commit/8ac3e0c019645a138ed6e79dd3988fc69d587589) docs: add USERS.md (#1454)
 * [8c214bc91](https://github.com/kubeovn/kube-ovn/commit/8c214bc91129481a69c4d815d8b832183d9165ec) update topology pic
 * [cd5c591cd](https://github.com/kubeovn/kube-ovn/commit/cd5c591cd521628dfe7aba4a3bd503b689977ed3) feature: add sb/nb db check bash script (#1441)
 * [fc5f7190a](https://github.com/kubeovn/kube-ovn/commit/fc5f7190ae5c9363422abba098c0746d44c4a632) add routed check in circulation (#1446)
 * [aa7565193](https://github.com/kubeovn/kube-ovn/commit/aa756519386eabb2225c2d669c706f14e8bbf6c1) modify init ipam by ip crd only for sts pod (#1448)
 * [3a5ead6d7](https://github.com/kubeovn/kube-ovn/commit/3a5ead6d751b9a0cbd3626270691dfb6acc0c46d) base: refactor ovn/ovs build (#1444)
 * [430511668](https://github.com/kubeovn/kube-ovn/commit/43051166883c0f8ade64add8b1049267fc4b578b) log: show the reason if get gw node failed (#1443)
 * [8f1e85ae6](https://github.com/kubeovn/kube-ovn/commit/8f1e85ae6fd4f2f89319edbee91c9e42eadb57c7) add doc for #1358 (#1440)
 * [0c0a03081](https://github.com/kubeovn/kube-ovn/commit/0c0a03081965e9642136a54a0e5f67158d5016ab) prepare windows support for cni server
 * [88b074984](https://github.com/kubeovn/kube-ovn/commit/88b0749846bb1ca49480fee75b6661313e4dc69d) modify webhook img to independent image (#1442)
 * [3dbfa4de2](https://github.com/kubeovn/kube-ovn/commit/3dbfa4de2899ef8d219085e07a3ab96f1c5e2b09) update alpine to fix CVE-2022-1271
 * [03af744f1](https://github.com/kubeovn/kube-ovn/commit/03af744f11e2b8686d774ae11c35752fec7085d2) fix adding key to delete Pod queue
 * [0ea24dcf2](https://github.com/kubeovn/kube-ovn/commit/0ea24dcf234502e0ca5d7104a7fe6549183a2137) fix IPAM initialization
 * [b26a06e7a](https://github.com/kubeovn/kube-ovn/commit/b26a06e7aacd9790008bc6b2a0d6c54042f51ecb) temporary cancel the external2cluater  e2e test for cilium (#1428)
 * [94bc20878](https://github.com/kubeovn/kube-ovn/commit/94bc20878860979aa3d4aaad1cbc0222a212e9a4) ignore all link local unicast addresses/routes
 * [9be57346b](https://github.com/kubeovn/kube-ovn/commit/9be57346b2388214adfe45c74703ba561418a825) fix error handling for netlink.AddrDel
 * [87164cc95](https://github.com/kubeovn/kube-ovn/commit/87164cc9531926eda12e52adfec5b2595ae04114) replace pod name when create ip crd (#1425)
 * [e7c69ba58](https://github.com/kubeovn/kube-ovn/commit/e7c69ba58d7cc16e68eec872c71e2f493e6474e0) add webhook vaildate the vpc resource whether can be deleted. (#1423)
 * [c9a58886a](https://github.com/kubeovn/kube-ovn/commit/c9a58886a818ac14d85cad42b722c9ae5535d11c) We are looking forward to your PR! (#1422)
 * [743ce241e](https://github.com/kubeovn/kube-ovn/commit/743ce241e1497245d1b70791c87e76940415b19a) support alloc static ip from any subnet after ns supports multi subnets (#1417)
 * [d3f6431f2](https://github.com/kubeovn/kube-ovn/commit/d3f6431f234b3310b4cc0f9604c36415ab404288) fix provider-networks status
 * [48e0c4ed7](https://github.com/kubeovn/kube-ovn/commit/48e0c4ed78d701f63d8f6fd2e6439086df387116) build ovs/ovn for windows in ci
 * [3b4ac99ac](https://github.com/kubeovn/kube-ovn/commit/3b4ac99ac9dc2ee11b44502ddd59808f12603a54) cilium e2e: deploy k8s without kube-proxy
 * [902315ed5](https://github.com/kubeovn/kube-ovn/commit/902315ed50a9699ae52ba6ec715eb500666861c8) windows support for CNI
 * [f2baa2f7f](https://github.com/kubeovn/kube-ovn/commit/f2baa2f7fd634f8a1da4eae2d0d5e550f75fee90) add simple e2e for multus integration
 * [e3693436c](https://github.com/kubeovn/kube-ovn/commit/e3693436c7972455452f525f66fb068115189306) update e2e testing
 * [60bf81a35](https://github.com/kubeovn/kube-ovn/commit/60bf81a35ecee8a0a5405d5dd39a040e0685ff39) recover ips CR on IPAM initialization
 * [8e1cd4687](https://github.com/kubeovn/kube-ovn/commit/8e1cd4687a00f17321c9dfe5870dca60b558354b) docs: update ROADMAP.md and MAINTAINERS
 * [19ecaeee2](https://github.com/kubeovn/kube-ovn/commit/19ecaeee27d3052802195ba4a85900bd5be99664) create ip crd in kube-ovn-controller (#1413)
 * [25abbce7d](https://github.com/kubeovn/kube-ovn/commit/25abbce7d83d37ef755e059c161fc84888a41088) add condition for triggering the deletion of redundant chassises in sbdb (#1411)
 * [a378fad24](https://github.com/kubeovn/kube-ovn/commit/a378fad2469da2916d254227bf9a0e682bcbb78f) fix: do not recreate port for terminating pods (#1409)
 * [9587ad41a](https://github.com/kubeovn/kube-ovn/commit/9587ad41a96f319f2dbfad17c8df8a6da2f7e21c) update cni version to 1.0
 * [df83c5fb7](https://github.com/kubeovn/kube-ovn/commit/df83c5fb7bf3d9376b9ba3ce1fa22e6e44b61ce9) update underlay environment requirements
 * [ff695aa36](https://github.com/kubeovn/kube-ovn/commit/ff695aa36a7011b475f33107a104226f2ca38b95) avoid frequent ipset update
 * [f475736c6](https://github.com/kubeovn/kube-ovn/commit/f475736c6f90b753d4a673e4829a87c80fab596a) add reset for kube-ovn-monitor metrics (#1403)
 * [87d6839dd](https://github.com/kubeovn/kube-ovn/commit/87d6839dda10a5f921d307171c6dec0cb9702607) fix: The underlay physical gateway config by external-gw-addr when use snat&eip (#1400)
 * [d36a0d8d7](https://github.com/kubeovn/kube-ovn/commit/d36a0d8d74ebe568ce55d3f4c21bae7b6f5a9283) add custom acls for subnet (#1395)
 * [3206a7a2a](https://github.com/kubeovn/kube-ovn/commit/3206a7a2ae88a2e03e61f888b92aa433da7c8564) check the cidr format whether is correct (#1396)
 * [a33d519b2](https://github.com/kubeovn/kube-ovn/commit/a33d519b24f66cba7b92ddde1408a0bda2a284ce) optimize docs due to frequently asked question. (#1393)
 * [7bd25c639](https://github.com/kubeovn/kube-ovn/commit/7bd25c639118fc8bcc5f679986c94ac0e7e75cd9) adding IP Protocol enumeration to CRD can reduce the kube-ovn Controller judgment logic (#1391)
 * [dcc7971ae](https://github.com/kubeovn/kube-ovn/commit/dcc7971ae09bc22e46dca4895e12fc50007879ea) change the wechat qcode
 * [677690d51](https://github.com/kubeovn/kube-ovn/commit/677690d51f3e4ecbf8868e752a64f3f356c8eb47) append vm deletion check (#1390)
 * [0d663ebe6](https://github.com/kubeovn/kube-ovn/commit/0d663ebe67d8b206be4137fe9bb629b3f9ebd354) We should handle the case where the subnet protocol is handled (#1373)
 * [7289e87c8](https://github.com/kubeovn/kube-ovn/commit/7289e87c8ab380c7842906ddfd8e5fc0082c22ce) VIP is decoupled from port security (#1389)
 * [12907270b](https://github.com/kubeovn/kube-ovn/commit/12907270bda18366bf591403ed4a8ebde4d69a0f) chore: reduce image size (#1388)
 * [5e108fe87](https://github.com/kubeovn/kube-ovn/commit/5e108fe873eed45f12746f63b473b55b808f523c) docs: update the maintainer and roadmap (#1387)
 * [fe7cbe1ba](https://github.com/kubeovn/kube-ovn/commit/fe7cbe1ba2ffed4adacfceb9368d4302d2e0c233) ci: update kind and k8s
 * [ea60cdf71](https://github.com/kubeovn/kube-ovn/commit/ea60cdf712e064356990bc908501e58959077a44) fix external egress gateway
 * [22cb15c51](https://github.com/kubeovn/kube-ovn/commit/22cb15c513ba94aa25597cbce8b0a396d70a0980) add missing link scope routes in vpc-nat-gateway
 * [5571619da](https://github.com/kubeovn/kube-ovn/commit/5571619da26fe3a45660037444d91a9016a7cb63) update nodeips for restore cmd in ko plugin
 * [33180a1c7](https://github.com/kubeovn/kube-ovn/commit/33180a1c7648500f16dfe55b22bbc7776f4e5115) increase memory limit of ovn-central
 * [aa24894ea](https://github.com/kubeovn/kube-ovn/commit/aa24894ea3b35fc7de50213344c001506b1bc7f8) fix range loop
 * [1f24d64d9](https://github.com/kubeovn/kube-ovn/commit/1f24d64d942e0655417f7d4be16d4e5dee98b7c0) fix probe error
 * [c621853ab](https://github.com/kubeovn/kube-ovn/commit/c621853abfd2de8a0050c59d940f38b253287cb0) update script to add restore plugin cmd
 * [dd4a5e0d6](https://github.com/kubeovn/kube-ovn/commit/dd4a5e0d62a284d347f986cffec2478c577cae2a) support dpdk (#1317)
 * [8ad9e8382](https://github.com/kubeovn/kube-ovn/commit/8ad9e8382b513b02d117be09301f1a38bddf18b6) Use camel case instead of snake case
 * [9f3426ee8](https://github.com/kubeovn/kube-ovn/commit/9f3426ee82611edc991ed814a1c8cfd24d35e14e) add detail error when failed to create resource
 * [44dae1f70](https://github.com/kubeovn/kube-ovn/commit/44dae1f704ed049126a07e85f53c9a54ddb8ef9e) add restore process for ovn nb db
 * [c4bb24543](https://github.com/kubeovn/kube-ovn/commit/c4bb24543a4b612661e80248d9cd562ee4dbb1c1) add reset porocess for ovs interface metrics
 * [8e8da1958](https://github.com/kubeovn/kube-ovn/commit/8e8da19585cc83ac6f67a4d4841c272c790d3727) fix SNAT/PR on Pod startup
 * [e9a4bd5c7](https://github.com/kubeovn/kube-ovn/commit/e9a4bd5c79823c6e2e67b13a221326da7d95bb51) optimize kube-ovn-monitor yaml
 * [b11ffa31c](https://github.com/kubeovn/kube-ovn/commit/b11ffa31c6f00616af8f70a4c62d2b7b4dc7d289) Update subnet.go
 * [0b43fc804](https://github.com/kubeovn/kube-ovn/commit/0b43fc8042de74cb11bbce8a0823cc048f8449c6) feat: add webhook to check subnet deletion.
 * [218377849](https://github.com/kubeovn/kube-ovn/commit/218377849857dbc53e5023ea658afe2e71deacf6) modify ipam v6 release ip problem
 * [1264684c6](https://github.com/kubeovn/kube-ovn/commit/1264684c69ddaea9e72e4a1cf2f57e50714e0013) skip ping gateway for pods during live migration
 * [0da84f83f](https://github.com/kubeovn/kube-ovn/commit/0da84f83f481db3fd3d597750ef0c891cd6b6c25) don't check conflict for migration pod with only static mac
 * [89aa2413d](https://github.com/kubeovn/kube-ovn/commit/89aa2413d9f6f6d4a9c19b9c01416363361a3dd4) add service cidr when init kubeadm
 * [bfcb0331e](https://github.com/kubeovn/kube-ovn/commit/bfcb0331eca84c37b5345247d1372fea0669a8ca) docs: add provide and ns spec for multus crd
 * [4f987b10a](https://github.com/kubeovn/kube-ovn/commit/4f987b10a203fd28e774cb87e1304e5943d184b8) update flag parse in webhook
 * [7354d0c30](https://github.com/kubeovn/kube-ovn/commit/7354d0c3005092660c6074a1ac75e0297f9d320f) fix usage of ovn commands
 * [ffd5c8448](https://github.com/kubeovn/kube-ovn/commit/ffd5c844854efc6b18f01e9a64ad872609260f63) add check for pod update process
 * [fe7a6e030](https://github.com/kubeovn/kube-ovn/commit/fe7a6e030947a874dd747b269450ff3682666804) log: rotate all logs in kube-ovn-cni and add compress
 * [024d1684b](https://github.com/kubeovn/kube-ovn/commit/024d1684b7e2ce9328621b516c56e14755930f3d) keep ip for kubevirt pod
 * [8c0b358d0](https://github.com/kubeovn/kube-ovn/commit/8c0b358d08c61e26c913e60695420c5549378280) docs: add integration with Corigine OVS offload
 * [07c531208](https://github.com/kubeovn/kube-ovn/commit/07c531208c806542457a94219ebc78b9c1f6d16f) fix OVS bridge with bond port in mode 6
 * [baeb3af41](https://github.com/kubeovn/kube-ovn/commit/baeb3af415464b9f53bf865f6fc65d49b0e0e4b3) fix: continue of deletion for del pod failed when can't found vpc or subnet (#1335)
 * [8e204be47](https://github.com/kubeovn/kube-ovn/commit/8e204be4759804dd90bf89c1e403fa83154f136f) feat: support DHCP
 * [8393f322f](https://github.com/kubeovn/kube-ovn/commit/8393f322f4ff5feccaa40d11d11112e59af50cf3) Fix usage of ovn commands
 * [bb7b5e56b](https://github.com/kubeovn/kube-ovn/commit/bb7b5e56b0c37a5437c4617e82caf9e8734bc09d) resync provider network status periodically
 * [62642ea8e](https://github.com/kubeovn/kube-ovn/commit/62642ea8efc56294e7a350e70dc8e58de9e9bc28) Revert "resync provider network status periodically"
 * [6ba89e8c0](https://github.com/kubeovn/kube-ovn/commit/6ba89e8c0ea9ebad9917932979d0feeab9e075a6) use const instead the string
 * [d8ba8d038](https://github.com/kubeovn/kube-ovn/commit/d8ba8d038ec1db6bdd04c06ae51f2964c4674799) when update gateway info, we should append old to new deploy
 * [cc124556d](https://github.com/kubeovn/kube-ovn/commit/cc124556d9b0b264531fb24358ae45008e56aef6) resync provider network status periodically
 * [c53b28b1e](https://github.com/kubeovn/kube-ovn/commit/c53b28b1e5d8a25ecc4e966343e6f28ca7dacee9) fix underlay subnet in custom VPC
 * [c4a807b1d](https://github.com/kubeovn/kube-ovn/commit/c4a807b1d4c2b7df9852b3f1e74c93365ef6ebaa) fix ips update
 * [3269bad93](https://github.com/kubeovn/kube-ovn/commit/3269bad932a630415addb233eefc888d2760a9ba) kube-ovn CNI配置文件名字可配置 (#1318)
 * [491abaa88](https://github.com/kubeovn/kube-ovn/commit/491abaa88e2b8c88b6e81967886ad69df33f32ab) delete the logic of repeated enqueueing
 * [31c0b0759](https://github.com/kubeovn/kube-ovn/commit/31c0b07597e059e4bb2f4e67ae1a8dd3ef44e4ff) add log to file, update upgrade script
 * [61c5ebb83](https://github.com/kubeovn/kube-ovn/commit/61c5ebb8399be1323fcdcea6e7c7c8e2b2797bc7) Temporarily comment out the compile and upload of the centos8 compile container.
 * [aef6595f5](https://github.com/kubeovn/kube-ovn/commit/aef6595f58cba068e5f61ae0b1f29f15f9e4fbb3) Revert "Temporarily comment out the compile and upload of the centos8 compile…"
 * [79a268738](https://github.com/kubeovn/kube-ovn/commit/79a26873882398f33e44a7d9a8926e02438b16e7) Temporarily comment out the compile and upload of the centos8 compile container.
 * [1fd27d7c0](https://github.com/kubeovn/kube-ovn/commit/1fd27d7c036a9d06681c5bea4105f66ae2cc747e) feat: add webhook for subnet update validation
 * [6ab8e3698](https://github.com/kubeovn/kube-ovn/commit/6ab8e36980f02baa86164a2aa3f971f3e885a2c1) optimized decision logic
 * [af0baa0ca](https://github.com/kubeovn/kube-ovn/commit/af0baa0ca66e5bcc7143dfd747b88098f2db4f03) Use camel case instead of snake case
 * [b6764e0bc](https://github.com/kubeovn/kube-ovn/commit/b6764e0bc6f5c9effad18a689a275f5894732cda) append add cidr and excludeIps annotation for namespace
 * [a34bb3538](https://github.com/kubeovn/kube-ovn/commit/a34bb353881285a897b31469bbd8faab0a40a3e1) feat: vpc peering connection
 * [9c5556c80](https://github.com/kubeovn/kube-ovn/commit/9c5556c80ba9bf5dfb70c1e7c6bf331539cdea3e) Remove excess code
 * [273eb844b](https://github.com/kubeovn/kube-ovn/commit/273eb844be70a7332ad2f6422ee0c521c4765ec6) chore: show install options when installing (#1293)
 * [d5e342c06](https://github.com/kubeovn/kube-ovn/commit/d5e342c068a743b4a940bf983d0a36b41786616c) feat: update provider network via node annotation
 * [e9c9b1cef](https://github.com/kubeovn/kube-ovn/commit/e9c9b1cef55107e5fd0f6af75ad68d1d77c8cf4c) add container compile and insmod
 * [a90b06a8d](https://github.com/kubeovn/kube-ovn/commit/a90b06a8d8f8077ca15e0a6d767cde35d489c303) add policy route for centralized subnet
 * [2a39f793b](https://github.com/kubeovn/kube-ovn/commit/2a39f793b6674548628e075ee3a3972d1b1b069a) fix: replace ecmp dp_hash with hash by src_ip (#1289)
 * [0fd564e40](https://github.com/kubeovn/kube-ovn/commit/0fd564e400452193a9a299a318b728efe3aad828) Use go to rerimplement ovn-is-leader.sh (#1243)
 * [432c4070e](https://github.com/kubeovn/kube-ovn/commit/432c4070e966ba3a22b59fae2a6417603f071815) fix: only log matched svc with np (#1287)
 * [cb1a698a2](https://github.com/kubeovn/kube-ovn/commit/cb1a698a254c2d2b1f53fe0fa9d68d1cb2b82790) feat: Replace command health check with k8s tcpSocket check (#1251)
 * [b220f0c6e](https://github.com/kubeovn/kube-ovn/commit/b220f0c6ee0652f9b677ecd2c4bafea60a9b8162) add 'virtual' port for vip (#1278)
 * [36c43c486](https://github.com/kubeovn/kube-ovn/commit/36c43c48653cf3782f03fe373b253e24f6e96ec2) skip the missing of kube-dns (#1286)
 * [dad0ef626](https://github.com/kubeovn/kube-ovn/commit/dad0ef62615fda516ac1ccab615aa9b16c9b9657) fix: check if taint exists before un-taint
 * [9365a62d3](https://github.com/kubeovn/kube-ovn/commit/9365a62d39dabd3d3aba802d39482d5fbede103e) add policy route for distributed subnet in default vpc
 * [a5ca73c8a](https://github.com/kubeovn/kube-ovn/commit/a5ca73c8a88519265a90f1b23be0e69b2bdcc102) ci: add retry to fix flaky test
 * [4fdca7146](https://github.com/kubeovn/kube-ovn/commit/4fdca714654b9265b8e20549693b49bdbb0d0087) set up tunnel correctly in hybrid mode
 * [7f8f322ba](https://github.com/kubeovn/kube-ovn/commit/7f8f322bac7740c9092695a76540b22609cd2563) check static route conflict
 * [e7bf87b89](https://github.com/kubeovn/kube-ovn/commit/e7bf87b89f2ebb235246c2a03acb636b31d8e833) fix: https://github.com/kubeovn/kube-ovn/issues/1271#issue-1108813998
 * [017e51252](https://github.com/kubeovn/kube-ovn/commit/017e5125207a5d276c8c0a6437eec03eb47f1482) transfer IP/route earlier in OVS startup
 * [ee2ccf1b9](https://github.com/kubeovn/kube-ovn/commit/ee2ccf1b93193e0cdc7fee64251e68d6e4f135cd) delete unused constant
 * [4022bd577](https://github.com/kubeovn/kube-ovn/commit/4022bd577cbe142a264c8f7544332711c271d95f) add metric for ovn nb/sb db status
 * [fdcc833a3](https://github.com/kubeovn/kube-ovn/commit/fdcc833a3e7a1478f1c0eac44cc3668dfd1ac5d1) add gateway check after update subnet
 * [f40e26ad7](https://github.com/kubeovn/kube-ovn/commit/f40e26ad78c375f131c0cbe8c7f4c77fd32449fb) we should first see if a condition is not going to be met
 * [3ae628cb8](https://github.com/kubeovn/kube-ovn/commit/3ae628cb8bec67852712d2f854afcc918acd53d1) add judge before use slices index
 * [47625c52c](https://github.com/kubeovn/kube-ovn/commit/47625c52c1d8262ded65671a1c325aeef2980caf) prevent multiple namespace reconcile
 * [4455c8692](https://github.com/kubeovn/kube-ovn/commit/4455c8692e306db226d2779df9bc6a3a74c51839) prevent multiple namespace reconcile
 * [6b60a5876](https://github.com/kubeovn/kube-ovn/commit/6b60a5876caacc68273fb858e0f0b408c34858fd) fix: validate statefulset pod by name
 * [fa02cb216](https://github.com/kubeovn/kube-ovn/commit/fa02cb2161b1d7ec8312569d5b84998fbb72aaca) fix golang and base image versions
 * [f210b9340](https://github.com/kubeovn/kube-ovn/commit/f210b93403240a13cbe8d2a704ba0338d088dd79) add back centralized subnet active-standby mode
 * [2557c5167](https://github.com/kubeovn/kube-ovn/commit/2557c51670b091d950859dbabcf2a660bf8ebb96) support to add multiple subnets for a namespace
 * [c230ed8a1](https://github.com/kubeovn/kube-ovn/commit/c230ed8a1b80181e055d6fb5d5e11a329166b79c) prepare for next release
 * [f95a90eb3](https://github.com/kubeovn/kube-ovn/commit/f95a90eb3ee579d01069bf610fcd184d70f22c4e) Support only configure static mac_address

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

## v1.9.41 (2025-09-04)

 * [196493105](https://github.com/kubeovn/kube-ovn/commit/1964931052c344a48e5634da926b0211bd8202cb) release v1.9.41
 * [3702b2645](https://github.com/kubeovn/kube-ovn/commit/3702b26455368d04b300e0f5bbfcd0c5bab94c39) add ignore cve
 * [9c92021a1](https://github.com/kubeovn/kube-ovn/commit/9c92021a1778acd035531ea7bbf1f924cca28e2f) add env
 * [8faa5c4c3](https://github.com/kubeovn/kube-ovn/commit/8faa5c4c39ba27d33d4cc03762f1e475a35ae55a) fix gofmt
 * [f76784685](https://github.com/kubeovn/kube-ovn/commit/f76784685fd783bf1be6fb86ebf8270d1307b60a) Support only configure static mac_address
 * [567f7a57f](https://github.com/kubeovn/kube-ovn/commit/567f7a57f121703dee87eed5453280469592c6d9) Fix mac conflict 1.11 (#5622)
 * [600995c27](https://github.com/kubeovn/kube-ovn/commit/600995c27299788ed56dfdc0d71747d747f3c0a6) Fix the problem that if available ip is 0 but there is a value in excludeIPs, the fixed ip is used as the ip in excludeIPs but the error noAddressAvaliable is still reported (#5571)
 * [80bab4153](https://github.com/kubeovn/kube-ovn/commit/80bab4153631456c320ccf580641e049affc11bc) check underlay nic exist before config external bridge (#5520)
 * [cfaca3dac](https://github.com/kubeovn/kube-ovn/commit/cfaca3daca462704ebfd27e28e74dbeeaeb9ca80) prepare for next release

### Contributors

 * Mengxin Liu
 * changluyi
 * chestack
 * clyi

## v1.9.40 (2025-05-09)

 * [02d8ebe3f](https://github.com/kubeovn/kube-ovn/commit/02d8ebe3f8786231449f78c825b44ad96bac39b9) release v1.9.40
 * [b5cefcec7](https://github.com/kubeovn/kube-ovn/commit/b5cefcec7db3c115bf2162fb94e0a34e20764b64) fix missing patch files
 * [e119c3073](https://github.com/kubeovn/kube-ovn/commit/e119c30732205c958940f18f0a0978152170fd0d) base: use local patch files (#5210)
 * [420707540](https://github.com/kubeovn/kube-ovn/commit/420707540cc43f31dbb7febf0d9f1ed5cc699fcc) bump go to 1.23.8 (#5209)
 * [2ffe461c3](https://github.com/kubeovn/kube-ovn/commit/2ffe461c39e98dbd2fc4c397c54d71f1fb1d6916) fix content of kubectl-ko (#5107)
 * [e9c0894f2](https://github.com/kubeovn/kube-ovn/commit/e9c0894f2d273f20b352147b4969c47dbf1ecfbd) kubectl-ko: fix conntrack state (#5038)
 * [df7811891](https://github.com/kubeovn/kube-ovn/commit/df78118916dde14afacf2e1b1cc4f70d6b2a400e) add gc interval (#5025)
 * [8b2598876](https://github.com/kubeovn/kube-ovn/commit/8b25988766b1a82bf252c940435052835e53e52f) bump base image
 * [eeed905f6](https://github.com/kubeovn/kube-ovn/commit/eeed905f6020037a4e95c451e0b26891bc96509c) prepare for next release

### Contributors

 * Mengxin Liu
 * changluyi
 * zhangzujian
 * 张祖建

## v1.9.39 (2025-02-19)

 * [d8396d307](https://github.com/kubeovn/kube-ovn/commit/d8396d30700f31c853ccac6208247f2c54a625e3) release v1.9.39
 * [582309418](https://github.com/kubeovn/kube-ovn/commit/5823094184d124f33f36898861616e49f203e9cb) fix: kube-ovn-cni always dump-flows br-provider per period (#4972)
 * [91ba72acd](https://github.com/kubeovn/kube-ovn/commit/91ba72acd7c42f31f1867c3593a5887da23cbdc9) bump go to 1.23.6 (#4961)
 * [2c56a40f2](https://github.com/kubeovn/kube-ovn/commit/2c56a40f22ac30d467b684bcb9bd3619b0e73373) prepare for next release

### Contributors

 * changluyi
 * 张祖建

## v1.9.38 (2025-01-13)

 * [9a300545d](https://github.com/kubeovn/kube-ovn/commit/9a300545d8216bd02728c1457f863ff6f8fde078) release v1.9.38
 * [6515a27e8](https://github.com/kubeovn/kube-ovn/commit/6515a27e8e79d56dc51b004ff0039046c5eb873f) cni-server: set node NetworkUnavailable condition after join subnet gateway check (#4915)
 * [23c6812d7](https://github.com/kubeovn/kube-ovn/commit/23c6812d77f53fafc3764260ba2aa60598df2369) subnet: exclude excludeIPs in available/using ips (#4909)
 * [1ec570efa](https://github.com/kubeovn/kube-ovn/commit/1ec570efa4383999de65ebbbc5904e3ff4308a97) ipam: check subnet's available ipv6 address count (#4903)
 * [823149efb](https://github.com/kubeovn/kube-ovn/commit/823149efb2c6730da1b72c205eb7ee82e555a902) bump go to 1.22.10 (#4902)
 * [315d77e97](https://github.com/kubeovn/kube-ovn/commit/315d77e97aa743bbf54aa8cd934f9ee58bf4e210) add release.sh
 * [d87bd1cd5](https://github.com/kubeovn/kube-ovn/commit/d87bd1cd50d78dd36326fc6a566233f4bbd0e3fd) prepare for next release

### Contributors

 * 张祖建

## v1.9.37 (2024-10-18)

 * [6d9104664](https://github.com/kubeovn/kube-ovn/commit/6d9104664cd176585df2df02c64335982a5126ac) release v1.9.37
 * [e4a612fb0](https://github.com/kubeovn/kube-ovn/commit/e4a612fb020fb4ac1b982b216c267610b5fc0baf) team device not set unmanage
 * [71fb97baf](https://github.com/kubeovn/kube-ovn/commit/71fb97baf10096a098eab5ada168ebd8b3293a8c) prepare for next release

### Contributors

 * clyi

## v1.9.36 (2024-10-16)

 * [67d448fe6](https://github.com/kubeovn/kube-ovn/commit/67d448fe622f750696c2b84b26a871eb30d9474d) release v1.9.36
 * [751c90ecc](https://github.com/kubeovn/kube-ovn/commit/751c90ecc5692afa53c4763309db609efcd9cb69) fix memory overflow, add mac_binding related options to router (#4612)
 * [b2e3dec0d](https://github.com/kubeovn/kube-ovn/commit/b2e3dec0deac8a619c147cf50980dcd0480e7a40) bump go to 1.22.8 (#4584)
 * [2d7e0ed13](https://github.com/kubeovn/kube-ovn/commit/2d7e0ed13418975c514a28416cd1fdc1edf6e5ee) ci: set trivy db repository to public.ecr.aws/aquasecurity/trivy-db:2 (#4570)
 * [8d0d6806f](https://github.com/kubeovn/kube-ovn/commit/8d0d6806f3c9dbf1522163d8c745f05a23a02992) base: rebuild go binary deps from source (#4524)
 * [a3f0dbdcf](https://github.com/kubeovn/kube-ovn/commit/a3f0dbdcf0dc4dc8015cecfa878af1fce0797806) bump kubectl to v1.30.5
 * [b0d836713](https://github.com/kubeovn/kube-ovn/commit/b0d836713094e7f3571bd154450c6c8051a25b51) netpol: add allow acl rules for u2o logical gateway (#4420)
 * [b27912fd8](https://github.com/kubeovn/kube-ovn/commit/b27912fd89e466ab5a8fdaa0b75e5e70ed305f7f) Makefile: simplify underlay u2o installation (#4419)
 * [85aa53ab0](https://github.com/kubeovn/kube-ovn/commit/85aa53ab0a47026d53f9372e17c8379ac9c9748a) bump go to 1.22.6
 * [72d03ac91](https://github.com/kubeovn/kube-ovn/commit/72d03ac910ab90b572f9a31416926d84615fee6f) cni-server: disable udp-fragmentation-offload (#4342)
 * [8a63e44a9](https://github.com/kubeovn/kube-ovn/commit/8a63e44a9fa6e103822db89712966cf1a36e3dba) bump base image
 * [b62f84fd4](https://github.com/kubeovn/kube-ovn/commit/b62f84fd4842d5b6fcc92d99bf7039ed7c52eca5) prepare for next release

### Contributors

 * changluyi
 * zhangzujian
 * 张祖建

## v1.9.35 (2024-07-19)

 * [358416caa](https://github.com/kubeovn/kube-ovn/commit/358416caa77467edc387d7b9cea769d69e88a261) prepare for next release
 * [43d7349db](https://github.com/kubeovn/kube-ovn/commit/43d7349dbd57eaac9b446c380de9fb83fe280b67) bump kubectl to v1.30.3 (#4308)
 * [30ab09d56](https://github.com/kubeovn/kube-ovn/commit/30ab09d56807e05dce9eff472b47d7c26f3b3015) underlay: set trunks of host nic port (#4282)
 * [769ef7b98](https://github.com/kubeovn/kube-ovn/commit/769ef7b9854d15cf1359da3b2340da9a04718dfe) fix ovn lb not updated due to service update failure (#4280)
 * [07785463b](https://github.com/kubeovn/kube-ovn/commit/07785463b44a73d7af6563cc8e11afca644c053c) klog: set log file max size to 200MB (#4272)
 * [f347ed0b6](https://github.com/kubeovn/kube-ovn/commit/f347ed0b607f609fd525f211680c7421bf12b1a6) logrotate: set file size limit to 100M (#4271)
 * [866b9a011](https://github.com/kubeovn/kube-ovn/commit/866b9a011e021144f469b65f1093749cfb0c0b49) bump base image
 * [c56ad42d2](https://github.com/kubeovn/kube-ovn/commit/c56ad42d2b7665ce1ff78447ce112c0e23071d7d) prepare for next release
 * [a8b527c2d](https://github.com/kubeovn/kube-ovn/commit/a8b527c2deda637f054d857154d9e2f28055e462) set release 1.9.34
 * [bb99e0d71](https://github.com/kubeovn/kube-ovn/commit/bb99e0d7126528659180f3b3f05040008e6c717a) ifx (#4261)
 * [0e06139b7](https://github.com/kubeovn/kube-ovn/commit/0e06139b708ab3c59edb06fb75d1132e884a8f5c) fix getting service cluster ips (#4206)
 * [ccdfa94a9](https://github.com/kubeovn/kube-ovn/commit/ccdfa94a99aab99e46ce22576fb8e1d3f65524bb) bump base image
 * [9a328b1ce](https://github.com/kubeovn/kube-ovn/commit/9a328b1ce9d6d136e0e88fe5034f0455f256c648) base: add traceroute
 * [e2f991a9c](https://github.com/kubeovn/kube-ovn/commit/e2f991a9c8b14d373a2710cc5b85e46a953974cf) base: bump kubectl to v1.30.2 (#4163)
 * [812589f68](https://github.com/kubeovn/kube-ovn/commit/812589f6845f53d9685314c6eace2c4b75382e8d) base: bump cni plugins to v1.5.1 (#4185)
 * [a1669f33b](https://github.com/kubeovn/kube-ovn/commit/a1669f33b1826c824ad024550c3ca69a1797c761) bump go to 1.22.4 (#4121)
 * [480a2aa4e](https://github.com/kubeovn/kube-ovn/commit/480a2aa4eb189ce0ebb37802ccec30bf737aa2ae) fix reconcile routes (#4168)
 * [25b6f47f9](https://github.com/kubeovn/kube-ovn/commit/25b6f47f9e2f53f20c3573f5d412123284de477b) replace util.DefaultVpc with c.config.ClusterRouter (#2782)
 * [9fbdcb74a](https://github.com/kubeovn/kube-ovn/commit/9fbdcb74ae92433ad8c0ffc674f3cf8d50d77be0) ci: bump actions
 * [3d5ed6eeb](https://github.com/kubeovn/kube-ovn/commit/3d5ed6eeb0b2400830094ba12b1e1c77e66c672c) prepare for next release

### Contributors

 * changluyi
 * zhangzujian
 * 张祖建

## v1.9.33 (2024-06-14)

 * [0b1187feb](https://github.com/kubeovn/kube-ovn/commit/0b1187febc31d41b638b7d897071416b22ba4b34) set release for 1.9.33
 * [5301eac20](https://github.com/kubeovn/kube-ovn/commit/5301eac20d1cb6cca17a4803528ed950ab5780d5) keep underlay subnet check same as before (#4167)
 * [04b45708e](https://github.com/kubeovn/kube-ovn/commit/04b45708e336a294e3559496d7c2b9b95bc8014d) ci: fix retrieving docker network subnet/gateway (#4161)
 * [6e42642a0](https://github.com/kubeovn/kube-ovn/commit/6e42642a0ae30af4a34ab0d9edac2e3ef5a81f6f)  Drop u2o arp request 1.9 (#4153)
 * [324bb6a50](https://github.com/kubeovn/kube-ovn/commit/324bb6a5050c10db429bbf404c5b504f9a546546) add ovn0 default route (#4127)
 * [65c26c383](https://github.com/kubeovn/kube-ovn/commit/65c26c383319370ef26c7c137e0dedd99c718576) bump base image
 * [8240b2d04](https://github.com/kubeovn/kube-ovn/commit/8240b2d0434151c83ae8db3c3a7580c67d2f1eb2) prepare for next release

### Contributors

 * changluyi
 * hzma
 * zhangzujian
 * 张祖建
 * 马洪贞

## v1.9.32 (2024-05-23)

 * [1391b41cf](https://github.com/kubeovn/kube-ovn/commit/1391b41cf95c0b4e586f4d15d3da429021bccd19) set release 1.9.32
 * [e49202304](https://github.com/kubeovn/kube-ovn/commit/e49202304e6de48070e4c9b198dfbf7aa8e5c76f) fix node gc (#4042)
 * [73a4fb2cb](https://github.com/kubeovn/kube-ovn/commit/73a4fb2cb34ddea1857b62a6a7b67942316853c4) fix ipv6 service ip not deleted (#4048)
 * [2cac431b5](https://github.com/kubeovn/kube-ovn/commit/2cac431b567ce0933d661b95a9bc75996430a37f) ci: fix missing env
 * [b20a57d99](https://github.com/kubeovn/kube-ovn/commit/b20a57d99d445081ab0f3ad0ac4b2f531a5adadc) fix deleting vip on ovn lb (#4047)
 * [75e742dbc](https://github.com/kubeovn/kube-ovn/commit/75e742dbcbe64c523d570bbb6e22c5af657362de) fix incorrect variable assignment (#3787)
 * [dff731e59](https://github.com/kubeovn/kube-ovn/commit/dff731e59effd111f1dcc37959cbe89374f9bd73) kube-ovn-monitor and kube-ovn-pinger export pprof path 1.9 (#3655)
 * [5b289f631](https://github.com/kubeovn/kube-ovn/commit/5b289f631be3f7eabe1c1c066a2d7c45bdbe1e83) fix typo (#3623)
 * [e26173e8d](https://github.com/kubeovn/kube-ovn/commit/e26173e8d28892a5ee433c480c2a583403ea69cc) fix u2o infinity recycle (#3568)
 * [b97f0d5a0](https://github.com/kubeovn/kube-ovn/commit/b97f0d5a044478c4b36a78a251cbdba912390ac7) clear load-balancer vips when delete last vip (#3555)
 * [d04912423](https://github.com/kubeovn/kube-ovn/commit/d04912423ca04e866f76454603fc83ca859427ed) iptables drop invalid rst (#3492)
 * [4e5573261](https://github.com/kubeovn/kube-ovn/commit/4e557326124843b426bcb54679598b51fc717c77) ovs-healthcheck: ignore error when log file does not exist (#3456)
 * [4f34a09a9](https://github.com/kubeovn/kube-ovn/commit/4f34a09a984dfb4e1596c6d51a38a86783e4c012) add sort for subnet.spec.excludeIps (#3436)
 * [d62da4a2a](https://github.com/kubeovn/kube-ovn/commit/d62da4a2a8845acd3106196ffe8215a7d156d24e) prepare for next release

### Contributors

 * changluyi
 * hzma
 * zhangzujian
 * 张祖建

## v1.9.31 (2023-11-13)

 * [4961476ad](https://github.com/kubeovn/kube-ovn/commit/4961476ad43aa2eced287c73b4eb19d7e490cb12) set release 1.9.31
 * [445527c66](https://github.com/kubeovn/kube-ovn/commit/445527c66b32ad46a87ee4c80d9f94d3a2f118d9) Subnet add mtu config 1.9 (#3397)
 * [7cc9a7e54](https://github.com/kubeovn/kube-ovn/commit/7cc9a7e54c789c900cc0481db5bfc081a1cb941c) add kube-ovn-controller nodeAffinity prefer not on ic gateway (#3390)
 * [b3f78fc25](https://github.com/kubeovn/kube-ovn/commit/b3f78fc257d14e1039f9aaaceab68b328162e21e) Revert "upgrade ovs-ovn pod by generation version instead of chart version (#1960)" (#3386)
 * [e36c6b925](https://github.com/kubeovn/kube-ovn/commit/e36c6b9254925757d28118e59286fdcce6c742b8) delete check for existing ip cr (#3361)
 * [630009758](https://github.com/kubeovn/kube-ovn/commit/630009758a6362f755bf2424faf592ab9369a534) update go net version (#3351)
 * [e5bb59b3c](https://github.com/kubeovn/kube-ovn/commit/e5bb59b3c824d8e8174c8ca593fa04386b481866) Iptables wrapper 1.9 (#3341)
 * [574f9bbbf](https://github.com/kubeovn/kube-ovn/commit/574f9bbbf791a2f27bd128f81fa5b01d6a32134a) add type assertion for ip crd (#3311)
 * [fae47ea7d](https://github.com/kubeovn/kube-ovn/commit/fae47ea7d681c28eabbd75cd085a27bbe2853c55) prepare for the next release

### Contributors

 * changluyi
 * hzma
 * 张祖建

## v1.9.30 (2023-10-08)

 * [1e5d80ca9](https://github.com/kubeovn/kube-ovn/commit/1e5d80ca9119df68b62e79a6baf41cc640d80e12) ovs: load kernel module ip_tables only when it exists (#3281)
 * [5cc81da2d](https://github.com/kubeovn/kube-ovn/commit/5cc81da2d1b94235df09e80a479e41db783c7675) pinger: increase packet send interval (#3259)
 * [3ba526b1c](https://github.com/kubeovn/kube-ovn/commit/3ba526b1c3907468f361db203c92ed05d3bc6896) prepare for the next release

### Contributors

 * 张祖建

## v1.9.29 (2023-09-11)

 * [b925c737d](https://github.com/kubeovn/kube-ovn/commit/b925c737d4fd8107bd1beb5ae5be809cf39fb6e0) add multicast snoop for release-1.9 (#3192)
 * [b2781c661](https://github.com/kubeovn/kube-ovn/commit/b2781c6615da3a56096980ee66cd0f9502edfbe6) underlay: fix ip/route tranfer when the nic is managed by NetworkManager (#3184)
 * [325464ecf](https://github.com/kubeovn/kube-ovn/commit/325464ecfe46e4341e607122d7570a6d86d9ce94) chart: fix ovs-ovn upgrade (#3164)
 * [2abd478da](https://github.com/kubeovn/kube-ovn/commit/2abd478dafdf6da846bf7f2a862fad8b2fa9b780) subnet: fix deleting lr policy on node deletion (#3178)
 * [b9b8c780d](https://github.com/kubeovn/kube-ovn/commit/b9b8c780d28db52965a9e68f218c1802c412b793) delete append externalIds process in initIPAM (#3134)
 * [ca061d532](https://github.com/kubeovn/kube-ovn/commit/ca061d5320f871789a88c5e2fd0d8076547f33df) move unnecessary init process after startWorkers (#3124)
 * [a8e681374](https://github.com/kubeovn/kube-ovn/commit/a8e681374a8282cfd59a3b39589161714ca9bb8a) delete append externalIds process in initIPAM (#3134)
 * [098ae5a7f](https://github.com/kubeovn/kube-ovn/commit/098ae5a7f31ef0cea45cacbeba80de4f00e7330e) prepare for the next release

### Contributors

 * changluyi
 * hzma
 * 张祖建
 * 马洪贞

## v1.9.28 (2023-08-04)

 * [b01d68e1c](https://github.com/kubeovn/kube-ovn/commit/b01d68e1c14a3660b29b8f7fa4f74c9e6bde2172) update version to v1.9.28
 * [7dcfd1719](https://github.com/kubeovn/kube-ovn/commit/7dcfd1719ca7343804e216fca59a36f759eede40) fix u2o policy route generate too many flow tables cause oom
 * [ca4b6c3ec](https://github.com/kubeovn/kube-ovn/commit/ca4b6c3ecd2b32422c779b091b0e1b8a6c8a06b4) distinguish nat ip for central subnet with ecmp and active-standby (#3100)
 * [a91f0a08d](https://github.com/kubeovn/kube-ovn/commit/a91f0a08d00a8688492eaf827ba9aa2c4d27fc14) fix .status.default when initializing the default vpc (#3086)
 * [fabbd4b84](https://github.com/kubeovn/kube-ovn/commit/fabbd4b84de7c7e1211882fb5b1a76ec7a6a1842) ci: do not pin go version (#3073)
 * [a280123f3](https://github.com/kubeovn/kube-ovn/commit/a280123f361a3aa70827665933f5e63601bc729e) ci: fix multus installation (#3062)
 * [2cdf8fe47](https://github.com/kubeovn/kube-ovn/commit/2cdf8fe47474983c51a988a144feb25c194d76d2) Revert "prepare for release 1.9.28"
 * [f347bcd0e](https://github.com/kubeovn/kube-ovn/commit/f347bcd0e150949babb610f142216d82ab31fe45) set genev_sys_6081 tx checksum off (#3045)
 * [77eb56944](https://github.com/kubeovn/kube-ovn/commit/77eb56944071e8ffd07aa85c50b3893d16e33e4d) prepare for release 1.9.28
 * [25160ba1a](https://github.com/kubeovn/kube-ovn/commit/25160ba1a15fc91958a3a4509c1c5af1bf6e3ff2) static ip in exclude-ips can be allocated normally when subnet's availableIPs is 0 #3031
 * [96613529e](https://github.com/kubeovn/kube-ovn/commit/96613529e8db1a186813dedf101ce1b597d93a67) ci: pin go version to 1.20.5 (#3034)
 * [990d4a7c5](https://github.com/kubeovn/kube-ovn/commit/990d4a7c59d3f07002e6eb0ae63d1925818207cf) pinger: use fully qualified domain name (#3032)
 * [11ce268e4](https://github.com/kubeovn/kube-ovn/commit/11ce268e46e025e6dca62ea80f074127e2a763c9) uninstall.sh: fix ipset name (#3028)
 * [66d0e439d](https://github.com/kubeovn/kube-ovn/commit/66d0e439d6bb6fe0147381315e19f7e88654687d) fix subnet finalizer (#3004)
 * [1c9fc6afb](https://github.com/kubeovn/kube-ovn/commit/1c9fc6afb2bb8e3289a4f988dfd44e9f8693372a)     kubectl ko performance enhance (#2975) (#2992)
 * [5292a08c7](https://github.com/kubeovn/kube-ovn/commit/5292a08c7db8377fe3348a380441f3745b77a466) underlay: fix NetworkManager syncer for virtual interfaces (#2988)
 * [74c652ee3](https://github.com/kubeovn/kube-ovn/commit/74c652ee312d89070a6db43589f30c4df3cd5026) underlay: does not set a device managed to no if it has VLAN managed by NM (#2986)

### Contributors

 * changluyi
 * hzma
 * 张祖建
 * 马洪贞

## v1.9.27 (2023-06-20)

 * [48187e49a](https://github.com/kubeovn/kube-ovn/commit/48187e49a4f499cda86381e4fc653f27c976061f) release 1.9.27
 * [c41a03a8f](https://github.com/kubeovn/kube-ovn/commit/c41a03a8fcfd59db0a1becf5fa119cb94119042e) add detail comment
 * [dfe43f9da](https://github.com/kubeovn/kube-ovn/commit/dfe43f9da98375cf710fa91cde00543a2893d7a6) prepare for next release
 * [b11c36e37](https://github.com/kubeovn/kube-ovn/commit/b11c36e37af385e6ea4978ed912b8a3bd7e1acf9) Kubectl ko diagnose perf release 1.9 (#2964)
 * [5995cce94](https://github.com/kubeovn/kube-ovn/commit/5995cce94ce5f73134aed543e578df494d535323) underlay: sync NetworkManager IP config to OVS bridge (#2949)
 * [2dc68307e](https://github.com/kubeovn/kube-ovn/commit/2dc68307ef9e669ee84b867f33d2ec955e8794af) typo (#2952)
 * [265392c47](https://github.com/kubeovn/kube-ovn/commit/265392c4761277c188ab0054f2eccb0bf78be46b) Revert "nm not managed only in the change provide nic name case (#2754)" (#2944)
 * [6d87274e7](https://github.com/kubeovn/kube-ovn/commit/6d87274e7065bee34ec4d5494c78bfdd7212226f) kubectl ko perf on release-1.9 (#2947)
 * [110440f58](https://github.com/kubeovn/kube-ovn/commit/110440f581d24e756a80429cc95ff23a2cf95625) u2o support specify u2o ip on release-1.9 (#2935)
 * [5c855cd6a](https://github.com/kubeovn/kube-ovn/commit/5c855cd6a74de38ed351f5b5f7df8d18e9daff45) support tos inherit from inner packet
 * [e5a13566e](https://github.com/kubeovn/kube-ovn/commit/e5a13566ee73c54a23ed097d84119d537cc8cb0a) underlay: do not delete patch ports created by ovn-controller (#2851)
 * [5bb5f45e3](https://github.com/kubeovn/kube-ovn/commit/5bb5f45e3d4808ae8b5d9a444d4001c1f9a22f1a) kubectl-ko: fix trace when u2oInterconnection is enabled (#2836)
 * [e45a29782](https://github.com/kubeovn/kube-ovn/commit/e45a29782204cb8068b9fe477a5b8a822187fa81) fix underlay access to node through ovn0 (#2847)
 * [7e32e57d2](https://github.com/kubeovn/kube-ovn/commit/7e32e57d222f1c0e43b51d185d2dbe901e6ea310) fix MTU when subnet is using logical gateway (#2834)

### Contributors

 * changluyi
 * yichanglu
 * 张祖建

## v1.9.25 (2023-05-11)

 * [a5a97ce68](https://github.com/kubeovn/kube-ovn/commit/a5a97ce680290c93028a2a85faa24999aedd0617) prepare for v1.9.26
 * [bce5b04db](https://github.com/kubeovn/kube-ovn/commit/bce5b04db57174e894e15f6717591ff124201ad1) fix ip statistics in subnet status (#2769)
 * [6b4786b3e](https://github.com/kubeovn/kube-ovn/commit/6b4786b3e7f3def75e230eeb064f94d6a30158d2) add EXCHANGE_LINK_NAME to installation script
 * [2dd1bee1c](https://github.com/kubeovn/kube-ovn/commit/2dd1bee1cfc4263359c8b30c40555b43528649c2) cni-server: wait ovs-vswitchd to be running (#2759)
 * [8c158d72a](https://github.com/kubeovn/kube-ovn/commit/8c158d72a20d6a927047b2e4728cc7cab9fa9e7b) ci: run kube-ovn e2e for underlay (#2762)
 * [5eef52ace](https://github.com/kubeovn/kube-ovn/commit/5eef52ace29adfc0f5fbf59cab265ca2ff3848ad) nm not managed only in the change provide nic name case (#2754)
 * [635c57b62](https://github.com/kubeovn/kube-ovn/commit/635c57b62058cd07614198ecb4d668c6257122cd) update policy route when change from ecmp to active-standby (#2716)
 * [af16c760b](https://github.com/kubeovn/kube-ovn/commit/af16c760b726fa36d2ffabdf0a3a882644067ecc) fix ovn lb gc (#2728)
 * [4a4397c73](https://github.com/kubeovn/kube-ovn/commit/4a4397c7329ec7fd10650c97cc72d1ebddbabc18) fix recover db failed using offical doc (#2721)
 * [786ec739b](https://github.com/kubeovn/kube-ovn/commit/786ec739b81bedac884755c73be53d77f5254675) bump base image
 * [9fe73bd29](https://github.com/kubeovn/kube-ovn/commit/9fe73bd29f0cfc131c777995a655f713d04e024e) base: remove patch for fixing ofpbuf memory leak (#2715)
 * [42da9ddde](https://github.com/kubeovn/kube-ovn/commit/42da9dddeb6a950551539d35cea11f3b1465a55d) cni-server: do not perform ipv4 conflict detection during VM live migration (#2693)
 * [e67cfd4c9](https://github.com/kubeovn/kube-ovn/commit/e67cfd4c95fa2f7ff57485dc6beb9c2739c7340a) ovn-controller: do not send GARP on localnet for Kube-OVN ports (#2690)
 * [a4cae607f](https://github.com/kubeovn/kube-ovn/commit/a4cae607ff00a9618eee50df36e0dc6e2b20f384) netpol: fix packet drop casued by incorrect address set deletion (#2677)
 * [96580be38](https://github.com/kubeovn/kube-ovn/commit/96580be38215f93e392d195301240dfa1c2c96b8) fix pg set port fail when lsp is already deleted
 * [905b541d7](https://github.com/kubeovn/kube-ovn/commit/905b541d7d596cd8eb8ec982d4eb5480ab8f9937) add subnetstatus lock for handleAddOrUpdateSubnet (#2668)
 * [abed47186](https://github.com/kubeovn/kube-ovn/commit/abed4718652112de9e4adb594a749c6f543c2840) prepare for next release
 * [b670e1c1d](https://github.com/kubeovn/kube-ovn/commit/b670e1c1da3557fc149ad57e56b2673e654ece96) broadcast free arp when pod setup (#2643)
 * [1c9e8eace](https://github.com/kubeovn/kube-ovn/commit/1c9e8eace250e659daa17401b4558ed1b873b09a) delete sync user (#2629)
 * [da11ccdc2](https://github.com/kubeovn/kube-ovn/commit/da11ccdc2b9102c25e523218aac557ede1596a77) prepare for next release
 * [c99d9ddaa](https://github.com/kubeovn/kube-ovn/commit/c99d9ddaa092a6c09012e0d4fde514ce78263f22) ci: deploy multus in thick mode (#2628)
 * [c8f55f9d4](https://github.com/kubeovn/kube-ovn/commit/c8f55f9d458d45ee9edf339855a7bef055a875cb) libovsdb: use monitor_cond as the monitor method (#2627)
 * [9f2e29e11](https://github.com/kubeovn/kube-ovn/commit/9f2e29e11a7a2e04a8ed154962e7075c7a25a3e2) ovs: fix dpif-netlink ofpbuf memory leak (#2620)
 * [bd6f1bb25](https://github.com/kubeovn/kube-ovn/commit/bd6f1bb25b63f5130b0c719d248147d00fb52b60) add debug image
 * [c3438b485](https://github.com/kubeovn/kube-ovn/commit/c3438b4852471082f5e9bf7ab36ba6c9253d2a50) ci: fix multus installation (#2604)
 * [e77adbba9](https://github.com/kubeovn/kube-ovn/commit/e77adbba9dd4dc827bf6d60d05bd1404a4ae28ae) cut invalid OVN_NB_DAEMON to make log more readable (#2601)
 * [e54d99049](https://github.com/kubeovn/kube-ovn/commit/e54d99049b6e6e3da0c8ed44bf1fb2d17c19f62e) unittest: fix length assertion (#2597)
 * [f731350de](https://github.com/kubeovn/kube-ovn/commit/f731350de20b569eb16a5a6787677679e892fbc1) bump base image
 * [b95b33957](https://github.com/kubeovn/kube-ovn/commit/b95b3395745e0874fe9c19ffa23aa66a1c4be3fb) ci: bump actions/upload-artifact to v3
 * [dd087cbfc](https://github.com/kubeovn/kube-ovn/commit/dd087cbfcea697b7c712830d2dcae3c4dda3cdbd) security: clear .trivyignore
 * [f44ec54d3](https://github.com/kubeovn/kube-ovn/commit/f44ec54d360278c660bd2282c23ba8b5f4daa9bf) underlay: get address/route before setting nm managed to no (#2592)
 * [250f34030](https://github.com/kubeovn/kube-ovn/commit/250f34030954254eaf2508ad8fd655c588d9fe6d) ci: bump kind image to v1.26.3 (#2581)

### Contributors

 * bobz965
 * changluyi
 * hzma
 * yichanglu
 * zhangzujian
 * 张祖建

## v1.9.23 (2023-03-29)

 * [d698c73da](https://github.com/kubeovn/kube-ovn/commit/d698c73da1193dfc1c35f3aa0a28cb7bed7534c3) move ipam.subnet.mutex to caller (#2571)
 * [b366ee82f](https://github.com/kubeovn/kube-ovn/commit/b366ee82f934f1ee4ace88a31ec1815db7480ec9) fix: memory leak in IPAM caused by leftover map keys (#2566)
 * [1cea97b95](https://github.com/kubeovn/kube-ovn/commit/1cea97b95e27532e87ce9b10b1a85a365e007886) fix ovn-bridge-mappings deletion (#2564)
 * [4f45cdd9f](https://github.com/kubeovn/kube-ovn/commit/4f45cdd9f3fc6e09855bfc476e3b84799e69c016) fix go mod list (#2556)
 * [e85e1ff93](https://github.com/kubeovn/kube-ovn/commit/e85e1ff93a7480f80c4c1b92bdfd3cf7e84335c4) do not set device unmanaged if NetworkManager is not running (#2549)
 * [8473f27ec](https://github.com/kubeovn/kube-ovn/commit/8473f27ec967c70574818772a9b1480f3be25295) underlay: fix network manager operation (#2546)
 * [0b0091c4f](https://github.com/kubeovn/kube-ovn/commit/0b0091c4f3a43007e289b720693e70ef0ebd37a2) controller: fix apiserver connection timeout on startup (#2545)
 * [ec81877b8](https://github.com/kubeovn/kube-ovn/commit/ec81877b878101336bf4ed3e8296691ede7fe2bd) underlay: delete altname after renaming the link (#2539)
 * [8f5c8088b](https://github.com/kubeovn/kube-ovn/commit/8f5c8088b74861d8bd40ff1698e9bf2d67e8f10a) underlay: fix link name exchange (#2516)
 * [90dcd0086](https://github.com/kubeovn/kube-ovn/commit/90dcd0086edbe4bc771428995829feed8aa1a815) change version to v1.9.23
 * [648c3c9f2](https://github.com/kubeovn/kube-ovn/commit/648c3c9f2c0504904a31a6b72fb5624071ec4672) fix changging the stopped vm's subnets, the vm cann't start normally (#2463)
 * [7df47d7d1](https://github.com/kubeovn/kube-ovn/commit/7df47d7d12cc994ec2c516e86b3c4559fca7273a) add kubevirt multus nic lsp before gc process (#2504)
 * [c89179698](https://github.com/kubeovn/kube-ovn/commit/c8917969807a51131eb9c017a11b925f3f6106ca) update for release v1.9.22

### Contributors

 * hzma
 * zhangzujian
 * 张祖建
 * 袁又袁
 * 马洪贞

## v1.9.22 (2023-03-16)

 * [439e47f89](https://github.com/kubeovn/kube-ovn/commit/439e47f893972d3590bf97b25303179f5737ea1c) ensure address label is correct before deleting it (#2487)
 * [0f567b443](https://github.com/kubeovn/kube-ovn/commit/0f567b443414baa8c8e03d0b1038764da50e79d5) add node to addNodeQueue if required annations are missing (#2481)
 * [db313ad1f](https://github.com/kubeovn/kube-ovn/commit/db313ad1fd39910065562b25dd97d710db8bed86) remove unused subnet status fields (#2482)
 * [183e34ff9](https://github.com/kubeovn/kube-ovn/commit/183e34ff9ab7bf8b3127964fcb6cac083edcd836) prepare for release v1.9.22
 * [bfa779dcd](https://github.com/kubeovn/kube-ovn/commit/bfa779dcd1bb4d5c4fdb52a458e6fd0e7ca97250) fix ips CR not found due to etcd error (#2472)
 * [e06f2b294](https://github.com/kubeovn/kube-ovn/commit/e06f2b2946a5c13311bc7d778b227f5a8db81f7a) ci: fix ovn-ic installation (#2456)
 * [694059cc6](https://github.com/kubeovn/kube-ovn/commit/694059cc66fbafe833e555ecd7ccef3000f9d251) do not set subnet's vlan empty on failure (#2445)
 * [00134846e](https://github.com/kubeovn/kube-ovn/commit/00134846e0c305906698f1656410b3aa0d70f375) set release v1.9.21
 * [a1f6a3d3f](https://github.com/kubeovn/kube-ovn/commit/a1f6a3d3f1cc4231e054e921a520c10ce4d7aa7c) prepare for release v1.9.21
 * [2861d079b](https://github.com/kubeovn/kube-ovn/commit/2861d079bb04a25edb120f1418d562808e572e91) fix: missing import netlink
 * [f1779eec3](https://github.com/kubeovn/kube-ovn/commit/f1779eec383b1ec229c25cc2c5e9e66907de6dcb) release-1.9 cni version update from v0.9.1 => v1.2.0 (#2434)
 * [511052071](https://github.com/kubeovn/kube-ovn/commit/51105207163b486228cd8e60cb4e0034230fada5) fix ovn-speaker router bug (#2433)
 * [4cec68c55](https://github.com/kubeovn/kube-ovn/commit/4cec68c557abb3d3f7ce4e026157922f944124ae) fix chart install/upgrade e2e (#2426)
 * [f2c55a544](https://github.com/kubeovn/kube-ovn/commit/f2c55a544b274d65ea22bfa8c9ab70fda45276e4) ci: fix cilium chaining e2e (#2391)
 * [8790b3ccc](https://github.com/kubeovn/kube-ovn/commit/8790b3ccca2c2a74d00e7b6628d04ffe7984f5d2) Fix routeregexp ipv6 (#2395)
 * [dc2052461](https://github.com/kubeovn/kube-ovn/commit/dc2052461e54c6a58ed2ce5bade4f2e3106f9f14) ci: fix ref name check (#2390)
 * [6ce0d02ad](https://github.com/kubeovn/kube-ovn/commit/6ce0d02ad9e8acdb9db45069b7ff2381803bb97b) bump base image
 * [551a7140d](https://github.com/kubeovn/kube-ovn/commit/551a7140d95bb59ca809bfe553eda0847553b491) ovs: fix re-creation of tunnel backing interfaces on restart.
 * [0b7e72f80](https://github.com/kubeovn/kube-ovn/commit/0b7e72f802ddaf30bd35a4541be07703ba6bd3c2) ci: skip netpol e2e automatically for push events (#2379)
 * [d2dfd104a](https://github.com/kubeovn/kube-ovn/commit/d2dfd104ab2964f9e0d58529fda74e533daa6fa2) e2e: run specs in parallel (#2375)

### Contributors

 * Daviddcc
 * KillMaster9
 * changluyi
 * zhangzujian
 * 张祖建

## v1.9.20 (2023-02-22)

 * [d32966613](https://github.com/kubeovn/kube-ovn/commit/d32966613933723b396ee47cbf81009627f0ad98) fix CVE-2022-41723
 * [2a9d70042](https://github.com/kubeovn/kube-ovn/commit/2a9d70042c8fe34f08af690eee1b33338925176a) ci: fix default branch test (#2369)
 * [25c190727](https://github.com/kubeovn/kube-ovn/commit/25c190727cc4d0535bd9392b9c0a726c461a655f) fix github actions workflows (#2363)
 * [0264ddc1d](https://github.com/kubeovn/kube-ovn/commit/0264ddc1dc1f18413a7e2bf74d42b16a877d1075) simplify github actions workflows (#2338)
 * [43b70761d](https://github.com/kubeovn/kube-ovn/commit/43b70761de4c31c29a0d914d63a5e2772423c80e) use existing node switch cidr instead of the configured one (#2359)
 * [5d3faaa94](https://github.com/kubeovn/kube-ovn/commit/5d3faaa9492232a51121c32b99705902ec5cac25) prepare for 1.9.20
 * [36c3d87ff](https://github.com/kubeovn/kube-ovn/commit/36c3d87fffa0cf9c4a564692cf250b9972729df4) do not remove link local route on ovn0 (#2341)
 * [cee5bb7f9](https://github.com/kubeovn/kube-ovn/commit/cee5bb7f9e68a9fbdbba5efa41d8384ea0437806) fix encap ip when the tunnel interface has multiple addresses (#2340)
 * [7c46ed2f7](https://github.com/kubeovn/kube-ovn/commit/7c46ed2f789b5e500cb4092c058a8592c202ea9c) enqueue endpoint when handling service add event (#2337)
 * [2f76a0fad](https://github.com/kubeovn/kube-ovn/commit/2f76a0fadcc8d63fb0128b93e67daba1ccaead62) fix getting service backends in dual-stack clusters (#2323)
 * [9b7960dd3](https://github.com/kubeovn/kube-ovn/commit/9b7960dd302ce1452bc79d800db93bc8d737f437) fix github actions workflow
 * [85fb41977](https://github.com/kubeovn/kube-ovn/commit/85fb4197704594038df3507f010bf8d22cd41454) fix u2o code err
 * [b9d58b42d](https://github.com/kubeovn/kube-ovn/commit/b9d58b42da77b501538245761a6c207a73d31e18) fix kube-ovn-controller crash on startup (#2305)
 * [a1e8e40a8](https://github.com/kubeovn/kube-ovn/commit/a1e8e40a8212753376ef959045cbe8e13856c6c2) fix gosec ci installation (#2295)
 * [3ab571646](https://github.com/kubeovn/kube-ovn/commit/3ab571646fecb80b4eaf8f4654c3d535242a7fd7) ovn northd: fix connection inactivity probe (#2286)
 * [1ab8b9e9d](https://github.com/kubeovn/kube-ovn/commit/1ab8b9e9d451133166f83243d4396d98f1154cd3) fix ct new config error
 * [63dc62a9b](https://github.com/kubeovn/kube-ovn/commit/63dc62a9b47c6e5c9b822f467776cca26422bcce) fix network break on kube-ovn-cni startup (#2272)
 * [4a8997b97](https://github.com/kubeovn/kube-ovn/commit/4a8997b97608ab59edd51437992b2b9ef76205e9) fix gosec installation
 * [5a234e02c](https://github.com/kubeovn/kube-ovn/commit/5a234e02c3154d74c33c7f6c67784267364c9071) bump base image version
 * [6427688b5](https://github.com/kubeovn/kube-ovn/commit/6427688b5723e2bef59d8258aac30da5c8a800eb) ovn db: add support for listening on pod ip (#2235)
 * [875bcd466](https://github.com/kubeovn/kube-ovn/commit/875bcd466b75b4389df682c841c1c3ea17680a36) add enable-metrics arg to disable metrics (#2232)

### Contributors

 * changluyi
 * hzma
 * zhangzujian
 * 张祖建

## v1.9.19 (2023-01-09)

 * [3aa2e78bd](https://github.com/kubeovn/kube-ovn/commit/3aa2e78bddc3faaa5911b577887af551604610d1) update install.sh
 * [22e35941d](https://github.com/kubeovn/kube-ovn/commit/22e35941d38eb6931a68c3c917cdd074bd9ba1e4) prepare release v1.9.19
 * [d48dd3652](https://github.com/kubeovn/kube-ovn/commit/d48dd36520ee80eddceaaa58a750ad884c3b8416) u2o feature merge to 1.9 (#2226)
 * [2788d8e31](https://github.com/kubeovn/kube-ovn/commit/2788d8e313a0f57d4011d61f837322dd3ecd6896) add release-1.8/1.9/1.10 to scheduled e2e (#2224)
 * [45d1c1589](https://github.com/kubeovn/kube-ovn/commit/45d1c1589ee288c2232dc2eca844d02bc8363e61) cni-server: fix waiting for routed annotation (#2225)
 * [938bd6800](https://github.com/kubeovn/kube-ovn/commit/938bd68008d985edf1355b795aa38981389c57ae) feature: detect ipv4 address conflict in underlay (#2208)
 * [82a7a51da](https://github.com/kubeovn/kube-ovn/commit/82a7a51daeacb1d54607e9f5260327e78264390d) fix git ref name in e2e
 * [3a7adc9af](https://github.com/kubeovn/kube-ovn/commit/3a7adc9af8e3717bfbc7874abac55a7469a4e4a5) release-1.9: refactor e2e (#2210)

### Contributors

 * changluyi
 * zhangzujian
 * 张祖建

## v1.9.18 (2023-01-03)

 * [015c427b6](https://github.com/kubeovn/kube-ovn/commit/015c427b6b86f5f2925ef8ebef1e78ac38175c93) ci: add publish action
 * [cd7633918](https://github.com/kubeovn/kube-ovn/commit/cd763391851c8b57d3f11904aa3656b62c48b13b) add netem qos when create pod (#1510)
 * [2dcc95ca1](https://github.com/kubeovn/kube-ovn/commit/2dcc95ca18bce84d4f999a7f800164bf6025d49d) ovn nb and sb can't bind lan ip in ssl merge to 1.9 (#2201)
 * [25281a9fe](https://github.com/kubeovn/kube-ovn/commit/25281a9fe37b64e756dccd64f50be75f1a659fce) ci: load image to kind for helm install
 * [0f3569ca3](https://github.com/kubeovn/kube-ovn/commit/0f3569ca3c85389a8424d318004e0696484c416b) prepare for release v1.9.18
 * [39bed3254](https://github.com/kubeovn/kube-ovn/commit/39bed3254107a260155cecfb2344f5f67d0d6bfe) local ip bind to service merge to release 1.9 (#2197)
 * [9ec4b1e78](https://github.com/kubeovn/kube-ovn/commit/9ec4b1e78ff348b1e401816ff0551fbe3f579cd5) fix: change condition to conditions
 * [c69685cde](https://github.com/kubeovn/kube-ovn/commit/c69685cdeb9864a7a19a8c18ac363d748218ffc7) fix: ovs gc just for pod if (#2187)
 * [799c824d2](https://github.com/kubeovn/kube-ovn/commit/799c824d2d16ec59b7e11d88b51159366ece8370) update docs link in install.sh (#2196)
 * [ec852551f](https://github.com/kubeovn/kube-ovn/commit/ec852551f338df49ef418784763d4f61772f81ef) Release 1.9 (#2181)
 * [28c5e0ce6](https://github.com/kubeovn/kube-ovn/commit/28c5e0ce636ef496a8f7cd13eca3c6edcf87cba2) ignore conflict check for pod ip crd (#2188)

### Contributors

 * Mengxin Liu
 * changluyi
 * hzma
 * lut777
 * tonyleu
 * 马洪贞

## v1.9.17 (2022-12-14)

 * [4c93a29fe](https://github.com/kubeovn/kube-ovn/commit/4c93a29fe0247f44dd9e7377bc6a6b7f837d8ebf) An error occurred when netpol was added in double-stack mode (#2160)
 * [f5b65e3ed](https://github.com/kubeovn/kube-ovn/commit/f5b65e3ed27bacd7ea6c97ec28381ddc824940c4) add process for delete networkpolicy start with number (#2157)
 * [37af103c2](https://github.com/kubeovn/kube-ovn/commit/37af103c238784160dbb6d9662dc81956eef7222) prepare for release 1.9.17
 * [6c32c3c89](https://github.com/kubeovn/kube-ovn/commit/6c32c3c89c1ce0bf40a44a617bae0036a5b90249) security: remove private key file
 * [0338f7e8f](https://github.com/kubeovn/kube-ovn/commit/0338f7e8fbaa8af4c7c25b0224daa352ae361437) security: fix security issues
 * [518283130](https://github.com/kubeovn/kube-ovn/commit/51828313039e3adbb781cca3236cc2328fdd7958) update version to v1.9.16 in install.sh
 * [abaa37bfc](https://github.com/kubeovn/kube-ovn/commit/abaa37bfc3a4ba543edd506302849499f13ae4cd) add check for subnet cidr (#2153)
 * [b2f78e9a1](https://github.com/kubeovn/kube-ovn/commit/b2f78e9a1021f11a2bb62a2c0223d4ef7d254517) delete nc cmd in image (#2148)
 * [d2b5b7c3b](https://github.com/kubeovn/kube-ovn/commit/d2b5b7c3bec7f1b954317a7cead4759d174abbfe) some optimization for provider network status update (#2135)
 * [d8d4e9130](https://github.com/kubeovn/kube-ovn/commit/d8d4e913052e919af75e395f37934711e2d9e178) kind: support to specify api server address/port (#2134)
 * [096c82f02](https://github.com/kubeovn/kube-ovn/commit/096c82f025f61743552131583864de8586f80266)  fix: sometimes alloc ipv6 address failed sometimes ipam.GetStaticAddress return NoAvailableAddress
 * [a15504c3b](https://github.com/kubeovn/kube-ovn/commit/a15504c3b8ca2a0a5bd960b32825ac3e425ce77c) optimize provider network (#2099)
 * [410c8af57](https://github.com/kubeovn/kube-ovn/commit/410c8af57d98c55ea329578213cacdb034692462) Revert "optimize provider network (#2099)"
 * [602901a21](https://github.com/kubeovn/kube-ovn/commit/602901a215059c8bbc51bbc2403c8de374a4aeef) optimize provider network (#2099)

### Contributors

 * Mengxin Liu
 * changluyi
 * fanriming
 * hzma
 * wangyd1988
 * zhangzujian
 * 张祖建
 * 马洪贞

## v1.9.16 (2022-12-05)

 * [0ea8c26ab](https://github.com/kubeovn/kube-ovn/commit/0ea8c26ab5a4a72eae675f58cf7fd1dab4cbd881) prepare for release 1.9.16
 * [aac811b58](https://github.com/kubeovn/kube-ovn/commit/aac811b582db526e2106b99fff4a4efe1ab11a38) fix policy route for subnets with logical gateway (#2108)
 * [ba632d66b](https://github.com/kubeovn/kube-ovn/commit/ba632d66b1553271f4d30b40634b7484f1dc7b42) fix lint
 * [2319d1eef](https://github.com/kubeovn/kube-ovn/commit/2319d1eef38c2c73ce4f9eaca7442033d383a56d) replace klog.Fatalf with klog.ErrorS and klog.FlushAndExit (#2093)

### Contributors

 * zhangzujian
 * 张祖建

## v1.9.15 (2022-11-29)

 * [989af9f3b](https://github.com/kubeovn/kube-ovn/commit/989af9f3b03fc3950a5b56783e11b24bc04906f0) prepare for release v1.9.15
 * [1343a9085](https://github.com/kubeovn/kube-ovn/commit/1343a90855b5bb5b269b2eb3fafd8e87c21e6b9e) fix: del createIPS (#2087)
 * [524b6d3f7](https://github.com/kubeovn/kube-ovn/commit/524b6d3f7af9f3ece35f8641a5c9b4ca394f8219) check if externalIds map is nil when add node as gw for centralized subnet (#2088)
 * [6a392dfa2](https://github.com/kubeovn/kube-ovn/commit/6a392dfa2b43734984df231e1d17a2a123b86e45) fix ovs bridge not deleted cause by port link not found (#2084)
 * [14c9840f4](https://github.com/kubeovn/kube-ovn/commit/14c9840f42d026dc7ea4817e8d06121245355b22) fix gosec error
 * [1ce4713e5](https://github.com/kubeovn/kube-ovn/commit/1ce4713e56fa6f06563f9d12af44f25775a385b6) bump go version to 1.18
 * [c52c9f3b8](https://github.com/kubeovn/kube-ovn/commit/c52c9f3b863dd8780f373b4a6da7bdc269a8fe49) fix libovsdb issues (#2070)
 * [c97b1f1d9](https://github.com/kubeovn/kube-ovn/commit/c97b1f1d994f88e227f47d83fa9806c47cdb4d4e) refactor: add unknown config to lsp (#2076)
 * [d1bd047b4](https://github.com/kubeovn/kube-ovn/commit/d1bd047b41507510b7097e524f706ac367855726) fix: replace replace with add to override existing route (#2061)
 * [06d22315e](https://github.com/kubeovn/kube-ovn/commit/06d22315ee13da4add9289e6194dbfb55e7c66dd) fix OVN LS/LB gc (#2069)
 * [3200e272a](https://github.com/kubeovn/kube-ovn/commit/3200e272aa6b4b77552dc31305e6148208ccd497) update ipv6 address for vpc peer (#2060)
 * [f90245403](https://github.com/kubeovn/kube-ovn/commit/f90245403d312a37e383224102afba4afe20df0e) perf: reduce controller init time (#2054)
 * [7ca28c9dd](https://github.com/kubeovn/kube-ovn/commit/7ca28c9dda26a4b691c39cc7ca5e9c0606f6c262) pass klog verbosity to libovsdb (#2048)
 * [6872bfd20](https://github.com/kubeovn/kube-ovn/commit/6872bfd208a9fa78a07a1272a0c3e7824c451250) use the latest base image
 * [bcd42d2ae](https://github.com/kubeovn/kube-ovn/commit/bcd42d2ae32641707737d52d52ecb6a5288a13c3) ovs: fix reaching resubmit limit in underlay (#2038)
 * [b45ee71f4](https://github.com/kubeovn/kube-ovn/commit/b45ee71f4acf2d8d7ff6c59963ce81f2377914b1) fix: vpc and vpc nat gw not clean (#2032)

### Contributors

 * Mengxin Liu
 * bobz965
 * changluyi
 * hzma
 * lut777
 * zhangzujian
 * 张祖建

## v1.9.14 (2022-11-11)

 * [9581d06bf](https://github.com/kubeovn/kube-ovn/commit/9581d06bfd66b835fe339b800efae2874c48638d) set release for 1.9.14
 * [6ba9954f6](https://github.com/kubeovn/kube-ovn/commit/6ba9954f63f9264cba3e5a4345ccf6bc91e317d2) fix pinger namespace error (#2034)
 * [0c9fd3f0e](https://github.com/kubeovn/kube-ovn/commit/0c9fd3f0e8f9b915c22db07cdabbc9d28887e397) prepare release for 1.9.14
 * [9cbb07a61](https://github.com/kubeovn/kube-ovn/commit/9cbb07a61c3c59f4a4d0ffa31221314f1cd74876) fix: gateway route should stay still when node is pingable (#2011)
 * [ab2a1f122](https://github.com/kubeovn/kube-ovn/commit/ab2a1f1222cae49f230131ed693381851b51ae9e) update np name with character prefix (#2024)
 * [ec4fe0223](https://github.com/kubeovn/kube-ovn/commit/ec4fe0223a75145a13817d88ebc26ee41d8cec1c) bump kind and node image versions (#2023)
 * [5f9dca931](https://github.com/kubeovn/kube-ovn/commit/5f9dca9314f51860164c382697479af4307e2bd3) fix ovn nb/sb health check (#2019)
 * [d7e78b8a3](https://github.com/kubeovn/kube-ovn/commit/d7e78b8a3682e68773bb4ce17ec8005b6c88d02b) fix ovs fdb for the local bridge port (#2014)
 * [d41c467a4](https://github.com/kubeovn/kube-ovn/commit/d41c467a4284d3dbb5fecbbf56983d9170441099) do not need to delete pg when update networkpolicy (#1959)
 * [523105954](https://github.com/kubeovn/kube-ovn/commit/52310595479730b4ea53459501afbc8d10d07d26) add helm and e2e test (#1992)
 * [85b8dd669](https://github.com/kubeovn/kube-ovn/commit/85b8dd6690350bbe25e2c9ebb427d736c0d2af8b) add check of write to ovn sb db for ovn-controller (#1989)

### Contributors

 * Noah
 * hzma
 * lut777
 * zhangzujian
 * 张祖建

## v1.9.13 (2022-10-26)

 * [354d6217e](https://github.com/kubeovn/kube-ovn/commit/354d6217e6aa9891e559e880ec92b73530f8dbba) update ovs version to branch-2.16 (#1988)
 * [574f31fdf](https://github.com/kubeovn/kube-ovn/commit/574f31fdff177d139c49876db725b688dbc2bb55) fix grep matching device in routes (#1986)
 * [8fa0fa34f](https://github.com/kubeovn/kube-ovn/commit/8fa0fa34f41f5eed83fd498e154124ae3fd6b594) delete pod after TerminationGracePeriodSeconds (#1984)
 * [1f7b58d42](https://github.com/kubeovn/kube-ovn/commit/1f7b58d422110700047117a8545ccf66153b6de7) ovs: fix waiting flows in underlay networking (#1983)
 * [2506a4dfe](https://github.com/kubeovn/kube-ovn/commit/2506a4dfeeccb05ec34ad90fb471ce900e0f3be7) use latest base image
 * [1c6ea0357](https://github.com/kubeovn/kube-ovn/commit/1c6ea035795bb083ddfe3be102bd01de77f1db48) ovn db: recover automatically on startup if db corruption is detected (#1980)
 * [d7aabe2cb](https://github.com/kubeovn/kube-ovn/commit/d7aabe2cb9d98259a9d447dbe5c204b3836e9941) prepare for release 1.9.13
 * [adda63c05](https://github.com/kubeovn/kube-ovn/commit/adda63c056a84808110172e3efce20d71cb99f11) fix CVE-2022-32149
 * [6ffaa44f9](https://github.com/kubeovn/kube-ovn/commit/6ffaa44f956d38955c5889da2066f806e1134344) avoid concurrent subnet status update (#1976)
 * [f07545870](https://github.com/kubeovn/kube-ovn/commit/f075458703d478c9ba8811e8e676dc07c07811c8) upgrade ovs-ovn pod by generation version instead of chart version (#1960)
 * [78d9cfd38](https://github.com/kubeovn/kube-ovn/commit/78d9cfd38e8ce593660df55c35ba79e7eb6037fc) fix metrics name (#1977)
 * [1aaa6e486](https://github.com/kubeovn/kube-ovn/commit/1aaa6e486a922a85a8e75898daccf7b5663bdc1a) add vm pod to ipam by ip when initIPAM (#1974)
 * [d7ac1503d](https://github.com/kubeovn/kube-ovn/commit/d7ac1503d6b3bf9f67227449d1d94a171507feee) validate nbctl socket path in start-controller.sh
 * [e6adb1e15](https://github.com/kubeovn/kube-ovn/commit/e6adb1e151aff310ff5695677b7b7f737ece61e3) skip CVE-2022-3358 (#1972)
 * [b4fe883c9](https://github.com/kubeovn/kube-ovn/commit/b4fe883c9db72ce8708b83dee8c897ac7005a48d) use latest base image
 * [b3a1cf65a](https://github.com/kubeovn/kube-ovn/commit/b3a1cf65ac30fca85afee17fe7ed0ae398ef35a1) fix: add  default deny acl (#1935)
 * [903eff030](https://github.com/kubeovn/kube-ovn/commit/903eff03078707b42275412df0e2ddea4ba777fe) ovs: fix mac learning in environments with hairpin enabled (#1943)
 * [aa50a2ef3](https://github.com/kubeovn/kube-ovn/commit/aa50a2ef3d16bb26b462cdc3edcadc22accb5989) Fix registry for ovn-central container in install.sh (#1951)
 * [e9a1af07c](https://github.com/kubeovn/kube-ovn/commit/e9a1af07c4069253cac1f1ea43da9ff8d6cc530a) ovs: add fdb update logging (#1941)

### Contributors

 * Mengxin Liu
 * hzma
 * lut777
 * runzhliu
 * zhangzujian
 * 张祖建
 * 马洪贞

## v1.9.12 (2022-09-29)

 * [42c2a82c9](https://github.com/kubeovn/kube-ovn/commit/42c2a82c959feea76f3968f5931f8116c85747ec) add chart version check when upgrade ovs-ovn pod
 * [04338c846](https://github.com/kubeovn/kube-ovn/commit/04338c84679481855a75ded2cc251fa656d14d5d) fix underlay e2e testing (#1929)
 * [6c710acbc](https://github.com/kubeovn/kube-ovn/commit/6c710acbca0ba71f7747eedc3d05804860174a3e) prepare for release v1.9.12
 * [4f2f4058b](https://github.com/kubeovn/kube-ovn/commit/4f2f4058b52319c85973ea7f0777eadb73c2ff3a) set leader flag when get leader
 * [495e16320](https://github.com/kubeovn/kube-ovn/commit/495e163207a66797899c941e2c7a075a9cf1f5c9) set ovsdb-server vlog level to avoid warnings caused by ovs-vsctl (#1937)
 * [5f23adc6d](https://github.com/kubeovn/kube-ovn/commit/5f23adc6dd7f1d8c9f5205527d48a5b010c59085) use leases for leader election (#1529)

### Contributors

 * 张祖建
 * 马洪贞

## v1.9.11 (2022-09-21)

 * [44cee1df1](https://github.com/kubeovn/kube-ovn/commit/44cee1df14d03db368b8efca2bf1a0bae80537b2) prepare release 1.9.11
 * [078192187](https://github.com/kubeovn/kube-ovn/commit/078192187084f87c0f7f56e4d8a04310276d004c) fix: pod mistaken ls label (#1925)
 * [ff176b894](https://github.com/kubeovn/kube-ovn/commit/ff176b894794c6c36ae9ed6ac45250284c082cad) ignore pod without lsp when add pod to port-group
 * [6df23c2b6](https://github.com/kubeovn/kube-ovn/commit/6df23c2b668a3b39338eabdc1da84985c0b47b88) add network partition check in ovn probes
 * [270e9dc39](https://github.com/kubeovn/kube-ovn/commit/270e9dc39867957a301e1cf08654883c49a3eaef) feat: Replace command health check with k8s tcpSocket check (#1251)
 * [64c41a5d8](https://github.com/kubeovn/kube-ovn/commit/64c41a5d8c2b2975ba0fd6a136f3506b9e17aeba) fix CVE-2022-27664
 * [ed8ba4c68](https://github.com/kubeovn/kube-ovn/commit/ed8ba4c6835554e1dea98e4e03cc75f2b5586be5) update ns annotation when subnet cidr changed (#1921)

### Contributors

 * hzma
 * lut777
 * zhangzujian
 * 尚墨
 * 马洪贞

## v1.9.10 (2022-09-13)

 * [f7a62ca77](https://github.com/kubeovn/kube-ovn/commit/f7a62ca77935da4d04a8151000976db87fddd678) set release 1.9.10
 * [f9f49266a](https://github.com/kubeovn/kube-ovn/commit/f9f49266a13c41c876f0d7ae08c358cae459ae9c) prepare for release 1.9.10
 * [455863a0d](https://github.com/kubeovn/kube-ovn/commit/455863a0d3c91ef5f3fee6b309fc1aa7eb47d966) fix: gatewaynode might be null (#1896)
 * [23756538b](https://github.com/kubeovn/kube-ovn/commit/23756538bd10bec3eb5c275b3e608c82889b1c15) fix: api rollback
 * [0522d9eb4](https://github.com/kubeovn/kube-ovn/commit/0522d9eb4cc806b7c9461f0421e24034147cc4fb) fix: diskfull may lead to wrong raft status for ovs db (#1635)
 * [23def0a2e](https://github.com/kubeovn/kube-ovn/commit/23def0a2e1cfc1d9410abe3e39e68d9281a29fa0) kubectl-ko: turn off pipefail for ovn leader check (#1891)
 * [451c88abf](https://github.com/kubeovn/kube-ovn/commit/451c88abf6ea791946e1c8e2be082de226795f00) fix logrotate issues
 * [a98cffa4a](https://github.com/kubeovn/kube-ovn/commit/a98cffa4a9a4e7e34e8dcef2e318193b30cfb2a5) fix security issues
 * [493b42ded](https://github.com/kubeovn/kube-ovn/commit/493b42ded518a7ba7da9f996bb962da1f88331ee) security: conform to gosec G114 (#1860)
 * [ceb3855e1](https://github.com/kubeovn/kube-ovn/commit/ceb3855e1e8945cd8ead48ae67efe3f6429f3990) fix duplicate logs for leader election (#1886)
 * [7ae439b0c](https://github.com/kubeovn/kube-ovn/commit/7ae439b0c3f0c9a63d99fcc54d0bd5d6b4675d52) delete and recreate netem qos when update process (#1872)

### Contributors

 * Mengxin Liu
 * hzma
 * lut777
 * zhangzujian
 * 尚墨
 * 张祖建

## v1.9.9 (2022-08-30)

 * [c4701fd29](https://github.com/kubeovn/kube-ovn/commit/c4701fd291b14424fc507cae2be9a76d1de918a8) set release 1.9.9
 * [33d027af6](https://github.com/kubeovn/kube-ovn/commit/33d027af66b7c89e18ca6f6c2e5cb416240e4093) feat: reduce downtime by increasing arp cache timeout
 * [b90769f31](https://github.com/kubeovn/kube-ovn/commit/b90769f31c5b3744a429a0ee2f7dee4de92480a3) feat: reduce wait time by counting the flow num.
 * [2afbe4089](https://github.com/kubeovn/kube-ovn/commit/2afbe4089ac297d0b58149d6c047a3ada27d05ed) fix: missing stop_ovn_daemon args
 * [37b9f2f85](https://github.com/kubeovn/kube-ovn/commit/37b9f2f85d6c8ea143c811de46ee746f47242a75) delete log severity for drop acl when update networkpolicy
 * [82026bbd6](https://github.com/kubeovn/kube-ovn/commit/82026bbd699d4af78903411cd50a85c3ef07395d) base: use patch from OVN upstream (#1844)
 * [f9a2d8de2](https://github.com/kubeovn/kube-ovn/commit/f9a2d8de2ec233b47ba8a1b19917ae84b3d34f87) prepare release for 1.9.9
 * [7138087c5](https://github.com/kubeovn/kube-ovn/commit/7138087c5b4286e676f123bc8f98c59a993ec572) ovs: fix log file descriptor leak in monitor process (#1855)
 * [c6f9565cb](https://github.com/kubeovn/kube-ovn/commit/c6f9565cb917e6b15f289337fafde75d03758b60) fix ovs-ovn logging (#1848)
 * [b3a6998e4](https://github.com/kubeovn/kube-ovn/commit/b3a6998e41d6bbe45f2f3842313d2c55459c2384) fix: add and set ENABLE_KEEP_VM_IP=true to keep vm ip (#1702)
 * [20ed2329e](https://github.com/kubeovn/kube-ovn/commit/20ed2329e0b41f30b993064fd4a73a1ef4645421) fix: multus macvlan ipvlan use kube-ovn ipam，but  ip not inited in init-ipam (#1843)
 * [4c40a20d9](https://github.com/kubeovn/kube-ovn/commit/4c40a20d91728d27f2fb57b090cefef1170d3052) fix underlay e2e (#1828)
 * [eb1706bc1](https://github.com/kubeovn/kube-ovn/commit/eb1706bc1977fe0055a826311b4e7b4554b287de) fix arping error log (#1841)
 * [5757b8ece](https://github.com/kubeovn/kube-ovn/commit/5757b8ecef6ced353f68860025f38e28a710d5e1) ko: fix kube-proxy check (#1842)
 * [2000e996c](https://github.com/kubeovn/kube-ovn/commit/2000e996c5cdc4cc923005094126324c29f98a63) ci: switch environment to ubuntu-20.04 (#1838)
 * [919bb236f](https://github.com/kubeovn/kube-ovn/commit/919bb236fe5f5072cdb18d8224865ce638cdffc7) update centralized subnet gateway ready patch operation (#1827)
 * [1c3b622cb](https://github.com/kubeovn/kube-ovn/commit/1c3b622cb270b5d78816db80c0a5b233ded6586f) fix duplicate log for tunnel interface decision (#1823)
 * [e4d53217d](https://github.com/kubeovn/kube-ovn/commit/e4d53217da145e7c74d7a26158941ca0f708ecfc) update centralize subnet gatewayNode until gw is ready (#1814)
 * [d44de3e06](https://github.com/kubeovn/kube-ovn/commit/d44de3e06f1b5682ab9863cfe8cb091ed6f955c3) initialize IPAM from IP CR with empty PodType for sts Pods (#1812)
 * [3eb1d1ad1](https://github.com/kubeovn/kube-ovn/commit/3eb1d1ad16fb6c9a3e75b2ef9b5486e03b4e1c79) kubectl-ko: fix missing env-check (#1804)
 * [5613b63c2](https://github.com/kubeovn/kube-ovn/commit/5613b63c26890825c26e606f8d6b8b5539979e0d) kubectl-ko: fix destination mac (#1801)
 * [1284f15d6](https://github.com/kubeovn/kube-ovn/commit/1284f15d627ce8d6903553b81c63f9f12980ba27) abort kube-ovn-controller on leader change (#1797)
 * [5bf8de0fc](https://github.com/kubeovn/kube-ovn/commit/5bf8de0fcb9a416552bf449677c08e95143ffec5) avoid invalid ovn-nbctl daemon socket path (#1799)
 * [4680e6323](https://github.com/kubeovn/kube-ovn/commit/4680e632388585430b9033af2c013d4e721e51ea) update vpc-nat-gateway base
 * [4cce7870d](https://github.com/kubeovn/kube-ovn/commit/4cce7870db4de63dce59b554b233ad9eb0feaf4a) fix: warning for empty chassis fixed (#1786)

### Contributors

 * Mengxin Liu
 * bobz965
 * hzma
 * lut777
 * zhangzujian
 * 张祖建

## v1.9.8 (2022-08-10)

 * [686d913c2](https://github.com/kubeovn/kube-ovn/commit/686d913c21d56d6f2a5bb2e6446de7fa2a8f5dc9) set release v1.9.8
 * [8de356930](https://github.com/kubeovn/kube-ovn/commit/8de356930fdaebce8136c4f6f033cad8db4815c5) prepare for release v1.9.8
 * [38ee83014](https://github.com/kubeovn/kube-ovn/commit/38ee83014556702604e77071870ca7f06fde0a43) delete htb qos when releated annotation is deleted (#1788)
 * [85bd5f94b](https://github.com/kubeovn/kube-ovn/commit/85bd5f94b2c2a8ce81452caffc6c6099e1b5504b) perf: fix memory leak
 * [46c970d6d](https://github.com/kubeovn/kube-ovn/commit/46c970d6dcab21e64b62cf13e0e4a285a734a96e) perf: disable mlockall to reduce memory usage
 * [d7fd3793e](https://github.com/kubeovn/kube-ovn/commit/d7fd3793e646c9dc5bbcef40a633a7baa61696df) perf: reduce metrics labels (#1784)
 * [d7a9f5e91](https://github.com/kubeovn/kube-ovn/commit/d7a9f5e91c5a44deb9801e4538386512e45da627) feature: support exchange link names of OVS bridge and provider nic in underlay networks (#1736)
 * [b966dd596](https://github.com/kubeovn/kube-ovn/commit/b966dd596c4d8898a267bf33e7e85d2b8144da00) perf: replace jemalloc to reduce memory usage (#1764)
 * [8bb8b1735](https://github.com/kubeovn/kube-ovn/commit/8bb8b17355b91456bde7535747c13ae937e0a894) fix: add omitempty to subnet spec (#1765)
 * [fd6764377](https://github.com/kubeovn/kube-ovn/commit/fd67643772e7b1e9ea1c5a39f4a7d3356fe853a8) set sysctl variables on cni server startup (#1758)
 * [7c6250f3f](https://github.com/kubeovn/kube-ovn/commit/7c6250f3fa69a7843e03791bb4eb268497874d4c) avoid patch interface deletion & recreation during restart (#1741)
 * [a91056a3c](https://github.com/kubeovn/kube-ovn/commit/a91056a3caff58c39f93bd87f5b677f5d50ac62a) enqueue subnets after vpc update (#1722)
 * [e895c5ff0](https://github.com/kubeovn/kube-ovn/commit/e895c5ff0062d0730b7dfb2abc120d782afa8907) do not add subnet not processed by kube-ovn to vpc (#1735)
 * [f13f3f462](https://github.com/kubeovn/kube-ovn/commit/f13f3f4621e8788caaf18751fb7766cd3ad7d3cd) add logrotate for kube-ovn log (#1740)
 * [70246fb9a](https://github.com/kubeovn/kube-ovn/commit/70246fb9ac6ecb7e38e04274e7fc043fd809bd88) fix: If pod has snat or eip, also need delete staticRoute when delete pod. (#1731)
 * [76e3c670e](https://github.com/kubeovn/kube-ovn/commit/76e3c670e75ff9cceef38a291b26c70014ff143a) fix iptables for service traffic when external traffic policy set to local(#1725)
 * [cee392133](https://github.com/kubeovn/kube-ovn/commit/cee392133310cb1f404f88613d2c8e3eaa4018aa) optimize lrp create for subnet in vpc (#1712)
 * [21f0b979c](https://github.com/kubeovn/kube-ovn/commit/21f0b979c38d18a5ed2abb93216b6fd3341d2d94) fix: cancel delete staticroute when it's used by NatRule (#1733)
 * [4c2d0c867](https://github.com/kubeovn/kube-ovn/commit/4c2d0c86765d6208c033df095b9a18aa3eee19fe) fix: wrong info when update subnet from dual to ipv4 or ipv6. (#1726)
 * [417176ed9](https://github.com/kubeovn/kube-ovn/commit/417176ed9bff4061720a3f6d8e86ab78c2bd42b0) fix: new ovn-ic static route method adapted due to old ovn version (#1718)

### Contributors

 * Mengxin Liu
 * hzma
 * lut777
 * xujunjie-cover
 * zhangzujian
 * 张祖建

## v1.9.7 (2022-07-18)

 * [eb412c96f](https://github.com/kubeovn/kube-ovn/commit/eb412c96ff98d50ea5fddcef30f089b11d186c51) set release 1.9.7
 * [07bec2a20](https://github.com/kubeovn/kube-ovn/commit/07bec2a203798c54f8585f1d0469eb3fb713a999) prepare for release 1.9.7
 * [a798a8c25](https://github.com/kubeovn/kube-ovn/commit/a798a8c25633e5dbc8ac72a3d90dce8147aa422a) Get latest vpc data from apiserver instead of cache (#1684)
 * [8bc1b1697](https://github.com/kubeovn/kube-ovn/commit/8bc1b1697145ed4dc488588c01862c8f20949a90) update priority range in htb qos (#1688)
 * [ef4673d20](https://github.com/kubeovn/kube-ovn/commit/ef4673d204a83dc7d98ace69b55007fbed265d7e) add upgrade-ovs script (#1681)

### Contributors

 * Mengxin Liu
 * Wang Bo
 * hzma

## v1.9.6 (2022-07-13)

 * [6db04118e](https://github.com/kubeovn/kube-ovn/commit/6db04118eb5885dfcf3ce9aa0f584c1d5cab84da) set release 1.9.6
 * [885e41f6a](https://github.com/kubeovn/kube-ovn/commit/885e41f6ae43084feb7cfd850e7619e4a1ba7911) prepare for release 1.9.6
 * [556a2cf83](https://github.com/kubeovn/kube-ovn/commit/556a2cf83af6f2dffdce61393d128aaa7c190e13) shim: fix diffs of commits
 * [67da728ad](https://github.com/kubeovn/kube-ovn/commit/67da728ad6e72ebb7af4d2101b07939dfc7c2465) fix: change ovn-ic static route to policy (#1670)
 * [a7a11f030](https://github.com/kubeovn/kube-ovn/commit/a7a11f0301adc92a4f4d0513bd393c8a5ccded22) fix: Do not Recreate Logical_Router_Port when Vpc recreated (#1570)
 * [e2ab703a4](https://github.com/kubeovn/kube-ovn/commit/e2ab703a4bc0fcbdba564275eb7631e08ab4fc38) feat: vpc peering connection
 * [7699a34bb](https://github.com/kubeovn/kube-ovn/commit/7699a34bb6b3400227abba6082f855aad7a32e04) Delete pod if subnet of the pod's owner(sts/vm) updated (#1678)
 * [02e8973a2](https://github.com/kubeovn/kube-ovn/commit/02e8973a22e153b49b45607010187add66d38962) security: disable pprof by default (#1672)
 * [0242b9c2a](https://github.com/kubeovn/kube-ovn/commit/0242b9c2ade5bce9275dd24f58c050fbe2ccbe91) bgp: consolidate service check and use service const (#1674)
 * [3401d933b](https://github.com/kubeovn/kube-ovn/commit/3401d933b8ceb1762f4e4675a32ea5bf38a43459) fix bgp: sync service cache (#1673)
 * [f818ca5c7](https://github.com/kubeovn/kube-ovn/commit/f818ca5c782c71387e8a7386190a2b4d54f54293) fix libovsdb (#1664)
 * [a11feff7e](https://github.com/kubeovn/kube-ovn/commit/a11feff7e3b4f98d83b1149433f4b9c257897c54) mount modules for auto load ip6tables moudles (#1665)
 * [2882cafc1](https://github.com/kubeovn/kube-ovn/commit/2882cafc1a0b94176bd3bf3d34813a39272bbcfb) ignore pod not scheduled when reconcile subnet (#1666)
 * [91dfbbf44](https://github.com/kubeovn/kube-ovn/commit/91dfbbf44c50e9f05e0f08e4cebf6e26c589e078) fix get security group name by external_ids (#1663)
 * [e56d581b8](https://github.com/kubeovn/kube-ovn/commit/e56d581b8778ad206fe936cd58abbfc008e26ae1) add policy route when add subnet

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

 * [8a2cc7418](https://github.com/kubeovn/kube-ovn/commit/8a2cc7418191fa5268779ac62da2d1d7405236d4) set for release 1.9.5
 * [9935ab544](https://github.com/kubeovn/kube-ovn/commit/9935ab544d44566c827339e2161049907f73ffc1) fix: no need routed when use v1.multus-cni.io/default-network (#1652)
 * [60d33ca97](https://github.com/kubeovn/kube-ovn/commit/60d33ca97749b75980a678762de597a0e4e7b097) prepare for release 1.9.5
 * [a48e64ae4](https://github.com/kubeovn/kube-ovn/commit/a48e64ae469e01b7de308667f61dd69f05586954) CI: delete resources in order to avoid a long time waiting for subnet deletions. (#1643)
 * [502a7a004](https://github.com/kubeovn/kube-ovn/commit/502a7a00480de870e3de33dca5517c523835989b) set networkpolicy log default to false (#1633)
 * [0bda2e6f6](https://github.com/kubeovn/kube-ovn/commit/0bda2e6f6aceae063ec22a972bec7d00d2764491) update policy route when join subnet cidr changed (#1638)
 * [3cfafe40d](https://github.com/kubeovn/kube-ovn/commit/3cfafe40d35cfda70782c857a107918016ce22c6) ci: update trivy options (#1637)
 * [71dba393d](https://github.com/kubeovn/kube-ovn/commit/71dba393dd75fbc9726cdcce12fcf5bbb89f1d46) increase initial delay of ovs-ovn liveness probe (#1634)
 * [cf0bbd921](https://github.com/kubeovn/kube-ovn/commit/cf0bbd9212c4438f8144d56f99a4f65a55550c94) wait ovn-central pods running before delete ovs-ovn pods (#1627)
 * [0877c3a75](https://github.com/kubeovn/kube-ovn/commit/0877c3a753bfd3a85c4c4f67b7af3f38de38ed5a) get dbstatus for all ovn-central pod (#1619)
 * [51c409bdc](https://github.com/kubeovn/kube-ovn/commit/51c409bdc5e285154e007e085c15e098ee98dc81) fix issues about OVN policy routing
 * [637503b46](https://github.com/kubeovn/kube-ovn/commit/637503b46f9429fef62b81cd6585796ca8255fad) use policy route instead of static route (#1618)

### Contributors

 * hzma
 * xujunjie-cover
 * zhangzujian
 * 张祖建

## v1.9.4 (2022-06-19)

 * [c85ab2032](https://github.com/kubeovn/kube-ovn/commit/c85ab20329420f1e494a1d1d5810581102ca3316) ci: disable cilium e2e for release
 * [0a841aa10](https://github.com/kubeovn/kube-ovn/commit/0a841aa10a67b7b33b35fe59a75d106688bc874b) prepare for release 1.9.4
 * [f99f4e815](https://github.com/kubeovn/kube-ovn/commit/f99f4e815f4f886859444043f77f882980a6d722) update ovs health check, delete connection to ovn sb db (#1588)
 * [82d7dd37b](https://github.com/kubeovn/kube-ovn/commit/82d7dd37b4a23bb9d7abea63c33a3c473673e0f4) fix: all cluster pod will be in podadd queue (#1587)
 * [3c68cb9bb](https://github.com/kubeovn/kube-ovn/commit/3c68cb9bbba5f3d1175a3cbcdd6c08d0e196e49a) fix pod could not be ready (#1562)
 * [f39ff7a8b](https://github.com/kubeovn/kube-ovn/commit/f39ff7a8b74aefecd15c67ae1d7e62cdeae27692) fix: delete pod panic when delete vm or statefulset. (#1565)
 * [4c60872fc](https://github.com/kubeovn/kube-ovn/commit/4c60872fcbf49cd396d31597677ab3ca8a07e0bc) fix: keep vm's and statefulset's ips when user specified subnet (#1520)
 * [81781a011](https://github.com/kubeovn/kube-ovn/commit/81781a0117a37652e630baef441dc4c9edf0128c) do not gc vm pod lsp when vm still exists (#1558)
 * [4a28c0149](https://github.com/kubeovn/kube-ovn/commit/4a28c0149292576ab15550d4a1fce4e2ba24d52f) fix exec cmd in vpc nat gateway (#1556)
 * [67db2bf31](https://github.com/kubeovn/kube-ovn/commit/67db2bf3158e951e003052a5a8a5b1a38b7aa0be) CNI: do not return route if nic is not eth0 (#1555)
 * [d5fce51d2](https://github.com/kubeovn/kube-ovn/commit/d5fce51d2ccb2904e9162d5905fde0380f4ae782) exit kube-ovn-controller on stopped leading (#1536)
 * [05a4b4dc1](https://github.com/kubeovn/kube-ovn/commit/05a4b4dc1ca7a94259f080fdc8ddb9a46126c045) remove name for default drop acl in networkpolicy (#1522)
 * [6fcc19756](https://github.com/kubeovn/kube-ovn/commit/6fcc19756bb98035082ec51f112c279bfb694f88) tmp cancel cilium external svc test (#1531)
 * [fe3bb3e53](https://github.com/kubeovn/kube-ovn/commit/fe3bb3e53721437740b8895072a2c572b4ae1c16) move dumb-init from base images to kube-ovn image

### Contributors

 * hzma
 * lut777
 * xujunjie-cover
 * 刘睿华
 * 张祖建

## v1.9.3 (2022-05-13)

 * [a2ba0c150](https://github.com/kubeovn/kube-ovn/commit/a2ba0c1503d56110084123591c8ff52f964bcd52) release 1.9.3
 * [0695d31e2](https://github.com/kubeovn/kube-ovn/commit/0695d31e2b780f2d874e3e0caf95d89f6346a8c1) fix defunct ovn-nbctl daemon
 * [f8594a29e](https://github.com/kubeovn/kube-ovn/commit/f8594a29eb5c7dbdf6887af081cfa32db35c3cb8) optimize ovs request in cni (#1518)
 * [08f2961d9](https://github.com/kubeovn/kube-ovn/commit/08f2961d98bd2a48f6b570f54329265f4d12fbff) optimize node port-group check (#1514)
 * [9ec4a4301](https://github.com/kubeovn/kube-ovn/commit/9ec4a43019e088b80f7c863345a25e443a4dca80) reduce ovs-ovn restart downtime (#1516)
 * [b55fa9876](https://github.com/kubeovn/kube-ovn/commit/b55fa98765d63ced385ca20dbd7b2ee3a479d886) prepare for release 1.9.3
 * [e4ba2e6dd](https://github.com/kubeovn/kube-ovn/commit/e4ba2e6ddb6394890e182013d3a848e7957a5262) fix: ovs trace flow always ends with controller action (#1508)
 * [2e681af36](https://github.com/kubeovn/kube-ovn/commit/2e681af36687db2422ca15af52e6e65bd1181275) optimize IPAM initialization
 * [76fe9cef2](https://github.com/kubeovn/kube-ovn/commit/76fe9cef23464e70a9399ea4e5031dc3bbe7b6fb) ci: skip some checks
 * [51dc92431](https://github.com/kubeovn/kube-ovn/commit/51dc92431a748a2f2453870c7629a1f6083384d5) delete ipam record and static route when gc lsp (#1490)

### Contributors

 * Mengxin Liu
 * hzma
 * zhangzujian

## v1.9.2 (2022-04-25)

 * [6273d2940](https://github.com/kubeovn/kube-ovn/commit/6273d2940a52c89f6722101b19fbb7b4aca988f1) release for v1.9.2
 * [c98322d7b](https://github.com/kubeovn/kube-ovn/commit/c98322d7b9413c991af94a46f35750d999b7476e) fix: wrong vpc-nat-gateway arm image (#1482)
 * [bc4f761ca](https://github.com/kubeovn/kube-ovn/commit/bc4f761ca57059875b3eb6d155cc0fce93b5563c) add delete ovs pods after restore nb db (#1474)
 * [945f23366](https://github.com/kubeovn/kube-ovn/commit/945f233661bde2b8626763ae1735a313f10c142b) delete monitor noexecute toleration (#1473)
 * [35ecc687d](https://github.com/kubeovn/kube-ovn/commit/35ecc687dc6717d9199e199d792b2851db08f908) add env-check (#1464)
 * [1f68e12a5](https://github.com/kubeovn/kube-ovn/commit/1f68e12a5ca03def17e64057d945ba796e9de957) append metrics (#1465)
 * [302156bcb](https://github.com/kubeovn/kube-ovn/commit/302156bcb05f54a99891a3aac5715154ba78167e) masquerade packets from Pods to service IP
 * [4faa88311](https://github.com/kubeovn/kube-ovn/commit/4faa88311d5988af2604456654a20585d9a9a0ae) add kube-ovn-controller switch for EIP and SNAT
 * [300a16437](https://github.com/kubeovn/kube-ovn/commit/300a16437bcc25630c35f34846654f5de2d1736e) ignore cni cve
 * [75383df31](https://github.com/kubeovn/kube-ovn/commit/75383df313aa5dae97ab8192fcc2aa8305b40dbe) add routed check in circulation (#1446)
 * [c4f5f4d67](https://github.com/kubeovn/kube-ovn/commit/c4f5f4d67b8c195c8c9f01bff9ebe07172db9973) modify init ipam by ip crd only for sts pod (#1448)
 * [135798dcc](https://github.com/kubeovn/kube-ovn/commit/135798dcce63b532fcdb40f1eb67f476737dd19f) log: show the reason if get gw node failed (#1443)
 * [9bec51be9](https://github.com/kubeovn/kube-ovn/commit/9bec51be9768f0e8c78204133aff7fb5ca7f90cb) modify webhook img to independent image (#1442)
 * [e1d6dbf68](https://github.com/kubeovn/kube-ovn/commit/e1d6dbf6808755e9cb624485054c711ef61a3d5d) support keep-vm-ip and live-migrate at the same time (#1439)
 * [613b6ae54](https://github.com/kubeovn/kube-ovn/commit/613b6ae54e80dd9361154de99b1a09ea63aec6b8) update alpine to fix CVE-2022-1271
 * [553bedd2f](https://github.com/kubeovn/kube-ovn/commit/553bedd2fab1147f1037f71399b08a093873af5a) fix adding key to delete Pod queue
 * [d899cc970](https://github.com/kubeovn/kube-ovn/commit/d899cc97021cdd5d8cbe34fdcfc3124c0e6fc745) fix IPAM initialization
 * [e159443db](https://github.com/kubeovn/kube-ovn/commit/e159443db6bd93ac32163ba6ebe7db3141784052) ignore all link local unicast addresses/routes
 * [06bd4f861](https://github.com/kubeovn/kube-ovn/commit/06bd4f861bb46dc1b3e75722de157dbd7355f5fe) fix error handling for netlink.AddrDel
 * [71e3f1193](https://github.com/kubeovn/kube-ovn/commit/71e3f119307c1549c3cf3e834fc542c9eec1adad) replace pod name when create ip crd
 * [8e65f6f60](https://github.com/kubeovn/kube-ovn/commit/8e65f6f608548e12b0f2b29af0c56b8212a47d93) support alloc static ip from any subnet after ns supports multi subnets (#1417)
 * [9bc2f96a8](https://github.com/kubeovn/kube-ovn/commit/9bc2f96a80fddbd4fa5e4d6a0cc42b58f73a33fd) fix provider-networks status
 * [269f819a3](https://github.com/kubeovn/kube-ovn/commit/269f819a36ae8c73780d54415aa8ad816a3189a4) recover ips CR on IPAM initialization
 * [dc43dc20a](https://github.com/kubeovn/kube-ovn/commit/dc43dc20a4354907051859cf7bd00d88108dfb6d) create ip crd in kube-ovn-controller (#1413)
 * [41f8e26b7](https://github.com/kubeovn/kube-ovn/commit/41f8e26b791c509220bca9e6bc2bc24eb328afab) add condition for triggering the deletion of redundant chassises in sbdb (#1411)
 * [2aedc6ac3](https://github.com/kubeovn/kube-ovn/commit/2aedc6ac39990b82ef09746bf38199037f16188e) fix: do not recreate port for terminating pods (#1409)
 * [d55564047](https://github.com/kubeovn/kube-ovn/commit/d5556404700bab6dbc1979a8b348d5d2f056906b) avoid frequent ipset update
 * [c86ff85e8](https://github.com/kubeovn/kube-ovn/commit/c86ff85e81c923e86a021ffb91ed9ee2c37171ce) fix: The underlay physical gateway config by external-gw-addr when use snat&eip (#1400)
 * [deea9ded6](https://github.com/kubeovn/kube-ovn/commit/deea9ded6b6df99508a4aa262a8a25ac2ea67cfe) add reset for kube-ovn-monitor metrics (#1403)
 * [899de6ffc](https://github.com/kubeovn/kube-ovn/commit/899de6ffc52776405a647de1edd2a07aba5deedc) check the cidr format whether is correct (#1396)
 * [b54364b46](https://github.com/kubeovn/kube-ovn/commit/b54364b469b8d7a177206d98384a117521b8b701) update dockerfile to use v1.9.1 base img
 * [241905010](https://github.com/kubeovn/kube-ovn/commit/2419050109c52b0ceb839fec135acfaf5905cc89) append vm deletion check
 * [1953712a4](https://github.com/kubeovn/kube-ovn/commit/1953712a41349abc35301c338a706d5d59338ec8) delete repeat para
 * [7c0348a77](https://github.com/kubeovn/kube-ovn/commit/7c0348a777212ae50bb566f8824cc1325185bdbe) update nodeips for restore cmd in ko plugin
 * [f320ef8fa](https://github.com/kubeovn/kube-ovn/commit/f320ef8fa07fca8f2a5e6a68bfec8ebb130d51ca) fix external egress gateway
 * [c3e17d8c0](https://github.com/kubeovn/kube-ovn/commit/c3e17d8c0df8f55da405957356b48200f057f255) add missing link scope routes in vpc-nat-gateway
 * [9d9d58784](https://github.com/kubeovn/kube-ovn/commit/9d9d58784d6476866c51c306e27d52c1ab4af253) increase memory limit of ovn-central
 * [c4092113f](https://github.com/kubeovn/kube-ovn/commit/c4092113f7650da07cc459fe804308d127453f85) fix range loop
 * [7397db27b](https://github.com/kubeovn/kube-ovn/commit/7397db27ba346b6a1c4efed23f4af960c677ba6e) update script to add restore plugin cmd

### Contributors

 * Mengxin Liu
 * hzma
 * lut777
 * wangyd1988
 * xujunjie-cover
 * zhangzujian

## v1.9.1 (2022-03-09)

 * [46eb49adc](https://github.com/kubeovn/kube-ovn/commit/46eb49adca18cae8a352b4b5949a7250c7a1f91a) release update 1.9.1 changelog (#1361)
 * [59594fed8](https://github.com/kubeovn/kube-ovn/commit/59594fed8406d5dc75db1d1e9ee671af5ca506b7) add restore process for ovn nb db
 * [de794986c](https://github.com/kubeovn/kube-ovn/commit/de794986cc6b67ce565d778ad7a0f09d278b49dd) optimize kube-ovn-monitor yaml
 * [47a16c38f](https://github.com/kubeovn/kube-ovn/commit/47a16c38fdb751cd50af6898bcfe4313d8180f8d) add reset porocess for ovs interface metrics
 * [a3618bcd8](https://github.com/kubeovn/kube-ovn/commit/a3618bcd8912912f35b89d6663d431d294138ca3) fix SNAT/PR on Pod startup
 * [81247723d](https://github.com/kubeovn/kube-ovn/commit/81247723de608dc7948b603461341b8fd26343f9) modify ipam v6 release ip problem
 * [0006902b3](https://github.com/kubeovn/kube-ovn/commit/0006902b3ddfd04f8022aa92acd17c9073275663) skip ping gateway for pods during live migration
 * [092db7818](https://github.com/kubeovn/kube-ovn/commit/092db781867ad34e3b6f1088f04bdd3c1f7d5a4f) update flag parse in webhook
 * [222a1fb63](https://github.com/kubeovn/kube-ovn/commit/222a1fb638f3d11ef573fae5b02ba0cd41ff69d5) feat: add webhook for subnet update validation
 * [0615254ed](https://github.com/kubeovn/kube-ovn/commit/0615254edd4d502c0b1c16a8e42e77ee02d01d94) keep ip for kubevirt pod
 * [87bb7f18b](https://github.com/kubeovn/kube-ovn/commit/87bb7f18b145d0bc9f5c8fb9b13710afc77e5a21) add check for pod update process
 * [7886467ab](https://github.com/kubeovn/kube-ovn/commit/7886467ab31694b1fc4bf00ac281e22a99262490) fix ips update
 * [ab3f0a6d2](https://github.com/kubeovn/kube-ovn/commit/ab3f0a6d2be67907d0fae1d55244292142bbb0d4) append htbqos para in crd yaml
 * [a68a55f97](https://github.com/kubeovn/kube-ovn/commit/a68a55f9762a5463996ac5466d2ffa6b39c8e69c) fix: replace ecmp dp_hash with hash by src_ip (#1289)
 * [dd08ecabe](https://github.com/kubeovn/kube-ovn/commit/dd08ecabe370f818cc15680d88149a2ed0ba1d1c) fix OVS bridge with bond port in mode 6
 * [5fd56d1e1](https://github.com/kubeovn/kube-ovn/commit/5fd56d1e12f1d3f5fad9151de2fe767a1da935c1) fix: continue of deletion for del pod failed when can't found vpc or subnet (#1335)
 * [0d1149584](https://github.com/kubeovn/kube-ovn/commit/0d11495840316233b7a0b78b84ee389257085a7a) Fix usage of ovn commands
 * [621e2b571](https://github.com/kubeovn/kube-ovn/commit/621e2b571eb1fd66db305c85155e5232ac6e7559) resync provider network status periodically
 * [10ac8c3af](https://github.com/kubeovn/kube-ovn/commit/10ac8c3aff2b365fdb114132d044544fd662399b) Revert "resync provider network status periodically"
 * [fadc13162](https://github.com/kubeovn/kube-ovn/commit/fadc13162c8d17cf3ba654dd09469dbe06557ab5) fix statefulset Pod deletion
 * [b74eaccc3](https://github.com/kubeovn/kube-ovn/commit/b74eaccc33fb436fbbaffd47f4a6b31c3ebcfde7) resync provider network status periodically
 * [9a0f708fd](https://github.com/kubeovn/kube-ovn/commit/9a0f708fdc1f06ac60206c837ccc572129731b88) fix underlay subnet in custom VPC
 * [69b3d72a0](https://github.com/kubeovn/kube-ovn/commit/69b3d72a02580dc5e270c3790a02e5be24f0916c) append add cidr and excludeIps annotation for namespace
 * [c63cb1067](https://github.com/kubeovn/kube-ovn/commit/c63cb1067df23b3095b2f51ac0f7fc57ca3303d0) support to add multiple subnets for a namespace
 * [3f818b729](https://github.com/kubeovn/kube-ovn/commit/3f818b729c7ddcf7d9f8ce9a63a8caf9ca05dbcd) feat: update provider network via node annotation
 * [57f16570a](https://github.com/kubeovn/kube-ovn/commit/57f16570ad32d3f25b08b9f54db09005f0a84841) fix: only log matched svc with np (#1287)
 * [288c5fe9e](https://github.com/kubeovn/kube-ovn/commit/288c5fe9e9492df75a33b4faa24b536c03673863) transfer IP/route earlier in OVS startup
 * [4c4390b36](https://github.com/kubeovn/kube-ovn/commit/4c4390b36ebfc056e458d4c473158b29a192f437) add metric for ovn nb/sb db status
 * [92e7b975a](https://github.com/kubeovn/kube-ovn/commit/92e7b975a9caffb97484bb8bf7fda8306d18f8be) check static route conflict
 * [67a7d85ba](https://github.com/kubeovn/kube-ovn/commit/67a7d85baec9cb90d8840fabc9ea23d7fd8520d6) set up tunnel correctly in hybrid mode
 * [eabed9ccd](https://github.com/kubeovn/kube-ovn/commit/eabed9ccdb39191d3144ef4e6e88e570bc014c02) fix clusterrole in ovn-ha.yaml
 * [65b832196](https://github.com/kubeovn/kube-ovn/commit/65b8321962558ff94e6b303b2b7b6d4c2e036b3a) add gateway check after update subnet
 * [f3f8c4dc1](https://github.com/kubeovn/kube-ovn/commit/f3f8c4dc17f3f260c61e2c6add90fff9b65fd0db) fix: validate statefulset pod by name
 * [b5544bc3c](https://github.com/kubeovn/kube-ovn/commit/b5544bc3cbf8bf614e56070ffa86cf04685b3532) add back centralized subnet active-standby mode

### Contributors

 * Mengxin Liu
 * chestack
 * hzma
 * lut777
 * xujunjie
 * xujunjie-cover
 * zhangzujian

## v1.9.0 (2022-01-12)

 * [e4d48df38](https://github.com/kubeovn/kube-ovn/commit/e4d48df38d6ed16acb77d92d66686df7d40f55ea) prepare for release 1.9.0
 * [c830594dc](https://github.com/kubeovn/kube-ovn/commit/c830594dc5b9575531b34eea358bad019d0ff3a5) fix: liveMigration with IPv6
 * [e52b68976](https://github.com/kubeovn/kube-ovn/commit/e52b689764cad9166f6b499b1757b3e76ee4a765) update networkpolicy port process
 * [851ad0ce6](https://github.com/kubeovn/kube-ovn/commit/851ad0ce6874fe2ec1dff3ecb8ded3079ce27f18) Add args to configure port ln-ovn-external
 * [5d95d6285](https://github.com/kubeovn/kube-ovn/commit/5d95d62857b3a4ffdcabe2b8ae945d48d9ef1249) update check for delete statefulset pod
 * [695f45320](https://github.com/kubeovn/kube-ovn/commit/695f45320200a29948264edac201675892cd8e4d) ignore hostnetwork pod when initipam
 * [4b98d15fb](https://github.com/kubeovn/kube-ovn/commit/4b98d15fb07ab23adefae6fb23aee365e15db18a) kubectl-ko: support trace Pods being created
 * [63bc25ea8](https://github.com/kubeovn/kube-ovn/commit/63bc25ea84da3adafc16bc9a1467adb6930aa9b1) add dnsutils for base image
 * [6318d0049](https://github.com/kubeovn/kube-ovn/commit/6318d004990743c87d88ff7885d01bb1e36fd858) Add new arg to configure ns of ExternalGatewayConfig
 * [715229204](https://github.com/kubeovn/kube-ovn/commit/71522920498015847c12455728fc38d17eaab5b5) update scripts for 1.8.2
 * [960f02c15](https://github.com/kubeovn/kube-ovn/commit/960f02c15bfb2665d9d2589713a6fbbab9958a69) Optimized decision logic
 * [8974f6a37](https://github.com/kubeovn/kube-ovn/commit/8974f6a3712d14d6fd5674b449dd62f903c22f98) add svc cidr in ovs LB for optimization
 * [0192a9ae8](https://github.com/kubeovn/kube-ovn/commit/0192a9ae8851b14a81fbf6dad7b4ebb006a4c71e) add doc for gateway pod in default vpc
 * [1f9dc754c](https://github.com/kubeovn/kube-ovn/commit/1f9dc754c9fdebe089699a07fec2fc3e76e1dc12) optimize log for node port-group
 * [36d6b00a8](https://github.com/kubeovn/kube-ovn/commit/36d6b00a8fae03b1df57116befc3b101a5f348dd) fix iptables rules and service e2e
 * [8dc938d83](https://github.com/kubeovn/kube-ovn/commit/8dc938d83d923ad1ffdcbbf72e559dc9497ddeed) add kubectl-ko to docker image
 * [c4cc8f0d9](https://github.com/kubeovn/kube-ovn/commit/c4cc8f0d9b43d9ed4b5f21879263eab1e235cd61) fix: invalid syntax error
 * [a4f4cb490](https://github.com/kubeovn/kube-ovn/commit/a4f4cb490ff257063f859cead802013018517563) fix pod tolerations
 * [8611de822](https://github.com/kubeovn/kube-ovn/commit/8611de8229214b732c858175bc2822a25bfcd02b) modify pod's process of update for use multus cni as default cni
 * [5ab83ba42](https://github.com/kubeovn/kube-ovn/commit/5ab83ba42b41e4dbab912be345a42adc0fdefdd1) fix installation script
 * [09ef9be09](https://github.com/kubeovn/kube-ovn/commit/09ef9be09c3fc1809dc0d09209502cbe156c7682) add log for ecmp route
 * [791b00f42](https://github.com/kubeovn/kube-ovn/commit/791b00f42c660f1afbb0ce1d4714344558774e12) fix: ipv6 traffic still go into ct
 * [55e6a8ca3](https://github.com/kubeovn/kube-ovn/commit/55e6a8ca326c73a62c91bfd6622a200d8366d1e7) append check for centralized subnet nat process
 * [58a44fb2b](https://github.com/kubeovn/kube-ovn/commit/58a44fb2b7d90086af77095aa20cfcc48e353a36) move chassis judge to the end of node processing
 * [9f0c42fae](https://github.com/kubeovn/kube-ovn/commit/9f0c42fae734db095a12921555eb8ae140bac192) change nbctl args 'wait=sb' to 'no-wait'
 * [6f3567055](https://github.com/kubeovn/kube-ovn/commit/6f35670556dc8fa924d0b41198433e8afc78084a) use different ip crd with provider suffix for pod multus nic
 * [f7b595dcf](https://github.com/kubeovn/kube-ovn/commit/f7b595dcf67fc2222c387058b88c811e6ed1116d) fix service cidr in dual stack cluster
 * [c510b4397](https://github.com/kubeovn/kube-ovn/commit/c510b43972bb8ee217e05856c8e0022f6c93c86b) add healthcheck cmd to probe live and ready
 * [e14bc40c5](https://github.com/kubeovn/kube-ovn/commit/e14bc40c512324808deab974d2235fa5a61b5ba1) delete frequently log
 * [bde98e757](https://github.com/kubeovn/kube-ovn/commit/bde98e7571069d50caead0e4b7968d4e2947feb4) support running ovn-ic e2e on macOS
 * [727ea53a8](https://github.com/kubeovn/kube-ovn/commit/727ea53a809a1846c6d701cab5bca26b151a9313) pinger: fix getting empty PodIPs
 * [205a0c021](https://github.com/kubeovn/kube-ovn/commit/205a0c021e3be8d8df0931bbbbba29f392fa8220) fix cni deepcopy
 * [650ea6d3c](https://github.com/kubeovn/kube-ovn/commit/650ea6d3c5693b9e11f1b016c936b4a95a1b11a4) add cilium e2e
 * [46ba84eef](https://github.com/kubeovn/kube-ovn/commit/46ba84eefebd9fc15e6749c253b4dd4fd8cf1840) filter used qos when delete qos
 * [1de284ebc](https://github.com/kubeovn/kube-ovn/commit/1de284ebc8504e53cd4a2ec0388d407731226671) add protocol check when subnet is dual-stack
 * [1f4a247dd](https://github.com/kubeovn/kube-ovn/commit/1f4a247ddf8801f32b5b177dd8408a5ea0827a60) lint: make go-lint happy
 * [91f3fa4b9](https://github.com/kubeovn/kube-ovn/commit/91f3fa4b9b715faa09ad7e52f547e59bb9b920a3) some fixes
 * [d57bc1d72](https://github.com/kubeovn/kube-ovn/commit/d57bc1d72e66cfa0822cdb73b216af74fe4bb7d7) compatible with OVN 20.06
 * [9116425a4](https://github.com/kubeovn/kube-ovn/commit/9116425a42fdb3fb6cc8812d04cc0aa900c0b385) use multus-cni as default cni to assign ip
 * [d18323a49](https://github.com/kubeovn/kube-ovn/commit/d18323a4906626eb2dbb51a5d81085cbc34c86b9) some fixes
 * [668c21256](https://github.com/kubeovn/kube-ovn/commit/668c2125646cc1c2d29893457885dc2d245170d7) perf: jemalloc and ISA optimization
 * [5c08d28da](https://github.com/kubeovn/kube-ovn/commit/5c08d28da0eb7fa6981e71ca65c59f8dedeb42ed) fix: check np switch
 * [365715556](https://github.com/kubeovn/kube-ovn/commit/36571555676739e4e2e6949cd37d13999b5d9175) fix: port security
 * [e713bdf0f](https://github.com/kubeovn/kube-ovn/commit/e713bdf0f281415536eeab623385fd534e694d6c) fix nat rule
 * [d8e84cf06](https://github.com/kubeovn/kube-ovn/commit/d8e84cf06fc232ae0e954dfb20e3aa08ce0de30e) When netpol is added to a workload, the workload's POD can be accessed using service
 * [51365b41d](https://github.com/kubeovn/kube-ovn/commit/51365b41d1745f45c401943b75589ad1779efaf4) when update subnet's execpt ip,we should filter repeat ip
 * [5aacec592](https://github.com/kubeovn/kube-ovn/commit/5aacec592b8f63c03cf3b2d41b0292a48a084a62) update wechat image
 * [6c8fa978b](https://github.com/kubeovn/kube-ovn/commit/6c8fa978b008a75727ce97e1d326d2b5a7c096df) fix: do not reuse released ip after subnet updated
 * [e4648cc81](https://github.com/kubeovn/kube-ovn/commit/e4648cc81c2a8cc9bf11157379ee5e99d5c97e0c) update: update 1.7-1.8 script
 * [b1f8332c6](https://github.com/kubeovn/kube-ovn/commit/b1f8332c68770fe1db8a1dae2bdb84ef601bc195) perf: do not send traffic to ct if not designate to svc
 * [178cf7b87](https://github.com/kubeovn/kube-ovn/commit/178cf7b87fbaecd4868620c7fdbd9e5a3fa89145) fix: add back the leader check
 * [7be43c974](https://github.com/kubeovn/kube-ovn/commit/7be43c9748ba4470d7d40c63f39fd52352b0913a) fix port_security
 * [e596c3c4b](https://github.com/kubeovn/kube-ovn/commit/e596c3c4b0f5118db38bb308dce9a21bf25ea952) sync live migration vm port
 * [e8b1ff5b4](https://github.com/kubeovn/kube-ovn/commit/e8b1ff5b4ec877840c1647c4544b23c0a38ac252) docs: add f5 ces integration docs
 * [7058d5680](https://github.com/kubeovn/kube-ovn/commit/7058d5680f9338e3d8ee0dd711a535ab2faba368) update Go modules
 * [84dbb102b](https://github.com/kubeovn/kube-ovn/commit/84dbb102b3782abe57f6cd66e51abff9f219ac4e) update delete operation for statefulset pod
 * [e9e2c9111](https://github.com/kubeovn/kube-ovn/commit/e9e2c9111e26b5c7b4814104ba94a76949b3f548) chore: update klog to v2 which embed log rotation
 * [fafd55552](https://github.com/kubeovn/kube-ovn/commit/fafd5555284ee8d766109f1a785cf043cd4e1715) fix: add kube-ovn-cni prob timeout
 * [490590a46](https://github.com/kubeovn/kube-ovn/commit/490590a4694b753f9733559492bc8380b4a2680a) append add db compact for nb and sb db
 * [4fb302f5e](https://github.com/kubeovn/kube-ovn/commit/4fb302f5ebbed2b363c3bafb9bb6157328641969) deleting all chassises which are not nodes
 * [c49a74040](https://github.com/kubeovn/kube-ovn/commit/c49a740401323cc09d2e09e6d8abc00bbdca827b) add db compact for nb and sb db
 * [3b7ec06c7](https://github.com/kubeovn/kube-ovn/commit/3b7ec06c766d59598a7a178f376f85682a4be84d) add vendor param for fix list LR
 * [ae23d3dfd](https://github.com/kubeovn/kube-ovn/commit/ae23d3dfd500820cb2c5ff79baca58f234f1efb6) fix LB: skip service without cluster IP
 * [df3d3977b](https://github.com/kubeovn/kube-ovn/commit/df3d3977b9f19941060d0d7a4af63ef99c4ef494) add webhook with cert-manager issued certificate
 * [2be11269a](https://github.com/kubeovn/kube-ovn/commit/2be11269a9646f4cc124b8c6f2178dd4fd289bbe) security: update base ubuntu image
 * [eb3647176](https://github.com/kubeovn/kube-ovn/commit/eb3647176ddbed8f61aedb688d6d2c4800e60446) add pod in default vpc to node port-group
 * [ea300d2bf](https://github.com/kubeovn/kube-ovn/commit/ea300d2bf179d58f51465c5e767d487e606f7894) fix pinger's compatibility for k8s v1.16
 * [3837b0a23](https://github.com/kubeovn/kube-ovn/commit/3837b0a231659728d51d3ad64570c565d66bfed4) check IPv4 gateway by resolving gateway MAC in underlay subnets
 * [75604b5d9](https://github.com/kubeovn/kube-ovn/commit/75604b5d94ef5ff9ada5f5533ebaa157894adf8c) add nodeSelector for vpc-nat-gateway pod
 * [fac6c725a](https://github.com/kubeovn/kube-ovn/commit/fac6c725acf646e5f3e5fd3fc3a797402b7a10df) do not send multicast packets to conntrack
 * [c3004bbc2](https://github.com/kubeovn/kube-ovn/commit/c3004bbc2aa9c7d020fead19da390144c6d18162) Revert "support to set NB_Global option mcast_privileged"
 * [2802b94dd](https://github.com/kubeovn/kube-ovn/commit/2802b94ddb002ee434236e564ce18a6e23350850) add ip address for lsp
 * [28a93927a](https://github.com/kubeovn/kube-ovn/commit/28a93927af69358c35116e68a53ebe703c1326ae) fix: no need to set address for ls to lr port
 * [2048007a2](https://github.com/kubeovn/kube-ovn/commit/2048007a2c8a0e7fa5f691639782c69ce7e3aae1) add sg acl check when init
 * [b9abee715](https://github.com/kubeovn/kube-ovn/commit/b9abee71542b934f36f6cfb569f46d0bc2f79201) cleanup command flags
 * [54a3b913e](https://github.com/kubeovn/kube-ovn/commit/54a3b913ef3c1481318512e3aac3fd81b21bcab8) replace port-group named address-set with port-group since there's no ip set for lsp when create lsp
 * [743502cd7](https://github.com/kubeovn/kube-ovn/commit/743502cd744c3c397665b60e81bfdd2817e5beeb) support to set NB_Global option mcast_privileged
 * [a5f0256a9](https://github.com/kubeovn/kube-ovn/commit/a5f0256a979041adfead4c7ed47b72dd0b828a72) add networkpolicy support for attachment cni
 * [45f64bfaf](https://github.com/kubeovn/kube-ovn/commit/45f64bfaf83ff131f88b0121f8e5951d8b852b5e) add process for pod attachment nic with subnet in default vpc
 * [49e9197e5](https://github.com/kubeovn/kube-ovn/commit/49e9197e57db258a9baebcdbc3ba997599e27b6d) fix security group
 * [60e896f89](https://github.com/kubeovn/kube-ovn/commit/60e896f8967e0d9a3f4f0f1fa1c3a73083ca5b14) fix the duplicate call about strings.Split
 * [c9f5f4b46](https://github.com/kubeovn/kube-ovn/commit/c9f5f4b46cf9969816b471fec1fa0fb79fa86933) deepcopy fix steps
 * [e0cb19aa7](https://github.com/kubeovn/kube-ovn/commit/e0cb19aa73f95bba16872bd532ef4b6fd0d0a1c2) fix: do not nat route traffic
 * [4e4d95d5f](https://github.com/kubeovn/kube-ovn/commit/4e4d95d5f31f5c36af2c8d137e39081b6128ec50) fix: Skip MAC address Settings when PCI addresse is unavailable
 * [adce05c74](https://github.com/kubeovn/kube-ovn/commit/adce05c74ea6e89bee155e15b1ff43a1ab3c170e) add ovn-ic e2e
 * [3b6b5034d](https://github.com/kubeovn/kube-ovn/commit/3b6b5034d9900d820e93ec08a3f18c5e3298bc88) other CNI can be used as the default network
 * [841f907b0](https://github.com/kubeovn/kube-ovn/commit/841f907b0d0e8b40e9a0d186cafc33152e5b818a) fix: move macvlan binary to host
 * [52ec0af47](https://github.com/kubeovn/kube-ovn/commit/52ec0af4721478bb4d0bc4d5aff09960891ca9a2) Revert "ci: init kind cluster before build finish"
 * [a85993254](https://github.com/kubeovn/kube-ovn/commit/a859932540fca9fc5a9b8d862e43f22cf04291e2) fix ko trace
 * [1dd66a770](https://github.com/kubeovn/kube-ovn/commit/1dd66a770612544342c9af0d0d962f23a818640a) add ovn-ic HA deploy
 * [bc3ce0bbf](https://github.com/kubeovn/kube-ovn/commit/bc3ce0bbf11c0b6bcb9d621e0cf5c9d94e1b775a) fix node address set name
 * [cbed28204](https://github.com/kubeovn/kube-ovn/commit/cbed282046713526f15d55ef6bba920051237f64) update cni init image
 * [a648bfc6a](https://github.com/kubeovn/kube-ovn/commit/a648bfc6a23f6ced4c5688b1689e772ea2511e64) chore: update kind k8s to 1.22 and remove pre 1.16 support
 * [a1d56e973](https://github.com/kubeovn/kube-ovn/commit/a1d56e9737b256da80da09b3aef83efb28f75945) do not set bridge-nf-call-iptables
 * [738c76126](https://github.com/kubeovn/kube-ovn/commit/738c76126e5f2eea8c57073436ab6330d7b31029) use logical router policy for accessing node
 * [6719ee242](https://github.com/kubeovn/kube-ovn/commit/6719ee242b798352a99d12d26b034f58aa1fe401) ci: init kind cluster before build finish
 * [61817bf41](https://github.com/kubeovn/kube-ovn/commit/61817bf417278b553aeec981c84275efac6a623c) reduce qos query with ovs-vsctl cmd
 * [1776c4471](https://github.com/kubeovn/kube-ovn/commit/1776c4471447ec2e34fb373019a2c55ff82e1f1b) fix read-only pointer in vlan and provider-network
 * [329228d4a](https://github.com/kubeovn/kube-ovn/commit/329228d4a9b11e45bfe122e44089f1f4f66ca9f9) fix: trace in custom vpc
 * [a9c0a4aa4](https://github.com/kubeovn/kube-ovn/commit/a9c0a4aa499da7c12991e7bfaa1047291f4e7ada) fix read-only pointer in vlan and provider-network
 * [62df34162](https://github.com/kubeovn/kube-ovn/commit/62df34162ef72710a3e532d8fb489cbb78ffc594) update docs
 * [a546ba954](https://github.com/kubeovn/kube-ovn/commit/a546ba9545d22bb2d366e94e84e4fb5914964dc5) fix LB in dual stack cluster
 * [eb63f72ec](https://github.com/kubeovn/kube-ovn/commit/eb63f72ec1db5cd6a5394f8717bb13514fe965f1) fix: check allocated annotation in update handler
 * [55b8b8ac2](https://github.com/kubeovn/kube-ovn/commit/55b8b8ac2d543eb99055451b2327d9fe19478049) support using logical gateway in underlay subnet
 * [ef424d731](https://github.com/kubeovn/kube-ovn/commit/ef424d731a07c791943b587ca0e65014dec439df) docs: optimize cilium integration docs
 * [a09e84d0c](https://github.com/kubeovn/kube-ovn/commit/a09e84d0c4ac028ce6de7944a60ff1bca0fed3c3) fix: ensure all kube-ovn components deleted before annotate pods
 * [e7aeb96ec](https://github.com/kubeovn/kube-ovn/commit/e7aeb96ec789ca45ac1f10ef1945f01cb17dd26e) fix bug: logical switch ts not ready
 * [dc4e693f1](https://github.com/kubeovn/kube-ovn/commit/dc4e693f1f90396a9094ce4e3e0136176a5c4b01) Fix unpopulated CPU charts
 * [003723e58](https://github.com/kubeovn/kube-ovn/commit/003723e5881364a27afb44667c2bfb4359c2c2e7) Revert "get default subnet"
 * [418feb1ba](https://github.com/kubeovn/kube-ovn/commit/418feb1ba8ea96c804265b6966f0dba28d81debd) add htbqoses rbac
 * [850e42186](https://github.com/kubeovn/kube-ovn/commit/850e42186e373c1456a8c612a10cf143d248cf67) feat: pod can use multiple nic with the same subnet
 * [5840d5093](https://github.com/kubeovn/kube-ovn/commit/5840d5093ff6c98710849ba078b02350b4419a78) add error detail
 * [e6377caef](https://github.com/kubeovn/kube-ovn/commit/e6377caefbe46869f3000fef05affe993aa053ca) add check switch for default subnet's gateway
 * [b5b6c3267](https://github.com/kubeovn/kube-ovn/commit/b5b6c32678a696efff4e4b10fb84b40b327b5a7d) get default subnet
 * [fbafca410](https://github.com/kubeovn/kube-ovn/commit/fbafca410eb4f79b0252029d1dee117aef8d1dd9) remove node chassis annotation on cleanup
 * [348eaf367](https://github.com/kubeovn/kube-ovn/commit/348eaf3670720181c1e5a82ca10a1bc02a3c1732) update: add 1.7 to 1.8 update scripts
 * [f934613d3](https://github.com/kubeovn/kube-ovn/commit/f934613d36f0e756201df03ac5e1bd1e5cf23f9f) base: add macvlan to help vpc setup
 * [cd1dda1e5](https://github.com/kubeovn/kube-ovn/commit/cd1dda1e5449f6debd5a6e95e87bf25067bb825d) fix: delete vpc-nat-gw deployment
 * [50eddac3e](https://github.com/kubeovn/kube-ovn/commit/50eddac3e95b97136399647b390dcc614257f7fa) ko: check ovsdb storage status
 * [20670e877](https://github.com/kubeovn/kube-ovn/commit/20670e8772d46e1a9a29714d2e18d24727a655b8) fix cleanup.sh and uninstall.sh
 * [b31c4d19f](https://github.com/kubeovn/kube-ovn/commit/b31c4d19f4761a846167d352c4606fd612b1441c) use constant instead a string
 * [86f63f26d](https://github.com/kubeovn/kube-ovn/commit/86f63f26dbc319e4b65263029ac140f8e4466ccd) fix: check and load ip_tables module
 * [3bfd82b7e](https://github.com/kubeovn/kube-ovn/commit/3bfd82b7e08ca9cf1694649c7dd43d5c4234aa62) fix: multus-cni subnet allocation
 * [e5ed1ace0](https://github.com/kubeovn/kube-ovn/commit/e5ed1ace04509669b2331d26fb8797f645275cde) docs: add svg
 * [17ff6c55f](https://github.com/kubeovn/kube-ovn/commit/17ff6c55f97c484cd37408db678224b2f3b4d648) chore: update install
 * [ce97b94c8](https://github.com/kubeovn/kube-ovn/commit/ce97b94c8e7c4c8f18b3702a1d4b757e7720259f) integrate Cilium into Kube-OVN
 * [fda0c17b4](https://github.com/kubeovn/kube-ovn/commit/fda0c17b4066f9d439a85b5ca47f9cc526db614d) fix kubectl-ko diagnose
 * [3f8a2b0ed](https://github.com/kubeovn/kube-ovn/commit/3f8a2b0ed4551290e047829e197f99a5750ea46d) change inspection logic from manually adding lsp to just readding pod queue
 * [01ca82f9c](https://github.com/kubeovn/kube-ovn/commit/01ca82f9ce006dbae6a418fe1bfe8fa8b139d7f0) fix pinger in dual stack cluster
 * [0ba64dea9](https://github.com/kubeovn/kube-ovn/commit/0ba64dea916c1898411962c9fc0567724252b863) add e2e testing for dual stack underlay
 * [7f27a05d5](https://github.com/kubeovn/kube-ovn/commit/7f27a05d53690aff302593b5e8a29fea68116df1) fix pinger and monitor in underlay networking
 * [6a56f8bb5](https://github.com/kubeovn/kube-ovn/commit/6a56f8bb5694336d6758f4567ca6b0220e256fa0) fix kubectl plugin ko
 * [2c9fe438d](https://github.com/kubeovn/kube-ovn/commit/2c9fe438de87f1ef2748224782db96f54247c3dc) adjust the location of the log
 * [86ee933aa](https://github.com/kubeovn/kube-ovn/commit/86ee933aa16c747a56cee523092d030afc042bc3) ci: push vpc-nat-gateway
 * [f459ca975](https://github.com/kubeovn/kube-ovn/commit/f459ca975fc3d31edb95d3f703b6ec892c85e320) replace api for get lsp id by name
 * [0a533984f](https://github.com/kubeovn/kube-ovn/commit/0a533984f0238c21594f334f3153308a5a61f22d) docs：revise vpc.md
 * [788478991](https://github.com/kubeovn/kube-ovn/commit/788478991bdcf5f5b3a9ad6fe883a0de8e50bf32) grafana: optimize grafana dashboard
 * [168a7c976](https://github.com/kubeovn/kube-ovn/commit/168a7c9768a3660ce2c1c1472721b324ef2c1798) In netpol egress rules, except rule should be set to != and should not be ==
 * [d7edf24bf](https://github.com/kubeovn/kube-ovn/commit/d7edf24bf98d8229e4e71827e7b5d0f26c7b295a) ci: add vpc-nat-gateway build
 * [5cd32df80](https://github.com/kubeovn/kube-ovn/commit/5cd32df809a895d0df5b8bae1f0ce4e1d772c31a) Update OVN to version 21.06
 * [dd36d61c3](https://github.com/kubeovn/kube-ovn/commit/dd36d61c3b70e20111d8246dcd7eb9b4a770bf05) modify kube-ovn as multus-cni problem
 * [d17f61513](https://github.com/kubeovn/kube-ovn/commit/d17f6151310e7024a2d8a04e89abdff586648832) support to set htb qos priority
 * [c20e01110](https://github.com/kubeovn/kube-ovn/commit/c20e01110e0f2dcc138aac2f13793815fafd5ae2) perf: add fastpath module for 4.x kernel
 * [ff5d3df3f](https://github.com/kubeovn/kube-ovn/commit/ff5d3df3f898834709cd73dafbd898e2ceb8b04d) add inspection
 * [3e9f9a99c](https://github.com/kubeovn/kube-ovn/commit/3e9f9a99c0bde8fc1857e7a3d73a0a542231d4b5) perf: add stt section and update benchmark
 * [d38423271](https://github.com/kubeovn/kube-ovn/commit/d3842327175880b73a09bfb86a95ddbaf876ebbf) feat: optimize log
 * [4c6c29a36](https://github.com/kubeovn/kube-ovn/commit/4c6c29a36a0300eac7646c590cabe144df915e35) fix: init node with wrong ipamkey and lead conflict
 * [47255a105](https://github.com/kubeovn/kube-ovn/commit/47255a10591ab69ff8566b33ece98875f6f1fff1) fix installation scripts
 * [fd7454870](https://github.com/kubeovn/kube-ovn/commit/fd745487040ff067d2b9f161658555201f223857) fix getting LSP UUID by name
 * [1f5719a59](https://github.com/kubeovn/kube-ovn/commit/1f5719a59afc1acf009eb7e3438806c780908bf6) fix StatefulSet down scale
 * [5bccd8453](https://github.com/kubeovn/kube-ovn/commit/5bccd8453419cb4477dc925555f686c1bf3a7705) fix vpc policy route
 * [acb82de0d](https://github.com/kubeovn/kube-ovn/commit/acb82de0dd34152e537036208e147ec86c5fed6a) docs: update roadmap
 * [87f9b863e](https://github.com/kubeovn/kube-ovn/commit/87f9b863ea2e666aa27f1fc99fb6670aff94d6f2) refactor: mute ovn0 ping log and add ping details
 * [a99c4200c](https://github.com/kubeovn/kube-ovn/commit/a99c4200cfcd3ef233e81bb778cfc0eac5356c27) fix: wrong link for iptables
 * [52b01c017](https://github.com/kubeovn/kube-ovn/commit/52b01c017bd11c72b0134d7e092e5cb6636590ff) fix IPAM for StatefulSet
 * [51511e633](https://github.com/kubeovn/kube-ovn/commit/51511e633030eb92b6cc29f28338e890ff10f6d7) append externalIds for pod and node when upgrade
 * [391f7014e](https://github.com/kubeovn/kube-ovn/commit/391f7014e5c617b8f29723b460e23cf54779c4a5) feature: LoadBalancer for custom VPC
 * [7fd8cf44f](https://github.com/kubeovn/kube-ovn/commit/7fd8cf44fb4532fc182652939c0fbed1cf13e8d8) feat: support vip
 * [25f634fbc](https://github.com/kubeovn/kube-ovn/commit/25f634fbcbcf54f8f1dfde6f90aed4cd31b65479) fix VPC document
 * [97a5b2a33](https://github.com/kubeovn/kube-ovn/commit/97a5b2a337a511917f600303cf5e0823a4a7b942) fix init ipam
 * [71fcbf121](https://github.com/kubeovn/kube-ovn/commit/71fcbf12153330db0ddae6a1ba8baed628eafcb1) fix: gc lb
 * [2b154b1a5](https://github.com/kubeovn/kube-ovn/commit/2b154b1a52fcbb873988fb11900120273a1e4163) Update prometheus.md
 * [1e766f9cd](https://github.com/kubeovn/kube-ovn/commit/1e766f9cd76229189a957a78f8cc5d17a3e771cf) feat: support VLAN subnet in VPC
 * [4c013a3e1](https://github.com/kubeovn/kube-ovn/commit/4c013a3e186273d01b33cd0812a95925471f7a1a) ci: push dev image to separate repo
 * [39c8a19c1](https://github.com/kubeovn/kube-ovn/commit/39c8a19c177808f8c7060c30b2cc903cb53f1ee7) fix: kubeclient timeout
 * [edaf41e04](https://github.com/kubeovn/kube-ovn/commit/edaf41e041ef527abd141a4bbafca6cb445c90fa) fix: serialize pod add/delete order
 * [78a77f797](https://github.com/kubeovn/kube-ovn/commit/78a77f797c43b62086817b5a421789cdedbf6191) perf: increase ovn-nb timeout
 * [5937ccbf7](https://github.com/kubeovn/kube-ovn/commit/5937ccbf7aba3d3aee7d5ce49fe7cae1f062a088) fix gc lsp statistic for multiple subnet
 * [c71620ce1](https://github.com/kubeovn/kube-ovn/commit/c71620ce129baf8cc2434129ba965440eebb3ed2) fix: re-check ns annotation to avoid annotations lost
 * [d40d57015](https://github.com/kubeovn/kube-ovn/commit/d40d5701522c6ca43e771556848dcd6191337af6) perf: do not diagnose external access
 * [871c1493b](https://github.com/kubeovn/kube-ovn/commit/871c1493b251792bb8cbf5ed6c8b9dbd7f4aee9c) feature: vpc support policy route
 * [90b1a2ea4](https://github.com/kubeovn/kube-ovn/commit/90b1a2ea4205ebcdcb392760f1af12456543e801) reactor: remove ovn ipam options
 * [7f43f25c1](https://github.com/kubeovn/kube-ovn/commit/7f43f25c1cf2f65e0e658894aa4a5312add2f5ec) perf: switch's router port's addresses to "router"
 * [8dbe8f946](https://github.com/kubeovn/kube-ovn/commit/8dbe8f94678a8f5c7ec6259477d7a9e9dd711057) lint: make staticcheck happy
 * [8ad46dad4](https://github.com/kubeovn/kube-ovn/commit/8ad46dad4f96855fe413b18a1e3d368b418c5c3f) fix e2e testing
 * [5a126378a](https://github.com/kubeovn/kube-ovn/commit/5a126378a3466bb7ce715174c702e0e9454e85b6) prepare for next release
 * [5b70c81d3](https://github.com/kubeovn/kube-ovn/commit/5b70c81d3b7707d8cc1efe3632df28822ecf347c) fix variable referrence
 * [42fed929e](https://github.com/kubeovn/kube-ovn/commit/42fed929e61bed1fc1e0e654d4b379ab9317d601) fix typos
 * [f59aff27e](https://github.com/kubeovn/kube-ovn/commit/f59aff27e39f038eb2a0bca50294f95e072e9a20) refactor: reuse waitNetworkReady to check ovn0 and slightly improve the installation speed
 * [ea723d6dc](https://github.com/kubeovn/kube-ovn/commit/ea723d6dc9afed1e246407f8a34499c481d67a6c) fix nat-outgoing/policy-routing on pod startup
 * [2439c86e6](https://github.com/kubeovn/kube-ovn/commit/2439c86e6e4945073f023dc51b3ab49a5fa2be19) feat: suport vm live migration

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

## v1.8.18 (2023-10-11)

 * [ec9304fd8](https://github.com/kubeovn/kube-ovn/commit/ec9304fd829a8ed0e708beb835f8b2e9e0e4a42a) Dockerfile: fix base image version
 * [772df6f17](https://github.com/kubeovn/kube-ovn/commit/772df6f17be4cd01ec692577177494a5dc40ccb9) pinger: increase packet send interval (#3259)
 * [74762b9f1](https://github.com/kubeovn/kube-ovn/commit/74762b9f1c3bd9525d1014bcd7594eceae38109b)  prepare for 1.8.18 release
 * [fd5344c72](https://github.com/kubeovn/kube-ovn/commit/fd5344c720d80ff4d044c7b338969735c8be2b51) ci: pin go version to 1.20.5 (#3034)
 * [98c0e3cd4](https://github.com/kubeovn/kube-ovn/commit/98c0e3cd40f2f478d4f6b54d0376f41b86de93a6) static ip in exclude-ips can be allocated normally when subnet's availableIPs is 0 #3031
 * [9f2981053](https://github.com/kubeovn/kube-ovn/commit/9f2981053375c71104039e3c2b2a275430309c8a) prepare for 1.8.17 release
 * [e84053f2e](https://github.com/kubeovn/kube-ovn/commit/e84053f2e72e870c78661c340649a0e7036c7534) add subnet match check when change subnet gatewayType from centralized to distributed (#2891)
 * [188a9aa74](https://github.com/kubeovn/kube-ovn/commit/188a9aa74fbd878c2c1765f4305be94e183075e8) add static route for active-standby centralized subnet when use old active gateway node (#2699)
 * [bb41c58d8](https://github.com/kubeovn/kube-ovn/commit/bb41c58d8987915ebc0435f5eb8831a1911ca006) prepare for next release

### Contributors

 * hzma
 * zhangzujian
 * 张祖建
 * 马洪贞

## v1.8.15 (2023-04-24)

 * [f5af43061](https://github.com/kubeovn/kube-ovn/commit/f5af430619ddcbf92a7cd496b9fc8ced1beedb94) ci: add publish action
 * [87deba9bd](https://github.com/kubeovn/kube-ovn/commit/87deba9bd5e0109883da709a4f2252df253ed5f3) netpol: fix packet drop casued by incorrect address set deletion (#2677)
 * [385a76aa7](https://github.com/kubeovn/kube-ovn/commit/385a76aa75f5e7515ae8eb5d89e05c235860f9b5) do not set subnet's vlan empty on failure (#2445)
 * [8177808eb](https://github.com/kubeovn/kube-ovn/commit/8177808eb6b0e2cace640ec3c664447cad1c67ab) ci: fix cilium chaining e2e (#2391)
 * [0c7a5018b](https://github.com/kubeovn/kube-ovn/commit/0c7a5018b0fb83d9eec5b788c849659802a6388d) ci: fix ref name check (#2390)
 * [7365a4c8d](https://github.com/kubeovn/kube-ovn/commit/7365a4c8d51d234f56decc1184d1c2a194d2b6e0) ci: skip netpol e2e automatically for push events (#2379)
 * [18ef2f283](https://github.com/kubeovn/kube-ovn/commit/18ef2f283f74f6695d445558a69ac01a8d465848) e2e: run specs in parallel (#2375)
 * [6b5325d71](https://github.com/kubeovn/kube-ovn/commit/6b5325d71c77ace32457039882bc655e98dc8f8a) fix CVE-2022-28948
 * [7a424dc8e](https://github.com/kubeovn/kube-ovn/commit/7a424dc8e83b1f852b10ef26be79689fe984a056) fix CVE-2022-41723
 * [1192be2c9](https://github.com/kubeovn/kube-ovn/commit/1192be2c91aa954dbb3d2278a8523ae36c4e7bf3) ci: fix default branch test (#2369)
 * [bb9145688](https://github.com/kubeovn/kube-ovn/commit/bb9145688ef85eafd4056d2cb58da78830721820) fix github actions workflows (#2363)
 * [6ac19b943](https://github.com/kubeovn/kube-ovn/commit/6ac19b943ea97011f3958908a8236b5ed79ea1d6) simplify github actions workflows (#2338)
 * [9db146c0f](https://github.com/kubeovn/kube-ovn/commit/9db146c0fbee8b1b52566f911bade23a918587df) do not remove link local route on ovn0 (#2341)
 * [eb6d0bbdb](https://github.com/kubeovn/kube-ovn/commit/eb6d0bbdba36895c30e368f46df2541156f193c8) fix encap ip when the tunnel interface has multiple addresses (#2340)
 * [53df20f85](https://github.com/kubeovn/kube-ovn/commit/53df20f85b091682077c90ba394b461fb66c52a8) enqueue endpoint when handling service add event (#2337)
 * [de289b747](https://github.com/kubeovn/kube-ovn/commit/de289b747188006544e751ca5c4ed0f87aba529a) fix getting service backends in dual-stack clusters (#2323)
 * [0f20dea6b](https://github.com/kubeovn/kube-ovn/commit/0f20dea6bff6760f78094aee20d64584097002fa) fix github actions workflow
 * [544c229de](https://github.com/kubeovn/kube-ovn/commit/544c229def3ff1890c3298520adec483f7ad2822) An error occurred when netpol was added in double-stack mode (#2160)
 * [1746f6cf4](https://github.com/kubeovn/kube-ovn/commit/1746f6cf48a66562c15b11202f03dc125ea160dd) bump base image
 * [f4efd0bc8](https://github.com/kubeovn/kube-ovn/commit/f4efd0bc8577a3c55eb41afd07cb215ab8af0aed) fix gosec ci installation (#2295)
 * [35b22bbcd](https://github.com/kubeovn/kube-ovn/commit/35b22bbcdbaa6165678e00202215be2ca22d5158) fix CVE-2022-41721
 * [ffd625f1f](https://github.com/kubeovn/kube-ovn/commit/ffd625f1f607c195a59adfdbeb9730225df9033a) fix network break on kube-ovn-cni startup (#2272)
 * [b42e9d151](https://github.com/kubeovn/kube-ovn/commit/b42e9d151067369067af6e7a1bf1dd0a06d84c4a) fix gosec installation
 * [8f24823d3](https://github.com/kubeovn/kube-ovn/commit/8f24823d30cff74b4da38737c67e0148c32314bc) add release-1.8/1.9/1.10 to scheduled e2e (#2224)
 * [f1d5369b7](https://github.com/kubeovn/kube-ovn/commit/f1d5369b717a15aa3b0989166d7e07d3c51423a0) release-1.8: refactor e2e (#2214)
 * [ae99c07a3](https://github.com/kubeovn/kube-ovn/commit/ae99c07a36a09e0e186ac9b628750b63404c2fd1) prepare for release 1.8.15
 * [25e7f4324](https://github.com/kubeovn/kube-ovn/commit/25e7f4324172e619ef1f88c8240893f49ffd3272) fix: ovs gc just for pod if (#2187)
 * [a2e5e5db5](https://github.com/kubeovn/kube-ovn/commit/a2e5e5db58256b3c896a3f43a80673f5babb4064) fix: change condition to conditions
 * [d54c44816](https://github.com/kubeovn/kube-ovn/commit/d54c4481640de44456456cb1121b98ebb52b82e4) do not add subnet not processed by kube-ovn to vpc (#1735)
 * [371f95c63](https://github.com/kubeovn/kube-ovn/commit/371f95c63f37b228512c6ba0e0c52d3d261e56f1) kind: support to specify api server address/port (#2134)
 * [b46b103c4](https://github.com/kubeovn/kube-ovn/commit/b46b103c4cd39742fa4ba67e64db6be0a860a700)  fix: sometimes alloc ipv6 address failed sometimes ipam.GetStaticAddress return NoAvailableAddress
 * [7d6a162ae](https://github.com/kubeovn/kube-ovn/commit/7d6a162aecaa3acb943c0baa1aa97c0be2c74cf9) fix lint
 * [afcfbea90](https://github.com/kubeovn/kube-ovn/commit/afcfbea90d72a856934e0f6b956672927cb01f07) replace klog.Fatalf with klog.ErrorS and klog.FlushAndExit (#2093)
 * [09f675cfe](https://github.com/kubeovn/kube-ovn/commit/09f675cfecae9865dc3f5b5e2aadee5b939a165c) fix: del createIPS (#2087)
 * [170a947d1](https://github.com/kubeovn/kube-ovn/commit/170a947d104facf2e34179f634366c643f883b0f) fix ovs bridge not deleted cause by port link not found (#2084)
 * [e132558d5](https://github.com/kubeovn/kube-ovn/commit/e132558d5e16aa043c6b6380654f0d03608af1d1) fix: replace replace with add to override existing route (#2061)
 * [73b037b2c](https://github.com/kubeovn/kube-ovn/commit/73b037b2c95f47777f1ef14755b3851af44e088c) fix OVN LS/LB gc (#2069)
 * [2b3c1f4fd](https://github.com/kubeovn/kube-ovn/commit/2b3c1f4fd38b4b46ced8f24e6ca2971a08769199) perf: reduce controller init time (#2054)
 * [ea80eb9ac](https://github.com/kubeovn/kube-ovn/commit/ea80eb9ac3f69933e1674aeba8f8d1b0bb633790) ovs: fix reaching resubmit limit in underlay (#2038)
 * [9c8ffcf6a](https://github.com/kubeovn/kube-ovn/commit/9c8ffcf6ac2b5e0214746f7001ace4245214e702) fix pinger namespace error (#2034)
 * [ea5b93134](https://github.com/kubeovn/kube-ovn/commit/ea5b93134f5523ba668343272d6df7fd9deabd6a) update np name with character prefix
 * [ff3ac8996](https://github.com/kubeovn/kube-ovn/commit/ff3ac89968292343312fb58171e6fecf62fd2f86) bump kind and node image versions (#2023)
 * [97424b11b](https://github.com/kubeovn/kube-ovn/commit/97424b11b9af638a85720de97bac5f2702e8b869) fix ovn nb/sb health check (#2019)
 * [ce4861f2e](https://github.com/kubeovn/kube-ovn/commit/ce4861f2ef64345c95d04d05b6f260359fcd35fe) fix ovs fdb for the local bridge port (#2014)

### Contributors

 * Mengxin Liu
 * Noah
 * changluyi
 * lut777
 * tonyleu
 * wangyd1988
 * zhangzujian
 * 张祖建
 * 马洪贞

## v1.8.14 (2022-11-04)

 * [aec4eaebc](https://github.com/kubeovn/kube-ovn/commit/aec4eaebc8141816d7260c106511d4193418d3e1) fix: get ecmp nodecheck back (#2016)
 * [b714e057a](https://github.com/kubeovn/kube-ovn/commit/b714e057a262c0d0b6ba64f4a22f15489f7d7b54) fix: gateway route should stay still when node is pingable (#2015)
 * [898247c0a](https://github.com/kubeovn/kube-ovn/commit/898247c0a94afa736f67073d53247f36a3550f66) do not need to delete pg when update networkpolicy (#1959)
 * [7adf4ea77](https://github.com/kubeovn/kube-ovn/commit/7adf4ea776192d63783110718bada2e15c11fdec) do not set bridge-nf-call-iptables
 * [d6ddf8916](https://github.com/kubeovn/kube-ovn/commit/d6ddf8916890fe51c3f124949bde10dbd27bfd18) add check of write to ovn sb db for ovn-controller (#1989)
 * [4e17fe730](https://github.com/kubeovn/kube-ovn/commit/4e17fe73060663201682d10052dfa806e410fc65) fix grep matching device in routes (#1986)
 * [eb0cf4746](https://github.com/kubeovn/kube-ovn/commit/eb0cf4746493b87ce4152c5b0a4d6085ad7cdc4d) delete pod after TerminationGracePeriodSeconds (#1984)
 * [264beb592](https://github.com/kubeovn/kube-ovn/commit/264beb59232997594a109403ce33d4bb1d4d45af) ovs: fix waiting flows in underlay networking (#1983)
 * [640806d54](https://github.com/kubeovn/kube-ovn/commit/640806d5404596965cd8922c9f95859cf406b41e) use latest base image
 * [469b32ae7](https://github.com/kubeovn/kube-ovn/commit/469b32ae7163c5802cb0b1120290116fd9599baf) ovn db: recover automatically on startup if db corruption is detected (#1980)
 * [fd1552933](https://github.com/kubeovn/kube-ovn/commit/fd155293373587fa0d981a0bb06ce0675fd4132c) prepare for release 1.8.14
 * [4dbefaf22](https://github.com/kubeovn/kube-ovn/commit/4dbefaf22130d2b349b8e6387a6c423041e2895f) fix CVE-2022-32149
 * [317780a45](https://github.com/kubeovn/kube-ovn/commit/317780a45f6a344eb1e384c854f4cd348444d834) avoid concurrent subnet status update (#1976)
 * [3d0c5eb6d](https://github.com/kubeovn/kube-ovn/commit/3d0c5eb6d801aab5d5b417866e8ef0613205e95b) modify build error
 * [b65b3de16](https://github.com/kubeovn/kube-ovn/commit/b65b3de1698dc4adf96785b7fcc117ecf684bea9) fix metrics name (#1977)
 * [050117189](https://github.com/kubeovn/kube-ovn/commit/05011718931fbe564bb3dd1730787cffa8ea2439) add vm pod to ipam by ip when initIPAM (#1974)
 * [0890fdf9d](https://github.com/kubeovn/kube-ovn/commit/0890fdf9d519c6703b3df25d0f5fa6d9361c18bc) validate nbctl socket path in start-controller.sh
 * [e5c59e5b5](https://github.com/kubeovn/kube-ovn/commit/e5c59e5b58b4460e1f9caac78e89b202caaabf6e) skip CVE-2022-3358 (#1972)
 * [2f4a56a3c](https://github.com/kubeovn/kube-ovn/commit/2f4a56a3cedf3374d4c33372334b4439842998ad) use latest base image
 * [ea03249d1](https://github.com/kubeovn/kube-ovn/commit/ea03249d1039da2809f99bfed290a991de9e544a) fix: add  default deny acl (#1935)
 * [e89ace5e5](https://github.com/kubeovn/kube-ovn/commit/e89ace5e5d04b7810d717ca19c19f8d5f30a69b0) ovs: fix mac learning in environments with hairpin enabled (#1943)
 * [62491a53b](https://github.com/kubeovn/kube-ovn/commit/62491a53b7040a2973207dc836bb65caff852032) Fix registry for ovn-central container in install.sh (#1951)
 * [d96cba579](https://github.com/kubeovn/kube-ovn/commit/d96cba5792c0f6457fe4dcc3719932bd87fa5290) ovs: add fdb update logging (#1941)
 * [433c3b933](https://github.com/kubeovn/kube-ovn/commit/433c3b933fec7d5a4abea25f5df9a72856411d06) prepare for release v1.8.13
 * [93e821479](https://github.com/kubeovn/kube-ovn/commit/93e821479cd56193295ce9b06c974a6a4992e589) set ovsdb-server vlog level to avoid warnings caused by ovs-vsctl (#1937)
 * [a03c8064c](https://github.com/kubeovn/kube-ovn/commit/a03c8064c0dd3cde37064df67e52dd041e8db6f4) update Go to v1.17
 * [41e697a12](https://github.com/kubeovn/kube-ovn/commit/41e697a1255ee696a3101039890ff7a1b39f0100) add network partition check in ovn probes
 * [78e739371](https://github.com/kubeovn/kube-ovn/commit/78e739371c8293cfdeffafb19b9fc0fdee180102) feat: Replace command health check with k8s tcpSocket check (#1251)
 * [df29bb2e3](https://github.com/kubeovn/kube-ovn/commit/df29bb2e3d8e9b9b2515a4c0ab60ec56eacd6c34) fix CVE-2022-27664
 * [b35037d0e](https://github.com/kubeovn/kube-ovn/commit/b35037d0e83d007a221b7e622ade7688f0b6069f) update ns annotation when subnet cidr changed (#1921)

### Contributors

 * Mengxin Liu
 * hzma
 * lut777
 * runzhliu
 * zhangzujian
 * 尚墨
 * 张祖建
 * 范日明
 * 马洪贞

## v1.8.12 (2022-09-13)

 * [6e97d651e](https://github.com/kubeovn/kube-ovn/commit/6e97d651e5decda2b37c86c5ac26fe388f5a7b73) set release 1.8.12
 * [845ee70f6](https://github.com/kubeovn/kube-ovn/commit/845ee70f6452b6b6d5235ae3b5de1e8837b4bb8b) prepare release 1.8.12
 * [c39d51a3c](https://github.com/kubeovn/kube-ovn/commit/c39d51a3c7d011ae8e33e6510ebcdb7612feada4) fix: gatewaynode might be null (#1896)
 * [08331baee](https://github.com/kubeovn/kube-ovn/commit/08331baeeb66162134761b956216617bcc060af5) fix: api rollback
 * [3f96a6322](https://github.com/kubeovn/kube-ovn/commit/3f96a6322d5fb3d94a9f1c3d33529bd64a42451d) fix logrotate issues
 * [fb4ac005a](https://github.com/kubeovn/kube-ovn/commit/fb4ac005a0bae33a914651fd87f22cf8997aa3eb) fix security issues
 * [d289215e9](https://github.com/kubeovn/kube-ovn/commit/d289215e94f78850fc2a453deb7208bde4b07730) security: conform to gosec G114 (#1860)
 * [7451d0989](https://github.com/kubeovn/kube-ovn/commit/7451d0989f3cabe6b3edec0af84725addbf9833a) fix: diskfull may lead to wrong raft status for ovs db (#1635)
 * [dd22f6829](https://github.com/kubeovn/kube-ovn/commit/dd22f6829296a8107053483490be4fa89e824e79) kubectl-ko: turn off pipefail for ovn leader check (#1891)
 * [d2be779e8](https://github.com/kubeovn/kube-ovn/commit/d2be779e82cd097689be1c4c831ccddbbc24b51c) fix ip6tables link
 * [e1034427a](https://github.com/kubeovn/kube-ovn/commit/e1034427a4ef708dbc75c09e6132d0bbb56c5b09) fix duplicate logs for leader election (#1886)

### Contributors

 * Mengxin Liu
 * lut777
 * zhangzujian
 * 尚墨
 * 张祖建

## v1.8.11 (2022-08-30)

 * [9f059091c](https://github.com/kubeovn/kube-ovn/commit/9f059091c073df2908b6c3dc846fb42ab6f73eff) set release 1.8.11
 * [5fa2a8e11](https://github.com/kubeovn/kube-ovn/commit/5fa2a8e11d73e0b5947da1dd8350fc955867de25) feat: reduce downtime by increasing arp cache timeout
 * [c18cae4ec](https://github.com/kubeovn/kube-ovn/commit/c18cae4ec1631b316a9a3c9c235f8c2fa8e85ac6) feat: reduce wait time by counting the flow num.
 * [c8e36b5ea](https://github.com/kubeovn/kube-ovn/commit/c8e36b5eab6c3b24073215866d90ab39598c29d4) fix: missing stop_ovn_daemon args
 * [e5735c209](https://github.com/kubeovn/kube-ovn/commit/e5735c20945af95e13d18a1a345233cdf61c4c70) delete log severity for drop acl when update networkpolicy (#1862)
 * [4bcfb3730](https://github.com/kubeovn/kube-ovn/commit/4bcfb37306ad718338d32a4cba7681e0fe31b3b0) prepare release for 1.8.11
 * [9d7f0a59e](https://github.com/kubeovn/kube-ovn/commit/9d7f0a59efcc9596cee53f46443d598af4a39ecf) ovs: fix log file descriptor leak in monitor process (#1855)
 * [446ee6a21](https://github.com/kubeovn/kube-ovn/commit/446ee6a21edc6b4594926603da950949b8e8d838) fix ovs-ovn logging (#1848)
 * [63b218c63](https://github.com/kubeovn/kube-ovn/commit/63b218c6339f69230805299d2d8fd399be46d680) fix: multus macvlan ipvlan use kube-ovn ipam，but  ip not inited in init-ipam (#1843)
 * [95c8ca4f3](https://github.com/kubeovn/kube-ovn/commit/95c8ca4f3140200de6e8eac983a2c1915f881789) ko: fix kube-proxy check (#1842)
 * [b7b7d26dc](https://github.com/kubeovn/kube-ovn/commit/b7b7d26dc1724569c6185f8e20f47b3b619b1ec4) avoid patch interface deletion & recreation during restart
 * [2746a1954](https://github.com/kubeovn/kube-ovn/commit/2746a195495026574e4617d5024db7927a1aaddc) ci: switch environment to ubuntu-20.04 (#1838)
 * [cacb1ec4a](https://github.com/kubeovn/kube-ovn/commit/cacb1ec4a0937b49ac68b8a00641aa571f7b80f9) fix base failure
 * [3941595b1](https://github.com/kubeovn/kube-ovn/commit/3941595b1feb8e1c39fb921d725eff7fccb4252b) update base image
 * [2938daaaa](https://github.com/kubeovn/kube-ovn/commit/2938daaaaa4c427f950da8237f6b424a8ce6643a) fix base build failure
 * [122754aa5](https://github.com/kubeovn/kube-ovn/commit/122754aa5a677d6fc42c0fccbd00f5d92c07b247) update centralized subnet gateway ready patch operation
 * [c3f23af3f](https://github.com/kubeovn/kube-ovn/commit/c3f23af3f448bf9825329cb79029bb94a960719b) fix duplicate log for tunnel interface decision (#1823)
 * [3d966bff9](https://github.com/kubeovn/kube-ovn/commit/3d966bff9cb766a6a9d4ced664dc41fd0ec7b8d0) update version to v1.8.10 (#1819)
 * [dfc899246](https://github.com/kubeovn/kube-ovn/commit/dfc899246dacce9f5db1cabb63904c7ba06b10c6) do not check static route conflict (#1817)
 * [a6403f0ea](https://github.com/kubeovn/kube-ovn/commit/a6403f0eaca39c571dfbf91b7f21b3d92c375895) update centralize subnet gatewayNode until gw is ready (#1814)
 * [7103aae83](https://github.com/kubeovn/kube-ovn/commit/7103aae83c356fa6e18a51cdaa406c6f88d26a8c) initialize IPAM from IP CR with empty PodType for sts Pods (#1812)
 * [b669c673a](https://github.com/kubeovn/kube-ovn/commit/b669c673af2932050fbc39e35d1fb15f32721d4e) abort kube-ovn-controller on leader change (#1797)
 * [0e0ea3c7e](https://github.com/kubeovn/kube-ovn/commit/0e0ea3c7e32ead9ca9f42ce65d745ecf62be0026) avoid invalid ovn-nbctl daemon socket path (#1799)
 * [a7f499dd7](https://github.com/kubeovn/kube-ovn/commit/a7f499dd72dd08418c084ad365e8a9491fc182cb) do not wait dynamic address for pod (#1800)
 * [2b34fd587](https://github.com/kubeovn/kube-ovn/commit/2b34fd587fc63767b81020403381ee8853651d07) update vpc-nat-gateway base
 * [8d2d0b1e6](https://github.com/kubeovn/kube-ovn/commit/8d2d0b1e6defd339065da9e3abe0e1a8222f35f5) append delete static route for sts pod (#1798)
 * [9dc6e15ee](https://github.com/kubeovn/kube-ovn/commit/9dc6e15ee02479e4dcb4823aec567f9066c92bde) perf: fix memory leak
 * [14beb4840](https://github.com/kubeovn/kube-ovn/commit/14beb4840878b99c7ab60a6e6dbc739bf1f81545) perf: disable mlockall to reduce memory usage
 * [e6eace89b](https://github.com/kubeovn/kube-ovn/commit/e6eace89b181e7f985b37d00af4482457e9465f5) set sysctl variables on cni server startup (#1758)
 * [020b20dec](https://github.com/kubeovn/kube-ovn/commit/020b20dec34db6f29433c02043d499aa49778935) fix: add omitempty to subnet spec (#1765)
 * [3e77c51cc](https://github.com/kubeovn/kube-ovn/commit/3e77c51cc2787aa67823947cf17c22641ae1fbd6) fix CVE-2022-21698
 * [c5212982d](https://github.com/kubeovn/kube-ovn/commit/c5212982ddb6bfa8c5bd910a4a4f5a4b489367a0) add logrotate for kube-ovn log (#1740)
 * [ef275cc1c](https://github.com/kubeovn/kube-ovn/commit/ef275cc1c5c257da02ed7219dc1067502c625b74) fix: cancel delete staticroute when it's used by NatRule (#1733)
 * [513a30b52](https://github.com/kubeovn/kube-ovn/commit/513a30b52000a434328318c8bc3192cff427f346) fix: wrong info when update subnet from dual to ipv4 or ipv6. (#1726)
 * [aef889ae7](https://github.com/kubeovn/kube-ovn/commit/aef889ae7a8119ea6205873d100be85059a67e5c) Get latest vpc data from apiserver instead of cache (#1684)

### Contributors

 * Mengxin Liu
 * Wang Bo
 * bobz965
 * hzma
 * xujunjie-cover
 * zhangzujian
 * 张祖建

## v1.8.9 (2022-07-13)

 * [9050b22da](https://github.com/kubeovn/kube-ovn/commit/9050b22da0750de7f8880f937de42bb6363024c4) set release 1.8.9
 * [c42900d63](https://github.com/kubeovn/kube-ovn/commit/c42900d6350ad90b10c7f091f67d9e3491477ce1) prepare for release 1.8.9
 * [ff928386e](https://github.com/kubeovn/kube-ovn/commit/ff928386e1a4a6ea9f3b9d497c94335c1c241849) [PATCH] Delete pod if subnet of the pod's owner(sts/vm) updated (#1678)
 * [f216a2f57](https://github.com/kubeovn/kube-ovn/commit/f216a2f57854d002b759dacbfecfb242fe89b760) security: disable pprof by default (#1672)
 * [a984c913d](https://github.com/kubeovn/kube-ovn/commit/a984c913d265597b1c9ca249a91833f0f7eb28dd) update ovs health check, delete connection to ovn sb db (#1588)

### Contributors

 * Mengxin Liu
 * Wang Bo
 * hzma

## v1.8.8 (2022-06-28)

 * [0fbefff55](https://github.com/kubeovn/kube-ovn/commit/0fbefff55b77bc991f97d216b301044af27a01b8) set release 1.8.8
 * [37df8e76a](https://github.com/kubeovn/kube-ovn/commit/37df8e76ada0731a7693d0cdd611736f9ab8aa72) prepare for release 1.8.8
 * [bf8733308](https://github.com/kubeovn/kube-ovn/commit/bf8733308c0b64fe1444e4d253463896e437864c) CI: delete resources in order to avoid a long time waiting for subnet deletions. (#1643)
 * [de117356f](https://github.com/kubeovn/kube-ovn/commit/de117356f0a6e0054b563005e994f11de7ae4a30) add ovn-ic HA deploy
 * [1dcf9a432](https://github.com/kubeovn/kube-ovn/commit/1dcf9a43223c595fe4af8ca22f3c3826de656394) set networkpolicy log default to false

### Contributors

 * hzma
 * lut777
 * 张祖建

## v1.8.7 (2022-06-19)

 * [469875515](https://github.com/kubeovn/kube-ovn/commit/46987551520b9aa5014c1dccfc7c3a6621b96f2a) prepare for release 1.8.7
 * [b6796d09c](https://github.com/kubeovn/kube-ovn/commit/b6796d09c46b3dd299c772a1f0c96a49bde16889) cni handler: do not wait routed annotation for net1 (#1586)
 * [f5c3ed3f7](https://github.com/kubeovn/kube-ovn/commit/f5c3ed3f71e853df23a16b5c2e0fb049cf2d55c1) fix adding static route after LSP deletion (#1571)
 * [f7ee860b2](https://github.com/kubeovn/kube-ovn/commit/f7ee860b2ed0119d3a4b6582b87a8da2942e2918) fix duplicate netns parameter (#1580)
 * [0a3468b14](https://github.com/kubeovn/kube-ovn/commit/0a3468b144fde5828cbbb9e49eed42bd1cdf06a1) do not gc vm pod lsp when vm still exists (#1558)
 * [d453add37](https://github.com/kubeovn/kube-ovn/commit/d453add371feef760c852e8c7d6023136e967d4e) fix exec cmd in vpc nat gateway (#1556)
 * [8303ace0b](https://github.com/kubeovn/kube-ovn/commit/8303ace0b44f8164f16d5f6f9708ae5d067117d5) CNI: do not return route if nic is not eth0 (#1555)
 * [bc7582454](https://github.com/kubeovn/kube-ovn/commit/bc75824545aace5e0360e98f1440878ddb681ec7) exit kube-ovn-controller on stopped leading (#1536)
 * [c51b09e8f](https://github.com/kubeovn/kube-ovn/commit/c51b09e8f5f17fc2241f8624893140aab0492990) remove name for default drop acl in networkpolicy (#1522)
 * [9fe8cfcd0](https://github.com/kubeovn/kube-ovn/commit/9fe8cfcd08578a39df5a3ef9a7ed8aaf5b8657e1) move dumb-init from base images to kube-ovn image
 * [2a8a45a16](https://github.com/kubeovn/kube-ovn/commit/2a8a45a16dbc8fe4ba6bce49f075bc875a0152bc) fix defunct ovn-nbctl daemon

### Contributors

 * hzma
 * zhangzujian
 * 张祖建

## v1.8.6 (2022-05-13)

 * [56bf06df9](https://github.com/kubeovn/kube-ovn/commit/56bf06df9b7159958b1c439518b7ab666083eea6) release 1.8.6
 * [9e5b2b288](https://github.com/kubeovn/kube-ovn/commit/9e5b2b288713d49f2f96b8f7add9e56ec3f6e033) reduce ovs-ovn restart downtime (#1516)
 * [e4d6cc2f3](https://github.com/kubeovn/kube-ovn/commit/e4d6cc2f3b3b579ad64ce72c2deef1056393c038) prepare for release 1.8.6
 * [60aa89139](https://github.com/kubeovn/kube-ovn/commit/60aa89139154b30a27f6912e8dee65849853cca7) fix: ovs trace flow always ends with controller action (#1508)
 * [2a074c6f6](https://github.com/kubeovn/kube-ovn/commit/2a074c6f6529f488723cd4fac407e9739f39a0ee) optimize IPAM initialization

### Contributors

 * Mengxin Liu
 * zhangzujian

## v1.8.5 (2022-04-27)

 * [9b96bacf4](https://github.com/kubeovn/kube-ovn/commit/9b96bacf49aae35dc6d7bfc6f42ee6d8adceac81) ci: skip some checks
 * [e20cf4a22](https://github.com/kubeovn/kube-ovn/commit/e20cf4a2207a388e08c7cd5b503ee934331fbe96) delete ipam record and static route when gc lsp (#1490)
 * [035f5072c](https://github.com/kubeovn/kube-ovn/commit/035f5072c9219be7e8d989fec6eee338150b6321) CVE-2022-27191 (#1479)
 * [e898c96e6](https://github.com/kubeovn/kube-ovn/commit/e898c96e667b13d700e55af67057f503ed3ff138) add delete ovs pods after restore nb db (#1474)
 * [89d7471c7](https://github.com/kubeovn/kube-ovn/commit/89d7471c77f722d3e28681f6a251fcf40403957b) delete monitor noexecute toleration (#1473)
 * [4b012aa6d](https://github.com/kubeovn/kube-ovn/commit/4b012aa6d53b44fa08a59ec2fc73774fb70a27d1) add env-check (#1464)
 * [3d0448b4b](https://github.com/kubeovn/kube-ovn/commit/3d0448b4b9469548ebe43f0da0d1fb8677ef66de) append metrics (#1465)
 * [a0e2404c9](https://github.com/kubeovn/kube-ovn/commit/a0e2404c9cc634c6579e8c88ded1a2055953900b) add kube-ovn-controller switch for EIP and SNAT
 * [ca2ca1a16](https://github.com/kubeovn/kube-ovn/commit/ca2ca1a1614133b31c190cda102eabd493e64461) add routed check in circulation (#1446)
 * [c9dfa5bbb](https://github.com/kubeovn/kube-ovn/commit/c9dfa5bbbcdf6ab0a8245dccc8be2554322b1d0a) modify init ipam by ip crd only for sts pod (#1448)
 * [8b5ce74ad](https://github.com/kubeovn/kube-ovn/commit/8b5ce74ad37720b6f0552573e8cdadace791b708) ignore cni cve
 * [22fe8fbe6](https://github.com/kubeovn/kube-ovn/commit/22fe8fbe6f6f8cf30b9e5456ab8e6f0cda366d14) log: show the reason if get gw node failed (#1443)
 * [8570e2861](https://github.com/kubeovn/kube-ovn/commit/8570e286173f77117e4a84a6d9345280f8e82b4d) update alpine to fix CVE-2022-1271
 * [6aa6b0a92](https://github.com/kubeovn/kube-ovn/commit/6aa6b0a92b4209fe58147df15b03768de798e4e3) fix adding key to delete Pod queue
 * [bf12ea0e0](https://github.com/kubeovn/kube-ovn/commit/bf12ea0e0480555e17376890b128d30e16f109d4) fix IPAM initialization
 * [5e0058846](https://github.com/kubeovn/kube-ovn/commit/5e0058846712b046a6b8442490223d6588e8b3ab) ignore all link local unicast addresses/routes
 * [632480401](https://github.com/kubeovn/kube-ovn/commit/6324804011e068eeeb9143c53a24b9219efce3d2) fix error handling for netlink.AddrDel
 * [aa7c3b8de](https://github.com/kubeovn/kube-ovn/commit/aa7c3b8def0e6363bee4322c1103bc5493b212f0) replace pod name when create ip crd
 * [f0bb2769b](https://github.com/kubeovn/kube-ovn/commit/f0bb2769bd493b327fd9c905502c26a307c2f235) support alloc static ip from any subnet after ns supports multi subnets
 * [7a67a213d](https://github.com/kubeovn/kube-ovn/commit/7a67a213d8aea8eb6d90873ec8882fcce292cfed) fix provider-networks status
 * [8529bf8b7](https://github.com/kubeovn/kube-ovn/commit/8529bf8b79565b3c268ace4a84601dd6b5940d40) recover ips CR on IPAM initialization

### Contributors

 * Mengxin Liu
 * hzma
 * zhangzujian

## v1.8.4 (2022-03-29)

 * [48eb70a4d](https://github.com/kubeovn/kube-ovn/commit/48eb70a4d90f9e6334c3df23919b0afe5b20311b) release update 1.8.4 changelog (#1414)
 * [2fe7fff2a](https://github.com/kubeovn/kube-ovn/commit/2fe7fff2a8c5fbe23df621c950299acbe14cd53b) create ip crd in kube-ovn-controller (#1412)
 * [01163c1c2](https://github.com/kubeovn/kube-ovn/commit/01163c1c2e331f63c5bf5c38bd1cf542c1a363a8) fix: add condition for triggering the deletion of redundant chassises in sbdb (#1411)
 * [c262bdcf0](https://github.com/kubeovn/kube-ovn/commit/c262bdcf0abbcf3528a964f6f4507bbf5f23a979) fix: do not recreate port for terminating pods (#1409)
 * [bf167a60d](https://github.com/kubeovn/kube-ovn/commit/bf167a60dc152490aa5b74adedee102799ecd44e) avoid frequent ipset update
 * [b44bbc5d0](https://github.com/kubeovn/kube-ovn/commit/b44bbc5d0325c1e70cd7c3d13c56369a71d79f77) fix: The underlay physical gateway config by external-gw-addr when use snat&eip (#1400)
 * [ffdd19672](https://github.com/kubeovn/kube-ovn/commit/ffdd196723f90c2441bb5ab6b406da36e7722018) add reset for kube-ovn-monitor metrics (#1403)
 * [eda71b3c5](https://github.com/kubeovn/kube-ovn/commit/eda71b3c54ee8419950a80c43e46bba140c65e21) check the cidr format whether is correct (#1396)
 * [626950326](https://github.com/kubeovn/kube-ovn/commit/626950326ce9f842bb04f93e31914cfbe52c366e) update dockerfile to use v1.8.3 base img
 * [c15afc542](https://github.com/kubeovn/kube-ovn/commit/c15afc542fe50fc72739fa951345a193b6c9d105) append vm deletion check
 * [9faf2a101](https://github.com/kubeovn/kube-ovn/commit/9faf2a101ad87363954a6a847b2b3d93776f4237) update nodeips for restore cmd in ko plugin
 * [621a37f08](https://github.com/kubeovn/kube-ovn/commit/621a37f08754493503025481b7a92731239c76b6) fix external egress gateway
 * [27af3335a](https://github.com/kubeovn/kube-ovn/commit/27af3335a6f1b3cb562467c9b3fdc32bd04adb8a) update ip assigned check
 * [4d88bea53](https://github.com/kubeovn/kube-ovn/commit/4d88bea538c5953dec1651d605d998129f2f8c4c) add missing link scope routes in vpc-nat-gateway
 * [bf8026ed6](https://github.com/kubeovn/kube-ovn/commit/bf8026ed6482e928d3effb77781480e4c8a7d3a0) increase memory limit of ovn-central
 * [5a52041b6](https://github.com/kubeovn/kube-ovn/commit/5a52041b6bc45429171c2c515b9178f0bccfa919) fix range loop

### Contributors

 * hzma
 * lut777
 * wangyd1988
 * xujunjie-cover
 * zhangzujian

## v1.8.3 (2022-03-09)

 * [37937fcf1](https://github.com/kubeovn/kube-ovn/commit/37937fcf13e8c646b863696770c119efcba6df7c) release update 1.8.3 changelog (#1360)
 * [014ecc871](https://github.com/kubeovn/kube-ovn/commit/014ecc871f093d3adcf9602fe9629c8925d47f2d) add restore process for ovn nb db
 * [dbf4774d6](https://github.com/kubeovn/kube-ovn/commit/dbf4774d6580b5cc4a94fef90006317bb10344f9) optimize kube-ovn-monitor yaml
 * [ce8087d75](https://github.com/kubeovn/kube-ovn/commit/ce8087d75a90399e125c11a762c4e59350494faa) add reset porocess for ovs interface metrics
 * [62938245f](https://github.com/kubeovn/kube-ovn/commit/62938245fb3c082575ac02815429901a9db08a45) deepcopy fix steps
 * [118f12991](https://github.com/kubeovn/kube-ovn/commit/118f129910a85e74c084f9f2f8cefb3d79d23bca) fix SNAT/PR on Pod startup
 * [9fa2c792e](https://github.com/kubeovn/kube-ovn/commit/9fa2c792ec28ab428befc8aef8fbde2d91a0f369) add check for pod update process
 * [f053f2a25](https://github.com/kubeovn/kube-ovn/commit/f053f2a25f7743eeb10e30ee18ab2aeb75ed037f) fix ips update
 * [fe9532d4e](https://github.com/kubeovn/kube-ovn/commit/fe9532d4e66e3625b56a08aec6232d4f21106184) fix cni deepcopy
 * [c76e9b012](https://github.com/kubeovn/kube-ovn/commit/c76e9b01286eb51362dff4342435d4b2fe49330c) fix: replace ecmp dp_hash with hash by src_ip (#1289)
 * [f3922ba9c](https://github.com/kubeovn/kube-ovn/commit/f3922ba9c90496ba62ab5a9715d204804848e260) keep ip for kubevirt pod
 * [f66289024](https://github.com/kubeovn/kube-ovn/commit/f66289024e0397e1163e57cc3aac39ef0b956aa9) fix OVS bridge with bond port in mode 6
 * [a421d9f86](https://github.com/kubeovn/kube-ovn/commit/a421d9f8658c95da27975bb2679eaad00dc2fe97) fix: continue of deletion for del pod failed when can't found vpc or subnet (#1335)
 * [cf7f4bd9f](https://github.com/kubeovn/kube-ovn/commit/cf7f4bd9f267b4f2550db3b78563f6ab8665ed12) Fix usage of ovn commands
 * [586a0764b](https://github.com/kubeovn/kube-ovn/commit/586a0764bd398da9b02da4994ab22364d2f75ca2) ignore cilint
 * [e083a2ba0](https://github.com/kubeovn/kube-ovn/commit/e083a2ba061f5ed57b797a5138c4d668da9081b3) resync provider network status periodically
 * [dcb3e82dd](https://github.com/kubeovn/kube-ovn/commit/dcb3e82dd96af722df20575a6df06ef2abb6f2f8) Revert "resync provider network status periodically"
 * [18740e5c9](https://github.com/kubeovn/kube-ovn/commit/18740e5c9e0fb3214810265afe308b0359ab6f89) fix statefulset Pod deletion
 * [85c15cb4e](https://github.com/kubeovn/kube-ovn/commit/85c15cb4efad3c577e2722fc26b254c5c4e4df52) resync provider network status periodically
 * [172c17339](https://github.com/kubeovn/kube-ovn/commit/172c173390ff921240e9f8bee1e654cbd1c4c37a) feat: optimize log
 * [136aedf99](https://github.com/kubeovn/kube-ovn/commit/136aedf9961fa5a513ad1fa91ea3ad3cbd2c5c1c) optimize log for node port-group
 * [0869e621a](https://github.com/kubeovn/kube-ovn/commit/0869e621a4ee76d022e96a4bbb61933bc99273b5) append add cidr and excludeIps annotation for namespace
 * [e04eaf7a5](https://github.com/kubeovn/kube-ovn/commit/e04eaf7a5d85935c3b41658985f96387c5eb383f) support to add multiple subnets for a namespace
 * [ae201ef51](https://github.com/kubeovn/kube-ovn/commit/ae201ef51dacd165f283b1537ee58a88bdddc3a8) feat: update provider network via node annotation
 * [5cf005e24](https://github.com/kubeovn/kube-ovn/commit/5cf005e249318ba9bf85488c923566ebe3e8d06c) fix: only log matched svc with np (#1287)
 * [6ef52c22e](https://github.com/kubeovn/kube-ovn/commit/6ef52c22e346f3d2f810d964ed026916cb518285) transfer IP/route earlier in OVS startup
 * [75157be80](https://github.com/kubeovn/kube-ovn/commit/75157be80e8383532c49c98650ea58cccc21b76f) add metric for ovn nb/sb db status
 * [4b23c84c2](https://github.com/kubeovn/kube-ovn/commit/4b23c84c2745039954a6ce40f330357f6efa5dac) check static route conflict
 * [0832f5efa](https://github.com/kubeovn/kube-ovn/commit/0832f5efa6e7c601a695f85621bd1ace664c6604) set up tunnel correctly in hybrid mode
 * [175d54d18](https://github.com/kubeovn/kube-ovn/commit/175d54d1897109e82ccef29b1b7e4ad1280b891f) fix clusterrole in ovn-ha.yaml
 * [457475f2d](https://github.com/kubeovn/kube-ovn/commit/457475f2da8485e643e1a25607f297e59ae1d795) add gateway check after update subnet
 * [45787fb74](https://github.com/kubeovn/kube-ovn/commit/45787fb743a55e8e34fb109ddc77d3904b026f29) add back centralized subnet active-standby mode
 * [a737e1966](https://github.com/kubeovn/kube-ovn/commit/a737e19662d4a5efc7e528633609aecb84806998) update networkpolicy port process
 * [ff6bf6fa6](https://github.com/kubeovn/kube-ovn/commit/ff6bf6fa6dd901459e831a882b0035bc88dbae8a) update check for delete statefulset pod

### Contributors

 * chestack
 * hzma
 * lut777
 * xujunjie-cover
 * zhangzujian

## v1.8.2 (2022-01-05)

 * [5acf95862](https://github.com/kubeovn/kube-ovn/commit/5acf958622bb896a21951ebb6d6eded7bca98d16) release: update 1.8.2 changelog
 * [49b2ae40c](https://github.com/kubeovn/kube-ovn/commit/49b2ae40c88f293cc09de6796b8b920358f4e4f9) add log for ecmp route
 * [798d0bb97](https://github.com/kubeovn/kube-ovn/commit/798d0bb97757726077d8a8ff6454aae4ee751e71) fix pod tolerations
 * [c5f4c8e61](https://github.com/kubeovn/kube-ovn/commit/c5f4c8e61920db9a03842b0b535d0c14fb47ee98) fix installation script
 * [270d28e47](https://github.com/kubeovn/kube-ovn/commit/270d28e47c7acd8b258ff27e31700fb851f64feb) append check for centralized subnet nat process
 * [ee691fb51](https://github.com/kubeovn/kube-ovn/commit/ee691fb5118be4f300e14b77e94b2cbb74b80df9) change nbctl args 'wait=sb' to 'no-wait'
 * [c4956ac3e](https://github.com/kubeovn/kube-ovn/commit/c4956ac3ea9d606d40d651fd58b69e521760045a) move chassis judge to the end of node processing
 * [636b946af](https://github.com/kubeovn/kube-ovn/commit/636b946af6fe08d0dc9d042f1a6701734a8c0c45) use different ip crd with provider suffix for pod multus nic
 * [a03a858c1](https://github.com/kubeovn/kube-ovn/commit/a03a858c167fc55f0b0683cbb90f2da17b36e505) use multus-cni as default cni to assign ip
 * [3205b88ea](https://github.com/kubeovn/kube-ovn/commit/3205b88eaf94238c6819760acd1e57b5b96d70f9) fix: do not reuse released ip after subnet updated
 * [7de6afb82](https://github.com/kubeovn/kube-ovn/commit/7de6afb828cf3456c10fdc72cb47526a60dc23bf) delete frequently log
 * [efefc20b1](https://github.com/kubeovn/kube-ovn/commit/efefc20b125310bf9362250b1b7aea2b9ea51fea) pinger: fix getting empty PodIPs
 * [d98fab8d9](https://github.com/kubeovn/kube-ovn/commit/d98fab8d9b4c9ccc45b282536ae9376ae949a665) add protocol check when subnet is dual-stack
 * [0a48f6a6a](https://github.com/kubeovn/kube-ovn/commit/0a48f6a6a38b164a37022ecd921a4abe9b1f1350) filter used qos when delete qos
 * [26f239aa0](https://github.com/kubeovn/kube-ovn/commit/26f239aa01cd79a8a681a0e8f730a4033659db96) fix: check np switch
 * [4187a329b](https://github.com/kubeovn/kube-ovn/commit/4187a329bde0884ef6586006fe5919c20a6288c2) When netpol is added to a workload, the workload's POD can be accessed using service
 * [e7c500775](https://github.com/kubeovn/kube-ovn/commit/e7c50077549a5f9858ed4ebe8cf618592a39c282) when update subnet's execpt ip,we should filter repeat ip
 * [860202959](https://github.com/kubeovn/kube-ovn/commit/86020295969e90350ec2364232a4ce7a65ecf54c) fix: add back the leader check
 * [dfa1a3a8e](https://github.com/kubeovn/kube-ovn/commit/dfa1a3a8ea4a9ac600304ab211438eabf7c97fb7) security: upadate base image
 * [7f1e9354d](https://github.com/kubeovn/kube-ovn/commit/7f1e9354d414ef95324a232171f2e61ddc4af654) update delete operation for statefulset pod
 * [17301ee2f](https://github.com/kubeovn/kube-ovn/commit/17301ee2fe3a5aa433aee4a37782c39bee3fdd3b) chore: update klog to v2 which embed log rotation
 * [7cfeee1e2](https://github.com/kubeovn/kube-ovn/commit/7cfeee1e296547ebeb40e54dae42cab8a45e3a49) fix: add kube-ovn-cni prob timeout
 * [88a92ac95](https://github.com/kubeovn/kube-ovn/commit/88a92ac95357112b5f11a5f02e63875588f7629c) append add db compact for nb and sb db
 * [9496e3863](https://github.com/kubeovn/kube-ovn/commit/9496e38634716ef45174b06661ecdcc7e33b28c5) add vendor param for fix list LR
 * [641dcdde2](https://github.com/kubeovn/kube-ovn/commit/641dcdde2ec0ac092e1c5cc8df0d988ca4d1d360) deleting all chassises which are not nodes
 * [ad0bc1b77](https://github.com/kubeovn/kube-ovn/commit/ad0bc1b775e7e0840e0a42b3b7d82941d6a1d900) add db compact for nb and sb db
 * [b50da0e19](https://github.com/kubeovn/kube-ovn/commit/b50da0e1921e136dfd942013efef7bfa4cc72eaf) fix pinger's compatibility for k8s v1.16
 * [723ec5c3b](https://github.com/kubeovn/kube-ovn/commit/723ec5c3b26449e5a642424f5fcc811e17b32c8c) fix LB: skip service without cluster IP
 * [d412c7805](https://github.com/kubeovn/kube-ovn/commit/d412c780510a770cfc7862ffd060caad4597d53b) security: update base ubuntu image
 * [b96b7056b](https://github.com/kubeovn/kube-ovn/commit/b96b7056b31cce1a4c3ba8bd0f2fa521e3d35a55) add pod in default vpc to node port-group
 * [e1dfa7b19](https://github.com/kubeovn/kube-ovn/commit/e1dfa7b19de891990607f762447818b3bcafb7ba) add sg acl check when init
 * [c8692dfb0](https://github.com/kubeovn/kube-ovn/commit/c8692dfb0cb2ef7e9500ea7ff92f24f06ba019bf) fix: no need to set address for ls to lr port
 * [ef0e3b95a](https://github.com/kubeovn/kube-ovn/commit/ef0e3b95a2a796cc7e1108f8abed656add9ea9de) fix ko trace
 * [7231a6f20](https://github.com/kubeovn/kube-ovn/commit/7231a6f2015c2449a2d9969c117e19df765ca675) fix read-only pointer in vlan and provider-network
 * [01e30a42b](https://github.com/kubeovn/kube-ovn/commit/01e30a42b19cad6ea555bb293adf1526c5f724f8) fix read-only pointer in vlan and provider-network
 * [72cf31dd4](https://github.com/kubeovn/kube-ovn/commit/72cf31dd4fda2c9cc0f1cd10c445a2364d97c597) fix: trace in custom vpc
 * [03639a4a8](https://github.com/kubeovn/kube-ovn/commit/03639a4a83db3962fe11415a8ff1464faccc45ec) fix: multus-cni subnet allocation
 * [1857130e2](https://github.com/kubeovn/kube-ovn/commit/1857130e2fafe3b9833e36fd1f3098f3c0e519ea) fix LB in dual stack cluster
 * [3773bedf1](https://github.com/kubeovn/kube-ovn/commit/3773bedf1c15ca0a27e63d95fd919b025b7640d6) prepare for release 1.8.2
 * [45316125c](https://github.com/kubeovn/kube-ovn/commit/45316125c746653935945b2a782dc1bd246dfaa7) fix: check allocated annotation in update handler
 * [79be0cde9](https://github.com/kubeovn/kube-ovn/commit/79be0cde96ffb64db06ba37cf5d8e9b4ef01ad5a) fix bug: logical switch ts not ready
 * [e3581cf14](https://github.com/kubeovn/kube-ovn/commit/e3581cf1483d444492fcc2974da74e8a8df47e49) fix: ensure all kube-ovn components deleted before annotate pods
 * [9847a1b67](https://github.com/kubeovn/kube-ovn/commit/9847a1b67f753a07853e257dcede7118a3377c2b) Revert "add check switch for default subnet's gateway"
 * [c106afa63](https://github.com/kubeovn/kube-ovn/commit/c106afa635a3742df2a01dfca18ff5fb83e1f96f) add check switch for default subnet's gateway
 * [bdf5b0e29](https://github.com/kubeovn/kube-ovn/commit/bdf5b0e29d312a9ff42ef52c4dadc77b9bd1cffd) remove node chassis annotation on cleanup
 * [31a5da222](https://github.com/kubeovn/kube-ovn/commit/31a5da222e8ddf3b47b3da8affd468dd9d4d6085) fix: delete vpc-nat-gw deployment
 * [765ede7bb](https://github.com/kubeovn/kube-ovn/commit/765ede7bb9feb3f0861910e7acba6002663b63ac) fix: serialize pod add/delete order
 * [78dc1fbf4](https://github.com/kubeovn/kube-ovn/commit/78dc1fbf43b45dcebf7dd7bbcde3a6dff348e662) change inspection logic from manually adding lsp to just readding pod queue
 * [986f8b4e4](https://github.com/kubeovn/kube-ovn/commit/986f8b4e4f74d954285d862b31ec3de32163db34) add inspection
 * [15ea6ab88](https://github.com/kubeovn/kube-ovn/commit/15ea6ab88217f14ccb2faf516c9982c095386479) fix: check and load ip_tables module
 * [9bb0cfc24](https://github.com/kubeovn/kube-ovn/commit/9bb0cfc242ee2f8887cdab6dcb2615f47da1098e) fix cleanup.sh and uninstall.sh
 * [da422ff9f](https://github.com/kubeovn/kube-ovn/commit/da422ff9fb9cd5767a21e59ae9d48287c15d0e44) fix kubectl-ko diagnose
 * [cc8a4da05](https://github.com/kubeovn/kube-ovn/commit/cc8a4da05fda180c00be109f0d396cb4070e6384) fix pinger in dual stack cluster
 * [9364d2a2d](https://github.com/kubeovn/kube-ovn/commit/9364d2a2d8392f5fd4710a43f50308093a500bcd) add e2e testing for dual stack underlay
 * [ecf4e011c](https://github.com/kubeovn/kube-ovn/commit/ecf4e011c8b8fc446bf32f306fcfbaae1717b542) fix pinger and monitor in underlay networking
 * [91a32d416](https://github.com/kubeovn/kube-ovn/commit/91a32d416c091b710e5d5c5c1cc4cf76ec41145b) fix kubectl plugin ko
 * [259f8d6a0](https://github.com/kubeovn/kube-ovn/commit/259f8d6a0834595e2d919b77b91841f1387e6a67) replace api for get lsp id by name
 * [7e775fa6a](https://github.com/kubeovn/kube-ovn/commit/7e775fa6a817a73dcfde1f0484eaa39a0dc5992e) In netpol egress rules, except rule should be set to "!=" and should not be "=="
 * [0a09e0557](https://github.com/kubeovn/kube-ovn/commit/0a09e0557eac09439d6d3fb531203f47a15eb628) modify kube-ovn as multus-cni problem

### Contributors

 * Mengxin Liu
 * hzma
 * lut777
 * wang_yudong
 * zhangzujian
 * 范日明

## v1.8.1 (2021-10-09)

 * [31f53094f](https://github.com/kubeovn/kube-ovn/commit/31f53094f5b85101e19ba2bcba5dc02491759a22) release: prepare for 1.8.1
 * [fa66c5f87](https://github.com/kubeovn/kube-ovn/commit/fa66c5f87df3df62b7c6c199484115b8149f2b76) fix: init node with wrong ipamkey and lead conflict
 * [fa17c3d63](https://github.com/kubeovn/kube-ovn/commit/fa17c3d6325150496addc2cc725483e8c0e6d817) fix installation scripts
 * [c7d050b99](https://github.com/kubeovn/kube-ovn/commit/c7d050b99d63b6469f26acb6c34c28d59c456605) fix getting LSP UUID by name
 * [f0bebbec8](https://github.com/kubeovn/kube-ovn/commit/f0bebbec809dbdadaaa2e4f8972bd431f3a91f08) fix StatefulSet down scale
 * [4c189b7f8](https://github.com/kubeovn/kube-ovn/commit/4c189b7f8d268f4fd4d9d36c3180924d37eddac7) refactor: mute ovn0 ping log and add ping details
 * [c208cd518](https://github.com/kubeovn/kube-ovn/commit/c208cd5181ef46e3392cba8aa113e4bf9af01736) fix: wrong link for iptables
 * [b4faf60b7](https://github.com/kubeovn/kube-ovn/commit/b4faf60b78a55fe98722de36a7642f6742db61d7) fix IPAM for StatefulSet
 * [d05259579](https://github.com/kubeovn/kube-ovn/commit/d05259579e9e929e8eef80d7b7bc97cee124d45b) append externalIds for pod and node when upgrade
 * [34ba16eac](https://github.com/kubeovn/kube-ovn/commit/34ba16eac715f46d2676b53de25feef118a1f3d3) perf: increase ovn-nb timeout
 * [f844a2bc3](https://github.com/kubeovn/kube-ovn/commit/f844a2bc30a3b2aeafbcbfaee3e153493948ce1a) fix: re-check ns annotation to avoid annotations lost
 * [f72141953](https://github.com/kubeovn/kube-ovn/commit/f72141953363e19a12f53f902770b90566c46c1d) perf: do not diagnose external access
 * [6232c73bb](https://github.com/kubeovn/kube-ovn/commit/6232c73bbddc761c526c31033137e46053306b09) reactor: remove ovn ipam options
 * [651ab41ed](https://github.com/kubeovn/kube-ovn/commit/651ab41ed587454be444fc2d51497fec120c120d) perf: switch's router port's addresses to "router"
 * [f5997a875](https://github.com/kubeovn/kube-ovn/commit/f5997a875f805e022b28619d861be7b458accc97) fix gc lsp statistic for multiple subnet
 * [da43e21b1](https://github.com/kubeovn/kube-ovn/commit/da43e21b198b2a9cf013786be95b9b706ecf73e7) fix e2e testing
 * [5e3c15073](https://github.com/kubeovn/kube-ovn/commit/5e3c1507371f7117fe5441070ce19e3a2062aec8) fix variable referrence
 * [bc95b5d3a](https://github.com/kubeovn/kube-ovn/commit/bc95b5d3a0c048bcfb500a93fec8ed9e88bd7a2c) fix nat-outgoing/policy-routing on pod startup

### Contributors

 * Mengxin Liu
 * hzma
 * lut777
 * zhangzujian

## v1.8.0 (2021-09-08)

 * [7c5fed654](https://github.com/kubeovn/kube-ovn/commit/7c5fed6547c8695056de3f12a5ca7e0754b37d39) fix adding OVN routes in dual stack Kubernetes
 * [80a037ee5](https://github.com/kubeovn/kube-ovn/commit/80a037ee557cf4c3ffe60845a52ae8f1f00196f8) release: prepare for 1.8
 * [f59bfb864](https://github.com/kubeovn/kube-ovn/commit/f59bfb864a261e8f0174c8940850389d79ae02a1) add update process and adding label to ls/lsp/lr
 * [e09d99b3b](https://github.com/kubeovn/kube-ovn/commit/e09d99b3b4e8e46f7af795fafeaa725bf8bb0d0b) fix: VLAN CIDR conflict check
 * [e6b8341e6](https://github.com/kubeovn/kube-ovn/commit/e6b8341e6b9cf337104aff383ef27279faf51bc7) security: update base image
 * [29422965e](https://github.com/kubeovn/kube-ovn/commit/29422965e5cf0b9a62143c66205f3f5bf059f7e8) update provider network CRD
 * [25b151c85](https://github.com/kubeovn/kube-ovn/commit/25b151c8587eb63778e0f87664ceb54801797687) fix external-vpc
 * [44a8b4f69](https://github.com/kubeovn/kube-ovn/commit/44a8b4f6912b5869c9eb19ec92d3b340fbb57821) perf: use link alias to filter packet
 * [e9984fe0c](https://github.com/kubeovn/kube-ovn/commit/e9984fe0c047f34d0be196b54062a6be2c450504) security: fix CVE-2021-3538
 * [d41c5e9b8](https://github.com/kubeovn/kube-ovn/commit/d41c5e9b840e740db398564d5c2d0b303457804d) add print columns for subnet/vpc/vpc-nat-gw crd
 * [730e4f171](https://github.com/kubeovn/kube-ovn/commit/730e4f17165d853055557e3a9b442655aa21fed5) improve support for dual-stack
 * [c148a5ac8](https://github.com/kubeovn/kube-ovn/commit/c148a5ac861f27247847df0114956a510a1162e6) initialize ipsets on cni server startup
 * [10613e872](https://github.com/kubeovn/kube-ovn/commit/10613e8727b33e60ba15f2f57869c4884a029c2a) delete residual ovs internal ports
 * [361d4bbe0](https://github.com/kubeovn/kube-ovn/commit/361d4bbe026784ea4314ee262c12745ad7e6e982) simplify vlan implement
 * [6fde0a566](https://github.com/kubeovn/kube-ovn/commit/6fde0a56686d543eabc6ba5d5368b5ba38adc768) fix: ovn-northd svc flip flop
 * [b11060565](https://github.com/kubeovn/kube-ovn/commit/b110605650819ee4aadce6c21b22c0cddf852f24) add container run command for runtime containerd
 * [42e212ca2](https://github.com/kubeovn/kube-ovn/commit/42e212ca24467174c56e71e9ca497dba6843371c) fix subnet conflict check for node address
 * [3d2c6eb96](https://github.com/kubeovn/kube-ovn/commit/3d2c6eb96e191e3f8346cf97fe086e241bf2f405) feat: read interface in installation from environment
 * [35acf424e](https://github.com/kubeovn/kube-ovn/commit/35acf424ee659eee1ee0872afa36bc5925974988) update encap ip by node annotation periodic
 * [13b2080a0](https://github.com/kubeovn/kube-ovn/commit/13b2080a03c922e766d11ef8a8739296c0689e5b) fix ipset on pod creation/deletion
 * [f415b1ba5](https://github.com/kubeovn/kube-ovn/commit/f415b1ba54836e1750667f40c1c86f529682f6b2) add ready status for provider network
 * [092838495](https://github.com/kubeovn/kube-ovn/commit/0928384955266226aef1119fcd8e90ebf4f34ccb) avoid Pod IP to be the same with node internal IP
 * [70fbbecc4](https://github.com/kubeovn/kube-ovn/commit/70fbbecc43aa9db9d015eef4a8fbacd60d47de68) remove subnet's `spec.underlayGateway` field
 * [96b0c1185](https://github.com/kubeovn/kube-ovn/commit/96b0c11859661bd6c377ae22add2e163aebe3d1d) add support for custom routes
 * [45aafca21](https://github.com/kubeovn/kube-ovn/commit/45aafca2104a94ab56b3ed03a3e8df67abc462bc) Add missing metadata directive in VpcNatGateway example
 * [0380d64ce](https://github.com/kubeovn/kube-ovn/commit/0380d64cea659085aabb1b77dbcc17e3a5a18ac6) use util.hostNameEnv instead KUBE_NODE_NAME
 * [38e04f34a](https://github.com/kubeovn/kube-ovn/commit/38e04f34aaadfbef889fae2f31e5df280e5429b8) chore: change wechat image
 * [5df9fdd4c](https://github.com/kubeovn/kube-ovn/commit/5df9fdd4c7594af4e02c5100e68e74c95eb94c35) fix typo
 * [4a7dd7340](https://github.com/kubeovn/kube-ovn/commit/4a7dd7340dc9e4ae024c4c670c7382d8c2e269ce) perf: add fastpath and tuning guide
 * [3d8cdb6cb](https://github.com/kubeovn/kube-ovn/commit/3d8cdb6cb0177b5212b235bf850fe706fe61eb70) update node labels and provider network's status.readyNodes when provider network is not initialized successfully in a node
 * [8596ddc90](https://github.com/kubeovn/kube-ovn/commit/8596ddc901a55a04474a941f06e1a67f6dd8e644) fix issues in underlay networking
 * [7724990dd](https://github.com/kubeovn/kube-ovn/commit/7724990dda02f599a2b9bb153e42d034591e6059) add external vpc switch
 * [ffef618db](https://github.com/kubeovn/kube-ovn/commit/ffef618db5aed74330627e6336a5b7f5aeaca525) update versions in docs and yamls
 * [6e8d5c80a](https://github.com/kubeovn/kube-ovn/commit/6e8d5c80a809f11f794841ba7881038030548079) update Go to version 1.16
 * [3deb57708](https://github.com/kubeovn/kube-ovn/commit/3deb57708fcf95f0b6a46dfc23c18fb970914341) fix IPv6-related issues
 * [2e4922d56](https://github.com/kubeovn/kube-ovn/commit/2e4922d560da774590694002cb95b8cfa6adad3b) ci: use stable version
 * [dcda11d6e](https://github.com/kubeovn/kube-ovn/commit/dcda11d6e19d9508ecd4a23f8b41857a9fe81fc4) fix: bad udp checksum when access nodeport
 * [f12e5ee58](https://github.com/kubeovn/kube-ovn/commit/f12e5ee587e26c1474351b9f49e8889e870d1680) fix port-security, address parameters should be merged into one
 * [f03d43503](https://github.com/kubeovn/kube-ovn/commit/f03d435038bdc8f7eeb131c0bbd3628a4dcc2af6) docs: optimize description
 * [b5b5bdb89](https://github.com/kubeovn/kube-ovn/commit/b5b5bdb89e717670a3f5f10e2c758148a73bdefc) ensure provider nic is up
 * [b5bbed38a](https://github.com/kubeovn/kube-ovn/commit/b5bbed38a3f175982f9ba25b14a57cd56e7ba611) fix uninstall.sh
 * [3ba5168ce](https://github.com/kubeovn/kube-ovn/commit/3ba5168cec17e251fff40254df057aaa837fdd16) some optimizations
 * [9ae0b3c35](https://github.com/kubeovn/kube-ovn/commit/9ae0b3c351e60539fcdf89cfbff6628e50f76c0a) fix gofmt lint
 * [410d93290](https://github.com/kubeovn/kube-ovn/commit/410d932900eab3184bfddd017c197b189a11a391) fix multi-nic.md
 * [5e9e41ac5](https://github.com/kubeovn/kube-ovn/commit/5e9e41ac5555e4521680221b5bb73dd81fc620cc) fix dual stack cluster created by kind
 * [386d6160f](https://github.com/kubeovn/kube-ovn/commit/386d6160f2970ef0ea498680f96e881f32b4f0a1) remove external egress gateway from additionalPrinterColumns
 * [70ae50efb](https://github.com/kubeovn/kube-ovn/commit/70ae50efb3ef53458b11a519efdf962c1b04a396) fix default bind socket of cni server
 * [56025edea](https://github.com/kubeovn/kube-ovn/commit/56025edea4628b755b6b6116772be1bc1efa0492) if the string of ip is empty,program will die
 * [9492f63f6](https://github.com/kubeovn/kube-ovn/commit/9492f63f6bd590153a17ea30f43509f8208a1e99) if the string of ip  is empty,program will die
 * [324dce2e3](https://github.com/kubeovn/kube-ovn/commit/324dce2e3b38f71461fd9a9b372aacb1cd54ca55) fix underlay networking on node reboot
 * [f7077d58e](https://github.com/kubeovn/kube-ovn/commit/f7077d58ee08a9864725a5b1962c6e9bb54a6033) add judge before use the index about cidrBlocks and ips
 * [f25b1ae2b](https://github.com/kubeovn/kube-ovn/commit/f25b1ae2b544af576842d0bf538b0cf90a9e0d32) add validation check function
 * [bda102a77](https://github.com/kubeovn/kube-ovn/commit/bda102a772483b84903bc4ac7532472e88608a78) docs: add wechat qcode
 * [14ccbeb37](https://github.com/kubeovn/kube-ovn/commit/14ccbeb377578ea18ea69b5c467445f5f08f13e3) feat: security group
 * [992a09d35](https://github.com/kubeovn/kube-ovn/commit/992a09d353a4bdb012aea0a591248b8a523a8f2f) delete subnet AvailableIPs and UsingIPs para
 * [057ade92e](https://github.com/kubeovn/kube-ovn/commit/057ade92edbdca16000598f65dd5afe6fc063c89) fix: panic when node has nil annotations
 * [59869daaf](https://github.com/kubeovn/kube-ovn/commit/59869daaf1246621c9dad409e0549483bf5a4a35) append pod/exec resource for vpc nat gw
 * [3ed2fe26d](https://github.com/kubeovn/kube-ovn/commit/3ed2fe26dfd913155335d2a97414426c8e72ed7e) update comment for SetInterfaceBandwidth
 * [e1caa5941](https://github.com/kubeovn/kube-ovn/commit/e1caa5941e55dd14f5e30dee5446900fb5138766) update qos process
 * [80e5e2bab](https://github.com/kubeovn/kube-ovn/commit/80e5e2babbe01cd15863837bb8ee74087bad9c91) fix LoadBalancer in custom VPCs
 * [bb1146eeb](https://github.com/kubeovn/kube-ovn/commit/bb1146eeb5887f8f3856e929626ef6b939b9e9bf) Support Pod annotations control port mirroring
 * [4c4b09007](https://github.com/kubeovn/kube-ovn/commit/4c4b09007b5d0ce1cbf8a361c30bcf5591a22dcf) fix docs
 * [a04d964dc](https://github.com/kubeovn/kube-ovn/commit/a04d964dcf1ef99a4a8f3ed62449a39a5cecc6f4) externalOvnRouters is ok with 0
 * [9524c93f3](https://github.com/kubeovn/kube-ovn/commit/9524c93f3b55a40518f8198a2d91ac369ebd49f4) delete attachment ips
 * [6dd6a51d7](https://github.com/kubeovn/kube-ovn/commit/6dd6a51d7d53a9d9caa175108e330e8dff43f93e) fix external_ids:pod_netns
 * [cbe8ae689](https://github.com/kubeovn/kube-ovn/commit/cbe8ae689fe0b8118cabe0e2d4fe683602026d3d) add switch for network policy support
 * [dc56d2385](https://github.com/kubeovn/kube-ovn/commit/dc56d23853ff6b0fc38a5f514b029387be0b5787) fix subnet e2e
 * [e3daee830](https://github.com/kubeovn/kube-ovn/commit/e3daee8306ab1d71957f01db5de8890e5d31194c) ignore empty strings when counting lbs
 * [81ce45c2e](https://github.com/kubeovn/kube-ovn/commit/81ce45c2e89a31c064eba54eec46cc729129f25c) fix iptables
 * [e9ea6a0f9](https://github.com/kubeovn/kube-ovn/commit/e9ea6a0f98220453fc3b36a581cc695445f7a503) fix issue #944
 * [1cb57358a](https://github.com/kubeovn/kube-ovn/commit/1cb57358ac0dbd36f7fa9fc6eda71b86f621fd12) fix openstackonkubernetes doc bugs
 * [fcdb0106b](https://github.com/kubeovn/kube-ovn/commit/fcdb0106b263afffba370987763d07f7486d3490) add switch for gateway connectivity check
 * [4dc4624f9](https://github.com/kubeovn/kube-ovn/commit/4dc4624f9bcf75ca296fc9c84c03590163c31505) fix cleanup.sh
 * [4fb974071](https://github.com/kubeovn/kube-ovn/commit/4fb974071601402230e5d5c2a45491ec9fa8df4c) security: fix CVE-2021-33910
 * [41b6429c4](https://github.com/kubeovn/kube-ovn/commit/41b6429c4334bdff5c2c85d17d9775484800958a) delete ecmp route when node is deleted
 * [5bd96ac71](https://github.com/kubeovn/kube-ovn/commit/5bd96ac718927a511f0111c3ceb20350f6a9effb) fix: if nftables not exists do no exit
 * [6c5efbc30](https://github.com/kubeovn/kube-ovn/commit/6c5efbc30e0ad3e7f38b46af6b455b0d8abcb118) update wechat contract method
 * [e449b8eaf](https://github.com/kubeovn/kube-ovn/commit/e449b8eaf156c387c9619016ca9f107a1c1353c4) delete overlapped var subnet
 * [2427a4b3a](https://github.com/kubeovn/kube-ovn/commit/2427a4b3a05471239f29bceb1b9e6f0444fa5788) add designative nat ip process for centralized subnet
 * [1595eac56](https://github.com/kubeovn/kube-ovn/commit/1595eac56740e388b718548204906cd7b5f9a4dc) fix ipsets
 * [7e24e7d6f](https://github.com/kubeovn/kube-ovn/commit/7e24e7d6f78430fee4fd3951adf9bb006ee0f07b) update underlay e2e testing
 * [27c649a50](https://github.com/kubeovn/kube-ovn/commit/27c649a50a40d4c96ed73262bbaaabbc47bc6dcc) match chassis until timeout
 * [df76038a0](https://github.com/kubeovn/kube-ovn/commit/df76038a0a0475a269a3829e14875a9e847a6e45) fix CRD provider-networks.kubeovn.io
 * [d1c7a2ee3](https://github.com/kubeovn/kube-ovn/commit/d1c7a2ee3664ba1b63220e50cb42034a8408e687) fix: set vf mac
 * [949c28c25](https://github.com/kubeovn/kube-ovn/commit/949c28c25d31322e4f7b910c2395911a571c28a4) update qos ingress_policing_burst
 * [8a05bdc88](https://github.com/kubeovn/kube-ovn/commit/8a05bdc88d51c236b8a20040282a808b51ccada2) add field defaultNetworkType in configmap ovn-config
 * [1810dfc32](https://github.com/kubeovn/kube-ovn/commit/1810dfc32f22e42654fab674ae125927e14c512a) keep subnet's vlan empty if not specified
 * [4e28600d4](https://github.com/kubeovn/kube-ovn/commit/4e28600d4a42844832bfb2cb70f30b51dea0b21b) delete ecmp route when node is not ready
 * [d145f5759](https://github.com/kubeovn/kube-ovn/commit/d145f5759a3245dcce407cadcd5271034fe9a224) add del learned routes when remove ovnic
 * [6499e5859](https://github.com/kubeovn/kube-ovn/commit/6499e5859f92c0ba58f266aa308c795a2c52ba3b) [kubectl-ko] support trace in underlay networking
 * [23d84f0a7](https://github.com/kubeovn/kube-ovn/commit/23d84f0a7e8ef9e83777b003986f2e1bbdf11a38) fix subnet available IPs
 * [eced6bacd](https://github.com/kubeovn/kube-ovn/commit/eced6bacdcc66cb523a0ed47271061f8ce654056) fix bug for deleting ovn-ic lrp failed
 * [a4abbb2e8](https://github.com/kubeovn/kube-ovn/commit/a4abbb2e8af05b04e0b5c0702439b7aa5966b019) add node internal ip into ovn-ic advertise blacklist
 * [2ec0aa749](https://github.com/kubeovn/kube-ovn/commit/2ec0aa7494805e472182b52b0c5fb643899e083a) underlay/vlan network refactoring
 * [ead2c65f0](https://github.com/kubeovn/kube-ovn/commit/ead2c65f00263f653dc093abbaf5f2d64710eff3) chore: update ovn to 21.03
 * [651a634d6](https://github.com/kubeovn/kube-ovn/commit/651a634d6b78c9d719947fcaee6aa6cda4ec84a0) security: fix CVE-2021-3121
 * [8cff68519](https://github.com/kubeovn/kube-ovn/commit/8cff685191f6911f477d4632d43a409311213da1) list ls with label to avoid listing ts failure
 * [3fd9c7acd](https://github.com/kubeovn/kube-ovn/commit/3fd9c7acd32e1f361ab59b90db3c58a88d70b8dd) Update log error
 * [0fe67258a](https://github.com/kubeovn/kube-ovn/commit/0fe67258a9e65eaf46671425592ba73d271cbcb9) delete the process of ip crd delete in cni delete request
 * [9049fc725](https://github.com/kubeovn/kube-ovn/commit/9049fc725eb66761461268fde65a6c0a3d7673af) update networkpolicy process
 * [a5b22a21e](https://github.com/kubeovn/kube-ovn/commit/a5b22a21e9ab6a210a5a0e4f24367b4181e55e12) modify func name Additonal to Additional
 * [0cd5dcfec](https://github.com/kubeovn/kube-ovn/commit/0cd5dcfec78517ee738d6620fb5cf3ae18e9eda0) fix uninstall.sh execution in OVS pods
 * [b4ce83a2c](https://github.com/kubeovn/kube-ovn/commit/b4ce83a2cf2df2467cd015682ac2812f8b5dabbc) perf: enable tx offload again as upstream already fix it
 * [9ca47b658](https://github.com/kubeovn/kube-ovn/commit/9ca47b6584a99866e7fee94381285e34df9cd1fa) label lr, ls and lsp, and add label filter when gc
 * [37a045a31](https://github.com/kubeovn/kube-ovn/commit/37a045a31d2581a0373538a04a9362cf7cb158b0) security: add go build security options
 * [bdf91846b](https://github.com/kubeovn/kube-ovn/commit/bdf91846bd646d084487f7637d79a2ff9ec3ab6a) feat: ko support cluster operations status/kick/backup
 * [efdce4645](https://github.com/kubeovn/kube-ovn/commit/efdce464567c2a9705b3f6f70629662a17d48367) docs: update docs about vlan/internal-port/kubeconfig
 * [ced434053](https://github.com/kubeovn/kube-ovn/commit/ced434053d9b5bf280de476b3a93988a3eabb78b) add judge before use slices's index
 * [3d98d7626](https://github.com/kubeovn/kube-ovn/commit/3d98d7626ff7db93927b3c25f80531400e1dceff) update kind to version v0.11.1
 * [e1e63cfaf](https://github.com/kubeovn/kube-ovn/commit/e1e63cfaff83fea058bfb6ad584c849a427c619a) adapt to vfio-pci driver
 * [205f57124](https://github.com/kubeovn/kube-ovn/commit/205f571245c939820faf27e292c383ecaecb7397) fix IP/route transfer on node reboot
 * [a3cac539f](https://github.com/kubeovn/kube-ovn/commit/a3cac539f6bb13d52135641b6132a79098e7d2ca) add master check when a node adding to a cluster and config sb/nb address
 * [b98afeefc](https://github.com/kubeovn/kube-ovn/commit/b98afeefcd415ec6bb236b36bf58ff7797ae8035) update installation scripts
 * [2d750cbf1](https://github.com/kubeovn/kube-ovn/commit/2d750cbf1226dd5e8e6f3ed8d0dc762a3fa59883) enable hw-offload
 * [64b9abae6](https://github.com/kubeovn/kube-ovn/commit/64b9abae6083eb0305cd85d9ff1ddb3631a93cab) do not delete statefulset pod when update pod
 * [4359c1980](https://github.com/kubeovn/kube-ovn/commit/4359c1980534d3b6e38a977ac327b07c5da35cbe) fix: node route should filter out 'vpc'
 * [744e6577b](https://github.com/kubeovn/kube-ovn/commit/744e6577ba7d705ec94acc9a7a53dfd0c33907c6) feat: lb switch
 * [7ec2f994d](https://github.com/kubeovn/kube-ovn/commit/7ec2f994dd06332eef4db6ff4507ef9a0b6929fe) docs: show openstack docs and docker image status
 * [5484387f2](https://github.com/kubeovn/kube-ovn/commit/5484387f24f60af534b55c44bfe1e5dde4c6b0fe) fix: clean up gateway chassis list for external gw
 * [acc95f1d6](https://github.com/kubeovn/kube-ovn/commit/acc95f1d67332abeb15bb67244af15179111a772) add doc for openstack/kubernetes hybrid deploy
 * [e2973c4f0](https://github.com/kubeovn/kube-ovn/commit/e2973c4f04b32528a2769db200e9c9f7c8f56f94) configure OVS internal port after dummy interface
 * [8608b7e5a](https://github.com/kubeovn/kube-ovn/commit/8608b7e5ab97273adfca03ec9de05f1144937aec) some fixes in vlan initialization
 * [872340c87](https://github.com/kubeovn/kube-ovn/commit/872340c87cb2e0db84fcd52f9669b99333a1d21f) clean up vpc service
 * [fde899145](https://github.com/kubeovn/kube-ovn/commit/fde89914587de175191517877dbbd33960bff0ba) feat: vpc load balancer
 * [8ed91be4a](https://github.com/kubeovn/kube-ovn/commit/8ed91be4a2fcfe969a55721ed18ea419120bbdb6) fix: lsp may lost when server pressure is high
 * [42fbe86e7](https://github.com/kubeovn/kube-ovn/commit/42fbe86e7d0d66a3f8e8ebcaa81ef82f2875be58) fix: check crds when controller start
 * [a5fef59b5](https://github.com/kubeovn/kube-ovn/commit/a5fef59b57a840b1248b581b3d00959914055fd2) start evpc ph1
 * [31ee8c104](https://github.com/kubeovn/kube-ovn/commit/31ee8c104396b57c3cc0b9963e8d5f462d5aa691) start evpc ph1
 * [44db142e5](https://github.com/kubeovn/kube-ovn/commit/44db142e511c7d655eee655d98387dd88701bcb8) ci: retry arm build when failed
 * [96c139851](https://github.com/kubeovn/kube-ovn/commit/96c139851d3ddd4f35f90729a9cc813f0d28526e) update ecmp notes
 * [8c169322f](https://github.com/kubeovn/kube-ovn/commit/8c169322fcbd168033cfacaa63a641fb799549c8) add interface name in cni response
 * [aa88e2a21](https://github.com/kubeovn/kube-ovn/commit/aa88e2a21b8749e6531479162aceaf0f174e7cfa) add nicType for offload
 * [eb387428e](https://github.com/kubeovn/kube-ovn/commit/eb387428e114bee62591c6850a2d21a5973f8300) 1.Support to specify node nic name 2.Delete extra blank lines
 * [cb8cc6454](https://github.com/kubeovn/kube-ovn/commit/cb8cc6454f19b55432a8378402cd1f29ae308522) ignore update pod nic annotation when is not nil
 * [3a4347b92](https://github.com/kubeovn/kube-ovn/commit/3a4347b92b343cc9fbc0bd8f841213b681cca9c9) set default UnderlayGateway to true in vlan mode
 * [a0d78920f](https://github.com/kubeovn/kube-ovn/commit/a0d78920fb1aaabd08d38992254be1e7da87bb3c) unify logical entity list funcs (#863)
 * [9e563d847](https://github.com/kubeovn/kube-ovn/commit/9e563d8476fd46c4bd23e0183a0f6f042437c034) ci: remove dpdk ci
 * [e48a0894e](https://github.com/kubeovn/kube-ovn/commit/e48a0894e0e82cd672b3de53682b5b8049cc4601) correct vlan e2e testing
 * [f690085d8](https://github.com/kubeovn/kube-ovn/commit/f690085d89ea26821dba38175da185ba22f951f1) fix: remove rollout check
 * [2b2df3dc1](https://github.com/kubeovn/kube-ovn/commit/2b2df3dc1c925de42b1674c34e56014b0ee0ebf2) adapt internal tcpdump
 * [2531779a4](https://github.com/kubeovn/kube-ovn/commit/2531779a464221bf63c309d043de14096697fab3) update docker buildx install method
 * [eef1b0aa8](https://github.com/kubeovn/kube-ovn/commit/eef1b0aa88408c80cc5120fa5b3299bd0848b053) fix: remove wait ovn sb
 * [2e59e81c9](https://github.com/kubeovn/kube-ovn/commit/2e59e81c9113b217351205e985c264b3acc69a0a) fix: ci issues
 * [df47c489e](https://github.com/kubeovn/kube-ovn/commit/df47c489e86305a92485e98f367f5b1d208e5fae) fix: cleanup kube-ovn-monitor resource
 * [598cffdd7](https://github.com/kubeovn/kube-ovn/commit/598cffdd7f2ee931fd247e96a0d12426d0a5c62f) fix multi-nic.md
 * [f4b75bd04](https://github.com/kubeovn/kube-ovn/commit/f4b75bd04fee12b8e55896ba7f325ef6974e455d) fix: acl overlay issues
 * [2fe4fe1d4](https://github.com/kubeovn/kube-ovn/commit/2fe4fe1d4d4e79c332dea720b85284ae7b8bfd29) ci: split ovn/ovs into base image
 * [db2b7b069](https://github.com/kubeovn/kube-ovn/commit/db2b7b069a1f99a65aeeb69f758a46cd05baddde) add judge before use slices's index
 * [3e259ae90](https://github.com/kubeovn/kube-ovn/commit/3e259ae909877e7e787a2533de325f1d3a24cd16) update version to v1.7 in docs
 * [eb54dc037](https://github.com/kubeovn/kube-ovn/commit/eb54dc037ea75cc58ba01aab97be9d340eeff274) update master version to v1.8.0

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

 * [6329a2750](https://github.com/kubeovn/kube-ovn/commit/6329a2750cf0e21f206d16d5e73dbc3b88cb7607) release: prepare for 1.7.3
 * [a17dd60d6](https://github.com/kubeovn/kube-ovn/commit/a17dd60d60c86829c08bd43006bcda4a8ec6ed0c) fix: disable periodically gc
 * [26a355d9c](https://github.com/kubeovn/kube-ovn/commit/26a355d9c6026247cc328b07ec1723b670c94022) fix installation scripts
 * [be8b5ea7a](https://github.com/kubeovn/kube-ovn/commit/be8b5ea7abd44194021006d3c7158e5202ce69b5) fix StatefulSet down scale
 * [506e95d5e](https://github.com/kubeovn/kube-ovn/commit/506e95d5ea70fea7e32ff04659d5995367281000) fix: init node with wrong ipamkey and lead conflict
 * [7fed7ee32](https://github.com/kubeovn/kube-ovn/commit/7fed7ee32fb5d7232d7337d512cfc08365428ad4) refactor: mute ovn0 ping log and add ping details
 * [9110bcef7](https://github.com/kubeovn/kube-ovn/commit/9110bcef7c035535cf27f0c93aa655b454453daf) fix: wrong alias for iptables
 * [18053abd2](https://github.com/kubeovn/kube-ovn/commit/18053abd2fe0bb04afe12b1258af19b9c40e1b0d) fix: northd probe issues
 * [698d92c65](https://github.com/kubeovn/kube-ovn/commit/698d92c659bfd6517e873fffe289921ed0417850) fix IPAM for StatefulSet
 * [0c1baacb2](https://github.com/kubeovn/kube-ovn/commit/0c1baacb2825c42dca94fbdf820555fbbb86a8ed) append externalIds for pod and node when upgrade
 * [905b789fa](https://github.com/kubeovn/kube-ovn/commit/905b789fa6b02081ca2abaf1f5602c6004d1c67c) security: update base image
 * [7d86e2c5f](https://github.com/kubeovn/kube-ovn/commit/7d86e2c5f7fdb13164851180227d22b5d8099707) fix gc lsp statistic for multiple subnet
 * [6ce5cd8be](https://github.com/kubeovn/kube-ovn/commit/6ce5cd8be21a717405e94ed11b586086363e342a) fix: kubeclient timeout
 * [c3b72cff5](https://github.com/kubeovn/kube-ovn/commit/c3b72cff50eff765b11d310661e59ec05e640667) fix: serialize pod add/delete order
 * [530a3dd0c](https://github.com/kubeovn/kube-ovn/commit/530a3dd0c175a75d4c8bd59a583944871db3405c) refactor: reuse waitNetworkReady to check ovn0 and slightly improve the installation speed
 * [121c9a41a](https://github.com/kubeovn/kube-ovn/commit/121c9a41a1c0b01395e38f7d8a5ed0e62f972564) perf: increase ovn-nb timeout
 * [1f97edccd](https://github.com/kubeovn/kube-ovn/commit/1f97edccdbc004a36b5386acd0ebfbade6d8c855) fix: re-check ns annotation to avoid annotations lost
 * [c79244fc1](https://github.com/kubeovn/kube-ovn/commit/c79244fc17640ad62a0f18854c8901438dfb444a) perf: do not diagnose external access
 * [6bc241fca](https://github.com/kubeovn/kube-ovn/commit/6bc241fca0282969b67295977e1bec7b9de5fdf2) reactor: remove ovn ipam options
 * [74ab9aa16](https://github.com/kubeovn/kube-ovn/commit/74ab9aa16ee2f0d61f58af895a862c7f48db787e) perf: switch's router port's addresses to "router"
 * [a5791a015](https://github.com/kubeovn/kube-ovn/commit/a5791a01565a9fda089166fc8a481f60a8bdfa57) fix e2e testing
 * [6505e2e41](https://github.com/kubeovn/kube-ovn/commit/6505e2e413eff0b7f53843292ce7684da09b912b) fix variable referrence
 * [d1f145098](https://github.com/kubeovn/kube-ovn/commit/d1f145098691ebf86b482979639170cc0080b697) fix nat-outgoing/policy-routing on pod startup

### Contributors

 * Mengxin Liu
 * hzma
 * lut777
 * zhangzujian

## v1.7.2 (2021-09-08)

 * [cd650db44](https://github.com/kubeovn/kube-ovn/commit/cd650db44953a94c8a78077da19e4922b7ff4fa5) fix: VLAN CIDR conflict check
 * [4cabb12cc](https://github.com/kubeovn/kube-ovn/commit/4cabb12cc4c6a46cc006ab46590e69f5c7a5f628) perf: use link alias to filter packet
 * [af4a19832](https://github.com/kubeovn/kube-ovn/commit/af4a1983253a4fd8f0c958b798398ec34333155e) security: fix CVE-2021-3538
 * [c6daff2a7](https://github.com/kubeovn/kube-ovn/commit/c6daff2a7be7512849d139d008766867701823ce) prepare for release v1.7.2
 * [18241707f](https://github.com/kubeovn/kube-ovn/commit/18241707f2408e3e99ef6dd3fb4d61b88903b0e8) initialize ipsets on cni server startup
 * [cf32ab1e0](https://github.com/kubeovn/kube-ovn/commit/cf32ab1e02b4fdb2386ff71eae97592a8c9547d9) delete residual ovs internal ports
 * [7d94413f3](https://github.com/kubeovn/kube-ovn/commit/7d94413f3d6255c8c4ef2a7518ebede58fef085d) fix: ovn-northd svc flip flop
 * [316d141e3](https://github.com/kubeovn/kube-ovn/commit/316d141e36e47beaf4f159c7905345faceac6eb7) fix subnet conflict check for node address
 * [d44273e9e](https://github.com/kubeovn/kube-ovn/commit/d44273e9edf4ee4777762a26447bb559dbbef2f5) update comment for SetInterfaceBandwidth
 * [06810be23](https://github.com/kubeovn/kube-ovn/commit/06810be2370e1857480d1af659817e60523b7297) update encap ip by node annotation periodic
 * [99ec3d4a8](https://github.com/kubeovn/kube-ovn/commit/99ec3d4a8b650733d76ae04ef348b6f3d2301fda) delete subnet AvailableIPs and UsingIPs para
 * [c57c6dbc7](https://github.com/kubeovn/kube-ovn/commit/c57c6dbc76cc2ebdbbd4cebf2aca9a34e5fdacfc) fix ipset on pod creation/deletion
 * [ef9dbc5bb](https://github.com/kubeovn/kube-ovn/commit/ef9dbc5bb5f39aaf6812c2042f487293378ca067) add ready status for provider network
 * [8906e457c](https://github.com/kubeovn/kube-ovn/commit/8906e457cba6ed76976ba20dcd025bb5e30938fa) avoid Pod IP to be the same with node internal IP
 * [85b572395](https://github.com/kubeovn/kube-ovn/commit/85b5723951bf8991355a070633ac2e7d4d665983) update node labels and provider network's status.readyNodes when provider network is not initialized successfully in a node
 * [078c0c8be](https://github.com/kubeovn/kube-ovn/commit/078c0c8be697352b3921878c24a48ab780b6fc5c) fix issues in underlay networking
 * [2919288ad](https://github.com/kubeovn/kube-ovn/commit/2919288ad12ddc394176531856663bcf16284fe9) fix IPv6-related issues
 * [aaf56e654](https://github.com/kubeovn/kube-ovn/commit/aaf56e654978295d4dc228602b03867ec9f00caa) ci: use stable version
 * [256098735](https://github.com/kubeovn/kube-ovn/commit/2560987355f067d365416d8ff5327720224dff95) fix: bad udp checksum when access nodeport
 * [78077f34f](https://github.com/kubeovn/kube-ovn/commit/78077f34f5cf5641afa9a2b9800a1b45f7b8e2d1) ensure provider nic is up
 * [154f21c3d](https://github.com/kubeovn/kube-ovn/commit/154f21c3d7b19cec787cd0d2043a0753b8608ed7) fix uninstall.sh
 * [7a4c5a59d](https://github.com/kubeovn/kube-ovn/commit/7a4c5a59d83b9aac0fb8dc6d841473dd3df5f063) fix gofmt lint
 * [169a32568](https://github.com/kubeovn/kube-ovn/commit/169a32568656f940a139fb0b78a1f4ee02992e84) if the string of ip is empty,program will die
 * [1065c8e47](https://github.com/kubeovn/kube-ovn/commit/1065c8e47e46a27250706c6f8dcfa183a5494b65) fix dual stack cluster created by kind
 * [dd756c059](https://github.com/kubeovn/kube-ovn/commit/dd756c0590b1b1e2e2547ead4889aa3bb2da605a) fix default bind socket of cni server
 * [6ebbbbf4f](https://github.com/kubeovn/kube-ovn/commit/6ebbbbf4fac32a5e540132014c2166a323aaf123) update kind to v0.11.1
 * [ad2b08ec6](https://github.com/kubeovn/kube-ovn/commit/ad2b08ec600416e4608d7661de124b8a54c18897) fix underlay networking on node reboot
 * [2ba31cc11](https://github.com/kubeovn/kube-ovn/commit/2ba31cc11fcb232d9dd27dc1179887a1520319d2) append pod/exec resource for vpc nat gw
 * [7831f8039](https://github.com/kubeovn/kube-ovn/commit/7831f80396ac6ec52830f1d1e89eb4e0fe4b3c55) fix: panic when node has nil annotations
 * [554cc0444](https://github.com/kubeovn/kube-ovn/commit/554cc0444f727c745e27b56ed30782b138770711) update qos process
 * [a47d92973](https://github.com/kubeovn/kube-ovn/commit/a47d92973893f766e3a76cd4e833ae8fe207fd09) delete attachment ips
 * [b633ab3cf](https://github.com/kubeovn/kube-ovn/commit/b633ab3cfa7ef69a6c64b4d26bbdf63f33c9c149) fix external_ids:pod_netns
 * [b3190ef88](https://github.com/kubeovn/kube-ovn/commit/b3190ef881ae573a4288b84a420250fa974a4210) fix subnet e2e
 * [ae3cc9546](https://github.com/kubeovn/kube-ovn/commit/ae3cc9546f098f3beb887a20424d33b1542aa5f1) ignore empty strings when counting lbs
 * [a9bee8098](https://github.com/kubeovn/kube-ovn/commit/a9bee8098dfb1cfd181e1e559bd4288d1928b403) fix iptables
 * [5cd1b14ed](https://github.com/kubeovn/kube-ovn/commit/5cd1b14ed798473f360c9ea4e94ea637e229bc44) fix image version
 * [a93e2dece](https://github.com/kubeovn/kube-ovn/commit/a93e2deceaa72ad0e07f7f27fe4730e3d0393b06) fix cleanup.sh
 * [0e3c1cbc7](https://github.com/kubeovn/kube-ovn/commit/0e3c1cbc7f92ae3fb8d29f55d4b16eb6063c64db) security: fix CVE-2021-33910
 * [50da96aea](https://github.com/kubeovn/kube-ovn/commit/50da96aeaff09cfcf37517dd007474da77001ac2) delete ecmp route when node is deleted
 * [851dd3034](https://github.com/kubeovn/kube-ovn/commit/851dd3034f31332c13d09bfb8f94912a323d6785) fix: if nftables not exists do no exit
 * [e48c985b5](https://github.com/kubeovn/kube-ovn/commit/e48c985b50f88413582ddfaa4a9b3829cfb2c8e9) delete overlapped var subnet
 * [1dfcf6dff](https://github.com/kubeovn/kube-ovn/commit/1dfcf6dff75d488022c06522cc3737f25bea2c95) match chassis until timeout
 * [4f09a0d53](https://github.com/kubeovn/kube-ovn/commit/4f09a0d53133cff5b932e2b45afee4800cb95e0a) update qos ingress_policing_burst
 * [a63de27a6](https://github.com/kubeovn/kube-ovn/commit/a63de27a6f898518e9399f19a6f8ae1354d68424) fix ipsets
 * [cc51be3d1](https://github.com/kubeovn/kube-ovn/commit/cc51be3d18768cb6e03a2ab8bc24fc6f6eadfe1b) update underlay e2e testing
 * [7cd02fef5](https://github.com/kubeovn/kube-ovn/commit/7cd02fef501a425e9c531e6fb8f699fc48cbc1c8) fix CRD provider-networks.kubeovn.io

### Contributors

 * Mengxin Liu
 * Ruijian Zhang
 * feixiang43
 * hzma
 * lut777
 * zhangzujian
 * 范日明

## v1.7.1 (2021-07-15)

 * [1b289a22c](https://github.com/kubeovn/kube-ovn/commit/1b289a22c791587b0269e8984118db35698542e8) ready for release v1.7.1
 * [795fbdf0f](https://github.com/kubeovn/kube-ovn/commit/795fbdf0febb10bfa93aee891688cf44bdb9cb6b) add field defaultNetworkType in configmap ovn-config
 * [dc440c76e](https://github.com/kubeovn/kube-ovn/commit/dc440c76e928b8e0d3ef3b170e156d20332334c3) keep subnet's vlan empty if not specified
 * [7b7eef98f](https://github.com/kubeovn/kube-ovn/commit/7b7eef98fc0c6d708809d1ff642ad510d4875dd8) update ecmp notes
 * [d26850de6](https://github.com/kubeovn/kube-ovn/commit/d26850de6cc8431b484c53e4c6b14db17e44e541) delete ecmp route when node is not ready
 * [72a73fb67](https://github.com/kubeovn/kube-ovn/commit/72a73fb679ee35ed2dbb5ff829e89b9b4909d581) delete the process of ip crd delete in cni delete request
 * [22a296e59](https://github.com/kubeovn/kube-ovn/commit/22a296e590d932d7d98615b6b4114b3dee7f3fb0) fix subnet available IPs
 * [b60760288](https://github.com/kubeovn/kube-ovn/commit/b60760288f79fad7213c54145c3563be95475829) [kubectl-ko] support trace in underlay networking
 * [0b877b965](https://github.com/kubeovn/kube-ovn/commit/0b877b9656867ba4637577996c13f543c9df1ba9) underlay/vlan network refactoring
 * [7c529a18f](https://github.com/kubeovn/kube-ovn/commit/7c529a18f089cfc159d482ea4d340352478c3111) adapt internal tcpdump
 * [10481d9b3](https://github.com/kubeovn/kube-ovn/commit/10481d9b3b37c8f511fa84dd2066080465eec70f) fix bug for deleting ovn-ic lrp failed
 * [1adb788fe](https://github.com/kubeovn/kube-ovn/commit/1adb788feb4102b0a4d739844550d0cf5425b480) add node internal ip into ovn-ic advertise blacklist
 * [f9d542ee0](https://github.com/kubeovn/kube-ovn/commit/f9d542ee0aca9b19e40e5bd8a73e10cb2c06d419) security: fix CVE-2021-3121
 * [498c7dd1b](https://github.com/kubeovn/kube-ovn/commit/498c7dd1b9f6c3a556094639d2e4bb971d14d953) feat: ko support cluster operations status/kick/backup
 * [d812c7467](https://github.com/kubeovn/kube-ovn/commit/d812c746720629f01c143a7c064478393ec717c0) fix uninstall.sh execution in OVS pods
 * [fd5125117](https://github.com/kubeovn/kube-ovn/commit/fd51251178985c994779114bc687c980cc404b11) perf: enable tx offload again as upstream already fix it
 * [f41d57429](https://github.com/kubeovn/kube-ovn/commit/f41d57429990103f6fcb34613f28bb7497d02fa5) security: add go build security options
 * [feedaca88](https://github.com/kubeovn/kube-ovn/commit/feedaca88ce7344abf15b2a828ad170cd74e4762) fix IP/route transfer on node reboot
 * [5406d7013](https://github.com/kubeovn/kube-ovn/commit/5406d70139946da7178df26c2610dac030170d0f) add master check when a node adding to a cluster and config sb/nb address
 * [136ead430](https://github.com/kubeovn/kube-ovn/commit/136ead4307c55493ac035bd0c19cc70177828a46) do not delete statefulset pod when update pod
 * [1ef87e133](https://github.com/kubeovn/kube-ovn/commit/1ef87e13317ea4f39ea6ff6de3389e00c5f74a40) fix: node route should filter out 'vpc'
 * [0761fe7ac](https://github.com/kubeovn/kube-ovn/commit/0761fe7ac17b2256d1ed23d299a9ec9f758c7314) some fixes in vlan initialization
 * [63122eb80](https://github.com/kubeovn/kube-ovn/commit/63122eb805d8f4ffc720e164685a5e04e1dc512b) fix: clean up gateway chassis list for external gw
 * [96e224519](https://github.com/kubeovn/kube-ovn/commit/96e224519b767965cf806fdc32c2f6a3d909e26c) ci: remove dpdk ci
 * [7003890e9](https://github.com/kubeovn/kube-ovn/commit/7003890e9c9c1abfd04ce2d5afaebd1b36c95d5d) correct vlan e2e testing
 * [dcdf75a38](https://github.com/kubeovn/kube-ovn/commit/dcdf75a38093154233072125f8b87b8d03586ea6) configure OVS internal port after dummy interface
 * [9b70842a0](https://github.com/kubeovn/kube-ovn/commit/9b70842a0ff644f3aac8d0be81e22f658c945eb1) fix: lsp may lost when server pressure is high
 * [1f48f9fd9](https://github.com/kubeovn/kube-ovn/commit/1f48f9fd9124e6089ad6c7a061f054a4476a2319) 1.Support to specify node nic name 2.Delete extra blank lines
 * [8c37d4b93](https://github.com/kubeovn/kube-ovn/commit/8c37d4b932268ea6fd2ff5159f6cdb2e378b0f4f) ignore update pod nic annotation when is not nil
 * [00e2e009e](https://github.com/kubeovn/kube-ovn/commit/00e2e009eb29f83aa360609b2627485072defa23) set default UnderlayGateway to true in vlan mode
 * [f11cdf942](https://github.com/kubeovn/kube-ovn/commit/f11cdf9421f2ba97a8aabeb23812953b7f46e82d) fix: remove rollout check
 * [2d67471db](https://github.com/kubeovn/kube-ovn/commit/2d67471db6e6a99ddb6c844832dc5f27323eac40) fix: remove wait ovn sb
 * [ba7d65532](https://github.com/kubeovn/kube-ovn/commit/ba7d655329e03042c05e3adbf736a767db16c75e) fix: cleanup kube-ovn-monitor resource
 * [1e1da5a55](https://github.com/kubeovn/kube-ovn/commit/1e1da5a5586c791b1ae4b55c7b47955b92cce4a5) fix: acl overlay issues
 * [00681fb09](https://github.com/kubeovn/kube-ovn/commit/00681fb09c32c5c545dbd011291a36aa87f5d794) update version to v1.7 in docs

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

 * [907b34d2b](https://github.com/kubeovn/kube-ovn/commit/907b34d2b7264436461c3b7d5fc02609e0a446d1) prepare for release v1.7.0
 * [ab727c98b](https://github.com/kubeovn/kube-ovn/commit/ab727c98be5dbe848dafc44025e766553508049e) diagnose: check sa related resource
 * [9bd2e9f87](https://github.com/kubeovn/kube-ovn/commit/9bd2e9f87dfe85062020c9ce957cff64e90242ec) fix: do not nat route traffic
 * [3bd14945b](https://github.com/kubeovn/kube-ovn/commit/3bd14945b4f2d998e939a1b3bd4d10b3b7535364) fix: release ip addresses even if pods not found
 * [f47941838](https://github.com/kubeovn/kube-ovn/commit/f47941838bd5acf9c1e87fb12cb01a0cff1ac688) fix typo
 * [2a2160d0c](https://github.com/kubeovn/kube-ovn/commit/2a2160d0c663b0d19bcd00347fe5239ea785ffa2) docs: add description of custom kubeconfig
 * [3dd99a791](https://github.com/kubeovn/kube-ovn/commit/3dd99a7910830c604d7bef30ff24415214b099d7) fix: add address_set to avoid error message
 * [ba40fd67e](https://github.com/kubeovn/kube-ovn/commit/ba40fd67e4a4c7bc6cf99c63549b97e8d65c5c0a) optimize Makefile
 * [cb95f4e6d](https://github.com/kubeovn/kube-ovn/commit/cb95f4e6db63a3476e7b11249f6ac25b5e387316) update vlan document
 * [31a96f219](https://github.com/kubeovn/kube-ovn/commit/31a96f219d162705345e5a76c278b79946aada58) add label to avoid deleting other
 * [6cd6b34b0](https://github.com/kubeovn/kube-ovn/commit/6cd6b34b0f1e836bc6cdf65532676dcce7e88390) delete unused log
 * [347340106](https://github.com/kubeovn/kube-ovn/commit/347340106c365edecfdca3756b1ea1461715cf05) add ovs internal-port for pod network interface
 * [9e715623d](https://github.com/kubeovn/kube-ovn/commit/9e715623deab30b7b72e21f03e001474fe77ed4a) support underlay mode with single nic
 * [d6c96d07d](https://github.com/kubeovn/kube-ovn/commit/d6c96d07dd56049ef708f8d156dadc57e3997e92) support underlay mode with single nic
 * [c1d3fc3cf](https://github.com/kubeovn/kube-ovn/commit/c1d3fc3cfe51d886e427923caa75b9fd9b4f4df7) fix: add node to pod allow acl
 * [ed49cd497](https://github.com/kubeovn/kube-ovn/commit/ed49cd4978c81982eb0dcabd3db4ef937b7af533) traffic rate for multus nic
 * [1b00190f3](https://github.com/kubeovn/kube-ovn/commit/1b00190f3025fa66ef0ba1c3a4b8145eeb9a98d8) add ovs internal-port for pod network interface
 * [775aec6c6](https://github.com/kubeovn/kube-ovn/commit/775aec6c6b24cfd9ab6fbcc46943556a1d282810) Add maintainers
 * [59847bc10](https://github.com/kubeovn/kube-ovn/commit/59847bc108cd37dc624da2c7d1332079263f6aea) add e2e tests for external egress gateway
 * [a0006ebf0](https://github.com/kubeovn/kube-ovn/commit/a0006ebf01b7a8425a72f8f1f51864585fde01e0) fix e2e testing on macOS
 * [0ff3d6bb7](https://github.com/kubeovn/kube-ovn/commit/0ff3d6bb78f6371790ca647cb2febc982210b9cb) ci: fix lint and scan error
 * [33e0ec27c](https://github.com/kubeovn/kube-ovn/commit/33e0ec27c302151b8c86fa47d17e13a0184df8d8) fix: check if provider network exists
 * [9e53d4cc8](https://github.com/kubeovn/kube-ovn/commit/9e53d4cc8b9382e9ef51471e5bf40250ebea31b0) update subnet document
 * [a2e4fec4f](https://github.com/kubeovn/kube-ovn/commit/a2e4fec4f943232b6fe6cac0d23a0e047dca11e3) rename ExternalGateway to ExternalEgressGateway
 * [1ccaec9af](https://github.com/kubeovn/kube-ovn/commit/1ccaec9af8fc86065276ecba48c7871c210ab5d5) fix installation doc
 * [34fb47594](https://github.com/kubeovn/kube-ovn/commit/34fb47594208c9a15b9edeb57668227b86feec48) fix: forward policy to accept
 * [bbbd091f6](https://github.com/kubeovn/kube-ovn/commit/bbbd091f6e0819405096ec5fd2f4ae77b5a21487) ci: fix lint error
 * [28cf4cc2d](https://github.com/kubeovn/kube-ovn/commit/28cf4cc2d2e69dda06b9ac2a1c61dab9db8925a8) traffic rate for multus nic
 * [0dcf69300](https://github.com/kubeovn/kube-ovn/commit/0dcf69300b864438685db904ce73679691d6d5d3) refactor: optimize service.go and subnet.go
 * [7719fc2ad](https://github.com/kubeovn/kube-ovn/commit/7719fc2ad42c542a8188c93cfb7c3cb260c8da2e) Check and Fetch all ValidatePodNetwork errors
 * [123ead482](https://github.com/kubeovn/kube-ovn/commit/123ead48272728ced58c721a86c19e34527d51d0) add judge about nic address
 * [17fe23027](https://github.com/kubeovn/kube-ovn/commit/17fe230273fdac9a979f2c3e523b5940f42e1286) implement new feature: external gateway
 * [01686e3e7](https://github.com/kubeovn/kube-ovn/commit/01686e3e77add612cdf1fc4d9fac93547c8c58e1) start_ic should run regardless of ts port
 * [c733c7e40](https://github.com/kubeovn/kube-ovn/commit/c733c7e40498f8eac88ca0d4f19b24d65a4c555e) add judge before use index
 * [ba709afb5](https://github.com/kubeovn/kube-ovn/commit/ba709afb5dbb11dc1041fcdf9f0c4e0bcb78ccee) specify ovs ops on diff nodes
 * [07089205d](https://github.com/kubeovn/kube-ovn/commit/07089205dc0f1031890f98886cbfb20fd6360050) fix mss rule
 * [4458a4d72](https://github.com/kubeovn/kube-ovn/commit/4458a4d7254010fe36028c46adf3d24c0eaf3e4b) Get node info from listerv1.NodeLister(index)
 * [19a7aed98](https://github.com/kubeovn/kube-ovn/commit/19a7aed98a20bdff6ba6cfa4f04bc541bd7dabc3) Clean up the wrong log
 * [27fe348a7](https://github.com/kubeovn/kube-ovn/commit/27fe348a7a7aa0a8e9807ff551754432ad911952) refactor: optimize subnet.go
 * [ddfd06b25](https://github.com/kubeovn/kube-ovn/commit/ddfd06b25e2bc35ca73814c690292f1d807ce521) Optimise the redundancy code
 * [bd55c104f](https://github.com/kubeovn/kube-ovn/commit/bd55c104fa532c030a0fad6512e02b2450b7a7f0) Handler the parse config error before used
 * [bd3f13dcc](https://github.com/kubeovn/kube-ovn/commit/bd3f13dccd3360efe3c6c9173d5d992b246f0e76) ci: remove 3-master e2e
 * [9e827e7b5](https://github.com/kubeovn/kube-ovn/commit/9e827e7b52f5cc2dcb08d9e2210d8ad2c9ced27f) Remove the unnecessary rm command
 * [587bbcdbb](https://github.com/kubeovn/kube-ovn/commit/587bbcdbb366e9b7dfae70e314acf11c9b29212d) Use localtime when the kube-ovn installed
 * [a52a38d06](https://github.com/kubeovn/kube-ovn/commit/a52a38d064e1ad83ae98b9255320b8750b02a95a) Fix the different time from container and host
 * [436e788be](https://github.com/kubeovn/kube-ovn/commit/436e788be597d039712fb32333d5b480d7a6da7e) add issue template
 * [5fc3cfb18](https://github.com/kubeovn/kube-ovn/commit/5fc3cfb184568cfc1b20c2b4a881881f0fe98224) add bgp doc
 * [f16fcb9ad](https://github.com/kubeovn/kube-ovn/commit/f16fcb9ada97e8060090b786b478c783e210ecaa) support afisafis
 * [d94af379b](https://github.com/kubeovn/kube-ovn/commit/d94af379bbc322c91c2b14a61e5dcf002f4565f9) feat: support graceful restart
 * [26a027252](https://github.com/kubeovn/kube-ovn/commit/26a0272527e5ad79652607ca20b2bf0bd7f6f486) fix: del might panic if duplicate delete
 * [41226d86f](https://github.com/kubeovn/kube-ovn/commit/41226d86f80d41d3fcacfc22d3e5b3bf9c9c920f) fix: lr-route for eip using nic-ip, and not external gateway addr.
 * [d176dac7c](https://github.com/kubeovn/kube-ovn/commit/d176dac7cc42aaa25ec5c1a8f43f595d24920f61) feat: support announce service ip
 * [136571d16](https://github.com/kubeovn/kube-ovn/commit/136571d167152b0d870e3b84d84da87de9734f1a) Fix some minor nits for docs
 * [2781a47b4](https://github.com/kubeovn/kube-ovn/commit/2781a47b46895e0c2e3d94514863a87f7ad564e0) add bpg options in bgp.md
 * [1b788902c](https://github.com/kubeovn/kube-ovn/commit/1b788902c388d7916dc711e44cf09cb1c09867fe) add Opstk&K8s ic doc
 * [cc8438161](https://github.com/kubeovn/kube-ovn/commit/cc84381617906003edfabe7ab9f92b91db3c4c57) add holdtime function
 * [b9e963393](https://github.com/kubeovn/kube-ovn/commit/b9e96339328c7340e297b34d8bf45dac38a45425) fix: do not re-generate ts port
 * [610f132bd](https://github.com/kubeovn/kube-ovn/commit/610f132bdbdb81be7b1bc1efc86ccbe132102200) fix: ignore root path doc ci
 * [bd1e0975f](https://github.com/kubeovn/kube-ovn/commit/bd1e0975f7dea75566ae7328b1fd3faef514e176) fix: do not gc learned routes
 * [be2048be3](https://github.com/kubeovn/kube-ovn/commit/be2048be3e636e7bb0cfc2105254ef3f23a68482) feat: add vxlan in README.md
 * [cbb2ddd47](https://github.com/kubeovn/kube-ovn/commit/cbb2ddd476f2e173f4f3e6dd440cc3da2d58dcb3) fix: get_leader_ip always return fist node ip
 * [03f597ce4](https://github.com/kubeovn/kube-ovn/commit/03f597ce47ed434ce5b1a28f5a4d67c464f68711) fix: remove tty error notification
 * [cc353bbca](https://github.com/kubeovn/kube-ovn/commit/cc353bbca9d547ac30e3ca39e816ed8838c999fd) fix ovn nb reconnect
 * [af2709dfa](https://github.com/kubeovn/kube-ovn/commit/af2709dfa32d79887c8573572a21475684afa2c1) add docs for 'multus ovn network'
 * [ffc20a915](https://github.com/kubeovn/kube-ovn/commit/ffc20a9158cb92cce59e105a0eff294ea940fd48) add vpc nat gateway docs
 * [a1ae937ac](https://github.com/kubeovn/kube-ovn/commit/a1ae937ac580bf9c529be99a56d8f45a8859c136) fix: static route for default multus network
 * [0489a72ad](https://github.com/kubeovn/kube-ovn/commit/0489a72adf650b8bf91fe1cf8df279e26ca1bafc) feat: support vxlan tunnel
 * [77f65449c](https://github.com/kubeovn/kube-ovn/commit/77f65449c1e5a1a5c90a256c10829c2a51ba53d4) append delete ovn-monitor in ovn.yaml
 * [c5ee49e82](https://github.com/kubeovn/kube-ovn/commit/c5ee49e8225f17f83393cd67ca666a33c49b6d32) split ovn-central and ovn-monitor
 * [e0890f727](https://github.com/kubeovn/kube-ovn/commit/e0890f727327afd792aed64521aa5c78a384ef41) Fix mount the systemid path
 * [fc92fbc2e](https://github.com/kubeovn/kube-ovn/commit/fc92fbc2eadfd2f83f16aac04ebf32dfe2d0fac7) handle update deployment vpc-nat-gw
 * [686681ef3](https://github.com/kubeovn/kube-ovn/commit/686681ef3603519588158a9b5e80f49dbac291b6) refactor: remove function genNatGwDeployment's return error
 * [064c38510](https://github.com/kubeovn/kube-ovn/commit/064c38510219e8fb81c1f0e65352ff15e35e6603) Update crd vpc-nat-gateways.kubeovn.io for pre-1.16
 * [a0dfea1b0](https://github.com/kubeovn/kube-ovn/commit/a0dfea1b0f32d9e5931f569409f0d0a2de079577) fix incorrect method for gateway node judgement
 * [86c99c378](https://github.com/kubeovn/kube-ovn/commit/86c99c378bb346cefc4a47005ec16c79150800a8) Fix the 'multus how to use' link
 * [1acb49921](https://github.com/kubeovn/kube-ovn/commit/1acb499213170a1a9d3f3d7c08a5374758455912) fix multi nic
 * [9c5ca0a04](https://github.com/kubeovn/kube-ovn/commit/9c5ca0a04b08b68d3d68c5dadc82e970b2fa1d10) fix duplicate imports
 * [b4750853d](https://github.com/kubeovn/kube-ovn/commit/b4750853db9ae994dc7a7599fa6e506885416984) fix: compatible with JSON format
 * [2a2cd27a6](https://github.com/kubeovn/kube-ovn/commit/2a2cd27a6a4f41125c4800a85ad87181f5ccb65d) fix: leader may change during startup, use cluster connection to set options.
 * [aad815484](https://github.com/kubeovn/kube-ovn/commit/aad8154848f71dab233f16eec3c160b2b2e8fa5c) fix SNAT on pod startup
 * [388119a75](https://github.com/kubeovn/kube-ovn/commit/388119a75ffe65896203136e5f4b6de22e2d3d23) fix development guide
 * [2efdac9a4](https://github.com/kubeovn/kube-ovn/commit/2efdac9a4b3a74b782f4f0531525ed311a0a6a33) fix gofmt
 * [c264bec18](https://github.com/kubeovn/kube-ovn/commit/c264bec18408064ddbfc7322af50c8f2cc688ea5) fix: configure nic failed when ifname empty
 * [763f8bcf7](https://github.com/kubeovn/kube-ovn/commit/763f8bcf79b1ce885b72d2b99bd9850fcdf941de) fix: port does not support vlan tag update
 * [a60764ea9](https://github.com/kubeovn/kube-ovn/commit/a60764ea98e695d91826057e143840b38d0c7663) fix build dev image
 * [faa7bc6a5](https://github.com/kubeovn/kube-ovn/commit/faa7bc6a5bc4e7d4bb2924bc4a4bb0c421437394) support hybrid mode for geneve and vlan
 * [d8472ba77](https://github.com/kubeovn/kube-ovn/commit/d8472ba77be74afae2bf1294d7f2f39265504cf3) remove extra space
 * [f9c836b6d](https://github.com/kubeovn/kube-ovn/commit/f9c836b6df232008743f7a2f5dc58a1bc54b8087) fix: compatible with no norhtd svc
 * [bbed09d36](https://github.com/kubeovn/kube-ovn/commit/bbed09d3634002299a2da36789aeb4ee5e329ef8) fix chassis check for node
 * [dfdf5f8b0](https://github.com/kubeovn/kube-ovn/commit/dfdf5f8b0fefc78d51e0b80b8b16f586924690a8) optimization for ovn/ovs status metric
 * [9e82ca3d5](https://github.com/kubeovn/kube-ovn/commit/9e82ca3d594ce169dc575f0eddf8f09c1918c162) fix: release norhtd lock when power off
 * [1fbfad523](https://github.com/kubeovn/kube-ovn/commit/1fbfad523fd9e95e9470181c76a5315ff291e4f9) add single node e2e
 * [f9ae6258e](https://github.com/kubeovn/kube-ovn/commit/f9ae6258e21e87a632bcb19f4acc6cc847612598) fix get pod attachment net
 * [0632e253a](https://github.com/kubeovn/kube-ovn/commit/0632e253a182793cd68c9bac841f56c83e0bb51d) support ovn defautl attach net
 * [2c1a8aa64](https://github.com/kubeovn/kube-ovn/commit/2c1a8aa6425e43055d5c3271ac96083a36a1da2f) add network-attachment-definitions clusterRole
 * [808a3a93b](https://github.com/kubeovn/kube-ovn/commit/808a3a93b3b4572f207480a5c94b9781939b0689) feat: multus ovn nic
 * [28e14188a](https://github.com/kubeovn/kube-ovn/commit/28e14188ae40ced07979f4e99fa231063ff9769d) update node ip when upgrade to dualstack
 * [0265747dc](https://github.com/kubeovn/kube-ovn/commit/0265747dc8bbb578eea25bc9f1963bcf2ebb6f5e) add details for prerequisite
 * [3e42f6842](https://github.com/kubeovn/kube-ovn/commit/3e42f68421b5e73a204fd08f0cd04d07c1ee11ab) Add Ecmp Static Route for centralized gateway
 * [b72e9d50b](https://github.com/kubeovn/kube-ovn/commit/b72e9d50bcfa8547233137a6a94c828172ad0ace) fix: disable offload if geneve port exists
 * [f4e665b95](https://github.com/kubeovn/kube-ovn/commit/f4e665b95f26927ec2aceecf4a709f27de4c03a9) disable offload for genev_sys_6081
 * [acade01b4](https://github.com/kubeovn/kube-ovn/commit/acade01b4c41b05332e0c6b3d923fdad1428fa48) refactor: optimize ovn command when error exists
 * [5251c2722](https://github.com/kubeovn/kube-ovn/commit/5251c2722310f89573337151d166a83d2e5232a1) add net-attach-def ClusterRole
 * [5126aedd1](https://github.com/kubeovn/kube-ovn/commit/5126aedd1c75680354bb7e44ea02fa01dcd6be53) add lsp with external_id
 * [ec7f7425b](https://github.com/kubeovn/kube-ovn/commit/ec7f7425b30d7c6cb27ebecb2586cb0d23917671) feat: multus ovn nic
 * [19e23d141](https://github.com/kubeovn/kube-ovn/commit/19e23d1419e04b6efc872fa29238e8e362a74015) fix: check ovn0 status
 * [c02afc00c](https://github.com/kubeovn/kube-ovn/commit/c02afc00c9c7835fcc267ff44de6e393f136e3c5) livenessprobe fail if ovn nb/ovn sb not running
 * [983831e0d](https://github.com/kubeovn/kube-ovn/commit/983831e0dfeb2c252bcc6d314d0f4d9df597ce9b) fix: disable checksum offload for ovn0 to prevent kernel issue
 * [d9f166b71](https://github.com/kubeovn/kube-ovn/commit/d9f166b7132a63619b305af7eed416915911af73) ignore ip6tabels check for v4 hostIP
 * [680802d61](https://github.com/kubeovn/kube-ovn/commit/680802d618956c48f6815820f2edb50c15b0644e) improve the code style of [import group ordering]
 * [8e38a79da](https://github.com/kubeovn/kube-ovn/commit/8e38a79da6c1c97bedbd5ff0103dae7bfc0ee60e) fix wrong sequence
 * [1e0d77c35](https://github.com/kubeovn/kube-ovn/commit/1e0d77c35f313d4d775963cf8dae2670349e1c92) update arm64 build
 * [638a03ac4](https://github.com/kubeovn/kube-ovn/commit/638a03ac4ef1edd42d57825901e9f1035f105e36) fix: restart ovn-controller to force update flows
 * [14784fbb9](https://github.com/kubeovn/kube-ovn/commit/14784fbb9c12b2c079319410ecb88cc7296ce209) fix: disable checksum validation
 * [a04dcfb62](https://github.com/kubeovn/kube-ovn/commit/a04dcfb626c6ad85083f40703fab0ea02a7735a8) Use public network effective image
 * [24095d7fe](https://github.com/kubeovn/kube-ovn/commit/24095d7fe927376d6bc315cf629303da3594dfc9) update usingips check when update finalizer for subnet
 * [54ef1af2a](https://github.com/kubeovn/kube-ovn/commit/54ef1af2afbb85584c16a1981d158372df2fc8fa) fix dependency
 * [717688d66](https://github.com/kubeovn/kube-ovn/commit/717688d66a20e0f13bd77dbd230eb7b7db4c6b61) Update vendor.
 * [496fc4ddd](https://github.com/kubeovn/kube-ovn/commit/496fc4dddbf1b863555029905a0f60fe3ed6afb5) trim space the port_binding's output
 * [00fdac83c](https://github.com/kubeovn/kube-ovn/commit/00fdac83cdfebeacccba8a8bb6ff21fe5dbaba58) refactor: remove unnecessary config logic
 * [b06dad21b](https://github.com/kubeovn/kube-ovn/commit/b06dad21b8c89c68d8d571293a03e8f712e53af2) update maintainers
 * [e5d9584e3](https://github.com/kubeovn/kube-ovn/commit/e5d9584e3c5c34f3a167877a8ffdf9698ab12785) docs: deprecated webhook
 * [92cc4ed37](https://github.com/kubeovn/kube-ovn/commit/92cc4ed376b62cdbeb1634bc5b4b2e5c60820be2) fix: add missing ovn-ic binary
 * [c0349e4fc](https://github.com/kubeovn/kube-ovn/commit/c0349e4fcb4a5afc278f2a1e72443f98da715e3f) chore: change action name
 * [1a448eccc](https://github.com/kubeovn/kube-ovn/commit/1a448eccce0d65f7cf48e0895f4a012e74ef70ff) chore update artworks
 * [537588c39](https://github.com/kubeovn/kube-ovn/commit/537588c397caec5b1ee5a32eb0150a436e58c9d6) fix: delete chassis_private when delete node
 * [a50fb1817](https://github.com/kubeovn/kube-ovn/commit/a50fb1817b7a3b4b8cbcf86e35b11583af64c91a) Add 'kubectl ko trace' command's default namespace
 * [fad9473d9](https://github.com/kubeovn/kube-ovn/commit/fad9473d90f360cf797955fa78b90aa3ab57e077) Add 'kubectl ko trace' command's default namespace
 * [77c92ca8c](https://github.com/kubeovn/kube-ovn/commit/77c92ca8c5ec5dd78268333a0d141687dceb470c) perf: reclaim heap memory after compaction.
 * [f3df58aea](https://github.com/kubeovn/kube-ovn/commit/f3df58aeafcb85d7107fa0a4974ac3d71f5f7466) remove the old script
 * [b69f389cf](https://github.com/kubeovn/kube-ovn/commit/b69f389cf6ea337da30fcc012eae208196f85abd) docs: add CNCF description
 * [08b95e747](https://github.com/kubeovn/kube-ovn/commit/08b95e747bd96d054a2ffd4371a0f0e7328de062) fix: gc not exist node error
 * [9f6614613](https://github.com/kubeovn/kube-ovn/commit/9f6614613cedfdbb30c268be96b3c68fc0755bf8) perf: use new option to decrease ovn-sb size
 * [9dc069083](https://github.com/kubeovn/kube-ovn/commit/9dc0690831cdc75d143b2a2eab20f3d6d739507c) fix: return err
 * [8bd446083](https://github.com/kubeovn/kube-ovn/commit/8bd4460832a207a62074cd60e8b8f731fa666b17) docs: add faq section
 * [482e6f71a](https://github.com/kubeovn/kube-ovn/commit/482e6f71a078396f51cd29fd223d3cb017e1a65c) add vpc nat gateway Dockerfile
 * [b0e983f04](https://github.com/kubeovn/kube-ovn/commit/b0e983f04521ffa9bf5fcac394331a3e11ab9774) feat: vpc nat gateway
 * [951e31ea3](https://github.com/kubeovn/kube-ovn/commit/951e31ea3135c72c0472cdcdb86d8a8864cf8b1a) add node address allocate check when init
 * [215c8f45b](https://github.com/kubeovn/kube-ovn/commit/215c8f45bbc3f77ba5928f50c897eea53aa8c2a6) update upgrade for ovn-default and join subnet
 * [a537985d9](https://github.com/kubeovn/kube-ovn/commit/a537985d9d5d4dea6208c1104f1800bf8dd281a8) fix: lint error
 * [d0d3e89cc](https://github.com/kubeovn/kube-ovn/commit/d0d3e89cc82159383c3e22e0a1fb62f815134c65) fix: add missing ovn-ic-db schema
 * [98651014c](https://github.com/kubeovn/kube-ovn/commit/98651014c8994ed08b4de0f9afaf32b9fe8c4384) update subnet ip num calculate
 * [d6bb03bd5](https://github.com/kubeovn/kube-ovn/commit/d6bb03bd527f03aa26a8b87d5a809cd1a80a603d) fix: masq traffic to ovn0 from other nodes
 * [0a7024f97](https://github.com/kubeovn/kube-ovn/commit/0a7024f97bba8511a84185474d907a236f71a93a) refactor: reduce duplicated GetNodeInternalIP function
 * [ac294669c](https://github.com/kubeovn/kube-ovn/commit/ac294669c7c7e2a24d2af6910494b6b8d175091b) chore: update go version
 * [0e9c717d0](https://github.com/kubeovn/kube-ovn/commit/0e9c717d05ebc26a01c2c6c3961a17316c675260) chore: move build dependency from alauda to kubeovn
 * [64fac57a9](https://github.com/kubeovn/kube-ovn/commit/64fac57a9c8626c916ff6ffdb103bd5f765ed729) feat: support set default gateway in install script
 * [ca71de3cf](https://github.com/kubeovn/kube-ovn/commit/ca71de3cfb8de87413de482c155df9927316bb4b) docs: fix typos
 * [582cb9ce6](https://github.com/kubeovn/kube-ovn/commit/582cb9ce6acb323fbe8e8dd19651901a24249b7f) Update install-pre-1.16.sh
 * [62fc20efe](https://github.com/kubeovn/kube-ovn/commit/62fc20efea653fc7463840c746ec7e70013fcf2f) Update install.sh
 * [87859ac19](https://github.com/kubeovn/kube-ovn/commit/87859ac1951acbe9c4331cdeb80e7eb10948c3e8) go import repo change to kubeovn
 * [1152744e9](https://github.com/kubeovn/kube-ovn/commit/1152744e9f47626310601a4c63a7af61e3fe6bf6) feat: vpc nat gateway
 * [298138e41](https://github.com/kubeovn/kube-ovn/commit/298138e41d5d36f0400633bc3183f191de0c74cf) Resolving typo.
 * [4701fcb37](https://github.com/kubeovn/kube-ovn/commit/4701fcb3741213c4fbcd6e5e1a151fdcd4c6d3c2) filter repeat exclude ips
 * [e3931f0ea](https://github.com/kubeovn/kube-ovn/commit/e3931f0ea926e91c36fb2ac2699cb4e080f87b58) modify ip count for dual
 * [a4ddb3604](https://github.com/kubeovn/kube-ovn/commit/a4ddb3604584dd5b558686e663face1cb2a15584) docs: add ARCHITECTURE.MD
 * [9eee6f938](https://github.com/kubeovn/kube-ovn/commit/9eee6f938da6d79aac59171b6f53a108d123bede) refactor: reduce duplicated function
 * [a7b687a05](https://github.com/kubeovn/kube-ovn/commit/a7b687a05fadf8acf7b25d4dd994f96a7aa67a5a) fix: add dpdk pod name
 * [d32b423b6](https://github.com/kubeovn/kube-ovn/commit/d32b423b68ffdb8d49394f65c99c9342fd73d699) Update cleanup.sh
 * [9faaff574](https://github.com/kubeovn/kube-ovn/commit/9faaff574f68bf0d763c3a29cfe4b31f26c0b659) Update cleanup.sh
 * [df065f94b](https://github.com/kubeovn/kube-ovn/commit/df065f94b4a835e8f6a1dc17d7e73570f4ec80ee) test: add service e2e
 * [60e49f5a0](https://github.com/kubeovn/kube-ovn/commit/60e49f5a0ff999a98e9edc733ad6b548bed224ab) modify test problem
 * [2dbcb76fa](https://github.com/kubeovn/kube-ovn/commit/2dbcb76fa31cce633a63f18d1ff0a2a592e16bf9) fix: kube-proxy check
 * [512044cb3](https://github.com/kubeovn/kube-ovn/commit/512044cb389df7257adca989f604e3a00da0ddd4) ovn-central: set default db addr same with leader node to fix nb and sb error 'bind: Address already in use'
 * [c755ef232](https://github.com/kubeovn/kube-ovn/commit/c755ef232313476a24111621cda522119aec7749) fix: reset ovn0 addr
 * [a168c2826](https://github.com/kubeovn/kube-ovn/commit/a168c28264e12dfcbec823c6756c76cb1c22f974) tests: add e2e for ofctl/dpctl/appctl
 * [f6dc58a5d](https://github.com/kubeovn/kube-ovn/commit/f6dc58a5dcabd0731db9abe1a5f92af7c396892e) ci: replace image
 * [b1d03370e](https://github.com/kubeovn/kube-ovn/commit/b1d03370e61d81a0d72a4f0f2b4917fbdcffbc59) docs: clarify dpdk usage scenario
 * [21d9940b9](https://github.com/kubeovn/kube-ovn/commit/21d9940b9d13257699291e516166ee02df1a45d2) ci: update kind version and set timeout
 * [8b833ee52](https://github.com/kubeovn/kube-ovn/commit/8b833ee52ec0139dcd75b55556b8fe9ce51703a0) Update install-pre-1.16.sh
 * [4b6f0eedb](https://github.com/kubeovn/kube-ovn/commit/4b6f0eedbed57a64c8b025f06a220f0a9670161a) Update install.sh
 * [f6f88501f](https://github.com/kubeovn/kube-ovn/commit/f6f88501f9876f9b95c0549abcc22a43733e18e6) refactor: remove duplicated call
 * [473cdc487](https://github.com/kubeovn/kube-ovn/commit/473cdc48766dea1a152b2c430069014cc4064804) Update kubectl-ko
 * [1ca17686b](https://github.com/kubeovn/kube-ovn/commit/1ca17686b9493f2e64b72d7b5e3d85f7e2df08d0) Fix missing square brackets in curl ipv6
 * [136336b21](https://github.com/kubeovn/kube-ovn/commit/136336b2124593ebedd8e5663f164b7ecf9b5981) Modify the health check for kube-proxy port, compatible with ipv6
 * [98a56dece](https://github.com/kubeovn/kube-ovn/commit/98a56dece7c20235cd806ed2cdef50cebb07de24) Update controller.go
 * [c52c067b9](https://github.com/kubeovn/kube-ovn/commit/c52c067b946b4aac7eef8cbb3002eb4aeefc9045) Fix: remove IsNotFound when get configmap external gateway
 * [74fa7729d](https://github.com/kubeovn/kube-ovn/commit/74fa7729d806b4129edb8604ef2effa0b79c13ca) Fix: check kube-proxy's 10256 port healthz
 * [d594554d7](https://github.com/kubeovn/kube-ovn/commit/d594554d73f83d7d5b9e601533c8fb27f225dc47) fix: ip6tables check error
 * [b17f23732](https://github.com/kubeovn/kube-ovn/commit/b17f23732c65807e84a7d520c5c64c6869e3ee8e) Add MAINTAINERS file
 * [2783c134f](https://github.com/kubeovn/kube-ovn/commit/2783c134f16a9ea9c172afd30206868025d677ec) add vpcs && vpcs/status clusterRole
 * [31e1226e0](https://github.com/kubeovn/kube-ovn/commit/31e1226e01080cdd4ce27ef7979a18ba2736439b) Update install-pre-1.16.sh
 * [f1efaa7f2](https://github.com/kubeovn/kube-ovn/commit/f1efaa7f256f0bb936a65b5a1a5b8b38d12d84ff) delete connect to ovsdb for ovn-monitor
 * [f69ae44be](https://github.com/kubeovn/kube-ovn/commit/f69ae44bec65b67e2ab61689de0aa27da7143b41) cni-bin-dir,cni-conf-dir configurable Fix https://github.com/alauda/kube-ovn/issues/655
 * [f5999b3b5](https://github.com/kubeovn/kube-ovn/commit/f5999b3b580f129c25c8882f136d6f35d25875c7) Update install.sh
 * [e13448aac](https://github.com/kubeovn/kube-ovn/commit/e13448aac6c524c3ee1bfa42f27b2569418e6a2c) Error: unknown command "ko" for "kubectl"
 * [7d56483a3](https://github.com/kubeovn/kube-ovn/commit/7d56483a3e76983a96eff5d894b3ad92df186c37) Fix: wrong split in FindLoadbalancer function
 * [34776b8a7](https://github.com/kubeovn/kube-ovn/commit/34776b8a7986225cae89be0b30773201578a84bb) vlan nic support regex
 * [f23093c4f](https://github.com/kubeovn/kube-ovn/commit/f23093c4f94194be1416d0aea760d0861c77b9fe) fix underlay gateway flood logs
 * [4a9901aa6](https://github.com/kubeovn/kube-ovn/commit/4a9901aa68f95ff8207cb45fb271c4d83095d563) fix: check required module before start
 * [8d4694f83](https://github.com/kubeovn/kube-ovn/commit/8d4694f835e724cac2cb23cfedbf47a1646d2044) docs: add underlay docs
 * [3713b2537](https://github.com/kubeovn/kube-ovn/commit/3713b2537f6bea24f4a1d87f64016265d5f5727e) chore: update ovn to 20.12 and ovs to 2.15
 * [1ab871307](https://github.com/kubeovn/kube-ovn/commit/1ab871307bf2c98b692bfff3f7d3e1db314dab34) prepare for next release
 * [a94803d32](https://github.com/kubeovn/kube-ovn/commit/a94803d3208faec094cafb7dfa80c6cb6a286218) fix: make sure northd leader change
 * [03487cf28](https://github.com/kubeovn/kube-ovn/commit/03487cf28ac435c1723b858f03471464a689504d) fix: make sure ovn-central is updated one by one
 * [9d3b78a31](https://github.com/kubeovn/kube-ovn/commit/9d3b78a319fcd623c8df02df1f87a1b7926e205e) fix: restart when init ping failed
 * [6e09c77de](https://github.com/kubeovn/kube-ovn/commit/6e09c77deeba8ff2643f3309b19ee0d109c6f838) fix: increase raft timer to avoid leader flap
 * [87aa15cbf](https://github.com/kubeovn/kube-ovn/commit/87aa15cbf9494d68d66d0e1494e7362c7f39aa76) pass golangci-lint
 * [134ea89d0](https://github.com/kubeovn/kube-ovn/commit/134ea89d0e196584547171d11c78caecda059cf7) add golangci-lint to github actions
 * [d325e7e0b](https://github.com/kubeovn/kube-ovn/commit/d325e7e0b606ca141b05a169ce3ea1f993782006) fix pod terminating not recycle ip when controller not ready
 * [87af4ca9f](https://github.com/kubeovn/kube-ovn/commit/87af4ca9ff374fc8ae1bcbe8af84e73532f2e7c3) fix: add new iptable cleanup commands
 * [d287063bf](https://github.com/kubeovn/kube-ovn/commit/d287063bf7e9a69fa740a6ea35f007e5878e78b8) modify static gw changed problem
 * [fcf3be190](https://github.com/kubeovn/kube-ovn/commit/fcf3be190e141fcc6099d34bcefd4313ae71ae81) Fix wait pod network ready take long time
 * [0b4e4458e](https://github.com/kubeovn/kube-ovn/commit/0b4e4458e51033f096eb229624a36fd18b39ce13) fix: when address is empty, skip route/nat deletion
 * [ed0e9ba22](https://github.com/kubeovn/kube-ovn/commit/ed0e9ba22a7e65fc1bc0fc50c220a9957a59ed44) fix: update ipam cidr when subnet changed
 * [06816efb5](https://github.com/kubeovn/kube-ovn/commit/06816efb55507304c44a33b90cf1e140533bc47a) modify test problem for dual-stack upgrade

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

 * [8e28e139b](https://github.com/kubeovn/kube-ovn/commit/8e28e139b14c6edc95d7dbd168807ac7b1f6ce19) prepare release for v1.6.3
 * [2818eb861](https://github.com/kubeovn/kube-ovn/commit/2818eb861af3c3cba8f5f1ecfa29e09eb7706910) fix: do not nat route traffic
 * [be20533ba](https://github.com/kubeovn/kube-ovn/commit/be20533bafab1cbd2ebff6404fe760ca88b48f44) fix: release ip addresses even if pods not found
 * [1bdff3443](https://github.com/kubeovn/kube-ovn/commit/1bdff3443cf716954e026733f482fdcc107a8342) security: fix crypto CVE
 * [f29958dbf](https://github.com/kubeovn/kube-ovn/commit/f29958dbfac8620786ae6240414c51ae067c3851) fix: add address_set to avoid error message
 * [04fc67f80](https://github.com/kubeovn/kube-ovn/commit/04fc67f801d3721c20ace44e34fae9ae9f3566e3) fix: add node to pod allow acl
 * [91d43e01a](https://github.com/kubeovn/kube-ovn/commit/91d43e01ae3ca38d8ae2bec34820182cf771fe2a) Handler the parse config error before used
 * [634f672bf](https://github.com/kubeovn/kube-ovn/commit/634f672bf6339be32959285ff03fbc2716afcc5c) fix: del might panic if duplicate delete
 * [7795b519c](https://github.com/kubeovn/kube-ovn/commit/7795b519c7e1aab3f6be63f27f99645ce46229af) fix: do not re-generate ts port
 * [37ed257fb](https://github.com/kubeovn/kube-ovn/commit/37ed257fb1e7a66495ef3ce90202c53922b562b8) fix: get_leader_ip always return fist node ip
 * [548a5c556](https://github.com/kubeovn/kube-ovn/commit/548a5c556ac6bab504c806a91e60742d26109c56) fix: do not gc learned routes
 * [4e8a7c998](https://github.com/kubeovn/kube-ovn/commit/4e8a7c99871faf286716a0626ccff7e1cf0e6a2d) fix: remove tty error notification
 * [9e060882b](https://github.com/kubeovn/kube-ovn/commit/9e060882b7219b904a3d06782c574b28eb1d506b) fix ovn nb reconnect
 * [1b35390fd](https://github.com/kubeovn/kube-ovn/commit/1b35390fd1cbd7bbf405346d08ead499828f0b34) perf: reclaim heap memory after compaction.
 * [703174a82](https://github.com/kubeovn/kube-ovn/commit/703174a82a5bc27a48b7c7a2fb9b1e0a811595e1) fix: leader may change during startup, use cluster connection to set options.
 * [14de53e73](https://github.com/kubeovn/kube-ovn/commit/14de53e7357302c8243f7477648e0996997acdf7) fix SNAT on pod startup

### Contributors

 * Mengxin Liu
 * Yan Zhu
 * caoyingjun
 * chestack
 * zhangzujian
 * 马洪贞

## v1.6.2 (2021-04-18)

 * [2f4211817](https://github.com/kubeovn/kube-ovn/commit/2f42118173983b94e82023b274850487bd144f05) release 1.6.2
 * [23c9240dc](https://github.com/kubeovn/kube-ovn/commit/23c9240dce812ecec1183b6b6b433d8f648cfc61) fix: configure nic failed when ifname empty
 * [6574447f6](https://github.com/kubeovn/kube-ovn/commit/6574447f6082538ec1571fd58242b41480b7bb8e) remove extra space
 * [b65d41ade](https://github.com/kubeovn/kube-ovn/commit/b65d41ade4a4bbd42703877b19a43ed98d069946) fix chassis check for node
 * [bec0d0f42](https://github.com/kubeovn/kube-ovn/commit/bec0d0f42a8623ce3d3a855c98edcce6abc4f7e1) fix: compatible with no norhtd svc
 * [ef76fcc06](https://github.com/kubeovn/kube-ovn/commit/ef76fcc06ffe2732106c1dc891c5f2ad44f637c7) fix: release norhtd lock when power off
 * [fefcff272](https://github.com/kubeovn/kube-ovn/commit/fefcff2726e1a3e13f8240b2ffc41df93f4c8fae) fix: disable offload if geneve port exists
 * [a16799237](https://github.com/kubeovn/kube-ovn/commit/a167992376bf4ff282cfe77b26d873083e0e367a) disable offload for genev_sys_6081
 * [12e6b0b18](https://github.com/kubeovn/kube-ovn/commit/12e6b0b182c37d5c2e2fe8cb4ccf4e9fb80ecfd9) rebuild to fix openssl cve
 * [a58623107](https://github.com/kubeovn/kube-ovn/commit/a58623107b68ff912713eab97593d4d0aeba4842) fix: check ovn0 status
 * [03956f1fc](https://github.com/kubeovn/kube-ovn/commit/03956f1fc42555d8fa74aa3e563b9d510e01c807) ignore ip6tabels check for v4 hostIP
 * [35f064953](https://github.com/kubeovn/kube-ovn/commit/35f0649537d2d6a79b7f775635c42026b64d735f) livenessprobe fail if ovn nb/ovn sb not running
 * [3f15c9238](https://github.com/kubeovn/kube-ovn/commit/3f15c9238da1c2444c8b3e18394c6231f4f1a636) fix: disable checksum offload for ovn0 to prevent kernel issue
 * [54f5102dd](https://github.com/kubeovn/kube-ovn/commit/54f5102dd9d08e58baeb3c30b201be9669cd9755) add node address allocate check when init
 * [07bea9354](https://github.com/kubeovn/kube-ovn/commit/07bea9354c55c9aa0e9d21059c675d19c36f4b0a) update arm64 build
 * [995022e6a](https://github.com/kubeovn/kube-ovn/commit/995022e6a8d00364d7133a079ee6dca902b87446) fix: restart ovn-controller to force update flows
 * [21c312c01](https://github.com/kubeovn/kube-ovn/commit/21c312c01508dcd4b91aef1308864bd3ce46c39b) fix: disable checksum validation
 * [73bb2d83b](https://github.com/kubeovn/kube-ovn/commit/73bb2d83b20685743332c5dde638fd802dd8d9cd) update usingips check when update finalizer for subnet

### Contributors

 * Mengxin Liu
 * danieldin95
 * halfcrazy
 * hzma
 * lut777

## v1.6.1 (2021-03-09)

 * [87e114817](https://github.com/kubeovn/kube-ovn/commit/87e114817fced318b64a79f9bf8f82a048447210) fix: add missing ovn-ic binary
 * [dbf53f6e2](https://github.com/kubeovn/kube-ovn/commit/dbf53f6e2b9d20bc76d138c46cedf87c2b0918de) release for 1.6.1
 * [2dcd7584b](https://github.com/kubeovn/kube-ovn/commit/2dcd7584b1ae7100ddcee2b194c441e4d3b0b86b) fix: delete chassis_private when delete node
 * [f8aeb887a](https://github.com/kubeovn/kube-ovn/commit/f8aeb887a007a31045b2c3fce9eb85817d9d9fe7) chore: update ovn to 20.12 ovs to 2.15
 * [35190e1c1](https://github.com/kubeovn/kube-ovn/commit/35190e1c197d2896a9491755ee02bc4c096c1bad) refactor: reduce duplicated function
 * [afe9a9f05](https://github.com/kubeovn/kube-ovn/commit/afe9a9f05d60b860dba32e0eb572058b3a0ebcc6) fix: masq traffic to ovn0 from other nodes
 * [96880905a](https://github.com/kubeovn/kube-ovn/commit/96880905a68d5a403d710ba8dc000b6ff5338ea6) ovn-central: set default db addr same with leader node to fix nb and sb error 'bind: Address already in use'
 * [cce2bb4d4](https://github.com/kubeovn/kube-ovn/commit/cce2bb4d4fce58a2ed09b49d45a72d95fe0f86de) fix: reset ovn0 addr
 * [8152bdf5e](https://github.com/kubeovn/kube-ovn/commit/8152bdf5e0615e3238a4763449e41b8d01ff6ebe) Fix: wrong split in FindLoadbalancer function
 * [33b0e186d](https://github.com/kubeovn/kube-ovn/commit/33b0e186d3bd8d3ba6452dfcc24574492537eb8f) fix underlay gateway flood logs
 * [9a8e78700](https://github.com/kubeovn/kube-ovn/commit/9a8e7870007aedcc3a4e5b3cf6429af317f9c66a) fix: check required module before start
 * [b70f6103f](https://github.com/kubeovn/kube-ovn/commit/b70f6103f3ab308fe940c65de6682662a012570a) fix: make sure northd leader change
 * [ecbd43e2a](https://github.com/kubeovn/kube-ovn/commit/ecbd43e2a4205896c7b9d45e63faf5e0a1319c07) fix: restart when init ping failed
 * [4b752988e](https://github.com/kubeovn/kube-ovn/commit/4b752988e7960452195380929c1ae9fa3d2555cf) fix pod terminating not recycle ip when controller not ready
 * [0e7946797](https://github.com/kubeovn/kube-ovn/commit/0e79467975372a51e19e515977f2dde2797f8184) fix: add new iptable cleanup commands
 * [cf725882c](https://github.com/kubeovn/kube-ovn/commit/cf725882c7f450b40e5f82dfc6214d84345f9d8e) Fix wait pod network ready take long time
 * [bbb7edc63](https://github.com/kubeovn/kube-ovn/commit/bbb7edc630544e906104d4132cd4e7a6fcc04394) fix: when address is empty, skip route/nat deletion
 * [7121fa801](https://github.com/kubeovn/kube-ovn/commit/7121fa801fe760520a3161799d7142801e5fc102) fix: update ipam cidr when subnet changed
 * [99d8981f9](https://github.com/kubeovn/kube-ovn/commit/99d8981f91b18b6a539ea34866edba736b189528) prepare for 1.6.1
 * [8559014f7](https://github.com/kubeovn/kube-ovn/commit/8559014f7950981623c8ad039298debbf08583aa) move build dependency from alauda to kubeovn
 * [9184aa939](https://github.com/kubeovn/kube-ovn/commit/9184aa939d387af3e7996e1536356854ca3a37ff) update upgrade for ovn-default and join subnet
 * [f11c6b3c2](https://github.com/kubeovn/kube-ovn/commit/f11c6b3c21b4b24358aba4114f42564ac8375d70) update subnet ip num calculate
 * [e5e6e302b](https://github.com/kubeovn/kube-ovn/commit/e5e6e302b40b35b7936897c549e8216f8112d7c3) fix: ip6tables check error
 * [23dcd2a35](https://github.com/kubeovn/kube-ovn/commit/23dcd2a35a5769768340ccadc3fcff63449680bf) delete unused import packet
 * [5ead6b1d4](https://github.com/kubeovn/kube-ovn/commit/5ead6b1d4bb120a41cd7ee4ad7f6b665127688b6) filter repeat exclude ips
 * [30217437d](https://github.com/kubeovn/kube-ovn/commit/30217437d26769cb82f85eabac07a5e41a4ee9a0) modify ip count for dual
 * [b4560b99c](https://github.com/kubeovn/kube-ovn/commit/b4560b99c5df7d351390a5156e45392dc7f4ff7a) modify test problem
 * [b4b55581b](https://github.com/kubeovn/kube-ovn/commit/b4b55581b6b827c5b1d84a09d3483d0d827e1082) add vpcs && vpcs/status clusterRole
 * [d6f14147a](https://github.com/kubeovn/kube-ovn/commit/d6f14147a7f07ce93c5ead2e953f1f547cded778) delete connect to ovsdb for ovn-monitor
 * [98859f9b2](https://github.com/kubeovn/kube-ovn/commit/98859f9b2cc502ae4835ae01ceb0f7be3536bdac) modify static gw changed problem
 * [255e20c69](https://github.com/kubeovn/kube-ovn/commit/255e20c699a7032af6a90eab4213bac834c4b36d) modify test problem for dual-stack upgrade

### Contributors

 * Mengxin Liu
 * Wan Junjie
 * Yan Zhu
 * cmj
 * hzma
 * wangyudong
 * xieyanker

## v1.6.0 (2021-01-04)

 * [d47ccb678](https://github.com/kubeovn/kube-ovn/commit/d47ccb678692e441a774d11477269a4c4e430544) release: 1.6.0
 * [b8f221bf7](https://github.com/kubeovn/kube-ovn/commit/b8f221bf7d47b2190acfd716878e1b5aa441a409) docs: add docs for vpc
 * [12cf140b1](https://github.com/kubeovn/kube-ovn/commit/12cf140b167755bcb7a29981f5962ff369689694) fix typo
 * [b13cb7bf8](https://github.com/kubeovn/kube-ovn/commit/b13cb7bf8f34516a9fe9cf64eb0d56b14644c7d1) ci: update go version to 1.15
 * [7f9eefedb](https://github.com/kubeovn/kube-ovn/commit/7f9eefedb1267d9d18059c638b23361d1c198891) Fix: replace the command to run the script via 'sh' with 'bash'
 * [076ab28f8](https://github.com/kubeovn/kube-ovn/commit/076ab28f80c46826c5237da406eb18eb38d4bb54) Fix the default mtu parameter's describe
 * [8e6086678](https://github.com/kubeovn/kube-ovn/commit/8e6086678b923eb032a8b95d13a9bf214b1f38e8) modify network policy process
 * [171dcff6d](https://github.com/kubeovn/kube-ovn/commit/171dcff6dd3b27899e06ae752f8fa34896b159de) upgrade for subnet from single protocol to dual-stack
 * [bbc68577b](https://github.com/kubeovn/kube-ovn/commit/bbc68577b91ab26cdec5208c02dc165fe73a8222) add network policy adapt for dual-stack
 * [c01766cfa](https://github.com/kubeovn/kube-ovn/commit/c01766cfaef9554b6acbb435d81050951c97a1de) feat: update ovn to 20.09
 * [315831aa0](https://github.com/kubeovn/kube-ovn/commit/315831aa0f5baacac396266d009f746565b79db0) docs: prepare docs for 1.6.0 release
 * [a1e7974fa](https://github.com/kubeovn/kube-ovn/commit/a1e7974fa950420d5e2520942335f6284f161bdc) perf: add pprof to pinger
 * [627956e95](https://github.com/kubeovn/kube-ovn/commit/627956e95e2c440b4b75709dfdc4e33050209815) doc for dual-stack
 * [02751bf42](https://github.com/kubeovn/kube-ovn/commit/02751bf42d196e6e5542b5284953a554eb83e857) Update the container nic name use the CNI_IFNAME parameter which passed by kubelet
 * [14f36814f](https://github.com/kubeovn/kube-ovn/commit/14f36814f7899402f62ef85941ac066b1f2312dc) ci: enable docker experimental feature
 * [9a785fc9e](https://github.com/kubeovn/kube-ovn/commit/9a785fc9eeb744ddcc3d8eb4d98cd419ca26910b) ci: build multi arch image
 * [03ff96e66](https://github.com/kubeovn/kube-ovn/commit/03ff96e66b8316348fa50ac7371cc27616464caa) <fix>(np) fix mulit np rule and gateway bug
 * [20f3fcb17](https://github.com/kubeovn/kube-ovn/commit/20f3fcb178c0bf300ad6b792de53cc9aab9218fd) fix start-db.sh echo message
 * [52b39d764](https://github.com/kubeovn/kube-ovn/commit/52b39d764dacece2dec406db485e46181f3bd7d3) fix: iface check error
 * [072870b16](https://github.com/kubeovn/kube-ovn/commit/072870b16e6858d3690b10db975b2f28da7e7b7b) fix: add missing ping due to deb build
 * [efdd3913b](https://github.com/kubeovn/kube-ovn/commit/efdd3913b22a2bed4a98b8882897403869fc82aa) fix: find iface by full match first then regex match
 * [f922ef752](https://github.com/kubeovn/kube-ovn/commit/f922ef752883131d5d70abdaa8a2bb4b3235ef32) fix: livenessProb/readinessProb might conflict when run logrotate at same time
 * [f1fe2b2ea](https://github.com/kubeovn/kube-ovn/commit/f1fe2b2ead442a58331efe8fb1b47ac7bc4858f6) modify subnet and ip crds
 * [a2d76df7c](https://github.com/kubeovn/kube-ovn/commit/a2d76df7c0fea1b64289024a4cf637ef8657e2c2) modify service vip parse error
 * [8aa5d0a4f](https://github.com/kubeovn/kube-ovn/commit/8aa5d0a4f978a85a460fe2adcc05b4f6b1a39dd5) update vendor
 * [44381c74e](https://github.com/kubeovn/kube-ovn/commit/44381c74e23437b03fdd31ce6b30f0f5a2c29005) update client-go
 * [96c1c1003](https://github.com/kubeovn/kube-ovn/commit/96c1c1003535a87166bbf1cf6fbccc9a99a99cc6) fix: np with multiple rules
 * [87e6ded04](https://github.com/kubeovn/kube-ovn/commit/87e6ded0411e1a27bc18b0d807c5eacff028bb58) modify loop error for get metrics
 * [1e2a74774](https://github.com/kubeovn/kube-ovn/commit/1e2a747749b8a702cf59304f00d1549778ff0e34) diagnose: add more diagnose info
 * [aea12bae5](https://github.com/kubeovn/kube-ovn/commit/aea12bae5a744b1b6c688ed5343cfab729dcd802) ci: trigger action when yamls change
 * [7bd6bf39f](https://github.com/kubeovn/kube-ovn/commit/7bd6bf39f793698e1b429018fb7a1b10cb19e192) fix: ha e2e failed
 * [56774aafd](https://github.com/kubeovn/kube-ovn/commit/56774aafd2d98d4589b2e4fd18ea758b7e6cf66f) fix: allow traffic to gateway
 * [a78c2661b](https://github.com/kubeovn/kube-ovn/commit/a78c2661bdc559e6b00521ae7ee62399ac633f05) fix: cni-server default encap ip use right interface ip
 * [7d31e617a](https://github.com/kubeovn/kube-ovn/commit/7d31e617a42719e68e338d60a278774e51f5146b) feat: change default build image to ubuntu
 * [e2cd78715](https://github.com/kubeovn/kube-ovn/commit/e2cd7871583f812271089c5002743460246e6242) add build for dualstack
 * [ddda63320](https://github.com/kubeovn/kube-ovn/commit/ddda633204486877d8176476d1bd0470a84c3ecc) feat: distributed eip
 * [a6fef94a8](https://github.com/kubeovn/kube-ovn/commit/a6fef94a823be183eb80ca14be4703300e2c5add) Add CNI modify for dualstack
 * [a54bfc284](https://github.com/kubeovn/kube-ovn/commit/a54bfc2840f84d6fcad315a7ba0fd05ded6c7d12) Debian: Add debian docker image support
 * [8a01cb1c2](https://github.com/kubeovn/kube-ovn/commit/8a01cb1c211e3becefc46262c0fc78257be47b02) Add adaption for dualstack, part of daemon process.
 * [9738af184](https://github.com/kubeovn/kube-ovn/commit/9738af184b7604013ef166a2fdf6ec1043d624a9) chore: reduce binary size
 * [6483d6e33](https://github.com/kubeovn/kube-ovn/commit/6483d6e330cb4f46a9e260d0d65add82c757ab91) modify build problem
 * [dab50b33d](https://github.com/kubeovn/kube-ovn/commit/dab50b33d5c7ac8e51ccb470a257ba4bb3a332fd) Append ip monitor to document
 * [344288195](https://github.com/kubeovn/kube-ovn/commit/344288195ea1a74df7f0b87fd32fed517b59fc89) license: fix felix dir
 * [2ef665683](https://github.com/kubeovn/kube-ovn/commit/2ef6656832d1c091acd33d83472efef7e871c886) feat: support advertise subnet route
 * [ecbd01a65](https://github.com/kubeovn/kube-ovn/commit/ecbd01a65546239c2786e0fe00b611f0c17fbb01) Add IP Num Alert
 * [d64e69317](https://github.com/kubeovn/kube-ovn/commit/d64e693178ce90a3dcec71a2ecfefb40b411ec4a) Add adaption for dualstack, part of controller process.
 * [7246037b1](https://github.com/kubeovn/kube-ovn/commit/7246037b16227deb35d4e675d056de519d432228) convert ip to string
 * [2aecb3d9b](https://github.com/kubeovn/kube-ovn/commit/2aecb3d9b3890b7d16dad0d17e5cb7d2c80699dd) add pod static ip validate
 * [b58e01b61](https://github.com/kubeovn/kube-ovn/commit/b58e01b6185ccccb02ca722e2a6f25498cbe60a9) chore: add COC and roadmap
 * [7bbdc00f2](https://github.com/kubeovn/kube-ovn/commit/7bbdc00f29e9011731d0f72b900afb5d8f77b3eb) fix: move felix to self repo to remove bird license
 * [d2b570cfb](https://github.com/kubeovn/kube-ovn/commit/d2b570cfbdd6b62314c4b646538305decaedf60a) Add license scan report and status
 * [86584b95d](https://github.com/kubeovn/kube-ovn/commit/86584b95d7ea4498ac61b026b46fa004c459823c) fix: default network
 * [ccea68bfa](https://github.com/kubeovn/kube-ovn/commit/ccea68bfa249b8d382c21e7133af4e84c6288d5b) release for 1.5.2
 * [07347501a](https://github.com/kubeovn/kube-ovn/commit/07347501aa62d191fb08824bf76cdbe1c7590f58) fix: ovn-ic support ssl
 * [4d8b186a3](https://github.com/kubeovn/kube-ovn/commit/4d8b186a35da4fcd0dc6d0f0c5959d292311061b) fix: nat rules can be modified
 * [f535460ff](https://github.com/kubeovn/kube-ovn/commit/f535460ff720851bee6586840a786b9d1cd23d1a) fix: remove svc cidr routes
 * [e3082cd77](https://github.com/kubeovn/kube-ovn/commit/e3082cd7727ffd47aeae6af0ea47dabbd397a80e) ci: specify ubuntu version to make github action happy
 * [f6cce9a0a](https://github.com/kubeovn/kube-ovn/commit/f6cce9a0afabe91904cd2cd13a9733c877cdd9e3) fix: specify exec container to mute warning message
 * [2215c05f4](https://github.com/kubeovn/kube-ovn/commit/2215c05f45f9f42c0fffe57a342d73d01f5da103) feat: remove cluster ip dependency for ovn/ovs components
 * [a9747b316](https://github.com/kubeovn/kube-ovn/commit/a9747b3161f51844cf3a052c6c589c0d4f580e9d) fix: add resources limits to avoid eviction
 * [005711961](https://github.com/kubeovn/kube-ovn/commit/005711961cf3b5cc1a11c06558d1b0b9f21d69ba) fix: vpc static route manage
 * [8deb5d8d0](https://github.com/kubeovn/kube-ovn/commit/8deb5d8d05babbe20881f492a775fc86d08b7b8d) fix: validate vpc subnet
 * [256ac6c53](https://github.com/kubeovn/kube-ovn/commit/256ac6c5350c5e8a67657a06872928c86b491363) Fix external-address config description
 * [ccda611a5](https://github.com/kubeovn/kube-ovn/commit/ccda611a5c3d8cc196bfeaab1985350f0168b7d9) Fix the problem of confusion between old and new versions of crd
 * [f2f648011](https://github.com/kubeovn/kube-ovn/commit/f2f6480112272ce221bf0bd4da3010633a15a541) fix: ovn-central check if it exits in NODE_IPS
 * [5b973a89e](https://github.com/kubeovn/kube-ovn/commit/5b973a89e501dd2e1c27cc0f78ca7267322d2827) fix: check ipv6 requirement before start
 * [86941a8a8](https://github.com/kubeovn/kube-ovn/commit/86941a8a87885765ceaafa5d9a07cf100723004e) feat: add ovs/ovn log rotation
 * [ef41733c5](https://github.com/kubeovn/kube-ovn/commit/ef41733c558ffd68c7b7ec79c91f93afbef727de) add node ping total count metric
 * [5e6bd9112](https://github.com/kubeovn/kube-ovn/commit/5e6bd9112b69ea1928830084961d4b3286381ccd) diagnose: add ovs-vsctl show to diagnose results
 * [7301e9926](https://github.com/kubeovn/kube-ovn/commit/7301e992665c6b18f47384ddc1e0fb36c07e8274) fix: nat rules
 * [6026028a4](https://github.com/kubeovn/kube-ovn/commit/6026028a48bdffb9252af001e3df110479287fa2) fix: masq other nodes to local pod to avoid nodrport triangle traffic
 * [d41110ec1](https://github.com/kubeovn/kube-ovn/commit/d41110ec10d8887c44c707e51dd4b1a8c6823221) Update install.sh to allow dpdk limits configuration (#546)
 * [a128d7fcc](https://github.com/kubeovn/kube-ovn/commit/a128d7fcc4a752f7deffa8aeb3d6c26bbf0eb76f) format
 * [b6ad17b53](https://github.com/kubeovn/kube-ovn/commit/b6ad17b537389e64f306d4837538b4c0d0ef0d59) test: e2e uses IPVS cluster by default
 * [f6951cf5d](https://github.com/kubeovn/kube-ovn/commit/f6951cf5d98db035df3471364bc396682c87f703) chore: update go version to 1.15
 * [1f703c3d3](https://github.com/kubeovn/kube-ovn/commit/1f703c3d3f49f887de951b4fe0b057a44e7f1fe6) fix: tolerate all taints
 * [f8ace73c1](https://github.com/kubeovn/kube-ovn/commit/f8ace73c190930b21ce005c8a7ad9a3d4b0ace7d) feature: add vpc static route
 * [f62cb4ebf](https://github.com/kubeovn/kube-ovn/commit/f62cb4ebfe52ef3de8f52b2f5c84acaee74e705b) fix: cleanup script error
 * [3bac21f7e](https://github.com/kubeovn/kube-ovn/commit/3bac21f7ecaa674eaa386e8d55f010a20aaac101) docs: modify eip config description
 * [1f07d96b7](https://github.com/kubeovn/kube-ovn/commit/1f07d96b7081b0496fb339635385f09f069dc6de) security: remove sqlite to mute cve warning
 * [015bc6259](https://github.com/kubeovn/kube-ovn/commit/015bc6259b7cb9e32bca94fa3a781371cf6a6c0a) test: add e2e for kubectl-ko
 * [aa86e406f](https://github.com/kubeovn/kube-ovn/commit/aa86e406fe17f9cf9c76803514e7b4c373e3bb8b) feat: pinger can return exit code when failed
 * [2cf855ecd](https://github.com/kubeovn/kube-ovn/commit/2cf855ecd7b4550fc4a273aeac7c3eb9d6641aed) fix: nat traffic that from host to svc
 * [cbe0ad55f](https://github.com/kubeovn/kube-ovn/commit/cbe0ad55f41c47a4d8e5f963802e7051f4561ab6) docs: new feat for disable-ic, regex iface and pod bind subnet
 * [5dbaf2d3a](https://github.com/kubeovn/kube-ovn/commit/5dbaf2d3a528e0bf10febcb9877bea7d51bcd003) sync the default subnet of ns by vpc's status
 * [dd2234f4e](https://github.com/kubeovn/kube-ovn/commit/dd2234f4e65d8fd31f2cad84aa055d12b3ae46e9) fix: devault vpc lb/dns
 * [32c49c1b9](https://github.com/kubeovn/kube-ovn/commit/32c49c1b93842130e60f74dd477aad0aad9c5a30) fix: shutdown vpc workqueue
 * [67076d62f](https://github.com/kubeovn/kube-ovn/commit/67076d62f0e3f26af1a864678e660edadfaf5464) fix: subnet CIDRConflict
 * [d5b819b03](https://github.com/kubeovn/kube-ovn/commit/d5b819b03710f149ae4c8d66687e87ea09589827) fix: subnet bind to ns
 * [921190ef8](https://github.com/kubeovn/kube-ovn/commit/921190ef8534b0d591e9f89c6eb9ca4b07d2fbc8) feature: add vpc crd
 * [b5ecac95c](https://github.com/kubeovn/kube-ovn/commit/b5ecac95cbb27b39250f870dd6e6c885ca7dae79) Release and gc the resources in vpc
 * [15eca9dca](https://github.com/kubeovn/kube-ovn/commit/15eca9dca39aa497cc3670bcb453aff9d020acdc) fix: gc logic router
 * [91fec5631](https://github.com/kubeovn/kube-ovn/commit/91fec5631ca0ecbdd3d677282a25e8029169ecad) gc and clean vpc
 * [7a0e28b98](https://github.com/kubeovn/kube-ovn/commit/7a0e28b98bfcbf90ee6766bf89a72d85df51428c) Remove the VPC while removing the default subnet
 * [99217cec1](https://github.com/kubeovn/kube-ovn/commit/99217cec142f3e6943280e34a0be85926664ae7c) feature: support custom vpc
 * [9d821bce0](https://github.com/kubeovn/kube-ovn/commit/9d821bce095b59b17cee1d86f365fa5032d74fcf) chore: refactor log
 * [240cd800a](https://github.com/kubeovn/kube-ovn/commit/240cd800a7545356d723f28c09e3cfdee5d8fe87) feat: iface support regexp
 * [94b6b1b59](https://github.com/kubeovn/kube-ovn/commit/94b6b1b59b089023864a70bf5c55334926a0abdf) feat: support disable interconnection for specific subnet
 * [652190c35](https://github.com/kubeovn/kube-ovn/commit/652190c359fa7bf0f37db317dc4f4680e70c1fb5) modify review problems
 * [7285581a5](https://github.com/kubeovn/kube-ovn/commit/7285581a535b3e9207d1a8231fba9e7fa852d4cc) docs: v1.5.1 changelog
 * [47f0acbbd](https://github.com/kubeovn/kube-ovn/commit/47f0acbbd9ca240abb2946b6a5d39d5fa271c0bd) perf: accelerate ic and ex gw update
 * [bafac87ee](https://github.com/kubeovn/kube-ovn/commit/bafac87ee1c885dfbefeee26db8fc4d5364ef835) fix: missing version date
 * [8ef12007c](https://github.com/kubeovn/kube-ovn/commit/8ef12007cc3b77612ba132fef4c0864a8aa92ec6) fix: check multicast and loopback subnet
 * [3b20abb0e](https://github.com/kubeovn/kube-ovn/commit/3b20abb0e7770d6cf6722f144b4b307ab5caac82) monitor: refactor grafana dashboard
 * [f9cbaea5b](https://github.com/kubeovn/kube-ovn/commit/f9cbaea5b3ea9adc6744064fde8b5841fc51c0d0) docs: do not allow install to namespace other than kube-system
 * [559e2cd8c](https://github.com/kubeovn/kube-ovn/commit/559e2cd8cbe25370b6745b90787781d147959a29) update review problems for ovn_monitor
 * [1c356a365](https://github.com/kubeovn/kube-ovn/commit/1c356a3659885d38cf1ac840b6e6d7e237f99967) monitor: add more dashboard
 * [aa7b20d75](https://github.com/kubeovn/kube-ovn/commit/aa7b20d75a0ff848cf99439ac36ce48d840fdb5d) chore: add vendor
 * [97d64f934](https://github.com/kubeovn/kube-ovn/commit/97d64f9342d77463fd7476d9511f4e924303ff8e) Updated Dockerfile.dpdk1911 to use Centos8 and DPDK19.11.4
 * [b4aa989da](https://github.com/kubeovn/kube-ovn/commit/b4aa989da6b93012e0c17410547cdb85970a4331) fix: CodeQL scan warning
 * [a27e17603](https://github.com/kubeovn/kube-ovn/commit/a27e176039549b00d8d914c387f565013f5315d3) fix: ipt wrong order and add cluster route
 * [9eb96dd78](https://github.com/kubeovn/kube-ovn/commit/9eb96dd788d3d5fbbabbadb9a867257e719a8be5) opt: only allow specifies default subnet
 * [0da634e8f](https://github.com/kubeovn/kube-ovn/commit/0da634e8fe592d16223257cd67cc5248331a21aa) chore: reduce image size
 * [93bf54235](https://github.com/kubeovn/kube-ovn/commit/93bf5423534d8f775be65b9fd7252d6a05677879) feature: Support for namespace binding multiple subnets
 * [e37159c23](https://github.com/kubeovn/kube-ovn/commit/e37159c23dea126b91aae598ce03c67fcc23935f) docs: fix multi nic subnet options
 * [c35a159b9](https://github.com/kubeovn/kube-ovn/commit/c35a159b974e321432e562b94a82ebd324271e7d) docs: add pinger/controller/cni metrics
 * [7f5b42374](https://github.com/kubeovn/kube-ovn/commit/7f5b423742d743ff4a9213df19c685f49c3532f6) fix: add default ssl var for compatibility
 * [59b706964](https://github.com/kubeovn/kube-ovn/commit/59b706964218988a5d6e5fb7623436a3a8a831df) Add monitor doc
 * [bb130cac2](https://github.com/kubeovn/kube-ovn/commit/bb130cac2cc0c2cc961f94750a72bc510d7d78fe) fix: ipv6 network format when update subnet
 * [dc62d1050](https://github.com/kubeovn/kube-ovn/commit/dc62d10506a7a9f9ff2f2f9d8b6514d14b3d008f) fix: ipv6 len mismatch
 * [6088851d4](https://github.com/kubeovn/kube-ovn/commit/6088851d49c5d21f37117a85c8e3a3b69b6a37f1) chore: add version info
 * [88001376d](https://github.com/kubeovn/kube-ovn/commit/88001376db58d8640ac67e294b00a25c57613cea) metrics: add ovs client latency metrics
 * [3cafd5f8a](https://github.com/kubeovn/kube-ovn/commit/3cafd5f8a5d57c3f77ee9ecd0b0ec4bbcbee09aa) Add OVN/OVS Monitor
 * [89567776a](https://github.com/kubeovn/kube-ovn/commit/89567776afcc203a4ed4aac76468d7b84e5968ce) docs: performance test method
 * [0c975e343](https://github.com/kubeovn/kube-ovn/commit/0c975e343bae483882d36b4c8480337fb6c971c8) fix: wrong port porto for udp
 * [f3759b78e](https://github.com/kubeovn/kube-ovn/commit/f3759b78e0d186fac611a2637b79317b63d3c7e4) docs: add descriptions of local files
 * [b46acd6c3](https://github.com/kubeovn/kube-ovn/commit/b46acd6c38f136ee0ca3e9f53265f239480cab81) ci: add github code scan
 * [2444d51ae](https://github.com/kubeovn/kube-ovn/commit/2444d51aefb075356b7b6665c593ffdfb83f19db) fix: do not adv join cidr when enable ovn-ic
 * [292bf4caf](https://github.com/kubeovn/kube-ovn/commit/292bf4caf51e2acba1226d64504a2c16300d0cb2) perf: remove default acl rules
 * [20e82c393](https://github.com/kubeovn/kube-ovn/commit/20e82c39388c7dcefebff4e6bb3ba720b3df5fb2) prepare for next release
 * [9324491cc](https://github.com/kubeovn/kube-ovn/commit/9324491cc02ee4e7b798f565f13bf39d52969205) fix: use internal IP when node connect pod
 * [c1870c1ac](https://github.com/kubeovn/kube-ovn/commit/c1870c1acda04d567d7aa2c40fa0bd3f0bdbeadb) ci: change to docker buildx action
 * [a1976650c](https://github.com/kubeovn/kube-ovn/commit/a1976650c25566fe37bc6274cf2f7c3db95dab47) fix: delete pod when marked with deletionTimestamp
 * [c3c4f1c5b](https://github.com/kubeovn/kube-ovn/commit/c3c4f1c5b00b62e7475e5defacac14adbb3bda07) fix: remove not alive pod in pg

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

 * [498d74d7b](https://github.com/kubeovn/kube-ovn/commit/498d74d7bf79de3f233e5fd43bed07fae651ecb5) release for 1.5.2
 * [271c07bd9](https://github.com/kubeovn/kube-ovn/commit/271c07bd9e8cf4d6e8b81b8214f4b0e20a359f39) fix: nat rules can be modified
 * [21a5edbd5](https://github.com/kubeovn/kube-ovn/commit/21a5edbd59c7187023433a0e22d30215b4e6a182) fix: add resources limits to avoid eviction
 * [762f1c21f](https://github.com/kubeovn/kube-ovn/commit/762f1c21fea3c105bf966df5c599af896208ccfd) ci: specify ubuntu version to make github action happy
 * [bd4019ddc](https://github.com/kubeovn/kube-ovn/commit/bd4019ddc77a482e41f8addcff2c713ed8fa531e) fix: remove svc cidr routes
 * [93a897539](https://github.com/kubeovn/kube-ovn/commit/93a8975393e153b6176a5d102e811bcb5eec27cc) Fix the problem of confusion between old and new versions of crd
 * [031f54368](https://github.com/kubeovn/kube-ovn/commit/031f54368a1f0347e649d4c142c4f165f2715505) Fix external-address config description
 * [3371ce4c0](https://github.com/kubeovn/kube-ovn/commit/3371ce4c076ad700fe3324dad5dfd22a33f7cce7) fix: ovn-central check if it exits in NODE_IPS
 * [cf4c41279](https://github.com/kubeovn/kube-ovn/commit/cf4c41279974ae28ff6b6f7340e9b66b81a3229b) fix: check ipv6 requirement before start
 * [186d90cdb](https://github.com/kubeovn/kube-ovn/commit/186d90cdb6408eff64bdfb6731398d3b29e2ce7f) feat: add ovs/ovn log rotation
 * [b5dfc1c65](https://github.com/kubeovn/kube-ovn/commit/b5dfc1c65148a608fbc14f9fabbee06a43cc6b58) diagnose: add ovs-vsctl show to diagnose results
 * [37cbb713e](https://github.com/kubeovn/kube-ovn/commit/37cbb713ed95d3b03b30ebbf75f2c34d8a5067f6) add node ping total count metric
 * [6ed020c24](https://github.com/kubeovn/kube-ovn/commit/6ed020c246352fc2bbc3c735da458c7e14c6e441) fix: tolerate all taints
 * [1a4f48a09](https://github.com/kubeovn/kube-ovn/commit/1a4f48a09844d4c65fecf88b439d027221de041f) chore: update go version to 1.15
 * [e0fc33316](https://github.com/kubeovn/kube-ovn/commit/e0fc3331683614711429e72f6849d605f62084fc) fix: masq other nodes to local pod to avoid nodrport triangle traffic
 * [f6ff27805](https://github.com/kubeovn/kube-ovn/commit/f6ff27805e83e7394d35a8540ab01291163e6db7) Update install.sh to allow dpdk limits configuration (#546)
 * [966363860](https://github.com/kubeovn/kube-ovn/commit/9663638607a23c6aa67c5b7fbda53a127c11b6ce) prepare for 1.5.2
 * [06d8b374e](https://github.com/kubeovn/kube-ovn/commit/06d8b374eb621f77ddb25638f32168158c13b0f6) fix: cleanup script error
 * [5ddf72b24](https://github.com/kubeovn/kube-ovn/commit/5ddf72b241cc45039572289a4fdb080569ea81e1) security: remove sqlite to mute cve warning
 * [1fe42677e](https://github.com/kubeovn/kube-ovn/commit/1fe42677e55bad2f945cbcb8547f88b70ae4d630) chore: refactor log
 * [0f1b74dc9](https://github.com/kubeovn/kube-ovn/commit/0f1b74dc91ff053f760ed51878e6980e7fe26d99) fix: nat traffic that from host to svc
 * [24b97cb08](https://github.com/kubeovn/kube-ovn/commit/24b97cb08ac3eca129c2d968ff79b635b9029e84) feat: iface support regexp

### Contributors

 * Mengxin Liu
 * emmakenny
 * hzma
 * xieyanker

## v1.5.1 (2020-10-26)

 * [bf860e26e](https://github.com/kubeovn/kube-ovn/commit/bf860e26eff8f99478c28ee3c9db8eb32ba5f14d) release 1.5.1
 * [cf96d6dbd](https://github.com/kubeovn/kube-ovn/commit/cf96d6dbdb0989c748c033e99169dbc0b32e5fee) opt: only allow specifies default subnet
 * [99e393ec6](https://github.com/kubeovn/kube-ovn/commit/99e393ec64a18e17e1e26c16ce50b74484093e5f) feature: Support for namespace binding multiple subnets
 * [fa4006c07](https://github.com/kubeovn/kube-ovn/commit/fa4006c07eeef58a3c85049a565cb317211fc2cf) perf: accelerate ic and ex gw update
 * [c327535ad](https://github.com/kubeovn/kube-ovn/commit/c327535ad8bc4eb597eb2ebdefcd2c6a27f6cf17) fix: check multicast and loopback subnet
 * [d74e20788](https://github.com/kubeovn/kube-ovn/commit/d74e2078870b7b89f7879fa9eb5537fc7ff2fb4e) fix: CodeQL scan warning
 * [df8530a3c](https://github.com/kubeovn/kube-ovn/commit/df8530a3caab12a3480a9e3dd2ce636216e1de55) fix: ipt wrong order and add cluster route
 * [33afdd183](https://github.com/kubeovn/kube-ovn/commit/33afdd183004aa3d8436acbc97dbdacc735b6aed) fix: add default ssl var for compatibility
 * [f14155e46](https://github.com/kubeovn/kube-ovn/commit/f14155e460dbeaf4423aafcc1d2305aa8f9c4c23) fix: broken rpm link
 * [a99ecbeea](https://github.com/kubeovn/kube-ovn/commit/a99ecbeea0c85239db2a6f8d3aeefe62b8f4f139) fix: ipv6 network format when update subnet
 * [5fbb92b0f](https://github.com/kubeovn/kube-ovn/commit/5fbb92b0f6257a62285d45922d456b77e7ac8de6) fix: ipv6 len mismatch
 * [bbda6a806](https://github.com/kubeovn/kube-ovn/commit/bbda6a8069bd1862337b62069f07a6291ef67778) fix: wrong port porto for udp
 * [42b7aa121](https://github.com/kubeovn/kube-ovn/commit/42b7aa121f495c266dfd68e3b6946ef5cf975c20) fix: do not adv join cidr when enable ovn-ic
 * [34952c809](https://github.com/kubeovn/kube-ovn/commit/34952c80970882269a81d6a0e787cbd0d268cba8) perf: remove default acl rules
 * [2ad711079](https://github.com/kubeovn/kube-ovn/commit/2ad7110793deea0f9e3c1fdb4b9f176f02b4d7d7) fix: use internal IP when node connect pod
 * [c42d42f1d](https://github.com/kubeovn/kube-ovn/commit/c42d42f1d7366401ded307eb0ff3779c9752b9ca) ci: change to docker buildx action
 * [ba4010651](https://github.com/kubeovn/kube-ovn/commit/ba40106519c2ac36db71f336fb5fcdf594ffe49c) fix: delete pod when marked with deletionTimestamp
 * [f8a4e6565](https://github.com/kubeovn/kube-ovn/commit/f8a4e6565a758a0c6c5c2dc9f14781d70b2f94a9) fix: remove not alive pod in pg

### Contributors

 * Mengxin Liu
 * 范日明

## v1.5.0 (2020-09-28)

 * [c0a34b842](https://github.com/kubeovn/kube-ovn/commit/c0a34b842eb4cf13121fec2f9f69c579019d6b84) release: prepare for release 1.5.0
 * [955484579](https://github.com/kubeovn/kube-ovn/commit/955484579832c8386a69baf3e82d231b8de7614d) perf: use podLister to optimize k8s calls
 * [6635f9302](https://github.com/kubeovn/kube-ovn/commit/6635f9302c33824dc126592e722c7cc4a6a08bb0) chore: enable ssl to default ci tests
 * [5f29fc307](https://github.com/kubeovn/kube-ovn/commit/5f29fc307c78c30d22a248972b072a562a50906e) security: change ovsdb file access to 600
 * [0e0a6887a](https://github.com/kubeovn/kube-ovn/commit/0e0a6887ac4c0e550aa7830b86070f18bb7bd0f8) docs: improve hw-offload
 * [a1a215dcc](https://github.com/kubeovn/kube-ovn/commit/a1a215dcc72314202879833d4ab68953ea23d04b) feat: support db ssl communication
 * [e7a88c113](https://github.com/kubeovn/kube-ovn/commit/e7a88c1133857eb1d8d3d2f100dd8a9b8f9059fb) diagnose: show nb/sb/node info
 * [090624fd9](https://github.com/kubeovn/kube-ovn/commit/090624fd9e00e1a2798fc3ac5671925f8dac82ee) fix: pinger diagnose should use cmd args
 * [fae393e3b](https://github.com/kubeovn/kube-ovn/commit/fae393e3b491153a5e49f23eea8168e3982bab51) fix: ipv6 get portmap failed again
 * [b74189fee](https://github.com/kubeovn/kube-ovn/commit/b74189feea76dc7acac069e6d0e85699666410e1) fix: ipv6 get portmap failed
 * [f1c2f9952](https://github.com/kubeovn/kube-ovn/commit/f1c2f99528f264941b50d97de0c574db2ba3aa67) fix: delay mv cni conf to when cniserver is ready
 * [98bb7510c](https://github.com/kubeovn/kube-ovn/commit/98bb7510cd0c6aef5d02aa7fe7524bf5786b65fe) chore: update kind and kube-ovn-cni updateStrategy
 * [646404216](https://github.com/kubeovn/kube-ovn/commit/646404216812dfff8461a2e6e9c019ed68f86a51) monitor: add cni grafana dashboard
 * [38adc18f1](https://github.com/kubeovn/kube-ovn/commit/38adc18f18cb8744b381fc67af62d297e80daa2c) monitor: add more kube-ovn-cni metrics
 * [36e9091d1](https://github.com/kubeovn/kube-ovn/commit/36e9091d10ff3c6799629fbb871ed9a69a915058) feat: update pinger dashboard
 * [ab736d8f5](https://github.com/kubeovn/kube-ovn/commit/ab736d8f523967ad31a43b5b0f7e566728e20c7d) fix: issues with vlan underlay gateway
 * [2e5f0ecbe](https://github.com/kubeovn/kube-ovn/commit/2e5f0ecbe48bcbe6c8fa98d550d2f45ab27592f3) feat: set more metadata to interface external_ids
 * [77c4a5f29](https://github.com/kubeovn/kube-ovn/commit/77c4a5f2932c66d304d2f87332838df7d8953d63) feat: grace stop ovn-controller
 * [ebfc1530d](https://github.com/kubeovn/kube-ovn/commit/ebfc1530dfaeab15127ffb0f234c9756a27cd294) refactor: fix bridge-mappings and refactor vlan code
 * [729ed3c7a](https://github.com/kubeovn/kube-ovn/commit/729ed3c7a502460aeadfb0cd9785832ad44f797c) fix: allow mirror config update
 * [84bb3c838](https://github.com/kubeovn/kube-ovn/commit/84bb3c8380c28f3b323dca5b0bbc91e8a77ec66e) fix: cleanup v6 iptables and ipset
 * [da4937172](https://github.com/kubeovn/kube-ovn/commit/da4937172279e42eda90eb4b8109eec263ba869a) docs: add gateway docs and optimize others
 * [ece4219f7](https://github.com/kubeovn/kube-ovn/commit/ece4219f7bc54da03f9678095335b5a8b0a3accb) feat: integrate ovn sfc
 * [2b2e7a9a7](https://github.com/kubeovn/kube-ovn/commit/2b2e7a9a788ee7cc11d44f7b6f821aec6f9086dc) feat: support pod snat
 * [7a60b569c](https://github.com/kubeovn/kube-ovn/commit/7a60b569c9e2f431eb98c948061cb35e796bfd87) prepare for next release
 * [e9933619e](https://github.com/kubeovn/kube-ovn/commit/e9933619e913f4bcd2136168faf2d78f9b007629) fix: ovn-ic-db restart failed
 * [115c12668](https://github.com/kubeovn/kube-ovn/commit/115c126684972af06f2eb0019bc25031004045f3) fix: stop ovn-ic when disabled
 * [e98614444](https://github.com/kubeovn/kube-ovn/commit/e98614444005451184e260e96ef0032ee8f9ae98) fix: use nodeName as chassis hostname

### Contributors

 * Mengxin Liu

## v1.4.0 (2020-09-01)

 * [0f973a5ad](https://github.com/kubeovn/kube-ovn/commit/0f973a5ad897f2c6b70eee404772852d911bcce1) prepare for 1.4 release
 * [78ab9b1e2](https://github.com/kubeovn/kube-ovn/commit/78ab9b1e2d21baa0c765529a2aebc048017af04b) fix: do not gc learned routes
 * [3ddb9614d](https://github.com/kubeovn/kube-ovn/commit/3ddb9614d9e1fee9b08f1eb5de612fc23750336c) chore: add psp
 * [f847e5be6](https://github.com/kubeovn/kube-ovn/commit/f847e5be666dfe7a8ff790013f539686aac56b9b) perf: apply udp improvement
 * [a8f0d2285](https://github.com/kubeovn/kube-ovn/commit/a8f0d2285e44ff0c035cc149e0ead9c6b2c4d08e) chore: sync pre-1.16 install.sh
 * [0918e9a20](https://github.com/kubeovn/kube-ovn/commit/0918e9a2073c16fb78908b5aed79ae9116e768db) ci: use go 1.15
 * [f43a10272](https://github.com/kubeovn/kube-ovn/commit/f43a102729c28cc97cc9f6e900598f05f90e782f) fix: add prob timeout to wait script finish
 * [c5ca0b1b7](https://github.com/kubeovn/kube-ovn/commit/c5ca0b1b70c1d56cf21daec590633210913bdc32) resolve review problem
 * [28d5a8aab](https://github.com/kubeovn/kube-ovn/commit/28d5a8aab4a75e983afb7d80bba026efa99524d0) chore: suppress verbose logs
 * [df54b0d13](https://github.com/kubeovn/kube-ovn/commit/df54b0d130394dc3be8a02932db9db170bde91b7) fix: do not gc ic logical_switch
 * [b9ab4d661](https://github.com/kubeovn/kube-ovn/commit/b9ab4d661f48ded7a9f5bc8bfb93e86ec690f8c1) fix: only gc VIF type logical_switch_port
 * [731fef992](https://github.com/kubeovn/kube-ovn/commit/731fef9924cb97ec618e266bb12078ccb1da38ec) docs: update docs
 * [e9ae40a98](https://github.com/kubeovn/kube-ovn/commit/e9ae40a98c0fa6214db4c1e7741d10c99d8f6884) chore: add back lflow reduction optimization
 * [022c79037](https://github.com/kubeovn/kube-ovn/commit/022c79037ba0adaf880b8ebf560d115885db7036) chore: update ovs to 2.14.0
 * [8e93c054e](https://github.com/kubeovn/kube-ovn/commit/8e93c054e252ce89dd0bc5480b64f26a1f248f22) fix: remove duplicated gcLogicalSwitch
 * [c3b7457a1](https://github.com/kubeovn/kube-ovn/commit/c3b7457a12728108e399705287c81df53ae577f7) fix: modify src-ip route priority
 * [e0096f9b9](https://github.com/kubeovn/kube-ovn/commit/e0096f9b9fc7c269c2da0d2d31fc0522c46da10f) fix: missing session lb to logical switch
 * [6fbcc198a](https://github.com/kubeovn/kube-ovn/commit/6fbcc198a1e7360955d8a15f839f406c8574bb32) feat: ovn-ic integration
 * [0ea62c163](https://github.com/kubeovn/kube-ovn/commit/0ea62c16375a890f4d6ea94bd1bf2f761b6a020c) fix:resolve gosec check problem
 * [b2d0393b2](https://github.com/kubeovn/kube-ovn/commit/b2d0393b2eb140ecbd0e38a6a96a0b7c79e5dbe4) feat: do not perform masq on external traffic
 * [4e1ad1260](https://github.com/kubeovn/kube-ovn/commit/4e1ad1260d1fa40542d2a23ba8f19f5e649aacfe) chore: fix patch failure
 * [a7c460a48](https://github.com/kubeovn/kube-ovn/commit/a7c460a48efe275bf82f6fac2f86f402f0c551c2) fix: subnet acl might conflict if allowSubnets and subnet cidr cover each other
 * [0dd85e469](https://github.com/kubeovn/kube-ovn/commit/0dd85e469e62c56ea67cc6d3c2b4bb83af67def6) feat: acl log drop packets
 * [6d048632f](https://github.com/kubeovn/kube-ovn/commit/6d048632fdbbea7c4c1258b80c46b7f021b14e66) chore: remove juju log dependency
 * [9535c26ba](https://github.com/kubeovn/kube-ovn/commit/9535c26ba2d65ad2c92e63a32685b6c9d7a9bf93) feat: gw switch from overlay to underlay
 * [4b0955808](https://github.com/kubeovn/kube-ovn/commit/4b095580830573e4c09d0debc6c480aeff1ced29) chore: prepare for 1.4 release
 * [c9d07e1d5](https://github.com/kubeovn/kube-ovn/commit/c9d07e1d5c3b23584993900807a862d11ffbf038) fix: prevent update failed logs
 * [a98ec5bd1](https://github.com/kubeovn/kube-ovn/commit/a98ec5bd19393587991853dde0ff6ac191613eb4) fix: ko use external-ids to find related nic
 * [1cad39cef](https://github.com/kubeovn/kube-ovn/commit/1cad39cef27c303737c122d406ecb0fbd994d68f) fix: forward accept rules

### Contributors

 * Mengxin Liu
 * hzma

## v1.3.0 (2020-07-31)

 * [45d30713d](https://github.com/kubeovn/kube-ovn/commit/45d30713d07ffff760127cf70a806bf5fc4446d9) chore: add build date
 * [c99532349](https://github.com/kubeovn/kube-ovn/commit/c995323497a1c9b0369e1b9164c9a83c5fa0a1c2) release: update 1.3.0 docs
 * [34627a669](https://github.com/kubeovn/kube-ovn/commit/34627a66953fceca9e905a53f1b642ba3a6ce687) fix: call appendMssRule function to resolved mss according problem
 * [bb961ae5c](https://github.com/kubeovn/kube-ovn/commit/bb961ae5c63c07cc597a54c3bcdbb80bd2b4c002) dpdk: add kmod, pdump and proc-info tools
 * [cf47ee1b9](https://github.com/kubeovn/kube-ovn/commit/cf47ee1b91db83551ae50fc3bf7a1d61dfb88236) fix: ci image tags
 * [467681796](https://github.com/kubeovn/kube-ovn/commit/46768179604d756186057d20475f637a239e51c8) chore: optimize dpdk build
 * [5c1076875](https://github.com/kubeovn/kube-ovn/commit/5c1076875ef7d8d330eb08311eeb1c8df34cedc5) docs: add hw-offload docs and resolve some issues
 * [e64c61327](https://github.com/kubeovn/kube-ovn/commit/e64c61327d12b66e7c0ea0aa20949b288dcfbec1) fix: if sriov device, do not delete the host nic
 * [f55c3fba2](https://github.com/kubeovn/kube-ovn/commit/f55c3fba21ba0f1f513ae834497eed76d623a17f) fix: use keymutex to serialize pod add/delete operation
 * [d438574d4](https://github.com/kubeovn/kube-ovn/commit/d438574d43fd8c0d108d54fed16a94bcb0b0f8c3) feat: assign a pod as the gw
 * [1806a5722](https://github.com/kubeovn/kube-ovn/commit/1806a57226d87152fead314f3df596510e3a8db9) ci: add arm build to normal ci process
 * [5aed1ef1c](https://github.com/kubeovn/kube-ovn/commit/5aed1ef1ca6a28ebc68fc7ca994b0a02a7f3319d) ci: add unfixed cve
 * [19201a369](https://github.com/kubeovn/kube-ovn/commit/19201a36999041309f4eea2cf12adda266d07da4) ci: arm64 build accelerate
 * [63fbc0086](https://github.com/kubeovn/kube-ovn/commit/63fbc00869c02ef62a1d861a9f297687bcd7937d) chore: add logs to sriov interface
 * [82140c931](https://github.com/kubeovn/kube-ovn/commit/82140c9313be9e50daf94f8c299674249007c4a1) ci: add ipv6 install e2e
 * [c3814c725](https://github.com/kubeovn/kube-ovn/commit/c3814c725f783aecf869f81f560fe5b463e0a86d) feat: recycle lsp at runtime
 * [3f9d7c928](https://github.com/kubeovn/kube-ovn/commit/3f9d7c928dfcfbebaff28409849bcac0e18bdeff) fix: qos error
 * [e460541dd](https://github.com/kubeovn/kube-ovn/commit/e460541dd3fdc938d43b17b2c71e81e5a7d03b8c) fix: variable error
 * [de9493f2f](https://github.com/kubeovn/kube-ovn/commit/de9493f2f03e7a365120a7e3a2285bff48f2753d) ci: modify cache usage
 * [1994e5c30](https://github.com/kubeovn/kube-ovn/commit/1994e5c30b1609a891b2f3222e25039e9dd41378) ci: save ci time
 * [5c4d5a3cf](https://github.com/kubeovn/kube-ovn/commit/5c4d5a3cfa64479c154ab3379f3f6dc3175ac546) chore: use j2 to render different kind.yaml
 * [d1a184ef9](https://github.com/kubeovn/kube-ovn/commit/d1a184ef9901055c79b0ba97dce2dadda4ba20aa) fix: set qlen for ovn0
 * [a2d969e8f](https://github.com/kubeovn/kube-ovn/commit/a2d969e8f99f406ca1651142cd9d8a6633e0773d) prepare for 1.3 release
 * [3a018a86e](https://github.com/kubeovn/kube-ovn/commit/3a018a86e9dde7308f76a1fb30902184a0150cb9) chore: update build.sh
 * [be7c68f23](https://github.com/kubeovn/kube-ovn/commit/be7c68f23fbdb18e166479608eb2b4d663bce8c3) fix: log error
 * [31723f669](https://github.com/kubeovn/kube-ovn/commit/31723f669e54f662d9d56c9833c7386a05fe768f) chore: check ovn-sb connectivity from ovn-ovs probe
 * [d017f1f2d](https://github.com/kubeovn/kube-ovn/commit/d017f1f2d0e6a0a0eef96e8ddcfa8b907a7007f1) fix: available ips calculation issues
 * [309c80800](https://github.com/kubeovn/kube-ovn/commit/309c808005ae3d38923569c9e9d0b39c0a082a66) perf: add hw offload
 * [4b8faede4](https://github.com/kubeovn/kube-ovn/commit/4b8faede48168fb21ac39cbb3084a40bd7a3777f) docs: add gateway qos doc
 * [32a9af2bd](https://github.com/kubeovn/kube-ovn/commit/32a9af2bde14322acd4a77661566f75e1aebf1a4) ci: remove master taint
 * [3865220df](https://github.com/kubeovn/kube-ovn/commit/3865220df86b2af451642037f9a12c1ad618bf28) chore: update cni dependencies
 * [8e032392d](https://github.com/kubeovn/kube-ovn/commit/8e032392d62d5a8c4b9545f66ec78ba214cbb9db) feat: session service
 * [34b7cba75](https://github.com/kubeovn/kube-ovn/commit/34b7cba751209975063c155fac2ecbf919348a6e) Revert "perf: use policy-route to replace src-ip route"
 * [1d13d5c38](https://github.com/kubeovn/kube-ovn/commit/1d13d5c38ab51d4c5829a3a76ce7eaa0552d8ed6) Revert "fix: ipv6 policy route"
 * [658136408](https://github.com/kubeovn/kube-ovn/commit/658136408bc58902645b17ac7b8d56ff07a0c09a) Revert "fix: reset address_set when delete subnet"
 * [e6817a651](https://github.com/kubeovn/kube-ovn/commit/e6817a65163ede10b76e5229896af0c6fafe5e1a) fix: reset address_set when delete subnet
 * [dbc968ca5](https://github.com/kubeovn/kube-ovn/commit/dbc968ca57823a5b939f6ae133581bb2c8a7f7f4) test: statefulset without ippool
 * [9440a11ff](https://github.com/kubeovn/kube-ovn/commit/9440a11ff09ab344224a0e7c0a97ceaa9c83cdbb) match apps/* statefulset
 * [ca122027b](https://github.com/kubeovn/kube-ovn/commit/ca122027b0dcf94b736306a982fc48bd2cec9981) fix: ipv6 policy route
 * [54acd0c35](https://github.com/kubeovn/kube-ovn/commit/54acd0c3576b7a09b97d6ffb34b2ed4da582260a) feat: support gw qos
 * [b8f032487](https://github.com/kubeovn/kube-ovn/commit/b8f032487624e5a189a7e0675a66640316a12673) perf: use policy-route to replace src-ip route
 * [83dc420e4](https://github.com/kubeovn/kube-ovn/commit/83dc420e4f8c21a1014de6a98bc7feb6ce0c0f2e) Solve the problem of non-standard statefulset creation mode
 * [32e6d5728](https://github.com/kubeovn/kube-ovn/commit/32e6d57282dd679b685d75ba0695244fe25bcf76) fix: arm64 build missing env
 * [c93f0d848](https://github.com/kubeovn/kube-ovn/commit/c93f0d848d99f20d0ecdee178521f545b45cfcef) action: use commit as image tag
 * [732b240c0](https://github.com/kubeovn/kube-ovn/commit/732b240c07a1134774e27606b1d40192587eddc8) Add libatomic to docker image
 * [9d5294bb9](https://github.com/kubeovn/kube-ovn/commit/9d5294bb96f4a0d19dcc742e9412e7f8631c3c9a) chore: save disk space when building
 * [4b1f5244c](https://github.com/kubeovn/kube-ovn/commit/4b1f5244c3ce141a0c74f15893b7615bec996777) chore: change crd form v1beta1 to v1
 * [e6fb0fcb9](https://github.com/kubeovn/kube-ovn/commit/e6fb0fcb9947c36d36d4bae1e3e64bca3add3c9c) kubectl-ko: add ovs-tracing info
 * [61aa3ba2b](https://github.com/kubeovn/kube-ovn/commit/61aa3ba2b47071de74ce10bd42eff43eb22520eb) pinger: add metrics to resolve external address
 * [ef0f3b279](https://github.com/kubeovn/kube-ovn/commit/ef0f3b279edcc62c520144b9d79f7138d27666d8) chore: update ovn to 20.06
 * [961f5f1ab](https://github.com/kubeovn/kube-ovn/commit/961f5f1ab42de8f40c28640565dbb894d53ef79b) update changelog
 * [85f2e0e0f](https://github.com/kubeovn/kube-ovn/commit/85f2e0e0f85e8b9c88730c5d38eada3f91c19ab0) fix some version in docs
 * [f989bdd8f](https://github.com/kubeovn/kube-ovn/commit/f989bdd8f449ec65381fd7fdfee7ecf95d3195a8) fix: rename variable
 * [990bf9839](https://github.com/kubeovn/kube-ovn/commit/990bf9839cc061a1adab0978e9d9e32222ade2ae) fix: minor fix
 * [8d7045b3a](https://github.com/kubeovn/kube-ovn/commit/8d7045b3a11c34cad82382b95d273dd6af1a869f) feat: use never used address first to reduce conflict
 * [db2516c2d](https://github.com/kubeovn/kube-ovn/commit/db2516c2d899803518880d7036aff4154619b2e9) ci: use tmpfs to accelerate e2e
 * [792723761](https://github.com/kubeovn/kube-ovn/commit/792723761535fb01cf20391af0b0d9f61a26da67) fix: create/delete order might lead ip conflict
 * [b27d75455](https://github.com/kubeovn/kube-ovn/commit/b27d754551ab336961f951540a905de4e4009a54) ci: do not push image when pr
 * [a1f53e67d](https://github.com/kubeovn/kube-ovn/commit/a1f53e67d76da37322dac333cf015e0bf07ac615) clean up all white noise
 * [a4f40370d](https://github.com/kubeovn/kube-ovn/commit/a4f40370d19c0b2712a8d9fefda388e4b6eb1a84) security: update yum repo
 * [270c825cd](https://github.com/kubeovn/kube-ovn/commit/270c825cd599c7ffa52632c7f897744d3722a860) fix node's annottaions overwrited incorrectly
 * [5adc5a440](https://github.com/kubeovn/kube-ovn/commit/5adc5a440a06aec9715c3e1f232b84f4332e8013) Fix typo in multi-nic.md
 * [3ac92a157](https://github.com/kubeovn/kube-ovn/commit/3ac92a157d2bb3015963a9e3304e002cdc184d98) Userspace-CNI updates in dpdk.md
 * [76e72b7ea](https://github.com/kubeovn/kube-ovn/commit/76e72b7ea09dab0a6299d9510a50e2b527ca92a2) Remove empty lines from DPDK Dockerfile
 * [9b5c018ac](https://github.com/kubeovn/kube-ovn/commit/9b5c018aca9e7968b6e98d595eca1dfe13ed9f5a) security: update loopback to fix CVE
 * [bd1f2acf7](https://github.com/kubeovn/kube-ovn/commit/bd1f2acf7c511af1cf670f6119cf7c4ea735368c) Make OVS-DPDK start script more robust
 * [3bfc39f82](https://github.com/kubeovn/kube-ovn/commit/3bfc39f82050e16e37a20a843185ca800fa7940e) Reduce DPDK image size
 * [4917afe98](https://github.com/kubeovn/kube-ovn/commit/4917afe98ed89302c1481d25fb1622d79acf54f1) fix: add back privilege for ipv6
 * [8121afd60](https://github.com/kubeovn/kube-ovn/commit/8121afd6037a3414b2ca1a0ff8f3f004d47695a7) Config support for OVS-DPDK
 * [ad30e6870](https://github.com/kubeovn/kube-ovn/commit/ad30e6870a2edc35fd6f624d317fcd5bbbeb2ed3) security: add trivy scan and fix image CVEs
 * [06256a09b](https://github.com/kubeovn/kube-ovn/commit/06256a09bce362754b680fa8dc1eba6a93e65830) docs: modify arm build
 * [9d2e64a40](https://github.com/kubeovn/kube-ovn/commit/9d2e64a40bab54394e60f90a2f3e8bee38dd35f2) docs: update development
 * [bd9757681](https://github.com/kubeovn/kube-ovn/commit/bd975768146fc27dc7ffcdecb2de2ceef9507482) refactor: use ovs.Exec replace raw command
 * [32024ba88](https://github.com/kubeovn/kube-ovn/commit/32024ba88963a56593cd112717ea433abf6cfc30) chore: add gosec to audit code security
 * [1db9046d6](https://github.com/kubeovn/kube-ovn/commit/1db9046d6803245d5ff3a5cbdbaff6e0a7c6794f) prepare for next release
 * [aa72ba6ce](https://github.com/kubeovn/kube-ovn/commit/aa72ba6ce070ff7406fec3c0d0bccebdd08508ca) fix: arm build
 * [628f5c5e1](https://github.com/kubeovn/kube-ovn/commit/628f5c5e154e0070f9f6de7dccfb3ae9d3f3a796) fix: change version in install.sh

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

 * [755f57bc5](https://github.com/kubeovn/kube-ovn/commit/755f57bc55977bcfa547f1a27bd816dab0b423fe) release 1.2.1
 * [88b847ca7](https://github.com/kubeovn/kube-ovn/commit/88b847ca7b860257eef8f118bbdfa37b7d9c5c48) fix: create/delete order might lead ip conflict
 * [0656f63c3](https://github.com/kubeovn/kube-ovn/commit/0656f63c3c52c401a9944f470c2c357059c45b3b) fix node's annottaions overwrited incorrectly
 * [86e20a097](https://github.com/kubeovn/kube-ovn/commit/86e20a097f55acc3ab4d0f7de8ac6e9f9bbd1138) security: update loopback to fix CVE
 * [b1ea8a364](https://github.com/kubeovn/kube-ovn/commit/b1ea8a3642830837cc6f1b0e08cc9b9710210cc5) fix: add back privilege for ipv6
 * [2a8775305](https://github.com/kubeovn/kube-ovn/commit/2a8775305d9d303197af40265cecc059ead1a027) fix: arm build
 * [8ec2c159a](https://github.com/kubeovn/kube-ovn/commit/8ec2c159a34b52381c432aa96e98f30af50b293c) fix: change version in install.sh

### Contributors

 * Mengxin Liu
 * MengxinLiu
 * ckji

## v1.2.0 (2020-05-30)

 * [280a1bd39](https://github.com/kubeovn/kube-ovn/commit/280a1bd39ea045ece2e8ef05259a023846873076) chore: prepare for release 1.2
 * [4342187d8](https://github.com/kubeovn/kube-ovn/commit/4342187d8f72ff18511cbfe7ebb3f70cf357a064) chore: prepare for release 1.2
 * [4a52bb43d](https://github.com/kubeovn/kube-ovn/commit/4a52bb43df3eb9fbbd1f56aa43f02085c87363b9) DPDK doc update and small image reduction
 * [b055cc68a](https://github.com/kubeovn/kube-ovn/commit/b055cc68af72111431130376ada2494102eb70fe) Add OVS-DPDK support, for issue 104
 * [f7fdd2dca](https://github.com/kubeovn/kube-ovn/commit/f7fdd2dcae463837bc98f51b08cb682372d1762e) fix: pod get deleted between configure nb and patch pod
 * [e13dc5ac0](https://github.com/kubeovn/kube-ovn/commit/e13dc5ac0ec194f90302f31a09c37492de4beab4) fix: native vlan and delete subnet issues
 * [44b5a6a7c](https://github.com/kubeovn/kube-ovn/commit/44b5a6a7cf53646b14f02524e3060238e0c0fce6) fix: trigger github action when dist dir change
 * [3a2ee051c](https://github.com/kubeovn/kube-ovn/commit/3a2ee051cba6d1f55f81abc2e37e6167ad35fedf) fix: update ovn patch
 * [6e1589cc5](https://github.com/kubeovn/kube-ovn/commit/6e1589cc50c009b07293bb83f726483586ac59ca) chore: improve log
 * [00f984890](https://github.com/kubeovn/kube-ovn/commit/00f9848902d14c4c1fec406001fd3c115f2bb300) fix: gc lsp for pod that not alive
 * [701e9efdc](https://github.com/kubeovn/kube-ovn/commit/701e9efdc3cde047b7336511c1a5f61538c13681) feat: support underlay without vlan encap
 * [83ad499fd](https://github.com/kubeovn/kube-ovn/commit/83ad499fdda2f32b7a1b6d0774ceddedf91aa5ac) chore: optimize kube-ovn-cni log
 * [84b6cdcf6](https://github.com/kubeovn/kube-ovn/commit/84b6cdcf64e7bd39aede73f87fc0d070869f6dfb) fix: gc node lsp
 * [7aafd9447](https://github.com/kubeovn/kube-ovn/commit/7aafd94477588c8d4fc022a7d0d6ce8676e8bd87) chore: remove vagrant
 * [92ccf729f](https://github.com/kubeovn/kube-ovn/commit/92ccf729f46adaee18ab00102aef496cfe2a6999) fix: dst route policy might be empty
 * [6c89a0467](https://github.com/kubeovn/kube-ovn/commit/6c89a046755837dc93e2c73cc258efc4a675ba4e) feat: in vlan mode if physical gateway exists, no need to create a virtual one
 * [1d5c6958a](https://github.com/kubeovn/kube-ovn/commit/1d5c6958aa83e83f4ad4bfa22ed8a58be69900c8) perf: add amd64 compile flags back
 * [b0f0947d0](https://github.com/kubeovn/kube-ovn/commit/b0f0947d010b81f486148c293cd22e323e728922) fix: init ipam before gc, other wise routes will be deleted
 * [dbc23c5e9](https://github.com/kubeovn/kube-ovn/commit/dbc23c5e99057a547cdd216487771095c14cf60a) fix: patch ovn to lower src-ip route priority to work with ovn-ic
 * [5a7638200](https://github.com/kubeovn/kube-ovn/commit/5a7638200612b179414342252cf1d0fd27de78ec) fix: return early if allocation is not ready
 * [b03c37684](https://github.com/kubeovn/kube-ovn/commit/b03c37684ded0dd89df7f3ea84b985aa09459a12) chore: remove networks crd
 * [2853438c4](https://github.com/kubeovn/kube-ovn/commit/2853438c4ad3632082ad9cb2d8b40d9325fb20a1) perf: remove more stale lflow
 * [0665f2e8f](https://github.com/kubeovn/kube-ovn/commit/0665f2e8fee7ff6b5a5f5d68731b7d15d967c723) ci: run ut and e2e in github action
 * [e71b68c01](https://github.com/kubeovn/kube-ovn/commit/e71b68c01e6045aa291fc60d398c531d44895b43) fix: check svc and endpoint protocol
 * [508eb7a28](https://github.com/kubeovn/kube-ovn/commit/508eb7a28ff3138010ac73808a95c3ca427f9cdb) perf: reduce lflow count
 * [5f8b9b406](https://github.com/kubeovn/kube-ovn/commit/5f8b9b406427d90fb5a3aca097839172aa7c57b1) fix: when podName or namespace contains dot, lsp cannot be deleted correctly
 * [27c72560c](https://github.com/kubeovn/kube-ovn/commit/27c72560c6caa58b1a125d020c7af06c19584c73) fix: wrong subnet status
 * [f0b17a69b](https://github.com/kubeovn/kube-ovn/commit/f0b17a69b0f1b0945e015232713170c01e75f56b) feat: change pod route when update gateway type
 * [13283daf3](https://github.com/kubeovn/kube-ovn/commit/13283daf39c4f1a7231d88f2e5d5e5c0ea72adf1) feat: refactor subnet and allow cidr change
 * [23821d6cb](https://github.com/kubeovn/kube-ovn/commit/23821d6cb256ea8a9adaf71d5af389d12e3b8207) fix: use kubectl to avoid tls handshake error
 * [e647cc6c0](https://github.com/kubeovn/kube-ovn/commit/e647cc6c05960b77a11781a2de98a51303a89ccd) chore: reduce logs
 * [aef4336d1](https://github.com/kubeovn/kube-ovn/commit/aef4336d1c13e67422508f5a504ac3016fb51122) feat: only show error log of kube-ovn-controller
 * [a9ab0bc25](https://github.com/kubeovn/kube-ovn/commit/a9ab0bc2531ca472c9c3f3d0608cd138db9f8bd2) fix: map concurrent panic
 * [2dd13b232](https://github.com/kubeovn/kube-ovn/commit/2dd13b2328a056cf343914f635da56cffb56057b) fix: ipv6 related issues
 * [86c443e71](https://github.com/kubeovn/kube-ovn/commit/86c443e71d72e624518ca71d285e115979cf626a) fix: validate if subnet cidr conflicts with svc ip
 * [eb4cb1b3a](https://github.com/kubeovn/kube-ovn/commit/eb4cb1b3a2ef58f566002b982a656f2bab00295d) fix: validate if node address conflict with subnet cidr
 * [7f595ee03](https://github.com/kubeovn/kube-ovn/commit/7f595ee030c6cc2e4a6099d47f592659c0a66eae) feat: github action
 * [1046b5727](https://github.com/kubeovn/kube-ovn/commit/1046b5727767a804f2001b9342cabf5211c4e696) fix: wait node annotations ready before handle pods
 * [7a0151cc2](https://github.com/kubeovn/kube-ovn/commit/7a0151cc21919d1b89b076c7262169418386d1c7) fix: check ovn-nbctl socket in new dir
 * [0dc76768b](https://github.com/kubeovn/kube-ovn/commit/0dc76768bfba4105ecece940e29a00142b02721a) fix: error log found in scale test
 * [047159438](https://github.com/kubeovn/kube-ovn/commit/047159438c7c500b54dc5e5aac2f6c8375605acb) fix: concurrent panic
 * [da14eaebb](https://github.com/kubeovn/kube-ovn/commit/da14eaebbe4950a369db0200e9531f20d79e0024) feat: use bgp to announce pod ip
 * [909b5a00d](https://github.com/kubeovn/kube-ovn/commit/909b5a00da781f56123f77add7016afc56a3a8be) release 1.1.1
 * [ab834b5a1](https://github.com/kubeovn/kube-ovn/commit/ab834b5a1d817a0dd9f7cd929c5ab6c74d32db6b) fix: labels might be nil
 * [0c0824db9](https://github.com/kubeovn/kube-ovn/commit/0c0824db916f5e36e1437fd4acdb347476a2f86a) fix: ping output format
 * [ce27fb31c](https://github.com/kubeovn/kube-ovn/commit/ce27fb31c44ce78f78fdcded6020fc1576ff561f) monitor: make graph more sensitive to changes
 * [9b05fccf2](https://github.com/kubeovn/kube-ovn/commit/9b05fccf2942a453ff818beb6e7e1a5bee72e074) docs: update vlan docs
 * [d0544d896](https://github.com/kubeovn/kube-ovn/commit/d0544d8969f7ece8fa352f5c81a78ba03b3fbd9b) docs: update docs
 * [28aef840e](https://github.com/kubeovn/kube-ovn/commit/28aef840e09a9bbf35883eb621c72f55f438691d) feat: improve install/uninstall
 * [8d8536564](https://github.com/kubeovn/kube-ovn/commit/8d853656496dd94d54f5fc681e2c8b18c5c468ef) refactor: refactor cni-server
 * [d99ffff0e](https://github.com/kubeovn/kube-ovn/commit/d99ffff0e39dfe71bf22e5de606bb42caa2f8813) refactor: controller refactor
 * [8f1f0135d](https://github.com/kubeovn/kube-ovn/commit/8f1f0135dd7188359502db41bcb801b86c051293) feat: modify install.sh for vlan type network
 * [cfe9d2761](https://github.com/kubeovn/kube-ovn/commit/cfe9d27614859cf73ea442a4c28d7d88b3e1c213) feat(vlan): vlan network type
 * [edd0ea81e](https://github.com/kubeovn/kube-ovn/commit/edd0ea81e935c9d5a76f1a7fb1c6871ea1a49511) feat(vlan): vlan network type
 * [c63accf42](https://github.com/kubeovn/kube-ovn/commit/c63accf42e109537b7e26d5b0dbadcb34e701349) fix: yaml indent and ovn central dir
 * [5bc84d7b4](https://github.com/kubeovn/kube-ovn/commit/5bc84d7b4a5b1f825f6b27d6c50e42879846b0c3) docs: chinese wechat info
 * [feaec4ddd](https://github.com/kubeovn/kube-ovn/commit/feaec4ddd63d816a22f5b25cc6bf568b2183fd76) fix: fork go-ping and apply patches
 * [58f73b33f](https://github.com/kubeovn/kube-ovn/commit/58f73b33f4062e25a730ad2570ac328b2291b2d5) chore: update kind node to 1.18 and ginkgo
 * [d274a979a](https://github.com/kubeovn/kube-ovn/commit/d274a979a786847f8eda80e8573083a26da1ef86) docs: add arm build steps
 * [d061fc3ca](https://github.com/kubeovn/kube-ovn/commit/d061fc3ca98e7a8f795c04648782b36c7881478e) fix: mount etc/origin/ovn to ovs-ovn
 * [f8d6fd5c1](https://github.com/kubeovn/kube-ovn/commit/f8d6fd5c1a922c178a2e43a42d6a211ba7865cd4) add support for multi-arch build
 * [953f5be7b](https://github.com/kubeovn/kube-ovn/commit/953f5be7b345a805e6080516fc5ba769a7251299) docs: change the cidr to avoid misunderstanding
 * [5c5b9e08f](https://github.com/kubeovn/kube-ovn/commit/5c5b9e08f4733c931e1eb82c433d67c87038987f) feat: diagnose check if dns/kubernetes svc exist
 * [7c6d6784f](https://github.com/kubeovn/kube-ovn/commit/7c6d6784f417dd5b9bf71376b30be95d3b5d9c98) OVS local interface table mac_in_use row is lower case, but pod annotation store mac in Upper case.
 * [b53a21537](https://github.com/kubeovn/kube-ovn/commit/b53a2153727a8bd6c0e7a1a1fa89a20cd2bc2fd7) prepare for 1.2
 * [0d60df326](https://github.com/kubeovn/kube-ovn/commit/0d60df326555aa06b1a90299872d0a0b73923e78) fix: separate log for no address and wrong address
 * [a4106b2d7](https://github.com/kubeovn/kube-ovn/commit/a4106b2d7eab2346bef84c9afa05d2c35accf291) docs: format docs

### Contributors

 * Gary
 * Mengxin Liu
 * MengxinLiu
 * Yan Zhu
 * fangtian
 * linruichao

## v1.1.1 (2020-04-27)

 * [08f39db71](https://github.com/kubeovn/kube-ovn/commit/08f39db71344c4f48df00597919e66dddd0672d8) release 1.1.1
 * [95d479f58](https://github.com/kubeovn/kube-ovn/commit/95d479f580649426b2d8ab030a2a453b2b18ddbc) fix: labels might be nil
 * [eb9c9fd64](https://github.com/kubeovn/kube-ovn/commit/eb9c9fd64a689236034d98e7c32700ebb12b8456) monitor: make graph more sensitive to changes
 * [7e9b36611](https://github.com/kubeovn/kube-ovn/commit/7e9b36611eb8ae76b69cf27f226bf37286f08491) fix: ping output format
 * [ae53bf57b](https://github.com/kubeovn/kube-ovn/commit/ae53bf57b2b151430eff218e78d3c6da9c101f91) fix: yaml indent and ovn central dir
 * [82128d2f5](https://github.com/kubeovn/kube-ovn/commit/82128d2f54090f6eb43ad9aaf67b8ad6e4e6d57f) fix: fork go-ping and apply patches
 * [142479393](https://github.com/kubeovn/kube-ovn/commit/142479393e608c15787953c7efaf77c4e53fdef7) fix: mount etc/origin/ovn to ovs-ovn
 * [83f3a9204](https://github.com/kubeovn/kube-ovn/commit/83f3a92042c13210e642e9698b290d13dc3c93d3) fix: use legacy iptables

### Contributors

 * MengxinLiu

## v1.1.0 (2020-04-07)

 * [de9b003da](https://github.com/kubeovn/kube-ovn/commit/de9b003da042aab313ee2ae9d4f069231c5df846) release 1.1.0
 * [4511a16b6](https://github.com/kubeovn/kube-ovn/commit/4511a16b68cc6919e170e55b357537d57ebb922f) feat: use buildx to reduce image size
 * [370689e79](https://github.com/kubeovn/kube-ovn/commit/370689e79633f05810eea404c15e9831cca6a2e6) test: check host route when add/del a subnet
 * [0df863b67](https://github.com/kubeovn/kube-ovn/commit/0df863b67680b5baebc62b5af72a9bc8bd4f7df0) [DO NOT REVIEW] vendor update: introduce klogr and do some tidy
 * [eeba4c019](https://github.com/kubeovn/kube-ovn/commit/eeba4c01918166017f06f434ca9312cdb3953094) [webhook] init logger for controller-runtime
 * [ae187152a](https://github.com/kubeovn/kube-ovn/commit/ae187152abd6004dcaa0958b7fd2128eaf3ce8ab) test: add node test
 * [e1038d222](https://github.com/kubeovn/kube-ovn/commit/e1038d222d35700c71cc5749d46181d0103235c1) fix: acl and qos issues
 * [a4c81ba78](https://github.com/kubeovn/kube-ovn/commit/a4c81ba78999532fc6a11bb48765ad59abd4075c) feat: expose iface in install.sh
 * [b6967f57b](https://github.com/kubeovn/kube-ovn/commit/b6967f57bd58407576eef6964e11f0d8ace7227e) fix: remove auto checksums
 * [dbc85075a](https://github.com/kubeovn/kube-ovn/commit/dbc85075ab7822b0f7478a5b528792683e339c7a) perf: offload udp checksum if possible
 * [bdb23691c](https://github.com/kubeovn/kube-ovn/commit/bdb23691cce02e8c7a1dab88dac5ab3070a3f2ae) release v1.0.1
 * [cdf4de3f9](https://github.com/kubeovn/kube-ovn/commit/cdf4de3f905029f8617c8657d7d1768df03840bd) perf: add x86 optimization CFLAGS
 * [131181c24](https://github.com/kubeovn/kube-ovn/commit/131181c248681602eb75cd2722f1b30119e943e4) chore: add scripts to build ovs
 * [2b5dd72b6](https://github.com/kubeovn/kube-ovn/commit/2b5dd72b61ef8d877ea43d1ee36079990daaa2bf) fix: lost route when subnet add and is not ready
 * [9032ac843](https://github.com/kubeovn/kube-ovn/commit/9032ac8439422b342c75f684d909607ed8cb5345) fix: ip prefix might be empty
 * [d1654e152](https://github.com/kubeovn/kube-ovn/commit/d1654e152d676598dfb01881b8334711a93da094) chore: reduce image size
 * [464e991ee](https://github.com/kubeovn/kube-ovn/commit/464e991ee8e1aac9785264b6d6bab70ea2b075de) chore: modify nodeSelector label to support k8s 1.17
 * [2814a1d5f](https://github.com/kubeovn/kube-ovn/commit/2814a1d5f846f5aa6ed240daf9d77956dc499ad5) fix: use ovn-appctl to do recompute
 * [0eaedd99f](https://github.com/kubeovn/kube-ovn/commit/0eaedd99fffe3c20b2e3e54c0b13f2749c52ea49) docs: multi nic
 * [dd1923c32](https://github.com/kubeovn/kube-ovn/commit/dd1923c3292c14a2d109b9b3b247481cc3af2a22) feat: ip cr support multi-nic
 * [b2ce6f08e](https://github.com/kubeovn/kube-ovn/commit/b2ce6f08e09f14102f5fac90608c530667cae59d) fix: update in svc 1.1.1.1 may del svc 1.1.1.10
 * [20bb7a784](https://github.com/kubeovn/kube-ovn/commit/20bb7a7842b52f5e6a4a801f23cc3b3eedc52997) feat: add cni side logical to support ipam for multi-nic
 * [1319eb5d4](https://github.com/kubeovn/kube-ovn/commit/1319eb5d436298cd402d8f2a26faf0ddee13b144) feat: add basic allocation function for multus-cni
 * [8f6997a93](https://github.com/kubeovn/kube-ovn/commit/8f6997a93584b45d5acd036f619268549af43afd) fix: only delete pod that restart policy is Always
 * [3a2de9cdc](https://github.com/kubeovn/kube-ovn/commit/3a2de9cdc94eaa2da174a25fd14e0e4e3a44805e) perf: only enqueue updatePod when needed
 * [0f7b9d4ce](https://github.com/kubeovn/kube-ovn/commit/0f7b9d4ceef86e75f35ad78bd06119cfb41d0318) fix: add iptables to accept container traffic
 * [bdd021c03](https://github.com/kubeovn/kube-ovn/commit/bdd021c0347c7adb207d4684e3c631ea97830f75) feat: check kube-proxy and coredns in diagnose
 * [502f18cf2](https://github.com/kubeovn/kube-ovn/commit/502f18cf28a68fb3a739149480e8d549e87c3421) feat: add label param in install script
 * [5a1cf3719](https://github.com/kubeovn/kube-ovn/commit/5a1cf3719832ad306e02062da40eabfbbb0200b5) perf: recycle ip and lsp for pod that in failed or succeeded phase
 * [d19685841](https://github.com/kubeovn/kube-ovn/commit/d1968584156cf77180a0cd5d834e434d937eea63) fix: add inactivity_probe back
 * [417a001b6](https://github.com/kubeovn/kube-ovn/commit/417a001b660f5ba8fb04060cddea9b7b06177245) feat: check if crds exist in diagnose
 * [e65a9d091](https://github.com/kubeovn/kube-ovn/commit/e65a9d091d669de597905cea0629d5995bdb26fa) fix: gc static routes
 * [91829d240](https://github.com/kubeovn/kube-ovn/commit/91829d2404672926364b980cd6281fdbcb9a02ec) fix: still delete lsp if pod not in ipam
 * [7d22430d4](https://github.com/kubeovn/kube-ovn/commit/7d22430d4b93902f7e74210af8ab629001486f1b) fix: delete chassis from sb when delete node
 * [5f5df34e9](https://github.com/kubeovn/kube-ovn/commit/5f5df34e9d721d54bb5408fd1cd460a0919dda7e) fix: missing label selector
 * [9822dba99](https://github.com/kubeovn/kube-ovn/commit/9822dba992442cfa669f944817642b4239bbeb79) feat: add one script installer
 * [479437a35](https://github.com/kubeovn/kube-ovn/commit/479437a35667e228ba605d1c8800867b1294a377) fix: cleanup in offline environment
 * [e707eb96d](https://github.com/kubeovn/kube-ovn/commit/e707eb96d95cb28468e04ecd47a7f0a877296c3b) feat: diagnose check ds/deployment status
 * [3c786f57f](https://github.com/kubeovn/kube-ovn/commit/3c786f57f77d808f310905c7f184b3d4fad5bc77) refactor: the ipam now has lock itself no need for ippool queue
 * [9211486be](https://github.com/kubeovn/kube-ovn/commit/9211486be0a91aa7ea8f55d4d7020276a0f3b42a) fix: if pod is evicted, recycle address
 * [2546deaf7](https://github.com/kubeovn/kube-ovn/commit/2546deaf7fea0721de546946a3e473cb5a109aa9) fix: use uuid to fetch vip
 * [51f06bd64](https://github.com/kubeovn/kube-ovn/commit/51f06bd64ca07a9e958f7da4d32848a3b9495881) refactor ipam
 * [2336dc75f](https://github.com/kubeovn/kube-ovn/commit/2336dc75f546b6fa13700d7359284b477dff47c9) release 1.0.0
 * [7d918f560](https://github.com/kubeovn/kube-ovn/commit/7d918f5600468e0ac5f834b768b78e4cc9d429c1) refactor pod controller
 * [866db995f](https://github.com/kubeovn/kube-ovn/commit/866db995f89a41f9de3ed9e66fd5ace2f6da6071) merge images into one
 * [8296a9e7e](https://github.com/kubeovn/kube-ovn/commit/8296a9e7e4b4deaaae9a28412af961a8f50c96a4) fix:enablebash alias option in Dockerfile CMD scripts
 * [68d87ec23](https://github.com/kubeovn/kube-ovn/commit/68d87ec2327068f4bd5549080d0b9cf2d94dfe50) webhook: use global variables to avoid repeated map constructing
 * [cf2784adf](https://github.com/kubeovn/kube-ovn/commit/cf2784adff77f30237fd1bcf9e66100810a7a313) remove useless fields in webhook.yaml
 * [657b5a295](https://github.com/kubeovn/kube-ovn/commit/657b5a295c9ad7d59677e03620d45262e668eeae) remove leader-election for webhook manager
 * [2bcf0d284](https://github.com/kubeovn/kube-ovn/commit/2bcf0d284f3edf8a041bc4289293c4970a75ffea) feat: update to 20.03.0 ovn

### Contributors

 * Bruce Ma
 * MengxinLiu
 * Your Name

## v1.0.1 (2020-03-31)

 * [706cdfc37](https://github.com/kubeovn/kube-ovn/commit/706cdfc377268b4ebff1cb7cc6cee9ea25727599) release v1.0.1
 * [a51a672a0](https://github.com/kubeovn/kube-ovn/commit/a51a672a042645287aabb9d6ae0f850b86b11f14) fix: lost route when subnet add and is not ready
 * [576cf7768](https://github.com/kubeovn/kube-ovn/commit/576cf77680c8f3fca375f7662296bd8cd3fc9990) fix: ip prefix might be empty
 * [0e1670bf7](https://github.com/kubeovn/kube-ovn/commit/0e1670bf7d110d39048b57b4f30576556d057262) fix: update in svc 1.1.1.1 may del svc 1.1.1.10
 * [63f05e5a1](https://github.com/kubeovn/kube-ovn/commit/63f05e5a141f7da01a1c22651da7297943a6dd82) fix: add inactivity_probe back
 * [bad0c43f6](https://github.com/kubeovn/kube-ovn/commit/bad0c43f65c818609c00b014e443581082996f7b) fix: use uuid to fetch vip

### Contributors

 * MengxinLiu

## v1.0.0 (2020-02-27)

 * [f40ce553d](https://github.com/kubeovn/kube-ovn/commit/f40ce553de4d480782a0e4b1c83e248208a970c3) release 1.0.0
 * [282387945](https://github.com/kubeovn/kube-ovn/commit/282387945fa246f2d2a1f22f0fe377f583d797d8) prepare for 1.0
 * [a036b37b7](https://github.com/kubeovn/kube-ovn/commit/a036b37b790ecf545aedd7c94fd4d60b666cc7db) fix: add back missing lsp gc
 * [44d53c24a](https://github.com/kubeovn/kube-ovn/commit/44d53c24ae740b8053f4d44c7d3befc66878b033) fix: delete lb if it has no backend
 * [b8498a836](https://github.com/kubeovn/kube-ovn/commit/b8498a8364dac6839049cbd3af58ff0553592448) metrics: expose cni operation metrics
 * [a75f99917](https://github.com/kubeovn/kube-ovn/commit/a75f99917742711661dba4db54f780051607ea32) refactor: refactor server.go
 * [c88221ee4](https://github.com/kubeovn/kube-ovn/commit/c88221ee41bfe6b460694b74241c4bda30cde480) fix: disable ovn-nb inactivity_probe
 * [957654f98](https://github.com/kubeovn/kube-ovn/commit/957654f9845aa2a02daaef58dffd132331a457d4) fix: wait for container network ready before cni return
 * [870d20b0a](https://github.com/kubeovn/kube-ovn/commit/870d20b0a0c95c8386aaadac2992132ca128ceca) refactor: refactor controller.go
 * [2885419d8](https://github.com/kubeovn/kube-ovn/commit/2885419d83c9e03103a6ee01244b798860ccdb66) ovn: pick upstream performance patch
 * [115987397](https://github.com/kubeovn/kube-ovn/commit/1159873975902d7d2cb94b63bc026758f7edd5ce) docs: add the development guide and fix the lint
 * [0be255161](https://github.com/kubeovn/kube-ovn/commit/0be2551616b2cbc9424a85902b7d1b0539b0ef12) docs: add companies using kube-ovn section
 * [d56552b89](https://github.com/kubeovn/kube-ovn/commit/d56552b896ef0060c96cb9711dda9cbfdce07677) docs: add community information
 * [8edd02255](https://github.com/kubeovn/kube-ovn/commit/8edd0225548c4c964d71ef0a60e5c7f8403a2792) fix: alleviate ping lost
 * [632bbc5e1](https://github.com/kubeovn/kube-ovn/commit/632bbc5e17565396043d615760845721c48285f5) refactor: refactor ovn-nbctl.go
 * [8aafa415e](https://github.com/kubeovn/kube-ovn/commit/8aafa415ef4a882363f7324b177149d8f5ba9f6a) docs: modify the readme
 * [60ce76592](https://github.com/kubeovn/kube-ovn/commit/60ce76592a20c890287d9613eaa0fb2d7772ddfb) fix: pinger percentage error
 * [276a28cf8](https://github.com/kubeovn/kube-ovn/commit/276a28cf8af18246d5953a062457469ce500b7e5) fix: add kube-ovn types to default scheme
 * [998a9e634](https://github.com/kubeovn/kube-ovn/commit/998a9e63449158b04c87defe6282ac4717747db4) refactor: cniserver
 * [a5d339b2b](https://github.com/kubeovn/kube-ovn/commit/a5d339b2b5132a0ce01dc416e0042204d7c7f239) docs: update docs
 * [dc92afa3d](https://github.com/kubeovn/kube-ovn/commit/dc92afa3dcfa9022da7c34526289bc65e8d354fd) fix: add a periodically recompute to ovn-controller to avoid inconsistency
 * [8488ae2a6](https://github.com/kubeovn/kube-ovn/commit/8488ae2a65f3361bd554025967294c3bb4faefa4) fix: add timeout to pinger access ovs/ovn
 * [ff1ff1457](https://github.com/kubeovn/kube-ovn/commit/ff1ff1457906480c7502b408707777df68676b83) fix: when subnet cidr conflict requeue the subnet
 * [e31a08ec3](https://github.com/kubeovn/kube-ovn/commit/e31a08ec3fa67d2ab6ea4281f8a952b57f4d2e3d) fix: add runGateway to wait.Until
 * [182390737](https://github.com/kubeovn/kube-ovn/commit/1823907373f12cb812bc2616cf68b2e4497ed8c8) fix: restart nbctl-daemon if not response
 * [839308e08](https://github.com/kubeovn/kube-ovn/commit/839308e080243fafa1a40d7a3161a3c75f4d402e) feat: display controller log in kubectl-ko diagnose
 * [8e6c3d62d](https://github.com/kubeovn/kube-ovn/commit/8e6c3d62d529e4c6e1c9b818b6c37f6f43629230) refactor: separate normal check and ovn specific check
 * [c97831817](https://github.com/kubeovn/kube-ovn/commit/c97831817d6fd962ef8e07c32fcbcb364d4fce2d) fix: do not return not found err
 * [f19e55964](https://github.com/kubeovn/kube-ovn/commit/f19e5596453b5ba97b97693d54add4f4fc4561e2) fix: move components to kube-system ns and add priorityClass
 * [a5d298dbf](https://github.com/kubeovn/kube-ovn/commit/a5d298dbfb45ae33059ce5e3d5d8ae9b633e0ee8) feat: cniserver check allocated annotation before configure pod network
 * [8f72b7ebb](https://github.com/kubeovn/kube-ovn/commit/8f72b7ebb7437a95759aa9b628145b8044c8e4d5) fix: set ovn-openflow-probe-interval
 * [3838a46d1](https://github.com/kubeovn/kube-ovn/commit/3838a46d146148bd8b9d7b7c0cc914f984a8ab84) pinger: add port binds check between local ovs and ovn-sb
 * [f8248cec4](https://github.com/kubeovn/kube-ovn/commit/f8248cec44caf653c0ade0537c14e5f6f4f34cd2) fix: if cidr block not ends with zero, reformat it
 * [dff1d6485](https://github.com/kubeovn/kube-ovn/commit/dff1d64857359919e1405f2dd11f8b3782e68fec) fix: resync iptables
 * [40fab55f2](https://github.com/kubeovn/kube-ovn/commit/40fab55f27081b87303dd45cf019195f19cac06d) update version
 * [920053c5d](https://github.com/kubeovn/kube-ovn/commit/920053c5dfddf85ea0d81f9c5d1d4303c488a9d5) pinger: add timeout for dns resolve
 * [513d2bd9f](https://github.com/kubeovn/kube-ovn/commit/513d2bd9f55a19446a65bb7c8aa14543c5c931be) e2e: add basic framework and tests for e2e

### Contributors

 * Bruce Ma
 * Mengxin Liu
 * MengxinLiu
 * withlin

## v0.10.2 (2020-01-09)

 * [c5f49f24c](https://github.com/kubeovn/kube-ovn/commit/c5f49f24cae17ec229076b0ca51fc29abcf0eb89) release 0.10.2
 * [61b7dded9](https://github.com/kubeovn/kube-ovn/commit/61b7dded9c974983c891bed6fba4840c3942eddc) fix: add a periodically recompute to ovn-controller to avoid inconsistency
 * [9de9d0b5d](https://github.com/kubeovn/kube-ovn/commit/9de9d0b5d131a894a5acb86074298f8bd3813b82) fix: when subnet cidr conflict requeue the subnet
 * [dca159143](https://github.com/kubeovn/kube-ovn/commit/dca1591436e601fb7228cf2bc2097d773730d65f) fix: add runGateway to wait.Until
 * [f16209b44](https://github.com/kubeovn/kube-ovn/commit/f16209b441537a2de63bd0b501e226f00d18b4ed) fix: restart nbctl-daemon if not response

### Contributors

 * Mengxin Liu

## v0.10.1 (2020-01-02)

 * [09e27cea7](https://github.com/kubeovn/kube-ovn/commit/09e27cea7e6f34dd0ca76973ab93ccad8d102a5e) release: v0.10.1
 * [fafa56071](https://github.com/kubeovn/kube-ovn/commit/fafa560712816d74515a0764c0f6bf2193e2bae0) fix: do not return not found err
 * [858d3331f](https://github.com/kubeovn/kube-ovn/commit/858d3331f6c1a066a7a66929d61db492738f9b54) fix: set ovn-openflow-probe-interval
 * [641d6f866](https://github.com/kubeovn/kube-ovn/commit/641d6f86627d84e013ae5a59c5b0f0a0dc5a54db) pinger: add port binds check between local ovs and ovn-sb
 * [8435a335c](https://github.com/kubeovn/kube-ovn/commit/8435a335c81955794053d72230a4473e63091ddd) fix: if cidr block not ends with zero, reformat it
 * [1f5df246a](https://github.com/kubeovn/kube-ovn/commit/1f5df246a24357fce175f182a66deab3e634636b) fix: resync iptables

### Contributors

 * Mengxin Liu

## v0.10.0 (2019-12-23)

 * [9747d5405](https://github.com/kubeovn/kube-ovn/commit/9747d5405f8c8552a33f89a0f35ffc916af7d11d) docs: update changelog
 * [adf5071e1](https://github.com/kubeovn/kube-ovn/commit/adf5071e1dc58dae33fafee7913429e2b93e2cfe) fix: address in ep might be empty
 * [182bb1513](https://github.com/kubeovn/kube-ovn/commit/182bb1513017de14327fd681fa940f2965f8287c) fix: cniserver wait ovs ready
 * [518c0a781](https://github.com/kubeovn/kube-ovn/commit/518c0a7817e174a353d3d2bfd9f4d7f3dc633af0) fix: wrong deletion in gc lb and portgroup
 * [2492a1661](https://github.com/kubeovn/kube-ovn/commit/2492a166147d008dcaa4fbd4aa71267618c1cdfb) ovn: add memory patch to slow down memory increase
 * [d0bd71fd7](https://github.com/kubeovn/kube-ovn/commit/d0bd71fd7acde3e2315491d8491cc21875bb0656) fix: wait default and node logical switch ready
 * [23cad463b](https://github.com/kubeovn/kube-ovn/commit/23cad463b9f6fe07a8668dcc1008dba70666cc2b) fix: podSelector in networkpolicy should only consider pods in the same ns
 * [ca5539f09](https://github.com/kubeovn/kube-ovn/commit/ca5539f09cbff947bec69487e5e8e70b377b0400) fix: do not add unallocated pod to port-group
 * [d5ed1ee73](https://github.com/kubeovn/kube-ovn/commit/d5ed1ee73151a67612a87372850e9b7da01a4d66) release 0.10.0
 * [3c62ea29e](https://github.com/kubeovn/kube-ovn/commit/3c62ea29e5688a7a8d019e942040c05995e35126) ovn: pick up commit from upstream
 * [4c966c371](https://github.com/kubeovn/kube-ovn/commit/4c966c371713474edecec051151330761c716806) feat: pinger support check an address out of cluster.
 * [f00960789](https://github.com/kubeovn/kube-ovn/commit/f009607895f0a4610fa3ca72c16d7ea8d05a604c) chore: double quote shell variables
 * [83364b52e](https://github.com/kubeovn/kube-ovn/commit/83364b52e1a74d9cf3805620599ff581a7a8cbbe) fix: cluster mode db will generate lots listen error log
 * [d9e1cd1ca](https://github.com/kubeovn/kube-ovn/commit/d9e1cd1cad29de427bfdeea25aea71092c51a613) fix: gc logical_switch_port form listing pods and nodes
 * [a5dc8bb9b](https://github.com/kubeovn/kube-ovn/commit/a5dc8bb9b3a2b00e8e700cf75e923395268fa50f) fix: some init and cleanup bugs
 * [a5eb5e7f3](https://github.com/kubeovn/kube-ovn/commit/a5eb5e7f3bb259f53c50fba35b225bd160ee3694) fix: ovn-cluster mode
 * [a6f0dd143](https://github.com/kubeovn/kube-ovn/commit/a6f0dd143b8bc0b47b313b54247a810f3b13b929) feat: exclude_ips can be changed dynamically
 * [d9c594343](https://github.com/kubeovn/kube-ovn/commit/d9c594343a3727d2c15a2f199eb1b0624605bd7d) update ovn to 2.12.0-1
 * [06eceb3b3](https://github.com/kubeovn/kube-ovn/commit/06eceb3b31fc389248309746e64664e541f78012) feat: use label to select leader to avoid pod status misleading
 * [aa53c7dd4](https://github.com/kubeovn/kube-ovn/commit/aa53c7dd4122dd2e7b1b9d20bd07e6393f2b352e) fix: ip conflict when use ippool
 * [590443301](https://github.com/kubeovn/kube-ovn/commit/59044330170e47c0a3b6f60857de8ac91405fab3) docs: add v0.9.1 changelog
 * [5efbea9f3](https://github.com/kubeovn/kube-ovn/commit/5efbea9f31ccd6b47bc3b30b4e5f3a27df2602ea) fix: block subnet deletion when there any ip in use
 * [a1dc8c11f](https://github.com/kubeovn/kube-ovn/commit/a1dc8c11fb95b1c60d52ae7ecb39df26932d5a0f) plugin: kubectl plugin now expose ovs-vsctl to each node
 * [d3c6a71c5](https://github.com/kubeovn/kube-ovn/commit/d3c6a71c50caa98b6b667ca71b7004c5e0a9b266) fix: nbctl need timeout to avoid hang infinitely
 * [77e589034](https://github.com/kubeovn/kube-ovn/commit/77e58903458c1765a9541bc3e4415e5ee52877fe) perf: as lr-route-add with --may-exist will replace exist route, no need for another delete
 * [d4a51bdc6](https://github.com/kubeovn/kube-ovn/commit/d4a51bdc67b4dbafd6a4d3e91c71242d47407250) perf: when controller restart skip pod already create lsp
 * [7617fa797](https://github.com/kubeovn/kube-ovn/commit/7617fa79776ffa04a38fc7c8d8a607f3e44c0ff6) fix: when delete node recycle related ip/route resource
 * [f4e874769](https://github.com/kubeovn/kube-ovn/commit/f4e8747693c1220597616cbbb4653696c45864ed) fix typo in start-ovs.sh
 * [9b88e0841](https://github.com/kubeovn/kube-ovn/commit/9b88e08419471e186e29d25dde47e5a53c7ef068) perf: skip evicted pod when enqueueAddPod and enqueueUpdatePod
 * [e48186240](https://github.com/kubeovn/kube-ovn/commit/e481862402cd5af579ce44391a34c1b4f9af44b8) fix: use ep.subset.port.name to infer target port number
 * [0d8ae20c3](https://github.com/kubeovn/kube-ovn/commit/0d8ae20c3eb42dfaf115c8ec0b67ba7d720be5f0) fix: if no available address delete pod might failed related to #155
 * [bbd4257d0](https://github.com/kubeovn/kube-ovn/commit/bbd4257d0236e69d7f0b480e22161ca10ea4ae3c) kind: support reload kube-ovn component in kind cluster
 * [d0479e903](https://github.com/kubeovn/kube-ovn/commit/d0479e9035aa63d64864182176a8ee293872af02) perf: filter pod in informer list-watch and disable resync
 * [61a7a7b9c](https://github.com/kubeovn/kube-ovn/commit/61a7a7b9c12d8ff364366f94eecfdd9b8479e41b) fix: index out of range err when create lsp
 * [623661ef2](https://github.com/kubeovn/kube-ovn/commit/623661ef28db2444f1ef6a4242357e59c251e449) prepare for next release
 * [1643c7f0d](https://github.com/kubeovn/kube-ovn/commit/1643c7f0da3c472e5265584960ad379adfe9f8bd) kind: support to install kube-ovn in kind
 * [9611599f1](https://github.com/kubeovn/kube-ovn/commit/9611599f1d447f911d20b6fad5b21a28de960bee) fix: mount /var/run/netns that kind will use it to store network ns files

### Contributors

 * Mengxin Liu
 * qsyqian

## v0.9.1 (2019-12-02)

 * [5d4714c1e](https://github.com/kubeovn/kube-ovn/commit/5d4714c1e042af91a51450784ac00c7518d414bf) release v0.9.1
 * [847ef8b08](https://github.com/kubeovn/kube-ovn/commit/847ef8b088b94041d1c435af66016a4b78fde376) fix: block subnet deletion when there any ip in use
 * [e0fbfea64](https://github.com/kubeovn/kube-ovn/commit/e0fbfea64ccd93c376083c97dca282dfa764d5ec) fix: nbctl need timeout to avoid hang infinitely
 * [dd63c5a41](https://github.com/kubeovn/kube-ovn/commit/dd63c5a41e830d9fbfae7af1ae2d4ea9c3b324e3) fix: when delete node recycle related ip/route resource
 * [4d0ad6c7b](https://github.com/kubeovn/kube-ovn/commit/4d0ad6c7bff32bd667c27062ddea48d0dbd929f8) fix typo in start-ovs.sh
 * [646a177ca](https://github.com/kubeovn/kube-ovn/commit/646a177ca778103b5c880cb2dc837a46072dcf47) fix: use ep.subset.port.name to infer target port number
 * [9ae58a81a](https://github.com/kubeovn/kube-ovn/commit/9ae58a81ae021948457860e805b2d842b494dda5) fix image tag
 * [3b793d4ac](https://github.com/kubeovn/kube-ovn/commit/3b793d4ac20493447d47646553dcfa776644075f) fix: mount /var/run/netns that kind will use it to store network ns files
 * [093770dd5](https://github.com/kubeovn/kube-ovn/commit/093770dd5d453188fa6a4bcb68ad91727cc78e77) fix: index out of range err when create lsp

### Contributors

 * Mengxin Liu
 * qsyqian

## v0.9.0 (2019-11-22)

 * [53db261a1](https://github.com/kubeovn/kube-ovn/commit/53db261a12444f89b3520a1b2528c7f08063ce6b) release: v0.9.0
 * [1984cbe80](https://github.com/kubeovn/kube-ovn/commit/1984cbe8010bc52d49989caa336076b2675110e3) feat: when use nodelocaldns do not nat the address
 * [446999f4c](https://github.com/kubeovn/kube-ovn/commit/446999f4cd022e4540f0239a346bd17178f4f9af) docs: add description about relation of cidr and static ip allocation
 * [6f1854f9f](https://github.com/kubeovn/kube-ovn/commit/6f1854f9f58cc94cfc74b71eebf89f2770e0509b) Check the short name of kubernetes services which is independant of the cluster domain name.
 * [c6f8efeb0](https://github.com/kubeovn/kube-ovn/commit/c6f8efeb059208c766fe3585558c8f8c38439b58) fix: some grafana modification
 * [40144160d](https://github.com/kubeovn/kube-ovn/commit/40144160d2926628d27a7766d3332dd7569342f9) fix: add missing cap
 * [7c464d692](https://github.com/kubeovn/kube-ovn/commit/7c464d692f7e3a80c47aa642034645b0ea70e28d) chore: update ovn and other minor fix
 * [ac5371521](https://github.com/kubeovn/kube-ovn/commit/ac53715217327651264af87b93e627c9908d39a9) fix re-annotate namespaces when subnet deleted
 * [fe2f2612a](https://github.com/kubeovn/kube-ovn/commit/fe2f2612a60164b1bbe049a4c5dbdcef984fc6d6) fix: add ingress_policing_burst to accurate limit ingress bandwidth
 * [20b2c83de](https://github.com/kubeovn/kube-ovn/commit/20b2c83deab57801aaf2fce8b475463cefc991d2) fix: network unreachable when add egress qos for pod
 * [758dbc1ce](https://github.com/kubeovn/kube-ovn/commit/758dbc1ce7c14b93bbf613f7385ea2e7f4bbb7c5) fix: err when add egress qos
 * [bdfd351d7](https://github.com/kubeovn/kube-ovn/commit/bdfd351d738f849f26b186c398da11c9453080bc) fix: remove privilege=true from long run container
 * [0859da1fe](https://github.com/kubeovn/kube-ovn/commit/0859da1fe5a2d0195a9ebd81a2c723e4b3be6d98) perf: optimize pod add
 * [3718851df](https://github.com/kubeovn/kube-ovn/commit/3718851df4087e92cdc2d68d2dd4592413c61742) fix: add keepalive to ovn-controller
 * [6ad981064](https://github.com/kubeovn/kube-ovn/commit/6ad981064336a19c441c4eceff2153066ba53273) feat: add controller metrics
 * [b87ed0eec](https://github.com/kubeovn/kube-ovn/commit/b87ed0eec24dde544770635e318c710a81adb0c7) If pod have not a status.PodIP skip add/del static route
 * [b9108fba4](https://github.com/kubeovn/kube-ovn/commit/b9108fba4d0d6f26f7940d55f8c7236c4f5f3858) fix: ippool pod static route might lost during leader election
 * [a2e24de62](https://github.com/kubeovn/kube-ovn/commit/a2e24de62a553e791506a7b8679f72f94e161dea) fix: static route might lost during leader election
 * [8202a1886](https://github.com/kubeovn/kube-ovn/commit/8202a188663e674eced3348ec1dac90d423930c2) feat: add grafana config and modify metrics.
 * [cae0ef274](https://github.com/kubeovn/kube-ovn/commit/cae0ef274857ada317cfe5092278c6f1b53e675c) fix: only keep the last iface-id
 * [f3528f237](https://github.com/kubeovn/kube-ovn/commit/f3528f237bcfc07b3b459bdd0a47a15077dd66d2) fix: add missing gc
 * [3791ba29d](https://github.com/kubeovn/kube-ovn/commit/3791ba29d0adf66bd4511a3e35efc207cff2659c) fix: gc resource when start controller
 * [f970615bb](https://github.com/kubeovn/kube-ovn/commit/f970615bb1a583b10f506a2f70510f45e293a08f) fix: watch will break if timeout is set
 * [ef285b213](https://github.com/kubeovn/kube-ovn/commit/ef285b213de5086980dc70eeb62542370e8e4427) feat: pinger add apiserver check metrics
 * [d33685e64](https://github.com/kubeovn/kube-ovn/commit/d33685e6403aea8838ab4f6ca2ccd04dadd9002e) fix: avoid conflict when init

### Contributors

 * Mengxin Liu
 * QIANSHUANGYANG [钱双洋]
 * Sébastien BERNARD
 * Yan Zhu

## v0.8.0 (2019-10-08)

 * [6b57f61b3](https://github.com/kubeovn/kube-ovn/commit/6b57f61b34c18ee5815db7e595bca327967d8f68) release v0.8.0
 * [6ed722f94](https://github.com/kubeovn/kube-ovn/commit/6ed722f9439bd5987f232b6ab584b5c61ceefc33) fix: loss might be negative number
 * [7c0517b51](https://github.com/kubeovn/kube-ovn/commit/7c0517b5120770ff72ff35b45376457ae85084e4) feat: pinger prometheus support
 * [e23bd552f](https://github.com/kubeovn/kube-ovn/commit/e23bd552f46bdb76065f869d4130533c670c55a9) feat: support pinger
 * [d837aa122](https://github.com/kubeovn/kube-ovn/commit/d837aa122b9d37728e3db349b1e5e9e1f9580c03) chore: update ovs/ovn
 * [4246cb74d](https://github.com/kubeovn/kube-ovn/commit/4246cb74de513fb1267b512e19eedcb2e8b969e4) feat: gateway ha
 * [e27c9e54d](https://github.com/kubeovn/kube-ovn/commit/e27c9e54df996c82319f91f8b255706b2ae8781c) chore: remove ovs-ipsec and update go to 1.13
 * [ba3084ebc](https://github.com/kubeovn/kube-ovn/commit/ba3084ebc827b3fba3dbac367f92f705151445db) feat: add kubectl plugin
 * [54a465d16](https://github.com/kubeovn/kube-ovn/commit/54a465d161f9351c49e103261c716ba2b3568e68) docs: add comparison
 * [38be68d6f](https://github.com/kubeovn/kube-ovn/commit/38be68d6f2a8c50c6116413a3f95d668de43f42e) fix: pod should be accessed from node when acl applied
 * [e62f0ab04](https://github.com/kubeovn/kube-ovn/commit/e62f0ab0412561d8607aca7843b899d2a1aac514) enable portmap by default to support hostport
 * [80de8e58d](https://github.com/kubeovn/kube-ovn/commit/80de8e58d53a5e98fdbce89030637535e63b1ea8) feat: add port security to pod port
 * [4849f0562](https://github.com/kubeovn/kube-ovn/commit/4849f05621dac0220d6db10c8bd3f52907035ee7) feat: add node switch allocated ip cr
 * [34e8406ee](https://github.com/kubeovn/kube-ovn/commit/34e8406ee17ef7d28229887e9fd6557e782cdee8) prepare for next release

### Contributors

 * Mengxin Liu
 * MengxinLiu
 * Yan Zhu

## v0.7.0 (2019-08-21)

 * [933fd8d25](https://github.com/kubeovn/kube-ovn/commit/933fd8d2505cc813f0db18f05d4535ac7d530b15) release: bump v0.7.0
 * [7e2bdf522](https://github.com/kubeovn/kube-ovn/commit/7e2bdf522015a7678f7a05b0f6b7cbba3af5c72d) fix: add default excludeIps and check kern version
 * [31544abb5](https://github.com/kubeovn/kube-ovn/commit/31544abb510f615172dc19eaeb96e342b80de222) fix: deal with ipv6 connection str
 * [0f8f2aad7](https://github.com/kubeovn/kube-ovn/commit/0f8f2aad7492fa295f680faeb4c65e14b5ed8a2a) fix missing condition when subnet is private
 * [d37da1bc4](https://github.com/kubeovn/kube-ovn/commit/d37da1bc48c130221c7f3631a7e7e2d8b4b25948) add subnet status
 * [4a5c54983](https://github.com/kubeovn/kube-ovn/commit/4a5c5498345af803e6ebd16d7bed648134855be1) fix: acl related issues
 * [62a395e6f](https://github.com/kubeovn/kube-ovn/commit/62a395e6f242c96f4af81be6f93781bcb7508326) Revert "add subnet status field"
 * [b8f1d9ef0](https://github.com/kubeovn/kube-ovn/commit/b8f1d9ef0fef618ee14bb33dd18bce560ac084a5) add missing subnets/status operation permission
 * [6c119ad1e](https://github.com/kubeovn/kube-ovn/commit/6c119ad1e2d784ab5b6397e722c8302867b29d38) Update cleanup.sh
 * [b08ece4fb](https://github.com/kubeovn/kube-ovn/commit/b08ece4fbca124d97ed83f9c6ac195ae84a4f64e) feat: add exclude_ips annotation to namespace
 * [a2774ed0e](https://github.com/kubeovn/kube-ovn/commit/a2774ed0e44a48e17efa0ea0bbb5b32277ea5430) fix: use pg-del to remove pg and acl, check if ports is empty before set pg
 * [422c6dc00](https://github.com/kubeovn/kube-ovn/commit/422c6dc0001108bb4bf394baf8b903ee1baa30ea) add subnet status
 * [fde683eaf](https://github.com/kubeovn/kube-ovn/commit/fde683eaf8e8574f6d53867478b955310a248594) feat: add subnet annotation to ns and automatically unbind ns from subnet.
 * [948e13063](https://github.com/kubeovn/kube-ovn/commit/948e130638f8870b7919d58229d1e6efd18d7c5d) docs: add cn docs link
 * [5278e1051](https://github.com/kubeovn/kube-ovn/commit/5278e1051139d986e78f04c32a1a95074787f61a) feat: add default values to subnet
 * [ea451a1aa](https://github.com/kubeovn/kube-ovn/commit/ea451a1aace30bbf35b93ddc14978c5dcab32322) write back subnet name to ip label
 * [1c7121dbb](https://github.com/kubeovn/kube-ovn/commit/1c7121dbb9ade6aa0edb362050223cbed2ad8b6d) chore: enable mirror in yaml and modify docs
 * [db9783a3a](https://github.com/kubeovn/kube-ovn/commit/db9783a3a5d045814250110d9d111a54277a622e) fix: duplicate import in network_policy.go
 * [8a57747ef](https://github.com/kubeovn/kube-ovn/commit/8a57747ef794ea6fb9c570992e0d023e01bc3a76) fix: improve cni-conf name priority
 * [5f1436bef](https://github.com/kubeovn/kube-ovn/commit/5f1436bef28b59fcc8a6a2b1b66b6b0a746633c2) fix: wait subnet ready before start worker.
 * [661387efe](https://github.com/kubeovn/kube-ovn/commit/661387efea058278baf503add43d53ef4cc4d03d) fix: check ls exists before handle it
 * [9e05f5333](https://github.com/kubeovn/kube-ovn/commit/9e05f53333afd65c6cb122200a3a6d21b1aa8c6d) docs: add more installation tools.
 * [dccb93c7b](https://github.com/kubeovn/kube-ovn/commit/dccb93c7b347613dcae28c59a347651d2188582d) docs: add support os and notes.
 * [c6a160b3f](https://github.com/kubeovn/kube-ovn/commit/c6a160b3f09df6a20fd48abe9a5ec338fa5792ef) Update subnet.md
 * [31ad00bd7](https://github.com/kubeovn/kube-ovn/commit/31ad00bd75a3e1fe4aaa4f7993b56e9ec1b94163) feat: add ip info to ip crd
 * [ad7b5c2f6](https://github.com/kubeovn/kube-ovn/commit/ad7b5c2f67d885c8e7ad6c1067d8b5ddc3d663cf) feat: update logo
 * [44c3077c7](https://github.com/kubeovn/kube-ovn/commit/44c3077c7b0153944861acf2c20b1363819cb39c) feat: add logo
 * [55d7fd6f7](https://github.com/kubeovn/kube-ovn/commit/55d7fd6f791357d666cca3bc24d11e3e96485acf) feat: reserve vport for statefulset pod
 * [7a3c8a6a8](https://github.com/kubeovn/kube-ovn/commit/7a3c8a6a8f7be35af6ab07d0363d2484dd4462d5) docs: add crd installation
 * [aa016c1b0](https://github.com/kubeovn/kube-ovn/commit/aa016c1b0220f51a3a273acc44bd5c9d48aadde2) fix: modify default header length
 * [85b406907](https://github.com/kubeovn/kube-ovn/commit/85b4069077dcc52600ccdca26bda24e9b5a723f8) fix: do not create exist logical switch
 * [362943660](https://github.com/kubeovn/kube-ovn/commit/3629436602367fcd8e4077a292859da59eb335a5) chore: prepare for next release

### Contributors

 * MengxinLiu
 * Yan Zhu
 * ftiannew
 * halfcrazy
 * shuangyang.qian

## v0.6.0 (2019-07-22)

 * [463d6253a](https://github.com/kubeovn/kube-ovn/commit/463d6253a4e5362496d2b9fd5f90d7c39ecf87e4) docs: add crd/ipv6 docs and bump version 0.6.0
 * [103c23af6](https://github.com/kubeovn/kube-ovn/commit/103c23af64b08998768770b512b894e609456528) fix build error
 * [9d173ba0c](https://github.com/kubeovn/kube-ovn/commit/9d173ba0c28dc84b322e4ea729edf6d7972f9100) feat: support ipv6-only mode
 * [05566017c](https://github.com/kubeovn/kube-ovn/commit/05566017cb41850fe1958c0ac392017f6f4bbeab) add webhook docs
 * [766cec9b8](https://github.com/kubeovn/kube-ovn/commit/766cec9b8435ae3a2b2f37f1fdd0056c9c582ebf) add admission webhook for static ip
 * [2abeacb4a](https://github.com/kubeovn/kube-ovn/commit/2abeacb4a85b7db613dcfe96106ddda3a57b059b) docs: add support platform version
 * [ed7264ea9](https://github.com/kubeovn/kube-ovn/commit/ed7264ea91cafe6201ad48559fc9ac7dfed46dea) feat: use subnet crd to manage logical switch
 * [1e5c9f6cb](https://github.com/kubeovn/kube-ovn/commit/1e5c9f6cbdc5b120da2a528e99883796281fd5a6) Use k8s hostname, fix #60
 * [873672952](https://github.com/kubeovn/kube-ovn/commit/873672952e945cfc90e9e1cba0630d6551cab644) fix: remove dependency on cluster-admin
 * [e0864a03d](https://github.com/kubeovn/kube-ovn/commit/e0864a03d012a311e5ff3ae854d86a7cafabc790) chore: use go mod to replace dep
 * [96ec620d9](https://github.com/kubeovn/kube-ovn/commit/96ec620d924568a67d482b003cba61627930c64a) docs: update mirror feature to readme
 * [855d834fa](https://github.com/kubeovn/kube-ovn/commit/855d834fa9721ebad93ed26d3fdfb88d8a0a8c39) feat: support traffic mirror
 * [d1c3ea853](https://github.com/kubeovn/kube-ovn/commit/d1c3ea853f935414eb0f34baadce8b377996ff62) prepare for next release

### Contributors

 * MengxinLiu
 * Yan Zhu

## v0.5.0 (2019-06-07)

 * [782e04bee](https://github.com/kubeovn/kube-ovn/commit/782e04bee9922016c2332602145f45cd186fc7c3) chore: bump v0.5.0
 * [a27f83394](https://github.com/kubeovn/kube-ovn/commit/a27f8339456a5264611af4dbbe5482e42fdd1dc1) fix: wrong mtu
 * [447071676](https://github.com/kubeovn/kube-ovn/commit/4470716766a6aa3da3ac6453aae2992846301c58) feat: support user define iface and mtu
 * [f8d8e186f](https://github.com/kubeovn/kube-ovn/commit/f8d8e186fee449dc7f1549a046566719c19c8047) fix: remove mask field from ip annotation
 * [550904047](https://github.com/kubeovn/kube-ovn/commit/5509040475fe64297c5e68a7e8f45b4dcc61be9e) feat: auto assign gw for controller config and expose more cmd args
 * [48da0fe17](https://github.com/kubeovn/kube-ovn/commit/48da0fe177dab436450650d1e346431be349bd03) feat: add pprof and use it as probe
 * [8984c90b5](https://github.com/kubeovn/kube-ovn/commit/8984c90b567750195b72b694d1a230bfdceb1c8a) feat: set kernel args when start cniserver
 * [208a1dfc7](https://github.com/kubeovn/kube-ovn/commit/208a1dfc797552d24bc385e2873198e8ae9f851f) feat: support network policy
 * [c8d208fbd](https://github.com/kubeovn/kube-ovn/commit/c8d208fbdfdf00e18653f9cba735b35cd03f74fd) prepare for next release

### Contributors

 * MengxinLiu

## v0.4.1 (2019-05-27)

 * [5a2cb093c](https://github.com/kubeovn/kube-ovn/commit/5a2cb093c6eddb1c450b74f1c942c153341a40bb) bump version to v0.4.1
 * [f8e8b0011](https://github.com/kubeovn/kube-ovn/commit/f8e8b00114af09ca62a8e00eb28623640a73ec87) fix: manual static ip allocation and automatic allocation should use different ip validation
 * [031924d10](https://github.com/kubeovn/kube-ovn/commit/031924d10dbcda61deb217fa4f3efe1b13748f6b) Fix json: cannot unmarshal string into Go value of type request.PodResponse https://github.com/alauda/kube-ovn/issues/33
 * [24259dbf9](https://github.com/kubeovn/kube-ovn/commit/24259dbf95af068990e0d1ebf52971a0d928c0d4) fix: use ovsdb-client to get leader info
 * [3541b6cf7](https://github.com/kubeovn/kube-ovn/commit/3541b6cf747dc33d7667acb467e5a1a0f86c8dbc) fix: use default-gw as default-exclude-ips and expose args to docs
 * [69c485380](https://github.com/kubeovn/kube-ovn/commit/69c48538074d0e002c444207fbd68995f63b8293) to cleanup all created resources, not only kube-ovn namespace.
 * [9361bb438](https://github.com/kubeovn/kube-ovn/commit/9361bb4383ccbea67c63858150366c439fb92360) prepare for next release

### Contributors

 * MengxinLiu
 * Yan Zhu
 * fanbin

## v0.4.0 (2019-05-16)

 * [509bf4a41](https://github.com/kubeovn/kube-ovn/commit/509bf4a4168f95e99e86ce8d4375967b50b66a64) feat: bump version to 0.4.0
 * [2e4145197](https://github.com/kubeovn/kube-ovn/commit/2e414519755441d90e0947a4337bd4b72fbf29d1) feat: support expose pod ip to external network
 * [8992bbe38](https://github.com/kubeovn/kube-ovn/commit/8992bbe3833e1f9ac80b0aa95fafcd7d173d0000) fix: check conflict subnet cidr
 * [0f9d1e4be](https://github.com/kubeovn/kube-ovn/commit/0f9d1e4be28947269394d44747f7bea06ece8911) fix: start informer when controller is leader
 * [71c15d65b](https://github.com/kubeovn/kube-ovn/commit/71c15d65b0ed3ce09f0296fa897c8a4c1957445d) feat: validate namespace/pod annotations
 * [89491b570](https://github.com/kubeovn/kube-ovn/commit/89491b5702ac3dbffca10c7b7e38e0fe2165c48a) fix: wait node-gw info ready
 * [0d86393d2](https://github.com/kubeovn/kube-ovn/commit/0d86393d298725de08b28401cbf094811eee0e93) fix: use ovn/ovs-ctl to health check
 * [278ccfe5e](https://github.com/kubeovn/kube-ovn/commit/278ccfe5e94e208fa2f8e812c01ea4a9fa992070) feat: remove finalizer dependency improve svc performance
 * [8f962673a](https://github.com/kubeovn/kube-ovn/commit/8f962673a7ee83bdb9706ebc2548885f4661aca5) fix: reuse node ip and mac annotation
 * [b8f85143b](https://github.com/kubeovn/kube-ovn/commit/b8f85143b6571462845fc4487966ea33cbfbcbe6) Add ha for ovn dbs and simplify makefile
 * [3c617451e](https://github.com/kubeovn/kube-ovn/commit/3c617451efe78b46acddcf012e00d049bf722171) feat: merge ovn-nbctl request
 * [b5ac7da4a](https://github.com/kubeovn/kube-ovn/commit/b5ac7da4a5c9a1f50a1065440abb0660440d2aaf) feat: separate ip pool pod and add parallelism to workers
 * [ce105dffb](https://github.com/kubeovn/kube-ovn/commit/ce105dffb1a6634bc414ed33030ee61ef507c106) Mute logrus log for ipset Dont need to change the vendored code.
 * [657470c8f](https://github.com/kubeovn/kube-ovn/commit/657470c8f27de71451c81639e64d148b56354829) Fix klog cant use V module The side affect of this commit is glog's V module not work.
 * [5429f51be](https://github.com/kubeovn/kube-ovn/commit/5429f51bea2157f321ada9400463df85db41f342) feat: use ovn macam to allocate mac for static ip pod
 * [5a8958cdb](https://github.com/kubeovn/kube-ovn/commit/5a8958cdb18d4907668db16191e98abc1d9b1cba) feat: update ovn to 2.11.1
 * [ca036f9e7](https://github.com/kubeovn/kube-ovn/commit/ca036f9e7eb9a65b3641fb98c6c744454880897a) Add vagrantfile
 * [660c0570c](https://github.com/kubeovn/kube-ovn/commit/660c0570cf2130b9bddb2d5706af5fc4caad3b41) fix: use tag version yaml url
 * [bc66671c5](https://github.com/kubeovn/kube-ovn/commit/bc66671c511f64cdd8c9d76588f8586464434b89) chore: fix go-report golint issues
 * [12a4bec93](https://github.com/kubeovn/kube-ovn/commit/12a4bec93e7b655e439af8e2903e895d62a7b047) ha for kube-ovn-controller
 * [b7d0f599e](https://github.com/kubeovn/kube-ovn/commit/b7d0f599e153f2c39cc96af59425c57db3e62ad1) cleanup unused code
 * [756831d72](https://github.com/kubeovn/kube-ovn/commit/756831d7213846d8c9676775a02a6d505c67dfd5) docs: add network topology
 * [c05594873](https://github.com/kubeovn/kube-ovn/commit/c055948732089450464c747842c5914703762d1b) chore: Minor updates to gateway.md
 * [21e34e9f2](https://github.com/kubeovn/kube-ovn/commit/21e34e9f217a64ef7bd8c078150d5c2e199542e8) chore: Gateway documentation touch-ups
 * [aa0b2b7c1](https://github.com/kubeovn/kube-ovn/commit/aa0b2b7c147c3108c0e1315870a83393efcb6d7a) chore: QoS documentation touch-ups
 * [3ec0098a4](https://github.com/kubeovn/kube-ovn/commit/3ec0098a4e0ab10b6924a58bcf18d04538b29ef1) chore: Subnet Isolation documentation touch-ups
 * [524845e93](https://github.com/kubeovn/kube-ovn/commit/524845e93c7197e3b61e50949012e5ab4dd30e14) chore: Static IP documentation touch-up
 * [b510016c2](https://github.com/kubeovn/kube-ovn/commit/b510016c2916b91b16930a2c36a6e41122b2c0a1) chore: Subnet documentation touch-ups
 * [524f7d3ff](https://github.com/kubeovn/kube-ovn/commit/524f7d3ff3ce02b4f6261c4e33b0137a935b4431) chore: Installation Guide touch-ups
 * [a1995d037](https://github.com/kubeovn/kube-ovn/commit/a1995d0374f47e8ab932fa09ed83fc690ec6bcb0) chore: README touch-up.

### Contributors

 * Kai Chen
 * MengxinLiu
 * Yan Zhu

## v0.3.0 (2019-04-19)

 * [79c0642eb](https://github.com/kubeovn/kube-ovn/commit/79c0642eba7b8146269262ce2d011f1c725ef1a3) docs: bump version
 * [cb2f50da4](https://github.com/kubeovn/kube-ovn/commit/cb2f50da4639aa02e6bde5acc26874d3a08a47ed) fix: acl rule error
 * [1a6f492ad](https://github.com/kubeovn/kube-ovn/commit/1a6f492ad603cdc1ac34992fb502f790d86c9fdb) fix: init node gw before run controller
 * [75c514a19](https://github.com/kubeovn/kube-ovn/commit/75c514a19a633c72068d094eae7fc31d25a5ec25) fix: external dns issues
 * [130688927](https://github.com/kubeovn/kube-ovn/commit/130688927a4282bb8c9c662f09a74bbefca77ef2) feat: use daemon ovn-nbctl to improve performance and cleanup unused dns code
 * [24cda418c](https://github.com/kubeovn/kube-ovn/commit/24cda418cd50af8173bff0e3698ddf7780bb1c53) Implement centralized gateway.
 * [890934f47](https://github.com/kubeovn/kube-ovn/commit/890934f473772c26453c49bca93a1fcb57dbc962) chore: migrate from bitbucket to github

### Contributors

 * MengxinLiu
 * Yan Zhu

## v0.2.0 (2019-04-15)

 * [adf655cb9](https://github.com/kubeovn/kube-ovn/commit/adf655cb95e57f4b9e9921dd1074dfa517a88fc5) remove dns from ls and bump new version
 * [ca21c6cb1](https://github.com/kubeovn/kube-ovn/commit/ca21c6cb1f50fb3b313f1104cadc5c0baf7deb73) make filter table forward chain default accept
 * [cd0ddf10a](https://github.com/kubeovn/kube-ovn/commit/cd0ddf10a39cf3e95bcff11dd9b0d37075968891) ipset exclude cluster service ip range
 * [1d753c8e1](https://github.com/kubeovn/kube-ovn/commit/1d753c8e1d189bb9a05500365be53618207b0e1c) fix: lb bugs
 * [cb91d9842](https://github.com/kubeovn/kube-ovn/commit/cb91d9842e9cf5fdfa9817a4da94c868cf786a6c) read cidr from ns annotation
 * [e99983329](https://github.com/kubeovn/kube-ovn/commit/e9998332991445ed4a9664071af1601fc6b57af6) fix: remove dns table from nodeswitch and remove unused other_config:namespace
 * [049cab2c1](https://github.com/kubeovn/kube-ovn/commit/049cab2c114f3fdca4b0b4b18fcd1a3515c2d855) fix pod has no ip
 * [170c3c634](https://github.com/kubeovn/kube-ovn/commit/170c3c63433cb75c8d522dcd7256e09bf059dcd4) Distributed gateway implement
 * [cebb8dfda](https://github.com/kubeovn/kube-ovn/commit/cebb8dfda9f7ee72a3b66d92066a60c9fe0a10ac) fix: clean lost interface.
 * [4367ba079](https://github.com/kubeovn/kube-ovn/commit/4367ba079c268d163b08c282af510ccc4ee0beb0) feat: support subnet isolation
 * [1fe8c916d](https://github.com/kubeovn/kube-ovn/commit/1fe8c916dcb6f678fa8dcde28ff570ce63a456c5) feat: support dynamic qos
 * [e04bc0939](https://github.com/kubeovn/kube-ovn/commit/e04bc09397a922e60e5b13be7192f80816624d98) fix: ovn restart issues
 * [014f1dcf5](https://github.com/kubeovn/kube-ovn/commit/014f1dcf531e8eb6be43a9d6d28e6cc952111f38) fix: ovn restart issues
 * [3e78ddc37](https://github.com/kubeovn/kube-ovn/commit/3e78ddc375a51a96bfb711a96d70912c10dafd60) fix: validate namespace switch annotations
 * [44eafc505](https://github.com/kubeovn/kube-ovn/commit/44eafc505a05bfebea49a7eaf8f09f98f4c7a885) fix lint && add docker build
 * [cb3e01a4e](https://github.com/kubeovn/kube-ovn/commit/cb3e01a4ef7c599ac78e40e802fd4a7346001dba) feat: update yaml, add readiness/liveness probe, add pass shell args
 * [004deefd9](https://github.com/kubeovn/kube-ovn/commit/004deefd9dd061e73d9a54ee2721afab4ee8ecf2) feat: support qos
 * [d37264e45](https://github.com/kubeovn/kube-ovn/commit/d37264e4547ef51e3bd533aba33525a2121aa0e6) feat: add simple gateway implementation

### Contributors

 * Mengxin Liu
 * MengxinLiu
 * Yan Zhu

