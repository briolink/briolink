// Package check collects every problem a Validate method finds, each with
// the path of the part it concerns, so one run reports them all.
package check

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// List accumulates problems; its zero value is ready to use.
type List struct {
	problems []string
}

// Addf records a problem at path.
func (l *List) Addf(path, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if path != "" {
		msg = path + ": " + msg
	}
	l.problems = append(l.problems, msg)
}

// Err returns nil when nothing was recorded, else one error listing every
// problem on its own line.
func (l *List) Err() error {
	if len(l.problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(l.problems, "\n"))
}

var (
	snake  = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	pascal = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	slug   = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	semver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

// Snake reports whether s is lower snake_case (states, events, fields, functions).
func Snake(s string) bool { return snake.MatchString(s) }

// Pascal reports whether s is PascalCase (entity, value and enum names).
func Pascal(s string) bool { return pascal.MatchString(s) }

// Slug reports whether s is a lower-case, hyphenated identifier (projects, tasks, plugins).
func Slug(s string) bool { return slug.MatchString(s) }

// Semver reports whether s is MAJOR.MINOR.PATCH.
func Semver(s string) bool { return semver.MatchString(s) }

// RelPath reports whether p is a clean, forward-slash path inside the repo:
// not absolute, no "..", no "." segments, no trailing slash.
func RelPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") || strings.Contains(p, `\`) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// Text reports whether s is non-empty, single-line and not padded with spaces.
func Text(s string) bool {
	return s != "" && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\n\r")
}
