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

<div class="min-h-screen bg-gray-50 flex flex-col items-center justify-center p-4">
	<div class="w-full max-w-md space-y-6">
		<div class="text-center">
			<h1 class="text-3xl font-bold text-gray-900">FinanceAnalysis</h1>
			<p class="mt-2 text-gray-600">
				{$i18n.t('home.subtitle')}
			</p>
		</div>

		{#if error}
			<div class="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded-md">
				{error}
			</div>
		{/if}

		<div class="bg-white shadow-md rounded-lg p-6 space-y-4">
			<div>
				<h2 class="text-lg font-semibold text-gray-900">{$i18n.t('home.databaseStatus')}</h2>
				<p class="text-sm text-gray-600 mt-1">
					Status: 
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
					class="w-full flex items-center justify-center px-4 py-2 bg-blue-600 text-white font-medium rounded-md hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 transition-colors"
				>
					+ {$i18n.t('home.createDatabase')}
				</button>
			{/if}

			{#if dbStatus === 'ready'}
				<div class="space-y-2">
					<a 
						href="/statements"
						class="block w-full px-4 py-2 bg-gray-100 text-gray-900 font-medium rounded-md hover:bg-gray-200 transition-colors"
					>
						{$i18n.t('nav.statements')}
					</a>
					<a 
						href="/categorize"
						class="block w-full px-4 py-2 bg-gray-100 text-gray-900 font-medium rounded-md hover:bg-gray-200 transition-colors"
					>
						{$i18n.t('nav.categorize')}
					</a>
					<a 
						href="/rules"
						class="block w-full px-4 py-2 bg-gray-100 text-gray-900 font-medium rounded-md hover:bg-gray-200 transition-colors"
					>
						{$i18n.t('nav.rules')}
					</a>
					<a 
						href="/visualize"
						class="block w-full px-4 py-2 bg-gray-100 text-gray-900 font-medium rounded-md hover:bg-gray-200 transition-colors"
					>
						{$i18n.t('nav.visualize')}
					</a>
					<a 
						href="/import"
						class="block w-full px-4 py-2 bg-gray-100 text-gray-900 font-medium rounded-md hover:bg-gray-200 transition-colors"
					>
						{$i18n.t('nav.import')}
					</a>
					<a 
						href="/settings"
						class="block w-full px-4 py-2 bg-gray-100 text-gray-900 font-medium rounded-md hover:bg-gray-200 transition-colors"
					>
						{$i18n.t('nav.settings')}
					</a>
				</div>
			{/if}
		</div>
	</div>
</div>

<style>
	.status :global(span) {
		font-variant: tabular-nums;
	}
</style>
