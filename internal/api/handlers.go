// Package api serves the REST API. The generated strict server in
// api.gen.go decodes requests and encodes responses. The handlers here
// only call the service layer, which makes every permission decision.
package api

import (
	"context"

	. "kowhai/internal/apitypes"
	"kowhai/internal/service"
)

// Handlers implements StrictServerInterface.
type Handlers struct {
	S *service.Service
}

var _ StrictServerInterface = (*Handlers)(nil)

func im(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ---- me ----

func (h *Handlers) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	me, err := h.S.Me(ctx)
	return GetMe200JSONResponse(me), err
}

func (h *Handlers) ListNotifications(ctx context.Context, r ListNotificationsRequestObject) (ListNotificationsResponseObject, error) {
	unread := r.Params.Unread != nil && *r.Params.Unread
	p, err := h.S.ListNotifications(ctx, r.Params.Cursor, r.Params.Limit, unread)
	return ListNotifications200JSONResponse(p), err
}

func (h *Handlers) ReadAllNotifications(ctx context.Context, _ ReadAllNotificationsRequestObject) (ReadAllNotificationsResponseObject, error) {
	return ReadAllNotifications204Response{}, h.S.ReadAllNotifications(ctx)
}

func (h *Handlers) UpdateNotification(ctx context.Context, r UpdateNotificationRequestObject) (UpdateNotificationResponseObject, error) {
	n, err := h.S.SetNotificationRead(ctx, r.Id, r.Body.Read)
	return UpdateNotification200JSONResponse(n), err
}

func (h *Handlers) GetNotificationPrefs(ctx context.Context, _ GetNotificationPrefsRequestObject) (GetNotificationPrefsResponseObject, error) {
	p, err := h.S.GetNotificationPrefs(ctx)
	return GetNotificationPrefs200JSONResponse(p), err
}

func (h *Handlers) PutNotificationPrefs(ctx context.Context, r PutNotificationPrefsRequestObject) (PutNotificationPrefsResponseObject, error) {
	p, err := h.S.PutNotificationPrefs(ctx, *r.Body)
	return PutNotificationPrefs200JSONResponse(p), err
}

func (h *Handlers) ListTokens(ctx context.Context, _ ListTokensRequestObject) (ListTokensResponseObject, error) {
	t, err := h.S.ListTokens(ctx)
	return ListTokens200JSONResponse(t), err
}

func (h *Handlers) CreateToken(ctx context.Context, r CreateTokenRequestObject) (CreateTokenResponseObject, error) {
	t, err := h.S.CreateToken(ctx, *r.Body)
	return CreateToken201JSONResponse(t), err
}

func (h *Handlers) RevokeToken(ctx context.Context, r RevokeTokenRequestObject) (RevokeTokenResponseObject, error) {
	return RevokeToken204Response{}, h.S.RevokeToken(ctx, r.Id)
}

func (h *Handlers) SearchUsers(ctx context.Context, r SearchUsersRequestObject) (SearchUsersResponseObject, error) {
	u, err := h.S.SearchUsers(ctx, r.Params.Q)
	return SearchUsers200JSONResponse(u), err
}

// ---- admin ----

func (h *Handlers) AdminListUsers(ctx context.Context, r AdminListUsersRequestObject) (AdminListUsersResponseObject, error) {
	p, err := h.S.AdminListUsers(ctx, r.Params.Q, r.Params.Cursor, r.Params.Limit)
	return AdminListUsers200JSONResponse(p), err
}

func (h *Handlers) AdminUpdateUser(ctx context.Context, r AdminUpdateUserRequestObject) (AdminUpdateUserResponseObject, error) {
	u, err := h.S.AdminSetDisabled(ctx, r.UserId, r.Body.Disabled)
	return AdminUpdateUser200JSONResponse(u), err
}

// ---- workspaces ----

func (h *Handlers) ListWorkspaces(ctx context.Context, r ListWorkspacesRequestObject) (ListWorkspacesResponseObject, error) {
	p, err := h.S.ListWorkspaces(ctx, r.Params.Cursor, r.Params.Limit)
	return ListWorkspaces200JSONResponse(p), err
}

