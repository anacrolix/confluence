package confluence

import (
	"net/http"
	"sync"
	"time"

	"github.com/anacrolix/missinggo/refclose"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

type Handler struct {
	TC               *torrent.Client
	TorrentGrace     time.Duration
	OnTorrentGrace   func(t *torrent.Torrent)
	MetainfoCacheDir *string
	// Called as soon as a new torrent is added, with the cached metainfo if it's found.
	OnNewTorrent func(newTorrent *torrent.Torrent, cachedMetainfo *metainfo.MetaInfo)

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
