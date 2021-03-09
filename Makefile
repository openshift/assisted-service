NAMESPACE := $(or ${NAMESPACE},assisted-installer)

PWD = $(shell pwd)
BUILD_FOLDER = $(PWD)/build/$(NAMESPACE)
ROOT_DIR = $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

BUILD_TYPE := $(or ${BUILD_TYPE},standalone)
TARGET := $(or ${TARGET},minikube)
PROFILE := $(or $(PROFILE),minikube)
KUBECTL=kubectl -n $(NAMESPACE)

ifeq ($(BUILD_TYPE), standalone)
    UNIT_TEST_TARGET = unit-test
else
    UNIT_TEST_TARGET = convert-coverage
endif

ifeq ($(TARGET), minikube)
ifdef E2E_TESTS_MODE
E2E_TESTS_CONFIG = --img-expr-time=5m --img-expr-interval=5m
endif
define get_service
minikube -p $(PROFILE) service --url $(1) -n $(NAMESPACE) | sed 's/http:\/\///g'
endef # get_service
VERIFY_CLUSTER = _verify_minikube
else
define get_service
kubectl get service $(1) -n $(NAMESPACE) | grep $(1) | awk '{print $$4 ":" $$5}' | \
	awk '{split($$0,a,":"); print a[1] ":" a[2]}'
endef # get_service
VERIFY_CLUSTER = _verify_cluster
endif # TARGET

ASSISTED_ORG := $(or ${ASSISTED_ORG},quay.io/ocpmetal)
ASSISTED_TAG := $(or ${ASSISTED_TAG},latest)

SERVICE := $(or ${SERVICE},${ASSISTED_ORG}/assisted-service:${ASSISTED_TAG})
CONTAINER_BUILD_PARAMS = --network=host --label git_revision=${GIT_REVISION} ${CONTAINER_BUILD_EXTRA_PARAMS}

# RHCOS_VERSION should be consistent with BaseObjectName in pkg/s3wrapper/client.go
RHCOS_BASE_ISO := $(or ${RHCOS_BASE_ISO},https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.6/latest/rhcos-live.x86_64.iso)
OPENSHIFT_VERSIONS := $(or ${OPENSHIFT_VERSIONS}, $(shell hack/get_ocp_versions_for_testing.sh))
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
DELETE_PVC := $(or ${DELETE_PVC},False)
TESTING_PUBLIC_CONTAINER_REGISTRIES := quay.io,registry.svc.ci.openshift.org
PUBLIC_CONTAINER_REGISTRIES := $(or ${PUBLIC_CONTAINER_REGISTRIES},$(TESTING_PUBLIC_CONTAINER_REGISTRIES))
PODMAN_PULL_FLAG := $(or ${PODMAN_PULL_FLAG},--pull always)
ENABLE_KUBE_API := $(or ${ENABLE_KUBE_API},false)
PERSISTENT_STORAGE := $(or ${PERSISTENT_STORAGE},True)

ifeq ($(ENABLE_KUBE_API),true)
	ENABLE_KUBE_API_CMD = --enable-kube-api true
endif

# We decided to have an option to change replicas count only while running in minikube
# That line is checking if we run on minikube
# check if SERVICE_REPLICAS_COUNT was set and if yes change default value to required one
# Default for 1 replica
REPLICAS_COUNT = $(shell if ! [ "${TARGET}" = "minikube" ] && ! [ "${TARGET}" = "oc" ];then echo 3; else echo $(or ${SERVICE_REPLICAS_COUNT},1);fi)

ifdef INSTALLATION_TIMEOUT
        INSTALLATION_TIMEOUT_FLAG = --installation-timeout $(INSTALLATION_TIMEOUT)
endif

# define focus flag for test so users can run individual tests or suites
ifdef FOCUS
		GINKGO_FOCUS_FLAG = -ginkgo.focus="$(FOCUS)"
endif
REPORTS = $(ROOT_DIR)/reports
TEST_PUBLISH_FLAGS = --junitfile-testsuite-name=relative --junitfile-testcase-classname=relative --junitfile $(REPORTS)/unittest.xml

.EXPORT_ALL_VARIABLES:

all: build

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
build: lint $(UNIT_TEST_TARGET) build-minimal

build-all: build-in-docker

build-in-docker:
	skipper make build-image

