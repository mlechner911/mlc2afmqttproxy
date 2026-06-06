<script lang="ts">
  import TreeNode from './TreeNode.svelte';
  export let name: string;
  export let node: any;
  export let depth: number = 0;

  let expanded = true;

  function toggle() {
    expanded = !expanded;
  }
  
  function formatPayload(payload: any): string {
    return typeof payload === 'object' ? JSON.stringify(payload) : String(payload);
  }

  $: hasChildren = Object.keys(node._children || {}).length > 0;
</script>

<div class="tree-node">
  <div class="tree-header" on:click={toggle} on:keydown={(e) => e.key === 'Enter' && toggle()} tabindex="0" role="button">
    {#if hasChildren}
      <span class="tree-icon" style="font-family: 'JetBrains Mono', monospace; width: 24px; color: #64748b; font-weight: bold;">
        {expanded ? '[-]' : '[+]'}
      </span>
    {:else}
      <span class="tree-icon" style="font-family: 'JetBrains Mono', monospace; width: 24px; color: #475569; font-weight: normal;">
        [•]
      </span>
    {/if}
    <span style="font-weight: {hasChildren ? '700' : '500'};">{name}</span>
  </div>

  {#if expanded}
    <div class="tree-children">
      <!-- Payload anzeigen wenn vorhanden -->
      {#if node._msg}
        <div class="payload-box">
          {formatPayload(node._msg.payload)}<span class="payload-time">{node._msg.timestamp.toLocaleTimeString()}</span>
        </div>
      {/if}

      <!-- Kinder rekursiv rendern -->
      {#each Object.entries(node._children || {}) as [childName, childNode]}
        <TreeNode name={childName} node={childNode} depth={depth + 1} />
      {/each}
    </div>
  {/if}
</div>

<style>
  .cursor-pointer { cursor: pointer; }
  .tree-node { transition: all 0.2s; }
</style>