func (h *Handlers) CreateWorkspace(ctx context.Context, r CreateWorkspaceRequestObject) (CreateWorkspaceResponseObject, error) {
	w, err := h.S.CreateWorkspace(ctx, *r.Body)
	return CreateWorkspace201JSONResponse(w), err
}

func (h *Handlers) GetWorkspace(ctx context.Context, r GetWorkspaceRequestObject) (GetWorkspaceResponseObject, error) {
	w, err := h.S.GetWorkspace(ctx, r.Id)
	return GetWorkspace200JSONResponse(w), err
}

func (h *Handlers) UpdateWorkspace(ctx context.Context, r UpdateWorkspaceRequestObject) (UpdateWorkspaceResponseObject, error) {
	w, err := h.S.UpdateWorkspace(ctx, r.Id, im(r.Params.IfMatch), *r.Body)
	return UpdateWorkspace200JSONResponse(w), err
}

func (h *Handlers) DeleteWorkspace(ctx context.Context, r DeleteWorkspaceRequestObject) (DeleteWorkspaceResponseObject, error) {
	return DeleteWorkspace204Response{}, h.S.DeleteWorkspace(ctx, r.Id)
}

func (h *Handlers) ListWorkspaceMembers(ctx context.Context, r ListWorkspaceMembersRequestObject) (ListWorkspaceMembersResponseObject, error) {
	m, err := h.S.ListWorkspaceMembers(ctx, r.Id)
	return ListWorkspaceMembers200JSONResponse(m), err
}

func (h *Handlers) AddWorkspaceMember(ctx context.Context, r AddWorkspaceMemberRequestObject) (AddWorkspaceMemberResponseObject, error) {
	m, err := h.S.AddWorkspaceMember(ctx, r.Id, *r.Body)
	return AddWorkspaceMember201JSONResponse(m), err
}

func (h *Handlers) UpdateWorkspaceMember(ctx context.Context, r UpdateWorkspaceMemberRequestObject) (UpdateWorkspaceMemberResponseObject, error) {
	m, err := h.S.UpdateWorkspaceMember(ctx, r.Id, r.UserId, *r.Body)
	return UpdateWorkspaceMember200JSONResponse(m), err
}

func (h *Handlers) RemoveWorkspaceMember(ctx context.Context, r RemoveWorkspaceMemberRequestObject) (RemoveWorkspaceMemberResponseObject, error) {
	return RemoveWorkspaceMember204Response{}, h.S.RemoveWorkspaceMember(ctx, r.Id, r.UserId)
}

func (h *Handlers) ListWorkspaceInvites(ctx context.Context, r ListWorkspaceInvitesRequestObject) (ListWorkspaceInvitesResponseObject, error) {
	i, err := h.S.ListWorkspaceInvites(ctx, r.Id)
	return ListWorkspaceInvites200JSONResponse(i), err
}

func (h *Handlers) CreateWorkspaceInvite(ctx context.Context, r CreateWorkspaceInviteRequestObject) (CreateWorkspaceInviteResponseObject, error) {
	i, err := h.S.CreateInvite(ctx, r.Id, *r.Body)
	return CreateWorkspaceInvite201JSONResponse(i), err
}

func (h *Handlers) DeleteWorkspaceInvite(ctx context.Context, r DeleteWorkspaceInviteRequestObject) (DeleteWorkspaceInviteResponseObject, error) {
	return DeleteWorkspaceInvite204Response{}, h.S.DeleteInvite(ctx, r.Id, r.InviteId)
}

// ---- boards ----

func (h *Handlers) ListWorkspaceBoards(ctx context.Context, r ListWorkspaceBoardsRequestObject) (ListWorkspaceBoardsResponseObject, error) {
	b, err := h.S.ListWorkspaceBoards(ctx, r.Id, r.Params.Archived != nil && *r.Params.Archived)
	return ListWorkspaceBoards200JSONResponse(b), err
}

func (h *Handlers) CreateBoard(ctx context.Context, r CreateBoardRequestObject) (CreateBoardResponseObject, error) {
	b, err := h.S.CreateBoard(ctx, r.Id, *r.Body)
	return CreateBoard201JSONResponse(b), err
}

func (h *Handlers) ListBoardTemplates(ctx context.Context, r ListBoardTemplatesRequestObject) (ListBoardTemplatesResponseObject, error) {
	b, err := h.S.ListBoardTemplates(ctx, r.Id)
	return ListBoardTemplates200JSONResponse(b), err
}

