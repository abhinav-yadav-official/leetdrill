// Package srs implements the review scheduler.
//
// Two pure functions, no DB, no time.Now() inside the core logic — the caller
// passes the clock in. That makes everything trivially testable and means you
// can replay history (recompute all review state from the attempts table) without
// surprises.
//
// The algorithm is SM-2 adapted for DSA problems:
//   - Rating is derived from objective signals (verdict, submission count,
//     time taken) rather than self-assessment.
//   - Four ratings instead of SM-2's 0–5 scale: failed/struggled/normal/strong.
//   - Intervals capped to avoid the "200-day next review" silliness that
//     happens with vanilla SM-2 on items you've nailed five times.
//   - Leech detection: 4+ failures flips a problem to Leech status so the
//     scheduler stops surfacing it in regular rotation.
package srs

import (
	"math"
	"time"
)

// Rating is the derived difficulty signal for a single attempt.
type Rating string

const (
	RatingFailed    Rating = "failed"    // never AC'd, or AC'd after >5 wrong submissions
	RatingStruggled Rating = "struggled" // AC'd but with 3–5 submissions or way over time
	RatingNormal    Rating = "normal"    // AC'd in 2 submissions, reasonable time
	RatingStrong    Rating = "strong"    // first-try AC, under expected time
)

// Status is the lifecycle stage of a (user, problem) pair.
type Status string

const (
	StatusNew      Status = "new"      // never attempted
	StatusLearning Status = "learning" // attempted, interval < 7 days
	StatusReview   Status = "review"   // interval >= 7 days
	StatusMastered Status = "mastered" // interval >= 60 days and ease >= 2.5
	StatusLeech    Status = "leech"    // failed >= 4 times total — removed from rotation
)

// Verdict mirrors LeetCode's submission verdicts.
type Verdict string

const (
	VerdictAC  Verdict = "AC"  // Accepted
	VerdictWA  Verdict = "WA"  // Wrong Answer
	VerdictTLE Verdict = "TLE" // Time Limit Exceeded
	VerdictMLE Verdict = "MLE" // Memory Limit Exceeded
	VerdictRE  Verdict = "RE"  // Runtime Error
	VerdictCE  Verdict = "CE"  // Compile Error
)

// Difficulty mirrors LeetCode's three tiers.
type Difficulty string

const (
	DifficultyEasy   Difficulty = "Easy"
	DifficultyMedium Difficulty = "Medium"
	DifficultyHard   Difficulty = "Hard"
)

// Outcome is the objective record of one solve attempt — everything we need to
// score performance, with no self-reported fields.
type Outcome struct {
	Verdict         Verdict    // final verdict reached in this session
	SubmissionCount int        // how many submissions before reaching final verdict
	TimeTakenSec    int        // wall-clock seconds from page-open to final verdict
	Difficulty      Difficulty // for normalizing "expected" time
}

// State is the review state of a (user, problem) pair before an attempt is applied.
// After scoring, the caller persists the new State returned by NextState.
type State struct {
	EaseFactor   float64 // SM-2 ease; starts at 2.5, range [1.3, 3.0]
	IntervalDays int     // days until next review
	Streak       int     // consecutive clean (Normal or Strong) solves
	TotalFails   int     // lifetime failed attempts — drives leech detection
	Status       Status
}

// NewState returns the initial state for a problem the user has never seen.
func NewState() State {
	return State{
		EaseFactor:   2.5,
		IntervalDays: 0,
		Streak:       0,
		TotalFails:   0,
		Status:       StatusNew,
	}
}

// expectedSolveSec returns a rough wall-clock budget for "fast solve" per
// difficulty. Anything under this and a first-try AC counts as Strong.
//
// These are deliberately generous — better to under-rate occasionally than
// to crush ease factors on a slightly slow but otherwise clean solve.
func expectedSolveSec(d Difficulty) int {
	switch d {
	case DifficultyEasy:
		return 15 * 60
	case DifficultyHard:
		return 40 * 60
	default: // Medium and unknown
		return 25 * 60
	}
}

// DeriveRating turns an objective Outcome into a four-level rating.
//
// The rules, in priority order:
//   - Non-AC verdict → Failed (couldn't solve in this session at all)
//   - >5 submissions to AC → Failed (essentially brute-forced via WA feedback)
//   - 3–5 submissions OR time > 2× expected → Struggled
//   - 2 submissions and reasonable time → Normal
//   - 1 submission and under expected time → Strong
//   - 1 submission but over expected time → Normal (right idea, slow execution)
func DeriveRating(o Outcome) Rating {
	if o.Verdict != VerdictAC {
		return RatingFailed
	}
	if o.SubmissionCount > 5 {
		return RatingFailed
	}

	expected := expectedSolveSec(o.Difficulty)
	wayOverTime := o.TimeTakenSec > 2*expected

	switch {
	case o.SubmissionCount >= 3:
		return RatingStruggled
	case o.SubmissionCount == 2:
		if wayOverTime {
			return RatingStruggled
		}
		return RatingNormal
	default: // SubmissionCount == 1
		if o.TimeTakenSec <= expected {
			return RatingStrong
		}
		return RatingNormal
	}
}

