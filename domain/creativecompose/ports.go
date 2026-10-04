package creativecompose

import "context"

type Images struct {
	Image string
	Logo  string
}

type Renderer interface {
	Render(ctx context.Context, layout Layout, images Images) ([]byte, error)
}
