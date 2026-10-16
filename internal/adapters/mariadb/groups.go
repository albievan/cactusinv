package mariadb

import (
	"context"

	"github.com/albievan/cactusinv/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/cactusinv/internal/domain"
)

type groupRepo struct{ q *sqlcgen.Queries }

func toGroup(g sqlcgen.AccessGroup) domain.Group {
	return domain.Group{
		ID: g.ID, Name: g.Name, Description: g.Description, Builtin: g.Builtin,
		CreatedAt: g.CreatedAt.UTC(), UpdatedAt: g.UpdatedAt.UTC(),
	}
}

// fill adds every group's permissions and members (two queries for any number of groups).
func (r groupRepo) fill(ctx context.Context, gs []domain.Group) error {
	perms, err := r.q.ListAccessGroupPermissions(ctx)
	if err != nil {
		return mapErr(err)
	}
	members, err := r.q.ListAccessGroupMembers(ctx)
	if err != nil {
		return mapErr(err)
	}
	byID := make(map[string]*domain.Group, len(gs))
	for i := range gs {
		gs[i].Permissions, gs[i].MemberIDs = []domain.Permission{}, []string{}
		byID[gs[i].ID] = &gs[i]
	}
	for _, p := range perms {
		if g := byID[p.GroupID]; g != nil {
			g.Permissions = append(g.Permissions, domain.Permission(p.Permission))
		}
	}
	for _, m := range members {
		if g := byID[m.GroupID]; g != nil {
			g.MemberIDs = append(g.MemberIDs, m.UserID)
		}
	}
	return nil
}

func (r groupRepo) List(ctx context.Context) ([]domain.Group, error) {
	rows, err := r.q.ListAccessGroups(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.Group, len(rows))
	for i, g := range rows {
		out[i] = toGroup(g)
	}
	return out, r.fill(ctx, out)
}

func (r groupRepo) one(ctx context.Context, g sqlcgen.AccessGroup, err error) (domain.Group, error) {
	if err != nil {
		return domain.Group{}, mapErr(err)
	}
	out := []domain.Group{toGroup(g)}
	if err := r.fill(ctx, out); err != nil {
		return domain.Group{}, err
	}
	return out[0], nil
}

func (r groupRepo) Get(ctx context.Context, id string) (domain.Group, error) {
	g, err := r.q.GetAccessGroup(ctx, id)
	return r.one(ctx, g, err)
}

func (r groupRepo) Lock(ctx context.Context, id string) (domain.Group, error) {
	g, err := r.q.LockAccessGroup(ctx, id)
	return r.one(ctx, g, err)
}

func (r groupRepo) Create(ctx context.Context, g domain.Group) error {
	err := r.q.InsertAccessGroup(ctx, sqlcgen.InsertAccessGroupParams{
		ID: g.ID, Name: g.Name, Description: g.Description, Builtin: g.Builtin, CreatedAt: g.CreatedAt.UTC(), UpdatedAt: g.UpdatedAt.UTC(),
	})
	if err != nil {
		return mapErr(err)
	}
	if err := r.setPermissions(ctx, g.ID, g.Permissions); err != nil {
		return err
	}
	return r.setMembers(ctx, g.ID, g.MemberIDs)
}

func (r groupRepo) Update(ctx context.Context, g domain.Group) error {
	if !g.Builtin {
		n, err := r.q.UpdateAccessGroup(ctx, sqlcgen.UpdateAccessGroupParams{Name: g.Name, Description: g.Description, UpdatedAt: g.UpdatedAt.UTC(), ID: g.ID})
		if err := affected(n, err, domain.ErrNotFound); err != nil {
			return err
		}
		if err := r.setPermissions(ctx, g.ID, g.Permissions); err != nil {
			return err
		}
	}
	return r.setMembers(ctx, g.ID, g.MemberIDs)
}

func (r groupRepo) setPermissions(ctx context.Context, id string, ps []domain.Permission) error {
	if err := r.q.DeleteAccessGroupPermissions(ctx, id); err != nil {
		return mapErr(err)
	}
	for _, p := range ps {
		if err := r.q.InsertAccessGroupPermission(ctx, sqlcgen.InsertAccessGroupPermissionParams{GroupID: id, Permission: string(p)}); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

func (r groupRepo) setMembers(ctx context.Context, id string, userIDs []string) error {
	if err := r.q.DeleteAccessGroupMembers(ctx, id); err != nil {
		return mapErr(err)
	}
	for _, u := range userIDs {
		if err := r.q.InsertAccessGroupMember(ctx, sqlcgen.InsertAccessGroupMemberParams{GroupID: id, UserID: u}); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

func (r groupRepo) Delete(ctx context.Context, id string) error {
	n, err := r.q.DeleteAccessGroup(ctx, id)
	return affected(n, err, domain.ErrNotFound)
}

func (r groupRepo) ForUser(ctx context.Context, userID string) ([]domain.Group, error) {
	rows, err := r.q.ListUserGroupPermissions(ctx, userID)
	if err != nil {
		return nil, mapErr(err)
	}
	var out []domain.Group
	for _, row := range rows {
		if len(out) == 0 || out[len(out)-1].ID != row.GroupID {
			out = append(out, domain.Group{ID: row.GroupID, Builtin: row.GroupID == domain.AdministratorsGroupID, Permissions: []domain.Permission{}})
		}
		if row.Permission.Valid {
			g := &out[len(out)-1]
			g.Permissions = append(g.Permissions, domain.Permission(row.Permission.String))
		}
	}
	return out, nil
}

func (r groupRepo) SetUserGroups(ctx context.Context, userID string, groupIDs []string) error {
	if err := r.q.DeleteUserAccessGroups(ctx, userID); err != nil {
		return mapErr(err)
	}
	for _, g := range groupIDs {
		err := r.q.InsertAccessGroupMember(ctx, sqlcgen.InsertAccessGroupMemberParams{GroupID: g, UserID: userID})
		if err != nil {
			return mapErr(err)
		}
	}
	return nil
}

func (r groupRepo) LockActiveAdministrators(ctx context.Context) ([]string, error) {
	ids, err := r.q.LockActiveAdministrators(ctx)
	return ids, mapErr(err)
}
