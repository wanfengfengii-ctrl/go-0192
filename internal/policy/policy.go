// Package policy maintains the policy catalog, participant directory and the
// immutable snapshots captured at ceremony lock time.
package policy

import (
	"errors"
	"fmt"
)

// Role identifies a participant role within a ceremony.
type Role string

// Version identifies a policy catalog version.
type Version string

// KeyVersion identifies an allowed root key version.
type KeyVersion string

// Policy describes a single policy catalog entry.
type Policy struct {
	Version           Version
	Threshold         int
	AllowedRoles      []Role
	AllowedKeyVersion KeyVersion
}

// AllowsRole reports whether the policy permits the given role.
func (p Policy) AllowsRole(r Role) bool {
	for _, allowed := range p.AllowedRoles {
		if allowed == r {
			return true
		}
	}
	return false
}

// AllowsKeyVersion reports whether the policy permits the given key version.
func (p Policy) AllowsKeyVersion(k KeyVersion) bool {
	return p.AllowedKeyVersion == k
}

// PersonID is a natural-person identifier used for de-duplication.
type PersonID string

// Participant is a directory entry for a single natural person.
type Participant struct {
	PersonID    PersonID
	Credentials []string
	Role        Role
	Revision    uint64
	Revoked     bool
}

// HasCredential reports whether cred is registered for this participant.
func (p Participant) HasCredential(cred string) bool {
	for _, c := range p.Credentials {
		if c == cred {
			return true
		}
	}
	return false
}

// Active reports whether the participant is currently eligible.
func (p Participant) Active() bool {
	return !p.Revoked
}

// Errors surfaced by the directory.
var (
	ErrPolicyNotFound      = errors.New("policy not found")
	ErrParticipantNotFound = errors.New("participant not found")
	ErrKeyVersionMismatch  = errors.New("key version not allowed by policy")
)

// InvalidRoleError describes a role that the policy does not permit.
type InvalidRoleError struct {
	Role   Role
	Policy Version
}

func (e InvalidRoleError) Error() string {
	return fmt.Sprintf("role %q not permitted by policy %q", e.Role, e.Policy)
}
