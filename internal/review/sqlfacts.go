package review

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"aisdlc/internal/model"
)

// SqlColumn: Constraints theo thứ tự PK, NOT NULL, UNIQUE, FK, DEFAULT <v>; References dạng "account(id)" (rỗng nếu không có).
type SqlColumn struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Constraints []string `json:"constraints"`
	References  string   `json:"references,omitempty"`
}

// SqlStatement: Unparsed là các dòng cột không đọc được (đã bị bỏ khỏi Columns). Table rỗng khi không nhận dạng được.
type SqlStatement struct {
	Kind     string
	Table    string
	HasWhere bool
	Columns  []SqlColumn
	Text     string
	Unparsed []string
}

const textMax = 120

const nameRe = "((?:[`\"\\[]?\\w+[`\"\\]]?\\.)*[`\"\\[]?\\w+[`\"\\]]?)"

// (?is): CASE_INSENSITIVE | DOTALL của Java.
var (
	createRe   = regexp.MustCompile(`(?is)^create\s+(?:\w+\s+)*?table\s+(?:if\s+not\s+exists\s+)?` + nameRe + `\s*(\(.*)$`)
	alterRe    = regexp.MustCompile(`(?is)^alter\s+table\s+(?:if\s+exists\s+)?(?:only\s+)?` + nameRe + `(.*)$`)
	dropRe     = regexp.MustCompile(`(?is)^drop\s+table\s+(?:if\s+exists\s+)?` + nameRe)
	truncateRe = regexp.MustCompile(`(?is)^truncate\s+(?:table\s+)?(?:only\s+)?` + nameRe)
	updateRe   = regexp.MustCompile(`(?is)^update\s+(?:only\s+)?` + nameRe)
	deleteRe   = regexp.MustCompile(`(?is)^delete\s+from\s+(?:only\s+)?` + nameRe)
	insertRe   = regexp.MustCompile(`(?is)^insert\s+(?:\w+\s+)?into\s+` + nameRe)
	indexRe    = regexp.MustCompile(`(?is)^create\s+(?:unique\s+)?index\s+(?:concurrently\s+)?(?:if\s+not\s+exists\s+)?(?:\S+\s+)?on\s+(?:only\s+)?` + nameRe)
	placeholRe = regexp.MustCompile(`'~(\d+)~'`)
	colTypeRe  = regexp.MustCompile(`(?is)^(\w+(?:\s+precision)?(?:\s*\([^)]*\))?)(.*)$`)
	refsRe     = regexp.MustCompile(`(?is)\breferences\s+` + nameRe + `\s*(?:\(([^)]*)\))?`)
	defaultRe  = regexp.MustCompile(`(?is)\bdefault\s+('~\d+~'|\([^)]*\)|\S+)`)
	tablePKRe  = regexp.MustCompile(`(?is)^(?:constraint\s+\S+\s+)?primary\s+key\s*\(([^)]*)\)`)
	tableUQRe  = regexp.MustCompile(`(?is)^(?:constraint\s+\S+\s+)?unique\s*(?:\w+\s*)?\(([^)]*)\)`)
	tableFKRe  = regexp.MustCompile(`(?is)^(?:constraint\s+\S+\s+)?foreign\s+key\s*\(([^)]*)\)\s*references\s+` + nameRe + `\s*(?:\(([^)]*)\))?`)
	addRe      = regexp.MustCompile(`(?is)^\s*add\s+(?:column\s+)?(?:if\s+not\s+exists\s+)?(.*)$`)
	pkWordRe   = regexp.MustCompile(`(?is)\bprimary\s+key\b`)
	nnWordRe   = regexp.MustCompile(`(?is)\bnot\s+null\b`)
	uqWordRe   = regexp.MustCompile(`(?is)\bunique\b`)
	colNameRe  = regexp.MustCompile("^[`\"\\[]?\\w+[`\"\\]]?$")
	quoteRe    = regexp.MustCompile("[`\"\\[\\]]")
	spacesRe   = regexp.MustCompile(`\s+`)
	parenRe    = regexp.MustCompile(`\s*\(\s*`)
)

var constraintWords = map[string]bool{"primary": true, "foreign": true, "constraint": true, "unique": true,
	"check": true, "key": true, "index": true, "exclude": true, "like": true}

