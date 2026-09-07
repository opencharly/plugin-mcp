package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// pickMcpEntry — discriminator semantics (relocated into the plugin from the former host-side resolution when the
// pick/template/URL-rewrite logic relocated into the plugin, H part 2).

func TestPickMcpEntry_Empty(t *testing.T) {
	if _, err := pickMcpEntry(nil, ""); err == nil {
		t.Fatal("expected error for empty entries")
	}
}

func TestPickMcpEntry_SingleAutoPicks(t *testing.T) {
	entries := []spec.MCPProvideEntry{{Name: "a", URL: "http://x:1/mcp"}}
	got, err := pickMcpEntry(entries, "")
	if err != nil || got.Name != "a" {
		t.Fatalf("single auto-pick: got %+v err %v", got, err)
	}
}

func TestPickMcpEntry_MultipleRequireName(t *testing.T) {
	entries := []spec.MCPProvideEntry{{Name: "a"}, {Name: "b"}}
	if _, err := pickMcpEntry(entries, ""); err == nil {
		t.Fatal("expected error requiring mcp_name for multiple entries")
	}
}

func TestPickMcpEntry_NamedMatch(t *testing.T) {
	entries := []spec.MCPProvideEntry{{Name: "chrome-devtools"}, {Name: "other"}}
	got, err := pickMcpEntry(entries, "chrome-devtools")
	if err != nil || got.Name != "chrome-devtools" {
		t.Fatalf("named match: got %+v err %v", got, err)
	}
}

func TestPickMcpEntry_UnknownName(t *testing.T) {
	entries := []spec.MCPProvideEntry{{Name: "a"}}
	if _, err := pickMcpEntry(entries, "bogus"); err == nil {
		t.Fatal("expected error for unknown mcp_name")
	}
}

func TestResolveContainerNameTemplate(t *testing.T) {
	cases := []struct{ raw, ctrName, want string }{
		{"http://{{.ContainerName}}:8888/mcp", "charly-jupyter", "http://charly-jupyter:8888/mcp"},
		{"http://static-host:8888/mcp", "charly-jupyter", "http://static-host:8888/mcp"},
		{"http://{{.ContainerName}}:8888/mcp", "", "http://{{.ContainerName}}:8888/mcp"},
	}
	for _, tc := range cases {
		if got := resolveContainerNameTemplate(tc.raw, tc.ctrName); got != tc.want {
			t.Errorf("template(%q,%q) = %q, want %q", tc.raw, tc.ctrName, got, tc.want)
		}
	}
}

// rewriteURLViaEndpoint — the load-bearing translator (now via the ResolveEndpoint func).

func fixedEndpoint(addr string) func(context.Context, int) (string, error) {
	return func(context.Context, int) (string, error) { return addr, nil }
}

func TestRewriteURL_ContainerName(t *testing.T) {
	got, err := rewriteURLViaEndpoint(context.Background(), fixedEndpoint("127.0.0.1:8888"), "http://charly-jupyter:8888/mcp", "charly-jupyter")
	if err != nil || got != "http://127.0.0.1:8888/mcp" {
		t.Fatalf("container-name rewrite: got %q err %v", got, err)
	}
}

func TestRewriteURL_RemappedHostPort(t *testing.T) {
	got, err := rewriteURLViaEndpoint(context.Background(), fixedEndpoint("127.0.0.1:18888"), "http://charly-jupyter:8888/mcp", "charly-jupyter")
	if err != nil || got != "http://127.0.0.1:18888/mcp" {
		t.Fatalf("remapped host port: got %q err %v", got, err)
	}
}

func TestRewriteURL_ExternalHostPassthrough(t *testing.T) {
	got, err := rewriteURLViaEndpoint(context.Background(), fixedEndpoint("127.0.0.1:8888"), "https://mcp.example.com/api", "charly-jupyter")
	if err != nil || got != "https://mcp.example.com/api" {
		t.Fatalf("external passthrough: got %q err %v", got, err)
	}
}

func TestRewriteURL_NoHostPort(t *testing.T) {
	if _, err := rewriteURLViaEndpoint(context.Background(), fixedEndpoint(""), "http://charly-jupyter:8888/mcp", "charly-jupyter"); err == nil {
		t.Fatal("expected error when the port is not published (empty addr)")
	}
}

func TestRewriteURL_EndpointError(t *testing.T) {
	failing := func(context.Context, int) (string, error) { return "", errors.New("no venue") }
	if _, err := rewriteURLViaEndpoint(context.Background(), failing, "http://charly-jupyter:8888/mcp", "charly-jupyter"); err == nil {
		t.Fatal("expected the ResolveEndpoint error to propagate")
	}
}

