package harness

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"aisdlc/internal/run"
)

const dumpSQL = `SELECT c.table_name, c.column_name,
  CASE WHEN c.data_type IN ('USER-DEFINED','ARRAY') THEN upper(c.udt_name) ELSE upper(c.data_type) END,
  c.is_nullable
FROM information_schema.columns c
JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
WHERE c.table_schema = 'public' AND t.table_type = 'BASE TABLE' AND c.table_name <> 'goose_db_version'`

// DumpSchema chụp các bảng trong schema public (bỏ goose_db_version): "<column>:<TYPE>:NULL|NOT NULL".
func DumpSchema(ctx context.Context, url string) (run.SchemaSnapshot, error) {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return run.SchemaSnapshot{}, err
	}
	defer conn.Close(context.WithoutCancel(ctx))
	rows, err := conn.Query(ctx, dumpSQL)
	if err != nil {
		return run.SchemaSnapshot{}, err
	}
	defer rows.Close()
	tables := map[string][]string{}
	for rows.Next() {
		var table, col, typ, nullable string
		if err := rows.Scan(&table, &col, &typ, &nullable); err != nil {
			return run.SchemaSnapshot{}, err
		}
		null := "NULL"
		if nullable == "NO" {
			null = "NOT NULL"
		}
		tables[table] = append(tables[table], strings.ToLower(col)+":"+typ+":"+null)
	}
	if err := rows.Err(); err != nil {
		return run.SchemaSnapshot{}, err
	}
	return run.NewSchemaSnapshot(tables), nil
}
