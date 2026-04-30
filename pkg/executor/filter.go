package executor

import (
	"fmt"
	"regexp"
	"strings"
)

// FilterRegexPrefix opts the test filter into regex matching when the value
// starts with this prefix. Without the prefix, the filter is treated as a
// plain substring match (existing behavior).
const FilterRegexPrefix = "regex:"

// filterMatcher decides whether a string passes the test filter. An empty
// matcher (or nil) matches everything. Constructed once per source so the
// regex (if any) is compiled a single time.
type filterMatcher struct {
	raw string
	sub string         // substring (when no regex prefix)
	re  *regexp.Regexp // compiled regex (when prefixed with FilterRegexPrefix)
}

// CompileFilter parses a filter string into a matcher.
// "regex:<expr>" → compiled regex; anything else → substring match.
// An empty string matches everything.
func CompileFilter(filter string) (*filterMatcher, error) {
	m := &filterMatcher{raw: filter}

	if filter == "" {
		return m, nil
	}

	if expr, ok := strings.CutPrefix(filter, FilterRegexPrefix); ok {
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid regex filter %q: %w", expr, err)
		}

		m.re = re

		return m, nil
	}

	m.sub = filter

	return m, nil
}

// Empty reports whether the matcher imposes no constraint (nil, empty
// substring, or empty regex pattern). Useful for callers that branch on
// "no filter applied" — like opcode counting that wants to short-circuit
// the per-entry check when nothing has been requested.
func (m *filterMatcher) Empty() bool {
	if m == nil {
		return true
	}

	if m.re != nil {
		// An empty regex matches everything; treat as "no filter".
		return m.re.String() == ""
	}

	return m.sub == ""
}

// match returns true when the input passes the filter. An empty/nil
// matcher always returns true.
func (m *filterMatcher) match(s string) bool {
	if m == nil {
		return true
	}

	if m.re != nil {
		return m.re.MatchString(s)
	}

	if m.sub == "" {
		return true
	}

	return strings.Contains(s, m.sub)
}

// String returns the original filter string for logging.
func (m *filterMatcher) String() string {
	if m == nil {
		return ""
	}

	return m.raw
}
