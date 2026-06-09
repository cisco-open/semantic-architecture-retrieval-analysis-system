/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"time"

	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/copilot"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/tui"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(copilotCmd)
	copilotCmd.AddCommand(copilotLoginCmd)
	copilotCmd.AddCommand(copilotLogoutCmd)
	copilotCmd.AddCommand(copilotStatusCmd)

	copilotLoginCmd.Flags().Bool("no-browser", false, "Do not attempt to open the verification URL in a browser")
}

var copilotCmd = &cobra.Command{
	Use:   "copilot",
	Short: "Authenticate saras with GitHub Copilot",
	Long: `Manage the GitHub Copilot integration. After signing in, saras can be
configured to use Copilot as either the embedding or the chat/LLM provider,
removing the need to type in an API key.

Subcommands:
  login    Authorize saras with your GitHub Copilot subscription
  logout   Remove the locally stored OAuth token
  status   Show the current authentication state`,
}

var copilotLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authorize saras with your GitHub Copilot subscription",
	Long: `Run the GitHub device-code OAuth flow to authorize saras. The resulting
OAuth token is stored in your user config directory (NOT in the project)
with 0600 permissions so it is never committed by accident.

Saras uses the same public OAuth client id as other editor integrations
(copilot.vim, copilot.lua); no API key from GitHub is required.`,
	RunE: runCopilotLogin,
}

var copilotLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove the locally stored Copilot OAuth token",
	RunE:  runCopilotLogout,
}

var copilotStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether saras is signed in to GitHub Copilot",
	RunE:  runCopilotStatus,
}

func runCopilotLogin(cmd *cobra.Command, _ []string) error {
	noBrowser, _ := cmd.Flags().GetBool("no-browser")
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	out := cmd.OutOrStdout()

	if existing, err := copilot.LoadPersistedToken(); err == nil && existing.AccessToken != "" {
		fmt.Fprintf(out, "%s Already signed in", tui.SymbolCheck)
		if existing.User != "" {
			fmt.Fprintf(out, " as %s", tui.SuccessStyle.Render("@"+existing.User))
		}
		fmt.Fprintln(out, ". Use 'saras copilot logout' to switch accounts.")
		return nil
	}

	fmt.Fprintln(out, "Requesting GitHub device code...")
	dc, err := copilot.RequestDeviceCode(ctx)
	if err != nil {
		return fmt.Errorf("device code: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s Go to:        %s\n", tui.SymbolArrow, tui.SuccessStyle.Render(dc.VerificationURI))
	fmt.Fprintf(out, "%s Enter code:   %s\n", tui.SymbolArrow, tui.SuccessStyle.Render(dc.UserCode))
	fmt.Fprintln(out)
	fmt.Fprintln(out, tui.MutedStyle.Render("Waiting for authorization... (press Ctrl+C to cancel)"))

	if !noBrowser {
		_ = openBrowser(dc.VerificationURI)
	}

	pt, err := copilot.PollAccessToken(ctx, dc)
	if err != nil {
		return fmt.Errorf("authorize: %w", err)
	}
	if err := copilot.SavePersistedToken(pt); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	tokenPath, _ := copilot.TokenPath()
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s Signed in", tui.SymbolCheck)
	if pt.User != "" {
		fmt.Fprintf(out, " as %s", tui.SuccessStyle.Render("@"+pt.User))
	}
	fmt.Fprintln(out, ".")
	if tokenPath != "" {
		fmt.Fprintf(out, "  Token saved to %s (0600).\n", tokenPath)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Next steps:")
	fmt.Fprintln(out, "  - Run 'saras init' and choose 'copilot' as the embedding and/or LLM provider")
	fmt.Fprintln(out, "  - Or in an existing project edit .saras/config.yaml and set provider: copilot")

	// Best-effort token health check so users get an immediate signal that
	// their account has access to the Copilot API.
	ts := copilot.NewTokenSource()
	if _, err := ts.Get(ctx); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "\n%s Could not validate Copilot API access: %v\n", tui.SymbolWarning, err)
	}

	return nil
}

func runCopilotLogout(cmd *cobra.Command, _ []string) error {
	loggedIn := true
	if _, err := copilot.LoadPersistedToken(); errors.Is(err, copilot.ErrNotLoggedIn) {
		loggedIn = false
	}
	if !loggedIn {
		// Even when not "logged in" we may still have a stale API token
		// cache lying around (e.g. left over from a previous account).
		// Clear it so the next login starts clean.
		_ = copilot.DeleteCachedAPIToken()
		fmt.Fprintln(cmd.OutOrStdout(), "Not signed in. Nothing to do.")
		return nil
	}
	if err := copilot.DeletePersistedToken(); err != nil {
		return err
	}
	// The cached short-lived API token is derived from the OAuth token we
	// just removed, so it must go too.
	if err := copilot.DeleteCachedAPIToken(); err != nil {
		// Best-effort: log but do not fail the logout.
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not remove api token cache: %v\n", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s Signed out. The on-disk OAuth token and API-token cache were removed.\n", tui.SymbolCheck)
	return nil
}

func runCopilotStatus(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()

	pt, err := copilot.LoadPersistedToken()
	if errors.Is(err, copilot.ErrNotLoggedIn) {
		fmt.Fprintf(out, "%s Not signed in. Run 'saras copilot login' to authenticate.\n", tui.SymbolCross)
		return nil
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "%s Signed in", tui.SymbolCheck)
	if pt.User != "" {
		fmt.Fprintf(out, " as %s", tui.SuccessStyle.Render("@"+pt.User))
	}
	fmt.Fprintln(out, ".")

	if tokenPath, perr := copilot.TokenPath(); perr == nil {
		fmt.Fprintf(out, "  Token file:  %s\n", tokenPath)
	}
	if !pt.CreatedAt.IsZero() {
		fmt.Fprintf(out, "  Created at:  %s\n", pt.CreatedAt.Local().Format("2006-01-02 15:04:05 MST"))
	}

	// Report on the on-disk API-token cache before doing a (potentially
	// network-touching) Get(). This gives the user a quick read on whether
	// the next saras command will hit GitHub or use a cached token.
	if cachePath, perr := copilot.APITokenCachePath(); perr == nil {
		fmt.Fprintf(out, "  Cache file:  %s\n", cachePath)
	}
	if cached, cerr := copilot.LoadCachedAPIToken(); cerr == nil && cached != nil {
		remain := time.Until(cached.ExpiresAt)
		if remain > 0 {
			fmt.Fprintf(out, "  Cached API:  %s valid for ~%s (fetched %s ago)\n",
				tui.SymbolCheck, formatDuration(remain), formatDuration(time.Since(cached.FetchedAt)))
		} else {
			fmt.Fprintf(out, "  Cached API:  %s expired; next call will refresh\n", tui.SymbolWarning)
		}
	} else {
		fmt.Fprintf(out, "  Cached API:  (none — next call will fetch)\n")
	}

	ts := copilot.NewTokenSource()
	tok, terr := ts.Get(cmd.Context())
	if terr != nil {
		fmt.Fprintf(out, "  API access:  %s %v\n", tui.SymbolCross, terr)
		return nil
	}
	fmt.Fprintf(out, "  API access:  %s ok (token valid for ~%s)\n",
		tui.SymbolCheck, formatDuration(time.Until(tok.ExpiresAt)))
	return nil
}

// openBrowser launches the platform-appropriate command to open a URL.
// Failures are silent because the URL is also printed to the terminal.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Start()
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}
	d = d.Round(time.Second)
	if d >= time.Minute {
		d = d.Round(time.Minute)
	}
	return d.String()
}
