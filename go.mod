module github.com/openshift/assisted-service

go 1.20

require (
	github.com/IBM/netaddr v1.5.0
	github.com/NYTimes/gziphandler v1.1.1
	github.com/alecthomas/units v0.0.0-20211218093645-b94a6e3cc137
	github.com/asaskevich/govalidator v0.0.0-20210307081110-f21760c49a8d
	github.com/aws/aws-sdk-go v1.44.163
	github.com/bombsimon/logrusr/v3 v3.0.0
	github.com/buger/jsonparser v1.1.1
	github.com/cavaliercoder/go-cpio v0.0.0-20180626203310-925f9528c45e
	github.com/containers/image/v5 v5.21.1
	github.com/coreos/ignition/v2 v2.14.0
	github.com/coreos/vcontext v0.0.0-20211021162308-f1dbbca7bef4
	github.com/danielerez/go-dns-client v0.0.0-20200630114514-0b60d1703f0b
	github.com/dustin/go-humanize v1.0.1
	github.com/filanov/stateswitch v1.0.1-0.20221122134945-bfa198e3a83a
	github.com/go-errors/errors v1.4.2
	github.com/go-gormigrate/gormigrate/v2 v2.0.1
	github.com/go-logr/logr v1.2.4
	github.com/go-openapi/errors v0.20.3
	github.com/go-openapi/loads v0.21.1
	github.com/go-openapi/runtime v0.19.24
	github.com/go-openapi/spec v0.20.7
	github.com/go-openapi/strfmt v0.21.3
	github.com/go-openapi/swag v0.22.3
	github.com/go-openapi/validate v0.22.0
	github.com/golang-collections/go-datastructures v0.0.0-20150211160725-59788d5eb259
	github.com/golang-jwt/jwt/v4 v4.5.0
	github.com/golang/mock v1.6.0
	github.com/google/go-cmp v0.6.0
	github.com/google/renameio v1.0.1
	github.com/google/uuid v1.6.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/hashicorp/go-version v1.6.0
	github.com/iancoleman/strcase v0.2.0
	github.com/itchyny/gojq v0.12.8
	github.com/jinzhu/copier v0.3.5
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/kennygrant/sanitize v1.2.4
	github.com/krishicks/yaml-patch v0.0.10
	github.com/metal3-io/baremetal-operator/apis v0.2.0
	github.com/moby/moby v26.0.0+incompatible
	github.com/nmstate/nmstate/rust/src/go/nmstate v0.0.0-20220811151154-801022633c42
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.28.1
	github.com/openshift-online/ocm-sdk-go v0.1.205
	github.com/openshift/api v0.0.0-20231031181504-3be12e93388f
	github.com/openshift/assisted-image-service v0.0.0-20231023144959-c402402f52bf
	github.com/openshift/assisted-service/api v0.0.0
	github.com/openshift/assisted-service/client v0.0.0
	github.com/openshift/assisted-service/models v0.0.0
	github.com/openshift/client-go v0.0.0-20230926161409-848405da69e1
	github.com/openshift/cluster-baremetal-operator v0.0.0-20240207191432-82df158cd2e9
	github.com/openshift/custom-resource-status v1.1.2
	github.com/openshift/generic-admission-server v1.14.1-0.20231020105858-8dcc3c9b298f
	github.com/openshift/hive/apis v0.0.0-20220222213051-def9088fdb5a
	github.com/openshift/image-customization-controller v0.0.0-20240129110832-60a3867d7f9e
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pelletier/go-toml v1.9.5
	github.com/pkg/errors v0.9.1
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.57.0
	github.com/prometheus/client_golang v1.17.0
	github.com/rs/cors v1.10.1
	github.com/samber/lo v1.39.0
	github.com/segmentio/kafka-go v0.4.38
	github.com/sirupsen/logrus v1.9.3
	github.com/slok/go-http-metrics v0.11.0
	github.com/stretchr/testify v1.9.0
	github.com/thedevsaddam/retry v1.2.1
	github.com/thoas/go-funk v0.9.3
	github.com/vincent-petithory/dataurl v1.0.0
	golang.org/x/crypto v0.14.0
	golang.org/x/sync v0.4.0
	golang.org/x/sys v0.16.0
	gopkg.in/ini.v1 v1.67.0
	gopkg.in/square/go-jose.v2 v2.6.0
	gopkg.in/yaml.v2 v2.4.0
	gorm.io/driver/postgres v1.3.5
	gorm.io/gorm v1.25.7
	k8s.io/api v0.28.4
	k8s.io/apiextensions-apiserver v0.28.2
	k8s.io/apimachinery v0.28.4
	k8s.io/client-go v0.28.4
	k8s.io/klog/v2 v2.100.1
	k8s.io/kube-aggregator v0.28.2
	k8s.io/kubectl v0.28.3
	k8s.io/utils v0.0.0-20230726121419-3b25d923346b
	open-cluster-management.io/api v0.12.0
	sigs.k8s.io/controller-runtime v0.15.0
	sigs.k8s.io/yaml v1.4.0
)

