import { describe, expect, it } from 'vitest';
import { formatDate, formatMinor } from './format';

describe('formatMinor', () => {
	it('formats øre as DKK with da-DK locale', () => {
		expect(formatMinor(-135797, 'da-DK')).toBe('-1.357,97 kr.');
	});
	it('formats positive amounts', () => {
		expect(formatMinor(2500000, 'en-US')).toBe('DKK 25,000.00');
	});
});

describe('formatDate', () => {
	it('formats ISO dates', () => {
		expect(formatDate('2024-12-31', 'en-US')).toBe('12/31/2024');
		expect(formatDate('2024-12-31', 'da-DK')).toBe('31.12.2024');
	});
	it('passes through invalid input', () => {
		expect(formatDate('bogus', 'en-US')).toBe('bogus');
	});
});
