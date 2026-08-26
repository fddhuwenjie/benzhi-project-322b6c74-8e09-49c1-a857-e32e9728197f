package persistence

import (
	"encoding/json"
	"icecoreacclimationgate/internal/domain"
	"os"
	"path/filepath"
)

func (s *Store) SnapshotMatches(id string, expected domain.AcclimationCase) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, err := os.ReadFile(filepath.Join(s.dir, "cases.json"))
	if err != nil {
		return false, err
	}
	var cases map[string]domain.AcclimationCase
	if err := json.Unmarshal(b, &cases); err != nil {
		return false, err
	}
	actual, ok := cases[id]
	if !ok {
		return false, nil
	}
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		return false, err
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		return false, err
	}
	return string(actualJSON) == string(expectedJSON), nil
}
