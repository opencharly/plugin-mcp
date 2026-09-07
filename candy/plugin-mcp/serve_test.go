package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// withCwd runs fn with the process cwd set to dir, restoring it afterwards.
func withCwd(t *testing.T, dir string, fn func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})
	fn()
}

// withEmptyCwd runs fn from a temp dir that carries NO charly.yml.
func withEmptyCwd(t *testing.T, fn func()) {
	t.Helper()
	withCwd(t, t.TempDir(), fn)
}

// withCharlyYmlCwd runs fn from a temp dir that carries a charly.yml.
func withCharlyYmlCwd(t *testing.T, fn func()) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, projectFileName), []byte("version: 2026.248.1030\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withCwd(t, dir, fn)
}

// clearProjectEnv guarantees neither project-scope env var is set (empty == unset for
// os.Getenv), so the env-authoritative branch of computeProjectPrefix cannot trigger.
func clearProjectEnv(t *testing.T) {
	t.Helper()
	t.Setenv(spec.ProjectDirEnv, "")
	t.Setenv(spec.ProjectRepoEnv, "")
}

// TestComputeProjectPrefix_EnvDirSet — P2: CHARLY_PROJECT_DIR set in the environment is
// authoritative; the server passes NO prefix (the child inherits and resolves it). This
// holds even with no charly.yml in cwd and noDefaultRepo=false.
func TestComputeProjectPrefix_EnvDirSet(t *testing.T) {
	t.Setenv(spec.ProjectDirEnv, "/some/project")
	t.Setenv(spec.ProjectRepoEnv, "")
	withEmptyCwd(t, func() {
		if got := computeProjectPrefix(false); got != nil {
			t.Fatalf("CHARLY_PROJECT_DIR set: expected nil prefix, got %v", got)
		}
	})
}

// TestComputeProjectPrefix_EnvRepoSet — CHARLY_PROJECT_REPO set is equally authoritative.
func TestComputeProjectPrefix_EnvRepoSet(t *testing.T) {
	t.Setenv(spec.ProjectDirEnv, "")
	t.Setenv(spec.ProjectRepoEnv, "opencharly/charly")
	withEmptyCwd(t, func() {
		if got := computeProjectPrefix(false); got != nil {
			t.Fatalf("CHARLY_PROJECT_REPO set: expected nil prefix, got %v", got)
		}
	})
}

// TestComputeProjectPrefix_CwdHasCharlyYml — no env, charly.yml in cwd → no prefix.
func TestComputeProjectPrefix_CwdHasCharlyYml(t *testing.T) {
	clearProjectEnv(t)
	withCharlyYmlCwd(t, func() {
		if got := computeProjectPrefix(false); got != nil {
			t.Fatalf("charly.yml in cwd: expected nil prefix, got %v", got)
		}
	})
}

// TestComputeProjectPrefix_NoDefaultRepo — no env, no charly.yml in cwd, --no-default-repo
// → no prefix (project-dependent tools error at call time).
func TestComputeProjectPrefix_NoDefaultRepo(t *testing.T) {
	clearProjectEnv(t)
	withEmptyCwd(t, func() {
		if got := computeProjectPrefix(true); got != nil {
			t.Fatalf("noDefaultRepo: expected nil prefix, got %v", got)
		}
	})
}

// TestComputeProjectPrefix_DefaultRepo — no env, no charly.yml in cwd → the --repo default
// fallback prefix (unchanged).
func TestComputeProjectPrefix_DefaultRepo(t *testing.T) {
	clearProjectEnv(t)
	withEmptyCwd(t, func() {
		want := []string{"--repo", "default"}
		got := computeProjectPrefix(false)
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("fallback: expected %v, got %v", want, got)
		}
	})
}

// TestChildCharlyEnv_InheritsProjectEnv — P2: the environment is authoritative; no CHARLY
// env var is ever stripped from a child. childCharlyEnv must pass CHARLY_PROJECT_DIR and
// CHARLY_PROJECT_REPO through verbatim.
func TestChildCharlyEnv_InheritsProjectEnv(t *testing.T) {
	t.Setenv(spec.ProjectDirEnv, "/workspace")
	t.Setenv(spec.ProjectRepoEnv, "opencharly/charly")
	env := childCharlyEnv()
	seen := map[string]string{}
	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		if name == spec.ProjectDirEnv || name == spec.ProjectRepoEnv {
			seen[name] = value
		}
	}
	if v, ok := seen[spec.ProjectDirEnv]; !ok || v != "/workspace" {
		t.Errorf("CHARLY_PROJECT_DIR was stripped or altered: got %q, want %q", v, "/workspace")
	}
	if v, ok := seen[spec.ProjectRepoEnv]; !ok || v != "opencharly/charly" {
		t.Errorf("CHARLY_PROJECT_REPO was stripped or altered: got %q, want %q", v, "opencharly/charly")
	}
}
