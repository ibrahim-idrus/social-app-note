<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { api, type Note } from '$lib/api';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Editor, defaultValueCtx, rootCtx } from '@milkdown/kit/core';
	import { listener, listenerCtx } from '@milkdown/kit/plugin/listener';
	import { commonmark } from '@milkdown/kit/preset/commonmark';
	import { getMarkdown } from '@milkdown/kit/utils';
	import { onMount } from 'svelte';

	let { note }: { note?: Note } = $props();
	let title = $state(note?.title ?? '');
	let content = $state(note?.content_markdown ?? '');
	let error = $state('');
	let saving = $state(false);
	let editorRoot: HTMLDivElement;
	let editor: Editor | undefined;

	onMount(() => {
		editor = Editor.make()
			.config((ctx) => {
				ctx.set(rootCtx, editorRoot);
				ctx.set(defaultValueCtx, content);
				ctx.get(listenerCtx).markdownUpdated((_ctx, markdown) => { content = markdown; });
			})
			.use(commonmark)
			.use(listener);
		void editor.create();
		return () => { void editor?.destroy(); };
	});

	async function submit() {
		if (editor) content = editor.action(getMarkdown());
		error = !title.trim() ? 'Add a title before saving.' : !content.trim() ? 'Add some note content before saving.' : '';
		if (error) return;
		saving = true;
		try {
			const saved = note ? await api.updateNote(note.id, { title: title.trim(), content: content.trim() }) : await api.createNote({ title: title.trim(), content: content.trim() });
			await goto(resolve('/app/notes/[id]', { id: String(saved.id) }));
		} catch (cause) { error = cause instanceof Error ? cause.message : 'The note could not be saved.'; }
		finally { saving = false; }
	}
</script>

<div class="form-grid">
	<div class="field"><Label for="note-title">Title</Label><Input id="note-title" placeholder="A clear, useful title" maxlength={120} bind:value={title} aria-invalid={!!error && !title.trim()} />{#if error}<span class="field-error" role="alert">{error}</span>{/if}</div>
	<div class="milkdown-field">
		<div class="editor-label"><span>Body</span><span>Markdown</span></div>
		<div class="milkdown-editor" bind:this={editorRoot} aria-label="Note body in Markdown"></div>
	</div>
	<p class="field-help">Markdown is formatted as you write. Raw HTML is stored as text and links are limited to HTTP and HTTPS when displayed.</p>
	<div class="form-actions"><Button variant="outline" onclick={() => history.back()}>Cancel</Button><Button onclick={submit} disabled={saving}>{saving ? 'Saving…' : note ? 'Save changes' : 'Save note'}</Button></div>
</div>
