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
let container, badge;

const BADGE = {
    on: ['Connected', 'pill bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300'],
    off: ['Disconnected', 'pill bg-rose-100 text-rose-800 dark:bg-rose-900/40 dark:text-rose-300'],
};

export function initLive() {
    container = document.getElementById('tree-container');
    badge = document.getElementById('conn-badge');
    document.getElementById('clear-tree-btn').addEventListener('click', () => {
        for (const k in treeData) delete treeData[k];
        showWaiting();
    });
}

// startLive verbindet einmal; danach hält sich die Verbindung selbst (Wiederverbinden nach 5 s).
export function startLive() {
    if (socket) return;
    connect();
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
        // CONNECT, MQTT 3.1.1, clean session, keep-alive 60 s
        socket.send(new Uint8Array([0x10, 12, 0, 4, 77, 81, 84, 84, 4, 2, 0, 60, 0, 0]));
        pingTimer = setInterval(() => {
            if (socket.readyState === WebSocket.OPEN) socket.send(new Uint8Array([0xc0, 0x00]));
        }, 30000);
    };
    socket.onclose = () => {
        setBadge(BADGE.off);
        clearInterval(pingTimer);
        setTimeout(connect, 5000);
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
        const nowOpen = head.nextElementSibling.classList.toggle('open');
        head.querySelector('.toggle-icon').classList.toggle('open');
        if (nowOpen) openPaths.add(p); else openPaths.delete(p);
    });
    head.append(icon(hasChildren, open), span('node-key', key));
    if (!isObj) {
        const wrap = span('ml-2', ': ');
        wrap.appendChild(valueNode(val));
        head.appendChild(wrap);
    }
    const kids = document.createElement('div');
    kids.className = 'node-children' + (open ? ' open' : '');
    if (hasChildren) for (const [k, v] of Object.entries(val)) kids.appendChild(createNode(k, v, p));
    node.append(head, kids);
    return node;
}

function renderTree() {
    if (Object.keys(treeData).length === 0) return;
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
