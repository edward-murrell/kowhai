// Package mcp serves Kōwhai to Claude and other MCP clients over the
// streamable HTTP transport. Each tool calls the service layer, so
// permissions, activity rows, live updates and search indexing all apply.
// Results are compact JSON with IDs and short text.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oapi-codegen/nullable"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/service"
)

const (
	maxDescription = 4000
	maxComment     = 1000
	recentComments = 10
)

// Handler returns the /mcp handler. It needs a bearer token: the
// request's actor is bound into the tools for that request.
func Handler(svc *service.Service, version, baseURL string) http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return newServer(svc, version, strings.TrimRight(baseURL, "/"), service.ActorFrom(r.Context()))
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := service.ActorFrom(r.Context())
		if a == nil || a.Token == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="kowhai"`)
			http.Error(w, "send a personal access token as Authorization: Bearer kowhai_pat_...", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

type tools struct {
	s    *service.Service
	a    *service.Actor
	base string
}

func newServer(svc *service.Service, version, base string, a *service.Actor) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "kowhai", Title: "Kōwhai boards", Version: version}, &mcp.ServerOptions{
		Instructions: "Kōwhai is a board app with workspaces, boards, lists and cards. Use list_boards and get_board to find " +
			"lists and cards, get_card for the full card, and the write tools to update cards, comment and tick checklist items. " +
			"IDs are opaque strings; pass them back exactly.",
	})
	t := &tools{s: svc, a: a, base: base}
	ro := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: ptr(false)}
	rw := func(destructive bool) *mcp.ToolAnnotations {
		return &mcp.ToolAnnotations{DestructiveHint: ptr(destructive), OpenWorldHint: ptr(false)}
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "list_boards", Description: "List the boards this token can see.", Annotations: ro}, wrap(t, t.listBoards))
	mcp.AddTool(srv, &mcp.Tool{Name: "get_board", Description: "Get a board's lists and a summary of each card.", Annotations: ro}, wrap(t, t.getBoard))
	mcp.AddTool(srv, &mcp.Tool{Name: "search_cards", Description: "Full-text search over cards, comments and checklist items. Supports label:, member:, due:overdue and is:archived filters.", Annotations: ro}, wrap(t, t.searchCards))
	mcp.AddTool(srv, &mcp.Tool{Name: "get_card", Description: "Get a card: description, labels, members, checklists, recent comments and linked sessions.", Annotations: ro}, wrap(t, t.getCard))
	mcp.AddTool(srv, &mcp.Tool{Name: "create_card", Description: "Create a card in a list, optionally from a card template.", Annotations: rw(false)}, wrap(t, t.createCard))
	mcp.AddTool(srv, &mcp.Tool{Name: "update_card", Description: "Change a card's title, description, labels or due date.", Annotations: rw(false)}, wrap(t, t.updateCard))
	mcp.AddTool(srv, &mcp.Tool{Name: "move_card", Description: "Move a card to another list on the same board, or to another position.", Annotations: rw(false)}, wrap(t, t.moveCard))
	mcp.AddTool(srv, &mcp.Tool{Name: "add_comment", Description: "Add a Markdown comment to a card.", Annotations: rw(false)}, wrap(t, t.addComment))
	mcp.AddTool(srv, &mcp.Tool{Name: "set_checklist_item", Description: "Add a checklist item, or tick or untick one.", Annotations: rw(false)}, wrap(t, t.setChecklistItem))
	return srv
}

func ptr[T any](v T) *T { return &v }

// wrap turns a handler that returns a value into an MCP tool handler that
// returns compact JSON text. Errors go back to the model as tool errors
// so it can correct itself.
func wrap[In any](t *tools, fn func(context.Context, In) (any, error)) mcp.ToolHandlerFor[In, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		ctx = service.WithActor(ctx, t.a)
		out, err := fn(ctx, in)
		if err != nil {
			msg := err.Error()
			var se *service.Error
			if errors.As(err, &se) && se.Status >= 500 {
				msg = "internal error"
			}
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}, nil, nil
		}
		b, _ := json.Marshal(out)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
	}
}

func cut(s string, n int) (string, bool) {
	r := []rune(s)
	if len(r) <= n {
		return s, false
	}
	return string(r[:n]) + "…", true
}

func (t *tools) cardURL(c string) string { return t.base + "/c/" + c }

// ---- list_boards ----

type listBoardsIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"only boards in this workspace"`
}