func (h *Handlers) ListBuiltinTemplates(ctx context.Context, r ListBuiltinTemplatesRequestObject) (ListBuiltinTemplatesResponseObject, error) {
	b, err := h.S.ListBuiltinTemplates(ctx, r.Id)
	return ListBuiltinTemplates200JSONResponse(b), err
}

func (h *Handlers) SeedBuiltinTemplate(ctx context.Context, r SeedBuiltinTemplateRequestObject) (SeedBuiltinTemplateResponseObject, error) {
	b, err := h.S.SeedBuiltinTemplate(ctx, r.Id, r.Body.Key)
	return SeedBuiltinTemplate201JSONResponse(b), err
}

func (h *Handlers) GetBoard(ctx context.Context, r GetBoardRequestObject) (GetBoardResponseObject, error) {
	b, err := h.S.GetBoard(ctx, r.Id)
	return GetBoard200JSONResponse(b), err
}

func (h *Handlers) UpdateBoard(ctx context.Context, r UpdateBoardRequestObject) (UpdateBoardResponseObject, error) {
	b, err := h.S.UpdateBoard(ctx, r.Id, im(r.Params.IfMatch), *r.Body)
	return UpdateBoard200JSONResponse(b), err
}

func (h *Handlers) DeleteBoard(ctx context.Context, r DeleteBoardRequestObject) (DeleteBoardResponseObject, error) {
	return DeleteBoard204Response{}, h.S.DeleteBoard(ctx, r.Id)
}

func (h *Handlers) GetBoardFull(ctx context.Context, r GetBoardFullRequestObject) (GetBoardFullResponseObject, error) {
	b, err := h.S.BoardFull(ctx, r.Id, r.Params.Archived != nil && *r.Params.Archived)
	return GetBoardFull200JSONResponse(b), err
}

func (h *Handlers) CopyBoard(ctx context.Context, r CopyBoardRequestObject) (CopyBoardResponseObject, error) {
	b, err := h.S.CopyBoard(ctx, r.Id, *r.Body)
	return CopyBoard201JSONResponse(b), err
}

func (h *Handlers) ListBoardMembers(ctx context.Context, r ListBoardMembersRequestObject) (ListBoardMembersResponseObject, error) {
	m, err := h.S.ListBoardMembers(ctx, r.Id)
	return ListBoardMembers200JSONResponse(m), err
}

func (h *Handlers) AddBoardMember(ctx context.Context, r AddBoardMemberRequestObject) (AddBoardMemberResponseObject, error) {
	m, err := h.S.SetBoardMember(ctx, r.Id, r.Body.UserId, string(r.Body.Role), false)
	return AddBoardMember201JSONResponse(m), err
}

func (h *Handlers) UpdateBoardMember(ctx context.Context, r UpdateBoardMemberRequestObject) (UpdateBoardMemberResponseObject, error) {
	m, err := h.S.SetBoardMember(ctx, r.Id, r.UserId, string(r.Body.Role), true)
	return UpdateBoardMember200JSONResponse(m), err
}

func (h *Handlers) RemoveBoardMember(ctx context.Context, r RemoveBoardMemberRequestObject) (RemoveBoardMemberResponseObject, error) {
	return RemoveBoardMember204Response{}, h.S.RemoveBoardMember(ctx, r.Id, r.UserId)
}

func (h *Handlers) ListLabels(ctx context.Context, r ListLabelsRequestObject) (ListLabelsResponseObject, error) {
	l, err := h.S.ListLabels(ctx, r.Id)
	return ListLabels200JSONResponse(l), err
}

func (h *Handlers) CreateLabel(ctx context.Context, r CreateLabelRequestObject) (CreateLabelResponseObject, error) {
	l, err := h.S.CreateLabel(ctx, r.Id, *r.Body)
	return CreateLabel201JSONResponse(l), err
}

func (h *Handlers) UpdateLabel(ctx context.Context, r UpdateLabelRequestObject) (UpdateLabelResponseObject, error) {
	l, err := h.S.UpdateLabel(ctx, r.Id, im(r.Params.IfMatch), *r.Body)
	return UpdateLabel200JSONResponse(l), err
}

