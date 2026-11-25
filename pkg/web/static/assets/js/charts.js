// Diagramme mit ml-time-graph (vendored, task web:vendor). Treppen mit Fläche statt Balken: ein
// Wert gilt für seinen ganzen Abschnitt (10 Minuten bzw. Tag). Unter dem Nachrichtendiagramm ein
// Zustandsband: wann weitergeleitet wurde und wann nicht.
import { mount } from '../vendor/ml-time-graph.js';
import { el, STATES } from './api.js';

const darkQuery = matchMedia('(prefers-color-scheme: dark)');
const DARK = { textColor: '#94a3b8', axisColor: '#475569', gridStroke: '#1e293b', legendText: '#cbd5e1' };
const LIGHT = { textColor: '#64748b', axisColor: '#cbd5e1', gridStroke: '#e2e8f0', legendText: '#334155' };

export const COLORS = {
    received: '#0ea5e9',
    forwarded: '#22c55e',
    filtered: '#94a3b8',
    failed: '#ef4444',
    buffer: '#a855f7',
    device: '#0ea5e9',
};

const redraws = new Set();
let resizeTimer;
addEventListener('resize', () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => redraws.forEach((f) => f()), 200);
});
darkQuery.addEventListener('change', () => redraws.forEach((f) => f()));

// draw zeichnet in target und merkt sich, wie es bei Größen- oder Themenwechsel neu zeichnet.
function draw(target, build) {
    const run = () => {
        if (!target.isConnected || target.offsetParent === null) return; // unsichtbar: später
        target.replaceChildren();
        mount(target, { locale: 'en-GB', margin: 'auto', theme: darkQuery.matches ? DARK : LIGHT, ...build() });
    };
    if (target._redraw) redraws.delete(target._redraw);
    target._redraw = run;
    redraws.add(run);
    run();
}

// redrawAll zeichnet alles neu (nach dem Einblenden eines Tabs).
export const redrawAll = () => redraws.forEach((f) => f());

// withEnd hängt einen Punkt am Ende des letzten Abschnitts an: Eine Treppe endet sonst am Beginn
// ihres letzten Werts, und der letzte Abschnitt (heute) wäre unsichtbar.
function withEnd(data, slotMs, to) {
    if (data.length === 0) return data;
    const last = data[data.length - 1];
    return [...data, { time: Math.min(last.time + slotMs, to), value: last.value }];
}

const step = (color, slotMs, extra = {}) => ({ color, shape: 'step', width: 1.5, gapThreshold: slotMs * 2.5, ...extra });

function legend(items) {
    return el('div', { class: 'mb-2 flex flex-wrap gap-4 text-xs text-slate-500 dark:text-slate-400' },
        items.map(([name, color, dashed]) => el('span', { class: 'inline-flex items-center gap-1.5' },
            el('span', { class: `inline-block h-0.5 w-4 ${dashed ? 'border-t-2 border-dashed' : ''}`, style: dashed ? `border-color:${color}` : `background:${color}` }),
            name)));
}

function yDomain(values) {
    const m = Math.max(0, ...values.filter((v) => v != null));
    return [0, m > 0 ? Math.ceil(m * 1.1) : 1];
}

// messagesChart: empfangen (Fläche), weitergeleitet, gefiltert; darunter das Zustandsband.
export function messagesChart(box, { points, spans, from, to, slotMs }) {
    box.replaceChildren(legend([
        ['Received', COLORS.received], ['Forwarded', COLORS.forwarded], ['Filtered', COLORS.filtered, true],
    ]));
    const target = el('div', { class: 'relative' });
    box.append(target, stateLegend());
    const series = (name, key, style) => ({ name, data: withEnd(points.map((p) => ({ time: p.t, value: p[key] })), slotMs, to), style });
    draw(target, () => ({
        height: 280,
        axes: { x: { domain: [from, to] }, left: { domain: yDomain(points.map((p) => p.received)) } },
        series: [
            series('Received', 'received', { line: step(COLORS.received, slotMs), fill: `${COLORS.received}33` }),
            series('Forwarded', 'forwarded', { line: step(COLORS.forwarded, slotMs, { width: 2 }) }),
            series('Filtered', 'filtered', { line: step(COLORS.filtered, slotMs, { style: 'dashed' }) }),
        ],
        annotationBands: [{
            name: 'Forwarding', height: 14, minItemWidth: 3,
            items: spans.map((s) => ({
                startTime: Date.parse(s.start), endTime: Date.parse(s.end),
                fill: (STATES[s.state] || STATES.ok).band,
            })),
        }],
        tooltip: { show: true },
    }));
}

function stateLegend() {
    return el('div', { class: 'mt-1 flex flex-wrap gap-4 text-xs text-slate-500 dark:text-slate-400' },
        Object.values(STATES).map((s) => el('span', { class: 'inline-flex items-center gap-1.5' },
            el('span', { class: 'inline-block size-2.5 rounded-sm', style: `background:${s.band}` }), s.label)),
        el('span', { class: 'inline-flex items-center gap-1.5' },
            el('span', { class: 'inline-block size-2.5 rounded-sm border border-slate-300 dark:border-slate-600' }), 'no data (proxy not running)'));
}

// bufferChart: höchster Pufferstand je Abschnitt.
export function bufferChart(box, { points, from, to, slotMs }) {
    const target = el('div', { class: 'relative' });
    box.replaceChildren(target);
    draw(target, () => ({
        height: 150,
        axes: { x: { domain: [from, to] }, left: { domain: yDomain(points.map((p) => p.buffer_max)) } },
        series: [{
            name: 'Buffer', data: withEnd(points.map((p) => ({ time: p.t, value: p.buffer_max })), slotMs, to),
            style: { line: step(COLORS.buffer, slotMs), fill: `${COLORS.buffer}33` },
        }],
        tooltip: { show: true },
    }));
}

// deviceChart: Nachrichten eines Geräts je Stunde oder Tag.
export function deviceChart(box, { points, from, to, slotMs }) {
    const target = el('div', { class: 'relative' });
    box.replaceChildren(target);
    draw(target, () => ({
        height: 140,
        axes: { x: { domain: [from, to] }, left: { domain: yDomain(points.map((p) => p.count)) } },
        series: [{
            name: 'Messages', data: withEnd(points.map((p) => ({ time: Date.parse(p.start), value: p.count })), slotMs, to),
            style: { line: step(COLORS.device, slotMs), fill: `${COLORS.device}33` },
        }],
        tooltip: { show: true },
    }));
}
