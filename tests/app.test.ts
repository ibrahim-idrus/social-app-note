import assert from 'node:assert/strict';
import test from 'node:test';

import { ApiError, createApiClient } from '../src/lib/api.ts';
import { renderMarkdown, slugHeading } from '../src/lib/app-utils.ts';

test('the root is a landing page and application routes live under /app', async () => {
	const { readFile, glob } = await import('node:fs/promises');
	const landing = await readFile('src/routes/+page.svelte', 'utf8');
	assert.match(landing, /Your social feed, turned into a useful notebook/);
	assert.match(landing, /resolve\('\/app\/register'\)/);
	const sources = (await Array.fromAsync(glob('src/**/*.svelte'), (file) => readFile(file, 'utf8'))).join('\n');
	assert.doesNotMatch(sources, /resolve\('\/(?:dashboard|login|register|notes|settings)/);
});

test('notes search, repeated source/tag filters, sorting, and pagination are sent to the backend', async () => {
	let request: Request | undefined;
	const api = createApiClient(async (input, init) => {
		request = new Request(input, init);
		return Response.json({ notes: [], total: 0, page: 3, page_size: 5, tags: [] });
	});

	await api.listNotes({ query: 'camera kit', searchIn: 'content', sources: ['instagram', 'manual'], tags: ['work', 'ideas'], sort: 'relevance', order: 'desc', page: 3, pageSize: 5 });
	const url = new URL(request!.url);
	assert.deepEqual(url.searchParams.getAll('source'), ['instagram', 'manual']);
	assert.deepEqual(url.searchParams.getAll('tag'), ['work', 'ideas']);
	assert.equal(url.searchParams.get('search_in'), 'content');
	assert.equal(request?.credentials, 'same-origin');
});

test('note detail defers Instagram resolution until the user loads the post', async () => {
	const source = await (await import('node:fs/promises')).readFile('src/routes/app/notes/[id]/+page.svelte', 'utf8');
	const initialLoad = source.match(/async function load\(\)[^]*?\n	}/)?.[0] ?? '';
	assert.doesNotMatch(initialLoad, /resolveInstagramMedia/);
	assert.match(source, /postLoading ?\? 'Loading…' : postError ?\? 'Retry loading post' : 'Load post'/);
	assert.match(source, /async function loadPost/);
	assert.match(source, /note\.source === 'instagram'.*instagram_attachments\.length.*note\.source === 'facebook'.*facebook_attachments\.length/s);
	assert.doesNotMatch(source, /\/https\?:\\\/\\\/\\S\+\//);
});

test('Instagram cached media uses the root API route from a note detail page', () => {
	const previousDocument = globalThis.document;
	Object.defineProperty(globalThis, 'document', { configurable: true, value: { baseURI: 'https://dev-social-notes.ahsanworks.com/notes/46' } });
	try {
		assert.equal(createApiClient().instagramMediaURL(46, 'media image.jfif'), '/api/notes/46/instagram-media/media%20image.jfif');
	} finally {
		Object.defineProperty(globalThis, 'document', { configurable: true, value: previousDocument });
	}
});

test('API requests use the page origin when the deployed document base is relative', async () => {
	const previousDocument = globalThis.document;
	const previousLocation = globalThis.location;
	let request: Request | undefined;
	Object.defineProperty(globalThis, 'document', { configurable: true, value: { baseURI: '/' } });
	Object.defineProperty(globalThis, 'location', { configurable: true, value: { origin: 'https://dev-socialnotes.ahsanworks.com' } });
	try {
		const api = createApiClient(async (input, init) => {
			request = new Request(input, init);
			return Response.json({ notes: [], total: 0, page: 1, page_size: 20 });
		});
		await api.listNotes();
		assert.equal(new URL(request!.url).origin, 'https://dev-socialnotes.ahsanworks.com');
		assert.equal(new URL(request!.url).pathname, '/api/notes');
	} finally {
		Object.defineProperty(globalThis, 'document', { configurable: true, value: previousDocument });
		Object.defineProperty(globalThis, 'location', { configurable: true, value: previousLocation });
	}
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

test('note sharing posts an exact email and supports revocation', async () => {
	const requests: Request[] = [];
	const client = createApiClient(async (input, init) => { requests.push(new Request(input, init)); return requests.length === 1 ? Response.json({ user_id: 2, name: 'Bob', email: 'bob@example.com' }, { status: 201 }) : new Response(null, { status: 204 }); }, () => 'csrf_token=token');
	await client.shareNote(7, 'bob@example.com');
	await client.revokeNoteShare(7, 2);
	assert.equal(requests[0].method, 'POST');
	assert.deepEqual(await requests[0].json(), { email: 'bob@example.com' });
	assert.equal(requests[1].method, 'DELETE');
	assert.equal(new URL(requests[1].url).pathname, '/api/notes/7/shares/2');
});

test('note detail exposes sharing only to owners and shared notes are read-only', async () => {
	const source = await (await import('node:fs/promises')).readFile('src/routes/app/notes/[id]/+page.svelte', 'utf8');
	assert.match(source, /note\.can_edit/);
	assert.match(source, /Share note/);
	assert.match(source, /Shared by.*note\.owner_name/);
	assert.match(source, /api\.shareNote/);
	assert.match(source, /api\.revokeNoteShare/);
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
	assert.match(html, /<h1 id="hello">Hello<\/h1>/);
	assert.match(html, /<strong>Bold<\/strong>/);
	assert.match(html, /href="https:\/\/example.com"/);
	assert.doesNotMatch(html, /<script>|javascript:/);
	assert.match(html, /&lt;script&gt;/);
});

test('heading slugs are stable and duplicates receive suffixes', () => {
	assert.equal(slugHeading('Hello, 東京 World!'), 'hello-東京-world');
	assert.match(renderMarkdown('# Same\n# Same'), /id="same"[^]*id="same-2"/);
});

test('note editor uses Milkdown as one Markdown-backed editing surface', async () => {
	const source = await (await import('node:fs/promises')).readFile('src/lib/components/NoteEditor.svelte', 'utf8');
	assert.match(source, /from '@milkdown\/kit\/core'/);
	assert.match(source, /from '@milkdown\/kit\/preset\/commonmark'/);
	assert.match(source, /listenerCtx/);
	assert.match(source, /getMarkdown/);
	assert.doesNotMatch(source, /<Textarea|>Preview</);
});

test('internal navigation uses SvelteKit base-path resolution without an HTML base override', async () => {
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
	assert.match(apiSource, /location\.origin/);
	assert.match(apiSource, /new URL\(path, base\)/);
	assert.doesNotMatch(source, /<base\b/);
});

test('social capture uses an add-platform flow and hides occupied platforms', async () => {
	const source = await (await import('node:fs/promises')).readFile('src/routes/app/settings/+page.svelte', 'utf8');
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

test('settings instructs users to verify from their own Instagram account', async () => {
	const source = await (await import('node:fs/promises')).readFile('src/routes/app/settings/+page.svelte', 'utf8');
	assert.match(source, /Dedicated Instagram inbox/);
	assert.match(source, /From your own @\{identity\.username\} account/);
	assert.match(source, /Connect sender account/);
	assert.doesNotMatch(source, /searchSocialIdentities|Is this your account\?|Yes, connect this account|Searching…/);
	assert.match(source, /DM this code/i);
	assert.match(source, /10 minutes/i);
});

test('Instagram verification UI waits for backend state and cleans up bounded polling', async () => {
	const source = await (await import('node:fs/promises')).readFile('src/routes/app/settings/+page.svelte', 'utf8');
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

test('Facebook Messenger settings use the configured Page and platform-specific verification states', async () => {
	const source = await (await import('node:fs/promises')).readFile('src/routes/app/settings/+page.svelte', 'utf8');
	assert.match(source, /Facebook Messenger/);
	assert.match(source, /facebook_page/);
	assert.match(source, /page_id/);
	assert.match(source, /10 minutes/i);
	assert.match(source, /Waiting for Facebook Messenger verification/);
	assert.match(source, /Facebook Messenger connected/);
	assert.match(source, /Regenerate code/);
	assert.match(source, /Remove account/);
	assert.match(source, /createSocialIdentity\(platform\.id, username\.trim\(\)\)/);
	assert.match(source, /onclick=\{\(\) => selectedPlatform = platform\}/);
	assert.doesNotMatch(source, /Threads/i);
});

test('Facebook is a note source with a badge and filter while Threads is absent', async () => {
	const { readFile } = await import('node:fs/promises');
	const apiSource = await readFile('src/lib/api.ts', 'utf8');
	const listSource = await readFile('src/routes/app/notes/+page.svelte', 'utf8');
	const detailSource = await readFile('src/routes/app/notes/[id]/+page.svelte', 'utf8');
	assert.match(apiSource, /'manual' \| 'instagram' \| 'facebook'/);
	assert.match(listSource, /\['facebook','Facebook Messenger'\]/);
	assert.match(listSource, /note\.source==='facebook'/);
	assert.match(detailSource, /Facebook Messenger note/);
	assert.doesNotMatch([apiSource, listSource, detailSource].join('\n'), /threads/i);
});

test('Facebook message history API remains authenticated but its table stays off the notes page', async () => {
	let request: Request | undefined;
	const client = createApiClient(async (input, init) => { request = new Request(input, init); return Response.json({ messages: [] }); });
	await client.facebookMessages();
	assert.equal(request?.url, 'http://localhost/api/facebook-messages');
	assert.equal(request?.credentials, 'same-origin');
	const source = await (await import('node:fs/promises')).readFile('src/routes/app/notes/+page.svelte', 'utf8');
	assert.doesNotMatch(source, /Facebook messages|facebookMessages|api\.facebookMessages/);
});

test('Instagram Reel notes render the official playable embed only when cached video is unavailable', async () => {
	const { readFile } = await import('node:fs/promises');
	const source = await readFile('src/lib/components/InstagramMedia.svelte', 'utf8');
	assert.match(source, /hasPlayableVideo/);
	assert.match(source, /attachment\.type === 'ig_reel' && link && !hasPlayableVideo/);
	assert.match(source, /<iframe[^>]+src=\{reelEmbed\(link\)\}/);
	assert.match(source, /title="Instagram Reel"/);
	assert.match(source, /Open on Instagram/);
});

test('Instagram shared post and Reel links are copyable in note details', async () => {
	const { readFile } = await import('node:fs/promises');
	const apiSource = await readFile('src/lib/api.ts', 'utf8');
	const source = await readFile('src/lib/components/InstagramMedia.svelte', 'utf8');
	assert.match(apiSource, /permalink\?: string/);
	assert.match(source, /navigator\.clipboard\.writeText/);
	assert.match(source, /aria-label="Copy Instagram link"/);
	assert.match(source, /attachment\.permalink/);
});

test('Facebook attachments render a native metadata card instead of an unreliable plugin iframe', async () => {
	const { readFile } = await import('node:fs/promises');
	const apiSource = await readFile('src/lib/api.ts', 'utf8');
	const detailSource = await readFile('src/routes/app/notes/[id]/+page.svelte', 'utf8');
	const mediaSource = await readFile('src/lib/components/FacebookAttachments.svelte', 'utf8');
	assert.match(apiSource, /FacebookAttachment/);
	assert.match(apiSource, /facebook_attachments: FacebookAttachment\[\]/);
	assert.match(detailSource, /FacebookAttachments attachments=\{note\.facebook_attachments\}/);
	assert.match(mediaSource, /Shared Facebook post/);
	assert.match(mediaSource, /Shared Facebook Reel/);
	assert.match(mediaSource, /focus-visible/);
	assert.match(mediaSource, /target="_blank"/);
	assert.match(mediaSource, /rel="noopener noreferrer"/);
	assert.match(mediaSource, /attachment\.title/);
	assert.match(mediaSource, /Preview unavailable/);
	assert.doesNotMatch(mediaSource, /facebook\.com\/plugins|<iframe|fetch\(/i);
});
