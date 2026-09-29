# User Note Search Implementation Plan

> **For Hermes:** Implement this plan in order and verify each phase before starting the next.

**Goal:** Let an authenticated user search only their own notes by title or Markdown content, with highlighted title matches and heading-bounded content excerpts that identify the matching section.

**Architecture:** Extend the existing authenticated `GET /api/notes` endpoint rather than adding a second search service. Keep SQLite `LIKE` for the current product size, parse matching Markdown sections in Go, return plain text plus match ranges, and let Svelte render safe `<mark>` elements without accepting provider-generated HTML.

**Tech Stack:** Go, SQLite, SvelteKit/Svelte 5, existing API client and Notes page. No new dependency or schema migration for MVP.

---

## Confirmed product behavior

- Search is available only after Social Notes authentication.
- Search never returns another user's notes.
- Search scopes are `all`, `title`, and `content`.
- Title matches display the note title and highlight every case-insensitive match.
- Content matches display the relevant Markdown section: heading path, bounded excerpt, and highlighted matching text.
- A Markdown section starts at a heading and ends before the next heading of the same or higher level.
- Text before the first heading belongs to a synthetic `Introduction` section.
- One note can return multiple matching sections, capped at three sections per note for MVP.
- Results are grouped and paginated by note, not by section.
- Clicking a title opens the note; clicking a section opens the note with a stable heading fragment when available.
- No result shows a clear empty state and does not display unrelated notes.
- Search query and scope are stored in the URL so refresh, Back, and Forward preserve them.
- Matching means plain-text, case-insensitive substring matching. Markdown syntax is not added to stored content.

## Current implementation evidence

- `backend/internal/store/phase1.go:140-172` already restricts list/search to `user_id` and searches `title` or `content_markdown` with escaped `LIKE`.
- `backend/internal/httpapi/phase1.go:247-274` already validates `q`, source, sorting, and pagination, but has no search scope.
- `src/lib/api.ts:93-98` already sends `q` through `GET /api/notes`.
- `src/routes/notes/+page.svelte` already has one search field but waits for `change` rather than offering deliberate submit/debounce behavior, shows full-note previews, strips Markdown with a broad regex, and does not expose matching sections or highlights.
- `backend/internal/httpapi/phase1_test.go:275-299` covers basic search/filter/sort/pagination but not scope, section extraction, ranking, match ranges, or isolation of search results.

## MVP boundaries

Use the existing notes table and SQLite `LIKE`. Do not add FTS5, Elasticsearch, search indexes, fuzzy matching, stemming, typo tolerance, saved searches, search history, global keyboard launchers, or a Markdown parser dependency.

Upgrade to SQLite FTS5 only after measured note volume or query latency shows `LIKE` is insufficient. The API result shape below allows the storage implementation to change later without changing the UI contract.

## API contract

Extend:

```http
GET /api/notes?q=camera&search_in=all&source=&sort=relevance&order=desc&page=1&page_size=20
```

Rules:

- `search_in`: `all` (default), `title`, or `content`.
- `sort=relevance` is valid only when `q` is non-empty; default to relevance during search and preserve `updated_at desc` when browsing.
- Trim leading/trailing query whitespace. Reject empty-after-trim scoped searches and queries over 200 characters.
- Existing no-query list responses remain compatible.

Search response:

```json
{
  "notes": [
    {
      "id": 42,
      "title": "Camera launch checklist",
      "title_matches": [{"start": 0, "end": 6}],
      "sections": [
        {
          "heading": "Requirements",
          "heading_path": ["Launch", "Requirements"],
          "anchor": "requirements",
          "excerpt": "The camera kit must include…",
          "matches": [{"start": 4, "end": 10}]
        }
      ],
      "source": "manual",
      "updated_at": "…"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

All offsets are UTF-8-safe character/rune offsets into the exact returned plain-text field, with half-open ranges `[start,end)`. The API returns no HTML.

## Matching and ranking rules

Rank notes by the first applicable class, then `updated_at DESC`, then `id DESC`:

1. Case-insensitive exact title.
2. Title starts with query.
3. Other title substring.
4. Content-only substring.

Within one note, order matching sections by document order. Return at most three. Each excerpt should be concise (target roughly 180 visible characters), centered around the first match where practical, and never cross its section boundary. Add leading/trailing ellipses only when text was omitted.

## Markdown section rules

Implement the minimum line-oriented parser required by this product:

- Recognize ATX headings `#` through `######` followed by whitespace.
- Ignore heading-looking lines inside fenced code blocks using backtick or tilde fences.
- Maintain a heading stack to produce `heading_path`.
- A section includes its heading body until the next heading at the same or higher level.
- Nested headings form separate searchable sections with hierarchical paths.
- Plain text before the first heading uses `Introduction`.
- Strip only the minimum Markdown markers needed for readable excerpts; preserve the text content of links and code.
- Generate deterministic heading anchors using the same slug rule used by the note detail renderer. If the current detail page has no heading IDs, add one shared frontend slug helper and use it in both detail rendering and result links.

