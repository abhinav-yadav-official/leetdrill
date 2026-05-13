// Package leetcode is a minimal client for LeetCode's unofficial GraphQL API at
// https://leetcode.com/graphql.
//
// IMPORTANT: this endpoint is undocumented and unstable. Treat every field as
// nullable, log every request/response on first error, and don't be surprised
// by occasional schema drift. The queries below are the ones in current use by
// LeetHub, Alfa-Leetcode-API, and leetcode-cli as of 2026 — they've been stable
// for 2+ years but that's no guarantee.
//
// Two client modes:
//   - Public: no auth, works for problem metadata and a user's public profile
//     including recently solved problems. This is the safety-net sync path.
//   - Authed: requires LEETCODE_SESSION and csrftoken cookies (captured by the
//     browser extension). Needed for full submission history and submission code.
//
// Rate limiting: stay under 1 req/sec. Retry 429 and 5xx with exponential
// backoff. Never run unauthed and authed requests in parallel.
package leetcode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	endpoint      = "https://leetcode.com/graphql"
	defaultUA     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36"
	defaultRefer  = "https://leetcode.com"
	requestTimout = 20 * time.Second
)

// Credentials carries the user's LeetCode session cookies. Both fields are
// required for authed queries. Leave empty for public queries.
type Credentials struct {
	Session string // LEETCODE_SESSION cookie value
	CSRF    string // csrftoken cookie value
}

// Client is the GraphQL client. Construct with New().
type Client struct {
	http *http.Client
}

func New() *Client {
	return &Client{
		http: &http.Client{Timeout: requestTimout},
	}
}

// do is the single chokepoint for every GraphQL call. Pass nil creds for
// public queries.
func (c *Client) do(ctx context.Context, query string, variables map[string]any, creds *Credentials, out any) error {
	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", defaultRefer)
	req.Header.Set("User-Agent", defaultUA)
	req.Header.Set("Origin", "https://leetcode.com")

	if creds != nil && creds.Session != "" {
		req.Header.Set("Cookie", fmt.Sprintf("LEETCODE_SESSION=%s; csrftoken=%s", creds.Session, creds.CSRF))
		req.Header.Set("x-csrftoken", creds.CSRF)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ErrAuthExpired
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%w: status %d", ErrServerError, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, string(raw))
	}

	// Wrapper: GraphQL returns { data: ..., errors: [...] }
	var envelope struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("unmarshal envelope: %w (body: %s)", err, string(raw))
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("graphql errors: %s", string(raw))
	}
	if out != nil {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("unmarshal data: %w (data: %s)", err, string(envelope.Data))
		}
	}
	return nil
}

// Sentinel errors. The sync worker checks for ErrAuthExpired specifically to
// flag the user's cookie as stale.
var (
	ErrAuthExpired = errors.New("leetcode: auth expired")
	ErrRateLimited = errors.New("leetcode: rate limited")
	ErrServerError = errors.New("leetcode: server error")
)

// =============================================================================
// Query 1: problemsetQuestionList — bulk problem ingestion
//
// One-time + incremental: pulls the full problem catalog. ~3300 problems as of
// 2026. Paginate by skip+limit, 50 at a time. ac_rate and difficulty come back
// here; topicTags too. This is the *only* query you need for the cold-start
// problem-DB ingestion script.
// =============================================================================

const queryProblemList = `
query problemsetQuestionList(
  $categorySlug: String, $limit: Int, $skip: Int, $filters: QuestionListFilterInput
) {
  problemsetQuestionList: questionList(
    categorySlug: $categorySlug
    limit: $limit
    skip: $skip
    filters: $filters
  ) {
    total: totalNum
    questions: data {
      questionId
      questionFrontendId
      title
      titleSlug
      difficulty
      isPaidOnly
      acRate
      topicTags { name slug }
    }
  }
}`

