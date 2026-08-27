package auditrevisiongap

import (
	"icecoreacclimationgate/internal/application"
	"icecoreacclimationgate/internal/audit"
	"icecoreacclimationgate/internal/domain"
	"icecoreacclimationgate/internal/persistence"
	"testing"
)

func TestAuditVerifyDetectsRevisionGap(t *testing.T) {
	dir := t.TempDir()
	chain, err := audit.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := persistence.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(store, chain)
	created, err := service.Create(application.CreateCommand{
		RequestID: "create-gap", OpenedBy: "operator", StorageTemperatureC: -35,
		Tubes: []domain.SpecimenTube{{TubeID: "T1", Label: "core-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The second event has a valid digest/prev_digest chain but skips revision 2.
	if err := chain.Append(created.CaseID, "ProtocolConfigured", 3, map[string]any{"case_id": created.CaseID}); err != nil {
		t.Fatal(err)
	}
	store, err = persistence.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	chain, err = audit.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	recovered := application.New(store, chain)
	if recovered.Writable() {
		t.Fatalf("service remained writable despite per-case audit revision gap")
	}
}
