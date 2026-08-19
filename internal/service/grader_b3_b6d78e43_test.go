package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"quorumforge/internal/api"
	"quorumforge/internal/ceremony"
	"quorumforge/internal/policy"
	"quorumforge/internal/service"
	"quorumforge/internal/store"
	"quorumforge/internal/store/sqlite"
)

func TestGoldB3_b6d78e43_RegisterArtifactBinding(t *testing.T) {
	ctx := context.Background()

	TestGoldB3_b6d78e43_newReadyService := func(t *testing.T, databasePath string) (*service.Service, *sqlite.Store, ceremony.Revision) {
		t.Helper()
		st, err := sqlite.Open(databasePath)
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		t.Cleanup(func() { _ = st.Close() })

		if err := st.UpsertPolicy(ctx, policy.Policy{
			Version:           "policy-v1",
			Threshold:         1,
			AllowedRoles:      []policy.Role{"officer"},
			AllowedKeyVersion: "key-v1",
		}); err != nil {
			t.Fatalf("seed policy: %v", err)
		}
		if err := st.UpsertParticipant(ctx, policy.Participant{
			PersonID:    "alice",
			Credentials: []string{"alice-cred"},
			Role:        "officer",
			Revision:    1,
		}); err != nil {
			t.Fatalf("seed participant: %v", err)
		}

		svc := service.NewService(st)
		if _, err := svc.Create(ctx, service.CreateRequest{ID: "ceremony-1"}); err != nil {
			t.Fatalf("create ceremony: %v", err)
		}
		lock, err := svc.Lock(ctx, service.LockRequest{
			CeremonyID:    "ceremony-1",
			Operation:     "lock",
			Revision:      0,
			Digest:        "digest-1",
			KeyVersion:    "key-v1",
			PolicyVersion: "policy-v1",
			Participants:  []string{"alice"},
		})
		if err != nil {
			t.Fatalf("lock ceremony: %v", err)
		}
		witness, err := svc.ConfirmWitness(ctx, service.ConfirmRequest{
			CeremonyID:       "ceremony-1",
			Operation:        "witness",
			Revision:         lock.Revision,
			PersonID:         "alice",
			Credential:       "alice-cred",
			IdentityRevision: 1,
		})
		if err != nil {
			t.Fatalf("confirm witness: %v", err)
		}
		begin, err := svc.BeginSignature(ctx, service.BeginRequest{
			CeremonyID: "ceremony-1",
			Operation:  "begin",
			Revision:   witness.Revision,
			Token:      "token-1",
			SessionID:  "session-1",
		})
		if err != nil {
			t.Fatalf("begin signature: %v", err)
		}
		receipt, err := svc.RecordReceipt(ctx, service.ReceiptRequest{
			CeremonyID: "ceremony-1",
			Operation:  "receipt",
			Revision:   begin.Revision,
			SessionID:  begin.SessionID,
			Receipt:    "receipt-1",
		})
		if err != nil {
			t.Fatalf("record receipt: %v", err)
		}
		return svc, st, receipt.Revision
	}

	TestGoldB3_b6d78e43_serveJSON := func(t *testing.T, handler http.Handler, method, path string, body any) (int, map[string]any) {
		t.Helper()
		var payload bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&payload).Encode(body); err != nil {
				t.Fatalf("encode request: %v", err)
			}
		}
		req := httptest.NewRequest(method, path, &payload)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		var decoded map[string]any
		if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return response.Code, decoded
	}

	t.Run("service rejects an artifact digest unrelated to the frozen request", func(t *testing.T) {
		svc, _, revision := TestGoldB3_b6d78e43_newReadyService(t, "")
		request := service.ArtifactRequest{
			CeremonyID: "ceremony-1",
			Operation:  "artifact",
			Revision:   revision,
			Digest:     "art-1",
		}
		if _, err := svc.RegisterArtifact(ctx, request); !errors.Is(err, service.ErrInvalidArgument) {
			t.Fatalf("RegisterArtifact() error = %v, want ErrInvalidArgument", err)
		}

		view, err := svc.Get(ctx, "ceremony-1")
		if err != nil {
			t.Fatalf("get rejected ceremony: %v", err)
		}
		if view.Ceremony.State != ceremony.StatePendingSignature || view.Ceremony.Revision != revision {
			t.Fatalf("rejected registration changed ceremony to state=%s revision=%d, want pending-signature revision=%d", view.Ceremony.State, view.Ceremony.Revision, revision)
		}
		if view.Artifact != nil {
			t.Fatalf("rejected registration persisted artifact %+v", view.Artifact)
		}

		request.Digest = "digest-1"
		result, err := svc.RegisterArtifact(ctx, request)
		if err != nil {
			t.Fatalf("retry same operation with bound digest: %v", err)
		}
		if result.Digest != "digest-1" || result.State != ceremony.StatePendingReview || result.Revision != revision+1 {
			t.Fatalf("bound retry result = %+v, want digest-1 pending-review revision=%d", result, revision+1)
		}
	})

	t.Run("empty digest remains an invalid boundary input", func(t *testing.T) {
		svc, _, revision := TestGoldB3_b6d78e43_newReadyService(t, "")
		_, err := svc.RegisterArtifact(ctx, service.ArtifactRequest{
			CeremonyID: "ceremony-1",
			Operation:  "empty-artifact",
			Revision:   revision,
		})
		if !errors.Is(err, service.ErrInvalidArgument) {
			t.Fatalf("RegisterArtifact() error = %v, want ErrInvalidArgument", err)
		}
		view, err := svc.Get(ctx, "ceremony-1")
		if err != nil {
			t.Fatalf("get ceremony: %v", err)
		}
		if view.Ceremony.Revision != revision || view.Artifact != nil {
			t.Fatalf("empty digest changed persisted view: revision=%d artifact=%+v", view.Ceremony.Revision, view.Artifact)
		}
	})

	t.Run("persisted HSM session must match the frozen digest and key version", func(t *testing.T) {
		for _, testCase := range []struct {
			name       string
			digest     string
			keyVersion string
		}{
			{name: "session digest mismatch", digest: "digest-other", keyVersion: "key-v1"},
			{name: "session key version mismatch", digest: "digest-1", keyVersion: "key-other"},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				st, err := sqlite.Open("")
				if err != nil {
					t.Fatalf("open store: %v", err)
				}
				t.Cleanup(func() { _ = st.Close() })
				const revision = ceremony.Revision(7)
				if err := st.CreateCeremony(ctx, ceremony.Ceremony{
					ID:               "ceremony-1",
					State:            ceremony.StatePendingSignature,
					Revision:         revision,
					RequestDigest:    "digest-1",
					KeyVersion:       "key-v1",
					PolicyVersion:    "policy-v1",
					ReviewConclusion: ceremony.ReviewPending,
				}); err != nil {
					t.Fatalf("seed ceremony: %v", err)
				}
				if err := st.Within(ctx, func(tx store.Tx) error {
					return tx.SaveSession("ceremony-1", store.SessionRecord{
						ID:         "session-1",
						Revision:   5,
						Digest:     testCase.digest,
						KeyVersion: testCase.keyVersion,
						Receipt:    "receipt-1",
					})
				}); err != nil {
					t.Fatalf("seed session: %v", err)
				}

				svc := service.NewService(st)
				_, err = svc.RegisterArtifact(ctx, service.ArtifactRequest{
					CeremonyID: "ceremony-1",
					Operation:  "artifact",
					Revision:   revision,
					Digest:     "digest-1",
				})
				if !errors.Is(err, service.ErrInvalidArgument) {
					t.Fatalf("RegisterArtifact() error = %v, want ErrInvalidArgument", err)
				}
				view, err := svc.Get(ctx, "ceremony-1")
				if err != nil {
					t.Fatalf("get ceremony: %v", err)
				}
				if view.Ceremony.State != ceremony.StatePendingSignature || view.Ceremony.Revision != revision || view.Artifact != nil {
					t.Fatalf("mismatched session changed view: state=%s revision=%d artifact=%+v", view.Ceremony.State, view.Ceremony.Revision, view.Artifact)
				}
			})
		}
	})

	t.Run("rejection survives store reopen without an artifact or revision change", func(t *testing.T) {
		databasePath := filepath.Join(t.TempDir(), "quorumforge.db")
		svc, st, revision := TestGoldB3_b6d78e43_newReadyService(t, databasePath)
		_, err := svc.RegisterArtifact(ctx, service.ArtifactRequest{
			CeremonyID: "ceremony-1",
			Operation:  "artifact",
			Revision:   revision,
			Digest:     "art-1",
		})
		if !errors.Is(err, service.ErrInvalidArgument) {
			t.Fatalf("RegisterArtifact() error = %v, want ErrInvalidArgument", err)
		}
		if err := st.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}

		reopened, err := sqlite.Open(databasePath)
		if err != nil {
			t.Fatalf("reopen store: %v", err)
		}
		t.Cleanup(func() { _ = reopened.Close() })
		view, err := service.NewService(reopened).Get(ctx, "ceremony-1")
		if err != nil {
			t.Fatalf("get reopened ceremony: %v", err)
		}
		if view.Ceremony.State != ceremony.StatePendingSignature || view.Ceremony.Revision != revision || view.Artifact != nil {
			t.Fatalf("reopened view after rejection: state=%s revision=%d artifact=%+v", view.Ceremony.State, view.Ceremony.Revision, view.Artifact)
		}
	})

	t.Run("bound registration is idempotent and conflicting replay is rejected", func(t *testing.T) {
		svc, _, revision := TestGoldB3_b6d78e43_newReadyService(t, "")
		request := service.ArtifactRequest{
			CeremonyID: "ceremony-1",
			Operation:  "artifact",
			Revision:   revision,
			Digest:     "digest-1",
		}
		first, err := svc.RegisterArtifact(ctx, request)
		if err != nil {
			t.Fatalf("register bound artifact: %v", err)
		}
		replay, err := svc.RegisterArtifact(ctx, request)
		if err != nil {
			t.Fatalf("replay bound artifact: %v", err)
		}
		if replay != first {
			t.Fatalf("idempotent replay = %+v, want %+v", replay, first)
		}

		request.Digest = "art-1"
		if _, err := svc.RegisterArtifact(ctx, request); !errors.Is(err, service.ErrOperationConflict) {
			t.Fatalf("conflicting replay error = %v, want ErrOperationConflict", err)
		}
		view, err := svc.Get(ctx, "ceremony-1")
		if err != nil {
			t.Fatalf("get ceremony: %v", err)
		}
		if view.Ceremony.Revision != first.Revision || view.Artifact == nil || view.Artifact.Digest != "digest-1" {
			t.Fatalf("replay changed persisted view: revision=%d artifact=%+v", view.Ceremony.Revision, view.Artifact)
		}
	})

	t.Run("HTTP boundary returns bad request and preserves the public view", func(t *testing.T) {
		svc, _, revision := TestGoldB3_b6d78e43_newReadyService(t, "")
		handler := api.New(svc, ":0").Handler()

		status, response := TestGoldB3_b6d78e43_serveJSON(t, handler, http.MethodPost,
			"/api/v1/ceremonies/ceremony-1/artifact", map[string]any{
				"operation": "artifact",
				"revision":  revision,
				"digest":    "art-1",
			})
		if status != http.StatusBadRequest {
			t.Fatalf("artifact response status=%d body=%v, want 400", status, response)
		}
		if response["error"] == nil || response["error"] == "" {
			t.Fatalf("artifact response body=%v, want stable error message", response)
		}

		status, response = TestGoldB3_b6d78e43_serveJSON(t, handler, http.MethodGet,
			"/api/v1/ceremonies/ceremony-1", nil)
		if status != http.StatusOK {
			t.Fatalf("view response status=%d body=%v, want 200", status, response)
		}
		if response["state"] != "pending-signature" || response["revision"] != float64(revision) || response["artifact"] != nil {
			t.Fatalf("public view changed after HTTP rejection: %v", response)
		}
	})

	t.Run("matching digest retains review and seal normal path", func(t *testing.T) {
		svc, _, revision := TestGoldB3_b6d78e43_newReadyService(t, "")
		artifact, err := svc.RegisterArtifact(ctx, service.ArtifactRequest{
			CeremonyID: "ceremony-1",
			Operation:  "artifact",
			Revision:   revision,
			Digest:     "digest-1",
		})
		if err != nil {
			t.Fatalf("register bound artifact: %v", err)
		}
		review, err := svc.Review(ctx, service.ReviewRequest{
			CeremonyID:   "ceremony-1",
			Operation:    "review",
			Revision:     artifact.Revision,
			Conclusion:   ceremony.ReviewApproved,
			ReviewDigest: "review-1",
		})
		if err != nil {
			t.Fatalf("review bound artifact: %v", err)
		}
		sealed, err := svc.Seal(ctx, service.SealRequest{
			CeremonyID: "ceremony-1",
			Operation:  "seal",
			Revision:   review.Revision,
		})
		if err != nil {
			t.Fatalf("seal bound artifact: %v", err)
		}
		if sealed.State != ceremony.StateSealed {
			t.Fatalf("seal state=%s, want sealed", sealed.State)
		}
		view, err := svc.Get(ctx, "ceremony-1")
		if err != nil {
			t.Fatalf("get sealed ceremony: %v", err)
		}
		if view.Artifact == nil || view.Artifact.Digest != "digest-1" || view.Ceremony.State != ceremony.StateSealed {
			t.Fatalf("sealed public view = %+v", view)
		}
	})
}
