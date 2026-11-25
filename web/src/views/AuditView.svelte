<script lang="ts">
  import { onMount } from 'svelte';
  import Pagination from '../components/Pagination.svelte';
  import { api, ApiError } from '../lib/api';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { AuditEntry, AuditVerifyResult, PaginatedResponse } from '../lib/types';

  let entries = $state<AuditEntry[]>([]);
  let total = $state(0);
  let limit = 25;
  let offset = $state(0);
  let filterAction = $state('');
  let filterActor = $state('');
  let loading = $state(true);

  let expandedIds = $state<Record<number, boolean>>({});
  let verifying = $state(false);
  let verifyResult = $state<AuditVerifyResult | null>(null);

  async function loadAudit() {
    loading = true;
    try {
      const qs = new URLSearchParams({
        limit: String(limit),
        offset: String(offset),
      });
      if (filterAction.trim()) qs.set('action', filterAction.trim());
      if (filterActor.trim()) qs.set('actor', filterActor.trim());
      const res = await api.get<PaginatedResponse<AuditEntry>>(`/audit?${qs.toString()}`);
      entries = res.items;
      total = res.total;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  onMount(loadAudit);

  function handleFilter(e: SubmitEvent) {
    e.preventDefault();
    offset = 0;
    loadAudit();
  }

  function toggleDetail(id: number) {
    expandedIds = { ...expandedIds, [id]: !expandedIds[id] };
  }

  async function verifyIntegrity() {
    verifying = true;
    verifyResult = null;
    try {
      verifyResult = await api.get<AuditVerifyResult>('/audit/verify');
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      verifying = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.audit.title}</h1>
    <p class="page-subtitle">{i18n.t.audit.subtitle}</p>
  </div>
  <button type="button" class="btn btn-primary" onclick={verifyIntegrity} disabled={verifying}>
    {verifying ? i18n.t.audit.verifying : i18n.t.audit.verifyIntegrity}
  </button>
</div>

{#if verifyResult}
  <div
    class="alert"
    class:alert-success={verifyResult.valid}
    class:alert-danger={!verifyResult.valid}
    role="status"
  >
    {#if verifyResult.valid}
      {fmt(i18n.t.audit.verifyValid, { checked: verifyResult.checked })}
    {:else}
      {fmt(i18n.t.audit.verifyBroken, {
        brokenAt: verifyResult.broken_at ?? '?',
        checked: verifyResult.checked,
      })}
    {/if}
  </div>
{/if}

<div class="toolbar">
  <form class="toolbar-filters" onsubmit={handleFilter}>
    <input
      type="text"
      class="form-control"
      placeholder={i18n.t.audit.filterAction}
      bind:value={filterAction}
      aria-label={i18n.t.audit.filterAction}
    />
    <input
      type="text"
      class="form-control"
      placeholder={i18n.t.audit.filterActor}
      bind:value={filterActor}
      aria-label={i18n.t.audit.filterActor}
    />
    <button type="submit" class="btn">{i18n.t.common.search}</button>
  </form>
  <button type="button" class="btn btn-sm" onclick={loadAudit} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>#</th>
        <th>{i18n.t.audit.timestamp}</th>
        <th>{i18n.t.audit.actor}</th>
        <th>{i18n.t.audit.action}</th>
        <th>{i18n.t.audit.object}</th>
        <th>{i18n.t.audit.result}</th>
        <th>{i18n.t.audit.ip}</th>
        <th>{i18n.t.audit.detail}</th>
      </tr>
    </thead>
    <tbody>
      {#if loading && entries.length === 0}
        <tr><td colspan="8" class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if entries.length === 0}
        <tr><td colspan="8" class="empty-state">{i18n.t.common.noData}</td></tr>
      {:else}
        {#each entries as entry (entry.id)}
          <tr>
            <td class="mono">{entry.id}</td>
            <td>{formatDateTime(entry.at, i18n.locale === 'id' ? 'id-ID' : 'en-US')}</td>
            <td class="mono">
              {entry.actor_username}
              {#if entry.acting_as}
                <span class="badge badge-warning">as {entry.acting_as}</span>
              {/if}
            </td>
            <td class="mono">{entry.action}</td>
            <td class="mono">{entry.object_type}:{entry.object_id}</td>
            <td>
              <span
                class="badge"
                class:badge-success={entry.result === 'success'}
                class:badge-danger={entry.result === 'failure'}
              >
                {entry.result}
              </span>
            </td>
            <td class="mono">{entry.ip}</td>
            <td>
              <button
                type="button"
                class="btn btn-sm"
                onclick={() => toggleDetail(entry.id)}
              >
                {expandedIds[entry.id] ? i18n.t.audit.hideDetail : i18n.t.audit.showDetail}
              </button>
            </td>
          </tr>
          {#if expandedIds[entry.id]}
            <tr>
              <td colspan="8">
                <pre class="json-box">{JSON.stringify(entry.detail, null, 2)}</pre>
              </td>
            </tr>
          {/if}
        {/each}
      {/if}
    </tbody>
  </table>
</div>

<Pagination
  {total}
  {limit}
  {offset}
  onchange={(newOffset) => {
    offset = newOffset;
    loadAudit();
  }}
/>
