# gokrazy rsync

[![tests](https://github.com/gokrazy/rsync/actions/workflows/main.yml/badge.svg)](https://github.com/gokrazy/rsync/actions/workflows/main.yml)
[![Sourcegraph](https://sourcegraph.com/github.com/gokrazy/rsync/-/badge.svg)](https://sourcegraph.com/github.com/gokrazy/rsync??badge)

This repository contains a native Go rsync implementation: the `gokr-rsync`
command implements an rsync client and server, which can send or receive files
(all directions supported). Daemon mode is supported, meaning you can deploy
`gokr-rsync` behind SSH (anonymous or authorized), as command or daemon, or
listening directly on the network (on port 873/tcp by default).

This project accepts contributions as time permits to merge them (best effort).

## How do I know this project won’t eat my data?

This rsync implementation is not as well-tested as the original “tridge”
implementation from the Samba project. gokrazy/rsync was started in 2021 and
doesn’t have many users yet.

With that warning out of the way, the rsync protocol uses MD4 checksums over
file contents, so at least your file contents should never be able to be
corrupted.

There is enough other functionality (delta transfers, file metadata, special
files like symlinks or devices, directory structures, etc.) in the rsync
protocol that provides opportunities for bugs to hide.

I recommend you carefully check that your transfers work, and please do report
any issues you run into!

## Existing rsync implementation survey

| Language | URL                                                                                 | Note                                                                                                                                  | Max Protocol                                                                                                        | Server mode? |
|----------|-------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------|--------------|
| C        | [RsyncProject/rsync](https://github.com/RsyncProject/rsync) (formerly WayneD/rsync) | original “tridge” implementation; I found [older versions](https://github.com/WayneD/rsync/tree/v2.6.1pre2) easier to study           | [31](https://github.com/WayneD/rsync/blob/592c6bc3e5e93f36c2fdc0a491a9fb43a41cf688/rsync.h#L113)                    | ✔ yes        |
| C        | [kristapsdz/openrsync](https://github.com/kristapsdz/openrsync)                     | OpenBSD, good docs                                                                                                                    | [27](https://github.com/kristapsdz/openrsync/blob/e54d57f7572381da2b549d39c7968fc79dac8e1d/extern.h#L30)            | ✔ yes        |
| **Go**   | [gokrazy/rsync](https://github.com/gokrazy/rsync)                                   | → you are here ←                                                                                                                      | [27](https://github.com/gokrazy/rsync/blob/b3b58770b864613551036a2ef2827b74ace77749/internal/rsyncd/rsyncd.go#L317) | ✔ yes 🎉     |
| **Go**   | [jbreiding/rsync-go](https://github.com/jbreiding/rsync-go)                         | rsync algorithm                                                                                                                       |                                                                                                                     | ❌ no        |
| **Go**   | [kaiakz/rsync-os](https://github.com/kaiakz/rsync-os)                               | only client/receiver                                                                                                                  | [27](https://github.com/kaiakz/rsync-os/blob/64e84daeabb1fa4d2c7cf766c196306adfba6cb2/rsync/const.go#L4)            | ❌ no        |
| **Go**   | [knight42](https://gist.github.com/knight42/6ad35ce6fbf96519259b43a8c3f37478)       | proxy                                                                                                                                 |                                                                                                                     | ❌ no        |
| **Go**   | [c4milo/gsync](https://github.com/c4milo/gsync)                                     |                                                                                                                                       |                                                                                                                     | ❌ no        |
| Java     | [APNIC-net/repositoryd](https://github.com/APNIC-net/repositoryd)                   | archived                                                                                                                              |                                                                                                                     | ✔ yes        |
| Java     | [JohannesBuchner/Jarsync](https://github.com/JohannesBuchner/Jarsync/)              | archived, [internet draft RFC “The rsync Network Protocol”](https://github.com/JohannesBuchner/Jarsync/blob/master/jarsync/rsync.txt) |                                                                                                                     | ✔ yes        |
| Java     | [perlundq/yajsync](https://github.com/perlundq/yajsync#example)                     |                                                                                                                                       |                                                                                                                     | ✔ yes        |
| C++      | [gilbertchen/acrosync-library](https://github.com/gilbertchen/acrosync-library)     | commercial                                                                                                                            |                                                                                                                     | ❌ no        |
| Rust     | [sourcefrog/rsyn](https://github.com/sourcefrog/rsyn#why-do-this)                   | archived, client, “rsyn is rsync with no c”                                                                                                     | [27](https://github.com/sourcefrog/rsyn/blob/2ebbfcfe999fdf2d1a434d8614d07aa93873461b/src/connection.rs#L38)        | ❌ no        |

## Getting started

To serve the `/usr/share/man` directory via rsync on `localhost:8730`, use:

```
go install github.com/gokrazy/rsync/cmd/gokr-rsync@latest
gokr-rsync --daemon --gokr.listen=localhost:8730 --gokr.modulemap=man=/usr/share/man
```

You can then copy the contents of the current directory with clients such as
`rsync(1)`:

```
% rsync -v --archive --port 8730 rsync://localhost/man/ man
receiving file list ... done
created directory man
./
ar/
ar/man1/
[…]
zh_TW/man8/userdel.8.gz
zh_TW/man8/usermod.8.gz

sent 658.973 bytes  received 88.012.067 bytes  3.940.935,11 bytes/sec
total size is 84.504.170  speedup is 0,95
```

…or [`openrsync(1)`](https://github.com/kristapsdz/openrsync), shown doing a
differential update:

```
% openrsync -v --archive --port 8730 rsync://localhost/man/ man
openrsync: warning: connect refused: ::1, localhost
Transfer starting: 40202 files
[…]
zh_TW/man8/userdel.8.gz (732 B, 100.0% downloaded)
zh_TW/man8/usermod.8.gz (1.8 KB, 100.0% downloaded)
Transfer complete: 83.93 MB sent, 643.5 KB read, 80.59 MB file size
```

## Usage / Setup

 | setup                                   | encrypted | authenticated      | private files?         | privileges                                                      | protocol version | config required                       |
 |-----------------------------------------|-----------|--------------------|------------------------|-----------------------------------------------------------------|------------------|---------------------------------------|
 | 1. rsync daemon protocol (TCP port 873) | ❌ no     | ⚠ rsync (insecure) | ❌ only world-readable | ✔ dropped + [namespace](#privileged-linux-including-gokrazyorg) | ✔ negotiated     | config required                       |
 | 2. anon SSH (daemon)                    | ✔ yes     | ✔ rsync            | ❌ only world-readable | ✔ dropped + [namespace](#privileged-linux-including-gokrazyorg) | ✔ negotiated     | config required                       |
 | 3. SSH (command)                        | ✔ yes     | ✔ SSH              | ✔ yes                  | ⚠ full user                                                     | ⚠ assumed       | no config                             |
 | 4. SSH (daemon)                         | ✔ yes     | ✔ SSH (+ rsync)    | ✔ yes                  | ⚠ full user                                                     | ✔ negotiated     | `~/.config/gokr-rsyncd.toml` required |

Regarding protocol version “assumed”: the flags to send over the network are
computed *before* starting SSH and hence the remote rsync process. You might
need to specify `--protocol=27` explicitly on the client. Once the connection is
established, both sides *do* negotiate the protocol, though.

### Setup 1: rsync daemon protocol (TCP port 873)

Serving rsync daemon protocol on TCP port 873 is only safe where the network
layer ensures trusted communication, e.g. in a local network (LAN), or when
using [Tailscale](https://tailscale.com/) or similar. In untrusted networks,
attackers can eavesdrop on file transfers and possibly even modify file
contents.

Prefer setup 2 instead.

Example:
* Server: `gokr-rsync --daemon --gokr.modulemap=module=/srv/rsync-module`
* Client: `rsync rsync://webserver/module/path`

### Setup 2: anon SSH (daemon)

This setup is well suited for serving world-readable files without
authentication.

Example:
* Server: `gokr-rsync --daemon --gokr.modulemap=module=/srv/rsync-module --gokr.anonssh_listen=:22873`
* Client: `rsync -e ssh rsync://webserver/module/path`


### Setup 3: SSH (command)

This setup is well suited for interactive one-off transfers or regular backups,
and uses SSH for both encryption and authentication.

Note that because `gokr-rsync` is invoked with user privileges (not root
privileges), it cannot do [namespacing](#privileged-linux-including-gokrazyorg)
and hence retains more privileges. When serving public data, it is generally
preferable to use setup 2 instead.

Note that `rsync(1)` assumes the server process understands all flags that it
sends, i.e. is running the same version on client and server, or at least a
compatible-enough version. You can either specify `--protocol=27` on the client,
or use setup 4, which negotiates the protocol version, side-stepping possible
compatibility gaps between rsync clients and `gokr-rsync`.

Example:
* Server will be started via SSH
* Client: `rsync --rsync-path=gokr-rsync webserver:path`

### Setup 4: SSH (daemon)

This setup is more reliable than setup 3 because the rsync protocol version will
be negotiated between client and server. This setup is slightly inconvenient
because it requires a config file to be present on the server in
`~/.config/gokr-rsyncd.toml`.

Note that this mode of operation is only implemented by the original “trigde”
rsync, not in openrsync. Apple started shipping openrsync with macOS 15 Sequoia,
so you might need to explicitly start /usr/libexec/rsync/rsync.samba on Macs.

Example:
* Server will be started via SSH
* Client: `rsync -e ssh --rsync-path=gokr-rsync rsync://webserver/module/path`

## Limitations

### Bandwidth

In my tests, `gokr-rsync` can easily transfer data at > 6 Gbit/s. The current
bottleneck is the MD4 algorithm itself (not sure whether in the “tridge” rsync
client, or in `gokr-rsync`). Implementing support for more recent protocol
versions would help here, as these include hash algorithm negotiation with more
recent choices.

### Protocol related limitations

* xattrs (including acls) was introduced in rsync protocol 30, so is currently
  not supported.

## Supported environments and privilege dropping

Supported environments:

1. systemd (Linux)
1. Docker (Linux)
1. privileged Linux
1. privileged non-Linux

In all environments, the default instructions will take care that:

* (On Linux only) Only configured rsync modules from the host file system are
  mounted **read-only** into a Linux mount namespace for `gokr-rsync`, to guard
  against data modification and data exfiltration.
* (On Linux only) File system access is restricted using the
  [Landlock](https://docs.kernel.org/userspace-api/landlock.html) Linux kernel
  security module, which works similar to OpenBSD’s
  [`unveil(2)`](https://man.openbsd.org/unveil.2) API.
* `gokr-rsync` is running without privileges, as user `nobody`, to limit the
  scope of what an attacker can do when exploiting a vulnerability.

Known gaps:

* `gokr-rsync` does not guard against denial of service attacks, i.e. consuming
  too many resources (connections, bandwidth, CPU, …).
  * See also [Per-IP rate limiting with
    iptables](https://making.pusher.com/per-ip-rate-limiting-with-iptables/).


### systemd (unprivileged)

We provide [a `gokr-rsyncd.socket` and `gokr-rsyncd.service`
file](https://github.com/gokrazy/rsync/tree/main/systemd/) for systemd. These
files enables most of systemd’s security features. You can check by running
`systemd-analyze security gokr-rsyncd.service`, which should result in an
exposure level of “0.2 SAFE” as of systemd 249 (September 2021).

First, configure your server flags by creating a systemd service override file:

```shell
systemctl edit gokr-rsyncd.service
```

In the opened editor, change the file to:
```
[Service]
ExecStart=
ExecStart=/usr/bin/gokr-rsync --daemon --gokr.modulemap=pwd=/etc/tmpfiles.d
```

Close the editor and install the service using:

```shell
systemctl enable --now gokr-rsyncd.socket
```

Additional hardening recommendations:

* Restrict which IP addresses are allowed to connect to your rsync server, for example:
  * using iptables or nftables on your host system
  * using [`gokr-rsync`’s built-in IP allow/deny mechanism](https://github.com/gokrazy/rsync/issues/4) (once implemented)
  * using [systemd’s `IPAddressDeny` and `IPAddressAllow`](https://manpages.debian.org/systemd.resource-control.5) in `gokr-rsyncd.socket`
* To reduce the impact of Denial Of Service attacks, you can restrict resources
  with systemd, see [Managing
  Resources](http://0pointer.de/blog/projects/resources.html).
* To hide system directories not relevant to any rsync module, use [systemd’s
  `TemporaryFileSystem=` and
  `BindReadOnlyPaths=`](https://manpages.debian.org/systemd.exec.5) directives
  as described in [Use TemporaryFileSystem to hide files or directories from
  systemd
  services](https://www.sherbers.de/use-temporaryfilesystem-to-hide-files-or-directories-from-systemd-services/). Note
  that you [may need to disable `ProtectSystem=strict` due to a
  bug](https://github.com/systemd/systemd/issues/18999).

### Docker (unprivileged)

We provide [a `Dockerfile` for
`gokr-rsyncd`](https://github.com/gokrazy/rsync/tree/main/docker/).

```shell
docker run \
  --read-only \
  -p 127.0.0.1:8730:8730 \
  -v /etc/tmpfiles.d:/srv/rsync:ro \
  stapelberg/gokrazy-rsync:latest \
    --gokr.modulemap=pwd=/srv/rsync
```

Additional hardening recommendations:

* Restrict which IP addresses are allowed to connect to your rsync server, for example:
  * using iptables or nftables on your host system
  * using [`gokr-rsync`’s built-in IP allow/deny mechanism](https://github.com/gokrazy/rsync/issues/4) (once implemented)
    * Be sure to set up Docker such that the remote IPv4 or IPv6 address is available inside the container, see https://michael.stapelberg.ch/posts/2018-12-12-docker-ipv6/

### privileged Linux (including gokrazy.org)

When started as `root` on Linux, `gokr-rsync` will create a [Linux mount
namespace](https://manpages.debian.org/mount_namespaces.7), mount all configured
rsync modules read-only into the namespace, then change into the namespace using
[`chroot(2)`](https://manpages.debian.org/chroot.2) and drop privileges using
[`setuid(2)`](https://manpages.debian.org/setuid.2).

**Tip:** you can verify which file system objects the daemon process can see by
using `ls -l /proc/$(pidof gokr-rsync)/root/`.

Additional hardening recommendations:

* Restrict which IP addresses are allowed to connect to your rsync server, for example:
  * using iptables or nftables on your host system
  * using [`gokr-rsync`’s built-in IP allow/deny mechanism](https://github.com/gokrazy/rsync/issues/4) (once implemented)

### privileged non-Linux (e.g. Mac)

When started as `root` on non-Linux (e.g. Mac), `gokr-rsync` will drop
privileges using [`setuid(2)`](https://manpages.debian.org/setuid.2).

### unprivileged with write permission (e.g. from a shell)

To prevent accidental misconfiguration, `gokr-rsync` refuses to start when it
detects that it has write permission in any configured rsync module.

