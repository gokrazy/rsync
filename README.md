# gokrazy rsync

[![tests](https://github.com/gokrazy/rsync/actions/workflows/main.yml/badge.svg)](https://github.com/gokrazy/rsync/actions/workflows/main.yml)

Package rsync contains a native Go rsync implementation.

The only component currently is gokr-rsyncd, a read-only rsync daemon
sender-only Go implementation of rsyncd. rsync daemon is a custom
(un-standardized) network protocol, running on port 873 by default.

This project accepts contributions as time permits to merge them (best effort).

## Existing rsync implementation survey

| Language | URL                                                                             | Note                                                                                                                                  | Max Protocol                                                                                                        | Server mode? |
|----------|---------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------|--------------|
| C        | [WayneD/rsync](https://github.com/WayneD/rsync)                                 | original “tridge” implementation; I found [older versions](https://github.com/WayneD/rsync/tree/v2.6.1pre2) easier to study           | [31](https://github.com/WayneD/rsync/blob/592c6bc3e5e93f36c2fdc0a491a9fb43a41cf688/rsync.h#L113)                    | ✔ yes        |
| C        | [kristapsdz/openrsync](https://github.com/kristapsdz/openrsync)                 | OpenBSD, good docs                                                                                                                    | [27](https://github.com/kristapsdz/openrsync/blob/e54d57f7572381da2b549d39c7968fc79dac8e1d/extern.h#L30)            | ✔ yes        |
| **Go**   | [gokrazy/rsync](https://github.com/gokrazy/rsync)                               | → you are here ←                                                                                                                      | [27](https://github.com/gokrazy/rsync/blob/b3b58770b864613551036a2ef2827b74ace77749/internal/rsyncd/rsyncd.go#L317) | ✔ yes 🎉     |
| **Go**   | [jbreiding/rsync-go](https://github.com/jbreiding/rsync-go)                     | rsync algorithm                                                                                                                       |                                                                                                                     | ❌ no        |
| **Go**   | [kaiakz/rsync-os](https://github.com/kaiakz/rsync-os)                           | only client/receiver                                                                                                                  | [27](https://github.com/kaiakz/rsync-os/blob/64e84daeabb1fa4d2c7cf766c196306adfba6cb2/rsync/const.go#L4)            | ❌ no        |
| **Go**   | [knight42](https://gist.github.com/knight42/6ad35ce6fbf96519259b43a8c3f37478)   | proxy                                                                                                                                 |                                                                                                                     | ❌ no        |
| **Go**   | [c4milo/gsync](https://github.com/c4milo/gsync)                                 |                                                                                                                                       |                                                                                                                     | ❌ no        |
| Java     | [APNIC-net/repositoryd](https://github.com/APNIC-net/repositoryd)               | archived                                                                                                                              |                                                                                                                     | ✔ yes        |
| Java     | [JohannesBuchner/Jarsync](https://github.com/JohannesBuchner/Jarsync/)          | archived, [internet draft RFC “The rsync Network Protocol”](https://github.com/JohannesBuchner/Jarsync/blob/master/jarsync/rsync.txt) |                                                                                                                     | ✔ yes        |
| Java     | [perlundq/yajsync](https://github.com/perlundq/yajsync#example)                 |                                                                                                                                       |                                                                                                                     | ✔ yes        |
| C++      | [gilbertchen/acrosync-library](https://github.com/gilbertchen/acrosync-library) | commercial                                                                                                                            |                                                                                                                     | ❌ no        |
| Rust     | [sourcefrog/rsyn](https://github.com/sourcefrog/rsyn#why-do-this)               | client, “rsyn is rsync with no c”                                                                                                     | [27](https://github.com/sourcefrog/rsyn/blob/2ebbfcfe999fdf2d1a434d8614d07aa93873461b/src/connection.rs#L38)        | ❌ no        |

## Getting started

To serve the current directory via rsync on `localhost:8730`, use:

```
go install github.com/gokrazy/rsync/cmd/gokr-rsyncd
gokr-rsyncd -modulemap=pwd=$PWD
```

You can then copy the contents of the current directory with clients such as
`rsync(1)`:

```
% rsync -v --archive --port 8730 rsync://localhost/pwd/ quine
receiving file list ... done
created directory quine
./
.git/
[…]
.github/workflows/main.yml
LICENSE
Makefile
README.md
cmd/gokr-rsyncd/rsyncd.go
doc.go
go.mod
go.sum
internal/rsyncd/connection.go
internal/rsyncd/rsyncd.go
interop_test.go

sent 1,234 bytes  received 5,678 bytes  13,824.00 bytes/sec
total size is 666  speedup is 0.10

```

…or [`openrsync(1)`](https://github.com/kristapsdz/openrsync), shown doing a
differential update:

```
% openrsync -v --archive --port 8730 rsync://localhost/pwd/ quine
socket.c:109: warning: connect refused: ::1, localhost
Transfer starting: 369 files
.git/index (1.1 KB, 100.0% downloaded)
Transfer complete: 5.5 KB sent, 1.2 KB read, 666 B file size

```
