# gokrazy rsync

[![tests](https://github.com/gokrazy/rsync/actions/workflows/main.yml/badge.svg)](https://github.com/gokrazy/rsync/actions/workflows/main.yml)

Package rsync contains a native Go rsync implementation.

The only component currently is gokr-rsyncd, a read-only rsync daemon
sender-only Go implementation of rsyncd. rsync daemon is a custom
(un-standardized) network protocol, running on port 873 by default.

This project accepts contributions as time permits to merge them (best effort).

## Existing rsync implementation survey

TODO: add max protocol version column

| Language | URL                                                                             | Note                                                                                                                                  | Server mode? |
|----------|---------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------|--------------|
| C        | [WayneD/rsync](https://github.com/WayneD/rsync)                                 | original “tridge” implementation                                                                                                      | ✔ yes        |
| C        | [kristapsdz/openrsync](https://github.com/kristapsdz/openrsync)                 | OpenBSD, good docs                                                                                                                    | ✔ yes        |
| **Go**   | [gokrazy/rsync](https://github.com/gokrazy/rsync)                               | → you are here ←                                                                                                                      | ✔ yes 🎉     |
| **Go**   | [jbreiding/rsync-go](https://github.com/jbreiding/rsync-go)                     | rsync algorithm                                                                                                                       | ❌ no        |
| **Go**   | [kaiakz/rsync-os](https://github.com/kaiakz/rsync-os)                           | only client/receiver                                                                                                                  | ❌ no        |
| **Go**   | [knight42](https://gist.github.com/knight42/6ad35ce6fbf96519259b43a8c3f37478)   | proxy                                                                                                                                 | ❌ no        |
| **Go**   | [c4milo/gsync](https://github.com/c4milo/gsync)                                 |                                                                                                                                       | ❌ no        |
| Java     | [APNIC-net/repositoryd](https://github.com/APNIC-net/repositoryd)               | archived                                                                                                                              | ✔ yes        |
| Java     | [JohannesBuchner/Jarsync](https://github.com/JohannesBuchner/Jarsync/)          | archived, [internet draft RFC “The rsync Network Protocol”](https://github.com/JohannesBuchner/Jarsync/blob/master/jarsync/rsync.txt) | ✔ yes        |
| Java     | [perlundq/yajsync](https://github.com/perlundq/yajsync#example)                 |                                                                                                                                       | ✔ yes        |
| C++      | [gilbertchen/acrosync-library](https://github.com/gilbertchen/acrosync-library) | commercial                                                                                                                            | ❌ no        |
| Rust     | [sourcefrog/rsyn](https://github.com/sourcefrog/rsyn#why-do-this)               | client, “rsyn is rsync with no c”                                                                                                     | ❌ no        |

TODO: recommend rsync v2.6.1pre2 as a simpler version to study

## Getting started

```
go install github.com/gokrazy/rsync/cmd/gokr-rsyncd
gokr-rsyncd -help
```
