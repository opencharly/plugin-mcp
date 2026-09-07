package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// resolve.go builds the mcp check endpoint from the GENERIC reverse-legs, replacing the former
// host-side mcp preresolver: the plugin reads the deployment's ai.opencharly.mcp_provide
// OCI label (cc.ResolveImageLabel), falling back to the check env's mcp_provide declarations
// (spec.CheckEnv — the P4 substrate-neutral source for VM/host venues, which have no
// podman-inspectable OCI label), + maps the picked server's port to a host-routable address
// (cc.ResolveEndpoint). The host owns the podman engine / OCI metadata / port-mapping machinery;
// the plugin decides WHAT to resolve and does the pure template / pod-aware / pick logic.

// mcpProvideLabel is the OCI label carrying a deployment's declared mcp_provide servers (a JSON
// array of spec.CandyMCPProvide). Mirrors charly's labels.go LabelMCPProvide. The container
// venue's declarations ride this label; a VM/host venue has no podman-inspectable label, so the
// declarations arrive in the check env instead (spec.CheckEnv.MCPProvide — P4 substrate
// neutrality).
const mcpProvideLabel = "ai.opencharly.mcp_provide"

// resolveMcpEndpoint resolves the mcp check context. It returns (nil, "") when the deployment
// declares no mcp_provides (a hard fail, mirroring the former host behaviour, surfaced by the
// caller). For `servers` only the Entries are filled (no dial); every other method also picks a
// single server and rewrites its container-network URL to a host-routable one.
//
// The declared servers come from the deployment's ai.opencharly.mcp_provide OCI label when that
// venue supplies one (container). When the label is absent/empty OR the label leg cannot inspect
// the venue (a VM/host deployment has no podman-inspectable image label) the check env's
// mcp_provide declarations — threaded by the host from the deployment's box config — are the
// fallback (P4 substrate neutrality). That env path skips the endpoint rewrite for a host-local
// 127.0.0.1 URL (already host-reachable; a VM/host server's published port lands on the check
// host's own loopback) and rewrites everything else via cc.ResolveEndpoint, exactly like the vnc
// verb's pod+VM endpoints.
func resolveMcpEndpoint(ctx context.Context, cc kit.CheckContext, env *mcpEnv, method, wantName string) (*mcpEndpoint, error) {
	provides, fromEnv, err := effectiveMCPProvides(ctx, cc, env)
	if err != nil {
		return nil, err
	}
	if len(provides) == 0 {
		return nil, fmt.Errorf("box %q declares no mcp_provides", env.Box)
	}

	entries := make([]spec.MCPProvideEntry, 0, len(provides))
	for _, p := range provides {
		entries = append(entries, spec.MCPProvideEntry{
			Name:      p.Name,
			URL:       resolveContainerNameTemplate(p.URL, env.ContainerName),
			Transport: p.Transport,
			Source:    env.Box,
		})
	}
	entries = spec.PodAwareMCPProvides(entries, env.Box, env.ContainerName)
	ep := &mcpEndpoint{Entries: entries}

	// `servers` is metadata-only — no dial, no URL rewrite.
	if method == "servers" {
		return ep, nil
	}

	// Every other method dials a single picked server.
	entry, err := pickMcpEntry(entries, wantName)
	if err != nil {
		return nil, err
	}
	rewritten := entry.URL
	// A host-local 127.0.0.1 URL declared in the env is already host-reachable (the VM/host
	// server's published port lands on the check host's own loopback) — no rewrite. The
	// container label path stays unchanged: container-network URLs (incl. loopback inside the
	// container) always go through ResolveEndpoint. Env URLs with a non-loopback host go through
	// ResolveEndpoint too (a VM's forwarded port, the vnc verb's pod+VM pattern).
	if !(fromEnv && isLoopbackURL(entry.URL)) {
		rewritten, err = rewriteURLViaEndpoint(ctx, cc.ResolveEndpoint, entry.URL, env.ContainerName)
		if err != nil {
			return nil, err
		}
	}
	ep.URL = rewritten
	ep.Transport = entry.Transport
	ep.Name = entry.Name
	return ep, nil
}

