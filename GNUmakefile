default: help

help: ## Prints help message.
	@ grep -h -E '^[a-zA-Z0-9_-].+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[1m%-30s\033[0m %s\n", $$1, $$2}'

VERSION=0.0.1
NAME=neo4j
BINARY=terraform-provider-$(NAME)_v$(VERSION)

build: ## Builds the provider.
	@ go build -a -gcflags=all="-l -B -C" -ldflags="-w -s -X main.version=$(VERSION)" -o $(BINARY) .

TEST?=$$(go list ./... | grep -v 'vendor')
test: ## Runs unit tests.
	go test -v -cover -timeout=120s -parallel=1 ./...

HOSTNAME=registry.terraform.io
NAMESPACE=kislerdm
PATH_PROVIDERS=/tmp/terraform/providers
define terraformrc
provider_installation {
  filesystem_mirror {
    path    = "$(PATH_PROVIDERS)"
  }
  direct {
    exclude = ["$(HOSTNAME)/$(NAMESPACE)/*"]
  }
}
endef
export terraformrc

.configure-local:
	@ echo "$$terraformrc" > ${HOME}/.terraformrc

OS?=$(shell uname | tr -s '[:lower:]')
ARCH?=$(shell uname -m)
PATH_PROVIDER := $(PATH_PROVIDERS)/$(HOSTNAME)/$(NAMESPACE)/$(NAME)/$(VERSION)/$(OS)_$(ARCH)

install: build .configure-local ## Builds and installs the provider.
	@ mkdir -p $(PATH_PROVIDER)
	@ mv $(BINARY) $(PATH_PROVIDER)/

generate: ## Generates docu.
	cd tools; go generate ./...

lint: ## Runs the linter.
	golangci-lint run

fmt: ## Formats the codebase.
	gofmt -s -w -e .

.PHONY: help fmt lint test testacc build install generate .configure-local
