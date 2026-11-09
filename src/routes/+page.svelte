<script lang="ts">
	import { base } from '$app/paths';
	import { homepageLinks } from '$lib/homepage-links';
	import { ArrowRight, BookOpenText, MessageCircle, Search, Send, ShieldCheck, Video } from '@lucide/svelte';

	const links = homepageLinks(base);
</script>

<svelte:head>
	<title>SocialNotes · DM a post. Find it later.</title>
	<meta name="description" content="Send useful Instagram and Facebook posts to SocialNotes, then search and revisit them in your private notes." />
</svelte:head>

<div class="landing">
	<header><a class="brand" href={links.home}><span class="brand-mark"><BookOpenText size={18} /></span>SocialNotes</a><nav aria-label="Account"><a href={links.login}>Sign in</a><a class="primary" href={links.register}>Get started</a></nav></header>
	<main>
		<section class="hero">
			<span class="eyebrow">Instagram and Facebook posts, made findable</span>
			<h1>DM a post.<br />Find it later.</h1>
			<p>Send useful content from Instagram or Facebook Messenger to SocialNotes. It becomes a private, searchable note you can revisit when you actually need it.</p>
			<div class="actions"><a class="primary" href={links.register}>Start saving posts <ArrowRight size={17} /></a><a href={links.howItWorks}>See how it works</a></div>
		</section>

		<section class="flow" id="how-it-works" aria-labelledby="flow-title">
			<div class="section-heading"><span class="eyebrow">Three simple steps</span><h2 id="flow-title">From social post to useful note</h2></div>
			<div class="steps">
				<article><span class="step-number">1</span><span class="step-icon"><Send /></span><h3>Send a useful post</h3><p>DM content to your connected Instagram inbox or Facebook Page in Messenger.</p></article>
				<article><span class="step-number">2</span><span class="step-icon"><MessageCircle /></span><h3>It becomes a private note</h3><p>SocialNotes captures the message or shared-post reference in your account.</p></article>
				<article><span class="step-number">3</span><span class="step-icon"><Search /></span><h3>Search and revisit</h3><p>Find it by title, content, or tags instead of scrolling through saved-post collections.</p></article>
			</div>
		</section>

		<section class="comparison" aria-labelledby="comparison-title">
			<div><span class="eyebrow">Beyond the save button</span><h2 id="comparison-title">Saved posts are easy to forget. Notes are made to be found.</h2></div>
			<p>Social-app saves stay inside separate feeds and collections. SocialNotes brings captured Instagram and Facebook content into one searchable notes list, alongside notes you write yourself.</p>
		</section>

		<section class="demo" aria-labelledby="demo-title">
			<div class="demo-copy"><span class="eyebrow">Product demo</span><h2 id="demo-title">Watch a post become a note</h2><p>A short walkthrough will show the complete flow—from sending a post in a social app to finding it again in SocialNotes.</p></div>
			<div class="video-placeholder" role="img" aria-label="Product demo video coming soon"><span><Video size={30} /><strong>Demo video coming soon</strong><small>Send → capture → search</small></span></div>
		</section>

		<section class="trust" aria-labelledby="trust-title"><span class="trust-icon"><ShieldCheck /></span><div><span class="eyebrow">Private by default</span><h2 id="trust-title">Only verified social accounts create notes.</h2><p>Your notes stay in your private workspace. Social capture is optional, and connections are managed from your settings.</p></div></section>

		<section class="final-cta"><h2>Keep the post. Lose the scroll.</h2><p>Build a searchable home for the useful things you find.</p><a class="primary" href={links.register}>Get started <ArrowRight size={17} /></a></section>
	</main>
</div>

