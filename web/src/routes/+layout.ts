import { setLocale } from '../paraglide/runtime.js';
import { loadMessagesAsync } from '../paraglide/messages.js';

export const ssr = false;
export const prerender = true;

export async function load({ url }) {
	const locale = new URL(url).pathname.split('/')[1] || 'en';
	setLocale(locale);
	const messages = await loadMessagesAsync();
	return { messages };
}
