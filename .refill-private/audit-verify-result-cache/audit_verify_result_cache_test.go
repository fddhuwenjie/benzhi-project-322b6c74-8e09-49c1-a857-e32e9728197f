package auditverifyresultcache

import (
	"icecoreacclimationgate/internal/audit"
	"os"
	"path/filepath"
	"testing"
)

func TestAuditVerificationRechecksChangedFile(t *testing.T) {
	dir := t.TempDir()
	chain, err := audit.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.Append("CASE-1", "CaseCreated", 1, map[string]any{
		"case_id":  "CASE-1",
		"revision": 1,
		"state":    "Draft",
	}); err != nil {
		t.Fatal(err)
	}
	if err := chain.Verify(); err != nil {
		t.Fatalf("initial audit verification failed: %v", err)
	}

	auditPath := filepath.Join(dir, "audit.jsonl")
	if err := os.WriteFile(auditPath, []byte("{\"revision\":1,\"digest\":\"tampered\"}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := chain.Verify(); err == nil {
		t.Fatal("changed audit file reused the earlier successful verification")
	}
}
