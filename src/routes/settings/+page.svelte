<script lang="ts">
	import { api, type SocialIdentity, type SocialPlatform } from '$lib/api';
	import { appState } from '$lib/app-state.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { MessageCircle, Plus, Trash2, X } from '@lucide/svelte';
	import { onMount } from 'svelte';

	let identities = $state<SocialIdentity[]>([]);
	let platforms = $state<SocialPlatform[]>([]);
	let selectedPlatform = $state<SocialPlatform | null>(null);
	let username = $state('');
	let error = $state('');
	let loading = $state(true);
	let availablePlatforms = $derived(platforms.filter((platform) => !identities.some((identity) => identity.platform === platform.id)));

	async function load() {
		loading = true;
		error = '';
		try { [platforms, identities] = await Promise.all([api.platforms(), api.identities()]); }
		catch (cause) { error = cause instanceof Error ? cause.message : 'Settings could not load.'; }
		finally { loading = false; }
	}
	async function add() {
		if (!selectedPlatform) return;
		error = '';
		try {
			identities = [...identities, await api.createSocialIdentity(selectedPlatform.id, username)];
			username = '';
			selectedPlatform = null;
		} catch (cause) { error = cause instanceof Error ? cause.message : 'Identity could not be saved.'; }
	}
	async function remove(identity: SocialIdentity) {
		error = '';
		try { await api.deleteSocialIdentity(identity.id); identities = identities.filter((item) => item.id !== identity.id); }
		catch (cause) { error = cause instanceof Error ? cause.message : 'Identity could not be removed.'; }
	}
	onMount(load);
</script>
<svelte:head><title>Settings · NoteDesk</title></svelte:head>
<div class="page page-narrow">
	<header class="page-header">
		<div><span class="eyebrow">Workspace</span><h1>Settings</h1><p class="subtle">Manage your profile and social capture accounts.</p></div>
	</header>
	<div class="settings-stack">
		<div class="panel">
			<div class="panel-head"><div><h2>Profile</h2><p class="field-help">Account profile is managed by the backend.</p></div></div>
			<div class="panel-body"><p><strong>{appState.user?.name}</strong><br />{appState.user?.email}</p></div>
		</div>
		<div class="panel">
			<div class="panel-head"><div><h2>Social capture</h2><p class="field-help">Add one account per supported media platform.</p></div></div>
			<div class="instagram-card">
				{#if loading}
					<p>Loading…</p>
				{:else}
					{#if error}<div class="notice error" role="alert">{error}</div>{/if}
					{#each identities as identity (identity.id)}
						<div class="platform-title"><span class="platform-icon"><MessageCircle size={20} /></span><div><strong>{platforms.find((platform) => platform.id === identity.platform)?.name ?? identity.platform}</strong><p class="field-help">@{identity.username} · {identity.status}</p></div></div>
						<Button variant="outline" onclick={() => remove(identity)}><Trash2 />Remove account</Button>
					{/each}
					{#if selectedPlatform}
						<form class="toolbar" onsubmit={(event) => { event.preventDefault(); add(); }}>
							<Input aria-label={`${selectedPlatform.name} username`} placeholder={`${selectedPlatform.name} username`} bind:value={username} />
							<Button type="submit" disabled={!username.trim()}>Add account</Button>
							<Button type="button" variant="ghost" aria-label="Cancel adding platform" onclick={() => { selectedPlatform = null; username = ''; }}><X /></Button>
						</form>
					{:else if availablePlatforms.length}
						<div class="toolbar">
							{#each availablePlatforms as platform (platform.id)}
								<Button variant="outline" onclick={() => selectedPlatform = platform}><Plus />Add platform: {platform.name}</Button>
							{/each}
						</div>
					{:else}
						<p class="field-help">All supported platforms have been added.</p>
					{/if}
				{/if}
			</div>
		</div>
	</div>
</div>
