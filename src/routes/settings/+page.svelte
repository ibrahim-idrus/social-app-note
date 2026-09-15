<script lang="ts">
	import { appState, persist } from '$lib/app-state.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { AlertCircle, CheckCircle2, LoaderCircle, MessageCircle, Search, Send, ShieldCheck, Trash2 } from '@lucide/svelte';

	type Flow = 'empty' | 'search' | 'loading' | 'results' | 'confirm' | 'unavailable' | 'error';
	let name = $state(appState.name); let email = $state(appState.email); let saved = $state(false);
	let flow = $state<Flow>(appState.instagram ? 'empty' : 'empty'); let username = $state(''); let selected = $state(''); let removeOpen = $state(false);
	const avatar = 'https://i.pravatar.cc/96?img=47';
	function saveProfile() { appState.name = name.trim() || appState.name; appState.email = email.trim() || appState.email; persist(); saved = true; setTimeout(() => saved = false, 1400); }
	function searchAccount(event: SubmitEvent) {
		event.preventDefault(); const normalized = username.trim().replace(/^@/, '').toLowerCase(); if (!normalized) return;
		flow = 'loading'; setTimeout(() => { selected = normalized; flow = normalized === 'reserved' ? 'unavailable' : normalized === 'serviceerror' ? 'error' : 'results'; }, 850);
	}
	function connect(status: 'active' | 'pending') { appState.instagram = { username: selected, displayName: selected === 'mayachen' ? 'Maya Chen' : selected.split(/[._]/).map((part) => part[0]?.toUpperCase() + part.slice(1)).join(' '), avatar, status, verifiedAt: status === 'active' ? new Date().toISOString() : undefined }; persist(); flow = 'empty'; }
	function removeIdentity() { appState.instagram = null; persist(); removeOpen = false; username = ''; selected = ''; flow = 'empty'; }
	function activate() { if (appState.instagram) { appState.instagram.status = 'active'; appState.instagram.verifiedAt = new Date().toISOString(); persist(); } }
</script>