require (
	dario.cat/mergo v1.0.0 // indirect
	github.com/MakeNowJust/heredoc v1.0.0 // indirect
	github.com/Microsoft/hcsshim v0.11.4 // indirect
	github.com/antlr/antlr4/runtime/Go/antlr/v4 v4.0.0-20230305170008-8188dc5388df // indirect
	github.com/chai2010/gettext-go v1.0.2 // indirect
	github.com/containerd/containerd v1.7.12 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/cpuguy83/dockercfg v0.3.1 // indirect
	github.com/distribution/reference v0.5.0 // indirect
	github.com/elliotwutingfeng/asciiset v0.0.0-20230602022725-51bbb787efab // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/evanphx/json-patch/v5 v5.6.0 // indirect
	github.com/exponent-io/jsonpath v0.0.0-20151013193312-d6023ce2651d // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-sql-driver/mysql v1.7.0 // indirect
	github.com/google/btree v1.0.1 // indirect
	github.com/google/cel-go v0.16.1 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/gregjones/httpcache v0.0.0-20180305231024-9cad4c3443a7 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.16.0 // indirect
	github.com/liggitt/tabwriter v0.0.0-20181228230101-89fcab3d43de // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/magiconair/properties v1.8.7 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/moby/patternmatcher v0.6.0 // indirect
	github.com/moby/spdystream v0.2.0 // indirect
	github.com/moby/sys/sequential v0.5.0 // indirect
	github.com/moby/sys/user v0.1.0 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/openshift/library-go v0.0.0-20231110170715-08d73a9c798b // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/shirou/gopsutil/v3 v3.23.12 // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/stoewer/go-strcase v1.2.0 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/xdg/scram v1.0.5 // indirect
	github.com/xdg/stringprep v1.0.3 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.3 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.19.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.19.0 // indirect
	go.starlark.net v0.0.0-20230525235612-a134d8f9ddca // indirect
	golang.org/x/exp v0.0.0-20230905200255-921286631fa9 // indirect
	golang.org/x/mod v0.16.0 // indirect
	golang.org/x/tools v0.13.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20230711160842-782d3b101e98 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230711160842-782d3b101e98 // indirect
	k8s.io/cli-runtime v0.28.3 // indirect
	k8s.io/kms v0.28.4 // indirect
	sigs.k8s.io/kustomize/api v0.13.5-0.20230601165947-6ce0bf390ce3 // indirect
	sigs.k8s.io/kustomize/kyaml v0.14.3-0.20230601165947-6ce0bf390ce3 // indirect
)

