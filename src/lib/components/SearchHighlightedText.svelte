<script lang="ts">
	import type { MatchRange } from '$lib/api';
	let { text, ranges = [] }: { text: string; ranges?: MatchRange[] } = $props();
	let parts = $derived.by(() => {
		const chars = [...text]; let cursor = 0; const result: { text: string; marked: boolean }[] = [];
		for (const range of ranges.filter((r) => r.start >= cursor && r.end > r.start && r.end <= chars.length)) {
			if (range.start > cursor) result.push({ text: chars.slice(cursor, range.start).join(''), marked: false });
			result.push({ text: chars.slice(range.start, range.end).join(''), marked: true }); cursor = range.end;
		}
		if (cursor < chars.length) result.push({ text: chars.slice(cursor).join(''), marked: false });
		return result;
	});
</script>
{#each parts as part}{#if part.marked}<mark>{part.text}</mark>{:else}{part.text}{/if}{/each}
