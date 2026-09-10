GO ?= go

default: build

.PHONY: build
build:
	$(GO) build ./...

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: lint
lint:
	gofmt -l . | tee /dev/stderr | (! read)
	$(GO) vet ./...
	golangci-lint run

# Unit tests plus the Porkbun /mock decode tests. No credentials needed.
.PHONY: test
test:
	$(GO) test ./... -timeout 10m

# Full lifecycle tests against the in-process fake API. Still no credentials:
# TF_ACC only tells terraform-plugin-testing to drive a real Terraform CLI.
.PHONY: testacc
testacc:
	TF_ACC=1 $(GO) test ./... -v -timeout 30m

# Regenerate docs/ from the schemas and examples/. tfplugindocs lives in the
# tools module so its dependency tree stays out of the provider's go.sum.
.PHONY: docs
docs:
	cd tools && $(GO) generate ./...

.PHONY: install
install:
	$(GO) install .
