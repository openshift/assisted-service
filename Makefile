NAMESPACE := $(or ${NAMESPACE},assisted-installer)

PWD = $(shell pwd)
UID = $(shell id -u)
BUILD_FOLDER = $(PWD)/build/$(NAMESPACE)
ROOT_DIR = $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

TARGET := $(or ${TARGET},minikube)
PROFILE := $(or $(PROFILE),minikube)
KUBECTL=kubectl -n $(NAMESPACE)

ifeq ($(TARGET), minikube)
define get_service
minikube -p $(PROFILE) service --url $(1) -n $(NAMESPACE) | sed 's/http:\/\///g'
endef # get_service
else
define get_service
kubectl get service $(1) -n $(NAMESPACE) | grep $(1) | awk '{print $$4 ":" $$5}' | \
	awk '{split($$0,a,":"); print a[1] ":" a[2]}'
endef # get_service
endif # TARGET

SERVICE := $(or ${SERVICE},quay.io/ocpmetal/assisted-service:latest)
SERVICE_ONPREM := $(or ${SERVICE_ONPREM},quay.io/ocpmetal/assisted-service-onprem:latest)
ISO_CREATION := $(or ${ISO_CREATION},quay.io/ocpmetal/assisted-iso-create:latest)
BASE_OS_IMAGE := $(or ${BASE_OS_IMAGE},https://releases-art-rhcos.svc.ci.openshift.org/art/storage/releases/rhcos-4.6/46.82.202009222340-0/x86_64/rhcos-46.82.202009222340-0-live.x86_64.iso)
DUMMY_IGNITION := $(or ${DUMMY_IGNITION},minikube-local-registry/ignition-dummy-generator:minikube-test)
OPENSHIFT_INSTALL_RELEASE_IMAGE := $(or ${OPENSHIFT_INSTALL_RELEASE_IMAGE},quay.io/ocpmetal/ocp-release:4.6.0-0.nightly-2020-08-31-220837)
GIT_REVISION := $(shell git rev-parse HEAD)
APPLY_NAMESPACE := $(or ${APPLY_NAMESPACE},True)
ROUTE53_SECRET := ${ROUTE53_SECRET}
OCM_CLIENT_ID := ${OCM_CLIENT_ID}
OCM_CLIENT_SECRET := ${OCM_CLIENT_SECRET}
ENABLE_AUTH := $(or ${ENABLE_AUTH},False)
DELETE_PVC := $(or ${DELETE_PVC},False)
ISO_CREATION_DOCKER_DAEMON_PULL_STRING := $(or ${ISO_CREATION_DOCKER_DAEMON_PULL_STRING},docker-daemon:${ISO_CREATION})
ASSISTED_SERVICE_DOCKER_DAEMON_PULL_STRING := $(or ${ASSISTED_SERVICE_DOCKER_DAEMON_PULL_STRING},docker-daemon:${SERVICE})

# We decided to have an option to change replicas count only while running in minikube
# That line is checking if we run on minikube
# check if SERVICE_REPLICAS_COUNT was set and if yes change default value to required one
REPLICAS_COUNT = $(shell if ! [ "${TARGET}" = "minikube" ];then echo 3; else echo $(or ${SERVICE_REPLICAS_COUNT},3);fi)

ifdef INSTALLATION_TIMEOUT
        INSTALLATION_TIMEOUT_FLAG = --installation-timeout $(INSTALLATION_TIMEOUT)
endif

# define focus flag for test so users can run individual tests or suites
ifdef FOCUS
        GINKGO_FOCUS_FLAG = -ginkgo.focus=${FOCUS}
endif


all: build

ci-lint:
	${ROOT_DIR}/tools/check-commits.sh

lint:
	golangci-lint run -v

$(BUILD_FOLDER):
	mkdir -p $(BUILD_FOLDER)

format:
	goimports -w -l cmd/ internal/ subsystem/ pkg/ assisted-iso-create/ dummy-ignition/
	gofmt -w -l cmd/ internal/ subsystem/ pkg/ assisted-iso-create/ dummy-ignition/

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

##################
# Build & Update #
##################

.PHONY: build
build: lint unit-test build-minimal build-iso-generator

build-minimal: $(BUILD_FOLDER)
	CGO_ENABLED=0 go build -o $(BUILD_FOLDER)/assisted-service cmd/main.go

build-iso-generator: $(BUILD_FOLDER)
	CGO_ENABLED=0 go build -o $(BUILD_FOLDER)/assisted-iso-create assisted-iso-create/main.go

build-dummy-ignition: $(BUILD_FOLDER)
	CGO_ENABLED=0 go build -o $(BUILD_FOLDER)/dummy-ignition dummy-ignition/main.go

build-onprem-dependencies: 
	skipper make build-image build-assisted-iso-generator-image

build-onprem: build-onprem-dependencies
	podman pull $(ISO_CREATION_DOCKER_DAEMON_PULL_STRING)
	podman pull $(ASSISTED_SERVICE_DOCKER_DAEMON_PULL_STRING)
	GIT_REVISION=${GIT_REVISION} podman build --network=host --build-arg GIT_REVISION \
 		-f Dockerfile.assisted-service-onprem . -t $(SERVICE_ONPREM)

build-image: build
	GIT_REVISION=${GIT_REVISION} docker build --network=host --build-arg GIT_REVISION \
 		-f Dockerfile.assisted-service . -t $(SERVICE)

build-assisted-iso-generator-image: lint unit-test build-minimal build-minimal-assisted-iso-generator-image

build-minimal-assisted-iso-generator-image: build-iso-generator
	GIT_REVISION=${GIT_REVISION} docker build --network=host --build-arg GIT_REVISION --build-arg NAMESPACE=$(NAMESPACE) --build-arg OS_IMAGE=$(BASE_OS_IMAGE) \
 		-f Dockerfile.assisted-iso-create . -t $(ISO_CREATION)

build-dummy-ignition-image: build-dummy-ignition
	docker build --network=host --build-arg NAMESPACE=$(NAMESPACE) -f Dockerfile.ignition-dummy . -t ${DUMMY_IGNITION}

update: build-image
	docker push $(SERVICE)

update-minimal: build-minimal
	GIT_REVISION=${GIT_REVISION} docker build --network=host --build-arg GIT_REVISION \
		-f Dockerfile.assisted-service . -t $(SERVICE)

update-minikube: build build-dummy-ignition
	eval $$(SHELL=$${SHELL:-/bin/sh} minikube -p $(PROFILE) docker-env) && \
		GIT_REVISION=${GIT_REVISION} docker build --network=host --build-arg GIT_REVISION \
		-f Dockerfile.assisted-service . -t $(SERVICE) && docker build --network=host -f Dockerfile.ignition-dummy . -t ${DUMMY_IGNITION} \
		&& docker build --network=host --build-arg GIT_REVISION --build-arg OS_IMAGE=$(BASE_OS_IMAGE) -f Dockerfile.assisted-iso-create . -t $(ISO_CREATION)

define publish_image
	docker tag ${1} ${2}
	docker push ${2}
endef # publish_image

publish:
	$(call publish_image,${SERVICE},quay.io/ocpmetal/assisted-service:latest)
	$(call publish_image,${SERVICE},quay.io/ocpmetal/assisted-service:${GIT_REVISION})
	$(call publish_image,${ISO_CREATION},quay.io/ocpmetal/assisted-iso-create:latest)
	$(call publish_image,${ISO_CREATION},quay.io/ocpmetal/assisted-iso-create:${GIT_REVISION})

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
		--ocp-release $(OPENSHIFT_INSTALL_RELEASE_IMAGE)

deploy-service: deploy-namespace deploy-service-requirements deploy-role
	python3 ./tools/deploy_assisted_installer.py $(DEPLOY_TAG_OPTION) --namespace "$(NAMESPACE)" \
		--profile "$(PROFILE)" $(TEST_FLAGS) --target "$(TARGET)" --replicas-count $(REPLICAS_COUNT)
	python3 ./tools/wait_for_assisted_service.py --target $(TARGET) --namespace "$(NAMESPACE)" \
		--profile "$(PROFILE)" --domain "$(INGRESS_DOMAIN)"

deploy-role: deploy-namespace
	python3 ./tools/deploy_role.py --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)"

