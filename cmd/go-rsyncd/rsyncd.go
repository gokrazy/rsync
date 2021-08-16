package main

import (
	"flag"
	"log"
	"net"

	"github.com/stapelberg/go-rsyncd-server/internal/rsyncd"
)

func rsyncdMain() error {
	flag.Parse()
	srv := &rsyncd.Server{}
	ln, err := net.Listen("tcp", "localhost:8730")
	if err != nil {
		return err
	}
	log.Printf("listening on %s", ln.Addr())
	return srv.Serve(ln)
}

func main() {
	if err := rsyncdMain(); err != nil {
		log.Fatal(err)
	}
}
