package query_cache_stale_test

import (
	"bytes"
	"encoding/json"
	"icecoreacclimationgate/internal/application"
	"icecoreacclimationgate/internal/audit"
	"icecoreacclimationgate/internal/domain"
	"icecoreacclimationgate/internal/persistence"
	transport "icecoreacclimationgate/internal/transport/http"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestCachedCaseSurvivesCommittedMutation(t *testing.T) {
	dir := t.TempDir()
	store, err := persistence.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	chain, err := audit.Open(filepath.Join(dir, "audit"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(transport.New(application.New(store, chain)).Handler())
	defer server.Close()

	created := requestCase(t, http.MethodPost, server.URL+"/api/v1/acclimation-cases", map[string]any{
		"request_id": "create-cache-case", "opened_by": "operator-a", "storage_temperature_c": -30,
		"specimen_tubes": []map[string]any{{"tube_id": "tube-1", "label": "core-a"}},
	})
	caseURL := server.URL + "/api/v1/acclimation-cases/" + created.CaseID

	first := requestCase(t, http.MethodGet, caseURL, nil)
	if first.Revision != 1 {
		t.Fatalf("unexpected initial revision: %d", first.Revision)
	}
	updated := requestCase(t, http.MethodPost, caseURL+"/draft-revision", map[string]any{
		"expected_revision": 1, "request_id": "revise-cache-case", "reason": "补充冰芯管",
		"storage_temperature_c": -30,
		"specimen_tubes": []map[string]any{
			{"tube_id": "tube-1", "label": "core-a"},
			{"tube_id": "tube-2", "label": "core-b"},
		},
	})
	if updated.Revision != 2 {
		t.Fatalf("mutation did not commit revision 2: %d", updated.Revision)
	}

	observed := requestCase(t, http.MethodGet, caseURL, nil)
	if observed.Revision != updated.Revision || len(observed.SpecimenTubes) != 2 {
		t.Fatalf("cached query returned stale aggregate after committed mutation: revision=%d tubes=%d", observed.Revision, len(observed.SpecimenTubes))
	}
}

func requestCase(t *testing.T, method, url string, payload any) domain.AcclimationCase {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, url, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("unexpected HTTP status: %d", resp.StatusCode)
	}
	var result domain.AcclimationCase
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
