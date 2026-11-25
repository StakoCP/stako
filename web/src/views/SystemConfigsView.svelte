<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError } from '../lib/api';
  import { formatDateTime } from '../lib/format';
  import { i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type {
    ServiceConfigDetail,
    ServiceConfigMeta,
    ServiceConfigRevision,
  } from '../lib/types';

  interface OutputBanner {
    ok: boolean;
    message: string;
    output: string;
  }

  let services = $state<ServiceConfigMeta[]>([]);
  let selectedService = $state<string>('');
  let detail = $state<ServiceConfigDetail | null>(null);
  let editorContent = $state('');
  let comment = $state('');

  let loadingList = $state(true);
  let loadingDetail = $state(false);
  let saving = $state(false);
  let banner = $state<OutputBanner | null>(null);

  let localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');
  let selectedMeta = $derived(
    services.find((s) => s.service === selectedService) ?? null
  );

  async function loadServices() {
    loadingList = true;
    try {
      const res = await api.get<{ items: ServiceConfigMeta[] }>('/system/configs');
      services = res.items;
      if (services.length > 0 && !selectedService) {
        await selectService(services[0].service);
      }
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingList = false;
    }
  }

  async function selectService(service: string, preserveBanner = false) {
    selectedService = service;
    if (!preserveBanner) {
      banner = null;
      comment = '';
    }
    loadingDetail = true;
    try {
      const res = await api.get<ServiceConfigDetail>(
        `/system/configs/${encodeURIComponent(service)}`
      );
      detail = res;
      editorContent = res.content;
    } catch (err) {
      detail = null;
      editorContent = '';
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loadingDetail = false;
    }
  }

  onMount(() => {
    loadServices();
  });

  async function handleSave(e: SubmitEvent) {
    e.preventDefault();
    if (!selectedService) return;
    saving = true;
    banner = null;
    try {
      const payload: { content: string; comment?: string } = {
        content: editorContent,
      };
      if (comment.trim()) {
        payload.comment = comment.trim();
      }
      const res = await api.put<{ ok: boolean; output: string }>(
        `/system/configs/${encodeURIComponent(selectedService)}`,
        payload
      );
      banner = {
        ok: true,
        message: i18n.t.sysConfigs.savedSuccess,
        output: res.output || '',
      };
      toast.success(i18n.t.sysConfigs.savedSuccess);
      comment = '';
      await selectService(selectedService, true);
    } catch (err) {
      if (err instanceof ApiError) {
        const msg = err.title || i18n.t.sysConfigs.validationFailed;
        banner = {
          ok: false,
          message: msg,
          output: err.detail || '',
        };
        toast.error(msg);
      } else {
        banner = {
          ok: false,
          message: i18n.t.common.errorGeneric,
          output: '',
        };
        toast.error(i18n.t.common.errorGeneric);
      }
    } finally {
      saving = false;
    }
  }

  function loadRevision(rev: ServiceConfigRevision) {
    editorContent = rev.content;
    toast.success(i18n.t.sysConfigs.revisionLoaded);
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.sysConfigs.title}</h1>
    <p class="page-subtitle">{i18n.t.sysConfigs.subtitle}</p>
  </div>
  {#if selectedService}
    <button
      type="button"
      class="btn btn-sm"
      onclick={() => selectService(selectedService)}
      disabled={loadingDetail || saving}
    >
      {i18n.t.sysConfigs.reloadCurrent}
    </button>
  {/if}
</div>

{#if loadingList && services.length === 0}
  <div class="card">
    <p class="empty-state">{i18n.t.common.loading}</p>
  </div>
{:else if services.length === 0}
  <div class="card">
    <p class="empty-state">{i18n.t.common.noData}</p>
  </div>
{:else}
  <div class="tabs sysconfig-tabs" role="tablist" aria-label={i18n.t.sysConfigs.servicesLabel}>
    {#each services as svc (svc.service)}
      <button
        type="button"
        role="tab"
        aria-selected={selectedService === svc.service}
        class="tab-btn"
        class:active={selectedService === svc.service}
        onclick={() => selectService(svc.service)}
        disabled={saving}
      >
        {svc.label}
      </button>
    {/each}
  </div>

  <div class="card">
    <div class="sysconfig-meta">
      <div class="sysconfig-meta-item">
        <span class="sysconfig-meta-label">{i18n.t.sysConfigs.filePath}:</span>
        <code class="mono">{detail?.path || selectedMeta?.path || '—'}</code>
      </div>
      <div class="sysconfig-meta-item">
        <span class="sysconfig-meta-label">{i18n.t.sysConfigs.validationCmd}:</span>
        {#if selectedMeta?.test_cmd}
          <span class="badge badge-info mono">{selectedMeta.test_cmd}</span>
        {:else}
          <span class="badge">{i18n.t.sysConfigs.noValidationCmd}</span>
        {/if}
      </div>
    </div>

    {#if banner}
      <div
        class="alert"
        class:alert-success={banner.ok}
        class:alert-danger={!banner.ok}
        role="status"
      >
        <strong>{banner.message}</strong>
        {#if banner.output.trim()}
          <pre class="sysconfig-output">{banner.output.trim()}</pre>
        {/if}
      </div>
    {/if}

    {#if loadingDetail}
      <p class="empty-state">{i18n.t.common.loading}</p>
    {:else}
      <form onsubmit={handleSave}>
        <div class="form-group">
          <label class="form-label" for="sysconfig-editor">
            {i18n.t.sysConfigs.editorLabel}
          </label>
          <textarea
            id="sysconfig-editor"
            class="form-control code-editor sysconfig-editor"
            bind:value={editorContent}
            rows="18"
            spellcheck="false"
            disabled={saving}
          ></textarea>
        </div>

        <div class="form-group">
          <label class="form-label" for="sysconfig-comment">
            {i18n.t.sysConfigs.commentLabel}
          </label>
          <input
            id="sysconfig-comment"
            type="text"
            class="form-control"
            bind:value={comment}
            placeholder={i18n.t.sysConfigs.commentPlaceholder}
            disabled={saving}
          />
        </div>

        <div class="table-actions">
          <button
            type="submit"
            class="btn btn-primary"
            disabled={saving || !editorContent}
          >
            {saving ? i18n.t.sysConfigs.saving : i18n.t.sysConfigs.saveAndReload}
          </button>
        </div>
      </form>
    {/if}
  </div>

  <div class="card">
    <h2 class="card-title">{i18n.t.sysConfigs.historyTitle}</h2>
    {#if !detail || detail.history.length === 0}
      <p class="empty-state">{i18n.t.sysConfigs.historyEmpty}</p>
    {:else}
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>{i18n.t.sysConfigs.historyTime}</th>
              <th>{i18n.t.sysConfigs.historyAuthor}</th>
              <th>{i18n.t.sysConfigs.historyComment}</th>
              <th>{i18n.t.common.actions}</th>
            </tr>
          </thead>
          <tbody>
            {#each detail.history as rev (rev.id)}
              <tr>
                <td>{formatDateTime(rev.created_at, localeCode)}</td>
                <td class="mono">{rev.created_by}</td>
                <td>{rev.comment || '—'}</td>
                <td>
                  <button
                    type="button"
                    class="btn btn-sm"
                    onclick={() => loadRevision(rev)}
                    disabled={saving}
                  >
                    {i18n.t.sysConfigs.loadRevision}
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </div>
{/if}
