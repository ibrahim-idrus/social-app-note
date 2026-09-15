import { browser } from '$app/environment';

export type Note = {
	id: string;
	title: string;
	content: string;
	source: 'manual' | 'instagram';
	createdAt: string;
	updatedAt: string;
};

export type InstagramIdentity = {
	username: string;
	displayName: string;
	avatar: string;
	status: 'pending' | 'active';
	verifiedAt?: string;
};

const seedNotes: Note[] = [
	{ id: 'field-notes', title: 'Field notes: quiet product signals', content: '# Quiet product signals\n\nLook for what people repeat without being prompted.\n\n- Workarounds they maintain\n- Language they borrow\n- Steps they consistently skip', source: 'manual', createdAt: '2026-09-09T10:00:00Z', updatedAt: '2026-09-14T15:30:00Z' },
	{ id: 'camera-kit', title: 'Compact camera kit for city walks', content: 'Try the 35mm framing this weekend. Keep the kit light: one body, one lens, spare battery.', source: 'instagram', createdAt: '2026-09-13T18:20:00Z', updatedAt: '2026-09-13T18:20:00Z' },
	{ id: 'launch-checklist', title: 'September release checklist', content: '## Before release\n\n- Confirm the launch window\n- Recheck empty states\n- Send the support brief\n\n**Owner:** Product team', source: 'manual', createdAt: '2026-09-11T09:10:00Z', updatedAt: '2026-09-12T16:45:00Z' },
	{ id: 'reading-list', title: 'Essays to revisit', content: 'A short reading queue for the train:\n\n- The Shape of Design\n- Working Backwards\n- Notes on attention', source: 'instagram', createdAt: '2026-09-08T20:00:00Z', updatedAt: '2026-09-08T20:00:00Z' },
	{ id: 'interview', title: 'Customer interview prompts', content: 'Ask about the last time they captured an idea from a message. Avoid hypothetical questions.', source: 'manual', createdAt: '2026-09-06T13:00:00Z', updatedAt: '2026-09-07T09:00:00Z' },
	{ id: 'studio', title: 'Studio shelf dimensions', content: 'Main shelf: 180 × 32 cm. Leave clearance for the record player lid.', source: 'instagram', createdAt: '2026-09-04T17:30:00Z', updatedAt: '2026-09-04T17:30:00Z' },
	{ id: 'weekly-review', title: 'Weekly review — September 1', content: '# Weekly review\n\nThe smaller launch scope is holding. Next week, protect two mornings for writing.', source: 'manual', createdAt: '2026-09-01T15:00:00Z', updatedAt: '2026-09-01T15:00:00Z' }
];

const defaults = { name: 'Maya Chen', email: 'maya@example.com', notes: seedNotes, instagram: null as InstagramIdentity | null };
const saved = browser ? localStorage.getItem('notedesk-state') : null;
const initial: typeof defaults = saved ? { ...defaults, ...(JSON.parse(saved) as Partial<typeof defaults>) } : defaults;

export const appState = $state(initial);

export function persist() {
	if (browser) localStorage.setItem('notedesk-state', JSON.stringify(appState));
}

export function saveNote(note: Pick<Note, 'title' | 'content'> & { id?: string }) {
	const now = new Date().toISOString();
	if (note.id) {
		const existing = appState.notes.find((item) => item.id === note.id);
		if (existing) Object.assign(existing, { title: note.title, content: note.content, updatedAt: now });
	} else {
		appState.notes.unshift({ id: crypto.randomUUID(), title: note.title, content: note.content, source: 'manual', createdAt: now, updatedAt: now });
	}
	persist();
}

export function deleteNote(id: string) {
	appState.notes = appState.notes.filter((note) => note.id !== id);
	persist();
}