type ProblemListItem struct {
	QuestionID         string  `json:"questionId"`
	QuestionFrontendID string  `json:"questionFrontendId"`
	Title              string  `json:"title"`
	TitleSlug          string  `json:"titleSlug"`
	Difficulty         string  `json:"difficulty"`
	IsPaidOnly         bool    `json:"isPaidOnly"`
	ACRate             float64 `json:"acRate"`
	TopicTags          []Tag   `json:"topicTags"`
}

type Tag struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// ListProblems returns one page. Caller loops with increasing skip.
func (c *Client) ListProblems(ctx context.Context, skip, limit int) ([]ProblemListItem, int, error) {
	var resp struct {
		ProblemsetQuestionList struct {
			Total     int               `json:"total"`
			Questions []ProblemListItem `json:"questions"`
		} `json:"problemsetQuestionList"`
	}
	vars := map[string]any{
		"categorySlug": "",
		"skip":         skip,
		"limit":        limit,
		"filters":      map[string]any{},
	}
	if err := c.do(ctx, queryProblemList, vars, nil, &resp); err != nil {
		return nil, 0, err
	}
	return resp.ProblemsetQuestionList.Questions, resp.ProblemsetQuestionList.Total, nil
}

const querySolvedProblemList = `
query solvedProblemsetQuestionList($limit: Int, $skip: Int) {
  problemsetQuestionList: questionList(
    categorySlug: ""
    limit: $limit
    skip: $skip
    filters: { status: AC }
  ) {
    total: totalNum
    questions: data {
      questionId
      questionFrontendId
      title
      titleSlug
      difficulty
      isPaidOnly
      acRate
      topicTags { name slug }
    }
  }
}`

func (c *Client) ListSolvedProblems(ctx context.Context, creds Credentials, skip, limit int) ([]ProblemListItem, int, error) {
	if creds.Session == "" {
		return nil, 0, errors.New("solved problem list requires authed credentials")
	}
	var resp struct {
		ProblemsetQuestionList struct {
			Total     int               `json:"total"`
			Questions []ProblemListItem `json:"questions"`
		} `json:"problemsetQuestionList"`
	}
	vars := map[string]any{
		"skip":  skip,
		"limit": limit,
	}
	if err := c.do(ctx, querySolvedProblemList, vars, &creds, &resp); err != nil {
		return nil, 0, err
	}
	return resp.ProblemsetQuestionList.Questions, resp.ProblemsetQuestionList.Total, nil
}

// =============================================================================
// Query 2: question — full problem details
//
// Use when you need the problem statement (HTML), example test cases, hints,
// or topic tags. You probably don't need to call this for every problem —
// only ones you're about to surface in the session UI.
// =============================================================================

const queryQuestion = `
query questionData($titleSlug: String!) {
  question(titleSlug: $titleSlug) {
    questionId
    questionFrontendId
    title
    titleSlug
    content
    difficulty
    isPaidOnly
    likes
    dislikes
    similarQuestions
    topicTags { name slug }
    codeSnippets { lang langSlug code }
    sampleTestCase
    exampleTestcases
    hints
  }
}`

type Question struct {
	QuestionID         string        `json:"questionId"`
	QuestionFrontendID string        `json:"questionFrontendId"`
	Title              string        `json:"title"`
	TitleSlug          string        `json:"titleSlug"`
	Content            string        `json:"content"`
	Difficulty         string        `json:"difficulty"`
	IsPaidOnly         bool          `json:"isPaidOnly"`
	Likes              int           `json:"likes"`
	Dislikes           int           `json:"dislikes"`
	SimilarQuestions   string        `json:"similarQuestions"` // JSON-encoded string
	TopicTags          []Tag         `json:"topicTags"`
	CodeSnippets       []CodeSnippet `json:"codeSnippets"`
	SampleTestCase     string        `json:"sampleTestCase"`
	ExampleTestcases   string        `json:"exampleTestcases"`
	Hints              []string      `json:"hints"`
}

type CodeSnippet struct {
	Lang     string `json:"lang"`
	LangSlug string `json:"langSlug"`
	Code     string `json:"code"`
}

