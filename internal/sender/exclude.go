package sender

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/gokrazy/rsync/internal/rsyncwire"
)

type filterRuleList struct {
	Filters []*filterRule
}

// exclude.c:add_rule
func (l *filterRuleList) addRule(fr *filterRule) {
	if strings.HasSuffix(fr.pattern, "/") {
		fr.flag |= filtruleDirectory
		fr.pattern = strings.TrimSuffix(fr.pattern, "/")
	}
	if strings.ContainsFunc(fr.pattern, func(r rune) bool {
		return r == '*' || r == '[' || r == '?'
	}) {
		fr.flag |= filtruleWild
	}
	l.Filters = append(l.Filters, fr)
}

// ParseFilterRules builds a filter rule list from textual rules (as produced by
// Options.FilterRules(), e.g. "- *.log" / "+ keep.txt"). It is used by the
// client when it is the sender, so that excluded files are not even put into the
// file list (rsync/exclude.c:parse_filter_str + add_rule).
func ParseFilterRules(rules []string) (*filterRuleList, error) {
	var l filterRuleList
	for _, rule := range rules {
		fr, err := parseFilter(rule)
		if err != nil {
			return nil, err
		}
		l.addRule(fr)
	}
	return &l, nil
}

// matches reports whether name should be excluded. Rules are evaluated in order
// and the first one that matches decides: an exclude rule excludes (true), an
// include rule protects the file from exclusion (false). This mirrors
// rsync/exclude.c:check_filter.
func (l *filterRuleList) matches(name string) bool {
	for _, fr := range l.Filters {
		if fr.matches(name) {
			return fr.flag&filtruleInclude == 0
		}
	}
	return false
}

// exclude.c:recv_filter_list
func RecvFilterList(c *rsyncwire.Conn) (*filterRuleList, error) {
	var l filterRuleList
	const exclusionListEnd = 0
	for {
		length, err := c.ReadInt32()
		if err != nil {
			return nil, err
		}
		if length == exclusionListEnd {
			break
		}
		line := make([]byte, length)
		if _, err := io.ReadFull(c.Reader, line); err != nil {
			return nil, err
		}
		fr, err := parseFilter(string(line))
		if err != nil {
			return nil, err
		}
		l.addRule(fr)
	}
	return &l, nil
}

const (
	filtruleInclude = 1 << iota
	filtruleClearList
	filtruleDirectory
	filtruleWild
)

type filterRule struct {
	flag    int
	pattern string
}

// exclude.c:rule_matches
func (fr *filterRule) matches(name string) bool {
	pattern := fr.pattern
	// A pattern with no slash matches against the basename only; a pattern with
	// a slash is anchored and matches against the whole (relative) path.
	if !strings.ContainsRune(pattern, '/') {
		name = filepath.Base(name)
	}
	if fr.flag&filtruleWild != 0 {
		return wildmatch(pattern, name)
	}
	return pattern == name
}

// wildmatch implements the subset of rsync's lib/wildmatch.c that rsync filter
// patterns use: '*' matches any run of characters except '/', '**' matches across
// '/' too, '?' matches any single character except '/', and '[...]' is a
// character class (with '!'/'^' negation and 'a-z' ranges).
func wildmatch(pattern, text string) bool {
	return dowild([]byte(pattern), []byte(text))
}

func dowild(p, t []byte) bool {
	ti := 0
	for pi := 0; pi < len(p); pi++ {
		pc := p[pi]
		if ti >= len(t) && pc != '*' {
			return false
		}
		switch pc {
		case '?':
			if t[ti] == '/' {
				return false
			}
			ti++
		case '*':
			doubleStar := pi+1 < len(p) && p[pi+1] == '*'
			for pi+1 < len(p) && p[pi+1] == '*' {
				pi++
			}
			if pi == len(p)-1 {
				// Trailing star: '**' matches the rest unconditionally, a single
				// '*' matches the rest only if it contains no '/'.
				return doubleStar || !bytesContainsSlash(t[ti:])
			}
			for ; ti <= len(t); ti++ {
				if dowild(p[pi+1:], t[ti:]) {
					return true
				}
				if ti < len(t) && t[ti] == '/' && !doubleStar {
					return false
				}
			}
			return false
		case '[':
			pi++
			negate := pi < len(p) && (p[pi] == '!' || p[pi] == '^')
			if negate {
				pi++
			}
			matched := false
			first := true
			for pi < len(p) && (p[pi] != ']' || first) {
				if pi+2 < len(p) && p[pi+1] == '-' && p[pi+2] != ']' {
					if t[ti] >= p[pi] && t[ti] <= p[pi+2] {
						matched = true
					}
					pi += 3
				} else {
					if t[ti] == p[pi] {
						matched = true
					}
					pi++
				}
				first = false
			}
			if matched == negate {
				return false
			}
			ti++
		default:
			if t[ti] != pc {
				return false
			}
			ti++
		}
	}
	return ti == len(t)
}

func bytesContainsSlash(b []byte) bool {
	for _, c := range b {
		if c == '/' {
			return true
		}
	}
	return false
}

// exclude.c:parse_filter_str / exclude.c:parse_rule_tok
func parseFilter(line string) (*filterRule, error) {
	rule := new(filterRule)

	// We only support what rsync calls XFLG_OLD_PREFIXES
	if strings.HasPrefix(line, "- ") {
		// clear include flag
		rule.flag &= ^filtruleInclude
		line = strings.TrimPrefix(line, "- ")
	} else if strings.HasPrefix(line, "+ ") {
		// set include flag
		rule.flag |= filtruleInclude
		line = strings.TrimPrefix(line, "+ ")
	} else if strings.HasPrefix(line, "!") {
		// set clear_list flag
		rule.flag |= filtruleClearList
	}

	rule.pattern = line

	return rule, nil
}
