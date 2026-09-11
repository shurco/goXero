<script lang="ts">
	import {
		AXIS_BAND_HEIGHT,
		AXIS_COLOR,
		AXIS_HEIGHT,
		BASELINE_COLOR,
		CHART_HEIGHT,
		NEGATIVE,
		POSITIVE,
		areaPath,
		buildBalanceChart,
		linePath,
		markerLabel,
		nearestPoint,
		tooltipDate
	} from '$lib/bank-balance-chart';
	import type { LedgerBalancePoint } from '$lib/types';
	import { formatAmount, formatCurrency } from '$lib/utils/format';

	interface Props {
		/** One ledger-balance point per calendar day, oldest first. */
		series: LedgerBalancePoint[];
		/** The account's own currency. */
		currency?: string;
		/** The organisation's currency, which the card prints without a code. */
		baseCurrency?: string;
	}
	let { series, currency = 'USD', baseCurrency = currency }: Props = $props();

	/** Xero draws the plot in real pixels at whatever width it has, so the width
	 *  is measured off the element and handed to the geometry. Stretching it with
	 *  a viewBox — which this chart used to do — is what turns a 1px stroke into a
	 *  hairline at one width and a 2px smear at another, and a marker into an
	 *  ellipse. */
	let width = $state(0);
	let graphEl: HTMLDivElement | null = $state(null);
	/** The date labels keep their own strip below the plot; "Skip graph" steps
	 *  over the thirty markers and lands here. */
	let axisEl: HTMLDivElement | null = $state(null);
	/** The day the pointer is over, or null when it is away. Xero builds the
	 *  tooltip on the first hover and takes it out of the DOM again on leaving,
	 *  so it is state, not a hidden element. */
	let hovered = $state<number | null>(null);

	const chart = $derived(buildBalanceChart(series, width));
	const area = $derived(chart ? areaPath(chart.points, chart.zeroY) : '');
	const line = $derived(chart ? linePath(chart.points) : '');
	const point = $derived(chart && hovered !== null ? (chart.points[hovered] ?? null) : null);
	const clip = $props.id();

	/** Xero selects on the horizontal only. The pointer can sit well above or
	 *  below the line, or past the last point, and still picks the nearest day —
	 *  which is what makes the whole plot hoverable rather than the 3px markers. */
	function trackPointer(event: PointerEvent) {
		if (!chart || !graphEl) return;
		hovered = nearestPoint(chart.points, event.clientX - graphEl.getBoundingClientRect().left);
	}

	/** A balance in the organisation's own currency is printed as a bare number;
	 *  only one held in something else carries the code, as on the card above. */
	function amount(value: number): string {
		return currency === baseCurrency ? formatAmount(value) : formatCurrency(value, currency);
	}
</script>