// ParseSQL trích sự kiện nhẹ từ SQL nháp (không parse SQL đầy đủ).
func ParseSQL(sql string) []SqlStatement {
	out := []SqlStatement{}
	for _, stmt := range splitSQL(sql) {
		out = append(out, classify(stmt))
	}
	return out
}

// splitSQL bỏ comment `--` và `/* */`, giữ nguyên chuỗi '...', tách theo ';' ngoài chuỗi.
func splitSQL(sql string) []string {
	var out []string
	var cur strings.Builder
	n := len(sql)
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for i := 0; i < n; {
		c := sql[i]
		switch {
		case c == '\'':
			cur.WriteByte(c)
			i++
			for i < n {
				d := sql[i]
				cur.WriteByte(d)
				i++
				if d == '\'' {
					if i < n && sql[i] == '\'' { // '' nằm trong chuỗi
						cur.WriteByte('\'')
						i++
					} else {
						break
					}
				}
			}
		case c == '-' && i+1 < n && sql[i+1] == '-':
			for i < n && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && sql[i+1] == '*':
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				i = n
			} else {
				i += 2 + end + 2
			}
			cur.WriteByte(' ')
		case c == ';':
			flush()
			i++
		default:
			cur.WriteByte(c)
			i++
		}
	}
	flush()
	return out
}

func classify(stmt string) SqlStatement {
	text := spacesRe.ReplaceAllString(stmt, " ")
	if r := []rune(text); len(r) > textMax {
		text = string(r[:textMax-1]) + "…"
	}
	masked := maskLiterals(stmt, nil)
	if m := createRe.FindStringSubmatch(masked); m != nil {
		var literals []string
		body := createBody(stmt, &literals)
		unparsed := []string{}
		cols := columns(body, literals, &unparsed)
		return SqlStatement{"CREATE_TABLE", tableName(m[1]), false, cols, text, unparsed}
	} else if m := alterRe.FindStringSubmatch(masked); m != nil {
		return stmtOf("ALTER_TABLE", tableName(m[1]), false, alterAdds(stmt), text)
	} else if m := dropRe.FindStringSubmatch(masked); m != nil {
		return stmtOf("DROP", tableName(m[1]), false, nil, text)
	} else if m := truncateRe.FindStringSubmatch(masked); m != nil {
		return stmtOf("TRUNCATE", tableName(m[1]), false, nil, text)
	} else if m := updateRe.FindStringSubmatch(masked); m != nil {
		return stmtOf("UPDATE", tableName(m[1]), topLevelWhere(masked), nil, text)
	} else if m := deleteRe.FindStringSubmatch(masked); m != nil {
		return stmtOf("DELETE", tableName(m[1]), topLevelWhere(masked), nil, text)
	} else if m := insertRe.FindStringSubmatch(masked); m != nil {
		return stmtOf("INSERT", tableName(m[1]), false, nil, text)
	} else if m := indexRe.FindStringSubmatch(masked); m != nil {
		return stmtOf("CREATE_INDEX", tableName(m[1]), false, nil, text)
	}
	return stmtOf("OTHER", "", false, nil, text)
}

func stmtOf(kind, table string, where bool, cols []SqlColumn, text string) SqlStatement {
	if cols == nil {
		cols = []SqlColumn{}
	}
	return SqlStatement{kind, table, where, cols, text, []string{}}
}

