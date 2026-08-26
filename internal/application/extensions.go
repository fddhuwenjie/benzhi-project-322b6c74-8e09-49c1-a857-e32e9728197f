package application

import (
	"encoding/json"
	"fmt"
	"icecoreacclimationgate/internal/audit"
	"icecoreacclimationgate/internal/domain"
	"reflect"
	"sort"
	"strings"
	"time"
)

func (s *Service) ReviseDraft(id, requestID string, expected int, tubes []domain.SpecimenTube, storage float64, reason string) (domain.AcclimationCase, error) {
	if err := validateMutationParameters(requestID, expected); err != nil {
		return domain.AcclimationCase{}, err
	}
	payload := struct {
		Tubes   []domain.SpecimenTube `json:"specimen_tubes"`
		Storage float64               `json:"storage_temperature_c"`
		Reason  string                `json:"reason"`
	}{tubes, storage, reason}
	return s.mutateFPWithAudit(id, requestID, mutationFingerprint("draft-revision", id, expected, payload), expected, "DraftRevised", func(c *domain.AcclimationCase) error {
		return c.ReviseDraft(tubes, storage, reason, time.Now())
	}, func(c domain.AcclimationCase) any {
		return struct {
			domain.AcclimationCase
			Reason  string                   `json:"reason"`
			Changes []domain.FieldDifference `json:"changes"`
		}{c, c.DraftRevisions[len(c.DraftRevisions)-1].Reason, c.DraftRevisions[len(c.DraftRevisions)-1].Changes}
	})
}

func (s *Service) PreviewProtocol(id string, plannedStart time.Time, protocol domain.AcclimationProtocol) (domain.ProtocolPreview, error) {
	c, err := s.Get(id)
	if err != nil {
		return domain.ProtocolPreview{}, err
	}
	cacheKey := fingerprint(protocol)
	s.previewMu.Lock()
	cached, ok := s.previewCache[cacheKey]
	s.previewMu.Unlock()
	if ok {
		return cached, nil
	}
	preview, err := c.PreviewProtocol(protocol, plannedStart)
	if err != nil {
		return domain.ProtocolPreview{}, err
	}
	s.previewMu.Lock()
	s.previewCache[cacheKey] = preview
	s.previewMu.Unlock()
	return preview, nil
}

func (s *Service) ReadingsBatch(id, requestID string, expected int, readings []domain.EnvironmentalReading) (domain.AcclimationCase, error) {
	if err := validateMutationParameters(requestID, expected); err != nil {
		return domain.AcclimationCase{}, err
	}
	for i := range readings {
		readings[i].Retest = false
	}
	return s.mutateFPWithAudit(id, requestID, mutationFingerprint("readings-batch", id, expected, readings), expected, "BatchReadingsRecorded", func(c *domain.AcclimationCase) error {
		return c.AddReadingsBatch(readings)
	}, func(c domain.AcclimationCase) any {
		type summary struct {
			ReadingID      string            `json:"reading_id"`
			Verdict        string            `json:"verdict"`
			QualityDetails map[string]string `json:"quality_details"`
		}
		summaries := make([]summary, 0, len(readings))
		for _, recorded := range c.Readings[len(c.Readings)-len(readings):] {
			summaries = append(summaries, summary{recorded.ReadingID, recorded.Verdict, recorded.QualityDetails})
		}
		return struct {
			domain.AcclimationCase
			ReadingSummaries []summary `json:"reading_summaries"`
		}{c, summaries}
	})
}

func (s *Service) QuarantineProgress(id string) (domain.QuarantineProgress, error) {
	c, err := s.Get(id)
	if err != nil {
		return domain.QuarantineProgress{}, err
	}
	return c.QuarantineProgress()
}

func (s *Service) RespondToIssues(id, requestID string, expected int, responderID string, responses []domain.IssueResponse) (domain.AcclimationCase, error) {
	if err := validateMutationParameters(requestID, expected); err != nil {
		return domain.AcclimationCase{}, err
	}
	payload := struct {
		ResponderID string                 `json:"responder_id"`
		Responses   []domain.IssueResponse `json:"responses"`
	}{responderID, responses}
	return s.mutateFPWithAudit(id, requestID, mutationFingerprint("issue-responses", id, expected, payload), expected, "ReviewIssuesResponded", func(c *domain.AcclimationCase) error {
		return c.RespondToIssues(responderID, responses, time.Now())
	}, func(c domain.AcclimationCase) any {
		refs := c.IssueResponses[len(c.IssueResponses)-len(responses):]
		type responseReference struct {
			IssueID         string `json:"issue_id"`
			ResponderID     string `json:"responder_id"`
			EvidenceVersion int    `json:"evidence_version"`
		}
		redacted := make([]responseReference, 0, len(refs))
		for _, ref := range refs {
			redacted = append(redacted, responseReference{ref.IssueID, ref.ResponderID, ref.EvidenceVersion})
		}
		return struct {
			CaseID    string              `json:"case_id"`
			State     domain.State        `json:"state"`
			Revision  int                 `json:"revision"`
			Responses []responseReference `json:"responses"`
		}{c.CaseID, c.State, c.Revision, redacted}
	})
}

