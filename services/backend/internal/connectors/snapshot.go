package connectors

import (
	"encoding/json"
	"github.com/ten2ten2/minoduck/services/backend/internal/ledger"
)

func (s Snapshot) validateUsage() error {
	seen := make(map[string]struct{}, len(s.Usage))
	for _, usage := range s.Usage {
		if usage.Key == "" || !usage.End.After(usage.Start) || usage.Start.Before(s.Start) || usage.End.After(s.End) {
			return Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
		}
		if _, exists := seen[usage.Key]; exists {
			return Failure{Code: "DUPLICATE_SOURCE_KEY", Permanent: true}
		}
		seen[usage.Key] = struct{}{}
		for _, raw := range usage.Metrics {
			value, err := ledger.Amount(string(raw))
			if err != nil || value.IsNegative() {
				return Failure{Code: "SOURCE_SCHEMA_CHANGED", Permanent: true}
			}
		}
	}
	return nil
}

// MarshalJSON is the final evidence boundary before a provider snapshot can be
// persisted. Usage receives the same conservative completeness treatment as
// cost entries: malformed provider data fails the whole shard instead of being
// stored and later participating in reconciliation or price simulations.
func (s Snapshot) MarshalJSON() ([]byte, error) {
	if err := s.validateUsage(); err != nil {
		return nil, err
	}
	type snapshotWithoutMethods Snapshot
	return json.Marshal(snapshotWithoutMethods(s))
}
