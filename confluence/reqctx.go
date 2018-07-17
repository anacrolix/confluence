package confluence

import (
	"net/http"

	"github.com/anacrolix/missinggo/refclose"
	"github.com/anacrolix/torrent"
)

var (
	torrentContextKey = new(byte)
	handlerContextKey = new(byte)
	torrentRefs       refclose.RefPool
)

func torrentForRequest(r *http.Request) *torrent.Torrent {
	return r.Context().Value(torrentContextKey).(*torrent.Torrent)
}

func getTorrentClientFromRequestContext(r *http.Request) *torrent.Client {
	return getHandler(r).TC
}

func getHandler(r *http.Request) Handler {
	return r.Context().Value(handlerContextKey).(Handler)
}
