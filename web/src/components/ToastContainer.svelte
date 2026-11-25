<script lang="ts">
  import { toast } from '../lib/toast.svelte';
  import { i18n } from '../lib/i18n.svelte';
</script>

{#if toast.items.length > 0}
  <div class="toast-container" aria-live="polite" aria-atomic="true">
    {#each toast.items as item (item.id)}
      <div
        class="toast"
        class:toast-success={item.kind === 'success'}
        class:toast-error={item.kind === 'error'}
        class:toast-info={item.kind === 'info'}
        role="status"
      >
        <span>
          {item.message}
          {#if item.link}
            <a class="toast-link" href={item.link.href}>{item.link.label} →</a>
          {/if}
        </span>
        <button
          type="button"
          class="btn btn-ghost btn-sm"
          aria-label={i18n.t.common.close}
          onclick={() => toast.dismiss(item.id)}
        >
          ✕
        </button>
      </div>
    {/each}
  </div>
{/if}
