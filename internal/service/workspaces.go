package service

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	. "github.com/edward-murrell/kowhai/internal/apitypes"
	"github.com/edward-murrell/kowhai/internal/store"
)

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.ToLower(name)
	// Drop macrons and other marks: "Kōwhai" becomes "kowhai".
	s = strings.Map(func(r rune) rune {
		switch r {
		case 'ā':
			return 'a'
		case 'ē':
			return 'e'
		case 'ī':
			return 'i'
		case 'ō':
			return 'o'
		case 'ū':
			return 'u'
		}
		return r
	}, s)
	s = strings.Trim(slugStrip.ReplaceAllString(s, "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		s = "workspace"
	}
	return s
}

func (s *Service) ListWorkspaces(ctx context.Context, cursor *string, limit *int) (WorkspacePage, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return WorkspacePage{}, err
	}
	l := clampLimit(limit, 100, 200)
	rows, err := s.DB.R.ListUserWorkspaces(ctx, store.ListUserWorkspacesParams{UserID: a.User.ID, After: deref(cursor, ""), Limit: int64(l + 1)})
	if err != nil {
		return WorkspacePage{}, err
	}
	page := WorkspacePage{Items: []Workspace{}}
	if len(rows) > l {
		rows = rows[:l]
		page.NextCursor = ptr(rows[l-1].ID)
	}
	lim := a.limits()
	for _, r := range rows {
		w := store.Workspace{ID: r.ID, Name: r.Name, Slug: r.Slug, Description: r.Description, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version}
		if lim != nil && !lim.allows(ActWorkspaceView, Resource{WorkspaceID: w.ID}) {
			continue
		}
		page.Items = append(page.Items, workspaceOut(w, r.Role))
	}
	return page, nil
}

func (s *Service) CreateWorkspace(ctx context.Context, in CreateWorkspace) (Workspace, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Workspace{}, err
	}
	if err := s.Authorize(ctx, a, ActWorkspaceCreate, Resource{}); err != nil {
		return Workspace{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Workspace{}, badRequest("name is required")
	}
	var out store.Workspace
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		base := slugify(name)
		slug := base
		for i := 2; ; i++ {
			n, err := tx.CountWorkspaceSlug(ctx, slug)
			if err != nil {
				return err
			}
			if n == 0 {
				break
			}
			slug = base + "-" + RandomString(4)
			if i > 10 {
				return conflict("could not pick a unique slug")
			}
		}
		now := s.now()
		w, err := tx.CreateWorkspace(ctx, store.CreateWorkspaceParams{
			ID: s.NewID(), Name: name, Slug: slug, Description: deref(in.Description, ""),
			CreatedBy: a.User.ID, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		if err := tx.UpsertWorkspaceMember(ctx, store.UpsertWorkspaceMemberParams{WorkspaceID: w.ID, UserID: a.User.ID, Role: "owner", CreatedAt: now}); err != nil {
			return err
		}
		out = w
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: w.ID, Type: TWorkspaceCreated, Payload: map[string]any{"name": w.Name}})
		return err
	})
	return workspaceOut(out, "owner"), err
}

func (s *Service) GetWorkspace(ctx context.Context, id string) (Workspace, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Workspace{}, err
	}
	w, err := s.loadWorkspace(ctx, a, id, ActWorkspaceView)
	if err != nil {
		return Workspace{}, err
	}
	role, _ := s.DB.R.GetWorkspaceRole(ctx, store.GetWorkspaceRoleParams{WorkspaceID: id, UserID: a.User.ID})
	return workspaceOut(w, role), nil
}

