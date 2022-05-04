#
# Copyright (c) 2018 Red Hat, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

# Enable Go modules:
export GO111MODULE=on
export GOPROXY=https://proxy.golang.org

# Disable CGO so that we always generate static binaries:
export CGO_ENABLED=0

# Details of the model to use:
model_version:=v0.0.142
model_url:=https://github.com/openshift-online/ocm-api-model.git

# Details of the metamodel to use:
metamodel_version:=v0.0.39
metamodel_url:=https://github.com/openshift-online/ocm-api-metamodel.git

.PHONY: examples
examples:
	cd examples && \
	for i in *.go; do \
		go build $${i} || exit 1; \
	done

.PHONY: test tests
test tests:
	ginkgo -r

.PHONY: fmt
fmt:
	gofmt -s -l -w .

.PHONY: lint
lint:
	golangci-lint --version
	golangci-lint run

.PHONY: generate
generate: model metamodel
	rm -rf \
		accountsmgmt \
		authorizations \
		clustersmgmt \
		servicelogs \
		errors \
		job_queue \
		helpers \
		openapi
	metamodel/metamodel generate go \
		--model=model/model \
		--base=github.com/openshift-online/ocm-sdk-go \
		--output=.
	metamodel/metamodel generate openapi \
		--model=model/model \
		--output=openapi

.PHONY: model
model:
	rm -rf "$@"
	if [ -d "$(model_url)" ]; then \
		cp -r "$(model_url)" "$@"; \
	else \
		git clone "$(model_url)" "$@"; \
		cd "$@"; \
		git fetch --tags origin; \
		git checkout -B build "$(model_version)"; \
	fi

.PHONY: metamodel
metamodel:
	rm -rf "$@"
	if [ -d "$(metamodel_url)" ]; then \
		cp -r "$(metamodel_url)" "$@"; \
	else \
		git clone "$(metamodel_url)" "$@"; \
		cd "$@"; \
		git fetch --tags origin; \
		git checkout -B build "$(metamodel_version)"; \
	fi
	make -C "$@"

.PHONY: clean
clean:
	rm -rf \
		.gobin \
		metamodel \
		model \
		$(NULL)
