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
	"quorumforge/internal/store/sqlite"
)

func TestGoldB4_8a1243f1_ReviewConclusionIsWriteOnce(t *testing.T) {
	t.Run("second operation is rejected without mutation", func(t *testing.T) {
		svc, st := goldB4_8a1243f1_openService(t, "")
		defer st.Close()
		view := goldB4_8a1243f1_advanceToPendingReview(t, svc, "second-operation")

		first, err := svc.Review(context.Background(), service.ReviewRequest{
			CeremonyID: "second-operation", Operation: "review-rejected", Revision: view.Ceremony.Revision,
			Conclusion: ceremony.ReviewRejected, ReviewDigest: "digest-rejected",
		})
		if err != nil {
			t.Fatalf("first review: %v", err)
		}
		before, err := svc.Get(context.Background(), "second-operation")
		if err != nil {
			t.Fatalf("get after first review: %v", err)
		}
		if _, err := svc.Seal(context.Background(), service.SealRequest{
			CeremonyID: "second-operation", Operation: "seal-rejected", Revision: before.Ceremony.Revision,
		}); err == nil {
			t.Fatal("seal with rejected review succeeded, want rejection")
		}

		_, err = svc.Review(context.Background(), service.ReviewRequest{
			CeremonyID: "second-operation", Operation: "review-approved", Revision: first.Revision,
			Conclusion: ceremony.ReviewApproved, ReviewDigest: "digest-approved",
		})
		if err == nil {
			t.Fatal("second review succeeded, want rejection")
		}

		after, err := svc.Get(context.Background(), "second-operation")
		if err != nil {
			t.Fatalf("get after rejected second review: %v", err)
		}
		if after.Ceremony.State != ceremony.StatePendingReview {
			t.Fatalf("state=%s, want pending-review", after.Ceremony.State)
		}
		if after.Ceremony.Revision != before.Ceremony.Revision {
			t.Fatalf("revision=%d, want unchanged %d", after.Ceremony.Revision, before.Ceremony.Revision)
		}
		if after.Ceremony.ReviewConclusion != ceremony.ReviewRejected || after.Ceremony.ReviewDigest != "digest-rejected" {
			t.Fatalf("review=%q digest=%q, want rejected/digest-rejected", after.Ceremony.ReviewConclusion, after.Ceremony.ReviewDigest)
		}
		if after.Artifact == nil || after.Artifact.ReviewDigest != "digest-rejected" {
			t.Fatalf("artifact=%+v, want persisted rejected digest", after.Artifact)
		}
		if _, err := svc.Seal(context.Background(), service.SealRequest{
			CeremonyID: "second-operation", Operation: "seal-after-review-retry", Revision: after.Ceremony.Revision,
		}); err == nil {
			t.Fatal("seal after rejected review retry succeeded, want rejection")
		}
	})

	t.Run("same operation replays idempotently", func(t *testing.T) {
		svc, st := goldB4_8a1243f1_openService(t, "")
		defer st.Close()
		view := goldB4_8a1243f1_advanceToPendingReview(t, svc, "idempotent")
		req := service.ReviewRequest{
			CeremonyID: "idempotent", Operation: "review-once", Revision: view.Ceremony.Revision,
			Conclusion: ceremony.ReviewRejected, ReviewDigest: "digest-once",
		}
		first, err := svc.Review(context.Background(), req)
		if err != nil {
			t.Fatalf("first review: %v", err)
		}
		replay, err := svc.Review(context.Background(), req)
		if err != nil {
			t.Fatalf("idempotent replay: %v", err)
		}
		if replay != first {
			t.Fatalf("replay=%+v, first=%+v, want identical result", replay, first)
		}
		current, err := svc.Get(context.Background(), "idempotent")
		if err != nil {
			t.Fatalf("get after replay: %v", err)
		}
		if current.Ceremony.Revision != first.Revision || current.Ceremony.ReviewConclusion != ceremony.ReviewRejected {
			t.Fatalf("view after replay=%+v, want one committed review", current.Ceremony)
		}
	})

	t.Run("pending conclusion boundary is rejected before persistence", func(t *testing.T) {
		svc, st := goldB4_8a1243f1_openService(t, "")
		defer st.Close()
		view := goldB4_8a1243f1_advanceToPendingReview(t, svc, "pending-boundary")

		_, err := svc.Review(context.Background(), service.ReviewRequest{
			CeremonyID: "pending-boundary", Operation: "review-pending", Revision: view.Ceremony.Revision,
			Conclusion: ceremony.ReviewPending, ReviewDigest: "should-not-write",
		})
		if !errors.Is(err, service.ErrInvalidArgument) {
			t.Fatalf("pending conclusion error=%v, want ErrInvalidArgument", err)
		}
		after, err := svc.Get(context.Background(), "pending-boundary")
		if err != nil {
			t.Fatalf("get after pending boundary: %v", err)
		}
		if after.Ceremony.Revision != view.Ceremony.Revision || after.Ceremony.ReviewConclusion != ceremony.ReviewPending || after.Ceremony.ReviewDigest != "" {
			t.Fatalf("boundary mutated ceremony=%+v", after.Ceremony)
		}
		if after.Artifact == nil || after.Artifact.ReviewDigest != "" {
			t.Fatalf("boundary mutated artifact=%+v", after.Artifact)
		}
	})

	t.Run("HTTP boundary rejects overwrite with conflict", func(t *testing.T) {
		svc, st := goldB4_8a1243f1_openService(t, "")
		defer st.Close()
		view := goldB4_8a1243f1_advanceToPendingReview(t, svc, "http-boundary")
		handler := api.New(svc, ":0").Handler()

		status, _ := goldB4_8a1243f1_postJSON(t, handler, "/api/v1/ceremonies/http-boundary/review", map[string]any{
			"operation": "http-rejected", "revision": view.Ceremony.Revision,
			"conclusion": "rejected", "review_digest": "http-rejected-digest",
		})
		if status != http.StatusOK {
			t.Fatalf("first HTTP review status=%d, want 200", status)
		}
		first, err := svc.Get(context.Background(), "http-boundary")
		if err != nil {
			t.Fatalf("get after HTTP review: %v", err)
		}
		status, body := goldB4_8a1243f1_postJSON(t, handler, "/api/v1/ceremonies/http-boundary/review", map[string]any{
			"operation": "http-approved", "revision": first.Ceremony.Revision,
			"conclusion": "approved", "review_digest": "http-approved-digest",
		})
		if status != http.StatusConflict {
			t.Fatalf("second HTTP review status=%d body=%s, want 409", status, body)
		}
		final, err := svc.Get(context.Background(), "http-boundary")
		if err != nil {
			t.Fatalf("get after HTTP rejection: %v", err)
		}
		if final.Ceremony.ReviewConclusion != ceremony.ReviewRejected || final.Ceremony.ReviewDigest != "http-rejected-digest" {
			t.Fatalf("HTTP overwrite changed review=%q digest=%q", final.Ceremony.ReviewConclusion, final.Ceremony.ReviewDigest)
		}
	})

	t.Run("rejected conclusion survives store reopen", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "review.db")
		st, err := sqlite.Open(path)
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		goldB4_8a1243f1_seedDirectory(t, st)
		svc := service.NewService(st)
		view := goldB4_8a1243f1_advanceToPendingReview(t, svc, "reopen")
		first, err := svc.Review(context.Background(), service.ReviewRequest{
			CeremonyID: "reopen", Operation: "review-rejected", Revision: view.Ceremony.Revision,
			Conclusion: ceremony.ReviewRejected, ReviewDigest: "persisted-rejected",
		})
		if err != nil {
			t.Fatalf("first review: %v", err)
		}
		if err := st.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}

		st2, err := sqlite.Open(path)
		if err != nil {
			t.Fatalf("reopen store: %v", err)
		}
		defer st2.Close()
		svc2 := service.NewService(st2)
		persisted, err := svc2.Get(context.Background(), "reopen")
		if err != nil {
			t.Fatalf("get after reopen: %v", err)
		}
		if persisted.Ceremony.ReviewConclusion != ceremony.ReviewRejected || persisted.Ceremony.ReviewDigest != "persisted-rejected" || persisted.Artifact == nil || persisted.Artifact.ReviewDigest != "persisted-rejected" {
			t.Fatalf("persisted review=%+v artifact=%+v", persisted.Ceremony, persisted.Artifact)
		}
		_, err = svc2.Review(context.Background(), service.ReviewRequest{
			CeremonyID: "reopen", Operation: "review-approved", Revision: first.Revision,
			Conclusion: ceremony.ReviewApproved, ReviewDigest: "should-not-persist",
		})
		if err == nil {
			t.Fatal("post-reopen overwrite succeeded, want rejection")
		}
		unchanged, err := svc2.Get(context.Background(), "reopen")
		if err != nil {
			t.Fatalf("get after post-reopen rejection: %v", err)
		}
		if unchanged.Ceremony.Revision != first.Revision || unchanged.Ceremony.ReviewConclusion != ceremony.ReviewRejected || unchanged.Artifact.ReviewDigest != "persisted-rejected" {
			t.Fatalf("post-reopen rejection mutated state=%+v artifact=%+v", unchanged.Ceremony, unchanged.Artifact)
		}
	})

	t.Run("approved review retains normal sealing path", func(t *testing.T) {
		svc, st := goldB4_8a1243f1_openService(t, "")
		defer st.Close()
		view := goldB4_8a1243f1_advanceToPendingReview(t, svc, "approved-normal")
		review, err := svc.Review(context.Background(), service.ReviewRequest{
			CeremonyID: "approved-normal", Operation: "review-approved", Revision: view.Ceremony.Revision,
			Conclusion: ceremony.ReviewApproved, ReviewDigest: "approved-digest",
		})
		if err != nil {
			t.Fatalf("approved review: %v", err)
		}
		sealed, err := svc.Seal(context.Background(), service.SealRequest{
			CeremonyID: "approved-normal", Operation: "seal", Revision: review.Revision, Reason: "normal seal",
		})
		if err != nil {
			t.Fatalf("seal after approved review: %v", err)
		}
		if sealed.State != ceremony.StateSealed || sealed.Revision != review.Revision+1 {
			t.Fatalf("seal result=%+v, want sealed at next revision", sealed)
		}
	})
}

