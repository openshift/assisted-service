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

generate-swagger:
	rm -rf client models restapi
	docker run -u $(UID):$(UID) -v $(PWD):$(PWD) -v /etc/passwd:/etc/passwd -w $(PWD) quay.io/goswagger/swagger generate server	--template=stratoscale -f swagger.yaml --template-dir=/templates/contrib
	docker run -u $(UID):$(UID) -v $(PWD):$(PWD) -v /etc/passwd:/etc/passwd -w $(PWD) quay.io/goswagger/swagger generate client	--template=stratoscale -f swagger.yaml --template-dir=/templates/contrib

update: build
	docker build -f Dockerfile.bm-inventory . -t $(SERVICE)
	docker push $(SERVICE)

deploy-all: deploy-postgres deploy-service

deploy-service: deploy-role
	kubectl apply -f deploy/bm-inventory.yaml

deploy-role:
	kubectl apply -f deploy/roles/role_binding.yaml

deploy-postgres:
	kubectl apply -f deploy/postgres/postgres-configmap.yaml
	kubectl apply -f deploy/postgres/postgres-storage.yaml
	kubectl apply -f deploy/postgres/postgres-deployment.yaml

subsystem-run:
	INVENTORY=$(shell minikube service bm-inventory --url| sed 's/http:\/\///g') \
		DB_HOST=$(shell minikube service postgres --url| sed 's/http:\/\///g' | cut -d ":" -f 1) \
		DB_PORT=$(shell minikube service postgres --url| sed 's/http:\/\///g' | cut -d ":" -f 2) \
		go test ./subsystem/... -count=1

.PHONY: subsystem
subsystem: deploy-all subsystem-run subsystem-clean

subsystem-clean:
	kubectl get pod -o name | grep create-image | xargs kubectl delete

agent:
	export DBHOST=$(shell minikube service postgres --url| sed 's/http:\/\///g')
	echo $(DBHOST)
