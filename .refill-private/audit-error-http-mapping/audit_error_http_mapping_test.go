package audit_error_http_mapping

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
	"os"
	"path/filepath"
	"testing"
)

func TestAuditIOFailureMapsToInternalServerError(t *testing.T) {
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
	created, err := service.Create(application.CreateCommand{
		RequestID: "mapping-create", OpenedBy: "operator", StorageTemperatureC: -30,
		Tubes: []domain.SpecimenTube{{TubeID: "T1", Label: "L1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 让审计路径在服务运行期间失效；下一次写入的快照保存成功，但 Append 遇到目录会返回 I/O 错误。
	auditPath := filepath.Join(dir, "audit.jsonl")
	if err := os.Remove(auditPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(auditPath, 0755); err != nil {
		t.Fatal(err)
	}

	payload := map[string]any{
		"expected_revision": 1,
		"request_id":        "mapping-protocol",
		"protocol": map[string]any{
			"protocol_id": "P1", "case_id": created.CaseID,
			"stages":                      []map[string]any{{"index": 0, "target_temperature_c": -20, "hold_minutes": 10}},
			"max_warming_rate_c_per_hour": 120, "reading_interval_minutes": 5, "target_temperature_c": -20,
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/acclimation-cases/"+created.CaseID+"/protocol", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	transport.New(service).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("audit I/O failure returned HTTP %d, want %d: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}
