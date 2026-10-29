import { locales, setLocale } from '../paraglide/runtime.js';
import * as messages from '../paraglide/messages.js';
import { writable } from 'svelte/store';

export { locales, setLocale };

export function getLocales() {
	return locales;
}

// Create a writable store for i18n
export const i18n = writable<{
	t(key: string, params?: Record<string, unknown>): string;
}>({
	t(key: string, params?: Record<string, unknown>) {
		// Navigate through nested message paths
		const parts = key.split('.');
		let current: any = messages;
		for (const part of parts.slice(0, -1)) {
			current = current[part];
		}
		const messageFn = current[parts[parts.length - 1]];
		return messageFn(params);
	}
});
