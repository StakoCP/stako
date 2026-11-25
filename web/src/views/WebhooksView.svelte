<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { Webhook } from '../lib/types';

  const AVAILABLE_EVENTS = [
    'account.created',
    'account.suspended',
    'backup.failed',
    'ssl.expiring',
  ] as const;

  let webhooks = $state<Webhook[]>([]);
  let loading = $state(true);
  let localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');

  // Create modal
  let createOpen = $state(false);
  let fUrl = $state('');
  let fEvents = $state<string[]>(['account.created']);
  let fSecret = $state('');
  let formError = $state('');
  let saving = $state(false);

  // One-time secret modal
  let createdSecret = $state<string | null>(null);
  let copiedSecret = $state(false);

  // Delete modal
  let deleteTarget = $state<Webhook | null>(null);
  let deleting = $state(false);

  async function loadWebhooks() {
    loading = true;
    try {
      const res = await api.get<{ items: Webhook[] }>('/webhooks');
      webhooks = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    loadWebhooks();
  });

  function openCreate() {
    fUrl = '';
    fEvents = ['account.created'];
    fSecret = '';
    formError = '';
    createOpen = true;
  }

  function toggleEvent(ev: string) {
    if (fEvents.includes(ev)) {
      fEvents = fEvents.filter((x) => x !== ev);
    } else {
      fEvents = [...fEvents, ev];
    }
  }

  async function submitCreate(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    if (fEvents.length === 0) {
      formError = i18n.t.webhooks.selectAtLeastOne;
      return;
    }
    saving = true;
    try {
      const body: Record<string, unknown> = {
        url: fUrl.trim(),
        events: fEvents,
      };
      if (fSecret.trim()) {
        body.secret = fSecret.trim();
      }

      const res = await api.post<{ webhook: Webhook; secret: string }>('/webhooks', body);
      toast.success(i18n.t.webhooks.createdSuccess);
      createOpen = false;
      createdSecret = res.secret;
      copiedSecret = false;
      await loadWebhooks();
    } catch (err) {
      if (err instanceof ApiError) {
        const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
        formError = fieldMsg || err.detail || err.title;
      } else {
        formError = i18n.t.common.errorGeneric;
      }
    } finally {
      saving = false;
    }
  }

  async function copySecret() {
    if (!createdSecret) return;
    try {
      await navigator.clipboard.writeText(createdSecret);
      copiedSecret = true;
      toast.success(i18n.t.common.copied);
    } catch {
      toast.error(i18n.t.common.errorGeneric);
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      await api.delete(`/webhooks/${deleteTarget.id}`);
      toast.success(i18n.t.webhooks.deletedSuccess);
      deleteTarget = null;
      await loadWebhooks();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      deleteTarget = null;
    } finally {
      deleting = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.webhooks.title}</h1>
    <p class="page-subtitle">{i18n.t.webhooks.subtitle}</p>
  </div>
  <button type="button" class="btn btn-primary" onclick={openCreate}>
    + {i18n.t.webhooks.createWebhook}
  </button>
</div>

<div class="toolbar">
  <div></div>
  <button type="button" class="btn btn-sm" onclick={loadWebhooks} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.webhooks.url}</th>
        <th>{i18n.t.webhooks.events}</th>
        <th>{i18n.t.common.createdAt}</th>
        <th>{i18n.t.common.actions}</th>
      </tr>
    </thead>
    <tbody>
      {#if loading && webhooks.length === 0}
        <tr><td colspan="4" class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if webhooks.length === 0}
        <tr><td colspan="4" class="empty-state">{i18n.t.common.noData}</td></tr>
      {:else}
        {#each webhooks as wh (wh.id)}
          <tr>
            <td class="mono"><strong>{wh.url}</strong></td>
            <td>
              <div class="table-actions">
                {#each wh.events as ev (ev)}
                  <span class="badge badge-info">{ev}</span>
                {/each}
              </div>
            </td>
            <td>{formatDateTime(wh.created_at, localeCode)}</td>
            <td>
              <button
                type="button"
                class="btn btn-sm btn-icon btn-danger"
                title={i18n.t.common.delete}
                aria-label={i18n.t.common.delete}
                onclick={() => (deleteTarget = wh)}
              >
                <ActionIcon name="delete" />
              </button>
            </td>
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>

<Modal open={createOpen} title={i18n.t.webhooks.createWebhook} onclose={() => (createOpen = false)}>
  <form id="webhook-form" onsubmit={submitCreate}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}

    <div class="form-group">
      <label class="form-label" for="wh-url">{i18n.t.webhooks.url}</label>
      <input
        id="wh-url"
        type="url"
        class="form-control"
        bind:value={fUrl}
        required
        placeholder={i18n.t.webhooks.urlPlaceholder}
      />
    </div>

    <div class="form-group">
      <span class="form-label">{i18n.t.webhooks.events}</span>
      <div class="checkbox-group">
        {#each AVAILABLE_EVENTS as ev (ev)}
          <label class="checkbox-row">
            <input
              type="checkbox"
              checked={fEvents.includes(ev)}
              onchange={() => toggleEvent(ev)}
            />
            <span class="mono">{ev}</span>
          </label>
        {/each}
      </div>
    </div>

    <div class="form-group">
      <label class="form-label" for="wh-secret">{i18n.t.webhooks.secretOptional}</label>
      <input
        id="wh-secret"
        type="text"
        class="form-control"
        bind:value={fSecret}
        placeholder={i18n.t.webhooks.secretPlaceholder}
      />
    </div>
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (createOpen = false)}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="webhook-form" class="btn btn-primary" disabled={saving}>
      {saving ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

<Modal
  open={createdSecret !== null}
  title={i18n.t.webhooks.secretOnceTitle}
  onclose={() => (createdSecret = null)}
>
  <p>{i18n.t.webhooks.secretOnceDesc}</p>
  <div class="secret-box">
    <span>{createdSecret}</span>
    <button type="button" class="btn btn-sm" onclick={copySecret}>
      {copiedSecret ? i18n.t.common.copied : i18n.t.common.copy}
    </button>
  </div>

  {#snippet footer()}
    <button type="button" class="btn btn-primary" onclick={() => (createdSecret = null)}>
      {i18n.t.common.close}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.webhooks.deleteTitle}
  onclose={() => (deleteTarget = null)}
>
  <p>{fmt(i18n.t.webhooks.deleteBody, { url: deleteTarget?.url ?? '' })}</p>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (deleteTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button
      type="button"
      class="btn btn-danger"
      onclick={confirmDelete}
      disabled={deleting}
    >
      {deleting ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  {/snippet}
</Modal>
