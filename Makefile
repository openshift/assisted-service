NAMESPACE := $(or ${NAMESPACE},assisted-installer)

PWD = $(shell pwd)
BUILD_FOLDER = $(PWD)/build/$(NAMESPACE)
ROOT_DIR = $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

BUILD_TYPE := $(or ${BUILD_TYPE},standalone)
TARGET := $(or ${TARGET},local)
KUBECTL=kubectl -n $(NAMESPACE)

define get_service
kubectl get service $(1) -n $(NAMESPACE) | grep $(1) | awk '{print $$4 ":" $$5}' | \
	awk '{split($$0,a,":"); print a[1] ":" a[2]}'
endef # get_service

ifdef E2E_TESTS_MODE
E2E_TESTS_CONFIG = --img-expr-time=5m --img-expr-interval=5m
endif

ASSISTED_ORG := $(or ${ASSISTED_ORG},quay.io/ocpmetal)
ASSISTED_TAG := $(or ${ASSISTED_TAG},latest)

SERVICE := $(or ${SERVICE},${ASSISTED_ORG}/assisted-service:${ASSISTED_TAG})
BUNDLE_IMAGE := $(or ${BUNDLE_IMAGE},${ASSISTED_ORG}/assisted-service-operator-bundle:${ASSISTED_TAG})
INDEX_IMAGE := $(or ${INDEX_IMAGE},${ASSISTED_ORG}/assisted-service-index:${ASSISTED_TAG})

ifdef DEBUG
    DEBUG_ARGS=-gcflags "all=-N -l"
endif

CONTAINER_BUILD_PARAMS = --network=host --label git_revision=${GIT_REVISION} ${CONTAINER_BUILD_EXTRA_PARAMS}

# RHCOS_VERSION should be consistent with BaseObjectName in pkg/s3wrapper/client.go
OPENSHIFT_VERSIONS := $(or ${OPENSHIFT_VERSIONS}, $(shell hack/get_ocp_versions_for_testing.sh))
RHCOS_BASE_ISO := $(shell (jq -n '$(OPENSHIFT_VERSIONS)' | jq '[.[].rhcos_image]|max'))
DUMMY_IGNITION := $(or ${DUMMY_IGNITION},False)
GIT_REVISION := $(shell git rev-parse HEAD)
PUBLISH_TAG := $(or ${GIT_REVISION})
APPLY_MANIFEST := $(or ${APPLY_MANIFEST},True)
APPLY_NAMESPACE := $(or ${APPLY_NAMESPACE},True)
ROUTE53_SECRET := ${ROUTE53_SECRET}
OCM_CLIENT_ID := ${OCM_CLIENT_ID}
OCM_CLIENT_SECRET := ${OCM_CLIENT_SECRET}
AUTH_TYPE := $(or ${AUTH_TYPE},none)
WITH_AMS_SUBSCRIPTIONS := $(or ${WITH_AMS_SUBSCRIPTIONS},False)
CHECK_CLUSTER_VERSION := $(or ${CHECK_CLUSTER_VERSION},False)
ENABLE_SINGLE_NODE_DNSMASQ := $(or ${ENABLE_SINGLE_NODE_DNSMASQ},True)
DELETE_PVC := $(or ${DELETE_PVC},False)
TESTING_PUBLIC_CONTAINER_REGISTRIES := quay.io
PUBLIC_CONTAINER_REGISTRIES := $(or ${PUBLIC_CONTAINER_REGISTRIES},$(TESTING_PUBLIC_CONTAINER_REGISTRIES))
PODMAN_PULL_FLAG := $(or ${PODMAN_PULL_FLAG},--pull always)
ENABLE_KUBE_API := $(or ${ENABLE_KUBE_API},false)
STORAGE := $(or ${STORAGE},s3)
GENERATE_CRD := $(or ${GENERATE_CRD},true)
PERSISTENT_STORAGE := $(or ${PERSISTENT_STORAGE},True)
IPV6_SUPPORT := $(or ${IPV6_SUPPORT}, True)
ifeq ($(ENABLE_KUBE_API),true)
	ENABLE_KUBE_API_CMD = --enable-kube-api true
	STORAGE = filesystem
