<script lang="ts">
	import { i18n } from '$lib/i18n';
</script>

<svelte:head>
	<title>{$i18n.t('welcome.title')}</title>
</svelte:head>

<div class="flex flex-col items-center justify-center min-h-[calc(100vh-160px)] p-4">
	<div class="w-full max-w-lg space-y-6">
		<div class="text-center">
			<h1 class="text-3xl font-bold text-gray-900">{$i18n.t('welcome.title')}</h1>
			<p class="mt-2 text-gray-600">{$i18n.t('welcome.subtitle')}</p>
		</div>

		<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
			<a
				href="/welcome/new"
				class="block bg-white shadow-xl rounded-xl p-6 border border-gray-100 hover:border-blue-400 hover:shadow-lg transition-all"
			>
				<h2 class="text-lg font-semibold text-gray-900">{$i18n.t('welcome.createNew')}</h2>
				<p class="text-sm text-gray-600 mt-2">{$i18n.t('welcome.createNewHint')}</p>
			</a>
			<a
				href="/welcome/upload"
				class="block bg-white shadow-xl rounded-xl p-6 border border-gray-100 hover:border-blue-400 hover:shadow-lg transition-all"
			>
				<h2 class="text-lg font-semibold text-gray-900">{$i18n.t('welcome.uploadExisting')}</h2>
				<p class="text-sm text-gray-600 mt-2">{$i18n.t('welcome.uploadExistingHint')}</p>
			</a>
		</div>
	</div>
</div>
