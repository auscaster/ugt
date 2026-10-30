package keychain

import (
	"context"
	"os"
	"testing"
)

// Contract: a secret goes in, comes back exactly, and goes away, in this
// system's real store. It writes to the user's keychain, so it runs only
// with UGT_KEYCHAIN_TEST=1, and removes what it wrote.
func TestRealStore(t *testing.T) {
	if os.Getenv("UGT_KEYCHAIN_TEST") != "1" {
		t.Skip("set UGT_KEYCHAIN_TEST=1 to write to this machine's keychain")
	}
	k, ctx := Keychain{}, context.Background()
	const recipe, name = "ugt-contract-test/keychain/roundtrip", "TEST_SECRET"
	value := `sk-test "quoted" \ back\slash and spaces`
	t.Cleanup(func() { _ = k.Delete(ctx, recipe, name) })
	if err := k.Set(ctx, recipe, name, value); err != nil {
		t.Fatal(err)
	}
	got, ok, err := k.Get(ctx, recipe, name)
	if err != nil || !ok || got != value {
		t.Fatalf("got %q, %v, %v", got, ok, err)
	}
	if err := k.Set(ctx, recipe, name, "replaced"); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := k.Get(ctx, recipe, name); got != "replaced" {
		t.Errorf("replace: %q", got)
	}
	if err := k.Delete(ctx, recipe, name); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := k.Get(ctx, recipe, name); ok || err != nil {
		t.Errorf("after delete: %v, %v", ok, err)
	}
}
