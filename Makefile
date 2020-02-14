.PHONY: install build image clean test

export GO111MODULE=on
export GOPROXY=https://goproxy.io

all: install

install:
	@go install -v -ldflags "-s -w" .

build: gomod
	@go build -o bin/goproxy -ldflags "-s -w" .

gomod:
	@go mod tidy
	@go mod download

image:
	@docker build -t zhcppy/goproxy .

test: gomod
	@go test -v ./...

clean:
	@git clean -f -d -X
