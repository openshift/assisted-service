module github.com/openshift/assisted-service/api

go 1.26.0

require (
	github.com/openshift/assisted-service/models v0.0.0
	github.com/openshift/custom-resource-status v1.1.3-0.20220503160415-f2fdb4999d87
	github.com/openshift/hive/apis v0.0.0-20260827190012-5068da00bed7
	k8s.io/api v0.36.2
	k8s.io/apimachinery v0.36.2
	sigs.k8s.io/yaml v1.6.0
)

require (
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-openapi/analysis v0.23.0 // indirect
	github.com/go-openapi/errors v0.22.4 // indirect
	github.com/go-openapi/jsonpointer v0.22.3 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/loads v0.22.0 // indirect
	github.com/go-openapi/spec v0.21.0 // indirect
	github.com/go-openapi/strfmt v0.25.0 // indirect
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
	github.com/go-openapi/validate v0.24.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.4 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/openshift/api v0.0.0-20260728120005-8ba0b25b0f29 // indirect
	github.com/openshift/installer v1.5.0-alpha.2 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.mongodb.org/mongo-driver v1.17.6 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gorm.io/gorm v1.24.5 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/kube-openapi v0.0.0-20260519202549-bbf5c5577288 // indirect
	k8s.io/utils v0.0.0-20260707023825-cf1189d6abe3 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.4.0 // indirect
)

replace (
	github.com/openshift/assisted-service/models => ../models
	golang.org/x/net => golang.org/x/net v0.24.0
)

replace github.com/golang/glog => github.com/golang/glog v1.2.4
