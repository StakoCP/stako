<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { DNSProvider, DNSRecord, DNSZone } from '../lib/types';

  const RECORD_TYPES = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA'] as const;

  let zones = $state<DNSZone[]>([]);
  let loadingZones = $state(true);
  let selectedZone = $state<DNSZone | null>(null);
  let records = $state<DNSRecord[]>([]);
  let loadingRecords = $state(false);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');

  // Create Zone modal
  let createZoneOpen = $state(false);
  let fDomain = $state('');
  let fProvider = $state<DNSProvider>('powerdns');
  let zoneFormError = $state('');
  let savingZone = $state(false);

  // Delete Zone modal
  let deleteZoneTarget = $state<DNSZone | null>(null);
  let deletingZone = $state(false);

  // Add Record modal
  let addRecordOpen = $state(false);
  let fRecType = $state('A');
  let fRecName = $state('');
  let fRecContent = $state('');
  let fRecTtl = $state(300);
  let fRecPriority = $state('');
  let recFormError = $state('');
  let savingRecord = $state(false);

  // Delete Record modal
  let deleteRecordTarget = $state<DNSRecord | null>(null);
  let deletingRecord = $state(false);

  async function loadZones() {
    loadingZones = true;
    try {
      const res = await api.get<{ items: DNSZone[] }>('/dns/zones');
      zones = res.items;
      if (selectedZone) {
        const updated = zones.find((z) => z.id === selectedZone?.id) ?? null;
        selectedZone = updated;
        if (updated) {
          await loadRecords(updated.id);
        } else {
          records = [];
        }
      }
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingZones = false;
    }
  }

  async function loadRecords(zoneId: string) {
    loadingRecords = true;
    try {
      const res = await api.get<{ items: DNSRecord[] }>(`/dns/zones/${zoneId}/records`);
      records = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingRecords = false;
    }
  }

  onMount(() => {
    loadZones();
  });

  function selectZone(zone: DNSZone) {
    selectedZone = zone;
    loadRecords(zone.id);
  }

  function openCreateZone() {
    fDomain = '';
    fProvider = 'powerdns';
    zoneFormError = '';
    createZoneOpen = true;
  }

  async function submitCreateZone(e: SubmitEvent) {
    e.preventDefault();
    zoneFormError = '';
    savingZone = true;
    try {
      const created = await api.post<DNSZone>('/dns/zones', {
        domain: fDomain.trim(),
        provider: fProvider,
      });
      toast.success(i18n.t.dns.zoneCreated);
      createZoneOpen = false;
      await loadZones();
      selectZone(created);
    } catch (err) {
      if (err instanceof ApiError) {
        const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
        zoneFormError = fieldMsg || err.detail || err.title;
      } else {
        zoneFormError = i18n.t.common.errorGeneric;
      }
    } finally {
      savingZone = false;
    }
  }

  async function confirmDeleteZone() {
    if (!deleteZoneTarget) return;
    deletingZone = true;
    try {
      await api.delete(`/dns/zones/${deleteZoneTarget.id}`);
      toast.success(i18n.t.dns.zoneDeleted);
      if (selectedZone?.id === deleteZoneTarget.id) {
        selectedZone = null;
        records = [];
      }
      deleteZoneTarget = null;
      await loadZones();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      deleteZoneTarget = null;
    } finally {
      deletingZone = false;
    }
  }

  function openAddRecord() {
    fRecType = 'A';
    fRecName = '';
    fRecContent = '';
    fRecTtl = 300;
    fRecPriority = '';
    recFormError = '';
    addRecordOpen = true;
  }

  async function submitAddRecord(e: SubmitEvent) {
    e.preventDefault();
    if (!selectedZone) return;
    recFormError = '';
    savingRecord = true;
    try {
      const body: Record<string, unknown> = {
        type: fRecType,
        name: fRecName.trim(),
        content: fRecContent.trim(),
        ttl: Number(fRecTtl) || 300,
      };
      if (fRecPriority.trim() !== '') {
        body.priority = Number(fRecPriority);
      }
      await api.post<DNSRecord>(`/dns/zones/${selectedZone.id}/records`, body);
      toast.success(i18n.t.dns.recordCreated);
      addRecordOpen = false;
      await loadRecords(selectedZone.id);
    } catch (err) {
      if (err instanceof ApiError) {
        const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
        recFormError = fieldMsg || err.detail || err.title;
      } else {
        recFormError = i18n.t.common.errorGeneric;
      }
    } finally {
      savingRecord = false;
    }
  }

  async function confirmDeleteRecord() {
    if (!deleteRecordTarget || !selectedZone) return;
    deletingRecord = true;
    try {
      await api.delete(`/dns/records/${deleteRecordTarget.id}`);
      toast.success(i18n.t.dns.recordDeleted);
      deleteRecordTarget = null;
      await loadRecords(selectedZone.id);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      deleteRecordTarget = null;
    } finally {
      deletingRecord = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.dns.title}</h1>
    <p class="page-subtitle">{i18n.t.dns.subtitle}</p>
  </div>
  {#if canWrite}
    <button type="button" class="btn btn-primary" onclick={openCreateZone}>
      + {i18n.t.dns.createZone}
    </button>
  {/if}
</div>

<div class="toolbar">
  <div></div>
  <button type="button" class="btn btn-sm" onclick={loadZones} disabled={loadingZones}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.dns.domain}</th>
        <th>{i18n.t.dns.provider}</th>
        <th>{i18n.t.common.createdAt}</th>
        <th>{i18n.t.common.actions}</th>
      </tr>
    </thead>
    <tbody>
      {#if loadingZones && zones.length === 0}
        <tr>
          <td colspan="4" class="empty-state">{i18n.t.common.loading}</td>
        </tr>
      {:else if zones.length === 0}
        <tr>
          <td colspan="4" class="empty-state">{i18n.t.dns.noZones}</td>
        </tr>
      {:else}
        {#each zones as z (z.id)}
          <tr>
            <td>
              <button type="button" class="link-btn mono" onclick={() => selectZone(z)}>
                {z.domain}
              </button>
            </td>
            <td>
              <span
                class="badge"
                class:badge-primary={z.provider === 'powerdns'}
                class:badge-info={z.provider === 'cloudflare'}
              >
                {z.provider}
              </span>
            </td>
            <td>{formatDateTime(z.created_at, localeCode)}</td>
            <td>
              <div class="table-actions">
                <button type="button" class="btn btn-sm" onclick={() => selectZone(z)}>
                  {i18n.t.dns.manageRecords}
                </button>
                {#if canWrite}
                  <button
                    type="button"
                    class="btn btn-sm btn-danger"
                    onclick={() => (deleteZoneTarget = z)}
                  >
                    {i18n.t.common.delete}
                  </button>
                {/if}
              </div>
            </td>
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>

{#if selectedZone}
  <hr class="section-divider" />
  <div class="toolbar">
    <h2 class="section-heading">
      {fmt(i18n.t.dns.recordsTitle, { domain: selectedZone.domain })}
    </h2>
    <div class="table-actions">
      {#if canWrite}
        <button type="button" class="btn btn-sm btn-primary" onclick={openAddRecord}>
          + {i18n.t.dns.addRecord}
        </button>
      {/if}
      <button type="button" class="btn btn-sm" onclick={() => (selectedZone = null)}>
        {i18n.t.common.close}
      </button>
    </div>
  </div>

  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.dns.recordType}</th>
          <th>{i18n.t.dns.recordName}</th>
          <th>{i18n.t.dns.recordContent}</th>
          <th>TTL</th>
          <th>Priority</th>
          {#if canWrite}
            <th>{i18n.t.common.actions}</th>
          {/if}
        </tr>
      </thead>
      <tbody>
        {#if loadingRecords && records.length === 0}
          <tr>
            <td colspan={canWrite ? 6 : 5} class="empty-state">{i18n.t.common.loading}</td>
          </tr>
        {:else if records.length === 0}
          <tr>
            <td colspan={canWrite ? 6 : 5} class="empty-state">{i18n.t.dns.noRecords}</td>
          </tr>
        {:else}
          {#each records as rec (rec.id)}
            <tr>
              <td><span class="badge badge-primary">{rec.type}</span></td>
              <td class="mono">{rec.name}</td>
              <td class="mono">{rec.content}</td>
              <td>{rec.ttl}</td>
              <td>{rec.priority ?? '—'}</td>
              {#if canWrite}
                <td>
                  <button
                    type="button"
                    class="btn btn-sm btn-danger"
                    onclick={() => (deleteRecordTarget = rec)}
                  >
                    {i18n.t.common.delete}
                  </button>
                </td>
              {/if}
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
{/if}

<Modal open={createZoneOpen} title={i18n.t.dns.createZone} onclose={() => (createZoneOpen = false)}>
  <form onsubmit={submitCreateZone}>
    {#if zoneFormError}
      <div class="alert alert-danger" role="alert">{zoneFormError}</div>
    {/if}
    <div class="form-group">
      <label class="form-label" for="dns-domain">{i18n.t.dns.domain}</label>
      <input
        id="dns-domain"
        class="form-control mono"
        type="text"
        bind:value={fDomain}
        placeholder={i18n.t.dns.domainPlaceholder}
        required
      />
    </div>
    <div class="form-group">
      <label class="form-label" for="dns-provider">{i18n.t.dns.provider}</label>
      <select id="dns-provider" class="form-control" bind:value={fProvider}>
        <option value="powerdns">{i18n.t.dns.providerPowerdns}</option>
        <option value="cloudflare">{i18n.t.dns.providerCloudflare}</option>
      </select>
    </div>
    <div class="modal-footer">
      <button type="button" class="btn" onclick={() => (createZoneOpen = false)}>
        {i18n.t.common.cancel}
      </button>
      <button type="submit" class="btn btn-primary" disabled={savingZone}>
        {savingZone ? i18n.t.common.loading : i18n.t.common.create}
      </button>
    </div>
  </form>
</Modal>

<Modal
  open={deleteZoneTarget !== null}
  title={i18n.t.dns.deleteZoneTitle}
  onclose={() => (deleteZoneTarget = null)}
>
  <p>{fmt(i18n.t.dns.deleteZoneBody, { domain: deleteZoneTarget?.domain ?? '' })}</p>
  <div class="modal-footer">
    <button type="button" class="btn" onclick={() => (deleteZoneTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button type="button" class="btn btn-danger" onclick={confirmDeleteZone} disabled={deletingZone}>
      {deletingZone ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  </div>
</Modal>

<Modal open={addRecordOpen} title={i18n.t.dns.addRecord} onclose={() => (addRecordOpen = false)}>
  <form onsubmit={submitAddRecord}>
    {#if recFormError}
      <div class="alert alert-danger" role="alert">{recFormError}</div>
    {/if}
    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="rec-type">{i18n.t.dns.recordType}</label>
        <select id="rec-type" class="form-control" bind:value={fRecType}>
          {#each RECORD_TYPES as rt (rt)}
            <option value={rt}>{rt}</option>
          {/each}
        </select>
      </div>
      <div class="form-group">
        <label class="form-label" for="rec-name">{i18n.t.dns.recordName}</label>
        <input
          id="rec-name"
          class="form-control mono"
          type="text"
          bind:value={fRecName}
          placeholder={i18n.t.dns.recordNamePlaceholder}
          required
        />
      </div>
    </div>
    <div class="form-group">
      <label class="form-label" for="rec-content">{i18n.t.dns.recordContent}</label>
      <input
        id="rec-content"
        class="form-control mono"
        type="text"
        bind:value={fRecContent}
        placeholder={i18n.t.dns.recordContentPlaceholder}
        required
      />
    </div>
    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="rec-ttl">{i18n.t.dns.recordTtl}</label>
        <input id="rec-ttl" class="form-control" type="number" min="60" bind:value={fRecTtl} />
      </div>
      <div class="form-group">
        <label class="form-label" for="rec-prio">{i18n.t.dns.recordPriority}</label>
        <input id="rec-prio" class="form-control" type="number" min="0" bind:value={fRecPriority} />
      </div>
    </div>
    <div class="modal-footer">
      <button type="button" class="btn" onclick={() => (addRecordOpen = false)}>
        {i18n.t.common.cancel}
      </button>
      <button type="submit" class="btn btn-primary" disabled={savingRecord}>
        {savingRecord ? i18n.t.common.loading : i18n.t.common.create}
      </button>
    </div>
  </form>
</Modal>

<Modal
  open={deleteRecordTarget !== null}
  title={i18n.t.dns.deleteRecordTitle}
  onclose={() => (deleteRecordTarget = null)}
>
  <p>
    {fmt(i18n.t.dns.deleteRecordBody, {
      type: deleteRecordTarget?.type ?? '',
      name: deleteRecordTarget?.name ?? '',
    })}
  </p>
  <div class="modal-footer">
    <button type="button" class="btn" onclick={() => (deleteRecordTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-danger"
      onclick={confirmDeleteRecord}
      disabled={deletingRecord}
    >
      {deletingRecord ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  </div>
</Modal>
