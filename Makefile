PWD = $(shell pwd)
UID = $(shell id -u)
BUILD_FOLDER = $(PWD)/build
ROOT_DIR = $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

TARGET := $(or ${TARGET},minikube)
NAMESPACE := $(or ${NAMESPACE},assisted-installer)
KUBECTL=kubectl -n $(NAMESPACE)

ifeq ($(TARGET), minikube)
define get_service
minikube service --url $(1) -n $(NAMESPACE) | sed 's/http:\/\///g'
endef # get_service
else
define get_service
kubectl get service $(1) -n $(NAMESPACE) | grep $(1) | awk '{print $$4 ":" $$5}' | \
	awk '{split($$0,a,":"); print a[1] ":" a[2]}'
endef # get_service
endif # TARGET

SERVICE := $(or ${SERVICE},quay.io/ocpmetal/assisted-service:latest)
ISO_CREATION := $(or ${ISO_CREATION},quay.io/ocpmetal/installer-image-build:latest)
GIT_REVISION := $(shell git rev-parse HEAD)
APPLY_NAMESPACE := $(or ${APPLY_NAMESPACE},True)
ROUTE53_SECRET := ${ROUTE53_SECRET}
OCM_CLIENT_ID := ${OCM_CLIENT_ID}
OCM_CLIENT_SECRET := ${OCM_CLIENT_SECRET}
ENABLE_AUTH := $(or ${ENABLE_AUTH},False)

all: build

lint:
	golangci-lint run -v

$(BUILD_FOLDER):
	mkdir -p $(BUILD_FOLDER)

format:
	goimports -w -l cmd/ internal/ subsystem/ assisted-iso-create/
	gofmt -w -l cmd/ internal/ subsystem/ assisted-iso-create/

############
# Generate #
############

generate:
	go generate $(shell go list ./... | grep -v 'assisted-service/models\|assisted-service/client\|assisted-service/restapi')

generate-from-swagger: generate-go-client generate-go-server

generate-go-server:
	rm -rf restapi
	docker run -u $(UID):$(UID) -v $(PWD):$(PWD):rw,Z -v /etc/passwd:/etc/passwd -w $(PWD) \
		quay.io/goswagger/swagger:v0.25.0 generate server --template=stratoscale -f swagger.yaml \
		--template-dir=/templates/contrib

generate-go-client:
	rm -rf client models
	docker run -u $(UID):$(UID) -v $(PWD):$(PWD):rw,Z -v /etc/passwd:/etc/passwd -w $(PWD) \
		quay.io/goswagger/swagger:v0.25.0 generate client --template=stratoscale -f swagger.yaml \
		--template-dir=/templates/contrib

generate-python-client: $(BUILD_FOLDER)
	rm -rf $(BUILD_FOLDER)/assisted-service-client*
	docker run --rm -u ${UID} --entrypoint /bin/sh \
		-v $(BUILD_FOLDER):/local:Z \
		-v $(ROOT_DIR)/swagger.yaml:/swagger.yaml:ro,Z \
		-v $(ROOT_DIR)/tools/generate_python_client.sh:/script.sh:ro,Z \
		-e SWAGGER_FILE=/swagger.yaml -e OUTPUT=/local/assisted-service-client/ \
		swaggerapi/swagger-codegen-cli:2.4.15 /script.sh
	cd $(BUILD_FOLDER)/assisted-service-client/ && python3 setup.py sdist --dist-dir $(BUILD_FOLDER)

generate-keys:
	cd tools && go run auth_keys_generator.go -keys-dir=$(BUILD_FOLDER)

##################
# Build & Update #
##################

.PHONY: build
build: lint unit-test build-minimal build-iso-generator generate-keys

build-minimal: $(BUILD_FOLDER)
	CGO_ENABLED=0 go build -o $(BUILD_FOLDER)/assisted-service cmd/main.go

build-iso-generator: $(BUILD_FOLDER)
	CGO_ENABLED=0 go build -o $(BUILD_FOLDER)/assisted-iso-create assisted-iso-create/main.go

