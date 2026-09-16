<script lang="ts">
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { api, type Note } from '$lib/api';
	import NoteEditor from '$lib/components/NoteEditor.svelte';
	import { Button } from '$lib/components/ui/button';
	import { onMount } from 'svelte';
	let note = $state<Note | null>(null);
	let loading = $state(true);
	let error = $state('');
	onMount(async () => { try { note = await api.getNote(Number(page.params.id)); } catch (cause) { error = cause instanceof Error ? cause.message : 'The note could not load.'; } finally { loading = false; } });
</script>
<svelte:head><title>Edit note · NoteDesk</title></svelte:head>
<div class="page"><header class="page-header"><div><span class="eyebrow">{note?.source === 'instagram' ? 'Imported from Instagram' : 'Manual note'}</span><h1>Edit note</h1><p class="subtle">Edits do not change the original source label.</p></div></header>{#if loading}<section class="panel state-box" aria-live="polite"><div><p>Loading note…</p></div></section>{:else if note}<NoteEditor {note} />{:else}<section class="panel state-box"><div><h2>Note not found</h2><p>{error || 'This note is not available to edit.'}</p><Button href={resolve('/notes')}>Return to notes</Button></div></section>{/if}</div>
