import assert from 'node:assert/strict';
import test from 'node:test';
import { notesToMarkdown } from '../src/lib/note-export.ts';

const note = (id: number, title: string, tags: string[] = []) => ({
	id, title, tags, content_markdown: `Body ${id}`, source: 'manual' as const,
	created_at: '2026-10-01T10:00:00Z', updated_at: '2026-10-02T10:00:00Z',
	social_identity_id: null, external_message_id: null, instagram_attachments: [], facebook_attachments: []
});

test('exports selected notes in visible order as one deterministic Markdown document', () => {
	assert.equal(notesToMarkdown([note(2, 'Second # title', ['Work']), note(1, 'First', ['idea', 'work'])]), `# Second \\# title

- Source: Manual
- Created: 2026-10-01T10:00:00Z
- Updated: 2026-10-02T10:00:00Z
- Tags: #Work

Body 2

---

# First

- Source: Manual
- Created: 2026-10-01T10:00:00Z
- Updated: 2026-10-02T10:00:00Z
- Tags: #idea, #work

Body 1
`);
});

test('omits tag metadata when a note has no tags', () => {
	assert.doesNotMatch(notesToMarkdown([note(1, 'Plain')]), /Tags:/);
});

test('escapes Markdown syntax in generated title metadata', () => {
	assert.ok(notesToMarkdown([note(1, '[Draft] *one*')]).startsWith('# \\[Draft\\] \\*one\\*'));
});
