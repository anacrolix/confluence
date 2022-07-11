package confluence

import (
	"github.com/anacrolix/log"
	"net/http"
	"sync"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/missinggo/refclose"
	"github.com/anacrolix/squirrel"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

type Handler struct {
	Logger           *log.Logger
	TC               *torrent.Client
	TorrentGrace     time.Duration
	OnTorrentGrace   func(t *torrent.Torrent)
	MetainfoCacheDir *string
	MetainfoStorage  *squirrel.Cache
	// Called as soon as a new torrent is added, with the cached metainfo if it's found.
	OnNewTorrent func(newTorrent *torrent.Torrent, cachedMetainfo *metainfo.MetaInfo)
	DhtServers   []*dht.Server
	Storage      *storage.Client
	// Alter metainfos returned from upload handler. For example to add trackers, nodes, comments etc.
	ModifyUploadMetainfo func(mi *metainfo.MetaInfo)

	mux         http.ServeMux
	initOnce    sync.Once
	torrentRefs refclose.RefPool
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.init()
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) metainfoCacheDir() string {
	if h.MetainfoCacheDir == nil {
		return "torrents"
	}
	return *h.MetainfoCacheDir
}
