package main

import (
	"net/http"

	"github.com/anacrolix/missinggo/httptoo"
	"github.com/anacrolix/torrent"
)

func dataHandler(w http.ResponseWriter, r *http.Request) {
	httptoo.WrapHandler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		t := r.Context().Value(torrentContextKey).(*torrent.Torrent)
		w.Header().Set("Content-Disposition", "inline")
		if len(q["path"]) == 0 {
			serveTorrent(w, r, t)
		} else {
			serveFile(w, r, t, q.Get("path"))
		}
	}))
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	getTorrentClientFromRequestContext(r).WriteStatus(w)
}

func infoHandler(w http.ResponseWriter, r *http.Request) {
	httptoo.WrapHandlerFunc(
		[]httptoo.Middleware{withTorrentContext},
		func(w http.ResponseWriter, r *http.Request) {
			t := torrentForRequest(r)
			select {
			case <-t.GotInfo():
			case <-r.Context().Done():
				return
			}
			mi := t.Metainfo()
			w.Write(mi.InfoBytes)
		},
	).ServeHTTP(w, r)
}
