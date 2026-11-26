// Tab „Filters“ (angemeldet): Topic-Regeln mit Treffern seit dem Start, Deduplizierung, Routen.
import { API, getJSON, fmtNum, fmtDuration, el } from './api.js';

const RULE = {
    allow: 'forwarded (allowed prefix)',
    not_allowed: 'dropped — matches no allowed prefix',
    ignore: 'dropped (ignored prefix)',
};

export async function loadFilters(uptimeS) {
    const box = document.getElementById('filters');
    let f, stats, ctl;
    try {
        [f, stats, ctl] = await Promise.all([getJSON(`${API}/filters`), getJSON(`${API}/stats`),
            getJSON(`${API}/control`).catch(() => null)]);
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
        controlCard(ctl),
        el('div', { class: 'card text-sm' },
            el('h2', { class: 'card-title' }, 'Additional routes'),
            f.routes.length
                ? el('ul', { class: 'space-y-1' }, f.routes.map((r) => el('li', {},
                    el('b', {}, r.name), ` → ${r.upstream}`, r.devices?.length ? ` · ${r.devices.length} devices` : ' · all devices')))
                : el('p', {}, 'None — everything goes to the main target only.')),
    );
}

// controlCard: Rückweg — welche Geräte der Monitor schalten darf, Ergebnisse, letzte Befehle.
const RESULT = {
    ok: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300',
    rejected: 'bg-rose-100 text-rose-800 dark:bg-rose-900/40 dark:text-rose-300',
    timeout: 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300',
};
const timeFmt = new Intl.DateTimeFormat('en-GB', { dateStyle: 'short', timeStyle: 'medium' });

function controlCard(ctl) {
    const card = el('div', { class: 'card mb-4 text-sm' }, el('h2', { class: 'card-title' }, 'Remote control (commands from the Sensor Monitor)'));
    if (!ctl || !ctl.enabled) {
        card.append(el('p', {}, 'Off — no devices released in control: (config.yaml). The proxy never switches anything by itself.'));
        return card;
    }
    card.append(el('p', { class: 'mb-2 text-slate-500 dark:text-slate-400' },
        `Commands go to zigbee2mqtt via ${ctl.direct ? 'the built-in broker' : 'control.broker'}. `
        + `Since start: ${['ok', 'rejected', 'timeout'].map((k) => `${fmtNum(ctl.counts[k] || 0)} ${k}`).join(' · ')}`));
    card.append(el('ul', { class: 'mb-3 space-y-1' }, ctl.tenants.map((t) => el('li', {},
        el('b', {}, t.name), ` (customer ${t.customer}): `,
        Object.entries(t.allowed).map(([ieee, f]) => `${ieee} → ${f.join(', ')}`).join(' · ')))));
    if (!ctl.recent.length) {
        card.append(el('p', { class: 'text-slate-500 dark:text-slate-400' }, 'No commands yet.'));
        return card;
    }
    card.append(el('div', { class: 'overflow-x-auto' }, el('table', { class: 'w-full min-w-[40rem]' },
        el('thead', {}, el('tr', { class: 'table-head' }, ['Time', 'Route', 'Device', 'Set', 'Result'].map((h) => el('th', { class: 'py-1.5 pr-4' }, h)))),
        el('tbody', {}, ctl.recent.slice(0, 50).map((e) => el('tr', { class: 'table-row' },
            el('td', { class: 'py-1.5 pr-4 tabular-nums' }, timeFmt.format(new Date(e.time))),
            el('td', { class: 'py-1.5 pr-4' }, e.tenant),
            el('td', { class: 'py-1.5 pr-4 font-mono text-xs' }, e.device || e.ieee),
            el('td', { class: 'py-1.5 pr-4 font-mono text-xs' }, e.fields ? JSON.stringify(e.fields) : '–'),
            el('td', { class: 'py-1.5' }, el('span', { class: `pill ${RESULT[e.result] || ''}` }, e.result), e.reason ? ` ${e.reason}` : '')))))));
    return card;
}
