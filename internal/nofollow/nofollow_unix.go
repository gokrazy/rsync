//go:build unix

package nofollow

import "golang.org/x/sys/unix"

// Maybe resolves to unix.O_NOFOLLOW on unix systems,
// 0 on other platforms.
const Maybe = unix.O_NOFOLLOW
