// API-Zugriff und Formatierung. Jede Anfrage trägt X-Proxy-UI: Dann antwortet der Server bei
// fehlender Anmeldung ohne WWW-Authenticate, und der Browser öffnet keinen eigenen Dialog.
export const API = '/api/v1';

const headers = { 'X-Proxy-UI': '1' };

export class HTTPError extends Error {
    constructor(status, message) {
        super(message);
        this.status = status;
    }
}

export async function getJSON(path) {
    const res = await fetch(path, { headers, credentials: 'same-origin' });
    if (!res.ok) throw new HTTPError(res.status, `${path}: HTTP ${res.status}`);
    return res.json();
}

export async function postJSON(path, body) {
    const res = await fetch(path, {
        method: 'POST',
        headers: { ...headers, 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify(body ?? {}),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new HTTPError(res.status, data.error || `HTTP ${res.status}`);
    return data;
}

const num = new Intl.NumberFormat('en-US');
export const fmtNum = (n) => num.format(n ?? 0);

// fmtDuration: 3725 → „1 h 2 min“; unter einer Minute in Sekunden.
export function fmtDuration(seconds) {
    if (seconds == null || seconds < 0) return '–';
    const s = Math.round(seconds);
    if (s < 60) return `${s} s`;
    const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
    if (d > 0) return `${d} d ${h} h`;
    if (h > 0) return `${h} h ${m} min`;
    return `${m} min`;
}

// fmtAge: Alter in Sekunden → „12 s ago“; -1 heißt: seit dem Start noch nie.
export const fmtAge = (s) => (s == null || s < 0 ? 'never' : `${fmtDuration(s)} ago`);

const dateTime = new Intl.DateTimeFormat('en-GB', { dateStyle: 'medium', timeStyle: 'short' });
export const fmtDateTime = (iso) => (iso ? dateTime.format(new Date(iso)) : '–');

// el baut ein Element; Kinder sind Elemente oder Text (immer als Text, nie als HTML — Namen und
// Topics kommen von Geräten und dürfen nichts ausführen).
export function el(tag, attrs = {}, ...children) {
    const e = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) {
        if (v == null || v === false) continue;
        if (k === 'class') e.className = v;
        else if (k.startsWith('on')) e.addEventListener(k.slice(2), v);
        else e.setAttribute(k, v === true ? '' : v);
    }
    for (const c of children.flat()) {
        if (c == null || c === false) continue;
        e.append(c instanceof Node ? c : String(c));
    }
    return e;
}

// Zustände der Weiterleitung (wie pkg/history) mit Bezeichnung und Farben.
export const STATES = {
    ok: { label: 'Forwarding', pill: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300', band: '#22c55e55' },
    upstream_down: { label: 'Upstream unreachable', pill: 'bg-rose-100 text-rose-800 dark:bg-rose-900/40 dark:text-rose-300', band: '#ef4444cc' },
    paused: { label: 'Paused (remote)', pill: 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300', band: '#f59e0bcc' },
    buffer_full: { label: 'Buffer full — dropping', pill: 'bg-fuchsia-100 text-fuchsia-800 dark:bg-fuchsia-900/40 dark:text-fuchsia-300', band: '#a855f7cc' },
};
