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
const chevronIcon = `<svg class="toggle-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 18l6-6-6-6"/></svg>`;
const dotIcon = `<svg class="toggle-icon" viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="12" r="4"/></svg>`;

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

// MQTT WebSocket
function connectMQTT() {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${proto}//${window.location.host}/mqtt`;
    
    mqttSocket = new WebSocket(wsUrl, ['mqtt']);
    
    mqttSocket.onopen = () => {
        elConnBadge.textContent = 'Connected';
        elConnBadge.className = 'badge bg-success rounded-pill px-3 py-2';
        
        // Sende MQTT CONNECT Paket
        const connectPacket = new Uint8Array([
            0x10, 12, 0, 4, 77, 81, 84, 84, 4, 2, 0, 60, 0, 0
        ]);
        mqttSocket.send(connectPacket);
    };
    
    mqttSocket.onclose = () => {
        elConnBadge.textContent = 'Disconnected';
        elConnBadge.className = 'badge bg-danger rounded-pill px-3 py-2';
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
    let offset = 2; // Fixed header simplified
    const topicLen = (data[offset] << 8) | data[offset+1];
    offset += 2;
    
    const topic = new TextDecoder().decode(data.slice(offset, offset + topicLen));
    offset += topicLen;
    
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
            current[part] = payload;
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
function createNodeHtml(key, val) {
    const isObj = val !== null && typeof val === 'object' && !Array.isArray(val);
    const hasChildren = isObj && Object.keys(val).length > 0;
    
    let valHtml = '';
    if (!isObj) {
        if (typeof val === 'string') valHtml = `<span class="node-val-string">"${val}"</span>`;
        else if (typeof val === 'number') valHtml = `<span class="node-val-number">${val}</span>`;
        else if (typeof val === 'boolean') valHtml = `<span class="node-val-boolean">${val}</span>`;
        else if (val === null) valHtml = `<span class="node-val-null">null</span>`;
        else valHtml = `<span class="node-val-string">${val}</span>`;
    }

    let html = `<div class="tree-node">
        <div class="tree-node-header" onclick="this.nextElementSibling.classList.toggle('open'); this.querySelector('.toggle-icon').classList.toggle('open')">
            ${hasChildren ? chevronIcon : dotIcon}
            <span class="node-key">${key}</span>
            ${!isObj ? `<span class="ms-2">: ${valHtml}</span>` : ''}
        </div>
        <div class="node-children ${key === 'root' ? 'open' : ''}">`;

    if (hasChildren) {
        for (const [k, v] of Object.entries(val)) {
            html += createNodeHtml(k, v);
        }
    }

    html += `</div></div>`;
    return html;
}

function renderTree() {
    if (Object.keys(treeData).length === 0) return;
    
    let html = '';
    for (const [k, v] of Object.entries(treeData)) {
        html += createNodeHtml(k, v);
    }
    treeContainer.innerHTML = html;
}

document.getElementById('clear-tree-btn').addEventListener('click', () => {
    for (const key in treeData) delete treeData[key];
    treeContainer.innerHTML = '<div class="text-muted text-center py-5">Waiting for MQTT messages...</div>';
});

// Boot
init();
