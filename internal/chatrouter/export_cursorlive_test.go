//go:build cursorlive

package chatrouter

import "github.com/Mutech-Innovation/coding-anywhere/internal/agent/acpagent"

// BuildAdapterForSessionForTest exposes buildAdapterForSession to the
// cursorlive live-fire tests (package chatrouter_test).
func BuildAdapterForSessionForTest(r *Router, agentID, model string) (*acpagent.Adapter, error) {
	return r.buildAdapterForSession(agentID, model)
}
