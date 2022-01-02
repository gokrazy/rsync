package receivermaincmd

import "github.com/DavidGamba/go-getoptions"

type Opts struct {
	Gokrazy struct {
		Listen           string
		MonitoringListen string
		AnonSSHListen    string
		ModuleMap        string
	}

	Archive           bool
	Update            bool
	PreserveHardlinks bool

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
	ShellCommand     string
}

func NewGetOpt() (*Opts, *getoptions.GetOpt) {
	var opts Opts
	// rsync itself uses /usr/include/popt.h for option parsing
	opt := getoptions.New()

	// rsync (but not openrsync) bundles short options together, i.e. it sends
	// e.g. -logDtpr
	opt.SetMode(getoptions.Bundling)

	opt.Bool("help", false, opt.Alias("h"))

	// // gokr-rsyncd flags
	// opt.StringVar(&opts.Gokrazy.Listen, "gokr.listen", "", opt.Description("[host]:port listen address for the rsync daemon protocol"))
	// opt.StringVar(&opts.Gokrazy.MonitoringListen, "gokr.monitoring_listen", "", opt.Description("optional [host]:port listen address for a HTTP debug interface"))
	// opt.StringVar(&opts.Gokrazy.AnonSSHListen, "gokr.anonssh_listen", "", opt.Description("optional [host]:port listen address for the rsync daemon protocol via anonymous SSH"))
	// opt.StringVar(&opts.Gokrazy.ModuleMap, "gokr.modulemap", "nonex=/nonexistant/path", opt.Description("<modulename>=<path> pairs for quick setup of the server, without a config file"))

	// rsync-compatible flags
	opt.BoolVar(&opts.Archive, "archive", false, opt.Alias("a"))
	opt.BoolVar(&opts.Update, "update", false, opt.Alias("u"))
	opt.BoolVar(&opts.PreserveHardlinks, "hard-links", false, opt.Alias("H"))

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

	opt.StringVar(&opts.ShellCommand, "rsh", "", opt.Alias("e"))

	return &opts, opt
}

