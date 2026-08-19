package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"quorumforge/internal/ceremony"
	"quorumforge/internal/service"
)

// writeJSON writes a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError maps an error to a stable HTTP status and JSON body.
func writeError(w http.ResponseWriter, err error) {
	status, message := mapError(err)
	writeJSON(w, status, map[string]string{"error": message})
}

// mapError converts a domain error into an HTTP status and message.
func mapError(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrNotFound), errors.Is(err, ceremony.ErrNotLocked):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, service.ErrAlreadyExists),
		errors.Is(err, service.ErrOperationConflict),
		errors.Is(err, ceremony.ErrStaleRevision),
		errors.Is(err, ceremony.ErrTerminal),
		errors.Is(err, ceremony.ErrIllegalTransition),
		errors.Is(err, service.ErrSessionExists),
		errors.Is(err, service.ErrTokenReused),
		errors.Is(err, service.ErrArtifactExists),
		errors.Is(err, service.ErrReviewExists),
		errors.Is(err, service.ErrDuplicateWitness),
		errors.Is(err, service.ErrQuorumNotReached),
		errors.Is(err, service.ErrNoSession):
		return http.StatusConflict, err.Error()
	case errors.Is(err, service.ErrInvalidArgument):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, service.ErrRevoked),
		errors.Is(err, service.ErrIdentityMismatch),
		errors.Is(err, service.ErrRoleNotAllowed):
		return http.StatusForbidden, err.Error()
	default:
		return http.StatusInternalServerError, err.Error()
	}
}

// decode reads and decodes a JSON request body.
func decode(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "quorumforge"})
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req service.CreateRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	c, err := s.svc.Create(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id := ceremony.ID(r.PathValue("id"))
	v, err := s.svc.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewResponse(v))
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	var req service.LockRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.CeremonyID = ceremony.ID(r.PathValue("id"))
	res, err := s.svc.Lock(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleWitness(w http.ResponseWriter, r *http.Request) {
	var req service.ConfirmRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.CeremonyID = ceremony.ID(r.PathValue("id"))
	res, err := s.svc.ConfirmWitness(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleBegin(w http.ResponseWriter, r *http.Request) {
	var req service.BeginRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.CeremonyID = ceremony.ID(r.PathValue("id"))
	res, err := s.svc.BeginSignature(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleReceipt(w http.ResponseWriter, r *http.Request) {
	var req service.ReceiptRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.CeremonyID = ceremony.ID(r.PathValue("id"))
	res, err := s.svc.RecordReceipt(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	var req service.ArtifactRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.CeremonyID = ceremony.ID(r.PathValue("id"))
	res, err := s.svc.RegisterArtifact(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	var req service.ReviewRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.CeremonyID = ceremony.ID(r.PathValue("id"))
	res, err := s.svc.Review(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSeal(w http.ResponseWriter, r *http.Request) {
	var req service.SealRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.CeremonyID = ceremony.ID(r.PathValue("id"))
	res, err := s.svc.Seal(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleQuarantine(w http.ResponseWriter, r *http.Request) {
	var req service.QuarantineRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.CeremonyID = ceremony.ID(r.PathValue("id"))
	res, err := s.svc.Quarantine(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	var req service.CancelRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	req.CeremonyID = ceremony.ID(r.PathValue("id"))
	res, err := s.svc.Cancel(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// viewResponse converts a service view into a JSON-friendly shape.
func viewResponse(v service.View) map[string]any {
	participants := make([]string, 0, len(v.Scope))
	for _, e := range v.Scope {
		participants = append(participants, e.PersonID)
	}
	return map[string]any{
		"id":             v.Ceremony.ID,
		"state":          v.Ceremony.State.String(),
		"revision":       v.Ceremony.Revision,
		"digest":         v.Ceremony.RequestDigest,
		"key_version":    v.Ceremony.KeyVersion,
		"policy_version": v.Ceremony.PolicyVersion,
		"participants":   participants,
		"review":         v.Ceremony.ReviewConclusion,
		"review_digest":  v.Ceremony.ReviewDigest,
		"witness_count":  v.WitnessCount,
		"threshold":      v.Threshold,
		"quorum_reached": v.QuorumReached,
		"session":        v.Session,
		"artifact":       v.Artifact,
	}
}
