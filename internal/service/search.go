package service

import (
	"context"
	"strconv"
	"strings"

	. "kowhai/internal/apitypes"
	"kowhai/internal/search"
	"kowhai/internal/store"
)

func (s *Service) unindexBoard(ctx context.Context, tx *store.Tx, boardID string) error {
	return search.RemoveBoard(ctx, tx.Conn, boardID)
}

// Search runs a full-text search over the boards the actor can see,
// optionally narrowed to one workspace or board. Results are grouped by
// board in rank order.
func (s *Service) Search(ctx context.Context, q string, workspaceID, boardID, cursor *string, limit *int) (SearchResults, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return SearchResults{}, err
	}
	out := SearchResults{Groups: []SearchGroup{}}
	query := search.Parse(q)
	if len(query.Terms) == 0 && len(query.Labels) == 0 && len(query.Members) == 0 && !query.Overdue && !query.Archived {
		return out, nil
	}
	boards, err := s.VisibleBoards(ctx, a)
	if err != nil {
		return out, err
	}
	names := map[string]store.Board{}
	var ids []string
	for _, b := range boards {
		if b.IsTemplate {
			continue
		}
		if workspaceID != nil && *workspaceID != "" && b.WorkspaceID != *workspaceID {
			continue
		}
		if boardID != nil && *boardID != "" && b.ID != *boardID {
			continue
		}
		ids = append(ids, b.ID)
		names[b.ID] = b
	}
	l := clampLimit(limit, 50, 200)
	offset, _ := strconv.Atoi(deref(cursor, "0"))
	hits, err := search.Run(ctx, s.DB.Read, query, ids, s.now(), offset, l+1)
	if err != nil {
		if strings.Contains(err.Error(), "fts5") {
			return out, badRequest("could not understand that search")
		}
		return out, err
	}
	if len(hits) > l {
		hits = hits[:l]
		out.NextCursor = ptr(strconv.Itoa(offset + l))
	}
	idx := map[string]int{}
	for _, h := range hits {
		i, ok := idx[h.BoardID]
		if !ok {
			b := names[h.BoardID]
			out.Groups = append(out.Groups, SearchGroup{BoardId: b.ID, BoardName: b.Name, WorkspaceId: b.WorkspaceID, Hits: []SearchHit{}})
			i = len(out.Groups) - 1
			idx[h.BoardID] = i
		}
		out.Groups[i].Hits = append(out.Groups[i].Hits, SearchHit{
			CardId: h.CardID, CardTitle: h.CardTitle, ListId: h.ListID, EntityType: SearchHitEntityType(h.EntityType),
			Snippet: h.Snippet, Archived: h.Archived,
		})
	}
	return out, nil
}
