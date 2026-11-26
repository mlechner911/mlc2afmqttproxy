// Live-Ansicht: MQTT über WebSocket (/mqtt, nur angemeldet) und Baum der zuletzt gesehenen Werte.
// Übernommen aus dem Dashboard bis 1.1 mit den Korrekturen vom 25.11.2026: ein Frame kann mehrere
// oder halbe MQTT-Pakete enthalten (gepuffert und Paket für Paket zerlegt), Neuaufbau höchstens
// alle 500 ms, Arrays als aufklappbare Knoten.
const SVG_NS = 'http://www.w3.org/2000/svg';
const treeData = {};
const openPaths = new Set(['root']);
let socket = null;
let rxBuffer = new Uint8Array(0);
let pingTimer = null;
let renderPending = false;
let dirty = false; // Daten geändert, während der Tab unsichtbar war
let container, badge;

const BADGE = {
    on: ['Connected', 'pill bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300'],
    off: ['Disconnected', 'pill bg-rose-100 text-rose-800 dark:bg-rose-900/40 dark:text-rose-300'],
};

// onLoggedOut: von app.js — die Sitzung ist weg (z. B. nach einem Neustart des Proxys, Sitzungen
// liegen nur im Speicher); dann zeigt die Seite das Anmeldeformular statt weiter zu verbinden.
let onLoggedOut = () => {};
let retryMs = 5000;
let reconnectTimer = null;

export function initLive(opts = {}) {
    if (opts.onLoggedOut) onLoggedOut = opts.onLoggedOut;
    container = document.getElementById('tree-container');
    badge = document.getElementById('conn-badge');
    document.getElementById('clear-tree-btn').addEventListener('click', () => {
        for (const k in treeData) delete treeData[k];
        showWaiting();
    });
}

// showLive zeichnet nach, was sich ändert, während der Tab nicht sichtbar war.
export function showLive() {
    if (dirty) requestAnimationFrame(renderTree);
}

// startLive verbindet einmal; danach hält sich die Verbindung selbst (Wiederverbinden nach 5 s).
export function startLive() {
    if (socket) return;
    clearTimeout(reconnectTimer);
    retryMs = 5000;
    connect();
}

// stopLive trennt und verbindet nicht wieder (abgemeldet).
export function stopLive() {
    clearTimeout(reconnectTimer);
    if (socket) {
        socket.onclose = null;
        socket.close();
        socket = null;
    }
    clearInterval(pingTimer);
    setBadge(BADGE.off);
}

// Nach einem Abbruch: Ist die Sitzung weg, nicht mehr verbinden, sondern anmelden lassen. Bis
// 26.11.2026 versuchte die Seite es endlos alle 5 s (jeder Versuch 401) und zeigte nie das
// Formular — die Live-Ansicht stand still, bis man die Seite neu lud.
async function afterClose() {
    socket = null;
    let authorized = true;
    try {
        const res = await fetch('/api/v1/status', { headers: { 'X-Proxy-UI': '1' }, credentials: 'same-origin' });
        if (res.ok) authorized = (await res.json()).authorized;
    } catch { /* Proxy nicht erreichbar: später erneut */ }
    if (!authorized) {
        onLoggedOut();
        return;
    }
    reconnectTimer = setTimeout(connect, retryMs);
    retryMs = Math.min(retryMs * 2, 60000);
}

function setBadge([text, cls]) {
    badge.textContent = text;
    badge.className = cls;
}

