// Pure helpers for the M4 transfer UI (statements column, categorize actions,
// /transfers page). Kept separate from the pages so vitest can cover them.
import type { StatementLine, TransferLink } from './api';

type LineLike = Pick<StatementLine, 'amountMinor' | 'transferLinkId' | 'transferCounterpartId'>;

/** Direction arrow of a linked line: the out side (amount < 0) points to the in side. */
export function transferArrow(line: Pick<StatementLine, 'amountMinor'>): '→' | '←' {
	return line.amountMinor < 0 ? '→' : '←';
}

/** "→ #123" / "← #456" label for a linked line, or null when not linked (STMT-9/D11). */
export function transferLabel(line: LineLike): string | null {
	if (line.transferLinkId == null || line.transferCounterpartId == null) return null;
	return `${transferArrow(line)} #${line.transferCounterpartId}`;
}

/** href that jumps to the counterpart line on the categorize page (CATZ-7). */
export function counterpartHref(line: LineLike, baseParams = ''): string | null {
	if (line.transferCounterpartId == null) return null;
	const sp = new URLSearchParams(baseParams);
	sp.set('focus', String(line.transferCounterpartId));
	return `/categorize?${sp.toString()}`;
}

/** Newest-first ordering of transfer links (defensive; the API already sorts). */
export function sortLinksNewestFirst(links: TransferLink[]): TransferLink[] {
	return [...links].sort((a, b) => b.id - a.id);
}

/** Client-side pre-check for a manual link (TRF-7); mirrors the server validation. */
export function linkError(
	out: Pick<StatementLine, 'accountId' | 'amountMinor' | 'transferLinkId'>,
	inLine: Pick<StatementLine, 'accountId' | 'amountMinor' | 'transferLinkId'>
): 'same_account' | 'amount_mismatch' | 'already_linked' | null {
	if (out.accountId === inLine.accountId) return 'same_account';
	if (out.transferLinkId != null || inLine.transferLinkId != null) return 'already_linked';
	if (!(out.amountMinor < 0 && inLine.amountMinor > 0 && out.amountMinor === -inLine.amountMinor)) {
		return 'amount_mismatch';
	}
	return null;
}

/** Stable DOM key for a review group (TRF-3). */
export function reviewGroupKey(group: { outLine: { id: number } }): string {
	return `review-${group.outLine.id}`;
}
