<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { api } from '$lib/api';
	import { appState } from '$lib/app-state.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { BookOpenText, Eye, EyeOff } from '@lucide/svelte';

	let email = $state('');
	let password = $state('');
	let show = $state(false);
	let error = $state('');
	let loading = $state(false);
	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = !/^\S+@\S+\.\S+$/.test(email) ? 'Enter a valid email address.' : password.length < 6 ? 'Password must be at least 6 characters.' : '';
		if (error) return;
		loading = true;
		try { appState.user = (await api.login({ email, password })).user; await goto(resolve('/dashboard')); }
		catch (cause) { error = cause instanceof Error ? cause.message : 'Sign in failed.'; }
		finally { loading = false; }
	}
</script>

<svelte:head><title>Sign in · NoteDesk</title></svelte:head>
<div class="auth-page">
	<section class="auth-side">
		<a class="brand" href={resolve('/login')}><span class="brand-mark"><BookOpenText size={18} /></span>NoteDesk</a>
		<div class="auth-quote"><p>Keep the useful things you find, without breaking your flow.</p><small>Manual notes and a dedicated Instagram inbox, in one quiet workspace.</small></div>
		<small>Your notes stay in your private workspace.</small>
	</section>
	<main class="auth-main">
		<div class="auth-card">
			<span class="eyebrow">Welcome back</span><h1>Sign in to your workspace</h1><p class="subtle">Enter the credentials for your NoteDesk account.</p>
			<form onsubmit={submit} novalidate>
				<div class="field"><Label for="email">Email</Label><Input id="email" type="email" autocomplete="email" bind:value={email} aria-invalid={!!error} /></div>
				<div class="field"><Label for="password">Password</Label><div class="password-wrap"><Input id="password" type={show ? 'text' : 'password'} autocomplete="current-password" bind:value={password} aria-invalid={!!error} /><Button type="button" variant="ghost" size="icon" aria-label={show ? 'Hide password' : 'Show password'} onclick={() => show = !show}>{#if show}<EyeOff />{:else}<Eye />{/if}</Button></div>{#if error}<span class="field-error" role="alert">{error}</span>{/if}</div>
				<Button type="submit" size="lg" disabled={loading}>{loading ? 'Signing in…' : 'Sign in'}</Button>
			</form>
			<p class="auth-footer">New to NoteDesk? <a href={resolve('/register')}>Create an account</a></p>
		</div>
	</main>
</div>
