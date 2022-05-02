# Copyright 2022 The KCP Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Ensure Make is run with bash shell as some syntax below is bash-specific
SHELL:=/usr/bin/env bash

.DEFAULT_GOAL := help

#
# Go.
#
GO_VERSION ?= 1.18-alpine
GO_CONTAINER_IMAGE ?= golang:$(GO_VERSION)

#
# Directories.
#
# Full directory of where the Makefile resides
ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
BIN_DIR := bin
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(TOOLS_DIR)/$(BIN_DIR)

#
# Binaries.
#
# Note: Need to use abspath so we can invoke these from subdirectories
GOLANGCI_LINT := $(abspath $(TOOLS_BIN_DIR)/golangci-lint)
# code gen
CONTROLLER_GEN := $(abspath $(TOOLS_BIN_DIR)/controller-gen)
CONVERSION_GEN := $(abspath $(TOOLS_BIN_DIR)/conversion-gen)
BOILERPLATE_FILE := hack/boilerplate/boilerplate.generatego.txt

# Define Docker related variables. Releases should modify and double check these vars.
REGISTRY ?= k8scloudplatform

#
# Images.
#
# vkubelet
IMAGE_NAME_VKUBELET ?= vkubelet
CONTROLLER_IMG_VKUBELET ?= $(REGISTRY)/$(IMAGE_NAME_VKUBELET)

# release
RELEASE_TAG ?= $(shell git describe --tags --abbrev=0)

help:  # Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[0-9A-Za-z_-]+:.*?##/ { printf "  \033[36m%-45s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

## --------------------------------------
## Lint / Verify
## --------------------------------------

##@ lint and verify:

.PHONY: modules
modules: ## Run go mod tidy to ensure modules are up to date.
	go mod tidy
	cd $(TOOLS_DIR); go mod tidy

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Lint the codebase.
	GO111MODULE=off $(GOLANGCI_LINT) run -v

.PHONY: verify-boilerplate
verify-boilerplate: ## Verify boilerplate text exists in each file.
	hack/verify-boilerplate.sh

## --------------------------------------
## Testing
## --------------------------------------

##@ test:

.PHONY: test
test: $(SETUP_ENVTEST) ## Run unit and integration tests.
	go test ./...

## --------------------------------------
## Docker
## --------------------------------------

##@ docker:

.PHONY: docker-build
docker-build: ## Build image.
	docker build --build-arg builder_image=$(GO_CONTAINER_IMAGE) --build-arg package=cmd/virtual-kubelet/main.go . -t $(CONTROLLER_IMG_VKUBELET):$(RELEASE_TAG)

.PHONY: docker-push
docker-push: ## Push image.
	docker push $(CONTROLLER_IMG_VKUBELET):$(RELEASE_TAG)

.PHONY: set-manifest
set-manifest: ## Update manifest image and pull policy.
	$(MAKE) set-manifest-image MANIFEST_IMG=$(CONTROLLER_IMG_VKUBELET) MANIFEST_TAG=$(RELEASE_TAG) TARGET_RESOURCE="./deploy/vkubelet.yaml"
	$(MAKE) set-manifest-pull-policy PULL_POLICY=IfNotPresent TARGET_RESOURCE="./deploy/vkubelet.yaml"

.PHONY: set-manifest-pull-policy
set-manifest-pull-policy: ## Update manifest pull policy.
	sed -i'' -e 's@imagePullPolicy: .*@imagePullPolicy: '"$(PULL_POLICY)"'@' $(TARGET_RESOURCE)

.PHONY: set-manifest-image
set-manifest-image: ## Update manifest image.
	sed -i'' -e 's@image: .*@image: '"${MANIFEST_IMG}:$(MANIFEST_TAG)"'@' $(TARGET_RESOURCE)

## --------------------------------------
## Hack / Tools
## --------------------------------------

##@ hack/tools:

golangci-lint: $(GOLANGCI_LINT) ## Build a local copy of golangci-lint.
controller-gen: $(CONTROLLER_GEN) ## Build a local copy of controller-gen.
conversion-gen: $(CONVERSION_GEN) ## Build a local copy of conversion-gen.

$(GOLANGCI_LINT): $(TOOLS_DIR)/go.mod # Build golangci-lint from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/golangci-lint github.com/golangci/golangci-lint/cmd/golangci-lint

$(CONTROLLER_GEN): $(TOOLS_DIR)/go.mod # Build controller-gen from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/controller-gen sigs.k8s.io/controller-tools/cmd/controller-gen

$(CONVERSION_GEN): $(TOOLS_DIR)/go.mod # Build conversion-gen from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/conversion-gen k8s.io/code-generator/cmd/conversion-gen
