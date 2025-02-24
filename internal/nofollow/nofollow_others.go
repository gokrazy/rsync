//go:build !unix

package nofollow

// Maybe resolves to unix.O_NOFOLLOW on unix systems,
// 0 on other platforms.
const Maybe = 0
