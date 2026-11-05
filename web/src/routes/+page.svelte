<script lang="ts">
	import { enhance } from '$app/forms';
	import { i18n } from '$lib/i18n';
	import { onMount } from 'svelte';

	let dbStatus: 'missing' | 'ready' | 'error' = 'missing';
	let schemaVersion: number | null = null;
	let error: string | null = null;

	onMount(() => {
		checkStatus();
	});

	async function checkStatus() {
		try {
			const response = await fetch('/api/status');
			const data = await response.json();
			dbStatus = data.database;
			schemaVersion = data.schemaVersion || null;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Unknown error';
			dbStatus = 'error';
		}
	}

	async function createDatabase() {
		try {
			const response = await fetch('/api/database', {
				method: 'POST'
			});
			if (response.ok) {
				// Reload to get new status
				location.reload();
			} else {
				const data = await response.json();
				throw new Error(data.error?.message || 'Failed to create database');
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Unknown error';
		}
	}
</script>

<svelte:head>
	<title>{$i18n.t('app.name')}</title>
</svelte:head>

<div class="flex flex-col items-center justify-center min-h-[calc(100vh-160px)] p-4">
	<div class="w-full max-w-md space-y-6">
		<div class="text-center">
			<h1 class="text-3xl font-bold text-gray-900">Welcome</h1>
			<p class="mt-2 text-gray-600">
				{$i18n.t('home.subtitle')}
			</p>
		</div>

		{#if error}
			<div class="bg-red-50 border-l-4 border-red-500 text-red-700 px-4 py-3 rounded-r-lg shadow-sm">
				<p class="font-medium">{$i18n.t('home.error')}</p>
				<p class="text-sm mt-1">{error}</p>
			</div>
		{/if}

		<div class="bg-white shadow-xl rounded-xl p-6 space-y-4 border border-gray-100">
			<div>
				<h2 class="text-lg font-semibold text-gray-900">{$i18n.t('home.databaseStatus')}</h2>
				<div class="mt-3 p-4 bg-gray-50 rounded-lg border border-gray-200">
					<p class="text-sm text-gray-600">
						<span class="font-medium text-gray-900">{$i18n.t('home.status')}: </span>
						<span class:status={dbStatus}>
							{#if dbStatus === 'ready'}
								<span class="text-green-600 font-medium">{$i18n.t('home.ready')}</span>
							{:else if dbStatus === 'missing'}
								<span class="text-yellow-600 font-medium">{$i18n.t('home.missing')}</span>
							{:else}
								<span class="text-red-600 font-medium">{$i18n.t('home.error')}</span>
							{/if}
							{#if schemaVersion}
								<span class="text-gray-500 ml-2">({$i18n.t('home.schemaVersion', { version: schemaVersion })})</span>
							{/if}
						</span>
					</p>
				</div>

			{#if dbStatus === 'missing'}
				<button 
					onclick={createDatabase}
					class="w-full flex items-center justify-center px-4 py-2 bg-gradient-to-r from-blue-600 to-blue-700 text-white font-medium rounded-lg hover:from-blue-700 hover:to-blue-800 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 transition-all shadow-md hover:shadow-lg"
				>
					+ {$i18n.t('home.createDatabase')}
				</button>
			{/if}

			{#if dbStatus === 'ready'}
				<div class="space-y-2">
					<a 
						href="/statements"
						class="block w-full px-4 py-3 bg-white border border-gray-200 text-gray-700 font-medium rounded-lg hover:bg-blue-50 hover:border-blue-300 transition-all shadow-sm hover:shadow-md"
					>
						<div class="flex items-center space-x-3">
							<svg class="w-5 h-5 text-blue-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
							</svg>
							<span>{$i18n.t('nav.statements')}</span>
						</div>
					</a>
					<a 
						href="/categorize"
						class="block w-full px-4 py-3 bg-white border border-gray-200 text-gray-700 font-medium rounded-lg hover:bg-purple-50 hover:border-purple-300 transition-all shadow-sm hover:shadow-md"
					>
						<div class="flex items-center space-x-3">
							<svg class="w-5 h-5 text-purple-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 7h.01M7 3h5c.512 0 1.024.195 1.414.586l7 7a2 2 0 010 2.828l-7 7a2 2 0 01-2.828 0l-7-7A2 2 0 013 12V7a4 4 0 014-4z" />
							</svg>
							<span>{$i18n.t('nav.categorize')}</span>
						</div>
					</a>
					<a 
						href="/rules"
						class="block w-full px-4 py-3 bg-white border border-gray-200 text-gray-700 font-medium rounded-lg hover:bg-green-50 hover:border-green-300 transition-all shadow-sm hover:shadow-md"
					>
						<div class="flex items-center space-x-3">
							<svg class="w-5 h-5 text-green-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
							</svg>
							<span>{$i18n.t('nav.rules')}</span>
						</div>
					</a>
					<a 
						href="/visualize"
						class="block w-full px-4 py-3 bg-white border border-gray-200 text-gray-700 font-medium rounded-lg hover:bg-orange-50 hover:border-orange-300 transition-all shadow-sm hover:shadow-md"
					>
						<div class="flex items-center space-x-3">
							<svg class="w-5 h-5 text-orange-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
							</svg>
							<span>{$i18n.t('nav.visualize')}</span>
						</div>
					</a>
					<a 
						href="/import"
						class="block w-full px-4 py-3 bg-white border border-gray-200 text-gray-700 font-medium rounded-lg hover:bg-cyan-50 hover:border-cyan-300 transition-all shadow-sm hover:shadow-md"
					>
						<div class="flex items-center space-x-3">
							<svg class="w-5 h-5 text-cyan-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M12 15l-3-3m0 0l3-3m-3 3h12" />
							</svg>
							<span>{$i18n.t('nav.import')}</span>
						</div>
					</a>
					<a 
						href="/settings"
						class="block w-full px-4 py-3 bg-white border border-gray-200 text-gray-700 font-medium rounded-lg hover:bg-gray-50 hover:border-gray-300 transition-all shadow-sm hover:shadow-md"
					>
						<div class="flex items-center space-x-3">
							<svg class="w-5 h-5 text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
							</svg>
							<span>{$i18n.t('nav.settings')}</span>
						</div>
					</a>
				</div>
			{/if}
		</div>
	</div>
	</div>
</div>

<style>
	.status :global(span) {
		font-variant: tabular-nums;
	}
</style>
