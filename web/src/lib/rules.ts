// Pure helpers for the M3 rules UI (RULE-1 prefill, RULE-8 list query).
// Kept framework-free so Vitest can test them directly.

import type { Rule, RuleDirection, RuleInput, StatementLine } from './api';

// escapeRegExp escapes all regex metacharacters, so a bank message can be used
// verbatim as a rule pattern (RULE-1 "create rule from this line" prefill).
export function escapeRegExp(s: string): string {
	return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

// directionFromAmount derives the rule direction filter from the sign of the
// line's amount (RULE-1 prefill): in = amount > 0, out = amount < 0.
export function directionFromAmount(amountMinor: number): RuleDirection {
	if (amountMinor > 0) return 'in';
	if (amountMinor < 0) return 'out';
	return 'both';
}

// suggestRuleName builds a suggested rule name from the message prefix
// (RULE-1): the first words of the trimmed message, capped at 40 chars.
export function suggestRuleName(message: string): string {
	const trimmed = message.trim().replace(/\s+/g, ' ');
	if (trimmed.length <= 40) return trimmed;
	return trimmed.slice(0, 40).trimEnd() + '…';
}

// prefillRuleFromLine builds the prefilled rule form values for "create rule
// from this line" (RULE-1, plan D9): escaped message, the line's account,
// direction from the amount sign, suggested name.
export function prefillRuleFromLine(line: StatementLine, subcategoryId: number): RuleInput {
	return {
		name: suggestRuleName(line.message),
		pattern: escapeRegExp(line.message.trim()),
		direction: directionFromAmount(line.amountMinor),
		subcategoryId,
		accountIds: [line.accountId],
		enabled: true
	};
}

// --- rules list query (RULE-8: server-side filter/sort via query params) ---

export type RuleSortKey = 'name' | 'pattern' | 'subcategory' | 'matchCount' | 'createdAt';

export interface RulesListFilter {
	search: string;
	enabled: '' | '1' | '0';
	subcategoryId: number | null;
	sort: RuleSortKey;
	order: 'asc' | 'desc';
}

export const defaultRulesListFilter: RulesListFilter = {
	search: '',
	enabled: '',
	subcategoryId: null,
	sort: 'name',
	order: 'asc'
};

// buildRulesQuery encodes the list filter as a query string for GET /api/rules.
export function buildRulesQuery(f: RulesListFilter): string {
	const sp = new URLSearchParams();
	if (f.search.trim() !== '') sp.set('search', f.search.trim());
	if (f.enabled !== '') sp.set('enabled', f.enabled);
	if (f.subcategoryId !== null) sp.set('subcategoryId', String(f.subcategoryId));
	sp.set('sort', f.sort);
	sp.set('order', f.order);
	return sp.toString();
}

// accountLabel renders a rule's account filter: the account names, or "all".
export function accountLabel(rule: Rule, accountName: (id: number) => string): string {
	if (rule.accountIds.length === 0) return 'all';
	return rule.accountIds.map((id) => accountName(id)).join(', ');
}
