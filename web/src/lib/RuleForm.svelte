<script lang="ts">
	import {
		type Account,
		APIError,
		type DiffResult,
		type PreviewResult,
		type Rule,
		type RuleInput,
		type Subcategory,
		createRule,
		diffRule,
		previewRules,
		updateRule
	} from '$lib/api';
	import { i18n } from '$lib/i18n';
	import { untrack } from 'svelte';

	// Shared create/edit form for rules (RULE-1..5): fields + debounced
	// server-side live preview (RULE-3) and, when editing, the edit diff
	// (RULE-4) as a toggle.

	interface Props {
		initial: RuleInput;
		ruleId: number | null; // null = create
		subcategories: Subcategory[];
		accounts: Account[];
		onSaved: (rule: Rule) => void;
		onCancel: () => void;
	}

	let { initial, ruleId, subcategories, accounts, onSaved, onCancel }: Props = $props();

	// The form intentionally captures the initial values only (the parent loads
	// them once), hence untrack.
	let name = $state(untrack(() => initial.name));
	let pattern = $state(untrack(() => initial.pattern));
	let direction = $state<RuleInput['direction']>(untrack(() => initial.direction));
	let subcategoryId = $state(untrack(() => initial.subcategoryId));
	let accountIds = $state<number[]>(untrack(() => initial.accountIds));
	let enabled = $state(untrack(() => initial.enabled));

	let regexError = $state<string | null>(null);
	let preview = $state<PreviewResult | null>(null);
	let diffMode = $state(false);
	let diff = $state<DiffResult | null>(null);
	let saving = $state(false);
	let error = $state<string | null>(null);

	function currentInput(): RuleInput {
		return {
			name,
			pattern,
			direction,
			subcategoryId,
			accountIds,
			enabled,
			...(ruleId !== null ? { id: ruleId } : {})
		};
	}

	// Live preview (RULE-3), debounced 300 ms; in diff mode (edit only) the
	// panel shows the edit diff (RULE-4) instead.
	$effect(() => {
		// track all fields
		void name;
		void pattern;
		void direction;
		void subcategoryId;
		void accountIds;
		void enabled;
		void diffMode;
		if (pattern.trim() === '') {
			preview = null;
			diff = null;
			regexError = null;
			return;
		}
		const input = currentInput();
		const timer = setTimeout(() => {
			void (async () => {
				try {
					if (diffMode && ruleId !== null) {
						diff = await diffRule(ruleId, input);
						preview = null;
					} else {
						preview = await previewRules(input);
						diff = null;
					}
					regexError = null;
				} catch (err) {
					if (err instanceof APIError && err.code === 'invalid_regex') {
						regexError = err.message;
						preview = null;
						diff = null;
					}
					// other errors (e.g. network): keep the last preview
				}
			})();
		}, 300);
		return () => clearTimeout(timer);
	});

	function toggleAccount(id: number) {
		accountIds = accountIds.includes(id) ? accountIds.filter((a) => a !== id) : [...accountIds, id];
	}

	async function submit() {
		if (regexError || saving) return;
		saving = true;
		error = null;
		try {
			const input = currentInput();
			const rule = ruleId === null ? await createRule(input) : await updateRule(ruleId, input);
			onSaved(rule);
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			saving = false;
		}
	}

	type PreviewGroupKey =
		| 'willCategorize'
		| 'alreadyThisRule'
		| 'willConflict'
		| 'unaffectedManual'
		| 'unaffectedTransfer'
		| 'staysWithResolution';

	const previewGroups: [string, PreviewGroupKey][] = [
		['rules.preview.willCategorize', 'willCategorize'],
		['rules.preview.alreadyThisRule', 'alreadyThisRule'],
		['rules.preview.willConflict', 'willConflict'],
		['rules.preview.unaffectedManual', 'unaffectedManual'],
		['rules.preview.unaffectedTransfer', 'unaffectedTransfer'],
		['rules.preview.staysWithResolution', 'staysWithResolution']
	];
</script>

<form
	class="bg-white shadow rounded-xl p-6 border border-gray-100 space-y-4"
	onsubmit={(e) => {
		e.preventDefault();
		void submit();
	}}
