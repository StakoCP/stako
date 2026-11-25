<script lang="ts">
  import { fmt, i18n } from '../lib/i18n.svelte';

  interface Props {
    total: number;
    limit: number;
    offset: number;
    onchange: (newOffset: number) => void;
  }

  let { total, limit, offset, onchange }: Props = $props();

  let from = $derived(total === 0 ? 0 : offset + 1);
  let to = $derived(Math.min(offset + limit, total));
  let hasPrev = $derived(offset > 0);
  let hasNext = $derived(offset + limit < total);
</script>

{#if total > 0}
  <div class="pagination">
    <span>{fmt(i18n.t.common.showing, { from, to, total })}</span>
    <div class="pagination-buttons">
      <button
        type="button"
        class="btn btn-sm"
        disabled={!hasPrev}
        onclick={() => onchange(Math.max(0, offset - limit))}
      >
        {i18n.t.common.prev}
      </button>
      <button
        type="button"
        class="btn btn-sm"
        disabled={!hasNext}
        onclick={() => onchange(offset + limit)}
      >
        {i18n.t.common.next}
      </button>
    </div>
  </div>
{/if}
