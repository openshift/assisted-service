include hack/Makefile

NAMESPACE := $(or ${NAMESPACE},assisted-installer)
PWD = $(shell pwd)
BUILD_FOLDER = $(PWD)/build/$(NAMESPACE)
ROOT_DIR := $(or ${ROOT_DIR},$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST)))))
CONTAINER_COMMAND := $(shell hack/utils.sh get_container_runtime_command)

GO_BUILD_TAGS = $(or ${BUILD_TAGS}, "strictfipsruntime")

# Selecting the right podman-remote version since podman-remote4 cannot work against podman-server3 and vice versa.
# It must be occurred before any other container related task.
# Makefile syntax force us to assign the shell result to a variable - please ignore it.
PODMAN_CLIENT_SELECTION_IGNORE_ME := $(shell hack/utils.sh select_podman_client)

ifeq ($(CONTAINER_COMMAND), docker)
	CONTAINER_COMMAND = $(shell docker -v 2>/dev/null | cut -f1 -d' ' | tr '[:upper:]' '[:lower:]')
endif

TARGET := $(or ${TARGET},kind)
KUBECTL=kubectl -n $(NAMESPACE)

define get_service_host_port
kubectl get service $(1) -n $(NAMESPACE) -ojson | jq --from-file ./hack/k8s_service_host_port.jq --raw-output
endef # get_service_host_port

ifdef E2E_TESTS_MODE
E2E_TESTS_CONFIG = --img-expr-time=5m --img-expr-interval=5m
endif

ifneq (,$(findstring podman,$(CONTAINER_COMMAND)))
	PUSH_FLAGS = --tls-verify=false
endif

ASSISTED_ORG := $(or ${ASSISTED_ORG},quay.io/edge-infrastructure)
ASSISTED_TAG := $(or ${ASSISTED_TAG},latest)

DEBUG_SERVICE_PORT := $(or ${DEBUG_SERVICE_PORT},40000)
SERVICE := $(or ${SERVICE},${ASSISTED_ORG}/assisted-service:${ASSISTED_TAG})
IMAGE_SERVICE := $(or ${IMAGE_SERVICE},${ASSISTED_ORG}/assisted-image-service:${ASSISTED_TAG})
ASSISTED_UI := $(or ${ASSISTED_UI},${ASSISTED_ORG}/assisted-installer-ui:${ASSISTED_TAG})
PSQL_IMAGE := $(or ${PSQL_IMAGE},quay.io/sclorg/postgresql-12-c8s:latest)
BUNDLE_IMAGE := $(or ${BUNDLE_IMAGE},${ASSISTED_ORG}/assisted-service-operator-bundle:${ASSISTED_TAG})
INDEX_IMAGE := $(or ${INDEX_IMAGE},${ASSISTED_ORG}/assisted-service-index:${ASSISTED_TAG})

ifdef ENABLE_EVENT_STREAMING
	EVENT_STREAMING_OPTIONS= --enable-event-stream=true
else
	EVENT_STREAMING_OPTIONS=
endif

ifeq ("${DEBUG_SERVICE}", "true")
	DEBUG_ARGS=-gcflags "all=-N -l"
	DEBUG_PORT_OPTIONS= --port ${DEBUG_SERVICE_PORT} debug-port
	UPDATE_IMAGE=update-debug-minimal
else
	UPDATE_IMAGE=update-minimal
endif

CONTAINER_BUILD_PARAMS = --network=host --label git_revision=${GIT_REVISION} ${CONTAINER_BUILD_EXTRA_PARAMS}

