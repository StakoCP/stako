<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError } from '../lib/api';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';

  interface OpenApiOperation {
    summary?: string;
    description?: string;
    tags?: string[];
    operationId?: string;
  }

  interface OpenApiSpec {
    openapi?: string;
    info?: {
      title?: string;
      version?: string;
      description?: string;
    };
    paths?: Record<string, Record<string, OpenApiOperation>>;
  }

  interface EndpointRow {
    method: string;
    path: string;
    tag: string;
    summary: string;
  }

  const HTTP_METHODS = ['get', 'post', 'put', 'patch', 'delete', 'head', 'options'];

  const SAMPLE_AUTH_CURL = `curl -sS -H "Authorization: Bearer stk_your_api_token" \\
  https://panel.example.com/api/v1/system/info`;

  const SAMPLE_CLI = `stako serve --config /etc/stako/stako.toml
stako user list
stako backup run --all
stako doctor`;

  const SAMPLE_PLUGIN_MANIFEST = `{
  "id": "community-example",
  "name": "Example Extension",
  "version": "1.0.0",
  "author": "Community",
  "description": "MIT-licensed Stako plugin manifest",
  "hooks": [{ "event": "domain.created", "url": "https://example.com/hook" }],
  "pages": [{ "slug": "example-page", "title": "Example", "url": "https://example.com/ui", "roles": ["admin"] }],
  "apps": [{ "slug": "example-app", "name": "Example App", "type": "docker", "category": "Tools", "version": "1.0" }]
}`;

  let spec = $state<OpenApiSpec | null>(null);
  let loading = $state(true);
  let loadError = $state('');
  let selectedTag = $state('');
  let searchQuery = $state('');
  let showRaw = $state(false);

  async function loadSpec() {
    loading = true;
    loadError = '';
    try {
      spec = await api.get<OpenApiSpec>('/openapi.json');
    } catch (err) {
      const msg = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
      loadError = msg;
      toast.error(msg);
    } finally {
      loading = false;
    }
  }

  onMount(loadSpec);

  let allEndpoints = $derived.by<EndpointRow[]>(() => {
    if (!spec?.paths) return [];
    const rows: EndpointRow[] = [];
    for (const [path, pathItem] of Object.entries(spec.paths)) {
      if (!pathItem || typeof pathItem !== 'object') continue;
      for (const [methodKey, op] of Object.entries(pathItem)) {
        const lower = methodKey.toLowerCase();
        if (!HTTP_METHODS.includes(lower) || !op || typeof op !== 'object') continue;
        rows.push({
          method: lower.toUpperCase(),
          path,
          tag: op.tags && op.tags.length > 0 ? op.tags[0] : 'General',
          summary: op.summary || op.description || op.operationId || '',
        });
      }
    }
    return rows.sort((a, b) => a.path.localeCompare(b.path) || a.method.localeCompare(b.method));
  });

  let availableTags = $derived.by<string[]>(() => {
    const set = new Set<string>();
    for (const row of allEndpoints) {
      if (row.tag) set.add(row.tag);
    }
    return Array.from(set).sort((a, b) => a.localeCompare(b));
  });

  let filteredEndpoints = $derived.by<EndpointRow[]>(() => {
    const q = searchQuery.trim().toLowerCase();
    return allEndpoints.filter((row) => {
      if (selectedTag && row.tag !== selectedTag) return false;
      if (!q) return true;
      return (
        row.path.toLowerCase().includes(q) ||
        row.method.toLowerCase().includes(q) ||
        row.tag.toLowerCase().includes(q) ||
        row.summary.toLowerCase().includes(q)
      );
    });
  });

  function methodBadgeClass(method: string): string {
    switch (method.toUpperCase()) {
      case 'GET':
        return 'badge badge-success';
      case 'POST':
        return 'badge badge-info';
      case 'PUT':
      case 'PATCH':
        return 'badge badge-warning';
      case 'DELETE':
        return 'badge badge-danger';
      default:
        return 'badge';
    }
  }

  async function copySpecUrl() {
    const url =
      typeof window !== 'undefined'
        ? `${window.location.origin}/api/v1/openapi.json`
        : '/api/v1/openapi.json';
    try {
      await navigator.clipboard.writeText(url);
      toast.success(i18n.t.common.copied);
    } catch {
      toast.error(i18n.t.common.errorGeneric);
    }
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.docs.title}</h1>
    <p class="page-subtitle">{i18n.t.docs.subtitle}</p>
  </div>
  <div class="table-actions">
    {#if spec?.info?.version}
      <span class="badge badge-primary">v{spec.info.version}</span>
    {/if}
    <button type="button" class="btn btn-sm" onclick={loadSpec} disabled={loading}>
      {i18n.t.common.refresh}
    </button>
  </div>
</div>

{#if loadError}
  <div class="alert alert-danger" role="alert">{loadError}</div>
{/if}

<div class="card">
  <div class="page-header">
    <h2 class="card-title">{i18n.t.docs.quickLinksTitle}</h2>
    {#if spec?.openapi}
      <span class="badge badge-info">OpenAPI {spec.openapi}</span>
    {/if}
  </div>
  <div class="toolbar-filters">
    <a
      href="/api/v1/openapi.json"
      target="_blank"
      rel="noopener"
      class="btn btn-primary btn-sm"
    >
      {i18n.t.docs.openJson}
    </a>
    <a
      href="/api/v1/openapi.yaml"
      target="_blank"
      rel="noopener"
      class="btn btn-sm"
    >
      {i18n.t.docs.openYaml}
    </a>
    <a
      href="/api/v1/docs"
      target="_blank"
      rel="noopener"
      class="btn btn-sm"
    >
      {i18n.t.docs.openHtmlDocs}
    </a>
    <button type="button" class="btn btn-sm" onclick={copySpecUrl}>
      {i18n.t.docs.copySpecUrl}
    </button>
  </div>
</div>

<div class="card">
  <h2 class="card-title">{i18n.t.docs.guidesTitle}</h2>
  <div class="grid-two">
    <div class="card">
      <h3 class="card-title">{i18n.t.docs.authGuideTitle}</h3>
      <p class="page-subtitle">{i18n.t.docs.authGuideBody}</p>
      <div class="section-divider"></div>
      <pre class="json-box">{SAMPLE_AUTH_CURL}</pre>
    </div>

    <div class="card">
      <h3 class="card-title">{i18n.t.docs.cliGuideTitle}</h3>
      <p class="page-subtitle">{i18n.t.docs.cliGuideBody}</p>
      <div class="section-divider"></div>
      <pre class="json-box">{SAMPLE_CLI}</pre>
    </div>
  </div>

  <div class="section-divider"></div>
  <h3 class="card-title">{i18n.t.docs.pluginGuideTitle}</h3>
  <p class="page-subtitle">{i18n.t.docs.pluginGuideBody}</p>
  <div class="section-divider"></div>
  <pre class="json-box">{SAMPLE_PLUGIN_MANIFEST}</pre>
</div>

<div class="card">
  <div class="page-header">
    <h2 class="card-title">{i18n.t.docs.explorerTitle}</h2>
    <button type="button" class="btn btn-sm" onclick={() => (showRaw = !showRaw)}>
      {showRaw ? i18n.t.docs.hideRaw : i18n.t.docs.showRaw}
    </button>
  </div>

  <div class="toolbar">
    <div class="toolbar-filters">
      <select
        class="form-control"
        bind:value={selectedTag}
        aria-label={i18n.t.docs.tag}
      >
        <option value="">{i18n.t.docs.allTags}</option>
        {#each availableTags as tag (tag)}
          <option value={tag}>{tag}</option>
        {/each}
      </select>
      <input
        type="text"
        class="form-control"
        placeholder={i18n.t.docs.searchPlaceholder}
        bind:value={searchQuery}
        aria-label={i18n.t.docs.searchPlaceholder}
      />
      <span class="badge badge-primary">
        {fmt(i18n.t.docs.totalEndpoints, { count: filteredEndpoints.length })}
      </span>
    </div>
  </div>

  {#if loading}
    <div class="empty-state">{i18n.t.common.loading}</div>
  {:else if filteredEndpoints.length === 0}
    <div class="empty-state">{i18n.t.docs.noEndpoints}</div>
  {:else}
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{i18n.t.docs.method}</th>
            <th>{i18n.t.docs.endpoint}</th>
            <th>{i18n.t.docs.tag}</th>
            <th>{i18n.t.docs.summary}</th>
          </tr>
        </thead>
        <tbody>
          {#each filteredEndpoints as row (`${row.method}:${row.path}`)}
            <tr>
              <td><span class={methodBadgeClass(row.method)}>{row.method}</span></td>
              <td class="mono">/api/v1{row.path}</td>
              <td><span class="badge">{row.tag}</span></td>
              <td>{row.summary || '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}

  {#if showRaw && spec}
    <div class="section-divider"></div>
    <h3 class="card-title">{i18n.t.docs.rawSpecTitle}</h3>
    <pre class="log-box">{JSON.stringify(spec, null, 2)}</pre>
  {/if}
</div>
