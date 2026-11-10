// Package rules implements the M3 regex rule engine (PRD section 7.5, plan
// docs/plans/M3-rules.md D2/D3): rule loading, RE2 matching, evaluation
// precedence (manual/transfer are never touched; 0 matches → none, 1 → rule,
// >1 → remembered conflict_resolution if still valid, else conflict), plus the
// read-only live preview (RULE-3) and edit diff (RULE-4).
//
// All matching happens in Go with RE2 (linear time, no catastrophic
// backtracking, NFR-8), case-sensitive unless the pattern opts into (?i).
// Previews, diffs and re-evaluation share this one code path.
package rules

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"

	"github.com/mrFred489/financeanalysis/internal/actions"
	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

// MaxPatternLength caps the regex size (NFR-8 risk mitigation).
const MaxPatternLength = 512

// sampleSize is the number of sample line IDs per preview group (plan D6).
const sampleSize = 5

// Rule is a rule loaded from the DB, with its account filter resolved.
type Rule struct {
	ID            int64
	Name          string
	Pattern       string
	Direction     string // "in" (amount > 0), "out" (amount < 0), "both"
	SubcategoryID int64
	Enabled       bool
	AccountIDs    []int64 // empty = all accounts
}

// Line is the minimal statement_line view the evaluator matches against.
type Line struct {
	ID            int64
	AccountID     int64
	Message       string
	AmountMinor   int64
	Source        string
	SubcategoryID sql.NullInt64
	RuleID        sql.NullInt64
}

// Compile compiles a rule pattern with RE2 (case-sensitive by default) and
// enforces the length cap. The compiler error is returned for RULE-2.
func Compile(pattern string) (*regexp.Regexp, error) {
	if len(pattern) > MaxPatternLength {
		return nil, fmt.Errorf("pattern is too long (max %d characters)", MaxPatternLength)
	}
	return regexp.Compile(pattern)
}

// ValidDirection reports whether d is a legal rule direction.
func ValidDirection(d string) bool {
	return d == "in" || d == "out" || d == "both"
}

// Matches reports whether the rule matches the line: direction filter (sign of
// the amount), account filter (empty = all), then the regex against the message.
func Matches(r *Rule, re *regexp.Regexp, l *Line) bool {
	if re == nil {
		return false
	}
	switch r.Direction {
	case "in":
		if l.AmountMinor <= 0 {
			return false
		}
	case "out":
		if l.AmountMinor >= 0 {
			return false
		}
	}
	if len(r.AccountIDs) > 0 {
		found := false
		for _, id := range r.AccountIDs {
			if id == l.AccountID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return re.MatchString(l.Message)
}

// compiledRule pairs a rule with its compiled pattern.
type compiledRule struct {
	rule *Rule
	re   *regexp.Regexp
}

// LoadRules loads all rules with their account filters.
func LoadRules(ctx context.Context, q *dbq.Queries) ([]Rule, error) {
	rows, err := q.ListRules(ctx, dbq.ListRulesParams{})
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	accounts, err := q.ListRuleAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list rule accounts: %w", err)
	}
	byRule := map[int64][]int64{}
	for _, ra := range accounts {
		byRule[ra.RuleID] = append(byRule[ra.RuleID], ra.AccountID)
	}
	out := make([]Rule, 0, len(rows))
	for _, r := range rows {
		out = append(out, Rule{
			ID: r.ID, Name: r.Name, Pattern: r.Pattern, Direction: r.Direction,
			SubcategoryID: r.SubcategoryID, Enabled: r.Enabled == 1,
			AccountIDs: byRule[r.ID],
		})
	}
	return out, nil
}

// LoadRule loads one rule with its account filter. Returns sql.ErrNoRows when missing.
func LoadRule(ctx context.Context, q *dbq.Queries, id int64) (*Rule, error) {
	r, err := q.GetRule(ctx, id)
	if err != nil {
		return nil, err
	}
	accs, err := q.ListAccountsForRule(ctx, id)
	if err != nil {
		return nil, err
	}
	rule := &Rule{
		ID: r.ID, Name: r.Name, Pattern: r.Pattern, Direction: r.Direction,
		SubcategoryID: r.SubcategoryID, Enabled: r.Enabled == 1,
	}
	rule.AccountIDs = append(rule.AccountIDs, accs...)
	return rule, nil
}

// loadCompiled loads all rules and compiles their patterns. Rules whose stored
// pattern no longer compiles (cannot happen via the API, which validates on
// write) are skipped so one bad row cannot break evaluation.
func loadCompiled(ctx context.Context, q *dbq.Queries) ([]compiledRule, error) {
	rules, err := LoadRules(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]compiledRule, 0, len(rules))
	for i := range rules {
		re, err := Compile(rules[i].Pattern)
		if err != nil {
			continue
		}
		out = append(out, compiledRule{rule: &rules[i], re: re})
	}
	return out, nil
}

