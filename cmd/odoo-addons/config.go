package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func renderConfiguration(sourceConfigPath string, entries []RepositoryEntry, installedPrefix string, basePaths string) ([]byte, error) {
	source, err := os.ReadFile(sourceConfigPath)
	if err != nil {
		return nil, fmt.Errorf("read user odoo.conf: %w", err)
	}
	if strings.ContainsRune(string(source), '\x00') {
		return nil, errors.New("odoo.conf contains NUL bytes")
	}
	prefix := filepath.ToSlash(filepath.Clean(installedPrefix))
	if !strings.HasPrefix(prefix, "/") || prefix == "/" {
		return nil, errors.New("installed-prefix must be a non-root absolute path")
	}
	var destinations []string
	for _, raw := range strings.Split(basePaths, ",") {
		p := strings.TrimSpace(raw)
		if p == "" || !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\r\n") {
			return nil, fmt.Errorf("invalid base addon path %q", p)
		}
		destinations = append(destinations, p)
	}
	for _, entry := range entries {
		destinations = append(destinations, prefix+"/"+entry.Directory)
	}
	generatedLine := "addons_path = " + strings.Join(destinations, ",")
	lines := strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n")
	section := ""
	foundSection := false
	replaced := false
	result := make([]string, 0, len(lines)+2)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if section == "options" && !replaced {
				result = append(result, generatedLine)
				replaced = true
			}
			section = strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			if section == "options" {
				foundSection = true
			}
		}
		if section == "options" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, ";") {
			equal := strings.IndexByte(trimmed, '=')
			if equal >= 0 && strings.EqualFold(strings.TrimSpace(trimmed[:equal]), "addons_path") {
				if !replaced {
					result = append(result, generatedLine)
					replaced = true
				}
				continue
			}
		}
		result = append(result, line)
	}
	if !foundSection {
		return nil, errors.New("odoo.conf must contain an [options] section")
	}
	if !replaced {
		result = append(result, generatedLine)
	}
	output := strings.Join(result, "\n")
	if !strings.HasSuffix(output, "\n") {
		output += "\n"
	}
	return []byte(output), nil
}

func rejectSameSourceAndDestination(sourcePath, destinationPath string) error {
	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	destinationAbs, err := filepath.Abs(destinationPath)
	if err != nil {
		return err
	}
	if sourceAbs == destinationAbs {
		return errors.New("refusing to overwrite user-maintained source configuration")
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	destInfo, err := os.Stat(destinationPath)
	if err == nil && os.SameFile(sourceInfo, destInfo) {
		return errors.New("destination and source are the same file")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func writeNewFileAtomically(destinationPath string, contents []byte, mode os.FileMode) error {
	if _, err := os.Lstat(destinationPath); err == nil {
		return fmt.Errorf("destination already exists: %s", destinationPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(destinationPath)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(parent, ".generated-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(contents); err != nil {
		file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// The destination is expected not to exist: not suitable as a replacement API.
	if _, err := os.Lstat(destinationPath); err == nil {
		return fmt.Errorf("destination appeared during write: %s", destinationPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(file.Name(), destinationPath)
}