deploy-postgres: deploy-namespace
	python3 ./tools/deploy_postgres.py --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)"

jenkins-deploy-for-subsystem: generate-keys build-dummy-ignition-image
	export TEST_FLAGS=--subsystem-test && export ENABLE_AUTH="True" && export DUMMY_IGNITION=${DUMMY_IGNITION} && $(MAKE) deploy-wiremock deploy-all

deploy-test: generate-keys
	export SERVICE=minikube-local-registry/assisted-service:minikube-test && export TEST_FLAGS=--subsystem-test && export ENABLE_AUTH="True" \
	&& export DUMMY_IGNITION=${DUMMY_IGNITION} && ISO_CREATION=minikube-local-registry/assisted-iso-create:minikube-test \
	$(MAKE) update-minikube deploy-wiremock deploy-all

deploy-onprem:
	podman pod create --name assisted-installer -p 5432,8000,8090,8080
	podman run -dt --pod assisted-installer --env-file onprem-environment --name db quay.io/ocpmetal/postgresql-12-centos7
	podman run -dt --pod assisted-installer --env-file onprem-environment --user assisted-installer  --restart always --name installer $(SERVICE_ONPREM)
	podman run -dt --pod assisted-installer --env-file onprem-environment --pull always -v $(PWD)/deploy/ui/nginx.conf:/opt/bitnami/nginx/conf/server_blocks/nginx.conf:z --name ui quay.io/ocpmetal/ocp-metal-ui:latest

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
		go test -v ./subsystem/... -count=1 $(GINKGO_FOCUS_FLAG) -ginkgo.v -timeout 30m