endif

# We decided to have an option to change replicas count only while running locally
# check if SERVICE_REPLICAS_COUNT was set and if yes change default value to required one
# Default for 1 replica
REPLICAS_COUNT = $(shell if ! [ "${TARGET}" = "local" ] && ! [ "${TARGET}" = "oc" ];then echo 3; else echo $(or ${SERVICE_REPLICAS_COUNT},1);fi)

ifdef INSTALLATION_TIMEOUT
	INSTALLATION_TIMEOUT_FLAG = --installation-timeout $(INSTALLATION_TIMEOUT)
endif

REPORTS ?= $(ROOT_DIR)/reports

ifneq ($(BUILD_TYPE), standalone)
	TEST_FORMAT = standard-verbose
else
	TEST_FORMAT = pkgname
endif

GOTEST_FLAGS = --format=$(TEST_FORMAT) $(GOTEST_PUBLISH_FLAGS) -- -count=1 -cover -coverprofile=$(REPORTS)/$(TEST_SCENARIO)_coverage.out
GINKGO_FLAGS = -ginkgo.focus="$(FOCUS)" -ginkgo.v -ginkgo.skip="$(SKIP)" -ginkgo.reportFile=./junit_$(TEST_SCENARIO)_test.xml

.EXPORT_ALL_VARIABLES:

all: build

init:
	./hack/setup_env.sh

ci-lint:
ifdef SKIPPER_USERNAME
	$(error Running this target using skipper is not supported, try `make ci-lint` instead)
endif

	${ROOT_DIR}/tools/check-commits.sh
	${ROOT_DIR}/tools/handle_ocp_versions.py
	skipper $(MAKE) generate-all
	git diff --exit-code  # this will fail if generate-all caused any diff

lint:
	golangci-lint run -v

$(BUILD_FOLDER):
	mkdir -p $(BUILD_FOLDER)

format:
	golangci-lint run --fix -v

generate:
	./hack/generate.sh print_help

generate-%: ${BUILD_FOLDER}
	./hack/generate.sh generate_$(subst -,_,$*)

##################
# Build & Update #
##################

.PHONY: build docs

validate: lint unit-test

build: validate build-minimal

build-all: build-in-docker

build-in-docker:
	skipper make build-image operator-bundle-build

build-assisted-service:
	CGO_ENABLED=0 go build $(DEBUG_ARGS) -o $(BUILD_FOLDER)/assisted-service cmd/main.go

build-assisted-service-operator:
	CGO_ENABLED=0 go build $(DEBUG_ARGS) -o $(BUILD_FOLDER)/assisted-service-operator cmd/operator/main.go

build-minimal: $(BUILD_FOLDER)
	$(MAKE) -j build-assisted-service build-assisted-service-operator

update-minimal:
	docker build $(CONTAINER_BUILD_PARAMS) -f Dockerfile.assisted-service . -t $(SERVICE)

update-debug-minimal:
	export DEBUG=True && $(MAKE) build-minimal
	mkdir -p build/debug-image && cp Dockerfile.assisted-service-debug $(BUILD_FOLDER)/assisted-service $(BUILD_FOLDER)/assisted-service-operator build/debug-image
	docker build $(CONTAINER_BUILD_PARAMS) --build-arg SERVICE=$(SERVICE) -f build/debug-image/Dockerfile.assisted-service-debug build/debug-image -t $(SERVICE)
	rm -r build/debug-image

build-image: validate update-minimal

update-service: build-in-docker
	docker push $(SERVICE)

update: build-all
	docker push $(SERVICE)

_update-local-image:
	# Temporary hack that updates the local k8s(e.g minikube) with the latest image.
	# Should be replaced after installing a local registry
	./hack/update_local_image.sh

define publish_image
	${1} tag ${2} ${3}
	${1} push ${3}
endef # publish_image

publish:
	$(call publish_image,docker,${SERVICE},quay.io/ocpmetal/assisted-service:${PUBLISH_TAG})
	$(call publish_image,docker,${BUNDLE_IMAGE},quay.io/ocpmetal/assisted-service-operator-bundle:${PUBLISH_TAG})
	skipper make publish-client

