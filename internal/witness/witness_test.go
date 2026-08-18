package witness

import "testing"

func TestRecordDeduplicatesByPerson(t *testing.T) {
	l := NewLedger()
	if err := l.Record(Confirmation{PersonID: "alice", Credential: "cred-a", Revision: 1}); err != nil {
		t.Fatalf("first Record() error = %v", err)
	}
	// Same person, different credential: still a duplicate.
	if err := l.Record(Confirmation{PersonID: "alice", Credential: "cred-b", Revision: 2}); err != ErrDuplicateWitness {
		t.Fatalf("second Record() error = %v, want %v", err, ErrDuplicateWitness)
	}
	if l.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", l.Count())
	}
}

func TestRecordRejectsStaleConfirmation(t *testing.T) {
	l := NewLedger()
	if err := l.Record(Confirmation{PersonID: "alice", Credential: "cred-a", Revision: 0}); err != ErrStaleConfirmation {
		t.Fatalf("Record() error = %v, want %v", err, ErrStaleConfirmation)
	}
	if l.Count() != 0 {
		t.Fatalf("Count() = %d, want 0 after stale rejection", l.Count())
	}
}

func TestQuorumReachedAndSorted(t *testing.T) {
	l := NewLedger()
	for _, c := range []Confirmation{
		{PersonID: "carol", Credential: "c", Revision: 1},
		{PersonID: "alice", Credential: "a", Revision: 1},
		{PersonID: "bob", Credential: "b", Revision: 1},
	} {
		if err := l.Record(c); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}
	if !l.Reached(3) {
		t.Fatal("Reached(3) = false, want true")
	}
	if l.Reached(4) {
		t.Fatal("Reached(4) = true, want false")
	}
	q := l.Quorum()
	if len(q) != 3 {
		t.Fatalf("Quorum() len = %d, want 3", len(q))
	}
	if q[0].PersonID != "alice" || q[1].PersonID != "bob" || q[2].PersonID != "carol" {
		t.Fatalf("Quorum() not stable-sorted: %v", q)
	}
}
