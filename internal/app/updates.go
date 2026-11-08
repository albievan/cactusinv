package app

import (
	"context"

	"github.com/albievan/cactusinv/internal/domain"
)

// UpdateCheck answers "is there a newer version?" (System Settings; M27). The app cannot
// replace itself (it has no Docker or cluster rights): an upgrade is ./deploy.sh --upgrade.
type UpdateCheck struct {
	// Current is the running version; Latest the newest published ("" when not read).
	Current, Latest string
	Available       bool
	// Problem says why the check could not be made ("" when it was).
	Problem string
}

// CheckForUpdate compares the running version with the newest published one. It only runs
// when asked: nothing is fetched in the background.
func (s *Service) CheckForUpdate(ctx context.Context) UpdateCheck {
	out := UpdateCheck{Current: s.version}
	if s.releases == nil {
		out.Problem = "Checking for updates is switched off on this installation (CACTUSINV_UPDATE_URL=off)."
		return out
	}
	cur, err := domain.ParseVersion(s.version)
	if err != nil {
		out.Problem = "This is a development build (" + s.version + "), which cannot be compared with a release."
		return out
	}
	published, err := s.releases.LatestVersion(ctx)
	if err != nil {
		if s.log != nil {
			s.log.WarnContext(ctx, "update check failed", "err", err)
		}
		out.Problem = "The newest version could not be read. Check that the server can reach the internet, then try again."
		return out
	}
	latest, err := domain.ParseVersion(published)
	if err != nil {
		out.Problem = "The published version could not be understood."
		return out
	}
	out.Latest, out.Available = latest.String(), cur.Less(latest)
	return out
}
