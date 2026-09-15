<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { appState, deleteNote } from '$lib/app-state.svelte';
	import { formatDate, renderMarkdown } from '$lib/app-utils';
	import { Button } from '$lib/components/ui/button';
	import * as Dialog from '$lib/components/ui/dialog';
	import { ArrowLeft, MessageCircle, Pencil, Trash2 } from '@lucide/svelte';
	let note = $derived(appState.notes.find((item) => item.id === page.params.id));
	let confirmOpen = $state(false);
	function remove() { if (note) { deleteNote(note.id); goto('/notes'); } }
</script>

<svelte:head><title>{note?.title ?? 'Note not found'} · NoteDesk</title></svelte:head>
<div class="page page-narrow">
	<a class="source" href="/notes"><ArrowLeft size={14} />Back to notes</a>
	{#if note}<header class="page-header" style="margin-top:24px"><div><span class="eyebrow">{note.source === 'instagram' ? 'Instagram note' : 'Manual note'}</span><h1>{note.title}</h1><div class="detail-meta"><span class="source">{#if note.source === 'instagram'}<MessageCircle size={13} />{/if}{note.source}</span><span>Created {formatDate(note.createdAt)}</span><span>Updated {formatDate(note.updatedAt)}</span></div></div><div style="display:flex;gap:9px"><Button href={`/notes/${note.id}/edit`} variant="outline"><Pencil />Edit</Button><Button variant="outline" aria-label="Delete note" onclick={() => confirmOpen = true}><Trash2 />Delete</Button></div></header>
		<article class="panel detail-body prose">{@html renderMarkdown(note.content)}</article>
	{:else}<section class="panel state-box"><div><h2>Note not found</h2><p>This note may have been deleted or is no longer available.</p><Button href="/notes">Return to notes</Button></div></section>{/if}
</div>
<Dialog.Root bind:open={confirmOpen}><Dialog.Content><Dialog.Header><Dialog.Title>Delete this note?</Dialog.Title><Dialog.Description>This removes “{note?.title}” from this browser. This action cannot be undone.</Dialog.Description></Dialog.Header><div class="dialog-actions"><Button variant="outline" onclick={() => confirmOpen = false}>Cancel</Button><Button variant="destructive" onclick={remove}>Delete note</Button></div></Dialog.Content></Dialog.Root>
