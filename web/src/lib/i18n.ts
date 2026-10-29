import { paraglide } from '@paraglide/sveltekit';
import type { Locale } from '@paraglide/sveltekit';

export const i18n = paraglide;

export function getLocales(): Locale[] {
	return ['en'];
}
