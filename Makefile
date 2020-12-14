NAMESPACE := $(or ${NAMESPACE},assisted-installer)

PWD = $(shell pwd)
UID = $(shell id -u)
BUILD_FOLDER = $(PWD)/build/$(NAMESPACE)
ROOT_DIR = $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

TARGET := $(or ${TARGET},minikube)
PROFILE := $(or $(PROFILE),minikube)
KUBECTL=kubectl -n $(NAMESPACE)

ifeq ($(TARGET), minikube)
ifdef E2E_TESTS_MODE
E2E_TESTS_CONFIG = --img-expr-time=5m --img-expr-interval=5m
endif
define get_service
minikube -p $(PROFILE) service --url $(1) -n $(NAMESPACE) | sed 's/http:\/\///g'
endef # get_service
else
define get_service
kubectl get service $(1) -n $(NAMESPACE) | grep $(1) | awk '{print $$4 ":" $$5}' | \
	awk '{split($$0,a,":"); print a[1] ":" a[2]}'
endef # get_service
endif # TARGET

ASSISTED_ORG := $(or ${ASSISTED_ORG},quay.io/ocpmetal)
ASSISTED_TAG := $(or ${ASSISTED_TAG},latest)

export SERVICE := $(or ${SERVICE},${ASSISTED_ORG}/assisted-service:${ASSISTED_TAG})
export ISO_CREATION := $(or ${ISO_CREATION},${ASSISTED_ORG}/assisted-iso-create:${ASSISTED_TAG})
CONTAINER_BUILD_PARAMS = --network=host --label git_revision=${GIT_REVISION} ${CONTAINER_BUILD_EXTRA_PARAMS}