MUST_GATHER_IMAGES := $(or ${MUST_GATHER_IMAGES}, $(shell (tr -d '\n\t ' < ${ROOT_DIR}/data/default_must_gather_versions.json)))
DUMMY_IGNITION := $(or ${DUMMY_IGNITION},False)
GIT_REVISION := $(shell git rev-parse HEAD)
APPLY_MANIFEST := $(or ${APPLY_MANIFEST},True)
APPLY_NAMESPACE := $(or ${APPLY_NAMESPACE},True)
ROUTE53_SECRET := ${ROUTE53_SECRET}
OCM_CLIENT_ID := ${OCM_CLIENT_ID}
OCM_CLIENT_SECRET := ${OCM_CLIENT_SECRET}
JWKS_URL := $(or ${JWKS_URL},https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/certs)
AUTH_TYPE := $(or ${AUTH_TYPE},none)
CHECK_CLUSTER_VERSION := $(or ${CHECK_CLUSTER_VERSION},False)
ENABLE_SINGLE_NODE_DNSMASQ := $(or ${ENABLE_SINGLE_NODE_DNSMASQ},True)
DISK_ENCRYPTION_SUPPORT := $(or ${DISK_ENCRYPTION_SUPPORT},True)
DELETE_PVC := $(or ${DELETE_PVC},False)
TESTING_PUBLIC_CONTAINER_REGISTRIES := quay.io
PUBLIC_CONTAINER_REGISTRIES := $(or ${PUBLIC_CONTAINER_REGISTRIES},$(TESTING_PUBLIC_CONTAINER_REGISTRIES))
PODMAN_PULL_FLAG := $(or ${PODMAN_PULL_FLAG},--pull always)
ENABLE_KUBE_API := $(or ${ENABLE_KUBE_API},false)
STORAGE := $(or ${STORAGE},s3)
GENERATE_CRD := $(or ${GENERATE_CRD},true)
PERSISTENT_STORAGE := $(or ${PERSISTENT_STORAGE},True)
IPV6_SUPPORT := $(or ${IPV6_SUPPORT}, True)
ISO_IMAGE_TYPE := $(or ${ISO_IMAGE_TYPE},full-iso)
MIRROR_REG_CA_FILE = mirror_ca.crt
REGISTRIES_FILE_PATH = registries.conf
MIRROR_REGISTRY_SUPPORT := $(or ${MIRROR_REGISTRY_SUPPORT},False)
HW_REQUIREMENTS := $(or ${HW_REQUIREMENTS}, $(shell cat $(ROOT_DIR)/data/default_hw_requirements.json | tr -d "\n\t "))
DISABLED_HOST_VALIDATIONS := $(or ${DISABLED_HOST_VALIDATIONS}, "")
DISABLED_STEPS := $(or ${DISABLED_STEPS}, "")
DISABLE_TLS := $(or ${DISABLE_TLS},false)
ENABLE_ORG_TENANCY := $(or ${ENABLE_ORG_TENANCY},False)
ALLOW_CONVERGED_FLOW := $(or ${ALLOW_CONVERGED_FLOW}, false)
ENABLE_ORG_BASED_FEATURE_GATES := $(or ${ENABLE_ORG_BASED_FEATURE_GATES},False)
KIND_EXPERIMENTAL_PROVIDER=podman
USE_LOCAL_SERVICE := $(or ${USE_LOCAL_SERVICE}, false)
LOCAL_ASSISTED_SERVICE_IMAGE=localhost/assisted-service:latest
LOCAL_IMAGE_ARCHIVE=build/assisted_service_image.tar
HUB_CLUSTER_NAME := $(or ${HUB_CLUSTER_NAME}, assisted-hub-cluster)
NVIDIA_REQUIRE_GPU := $(or ${NVIDIA_REQUIRE_GPU}, true)
AMD_REQUIRE_GPU := $(or ${AMD_REQUIRE_GPU}, true)


ifeq ($(DISABLE_TLS),true)
	DISABLE_TLS_CMD := --disable-tls
endif

PODMAN_CONFIGMAP := deploy/podman/configmap.yml
OKD := $(or ${OKD},false)
ifeq ($(OKD),true)
	PODMAN_CONFIGMAP := deploy/podman/okd-configmap.yml
endif

ifeq ($(ENABLE_KUBE_API),true)
	ENABLE_KUBE_API_CMD = --enable-kube-api true
	STORAGE = filesystem
endif

ifeq ($(ALLOW_CONVERGED_FLOW),true)
	ALLOW_CONVERGED_FLOW_CMD = --allow-converged-flow
	STORAGE = filesystem
endif


# Operator Vars - these must be kept up to date
BUNDLE_CHANNELS ?= alpha,ocm-2.15
BUNDLE_OUTPUT_DIR ?= deploy/olm-catalog
BUNDLE_METADATA_OPTS ?= --channels=$(BUNDLE_CHANNELS) --default-channel=alpha

# We decided to have an option to change replicas count only while running locally
# check if SERVICE_REPLICAS_COUNT was set and if yes change default value to required one
# Default for 1 replica
REPLICAS_COUNT = $(shell if [[ "${TARGET}" != @(minikube|kind|oc) ]]; then echo 3; else echo $(or ${SERVICE_REPLICAS_COUNT},1);fi)

ifdef INSTALLATION_TIMEOUT
	INSTALLATION_TIMEOUT_FLAG = --installation-timeout $(INSTALLATION_TIMEOUT)
endif

