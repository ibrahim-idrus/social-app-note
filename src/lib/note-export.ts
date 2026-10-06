import type { Note } from './api';

const escape = (value: string) => value.replace(/([\\`*_[\]<>#+.!|{}()-])/g, '\\$1');

export function notesToMarkdown(notes: Note[]) {
	return notes.map((note) => [
		`# ${escape(note.title)}`,
		'',
		`- Source: ${note.source[0].toUpperCase()}${note.source.slice(1)}`,
		`- Created: ${note.created_at}`,
		`- Updated: ${note.updated_at}`,
		...(note.tags.length ? [`- Tags: ${note.tags.map((tag) => `#${escape(tag)}`).join(', ')}`] : []),
		'',
		note.content_markdown
	].join('\n')).join('\n\n---\n\n') + '\n';
}
