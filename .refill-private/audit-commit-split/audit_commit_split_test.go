package audit_commit_split_test

import (
	"icecoreacclimationgate/internal/application"
	"icecoreacclimationgate/internal/audit"
	"icecoreacclimationgate/internal/domain"
	"icecoreacclimationgate/internal/persistence"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAuditFailureDoesNotCommit(t *testing.T) {
	dir := t.TempDir()
	store, err := persistence.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := audit.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(store, chain)
	if err := os.Mkdir(filepath.Join(dir, "audit.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err = service.Create(application.CreateCommand{
		RequestID: "create-with-broken-audit", OpenedBy: "operator", StorageTemperatureC: -30,
		Tubes: []domain.SpecimenTube{{TubeID: "T1", Label: "core-1"}},
	})
	if err == nil {
		t.Fatal("expected the audit append to fail")
	}
	if cases := store.List(); len(cases) != 0 {
		t.Fatalf("failed operation committed %d case snapshot(s)", len(cases))
	}
	if _, ok := store.GetRequest("create-with-broken-audit"); ok {
		t.Fatal("failed operation committed its idempotency response")
	}
}
