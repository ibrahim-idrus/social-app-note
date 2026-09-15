export type NoteLike = {
	id: string;
	title: string;
	content: string;
	source: string;
	createdAt: string;
	updatedAt: string;
};

export function filterNotes<T extends NoteLike>(notes: readonly T[], query: string, source: string, sort: string): T[] {
	const term = query.trim().toLowerCase();
	return notes
		.filter((note) => (source === 'all' || note.source === source) && (!term || `${note.title} ${note.content}`.toLowerCase().includes(term)))
		.sort((a, b) => {
			if (sort === 'title-asc') return a.title.localeCompare(b.title);
			if (sort === 'created-desc') return b.createdAt.localeCompare(a.createdAt);
			return b.updatedAt.localeCompare(a.updatedAt);
		});
}

const escapeHtml = (value: string) => value.replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' })[char]!);

function inlineMarkdown(value: string) {
	return escapeHtml(value.replace(/javascript:/gi, ''))
		.replace(/`([^`]+)`/g, '<code>$1</code>')
		.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
		.replace(/\*([^*]+)\*/g, '<em>$1</em>')
		.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noreferrer">$1</a>');
}

export function renderMarkdown(markdown: string): string {
	const lines = markdown.replace(/\r/g, '').split('\n');
	let inList = false;
	const html: string[] = [];
	for (const line of lines) {
		if (/^[-*] /.test(line)) {
			if (!inList) html.push('<ul>');
			inList = true;
			html.push(`<li>${inlineMarkdown(line.slice(2))}</li>`);
			continue;
		}
		if (inList) { html.push('</ul>'); inList = false; }
		if (!line.trim()) continue;
		const heading = line.match(/^(#{1,3})\s+(.+)$/);
		if (heading) html.push(`<h${heading[1].length}>${inlineMarkdown(heading[2])}</h${heading[1].length}>`);
		else if (line.startsWith('> ')) html.push(`<blockquote>${inlineMarkdown(line.slice(2))}</blockquote>`);
		else html.push(`<p>${inlineMarkdown(line)}</p>`);
	}
	if (inList) html.push('</ul>');
	return html.join('');
}

export const formatDate = (value: string) => new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', year: 'numeric' }).format(new Date(value));
