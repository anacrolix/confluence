package main

import (
	"context"
	"net/http"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

func withTorrentContext(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var ih metainfo.Hash
		err := ih.FromHexString(q.Get("ih"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ref := torrentRefs.NewRef(ih)
		tc := r.Context().Value(torrentClientContextKey).(*torrent.Client)
		t, _ := tc.AddTorrentInfoHash(ih)
		ref.SetCloser(t.Drop)
		defer time.AfterFunc(time.Minute, ref.Release)
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), torrentContextKey, ref)))
	})
}
