import assert from 'node:assert/strict';
import test from 'node:test';

import { filterNotes, renderMarkdown } from '../src/lib/app-utils.ts';

const notes = [
	{ id: '1', title: 'Release checklist', content: 'Confirm the launch window', source: 'manual', createdAt: '2026-09-12', updatedAt: '2026-09-14' },
	{ id: '2', title: 'Camera notes', content: 'Try the 35mm framing', source: 'instagram', createdAt: '2026-09-13', updatedAt: '2026-09-13' }
] as const;

test('filters by search and source, then sorts newest first', () => {
	assert.deepEqual(filterNotes(notes, 'camera', 'instagram', 'updated-desc').map((note) => note.id), ['2']);
	assert.deepEqual(filterNotes(notes, '', 'all', 'title-asc').map((note) => note.id), ['2', '1']);
});

test('renders basic markdown without allowing raw HTML or unsafe links', () => {
	const html = renderMarkdown('# Hello\n\n**Bold** and [safe](https://example.com)\n\n<script>alert(1)</script> [bad](javascript:alert(1))');
	assert.match(html, /<h1>Hello<\/h1>/);
	assert.match(html, /<strong>Bold<\/strong>/);
	assert.match(html, /href="https:\/\/example.com"/);
	assert.doesNotMatch(html, /<script>|javascript:/);
	assert.match(html, /&lt;script&gt;/);
});

test('editor initializes the bound textarea ref to the child fallback value', async () => {
	const source = await (await import('node:fs/promises')).readFile('src/lib/components/NoteEditor.svelte', 'utf8');
	assert.match(source, /let area = \$state<HTMLTextAreaElement \| null>\(null\)/);
	assert.match(source, /bind:ref=\{area\}/);
});
