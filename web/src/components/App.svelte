<script lang="ts">
  import { onMount } from 'svelte';
  import Layout from './Layout.svelte';
  import ReauthModal from './ReauthModal.svelte';
  import ToastContainer from './ToastContainer.svelte';
  import { auth } from '../lib/auth.svelte';
  import { i18n } from '../lib/i18n.svelte';
  import { router } from '../lib/router.svelte';
  import AccountView from '../views/AccountView.svelte';
  import AppsView from '../views/AppsView.svelte';
  import AuditView from '../views/AuditView.svelte';
  import BackupsView from '../views/BackupsView.svelte';
  import CatalogView from '../views/CatalogView.svelte';
  import CronView from '../views/CronView.svelte';
  import DashboardView from '../views/DashboardView.svelte';
  import DatabasesView from '../views/DatabasesView.svelte';
  import DnsView from '../views/DnsView.svelte';
  import DockerView from '../views/DockerView.svelte';
  import DomainsView from '../views/DomainsView.svelte';
  import FilesView from '../views/FilesView.svelte';
  import FirewallView from '../views/FirewallView.svelte';
  import ForceTwoFactorView from '../views/ForceTwoFactorView.svelte';
  import FtpView from '../views/FtpView.svelte';
  import GitDeployView from '../views/GitDeployView.svelte';
  import IPsView from '../views/IPsView.svelte';
  import JobsView from '../views/JobsView.svelte';
  import LoginView from '../views/LoginView.svelte';
  import LogsView from '../views/LogsView.svelte';
  import MailView from '../views/MailView.svelte';
  import MigrationsView from '../views/MigrationsView.svelte';
  import MonitoringView from '../views/MonitoringView.svelte';
  import NodesView from '../views/NodesView.svelte';
  import PackagesView from '../views/PackagesView.svelte';
  import PluginPageView from '../views/PluginPageView.svelte';
  import PluginsView from '../views/PluginsView.svelte';
  import SecurityView from '../views/SecurityView.svelte';
  import ServicesView from '../views/ServicesView.svelte';
  import StatusPageView from '../views/StatusPageView.svelte';
  import SystemConfigsView from '../views/SystemConfigsView.svelte';
  import SystemServicesView from '../views/SystemServicesView.svelte';
  import TerminalView from '../views/TerminalView.svelte';
  import UsersView from '../views/UsersView.svelte';
  import WebhooksView from '../views/WebhooksView.svelte';
  import WordPressView from '../views/WordPressView.svelte';

  onMount(() => {
    document.documentElement.lang = i18n.locale;
    auth.init();
  });

  $effect(() => {
    if (!auth.initialized) return;
    if (router.current.name === 'status') return;
    if (!auth.user && router.current.name !== 'login') {
      router.navigate('/login');
    } else if (auth.user && auth.twoFactorRequired && router.current.name !== '2fa-setup') {
      router.navigate('/2fa-setup');
    } else if (
      auth.user &&
      !auth.twoFactorRequired &&
      (router.current.name === 'login' || router.current.name === '2fa-setup')
    ) {
      router.navigate('/dashboard');
    }
  });
</script>

{#if router.current.name === 'status'}
  <StatusPageView />
{:else if !auth.initialized}
  <div class="auth-page">
    <p>{i18n.t.common.loading}</p>
  </div>
{:else if !auth.user}
  <LoginView />
{:else if auth.twoFactorRequired}
  <ForceTwoFactorView />
{:else}
  <Layout>
    {#if router.current.name === 'users' && (auth.user.role === 'admin' || auth.user.role === 'reseller')}
      <UsersView />
    {:else if router.current.name === 'packages' && (auth.user.role === 'admin' || auth.user.role === 'reseller')}
      <PackagesView />
    {:else if router.current.name === 'domains'}
      <DomainsView />
    {:else if router.current.name === 'dns'}
      <DnsView />
    {:else if router.current.name === 'apps'}
      <AppsView />
    {:else if router.current.name === 'wordpress'}
      <WordPressView />
    {:else if router.current.name === 'git-deploy'}
      <GitDeployView />
    {:else if router.current.name === 'docker'}
      <DockerView />
    {:else if router.current.name === 'catalog'}
      <CatalogView />
    {:else if router.current.name === 'databases'}
      <DatabasesView />
    {:else if router.current.name === 'services'}
      <ServicesView />
    {:else if router.current.name === 'ftp'}
      <FtpView />
    {:else if router.current.name === 'files'}
      <FilesView />
    {:else if router.current.name === 'cron'}
      <CronView />
    {:else if router.current.name === 'mail'}
      <MailView />
    {:else if router.current.name === 'backups'}
      <BackupsView />
    {:else if router.current.name === 'webhooks' && (auth.user.role === 'admin' || auth.user.role === 'reseller')}
      <WebhooksView />
    {:else if router.current.name === 'ips' && (auth.user.role === 'admin' || auth.user.role === 'reseller')}
      <IPsView />
    {:else if router.current.name === 'firewall' && auth.user.role === 'admin'}
      <FirewallView />
    {:else if router.current.name === 'security' && auth.user.role === 'admin'}
      <SecurityView />
    {:else if router.current.name === 'monitoring' && auth.user.role === 'admin'}
      <MonitoringView />
    {:else if router.current.name === 'logs' && auth.user.role === 'admin'}
      <LogsView />
    {:else if router.current.name === 'nodes' && auth.user.role === 'admin'}
      <NodesView />
    {:else if router.current.name === 'migrations' && auth.user.role === 'admin'}
      <MigrationsView />
    {:else if router.current.name === 'configs' && auth.user.role === 'admin'}
      <SystemConfigsView />
    {:else if router.current.name === 'system-services' && auth.user.role === 'admin'}
      <SystemServicesView />
    {:else if router.current.name === 'plugins' && auth.user.role === 'admin'}
      <PluginsView />
    {:else if router.current.name === 'plugin-page'}
      <PluginPageView slug={router.current.params.slug} />
    {:else if router.current.name === 'terminal' && auth.user.role !== 'readonly'}
      <TerminalView />
    {:else if router.current.name === 'jobs'}
      <JobsView />
    {:else if router.current.name === 'job-detail'}
      <JobsView jobId={router.current.params.id} />
    {:else if router.current.name === 'audit' && auth.user.role === 'admin'}
      <AuditView />
    {:else if router.current.name === 'account'}
      <AccountView />
    {:else}
      <DashboardView />
    {/if}
  </Layout>
{/if}

<ReauthModal />
<ToastContainer />
