package rsyncopts

func (o *Options) CommandOptions(path string, paths ...string) []string {
	return append(o.ServerOptions(), append([]string{".", path}, paths...)...)
}

// rsync/options.c:server_options
func (o *Options) ServerOptions() []string {
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

	if !o.Sender() {
		sargv = append(sargv, "--sender")
	}

	argstr := "-"
	// x = 1;
	// argstr[0] = '-';
	// for (i = 0; i < verbose; i++)
	// 	argstr[x++] = 'v';

	// /* the -q option is intentionally left out */
	// if (make_backups)
	// 	argstr[x++] = 'b';
	if o.UpdateOnly() {
		argstr += "u"
	}
	if o.DryRun() {
		argstr += "n"
	}
	if o.PreserveLinks() {
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
	if o.PreserveUid() {
		argstr += "o"
	}
	if o.PreserveGid() {
		argstr += "g"
	}
	if o.PreserveDevices() {
		argstr += "D"
	}
	if o.PreserveMTimes() {
		argstr += "t"
	}
	if o.PreservePerms() {
		argstr += "p"
	}
	if o.Recurse() {
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
