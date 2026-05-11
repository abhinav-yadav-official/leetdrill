package leetcode

import "strings"

// Six-letter verdict codes. Match the CHECK constraint on attempts.verdict
// and the srs.Verdict values; we keep these as plain strings here so this
// package has no dependency on internal/srs.
const (
	VerdictCodeAC  = "AC"
	VerdictCodeWA  = "WA"
	VerdictCodeTLE = "TLE"
	VerdictCodeMLE = "MLE"
	VerdictCodeRE  = "RE"
	VerdictCodeCE  = "CE"
)

// NormalizeVerdict maps LeetCode's human verdict strings (returned by the
// /submissions/detail/<id>/check/ poll as `status_msg`) to our compact codes.
//
// Unknown verdicts fall through to RE so we still record an attempt; the code
// is conservative because LeetCode occasionally adds new statuses (e.g.
// "Output Limit Exceeded") and we don't want to drop the row.
func NormalizeVerdict(s string) string {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "accepted", "ac":
		return VerdictCodeAC
	case "wrong answer", "wa":
		return VerdictCodeWA
	case "time limit exceeded", "tle":
		return VerdictCodeTLE
	case "memory limit exceeded", "mle":
		return VerdictCodeMLE
	case "compile error", "ce":
		return VerdictCodeCE
	case "runtime error", "re", "output limit exceeded":
		return VerdictCodeRE
	}
	return VerdictCodeRE
}
