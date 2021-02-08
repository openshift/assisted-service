module github.com/openshift/assisted-service

go 1.13

require (
	github.com/alecthomas/units v0.0.0-20190924025748-f65c72e2690d
	github.com/alessio/shellescape v1.4.1
	github.com/asaskevich/govalidator v0.0.0-20200428143746-21a406dcc535
	github.com/aws/aws-sdk-go v1.32.6
	github.com/cavaliercoder/go-cpio v0.0.0-20180626203310-925f9528c45e
	github.com/cenkalti/backoff/v3 v3.2.2 // indirect
	github.com/containerd/continuity v0.0.0-20200710164510-efbc4488d8fe // indirect
	github.com/containers/image/v5 v5.7.0
	github.com/coreos/ignition/v2 v2.6.0
	github.com/danielerez/go-dns-client v0.0.0-20200630114514-0b60d1703f0b
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/diskfs/go-diskfs v1.1.2-0.20210126085940-5b529d606a0a
	github.com/docker/go-units v0.4.0
	github.com/dustin/go-humanize v1.0.0
	github.com/filanov/stateswitch v0.0.0-20200714113403-51a42a34c604
	github.com/frankban/quicktest v1.11.3 // indirect
	github.com/go-openapi/errors v0.19.6
	github.com/go-openapi/loads v0.19.5
	github.com/go-openapi/runtime v0.19.20
	github.com/go-openapi/spec v0.19.8
	github.com/go-openapi/strfmt v0.19.5
	github.com/go-openapi/swag v0.19.9
	github.com/go-openapi/validate v0.19.10
	github.com/golang-collections/go-datastructures v0.0.0-20150211160725-59788d5eb259
	github.com/golang/mock v1.4.4
	github.com/google/uuid v1.1.1
	github.com/hashicorp/go-multierror v1.1.0
	github.com/hashicorp/go-version v1.2.1
	github.com/iancoleman/strcase v0.1.2
	github.com/jinzhu/gorm v1.9.12
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/kennygrant/sanitize v1.2.4
	github.com/metal3-io/baremetal-operator v0.0.0
	github.com/mitchellh/mapstructure v1.3.3 // indirect
	github.com/moby/moby v1.13.1
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/openshift-online/ocm-sdk-go v0.1.130
	github.com/openshift/api v3.9.1-0.20191111211345-a27ff30ebf09+incompatible
	github.com/openshift/assisted-service/models v0.0.0-00010101000000-000000000000
	github.com/openshift/client-go v0.0.0-20200827190008-3062137373b5
	github.com/openshift/hive v0.0.0-00010101000000-000000000000
	github.com/ory/dockertest/v3 v3.6.0
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pborman/uuid v1.2.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.7.1
	github.com/prometheus/common v0.10.0
	github.com/rs/cors v1.7.0
	github.com/sirupsen/logrus v1.7.0
	github.com/slok/go-http-metrics v0.8.0
	github.com/stretchr/objx v0.3.0 // indirect
	github.com/stretchr/testify v1.6.1
	github.com/thoas/go-funk v0.6.0
	github.com/vincent-petithory/dataurl v0.0.0-20191104211930-d1553a71de50
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9
	golang.org/x/sync v0.0.0-20201008141435-b3e1573b7520
	golang.org/x/sys v0.0.0-20201009025420-dfb3f7c4e634 // indirect
	golang.org/x/tools v0.0.0-20201013201025-64a9e34f3752 // indirect
	gopkg.in/gormigrate.v1 v1.6.0
	gopkg.in/square/go-jose.v2 v2.3.1
	gopkg.in/yaml.v2 v2.3.0
	honnef.co/go/tools v0.0.1-2020.1.6 // indirect
	k8s.io/api v0.19.0
	k8s.io/apimachinery v0.19.0
	k8s.io/client-go v12.0.0+incompatible
	sigs.k8s.io/controller-runtime v0.6.2
)

replace (
	github.com/metal3-io/baremetal-operator => github.com/openshift/baremetal-operator v0.0.0-20200715132148-0f91f62a41fe // Use OpenShift fork
	github.com/metal3-io/cluster-api-provider-baremetal => github.com/openshift/cluster-api-provider-baremetal v0.0.0-20190821174549-a2a477909c1d
	github.com/openshift/assisted-service/models => ./models
	github.com/openshift/hive => github.com/dgoodwin/hive v0.0.0-20210121160047-23364b143670
	github.com/openshift/library-go => github.com/openshift/library-go v0.0.0-20200918101923-1e4c94603efe
	github.com/terraform-providers/terraform-provider-aws => github.com/openshift/terraform-provider-aws v1.60.1-0.20200630224953-76d1fb4e5699
	github.com/terraform-providers/terraform-provider-azurerm => github.com/openshift/terraform-provider-azurerm v1.40.1-0.20200707062554-97ea089cc12a
	github.com/terraform-providers/terraform-provider-ignition/v2 => github.com/community-terraform-providers/terraform-provider-ignition/v2 v2.1.0
	k8s.io/client-go => k8s.io/client-go v0.19.0
	sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20200506073438-9d49428ff837
	sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.1.0-alpha.3.0.20200120114645-8a9592f1f87b
	sigs.k8s.io/cluster-api-provider-openstack => github.com/openshift/cluster-api-provider-openstack v0.0.0-20200526112135-319a35b2e38e
)
