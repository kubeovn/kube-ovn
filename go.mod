module github.com/kubeovn/kube-ovn

go 1.24.3

require (
	github.com/Microsoft/go-winio v0.6.2
	github.com/Microsoft/hcsshim v0.13.0
	github.com/bhendo/go-powershell v0.0.0-20190719160123-219e7fb4e41e
	github.com/brianvoe/gofakeit/v7 v7.2.1
	github.com/cenkalti/backoff/v4 v4.3.0
	github.com/cnf/structhash v0.0.0-20201127153200-e1b16c1ebc08
	github.com/containerd/containerd v1.7.27
	github.com/containernetworking/cni v1.3.0
	github.com/containernetworking/plugins v1.7.1
	github.com/digitalocean/go-openvswitch v0.0.0-20241021184246-19e734367535
	github.com/docker/docker v28.2.2+incompatible
	github.com/emicklei/go-restful/v3 v3.12.2
	github.com/evanphx/json-patch/v5 v5.9.11
	github.com/go-logr/logr v1.4.3
	github.com/go-logr/stdr v1.2.2
	github.com/google/uuid v1.6.0
	github.com/httprunner/httprunner/v4 v4.3.7-0.20240124083022-402b74876a59
	github.com/k8snetworkplumbingwg/network-attachment-definition-client v1.7.6
	github.com/k8snetworkplumbingwg/sriovnet v1.2.0
	github.com/kubeovn/felix v0.0.0-20240506083207-ed396be1b6cf
	github.com/kubeovn/go-iptables v0.0.0-20230322103850-8619a8ab3dca
	github.com/kubeovn/gonetworkmanager/v3 v3.0.0-20250410050455-ce7c8d9ddfb1
	github.com/kubeovn/ovsdb v0.0.0-20240410091831-5dd26006c475
	github.com/mdlayher/arp v0.0.0-20220512170110-6706a2966875
	github.com/moby/sys/mountinfo v0.7.2
	github.com/onsi/ginkgo/v2 v2.23.4
	github.com/onsi/gomega v1.37.0
	github.com/osrg/gobgp/v3 v3.37.0
	github.com/ovn-org/libovsdb v0.7.0
	github.com/parnurzeal/gorequest v0.3.0
	github.com/prometheus-community/pro-bing v0.7.0
	github.com/prometheus/client_golang v1.22.0
	github.com/puzpuzpuz/xsync/v3 v3.5.1
	github.com/rs/zerolog v1.34.0
	github.com/scylladb/go-set v1.0.2
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/pflag v1.0.6
	github.com/stretchr/testify v1.10.0
	github.com/vishvananda/netlink v1.3.1
	go.uber.org/mock v0.5.2
	go.universe.tf/metallb v0.14.9
	golang.org/x/mod v0.24.0
	golang.org/x/sys v0.33.0
	golang.org/x/time v0.11.0
	golang.org/x/tools v0.33.0
	google.golang.org/grpc v1.72.2
	google.golang.org/protobuf v1.36.6
	gopkg.in/k8snetworkplumbingwg/multus-cni.v4 v4.2.0
	k8s.io/api v0.32.5
	k8s.io/apiextensions-apiserver v0.32.5
	k8s.io/apimachinery v0.32.5
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/component-base v0.32.5
	k8s.io/klog/v2 v2.130.1
	k8s.io/kube-aggregator v0.32.5
	k8s.io/kubectl v0.32.5
	k8s.io/kubernetes v1.32.5
	k8s.io/pod-security-admission v0.32.5
	k8s.io/utils v0.0.0-20250502105355-0f33e8f1c979
	kernel.org/pub/linux/libs/security/libcap/cap v1.2.76
	kubevirt.io/api v1.5.1
	kubevirt.io/client-go v1.5.1
	sigs.k8s.io/controller-runtime v0.20.4
	sigs.k8s.io/network-policy-api v0.1.5
)

