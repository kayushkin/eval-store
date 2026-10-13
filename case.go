package evalstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Case is one real model call, frozen: the prompts and the original answer
// are copied here because their sources may be pruned. SourceKind and
// SourceRef still name where it came from, by the owning store's id.
type Case struct {
	ID                       string             `json:"id"`
	SuiteID                  string             `json:"suite_id"`
	SourceKind               string             `json:"source_kind"`
	SourceRef                string             `json:"source_ref"`
	SystemPrompt             string             `json:"system_prompt,omitempty"`
	UserPrompt               string             `json:"user_prompt,omitempty"`
	OriginalModelID          string             `json:"original_model_id"`
	OriginalAnswer           json.RawMessage    `json:"original_answer,omitempty"`
	OriginalUsageKnown       bool               `json:"original_usage_known"`
	OriginalInputTokens      int64              `json:"original_input_tokens"`
	OriginalCacheReadTokens  int64              `json:"original_cache_read_tokens"`
	OriginalCacheWriteTokens int64              `json:"original_cache_write_tokens"`
	OriginalOutputTokens     int64              `json:"original_output_tokens"`
	OriginalDurationMs       *int64             `json:"original_duration_ms"`
	OriginalAt               int64              `json:"original_at"`
	Features                 map[string]float64 `json:"features"`
	CreatedAt                int64              `json:"created_at"`
	// HasBodies says whether the prompts and the answer were read. A list
	// leaves them out; only GET /cases/{id} carries them.
	HasBodies bool `json:"has_bodies"`
}

// The kinds of source a case can come from.
const (
	SourceKindLLMBridgeOperation   = "llm-bridge-operation"
	SourceKindClaudeCodeTranscript = "claude-code-transcript"
)

var sourceKinds = []string{SourceKindLLMBridgeOperation, SourceKindClaudeCodeTranscript}

const caseSummaryColumns = `id, suite_id, source_kind, source_ref, original_model_id, original_usage_known, original_input_tokens,
	original_cache_read_tokens, original_cache_write_tokens, original_output_tokens, original_duration_ms, original_at, features, created_at`

func scanCase(row interface{ Scan(...any) error }, withBodies bool) (*Case, error) {
	var c Case
	var usageKnown int
	var duration sql.NullInt64
	var features string
	var system, prompt string
	var answer sql.NullString
	targets := []any{&c.ID, &c.SuiteID, &c.SourceKind, &c.SourceRef, &c.OriginalModelID, &usageKnown, &c.OriginalInputTokens,
		&c.OriginalCacheReadTokens, &c.OriginalCacheWriteTokens, &c.OriginalOutputTokens, &duration, &c.OriginalAt, &features, &c.CreatedAt}
	if withBodies {
		targets = append(targets, &system, &prompt, &answer)
	}
	if err := row.Scan(targets...); err != nil {
		return nil, err
	}
	c.OriginalUsageKnown = usageKnown == 1
	c.OriginalDurationMs = intFromNullable(duration)
	if err := decodeJSONColumn(features, &c.Features, "case features"); err != nil {
		return nil, err
	}
	if withBodies {
		c.SystemPrompt, c.UserPrompt, c.OriginalAnswer, c.HasBodies = system, prompt, rawFromNullable(answer), true
	}
	return &c, nil
}

// insertCase stores a case inside tx, computing its features with the
// suite's checker. A case already imported from the same source is a
// conflict, so importing twice adds nothing twice.
func insertCase(tx *sql.Tx, suite *Suite, c *Case, now int64) (string, error) {
	if !containsString(sourceKinds, c.SourceKind) {
		return "", invalidf("case source_kind %q is not one of %v", c.SourceKind, sourceKinds)
	}
	if strings.TrimSpace(c.SourceRef) == "" || c.UserPrompt == "" {
		return "", invalidf("a case needs a source_ref and a user prompt")
	}
	features := map[string]float64{}
	if suite.CheckerName != "" {
		features = checkers[suite.CheckerName].Features(c)
	}
	id, err := insertWithID(tx, "cases", caseIDPrefix,
		`suite_id, source_kind, source_ref, system_prompt, user_prompt, original_model_id, original_answer, original_usage_known,
		 original_input_tokens, original_cache_read_tokens, original_cache_write_tokens, original_output_tokens, original_duration_ms,
		 original_at, features, created_at`,
		"?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?",
		suite.ID, c.SourceKind, c.SourceRef, c.SystemPrompt, c.UserPrompt, c.OriginalModelID, nullableRaw(c.OriginalAnswer), boolToInt(c.OriginalUsageKnown),
		c.OriginalInputTokens, c.OriginalCacheReadTokens, c.OriginalCacheWriteTokens, c.OriginalOutputTokens, nullableInt(c.OriginalDurationMs),
		c.OriginalAt, mustJSON(features), now)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: cases.suite_id") {
		return "", conflictf("suite %s already has a case from %s %s", suite.ID, c.SourceKind, c.SourceRef)
	}
	return id, err
}

// CreateCase stores one case in a suite.
func (s *Store) CreateCase(suiteID string, c *Case) (*Case, error) {
	suite, err := s.GetSuite(suiteID)
	if err != nil {
		return nil, err
	}
	var id string
	if err := s.inTransaction(func(tx *sql.Tx) error {
		id, err = insertCase(tx, suite, c, s.unixNow())
		return err
	}); err != nil {
		return nil, err
	}
	return s.GetCase(id)
}

// GetCase reads one case with its prompts and original answer.
func (s *Store) GetCase(id string) (*Case, error) {
	c, err := scanCase(s.db.QueryRow("SELECT "+caseSummaryColumns+", system_prompt, user_prompt, original_answer FROM cases WHERE id = ?", id), true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: case %s", ErrNotFound, id)
	}
	return c, err
}

// ListCases reads a suite's cases without their prompts or answers.
func (s *Store) ListCases(suiteID string) ([]*Case, error) {
	if _, err := s.GetSuite(suiteID); err != nil {
		return nil, err
	}
	return s.queryCases("SELECT "+caseSummaryColumns+" FROM cases WHERE suite_id = ? ORDER BY seq", false, suiteID)
}

// casesWithBodies reads the named cases with their prompts, in the order
// given. A missing id is an error: a run's case list is frozen.
func (s *Store) casesWithBodies(ids []string) ([]*Case, error) {
	cases := make([]*Case, 0, len(ids))
	for _, id := range ids {
		c, err := s.GetCase(id)
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}

func (s *Store) queryCases(query string, withBodies bool, arguments ...any) ([]*Case, error) {
	rows, err := s.db.Query(query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cases := []*Case{}
	for rows.Next() {
		c, err := scanCase(rows, withBodies)
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, rows.Err()
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
