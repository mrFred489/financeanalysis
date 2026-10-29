import { paraglide } from '@paraglide/sveltekit';

export const ssr = false;
export const prerender = true;

export function load({ url }) {
	const ll = paraglide.loadLocaleAsync(url);
	return { ...ll };
}