CI ?= false
VERBOSE ?= false
REPORTS ?= $(ROOT_DIR)/reports
GO_TEST_FORMAT = pkgname
DEFAULT_RELEASE_IMAGES = $(shell (tr -d '\n\t ' < ${ROOT_DIR}/data/default_release_images.json))
DEFAULT_OS_IMAGES = $(shell (tr -d '\n\t ' < ${ROOT_DIR}/data/default_os_images.json))
DEFAULT_RELEASE_SOURCES = $(shell (tr -d '\n\t ' < ${ROOT_DIR}/data/default_release_sources.json))

# Support all Release/OS images for CI
ifeq ($(CI), true)
	VERBOSE = true
endif

RELEASE_IMAGES := $(or ${RELEASE_IMAGES},${DEFAULT_RELEASE_IMAGES})
OS_IMAGES := $(or ${OS_IMAGES},${DEFAULT_OS_IMAGES})

# Support given Release/OS images.
ifdef OPENSHIFT_VERSION
	ifeq ($(OPENSHIFT_VERSION), all)
		RELEASE_IMAGES := ${DEFAULT_RELEASE_IMAGES}
		OS_IMAGES := ${DEFAULT_OS_IMAGES}
	else
		RELEASE_IMAGES := $(shell (echo '$(RELEASE_IMAGES)' | jq -c --arg v $(OPENSHIFT_VERSION) 'map(select(.openshift_version==$$v))'))
		OS_IMAGES := $(shell (echo '$(OS_IMAGES)' | jq -c --arg v $(OPENSHIFT_VERSION) 'map(select(.openshift_version==$$v))'))
	endif
endif

ifeq ($(VERBOSE), true)
	GO_TEST_FORMAT=standard-verbose
endif

GOTEST_FLAGS = --format=$(GO_TEST_FORMAT) $(GOTEST_PUBLISH_FLAGS) -- -count=1 -cover -coverprofile=$(REPORTS)/$(TEST_SCENARIO)_coverage.out
GINKGO_FLAGS = -ginkgo.focus="$(FOCUS)" -ginkgo.v -ginkgo.skip="$(SKIP)" -ginkgo.v -ginkgo.reportFile=./junit_$(TEST_SCENARIO)_test.xml

COVER_PROFILE := $(or ${COVER_PROFILE},$(REPORTS)/unit_coverage.out)
GINKGO_REPORTFILE := $(or $(GINKGO_REPORTFILE), ./junit_unit_test.xml)
GO_UNITTEST_FLAGS = --format=$(GO_TEST_FORMAT) $(GOTEST_PUBLISH_FLAGS) -- -count=1 -cover -coverprofile=$(COVER_PROFILE)
GINKGO_UNITTEST_FLAGS = -ginkgo.focus="$(FOCUS)" -ginkgo.v -ginkgo.skip="$(SKIP)" -ginkgo.v -ginkgo.reportFile=$(GINKGO_REPORTFILE)

.EXPORT_ALL_VARIABLES:

all: build

init:
	./hack/setup_env.sh assisted_service

ci-lint:
ifeq ($(shell hack/utils.sh running_from_skipper && echo 1 || echo 0),1)
	$(error Running this target using skipper is not supported, try `make ci-lint` instead)
endif

	${ROOT_DIR}/hack/check-commits.sh
	${ROOT_DIR}/tools/handle_ocp_versions.py
	skipper -v $(MAKE) generate
	git diff --exit-code  # this will fail if code generation caused any diff

lint:
	golangci-lint run -v

$(BUILD_FOLDER):
	mkdir -p $(BUILD_FOLDER)

format:
	golangci-lint run --fix -v

##################
# Build & Update #
##################

.PHONY: build

validate: lint unit-test

build: validate build-minimal

build-all: build-in-docker

build-in-docker:
	skipper make build-image operator-bundle-build

build-assisted-service:
	# We need the CGO_ENABLED for the go-sqlite library build.
	cd ./cmd && CGO_ENABLED=1 go build -tags $(GO_BUILD_TAGS) $(DEBUG_ARGS) -o $(BUILD_FOLDER)/assisted-service

build-assisted-service-operator:
	cd ./cmd/operator && CGO_ENABLED=1 go build -tags $(GO_BUILD_TAGS) $(DEBUG_ARGS) -o $(BUILD_FOLDER)/assisted-service-operator

build-minimal: $(BUILD_FOLDER)
	$(MAKE) -j build-assisted-service build-assisted-service-operator

update-minimal:
	$(CONTAINER_COMMAND) build $(CONTAINER_BUILD_PARAMS) -f Dockerfile.assisted-service . -t $(SERVICE)

