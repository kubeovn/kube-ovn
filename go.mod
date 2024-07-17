module github.com/kubeovn/kube-ovn

go 1.22.5

require (
	github.com/Microsoft/go-winio v0.6.1
	github.com/Microsoft/hcsshim v0.12.2
	github.com/alauda/felix v3.6.6-0.20201207121355-187332daf314+incompatible
	github.com/bhendo/go-powershell v0.0.0-20190719160123-219e7fb4e41e
	github.com/cenkalti/backoff/v4 v4.3.0
	github.com/cnf/structhash v0.0.0-20201127153200-e1b16c1ebc08
	github.com/containernetworking/cni v1.1.2
	github.com/containernetworking/plugins v1.4.1
	github.com/digitalocean/go-openvswitch v0.0.0-20240130171624-c0f7d42efe24
	github.com/docker/docker v26.0.2+incompatible
	github.com/emicklei/go-restful/v3 v3.12.0
	github.com/evanphx/json-patch/v5 v5.9.0
	github.com/go-logr/stdr v1.2.2
	github.com/k8snetworkplumbingwg/network-attachment-definition-client v1.4.0
	github.com/k8snetworkplumbingwg/sriovnet v1.2.0
	github.com/kubeovn/go-iptables v0.0.0-20230322103850-8619a8ab3dca
	github.com/kubeovn/gonetworkmanager/v2 v2.0.0-20230905082151-e28c4d73a589
	github.com/kubeovn/ovsdb v0.0.0-20240410091831-5dd26006c475
	github.com/mdlayher/arp v0.0.0-20220512170110-6706a2966875
	github.com/moby/sys/mountinfo v0.7.1
	github.com/onsi/ginkgo/v2 v2.19.0
	github.com/onsi/gomega v1.33.1
	github.com/osrg/gobgp/v3 v3.28.0
	github.com/ovn-org/libovsdb v0.0.0-20230711201130-6785b52d4020
	github.com/parnurzeal/gorequest v0.3.0
	github.com/prometheus-community/pro-bing v0.4.0
	github.com/prometheus/client_golang v1.18.0
	github.com/scylladb/go-set v1.0.2
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.9.0
	github.com/vishvananda/netlink v1.2.1-beta.2
	go.uber.org/mock v0.4.0
	golang.org/x/exp v0.0.0-20240716175740-e3f259677ff7
	golang.org/x/mod v0.19.0
	golang.org/x/sys v0.22.0
	golang.org/x/time v0.5.0
	google.golang.org/grpc v1.65.0
	google.golang.org/protobuf v1.34.2
	gopkg.in/k8snetworkplumbingwg/multus-cni.v4 v4.0.2
	k8s.io/api v0.27.16
	k8s.io/apimachinery v0.27.16
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/klog/v2 v2.100.1
	k8s.io/kubernetes v1.27.16
	k8s.io/sample-controller v0.27.16
	k8s.io/utils v0.0.0-20240711033017-18e509b52bc8
	kubevirt.io/client-go v0.58.1
	sigs.k8s.io/controller-runtime v0.15.3
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/cenkalti/hub v1.0.2 // indirect
	github.com/cenkalti/rpc2 v1.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containerd/cgroups/v3 v3.0.3 // indirect
	github.com/containerd/errdefs v0.1.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/coreos/prometheus-operator v0.38.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dgryski/go-farm v0.0.0-20200201041132-a6ae2369ad13 // indirect
	github.com/eapache/channels v1.1.0 // indirect
	github.com/eapache/queue v1.1.0 // indirect
	github.com/elazarl/goproxy v0.0.0-20230731152917-f99041a5c027 // indirect
	github.com/evanphx/json-patch v5.6.0+incompatible // indirect
	github.com/frankban/quicktest v1.14.5 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-ini/ini v1.67.0 // indirect
	github.com/go-kit/kit v0.12.0 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.6.0 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/glog v1.2.1 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic v0.6.9 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20240424215950-a892ee059fd6 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gopherjs/gopherjs v1.17.2 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/hashicorp/go-version v1.6.0 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/imdario/mergo v0.3.15 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/josharian/native v1.1.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/juju/errors v1.0.0 // indirect
	github.com/k-sone/critbitgo v1.4.0 // indirect
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0 // indirect
	github.com/kelseyhightower/envconfig v1.4.0 // indirect
	github.com/kubernetes-csi/external-snapshotter/client/v4 v4.2.0 // indirect
	github.com/magiconair/properties v1.8.7 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/mdlayher/ethernet v0.0.0-20220221185849-529eae5b6118 // indirect
	github.com/mdlayher/packet v1.1.2 // indirect
	github.com/mdlayher/socket v0.5.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/moby/spdystream v0.2.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/moul/http2curl v1.0.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/openshift/api v0.0.0-20221103085154-ea838af1820e // indirect
	github.com/openshift/client-go v3.9.0+incompatible // indirect
	github.com/openshift/custom-resource-status v1.1.2 // indirect
	github.com/pborman/uuid v1.2.1 // indirect
	github.com/pelletier/go-toml/v2 v2.0.8 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/projectcalico/go-json v0.0.0-20161128004156-6219dc7339ba // indirect
	github.com/projectcalico/go-yaml-wrapper v0.0.0-20191112210931-090425220c54 // indirect
	github.com/projectcalico/libcalico-go v0.0.0-20190305235709-3d935c3b8b86 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/smartystreets/assertions v1.13.0 // indirect
	github.com/spf13/afero v1.11.0 // indirect
	github.com/spf13/cast v1.5.1 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/spf13/viper v1.16.0 // indirect
	github.com/subosito/gotenv v1.4.2 // indirect
	github.com/vishvananda/netns v0.0.4 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/net v0.27.0 // indirect
	golang.org/x/oauth2 v0.21.0 // indirect
	golang.org/x/sync v0.7.0 // indirect
	golang.org/x/term v0.22.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	golang.org/x/tools v0.23.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240701130421-f6361c86f094 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gotest.tools/v3 v3.0.2 // indirect
	k8s.io/apiextensions-apiserver v0.27.16 // indirect
	k8s.io/component-base v0.27.16 // indirect
	k8s.io/kube-openapi v0.0.0-20230515203736-54b630e78af5 // indirect
	kubevirt.io/api v0.58.1 // indirect
	kubevirt.io/containerized-data-importer-api v1.55.2 // indirect
	kubevirt.io/controller-lifecycle-operator-sdk/api v0.2.4 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)

