import { describe, expect, it } from 'vitest';
import {
	accountLabel,
	buildRulesQuery,
	defaultRulesListFilter,
	directionFromAmount,
	escapeRegExp,
	prefillRuleFromLine,
	suggestRuleName,
	type RulesListFilter
} from './rules';
import type { Rule, StatementLine } from './api';

describe('escapeRegExp', () => {
	it('escapes all regex metacharacters', () => {
		expect(escapeRegExp('a.b*c+d?e^f$g{h}(i)[j]k\\l|m')).toBe(
			'a\\.b\\*c\\+d\\?e\\^f\\$g\\{h\\}\\(i\\)\\[j\\]k\\\\l\\|m'
		);
	});
	it('leaves plain text unchanged', () => {
		expect(escapeRegExp('Køb i Netto')).toBe('Køb i Netto');
	});
	it('produces a pattern that matches the literal message', () => {
		const msg = 'MobilePay (123) * test, ok';
		const re = new RegExp(escapeRegExp(msg));
		expect(re.test(msg)).toBe(true);
		expect(re.test('MobilePay 123  test')).toBe(false);
	});
});

describe('directionFromAmount', () => {
	it('maps the sign of the amount', () => {
		expect(directionFromAmount(100)).toBe('in');
		expect(directionFromAmount(-1)).toBe('out');
		expect(directionFromAmount(0)).toBe('both');
	});
});

describe('suggestRuleName', () => {
	it('uses the trimmed message when short', () => {
		expect(suggestRuleName('  Køb i Netto  ')).toBe('Køb i Netto');
	});
	it('collapses whitespace and caps long messages', () => {
		const long = 'A very long bank message that just keeps going and going and going';
		const name = suggestRuleName(long);
		expect(name.length).toBeLessThanOrEqual(41);
		expect(name.endsWith('…')).toBe(true);
	});
});

describe('prefillRuleFromLine', () => {
	it('prefills pattern, account, direction and name from the line (RULE-1)', () => {
		const line = {
			id: 7,
			accountId: 3,
			amountMinor: -13597,
			message: 'Køb i Netto, CITY N '
		} as StatementLine;
		const input = prefillRuleFromLine(line, 9);
		expect(input.pattern).toBe(escapeRegExp('Køb i Netto, CITY N'));
		expect(input.accountIds).toEqual([3]);
		expect(input.direction).toBe('out');
		expect(input.name).toBe('Køb i Netto, CITY N');
		expect(input.subcategoryId).toBe(9);
		expect(input.enabled).toBe(true);
	});
});

describe('buildRulesQuery', () => {
	it('encodes filters, sort and order (RULE-8)', () => {
		const f: RulesListFilter = {
			search: 'netto',
			enabled: '1',
			subcategoryId: 5,
			sort: 'matchCount',
			order: 'desc'
		};
		const q = buildRulesQuery(f);
		expect(q).toContain('search=netto');
		expect(q).toContain('enabled=1');
		expect(q).toContain('subcategoryId=5');
		expect(q).toContain('sort=matchCount');
		expect(q).toContain('order=desc');
	});
	it('omits empty filters', () => {
		expect(buildRulesQuery(defaultRulesListFilter)).toBe('sort=name&order=asc');
	});
});

describe('accountLabel', () => {
	it('shows all when no account filter', () => {
		const rule = { accountIds: [] } as Rule;
		expect(accountLabel(rule, (id) => `acc${id}`)).toBe('all');
	});
	it('lists account names otherwise', () => {
		const rule = { accountIds: [1, 2] } as Rule;
		expect(accountLabel(rule, (id) => `acc${id}`)).toBe('acc1, acc2');
	});
});