update-debug-minimal:
	$(CONTAINER_COMMAND) build $(CONTAINER_BUILD_PARAMS) --build-arg DEBUG_SERVICE_PORT=$(DEBUG_SERVICE_PORT) -f Dockerfile.assisted-service-debug -t $(SERVICE)

update-image: $(UPDATE_IMAGE)

load-image:
	rm -f $(LOCAL_IMAGE_ARCHIVE) || true
	mkdir -p build
	if [ -z ${SERVICE_IMAGE} ]; then \
		echo "Building and using local assisted-service image..."; \
		skipper $(MAKE) update-image SERVICE=$(LOCAL_ASSISTED_SERVICE_IMAGE); \
	else \
		echo "Using image from ${SERVICE_IMAGE}..."; \
		$(CONTAINER_COMMAND) pull ${SERVICE_IMAGE}; \
		$(CONTAINER_COMMAND) tag ${SERVICE_IMAGE} $(LOCAL_ASSISTED_SERVICE_IMAGE); \
	fi
	$(CONTAINER_COMMAND) save -o $(LOCAL_IMAGE_ARCHIVE) $(LOCAL_ASSISTED_SERVICE_IMAGE)
	./hack/hub_cluster.sh load_image $(LOCAL_IMAGE_ARCHIVE) $(HUB_CLUSTER_NAME)

build-image: update-minimal

update-service: build-in-docker
	$(CONTAINER_COMMAND) push $(SERVICE)

update: build-all
	$(CONTAINER_COMMAND) push $(SERVICE)

publish-client: generate-python-client
	python3 -m twine upload --skip-existing "$(BUILD_FOLDER)/assisted-service-client/dist/*"

build-openshift-ci-test-bin: init

remove_assisted_service_deployment_liveness_probe:
	$(KUBECTL) patch deployment assisted-service --type json -p='[{"op": "remove", "path": "/spec/template/spec/containers/0/livenessProbe"}]' || true

set_assisted_service_deployment_replicas_to_one:
	$(KUBECTL) patch deployment assisted-service --type='json' -p='[{"op": "replace", "path": "/spec/replicas", "value": 1}]'

restart_service_pods:
	$(KUBECTL) rollout restart deployment assisted-service
	$(KUBECTL) rollout status  deployment assisted-service

prepare_assisted_service_for_debug: remove_assisted_service_deployment_liveness_probe set_assisted_service_deployment_replicas_to_one restart_service_pods

patch-service: load-image
	$(KUBECTL) patch deployment assisted-service \
		--type json \
		-p='[{"op": "replace", "path": "/spec/template/spec/containers/0/image", "value": "$(LOCAL_ASSISTED_SERVICE_IMAGE)"},{"op": "replace", "path": "/spec/template/spec/containers/0/imagePullPolicy", "value": "Never"}]'
	if [ "${DEBUG_SERVICE}" == "true" ]; then \
		$(MAKE) remove_assisted_service_deployment_liveness_probe set_assisted_service_deployment_replicas_to_one; \
	fi
	$(MAKE) restart_service_pods

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

_verify_cluster:
	python3 ./tools/wait_for_cluster_info.py --namespace "$(NAMESPACE)"

deploy-all: $(BUILD_FOLDER) _verify_cluster deploy-namespace deploy-postgres deploy-kafka deploy-s3 deploy-ocm-secret deploy-route53 deploy-image-service deploy-service
	echo "Deployment done"

deploy-ui: deploy-namespace
	python3 ./tools/deploy_ui.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --namespace "$(NAMESPACE)" \
		--apply-manifest $(APPLY_MANIFEST) $(DEPLOY_TAG_OPTION)

deploy-namespace: $(BUILD_FOLDER)
	python3 ./tools/deploy_namespace.py --deploy-namespace $(APPLY_NAMESPACE) --namespace "$(NAMESPACE)" --target "$(TARGET)"

deploy-s3-secret:
	python3 ./tools/deploy_s3_secrets.py --namespace "$(NAMESPACE)" --target "$(TARGET)" \
		--apply-manifest $(APPLY_MANIFEST)

deploy-s3: deploy-namespace
	python3 ./tools/deploy_s3.py --namespace "$(NAMESPACE)" --target "$(TARGET)"
	make deploy-s3-secret

deploy-route53: deploy-namespace
	python3 ./tools/deploy_route53.py --secret "$(ROUTE53_SECRET)" --namespace "$(NAMESPACE)" --target "$(TARGET)"

deploy-ocm-secret: deploy-namespace
	python3 ./tools/deploy_sso_secret.py --secret "$(OCM_CLIENT_SECRET)" --id "$(OCM_CLIENT_ID)" --namespace "$(NAMESPACE)" \
		--target "$(TARGET)" --apply-manifest $(APPLY_MANIFEST)

