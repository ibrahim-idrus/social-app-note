package store

import (
	"context"
	"database/sql"
	"fmt"
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
	ID                int64   `json:"id"`
	UserID            int64   `json:"-"`
	SocialIdentityID  *int64  `json:"social_identity_id"`
	Title             string  `json:"title"`
	ContentMarkdown   string  `json:"content_markdown"`
	Source            string  `json:"source"`
	ExternalMessageID *string `json:"external_message_id"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
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
	result, err := db.ExecContext(ctx, `INSERT INTO notes (user_id, title, content_markdown, source) VALUES (?, ?, ?, 'manual')`, userID, title, content)
	if err != nil {
		return Note{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Note{}, err
	}
	return NoteByID(ctx, db, userID, id)
}

func NoteByID(ctx context.Context, db *sql.DB, userID, id int64) (Note, error) {
	var note Note
	err := db.QueryRowContext(ctx, `
		SELECT id, user_id, social_identity_id, title, content_markdown, source, external_message_id, created_at, updated_at
		FROM notes WHERE id = ? AND user_id = ?`, id, userID).Scan(
		&note.ID, &note.UserID, &note.SocialIdentityID, &note.Title, &note.ContentMarkdown, &note.Source, &note.ExternalMessageID, &note.CreatedAt, &note.UpdatedAt,
	)
	return note, err
}

func UpdateNote(ctx context.Context, db *sql.DB, userID, id int64, title, content string) (Note, error) {
	result, err := db.ExecContext(ctx, `UPDATE notes SET title = ?, content_markdown = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ? AND user_id = ?`, title, content, id, userID)
	if err != nil {
		return Note{}, err
	}
	if count, err := result.RowsAffected(); err != nil || count == 0 {
		if err != nil {
			return Note{}, err
		}
		return Note{}, sql.ErrNoRows
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

func ListNotes(ctx context.Context, db *sql.DB, userID int64, query, source, sortField, order string, page, pageSize int) (NoteList, error) {
	where := `user_id = ?`
	args := []any{userID}
	if query != "" {
		where += ` AND (title LIKE ? ESCAPE '\' OR content_markdown LIKE ? ESCAPE '\')`
		term := "%" + escapeLike(query) + "%"
		args = append(args, term, term)
	}
	if source != "" {
		where += ` AND source = ?`
		args = append(args, source)
	}
	var total int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notes WHERE `+where, args...).Scan(&total); err != nil {
		return NoteList{}, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, user_id, social_identity_id, title, content_markdown, source, external_message_id, created_at, updated_at
		FROM notes WHERE %s ORDER BY %s %s, id %s LIMIT ? OFFSET ?`, where, sortField, order, order), args...)
	if err != nil {
		return NoteList{}, err
	}
	defer rows.Close()
	result := NoteList{Notes: []Note{}, Total: total}
	for rows.Next() {
		var note Note
		if err := rows.Scan(&note.ID, &note.UserID, &note.SocialIdentityID, &note.Title, &note.ContentMarkdown, &note.Source, &note.ExternalMessageID, &note.CreatedAt, &note.UpdatedAt); err != nil {
			return NoteList{}, err
		}
		result.Notes = append(result.Notes, note)
	}
	return result, rows.Err()
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
