import enMessages from '../../messages/en.json';
import { writable } from 'svelte/store';

// Helper to format messages with parameters
export function formatMessage(message: string, params?: Record<string, unknown>): string {
	if (!params) return message;
	return message.replace(/\{(\w+)\}/g, (match, key) => {
		const value = params[key];
		return value !== undefined ? String(value) : match;
	});
}

// Create a writable store for i18n
// Using a simple implementation that works with the existing message structure
const messages = { en: enMessages };

export const locales = ['en'];
export const setLocale = (locale: string) => {};

export function getLocales() {
	return locales;
}

// Simple i18n store that navigates through nested message paths
export const i18n = writable<{
	locales: string[];
	t(key: string, params?: Record<string, unknown>): string;
}>({
	locales,
	t(key: string, params?: Record<string, unknown>) {
		// Navigate through nested message paths
		const parts = key.split('.');
		let current: any = messages.en;
		for (const part of parts) {
			if (current === undefined || current === null) {
				return key; // Fallback: return the key itself if not found
			}
			current = current[part];
		}
		if (typeof current === 'string') {
			return formatMessage(current, params);
		}
		return key; // Fallback if the value is not a string
	}
});
