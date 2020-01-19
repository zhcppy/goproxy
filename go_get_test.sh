#!/usr/bin/env bash

export GO111MODULE=on
export GOPROXY='http://127.0.0.1:8081'
export GOPATH=/tmp/go

if [ "$1" ]; then
  export GOPROXY='https://goproxy.io'
fi

cd /tmp || exit

while read -r line; do
	go get -v "${line}"
done << EOF
golang.org/x/net@latest
github.com/oiooj/agent@v0.2.2
github.com/micro/go-api/resolver@v0.5.0
cloud.google.com/go
golang.org/x/tools/cmd/gopls
golang.org/x/tools/cmd/guru@latest
github.com/gorilla/mux@v1.7.3
EOF