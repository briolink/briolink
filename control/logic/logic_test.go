package logic

import (
	"reflect"
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
