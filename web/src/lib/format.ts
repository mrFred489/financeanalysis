// Number/date formatting via Intl with a configurable locale (NFR-7).
// Amounts are integer minor units (øre); dates are ISO YYYY-MM-DD.

export function formatMinor(minor: number, locale: string): string {
	return new Intl.NumberFormat(locale, {
		style: 'currency',
		currency: 'DKK'
	}).format(minor / 100);
}

export function formatDate(iso: string, locale: string): string {
	const [y, m, d] = iso.split('-').map(Number);
	if (!y || !m || !d) return iso;
	return new Intl.DateTimeFormat(locale, {
		year: 'numeric',
		month: '2-digit',
		day: '2-digit'
	}).format(new Date(Date.UTC(y, m - 1, d)));
}

export function formatDateTime(iso: string, locale: string): string {
	const date = new Date(iso);
	if (isNaN(date.getTime())) return iso;
	return new Intl.DateTimeFormat(locale, { dateStyle: 'short', timeStyle: 'short' }).format(date);
}