replace (
	github.com/alauda/felix => github.com/kubeovn/felix v0.0.0-20220325073257-c8a0f705d139
	github.com/mdlayher/arp => github.com/kubeovn/arp v0.0.0-20230101053045-8a0772d9c34c
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20221107163225-3335a34a1d24
	github.com/ovn-org/libovsdb => github.com/kubeovn/libovsdb v0.0.0-20230517064328-9d5a1383643f
	github.com/vishvananda/netlink => github.com/kubeovn/netlink v0.0.0-20230322092337-960188369daf
	k8s.io/api => k8s.io/api v0.27.16
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.27.16
	k8s.io/apimachinery => k8s.io/apimachinery v0.27.16
	k8s.io/apiserver => k8s.io/apiserver v0.27.16
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.27.16
	k8s.io/client-go => k8s.io/client-go v0.27.16
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.27.16
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.27.16
	k8s.io/code-generator => k8s.io/code-generator v0.27.16
	k8s.io/component-base => k8s.io/component-base v0.27.16
	k8s.io/component-helpers => k8s.io/component-helpers v0.27.16
	k8s.io/controller-manager => k8s.io/controller-manager v0.27.16
	k8s.io/cri-api => k8s.io/cri-api v0.27.16
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.27.16
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.27.16
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.27.16
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.27.16
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.27.16
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.27.16
	k8s.io/kubectl => k8s.io/kubectl v0.27.16
	k8s.io/kubelet => k8s.io/kubelet v0.27.16
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.27.16
	k8s.io/metrics => k8s.io/metrics v0.27.16
	k8s.io/mount-utils => k8s.io/mount-utils v0.27.16
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.27.16
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.27.16
	kubevirt.io/client-go => github.com/kubeovn/kubevirt-client-go v0.0.0-20230517062539-8dd832f39ec5
)
