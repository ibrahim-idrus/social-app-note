<script lang="ts">
	import './layout.css';
	import favicon from '$lib/assets/favicon.svg';
	import AppShell from '$lib/components/AppShell.svelte';
	import { page } from '$app/state';
	import { base } from '$app/paths';

	let { children } = $props();
	let publicRoute = $derived(page.route.id === '/login' || page.route.id === '/register');
</script>

<svelte:head><base href={`${base}/`} /><link rel="icon" href={favicon} /><title>NoteDesk</title><meta name="description" content="Capture and organize notes from your desk or Instagram inbox." /></svelte:head>
{#if publicRoute}{@render children()}{:else}<AppShell>{@render children()}</AppShell>{/if}
