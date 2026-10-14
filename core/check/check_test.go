package check

import "testing"

func TestList(t *testing.T) {
	var l List
	if l.Err() != nil {
		t.Fatal("empty list must give nil")
	}
	l.Addf("a.b", "bad %d", 1)
	l.Addf("", "top")
	if got, want := l.Err().Error(), "a.b: bad 1\ntop"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPredicates(t *testing.T) {
	cases := []struct {
		name string
		fn   func(string) bool
		in   string
		want bool
	}{
		{"snake", Snake, "can_issue", true},
		{"snake upper", Snake, "canIssue", false},
		{"snake double underscore", Snake, "a__b", false},
		{"snake leading digit", Snake, "1a", false},
		{"pascal", Pascal, "Invoice", true},
		{"pascal lower", Pascal, "invoice", false},
		{"slug", Slug, "acme-0001", true},
		{"slug trailing hyphen", Slug, "acme-", false},
		{"semver", Semver, "0.1.0", true},
		{"semver leading zero", Semver, "0.01.0", false},
		{"semver short", Semver, "1.0", false},
		{"path", RelPath, "plugins/domain/x.go", true},
		{"path abs", RelPath, "/etc/passwd", false},
		{"path dotdot", RelPath, "a/../b", false},
		{"path dot", RelPath, "./a", false},
		{"path trailing slash", RelPath, "a/", false},
		{"path backslash", RelPath, `a\b`, false},
		{"text", Text, "one line", true},
		{"text empty", Text, "", false},
		{"text padded", Text, " x", false},
		{"text multiline", Text, "a\nb", false},
	}
	for _, c := range cases {
		if got := c.fn(c.in); got != c.want {
			t.Errorf("%s(%q) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}
