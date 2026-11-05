<script lang="ts">
	import { i18n } from '$lib/i18n';
	import '../app.css';

	let { children } = $props();
</script>
<div class="min-h-screen bg-gradient-to-br from-gray-50 to-gray-100">
	<!-- Header -->
	<header class="bg-white shadow-sm border-b border-gray-200 sticky top-0 z-50">
		<div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
			<div class="flex justify-between items-center h-16">
				<div class="flex items-center space-x-3">
					<div class="w-10 h-10 bg-gradient-to-br from-blue-600 to-blue-800 rounded-lg flex items-center justify-center shadow-md">
						<span class="text-white font-bold text-xl">FA</span>
					</div>
					<div>
						<h1 class="text-xl font-bold text-gray-900">FinanceAnalysis</h1>
						<p class="text-sm text-gray-500">{$i18n.t('app.name')}</p>
					</div>
				</div>
				
				<!-- Language switcher -->
				<div class="flex items-center space-x-2">
					<a 
						href="/en" 
						class="px-3 py-1 text-sm font-medium rounded-md transition-colors {i18n.locale === 'en' ? 'bg-blue-100 text-blue-800' : 'text-gray-600 hover:text-gray-900 hover:bg-gray-100'}"
					>
						EN
					</a>
					<a 
						href="/da" 
						class="px-3 py-1 text-sm font-medium rounded-md transition-colors {i18n.locale === 'da' ? 'bg-blue-100 text-blue-800' : 'text-gray-600 hover:text-gray-900 hover:bg-gray-100'}"
					>
						DA
					</a>
				</div>
			</div>
		</div>
	</header>

	<!-- Main content -->
	<main class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8 flex-grow">
		{@render children()}
	</main>

	<!-- Footer -->
	<footer class="bg-gray-800 text-gray-300 mt-auto">
		<div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
			<div class="flex flex-col md:flex-row justify-between items-center">
				<div class="mb-4 md:mb-0">
					<p class="text-sm">&copy; {new Date().getFullYear()} FinanceAnalysis. All rights reserved.</p>
				</div>
			</div>
		</div>
	</footer>
</div>