<style>
	.landing{min-height:100vh;background:linear-gradient(145deg,#fafaff 0%,#f4f2ff 48%,#fff 100%);overflow:hidden}header{display:flex;width:min(100% - 40px,1120px);margin:auto;padding:24px 0;align-items:center;justify-content:space-between}nav,.actions{display:flex;align-items:center;gap:12px}nav a,.actions a,.final-cta a{min-height:44px;display:inline-flex;align-items:center;justify-content:center;gap:8px;padding:0 17px;border-radius:8px;color:var(--foreground);font-size:14px;font-weight:650;text-decoration:none}.primary{background:var(--primary);color:white!important}.primary:hover{filter:brightness(.94)}a:focus-visible{outline:3px solid color-mix(in srgb,var(--primary) 35%,transparent);outline-offset:3px}.hero{width:min(100% - 40px,900px);margin:auto;padding:112px 0 118px;text-align:center}.hero h1{margin:18px auto 0;max-width:840px;font-size:clamp(52px,9vw,92px);letter-spacing:-.055em;line-height:.93}.hero>p{max-width:680px;margin:28px auto;color:#555b69;font-size:clamp(17px,2vw,20px);line-height:1.65}.actions{justify-content:center;margin-top:34px}.section-heading{max-width:680px}.section-heading h2,.comparison h2,.demo h2,.trust h2,.final-cta h2{margin:12px 0 0;font-size:clamp(30px,4.5vw,50px);letter-spacing:-.04em;line-height:1.06}.flow{width:min(100% - 40px,1080px);margin:auto;padding:92px 0}.steps{display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin-top:40px}.steps article{position:relative;border:1px solid var(--border);border-radius:16px;background:#ffffffc7;padding:30px;box-shadow:0 20px 50px #38306a0b}.step-icon{color:var(--primary)}.steps h3{margin:20px 0 8px;font-size:19px}.steps p,.comparison>p,.demo-copy p,.trust p,.final-cta p{margin:0;color:#656a77;line-height:1.7}.step-number{position:absolute;right:22px;top:20px;color:#8c8897;font-size:13px;font-weight:750}.comparison{display:grid;grid-template-columns:1.1fr .9fr;gap:80px;align-items:end;width:min(100% - 40px,1080px);margin:auto;padding:100px 0;border-block:1px solid var(--border)}.comparison>p{font-size:18px}.demo{display:grid;grid-template-columns:.75fr 1.25fr;gap:70px;align-items:center;width:min(100% - 40px,1080px);margin:auto;padding:110px 0}.demo-copy p{margin-top:22px}.video-placeholder{display:grid;aspect-ratio:16/9;place-items:center;border:1px solid #d9d4ed;border-radius:20px;background:radial-gradient(circle at 75% 20%,#dcd4ff 0,transparent 36%),linear-gradient(145deg,#25203c,#433b6c);box-shadow:0 30px 80px #2b245029;color:white;text-align:center}.video-placeholder span{display:grid;justify-items:center;gap:10px}.video-placeholder small{color:#d8d4e7}.trust{display:grid;grid-template-columns:auto 1fr;gap:28px;align-items:start;width:min(100% - 40px,900px);margin:0 auto 110px;padding:36px;border:1px solid var(--border);border-radius:18px;background:#fff}.trust-icon{box-sizing:content-box;padding:13px;border-radius:12px;background:#eeeaff;color:var(--primary)}.trust h2{font-size:clamp(25px,3.5vw,38px)}.trust p{margin-top:14px}.final-cta{padding:90px 20px;background:#201b34;color:white;text-align:center}.final-cta p{margin:18px 0 28px;color:#c9c5d6}.final-cta a{width:max-content;margin:auto;background:white;color:#241d3a!important}@media(max-width:760px){header nav a:first-child{display:none}.hero{padding:76px 0 86px}.hero h1{font-size:clamp(48px,15vw,70px)}.steps,.comparison,.demo{grid-template-columns:1fr}.flow{padding:70px 0}.comparison{gap:26px;padding:72px 0}.demo{gap:36px;padding:78px 0}.actions{align-items:stretch;flex-direction:column}.actions a{justify-content:center}.trust{grid-template-columns:1fr;margin-bottom:72px;padding:26px}.video-placeholder{border-radius:14px}}@media(prefers-reduced-motion:no-preference){.primary{transition:filter .15s ease,transform .15s ease}.primary:hover{transform:translateY(-1px)}}
</style>
