<script lang="ts">
	import { api, type SocialIdentity } from '$lib/api';
	import { appState } from '$lib/app-state.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { MessageCircle, Trash2 } from '@lucide/svelte';
	import { onMount } from 'svelte';
	let identity = $state<SocialIdentity | null>(null);
	let username = $state('');
	let error = $state('');
	let loading = $state(true);
	async function load() {
		loading = true;
		try { identity = (await api.identities())[0] ?? null; }
		catch (cause) { error = cause instanceof Error ? cause.message : 'Settings could not load.'; }
		finally { loading = false; }
	}
	async function add() {
		error = '';
		try { identity = await api.createSocialIdentity('instagram', username); username = ''; }
		catch (cause) { error = cause instanceof Error ? cause.message : 'Identity could not be saved.'; }
	}
	async function remove() {
		if (!identity) return;
		try { await api.deleteSocialIdentity(identity.id); identity = null; }
		catch (cause) { error = cause instanceof Error ? cause.message : 'Identity could not be removed.'; }
	}
	onMount(load);
</script>
<svelte:head><title>Settings · NoteDesk</title></svelte:head>
<div class="page page-narrow">
	<header class="page-header">
		<div><span class="eyebrow">Workspace</span><h1>Settings</h1><p class="subtle">Manage your profile and optional Instagram capture identity.</p></div>
	</header>
	<div class="settings-stack">
		<div class="panel">
			<div class="panel-head"><div><h2>Profile</h2><p class="field-help">Account profile is managed by the backend.</p></div></div>
			<div class="panel-body"><p><strong>{appState.user?.name}</strong><br />{appState.user?.email}</p></div>
		</div>
		<div class="panel">
			<div class="panel-head"><div><h2>Social capture</h2><p class="field-help">One identity per supported platform.</p></div></div>
			<div class="instagram-card">
				<div class="platform-title"><span class="platform-icon"><MessageCircle size={20} /></span><div><strong>Instagram</strong><p class="field-help">No personal OAuth. Real ingestion remains disabled until Meta feasibility validation passes.</p></div></div></div>
				{#if loading}
					<p>Loading…</p>
				{:else if error}
					<div class="notice error" role="alert">{error}</div>
				{:else if identity}
					<div class="notice"><strong>@{identity.username}</strong> is pending. Message ingestion is not enabled.</div>
					<Button variant="outline" onclick={remove}><Trash2 />Remove identity</Button>
				{:else}
					<form class="toolbar" onsubmit={(event) => { event.preventDefault(); add(); }}>
						<Input aria-label="Instagram username" placeholder="Instagram username" bind:value={username} />
						<Button type="submit" disabled={!username.trim()}>Save as pending</Button>
					</form>
				{/if}
			</div>
		</div>
	</div>