func (c *Client) GetQuestion(ctx context.Context, slug string) (*Question, error) {
	var resp struct {
		Question Question `json:"question"`
	}
	vars := map[string]any{"titleSlug": slug}
	if err := c.do(ctx, queryQuestion, vars, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Question, nil
}

// =============================================================================
// Query 3: matchedUser — public profile stats
//
// Returns aggregate solve counts (easy/medium/hard) and beats-percentage.
// Cheap, public, good for the dashboard "you've solved X total" header and for
// cold-start: tagProblemCounts gives you a per-topic distribution to seed
// pattern strength estimates.
// =============================================================================

const queryMatchedUser = `
query getUserProfile($username: String!) {
  matchedUser(username: $username) {
    username
    profile { reputation ranking }
    submitStats: submitStatsGlobal {
      acSubmissionNum { difficulty count submissions }
      totalSubmissionNum { difficulty count submissions }
    }
    tagProblemCounts {
      advanced { tagName tagSlug problemsSolved }
      intermediate { tagName tagSlug problemsSolved }
      fundamental { tagName tagSlug problemsSolved }
    }
  }
}`

type MatchedUser struct {
	Username string `json:"username"`
	Profile  struct {
		Reputation int `json:"reputation"`
		Ranking    int `json:"ranking"`
	} `json:"profile"`
	SubmitStats struct {
		ACSubmissionNum    []SubmitCount `json:"acSubmissionNum"`
		TotalSubmissionNum []SubmitCount `json:"totalSubmissionNum"`
	} `json:"submitStats"`
	TagProblemCounts struct {
		Advanced     []TagCount `json:"advanced"`
		Intermediate []TagCount `json:"intermediate"`
		Fundamental  []TagCount `json:"fundamental"`
	} `json:"tagProblemCounts"`
}

type SubmitCount struct {
	Difficulty  string `json:"difficulty"`
	Count       int    `json:"count"`
	Submissions int    `json:"submissions"`
}

type TagCount struct {
	TagName        string `json:"tagName"`
	TagSlug        string `json:"tagSlug"`
	ProblemsSolved int    `json:"problemsSolved"`
}

func (c *Client) GetUser(ctx context.Context, username string) (*MatchedUser, error) {
	var resp struct {
		MatchedUser *MatchedUser `json:"matchedUser"`
	}
	vars := map[string]any{"username": username}
	if err := c.do(ctx, queryMatchedUser, vars, nil, &resp); err != nil {
		return nil, err
	}
	if resp.MatchedUser == nil {
		return nil, fmt.Errorf("user not found: %s", username)
	}
	return resp.MatchedUser, nil
}

// =============================================================================
// Query 4: recentAcSubmissionList — recent accepted submissions (public)
//
// Up to 20 most recent ACs for a public profile. This is the sync worker's
// primary tool: poll every 30 min, find new entries by timestamp, update the review schedule
// updates. The endpoint is unauthed which is great — no cookie expiry risk.
// =============================================================================

const queryRecentAC = `
query recentAcSubmissions($username: String!, $limit: Int!) {
  recentAcSubmissionList(username: $username, limit: $limit) {
    id
    title
    titleSlug
    timestamp
  }
}`

type RecentSubmission struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	TitleSlug string `json:"titleSlug"`
	Timestamp string `json:"timestamp"` // unix seconds as string — LeetCode is weird
}

func (c *Client) RecentACSubmissions(ctx context.Context, username string, limit int) ([]RecentSubmission, error) {
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	var resp struct {
		RecentAcSubmissionList []RecentSubmission `json:"recentAcSubmissionList"`
	}
	vars := map[string]any{"username": username, "limit": limit}
	if err := c.do(ctx, queryRecentAC, vars, nil, &resp); err != nil {
		return nil, err
	}
	return resp.RecentAcSubmissionList, nil
}

// =============================================================================
// Query 5: submissionList — full submission history (AUTHED)
//
// Paginated through lastKey + hasNext. Includes WAs/TLEs/etc, not just ACs.
// Useful for the cold-start backfill: importing not just what was solved but
// how much struggle it took. Captures the "first-AC after 4 WAs" signal that
// gives us better review ratings than just AC/no-AC.
// =============================================================================

