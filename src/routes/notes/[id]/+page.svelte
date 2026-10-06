<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { api, type Note, type InstagramCachedMedia } from '$lib/api';
	import { formatDate, renderMarkdown } from '$lib/app-utils';
	import { Button } from '$lib/components/ui/button';
	import * as Dialog from '$lib/components/ui/dialog';
	import InstagramMedia from '$lib/components/InstagramMedia.svelte';
	import FacebookAttachments from '$lib/components/FacebookAttachments.svelte';
	import { ArrowLeft, MessageCircle, Pencil, Trash2 } from '@lucide/svelte';
	import { onMount } from 'svelte';
	let note = $state<Note | null>(null);
	let loading = $state(true);
	let error = $state('');
	let confirmOpen = $state(false);
	let instagramMedia = $state<InstagramCachedMedia[]>([]);
	let postLoading = $state(false);
	let postError = $state('');
	let postLoaded = $state(false);
	let hasPostURL = $derived(!!note && (/https?:\/\/\S+/.test(note.content_markdown) || note.instagram_attachments.length > 0 || note.facebook_attachments.length > 0));
	async function load() { loading = true; error = ''; try { note = await api.getNote(Number(page.params.id)); } catch (cause) { error = cause instanceof Error ? cause.message : 'The note could not load.'; } finally { loading = false; } }
	async function loadPost() { if (!note || postLoading) return; postLoading = true; postError = ''; try { if (note.instagram_attachments.length) { const resolved = await api.resolveInstagramMedia(note.id); instagramMedia = resolved.media; note.instagram_attachments = resolved.attachments; } postLoaded = true; } catch (cause) { postError = cause instanceof Error ? cause.message : 'The post could not load.'; } finally { postLoading = false; } }
	async function remove() { if (!note) return; try { await api.deleteNote(note.id); await goto(resolve('/notes')); } catch (cause) { error = cause instanceof Error ? cause.message : 'The note could not be deleted.'; confirmOpen = false; } }
	onMount(load);
</script>

<svelte:head><title>{note?.title ?? 'Note not found'} · NoteDesk</title></svelte:head>
<div class="page page-narrow">
	<a class="source" href={resolve('/notes')}><ArrowLeft size={14} />Back to notes</a>
	{#if loading}<section class="panel state-box" aria-live="polite"><div><p>Loading note…</p></div></section>
	{:else if note}<header class="page-header" style="margin-top:24px"><div><span class="eyebrow">{note.source === 'facebook' ? 'Facebook Messenger note' : note.source === 'instagram' ? 'Instagram note' : 'Manual note'}</span><h1>{note.title}</h1><div class="detail-meta"><span class="source">{#if note.source === 'instagram' || note.source === 'facebook'}<MessageCircle size={13} />{/if}{note.source === 'facebook' ? 'Facebook Messenger' : note.source}</span><span>Created {formatDate(note.created_at)}</span><span>Updated {formatDate(note.updated_at)}</span></div></div><div style="display:flex;gap:9px"><Button href={resolve('/notes/[id]/edit', { id: String(note.id) })} variant="outline"><Pencil />Edit</Button><Button variant="outline" aria-label="Delete note" onclick={() => confirmOpen = true}><Trash2 />Delete</Button></div></header>
		{#if error}<p class="field-error" role="alert">{error}</p>{/if}
		{#if hasPostURL && !postLoaded}<div class="post-loader"><Button variant="outline" disabled={postLoading} onclick={loadPost}>{postLoading ? 'Loading…' : postError ? 'Retry loading post' : 'Load post'}</Button>{#if postError}<p class="field-error" role="alert">{postError}</p>{/if}</div>{/if}
		{#if postLoaded}<InstagramMedia attachments={note.instagram_attachments} media={instagramMedia} noteId={note.id} /><FacebookAttachments attachments={note.facebook_attachments} />{/if}<article class="panel detail-body prose">{@html renderMarkdown(note.content_markdown)}</article>
	{:else}<section class="panel state-box"><div><h2>Note not found</h2><p>{error || 'This note may have been deleted or is no longer available.'}</p><Button href={resolve('/notes')}>Return to notes</Button></div></section>{/if}
</div>
<Dialog.Root bind:open={confirmOpen}><Dialog.Content><Dialog.Header><Dialog.Title>Delete this note?</Dialog.Title><Dialog.Description>This permanently removes “{note?.title}”. This action cannot be undone.</Dialog.Description></Dialog.Header><div class="dialog-actions"><Button variant="outline" onclick={() => confirmOpen = false}>Cancel</Button><Button variant="destructive" onclick={remove}>Delete note</Button></div></Dialog.Content></Dialog.Root>
