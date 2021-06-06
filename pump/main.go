package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

var (
	//localAddr       = flag.String("l", ":15006", "local address")
	//monitoringAddr  = flag.String("m", ":5678", "monitoring address")
	remoteAddr      = flag.String("r", "localhost:8080", "remote address")
	remoteAddresses []string
)

var request = []byte("GET / HTTP/1.1\r\n" +
	"Host: localhost\r\n" +
	"Connection: keep-alive\r\n" +
	"\r\n")

var requests = repeat(request, 1024)

func repeat(bytes []byte, i int) []byte {
	ret := make([]byte, 0, len(bytes)*i)
	for n := 0; n < i; n++ {
		ret = append(ret, bytes...)
	}
	return ret
}

func main() {
	flag.Parse()
	remoteAddresses = strings.Split(*remoteAddr, ",")
	fmt.Printf("Sending: %v\n", *remoteAddr)

	//cId := atomic.AddUint64(&connectionId, 1) - 1
	us := remoteAddresses[0]
	rConnr, err := net.Dial("tcp", us)
	if err != nil {
		panic(err)
	}
	rConn := rConnr.(*net.TCPConn)
	log.Println("connected to upstream ", us)
	go func() {
		for {
			n, err := io.Copy(io.Discard, rConn)
			log.Println("copy", n, err)
			time.Sleep(time.Second)
		}
	}()
	start := time.Now()
	reqs := 0
	for {
		n, err := rConn.Write(requests)
		reqs++
		log.Println(n, err, reqs, 1024*float64(reqs)/time.Since(start).Seconds())
		//time.Sleep(time.Millisecond*10)
	}
}
func nop(...interface{}) {}
