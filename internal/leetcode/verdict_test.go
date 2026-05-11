package leetcode

import "testing"

func TestNormalizeVerdict(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Accepted", VerdictCodeAC},
		{"accepted", VerdictCodeAC},
		{"  AC ", VerdictCodeAC},
		{"Wrong Answer", VerdictCodeWA},
		{"Time Limit Exceeded", VerdictCodeTLE},
		{"Memory Limit Exceeded", VerdictCodeMLE},
		{"Compile Error", VerdictCodeCE},
		{"Runtime Error", VerdictCodeRE},
		{"Output Limit Exceeded", VerdictCodeRE},
		{"who knows", VerdictCodeRE},
	}
	for _, c := range cases {
		if got := NormalizeVerdict(c.in); got != c.want {
			t.Errorf("NormalizeVerdict(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
