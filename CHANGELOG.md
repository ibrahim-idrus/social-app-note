# Changelog

## 2026-10-08

- Added revocable, read-only note sharing with existing SocialNotes users by exact email.
- Added a public landing page at `/` and moved all account and application screens under `/app`.
- Added account-scoped recovery of uncaptured Instagram and Facebook messages with durable oldest-first processing and webhook queueing.

## 2026-10-07

- Remove the rewritten HTML base tag that caused blank pages on the root-mounted domain.
- Fix root-domain API requests failing when the document base URL is relative.
- Rename the product and project branding from NoteDesk/Social Notes to SocialNotes.

- Replace source and tag checkbox filters with searchable multi-select dropdowns.
- Add bulk tag editing for selected notes, with independent append and remove operations.

## 2026-10-06

- Break long note titles, body text, dashboard rows, and search excerpts instead of allowing horizontal page overflow.
- Replace the split Markdown textarea and preview with a single Milkdown live editor while preserving Markdown storage.
- Use incoming Instagram and Facebook messages as note titles and store shared URLs before message text.
- Parse hashtags into tags and reanalyze tags whenever notes are created or updated.
- Add title/body search with multi-source and multi-tag filters.
- Require an explicit **Load post** action before resolving social media details.
- Add note multi-selection, confirmed bulk deletion, and combined Markdown download.
- Backfill tags for existing notes, bound repeated filters, and hide post loading for unsupported URLs.
- Move selected-note actions into an accessible dropdown menu.
