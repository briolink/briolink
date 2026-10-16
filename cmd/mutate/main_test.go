package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunExitCodes(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		want int
		out  string
	}{
		{"all caught", []string{"../../quality/mutate/testdata/strong", "-func", "settles_balance"}, 0, "0 survived"},
		{"survivors", []string{"../../quality/mutate/testdata/weak", "-func", "settles_balance"}, 1, "SURVIVED"},
		{"tests fail first", []string{"../../quality/mutate/testdata/broken"}, 2, ""},
		{"unknown function", []string{"../../quality/mutate/testdata/strong", "-func", "nope"}, 2, ""},
		{"no directory", nil, 2, ""},
		{"bad flag", []string{"-nope"}, 2, ""},
	} {
		var out, errb bytes.Buffer
		if got := run(c.args, &out, &errb); got != c.want {
			t.Errorf("%s: exit %d, want %d\n%s%s", c.name, got, c.want, out.String(), errb.String())
		}
		if !strings.Contains(out.String(), c.out) {
			t.Errorf("%s: output %q lacks %q", c.name, out.String(), c.out)
		}
	}
}
