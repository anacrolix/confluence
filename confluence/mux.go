package confluence

import (
	"github.com/anacrolix/log"
	"io"
	"net/http"

	"github.com/anacrolix/missinggo/httptoo"
)

func (h *Handler) init() {
	h.initOnce.Do(func() {
		if h.Logger == nil {
			h.Logger = &log.Default
		}
		h.Logger.Levelf(log.Debug, "initing handler %p", h)
		mux := &h.mux
		mux.Handle("/data", h.withTorrentContextFromQuery(dataQueryHandler))
		mux.Handle("/data/infohash/", http.StripPrefix(
			"/data/infohash",
			h.withTorrentContextFromInfohashPath(dataPathHandler)))
		mux.HandleFunc("/status", h.statusHandler)
		mux.Handle("/info", h.withTorrentContextFromQuery(infoHandler))
		mux.Handle("/events", h.withTorrentContextFromQuery(eventHandler))
		mux.Handle("/fileState", h.withTorrentContextFromQuery(func(w http.ResponseWriter, r *request) {
			httptoo.GzipHandler(http.HandlerFunc(func(w http.ResponseWriter, hr *http.Request) {
				r.Request = hr
				fileStateHandler(w, r)
			})).ServeHTTP(w, r.Request)
		}))
		mux.Handle("/metainfo", h.withTorrentContextFromQuery(h.metainfoHandler))
		mux.HandleFunc("/bep44", h.handleBep44)
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			h.TC.WriteStatus(io.Discard)
		})
		mux.HandleFunc("/upload", h.uploadHandler)
	})
}
