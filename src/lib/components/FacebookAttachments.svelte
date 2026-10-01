<script lang="ts">
	import type { FacebookAttachment } from '$lib/api';
	import { ExternalLink } from '@lucide/svelte';

	let { attachments = [] }: { attachments?: FacebookAttachment[] } = $props();
	function embedURL(attachment: FacebookAttachment) {
		const plugin = attachment.type === 'reel' ? 'video.php' : 'post.php';
		return `https://www.facebook.com/plugins/${plugin}?href=${encodeURIComponent(attachment.url)}&show_text=true&width=500`;
	}
</script>

{#if attachments.length}
	<section class="facebook-attachments" aria-label="Shared Facebook posts">
		{#each attachments as attachment}
			<div class="embed panel">
				<iframe
					src={embedURL(attachment)}
					title={attachment.type === 'reel' ? 'Shared Facebook Reel' : 'Shared Facebook post'}
					loading="lazy"
					allow="autoplay; clipboard-write; encrypted-media; picture-in-picture; web-share"
					allowfullscreen
				></iframe>
			<a class="panel" href={attachment.url} target="_blank" rel="noopener noreferrer">
				<span>{attachment.type === 'reel' ? 'Open shared Facebook Reel' : 'Open shared Facebook post'}</span>
				<span class="icon"><ExternalLink size={18} aria-hidden="true" /></span>
			</a>
			</div>
		{/each}
	</section>
{/if}

<style>
	.facebook-attachments { display: grid; gap: 10px; margin-top: 24px; }
	.embed { overflow: hidden; }
	iframe { display: block; width: 100%; min-height: min(760px, 80vh); border: 0; }
	.embed > a { border: 0; border-top: 1px solid var(--border); border-radius: 0; }
	a { display: flex; min-height: 56px; align-items: center; justify-content: space-between; gap: 16px; padding: 0 18px; color: inherit; font-size: 14px; font-weight: 650; text-decoration: none; }
	a:hover { border-color: #c5c8d1; background: #fafafa; }
	a:focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; }
	.icon { display: inline-flex; flex: none; color: var(--primary); }
</style>
