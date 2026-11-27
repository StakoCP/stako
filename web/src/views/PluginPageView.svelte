<script lang="ts">
  import { onMount } from 'svelte';
  import ActionIcon from '../components/ActionIcon.svelte';
  import { api, ApiError } from '../lib/api';
  import { i18n } from '../lib/i18n.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { PluginPageNav } from '../lib/types';

  interface Props {
    slug: string;
  }

  let { slug }: Props = $props();

  let pages = $state<PluginPageNav[]>([]);
  let loading = $state(true);

  let currentPage = $derived(pages.find((p) => p.slug === slug) ?? null);

  async function loadPages() {
    loading = true;
    try {
      const res = await api.get<{ items: PluginPageNav[] }>('/plugins/pages');
      pages = res.items ?? [];
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    loadPages();
  });
</script>

{#if loading && pages.length === 0}
  <div class="card">
    <p class="empty-state">{i18n.t.common.loading}</p>
  </div>
{:else if !currentPage}
  <div class="card">
    <p class="empty-state">{i18n.t.plugins.pageNotFound}</p>
  </div>
{:else}
  <div class="page-header">
    <div>
      <h1 class="page-title">{currentPage.title}</h1>
      <p class="page-subtitle">{currentPage.plugin_name}</p>
    </div>
    <a
      href={currentPage.url}
      target="_blank"
      rel="noopener noreferrer"
      class="btn btn-sm"
    >
      <ActionIcon name="external" />
      <span>{i18n.t.plugins.openExternal}</span>
    </a>
  </div>

  <iframe
    src={currentPage.url}
    title={currentPage.title}
    sandbox="allow-scripts allow-forms allow-same-origin"
    class="plugin-frame"
  ></iframe>
{/if}
