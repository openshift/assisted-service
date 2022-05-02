package mocks

//go:generate mockgen --build_flags=--mod=mod -package mocks -destination mock_assisted_service.go github.com/openshift/assisted-service/restapi InstallerAPI
//go:generate mockgen --build_flags=--mod=mod -package mocks -destination mock_manifests.go github.com/openshift/assisted-service/restapi ManifestsAPI