require (
	cel.dev/expr v0.24.0 // indirect
	cloud.google.com/go/compute/metadata v0.7.0 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/JeffAshton/win_pdh v0.0.0-20161109143554-76bb4ee9f0ab // indirect
	github.com/MakeNowJust/heredoc v1.0.0 // indirect
	github.com/Microsoft/hnslib v0.1.1 // indirect
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/andybalholm/brotli v1.1.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/armon/circbuf v0.0.0-20190214190532-5111143e8da2 // indirect
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/cenkalti/hub v1.0.2 // indirect
	github.com/cenkalti/rpc2 v1.0.4 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/chai2010/gettext-go v1.0.3 // indirect
	github.com/container-storage-interface/spec v1.11.0 // indirect
	github.com/containerd/cgroups/v3 v3.0.5 // indirect
	github.com/containerd/containerd/api v1.9.0 // indirect
	github.com/containerd/continuity v0.4.5 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/ttrpc v1.2.7 // indirect
	github.com/containerd/typeurl/v2 v2.2.3 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/cyphar/filepath-securejoin v0.4.1 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/denisbrodbeck/machineid v1.0.1 // indirect
	github.com/dgryski/go-farm v0.0.0-20240924180020-3414d57e47da // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-connections v0.5.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/eapache/channels v1.1.0 // indirect
	github.com/eapache/queue v1.1.0 // indirect
	github.com/elazarl/goproxy v1.7.2 // indirect
	github.com/euank/go-kmsg-parser v2.0.0+incompatible // indirect
	github.com/evanphx/json-patch v5.9.11+incompatible // indirect
	github.com/exponent-io/jsonpath v0.0.0-20210407135951-1de76d718b3f // indirect
	github.com/fatih/camelcase v1.0.0 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/fxamacker/cbor/v2 v2.8.0 // indirect
	github.com/getsentry/sentry-go v0.33.0 // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-kit/kit v0.13.0 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.6.0 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.1 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.1 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.2.1 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/cadvisor v0.52.1 // indirect
	github.com/google/cel-go v0.22.1 // indirect
	github.com/google/gnostic-models v0.6.9 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20250501235452-c0086092b71a // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.4.0 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.3 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-plugin v1.6.3 // indirect
	github.com/hashicorp/go-version v1.7.0 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/httprunner/funplugin v0.5.5 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jinzhu/copier v0.4.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/josharian/native v1.1.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/juju/errors v1.0.0 // indirect
	github.com/k-sone/critbitgo v1.4.0 // indirect
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0 // indirect
	github.com/karrick/godirwalk v1.17.0 // indirect
	github.com/kelseyhightower/envconfig v1.4.0 // indirect
	github.com/kubeovn/dbus/v5 v5.0.0-20250410044920-11a753c7a13f // indirect
	github.com/kubernetes-csi/external-snapshotter/client/v4 v4.2.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/libopenstorage/openstorage v1.0.0 // indirect
	github.com/liggitt/tabwriter v0.0.0-20181228230101-89fcab3d43de // indirect
	github.com/lunixbochs/struc v0.0.0-20241101090106-8d528fa2c543 // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/maja42/goval v1.6.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mdlayher/ethernet v0.0.0-20220221185849-529eae5b6118 // indirect
	github.com/mdlayher/packet v1.1.2 // indirect
	github.com/mdlayher/socket v0.5.1 // indirect
	github.com/mistifyio/go-zfs v2.1.2-0.20190413222219-f784269be439+incompatible // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/spdystream v0.5.0 // indirect
	github.com/moby/sys/atomicwriter v0.1.0 // indirect
	github.com/moby/sys/sequential v0.6.0 // indirect
	github.com/moby/sys/symlink v0.3.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/moul/http2curl v1.0.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/oklog/run v1.1.0 // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/opencontainers/cgroups v0.0.2 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/opencontainers/runc v1.2.6 // indirect
	github.com/opencontainers/runtime-spec v1.2.1 // indirect
	github.com/opencontainers/selinux v1.12.0 // indirect
	github.com/openshift/api v0.0.0 // indirect
	github.com/openshift/client-go v3.9.0+incompatible // indirect
	github.com/openshift/custom-resource-status v1.1.2 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/projectcalico/go-json v0.0.0-20161128004156-6219dc7339ba // indirect
	github.com/projectcalico/go-yaml-wrapper v0.0.0-20191112210931-090425220c54 // indirect
	github.com/projectcalico/libcalico-go v0.0.0-20190305235709-3d935c3b8b86 // indirect
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.82.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.63.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sagikazarmark/locafero v0.9.0 // indirect
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/shirou/gopsutil v3.21.11+incompatible // indirect
	github.com/smartystreets/goconvey v1.8.1 // indirect
	github.com/sourcegraph/conc v0.3.0 // indirect
	github.com/spf13/afero v1.14.0 // indirect
	github.com/spf13/cast v1.8.0 // indirect
	github.com/spf13/cobra v1.9.1 // indirect
	github.com/spf13/viper v1.20.1 // indirect
	github.com/stoewer/go-strcase v1.3.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tklauser/go-sysconf v0.3.15 // indirect
	github.com/tklauser/numcpus v0.10.0 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.etcd.io/etcd/api/v3 v3.6.0 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.6.0 // indirect
	go.etcd.io/etcd/client/v3 v3.6.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/github.com/emicklei/go-restful/otelrestful v0.60.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.60.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.60.0 // indirect
	go.opentelemetry.io/otel v1.35.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.35.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.35.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.35.0 // indirect
	go.opentelemetry.io/otel/metric v1.35.0 // indirect
	go.opentelemetry.io/otel/sdk v1.35.0 // indirect
	go.opentelemetry.io/otel/trace v1.35.0 // indirect
	go.opentelemetry.io/proto/otlp v1.6.0 // indirect
	go.uber.org/automaxprocs v1.6.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	gocv.io/x/gocv v0.41.0 // indirect
	golang.org/x/crypto v0.38.0 // indirect
	golang.org/x/exp v0.0.0-20250506013437-ce4c2cf36ca6 // indirect
	golang.org/x/net v0.40.0 // indirect
	golang.org/x/oauth2 v0.30.0 // indirect
	golang.org/x/sync v0.14.0 // indirect
	golang.org/x/term v0.32.0 // indirect
	golang.org/x/text v0.25.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.5.0 // indirect
	google.golang.org/genproto v0.0.0-20250512202823-5a2f75b736a9 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250512202823-5a2f75b736a9 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250512202823-5a2f75b736a9 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	howett.net/plist v1.0.1 // indirect
	k8s.io/apiserver v0.32.5 // indirect
	k8s.io/cli-runtime v0.32.5 // indirect
	k8s.io/cloud-provider v0.32.5 // indirect
	k8s.io/cluster-bootstrap v0.32.5 // indirect
	k8s.io/component-helpers v0.32.5 // indirect
	k8s.io/controller-manager v0.32.5 // indirect
	k8s.io/cri-api v0.32.5 // indirect
	k8s.io/cri-client v0.32.5 // indirect
	k8s.io/csi-translation-lib v0.32.5 // indirect
	k8s.io/dynamic-resource-allocation v0.32.5 // indirect
	k8s.io/kms v0.32.5 // indirect
	k8s.io/kube-openapi v0.32.5 // indirect
	k8s.io/kube-scheduler v0.32.5 // indirect
	k8s.io/kubelet v0.32.5 // indirect
	k8s.io/mount-utils v0.32.5 // indirect
	kernel.org/pub/linux/libs/security/libcap/psx v1.2.76 // indirect
	kubevirt.io/containerized-data-importer-api v1.62.0 // indirect
	kubevirt.io/controller-lifecycle-operator-sdk/api v0.2.4 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.32.1 // indirect
	sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect
	sigs.k8s.io/kustomize/api v0.19.0 // indirect
	sigs.k8s.io/kustomize/kyaml v0.19.0 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.7.0 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)

