package main

import (
	"flag"
	"log"
	"net"
	"net/http"

	"github.com/stapelberg/go-rsyncd-server/internal/rsyncd"

	// For profiling and debugging
	_ "net/http/pprof"
)

func rsyncdMain() error {
	monitoringListen := flag.String("monitoring_listen",
		"",
		"optional [host]:port listen address for a HTTP debug interface")
	flag.Parse()
	if *monitoringListen != "" {
		go func() {
			log.Printf("HTTP server for monitoring listening on http://%s/debug/pprof", *monitoringListen)
			if err := http.ListenAndServe(*monitoringListen, nil); err != nil {
				log.Printf("-monitoring_listen: %v", err)
			}
		}()
	}
	srv := &rsyncd.Server{}
	ln, err := net.Listen("tcp", "localhost:8730")
	if err != nil {
		return err
	}
	log.Printf("rsync daemon listening on rsync://%s", ln.Addr())
	return srv.Serve(ln)
}

func main() {
	if err := rsyncdMain(); err != nil {
		log.Fatal(err)
	}
}
