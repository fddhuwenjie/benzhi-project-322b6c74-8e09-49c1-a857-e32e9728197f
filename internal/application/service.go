package application

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"icecoreacclimationgate/internal/audit"
	"icecoreacclimationgate/internal/domain"
	"icecoreacclimationgate/internal/persistence"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

type Service struct {
	Store            *persistence.Store
	Audit            *audit.Chain
	locks            sync.Map
	createMu         sync.Mutex
	writeEnabled     bool
	integrityReasons []string
}

func New(store *persistence.Store, chain *audit.Chain) *Service {
	service := &Service{Store: store, Audit: chain, writeEnabled: true}
	service.checkRecoveredIntegrity()
	return service
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func (s *Service) Writable() bool { return s.writeEnabled }

func (s *Service) IntegrityReasons() []string {
	return append([]string{}, s.integrityReasons...)
}

func (s *Service) checkRecoveredIntegrity() {
	if s.Audit != nil {
		if err := s.Audit.Verify(); err != nil {
			s.writeEnabled = false
			s.integrityReasons = append(s.integrityReasons, "audit_chain_invalid")
		}
	}
	for _, c := range s.Store.List() {
		if c.State != domain.ReleasedArchived {
			continue
		}
		if c.Archive == nil || c.Archive.FinalRevision != c.Revision {
			s.writeEnabled = false
			s.integrityReasons = append(s.integrityReasons, "archive_revision_invalid:"+c.CaseID)
			continue
		}
		entries := audit.Manifest(c)
		if !reflect.DeepEqual(entries, c.Archive.ManifestEntries) || audit.Digest(entries) != c.Archive.ManifestDigest {
			s.writeEnabled = false
			s.integrityReasons = append(s.integrityReasons, "manifest_invalid:"+c.CaseID)
		}
	}
}

func (s *Service) ensureWritable() error {
	if !s.writeEnabled {
		return domain.Conflict("恢复完整性校验失败，写接口已关闭")
	}
	return nil
}

func (s *Service) caseLock(id string) *sync.Mutex {
	lock, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func (s *Service) mutateFP(id, requestID, fp string, expected int, eventType string, fn func(*domain.AcclimationCase) error) (domain.AcclimationCase, error) {
	return s.mutateFPWithAudit(id, requestID, fp, expected, eventType, fn, nil)
}

func (s *Service) mutateFPWithAudit(id, requestID, fp string, expected int, eventType string, fn func(*domain.AcclimationCase) error, eventData func(domain.AcclimationCase) any) (domain.AcclimationCase, error) {
	if err := s.ensureWritable(); err != nil {
		return domain.AcclimationCase{}, err
	}
	if requestID != "" {
		if old, ok := s.Store.GetRequest(requestID); ok {
			if old.Fingerprint != fp {
				return domain.AcclimationCase{}, domain.Conflict("request_id 幂等载荷冲突")
			}
			return decodeResponse(old)
		}
	}
	mu := s.caseLock(id)
	mu.Lock()
	defer mu.Unlock()
	if requestID != "" {
		if old, ok := s.Store.GetRequest(requestID); ok {
			if old.Fingerprint != fp {
				return domain.AcclimationCase{}, domain.Conflict("request_id 幂等载荷冲突")
			}
			return decodeResponse(old)
		}
	}
	c, ok := s.Store.Get(id)
	if !ok {
		return c, domain.NotFound(id)
	}
	if expected > 0 && c.Revision != expected {
		return c, domain.Conflict("revision mismatch")
	}
	original := c.Clone()
	if err := fn(&c); err != nil {
		return c, err
	}
	var record *persistence.RequestRecord
	if requestID != "" {
		body, _ := json.Marshal(c)
		record = &persistence.RequestRecord{RequestID: requestID, Fingerprint: fp, Status: 200, Body: body}
	}
	if err := s.Store.SaveMutation(c, record); err != nil {
		return c, err
	}
	if s.Audit != nil {
		data := any(c)
		if eventData != nil {
			data = eventData(c)
		}
		if err := s.Audit.Append(c.CaseID, eventType, c.Revision, data); err != nil {
			s.Store.RevertMutation(c.CaseID, requestID, &original)
			return c, err
		}
	}
	return c, nil
}

func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func mutationFingerprint(action, id string, expected int, payload any) string {
	return fingerprint(struct {
		Action   string `json:"action"`
		CaseID   string `json:"case_id"`
		Expected int    `json:"expected_revision"`
		Payload  any    `json:"payload"`
	}{action, id, expected, payload})
}

func validateMutationParameters(requestID string, expected int) error {
	if strings.TrimSpace(requestID) == "" || expected <= 0 {
		return domain.Invalid("必须提供正数 expected_revision 和非空 request_id")
	}
	return nil
}

func decodeResponse(record persistence.RequestRecord) (domain.AcclimationCase, error) {
	var c domain.AcclimationCase
	return c, json.Unmarshal(record.Body, &c)
}

func (s *Service) Create(command CreateCommand) (domain.AcclimationCase, error) {
	if err := s.ensureWritable(); err != nil {
		return domain.AcclimationCase{}, err
	}
	requestID := strings.TrimSpace(command.RequestID)
	if requestID == "" {
		return domain.AcclimationCase{}, domain.Invalid("request_id 不能为空")
	}
	fp := fingerprint(command)
	s.createMu.Lock()
	defer s.createMu.Unlock()
	if old, ok := s.Store.GetRequest(requestID); ok {
		if old.Fingerprint != fp {
			return domain.AcclimationCase{}, domain.Conflict("request_id 幂等载荷冲突")
		}
		return decodeResponse(old)
	}
	c, err := domain.NewAcclimationCase(newID("CASE-"), command.Tubes, command.StorageTemperatureC, command.OpenedBy, time.Now())
	if err != nil {
		return c, err
	}
	body, _ := json.Marshal(c)
	record := persistence.RequestRecord{RequestID: requestID, Fingerprint: fp, Status: 201, Body: body}
	if err = s.Store.SaveMutation(c, &record); err != nil {
		return c, err
	}
	if s.Audit != nil {
		if err = s.Audit.Append(c.CaseID, "CaseCreated", c.Revision, StateOf(c)); err != nil {
			s.Store.RevertMutation(c.CaseID, requestID, nil)
			return c, err
		}
	}
	return c, nil
}

func (s *Service) Protocol(id, requestID string, expected int, p domain.AcclimationProtocol) (domain.AcclimationCase, error) {
	if err := validateMutationParameters(requestID, expected); err != nil {
		return domain.AcclimationCase{}, err
	}
	return s.mutateFP(id, requestID, mutationFingerprint("protocol", id, expected, p), expected, "ProtocolConfigured", func(c *domain.AcclimationCase) error {
		return c.ConfigureProtocol(p, time.Now())
	})
}

func (s *Service) Start(id string, expected int) (domain.AcclimationCase, error) {
	return s.mutateFP(id, "", "", expected, "MonitoringStarted", func(c *domain.AcclimationCase) error { return c.StartMonitoring() })
}

func (s *Service) Reading(id, requestID string, expected int, r domain.EnvironmentalReading) (domain.AcclimationCase, error) {
	if err := validateMutationParameters(requestID, expected); err != nil {
		return domain.AcclimationCase{}, err
	}
	r.Retest = false
	return s.mutateFP(id, requestID, mutationFingerprint("reading", id, expected, r), expected, "ReadingRecorded", func(c *domain.AcclimationCase) error { return c.AddReading(r) })
}

func (s *Service) Deviation(id, requestID string, expected int, d domain.DeviationRecord) (domain.AcclimationCase, error) {
	if err := validateMutationParameters(requestID, expected); err != nil {
		return domain.AcclimationCase{}, err
	}
	return s.mutateFP(id, requestID, mutationFingerprint("deviation", id, expected, d), expected, "DeviationRecorded", func(c *domain.AcclimationCase) error {
		return c.RecordDeviation(d, time.Now())
	})
}

func (s *Service) Retest(id, requestID string, expected int, r domain.EnvironmentalReading) (domain.AcclimationCase, error) {
	if err := validateMutationParameters(requestID, expected); err != nil {
		return domain.AcclimationCase{}, err
	}
	r.Retest = true
	return s.mutateFP(id, requestID, mutationFingerprint("retest", id, expected, r), expected, "RetestReadingRecorded", func(c *domain.AcclimationCase) error { return c.AddRetestReading(r) })
}

func (s *Service) Evidence(id, requestID string, expected int, e domain.ContaminationEvidence) (domain.AcclimationCase, error) {
	if err := validateMutationParameters(requestID, expected); err != nil {
		return domain.AcclimationCase{}, err
	}
	return s.mutateFPWithAudit(id, requestID, mutationFingerprint("evidence", id, expected, e), expected, "EvidenceSubmitted", func(c *domain.AcclimationCase) error {
		return c.AddEvidence(e, time.Now())
	}, func(c domain.AcclimationCase) any {
		return struct {
			domain.AcclimationCase
			EvidenceVersion int      `json:"evidence_version"`
			FailedTubeIDs   []string `json:"failed_tube_ids"`
		}{c, c.Evidence.Version, c.Evidence.FailedTubeIDs}
	})
}

func (s *Service) Review(id, requestID string, expected int, r domain.Review) (domain.AcclimationCase, error) {
	if err := validateMutationParameters(requestID, expected); err != nil {
		return domain.AcclimationCase{}, err
	}
	return s.mutateFP(id, requestID, mutationFingerprint("review", id, expected, r), expected, "ReviewSubmitted", func(c *domain.AcclimationCase) error {
		return c.SubmitReview(r, time.Now())
	})
}

func (s *Service) Release(id, requestID string, expected int, authorizer string) (domain.AcclimationCase, error) {
	requestID, authorizer = strings.TrimSpace(requestID), strings.TrimSpace(authorizer)
	if requestID == "" || expected <= 0 || authorizer == "" {
		return domain.AcclimationCase{}, domain.Invalid("release 必须提供正数 expected_revision、request_id 和 authorizer_id")
	}
	payload := struct {
		CaseID     string `json:"case_id"`
		Expected   int    `json:"expected_revision"`
		Authorizer string `json:"authorizer_id"`
	}{id, expected, authorizer}
	return s.mutateFP(id, requestID, fingerprint(payload), expected, "ReleaseArchived", func(c *domain.AcclimationCase) error {
		for _, check := range c.ReleaseChecks(authorizer, expected) {
			if !check.Passed {
				if check.Name == "revision_matches" {
					return domain.Conflict(check.Reason)
				}
				return domain.Invalid(check.Reason)
			}
		}
		now := time.Now().UTC()
		if err := c.AuthorizeRelease(authorizer, now); err != nil {
			return err
		}
		c.Archive = &domain.ReleaseArchive{
			ArchiveID: newID("ARCH-"), CaseID: c.CaseID, ReviewerID: c.Review.ReviewerID,
			ReleaseAuthorizerID: authorizer, FinalRevision: c.Revision, SealedAt: now,
		}
		entries := audit.Manifest(*c)
		c.Archive.ManifestEntries = entries
		c.Archive.ManifestDigest = audit.Digest(entries)
		return nil
	})
}

func (s *Service) Get(id string) (domain.AcclimationCase, error) {
	c, ok := s.Store.Get(id)
	if !ok {
		return c, domain.NotFound(id)
	}
	return c, nil
}

type CaseListQuery struct {
	State       *domain.State
	OpenedBy    string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Limit       int
	Cursor      string
}

type CaseSummary struct {
	CaseID    string       `json:"case_id"`
	State     domain.State `json:"state"`
	Revision  int          `json:"revision"`
	OpenedBy  string       `json:"opened_by"`
	CreatedAt time.Time    `json:"created_at"`
	TubeCount int          `json:"tube_count"`
}

type CaseListResult struct {
	Total       int                  `json:"total"`
	Items       []CaseSummary        `json:"items"`
	Cursor      string               `json:"cursor"`
	NextCursor  string               `json:"next_cursor,omitempty"`
	Revision    int                  `json:"revision"`
	StateCounts map[domain.State]int `json:"state_counts"`
}

func (s *Service) List(query CaseListQuery) (CaseListResult, error) {
	if query.Limit <= 0 || query.Limit > 200 {
		return CaseListResult{}, domain.Invalid("limit 必须在 1 到 200 之间")
	}
	var cursorTime time.Time
	var cursorID string
	if query.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(query.Cursor)
		parts := strings.SplitN(string(raw), "|", 2)
		if err != nil || len(parts) != 2 {
			return CaseListResult{}, domain.Invalid("cursor 无效")
		}
		cursorTime, err = time.Parse(time.RFC3339Nano, parts[0])
		if err != nil || parts[1] == "" {
			return CaseListResult{}, domain.Invalid("cursor 无效")
		}
		cursorID = parts[1]
	}
	cases := s.Store.List()
	sort.Slice(cases, func(i, j int) bool {
		if cases[i].CreatedAt.Equal(cases[j].CreatedAt) {
			return cases[i].CaseID < cases[j].CaseID
		}
		return cases[i].CreatedAt.Before(cases[j].CreatedAt)
	})
	filtered := make([]domain.AcclimationCase, 0, len(cases))
	counts := map[domain.State]int{
		domain.Draft: 0, domain.ProtocolReady: 0, domain.Monitoring: 0, domain.Quarantined: 0,
		domain.MonitoringPassed: 0, domain.EvidenceReady: 0, domain.Reviewed: 0, domain.ReleasedArchived: 0,
	}
	maxRevision := 0
	for _, c := range cases {
		if query.State != nil && c.State != *query.State || query.OpenedBy != "" && c.OpenedBy != query.OpenedBy {
			continue
		}
		if query.CreatedFrom != nil && c.CreatedAt.Before(*query.CreatedFrom) || query.CreatedTo != nil && c.CreatedAt.After(*query.CreatedTo) {
			continue
		}
		filtered = append(filtered, c)
		counts[c.State]++
		if c.Revision > maxRevision {
			maxRevision = c.Revision
		}
	}
	result := CaseListResult{Total: len(filtered), Items: []CaseSummary{}, Cursor: query.Cursor, Revision: maxRevision, StateCounts: counts}
	start := 0
	if !cursorTime.IsZero() {
		start = sort.Search(len(filtered), func(i int) bool {
			return filtered[i].CreatedAt.After(cursorTime) || filtered[i].CreatedAt.Equal(cursorTime) && filtered[i].CaseID > cursorID
		})
	}
	end := start + query.Limit
	if end > len(filtered) {
		end = len(filtered)
	}
	for _, c := range filtered[start:end] {
		result.Items = append(result.Items, CaseSummary{c.CaseID, c.State, c.Revision, c.OpenedBy, c.CreatedAt, len(c.SpecimenTubes)})
	}
	if end < len(filtered) && end > start {
		last := filtered[end-1]
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt.Format(time.RFC3339Nano) + "|" + last.CaseID))
	}
	return result, nil
}

