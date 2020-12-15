# Root directory of the project (absolute path).
ROOTDIR=$(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# Used to populate version variable in main package.
VERSION=$(shell git describe --match 'v[0-9]*' --dirty='.m' --always)
REVISION=$(shell git rev-parse HEAD)$(shell if ! git diff --no-ext-diff --quiet --exit-code; then echo .m; fi)
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%S")

# Build with go modules.
GOPROXY ?= https://proxy.golang.org
GO111MODULE=on

PKG=github.com/docker/distribution

# Project packages.
PACKAGES=$(shell go list -tags "${BUILDTAGS}" ./... | grep -v /vendor/)
INTEGRATION_PACKAGE=${PKG}
COVERAGE_PACKAGES=$(filter-out ${PKG}/registry/storage/driver/%,${PACKAGES})


# Project binaries.
COMMANDS=registry digest registry-api-descriptor-template

# Allow turning off function inlining and variable registerization
ifeq (${DISABLE_OPTIMIZATION},true)
	GO_GCFLAGS=-gcflags "-N -l"
	VERSION:="$(VERSION)-noopt"
endif

WHALE = "+"

# Go files
#
TESTFLAGS_RACE=
GOFILES=$(shell find . -type f -name '*.go')
GO_TAGS=$(if $(BUILDTAGS),-tags "$(BUILDTAGS)",)
GO_LDFLAGS=-ldflags '-s -w -X $(PKG)/version.Version=$(VERSION) -X $(PKG)/version.Revision=$(REVISION) -X $(PKG)/version.Package=$(PKG) -X $(PKG)/version.BuildTime=$(BUILD_TIME) $(EXTRA_LDFLAGS)'

BINARIES=$(addprefix bin/,$(COMMANDS))

# Flags passed to `go test`
TESTFLAGS ?= -v $(TESTFLAGS_RACE)
TESTFLAGS_PARALLEL ?= 8

.PHONY: all build binaries check clean test test-race test-full integration coverage
.DEFAULT: all

all: binaries

check: ## run golangci-lint, with defaults
	@echo "$(WHALE) $@"
	golangci-lint run

test: ## run tests, except integration test with test.short
	@echo "$(WHALE) $@"
	@go test ${GO_TAGS} -test.short ${TESTFLAGS} $(filter-out ${INTEGRATION_PACKAGE},${PACKAGES})

test-race: ## run tests, except integration test with test.short and race
	@echo "$(WHALE) $@"
	@go test ${GO_TAGS} -race -test.short ${TESTFLAGS} $(filter-out ${INTEGRATION_PACKAGE},${PACKAGES})

test-full: ## run tests, except integration tests
	@echo "$(WHALE) $@"
	@go test ${GO_TAGS} ${TESTFLAGS} $(filter-out ${INTEGRATION_PACKAGE},${PACKAGES})

integration: ## run integration tests
	@echo "$(WHALE) $@"
	@go test ${TESTFLAGS} -parallel ${TESTFLAGS_PARALLEL} ${INTEGRATION_PACKAGE}

coverage: ## generate coverprofiles from the unit tests
	@echo "$(WHALE) $@"
	@rm -f coverage.txt
	@go test ${GO_TAGS} -i ${TESTFLAGS} $(filter-out ${INTEGRATION_PACKAGE},${COVERAGE_PACKAGES}) 2> /dev/null
	@( for pkg in $(filter-out ${INTEGRATION_PACKAGE},${COVERAGE_PACKAGES}); do \
		go test ${GO_TAGS} ${TESTFLAGS} \
			-cover \
			-coverprofile=profile.out \
			-covermode=atomic $$pkg || exit; \
		if [ -f profile.out ]; then \
			cat profile.out >> coverage.txt; \
			rm profile.out; \
		fi; \
	done )

FORCE:

# Build a binary from a cmd.
bin/%: cmd/% FORCE
	@echo "$(WHALE) $@${BINARY_SUFFIX}"
	@go build ${GO_GCFLAGS} ${GO_BUILD_FLAGS} -o $@${BINARY_SUFFIX} ${GO_LDFLAGS} ${GO_TAGS}  ./$<

binaries: $(BINARIES) ## build binaries
	@echo "$(WHALE) $@"

build:
	@echo "$(WHALE) $@"
	@go build ${GO_GCFLAGS} ${GO_BUILD_FLAGS} ${GO_LDFLAGS} ${GO_TAGS} $(PACKAGES)

clean: ## clean up binaries
	@echo "$(WHALE) $@"
	@rm -f $(BINARIES)
