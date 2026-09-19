# Confirm Social Account Before Connect — Implementation Plan

> **For Hermes:** Implement this plan task-by-task, verifying each item before starting the next.

**Goal:** Let a user search an Instagram username, inspect the exact account returned by Instagram, explicitly confirm it, and only then create the pending social identity.

**Architecture:** Keep the existing Instagram Business Discovery lookup and verification flow. Tighten the Settings UI into a search → review → confirm flow, and send the selected provider account ID with creation so the backend can reject stale or mismatched confirmations before saving. No new dependency or database table is needed.

**Tech stack:** Svelte 5, TypeScript, Go `net/http`, SQLite, Node test runner, Go tests.

---

## Problems captured from the request

- Entering a username currently does not clearly show a confirmation step.
- The user cannot confidently tell whether the returned account is theirs.
- The current **Add exact username** action can save directly from typed text without selecting or confirming a search result.
- If the result is wrong, there is no explicit **Not my account / search again** path.

## Current evidence from code inspection

- `src/routes/settings/+page.svelte:40-44` already calls `GET /api/social-identities/search`.
- `src/routes/settings/+page.svelte:137-148` renders results, but clicking one only copies its username into the field; the form can still submit unconfirmed text.
- `src/lib/api.ts:14,102-103` already models search results but creates an identity with only `platform` and `username`.
- `backend/internal/httpapi/phase2.go:29-64` returns Instagram `id`, `username`, `name`, and `profile_picture_url` from Business Discovery.
- `backend/internal/httpapi/phase2.go:75-107` creates a pending identity without proving that the submitted username matches the reviewed provider result.
- `src/routes/layout.css:76` already has an `.account-result` visual primitive, so the implementation should reuse it rather than introduce a new component system.

## Confirmed product flow

1. User enters an Instagram username and selects **Search**.
2. While searching, the form announces a loading state and prevents duplicate requests.
3. If found, show one review card with profile photo (or fallback), display name, exact `@username`, and an external Instagram profile link.
4. Ask **“Is this your account?”**
5. **Yes, connect this account** submits the reviewed provider ID and username.
6. **No, search again** clears the reviewed result, returns focus to the input, and does not create anything.
7. Empty, unavailable, and failed lookups show distinct guidance. No identity is created until explicit confirmation.
8. Existing DM-code ownership verification remains required after confirmation; profile preview confirms selection, while the DM proves control.

## Implementation tasks

### Task 1: Lock the backend creation contract to the reviewed account

**Objective:** Prevent the client from creating a pending identity from arbitrary typed text after a different account was reviewed.

**Files:**
- Modify: `backend/internal/httpapi/phase2.go`
- Test: `backend/internal/httpapi/phase2_test.go`

**Steps:**
1. Add failing tests for missing `platform_user_id`, a provider ID/username mismatch, and a valid reviewed match.
2. Extend the create payload to accept `platform_user_id`.
3. Extract/reuse the smallest Business Discovery lookup helper so search and create resolve the same provider data.
4. During create, look up the normalized username again and require both provider ID and normalized username to equal the reviewed selection. Return a specific `social_account_mismatch` conflict when they do not.
5. Continue storing a pending identity and issuing the one-time DM verification code only after the check passes.
6. Run `cd backend && go test ./internal/httpapi`, expected: PASS.

**Deliberate limit:** This is not OAuth. The existing DM-code verification remains the ownership proof.

### Task 2: Update the typed API contract

**Objective:** Make the selected Instagram account identity explicit in the frontend API.

**Files:**
- Modify: `src/lib/api.ts`
- Test: `tests/app.test.ts`

**Steps:**
1. Add `profile_url` locally from the exact username (or construct it in the view); do not add backend persistence for it.
2. Change `createSocialIdentity(platform, username)` to accept/send `platform_user_id` as well.
3. Add a user-facing message for `social_account_mismatch`: the account changed or no longer matches; search again.
4. Test the exact POST JSON body.
5. Run `npm test`, expected: PASS.

### Task 3: Replace direct add with search → review → confirm