deploy-wiremock: deploy-namespace
	python3 ./tools/deploy_wiremock.py --target $(TARGET) --namespace "$(NAMESPACE)" --profile "$(PROFILE)"

deploy-olm: deploy-namespace
	python3 ./tools/deploy_olm.py --target $(TARGET) --profile $(PROFILE)

deploy-prometheus: $(BUILD_FOLDER) deploy-namespace
	python3 ./tools/deploy_prometheus.py --target $(TARGET) --namespace "$(NAMESPACE)" --profile "$(PROFILE)"

deploy-grafana: $(BUILD_FOLDER)
	python3 ./tools/deploy_grafana.py --target $(TARGET) --namespace "$(NAMESPACE)" --profile "$(PROFILE)"

deploy-monitoring: deploy-olm deploy-prometheus deploy-grafana

unit-test:
	docker kill postgres || true
	sleep 3
	docker run -d  --rm --name postgres -e POSTGRES_PASSWORD=admin -e POSTGRES_USER=admin -p 127.0.0.1:5432:5432 postgres:12.3-alpine -c 'max_connections=10000'
	until PGPASSWORD=admin pg_isready -U admin --dbname postgres --host 127.0.0.1 --port 5432; do sleep 1; done
	SKIP_UT_DB=1 go test -v $(or ${TEST}, ${TEST}, $(shell go list ./... | grep -v subsystem)) $(GINKGO_FOCUS_FLAG) -cover || (docker kill postgres && /bin/false)
	docker kill postgres

test-onprem:
	INVENTORY=127.0.0.1:8090 \
	INVENTORY=127.0.0.1:8090 \
	DB_HOST=127.0.0.1 \
	DB_PORT=5432 \
	go test -v ./subsystem/... -count=1 $(GINKGO_FOCUS_FLAG) -ginkgo.v -timeout 30m

#########
# Clean #
#########

clear-all: clean subsystem-clean clear-deployment

clean:
	-rm -rf $(BUILD_FOLDER)

subsystem-clean:
	-$(KUBECTL) get pod -o name | grep dummyimage | xargs -r $(KUBECTL) delete 1> /dev/null || true
	-$(KUBECTL) get pod -o name | grep createimage | xargs -r $(KUBECTL) delete 1> /dev/null || true
	-$(KUBECTL) get pod -o name | grep ignition-generator | xargs -r $(KUBECTL) delete 1> /dev/null || true

clear-deployment:
	-python3 ./tools/clear_deployment.py --delete-namespace $(APPLY_NAMESPACE) --delete-pvc $(DELETE_PVC) --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)" || true

clean-onprem:
	podman pod rm -f -i assisted-installer

delete-minikube-profile:
	minikube delete -p $(PROFILE)

delete-all-minikube-profiles:
	minikube delete --all
