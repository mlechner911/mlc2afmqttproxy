// Statistik-Tab: Zeitraum, Kennzahlen, Diagramme, Unterbrechungen, Geräte, Filter.
import { API, getJSON, fmtNum, fmtDuration, fmtDateTime, el, STATES, HTTPError } from './api.js';
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
const $ = (id) => document.getElementById(id);

export function initStats() {
    document.querySelectorAll('[data-range]').forEach((x) => x.setAttribute('aria-pressed', String(x.dataset.range === range)));
    for (const b of document.querySelectorAll('[data-range]')) {
        b.addEventListener('click', () => {
            range = b.dataset.range;
            document.querySelectorAll('[data-range]').forEach((x) => x.setAttribute('aria-pressed', String(x === b)));
            loadStats();
        });
    }
    setInterval(() => { if (!document.hidden) loadStats(); }, 60e3);
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
        messagesChart($('chart-messages'), { points, spans, from, to, slotMs });
        bufferChart($('chart-buffer'), { points, from, to, slotMs });
        renderInterruptions(hist.interruptions.filter((s) => Date.parse(s.end) >= from));
    } catch (err) {
        if (err instanceof HTTPError && (err.status === 401 || err.status === 403)) {
            $('stats-locked').classList.remove('hidden');
            return;
        }
        console.error(err);
    }
    loadDevices({ range: r, from, to, auth });
    loadFilters();
}

const COUNTERS = ['received', 'filtered', 'deduplicated', 'stored', 'forwarded', 'failed', 'dead_lettered', 'dropped_buffer_full'];

// sumWindow bildet dieselbe Form wie ein Period aus /history: Zähler, Sekunden je Zustand.
function sumWindow(points, spans, from, to) {
    const p = Object.fromEntries(COUNTERS.map((k) => [k, 0]));
    for (const pt of points) for (const k of COUNTERS) p[k] += pt[k] || 0;
    p.seconds = {};
    for (const s of spans) {
        const a = Math.max(from, Date.parse(s.start)), b = Math.min(to, Date.parse(s.end));
        if (b > a) p.seconds[s.state] = (p.seconds[s.state] || 0) + (b - a) / 1000;
    }
    return p;
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
}

function renderInterruptions(list) {
    const box = $('interruptions');
    if (list.length === 0) {
        box.replaceChildren(el('p', { class: 'text-slate-500 dark:text-slate-400' }, 'None in this period — forwarding ran whenever the proxy was running.'));
        return;
    }
    box.replaceChildren(el('div', { class: 'overflow-x-auto' }, el('table', { class: 'w-full min-w-[32rem]' },
        el('thead', {}, el('tr', { class: 'table-head' }, ['From', 'To', 'Duration', 'Reason'].map((h) => el('th', { class: 'py-1.5 pr-4' }, h)))),
        el('tbody', {}, list.map((s) => {
            const st = STATES[s.state] || { label: s.state, pill: '' };
            const secs = (Date.parse(s.end) - Date.parse(s.start)) / 1000;
            return el('tr', { class: 'table-row' },
                el('td', { class: 'py-1.5 pr-4 tabular-nums' }, fmtDateTime(s.start)),
                el('td', { class: 'py-1.5 pr-4 tabular-nums' }, s.open ? 'ongoing' : fmtDateTime(s.end)),
                el('td', { class: 'py-1.5 pr-4 tabular-nums' }, fmtDuration(secs)),
                el('td', { class: 'py-1.5' }, el('span', { class: `pill ${st.pill}` }, st.label)));
        })))));
}

async function loadFilters() {
    const card = $('filters-card');
    if (!auth.authorized) { card.classList.add('hidden'); return; }
    let f;
    try { f = await getJSON(`${API}/filters`); } catch { card.classList.add('hidden'); return; }
    card.classList.remove('hidden');
    const rule = (r) => {
        const what = { allow: 'forward only', not_allowed: 'dropped: matches no allowed prefix', ignore: 'dropped' }[r.kind] || r.kind;
        return el('tr', { class: 'table-row' },
            el('td', { class: 'py-1.5 pr-4 font-mono text-xs' }, r.prefix ?? '—'),
            el('td', { class: 'py-1.5 pr-4' }, what),
            el('td', { class: 'py-1.5 text-right tabular-nums' }, r.kind === 'allow' ? '' : fmtNum(r.hits)));
    };
    $('filters').replaceChildren(
        el('p', { class: 'mb-2 text-xs text-slate-500 dark:text-slate-400' }, `Target: ${f.target} · hits since the proxy started`),
        f.rules.length
            ? el('table', { class: 'mb-3 w-full' },
                el('thead', {}, el('tr', { class: 'table-head' }, ['Topic prefix', 'Rule', 'Hits'].map((h, i) => el('th', { class: `py-1.5 ${i === 2 ? 'text-right' : 'pr-4'}` }, h)))),
                el('tbody', {}, f.rules.map(rule)))
            : el('p', { class: 'mb-3' }, 'No topic filters — every topic is forwarded.'),
        el('p', {}, f.dedup_interval_ms > 0
            ? `Duplicates within ${f.dedup_interval_ms} ms are skipped${f.dedup_ignore_keys.length ? ` (ignoring ${f.dedup_ignore_keys.join(', ')})` : ''}: ${fmtNum(f.deduplicated_total)} since start.`
            : 'Deduplication is off.'),
        ...(f.routes.length ? [el('p', { class: 'mt-2' }, `Additional routes: ${f.routes.map((r) => `${r.name} → ${r.upstream}${r.devices?.length ? ` (${r.devices.length} devices)` : ''}`).join(' · ')}`)] : []),
    );
}