// loadResolutions returns line_id → rule_id for all conflict_resolution rows.
func loadResolutions(ctx context.Context, q *dbq.Queries) (map[int64]int64, error) {
	rows, err := q.ListConflictResolutions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list conflict resolutions: %w", err)
	}
	m := make(map[int64]int64, len(rows))
	for _, r := range rows {
		m[r.LineID] = r.RuleID
	}
	return m, nil
}

func lineFromRow(r *dbq.ListCandidateLinesForRulesRow) Line {
	return Line{
		ID: r.ID, AccountID: r.AccountID, Message: r.Message, AmountMinor: r.AmountMinor,
		Source: r.Source, SubcategoryID: r.SubcategoryID, RuleID: r.RuleID,
	}
}

// outcome is the evaluated state of one line.
type outcome struct {
	source        string
	subcategoryID sql.NullInt64
	ruleID        sql.NullInt64
}

// matchAll returns the enabled rules matching the line.
func matchAll(enabled []compiledRule, line *Line) []compiledRule {
	var matches []compiledRule
	for i := range enabled {
		if Matches(enabled[i].rule, enabled[i].re, line) {
			matches = append(matches, enabled[i])
		}
	}
	return matches
}

// evaluate applies the PRD 7.5 precedence to one candidate line (source not
// manual/transfer): all enabled matching rules; 0 → none, 1 → rule, >1 → the
// remembered resolution when its rule still matches, else conflict.
func evaluate(line *Line, enabled []compiledRule, resolutions map[int64]int64) outcome {
	return outcomeFromMatches(line, matchAll(enabled, line), resolutions)
}

// outcomeFromMatches applies the precedence to an already-computed match set.
func outcomeFromMatches(line *Line, matches []compiledRule, resolutions map[int64]int64) outcome {
	switch len(matches) {
	case 0:
		return outcome{source: "none"}
	case 1:
		return outcome{source: "rule", subcategoryID: nullInt(matches[0].rule.SubcategoryID), ruleID: nullInt(matches[0].rule.ID)}
	}
	// conflict unless a still-valid resolution picks one of the matches
	if resRuleID, ok := resolutions[line.ID]; ok {
		for i := range matches {
			if matches[i].rule.ID == resRuleID {
				return outcome{source: "rule", subcategoryID: nullInt(matches[i].rule.SubcategoryID), ruleID: nullInt(matches[i].rule.ID)}
			}
		}
	}
	return outcome{source: "conflict"}
}

func nullInt(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: true}
}

// staleResolution reports whether the line has a remembered resolution whose
// rule is no longer among the matches (RULE-9: dropped automatically).
func staleResolution(line *Line, matches []compiledRule, resolutions map[int64]int64) bool {
	resRuleID, ok := resolutions[line.ID]
	if !ok {
		return false
	}
	for i := range matches {
		if matches[i].rule.ID == resRuleID {
			return false
		}
	}
	return true
}

