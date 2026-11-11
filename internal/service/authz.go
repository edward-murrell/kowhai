package service

import (
	"context"
	"database/sql"
	"errors"

	"kowhai/internal/store"
)

// Action is something an actor wants to do.
type Action int

const (
	ActWorkspaceCreate  Action = iota // create a new workspace
	ActWorkspaceView                  // see the workspace and its member list
	ActWorkspaceUpdate                // rename, change description
	ActWorkspaceDelete                // delete the workspace
	ActWorkspaceMembers               // invite, add, remove, change roles
	ActBoardCreate                    // create a board in the workspace
	ActBoardView                      // see a board and everything on it
	ActBoardEdit                      // change lists, cards, comments, labels
	ActBoardAdmin                     // board settings, members, delete
	ActSiteAdmin                      // user management
	ActTokenManage                    // create and revoke tokens
)

var actionNames = map[Action]string{
	ActWorkspaceCreate: "workspace.create", ActWorkspaceView: "workspace.view",
	ActWorkspaceUpdate: "workspace.update", ActWorkspaceDelete: "workspace.delete",
	ActWorkspaceMembers: "workspace.members", ActBoardCreate: "board.create",
	ActBoardView: "board.view", ActBoardEdit: "board.edit", ActBoardAdmin: "board.admin",
	ActSiteAdmin: "site.admin", ActTokenManage: "token.manage",
}

func (a Action) String() string { return actionNames[a] }

func (a Action) writes() bool {
	return a != ActWorkspaceView && a != ActBoardView
}

// Resource is what the action applies to. Leave fields empty when they do
// not apply.
type Resource struct {
	WorkspaceID string
	BoardID     string
	// Visibility of the board: "private" or "workspace".
	Visibility string
}

// BoardResource builds a Resource from a board row.
func BoardResource(b store.Board) Resource {
	return Resource{WorkspaceID: b.WorkspaceID, BoardID: b.ID, Visibility: b.Visibility}
}

// Roles are the facts a decision is made from.
type Roles struct {
	SiteAdmin bool
	// Workspace is "", owner, admin, member or viewer.
	Workspace string
	// Board is "", admin, editor or viewer. Only set from board_members.
	Board string
}

// EffectiveBoardRole works out the role a user has on a board:
//   - workspace owners and admins are board admins on every board;
//   - a board member has their board role;
//   - on a workspace-visible board, workspace members are editors and
//     workspace viewers are viewers;
//   - otherwise there is no access.
func EffectiveBoardRole(r Roles, visibility string) string {
	switch r.Workspace {
	case "owner", "admin":
		return "admin"
	}
	if r.Board != "" {
		return r.Board
	}
	if visibility == "workspace" {
		switch r.Workspace {
		case "member":
			return "editor"
		case "viewer":
			return "viewer"
		}
	}
	return ""
}

// Decide is the pure permission rule. Authorize loads the roles and
// calls it. tok is nil for session requests.
func Decide(action Action, r Roles, res Resource, tok *TokenLimits) bool {
	if tok != nil && !tok.allows(action, res) {
		return false
	}
	switch action {
	case ActWorkspaceCreate:
		return true
	case ActSiteAdmin:
		return r.SiteAdmin
	case ActTokenManage:
		return true
	case ActWorkspaceView:
		return r.Workspace != ""
	case ActWorkspaceUpdate, ActWorkspaceMembers:
		return r.Workspace == "owner" || r.Workspace == "admin"
	case ActWorkspaceDelete:
		return r.Workspace == "owner"
	case ActBoardCreate:
		return r.Workspace == "owner" || r.Workspace == "admin" || r.Workspace == "member"
	}
	role := EffectiveBoardRole(r, res.Visibility)
	switch action {
	case ActBoardView:
		return role != ""
	case ActBoardEdit:
		return role == "admin" || role == "editor"
	case ActBoardAdmin:
		return role == "admin"
	}
	return false
}

// TokenLimits are the limits a personal access token adds on top of the
// user's own roles.
type TokenLimits struct {
	Write       bool
	WorkspaceID string
	BoardID     string
	// BoardWorkspace is the workspace that BoardID belongs to.
	BoardWorkspace string
}

