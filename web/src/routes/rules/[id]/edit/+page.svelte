<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import {
		type Account,
		type Rule,
		type RuleInput,
		type Subcategory,
		getRule,
		listAccounts,
		listSubcategories
	} from '$lib/api';
	import { i18n } from '$lib/i18n';
	import RuleForm from '$lib/RuleForm.svelte';
	import { onMount } from 'svelte';

	// Edit rule (RULE-4/5): same form as /rules/new, plus the edit diff toggle.

	let accounts = $state<Account[]>([]);
	let subcategories = $state<Subcategory[]>([]);
	let initial = $state<RuleInput | null>(null);
	let error = $state<string | null>(null);

	onMount(async () => {
		try {
			[accounts, subcategories] = await Promise.all([listAccounts(), listSubcategories()]);
			const id = parseInt($page.params.id ?? '', 10);
			const rule = await getRule(id);
			initial = {
				name: rule.name,
				pattern: rule.pattern,
				direction: rule.direction,
				subcategoryId: rule.subcategoryId,
				accountIds: rule.accountIds,
				enabled: rule.enabled
			};
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
	});

	function saved(_rule: Rule) {
		void goto('/rules');
	}
</script>

<svelte:head>
	<title>{$i18n.t('rules.editTitle')}</title>
</svelte:head>

<div class="max-w-3xl mx-auto space-y-4">
	<h1 class="text-2xl font-bold text-gray-900">{$i18n.t('rules.editTitle')}</h1>
	{#if error}
		<div class="bg-red-50 border-l-4 border-red-500 text-red-700 px-4 py-3 rounded-r-lg shadow-sm">
			<p class="text-sm">{error}</p>
		</div>
	{/if}
	{#if initial}
		<RuleForm
			{initial}
			ruleId={parseInt($page.params.id ?? '0', 10)}
			{subcategories}
			{accounts}
			onSaved={saved}
			onCancel={() => void goto('/rules')}
		/>
	{/if}
</div>
