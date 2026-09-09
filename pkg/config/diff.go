package config

import (
	"fmt"
	"reflect"
	"strings"
)

// ConfigDiff describes semantic changes between an active configuration and an incoming candidate.
type ConfigDiff struct {
	ListenerChanged   bool     `json:"listener_changed"`
	PortChanged       bool     `json:"port_changed"`
	AddedRoutes       []string `json:"added_routes,omitempty"`
	RemovedRoutes     []string `json:"removed_routes,omitempty"`
	ModifiedRoutes    []string `json:"modified_routes,omitempty"`
	AddedUpstreams    []string `json:"added_upstreams,omitempty"`
	RemovedUpstreams  []string `json:"removed_upstreams,omitempty"`
	ModifiedUpstreams []string `json:"modified_upstreams,omitempty"`
	Changes           []string `json:"changes,omitempty"`
}

// HasChanges returns true if any semantic configuration difference was detected.
func (d *ConfigDiff) HasChanges() bool {
	return d.ListenerChanged ||
		len(d.AddedRoutes) > 0 ||
		len(d.RemovedRoutes) > 0 ||
		len(d.ModifiedRoutes) > 0 ||
		len(d.AddedUpstreams) > 0 ||
		len(d.RemovedUpstreams) > 0 ||
		len(d.ModifiedUpstreams) > 0 ||
		len(d.Changes) > 0
}

// String returns a human-readable summary of the diff.
func (d *ConfigDiff) String() string {
	if !d.HasChanges() {
		return "No configuration changes detected."
	}

	var sb strings.Builder
	sb.WriteString("Configuration Diff Summary:\n")
	for _, c := range d.Changes {
		sb.WriteString(fmt.Sprintf("  • %s\n", c))
	}
	return sb.String()
}

