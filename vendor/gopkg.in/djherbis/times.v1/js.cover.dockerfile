FROM golang:1.23rc1

RUN curl -sL https://deb.nodesource.com/setup_8.x | bash
RUN apt-get install --yes nodejs

WORKDIR /go/src/github.com/djherbis/times
COPY . .

RUN GO111MODULE=auto GOOS=js GOARCH=wasm go test -covermode=count -coverprofile=profile.cov -exec="$(go env GOROOT)/misc/wasm/go_js_wasm_exec"