build-onprem: build
	podman build -f Dockerfile.assisted-service-onprem -t ${SERVICE} .

build-image: build
	GIT_REVISION=${GIT_REVISION} docker build --network=host --build-arg GIT_REVISION \
 		-f Dockerfile.assisted-service . -t $(SERVICE)

build-assisted-iso-generator-image: build
	GIT_REVISION=${GIT_REVISION} docker build --network=host --build-arg GIT_REVISION \
 		-f Dockerfile.assisted-iso-create . -t $(ISO_CREATION)

update: build-image
	docker push $(SERVICE)

update-minimal: build-minimal
	GIT_REVISION=${GIT_REVISION} docker build --network=host --build-arg GIT_REVISION \
		-f Dockerfile.assisted-service . -t $(SERVICE)

update-minikube: build
	eval $$(SHELL=$${SHELL:-/bin/sh} minikube docker-env) && \
		GIT_REVISION=${GIT_REVISION} docker build --network=host --build-arg GIT_REVISION \
		-f Dockerfile.assisted-service . -t $(SERVICE)

##########
# Deploy #
##########
ifdef DEPLOY_TAG
  DEPLOY_TAG_OPTION = --deploy-tag "$(DEPLOY_TAG)"
else ifdef DEPLOY_MANIFEST_PATH
  DEPLOY_TAG_OPTION = --deploy-manifest-path "$(DEPLOY_MANIFEST_PATH)"
else ifdef DEPLOY_MANIFEST_TAG
  DEPLOY_TAG_OPTION = --deploy-manifest-tag "$(DEPLOY_MANIFEST_TAG)"
endif

deploy-all: $(BUILD_FOLDER) deploy-namespace deploy-postgres deploy-s3 deploy-ocm-secret deploy-route53 deploy-service
	echo "Deployment done"

deploy-ui: deploy-namespace
	python3 ./tools/deploy_ui.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --namespace "$(NAMESPACE)" $(DEPLOY_TAG_OPTION)

deploy-namespace: $(BUILD_FOLDER)
	python3 ./tools/deploy_namespace.py --deploy-namespace $(APPLY_NAMESPACE) --namespace "$(NAMESPACE)"

deploy-s3-secret:
	python3 ./tools/deploy_scality_configmap.py --namespace "$(NAMESPACE)"

deploy-s3: deploy-namespace
	python3 ./tools/deploy_s3.py --namespace "$(NAMESPACE)"
	sleep 5;  # wait for service to get an address
	make deploy-s3-secret

deploy-route53: deploy-namespace
	python3 ./tools/deploy_route53.py --secret "$(ROUTE53_SECRET)" --namespace "$(NAMESPACE)"

deploy-ocm-secret: deploy-namespace
	python3 ./tools/deploy_sso_secret.py --secret "$(OCM_CLIENT_SECRET)" --id "$(OCM_CLIENT_ID)" --namespace "$(NAMESPACE)"

deploy-inventory-service-file: deploy-namespace
	python3 ./tools/deploy_inventory_service.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --namespace "$(NAMESPACE)"
	sleep 5;  # wait for service to get an address

deploy-service-requirements: deploy-namespace deploy-inventory-service-file
	python3 ./tools/deploy_assisted_installer_configmap.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --base-dns-domains "$(BASE_DNS_DOMAINS)" --namespace "$(NAMESPACE)" $(DEPLOY_TAG_OPTION) --enable-auth "$(ENABLE_AUTH)"

deploy-service: deploy-namespace deploy-service-requirements deploy-role
	python3 ./tools/deploy_assisted_installer.py $(DEPLOY_TAG_OPTION) --namespace "$(NAMESPACE)" $(TEST_FLAGS)
	python3 ./tools/wait_for_assisted_service.py --target $(TARGET) --namespace "$(NAMESPACE)" --domain "$(INGRESS_DOMAIN)"

deploy-role: deploy-namespace
	python3 ./tools/deploy_role.py --namespace "$(NAMESPACE)"

deploy-postgres: deploy-namespace
	python3 ./tools/deploy_postgres.py --namespace "$(NAMESPACE)"

