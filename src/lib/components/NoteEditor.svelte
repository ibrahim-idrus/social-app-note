<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { api, type Note } from '$lib/api';
	import { renderMarkdown } from '$lib/app-utils';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Bold, Eye, Heading2, Italic, Link, List } from '@lucide/svelte';

	let { note }: { note?: Note } = $props();
	let title = $state('');
	let content = $state('');
	let error = $state('');
	let area = $state<HTMLTextAreaElement | null>(null);
	let initialized = $state(false);
	let saving = $state(false);
	$effect.pre(() => {
		if (!initialized) { title = note?.title ?? ''; content = note?.content_markdown ?? ''; initialized = true; }
	});
	function wrap(before: string, after = before, placeholder = 'text') {
		if (!area) return;
		const target = area; const start = target.selectionStart; const end = target.selectionEnd; const selected = content.slice(start, end) || placeholder;
		content = `${content.slice(0, start)}${before}${selected}${after}${content.slice(end)}`;
		requestAnimationFrame(() => { target.focus(); target.setSelectionRange(start + before.length, start + before.length + selected.length); });
	}
	async function submit() {
		error = !title.trim() ? 'Add a title before saving.' : !content.trim() ? 'Add some note content before saving.' : '';
		if (error) return;
		saving = true;
		try {
			const saved = note ? await api.updateNote(note.id, { title: title.trim(), content: content.trim() }) : await api.createNote({ title: title.trim(), content: content.trim() });
			await goto(resolve('/notes/[id]', { id: String(saved.id) }));
		} catch (cause) { error = cause instanceof Error ? cause.message : 'The note could not be saved.'; }
		finally { saving = false; }
	}
</script>

<div class="form-grid">
	<div class="field"><Label for="note-title">Title</Label><Input id="note-title" placeholder="A clear, useful title" maxlength={120} bind:value={title} aria-invalid={!!error && !title.trim()} />{#if error}<span class="field-error" role="alert">{error}</span>{/if}</div>
	<div class="editor-grid">
		<section class="editor-pane"><div class="editor-label"><span>Markdown</span><div class="markdown-tools" aria-label="Markdown tools"><button type="button" title="Heading" aria-label="Add heading" onclick={() => wrap('## ', '', 'Heading')}><Heading2 size={16} /></button><button type="button" title="Bold" aria-label="Bold selection" onclick={() => wrap('**')}><Bold size={16} /></button><button type="button" title="Italic" aria-label="Italicize selection" onclick={() => wrap('*')}><Italic size={16} /></button><button type="button" title="List" aria-label="Add list item" onclick={() => wrap('- ', '', 'List item')}><List size={16} /></button><button type="button" title="Link" aria-label="Add link" onclick={() => wrap('[', '](https://)', 'link text')}><Link size={16} /></button></div></div><Textarea id="note-content" aria-label="Note content in Markdown" class="editor-area" placeholder="# Start writing…" bind:ref={area} bind:value={content} /></section>
		<section class="editor-pane"><div class="editor-label"><span>Preview</span><Eye size={15} /></div><div class="preview prose">{#if content}<div>{@html renderMarkdown(content)}</div>{:else}<p class="subtle">Your formatted note will appear here.</p>{/if}</div></section>
	</div>
	<p class="field-help">Raw HTML is displayed as text. Links are limited to HTTP and HTTPS.</p>
	<div class="form-actions"><Button variant="outline" onclick={() => history.back()}>Cancel</Button><Button onclick={submit} disabled={saving}>{saving ? 'Saving…' : note ? 'Save changes' : 'Save note'}</Button></div>
</div>
