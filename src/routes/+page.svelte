<script lang="ts">
	import { resolve } from '$app/paths';
	import { ArrowRight, BookOpenText, Camera, MessageCircle, NotebookPen } from '@lucide/svelte';
</script>

<svelte:head><title>SocialNotes · Keep what matters</title></svelte:head>
<div class="landing">
	<header><a class="brand" href={resolve('/')}><span class="brand-mark"><BookOpenText size={18} /></span>SocialNotes</a><nav aria-label="Account"><a href={resolve('/app/login')}>Sign in</a><a class="primary" href={resolve('/app/register')}>Get started</a></nav></header>
	<main>
		<section class="hero"><span class="eyebrow">Your private knowledge workspace</span><h1>Your social feed, turned into a useful notebook.</h1><p>Save ideas manually or send posts through Instagram and Facebook Messenger. Search, organize, and revisit everything without the noise.</p><div class="actions"><a class="primary" href={resolve('/app/register')}>Start taking notes <ArrowRight size={17} /></a><a href={resolve('/app/login')}>Sign in</a></div></section>
		<section class="features" aria-label="Features"><article><NotebookPen /><h2>Write your way</h2><p>Create focused Markdown notes in a calm workspace.</p></article><article><Camera /><h2>Capture from Instagram</h2><p>Send useful posts to your dedicated social inbox.</p></article><article><MessageCircle /><h2>Save from Messenger</h2><p>Turn Facebook messages and shared posts into notes.</p></article></section>
	</main>
</div>

<style>
	.landing{min-height:100vh;background:linear-gradient(145deg,#fafaff 0%,#f4f2ff 48%,#fff 100%)}header{display:flex;width:min(100% - 40px,1120px);margin:auto;padding:24px 0;align-items:center;justify-content:space-between}nav,.actions{display:flex;align-items:center;gap:12px}nav a,.actions a{min-height:42px;display:inline-flex;align-items:center;gap:8px;padding:0 16px;border-radius:7px;color:var(--foreground);font-size:14px;font-weight:650;text-decoration:none}.primary{background:var(--primary);color:white!important}.hero{width:min(100% - 40px,860px);margin:auto;padding:110px 0 90px;text-align:center}.hero h1{margin:18px auto 0;max-width:820px;font-size:clamp(42px,7vw,76px);line-height:1.02}.hero>p{max-width:650px;margin:26px auto;color:#606574;font-size:18px}.actions{justify-content:center;margin-top:32px}.features{display:grid;width:min(100% - 40px,1000px);grid-template-columns:repeat(3,1fr);gap:16px;margin:0 auto;padding-bottom:90px}.features article{border:1px solid var(--border);border-radius:12px;background:#ffffffb8;padding:26px}.features svg{color:var(--primary)}.features h2{margin:18px 0 6px}.features p{margin:0;color:#6e7380;font-size:14px}@media(max-width:700px){header nav a:first-child{display:none}.hero{padding:72px 0 58px}.features{grid-template-columns:1fr}.actions{align-items:stretch;flex-direction:column}.actions a{justify-content:center}}
</style>