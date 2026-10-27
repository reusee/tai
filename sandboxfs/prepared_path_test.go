package sandboxfs

import (
	"testing"
)

// TestReadDirAndAuditUsePreparedStatements protects
// TheoryOfPreparedStatements: the directory listing and the audit-row insert
// run on the request path, so each has a prepared form.
func TestReadDirAndAuditUsePreparedStatements(t *testing.T) {
	s, _ := newTestStore(t)
	u := sqliteOf(t, s)
	if u.stmts.selectDir == nil {
		t.Error("the directory listing has no prepared form")
	}
	if u.stmts.putOp == nil {
		t.Error("the audit-row insert has no prepared form")
	}
	if err := s.WriteFile("/f", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.audit("touch", "/f", "")
	ops, err := s.Ops()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range ops {
		if o.Op == "touch" && o.Path == "/f" {
			found = true
		}
	}
	if !found {
		t.Errorf("the audit row never reached Ops: %+v", ops)
	}
	entries, err := s.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Error("the listing returned no entries")
	}
}
