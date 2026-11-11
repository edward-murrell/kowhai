// Package search keeps the FTS5 index up to date and runs queries on it.
// Index writes take the caller's transaction so the index changes with
// the source row.
package search

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/edward-murrell/kowhai/internal/store"
)

const (
	TypeCard    = "card"
	TypeComment = "comment"
	TypeItem    = "checklist_item"
)

func put(ctx context.Context, db store.DBTX, typ, id, cardID, boardID, title, body string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM search_index WHERE entity_type = ? AND entity_id = ?`, typ, id); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO search_index (title, body, entity_type, entity_id, card_id, board_id) VALUES (?, ?, ?, ?, ?, ?)`,
		title, body, typ, id, cardID, boardID)
	return err
}

// IndexCard indexes a card's title and description.
func IndexCard(ctx context.Context, db store.DBTX, c store.Card) error {
	if c.IsTemplate {
		return Remove(ctx, db, TypeCard, c.ID)
	}
	return put(ctx, db, TypeCard, c.ID, c.ID, c.BoardID, c.Title, c.Description)
}

// IndexComment indexes a comment body.
func IndexComment(ctx context.Context, db store.DBTX, c store.Comment, boardID string) error {
	if c.DeletedAt.Valid {
		return Remove(ctx, db, TypeComment, c.ID)
	}
	return put(ctx, db, TypeComment, c.ID, c.CardID, boardID, "", c.Body)
}

// IndexItem indexes a checklist item.
func IndexItem(ctx context.Context, db store.DBTX, i store.ChecklistItem, boardID string) error {
	return put(ctx, db, TypeItem, i.ID, i.CardID, boardID, "", i.Text)
}

func Remove(ctx context.Context, db store.DBTX, typ, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM search_index WHERE entity_type = ? AND entity_id = ?`, typ, id)
	return err
}

// RemoveCard drops a card and everything on it.
func RemoveCard(ctx context.Context, db store.DBTX, cardID string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM search_index WHERE card_id = ?`, cardID)
	return err
}

// RemoveChecklist drops the items of one checklist.
func RemoveItems(ctx context.Context, db store.DBTX, itemIDs []string) error {
	for _, id := range itemIDs {
		if err := Remove(ctx, db, TypeItem, id); err != nil {
			return err
		}
	}
	return nil
}

// RemoveBoard drops a whole board.
func RemoveBoard(ctx context.Context, db store.DBTX, boardID string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM search_index WHERE board_id = ?`, boardID)
	return err
}

// Query is a parsed search.
type Query struct {
	Terms    []string
	Labels   []string
	Members  []string
	Overdue  bool
	Archived bool
}

// Parse splits user input into terms and filters. Supported filters are
// label:, member:, due:overdue and is:archived. Quote a value to keep
// spaces: label:"to do".
func Parse(s string) Query {
	var q Query
	for _, tok := range tokenize(s) {
		k, v, ok := strings.Cut(tok, ":")
		if ok && v != "" {
			switch strings.ToLower(k) {
			case "label":
				q.Labels = append(q.Labels, strings.ToLower(v))
				continue
			case "member":
				q.Members = append(q.Members, strings.ToLower(strings.TrimPrefix(v, "@")))
				continue
			case "due":
				if strings.EqualFold(v, "overdue") {
					q.Overdue = true
					continue
				}
			case "is":
				if strings.EqualFold(v, "archived") {
					q.Archived = true
					continue
				}
			}
		}
		q.Terms = append(q.Terms, tok)
	}
	return q
}

func tokenize(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case unicode.IsSpace(r) && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// matchExpr builds an FTS5 expression: every term must match, and the
// last term matches as a prefix so results appear while typing.
func (q Query) matchExpr() string {
	var parts []string
	for i, t := range q.Terms {
		// Keep letters and digits only, so user input cannot inject FTS syntax.
		clean := strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
				return r
			}
			return ' '
		}, t)
		for _, w := range strings.Fields(clean) {
			p := `"` + w + `"`
			if i == len(q.Terms)-1 {
				p += "*"
			}
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

// Hit is one search match.
type Hit struct {
	EntityType string
	EntityID   string
	CardID     string
	BoardID    string
	CardTitle  string
	ListID     string
	Snippet    string
	Archived   bool
}

// Run searches within boardIDs. offset and limit page through results.
func Run(ctx context.Context, db store.DBTX, q Query, boardIDs []string, now time.Time, offset, limit int) ([]Hit, error) {
	if len(boardIDs) == 0 {
		return nil, nil
	}
	var where []string
	var args []any
	match := q.matchExpr()
	var sb strings.Builder
	if match != "" {
		sb.WriteString(`SELECT s.entity_type, s.entity_id, c.id, c.board_id, c.title, c.list_id,
			snippet(search_index, -1, '[[', ']]', '…', 16), c.archived_at IS NOT NULL
			FROM search_index s JOIN cards c ON c.id = s.card_id`)
		where = append(where, "search_index MATCH ?")
		args = append(args, match)
	} else {
		sb.WriteString(`SELECT 'card', c.id, c.id, c.board_id, c.title, c.list_id, c.title, c.archived_at IS NOT NULL FROM cards c`)
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(boardIDs)), ",")
	where = append(where, "c.board_id IN ("+ph+")", "c.is_template = FALSE")
	for _, id := range boardIDs {
		args = append(args, id)
	}
	if q.Archived {
		where = append(where, "c.archived_at IS NOT NULL")
	} else {
		where = append(where, "c.archived_at IS NULL")
	}
	for _, l := range q.Labels {
		where = append(where, `EXISTS (SELECT 1 FROM card_labels cl JOIN labels l ON l.id = cl.label_id WHERE cl.card_id = c.id AND (lower(l.name) = ? OR lower(l.colour) = ?))`)
		args = append(args, l, l)
	}
	for _, m := range q.Members {
		where = append(where, `EXISTS (SELECT 1 FROM card_members cm JOIN users u ON u.id = cm.user_id WHERE cm.card_id = c.id
			AND (lower(u.email) = ? OR lower(u.email) LIKE ? OR lower(replace(u.display_name, ' ', '')) = ?))`)
		args = append(args, m, m+"@%", strings.ReplaceAll(m, " ", ""))
	}
	if q.Overdue {
		where = append(where, "c.due_at IS NOT NULL AND c.due_at < ? AND c.due_done = FALSE")
		args = append(args, now.UTC())
	}
	sb.WriteString(" WHERE " + strings.Join(where, " AND "))
	if match != "" {
		sb.WriteString(" ORDER BY bm25(search_index, 5.0, 1.0)")
	} else {
		sb.WriteString(" ORDER BY c.updated_at DESC")
	}
	sb.WriteString(" LIMIT " + strconv.Itoa(limit) + " OFFSET " + strconv.Itoa(offset))
	rows, err := db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.EntityType, &h.EntityID, &h.CardID, &h.BoardID, &h.CardTitle, &h.ListID, &h.Snippet, &h.Archived); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
