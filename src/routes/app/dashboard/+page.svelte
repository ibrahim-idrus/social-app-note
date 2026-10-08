<script lang="ts">
	import { resolve } from '$app/paths';
	import { api, type Note, type SocialIdentity } from '$lib/api';
	import { appState } from '$lib/app-state.svelte';
	import { formatDate } from '$lib/app-utils';
	import { Button } from '$lib/components/ui/button';
	import { ArrowRight, MessageCircle, NotebookPen, Plus } from '@lucide/svelte';
	import { onMount } from 'svelte';
	let notes = $state<Note[]>([]); let identities = $state<SocialIdentity[]>([]); let error = $state('');
	let manual = $derived(notes.filter((note) => note.source === 'manual').length); let instagram = $derived(notes.filter((note) => note.source === 'instagram').length); let facebook = $derived(notes.filter((note) => note.source === 'facebook').length);
	onMount(async () => { try { const [result, connected] = await Promise.all([api.listNotes({ pageSize: 100 }), api.identities()]); notes = result.notes; identities = connected; } catch (cause) { error = cause instanceof Error ? cause.message : 'Dashboard could not load.'; } });
</script>
<svelte:head><title>Dashboard · SocialNotes</title></svelte:head>
<div class="page">
<header class="page-header"><div><span class="eyebrow">Workspace</span><h1>Hello, {appState.user?.name.split(' ')[0]}</h1><p class="subtle">A quick view of what you’ve captured lately.</p></div><Button href={resolve('/app/notes/new')}><Plus />Create note</Button></header>
{#if error}<div class="notice error" role="alert">{error}</div>{/if}
<section class="stats" aria-label="Note overview"><div class="stat"><small>Total notes</small><strong>{notes.length}</strong></div><div class="stat"><small>Manual</small><strong>{manual}</strong></div><div class="stat"><small>From Instagram</small><strong>{instagram}</strong></div><div class="stat"><small>From Facebook</small><strong>{facebook}</strong></div></section>
<div class="dashboard-grid"><section class="panel"><div class="panel-head"><h2>Recent notes</h2><Button href={resolve('/app/notes')} variant="ghost" size="sm">View all <ArrowRight /></Button></div>{#each notes.slice(0,4) as note}<a class="note-row" href={resolve('/app/notes/[id]', { id: String(note.id) })}><div><h3>{note.title}</h3><p>{note.content_markdown.replace(/[#*_>`-]/g, '')}</p></div><div class="note-meta"><span class="source">{note.source}</span><br />{formatDate(note.updated_at)}</div></a>{/each}</section>
<aside class="panel"><div class="panel-head"><h2>Social inboxes</h2></div><div class="panel-body">{#each identities as identity}<div class="status-line"><MessageCircle size={19} /><div><strong>{identity.platform === 'facebook' ? identity.username || `Page ${identity.facebook_page?.page_id}` : `@${identity.username}`}</strong><div class="field-help">{identity.platform === 'facebook' ? 'Facebook Messenger' : 'Instagram'} · {identity.verification_state === 'active' ? 'Connected' : 'Pending'}</div></div></div>{/each}{#if identities.length === 0}<p class="field-help">Optional social capture is not configured.</p>{/if}<div class="quick-list"><a class="quick-link" href={resolve('/app/notes/new')}><NotebookPen size={17} />Write a manual note</a><a class="quick-link" href={resolve('/app/settings')}><MessageCircle size={17} />Manage social inboxes</a></div></div></aside></div>
</div>
