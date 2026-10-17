package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
)

type RepositoryEntry struct {
	Repository string
	Branch     string
	Directory  string
}
type LockedEntry struct {
	RepositoryEntry
	CommitSHA     string
	ArchiveSHA256 string
}

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var branchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var directoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var modulePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_]*$`)

func readManifest(sourceModPath string) ([]RepositoryEntry, error) {
	file, err := os.Open(sourceModPath)
	if err != nil {
		return nil, fmt.Errorf("open addons.mod %s: %w", sourceModPath, err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var entries []RepositoryEntry
	repositories := make(map[string]bool)
	directories := make(map[string]bool)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 3 {
			return nil, fmt.Errorf("%s:%d: expected repository|branch|directory", sourceModPath, lineNumber)
		}
		entry := RepositoryEntry{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])}
		if !repositoryPattern.MatchString(entry.Repository) || strings.Contains(entry.Repository, "..") {
			return nil, fmt.Errorf("%s:%d: invalid GitHub repository %q", sourceModPath, lineNumber, entry.Repository)
		}
		if !branchPattern.MatchString(entry.Branch) || strings.Contains(entry.Branch, "..") || strings.Contains(entry.Branch, "//") || strings.HasSuffix(entry.Branch, "/") || strings.HasSuffix(entry.Branch, ".lock") {
			return nil, fmt.Errorf("%s:%d: invalid branch %q", sourceModPath, lineNumber, entry.Branch)
		}
		if !directoryPattern.MatchString(entry.Directory) || entry.Directory == "." || entry.Directory == ".." {
			return nil, fmt.Errorf("%s:%d: invalid directory %q", sourceModPath, lineNumber, entry.Directory)
		}
		if repositories[strings.ToLower(entry.Repository)] || directories[entry.Directory] {
			return nil, fmt.Errorf("%s:%d: duplicate repository or destination directory", sourceModPath, lineNumber)
		}
		repositories[strings.ToLower(entry.Repository)] = true
		directories[entry.Directory] = true
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("addons.mod does not declare any repositories")
	}
	return entries, nil
}

func readRequirements(requiredPath string) ([]string, error) {
	file, err := os.Open(requiredPath)
	if err != nil {
		return nil, fmt.Errorf("open required addons file %s: %w", requiredPath, err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var modules []string
	known := make(map[string]bool)
	for number := 1; scanner.Scan(); number++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !modulePattern.MatchString(line) {
			return nil, fmt.Errorf("%s:%d: invalid module name %q", requiredPath, number, line)
		}
		if known[line] {
			return nil, fmt.Errorf("%s:%d: duplicate module %q", requiredPath, number, line)
		}
		known[line] = true
		modules = append(modules, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return modules, nil
}

func validateSHA(value string, byteCount int) error {
	if len(value) != byteCount*2 {
		return fmt.Errorf("expected %d hex characters", byteCount*2)
	}
	_, err := hex.DecodeString(value)
	return err
}

func loadOptionalLock(lockPath string, entries []RepositoryEntry) ([]LockedEntry, error) {
	file, err := os.Open(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open addons.lock: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	locked := make([]LockedEntry, 0, len(entries))
	for number := 1; scanner.Scan(); number++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 5 {
			return nil, fmt.Errorf("%s:%d: expected repository|branch|directory|commit|sha256", lockPath, number)
		}
		index := len(locked)
		if index >= len(entries) {
			return nil, fmt.Errorf("%s:%d: lock has more entries than addons.mod", lockPath, number)
		}
		source := entries[index]
		if parts[0] != source.Repository || parts[1] != source.Branch || parts[2] != source.Directory {
			return nil, fmt.Errorf("%s:%d: lock entry does not match addons.mod entry %d", lockPath, number, index+1)
		}
		sha := strings.ToLower(parts[3])
		checksum := strings.ToLower(parts[4])
		if err := validateSHA(sha, 20); err != nil {
			return nil, fmt.Errorf("%s:%d: invalid commit: %w", lockPath, number, err)
		}
		if err := validateSHA(checksum, 32); err != nil {
			return nil, fmt.Errorf("%s:%d: invalid archive SHA-256: %w", lockPath, number, err)
		}
		locked = append(locked, LockedEntry{source, sha, checksum})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(locked) != len(entries) {
		return nil, fmt.Errorf("addons.lock has %d entries but addons.mod has %d; update the lock or remove it", len(locked), len(entries))
	}
	return locked, nil
}

func validateModuleLocations(destinationRoot string, required []string) error {
	for _, module := range required {
		entries, err := os.ReadDir(destinationRoot)
		if err != nil {
			return err
		}
		found := false
		for _, repo := range entries {
			if !repo.IsDir() {
				continue
			}
			p := path.Join(destinationRoot, repo.Name(), module, "__manifest__.py")
			info, err := os.Lstat(p)
			if err == nil && info.Mode().IsRegular() {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("mandatory Odoo module missing or lacks __manifest__.py: %s", module)
		}
	}
	return nil
}
