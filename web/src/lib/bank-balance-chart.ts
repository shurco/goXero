import { num } from '$lib/dashboard-utils';
import type { BankTransaction } from '$lib/types';

export function signedTxnAmount(t: BankTransaction): number {
	const raw = num(t.Total);
	return t.Type === 'SPEND' || t.Type?.startsWith('SPEND') ? -Math.abs(raw) : Math.abs(raw);
}

/** Cumulative statement balance after each transaction (chronological). */
export function runningBalanceSeries(txs: BankTransaction[]): { t: number; balance: number }[] {
	const sorted = [...txs]
		.filter((t) => t.Date)
		.sort((a, b) => new Date(a.Date!).getTime() - new Date(b.Date!).getTime());
	let run = 0;
	const out: { t: number; balance: number }[] = [];
	for (const t of sorted) {
		run += signedTxnAmount(t);
		out.push({ t: new Date(t.Date!).getTime(), balance: run });
	}
	return out;
}

/** Insert points at balance = 0 between sign changes so area fills don’t self-intersect. */
export function insertZeroCrossings(series: { t: number; balance: number }[]): { t: number; balance: number }[] {
	if (series.length < 2) return series;
	const out: { t: number; balance: number }[] = [];
	for (let i = 0; i < series.length; i++) {
		out.push(series[i]!);
		const b = series[i + 1];
		if (!b) break;
		const a = series[i]!;
		if (a.balance === 0 || b.balance === 0) continue;
		if ((a.balance > 0 && b.balance < 0) || (a.balance < 0 && b.balance > 0)) {
			const frac = Math.abs(a.balance) / (Math.abs(a.balance) + Math.abs(b.balance));
			const t = a.t + frac * (b.t - a.t);
			out.push({ t, balance: 0 });
		}
	}
	return out.sort((x, y) => x.t - y.t);
}

export interface ChartGeom {
	W: number;
	H: number;
	pl: number;
	pr: number;
	pt: number;
	pb: number;
	tMin: number;
	tMax: number;
	minB: number;
	maxB: number;
	yZero: number;
	x(t: number): number;
	y(b: number): number;
}

export function buildChartGeom(
	series: { t: number; balance: number }[],
	W = 800,
	H = 200,
	pl = 44,
	pr = 16,
	pt = 14,
	pb = 36
): ChartGeom | null {
	if (series.length === 0) return null;
	const pts = [...series];
	const balances = pts.map((p) => p.balance);
	let minB = Math.min(0, ...balances);
	let maxB = Math.max(0, ...balances);
	if (minB === maxB) {
		minB -= 1;
		maxB += 1;
	}
	const padB = (maxB - minB) * 0.08;
	minB -= padB;
	maxB += padB;
	const tMin = Math.min(...pts.map((p) => p.t));
	const tMax = Math.max(...pts.map((p) => p.t));
	const innerW = W - pl - pr;
	const innerH = H - pt - pb;
	const x = (t: number) => pl + ((t - tMin) / (tMax - tMin || 1)) * innerW;
	const y = (b: number) => pt + ((maxB - b) / (maxB - minB)) * innerH;
	const yZero = y(0);
	return { W, H, pl, pr, pt, pb, tMin, tMax, minB, maxB, yZero, x, y };
}

/** SVG path for the balance line. */
export function linePath(pts: { t: number; balance: number }[], g: ChartGeom): string {
	const d = pts.map((p, i) => `${i === 0 ? 'M' : 'L'} ${g.x(p.t).toFixed(2)} ${g.y(p.balance).toFixed(2)}`);
	return d.join(' ');
}

/** Separate polylines above/below zero (Xero-style blue / red stroke). Requires zero-crossing points in series. */
export function strokeSegmentPaths(
	pts: { t: number; balance: number }[],
	g: ChartGeom
): { d: string; positive: boolean }[] {
	if (pts.length < 2) return [];
	const out: { d: string; positive: boolean }[] = [];
	for (let i = 0; i < pts.length - 1; i++) {
		const a = pts[i]!;
		const b = pts[i + 1]!;
		const d = `M ${g.x(a.t).toFixed(2)} ${g.y(a.balance).toFixed(2)} L ${g.x(b.t).toFixed(2)} ${g.y(b.balance).toFixed(2)}`;
		const mid = (a.balance + b.balance) / 2;
		out.push({ d, positive: mid >= 0 });
	}
	return out;
}

/**
 * Filled polygons between the line and the zero line (blue above zero, rose below).
 */
export function bandPaths(
	pts: { t: number; balance: number }[],
	g: ChartGeom
): { blue: string; rose: string } {
	let blue = '';
	let rose = '';
	for (let i = 0; i < pts.length - 1; i++) {
		const a = pts[i]!;
		const b = pts[i + 1]!;
		const xa = g.x(a.t);
		const ya = g.y(a.balance);
		const xb = g.x(b.t);
		const yb = g.y(b.balance);
		const yZ = g.yZero;
		if (a.balance >= 0 && b.balance >= 0 && (a.balance > 0 || b.balance > 0)) {
			blue += `M ${xa} ${ya} L ${xb} ${yb} L ${xb} ${yZ} L ${xa} ${yZ} Z `;
		} else if (a.balance <= 0 && b.balance <= 0 && (a.balance < 0 || b.balance < 0)) {
			rose += `M ${xa} ${ya} L ${xb} ${yb} L ${xb} ${yZ} L ${xa} ${yZ} Z `;
		}
	}
	return { blue: blue.trim(), rose: rose.trim() };
}
