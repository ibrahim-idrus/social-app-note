<script lang="ts">
	type Option = { value: string; label: string };
	let { label, options, value = [], onchange }: { label: string; options: Option[]; value: string[]; onchange: (value: string[]) => void } = $props();
	let search = $state('');
	let filtered = $derived(options.filter((option) => option.label.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase())));
	function toggle(option: string) { onchange(value.includes(option) ? value.filter((item) => item !== option) : [...value, option]); }
</script>

<details class="multi-select">
	<summary>{label}{#if value.length}<span>{value.length}</span>{/if}</summary>
	<div class="multi-select-menu">
		<input type="search" aria-label={`Search ${label.toLocaleLowerCase()}`} placeholder={`Search ${label.toLocaleLowerCase()}…`} bind:value={search} />
		<div class="multi-select-options" role="listbox" aria-label={label} aria-multiselectable="true">
			{#each filtered as option}
				<button type="button" role="option" aria-selected={value.includes(option.value)} onclick={() => toggle(option.value)}>
					<span class="check">{value.includes(option.value) ? '✓' : ''}</span>{option.label}
				</button>
			{:else}<p>No matches</p>{/each}
		</div>
	</div>
</details>

<style>
	.multi-select { position: relative; min-width: 150px; }
	summary { display: flex; min-height: 36px; align-items: center; justify-content: space-between; gap: 12px; box-sizing: border-box; border: 1px solid var(--input); border-radius: 6px; background: white; padding: 0 10px; color: #404550; font-size: 13px; cursor: pointer; list-style: none; }
	summary::-webkit-details-marker { display: none; }
	summary::after { content: '⌄'; color: #737883; }
	summary span { display: grid; width: 19px; height: 19px; place-items: center; margin-left: auto; border-radius: 999px; background: #eef0f4; font-size: 11px; font-weight: 700; }
	.multi-select-menu { position: absolute; z-index: 20; top: calc(100% + 6px); left: 0; width: 240px; border: 1px solid var(--border); border-radius: 7px; background: white; padding: 7px; box-shadow: 0 10px 30px rgb(0 0 0 / 12%); }
	input { width: 100%; height: 34px; box-sizing: border-box; border: 1px solid var(--input); border-radius: 5px; padding: 0 9px; font: inherit; }
	.multi-select-options { max-height: 220px; overflow-y: auto; margin-top: 6px; }
	button { display: flex; width: 100%; align-items: center; gap: 8px; border: 0; border-radius: 5px; background: transparent; padding: 8px; color: inherit; font: inherit; text-align: left; cursor: pointer; }
	button:hover, button:focus-visible { background: #f3f4f6; outline: none; }
	.check { width: 17px; height: 17px; flex: 0 0 auto; border: 1px solid #c8cbd2; border-radius: 4px; text-align: center; line-height: 15px; }
	button[aria-selected='true'] .check { border-color: #20242c; background: #20242c; color: white; }
	p { margin: 10px 8px; color: #737883; font-size: 12px; }
	@media (max-width: 760px) { .multi-select { width: 100%; } .multi-select-menu { width: 100%; box-sizing: border-box; } }
</style>
