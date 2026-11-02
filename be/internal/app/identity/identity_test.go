package identity_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

// roleCall records one SetUserRole invocation.
type roleCall struct {
	tg   int64
	role string
}

// fakeStore is the smallest identity.Store that exercises role sync. Everything unrelated to roles
// returns a zero value — these tests never call those paths.
type fakeStore struct {
	user      identity.User
	ensureErr error

	setRoleCalls []roleCall
	setRoleErr   error

	syncSuper []int64
	syncAdmin []int64
	syncN     int64
	syncErr   error
}

func (f *fakeStore) EnsureUser(_ context.Context, tgUserID int64, _, _ string) (identity.User, error) {
	if f.ensureErr != nil {
		return identity.User{}, f.ensureErr
	}
	u := f.user
	u.TGUserID = tgUserID
	return u, nil
}

func (f *fakeStore) SetUserRole(_ context.Context, tgUserID int64, role string) error {
	f.setRoleCalls = append(f.setRoleCalls, roleCall{tg: tgUserID, role: role})
	return f.setRoleErr
}

func (f *fakeStore) SyncUserRoles(_ context.Context, superadminIDs, adminIDs []int64) (int64, error) {
	f.syncSuper, f.syncAdmin = superadminIDs, adminIDs
	return f.syncN, f.syncErr
}

func (f *fakeStore) APIBotPool(context.Context) ([]string, error)   { return []string{"x_api_1"}, nil }
func (f *fakeStore) SupersedeBindings(context.Context, int64) error { return nil }

func (f *fakeStore) CreateBinding(context.Context, identity.NewBinding) (identity.Binding, error) {
	return identity.Binding{}, nil
}

func (f *fakeStore) BindingByChat(context.Context, int64) (identity.Binding, error) {
	return identity.Binding{}, nil
}

func (f *fakeStore) TombstoneUser(context.Context, int64) error { return nil }
func (f *fakeStore) LiftTombstone(context.Context, int64) error { return nil }

type roleCase struct {
	name           string
	super, admin   []int64
	stored         string
	tg             int64
	wantRole       string
	wantCorrection bool
}

func (tc roleCase) run(t *testing.T) {
	t.Helper()

	store := &fakeStore{user: identity.User{ID: 1, Role: tc.stored}}
	svc := identity.New(store).WithRoleSync(tc.super, tc.admin)

	u, err := svc.EnsureUser(context.Background(), tc.tg, "", "")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	if u.Role != tc.wantRole {
		t.Errorf("returned role = %q, want %q", u.Role, tc.wantRole)
	}

	wrote := len(store.setRoleCalls) > 0
	if wrote != tc.wantCorrection {
		t.Fatalf("SetUserRole written = %v, want %v", wrote, tc.wantCorrection)
	}
	if wrote {
		got := store.setRoleCalls[0]
		if got.tg != tc.tg || got.role != tc.wantRole {
			t.Errorf("SetUserRole(%d, %q), want (%d, %q)", got.tg, got.role, tc.tg, tc.wantRole)
		}
	}
}

