// Einstieg: Kopfzeile (Kurzstatus), Tabs, Anmeldung. Statistik ist Standard. Filter und Live sind
// geschützt: ohne Anmeldung zeigt der Tab das Anmeldeformular (oder, ohne Passwort, den Hinweis
// „nur am Gerät“).
import { API, getJSON, postJSON, fmtNum, fmtAge, fmtDuration, STATES } from './api.js';
import { initStats, loadStats, setAuth } from './stats.js';
import { initInterruptions, loadInterruptions } from './interruptions.js';
import { loadFilters } from './filters.js';
import { initLive, startLive, showLive, stopLive } from './live.js';
import { redrawAll } from './charts.js';

const TABS = ['stats', 'interruptions', 'filters', 'live'];
const PROTECTED = new Set(['filters', 'live']);
const $ = (id) => document.getElementById(id);
let status = null;
let current = 'stats';

async function refreshStatus() {
    const wasAuthorized = status?.authorized;
    try {
        status = await getJSON(`${API}/status`);
    } catch {
        $('state-text').textContent = 'Proxy not reachable';
        $('state-pill').className = `pill ${STATES.upstream_down.pill}`;
        return;
    }
    setAuth({ authorized: status.authorized, devicesPublic: status.devices_public });
    // Sitzung verloren (Neustart des Proxys, abgelaufen): geschützte Tabs zeigen wieder das Formular.
    if (wasAuthorized && !status.authorized) loggedOut();
    document.querySelectorAll('.lock').forEach((l) => l.classList.toggle('hidden', status.authorized));
    $('logout-btn').classList.toggle('hidden', !(status.authorized && status.login_configured));
    if (!status.state) { // Statistik nicht öffentlich und nicht angemeldet
        $('state-text').textContent = 'Sign in required';
        return;
    }
    const st = STATES[status.state] || STATES.ok;
    $('state-pill').className = `pill ${st.pill}`;
    $('state-text').textContent = st.label;
    $('hdr-buffer').textContent = fmtNum(status.buffer_pending);
    $('hdr-forward').textContent = fmtAge(status.last_forward_age_s);
    $('hdr-zigbee').textContent = status.zigbee_connected ? 'connected' : 'not connected';
    $('hdr-zigbee').className = `font-semibold ${status.zigbee_connected ? '' : 'text-rose-600 dark:text-rose-400'}`;
    $('hdr-uptime').textContent = fmtDuration(status.uptime_s);
    $('hdr-version').textContent = status.version;
}

function loggedOut() {
    stopLive();
    if (status) status.authorized = false;
    selectTab(current);
}

function selectTab(name) {
    current = name;
    const locked = PROTECTED.has(name) && !status?.authorized;
    for (const t of TABS) {
        $(`tab-${t}`).setAttribute('aria-selected', String(t === name));
        $(`panel-${t}`).classList.toggle('hidden', t !== name || locked);
    }
    $('panel-auth').classList.toggle('hidden', !locked);
    history.replaceState(null, '', name === 'stats' ? location.pathname + location.search : `#${name}`);
    if (locked) {
        const canLogin = !!status?.login_configured;
        $('login').classList.toggle('hidden', !canLogin);
        $('local-only').classList.toggle('hidden', canLogin);
        if (canLogin) $('login-password').focus();
        return;
    }
    if (name === 'stats') redrawAll();
    else if (name === 'interruptions') loadInterruptions();
    else if (name === 'filters') loadFilters(status?.uptime_s);
    else if (name === 'live') { startLive(); showLive(); }
}

async function login(ev) {
    ev.preventDefault();
    const btn = ev.submitter, err = $('login-error');
    btn.disabled = true;
    err.classList.add('hidden');
    try {
        await postJSON(`${API}/login`, { password: $('login-password').value });
        $('login-password').value = '';
        await refreshStatus();
        selectTab(current);
        loadStats(); // Geräte sind ggf. erst jetzt sichtbar
    } catch (e) {
        err.textContent = e.message;
        err.classList.remove('hidden');
    } finally {
        btn.disabled = false;
    }
}

async function logout() {
    await postJSON(`${API}/logout`).catch(() => {});
    location.hash = '';
    location.reload(); // trennt auch die Live-Verbindung
}

async function boot() {
    initLive({ onLoggedOut: async () => { await refreshStatus(); loggedOut(); } });
    initStats();
    initInterruptions();
    for (const t of TABS) $(`tab-${t}`).addEventListener('click', () => selectTab(t));
    $('login-form').addEventListener('submit', login);
    $('logout-btn').addEventListener('click', logout);
    await refreshStatus();
    setInterval(() => { if (!document.hidden) refreshStatus(); }, 10e3);
    const fromHash = location.hash.slice(1);
    selectTab(TABS.includes(fromHash) ? fromHash : 'stats');
    loadStats();
}

boot();