## Ordered implementation plan

### Phase 1 — Lock the search contract with backend tests

**Files:**
- Modify: `backend/internal/httpapi/phase1_test.go`
- Modify: `backend/internal/store/store_test.go`

Add failing tests for:

1. `search_in=title` excludes content-only matches.
2. `search_in=content` excludes title-only matches.
3. `search_in=all` includes both.
4. Invalid scope, overlong query, and whitespace-only scoped query return `400`.
5. `%`, `_`, backslash, mixed case, quotes, and Unicode are treated as literal query text.
6. Search pagination totals notes, not matching sections.
7. A second authenticated user cannot retrieve the first user's match.
8. Ranking is exact title → title prefix → title substring → content only.

Run: `go test ./backend/internal/httpapi ./backend/internal/store`
Expected initially: new assertions fail while all existing tests pass.

### Phase 2 — Implement Markdown section extraction

**Files:**
- Create: `backend/internal/store/search.go`
- Create: `backend/internal/store/search_test.go`

Add small pure functions for:

- scanning Markdown into heading-bounded sections;
- maintaining heading paths;
- ignoring fenced-code pseudo-headings;
- converting section bodies to readable plain text;
- finding all case-insensitive Unicode-aware match ranges;
- creating bounded excerpts and rebasing match offsets.

Test:

- Introduction content;
- nested and sibling headings;
- heading boundaries;
- fenced code;
- repeated query occurrences;
- Unicode offsets;
- long prefix/suffix truncation;
- no match;
- three-section cap.

Run: `go test ./backend/internal/store -run 'Search|Section|Excerpt'`
Expected: PASS.

### Phase 3 — Extend storage search and ranking

**Files:**
- Modify: `backend/internal/store/phase1.go`
- Modify: `backend/internal/store/search.go`
- Modify: `backend/internal/store/store_test.go`

1. Extend `ListNotes` with `searchIn` and search-result metadata.
2. Keep parameterized SQL and existing `escapeLike`; never interpolate query input.
3. Apply the authenticated `user_id` condition before every scope condition.
4. Rank title classes in SQL with a small `CASE` expression; use updated time and ID as stable tie-breakers.
5. Parse content sections only for notes selected on the current page, not the entire table.
6. Return title ranges and matching section results only when `q` is present.
7. Preserve the existing response shape for no-query browsing except for optional empty search metadata.

Run: `go test ./backend/internal/store`
Expected: PASS.

### Phase 4 — Validate and expose the API contract

**Files:**
- Modify: `backend/internal/httpapi/phase1.go`
- Modify: `backend/internal/httpapi/phase1_test.go`

1. Parse and validate `search_in`.
2. Select `relevance` by default when searching; retain current browse defaults.
3. Pass normalized query and scope to storage.
4. Return plain text and ranges, never highlighted HTML.
5. Preserve source filtering and page-size limits.

Run: `go test ./backend/internal/httpapi`
Expected: PASS, including existing CRUD/list tests.

### Phase 5 — Extend frontend types and URL state

**Files:**
- Modify: `src/lib/api.ts`
- Modify: `tests/app.test.ts`
- Modify: `src/routes/notes/+page.svelte`

1. Add `MatchRange`, `NoteSearchSection`, and optional search metadata to `Note`.
2. Add `searchIn` to `api.listNotes` and test its query serialization.
3. Initialize query, scope, source, sort, order, and page from `URLSearchParams`.
4. Update the URL with SvelteKit navigation after committed filter changes.
5. Use a search form with explicit submit; do not issue a network request on every keystroke in MVP.
6. Reset page to 1 when query/scope/source changes.
7. Support Back/Forward by reloading state from the URL.

Run: `npm test`
Expected: PASS.

### Phase 6 — Render safe highlighted results

