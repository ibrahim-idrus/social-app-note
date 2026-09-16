import type { User } from '$lib/api';

export const appState = $state<{ user: User | null }>({ user: null });