type ReleasePreflight struct {
	Ready           bool                  `json:"ready"`
	Checks          []domain.ReleaseCheck `json:"checks"`
	BlockingReasons []string              `json:"blocking_reasons"`
	CurrentRevision int                   `json:"current_revision"`
	ManifestEntries []string              `json:"manifest_entries,omitempty"`
	ManifestDigest  string                `json:"manifest_digest,omitempty"`
	Frozen          bool                  `json:"frozen"`
	ArchiveID       string                `json:"archive_id,omitempty"`
}

func (s *Service) ReleasePreflight(id, authorizer string, expected int) (ReleasePreflight, error) {
	c, err := s.Get(id)
	if err != nil {
		return ReleasePreflight{}, err
	}
	if c.State == domain.ReleasedArchived && c.Archive != nil {
		return ReleasePreflight{Ready: true, CurrentRevision: c.Revision, ManifestEntries: c.Archive.ManifestEntries, ManifestDigest: c.Archive.ManifestDigest, Frozen: true, ArchiveID: c.Archive.ArchiveID, Checks: []domain.ReleaseCheck{}, BlockingReasons: []string{}}, nil
	}
	result := ReleasePreflight{Checks: c.ReleaseChecks(authorizer, expected), CurrentRevision: c.Revision, BlockingReasons: []string{}}
	for _, check := range result.Checks {
		if !check.Passed {
			result.BlockingReasons = append(result.BlockingReasons, check.Name)
		}
	}
	result.Ready = len(result.BlockingReasons) == 0
	if result.Ready {
		candidate := c.Clone()
		candidate.State, candidate.Revision = domain.ReleasedArchived, c.Revision+1
		candidate.Archive = &domain.ReleaseArchive{CaseID: c.CaseID, ReviewerID: c.Review.ReviewerID, ReleaseAuthorizerID: strings.TrimSpace(authorizer), FinalRevision: candidate.Revision}
		result.ManifestEntries = audit.Manifest(candidate)
		result.ManifestDigest = audit.Digest(result.ManifestEntries)
	}
	return result, nil
}

type AuditQuery struct {
	EventType                       string
	RevisionFrom, RevisionTo, Limit int
}
type EventLocation struct {
	ManifestEntry string `json:"manifest_entry"`
	EventType     string `json:"event_type"`
	Revision      int    `json:"revision"`
	EventDigest   string `json:"event_digest"`
}
type EvidenceEventsResult struct {
	Timeline         []audit.Event   `json:"timeline"`
	IntegrityValid   bool            `json:"integrity_valid"`
	IntegrityReasons []string        `json:"integrity_reasons"`
	Locations        []EventLocation `json:"locations,omitempty"`
	UnmatchedEntries []string        `json:"unmatched_entries,omitempty"`
}

func (s *Service) EvidenceEvents(id string, query AuditQuery) (EvidenceEventsResult, error) {
	mu := s.caseLock(id)
	mu.Lock()
	defer mu.Unlock()
	c, ok := s.Store.Get(id)
	if !ok {
		return EvidenceEventsResult{}, domain.NotFound(id)
	}
	if s.Audit == nil {
		return EvidenceEventsResult{}, domain.Conflict("审计链不可用")
	}
	all, readErr := s.Audit.Events()
	if readErr != nil {
		return EvidenceEventsResult{}, readErr
	}
	caseEvents := make([]audit.Event, 0)
	for _, event := range all {
		if event.CaseID == id {
			caseEvents = append(caseEvents, event)
		}
	}
	sort.SliceStable(caseEvents, func(i, j int) bool {
		if caseEvents[i].Revision == caseEvents[j].Revision {
			return caseEvents[i].Type < caseEvents[j].Type
		}
		return caseEvents[i].Revision < caseEvents[j].Revision
	})
	result := EvidenceEventsResult{Timeline: []audit.Event{}, IntegrityReasons: []string{}, Locations: []EventLocation{}, UnmatchedEntries: []string{}}
	if err := s.Audit.Verify(); err != nil {
		result.IntegrityReasons = append(result.IntegrityReasons, "global_chain_digest_invalid")
	}
	byRevision := map[int]int{}
	for _, event := range caseEvents {
		byRevision[event.Revision]++
		var snapshot domain.AcclimationCase
		if json.Unmarshal(event.Data, &snapshot) != nil || snapshot.CaseID != id || snapshot.Revision != event.Revision {
			result.IntegrityReasons = append(result.IntegrityReasons, fmt.Sprintf("case_event_snapshot_mismatch:%d", event.Revision))
		} else if !validEventState(event.Type, snapshot.State) {
			result.IntegrityReasons = append(result.IntegrityReasons, fmt.Sprintf("case_event_state_invalid:%d", event.Revision))
		}
	}
	for revision := 1; revision <= c.Revision; revision++ {
		if byRevision[revision] == 0 {
			result.IntegrityReasons = append(result.IntegrityReasons, fmt.Sprintf("case_event_revision_missing:%d", revision))
		} else if byRevision[revision] > 1 {
			result.IntegrityReasons = append(result.IntegrityReasons, fmt.Sprintf("case_event_revision_duplicate:%d", revision))
		}
	}
	if len(caseEvents) == 0 || caseEvents[len(caseEvents)-1].Revision != c.Revision {
		result.IntegrityReasons = append(result.IntegrityReasons, "final_snapshot_revision_mismatch")
	}
	if c.State == domain.ReleasedArchived && c.Archive != nil {
		recomputed := audit.Manifest(c)
		if !reflect.DeepEqual(recomputed, c.Archive.ManifestEntries) || audit.Digest(recomputed) != c.Archive.ManifestDigest {
			result.IntegrityReasons = append(result.IntegrityReasons, "archive_manifest_digest_mismatch")
		}
		for _, entry := range c.Archive.ManifestEntries {
			if !traceableManifestEntry(entry) {
				continue
			}
			matches := locateEntry(entry, caseEvents)
			if len(matches) == 1 {
				result.Locations = append(result.Locations, matches[0])
			} else {
				result.UnmatchedEntries = append(result.UnmatchedEntries, entry)
			}
		}
		if len(result.UnmatchedEntries) > 0 {
			result.IntegrityReasons = append(result.IntegrityReasons, "archive_manifest_entry_unmatched")
		}
	}
	for _, event := range caseEvents {
		if query.EventType != "" && event.Type != query.EventType || query.RevisionFrom > 0 && event.Revision < query.RevisionFrom || query.RevisionTo > 0 && event.Revision > query.RevisionTo {
			continue
		}
		if len(result.Timeline) < query.Limit {
			result.Timeline = append(result.Timeline, event)
		}
	}
	result.IntegrityValid = len(result.IntegrityReasons) == 0
	return result, nil
}

