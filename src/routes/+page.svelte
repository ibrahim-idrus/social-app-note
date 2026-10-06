<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { onMount } from 'svelte';
	onMount(() => goto(resolve('/notes'), { replaceState: true }));
</script>

<p class="sr-only">Opening notes…</p>
