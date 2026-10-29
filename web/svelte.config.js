import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { paraglide } from '@paraglide/sveltekit';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),

	kit: {
		adapter: adapter({
			fallback: 'index.html'
		}),
		alias: {
			$lib: './src/lib'
		},
		csr: true
	}
};

export default paraglide(config);
