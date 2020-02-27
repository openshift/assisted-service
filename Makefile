PWD = $(shell pwd)
UID = $(shell id -u)

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
