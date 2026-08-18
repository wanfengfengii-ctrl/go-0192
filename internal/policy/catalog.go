package policy

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrThresholdInvalid is returned when a policy threshold is not positive.
var ErrThresholdInvalid = errors.New("policy threshold must be positive")

// Snapshot is the immutable view captured at ceremony lock time. It binds a
// policy version, its threshold, roles and allowed key version to a frozen set
// of participant identities (each carrying its revision at lock time).
type Snapshot struct {
	PolicyVersion     Version
	Threshold         int
	AllowedRoles      []Role
	AllowedKeyVersion KeyVersion
	Participants      map[PersonID]Participant
}

// Catalog is a concurrency-safe in-memory directory of policy versions and
// participant identities. It is the source consulted when a ceremony is locked.
type Catalog struct {
	mu           sync.RWMutex
	policies     map[Version]Policy
	participants map[PersonID]Participant
}

// NewCatalog returns an empty policy and participant catalog.
func NewCatalog() *Catalog {
	return &Catalog{
		policies:     make(map[Version]Policy),
		participants: make(map[PersonID]Participant),
	}
}

// SetPolicy stores a policy version, validating its threshold first.
func (c *Catalog) SetPolicy(p Policy) error {
	if p.Threshold <= 0 {
		return fmt.Errorf("%w: %s", ErrThresholdInvalid, p.Version)
	}
	if p.AllowedKeyVersion == "" {
		return fmt.Errorf("policy %s missing allowed key version", p.Version)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := p
	cp.AllowedRoles = append([]Role(nil), p.AllowedRoles...)
	c.policies[p.Version] = cp
	return nil
}

// Policy returns the policy for a version.
func (c *Catalog) Policy(v Version) (Policy, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.policies[v]
	if !ok {
		return Policy{}, ErrPolicyNotFound
	}
	cp := p
	cp.AllowedRoles = append([]Role(nil), p.AllowedRoles...)
	return cp, nil
}

// SetParticipant stores or updates a participant identity. An update bumps the
// revision so stale confirmations referencing an older identity are rejected.
func (c *Catalog) SetParticipant(p Participant) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := p
	cp.Credentials = append([]string(nil), p.Credentials...)
	c.participants[p.PersonID] = cp
}

// RevokeParticipant marks a participant ineligible and bumps its revision.
func (c *Catalog) RevokeParticipant(id PersonID) (Participant, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.participants[id]
	if !ok {
		return Participant{}, ErrParticipantNotFound
	}
	p.Revoked = true
	p.Revision++
	c.participants[id] = p
	return p, nil
}

// Participant returns the current identity for a natural person.
func (c *Catalog) Participant(id PersonID) (Participant, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.participants[id]
	if !ok {
		return Participant{}, ErrParticipantNotFound
	}
	cp := p
	cp.Credentials = append([]string(nil), p.Credentials...)
	return cp, nil
}

// ListParticipants returns all participants sorted by person id for stability.
func (c *Catalog) ListParticipants() []Participant {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Participant, 0, len(c.participants))
	for _, p := range c.participants {
		cp := p
		cp.Credentials = append([]string(nil), p.Credentials...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PersonID < out[j].PersonID })
	return out
}

// Snapshot freezes a policy version and a set of participant identities. It
// returns an error when the policy is missing, a participant is unknown, or a
// participant's role is not permitted by the policy.
func (c *Catalog) Snapshot(policyVersion Version, keyVersion KeyVersion, personIDs []PersonID) (Snapshot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	p, ok := c.policies[policyVersion]
	if !ok {
		return Snapshot{}, ErrPolicyNotFound
	}
	if !p.AllowsKeyVersion(keyVersion) {
		return Snapshot{}, fmt.Errorf("%w: policy %s allows %s", ErrKeyVersionMismatch, policyVersion, p.AllowedKeyVersion)
	}

	snap := Snapshot{
		PolicyVersion:     policyVersion,
		Threshold:         p.Threshold,
		AllowedRoles:      append([]Role(nil), p.AllowedRoles...),
		AllowedKeyVersion: p.AllowedKeyVersion,
		Participants:      make(map[PersonID]Participant, len(personIDs)),
	}

	seen := make(map[PersonID]bool, len(personIDs))
	for _, id := range personIDs {
		if seen[id] {
			return Snapshot{}, fmt.Errorf("duplicate participant %q in scope", id)
		}
		seen[id] = true
		part, ok := c.participants[id]
		if !ok {
			return Snapshot{}, fmt.Errorf("%w: %s", ErrParticipantNotFound, id)
		}
		if !p.AllowsRole(part.Role) {
			return Snapshot{}, fmt.Errorf("%w", InvalidRoleError{Role: part.Role, Policy: policyVersion})
		}
		part.Credentials = append([]string(nil), part.Credentials...)
		snap.Participants[id] = part
	}
	return snap, nil
}

// InScope reports whether the person belongs to the frozen participant set.
func (s Snapshot) InScope(id PersonID) bool {
	_, ok := s.Participants[id]
	return ok
}
