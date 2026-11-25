import type { ProblemDetails, ValidationFieldError } from './types';
import { router } from './router.svelte';

const API_BASE = '/api/v1';

export class ApiError extends Error {
  type: string;
  status: number;
  title: string;
  detail: string;
  errors: ValidationFieldError[];

  constructor(problem: ProblemDetails) {
    super(problem.detail || problem.title || `HTTP ${problem.status}`);
    this.name = 'ApiError';
    this.type = problem.type || 'urn:stako:problem:internal';
    this.status = problem.status;
    this.title = problem.title || 'Error';
    this.detail = problem.detail || '';
    this.errors = problem.errors || [];
  }
}

let csrfToken = '';

export function setCsrfToken(token: string) {
  csrfToken = token;
}

export function getCsrfToken(): string {
  return csrfToken;
}

type ReauthPromptFn = () => Promise<boolean>;
let reauthPrompt: ReauthPromptFn | null = null;

export function registerReauthHandler(fn: ReauthPromptFn) {
  reauthPrompt = fn;
}

let onUnauthenticated: (() => void) | null = null;
export function registerUnauthenticatedHandler(fn: () => void) {
  onUnauthenticated = fn;
}

export interface RequestOptions {
  method?: string;
  body?: unknown;
  skipReauth?: boolean;
  skipAuthRedirect?: boolean;
}

async function parseErrorResponse(res: Response): Promise<ApiError> {
  const ct = res.headers.get('content-type') || '';
  if (ct.includes('application/problem+json') || ct.includes('application/json')) {
    try {
      const data = (await res.json()) as Partial<ProblemDetails>;
      return new ApiError({
        type: data.type || 'urn:stako:problem:internal',
        title: data.title || res.statusText || 'Error',
        status: data.status || res.status,
        detail: data.detail,
        errors: data.errors,
      });
    } catch {
      // fall through
    }
  }
  return new ApiError({
    type: 'urn:stako:problem:internal',
    title: res.statusText || 'Error',
    status: res.status,
    detail: await res.text().catch(() => ''),
    errors: [],
  });
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const method = (opts.method || 'GET').toUpperCase();
  const headers: Record<string, string> = {};

  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }
  if (['POST', 'PATCH', 'PUT', 'DELETE'].includes(method) && csrfToken) {
    headers['X-CSRF-Token'] = csrfToken;
  }

  const res = await fetch(`${API_BASE}${path}`, {
    method,
    credentials: 'same-origin',
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });

  if (res.ok) {
    if (res.status === 204) {
      return undefined as T;
    }
    const text = await res.text();
    if (!text) return undefined as T;
    return JSON.parse(text) as T;
  }

  const err = await parseErrorResponse(res);

  if (err.type === 'urn:stako:problem:reauth-required' && !opts.skipReauth && reauthPrompt) {
    const ok = await reauthPrompt();
    if (ok) {
      return request<T>(path, { ...opts, skipReauth: true });
    }
    throw err;
  }

  if (
    (err.type === 'urn:stako:problem:unauthenticated' || err.status === 401) &&
    err.type !== 'urn:stako:problem:totp-required' &&
    err.type !== 'urn:stako:problem:invalid-credentials' &&
    !opts.skipAuthRedirect
  ) {
    onUnauthenticated?.();
    router.navigate('/login');
  }

  throw err;
}

export const api = {
  get: <T>(path: string, opts?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...opts, method: 'GET' }),
  post: <T>(path: string, body?: unknown, opts?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...opts, method: 'POST', body }),
  patch: <T>(path: string, body?: unknown, opts?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...opts, method: 'PATCH', body }),
  put: <T>(path: string, body?: unknown, opts?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...opts, method: 'PUT', body }),
  delete: <T>(path: string, opts?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...opts, method: 'DELETE' }),
};
