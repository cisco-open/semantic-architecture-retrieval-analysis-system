/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

// Package copilot integrates GitHub Copilot as an LLM and embedding backend
// for saras. It implements:
//   - GitHub device-code OAuth flow (no API key required from the user)
//   - Persistent storage of the GitHub OAuth token in the user's config dir
//     (NEVER inside the project directory, to avoid accidental git commits)
//   - On-demand exchange and caching of short-lived Copilot API tokens
//   - An http.RoundTripper that transparently injects the required Copilot
//     headers (Authorization, Editor-Version, Copilot-Integration-Id, etc.)
//     and refreshes the Copilot API token when it nears expiry.
//
// The public Copilot OAuth client id used here is the same one used by
// editor integrations such as copilot.vim / copilot.lua / zed. Saras does NOT
// ship any private credentials; the OAuth flow is performed entirely on the
// user's machine and the resulting token grants access only to the user's
// own Copilot subscription.
package copilot
