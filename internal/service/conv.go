package service

import (
	"context"
	"encoding/json"
	"strings"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/store"
)

// Conversions from store rows to API models.

func userOut(u store.User) User {
	return User{
		Id: u.ID, Email: u.Email, DisplayName: u.DisplayName, AvatarUrl: u.AvatarUrl,
		IsAdmin: u.IsAdmin, Disabled: u.DisabledAt.Valid, CreatedAt: u.CreatedAt,
	}
}

func userRef(u store.User) *UserRef {
	name := u.DisplayName
	if name == "" {
		name = u.Email
	}
	return &UserRef{Id: u.ID, DisplayName: name, AvatarUrl: u.AvatarUrl}
}

func (s *Service) usersByID(ctx context.Context, ids []string) (map[string]store.User, error) {
	out := map[string]store.User{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.DB.R.GetUsersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, u := range rows {
		out[u.ID] = u
	}
	return out, nil
}

func workspaceOut(w store.Workspace, role string) Workspace {
	return Workspace{
		Id: w.ID, Name: w.Name, Slug: w.Slug, Description: w.Description, Role: WorkspaceRole(role),
		CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt, Version: w.Version,
	}
}

func parseSettings(raw string) BoardSettings {
	var st BoardSettings
	_ = json.Unmarshal([]byte(raw), &st)
	return st
}

func boardOut(b store.Board, role string) Board {
	return Board{
		Id: b.ID, WorkspaceId: b.WorkspaceID, Name: b.Name, Description: b.Description,
		Visibility: BoardVisibility(b.Visibility), Background: b.Background, IsTemplate: b.IsTemplate,
		Archived: b.ArchivedAt.Valid, Settings: parseSettings(b.Settings), Role: BoardRole(role),
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, Version: b.Version,
	}
}

func listOut(l store.List) List {
	return List{
		Id: l.ID, BoardId: l.BoardID, Name: l.Name, Position: l.Position, Archived: l.ArchivedAt.Valid,
		CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt, Version: l.Version,
	}
}

func labelOut(l store.Label) Label {
	return Label{Id: l.ID, BoardId: l.BoardID, Name: l.Name, Colour: l.Colour, Version: l.Version}
}

// cardBase converts the card row. Callers fill labels, members and counts.
func cardBase(c store.Card) Card {
	return Card{
		Id: c.ID, ListId: c.ListID, BoardId: c.BoardID, Title: c.Title, Description: c.Description,
		Position: c.Position, DueAt: timep(c.DueAt), DueDone: c.DueDone, StartAt: timep(c.StartAt),
		CoverAttachmentId: strp(c.CoverAttachmentID), IsTemplate: c.IsTemplate,
		RecurrenceId: strp(c.RecurrenceID), Archived: c.ArchivedAt.Valid,
		LabelIds: []string{}, MemberIds: []string{},
		CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Version: c.Version,
	}
}

// cardOut converts one card with its labels, members and counts, read
// through q so it sees uncommitted changes in a transaction.
func cardOut(ctx context.Context, q *store.Queries, c store.Card) (Card, error) {
	out := cardBase(c)
	var err error
	if out.LabelIds, err = q.ListCardLabelIDs(ctx, c.ID); err != nil {
		return out, err
	}
	if out.MemberIds, err = q.ListCardMembers(ctx, c.ID); err != nil {
		return out, err
	}
	if out.LabelIds == nil {
		out.LabelIds = []string{}
	}
	if out.MemberIds == nil {
		out.MemberIds = []string{}
	}
	cnt, err := q.GetCardCounts(ctx, c.ID)
	if err != nil {
		return out, err
	}
	out.Counts = CardCounts{
		ChecklistItems: int(cnt.ChecklistItems), ChecklistDone: int(cnt.ChecklistDone),
		Comments: int(cnt.Comments), Attachments: int(cnt.Attachments),
	}
	return out, nil
}

func checklistItemOut(i store.ChecklistItem) ChecklistItem {
	return ChecklistItem{
		Id: i.ID, ChecklistId: i.ChecklistID, CardId: i.CardID, Text: i.Text, Done: i.Done,
		Position: i.Position, AssigneeId: strp(i.AssigneeID), DueAt: timep(i.DueAt), Version: i.Version,
	}
}

func checklistOut(c store.Checklist, items []store.ChecklistItem) Checklist {
	out := Checklist{Id: c.ID, CardId: c.CardID, Title: c.Title, Position: c.Position, Version: c.Version, Items: []ChecklistItem{}}
	for _, i := range items {
		if i.ChecklistID == c.ID {
			out.Items = append(out.Items, checklistItemOut(i))
		}
	}
	return out
}

func commentOut(c store.Comment, author store.User) Comment {
	ref := userRef(author)
	if ref.Id == "" {
		ref = &UserRef{Id: c.AuthorID, DisplayName: "Unknown user"}
	}
	return Comment{
		Id: c.ID, CardId: c.CardID, Author: *ref, ActorLabel: c.ActorLabel, Body: c.Body,
		EditedAt: timep(c.EditedAt), CreatedAt: c.CreatedAt, Version: c.Version,
	}
}

func attachmentOut(a store.Attachment) Attachment {
	return Attachment{
		Id: a.ID, CardId: a.CardID, Filename: a.Filename, ContentType: a.ContentType, SizeBytes: a.SizeBytes,
		UploaderId: a.UploaderID, CreatedAt: a.CreatedAt, Url: "/api/v1/attachments/" + a.ID + "/download",
	}
}

func linkOut(l store.CardLink) CardLink {
	out := CardLink{
		Id: l.ID, CardId: l.CardID, BoardId: l.BoardID, Kind: LinkKind(l.Kind), ExternalKey: l.ExternalKey,
		Title: l.Title, Repo: l.Repo, Branch: l.Branch, Host: l.Host, StatusDetail: l.StatusDetail,
		LastSeenAt: timep(l.LastSeenAt), Meta: map[string]any{}, CreatedAt: l.CreatedAt,
		UpdatedAt: l.UpdatedAt, Version: l.Version,
	}
	if l.Status != "" {
		out.Status = ptr(LinkStatus(l.Status))
	}
	_ = json.Unmarshal([]byte(l.Meta), &out.Meta)
	return out
}

func recurrenceOut(r store.CardRecurrence) Recurrence {
	out := Recurrence{
		Id: r.ID, BoardId: r.BoardID, TemplateCardId: strp(r.TemplateCardID), TargetListId: r.TargetListID,
		Rrule: r.Rrule, Dtstart: r.Dtstart, Timezone: r.Timezone, NextRunAt: timep(r.NextRunAt),
		LastRunAt: timep(r.LastRunAt), Paused: r.PausedAt.Valid, CreatedBy: r.CreatedBy, Version: r.Version,
	}
	if r.DueOffsetMinutes.Valid {
		out.DueOffsetMinutes = ptr(int(r.DueOffsetMinutes.Int64))
	}
	return out
}

func activityOut(r store.Activity, actor *UserRef) Activity {
	out := Activity{
		Id: r.ID, WorkspaceId: r.WorkspaceID, BoardId: strp(r.BoardID), CardId: strp(r.CardID),
		Actor: actor, ActorLabel: r.ActorLabel, Type: r.Type, Payload: map[string]any{}, CreatedAt: r.CreatedAt,
	}
	_ = json.Unmarshal([]byte(r.Payload), &out.Payload)
	return out
}

func tokenOut(t store.ApiToken) Token {
	out := Token{
		Id: t.ID, Name: t.Name, Prefix: t.Prefix, Scopes: []TokenScope{}, WorkspaceId: strp(t.WorkspaceID),
		BoardId: strp(t.BoardID), ActorLabel: t.ActorLabel, ExpiresAt: timep(t.ExpiresAt),
		LastUsedAt: timep(t.LastUsedAt), Revoked: t.RevokedAt.Valid, CreatedAt: t.CreatedAt,
	}
	for _, s := range strings.Split(t.Scopes, ",") {
		if s != "" {
			out.Scopes = append(out.Scopes, TokenScope(s))
		}
	}
	return out
}
