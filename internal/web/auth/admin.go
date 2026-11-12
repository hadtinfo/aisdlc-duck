package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/i18n"
	"aisdlc/internal/web/httpx"
)

var roles = map[string]bool{"dev": true, "lead": true}

// RunAdmin là UserAdminCli (args không gồm từ "user"), trả exit code (0 thành công, 1 lỗi nghiệp vụ, 2 sai cú pháp):
//   - add <login> "<Tên>" [--email e] [--role dev|lead]: in token login đúng một lần
//   - role <login> dev|lead
//   - login-token <login>: cấp thêm token login cho user đã có, từ chối user CI (ci:...)
//   - ci-token <tên>: tạo user ci:<tên> nếu chưa có, in token ci đúng một lần
//   - list: login, tên, email, vai trò (tab)
//
// Audit kênh admin, không bao giờ chứa token. Lỗi DB in ra out và trả 1.
func RunAdmin(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) int {
	a := &admin{s: &Service{Pool: pool}, out: out, loc: httpx.OpLocale()}
	code, err := a.run(ctx, args)
	if err != nil {
		fmt.Fprintln(out, a.loc.Tf("Lỗi: %v", err))
		return 1
	}
	return code
}

// admin in theo ngôn ngữ của người vận hành (AISDLC_LOCALE, xem httpx.OpLocale).
type admin struct {
	s   *Service
	out io.Writer
	loc i18n.Locale
}

func (a *admin) println(s string) { fmt.Fprintln(a.out, s) }

func (a *admin) usage() int {
	a.println(a.loc.T("Cách dùng: user add <login> \"<Tên>\" [--email <e>] [--role dev|lead] | user role <login> dev|lead" +
		" | user login-token <login> | user ci-token <tên> | user list"))
	return 2
}

func (a *admin) run(ctx context.Context, args []string) (int, error) {
	if len(args) == 0 {
		return a.usage(), nil
	}
	switch args[0] {
	case "add":
		return a.add(ctx, args)
	case "role":
		return a.role(ctx, args)
	case "ci-token":
		return a.ciToken(ctx, args)
	case "login-token":
		return a.loginToken(ctx, args)
	case "list":
		rows, err := a.s.Pool.Query(ctx, "SELECT "+userCols+" FROM users ORDER BY login")
		if err != nil {
			return 1, err
		}
		users, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*User, error) { return scanUser(row) })
		if err != nil {
			return 1, err
		}
		for _, u := range users {
			email := ""
			if u.Email != nil {
				email = *u.Email
			}
			a.println(u.Login + "\t" + u.DisplayName + "\t" + email + "\t" + u.Role)
		}
		return 0, nil
	}
	return a.usage(), nil
}

// tx chạy f trong một giao dịch.
func (a *admin) tx(ctx context.Context, f func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, a.s.Pool, f)
}

func (a *admin) add(ctx context.Context, args []string) (int, error) {
	if len(args) < 3 {
		return a.usage(), nil
	}
	login := args[1]
	if strings.HasPrefix(login, "ci:") {
		// Tên ci:... dành cho user CI (`user ci-token`); user thường mang tên này sẽ lẫn với CI.
		a.println(a.loc.Tf("Login %s dành cho user CI: dùng `user ci-token`", login))
		return 1, nil
	}
	if Reserved(login) {
		a.println(a.loc.Tf("Login %s dành cho bản tự duyệt của server, chọn tên khác", login))
		return 1, nil
	}
	name := args[2]
	var email *string
	role := "dev"
	for i := 3; i < len(args); i++ {
		switch {
		case args[i] == "--email" && i+1 < len(args):
			i++
			e := args[i]
			email = &e
		case args[i] == "--role" && i+1 < len(args):
			i++
			role = args[i]
		default:
			return a.usage(), nil
		}
	}
	if !roles[role] {
		return a.usage(), nil
	}
	var created CreatedToken
	err := a.tx(ctx, func(tx pgx.Tx) error {
		id, err := a.s.addUser(ctx, tx, login, name, email, role)
		if err != nil {
			return err
		}
		if created, err = a.s.CreateToken(ctx, tx, id, "login", "admin"); err != nil {
			return err
		}
		return a.s.WriteAudit(ctx, tx, AuditEntry{Channel: "admin", Action: "user.add", Target: login,
			Detail: map[string]any{"role": role, "tokenId": created.ID}})
	})
	if errors.Is(err, errDuplicate) {
		a.println(a.loc.Tf("Login đã tồn tại: %s", login))
		return 1, nil
	}
	if err != nil {
		return 1, err
	}
	a.println("Token: " + created.Token)
	return 0, nil
}

