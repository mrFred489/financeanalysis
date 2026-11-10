<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { getStatus } from '$lib/api';
	import { i18n } from '$lib/i18n';
	import { locale } from '$lib/locale';
	import { getSettings } from '$lib/api';
	import '../app.css';
	import { onMount } from 'svelte';
	import { get } from 'svelte/store';

	let { children } = $props();

	onMount(async () => {
		try {
			const status = await getStatus();
			const path = get(page).url.pathname;
			if (status.database === 'missing' && !path.startsWith('/welcome')) {
				await goto('/welcome');
			} else if (status.database === 'ready' && path.startsWith('/welcome')) {
				await goto('/');
			}
			if (status.database === 'ready') {
				try {
					const settings = await getSettings();
					if (settings.locale) locale.set(settings.locale);
				} catch {
					// settings unavailable; keep default locale
				}
			}
		} catch {
			// API unreachable; stay on the current page
		}
	});

	const navItems = [
		{ href: '/statements', key: 'nav.statements' },
		{ href: '/categorize', key: 'nav.categorize' },
		{ href: '/rules', key: 'nav.rules' },
		{ href: '/visualize', key: 'nav.visualize', disabled: true },
		{ href: '/import', key: 'nav.import' },
		{ href: '/settings', key: 'nav.settings' }
	];
</script>

<div class="min-h-screen bg-gradient-to-br from-gray-50 to-gray-100 flex flex-col">
	<!-- Header -->
	<header class="bg-white shadow-sm border-b border-gray-200 sticky top-0 z-50">
		<div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
			<div class="flex justify-between items-center h-16">
				<a href="/" class="flex items-center space-x-3">
					<div
						class="w-10 h-10 bg-gradient-to-br from-blue-600 to-blue-800 rounded-lg flex items-center justify-center shadow-md"
					>
						<span class="text-white font-bold text-xl">FA</span>
					</div>
					<div>
						<h1 class="text-xl font-bold text-gray-900">FinanceAnalysis</h1>
						<p class="text-sm text-gray-500">{$i18n.t('app.name')}</p>
					</div>
				</a>

				<nav class="flex items-center space-x-1">
					{#each navItems as item}
						{#if item.disabled}
							<span
								class="px-3 py-2 text-sm font-medium rounded-md text-gray-300 cursor-not-allowed"
								title={$i18n.t('nav.comingSoon')}>{item.key ? $i18n.t(item.key) : ''}</span
							>
						{:else}
							<a
								href={item.href}
								class="px-3 py-2 text-sm font-medium rounded-md transition-colors {$page.url.pathname.startsWith(
									item.href
								)
									? 'bg-blue-100 text-blue-800'
									: 'text-gray-600 hover:text-gray-900 hover:bg-gray-100'}">{$i18n.t(item.key)}</a
							>
						{/if}
					{/each}
				</nav>
			</div>
		</div>
	</header>

	<!-- Main content -->
	<main class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8 flex-grow w-full">
		{@render children()}
	</main>

	<!-- Footer -->
	<footer class="bg-gray-800 text-gray-300 mt-auto">
		<div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
			<div class="flex flex-col md:flex-row justify-between items-center">
				<div class="mb-4 md:mb-0">
					<p class="text-sm">
						&copy; {new Date().getFullYear()} FinanceAnalysis. All rights reserved.
					</p>
				</div>
			</div>
		</div>
	</footer>
</div>
