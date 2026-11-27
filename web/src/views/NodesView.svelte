<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { ServerNode, ServerNodeRole } from '../lib/types';

  let nodes = $state<ServerNode[]>([]);
  let loading = $state(true);

  // Add Node modal
  let createOpen = $state(false);
  let fName = $state('');
  let fHost = $state('');
  let fPort = $state(9443);
  let fRole = $state<ServerNodeRole>('worker');
  let formError = $state('');
  let saving = $state(false);

  // One-time agent_token modal
  let createdToken = $state<string | null>(null);
  let copiedToken = $state(false);

  // Delete modal
  let deleteTarget = $state<ServerNode | null>(null);
  let deleting = $state(false);

  function statusBadge(status: string): string {
    const s = status.toLowerCase();
    if (s === 'online' || s === 'connected' || s === 'active') return 'badge-success';
    if (s === 'pending' || s === 'syncing') return 'badge-warning';
    return 'badge-danger';
  }

  async function loadNodes() {
    loading = true;
    try {
      const res = await api.get<{ items: ServerNode[] }>('/nodes');
      nodes = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    loadNodes();
  });

  function openCreate() {
    fName = '';
    fHost = '';
    fPort = 9443;
    fRole = 'worker';
    formError = '';
    createOpen = true;
  }

  async function submitCreate(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    saving = true;
    try {
      const res = await api.post<{ node: ServerNode; agent_token: string }>('/nodes', {
        name: fName.trim(),
        host: fHost.trim(),
        port: Number(fPort) || 9443,
        role: fRole,
      });
      toast.success(i18n.t.nodes.createdSuccess);
      createOpen = false;
      createdToken = res.agent_token;
      copiedToken = false;
      await loadNodes();
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

  async function copyToken() {
    if (!createdToken) return;
    try {
      await navigator.clipboard.writeText(createdToken);
      copiedToken = true;
      toast.success(i18n.t.common.copied);
    } catch {
      toast.error(i18n.t.common.errorGeneric);
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      await api.delete(`/nodes/${deleteTarget.id}`);
      toast.success(i18n.t.nodes.deletedSuccess);
      deleteTarget = null;
      await loadNodes();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      deleting = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.nodes.title}</h1>
    <p class="page-subtitle">{i18n.t.nodes.subtitle}</p>
  </div>
</div>

<div class="card">
  <div class="toolbar">
    <h2 class="card-title">{i18n.t.nodes.sectionNodes}</h2>
    <div class="table-actions">
      <button type="button" class="btn btn-sm" onclick={loadNodes} disabled={loading}>
        {i18n.t.common.refresh}
      </button>
      <button type="button" class="btn btn-sm btn-primary" onclick={openCreate}>
        {i18n.t.nodes.addNode}
      </button>
    </div>
  </div>

  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>{i18n.t.nodes.name}</th>
          <th>{i18n.t.nodes.host}</th>
          <th>{i18n.t.nodes.port}</th>
          <th>{i18n.t.nodes.role}</th>
          <th>{i18n.t.common.status}</th>
          <th>{i18n.t.common.actions}</th>
        </tr>
      </thead>
      <tbody>
        {#if loading && nodes.length === 0}
          <tr><td colspan="6" class="empty-state">{i18n.t.common.loading}</td></tr>
        {:else if nodes.length === 0}
          <tr><td colspan="6" class="empty-state">{i18n.t.common.noData}</td></tr>
        {:else}
          {#each nodes as node (node.id)}
            <tr>
              <td><strong>{node.name}</strong></td>
              <td class="mono">{node.host}</td>
              <td class="mono">{node.port}</td>
              <td><span class="badge badge-info">{node.role}</span></td>
              <td><span class="badge {statusBadge(node.status)}">{node.status}</span></td>
              <td>
                <button
                  type="button"
                  class="btn btn-sm btn-icon btn-danger"
                  title={i18n.t.common.delete}
                  aria-label={i18n.t.common.delete}
                  onclick={() => (deleteTarget = node)}
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
</div>

<Modal
  open={createOpen}
  title={i18n.t.nodes.addNode}
  onclose={() => (createOpen = false)}
>
  {#if formError}
    <div class="alert alert-danger" role="alert">{formError}</div>
  {/if}
  <form id="node-form" onsubmit={submitCreate}>
    <div class="form-group">
      <label class="form-label" for="node-name">{i18n.t.nodes.name}</label>
      <input
        id="node-name"
        type="text"
        class="form-control"
        bind:value={fName}
        required
        placeholder={i18n.t.nodes.namePlaceholder}
      />
    </div>

    <div class="form-group">
      <label class="form-label" for="node-host">{i18n.t.nodes.host}</label>
      <input
        id="node-host"
        type="text"
        class="form-control mono"
        bind:value={fHost}
        required
        placeholder={i18n.t.nodes.hostPlaceholder}
      />
    </div>

    <div class="form-row">
      <div class="form-group">
        <label class="form-label" for="node-port">{i18n.t.nodes.port}</label>
        <input
          id="node-port"
          type="number"
          min="1"
          max="65535"
          class="form-control mono"
          bind:value={fPort}
        />
      </div>

      <div class="form-group">
        <label class="form-label" for="node-role">{i18n.t.nodes.role}</label>
        <select id="node-role" class="form-control" bind:value={fRole}>
          <option value="worker">{i18n.t.nodes.roleWorker}</option>
          <option value="db">{i18n.t.nodes.roleDb}</option>
          <option value="mail">{i18n.t.nodes.roleMail}</option>
        </select>
      </div>
    </div>
  </form>

  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (createOpen = false)}>
      {i18n.t.common.cancel}
    </button>
    <button type="submit" form="node-form" class="btn btn-primary" disabled={saving}>
      {saving ? i18n.t.common.loading : i18n.t.common.create}
    </button>
  {/snippet}
</Modal>

<Modal
  open={createdToken !== null}
  title={i18n.t.nodes.tokenOnceTitle}
  onclose={() => (createdToken = null)}
>
  <p>{i18n.t.nodes.tokenOnceDesc}</p>
  <div class="secret-box">
    <span>{createdToken}</span>
    <button type="button" class="btn btn-sm" onclick={copyToken}>
      {copiedToken ? i18n.t.common.copied : i18n.t.common.copy}
    </button>
  </div>

  {#snippet footer()}
    <button type="button" class="btn btn-primary" onclick={() => (createdToken = null)}>
      {i18n.t.common.close}
    </button>
  {/snippet}
</Modal>

<Modal
  open={deleteTarget !== null}
  title={i18n.t.nodes.deleteTitle}
  onclose={() => (deleteTarget = null)}
>
  <p>
    {fmt(i18n.t.nodes.deleteBody, {
      name: deleteTarget?.name ?? '',
      host: deleteTarget?.host ?? '',
    })}
  </p>
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
