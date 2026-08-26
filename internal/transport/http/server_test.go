package http

import (
	"encoding/json"
	"icecoreacclimationgate/internal/application"
	"icecoreacclimationgate/internal/audit"
	"icecoreacclimationgate/internal/persistence"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCreateRejectsUnknownField(t *testing.T) {
	d := t.TempDir()
	s, _ := persistence.Open(d)
	a, _ := audit.Open(d)
	h := New(application.New(s, a)).Handler()
	r := httptest.NewRequest("POST", "/api/v1/acclimation-cases", strings.NewReader(`{"opened_by":"x","specimen_tubes":[],"extra":1}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("status=%d", w.Code)
	}
	_ = os.RemoveAll(d)
}

func TestCreateIdempotencyAndDirectoryPagination(t *testing.T) {
	d := t.TempDir()
	store, _ := persistence.Open(d)
	chain, _ := audit.Open(d)
	handler := New(application.New(store, chain)).Handler()
	create := func(body string) (int, map[string]any, string) {
		request := httptest.NewRequest("POST", "/api/v1/acclimation-cases", strings.NewReader(body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var result map[string]any
		_ = json.Unmarshal(response.Body.Bytes(), &result)
		return response.Code, result, response.Body.String()
	}
	body := `{"request_id":"create-1","opened_by":" operator-a ","storage_temperature_c":-35,"specimen_tubes":[{"tube_id":" T2 ","label":" core-2 "},{"tube_id":"T1","label":"core-1"}]}`
	status, first, firstRaw := create(body)
	if status != 201 || first["revision"] != float64(1) {
		t.Fatalf("create status=%d body=%v", status, first)
	}
	status, retried, retryRaw := create(body)
	if status != 201 || retryRaw != firstRaw || retried["case_id"] != first["case_id"] {
		t.Fatalf("idempotent retry changed response: %d %s %s", status, firstRaw, retryRaw)
	}
	status, _, _ = create(`{"request_id":"create-2","opened_by":"operator-b","storage_temperature_c":-30,"specimen_tubes":[{"tube_id":"T3","label":"core-3"}]}`)
	if status != 201 {
		t.Fatalf("second create status=%d", status)
	}

	request := httptest.NewRequest("GET", "/api/v1/acclimation-cases?state=Draft&opened_by=operator-a&limit=1", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var directory struct {
		Total       int                       `json:"total"`
		Items       []application.CaseSummary `json:"items"`
		StateCounts map[string]int            `json:"state_counts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &directory); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || directory.Total != 1 || len(directory.Items) != 1 || directory.Items[0].CaseID != first["case_id"] || directory.StateCounts["Draft"] != 1 {
		t.Fatalf("unexpected directory: status=%d body=%s", response.Code, response.Body.String())
	}

	for _, target := range []string{"/api/v1/acclimation-cases?unknown=x", "/api/v1/acclimation-cases?created_from=not-a-time"} {
		request = httptest.NewRequest("GET", target, nil)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 400 {
			t.Fatalf("%s status=%d", target, response.Code)
		}
	}
}
