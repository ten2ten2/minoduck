package subscriptions

import (
	"testing"
	"time"
)

func TestEntitlementsAndSwitches(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Second)
	future := now.Add(time.Hour)
	if Effective("team", "past_due", &past, now).Code != "free" || Effective("team", "past_due", &future, now).Code != "team" {
		t.Fatal("grace incorrectly evaluated")
	}
	if Effective("team", "incomplete", nil, now).Details {
		t.Fatal("unpaid subscription granted access")
	}
	if !DeferredChange("team", "starter", "month", "year") || !DeferredChange("starter", "team", "year", "month") || DeferredChange("starter", "team", "month", "month") {
		t.Fatal("switch rules")
	}
	if Plans["starter"].Yearly != 290 || Plans["team"].Workspaces != 3 {
		t.Fatal("pricing drift")
	}
}
