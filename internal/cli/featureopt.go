package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
)

// FeatureOption là cờ --feature KEY (-f) ghi rõ tính năng, thắng mọi cách phân giải khác (FeatureOption của Java).
type FeatureOption struct {
	value string
	cmd   *cobra.Command
}

// AddTo khai báo cờ trên lệnh.
func (o *FeatureOption) AddTo(cmd *cobra.Command) {
	o.cmd = cmd
	cmd.Flags().StringVarP(&o.value, "feature", "f", "", i18n.T("Tính năng cần làm (mặc định theo session)"))
}

// Key là key ghi trên lệnh; ok=false khi không có cờ. Key sai dạng thì exit 2.
func (o *FeatureOption) Key() (model.FeatureKey, bool, error) {
	if o.cmd == nil || !o.cmd.Flags().Changed("feature") {
		return "", false, nil
	}
	k, err := model.ParseFeatureKey(strings.TrimSpace(o.value))
	if err != nil {
		return "", false, abort(2, err.Error())
	}
	return k, true, nil
}

// Feature là tính năng của lệnh: áp --feature (nếu có) rồi phân giải.
func (o *FeatureOption) Feature(ctx *Context) (*Feature, error) {
	k, ok, err := o.Key()
	if err != nil {
		return nil, err
	}
	if ok {
		ctx.WithFeatureOverride(k)
	}
	return ctx.Feature()
}