deploy-image-service: deploy-namespace
	python3 ./tools/deploy_image_service.py --namespace "$(NAMESPACE)" --apply-manifest $(APPLY_MANIFEST) --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)"

deploy-inventory-service-file: deploy-namespace
	python3 ./tools/deploy_inventory_service.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --namespace "$(NAMESPACE)" \
		--apply-manifest $(APPLY_MANIFEST) $(DEBUG_PORT_OPTIONS)

deploy-service-requirements: | deploy-namespace deploy-inventory-service-file
	python3 ./tools/deploy_local_auth_secret.py --namespace "$(NAMESPACE)" --target "$(TARGET)" --apply-manifest $(APPLY_MANIFEST)
	python3 ./tools/deploy_assisted_installer_configmap.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" \
		--base-dns-domains "$(BASE_DNS_DOMAINS)" --namespace "$(NAMESPACE)" \
		$(INSTALLATION_TIMEOUT_FLAG) $(DEPLOY_TAG_OPTION) --auth-type "$(AUTH_TYPE)" $(TEST_FLAGS) \
		--os-images '$(subst ",\",$(OS_IMAGES))' --release-images '$(subst ",\",$(RELEASE_IMAGES))' \
		--must-gather-images '$(subst ",\",$(MUST_GATHER_IMAGES))' \
		--public-registries "$(PUBLIC_CONTAINER_REGISTRIES)" --iso-image-type $(ISO_IMAGE_TYPE) \
		--check-cvo $(CHECK_CLUSTER_VERSION) --apply-manifest $(APPLY_MANIFEST) $(ENABLE_KUBE_API_CMD) $(E2E_TESTS_CONFIG) \
		--storage $(STORAGE) --ipv6-support $(IPV6_SUPPORT) --enable-sno-dnsmasq $(ENABLE_SINGLE_NODE_DNSMASQ) \
		--disk-encryption-support $(DISK_ENCRYPTION_SUPPORT) --hw-requirements '$(subst ",\",$(HW_REQUIREMENTS))' \
		--disabled-host-validations "$(DISABLED_HOST_VALIDATIONS)" --disabled-steps "$(DISABLED_STEPS)" \
		--enable-org-tenancy $(ENABLE_ORG_TENANCY) \
		--enable-org-based-feature-gate $(ENABLE_ORG_BASED_FEATURE_GATES) $(ALLOW_CONVERGED_FLOW_CMD) $(DISABLE_TLS_CMD)
ifeq ($(MIRROR_REGISTRY_SUPPORT), True)
	python3 ./tools/deploy_assisted_installer_configmap_registry_ca.py  --target "$(TARGET)" \
		--namespace "$(NAMESPACE)"  --apply-manifest $(APPLY_MANIFEST) --ca-file-path $(MIRROR_REG_CA_FILE) --registries-file-path $(REGISTRIES_FILE_PATH)
endif
	$(MAKE) deploy-role deploy-resources

deploy-resources: generate-manifests
	python3 ./tools/deploy_crd.py $(ENABLE_KUBE_API_CMD) --apply-manifest $(APPLY_MANIFEST) \
	--target "$(TARGET)" --namespace "$(NAMESPACE)"

deploy-converged-flow-requirements:
	python3 ./tools/deploy_converged_flow_requirements.py $(ENABLE_KUBE_API_CMD) $(ALLOW_CONVERGED_FLOW_CMD) \
	--target "$(TARGET)" --namespace "$(NAMESPACE)"

deploy-service: deploy-service-requirements
	python3 ./tools/deploy_assisted_installer.py $(DEPLOY_TAG_OPTION) --namespace "$(NAMESPACE)" \
		$(TEST_FLAGS) --target "$(TARGET)" --replicas-count $(REPLICAS_COUNT) \
		--apply-manifest $(APPLY_MANIFEST) $(EVENT_STREAMING_OPTIONS) $(DEBUG_PORT_OPTIONS)
	$(MAKE) wait-for-service

wait-for-service:
	python3 ./tools/wait_for_assisted_service.py --target $(TARGET) --namespace "$(NAMESPACE)" \
		--domain "$(INGRESS_DOMAIN)" --apply-manifest $(APPLY_MANIFEST)

deploy-role: deploy-namespace generate-manifests
	python3 ./tools/deploy_role.py --namespace "$(NAMESPACE)" --target "$(TARGET)" \
		--apply-manifest $(APPLY_MANIFEST) $(ENABLE_KUBE_API_CMD)

