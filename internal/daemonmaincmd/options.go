package maincmd

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/gokrazy/rsync/internal/rsyncopts"
)

// rsync/options.c:server_options
func serverOptions(clientOptions *rsyncopts.Options) []string {
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

	if !clientOptions.Sender() {
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
	if clientOptions.UpdateOnly() {
		argstr += "u"
	}
	if clientOptions.DryRun() {
		argstr += "n"
	}
	if clientOptions.PreserveLinks() {
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
	if clientOptions.PreserveUid() {
		argstr += "o"
	}
	if clientOptions.PreserveGid() {
		argstr += "g"
	}
	if clientOptions.PreserveDevices() {
		argstr += "D"
	}
	if clientOptions.PreserveMTimes() {
		argstr += "t"
	}
	if clientOptions.PreservePerms() {
		argstr += "p"
	}
	if clientOptions.Recurse() {
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

// parseHostspec returns the [USER@]HOST part of the string
//
// rsync/options.c:parse_hostspec
func parseHostspec(src string, parsingURL bool) (host, path string, port int, _ error) {
	var userlen int
	var hostlen int
	var hoststart int
	i := 0
	for ; i <= len(src); i++ {
		if i == len(src) {
			if !parsingURL {
				return "", "", 0, fmt.Errorf("ran out of string")
			}
			if hostlen == 0 {
				hostlen = len(src[hoststart:])
			}
			break
		}

		s := src[i]
		if s == ':' || s == '/' {
			if hostlen == 0 {
				hostlen = len(src[hoststart:i])
			}
			i++
			if s == '/' {
				if !parsingURL {
					return "", "", 0, fmt.Errorf("/, but not parsing URL")
				}
			} else if s == ':' && parsingURL {
				rest := src[i:]
				digits := ""
				for _, s := range rest {
					if !unicode.IsDigit(s) {
						break
					}
					digits += string(s)
				}
				if digits != "" {
					p, err := strconv.ParseInt(digits, 0, 64)
					if err != nil {
						return "", "", port, err
					}
					port = int(p)
					i += len(digits)
				}
				if i < len(src) && src[i] != '/' {
					return "", "", 0, fmt.Errorf("expected / or end, got %q", src[i:])
				}
				if i < len(src) {
					i++
				}
			}
			break
		}
		if s == '@' {
			userlen = i + 1
			hoststart = i + 1
		} else if s == '[' {
			if i != hoststart {
				return "", "", 0, fmt.Errorf("brackets not at host position")
			}
			hoststart++
			for i < len(src) && src[i] != ']' && src[i] != '/' {
				i++
			}
			hostlen = len(src[hoststart : i+1])
			if i == len(src) ||
				src[i] != ']' ||
				(i < len(src)-1 && src[i+1] != '/' && src[i+1] != ':') ||
				hostlen == 0 {
				return "", "", 0, fmt.Errorf("WTF")
			}
		}
	}
	if userlen > 0 {
		host = src[:userlen]
		hostlen += userlen
	}
	host += src[hoststart:hostlen]
	return host, src[i:], port, nil
}

// rsync/options.c:check_for_hostspec
func checkForHostspec(src string) (host, path string, port int, _ error) {
	if strings.HasPrefix(src, "rsync://") {
		var err error
		if host, path, port, err = parseHostspec(strings.TrimPrefix(src, "rsync://"), true); err == nil {
			if port == 0 {
				port = -1
			}
			return host, path, port, nil
		}
	}
	var err error
	host, path, port, err = parseHostspec(src, false)
	if err != nil {
		return host, path, port, err
	}
	if strings.HasPrefix(path, ":") {
		if port == 0 {
			port = -1
		}
		path = strings.TrimPrefix(path, ":")
		return host, path, port, nil
	}
	port = 0 // not a daemon-accessing spec
	return host, path, port, nil
}