# RHCOS_VERSION should be consistent with BaseObjectName in pkg/s3wrapper/client.go
RHCOS_VERSION := $(or ${RHCOS_VERSION},46.82.202009222340-0)
BASE_OS_IMAGE := $(or ${BASE_OS_IMAGE},https://releases-art-rhcos.svc.ci.openshift.org/art/storage/releases/rhcos-4.6/${RHCOS_VERSION}/x86_64/rhcos-${RHCOS_VERSION}-live.x86_64.iso)
OPENSHIFT_INSTALL_RELEASE_IMAGE := $(or ${OPENSHIFT_INSTALL_RELEASE_IMAGE},quay.io/openshift-release-dev/ocp-release:4.6.4-x86_64)
DUMMY_IGNITION := $(or ${DUMMY_IGNITION},False)
GIT_REVISION := $(shell git rev-parse HEAD)
PUBLISH_TAG := $(or ${GIT_REVISION})
APPLY_NAMESPACE := $(or ${APPLY_NAMESPACE},True)
ROUTE53_SECRET := ${ROUTE53_SECRET}
OCM_CLIENT_ID := ${OCM_CLIENT_ID}
OCM_CLIENT_SECRET := ${OCM_CLIENT_SECRET}
ENABLE_AUTH := $(or ${ENABLE_AUTH},False)
DELETE_PVC := $(or ${DELETE_PVC},False)
PUBLIC_CONTAINER_REGISTRIES := $(or ${PUBLIC_CONTAINER_REGISTRIES},quay.io)
PODMAN_PULL_FLAG := $(or ${PODMAN_PULL_FLAG},--pull always)

# We decided to have an option to change replicas count only while running in minikube
# That line is checking if we run on minikube
# check if SERVICE_REPLICAS_COUNT was set and if yes change default value to required one
REPLICAS_COUNT = $(shell if ! [ "${TARGET}" = "minikube" ];then echo 3; else echo $(or ${SERVICE_REPLICAS_COUNT},3);fi)

ifdef INSTALLATION_TIMEOUT
        INSTALLATION_TIMEOUT_FLAG = --installation-timeout $(INSTALLATION_TIMEOUT)
endif

# define focus flag for test so users can run individual tests or suites
ifdef FOCUS
		GINKGO_FOCUS_FLAG = -ginkgo.focus="$(FOCUS)"
endif
REPORTS = $(ROOT_DIR)/reports
TEST_PUBLISH_FLAGS = --junitfile-testsuite-name=relative --junitfile-testcase-classname=relative --junitfile $(REPORTS)/unittest.xml

all: build

ci-lint:
	${ROOT_DIR}/tools/check-commits.sh
	$(MAKE) verify-latest-onprem-config

lint:
	golangci-lint run -v

$(BUILD_FOLDER):
	mkdir -p $(BUILD_FOLDER)

format:
	goimports -w -l cmd/ internal/ subsystem/ pkg/ assisted-iso-create/
	gofmt -w -l cmd/ internal/ subsystem/ pkg/ assisted-iso-create/

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

generate-keys: $(BUILD_FOLDER)
	cd tools && go run auth_keys_generator.go -keys-dir=$(BUILD_FOLDER)

generate-migration:
	go run tools/migration_generator/migration_generator.go -name=$(MIGRATION_NAME)

##################
# Build & Update #
##################

.PHONY: build docs
build: lint unit-test build-minimal build-iso-generator

build-all: build-in-docker

build-in-docker:
	skipper make build-image build-minimal-assisted-iso-generator-image

build-minimal: $(BUILD_FOLDER)
	CGO_ENABLED=0 go build -o $(BUILD_FOLDER)/assisted-service cmd/main.go

build-iso-generator: $(BUILD_FOLDER)
	CGO_ENABLED=0 go build -o $(BUILD_FOLDER)/assisted-iso-create assisted-iso-create/main.go

build-image: build
	docker build $(CONTAINER_BUILD_PARAMS) -f Dockerfile.assisted-service . -t $(SERVICE)

build-assisted-iso-generator-image: lint unit-test build-minimal build-minimal-assisted-iso-generator-image

build-minimal-assisted-iso-generator-image:
	docker build $(CONTAINER_BUILD_PARAMS) --build-arg OS_IMAGE=$(BASE_OS_IMAGE) \
 		-f Dockerfile.assisted-iso-create . -t $(ISO_CREATION)

update-service:
	skipper make build-image
	docker push $(SERVICE)

update: build-all
	docker push $(SERVICE)
	docker push $(ISO_CREATION)

update-minimal: build-minimal
	docker build $(CONTAINER_BUILD_PARAMS) -f Dockerfile.assisted-service . -t $(SERVICE)

_update-minikube: build
	eval $$(SHELL=$${SHELL:-/bin/sh} minikube -p $(PROFILE) docker-env) && \
		docker build $(CONTAINER_BUILD_PARAMS) -f Dockerfile.assisted-service . -t $(SERVICE) && \
		docker build $(CONTAINER_BUILD_PARAMS) --build-arg OS_IMAGE=$(BASE_OS_IMAGE) -f Dockerfile.assisted-iso-create . -t $(ISO_CREATION)

define publish_image
	${1} tag ${2} ${3}
	${1} push ${3}
endef # publish_image

publish:
	$(call publish_image,docker,${SERVICE},quay.io/ocpmetal/assisted-service:${PUBLISH_TAG})
	$(call publish_image,docker,${ISO_CREATION},quay.io/ocpmetal/assisted-iso-create:${PUBLISH_TAG})

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

_verify_minikube:
	minikube status

deploy-all: $(BUILD_FOLDER) deploy-namespace deploy-postgres deploy-s3 deploy-ocm-secret deploy-route53 deploy-service
	echo "Deployment done"

deploy-ui: deploy-namespace
	python3 ./tools/deploy_ui.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --namespace "$(NAMESPACE)" --profile "$(PROFILE)" $(DEPLOY_TAG_OPTION)

deploy-namespace: $(BUILD_FOLDER)
	python3 ./tools/deploy_namespace.py --deploy-namespace $(APPLY_NAMESPACE) --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)"

deploy-s3-secret:
	python3 ./tools/deploy_scality_configmap.py --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)"

deploy-s3: deploy-namespace
	python3 ./tools/deploy_s3.py --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)"
	sleep 5;  # wait for service to get an address
	make deploy-s3-secret

deploy-route53: deploy-namespace
	python3 ./tools/deploy_route53.py --secret "$(ROUTE53_SECRET)" --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)"

deploy-ocm-secret: deploy-namespace
	python3 ./tools/deploy_sso_secret.py --secret "$(OCM_CLIENT_SECRET)" --id "$(OCM_CLIENT_ID)" --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)"

deploy-inventory-service-file: deploy-namespace
	python3 ./tools/deploy_inventory_service.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --namespace "$(NAMESPACE)" --profile "$(PROFILE)"
	sleep 5;  # wait for service to get an address

deploy-service-requirements: deploy-namespace deploy-inventory-service-file
	python3 ./tools/deploy_assisted_installer_configmap.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" \
		--base-dns-domains "$(BASE_DNS_DOMAINS)" --namespace "$(NAMESPACE)" --profile "$(PROFILE)" \
		$(INSTALLATION_TIMEOUT_FLAG) $(DEPLOY_TAG_OPTION) --enable-auth "$(ENABLE_AUTH)" $(TEST_FLAGS) \
		--ocp-release $(OPENSHIFT_INSTALL_RELEASE_IMAGE) --public-registries "$(PUBLIC_CONTAINER_REGISTRIES)" \
		$(E2E_TESTS_CONFIG)

