package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"quorumforge/internal/ceremony"
	"quorumforge/internal/policy"
	"quorumforge/internal/store/sqlite"
)

func newTestService(t *testing.T) (*Service, *sqlite.Store) {
	t.Helper()
	st, err := sqlite.Open("")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	if err := st.UpsertPolicy(ctx, policy.Policy{
		Version: "policy-v1", Threshold: 3,
		AllowedRoles:      []policy.Role{"officer", "auditor"},
		AllowedKeyVersion: "key-v1",
	}); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	for _, p := range []policy.Participant{
		{PersonID: "alice", Credentials: []string{"alice-cred"}, Role: "officer", Revision: 1},
		{PersonID: "bob", Credentials: []string{"bob-cred"}, Role: "officer", Revision: 1},
		{PersonID: "carol", Credentials: []string{"carol-cred"}, Role: "officer", Revision: 1},
		{PersonID: "dave", Credentials: []string{"dave-cred"}, Role: "auditor", Revision: 1},
	} {
		if err := st.UpsertParticipant(ctx, p); err != nil {
			t.Fatalf("seed participant %s: %v", p.PersonID, err)
		}
	}
	return NewService(st), st
}

func lockCeremony(t *testing.T, svc *Service, id ceremony.ID) {
	t.Helper()
	_, err := svc.Create(context.Background(), CreateRequest{ID: id})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.Lock(context.Background(), LockRequest{
		CeremonyID: id, Operation: "lock-1", Revision: 0,
		Digest: "digest-1", KeyVersion: "key-v1", PolicyVersion: "policy-v1",
		Participants: []string{"alice", "bob", "carol"},
	})
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
}

func confirmAll(t *testing.T, svc *Service, id ceremony.ID) {
	t.Helper()
	rev := ceremony.Revision(1)
	for _, p := range []string{"alice", "bob", "carol"} {
		res, err := svc.ConfirmWitness(context.Background(), ConfirmRequest{
			CeremonyID: id, Operation: "w-" + p, Revision: rev,
			PersonID: p, Credential: p + "-cred", IdentityRevision: 1,
		})
		if err != nil {
			t.Fatalf("confirm %s: %v", p, err)
		}
		rev = res.Revision
	}
}

func TestFullSealWorkflow(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lockCeremony(t, svc, "c1")
	confirmAll(t, svc, "c1")

	v, err := svc.Get(ctx, "c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if v.Ceremony.State != ceremony.StatePendingSignature || !v.QuorumReached {
		t.Fatalf("state=%s quorum=%v, want pending-signature true", v.Ceremony.State, v.QuorumReached)
	}

	if _, err := svc.BeginSignature(ctx, BeginRequest{
		CeremonyID: "c1", Operation: "begin", Revision: v.Ceremony.Revision, Token: "tok-1", SessionID: "sess-1",
	}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	v, _ = svc.Get(ctx, "c1")
	if _, err := svc.RecordReceipt(ctx, ReceiptRequest{
		CeremonyID: "c1", Operation: "receipt", Revision: v.Ceremony.Revision, SessionID: "sess-1", Receipt: "rcpt-1",
	}); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	v, _ = svc.Get(ctx, "c1")
	if _, err := svc.RegisterArtifact(ctx, ArtifactRequest{
		CeremonyID: "c1", Operation: "artifact", Revision: v.Ceremony.Revision, Digest: "art-1",
	}); err != nil {
		t.Fatalf("artifact: %v", err)
	}
	v, _ = svc.Get(ctx, "c1")
	if _, err := svc.Review(ctx, ReviewRequest{
		CeremonyID: "c1", Operation: "review", Revision: v.Ceremony.Revision,
		Conclusion: ceremony.ReviewApproved, ReviewDigest: "review-1",
	}); err != nil {
		t.Fatalf("review: %v", err)
	}
	v, _ = svc.Get(ctx, "c1")
	res, err := svc.Seal(ctx, SealRequest{CeremonyID: "c1", Operation: "seal", Revision: v.Ceremony.Revision})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if res.State != ceremony.StateSealed {
		t.Fatalf("state=%s, want sealed", res.State)
	}
}

func TestIdempotentReplayReturnsSameResult(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lockCeremony(t, svc, "c1")

	req := ConfirmRequest{
		CeremonyID: "c1", Operation: "w-alice", Revision: 1,
		PersonID: "alice", Credential: "alice-cred", IdentityRevision: 1,
	}
	first, err := svc.ConfirmWitness(ctx, req)
	if err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	second, err := svc.ConfirmWitness(ctx, req)
	if err != nil {
		t.Fatalf("replay confirm: %v", err)
	}
	if first.Revision != second.Revision || first.WitnessCount != second.WitnessCount {
		t.Fatalf("replay result differs: %+v vs %+v", first, second)
	}
	v, _ := svc.Get(ctx, "c1")
	if v.WitnessCount != 1 {
		t.Fatalf("witness count=%d, want 1 after idempotent replay", v.WitnessCount)
	}
}

func TestOperationContentConflict(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lockCeremony(t, svc, "c1")

	if _, err := svc.ConfirmWitness(ctx, ConfirmRequest{
		CeremonyID: "c1", Operation: "op", Revision: 1, PersonID: "alice", Credential: "alice-cred", IdentityRevision: 1,
	}); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	// Same operation, different content.
	_, err := svc.ConfirmWitness(ctx, ConfirmRequest{
		CeremonyID: "c1", Operation: "op", Revision: 1, PersonID: "bob", Credential: "bob-cred", IdentityRevision: 1,
	})
	if !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("err=%v, want ErrOperationConflict", err)
	}
}