func (h *Handlers) DeleteLabel(ctx context.Context, r DeleteLabelRequestObject) (DeleteLabelResponseObject, error) {
	return DeleteLabel204Response{}, h.S.DeleteLabel(ctx, r.Id)
}

func (h *Handlers) ListBoardActivity(ctx context.Context, r ListBoardActivityRequestObject) (ListBoardActivityResponseObject, error) {
	p, err := h.S.ListBoardActivity(ctx, r.Id, r.Params.Cursor, r.Params.Limit)
	return ListBoardActivity200JSONResponse(p), err
}

func (h *Handlers) ListCardTemplates(ctx context.Context, r ListCardTemplatesRequestObject) (ListCardTemplatesResponseObject, error) {
	c, err := h.S.ListCardTemplates(ctx, r.Id)
	return ListCardTemplates200JSONResponse(c), err
}

// ---- lists ----

func (h *Handlers) CreateList(ctx context.Context, r CreateListRequestObject) (CreateListResponseObject, error) {
	l, err := h.S.CreateList(ctx, r.Id, *r.Body)
	return CreateList201JSONResponse(l), err
}

func (h *Handlers) UpdateList(ctx context.Context, r UpdateListRequestObject) (UpdateListResponseObject, error) {
	l, err := h.S.UpdateList(ctx, r.Id, im(r.Params.IfMatch), *r.Body)
	return UpdateList200JSONResponse(l), err
}

func (h *Handlers) MoveList(ctx context.Context, r MoveListRequestObject) (MoveListResponseObject, error) {
	l, err := h.S.MoveList(ctx, r.Id, Placement(*r.Body))
	return MoveList200JSONResponse(l), err
}

// ---- cards ----

func (h *Handlers) CreateCard(ctx context.Context, r CreateCardRequestObject) (CreateCardResponseObject, error) {
	c, err := h.S.CreateCard(ctx, r.Id, *r.Body)
	return CreateCard201JSONResponse(c), err
}

func (h *Handlers) GetCard(ctx context.Context, r GetCardRequestObject) (GetCardResponseObject, error) {
	c, err := h.S.GetCard(ctx, r.Id)
	return GetCard200JSONResponse(c), err
}

func (h *Handlers) UpdateCard(ctx context.Context, r UpdateCardRequestObject) (UpdateCardResponseObject, error) {
	c, err := h.S.UpdateCard(ctx, r.Id, im(r.Params.IfMatch), *r.Body)
	return UpdateCard200JSONResponse(c), err
}

func (h *Handlers) DeleteCard(ctx context.Context, r DeleteCardRequestObject) (DeleteCardResponseObject, error) {
	return DeleteCard204Response{}, h.S.DeleteCard(ctx, r.Id)
}

func (h *Handlers) MoveCard(ctx context.Context, r MoveCardRequestObject) (MoveCardResponseObject, error) {
	c, err := h.S.MoveCard(ctx, r.Id, *r.Body)
	return MoveCard200JSONResponse(c), err
}

func (h *Handlers) CopyCard(ctx context.Context, r CopyCardRequestObject) (CopyCardResponseObject, error) {
	c, err := h.S.CopyCard(ctx, r.Id, *r.Body)
	return CopyCard201JSONResponse(c), err
}

func (h *Handlers) ListCardActivity(ctx context.Context, r ListCardActivityRequestObject) (ListCardActivityResponseObject, error) {
	p, err := h.S.ListCardActivity(ctx, r.Id, r.Params.Cursor, r.Params.Limit)
	return ListCardActivity200JSONResponse(p), err
}

func (h *Handlers) AddCardMember(ctx context.Context, r AddCardMemberRequestObject) (AddCardMemberResponseObject, error) {
	c, err := h.S.AddCardMember(ctx, r.Id, r.Body.UserId)
	return AddCardMember200JSONResponse(c), err
}

func (h *Handlers) RemoveCardMember(ctx context.Context, r RemoveCardMemberRequestObject) (RemoveCardMemberResponseObject, error) {
	c, err := h.S.RemoveCardMember(ctx, r.Id, r.UserId)
	return RemoveCardMember200JSONResponse(c), err
}

