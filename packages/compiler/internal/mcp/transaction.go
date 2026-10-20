package mcp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
)

const (
	defaultMaxFileBytes  = 2 * 1024 * 1024
	defaultMaxBatchFiles = 50
)

type txFile struct {
	Path     string      `json:"path"`
	Original string      `json:"original,omitempty"`
	New      string      `json:"new,omitempty"`
	Existed  bool        `json:"existed"`
	Delete   bool        `json:"delete,omitempty"`
	Mode     os.FileMode `json:"mode,omitempty"`
}

type txMeta struct {
	ID      string   `json:"id"`
	Created string   `json:"created"`
	Undone  bool     `json:"undone,omitempty"`
	Files   []txFile `json:"files"`
}

type fileTransaction struct {
	svc   *Service
	id    string
	files map[string]*txFile
	order []string
}

func newFileTransaction(svc *Service) *fileTransaction {
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	id := time.Now().UTC().Format("20060102T150405.000000000") + "-" + hex.EncodeToString(suffix[:])
	return &fileTransaction{svc: svc, id: id, files: map[string]*txFile{}}
}

func (t *fileTransaction) maxFileBytes() int64 {
	if t.svc != nil && t.svc.policy != nil && t.svc.policy.MaxFileBytes > 0 {
		return t.svc.policy.MaxFileBytes
	}
	return defaultMaxFileBytes
}

func (t *fileTransaction) maxBatchFiles() int {
	if t.svc != nil && t.svc.policy != nil && t.svc.policy.MaxBatchFiles > 0 {
		return t.svc.policy.MaxBatchFiles
	}
	return defaultMaxBatchFiles
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// stageWrite loads a file and records a replacement. expected, when non-empty,
// is the SHA-256 of the content the caller read.
func (t *fileTransaction) stageWrite(abs, rel, content, expected string) error {
	rel = normalizeRel(rel)
	if _, exists := t.files[rel]; exists {
		return fmt.Errorf("%s is already part of this transaction", rel)
	}
	data, err := os.ReadFile(abs)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", rel, err)
	}
	original := string(data)
	if expected != "" && hashString(original) != expected {
		return fmt.Errorf("%s changed since it was read; refresh and retry", rel)
	}
	if int64(len(content)) > t.maxFileBytes() {
		return fmt.Errorf("%s is larger than the %d-byte limit", rel, t.maxFileBytes())
	}
	mode := os.FileMode(0644)
	if existed {
		if info, statErr := os.Stat(abs); statErr == nil {
			mode = info.Mode().Perm()
		}
	}
	t.files[rel] = &txFile{Path: rel, Original: original, New: content, Existed: existed, Mode: mode}
	t.order = append(t.order, rel)
	return nil
}

func (t *fileTransaction) stageDelete(abs, rel, expected string) error {
	rel = normalizeRel(rel)
	if _, exists := t.files[rel]; exists {
		return fmt.Errorf("%s is already part of this transaction", rel)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", rel, err)
	}
	if expected != "" && hashString(string(data)) != expected {
		return fmt.Errorf("%s changed since it was read; refresh and retry", rel)
	}
	mode := os.FileMode(0644)
	if info, statErr := os.Stat(abs); statErr == nil {
		mode = info.Mode().Perm()
	}
	t.files[rel] = &txFile{Path: rel, Original: string(data), Existed: true, Delete: true, Mode: mode}
	t.order = append(t.order, rel)
	return nil
}

func (t *fileTransaction) diff() string {
	var parts []string
	for _, rel := range t.order {
		f := t.files[rel]
		newContent := f.New
		if f.Delete {
			newContent = ""
		}
		parts = append(parts, unifiedDiff(rel, f.Original, newContent))
	}
	return strings.Join(parts, "\n")
}

func (t *fileTransaction) count() int { return len(t.files) }