func TestStaleRevisionRejected(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lockCeremony(t, svc, "c1")

	// Correct revision is 1; pass 0.
	_, err := svc.ConfirmWitness(ctx, ConfirmRequest{
		CeremonyID: "c1", Operation: "w", Revision: 0, PersonID: "alice", Credential: "alice-cred", IdentityRevision: 1,
	})
	if !errors.Is(err, ceremony.ErrStaleRevision) {
		t.Fatalf("err=%v, want ErrStaleRevision", err)
	}
}

func TestDuplicateWitnessDeduped(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lockCeremony(t, svc, "c1")

	first, err := svc.ConfirmWitness(ctx, ConfirmRequest{
		CeremonyID: "c1", Operation: "w1", Revision: 1, PersonID: "alice", Credential: "alice-cred", IdentityRevision: 1,
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err = svc.ConfirmWitness(ctx, ConfirmRequest{
		CeremonyID: "c1", Operation: "w2", Revision: first.Revision, PersonID: "alice", Credential: "alice-cred", IdentityRevision: 1,
	})
	if !errors.Is(err, ErrDuplicateWitness) {
		t.Fatalf("err=%v, want ErrDuplicateWitness", err)
	}
	v, _ := svc.Get(ctx, "c1")
	if v.WitnessCount != 1 {
		t.Fatalf("witness count=%d, want 1", v.WitnessCount)
	}
}

func TestSingleSessionUnderConcurrency(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lockCeremony(t, svc, "c1")
	confirmAll(t, svc, "c1")
	v, _ := svc.Get(ctx, "c1")

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.BeginSignature(ctx, BeginRequest{
				CeremonyID: "c1", Operation: "begin", Revision: v.Ceremony.Revision,
				Token: "tok-" + string(rune('a'+i)), SessionID: "sess-" + string(rune('a'+i)),
			})
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful BeginSignature calls=%d, want exactly 1", successes)
	}
}

func TestBeginSignatureGeneratesSessionID(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lockCeremony(t, svc, "generated-session")
	confirmAll(t, svc, "generated-session")
	v, err := svc.Get(ctx, "generated-session")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	result, err := svc.BeginSignature(ctx, BeginRequest{
		CeremonyID: "generated-session",
		Operation:  "begin-generated",
		Revision:   v.Ceremony.Revision,
		Token:      "token-generated",
	})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if len(result.SessionID) != 32 {
		t.Fatalf("generated session id=%q, want 32 hex characters", result.SessionID)
	}
	view, err := svc.Get(ctx, "generated-session")
	if err != nil {
		t.Fatalf("get after begin: %v", err)
	}
	if view.Session == nil || view.Session.ID != result.SessionID {
		t.Fatalf("persisted session=%+v, result=%+v", view.Session, result)
	}
}

func TestSealQuarantineRaceSingleTerminal(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lockCeremony(t, svc, "c1")
	confirmAll(t, svc, "c1")

	v, _ := svc.Get(ctx, "c1")
	_ = v
	// Drive to pending review.
	beginRes, err := svc.BeginSignature(ctx, BeginRequest{
		CeremonyID: "c1", Operation: "begin", Revision: v.Ceremony.Revision, Token: "tok-1", SessionID: "s-1",
	})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := svc.RecordReceipt(ctx, ReceiptRequest{
		CeremonyID: "c1", Operation: "receipt", Revision: beginRes.Revision, SessionID: "s-1", Receipt: "r-1",
	}); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	v, _ = svc.Get(ctx, "c1")
	if _, err := svc.RegisterArtifact(ctx, ArtifactRequest{
		CeremonyID: "c1", Operation: "artifact", Revision: v.Ceremony.Revision, Digest: "a-1",
	}); err != nil {
		t.Fatalf("artifact: %v", err)
	}
	v, _ = svc.Get(ctx, "c1")
	if _, err := svc.Review(ctx, ReviewRequest{
		CeremonyID: "c1", Operation: "review", Revision: v.Ceremony.Revision, Conclusion: ceremony.ReviewApproved, ReviewDigest: "rd",
	}); err != nil {
		t.Fatalf("review: %v", err)
	}
	v, _ = svc.Get(ctx, "c1")

	var wg sync.WaitGroup
	results := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, results[0] = svc.Seal(ctx, SealRequest{CeremonyID: "c1", Operation: "seal", Revision: v.Ceremony.Revision})
	}()
	go func() {
		defer wg.Done()
		_, results[1] = svc.Quarantine(ctx, QuarantineRequest{CeremonyID: "c1", Operation: "quar", Revision: v.Ceremony.Revision})
	}()
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("terminal successes=%d, want exactly 1", successes)
	}
	final, _ := svc.Get(ctx, "c1")
	if !final.Ceremony.State.IsTerminal() {
		t.Fatalf("state=%s, want terminal", final.Ceremony.State)
	}
}

