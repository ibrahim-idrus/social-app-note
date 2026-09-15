<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { appState } from '$lib/app-state.svelte';
	import { Button } from '$lib/components/ui/button';
	import { BookOpenText, House, LogOut, Menu, NotebookPen, Plus, Settings, X } from '@lucide/svelte';

	let { children } = $props();
	let open = $state(false);
	const links = [
		{ href: resolve('/dashboard'), label: 'Dashboard', icon: House },
		{ href: resolve('/notes'), label: 'Notes', icon: NotebookPen },
		{ href: resolve('/settings'), label: 'Settings', icon: Settings }
	];
</script>

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
			<button class="profile-row" onclick={() => goto(resolve('/login'))}><span class="avatar">{appState.name.split(' ').map((part: string) => part[0]).join('').slice(0, 2)}</span><span><strong>{appState.name}</strong><small>{appState.email}</small></span><LogOut size={16} /></button>
		</div>
	</aside>
	<main class="app-main">{@render children()}</main>
</div>
