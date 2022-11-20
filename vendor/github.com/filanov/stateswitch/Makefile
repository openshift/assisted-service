PWD = $(shell pwd)

all: lint test

lint:
	docker run --rm -v $(PWD):/app -w /app golangci/golangci-lint:v1.50.1 golangci-lint run -v

test:
	go test -v ./... -cover -ginkgo.v