// resolveMcpEndpoint — P4 substrate-neutral source selection. The container OCI label wins when
// present; a VM/host venue (no podman-inspectable label — an absent label OR a label-leg failure)
// resolves from the check env's mcp_provide declarations; env-declared 127.0.0.1 URLs are used
// as-is (already host-reachable, no ResolveEndpoint rewrite).

// stubCheckContext is a kit.CheckContext test double: every leg no-ops or returns the configured
// label / endpoint values.
type stubCheckContext struct {
	label    string
	labelErr error
	endpoint func(port int) (string, error)
}

var _ kit.CheckContext = (*stubCheckContext)(nil)

func (s *stubCheckContext) Exec() kit.Executor      { return nil }
func (s *stubCheckContext) Mode() spec.CheckRunMode { return spec.CheckModeLive }
func (s *stubCheckContext) HTTPDo(context.Context, spec.CheckHTTPRequest) (spec.CheckHTTPResponse, error) {
	return spec.CheckHTTPResponse{}, nil
}
func (s *stubCheckContext) ResolveEndpoint(_ context.Context, port int) (string, error) {
	if s.endpoint != nil {
		return s.endpoint(port)
	}
	return "", nil
}
func (s *stubCheckContext) ResolveGraphicsEndpoint(context.Context, string) (spec.CheckGraphicsEndpoint, error) {
	return spec.CheckGraphicsEndpoint{}, nil
}
func (s *stubCheckContext) ResolveImageLabel(context.Context, string) (string, error) {
	return s.label, s.labelErr
}
func (s *stubCheckContext) DialTimeout() time.Duration { return 0 }
func (s *stubCheckContext) Box() string                { return "" }
func (s *stubCheckContext) Instance() string           { return "" }
func (s *stubCheckContext) Distros() []string          { return nil }
func (s *stubCheckContext) AddBackground(int)          {}
func (s *stubCheckContext) InvokeProvider(context.Context, string, string, string, []byte, []byte) ([]byte, error) {
	return nil, nil
}

// TestResolveMcpEndpoint_EnvFallback — no OCI label + env mcp_provide: the env declarations
// resolve (P4 VM/host venue). The picked URL is pod-aware (container name → localhost) and the
// loopback exemption applies, so ResolveEndpoint is never consulted.
func TestResolveMcpEndpoint_EnvFallback(t *testing.T) {
	cc := &stubCheckContext{
		endpoint: func(port int) (string, error) { return "10.8.0.3:18765", nil },
	}
	env := &mcpEnv{
		Box:           "cachyos-mcp",
		ContainerName: "cachyos-mcp-1",
		McpProvide: []spec.CandyMCPProvide{
			{Name: "sys-mcp", URL: "http://cachyos-mcp-1:18765/mcp", Transport: "stdio"},
		},
	}
	ep, err := resolveMcpEndpoint(context.Background(), cc, env, "ping", "")
	if err != nil {
		t.Fatalf("env fallback: %v", err)
	}
	if len(ep.Entries) != 1 || ep.Entries[0].Name != "sys-mcp" {
		t.Fatalf("env fallback entries: %+v", ep.Entries)
	}
	if ep.Name != "sys-mcp" || ep.Transport != "stdio" {
		t.Fatalf("env fallback picked: %+v", ep)
	}
	// The pod-aware rewrite lands on localhost and the loopback exemption skips ResolveEndpoint
	// (a 10.8.0.3 rewrite would mean the exemption failed).
	if ep.URL != "http://localhost:18765/mcp" {
		t.Fatalf("env fallback URL = %q, want localhost (no endpoint rewrite)", ep.URL)
	}
}

// TestResolveMcpEndpoint_LabelWins — the container OCI label path stays authoritative when the
// label carries entries: the env declarations are ignored and the picked URL goes through
// ResolveEndpoint exactly as before (loopback exemption applies only to the env path).
func TestResolveMcpEndpoint_LabelWins(t *testing.T) {
	cc := &stubCheckContext{
		label:    `[{"name":"ctr-mcp","url":"http://fedora-coder-1:18765/mcp","transport":"stdio"}]`,
		endpoint: func(port int) (string, error) { return "127.0.0.1:18865", nil },
	}
	env := &mcpEnv{
		Box:           "fedora-coder",
		ContainerName: "fedora-coder-1",
		McpProvide: []spec.CandyMCPProvide{
			{Name: "env-mcp", URL: "http://127.0.0.1:18765/mcp", Transport: "stdio"},
		},
	}
	ep, err := resolveMcpEndpoint(context.Background(), cc, env, "ping", "")
	if err != nil {
		t.Fatalf("label path: %v", err)
	}
	if len(ep.Entries) != 1 || ep.Entries[0].Name != "ctr-mcp" || ep.Name != "ctr-mcp" {
		t.Fatalf("label path must win: entries %+v ep %+v", ep.Entries, ep)
	}
	if ep.URL != "http://127.0.0.1:18865/mcp" {
		t.Fatalf("label path URL = %q, want the ResolveEndpoint-rewritten 127.0.0.1:18865", ep.URL)
	}
}

