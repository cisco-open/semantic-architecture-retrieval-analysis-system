/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package cli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const (
	githubRepo   = "cisco-open/semantic-architecture-retrieval-analysis-system"
	githubAPIURL = "https://api.github.com/repos/" + githubRepo + "/releases/latest"
)

func init() {
	rootCmd.AddCommand(updateCmd)
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update saras to the latest version",
	Long: `Check GitHub for the latest release and update the saras binary in-place.

If the current version is already the latest, no update is performed.
The binary is replaced atomically by writing to a temp file first.`,
	RunE: runUpdate,
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func runUpdate(cmd *cobra.Command, args []string) error {
	fmt.Fprintln(cmd.OutOrStdout(), "Checking for updates...")

	// Fetch latest release info
	release, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("fetch latest release: %w", err)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := strings.TrimPrefix(version, "v")

	if currentVersion == latestVersion && currentVersion != "dev" {
		fmt.Fprintf(cmd.OutOrStdout(), "Already up to date (v%s)\n", currentVersion)
		return nil
	}

	if currentVersion != "dev" {
		fmt.Fprintf(cmd.OutOrStdout(), "Current version: v%s\n", currentVersion)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Latest version:  v%s\n", latestVersion)

	// Find the matching asset
	assetName, downloadURL, err := findAsset(release)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Downloading %s...\n", assetName)

	// Download to temp file
	tmpDir, err := os.MkdirTemp("", "saras-update-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, assetName)
	if err := downloadFile(archivePath, downloadURL); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Extract the binary
	fmt.Fprintln(cmd.OutOrStdout(), "Extracting...")
	binaryName := "saras"
	if runtime.GOOS == "windows" {
		binaryName = "saras.exe"
	}

	extractedPath := filepath.Join(tmpDir, binaryName)
	if strings.HasSuffix(assetName, ".tar.gz") {
		if err := extractTarGz(archivePath, tmpDir, binaryName); err != nil {
			return fmt.Errorf("extract tar.gz: %w", err)
		}
	} else if strings.HasSuffix(assetName, ".zip") {
		if err := extractZip(archivePath, tmpDir, binaryName); err != nil {
			return fmt.Errorf("extract zip: %w", err)
		}
	} else {
		return fmt.Errorf("unsupported archive format: %s", assetName)
	}

	// Replace the current binary
	currentBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find current binary: %w", err)
	}
	currentBinary, err = filepath.EvalSymlinks(currentBinary)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Updating %s...\n", currentBinary)
	if err := replaceBinary(extractedPath, currentBinary); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Successfully updated to v%s\n", latestVersion)
	return nil
}

func fetchLatestRelease() (*githubRelease, error) {
	req, err := http.NewRequest("GET", githubAPIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "saras-updater")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

func findAsset(release *githubRelease) (name, url string, err error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}

	ver := strings.TrimPrefix(release.TagName, "v")

	// Match goreleaser naming: saras_<version>_<os>_<arch>.<ext>
	expected := fmt.Sprintf("saras_%s_%s_%s.%s", ver, goos, goarch, ext)

	for _, asset := range release.Assets {
		if asset.Name == expected {
			return asset.Name, asset.BrowserDownloadURL, nil
		}
	}

	return "", "", fmt.Errorf("no release asset found for %s/%s (expected %s)", goos, goarch, expected)
}

func downloadFile(dest, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func extractTarGz(archivePath, destDir, targetName string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if filepath.Base(header.Name) == targetName && header.Typeflag == tar.TypeReg {
			out, err := os.OpenFile(filepath.Join(destDir, targetName), os.O_CREATE|os.O_WRONLY, 0755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
			return nil
		}
	}
	return fmt.Errorf("%s not found in archive", targetName)
}

func extractZip(archivePath, destDir, targetName string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if filepath.Base(f.Name) == targetName {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			out, err := os.OpenFile(filepath.Join(destDir, targetName), os.O_CREATE|os.O_WRONLY, 0755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, rc); err != nil {
				out.Close()
				return err
			}
			out.Close()
			return nil
		}
	}
	return fmt.Errorf("%s not found in archive", targetName)
}

func replaceBinary(newPath, currentPath string) error {
	// Read the new binary into memory to handle cross-device moves
	newBinary, err := os.ReadFile(newPath)
	if err != nil {
		return err
	}

	// Get permissions of the current binary
	info, err := os.Stat(currentPath)
	if err != nil {
		return err
	}

	// Write to a temp file next to the current binary for atomic replace
	dir := filepath.Dir(currentPath)
	tmp, err := os.CreateTemp(dir, ".saras-update-*")
	if err != nil {
		// If we can't write next to the binary (e.g. /usr/local/bin), try with sudo hint
		return fmt.Errorf("cannot write to %s (try running with sudo): %w", dir, err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(newBinary); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()

	if err := os.Chmod(tmpPath, info.Mode()); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Atomic rename
	if err := os.Rename(tmpPath, currentPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename failed (try running with sudo): %w", err)
	}

	return nil
}
