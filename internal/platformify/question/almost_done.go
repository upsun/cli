package question

import (
	"context"
	"fmt"

	"github.com/upsun/cli/internal/platformify/colors"
)

type AlmostDone struct{}

func (q *AlmostDone) Ask(ctx context.Context) error {
	out, _, ok := colors.FromContext(ctx)
	if !ok {
		return nil
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, colors.Colorize(colors.AccentCode, "                       (\\_/)"))
	fmt.Fprintln(out, colors.Colorize(colors.AccentCode, "We’re almost done...  =(^.^)="))
	fmt.Fprintln(out)
	return nil
}
