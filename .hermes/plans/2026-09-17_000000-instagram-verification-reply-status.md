# Instagram Verification Reply and Status Implementation Plan

> **For Hermes:** Implement task-by-task with Codex on `main`, verifying each ordered item before starting the next.

**Goal:** Extend the existing Instagram verification flow with one duplicate-safe success reply and backend-driven waiting, success, invalid, expired, and recoverable-failure feedback.

**Architecture:** Keep `ProcessInstagramDM` as the source of truth for activation and note suppression. Add minimal persisted verification-result and outbound-attempt metadata, expose sanitized fields through the existing identity-list API, poll that API only while pending, and send through Meta's Instagram Login messaging endpoint after activation commits.

**Tech Stack:** Go `net/http` + SQLite, SvelteKit/Svelte 5, existing Node and Go tests.

---

## Confirmed decisions

- Work directly on `main` after explicit implementation approval.
- Authentication topology is **Instagram Login with an Instagram User access token**.
- Do not use Facebook Login, a Page access token, or the Page-linked Messenger endpoint.
- Manual Instagram username entry remains unchanged.
- Do not add username search, discovery, autocomplete, dropdowns, Business Discovery, profile lookup, arbitrary account lookup, or `/api/social-identities/search` as part of this task.
- The verification message activates the identity and never becomes a note.

## Current evidence

- `src/routes/settings/+page.svelte` loads identities once on mount and currently renders only the raw `pending` status; it has no waiting action or polling.
- `src/lib/api.ts` already provides identity list/create/regenerate calls.
- `backend/internal/httpapi/phase2.go` creates 10-minute codes and sends simulated inbound DMs into the store.
- `backend/internal/store/phase2.go:177-239` transactionally suppresses a consumed verification code, routes later DMs by stable sender ID, activates an unexpired pending identity, and does not create a note for a successful verification message.
- Existing tests already cover replacement, expiration, single use, note suppression, stable-ID routing, isolation, and note deduplication.

## Official Meta contract — Instagram Login

Verified against Meta documentation on 2026-09-17:

- Messaging overview: https://developers.facebook.com/docs/instagram-platform/instagram-api-with-instagram-login/messaging-api
- Send API host and operation: `POST https://graph.instagram.com/v26.0/{IG_ID}/messages`
- Authentication: `Authorization: Bearer <Instagram User access token>`
- Text payload: `{"recipient":{"id":"<IGSID>"},"message":{"text":"<TEXT>"}}`
- Required permissions documented for this topology: `instagram_business_basic` and `instagram_business_manage_messages`.
- The recipient must have initiated messaging with the professional Instagram account; this verification DM satisfies that prerequisite.

The implementation must keep the Graph version configurable or reuse the project's selected version rather than scattering it. It must never log or return the access token or raw Meta response.

## Ordered implementation plan

### 1. Add failing backend tests for outcomes and reply deduplication

**Modify:** `backend/internal/httpapi/instagram_test.go`

Add mocked-HTTP tests proving:

1. Successful activation produces exactly one request with the Instagram Login host, bearer token, recipient sender ID, and product-aware success text.
2. Duplicate delivery of the same external event ID does not send twice.
3. Invalid, expired, unmatched, malformed, already-consumed, and system-failure paths do not send a success reply.
4. The verification message still creates zero notes.
5. Meta response details and credentials are not exposed.

Run the targeted tests and confirm they fail for missing behavior before implementation.

### 2. Persist only the state required for truthful UI and one send attempt

**Create:** `backend/internal/store/migrations/0004_instagram_verification_feedback.sql`

Add the smallest columns/table needed to represent:

- recoverable verification result (`waiting`, `invalid_code`, `expired`, `system_failure`; `active` remains the existing identity status),
- result timestamp,
- unique inbound external message ID used to claim the automatic reply,
- reply attempt timestamp/result without storing tokens or raw Meta payloads.

Prefer a unique database constraint for the inbound event claim. Do not add new permanent identity lifecycle states when the existing `pending`/`active` model suffices.

**Modify/Test:** `backend/internal/store/store_test.go` and migration assertions.

### 3. Return a structured activation outcome from the existing store path

**Modify:** `backend/internal/store/phase2.go`

Refactor `ProcessInstagramDM` minimally so the caller can distinguish:

- activation succeeded and owns the reply claim,
- duplicate/no-op,
- invalid code,
- expired code,
- unmatched sender/code,
- system failure.

