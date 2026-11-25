<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatBytes, formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { FileEntry, FileListResponse, FileReadResponse } from '../lib/types';

  let currentPath = $state('');
  let items = $state<FileEntry[]>([]);
  let loading = $state(true);

  let canWrite = $derived(auth.user?.role !== 'readonly');
  let localeCode = $derived(i18n.locale === 'id' ? 'id-ID' : 'en-US');

  let breadcrumbs = $derived.by(() => {
    const clean = currentPath.replace(/^\/+|\/+$/g, '');
    if (!clean) return [];
    const parts = clean.split('/').filter(Boolean);
    return parts.map((name, idx) => ({
      name,
      path: parts.slice(0, idx + 1).join('/'),
    }));
  });

  // New File modal
  let newFileOpen = $state(false);
  let newFilePath = $state('');
  let newFileContent = $state('');
  let newFileError = $state('');
  let creatingFile = $state(false);

  // Edit File modal
  let editFilePath = $state<string | null>(null);
  let editFileContent = $state('');
  let loadingFile = $state(false);
  let savingFile = $state(false);
  let editFileError = $state('');

  // Delete modal
  let deleteTargetPath = $state<string | null>(null);
  let deleting = $state(false);

  function joinPath(base: string, name: string): string {
    const cleanBase = base.replace(/\/+$/, '');
    return cleanBase ? `${cleanBase}/${name}` : name;
  }

  async function loadDir(path = currentPath) {
    loading = true;
    try {
      const qs = path ? `?path=${encodeURIComponent(path)}` : '';
      const res = await api.get<FileListResponse>(`/files${qs}`);
      currentPath = res.path === '/' ? '' : res.path.replace(/^\/+/, '');
      items = res.items;
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    loadDir('');
  });

  function goUp() {
    if (breadcrumbs.length <= 1) {
      loadDir('');
    } else {
      loadDir(breadcrumbs[breadcrumbs.length - 2].path);
    }
  }

  function openNewFile() {
    newFilePath = currentPath ? `${currentPath}/` : '';
    newFileContent = '';
    newFileError = '';
    newFileOpen = true;
  }

  async function submitNewFile(e: SubmitEvent) {
    e.preventDefault();
    newFileError = '';
    creatingFile = true;
    try {
      await api.post<{ ok: boolean }>('/files/write', {
        path: newFilePath.trim(),
        content: newFileContent,
      });
      toast.success(i18n.t.files.savedSuccess);
      newFileOpen = false;
      await loadDir(currentPath);
    } catch (err) {
      if (err instanceof ApiError) {
        const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
        newFileError = fieldMsg || err.detail || err.title;
      } else {
        newFileError = i18n.t.common.errorGeneric;
      }
    } finally {
      creatingFile = false;
    }
  }

  async function openFile(entry: FileEntry) {
    const fullPath = joinPath(currentPath, entry.name);
    if (entry.is_dir) {
      await loadDir(fullPath);
      return;
    }
    editFilePath = fullPath;
    editFileContent = '';
    editFileError = '';
    loadingFile = true;
    try {
      const res = await api.get<FileReadResponse>(
        `/files/read?path=${encodeURIComponent(fullPath)}`
      );
      editFileContent = res.content;
    } catch (err) {
      editFileError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      loadingFile = false;
    }
  }

  async function submitEditFile(e: SubmitEvent) {
    e.preventDefault();
    if (!editFilePath) return;
    editFileError = '';
    savingFile = true;
    try {
      await api.post<{ ok: boolean }>('/files/write', {
        path: editFilePath,
        content: editFileContent,
      });
      toast.success(i18n.t.files.savedSuccess);
      editFilePath = null;
      await loadDir(currentPath);
    } catch (err) {
      editFileError = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
    } finally {
      savingFile = false;
    }
  }

  async function confirmDelete() {
    if (!deleteTargetPath) return;
    deleting = true;
    try {
      await api.post<{ ok: boolean }>('/files/delete', { path: deleteTargetPath });
      toast.success(i18n.t.files.deletedSuccess);
      deleteTargetPath = null;
      await loadDir(currentPath);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
      deleteTargetPath = null;
    } finally {
      deleting = false;
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.files.title}</h1>
    <p class="page-subtitle">{i18n.t.files.subtitle}</p>
  </div>
  {#if canWrite}
    <button type="button" class="btn btn-primary" onclick={openNewFile}>
      + {i18n.t.files.newFile}
    </button>
  {/if}
</div>

<div class="toolbar">
  <div class="toolbar-filters">
    {#if breadcrumbs.length > 0}
      <button type="button" class="btn btn-sm" onclick={goUp}>
        ↑ {i18n.t.files.upDir}
      </button>
    {/if}
  </div>
  <button type="button" class="btn btn-sm" onclick={() => loadDir(currentPath)} disabled={loading}>
    {i18n.t.common.refresh}
  </button>
</div>

<nav class="breadcrumb-bar" aria-label="Breadcrumb">
  <button type="button" class="link-btn" onclick={() => loadDir('')}>
    {i18n.t.files.rootLabel}
  </button>
  {#each breadcrumbs as crumb (crumb.path)}
    <span class="breadcrumb-sep">/</span>
    <button type="button" class="link-btn" onclick={() => loadDir(crumb.path)}>
      {crumb.name}
    </button>
  {/each}
</nav>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th>{i18n.t.files.name}</th>
        <th>{i18n.t.files.size}</th>
        <th>{i18n.t.files.mode}</th>
        <th>{i18n.t.files.modTime}</th>
        {#if canWrite}
          <th>{i18n.t.common.actions}</th>
        {/if}
      </tr>
    </thead>
    <tbody>
      {#if loading && items.length === 0}
        <tr>
          <td colspan={canWrite ? 5 : 4} class="empty-state">{i18n.t.common.loading}</td>
        </tr>
      {:else if items.length === 0}
        <tr>
          <td colspan={canWrite ? 5 : 4} class="empty-state">{i18n.t.files.emptyDir}</td>
        </tr>
      {:else}
        {#each items as entry (entry.name)}
          <tr>
            <td>
              <div class="table-actions">
                {#if entry.is_dir}
                  <span class="badge badge-info">{i18n.t.files.dirBadge}</span>
                {/if}
                <button type="button" class="link-btn mono" onclick={() => openFile(entry)}>
                  {entry.name}
                </button>
              </div>
            </td>
            <td>{entry.is_dir ? '—' : formatBytes(entry.size)}</td>
            <td class="mono">{entry.mode}</td>
            <td>{formatDateTime(entry.mod_time, localeCode)}</td>
            {#if canWrite}
              <td>
                <button
                  type="button"
                  class="btn btn-sm btn-danger"
                  onclick={() => (deleteTargetPath = joinPath(currentPath, entry.name))}
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

<Modal
  open={newFileOpen}
  title={i18n.t.files.newFileTitle}
  size="lg"
  onclose={() => (newFileOpen = false)}
>
  <form onsubmit={submitNewFile}>
    {#if newFileError}
      <div class="alert alert-danger" role="alert">{newFileError}</div>
    {/if}
    <div class="form-group">
      <label class="form-label" for="new-file-path">{i18n.t.files.filePath}</label>
      <input
        id="new-file-path"
        class="form-control mono"
        type="text"
        bind:value={newFilePath}
        placeholder={i18n.t.files.filePathPlaceholder}
        required
      />
    </div>
    <div class="form-group">
      <label class="form-label" for="new-file-content">{i18n.t.files.fileContent}</label>
      <textarea
        id="new-file-content"
        class="form-control code-editor"
        bind:value={newFileContent}
      ></textarea>
    </div>
    <div class="modal-footer">
      <button type="button" class="btn" onclick={() => (newFileOpen = false)}>
        {i18n.t.common.cancel}
      </button>
      <button type="submit" class="btn btn-primary" disabled={creatingFile}>
        {creatingFile ? i18n.t.common.loading : i18n.t.common.save}
      </button>
    </div>
  </form>
</Modal>

<Modal
  open={editFilePath !== null}
  title={fmt(i18n.t.files.editFileTitle, { path: editFilePath ?? '' })}
  size="lg"
  onclose={() => (editFilePath = null)}
>
  <form onsubmit={submitEditFile}>
    {#if editFileError}
      <div class="alert alert-danger" role="alert">{editFileError}</div>
    {/if}
    {#if loadingFile}
      <p>{i18n.t.common.loading}</p>
    {:else}
      <div class="form-group">
        <label class="form-label" for="edit-file-content">{i18n.t.files.fileContent}</label>
        <textarea
          id="edit-file-content"
          class="form-control code-editor"
          bind:value={editFileContent}
          readonly={!canWrite}
        ></textarea>
      </div>
    {/if}
    <div class="modal-footer">
      <button type="button" class="btn" onclick={() => (editFilePath = null)}>
        {i18n.t.common.close}
      </button>
      {#if canWrite && !loadingFile}
        <button type="submit" class="btn btn-primary" disabled={savingFile}>
          {savingFile ? i18n.t.common.loading : i18n.t.common.save}
        </button>
      {/if}
    </div>
  </form>
</Modal>

<Modal
  open={deleteTargetPath !== null}
  title={i18n.t.files.deleteTitle}
  onclose={() => (deleteTargetPath = null)}
>
  <p>{fmt(i18n.t.files.deleteBody, { path: deleteTargetPath ?? '' })}</p>
  <div class="modal-footer">
    <button type="button" class="btn" onclick={() => (deleteTargetPath = null)}>
      {i18n.t.common.cancel}
    </button>
    <button type="button" class="btn btn-danger" onclick={confirmDelete} disabled={deleting}>
      {deleting ? i18n.t.common.loading : i18n.t.common.delete}
    </button>
  </div>
</Modal>
