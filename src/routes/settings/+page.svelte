<script lang="ts">
	import { api, type InstagramRegistration, type SocialAccountMatch, type SocialIdentity, type SocialPlatform } from '$lib/api';
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
	let registration = $state<InstagramRegistration | null>(null);
	let matches = $state<SocialAccountMatch[]>([]);
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
			registration = await api.createSocialIdentity(selectedPlatform.id, username);
			identities = [...identities, registration];
			username = '';
			selectedPlatform = null;
		} catch (cause) { error = cause instanceof Error ? cause.message : 'Identity could not be saved.'; }
	}
	async function search() {
		error = '';
		try { matches = await api.searchSocialIdentities(username); }
		catch (cause) { matches = []; error = cause instanceof Error ? cause.message : 'Instagram search failed.'; }
	}
	async function regenerate(identity: SocialIdentity) {
		error = '';
		try { registration = await api.regenerateSocialIdentityCode(identity.id); }
		catch (cause) { error = cause instanceof Error ? cause.message : 'Code could not be regenerated.'; }
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
						{#if identity.status === 'pending'}
							<div class="notice verification-instructions">
								<div>
									<p><strong>DM this code to @{(registration?.id === identity.id ? registration.instagram_account : identity.instagram_account)?.username}</strong></p>
									{#if registration?.id === identity.id}<code>{registration.verification_code}</code>{:else}<p>Your code is hidden after reload.</p>{/if}
									<p>The code expires after 10 minutes. Regenerate it if needed.</p>
								</div>
							</div>
							<Button variant="outline" onclick={() => regenerate(identity)}>Regenerate code</Button>
						{/if}
						<Button variant="outline" onclick={() => remove(identity)}><Trash2 />Remove account</Button>
					{/each}
					{#if selectedPlatform}
						<form class="toolbar" onsubmit={(event) => { event.preventDefault(); add(); }}>
							<Input aria-label={`${selectedPlatform.name} username`} placeholder={`${selectedPlatform.name} username`} bind:value={username} />
							<Button type="button" variant="outline" disabled={!username.trim()} onclick={search}>Search</Button>
							<Button type="submit" disabled={!username.trim()}>Add exact username</Button>
							<Button type="button" variant="ghost" aria-label="Cancel adding platform" onclick={() => { selectedPlatform = null; username = ''; }}><X /></Button>
						</form>
						{#each matches as match (match.id)}
							<button class="platform-title" type="button" onclick={() => username = match.username}>
								{#if match.profile_picture_url}<img class="avatar" src={match.profile_picture_url} alt="" />{/if}
								<span><strong>@{match.username}</strong>{#if match.name}<br />{match.name}{/if}</span>
							</button>
						{/each}
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