build-minimal: $(BUILD_FOLDER)
	CGO_ENABLED=0 go build -o $(BUILD_FOLDER)/assisted-service cmd/main.go

build-image: build
	docker build $(CONTAINER_BUILD_PARAMS) -f Dockerfile.assisted-service . -t $(SERVICE)

update-service: build-in-docker
	docker push $(SERVICE)

update: build-all
	docker push $(SERVICE)

update-minimal: build-minimal
	docker build $(CONTAINER_BUILD_PARAMS) -f Dockerfile.assisted-service . -t $(SERVICE)

_update-minikube: build-minimal
	eval $$(SHELL=$${SHELL:-/bin/sh} minikube -p $(PROFILE) docker-env) && \
		docker build $(CONTAINER_BUILD_PARAMS) -f Dockerfile.assisted-service . -t $(SERVICE)

define publish_image
	${1} tag ${2} ${3}
	${1} push ${3}
endef # publish_image

publish:
	$(call publish_image,docker,${SERVICE},quay.io/ocpmetal/assisted-service:${PUBLISH_TAG})
	skipper make publish-client

publish-client: generate-python-client
	python3 -m twine upload --skip-existing "$(BUILD_FOLDER)/assisted-service-client/dist/*"

build-openshift-ci-test-bin:
	pip3 install pyyaml waiting

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
	$(KUBECTL) cluster-info

_verify_minikube:
	minikube -p $(PROFILE) update-context
	minikube -p $(PROFILE) status

deploy-all: $(BUILD_FOLDER) $(VERIFY_CLUSTER) deploy-namespace deploy-postgres deploy-s3 deploy-ocm-secret deploy-route53 deploy-service
	echo "Deployment done"

deploy-ui: deploy-namespace
	python3 ./tools/deploy_ui.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --namespace "$(NAMESPACE)" \
		--profile "$(PROFILE)" --apply-manifest $(APPLY_MANIFEST) $(DEPLOY_TAG_OPTION)

deploy-namespace: $(BUILD_FOLDER)
	python3 ./tools/deploy_namespace.py --deploy-namespace $(APPLY_NAMESPACE) --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)"

deploy-s3-secret:
	python3 ./tools/deploy_scality_configmap.py --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)" \
		--apply-manifest $(APPLY_MANIFEST)

deploy-s3: deploy-namespace
	python3 ./tools/deploy_s3.py --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)"
	sleep 5;  # wait for service to get an address
	make deploy-s3-secret

deploy-route53: deploy-namespace
	python3 ./tools/deploy_route53.py --secret "$(ROUTE53_SECRET)" --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)"

deploy-ocm-secret: deploy-namespace
	python3 ./tools/deploy_sso_secret.py --secret "$(OCM_CLIENT_SECRET)" --id "$(OCM_CLIENT_ID)" --namespace "$(NAMESPACE)" \
		--profile "$(PROFILE)" --target "$(TARGET)" --apply-manifest $(APPLY_MANIFEST)

deploy-inventory-service-file: deploy-namespace
	python3 ./tools/deploy_inventory_service.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --namespace "$(NAMESPACE)" \
		--profile "$(PROFILE)" --apply-manifest $(APPLY_MANIFEST)
	sleep 5;  # wait for service to get an address

deploy-service-requirements: deploy-namespace deploy-inventory-service-file
	python3 ./tools/deploy_assisted_installer_configmap.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" \
		--base-dns-domains "$(BASE_DNS_DOMAINS)" --namespace "$(NAMESPACE)" --profile "$(PROFILE)" \
		$(INSTALLATION_TIMEOUT_FLAG) $(DEPLOY_TAG_OPTION) --auth-type "$(AUTH_TYPE)" --with-ams-subscriptions "$(WITH_AMS_SUBSCRIPTIONS)" $(TEST_FLAGS) \
		--ocp-versions '$(subst ",\",$(OPENSHIFT_VERSIONS))' --public-registries "$(PUBLIC_CONTAINER_REGISTRIES)" \
		--check-cvo $(CHECK_CLUSTER_VERSION) --apply-manifest $(APPLY_MANIFEST) $(ENABLE_KUBE_API_CMD) $(E2E_TESTS_CONFIG)

deploy-resources: generate-manifests
	python3 ./tools/deploy_crd.py $(ENABLE_KUBE_API_CMD) --apply-manifest $(APPLY_MANIFEST) --profile "$(PROFILE)" --target "$(TARGET)"

