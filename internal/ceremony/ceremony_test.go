package ceremony

import "testing"

func TestLockFreezesSnapshot(t *testing.T) {
	c := Ceremony{ID: "c1"}
	participants := []string{"p2", "p1"}
	if err := c.Lock("digest-abc", "key-v1", "policy-v2", participants); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	if c.State != StateGatheringWitnesses {
		t.Fatalf("State = %s, want %s", c.State, StateGatheringWitnesses)
	}
	if c.RequestDigest != "digest-abc" || c.KeyVersion != "key-v1" || c.PolicyVersion != "policy-v2" {
		t.Fatalf("snapshot fields not frozen: %+v", c)
	}
	if len(c.ParticipantScope) != 2 {
		t.Fatalf("ParticipantScope len = %d, want 2", len(c.ParticipantScope))
	}
	// Mutating the caller's slice must not affect the frozen scope.
	participants[0] = "changed"
	if c.ParticipantScope[0] != "p2" {
		t.Fatalf("participant scope was aliased, got %q", c.ParticipantScope[0])
	}
}

func TestLockRejectsInvalidSnapshot(t *testing.T) {
	c := Ceremony{ID: "c1"}
	if err := c.Lock("", "key-v1", "policy-v2", []string{"p1"}); err == nil {
		t.Fatal("Lock() with empty digest succeeded, want error")
	}
	if err := c.Lock("d", "k", "p", nil); err == nil {
		t.Fatal("Lock() with empty participant scope succeeded, want error")
	}
}

func TestLockNotRepeatable(t *testing.T) {
	c := Ceremony{ID: "c1"}
	if err := c.Lock("d", "k", "p", []string{"p1"}); err != nil {
		t.Fatalf("first Lock() error = %v", err)
	}
	if err := c.Lock("d2", "k2", "p2", []string{"p2"}); err == nil {
		t.Fatal("second Lock() succeeded, want error")
	}
}

func TestSnapshotBeforeLockFails(t *testing.T) {
	c := Ceremony{ID: "c1"}
	if _, err := c.Snapshot(); err == nil {
		t.Fatal("Snapshot() before Lock succeeded, want error")
	}
}
