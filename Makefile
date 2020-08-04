PWD = $(shell pwd)
UID = $(shell id -u)
BUILD_FOLDER = $(PWD)/build

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
GIT_REVISION := $(shell git rev-parse HEAD)
APPLY_NAMESPACE := $(or ${APPLY_NAMESPACE},True)
ROUTE53_SECRET := ${ROUTE53_SECRET}

all: build

lint:
	golangci-lint run -v

.PHONY: build
build: lint unit-test build-minimal

build-minimal: create-build-dir
	CGO_ENABLED=0 go build -o $(BUILD_FOLDER)/assisted-service cmd/main.go

create-build-dir:
	mkdir -p $(BUILD_FOLDER)

format:
	goimports -w -l cmd/ internal/ subsystem/

generate:
	go generate $(shell go list ./... | grep -v 'assisted-service/models\|assisted-service/client\|assisted-service/restapi')

generate-from-swagger:
	rm -rf client models restapi
	docker run -u $(UID):$(UID) -v $(PWD):$(PWD):rw,Z -v /etc/passwd:/etc/passwd -w $(PWD) \
		quay.io/goswagger/swagger:v0.24.0 generate server	--template=stratoscale -f swagger.yaml \
		--template-dir=/templates/contrib
	docker run -u $(UID):$(UID) -v $(PWD):$(PWD):rw,Z -v /etc/passwd:/etc/passwd -w $(PWD) \
		quay.io/goswagger/swagger:v0.24.0 generate client	--template=stratoscale -f swagger.yaml \
		--template-dir=/templates/contrib

##########
# Update #
##########

update: build create-python-client
	GIT_REVISION=${GIT_REVISION} docker build --build-arg GIT_REVISION -f Dockerfile.assisted-service . -t $(SERVICE)
	docker push $(SERVICE)

update-minimal: build-minimal create-python-client
	GIT_REVISION=${GIT_REVISION} docker build --build-arg GIT_REVISION -f Dockerfile.assisted-service . -t $(SERVICE)

update-minikube: build create-python-client
	eval $$(SHELL=$${SHELL:-/bin/sh} minikube docker-env) && \
	GIT_REVISION=${GIT_REVISION} docker build --build-arg GIT_REVISION -f Dockerfile.assisted-service . -t $(SERVICE)

create-python-client: build/assisted-service-client-${GIT_REVISION}.tar.gz

build/assisted-service-client/setup.py: swagger.yaml
	cp swagger.yaml $(BUILD_FOLDER)
	echo '{"packageName" : "assisted_service_client", "packageVersion": "1.0.0"}' > $(BUILD_FOLDER)/code-gen-config.json
	sed -i '/pattern:/d' $(BUILD_FOLDER)/swagger.yaml
	docker run --rm -u $(shell id -u $(USER)) -v $(BUILD_FOLDER):/swagger-api/out:Z \
		-v $(BUILD_FOLDER)/swagger.yaml:/swagger.yaml:ro,Z -v $(BUILD_FOLDER)/code-gen-config.json:/config.json:ro,Z \
		jimschubert/swagger-codegen-cli:2.3.1 generate --lang python --config /config.json --output ./assisted-service-client/ --input-spec /swagger.yaml
	rm -f $(BUILD_FOLDER)/swagger.yaml

build/assisted-service-client-%.tar.gz: build/assisted-service-client/setup.py
	rm -rf $@
	cd $(BUILD_FOLDER)/assisted-service-client/ && python3 setup.py sdist --dist-dir $(BUILD_FOLDER)
	rm -rf assisted-service-client/assisted-service-client.egg-info

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

deploy-all: create-build-dir deploy-namespace deploy-postgres deploy-s3 deploy-route53 deploy-service
	echo "Deployment done"

deploy-ui: deploy-namespace
	python3 ./tools/deploy_ui.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --namespace "$(NAMESPACE)" $(DEPLOY_TAG_OPTION)

deploy-namespace: create-build-dir
	python3 ./tools/deploy_namespace.py --deploy-namespace $(APPLY_NAMESPACE) --namespace "$(NAMESPACE)"

