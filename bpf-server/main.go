package main

import (
	"flag"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"golang.org/x/sys/unix"
)

var localAddr = flag.String("l", "localhost:8080", "local address")

func load() (*ebpf.Map, func() error) {
	if err := unix.Setrlimit(unix.RLIMIT_MEMLOCK, &unix.Rlimit{
		Cur: unix.RLIM_INFINITY,
		Max: unix.RLIM_INFINITY,
	}); err != nil {
		log.Printf("failed to set temporary rlimit: %v", err)
		return nil, nil
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
	return objs.SockMap, objs.Close
}

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target bpfel server bpf/server.c
func main() {
	flag.Parse()

	sockmap, clean := load()
	defer clean()

	listener, err := net.Listen("tcp", *localAddr)
	fatal(err)
	log.Println("Listening on", listener.Addr().String())

	for {
		conn, err := listener.Accept()
		fatal(err)
		tcpConn := conn.(*net.TCPConn)
		remoteAddr := tcpConn.RemoteAddr().String()
		_, port, err := net.SplitHostPort(remoteAddr)
		fatal(err)
		portNumber, err := strconv.Atoi(port)
		fatal(err)
		fatal(tcpConn.SetNoDelay(true))
		// There is a bug in sockmap which prevents it from
		// working right when snd buffer is full. Set it to
		// gigantic value.
		fatal(tcpConn.SetWriteBuffer(32 * 1024 * 1024))
		fd, err := tcpConn.File()
		fatal(err)

		log.Println("accepted connection from", uint32(portNumber))
		fatal(sockmap.Update(uint32(portNumber), uint32(fd.Fd()), ebpf.UpdateAny))
		go proxyConn(tcpConn)
	}
}

var response = []byte("HTTP/1.1 200 OK\r\n" +
	"content-length: 0\r\n" +
	"\r\n")

func proxyConn(conn *net.TCPConn) {
	buf := make([]byte, 1024)
	t0 := time.Now()
	reqs := 0
	for {
		_, err := conn.Read(buf)
		if err != nil {
			return
		}
		_, err = conn.Write(response)
		if err != nil {
			return
		}
		reqs++
		log.Println("Completed userspace request", reqs, "rate", uint64(float64(reqs)/time.Since(t0).Seconds()), "per second")
	}
}

func fatal(err error) {
	if err != nil {
		panic(err.Error())
	}
}
