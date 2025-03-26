module github.com/kubeovn/kube-ovn

go 1.23.7

require (
	github.com/Microsoft/go-winio v0.6.2
	github.com/Microsoft/hcsshim v0.12.5
	github.com/bhendo/go-powershell v0.0.0-20190719160123-219e7fb4e41e
	github.com/cenkalti/backoff/v4 v4.3.0
	github.com/cnf/structhash v0.0.0-20201127153200-e1b16c1ebc08
	github.com/containernetworking/cni v1.2.3
	github.com/containernetworking/plugins v1.5.1
	github.com/digitalocean/go-openvswitch v0.0.0-20240130171624-c0f7d42efe24
	github.com/docker/docker v27.1.1+incompatible
	github.com/emicklei/go-restful/v3 v3.12.1
	github.com/evanphx/json-patch/v5 v5.9.11
	github.com/go-logr/logr v1.4.2
	github.com/go-logr/stdr v1.2.2
	github.com/google/uuid v1.6.0
	github.com/k8snetworkplumbingwg/network-attachment-definition-client v1.7.1
	github.com/k8snetworkplumbingwg/sriovnet v1.2.0
	github.com/kubeovn/felix v0.0.0-20240506083207-ed396be1b6cf
	github.com/kubeovn/go-iptables v0.0.0-20230322103850-8619a8ab3dca
	github.com/kubeovn/gonetworkmanager/v2 v2.0.0-20230905082151-e28c4d73a589
	github.com/kubeovn/ovsdb v0.0.0-20240410091831-5dd26006c475
	github.com/mdlayher/arp v0.0.0-20220512170110-6706a2966875
	github.com/moby/sys/mountinfo v0.7.2
	github.com/onsi/ginkgo/v2 v2.20.0
	github.com/onsi/gomega v1.34.1
	github.com/osrg/gobgp/v3 v3.29.0
	github.com/ovn-org/libovsdb v0.7.0
	github.com/parnurzeal/gorequest v0.3.0
	github.com/prometheus-community/pro-bing v0.4.1
	github.com/prometheus/client_golang v1.18.0
	github.com/scylladb/go-set v1.0.2
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.9.0
	github.com/vishvananda/netlink v1.2.1-beta.2.0.20240713210050-d13535d71ed3
	go.uber.org/mock v0.4.0
	golang.org/x/exp v0.0.0-20250305212735-054e65f0b394
	golang.org/x/mod v0.24.0
	golang.org/x/sys v0.31.0
	golang.org/x/time v0.11.0
	google.golang.org/grpc v1.66.2
	google.golang.org/protobuf v1.34.2
	gopkg.in/k8snetworkplumbingwg/multus-cni.v4 v4.1.0
	k8s.io/api v0.30.11
	k8s.io/apimachinery v0.30.11
	k8s.io/client-go v1.5.2
	k8s.io/klog/v2 v2.130.1
	k8s.io/kubernetes v1.30.11
	k8s.io/utils v0.0.0-20241210054802-24370beab758
	kubevirt.io/client-go v1.3.0
	sigs.k8s.io/controller-runtime v0.18.7
)

