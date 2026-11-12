package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/i18n"
)

// usageTemplate là template usage mặc định của cobra theo locale (bản tiếng Việt cùng cấu trúc; bản tiếng Anh là
// nguyên template của cobra, trừ viUse).
func usageTemplate() string {
	return i18n.T(`Cách dùng:{{if .Runnable}}
  {{viUse .UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [lệnh]{{end}}{{if gt (len .Aliases) 0}}

Tên khác:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Ví dụ:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Lệnh:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Lệnh khác:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Cờ:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Cờ chung:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Chủ đề trợ giúp khác:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Chạy "{{.CommandPath}} [lệnh] --help" để xem chi tiết một lệnh.{{end}}
`)
}

func init() {
	// UseLine của cobra luôn thêm "[flags]"; dịch lúc in (theo locale của lần chạy).
	cobra.AddTemplateFunc("viUse", func(s string) string { return strings.Replace(s, "[flags]", i18n.T("[cờ]"), 1) })
}

// localizeHelp đặt help và usage tiếng Việt cho root và mọi lệnh con (gọi sau khi đã thêm hết lệnh): template usage,
// lệnh `help` và cờ --help của từng lệnh.
func localizeHelp(root *cobra.Command) {
	root.SetUsageTemplate(usageTemplate())
	root.SetHelpCommand(&cobra.Command{
		Use:   i18n.T("help [lệnh]"),
		Short: i18n.T("In trợ giúp của một lệnh."),
		Long:  i18n.T("In trợ giúp của một lệnh: `aisdlc help <lệnh>`."),
		RunE: func(c *cobra.Command, args []string) error {
			cmd, rest, err := c.Root().Find(args)
			if cmd == nil || err != nil || len(rest) > 0 {
				c.Printf(i18n.T("Không có chủ đề trợ giúp %q\n"), strings.Join(args, " "))
				return c.Root().Usage()
			}
			cmd.InitDefaultHelpFlag()
			return cmd.Help()
		},
	})
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.InitDefaultHelpFlag()
		if f := c.Flags().Lookup("help"); f != nil {
			f.Usage = fmt.Sprintf(i18n.T("In trợ giúp cho %s."), c.Name())
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(root)
}