deploy-service: deploy-namespace deploy-service-requirements deploy-role
	python3 ./tools/deploy_assisted_installer.py $(DEPLOY_TAG_OPTION) --namespace "$(NAMESPACE)" \
		--profile "$(PROFILE)" $(TEST_FLAGS) --target "$(TARGET)" --replicas-count $(REPLICAS_COUNT)
	python3 ./tools/wait_for_assisted_service.py --target $(TARGET) --namespace "$(NAMESPACE)" \
		--profile "$(PROFILE)" --domain "$(INGRESS_DOMAIN)"

deploy-role: deploy-namespace
	python3 ./tools/deploy_role.py --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)"

deploy-postgres: deploy-namespace
	python3 ./tools/deploy_postgres.py --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)"

deploy-service-on-ocp-cluster:
	export TARGET=ocp && $(MAKE) deploy-postgres deploy-ocm-secret deploy-s3-secret deploy-service

deploy-ui-on-ocp-cluster:
	export TARGET=ocp && $(MAKE) deploy-ui

jenkins-deploy-for-subsystem: _verify_minikube generate-keys
	export TEST_FLAGS=--subsystem-test && export ENABLE_AUTH="True" && export DUMMY_IGNITION=${DUMMY_IGNITION} && \
	$(MAKE) deploy-wiremock deploy-all

deploy-test: _verify_minikube generate-keys
	export ASSISTED_ORG=minikube-local-registry && export ASSISTED_TAG=minikube-test && export TEST_FLAGS=--subsystem-test && \
	export ENABLE_AUTH="True" && export DUMMY_IGNITION="True" && \
	$(MAKE) _update-minikube deploy-wiremock deploy-all

generate-onprem-environment:
	sed -i "s|OPENSHIFT_INSTALL_RELEASE_IMAGE=.*|OPENSHIFT_INSTALL_RELEASE_IMAGE=${OPENSHIFT_INSTALL_RELEASE_IMAGE}|" onprem-environment
	sed -i "s|PUBLIC_CONTAINER_REGISTRIES=.*|PUBLIC_CONTAINER_REGISTRIES=${PUBLIC_CONTAINER_REGISTRIES}|" onprem-environment

generate-onprem-iso-ignition:
	sed -i "s|OPENSHIFT_INSTALL_RELEASE_IMAGE=.*|OPENSHIFT_INSTALL_RELEASE_IMAGE=${OPENSHIFT_INSTALL_RELEASE_IMAGE}|" ./config/onprem-iso-fcc.yaml
	sed -i "s|PUBLIC_CONTAINER_REGISTRIES=.*|PUBLIC_CONTAINER_REGISTRIES=${PUBLIC_CONTAINER_REGISTRIES}|" ./config/onprem-iso-fcc.yaml
	podman run --rm -v ./config/onprem-iso-fcc.yaml:/config.fcc:z quay.io/coreos/fcct:release --pretty --strict /config.fcc > ./config/onprem-iso-config.ign

verify-latest-onprem-config: generate-onprem-environment generate-onprem-iso-ignition
	@echo "Verifying onprem config changes"
	hack/verify-latest-onprem-config.sh

# $SERVICE is built with docker. If we want the latest version of $SERVICE
# we need to pull it from the docker daemon before deploy-onprem.
podman-pull-service-from-docker-daemon:
	podman pull "docker-daemon:${SERVICE}"

deploy-onprem:
	# Format: ip:hostPort:containerPort | ip::containerPort | hostPort:containerPort | containerPort
	podman pod create --name assisted-installer -p 5432:5432,8000:8000,8090:8090,8080:8080
	# These are required because when running on RHCOS livecd, the coreos-installer binary and
	# livecd are bind-mounted from the host into the assisted-service container at runtime.
	[ -f livecd.iso ] || curl $(BASE_OS_IMAGE) --retry 5 -o livecd.iso
	[ -f coreos-installer ] || podman run --privileged --pull=always -it --rm \
		-v .:/data -w /data --entrypoint /bin/bash \
		quay.io/coreos/coreos-installer:v0.7.0 -c 'cp /usr/sbin/coreos-installer /data/coreos-installer'
	podman run -dt --pod assisted-installer --env-file onprem-environment --pull always --name db quay.io/ocpmetal/postgresql-12-centos7
	podman run -dt --pod assisted-installer --env-file onprem-environment --pull always -v $(PWD)/deploy/ui/nginx.conf:/opt/bitnami/nginx/conf/server_blocks/nginx.conf:z --name ui quay.io/ocpmetal/ocp-metal-ui:latest
	podman run -dt --pod assisted-installer --env-file onprem-environment ${PODMAN_PULL_FLAG} --env DUMMY_IGNITION=$(DUMMY_IGNITION) \
		-v ./livecd.iso:/data/livecd.iso:z \
		-v ./coreos-installer:/data/coreos-installer:z \
		--restart always --name installer $(SERVICE)

