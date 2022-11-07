lint-spectral:
	spectral lint swagger.yaml

goswagger-validate:
	goswagger validate swagger.yaml

generate-go-client:
	rm -rf client/ models/*.go
	goswagger generate client --skip-validation --template=stratoscale -f swagger.yaml

generate-go-server:
	rm -rf restapi/
	goswagger generate server --skip-validation --template=stratoscale -f swagger.yaml

generate-python-client:
	mkdir -p ${BUILD_FOLDER}
	./hack/generate.sh generate_python_client

packages-for-generate = $(addsuffix -go-generate-package, $(shell go list ./... | grep -v 'assisted-service/models\|assisted-service/client\|assisted-service/restapi'))

%-go-generate-package: delete-mock-files
	@go generate $*

delete-mock-files: post-swagger-generate
	find . -name 'mock_*.go' -type f -not -path './vendor/*' -delete

generate-go-generate: $(packages-for-generate)

rename-generated: generate-go-generate
	find . -type f -name '*.generated_go' -exec sh -c 'mv "$$1" "$${1%.generated_go}.go"' _ {} \;

generate-migration:
	go run ./tools/migration_generator/migration_generator.go -name=${MIGRATION_NAME}

generate-keys:
	mkdir -p ${BUILD_FOLDER}
	(cd ./tools && go run auth_keys_generator.go -keys-dir=${BUILD_FOLDER})

generate-events:
	rm -rf internal/common/events
	mkdir -p internal/common/events
	tools/generate_events.py ./docs/events.yaml internal/common/events/events.go

remove-models-dashes-and-dots: validate-generated-models 
	find models/ -name "*.go" -maxdepth 1 -exec sed -i 's/Dash//g;s/Dot//g' {} \;

# A collection of independent targets that have no dependencies, make will run them in parallel
generate-parallel: goswagger-validate lint-spectral generate-go-client generate-go-server generate-events generate-configuration

validate-generated-models: generate-parallel
	./hack/generate.sh validate_generated_models

generate-configuration:
	./hack/generate.sh generate_configuration

generate-manifests:
	mkdir -p ${BUILD_FOLDER}
	./hack/generate.sh generate_manifests

generate-bundle: generate-vendor
	mkdir -p ${BUILD_FOLDER}
	./hack/generate.sh generate_bundle

post-swagger-generate: remove-models-dashes-and-dots

generate-vendor: rename-generated
	go mod vendor
	cd api && go mod vendor
	cd models && go mod vendor

generate-all: generate-bundle generate-vendor generate-go-generate generate-configuration generate-events generate-go-client generate-go-server

generate:
	$(MAKE) -r -j$(shell nproc) generate-all
