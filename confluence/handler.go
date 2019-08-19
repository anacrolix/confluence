package confluence

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	"github.com/anacrolix/torrent"
)

type Handler struct {
	TC             *torrent.Client
	TorrentGrace   time.Duration
	OnTorrentGrace func(t *torrent.Torrent)
	CacheDir       string
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), handlerContextKey, h))
	mux.ServeHTTP(w, r)
}

func (h Handler) cachePath(s string) string {
	return filepath.Join(h.CacheDir, "torrents", string(s[0]), string(s[1]), string(s[2]))
}