// maskLiterals thay mỗi chuỗi '...' (có ” bên trong) bằng ” khi literals nil, hoặc bằng '~i~' và lưu chuỗi
// gốc vào *literals. Quét tuyến tính; chuỗi không đóng được giữ nguyên.
func maskLiterals(s string, literals *[]string) string {
	var sb strings.Builder
	n := len(s)
	for i := 0; i < n; {
		if s[i] != '\'' {
			sb.WriteByte(s[i])
			i++
			continue
		}
		j, end := i+1, -1
		for j < n {
			if s[j] == '\'' {
				if j+1 < n && s[j+1] == '\'' {
					j += 2
					continue
				}
				end = j
				break
			}
			j++
		}
		if end < 0 {
			sb.WriteString(s[i:])
			break
		}
		if literals == nil {
			sb.WriteString("''")
		} else {
			sb.WriteString("'~" + strconv.Itoa(len(*literals)) + "~'")
			*literals = append(*literals, s[i:end+1])
		}
		i = end + 1
	}
	return sb.String()
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// topLevelWhere: có từ khóa WHERE ở cấp ngoài cùng (không nằm trong ngoặc/subquery); chuỗi đã được che từ trước.
func topLevelWhere(masked string) bool {
	depth := 0
	n := len(masked)
	for i := 0; i < n; i++ {
		switch c := masked[i]; {
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case depth == 0 && (c == 'w' || c == 'W') && i+5 <= n && strings.EqualFold(masked[i:i+5], "where"):
			if i > 0 {
				if r, _ := utf8.DecodeLastRuneInString(masked[:i]); isWordRune(r) {
					continue
				}
			}
			if i+5 < n {
				if r, _ := utf8.DecodeRuneInString(masked[i+5:]); isWordRune(r) {
					continue
				}
			}
			return true
		}
	}
	return false
}

// createBody: phần từ dấu "(" đầu tiên, chuỗi '...' được thay bằng '~i~'.
func createBody(stmt string, literals *[]string) string {
	ph := maskLiterals(stmt, literals)
	open := strings.IndexByte(ph, '(')
	if open < 0 {
		return ""
	}
	return ph[open:]
}

// colName: chữ thường, bỏ dấu trích và tiền tố schema.
func colName(raw string) string {
	last := raw[strings.LastIndexByte(raw, '.')+1:]
	return strings.ToLower(quoteRe.ReplaceAllString(last, ""))
}

// tableName: chữ thường, bỏ dấu trích, giữ tiền tố schema nếu có.
func tableName(raw string) string { return strings.ToLower(quoteRe.ReplaceAllString(raw, "")) }

func unqualified(t string) string { return t[strings.LastIndexByte(t, '.')+1:] }

// topLevelItems: các phần tử cấp ngoài cùng của danh sách trong ngoặc đầu tiên của body.
func topLevelItems(body string) []string {
	var items []string
	depth, start := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '(':
			if depth == 0 {
				start = i + 1
			}
			depth++
		case ')':
			depth--
			if depth == 0 {
				return append(items, body[start:i])
			}
		case ',':
			if depth == 1 {
				items = append(items, body[start:i])
				start = i + 1
			}
		}
	}
	return items
}

func firstWord(def string) string {
	return strings.ToLower(strings.SplitN(def, " ", 2)[0])
}

func columns(body string, literals []string, unparsed *[]string) []SqlColumn {
	cols := map[string]*SqlColumn{}
	var order []string
	var tableLevel []string
	for _, raw := range topLevelItems(body) {
		def := spacesRe.ReplaceAllString(strings.TrimSpace(raw), " ")
		if def == "" {
			continue
		}
		if constraintWords[firstWord(def)] {
			tableLevel = append(tableLevel, def)
			continue
		}
		c, ok := parseColumn(def, literals)
		if !ok {
			*unparsed = append(*unparsed, restore(def, literals))
			continue
		}
		if _, dup := cols[c.Name]; !dup {
			order = append(order, c.Name)
		}
		cc := c
		cols[c.Name] = &cc
	}
	for _, def := range tableLevel {
		if m := tablePKRe.FindStringSubmatch(def); m != nil {
			for _, c := range idents(m[1]) {
				addConstraint(cols, c, "PK", "")
			}
		} else if m := tableUQRe.FindStringSubmatch(def); m != nil {
			for _, c := range idents(m[1]) {
				addConstraint(cols, c, "UNIQUE", "")
			}
		} else if m := tableFKRe.FindStringSubmatch(def); m != nil {
			from := idents(m[1])
			var to []string
			if m[3] != "" {
				to = idents(m[3])
			}
			for i, f := range from {
				target := tableName(m[2])
				if i < len(to) {
					target += "(" + to[i] + ")"
				}
				addConstraint(cols, f, "FK", target)
			}
		}
	}
	out := make([]SqlColumn, 0, len(order))
	for _, n := range order {
		out = append(out, *cols[n])
	}
	return out
}

func idents(list string) []string {
	var out []string
	for _, s := range strings.Split(list, ",") {
		if strings.TrimSpace(s) != "" {
			out = append(out, colName(strings.TrimSpace(s)))
		}
	}
	return out
}

