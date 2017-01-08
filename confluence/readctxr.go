package confluence

import "golang.org/x/net/context"

type readContexter struct {
	r interface {
		ReadContext([]byte, context.Context) (int, error)
	}
	ctx context.Context
}

func (me readContexter) Read(b []byte) (int, error) {
	return me.r.ReadContext(b, me.ctx)
}
