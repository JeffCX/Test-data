package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	defaultRoot = "/home/agent"
	iterations  = 600000
	saltLength  = 8 // OpenSSL enc's default salt length.
)

type result struct {
	path     string
	size     int64
	sha256   string
	password string
}

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: codex-snapshot (no arguments; snapshots /home/agent)")
		os.Exit(2)
	}
	syscall.Umask(0077)
	r, err := createSnapshot(defaultRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snapshot failed:", err)
		os.Exit(1)
	}
	fmt.Printf("FILE=%s\nBYTES=%d\n%s  %s\nPASSWORD=%s\n", r.path, r.size, r.sha256, r.path, r.password)
}

func createSnapshot(root string) (result, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return result{}, err
	}
	for _, name := range []string{".codex", "workspace"} {
		info, statErr := os.Lstat(filepath.Join(root, name))
		if statErr != nil {
			return result{}, fmt.Errorf("required directory %s: %w", name, statErr)
		}
		if !info.IsDir() {
			return result{}, fmt.Errorf("required path %s is not a directory", name)
		}
	}

	var nameRandom [4]byte
	var passwordRandom [32]byte
	var salt [saltLength]byte
	if _, err := rand.Read(nameRandom[:]); err != nil {
		return result{}, err
	}
	if _, err := rand.Read(passwordRandom[:]); err != nil {
		return result{}, err
	}
	if _, err := rand.Read(salt[:]); err != nil {
		return result{}, err
	}
	password := hex.EncodeToString(passwordRandom[:])
	name := fmt.Sprintf("codex-snapshot-%s-%s.tar.gz.enc",
		time.Now().UTC().Format("20060102T150405Z"), hex.EncodeToString(nameRandom[:]))
	outPath := filepath.Join(root, name)

	derived, err := pbkdf2.Key(sha256.New, password, salt[:], iterations, 48)
	if err != nil {
		return result{}, err
	}
	defer clear(derived)
	block, err := aes.NewCipher(derived[:32])
	if err != nil {
		return result{}, err
	}

	tmp, err := os.CreateTemp(root, ".codex-snapshot-*.partial")
	if err != nil {
		return result{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()
	if err := tmp.Chmod(0600); err != nil {
		return result{}, err
	}

	digest := sha256.New()
	written := io.MultiWriter(tmp, digest)
	if _, err := written.Write(append([]byte("Salted__"), salt[:]...)); err != nil {
		return result{}, err
	}
	encrypted := &cbcWriter{out: written, mode: cipher.NewCBCEncrypter(block, derived[32:48])}
	compressed := gzip.NewWriter(encrypted)
	archive := tar.NewWriter(compressed)
	if err := writeArchive(archive, root); err != nil {
		return result{}, err
	}
	if err := archive.Close(); err != nil {
		return result{}, err
	}
	if err := compressed.Close(); err != nil {
		return result{}, err
	}
	if err := encrypted.Close(); err != nil {
		return result{}, err
	}
	if err := tmp.Sync(); err != nil {
		return result{}, err
	}
	info, err := tmp.Stat()
	if err != nil {
		return result{}, err
	}
	if err := tmp.Close(); err != nil {
		return result{}, err
	}
	// The temporary and final files are in the same directory. Link fails if the
	// final name already exists, so concurrent runs cannot overwrite a snapshot.
	if err := os.Link(tmpPath, outPath); err != nil {
		return result{}, err
	}
	return result{
		path:     outPath,
		size:     info.Size(),
		sha256:   hex.EncodeToString(digest.Sum(nil)),
		password: password,
	}, nil
}

func writeArchive(w *tar.Writer, root string) error {
	for _, name := range []string{".codex", "workspace", ".bash_history"} {
		path := filepath.Join(root, name)
		if name == ".bash_history" {
			if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return fmt.Errorf("inspect %s: %w", name, err)
			}
		}
		if err := filepath.WalkDir(path, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if rel == ".codex/tmp" || entry.Name() == "node_modules" {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("stat %s: %w", rel, err)
			}
			if info.Mode().IsRegular() {
				return addRegularFile(w, path, rel, info)
			}
			if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				// tar cannot safely read a live socket or FIFO as a regular file.
				fmt.Fprintf(os.Stderr, "skipping special file %s\n", rel)
				return nil
			}
			link := ""
			if info.Mode()&os.ModeSymlink != 0 {
				link, err = os.Readlink(path)
				if err != nil {
					return fmt.Errorf("read link %s: %w", rel, err)
				}
			}
			header, err := tar.FileInfoHeader(info, link)
			if err != nil {
				return fmt.Errorf("header %s: %w", rel, err)
			}
			header.Name = rel
			if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
				header.Name += "/"
			}
			return w.WriteHeader(header)
		}); err != nil {
			return fmt.Errorf("archive %s: %w", name, err)
		}
	}
	return nil
}

func addRegularFile(w *tar.Writer, path, rel string, info os.FileInfo) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", rel, err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", rel, err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return fmt.Errorf("%s changed while archiving", rel)
	}
	header, err := tar.FileInfoHeader(openedInfo, "")
	if err != nil {
		return fmt.Errorf("header %s: %w", rel, err)
	}
	header.Name = rel
	if err := w.WriteHeader(header); err != nil {
		return fmt.Errorf("write header %s: %w", rel, err)
	}
	if _, err := io.CopyN(w, file, openedInfo.Size()); err != nil {
		return fmt.Errorf("read %s: %w", rel, err)
	}
	afterInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", rel, err)
	}
	if afterInfo.Size() != openedInfo.Size() || !afterInfo.ModTime().Equal(openedInfo.ModTime()) {
		return fmt.Errorf("%s changed while archiving", rel)
	}
	return nil
}

type cbcWriter struct {
	out     io.Writer
	mode    cipher.BlockMode
	pending []byte
	closed  bool
}

func (w *cbcWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("write after close")
	}
	original := len(p)
	if len(w.pending) > 0 {
		take := min(aes.BlockSize-len(w.pending), len(p))
		w.pending = append(w.pending, p[:take]...)
		p = p[take:]
		if len(w.pending) == aes.BlockSize {
			w.mode.CryptBlocks(w.pending, w.pending)
			if err := writeFull(w.out, w.pending); err != nil {
				return 0, err
			}
			w.pending = w.pending[:0]
		}
	}
	if full := len(p) / aes.BlockSize * aes.BlockSize; full > 0 {
		buf := append([]byte(nil), p[:full]...)
		w.mode.CryptBlocks(buf, buf)
		if err := writeFull(w.out, buf); err != nil {
			return 0, err
		}
		p = p[full:]
	}
	w.pending = append(w.pending, p...)
	return original, nil
}

func (w *cbcWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	padding := aes.BlockSize - len(w.pending)
	for len(w.pending) < aes.BlockSize {
		w.pending = append(w.pending, byte(padding))
	}
	w.mode.CryptBlocks(w.pending, w.pending)
	return writeFull(w.out, w.pending)
}

func writeFull(w io.Writer, p []byte) error {
	n, err := w.Write(p)
	if err != nil {
		return err
	}
	if n != len(p) {
		return io.ErrShortWrite
	}
	return nil
}
