/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package config

import "fmt"

// ResolveDeps returns the list of dependencies to query based on the flags.
// Exactly one of fromDep, allDeps, or withDeps should be non-empty / true.
// An empty return with withDeps means "current repo + all deps" (caller adds current).
func ResolveDeps(cfg *Config, fromDep string, allDeps, withDeps bool) ([]Dependency, bool, error) {
	set := 0
	if fromDep != "" {
		set++
	}
	if allDeps {
		set++
	}
	if withDeps {
		set++
	}
	if set > 1 {
		return nil, false, fmt.Errorf("--from-dep, --all-deps, and --with-deps are mutually exclusive")
	}

	// No dep flag set → current repo only
	if set == 0 {
		return nil, false, nil
	}

	if fromDep != "" {
		dep := cfg.FindDependency(fromDep)
		if dep == nil {
			return nil, false, fmt.Errorf("dependency %q not found (run 'saras dep list' to see available dependencies)", fromDep)
		}
		return []Dependency{*dep}, false, nil
	}

	if len(cfg.Dependencies) == 0 {
		return nil, false, fmt.Errorf("no dependencies configured (run 'saras dep add' first)")
	}

	// allDeps → deps only, withDeps → include current too
	includeCurrent := withDeps
	return cfg.Dependencies, includeCurrent, nil
}