deploy-s3-secret:
	python3 ./tools/deploy_scality_configmap.py --namespace "$(NAMESPACE)"

deploy-s3: deploy-namespace
	python3 ./tools/deploy_s3.py --namespace "$(NAMESPACE)"
	sleep 5;  # wait for service to get an address
	make deploy-s3-secret

deploy-route53: deploy-namespace
	python3 ./tools/deploy_route53.py --secret "$(ROUTE53_SECRET)" --namespace "$(NAMESPACE)"

deploy-inventory-service-file: deploy-namespace
	python3 ./tools/deploy_inventory_service.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --namespace "$(NAMESPACE)"
	sleep 5;  # wait for service to get an address

deploy-service-requirements: deploy-namespace deploy-inventory-service-file
	python3 ./tools/deploy_assisted_installer_configmap.py --target "$(TARGET)" --domain "$(INGRESS_DOMAIN)" --base-dns-domains "$(BASE_DNS_DOMAINS)" --namespace "$(NAMESPACE)" $(DEPLOY_TAG_OPTION)

deploy-service: deploy-namespace deploy-service-requirements deploy-role
	python3 ./tools/deploy_assisted_installer.py $(DEPLOY_TAG_OPTION) --namespace "$(NAMESPACE)" $(TEST_FLAGS)
	python3 ./tools/wait_for_pod.py --app=assisted-service --state=running --namespace "$(NAMESPACE)"

deploy-role: deploy-namespace
	python3 ./tools/deploy_role.py --namespace "$(NAMESPACE)"

deploy-postgres: deploy-namespace
	python3 ./tools/deploy_postgres.py --namespace "$(NAMESPACE)"

deploy-test:
	export SERVICE=minikube-local-registry/assisted-service:minikube-test && export TEST_FLAGS=--subsystem-test && \
	$(MAKE) update-minikube deploy-all

########
# Test #
########

subsystem-run: test subsystem-clean

test:
	INVENTORY=$(shell $(call get_service,assisted-service) | sed 's/http:\/\///g') \
		DB_HOST=$(shell $(call get_service,postgres) | sed 's/http:\/\///g' | cut -d ":" -f 1) \
		DB_PORT=$(shell $(call get_service,postgres) | sed 's/http:\/\///g' | cut -d ":" -f 2) \
		go test -v ./subsystem/... -count=1 -ginkgo.focus=${FOCUS} -ginkgo.v -timeout 20m

deploy-olm: deploy-namespace
	python3 ./tools/deploy_olm.py --target $(TARGET)

deploy-prometheus: create-build-dir deploy-namespace 
	python3 ./tools/deploy_prometheus.py --target $(TARGET) --namespace "$(NAMESPACE)"

deploy-grafana: create-build-dir
	python3 ./tools/deploy_grafana.py --target $(TARGET) --namespace "$(NAMESPACE)"

deploy-monitoring: deploy-olm deploy-prometheus deploy-grafana

unit-test:
	docker stop postgres || true
	docker run -d  --rm --name postgres -e POSTGRES_PASSWORD=admin -e POSTGRES_USER=admin -p 127.0.0.1:5432:5432 postgres:12.3-alpine -c 'max_connections=10000'
	until PGPASSWORD=admin pg_isready -U admin --dbname postgres --host 127.0.0.1 --port 5432; do sleep 1; done
	SKIP_UT_DB=1 go test -v $(or ${TEST}, ${TEST}, $(shell go list ./... | grep -v subsystem)) -cover || (docker stop postgres && /bin/false)
	docker stop postgres

#########
# Clean #
#########

clear-all: clean subsystem-clean clear-deployment

clean:
	-rm -rf $(BUILD_FOLDER)

subsystem-clean:
	-$(KUBECTL) get pod -o name | grep create-image | xargs $(KUBECTL) delete 1> /dev/null || true
	-$(KUBECTL) get pod -o name | grep generate-kubeconfig | xargs $(KUBECTL) delete 1> /dev/null || true

clear-deployment:
	-python3 ./tools/clear_deployment.py --delete-namespace $(APPLY_NAMESPACE) --namespace "$(NAMESPACE)" || true
