package http

import (
	"encoding/json"
	"icecoreacclimationgate/internal/application"
	"icecoreacclimationgate/internal/domain"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	App       *application.Service
	caseCache sync.Map
}

func New(app *application.Service) *Server { return &Server{App: app} }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.HandleHealth)
	mux.HandleFunc("/api/v1/acclimation-cases", s.HandleCases)
	mux.HandleFunc("/api/v1/acclimation-cases/", s.HandleCaseRoute)
	return limit(mux)
}

func limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, application.MaxRequestBody)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	write(w, http.StatusOK, map[string]any{"ok": true, "writable": s.App.Writable(), "integrity_reasons": s.App.IntegrityReasons()})
}

func (s *Server) HandleCases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var command application.CreateCommand
		if !decode(r, &command) {
			writeErr(w, http.StatusBadRequest, "INVALID_JSON", "JSON 载荷无效或含未知字段")
			return
		}
		if !command.StorageProvided {
			writeDomain(w, domain.Invalid("必须提供 storage_temperature_c"))
			return
		}
		c, err := s.App.Create(command)
		if err != nil {
			writeDomain(w, err)
			return
		}
		write(w, http.StatusCreated, c)
	case http.MethodGet:
		query, err := parseCaseListQuery(r)
		if err != nil {
			writeDomain(w, err)
			return
		}
		result, err := s.App.List(query)
		if err != nil {
			writeDomain(w, err)
			return
		}
		write(w, http.StatusOK, result)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "METHOD", "method not allowed")
	}
}

func parseCaseListQuery(r *http.Request) (application.CaseListQuery, error) {
	values := r.URL.Query()
	allowed := map[string]bool{"state": true, "opened_by": true, "created_from": true, "created_to": true, "created_at_from": true, "created_at_to": true, "limit": true, "cursor": true}
	for key, value := range values {
		if !allowed[key] || len(value) != 1 {
			return application.CaseListQuery{}, domain.Invalid("未知或重复查询参数: " + key)
		}
	}
	query := application.CaseListQuery{Limit: 50, OpenedBy: strings.TrimSpace(values.Get("opened_by")), Cursor: values.Get("cursor")}
	if raw := values.Get("state"); raw != "" {
		state := domain.State(raw)
		if !validState(state) {
			return query, domain.Invalid("state 无效")
		}
		query.State = &state
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return query, domain.Invalid("limit 无效")
		}
		query.Limit = limit
	}
	for _, item := range []struct {
		keys   []string
		target **time.Time
	}{{[]string{"created_from", "created_at_from"}, &query.CreatedFrom}, {[]string{"created_to", "created_at_to"}, &query.CreatedTo}} {
		for _, key := range item.keys {
			raw := values.Get(key)
			if raw == "" {
				continue
			}
			if *item.target != nil {
				return query, domain.Invalid("创建时间查询参数重复")
			}
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return query, domain.Invalid(key + " 必须为 RFC3339 时间")
			}
			parsed = parsed.UTC()
			*item.target = &parsed
		}
	}
	if query.CreatedFrom != nil && query.CreatedTo != nil && query.CreatedFrom.After(*query.CreatedTo) {
		return query, domain.Invalid("创建时间区间无效")
	}
	return query, nil
}

func validState(state domain.State) bool {
	switch state {
	case domain.Draft, domain.ProtocolReady, domain.Monitoring, domain.Quarantined, domain.MonitoringPassed, domain.EvidenceReady, domain.Reviewed, domain.ReleasedArchived:
		return true
	default:
		return false
	}
}

