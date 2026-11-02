package catalog

import "testing"

func TestJoinNetworksPrefersTVNetworks(t *testing.T) {
	t.Parallel()
	got := JoinNetworks(
		[]byte(`[{"name":"Netflix"},{"name":"Netflix"},{"name":" Disney+ "},{"name":"Hulu"},{"name":"Max"}]`),
		[]byte(`[{"name":"Warner Bros. Pictures"}]`),
	)
	if got != "Netflix, Disney+, Hulu" {
		t.Fatalf("got %q", got)
	}
}

func TestJoinNetworksFallsBackToStreamerCompany(t *testing.T) {
	t.Parallel()
	got := JoinNetworks(nil, []byte(`[{"name":"Warner Bros. Pictures"},{"name":"Walt Disney Pictures"}]`))
	if got != "Walt Disney Pictures" {
		t.Fatalf("got %q", got)
	}
	if JoinNetworks(nil, []byte(`[{"name":"Warner Bros. Pictures"}]`)) != "" {
		t.Fatal("a theatrical studio is not a network")
	}
}
