package config

import (
	"fmt"
	"os"
	"strings"
)

// AccessMode is whether strangers may register. Set by SUSHI_ACCESS_MODE (ADR 0039).
type AccessMode string

const (
	// AccessTesting refuses anyone without an account or an admin's authorization, on every bot
	// and in the app. It is what an unset variable means: the failure of forgetting it must be a
	// closed door, not an open one.
	AccessTesting AccessMode = "testing"
	// AccessPublic lets anyone register.
	AccessPublic AccessMode = "public"
)

// Access is the registration policy every user-facing process applies the same way.
type Access struct {
	Mode AccessMode
	// StaffIDs always pass: a fresh deployment's admins have to reach /admin to authorize anyone.
	StaffIDs []int64
}

// Closed reports whether the testing-mode gate applies.
func (a Access) Closed() bool { return a.Mode != AccessPublic }

// appendAccess is loadAccess in the shape every Load* builds its problem list.
func appendAccess(problems []string) (Access, []string) {
	access, accessProblems := loadAccess()
	return access, append(problems, accessProblems...)
}

func loadAccess() (Access, []string) {
	mode := AccessMode(strings.ToLower(strings.TrimSpace(os.Getenv("SUSHI_ACCESS_MODE"))))
	if mode == "" {
		mode = AccessTesting
	}
	if mode != AccessTesting && mode != AccessPublic {
		return Access{Mode: AccessTesting}, []string{
			fmt.Sprintf("SUSHI_ACCESS_MODE %q is not public or testing", mode),
		}
	}
	staff := intList(os.Getenv("SUPERADMIN_IDS"))
	staff = append(staff, intList(os.Getenv("ADMIN_IDS"))...)
	return Access{Mode: mode, StaffIDs: staff}, nil
}
