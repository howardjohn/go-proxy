package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/http/pprof"
	"net/url"
	"strings"
)

var (
	localAddr       = flag.String("l", ":15006", "local address")
	monitoringAddr  = flag.String("m", ":5678", "monitoring address")
	remoteAddr      = flag.String("r", "http://localhost:8080", "remote address")
	remoteAddresses []string
)

func main() {
	flag.Parse()
	remoteAddresses = strings.Split(*remoteAddr, ",")
	fmt.Printf("Listening: %v\nProxying: %v\n\n", *localAddr, *remoteAddr)

	go StartMonitoring()

	rurl, err := url.Parse(*remoteAddr)
	if err != nil {
		panic(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(rurl)
	http.HandleFunc("/", handler(proxy))
	err = http.ListenAndServe(*localAddr, nil)
	if err != nil {
		panic(err)
	}
}

func handler(p *httputil.ReverseProxy) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		p.ServeHTTP(w, r)
	}
}

func StartMonitoring() {
	// TODO add metrics
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	server := &http.Server{
		Handler: mux,
		Addr:    *monitoringAddr,
	}
	server.ListenAndServe()
	server.Close()
}
