package sqlite

import (
	"errors"
	"sync"
)

// Fault points understood by the store. Injecting a fault at one of these
// points causes the current transaction to fail, which must roll back every
// write performed so far.
const (
	FaultAfterSaveCeremony  = "save-ceremony"
	FaultAfterSaveScope     = "save-scope"
	FaultAfterSaveWitness   = "save-witness"
	FaultAfterSaveSession   = "save-session"
	FaultAfterConsumeToken  = "consume-token"
	FaultAfterSaveArtifact  = "save-artifact"
	FaultAfterSaveOperation = "save-operation"
	FaultBeforeCommit       = "before-commit"
)

// FaultInjector is a concurrency-safe registry of injected failures used by
// tests to exercise rollback and recovery behaviour.
type FaultInjector struct {
	mu     sync.RWMutex
	faults map[string]error
}

// NewFaultInjector returns an empty injector.
func NewFaultInjector() *FaultInjector {
	return &FaultInjector{faults: make(map[string]error)}
}

// Set arms the fault at the named point.
func (f *FaultInjector) Set(point string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults[point] = err
}

// Clear removes all armed faults.
func (f *FaultInjector) Clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults = make(map[string]error)
}

// Fail returns the armed error for a point, or nil when none is armed.
func (f *FaultInjector) Fail(point string) error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.faults[point]
}

// ErrInjectedFault is the default error used by tests when arming a fault.
var ErrInjectedFault = errors.New("injected fault")
