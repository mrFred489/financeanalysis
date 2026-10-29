import { setLocale } from '../paraglide/runtime.js';

export const ssr = false;
export const prerender = true;

export function load({ url }) {
	const locale = new URL(url).pathname.split('/')[1] || 'en';
	setLocale(locale);
}
