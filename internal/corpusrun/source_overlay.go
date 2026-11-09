package corpusrun

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// safeRelative rejects absolute, ambiguous and repository-metadata paths.
func safeRelative(path string) (string, error) {
	if path == "" || strings.Contains(path, `\`) || filepath.IsAbs(path) {
		return "", fmt.Errorf("invalid relative path")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root")
	}
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if part == ".git" || part == depsDir {
			return "", fmt.Errorf("path enters runner or Git metadata")
		}
	}
	return clean, nil
}

func verifyComponentPins(root string, pins map[string]string) error {
	for path, pin := range pins {
		rel, err := safeRelative(path)
		if err != nil {
			return fmt.Errorf("component: %w", err)
		}
		full := filepath.Join(root, rel)
		if err := verifyNoSymlinks(root, rel, true); err != nil {
			return fmt.Errorf("component path: %w", err)
		}
		gitDir, err := os.Lstat(filepath.Join(full, ".git"))
		if err != nil || !gitDir.IsDir() {
			return fmt.Errorf("component has no Git metadata")
		}
		head, err := gitHead(context.Background(), full)
		if err != nil || pin == "" || head != pin {
			return fmt.Errorf("component pin differs")
		}
		st, err := exec.Command("git", "-C", full, "status", "--porcelain", "--untracked-files=all").Output()
		if err != nil || len(st) > 0 {
			return fmt.Errorf("component is not clean")
		}
		ignored, err := exec.Command("git", "-C", full, "ls-files", "--others", "--ignored", "--exclude-standard", "-z").Output()
		if err != nil || len(ignored) > 0 {
			return fmt.Errorf("component contains ignored files")
		}
	}
	return nil
}

// verifyNoSymlinks checks each path component before opening it. For files,
// create missing parent directories but reject existing symlinks and non-dirs.
func verifyNoSymlinks(root, rel string, mustExist bool) error {
	parts := strings.Split(rel, string(filepath.Separator))
	current := root
	for i, part := range parts {
		current = filepath.Join(current, part)
		last := i == len(parts)-1
		st, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if mustExist {
				return err
			}
			if !last {
				if err := os.Mkdir(current, 0755); err != nil {
					return err
				}
			}
			continue
		}
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in path")
		}
		if !last && !st.IsDir() {
			return fmt.Errorf("non-directory in path")
		}
	}
	return nil
}

// applySourceArchive applies a hash-pinned private overlay only to the copied
// project. Component trees and Git metadata cannot be changed by the archive.
func applySourceArchive(root, path, want string, components map[string]string) error {
	if path == "" || len(want) != 64 {
		return fmt.Errorf("archive path or SHA-256 missing")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if fmt.Sprintf("%x", h.Sum(nil)) != want {
		return fmt.Errorf("source archive SHA-256 differs")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	tr := tar.NewReader(f)
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		rel, err := safeRelative(hdr.Name)
		if err != nil {
			return err
		}
		if seen[rel] {
			return fmt.Errorf("duplicate archive path")
		}
		seen[rel] = true
		for component := range components {
			c, err := safeRelative(component)
			if err != nil {
				return err
			}
			if rel == c || strings.HasPrefix(rel, c+string(filepath.Separator)) || hdr.Typeflag != tar.TypeDir && strings.HasPrefix(c, rel+string(filepath.Separator)) {
				return fmt.Errorf("source archive overlaps pinned component")
			}
		}
		if err := verifyNoSymlinks(root, rel, false); err != nil {
			return err
		}
		dest := filepath.Join(root, rel)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, tr)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if err := os.Chmod(dest, 0644|os.FileMode(hdr.Mode)&0111); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry type")
		}
	}
	return nil
}
