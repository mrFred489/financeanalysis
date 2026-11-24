<script lang="ts">
	import {
		type ReviewGroup,
		type TransferLink,
		APIError,
		detectTransfers,
		dismissTransferGroup,
		linkTransfer,
		listTransferReview,
		listTransfers,
		unlinkTransfer
	} from '$lib/api';
	import { formatDate, formatMinor } from '$lib/format';
	import { i18n } from '$lib/i18n';
	import { locale } from '$lib/locale';
	import { reviewGroupKey, sortLinksNewestFirst } from '$lib/transfers';
	import { onMount } from 'svelte';

	let links = $state<TransferLink[]>([]);
	let review = $state<ReviewGroup[]>([]);
	let error = $state<string | null>(null);
	let toast = $state<string | null>(null);
	let toastTimer: ReturnType<typeof setTimeout> | undefined;
	let busy = $state(false);
	let confirmUnlink = $state<number | null>(null);

	onMount(load);

	async function load() {
		error = null;
		try {
			const [l, r] = await Promise.all([listTransfers(), listTransferReview()]);
			links = sortLinksNewestFirst(l);
			review = r;
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
	}

	function showToast(msg: string) {
		toast = msg;
		clearTimeout(toastTimer);
		toastTimer = setTimeout(() => (toast = null), 3500);
	}

	async function runDetect() {
		busy = true;
		try {
			const res = await detectTransfers();
			showToast(
				$i18n.t('transfers.toasts.detected', { auto: res.autoLinked, ambiguous: res.ambiguous })
			);
			await load();
		} catch (err) {
			showToast(
				$i18n.t('transfers.toasts.error', {
					message: err instanceof Error ? err.message : String(err)
				})
			);
		} finally {
			busy = false;
		}
	}

	async function doUnlink(link: TransferLink) {
		confirmUnlink = null;
		try {
			await unlinkTransfer(link.id);
			showToast($i18n.t('transfers.toasts.unlinked'));
			await load();
		} catch (err) {
			showToast(
				$i18n.t('transfers.toasts.error', {
					message: err instanceof Error ? err.message : String(err)
				})
			);
		}
	}

	async function doLink(outLineId: number, inLineId: number) {
		try {
			await linkTransfer(outLineId, inLineId);
			showToast($i18n.t('transfers.toasts.linked'));
			await load();
		} catch (err) {
			const msg = err instanceof APIError ? err.message : String(err);
			showToast($i18n.t('transfers.toasts.error', { message: msg }));
		}
	}

	async function doDismiss(outLineId: number) {
		try {
			await dismissTransferGroup(outLineId);
			showToast($i18n.t('transfers.toasts.dismissed'));
			await load();
		} catch (err) {
			showToast(
				$i18n.t('transfers.toasts.error', {
					message: err instanceof Error ? err.message : String(err)
				})
			);
		}
	}

	const categorizeHref = (lineId: number) => `/categorize?focus=${lineId}`;
</script>

<svelte:head>
	<title>{$i18n.t('transfers.title')}</title>
</svelte:head>

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-2xl font-bold text-gray-900">{$i18n.t('transfers.title')}</h1>
			<p class="text-sm text-gray-500">{$i18n.t('transfers.subtitle')}</p>
		</div>
		<button
			type="button"
			onclick={runDetect}
			disabled={busy}
			data-detect
			class="px-3 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg shadow"
			>{$i18n.t('transfers.runDetection')}</button
		>
	</div>

	{#if error}
		<div class="bg-red-50 border-l-4 border-red-500 text-red-700 px-4 py-3 rounded-r-lg shadow-sm">
			<p class="text-sm">{error}</p>
		</div>
	{/if}

	<!-- Linked transfers -->
	<section class="bg-white shadow rounded-xl border border-gray-100 overflow-x-auto">
		<h2 class="font-semibold text-gray-900 text-sm px-4 pt-4">
			{$i18n.t('transfers.linkedTitle')}
		</h2>
		{#if links.length === 0}
			<p class="p-4 text-sm text-gray-500">{$i18n.t('transfers.noLinks')}</p>
		{:else}
			<table class="w-full text-sm whitespace-nowrap">
				<thead>
					<tr class="text-left text-gray-500 border-b bg-gray-50">
						<th class="py-2 px-3">{$i18n.t('transfers.date')}</th>
						<th class="py-2 px-3">{$i18n.t('transfers.route')}</th>
						<th class="py-2 px-3">{$i18n.t('transfers.message')}</th>
						<th class="py-2 px-3 text-right">{$i18n.t('transfers.amount')}</th>
						<th class="py-2 px-3">{$i18n.t('transfers.method')}</th>
						<th class="py-2 px-3">{$i18n.t('transfers.actions')}</th>
					</tr>
				</thead>
				<tbody>
					{#each links as link (link.id)}
						<tr class="border-b last:border-0 hover:bg-gray-50" data-transfer-link={link.id}>
							<td class="py-1.5 px-3">{formatDate(link.out.bookingDate, $locale)}</td>
							<td class="py-1.5 px-3">
								<a
									href={categorizeHref(link.out.id)}
									class="text-blue-600 hover:underline"
									data-view-out={link.out.id}>{link.out.accountName} #&#8203;{link.out.id}</a
								>
								→
								<a
									href={categorizeHref(link.in.id)}
									class="text-blue-600 hover:underline"
									data-view-in={link.in.id}>{link.in.accountName} #&#8203;{link.in.id}</a
								>
							</td>
							<td class="py-1.5 px-3 max-w-md truncate" title={link.out.message}
								>{link.out.message}</td
							>
							<td class="py-1.5 px-3 text-right font-variant-numeric-tabular text-red-700"
								>{formatMinor(link.out.amountMinor, $locale)}</td
							>
							<td class="py-1.5 px-3">
								<span
									class="px-1.5 py-0.5 rounded text-xs font-medium {link.method === 'auto'
										? 'bg-blue-100 text-blue-800'
										: 'bg-purple-100 text-purple-800'}"
									data-method={link.method}>{$i18n.t(`transfers.methodValue.${link.method}`)}</span
								>
							</td>
							<td class="py-1.5 px-3">
								{#if confirmUnlink === link.id}
									<button
										type="button"
										onclick={() => doUnlink(link)}
										data-unlink-confirm={link.id}
										class="px-2 py-1 rounded-lg bg-red-600 text-white text-xs hover:bg-red-700"
										>{$i18n.t('transfers.confirmUnlink')}</button
									>
									<button
										type="button"
										onclick={() => (confirmUnlink = null)}
										class="ml-1 px-2 py-1 rounded-lg border border-gray-200 text-xs hover:bg-gray-50"
										>{$i18n.t('transfers.cancel')}</button
									>
								{:else}
									<button
										type="button"
										onclick={() => (confirmUnlink = link.id)}
										data-unlink={link.id}
										class="px-2 py-1 rounded-lg border border-red-200 text-red-700 text-xs hover:bg-red-50"
										>{$i18n.t('transfers.unlink')}</button
									>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</section>

	<!-- Review: ambiguous candidates (TRF-3) -->
	<section class="bg-white shadow rounded-xl border border-gray-100 p-4 space-y-3">
		<h2 class="font-semibold text-gray-900 text-sm">{$i18n.t('transfers.reviewTitle')}</h2>
		{#if review.length === 0}
			<p class="text-sm text-gray-500">{$i18n.t('transfers.noReview')}</p>
		{:else}
			{#each review as group (reviewGroupKey(group))}
				<div
					class="border border-gray-200 rounded-lg p-3 space-y-2"
					data-review-group={group.outLine.id}
				>
					<div class="text-sm">
						<a
							href={categorizeHref(group.outLine.id)}
							class="text-blue-600 hover:underline"
							data-review-out={group.outLine.id}
						>
							{formatDate(group.outLine.bookingDate, $locale)} · {group.outLine.accountName} #&#8203;{group
								.outLine.id}
						</a>
						<span class="text-gray-500">· {group.outLine.message} ·</span>
						<span class="font-variant-numeric-tabular text-red-700"
							>{formatMinor(group.outLine.amountMinor, $locale)}</span
						>
					</div>
					<div class="flex flex-wrap items-center gap-2">
						<span class="text-xs text-gray-500">{$i18n.t('transfers.candidates')}:</span>
						{#each group.candidates as cand (cand.id)}
							<button
								type="button"
								onclick={() => doLink(group.outLine.id, cand.id)}
								data-candidate={cand.id}
								class="px-2 py-1 rounded-lg border border-gray-200 text-xs hover:bg-purple-50"
								title="{cand.accountName} · {formatMinor(cand.amountMinor, $locale)}"
							>
								{cand.accountName} #&#8203;{cand.id} · {formatMinor(cand.amountMinor, $locale)}
							</button>
						{/each}
						<button
							type="button"
							onclick={() => doDismiss(group.outLine.id)}
							data-dismiss={group.outLine.id}
							class="ml-auto px-2 py-1 rounded-lg border border-gray-200 text-gray-600 text-xs hover:bg-gray-50"
							>{$i18n.t('transfers.dismiss')}</button
						>
					</div>
				</div>
			{/each}
		{/if}
	</section>
</div>

{#if toast}
	<div
		class="fixed bottom-4 right-4 bg-gray-900 text-white text-sm px-4 py-2 rounded-lg shadow-lg z-50"
		role="status"
	>
		{toast}
	</div>
{/if}
