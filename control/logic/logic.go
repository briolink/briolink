// Package logic is the control plane's pure functions: the implementation of
// the control context's Funcs (spec/domain/control.json). Nothing here reads a
// clock, the network or the disk.
package logic

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/briolink/briolink/gen/domain/control"
)

// Impl implements control.Funcs.
type Impl struct{}

var _ control.Funcs = Impl{}

// maxPage and defaultPage bound a list's size.
const (
	defaultPage = 50
	maxPage     = 200
)

func (Impl) RoutingView(routing control.Routing) control.Routing {
	routing.Tiers = slices.SortedFunc(slices.Values(routing.Tiers), func(a, b control.TierModel) int {
		return strings.Compare(a.Tier, b.Tier)
	})
	routing.Jobs = slices.SortedFunc(slices.Values(routing.Jobs), func(a, b control.JobTier) int {
		return strings.Compare(a.Job, b.Job)
	})
	return routing
}

func (Impl) PageSize(limit int64) int64 {
	if limit <= 0 {
		return defaultPage
	}
	return min(limit, maxPage)
}

// taskStates are the states a state: label may name; unlabelled and conflict
// are never read from a label.
var taskStates = map[string]control.TaskState{
	"queued":         control.TaskStateQueued,
	"running":        control.TaskStateRunning,
	"needs_founder":  control.TaskStateNeedsFounder,
	"needs_reasoner": control.TaskStateNeedsReasoner,
	"blocked":        control.TaskStateBlocked,
	"waiting":        control.TaskStateWaiting,
}

func (Impl) TaskFromIssue(number int64, title string, labels []string, milestone string) control.Task {
	t := control.Task{Number: number, Title: title, Milestone: milestone, State: control.TaskStateUnlabelled}
	states := 0
	for _, l := range labels {
		if v, ok := strings.CutPrefix(l, "state:"); ok {
			if s, known := taskStates[strings.ReplaceAll(v, "-", "_")]; known {
				states++
				t.State = s
			}
		}
		if v, ok := strings.CutPrefix(l, "risk:"); ok && t.Risk == "" {
			t.Risk = v
		}
		if v, ok := strings.CutPrefix(l, "kind:"); ok && t.Kind == "" {
			t.Kind = v
		}
	}
	if states > 1 {
		t.State = control.TaskStateConflict
	}
	return t
}

func (Impl) CredentialsOfKind(credentials []control.Credential, kind string) []control.Credential {
	out := []control.Credential{}
	for _, c := range credentials {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

const dateLayout = "2006-01-02"

func (Impl) CredentialProblem(name, expiresOn, remindOn string) string {
	if len(name) == 0 || len(name) > 100 || strings.IndexFunc(name, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0 {
		return "the name must be 1 to 100 printable characters, without spaces"
	}
	expires, err := time.Parse(dateLayout, expiresOn)
	if expiresOn != "" && (err != nil || expires.Format(dateLayout) != expiresOn) {
		return "the expiry must be a date written YYYY-MM-DD"
	}
	remind, err := time.Parse(dateLayout, remindOn)
	if remindOn != "" && (err != nil || remind.Format(dateLayout) != remindOn) {
		return "the reminder must be a date written YYYY-MM-DD"
	}
	if expiresOn != "" && remindOn != "" && remindOn > expiresOn {
		return "the reminder must not be after the expiry"
	}
	return ""
}

// remindWithinDays is how close an expiry must be to be reported when the
// credential has no reminder date of its own.
const remindWithinDays = 30

func (Impl) CredentialAlerts(credentials []control.Credential, today string) []control.CredentialAlert {
	out := []control.CredentialAlert{}
	day, err := time.Parse(dateLayout, today)
	if err != nil {
		return out
	}
	horizon := day.AddDate(0, 0, remindWithinDays).Format(dateLayout)
	for _, c := range credentials {
		if c.Revoked || c.ExpiresOn == "" {
			continue
		}
		var state control.AlertState
		switch {
		case c.ExpiresOn < today:
			state = control.AlertStateExpired
		case c.RemindOn != "" && c.RemindOn <= today, c.RemindOn == "" && c.ExpiresOn <= horizon:
			state = control.AlertStateRemind
		default:
			continue
		}
		out = append(out, control.CredentialAlert{Kind: c.Kind, Name: c.Name, State: state, ExpiresOn: c.ExpiresOn})
	}
	slices.SortStableFunc(out, func(a, b control.CredentialAlert) int {
		return cmp.Or(strings.Compare(a.ExpiresOn, b.ExpiresOn), strings.Compare(a.Kind, b.Kind), strings.Compare(a.Name, b.Name))
	})
	return out
}
