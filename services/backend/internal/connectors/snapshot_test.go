package connectors

import (
	"encoding/json"
	"testing"
	"time"
)

func validUsageSnapshot() Snapshot {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	usage := Usage{
		Key:   "usage-key",
		Start: start,
		End:   start.Add(24 * time.Hour),
		Model: "synthetic-model",
		Metrics: map[string]Scalar{
			"input_total": "10",
			"output":      "2",
		},
		Dimensions: map[string]string{},
	}
	return Snapshot{Start: start, End: start.Add(24 * time.Hour), Usage: []Usage{usage}}
}

func TestSnapshotRejectsInvalidUsageBeforePersistence(t *testing.T) {
	for _, mutate := range []func(*Snapshot){
		func(s *Snapshot) { s.Usage[0].Metrics["input_total"] = "-1" },
		func(s *Snapshot) { s.Usage[0].Start = s.Start.Add(-time.Second) },
		func(s *Snapshot) { s.Usage[0].End = s.End.Add(time.Second) },
		func(s *Snapshot) { s.Usage[0].Key = "" },
		func(s *Snapshot) { s.Usage = append(s.Usage, s.Usage[0]) },
	} {
		snapshot := validUsageSnapshot()
		mutate(&snapshot)
		if _, err := json.Marshal(snapshot); err == nil {
			t.Fatal("invalid usage snapshot was serializable")
		}
	}
}

func TestSnapshotAllowsValidUsage(t *testing.T) {
	if _, err := json.Marshal(validUsageSnapshot()); err != nil {
		t.Fatal(err)
	}
}