function connect() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    socket = new WebSocket(`${proto}//${location.host}/mqtt`, ['mqtt']);
    socket.binaryType = 'arraybuffer';
    rxBuffer = new Uint8Array(0);
    socket.onopen = () => {
        setBadge(BADGE.on);
        retryMs = 5000;
        // CONNECT, MQTT 3.1.1, clean session, keep-alive 60 s
        socket.send(new Uint8Array([0x10, 12, 0, 4, 77, 81, 84, 84, 4, 2, 0, 60, 0, 0]));
        pingTimer = setInterval(() => {
            if (socket.readyState === WebSocket.OPEN) socket.send(new Uint8Array([0xc0, 0x00]));
        }, 30000);
    };
    socket.onclose = () => {
        setBadge(BADGE.off);
        clearInterval(pingTimer);
        afterClose();
    };
    socket.onmessage = (ev) => {
        const chunk = new Uint8Array(ev.data);
        const merged = new Uint8Array(rxBuffer.length + chunk.length);
        merged.set(rxBuffer);
        merged.set(chunk, rxBuffer.length);
        rxBuffer = merged;
        let pos = 0;
        for (let p = nextPacket(rxBuffer, pos); p; p = nextPacket(rxBuffer, pos)) {
            handlePacket(p.header, p.body);
            pos = p.end;
        }
        rxBuffer = rxBuffer.slice(pos);
    };
}

// nextPacket liefert das vollständige Paket ab pos, oder null, wenn es noch nicht ganz da ist.
export function nextPacket(buf, pos) {
    if (buf.length - pos < 2) return null;
    let offset = pos + 1, multiplier = 1, length = 0, digit;
    do {
        if (offset >= buf.length) return null;
        digit = buf[offset++];
        length += (digit & 127) * multiplier;
        multiplier *= 128;
    } while (digit & 128);
    const end = offset + length;
    if (end > buf.length) return null;
    return { header: buf[pos], body: buf.subarray(offset, end), end };
}

function handlePacket(header, body) {
    const type = header >> 4;
    if (type === 2) socket.send(new Uint8Array([0x82, 6, 0, 1, 0, 1, 35, 0])); // CONNACK → SUBSCRIBE #
    else if (type === 3) handlePublish(header, body);
}

function handlePublish(header, data) {
    const topicLen = (data[0] << 8) | data[1];
    let offset = 2;
    const topic = new TextDecoder().decode(data.subarray(offset, offset + topicLen));
    offset += topicLen;
    if ((header & 0x06) >> 1) offset += 2; // Paket-ID bei QoS > 0
    const text = new TextDecoder().decode(data.subarray(offset));
    let payload;
    try { payload = JSON.parse(text); } catch { payload = text; }
    updateTree(topic, payload);
}

function updateTree(topic, payload) {
    const parts = topic.split('/');
    let cur = treeData;
    parts.forEach((part, i) => {
        if (i === parts.length - 1) {
            const isObj = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);
            if (isObj(payload) && isObj(cur[part])) Object.assign(cur[part], payload);
            else cur[part] = payload;
            return;
        }
        if (!cur[part] || typeof cur[part] !== 'object' || Array.isArray(cur[part])) cur[part] = {};
        cur = cur[part];
    });
    if (!renderPending) {
        renderPending = true;
        setTimeout(() => { renderPending = false; requestAnimationFrame(renderTree); }, 500);
    }
}

// Baum über DOM-Funktionen und textContent, nie innerHTML: Topics und Werte kommen von jedem,
// der am lokalen Broker veröffentlichen kann (Sicherheitsprüfung P3-06).
function icon(chevron, open) {
    const svg = document.createElementNS(SVG_NS, 'svg');
    svg.setAttribute('class', 'toggle-icon' + (chevron && open ? ' open' : ''));
    svg.setAttribute('viewBox', '0 0 24 24');
    if (chevron) {
        svg.setAttribute('fill', 'none');
        svg.setAttribute('stroke', 'currentColor');
        svg.setAttribute('stroke-width', '2');
        const p = document.createElementNS(SVG_NS, 'path');
        p.setAttribute('d', 'M9 18l6-6-6-6');
        svg.appendChild(p);
    } else {
        svg.setAttribute('fill', 'currentColor');
        const c = document.createElementNS(SVG_NS, 'circle');
        c.setAttribute('cx', '12'); c.setAttribute('cy', '12'); c.setAttribute('r', '4');
        svg.appendChild(c);
    }
    return svg;
}

