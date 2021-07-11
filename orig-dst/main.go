package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	syscall "golang.org/x/sys/unix"
)

var (
	localAddr      = flag.String("l", ":15006,:15001", "local addresses")
	monitoringAddr = flag.String("m", ":5678", "monitoring address")
)

var UpstreamLocalAddressIPv4 = &net.TCPAddr{IP: net.ParseIP("127.0.0.6")}

func main() {
	flag.Parse()
	localAddrs := strings.Split(*localAddr, ",")
	fmt.Println("Listening:", localAddrs)

	go StartMonitoring()

	for _, addr := range localAddrs {
		addr := addr
		go func() {
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				panic(err)
			}

			for {
				conn, err := listener.Accept()
				if err != nil {
					panic(err)
				}
				log.Println("accepted connection on", addr)
				go proxyConn(conn.(*net.TCPConn))
			}
		}()
	}
	WaitSignal()
}

func WaitSignal() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
}

var connectionId uint64 = 0

func proxyConn(conn *net.TCPConn) {
	orig, err := GetOriginalDST(conn)
	if err != nil {
		log.Println("failed to find original destination")
		conn.Close()
		return
	}
	log.Println("original dst", orig)
	us := orig.String()
	d := &net.Dialer{
		LocalAddr: UpstreamLocalAddressIPv4,
	}
	rConnr, err := d.Dial("tcp", us)
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