Keep activation and reply-claim persistence atomic. Commit activation before any network request. Preserve stable sender routing, cross-user isolation, single-use behavior, and note suppression.

For invalid/expired input, update only a safely attributable pending identity. Do not assign arbitrary invalid text globally or leak whether another user's code exists.

### 4. Add the minimal Instagram Login text sender

**Modify:** `backend/internal/httpapi/phase1.go`, `backend/internal/httpapi/health.go`, `backend/internal/httpapi/phase2.go`

Use the existing injected `http.Client`. Add one focused helper that:

- posts to `https://graph.instagram.com/v26.0/{dedicated_IG_ID}/messages`,
- supplies the Instagram User token as a bearer token,
- JSON-encodes `recipient.id` and `message.text`,
- uses the configured product name if one exists, otherwise the established `NoteDesk` name,
- mentions `@<username>` but never the user's email,
- bounds request time and response-body reads,
- converts all failures to sanitized internal outcomes.

Call it only when the committed store outcome says this event activated the identity and owns the one reply attempt. Because Meta documents no request idempotency key, do not automatically retry an ambiguous network/timeout result; record a recoverable `system_failure` instead.

### 5. Expose sanitized backend-driven feedback

**Modify:** `backend/internal/store/phase2.go`, `src/lib/api.ts`

Extend the existing identity response with only UI-safe fields such as `verification_state` and `verification_updated_at`. Derive `expired` from the authoritative expiry when appropriate. Never expose hashes, consumed timestamps, event IDs, Meta errors, tokens, database IDs beyond the already-public identity ID, or webhook payloads.

Code regeneration resets recoverable feedback to the normal pending state and makes the old code remain invalid.

### 6. Add the smallest waiting and polling UI

**Modify:** `src/routes/settings/+page.svelte`, and only if needed `src/routes/layout.css`

- Add an “I've sent the code” action that changes display state to waiting but never sets `active` locally.
- While pending/waiting, poll the existing identity-list API at a modest interval and stop on active, removal, navigation, or terminal feedback.
- Render actual backend state:
  - waiting: “Waiting for Instagram verification…”
  - active: “✓ Instagram connected” and the connected username
  - invalid: recoverable guidance without permanent failure
  - expired: “Your verification code has expired” plus existing regeneration action
  - system failure: generic recoverable message and retry/regeneration path
- Preserve the existing visual system and manual username input. No WebSockets and no search-related work.

### 7. Add frontend state tests

**Modify:** `tests/app.test.ts` (or the smallest existing Svelte test target)

Verify source/API behavior for waiting, backend-confirmed active, invalid, expired, and recoverable failure. Prove the sent button does not mark the identity active and polling is bounded/cleaned up.

### 8. Ordered verification and delivery

Run each before proceeding to the next command; stop and fix failures:

```sh
cd backend && go test ./internal/store ./internal/httpapi
cd .. && npm test
cd backend && go test ./...
cd .. && npm run check
npm run build
git diff --check
git status --short --branch
```

Then inspect the exact diff, confirm no search/discovery changes were introduced, perform authenticated public E2E for waiting → webhook/simulation → active and fallback states, commit on `main`, push, and report public link, commit, and push result.

## Delivery semantics and risk

Meta's Send API does not provide a documented idempotency key. The defensible guarantee is **one database-claimed automatic send attempt per unique inbound event**. Retrying an ambiguous timeout could duplicate a user-visible message, so ambiguous outcomes become recoverable system failures rather than automatic retries. A confirmed HTTP failure may be exposed only as sanitized UI state.

A production real webhook is not invented in this task. The reply hooks into the repository's existing inbound architecture; if only the simulated endpoint is currently enabled, real Meta webhook enablement/signature validation remains a separate deployment prerequisite.

## Acceptance criteria

- Exactly one success-reply attempt follows a valid, unexpired, unused, sender-matched activation.
- No success reply follows invalid, expired, unmatched, malformed, duplicate, consumed, or failed activation.
- Verification DM creates no note.
- UI states come from backend identity data; clicking sent never activates locally.
- Old regenerated codes remain invalid.
- No secrets or raw provider errors reach the UI.
- No deferred search/discovery feature is added or changed.

## Implementation gate

**No implementation is authorized by this plan.** Begin edits only after Baimeme explicitly approves implementation. Approval of the Instagram Login topology or this review artifact alone is not implementation permission.