deploy-postgres: deploy-namespace
	python3 ./tools/deploy_postgres.py --namespace "$(NAMESPACE)" --target "$(TARGET)" \
		--apply-manifest $(APPLY_MANIFEST) --persistent-storage $(PERSISTENT_STORAGE)

deploy-kafka: deploy-namespace
ifdef ENABLE_EVENT_STREAMING
	python3 ./tools/deploy_kafka.py --namespace "$(NAMESPACE)" --target "$(TARGET)" \
		--apply-manifest $(APPLY_MANIFEST)
endif

deploy-service-on-ocp-cluster:
	export TARGET=ocp && export PERSISTENT_STORAGE=False && $(MAKE) deploy-postgres deploy-ocm-secret deploy-s3-secret deploy-service

deploy-ui-on-ocp-cluster:
	export TARGET=ocp && $(MAKE) deploy-ui

create-ocp-manifests:
	export APPLY_MANIFEST=False && export APPLY_NAMESPACE=False && \
	export ENABLE_KUBE_API=true && export TARGET=ocp && \
	export OS_IMAGES="$(subst ",\", $(shell cat $(ROOT_DIR)/data/default_os_images.json | tr -d "\n\t "))" && \
	export RELEASE_IMAGES="$(subst ",\", $(shell cat $(ROOT_DIR)/data/default_release_images.json | tr -d "\n\t "))" && \
	export MUST_GATHER_IMAGES="$(subst ",\", $(shell cat $(ROOT_DIR)/data/default_must_gather_versions.json | tr -d "\n\t "))" && \
	export HW_REQUIREMENTS="$(subst ",\", $(shell cat $(ROOT_DIR)/data/default_hw_requirements.json | tr -d "\n\t "))" && \
	$(MAKE) deploy-postgres deploy-ocm-secret deploy-s3-secret deploy-service deploy-ui

ci-deploy-for-subsystem: deploy-test

deploy-test: _verify_cluster generate-keys
	-$(KUBECTL) delete deployments.apps assisted-service &> /dev/null
	export TEST_FLAGS=--subsystem-test && \
	export AUTH_TYPE="rhsso" && export DUMMY_IGNITION="True" && \
	export IPV6_SUPPORT="True" && ENABLE_ORG_TENANCY="True" && ENABLE_ORG_BASED_FEATURE_GATES="True" && \
	export RELEASE_SOURCES='$(or ${RELEASE_SOURCES},${DEFAULT_RELEASE_SOURCES})' && \
	$(MAKE) deploy-wiremock deploy-all

# execute on the host, without skipper
deploy-service-for-subsystem-test: create-hub-cluster load-image
	skipper $(MAKE) deploy-test SERVICE=$(LOCAL_ASSISTED_SERVICE_IMAGE) IP=$(shell ip route get 1 | sed 's/^.*src \([^ ]*\).*$$/\1/;q')
	if [ "$(ENABLE_KUBE_API)" == "true" ]; then \
		echo "Deploying assisted-service for subsystem tests in kube-api mode..."; \
		skipper make enable-kube-api-for-subsystem; \
	fi
	if [ "${DEBUG_SERVICE}" == "true" ]; then \
		$(MAKE) prepare_assisted_service_for_debug; \
	fi
	echo "assited service deployment for subsystem tests is ready"

# $SERVICE is built with docker. If we want the latest version of $SERVICE
# we need to pull it from the docker daemon before deploy-onprem.
podman-pull-service-from-docker-daemon:
	podman pull "docker-daemon:${SERVICE}"

deploy-onprem:
	podman play kube --configmap ${PODMAN_CONFIGMAP} deploy/podman/pod.yml
	./hack/retry.sh 90 2 "curl -f http://127.0.0.1:8090/ready"
	./hack/retry.sh 60 10 "curl -f http://127.0.0.1:8888/health"

deploy-on-openshift-ci:
	export PERSISTENT_STORAGE="False" && export TARGET='oc' && export GENERATE_CRD='false' && unset GOFLAGS && \
	$(MAKE) deploy-test
	oc get pods

deploy-on-k8s: create-hub-cluster
	skipper $(MAKE) deploy-all IP=$(shell ip route get 1 | sed 's/^.*src \([^ ]*\).*$$/\1/;q')
	if [ "$(USE_LOCAL_SERVICE)" == "true" ] || [ "${DEBUG_SERVICE}" == "true" ]; then \
		$(MAKE) patch-service; \
	fi
	skipper $(MAKE) deploy-ui

deploy-sylva: create-hub-cluster
	./hack/kind/dev-env-sylva.sh

