package carrier

import (
	"context"

	pgxmock "github.com/pashagolub/pgxmock"
)

// mockAdapter adapts pgxmock.PgxPoolIface to the package DB interface for tests.
// It wraps pgxmock return values into the package-local RowScanner/Rows
// interfaces so tests compiled against pgxmock (which returns pgx/v4-style
// rows/row types) can be used with the production-facing DB abstraction.
type mockAdapter struct{ p pgxmock.PgxPoolIface }

type rowScannerWrapper struct {
	r interface{ Scan(dest ...any) error }
}

func (w *rowScannerWrapper) Scan(dest ...any) error { return w.r.Scan(dest...) }

type rowsWrapper struct {
	r interface {
		Next() bool
		Scan(dest ...any) error
		Close()
	}
}

func (w *rowsWrapper) Next() bool             { return w.r.Next() }
func (w *rowsWrapper) Scan(dest ...any) error { return w.r.Scan(dest...) }
func (w *rowsWrapper) Close()                 { w.r.Close() }

func (m *mockAdapter) QueryRow(ctx context.Context, sql string, args ...any) RowScanner {
	r := m.p.QueryRow(ctx, sql, args...)
	var scanner interface{ Scan(dest ...any) error } = r
	return &rowScannerWrapper{r: scanner}
}

func (m *mockAdapter) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	r, err := m.p.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	var rowsIface interface {
		Next() bool
		Scan(dest ...any) error
		Close()
	} = r
	return &rowsWrapper{r: rowsIface}, nil
}

func (m *mockAdapter) Exec(ctx context.Context, sql string, args ...any) (any, error) {
	res, err := m.p.Exec(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return res, nil
}
