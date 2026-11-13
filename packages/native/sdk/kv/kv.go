// Package kv is a small key-value store in the app's SQLite database
// (package db, file app.db, table kv). Values are wire values stored as
// frame bytes, so anything the wire format carries round-trips: JSON values,
// bytes, dates, maps and sets.
//
// Keys are strings; Keys(prefix) lists them in byte order.
package kv

import (
	"context"
	"sync"

	"github.com/kratejs/krate/packages/native/sdk/db"
	"github.com/kratejs/krate/packages/native/wire"
)

// Store is a key-value table in a database.
type Store struct {
	d    *db.DB
	once sync.Once
	err  error
}

// New returns a Store on d, creating the kv table on first use.
func New(d *db.DB) *Store { return &Store{d: d} }

var (
	defMu sync.Mutex
	def   *Store
	defDB *db.DB
)

// Default is the Store on db.Default.
func Default() (*Store, error) {
	d, err := db.Default()
	if err != nil {
		return nil, err
	}
	defMu.Lock()
	defer defMu.Unlock()
	if def == nil || defDB != d {
		def, defDB = New(d), d
	}
	return def, nil
}

func (s *Store) init(_ context.Context) error {
	s.once.Do(func() {
		_, s.err = s.d.Exec(context.Background(), "CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY NOT NULL, value BLOB NOT NULL) WITHOUT ROWID")
	})
	return s.err
}

// Set stores frame (a complete wire frame) under key.
func (s *Store) Set(ctx context.Context, key string, frame []byte) error {
	if err := s.init(ctx); err != nil {
		return err
	}
	_, err := s.d.Exec(ctx, "INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, frame)
	return err
}

// Get returns the frame stored under key.
func (s *Store) Get(ctx context.Context, key string) (frame []byte, ok bool, err error) {
	if err := s.init(ctx); err != nil {
		return nil, false, err
	}
	rows, err := s.d.QueryRows(ctx, "SELECT value FROM kv WHERE key = ?", key)
	if err != nil {
		return nil, false, err
	}
	if len(rows.Data) == 0 {
		return nil, false, nil
	}
	b, _ := rows.Data[0][0].([]byte)
	return b, true, nil
}

// SetWith stores the value that enc writes.
func (s *Store) SetWith(ctx context.Context, key string, enc func(*wire.Encoder)) error {
	e := wire.NewEncoder()
	enc(e)
	frame, err := e.Finish()
	if err != nil {
		return err
	}
	return s.Set(ctx, key, frame)
}

// GetWith reads the value under key through dec. It reports false, without
// calling dec, when the key is missing.
func (s *Store) GetWith(ctx context.Context, key string, dec func(*wire.Decoder) error) (bool, error) {
	frame, ok, err := s.Get(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	d, err := wire.NewDecoder(frame)
	if err != nil {
		return true, err
	}
	if err := dec(d); err != nil {
		return true, err
	}
	return true, d.Finish()
}

// SetBytes stores a byte slice.
func (s *Store) SetBytes(ctx context.Context, key string, b []byte) error {
	return s.SetWith(ctx, key, func(e *wire.Encoder) { e.Bytes(b) })
}

// GetBytes returns a byte slice stored with SetBytes.
func (s *Store) GetBytes(ctx context.Context, key string) (b []byte, ok bool, err error) {
	ok, err = s.GetWith(ctx, key, func(d *wire.Decoder) error {
		v, err := d.Bytes()
		b = append([]byte(nil), v...)
		return err
	})
	return b, ok, err
}

// SetString stores a string.
func (s *Store) SetString(ctx context.Context, key, v string) error {
	return s.SetWith(ctx, key, func(e *wire.Encoder) { e.String(v) })
}

// GetString returns a string stored with SetString.
func (s *Store) GetString(ctx context.Context, key string) (v string, ok bool, err error) {
	ok, err = s.GetWith(ctx, key, func(d *wire.Decoder) (err error) { v, err = d.String(); return })
	return v, ok, err
}

// Delete removes key. A missing key is not an error.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := s.init(ctx); err != nil {
		return err
	}
	_, err := s.d.Exec(ctx, "DELETE FROM kv WHERE key = ?", key)
	return err
}

// Keys lists the keys that start with prefix, in byte order.
func (s *Store) Keys(ctx context.Context, prefix string) ([]string, error) {
	if err := s.init(ctx); err != nil {
		return nil, err
	}
	rows, err := s.d.QueryRows(ctx, "SELECT key FROM kv WHERE substr(key, 1, length(?1)) = ?1 ORDER BY key", prefix)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(rows.Data))
	for _, r := range rows.Data {
		k, _ := r[0].(string)
		keys = append(keys, k)
	}
	return keys, nil
}
