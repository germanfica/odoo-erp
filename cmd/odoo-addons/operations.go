package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FetchOptions struct {
	SourceModPath         string
	SourceLockPath        string
	SourceRequiredPath    string
	DestinationAddonsRoot string
	SourceConfigPath      string
	DestinationConfigPath string
	InstalledPrefix       string
	BaseAddonPaths        string
	DryRun                bool
}

func fetch(options FetchOptions) error {
	entries, err := readManifest(options.SourceModPath)
	if err != nil {
		return err
	}
	required, err := readRequirements(options.SourceRequiredPath)
	if err != nil {
		return err
	}
	locked, err := loadOptionalLock(options.SourceLockPath, entries)
	if err != nil {
		return err
	}
	rendered, err := renderConfiguration(options.SourceConfigPath, entries, options.InstalledPrefix, options.BaseAddonPaths)
	if err != nil {
		return err
	}
	if err := rejectSameSourceAndDestination(options.SourceConfigPath, options.DestinationConfigPath); err != nil {
		return err
	}
	if err := validateUnusedDestination(options.DestinationAddonsRoot); err != nil {
		return err
	}
	if err := validateUnusedDestination(options.DestinationConfigPath); err != nil {
		return err
	}
	absoluteAddons, err := filepath.Abs(options.DestinationAddonsRoot)
	if err != nil {
		return err
	}
	absoluteConfig, err := filepath.Abs(options.DestinationConfigPath)
	if err != nil {
		return err
	}
	if strings.HasPrefix(absoluteConfig, absoluteAddons+string(os.PathSeparator)) {
		return errors.New("generated config path cannot be inside addon destination")
	}
	lockMode := "floating branches"
	if locked != nil {
		lockMode = "verified locked commits"
	}
	fmt.Printf("PLAN: fetch %d repositories (%s), check %d required modules\n", len(entries), lockMode, len(required))
	fmt.Printf("PLAN: create %s and %s; source configuration stays unchanged\n", options.DestinationAddonsRoot, options.DestinationConfigPath)
	for _, entry := range entries {
		fmt.Printf("PLAN: %s branch=%s -> %s\n", entry.Repository, entry.Branch, entry.Directory)
	}
	if options.DryRun {
		return nil
	}
	// No build output is touched until the entire download and validation phase succeeds.
	parent := filepath.Dir(options.DestinationAddonsRoot)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".odoo-download-staging-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	stagedAddons := filepath.Join(staging, "addons")
	if err := os.Mkdir(stagedAddons, 0755); err != nil {
		return err
	}
	client := githubHTTPClient()
	var revisions []string
	for i, entry := range entries {
		commitSHA := ""
		expectedChecksum := ""
		if locked != nil {
			commitSHA = locked[i].CommitSHA
			expectedChecksum = locked[i].ArchiveSHA256
		} else {
			commitSHA, err = resolveBranchSHA(client, entry)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", entry.Repository, err)
			}
		}
		fmt.Printf("FETCH: %s %s commit=%s\n", entry.Repository, entry.Branch, commitSHA)
		archivePath, checksum, err := downloadArchive(client, entry, commitSHA, staging)
		if err != nil {
			return err
		}
		if expectedChecksum != "" && checksum != expectedChecksum {
			os.Remove(archivePath)
			return fmt.Errorf("SHA-256 mismatch for %s@%s: expected %s, got %s", entry.Repository, commitSHA, expectedChecksum, checksum)
		}
		destinationRepositoryPath := filepath.Join(stagedAddons, entry.Directory)
		err = extractArchiveSafely(archivePath, destinationRepositoryPath)
		os.Remove(archivePath)
		if err != nil {
			return fmt.Errorf("extract %s: %w", entry.Repository, err)
		}
		if err := validateRepositoryHasManifest(destinationRepositoryPath); err != nil {
			return err
		}
		revisions = append(revisions, fmt.Sprintf("%s %s %s %s", entry.Repository, entry.Branch, commitSHA, checksum))
	}
	if err := validateModuleLocations(stagedAddons, required); err != nil {
		return err
	}
	revisionsText := "# repository branch resolved_commit archive_sha256\n" + strings.Join(revisions, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(stagedAddons, "REVISIONS.txt"), []byte(revisionsText), 0644); err != nil {
		return err
	}
	// Stage configuration separately so it is not generated from unvalidated addons.
	fmt.Println("VALIDATED: archives, manifest files, required modules, hashes and configuration")
	if err := validateUnusedDestination(options.DestinationAddonsRoot); err != nil {
		return err
	}
	if err := validateUnusedDestination(options.DestinationConfigPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(options.DestinationConfigPath), 0755); err != nil {
		return err
	}
	if err := os.Rename(stagedAddons, options.DestinationAddonsRoot); err != nil {
		return err
	}
	if err := writeNewFileAtomically(options.DestinationConfigPath, rendered, 0644); err != nil {
		// Remove only the brand-new generated directory, never any pre-existing data.
		if rollbackErr := os.RemoveAll(options.DestinationAddonsRoot); rollbackErr != nil {
			return fmt.Errorf("config generation: %v; addon rollback: %v", err, rollbackErr)
		}
		return fmt.Errorf("config generation (addon installation rolled back): %w", err)
	}
	fmt.Printf("DONE: generated %s and %s\n", options.DestinationAddonsRoot, options.DestinationConfigPath)
	return nil
}

