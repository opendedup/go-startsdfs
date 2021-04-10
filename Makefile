PWD := $(shell pwd)
GOPATH := $(shell go env GOPATH)

GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)

VERSION ?= $(shell git describe --tags)
BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
TAG ?= "sdfs/startsdfs:$(VERSION)"

all: getdeps build

checks:
	@echo "Checking dependencies"
	@(env bash $(PWD)/buildscripts/checkdeps.sh)

getdeps:
	@go get ./...
	@mkdir -p ${GOPATH}/bin
	@which golangci-lint 1>/dev/null || (echo "Installing golangci-lint" && curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v1.27.0)

crosscompile:
	@(env bash $(PWD)/buildscripts/cross-compile.sh)

verifiers: getdeps fmt lint

fmt:
	@echo "Running $@ check"
	@GO111MODULE=on gofmt -d app/
	@GO111MODULE=on gofmt -d fs/

lint:
	@echo "Running $@ check"
	@GO111MODULE=on ${GOPATH}/bin/golangci-lint cache clean
	@GO111MODULE=on ${GOPATH}/bin/golangci-lint run --timeout=5m --config ./.golangci.yml



# Builds startsdfs locally.
build:
	@echo "Building startsdfs binary to './startsdfs'"
	@go build  -ldflags="-X 'main.Version=$(BRANCH)' -X 'main.BuildDate=$$(date -Iseconds)'" -o ./startsdfs cmd/startsdfs/* 

# Builds startsdfs and installs it to $GOPATH/bin.
install: build
	@echo "Installing startsdfs binary to '$(GOPATH)/bin/startsdfs'"
	@mkdir -p $(GOPATH)/bin && cp -f $(PWD)/startsdfs $(GOPATH)/bin/startsdfs
	@echo "Installation successful. To learn more, try \"startsdfs -h\"."

clean:
	@echo "Cleaning up all the generated files"
	@find . -name '*.test' | xargs rm -fv
	@find . -name '*~' | xargs rm -fv
	@rm -rvf startsdfs
	@rm -rvf build
	@rm -rvf release
	@rm -rvf .verify*
