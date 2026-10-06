<script lang="ts">
	import { goto } from '$app/navigation';
	import { page as route } from '$app/state';
	import { resolve } from '$app/paths';
	import { api, type Note } from '$lib/api';
	import { formatDate } from '$lib/app-utils';
	import { notesToMarkdown } from '$lib/note-export';
	import SearchHighlightedText from '$lib/components/SearchHighlightedText.svelte';
	import { Button, buttonVariants } from '$lib/components/ui/button';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import { Input } from '$lib/components/ui/input';
	import { AlertCircle, ChevronDown, ChevronLeft, ChevronRight, Download, FileX2, LoaderCircle, MessageCircle, Plus, Search, Trash2 } from '@lucide/svelte';
	import { onMount } from 'svelte';

	const sourceOptions = [['manual','Manual'],['instagram','Instagram'],['facebook','Facebook Messenger']] as const;
	let query=$state(''), searchIn=$state('all'), sources=$state<string[]>([]), tags=$state<string[]>([]), sort=$state('updated_at'), order=$state('desc'), page=$state(1);
	let notes=$state<Note[]>([]), availableTags=$state<string[]>([]), total=$state(0), loading=$state(true), error=$state(''), actionError=$state(''), requestID=0;
	let selected=$state(new Set<number>()); const pageSize=5; let pageCount=$derived(Math.max(1,Math.ceil(total/pageSize))); let allVisible=$derived(notes.length>0&&notes.every((note)=>selected.has(note.id)));
	function readURL(){const p=route.url.searchParams;query=p.get('q')??'';searchIn=p.get('search_in')??'all';sources=p.getAll('source');tags=p.getAll('tag');sort=p.get('sort')??(query?'relevance':'updated_at');order=p.get('order')??'desc';page=Number(p.get('page'))||1;}
	async function load(){const request=++requestID;loading=true;error='';try{const [result,allTags]=await Promise.all([api.listNotes({query,searchIn,sources,tags,sort,order,page,pageSize}),api.tags()]);if(request!==requestID)return;notes=result.notes.map((note)=>({...note,tags:note.tags??[]}));total=result.total;availableTags=allTags;selected=new Set([...selected].filter((id)=>notes.some((note)=>note.id===id)));}catch(cause){if(request!==requestID)return;error=cause instanceof Error?cause.message:'Notes could not load.';}finally{if(request===requestID)loading=false;}}
	async function commit(reset=true){if(reset)page=1;if(!query&&sort==='relevance')sort='updated_at';const p=new URLSearchParams();if(query){p.set('q',query.trim());p.set('search_in',searchIn)}sources.forEach((value)=>p.append('source',value));tags.forEach((value)=>p.append('tag',value));if(sort!==(query?'relevance':'updated_at'))p.set('sort',sort);if(order!=='desc')p.set('order',order);if(page>1)p.set('page',String(page));await goto(`${resolve('/notes')}${p.size?'?'+p:''}`,{replaceState:false,noScroll:true,keepFocus:true});await load();}
	function toggle(list:string[],value:string){return list.includes(value)?list.filter((item)=>item!==value):[...list,value]}
	function toggleNote(id:number){const next=new Set(selected);next.has(id)?next.delete(id):next.add(id);selected=next;}
	function toggleAll(){const next=new Set(selected);allVisible?notes.forEach((note)=>next.delete(note.id)):notes.forEach((note)=>next.add(note.id));selected=next;}
	function clear(){query='';searchIn='all';sources=[];tags=[];sort='updated_at';order='desc';commit();}
	function download(){const blob=new Blob([notesToMarkdown(notes.filter((note)=>selected.has(note.id)))],{type:'text/markdown;charset=utf-8'});const link=document.createElement('a');link.href=URL.createObjectURL(blob);link.download=`notes-${new Date().toISOString().slice(0,10)}.md`;link.click();URL.revokeObjectURL(link.href);}
	async function removeSelected(){if(!confirm(`Delete ${selected.size} selected note${selected.size===1?'':'s'}? This cannot be undone.`))return;const failed:number[]=[];actionError='';for (const id of [...selected]){try{await api.deleteNote(id)}catch{failed.push(id)}}selected = new Set(failed);if(failed.length)actionError=`${failed.length} note${failed.length===1?'':'s'} could not be deleted and remain selected.`;await load();}
	onMount(()=>{readURL();load();const back=()=>{readURL();load()};addEventListener('popstate',back);return()=>removeEventListener('popstate',back)});