deploy-service: deploy-namespace deploy-service-requirements deploy-role deploy-resources
	python3 ./tools/deploy_assisted_installer.py $(DEPLOY_TAG_OPTION) --namespace "$(NAMESPACE)" \
		--profile "$(PROFILE)" $(TEST_FLAGS) --target "$(TARGET)" --replicas-count $(REPLICAS_COUNT) \
		--apply-manifest $(APPLY_MANIFEST) \
		$(ENABLE_KUBE_API_CMD)
	python3 ./tools/wait_for_assisted_service.py --target $(TARGET) --namespace "$(NAMESPACE)" \
		--profile "$(PROFILE)" --domain "$(INGRESS_DOMAIN)" --apply-manifest $(APPLY_MANIFEST)

deploy-role: deploy-namespace generate-manifests
	python3 ./tools/deploy_role.py --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)" \
		--apply-manifest $(APPLY_MANIFEST) $(ENABLE_KUBE_API_CMD)

deploy-postgres: deploy-namespace
	python3 ./tools/deploy_postgres.py --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)" \
		--apply-manifest $(APPLY_MANIFEST) --persistent-storage $(PERSISTENT_STORAGE)

deploy-service-on-ocp-cluster:
	export TARGET=ocp && export PERSISTENT_STORAGE=False && $(MAKE) deploy-postgres deploy-ocm-secret deploy-s3-secret deploy-service

deploy-ui-on-ocp-cluster:
	export TARGET=ocp && $(MAKE) deploy-ui

create-ocp-manifests:
	export APPLY_MANIFEST=False && export APPLY_NAMESPACE=False && \
	export ENABLE_KUBE_API=true && export TARGET=ocp && \
	$(MAKE) deploy-postgres deploy-ocm-secret deploy-s3-secret deploy-service deploy-ui

jenkins-deploy-for-subsystem: ci-deploy-for-subsystem

ci-deploy-for-subsystem: $(VERIFY_CLUSTER) generate-keys
	export TEST_FLAGS=--subsystem-test && export AUTH_TYPE="rhsso" && export DUMMY_IGNITION=${DUMMY_IGNITION} && WITH_AMS_SUBSCRIPTIONS="True" && \
	$(MAKE) deploy-wiremock deploy-all

deploy-test: $(VERIFY_CLUSTER) generate-keys
	-$(KUBECTL) delete deployments.apps assisted-service &> /dev/null
	export ASSISTED_ORG=minikube-local-registry && export ASSISTED_TAG=minikube-test && export TEST_FLAGS=--subsystem-test && \
	export AUTH_TYPE="rhsso" && export DUMMY_IGNITION="True" && export WITH_AMS_SUBSCRIPTIONS="True" && \
	$(MAKE) _update-minikube deploy-wiremock deploy-all

# $SERVICE is built with docker. If we want the latest version of $SERVICE
# we need to pull it from the docker daemon before deploy-onprem.
podman-pull-service-from-docker-daemon:
	podman pull "docker-daemon:${SERVICE}"

deploy-onprem:
	# Format: ip:hostPort:containerPort | ip::containerPort | hostPort:containerPort | containerPort
	podman pod create --name assisted-installer -p 5432:5432,8000:8000,8090:8090,8080:8080
	# These are required because when running on RHCOS livecd, the coreos-installer binary and
	# livecd are bind-mounted from the host into the assisted-service container at runtime.
	[ -f livecd.iso ] || ./hack/retry.sh 5 1 "curl $(RHCOS_BASE_ISO) -o livecd.iso"
	[ -f coreos-installer ] || podman run --privileged --pull=always -it --rm \
		-v .:/data -w /data --entrypoint /bin/bash \
		quay.io/coreos/coreos-installer:v0.7.0 -c 'cp /usr/sbin/coreos-installer /data/coreos-installer'
	podman run -dt --pod assisted-installer --env-file onprem-environment --pull always --name db quay.io/ocpmetal/postgresql-12-centos7
	podman run -dt --pod assisted-installer --env-file onprem-environment --pull always -v $(PWD)/deploy/ui/nginx.conf:/opt/bitnami/nginx/conf/server_blocks/nginx.conf:z --name ui quay.io/ocpmetal/ocp-metal-ui:latest
	podman run -dt --pod assisted-installer --env-file onprem-environment ${PODMAN_PULL_FLAG} --env DUMMY_IGNITION=$(DUMMY_IGNITION) \
		-v ./livecd.iso:/data/livecd.iso:z \
		-v ./coreos-installer:/data/coreos-installer:z \
		--restart always --name installer $(SERVICE)
	./hack/retry.sh 90 2 "curl http://127.0.0.1:8090/ready"

