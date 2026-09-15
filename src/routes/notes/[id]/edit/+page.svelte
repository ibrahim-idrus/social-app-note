<script lang="ts">
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { appState } from '$lib/app-state.svelte';
	import NoteEditor from '$lib/components/NoteEditor.svelte';
	import { Button } from '$lib/components/ui/button';
	let note = $derived(appState.notes.find((item) => item.id === page.params.id));
</script>
<svelte:head><title>Edit note · NoteDesk</title></svelte:head>
<div class="page"><header class="page-header"><div><span class="eyebrow">{note?.source === 'instagram' ? 'Imported from Instagram' : 'Manual note'}</span><h1>Edit note</h1><p class="subtle">Edits do not change the original source label.</p></div></header>{#if note}<NoteEditor {note} />{:else}<section class="panel state-box"><div><h2>Note not found</h2><p>This note is not available to edit.</p><Button href={resolve('/notes')}>Return to notes</Button></div></section>{/if}</div>