// effectiveMCPProvides returns the deployment's mcp_provide declarations and whether they came
// from the check env (fromEnv — a VM/host venue) rather than the container OCI label. The label
// path is authoritative when the label carries entries; an absent/empty label falls back to the
// env's declarations. A label-leg failure only falls back too — the plugin-check leg refuses
// non-container venues with "container for %s is not running", the exact P4 case — and is
// propagated when the env has nothing to stand on (a genuine resolution failure must not be
// masked).
func effectiveMCPProvides(ctx context.Context, cc kit.CheckContext, env *mcpEnv) ([]spec.CandyMCPProvide, bool, error) {
	raw, labelErr := cc.ResolveImageLabel(ctx, mcpProvideLabel)
	if labelErr == nil && raw != "" {
		var labelProvides []spec.CandyMCPProvide
		if err := json.Unmarshal([]byte(raw), &labelProvides); err != nil {
			return nil, false, fmt.Errorf("parsing %s label: %w", mcpProvideLabel, err)
		}
		if len(labelProvides) > 0 {
			return labelProvides, false, nil
		}
		// A present-but-empty label is treated as absent — fall through to the env.
	}
	if labelErr != nil && len(env.McpProvide) == 0 {
		return nil, false, labelErr
	}
	return env.McpProvide, true, nil
}

// isLoopbackURL reports whether raw's host is loopback (127.0.0.1 / localhost) — a host-local
// URL the check host dials as-is.
func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost":
		return true
	}
	return false
}

// resolveContainerNameTemplate substitutes the only placeholder charly emits into mcp_provide
// URLs, {{.ContainerName}}, with the running container name. Relocated from the host.
func resolveContainerNameTemplate(raw, ctrName string) string {
	if ctrName == "" {
		return raw
	}
	return strings.ReplaceAll(raw, "{{.ContainerName}}", ctrName)
}

// pickMcpEntry disambiguates by name. Empty wantName auto-picks when there is exactly one
// entry; errors on multiple with a clear listing. Relocated from the host.
func pickMcpEntry(entries []spec.MCPProvideEntry, wantName string) (spec.MCPProvideEntry, error) {
	switch {
	case len(entries) == 0:
		return spec.MCPProvideEntry{}, fmt.Errorf("no mcp_provides entries to pick from")
	case wantName != "":
		for _, e := range entries {
			if e.Name == wantName {
				return e, nil
			}
		}
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name
		}
		return spec.MCPProvideEntry{}, fmt.Errorf("no mcp_provides entry named %q (available: %s)", wantName, strings.Join(names, ", "))
	case len(entries) == 1:
		return entries[0], nil
	default:
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name
		}
		return spec.MCPProvideEntry{}, fmt.Errorf("image provides multiple mcp servers; use mcp_name (available: %s)", strings.Join(names, ", "))
	}
}

// rewriteURLViaEndpoint rewrites a container-network URL to a host-routable one using the
// generic ResolveEndpoint reverse-leg (the host maps the container's published port to a host
// address). A URL whose host is not the container name / localhost is returned unchanged (the
// user may have set an explicit external URL). Replaces the former host-side URL rewrite + container
// inspection. Takes the resolveEndpoint func (cc.ResolveEndpoint) so the pure logic is testable.
func rewriteURLViaEndpoint(ctx context.Context, resolveEndpoint func(context.Context, int) (string, error), rawURL, ctrName string) (string, error) {
	if rawURL == "" {
		return "", fmt.Errorf("mcp URL is empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing mcp URL %q: %w", rawURL, err)
	}
	host := u.Hostname()
	if host != ctrName && host != "localhost" && host != "127.0.0.1" {
		return rawURL, nil
	}
	if u.Port() == "" {
		return "", fmt.Errorf("mcp URL %q has no port (cannot map to a host port)", rawURL)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return "", fmt.Errorf("mcp URL %q has a non-numeric port: %w", rawURL, err)
	}
	addr, err := resolveEndpoint(ctx, port)
	if err != nil {
		return "", err
	}
	if addr == "" {
		return "", fmt.Errorf("container port %d is not published to a host port; declare `ports: [%d:%d]` in the image or run the test from inside the pod", port, port, port)
	}
	u.Host = addr
	return u.String(), nil
}