func addConstraint(cols map[string]*SqlColumn, col, constraint, ref string) {
	c := cols[col]
	if c == nil {
		return
	}
	has := false
	for _, x := range c.Constraints {
		has = has || x == constraint
	}
	if !has {
		c.Constraints = append(c.Constraints, constraint)
	}
	sortConstraints(c.Constraints)
	if ref != "" {
		c.References = ref
	}
}

func rank(c string) int {
	switch c {
	case "PK":
		return 0
	case "NOT NULL":
		return 1
	case "UNIQUE":
		return 2
	case "FK":
		return 3
	}
	return 4
}

func sortConstraints(cs []string) {
	sort.SliceStable(cs, func(i, j int) bool { return rank(cs[i]) < rank(cs[j]) })
}

// parseColumn đọc một dòng cột; ok=false nếu không đọc được (thiếu tên hoặc kiểu).
func parseColumn(def string, literals []string) (SqlColumn, bool) {
	tok := strings.SplitN(def, " ", 2)
	if len(tok) < 2 || !colNameRe.MatchString(tok[0]) {
		return SqlColumn{}, false
	}
	tm := colTypeRe.FindStringSubmatch(strings.TrimSpace(tok[1]))
	if tm == nil {
		return SqlColumn{}, false
	}
	typ := strings.ToUpper(parenRe.ReplaceAllString(spacesRe.ReplaceAllString(tm[1], " "), "("))
	if constraintWords[strings.ToLower(typ)] || typ == "NOT" || typ == "NULL" || typ == "DEFAULT" || typ == "REFERENCES" {
		return SqlColumn{}, false
	}
	rest := tm[2]
	cs := []string{}
	references := ""
	if pkWordRe.MatchString(rest) {
		cs = append(cs, "PK")
	}
	if nnWordRe.MatchString(rest) {
		cs = append(cs, "NOT NULL")
	}
	if uqWordRe.MatchString(rest) {
		cs = append(cs, "UNIQUE")
	}
	if rm := refsRe.FindStringSubmatch(rest); rm != nil {
		cs = append(cs, "FK")
		cols := ""
		if rm[2] != "" {
			cols = "(" + strings.Join(idents(rm[2]), ",") + ")"
		}
		references = tableName(rm[1]) + cols
	}
	if dm := defaultRe.FindStringSubmatch(rest); dm != nil {
		cs = append(cs, "DEFAULT "+restore(dm[1], literals))
	}
	return SqlColumn{colName(tok[0]), typ, cs, references}, true
}

func restore(s string, literals []string) string {
	return placeholRe.ReplaceAllStringFunc(s, func(m string) string {
		i, _ := strconv.Atoi(placeholRe.FindStringSubmatch(m)[1])
		return literals[i]
	})
}

// alterAdds: cột thêm bởi ALTER TABLE ... ADD [COLUMN] ...; bỏ qua ràng buộc cấp bảng.
func alterAdds(stmt string) []SqlColumn {
	var lits []string
	am := alterRe.FindStringSubmatch(maskLiterals(stmt, &lits))
	if am == nil {
		return nil
	}
	var out []SqlColumn
	for _, clause := range splitTopLevel(am[2]) {
		add := addRe.FindStringSubmatch(clause)
		if add == nil {
			continue
		}
		def := spacesRe.ReplaceAllString(strings.TrimSpace(add[1]), " ")
		if def == "" || constraintWords[firstWord(def)] {
			continue
		}
		if c, ok := parseColumn(def, lits); ok {
			out = append(out, c)
		}
	}
	return out
}

func splitTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// ExistingTables gom bảng và cột từ các CREATE TABLE (cộng ALTER TABLE ... ADD COLUMN) trong file .sql dưới
// s.MigrationDirs, áp theo thứ tự order (adapter.MigrationOrder). File không đọc được thì bỏ qua.
// Map không có thứ tự; người gọi sắp tên bảng khi cần.
func ExistingTables(repoRoot string, files []string, s model.ReviewSettings, order func([]string) []string) map[string][]SqlColumn {
	var sqlFiles []string
	for _, f := range files {
		if strings.HasSuffix(strings.ToLower(f), ".sql") && inDirs(f, s.MigrationDirs) {
			sqlFiles = append(sqlFiles, f)
		}
	}
	tables := map[string][]SqlColumn{}
	for _, f := range order(sqlFiles) {
		b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		for _, stmt := range splitSQL(string(b)) {
			st := classify(stmt)
			if st.Table == "" {
				continue
			}
			switch st.Kind {
			case "CREATE_TABLE":
				tables[st.Table] = append([]SqlColumn{}, st.Columns...)
			case "DROP":
				delete(tables, st.Table)
			case "ALTER_TABLE":
				if adds := alterAdds(stmt); len(adds) > 0 {
					tables[st.Table] = append(tables[st.Table], adds...)
				}
			}
		}
	}
	return tables
}