replace (
	github.com/mdlayher/arp => github.com/kubeovn/arp v0.0.0-20240218024213-d9612a263f68
	github.com/openshift/api => github.com/openshift/api v0.0.0-20191219222812-2987a591a72c
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20210112165513-ebc401615f47
	github.com/ovn-org/libovsdb => github.com/kubeovn/libovsdb v0.0.0-20241120032411-25ef1bbc83a5
	k8s.io/api => k8s.io/api v0.32.5
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.32.5
	k8s.io/apimachinery => k8s.io/apimachinery v0.32.5
	k8s.io/apiserver => k8s.io/apiserver v0.32.5
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.32.5
	k8s.io/client-go => k8s.io/client-go v0.32.5
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.32.5
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.32.5
	k8s.io/code-generator => k8s.io/code-generator v0.32.5
	k8s.io/component-base => k8s.io/component-base v0.32.5
	k8s.io/component-helpers => k8s.io/component-helpers v0.32.5
	k8s.io/controller-manager => k8s.io/controller-manager v0.32.5
	k8s.io/cri-api => k8s.io/cri-api v0.32.5
	k8s.io/cri-client => k8s.io/cri-client v0.32.5
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.32.5
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.32.5
	k8s.io/endpointslice => k8s.io/endpointslice v0.32.5
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.32.5
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.32.5
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20241212222426-2c72e554b1e7
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.32.5
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.32.5
	k8s.io/kubectl => k8s.io/kubectl v0.32.5
	k8s.io/kubelet => k8s.io/kubelet v0.32.5
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.32.5
	k8s.io/metrics => k8s.io/metrics v0.32.5
	k8s.io/mount-utils => k8s.io/mount-utils v0.32.5
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.32.5
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.32.5
	kubevirt.io/client-go => github.com/kubeovn/kubevirt-client-go v0.0.0-20250507014510-dc51721a96f1
)
