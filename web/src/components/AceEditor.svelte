<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import ace from 'ace-builds/src-noconflict/ace';
  import 'ace-builds/css/ace.css';
  import 'ace-builds/css/theme/tomorrow_night.css';
  import 'ace-builds/src-noconflict/mode-javascript';
  import 'ace-builds/src-noconflict/mode-typescript';
  import 'ace-builds/src-noconflict/mode-json';
  import 'ace-builds/src-noconflict/mode-html';
  import 'ace-builds/src-noconflict/mode-css';
  import 'ace-builds/src-noconflict/mode-php';
  import 'ace-builds/src-noconflict/mode-python';
  import 'ace-builds/src-noconflict/mode-golang';
  import 'ace-builds/src-noconflict/mode-sh';
  import 'ace-builds/src-noconflict/mode-yaml';
  import 'ace-builds/src-noconflict/mode-xml';
  import 'ace-builds/src-noconflict/mode-sql';
  import 'ace-builds/src-noconflict/mode-markdown';
  import 'ace-builds/src-noconflict/mode-ini';
  import 'ace-builds/src-noconflict/mode-nginx';
  import 'ace-builds/src-noconflict/mode-apache_conf';
  import 'ace-builds/src-noconflict/mode-text';
  import 'ace-builds/src-noconflict/theme-tomorrow_night';

  interface Props {
    value: string;
    filename?: string;
    readOnly?: boolean;
    onchange?: (val: string) => void;
  }

  let { value = $bindable(''), filename = '', readOnly = false, onchange }: Props = $props();

  let hostEl = $state<HTMLDivElement | null>(null);
  let editor: ReturnType<typeof ace.edit> | null = null;
  let syncingFromProp = false;

  const EXT_MODE_MAP: Record<string, string> = {
    js: 'javascript',
    mjs: 'javascript',
    cjs: 'javascript',
    ts: 'typescript',
    json: 'json',
    html: 'html',
    htm: 'html',
    svelte: 'html',
    astro: 'html',
    css: 'css',
    scss: 'css',
    php: 'php',
    py: 'python',
    go: 'golang',
    sh: 'sh',
    bash: 'sh',
    yml: 'yaml',
    yaml: 'yaml',
    xml: 'xml',
    svg: 'xml',
    sql: 'sql',
    md: 'markdown',
    ini: 'ini',
    env: 'ini',
    toml: 'ini',
    conf: 'nginx',
    htaccess: 'apache_conf',
  };

  function detectMode(name: string): string {
    const base = name.split('/').pop()?.toLowerCase() ?? '';
    if (base === '.htaccess') return 'apache_conf';
    if (base.startsWith('.env')) return 'ini';
    const dotIdx = base.lastIndexOf('.');
    if (dotIdx === -1) return 'text';
    const ext = base.slice(dotIdx + 1);
    return EXT_MODE_MAP[ext] ?? 'text';
  }

  onMount(() => {
    if (!hostEl) return;
    ace.config.set('useStrictCSP', true);
    editor = ace.edit(hostEl);
    editor.setTheme('ace/theme/tomorrow_night');
    editor.session.setMode(`ace/mode/${detectMode(filename)}`);
    editor.setReadOnly(readOnly);
    editor.setOptions({
      fontSize: '13px',
      showPrintMargin: false,
      tabSize: 2,
      useSoftTabs: true,
    });
    syncingFromProp = true;
    editor.setValue(value ?? '', -1);
    syncingFromProp = false;

    editor.session.on('change', () => {
      if (syncingFromProp || !editor) return;
      const next = editor.getValue();
      value = next;
      onchange?.(next);
    });
  });

  $effect(() => {
    if (!editor) return;
    const target = value ?? '';
    if (editor.getValue() !== target) {
      syncingFromProp = true;
      const pos = editor.getCursorPosition();
      editor.setValue(target, -1);
      editor.moveCursorToPosition(pos);
      syncingFromProp = false;
    }
  });

  $effect(() => {
    if (!editor) return;
    editor.setReadOnly(readOnly);
  });

  $effect(() => {
    if (!editor) return;
    editor.session.setMode(`ace/mode/${detectMode(filename)}`);
  });

  onDestroy(() => {
    editor?.destroy();
    editor = null;
  });
</script>

<div class="ace-host" bind:this={hostEl}></div>