func (a *admin) role(ctx context.Context, args []string) (int, error) {
	if len(args) != 3 || !roles[args[2]] {
		return a.usage(), nil
	}
	login, role := args[1], args[2]
	found := false
	err := a.tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE users SET role = $1 WHERE login = $2", role, login)
		if err != nil || tag.RowsAffected() != 1 {
			return err
		}
		found = true
		return a.s.WriteAudit(ctx, tx, AuditEntry{Channel: "admin", Action: "user.role", Target: login,
			Detail: map[string]any{"role": role}})
	})
	if err != nil {
		return 1, err
	}
	if !found {
		a.println(a.loc.Tf("Không có login: %s", login))
		return 1, nil
	}
	a.println(a.loc.Tf("Đã đặt vai trò %s cho %s", role, login))
	return 0, nil
}

func (a *admin) ciToken(ctx context.Context, args []string) (int, error) {
	if len(args) != 2 || javaBlank(args[1]) {
		return a.usage(), nil
	}
	name := args[1]
	login := "ci:" + name
	var created CreatedToken
	err := a.tx(ctx, func(tx pgx.Tx) error {
		u, err := userByLogin(ctx, tx, login)
		if err != nil {
			return err
		}
		var id int64
		if u != nil {
			id = u.ID
		} else if id, err = a.s.addUser(ctx, tx, login, "CI "+name, nil, "dev"); err != nil {
			return err
		}
		if created, err = a.s.CreateToken(ctx, tx, id, "ci", name); err != nil {
			return err
		}
		return a.s.WriteAudit(ctx, tx, AuditEntry{Channel: "admin", Action: "token.create", Target: strconv.FormatInt(created.ID, 10),
			Detail: map[string]any{"tokenId": created.ID, "kind": "ci", "label": name, "user": login}})
	})
	if err != nil {
		return 1, err
	}
	a.println("Token: " + created.Token)
	return 0, nil
}

func (a *admin) loginToken(ctx context.Context, args []string) (int, error) {
	if len(args) != 2 || javaBlank(args[1]) {
		return a.usage(), nil
	}
	login := args[1]
	if strings.HasPrefix(login, "ci:") {
		// Token login mở được phiên trình duyệt (duyệt được): user CI chỉ có token ci.
		a.println(a.loc.Tf("Không cấp token login cho user CI (%s): dùng `user ci-token`", login))
		return 1, nil
	}
	var created *CreatedToken
	err := a.tx(ctx, func(tx pgx.Tx) error {
		u, err := userByLogin(ctx, tx, login)
		if err != nil || u == nil {
			return err
		}
		c, err := a.s.CreateToken(ctx, tx, u.ID, "login", "admin")
		if err != nil {
			return err
		}
		created = &c
		return a.s.WriteAudit(ctx, tx, AuditEntry{Channel: "admin", Action: "token.create", Target: strconv.FormatInt(c.ID, 10),
			Detail: map[string]any{"tokenId": c.ID, "kind": "login", "label": "admin", "user": login}})
	})
	if err != nil {
		return 1, err
	}
	if created == nil {
		a.println(a.loc.Tf("Không có login: %s", login))
		return 1, nil
	}
	a.println("Token: " + created.Token)
	return 0, nil
}
