package confluence

import (
	"net/http"
	"time"

	"github.com/anacrolix/missinggo/refclose"
	"github.com/anacrolix/torrent"
)

var (
	torrentClientContextKey     = new(byte)
	torrentContextKey           = new(byte)
	torrentCloseGraceContextKey = new(byte)
	torrentRefs                 refclose.RefPool
)

func torrentClientForRequest(r *http.Request) *torrent.Client {
	return r.Context().Value(torrentClientContextKey).(*torrent.Client)
}

func torrentForRequest(r *http.Request) *torrent.Torrent {
	return r.Context().Value(torrentContextKey).(*torrent.Torrent)
}

func torrentCloseGraceForRequest(r *http.Request) time.Duration {
	return r.Context().Value(torrentCloseGraceContextKey).(time.Duration)
}
