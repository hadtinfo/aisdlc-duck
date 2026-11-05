package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aisdlc/internal/lang/golang"
)

const sqlSample = "-- c;\nCREATE TABLE daily_limit (account_id VARCHAR(20) PRIMARY KEY, limit_amount BIGINT NOT NULL);\n" +
	"insert into x values ('a;b');\nalter table ACCOUNT add column y int;\nUPDATE account SET y=1;\n" +
	"DELETE FROM daily_limit WHERE account_id='a';\nGRANT SELECT ON x TO y;"

func kinds(st []SqlStatement) (k, tbl []string) {
	for _, s := range st {
		k = append(k, s.Kind)
		tbl = append(tbl, s.Table)
	}
	return
}

func colNames(cs []SqlColumn) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func flagCodes(fs []ReviewFlag) []string {
	out := []string{}
	for _, f := range fs {
		out = append(out, f.Code)
	}
	return out
}

func TestSplitsAndClassifies(t *testing.T) {
	st := ParseSQL(sqlSample)
	k, tbl := kinds(st)
	eq(t, "kinds", k, []string{"CREATE_TABLE", "INSERT", "ALTER_TABLE", "UPDATE", "DELETE", "OTHER"})
	eq(t, "tables", tbl, []string{"daily_limit", "x", "account", "account", "daily_limit", ""})
	eq(t, "cols", colNames(st[0].Columns), []string{"account_id", "limit_amount"})
	eq(t, "where3", st[3].HasWhere, false)
	eq(t, "where4", st[4].HasWhere, true)
}

func TestColumnDetailForPay1Draft(t *testing.T) {
	st := ParseSQL("CREATE TABLE daily_limit (\n account_id VARCHAR(20) PRIMARY KEY REFERENCES account (id),\n" +
		" limit_amount BIGINT NOT NULL,\n used_amount BIGINT NOT NULL DEFAULT 0,\n note VARCHAR(5) DEFAULT 'a;b'\n);")[0]
	eq(t, "cols", st.Columns, []SqlColumn{
		{"account_id", "VARCHAR(20)", []string{"PK", "FK"}, "account(id)"},
		{"limit_amount", "BIGINT", []string{"NOT NULL"}, ""},
		{"used_amount", "BIGINT", []string{"NOT NULL", "DEFAULT 0"}, ""},
		{"note", "VARCHAR(5)", []string{"DEFAULT 'a;b'"}, ""},
	})
}

func TestTableLevelConstraintsAndUnparsedColumn(t *testing.T) {
	st := ParseSQL("CREATE TABLE t (a INT, b INT, c, PRIMARY KEY (a), FOREIGN KEY (b) REFERENCES p(id), UNIQUE (b))")[0]
	eq(t, "cols", st.Columns, []SqlColumn{
		{"a", "INT", []string{"PK"}, ""},
		{"b", "INT", []string{"UNIQUE", "FK"}, "p(id)"},
	})
	eq(t, "unparsed", st.Unparsed, []string{"c"})
	eq(t, "codes", flagCodes(SqlFlags([]SqlStatement{st}, nil)), []string{"DB_CREATE_ONLY", "DB_COLUMN_UNPARSED"})
}