type boardOut struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Workspace string `json:"workspace"`
	Role      string `json:"role"`
	Archived  bool   `json:"archived,omitempty"`
}

func (t *tools) listBoards(ctx context.Context, in listBoardsIn) (any, error) {
	ws, err := t.s.ListWorkspaces(ctx, nil, ptr(200))
	if err != nil {
		return nil, err
	}
	out := []boardOut{}
	for _, w := range ws.Items {
		if in.WorkspaceID != "" && w.Id != in.WorkspaceID {
			continue
		}
		boards, err := t.s.ListWorkspaceBoards(ctx, w.Id, false)
		if err != nil {
			return nil, err
		}
		for _, b := range boards {
			out = append(out, boardOut{ID: b.Id, Name: b.Name, Workspace: w.Name, Role: string(b.Role), Archived: b.Archived})
		}
	}
	return out, nil
}

// ---- get_board ----

type getBoardIn struct {
	BoardID string `json:"board_id" jsonschema:"the board ID"`
}

type cardSummary struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Labels    []string `json:"labels,omitempty"`
	Members   []string `json:"members,omitempty"`
	Due       string   `json:"due,omitempty"`
	Checklist string   `json:"checklist,omitempty"`
	Session   string   `json:"session,omitempty"`
}

type listSummary struct {
	ID    string        `json:"id"`
	Name  string        `json:"name"`
	Cards []cardSummary `json:"cards"`
}

func labelName(l Label) string {
	if l.Name != "" {
		return l.Name
	}
	return l.Colour
}

func (t *tools) getBoard(ctx context.Context, in getBoardIn) (any, error) {
	full, err := t.s.BoardFull(ctx, in.BoardID, false)
	if err != nil {
		return nil, err
	}
	labels := map[string]string{}
	for _, l := range full.Labels {
		labels[l.Id] = labelName(l)
	}
	users := map[string]string{}
	for _, u := range full.Users {
		users[u.Id] = u.DisplayName
	}
	sessions := map[string][]string{}
	for _, l := range full.Links {
		if l.Status != nil && *l.Status != "ended" {
			sessions[l.CardId] = append(sessions[l.CardId], string(*l.Status))
		}
	}
	lists := []listSummary{}
	idx := map[string]int{}
	for _, l := range full.Lists {
		idx[l.Id] = len(lists)
		lists = append(lists, listSummary{ID: l.Id, Name: l.Name, Cards: []cardSummary{}})
	}
	for _, c := range full.Cards {
		i, ok := idx[c.ListId]
		if !ok {
			continue
		}
		cs := cardSummary{ID: c.Id, Title: c.Title}
		for _, id := range c.LabelIds {
			cs.Labels = append(cs.Labels, labels[id])
		}
		for _, id := range c.MemberIds {
			cs.Members = append(cs.Members, users[id])
		}
		if c.DueAt != nil {
			cs.Due = c.DueAt.Format(time.RFC3339)
			if c.DueDone {
				cs.Due += " (done)"
			}
		}
		if c.Counts.ChecklistItems > 0 {
			cs.Checklist = fmt.Sprintf("%d/%d", c.Counts.ChecklistDone, c.Counts.ChecklistItems)
		}
		if s := sessions[c.Id]; len(s) > 0 {
			cs.Session = strings.Join(s, ",")
		}
		lists[i].Cards = append(lists[i].Cards, cs)
	}
	return map[string]any{"board": map[string]string{"id": full.Board.Id, "name": full.Board.Name, "role": string(full.Board.Role)}, "lists": lists}, nil
}

// ---- search_cards ----

type searchIn struct {
	Query   string `json:"query" jsonschema:"search words and filters"`
	BoardID string `json:"board_id,omitempty" jsonschema:"limit to one board"`
	Limit   int    `json:"limit,omitempty" jsonschema:"maximum results, default 20"`
}