deploy-onprem-for-subsystem:
	export DUMMY_IGNITION="true" && $(MAKE) deploy-onprem

docs:
	mkdocs build

docs_serve:
	mkdocs serve

########
# Test #
########

subsystem-run: test subsystem-clean

test:
	INVENTORY=$(shell $(call get_service,assisted-service) | sed 's/http:\/\///g') \
		DB_HOST=$(shell $(call get_service,postgres) | sed 's/http:\/\///g' | cut -d ":" -f 1) \
		DB_PORT=$(shell $(call get_service,postgres) | sed 's/http:\/\///g' | cut -d ":" -f 2) \
		OCM_HOST=$(shell $(call get_service,wiremock) | sed 's/http:\/\///g') \
		TEST_TOKEN="$(shell cat $(BUILD_FOLDER)/auth-tokenString)" \
		TEST_TOKEN_ADMIN="$(shell cat $(BUILD_FOLDER)/auth-tokenAdminString)" \
		TEST_TOKEN_UNALLOWED="$(shell cat $(BUILD_FOLDER)/auth-tokenUnallowedString)" \
		ENABLE_AUTH="true" \
		go test -v ./subsystem/... -count=1 $(GINKGO_FOCUS_FLAG) -ginkgo.v -timeout 120m

deploy-wiremock: deploy-namespace
	python3 ./tools/deploy_wiremock.py --target $(TARGET) --namespace "$(NAMESPACE)" --profile "$(PROFILE)"

deploy-olm: deploy-namespace
	python3 ./tools/deploy_olm.py --target $(TARGET) --profile $(PROFILE)

deploy-prometheus: $(BUILD_FOLDER) deploy-namespace
	python3 ./tools/deploy_prometheus.py --target $(TARGET) --namespace "$(NAMESPACE)" --profile "$(PROFILE)"

deploy-grafana: $(BUILD_FOLDER)
	python3 ./tools/deploy_grafana.py --target $(TARGET) --namespace "$(NAMESPACE)" --profile "$(PROFILE)"

deploy-monitoring: deploy-olm deploy-prometheus deploy-grafana

unit-test: $(REPORTS)
	docker ps -q --filter "name=postgres" | xargs -r docker kill && sleep 3
	docker run -d  --rm --name postgres -e POSTGRES_PASSWORD=admin -e POSTGRES_USER=admin -p 127.0.0.1:5432:5432 \
		quay.io/ocpmetal/postgres:12.3-alpine -c 'max_connections=10000'
	timeout 1m bash -c "until PGPASSWORD=admin pg_isready -U admin --dbname postgres --host 127.0.0.1 --port 5432; do sleep 1; done"
	SKIP_UT_DB=1 gotestsum --format=pkgname $(TEST_PUBLISH_FLAGS) -- -cover -coverprofile=$(REPORTS)/coverage.out $(or ${TEST},${TEST},$(shell go list ./... | grep -v subsystem)) $(GINKGO_FOCUS_FLAG) \
		-ginkgo.v -timeout 30m -count=1 || (docker kill postgres && /bin/false)
	gocov convert $(REPORTS)/coverage.out | gocov-xml > $(REPORTS)/coverage.xml
	docker kill postgres

$(REPORTS):
	-mkdir -p $(REPORTS)

test-onprem:
	INVENTORY=127.0.0.1:8090 \
	DB_HOST=127.0.0.1 \
	DB_PORT=5432 \
	DEPLOY_TARGET=onprem \
	go test -v ./subsystem/... -count=1 $(GINKGO_FOCUS_FLAG) -ginkgo.v -timeout 30m

#########
# Clean #
#########

clear-all: clean subsystem-clean clear-deployment clear-images clean-onprem

clean:
	-rm -rf $(BUILD_FOLDER) $(REPORTS)

subsystem-clean:
	-$(KUBECTL) get pod -o name | grep createimage | xargs -r $(KUBECTL) delete --force --grace-period=0 1> /dev/null || true

clear-deployment:
	-python3 ./tools/clear_deployment.py --delete-namespace $(APPLY_NAMESPACE) --delete-pvc $(DELETE_PVC) --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)" || true

clear-images:
	-docker rmi -f $(SERVICE)
	-docker rmi -f $(ISO_CREATION)

clean-onprem:
	podman pod rm -f assisted-installer | true
	rm livecd.iso | true
	rm coreos-installer | true

delete-minikube-profile:
	minikube delete -p $(PROFILE)

delete-all-minikube-profiles:
	minikube delete --all
