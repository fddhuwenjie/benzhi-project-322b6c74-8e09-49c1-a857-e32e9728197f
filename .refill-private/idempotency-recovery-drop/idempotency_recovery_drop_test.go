package idempotency_recovery_drop_test

import (
	"encoding/json"
	"icecoreacclimationgate/internal/application"
	"icecoreacclimationgate/internal/audit"
	"icecoreacclimationgate/internal/domain"
	"icecoreacclimationgate/internal/persistence"
	"os"
	"path/filepath"
	"testing"
)

func TestRestartDropsCorruptIdempotencyRecord(t *testing.T) {
	dataDir := t.TempDir()
	store, err := persistence.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := audit.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	command := application.CreateCommand{
		Tubes:               []domain.SpecimenTube{{TubeID: "TUBE-1", Label: "core-one"}},
		StorageTemperatureC: -30,
		OpenedBy:            "operator-a",
		RequestID:           "create-recovery-1",
		StorageProvided:     true,
	}
	first, err := application.New(store, chain).Create(command)
	if err != nil {
		t.Fatal(err)
	}

	requestPath := filepath.Join(dataDir, "requests.json")
	raw, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	records := map[string]persistence.RequestRecord{}
	if err := json.Unmarshal(raw, &records); err != nil {
		t.Fatal(err)
	}
	record := records[command.RequestID]
	record.Body = json.RawMessage(`{}`)
	records[command.RequestID] = record
	corrupt, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(requestPath, corrupt, 0644); err != nil {
		t.Fatal(err)
	}

	reopened, err := persistence.Open(dataDir)
	if err != nil {
		return
	}
	reopenedChain, err := audit.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.New(reopened, reopenedChain).Create(command)
	if err != nil {
		t.Fatalf("重启接受损坏的幂等记录，且重试未返回稳定结果: %v", err)
	}
	if second.CaseID != first.CaseID && len(reopened.List()) == 2 {
		t.Fatalf("同一 request_id 在重启后创建了第二个批次: first=%s second=%s", first.CaseID, second.CaseID)
	}
	t.Fatalf("persistence.Open 接受了响应体损坏的幂等记录")
}