func (s *Service) UpdateWorkspace(ctx context.Context, id, ifMatch string, in UpdateWorkspace) (Workspace, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return Workspace{}, err
	}
	if _, err := s.loadWorkspace(ctx, a, id, ActWorkspaceUpdate); err != nil {
		return Workspace{}, err
	}
	var out store.Workspace
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		w, err := tx.GetWorkspace(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if err := CheckVersion(ifMatch, w.Version); err != nil {
			return err
		}
		name := strings.TrimSpace(deref(in.Name, w.Name))
		if name == "" {
			return badRequest("name is required")
		}
		out, err = tx.UpdateWorkspace(ctx, store.UpdateWorkspaceParams{ID: id, Name: name, Description: deref(in.Description, w.Description), UpdatedAt: s.now()})
		if err != nil {
			return err
		}
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: id, Type: TWorkspaceUpdated, Payload: map[string]any{"name": out.Name}})
		return err
	})
	role, _ := s.DB.R.GetWorkspaceRole(ctx, store.GetWorkspaceRoleParams{WorkspaceID: id, UserID: a.User.ID})
	return workspaceOut(out, role), err
}

func (s *Service) DeleteWorkspace(ctx context.Context, id string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	if _, err := s.loadWorkspace(ctx, a, id, ActWorkspaceDelete); err != nil {
		return err
	}
	var boards []store.Board
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		keys, err := tx.ListAllAttachmentKeysForWorkspace(ctx, id)
		if err != nil {
			return err
		}
		for _, k := range keys {
			if err := tx.QueueBlobDeletion(ctx, store.QueueBlobDeletionParams{ObjectKey: k, CreatedAt: s.now()}); err != nil {
				return err
			}
		}
		if boards, err = tx.ListWorkspaceBoards(ctx, store.ListWorkspaceBoardsParams{WorkspaceID: id, IncludeArchived: true}); err != nil {
			return err
		}
		tmpl, err := tx.ListWorkspaceBoards(ctx, store.ListWorkspaceBoardsParams{WorkspaceID: id, IsTemplate: true, IncludeArchived: true})
		if err != nil {
			return err
		}
		boards = append(boards, tmpl...)
		for _, b := range boards {
			if err := s.unindexBoard(ctx, tx, b.ID); err != nil {
				return err
			}
		}
		return tx.DeleteWorkspace(ctx, id)
	})
	if err == nil {
		for _, b := range boards {
			s.PublishResync(b.ID)
		}
	}
	return err
}

// ---- Members ----

func memberOut(u store.User, role string, joined time.Time) WorkspaceMember {
	return WorkspaceMember{User: userOut(u), Role: WorkspaceRole(role), JoinedAt: joined}
}

func (s *Service) ListWorkspaceMembers(ctx context.Context, id string) ([]WorkspaceMember, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.loadWorkspace(ctx, a, id, ActWorkspaceView); err != nil {
		return nil, err
	}
	rows, err := s.DB.R.ListWorkspaceMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	out := []WorkspaceMember{}
	for _, r := range rows {
		out = append(out, memberOut(r.User, r.Role, r.JoinedAt))
	}
	return out, nil
}

func validWorkspaceRole(r string) bool {
	return r == "owner" || r == "admin" || r == "member" || r == "viewer"
}