// TestResolveMcpEndpoint_EnvLoopbackAsIs — a 127.0.0.1 env-declared URL is used as-is: the
// loopback exemption skips the ResolveEndpoint rewrite (a rewrite would land on 127.0.0.1:19999).
func TestResolveMcpEndpoint_EnvLoopbackAsIs(t *testing.T) {
	cc := &stubCheckContext{
		endpoint: func(port int) (string, error) { return "127.0.0.1:19999", nil },
	}
	env := &mcpEnv{
		Box:           "cachyos-mcp",
		ContainerName: "cachyos-mcp-1",
		McpProvide: []spec.CandyMCPProvide{
			{Name: "sys-mcp", URL: "http://127.0.0.1:18765/mcp", Transport: "stdio"},
		},
	}
	ep, err := resolveMcpEndpoint(context.Background(), cc, env, "ping", "")
	if err != nil {
		t.Fatalf("env loopback: %v", err)
	}
	if ep.Name != "sys-mcp" || ep.URL != "http://127.0.0.1:18765/mcp" {
		t.Fatalf("127.0.0.1 env URL must be used as-is: %+v", ep)
	}
}

// TestResolveMcpEndpoint_LabelLegErrorFallsBackToEnv — the label leg ERROR is the VM/host
// mechanism (plugin-check refuses non-container venues with "container for %s is not running"):
// with env declarations present the resolution falls back to them instead of failing.
func TestResolveMcpEndpoint_LabelLegErrorFallsBackToEnv(t *testing.T) {
	cc := &stubCheckContext{
		labelErr: errors.New("container for cachyos-mcp is not running"),
		endpoint: func(port int) (string, error) { return "192.168.122.15:18765", nil },
	}
	env := &mcpEnv{
		Box:           "cachyos-mcp",
		ContainerName: "charly-cachyos-mcp-1",
		McpProvide: []spec.CandyMCPProvide{
			{Name: "sys-mcp", URL: "http://charly-cachyos-mcp-1:18765/mcp", Transport: "stdio"},
		},
	}
	ep, err := resolveMcpEndpoint(context.Background(), cc, env, "ping", "")
	if err != nil {
		t.Fatalf("label-leg error must fall back to the env declarations: %v", err)
	}
	if ep.Name != "sys-mcp" {
		t.Fatalf("env fallback after label-leg error: %+v", ep)
	}
	if ep.URL != "http://localhost:18765/mcp" {
		t.Fatalf("VM URL = %q, want the pod-aware localhost form", ep.URL)
	}
}

// TestResolveMcpEndpoint_LabelLegErrorPropagates — with NO env declarations to stand on, the
// label-leg failure must propagate (a genuine resolution failure is never masked).
func TestResolveMcpEndpoint_LabelLegErrorPropagates(t *testing.T) {
	cc := &stubCheckContext{labelErr: errors.New("container for cachyos-mcp is not running")}
	env := &mcpEnv{Box: "cachyos-mcp", ContainerName: "cachyos-mcp-1"}
	if _, err := resolveMcpEndpoint(context.Background(), cc, env, "ping", ""); err == nil {
		t.Fatal("expected the label-leg error to propagate when the env has no declarations")
	}
}

// TestResolveMcpEndpoint_NoDeclarations — neither source carries declarations: the hard fail
// message matches the former host behaviour.
func TestResolveMcpEndpoint_NoDeclarations(t *testing.T) {
	cc := &stubCheckContext{}
	env := &mcpEnv{Box: "fedora-coder", ContainerName: "fedora-coder-1"}
	_, err := resolveMcpEndpoint(context.Background(), cc, env, "ping", "")
	if err == nil || !strings.Contains(err.Error(), "declares no mcp_provides") {
		t.Fatalf("expected the declares-no-mcp_provides fail, got %v", err)
	}
}
