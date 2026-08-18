package service

import (
	"context"
	"fmt"

	"quorumforge/internal/ceremony"
	"quorumforge/internal/store"
)

// ReviewRequest records the artifact review conclusion.
type ReviewRequest struct {
	CeremonyID   ceremony.ID               `json:"ceremony_id"`
	Operation    string                    `json:"operation"`
	Revision     ceremony.Revision         `json:"revision"`
	Conclusion   ceremony.ReviewConclusion `json:"conclusion"`
	ReviewDigest string                    `json:"review_digest"`
}

// ReviewResult is returned after recording a review conclusion.
type ReviewResult struct {
	Conclusion   ceremony.ReviewConclusion `json:"conclusion"`
	ReviewDigest string                    `json:"review_digest"`
	State        ceremony.State            `json:"state"`
	Revision     ceremony.Revision         `json:"revision"`
}

// Review records the artifact review conclusion and stamps the review digest
// onto both the aggregate and the registered artifact.
func (s *Service) Review(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	if req.CeremonyID == "" || req.Operation == "" {
		return ReviewResult{}, fmt.Errorf("%w: missing required fields", ErrInvalidArgument)
	}
	if !req.Conclusion.Valid() || req.Conclusion == ceremony.ReviewPending {
		return ReviewResult{}, fmt.Errorf("%w: invalid review conclusion", ErrInvalidArgument)
	}

	content := contentOf(string(req.Conclusion), req.ReviewDigest)

	return runCommand(s, ctx, commandSpec{
		id:        req.CeremonyID,
		operation: req.Operation,
		kind:      "review",
		content:   content,
	}, func(tx store.Tx) (ReviewResult, error) {
		c, err := tx.LoadCeremony(ctx, req.CeremonyID)
		if err != nil {
			return ReviewResult{}, err
		}
		if c.State != ceremony.StatePendingReview {
			return ReviewResult{}, fmt.Errorf("review not allowed in state %s", c.State)
		}
		if err := c.GuardRevision(req.Revision); err != nil {
			return ReviewResult{}, err
		}

		art, err := tx.LoadArtifact(ctx, req.CeremonyID)
		if err != nil {
			return ReviewResult{}, err
		}
		art.ReviewDigest = req.ReviewDigest
		if err := tx.SaveArtifact(req.CeremonyID, *art); err != nil {
			return ReviewResult{}, err
		}

		c.ReviewConclusion = req.Conclusion
		c.ReviewDigest = req.ReviewDigest
		c.BumpRevision()
		if err := tx.SaveCeremony(c); err != nil {
			return ReviewResult{}, err
		}

		return ReviewResult{
			Conclusion:   req.Conclusion,
			ReviewDigest: req.ReviewDigest,
			State:        c.State,
			Revision:     c.Revision,
		}, nil
	})
}
