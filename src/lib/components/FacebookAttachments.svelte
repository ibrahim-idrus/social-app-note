<script lang="ts">
	import type { FacebookAttachment } from '$lib/api';
	import { ExternalLink } from '@lucide/svelte';

	let { attachments = [] }: { attachments?: FacebookAttachment[] } = $props();
</script>

{#if attachments.length}
	<section class="facebook-attachments" aria-label="Shared Facebook posts">
		{#each attachments as attachment}
			<a class="preview panel" href={attachment.url} target="_blank" rel="noopener noreferrer">
				<span class="copy">
					<strong>{attachment.title || (attachment.type === 'reel' ? 'Shared Facebook Reel' : 'Shared Facebook post')}</strong>
					<small>{attachment.title ? 'Saved from Facebook' : 'Preview unavailable · Open on Facebook to view'}</small>
				</span>
				<span class="icon"><ExternalLink size={18} aria-hidden="true" /></span>
			</a>
		{/each}
	</section>
{/if}

<style>
	.facebook-attachments { display: grid; gap: 10px; margin-top: 24px; }
	a { display: flex; min-height: 76px; align-items: center; justify-content: space-between; gap: 16px; padding: 14px 18px; color: inherit; font-size: 14px; text-decoration: none; }
	a:hover { border-color: #c5c8d1; background: #fafafa; }
	a:focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; }
	.copy { display: grid; gap: 4px; }
	.copy strong { font-weight: 650; }
	.copy small { color: var(--muted-foreground); font-size: 12px; }
	.icon { display: inline-flex; flex: none; color: var(--primary); }
</style>
