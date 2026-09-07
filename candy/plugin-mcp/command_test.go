package mcp

import (
	"testing"

	"github.com/alecthomas/kong"
)

// TestMcpServeListenDefaultLoopback — P3: `charly mcp serve` binds 127.0.0.1 ONLY by
// default (loopback); `--listen 0.0.0.0:18765` is the explicit opt-in for remote/container
// access. Guards the flag default so a regression to all-interfaces binding fails.
func TestMcpServeListenDefaultLoopback(t *testing.T) {
	var grp McpCmdGroup
	parser, err := kong.New(&grp, kong.Name("mcp"))
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	if _, err := parser.Parse([]string{"serve"}); err != nil {
		t.Fatalf("parse serve: %v", err)
	}
	if got := grp.Serve.Listen; got != "127.0.0.1:18765" {
		t.Errorf("default listen = %q, want 127.0.0.1:18765 (loopback only; 0.0.0.0 is the explicit opt-in)", got)
	}
}
