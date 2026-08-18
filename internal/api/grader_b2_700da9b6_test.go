package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"quorumforge/internal/policy"
	"quorumforge/internal/service"
	"quorumforge/internal/store/sqlite"
)

const goldB2_700da9b6Token = "gold-b2-700da9b6-api-token"

type goldB2_700da9b6AuthorizationTransport struct {
	base http.RoundTripper
}

func (g goldB2_700da9b6AuthorizationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	request := r.Clone(r.Context())
	request.Header = r.Header.Clone()
	if request.Header.Get("Authorization") == "" && request.URL.Path != "/" && request.URL.Path != "/healthz" {
		request.Header.Set("Authorization", "Bearer "+goldB2_700da9b6Token)
	}
	return g.base.RoundTrip(request)
}

var goldB2_700da9b6TestEnvironment = goldB2_700da9b6ConfigureTestEnvironment()

func goldB2_700da9b6ConfigureTestEnvironment() bool {
	_ = os.Setenv("QUORUMFORGE_API_TOKEN", goldB2_700da9b6Token)
	http.DefaultTransport = goldB2_700da9b6AuthorizationTransport{base: http.DefaultTransport}
	return true
}

func goldB2_700da9b6Request(t *testing.T, handler http.Handler, method, path string, body any, authorization string) *http.Response {
	t.Helper()

	var encoded bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&encoded).Encode(body); err != nil {
			t.Fatalf("encode %s %s request: %v", method, path, err)
		}
	}
	request := httptest.NewRequest(method, path, &encoded)
	request.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Result()
}

func goldB2_700da9b6JSON(t *testing.T, response *http.Response, wantStatus int) map[string]any {
	t.Helper()
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode status %d response: %v", response.StatusCode, err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("status = %d, want %d; body = %v", response.StatusCode, wantStatus, body)
	}
	return body
}