// SqlFlags sinh cờ DB từ SQL nháp và tên các bảng đã có.
func SqlFlags(drafts []SqlStatement, existingNames []string) []ReviewFlag {
	existing := map[string]bool{}
	for _, n := range existingNames {
		existing[unqualified(n)] = true
	}
	high, warn, created, indexed := set{}, set{}, set{}, set{}
	others, unparsed := set{}, set{}
	// chỉ "tạo mới" khi mọi câu là CREATE TABLE bảng mới hoặc CREATE INDEX trên bảng mới
	createOnly := true
	for _, st := range drafts {
		t := st.Table
		known := t != "" && existing[unqualified(t)]
		for _, u := range st.Unparsed {
			unparsed[t+": "+u] = true
		}
		switch st.Kind {
		case "CREATE_TABLE":
			if known {
				warn[t] = true
			} else if t != "" {
				created[t] = true
			} else {
				createOnly = false
			}
		case "CREATE_INDEX":
			if known {
				warn[t] = true
			} else if t != "" {
				indexed[unqualified(t)] = true
			} else {
				createOnly = false
			}
		case "ALTER_TABLE":
			if known {
				warn[t] = true
			}
			createOnly = false
		case "DROP", "TRUNCATE":
			if known {
				high[t] = true
			}
			createOnly = false
		case "UPDATE", "DELETE":
			if known && !st.HasWhere {
				high[t] = true
			}
			createOnly = false
		case "OTHER":
			others[st.Text] = true
			createOnly = false
		default:
			createOnly = false
		}
	}
	createdShort := set{}
	for c := range created {
		createdShort[unqualified(c)] = true
	}
	for i := range indexed {
		if !createdShort[i] {
			createOnly = false
		}
	}
	for h := range high {
		delete(warn, h)
	}
	out := []ReviewFlag{}
	if len(high) > 0 {
		out = append(out, NewFlag("DB_TOUCHES_EXISTING", HIGH,
			"SQL nháp xóa hoặc sửa hàng loạt bảng đã có: "+strings.Join(high.sorted(), ", "),
			"DROP/TRUNCATE hoặc UPDATE/DELETE không WHERE: kiểm tra ảnh hưởng tới dữ liệu.", high.sorted()))
	}
	if len(warn) > 0 {
		out = append(out, NewFlag("DB_TOUCHES_EXISTING", WARN,
			"SQL nháp thay đổi cấu trúc bảng đã có: "+strings.Join(warn.sorted(), ", "),
			"Kiểm tra ảnh hưởng tới dữ liệu và code hiện tại.", warn.sorted()))
	}
	if createOnly && len(high) == 0 && len(warn) == 0 && len(created) > 0 {
		out = append(out, NewFlag("DB_CREATE_ONLY", INFO,
			"SQL nháp chỉ tạo bảng mới: "+strings.Join(created.sorted(), ", "), "", created.sorted()))
	}
	if len(others) > 0 {
		out = append(out, NewFlag("DB_UNRECOGNIZED_SQL", NA,
			strconv.Itoa(len(others))+" câu SQL không nhận dạng được, cần đọc tay",
			"Đọc trực tiếp file SQL nháp.", others.sorted()))
	}
	if len(unparsed) > 0 {
		out = append(out, NewFlag("DB_COLUMN_UNPARSED", NA,
			strconv.Itoa(len(unparsed))+" dòng cột không đọc được, đã bỏ khỏi sơ đồ",
			"Đọc trực tiếp file SQL nháp.", unparsed.sorted()))
	}
	return out
}

type set map[string]bool

func (s set) sorted() []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
