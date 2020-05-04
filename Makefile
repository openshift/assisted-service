PWD = $(shell pwd)
UID = $(shell id -u)

SERVICE := $(or ${SERVICE},quay.io/ocpmetal/bm-inventory:stable)

all: build

lint:
	golangci-lint run -v

.PHONY: build
build: lint unit-test
	mkdir -p build
	CGO_ENABLED=0 go build -o build/bm-inventory cmd/main.go

clean:
	rm -rf build

format:
	goimports -w -l cmd/ internal/

generate:
	go generate $(shell go list ./... | grep -v 'restapi')

generate-from-swagger:
	rm -rf client models restapi
	docker run -u $(UID):$(UID) -v $(PWD):$(PWD) -v /etc/passwd:/etc/passwd -w $(PWD) quay.io/goswagger/swagger generate server	--template=stratoscale -f swagger.yaml --template-dir=/templates/contrib
	docker run -u $(UID):$(UID) -v $(PWD):$(PWD) -v /etc/passwd:/etc/passwd -w $(PWD) quay.io/goswagger/swagger generate client	--template=stratoscale -f swagger.yaml --template-dir=/templates/contrib

update: build
	docker build -f Dockerfile.bm-inventory . -t $(SERVICE)
	docker push $(SERVICE)

deploy-for-test: deploy-mariadb deploy-s3-configmap deploy-service-for-test

deploy-all: deploy-mariadb deploy-s3 deploy-service

deploy-s3-configmap:
	./deploy/s3/deploy-configmap.sh

# scalitiy default credentials
define CREDENTIALS =
[default]
aws_access_key_id = accessKey1
aws_secret_access_key = verySecretKey1
endef
export CREDENTIALS
export AWS_DIR = ${PWD}/build/.aws
export AWS_SHARED_CREDENTIALS_FILE = ${AWS_DIR}/credentials

deploy-s3:
	kubectl apply -f deploy/s3/scality-deployment.yaml
	make deploy-s3-configmap
	mkdir -p "${AWS_DIR}" ; echo "$$CREDENTIALS" > ${AWS_SHARED_CREDENTIALS_FILE}
	n=20 ; \
	aws --endpoint-url=`minikube service scality --url` s3api create-bucket --bucket test ; \
	REPLY=$$? ; \
	echo $(REPLY) ; \
	while [ $${n} -gt 0 ] && [ $${REPLY} -ne 0 ] ; do \
		sleep 5 ; \
		echo $$n ; \
		n=`expr $$n - 1`; \
		aws --endpoint-url=`minikube service scality --url` s3api create-bucket --bucket test ; \
		REPLY=$$? ; \
	done; \
	if [ $${n} -eq 0 ]; \
	then \
		echo "bucket creation failed"; \
		false; \
	fi

deploy-service-requirements: deploy-role
	kubectl apply -f deploy/bm-inventory-service.yaml
	./deploy/deploy-configmap.sh

deploy-service-for-test: deploy-service-requirements
	sed '/IMAGE_BUILDER_CMD$$/{$$!{N;s/IMAGE_BUILDER_CMD\n              value: \"\"$$/IMAGE_BUILDER_CMD\n              value: \"echo hello\"/;ty;P;D;:y}}' deploy/bm-inventory.yaml > deploy/bm-inventory-tmp.yaml
	sed -i "s#REPLACE_IMAGE#${SERVICE}#g" deploy/bm-inventory-tmp.yaml
	kubectl apply -f deploy/bm-inventory-tmp.yaml
	rm deploy/bm-inventory-tmp.yaml

deploy-service: deploy-service-requirements
	sed "s#REPLACE_IMAGE#${SERVICE}#g" deploy/bm-inventory.yaml > deploy/bm-inventory-tmp.yaml
	kubectl apply -f deploy/bm-inventory-tmp.yaml
	rm deploy/bm-inventory-tmp.yaml

deploy-role:
	kubectl apply -f deploy/roles/role_binding.yaml

deploy-mariadb:
	kubectl apply -f deploy/mariadb/mariadb-configmap.yaml
	kubectl apply -f deploy/mariadb/mariadb-deployment.yaml

subsystem-run: test subsystem-clean

ifndef SYSTEM
SYSTEM_TEST=-ginkgo.skip="system-test"
endif
test:
	INVENTORY=$(shell minikube service bm-inventory --url| sed 's/http:\/\///g') \
		DB_HOST=$(shell minikube service mariadb --url| sed 's/http:\/\///g' | cut -d ":" -f 1) \
		DB_PORT=$(shell minikube service mariadb --url| sed 's/http:\/\///g' | cut -d ":" -f 2) \
		go test -v ./subsystem/... -count=1 -ginkgo.focus=${FOCUS} -ginkgo.v $(SYSTEM_TEST)

unit-test:
	go test -v $(shell go list ./... | grep -v subsystem) -cover

.PHONY: subsystem
subsystem: deploy-all subsystem-run

subsystem-clean:
	kubectl get pod -o name | grep create-image | xargs kubectl delete 1> /dev/null ; true
	kubectl get pod -o name | grep generate-kubeconfig | xargs kubectl delete 1> /dev/null ; true

clear-deployment:
	kubectl delete deployments.apps bm-inventory 1> /dev/null ; true
	kubectl delete deployments.apps mariadb 1> /dev/null ; true
	kubectl delete deployments.apps scality 1> /dev/null ; true
	kubectl get job -o name | grep create-image | xargs kubectl delete 1> /dev/null ; true
	kubectl get pod -o name | grep create-image | xargs kubectl delete 1> /dev/null ; true
	kubectl get job -o name | grep generate-kubeconfig | xargs kubectl delete 1> /dev/null ; true
	kubectl get pod -o name | grep generate-kubeconfig | xargs kubectl delete 1> /dev/null ; true
	kubectl delete service bm-inventory 1> /dev/null ; true
	kubectl delete service mariadb 1> /dev/null ; true
	kubectl delete service scality 1> /dev/null ; true
	kubectl delete configmap bm-inventory-config 1> /dev/null ; true
	kubectl delete configmap mariadb-config 1> /dev/null ; true
	kubectl delete configmap s3-config 1> /dev/null ; true
	kubectl delete configmap scality-config 1> /dev/null ; true
