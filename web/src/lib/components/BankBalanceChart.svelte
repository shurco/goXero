<script lang="ts">
	import { formatDate } from '$lib/utils/format';
	import {
		bandPaths,
		buildChartGeom,
		insertZeroCrossings,
		strokeSegmentPaths
	} from '$lib/bank-balance-chart';

	interface Props {
		series: { t: number; balance: number }[];
		/** Ниже и плотнее — как area chart на карточке Xero (~⅓ высоты блока). */
		compact?: boolean;
	}
	let { series, compact = true }: Props = $props();

	const normalized = $derived.by(() => {
		if (series.length === 0) return [];
		if (series.length === 1) {
			const p = series[0]!;
			return insertZeroCrossings([
				{ t: p.t - 86400000 * 3, balance: p.balance },
				p,
				{ t: p.t + 86400000 * 3, balance: p.balance }
			]);
		}
		return insertZeroCrossings([...series]);
	});

	const g = $derived.by(() => {
		if (normalized.length === 0) return null;
		return compact
			? buildChartGeom(normalized, 800, 132, 36, 12, 6, 26)
			: buildChartGeom(normalized);
	});

	const paths = $derived.by(() => {
		if (!g || normalized.length < 2) return { blue: '', rose: '' };
		return bandPaths(normalized, g);
	});

	const strokeSegs = $derived.by(() => {
		if (!g || normalized.length < 2) return [] as { d: string; positive: boolean }[];
		return strokeSegmentPaths(normalized, g);
	});

	const xTicks = $derived.by(() => {
		if (!g) return [] as { x: number; label: string }[];
		const n = 5;
		const out: { x: number; label: string }[] = [];
		for (let i = 0; i < n; i++) {
			const t = g.tMin + ((g.tMax - g.tMin) * i) / (n - 1 || 1);
			out.push({ x: g.x(t), label: formatDate(new Date(t).toISOString().slice(0, 10), 'MMM D') });
		}
		return out;
	});

	const dotPts = $derived.by(() => {
		const p = normalized;
		if (p.length <= 14) return p;
		const maxDots = 14;
		const step = Math.max(1, Math.ceil(p.length / maxDots));
		const out: typeof p = [];
		for (let i = 0; i < p.length; i += step) out.push(p[i]!);
		const last = p[p.length - 1]!;
		if (out[out.length - 1]!.t !== last.t) out.push(last);
		return out;
	});
</script>

{#if g && series.length > 0 && normalized.length >= 2}
	<div class="bank-balance-chart border-t border-slate-200/80 bg-white">
		<div
			class="mx-auto w-full {compact ? 'max-h-[118px] min-h-[88px]' : 'max-h-[168px] min-h-[112px]'}"
			style:aspect-ratio="{g.W} / {g.H}"
		>
			<svg
				class="block h-full w-full"
				viewBox="0 0 {g.W} {g.H}"
				preserveAspectRatio="xMidYMid meet"
				aria-hidden="true"
			>
				<line
					x1={g.pl}
					y1={g.yZero}
					x2={g.W - g.pr}
					y2={g.yZero}
					stroke="#cbd5e1"
					stroke-width="1"
				/>

				{#if paths.blue}
					<path d={paths.blue} fill="rgb(191 219 254 / 0.95)" />
				{/if}
				{#if paths.rose}
					<path d={paths.rose} fill="rgb(254 202 202 / 0.9)" />
				{/if}

				{#each strokeSegs as seg, i (i + seg.d)}
					<path
						d={seg.d}
						fill="none"
						stroke={seg.positive ? '#2c6cb0' : '#dc2626'}
						stroke-width="1.75"
						stroke-linecap="round"
						stroke-linejoin="round"
					/>
				{/each}

				{#each dotPts as p, i (p.t + '-' + i)}
					<circle
						cx={g.x(p.t)}
						cy={g.y(p.balance)}
						r="3.5"
						fill="white"
						stroke={p.balance >= 0 ? '#2c6cb0' : '#dc2626'}
						stroke-width="1.75"
					/>
				{/each}

				{#each xTicks as tk (tk.label + '-' + tk.x)}
					<text
						x={tk.x}
						y={g.H - 6}
						text-anchor="middle"
						fill="#94a3b8"
						style="font-family: system-ui, -apple-system, sans-serif; font-size: 9px;"
					>
						{tk.label}
					</text>
				{/each}
			</svg>
		</div>
	</div>
{/if}
