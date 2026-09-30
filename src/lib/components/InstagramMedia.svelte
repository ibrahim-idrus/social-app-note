<script lang="ts">
	import type { InstagramAttachment } from '$lib/api';
	let { attachments = [] }: { attachments?: InstagramAttachment[] } = $props();
	function reelEmbed(url: string) {
		const path = new URL(url).pathname.replace(/\/$/, '');
		return `https://www.instagram.com${path}/embed/`;
	}
</script>

{#if attachments.length}
	<section class="instagram-media panel" aria-label="Instagram media">
		{#each attachments as attachment}
			{#if attachment.type === 'ig_post'}
				<img src={attachment.url} alt={attachment.alt || 'Shared Instagram post'} loading="lazy" />
			{:else if attachment.type === 'ig_reel'}
				<iframe src={reelEmbed(attachment.url)} title={attachment.alt || 'Shared Instagram Reel'} loading="lazy" allowfullscreen></iframe>
				<a href={attachment.url} target="_blank" rel="noopener noreferrer">Open on Instagram</a>
			{/if}
		{/each}
	</section>
{/if}

<style>
	.instagram-media { margin-top: 24px; overflow: hidden; }
	img { display: block; width: 100%; max-height: 70vh; object-fit: contain; background: #f4f4f5; }
	iframe { display: block; width: 100%; min-height: min(760px, 80vh); border: 0; }
	a { display: inline-block; margin: 12px; }
</style>
