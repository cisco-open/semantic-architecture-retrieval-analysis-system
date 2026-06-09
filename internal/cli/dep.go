/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(depCmd)
	depCmd.AddCommand(depAddCmd)
	depCmd.AddCommand(depRemoveCmd)
	depCmd.AddCommand(depListCmd)

	depAddCmd.Flags().String("role", "", "Dependency role (required): legacy, shared-lib, reference, service")
	depAddCmd.Flags().String("name", "", "Alias for the dependency (defaults to directory name)")
	depAddCmd.Flags().Bool("force", false, "Add even if embedding configuration is incompatible")
	_ = depAddCmd.MarkFlagRequired("role")
}

var depCmd = &cobra.Command{
	Use:   "dep",
	Short: "Manage cross-repository dependencies",
	Long: `Manage references to other saras-initialized repositories.

Dependencies allow saras to search, ask, trace, flow, and map across
multiple codebases. Each dependency must have a role that describes its
relationship to the current project.

Roles:
  legacy      Legacy codebase being migrated from
  shared-lib  Shared library or utility repository
  reference   Reference implementation or documentation
  service     Microservice or API that this project communicates with

Examples:
  saras dep add /path/to/old-auth --role legacy --name legacy-auth
  saras dep add ../shared-proto --role shared-lib
  saras dep remove legacy-auth
  saras dep list`,
}

var depAddCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Add a dependency on another saras-initialized repository",
	Args:  cobra.ExactArgs(1),
	RunE:  runDepAdd,
}

var depRemoveCmd = &cobra.Command{
	Use:   "remove <name-or-path>",
	Short: "Remove a dependency",
	Args:  cobra.ExactArgs(1),
	RunE:  runDepRemove,
}

var depListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured dependencies",
	RunE:  runDepList,
}

func runDepAdd(cmd *cobra.Command, args []string) error {
	depPath := args[0]
	role, _ := cmd.Flags().GetString("role")
	name, _ := cmd.Flags().GetString("name")
	force, _ := cmd.Flags().GetBool("force")

	// Resolve to absolute path
	absPath, err := filepath.Abs(depPath)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Check that the target is a saras-initialized project
	if !config.Exists(absPath) {
		return fmt.Errorf("path %q is not a saras-initialized project (no .saras/config.yaml found)", absPath)
	}

	// Default name to directory base name
	if name == "" {
		name = filepath.Base(absPath)
	}

	// Load current project config
	projectRoot, err := config.FindProjectRoot()
	if err != nil {
		return fmt.Errorf("not a saras project (run 'saras init' first): %w", err)
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Load dependency config and check embedding compatibility
	depCfg, err := config.Load(absPath)
	if err != nil {
		return fmt.Errorf("load dependency config: %w", err)
	}

	if err := cfg.CheckEmbeddingCompatibility(depCfg); err != nil {
		if !force {
			return fmt.Errorf("%s\nUse --force to add anyway (vector search across this dependency may return poor results)", err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", err)
	}

	// Add dependency
	dep := config.Dependency{
		Name: name,
		Path: absPath,
		Role: role,
	}
	if err := cfg.AddDependency(dep); err != nil {
		return err
	}

	// Save
	if err := cfg.Save(projectRoot); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Added dependency %q (%s) → %s\n", name, role, absPath)
	return nil
}

func runDepRemove(cmd *cobra.Command, args []string) error {
	nameOrPath := args[0]

	projectRoot, err := config.FindProjectRoot()
	if err != nil {
		return fmt.Errorf("not a saras project (run 'saras init' first): %w", err)
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := cfg.RemoveDependency(nameOrPath); err != nil {
		return err
	}

	if err := cfg.Save(projectRoot); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Removed dependency %q\n", nameOrPath)
	return nil
}

func runDepList(cmd *cobra.Command, args []string) error {
	projectRoot, err := config.FindProjectRoot()
	if err != nil {
		return fmt.Errorf("not a saras project (run 'saras init' first): %w", err)
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.Dependencies) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No dependencies configured. Use 'saras dep add' to add one.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tROLE\tPATH\tSTATUS")
	for _, dep := range cfg.Dependencies {
		status := "ok"
		if !config.Exists(dep.Path) {
			status = "missing"
		} else if _, err := os.Stat(config.GetIndexPath(dep.Path)); err != nil {
			status = "not indexed"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", dep.Name, dep.Role, dep.Path, status)
	}
	w.Flush()
	return nil
}