func validEventState(eventType string, state domain.State) bool {
	switch eventType {
	case "CaseCreated", "DraftRevised":
		return state == domain.Draft
	case "ProtocolConfigured":
		return state == domain.ProtocolReady
	case "MonitoringStarted":
		return state == domain.Monitoring
	case "ReadingRecorded", "BatchReadingsRecorded":
		return state == domain.Monitoring || state == domain.Quarantined || state == domain.MonitoringPassed
	case "DeviationRecorded":
		return state == domain.Quarantined
	case "RetestReadingRecorded":
		return state == domain.Quarantined || state == domain.MonitoringPassed
	case "EvidenceSubmitted":
		return state == domain.MonitoringPassed || state == domain.EvidenceReady
	case "ReviewIssuesResponded":
		return state == domain.EvidenceReady
	case "ReviewSubmitted":
		return state == domain.EvidenceReady || state == domain.Reviewed
	case "ReleaseArchived":
		return state == domain.ReleasedArchived
	default:
		return false
	}
}

func traceableManifestEntry(entry string) bool {
	for _, prefix := range []string{"protocol:", "reading:", "deviation:", "evidence-version:", "issue:", "authorization:"} {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}

func locateEntry(entry string, events []audit.Event) []EventLocation {
	out := []EventLocation{}
	for _, event := range events {
		if eventContainsEntry(event, entry) {
			out = append(out, EventLocation{ManifestEntry: entry, EventType: event.Type, Revision: event.Revision, EventDigest: event.Digest})
			break
		}
	}
	return out
}

func eventContainsEntry(event audit.Event, entry string) bool {
	var c domain.AcclimationCase
	if json.Unmarshal(event.Data, &c) != nil {
		return false
	}
	value := entry[strings.IndexByte(entry, ':')+1:]
	switch {
	case strings.HasPrefix(entry, "protocol:"):
		return event.Type == "ProtocolConfigured" && c.Protocol != nil && c.Protocol.ProtocolID == value
	case strings.HasPrefix(entry, "reading:"):
		if event.Type != "ReadingRecorded" && event.Type != "BatchReadingsRecorded" && event.Type != "RetestReadingRecorded" {
			return false
		}
		for _, reading := range c.Readings {
			if reading.ReadingID == value {
				return true
			}
		}
	case strings.HasPrefix(entry, "deviation:"):
		return event.Type == "DeviationRecorded" && c.Deviation != nil && c.Deviation.DeviationID == value
	case strings.HasPrefix(entry, "evidence-version:"):
		return event.Type == "EvidenceSubmitted" && c.Evidence != nil && fmt.Sprintf("%d", c.Evidence.Version) == value
	case strings.HasPrefix(entry, "issue:"):
		if event.Type != "ReviewSubmitted" {
			return false
		}
		for _, issue := range c.Review.StructuredIssues {
			if issue.IssueID == value {
				return true
			}
		}
	case strings.HasPrefix(entry, "authorization:"):
		return event.Type == "ReleaseArchived" && c.Archive != nil && c.Archive.ReleaseAuthorizerID == value
	}
	return false
}
