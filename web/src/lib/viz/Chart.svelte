<script lang="ts">
	// Generic ECharts wrapper (M5 D7): creates the chart on mount, updates on
	// option change, handles resize, cleans up on destroy. Uses echarts/core
	// with explicit imports for tree-shaking (no full bundle).
	import * as echarts from 'echarts/core';
	import { BarChart, LineChart, PieChart } from 'echarts/charts';
	import {
		GridComponent,
		TooltipComponent,
		LegendComponent,
		TitleComponent,
		DataZoomComponent
	} from 'echarts/components';
	import { CanvasRenderer } from 'echarts/renderers';
	import type { EChartsOption, EChartsType } from 'echarts';
	import { onMount, onDestroy } from 'svelte';

	echarts.use([
		BarChart,
		LineChart,
		PieChart,
		GridComponent,
		TooltipComponent,
		LegendComponent,
		TitleComponent,
		DataZoomComponent,
		CanvasRenderer
	]);

	interface Props {
		options: EChartsOption;
		height?: string;
		onClick?: (params: {
			name: string;
			seriesName: string;
			dataIndex: number;
			value: unknown;
		}) => void;
	}

	let { options, height = '320px', onClick }: Props = $props();

	let container: HTMLDivElement;
	let chart: EChartsType | null = null;

	onMount(() => {
		chart = echarts.init(container);
		chart.setOption(options);
		if (onClick) {
			chart.on('click', (params) => {
				onClick({
					name: (params as { name?: string }).name ?? '',
					seriesName: (params as { seriesName?: string }).seriesName ?? '',
					dataIndex: (params as { dataIndex?: number }).dataIndex ?? -1,
					value: (params as { value?: unknown }).value
				});
			});
		}
		const ro = new ResizeObserver(() => chart?.resize());
		ro.observe(container);
		onDestroy(() => {
			ro.disconnect();
			chart?.dispose();
			chart = null;
		});
	});

	$effect(() => {
		if (chart && options) {
			chart.setOption(options, { notMerge: true });
		}
	});
</script>

<div bind:this={container} style="width: 100%; height: {height};"></div>
