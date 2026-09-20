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
	const apiSource = await readFile('src/lib/api.ts', 'utf8');
	assert.match(apiSource, /document\.baseURI/);
	assert.match(apiSource, /new URL\(`\.\$\{path\}`/);
	assert.match(source, /<base href=\{`\$\{base\}\/`\}/);
});

test('social capture uses an add-platform flow and hides occupied platforms', async () => {
	const source = await (await import('node:fs/promises')).readFile('src/routes/settings/+page.svelte', 'utf8');
	assert.match(source, />Add platform: \{platform\.name\}</);
	assert.match(source, /availablePlatforms/);
	assert.match(source, /identities\.some\(\(identity\) => identity\.platform === platform\.id\)/);
	assert.doesNotMatch(source, /\(await api\.identities\(\)\)\[0\]/);
});

test('Instagram sender registration posts a username without treating the inbox as a user', async () => {
	const requests: Request[] = [];
	const response = { id: 4, platform: 'instagram', platform_user_id: null, username: 'alice', normalized_username: 'alice', display_name: null, avatar_url: null, status: 'pending', verified_at: null, created_at: '', updated_at: '', verification_code: 'ABC123', verification_expires_at: '2026-09-17T12:10:00Z', instagram_account: { instagram_user_id: 'prototype-inbox', username: 'notedesk_inbox' } };
	const client = createApiClient(async (input, init) => { requests.push(new Request(input, init)); return Response.json(response, { status: 201 }); }, () => 'csrf_token=token');
	await client.createSocialIdentity('instagram', 'alice');
	assert.deepEqual(await requests[0].json(), { platform: 'instagram', username: 'alice' });
});

test('settings separates the dedicated receiving inbox from sender registration', async () => {
	const source = await (await import('node:fs/promises')).readFile('src/routes/settings/+page.svelte', 'utf8');
	assert.match(source, /Dedicated receiving inbox/);
	assert.match(source, /It is not a user account/);
	assert.match(source, /Connect sender account/);
	assert.doesNotMatch(source, /searchSocialIdentities|Is this your account\?|Yes, connect this account|Searching…/);
	assert.match(source, /DM this code/i);
	assert.match(source, /10 minutes/i);
});

test('Instagram verification UI waits for backend state and cleans up bounded polling', async () => {
	const source = await (await import('node:fs/promises')).readFile('src/routes/settings/+page.svelte', 'utf8');
	for (const state of ['waiting', 'active', 'invalid_code', 'expired', 'system_failure']) {
		assert.match(source, new RegExp(`['"]${state}['"]`));
	}
	assert.match(source, /I've sent the code/);
	assert.match(source, /Waiting for Instagram verification/);
	assert.match(source, /Instagram connected/);
	assert.match(source, /verification code has expired/);
	assert.match(source, /setInterval\(pollIdentities, 3000\)/);
	assert.match(source, /if \(polling\) return/);
	assert.match(source, /return stopPolling/);
	assert.match(source, /clearInterval\(pollTimer\)/);
	assert.match(source, /identities = await api\.identities\(\)/);
	const sentBody = source.match(/function sent\([^]*?\n\t}/)?.[0] ?? '';
	assert.doesNotMatch(sentBody, /status|active/);
});

test('social identity API exposes only the verification feedback contract', async () => {
	const source = await (await import('node:fs/promises')).readFile('src/lib/api.ts', 'utf8');
	assert.match(source, /verification_state: 'waiting' \| 'active' \| 'invalid_code' \| 'expired' \| 'system_failure'/);
	assert.match(source, /verification_updated_at: string \| null/);
	assert.doesNotMatch(source, /verification_code_hash|verification_consumed_at/);
});

test('frontend contains no seeded, local-only, or simulated product state', async () => {
	const { readFile } = await import('node:fs/promises');
	const { glob } = await import('node:fs/promises');
	const sources = await Array.fromAsync(glob('src/**/*.{svelte,ts}'), (file) => readFile(file, 'utf8'));
	const source = sources.join('\n');
	assert.doesNotMatch(source, /localStorage|seedNotes|pravatar|Maya Chen|maya@example\.com|demo details|prototype stays|Simulate first DM|Demonstration state|setTimeout/);
});