</script>
<svelte:head><title>Notes · NoteDesk</title></svelte:head>
<div class="page">
	<header class="page-header"><div><span class="eyebrow">Library</span><h1>Notes</h1><p class="subtle">Search and review everything you’ve saved.</p></div><Button href={resolve('/notes/new')}><Plus/>New note</Button></header>
	<form class="toolbar" onsubmit={(e)=>{e.preventDefault();sort=query?'relevance':'updated_at';commit()}}>
		<div class="search-wrap"><Search size={17}/><Input aria-label="Search notes" placeholder="Search title or content…" bind:value={query}/></div>
		<select class="select" aria-label="Search in" bind:value={searchIn} onchange={()=>query&&commit()}><option value="all">Title and body</option><option value="title">Title</option><option value="content">Body</option></select><Button type="submit">Search</Button>
		<fieldset class="filter-group"><legend>Sources</legend>{#each sourceOptions as option}<label><input type="checkbox" checked={sources.includes(option[0])} onchange={()=>{sources=toggle(sources,option[0]);commit()}}/>{option[1]}</label>{/each}</fieldset>
		{#if availableTags.length}<fieldset class="filter-group"><legend>Tags</legend>{#each availableTags as tag}<label><input type="checkbox" checked={tags.includes(tag)} onchange={()=>{tags=toggle(tags,tag);commit()}}/>#{tag}</label>{/each}</fieldset>{/if}
		<select class="select" aria-label="Sort notes" bind:value={sort} onchange={()=>commit(false)}>{#if query}<option value="relevance">Relevance</option>{/if}<option value="updated_at">Recently updated</option><option value="created_at">Recently created</option><option value="title">Title</option></select>
	</form>
	{#if sources.length||tags.length}<div class="active-filters">{#each sources as source}<button onclick={()=>{sources=sources.filter((v)=>v!==source);commit()}}>Source: {source} ×</button>{/each}{#each tags as tag}<button onclick={()=>{tags=tags.filter((v)=>v!==tag);commit()}}>#{tag} ×</button>{/each}</div>{/if}
	{#if selected.size}<div class="bulk-bar"><strong>{selected.size} selected</strong><DropdownMenu.Root><DropdownMenu.Trigger class={buttonVariants({variant:'outline'})}>Actions <ChevronDown/></DropdownMenu.Trigger><DropdownMenu.Content align="end"><DropdownMenu.Item onclick={download}><Download/>Download as Markdown</DropdownMenu.Item><DropdownMenu.Item variant="destructive" onclick={removeSelected}><Trash2/>Delete</DropdownMenu.Item></DropdownMenu.Content></DropdownMenu.Root></div>{/if}
	{#if actionError}<p class="field-error" role="alert">{actionError}</p>{/if}
	<section class="panel" aria-live="polite">
		{#if loading}<div class="state-box"><div><LoaderCircle class="spinner" size={28}/><h2>Loading notes</h2></div></div>
		{:else if error}<div class="state-box" role="alert"><div><AlertCircle size={28}/><h2>Notes couldn’t load</h2><p>{error}</p><Button onclick={load}>Try again</Button></div></div>
		{:else if notes.length===0}<div class="state-box"><div><FileX2 size={28}/><h2>{query?'No matching notes':'No notes found'}</h2><p>{query||sources.length||tags.length?'Try different filters.':'Create your first note.'}</p>{#if query||sources.length||tags.length}<Button variant="outline" onclick={clear}>Clear search</Button>{/if}</div></div>
		{:else}<div class="select-all"><label><input type="checkbox" aria-label="Select all visible notes" checked={allVisible} onchange={toggleAll}/> Select all visible</label></div>{#each notes as note}<article class="note-row selectable"><input type="checkbox" aria-label={`Select ${note.title}`} checked={selected.has(note.id)} onchange={()=>toggleNote(note.id)}/><div><h3><a href={resolve('/notes/[id]',{id:String(note.id)})}><SearchHighlightedText text={note.title} ranges={note.title_matches}/></a></h3>{#if note.sections?.length}<div class="search-sections">{#each note.sections as section}<a href={`${resolve('/notes/[id]',{id:String(note.id)})}#${section.anchor}`}><strong>{section.heading}</strong><p><SearchHighlightedText text={section.excerpt} ranges={section.matches}/></p></a>{/each}</div>{:else}<p>{note.content_markdown.replace(/[#*_>`-]/g,'')}</p>{/if}{#if note.tags.length}<div class="tag-list">{#each note.tags as tag}<span class="tag-chip">#{tag}</span>{/each}</div>{/if}</div><div class="note-meta"><span class="source">{#if note.source!=='manual'}<MessageCircle size={12}/>{/if}{note.source==='facebook'?'Facebook Messenger':note.source}</span><br/>Updated {formatDate(note.updated_at)}</div></article>{/each}<div class="pagination"><span>{total} note{total===1?'':'s'} · Page {page} of {pageCount}</span><div><Button variant="outline" size="icon" aria-label="Previous page" disabled={page===1} onclick={()=>{page--;commit(false)}}><ChevronLeft/></Button><Button variant="outline" size="icon" aria-label="Next page" disabled={page>=pageCount} onclick={()=>{page++;commit(false)}}><ChevronRight/></Button></div></div>{/if}
	</section>
</div>
