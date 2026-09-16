export type User = { id: number; name: string; email: string; created_at: string; updated_at: string };
export type Note = {
	id: number;
	social_identity_id: number | null;
	title: string;
	content_markdown: string;
	source: 'manual' | 'instagram';
	external_message_id: string | null;
	created_at: string;
	updated_at: string;
};
export type NotesPage = { notes: Note[]; total: number; page: number; page_size: number };
export type SocialPlatform = { id: string; name: string; available: boolean; search_enabled: boolean };
export type SocialIdentity = {
	id: number;
	platform: string;
	platform_user_id: string | null;
	username: string;
	normalized_username: string;
	display_name: string | null;
	avatar_url: string | null;
	status: 'pending' | 'active';
	verified_at: string | null;
	created_at: string;
	updated_at: string;
};

const messages: Record<string, string> = {
	authentication_required: 'Please sign in to continue.',
	csrf_failed: 'Your session changed. Refresh the page and try again.',
	email_unavailable: 'An account already uses that email address.',
	identity_unavailable: 'This Instagram identity is unavailable.',
	platform_identity_already_registered: 'An Instagram identity is already registered.',
	invalid_credentials: 'Email or password is incorrect.',
	invalid_input: 'Check the information and try again.',
	invalid_query: 'The requested filters are invalid.',
	note_not_found: 'This note was not found.',
	identity_not_found: 'This social identity was not found.',
	internal_error: 'The service could not complete the request.'
};

export class ApiError extends Error {
	status: number;
	code: string;
	constructor(status: number, code: string) {
		super(messages[code] ?? 'The request could not be completed.');
		this.status = status;
		this.code = code;
	}
}

type Fetch = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

function cookieValue(cookies: string, name: string) {
	const value = cookies.split(';').map((part) => part.trim()).find((part) => part.startsWith(`${name}=`));
	return value ? decodeURIComponent(value.slice(name.length + 1)) : '';
}

export function createApiClient(fetcher: Fetch = fetch, cookies = () => typeof document === 'undefined' ? '' : document.cookie) {
	async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
		const headers = new Headers(init.headers);
		if (init.body) headers.set('Content-Type', 'application/json');
		if (init.method && init.method !== 'GET' && path !== '/api/auth/logout') headers.set('X-CSRF-Token', cookieValue(cookies(), 'csrf_token'));
		const origin = typeof location === 'undefined' ? 'http://localhost' : location.origin;
		const response = await fetcher(new URL(path, origin), { ...init, headers, credentials: 'same-origin' });
		if (!response.ok) {
			const body = await response.json().catch(() => ({})) as { error?: string };
			throw new ApiError(response.status, body.error ?? 'unknown_error');
		}
		return response.status === 204 ? undefined as T : response.json() as Promise<T>;
	}

	return {
		register: (input: { name: string; email: string; password: string }) => request<{ user: User; csrf_token: string }>('/api/auth/register', { method: 'POST', body: JSON.stringify(input) }),
		login: (input: { email: string; password: string }) => request<{ user: User; csrf_token: string }>('/api/auth/login', { method: 'POST', body: JSON.stringify(input) }),
		logout: () => request<void>('/api/auth/logout', { method: 'POST' }),
		profile: async () => (await request<{ user: User }>('/api/profile')).user,
		listNotes: ({ query = '', source = '', sort = 'updated_at', order = 'desc', page = 1, pageSize = 20 } = {}) => {
			const params = new URLSearchParams({ sort, order, page: String(page), page_size: String(pageSize) });
			if (query) params.set('q', query);
			if (source) params.set('source', source);
			return request<NotesPage>(`/api/notes?${params}`);
		},
		getNote: (id: number) => request<Note>(`/api/notes/${id}`),
		createNote: (input: { title: string; content: string }) => request<Note>('/api/notes', { method: 'POST', body: JSON.stringify({ title: input.title, content_markdown: input.content }) }),
		updateNote: (id: number, input: { title: string; content: string }) => request<Note>(`/api/notes/${id}`, { method: 'PUT', body: JSON.stringify({ title: input.title, content_markdown: input.content }) }),
		deleteNote: (id: number) => request<void>(`/api/notes/${id}`, { method: 'DELETE' }),
		platforms: async () => (await request<{ platforms: SocialPlatform[] }>('/api/social-platforms')).platforms,
		identities: async () => (await request<{ identities: SocialIdentity[] }>('/api/social-identities')).identities,
		createSocialIdentity: (platform: string, username: string) => request<SocialIdentity>('/api/social-identities', { method: 'POST', body: JSON.stringify({ platform, username }) }),
		deleteSocialIdentity: (id: number) => request<void>(`/api/social-identities/${id}`, { method: 'DELETE' })
	};
}

export const api = createApiClient();
