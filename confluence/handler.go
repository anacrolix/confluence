package confluence

import (
	"net/http"
	"sync"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2/refclose"
	"github.com/anacrolix/squirrel"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

type Handler struct {
	Logger         *log.Logger
	TC             *torrent.Client
	TorrentGrace   time.Duration
	OnTorrentGrace func(t *torrent.Torrent)

	// Caching metainfos is worthwhile for Confluence because it provides a cache eviction
	// implemention by default. The torrent Client handles caching pieces, but doesn't cache the
	// infos. If the metainfo isn't provided each time a torrent is evicted, each new access of
	// torrent will have to wait to get the info again to be useful.

	// If non-nil, this is the directory to cache metainfos in.
	MetainfoCacheDir *string
	// A squirrel Cache to storage the metainfos. Supercedes the MetainfoCacheDir.
	MetainfoStorage *squirrel.Cache
	// Bring your own metainfo storage. Supercedes all alternatives.
	MetainfoStorageInterface MetainfoStorage

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
