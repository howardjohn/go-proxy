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
	"sync"
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

var blackHolePool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 8192)
		return &b
	},
}

func DiscardReadFrom(r io.Reader) (n int64, err error) {
	bufp := blackHolePool.Get().(*[]byte)
	readSize := 0
	reads := 0
	t0 := time.Now()
	for {
		readSize, err = r.Read(*bufp)
		n += int64(readSize)
		reads++
		if reads%1000 == 0 {
			log.Println("Completed read", reads, "rate", uint64(float64(reads)/time.Since(t0).Seconds()), "per second", ByteCount(n), "total")
		}
		if err != nil {
			blackHolePool.Put(bufp)
			if err == io.EOF {
				return n, nil
			}
			return
		}
	}
}

func connect(remote string) {
	rConnr, err := net.Dial("tcp", remote)
	if err != nil {
		panic(err)
	}
	rConn := rConnr.(*net.TCPConn)
	log.Println("connected to upstream ", remote)
	go func() {
		for {
			n, err := DiscardReadFrom(rConn)
			if n == 0 || err != nil {
				log.Fatal(err)
			}
			log.Println("copy", n, err)
		}
	}()
	start := time.Now()
	for {
		_, err := rConn.Write(requests)
		if err != nil {
			log.Fatal(err)
		}
		nr := reqs.Add(1)
		if nr%1000 == 0 {
			// Each write has 512 requests
			log.Println("Completed request", reqs, "rate", uint64(float64(nr*512)/time.Since(start).Seconds()), "per second")
		}
	}
}
func nop(...interface{}) {}

// ByteCount returns a human readable byte format
// Inspired by https://yourbasic.org/golang/formatting-byte-size-to-human-readable-format/
func ByteCount(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB",
		float64(b)/float64(div), "kMGTPE"[exp])
}
