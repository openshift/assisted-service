module github.com/openshift/assisted-service/api

go 1.25.0

toolchain go1.25.5

require (
	github.com/openshift/assisted-service/models v0.0.0
	github.com/openshift/custom-resource-status v1.1.3-0.20220503160415-f2fdb4999d87
	github.com/openshift/hive/apis v0.0.0-20231220215202-ad99b9e52d27
	k8s.io/api v0.29.9
	k8s.io/apimachinery v0.29.9
	sigs.k8s.io/controller-runtime v0.16.3
	sigs.k8s.io/yaml v1.4.0
)

require (
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/go-logr/logr v1.4.1 // indirect
	github.com/go-openapi/analysis v0.23.0 // indirect
	github.com/go-openapi/errors v0.22.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/loads v0.22.0 // indirect
	github.com/go-openapi/spec v0.21.0 // indirect
	github.com/go-openapi/strfmt v0.23.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-openapi/validate v0.24.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/openshift/api v0.0.0-20240521212423-414cf30d37be // indirect
	github.com/openshift/assisted-service v1.0.10-0.20251114000904-3797225d8915 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/thoas/go-funk v0.9.3 // indirect
	go.mongodb.org/mongo-driver v1.14.0 // indirect
	golang.org/x/net v0.33.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gorm.io/gorm v1.25.8 // indirect
	k8s.io/klog/v2 v2.110.1 // indirect
	k8s.io/utils v0.0.0-20260210185600-b8788abfbbc2 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
)

replace (
	github.com/openshift/assisted-service/models => ../models
	golang.org/x/net => golang.org/x/net v0.24.0
)

replace github.com/golang/glog => github.com/golang/glog v1.2.4

replace github.com/containerd/containerd => github.com/containerd/containerd/v2 v2.1.5
