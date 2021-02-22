package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	localAddr       = flag.String("l", ":15006", "local address")
	monitoringAddr  = flag.String("m", ":5678", "monitoring address")
	remoteAddr      = flag.String("r", "localhost:8080", "remote address")
	remoteAddresses []string
)

func main() {
	flag.Parse()
	remoteAddresses = strings.Split(*remoteAddr, ",")
	fmt.Printf("Listening: %v\nProxying: %v\n\n", *localAddr, *remoteAddr)

	go StartMonitoring()

	listener, err := net.Listen("tcp", *localAddr)
	if err != nil {
		panic(err)
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			panic(err)
		}
		log.Println("accepted connection")
		go proxyConn(conn.(*net.TCPConn))
	}
}

var connectionId uint64 = 0

func proxyConn(conn *net.TCPConn) {
	cId := atomic.AddUint64(&connectionId, 1) - 1
	us := remoteAddresses[cId%uint64(len(remoteAddresses))]
	rConnr, err := net.Dial("tcp", us)
	if err != nil {
		panic(err)
	}
	rConn := rConnr.(*net.TCPConn)

	log.Println("connected to upstream ", us)
	t0 := time.Now()
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		splice(conn, rConn)
		log.Println("upstream complete")
		conn.Close()
		rConn.Close()
		wg.Done()
	}()
	splice(rConn, conn)
	log.Println("downstream complete")
	conn.Close()
	rConn.Close()
	wg.Wait()
	log.Println("connection closed in ", time.Since(t0))
}

func splice(to, from *net.TCPConn) {
	n, err := to.ReadFrom(from)
	log.Printf("spliced %v\n", n)
	if err != nil && !errors.Is(err, net.ErrClosed) {
		log.Printf("error in read: %v\n", err)
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