func (s *Server) HandleCaseRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 || parts[3] == "" {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "not found")
		return
	}
	id := parts[3]
	if len(parts) == 4 {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "METHOD", "method not allowed")
			return
		}
		c, cached := s.caseCache.Load(id)
		if !cached {
			loaded, err := s.App.Get(id)
			if err != nil {
				writeDomain(w, err)
				return
			}
			c = loaded.Clone()
			s.caseCache.Store(id, c)
		}
		cachedCase := c.(domain.AcclimationCase)
		view := cachedCase.Clone()
		body, _ := json.Marshal(view)
		out := map[string]any{}
		_ = json.Unmarshal(body, &out)
		out["monitoring_progress"] = view.MonitoringProgress()
		write(w, http.StatusOK, out)
		return
	}
	if len(parts) != 5 {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "unknown action")
		return
	}
	action := parts[4]
	if !validAction(action) {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "unknown action")
		return
	}
	if action == "quarantine-progress" || action == "evidence-events" {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "METHOD", "method not allowed")
			return
		}
		s.handleExtensionQuery(w, r, id, action)
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "METHOD", "method not allowed")
		return
	}
	if isExtensionMutation(action) {
		s.handleExtensionMutation(w, r, id, action)
		return
	}
	var input map[string]any
	if !decodeMap(r, &input) {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "JSON 载荷无效")
		return
	}
	allowed := map[string]bool{}
	if action != "verify" {
		allowed["expected_revision"] = true
	}
	if action != "verify" && action != "start" {
		allowed["request_id"] = true
	}
	payloadKey := action
	if action == "readings" || action == "retest" {
		payloadKey = "reading"
	}
	if action == "release" {
		allowed["authorizer_id"] = true
	} else if action != "start" && action != "verify" {
		allowed[payloadKey] = true
	}
	for key := range input {
		if !allowed[key] {
			writeErr(w, http.StatusBadRequest, "INVALID_JSON", "未知 JSON 字段: "+key)
			return
		}
	}
	expected := 0
	if _, present := input["expected_revision"]; present {
		var ok bool
		expected, ok = intVal(input, "expected_revision")
		if !ok || expected <= 0 {
			writeErr(w, http.StatusBadRequest, "INVALID_JSON", "expected_revision 必须为正整数")
			return
		}
	}
	requestID := strings.TrimSpace(strVal(input, "request_id"))
	var c domain.AcclimationCase
	var err error
	switch action {
	case "protocol":
		var payload domain.AcclimationProtocol
		if !mapDecode(input, payloadKey, &payload) {
			err = domain.Invalid("protocol 载荷无效或含未知字段")
		} else {
			c, err = s.App.Protocol(id, requestID, expected, payload)
		}
	case "start":
		c, err = s.App.Start(id, expected)
	case "readings":
		var payload domain.EnvironmentalReading
		if !objectHasKeys(input[payloadKey], "reading_id", "case_id", "stage_index", "observed_at", "chamber_temperature_c", "specimen_temperature_c", "relative_humidity_percent") || !mapDecode(input, payloadKey, &payload) {
			err = domain.Invalid("reading 载荷无效或含未知字段")
		} else {
			c, err = s.App.Reading(id, requestID, expected, payload)
		}
	case "deviation":
		var payload domain.DeviationRecord
		if !objectHasKeys(input[payloadKey], "deviation_id", "trigger_reading_id", "reason_code", "root_cause", "corrective_action", "retest_from_stage") || !mapDecode(input, payloadKey, &payload) {
			err = domain.Invalid("deviation 载荷无效或含未知字段")
		} else {
			c, err = s.App.Deviation(id, requestID, expected, payload)
		}
	case "retest":
		var payload domain.EnvironmentalReading
		if !objectHasKeys(input[payloadKey], "reading_id", "case_id", "stage_index", "observed_at", "chamber_temperature_c", "specimen_temperature_c", "relative_humidity_percent") || !mapDecode(input, payloadKey, &payload) {
			err = domain.Invalid("reading 载荷无效或含未知字段")
		} else {
			c, err = s.App.Retest(id, requestID, expected, payload)
		}
	case "evidence":
		var payload domain.ContaminationEvidence
		legacy := objectHasKeys(input[payloadKey], "blank_sample_id", "particle_count", "packaging_intact", "collected_at")
		perTube := objectHasKeys(input[payloadKey], "blank_sample_id", "blank_sample_passed", "tube_inspections", "collected_at")
		if perTube {
			object := input[payloadKey].(map[string]any)
			items, ok := object["tube_inspections"].([]any)
			perTube = ok && len(items) > 0
			for _, item := range items {
				if !objectHasKeys(item, "tube_id", "particle_count", "packaging_intact") {
					perTube = false
					break
				}
			}
		}
		if (!legacy && !perTube) || !mapDecode(input, payloadKey, &payload) {
			err = domain.Invalid("evidence 载荷无效或含未知字段")
		} else {
			c, err = s.App.Evidence(id, requestID, expected, payload)
		}
	case "review":
		var payload domain.Review
		if !mapDecode(input, payloadKey, &payload) {
			err = domain.Invalid("review 载荷无效或含未知字段")
		} else {
			c, err = s.App.Review(id, requestID, expected, payload)
		}
	case "release":
		c, err = s.App.Release(id, requestID, expected, strings.TrimSpace(strVal(input, "authorizer_id")))
	case "verify":
		result, verifyErr := s.App.Verify(id)
		if verifyErr != nil {
			writeDomain(w, verifyErr)
		} else {
			write(w, http.StatusOK, result)
		}
		return
	}
	if err != nil {
		writeDomain(w, err)
		return
	}
	s.caseCache.Delete(id)
	write(w, http.StatusOK, c)
}