function span(cls, text) {
    const e = document.createElement('span');
    if (cls) e.className = cls;
    e.textContent = text;
    return e;
}

function valueNode(val) {
    if (typeof val === 'string') { const e = span('node-val-string', `"${val}"`); e.title = val; return e; }
    if (typeof val === 'number') return span('node-val-number', String(val));
    if (typeof val === 'boolean') return span('node-val-boolean', String(val));
    if (val === null) return span('node-val-null', 'null');
    return span('node-val-string', String(val));
}

// Gebaut werden nur aufgeklappte Zweige, und je Knoten höchstens MAX_CHILDREN Kinder. Bis
// 26.11.2026 entstand bei jeder Nachricht der GANZE Baum neu — mit bridge/definitions und
// bridge/devices von zigbee2mqtt rund 118 000 DOM-Knoten, bis zu 460 ms je Durchlauf: Die Seite
// reagierte nicht mehr, auch im Statistik-Tab (die Live-Verbindung blieb offen).
const MAX_CHILDREN = 200;
const shownLimit = new Map(); // Pfad → aufgedeckte Kinder („… more“)

function fillChildren(kids, val, p) {
    const entries = Object.entries(val);
    const limit = shownLimit.get(p) || MAX_CHILDREN;
    const frag = document.createDocumentFragment();
    for (const [k, v] of entries.slice(0, limit)) frag.appendChild(createNode(k, v, p));
    if (entries.length > limit) {
        const more = document.createElement('button');
        more.className = 'ml-6 py-0.5 text-sky-600 hover:underline dark:text-sky-400';
        more.textContent = `… ${entries.length - limit} more`;
        more.addEventListener('click', (ev) => {
            ev.stopPropagation();
            shownLimit.set(p, limit + MAX_CHILDREN);
            kids.replaceChildren();
            fillChildren(kids, val, p);
        });
        frag.appendChild(more);
    }
    kids.replaceChildren(frag);
}

function createNode(key, val, path) {
    const isObj = val !== null && typeof val === 'object';
    const hasChildren = isObj && Object.keys(val).length > 0;
    const p = path ? `${path}/${key}` : key;
    const open = openPaths.has(p);
    const node = document.createElement('div');
    node.className = 'tree-node';
    const head = document.createElement('div');
    head.className = 'tree-node-header';
    head.addEventListener('click', () => {
        const kids = head.nextElementSibling;
        const nowOpen = kids.classList.toggle('open');
        head.querySelector('.toggle-icon').classList.toggle('open');
        if (nowOpen) {
            openPaths.add(p);
            if (hasChildren && !kids.hasChildNodes()) fillChildren(kids, val, p);
        } else {
            openPaths.delete(p);
            kids.replaceChildren(); // zugeklappt: nichts im DOM halten
        }
    });
    head.append(icon(hasChildren, open), span('node-key', key));
    if (!isObj) {
        const wrap = span('ml-2', ': ');
        wrap.appendChild(valueNode(val));
        head.appendChild(wrap);
    }
    const kids = document.createElement('div');
    kids.className = 'node-children' + (open ? ' open' : '');
    if (hasChildren && open) fillChildren(kids, val, p);
    node.append(head, kids);
    return node;
}

function renderTree() {
    if (Object.keys(treeData).length === 0) return;
    if (container.offsetParent === null) { dirty = true; return; } // Tab unsichtbar: beim Zeigen
    dirty = false;
    const frag = document.createDocumentFragment();
    for (const [k, v] of Object.entries(treeData)) frag.appendChild(createNode(k, v, ''));
    container.replaceChildren(frag);
}

function showWaiting() {
    const e = document.createElement('div');
    e.className = 'py-10 text-center text-slate-400';
    e.textContent = 'Waiting for MQTT messages…';
    container.replaceChildren(e);
}
