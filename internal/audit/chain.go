package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"icecoreacclimationgate/internal/domain"
	"os"
	"path/filepath"
	"sync"
)

type Event struct {
	Revision   int             `json:"revision"`
	CaseID     string          `json:"case_id"`
	Type       string          `json:"type"`
	Data       json.RawMessage `json:"data"`
	PrevDigest string          `json:"prev_digest"`
	Digest     string          `json:"digest"`
}
type Chain struct {
	path string
	mu   sync.Mutex
}

func Open(dir string) (*Chain, error) {
	if e := os.MkdirAll(dir, 0755); e != nil {
		return nil, e
	}
	return &Chain{path: filepath.Join(dir, "audit.jsonl")}, nil
}
func (c *Chain) Append(caseID, typ string, rev int, data any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	prev, _ := c.previousDigest()
	encodedData, e := json.Marshal(data)
	if e != nil {
		return e
	}
	ev := Event{rev, caseID, typ, encodedData, prev, ""}
	raw, _ := json.Marshal(ev)
	sum := sha256.Sum256(raw)
	ev.Digest = hex.EncodeToString(sum[:])
	line, _ := json.Marshal(ev)
	f, e := os.OpenFile(c.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	defer f.Close()
	_, e = f.Write(append(line, '\n'))
	return e
}

// previousDigest reads only the current tail of the append-only chain. The
// caller currently ignores read/parse failures and does not validate the
// decoded tail digest, so a subsequent append can silently sever the link
// until Verify is called.
func (c *Chain) previousDigest() (string, error) {
	b, err := os.ReadFile(c.path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	lines := splitLines(b)
	if len(lines) == 0 {
		return "", nil
	}
	var last Event
	if err := json.Unmarshal(lines[len(lines)-1], &last); err != nil {
		return "", err
	}
	return last.Digest, nil
}
func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, b[start:i])
			}
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}
func (c *Chain) Verify() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, e := os.ReadFile(c.path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	prev := ""
	for _, line := range splitLines(b) {
		var ev Event
		if json.Unmarshal(line, &ev) != nil {
			return fmt.Errorf("invalid audit event")
		}
		d := ev.Digest
		ev.Digest = ""
		raw, _ := json.Marshal(ev)
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != d || ev.PrevDigest != prev {
			return fmt.Errorf("audit chain broken")
		}
		prev = d
	}
	return nil
}

func (c *Chain) Events() ([]Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := os.ReadFile(c.path)
	if os.IsNotExist(err) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	events := make([]Event, 0)
	for _, line := range splitLines(b) {
		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("invalid audit event")
		}
		events = append(events, event)
	}
	return events, nil
}
func Manifest(c domain.AcclimationCase) []string {
	entries := []string{"case:" + c.CaseID, "state:" + string(c.State), fmt.Sprintf("revision:%d", c.Revision)}
	for _, t := range c.SpecimenTubes {
		entries = append(entries, "tube:"+t.TubeID+":"+t.Label)
	}
	if c.Protocol != nil {
		entries = append(entries, "protocol:"+c.Protocol.ProtocolID)
	}
	for _, r := range c.Readings {
		entries = append(entries, "reading:"+r.ReadingID)
	}
	for _, deviation := range c.DeviationHistory {
		entries = append(entries, "deviation:"+deviation.DeviationID)
	}
	if len(c.DeviationHistory) == 0 && c.Deviation != nil {
		entries = append(entries, "deviation:"+c.Deviation.DeviationID)
	}
	for _, version := range c.EvidenceVersions {
		entries = append(entries, fmt.Sprintf("evidence-version:%d", version.Version))
	}
	if c.Evidence != nil {
		entries = append(entries, "blank:"+c.Evidence.BlankSampleID)
	}
	if c.Review != nil {
		entries = append(entries, "reviewer:"+c.Review.ReviewerID)
	}
	seenIssues := map[string]bool{}
	for _, review := range c.ReviewHistory {
		for _, issue := range review.StructuredIssues {
			if !seenIssues[issue.IssueID] {
				entries = append(entries, "issue:"+issue.IssueID)
				seenIssues[issue.IssueID] = true
			}
		}
	}
	if c.Archive != nil && c.Archive.ReleaseAuthorizerID != "" {
		entries = append(entries, "authorization:"+c.Archive.ReleaseAuthorizerID)
	}
	canonical := c.Clone()
	canonical.ArchivedAt = nil
	canonical.Archive = nil
	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(b)
	entries = append(entries, "snapshot:"+hex.EncodeToString(sum[:]))
	return entries
}