<!-- Xero's graph block: a 70px plot with its own 30px strip of dates under it.
     A series too short to draw — an account the endpoint could not answer for —
     leaves the card without a graph rather than with an empty 100px gap. -->
{#if series.length >= 2}
	<div class="h-[100px]" data-automationid="balance-graph">
		<div
			class="relative h-[70px]"
			data-automationid="graph"
			role="group"
			aria-label="Balance graph"
			bind:this={graphEl}
			bind:clientWidth={width}
			onpointermove={trackPointer}
			onpointerleave={() => (hovered = null)}
		>
			<!-- First child, and off-screen until it is focused: the markers are the
			     graph's accessible content, and thirty of them is a lot to tab
			     through on the way to the rest of the card. -->
			<button type="button" class="skip-graph" onclick={() => axisEl?.focus()}>Skip graph</button>

			{#if chart}
				<svg class="block" width={chart.W} height={CHART_HEIGHT}>
					<defs>
						<!-- Xero clips each side to its own band: the positive line and fill to
						     everything above the zero line, the negative ones to the 5px strip
						     below it. That is why a balance under zero reads as a sliver along
						     the baseline rather than plunging off the bottom of the canvas —
						     and why one path can be drawn twice, in both colours, and come out
						     split at the crossing. -->
						<clipPath id="{clip}-positive">
							<rect x="0" y="0" width={chart.W} height="67" />
						</clipPath>
						<clipPath id="{clip}-negative">
							<rect x="0" y="66" width={chart.W} height="5" />
						</clipPath>
					</defs>

					<!-- Xero's own invisible gridline layer, kept so the drawing matches
					     element for element. -->
					<path
						class="balance-chart--domain"
						d="M0,{chart.zeroY}H{chart.W}"
						fill="none"
						stroke="none"
					/>

					<g clip-path="url(#{clip}-positive)">
						<path d={area} fill={POSITIVE.fill} />
						<path d={line} fill="none" stroke={POSITIVE.line} stroke-width="1" />
					</g>
					<g clip-path="url(#{clip}-negative)">
						<path d={area} fill={NEGATIVE.fill} />
						<path d={line} fill="none" stroke={NEGATIVE.line} stroke-width="1" />
					</g>

					<line
						x1="0"
						y1={chart.zeroY}
						x2={chart.W}
						y2={chart.zeroY}
						stroke={BASELINE_COLOR}
						stroke-width="1"
					/>

					<!-- One marker per day, drawn over the baseline. They keep r=3 and their
					     own colour while hovering: Xero moves the tooltip, not the marker. -->
					<g clip-path="url(#{clip}-positive)">
						{#each chart.points.filter((p) => p.positive) as p (p.date)}
							<!-- Xero makes every marker a tab stop, so the series can be read a
							     day at a time without a pointer. The warning is about the
							     pattern, which here is the point. -->
							<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
							<circle
								cx={p.x}
								cy={p.y}
								r="3"
								fill="#ffffff"
								stroke={POSITIVE.line}
								stroke-width="1"
								shape-rendering="geometricPrecision"
								role="img"
								tabindex="0"
								aria-label={markerLabel(p)}
							/>
						{/each}
					</g>
					<g clip-path="url(#{clip}-negative)">
						{#each chart.points.filter((p) => !p.positive) as p (p.date)}
							<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
							<circle
								cx={p.x}
								cy={p.y}
								r="3"
								fill="#ffffff"
								stroke={NEGATIVE.line}
								stroke-width="1"
								shape-rendering="geometricPrecision"
								role="img"
								tabindex="0"
								aria-label={markerLabel(p)}
							/>
						{/each}
					</g>
				</svg>
			{/if}

			<!-- Absolutely positioned in the plot's own coordinates, above the point and
			     centred on it. Xero does not clamp it, so on the first day of the window
			     it hangs off the left edge of the card; nor does it open on focus, which
			     is why there is no focus handler here. -->
			{#if point}
				<div
					role="dialog"
					data-automationid="bank-tooltip"
					class="pointer-events-none absolute z-[1] w-[180px] rounded-[3px] bg-white"
					style="left: {point.x}px; top: {point.y -
						10}px; transform: translateX(-50%) translateY(-100%); box-shadow: 0 0 0 1px rgba(0, 10, 30, 0.2), 0 3px 6px 0 rgba(0, 10, 30, 0.2);"
				>
					<div class="border-b border-[#ccced2]">
						<h3
							class="m-0 font-bold text-xero-ink"
							style="margin: 12px 16px; font-size: 0.95rem; line-height: 20px;"
						>
							{tooltipDate(point.date)}
						</h3>
					</div>
					<div class="flex">
						<div
							class="w-1/2"
							style="padding: 8px 16px; font-size: 13px; line-height: 20px; color: {point.value >=
							0
								? POSITIVE.line
								: NEGATIVE.line};"
						>
							Balance
						</div>
						<div
							class="w-1/2 text-right"
							style="padding: 8px 16px; font-size: 13px; line-height: 20px; color: {point.value >=
							0
								? POSITIVE.line
								: NEGATIVE.line};"
						>
							{amount(point.value)}
						</div>
					</div>
				</div>
			{/if}
		</div>

		<!-- The dates take currentColor for their tick marks, which is how d3 draws
		     them; the labels are the same colour. -->
		<div
			class="flex flex-col justify-center"
			style="height: {AXIS_BAND_HEIGHT}px; color: {AXIS_COLOR};"
			bind:this={axisEl}
			tabindex="-1"
		>
			{#if chart}
				<svg class="block" width={chart.W} height={AXIS_HEIGHT} aria-hidden="true">
					<path class="balance-chart--domain" d="M0,0H{chart.W}" fill="none" stroke="none" />
					{#each chart.ticks as tick (tick.date)}
						<g class="tick" transform="translate({tick.x},0)" text-anchor="middle">
							<line y2="0" stroke="currentColor" />
							<text y="3" dy="0.71em" fill={AXIS_COLOR} style="font-size: 12px;">
								{tick.label}
							</text>
						</g>
					{/each}
				</svg>
			{/if}
		</div>
	</div>
{/if}

<style>
	/* Xero keeps the skip button in the DOM and out of the way, and brings it
	   back only when it is focused: reachable by keyboard, invisible to a mouse. */
	.skip-graph {
		position: absolute;
		left: -9999px;
		width: 1px;
		height: 1px;
		overflow: hidden;
	}
	.skip-graph:focus {
		left: 0;
		top: 0;
		z-index: 2;
		width: auto;
		height: auto;
		overflow: visible;
		border: 1px solid #0078c8;
		border-radius: 3px;
		background: #ffffff;
		padding: 4px 8px;
		font-size: 13px;
		line-height: 20px;
		color: #0078c8;
	}
</style>
