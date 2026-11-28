// Statistik-Tab: Zeitraum, Kennzahlen, Diagramme, Geräte. Unterbrechungen und Filter haben eigene
// Tabs (interruptions.js, filters.js).
import { API, getJSON, fmtNum, fmtDuration, fmtDateTime, el, HTTPError } from './api.js';
import { messagesChart, bufferChart } from './charts.js';
import { loadDevices } from './devices.js';

const HOUR = 3600e3, DAY = 24 * HOUR, SLOT = 10 * 60e3;

// Tag und Woche aus den 10-Minuten-Abschnitten, Monat und Jahr aus den Tageswerten.
export const RANGES = {
    day: { days: 1, label: 'Last 24 hours', window: DAY, detail: true },
    week: { days: 7, label: 'Last 7 days', window: 7 * DAY, detail: true },
    month: { days: 30, label: 'Last 30 days', window: 30 * DAY },
    year: { days: 365, label: 'Last 365 days', window: 365 * DAY },
};

let range = RANGES[new URLSearchParams(location.search).get('range')] ? new URLSearchParams(location.search).get('range') : 'day';
let auth = { authorized: false, devicesPublic: true };
// base: die Daten des gewählten Zeitraums; zoom: [von, bis] oder null.
let base = null;
let zoom = null;
const $ = (id) => document.getElementById(id);

export function initStats() {
    document.querySelectorAll('[data-range]').forEach((x) => x.setAttribute('aria-pressed', String(x.dataset.range === range)));
    for (const b of document.querySelectorAll('[data-range]')) {
        b.addEventListener('click', () => {
            range = b.dataset.range;
            zoom = null;
            document.querySelectorAll('[data-range]').forEach((x) => x.setAttribute('aria-pressed', String(x === b)));
            loadStats();
        });
    }
    document.getElementById('zoom-reset').addEventListener('click', resetZoom);
    setInterval(() => { if (!document.hidden) loadStats(); }, 60e3);
}

function resetZoom() {
    zoom = null;
    renderCharts();
}

const zoomHandlers = {
    onSelect: (a, b) => { zoom = [a, b]; renderCharts(); },
    onReset: resetZoom,
};

// renderCharts zeichnet Nachrichten und Puffer für den Zeitraum oder den Zoom. Ein Zoom aus Monat
// oder Jahr in die letzten 31 Tage lädt die 10-Minuten-Werte nach (wie mlc-autotunnel), statt
// Tageswerte zu strecken.
async function renderCharts() {
    if (!base) return;
    let view = base;
    if (zoom) {
        const [a, b] = zoom;
        if (base.slotMs > SLOT && a >= Date.now() - 31 * DAY && b - a <= 31 * DAY) {
            try {
                const det = await getJSON(`${API}/history/detail?from=${new Date(a).toISOString()}&to=${new Date(b).toISOString()}`);
                view = { points: det.slots.map((x) => ({ ...x, t: Date.parse(x.start) })), spans: det.spans, slotMs: SLOT };
            } catch { /* bei Fehler: Tageswerte */ }
        }
        // Für die Y-Achse nur, was im Fenster liegt (plus der Abschnitt davor, der hineinreicht).
        view = { ...view, points: view.points.filter((p) => p.t + view.slotMs > a && p.t < b) };
        if (!zoom) return; // inzwischen zurückgesetzt
    }
    const from = zoom ? zoom[0] : base.from, to = zoom ? zoom[1] : base.to;
    $('zoom-info').classList.toggle('hidden', !zoom);
    $('zoom-label').textContent = zoom ? `${fmtDateTime(new Date(from).toISOString())} – ${fmtDateTime(new Date(to).toISOString())}` : '';
    messagesChart($('chart-messages'), { ...view, from, to, zoom: zoomHandlers });
    bufferChart($('chart-buffer'), { ...view, from, to, zoom: zoomHandlers });
}

export function setAuth(a) { auth = a; }

export async function loadStats() {
    const r = RANGES[range];
    const to = Date.now(), from = to - r.window;
    $('range-label').textContent = r.label;
    try {
        const hist = await getJSON(`${API}/history?days=${r.days}`);
        $('stats-locked').classList.add('hidden');
        let points, spans, slotMs;
        if (r.detail) {
            const det = await getJSON(`${API}/history/detail?from=${new Date(from).toISOString()}`);
            points = det.slots.map((s) => ({ ...s, t: Date.parse(s.start) }));
            spans = det.spans;
            slotMs = SLOT;
        } else {
            points = hist.daily.map((d) => ({ ...d, t: localMidnight(d.date) }));
            // Band: Tage mit Messung grün, darüber die Unterbrechungen (die Tageswerte kennen keine
            // ok-Intervalle; die Unterbrechungen sind sekundengenau).
            spans = [
                ...points.map((p) => ({ state: 'ok', start: new Date(p.t).toISOString(), end: new Date(Math.min(p.t + DAY, to)).toISOString() })),
                ...hist.interruptions,
            ];
            slotMs = DAY;
        }
        // Tag/Woche: Kennzahlen aus genau dem Fenster des Diagramms (letzte 24 h / 7 Tage);
        // Monat/Jahr: aus den Tageswerten (Kalendertage).
        renderKPIs(r.detail ? sumWindow(points, spans, from, to) : hist.periods.find((p) => p.days === r.days));
        base = { points, spans, from, to, slotMs };
        await renderCharts();
    } catch (err) {
        if (err instanceof HTTPError && (err.status === 401 || err.status === 403)) {
            $('stats-locked').classList.remove('hidden');
            return;
        }
        console.error(err);
    }
    loadDevices({ range: r, from, to, auth });
}

