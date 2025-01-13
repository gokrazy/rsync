// Package maincmd implements a subset of the '$ rsync' CLI surface, namely that it can:
//   - serve as a server daemon over TCP or SSH (via SSH session stdin/stdout)
//   - act as "client" CLI for connecting to the server
//   - Not yet implemented: both "client" and "server" can act as the sender and the receiver
package maincmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/gokrazy/rsync/internal/anonssh"
	"github.com/gokrazy/rsync/internal/log"
	"github.com/gokrazy/rsync/internal/rsyncdconfig"
	"github.com/gokrazy/rsync/rsyncd"

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

func Main(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer, cfg *rsyncdconfig.Config) error {
	opts, opt := rsyncd.NewGetOpt()
	remaining, err := opt.Parse(args[1:])
	if opt.Called("help") {
		fmt.Fprint(stderr, opt.Help())
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
			cfg, _, err = rsyncdconfig.FromDefaultFiles()
			if err != nil {
				return err
			}
		}
		srv, err := rsyncd.NewServer(cfg.Modules)
		if err != nil {
			return err
		}
		rw := readWriter{
			r: stdin,
			w: stdout,
		}
		return srv.HandleDaemonConn(ctx, &rw, nil)
	}

	// calling convention: command mode (over remote shell or locally)
	// Example: --server --sender -vvvvlogDtpre.iLsfxCIvu . .
	if opts.Server {
		// start_server()
		srv, err := rsyncd.NewServer(nil)
		if err != nil {
			return err
		}
		mod := rsyncd.Module{
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

	var cfgfn string
	var cfgErr error
	if cfg == nil {
		if opts.Gokrazy.Config != "" {
			cfgfn = opts.Gokrazy.Config
			cfg, cfgErr = rsyncdconfig.FromFile(cfgfn)
		} else {
			cfg, cfgfn, cfgErr = rsyncdconfig.FromDefaultFiles()
		}
		if cfgErr != nil {
			if os.IsNotExist(cfgErr) {
				log.Printf("config file not found, relying on flags")
				// a non-existant config file is not an error: users can start
				// gokr-rsyncd with e.g. the -gokr.listen and -gokr.modulemap flags.
				cfg = &rsyncdconfig.Config{
					Listeners: []rsyncdconfig.Listener{
						{
							Rsyncd:  opts.Gokrazy.Listen,
							AnonSSH: opts.Gokrazy.AnonSSHListen,
						},
					},
					Modules: []rsyncd.Module{},
				}
			} else {
				return cfgErr
			}
		} else {
			log.Printf("config file %s loaded", cfgfn)
		}
	}

	if os.IsNotExist(cfgErr) {
		if opts.Gokrazy.Listen == "" &&
			opts.Gokrazy.AnonSSHListen == "" {
			return fmt.Errorf("neither -gokr.listen nor -gokr.anonssh_listen specified, and config file not found: %v", cfgErr)
		}
		// If no config file was found, and the user did not specify a
		// -gokr.modulemap flag, use a default value to force the user to
		// configure a module map.
		if opts.Gokrazy.ModuleMap == "" {
			opts.Gokrazy.ModuleMap = "nonex=/nonexistant/path"
		}
	} else {
		if len(cfg.Listeners) == 0 ||
			(cfg.Listeners[0].Rsyncd == "" &&
				cfg.Listeners[0].AnonSSH == "" &&
				cfg.Listeners[0].AuthorizedSSH.Address == "") {
			return fmt.Errorf("no rsyncd listeners configured, add a [[listener]] to %s", cfgfn)
		}
	}
	// TODO: loosen this restriction, create multiple listeners

	if len(cfg.Listeners) != 1 ||
		(cfg.Listeners[0].Rsyncd == "" &&
			cfg.Listeners[0].AnonSSH == "" &&
			cfg.Listeners[0].AuthorizedSSH.Address == "") {
		return fmt.Errorf("not precisely 1 rsyncd listener specified")
	}

	var sshListener *anonssh.Listener
	listenAddr := cfg.Listeners[0].Rsyncd
	if listenAddr == "" {
		listenAddr = cfg.Listeners[0].AnonSSH
		if listenAddr == "" {
			listenAddr = cfg.Listeners[0].AuthorizedSSH.Address
			var err error
			sshListener, err = anonssh.ListenerFromConfig(cfg.Listeners[0])
			if err != nil {
				return err
			}
		} else {
			var err error
			sshListener, err = anonssh.ListenerFromConfig(cfg.Listeners[0])
			if err != nil {
				return err
			}
		}
	}

	if moduleMap := opts.Gokrazy.ModuleMap; moduleMap != "" {
		parts := strings.Split(moduleMap, "=")
		if len(parts) != 2 {
			return fmt.Errorf("malformed -gokr.modulemap parameter %q, expected <modulename>=<path>", moduleMap)
		}
		module := rsyncd.Module{
			Name: parts[0],
			Path: parts[1],
		}
		cfg.Modules = append(cfg.Modules, module)
	}
	if cfg.DontNamespace {
		if cfg.Listeners[0].Rsyncd != "" ||
			cfg.Listeners[0].AnonSSH != "" {
			return fmt.Errorf("dont_namespace must be used with authorized_ssh listeners only")
		}
		version()
		log.Printf("environment: not namespace due to dont_namespace option")
	} else {
		if err := namespace(cfg.Modules, listenAddr); err == errIsParent {
			return nil
		} else if err != nil {
			return fmt.Errorf("namespace: %v", err)
		}
	}
	log.Printf("%d rsync modules configured in total", len(cfg.Modules))
	for _, mod := range cfg.Modules {
		if !cfg.DontNamespace {
			if err := canUnexpectedlyWriteTo(mod.Path); err != nil {
				return err
			}
		}

		log.Printf("rsync module %q with path %s configured", mod.Name, mod.Path)
	}

	if monitoringListen := opts.Gokrazy.MonitoringListen; monitoringListen != "" {
		go func() {
			log.Printf("HTTP server for monitoring listening on http://%s/debug/pprof", monitoringListen)
			if err := http.ListenAndServe(monitoringListen, nil); err != nil {
				log.Printf("-monitoring_listen: %v", err)
			}
		}()
	}

	srv, err := rsyncd.NewServer(cfg.Modules)
	if err != nil {
		return err
	}
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

	if cfg.Listeners[0].AuthorizedSSH.Address != "" {
		if cfg.Listeners[0].AuthorizedSSH.AuthorizedKeys == "" {
			return fmt.Errorf("misconfiguration: authorized_keys must not be empty when using an authorized_ssh listener")
		}
		log.Printf("rsync daemon listening (authorized SSH) on %s", ln.Addr())
		return anonssh.Serve(ln, sshListener, cfg, func(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
			return Main(ctx, args, stdin, stdout, stderr, cfg)
		})
	}

	if cfg.Listeners[0].AnonSSH != "" {
		log.Printf("rsync daemon listening (anon SSH) on %s", ln.Addr())
		return anonssh.Serve(ln, sshListener, cfg, func(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
			return Main(ctx, args, stdin, stdout, stderr, cfg)
		})
	}

	log.Printf("rsync daemon listening on rsync://%s", ln.Addr())
	return srv.Serve(ctx, ln)
}
