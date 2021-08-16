package rsyncd

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
)

type Server struct {
}

func (s *Server) handleConn(conn net.Conn) error {
	const terminationCommand = "@RSYNCD: OK\n"
	rd := bufio.NewReader(conn)
	// send server greeting
	const protocolVersion = "27" // TODO: which is which?
	fmt.Fprintf(conn, "@RSYNCD: %s\n", protocolVersion)

	// read client greeting
	clientGreeting, err := rd.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(clientGreeting, "@RSYNCD: ") {
		return fmt.Errorf("invalid client greeting: got %q", clientGreeting)
	}
	io.WriteString(conn, terminationCommand)

	// read requested module(s), if any
	requestedModule, err := rd.ReadString('\n')
	if err != nil {
		return err
	}
	requestedModule = strings.TrimSpace(requestedModule)
	log.Printf("client sent: %q", requestedModule)
	if requestedModule == "" || requestedModule == "#list" {
		// send available modules
	}
	// TODO: check if requested module exists
	io.WriteString(conn, terminationCommand)

	// read requested flags
	for {
		flag, err := rd.ReadString('\n')
		if err != nil {
			return err
		}
		flag = strings.TrimSpace(flag)
		log.Printf("client sent: %q", flag)
		if flag == "." {
			break
		}
	}

	// TODO: switch to binary protocol

	return fmt.Errorf("NYI")
}

func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			if err := s.handleConn(conn); err != nil {
				log.Printf("[%s] handle: %v", conn.RemoteAddr(), err)
			}
		}()
	}
}
