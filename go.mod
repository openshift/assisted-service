module github.com/openshift/assisted-service

go 1.16

require (
	github.com/alecthomas/units v0.0.0-20190924025748-f65c72e2690d
	github.com/alessio/shellescape v1.4.1
	github.com/asaskevich/govalidator v0.0.0-20200428143746-21a406dcc535
	github.com/aws/aws-sdk-go v1.34.21
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
	github.com/go-openapi/errors v0.19.6
	github.com/go-openapi/loads v0.19.5
	github.com/go-openapi/runtime v0.19.20
	github.com/go-openapi/spec v0.19.9
	github.com/go-openapi/strfmt v0.19.5
	github.com/go-openapi/swag v0.19.9
	github.com/go-openapi/validate v0.19.10
	github.com/golang-collections/go-datastructures v0.0.0-20150211160725-59788d5eb259
	github.com/golang/mock v1.5.0
	github.com/google/renameio v0.1.0
	github.com/google/uuid v1.1.2
	github.com/hashicorp/go-multierror v1.1.0
	github.com/hashicorp/go-version v1.2.1
	github.com/iancoleman/strcase v0.1.2
	github.com/jinzhu/gorm v1.9.12
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/kennygrant/sanitize v1.2.4
	github.com/mattn/go-sqlite3 v2.0.3+incompatible
	github.com/metal3-io/baremetal-operator v0.0.0-20210317131627-82fd2d7f8daa
	github.com/moby/moby v1.13.1
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.13.0
	github.com/openshift-online/ocm-sdk-go v0.1.190
	github.com/openshift/api v3.9.1-0.20191111211345-a27ff30ebf09+incompatible
	github.com/openshift/client-go v0.0.0-20201020074620-f8fd44879f7c
	github.com/openshift/custom-resource-status v1.1.0
	github.com/openshift/hive/apis v0.0.0-20210506000654-5c038fb05190
	github.com/openshift/machine-api-operator v0.2.1-0.20201002104344-6abfb5440597
	github.com/ory/dockertest/v3 v3.6.3
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
	golang.org/x/crypto v0.0.0-20220331220935-ae2d96664a29
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/sys v0.0.0-20210630005230-0f9fa26af87c
	golang.org/x/tools v0.1.5 // indirect
	gopkg.in/gormigrate.v1 v1.6.0
	gopkg.in/ini.v1 v1.51.0
	gopkg.in/square/go-jose.v2 v2.3.1
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.21.1
	k8s.io/apimachinery v0.21.1
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/utils v0.0.0-20210527160623-6fdb442a123b
	sigs.k8s.io/controller-runtime v0.9.0
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/metal3-io/baremetal-operator => github.com/openshift/baremetal-operator v0.0.0-20210409032903-31b989a197eb // Use OpenShift fork
	k8s.io/api => k8s.io/api v0.21.1
	k8s.io/client-go => k8s.io/client-go v0.21.1
	sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20201022175424-d30c7a274820
	sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.1.0-alpha.3.0.20201016155852-4090a6970205
)
