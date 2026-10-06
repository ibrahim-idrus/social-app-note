import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const source = await readFile('src/routes/notes/+page.svelte', 'utf8');

test('changing search scope reruns an active search immediately', () => {
	assert.match(source, /aria-label="Search in"[^>]*onchange=\{\(\)=>query&&commit\(\)\}/);
});

test('older note requests cannot replace newer search results', () => {
	assert.match(source, /const request\s*=\s*\+\+requestID/);
	assert.match(source, /if\(request!==requestID\)return/);
});

test('restores and persists repeated source and tag filters in the URL', () => {
	assert.match(source, /getAll\('source'\)/);
	assert.match(source, /getAll\('tag'\)/);
	assert.match(source, /sources\.forEach\(\(value\)=>p\.append\('source',value\)\)/);
	assert.match(source, /tags\.forEach\(\(value\)=>p\.append\('tag',value\)\)/);
});

test('offers row and all-visible selection, combined export, and confirmed sequential delete', () => {
	assert.match(source, /aria-label="Select all visible notes"/);
	assert.match(source, /aria-label=\{`Select \$\{note\.title\}`\}/);
	assert.match(source, /notesToMarkdown/);
	assert.match(source, /confirm\(`Delete \$\{selected\.size\} selected note/);
	assert.match(source, /for \(const id of \[\.\.\.selected\]\)/);
	assert.match(source, /selected = new Set\(failed\)/);
	assert.match(source, /<DropdownMenu\.Trigger[^>]*>Actions/);
	assert.match(source, /<DropdownMenu\.Item onclick=\{download\}>/);
	assert.match(source, /<DropdownMenu\.Item variant="destructive" onclick=\{removeSelected\}>/);
});

test('shows note tags and accessible multi-value source and tag filters', () => {
	assert.match(source, /fieldset class="filter-group"/);
	assert.match(source, /note\.tags/);
	assert.match(source, /class="tag-chip"/);
});

test('loads the complete user tag list instead of deriving filters from one page', () => {
	assert.match(source, /Promise\.all\(\[api\.listNotes\([^]*api\.tags\(\)\]\)/);
	assert.match(source, /availableTags=allTags/);
});
