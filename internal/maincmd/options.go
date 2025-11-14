package maincmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
)

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

	// On Windows, a local disk path like C:\rsync parses as
	// host="C", path="\\rsync". Detect that and error out.
	isDriveLetter := len(host) == 1 &&
		((host[0] >= 'A' && host[0] <= 'Z') ||
			(host[0] >= 'a' && host[0] <= 'z'))
	if isDriveLetter && src[i] == os.PathSeparator {
		return "", "", 0, fmt.Errorf("local disk path detected")
	}

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