build-publish-index:
	skipper make operator-index-build BUNDLE_IMAGE=quay.io/ocpmetal/assisted-service-operator-bundle:${PUBLISH_TAG}
	$(call publish_image,docker,${INDEX_IMAGE},quay.io/ocpmetal/assisted-service-index:${PUBLISH_TAG})

publish-client: generate-python-client
	python3 -m twine upload --skip-existing "$(BUILD_FOLDER)/assisted-service-client/dist/*"

build-openshift-ci-test-bin: init

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

define restart_service_pods
$(KUBECTL) rollout restart deployment assisted-service
$(KUBECTL) rollout status  deployment assisted-service
endef

_verify_cluster:
	$(KUBECTL) cluster-info

deploy-all: $(BUILD_FOLDER) _verify_cluster deploy-namespace deploy-postgres deploy-s3 deploy-ocm-secret deploy-route53 deploy-service
	echo "Deployment done"

deploy-ui: deploy-namespace
	python3 ./tools/deploy_ui.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --namespace "$(NAMESPACE)" \
		--apply-manifest $(APPLY_MANIFEST) $(DEPLOY_TAG_OPTION)

deploy-namespace: $(BUILD_FOLDER)
	python3 ./tools/deploy_namespace.py --deploy-namespace $(APPLY_NAMESPACE) --namespace "$(NAMESPACE)" --target "$(TARGET)"

deploy-s3-secret:
	python3 ./tools/deploy_scality_configmap.py --namespace "$(NAMESPACE)" --target "$(TARGET)" \
		--apply-manifest $(APPLY_MANIFEST)

deploy-s3: deploy-namespace
	python3 ./tools/deploy_s3.py --namespace "$(NAMESPACE)" --target "$(TARGET)"
	sleep 5;  # wait for service to get an address
	make deploy-s3-secret

deploy-route53: deploy-namespace
	python3 ./tools/deploy_route53.py --secret "$(ROUTE53_SECRET)" --namespace "$(NAMESPACE)" --target "$(TARGET)"

deploy-ocm-secret: deploy-namespace
	python3 ./tools/deploy_sso_secret.py --secret "$(OCM_CLIENT_SECRET)" --id "$(OCM_CLIENT_ID)" --namespace "$(NAMESPACE)" \
		--target "$(TARGET)" --apply-manifest $(APPLY_MANIFEST)

deploy-inventory-service-file: deploy-namespace
	python3 ./tools/deploy_inventory_service.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --namespace "$(NAMESPACE)" \
		--apply-manifest $(APPLY_MANIFEST)
	sleep 5;  # wait for service to get an address

deploy-service-requirements: | deploy-namespace deploy-inventory-service-file
	python3 ./tools/deploy_local_auth_secret.py --namespace "$(NAMESPACE)" --target "$(TARGET)" --apply-manifest $(APPLY_MANIFEST)
	python3 ./tools/deploy_assisted_installer_configmap.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" \
		--base-dns-domains "$(BASE_DNS_DOMAINS)" --namespace "$(NAMESPACE)" \
		$(INSTALLATION_TIMEOUT_FLAG) $(DEPLOY_TAG_OPTION) --auth-type "$(AUTH_TYPE)" --with-ams-subscriptions "$(WITH_AMS_SUBSCRIPTIONS)" $(TEST_FLAGS) \
		--ocp-versions '$(subst ",\",$(OPENSHIFT_VERSIONS))' --public-registries "$(PUBLIC_CONTAINER_REGISTRIES)" \
		--check-cvo $(CHECK_CLUSTER_VERSION) --apply-manifest $(APPLY_MANIFEST) $(ENABLE_KUBE_API_CMD) $(E2E_TESTS_CONFIG) \
		--storage $(STORAGE) --ipv6-support $(IPV6_SUPPORT) --enable-sno-dnsmasq $(ENABLE_SINGLE_NODE_DNSMASQ)
	$(MAKE) deploy-role deploy-resources