func TestIdentityRevisionMismatchRejected(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lockCeremony(t, svc, "c1")

	_, err := svc.ConfirmWitness(ctx, ConfirmRequest{
		CeremonyID: "c1", Operation: "w", Revision: 1, PersonID: "alice", Credential: "alice-cred", IdentityRevision: 99,
	})
	if !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("err=%v, want ErrIdentityMismatch", err)
	}
}

func TestRevokedParticipantRejected(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	lockCeremony(t, svc, "c1")

	if err := st.UpsertParticipant(ctx, policy.Participant{
		PersonID: "alice", Credentials: []string{"alice-cred"}, Role: "officer", Revision: 2, Revoked: true,
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	_, err := svc.ConfirmWitness(ctx, ConfirmRequest{
		CeremonyID: "c1", Operation: "w", Revision: 1, PersonID: "alice", Credential: "alice-cred", IdentityRevision: 1,
	})
	if !errors.Is(err, ErrRevoked) {
		t.Fatalf("err=%v, want ErrRevoked", err)
	}
}

func TestSealRequiresApprovedReview(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lockCeremony(t, svc, "c1")
	confirmAll(t, svc, "c1")

	v, _ := svc.Get(ctx, "c1")
	if _, err := svc.BeginSignature(ctx, BeginRequest{
		CeremonyID: "c1", Operation: "b", Revision: v.Ceremony.Revision, Token: "t", SessionID: "s",
	}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	v, _ = svc.Get(ctx, "c1")
	if _, err := svc.RecordReceipt(ctx, ReceiptRequest{
		CeremonyID: "c1", Operation: "r", Revision: v.Ceremony.Revision, SessionID: "s", Receipt: "rc",
	}); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	v, _ = svc.Get(ctx, "c1")
	if _, err := svc.RegisterArtifact(ctx, ArtifactRequest{
		CeremonyID: "c1", Operation: "a", Revision: v.Ceremony.Revision, Digest: "d",
	}); err != nil {
		t.Fatalf("artifact: %v", err)
	}
	v, _ = svc.Get(ctx, "c1")
	if _, err := svc.Review(ctx, ReviewRequest{
		CeremonyID: "c1", Operation: "rv", Revision: v.Ceremony.Revision, Conclusion: ceremony.ReviewRejected, ReviewDigest: "rd",
	}); err != nil {
		t.Fatalf("review: %v", err)
	}
	v, _ = svc.Get(ctx, "c1")
	_, err := svc.Seal(ctx, SealRequest{CeremonyID: "c1", Operation: "seal", Revision: v.Ceremony.Revision})
	if err == nil {
		t.Fatal("seal with rejected review succeeded, want error")
	}
}