// EvaluateLines re-evaluates the given lines through the rules engine and
// returns one actions.Change per line whose source/subcategory_id/rule_id
// changed (before/after whole-row snapshots, so callers can Record them).
// Stale conflict_resolution rows for evaluated lines are deleted in the same
// transaction (derived state, not recorded). Manual/transfer lines are skipped.
func EvaluateLines(ctx context.Context, q *dbq.Queries, lineIDs []int64) ([]actions.Change, error) {
	enabled, err := loadCompiled(ctx, q)
	if err != nil {
		return nil, err
	}
	enabled = filterEnabled(enabled)
	resolutions, err := loadResolutions(ctx, q)
	if err != nil {
		return nil, err
	}
	var changes []actions.Change
	for _, id := range lineIDs {
		row, err := q.GetLineForAction(ctx, id)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return nil, fmt.Errorf("get line %d: %w", id, err)
		}
		line := Line{
			ID: row.ID, AccountID: row.AccountID, Message: row.Message, AmountMinor: row.AmountMinor,
			Source: row.Source, SubcategoryID: row.SubcategoryID, RuleID: row.RuleID,
		}
		if line.Source == "manual" || line.Source == "transfer" {
			continue
		}
		matches := matchAll(enabled, &line)
		stale := staleResolution(&line, matches, resolutions)
		oc := outcomeFromMatches(&line, matches, resolutions)
		if stale {
			if err := q.DeleteConflictResolution(ctx, id); err != nil {
				return nil, fmt.Errorf("drop stale conflict resolution for line %d: %w", id, err)
			}
			delete(resolutions, id)
		}
		if oc.source == line.Source && oc.subcategoryID == line.SubcategoryID && oc.ruleID == line.RuleID {
			continue
		}
		if err := q.UpdateLineSource(ctx, dbq.UpdateLineSourceParams{
			SubcategoryID: oc.subcategoryID, Source: oc.source, RuleID: oc.ruleID, ID: id,
		}); err != nil {
			return nil, fmt.Errorf("update line %d: %w", id, err)
		}
		after, err := q.GetLineForAction(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get line %d after update: %w", id, err)
		}
		changes = append(changes, actions.Change{
			Entity: actions.EntityStatementLine, EntityID: id, Before: *row, After: *after,
		})
	}
	return changes, nil
}

// EvaluateAll re-evaluates every candidate line (source not manual/transfer).
// Used by rule mutations and on import (IMP-11, D11).
func EvaluateAll(ctx context.Context, q *dbq.Queries) ([]actions.Change, error) {
	rows, err := q.ListCandidateLinesForRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list candidate lines: %w", err)
	}
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return EvaluateLines(ctx, q, ids)
}

func filterEnabled(in []compiledRule) []compiledRule {
	out := in[:0]
	for _, c := range in {
		if c.rule.Enabled {
			out = append(out, c)
		}
	}
	return out
}

// --- live preview (RULE-3, plan D6) ---

// Group is one preview group: total count plus up to sampleSize sample line IDs.
type Group struct {
	Count         int     `json:"count"`
	SampleLineIDs []int64 `json:"sampleLineIds"`
}

func newGroup() Group { return Group{SampleLineIDs: []int64{}} }

func (g *Group) add(id int64) {
	g.Count++
	if len(g.SampleLineIDs) < sampleSize {
		g.SampleLineIDs = append(g.SampleLineIDs, id)
	}
}

// PreviewResult is the grouped effect of a hypothetical rule (RULE-3).
type PreviewResult struct {
	WillCategorize      Group `json:"willCategorize"`      // currently uncategorized, will become this rule
	AlreadyThisRule     Group `json:"alreadyThisRule"`     // already categorized by this rule
	WillConflict        Group `json:"willConflict"`        // this rule + >=1 other enabled rule, no valid resolution
	UnaffectedManual    Group `json:"unaffectedManual"`    // matches, but manual lines are never touched
	UnaffectedTransfer  Group `json:"unaffectedTransfer"`  // matches, but transfer lines are never touched
	StaysWithResolution Group `json:"staysWithResolution"` // matches, but a valid resolution keeps another rule
	TotalMatches        int   `json:"totalMatches"`
}

