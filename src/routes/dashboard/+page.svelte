<script lang="ts">
	import { resolve } from '$app/paths';
	import { api, type Note, type SocialIdentity } from '$lib/api';
	import { appState } from '$lib/app-state.svelte';
	import { formatDate } from '$lib/app-utils';
	import { Button } from '$lib/components/ui/button';
	import { ArrowRight, MessageCircle, NotebookPen, Plus } from '@lucide/svelte';
	import { onMount } from 'svelte';
	let notes = $state<Note[]>([]); let identity = $state<SocialIdentity | null>(null); let error = $state('');
	let manual = $derived(notes.filter((note) => note.source === 'manual').length); let instagram = $derived(notes.length - manual);
	onMount(async () => { try { const [result, identities] = await Promise.all([api.listNotes({ pageSize: 100 }), api.identities()]); notes = result.notes; identity = identities[0] ?? null; } catch (cause) { error = cause instanceof Error ? cause.message : 'Dashboard could not load.'; } });
</script>
<svelte:head><title>Dashboard · NoteDesk</title></svelte:head>
<div class="page">
<header class="page-header"><div><span class="eyebrow">Workspace</span><h1>Hello, {appState.user?.name.split(' ')[0]}</h1><p class="subtle">A quick view of what you’ve captured lately.</p></div><Button href={resolve('/notes/new')}><Plus />Create note</Button></header>
{#if error}<div class="notice error" role="alert">{error}</div>{/if}
<section class="stats" aria-label="Note overview"><div class="stat"><small>Total notes</small><strong>{notes.length}</strong></div><div class="stat"><small>Manual</small><strong>{manual}</strong></div><div class="stat"><small>From Instagram</small><strong>{instagram}</strong></div></section>
<div class="dashboard-grid"><section class="panel"><div class="panel-head"><h2>Recent notes</h2><Button href={resolve('/notes')} variant="ghost" size="sm">View all <ArrowRight /></Button></div>{#each notes.slice(0,4) as note}<a class="note-row" href={resolve('/notes/[id]', { id: String(note.id) })}><div><h3>{note.title}</h3><p>{note.content_markdown.replace(/[#*_>`-]/g, '')}</p></div><div class="note-meta"><span class="source">{note.source}</span><br />{formatDate(note.updated_at)}</div></a>{/each}</section>
<aside class="panel"><div class="panel-head"><h2>Instagram inbox</h2></div><div class="panel-body"><div class="status-line"><MessageCircle size={19} /><div><strong>{identity ? `@${identity.username}` : 'Not set up'}</strong><div class="field-help">{identity ? 'Pending Meta feasibility validation' : 'Optional social capture is not configured.'}</div></div></div><div class="quick-list"><a class="quick-link" href={resolve('/notes/new')}><NotebookPen size={17} />Write a manual note</a><a class="quick-link" href={resolve('/settings')}><MessageCircle size={17} />Manage Instagram</a></div></div></aside></div>
</div>
