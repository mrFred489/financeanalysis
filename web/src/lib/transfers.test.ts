import { describe, expect, it } from 'vitest';
import {
	counterpartHref,
	linkError,
	reviewGroupKey,
	sortLinksNewestFirst,
	transferArrow,
	transferLabel
} from './transfers';
import type { StatementLine, TransferLink } from './api';

const line = (over: Partial<StatementLine>): StatementLine =>
	({
		id: 1,
		accountId: 1,
		accountName: 'Main',
		importId: null,
		bookingDate: '2026-01-15',
		transactionDate: null,
		valueDate: null,
		message: 'm',
		amountMinor: -100,
		balanceMinor: null,
		currency: 'DKK',
		bankRef: null,
		counterpartyAccount: null,
		fileRow: 1,
		seq: 1,
		occurrence: 1,
		subcategoryId: null,
		subcategoryName: null,
		subcategoryColor: null,
		categoryId: null,
		categoryName: null,
		source: 'none',
		ruleId: null,
		spreadMonths: 1,
		transferLinkId: null,
		transferCounterpartId: null,
		...over
	}) as StatementLine;

describe('transferLabel / transferArrow', () => {
	it('renders → for the out side and ← for the in side', () => {
		const out = line({ amountMinor: -4000, transferLinkId: 7, transferCounterpartId: 42 });
		const inn = line({ amountMinor: 4000, transferLinkId: 7, transferCounterpartId: 9 });
		expect(transferArrow(out)).toBe('→');
		expect(transferArrow(inn)).toBe('←');
		expect(transferLabel(out)).toBe('→ #42');
		expect(transferLabel(inn)).toBe('← #9');
	});

	it('is null when the line is not linked', () => {
		expect(transferLabel(line({}))).toBeNull();
		expect(transferLabel(line({ transferLinkId: 7 }))).toBeNull();
	});
});

describe('counterpartHref', () => {
	it('builds a categorize URL focusing the counterpart, keeping other params', () => {
		const l = line({ transferLinkId: 7, transferCounterpartId: 42 });
		expect(counterpartHref(l, 'source=none&page=2')).toBe(
			'/categorize?source=none&page=2&focus=42'
		);
	});

	it('is null when unlinked', () => {
		expect(counterpartHref(line({}))).toBeNull();
	});
});

describe('linkError (TRF-7 client pre-check)', () => {
	it('rejects the same account', () => {
		expect(
			linkError(line({ accountId: 1, amountMinor: -100 }), line({ accountId: 1, amountMinor: 100 }))
		).toBe('same_account');
	});

	it('rejects already-linked lines', () => {
		expect(
			linkError(
				line({ accountId: 1, amountMinor: -100, transferLinkId: 5 }),
				line({ accountId: 2, amountMinor: 100 })
			)
		).toBe('already_linked');
	});

	it('rejects amounts that are not exact opposites', () => {
		expect(
			linkError(line({ accountId: 1, amountMinor: -100 }), line({ accountId: 2, amountMinor: 200 }))
		).toBe('amount_mismatch');
		expect(
			linkError(line({ accountId: 1, amountMinor: 100 }), line({ accountId: 2, amountMinor: -100 }))
		).toBe('amount_mismatch');
	});

	it('accepts a valid pair (any date, different accounts, opposite amounts)', () => {
		expect(
			linkError(
				line({ accountId: 1, amountMinor: -100, bookingDate: '2026-01-15' }),
				line({ accountId: 2, amountMinor: 100, bookingDate: '2026-02-20' })
			)
		).toBeNull();
	});
});

describe('sortLinksNewestFirst', () => {
	it('orders by id descending without mutating the input', () => {
		const mk = (id: number): TransferLink => ({
			id,
			outLineId: id,
			inLineId: id + 100,
			method: 'auto',
			createdAt: '',
			out: {} as never,
			in: {} as never
		});
		const links = [mk(1), mk(3), mk(2)];
		const sorted = sortLinksNewestFirst(links);
		expect(sorted.map((l) => l.id)).toEqual([3, 2, 1]);
		expect(links.map((l) => l.id)).toEqual([1, 3, 2]);
	});
});

describe('reviewGroupKey', () => {
	it('is stable per out line', () => {
		expect(reviewGroupKey({ outLine: line({ id: 12 }) })).toBe('review-12');
	});
});