func (t *fileTransaction) commit() error {
	if len(t.files) == 0 {
		return fmt.Errorf("no changes")
	}
	if len(t.files) > t.maxBatchFiles() {
		return fmt.Errorf("transaction has %d files; limit is %d", len(t.files), t.maxBatchFiles())
	}

	txDir := filepath.Join(t.svc.root, ".krate", "mcp", "transactions", t.id)
	if err := os.MkdirAll(filepath.Join(txDir, "stage"), 0755); err != nil {
		return fmt.Errorf("creating transaction directory: %w", err)
	}

	absPaths := make([]string, 0, len(t.order))
	for _, rel := range t.order {
		abs := filepath.Join(t.svc.root, filepath.FromSlash(rel))
		absPaths = append(absPaths, abs)
		f := t.files[rel]
		// Keep a real backup on disk so crash recovery and undo do not depend on
		// this process still being alive.
		if f.Existed {
			if err := os.WriteFile(filepath.Join(txDir, "backup-"+hashString(rel)), []byte(f.Original), f.Mode); err != nil {
				return fmt.Errorf("backing up %s: %w", rel, err)
			}
		}
		if !f.Delete {
			stage := filepath.Join(txDir, "stage", hashString(rel))
			if err := os.WriteFile(stage, []byte(f.New), f.Mode); err != nil {
				return fmt.Errorf("staging %s: %w", rel, err)
			}
		}
	}

	meta := txMeta{ID: t.id, Created: time.Now().UTC().Format(time.RFC3339Nano)}
	for _, rel := range t.order {
		meta.Files = append(meta.Files, *t.files[rel])
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	metaPath := filepath.Join(txDir, "transaction.json")
	if err := os.WriteFile(metaPath, metaBytes, 0644); err != nil {
		return fmt.Errorf("writing transaction metadata: %w", err)
	}

	rollback := func() {
		for i := len(t.order) - 1; i >= 0; i-- {
			rel := t.order[i]
			f := t.files[rel]
			abs := absPaths[i]
			if f.Existed {
				_ = os.MkdirAll(filepath.Dir(abs), 0755)
				_ = os.WriteFile(abs, []byte(f.Original), f.Mode)
			} else {
				_ = os.Remove(abs)
			}
		}
	}

	for i, rel := range t.order {
		abs := absPaths[i]
		f := t.files[rel]
		if f.Delete {
			if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
				rollback()
				return fmt.Errorf("deleting %s: %w", rel, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			rollback()
			return fmt.Errorf("creating the directory for %s: %w", rel, err)
		}
		stage := filepath.Join(txDir, "stage", hashString(rel))
		// Windows does not replace an existing destination with Rename.
		_ = os.Remove(abs)
		if err := os.Rename(stage, abs); err != nil {
			rollback()
			return fmt.Errorf("committing %s: %w", rel, err)
		}
	}
	t.svc.notifyChangedResources()
	return nil
}

func (s *Service) transactionsDir() string {
	return filepath.Join(s.root, ".krate", "mcp", "transactions")
}

func (s *Service) latestTransaction() (*txMeta, string, error) {
	entries, err := os.ReadDir(s.transactionsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("no transactions to undo")
		}
		return nil, "", err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, name := range dirs {
		dir := filepath.Join(s.transactionsDir(), name)
		data, err := os.ReadFile(filepath.Join(dir, "transaction.json"))
		if err != nil {
			continue
		}
		var meta txMeta
		if json.Unmarshal(data, &meta) != nil || meta.Undone {
			continue
		}
		return &meta, dir, nil
	}
	return nil, "", fmt.Errorf("no transactions to undo")
}

// undoLatest restores the most recent transaction. It refuses if any file was
// modified after the transaction, unless force is set.
func (s *Service) undoLatest(force bool) (string, error) {
	meta, dir, err := s.latestTransaction()
	if err != nil {
		return "", err
	}
	return s.undoTransaction(meta, dir, force)
}

func (s *Service) undoByID(id string, force bool) (string, error) {
	dir := filepath.Join(s.transactionsDir(), filepath.Base(id))
	data, err := os.ReadFile(filepath.Join(dir, "transaction.json"))
	if err != nil {
		return "", fmt.Errorf("transaction not found: %s", id)
	}
	var meta txMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("invalid transaction metadata: %w", err)
	}
	if meta.Undone {
		return "", fmt.Errorf("transaction %s was already undone", id)
	}
	return s.undoTransaction(&meta, dir, force)
}

func (s *Service) undoTransaction(meta *txMeta, dir string, force bool) (string, error) {
	files := meta.Files
	// Validate first so a conflict never leaves a half-undone transaction.
	for _, f := range files {
		abs := filepath.Join(s.root, filepath.FromSlash(f.Path))
		data, readErr := os.ReadFile(abs)
		if f.Delete {
			if readErr == nil {
				if !force {
					return "", fmt.Errorf("%s was recreated after the transaction; pass force to undo", f.Path)
				}
			}
			continue
		}
		if readErr != nil {
			if os.IsNotExist(readErr) && !force {
				return "", fmt.Errorf("%s is missing; pass force to undo", f.Path)
			}
			continue
		}
		if !force && hashString(string(data)) != hashString(f.New) {
			return "", fmt.Errorf("%s changed after the transaction; pass force to undo", f.Path)
		}
	}
	for _, f := range files {
		abs := filepath.Join(s.root, filepath.FromSlash(f.Path))
		if f.Existed {
			if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
				return "", err
			}
			if err := os.WriteFile(abs, []byte(f.Original), f.Mode); err != nil {
				return "", fmt.Errorf("restoring %s: %w", f.Path, err)
			}
		} else {
			_ = os.Remove(abs)
		}
	}
	meta.Undone = true
	if data, err := json.MarshalIndent(meta, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "transaction.json"), data, 0644)
	}
	return meta.ID, nil
}

func validateGoSource(rel, content string) error {
	if !isEditableSourceExt(rel) {
		return nil
	}
	if _, perrs, _ := astprint.Parse(content); len(perrs) > 0 {
		msgs := make([]string, 0, len(perrs))
		for _, e := range perrs {
			msgs = append(msgs, e.Error())
		}
		return fmt.Errorf("refusing to write %s: it would not parse after the edit:\n%s", rel, strings.Join(msgs, "\n"))
	}
	return nil
}
