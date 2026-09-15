<script lang="ts">
	import { appState } from '$lib/app-state.svelte';
	import { formatDate } from '$lib/app-utils';
	import { Button } from '$lib/components/ui/button';
	import { ArrowRight, MessageCircle, NotebookPen, Plus } from '@lucide/svelte';
	let manual = $derived(appState.notes.filter((note) => note.source === 'manual').length);
	let instagram = $derived(appState.notes.length - manual);
	let recent = $derived([...appState.notes].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)).slice(0, 4));
</script>

<svelte:head><title>Dashboard · NoteDesk</title></svelte:head>
<div class="page">
	<header class="page-header"><div><span class="eyebrow">Workspace</span><h1>Good morning, {appState.name.split(' ')[0]}</h1><p class="subtle">A quick view of what you’ve captured lately.</p></div><Button href="/notes/new"><Plus />Create note</Button></header>
	<section class="stats" aria-label="Note overview"><div class="stat"><small>Total notes</small><strong>{appState.notes.length}</strong></div><div class="stat"><small>Manual</small><strong>{manual}</strong></div><div class="stat"><small>From Instagram</small><strong>{instagram}</strong></div></section>
	<div class="dashboard-grid">
		<section class="panel"><div class="panel-head"><h2>Recent notes</h2><Button href="/notes" variant="ghost" size="sm">View all <ArrowRight /></Button></div>{#each recent as note}<a class="note-row" href={`/notes/${note.id}`}><div><h3>{note.title}</h3><p>{note.content.replace(/[#*_>`-]/g, '')}</p></div><div class="note-meta"><span class="source">{note.source === 'instagram' ? 'Instagram' : 'Manual'}</span><br />{formatDate(note.updatedAt)}</div></a>{/each}</section>
		<aside class="panel"><div class="panel-head"><h2>Instagram inbox</h2></div><div class="panel-body">
			{#if appState.instagram}<div class="status-line"><span class:pending={appState.instagram.status === 'pending'} class="status-dot"></span><div><strong style="font-size:13px">@{appState.instagram.username}</strong><div class="field-help">{appState.instagram.status === 'active' ? 'Active and ready for DMs' : 'Waiting for the first DM'}</div></div></div>{:else}<div class="status-line"><MessageCircle size={19} /><div><strong style="font-size:13px">Not set up</strong><div class="field-help">Add one Instagram identity as your capture address.</div></div></div>{/if}
			<div class="quick-list"><a class="quick-link" href="/notes/new"><NotebookPen size={17} />Write a manual note</a><a class="quick-link" href="/settings"><MessageCircle size={17} />{appState.instagram ? 'Manage Instagram' : 'Set up Instagram'}</a></div>
		</div></aside>
	</div>
</div>
