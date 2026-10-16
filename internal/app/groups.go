package app

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// GroupInput is a group as the administrator edits it.
type GroupInput struct {
	Name        string
	Description string
	Permissions []domain.Permission
	MemberIDs   []string
}

// ListGroups returns every group with its permissions and members, Administrators first.
func (s *Service) ListGroups(ctx context.Context) ([]domain.Group, error) {
	gs, err := s.tx.Repos().Groups.List(ctx)
	return gs, wrap("app.ListGroups", err)
}

// CreateGroup adds a group. Like every group and membership change it needs the actor's
// current password and an actor who is still an active administrator (ADR-012).
func (s *Service) CreateGroup(ctx context.Context, in GroupInput, currentPassword string) (domain.Group, error) {
	now := s.now()
	g := domain.Group{ID: s.ids.NewID(), Name: in.Name, Description: in.Description, Permissions: in.Permissions, MemberIDs: in.MemberIDs, CreatedAt: now, UpdatedAt: now}
	if err := g.Normalize(); err != nil {
		return domain.Group{}, wrap("app.CreateGroup", err)
	}
	if err := s.reauthUser(ctx, "creating the group "+g.Name, currentPassword); err != nil {
		return domain.Group{}, wrap("app.CreateGroup", err)
	}
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if _, err := lockAdministrators(ctx, r, nil); err != nil {
			return err
		}
		names, err := userNames(ctx, r, g.MemberIDs)
		if err != nil {
			return err
		}
		if err := r.Groups.Create(ctx, g); err != nil {
			return err
		}
		return s.audit(ctx, r, "group.created", "group", g.ID, g.Name+"; access: "+permList(g.Permissions)+"; members: "+orNone(names))
	})
	return g, wrap("app.CreateGroup", err)
}

// UpdateGroup replaces a group's name, description, permissions and members. For the
// built-in Administrators group only the members change (it always has everything), and
// nobody removes themselves from it.
func (s *Service) UpdateGroup(ctx context.Context, id string, in GroupInput, currentPassword string) (domain.Group, error) {
	g := domain.Group{ID: id, Name: in.Name, Description: in.Description, Permissions: in.Permissions, MemberIDs: in.MemberIDs}
	if id == domain.AdministratorsGroupID {
		g.Name, g.Permissions = "Administrators", nil // only the members are taken
	}
	if err := g.Normalize(); err != nil {
		return domain.Group{}, wrap("app.UpdateGroup", err)
	}
	if err := s.reauthUser(ctx, "changing the group "+g.Name, currentPassword); err != nil {
		return domain.Group{}, wrap("app.UpdateGroup", err)
	}
	var out domain.Group
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		old, err := r.Groups.Lock(ctx, id)
		if err != nil {
			return err
		}
		var removed []string
		for _, m := range old.MemberIDs {
			if !slices.Contains(g.MemberIDs, m) {
				removed = append(removed, m)
			}
		}
		if !old.IsAdministrators() {
			removed = nil
		}
		if _, err := lockAdministrators(ctx, r, removed); err != nil {
			return err
		}
		if old.Builtin {
			g.Name, g.Description, g.Permissions, g.Builtin = old.Name, old.Description, old.Permissions, true
		}
		if _, err := userNames(ctx, r, g.MemberIDs); err != nil {
			return err
		}
		g.CreatedAt, g.UpdatedAt = old.CreatedAt, s.now()
		if err := r.Groups.Update(ctx, g); err != nil {
			return err
		}
		out = g
		summary, err := groupChange(ctx, r, old, g)
		if err != nil {
			return err
		}
		return s.audit(ctx, r, "group.updated", "group", g.ID, summary)
	})
	return out, wrap("app.UpdateGroup", err)
}

// DeleteGroup removes a group that is not built in; its members lose what it gave them.
func (s *Service) DeleteGroup(ctx context.Context, id, currentPassword string) error {
	if id == domain.AdministratorsGroupID {
		return wrap("app.DeleteGroup", domain.ErrForbidden)
	}
	g, err := s.tx.Repos().Groups.Get(ctx, id)
	if err != nil {
		return wrap("app.DeleteGroup", err)
	}
	if err := s.reauthUser(ctx, "deleting the group "+g.Name, currentPassword); err != nil {
		return wrap("app.DeleteGroup", err)
	}
	return wrap("app.DeleteGroup", s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if _, err := lockAdministrators(ctx, r, nil); err != nil {
			return err
		}
		g, err := r.Groups.Lock(ctx, id)
		if err != nil {
			return err
		}
		names, err := userNames(ctx, r, g.MemberIDs)
		if err != nil {
			return err
		}
		if err := r.Groups.Delete(ctx, id); err != nil {
			return err
		}
		return s.audit(ctx, r, "group.deleted", "group", g.ID, g.Name+" deleted; its members lost its access: "+orNone(names))
	}))
}

