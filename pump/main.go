package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.uber.org/atomic"
)

var (
	//localAddr       = flag.String("l", ":15006", "local address")
	//monitoringAddr  = flag.String("m", ":5678", "monitoring address")
	remoteAddr      = flag.String("r", "localhost:8080", "remote address")
	connections     = flag.Int("c", 1, "number of connections")
	remoteAddresses []string
)

var request = []byte("GET / HTTP/1.1\r\n" +
	"Host: localhost\r\n" +
	"\r\n")

var requests = repeat(request, 512)

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
	remote := remoteAddresses[0]
	for conn := 0; conn < *connections; conn++ {
		go connect(remote)
	}
	WaitSignal()
}
func WaitSignal() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
}

var reqs = atomic.NewUint64(0)

func connect(remote string) {
	rConnr, err := net.Dial("tcp", remote)
	if err != nil {
		panic(err)
	}
	rConn := rConnr.(*net.TCPConn)
	log.Println("connected to upstream ", remote)
	go func() {
		for {
			n, err := io.Copy(io.Discard, rConn)
			if n == 0 || err != nil {
				log.Fatal(err)
			}
			log.Println("copy", n, err)
		}
	}()
	start := time.Now()
	for {
		n, err := rConn.Write(requests)
		if err != nil {
			log.Fatal(err)
		}
		nr := reqs.Add(512) // Each write has 512 requests
		log.Println(n, err, reqs, float64(nr)/time.Since(start).Seconds())
	}
}
func nop(...interface{}) {}
