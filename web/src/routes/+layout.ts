import { locales, setLocale } from '../paraglide/runtime.js';

export const ssr = false;
export const prerender = true;

export function load({ url }) {
	// The app has no locale-prefixed routes; only adopt a leading path
	// segment as locale if it is a valid one. Never reload the page from
	// here — setLocale defaults to reload: true, which caused a reload
	// loop on every non-home route.
	const segment = new URL(url).pathname.split('/')[1] || '';
	if ((locales as readonly string[]).includes(segment)) {
		setLocale(segment as (typeof locales)[number], { reload: false });
	}
}
