package logic

import (
	"reflect"
	"strings"
	"testing"

	"github.com/briolink/briolink/gen/domain/control"
)

func TestPageSize(t *testing.T) {
	for limit, want := range map[int64]int64{-5: 50, 0: 50, 1: 1, 49: 49, 50: 50, 199: 199, 200: 200, 201: 200, 1 << 40: 200} {
		if got := (Impl{}).PageSize(limit); got != want {
			t.Errorf("PageSize(%d) = %d, want %d", limit, got, want)
		}
	}
}

func TestRoutingViewSortsAndKeepsItsArgument(t *testing.T) {
	in := control.Routing{
		Tiers:                   []control.TierModel{{Tier: "top", Model: "m3"}, {Tier: "coder", Model: "m1"}, {Tier: "routine", Model: "m2"}},
		Jobs:                    []control.JobTier{{Job: "b.two", Tier: "top"}, {Job: "a.one", Tier: "coder"}},
		ProposePromotionBelowBp: 6000,
		OnTopLimit:              "queue-design-jobs",
	}
	before := in
	before.Tiers = append([]control.TierModel(nil), in.Tiers...)
	before.Jobs = append([]control.JobTier(nil), in.Jobs...)

	got := (Impl{}).RoutingView(in)
	if got.Tiers[0].Tier != "coder" || got.Tiers[1].Tier != "routine" || got.Tiers[2].Tier != "top" {
		t.Errorf("tiers = %v", got.Tiers)
	}
	if got.Jobs[0].Job != "a.one" || got.Jobs[1].Job != "b.two" {
		t.Errorf("jobs = %v", got.Jobs)
	}
	if got.ProposePromotionBelowBp != 6000 || got.OnTopLimit != "queue-design-jobs" {
		t.Errorf("scalars changed: %+v", got)
	}
	if !reflect.DeepEqual(in, before) {
		t.Errorf("argument changed: %+v", in)
	}
}

func TestTaskFromIssue(t *testing.T) {
	for name, tc := range map[string]struct {
		labels []string
		want   control.Task
	}{
		"no labels": {nil, control.Task{State: control.TaskStateUnlabelled}},
		"state with a hyphen": {[]string{"state:needs-founder"},
			control.Task{State: control.TaskStateNeedsFounder}},
		"risk and kind": {[]string{"kind:spike", "risk:medium", "state:queued"},
			control.Task{State: control.TaskStateQueued, Risk: "medium", Kind: "spike"}},
		"first risk and first kind win": {[]string{"kind:a", "kind:b", "risk:high", "risk:low"},
			control.Task{State: control.TaskStateUnlabelled, Risk: "high", Kind: "a"}},
		"two states conflict": {[]string{"state:queued", "state:blocked"},
			control.Task{State: control.TaskStateConflict}},
		"the same state twice is two labels": {[]string{"state:queued", "state:queued"},
			control.Task{State: control.TaskStateConflict}},
		"an unknown state is ignored": {[]string{"state:bogus", "state:waiting"},
			control.Task{State: control.TaskStateWaiting}},
		"only an unknown state": {[]string{"state:bogus"},
			control.Task{State: control.TaskStateUnlabelled}},
		"unlabelled and conflict are not read from a label": {[]string{"state:unlabelled", "state:conflict"},
			control.Task{State: control.TaskStateUnlabelled}},
		"other labels are ignored": {[]string{"bug", "restate:queued", "state"},
			control.Task{State: control.TaskStateUnlabelled}},
	} {
		t.Run(name, func(t *testing.T) {
			want := tc.want
			want.Number, want.Title, want.Milestone = 7, "A task", "P0"
			if got := (Impl{}).TaskFromIssue(7, "A task", tc.labels, "P0"); got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestCredentialsOfKind(t *testing.T) {
	in := []control.Credential{{Kind: "token", Name: "b"}, {Kind: "secret", Name: "a"}, {Kind: "token", Name: "a"}}
	before := append([]control.Credential(nil), in...)
	if got := (Impl{}).CredentialsOfKind(in, "token"); !reflect.DeepEqual(got, []control.Credential{in[0], in[2]}) {
		t.Errorf("tokens = %v", got)
	}
	if got := (Impl{}).CredentialsOfKind(in, "secret"); !reflect.DeepEqual(got, []control.Credential{in[1]}) {
		t.Errorf("secrets = %v", got)
	}
	for _, got := range [][]control.Credential{(Impl{}).CredentialsOfKind(in, "other"), (Impl{}).CredentialsOfKind(nil, "token")} {
		if got == nil || len(got) != 0 {
			t.Errorf("none = %#v, want an empty list that is not nil", got)
		}
	}
	if !reflect.DeepEqual(in, before) {
		t.Errorf("argument changed: %v", in)
	}
}

func TestCredentialProblem(t *testing.T) {
	long := strings.Repeat("a", 101)
	for name, tc := range map[string]struct {
		name, expires, remind string
		ok                    bool
	}{
		"name only":                 {"CF_API_TOKEN", "", "", true},
		"one character":             {"a", "", "", true},
		"a hundred characters":      {long[:100], "", "", true},
		"a hundred and one":         {long, "", "", false},
		"empty name":                {"", "", "", false},
		"a space":                   {"a b", "", "", false},
		"a control character":       {"a\tb", "", "", false},
		"not ASCII":                 {"é", "", "", false},
		"both dates":                {"a", "2027-10-17", "2027-09-17", true},
		"the same day":              {"a", "2027-10-17", "2027-10-17", true},
		"reminder alone":            {"a", "", "2027-09-17", true},
		"expiry alone":              {"a", "2027-10-17", "", true},
		"leap day":                  {"a", "2028-02-29", "", true},
		"no such leap day":          {"a", "2027-02-29", "", false},
		"month 13":                  {"a", "2027-13-01", "", false},
		"not a date":                {"a", "soon", "", false},
		"no zero padding":           {"a", "2027-1-7", "", false},
		"bad reminder":              {"a", "", "2027-02-30", false},
		"reminder after the expiry": {"a", "2027-09-17", "2027-10-17", false},
	} {
		got := (Impl{}).CredentialProblem(tc.name, tc.expires, tc.remind)
		if (got == "") != tc.ok {
			t.Errorf("%s: problem = %q, want ok = %v", name, got, tc.ok)
		}
		for _, secret := range []string{tc.name, tc.expires, tc.remind} {
			if len(secret) > 3 && strings.Contains(got, secret) {
				t.Errorf("%s: the sentence repeats %q: %q", name, secret, got)
			}
		}
	}
	// The first problem wins, in the order name, expiry, reminder.
	first := (Impl{}).CredentialProblem("", "x", "y")
	if second := (Impl{}).CredentialProblem("a", "x", "y"); first == second || second == (Impl{}).CredentialProblem("a", "2027-01-01", "y") {
		t.Errorf("problems are not told apart: %q / %q", first, second)
	}
}
