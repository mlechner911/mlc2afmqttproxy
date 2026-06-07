let apiPrefix = '/api/v1';
let mqttSocket = null;
const treeData = {};
const treeContainer = document.getElementById('tree-container');

// Elements
const elConnBadge = document.getElementById('conn-badge');
const elUptime = document.getElementById('uptime-display');
const elMode = document.getElementById('mode-display');
const elTarget = document.getElementById('target-display');
const elBuffer = document.getElementById('buffer-display');

const elMetrics = {
    received: document.getElementById('metric-received'),
    stored: document.getElementById('metric-stored'),
    forwarded: document.getElementById('metric-forwarded'),
    failed: document.getElementById('metric-failed')
};

// SVG Icons
const dotIcon = `<svg class="toggle-icon" viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="12" r="4"/></svg>`;

// Tree State
const openPaths = new Set();
openPaths.add('root');

function toggleNode(el, path) {
    const isClosed = el.nextElementSibling.classList.toggle('open');
    el.querySelector('.toggle-icon').classList.toggle('open');
    if (isClosed) openPaths.add(path);
    else openPaths.delete(path);
}

// Format Uptime
function formatUptime(seconds) {
    const d = Math.floor(seconds / (3600*24));
    const h = Math.floor(seconds % (3600*24) / 3600);
    const m = Math.floor(seconds % 3600 / 60);
    const s = Math.floor(seconds % 60);
    
    let parts = [];
    if(d > 0) parts.push(`${d}d`);
    if(h > 0) parts.push(`${h}h`);
    if(m > 0) parts.push(`${m}m`);
    parts.push(`${s}s`);
    return parts.join(' ');
}

// Init
async function init() {
    try {
        const res = await fetch('/config');
        const config = await res.json();
        
        apiPrefix = config.api_prefix;
        elMode.textContent = config.mode.toUpperCase();
        elTarget.textContent = config.target;
        elUptime.textContent = `Uptime: ${formatUptime(config.uptime_seconds)}`;

        startPollingStats();
        connectMQTT();
        
        // Lokale Uptime-Uhr weiter ticken lassen
        let currentSeconds = config.uptime_seconds;
        setInterval(() => {
            currentSeconds++;
            elUptime.textContent = `Uptime: ${formatUptime(currentSeconds)}`;
        }, 1000);

    } catch(err) {
        console.error("Config fetch failed:", err);
    }
}

// Poll Stats
async function pollStats() {
    try {
        const res = await fetch(`${apiPrefix}/stats`);
        const stats = await res.json();
        
        elBuffer.textContent = stats.buffer_count.toLocaleString();
        elMetrics.received.textContent = stats.messages_received_total.toLocaleString();
        elMetrics.stored.textContent = stats.messages_stored_total.toLocaleString();
        elMetrics.forwarded.textContent = stats.messages_forwarded_total.toLocaleString();
        elMetrics.failed.textContent = stats.messages_forward_failed_total.toLocaleString();
    } catch(err) {
        console.error("Stats poll failed:", err);
    }
}

function startPollingStats() {
    pollStats();
    setInterval(pollStats, 2000);
}

let pingInterval;

// MQTT WebSocket
function connectMQTT() {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${proto}//${window.location.host}/mqtt`;
    
    mqttSocket = new WebSocket(wsUrl, ['mqtt']);
    
    mqttSocket.onopen = () => {
        elConnBadge.textContent = 'Connected';
        elConnBadge.className = 'badge bg-success rounded-pill px-3 py-2';
        
        // Sende MQTT CONNECT Paket (Keep-Alive 60s)
        const connectPacket = new Uint8Array([
            0x10, 12, 0, 4, 77, 81, 84, 84, 4, 2, 0, 60, 0, 0
        ]);
        mqttSocket.send(connectPacket);
        
        // Keep-Alive Ping (PINGREQ) alle 30 Sekunden senden
        pingInterval = setInterval(() => {
            if (mqttSocket.readyState === WebSocket.OPEN) {
                mqttSocket.send(new Uint8Array([0xC0, 0x00])); // PINGREQ
            }
        }, 30000);
    };
    
    mqttSocket.onclose = () => {
        elConnBadge.textContent = 'Disconnected';
        elConnBadge.className = 'badge bg-danger rounded-pill px-3 py-2';
        if (pingInterval) clearInterval(pingInterval);
        setTimeout(connectMQTT, 5000);
    };

    mqttSocket.onmessage = async (event) => {
        const buffer = await event.data.arrayBuffer();
        const data = new Uint8Array(buffer);
        
        if (data.length > 0) {
            const type = data[0] >> 4;
            
            if (type === 2) { // CONNACK
                // Subscribe to #
                const subPacket = new Uint8Array([
                    0x82, 6, 0, 1, 0, 1, 35, 0
                ]);
                mqttSocket.send(subPacket);
            } else if (type === 3) { // PUBLISH
                handlePublish(data);
            }
        }
    };
}

