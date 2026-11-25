// Tab „Interruptions“: Zeiten ohne Weiterleitung — Zusammenfassung je Grund und Liste.
import { API, getJSON, fmtNum, fmtDuration, fmtDateTime, el, STATES, HTTPError } from './api.js';

const LIMIT = 200; // so viele liefert /history höchstens
let days = 30;
const $ = (id) => document.getElementById(id);

export function initInterruptions() {
    for (const b of document.querySelectorAll('[data-int-days]')) {
        b.addEventListener('click', () => {
            days = Number(b.dataset.intDays);
            document.querySelectorAll('[data-int-days]').forEach((x) => x.setAttribute('aria-pressed', String(x === b)));
            loadInterruptions();
        });
    }
}

const secs = (s) => (Date.parse(s.end) - Date.parse(s.start)) / 1000;

export async function loadInterruptions() {
    let list;
    try {
        list = (await getJSON(`${API}/history?days=${days}`)).interruptions;
    } catch (err) {
        const locked = err instanceof HTTPError && (err.status === 401 || err.status === 403);
        $('int-summary').replaceChildren();
        $('int-list').replaceChildren(el('p', { class: 'text-slate-500 dark:text-slate-400' },
            locked ? 'Sign in required.' : 'History is not available.'));
        return;
    }
    renderSummary(list);
    renderList(list);
}

function tile(label, value, sub, tone = '') {
    return el('div', { class: 'card' },
        el('div', { class: 'card-title mb-1' }, label),
        el('div', { class: `kpi-value ${tone}` }, value),
        sub ? el('div', { class: 'mt-0.5 text-xs text-slate-500 dark:text-slate-400' }, sub) : null);
}

function renderSummary(list) {
    const total = list.reduce((a, s) => a + secs(s), 0);
    const longest = list.reduce((m, s) => (!m || secs(s) > secs(m) ? s : m), null);
    const byState = {};
    for (const s of list) {
        const b = (byState[s.state] ??= { n: 0, secs: 0 });
        b.n++;
        b.secs += secs(s);
    }
    const tiles = [
        tile('Interruptions', fmtNum(list.length), list.length >= LIMIT ? `latest ${LIMIT} shown` : `last ${days} days`,
            list.length ? 'text-amber-600 dark:text-amber-400' : 'text-emerald-600 dark:text-emerald-400'),
        tile('Total duration', list.length ? fmtDuration(total) : '–', 'without forwarding'),
        tile('Longest', longest ? fmtDuration(secs(longest)) : '–', longest ? fmtDateTime(longest.start) : null),
    ];
    for (const [state, b] of Object.entries(byState)) {
        const st = STATES[state] || { label: state };
        tiles.push(tile(st.label, fmtDuration(b.secs), `${fmtNum(b.n)} ×`));
    }
    $('int-summary').replaceChildren(...tiles);
}

function renderList(list) {
    const box = $('int-list');
    if (list.length === 0) {
        box.replaceChildren(el('p', { class: 'text-slate-500 dark:text-slate-400' },
            'None in this period — forwarding ran whenever the proxy was running.'));
        return;
    }
    box.replaceChildren(el('div', { class: 'overflow-x-auto' }, el('table', { class: 'w-full min-w-[32rem]' },
        el('thead', {}, el('tr', { class: 'table-head' }, ['From', 'To', 'Duration', 'Reason'].map((h) => el('th', { class: 'py-1.5 pr-4' }, h)))),
        el('tbody', {}, list.map((s) => {
            const st = STATES[s.state] || { label: s.state, pill: '' };
            return el('tr', { class: 'table-row' },
                el('td', { class: 'py-1.5 pr-4 tabular-nums' }, fmtDateTime(s.start)),
                el('td', { class: 'py-1.5 pr-4 tabular-nums' }, s.open ? 'ongoing' : fmtDateTime(s.end)),
                el('td', { class: 'py-1.5 pr-4 tabular-nums' }, fmtDuration(secs(s))),
                el('td', { class: 'py-1.5' }, el('span', { class: `pill ${st.pill}` }, st.label)));
        })))));
}