jenkins-deploy-for-subsystem:
	export TEST_FLAGS=--subsystem-test && export ENABLE_AUTH="True" && $(MAKE) deploy-all

deploy-test:
	export SERVICE=minikube-local-registry/assisted-service:minikube-test && export TEST_FLAGS=--subsystem-test && export ENABLE_AUTH="True" \
	&& $(MAKE) update-minikube deploy-all

deploy-onprem:
	podman pod create --name assisted-installer -p 5432,8000,8090,8080
	podman volume create s3-volume
	podman run -dt --pod assisted-installer --env-file onprem-environment -v s3-volume:/mnt/data:rw --name s3 scality/s3server:latest
	podman run -dt --pod assisted-installer --env-file onprem-environment --name db centos/postgresql-12-centos7
	podman run -dt --pod assisted-installer --env-file onprem-environment --user assisted-installer  --restart always --name installer ${SERVICE}
	podman run -dt --pod assisted-installer --env-file onprem-environment --pull always -v $(PWD)/deploy/ui/nginx.conf:/opt/bitnami/nginx/conf/server_blocks/nginx.conf:z --name ui quay.io/ocpmetal/ocp-metal-ui:latest

########
# Test #
########

subsystem-run: test subsystem-clean

test:
	INVENTORY=$(shell $(call get_service,assisted-service) | sed 's/http:\/\///g') \
		DB_HOST=$(shell $(call get_service,postgres) | sed 's/http:\/\///g' | cut -d ":" -f 1) \
		DB_PORT=$(shell $(call get_service,postgres) | sed 's/http:\/\///g' | cut -d ":" -f 2) \
		TEST_TOKEN="$(shell cat $(BUILD_FOLDER)/auth-tokenString)" \
		ENABLE_AUTH="true" \
		go test -v ./subsystem/... -count=1 -ginkgo.focus=${FOCUS} -ginkgo.v -timeout 30m

deploy-olm: deploy-namespace
	python3 ./tools/deploy_olm.py --target $(TARGET)

deploy-prometheus: $(BUILD_FOLDER) deploy-namespace
	python3 ./tools/deploy_prometheus.py --target $(TARGET) --namespace "$(NAMESPACE)"

deploy-grafana: $(BUILD_FOLDER)
	python3 ./tools/deploy_grafana.py --target $(TARGET) --namespace "$(NAMESPACE)"

deploy-monitoring: deploy-olm deploy-prometheus deploy-grafana

unit-test:
	docker stop postgres || true
	docker run -d  --rm --name postgres -e POSTGRES_PASSWORD=admin -e POSTGRES_USER=admin -p 127.0.0.1:5432:5432 postgres:12.3-alpine -c 'max_connections=10000'
	until PGPASSWORD=admin pg_isready -U admin --dbname postgres --host 127.0.0.1 --port 5432; do sleep 1; done
	SKIP_UT_DB=1 go test -v $(or ${TEST}, ${TEST}, $(shell go list ./... | grep -v subsystem)) -cover || (docker stop postgres && /bin/false)
	docker stop postgres

test-onprem:
	INVENTORY=127.0.0.1:8090 \
	DB_HOST=127.0.0.1 \
	DB_PORT=5432 \
	go test -v ./subsystem/... -count=1 -ginkgo.focus=${FOCUS} -ginkgo.v

#########
# Clean #
#########

clear-all: clean subsystem-clean clear-deployment

clean:
	-rm -rf $(BUILD_FOLDER)

subsystem-clean:
	-$(KUBECTL) get pod -o name | grep create-image | xargs $(KUBECTL) delete 1> /dev/null || true
	-$(KUBECTL) get pod -o name | grep ignition-generator | xargs $(KUBECTL) delete 1> /dev/null || true

clear-deployment:
	-python3 ./tools/clear_deployment.py --delete-namespace $(APPLY_NAMESPACE) --namespace "$(NAMESPACE)" || true

clean-onprem:
	podman pod rm -f assisted-installer
	podman volume rm s3-volume
