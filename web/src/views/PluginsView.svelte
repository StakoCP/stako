<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { PluginManifest } from '../lib/types';

  const SAMPLE_MANIFEST = {
    id: 'community-plausible',
    name: 'Plausible Analytics',
    version: '1.0.0',
    author: 'Stako Community',
    description: 'Privacy-friendly analytics integration with event hooks, embedded dashboard, and one-click catalog app.',
    homepage: 'https://plausible.io',
    hooks: [
      {
        event: 'domain.created',
        url: 'https://analytics.example.com/api/hooks/stako',
        secret: 'change-me-secret',
      },
    ],
    pages: [
      {
        slug: 'plausible-analytics',
        title: 'Plausible Analytics',
        url: 'https://plausible.io/share/example',
        roles: ['admin', 'reseller', 'user'],
      },
    ],
    apps: [
      {
        slug: 'plausible-ce',
        name: 'Plausible Analytics CE',
        type: 'docker',
        category: 'Analytics',
        version: '2.1',
        description: 'Self-hosted lightweight web analytics.',
      },
    ],
  };

  let plugins = $state<PluginManifest[]>([]);
  let loading = $state(true);
  let togglingId = $state<string | null>(null);

  // Install modal
  let installOpen = $state(false);
  let manifestJson = $state('');
  let formError = $state('');
  let installing = $state(false);

  // Delete modal
  let deleteTarget = $state<PluginManifest | null>(null);
  let deleting = $state(false);

  async function loadPlugins() {
    loading = true;
    try {
      const res = await api.get<{ items: PluginManifest[] }>('/plugins');
      plugins = res.items ?? [];
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    loadPlugins();
  });

  function openInstall() {
    manifestJson = '';
    formError = '';
    installOpen = true;
  }

  function loadSample() {
    manifestJson = JSON.stringify(SAMPLE_MANIFEST, null, 2);
    formError = '';
  }

  async function submitInstall(e: SubmitEvent) {
    e.preventDefault();
    formError = '';

    let parsed: unknown;
    try {
      parsed = JSON.parse(manifestJson);
    } catch {
      formError = i18n.t.plugins.invalidJson;
      return;
    }

    installing = true;
    try {
      await api.post<PluginManifest>('/plugins', parsed);
      toast.success(i18n.t.plugins.installedSuccess);
      installOpen = false;
      await loadPlugins();
    } catch (err) {
      if (err instanceof ApiError) {
        const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
        formError = fieldMsg || err.detail || err.title;
      } else {
        formError = i18n.t.common.errorGeneric;
      }
    } finally {
      installing = false;
    }
  }

  async function togglePlugin(plugin: PluginManifest) {
    togglingId = plugin.id;
    try {
      await api.post(`/plugins/${encodeURIComponent(plugin.id)}/toggle`, {
        enabled: !plugin.enabled,
      });
      toast.success(i18n.t.plugins.toggledSuccess);
      await loadPlugins();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      togglingId = null;
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      await api.delete(`/plugins/${encodeURIComponent(deleteTarget.id)}`);
      toast.success(i18n.t.plugins.deletedSuccess);
      deleteTarget = null;
      await loadPlugins();
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
    <h1 class="page-title">{i18n.t.plugins.title}</h1>
    <p class="page-subtitle">{i18n.t.plugins.subtitle}</p>
  </div>
  <button type="button" class="btn btn-primary" onclick={openInstall}>
    + {i18n.t.plugins.installBtn}
  </button>
</div>

<div class="toolbar">
  <div></div>
  <button type="button" class="btn btn-sm" onclick={loadPlugins} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.plugins.name}</th>
        <th>{i18n.t.plugins.version}</th>
        <th>{i18n.t.plugins.author}</th>
        <th>{i18n.t.plugins.capabilities}</th>
        <th>{i18n.t.plugins.status}</th>
        <th>{i18n.t.plugins.actions}</th>
      </tr>
    </thead>
    <tbody>
      {#if loading && plugins.length === 0}
        <tr><td colspan="6" class="empty-state">{i18n.t.common.loading}</td></tr>
      {:else if plugins.length === 0}
        <tr><td colspan="6" class="empty-state">{i18n.t.plugins.emptyState}</td></tr>
      {:else}
        {#each plugins as plugin (plugin.id)}
          <tr>
            <td>
              <div><strong>{plugin.name}</strong> <span class="mono text-muted">({plugin.id})</span></div>
              {#if plugin.description}
                <div class="text-muted text-sm">{plugin.description}</div>
              {/if}
            </td>
            <td class="mono">{plugin.version}</td>
            <td>
              {#if plugin.homepage}
                <a href={plugin.homepage} target="_blank" rel="noopener noreferrer">{plugin.author || plugin.homepage}</a>
              {:else}
                {plugin.author || '-'}
              {/if}
            </td>
            <td>
              <div class="table-actions">
                <span class="badge badge-info">{fmt(i18n.t.plugins.hooksCount, { count: plugin.hooks?.length ?? 0 })}</span>
                <span class="badge badge-info">{fmt(i18n.t.plugins.pagesCount, { count: plugin.pages?.length ?? 0 })}</span>
                <span class="badge badge-info">{fmt(i18n.t.plugins.appsCount, { count: plugin.apps?.length ?? 0 })}</span>
              </div>
            </td>
            <td>
              {#if plugin.enabled}
                <span class="badge badge-success">{i18n.t.plugins.enabled}</span>
              {:else}
                <span class="badge badge-warning">{i18n.t.plugins.disabled}</span>
              {/if}
            </td>
            <td>
              <div class="table-actions">
                <button
                  type="button"
                  class="btn btn-sm"
                  disabled={togglingId === plugin.id}
                  onclick={() => togglePlugin(plugin)}
                >
                  {plugin.enabled ? i18n.t.plugins.disable : i18n.t.plugins.enable}
                </button>
                <button
                  type="button"
                  class="btn btn-sm btn-icon btn-danger"
                  title={i18n.t.plugins.uninstall}
                  aria-label={i18n.t.plugins.uninstall}
                  onclick={() => (deleteTarget = plugin)}
                >
                  <ActionIcon name="delete" />
                </button>
              </div>
            </td>
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>

<Modal
  open={installOpen}
  size="lg"
  title={i18n.t.plugins.installBtn}
  onclose={() => (installOpen = false)}
>
  <form id="plugin-install-form" onsubmit={submitInstall}>
    {#if formError}
      <div class="alert alert-danger" role="alert">{formError}</div>
    {/if}

    <div class="toolbar">
      <label class="form-label" for="plugin-manifest-json">{i18n.t.plugins.manifestLabel}</label>
      <button type="button" class="btn btn-sm" onclick={loadSample}>
        {i18n.t.plugins.loadSampleBtn}
      </button>
    </div>

    <div class="form-group">
      <textarea
        id="plugin-manifest-json"
        class="form-control mono"
        rows="16"
        bind:value={manifestJson}
        required
        placeholder={i18n.t.plugins.manifestPlaceholder}
      ></textarea>
    </div>
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (installOpen = false)}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="plugin-install-form" class="btn btn-primary" disabled={installing}>
      {installing ? i18n.t.common.loading : i18n.t.plugins.installBtn}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.plugins.uninstallTitle}
  onclose={() => (deleteTarget = null)}
>
  {#if deleteTarget}
    <p>{fmt(i18n.t.plugins.uninstallBody, { name: deleteTarget.name })}</p>
  {/if}

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (deleteTarget = null)}>
      {i18n.t.common.cancel}
    </button>
    <button type="button" class="btn btn-danger" onclick={confirmDelete} disabled={deleting}>
      {deleting ? i18n.t.common.loading : i18n.t.plugins.uninstall}
    </button>
  {/snippet}
</Modal>
