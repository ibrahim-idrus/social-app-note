<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { api, ApiError } from '$lib/api';
	import { appState } from '$lib/app-state.svelte';
	import { Button } from '$lib/components/ui/button';
	import { BookOpenText, House, LogOut, Menu, NotebookPen, Plus, Settings, X } from '@lucide/svelte';
	import { onMount } from 'svelte';

	let { children } = $props();
	let open = $state(false);
	let loading = $state(true);
	let error = $state('');
	const links = [
		{ href: resolve('/dashboard'), label: 'Dashboard', icon: House },
		{ href: resolve('/notes'), label: 'Notes', icon: NotebookPen },
		{ href: resolve('/settings'), label: 'Settings', icon: Settings }
	];
	async function loadProfile() {
		loading = true; error = '';
		try { appState.user = await api.profile(); }
		catch (cause) {
			if (cause instanceof ApiError && cause.status === 401) { await goto(resolve('/login')); return; }
			error = cause instanceof Error ? cause.message : 'Profile could not load.';
		} finally { loading = false; }
	}
	async function logout() {
		try { await api.logout(); }
		finally { appState.user = null; await goto(resolve('/login')); }
	}
	onMount(loadProfile);
</script>

{#if loading}<div class="state-box" aria-live="polite"><div><p>Loading workspace…</p></div></div>
{:else if error}<div class="state-box" role="alert"><div><h2>Workspace couldn’t load</h2><p>{error}</p><Button onclick={loadProfile}>Try again</Button></div></div>
{:else if appState.user}
<div class="app-frame">
	<header class="mobile-bar">
		<a class="brand" href={resolve('/dashboard')}><span class="brand-mark"><BookOpenText size={18} /></span>NoteDesk</a>
		<Button variant="ghost" size="icon" aria-label={open ? 'Close navigation' : 'Open navigation'} onclick={() => open = !open}>{#if open}<X />{:else}<Menu />{/if}</Button>
	</header>
	{#if open}<button class="nav-scrim" aria-label="Close navigation" onclick={() => open = false}></button>{/if}
	<aside class:open class="sidebar">
		<a class="brand desktop-brand" href={resolve('/dashboard')}><span class="brand-mark"><BookOpenText size={18} /></span>NoteDesk</a>
		<nav aria-label="Main navigation">
			{#each links as item}
				<a href={item.href} class:active={page.url.pathname.startsWith(item.href)} onclick={() => open = false}><item.icon size={18} />{item.label}</a>
			{/each}
		</nav>
		<div class="sidebar-foot">
			<Button href={resolve('/notes/new')} class="w-full"><Plus />New note</Button>
			<button class="profile-row" aria-label="Sign out" onclick={logout}><span class="avatar">{appState.user.name.split(' ').map((part: string) => part[0]).join('').slice(0, 2)}</span><span><strong>{appState.user.name}</strong><small>{appState.user.email}</small></span><LogOut size={16} /></button>
		</div>
	</aside>
	<main class="app-main">{@render children()}</main>
</div>
{/if}