func goldB4_8a1243f1_openService(t *testing.T, path string) (*service.Service, *sqlite.Store) {
	t.Helper()
	st, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	goldB4_8a1243f1_seedDirectory(t, st)
	return service.NewService(st), st
}

func goldB4_8a1243f1_seedDirectory(t *testing.T, st *sqlite.Store) {
	t.Helper()
	ctx := context.Background()
	if err := st.UpsertPolicy(ctx, policy.Policy{
		Version: "policy-v1", Threshold: 3,
		AllowedRoles: []policy.Role{"officer", "auditor"}, AllowedKeyVersion: "key-v1",
	}); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	for _, participant := range []policy.Participant{
		{PersonID: "alice", Credentials: []string{"alice-cred"}, Role: "officer", Revision: 1},
		{PersonID: "bob", Credentials: []string{"bob-cred"}, Role: "officer", Revision: 1},
		{PersonID: "carol", Credentials: []string{"carol-cred"}, Role: "officer", Revision: 1},
	} {
		if err := st.UpsertParticipant(ctx, participant); err != nil {
			t.Fatalf("seed participant %s: %v", participant.PersonID, err)
		}
	}
}

func goldB4_8a1243f1_advanceToPendingReview(t *testing.T, svc *service.Service, id ceremony.ID) service.View {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.Create(ctx, service.CreateRequest{ID: id}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Lock(ctx, service.LockRequest{
		CeremonyID: id, Operation: "lock", Revision: 0, Digest: "request-digest",
		KeyVersion: "key-v1", PolicyVersion: "policy-v1", Participants: []string{"alice", "bob", "carol"},
	}); err != nil {
		t.Fatalf("lock: %v", err)
	}
	revision := ceremony.Revision(1)
	for _, person := range []string{"alice", "bob", "carol"} {
		result, err := svc.ConfirmWitness(ctx, service.ConfirmRequest{
			CeremonyID: id, Operation: "witness-" + person, Revision: revision,
			PersonID: person, Credential: person + "-cred", IdentityRevision: 1,
		})
		if err != nil {
			t.Fatalf("confirm %s: %v", person, err)
		}
		revision = result.Revision
	}
	begin, err := svc.BeginSignature(ctx, service.BeginRequest{
		CeremonyID: id, Operation: "begin", Revision: revision, Token: "token", SessionID: "session",
	})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	receipt, err := svc.RecordReceipt(ctx, service.ReceiptRequest{
		CeremonyID: id, Operation: "receipt", Revision: begin.Revision, SessionID: "session", Receipt: "receipt",
	})
	if err != nil {
		t.Fatalf("receipt: %v", err)
	}
	artifact, err := svc.RegisterArtifact(ctx, service.ArtifactRequest{
		CeremonyID: id, Operation: "artifact", Revision: receipt.Revision, Digest: "artifact",
	})
	if err != nil {
		t.Fatalf("artifact: %v", err)
	}
	view, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("get pending review: %v", err)
	}
	if view.Ceremony.State != ceremony.StatePendingReview || view.Ceremony.Revision != artifact.Revision {
		t.Fatalf("pending review view=%+v, artifact=%+v", view.Ceremony, artifact)
	}
	return view
}

func goldB4_8a1243f1_postJSON(t *testing.T, handler http.Handler, path string, payload any) (int, string) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	var response bytes.Buffer
	if _, err := response.ReadFrom(resp.Result().Body); err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp.Code, response.String()
}