deploy-resources: generate-manifests
	python3 ./tools/deploy_crd.py $(ENABLE_KUBE_API_CMD) --apply-manifest $(APPLY_MANIFEST) \
 	--target "$(TARGET)" --namespace "$(NAMESPACE)"

deploy-service: deploy-service-requirements
	python3 ./tools/deploy_assisted_installer.py $(DEPLOY_TAG_OPTION) --namespace "$(NAMESPACE)" \
		$(TEST_FLAGS) --target "$(TARGET)" --replicas-count $(REPLICAS_COUNT) \
		--apply-manifest $(APPLY_MANIFEST)
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

deploy-service-on-ocp-cluster:
	export TARGET=ocp && export PERSISTENT_STORAGE=False && $(MAKE) deploy-postgres deploy-ocm-secret deploy-s3-secret deploy-service

deploy-ui-on-ocp-cluster:
	export TARGET=ocp && $(MAKE) deploy-ui

create-ocp-manifests:
	export APPLY_MANIFEST=False && export APPLY_NAMESPACE=False && \
	export ENABLE_KUBE_API=true && export TARGET=ocp && \
	export OPENSHIFT_VERSIONS="$(subst ",\", $(shell cat default_ocp_versions.json | tr -d "\n\t "))" && \
	$(MAKE) deploy-postgres deploy-ocm-secret deploy-s3-secret deploy-service deploy-ui

ci-deploy-for-subsystem: _verify_cluster generate-keys
	export TEST_FLAGS=--subsystem-test && export AUTH_TYPE="rhsso" && export DUMMY_IGNITION=${DUMMY_IGNITION} && export WITH_AMS_SUBSCRIPTIONS="True" && \
	export IPV6_SUPPORT="True" && \
	$(MAKE) deploy-wiremock deploy-all

deploy-test: _verify_cluster generate-keys
	-$(KUBECTL) delete deployments.apps assisted-service &> /dev/null
	export ASSISTED_ORG=assisted-local-registry && export ASSISTED_TAG=assisted-test && export TEST_FLAGS=--subsystem-test && \
	export AUTH_TYPE="rhsso" && export DUMMY_IGNITION="True" && export WITH_AMS_SUBSCRIPTIONS="True" && \
	export IPV6_SUPPORT="True" && \
	$(MAKE) _update-local-image deploy-wiremock deploy-all

# $SERVICE is built with docker. If we want the latest version of $SERVICE
# we need to pull it from the docker daemon before deploy-onprem.
podman-pull-service-from-docker-daemon:
	podman pull "docker-daemon:${SERVICE}"

deploy-onprem:
	# Format: ip:hostPort:containerPort | ip::containerPort | hostPort:containerPort | containerPort
	podman pod create --name assisted-installer -p 5432:5432,8000:8000,8090:8090,8080:8080
	podman run -dt --pod assisted-installer --env-file onprem-environment --pull always --name db quay.io/ocpmetal/postgresql-12-centos7
	podman run -dt --pod assisted-installer --env-file onprem-environment --pull always -v $(PWD)/deploy/ui/nginx.conf:/opt/bitnami/nginx/conf/server_blocks/nginx.conf:z --name ui quay.io/ocpmetal/ocp-metal-ui:latest
	podman run -dt --pod assisted-installer --env-file onprem-environment ${PODMAN_PULL_FLAG} --env DUMMY_IGNITION=$(DUMMY_IGNITION) \
		--restart always --name installer $(SERVICE)
	./hack/retry.sh 90 2 "curl http://127.0.0.1:8090/ready"

deploy-onprem-for-subsystem:
	export DUMMY_IGNITION="true" && $(MAKE) deploy-onprem

deploy-on-openshift-ci:
	ln -s $(shell which oc) $(shell dirname $(shell which oc))/kubectl
	export TARGET='oc' && export GENERATE_CRD='false' && unset GOFLAGS && \
	$(MAKE) ci-deploy-for-subsystem
	oc get pods

docs:
	mkdocs build

docs_serve:
	mkdocs serve

########
# Test #
########

subsystem-run: test subsystem-clean

