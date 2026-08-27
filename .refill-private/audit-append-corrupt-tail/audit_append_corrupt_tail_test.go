package audit_append_corrupt_tail

import (
	"icecoreacclimationgate/internal/audit"
	"os"
	"path/filepath"
	"testing"
)

func TestAuditAppendRejectsCorruptTail(t *testing.T) {
	dir := t.TempDir()
	chain, err := audit.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.Append("CASE-1", "CaseCreated", 1, map[string]string{"state": "Draft"}); err != nil {
		t.Fatal(err)
	}
	auditPath := filepath.Join(dir, "audit.jsonl")
	f, err := os.OpenFile(auditPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"revision":99,"case_id":"CASE-1","type":"CorruptTail","data":{},"prev_digest":"","digest":"bogus"}` + "\n"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := chain.Append("CASE-1", "ProtocolConfigured", 2, map[string]string{"state": "ProtocolReady"}); err == nil {
		t.Fatal("append unexpectedly accepted a corrupt audit tail")
	}
}
