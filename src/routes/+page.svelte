<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { onMount } from 'svelte';
	onMount(() => goto(resolve('/dashboard'), { replaceState: true }));
</script>

<p class="sr-only">Opening dashboard…</p>
