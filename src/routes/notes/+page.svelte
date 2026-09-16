<script lang="ts">
	import { resolve } from '$app/paths';
	import { api, type Note } from '$lib/api';
	import { formatDate } from '$lib/app-utils';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { AlertCircle, ChevronLeft, ChevronRight, FileX2, LoaderCircle, MessageCircle, Plus, Search } from '@lucide/svelte';
	import { onMount } from 'svelte';

	let query = $state(''); let source = $state(''); let sort = $state('updated_at'); let order = $state('desc'); let page = $state(1);
	let notes = $state<Note[]>([]); let total = $state(0); let loading = $state(true); let error = $state('');
	const pageSize = 5; let pageCount = $derived(Math.max(1, Math.ceil(total / pageSize)));
	async function load() { loading = true; error = ''; try { const result = await api.listNotes({ query, source, sort, order, page, pageSize }); notes = result.notes; total = result.total; } catch (cause) { error = cause instanceof Error ? cause.message : 'Notes could not load.'; } finally { loading = false; } }
	async function changeFilters() { page = 1; await load(); }
	onMount(load);
</script>
<svelte:head><title>Notes · NoteDesk</title></svelte:head>
<div class="page">
	<header class="page-header"><div><span class="eyebrow">Library</span><h1>Notes</h1><p class="subtle">Search and review everything you’ve saved.</p></div><Button href={resolve('/notes/new')}><Plus />New note</Button></header>
	<div class="toolbar">
		<div class="search-wrap"><Search size={17} /><Input aria-label="Search notes" placeholder="Search title or content…" bind:value={query} onchange={changeFilters} /></div>
		<select class="select" aria-label="Filter by source" bind:value={source} onchange={changeFilters}><option value="">All sources</option><option value="manual">Manual</option><option value="instagram">Instagram</option></select>
		<select class="select" aria-label="Sort notes" bind:value={sort} onchange={load}><option value="updated_at">Recently updated</option><option value="created_at">Recently created</option><option value="title">Title</option></select>
		<select class="select" aria-label="Sort direction" bind:value={order} onchange={load}><option value="desc">Descending</option><option value="asc">Ascending</option></select>
	</div>
	<section class="panel" aria-live="polite">
		{#if loading}<div class="state-box"><div><LoaderCircle class="spinner" size={28} /><h2>Loading notes</h2></div></div>
		{:else if error}<div class="state-box" role="alert"><div><AlertCircle size={28} /><h2>Notes couldn’t load</h2><p>{error}</p><Button onclick={load}>Try again</Button></div></div>
		{:else if notes.length === 0}<div class="state-box"><div><FileX2 size={28} /><h2>No notes found</h2><p>{query || source ? 'Try a different search or source filter.' : 'Create your first note to start this workspace.'}</p>{#if query || source}<Button variant="outline" onclick={() => { query = ''; source = ''; changeFilters(); }}>Clear filters</Button>{:else}<Button href={resolve('/notes/new')}>Create a note</Button>{/if}</div></div>
		{:else}{#each notes as note}<a class="note-row" href={resolve('/notes/[id]', { id: String(note.id) })}><div><h3>{note.title}</h3><p>{note.content_markdown.replace(/[#*_>`-]/g, '')}</p></div><div class="note-meta"><span class="source">{#if note.source === 'instagram'}<MessageCircle size={12} />{/if}{note.source}</span><br />Updated {formatDate(note.updated_at)}</div></a>{/each}
		<div class="pagination"><span>{total} note{total === 1 ? '' : 's'} · Page {page} of {pageCount}</span><div><Button variant="outline" size="icon" aria-label="Previous page" disabled={page === 1} onclick={() => { page--; load(); }}><ChevronLeft /></Button><Button variant="outline" size="icon" aria-label="Next page" disabled={page >= pageCount} onclick={() => { page++; load(); }}><ChevronRight /></Button></div></div>{/if}
	</section>
</div>
