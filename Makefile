GO_IMAGE := golang:1.26
GO := docker run --rm -v $(CURDIR):/src -w /src -v cwq-gocache:/go $(GO_IMAGE)

# build makes a binary for this machine, cross-compiled inside the container.
GOOS := $(shell uname -s | tr '[:upper:]' '[:lower:]')
GOARCH := $(shell uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')

test:
	$(GO) sh -c 'go vet ./... && go test ./...'

tidy:
	$(GO) go mod tidy

build:
	$(GO) env CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -trimpath -o cwq ./cmd/cwq

.PHONY: test tidy build
