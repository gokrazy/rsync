//go:build unix

package nofollow

import "golang.org/x/sys/unix"

// Maybe resolves to unix.O_NOFOLLOW on unix systems,
// 0 on other platforms. TODO(go1.24): use os.Root.
const Maybe = unix.O_NOFOLLOW