// setWorkspaceRole adds or changes a member. Only owners can grant or
// take away the owner role, and the last owner cannot be demoted.
func (s *Service) setWorkspaceRole(ctx context.Context, tx *store.Tx, a *Actor, wsID, userID, role string) (WorkspaceMember, error) {
	if !validWorkspaceRole(role) {
		return WorkspaceMember{}, badRequest("unknown role %q", role)
	}
	myRole, _ := tx.GetWorkspaceRole(ctx, store.GetWorkspaceRoleParams{WorkspaceID: wsID, UserID: a.User.ID})
	current, err := tx.GetWorkspaceRole(ctx, store.GetWorkspaceRoleParams{WorkspaceID: wsID, UserID: userID})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return WorkspaceMember{}, err
	}
	if (role == "owner" || current == "owner") && myRole != "owner" {
		return WorkspaceMember{}, forbidden("only owners can change the owner role")
	}
	if current == "owner" && role != "owner" {
		n, err := tx.CountWorkspaceOwners(ctx, wsID)
		if err != nil {
			return WorkspaceMember{}, err
		}
		if n <= 1 {
			return WorkspaceMember{}, conflict("a workspace needs at least one owner")
		}
	}
	u, err := tx.GetUser(ctx, userID)
	if err != nil {
		return WorkspaceMember{}, notFound(err)
	}
	now := s.now()
	if err := tx.UpsertWorkspaceMember(ctx, store.UpsertWorkspaceMemberParams{WorkspaceID: wsID, UserID: userID, Role: role, CreatedAt: now}); err != nil {
		return WorkspaceMember{}, err
	}
	ws, err := tx.GetWorkspace(ctx, wsID)
	if err != nil {
		return WorkspaceMember{}, err
	}
	in := activityInput{
		WorkspaceID: wsID, Type: TWorkspaceMemberAdded,
		Payload: map[string]any{"user_id": u.ID, "user_name": u.DisplayName, "role": role, "workspace_name": ws.Name},
	}
	if current == "" {
		in.Notify = []notifyGroup{{Pref: PrefInvite, Users: []string{userID}}}
	} else {
		in.Payload["previous_role"] = current
	}
	if _, err := s.record(ctx, tx, a, in); err != nil {
		return WorkspaceMember{}, err
	}
	return memberOut(u, role, now), nil
}

func (s *Service) AddWorkspaceMember(ctx context.Context, wsID string, in AddWorkspaceMember) (WorkspaceMember, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return WorkspaceMember{}, err
	}
	if _, err := s.loadWorkspace(ctx, a, wsID, ActWorkspaceMembers); err != nil {
		return WorkspaceMember{}, err
	}
	var out WorkspaceMember
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		out, err = s.setWorkspaceRole(ctx, tx, a, wsID, in.UserId, string(in.Role))
		return err
	})
	return out, err
}

func (s *Service) UpdateWorkspaceMember(ctx context.Context, wsID, userID string, in UpdateWorkspaceMember) (WorkspaceMember, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return WorkspaceMember{}, err
	}
	if _, err := s.loadWorkspace(ctx, a, wsID, ActWorkspaceMembers); err != nil {
		return WorkspaceMember{}, err
	}
	var out WorkspaceMember
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		if _, err := tx.GetWorkspaceRole(ctx, store.GetWorkspaceRoleParams{WorkspaceID: wsID, UserID: userID}); err != nil {
			return notFound(err)
		}
		out, err = s.setWorkspaceRole(ctx, tx, a, wsID, userID, string(in.Role))
		return err
	})
	return out, err
}

// RemoveWorkspaceMember removes a member. Members can always remove
// themselves (leave), unless they are the last owner.
func (s *Service) RemoveWorkspaceMember(ctx context.Context, wsID, userID string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	action := ActWorkspaceMembers
	if userID == a.User.ID {
		action = ActWorkspaceView
	}
	if _, err := s.loadWorkspace(ctx, a, wsID, action); err != nil {
		return err
	}
	return s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		current, err := tx.GetWorkspaceRole(ctx, store.GetWorkspaceRoleParams{WorkspaceID: wsID, UserID: userID})
		if err != nil {
			return notFound(err)
		}
		if current == "owner" {
			myRole, _ := tx.GetWorkspaceRole(ctx, store.GetWorkspaceRoleParams{WorkspaceID: wsID, UserID: a.User.ID})
			if myRole != "owner" {
				return forbidden("only owners can remove an owner")
			}
			n, err := tx.CountWorkspaceOwners(ctx, wsID)
			if err != nil {
				return err
			}
			if n <= 1 {
				return conflict("a workspace needs at least one owner")
			}
		}
		if err := tx.DeleteWorkspaceMember(ctx, store.DeleteWorkspaceMemberParams{WorkspaceID: wsID, UserID: userID}); err != nil {
			return err
		}
		// Board memberships in this workspace go too.
		boards, err := tx.ListWorkspaceBoards(ctx, store.ListWorkspaceBoardsParams{WorkspaceID: wsID, IncludeArchived: true})
		if err != nil {
			return err
		}
		for _, b := range boards {
			if err := tx.DeleteBoardMember(ctx, store.DeleteBoardMemberParams{BoardID: b.ID, UserID: userID}); err != nil {
				return err
			}
		}
		u, _ := tx.GetUser(ctx, userID)
		_, err = s.record(ctx, tx, a, activityInput{
			WorkspaceID: wsID, Type: TWorkspaceMemberRemove,
			Payload: map[string]any{"user_id": userID, "user_name": u.DisplayName},
		})
		return err
	})
}

