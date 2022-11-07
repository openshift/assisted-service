package mocks

//go:generate mockgen --build_flags=--mod=mod -package mocks -destination mock_assisted_service.generated_go github.com/openshift/assisted-service/restapi InstallerAPI
//go:generate mockgen --build_flags=--mod=mod -package mocks -destination mock_manifests.generated_go github.com/openshift/assisted-service/restapi ManifestsAPI