func objectHasKeys(value any, keys ...string) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range keys {
		if _, exists := object[key]; !exists {
			return false
		}
	}
	return true
}

func validAction(action string) bool {
	switch action {
	case "protocol", "start", "readings", "deviation", "retest", "evidence", "review", "release", "verify",
		"draft-revision", "protocol-preview", "readings-batch", "quarantine-progress", "issue-responses", "release-preflight", "evidence-events":
		return true
	default:
		return false
	}
}

func isExtensionMutation(action string) bool {
	switch action {
	case "draft-revision", "protocol-preview", "readings-batch", "issue-responses", "release-preflight":
		return true
	default:
		return false
	}
}

func (s *Server) handleExtensionMutation(w http.ResponseWriter, r *http.Request, id, action string) {
	var input map[string]any
	if !decodeMap(r, &input) {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "JSON 载荷无效")
		return
	}
	allowedByAction := map[string]map[string]bool{
		"draft-revision":    {"expected_revision": true, "request_id": true, "specimen_tubes": true, "storage_temperature_c": true, "reason": true},
		"protocol-preview":  {"planned_start_at": true, "protocol": true},
		"readings-batch":    {"expected_revision": true, "request_id": true, "readings": true},
		"issue-responses":   {"expected_revision": true, "request_id": true, "responder_id": true, "responses": true},
		"release-preflight": {"expected_revision": true, "authorizer_id": true},
	}
	for key := range input {
		if !allowedByAction[action][key] {
			writeErr(w, http.StatusBadRequest, "INVALID_JSON", "未知 JSON 字段: "+key)
			return
		}
	}
	expected := 0
	if action != "protocol-preview" {
		var ok bool
		expected, ok = intVal(input, "expected_revision")
		if !ok || expected <= 0 {
			writeErr(w, http.StatusBadRequest, "INVALID_JSON", "expected_revision 必须为正整数")
			return
		}
	}
	requestID := strings.TrimSpace(strVal(input, "request_id"))
	var c domain.AcclimationCase
	var err error
	switch action {
	case "draft-revision":
		var tubes []domain.SpecimenTube
		storage, storageOK := input["storage_temperature_c"].(float64)
		if !mapDecode(input, "specimen_tubes", &tubes) || !storageOK {
			err = domain.Invalid("必须完整提供 specimen_tubes 和 storage_temperature_c")
		} else {
			c, err = s.App.ReviseDraft(id, requestID, expected, tubes, storage, strVal(input, "reason"))
		}
	case "protocol-preview":
		var protocol domain.AcclimationProtocol
		plannedStart, parseErr := time.Parse(time.RFC3339, strVal(input, "planned_start_at"))
		if !mapDecode(input, "protocol", &protocol) || parseErr != nil {
			err = domain.Invalid("planned_start_at 或 protocol 载荷无效")
		} else {
			if strings.TrimSpace(protocol.ProtocolID) == "" {
				protocol.ProtocolID = "PREVIEW"
			}
			result, previewErr := s.App.PreviewProtocol(id, plannedStart, protocol)
			if previewErr != nil {
				writeDomain(w, previewErr)
			} else {
				write(w, http.StatusOK, result)
			}
			return
		}
	case "readings-batch":
		var readings []domain.EnvironmentalReading
		rawReadings, rawOK := input["readings"].([]any)
		complete := rawOK
		for _, raw := range rawReadings {
			if !objectHasKeys(raw, "reading_id", "case_id", "stage_index", "checkpoint_sequence", "observed_at", "chamber_temperature_c", "specimen_temperature_c", "relative_humidity_percent") {
				complete = false
				break
			}
		}
		if !complete || !mapDecode(input, "readings", &readings) {
			err = domain.Invalid("readings 载荷无效或含未知字段")
		} else {
			for _, reading := range readings {
				if reading.ReadingID == "" || reading.CaseID == "" || reading.ObservedAt.IsZero() {
					err = domain.Invalid("批量读数字段不完整")
					break
				}
			}
			if err == nil {
				c, err = s.App.ReadingsBatch(id, requestID, expected, readings)
			}
		}
	case "issue-responses":
		var responses []domain.IssueResponse
		if !mapDecode(input, "responses", &responses) {
			err = domain.Invalid("responses 载荷无效或含未知字段")
		} else {
			c, err = s.App.RespondToIssues(id, requestID, expected, strVal(input, "responder_id"), responses)
		}
	case "release-preflight":
		result, preflightErr := s.App.ReleasePreflight(id, strVal(input, "authorizer_id"), expected)
		if preflightErr != nil {
			writeDomain(w, preflightErr)
		} else {
			write(w, http.StatusOK, result)
		}
		return
	}
	if err != nil {
		writeDomain(w, err)
		return
	}
	s.caseCache.Delete(id)
	write(w, http.StatusOK, c)
}

