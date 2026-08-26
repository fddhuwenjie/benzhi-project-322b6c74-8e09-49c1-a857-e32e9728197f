package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func SelfCheck(base string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	post := func(path string, value any) (map[string]any, error) {
		body, _ := json.Marshal(value)
		response, err := client.Post(base+path, "application/json", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		var output map[string]any
		_ = json.NewDecoder(response.Body).Decode(&output)
		if response.StatusCode >= 300 {
			return nil, fmt.Errorf("%s status %d: %v", path, response.StatusCode, output)
		}
		return output, nil
	}
	get := func(path string) (map[string]any, error) {
		response, err := client.Get(base + path)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		var output map[string]any
		_ = json.NewDecoder(response.Body).Decode(&output)
		if response.StatusCode >= 300 {
			return nil, fmt.Errorf("%s status %d: %v", path, response.StatusCode, output)
		}
		return output, nil
	}
	revision := func(value map[string]any) int { return int(value["revision"].(float64)) }

	caseView, err := post("/api/v1/acclimation-cases", map[string]any{
		"request_id": "self-create", "opened_by": "admin", "storage_temperature_c": -35,
		"specimen_tubes": []any{map[string]any{"tube_id": "T1", "label": "core-1"}},
	})
	if err != nil {
		return err
	}
	id := caseView["case_id"].(string)
	caseView, err = post("/api/v1/acclimation-cases/"+id+"/draft-revision", map[string]any{
		"expected_revision": revision(caseView), "request_id": "self-draft-revision", "reason": "规范化登记标签",
		"storage_temperature_c": -35, "specimen_tubes": []any{map[string]any{"tube_id": "T1", "label": "core-one"}},
	})
	if err != nil {
		return err
	}
	protocol := map[string]any{
		"stages": []any{
			map[string]any{"index": 0, "target_temperature_c": -20, "hold_minutes": 5},
			map[string]any{"index": 1, "target_temperature_c": 0, "hold_minutes": 5},
		},
		"max_warming_rate_c_per_hour": 300, "reading_interval_minutes": 5,
		"protocol_id": "P1", "target_temperature_c": 0,
	}
	if _, err = post("/api/v1/acclimation-cases/"+id+"/protocol-preview", map[string]any{"planned_start_at": time.Now().UTC(), "protocol": protocol}); err != nil {
		return err
	}
	caseView, err = post("/api/v1/acclimation-cases/"+id+"/protocol", map[string]any{
		"expected_revision": revision(caseView), "request_id": "self-protocol", "protocol": protocol,
	})
	if err != nil {
		return err
	}
	caseView, err = post("/api/v1/acclimation-cases/"+id+"/start", map[string]any{"expected_revision": revision(caseView)})
	if err != nil {
		return err
	}
	observedAt := time.Now().UTC().Add(-time.Hour)
	caseView, err = post("/api/v1/acclimation-cases/"+id+"/readings", map[string]any{
		"expected_revision": revision(caseView), "request_id": "self-reading-fail",
		"reading": map[string]any{"reading_id": "R1", "case_id": id, "stage_index": 0, "observed_at": observedAt, "chamber_temperature_c": -10, "specimen_temperature_c": -10, "relative_humidity_percent": 50},
	})
	if err != nil {
		return err
	}
	if _, err = get("/api/v1/acclimation-cases/" + id + "/quarantine-progress"); err != nil {
		return err
	}
	caseView, err = post("/api/v1/acclimation-cases/"+id+"/deviation", map[string]any{
		"expected_revision": revision(caseView), "request_id": "self-deviation",
		"deviation": map[string]any{"deviation_id": "D1", "trigger_reading_id": "R1", "reason_code": "TEMP", "root_cause": "sensor", "corrective_action": "reset", "retest_from_stage": 0},
	})
	if err != nil {
		return err
	}
	for index, temperature := range []float64{-20, 0} {
		observedAt = observedAt.Add(5 * time.Minute)
		caseView, err = post("/api/v1/acclimation-cases/"+id+"/retest", map[string]any{
			"expected_revision": revision(caseView), "request_id": fmt.Sprintf("self-retest-%d", index),
			"reading": map[string]any{"reading_id": fmt.Sprintf("RR%d", index), "case_id": id, "stage_index": index, "sequence": index + 1, "observed_at": observedAt, "chamber_temperature_c": temperature, "specimen_temperature_c": temperature, "relative_humidity_percent": 50},
		})
		if err != nil {
			return err
		}
	}
	collectedAt := time.Now().UTC().Add(-time.Minute)
	caseView, err = post("/api/v1/acclimation-cases/"+id+"/evidence", map[string]any{
		"expected_revision": revision(caseView), "request_id": "self-evidence-1",
		"evidence": map[string]any{"blank_sample_id": "B1", "blank_sample_passed": true, "collected_at": collectedAt, "tube_inspections": []any{map[string]any{"tube_id": "T1", "particle_count": 2, "packaging_intact": true}}},
	})
	if err != nil {
		return err
	}
	issue := map[string]any{"issue_id": "I1", "category": "PHOTO", "description": "补充颗粒照片", "evidence_version": 1, "resolved": false}
	caseView, err = post("/api/v1/acclimation-cases/"+id+"/review", map[string]any{
		"expected_revision": revision(caseView), "request_id": "self-review-return",
		"review": map[string]any{"reviewer_id": "reviewer", "approved": false, "structured_issues": []any{issue}},
	})
	if err != nil {
		return err
	}
	caseView, err = post("/api/v1/acclimation-cases/"+id+"/evidence", map[string]any{
		"expected_revision": revision(caseView), "request_id": "self-evidence-2",
		"evidence": map[string]any{"blank_sample_id": "B2", "blank_sample_passed": true, "collected_at": collectedAt, "tube_inspections": []any{map[string]any{"tube_id": "T1", "particle_count": 1, "packaging_intact": true}}},
	})
	if err != nil {
		return err
	}
	caseView, err = post("/api/v1/acclimation-cases/"+id+"/issue-responses", map[string]any{
		"expected_revision": revision(caseView), "request_id": "self-issue-response", "responder_id": "admin",
		"responses": []any{map[string]any{"issue_id": "I1", "response": "已补充逐管照片", "evidence_version": 2}},
	})
	if err != nil {
		return err
	}
	issue["evidence_version"], issue["resolved"], issue["resolution"] = 2, true, "已补充照片并复核"
	caseView, err = post("/api/v1/acclimation-cases/"+id+"/review", map[string]any{
		"expected_revision": revision(caseView), "request_id": "self-review-pass",
		"review": map[string]any{"reviewer_id": "reviewer", "approved": true, "structured_issues": []any{issue}},
	})
	if err != nil {
		return err
	}
	preflight, err := post("/api/v1/acclimation-cases/"+id+"/release-preflight", map[string]any{"expected_revision": revision(caseView), "authorizer_id": "quality"})
	if err != nil {
		return err
	}
	if ready, _ := preflight["ready"].(bool); !ready {
		return fmt.Errorf("放行预检未就绪: %v", preflight)
	}
	caseView, err = post("/api/v1/acclimation-cases/"+id+"/release", map[string]any{
		"expected_revision": revision(caseView), "request_id": "self-release", "authorizer_id": "quality",
	})
	if err != nil {
		return err
	}
	archive, _ := caseView["archive"].(map[string]any)
	if archive["manifest_digest"] != preflight["manifest_digest"] {
		return fmt.Errorf("预检摘要与冻结档案不一致")
	}
	verified, err := post("/api/v1/acclimation-cases/"+id+"/verify", map[string]any{})
	if err != nil {
		return err
	}
	if valid, _ := verified["valid"].(bool); !valid {
		return fmt.Errorf("档案校验未通过: %v", verified)
	}
	events, err := get("/api/v1/acclimation-cases/" + id + "/evidence-events?limit=100")
	if err != nil {
		return err
	}
	if valid, _ := events["integrity_valid"].(bool); !valid {
		return fmt.Errorf("证据事件追溯未通过: %v", events)
	}
	return nil
}
