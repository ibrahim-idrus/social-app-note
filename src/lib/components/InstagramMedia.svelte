<script lang="ts">
	import type { InstagramAttachment } from '$lib/api';
	import { Check, Copy } from '@lucide/svelte';
	let { attachments = [] }: { attachments?: InstagramAttachment[] } = $props();
	let copied = $state('');
	function reelEmbed(url: string) {
		const path = new URL(url).pathname.replace(/\/$/, '');
		return `https://www.instagram.com${path}/embed/`;
	}
	async function copy(url: string) {
		await navigator.clipboard.writeText(url);
		copied = url;
	}
</script>

{#if attachments.length}
	<section class="instagram-media panel" aria-label="Instagram media">
		{#each attachments as attachment}
			{#if attachment.type === 'ig_post' && attachment.url.includes('lookaside.fbsbx.com')}
				<img src={attachment.url} alt={attachment.alt || 'Shared Instagram post'} loading="lazy" />
			{:else if attachment.type === 'ig_reel'}
				<iframe src={reelEmbed(attachment.permalink || attachment.url)} title={attachment.alt || 'Shared Instagram Reel'} loading="lazy" allowfullscreen></iframe>
			{/if}
			{@const link = attachment.permalink || (attachment.type === 'ig_reel' ? attachment.url : '')}
			{#if link}
				<div class="permalink">
					<a href={link} target="_blank" rel="noopener noreferrer" aria-label="Open on Instagram">{link}</a>
					<button type="button" aria-label="Copy Instagram link" onclick={() => copy(link)}>{#if copied === link}<Check size={16} />Copied{:else}<Copy size={16} />Copy{/if}</button>
				</div>
			{/if}
		{/each}
	</section>
{/if}

<style>
	.instagram-media { margin-top: 24px; overflow: hidden; }
	img { display: block; width: 100%; max-height: 70vh; object-fit: contain; background: #f4f4f5; }
	iframe { display: block; width: 100%; min-height: min(760px, 80vh); border: 0; }
	.permalink { display: flex; align-items: center; gap: 8px; margin: 12px; }
	.permalink a { min-width: 0; overflow-wrap: anywhere; color: var(--primary); }
	button { display: inline-flex; align-items: center; gap: 6px; flex: none; border: 1px solid var(--border); border-radius: 8px; background: var(--background); padding: 7px 10px; cursor: pointer; }
	button:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
</style>
