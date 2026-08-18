package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"quorumforge/internal/policy"
	"quorumforge/internal/service"
	"quorumforge/internal/store/sqlite"
)

func newTestServer(t *testing.T) *httptest.Server {
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
	} {
		if err := st.UpsertParticipant(ctx, p); err != nil {
			t.Fatalf("seed participant: %v", err)
		}
	}

	srv := New(service.NewService(st), ":0")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func doJSON(t *testing.T, ts *httptest.Server, method, path string, body any, wantStatus int) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != wantStatus {
		var m map[string]any
		_ = json.NewDecoder(res.Body).Decode(&m)
		t.Fatalf("%s %s status=%d want=%d body=%v", method, path, res.StatusCode, wantStatus, m)
	}
	var m map[string]any
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return m
}

func TestHealthEndpoint(t *testing.T) {
	ts := newTestServer(t)
	res, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("healthz status=%d", res.StatusCode)
	}
}

func TestCreateAndGet(t *testing.T) {
	ts := newTestServer(t)
	doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies", map[string]any{"id": "c1"}, http.StatusCreated)
	m := doJSON(t, ts, http.MethodGet, "/api/v1/ceremonies/c1", nil, http.StatusOK)
	if m["id"] != "c1" || m["state"] != "pending-lock" {
		t.Fatalf("unexpected view: %v", m)
	}
}

func TestFullFlowViaHTTP(t *testing.T) {
	ts := newTestServer(t)
	doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies", map[string]any{"id": "c1"}, http.StatusCreated)

	doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies/c1/lock", map[string]any{
		"operation": "lock", "revision": 0, "digest": "digest-1",
		"key_version": "key-v1", "policy_version": "policy-v1",
		"participants": []string{"alice", "bob", "carol"},
	}, http.StatusOK)

	rev := 1
	for _, p := range []string{"alice", "bob", "carol"} {
		m := doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies/c1/witness", map[string]any{
			"operation": "w-" + p, "revision": rev, "person_id": p,
			"credential": p + "-cred", "identity_revision": 1,
		}, http.StatusOK)
		rev = int(m["revision"].(float64))
	}

	doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies/c1/begin", map[string]any{
		"operation": "begin", "revision": rev, "token": "tok-1", "session_id": "s-1",
	}, http.StatusOK)
	rev++

	doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies/c1/receipt", map[string]any{
		"operation": "receipt", "revision": rev, "session_id": "s-1", "receipt": "r-1",
	}, http.StatusOK)
	rev++

	doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies/c1/artifact", map[string]any{
		"operation": "artifact", "revision": rev, "digest": "digest-1",
	}, http.StatusOK)
	rev++

	doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies/c1/review", map[string]any{
		"operation": "review", "revision": rev, "conclusion": "approved", "review_digest": "rd-1",
	}, http.StatusOK)
	rev++

	m := doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies/c1/seal", map[string]any{
		"operation": "seal", "revision": rev,
	}, http.StatusOK)
	if m["state"] != "sealed" {
		t.Fatalf("state=%v, want sealed", m["state"])
	}

	view := doJSON(t, ts, http.MethodGet, "/api/v1/ceremonies/c1", nil, http.StatusOK)
	if view["state"] != "sealed" {
		t.Fatalf("view state=%v, want sealed", view["state"])
	}
}

func TestOperationPageServed(t *testing.T) {
	ts := newTestServer(t)
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("page status=%d", res.StatusCode)
	}
}

func TestInvalidLockReturnsBadRequest(t *testing.T) {
	ts := newTestServer(t)
	doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies", map[string]any{"id": "c1"}, http.StatusCreated)
	doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies/c1/lock", map[string]any{
		"operation": "lock", "revision": 0, "digest": "",
		"key_version": "key-v1", "policy_version": "policy-v1",
		"participants": []string{"alice"},
	}, http.StatusBadRequest)
}

func TestStaleRevisionReturnsConflict(t *testing.T) {
	ts := newTestServer(t)
	doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies", map[string]any{"id": "c1"}, http.StatusCreated)
	doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies/c1/lock", map[string]any{
		"operation": "lock", "revision": 0, "digest": "d", "key_version": "key-v1",
		"policy_version": "policy-v1", "participants": []string{"alice", "bob", "carol"},
	}, http.StatusOK)
	// Stale revision 0 after lock (current is 1).
	doJSON(t, ts, http.MethodPost, "/api/v1/ceremonies/c1/witness", map[string]any{
		"operation": "w", "revision": 0, "person_id": "alice",
		"credential": "alice-cred", "identity_revision": 1,
	}, http.StatusConflict)
}