func TestGoldB2_700da9b6_SensitiveAPIAuthorizationBoundary(t *testing.T) {
	if !goldB2_700da9b6TestEnvironment || os.Getenv("QUORUMFORGE_API_TOKEN") != goldB2_700da9b6Token {
		t.Fatal("authorization test environment was not configured")
	}

	databasePath := filepath.Join(t.TempDir(), "quorumforge.db")
	store, err := sqlite.Open(databasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ctx := context.Background()
	if err := store.UpsertPolicy(ctx, policy.Policy{
		Version:           "policy-v1",
		Threshold:         3,
		AllowedRoles:      []policy.Role{"officer"},
		AllowedKeyVersion: "key-v1",
	}); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	for _, person := range []string{"alice", "bob", "carol"} {
		if err := store.UpsertParticipant(ctx, policy.Participant{
			PersonID:    policy.PersonID(person),
			Credentials: []string{person + "-cred"},
			Role:        "officer",
			Revision:    1,
		}); err != nil {
			t.Fatalf("seed participant %s: %v", person, err)
		}
	}

	handler := New(service.NewService(store), ":0").Handler()
	authorized := "Bearer " + goldB2_700da9b6Token
	requestJSON := func(current *testing.T, method, path string, body any, authorization string, wantStatus int) map[string]any {
		current.Helper()
		return goldB2_700da9b6JSON(current, goldB2_700da9b6Request(current, handler, method, path, body, authorization), wantStatus)
	}
	assertUnauthorized := func(current *testing.T, method, path string, body any, authorization string) {
		current.Helper()
		response := goldB2_700da9b6Request(current, handler, method, path, body, authorization)
		if got := response.Header.Get("WWW-Authenticate"); got != `Bearer realm="quorumforge"` {
			response.Body.Close()
			current.Fatalf("WWW-Authenticate = %q, want Bearer challenge", got)
		}
		result := goldB2_700da9b6JSON(current, response, http.StatusUnauthorized)
		if result["error"] != "unauthorized" {
			current.Fatalf("error = %v, want unauthorized", result["error"])
		}
	}

	t.Run("public operational endpoints remain available", func(t *testing.T) {
		for _, path := range []string{"/healthz", "/"} {
			response := goldB2_700da9b6Request(t, handler, http.MethodGet, path, nil, "")
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200", path, response.StatusCode)
			}
		}
	})

	t.Run("missing server credential fails closed", func(t *testing.T) {
		t.Setenv("QUORUMFORGE_API_TOKEN", "")
		unconfigured := New(service.NewService(store), ":0").Handler()
		response := goldB2_700da9b6Request(t, unconfigured, http.MethodPost, "/api/v1/ceremonies", map[string]any{"id": "unconfigured"}, authorized)
		goldB2_700da9b6JSON(t, response, http.StatusUnauthorized)
	})

	t.Run("authentication precedes request decoding", func(t *testing.T) {
		assertUnauthorized(t, http.MethodPost, "/api/v1/ceremonies", "not-a-create-request", "")
	})

	t.Run("credential boundaries reject before creation", func(t *testing.T) {
		credentials := []struct {
			name          string
			authorization string
		}{
			{name: "missing"},
			{name: "wrong scheme", authorization: "Basic Zm9vOmJhcg=="},
			{name: "empty bearer", authorization: "Bearer "},
			{name: "wrong bearer", authorization: "Bearer wrong-token"},
			{name: "extra bearer field", authorization: "Bearer " + goldB2_700da9b6Token + " extra"},
		}
		for i, credential := range credentials {
			t.Run(credential.name, func(t *testing.T) {
				id := fmt.Sprintf("denied-%d", i)
				assertUnauthorized(t, http.MethodPost, "/api/v1/ceremonies", map[string]any{"id": id}, credential.authorization)
				requestJSON(t, http.MethodGet, "/api/v1/ceremonies/"+id, nil, authorized, http.StatusNotFound)
			})
		}
	})

	t.Run("authenticated creation remains available", func(t *testing.T) {
		requestJSON(t, http.MethodPost, "/api/v1/ceremonies", map[string]any{"id": "sealed-flow"}, authorized, http.StatusCreated)
	})
	command := func(name, path string, body any, wantState string, wantRevision int) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			assertUnauthorized(t, http.MethodPost, path, body, "")
			before := requestJSON(t, http.MethodGet, "/api/v1/ceremonies/sealed-flow", nil, authorized, http.StatusOK)
			if before["revision"] != float64(wantRevision-1) {
				t.Fatalf("revision after denied command = %v, want %d", before["revision"], wantRevision-1)
			}
			result := requestJSON(t, http.MethodPost, path, body, authorized, http.StatusOK)
			if result["revision"] != float64(wantRevision) {
				t.Fatalf("authorized revision = %v, want %d", result["revision"], wantRevision)
			}
			view := requestJSON(t, http.MethodGet, "/api/v1/ceremonies/sealed-flow", nil, authorized, http.StatusOK)
			if view["state"] != wantState || view["revision"] != float64(wantRevision) {
				t.Fatalf("view after authorized command = %v, want state %s revision %d", view, wantState, wantRevision)
			}
		})
	}

	command("lock", "/api/v1/ceremonies/sealed-flow/lock", map[string]any{
		"operation": "lock-op", "revision": 0, "digest": "digest-1",
		"key_version": "key-v1", "policy_version": "policy-v1",
		"participants": []string{"alice", "bob", "carol"},
	}, "gathering-witnesses", 1)
	for index, person := range []string{"alice", "bob", "carol"} {
		command("witness "+person, "/api/v1/ceremonies/sealed-flow/witness", map[string]any{
			"operation": "witness-" + person, "revision": index + 1, "person_id": person,
			"credential": person + "-cred", "identity_revision": 1,
		}, map[bool]string{true: "pending-signature", false: "gathering-witnesses"}[index == 2], index+2)
	}
	command("begin signature", "/api/v1/ceremonies/sealed-flow/begin", map[string]any{
		"operation": "begin-op", "revision": 4, "token": "token-1", "session_id": "session-1",
	}, "pending-signature", 5)
	command("record receipt", "/api/v1/ceremonies/sealed-flow/receipt", map[string]any{
		"operation": "receipt-op", "revision": 5, "session_id": "session-1", "receipt": "receipt-1",
	}, "pending-signature", 6)
	command("register artifact", "/api/v1/ceremonies/sealed-flow/artifact", map[string]any{
		"operation": "artifact-op", "revision": 6, "digest": "artifact-1",
	}, "pending-review", 7)
	command("review", "/api/v1/ceremonies/sealed-flow/review", map[string]any{
		"operation": "review-op", "revision": 7, "conclusion": "approved", "review_digest": "review-1",
	}, "pending-review", 8)
	command("seal", "/api/v1/ceremonies/sealed-flow/seal", map[string]any{
		"operation": "seal-op", "revision": 8,
	}, "sealed", 9)

	t.Run("query is protected from anonymous disclosure", func(t *testing.T) {
		assertUnauthorized(t, http.MethodGet, "/api/v1/ceremonies/sealed-flow", nil, "")
	})

	t.Run("authorization precedes idempotent replay", func(t *testing.T) {
		seal := map[string]any{"operation": "seal-op", "revision": 8}
		assertUnauthorized(t, http.MethodPost, "/api/v1/ceremonies/sealed-flow/seal", seal, "")
		result := requestJSON(t, http.MethodPost, "/api/v1/ceremonies/sealed-flow/seal", seal, authorized, http.StatusOK)
		if result["state"] != "sealed" || result["revision"] != float64(9) {
			t.Fatalf("authorized idempotent replay = %v", result)
		}
		view := requestJSON(t, http.MethodGet, "/api/v1/ceremonies/sealed-flow", nil, authorized, http.StatusOK)
		if view["revision"] != float64(9) {
			t.Fatalf("revision after idempotent replay = %v, want 9", view["revision"])
		}
	})

	t.Run("quarantine and cancellation commands are protected", func(t *testing.T) {
		requestJSON(t, http.MethodPost, "/api/v1/ceremonies", map[string]any{"id": "quarantine-flow"}, authorized, http.StatusCreated)
		requestJSON(t, http.MethodPost, "/api/v1/ceremonies/quarantine-flow/lock", map[string]any{
			"operation": "lock-q", "revision": 0, "digest": "digest-q", "key_version": "key-v1",
			"policy_version": "policy-v1", "participants": []string{"alice", "bob", "carol"},
		}, authorized, http.StatusOK)
		quarantine := map[string]any{"operation": "quarantine-op", "revision": 1, "reason": "suspected compromise"}
		assertUnauthorized(t, http.MethodPost, "/api/v1/ceremonies/quarantine-flow/quarantine", quarantine, "")
		result := requestJSON(t, http.MethodPost, "/api/v1/ceremonies/quarantine-flow/quarantine", quarantine, authorized, http.StatusOK)
		if result["state"] != "quarantined" || result["revision"] != float64(2) {
			t.Fatalf("authorized quarantine result = %v", result)
		}

		requestJSON(t, http.MethodPost, "/api/v1/ceremonies", map[string]any{"id": "cancel-flow"}, authorized, http.StatusCreated)
		cancel := map[string]any{"operation": "cancel-op", "revision": 0, "reason": "operator request"}
		assertUnauthorized(t, http.MethodPost, "/api/v1/ceremonies/cancel-flow/cancel", cancel, "")
		result = requestJSON(t, http.MethodPost, "/api/v1/ceremonies/cancel-flow/cancel", cancel, authorized, http.StatusOK)
		if result["state"] != "cancelled" || result["revision"] != float64(1) {
			t.Fatalf("authorized cancellation result = %v", result)
		}
	})

	if err := store.Close(); err != nil {
		t.Fatalf("close store before restart: %v", err)
	}

	t.Run("rejected requests leave no persistent effects after restart", func(t *testing.T) {
		reopened, err := sqlite.Open(databasePath)
		if err != nil {
			t.Fatalf("reopen store: %v", err)
		}
		defer reopened.Close()
		restarted := New(service.NewService(reopened), ":0").Handler()
		check := func(path string, wantStatus int) map[string]any {
			t.Helper()
			return goldB2_700da9b6JSON(t, goldB2_700da9b6Request(t, restarted, http.MethodGet, path, nil, authorized), wantStatus)
		}
		sealed := check("/api/v1/ceremonies/sealed-flow", http.StatusOK)
		if sealed["state"] != "sealed" || sealed["revision"] != float64(9) {
			t.Fatalf("persisted sealed flow = %v", sealed)
		}
		if quarantined := check("/api/v1/ceremonies/quarantine-flow", http.StatusOK); quarantined["state"] != "quarantined" || quarantined["revision"] != float64(2) {
			t.Fatalf("persisted quarantine flow = %v", quarantined)
		}
		if cancelled := check("/api/v1/ceremonies/cancel-flow", http.StatusOK); cancelled["state"] != "cancelled" || cancelled["revision"] != float64(1) {
			t.Fatalf("persisted cancellation flow = %v", cancelled)
		}
		for i := 0; i < 5; i++ {
			check(fmt.Sprintf("/api/v1/ceremonies/denied-%d", i), http.StatusNotFound)
		}
		assertResponse := goldB2_700da9b6Request(t, restarted, http.MethodGet, "/api/v1/ceremonies/sealed-flow", nil, "")
		if assertResponse.StatusCode != http.StatusUnauthorized {
			assertResponse.Body.Close()
			t.Fatalf("anonymous query after restart status = %d, want 401", assertResponse.StatusCode)
		}
		assertResponse.Body.Close()
	})
}
