<script lang="ts">
  import { api, ApiError } from '../lib/api';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import type { TerminalExecResponse } from '../lib/types';

  interface HistoryEntry {
    id: number;
    command: string;
    output: string;
    exitCode: number;
    isError?: boolean;
  }

  let commandInput = $state('');
  let running = $state(false);
  let history = $state<HistoryEntry[]>([]);
  let nextId = 1;

  async function runCommand(e: SubmitEvent) {
    e.preventDefault();
    const cmd = commandInput.trim();
    if (!cmd || running) return;

    running = true;
    commandInput = '';
    try {
      const res = await api.post<TerminalExecResponse>('/terminal/exec', { command: cmd });
      history = [
        ...history,
        {
          id: nextId++,
          command: cmd,
          output: res.output || i18n.t.terminal.emptyOutput,
          exitCode: res.exit_code,
        },
      ];
    } catch (err) {
      const msg = err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric;
      history = [
        ...history,
        {
          id: nextId++,
          command: cmd,
          output: msg,
          exitCode: err instanceof ApiError ? err.status : 1,
          isError: true,
        },
      ];
    } finally {
      running = false;
    }
  }

  function clearHistory() {
    history = [];
  }
</script>

<div class="page-header">
  <div>
    <h1 class="page-title">{i18n.t.terminal.title}</h1>
    <p class="page-subtitle">{i18n.t.terminal.subtitle}</p>
  </div>
  <button type="button" class="btn btn-sm" onclick={clearHistory} disabled={history.length === 0}>
    {i18n.t.terminal.clear}
  </button>
</div>

<div class="card">
  <div class="terminal-output" role="log" aria-live="polite">
    {#if history.length === 0}
      <div class="terminal-meta">{i18n.t.terminal.welcome}</div>
    {:else}
      {#each history as item (item.id)}
        <div class="terminal-entry">
          <div class="terminal-cmd">$ {item.command}</div>
          <pre class="terminal-stdout">{item.output}</pre>
          <div class="terminal-meta">
            {fmt(i18n.t.terminal.exitCode, { code: item.exitCode })}
          </div>
        </div>
      {/each}
    {/if}
  </div>

  <form class="terminal-form" onsubmit={runCommand}>
    <span class="terminal-prompt">$</span>
    <input
      class="form-control mono"
      type="text"
      bind:value={commandInput}
      placeholder={i18n.t.terminal.commandPlaceholder}
      disabled={running}
      autocomplete="off"
    />
    <button type="submit" class="btn btn-primary" disabled={running || !commandInput.trim()}>
      {running ? i18n.t.common.loading : i18n.t.terminal.run}
    </button>
  </form>
</div>
