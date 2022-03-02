package maincmd

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/gokrazy/rsync/internal/anonssh"
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

func Main(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer, cfg *config.Config) error {
	opts, opt := rsyncd.NewGetOpt()
	remaining, err := opt.Parse(args[1:])
	if opt.Called("help") {
		fmt.Fprintf(stderr, opt.Help())
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
		if cfg == nil {
			var err error
			cfg, _, err = config.FromDefaultFiles()
			if err != nil {
				return err
			}
		}
		srv := &rsyncd.Server{
			Modules: cfg.ModuleMap,
		}
		rw := readWriter{
			r: stdin,
			w: stdout,
		}
		return srv.HandleDaemonConn(&rw, nil)
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

		// TODO: copy seed+multiplex error handling from handleDaemonConn

		// TODO: remove duplication with handleDaemonConn
		if len(remaining) < 2 {
			return fmt.Errorf("invalid args: at least one directory required")
		}
		if got, want := remaining[0], "."; got != want {
			return fmt.Errorf("protocol error: got %q, expected %q", got, want)
		}
		paths := remaining[1:]

		crd, cwr := rsyncd.CounterPair(stdin, stdout)
		rd := crd
		return srv.HandleConn(mod, rd, crd, cwr, paths, opts, true)
	}

	if !opts.Daemon {
		return fmt.Errorf("not implemented yet: client mode")
	}

	// daemon_main()

	// calling convention: start a daemon in TCP listening mode (or with systemd
	// socket activation)

	if cfg == nil {
		var cfgfn string
		var err error
		cfg, cfgfn, err = config.FromDefaultFiles()
		if err != nil {
			if os.IsNotExist(err) {
				// a non-existant config file is not an error: users can start
				// gokr-rsyncd with e.g. the -gokr.listen and -gokr.modulemap flags.
				cfg = &config.Config{
					Listeners: []config.Listener{
						{
							Rsyncd:  opts.Gokrazy.Listen,
							AnonSSH: opts.Gokrazy.AnonSSHListen,
						},
					},
					ModuleMap: make(map[string]config.Module),
				}
			} else {
				return err
			}
		} else {
			log.Printf("config file %s loaded", cfgfn)
		}
	}

	if os.IsNotExist(err) &&
		opts.Gokrazy.Listen == "" &&
		opts.Gokrazy.AnonSSHListen == "" {
		return fmt.Errorf("neither -gokr.listen nor -gokr.anonssh_listen specified, and config file not found: %v", err)
	}

	// TODO: loosen this restriction, create multiple listeners
	if len(cfg.Listeners) != 1 ||
		(cfg.Listeners[0].Rsyncd == "" &&
			cfg.Listeners[0].AnonSSH == "") {
		return fmt.Errorf("not precisely 1 rsyncd listener specified")
	}

	listenAddr := cfg.Listeners[0].Rsyncd
	if listenAddr == "" {
		listenAddr = cfg.Listeners[0].AnonSSH
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
	if err := namespace(cfg.ModuleMap, listenAddr); err == errIsParent {
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
	listeners, err := systemdListeners()
	if err != nil {
		return err
	}
	if len(listeners) > 0 {
		ln = listeners[0]
	} else {
		log.Printf("not using systemd socket activation, creating listener")
		ln, err = net.Listen("tcp", listenAddr)
		if err != nil {
			return err
		}
	}

	if cfg.Listeners[0].AnonSSH != "" {
		log.Printf("rsync daemon listening (anon SSH) on %s", ln.Addr())
		return anonssh.Serve(ln, cfg, func(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
			return Main(args, stdin, stdout, stderr, cfg)
		})
	}

	log.Printf("rsync daemon listening on rsync://%s", ln.Addr())
	return srv.Serve(ln)
}