// ComputeDiff compares two GatewayConfig structs and generates a structured diff report.
func ComputeDiff(oldCfg, newCfg *GatewayConfig) ConfigDiff {
	diff := ConfigDiff{}
	if oldCfg == nil && newCfg == nil {
		return diff
	}
	if oldCfg == nil {
		diff.Changes = append(diff.Changes, "Initialized new configuration from nil")
		return diff
	}
	if newCfg == nil {
		diff.Changes = append(diff.Changes, "Incoming configuration is nil (destruction)")
		return diff
	}

	// 1. Compare Listener
	if oldCfg.Listener.Port != newCfg.Listener.Port {
		diff.ListenerChanged = true
		diff.PortChanged = true
		diff.Changes = append(diff.Changes, fmt.Sprintf("Listener port changed from %d to %d (REQUIRES FULL PROCESS RESTART TO REBIND)", oldCfg.Listener.Port, newCfg.Listener.Port))
	}
	if oldCfg.Listener.Host != newCfg.Listener.Host {
		diff.ListenerChanged = true
		diff.Changes = append(diff.Changes, fmt.Sprintf("Listener host changed from %q to %q", oldCfg.Listener.Host, newCfg.Listener.Host))
	}
	if oldCfg.Listener.ReadTimeout != newCfg.Listener.ReadTimeout {
		diff.ListenerChanged = true
		diff.Changes = append(diff.Changes, fmt.Sprintf("Listener read timeout changed from %v to %v", oldCfg.Listener.ReadTimeout, newCfg.Listener.ReadTimeout))
	}
	if oldCfg.Listener.WriteTimeout != newCfg.Listener.WriteTimeout {
		diff.ListenerChanged = true
		diff.Changes = append(diff.Changes, fmt.Sprintf("Listener write timeout changed from %v to %v", oldCfg.Listener.WriteTimeout, newCfg.Listener.WriteTimeout))
	}

	// 2. Compare Routes
	oldRoutes := make(map[string]RouteConfig)
	for _, r := range oldCfg.Routes {
		oldRoutes[r.ID] = r
	}
	newRoutes := make(map[string]RouteConfig)
	for _, r := range newCfg.Routes {
		newRoutes[r.ID] = r
	}

	for id, nr := range newRoutes {
		if or, exists := oldRoutes[id]; !exists {
			diff.AddedRoutes = append(diff.AddedRoutes, id)
			diff.Changes = append(diff.Changes, fmt.Sprintf("Route added: %q [path=%s, upstream=%s]", id, nr.Path, nr.UpstreamID))
		} else {
			methodsChanged := !reflect.DeepEqual(or.Methods, nr.Methods)
			if or.Path != nr.Path || or.UpstreamID != nr.UpstreamID || or.Timeout != nr.Timeout || or.StripPrefix != nr.StripPrefix || methodsChanged {
				diff.ModifiedRoutes = append(diff.ModifiedRoutes, id)
				diff.Changes = append(diff.Changes, fmt.Sprintf("Route modified: %q [path: %s -> %s, upstream: %s -> %s]", id, or.Path, nr.Path, or.UpstreamID, nr.UpstreamID))
			}
		}
	}

	for id := range oldRoutes {
		if _, exists := newRoutes[id]; !exists {
			diff.RemovedRoutes = append(diff.RemovedRoutes, id)
			diff.Changes = append(diff.Changes, fmt.Sprintf("Route removed: %q", id))
		}
	}

	// 3. Compare Upstreams
	oldUpstreams := make(map[string]UpstreamConfig)
	for _, u := range oldCfg.Upstreams {
		oldUpstreams[u.ID] = u
	}
	newUpstreams := make(map[string]UpstreamConfig)
	for _, u := range newCfg.Upstreams {
		newUpstreams[u.ID] = u
	}

	for id, nu := range newUpstreams {
		if ou, exists := oldUpstreams[id]; !exists {
			diff.AddedUpstreams = append(diff.AddedUpstreams, id)
			diff.Changes = append(diff.Changes, fmt.Sprintf("Upstream pool added: %q [algo=%s, targets=%d]", id, nu.Algorithm, len(nu.Targets)))
		} else {
			if ou.Algorithm != nu.Algorithm || len(ou.Targets) != len(nu.Targets) {
				diff.ModifiedUpstreams = append(diff.ModifiedUpstreams, id)
				diff.Changes = append(diff.Changes, fmt.Sprintf("Upstream pool modified: %q [algo: %s -> %s, targets: %d -> %d]", id, ou.Algorithm, nu.Algorithm, len(ou.Targets), len(nu.Targets)))
			} else {
				// Compare targets
				targetDiff := false
				for idx, ot := range ou.Targets {
					nt := nu.Targets[idx]
					if ot.URL != nt.URL || ot.Weight != nt.Weight || ot.Drain != nt.Drain {
						targetDiff = true
						break
					}
				}
				if targetDiff {
					diff.ModifiedUpstreams = append(diff.ModifiedUpstreams, id)
					diff.Changes = append(diff.Changes, fmt.Sprintf("Upstream pool targets updated: %q", id))
				}
			}
		}
	}

	for id := range oldUpstreams {
		if _, exists := newUpstreams[id]; !exists {
			diff.RemovedUpstreams = append(diff.RemovedUpstreams, id)
			diff.Changes = append(diff.Changes, fmt.Sprintf("Upstream pool removed: %q", id))
		}
	}

	return diff
}

// DryRun performs end-to-end deserialization and validation of candidate configuration bytes
// without mutating any active gateway state.
// Returns the candidate struct, the semantic diff relative to currentCfg, and any error encountered.
func DryRun(rawBytes []byte, currentCfg *GatewayConfig) (*GatewayConfig, ConfigDiff, error) {
	candidate, err := Parse(rawBytes)
	if err != nil {
		return nil, ConfigDiff{}, fmt.Errorf("dry-run parse failed: %w", err)
	}

	ApplyDefaults(candidate)
	ApplyEnvironmentOverrides(candidate)

	if err := Validate(candidate); err != nil {
		return nil, ConfigDiff{}, fmt.Errorf("dry-run validation failed: %w", err)
	}

	diff := ComputeDiff(currentCfg, candidate)
	return candidate, diff, nil
}