require (
	github.com/antlr/antlr4/runtime/Go/antlr/v4 v4.0.0-20230305170008-8188dc5388df // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/cenkalti/hub v1.0.2 // indirect
	github.com/cenkalti/rpc2 v1.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containerd/cgroups/v3 v3.0.3 // indirect
	github.com/containerd/errdefs v0.1.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dgryski/go-farm v0.0.0-20200201041132-a6ae2369ad13 // indirect
	github.com/eapache/channels v1.1.0 // indirect
	github.com/eapache/queue v1.1.0 // indirect
	github.com/elazarl/goproxy v0.0.0-20231117061959-7cc037d33fb5 // indirect
	github.com/evanphx/json-patch v5.7.0+incompatible // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/go-ini/ini v1.67.0 // indirect
	github.com/go-kit/kit v0.13.0 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.6.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/glog v1.2.4 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/cel-go v0.17.8 // indirect
	github.com/google/gnostic-models v0.6.9-0.20230804172637-c7be7c783f49 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20240727154555-813a5fbdbec8 // indirect
	github.com/gopherjs/gopherjs v1.17.2 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.22.0 // indirect
	github.com/hashicorp/go-version v1.7.0 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
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
	github.com/mdlayher/ethernet v0.0.0-20220221185849-529eae5b6118 // indirect
	github.com/mdlayher/packet v1.1.2 // indirect
	github.com/mdlayher/socket v0.5.1 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/moby/spdystream v0.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/moul/http2curl v1.0.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/openshift/api v0.0.0-20231207204216-5efc6fca4b2d // indirect
	github.com/openshift/client-go v3.9.0+incompatible // indirect
	github.com/openshift/custom-resource-status v1.1.2 // indirect
	github.com/pelletier/go-toml/v2 v2.2.2 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/projectcalico/go-json v0.0.0-20161128004156-6219dc7339ba // indirect
	github.com/projectcalico/go-yaml-wrapper v0.0.0-20191112210931-090425220c54 // indirect
	github.com/projectcalico/libcalico-go v0.0.0-20190305235709-3d935c3b8b86 // indirect
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.68.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.47.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/sagikazarmark/locafero v0.6.0 // indirect
	github.com/sagikazarmark/slog-shim v0.1.0 // indirect
	github.com/smartystreets/assertions v1.13.0 // indirect
	github.com/smartystreets/goconvey v1.6.4 // indirect
	github.com/sourcegraph/conc v0.3.0 // indirect
	github.com/spf13/afero v1.11.0 // indirect
	github.com/spf13/cast v1.7.0 // indirect
	github.com/spf13/viper v1.19.0 // indirect
	github.com/stoewer/go-strcase v1.3.0 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/vishvananda/netns v0.0.4 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.55.0 // indirect
	go.opentelemetry.io/otel v1.30.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.30.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.30.0 // indirect
	go.opentelemetry.io/otel/metric v1.30.0 // indirect
	go.opentelemetry.io/otel/sdk v1.30.0 // indirect
	go.opentelemetry.io/otel/trace v1.30.0 // indirect
	go.opentelemetry.io/proto/otlp v1.3.1 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/net v0.37.0 // indirect
	golang.org/x/oauth2 v0.28.0 // indirect
	golang.org/x/sync v0.12.0 // indirect
	golang.org/x/term v0.30.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	golang.org/x/tools v0.31.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240903143218-8af14fe29dc1 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240903143218-8af14fe29dc1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gotest.tools/v3 v3.0.2 // indirect
	k8s.io/apiextensions-apiserver v0.30.11 // indirect
	k8s.io/apiserver v0.30.11 // indirect
	k8s.io/component-base v0.30.11 // indirect
	k8s.io/kube-openapi v0.30.0 // indirect
	kubevirt.io/api v1.3.0 // indirect
	kubevirt.io/containerized-data-importer-api v1.58.1 // indirect
	kubevirt.io/controller-lifecycle-operator-sdk/api v0.0.0-20220329064328-f3cc58c6ed90 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.30.4 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.5.0 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)

replace (
	github.com/mdlayher/arp => github.com/kubeovn/arp v0.0.0-20240218024213-d9612a263f68
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.1
	github.com/ovn-org/libovsdb => github.com/kubeovn/libovsdb v0.0.0-20240814054845-978196448fb2
	k8s.io/api => k8s.io/api v0.30.11
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.30.11
	k8s.io/apimachinery => k8s.io/apimachinery v0.30.11
	k8s.io/apiserver => k8s.io/apiserver v0.30.11
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.30.11
	k8s.io/client-go => k8s.io/client-go v0.30.11
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.30.11
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.30.11
	k8s.io/code-generator => k8s.io/code-generator v0.30.11
	k8s.io/component-base => k8s.io/component-base v0.30.11
	k8s.io/component-helpers => k8s.io/component-helpers v0.30.11
	k8s.io/controller-manager => k8s.io/controller-manager v0.30.11
	k8s.io/cri-api => k8s.io/cri-api v0.30.11
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.30.11
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.30.11
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.30.11
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.30.11
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20240430033511-f0e62f92d13f
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.30.11
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.30.11
	k8s.io/kubectl => k8s.io/kubectl v0.30.11
	k8s.io/kubelet => k8s.io/kubelet v0.30.11
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.30.11
	k8s.io/metrics => k8s.io/metrics v0.30.11
	k8s.io/mount-utils => k8s.io/mount-utils v0.30.11
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.30.11
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.30.11
)
