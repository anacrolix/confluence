package confluence

import (
	"net/http"

	"github.com/anacrolix/missinggo/httptoo"
)

func (h *Handler) initMux() {
	h.initMuxOnce.Do(func() {
		mux := &h.mux
		mux.Handle("/data", h.withTorrentContextFromQuery(dataQueryHandler))
		mux.Handle("/data/", http.StripPrefix("/data", h.withTorrentContextFromPath(dataPathHandler)))
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
	})
}
