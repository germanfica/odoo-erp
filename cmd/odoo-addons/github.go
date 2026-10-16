package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const maximumArchiveBytes int64 = 150 * 1024 * 1024
const maximumExtractedBytes int64 = 1200 * 1024 * 1024
const maximumIndividualFileBytes int64 = 150 * 1024 * 1024
const maximumArchiveEntries = 100000

type GitHubCommit struct {
	SHA string `json:"sha"`
}

func githubHTTPClient() *http.Client { return &http.Client{Timeout: 4 * time.Minute} }
func githubRequest(client *http.Client, urlString string) (*http.Response, error) {
	request, err := http.NewRequest("GET", urlString, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "odoo-addons-go/1.0")
	request.Header.Set("Accept", "application/vnd.github+json")
	// Credentials are only ever sent to api.github.com, never to codeload.github.com.
	if request.URL.Host == "api.github.com" {
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", request.URL.Host+request.URL.Path, err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("GitHub %s: HTTP %d (check branch, public access and API rate limits)", request.URL.Host+request.URL.Path, response.StatusCode)
	}
	return response, nil
}
func resolveBranchSHA(client *http.Client, entry RepositoryEntry) (string, error) {
	// API accepts a branch name as a path component; encode '/' within the name.
	address := "https://api.github.com/repos/" + entry.Repository + "/commits/" + url.PathEscape(entry.Branch)
	response, err := githubRequest(client, address)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var commit GitHubCommit
	if err := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&commit); err != nil {
		return "", err
	}
	if err := validateSHA(commit.SHA, 20); err != nil {
		return "", fmt.Errorf("invalid commit from GitHub for %s: %w", entry.Repository, err)
	}
	return strings.ToLower(commit.SHA), nil
}
func downloadArchive(client *http.Client, entry RepositoryEntry, commitSHA string, temporaryDirectory string) (string, string, error) {
	if err := validateSHA(commitSHA, 20); err != nil {
		return "", "", fmt.Errorf("invalid resolved commit: %w", err)
	}
	address := "https://codeload.github.com/" + entry.Repository + "/tar.gz/" + commitSHA
	response, err := githubRequest(client, address)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	archiveFile, err := os.CreateTemp(temporaryDirectory, ".github-archive-*.tar.gz")
	if err != nil {
		return "", "", err
	}
	archivePath := archiveFile.Name()
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(archiveFile, digest), io.LimitReader(response.Body, maximumArchiveBytes+1))
	closeErr := archiveFile.Close()
	if copyErr != nil || closeErr != nil || written > maximumArchiveBytes {
		os.Remove(archivePath)
		if copyErr != nil {
			return "", "", fmt.Errorf("download archive %s: %w", entry.Repository, copyErr)
		}
		if closeErr != nil {
			return "", "", closeErr
		}
		return "", "", fmt.Errorf("archive %s exceeds %d bytes", entry.Repository, maximumArchiveBytes)
	}
	return archivePath, hex.EncodeToString(digest.Sum(nil)), nil
}

func extractArchiveSafely(sourceArchivePath string, destinationDirectory string) error {
	source, err := os.Open(sourceArchivePath)
	if err != nil {
		return err
	}
	defer source.Close()
	stream, err := gzip.NewReader(source)
	if err != nil {
		return fmt.Errorf("invalid gzip archive: %w", err)
	}
	defer stream.Close()
	reader := tar.NewReader(stream)
	if err := os.MkdirAll(destinationDirectory, 0755); err != nil {
		return err
	}
	rootDirectory := ""
	totalBytes := int64(0)
	fileCount := 0
	extractedFiles := make(map[string]bool)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("corrupt tar archive: %w", err)
		}
		fileCount++
		if fileCount > maximumArchiveEntries {
			return errors.New("too many tar archive entries")
		}
		// Go's archive/tar.Reader.Next returns PAX global headers as entries.
		// They contain metadata, not repository files, and can precede the root
		// directory in GitHub's tar.gz archives (e.g. pax_global_header).
		// Skip only the explicit TAR metadata type, never an ordinary file with
		// the same name. Regular files outside the repository root stay invalid.
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if header.Name == "" || strings.HasPrefix(header.Name, "/") || strings.Contains(header.Name, "\\") {
			return fmt.Errorf("unsafe archive member %q", header.Name)
		}
		raw := strings.TrimSuffix(header.Name, "/")
		sections := strings.SplitN(raw, "/", 2)
		if rootDirectory == "" {
			rootDirectory = sections[0]
		}
		if sections[0] != rootDirectory || sections[0] == "." || sections[0] == ".." {
			return fmt.Errorf("tar archive contains inconsistent roots: %q", header.Name)
		}
		if len(sections) == 1 {
			if header.Typeflag != tar.TypeDir {
				return fmt.Errorf("root is not directory: %s", header.Name)
			}
			continue
		}
		relative := sections[1]
		if relative == "" {
			continue
		}
		for _, component := range strings.Split(relative, "/") {
			if component == "" || component == "." || component == ".." {
				return fmt.Errorf("unsafe archive path %q", header.Name)
			}
		}
		clean := path.Clean(relative)
		if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			return fmt.Errorf("unsafe archive path %q", header.Name)
		}
		destinationPath := filepath.Join(destinationDirectory, filepath.FromSlash(clean))
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(destinationPath, 0755); err != nil {
				return err
			}
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return fmt.Errorf("archive member %q is not a regular file or directory (symlinks forbidden)", header.Name)
		}
		if extractedFiles[clean] {
			return fmt.Errorf("duplicate archive path %q", clean)
		}
		extractedFiles[clean] = true
		if header.Size < 0 || header.Size > maximumIndividualFileBytes {
			return fmt.Errorf("oversized or invalid file %q", header.Name)
		}
		totalBytes += header.Size
		if totalBytes > maximumExtractedBytes {
			return errors.New("extracted archive exceeds total size limit")
		}
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0755); err != nil {
			return err
		}
		destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return err
		}
		copied, copyErr := io.CopyN(destination, reader, header.Size)
		closeErr := destination.Close()
		if copyErr != nil {
			return fmt.Errorf("extract %q: %w", header.Name, copyErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if copied != header.Size {
			return fmt.Errorf("incomplete archive file %q", header.Name)
		}
	}
	if rootDirectory == "" {
		return errors.New("empty tar archive")
	}
	return nil
}

func validateRepositoryHasManifest(destinationRepositoryPath string) error {
	directories, err := os.ReadDir(destinationRepositoryPath)
	if err != nil {
		return err
	}
	for _, candidate := range directories {
		if !candidate.IsDir() {
			continue
		}
		manifest := filepath.Join(destinationRepositoryPath, candidate.Name(), "__manifest__.py")
		stat, err := os.Lstat(manifest)
		if err == nil && stat.Mode().IsRegular() {
			return nil
		}
	}
	return fmt.Errorf("no Odoo addon manifests under %s", destinationRepositoryPath)
}
