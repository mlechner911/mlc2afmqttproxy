// Diagramme mit ml-time-graph (vendored, task web:vendor). Treppen mit Fläche statt Balken: ein
// Wert gilt für seinen ganzen Abschnitt (10 Minuten bzw. Tag). Unter dem Nachrichtendiagramm ein
// Zustandsband: wann weitergeleitet wurde und wann nicht.
import { mount, zeigerImSvg } from '../vendor/ml-time-graph.js';
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
// zoom (optional): { onSelect(from, to), onReset() } — Bereich aufziehen zoomt, Doppelklick zurück.
function draw(target, build, zoom) {
    const run = () => {
        if (!target.isConnected || target.offsetParent === null) return; // unsichtbar: später
        target.replaceChildren();
        const chart = mount(target, { locale: 'en-GB', margin: 'auto', theme: darkQuery.matches ? DARK : LIGHT, ...build() });
        if (zoom) attachBrush(target, chart, zoom);
    };
    if (target._redraw) redraws.delete(target._redraw);
    target._redraw = run;
    redraws.add(run);
    run();
}

// attachBrush: Bereich im Diagramm mit der Maus aufziehen → zoom.onSelect(von, bis); Doppelklick →
// zoom.onReset(). ml-time-graph bringt Zoom nur als Zustand mit (interaction/Zoom, Mausrad); das
// Aufziehen braucht nur invertTime, plotRect und zeigerImSvg aus dem Hauptpaket.
function attachBrush(target, chart, zoom) {
    const svg = target.querySelector('svg');
    if (!svg) return;
    const box = el('div', { class: 'pointer-events-none absolute hidden rounded-sm border border-sky-500/60 bg-sky-500/15' });
    target.append(box);
    target.classList.add('cursor-crosshair', 'select-none');
    let start = null;
    const plot = () => chart.plotRect;
    const toCss = (svgX) => { // SVG-Pixel → CSS-Pixel relativ zu target
        const r = svg.getBoundingClientRect(), t = target.getBoundingClientRect();
        const w = svg.viewBox?.baseVal?.width || r.width;
        return r.left - t.left + (svgX * r.width) / w;
    };
    const cssY = (svgY) => {
        const r = svg.getBoundingClientRect(), t = target.getBoundingClientRect();
        const h = svg.viewBox?.baseVal?.height || r.height;
        return r.top - t.top + (svgY * r.height) / h;
    };
    const clampX = (x) => Math.min(Math.max(x, plot().x), plot().x + plot().width);
    target.addEventListener('pointerdown', (ev) => {
        if (ev.button !== 0) return;
        const p = zeigerImSvg(svg, ev.clientX, ev.clientY), pr = plot();
        if (p.x < pr.x || p.x > pr.x + pr.width || p.y < pr.y || p.y > pr.y + pr.height) return;
        start = p.x;
        target.setPointerCapture(ev.pointerId);
    });
    target.addEventListener('pointermove', (ev) => {
        if (start == null) return;
        const x = clampX(zeigerImSvg(svg, ev.clientX, ev.clientY).x), pr = plot();
        const a = toCss(Math.min(start, x)), b = toCss(Math.max(start, x));
        Object.assign(box.style, { left: `${a}px`, width: `${b - a}px`, top: `${cssY(pr.y)}px`, height: `${cssY(pr.y + pr.height) - cssY(pr.y)}px` });
        box.classList.remove('hidden');
    });
    target.addEventListener('pointerup', (ev) => {
        if (start == null) return;
        const x = clampX(zeigerImSvg(svg, ev.clientX, ev.clientY).x);
        const a = Math.min(start, x), b = Math.max(start, x);
        start = null;
        box.classList.add('hidden');
        if (toCss(b) - toCss(a) >= 8) zoom.onSelect(chart.invertTime(a), chart.invertTime(b));
    });
    target.addEventListener('dblclick', () => zoom.onReset());
}

// tooltip: kompakt — Zeitraum des Abschnitts als Kopf, je Serie Farbe, Name, ganze Zahl. Der
// Standard schrieb Nachkommastellen und je Wert die Uhrzeit dazu und brach am Rand um.
const int = new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 });
const dayFmt = new Intl.DateTimeFormat('en-GB', { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' });
const timeFmt = new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit' });
function tooltip(slotMs) {
    return {
        show: true,
        format: (samples, cursor) => {
            const t = samples.length ? Math.max(...samples.map((s) => s.time)) : cursor.time;
            const head = slotMs >= 86400e3
                ? dayFmt.format(t)
                : `${dayFmt.format(t)}, ${timeFmt.format(t)}–${timeFmt.format(t + slotMs)}`;
            const rows = samples.map((s) => {
                const line = s.series.style?.line;
                const color = (Array.isArray(line) ? line[0] : line)?.color || '#94a3b8';
                return `<div class="mlc-tooltip__row"><span class="mlc-tooltip__dot" style="background:${color}"></span>`
                    + `<span class="mlc-tooltip__name">${s.series.name}</span><b>${int.format(s.value)}</b></div>`;
            });
            return `<div class="mlc-tooltip__time">${head}</div>${rows.join('')}`;
        },
    };
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
export function messagesChart(box, { points, spans, from, to, slotMs, zoom }) {
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
        tooltip: tooltip(slotMs),
    }), zoom);
}

function stateLegend() {
    return el('div', { class: 'mt-1 flex flex-wrap gap-4 text-xs text-slate-500 dark:text-slate-400' },
        Object.values(STATES).map((s) => el('span', { class: 'inline-flex items-center gap-1.5' },
            el('span', { class: 'inline-block size-2.5 rounded-sm', style: `background:${s.band}` }), s.label)),
        el('span', { class: 'inline-flex items-center gap-1.5' },
            el('span', { class: 'inline-block size-2.5 rounded-sm border border-slate-300 dark:border-slate-600' }), 'no data (proxy not running)'));
}

// bufferChart: höchster Pufferstand je Abschnitt.
export function bufferChart(box, { points, from, to, slotMs, zoom }) {
    const target = el('div', { class: 'relative' });
    box.replaceChildren(target);
    draw(target, () => ({
        height: 150,
        axes: { x: { domain: [from, to] }, left: { domain: yDomain(points.map((p) => p.buffer_max)) } },
        series: [{
            name: 'Buffer', data: withEnd(points.map((p) => ({ time: p.t, value: p.buffer_max })), slotMs, to),
            style: { line: step(COLORS.buffer, slotMs), fill: `${COLORS.buffer}33` },
        }],
        tooltip: tooltip(slotMs),
    }), zoom);
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
        tooltip: tooltip(slotMs),
    }));
}