func TestSqlFlags(t *testing.T) {
	acct := []string{"account"}
	fs := SqlFlags(ParseSQL(sqlSample), acct)
	var touch *ReviewFlag
	na := false
	for i := range fs {
		if fs[i].Code == "DB_TOUCHES_EXISTING" {
			touch = &fs[i]
		}
		if fs[i].Level == NA {
			na = true
		}
	}
	if touch == nil {
		t.Fatal("thiếu DB_TOUCHES_EXISTING")
	}
	eq(t, "refs", touch.Refs, []string{"account"})
	eq(t, "level", touch.Level, HIGH)
	eq(t, "na", na, true)

	eq(t, "alter", SqlFlags(ParseSQL("alter table account add column y int"), acct)[0].Level, WARN)
	eq(t, "drop", SqlFlags(ParseSQL("DROP TABLE account"), acct)[0].Level, HIGH)

	only := SqlFlags(ParseSQL("CREATE TABLE daily_limit (account_id VARCHAR(20) REFERENCES account (id))"), acct)
	eq(t, "only", flagCodes(only), []string{"DB_CREATE_ONLY"})
	eq(t, "only level", only[0].Level, INFO)

	eq(t, "delete where", len(SqlFlags(ParseSQL("DELETE FROM account WHERE id='a'"), acct)), 0)
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExistingTablesFromMigrations(t *testing.T) {
	tmp := t.TempDir()
	d := "db/migrations/"
	writeFile(t, tmp, d+"00001_init.sql", "CREATE TABLE account (\n    id         VARCHAR(20)  PRIMARY KEY,\n    owner_name VARCHAR(100) NOT NULL\n);")
	writeFile(t, tmp, d+"00002_more.sql", "ALTER TABLE account ADD COLUMN status VARCHAR(10) DEFAULT 'A';")
	writeFile(t, tmp, "other.sql", "CREATE TABLE nope (id INT);")
	tb := ExistingTables(tmp, []string{"other.sql", d + "00001_init.sql", d + "00002_more.sql"},
		testSettings, golang.Adapter{}.MigrationOrder)
	eq(t, "keys", len(tb), 1)
	eq(t, "account", tb["account"], []SqlColumn{
		{"id", "VARCHAR(20)", []string{"PK"}, ""},
		{"owner_name", "VARCHAR(100)", []string{"NOT NULL"}, ""},
		{"status", "VARCHAR(10)", []string{"DEFAULT 'A'"}, ""},
	})
}

func TestMigrationsApplyInAdapterVersionOrderAndDrop(t *testing.T) {
	tmp := t.TempDir()
	d := "db/migrations/"
	writeFile(t, tmp, d+"00002_a.sql", "CREATE TABLE t (id INT); CREATE TABLE gone (id INT);")
	writeFile(t, tmp, d+"00010_b.sql", "ALTER TABLE t ADD COLUMN x INT; DROP TABLE gone;")
	tb := ExistingTables(tmp, []string{d + "00010_b.sql", d + "00002_a.sql"}, testSettings, golang.Adapter{}.MigrationOrder)
	eq(t, "keys", len(tb), 1)
	eq(t, "cols", colNames(tb["t"]), []string{"id", "x"})
}

func TestSeparateFlagsPerLevelAndQualifiedNames(t *testing.T) {
	fs := SqlFlags(ParseSQL("DROP TABLE public.account; ALTER TABLE orders ADD COLUMN y int"), []string{"account", "orders"})
	eq(t, "len", len(fs), 2)
	eq(t, "l0", fs[0].Level, HIGH)
	eq(t, "l1", fs[1].Level, WARN)
	eq(t, "r0", fs[0].Refs, []string{"public.account"})
	eq(t, "r1", fs[1].Refs, []string{"orders"})
}

func flagsOf(sql string) []ReviewFlag { return SqlFlags(ParseSQL(sql), []string{"account"}) }

func TestCreateOnlyNeedsEveryStatementToCreate(t *testing.T) {
	ok := false
	for _, f := range flagsOf("CREATE TABLE x (id INT);\nCREATE INDEX ix_x ON x (id);") {
		if f.Code == "DB_CREATE_ONLY" && len(f.Refs) == 1 && f.Refs[0] == "x" {
			ok = true
		}
	}
	if !ok {
		t.Error("thiếu DB_CREATE_ONLY")
	}
	for _, extra := range []string{"UPDATE account SET y = 1 WHERE id = 2;", "INSERT INTO account VALUES (1);",
		"DELETE FROM account WHERE id = 1;", "INSERT INTO x VALUES (1);", "GRANT SELECT ON x TO y;",
		"CREATE INDEX ix_other ON other_t (id);", "ALTER TABLE x ADD COLUMN z INT;"} {
		for _, c := range flagCodes(flagsOf("CREATE TABLE x (id INT);\n" + extra)) {
			if c == "DB_CREATE_ONLY" {
				t.Errorf("%s: không được có DB_CREATE_ONLY", extra)
			}
		}
	}
}

func TestIndexOnExistingTableWarns(t *testing.T) {
	st := ParseSQL("CREATE UNIQUE INDEX IF NOT EXISTS ix_a ON public.account (owner_name);")
	eq(t, "len", len(st), 1)
	eq(t, "kind", st[0].Kind, "CREATE_INDEX")
	eq(t, "table", st[0].Table, "public.account")
	ok := false
	for _, f := range SqlFlags(st, []string{"account"}) {
		if f.Code == "DB_TOUCHES_EXISTING" && f.Level == WARN {
			ok = true
		}
	}
	if !ok {
		t.Error("thiếu cờ WARN")
	}
}

func TestWhereOnlyCountsAtTopLevel(t *testing.T) {
	ok := false
	for _, f := range flagsOf("UPDATE account SET y = (SELECT max(v) FROM other WHERE other.id = account.id);") {
		if f.Code == "DB_TOUCHES_EXISTING" && f.Level == HIGH {
			ok = true
		}
	}
	if !ok {
		t.Error("thiếu cờ HIGH")
	}
	eq(t, "a", ParseSQL("UPDATE account SET y = (SELECT 1 FROM o WHERE o.a = 1) WHERE id = 3")[0].HasWhere, true)
	eq(t, "b", ParseSQL("DELETE FROM account WHERE_x = 1")[0].HasWhere, false)
	eq(t, "c", ParseSQL("UPDATE account SET note = 'where'")[0].HasWhere, false)
}

func TestHugeLiteralIsScannedLinearly(t *testing.T) {
	big := strings.Repeat("a'';(", 40_000)
	sql := "CREATE TABLE t (id INT PRIMARY KEY, note VARCHAR(10) DEFAULT 'x');\nINSERT INTO t VALUES (1, '" +
		big + "');\nUPDATE t SET note = '" + big + "' WHERE id = 1;"
	st := ParseSQL(sql)
	k, _ := kinds(st)
	eq(t, "kinds", k, []string{"CREATE_TABLE", "INSERT", "UPDATE"})
	eq(t, "where", st[2].HasWhere, true)
	eq(t, "cols", colNames(st[0].Columns), []string{"id", "note"})
	eq(t, "mask", maskLiterals("a 'x''y' b 'z", nil), "a '' b 'z")
	var lits []string
	eq(t, "mask2", maskLiterals("'p' + 'q''r'", &lits), "'~0~' + '~1~'")
	eq(t, "lits", lits, []string{"'p'", "'q''r'"})
}

func TestAlterTableAddColumnFillsColumns(t *testing.T) {
	st := ParseSQL("ALTER TABLE account ADD COLUMN status VARCHAR(10) NOT NULL;")[0]
	eq(t, "kind", st.Kind, "ALTER_TABLE")
	eq(t, "cols", colNames(st.Columns), []string{"status"})
	eq(t, "type", st.Columns[0].Type, "VARCHAR(10)")
	eq(t, "cons", st.Columns[0].Constraints, []string{"NOT NULL"})
}