func TestEnsureUserCorrectsRoleFromLists(t *testing.T) {
	t.Parallel()

	cases := []roleCase{
		{"promote to superadmin", []int64{42}, nil, identity.RoleUser, 42, identity.RoleSuperadmin, true},
		{"promote to admin", nil, []int64{7}, identity.RoleUser, 7, identity.RoleAdmin, true},
		{"superadmin list wins over admin list", []int64{9}, []int64{9}, identity.RoleUser, 9, identity.RoleSuperadmin, true},
		{"demote a stray admin not in any list", nil, nil, identity.RoleAdmin, 5, identity.RoleUser, true},
		{"no write when already correct", []int64{1}, nil, identity.RoleSuperadmin, 1, identity.RoleSuperadmin, false},
		{"plain user stays a user, no write", nil, nil, identity.RoleUser, 3, identity.RoleUser, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// Without WithRoleSync (apid, init-bot) the column is never touched, whatever it holds.
func TestEnsureUserLeavesRoleAloneWithoutRoleSync(t *testing.T) {
	t.Parallel()

	store := &fakeStore{user: identity.User{ID: 1, Role: identity.RoleAdmin}}
	u, err := identity.New(store).EnsureUser(context.Background(), 99, "", "")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	if u.Role != identity.RoleAdmin {
		t.Errorf("role = %q, want it untouched", u.Role)
	}
	if len(store.setRoleCalls) != 0 {
		t.Errorf("SetUserRole called %d times without WithRoleSync", len(store.setRoleCalls))
	}
}

// A failed correction must not fail the person's message; the local view stays at the stored role.
func TestEnsureUserSurvivesARoleCorrectionFailure(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		user:       identity.User{ID: 1, Role: identity.RoleUser},
		setRoleErr: errors.New("db blip"),
	}
	svc := identity.New(store).WithRoleSync([]int64{5}, nil)

	u, err := svc.EnsureUser(context.Background(), 5, "", "")
	if err != nil {
		t.Fatalf("EnsureUser should not fail on a role blip: %v", err)
	}
	if u.Role != identity.RoleUser {
		t.Errorf("role = %q, want the un-updated %q", u.Role, identity.RoleUser)
	}
}

// A store error from EnsureUser itself is still surfaced.
func TestEnsureUserSurfacesStoreErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("users table gone")
	store := &fakeStore{ensureErr: want}
	_, err := identity.New(store).WithRoleSync(nil, nil).EnsureUser(context.Background(), 1, "", "")
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want it to wrap %v", err, want)
	}
}

func TestSyncRolesPassesTheListsThrough(t *testing.T) {
	t.Parallel()

	store := &fakeStore{syncN: 3}
	svc := identity.New(store).WithRoleSync([]int64{1, 2}, []int64{3})

	n, err := svc.SyncRoles(context.Background())
	if err != nil {
		t.Fatalf("SyncRoles: %v", err)
	}
	if n != 3 {
		t.Errorf("changed = %d, want 3", n)
	}
	if !slices.Equal(store.syncSuper, []int64{1, 2}) || !slices.Equal(store.syncAdmin, []int64{3}) {
		t.Errorf("passed lists = %v / %v, want [1 2] / [3]", store.syncSuper, store.syncAdmin)
	}
}

func TestSyncRolesIsANoopWithoutRoleSync(t *testing.T) {
	t.Parallel()

	store := &fakeStore{syncN: 99, syncErr: errors.New("should not be called")}
	n, err := identity.New(store).SyncRoles(context.Background())
	if err != nil || n != 0 {
		t.Errorf("SyncRoles() = (%d, %v), want (0, nil) when role sync is off", n, err)
	}
}

func TestTombstoneRefusesStaffOnTheLists(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	err := identity.New(store).WithRoleSync([]int64{9}, nil).Tombstone(context.Background(), 9)
	if !errors.Is(err, identity.ErrProtected) {
		t.Errorf("err = %v, want ErrProtected", err)
	}
}

func TestTombstonePassesThroughForOrdinaryUsers(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	if err := identity.New(store).WithRoleSync(nil, nil).Tombstone(context.Background(), 3); err != nil {
		t.Errorf("Tombstone: %v", err)
	}
}

func TestEnsureUserSurfacesCreatedFromTheStore(t *testing.T) {
	t.Parallel()

	store := &fakeStore{user: identity.User{ID: 4, Created: true, Name: "Ada"}}
	u, err := identity.New(store).EnsureUser(context.Background(), 11, "Ada", "")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	if !u.Created || u.Name != "Ada" || u.TGUserID != 11 {
		t.Fatalf("user = %+v, want Created with name Ada and tg 11", u)
	}
}

func TestEnsureUserReturnsADeletedMarkerWithoutRoleSync(t *testing.T) {
	t.Parallel()

	store := &fakeStore{user: identity.User{TGUserID: 8, Deleted: true}}
	u, err := identity.New(store).EnsureUser(context.Background(), 8, "", "")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	if !u.Deleted {
		t.Fatal("want Deleted")
	}
	if len(store.setRoleCalls) != 0 {
		t.Error("must not SetUserRole on a tombstoned id")
	}
}
