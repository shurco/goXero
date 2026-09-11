import { xeroDate } from '$lib/utils/format';
import type { LedgerBalancePoint } from '$lib/types';

/**
 * Geometry for the balance graph on a bank account card.
 *
 * Everything here is in the graph's own pixels, and the graph is drawn at the
 * width it is given rather than scaled to fit: Xero sizes the plot in real
 * pixels, and a viewBox stretch is what turns its 1px stroke into a hairline in
 * one place and a 2px smear in another, and its markers into ellipses. So the
 * component measures itself and hands the measurement in.
 *
 * The numbers are Xero's, read off go.xero.com: a 70px plot, points inset 4px
 * from each edge, and a domain that starts at zero — never at the data's
 * minimum, and never padded by a percentage of it, which is what makes the
 * zero line land on the same pixel every time.
 */

/** Height of the plot. The 30px axis strip below it is separate (see AXIS_BAND_HEIGHT). */
export const CHART_HEIGHT = 70;
/** The date labels keep their own strip, so a low marker never lands on them. */
export const AXIS_BAND_HEIGHT = 30;
/** Height of the axis svg centred inside its band. */
export const AXIS_HEIGHT = 20;
/** The series is inset from each edge of the canvas; the axis is not (Xero's own quirk). */
export const POINT_INSET = 4;
/** The domain floor is zero, so the zero line sits 4px above the canvas bottom. */
export const ZERO_Y = 66;
/** The tallest balance in the window is drawn 4px below the top of the canvas. */
export const TOP_Y = 4;
/** Xero's colours, for the side of zero a point falls on. */
export const POSITIVE = { line: '#0078c8', fill: '#a6d0ec' };
export const NEGATIVE = { line: '#dc3246', fill: '#f3b7be' };
/** The rule the area is filled down to. */
export const BASELINE_COLOR = '#a6a9b0';
/** Axis labels — and the tick marks above them, which take currentColor. */
export const AXIS_COLOR = '#404756';

/** One day of the series, placed on the canvas. */
export interface ChartPoint {
	/** The calendar day, as the API writes it: YYYY-MM-DD. */
	date: string;
	value: number;
	x: number;
	y: number;
	positive: boolean;
}

export interface BalanceChart {
	/** The width the graph was drawn at, in pixels. */
	W: number;
	H: number;
	points: ChartPoint[];
	/** The top of the domain — the largest balance in the window, or 0 when none is positive. */
	max: number;
	zeroY: number;
	/** The d3 time ticks Xero shows: one a week, anchored on the week start. */
	ticks: { x: number; date: string; label: string }[];
}

/** The tooltip's heading reads the day out in full ("22 August 2026"). */
export function tooltipDate(date: string): string {
	return xeroDate(date, 'D MMMM YYYY');
}

/** The marker's accessible name, which Xero also reads out in full. */
export function markerLabel(point: ChartPoint): string {
	return `${tooltipDate(point.date)} Balance ${point.value.toFixed(2)}`;
}

/**
 * Place the series on a canvas of the given width. Returns null when there is
 * nothing to draw — a graph needs two points to have a line between them, and a
 * width to be drawn at.
 */
export function buildBalanceChart(series: LedgerBalancePoint[], W: number): BalanceChart | null {
	const values = series.map((p) => Number(p.Balance ?? 0)).filter((v) => Number.isFinite(v));
	if (values.length < 2 || W <= 0) return null;

	const max = Math.max(0, ...values);
	const days = series.map((p) => Date.parse(`${p.Date}T00:00:00Z`));
	const n = series.length;

	// The points are inset; the axis underneath them is not. The two therefore
	// disagree by up to 4px about where a day sits, which is Xero's own
	// arrangement and is kept.
	const x = (i: number) => POINT_INSET + (i * (W - 2 * POINT_INSET)) / (n - 1);
	// A window with nothing positive in it has no domain to scale against, so
	// every point lands on the zero line — which is where Xero's own 5px clip
	// band leaves a negative anyway.
	const y = (value: number) =>
		max > 0 ? TOP_Y + (1 - value / max) * (ZERO_Y - TOP_Y) : ZERO_Y;

	const points: ChartPoint[] = series.map((p, i) => {
		const value = Number(p.Balance ?? 0);
		return {
			date: p.Date,
			value,
			x: x(i),
			y: y(value),
			positive: value >= 0
		};
	});

	return {
		W,
		H: CHART_HEIGHT,
		points,
		max,
		zeroY: ZERO_Y,
		ticks: buildTicks(days, W)
	};
}

/**
 * d3's time ticks, as Xero shows them over a month: one a week, anchored on the
 * week start rather than on the window — so the labels fall on Sundays
 * whatever day the month happens to begin. A window too short to hold a week
 * start falls back to evenly spaced days, which is what d3 would do too.
 */
function buildTicks(days: number[], W: number): BalanceChart['ticks'] {
	const first = days[0]!;
	const last = days[days.length - 1]!;
	const span = last - first || 1;
	const at = (t: number) => ((t - first) / span) * W;

	const sundays: number[] = [];
	const DAY = 86400000;
	// 1970-01-01 was a Thursday, so an epoch day is 4 past a Sunday.
	const firstSunday = first + ((7 - ((Math.floor(first / DAY) + 4) % 7)) % 7) * DAY;
	for (let t = firstSunday; t <= last; t += 7 * DAY) sundays.push(t);

	const stamps = sundays.length > 0
		? sundays
		: Array.from({ length: 5 }, (_, i) => first + (span * i) / 4);

	return stamps.map((t) => {
		const date = new Date(t).toISOString().slice(0, 10);
		return { x: at(t), date, label: xeroDate(date, 'D MMM') };
	});
}

/** The polyline through the points. Both sides are drawn from the same path. */
export function linePath(points: ChartPoint[]): string {
	return points
		.map((p, i) => `${i === 0 ? 'M' : 'L'} ${round(p.x)} ${round(p.y)}`)
		.join(' ');
}

/**
 * The area between the line and the zero line, closed along the baseline. Both
 * fills come from this one path: the blue is the part above the baseline, the
 * red the part below, and Xero's two clips do the splitting — which is also how
 * a single segment that crosses zero gets both colours without being cut in two.
 */
export function areaPath(points: ChartPoint[], zeroY: number): string {
	const top = points.map((p, i) => `${i === 0 ? 'M' : 'L'} ${round(p.x)} ${round(p.y)}`).join(' ');
	const back = round(points[points.length - 1]!.x) + ' ' + round(zeroY);
	const home = round(points[0]!.x) + ' ' + round(zeroY);
	return `${top} L ${back} L ${home} Z`;
}

/**
 * The point nearest the pointer along x. Xero selects on the horizontal only:
 * the pointer can be far above or below the line, or well past the last point,
 * and the nearest day still wins — which is what makes the whole plot hoverable
 * rather than just the 3px markers, and what puts the tooltip above the
 * pointer's column rather than under it.
 */
export function nearestPoint(points: ChartPoint[], x: number): number {
	let best = 0;
	for (let i = 1; i < points.length; i++) {
		if (Math.abs(points[i]!.x - x) < Math.abs(points[best]!.x - x)) best = i;
	}
	return best;
}

/** Sub-pixel precision is enough for a 1px stroke, and keeps the DOM readable. */
function round(n: number): number {
	return Math.round(n * 100) / 100;
}