func (t *tools) searchCards(ctx context.Context, in searchIn) (any, error) {
	limit := in.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var board *string
	if in.BoardID != "" {
		board = &in.BoardID
	}
	res, err := t.s.Search(ctx, in.Query, nil, board, nil, &limit)
	if err != nil {
		return nil, err
	}
	type hit struct {
		CardID string `json:"card_id"`
		Title  string `json:"title"`
		Board  string `json:"board"`
		In     string `json:"matched_in"`
		Text   string `json:"text"`
	}
	out := []hit{}
	for _, g := range res.Groups {
		for _, h := range g.Hits {
			out = append(out, hit{CardID: h.CardId, Title: h.CardTitle, Board: g.BoardName, In: string(h.EntityType), Text: h.Snippet})
		}
	}
	return out, nil
}

// ---- get_card ----

type cardIn struct {
	CardID string `json:"card_id" jsonschema:"the card ID"`
}

func (t *tools) getCard(ctx context.Context, in cardIn) (any, error) {
	c, err := t.s.GetCard(ctx, in.CardID)
	if err != nil {
		return nil, err
	}
	full, err := t.s.BoardFull(ctx, c.BoardId, true)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"id": c.Id, "title": c.Title, "board_id": c.BoardId, "list_id": c.ListId, "url": t.cardURL(c.Id), "version": c.Version}
	for _, l := range full.Lists {
		if l.Id == c.ListId {
			out["list"] = l.Name
		}
	}
	desc, cutDesc := cut(c.Description, maxDescription)
	out["description"] = desc
	if cutDesc {
		out["description_note"] = fmt.Sprintf("description cut to %d characters", maxDescription)
	}
	var labels, members []string
	for _, l := range full.Labels {
		if slices.Contains(c.LabelIds, l.Id) {
			labels = append(labels, labelName(l))
		}
	}
	for _, u := range full.Users {
		if slices.Contains(c.MemberIds, u.Id) {
			members = append(members, u.DisplayName)
		}
	}
	out["labels"], out["members"] = labels, members
	if c.DueAt != nil {
		out["due"] = c.DueAt.Format(time.RFC3339)
		out["due_done"] = c.DueDone
	}
	if c.Archived {
		out["archived"] = true
	}
	type item struct {
		ID   string `json:"id"`
		Text string `json:"text"`
		Done bool   `json:"done"`
	}
	type checklist struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Items []item `json:"items"`
	}
	var cls []checklist
	for _, cl := range c.Checklists {
		x := checklist{ID: cl.Id, Title: cl.Title, Items: []item{}}
		for _, it := range cl.Items {
			x.Items = append(x.Items, item{it.Id, it.Text, it.Done})
		}
		cls = append(cls, x)
	}
	out["checklists"] = cls
	comments, err := t.s.ListComments(ctx, c.Id, nil, ptr(recentComments))
	if err != nil {
		return nil, err
	}
	type comment struct {
		Author string `json:"author"`
		At     string `json:"at"`
		Body   string `json:"body"`
	}
	var cs []comment
	for i := len(comments.Items) - 1; i >= 0; i-- {
		cm := comments.Items[i]
		body, _ := cut(cm.Body, maxComment)
		author := cm.Author.DisplayName
		if cm.ActorLabel != "" {
			author += " (" + cm.ActorLabel + ")"
		}
		cs = append(cs, comment{Author: author, At: cm.CreatedAt.Format(time.RFC3339), Body: body})
	}
	out["recent_comments"] = cs
	if comments.NextCursor != nil {
		out["comments_note"] = fmt.Sprintf("showing the latest %d comments", recentComments)
	}
	var links []map[string]any
	for _, l := range c.Links {
		m := map[string]any{"kind": l.Kind, "key": l.ExternalKey}
		if l.Status != nil {
			m["status"] = *l.Status
		}
		if l.Branch != "" {
			m["branch"] = l.Branch
		}
		links = append(links, m)
	}
	if links != nil {
		out["links"] = links
	}
	return out, nil
}

// ---- create_card ----

type createCardIn struct {
	ListID         string   `json:"list_id" jsonschema:"the list to add the card to"`
	Title          string   `json:"title" jsonschema:"card title"`
	Description    string   `json:"description,omitempty" jsonschema:"Markdown description"`
	TemplateCardID string   `json:"template_card_id,omitempty" jsonschema:"copy this card template"`
	Labels         []string `json:"labels,omitempty" jsonschema:"label names or IDs"`
	DueAt          string   `json:"due_at,omitempty" jsonschema:"RFC 3339 due date"`
}

