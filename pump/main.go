package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
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
	monitoringAddr  = flag.String("m", ":5678", "monitoring address")
	remoteAddr      = flag.String("r", "localhost:8080", "remote address")
	connections     = flag.Int("c", 1, "number of connections")
	disablePipeline = flag.Bool("s", false, "disable pipelining")
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
	go StartMonitoring()
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

// Since Linux 2.6.11, the pipe capacity is 65536 bytes.
// TODO detect real size from https://github.com/hanwen/go-fuse/blob/v1.0.0/splice/splice.go#L72?
const DefaultPipeSize = 4 << 20

const _SPLICE_F_NONBLOCK = 0x2

func SpliceDiscard(conn *net.TCPConn) (n int64, err error) {
	r, w, err := os.Pipe()
	if err != nil {
		return 0, err
	}

	devNull, err := syscall.Open("/dev/null", os.O_WRONLY, 0)
	if err != nil {
		return 0, err
	}

	connFile, err := conn.File()
	if err != nil {
		return 0, err
	}
	connFd := int(connFile.Fd())
	rfd := int(r.Fd())
	wfd := int(w.Fd())
	reads := 0
	t0 := time.Now()
	for {
		readSize, err := syscall.Splice(connFd, nil, wfd, nil, DefaultPipeSize, _SPLICE_F_NONBLOCK)
		n += readSize
		if err != nil {
			return n, fmt.Errorf("splice to pipe: %v", err)
		}
		_, err = syscall.Splice(rfd, nil, devNull, nil, DefaultPipeSize, _SPLICE_F_NONBLOCK)
		if err != nil {
			return n, fmt.Errorf("splice from pipe: %v", err)
		}
		reads++
		if reads%1000 == 0 {
			log.Println(conn.LocalAddr().String(), "Completed read", reads,
				"rate", uint64(float64(reads)/time.Since(t0).Seconds()), "per second",
				ByteCount(n), "total",
				ByteCount(int64(float64(n)/time.Since(t0).Seconds())), "per second")
		}
	}
}

func DiscardReadFrom(r *net.TCPConn) (n int64, err error) {
	bufp := blackHolePool.Get().(*[]byte)
	readSize := 0
	reads := 0
	t0 := time.Now()
	for {
		readSize, err = r.Read(*bufp)
		n += int64(readSize)
		reads++
		if reads%1000 == 0 {
			log.Println(r.LocalAddr().String(), "Completed read", reads,
				"rate", uint64(float64(reads)/time.Since(t0).Seconds()), "per second",
				ByteCount(n), "total",
				ByteCount(int64(float64(n)/time.Since(t0).Seconds())), "per second")
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
	id := rConn.LocalAddr().String()
	log.Println(id, "connected to upstream ", remote)

	// Delay for bpf server bug
	time.Sleep(time.Millisecond * 500)
	start := time.Now()
	if *disablePipeline {
		bufLen := 1024
		localReqs := 0
		for {
			_, err = rConn.Write(request)
			if err != nil {
				log.Println(err)
				return
			}
			buf := make([]byte, bufLen)
			n, err := rConn.Read(buf)
			if err != nil {
				log.Println(err)
				return
			}
			if n == bufLen {
				log.Println("warning: filled up buffer")
			}
			greqs := reqs.Add(1)
			localReqs++
			if localReqs%1000 == 0 {
				log.Println("Completed request", localReqs, "rate", uint64(float64(localReqs)/time.Since(start).Seconds()), "per second",
					uint64(float64(greqs)/time.Since(start).Seconds()), "global per second",
				)
			}
		}
	}

	go func() {
		for {
			n, err := DiscardReadFrom(rConn)
			//n, err := SpliceDiscard(rConn)
			if n == 0 || err != nil {
				log.Fatal(err)
			}
			log.Println("copy", n, err)
		}
	}()
	for {
		_, err := rConn.Write(requests)
		if err != nil {
			log.Fatal(err)
		}
		nr := reqs.Add(1)
		if nr%1000 == 0 {
			// Each write has 512 requests
			log.Println(id, "Completed request", reqs, "rate", uint64(float64(nr*512)/time.Since(start).Seconds()), "per second")
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
