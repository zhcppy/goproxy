// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhcppy/goproxy/renameio"
)

const ListExpire = 5 * time.Minute

// A Router is the proxy HTTP server,
// which implements Route Filter to
// routing private module or public module .
type Router struct {
	server       *Server
	proxy        *httputil.ReverseProxy
	pattern      string
	downloadRoot string
}

// NewRouter returns a new Router using the given operations.
func NewRouter(server *Server, proxyUrl, pattern, downRoot string) *Router {
	rt := &Router{server: server}

	if proxyUrl == "" {
		log.Println("not set proxy, all direct.")
		return rt
	}
	remote, err := url.Parse(proxyUrl)
	if err != nil {
		log.Println("parse proxy fail, all direct.", err.Error())
		return rt
	}

	rt.proxy = httputil.NewSingleHostReverseProxy(remote)
	director := rt.proxy.Director
	rt.proxy.Director = func(r *http.Request) {
		director(r)
		r.Host = remote.Host
	}

	rt.proxy.Transport = &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	rt.proxy.ModifyResponse = func(r *http.Response) error {
		if r.StatusCode != http.StatusOK {
			log.Println("response status code:", r.StatusCode)
			return nil
		}
		var buf []byte
		if strings.Contains(r.Header.Get("Content-Encoding"), "gzip") {
			gr, err := gzip.NewReader(r.Body)
			if err != nil {
				return err
			}
			defer gr.Close()
			buf, err = ioutil.ReadAll(gr)
			if err != nil {
				return err
			}
			r.Header.Del("Content-Encoding")
		} else {
			buf, err = ioutil.ReadAll(r.Body)
			if err != nil {
				return err
			}
		}
		r.Body = ioutil.NopCloser(bytes.NewReader(buf))
		if buf == nil {
			return nil
		}
		file := filepath.Join(downRoot, r.Request.URL.Path)
		os.MkdirAll(path.Dir(file), os.ModePerm)
		return renameio.WriteFile(file, buf, 0666)
	}
	rt.pattern = pattern
	rt.downloadRoot = downRoot
	return rt
}

func (rt *Router) Direct(path string) bool {
	if rt.pattern == "" {
		return false
	}
	return GlobsMatchPath(rt.pattern, path)
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if rt.proxy == nil || rt.Direct(strings.TrimPrefix(r.URL.Path, "/")) {
		log.Printf("------ --- %s [direct]\n", r.URL)
		rt.server.ServeHTTP(w, r)
		return
	}

	fileName := filepath.Join(rt.downloadRoot, r.URL.Path)
	fileInfo, err1 := os.Stat(fileName)
	file, err2 := os.Open(fileName)
	if err1 != nil || err2 != nil {
		log.Printf("------ --- %s [proxy]\n", r.URL)
		rt.proxy.ServeHTTP(w, r)
		return
	}
	defer file.Close()
	var contentType string
	if strings.HasSuffix(r.URL.Path, "/@latest") {
		if time.Since(fileInfo.ModTime()) >= ListExpire {
			log.Printf("------ --- %s [proxy]\n", r.URL)
			rt.proxy.ServeHTTP(w, r)
		} else {
			contentType = "text/plain; charset=UTF-8"
			w.Header().Set("Content-Type", contentType)
			log.Printf("------ --- %s [cached]\n", r.URL)
			http.ServeContent(w, r, "", fileInfo.ModTime(), file)
		}
		return
	}

	pathIndex := strings.Index(r.URL.Path, "/@v/")
	if pathIndex < 0 {
		http.Error(w, "no such path", http.StatusNotFound)
		return
	}

	what := r.URL.Path[pathIndex+len("/@v/"):]
	if what == "list" {
		if time.Since(fileInfo.ModTime()) >= ListExpire {
			log.Printf("------ --- %s [proxy]\n", r.URL)
			rt.proxy.ServeHTTP(w, r)
			return
		} else {
			contentType = "text/plain; charset=UTF-8"
		}
	} else {
		ext := path.Ext(what)
		switch ext {
		case ".info":
			contentType = "application/json"
		case ".mod":
			contentType = "text/plain; charset=UTF-8"
		case ".zip":
			contentType = "application/octet-stream"
		default:
			http.Error(w, "request not recognized", http.StatusNotFound)
			return
		}
	}
	w.Header().Set("Content-Type", contentType)
	log.Printf("------ --- %s [cached]\n", r.URL)
	http.ServeContent(w, r, "", fileInfo.ModTime(), file)
	return
}

// GlobsMatchPath reports whether any path prefix of target
// matches one of the glob patterns (as defined by path.Match)
// in the comma-separated globs list.
// It ignores any empty or malformed patterns in the list.
func GlobsMatchPath(globs, target string) bool {
	for globs != "" {
		// Extract next non-empty glob in comma-separated list.
		var glob string
		if i := strings.Index(globs, ","); i >= 0 {
			glob, globs = globs[:i], globs[i+1:]
		} else {
			glob, globs = globs, ""
		}
		if glob == "" {
			continue
		}

		// A glob with N+1 path elements (N slashes) needs to be matched
		// against the first N+1 path elements of target,
		// which end just before the N+1'th slash.
		n := strings.Count(glob, "/")
		prefix := target
		// Walk target, counting slashes, truncating at the N+1'th slash.
		for i := 0; i < len(target); i++ {
			if target[i] == '/' {
				if n == 0 {
					prefix = target[:i]
					break
				}
				n--
			}
		}
		if n > 0 {
			// Not enough prefix elements.
			continue
		}
		matched, _ := path.Match(glob, prefix)
		if matched {
			return true
		}
	}
	return false
}
