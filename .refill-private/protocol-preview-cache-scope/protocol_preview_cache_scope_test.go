package protocol_preview_cache_scope_test

import (
	"icecoreacclimationgate/internal/application"
	"icecoreacclimationgate/internal/audit"
	"icecoreacclimationgate/internal/domain"
	"icecoreacclimationgate/internal/persistence"
	"testing"
	"time"
)

func TestProtocolPreviewCacheIsCaseScoped(t *testing.T) {
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

	create := func(requestID string, storage float64) domain.AcclimationCase {
		t.Helper()
		created, createErr := service.Create(application.CreateCommand{
			RequestID:           requestID,
			OpenedBy:            "operator-" + requestID,
			StorageTemperatureC: storage,
			Tubes:               []domain.SpecimenTube{{TubeID: "T-" + requestID, Label: "core-" + requestID}},
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return created
	}

	colder := create("colder", -40)
	warmer := create("warmer", -30)
	protocol := func() domain.AcclimationProtocol {
		return domain.AcclimationProtocol{
			ProtocolID:             "shared-protocol",
			Stages:                 []domain.Stage{{Index: 0, TargetTemperatureC: -20, HoldMinutes: 10}},
			MaxWarmingRateCPerHour: 120,
			ReadingIntervalMinutes: 5,
			TargetTemperatureC:     -20,
		}
	}
	firstStart := time.Date(2026, time.January, 2, 8, 0, 0, 0, time.UTC)
	secondStart := time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC)

	first, err := service.PreviewProtocol(colder.CaseID, firstStart, protocol())
	if err != nil {
		t.Fatal(err)
	}
	if first.Stages[0].MinimumWarmingMinutes != 10 {
		t.Fatalf("first preview setup mismatch: %+v", first)
	}
	second, err := service.PreviewProtocol(warmer.CaseID, secondStart, protocol())
	if err != nil {
		t.Fatal(err)
	}
	expectedCheckpoint := secondStart.Add(5 * time.Minute)
	if second.Stages[0].MinimumWarmingMinutes != 5 || !second.Stages[0].CheckpointTimes[0].Equal(expectedCheckpoint) {
		t.Fatalf("second case reused first case preview: got=%+v expected_warming_minutes=5 expected_checkpoint=%s", second, expectedCheckpoint)
	}
}
