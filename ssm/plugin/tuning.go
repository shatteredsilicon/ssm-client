package plugin

import (
	"context"
	"net/url"
)

type Tuning interface {
	Name() string
	Init(context.Context, string, url.URL) (*Info, error)
}
