import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const source = await readFile('src/routes/notes/+page.svelte', 'utf8');

test('changing search scope reruns an active search immediately', () => {
	assert.match(source, /aria-label="Search in"[^>]*onchange=\{\(\)=>query&&commit\(\)\}/);
});

test('older note requests cannot replace newer search results', () => {
	assert.match(source, /const request = \+\+requestID/);
	assert.match(source, /if\(request!==requestID\)return/);
});
