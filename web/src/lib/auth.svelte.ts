import { api, registerUnauthenticatedHandler, setCsrfToken } from './api';
import { router } from './router.svelte';
import type { AuthMeResponse, ImpersonateResponse, LoginResponse, SystemCapabilities, User } from './types';

const defaultCapabilities: SystemCapabilities = {
  web: true,
  dns: true,
  docker: true,
  databases: true,
  redis_rabbitmq: true,
  ftp: true,
  mail: true,
  firewall: true,
};

let user = $state<User | null>(null);
let impersonator = $state<User | null>(null);
let capabilities = $state<SystemCapabilities>({ ...defaultCapabilities });
let twoFactorRequired = $state(false);
let initialized = $state(false);

registerUnauthenticatedHandler(() => {
  user = null;
  impersonator = null;
  capabilities = { ...defaultCapabilities };
  twoFactorRequired = false;
  setCsrfToken('');
});

export const auth = {
  get user(): User | null {
    return user;
  },
  get impersonator(): User | null {
    return impersonator;
  },
  get capabilities(): SystemCapabilities {
    return capabilities;
  },
  get twoFactorRequired(): boolean {
    return twoFactorRequired;
  },
  get initialized(): boolean {
    return initialized;
  },
  async init(): Promise<void> {
    try {
      const me = await api.get<AuthMeResponse>('/auth/me', { skipAuthRedirect: true });
      user = me.user;
      impersonator = me.impersonator ?? null;
      if (me.capabilities) capabilities = me.capabilities;
      twoFactorRequired = me.two_factor_required;
      setCsrfToken(me.csrf_token);
    } catch {
      user = null;
      impersonator = null;
      capabilities = { ...defaultCapabilities };
      twoFactorRequired = false;
      setCsrfToken('');
    } finally {
      initialized = true;
    }
  },
  async refreshMe(): Promise<void> {
    const me = await api.get<AuthMeResponse>('/auth/me');
    user = me.user;
    impersonator = me.impersonator ?? null;
    if (me.capabilities) capabilities = me.capabilities;
    twoFactorRequired = me.two_factor_required;
    setCsrfToken(me.csrf_token);
  },
  async login(username: string, password: string, totpCode?: string): Promise<void> {
    const body: Record<string, string> = { username, password };
    if (totpCode) body.totp_code = totpCode;
    const res = await api.post<LoginResponse>('/auth/login', body, { skipAuthRedirect: true });
    user = res.user;
    impersonator = null;
    setCsrfToken(res.csrf_token);
    await this.refreshMe();
    if (twoFactorRequired) {
      router.navigate('/2fa-setup');
    } else {
      router.navigate('/dashboard');
    }
  },
  async impersonate(userId: string, reason?: string): Promise<void> {
    const body: Record<string, string> = { user_id: userId };
    if (reason) body.reason = reason;
    const res = await api.post<ImpersonateResponse>('/auth/impersonate', body);
    user = res.user;
    impersonator = res.impersonator ?? null;
    setCsrfToken(res.csrf_token);
  },
  async exitImpersonation(): Promise<void> {
    const res = await api.post<ImpersonateResponse>('/auth/impersonate/exit');
    user = res.user;
    impersonator = res.impersonator ?? null;
    setCsrfToken(res.csrf_token);
  },
  async logout(): Promise<void> {
    try {
      await api.post('/auth/logout', undefined, { skipAuthRedirect: true });
    } catch {
      // ignore errors on logout
    }
    user = null;
    impersonator = null;
    twoFactorRequired = false;
    setCsrfToken('');
    router.navigate('/login');
  },
};
