package application

import (
	"icecoreacclimationgate/internal/audit"
	"icecoreacclimationgate/internal/domain"
	"icecoreacclimationgate/internal/persistence"
	"testing"
)

func TestCreateIdempotencyAndProtocolSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := persistence.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := audit.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := New(store, chain)
	command := CreateCommand{
		RequestID: "create-restart", OpenedBy: "operator", StorageTemperatureC: -30,
		Tubes: []domain.SpecimenTube{{TubeID: "T2", Label: "L2"}, {TubeID: "T1", Label: "L1"}},
	}
	created, err := service.Create(command)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := service.Protocol(created.CaseID, "protocol-restart", created.Revision, domain.AcclimationProtocol{
		ProtocolID: "P1", Stages: []domain.Stage{{Index: 0, TargetTemperatureC: -20, HoldMinutes: 10}},
		MaxWarmingRateCPerHour: 120, ReadingIntervalMinutes: 5, TargetTemperatureC: -20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.Protocol.Stages[0].ExpectedCheckpoints != 2 {
		t.Fatalf("unexpected checkpoint budget: %+v", configured.Protocol.Stages)
	}

	reopenedStore, err := persistence.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	reopenedChain, err := audit.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	reopened := New(reopenedStore, reopenedChain)
	if !reopened.Writable() {
		t.Fatalf("valid recovery disabled writes: %v", reopened.IntegrityReasons())
	}
	retried, err := reopened.Create(command)
	if err != nil {
		t.Fatal(err)
	}
	if retried.CaseID != created.CaseID || retried.Revision != created.Revision || len(reopenedStore.List()) != 1 {
		t.Fatalf("create retry was not stable: created=%+v retried=%+v", created, retried)
	}
	restored, err := reopened.Get(created.CaseID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Protocol == nil || restored.Protocol.Stages[0].ExpectedCheckpoints != 2 || restored.State != domain.ProtocolReady {
		t.Fatalf("protocol recovery changed snapshot: %+v", restored)
	}
}

func TestInvalidRegistrationDoesNotPersist(t *testing.T) {
	dir := t.TempDir()
	store, _ := persistence.Open(dir)
	chain, _ := audit.Open(dir)
	service := New(store, chain)
	_, err := service.Create(CreateCommand{
		RequestID: "invalid-create", OpenedBy: "operator", StorageTemperatureC: -35,
		Tubes: []domain.SpecimenTube{{TubeID: "T1", Label: "same"}, {TubeID: "T2", Label: " same "}},
	})
	if err == nil {
		t.Fatal("duplicate label was accepted")
	}
	if len(store.List()) != 0 {
		t.Fatal("invalid registration was persisted")
	}
	if err := chain.Verify(); err != nil {
		t.Fatalf("invalid registration damaged audit chain: %v", err)
	}
}
