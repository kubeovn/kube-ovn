module github.com/kubeovn/kube-ovn

go 1.25.6

require (
	github.com/brianvoe/gofakeit/v7 v7.14.0
	github.com/cenkalti/backoff/v5 v5.0.3
	github.com/cert-manager/cert-manager v1.19.2
	github.com/cnf/structhash v0.0.0-20250313080605-df4c6cc74a9a
	github.com/containerd/containerd/v2 v2.2.1
	github.com/containerd/nerdctl/v2 v2.2.1
	github.com/containernetworking/cni v1.3.0
	github.com/containernetworking/plugins v1.9.0
	github.com/digitalocean/go-openvswitch v0.0.0-20251118231545-85aec29b8ee6
	github.com/emicklei/go-restful/v3 v3.13.0
	github.com/evanphx/json-patch/v5 v5.9.11
	github.com/go-logr/logr v1.4.3
	github.com/go-logr/stdr v1.2.2
	github.com/go-logr/zapr v1.3.0
	github.com/google/gopacket v1.1.19
	github.com/k8snetworkplumbingwg/network-attachment-definition-client v1.7.7
	github.com/k8snetworkplumbingwg/sriovnet v1.2.0
	github.com/kubeovn/dbus/v5 v5.0.0-20250410044920-11a753c7a13f
	github.com/kubeovn/felix v0.0.0-20240506083207-ed396be1b6cf
	github.com/kubeovn/go-iptables v0.0.0-20230322103850-8619a8ab3dca
	github.com/kubeovn/gonetworkmanager/v3 v3.0.0-20250410045132-f48faac0d315
	github.com/kubeovn/ovsdb v0.0.0-20240410091831-5dd26006c475
	github.com/mdlayher/arp v0.0.0-20220512170110-6706a2966875
	github.com/mdlayher/ndp v1.1.0
	github.com/mdlayher/netx v0.0.0-20230430222610-7e21880baee8
	github.com/mdlayher/packet v1.1.2
	github.com/moby/sys/mountinfo v0.7.2
	github.com/onsi/ginkgo/v2 v2.27.3
	github.com/onsi/gomega v1.38.3
	github.com/osrg/gobgp/v4 v4.1.0
	github.com/ovn-kubernetes/libovsdb v0.8.1
	github.com/parnurzeal/gorequest v0.3.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus-community/pro-bing v0.7.0
	github.com/prometheus/client_golang v1.23.2
	github.com/puzpuzpuz/xsync/v4 v4.2.0
	github.com/scylladb/go-set v1.0.2
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/pflag v1.0.10
	github.com/stretchr/testify v1.11.1
	github.com/vishvananda/netlink v1.3.1
	go.uber.org/mock v0.6.0
	go.uber.org/zap v1.27.1
	golang.org/x/mod v0.32.0
	golang.org/x/net v0.49.0
	golang.org/x/sys v0.40.0
	golang.org/x/time v0.14.0
	golang.org/x/tools v0.41.0
	google.golang.org/grpc v1.78.0
	gopkg.in/k8snetworkplumbingwg/multus-cni.v4 v4.2.3
	k8s.io/api v0.34.3
	k8s.io/apiextensions-apiserver v0.34.3
	k8s.io/apimachinery v0.34.3
	k8s.io/apiserver v0.34.3
	k8s.io/client-go v0.34.3
	k8s.io/klog/v2 v2.130.1
	k8s.io/kube-aggregator v0.34.3
	k8s.io/kubernetes v1.34.3
	k8s.io/utils v0.0.0-20251002143259-bc988d571ff4
	kernel.org/pub/linux/libs/security/libcap/cap v1.2.77
	kubevirt.io/api v1.7.0
	kubevirt.io/client-go v1.7.0
	sigs.k8s.io/controller-runtime v0.22.4
	sigs.k8s.io/network-policy-api v0.1.8-0.20251209142732-3910463a5686
)

