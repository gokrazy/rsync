// Tool gokr-rsyncd is a read-only rsync daemon sender-only Go implementation of
// rsyncd. rsync daemon is a custom (un-standardized) network protocol, running
// on port 873 by default.
//
// For the corresponding way of operation in the original “tridge” rsync
// (https://github.com/WayneD/rsync), see
// https://manpages.debian.org/bullseye/rsync/rsync.1.en.html#DAEMON_OPTIONS
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/gokrazy/rsync/internal/rsyncd"

	// For profiling and debugging
	_ "net/http/pprof"
)

func rsyncdMain() error {
	listen := flag.String("listen",
		"localhost:8730",
		"[host]:port listen address for the rsync daemon protocol")

	monitoringListen := flag.String("monitoring_listen",
		"",
		"optional [host]:port listen address for a HTTP debug interface")

	moduleMap := flag.String("modulemap",
		"nonex=/nonexistant/path",
		"<modulename>=<path> pairs for quick setup of the server, without a config file")

	flag.Parse()
	if *monitoringListen != "" {
		go func() {
			log.Printf("HTTP server for monitoring listening on http://%s/debug/pprof", *monitoringListen)
			if err := http.ListenAndServe(*monitoringListen, nil); err != nil {
				log.Printf("-monitoring_listen: %v", err)
			}
		}()
	}
	var modMap map[string]rsyncd.Module
	if *moduleMap != "" {
		parts := strings.Split(*moduleMap, "=")
		if len(parts) != 2 {
			return fmt.Errorf("malformed -modulemap parameter %q, expected <modulename>=<path>", *moduleMap)
		}
		modMap = map[string]rsyncd.Module{
			parts[0]: rsyncd.Module{
				Path: parts[1],
			},
		}
		log.Printf("rsync module %q with path %s configured", parts[0], parts[1])
	}
	srv := &rsyncd.Server{Modules: modMap}
	ln, err := net.Listen("tcp", *listen)
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
