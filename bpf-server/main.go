package main

import (
	"flag"
	"fmt"
	"log"
	"net"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"golang.org/x/sys/unix"
)

var (
	localAddr = flag.String("l", "localhost:8080", "local address")
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go server bpf/server.c
func main() {
	flag.Parse()
	// Increase the rlimit of the current process to provide sufficient space
	// for locking memory for the eBPF map.
	if err := unix.Setrlimit(unix.RLIMIT_MEMLOCK, &unix.Rlimit{
		Cur: unix.RLIM_INFINITY,
		Max: unix.RLIM_INFINITY,
	}); err != nil {
		log.Printf("failed to set temporary rlimit: %v", err)
		return
	}
	var objs serverObjects
	if err := loadServerObjects(&objs, nil); err != nil {
		panic("Can't load objects: " + err.Error())
	}
	defer objs.Close()

	// Do something useful with the program.
	fmt.Println(objs)
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

	listener, err := net.Listen("tcp", *localAddr)
	fatal(err)
	for {
		conn, err := listener.Accept()
		fatal(err)
		tcpConn := conn.(*net.TCPConn)
		fatal(tcpConn.SetNoDelay(true))
		fd, err := tcpConn.File()
		fatal(err)

		fatal(objs.SockMap.Update(uint32(0), uint32(fd.Fd()), ebpf.UpdateAny))
		log.Println("accepted connection")
		go proxyConn(tcpConn)
	}
}

func proxyConn(conn *net.TCPConn) {
	// TODO SO_SNDBUF
}

func fatal(err error) {
	if err != nil {
		panic(err.Error())
	}
}
