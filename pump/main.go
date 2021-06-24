package main

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target bpfel server bpf/server.c
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"go.uber.org/atomic"
	"golang.org/x/sys/unix"
)

var (
	// localAddr       = flag.String("l", ":15006", "local address")
	monitoringAddr  = flag.String("m", ":5678", "monitoring address")
	remoteAddr      = flag.String("r", "localhost:8080", "remote address")
	connections     = flag.Int("c", 1, "number of connections")
	disablePipeline = flag.Bool("s", false, "disable pipelining")
	useEbpf         = flag.Bool("b", false, "drain with ebpf")
	serverPipelines = flag.Bool("server-pipelines", false, "should be set to true if the server supports pipelining, to get accurate metrics")
	remoteAddresses []string
)

var request = []byte("GET / HTTP/1.1\r\n" +
	"Host: localhost\r\n" +
	"\r\n")

var requests = repeat(request, 512)
var jumbo = repeat(request, 4194304)

func repeat(bytes []byte, i int) []byte {
	ret := make([]byte, 0, len(bytes)*i)
	for n := 0; n < i; n++ {
		ret = append(ret, bytes...)
	}
	return ret
}

func main() {
	flag.Parse()
	if len(flag.Args()) > 0 {
		fatal(fmt.Errorf("unexpected args %v", flag.Args()))
	}
	remoteAddresses = strings.Split(*remoteAddr, ",")
	log.Printf("Sending to %v, with %d connections, pipeline=%v, ebpf=%v", *remoteAddr, *connections, !*disablePipeline, *useEbpf)
	go StartMonitoring()

	remote := remoteAddresses[0]
	if *useEbpf {
		sockmap, counter, clean := load()
		defer clean()
		for conn := 0; conn < *connections; conn++ {
			go connectEbpf(remote, sockmap, counter)
			counter = nil
		}
	} else {
		for conn := 0; conn < *connections; conn++ {
			go connect(remote)
		}
	}

	WaitSignal()
}

func connectEbpf(remote string, sockmap *ebpf.Map, counter *ebpf.Map) {
	rConnr, err := net.Dial("tcp", remote)
	if err != nil {
		panic(err)
	}

	rConn := rConnr.(*net.TCPConn)
	id := rConn.LocalAddr().String()
	log.Println(id, "connected to upstream ", remote)

	_, port, err := net.SplitHostPort(rConn.LocalAddr().String())
	fatal(err)
	portNumber, err := strconv.Atoi(port)
	fatal(err)

	file, err := rConn.File()
	fatal(err)

	fatal(sockmap.Update(uint32(portNumber), uint32(file.Fd()), ebpf.UpdateAny))
	// Delay for bpf server bug
	time.Sleep(time.Millisecond * 500)

	start := time.Now()
	localReqs := 0
	toSend := request
	multiplier := 1
	if !*disablePipeline {
		toSend = jumbo
		multiplier = 4194304
	}

	if counter != nil {
		go func() {
			start := time.Now()
			prevTime := time.Now()
			prev := uint64(0)
			for {
				var v uint64
				err := counter.Lookup(uint64(0), &v)
				if err != nil {
					log.Println("failed to extract request count", err)
					time.Sleep(time.Second)
					continue
				}
				log.Println("Completed request", v,
					"rate", uint64(float64(v)/time.Since(start).Seconds()), "per second,",
					"recent rate", uint64(float64(v - prev)/time.Since(prevTime).Seconds()), "per second",
				)
				prev = v
				prevTime = time.Now()
				time.Sleep(time.Second)
			}
		}()
	}

	for {
		_, err = rConn.Write(toSend)
		if err != nil {
			log.Println(err)
			return
		}
		// No need to read the request, ebpf code will just drop it
		// Eventually we will fill the write buffer, causing write to block.
		greqs := reqs.Add(1)
		localReqs++
		if localReqs%1000 == 0 && !*serverPipelines {
			log.Println("Completed request", localReqs, "rate", uint64(float64(localReqs*multiplier)/time.Since(start).Seconds()), "per second",
				uint64(float64(greqs*uint64(multiplier))/time.Since(start).Seconds()), "global per second",
			)
		}
	}
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
			// n, err := SpliceDiscard(rConn)
			if n == 0 || err != nil {
				log.Fatal(err)
			}
			log.Println("copy", n, err)
		}
	}()
	for {
		_, err := rConn.Write(jumbo)
		if err != nil {
			log.Fatal(err)
		}
		nr := reqs.Add(1)
		if nr%1000 == 0 {
			// Each write has 512 requests
			log.Println(id, "Completed request", reqs, "rate", uint64(float64(nr*4194304)/time.Since(start).Seconds()), "per second")
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

func load() (*ebpf.Map, *ebpf.Map, func() error) {
	if err := unix.Setrlimit(unix.RLIMIT_MEMLOCK, &unix.Rlimit{
		Cur: unix.RLIM_INFINITY,
		Max: unix.RLIM_INFINITY,
	}); err != nil {
		log.Printf("failed to set temporary rlimit: %v", err)
		return nil, nil, nil
	}
	var objs serverObjects
	if err := loadServerObjects(&objs, nil); err != nil {
		panic("Can't load objects: " + err.Error())
	}

	// Do something useful with the program.
	fatal(link.RawAttachProgram(link.RawAttachProgramOptions{
		Target:  objs.SockMap.FD(),
		Program: objs.serverPrograms.ProgParser,
		Attach:  ebpf.AttachSkSKBStreamParser,
	}))
	fatal(link.RawAttachProgram(link.RawAttachProgramOptions{
		Target:  objs.SockMap.FD(),
		Program: objs.serverPrograms.ProgVerdict,
		Attach:  ebpf.AttachSkSKBStreamVerdict,
	}))
	return objs.SockMap, objs.Counter, objs.Close
}

func fatal(err error) {
	if err != nil {
		panic(err.Error())
	}
}