func (t *TokenLimits) allows(action Action, res Resource) bool {
	if action.writes() && !t.Write {
		return false
	}
	switch action {
	case ActTokenManage, ActSiteAdmin:
		// Tokens cannot mint tokens or manage users.
		return false
	case ActWorkspaceCreate:
		return t.WorkspaceID == "" && t.BoardID == ""
	}
	if t.WorkspaceID != "" && res.WorkspaceID != t.WorkspaceID {
		return false
	}
	if t.BoardID != "" {
		if res.BoardID == "" {
			// Workspace level: only viewing the board's own workspace.
			return action == ActWorkspaceView && res.WorkspaceID == t.BoardWorkspace
		}
		return res.BoardID == t.BoardID
	}
	return true
}

func (a *Actor) limits() *TokenLimits {
	if a.Token == nil {
		return nil
	}
	return &TokenLimits{
		Write:          a.CanWrite(),
		WorkspaceID:    a.Token.WorkspaceID.String,
		BoardID:        a.Token.BoardID.String,
		BoardWorkspace: a.TokenBoardWorkspace,
	}
}

// roles loads the actor's roles for res.
func (s *Service) roles(ctx context.Context, a *Actor, res Resource) (Roles, error) {
	r := Roles{SiteAdmin: a.User.IsAdmin}
	if res.WorkspaceID != "" {
		role, err := s.DB.R.GetWorkspaceRole(ctx, store.GetWorkspaceRoleParams{WorkspaceID: res.WorkspaceID, UserID: a.User.ID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return r, err
		}
		r.Workspace = role
	}
	if res.BoardID != "" {
		role, err := s.DB.R.GetBoardRole(ctx, store.GetBoardRoleParams{BoardID: res.BoardID, UserID: a.User.ID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return r, err
		}
		r.Board = role
	}
	return r, nil
}

// Authorize makes every permission decision. Handlers never check roles
// directly. A caller who cannot even see the resource gets ErrNotFound so
// that private boards do not leak.
func (s *Service) Authorize(ctx context.Context, a *Actor, action Action, res Resource) error {
	if a == nil {
		return ErrUnauthorized
	}
	r, err := s.roles(ctx, a, res)
	if err != nil {
		return err
	}
	if Decide(action, r, res, a.limits()) {
		return nil
	}
	if a.Token != nil && action.writes() && !a.CanWrite() {
		return forbidden("this token is read-only")
	}
	// Hide things the actor cannot see at all.
	view := ActWorkspaceView
	if res.BoardID != "" {
		view = ActBoardView
	}
	if action != ActWorkspaceCreate && action != ActSiteAdmin && action != ActTokenManage &&
		(res.WorkspaceID != "" || res.BoardID != "") && !Decide(view, r, res, a.limits()) {
		return ErrNotFound
	}
	return ErrForbidden
}

// boardRole returns the actor's effective role on b ("" for none).
func (s *Service) boardRole(ctx context.Context, a *Actor, b store.Board) (string, error) {
	res := BoardResource(b)
	r, err := s.roles(ctx, a, res)
	if err != nil {
		return "", err
	}
	if !Decide(ActBoardView, r, res, a.limits()) {
		return "", nil
	}
	role := EffectiveBoardRole(r, b.Visibility)
	// A read-only token sees everything as a viewer.
	if role != "viewer" && !a.CanWrite() {
		role = "viewer"
	}
	return role, nil
}

// loadBoard fetches a board and checks action on it.
func (s *Service) loadBoard(ctx context.Context, a *Actor, id string, action Action) (store.Board, error) {
	b, err := s.DB.R.GetBoard(ctx, id)
	if err != nil {
		return b, notFound(err)
	}
	return b, s.Authorize(ctx, a, action, BoardResource(b))
}

// loadCard fetches a card and its board and checks action on the board.
func (s *Service) loadCard(ctx context.Context, a *Actor, id string, action Action) (store.Card, store.Board, error) {
	c, err := s.DB.R.GetCard(ctx, id)
	if err != nil {
		return c, store.Board{}, notFound(err)
	}
	b, err := s.loadBoard(ctx, a, c.BoardID, action)
	return c, b, err
}

// loadWorkspace fetches a workspace and checks action on it.
func (s *Service) loadWorkspace(ctx context.Context, a *Actor, id string, action Action) (store.Workspace, error) {
	w, err := s.DB.R.GetWorkspace(ctx, id)
	if err != nil {
		return w, notFound(err)
	}
	return w, s.Authorize(ctx, a, action, Resource{WorkspaceID: w.ID})
}
