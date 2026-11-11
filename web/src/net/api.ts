// The HTTPS API. Requests carry no cookies, no referrer and no cache; every
// response is checked before use, since the server isn't trusted.

/** Error codes the backend returns (backend/internal/api). */
export type ErrorCode =
  | 'bad_request'
  | 'unsupported_media_type'
  | 'init_data_rejected'
  | 'signature_rejected'
  | 'invalid_device_list'
  | 'account_exists'
  | 'conflict'
  | 'rate_limited'
  | 'login_refused'
  | 'account_replaced'
  | 'session_expired'
  | 'invitation_required'
  | 'invalid_link'
  | 'link_limit'
  | 'no_invitations_left'
  | 'invalid_invitation'
  | 'already_member'
  | 'not_member'
  | 'admin_only'
  | 'internal'
  | 'not_found'
  | 'method_not_allowed';

const CODES = new Set<string>([
  'bad_request',
  'unsupported_media_type',
  'init_data_rejected',
  'signature_rejected',
  'invalid_device_list',
  'account_exists',
  'conflict',
  'rate_limited',
  'login_refused',
  'account_replaced',
  'session_expired',
  'invitation_required',
  'invalid_link',
  'link_limit',
  'no_invitations_left',
  'invalid_invitation',
  'already_member',
  'not_member',
  'admin_only',
  'internal',
  'not_found',
  'method_not_allowed',
]);

/** The server answered with an error. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: ErrorCode | 'unknown',
  ) {
    super(`API error ${status} ${code}`);
    this.name = 'ApiError';
  }
}

/** The server couldn't be reached, or answered with something unexpected. */
export class NetworkError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'NetworkError';
  }
}

export type Fetch = (input: string, init: RequestInit) => Promise<Response>;

const TIMEOUT_MS = 15_000;

export type Method = 'GET' | 'POST' | 'PUT' | 'DELETE';

export interface Api {
  /** Sends a request. body is the exact JSON text, since signatures cover it. */
  request<T>(method: Method, path: string, init: { body?: string; headers?: Record<string, string> }, check: (value: unknown) => T): Promise<T>;
  post<T>(path: string, body: unknown, check: (value: unknown) => T): Promise<T>;
}

export function createApi(baseUrl: string, fetchImpl: Fetch = (input, init) => fetch(input, init)): Api {
  const api: Api = {
    post: (path, body, check) => api.request('POST', path, { body: JSON.stringify(body) }, check),
    async request(method, path, init, check) {
      let response: Response;
      try {
        response = await fetchImpl(baseUrl + path, {
          method,
          headers: { 'Content-Type': 'application/json', ...init.headers },
          ...(init.body === undefined ? {} : { body: init.body }),
          credentials: 'omit',
          referrerPolicy: 'no-referrer',
          cache: 'no-store',
          mode: 'cors',
          redirect: 'error',
          signal: AbortSignal.timeout(TIMEOUT_MS),
        });
      } catch {
        throw new NetworkError('request failed');
      }
      let value: unknown;
      try {
        value = await response.json();
      } catch {
        throw new NetworkError(`unreadable response (${response.status})`);
      }
      if (!response.ok) {
        const code = value && typeof value === 'object' ? (value as { error?: unknown }).error : undefined;
        throw new ApiError(response.status, typeof code === 'string' && CODES.has(code) ? (code as ErrorCode) : 'unknown');
      }
      try {
        return check(value);
      } catch {
        throw new NetworkError('unexpected response');
      }
    },
  };
  return api;
}

/** Accepts any JSON object, for responses whose content doesn't matter. */
export function checkObject(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('not an object');
  return value as Record<string, unknown>;
}

/** A session token and when it expires, as /register and /session return. */
export interface TokenResponse {
  account: string;
  token: string;
  /** Unix seconds. */
  expires: number;
}

const ID = /^[A-Za-z0-9_-]{22}$/;

export function checkTokenResponse(value: unknown): TokenResponse {
  if (!value || typeof value !== 'object') throw new Error('not an object');
  const { account, token, expires } = value as Record<string, unknown>;
  if (typeof account !== 'string' || !ID.test(account)) throw new Error('bad account');
  if (typeof token !== 'string' || !/^v1\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(token) || token.length > 512) {
    throw new Error('bad token');
  }
  if (typeof expires !== 'number' || !Number.isSafeInteger(expires) || expires <= 0) throw new Error('bad expiry');
  return { account, token, expires };
}
