package confluence

import (
	"context"
	"net/http"
	"time"

	"github.com/anacrolix/torrent"
)

type Handler struct {
	TC                *torrent.Client
	TorrentCloseGrace time.Duration
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), torrentClientContextKey, h.TC))
	r = r.WithContext(context.WithValue(r.Context(), torrentCloseGraceContextKey, h.TorrentCloseGrace))
	mux.ServeHTTP(w, r)
}
