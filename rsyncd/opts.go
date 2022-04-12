package rsyncd

import "github.com/DavidGamba/go-getoptions"

type Opts struct {
	Gokrazy struct {
		Config           string
		Listen           string
		MonitoringListen string
		AnonSSHListen    string
		ModuleMap        string
	}

	Daemon           bool
	Server           bool
	Sender           bool
	PreserveGid      bool
	PreserveUid      bool
	PreserveLinks    bool
	PreservePerms    bool
	PreserveDevices  bool
	PreserveSpecials bool
	PreserveTimes    bool
	Recurse          bool
	IgnoreTimes      bool
	DryRun           bool
	D                bool
}

func NewGetOpt() (*Opts, *getoptions.GetOpt) {
	var opts Opts
	// rsync itself uses /usr/include/popt.h for option parsing
	opt := getoptions.New()

	// rsync (but not openrsync) bundles short options together, i.e. it sends
	// e.g. -logDtpr
	opt.SetMode(getoptions.Bundling)

	opt.Bool("help", false, opt.Alias("h"))

	// gokr-rsyncd flags
	opt.StringVar(&opts.Gokrazy.Config, "gokr.config", "", opt.Description("path to a config file (if unspecified, os.UserConfigDir()/gokr-rsyncd.toml is used)"))
	opt.StringVar(&opts.Gokrazy.Listen, "gokr.listen", "", opt.Description("[host]:port listen address for the rsync daemon protocol"))
	opt.StringVar(&opts.Gokrazy.MonitoringListen, "gokr.monitoring_listen", "", opt.Description("optional [host]:port listen address for a HTTP debug interface"))
	opt.StringVar(&opts.Gokrazy.AnonSSHListen, "gokr.anonssh_listen", "", opt.Description("optional [host]:port listen address for the rsync daemon protocol via anonymous SSH"))
	opt.StringVar(&opts.Gokrazy.ModuleMap, "gokr.modulemap", "", opt.Description("<modulename>=<path> pairs for quick setup of the server, without a config file"))

	// rsync-compatible flags
	opt.BoolVar(&opts.Daemon, "daemon", false, opt.Description("run as an rsync daemon"))
	opt.BoolVar(&opts.Server, "server", false)
	opt.BoolVar(&opts.Sender, "sender", false)
	opt.BoolVar(&opts.PreserveGid, "group", false, opt.Alias("g"))
	opt.BoolVar(&opts.PreserveUid, "owner", false, opt.Alias("o"))
	opt.BoolVar(&opts.PreserveLinks, "links", false, opt.Alias("l"))
	// TODO: implement PreservePerms
	opt.BoolVar(&opts.PreservePerms, "perms", false, opt.Alias("p"))
	opt.BoolVar(&opts.D, "D", false)
	opt.BoolVar(&opts.Recurse, "recursive", false, opt.Alias("r"))
	// TODO: implement PreserveTimes
	opt.BoolVar(&opts.PreserveTimes, "times", false, opt.Alias("t"))
	opt.Bool("v", false)     // verbosity; ignored
	opt.Bool("debug", false) // debug; ignored
	// TODO: implement IgnoreTimes
	opt.BoolVar(&opts.IgnoreTimes, "ignore-times", false, opt.Alias("I"))
	opt.BoolVar(&opts.DryRun, "dry-run", false, opt.Alias("n"))

	return &opts, opt
}