function handlePublish(data) {
    let offset = 1; // Skip Type & Flags byte
    
    // Decode Remaining Length (1-4 bytes)
    let multiplier = 1;
    let remainingLength = 0;
    let digit = 0;
    do {
        digit = data[offset++];
        remainingLength += (digit & 127) * multiplier;
        multiplier *= 128;
    } while ((digit & 128) !== 0);
    
    // Read Topic Length
    const topicLen = (data[offset] << 8) | data[offset+1];
    offset += 2;
    
    // Read Topic
    const topic = new TextDecoder().decode(data.slice(offset, offset + topicLen));
    offset += topicLen;
    
    // Check QoS from first byte (data[0])
    const qos = (data[0] & 0x06) >> 1;
    if (qos > 0) {
        offset += 2; // Skip Packet Identifier
    }
    
    // Remaining bytes are payload
    const payloadStr = new TextDecoder().decode(data.slice(offset));
    
    let parsedPayload;
    try {
        parsedPayload = JSON.parse(payloadStr);
    } catch {
        parsedPayload = payloadStr;
    }

    updateTree(topic, parsedPayload);
}

function updateTree(topic, payload) {
    const parts = topic.split('/');
    let current = treeData;
    
    for (let i = 0; i < parts.length; i++) {
        const part = parts[i];
        if (i === parts.length - 1) {
            if (typeof payload === 'object' && payload !== null && !Array.isArray(payload) &&
                typeof current[part] === 'object' && current[part] !== null && !Array.isArray(current[part])) {
                Object.assign(current[part], payload);
            } else {
                current[part] = payload;
            }
        } else {
            if (!current[part] || typeof current[part] !== 'object' || Array.isArray(current[part])) {
                current[part] = {};
            }
            current = current[part];
        }
    }
    
    renderTree();
}

// Tree Rendering
function createNodeHtml(key, val, path) {
    const isObj = val !== null && typeof val === 'object' && !Array.isArray(val);
    const hasChildren = isObj && Object.keys(val).length > 0;
    const currentPath = path ? `${path}/${key}` : key;
    const isOpen = openPaths.has(currentPath) || key === 'root';
    
    let valHtml = '';
    if (!isObj) {
        if (typeof val === 'string') {
            const escapedVal = val.replace(/"/g, '&quot;');
            valHtml = `<span class="node-val-string" title="${escapedVal}">"${escapedVal}"</span>`;
        }
        else if (typeof val === 'number') valHtml = `<span class="node-val-number">${val}</span>`;
        else if (typeof val === 'boolean') valHtml = `<span class="node-val-boolean">${val}</span>`;
        else if (val === null) valHtml = `<span class="node-val-null">null</span>`;
        else valHtml = `<span class="node-val-string">${val}</span>`;
    }

    const chevronSvg = `<svg class="toggle-icon ${isOpen ? 'open' : ''}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 18l6-6-6-6"/></svg>`;

    let html = `<div class="tree-node">
        <div class="tree-node-header" onclick="toggleNode(this, '${currentPath}')">
            ${hasChildren ? chevronSvg : dotIcon}
            <span class="node-key">${key}</span>
            ${!isObj ? `<span class="ms-2">: ${valHtml}</span>` : ''}
        </div>
        <div class="node-children ${isOpen ? 'open' : ''}">`;

    if (hasChildren) {
        for (const [k, v] of Object.entries(val)) {
            html += createNodeHtml(k, v, currentPath);
        }
    }

    html += `</div></div>`;
    return html;
}

function renderTree() {
    if (Object.keys(treeData).length === 0) return;
    
    let html = '';
    for (const [k, v] of Object.entries(treeData)) {
        html += createNodeHtml(k, v, '');
    }
    treeContainer.innerHTML = html;
}

document.getElementById('clear-tree-btn').addEventListener('click', () => {
    for (const key in treeData) delete treeData[key];
    treeContainer.innerHTML = '<div class="text-muted text-center py-5">Waiting for MQTT messages...</div>';
});

// Boot
init();
