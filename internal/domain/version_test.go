package domain_test

import (
	"errors"
	"testing"

	"github.com/albievan/cactusinv/internal/domain"
)

func TestParseVersion(t *testing.T) {
	for _, c := range []struct {
		in   string
		want domain.Version
	}{{"1.13.0", domain.Version{1, 13, 0}}, {"v2.0.10", domain.Version{2, 0, 10}}, {" 1.2.3\n", domain.Version{1, 2, 3}}} {
		got, err := domain.ParseVersion(c.in)
		if err != nil || got != c.want || got.String() != c.want.String() {
			t.Errorf("%q: %v %v", c.in, got, err)
		}
	}
	for _, bad := range []string{"", "dev", "1.2", "1.2.3.4", "1.2.x", "1.02.3", "1.-2.3", "1.2.3-rc1", "+1.2.3"} {
		if _, err := domain.ParseVersion(bad); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestVersionLess(t *testing.T) {
	order := []domain.Version{{0, 9, 9}, {1, 0, 0}, {1, 9, 0}, {1, 10, 0}, {1, 10, 2}, {2, 0, 0}}
	for i := range order {
		for j := range order {
			if got := order[i].Less(order[j]); got != (i < j) {
				t.Errorf("%v < %v = %v", order[i], order[j], got)
			}
		}
	}
}
