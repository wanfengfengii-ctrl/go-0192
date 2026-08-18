package hsm

import "testing"

func TestOpenSessionUnique(t *testing.T) {
	r := NewRegistry()
	s1 := Session{ID: "s1", Revision: 1, Digest: "d", KeyVersion: "k", Token: "t1"}
	if err := r.OpenSession(s1); err != nil {
		t.Fatalf("first OpenSession() error = %v", err)
	}
	s2 := Session{ID: "s2", Revision: 1, Digest: "d", KeyVersion: "k", Token: "t2"}
	if err := r.OpenSession(s2); err != ErrSessionExists {
		t.Fatalf("second OpenSession() error = %v, want %v", err, ErrSessionExists)
	}
}

func TestTokenSingleUse(t *testing.T) {
	r := NewRegistry()
	s1 := Session{ID: "s1", Revision: 1, Digest: "d", KeyVersion: "k", Token: "tok"}
	if err := r.OpenSession(s1); err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	// A fresh registry sees the same token as already consumed.
	r2 := NewRegistry()
	r2.consumed["tok"] = true
	if err := r2.OpenSession(Session{ID: "s2", Revision: 1, Digest: "d", KeyVersion: "k", Token: "tok"}); err != ErrTokenReused {
		t.Fatalf("reused token error = %v, want %v", err, ErrTokenReused)
	}
}

func TestArtifactAtMostOne(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterArtifact(Artifact{Digest: "d1", ReviewDigest: "r1"}); err != nil {
		t.Fatalf("first RegisterArtifact() error = %v", err)
	}
	if err := r.RegisterArtifact(Artifact{Digest: "d2", ReviewDigest: "r2"}); err != ErrArtifactExists {
		t.Fatalf("second RegisterArtifact() error = %v, want %v", err, ErrArtifactExists)
	}
	if r.ArtifactCount() != 1 {
		t.Fatalf("ArtifactCount() = %d, want 1", r.ArtifactCount())
	}
}

func TestReceiptRequiresSession(t *testing.T) {
	r := NewRegistry()
	if err := r.RecordReceipt("receipt"); err != ErrNoSession {
		t.Fatalf("RecordReceipt() error = %v, want %v", err, ErrNoSession)
	}
}
