package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"leetdrill/internal/models"
	"leetdrill/internal/srs"
)

// ApplyInput is everything the Apply pipeline needs that isn't already in DB.
type ApplyInput struct {
	UserID    int64
	ProblemID int64

	// Difficulty is needed by DeriveRating; pass it through to avoid an extra
	// lookup. Caller (handler) already fetched the problem to validate it.
	Difficulty models.Difficulty

	StartedAt       time.Time
	CompletedAt     time.Time
	Verdict         string // AC/WA/TLE/MLE/RE/CE
	SubmissionCount int
	TimeTakenSec    int
	RuntimeMs       *int
	MemoryKB        *int
	Language        string
	Code            string
	Journal         string
	MistakeTags     []string
	LeetcodeSubmID  string

	// Now is injected for testability; if zero, time.Now() is used.
	Now time.Time
}

// ApplyResult bundles what the pipeline produced.
type ApplyResult struct {
	AttemptID   int64
	Rating      srs.Rating
	UserProblem models.UserProblem
}

// Apply records an attempt and advances the review schedule atomically.
func (s *Store) Apply(ctx context.Context, in ApplyInput) (ApplyResult, error) {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if in.CompletedAt.IsZero() {
		in.CompletedAt = now
	}

	outcome := srs.Outcome{
		Verdict:         srs.Verdict(in.Verdict),
		SubmissionCount: in.SubmissionCount,
		TimeTakenSec:    in.TimeTakenSec,
		Difficulty:      srs.Difficulty(in.Difficulty),
	}
	rating := srs.DeriveRating(outcome)

	var result ApplyResult
	result.Rating = rating

	err := s.InTx(ctx, func(tx pgx.Tx) error {
		current, err := GetUserProblem(ctx, tx, in.UserID, in.ProblemID)
		if err != nil {
			return err
		}

		next := srs.NextState(srs.State{
			EaseFactor:   current.EaseFactor,
			IntervalDays: current.IntervalDays,
			Streak:       current.Streak,
			TotalFails:   current.TotalFails,
			Status:       srs.Status(current.Status),
		}, rating)

		due := srs.NextDueAt(next, now)

		updated := models.UserProblem{
			UserID:          in.UserID,
			ProblemID:       in.ProblemID,
			EaseFactor:      next.EaseFactor,
			IntervalDays:    next.IntervalDays,
			NextDueAt:       &due,
			LastAttemptedAt: timeOrNil(in.CompletedAt),
			TotalAttempts:   current.TotalAttempts + 1,
			CleanSolves:     current.CleanSolves,
			TotalFails:      next.TotalFails,
			Streak:          next.Streak,
			Status:          models.Status(next.Status),
		}
		if rating == srs.RatingNormal || rating == srs.RatingStrong {
			updated.CleanSolves++
		}

		if err := UpsertUserProblem(ctx, tx, updated); err != nil {
			return err
		}

		attempt := models.Attempt{
			UserID:                   in.UserID,
			ProblemID:                in.ProblemID,
			StartedAt:                timeOrNil(in.StartedAt),
			CompletedAt:              in.CompletedAt,
			Verdict:                  in.Verdict,
			SubmissionCountInSession: in.SubmissionCount,
			TimeTakenSec:             in.TimeTakenSec,
			RuntimeMs:                in.RuntimeMs,
			MemoryKB:                 in.MemoryKB,
			Language:                 in.Language,
			Code:                     in.Code,
			DerivedRating:            string(rating),
			Journal:                  in.Journal,
			MistakeTags:              in.MistakeTags,
			LeetcodeSubmissionID:     in.LeetcodeSubmID,
		}
		id, err := InsertAttempt(ctx, tx, attempt)
		if err != nil {
			return fmt.Errorf("apply: %w", err)
		}
		result.AttemptID = id
		result.UserProblem = updated
		return nil
	})
	return result, err
}
