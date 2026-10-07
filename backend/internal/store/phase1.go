package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type User struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type Session struct {
	User
	CSRFHash []byte
}

type Note struct {
	ID                   int64                 `json:"id"`
	UserID               int64                 `json:"-"`
	SocialIdentityID     *int64                `json:"social_identity_id"`
	Title                string                `json:"title"`
	ContentMarkdown      string                `json:"content_markdown"`
	Source               string                `json:"source"`
	ExternalMessageID    *string               `json:"external_message_id"`
	CreatedAt            string                `json:"created_at"`
	UpdatedAt            string                `json:"updated_at"`
	TitleMatches         []MatchRange          `json:"title_matches,omitempty"`
	Sections             []NoteSearchSection   `json:"sections,omitempty"`
	InstagramAttachments []InstagramAttachment `json:"instagram_attachments"`
	FacebookAttachments  []FacebookAttachment  `json:"facebook_attachments"`
	Tags                 []string              `json:"tags"`
}

var hashtagRE = regexp.MustCompile(`#[\pL\pN_]+`)

func messageNoteFields(text string, urls []string) (string, string) {
	text = strings.TrimSpace(text)
	title := text
	if title == "" {
		title = "Shared post"
	}
	r := []rune(title)
	if len(r) > 200 {
		title = string(r[:200])
	}
	body := text
	for i := 0; i < len(urls) && i < 1; i++ {
		if urls[i] != "" && !strings.Contains(body, urls[i]) {
			if body == "" {
				body = urls[i]
			} else {
				body = urls[i] + "\n" + body
			}
		}
	}
	return title, body
}

func analyzeNoteTags(ctx context.Context, tx *sql.Tx, noteID int64, content string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM note_tags WHERE note_id=?`, noteID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, raw := range hashtagRE.FindAllString(content, -1) {
		display := strings.TrimPrefix(raw, "#")
		normalized := strings.ToLower(display)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO tags(normalized_name,display_name) VALUES(?,?) ON CONFLICT(normalized_name) DO NOTHING`, normalized, display); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO note_tags(note_id,tag_id) SELECT ?,id FROM tags WHERE normalized_name=?`, noteID, normalized); err != nil {
			return err
		}
	}
	return nil
}

func loadNoteTags(ctx context.Context, db *sql.DB, n *Note) error {
	rows, err := db.QueryContext(ctx, `SELECT t.display_name FROM tags t JOIN note_tags nt ON nt.tag_id=t.id WHERE nt.note_id=? ORDER BY t.normalized_name`, n.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	n.Tags = []string{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return err
		}
		n.Tags = append(n.Tags, tag)
	}
	return rows.Err()
}

type InstagramAttachment struct {
	Type             string `json:"type"`
	URL              string `json:"url"`
	InstagramMediaID string `json:"media_id,omitempty"`
	Permalink        string `json:"permalink,omitempty"`
	Alt              string `json:"alt"`
}

type FacebookAttachment struct {
	Type         string `json:"type"`
	URL          string `json:"url"`
	Title        string `json:"title,omitempty"`
	LookupStatus string `json:"lookup_status,omitempty"`
}

func scanNote(s interface{ Scan(...any) error }, n *Note) error {
	var instagramRaw, facebookRaw sql.NullString
	if err := s.Scan(&n.ID, &n.UserID, &n.SocialIdentityID, &n.Title, &n.ContentMarkdown, &n.Source, &n.ExternalMessageID, &n.CreatedAt, &n.UpdatedAt, &instagramRaw, &facebookRaw); err != nil {
		return err
	}
	n.InstagramAttachments = []InstagramAttachment{}
	n.FacebookAttachments = []FacebookAttachment{}
	if instagramRaw.Valid {
		if err := json.Unmarshal([]byte(instagramRaw.String), &n.InstagramAttachments); err != nil {
			return err
		}
	}
	if facebookRaw.Valid {
		return json.Unmarshal([]byte(facebookRaw.String), &n.FacebookAttachments)
	}
	return nil
}

type NoteList struct {
	Notes []Note `json:"notes"`
	Total int    `json:"total"`
}

func CreateUser(ctx context.Context, db *sql.DB, name, email, passwordHash string) (User, error) {
	result, err := db.ExecContext(ctx, `INSERT INTO users (name, email, password_hash) VALUES (?, ?, ?)`, name, email, passwordHash)
	if err != nil {
		return User{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return UserByID(ctx, db, id)
}

func UserByID(ctx context.Context, db *sql.DB, id int64) (User, error) {
	var user User
	err := db.QueryRowContext(ctx, `SELECT id, name, email, password_hash, created_at, updated_at FROM users WHERE id = ?`, id).Scan(
		&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt,
	)
	return user, err
}

func UserByEmail(ctx context.Context, db *sql.DB, email string) (User, error) {
	var user User
	err := db.QueryRowContext(ctx, `SELECT id, name, email, password_hash, created_at, updated_at FROM users WHERE email = ?`, email).Scan(
		&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt,
	)
	return user, err
}

func CreateSession(ctx context.Context, db *sql.DB, userID int64, tokenHash, csrfHash []byte, expiresAt string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO sessions (user_id, token_hash, csrf_hash, expires_at) VALUES (?, ?, ?, ?)`, userID, tokenHash, csrfHash, expiresAt)
	return err
}

