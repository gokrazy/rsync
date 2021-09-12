package maincmd

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/coreos/go-systemd/activation"
	"github.com/gokrazy/rsync/internal/config"
	"github.com/gokrazy/rsync/internal/rsyncd"

	// For profiling and debugging
	_ "net/http/pprof"
)

func version() {
	log.Printf("gokrazy rsync, pid %d", os.Getpid())
}

type readWriter struct {
	r io.Reader
	w io.Writer
}

func (r *readWriter) Read(p []byte) (n int, err error)  { return r.r.Read(p) }
func (r *readWriter) Write(p []byte) (n int, err error) { return r.w.Write(p) }

func Main() error {
	opts, opt := rsyncd.NewGetOpt()
	remaining, err := opt.Parse(os.Args[1:])
	if opt.Called("help") {
		fmt.Fprintf(os.Stderr, opt.Help())
		os.Exit(1)
	}
	if err != nil {
		return err
	}
	// log.Printf("remaining: %v", remaining)

	// calling convention: daemon mode over remote shell
	// Example: --server --daemon .
	if opts.Daemon && opts.Server {
		// start_daemon()
		cfg, _, err := config.FromDefaultFiles()
		if err != nil {
			return err
		}
		srv := &rsyncd.Server{
			Modules: cfg.ModuleMap,
		}
		rw := readWriter{
			r: os.Stdin,
			w: os.Stdout,
		}
		return srv.HandleDaemonConn(&rw)
	}

	// calling convention: command mode (over remote shell or locally)
	// Example: --server --sender -vvvvlogDtpre.iLsfxCIvu . .
	if opts.Server {
		// start_server()
		srv := &rsyncd.Server{}
		mod := config.Module{
			Name: "implicit",
			Path: "/",
		}

		// TODO: remove duplication with handleDaemonConn
		if len(remaining) < 2 {
			return fmt.Errorf("invalid args: at least one directory required")
		}
		if got, want := remaining[0], "."; got != want {
			return fmt.Errorf("protocol error: got %q, expected %q", got, want)
		}
		paths := remaining[1:]

		crd, cwr := rsyncd.CounterPair(os.Stdin, os.Stdout)
		rd := crd
		return srv.HandleConn(mod, rd, crd, cwr, paths, opts, true)
	}

	if !opts.Daemon {
		return fmt.Errorf("NYI: client mode")
	}

	// daemon_main()

	// calling convention: start a daemon in TCP listening mode (or with systemd
	// socket activation)

	cfg, cfgfn, err := config.FromDefaultFiles()
	if err != nil {
		if os.IsNotExist(err) {
			// a non-existant config file is not an error: users can start
			// gokr-rsyncd with e.g. the -gokr.listen and -gokr.modulemap flags.
			cfg = &config.Config{
				Listeners: []config.Listener{
					{Rsyncd: opts.Gokrazy.Listen},
				},
				ModuleMap: make(map[string]config.Module),
			}
		} else {
			return err
		}
	} else {
		log.Printf("config file %s loaded", cfgfn)
	}

	if os.IsNotExist(err) && opts.Gokrazy.Listen == "" {
		return fmt.Errorf("-gokr.listen not specified, and config file not found: %v", err)
	}

	// TODO: loosen this restriction, create multiple listeners
	if len(cfg.Listeners) != 1 || cfg.Listeners[0].Rsyncd == "" {
		return fmt.Errorf("not precisely 1 rsyncd listener specified")
	}

	if moduleMap := opts.Gokrazy.ModuleMap; moduleMap != "" {
		parts := strings.Split(moduleMap, "=")
		if len(parts) != 2 {
			return fmt.Errorf("malformed -gokr.modulemap parameter %q, expected <modulename>=<path>", moduleMap)
		}
		cfg.ModuleMap[parts[0]] = config.Module{
			Name: parts[0],
			Path: parts[1],
		}
	}
	if err := namespace(cfg.ModuleMap, cfg.Listeners[0].Rsyncd); err == errIsParent {
		return nil
	} else if err != nil {
		return fmt.Errorf("namespace: %v", err)
	}
	for name, mod := range cfg.ModuleMap {
		if err := canUnexpectedlyWriteTo(mod.Path); err != nil {
			return err
		}

		log.Printf("rsync module %q with path %s configured", name, mod.Path)
	}

	if monitoringListen := opts.Gokrazy.MonitoringListen; monitoringListen != "" {
		go func() {
			log.Printf("HTTP server for monitoring listening on http://%s/debug/pprof", monitoringListen)
			if err := http.ListenAndServe(monitoringListen, nil); err != nil {
				log.Printf("-monitoring_listen: %v", err)
			}
		}()
	}

	srv := &rsyncd.Server{Modules: cfg.ModuleMap}
	var ln net.Listener
	if listeners, err := activation.Listeners(); err == nil && len(listeners) > 0 {
		if got, want := len(listeners), 1; got != want {
			return fmt.Errorf("unexpected number of sockets received from systemd: got %d, want %d", got, want)
		}
		ln = listeners[0]
	} else if err != nil || len(listeners) == 0 {
		log.Printf("could not obtain listeners from systemd, creating listener")
		ln, err = net.Listen("tcp", cfg.Listeners[0].Rsyncd)
		if err != nil {
			return err
		}
	}
	log.Printf("rsync daemon listening on rsync://%s", ln.Addr())
	return srv.Serve(ln)
}