// ---- Invites ----

func inviteOut(i store.WorkspaceInvite) WorkspaceInvite {
	return WorkspaceInvite{Id: i.ID, Email: i.Email, Role: InviteRole(i.Role), ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt}
}

func (s *Service) ListWorkspaceInvites(ctx context.Context, wsID string) ([]WorkspaceInvite, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.loadWorkspace(ctx, a, wsID, ActWorkspaceMembers); err != nil {
		return nil, err
	}
	rows, err := s.DB.R.ListWorkspaceInvites(ctx, store.ListWorkspaceInvitesParams{WorkspaceID: wsID, ExpiresAt: s.now()})
	if err != nil {
		return nil, err
	}
	out := []WorkspaceInvite{}
	for _, r := range rows {
		out = append(out, inviteOut(r))
	}
	return out, nil
}

// CreateInvite adds an existing user at once, or stores an invite that is
// accepted when someone with that email first signs in.
func (s *Service) CreateInvite(ctx context.Context, wsID string, in CreateInvite) (InviteResult, error) {
	a, err := mustActor(ctx)
	if err != nil {
		return InviteResult{}, err
	}
	if _, err := s.loadWorkspace(ctx, a, wsID, ActWorkspaceMembers); err != nil {
		return InviteResult{}, err
	}
	email := strings.ToLower(strings.TrimSpace(string(in.Email)))
	if !strings.Contains(email, "@") {
		return InviteResult{}, badRequest("email is not valid")
	}
	var out InviteResult
	err = s.DB.WriteTx(ctx, func(tx *store.Tx) error {
		if u, err := tx.GetUserByEmail(ctx, email); err == nil {
			if _, err := tx.GetWorkspaceRole(ctx, store.GetWorkspaceRoleParams{WorkspaceID: wsID, UserID: u.ID}); err == nil {
				return conflict("that person is already a member")
			}
			m, err := s.setWorkspaceRole(ctx, tx, a, wsID, u.ID, string(in.Role))
			out.Member = &m
			return err
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := s.now()
		inv, err := tx.UpsertWorkspaceInvite(ctx, store.UpsertWorkspaceInviteParams{
			ID: s.NewID(), WorkspaceID: wsID, Email: email, Role: string(in.Role), InvitedBy: a.User.ID,
			ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now,
		})
		if err != nil {
			return err
		}
		out.Invite = ptr(inviteOut(inv))
		_, err = s.record(ctx, tx, a, activityInput{WorkspaceID: wsID, Type: TWorkspaceInvited, Payload: map[string]any{"email": email, "role": string(in.Role)}})
		return err
	})
	return out, err
}

func (s *Service) DeleteInvite(ctx context.Context, wsID, inviteID string) error {
	a, err := mustActor(ctx)
	if err != nil {
		return err
	}
	if _, err := s.loadWorkspace(ctx, a, wsID, ActWorkspaceMembers); err != nil {
		return err
	}
	return store.New(s.DB.Write).DeleteWorkspaceInviteByID(ctx, store.DeleteWorkspaceInviteByIDParams{ID: inviteID, WorkspaceID: wsID})
}
