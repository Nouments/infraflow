.PHONY: fmt fmt-check test test-race test-provider test-agent vet build build-provider build-agent

fmt:
	gofmt -w $(shell find agent internal pkg provider -name '*.go')

fmt-check:
	test -z "$$(gofmt -l $$(find agent internal pkg provider -type f -name '*.go'))"

test:
	go test ./...

test-race:
	go test -race ./...

test-provider:
	go test ./provider/...

test-agent:
	go test ./agent/...

vet:
	go vet ./...

build:
	$(MAKE) build-provider build-agent

build-provider:
	mkdir -p bin
	go build -o bin/infraflow-provider ./provider/cmd

build-agent:
	mkdir -p bin
	go build -o bin/infraflow-agent ./agent/cmd