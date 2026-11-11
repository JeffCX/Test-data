package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotDecryptsWithOpenSSL(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl is not installed")
	}
	root := t.TempDir()
	for _, dir := range []string{".codex/tmp", "workspace/node_modules/pkg", "workspace/src"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		".codex/config.toml":           "model = 'test'\n",
		".codex/tmp/ignore.txt":        "excluded",
		"workspace/src/app.txt":        "hello snapshot\n",
		"workspace/node_modules/pkg/a": "excluded",
		".bash_history":                "echo fixture\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("src/app.txt", filepath.Join(root, "workspace", "app-link")); err != nil {
		t.Fatal(err)
	}
	r, err := createSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(r.path), "codex-snapshot-") || !strings.HasSuffix(r.path, ".tar.gz.enc") {
		t.Fatalf("unexpected output path %s", r.path)
	}
	info, err := os.Stat(r.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 || info.Size() != r.size {
		t.Fatalf("bad output mode or size: %v, %d", info.Mode(), info.Size())
	}
	ciphertext, err := os.ReadFile(r.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(ciphertext), "Salted__") {
		t.Fatal("missing OpenSSL salted header")
	}
	sum := sha256.Sum256(ciphertext)
	if hex.EncodeToString(sum[:]) != r.sha256 {
		t.Fatal("SHA-256 output does not match file")
	}
	passwordFile := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(passwordFile, []byte(r.password+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	plainArchive := filepath.Join(t.TempDir(), "snapshot.tar.gz")
	cmd := exec.Command("openssl", "enc", "-d", "-aes-256-cbc", "-salt", "-pbkdf2", "-iter", "600000", "-md", "sha256", "-pass", "file:"+passwordFile, "-in", r.path, "-out", plainArchive)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("OpenSSL decrypt: %v: %s", err, output)
	}
	f, err := os.Open(plainArchive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]string{}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			body, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			seen[header.Name] = string(body)
		} else if header.Typeflag == tar.TypeSymlink {
			seen[header.Name] = "symlink:" + header.Linkname
		}
	}
	for name, want := range map[string]string{
		".codex/config.toml":    files[".codex/config.toml"],
		"workspace/src/app.txt": files["workspace/src/app.txt"],
		"workspace/app-link":    "symlink:src/app.txt",
		".bash_history":         files[".bash_history"],
	} {
		if seen[name] != want {
			t.Errorf("%s: got %q, want %q", name, seen[name], want)
		}
	}
	if _, ok := seen[".codex/tmp/ignore.txt"]; ok {
		t.Error("included .codex/tmp")
	}
	if _, ok := seen["workspace/node_modules/pkg/a"]; ok {
		t.Error("included node_modules")
	}
}

func TestMissingRequiredDirectoryCreatesNoOutput(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := createSnapshot(root); err == nil {
		t.Fatal("expected missing workspace error")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != ".codex" {
		t.Fatalf("unexpected files after failure: %v", entries)
	}
}
