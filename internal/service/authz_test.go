package service

import (
	"fmt"
	"testing"
)

// The full role matrix: every workspace role, board role and visibility
// against every action. Each row lists the actions that are allowed.
func TestDecideMatrix(t *testing.T) {
	ws := Resource{WorkspaceID: "w"}
	all := []Action{ActWorkspaceView, ActWorkspaceUpdate, ActWorkspaceDelete, ActWorkspaceMembers, ActBoardCreate, ActBoardView, ActBoardEdit, ActBoardAdmin}
	type row struct {
		wsRole, boardRole, vis string
		allow                  []Action
	}
	rows := []row{
		// Workspace owners and admins can do everything on every board.
		{"owner", "", "private", all},
		{"owner", "", "workspace", all},
		{"admin", "", "private", []Action{ActWorkspaceView, ActWorkspaceUpdate, ActWorkspaceMembers, ActBoardCreate, ActBoardView, ActBoardEdit, ActBoardAdmin}},
		{"admin", "viewer", "workspace", []Action{ActWorkspaceView, ActWorkspaceUpdate, ActWorkspaceMembers, ActBoardCreate, ActBoardView, ActBoardEdit, ActBoardAdmin}},
		// Members edit workspace boards and cannot see private ones.
		{"member", "", "workspace", []Action{ActWorkspaceView, ActBoardCreate, ActBoardView, ActBoardEdit}},
		{"member", "", "private", []Action{ActWorkspaceView, ActBoardCreate}},
		// A board role decides edit rights on that board.
		{"member", "viewer", "workspace", []Action{ActWorkspaceView, ActBoardCreate, ActBoardView}},
		{"member", "editor", "private", []Action{ActWorkspaceView, ActBoardCreate, ActBoardView, ActBoardEdit}},
		{"member", "admin", "private", []Action{ActWorkspaceView, ActBoardCreate, ActBoardView, ActBoardEdit, ActBoardAdmin}},
		// Viewers read workspace boards only.
		{"viewer", "", "workspace", []Action{ActWorkspaceView, ActBoardView}},
		{"viewer", "", "private", []Action{ActWorkspaceView}},
		{"viewer", "editor", "private", []Action{ActWorkspaceView, ActBoardView, ActBoardEdit}},
		// Outsiders see nothing, unless added to a board.
		{"", "", "workspace", nil},
		{"", "", "private", nil},
		{"", "viewer", "private", []Action{ActBoardView}},
	}
	for _, r := range rows {
		res := ws
		res.BoardID, res.Visibility = "b", r.vis
		for _, act := range all {
			want := false
			for _, a := range r.allow {
				want = want || a == act
			}
			got := Decide(act, Roles{Workspace: r.wsRole, Board: r.boardRole}, res, nil)
			if got != want {
				t.Errorf("ws=%q board=%q vis=%s %s: got %v want %v", r.wsRole, r.boardRole, r.vis, act, got, want)
			}
		}
	}
}

func TestDecideSiteAdmin(t *testing.T) {
	if !Decide(ActSiteAdmin, Roles{SiteAdmin: true}, Resource{}, nil) {
		t.Error("site admin denied")
	}
	if Decide(ActSiteAdmin, Roles{Workspace: "owner"}, Resource{}, nil) {
		t.Error("owner allowed site admin")
	}
	// Site admins get no content access by that flag alone.
	if Decide(ActBoardView, Roles{SiteAdmin: true}, Resource{WorkspaceID: "w", BoardID: "b", Visibility: "workspace"}, nil) {
		t.Error("site admin can see boards")
	}
}

func TestDecideTokenLimits(t *testing.T) {
	owner := Roles{Workspace: "owner"}
	b1 := Resource{WorkspaceID: "w1", BoardID: "b1", Visibility: "workspace"}
	b2 := Resource{WorkspaceID: "w1", BoardID: "b2", Visibility: "workspace"}
	other := Resource{WorkspaceID: "w2", BoardID: "b3", Visibility: "workspace"}
	cases := []struct {
		name string
		tok  TokenLimits
		act  Action
		res  Resource
		want bool
	}{
		{"read token reads", TokenLimits{}, ActBoardView, b1, true},
		{"read token cannot edit", TokenLimits{}, ActBoardEdit, b1, false},
		{"write token edits", TokenLimits{Write: true}, ActBoardEdit, b1, true},
		{"workspace token in workspace", TokenLimits{Write: true, WorkspaceID: "w1"}, ActBoardEdit, b2, true},
		{"workspace token elsewhere", TokenLimits{Write: true, WorkspaceID: "w1"}, ActBoardView, other, false},
		{"board token on board", TokenLimits{Write: true, BoardID: "b1", BoardWorkspace: "w1"}, ActBoardEdit, b1, true},
		{"board token on other board", TokenLimits{Write: true, BoardID: "b1", BoardWorkspace: "w1"}, ActBoardView, b2, false},
		{"board token views its workspace", TokenLimits{BoardID: "b1", BoardWorkspace: "w1"}, ActWorkspaceView, Resource{WorkspaceID: "w1"}, true},
		{"board token cannot create boards", TokenLimits{Write: true, BoardID: "b1", BoardWorkspace: "w1"}, ActBoardCreate, Resource{WorkspaceID: "w1"}, false},
		{"limited token cannot create workspaces", TokenLimits{Write: true, WorkspaceID: "w1"}, ActWorkspaceCreate, Resource{}, false},
		{"tokens cannot manage tokens", TokenLimits{Write: true}, ActTokenManage, Resource{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Decide(c.act, owner, c.res, &c.tok); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestPlaceAmong(t *testing.T) {
	sibs := []sibling{{"a", "V"}, {"b", "k"}, {"c", "t"}}
	k, re, err := placeAmong(sibs, "", "a", "")
	if err != nil || re != nil || !(k > "V" && k < "k") {
		t.Fatalf("after a: %q %v %v", k, re, err)
	}
	k, _, _ = placeAmong(sibs, "", "", "a")
	if k >= "V" {
		t.Fatalf("before a: %q", k)
	}
	k, _, _ = placeAmong(sibs, "a", "c", "")
	if k <= "t" {
		t.Fatalf("move a to end: %q", k)
	}
	if _, _, err := placeAmong(sibs, "", "zz", ""); err == nil {
		t.Fatal("unknown after_id accepted")
	}
	// Duplicate keys force a respace.
	dup := []sibling{{"a", "V"}, {"b", "V"}}
	k, re, err = placeAmong(dup, "", "a", "")
	if err != nil || len(re) != 2 || !(re[0].Pos < k && k < re[1].Pos) {
		t.Fatalf("respace: %q %v %v", k, re, err)
	}
	_ = fmt.Sprint
}
