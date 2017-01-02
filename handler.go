package main

import (
	"context"
	"net/http"

	"github.com/anacrolix/missinggo/refclose"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

var (
	torrentClientContextKey = new(byte)
	torrentContextKey       = new(byte)
	torrentRefs             refclose.RefPool
)

type handler struct {
	tc *torrent.Client
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), torrentClientContextKey, h.tc))
	mux.ServeHTTP(w, r)
}

func torrentClientForRequest(r *http.Request) *torrent.Client {
	return r.Context().Value(torrentClientContextKey).(*torrent.Client)
}

func torrentForRequest(r *http.Request) *torrent.Torrent {
	ih := r.Context().Value(torrentContextKey).(*refclose.Ref).Key().(metainfo.Hash)
	t, ok := torrentClientForRequest(r).Torrent(ih)
	if !ok {
		panic(ih)
	}
	return t
}
