<script lang="ts">
	import { appState } from '$lib/app-state.svelte';
	import { filterNotes, formatDate } from '$lib/app-utils';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { AlertCircle, ChevronLeft, ChevronRight, FileX2, LoaderCircle, MessageCircle, Plus, Search } from '@lucide/svelte';

	let query = $state(''); let source = $state('all'); let sort = $state('updated-desc'); let page = $state(1); let demoState = $state<'ready' | 'loading' | 'error'>('ready');
	const pageSize = 5;
	let filtered = $derived(filterNotes(appState.notes, query, source, sort));
	let pageCount = $derived(Math.max(1, Math.ceil(filtered.length / pageSize)));
	let visible = $derived(filtered.slice((page - 1) * pageSize, page * pageSize));
	function setDemo(value: 'ready' | 'loading' | 'error') { demoState = value; if (value === 'loading') setTimeout(() => demoState = 'ready', 900); }
</script>

<svelte:head><title>Notes · NoteDesk</title></svelte:head>
<div class="page">
	<header class="page-header"><div><span class="eyebrow">Library</span><h1>Notes</h1><p class="subtle">Search and review everything you’ve saved.</p></div><Button href="/notes/new"><Plus />New note</Button></header>
	<div class="toolbar">
		<div class="search-wrap"><Search size={17} /><Input aria-label="Search notes" placeholder="Search title or content…" bind:value={query} oninput={() => page = 1} /></div>
		<select class="select" aria-label="Filter by source" bind:value={source} onchange={() => page = 1}><option value="all">All sources</option><option value="manual">Manual</option><option value="instagram">Instagram</option></select>
		<select class="select" aria-label="Sort notes" bind:value={sort}><option value="updated-desc">Recently updated</option><option value="created-desc">Recently created</option><option value="title-asc">Title A–Z</option></select>
		<select class="select" aria-label="Demonstration state" value={demoState} onchange={(event) => setDemo(event.currentTarget.value as typeof demoState)}><option value="ready">Ready state</option><option value="loading">Show loading</option><option value="error">Show error</option></select>
	</div>
	<section class="panel" aria-live="polite">
		{#if demoState === 'loading'}<div class="state-box"><div><LoaderCircle class="spinner" size={28} /><h2>Loading notes</h2><p>Collecting your latest notes…</p></div></div>
		{:else if demoState === 'error'}<div class="state-box"><div><AlertCircle size={28} /><h2>Notes couldn’t load</h2><p>This demonstration error doesn’t affect your saved notes.</p><Button onclick={() => setDemo('ready')}>Try again</Button></div></div>
		{:else if visible.length === 0}<div class="state-box"><div><FileX2 size={28} /><h2>No notes found</h2><p>{query || source !== 'all' ? 'Try a different search or source filter.' : 'Create your first note to start this workspace.'}</p>{#if query || source !== 'all'}<Button variant="outline" onclick={() => { query = ''; source = 'all'; }}>Clear filters</Button>{:else}<Button href="/notes/new">Create a note</Button>{/if}</div></div>
		{:else}{#each visible as note}<a class="note-row" href={`/notes/${note.id}`}><div><h3>{note.title}</h3><p>{note.content.replace(/[#*_>`-]/g, '')}</p></div><div class="note-meta"><span class="source">{#if note.source === 'instagram'}<MessageCircle size={12} />{/if}{note.source}</span><br />Updated {formatDate(note.updatedAt)}</div></a>{/each}
			<div class="pagination"><span>{filtered.length} note{filtered.length === 1 ? '' : 's'} · Page {page} of {pageCount}</span><div><Button variant="outline" size="icon" aria-label="Previous page" disabled={page === 1} onclick={() => page--}><ChevronLeft /></Button><Button variant="outline" size="icon" aria-label="Next page" disabled={page === pageCount} onclick={() => page++}><ChevronRight /></Button></div></div>
		{/if}
	</section>
</div>