func (t *tools) labelIDs(ctx context.Context, boardID string, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	labels, err := t.s.ListLabels(ctx, boardID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range names {
		found := ""
		for _, l := range labels {
			if l.Id == n || strings.EqualFold(l.Name, n) || (l.Name == "" && strings.EqualFold(l.Colour, n)) {
				found = l.Id
				break
			}
		}
		if found == "" {
			return nil, fmt.Errorf("no label %q on this board", n)
		}
		out = append(out, found)
	}
	return out, nil
}

func parseDue(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("due_at must be RFC 3339, like 2026-11-20T17:00:00+13:00")
}

func (t *tools) cardResult(c Card) map[string]any {
	return map[string]any{"id": c.Id, "title": c.Title, "list_id": c.ListId, "url": t.cardURL(c.Id), "version": c.Version}
}

func (t *tools) createCard(ctx context.Context, in createCardIn) (any, error) {
	due, err := parseDue(in.DueAt)
	if err != nil {
		return nil, err
	}
	if in.TemplateCardID != "" {
		c, err := t.s.CopyCard(ctx, in.TemplateCardID, CopyCard{ListId: in.ListID, Title: &in.Title, AsTemplate: ptr(false)})
		if err != nil {
			return nil, err
		}
		if in.Description != "" || due != nil {
			u := UpdateCard{}
			if in.Description != "" {
				u.Description = &in.Description
			}
			if due != nil {
				u.DueAt = nullable.NewNullableWithValue(*due)
			}
			if c, err = t.s.UpdateCard(ctx, c.Id, "", u); err != nil {
				return nil, err
			}
		}
		return t.cardResult(c), nil
	}
	boardID, err := t.boardOfList(ctx, in.ListID)
	if err != nil {
		return nil, err
	}
	ids, err := t.labelIDs(ctx, boardID, in.Labels)
	if err != nil {
		return nil, err
	}
	c, err := t.s.CreateCard(ctx, in.ListID, CreateCard{Title: in.Title, Description: &in.Description, DueAt: due, LabelIds: &ids})
	if err != nil {
		return nil, err
	}
	return t.cardResult(c), nil
}

func (t *tools) boardOfList(ctx context.Context, listID string) (string, error) {
	boardID, err := t.s.BoardOfList(ctx, listID)
	if err != nil {
		return "", err
	}
	return boardID, nil
}

// ---- update_card ----

type updateCardIn struct {
	CardID       string   `json:"card_id" jsonschema:"the card ID"`
	Title        *string  `json:"title,omitempty" jsonschema:"new title"`
	Description  *string  `json:"description,omitempty" jsonschema:"new Markdown description. Replaces the old one"`
	DueAt        *string  `json:"due_at,omitempty" jsonschema:"RFC 3339 due date, or empty to clear"`
	DueDone      *bool    `json:"due_done,omitempty" jsonschema:"mark the due date complete"`
	AddLabels    []string `json:"add_labels,omitempty" jsonschema:"label names or IDs to add"`
	RemoveLabels []string `json:"remove_labels,omitempty" jsonschema:"label names or IDs to remove"`
}

func (t *tools) updateCard(ctx context.Context, in updateCardIn) (any, error) {
	c, err := t.s.GetCard(ctx, in.CardID)
	if err != nil {
		return nil, err
	}
	u := UpdateCard{Title: in.Title, Description: in.Description, DueDone: in.DueDone}
	if in.DueAt != nil {
		if *in.DueAt == "" {
			u.DueAt = nullable.NewNullNullable[time.Time]()
		} else {
			due, err := parseDue(*in.DueAt)
			if err != nil {
				return nil, err
			}
			u.DueAt = nullable.NewNullableWithValue(*due)
		}
	}
	out, err := t.s.UpdateCard(ctx, in.CardID, "", u)
	if err != nil {
		return nil, err
	}
	add, err := t.labelIDs(ctx, c.BoardId, in.AddLabels)
	if err != nil {
		return nil, err
	}
	for _, id := range add {
		if out, err = t.s.AddCardLabel(ctx, in.CardID, id); err != nil {
			return nil, err
		}
	}
	rm, err := t.labelIDs(ctx, c.BoardId, in.RemoveLabels)
	if err != nil {
		return nil, err
	}
	for _, id := range rm {
		if out, err = t.s.RemoveCardLabel(ctx, in.CardID, id); err != nil {
			return nil, err
		}
	}
	return t.cardResult(out), nil
}

// ---- move_card ----

type moveCardIn struct {
	CardID       string `json:"card_id" jsonschema:"the card ID"`
	ListID       string `json:"list_id" jsonschema:"the list to move to"`
	AfterCardID  string `json:"after_card_id,omitempty" jsonschema:"put the card just after this card"`
	BeforeCardID string `json:"before_card_id,omitempty" jsonschema:"put the card just before this card"`
	Top          bool   `json:"top,omitempty" jsonschema:"put the card at the top of the list. The default is the bottom"`
}

func (t *tools) moveCard(ctx context.Context, in moveCardIn) (any, error) {
	m := MoveCard{ListId: in.ListID}
	if in.AfterCardID != "" {
		m.AfterId = &in.AfterCardID
	}
	if in.BeforeCardID != "" {
		m.BeforeId = &in.BeforeCardID
	}
	if in.Top && m.AfterId == nil && m.BeforeId == nil {
		first, err := t.s.FirstCardInList(ctx, in.ListID)
		if err != nil {
			return nil, err
		}
		if first != "" && first != in.CardID {
			m.BeforeId = &first
		}
	}
	c, err := t.s.MoveCard(ctx, in.CardID, m)
	if err != nil {
		return nil, err
	}
	return t.cardResult(c), nil
}

// ---- add_comment ----

type commentIn struct {
	CardID string `json:"card_id" jsonschema:"the card ID"`
	Body   string `json:"body" jsonschema:"Markdown comment"`
}

func (t *tools) addComment(ctx context.Context, in commentIn) (any, error) {
	c, err := t.s.CreateComment(ctx, in.CardID, in.Body)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": c.Id, "card_id": c.CardId}, nil
}

// ---- set_checklist_item ----

type checklistIn struct {
	CardID    string `json:"card_id" jsonschema:"the card ID"`
	ItemID    string `json:"item_id,omitempty" jsonschema:"the item to tick or untick. Leave out to add a new item"`
	Done      *bool  `json:"done,omitempty" jsonschema:"true to tick, false to untick"`
	Text      string `json:"text,omitempty" jsonschema:"text for a new item, or new text for an existing one"`
	Checklist string `json:"checklist,omitempty" jsonschema:"checklist ID or title for a new item. Made if missing. Defaults to the first checklist"`
}

func (t *tools) setChecklistItem(ctx context.Context, in checklistIn) (any, error) {
	c, err := t.s.GetCard(ctx, in.CardID)
	if err != nil {
		return nil, err
	}
	if in.ItemID != "" {
		found := false
		for _, cl := range c.Checklists {
			for _, it := range cl.Items {
				found = found || it.Id == in.ItemID
			}
		}
		if !found {
			return nil, errors.New("no item with that ID on this card")
		}
		u := UpdateChecklistItem{Done: in.Done}
		if in.Text != "" {
			u.Text = &in.Text
		}
		it, err := t.s.UpdateChecklistItem(ctx, in.ItemID, "", u)
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": it.Id, "text": it.Text, "done": it.Done}, nil
	}
	if strings.TrimSpace(in.Text) == "" {
		return nil, errors.New("text is required to add an item")
	}
	listID := ""
	for _, cl := range c.Checklists {
		if in.Checklist == "" || cl.Id == in.Checklist || strings.EqualFold(cl.Title, in.Checklist) {
			listID = cl.Id
			break
		}
	}
	if listID == "" {
		title := in.Checklist
		if title == "" {
			title = "Checklist"
		}
		cl, err := t.s.CreateChecklist(ctx, in.CardID, CreateChecklist{Title: title})
		if err != nil {
			return nil, err
		}
		listID = cl.Id
	}
	it, err := t.s.CreateChecklistItem(ctx, listID, CreateChecklistItem{Text: in.Text, Done: in.Done})
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": it.Id, "checklist_id": it.ChecklistId, "text": it.Text, "done": it.Done}, nil
}