func (h *Handlers) AddCardLabel(ctx context.Context, r AddCardLabelRequestObject) (AddCardLabelResponseObject, error) {
	c, err := h.S.AddCardLabel(ctx, r.Id, r.Body.LabelId)
	return AddCardLabel200JSONResponse(c), err
}

func (h *Handlers) RemoveCardLabel(ctx context.Context, r RemoveCardLabelRequestObject) (RemoveCardLabelResponseObject, error) {
	c, err := h.S.RemoveCardLabel(ctx, r.Id, r.LabelId)
	return RemoveCardLabel200JSONResponse(c), err
}

func (h *Handlers) WatchCard(ctx context.Context, r WatchCardRequestObject) (WatchCardResponseObject, error) {
	return WatchCard204Response{}, h.S.SetWatching(ctx, r.Id, true)
}

func (h *Handlers) UnwatchCard(ctx context.Context, r UnwatchCardRequestObject) (UnwatchCardResponseObject, error) {
	return UnwatchCard204Response{}, h.S.SetWatching(ctx, r.Id, false)
}

// ---- comments ----

func (h *Handlers) ListComments(ctx context.Context, r ListCommentsRequestObject) (ListCommentsResponseObject, error) {
	p, err := h.S.ListComments(ctx, r.Id, r.Params.Cursor, r.Params.Limit)
	return ListComments200JSONResponse(p), err
}

func (h *Handlers) CreateComment(ctx context.Context, r CreateCommentRequestObject) (CreateCommentResponseObject, error) {
	c, err := h.S.CreateComment(ctx, r.Id, r.Body.Body)
	return CreateComment201JSONResponse(c), err
}

func (h *Handlers) UpdateComment(ctx context.Context, r UpdateCommentRequestObject) (UpdateCommentResponseObject, error) {
	c, err := h.S.UpdateComment(ctx, r.Id, im(r.Params.IfMatch), r.Body.Body)
	return UpdateComment200JSONResponse(c), err
}

func (h *Handlers) DeleteComment(ctx context.Context, r DeleteCommentRequestObject) (DeleteCommentResponseObject, error) {
	return DeleteComment204Response{}, h.S.DeleteComment(ctx, r.Id)
}

// ---- checklists ----

func (h *Handlers) CreateChecklist(ctx context.Context, r CreateChecklistRequestObject) (CreateChecklistResponseObject, error) {
	c, err := h.S.CreateChecklist(ctx, r.Id, *r.Body)
	return CreateChecklist201JSONResponse(c), err
}

func (h *Handlers) UpdateChecklist(ctx context.Context, r UpdateChecklistRequestObject) (UpdateChecklistResponseObject, error) {
	c, err := h.S.UpdateChecklist(ctx, r.Id, im(r.Params.IfMatch), *r.Body)
	return UpdateChecklist200JSONResponse(c), err
}

func (h *Handlers) DeleteChecklist(ctx context.Context, r DeleteChecklistRequestObject) (DeleteChecklistResponseObject, error) {
	return DeleteChecklist204Response{}, h.S.DeleteChecklist(ctx, r.Id)
}

func (h *Handlers) CreateChecklistItem(ctx context.Context, r CreateChecklistItemRequestObject) (CreateChecklistItemResponseObject, error) {
	i, err := h.S.CreateChecklistItem(ctx, r.Id, *r.Body)
	return CreateChecklistItem201JSONResponse(i), err
}

func (h *Handlers) UpdateChecklistItem(ctx context.Context, r UpdateChecklistItemRequestObject) (UpdateChecklistItemResponseObject, error) {
	i, err := h.S.UpdateChecklistItem(ctx, r.Id, im(r.Params.IfMatch), *r.Body)
	return UpdateChecklistItem200JSONResponse(i), err
}

func (h *Handlers) DeleteChecklistItem(ctx context.Context, r DeleteChecklistItemRequestObject) (DeleteChecklistItemResponseObject, error) {
	return DeleteChecklistItem204Response{}, h.S.DeleteChecklistItem(ctx, r.Id)
}

// ---- attachments (upload and download are in stream.go) ----

func (h *Handlers) DeleteAttachment(ctx context.Context, r DeleteAttachmentRequestObject) (DeleteAttachmentResponseObject, error) {
	return DeleteAttachment204Response{}, h.S.DeleteAttachment(ctx, r.Id)
}

// ---- links ----