func SessionByToken(ctx context.Context, db *sql.DB, tokenHash []byte, now string) (Session, error) {
	var session Session
	err := db.QueryRowContext(ctx, `
		SELECT users.id, users.name, users.email, users.created_at, users.updated_at, sessions.csrf_hash
		FROM sessions JOIN users ON users.id = sessions.user_id
		WHERE sessions.token_hash = ? AND sessions.expires_at > ?`, tokenHash, now).Scan(
		&session.ID, &session.Name, &session.Email, &session.CreatedAt, &session.UpdatedAt, &session.CSRFHash,
	)
	return session, err
}

func DeleteSession(ctx context.Context, db *sql.DB, tokenHash []byte) error {
	_, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func CreateNote(ctx context.Context, db *sql.DB, userID int64, title, content string) (Note, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Note{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO notes (user_id, title, content_markdown, source) VALUES (?, ?, ?, 'manual')`, userID, title, content)
	if err != nil {
		return Note{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Note{}, err
	}
	if err = analyzeNoteTags(ctx, tx, id, content); err != nil {
		return Note{}, err
	}
	if err = tx.Commit(); err != nil {
		return Note{}, err
	}
	return NoteByID(ctx, db, userID, id)
}

func NoteByID(ctx context.Context, db *sql.DB, userID, id int64) (Note, error) {
	var note Note
	row := db.QueryRowContext(ctx, `
		SELECT id, user_id, social_identity_id, title, content_markdown, source, external_message_id, created_at, updated_at, instagram_attachments_json, facebook_attachments_json
		FROM notes WHERE id = ? AND user_id = ?`, id, userID)
	err := scanNote(row, &note)
	if err == nil {
		err = loadNoteTags(ctx, db, &note)
	}
	return note, err
}

func UpdateInstagramAttachments(ctx context.Context, db *sql.DB, userID, id int64, attachments []InstagramAttachment) error {
	raw, err := json.Marshal(attachments)
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, `UPDATE notes SET instagram_attachments_json=? WHERE id=? AND user_id=?`, raw, id, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func UpdateNote(ctx context.Context, db *sql.DB, userID, id int64, title, content string) (Note, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Note{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE notes SET title = ?, content_markdown = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ? AND user_id = ?`, title, content, id, userID)
	if err != nil {
		return Note{}, err
	}
	if count, err := result.RowsAffected(); err != nil || count == 0 {
		if err != nil {
			return Note{}, err
		}
		return Note{}, sql.ErrNoRows
	}
	if err = analyzeNoteTags(ctx, tx, id, content); err != nil {
		return Note{}, err
	}
	if err = tx.Commit(); err != nil {
		return Note{}, err
	}
	return NoteByID(ctx, db, userID, id)
}

func DeleteNote(ctx context.Context, db *sql.DB, userID, id int64) error {
	result, err := db.ExecContext(ctx, `DELETE FROM notes WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count == 0 {
		if err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	return nil
}

func BulkEditNoteTags(ctx context.Context, db *sql.DB, userID int64, noteIDs []int64, add, remove []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range noteIDs {
		var owned int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM notes WHERE id=? AND user_id=?`, id, userID).Scan(&owned); err != nil {
			return err
		}
		if owned == 0 {
			continue
		}
		for _, tag := range remove {
			if _, err := tx.ExecContext(ctx, `DELETE FROM note_tags WHERE note_id=? AND tag_id IN (SELECT id FROM tags WHERE normalized_name=?)`, id, strings.ToLower(tag)); err != nil {
				return err
			}
		}
		for _, tag := range add {
			normalized := strings.ToLower(tag)
			if _, err := tx.ExecContext(ctx, `INSERT INTO tags(normalized_name,display_name) VALUES(?,?) ON CONFLICT(normalized_name) DO NOTHING`, normalized, tag); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO note_tags(note_id,tag_id) SELECT ?,id FROM tags WHERE normalized_name=?`, id, normalized); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func ListNotes(ctx context.Context, db *sql.DB, userID int64, query, searchIn, source, sortField, order string, page, pageSize int) (NoteList, error) {
	var sources []string
	if source != "" {
		sources = []string{source}
	}
	return ListNotesFiltered(ctx, db, userID, query, searchIn, sources, nil, sortField, order, page, pageSize)
}

func ListNotesFiltered(ctx context.Context, db *sql.DB, userID int64, query, searchIn string, sources, tags []string, sortField, order string, page, pageSize int) (NoteList, error) {
	where := `user_id = ?`
	args := []any{userID}
	if query != "" {
		term := "%" + escapeLike(query) + "%"
		switch searchIn {
		case "title":
			where += ` AND title LIKE ? ESCAPE '\'`
			args = append(args, term)
		case "content":
			where += ` AND content_markdown LIKE ? ESCAPE '\'`
			args = append(args, term)
		default:
			where += ` AND (title LIKE ? ESCAPE '\' OR content_markdown LIKE ? ESCAPE '\')`
			args = append(args, term, term)
		}
	}
	if len(sources) > 0 {
		where += ` AND source IN (` + strings.TrimRight(strings.Repeat("?,", len(sources)), ",") + ")"
		for _, v := range sources {
			args = append(args, v)
		}
	}
	if len(tags) > 0 {
		where += ` AND EXISTS(SELECT 1 FROM note_tags nt JOIN tags t ON t.id=nt.tag_id WHERE nt.note_id=notes.id AND t.normalized_name IN (` + strings.TrimRight(strings.Repeat("?,", len(tags)), ",") + `))`
		for _, v := range tags {
			args = append(args, strings.ToLower(v))
		}
	}
	var total int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notes WHERE `+where, args...).Scan(&total); err != nil {
		return NoteList{}, err
	}
	orderBy := fmt.Sprintf("%s %s, id %s", sortField, order, order)
	if query != "" && sortField == "relevance" {
		orderBy = `CASE WHEN lower(title) = lower(?) THEN 1 WHEN title LIKE ? ESCAPE '\' THEN 2 WHEN title LIKE ? ESCAPE '\' THEN 3 ELSE 4 END, updated_at DESC, id DESC`
		args = append(args, query, escapeLike(query)+"%", "%"+escapeLike(query)+"%")
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, user_id, social_identity_id, title, content_markdown, source, external_message_id, created_at, updated_at, instagram_attachments_json, facebook_attachments_json
		FROM notes WHERE %s ORDER BY %s LIMIT ? OFFSET ?`, where, orderBy), args...)
	if err != nil {
		return NoteList{}, err
	}
	result := NoteList{Notes: []Note{}, Total: total}
	for rows.Next() {
		var note Note
		if err := scanNote(rows, &note); err != nil {
			return NoteList{}, err
		}

		if query != "" {
			if searchIn != "content" {
				note.TitleMatches = matchRanges(note.Title, query)
			}
			if searchIn != "title" {
				note.Sections = searchSections(note.ContentMarkdown, query)
			}
		}
		result.Notes = append(result.Notes, note)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return NoteList{}, err
	}
	if err := rows.Close(); err != nil {
		return NoteList{}, err
	}
	for i := range result.Notes {
		if err := loadNoteTags(ctx, db, &result.Notes[i]); err != nil {
			return NoteList{}, err
		}
	}
	return result, nil
}

func ListTags(ctx context.Context, db *sql.DB, userID int64) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT t.display_name,t.normalized_name FROM tags t JOIN note_tags nt ON nt.tag_id=t.id JOIN notes n ON n.id=nt.note_id WHERE n.user_id=? ORDER BY t.normalized_name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var display, normalized string
		if err := rows.Scan(&display, &normalized); err != nil {
			return nil, err
		}
		out = append(out, display)
	}
	return out, rows.Err()
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