subsystem-run-kube-api: enable-kube-api-for-subsystem test-kube-api subsystem-clean

test:
	$(MAKE) _run_subsystem_test AUTH_TYPE=rhsso WITH_AMS_SUBSCRIPTIONS=true

test-kube-api:
	$(MAKE) _run_subsystem_test AUTH_TYPE=local ENABLE_KUBE_API=true FOCUS="$(or ${FOCUS},kube-api)"

_run_subsystem_test:
	INVENTORY=$(shell $(call get_service,assisted-service) | sed 's/http:\/\///g') \
	DB_HOST=$(shell $(call get_service,postgres) | sed 's/http:\/\///g' | cut -d ":" -f 1) \
	DB_PORT=$(shell $(call get_service,postgres) | sed 's/http:\/\///g' | cut -d ":" -f 2) \
	OCM_HOST=$(shell $(call get_service,wiremock) | sed 's/http:\/\///g') \
	TEST_TOKEN="$(shell cat $(BUILD_FOLDER)/auth-tokenString)" \
	TEST_TOKEN_ADMIN="$(shell cat $(BUILD_FOLDER)/auth-tokenAdminString)" \
	TEST_TOKEN_UNALLOWED="$(shell cat $(BUILD_FOLDER)/auth-tokenUnallowedString)" \
	$(MAKE) _test TEST_SCENARIO=subsystem TIMEOUT=120m TEST="$(or $(TEST),./subsystem/...)"

enable-kube-api-for-subsystem: $(BUILD_FOLDER)
	$(MAKE) deploy-service-requirements AUTH_TYPE=local ENABLE_KUBE_API=true
	$(call restart_service_pods)
	$(MAKE) wait-for-service

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

_post_test: $(REPORTS)
	@for name in `find '$(ROOT_DIR)' -name 'junit*.xml' -type f -not -path '$(REPORTS)/*'`; do \
		mv -f $$name $(REPORTS)/junit_$(TEST_SCENARIO)_$$(basename $$(dirname $$name)).xml; \
	done
	$(MAKE) _coverage

_coverage: $(REPORTS)
ifneq ($(BUILD_TYPE), standalone)
	gocov convert $(REPORTS)/$(TEST_SCENARIO)_coverage.out | gocov-xml > $(REPORTS)/$(TEST_SCENARIO)_coverage.xml
endif

unit-test:
	docker ps -q --filter "name=postgres" | xargs -r docker kill && sleep 3
	docker run -d  --rm --tmpfs /var/lib/postgresql/data --name postgres -e POSTGRES_PASSWORD=admin -e POSTGRES_USER=admin -p 127.0.0.1:5432:5432 \
		quay.io/ocpmetal/postgres:12.3-alpine -c 'max_connections=10000'
	timeout 5m ./hack/wait_for_postgres.sh
	SKIP_UT_DB=1 $(MAKE) _test TEST_SCENARIO=unit TIMEOUT=30m TEST="$(or $(TEST),$(shell go list ./... | grep -v subsystem))" || (docker kill postgres && /bin/false)
	docker kill postgres

$(REPORTS):
	-mkdir -p $(REPORTS)

test-onprem:
	INVENTORY=127.0.0.1:8090 \
	DB_HOST=127.0.0.1 \
	DB_PORT=5432 \
	DEPLOY_TARGET=onprem \
	STORAGE=filesystem \
	$(MAKE) _test TEST_SCENARIO=onprem TIMEOUT=30m TEST="$(or $(TEST),./subsystem/...)"

test-on-openshift-ci:
	export TARGET='oc' && unset GOFLAGS && \
	$(MAKE) test FOCUS="[minimal-set]"

#########
# Clean #
#########

clear-all: clean subsystem-clean clear-deployment clear-images clean-onprem