// SetUserGroups replaces the groups a user is in. Nobody removes themselves from
// Administrators.
func (s *Service) SetUserGroups(ctx context.Context, userID string, groupIDs []string, currentPassword string) (domain.User, error) {
	groupIDs = slices.Compact(slices.Sorted(slices.Values(groupIDs)))
	u, err := s.tx.Repos().Users.Get(ctx, userID)
	if err != nil {
		return domain.User{}, wrap("app.SetUserGroups", err)
	}
	if err := s.reauthUser(ctx, "changing the groups of "+u.Email, currentPassword); err != nil {
		return domain.User{}, wrap("app.SetUserGroups", err)
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		before, err := r.Groups.ForUser(ctx, userID)
		if err != nil {
			return err
		}
		var removed []string
		if slices.ContainsFunc(before, domain.Group.IsAdministrators) && !slices.Contains(groupIDs, domain.AdministratorsGroupID) {
			removed = []string{userID}
		}
		if _, err := lockAdministrators(ctx, r, removed); err != nil {
			return err
		}
		names, err := groupNames(ctx, r, groupIDs)
		if err != nil {
			return err
		}
		if err := r.Groups.SetUserGroups(ctx, userID, groupIDs); err != nil {
			return err
		}
		was := make([]string, 0, len(before))
		for _, g := range before {
			was = append(was, g.ID)
		}
		wasNames, err := groupNames(ctx, r, was)
		if err != nil {
			return err
		}
		u.GroupIDs = groupIDs
		return s.audit(ctx, r, "user.groups_changed", "user", u.ID, u.Email+": groups "+orNone(names)+" (was "+orNone(wasNames)+")")
	})
	return u, wrap("app.SetUserGroups", err)
}

// lockAdministrators locks who administers and refuses an actor who is no longer an
// active administrator, or who would remove themselves from Administrators (removed lists
// the users leaving it). It returns the active administrators.
func lockAdministrators(ctx context.Context, r ports.Repos, removed []string) ([]string, error) {
	admins, err := r.Groups.LockActiveAdministrators(ctx)
	if err != nil {
		return nil, err
	}
	return admins, domain.CheckGroupChange(ActorFrom(ctx).ID, admins, removed)
}

// userNames returns the logins of ids, refusing an unknown user.
func userNames(ctx context.Context, r ports.Repos, ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		u, err := r.Users.Get(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			return nil, &domain.ValidationError{Field: "memberIds", Reason: "unknown user " + id}
		}
		if err != nil {
			return nil, err
		}
		out = append(out, u.Email)
	}
	return out, nil
}

// groupNames returns the names of ids, refusing an unknown group.
func groupNames(ctx context.Context, r ports.Repos, ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		g, err := r.Groups.Get(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			return nil, &domain.ValidationError{Field: "groupIds", Reason: "unknown group " + id}
		}
		if err != nil {
			return nil, err
		}
		out = append(out, g.Name)
	}
	return out, nil
}

// groupChange describes what an update changed, for the audit trail.
func groupChange(ctx context.Context, r ports.Repos, old, g domain.Group) (string, error) {
	parts := []string{g.Name}
	if old.Name != g.Name {
		parts = append(parts, "renamed from "+old.Name)
	}
	if old.Description != g.Description {
		parts = append(parts, "description changed")
	}
	added, removed := diff(old.Permissions, g.Permissions)
	if len(added) > 0 {
		parts = append(parts, "access added: "+permList(added))
	}
	if len(removed) > 0 {
		parts = append(parts, "access removed: "+permList(removed))
	}
	joined, left := diff(old.MemberIDs, g.MemberIDs)
	if len(joined) > 0 {
		names, err := userNames(ctx, r, joined)
		if err != nil {
			return "", err
		}
		parts = append(parts, "members added: "+strings.Join(names, ", "))
	}
	if len(left) > 0 {
		names, err := userNames(ctx, r, left)
		if err != nil {
			return "", err
		}
		parts = append(parts, "members removed: "+strings.Join(names, ", "))
	}
	if len(parts) == 1 {
		parts = append(parts, "no change")
	}
	return strings.Join(parts, "; "), nil
}

func diff[T comparable](before, after []T) (added, removed []T) {
	for _, x := range after {
		if !slices.Contains(before, x) {
			added = append(added, x)
		}
	}
	for _, x := range before {
		if !slices.Contains(after, x) {
			removed = append(removed, x)
		}
	}
	return added, removed
}

func permList(ps []domain.Permission) string {
	ss := make([]string, len(ps))
	for i, p := range ps {
		ss[i] = string(p)
	}
	return orNone(ss)
}

func orNone(ss []string) string {
	if len(ss) == 0 {
		return "none"
	}
	return strings.Join(ss, ", ")
}
