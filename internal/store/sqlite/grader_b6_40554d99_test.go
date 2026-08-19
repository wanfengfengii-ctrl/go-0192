package sqlite_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"quorumforge/internal/api"
	"quorumforge/internal/ceremony"
	"quorumforge/internal/service"
	"quorumforge/internal/store"
	"quorumforge/internal/store/sqlite"
)

func TestGoldB6_40554d99_RestartRecoveryRebuildsParticipantScope(t *testing.T) {
	ctx := context.Background()
	participants := []string{"alice", "bob", "carol"}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ceremony.db")

	st, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	testGoldB6_40554d99_seedLockedCeremony(t, st, "c1", participants)
	if err := st.Close(); err != nil {
		t.Fatalf("close before restart: %v", err)
	}

	reopened, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	t.Run("persistence-rebuilds-both-scope-views", func(t *testing.T) {
		agg, err := reopened.LoadAggregate(ctx, "c1")
		if err != nil {
			t.Fatalf("load aggregate: %v", err)
		}
		gotScope := make([]string, 0, len(agg.Scope))
		for _, entry := range agg.Scope {
			gotScope = append(gotScope, entry.PersonID)
		}
		if !reflect.DeepEqual(gotScope, participants) {
			t.Fatalf("persisted scope = %v, want %v", gotScope, participants)
		}
		if !reflect.DeepEqual(agg.Ceremony.ParticipantScope, participants) {
			t.Fatalf("ceremony participant scope = %v, want %v", agg.Ceremony.ParticipantScope, participants)
		}
	})

	t.Run("snapshot-retains-frozen-participants", func(t *testing.T) {
		agg, err := reopened.LoadAggregate(ctx, "c1")
		if err != nil {
			t.Fatalf("load aggregate: %v", err)
		}
		snapshot, err := agg.Ceremony.Snapshot()
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		if !reflect.DeepEqual(snapshot.ParticipantScope, participants) {
			t.Fatalf("snapshot participants = %v, want %v", snapshot.ParticipantScope, participants)
		}
	})

	t.Run("state-and-revision-survive-recovery", func(t *testing.T) {
		agg, err := reopened.LoadAggregate(ctx, "c1")
		if err != nil {
			t.Fatalf("load aggregate: %v", err)
		}
		if agg.Ceremony.State != ceremony.StateGatheringWitnesses {
			t.Fatalf("state = %v, want %v", agg.Ceremony.State, ceremony.StateGatheringWitnesses)
		}
		if agg.Ceremony.Revision != 7 {
			t.Fatalf("revision = %d, want 7", agg.Ceremony.Revision)
		}
	})

	t.Run("repeated-load-is-idempotent", func(t *testing.T) {
		first, err := reopened.LoadAggregate(ctx, "c1")
		if err != nil {
			t.Fatalf("first load: %v", err)
		}
		second, err := reopened.LoadAggregate(ctx, "c1")
		if err != nil {
			t.Fatalf("second load: %v", err)
		}
		if !reflect.DeepEqual(first.Ceremony.ParticipantScope, second.Ceremony.ParticipantScope) ||
			!reflect.DeepEqual(first.Scope, second.Scope) {
			t.Fatalf("repeated recovery changed scope: first=%v/%v second=%v/%v", first.Ceremony.ParticipantScope, first.Scope, second.Ceremony.ParticipantScope, second.Scope)
		}
	})

	t.Run("single-participant-boundary", func(t *testing.T) {
		boundaryPath := testGoldB6_40554d99_seedRestartDatabase(t, []string{"alice"})
		boundaryStore, err := sqlite.Open(boundaryPath)
		if err != nil {
			t.Fatalf("reopen boundary database: %v", err)
		}
		defer boundaryStore.Close()
		agg, err := boundaryStore.LoadAggregate(ctx, "c1")
		if err != nil {
			t.Fatalf("load boundary aggregate: %v", err)
		}
		if !reflect.DeepEqual(agg.Ceremony.ParticipantScope, []string{"alice"}) {
			t.Fatalf("boundary participant scope = %v, want [alice]", agg.Ceremony.ParticipantScope)
		}
	})

	t.Run("service-boundary-exposes-restored-scope", func(t *testing.T) {
		view, err := service.NewService(reopened).Get(ctx, "c1")
		if err != nil {
			t.Fatalf("service get: %v", err)
		}
		if !reflect.DeepEqual(view.Ceremony.ParticipantScope, participants) {
			t.Fatalf("service ceremony scope = %v, want %v", view.Ceremony.ParticipantScope, participants)
		}
	})

	t.Run("http-boundary-retains-normal-participant-view", func(t *testing.T) {
		handler := api.New(service.NewService(reopened), ":0").Handler()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/ceremonies/c1", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("http status = %d, want %d", response.Code, http.StatusOK)
		}
		var body struct {
			Participants []string `json:"participants"`
		}
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatalf("decode http response: %v", err)
		}
		if !reflect.DeepEqual(body.Participants, participants) {
			t.Fatalf("http participants = %v, want %v", body.Participants, participants)
		}
	})

	t.Run("empty-snapshot-input-remains-rejected", func(t *testing.T) {
		pending := ceremony.Ceremony{ID: "pending", State: ceremony.StatePendingLock}
		if _, err := pending.Snapshot(); err == nil {
			t.Fatal("snapshot before lock succeeded, want error")
		}
	})

}

func testGoldB6_40554d99_seedLockedCeremony(t *testing.T, st *sqlite.Store, id ceremony.ID, participants []string) {
	t.Helper()
	ctx := context.Background()
	c := ceremony.Ceremony{
		ID: id, State: ceremony.StateGatheringWitnesses, Revision: 7,
		RequestDigest: "digest-1", KeyVersion: "key-v1", PolicyVersion: "policy-v1",
		ParticipantScope: append([]string(nil), participants...),
	}
	if err := st.CreateCeremony(ctx, c); err != nil {
		t.Fatalf("create ceremony: %v", err)
	}
	entries := make([]store.ScopeEntry, 0, len(participants))
	for _, personID := range participants {
		entries = append(entries, store.ScopeEntry{PersonID: personID, Role: "officer", Revision: 1})
	}
	if err := st.Within(ctx, func(tx store.Tx) error { return tx.SaveScope(id, entries) }); err != nil {
		t.Fatalf("save scope: %v", err)
	}
}

func testGoldB6_40554d99_seedRestartDatabase(t *testing.T, participants []string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "boundary.db")
	st, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open boundary database: %v", err)
	}
	testGoldB6_40554d99_seedLockedCeremony(t, st, "c1", participants)
	if err := st.Close(); err != nil {
		t.Fatalf("close boundary database: %v", err)
	}
	return dbPath
}