deploy-onprem-for-subsystem:
	export DUMMY_IGNITION="true" && $(MAKE) deploy-onprem

deploy-on-openshift-ci:
	ln -s $(shell which oc) $(shell dirname $(shell which oc))/kubectl
	export TARGET='oc' && export PROFILE='openshift-ci' && unset GOFLAGS && \
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

test:
	INVENTORY=$(shell $(call get_service,assisted-service) | sed 's/http:\/\///g') \
		DB_HOST=$(shell $(call get_service,postgres) | sed 's/http:\/\///g' | cut -d ":" -f 1) \
		DB_PORT=$(shell $(call get_service,postgres) | sed 's/http:\/\///g' | cut -d ":" -f 2) \
		OCM_HOST=$(shell $(call get_service,wiremock) | sed 's/http:\/\///g') \
		TEST_TOKEN="$(shell cat $(BUILD_FOLDER)/auth-tokenString)" \
		TEST_TOKEN_ADMIN="$(shell cat $(BUILD_FOLDER)/auth-tokenAdminString)" \
		TEST_TOKEN_UNALLOWED="$(shell cat $(BUILD_FOLDER)/auth-tokenUnallowedString)" \
		AUTH_TYPE="rhsso" \
		WITH_AMS_SUBSCRIPTIONS="true" \
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
	timeout 5m ./hack/wait_for_postgres.sh
	SKIP_UT_DB=1 gotestsum --format=pkgname $(TEST_PUBLISH_FLAGS) -- -cover -coverprofile=$(REPORTS)/coverage.out $(or ${TEST},${TEST},$(shell go list ./... | grep -v subsystem)) $(GINKGO_FOCUS_FLAG) \
		-ginkgo.v -timeout 30m -count=1 || (docker kill postgres && /bin/false)
	docker kill postgres

convert-coverage: unit-test
	gocov convert $(REPORTS)/coverage.out | gocov-xml > $(REPORTS)/coverage.xml

$(REPORTS):
	-mkdir -p $(REPORTS)

test-onprem:
	INVENTORY=127.0.0.1:8090 \
	DB_HOST=127.0.0.1 \
	DB_PORT=5432 \
	DEPLOY_TARGET=onprem \
	STORAGE=filesystem \
	go test -v ./subsystem/... -count=1 $(GINKGO_FOCUS_FLAG) -ginkgo.v -timeout 30m

test-on-openshift-ci:
	export TARGET='oc' && export PROFILE='openshift-ci' && unset GOFLAGS && \
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
	-rm -rf bundle*

subsystem-clean:
	-$(KUBECTL) get pod -o name | grep createimage | xargs -r $(KUBECTL) delete --force --grace-period=0 1> /dev/null || true

clear-deployment:
	-python3 ./tools/clear_deployment.py --delete-namespace $(APPLY_NAMESPACE) --delete-pvc $(DELETE_PVC) --namespace "$(NAMESPACE)" --profile "$(PROFILE)" --target "$(TARGET)" || true

clear-images:
	-docker rmi -f $(SERVICE)
	-docker rmi -f $(ISO_CREATION)

clean-onprem:
	podman pod rm -f assisted-installer || true
	rm livecd.iso || true
	rm coreos-installer || true

delete-minikube-profile:
	minikube delete -p $(PROFILE)

delete-all-minikube-profiles:
	minikube delete --all

############
# Operator #
############

# Current Operator version
OPERATOR_VERSION ?= 0.0.1

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
	#operator-sdk generate kustomize manifests -q
	#cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	kustomize build config/manifests | operator-sdk generate bundle -q --overwrite --version $(OPERATOR_VERSION) $(BUNDLE_METADATA_OPTS)
	operator-sdk bundle validate ./bundle

# Build the bundle image.
.PHONY: operator-bundle-build
operator-bundle-build:
	podman build -f bundle.Dockerfile -t $(BUNDLE_IMG) .
