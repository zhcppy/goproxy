// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/zhcppy/goproxy/proxy"
	"golang.org/x/mod/module"
)

const listExpire = proxy.ListExpire

func init() {
	if os.Getenv("GIT_TERMINAL_PROMPT") == "" {
		os.Setenv("GIT_TERMINAL_PROMPT", "0")
	}
	if os.Getenv("GIT_SSH") == "" && os.Getenv("GIT_SSH_COMMAND") == "" {
		os.Setenv("GIT_SSH_COMMAND", "ssh -o ControlMaster=no")
	}

	// Enable Go module
	os.Setenv("GO111MODULE", "on")

	log.SetPrefix("goproxy: ")
	log.SetFlags(0)
}

func main() {
	var listen, cache, proxyUrl, exclude string
	flag.StringVar(&exclude, "exclude", "", "exclude package path pattern")
	flag.StringVar(&proxyUrl, "proxy", "https://goproxy.io", "next hop proxy for go modules")
	flag.StringVar(&cache, "cache", os.Getenv("GOPATH"), "go modules cache dir")
	flag.StringVar(&listen, "listen", "0.0.0.0:8081", "service listen address")
	flag.Parse()

	if exclude != "" {
		os.Setenv("GOPRIVATE", exclude)
	}

	var downloadRoot = getDownloadRoot(cache)
	var proxyServer = proxy.NewServer(&ops{downloadRoot: downloadRoot})

	var handle http.Handler
	if proxyUrl != "" {
		log.Printf("ProxyUrl: %s\n", proxyUrl)
		log.Printf("Exclude: %s\n", exclude)
		handle = &httpHandler{proxy.NewRouter(proxyServer, exclude, proxyUrl, downloadRoot)}
	} else {
		handle = &httpHandler{proxyServer}
	}

	httpServer := &http.Server{Addr: listen, Handler: handle}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil {
			if err != http.ErrServerClosed {
				log.Fatal(err)
			}
		}
	}()

	s := make(chan os.Signal, 1)
	signal.Notify(s, os.Interrupt, syscall.SIGTERM)
	<-s
	log.Println("Making a graceful shutdown...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("Error while shutting down the server: %v", err)
	}
	log.Println("Successful server shutdown.")
}

func getDownloadRoot(cache string) string {
	var env struct {
		GOPATH string
	}
	if cache != "" {
		os.Setenv("GOPATH", cache)
		return filepath.Join(cache, "pkg", "mod", "cache", "download")
	}
	if err := goJSON(&env, "go", "env", "-json", "GOPATH"); err != nil {
		log.Fatal(err)
	}
	list := filepath.SplitList(env.GOPATH)
	if len(list) == 0 || list[0] == "" {
		log.Fatalf("missing $GOPATH")
	}
	return filepath.Join(list[0], "pkg", "mod", "cache", "download")
}

// goJSON runs the go command and parses its JSON output into dst.
func goJSON(dst interface{}, command ...string) error {
	cmd := exec.Command(command[0], command[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s:\n%s%s", strings.Join(command, " "), stderr.String(), stdout.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), dst); err != nil {
		return fmt.Errorf("%s: reading json: %v", strings.Join(command, " "), err)
	}
	return nil
}

// Just to print the log
type httpHandler struct {
	handler http.Handler
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rl := &responseLogger{code: 200, ResponseWriter: w}
	h.handler.ServeHTTP(rl, r)
	log.Printf("%.3fs %d %s\n", time.Since(start).Seconds(), rl.code, r.URL)
}

type responseLogger struct {
	code int
	http.ResponseWriter
}

func (r *responseLogger) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

// An ops is a proxy.ServerOps implementation.
type ops struct {
	downloadRoot string
}

func (ops *ops) NewContext(r *http.Request) (context.Context, error) {
	return context.Background(), nil
}
func (ops *ops) List(ctx context.Context, mpath string) (proxy.File, error) {
	escMod, err := module.EscapePath(mpath)
	if err != nil {
		return nil, err
	}
	file := filepath.Join(ops.downloadRoot, escMod, "@v", "list")
	if info, err := os.Stat(file); err == nil && time.Since(info.ModTime()) < listExpire {
		return os.Open(file)
	}
	var list struct {
		Path     string
		Versions []string
	}
	if err := goJSON(&list, "go", "list", "-m", "-json", "-versions", mpath+"@latest"); err != nil {
		return nil, err
	}
	if list.Path != mpath {
		return nil, fmt.Errorf("go list -m: asked for %s but got %s", mpath, list.Path)
	}
	data := []byte(strings.Join(list.Versions, "\n") + "\n")
	if len(data) == 1 {
		data = nil
	}
	err = os.MkdirAll(path.Dir(file), os.ModePerm)
	if err != nil {
		log.Printf("make cache dir failed, err: %v.", err)
		return nil, err
	}
	if err := ioutil.WriteFile(file, data, 0666); err != nil {
		return nil, err
	}

	return os.Open(file)
}
func (*ops) Latest(ctx context.Context, path string) (proxy.File, error) {
	d, err := download(module.Version{Path: path, Version: "latest"})
	if err != nil {
		return nil, err
	}
	return os.Open(d.Info)
}
func (*ops) Info(ctx context.Context, m module.Version) (proxy.File, error) {
	d, err := download(m)
	if err != nil {
		return nil, err
	}
	return os.Open(d.Info)
}
func (*ops) GoMod(ctx context.Context, m module.Version) (proxy.File, error) {
	d, err := download(m)
	if err != nil {
		return nil, err
	}
	return os.Open(d.GoMod)
}
func (*ops) Zip(ctx context.Context, m module.Version) (proxy.File, error) {
	d, err := download(m)
	if err != nil {
		return nil, err
	}
	return os.Open(d.Zip)
}

type downloadInfo struct {
	Path     string
	Version  string
	Info     string
	GoMod    string
	Zip      string
	Dir      string
	Sum      string
	GoModSum string
}

func download(m module.Version) (*downloadInfo, error) {
	d := new(downloadInfo)
	return d, goJSON(d, "go", "mod", "download", "-json", m.String())
}