const querySubmissionList = `
query submissionList(
  $offset: Int!, $limit: Int!, $lastKey: String, $questionSlug: String
) {
  submissionList(
    offset: $offset
    limit: $limit
    lastKey: $lastKey
    questionSlug: $questionSlug
  ) {
    lastKey
    hasNext
    submissions {
      id
      title
      titleSlug
      statusDisplay
      lang
      runtime
      timestamp
      url
      isPending
      memory
    }
  }
}`

type SubmissionListEntry struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	TitleSlug     string `json:"titleSlug"`
	StatusDisplay string `json:"statusDisplay"` // "Accepted", "Wrong Answer", etc.
	Lang          string `json:"lang"`
	Runtime       string `json:"runtime"` // human-formatted, e.g. "52 ms"
	Timestamp     string `json:"timestamp"`
	URL           string `json:"url"`
	IsPending     string `json:"isPending"`
	Memory        string `json:"memory"`
}

type SubmissionListPage struct {
	LastKey     string                `json:"lastKey"`
	HasNext     bool                  `json:"hasNext"`
	Submissions []SubmissionListEntry `json:"submissions"`
}

func (c *Client) ListSubmissions(
	ctx context.Context, creds Credentials,
	offset, limit int, lastKey, questionSlug string,
) (*SubmissionListPage, error) {
	if creds.Session == "" {
		return nil, errors.New("submission list requires authed credentials")
	}
	vars := map[string]any{
		"offset":       offset,
		"limit":        limit,
		"lastKey":      lastKey,
		"questionSlug": questionSlug,
	}
	var resp struct {
		SubmissionList SubmissionListPage `json:"submissionList"`
	}
	if err := c.do(ctx, querySubmissionList, vars, &creds, &resp); err != nil {
		return nil, err
	}
	return &resp.SubmissionList, nil
}

// =============================================================================
// Query 6: submissionDetails — code + full verdict for one submission (AUTHED)
//
// Fetches the actual submitted code, runtime distribution, memory distribution.
// You almost certainly don't need this in the hot path — the extension
// captures code at submission time. Use it for backfilling the code field of
// historical attempts surfaced by submissionList.
// =============================================================================

const querySubmissionDetails = `
query submissionDetails($submissionId: Int!) {
  submissionDetails(submissionId: $submissionId) {
    runtime
    runtimeDisplay
    runtimePercentile
    memory
    memoryDisplay
    memoryPercentile
    code
    timestamp
    statusCode
    lang { name verboseName }
    question { questionId titleSlug }
    totalCorrect
    totalTestcases
  }
}`

type SubmissionDetails struct {
	Runtime           int     `json:"runtime"`
	RuntimeDisplay    string  `json:"runtimeDisplay"`
	RuntimePercentile float64 `json:"runtimePercentile"`
	Memory            int     `json:"memory"`
	MemoryDisplay     string  `json:"memoryDisplay"`
	MemoryPercentile  float64 `json:"memoryPercentile"`
	Code              string  `json:"code"`
	Timestamp         int64   `json:"timestamp"`
	StatusCode        int     `json:"statusCode"`
	Lang              struct {
		Name        string `json:"name"`
		VerboseName string `json:"verboseName"`
	} `json:"lang"`
	Question struct {
		QuestionID string `json:"questionId"`
		TitleSlug  string `json:"titleSlug"`
	} `json:"question"`
	TotalCorrect   int `json:"totalCorrect"`
	TotalTestcases int `json:"totalTestcases"`
}

func (c *Client) GetSubmissionDetails(
	ctx context.Context, creds Credentials, submissionID int,
) (*SubmissionDetails, error) {
	if creds.Session == "" {
		return nil, errors.New("submission details requires authed credentials")
	}
	vars := map[string]any{"submissionId": submissionID}
	var resp struct {
		SubmissionDetails SubmissionDetails `json:"submissionDetails"`
	}
	if err := c.do(ctx, querySubmissionDetails, vars, &creds, &resp); err != nil {
		return nil, err
	}
	return &resp.SubmissionDetails, nil
}