deploy-dev-infra: create-hub-cluster
	./hack/kind/dev-env-infra.sh

########
# Test #
########

test:
	$(MAKE) _run_subsystem_test AUTH_TYPE=rhsso ENABLE_ORG_TENANCY=true ENABLE_ORG_BASED_FEATURE_GATES=true TEST="$(or $(TEST),'github.com/openshift/assisted-service/subsystem')"

test-kube-api:
	$(MAKE) _run_subsystem_test AUTH_TYPE=local TEST="$(or $(TEST),'github.com/openshift/assisted-service/subsystem/kubeapi')"

# Alias for test
subsystem-test: test

subsystem-test-kube-api:
	$(MAKE) test-kube-api ENABLE_KUBE_API=true

_run_subsystem_test:
	@if [ $(TARGET) == "kind" ]; then \
		assisted_service_url="localhost:8090"; \
		db_host="localhost"; \
		db_port="5432"; \
		ocm_host="localhost:8070"; \
	else \
		assisted_service_url="$(shell $(call get_service_host_port,assisted-service) | sed 's/http:\/\///g')"; \
		db_host="$(shell $(call get_service_host_port,postgres) | sed 's/http:\/\///g' | cut -d ":" -f 1)"; \
		db_port="$(shell $(call get_service_host_port,postgres) | sed 's/http:\/\///g' | cut -d ":" -f 2)"; \
		ocm_host="$(shell $(call get_service_host_port,wiremock) | sed 's/http:\/\///g')"; \
	fi; \
	echo "INVENTORY=$$assisted_service_url\n"; \
	echo "DB_HOST=$$db_host\n"; \
	echo "DB_PORT=$$db_port\n"; \
	echo "OCM_HOST=$$ocm_host\n"; \
	echo "TEST_TOKEN=$(shell cat $(BUILD_FOLDER)/auth-tokenString)"; \
	echo "TEST_TOKEN_2=$(shell cat $(BUILD_FOLDER)/auth-tokenString2)"; \
	echo "TEST_TOKEN_ADMIN=$(shell cat $(BUILD_FOLDER)/auth-tokenAdminString)"; \
	echo "TEST_TOKEN_UNALLOWED=$(shell cat $(BUILD_FOLDER)/auth-tokenUnallowedString)"; \
	echo "TEST_TOKEN_EDITOR=$(shell cat $(BUILD_FOLDER)/auth-tokenClusterEditor)"; \
	INVENTORY=$$assisted_service_url \
	DB_HOST=$$db_host \
	DB_PORT=$$db_port \
	OCM_HOST=$$ocm_host \
	TEST_TOKEN="$(shell cat $(BUILD_FOLDER)/auth-tokenString)" \
	TEST_TOKEN_2="$(shell cat $(BUILD_FOLDER)/auth-tokenString2)" \
	TEST_TOKEN_ADMIN="$(shell cat $(BUILD_FOLDER)/auth-tokenAdminString)" \
	TEST_TOKEN_UNALLOWED="$(shell cat $(BUILD_FOLDER)/auth-tokenUnallowedString)" \
	TEST_TOKEN_EDITOR="$(shell cat $(BUILD_FOLDER)/auth-tokenClusterEditor)" \
	RELEASE_SOURCES='$(or ${RELEASE_SOURCES},${DEFAULT_RELEASE_SOURCES})' \
	$(MAKE) _test TEST_SCENARIO=subsystem TIMEOUT=120m

enable-kube-api-for-subsystem: $(BUILD_FOLDER)
	$(MAKE) deploy-service-requirements AUTH_TYPE=local ENABLE_KUBE_API=true ALLOW_CONVERGED_FLOW=true ISO_IMAGE_TYPE=minimal-iso
	$(MAKE) deploy-converged-flow-requirements  ENABLE_KUBE_API=true  ALLOW_CONVERGED_FLOW=true
	$(MAKE) restart_service_pods wait-for-service

deploy-wiremock: deploy-namespace
	python3 ./tools/deploy_wiremock.py --target $(TARGET) --namespace "$(NAMESPACE)"

deploy-olm: deploy-namespace
	python3 ./tools/deploy_olm.py --target $(TARGET)

deploy-prometheus: $(BUILD_FOLDER) deploy-namespace
	python3 ./tools/deploy_prometheus.py --target $(TARGET) --namespace "$(NAMESPACE)"

deploy-grafana: $(BUILD_FOLDER)
	python3 ./tools/deploy_grafana.py --target $(TARGET) --namespace "$(NAMESPACE)"

deploy-monitoring: deploy-olm deploy-prometheus deploy-grafana

