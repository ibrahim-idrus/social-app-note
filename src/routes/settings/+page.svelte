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
	let waitingFor = $state<Set<number>>(new Set());
	let pollTimer: ReturnType<typeof setInterval> | undefined;
	let polling = false;
	let availablePlatforms = $derived(platforms.filter((platform) => !identities.some((identity) => identity.platform === platform.id)));
	const terminalStates = new Set<SocialIdentity['verification_state']>(['active', 'invalid_code', 'expired', 'system_failure']);

	async function load() {
		loading = true;
		error = '';
		try { [platforms, identities] = await Promise.all([api.platforms(), api.identities()]); }
		catch (cause) { error = cause instanceof Error ? cause.message : 'Settings could not load.'; }
		finally { loading = false; }
	}
	async function add(match: SocialAccountMatch) {
		if (!selectedPlatform) return;
		error = '';
		try {
			registration = await api.createSocialIdentity(selectedPlatform.id, match);
			identities = [...identities, registration];
			username = ''; matches = [];
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
		try {
			registration = await api.regenerateSocialIdentityCode(identity.id);
			identities = identities.map((item) => item.id === identity.id ? registration! : item);
			waitingFor = new Set([...waitingFor].filter((id) => id !== identity.id));
			syncPolling();
		}
		catch (cause) { error = cause instanceof Error ? cause.message : 'Code could not be regenerated.'; }
	}
	async function remove(identity: SocialIdentity) {
		error = '';
		try {
			await api.deleteSocialIdentity(identity.id);
			identities = identities.filter((item) => item.id !== identity.id);
			waitingFor = new Set([...waitingFor].filter((id) => id !== identity.id));
			syncPolling();
		}
		catch (cause) { error = cause instanceof Error ? cause.message : 'Identity could not be removed.'; }
	}
	function sent(identity: SocialIdentity) {
		waitingFor = new Set(waitingFor).add(identity.id);
		syncPolling();
	}
	async function pollIdentities() {
		if (polling) return;
		polling = true;
		try {
			identities = await api.identities();
			waitingFor = new Set([...waitingFor].filter((id) => {
				const identity = identities.find((item) => item.id === id);
				return identity && !terminalStates.has(identity.verification_state);
			}));
		} catch (cause) { error = cause instanceof Error ? cause.message : 'Verification status could not be refreshed.'; }
		finally { polling = false; syncPolling(); }
	}
	function syncPolling() {
		const shouldPoll = identities.some((identity) => waitingFor.has(identity.id) && identity.verification_state === 'waiting');
		if (shouldPoll && !pollTimer) pollTimer = setInterval(pollIdentities, 3000);
		if (!shouldPoll && pollTimer) { clearInterval(pollTimer); pollTimer = undefined; }
	}
	function stopPolling() {
		if (pollTimer) clearInterval(pollTimer);
		pollTimer = undefined;
	}
	onMount(() => { void load(); return stopPolling; });
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
						<div class="platform-title"><span class="platform-icon"><MessageCircle size={20} /></span><div><strong>{platforms.find((platform) => platform.id === identity.platform)?.name ?? identity.platform}</strong><p class="field-help">@{identity.username}</p></div></div>
						{#if identity.verification_state === 'active'}
							<div class="notice"><strong>✓ Instagram connected</strong><p>@{identity.username}</p></div>
						{:else if identity.verification_state === 'expired'}
							<div class="notice error" role="status"><strong>Your verification code has expired</strong><p>Regenerate it and send the new code to Instagram.</p></div>
							<Button variant="outline" onclick={() => regenerate(identity)}>Regenerate code</Button>
						{:else if identity.verification_state === 'invalid_code'}
							<div class="notice error" role="status"><strong>That verification code is no longer valid.</strong><p>Regenerate a code and try again.</p></div>
							<Button variant="outline" onclick={() => regenerate(identity)}>Regenerate code</Button>
						{:else if identity.verification_state === 'system_failure'}
							<div class="notice error" role="status"><strong>Instagram verification needs another try.</strong><p>No automatic retry was sent. Remove and reconnect this account to try again.</p></div>
						{:else if waitingFor.has(identity.id)}
							<div class="notice" role="status"><strong>Waiting for Instagram verification…</strong><p>Keep this page open while NoteDesk checks the connection.</p></div>
						{:else if identity.status === 'pending'}
							<div class="notice verification-instructions">
								<div>
									<p><strong>DM this code to @{(registration?.id === identity.id ? registration.instagram_account : identity.instagram_account)?.username}</strong></p>
									{#if registration?.id === identity.id}<code>{registration.verification_code}</code>{:else}<p>Your code is hidden after reload.</p>{/if}
									<p>The code expires after 10 minutes. Regenerate it if needed.</p>
								</div>
							</div>
							<Button onclick={() => sent(identity)}>I've sent the code</Button>
							<Button variant="outline" onclick={() => regenerate(identity)}>Regenerate code</Button>
						{/if}
						<Button variant="outline" onclick={() => remove(identity)}><Trash2 />Remove account</Button>
					{/each}
					{#if selectedPlatform}
						<form class="toolbar" onsubmit={(event) => { event.preventDefault(); search(); }}>
							<Input aria-label={`${selectedPlatform.name} username`} placeholder={`${selectedPlatform.name} username`} bind:value={username} />
							<Button type="submit" disabled={!username.trim()}>Search</Button>
							<Button type="button" variant="ghost" aria-label="Cancel adding platform" onclick={() => { selectedPlatform = null; username = ''; }}><X /></Button>
						</form>
						{#each matches as match (match.id)}
							<div class="account-result">
								{#if match.profile_picture_url}<img src={match.profile_picture_url} alt="" />{:else}<span class="avatar-fallback" aria-hidden="true">IG</span>{/if}
								<div><strong>{match.name || `@${match.username}`}</strong><span>@{match.username}</span><a href={`https://www.instagram.com/${match.username}/`} target="_blank" rel="noreferrer">View Instagram profile</a></div>
							</div>
							<p><strong>Is this your account?</strong></p>
							<div class="toolbar"><Button onclick={() => add(match)}>Yes, connect this account</Button><Button variant="outline" onclick={() => { matches = []; username = ''; }}>No, search again</Button></div>
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
