PWD = $(shell pwd)
UID = $(shell id -u)

TARGET := $(or ${TARGET},minikube)

ifeq ($(TARGET), minikube)
define get_service
minikube service --url $(1) | sed 's/http:\/\///g'
endef
else
define get_service
kubectl get service $(1) | grep $(1) | awk '{print $$4 ":" $$5}' | \
	awk '{split($$0,a,":"); print a[1] ":" a[2]}'
endef
endif

SERVICE := $(or ${SERVICE},quay.io/ocpmetal/bm-inventory:stable)

all: build

lint:
	golangci-lint run -v

.PHONY: build
build: create-build-dir lint unit-test
	CGO_ENABLED=0 go build -o build/bm-inventory cmd/main.go

create-build-dir:
	mkdir -p build

clean:
	rm -rf build

format:
	goimports -w -l cmd/ internal/

generate:
	go generate $(shell go list ./...)

generate-from-swagger:
	rm -rf client models restapi
	docker run -u $(UID):$(UID) -v $(PWD):$(PWD) -v /etc/passwd:/etc/passwd -w $(PWD) quay.io/goswagger/swagger generate server	--template=stratoscale -f swagger.yaml --template-dir=/templates/contrib
	docker run -u $(UID):$(UID) -v $(PWD):$(PWD) -v /etc/passwd:/etc/passwd -w $(PWD) quay.io/goswagger/swagger generate client	--template=stratoscale -f swagger.yaml --template-dir=/templates/contrib
	go generate $(shell go list ./client/... ./models/... ./restapi/...)

update: build
	docker build -f Dockerfile.bm-inventory . -t $(SERVICE)
	docker push $(SERVICE)

deploy-all: create-build-dir deploy-mariadb deploy-s3 deploy-service

deploy-s3-configmap:
	$(eval CONFIGMAP=./build/scality-configmap.yaml)
	cp ./deploy/s3/scality-configmap.yaml $(CONFIGMAP)
	$(eval URL=$(shell $(call get_service,scality)))
	sed -i "s#REPLACE_URL#http://$(URL)#" $(CONFIGMAP)
	$(eval HOST=$(shell $(call get_service,scality) | sed 's/http:\/\///g' | cut -d ":" -f 1))
	sed -i "s#REPLACE_HOST_NAME#$(HOST)#" $(CONFIGMAP)
	echo "deploying s3 configmap"
	cat $(CONFIGMAP)
	kubectl apply -f $(CONFIGMAP)

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
	sleep 5;  # wait for service to get an address
	make deploy-s3-configmap
	mkdir -p "${AWS_DIR}" ; echo "$$CREDENTIALS" > ${AWS_SHARED_CREDENTIALS_FILE}
	n=20 ; \
	aws --endpoint-url=http://`$(call get_service,scality)` s3api create-bucket --bucket test ; \
	REPLY=$$? ; \
	echo $(REPLY) ; \
	while [ $${n} -gt 0 ] && [ $${REPLY} -ne 0 ] ; do \
		sleep 5 ; \
		echo $$n ; \
		n=`expr $$n - 1`; \
		aws --endpoint-url=http://`$(call get_service,scality)` s3api create-bucket --bucket test ; \
		REPLY=$$? ; \
	done; \
	if [ $${n} -eq 0 ]; \
	then \
		echo "bucket creation failed"; \
		false; \
	fi

deploy-inventory-service-file:
	kubectl apply -f deploy/bm-inventory-service.yaml
	sleep 5;  # wait for service to get an address

deploy-service-requirements: deploy-inventory-service-file
	$(eval CONFIGMAP=./deploy/tmp-bm-inventory-configmap.yaml)
	$(eval URL=$(shell $(call get_service,bm-inventory) | sed 's/http:\/\///g' | cut -d ":" -f 1))
	$(eval PORT=$(shell $(call get_service,bm-inventory) | sed 's/http:\/\///g' | cut -d ":" -f 2))
	sed "s#REPLACE_URL#\"$(URL)\"#;s#REPLACE_PORT#\"$(PORT)\"#" ./deploy/bm-inventory-configmap.yaml > $(CONFIGMAP)
	echo "Apply bm-inventory-config configmap"
	cat $(CONFIGMAP)
	kubectl apply -f $(CONFIGMAP)
	rm $(CONFIGMAP)

deploy-service: deploy-service-requirements deploy-role
	sed "s#REPLACE_IMAGE#${SERVICE}#g" deploy/bm-inventory.yaml > deploy/bm-inventory-tmp.yaml
	kubectl apply -f deploy/bm-inventory-tmp.yaml
	rm deploy/bm-inventory-tmp.yaml

deploy-role:
	kubectl apply -f deploy/roles/role_binding.yaml

deploy-mariadb:
	kubectl apply -f deploy/mariadb/mariadb-configmap.yaml
	kubectl apply -f deploy/mariadb/mariadb-deployment.yaml

subsystem-run: test subsystem-clean

test:
	INVENTORY=$(shell $(call get_service,bm-inventory) | sed 's/http:\/\///g') \
		DB_HOST=$(shell $(call get_service,mariadb) | sed 's/http:\/\///g' | cut -d ":" -f 1) \
		DB_PORT=$(shell $(call get_service,mariadb) | sed 's/http:\/\///g' | cut -d ":" -f 2) \
		go test -v ./subsystem/... -count=1 -ginkgo.focus=${FOCUS} -ginkgo.v

unit-test:
	go test -v $(shell go list ./... | grep -v subsystem) -cover

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
