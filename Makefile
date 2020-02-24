all: build

.PHONY: build
build:
	mkdir -p build
	go build -o build/bm-inventory cmd/main.go

clean:
	rm -rf build
