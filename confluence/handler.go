package confluence

import (
	"net/http"
	"sync"
	"time"

	"github.com/anacrolix/missinggo/refclose"
	"github.com/anacrolix/torrent"
)

type Handler struct {
	TC               *torrent.Client
	TorrentGrace     time.Duration
	OnTorrentGrace   func(t *torrent.Torrent)
	MetainfoCacheDir *string
	OnNewTorrent     func(*torrent.Torrent)

	mux         http.ServeMux
	initMuxOnce sync.Once
	torrentRefs refclose.RefPool
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.initMux()
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) metainfoCacheDir() string {
	if h.MetainfoCacheDir == nil {
		return "torrents"
	}
	return *h.MetainfoCacheDir
}
