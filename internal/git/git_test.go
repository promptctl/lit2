package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func locate(t *testing.T, dir string, want Checkout) {
	t.Helper()
	got, err := Locate(dir)
	if err != nil || got != want {
		t.Errorf("Locate(%s) = %+v, %v; want %+v", dir, got, err, want)
	}
}

func TestLocateFindsAMainWorktree(t *testing.T) {
	main := t.TempDir()
	mkdir(t, filepath.Join(main, ".git"))
	locate(t, main, Checkout{WorkTree: main, GitDir: filepath.Join(main, ".git"), CommonDir: filepath.Join(main, ".git")})
}

func TestLocateFindsALinkedWorktreeAndItsCommonDir(t *testing.T) {
	root := t.TempDir()
	main, linked := filepath.Join(root, "main"), filepath.Join(root, "linked")
	gitDir := filepath.Join(main, ".git", "worktrees", "linked")
	// as git worktree add writes them
	write(t, filepath.Join(gitDir, "commondir"), "../..\n")
	write(t, filepath.Join(linked, ".git"), "gitdir: "+gitDir+"\n")
	locate(t, linked, Checkout{WorkTree: linked, GitDir: gitDir, CommonDir: filepath.Join(main, ".git")})
}

func TestLocateReadsAGitFileWithNoCommondir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	mkdir(t, filepath.Join(root, ".git", "modules", "sub"))
	write(t, filepath.Join(sub, ".git"), "gitdir: ../.git/modules/sub\n")
	gitDir := filepath.Join(root, ".git", "modules", "sub")
	locate(t, sub, Checkout{WorkTree: sub, GitDir: gitDir, CommonDir: gitDir})
}

func TestLocateWalksUpFromANestedDirectory(t *testing.T) {
	main := t.TempDir()
	mkdir(t, filepath.Join(main, ".git"))
	nested := filepath.Join(main, "a", "b")
	mkdir(t, nested)
	locate(t, nested, Checkout{WorkTree: main, GitDir: filepath.Join(main, ".git"), CommonDir: filepath.Join(main, ".git")})
}

func TestLocateWithNoGitIsNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := Locate(dir)
	var none NotFound
	if !errors.As(err, &none) || none.Dir != dir {
		t.Errorf("Locate(%s) failed with %v; want NotFound for it", dir, err)
	}
}

func TestLocateStopsAtAGitFileThatNamesNoGitDir(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".git"), "not a gitdir line\n")
	_, err := Locate(dir)
	var none NotFound
	if err == nil || errors.As(err, &none) {
		t.Errorf("Locate(%s) failed with %v; want the error that stopped the walk", dir, err)
	}
}
