package db

import (
	"fmt"
	"math"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/glue"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

// Register exposes the database to the UI as krate/db.* ids on Default.
func Register(r *scheme.Registry, set permission.Set) {
	RegisterWith(r, set, Default)
}

// RegisterWith is Register with a custom way to get the database (tests).
func RegisterWith(r *scheme.Registry, set permission.Set, get func() (*DB, error)) {
	permission.Register(r, set, "krate/db.exec", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		const id = "krate/db.exec"
		query, params, err := readStatement(id, args)
		if err != nil {
			return err
		}
		d, err := get()
		if err != nil {
			return err
		}
		res, err := d.Exec(ctx, query, params...)
		if err != nil {
			return err
		}
		encodeResult(out, res)
		return nil
	})

	permission.Register(r, set, "krate/db.query", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		const id = "krate/db.query"
		query, params, err := readStatement(id, args)
		if err != nil {
			return err
		}
		d, err := get()
		if err != nil {
			return err
		}
		rows, err := d.QueryRows(ctx, query, params...)
		if err != nil {
			return err
		}
		out.BeginArray()
		for _, row := range rows.Data {
			out.BeginObject()
			for i, c := range rows.Columns {
				out.Key(c)
				encodeValue(out, row[i])
			}
			out.EndObject()
		}
		out.EndArray()
		return nil
	})

	permission.Register(r, set, "krate/db.transaction", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		const id = "krate/db.transaction"
		a, err := glue.Open(id, args)
		if err != nil {
			return err
		}
		if err := a.Next("statements"); err != nil {
			return err
		}
		d := a.Decoder()
		type stmt struct {
			sql    string
			params []any
		}
		var stmts []stmt
		if err := d.BeginArray(); err != nil {
			return a.Fail("statements", err)
		}
		for d.More() {
			var s stmt
			if err := d.BeginObject(); err != nil {
				return a.Fail("statements", err)
			}
			for d.More() {
				k, err := d.Key()
				if err != nil {
					return a.Fail("statements", err)
				}
				switch k {
				case "sql":
					s.sql, err = d.String()
				case "params":
					s.params, err = readParams(d)
				default:
					err = d.Skip()
				}
				if err != nil {
					return a.Fail("statements", err)
				}
			}
			if err := d.EndObject(); err != nil {
				return a.Fail("statements", err)
			}
			if err := CheckUISQL(s.sql); err != nil {
				return err
			}
			stmts = append(stmts, s)
		}
		if err := d.EndArray(); err != nil {
			return a.Fail("statements", err)
		}
		if err := a.Done(); err != nil {
			return err
		}
		h, err := get()
		if err != nil {
			return err
		}
		results := make([]Result, 0, len(stmts))
		err = h.Transaction(ctx, func(tx *Tx) error {
			for i, s := range stmts {
				r, err := tx.Exec(ctx, s.sql, s.params...)
				if err != nil {
					return krate.Errorf(codeOf(err), "statement %d: %w", i, err)
				}
				results = append(results, r)
			}
			return nil
		})
		if err != nil {
			return err
		}
		out.BeginArray()
		for _, r := range results {
			encodeResult(out, r)
		}
		out.EndArray()
		return nil
	})
}

// codeOf returns the krate code of err, or "sql_error".
func codeOf(err error) string {
	if ke, ok := err.(*krate.Error); ok {
		return ke.Code
	}
	return "sql_error"
}

// readStatement reads [sql, params?] and applies the UI SQL check.
func readStatement(id string, args *wire.Decoder) (string, []any, error) {
	a, err := glue.Open(id, args)
	if err != nil {
		return "", nil, err
	}
	q, err := a.String("sql")
	if err != nil {
		return "", nil, err
	}
	var params []any
	if a.More() {
		if err := a.Next("params"); err != nil {
			return "", nil, err
		}
		if a.Decoder().IsNull() {
			params = nil
		} else if params, err = readParams(a.Decoder()); err != nil {
			return "", nil, a.Fail("params", err)
		}
	}
	if err := a.Done(); err != nil {
		return "", nil, err
	}
	if err := CheckUISQL(q); err != nil {
		return "", nil, err
	}
	return q, params, nil
}

func readParams(d *wire.Decoder) ([]any, error) {
	if err := d.BeginArray(); err != nil {
		return nil, err
	}
	var out []any
	for d.More() {
		v, err := glue.Scalar(d)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, d.EndArray()
}

func encodeResult(e *wire.Encoder, r Result) {
	e.BeginObject()
	e.Key("rowsAffected")
	e.Int(r.RowsAffected)
	e.Key("lastInsertId")
	e.Int(r.LastInsertID)
	e.EndObject()
}

func encodeValue(e *wire.Encoder, v any) {
	switch x := v.(type) {
	case nil:
		e.Null()
	case int64:
		e.Int(x)
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			e.Null()
			return
		}
		e.Float(x)
	case string:
		e.String(x)
	case []byte:
		e.Bytes(x)
	case bool:
		e.Bool(x)
	case time.Time:
		e.Time(x)
	default:
		e.String(fmt.Sprint(v))
	}
}
