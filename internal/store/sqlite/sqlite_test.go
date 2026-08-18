package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"quorumforge/internal/ceremony"
	"quorumforge/internal/store"
	"quorumforge/internal/store/sqlite"
)

func createLockedCeremony(t *testing.T, st *sqlite.Store, id ceremony.ID) {
	t.Helper()
	ctx := context.Background()
	c := ceremony.Ceremony{
		ID: id, State: ceremony.StateGatheringWitnesses, Revision: 1,
		RequestDigest: "digest-1", KeyVersion: "key-v1", PolicyVersion: "policy-v1",
		ParticipantScope: []string{"alice", "bob", "carol"},
	}
	if err := st.CreateCeremony(ctx, c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.Within(ctx, func(tx store.Tx) error {
		return tx.SaveScope(id, []store.ScopeEntry{
			{PersonID: "alice", Role: "officer", Revision: 1},
			{PersonID: "bob", Role: "officer", Revision: 1},
			{PersonID: "carol", Role: "officer", Revision: 1},
		})
	}); err != nil {
		t.Fatalf("save scope: %v", err)
	}
}

func TestRestartRecoveryRebuildsAggregate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ceremony.db")
	ctx := context.Background()

	st, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	createLockedCeremony(t, st, "c1")

	// Commit a witness, session and artifact through transactions.
	if err := st.Within(ctx, func(tx store.Tx) error {
		return tx.SaveWitness("c1", store.WitnessRecord{PersonID: "alice", Credential: "alice-cred", Revision: 1})
	}); err != nil {
		t.Fatalf("witness: %v", err)
	}
	if err := st.Within(ctx, func(tx store.Tx) error {
		if err := tx.SaveSession("c1", store.SessionRecord{ID: "s-1", Revision: 1, Digest: "digest-1", KeyVersion: "key-v1", Receipt: "r-1"}); err != nil {
			return err
		}
		return tx.SaveArtifact("c1", store.ArtifactRecord{Digest: "art-1", ReviewDigest: "rd-1"})
	}); err != nil {
		t.Fatalf("session+artifact: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen and reconstruct.
	st2, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()

	agg, err := st2.LoadAggregate(ctx, "c1")
	if err != nil {
		t.Fatalf("load aggregate: %v", err)
	}
	if agg.Ceremony.ID != "c1" || agg.Ceremony.State != ceremony.StateGatheringWitnesses {
		t.Fatalf("ceremony not recovered: %+v", agg.Ceremony)
	}
	if len(agg.Scope) != 3 {
		t.Fatalf("scope len=%d, want 3", len(agg.Scope))
	}
	if len(agg.Witnesses) != 1 || agg.Witnesses[0].PersonID != "alice" {
		t.Fatalf("witnesses=%+v", agg.Witnesses)
	}
	if agg.Session == nil || agg.Session.ID != "s-1" || agg.Session.Receipt != "r-1" {
		t.Fatalf("session=%+v", agg.Session)
	}
	if agg.Artifact == nil || agg.Artifact.Digest != "art-1" {
		t.Fatalf("artifact=%+v", agg.Artifact)
	}
}

func TestFaultRollsBackWitness(t *testing.T) {
	st, err := sqlite.Open("")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	createLockedCeremony(t, st, "c1")

	st.InjectFault(sqlite.FaultAfterSaveWitness, sqlite.ErrInjectedFault)
	err = st.Within(ctx, func(tx store.Tx) error {
		return tx.SaveWitness("c1", store.WitnessRecord{PersonID: "alice", Credential: "alice-cred", Revision: 1})
	})
	if !errors.Is(err, sqlite.ErrInjectedFault) {
		t.Fatalf("err=%v, want injected fault", err)
	}

	agg, err := st.LoadAggregate(ctx, "c1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(agg.Witnesses) != 0 {
		t.Fatalf("orphan witness left after rollback: %+v", agg.Witnesses)
	}
}

func TestFaultRollsBackSessionAndArtifact(t *testing.T) {
	st, err := sqlite.Open("")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	createLockedCeremony(t, st, "c1")

	st.InjectFault(sqlite.FaultAfterSaveArtifact, sqlite.ErrInjectedFault)
	err = st.Within(ctx, func(tx store.Tx) error {
		if err := tx.SaveSession("c1", store.SessionRecord{ID: "s-1", Revision: 1, Digest: "d", KeyVersion: "k"}); err != nil {
			return err
		}
		return tx.SaveArtifact("c1", store.ArtifactRecord{Digest: "a"})
	})
	if !errors.Is(err, sqlite.ErrInjectedFault) {
		t.Fatalf("err=%v, want injected fault", err)
	}

	agg, _ := st.LoadAggregate(ctx, "c1")
	if agg.Session != nil {
		t.Fatalf("orphan session left after rollback: %+v", agg.Session)
	}
	if agg.Artifact != nil {
		t.Fatalf("orphan artifact left after rollback: %+v", agg.Artifact)
	}
}

func TestTokenGloballySingleUse(t *testing.T) {
	st, err := sqlite.Open("")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	createLockedCeremony(t, st, "c1")
	createLockedCeremony(t, st, "c2")

	// Consume the token in c1.
	if err := st.Within(ctx, func(tx store.Tx) error {
		consumed, err := tx.ConsumeToken(ctx, "c1", "shared-token")
		if err != nil {
			return err
		}
		if consumed {
			t.Fatal("first consumption reported already consumed")
		}
		return nil
	}); err != nil {
		t.Fatalf("consume: %v", err)
	}

	// The same token must be rejected for c2.
	if err := st.Within(ctx, func(tx store.Tx) error {
		consumed, err := tx.ConsumeToken(ctx, "c2", "shared-token")
		if err != nil {
			return err
		}
		if !consumed {
			t.Fatal("second consumption not detected as reused")
		}
		return nil
	}); err != nil {
		t.Fatalf("consume reuse: %v", err)
	}
}

func TestUniqueSessionConstraint(t *testing.T) {
	st, err := sqlite.Open("")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	createLockedCeremony(t, st, "c1")

	if err := st.Within(ctx, func(tx store.Tx) error {
		return tx.SaveSession("c1", store.SessionRecord{ID: "s-1", Revision: 1, Digest: "d", KeyVersion: "k"})
	}); err != nil {
		t.Fatalf("first session: %v", err)
	}
	// A second session for the same ceremony must violate the unique index.
	err = st.Within(ctx, func(tx store.Tx) error {
		return tx.SaveSession("c1", store.SessionRecord{ID: "s-2", Revision: 1, Digest: "d", KeyVersion: "k"})
	})
	if err == nil {
		t.Fatal("second session succeeded, want unique constraint error")
	}
}