<svelte:head><title>Settings · NoteDesk</title></svelte:head>
<div class="page page-narrow">
	<header class="page-header"><div><span class="eyebrow">Workspace</span><h1>Settings</h1><p class="subtle">Manage your profile and optional Instagram capture identity.</p></div></header>
	<div class="settings-stack">
		<section class="panel"><div class="panel-head"><div><h2>Profile</h2><p class="field-help">Used to personalize this workspace.</p></div></div><div class="panel-body"><div class="profile-form"><div class="field"><Label for="profile-name">Name</Label><Input id="profile-name" bind:value={name} /></div><div class="field"><Label for="profile-email">Email</Label><Input id="profile-email" type="email" bind:value={email} /></div><Button onclick={saveProfile}>{saved ? 'Saved' : 'Save profile'}</Button></div></div></section>

		<section class="panel"><div class="panel-head"><div><h2>Social capture</h2><p class="field-help">One identity per supported platform.</p></div></div><div class="instagram-card">
			<div class="platform-title"><span class="platform-icon"><MessageCircle size={20} /></span><div><strong>Instagram</strong><p class="field-help">Register an identity that sends text DMs to the dedicated NoteDesk inbox. You do not connect or authorize your personal account.</p></div></div>
			{#if appState.instagram}
				<div style="display:grid;gap:16px;margin-top:20px">
					<div class="account-result"><img src={appState.instagram.avatar} alt="" /><div><strong>{appState.instagram.displayName}</strong><span>@{appState.instagram.username}</span></div><span class="status-line"><span class:pending={appState.instagram.status === 'pending'} class="status-dot"></span><strong style="font-size:12px;text-transform:capitalize">{appState.instagram.status}</strong></span></div>
					{#if appState.instagram.status === 'active'}<div class="notice"><ShieldCheck size={18} /><span><strong>Connected and ready.</strong><br />Text DMs from this identity will appear as Instagram notes. Imported notes remain if the identity is removed.</span></div>
					{:else}<div class="notice"><Send size={18} /><span><strong>Waiting for the first DM.</strong><br />Send a text message from @{appState.instagram.username} to the dedicated inbox to finish activation. Earlier unmatched messages are not imported.</span></div><Button onclick={activate}><CheckCircle2 />Simulate first DM</Button>{/if}
					<div><Button variant="outline" onclick={() => removeOpen = true}><Trash2 />Remove identity</Button><p class="field-help" style="margin-top:8px">To use another Instagram identity, remove this one first.</p></div>
				</div>
			{:else if flow === 'empty'}
				<div style="margin-top:20px"><Button onclick={() => flow = 'search'}><MessageCircle />Set up Instagram</Button></div>
			{:else}
				<div style="display:grid;gap:16px;margin-top:20px">
					{#if flow === 'search' || flow === 'loading' || flow === 'unavailable' || flow === 'error'}
						<form class="toolbar" style="margin:0" onsubmit={searchAccount}><div class="search-wrap"><Search size={17} /><Input aria-label="Instagram username" placeholder="Instagram username" bind:value={username} disabled={flow === 'loading'} /></div><Button type="submit" disabled={flow === 'loading'}>{#if flow === 'loading'}<LoaderCircle class="spinner" />Searching{:else}Search{/if}</Button><Button type="button" variant="ghost" onclick={() => flow = 'empty'}>Cancel</Button></form>
					{/if}
					{#if flow === 'search'}<p class="field-help">Try any username. Use “reserved” for unavailable or “serviceerror” for a service error.</p>{/if}
					{#if flow === 'loading'}<div class="notice" aria-live="polite"><LoaderCircle class="spinner" size={18} />Checking Instagram for @{username.replace(/^@/, '')}…</div>{/if}
					{#if flow === 'unavailable'}<div class="notice error" role="alert"><AlertCircle size={18} /><span><strong>This identity is unavailable.</strong><br />Choose a different Instagram username. For privacy, no ownership details are shown.</span></div>{/if}
					{#if flow === 'error'}<div class="notice error" role="alert"><AlertCircle size={18} /><span><strong>Instagram search is temporarily unavailable.</strong><br />Try again, or continue with the pending setup fallback.</span></div><Button variant="outline" onclick={() => { selected = username.trim().replace(/^@/, ''); flow = 'confirm'; }}>Continue as pending</Button>{/if}
					{#if flow === 'results'}<div><p class="field-help" style="margin-bottom:8px">1 account found</p><button class="account-result" style="width:100%;text-align:left;background:white" onclick={() => flow = 'confirm'}><img src={avatar} alt="" /><div><strong>{selected === 'mayachen' ? 'Maya Chen' : selected.split(/[._]/).map((part) => part[0]?.toUpperCase() + part.slice(1)).join(' ')}</strong><span>@{selected}</span></div><span class="source">Select</span></button></div>{/if}
					{#if flow === 'confirm'}<div class="panel-body" style="border:1px solid var(--border);border-radius:7px"><h2 style="margin:0 0 14px">Confirm Instagram identity</h2><div class="account-result"><img src={avatar} alt="" /><div><strong>{selected.split(/[._]/).map((part) => part[0]?.toUpperCase() + part.slice(1)).join(' ')}</strong><span>@{selected}</span></div></div><div class="notice" style="margin-top:14px"><ShieldCheck size={18} />Only one Instagram identity can be registered. It must be removed before a replacement is added.</div><div class="form-actions"><Button variant="outline" onclick={() => flow = 'search'}>Cancel</Button><Button variant="outline" onclick={() => connect('pending')}>Save as pending</Button><Button onclick={() => connect('active')}>Confirm identity</Button></div></div>{/if}
				</div>
			{/if}
		</div></section>
	</div>
</div>

<Dialog.Root bind:open={removeOpen}><Dialog.Content><Dialog.Header><Dialog.Title>Remove Instagram identity?</Dialog.Title><Dialog.Description>Future DMs from @{appState.instagram?.username} will no longer create notes. Existing imported notes stay in your library. Remove it before registering a replacement.</Dialog.Description></Dialog.Header><div class="dialog-actions"><Button variant="outline" onclick={() => removeOpen = false}>Cancel</Button><Button variant="destructive" onclick={removeIdentity}>Remove identity</Button></div></Dialog.Content></Dialog.Root>
