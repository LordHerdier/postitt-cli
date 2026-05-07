package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Command is a single saved command with its metadata.
type Command struct {
	ID            int64
	Command       string
	Description   string
	Bookmarked    bool
	UseCount      int64
	LastUsed      *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	AutoFillable  bool
	Tags          []string
}

// ErrNotFound is returned when a command does not exist.
var ErrNotFound = errors.New("command not found")

// ErrDuplicate is returned when a command with the same text already exists.
var ErrDuplicate = errors.New("command already exists")

// Add inserts a new command with the given tags. If a command with the same
// text already exists, ErrDuplicate is returned and no changes are made.
func (s *Store) Add(cmd, desc string, tags []string, autoFillable bool) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	res, err := tx.Exec(
		`INSERT INTO commands(command, description, created_at, updated_at, auto_fillable)
		 VALUES (?, ?, ?, ?, ?)`,
		cmd, desc, now, now, boolToInt(autoFillable),
	)
	if err != nil {
		// modernc returns a SQLITE_CONSTRAINT error string on UNIQUE violation.
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, ErrDuplicate
		}
		return 0, fmt.Errorf("insert command: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	if err := setTagsTx(tx, id, tags); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// Get returns the command with the given ID, including its tags.
func (s *Store) Get(id int64) (*Command, error) {
	row := s.db.QueryRow(
		`SELECT id, command, description, bookmarked, use_count, last_used,
		        created_at, updated_at, auto_fillable
		   FROM commands WHERE id = ?`,
		id,
	)
	c, err := scanCommand(row)
	if err != nil {
		return nil, err
	}
	tags, err := s.tagsFor(id)
	if err != nil {
		return nil, err
	}
	c.Tags = tags
	return c, nil
}

// GetByText returns the command with the given text. Useful for the "save"
// flow where we want to bump use_count on a duplicate.
func (s *Store) GetByText(cmd string) (*Command, error) {
	row := s.db.QueryRow(
		`SELECT id, command, description, bookmarked, use_count, last_used,
		        created_at, updated_at, auto_fillable
		   FROM commands WHERE command = ?`,
		cmd,
	)
	c, err := scanCommand(row)
	if err != nil {
		return nil, err
	}
	tags, err := s.tagsFor(c.ID)
	if err != nil {
		return nil, err
	}
	c.Tags = tags
	return c, nil
}

// List returns all commands, sorted by bookmarked DESC, use_count DESC,
// last_used DESC, id DESC. tagFilter, when non-empty, restricts to commands
// having ALL of the named tags.
func (s *Store) List(tagFilter []string) ([]*Command, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if len(tagFilter) == 0 {
		rows, err = s.db.Query(
			`SELECT id, command, description, bookmarked, use_count, last_used,
			        created_at, updated_at, auto_fillable
			   FROM commands
			   ORDER BY bookmarked DESC, use_count DESC, last_used DESC, id DESC`,
		)
	} else {
		// Each tag adds an INTERSECT clause; the command must appear in every
		// per-tag result set.
		var (
			subs []string
			args []any
		)
		for _, t := range tagFilter {
			subs = append(subs, `SELECT command_id FROM command_tags
				JOIN tags ON tags.id = command_tags.tag_id
				WHERE tags.name = ?`)
			args = append(args, t)
		}
		query := fmt.Sprintf(
			`SELECT id, command, description, bookmarked, use_count, last_used,
			        created_at, updated_at, auto_fillable
			   FROM commands
			  WHERE id IN (%s)
			  ORDER BY bookmarked DESC, use_count DESC, last_used DESC, id DESC`,
			strings.Join(subs, " INTERSECT "),
		)
		rows, err = s.db.Query(query, args...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Command
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Hydrate tags in a second pass. Could be a single JOIN, but this keeps
	// the row-scan logic simple and the perf is fine for our scale.
	for _, c := range out {
		tags, err := s.tagsFor(c.ID)
		if err != nil {
			return nil, err
		}
		c.Tags = tags
	}
	return out, nil
}

// Update replaces the command text, description, and auto_fillable flag for
// the given ID. Tags are not modified here; use SetTags / AdjustTags.
func (s *Store) Update(id int64, cmd, desc string, autoFillable bool) error {
	res, err := s.db.Exec(
		`UPDATE commands
		    SET command = ?, description = ?, auto_fillable = ?,
		        updated_at = ?
		  WHERE id = ?`,
		cmd, desc, boolToInt(autoFillable), time.Now().Unix(), id,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrDuplicate
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes the command (and its tag links via cascade) by ID.
func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM commands WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetBookmark sets the bookmarked flag.
func (s *Store) SetBookmark(id int64, bookmarked bool) error {
	res, err := s.db.Exec(
		`UPDATE commands SET bookmarked = ?, updated_at = ? WHERE id = ?`,
		boolToInt(bookmarked), time.Now().Unix(), id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordUse increments use_count and updates last_used to now.
func (s *Store) RecordUse(id int64) error {
	res, err := s.db.Exec(
		`UPDATE commands
		    SET use_count = use_count + 1, last_used = ?
		  WHERE id = ?`,
		time.Now().Unix(), id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AutoFillable returns commands whose description is blank and which were
// flagged for retry. Useful for a future `postitt sync-descriptions`.
func (s *Store) AutoFillable() ([]*Command, error) {
	rows, err := s.db.Query(
		`SELECT id, command, description, bookmarked, use_count, last_used,
		        created_at, updated_at, auto_fillable
		   FROM commands WHERE auto_fillable = 1 AND description = ''`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Command
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// rowScanner abstracts *sql.Row and *sql.Rows for scanCommand.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCommand(r rowScanner) (*Command, error) {
	var (
		c        Command
		lastUsed sql.NullInt64
		created  int64
		updated  int64
		bm, af   int
	)
	err := r.Scan(
		&c.ID, &c.Command, &c.Description, &bm, &c.UseCount, &lastUsed,
		&created, &updated, &af,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Bookmarked = bm != 0
	c.AutoFillable = af != 0
	c.CreatedAt = time.Unix(created, 0)
	c.UpdatedAt = time.Unix(updated, 0)
	if lastUsed.Valid {
		t := time.Unix(lastUsed.Int64, 0)
		c.LastUsed = &t
	}
	return &c, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
