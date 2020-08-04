module github.com/openshift/assisted-service

go 1.13

require (
	github.com/alecthomas/units v0.0.0-20190924025748-f65c72e2690d
	github.com/aws/aws-sdk-go v1.32.6
	github.com/cenkalti/backoff/v3 v3.2.2 // indirect
	github.com/containerd/continuity v0.0.0-20200710164510-efbc4488d8fe // indirect
	github.com/danielerez/go-dns-client v0.0.0-20200630114514-0b60d1703f0b
	github.com/docker/go-units v0.4.0
	github.com/filanov/stateswitch v0.0.0-20200714113403-51a42a34c604
	github.com/go-openapi/errors v0.19.6
	github.com/go-openapi/loads v0.19.5
	github.com/go-openapi/runtime v0.19.19
	github.com/go-openapi/spec v0.19.8
	github.com/go-openapi/strfmt v0.19.5
	github.com/go-openapi/swag v0.19.9
	github.com/go-openapi/validate v0.19.10
	github.com/golang/mock v1.2.0
	github.com/google/uuid v1.1.1
	github.com/jinzhu/gorm v1.9.12
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/minio/minio-go/v6 v6.0.55
	github.com/onsi/ginkgo v1.14.0
	github.com/onsi/gomega v1.10.1
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/ory/dockertest/v3 v3.6.0
	github.com/pborman/uuid v1.2.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.6.0
	github.com/prometheus/common v0.9.1
	github.com/sirupsen/logrus v1.6.0
	github.com/slok/go-http-metrics v0.8.0
	github.com/stretchr/testify v1.6.1
	github.com/thoas/go-funk v0.6.0
	github.com/vincent-petithory/dataurl v0.0.0-20191104211930-d1553a71de50
	golang.org/x/net v0.0.0-20200707034311-ab3426394381 // indirect
	golang.org/x/sys v0.0.0-20200625212154-ddb9806d33ae // indirect
	gopkg.in/yaml.v2 v2.3.0
	gotest.tools/gotestsum v0.5.2 // indirect
	k8s.io/api v0.17.3
	k8s.io/apimachinery v0.17.3
	k8s.io/client-go v11.0.0+incompatible
	sigs.k8s.io/controller-runtime v0.5.0
)

replace (
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.0.0-20191016114015-74ad18325ed5
	k8s.io/client-go => k8s.io/client-go v0.0.0-20191016111102-bec269661e48

)
