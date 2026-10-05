package fleet

import (
	"strings"
	"testing"
	"time"

	"github.com/fleetlint/fleetlint/internal/model"
)

func TestTeamSummaryCountsExpiringExceptions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	passing := model.Counts{Pass: 1}
	rep := &Report{GeneratedAt: now, Repos: []RepoReport{
		{Name: "api", Team: "platform", Summary: passing, Exceptions: []model.Exception{
			{Rule: "a/soon", Reason: "r", Until: "2026-10-20"},
			{Rule: "a/later", Reason: "r", Until: "2027-03-31"},
			{Rule: "a/open", Reason: "r"},
		}},
		{Name: "web", Team: "platform", Summary: passing, Exceptions: []model.Exception{{Rule: "a/past", Reason: "r", Until: "2026-09-01", Expired: true}}},
		{Name: "broken", Team: "platform", Err: "clone failed"},
		{Name: "tool", Summary: passing},
	}}
	teams := summarizeTeams(rep.Repos, now)
	if len(teams) != 2 || teams[0].Team != noTeam || teams[1].Team != "platform" {
		t.Fatalf("teams: %+v", teams)
	}
	if p := teams[1]; p.Repos != 3 || p.Unchecked != 1 || p.Expiring != 2 || p.Compliance != 1 {
		t.Errorf("platform: %+v", p)
	}
	rows := strings.Join(collectExceptions(rep), "\n")
	for _, want := range []string{"| api | platform | `a/soon` | r | 2026-10-20 (in 15 days) |", "2027-03-31 |", "no deadline", "2026-09-01 (expired)"} {
		if !strings.Contains(rows, want) {
			t.Errorf("exception rows lack %q:\n%s", want, rows)
		}
	}
	if summarizeTeams([]RepoReport{{Name: "solo", Summary: passing}}, now) != nil {
		t.Error("a fleet without team names has no team summary")
	}
}
