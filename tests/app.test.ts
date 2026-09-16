import assert from 'node:assert/strict';
import test from 'node:test';

import { ApiError, createApiClient } from '../src/lib/api.ts';
import { renderMarkdown } from '../src/lib/app-utils.ts';

test('notes search, filtering, sorting, and pagination are sent to the backend', async () => {
	let request: Request | undefined;
	const api = createApiClient(async (input, init) => {
		request = new Request(input, init);
		return Response.json({ notes: [], total: 0, page: 3, page_size: 5 });
	});

	await api.listNotes({ query: 'camera kit', source: 'instagram', sort: 'title', order: 'asc', page: 3, pageSize: 5 });
	const url = new URL(request!.url);
	assert.deepEqual(Object.fromEntries(url.searchParams), { sort: 'title', order: 'asc', page: '3', page_size: '5', q: 'camera kit', source: 'instagram' });
	assert.equal(request?.credentials, 'same-origin');
});

test('mutations send JSON and the CSRF cookie to the same-origin API', async () => {
	let request: Request | undefined;
	const api = createApiClient(async (input, init) => {
		request = new Request(input, init);
		return Response.json({ id: 7, title: 'New', content_markdown: 'Body', source: 'manual', created_at: '', updated_at: '', social_identity_id: null, external_message_id: null }, { status: 201 });
	}, () => 'other=x; csrf_token=token%20value');

	await api.createNote({ title: 'New', content: 'Body' });
	assert.equal(request?.url, 'http://localhost/api/notes');
	assert.equal(request?.method, 'POST');
	assert.equal(request?.headers.get('x-csrf-token'), 'token value');
	assert.deepEqual(await request?.json(), { title: 'New', content_markdown: 'Body' });
});

test('logout is sent without a CSRF dependency so a stale client token cannot keep the session alive', async () => {
	let request: Request | undefined;
	const api = createApiClient(async (input, init) => {
		request = new Request(input, init);
		return new Response(null, { status: 204 });
	}, () => 'csrf_token=stale');

	await api.logout();
	assert.equal(request?.method, 'POST');
	assert.equal(request?.headers.get('x-csrf-token'), null);
});

test('API errors retain status and translate backend error codes', async () => {
	const api = createApiClient(async () => Response.json({ error: 'identity_unavailable' }, { status: 409 }));
	await assert.rejects(api.createSocialIdentity('instagram', 'alice'), (error) => {
		assert.ok(error instanceof ApiError);
		assert.equal(error.status, 409);
		assert.equal(error.message, 'This Instagram identity is unavailable.');
		return true;
	});
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

test('internal navigation uses SvelteKit base-path resolution', async () => {
	const { readFile } = await import('node:fs/promises');
	const { glob } = await import('node:fs/promises');
	const sources = await Array.fromAsync(glob('src/**/*.svelte'), (file) => readFile(file, 'utf8'));
	const source = sources.join('\n');
	assert.doesNotMatch(source, /href=["'`]\/|goto\(["'`]\//);
	assert.match(source, /from '\$app\/paths'/);
	const config = await readFile('svelte.config.js', 'utf8');
	assert.match(config, /fallback: 'index\.html'/);
	assert.match(config, /base: process\.env\.BASE_PATH \?\? ''/);
});

test('social capture uses an add-platform flow and hides occupied platforms', async () => {
	const source = await (await import('node:fs/promises')).readFile('src/routes/settings/+page.svelte', 'utf8');
	assert.match(source, />Add platform: \{platform\.name\}</);
	assert.match(source, /availablePlatforms/);
	assert.match(source, /identities\.some\(\(identity\) => identity\.platform === platform\.id\)/);
	assert.doesNotMatch(source, /\(await api\.identities\(\)\)\[0\]/);
});

test('frontend contains no seeded, local-only, or simulated product state', async () => {
	const { readFile } = await import('node:fs/promises');
	const { glob } = await import('node:fs/promises');
	const sources = await Array.fromAsync(glob('src/**/*.{svelte,ts}'), (file) => readFile(file, 'utf8'));
	const source = sources.join('\n');
	assert.doesNotMatch(source, /localStorage|seedNotes|pravatar|Maya Chen|maya@example\.com|demo details|prototype stays|Simulate first DM|Demonstration state|setTimeout/);
});
