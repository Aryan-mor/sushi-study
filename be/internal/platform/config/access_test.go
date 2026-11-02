package config

import (
	"slices"
	"testing"
)

func TestLoadAccessMode(t *testing.T) {
	tests := []struct {
		name, raw string
		closed    bool
		problem   bool
	}{
		{name: "unset fails closed", raw: "", closed: true},
		{name: "testing", raw: "testing", closed: true},
		{name: "public", raw: "public", closed: false},
		{name: "case and spaces", raw: " Public ", closed: false},
		{name: "typo is a boot failure", raw: "publik", closed: true, problem: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SUSHI_ACCESS_MODE", tc.raw)
			got, problems := loadAccess()
			if (len(problems) > 0) != tc.problem {
				t.Fatalf("problems = %v, want problem=%v", problems, tc.problem)
			}
			if got.Closed() != tc.closed {
				t.Errorf("Closed() = %v, want %v", got.Closed(), tc.closed)
			}
		})
	}
}

func TestLoadAccessStaffIncludesBothLists(t *testing.T) {
	t.Setenv("SUSHI_ACCESS_MODE", "testing")
	t.Setenv("SUPERADMIN_IDS", "11")
	t.Setenv("ADMIN_IDS", "22,33")
	got, problems := loadAccess()
	if len(problems) > 0 {
		t.Fatalf("problems: %v", problems)
	}
	for _, want := range []int64{11, 22, 33} {
		if !slices.Contains(got.StaffIDs, want) {
			t.Errorf("StaffIDs %v is missing %d", got.StaffIDs, want)
		}
	}
}