const COUNTERS = ['received', 'filtered', 'deduplicated', 'stored', 'forwarded', 'failed', 'dead_lettered', 'dropped_buffer_full'];

// sumWindow bildet dieselbe Form wie ein Period aus /history: Zähler, Sekunden je Zustand.
function sumWindow(points, spans, from, to) {
    const p = Object.fromEntries(COUNTERS.map((k) => [k, 0]));
    for (const pt of points) for (const k of COUNTERS) p[k] += pt[k] || 0;
    p.rates = { received: emptyRate(), forwarded: emptyRate() };
    for (const pt of points) for (const k of ['received', 'forwarded']) mergeRate(p.rates[k], pt.rates?.[k]);
    p.seconds = {};
    for (const s of spans) {
        const a = Math.max(from, Date.parse(s.start)), b = Math.min(to, Date.parse(s.end));
        if (b > a) p.seconds[s.state] = (p.seconds[s.state] || 0) + (b - a) / 1000;
    }
    return p;
}

const emptyRate = () => ({ min: 0, max: 0, samples: 0 });
function mergeRate(r, o) {
    if (!o || !o.samples) return;
    r.min = r.samples ? Math.min(r.min, o.min) : o.min;
    r.max = r.samples ? Math.max(r.max, o.max) : o.max;
    r.samples += o.samples;
}

function localMidnight(date) {
    const [y, m, d] = date.split('-').map(Number);
    return new Date(y, m - 1, d).getTime();
}

function kpi(label, value, sub, tone = '') {
    return el('div', { class: 'card' },
        el('div', { class: 'card-title mb-1' }, label),
        el('div', { class: `kpi-value ${tone}` }, value),
        sub ? el('div', { class: 'mt-0.5 text-xs text-slate-500 dark:text-slate-400' }, sub) : null);
}

function renderKPIs(p) {
    if (!p) return;
    const total = Object.values(p.seconds || {}).reduce((a, b) => a + b, 0);
    const ok = p.seconds?.ok || 0;
    const avail = total > 0 ? (100 * ok) / total : null;
    const lost = p.dropped_buffer_full + p.dead_lettered;
    $('kpis').replaceChildren(
        kpi('Received', fmtNum(p.received), `${fmtNum(p.deduplicated)} duplicates skipped`),
        kpi('Forwarded', fmtNum(p.forwarded), null, 'text-emerald-600 dark:text-emerald-400'),
        kpi('Filtered', fmtNum(p.filtered), 'by topic filters'),
        kpi('Send errors', fmtNum(p.failed), 'retried automatically', p.failed ? 'text-amber-600 dark:text-amber-400' : ''),
        kpi('Lost', fmtNum(lost), 'buffer full or rejected', lost ? 'text-rose-600 dark:text-rose-400' : ''),
        kpi('Forwarding', avail == null ? '–' : `${avail.toFixed(avail >= 99.95 || avail === 0 ? 0 : 1)} %`,
            total ? `of ${fmtDuration(total)} running` : 'no data yet',
            avail == null ? '' : avail >= 99 ? 'text-emerald-600 dark:text-emerald-400' : 'text-amber-600 dark:text-amber-400'),
    );
    renderRates(p, total);
}

// renderRates: Nachrichten je Sekunde — Durchschnitt aus Zähler / Laufzeit, Min/Max aus den
// 15-s-Messungen. Min/Max gibt es erst seit dem Einbau; deckt die Messung den Zeitraum nicht ab,
// steht dabei, wie viel davon.
const fmtRate = (v) => (v === 0 ? '0' : v < 10 ? v.toFixed(2) : v.toFixed(1));
function renderRates(p, running) {
    const rows = [['Received', 'received', ''], ['Forwarded', 'forwarded', 'text-emerald-600 dark:text-emerald-400']];
    const cell = (v, extra = '') => el('td', { class: `py-1 pl-6 text-right text-lg font-semibold tabular-nums ${extra}` }, v);
    const measured = Math.max(...rows.map(([, k]) => p.rates?.[k]?.samples || 0)) * 15;
    const note = !running ? 'No running time in this period yet.'
        : !measured ? 'Min/max: not measured in this period yet (recorded since version 1.5).'
            : measured < 0.9 * running ? `Min/max over 15-s windows, measured for ${fmtDuration(measured)} of ${fmtDuration(running)} running.`
                : 'Average over the running time; min/max over 15-s windows.';
    $('rates').replaceChildren(el('div', { class: 'card' },
        el('h2', { class: 'card-title' }, 'Messages per second'),
        el('div', { class: 'overflow-x-auto' }, el('table', { class: 'text-sm' },
            el('thead', {}, el('tr', { class: 'table-head' }, ['', 'Average', 'Min', 'Max'].map((h, i) =>
                el('th', { class: `py-1 ${i ? 'pl-6 text-right' : ''}` }, h)))),
            el('tbody', {}, rows.map(([label, k, tone]) => {
                const r = p.rates?.[k];
                const has = r && r.samples > 0;
                return el('tr', { class: 'table-row' },
                    el('td', { class: 'py-1 font-medium' }, label),
                    cell(running ? fmtRate((p[k] || 0) / running) : '–', tone),
                    cell(has ? fmtRate(r.min) : '–'),
                    cell(has ? fmtRate(r.max) : '–'));
            })))),
        el('p', { class: 'mt-2 text-xs text-slate-500 dark:text-slate-400' }, note)));
}
