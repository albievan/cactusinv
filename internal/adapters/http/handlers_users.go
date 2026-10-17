package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// ---- users (ADR-011) ----

func toAPIUser(u domain.User) User {
	out := User{Id: u.ID, Email: u.Email, CreatedAt: u.CreatedAt, Active: u.Active(), MustChangePassword: u.MustChangePassword, GroupIds: u.GroupIDs}
	if out.GroupIds == nil {
		out.GroupIds = []string{}
	}
	if !u.DisabledAt.IsZero() {
		t := u.DisabledAt
		out.DisabledAt = &t
	}
	if !u.LastLoginAt.IsZero() {
		t := u.LastLoginAt
		out.LastLoginAt = &t
	}
	return out
}

func (h *handlers) ListUsers(ctx context.Context, _ ListUsersRequestObject) (ListUsersResponseObject, error) {
	us, err := h.svc.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]User, len(us))
	for i, u := range us {
		out[i] = toAPIUser(u)
	}
	return ListUsers200JSONResponse{Items: out}, nil
}

func (h *handlers) AddUser(ctx context.Context, req AddUserRequestObject) (AddUserResponseObject, error) {
	u, err := h.svc.AddUser(ctx, req.Body.Email, req.Body.Password, req.Body.CurrentPassword, req.Body.GroupIds)
	if err != nil {
		return nil, err
	}
	return AddUser201JSONResponse(toAPIUser(u)), nil
}

func (h *handlers) ResetUserPassword(ctx context.Context, req ResetUserPasswordRequestObject) (ResetUserPasswordResponseObject, error) {
	u, err := h.svc.ResetUserPassword(ctx, req.Id, req.Body.Password, req.Body.CurrentPassword)
	if err != nil {
		return nil, err
	}
	return ResetUserPassword200JSONResponse(toAPIUser(u)), nil
}

func (h *handlers) DisableUser(ctx context.Context, req DisableUserRequestObject) (DisableUserResponseObject, error) {
	u, err := h.svc.DisableUser(ctx, req.Id, req.Body.CurrentPassword)
	if err != nil {
		return nil, err
	}
	return DisableUser200JSONResponse(toAPIUser(u)), nil
}

func (h *handlers) EnableUser(ctx context.Context, req EnableUserRequestObject) (EnableUserResponseObject, error) {
	u, err := h.svc.EnableUser(ctx, req.Id, req.Body.CurrentPassword)
	if err != nil {
		return nil, err
	}
	return EnableUser200JSONResponse(toAPIUser(u)), nil
}

func (h *handlers) SetUserGroups(ctx context.Context, req SetUserGroupsRequestObject) (SetUserGroupsResponseObject, error) {
	u, err := h.svc.SetUserGroups(ctx, req.Id, req.Body.GroupIds, req.Body.CurrentPassword)
	if err != nil {
		return nil, err
	}
	return SetUserGroups200JSONResponse(toAPIUser(u)), nil
}

// ---- access groups (ADR-012) ----

func toAPIGroup(g domain.Group) Group {
	out := Group{
		Id: g.ID, Name: g.Name, Description: g.Description, Builtin: g.Builtin, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
		Permissions: make([]string, len(g.Permissions)), MemberIds: append([]string{}, g.MemberIDs...),
	}
	for i, p := range g.Permissions {
		out.Permissions[i] = string(p)
	}
	return out
}

func groupInput(b GroupRequest) app.GroupInput {
	in := app.GroupInput{Name: b.Name, Description: b.Description, MemberIDs: b.MemberIds, Permissions: make([]domain.Permission, len(b.Permissions))}
	for i, p := range b.Permissions {
		in.Permissions[i] = domain.Permission(p)
	}
	return in
}

func (h *handlers) ListGroups(ctx context.Context, _ ListGroupsRequestObject) (ListGroupsResponseObject, error) {
	gs, err := h.svc.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	out := ListGroups200JSONResponse{Items: make([]Group, len(gs)), Permissions: make([]PermissionInfo, len(domain.Permissions))}
	for i, g := range gs {
		out.Items[i] = toAPIGroup(g)
	}
	for i, p := range domain.Permissions {
		info := PermissionInfo{Key: string(p.Key), Area: p.Area, Label: p.Label, Implies: make([]string, len(p.Implies))}
		for j, q := range p.Implies {
			info.Implies[j] = string(q)
		}
		out.Permissions[i] = info
	}
	return out, nil
}

func (h *handlers) CreateGroup(ctx context.Context, req CreateGroupRequestObject) (CreateGroupResponseObject, error) {
	g, err := h.svc.CreateGroup(ctx, groupInput(*req.Body), req.Body.CurrentPassword)
	if err != nil {
		return nil, err
	}
	return CreateGroup201JSONResponse(toAPIGroup(g)), nil
}

func (h *handlers) UpdateGroup(ctx context.Context, req UpdateGroupRequestObject) (UpdateGroupResponseObject, error) {
	g, err := h.svc.UpdateGroup(ctx, req.Id, groupInput(*req.Body), req.Body.CurrentPassword)
	if err != nil {
		return nil, err
	}
	return UpdateGroup200JSONResponse(toAPIGroup(g)), nil
}

func (h *handlers) DeleteGroup(ctx context.Context, req DeleteGroupRequestObject) (DeleteGroupResponseObject, error) {
	if err := h.svc.DeleteGroup(ctx, req.Id, req.Body.CurrentPassword); err != nil {
		return nil, err
	}
	return DeleteGroup204Response{}, nil
}

// sessionGate refuses every operation a signed-in user's session may not run: while the
// user must change their password, everything but me, changePassword and logout, and
// otherwise what their access groups do not allow (app.SessionAllows). It is a strict-server middleware because only there is the
// operation known; it runs before the handler reads an upload or export body.
func sessionGate(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		if u := currentUser(ctx); u.ID != "" {
			if err := app.SessionAllows(u, operationID); err != nil {
				return nil, err
			}
		}
		return f(ctx, w, r, request)
	}
}

// reauthPaths are the routes that check a password and so share the sign-in rate limit
// and are never fingerprinted for Idempotency-Key (an unsalted hash of a password could be
// guessed).
func reauthPath(r *http.Request) bool {
	p := r.URL.Path
	if r.Method == http.MethodPut {
		// Changing a group, or a user's groups (ADR-012). Every PUT under /groups/ counts:
		// the decoded path of an id with an escaped slash (x%2Fy) has more segments than the
		// route, which must not slip past the limit.
		return strings.HasPrefix(p, "/api/v1/groups/") ||
			(strings.HasPrefix(p, "/api/v1/users/") && strings.HasSuffix(p, "/groups"))
	}
	if r.Method != http.MethodPost {
		return false
	}
	if p == "/api/v1/groups" || (strings.HasPrefix(p, "/api/v1/groups/") && strings.HasSuffix(p, "/delete")) {
		return true
	}
	if p == "/api/v1/auth/login" || p == "/api/v1/auth/password" || p == "/api/v1/users" {
		return true
	}
	if rest, ok := strings.CutPrefix(p, "/api/v1/users/"); ok {
		return strings.HasSuffix(rest, "/reset-password") || strings.HasSuffix(rest, "/disable") || strings.HasSuffix(rest, "/enable")
	}
	return strings.HasPrefix(p, "/api/v1/restores/") && strings.HasSuffix(p, "/apply")
}
