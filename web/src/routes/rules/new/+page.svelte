<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import {
		type Account,
		type Rule,
		type RuleInput,
		type Subcategory,
		getLine,
		listAccounts,
		listSubcategories
	} from '$lib/api';
	import { i18n } from '$lib/i18n';
	import RuleForm from '$lib/RuleForm.svelte';
	import { prefillRuleFromLine } from '$lib/rules';
	import { onMount } from 'svelte';

	// New rule (RULE-1). Deep link /rules/new?fromLine=<id> prefills the form
	// from the line: escaped message pattern, account filter, direction (D9).

	let accounts = $state<Account[]>([]);
	let subcategories = $state<Subcategory[]>([]);
	let initial = $state<RuleInput | null>(null);
	let error = $state<string | null>(null);

	onMount(async () => {
		try {
			[accounts, subcategories] = await Promise.all([listAccounts(), listSubcategories()]);
			const fromLine = $page.url.searchParams.get('fromLine');
			const fallbackSub = subcategories[0]?.id ?? 0;
			if (fromLine) {
				const line = await getLine(parseInt(fromLine, 10));
				initial = prefillRuleFromLine(line, fallbackSub);
			} else {
				initial = {
					name: '',
					pattern: '',
					direction: 'both',
					subcategoryId: fallbackSub,
					accountIds: [],
					enabled: true
				};
			}
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
	});

	function saved(_rule: Rule) {
		void goto('/rules');
	}
</script>

<svelte:head>
	<title>{$i18n.t('rules.newTitle')}</title>
</svelte:head>

<div class="max-w-3xl mx-auto space-y-4">
	<h1 class="text-2xl font-bold text-gray-900">{$i18n.t('rules.newTitle')}</h1>
	{#if error}
		<div class="bg-red-50 border-l-4 border-red-500 text-red-700 px-4 py-3 rounded-r-lg shadow-sm">
			<p class="text-sm">{error}</p>
		</div>
	{/if}
	{#if initial}
		<RuleForm
			{initial}
			ruleId={null}
			{subcategories}
			{accounts}
			onSaved={saved}
			onCancel={() => void goto('/rules')}
		/>
	{/if}
</div>
