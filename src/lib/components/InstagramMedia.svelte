<script lang="ts">
	import { api, type InstagramAttachment, type InstagramCachedMedia } from '$lib/api';
	import { Check, Copy } from '@lucide/svelte';
	let { attachments = [], media = [], noteId }: { attachments?: InstagramAttachment[]; media?: InstagramCachedMedia[]; noteId: number } = $props();
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
		{#each attachments as attachment, index}
			{#each media.filter((item) => Math.floor(item.position / 1000) === index) as item}
				{#if item.kind === 'video'}<video src={api.instagramMediaURL(noteId, item.cache_key)} controls preload="metadata"><track kind="captions" /></video>
				{:else}<img src={api.instagramMediaURL(noteId, item.cache_key)} alt={attachment.alt || 'Shared Instagram post'} loading="lazy" />{/if}
			{/each}
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
	video { display: block; width: 100%; max-height: 70vh; background: #000; }
	.permalink { display: flex; align-items: center; gap: 8px; margin: 12px; }
	.permalink a { min-width: 0; overflow-wrap: anywhere; color: var(--primary); }
	button { display: inline-flex; align-items: center; gap: 6px; flex: none; border: 1px solid var(--border); border-radius: 8px; background: var(--background); padding: 7px 10px; cursor: pointer; }
	button:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
</style>
