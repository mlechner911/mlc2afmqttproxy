<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import TreeNode from './TreeNode.svelte';

  if (typeof window !== 'undefined') {
    window.onerror = function(msg, src, lineno, colno, error) {
      document.body.innerHTML += `<div style="color:red; z-index:9999; position:absolute; top:0; left:0; background:white; padding:20px; border: 2px solid red;">Global JS Error: ${msg} <br> ${error?.stack}</div>`;
    };
  }

  declare const mqtt: any;

  interface MqttMessage {
    payload: string;
    timestamp: Date;
  }

  let client: any;
  let messages: Record<string, MqttMessage> = {};
  let bufferCount = 0;
  let connected = false;
  let errorMsg = '';
  let statsInterval: number;
  let treeData: Record<string, any> = {};

  // Build a recursive tree from flat topics
  function buildTree(msgs: Record<string, MqttMessage>) {
    const tree: Record<string, any> = {};
    for (const [topic, msg] of Object.entries(msgs)) {
      const parts = topic.split('/');
      let current = tree;
      for (let i = 0; i < parts.length; i++) {
        const part = parts[i];
        if (!current[part]) {
          current[part] = { _msg: null, _children: {} };
        }
        if (i === parts.length - 1) {
          current[part]._msg = msg;
        }
        current = current[part]._children;
      }
    }
    return tree;
  }

  $: treeData = buildTree(messages);

  onMount(() => {
    const fetchStats = async () => {
      try {
        const res = await fetch('/api/v1/stats');
        const data = await res.json();
        bufferCount = data.buffer_count;
      } catch (e) {
        console.error('Stats error', e);
      }
    };
    fetchStats();
    statsInterval = setInterval(fetchStats, 2000) as unknown as number;

    const wsUrl = `ws://${window.location.host}/mqtt`;
    client = mqtt.connect(wsUrl, {
      protocolId: 'MQTT',
      protocolVersion: 4,
      clean: true,
      connectTimeout: 5000,
      clientId: 'web-dashboard-' + Math.random().toString(16).substr(2, 8)
    });

    client.on('connect', () => {
      connected = true;
      errorMsg = '';
      client.subscribe('#');
    });

    client.on('offline', () => {
      connected = false;
      errorMsg = 'Verbindung offline / getrennt.';
    });

    client.on('reconnect', () => {
      errorMsg = 'Verbinde neu...';
    });

    client.on('error', (err: any) => {
      console.error('MQTT Error:', err);
      errorMsg = String(err.message || err);
    });

    client.on('close', () => {
      connected = false;
    });

    client.on('message', (topic: string, payload: any) => {
      let payloadStr = payload.toString();
      try {
        const parsed = JSON.parse(payloadStr);
        payloadStr = JSON.stringify(parsed, null, 2);
      } catch (e) {}

      messages[topic] = {
        payload: payloadStr,
        timestamp: new Date()
      };
      messages = { ...messages };
    });
  });

  onDestroy(() => {
    clearInterval(statsInterval);
    if (client) client.end();
  });
</script>

<main>
  <header class="app-header">
    <div class="container-fluid px-4">
      <h1 class="app-title">mlc2afmqttproxy</h1>
    </div>
  </header>

  <div class="container-fluid px-4">
    <div class="row g-3">
      <!-- Status Column -->
      <div class="col-12 col-md-3 col-xl-2">
        <div class="card mb-3">
          <div class="card-header">System Link</div>
          <div class="card-body">
            <div class="d-flex justify-content-between align-items-center mb-2">
              <span class="text-muted">Websocket</span>
              <span class="badge {connected ? 'bg-success' : 'bg-danger'}">
                {connected ? 'CONNECTED' : 'OFFLINE'}
              </span>
            </div>
            <div class="d-flex justify-content-between align-items-center">
              <span class="text-muted">Stored data</span>
              <span class="badge {bufferCount === 0 ? 'bg-success' : 'bg-primary'}">
                {bufferCount} items
              </span>
            </div>

            {#if errorMsg}
              <div class="alert alert-danger mt-3 mb-0 p-2" style="font-size: 0.75rem; border-radius: 4px;">
                {errorMsg}
              </div>
            {/if}
          </div>
        </div>
      </div>

      <!-- Tree Column -->
      <div class="col-12 col-md-9 col-xl-10">
        <div class="card">
          <div class="card-header d-flex justify-content-between align-items-center">
            <span>Live Topic Stream</span>
            <span class="badge bg-secondary" style="font-size:0.65rem;">SUB: #</span>
          </div>
          <div class="card-body" style="background-color: #0f0f11; min-height: 60vh; max-height: 80vh; overflow-y: auto;">
            {#if Object.keys(treeData).length === 0}
              <div class="text-center text-muted py-5" style="font-family: 'JetBrains Mono', monospace;">
                [ Awaiting network packets... ]
              </div>
            {:else}
              <div class="animate-fade-in">
                {#each Object.entries(treeData) as [rootName, rootNode]}
                  <TreeNode name={rootName} node={rootNode} depth={0} />
                {/each}
              </div>
            {/if}
          </div>
        </div>
      </div>
    </div>
  </div>
</main>
