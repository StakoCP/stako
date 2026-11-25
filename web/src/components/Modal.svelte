<script lang="ts">
  import type { Snippet } from 'svelte';
  import { i18n } from '../lib/i18n.svelte';

  interface Props {
    open: boolean;
    title: string;
    size?: 'md' | 'lg';
    onclose: () => void;
    children: Snippet;
    footer?: Snippet;
  }

  let { open, title, size = 'md', onclose, children, footer }: Props = $props();

  const headingId = `modal-title-${Math.random().toString(36).slice(2, 9)}`;

  function handleKeydown(e: KeyboardEvent) {
    if (open && e.key === 'Escape') {
      e.stopPropagation();
      onclose();
    }
  }

  function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) {
      onclose();
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
  <div
    class="modal-backdrop"
    role="presentation"
    onclick={handleBackdropClick}
  >
    <div
      class="modal-dialog"
      class:modal-dialog-lg={size === 'lg'}
      role="dialog"
      aria-modal="true"
      aria-labelledby={headingId}
    >
      <div class="modal-header">
        <h2 id={headingId} class="modal-title">{title}</h2>
        <button
          type="button"
          class="btn btn-ghost btn-sm"
          aria-label={i18n.t.common.close}
          onclick={onclose}
        >
          ✕
        </button>
      </div>
      <div class="modal-body">
        {@render children()}
      </div>
      {#if footer}
        <div class="modal-footer">
          {@render footer()}
        </div>
      {/if}
    </div>
  </div>
{/if}
