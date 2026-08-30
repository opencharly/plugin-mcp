package mcp

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// chdir moves into dir for the duration of the test (computeProjectPrefix stats the cwd).
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

// TestComputeProjectPrefix_HonoursCharlyProjectDir is the regression guard for the
// container-venue failure: pod-charly-mcp sets CHARLY_PROJECT_DIR=/workspace and puts the
// project there, but supervisord starts the service with a DIFFERENT cwd. Statting only the
// cwd therefore missed the project and fell through to ["--repo", "default"], which makes the
// child charly run `git ls-remote` against github.com from inside the venue — a hard failure
// in any pod without DNS, for a project sitting on local disk.
func TestComputeProjectPrefix_HonoursCharlyProjectDir(t *testing.T) {
	empty := t.TempDir()  // the service's cwd: no charly.yml
	projDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, projectFileName), []byte("version: 2026.225.1508\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, empty)
	t.Setenv("CHARLY_PROJECT_DIR", projDir)

	got := computeProjectPrefix(false)
	want := []string{"--dir", projDir}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("computeProjectPrefix = %v, want %v (a project named by CHARLY_PROJECT_DIR must not fall through to --repo default)", got, want)
	}
}

// A cwd project still wins and needs no prefix at all — the pre-existing behaviour.
func TestComputeProjectPrefix_CwdProjectNeedsNoPrefix(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, projectFileName), []byte("version: 2026.225.1508\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)
	t.Setenv("CHARLY_PROJECT_DIR", "")

	if got := computeProjectPrefix(false); got != nil {
		t.Errorf("computeProjectPrefix = %v, want nil for a cwd project", got)
	}
}

// CHARLY_PROJECT_DIR pointing at a directory with NO project must not be trusted: the
// fallback still applies, so this does not quietly turn a missing project into a broken --dir.
func TestComputeProjectPrefix_EmptyProjectDirFallsBack(t *testing.T) {
	chdir(t, t.TempDir())
	t.Setenv("CHARLY_PROJECT_DIR", t.TempDir()) // exists, but holds no charly.yml

	got := computeProjectPrefix(false)
	want := []string{"--repo", "default"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("computeProjectPrefix = %v, want %v", got, want)
	}
}

// --no-default-repo still suppresses the network fallback entirely.
func TestComputeProjectPrefix_NoDefaultRepoSuppressesFallback(t *testing.T) {
	chdir(t, t.TempDir())
	t.Setenv("CHARLY_PROJECT_DIR", "")

	if got := computeProjectPrefix(true); got != nil {
		t.Errorf("computeProjectPrefix = %v, want nil under --no-default-repo", got)
	}
}
