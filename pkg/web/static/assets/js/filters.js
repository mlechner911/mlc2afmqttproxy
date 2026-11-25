// Tab „Filters“ (angemeldet): Topic-Regeln mit Treffern seit dem Start, Deduplizierung, Routen.
import { API, getJSON, fmtNum, fmtDuration, el } from './api.js';

const RULE = {
    allow: 'forwarded (allowed prefix)',
    not_allowed: 'dropped — matches no allowed prefix',
    ignore: 'dropped (ignored prefix)',
};

export async function loadFilters(uptimeS) {
    const box = document.getElementById('filters');
    let f, stats;
    try {
        [f, stats] = await Promise.all([getJSON(`${API}/filters`), getJSON(`${API}/stats`)]);
    } catch (err) {
        box.replaceChildren(el('p', { class: 'text-rose-600' }, String(err.message || err)));
        return;
    }
    const received = stats.messages_received_total || 0;
    const share = (n) => (received ? `${((100 * n) / received).toFixed(n && (100 * n) / received < 1 ? 1 : 0)} %` : '–');
    const rule = (r) => el('tr', { class: 'table-row' },
        el('td', { class: 'py-1.5 pr-4 font-mono text-xs' }, r.prefix ?? '(anything else)'),
        el('td', { class: 'py-1.5 pr-4' }, RULE[r.kind] || r.kind),
        el('td', { class: 'py-1.5 pr-4 text-right tabular-nums' }, r.kind === 'allow' ? '' : fmtNum(r.hits)),
        el('td', { class: 'py-1.5 text-right tabular-nums' }, r.kind === 'allow' ? '' : share(r.hits)));
    box.replaceChildren(
        el('p', { class: 'mb-3 text-sm text-slate-500 dark:text-slate-400' },
            `Since the proxy started${uptimeS ? ` ${fmtDuration(uptimeS)} ago` : ''}: ${fmtNum(received)} received, `
            + `${fmtNum(stats.messages_filtered_total)} filtered, ${fmtNum(f.deduplicated_total)} duplicates skipped. Target: ${f.target}`),
        el('div', { class: 'card mb-4' },
            el('h2', { class: 'card-title' }, 'Topic rules'),
            f.rules.length
                ? el('div', { class: 'overflow-x-auto' }, el('table', { class: 'w-full min-w-[32rem] text-sm' },
                    el('thead', {}, el('tr', { class: 'table-head' }, ['Topic prefix', 'Rule', 'Hits', 'Share'].map((h, i) =>
                        el('th', { class: `py-1.5 ${i >= 2 ? 'text-right' : ''} ${i < 3 ? 'pr-4' : ''}` }, h)))),
                    el('tbody', {}, f.rules.map(rule))))
                : el('p', { class: 'text-sm' }, 'No topic filters — every topic is forwarded.')),
        el('div', { class: 'card mb-4 text-sm' },
            el('h2', { class: 'card-title' }, 'Deduplication'),
            f.dedup_interval_ms > 0
                ? el('p', {}, `Identical messages on the same topic within ${f.dedup_interval_ms} ms are skipped`
                    + `${f.dedup_ignore_keys.length ? `, ignoring ${f.dedup_ignore_keys.join(', ')}` : ''}.`)
                : el('p', {}, 'Off.')),
        el('div', { class: 'card text-sm' },
            el('h2', { class: 'card-title' }, 'Additional routes'),
            f.routes.length
                ? el('ul', { class: 'space-y-1' }, f.routes.map((r) => el('li', {},
                    el('b', {}, r.name), ` → ${r.upstream}`, r.devices?.length ? ` · ${r.devices.length} devices` : ' · all devices')))
                : el('p', {}, 'None — everything goes to the main target only.')),
    );
}
