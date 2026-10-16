package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, name string, body string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}
func makeTestPaths(t *testing.T) (string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	mod := filepath.Join(root, "addons.mod")
	required := filepath.Join(root, "addons.required")
	userConfig := filepath.Join(root, "odoo.conf")
	writeTestFile(t, mod, "# repo|branch|name\nOCA/web|18.0|web\n")
	writeTestFile(t, required, "# modules\nweb_responsive\n")
	writeTestFile(t, userConfig, "[options]\nproxy_mode = True\n; addons_path is auto-generated, overwriting any existing value\naddons_path = /custom/ignored\nhttp_port = 8069\n[other]\naddons_path = keep_here\n")
	return root, mod, required, userConfig
}
func TestManifestValidation(t *testing.T) {
	root, mod, _, _ := makeTestPaths(t)
	entries, err := readManifest(mod)
	if err != nil || len(entries) != 1 {
		t.Fatalf("unexpected %v %v", entries, err)
	}
	writeTestFile(t, mod, "OCA/web|18.0|web\nOCA/other|18.0|web\n")
	if _, err := readManifest(mod); err == nil {
		t.Fatal("duplicate directory accepted")
	}
	writeTestFile(t, mod, "OCA/web|18.0|../outside\n")
	if _, err := readManifest(mod); err == nil {
		t.Fatal("path traversal accepted")
	}
	writeTestFile(t, mod, "OCA/web|18.0|web\n")
	if _, err := readManifest(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing manifest accepted")
	}
}
func TestOptionalLockAndConsistency(t *testing.T) {
	root, mod, _, _ := makeTestPaths(t)
	entries, _ := readManifest(mod)
	lock := filepath.Join(root, "addons.lock")
	result, err := loadOptionalLock(lock, entries)
	if err != nil || result != nil {
		t.Fatalf("optional lock not optional: %v", err)
	}
	writeTestFile(t, lock, "# header only\n")
	if _, err := loadOptionalLock(lock, entries); err == nil {
		t.Fatal("empty lock accepted")
	}
	writeTestFile(t, lock, "OCA/web|18.0|web|"+strings.Repeat("a", 40)+"|"+strings.Repeat("b", 64)+"\n")
	result, err = loadOptionalLock(lock, entries)
	if err != nil || len(result) != 1 {
		t.Fatalf("correct lock rejected: %v", err)
	}
	writeTestFile(t, lock, "OCA/web|19.0|web|"+strings.Repeat("a", 40)+"|"+strings.Repeat("b", 64)+"\n")
	if _, err := loadOptionalLock(lock, entries); err == nil {
		t.Fatal("mismatched branch accepted")
	}
}
func TestGeneratedConfigPreservesUserFileAndOtherValues(t *testing.T) {
	_, mod, _, sourceConfig := makeTestPaths(t)
	entries, _ := readManifest(mod)
	original, err := os.ReadFile(sourceConfig)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := renderConfiguration(sourceConfig, entries, "/opt/odoo-addons", "/core,/mnt/extra-addons")
	if err != nil {
		t.Fatal(err)
	}
	text := string(rendered)
	if strings.Count(text, "addons_path = ") != 2 {
		t.Fatalf("expected one options line and one other section line: %s", text)
	}
	if !strings.Contains(text, "addons_path = /core,/mnt/extra-addons,/opt/odoo-addons/web") {
		t.Fatal(text)
	}
	if strings.Contains(text, "addons_path = /custom/ignored") {
		t.Fatal("existing addons_path not overwritten")
	}
	if !strings.Contains(text, "proxy_mode = True") || !strings.Contains(text, "http_port = 8069") || !strings.Contains(text, "addons_path = keep_here") {
		t.Fatal("existing options lost")
	}
	after, _ := os.ReadFile(sourceConfig)
	if !bytes.Equal(original, after) {
		t.Fatal("source configuration mutated")
	}
	if _, err := renderConfiguration(sourceConfig, entries, "relative", "/core"); err == nil {
		t.Fatal("relative prefix accepted")
	}
}
func TestSafeTarExtractionAndRequiredModules(t *testing.T) {
	root, _, required, _ := makeTestPaths(t)
	goodTar := filepath.Join(root, "good.tar.gz")
	createTestArchive(t, goodTar, []tar.Header{{Name: "web-hash/", Typeflag: tar.TypeDir}, {Name: "web-hash/web_responsive/__manifest__.py", Typeflag: tar.TypeReg, Size: 2}}, "{}")
	dest := filepath.Join(root, "extracted")
	if err := extractArchiveSafely(goodTar, filepath.Join(dest, "web")); err != nil {
		t.Fatal(err)
	}
	requirements, err := readRequirements(required)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateModuleLocations(dest, requirements); err != nil {
		t.Fatal(err)
	}
	badTar := filepath.Join(root, "bad.tar.gz")
	createTestArchive(t, badTar, []tar.Header{{Name: "web-hash/", Typeflag: tar.TypeDir}, {Name: "web-hash/../../escape", Typeflag: tar.TypeReg, Size: 2}}, "{}")
	if err := extractArchiveSafely(badTar, filepath.Join(root, "rejected")); err == nil {
		t.Fatal("unsafe traversal accepted")
	}
	linkTar := filepath.Join(root, "link.tar.gz")
	createTestArchive(t, linkTar, []tar.Header{{Name: "web-hash/", Typeflag: tar.TypeDir}, {Name: "web-hash/unsafe", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}}, "")
	if err := extractArchiveSafely(linkTar, filepath.Join(root, "rejected-link")); err == nil {
		t.Fatal("symlink accepted")
	}
}
func createTestArchive(t *testing.T, target string, headers []tar.Header, contents string) {
	t.Helper()
	f, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, header := range headers {
		h := header
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(contents)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestFetchDryRunDoesNotWrite(t *testing.T) {
	root, mod, required, source := makeTestPaths(t)
	dest := filepath.Join(root, "addons")
	confDest := filepath.Join(root, "generated.conf")
	err := fetch(FetchOptions{mod, filepath.Join(root, "addons.lock"), required, dest, source, confDest, "/opt/odoo-addons", "/core,/mnt/extra-addons", true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("dry run wrote addons")
	}
	if _, err := os.Stat(confDest); !os.IsNotExist(err) {
		t.Fatal("dry run wrote config")
	}
	if err := rejectSameSourceAndDestination(source, source); err == nil {
		t.Fatal("refused to detect source overwrite")
	}
	writeTestFile(t, filepath.Join(root, "addons.lock"), "# invalid/empty lock\n")
	if err := fetch(FetchOptions{mod, filepath.Join(root, "addons.lock"), required, dest, source, confDest, "/opt/odoo-addons", "/core,/mnt/extra-addons", true}); err == nil {
		t.Fatal("invalid lock accepted")
	}
}
