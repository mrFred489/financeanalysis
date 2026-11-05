<script lang="ts">
	import { page } from '$app/stores';
	import { i18n } from '$lib/i18n';
</script>

<div class="max-w-2xl mx-auto bg-white shadow rounded-xl p-6 border border-gray-100">
	<h1 class="text-2xl font-bold text-gray-900">{$i18n.t('nav.categorize')}</h1>
	<p class="mt-2 text-gray-600">{$i18n.t('placeholder.m2')}</p>
	{#if $page.url.searchParams.toString()}
		<p class="mt-2 text-sm text-gray-500 font-mono">?{$page.url.searchParams.toString()}</p>
	{/if}
</div>
