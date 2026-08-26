package rush

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostDirectoriesArePrivateAndCommandScoped(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	first, err := createHostDirectory()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(first)
	second, err := createHostDirectory()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(second)
	if first == second {
		t.Fatalf("host directory was reused: %s", first)
	}
	wantParent := filepath.Join(cache, "rush")
	if filepath.Dir(first) != wantParent || filepath.Dir(second) != wantParent {
		t.Fatalf("host directories = %q and %q; want parent %q", first, second, wantParent)
	}
	info, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("host directory permissions = %o", info.Mode().Perm())
	}
}
