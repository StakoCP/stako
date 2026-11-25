<script lang="ts">
  import type { Snippet } from 'svelte';
  import { ApiError } from '../lib/api';
  import { auth } from '../lib/auth.svelte';
  import { fmt, i18n } from '../lib/i18n.svelte';
  import { router } from '../lib/router.svelte';
  import { themeStore } from '../lib/theme.svelte';
  import { toast } from '../lib/toast.svelte';
  import type { UserRole } from '../lib/types';

  interface Props {
    children: Snippet;
  }

  let { children }: Props = $props();
  let mobileOpen = $state(false);
  let exitingImpersonation = $state(false);

  interface NavItem {
    path: string;
    label: () => string;
    activeNames: string[];
    roles: UserRole[];
  }

  const navItems: NavItem[] = [
    {
      path: '/dashboard',
      label: () => i18n.t.nav.dashboard,
      activeNames: ['dashboard'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/users',
      label: () => i18n.t.nav.users,
      activeNames: ['users'],
      roles: ['admin', 'reseller'],
    },
    {
      path: '/packages',
      label: () => i18n.t.nav.packages,
      activeNames: ['packages'],
      roles: ['admin', 'reseller'],
    },
    {
      path: '/domains',
      label: () => i18n.t.nav.domains,
      activeNames: ['domains'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/dns',
      label: () => i18n.t.nav.dns,
      activeNames: ['dns'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/apps',
      label: () => i18n.t.nav.apps,
      activeNames: ['apps'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/git-deploy',
      label: () => i18n.t.nav.gitDeploy,
      activeNames: ['git-deploy'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/docker',
      label: () => i18n.t.nav.docker,
      activeNames: ['docker'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/catalog',
      label: () => i18n.t.nav.catalog,
      activeNames: ['catalog'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/databases',
      label: () => i18n.t.nav.databases,
      activeNames: ['databases'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/services',
      label: () => i18n.t.nav.services,
      activeNames: ['services'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/ftp',
      label: () => i18n.t.nav.ftp,
      activeNames: ['ftp'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/files',
      label: () => i18n.t.nav.files,
      activeNames: ['files'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/cron',
      label: () => i18n.t.nav.cron,
      activeNames: ['cron'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/mail',
      label: () => i18n.t.nav.mail,
      activeNames: ['mail'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/backups',
      label: () => i18n.t.nav.backups,
      activeNames: ['backups'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/webhooks',
      label: () => i18n.t.nav.webhooks,
      activeNames: ['webhooks'],
      roles: ['admin', 'reseller'],
    },
    {
      path: '/ips',
      label: () => i18n.t.nav.ips,
      activeNames: ['ips'],
      roles: ['admin', 'reseller'],
    },
    {
      path: '/firewall',
      label: () => i18n.t.nav.firewall,
      activeNames: ['firewall'],
      roles: ['admin'],
    },
    {
      path: '/security',
      label: () => i18n.t.nav.security,
      activeNames: ['security'],
      roles: ['admin'],
    },
    {
      path: '/monitoring',
      label: () => i18n.t.nav.monitoring,
      activeNames: ['monitoring'],
      roles: ['admin'],
    },
    {
      path: '/nodes',
      label: () => i18n.t.nav.nodes,
      activeNames: ['nodes'],
      roles: ['admin'],
    },
    {
      path: '/configs',
      label: () => i18n.t.nav.configs,
      activeNames: ['configs'],
      roles: ['admin'],
    },
    {
      path: '/terminal',
      label: () => i18n.t.nav.terminal,
      activeNames: ['terminal'],
      roles: ['admin', 'reseller', 'user'],
    },
    {
      path: '/jobs',
      label: () => i18n.t.nav.jobs,
      activeNames: ['jobs', 'job-detail'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
    {
      path: '/audit',
      label: () => i18n.t.nav.audit,
      activeNames: ['audit'],
      roles: ['admin'],
    },
    {
      path: '/account',
      label: () => i18n.t.nav.account,
      activeNames: ['account'],
      roles: ['admin', 'reseller', 'user', 'readonly'],
    },
  ];

  let visibleNav = $derived(
    navItems.filter((item) => auth.user && item.roles.includes(auth.user.role))
  );

  function closeMobile() {
    mobileOpen = false;
  }

  async function handleExitImpersonation() {
    exitingImpersonation = true;
    try {
      await auth.exitImpersonation();
      if (auth.user) {
        toast.success(fmt(i18n.t.impersonation.exited, { username: auth.user.username }));
      }
      router.navigate('/dashboard');
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail || err.title : i18n.t.common.errorGeneric);
    } finally {
      exitingImpersonation = false;
    }
  }
</script>

<div class="app-shell">
  <div
    class="sidebar-overlay"
    class:open={mobileOpen}
    role="presentation"
    onclick={closeMobile}
  ></div>

  <aside class="sidebar" class:open={mobileOpen} aria-label="Sidebar">
    <div class="sidebar-brand">
      <span class="brand-mark">S</span>
      <span>{i18n.t.appName}</span>
    </div>
    <nav class="sidebar-nav">
      {#each visibleNav as item (item.path)}
        <a
          href={`#${item.path}`}
          class="nav-link"
          class:active={item.activeNames.includes(router.current.name)}
          onclick={closeMobile}
        >
          <span>{item.label()}</span>
        </a>
      {/each}
    </nav>
  </aside>

  <div class="main-area">
    {#if auth.impersonator && auth.user}
      <div class="impersonation-banner" role="status">
        <span>
          {fmt(i18n.t.impersonation.banner, {
            username: auth.user.username,
            role: i18n.t.roles[auth.user.role],
          })}
        </span>
        <button
          type="button"
          class="btn btn-sm"
          onclick={handleExitImpersonation}
          disabled={exitingImpersonation}
        >
          {exitingImpersonation ? i18n.t.common.loading : i18n.t.impersonation.exit}
        </button>
      </div>
    {/if}

    <header class="topbar">
      <div class="topbar-left">
        <button
          type="button"
          class="btn btn-sm menu-toggle"
          aria-label="Toggle navigation"
          onclick={() => (mobileOpen = !mobileOpen)}
        >
          ☰
        </button>
      </div>
      <div class="topbar-right">
        {#if auth.user}
          <span>{auth.user.username}</span>
          <span class="badge badge-primary">{i18n.t.roles[auth.user.role]}</span>
        {/if}
        <button
          type="button"
          class="btn btn-sm"
          onclick={() => i18n.toggleLocale()}
          aria-label="Language"
        >
          {i18n.locale.toUpperCase()}
        </button>
        <button
          type="button"
          class="btn btn-sm"
          onclick={() => themeStore.toggle()}
          aria-label={themeStore.current === 'dark' ? i18n.t.common.themeLight : i18n.t.common.themeDark}
        >
          {themeStore.current === 'dark' ? '☀' : '☾'}
        </button>
        <button
          type="button"
          class="btn btn-sm"
          onclick={() => auth.logout()}
        >
          {i18n.t.common.logout}
        </button>
      </div>
    </header>

    <main class="content">
      {@render children()}
    </main>
  </div>
</div>
