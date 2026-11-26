<script lang="ts">
  import { onMount } from 'svelte';
  import AceEditor from '../components/AceEditor.svelte';
  import ActionIcon, { type ActionIconName } from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError, getCsrfToken } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { formatBytes, formatDateTime } from '../lib/format';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { FileEntry, FileListResponse, FileReadResponse } from '../lib/types';

  type PreviewKind = 'image' | 'video' | 'audio' | 'pdf' | 'binary';

  interface PreviewState {
    entry: FileEntry;
    path: string;
    kind: PreviewKind;
  }

  interface ClipboardState {
    mode: 'copy' | 'cut';
    path: string;
    name: string;
  }

  const IMAGE_EXTS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'ico', 'bmp']);
  const VIDEO_EXTS = new Set(['mp4', 'webm', 'ogv', 'mov', 'mkv']);
  const AUDIO_EXTS = new Set(['mp3', 'wav', 'ogg', 'flac', 'aac', 'm4a']);
  const ARCHIVE_SUFFIXES = ['.tar.gz', '.tar.bz2', '.tgz', '.tbz2', '.zip', '.tar'];
  const BINARY_EXTS = new Set([
    'exe', 'bin', 'so', 'dll', 'wasm', 'o', 'a', 'class', 'jar', 'pyc',
    'db', 'sqlite', 'sqlite3', 'iso', 'img', 'dmg', 'deb', 'rpm',
    'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx',
    'ttf', 'otf', 'woff', 'woff2', 'eot', 'psd', 'ai',
  ]);

  let currentPath = $state('');
  let items = $state<FileEntry[]>([]);
  let loading = $state(true);
  let uploading = $state(false);
  let uploadInput = $state<HTMLInputElement | null>(null);
  let clipboard = $state<ClipboardState | null>(null);

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

  // New Folder modal
  let newFolderOpen = $state(false);
  let newFolderName = $state('');
  let newFolderError = $state('');
  let creatingFolder = $state(false);

  // Edit File modal (AceEditor)
  let editFilePath = $state<string | null>(null);
  let editFileContent = $state('');
  let loadingFile = $state(false);
  let savingFile = $state(false);
  let editFileError = $state('');

  // Preview modal
  let previewTarget = $state<PreviewState | null>(null);

  // Rename modal
  let renameTarget = $state<{ path: string; name: string } | null>(null);
  let renameNewName = $state('');
  let renameError = $state('');
  let renaming = $state(false);

  // Chmod modal
  let chmodTargetPath = $state<string | null>(null);
  let chmodMode = $state('0644');
  let chmodError = $state('');
  let savingChmod = $state(false);

  // Extract modal
  let extractTargetPath = $state<string | null>(null);
  let extractDstDir = $state('');
  let extractError = $state('');
  let extracting = $state(false);

  // Compress modal
  let compressTargetPath = $state<string | null>(null);
  let compressDst = $state('');
  let compressError = $state('');
  let compressing = $state(false);

  // Delete modal
  let deleteTargetPath = $state<string | null>(null);
  let deleting = $state(false);

  function joinPath(base: string, name: string): string {
    const cleanBase = base.replace(/\/+$/, '');
    return cleanBase ? `${cleanBase}/${name}` : name;
  }

  function getExt(name: string): string {
    const base = name.split('/').pop()?.toLowerCase() ?? '';
    const idx = base.lastIndexOf('.');
    return idx === -1 ? '' : base.slice(idx + 1);
  }

  function isArchive(name: string): boolean {
    const lower = name.toLowerCase();
    return ARCHIVE_SUFFIXES.some((s) => lower.endsWith(s));
  }

  const CODE_EXTS = new Set([
    'js', 'mjs', 'cjs', 'ts', 'tsx', 'jsx', 'json', 'html', 'htm', 'css', 'scss',
    'php', 'py', 'go', 'sh', 'bash', 'yml', 'yaml', 'xml', 'sql', 'svelte', 'astro',
    'conf', 'ini', 'env', 'toml', 'htaccess',
  ]);

  function getEntryIcon(entry: FileEntry): { name: ActionIconName; cls: string } {
    if (entry.is_dir) return { name: 'folder', cls: 'file-icon file-icon-folder' };
    if (isArchive(entry.name)) return { name: 'archive', cls: 'file-icon file-icon-archive' };
    const ext = getExt(entry.name);
    if (IMAGE_EXTS.has(ext)) return { name: 'image', cls: 'file-icon file-icon-media' };
    if (VIDEO_EXTS.has(ext)) return { name: 'video', cls: 'file-icon file-icon-media' };
    if (AUDIO_EXTS.has(ext)) return { name: 'audio', cls: 'file-icon file-icon-media' };
    if (ext === 'pdf') return { name: 'pdf', cls: 'file-icon file-icon-archive' };
    if (CODE_EXTS.has(ext) || entry.name.startsWith('.')) {
      return { name: 'file-code', cls: 'file-icon file-icon-code' };
    }
    return { name: 'file', cls: 'file-icon file-icon-default' };
  }

  function rawUrl(path: string): string {
    return `/api/v1/files/raw?path=${encodeURIComponent(path)}`;
  }

  function downloadUrl(path: string): string {
    return `/api/v1/files/download?path=${encodeURIComponent(path)}`;
  }

  function parseOctalDigits(modeStr: string): [number, number, number] {
    const clean = modeStr.trim();
    const match = clean.match(/^0?([0-7])([0-7])([0-7])$/);
    if (!match) return [6, 4, 4];
    return [Number(match[1]), Number(match[2]), Number(match[3])];
  }

  let permDigits = $derived(parseOctalDigits(chmodMode));

  function togglePermBit(whoIdx: 0 | 1 | 2, bit: 4 | 2 | 1) {
    const next: [number, number, number] = [...permDigits] as [number, number, number];
    next[whoIdx] = next[whoIdx] ^ bit;
    chmodMode = `0${next[0]}${next[1]}${next[2]}`;
  }

  function extractErrorMsg(err: unknown): string {
    if (err instanceof ApiError) {
      const fieldMsg = err.errors.map((x) => `${x.field}: ${x.message}`).join('; ');
      return fieldMsg || err.detail || err.title;
    }
    return i18n.t.common.errorGeneric;
  }

  async function loadDir(path = currentPath) {
    loading = true;
    try {
      const qs = path ? `?path=${encodeURIComponent(path)}` : '';
      const res = await api.get<FileListResponse>(`/files${qs}`);
      currentPath = res.path === '/' ? '' : res.path.replace(/^\/+/, '');
      items = res.items;
    } catch (err) {
      toast.error(extractErrorMsg(err));
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
      newFileError = extractErrorMsg(err);
    } finally {
      creatingFile = false;
    }
  }

  function openNewFolder() {
    newFolderName = '';
    newFolderError = '';
    newFolderOpen = true;
  }

  async function submitNewFolder(e: SubmitEvent) {
    e.preventDefault();
    newFolderError = '';
    const trimmed = newFolderName.trim().replace(/^\/+|\/+$/g, '');
    if (!trimmed) return;
    creatingFolder = true;
    try {
      await api.post<{ ok: boolean }>('/files/mkdir', {
        path: joinPath(currentPath, trimmed),
      });
      toast.success(i18n.t.files.savedSuccess);
      newFolderOpen = false;
      await loadDir(currentPath);
    } catch (err) {
      newFolderError = extractErrorMsg(err);
    } finally {
      creatingFolder = false;
    }
  }

  async function handleUpload(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const files = input.files;
    if (!files || files.length === 0) return;

    uploading = true;
    try {
      const csrf = getCsrfToken();
      for (const file of files) {
        const form = new FormData();
        form.append('path', currentPath || '/');
        form.append('file', file);
        const headers: Record<string, string> = {};
        if (csrf) headers['X-CSRF-Token'] = csrf;
        const res = await fetch('/api/v1/files/upload', {
          method: 'POST',
          credentials: 'same-origin',
          headers,
          body: form,
        });
        if (!res.ok) {
          const data = await res.json().catch(() => null);
          throw new Error(data?.detail || data?.title || res.statusText);
        }
      }
      toast.success(i18n.t.files.uploadSuccess);
      await loadDir(currentPath);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : i18n.t.common.errorGeneric);
    } finally {
      uploading = false;
      input.value = '';
    }
  }

  async function openFile(entry: FileEntry) {
    const fullPath = joinPath(currentPath, entry.name);
    if (entry.is_dir) {
      await loadDir(fullPath);
      return;
    }

    const ext = getExt(entry.name);
    if (IMAGE_EXTS.has(ext)) {
      previewTarget = { entry, path: fullPath, kind: 'image' };
      return;
    }
    if (VIDEO_EXTS.has(ext)) {
      previewTarget = { entry, path: fullPath, kind: 'video' };
      return;
    }
    if (AUDIO_EXTS.has(ext)) {
      previewTarget = { entry, path: fullPath, kind: 'audio' };
      return;
    }
    if (ext === 'pdf') {
      previewTarget = { entry, path: fullPath, kind: 'pdf' };
      return;
    }
    if (isArchive(entry.name)) {
      openExtract(entry);
      return;
    }
    if (BINARY_EXTS.has(ext)) {
      previewTarget = { entry, path: fullPath, kind: 'binary' };
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
      editFileError = extractErrorMsg(err);
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
      editFileError = extractErrorMsg(err);
    } finally {
      savingFile = false;
    }
  }

  function setClipboard(mode: 'copy' | 'cut', entry: FileEntry) {
    clipboard = {
      mode,
      path: joinPath(currentPath, entry.name),
      name: entry.name,
    };
  }

  async function pasteClipboard() {
    if (!clipboard) {
      toast.error(i18n.t.files.clipboardEmpty);
      return;
    }
    const dst = joinPath(currentPath, clipboard.name);
    const endpoint = clipboard.mode === 'copy' ? '/files/copy' : '/files/move';
    try {
      await api.post<{ ok: boolean }>(endpoint, { src: clipboard.path, dst });
      toast.success(i18n.t.files.pastedSuccess);
      if (clipboard.mode === 'cut') {
        clipboard = null;
      }
      await loadDir(currentPath);
    } catch (err) {
      toast.error(extractErrorMsg(err));
    }
  }

  function openRename(entry: FileEntry) {
    renameTarget = { path: joinPath(currentPath, entry.name), name: entry.name };
    renameNewName = entry.name;
    renameError = '';
  }

  async function submitRename(e: SubmitEvent) {
    e.preventDefault();
    if (!renameTarget) return;
    const trimmed = renameNewName.trim();
    if (!trimmed) return;
    renameError = '';
    renaming = true;
    try {
      await api.post<{ ok: boolean }>('/files/move', {
        src: renameTarget.path,
        dst: joinPath(currentPath, trimmed),
      });
      toast.success(i18n.t.files.renamedSuccess);
      renameTarget = null;
      await loadDir(currentPath);
    } catch (err) {
      renameError = extractErrorMsg(err);
    } finally {
      renaming = false;
    }
  }

  function openChmod(entry: FileEntry) {
    chmodTargetPath = joinPath(currentPath, entry.name);
    chmodMode = entry.octal_mode || (entry.is_dir ? '0755' : '0644');
    chmodError = '';
  }

  async function submitChmod(e: SubmitEvent) {
    e.preventDefault();
    if (!chmodTargetPath) return;
    chmodError = '';
    savingChmod = true;
    try {
      await api.post<{ ok: boolean }>('/files/chmod', {
        path: chmodTargetPath,
        mode: chmodMode.trim(),
      });
      toast.success(i18n.t.files.chmodSuccess);
      chmodTargetPath = null;
      await loadDir(currentPath);
    } catch (err) {
      chmodError = extractErrorMsg(err);
    } finally {
      savingChmod = false;
    }
  }

  function openExtract(entry: FileEntry) {
    extractTargetPath = joinPath(currentPath, entry.name);
    extractDstDir = currentPath || '/';
    extractError = '';
  }

  async function submitExtract(e: SubmitEvent) {
    e.preventDefault();
    if (!extractTargetPath) return;
    extractError = '';
    extracting = true;
    try {
      await api.post<{ ok: boolean }>('/files/extract', {
        src: extractTargetPath,
        dst_dir: extractDstDir.trim() || '/',
      });
      toast.success(i18n.t.files.extractedSuccess);
      extractTargetPath = null;
      await loadDir(currentPath);
    } catch (err) {
      extractError = extractErrorMsg(err);
    } finally {
      extracting = false;
    }
  }

  function openCompress(entry: FileEntry) {
    compressTargetPath = joinPath(currentPath, entry.name);
    compressDst = joinPath(currentPath, `${entry.name}.zip`);
    compressError = '';
  }

  async function submitCompress(e: SubmitEvent) {
    e.preventDefault();
    if (!compressTargetPath) return;
    compressError = '';
    compressing = true;
    try {
      await api.post<{ ok: boolean }>('/files/compress', {
        src: compressTargetPath,
        dst: compressDst.trim(),
      });
      toast.success(i18n.t.files.compressedSuccess);
      compressTargetPath = null;
      await loadDir(currentPath);
    } catch (err) {
      compressError = extractErrorMsg(err);
    } finally {
      compressing = false;
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
      toast.error(extractErrorMsg(err));
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
    <div class="table-actions">
      <button type="button" class="btn btn-primary" onclick={openNewFile}>
        <ActionIcon name="plus" />
        {i18n.t.files.newFile}
      </button>
      <button type="button" class="btn" onclick={openNewFolder}>
        <ActionIcon name="folder-plus" />
        {i18n.t.files.newFolder}
      </button>
      <button
        type="button"
        class="btn"
        disabled={uploading}
        onclick={() => uploadInput?.click()}
      >
        <ActionIcon name="upload" />
        {uploading ? i18n.t.files.uploading : i18n.t.files.upload}
      </button>
      <input
        type="file"
        multiple
        bind:this={uploadInput}
        onchange={handleUpload}
        hidden
      />
    </div>
  {/if}
</div>

<div class="toolbar">
  <div class="toolbar-filters">
    {#if breadcrumbs.length > 0}
      <button type="button" class="btn btn-sm" onclick={goUp}>
        ↑ {i18n.t.files.upDir}
      </button>
    {/if}
    {#if canWrite && clipboard}
      <button type="button" class="btn btn-sm btn-primary" onclick={pasteClipboard}>
        <ActionIcon name="clipboard" />
        {i18n.t.files.paste} ({clipboard.name})
      </button>
      <button type="button" class="btn btn-sm" onclick={() => (clipboard = null)}>
        {i18n.t.files.clear}
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
        <th>{i18n.t.common.actions}</th>
      </tr>
    </thead>
    <tbody>
      {#if loading && items.length === 0}
        <tr>
          <td colspan={5} class="empty-state">{i18n.t.common.loading}</td>
        </tr>
      {:else if items.length === 0}
        <tr>
          <td colspan={5} class="empty-state">{i18n.t.files.emptyDir}</td>
        </tr>
      {:else}
        {#each items as entry (entry.name)}
          {@const fullPath = joinPath(currentPath, entry.name)}
          {@const iconInfo = getEntryIcon(entry)}
          <tr>
            <td>
              <button type="button" class="link-btn mono file-entry-btn" onclick={() => openFile(entry)}>
                <span class={iconInfo.cls} aria-hidden="true">
                  <ActionIcon name={iconInfo.name} />
                </span>
                <span>{entry.name}</span>
              </button>
            </td>
            <td>{entry.is_dir ? '—' : formatBytes(entry.size)}</td>
            <td class="mono">{entry.octal_mode ?? entry.mode}</td>
            <td>{formatDateTime(entry.mod_time, localeCode)}</td>
            <td>
              <div class="table-actions">
                {#if !entry.is_dir}
                  <a
                    class="btn btn-sm btn-icon"
                    href={downloadUrl(fullPath)}
                    download={entry.name}
                    title={i18n.t.files.download}
                    aria-label={i18n.t.files.download}
                  >
                    <ActionIcon name="download" />
                  </a>
                {/if}
                {#if canWrite}
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.files.copy}
                    aria-label={i18n.t.files.copy}
                    onclick={() => setClipboard('copy', entry)}
                  >
                    <ActionIcon name="copy" />
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.files.cut}
                    aria-label={i18n.t.files.cut}
                    onclick={() => setClipboard('cut', entry)}
                  >
                    <ActionIcon name="scissors" />
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.files.rename}
                    aria-label={i18n.t.files.rename}
                    onclick={() => openRename(entry)}
                  >
                    <ActionIcon name="edit" />
                  </button>
                  <button
                    type="button"
                    class="btn btn-sm btn-icon"
                    title={i18n.t.files.chmod}
                    aria-label={i18n.t.files.chmod}
                    onclick={() => openChmod(entry)}
                  >
                    <ActionIcon name="shield" />
                  </button>
                  {#if !entry.is_dir && isArchive(entry.name)}
                    <button
                      type="button"
                      class="btn btn-sm btn-icon"
                      title={i18n.t.files.extract}
                      aria-label={i18n.t.files.extract}
                      onclick={() => openExtract(entry)}
                    >
                      <ActionIcon name="archive" />
                    </button>
                  {:else}
                    <button
                      type="button"
                      class="btn btn-sm btn-icon"
                      title={i18n.t.files.compress}
                      aria-label={i18n.t.files.compress}
                      onclick={() => openCompress(entry)}
                    >
                      <ActionIcon name="archive" />
                    </button>
                  {/if}
                  <button
                    type="button"
                    class="btn btn-sm btn-icon btn-danger"
                    title={i18n.t.common.delete}
                    aria-label={i18n.t.common.delete}
                    onclick={() => (deleteTargetPath = fullPath)}
                  >
                    <ActionIcon name="delete" />
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

<!-- New File Modal -->
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
      <span class="form-label">{i18n.t.files.fileContent}</span>
      <AceEditor bind:value={newFileContent} filename={newFilePath} />
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

<!-- New Folder Modal -->
<Modal
  open={newFolderOpen}
  title={i18n.t.files.newFolder}
  onclose={() => (newFolderOpen = false)}
>
  <form onsubmit={submitNewFolder}>
    {#if newFolderError}
      <div class="alert alert-danger" role="alert">{newFolderError}</div>
    {/if}
    <div class="form-group">
      <label class="form-label" for="new-folder-name">{i18n.t.files.folderName}</label>
      <input
        id="new-folder-name"
        class="form-control mono"
        type="text"
        bind:value={newFolderName}
        required
      />
    </div>
    <div class="modal-footer">
      <button type="button" class="btn" onclick={() => (newFolderOpen = false)}>
        {i18n.t.common.cancel}
      </button>
      <button type="submit" class="btn btn-primary" disabled={creatingFolder}>
        {creatingFolder ? i18n.t.common.loading : i18n.t.common.save}
      </button>
    </div>
  </form>
</Modal>

<!-- Edit File Modal (AceEditor) -->
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
        <span class="form-label">{i18n.t.files.fileContent}</span>
        <AceEditor
          bind:value={editFileContent}
          filename={editFilePath ?? ''}
          readOnly={!canWrite}
        />
      </div>
    {/if}
    <div class="modal-footer">
      {#if editFilePath}
        <a
          class="btn"
          href={downloadUrl(editFilePath)}
          download={editFilePath.split('/').pop() ?? 'file'}
        >
          <ActionIcon name="download" />
          {i18n.t.files.download}
        </a>
      {/if}
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

<!-- Preview Modal -->
<Modal
  open={previewTarget !== null}
  title={`${i18n.t.files.preview}: ${previewTarget?.entry.name ?? ''}`}
  size="lg"
  onclose={() => (previewTarget = null)}
>
  {#if previewTarget}
    {#if previewTarget.kind === 'image'}
      <img
        class="media-preview"
        src={rawUrl(previewTarget.path)}
        alt={previewTarget.entry.name}
      />
    {:else if previewTarget.kind === 'video'}
      <video class="media-preview" controls src={rawUrl(previewTarget.path)}>
        <track kind="captions" />
      </video>
    {:else if previewTarget.kind === 'audio'}
      <audio class="audio-preview" controls src={rawUrl(previewTarget.path)}></audio>
    {:else if previewTarget.kind === 'pdf'}
      <iframe
        class="pdf-preview"
        title={previewTarget.entry.name}
        src={rawUrl(previewTarget.path)}
      ></iframe>
    {:else}
      <div class="alert alert-info">{i18n.t.files.binaryNotice}</div>
      <p class="mono">
        {i18n.t.files.size}: {formatBytes(previewTarget.entry.size)} | {i18n.t.files.mode}: {previewTarget.entry.octal_mode ?? previewTarget.entry.mode}
      </p>
    {/if}
  {/if}
  <div class="modal-footer">
    {#if previewTarget}
      <a
        class="btn btn-primary"
        href={downloadUrl(previewTarget.path)}
        download={previewTarget.entry.name}
      >
        <ActionIcon name="download" />
        {i18n.t.files.download}
      </a>
    {/if}
    <button type="button" class="btn" onclick={() => (previewTarget = null)}>
      {i18n.t.common.close}
    </button>
  </div>
</Modal>

<!-- Rename Modal -->
<Modal
  open={renameTarget !== null}
  title={i18n.t.files.rename}
  onclose={() => (renameTarget = null)}
>
  <form onsubmit={submitRename}>
    {#if renameError}
      <div class="alert alert-danger" role="alert">{renameError}</div>
    {/if}
    <div class="form-group">
      <label class="form-label" for="rename-input">{i18n.t.files.newName}</label>
      <input
        id="rename-input"
        class="form-control mono"
        type="text"
        bind:value={renameNewName}
        required
      />
    </div>
    <div class="modal-footer">
      <button type="button" class="btn" onclick={() => (renameTarget = null)}>
        {i18n.t.common.cancel}
      </button>
      <button type="submit" class="btn btn-primary" disabled={renaming}>
        {renaming ? i18n.t.common.loading : i18n.t.common.save}
      </button>
    </div>
  </form>
</Modal>

<!-- Chmod Modal -->
<Modal
  open={chmodTargetPath !== null}
  title={`${i18n.t.files.permissions}: ${chmodTargetPath ?? ''}`}
  onclose={() => (chmodTargetPath = null)}
>
  <form onsubmit={submitChmod}>
    {#if chmodError}
      <div class="alert alert-danger" role="alert">{chmodError}</div>
    {/if}
    <div class="form-group">
      <label class="form-label" for="chmod-octal">{i18n.t.files.mode}</label>
      <input
        id="chmod-octal"
        class="form-control mono"
        type="text"
        bind:value={chmodMode}
        pattern={'0?[0-7]{3}'}
        placeholder="0644"
        required
      />
    </div>
    <div class="perm-grid">
      <span class="perm-grid-header"></span>
      <span class="perm-grid-header">{i18n.t.files.read}</span>
      <span class="perm-grid-header">{i18n.t.files.write}</span>
      <span class="perm-grid-header">{i18n.t.files.execute}</span>

      <span>{i18n.t.files.owner}</span>
      <input type="checkbox" checked={(permDigits[0] & 4) !== 0} onchange={() => togglePermBit(0, 4)} aria-label={`${i18n.t.files.owner} ${i18n.t.files.read}`} />
      <input type="checkbox" checked={(permDigits[0] & 2) !== 0} onchange={() => togglePermBit(0, 2)} aria-label={`${i18n.t.files.owner} ${i18n.t.files.write}`} />
      <input type="checkbox" checked={(permDigits[0] & 1) !== 0} onchange={() => togglePermBit(0, 1)} aria-label={`${i18n.t.files.owner} ${i18n.t.files.execute}`} />

      <span>{i18n.t.files.group}</span>
      <input type="checkbox" checked={(permDigits[1] & 4) !== 0} onchange={() => togglePermBit(1, 4)} aria-label={`${i18n.t.files.group} ${i18n.t.files.read}`} />
      <input type="checkbox" checked={(permDigits[1] & 2) !== 0} onchange={() => togglePermBit(1, 2)} aria-label={`${i18n.t.files.group} ${i18n.t.files.write}`} />
      <input type="checkbox" checked={(permDigits[1] & 1) !== 0} onchange={() => togglePermBit(1, 1)} aria-label={`${i18n.t.files.group} ${i18n.t.files.execute}`} />

      <span>{i18n.t.files.public}</span>
      <input type="checkbox" checked={(permDigits[2] & 4) !== 0} onchange={() => togglePermBit(2, 4)} aria-label={`${i18n.t.files.public} ${i18n.t.files.read}`} />
      <input type="checkbox" checked={(permDigits[2] & 2) !== 0} onchange={() => togglePermBit(2, 2)} aria-label={`${i18n.t.files.public} ${i18n.t.files.write}`} />
      <input type="checkbox" checked={(permDigits[2] & 1) !== 0} onchange={() => togglePermBit(2, 1)} aria-label={`${i18n.t.files.public} ${i18n.t.files.execute}`} />
    </div>
    <div class="modal-footer">
      <button type="button" class="btn" onclick={() => (chmodTargetPath = null)}>
        {i18n.t.common.cancel}
      </button>
      <button type="submit" class="btn btn-primary" disabled={savingChmod}>
        {savingChmod ? i18n.t.common.loading : i18n.t.common.save}
      </button>
    </div>
  </form>
</Modal>

<!-- Extract Modal -->
<Modal
  open={extractTargetPath !== null}
  title={`${i18n.t.files.extract}: ${extractTargetPath ?? ''}`}
  onclose={() => (extractTargetPath = null)}
>
  <form onsubmit={submitExtract}>
    {#if extractError}
      <div class="alert alert-danger" role="alert">{extractError}</div>
    {/if}
    <div class="form-group">
      <label class="form-label" for="extract-dst">{i18n.t.files.targetDir}</label>
      <input
        id="extract-dst"
        class="form-control mono"
        type="text"
        bind:value={extractDstDir}
        required
      />
    </div>
    <div class="modal-footer">
      <button type="button" class="btn" onclick={() => (extractTargetPath = null)}>
        {i18n.t.common.cancel}
      </button>
      <button type="submit" class="btn btn-primary" disabled={extracting}>
        {extracting ? i18n.t.common.loading : i18n.t.files.extract}
      </button>
    </div>
  </form>
</Modal>

<!-- Compress Modal -->
<Modal
  open={compressTargetPath !== null}
  title={`${i18n.t.files.compress}: ${compressTargetPath ?? ''}`}
  onclose={() => (compressTargetPath = null)}
>
  <form onsubmit={submitCompress}>
    {#if compressError}
      <div class="alert alert-danger" role="alert">{compressError}</div>
    {/if}
    <div class="form-group">
      <label class="form-label" for="compress-dst">{i18n.t.files.archiveName}</label>
      <input
        id="compress-dst"
        class="form-control mono"
        type="text"
        bind:value={compressDst}
        required
      />
    </div>
    <div class="modal-footer">
      <button type="button" class="btn" onclick={() => (compressTargetPath = null)}>
        {i18n.t.common.cancel}
      </button>
      <button type="submit" class="btn btn-primary" disabled={compressing}>
        {compressing ? i18n.t.common.loading : i18n.t.files.compress}
      </button>
    </div>
  </form>
</Modal>

<!-- Delete Modal -->
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