// rsync/options.c:server_options
func serverOptions(clientOptions *Opts) []string {
	var sargv []string

	// if (blocking_io == -1)
	// 	blocking_io = 0;

	sargv = append(sargv, "--server")

	// if (daemon_over_rsh) {
	// 	args[ac++] = "--daemon";
	// 	*argc = ac;
	// 	/* if we're passing --daemon, we're done */
	// 	return;
	// }

	sargv = append(sargv, "--sender")

	argstr := "-"
	// x = 1;
	// argstr[0] = '-';
	// for (i = 0; i < verbose; i++)
	// 	argstr[x++] = 'v';

	// /* the -q option is intentionally left out */
	// if (make_backups)
	// 	argstr[x++] = 'b';
	if clientOptions.Update {
		argstr += "u"
	}
	if clientOptions.DryRun {
		argstr += "n"
	}
	if clientOptions.PreserveLinks {
		argstr += "l"
	}
	// if (copy_links)
	// 	argstr[x++] = 'L';

	// if (whole_file > 0)
	// 	argstr[x++] = 'W';
	// /* We don't need to send --no-whole-file, because it's the
	//  * default for remote transfers, and in any case old versions
	//  * of rsync will not understand it. */

	// if (preserve_hard_links)
	// 	argstr[x++] = 'H';
	if clientOptions.PreserveUid {
		argstr += "o"
	}
	if clientOptions.PreserveGid {
		argstr += "g"
	}
	if clientOptions.PreserveDevices {
		argstr += "D"
	}
	if clientOptions.PreserveTimes {
		argstr += "t"
	}
	if clientOptions.PreservePerms {
		argstr += "p"
	}
	if clientOptions.Recurse {
		argstr += "r"
	}
	// if (always_checksum)
	// 	argstr[x++] = 'c';
	// if (cvs_exclude)
	// 	argstr[x++] = 'C';
	// if (ignore_times)
	// 	argstr[x++] = 'I';
	// if (relative_paths)
	// 	argstr[x++] = 'R';
	// if (one_file_system)
	// 	argstr[x++] = 'x';
	// if (sparse_files)
	// 	argstr[x++] = 'S';
	// if (do_compression)
	// 	argstr[x++] = 'z';

	// /* this is a complete hack - blame Rusty

	//    this is a hack to make the list_only (remote file list)
	//    more useful */
	// if (list_only && !recurse)
	// 	argstr[x++] = 'r';

	// argstr[x] = 0;

	if argstr != "-" {
		sargv = append(sargv, argstr)
	}

	// if (block_size) {
	// 	if (asprintf(&arg, "-B%u", block_size) < 0)
	// 		goto oom;
	// 	args[ac++] = arg;
	// }

	// if (max_delete && am_sender) {
	// 	if (asprintf(&arg, "--max-delete=%d", max_delete) < 0)
	// 		goto oom;
	// 	args[ac++] = arg;
	// }

	// if (batch_prefix) {
	// 	char *r_or_w = write_batch ? "write" : "read";
	// 	if (asprintf(&arg, "--%s-batch=%s", r_or_w, batch_prefix) < 0)
	// 		goto oom;
	// 	args[ac++] = arg;
	// }

	// if (io_timeout) {
	// 	if (asprintf(&arg, "--timeout=%d", io_timeout) < 0)
	// 		goto oom;
	// 	args[ac++] = arg;
	// }

	// if (bwlimit) {
	// 	if (asprintf(&arg, "--bwlimit=%d", bwlimit) < 0)
	// 		goto oom;
	// 	args[ac++] = arg;
	// }

	// if (backup_dir) {
	// 	args[ac++] = "--backup-dir";
	// 	args[ac++] = backup_dir;
	// }

	// /* Only send --suffix if it specifies a non-default value. */
	// if (strcmp(backup_suffix, backup_dir ? "" : BACKUP_SUFFIX) != 0) {
	// 	/* We use the following syntax to avoid weirdness with '~'. */
	// 	if (asprintf(&arg, "--suffix=%s", backup_suffix) < 0)
	// 		goto oom;
	// 	args[ac++] = arg;
	// }

	// if (delete_excluded)
	// 	args[ac++] = "--delete-excluded";
	// else if (delete_mode)
	// 	args[ac++] = "--delete";

	// if (size_only)
	// 	args[ac++] = "--size-only";

	// if (modify_window_set) {
	// 	if (asprintf(&arg, "--modify-window=%d", modify_window) < 0)
	// 		goto oom;
	// 	args[ac++] = arg;
	// }

	// if (keep_partial)
	// 	args[ac++] = "--partial";

	// if (force_delete)
	// 	args[ac++] = "--force";

	// if (delete_after)
	// 	args[ac++] = "--delete-after";

	// if (ignore_errors)
	// 	args[ac++] = "--ignore-errors";

	// if (copy_unsafe_links)
	// 	args[ac++] = "--copy-unsafe-links";

	// if (safe_symlinks)
	// 	args[ac++] = "--safe-links";

	// if (numeric_ids)
	// 	args[ac++] = "--numeric-ids";

	// if (only_existing && am_sender)
	// 	args[ac++] = "--existing";

	// if (opt_ignore_existing && am_sender)
	// 	args[ac++] = "--ignore-existing";

	// if (tmpdir) {
	// 	args[ac++] = "--temp-dir";
	// 	args[ac++] = tmpdir;
	// }

	// if (compare_dest && am_sender) {
	// 	/* the server only needs this option if it is not the sender,
	// 	 *   and it may be an older version that doesn't know this
	// 	 *   option, so don't send it if client is the sender.
	// 	 */
	// 	args[ac++] = link_dest ? "--link-dest" : "--compare-dest";
	// 	args[ac++] = compare_dest;
	// }

	// if (files_from && (!am_sender || remote_filesfrom_file)) {
	// 	if (remote_filesfrom_file) {
	// 		args[ac++] = "--files-from";
	// 		args[ac++] = remote_filesfrom_file;
	// 		if (eol_nulls)
	// 			args[ac++] = "--from0";
	// 	} else {
	// 		args[ac++] = "--files-from=-";
	// 		args[ac++] = "--from0";
	// 	}
	// }

	return sargv
}