func (h *Handlers) ListCardLinks(ctx context.Context, r ListCardLinksRequestObject) (ListCardLinksResponseObject, error) {
	l, err := h.S.ListCardLinks(ctx, r.Id)
	return ListCardLinks200JSONResponse(l), err
}

func (h *Handlers) CreateCardLink(ctx context.Context, r CreateCardLinkRequestObject) (CreateCardLinkResponseObject, error) {
	l, err := h.S.CreateCardLink(ctx, r.Id, *r.Body)
	return CreateCardLink201JSONResponse(l), err
}

func (h *Handlers) PutCardLink(ctx context.Context, r PutCardLinkRequestObject) (PutCardLinkResponseObject, error) {
	l, err := h.S.PutCardLink(ctx, r.Id, string(r.Kind), r.ExternalKey, *r.Body, false)
	return PutCardLink200JSONResponse(l), err
}

func (h *Handlers) UpdateCardLink(ctx context.Context, r UpdateCardLinkRequestObject) (UpdateCardLinkResponseObject, error) {
	l, err := h.S.UpdateCardLink(ctx, r.Id, im(r.Params.IfMatch), *r.Body)
	return UpdateCardLink200JSONResponse(l), err
}

func (h *Handlers) DeleteCardLink(ctx context.Context, r DeleteCardLinkRequestObject) (DeleteCardLinkResponseObject, error) {
	return DeleteCardLink204Response{}, h.S.DeleteCardLink(ctx, r.Id)
}

// ---- recurrences ----

func (h *Handlers) ListRecurrences(ctx context.Context, r ListRecurrencesRequestObject) (ListRecurrencesResponseObject, error) {
	l, err := h.S.ListRecurrences(ctx, r.Id)
	return ListRecurrences200JSONResponse(l), err
}

func (h *Handlers) CreateRecurrence(ctx context.Context, r CreateRecurrenceRequestObject) (CreateRecurrenceResponseObject, error) {
	rec, err := h.S.CreateRecurrence(ctx, r.Id, *r.Body)
	return CreateRecurrence201JSONResponse(rec), err
}

func (h *Handlers) GetRecurrence(ctx context.Context, r GetRecurrenceRequestObject) (GetRecurrenceResponseObject, error) {
	rec, err := h.S.GetRecurrence(ctx, r.Id)
	return GetRecurrence200JSONResponse(rec), err
}

func (h *Handlers) UpdateRecurrence(ctx context.Context, r UpdateRecurrenceRequestObject) (UpdateRecurrenceResponseObject, error) {
	rec, err := h.S.UpdateRecurrence(ctx, r.Id, im(r.Params.IfMatch), *r.Body)
	return UpdateRecurrence200JSONResponse(rec), err
}

func (h *Handlers) DeleteRecurrence(ctx context.Context, r DeleteRecurrenceRequestObject) (DeleteRecurrenceResponseObject, error) {
	return DeleteRecurrence204Response{}, h.S.DeleteRecurrence(ctx, r.Id)
}

func (h *Handlers) RunRecurrence(ctx context.Context, r RunRecurrenceRequestObject) (RunRecurrenceResponseObject, error) {
	c, err := h.S.RunRecurrence(ctx, r.Id)
	return RunRecurrence201JSONResponse(c), err
}

func (h *Handlers) PreviewRecurrence(ctx context.Context, r PreviewRecurrenceRequestObject) (PreviewRecurrenceResponseObject, error) {
	n := 5
	if r.Params.Count != nil {
		n = *r.Params.Count
	}
	p, err := h.S.PreviewRecurrence(ctx, r.Id, n)
	return PreviewRecurrence200JSONResponse(p), err
}

func (h *Handlers) PreviewRule(ctx context.Context, r PreviewRuleRequestObject) (PreviewRuleResponseObject, error) {
	p, err := h.S.PreviewRule(ctx, *r.Body)
	return PreviewRule200JSONResponse(p), err
}

// ---- search ----

func (h *Handlers) Search(ctx context.Context, r SearchRequestObject) (SearchResponseObject, error) {
	res, err := h.S.Search(ctx, r.Params.Q, r.Params.Workspace, r.Params.Board, r.Params.Cursor, r.Params.Limit)
	return Search200JSONResponse(res), err
}
