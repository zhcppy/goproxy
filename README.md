# GOPROXY

A global proxy for go modules.

## Requirements

It invokes the local go command to answer requests.
The default cacheDir is GOPATH, you can set it up by yourself according to the situation.

## Install

```shell script
git clone https://github.com/zhcppy/goproxy.git
cd goproxy
make
```

### Router mode    

Use the -proxy flag switch to "Router mode", which 
implements route filter to routing private module 
or public module .

```
                                         direct
                      +----------------------------------> private repo
                      |
                 match|pattern
                      |
                  +---+---+           +----------+
go get  +-------> |goproxy| +-------> |goproxy.io| +---> golang.org/x/net
                  +-------+           +----------+
                 router mode           proxy mode
```

In Router mode, use the -exclude flag set pattern , direct to the repo which 
match the module path, pattern are matched to the full path specified, not only 
to the host component.

```shell script
goproxy -listen=0.0.0.0:80 -cache=/tmp/test -proxy https://goproxy.io -exclude "*.corp.example.com,rsc.io/private"
```

## Use docker image

    docker run -d -p80:8081 goproxy/goproxy

Use the -v flag to persisting the proxy module data (change ___cache___ to your own dir):

    docker run -d -p80:8081 -v cache:/go goproxy/goproxy

## Docker Compose

    docker-compose up

## Appendix

1. set `export GOPROXY=http://localhost` to enable your goproxy.
2. set `export GOPROXY=direct` to disable it.
