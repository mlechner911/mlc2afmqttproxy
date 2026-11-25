// Einstieg: Kopfzeile (Kurzstatus), Tabs, Anmeldung. Statistik ist Standard; die Live-Ansicht
// braucht eine Anmeldung (oder läuft ohne Passwort nur am Gerät selbst).
import { API, getJSON, postJSON, fmtNum, fmtAge, fmtDuration, STATES } from './api.js';
import { initStats, loadStats, setAuth } from './stats.js';
import { initLive, startLive } from './live.js';
import { redrawAll } from './charts.js';

const $ = (id) => document.getElementById(id);
let status = null;

async function refreshStatus() {
    try {
        status = await getJSON(`${API}/status`);
    } catch {
        $('state-text').textContent = 'Proxy not reachable';
        $('state-pill').className = `pill ${STATES.upstream_down.pill}`;
        return;
    }
    setAuth({ authorized: status.authorized, devicesPublic: status.devices_public });
    $('live-lock').classList.toggle('hidden', status.authorized);
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

function selectTab(name) {
    for (const t of ['stats', 'live']) {
        const on = t === name;
        $(`tab-${t}`).setAttribute('aria-selected', String(on));
        $(`panel-${t}`).classList.toggle('hidden', !on);
    }
    history.replaceState(null, '', name === 'live' ? '#live' : '#');
    if (name === 'stats') redrawAll();
    else showLive();
}

function showLive() {
    const authorized = status?.authorized;
    $('login').classList.toggle('hidden', authorized || !status?.login_configured);
    $('live-local-only').classList.toggle('hidden', authorized || status?.login_configured !== false);
    $('live').classList.toggle('hidden', !authorized);
    $('live').classList.toggle('flex', !!authorized);
    if (authorized) startLive();
    else if (status?.login_configured) $('login-password').focus();
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
        showLive();
        loadStats(); // Filter und ggf. Geräte sind jetzt sichtbar
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
    initLive();
    initStats();
    $('tab-stats').addEventListener('click', () => selectTab('stats'));
    $('tab-live').addEventListener('click', () => selectTab('live'));
    $('login-form').addEventListener('submit', login);
    $('logout-btn').addEventListener('click', logout);
    await refreshStatus();
    setInterval(() => { if (!document.hidden) refreshStatus(); }, 10e3);
    selectTab(location.hash === '#live' ? 'live' : 'stats');
    loadStats();
}

boot();