func (s *Service) Verify(id string) (map[string]any, error) {
	c, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if c.Archive == nil {
		return nil, domain.Invalid("档案尚未生成")
	}
	recomputed := audit.Manifest(c)
	entriesOK := reflect.DeepEqual(recomputed, c.Archive.ManifestEntries)
	digestOK := entriesOK && audit.Digest(recomputed) == c.Archive.ManifestDigest
	chainOK := s.Audit == nil || s.Audit.Verify() == nil
	snapshotOK, snapshotErr := s.Store.SnapshotMatches(id, c)
	if snapshotErr != nil {
		snapshotOK = false
	}
	revisionOK := c.Archive.FinalRevision == c.Revision
	stateOK := c.State == domain.ReleasedArchived
	reasons := []string{}
	if !digestOK {
		reasons = append(reasons, "manifest_digest_mismatch")
	}
	if !chainOK {
		reasons = append(reasons, "audit_chain_invalid")
	}
	if !snapshotOK {
		reasons = append(reasons, "snapshot_invalid")
	}
	if !revisionOK {
		reasons = append(reasons, "final_revision_mismatch")
	}
	if !stateOK {
		reasons = append(reasons, "archive_state_invalid")
	}
	checks := []map[string]any{
		{"name": "manifest_digest", "valid": digestOK},
		{"name": "audit_chain", "valid": chainOK},
		{"name": "snapshot", "valid": snapshotOK},
		{"name": "final_revision", "valid": revisionOK},
		{"name": "archive_state", "valid": stateOK},
	}
	return map[string]any{
		"case_id": id, "valid": digestOK && chainOK && snapshotOK && revisionOK && stateOK,
		"manifest_digest": c.Archive.ManifestDigest, "state": c.State, "final_revision": c.Archive.FinalRevision,
		"checks": checks, "failure_reasons": reasons,
	}, nil
}
