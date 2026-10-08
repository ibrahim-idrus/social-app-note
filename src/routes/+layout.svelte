<script lang="ts">
	import './layout.css';
	import favicon from '$lib/assets/favicon.svg';
	import AppShell from '$lib/components/AppShell.svelte';
	import { page } from '$app/state';

	let { children } = $props();
	let publicRoute = $derived(page.route.id === '/' || page.route.id === '/app/login' || page.route.id === '/app/register');
</script>

<svelte:head><link rel="icon" href={favicon} /><title>SocialNotes</title><meta name="description" content="Capture and organize notes manually or from Instagram and Facebook Messenger." /></svelte:head>
{#if publicRoute}{@render children()}{:else}<AppShell>{@render children()}</AppShell>{/if}