**Files:**
- Create: `src/lib/components/SearchHighlightedText.svelte`
- Modify: `src/routes/notes/+page.svelte`
- Modify: `src/routes/layout.css`
- Add/modify focused frontend tests under `tests/`

1. Render escaped text segments and wrap only validated ranges in semantic `<mark>`.
2. Never use `{@html}` for titles, excerpts, or highlights.
3. Add the scope selector (`All`, `Title`, `Content`) next to the current field.
4. For title-only matches, show highlighted title and a short normal preview.
5. For content matches, show each returned heading path and highlighted excerpt beneath the note.
6. Cap displayed sections at the API-provided maximum and avoid duplicate snippets.
7. Keep the existing source, date, pagination, loading, error, and empty states.
8. Update no-results copy to include the query and provide Clear search.
9. Ensure `<mark>` contrast works in the current light visual system and keyboard focus remains visible.

Run: `npm test && npm run check && npm run build`
Expected: all pass.

### Phase 7 — Add stable section navigation

**Files:**
- Inspect/modify: `src/routes/notes/[id]/+page.svelte`
- Create or modify: `src/lib/app-utils.ts`
- Modify search result links in `src/routes/notes/+page.svelte`
- Add focused tests

1. Reuse the current Markdown rendering path.
2. Assign deterministic IDs to rendered headings using one shared slug function.
3. Handle duplicate headings with deterministic numeric suffixes.
4. Link section results to `/notes/{id}#{anchor}`.
5. Verify browser focus/scroll lands at the section and does not hide it under navigation.
6. If exact match scrolling within a section would require invasive renderer changes, postpone it; heading navigation satisfies MVP.

### Phase 8 — Regression and authenticated public E2E

Run locally:

```bash
go test ./backend/...
npm test
npm run check
npm run build
```

Then deploy through the project's existing deployment process and verify publicly with authenticated users:

1. Create User A and User B.
2. Under User A, create notes covering exact title, title prefix, title substring, content-only, nested headings, Introduction, repeated matches, Unicode, special SQL wildcard characters, and fenced code.
3. Under User B, create a note containing the same query.
4. Verify title scope, content scope, all scope, ranking, highlights, heading paths, fragments, pagination, URL persistence, Back/Forward, clear search, and no-result state.
5. Confirm User A never sees User B's matching note and vice versa.
6. Verify edited note content is reflected immediately and deleted notes disappear.
7. Check 320, 375, 414, 768, and desktop widths; require no horizontal overflow or console errors.
8. Remove E2E fixtures and verify no residual rows.

**READY definition:** authenticated public E2E passes. Local tests alone are VERIFYING, not READY.

## Acceptance matrix

| Scenario | Expected result |
|---|---|
| Unauthenticated search | `401` |
| Exact/prefix/partial title | Correct ranking and highlighted title |
| Content-only match | Matching section path and bounded highlighted excerpt |
| Multiple sections in one note | Up to three, in document order; one note in total count |
| Text before first heading | `Introduction` section |
| Heading text in fenced code | Not treated as a section heading |
| `%`, `_`, backslash, quotes | Literal safe search |
| Mixed case and Unicode | Case-insensitive match with correct displayed ranges |
| No result | Query-specific empty state and Clear search |
| Another user's matching note | Never returned |
| Refresh / Back / Forward | Query and scope preserved |
| Section result click | Opens owned note at stable heading fragment |
| Malicious Markdown/HTML | Displayed safely; no executable result HTML |
| Existing filters and CRUD | Continue working |

## Risks and trade-offs

- **`LIKE` scalability:** acceptable for current scope; move to SQLite FTS5 only after measurement.
- **Markdown complexity:** MVP supports ATX headings and fenced blocks, not every CommonMark construct.
- **Unicode case folding:** use Go's Unicode-aware comparison for returned ranges; verify SQL candidate selection against representative Unicode data.
- **Offset safety:** return rune offsets and validate/sort/non-overlap ranges before rendering.
- **Anchor consistency:** one shared slug rule must drive detail headings and search links.

## Explicitly postponed

- Fuzzy/semantic search
- FTS5 or external search service
- Search suggestions/history
- Saved searches
- Tags and filters beyond existing source
- Highlighting inside the full note body
- Exact in-section match scrolling
- Setext headings and full CommonMark parsing unless real notes require them

## Implementation gate

This document is a plan only. Do not change application behavior, database schema, deployment, or production data until Baimeme explicitly commands implementation. Plan approval by itself is not implementation permission.
