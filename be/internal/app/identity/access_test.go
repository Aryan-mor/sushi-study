package identity_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

type fakeRegistration struct {
	allowed bool
	err     error
	calls   int
}

func (f *fakeRegistration) RegistrationAllowed(context.Context, int64) (bool, error) {
	f.calls++
	return f.allowed, f.err
}

// countingStore proves a refused person never reaches the insert.
type countingStore struct {
	fakeStore
	ensured int
}

func (s *countingStore) EnsureUser(ctx context.Context, tg int64, name, username string) (identity.User, error) {
	s.ensured++
	return s.fakeStore.EnsureUser(ctx, tg, name, username)
}

func TestClosedRegistrationRefusesAStrangerWithoutCreatingARow(t *testing.T) {
	t.Parallel()
	store := &countingStore{}
	svc := identity.New(store).WithClosedRegistration(&fakeRegistration{}, nil)

	if _, err := svc.EnsureUser(context.Background(), 7, "n", "u"); !errors.Is(err, identity.ErrNotAuthorized) {
		t.Fatalf("EnsureUser err = %v, want ErrNotAuthorized", err)
	}
	if _, err := svc.Bind(context.Background(), 7, 7, ""); !errors.Is(err, identity.ErrNotAuthorized) {
		t.Fatalf("Bind err = %v, want ErrNotAuthorized", err)
	}
	if store.ensured != 0 {
		t.Errorf("store.EnsureUser ran %d times for a refused person", store.ensured)
	}
}

func TestClosedRegistrationAdmitsAccountsAndInvites(t *testing.T) {
	t.Parallel()
	store := &countingStore{}
	svc := identity.New(store).WithClosedRegistration(&fakeRegistration{allowed: true}, nil)

	if _, err := svc.EnsureUser(context.Background(), 7, "n", "u"); err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	if store.ensured != 1 {
		t.Errorf("store.EnsureUser ran %d times, want 1", store.ensured)
	}
}

func TestStaffPassWithoutAStoreLookup(t *testing.T) {
	t.Parallel()
	reg := &fakeRegistration{}
	svc := identity.New(&countingStore{}).WithClosedRegistration(reg, []int64{42})

	ok, err := svc.Authorized(context.Background(), 42)
	if err != nil || !ok {
		t.Fatalf("Authorized(staff) = %v, %v", ok, err)
	}
	if reg.calls != 0 {
		t.Errorf("staff check hit the store %d times", reg.calls)
	}
}

func TestOpenRegistrationNeverAsks(t *testing.T) {
	t.Parallel()
	ok, err := identity.New(&countingStore{}).Authorized(context.Background(), 7)
	if err != nil || !ok {
		t.Fatalf("Authorized = %v, %v", ok, err)
	}
}

func TestGateFailsClosedWhenTheStoreFails(t *testing.T) {
	t.Parallel()
	boom := errors.New("db down")
	svc := identity.New(&countingStore{}).WithClosedRegistration(&fakeRegistration{allowed: true, err: boom}, nil)

	if _, err := svc.EnsureUser(context.Background(), 7, "", ""); !errors.Is(err, boom) {
		t.Fatalf("EnsureUser err = %v, want the store failure", err)
	}
}

func TestNotPublicTextCarriesTheIDAnAdminNeeds(t *testing.T) {
	t.Parallel()
	for _, lang := range []string{"en", "fa", "fa-IR", ""} {
		if got := identity.NotPublicText(lang, 12345); !strings.Contains(got, "12345") {
			t.Errorf("NotPublicText(%q) omits the id: %q", lang, got)
		}
	}
}