>
	<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
		<label class="block">
			<span class="text-xs text-gray-500">{$i18n.t('rules.form.name')}</span>
			<input
				type="text"
				bind:value={name}
				required
				class="block w-full rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
			/>
		</label>
		<label class="block">
			<span class="text-xs text-gray-500">{$i18n.t('rules.form.pattern')}</span>
			<input
				type="text"
				bind:value={pattern}
				required
				spellcheck="false"
				class="block w-full rounded-lg border-gray-300 shadow-sm font-mono text-sm focus:border-blue-500 focus:ring-blue-500"
				placeholder={$i18n.t('rules.form.patternPlaceholder')}
			/>
		</label>
		<label class="block">
			<span class="text-xs text-gray-500">{$i18n.t('rules.form.direction')}</span>
			<select
				bind:value={direction}
				class="block w-full rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
			>
				<option value="both">{$i18n.t('rules.form.directionBoth')}</option>
				<option value="in">{$i18n.t('rules.form.directionIn')}</option>
				<option value="out">{$i18n.t('rules.form.directionOut')}</option>
			</select>
		</label>
		<label class="block">
			<span class="text-xs text-gray-500">{$i18n.t('rules.form.subcategory')}</span>
			<select
				bind:value={subcategoryId}
				class="block w-full rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
			>
				{#each subcategories as sub (sub.id)}
					<option value={sub.id}>{sub.name}</option>
				{/each}
			</select>
		</label>
	</div>

	<fieldset>
		<legend class="text-xs text-gray-500 mb-1">{$i18n.t('rules.form.accounts')}</legend>
		<span class="text-xs text-gray-400 mr-2">{$i18n.t('rules.form.accountsHint')}</span>
		{#each accounts as acc (acc.id)}
			<label class="mr-3 inline-flex items-center gap-1 text-sm">
				<input
					type="checkbox"
					checked={accountIds.includes(acc.id)}
					onchange={() => toggleAccount(acc.id)}
				/>
				{acc.name}
			</label>
		{/each}
	</fieldset>

	<label class="inline-flex items-center gap-2 text-sm">
		<input type="checkbox" bind:checked={enabled} />
		{$i18n.t('rules.form.enabled')}
	</label>

	{#if regexError}
		<p
			class="text-sm text-red-700 bg-red-50 border-l-4 border-red-500 px-3 py-2 rounded-r-lg"
			data-regex-error
		>
			{$i18n.t('rules.form.invalidRegex')}: {regexError}
		</p>
	{/if}
	{#if error}
		<p class="text-sm text-red-700 bg-red-50 border-l-4 border-red-500 px-3 py-2 rounded-r-lg">
			{error}
		</p>
	{/if}

	<!-- Live preview (RULE-3) / edit diff (RULE-4) -->
	{#if ruleId !== null}
		<div class="flex items-center gap-2 text-sm">
			<span class="text-xs text-gray-500">{$i18n.t('rules.preview.mode')}</span>
			<button
				type="button"
				class="px-2 py-1 rounded-lg border text-xs {diffMode
					? 'bg-white border-gray-300 text-gray-700'
					: 'bg-purple-600 border-purple-600 text-white'}"
				onclick={() => (diffMode = false)}
				data-preview-mode>{$i18n.t('rules.preview.previewMode')}</button
			>
			<button
				type="button"
				class="px-2 py-1 rounded-lg border text-xs {diffMode
					? 'bg-purple-600 border-purple-600 text-white'
					: 'bg-white border-gray-300 text-gray-700'}"
				onclick={() => (diffMode = true)}
				data-diff-mode>{$i18n.t('rules.preview.diffMode')}</button
			>
		</div>
	{/if}

	{#if preview}
		<section class="border border-gray-200 rounded-lg p-3 bg-gray-50 text-sm" data-preview-panel>
			<h3 class="font-medium text-gray-900 mb-2">
				{$i18n.t('rules.preview.title')} · {preview.totalMatches}
				{$i18n.t('rules.preview.totalMatches')}
			</h3>
			<dl class="grid grid-cols-2 sm:grid-cols-3 gap-2">
				{#each previewGroups as [labelKey, key] (key)}
					<div class="bg-white border border-gray-200 rounded-lg px-2 py-1.5">
						<dt class="text-xs text-gray-500">{$i18n.t(labelKey)}</dt>
						<dd class="font-medium" data-preview-group={key}>{preview[key].count}</dd>
					</div>
				{/each}
			</dl>
		</section>
	{/if}

	{#if diff}
		<section class="border border-gray-200 rounded-lg p-3 bg-gray-50 text-sm" data-diff-panel>
			<h3 class="font-medium text-gray-900 mb-2">{$i18n.t('rules.diff.title')}</h3>
			<dl class="flex gap-4 text-sm mb-2">
				<div>
					<dt class="text-xs text-gray-500">{$i18n.t('rules.diff.added')}</dt>
					<dd class="font-medium text-green-700" data-diff-added>{diff.added.length}</dd>
				</div>
				<div>
					<dt class="text-xs text-gray-500">{$i18n.t('rules.diff.removed')}</dt>
					<dd class="font-medium text-red-700" data-diff-removed>{diff.removed.length}</dd>
				</div>
				<div>
					<dt class="text-xs text-gray-500">{$i18n.t('rules.diff.unchanged')}</dt>
					<dd class="font-medium" data-diff-unchanged>{diff.unchanged.length}</dd>
				</div>
			</dl>
			{#if diff.removedFuture.length > 0}
				<p class="text-xs text-gray-500 mb-1">
					{$i18n.t('rules.diff.removedFuture', {
						shown: diff.removedFuture.length,
						total: diff.removedFutureTotal
					})}
				</p>
				<ul class="text-xs text-gray-600 max-h-32 overflow-y-auto">
					{#each diff.removedFuture as rf (rf.lineId)}
						<li data-diff-future-line={rf.lineId}>
							# {rf.lineId} → {$i18n.t(`rules.diff.future.${rf.future}`)}
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	{/if}

	<div class="flex justify-end gap-2">
		<button
			type="button"
			onclick={onCancel}
			class="px-4 py-2 rounded-lg border border-gray-200 hover:bg-gray-50"
			>{$i18n.t('rules.form.cancel')}</button
		>
		<button
			type="submit"
			disabled={saving || regexError !== null || pattern.trim() === '' || name.trim() === ''}
			class="px-4 py-2 rounded-lg bg-purple-600 text-white hover:bg-purple-700 disabled:opacity-50"
			data-save-rule
		>
			{saving ? $i18n.t('rules.form.saving') : $i18n.t('rules.form.save')}
		</button>
	</div>
</form>
