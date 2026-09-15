<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { appState, persist } from '$lib/app-state.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { BookOpenText, Eye, EyeOff } from '@lucide/svelte';

	let name = $state(''); let email = $state(''); let password = $state(''); let confirm = $state(''); let show = $state(false); let error = $state('');
	function submit(event: SubmitEvent) {
		event.preventDefault();
		error = name.trim().length < 2 ? 'Enter your name.' : !/^\S+@\S+\.\S+$/.test(email) ? 'Enter a valid email address.' : password.length < 8 ? 'Use at least 8 characters for your password.' : password !== confirm ? 'Passwords do not match.' : '';
		if (!error) { appState.name = name.trim(); appState.email = email.trim(); persist(); goto(resolve('/dashboard')); }
	}
</script>

<svelte:head><title>Create account · NoteDesk</title></svelte:head>
<div class="auth-page">
	<section class="auth-side"><a class="brand" href={resolve('/register')}><span class="brand-mark"><BookOpenText size={18} /></span>NoteDesk</a><div class="auth-quote"><p>Your notes should feel like a workbench, not another feed.</p><small>Capture ideas manually or send text to your dedicated Instagram inbox.</small></div><small>No social account connection required</small></section>
	<main class="auth-main"><div class="auth-card"><span class="eyebrow">Get started</span><h1>Create your workspace</h1><p class="subtle">Everything in this prototype stays in this browser.</p>
		<form onsubmit={submit} novalidate>
			<div class="field"><Label for="name">Name</Label><Input id="name" autocomplete="name" bind:value={name} /></div>
			<div class="field"><Label for="email">Email</Label><Input id="email" type="email" autocomplete="email" bind:value={email} /></div>
			<div class="field"><Label for="password">Password</Label><div class="password-wrap"><Input id="password" type={show ? 'text' : 'password'} autocomplete="new-password" bind:value={password} /><Button type="button" variant="ghost" size="icon" aria-label={show ? 'Hide password' : 'Show password'} onclick={() => show = !show}>{#if show}<EyeOff />{:else}<Eye />{/if}</Button></div><span class="field-help">At least 8 characters.</span></div>
			<div class="field"><Label for="confirm">Confirm password</Label><Input id="confirm" type={show ? 'text' : 'password'} autocomplete="new-password" bind:value={confirm} />{#if error}<span class="field-error" role="alert">{error}</span>{/if}</div>
			<Button type="submit" size="lg">Create account</Button>
		</form><p class="auth-footer">Already have an account? <a href={resolve('/login')}>Sign in</a></p></div></main>
</div>
