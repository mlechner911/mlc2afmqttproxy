// Gerätetabelle: Status, Nachrichten, mittlerer Abstand, zuletzt gesehen; Klick öffnet den Verlauf.
import { API, getJSON, fmtNum, fmtDuration, el, HTTPError } from './api.js';
import { deviceChart } from './charts.js';

const STATUS = {
    ok: ['OK', 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300'],
    silent: ['Silent', 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300'],
    offline: ['Offline', 'bg-rose-100 text-rose-800 dark:bg-rose-900/40 dark:text-rose-300'],
    unknown: ['Not seen', 'bg-slate-200 text-slate-600 dark:bg-slate-800 dark:text-slate-400'],
};
// Probleme zuerst, dann nach Name.
const ORDER = { offline: 0, silent: 1, unknown: 2, ok: 3 };

let open = null; // Name des aufgeklappten Geräts
let ctx = null;

export async function loadDevices(c) {
    ctx = c;
    const box = document.getElementById('devices');
    const summary = document.getElementById('device-summary');
    let list;
    try {
        list = (await getJSON(`${API}/devices`)).devices;
    } catch (err) {
        const locked = err instanceof HTTPError && (err.status === 401 || err.status === 403);
        box.replaceChildren(el('p', { class: 'text-slate-500 dark:text-slate-400' },
            locked ? 'Device statistics require sign-in (Live tab).' : 'Device statistics are not available.'));
        summary.textContent = '';
        return;
    }
    if (list.length === 0) {
        box.replaceChildren(el('p', { class: 'text-slate-500 dark:text-slate-400' }, 'No Zigbee devices seen yet. They appear once zigbee2mqtt publishes bridge/devices or a device reports.'));
        summary.textContent = '';
        return;
    }
    const count = (s) => list.filter((d) => d.status === s).length;
    summary.textContent = `${list.length} devices · ${count('ok')} OK · ${count('silent')} silent · ${count('offline')} offline`;
    list.sort((a, b) => ORDER[a.status] - ORDER[b.status] || a.name.localeCompare(b.name));
    const periodKey = ctx.range.days === 1 ? 'today' : ctx.range.days === 7 ? 'week' : 'month';
    const periodLabel = { today: 'Today', week: '7 days', month: '30 days' }[periodKey];
    box.replaceChildren(el('table', { class: 'w-full min-w-[48rem]' },
        el('thead', {}, el('tr', { class: 'table-head' },
            ['Device', 'Model', 'Status', periodLabel, 'Every', 'Last seen', 'Battery', 'Link'].map((h, i) =>
                el('th', { class: `py-1.5 pr-4 ${i >= 3 ? 'text-right' : ''}` }, h)))),
        el('tbody', {}, list.flatMap((d) => row(d, periodKey)))));
}

function row(d, periodKey) {
    const [label, cls] = STATUS[d.status] || STATUS.unknown;
    const age = d.last_seen ? (Date.now() - Date.parse(d.last_seen)) / 1000 : null;
    const tr = el('tr', { class: 'table-row cursor-pointer hover:bg-slate-50 dark:hover:bg-slate-800/40', title: d.ieee || '' },
        el('td', { class: 'py-1.5 pr-4 font-medium' }, d.name),
        el('td', { class: 'py-1.5 pr-4 text-slate-500 dark:text-slate-400' }, [d.vendor, d.model].filter(Boolean).join(' ') || '–'),
        el('td', { class: 'py-1.5 pr-4' }, el('span', { class: `pill ${cls}` }, label)),
        el('td', { class: 'py-1.5 pr-4 text-right tabular-nums' }, fmtNum(d.messages[periodKey])),
        el('td', { class: 'py-1.5 pr-4 text-right tabular-nums' }, d.avg_interval_s ? fmtDuration(d.avg_interval_s) : '–'),
        el('td', { class: 'py-1.5 pr-4 text-right tabular-nums' }, age == null ? '–' : `${fmtDuration(age)} ago`),
        el('td', { class: 'py-1.5 pr-4 text-right tabular-nums' }, d.battery != null ? `${d.battery} %` : '–'),
        el('td', { class: 'py-1.5 text-right tabular-nums' }, d.linkquality ?? '–'));
    const detail = el('tr', { class: open === d.name ? '' : 'hidden' }, el('td', { colspan: 8, class: 'pb-3' }));
    tr.addEventListener('click', () => {
        open = open === d.name ? null : d.name;
        document.querySelectorAll('#devices tbody tr.detail-open').forEach((x) => x.classList.add('hidden'));
        if (open) { detail.classList.remove('hidden'); detail.classList.add('detail-open'); showHistory(d.name, detail.firstChild); }
    });
    if (open === d.name) { detail.classList.add('detail-open'); queueMicrotask(() => showHistory(d.name, detail.firstChild)); }
    return [tr, detail];
}

// showHistory: Tag und Woche stundenweise, Monat und Jahr tageweise.
async function showHistory(name, cell) {
    const { range, from, to } = ctx;
    const q = new URLSearchParams({ name });
    const hourly = range.days <= 7;
    if (hourly) q.set('from', new Date(from).toISOString()); else q.set('days', range.days);
    cell.replaceChildren(el('p', { class: 'text-xs text-slate-400' }, 'Loading…'));
    try {
        const h = await getJSON(`${API}/devices/history?${q}`);
        const box = el('div', { class: 'rounded-lg bg-slate-50 p-2 dark:bg-slate-950/50' });
        cell.replaceChildren(el('p', { class: 'mb-1 text-xs text-slate-500 dark:text-slate-400' },
            `Messages per ${hourly ? 'hour' : 'day'} — ${name}`), box);
        deviceChart(box, { points: h.points, from, to, slotMs: hourly ? 3600e3 : 86400e3 });
    } catch (err) {
        cell.replaceChildren(el('p', { class: 'text-xs text-rose-600' }, String(err.message || err)));
    }
}