require (
	cloud.google.com/go/compute v1.23.1 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20210617225240-d185dfc1b5a1 // indirect
	github.com/Microsoft/go-winio v0.6.1 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cenkalti/backoff/v4 v4.2.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/coreos/go-json v0.0.0-20211020211907-c63f628265de // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/diskfs/go-diskfs v1.4.0 // indirect
	github.com/docker/docker v25.0.3+incompatible // indirect
	github.com/docker/go-connections v0.5.0
	github.com/docker/go-units v0.5.0 // indirect
	github.com/evanphx/json-patch v5.6.0+incompatible // indirect
	github.com/felixge/httpsnoop v1.0.3 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-logr/zapr v1.2.4 // indirect
	github.com/go-openapi/analysis v0.21.2 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt v3.2.2+incompatible // indirect
	github.com/golang/glog v1.1.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/gorilla/css v1.0.0 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/imdario/mergo v0.3.13 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/itchyny/timefmt-go v0.1.3 // indirect
	github.com/jackc/chunkreader/v2 v2.0.1 // indirect
	github.com/jackc/pgconn v1.14.1 // indirect
	github.com/jackc/pgio v1.0.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgproto3/v2 v2.3.2 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/pgtype v1.14.0 // indirect
	github.com/jackc/pgx/v4 v4.16.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.17.1 // indirect
	github.com/lib/pq v1.10.9
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-sqlite3 v2.0.3+incompatible // indirect
	github.com/metal3-io/baremetal-operator/pkg/hardwareutils v0.2.0 // indirect
	github.com/microcosm-cc/bluemonday v1.0.26 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/moby/term v0.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nxadm/tail v1.4.8 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.0 // indirect
	github.com/openshift/machine-config-operator v0.0.1-0.20201023110058-6c8bd9b2915c
	github.com/pierrec/lz4/v4 v4.1.17 // indirect
	github.com/pkg/xattr v0.4.9 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/spf13/cobra v1.7.0 // indirect
	github.com/spf13/pflag v1.0.6-0.20210604193023-d5e0c0615ace // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/testcontainers/testcontainers-go v0.29.1
	github.com/ulikunitz/xz v0.5.11 // indirect
	go.etcd.io/etcd/api/v3 v3.5.9 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.5.9 // indirect
	go.etcd.io/etcd/client/v3 v3.5.9 // indirect
	go.mongodb.org/mongo-driver v1.10.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.45.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.45.0 // indirect
	go.opentelemetry.io/otel v1.19.0 // indirect
	go.opentelemetry.io/otel/metric v1.19.0 // indirect
	go.opentelemetry.io/otel/sdk v1.19.0 // indirect
	go.opentelemetry.io/otel/trace v1.19.0 // indirect
	go.opentelemetry.io/proto/otlp v1.0.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.24.0 // indirect
	golang.org/x/net v0.17.0
	golang.org/x/oauth2 v0.12.0 // indirect
	golang.org/x/term v0.13.0 // indirect
	golang.org/x/text v0.13.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.3.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20230711160842-782d3b101e98 // indirect
	google.golang.org/grpc v1.58.3 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
	gopkg.in/djherbis/times.v1 v1.3.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiserver v0.28.4 // indirect
	k8s.io/component-base v0.28.4 // indirect
	k8s.io/kube-openapi v0.0.0-20230717233707-2695361300d9 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.1.2 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/kube-storage-version-migrator v0.0.6-0.20230721195810-5c8923c5ff96 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
)

replace (
	github.com/metal3-io/baremetal-operator => github.com/openshift/baremetal-operator v0.0.0-20231019133159-8643f32fea3e
	github.com/metal3-io/baremetal-operator/apis => github.com/openshift/baremetal-operator/apis v0.0.0-20231019133159-8643f32fea3e
	github.com/metal3-io/baremetal-operator/pkg/hardwareutils => github.com/openshift/baremetal-operator/pkg/hardwareutils v0.0.0-20231019133159-8643f32fea3e
	github.com/opencontainers/runc => github.com/opencontainers/runc v1.1.12
	github.com/openshift/assisted-service/api => ./api
	github.com/openshift/assisted-service/client => ./client
	github.com/openshift/assisted-service/cmd/agentbasedinstaller => ./cmd/agentbasedinstaller
	github.com/openshift/assisted-service/models => ./models
	sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20201022175424-d30c7a274820
	sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.1.0-alpha.3.0.20201016155852-4090a6970205
)