// Preview runs a rule definition against all lines without writing anything.
// excludeRuleID removes the stored rule with that id from the "other rules"
// (used when editing, so the submitted definition replaces it). A disabled
// definition matches nothing. Invalid patterns return the compiler error (RULE-2).
func Preview(ctx context.Context, q *dbq.Queries, def Rule, excludeRuleID int64) (*PreviewResult, error) {
	re, err := Compile(def.Pattern)
	if err != nil {
		return nil, err
	}
	res := &PreviewResult{
		WillCategorize: newGroup(), AlreadyThisRule: newGroup(), WillConflict: newGroup(),
		UnaffectedManual: newGroup(), UnaffectedTransfer: newGroup(), StaysWithResolution: newGroup(),
	}
	if !def.Enabled {
		return res, nil
	}
	all, err := loadCompiled(ctx, q)
	if err != nil {
		return nil, err
	}
	var others []compiledRule
	for _, c := range filterEnabled(all) {
		if c.rule.ID != excludeRuleID {
			others = append(others, c)
		}
	}
	resolutions, err := loadResolutions(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, err := q.ListLinesForRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list lines: %w", err)
	}
	defRule := def
	for _, r := range rows {
		line := Line{
			ID: r.ID, AccountID: r.AccountID, Message: r.Message, AmountMinor: r.AmountMinor,
			Source: r.Source, SubcategoryID: r.SubcategoryID, RuleID: r.RuleID,
		}
		if !Matches(&defRule, re, &line) {
			continue
		}
		res.TotalMatches++
		switch line.Source {
		case "manual":
			res.UnaffectedManual.add(line.ID)
			continue
		case "transfer":
			res.UnaffectedTransfer.add(line.ID)
			continue
		}
		// candidate line: the def is always among the matches
		matching := append([]compiledRule{{rule: &defRule, re: re}}, matchAll(others, &line)...)
		oc := outcomeFromMatches(&line, matching, resolutions)
		switch {
		case oc.source == "rule" && oc.ruleID.Valid && oc.ruleID.Int64 == defRule.ID:
			if line.Source == "rule" && line.RuleID.Valid && line.RuleID.Int64 == defRule.ID {
				res.AlreadyThisRule.add(line.ID)
			} else {
				res.WillCategorize.add(line.ID)
			}
		case oc.source == "conflict":
			res.WillConflict.add(line.ID)
		default:
			res.StaysWithResolution.add(line.ID)
		}
	}
	return res, nil
}

// --- edit diff (RULE-4) ---

// RemovedLine describes what a removed line will become after re-evaluation.
type RemovedLine struct {
	LineID        int64  `json:"lineId"`
	Future        string `json:"future"` // "none" | "conflict" | "rule"
	RuleID        *int64 `json:"ruleId,omitempty"`
	SubcategoryID *int64 `json:"subcategoryId,omitempty"`
}

// DiffResult compares the stored rule with a submitted new definition (RULE-4).
// added/removed/unchanged are line IDs among candidate lines (manual/transfer
// are never affected by rules).
type DiffResult struct {
	Added              []int64       `json:"added"`
	Removed            []int64       `json:"removed"`
	Unchanged          []int64       `json:"unchanged"`
	RemovedFuture      []RemovedLine `json:"removedFuture"` // capped at 100 entries
	RemovedFutureTotal int           `json:"removedFutureTotal"`
}

const removedFutureCap = 100

