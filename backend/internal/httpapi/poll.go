package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"social-notes/backend/internal/store"
)

// StartInstagramPoller ticks every interval until ctx ends, saving new inbox
// DMs as notes. No-op when the inbox owner email or token is unconfigured.
func StartInstagramPoller(ctx context.Context, db *sql.DB, options Options, interval time.Duration, onPoll func(saved int, err error)) {
	if options.InboxOwnerEmail == "" || options.InstagramAccessToken == "" || interval <= 0 {
		return
	}
	if options.InstagramGraphVersion == "" {
		options.InstagramGraphVersion = "v26.0"
	}
	client := options.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	api := &API{db: db, instagramAccountID: options.InstagramAccountID, instagramAccessToken: options.InstagramAccessToken,
		instagramGraphVersion: options.InstagramGraphVersion, httpClient: client, inboxOwnerEmail: options.InboxOwnerEmail}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			saved, err := api.pollInstagramInbox(ctx)
			if onPoll != nil {
				onPoll(saved, err)
			}
		}
	}
}

// pollInstagramInbox fetches recent inbox conversations (no webhook or public
// URL needed) and saves every inbound DM as a note under the inbox owner's
// account. Verified senders flow through ProcessInstagramDM; anything
// unmatched (e.g. strangers) falls back to SaveInstagramInboxNote.
// ponytail: naive full-scan poll, details only cover the last 20 messages per
// conversation; switch to webhooks if realtime or full history matters.
func (api *API) pollInstagramInbox(ctx context.Context) (int, error) {
	if api.instagramAccountID == "" || api.instagramAccessToken == "" || api.inboxOwnerEmail == "" {
		return 0, nil
	}
	owner, err := store.UserByEmail(ctx, api.db, api.inboxOwnerEmail)
	if err != nil {
		return 0, nil
	}
	inbox, err := store.GetInstagramIntegration(ctx, api.db)
	if err != nil {
		return 0, err
	}
	var conversations struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := api.instagramGet(ctx, fmt.Sprintf("/%s/conversations", url.PathEscape(api.instagramAccountID)), map[string]string{"platform": "instagram"}, &conversations); err != nil {
		return 0, err
	}
	saved := 0
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, conversation := range conversations.Data {
		if conversation.ID == "" {
			continue
		}
		var thread struct {
			Messages struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			} `json:"messages"`
		}
		if err := api.instagramGet(ctx, "/"+url.PathEscape(conversation.ID), map[string]string{"fields": "messages.limit(20)"}, &thread); err != nil {
			continue
		}
		for _, item := range thread.Messages.Data {
			if item.ID == "" {
				continue
			}
			var detail struct {
				ID      string `json:"id"`
				Message string `json:"message"`
				From    struct {
					ID       string `json:"id"`
					Username string `json:"username"`
				} `json:"from"`
			}
			if err := api.instagramGet(ctx, "/"+url.PathEscape(item.ID), map[string]string{"fields": "id,created_time,from,to,message"}, &detail); err != nil {
				continue
			}
			if detail.Message == "" || detail.From.ID == "" || detail.From.ID == api.instagramAccountID ||
				(inbox.Username != "" && strings.EqualFold(detail.From.Username, inbox.Username)) {
				continue
			}
			outcome, err := store.ProcessInstagramDM(ctx, api.db, api.instagramAccountID, detail.From.ID, detail.ID, detail.Message, now)
			if err != nil {
				continue
			}
			switch outcome.Kind {
			case store.InstagramDMNoteCreated, store.InstagramDMActivated:
				saved++
			case store.InstagramDMUnmatched:
				label := detail.From.Username
				if label == "" {
					label = detail.From.ID
				}
				if ok, err := store.SaveInstagramInboxNote(ctx, api.db, owner.ID, label, detail.ID, detail.Message); err == nil && ok {
					saved++
				}
			}
		}
	}
	return saved, nil
}

func (api *API) instagramGet(ctx context.Context, path string, query map[string]string, target any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	endpoint := fmt.Sprintf("https://graph.instagram.com/%s%s", url.PathEscape(api.instagramGraphVersion), path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	values := req.URL.Query()
	for key, value := range query {
		values.Set(key, value)
	}
	req.URL.RawQuery = values.Encode()
	req.Header.Set("Authorization", "Bearer "+api.instagramAccessToken)
	res, err := api.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("instagram poll failed: %s", res.Status)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(target)
}
