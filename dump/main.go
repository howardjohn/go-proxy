package main

import (
	"flag"
	"log"
	"net"
	"time"

	"go.uber.org/atomic"
)

var (
	//localAddr       = flag.String("l", ":15006", "local address")
	//monitoringAddr  = flag.String("m", ":5678", "monitoring address")
	localAddr      = flag.String("l", "localhost:8080", "local address")
)

var response = []byte("HTTP/1.1 200 OK\r\n" +
	"content-length: 0\r\n" +
	"\r\n")

func repeat(bytes []byte, i int) []byte {
	ret := make([]byte, 0, len(bytes)*i)
	for n := 0; n < i; n++ {
		ret = append(ret, bytes...)
	}
	return ret
}

func main() {
	flag.Parse()
	listener, err := net.Listen("tcp", *localAddr)
	if err != nil {
		panic(err)
	}
	first := true
	for {
		conn, err := listener.Accept()
		if err != nil {
			panic(err)
		}
		if first {
			progStart = time.Now()
			first = false
		}
		log.Println("accepted connection")
		go proxyConn(conn.(*net.TCPConn))
	}
}

var total = atomic.NewUint64(0)
var progStart = time.Now()

func proxyConn(conn *net.TCPConn) {
	buf := make([]byte, 1024)
	t0 := time.Now()
	reqs := 0
	for {
		_, err := conn.Read(buf)
		if err != nil {
			log.Println(err)
			return
		}
		_, err = conn.Write(response)
		if err != nil {
			log.Println(err)
			return
		}
		greqs := total.Add(1)
		reqs++
		if reqs % 1000 == 0 {
			log.Println("Completed request", reqs, "rate", uint64(float64(reqs)/time.Since(t0).Seconds()), "per second",
				uint64(float64(greqs)/time.Since(progStart).Seconds()), "global per second",
				)
		}
	}
}