clean:
	-rm -rf $(BUILD_FOLDER) $(REPORTS)
	-rm config/rbac/ocp_role.yaml
	-rm config/rbac/kube_api_roles.yaml
	-rm config/rbac/controller_roles.yaml
	-rm config/assisted-service/scality-secret.yaml
	-rm config/assisted-service/scality-public-secret.yaml
	-rm config/assisted-service/postgres-deployment.yaml
	-rm config/assisted-service/assisted-installer-sso.yaml
	-rm config/assisted-service/assisted-service-configmap.yaml
	-rm config/assisted-service/assisted-service-service.yaml
	-rm config/assisted-service/assisted-service.yaml
	-rm config/assisted-service/deploy_ui.yaml
	-rm config/assisted-service/assisted-installer-local-auth.yaml
	-rm -rf bundle*

subsystem-clean:
	-$(KUBECTL) get pod -o name | grep createimage | xargs -r $(KUBECTL) delete --force --grace-period=0 1> /dev/null || true

clear-deployment:
	-python3 ./tools/clear_deployment.py --delete-namespace $(APPLY_NAMESPACE) --delete-pvc $(DELETE_PVC) --namespace "$(NAMESPACE)" --target "$(TARGET)" || true

clear-images:
	-docker rmi -f $(SERVICE)
	-docker rmi -f $(ISO_CREATION)

clean-onprem:
	podman pod rm -f assisted-installer || true

############
# Operator #
############

# Current Operator version
OPERATOR_VERSION ?= 0.0.1
BUNDLE_OUTPUT_DIR := $(or ${BUNDLE_OUTPUT_DIR},$(BUILD_FOLDER)/bundle)

# Generate bundle manifests and metadata, then validate generated files.
.PHONY: operator-bundle
operator-bundle: create-ocp-manifests
	set -eux
	cp ./build/assisted-installer/ocp_role.yaml config/rbac
	cp ./build/assisted-installer/kube_api_roles.yaml config/rbac
	cp ./build/assisted-installer/controller_roles.yaml config/rbac
	cp ./build/assisted-installer/scality-secret.yaml config/assisted-service
	cp ./build/assisted-installer/scality-public-secret.yaml config/assisted-service
	cp ./build/assisted-installer/postgres-deployment.yaml config/assisted-service
	cp ./build/assisted-installer/assisted-installer-sso.yaml config/assisted-service
	cp ./build/assisted-installer/assisted-service-configmap.yaml config/assisted-service
	cp ./build/assisted-installer/assisted-service-service.yaml config/assisted-service
	cp ./build/assisted-installer/assisted-service.yaml config/assisted-service
	cp ./build/assisted-installer/deploy_ui.yaml config/assisted-service
	cp ./build/assisted-installer/assisted-installer-local-auth.yaml config/assisted-service
	# To use --output-dir, needed to break manifests and metadata generation into two steps
	mkdir -p $(BUNDLE_OUTPUT_DIR)/temp1
	mkdir -p $(BUNDLE_OUTPUT_DIR)/temp2
	kustomize build config/manifests | operator-sdk generate bundle --version $(OPERATOR_VERSION) --manifests --output-dir $(BUNDLE_OUTPUT_DIR)/temp1
	operator-sdk generate bundle --version $(OPERATOR_VERSION) --metadata --input-dir $(BUNDLE_OUTPUT_DIR)/temp1 --output-dir $(BUNDLE_OUTPUT_DIR)/temp2
	mv $(BUNDLE_OUTPUT_DIR)/temp1/* $(BUNDLE_OUTPUT_DIR)
	mv $(BUNDLE_OUTPUT_DIR)/temp2/metadata $(BUNDLE_OUTPUT_DIR)
	rm -rf $(BUNDLE_OUTPUT_DIR)/temp1
	rm -rf $(BUNDLE_OUTPUT_DIR)/temp2
	operator-sdk bundle validate $(BUNDLE_OUTPUT_DIR)

# Build the bundle and index images.
.PHONY: operator-bundle-build operator-bundle-update
operator-bundle-build:
	docker build $(CONTAINER_BUILD_PARAMS) -f Dockerfile.bundle -t $(BUNDLE_IMAGE) .

operator-bundle-update:
	docker push $(BUNDLE_IMAGE)

operator-index-build:
	opm index add --bundles $(BUNDLE_IMAGE) --tag $(INDEX_IMAGE) --container-tool docker
