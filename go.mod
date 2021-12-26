module github.com/openshift/assisted-service

go 1.16

require (
	github.com/alecthomas/units v0.0.0-20190924025748-f65c72e2690d
	github.com/alessio/shellescape v1.4.1
	github.com/asaskevich/govalidator v0.0.0-20200907205600-7a23bdc65eef
	github.com/aws/aws-sdk-go v1.34.28
	github.com/buger/jsonparser v1.1.1
	github.com/cavaliercoder/go-cpio v0.0.0-20180626203310-925f9528c45e
	github.com/cenkalti/backoff/v3 v3.2.2 // indirect
	github.com/containerd/continuity v0.0.0-20200710164510-efbc4488d8fe // indirect
	github.com/containers/image/v5 v5.7.0
	github.com/coreos/ignition/v2 v2.9.0
	github.com/coreos/vcontext v0.0.0-20201120045928-b0e13dab675c
	github.com/danielerez/go-dns-client v0.0.0-20200630114514-0b60d1703f0b
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/diskfs/go-diskfs v1.1.2-0.20210216073915-ba492710e2d8
	github.com/dustin/go-humanize v1.0.0
	github.com/filanov/stateswitch v0.0.0-20200714113403-51a42a34c604
	github.com/frankban/quicktest v1.11.3 // indirect
	github.com/go-gormigrate/gormigrate/v2 v2.0.0
	github.com/go-openapi/errors v0.20.1
	github.com/go-openapi/loads v0.20.2
	github.com/go-openapi/runtime v0.19.24
	github.com/go-openapi/spec v0.20.4
	github.com/go-openapi/strfmt v0.20.3
	github.com/go-openapi/swag v0.19.15
	github.com/go-openapi/validate v0.20.3
	github.com/golang-collections/go-datastructures v0.0.0-20150211160725-59788d5eb259
	github.com/golang/mock v1.5.0
	github.com/google/go-cmp v0.5.5
	github.com/google/renameio v0.1.0
	github.com/google/uuid v1.3.0
	github.com/hashicorp/go-multierror v1.1.0
	github.com/hashicorp/go-version v1.2.1
	github.com/iancoleman/strcase v0.1.2
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/kennygrant/sanitize v1.2.4
	github.com/mattn/go-sqlite3 v2.0.3+incompatible
	github.com/metal3-io/baremetal-operator v0.0.0-20210317131627-82fd2d7f8daa
	github.com/moby/moby v20.10.12+incompatible
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.13.0
	github.com/openshift-online/ocm-sdk-go v0.1.190
	github.com/openshift/api v3.9.1-0.20191111211345-a27ff30ebf09+incompatible
	github.com/openshift/client-go v0.0.0-20201020074620-f8fd44879f7c
	github.com/openshift/custom-resource-status v1.1.0
	github.com/openshift/generic-admission-server v1.14.1-0.20210422140326-da96454c926d
	github.com/openshift/hive/apis v0.0.0-20210506000654-5c038fb05190
	github.com/openshift/machine-api-operator v0.2.1-0.20201002104344-6abfb5440597
	github.com/ory/dockertest/v3 v3.6.3
	github.com/ovirt/go-ovirt-client v0.7.1
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pelletier/go-toml v1.8.1
	github.com/pkg/errors v0.9.1
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.49.0
	github.com/prometheus/client_golang v1.11.0
	github.com/rs/cors v1.7.0
	github.com/sirupsen/logrus v1.7.0
	github.com/slok/go-http-metrics v0.8.0
	github.com/stretchr/testify v1.7.0
	github.com/thedevsaddam/retry v0.0.0-20200324223450-9769a859cc6d
	github.com/thoas/go-funk v0.8.0
	github.com/vincent-petithory/dataurl v0.0.0-20191104211930-d1553a71de50
	go.elastic.co/apm/module/apmhttp v1.11.0
	go.elastic.co/apm/module/apmlogrus v1.11.0
	golang.org/x/crypto v0.0.0-20210921155107-089bfa567519
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/sys v0.0.0-20210630005230-0f9fa26af87c
	golang.org/x/tools v0.1.5 // indirect
	gopkg.in/ini.v1 v1.51.0
	gopkg.in/square/go-jose.v2 v2.3.1
	gopkg.in/yaml.v2 v2.4.0
	gorm.io/driver/postgres v1.2.1
	gorm.io/gorm v1.22.3
	k8s.io/api v0.21.1
	k8s.io/apimachinery v0.21.1
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/kube-aggregator v0.20.0
	k8s.io/utils v0.0.0-20210527160623-6fdb442a123b
	sigs.k8s.io/controller-runtime v0.9.0
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/coreos/etcd => github.com/coreos/etcd v3.3.13+incompatible
	github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.4.1
	github.com/metal3-io/baremetal-operator => github.com/openshift/baremetal-operator v0.0.0-20210409032903-31b989a197eb // Use OpenShift fork
	go.etcd.io/bbolt => go.etcd.io/bbolt v1.3.5
	go.etcd.io/etcd => go.etcd.io/etcd v0.5.0-alpha.5.0.20200910180754-dd1b699fc489 // ae9734ed278b is the SHA for git tag v3.4.13
	google.golang.org/grpc => google.golang.org/grpc v1.27.1
	k8s.io/api => k8s.io/api v0.21.1
	k8s.io/apiserver => k8s.io/apiserver v0.21.1 // indirect
	k8s.io/client-go => k8s.io/client-go v0.21.1
	k8s.io/component-base => k8s.io/component-base v0.21.1
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20201113171705-d219536bb9fd
	sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20201022175424-d30c7a274820
	sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.1.0-alpha.3.0.20201016155852-4090a6970205
)
