// Package rsync (gokrazy/rsync) contains a native Go rsync implementation that
// supports sending and receiving files as client or server, compatible with the
// original tridge rsync (from the samba project) or openrsync (used on OpenBSD
// and macOS 15+).
//
// The only component currently is gokr-rsyncd, a read-only rsync daemon
// sender-only Go implementation of rsyncd. rsync daemon is a custom
// (un-standardized) network protocol, running on port 873 by default.
package rsync
