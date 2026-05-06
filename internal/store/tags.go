package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// TagCount is a tag name paired with the number of commands wearing it.
type TagCount struct {
	Name  string
	Count int64
}

// AllTags returns every tag with its associated command count, sorted by
// count DESC, name ASC.
func (s *Store) AllTags() ([]TagCount, error) {
	rows, err := s.db.Query(
		`SELECT t.name, COUNT(ct.command_id)
		   FROM tags t
		   LEFT JOIN command_tags ct ON ct.tag_id = t.id
		  GROUP BY t.id
		  ORDER BY COUNT(ct.command_id) DESC, t.name ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TagCount
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Name, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

// SetTags replaces the entire tag set for a command.
func (s *Store) SetTags(commandID int64, tags []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setTagsTx(tx, commandID, tags); err != nil {
		return err
	}
	return tx.Commit()
}

// AdjustTags applies +tag/-tag style modifications without replacing the
// entire set. add and remove are simple slices of tag names; either may be
// empty.
func (s *Store) AdjustTags(commandID int64, add, remove []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, name := range add {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		tagID, err := upsertTagTx(tx, name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO command_tags(command_id, tag_id) VALUES (?, ?)`,
			commandID, tagID,
		); err != nil {
			return err
		}
	}

	for _, name := range remove {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, err := tx.Exec(
			`DELETE FROM command_tags
			  WHERE command_id = ?
			    AND tag_id = (SELECT id FROM tags WHERE name = ?)`,
			commandID, name,
		); err != nil {
			return err
		}
	}

	// Garbage-collect tags that have no remaining commands.
	if _, err := tx.Exec(
		`DELETE FROM tags
		  WHERE id NOT IN (SELECT DISTINCT tag_id FROM command_tags)`,
	); err != nil {
		return err
	}

	return tx.Commit()
}

// tagsFor returns the names of tags attached to a command, sorted alphabetically.
func (s *Store) tagsFor(commandID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT t.name
		   FROM tags t
		   JOIN command_tags ct ON ct.tag_id = t.id
		  WHERE ct.command_id = ?
		  ORDER BY t.name ASC`,
		commandID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// setTagsTx replaces the tag set inside an open transaction.
func setTagsTx(tx *sql.Tx, commandID int64, tags []string) error {
	if _, err := tx.Exec(
		`DELETE FROM command_tags WHERE command_id = ?`, commandID,
	); err != nil {
		return fmt.Errorf("clear tags: %w", err)
	}

	seen := make(map[string]bool)
	for _, raw := range tags {
		name := strings.TrimSpace(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		tagID, err := upsertTagTx(tx, name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO command_tags(command_id, tag_id) VALUES (?, ?)`,
			commandID, tagID,
		); err != nil {
			return fmt.Errorf("link tag %q: %w", name, err)
		}
	}

	// Clean up orphaned tags.
	if _, err := tx.Exec(
		`DELETE FROM tags
		  WHERE id NOT IN (SELECT DISTINCT tag_id FROM command_tags)`,
	); err != nil {
		return fmt.Errorf("gc tags: %w", err)
	}
	return nil
}

// upsertTagTx returns the id of a tag, creating it if it doesn't exist.
func upsertTagTx(tx *sql.Tx, name string) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM tags WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("lookup tag: %w", err)
	}
	res, err := tx.Exec(`INSERT INTO tags(name) VALUES (?)`, name)
	if err != nil {
		return 0, fmt.Errorf("insert tag: %w", err)
	}
	return res.LastInsertId()
}

// ParseTags splits a comma-separated tag string into a deduplicated, trimmed
// slice. Empty entries are dropped. Tag names may not contain whitespace.
func ParseTags(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]bool)
	var out []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}