_test: $(REPORTS)
	gotestsum $(GOTEST_FLAGS) $(TEST) $(GINKGO_FLAGS) -timeout $(TIMEOUT) || ($(MAKE) _post_test && /bin/false)
	$(MAKE) _post_test

_unit_test: $(REPORTS)
	gotestsum $(GO_UNITTEST_FLAGS) $(TEST) $(GINKGO_UNITTEST_FLAGS) -timeout $(TIMEOUT) || ($(MAKE) _post_unit_test && /bin/false)
	$(MAKE) _post_unit_test

_post_test: $(REPORTS)
	@for name in `find '$(ROOT_DIR)' -name 'junit*.xml' -type f -not -path '$(REPORTS)/*'`; do \
		mv -f $$name $(REPORTS)/junit_$(TEST_SCENARIO)_$$(basename $$(dirname $$name)).xml; \
	done
	$(MAKE) _coverage

_post_unit_test: $(REPORTS)
	@for name in `find '$(ROOT_DIR)' -name 'junit*.xml' -type f -not -path '$(REPORTS)/*'`; do \
		mv -f $$name $(REPORTS)/junit_unit_$$(basename $$(dirname $$name)).xml; \
	done
	$(MAKE) _unit_test_coverage

_coverage: $(REPORTS)
ifeq ($(CI), true)
	gocov convert $(REPORTS)/$(TEST_SCENARIO)_coverage.out | gocov-xml > $(REPORTS)/$(TEST_SCENARIO)_coverage.xml
endif

_unit_test_coverage: $(REPORTS)
ifeq ($(CI), true)
	gocov convert $(REPORTS)/unit_coverage.out | gocov-xml > $(REPORTS)/unit_coverage.xml
	./hack/publish-codecov.sh
endif

display-coverage:
	./hack/display_cover_profile.sh

run-db-container:
	$(CONTAINER_COMMAND) ps -q --filter "name=postgres" | xargs -r $(CONTAINER_COMMAND) kill && sleep 3
	$(CONTAINER_COMMAND) run -d  --rm --tmpfs /var/lib/pgsql/data --name postgres -e POSTGRESQL_ADMIN_PASSWORD=admin -e POSTGRESQL_MAX_CONNECTIONS=10000 -p 127.0.0.1:5433:5432 \
		$(PSQL_IMAGE)
	timeout 5m ./hack/wait_for_postgres.sh

run-unit-test:
	SKIP_UT_DB=1 $(MAKE) _unit_test TIMEOUT=30m TEST="$(or $(TEST),$(shell go list ./... | grep -v subsystem))"

ci-unit-test:
	./hack/start_db.sh
	$(MAKE) run-unit-test

kill-db-container:
	$(CONTAINER_COMMAND) kill postgres

unit-test: run-db-container run-unit-test kill-db-container

$(REPORTS):
	-mkdir -p $(REPORTS)

test-on-openshift-ci:
	export TARGET='oc' && unset GOFLAGS && \
	$(MAKE) test FOCUS="[minimal-set]"

install-kind-if-needed:
	./hack/kind/kind.sh install

create-hub-cluster:
	./hack/hub_cluster.sh create

destroy-hub-cluster:
	./hack/hub_cluster.sh delete

#########
# Clean #
#########

clear-all: clean subsystem-clean clear-deployment clear-images clean-onprem

clean:
	-rm -rf $(BUILD_FOLDER) $(REPORTS)
	-rm -rf bundle*

clear-deployment:
	-python3 ./tools/clear_deployment.py --delete-namespace $(APPLY_NAMESPACE) --delete-pvc $(DELETE_PVC) --namespace "$(NAMESPACE)" --target "$(TARGET)" || true

clear-images:
	-$(CONTAINER_COMMAND) rmi -f $(SERVICE)
	-$(CONTAINER_COMMAND) rmi -f $(ISO_CREATION)

clean-onprem:
	podman pod rm --force --ignore assisted-installer

subsystem-clean:
	-$(KUBECTL) get pod -o name | grep createimage | xargs -r $(KUBECTL) delete --force --grace-period=0 1> /dev/null || true

############
# Operator #
############

.PHONY: operator-bundle-build operator-index-build
operator-bundle-build: generate-bundle
	$(CONTAINER_COMMAND) build $(CONTAINER_BUILD_PARAMS) -f deploy/olm-catalog/bundle.Dockerfile -t $(BUNDLE_IMAGE) .

operator-index-build:
	opm index add --bundles $(BUNDLE_IMAGE) --tag $(INDEX_IMAGE) --container-tool $(CONTAINER_COMMAND)