func (s *Server) handleExtensionQuery(w http.ResponseWriter, r *http.Request, id, action string) {
	if action == "quarantine-progress" {
		if len(r.URL.Query()) != 0 {
			writeDomain(w, domain.Invalid("隔离进度查询不接受查询参数"))
			return
		}
		result, err := s.App.QuarantineProgress(id)
		if err != nil {
			writeDomain(w, err)
		} else {
			write(w, http.StatusOK, result)
		}
		return
	}
	values := r.URL.Query()
	allowed := map[string]bool{"event_type": true, "revision_from": true, "revision_to": true, "limit": true}
	for key, value := range values {
		if !allowed[key] || len(value) != 1 {
			writeDomain(w, domain.Invalid("未知或重复查询参数: "+key))
			return
		}
	}
	query := application.AuditQuery{EventType: strings.TrimSpace(values.Get("event_type")), Limit: 100}
	if query.EventType != "" && !validAuditEventType(query.EventType) {
		writeDomain(w, domain.Invalid("event_type 无效"))
		return
	}
	for _, item := range []struct {
		key    string
		target *int
	}{{"revision_from", &query.RevisionFrom}, {"revision_to", &query.RevisionTo}, {"limit", &query.Limit}} {
		if raw := values.Get(item.key); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				writeDomain(w, domain.Invalid(item.key+" 必须为正整数"))
				return
			}
			*item.target = parsed
		}
	}
	if query.Limit > 500 || query.RevisionFrom > 0 && query.RevisionTo > 0 && query.RevisionFrom > query.RevisionTo {
		writeDomain(w, domain.Invalid("revision 区间或 limit 无效"))
		return
	}
	result, err := s.App.EvidenceEvents(id, query)
	if err != nil {
		writeDomain(w, err)
	} else {
		write(w, http.StatusOK, result)
	}
}

func validAuditEventType(value string) bool {
	switch value {
	case "CaseCreated", "DraftRevised", "ProtocolConfigured", "MonitoringStarted", "ReadingRecorded", "BatchReadingsRecorded", "DeviationRecorded", "RetestReadingRecorded", "EvidenceSubmitted", "ReviewIssuesResponded", "ReviewSubmitted", "ReleaseArchived":
		return true
	default:
		return false
	}
}
