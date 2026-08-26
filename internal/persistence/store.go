package persistence

import (
	"encoding/json"
	"icecoreacclimationgate/internal/domain"
	"os"
	"path/filepath"
	"sync"
)

type RequestRecord struct {
	RequestID   string          `json:"request_id"`
	Fingerprint string          `json:"fingerprint"`
	Status      int             `json:"status"`
	Body        json.RawMessage `json:"body"`
}
type Store struct {
	dir      string
	mu       sync.RWMutex
	cases    map[string]domain.AcclimationCase
	requests map[string]RequestRecord
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, cases: map[string]domain.AcclimationCase{}, requests: map[string]RequestRecord{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) load() error {
	b, err := os.ReadFile(filepath.Join(s.dir, "cases.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if e := json.Unmarshal(b, &s.cases); e != nil {
		return e
	}
	rb, e := os.ReadFile(filepath.Join(s.dir, "requests.json"))
	if e == nil {
		s.requests = decodeRequestRecords(rb)
	}
	return nil
}

func decodeRequestRecords(data []byte) map[string]RequestRecord {
	records := map[string]RequestRecord{}
	if err := json.Unmarshal(data, &records); err != nil {
		return map[string]RequestRecord{}
	}
	for key, record := range records {
		var response domain.AcclimationCase
		if record.RequestID != key || record.Fingerprint == "" || json.Unmarshal(record.Body, &response) != nil || response.CaseID == "" {
			delete(records, key)
		}
	}
	return records
}
func (s *Store) Get(id string) (domain.AcclimationCase, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.cases[id]
	if !ok {
		return c, false
	}
	return c.Clone(), true
}
func (s *Store) List() []domain.AcclimationCase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.AcclimationCase, 0, len(s.cases))
	for _, c := range s.cases {
		out = append(out, c.Clone())
	}
	return out
}
func (s *Store) SaveMutation(c domain.AcclimationCase, request *RequestRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldCase, caseExisted := s.cases[c.CaseID]
	s.cases[c.CaseID] = c.Clone()
	var oldRequest RequestRecord
	var requestExisted bool
	if request != nil {
		oldRequest, requestExisted = s.requests[request.RequestID]
		s.requests[request.RequestID] = *request
	}
	rollback := func() {
		if caseExisted {
			s.cases[c.CaseID] = oldCase
		} else {
			delete(s.cases, c.CaseID)
		}
		if request != nil {
			if requestExisted {
				s.requests[request.RequestID] = oldRequest
			} else {
				delete(s.requests, request.RequestID)
			}
		}
	}
	if err := s.flushLocked(); err != nil {
		rollback()
		return err
	}
	if request != nil {
		if err := s.flushRequestsLocked(); err != nil {
			rollback()
			_ = s.flushLocked()
			return err
		}
	}
	return nil
}
func (s *Store) flushLocked() error {
	b, e := json.MarshalIndent(s.cases, "", "  ")
	if e != nil {
		return e
	}
	tmp := filepath.Join(s.dir, "cases.json.tmp")
	f, e := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	_ = f.Close()
	if e != nil {
		return e
	}
	if e = os.Rename(tmp, filepath.Join(s.dir, "cases.json")); e != nil {
		return e
	}
	return nil
}
func (s *Store) GetRequest(id string) (RequestRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.requests[id]
	return r, ok
}
func (s *Store) flushRequestsLocked() error {
	b, e := json.Marshal(s.requests)
	if e != nil {
		return e
	}
	tmp := filepath.Join(s.dir, "requests.json.tmp")
	f, e := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	_ = f.Close()
	if e != nil {
		return e
	}
	return os.Rename(tmp, filepath.Join(s.dir, "requests.json"))
}