require (
	cel.dev/expr v0.24.0 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/Microsoft/hcsshim v0.14.0-rc.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/hub v1.0.2 // indirect
	github.com/cenkalti/rpc2 v1.0.5 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containerd/cgroups/v3 v3.1.2 // indirect
	github.com/containerd/containerd/api v1.10.0 // indirect
	github.com/containerd/continuity v0.4.5 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/typeurl/v2 v2.2.3 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dgryski/go-farm v0.0.0-20240924180020-3414d57e47da // indirect
	github.com/eapache/channels v1.1.0 // indirect
	github.com/eapache/queue v1.1.0 // indirect
	github.com/elazarl/goproxy v1.7.2 // indirect
	github.com/evanphx/json-patch v5.9.11+incompatible // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.11 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.6.1 // indirect
	github.com/go-openapi/jsonpointer v0.22.4 // indirect
	github.com/go-openapi/jsonreference v0.21.4 // indirect
	github.com/go-openapi/swag v0.25.4 // indirect
	github.com/go-openapi/swag/cmdutils v0.25.4 // indirect
	github.com/go-openapi/swag/conv v0.25.4 // indirect
	github.com/go-openapi/swag/fileutils v0.25.4 // indirect
	github.com/go-openapi/swag/jsonname v0.25.4 // indirect
	github.com/go-openapi/swag/jsonutils v0.25.4 // indirect
	github.com/go-openapi/swag/loading v0.25.4 // indirect
	github.com/go-openapi/swag/mangling v0.25.4 // indirect
	github.com/go-openapi/swag/netutils v0.25.4 // indirect
	github.com/go-openapi/swag/stringutils v0.25.4 // indirect
	github.com/go-openapi/swag/typeutils v0.25.4 // indirect
	github.com/go-openapi/swag/yamlutils v0.25.4 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.28.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/godbus/dbus/v5 v5.2.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/cel-go v0.26.0 // indirect
	github.com/google/gnostic-models v0.7.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20251114195745-4902fdda35c8 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.3 // indirect
	github.com/hashicorp/go-version v1.8.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/native v1.1.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/k-sone/critbitgo v1.4.0 // indirect
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0 // indirect
	github.com/kelseyhightower/envconfig v1.4.0 // indirect
	github.com/kubernetes-csi/external-snapshotter/client/v4 v4.2.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mdlayher/ethernet v0.0.0-20220221185849-529eae5b6118 // indirect
	github.com/mdlayher/socket v0.5.1 // indirect
	github.com/moby/spdystream v0.5.0 // indirect
	github.com/moby/sys/symlink v0.3.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/moul/http2curl v1.0.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/openshift/api v0.0.0 // indirect
	github.com/openshift/client-go v3.9.0+incompatible // indirect
	github.com/openshift/custom-resource-status v1.1.2 // indirect
	github.com/orcaman/concurrent-map/v2 v2.0.1 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/projectcalico/go-json v0.0.0-20161128004156-6219dc7339ba // indirect
	github.com/projectcalico/go-yaml-wrapper v0.0.0-20191112210931-090425220c54 // indirect
	github.com/projectcalico/libcalico-go v0.0.0-20190305235709-3d935c3b8b86 // indirect
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.83.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.4 // indirect
	github.com/prometheus/procfs v0.19.2 // indirect
	github.com/sagikazarmark/locafero v0.12.0 // indirect
	github.com/segmentio/fasthash v1.0.3 // indirect
	github.com/smartystreets/goconvey v1.8.1 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/cobra v1.10.2 // indirect
	github.com/spf13/viper v1.21.0 // indirect
	github.com/stoewer/go-strcase v1.3.1 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.64.0 // indirect
	go.opentelemetry.io/otel v1.39.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.39.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.39.0 // indirect
	go.opentelemetry.io/otel/metric v1.39.0 // indirect
	go.opentelemetry.io/otel/sdk v1.39.0 // indirect
	go.opentelemetry.io/otel/trace v1.39.0 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.47.0 // indirect
	golang.org/x/exp v0.0.0-20251209150349-8475f28825e9 // indirect
	golang.org/x/oauth2 v0.34.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/term v0.39.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.5.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/code-generator v0.34.3 // indirect
	k8s.io/component-base v0.34.3 // indirect
	k8s.io/gengo/v2 v2.0.0-20250820003526-c297c0c1eb9d // indirect
	k8s.io/kube-openapi v0.34.3 // indirect
	kernel.org/pub/linux/libs/security/libcap/psx v1.2.77 // indirect
	kubevirt.io/containerized-data-importer-api v1.63.1 // indirect
	kubevirt.io/controller-lifecycle-operator-sdk/api v0.2.4 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.34.0 // indirect
	sigs.k8s.io/gateway-api v1.4.1 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.1 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

replace (
	github.com/mdlayher/arp => github.com/kubeovn/arp v0.0.0-20240218024213-d9612a263f68
	github.com/openshift/api => github.com/openshift/api v0.0.0-20191219222812-2987a591a72c
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20210112165513-ebc401615f47
	github.com/ovn-kubernetes/libovsdb => github.com/kubeovn/libovsdb v0.0.0-20251212071713-cb1c2bc5d43e
	k8s.io/api => k8s.io/api v0.34.3
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.34.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.34.3
	k8s.io/apiserver => k8s.io/apiserver v0.34.3
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.34.3
	k8s.io/client-go => k8s.io/client-go v0.34.3
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.34.3
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.34.3
	k8s.io/code-generator => k8s.io/code-generator v0.34.3
	k8s.io/component-base => k8s.io/component-base v0.34.3
	k8s.io/component-helpers => k8s.io/component-helpers v0.34.3
	k8s.io/controller-manager => k8s.io/controller-manager v0.34.3
	k8s.io/cri-api => k8s.io/cri-api v0.34.3
	k8s.io/cri-client => k8s.io/cri-client v0.34.3
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.34.3
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.34.3
	k8s.io/endpointslice => k8s.io/endpointslice v0.34.3
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.34.3
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.34.3
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20250710124328-f3f2b991d03b
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.34.3
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.34.3
	k8s.io/kubectl => k8s.io/kubectl v0.34.3
	k8s.io/kubelet => k8s.io/kubelet v0.34.3
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.34.3
	k8s.io/metrics => k8s.io/metrics v0.34.3
	k8s.io/mount-utils => k8s.io/mount-utils v0.34.3
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.34.3
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.34.3
	kubevirt.io/client-go => github.com/kubeovn/kubevirt-client-go v0.0.0-20251128080558-ab7d8b8b5a66
)

tool (
	github.com/kubeovn/kube-ovn/tools/modernize
	github.com/ovn-kubernetes/libovsdb/cmd/modelgen
	go.uber.org/mock/mockgen
	k8s.io/code-generator/cmd/applyconfiguration-gen
	k8s.io/code-generator/cmd/client-gen
	k8s.io/code-generator/cmd/conversion-gen
	k8s.io/code-generator/cmd/deepcopy-gen
	k8s.io/code-generator/cmd/defaulter-gen
	k8s.io/code-generator/cmd/go-to-protobuf
	k8s.io/code-generator/cmd/informer-gen
	k8s.io/code-generator/cmd/lister-gen
	k8s.io/code-generator/cmd/register-gen
	k8s.io/kube-openapi/cmd/openapi-gen
)
