package composition

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func hashDirectory(root string) (string, error) {
	hasher := sha256.New()
	if err := writeDirectoryHash(hasher, root); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

func hashDirectories(roots []string) (string, error) {
	normalized := append([]string(nil), roots...)
	sort.Strings(normalized)
	hasher := sha256.New()
	for _, root := range normalized {
		if _, err := io.WriteString(hasher, filepath.ToSlash(filepath.Clean(root))+"\n"); err != nil {
			return "", err
		}
		if err := writeDirectoryHash(hasher, root); err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

func writeDirectoryHash(hasher hash.Hash, root string) error {
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported in composition packages: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return err
	}

	sort.Strings(files)
	for _, path := range files {
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(filepath.Clean(relPath))
		if _, err := io.WriteString(hasher, relPath+"\n"); err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(hasher, file); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		if _, err := io.WriteString(hasher, "\n"); err != nil {
			return err
		}
	}

	return nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported in composition packages: %s", path)
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, relPath)

		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := os.Create(targetPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}

		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		return os.Chmod(targetPath, mode)
	})
}

func extractTarGz(archivePath, dst string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		targetPath, err := secureExtractPath(dst, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}

			out, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tarReader); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}

			mode := os.FileMode(header.Mode)
			if mode == 0 {
				mode = 0o644
			}
			if err := os.Chmod(targetPath, mode); err != nil {
				return err
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("links are not supported in composition archives: %s", header.Name)
		default:
			continue
		}
	}

	return nil
}

func secureExtractPath(root, name string) (string, error) {
	cleanName := filepath.Clean(name)
	if cleanName == "." {
		return root, nil
	}
	cleanName = strings.TrimPrefix(cleanName, string(filepath.Separator))
	targetPath := filepath.Join(root, cleanName)
	cleanRoot := filepath.Clean(root)
	if targetPath != cleanRoot && !strings.HasPrefix(targetPath, cleanRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry escapes destination: %s", name)
	}
	return targetPath, nil
}