**Objective:** Make it impossible to connect an account without reviewing the returned profile.

**Files:**
- Modify: `src/routes/settings/+page.svelte`
- Modify only if needed: `src/routes/layout.css`
- Test: `tests/app.test.ts`

**Steps:**
1. Add explicit states for `searching`, `selectedMatch`, and `connecting`.
2. Make the username form submit search only; remove **Add exact username**.
3. Clear stale matches whenever username/platform changes or the add flow is cancelled.
4. Render the selected `.account-result` card with avatar fallback, display name, exact username, and safe external link (`target="_blank"`, `rel="noreferrer"`).
5. Render the question **Is this your account?** and actions **Yes, connect this account** and **No, search again**.
6. On Yes, POST the selected platform ID, exact returned username, and returned provider ID. On success, continue into the existing DM-code instructions.
7. On No, clear selection, preserve or clear the input consistently, and focus the username field for correction.
8. Add accessible `role="status"`/`role="alert"`, disabled/loading buttons, meaningful image alt text or decorative fallback, and keyboard-operable controls.
9. Add empty-result copy: **No matching professional Instagram account found. Check the username and try again.**
10. Run `npm test && npm run check && npm run build`, expected: all PASS.

### Task 4: Verify backend edge cases and regression safety

**Objective:** Prove wrong or stale accounts cannot be saved and existing verification remains intact.

**Files:**
- Test: `backend/internal/httpapi/phase2_test.go`
- Test: `backend/internal/httpapi/instagram_test.go`

**Checks:**
- Invalid username → 400.
- Missing search configuration → clear 503 and no saved row.
- Empty provider result → no confirmation and no saved row.
- Submitted provider ID differs from fresh lookup → 409, no saved row/code.
- Valid confirmed result → pending identity created once.
- Duplicate account rules, CSRF, one-time verification code, expiration, and DM ownership verification still pass.

Run: `cd backend && go test ./...`, expected: PASS.

### Task 5: Authenticated public E2E and delivery

**Objective:** Verify the real user journey at the public application URL before marking ready.

**Steps:**
1. Build and deploy using the project’s existing deployment path; do not infer readiness from local tests alone.
2. Sign in through the public UI.
3. Open Settings → Social capture → Add Instagram.
4. Search a known test professional account and confirm photo/name/username/profile link are visible.
5. Choose **No, search again** and verify no identity appears after reload.
6. Search again, choose **Yes, connect this account**, and verify the pending identity and DM-code instructions appear.
7. Test a wrong/nonexistent username and provider mismatch; verify no account is saved.
8. Verify mobile width (390px), desktop width (1440px), keyboard navigation, no horizontal overflow, and no console/network errors.
9. Commit and push only after authenticated public E2E passes.

## Acceptance criteria

- Typed username alone can never create a social identity.
- The exact provider-returned photo/fallback, display name, username, and profile link are visible before confirmation.
- The user must explicitly choose Yes or No.
- No leaves no saved identity and supports another search.
- Yes sends the reviewed provider ID; backend revalidates it against Instagram before saving.
- A changed/mismatched result is rejected without issuing or storing a verification attempt.
- Existing DM verification remains the final ownership proof.
- Tests, type checks, build, and authenticated public E2E all pass.

## Risks and trade-offs

- Instagram Business Discovery may only resolve eligible professional accounts; explain this in empty/error copy.
- Re-querying on confirmation adds one provider request but closes the stale/tampered client gap.
- Profile photos can expire or fail; use a local visual fallback and never block confirmation solely on image loading.
- OAuth would provide stronger native authorization but is postponed because the current DM-code flow already proves ownership.

## Files likely to change

- `backend/internal/httpapi/phase2.go`
- `backend/internal/httpapi/phase2_test.go`
- `src/lib/api.ts`
- `src/routes/settings/+page.svelte`
- `src/routes/layout.css` (only if existing styles are insufficient)
- `tests/app.test.ts`

## Implementation gate

This document is planning only. Do not implement, deploy, commit, or push until the user explicitly approves implementation. Plan approval by itself is not deployment permission.
