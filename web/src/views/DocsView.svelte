<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import Modal from '../components/Modal.svelte';
  import { api, ApiError } from '../lib/api';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';

  interface OpenApiParameter {
    name: string;
    in: string;
    required?: boolean;
    description?: string;
    schema?: { type?: string };
  }

  interface OpenApiMediaType {
    schema?: unknown;
    example?: unknown;
  }

  interface OpenApiRequestBody {
    required?: boolean;
    description?: string;
    content?: Record<string, OpenApiMediaType>;
  }

  interface OpenApiResponse {
    description?: string;
    content?: Record<string, OpenApiMediaType>;
  }

  interface OpenApiOperation {
    summary?: string;
    description?: string;
    tags?: string[];
    operationId?: string;
    parameters?: OpenApiParameter[];
    requestBody?: OpenApiRequestBody;
    responses?: Record<string, OpenApiResponse>;
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
    parameters: OpenApiParameter[];
    requestExample: unknown;
    hasRequestExample: boolean;
    responseStatus: string;
    responseDescription: string;
    responseExample: unknown;
    hasResponseExample: boolean;
    validationErrorExample: unknown;
    hasValidationErrorExample: boolean;
  }

  const HTTP_METHODS = ['get', 'post', 'put', 'patch', 'delete', 'head', 'options'];
  const SUCCESS_STATUS_CODES = ['200', '201', '202', '204'];

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
  let selectedEndpoint = $state<EndpointRow | null>(null);

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
        const reqEx = op.requestBody?.content?.['application/json']?.example;
        const responses = op.responses ?? {};
        const responseStatus =
          SUCCESS_STATUS_CODES.find((code) => code in responses) ??
          Object.keys(responses).find((code) => code.startsWith('2')) ??
          '';
        const resObj = responseStatus ? responses[responseStatus] : undefined;
        const resEx = resObj?.content?.['application/json']?.example;
        const valEx = responses['422']?.content?.['application/json']?.example;

        rows.push({
          method: lower.toUpperCase(),
          path,
          tag: op.tags && op.tags.length > 0 ? op.tags[0] : 'General',
          summary: op.summary || op.description || op.operationId || '',
          parameters: Array.isArray(op.parameters) ? op.parameters : [],
          requestExample: reqEx,
          hasRequestExample: reqEx !== undefined,
          responseStatus,
          responseDescription: resObj?.description ?? '',
          responseExample: resEx,
          hasResponseExample: resEx !== undefined,
          validationErrorExample: valEx,
          hasValidationErrorExample: valEx !== undefined,
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

  function formatJson(value: unknown): string {
    if (typeof value === 'string') return value;
    return JSON.stringify(value, null, 2);
  }

  function buildCurlExample(row: EndpointRow): string {
    const origin =
      typeof window !== 'undefined' ? window.location.origin : 'https://panel.example.com';
    const lines = [
      `curl -X ${row.method} "${origin}/api/v1${row.path}" \\`,
      `  -H "Authorization: Bearer <API_KEY>"`,
    ];
    if (row.hasRequestExample) {
      lines[lines.length - 1] += ' \\';
      lines.push(`  -H "Content-Type: application/json" \\`);
      lines.push(`  -d '${formatJson(row.requestExample)}'`);
    }
    return lines.join('\n');
  }

  async function copyText(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(i18n.t.common.copied);
    } catch {
      toast.error(i18n.t.common.errorGeneric);
    }
  }

  async function copySpecUrl() {
    const url =
      typeof window !== 'undefined'
        ? `${window.location.origin}/api/v1/openapi.json`
        : '/api/v1/openapi.json';
    await copyText(url);
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
            <th>{i18n.t.common.actions}</th>
          </tr>
        </thead>
        <tbody>
          {#each filteredEndpoints as row (`${row.method}:${row.path}`)}
            <tr onclick={() => (selectedEndpoint = row)}>
              <td><span class={methodBadgeClass(row.method)}>{row.method}</span></td>
              <td class="mono">/api/v1{row.path}</td>
              <td><span class="badge">{row.tag}</span></td>
              <td>{row.summary || '—'}</td>
              <td>
                <div class="table-actions">
                  <button
                    type="button"
                    class="btn btn-sm"
                    title={i18n.t.docs.viewExample}
                    aria-label={i18n.t.docs.viewExample}
                    onclick={(e) => {
                      e.stopPropagation();
                      selectedEndpoint = row;
                    }}
                  >
                    <ActionIcon name="eye" />
                    {i18n.t.docs.viewExample}
                  </button>
                </div>
              </td>
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

<Modal
  open={selectedEndpoint !== null}
  title={selectedEndpoint
    ? fmt(i18n.t.docs.detailTitle, {
        method: selectedEndpoint.method,
        path: `/api/v1${selectedEndpoint.path}`,
      })
    : ''}
  size="lg"
  onclose={() => (selectedEndpoint = null)}
>
  {#if selectedEndpoint}
    <div class="toolbar">
      <div class="toolbar-filters">
        <span class={methodBadgeClass(selectedEndpoint.method)}>{selectedEndpoint.method}</span>
        <code class="mono">/api/v1{selectedEndpoint.path}</code>
        <span class="badge">{selectedEndpoint.tag}</span>
      </div>
    </div>
    {#if selectedEndpoint.summary}
      <p class="page-subtitle">{selectedEndpoint.summary}</p>
    {/if}

    {#if selectedEndpoint.parameters.length > 0}
      <div class="section-divider"></div>
      <h3 class="card-title">{i18n.t.docs.parametersTitle}</h3>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>In</th>
              <th>Required</th>
            </tr>
          </thead>
          <tbody>
            {#each selectedEndpoint.parameters as param (`${param.in}:${param.name}`)}
              <tr>
                <td class="mono">{param.name}</td>
                <td><span class="badge">{param.in}</span></td>
                <td>
                  {#if param.required}
                    <span class="badge badge-warning">required</span>
                  {:else}
                    <span class="badge">optional</span>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

    <div class="section-divider"></div>
    <div class="page-header">
      <h3 class="card-title">{i18n.t.docs.requestExampleTitle}</h3>
      {#if selectedEndpoint.hasRequestExample}
        <button
          type="button"
          class="btn btn-sm"
          onclick={() => copyText(formatJson(selectedEndpoint!.requestExample))}
        >
          <ActionIcon name="copy" />
          {i18n.t.docs.copyBtn}
        </button>
      {/if}
    </div>
    {#if selectedEndpoint.hasRequestExample}
      <pre class="log-box">{formatJson(selectedEndpoint.requestExample)}</pre>
    {:else}
      <p class="page-subtitle">{i18n.t.docs.noRequestBody}</p>
    {/if}

    <div class="section-divider"></div>
    <div class="page-header">
      <h3 class="card-title">{i18n.t.docs.curlExampleTitle}</h3>
      <button
        type="button"
        class="btn btn-sm"
        onclick={() => copyText(buildCurlExample(selectedEndpoint!))}
      >
        <ActionIcon name="copy" />
        {i18n.t.docs.copyBtn}
      </button>
    </div>
    <pre class="log-box">{buildCurlExample(selectedEndpoint)}</pre>

    {#if selectedEndpoint.responseStatus}
      <div class="section-divider"></div>
      <div class="page-header">
        <h3 class="card-title">
          {fmt(i18n.t.docs.responseExampleTitle, { status: selectedEndpoint.responseStatus })}
        </h3>
        {#if selectedEndpoint.hasResponseExample}
          <button
            type="button"
            class="btn btn-sm"
            onclick={() => copyText(formatJson(selectedEndpoint!.responseExample))}
          >
            <ActionIcon name="copy" />
            {i18n.t.docs.copyBtn}
          </button>
        {/if}
      </div>
      {#if selectedEndpoint.hasResponseExample}
        <pre class="log-box">{formatJson(selectedEndpoint.responseExample)}</pre>
      {:else if selectedEndpoint.responseDescription}
        <p class="page-subtitle">{selectedEndpoint.responseDescription}</p>
      {/if}
    {/if}

    {#if selectedEndpoint.hasValidationErrorExample}
      <div class="section-divider"></div>
      <div class="page-header">
        <h3 class="card-title">
          {fmt(i18n.t.docs.responseExampleTitle, { status: '422' })}
        </h3>
        <button
          type="button"
          class="btn btn-sm"
          onclick={() => copyText(formatJson(selectedEndpoint!.validationErrorExample))}
        >
          <ActionIcon name="copy" />
          {i18n.t.docs.copyBtn}
        </button>
      </div>
      <pre class="log-box">{formatJson(selectedEndpoint.validationErrorExample)}</pre>
    {/if}
  {/if}

  {#snippet footer()}
    <button type="button" class="btn btn-sm" onclick={() => (selectedEndpoint = null)}>
      {i18n.t.common.close}
    </button>
  {/snippet}
</Modal>