const (
	minEase = 1.3
	maxEase = 3.0

	maxIntervalDays = 180 // cap to avoid runaway intervals on over-mastered items
)

// intervalMultiplier is the per-rating multiplier applied to the current interval.
// Failed resets aggressively. Strong rewards but is bounded by the ease cap.
func intervalMultiplier(r Rating, ease float64) float64 {
	switch r {
	case RatingStrong:
		return ease * 1.1
	case RatingNormal:
		return ease
	case RatingStruggled:
		// Cap at 1.0 — struggling must never *grow* the interval, that
		// defeats the point of the signal. Floor at 0.5 so we don't
		// collapse a long interval to nothing on a single struggle.
		return math.Min(1.0, math.Max(0.5, ease*0.4))
	case RatingFailed:
		return 0 // forces interval = 1
	default:
		return ease
	}
}

// easeDelta is how much the ease factor moves per rating. Matches SM-2's
// general shape but compressed because we have four ratings instead of six.
func easeDelta(r Rating) float64 {
	switch r {
	case RatingStrong:
		return +0.15
	case RatingNormal:
		return 0
	case RatingStruggled:
		return -0.15
	case RatingFailed:
		return -0.20
	default:
		return 0
	}
}

// clampEase keeps the ease factor in a sane range.
func clampEase(e float64) float64 {
	if e < minEase {
		return minEase
	}
	if e > maxEase {
		return maxEase
	}
	return e
}

// firstInterval is the interval (in days) for the very first non-failed solve.
// Stronger first solves earn longer first intervals — this is where the
// "graduating from learning" speed comes from.
func firstInterval(r Rating) int {
	switch r {
	case RatingStrong:
		return 3
	case RatingNormal:
		return 2
	case RatingStruggled:
		return 1
	default:
		return 1
	}
}

// classifyStatus returns the lifecycle status for a state. Pure function of
// the numbers, so it stays consistent whether you call it after NextState or
// when reading from the DB.
func classifyStatus(s State) Status {
	if s.Status == StatusNew {
		return StatusNew
	}
	if s.TotalFails >= 4 {
		return StatusLeech
	}
	switch {
	case s.IntervalDays >= 60 && s.EaseFactor >= 2.5:
		return StatusMastered
	case s.IntervalDays >= 7:
		return StatusReview
	default:
		return StatusLearning
	}
}

// NextState applies a rating to the current review state and returns the new state.
//
// Caller computes next_due_at = now + IntervalDays*24h.
//
// Once a problem hits Leech status, NextState still updates ease/streak/fails
// honestly, but the scheduler should consult Status before queueing it — leech
// problems live in a separate view, not in daily reviews.
func NextState(curr State, r Rating) State {
	// First time seeing this problem (Status == "new") and we have a rating.
	if curr.Status == StatusNew {
		switch r {
		case RatingFailed:
			out := State{
				EaseFactor:   clampEase(curr.EaseFactor + easeDelta(r)),
				IntervalDays: 1,
				Streak:       0,
				TotalFails:   curr.TotalFails + 1,
			}
			out.Status = classifyStatus(out)
			if out.Status == StatusNew {
				out.Status = StatusLearning // any non-new state is at minimum Learning
			}
			return out
		default:
			out := State{
				EaseFactor:   clampEase(curr.EaseFactor + easeDelta(r)),
				IntervalDays: firstInterval(r),
				Streak:       1,
				TotalFails:   curr.TotalFails,
			}
			out.Status = classifyStatus(out)
			if out.Status == StatusNew {
				out.Status = StatusLearning
			}
			return out
		}
	}

	// Subsequent attempts: standard SM-2-ish update.
	newEase := clampEase(curr.EaseFactor + easeDelta(r))

	var newInterval int
	if r == RatingFailed {
		newInterval = 1
	} else {
		base := curr.IntervalDays
		if base < 1 {
			base = 1
		}
		raw := float64(base) * intervalMultiplier(r, newEase)
		newInterval = int(math.Round(raw))
		if newInterval < 1 {
			newInterval = 1
		}
		if newInterval > maxIntervalDays {
			newInterval = maxIntervalDays
		}
	}

	newStreak := curr.Streak + 1
	newFails := curr.TotalFails
	if r == RatingFailed || r == RatingStruggled {
		newStreak = 0
	}
	if r == RatingFailed {
		newFails++
	}

	out := State{
		EaseFactor:   newEase,
		IntervalDays: newInterval,
		Streak:       newStreak,
		TotalFails:   newFails,
	}
	out.Status = classifyStatus(out)
	return out
}

// NextDueAt is a convenience for the caller — keeps time.Now out of the core.
func NextDueAt(s State, from time.Time) time.Time {
	return from.Add(time.Duration(s.IntervalDays) * 24 * time.Hour)
}
