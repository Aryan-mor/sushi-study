package llm

import (
	"strconv"
	"testing"
)

func TestParseServiceAccountDotenvEscapes(t *testing.T) {
	t.Parallel()
	raw := `{"client_email":"bot@proj.iam.gserviceaccount.com","private_key":"-----BEGIN PRIVATE KEY-----\nMII\n-----END PRIVATE KEY-----\n","token_uri":"https://oauth2.googleapis.com/token"}`
	escaped := strconv.Quote(raw)
	inner := escaped[1 : len(escaped)-1]

	for i, in := range []string{raw, escaped, inner} {
		sa, err := parseServiceAccount(in)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if sa.ClientEmail != "bot@proj.iam.gserviceaccount.com" {
			t.Fatalf("case %d email = %q", i, sa.ClientEmail)
		}
		if sa.PrivateKey == "" {
			t.Fatalf("case %d missing private_key", i)
		}
	}
}
