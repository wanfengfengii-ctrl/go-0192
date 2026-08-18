package policy

import "testing"

func TestPolicyAllowsRoleAndKey(t *testing.T) {
	p := Policy{
		Version:           "v2",
		Threshold:         3,
		AllowedRoles:      []Role{"officer", "auditor"},
		AllowedKeyVersion: "key-v1",
	}
	if !p.AllowsRole("officer") {
		t.Fatal("AllowsRole(officer) = false, want true")
	}
	if p.AllowsRole("unknown") {
		t.Fatal("AllowsRole(unknown) = true, want false")
	}
	if !p.AllowsKeyVersion("key-v1") {
		t.Fatal("AllowsKeyVersion(key-v1) = false, want true")
	}
	if p.AllowsKeyVersion("key-v2") {
		t.Fatal("AllowsKeyVersion(key-v2) = true, want false")
	}
}

func TestParticipantCredentialsAndActive(t *testing.T) {
	p := Participant{
		PersonID:    "alice",
		Credentials: []string{"cred-a", "cred-b"},
		Role:        "officer",
		Revision:    3,
	}
	if !p.HasCredential("cred-b") {
		t.Fatal("HasCredential(cred-b) = false, want true")
	}
	if p.HasCredential("cred-c") {
		t.Fatal("HasCredential(cred-c) = true, want false")
	}
	if !p.Active() {
		t.Fatal("Active() = false, want true")
	}
	p.Revoked = true
	if p.Active() {
		t.Fatal("Active() = true after revocation, want false")
	}
}
