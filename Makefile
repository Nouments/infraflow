.PHONY: fmt test vet build

fmt:
	gofmt -w ./cmd ./internal

test:
	go test ./...

vet:
	go vet ./...

build:
	go build -o bin/infraflow ./cmd/infraflow