// Diff compares the stored rule ruleID against newDef without writing anything.
// Matching for added/removed/unchanged ignores the enabled flag (the diff is
// about the pattern/filter change); the future state of removed lines is
// computed against the enabled rules with newDef substituted for the old one.
func Diff(ctx context.Context, q *dbq.Queries, ruleID int64, newDef Rule) (*DiffResult, error) {
	old, err := LoadRule(ctx, q, ruleID)
	if err != nil {
		return nil, err
	}
	oldRe, err := Compile(old.Pattern)
	if err != nil {
		return nil, fmt.Errorf("stored rule has invalid pattern: %w", err)
	}
	newRe, err := Compile(newDef.Pattern)
	if err != nil {
		return nil, err
	}
	all, err := loadCompiled(ctx, q)
	if err != nil {
		return nil, err
	}
	// enabled rule set for the future state: others + newDef when enabled
	var future []compiledRule
	for _, c := range filterEnabled(all) {
		if c.rule.ID != ruleID {
			future = append(future, c)
		}
	}
	if newDef.Enabled {
		nd := newDef
		nd.ID = ruleID // the new definition takes the old rule's place
		future = append(future, compiledRule{rule: &nd, re: newRe})
	}
	resolutions, err := loadResolutions(ctx, q)
	if err != nil {
		return nil, err
	}
	rows, err := q.ListLinesForRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list lines: %w", err)
	}
	res := &DiffResult{Added: []int64{}, Removed: []int64{}, Unchanged: []int64{}, RemovedFuture: []RemovedLine{}}
	for _, r := range rows {
		line := Line{
			ID: r.ID, AccountID: r.AccountID, Message: r.Message, AmountMinor: r.AmountMinor,
			Source: r.Source, SubcategoryID: r.SubcategoryID, RuleID: r.RuleID,
		}
		if line.Source == "manual" || line.Source == "transfer" {
			continue
		}
		oldMatch := Matches(old, oldRe, &line)
		newMatch := Matches(&newDef, newRe, &line)
		switch {
		case newMatch && oldMatch:
			res.Unchanged = append(res.Unchanged, line.ID)
		case newMatch && !oldMatch:
			res.Added = append(res.Added, line.ID)
		case oldMatch && !newMatch:
			res.Removed = append(res.Removed, line.ID)
			res.RemovedFutureTotal++
			if len(res.RemovedFuture) < removedFutureCap {
				oc := evaluate(&line, future, resolutions)
				rl := RemovedLine{LineID: line.ID, Future: oc.source}
				if oc.source == "rule" {
					rl.RuleID = &oc.ruleID.Int64
					sub := oc.subcategoryID.Int64
					rl.SubcategoryID = &sub
				}
				res.RemovedFuture = append(res.RemovedFuture, rl)
			}
		default:
			// matches neither: not part of the diff
		}
	}
	return res, nil
}

// MatchingEnabledRules returns the enabled rules that match the line (used by
// the conflicts API, RULE-7).
func MatchingEnabledRules(ctx context.Context, q *dbq.Queries, l *Line) ([]Rule, error) {
	all, err := loadCompiled(ctx, q)
	if err != nil {
		return nil, err
	}
	enabled := filterEnabled(all)
	var out []Rule
	for i := range enabled {
		if Matches(enabled[i].rule, enabled[i].re, l) {
			out = append(out, *enabled[i].rule)
		}
	}
	return out, nil
}

// CountMatchCounts returns, per rule id, how many lines the rule currently
// categorizes (source='rule') — part 1 of the live matchCount (D10). Part 2
// (conflicts the rule participates in) is computed by ConflictCounts.
func CountCategorizedByRule(ctx context.Context, q *dbq.Queries, ruleID int64) (int64, error) {
	return q.CountLinesByRule(ctx, sql.NullInt64{Int64: ruleID, Valid: true})
}

// ConflictCounts returns, per rule id, in how many conflicted lines the rule
// participates (part 2 of matchCount, D10).
func ConflictCounts(ctx context.Context, q *dbq.Queries) (map[int64]int, error) {
	all, err := loadCompiled(ctx, q)
	if err != nil {
		return nil, err
	}
	enabled := filterEnabled(all)
	rows, err := q.ListCandidateLinesForRules(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[int64]int{}
	for _, r := range rows {
		if r.Source != "conflict" {
			continue
		}
		line := lineFromRow(r)
		for i := range enabled {
			if Matches(enabled[i].rule, enabled[i].re, &line) {
				counts[enabled[i].rule.ID]++
			}
		}
	}
	return counts, nil
}
