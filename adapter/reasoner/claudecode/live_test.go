package claudecode

import (
	"os"
	"testing"

	"github.com/briolink/briolink/port/reasoner"
)

// TestLive runs the Reasoner contract against the real `claude` on the
// subscription login, one short call per tier, with both tiers mapped to the
// aliases `sonnet` and `opus`. Skipped unless BRIOLINK_LIVE_CLAUDE=1; it spends
// a little of the plan's usage, and BRIOLINK_LIVE_CLAUDE_TIERS (default
// "routine") adds `top`.
func TestLive(t *testing.T) {
	if os.Getenv("BRIOLINK_LIVE_CLAUDE") != "1" {
		t.Skip("BRIOLINK_LIVE_CLAUDE is not 1")
	}
	tiers := []reasoner.Tier{reasoner.Routine}
	if os.Getenv("BRIOLINK_LIVE_CLAUDE_TIERS") == "routine,top" {
		tiers = append(tiers, reasoner.Top)
	}
	reasoner.Contract(t, func(*testing.T) reasoner.Reasoner {
		return &Reasoner{Models: map[reasoner.Tier]string{reasoner.Routine: "sonnet", reasoner.Top: "opus"}}
	}, tiers, `Reply with the JSON object {"ok": true}.`)
}
