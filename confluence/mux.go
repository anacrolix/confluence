package confluence

import (
	"net/http"

	"github.com/anacrolix/missinggo/httptoo"
)

func (h *Handler) initMux() {
	h.initMuxOnce.Do(func() {
		mux := &h.mux
		mux.Handle("/data/", h.withTorrentContext(dataHandler))
		mux.HandleFunc("/status", h.statusHandler)
		mux.Handle("/info/", h.withTorrentContext(infoHandler))
		mux.Handle("/events/", h.withTorrentContext(eventHandler))
		mux.Handle("/fileState/", h.withTorrentContext(func(w http.ResponseWriter, r *request) {
			httptoo.GzipHandler(http.HandlerFunc(func(w http.ResponseWriter, hr *http.Request) {
				r.Request = hr
				fileStateHandler(w, r)
			})).ServeHTTP(w, r.Request)
		}))
		mux.Handle("/metainfo/", h.withTorrentContext(h.metainfoHandler))
		mux.HandleFunc("/bep44", h.handleBep44)
	})
}
