package confluence

import (
	"context"
	"net/http"

	"github.com/anacrolix/torrent"
)

type Handler struct {
	TC *torrent.Client
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), torrentClientContextKey, h.TC))
	mux.ServeHTTP(w, r)
}
