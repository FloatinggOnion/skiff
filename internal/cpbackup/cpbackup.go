// Package cpbackup packs Skiff's own state (its database, keys and registry) into an encrypted bundle, and restores it.
package cpbackup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const magic = "SKIFFCP1"

// Files is what a bundle carries besides the database snapshot, relative to ~/.skiff. Missing ones are skipped.
var Files = []string{"github.json", "srs.key", "visitor.key", "apps.json", "domains.json", "mail.json", "certs", "deploys"}

// DBName is the database's name inside the bundle and in the state dir.
const DBName = "skiff.db"

// ParseKey reads a 64-character hex recovery key.
func ParseKey(s string) ([]byte, error) {
	k, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(k) != 32 {
		return nil, errors.New("the recovery key is 64 hex characters")
	}
	return k, nil
}

func Seal(key, plain []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte(magic), nonce...)
	return gcm.Seal(out, nonce, plain, []byte(magic)), nil
}

func Open(key, sealed []byte) ([]byte, error) {
	if !bytes.HasPrefix(sealed, []byte(magic)) {
		return nil, errors.New("not a Skiff control-plane backup")
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	rest := sealed[len(magic):]
	if len(rest) < gcm.NonceSize() {
		return nil, errors.New("backup is truncated")
	}
	plain, err := gcm.Open(nil, rest[:gcm.NonceSize()], rest[gcm.NonceSize():], []byte(magic))
	if err != nil {
		return nil, errors.New("wrong recovery key, or the backup is damaged")
	}
	return plain, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Pack writes a gzipped tar of the database snapshot plus the state files found under dir.
func Pack(w io.Writer, dir, dbSnapshot string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	if err := addFile(tw, dbSnapshot, DBName); err != nil {
		return err
	}
	for _, name := range Files {
		root := filepath.Join(dir, name)
		info, err := os.Stat(root)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			if err := addFile(tw, root, name); err != nil {
				return err
			}
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(dir, path)
			return addFile(tw, path, filepath.ToSlash(rel))
		})
		if err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func addFile(tw *tar.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime()}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// Unpack writes a bundle's files into dir, refusing any path that would land outside it.
func Unpack(r io.Reader, dir string) ([]string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return names, nil
		}
		if err != nil {
			return names, err
		}
		target := filepath.Join(dir, filepath.FromSlash(h.Name))
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) {
			return names, fmt.Errorf("refusing unsafe path %q in backup", h.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return names, err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return names, err
		}
		_, err = io.Copy(f, io.LimitReader(tr, 4<<30))
		f.Close()
		if err != nil {
			return names, err
		}
		names = append(names, h.Name)
	}
}