func validateUnusedDestination(destinationPath string) error {
	if destinationPath == "" {
		return errors.New("destination path cannot be empty")
	}
	stat, err := os.Lstat(destinationPath)
	if err == nil {
		return fmt.Errorf("destination %s already exists (refusing to overwrite %s)", destinationPath, stat.Mode())
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func update(sourceModPath string, destinationLockPath string, sourceRequiredPath string, dryRun bool) error {
	entries, err := readManifest(sourceModPath)
	if err != nil {
		return err
	}
	required, err := readRequirements(sourceRequiredPath)
	if err != nil {
		return err
	}
	modAbs, err := filepath.Abs(sourceModPath)
	if err != nil {
		return err
	}
	lockAbs, err := filepath.Abs(destinationLockPath)
	if err != nil {
		return err
	}
	if modAbs == lockAbs {
		return errors.New("lock destination must not equal addons.mod")
	}
	if stat, err := os.Lstat(destinationLockPath); err == nil {
		if !stat.Mode().IsRegular() {
			return errors.New("lock destination is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	operation := "replace"
	if dryRun {
		operation = "inspect"
	}
	fmt.Printf("PLAN: resolve %d GitHub branches, verify archives and %s addons.lock\n", len(entries), operation)
	client := githubHTTPClient()
	var rows []string
	var staging string
	if !dryRun {
		parent := filepath.Dir(destinationLockPath)
		if err := os.MkdirAll(parent, 0755); err != nil {
			return err
		}
		staging, err = os.MkdirTemp(parent, ".odoo-lock-staging-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(staging)
		if err := os.Mkdir(filepath.Join(staging, "addons"), 0755); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		sha, err := resolveBranchSHA(client, entry)
		if err != nil {
			return err
		}
		fmt.Printf("RESOLVED: %s %s -> %s\n", entry.Repository, entry.Branch, sha)
		if dryRun {
			continue
		}
		archivePath, checksum, err := downloadArchive(client, entry, sha, staging)
		if err != nil {
			return err
		}
		// Validate the archive, not just a checksum of arbitrary bytes.
		extractionPath := filepath.Join(staging, "addons", entry.Directory)
		err = extractArchiveSafely(archivePath, extractionPath)
		os.Remove(archivePath)
		if err != nil {
			return fmt.Errorf("validate %s: %w", entry.Repository, err)
		}
		if err := validateRepositoryHasManifest(extractionPath); err != nil {
			return err
		}
		rows = append(rows, fmt.Sprintf("%s|%s|%s|%s|%s", entry.Repository, entry.Branch, entry.Directory, sha, checksum))
	}
	if dryRun {
		return nil
	}
	if err := validateModuleLocations(filepath.Join(staging, "addons"), required); err != nil {
		return err
	}
	body := []byte("# repository|branch|directory|commit|sha256\n" + strings.Join(rows, "\n") + "\n")
	temporaryFile, err := os.CreateTemp(filepath.Dir(destinationLockPath), ".addons-lock-")
	if err != nil {
		return err
	}
	temporaryPath := temporaryFile.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporaryFile.Write(body); err != nil {
		temporaryFile.Close()
		return err
	}
	if err := temporaryFile.Chmod(0644); err != nil {
		temporaryFile.Close()
		return err
	}
	if err := temporaryFile.Sync(); err != nil {
		temporaryFile.Close()
		return err
	}
	if err := temporaryFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, destinationLockPath); err != nil {
		return err
	}
	fmt.Printf("DONE: updated %s (%d locked repositories)\n", destinationLockPath, len(entries))
	return nil
}
