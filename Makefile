PWD = $(shell pwd)
UID = $(shell id -u)

SERVICE = quay.io/mfilanov/bm-inventory:latest

all: build

.PHONY: build
build:
	mkdir -p build
	CGO_ENABLED=0 go build -o build/bm-inventory cmd/main.go

clean:
	rm -rf build

generate-from-swagger:
	rm -rf client models restapi
	docker run -u $(UID):$(UID) -v $(PWD):$(PWD) -v /etc/passwd:/etc/passwd -w $(PWD) quay.io/goswagger/swagger generate server	--template=stratoscale -f swagger.yaml --template-dir=/templates/contrib
	docker run -u $(UID):$(UID) -v $(PWD):$(PWD) -v /etc/passwd:/etc/passwd -w $(PWD) quay.io/goswagger/swagger generate client	--template=stratoscale -f swagger.yaml --template-dir=/templates/contrib

update: build
	docker build -f Dockerfile.bm-inventory . -t $(SERVICE)
	docker push $(SERVICE)

deploy-for-test: deploy-postgres deploy-s3-configmap deploy-service-for-test

deploy-all: deploy-postgres deploy-s3 deploy-service

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
	n=3 ; \
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
		echo "mycommand failed"; \
		false; \
	fi

deploy-service-requirements: deploy-role
	kubectl apply -f deploy/bm-inventory-service.yaml
	./deploy/deploy-configmap.sh

deploy-service-for-test: deploy-service-requirements
	sed '/IMAGE_BUILDER_CMD$$/{$$!{N;s/IMAGE_BUILDER_CMD\n              value: \"\"$$/IMAGE_BUILDER_CMD\n              value: \"echo hello\"/;ty;P;D;:y}}' deploy/bm-inventory.yaml > deploy/bm-inventory-tmp.yaml
	kubectl apply -f deploy/bm-inventory-tmp.yaml
	rm deploy/bm-inventory-tmp.yaml

deploy-service: deploy-service-requirements
	kubectl apply -f deploy/bm-inventory.yaml

deploy-role:
	kubectl apply -f deploy/roles/role_binding.yaml

deploy-postgres:
	kubectl apply -f deploy/postgres/postgres-configmap.yaml
	kubectl apply -f deploy/postgres/postgres-storage.yaml
	kubectl apply -f deploy/postgres/postgres-deployment.yaml

subsystem-run: test subsystem-clean

test:
	INVENTORY=$(shell minikube service bm-inventory --url| sed 's/http:\/\///g') \
		DB_HOST=$(shell minikube service postgres --url| sed 's/http:\/\///g' | cut -d ":" -f 1) \
		DB_PORT=$(shell minikube service postgres --url| sed 's/http:\/\///g' | cut -d ":" -f 2) \
		go test ./subsystem/... -count=1

.PHONY: subsystem
subsystem: deploy-all subsystem-run

subsystem-clean:
	kubectl get pod -o name | grep create-image | xargs kubectl delete

clear-deployment:
	kubectl delete deployments.apps bm-inventory 1> /dev/null ; true
	kubectl delete deployments.apps postgres 1> /dev/null ; true
	kubectl delete deployments.apps scality 1> /dev/null ; true
	kubectl get job -o name | grep create-image | xargs kubectl delete 1> /dev/null ; true
	kubectl delete service bm-inventory 1> /dev/null ; true
	kubectl delete service postgres 1> /dev/null ; true
	kubectl delete service scality 1> /dev/null ; true
	kubectl delete configmap bm-inventory-config 1> /dev/null ; true
	kubectl delete configmap postgres-config 1> /dev/null ; true
	kubectl delete configmap s3-config 1> /dev/null ; true
	kubectl delete configmap scality-config 1> /dev/null ; true
