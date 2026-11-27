export type RouteName =
  | 'login'
  | '2fa-setup'
  | 'dashboard'
  | 'users'
  | 'packages'
  | 'domains'
  | 'dns'
  | 'apps'
  | 'git-deploy'
  | 'docker'
  | 'catalog'
  | 'databases'
  | 'services'
  | 'ftp'
  | 'files'
  | 'cron'
  | 'mail'
  | 'backups'
  | 'webhooks'
  | 'ips'
  | 'firewall'
  | 'security'
  | 'monitoring'
  | 'logs'
  | 'nodes'
  | 'migrations'
  | 'configs'
  | 'system-services'
  | 'terminal'
  | 'jobs'
  | 'job-detail'
  | 'audit'
  | 'account';

export interface RouteMatch {
  name: RouteName;
  params: Record<string, string>;
  path: string;
}

function parseHash(): RouteMatch {
  if (typeof window === 'undefined') {
    return { name: 'dashboard', params: {}, path: '/dashboard' };
  }
  const raw = window.location.hash.replace(/^#/, '') || '/dashboard';
  const path = raw.startsWith('/') ? raw : `/${raw}`;

  if (path === '/login') return { name: 'login', params: {}, path };
  if (path === '/2fa-setup') return { name: '2fa-setup', params: {}, path };
  if (path === '/' || path === '/dashboard') return { name: 'dashboard', params: {}, path: '/dashboard' };
  if (path === '/users') return { name: 'users', params: {}, path };
  if (path === '/packages') return { name: 'packages', params: {}, path };
  if (path === '/domains') return { name: 'domains', params: {}, path };
  if (path === '/dns') return { name: 'dns', params: {}, path };
  if (path === '/apps') return { name: 'apps', params: {}, path };
  if (path === '/git-deploy') return { name: 'git-deploy', params: {}, path };
  if (path === '/docker') return { name: 'docker', params: {}, path };
  if (path === '/catalog') return { name: 'catalog', params: {}, path };
  if (path === '/databases') return { name: 'databases', params: {}, path };
  if (path === '/services') return { name: 'services', params: {}, path };
  if (path === '/ftp') return { name: 'ftp', params: {}, path };
  if (path === '/files') return { name: 'files', params: {}, path };
  if (path === '/cron') return { name: 'cron', params: {}, path };
  if (path === '/mail') return { name: 'mail', params: {}, path };
  if (path === '/backups') return { name: 'backups', params: {}, path };
  if (path === '/webhooks') return { name: 'webhooks', params: {}, path };
  if (path === '/ips') return { name: 'ips', params: {}, path };
  if (path === '/firewall') return { name: 'firewall', params: {}, path };
  if (path === '/security') return { name: 'security', params: {}, path };
  if (path === '/monitoring') return { name: 'monitoring', params: {}, path };
  if (path === '/logs') return { name: 'logs', params: {}, path };
  if (path === '/nodes') return { name: 'nodes', params: {}, path };
  if (path === '/migrations') return { name: 'migrations', params: {}, path };
  if (path === '/configs') return { name: 'configs', params: {}, path };
  if (path === '/system-services') return { name: 'system-services', params: {}, path };
  if (path === '/terminal') return { name: 'terminal', params: {}, path };
  if (path === '/jobs') return { name: 'jobs', params: {}, path };
  if (path.startsWith('/jobs/')) {
    const id = decodeURIComponent(path.slice('/jobs/'.length));
    if (id) return { name: 'job-detail', params: { id }, path };
  }
  if (path === '/audit') return { name: 'audit', params: {}, path };
  if (path === '/account') return { name: 'account', params: {}, path };

  return { name: 'dashboard', params: {}, path: '/dashboard' };
}

let currentRoute = $state<RouteMatch>(parseHash());

if (typeof window !== 'undefined') {
  window.addEventListener('hashchange', () => {
    currentRoute = parseHash();
  });
}

export function navigate(path: string) {
  const normalized = path.startsWith('/') ? path : `/${path}`;
  if (typeof window !== 'undefined') {
    if (window.location.hash !== `#${normalized}`) {
      window.location.hash = `#${normalized}`;
    } else {
      currentRoute = parseHash();
    }
  }
}

export const router = {
  get current(): RouteMatch {
    return currentRoute;
  },
  navigate,
};
