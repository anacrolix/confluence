package main

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/anacrolix/missinggo/httptoo"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/justinas/alice"
	"golang.org/x/net/websocket"

	"github.com/anacrolix/confluence/confluence"
)

func dataHandler(w http.ResponseWriter, r *http.Request) {
	httptoo.WrapHandler([]httptoo.Middleware{withTorrentContext}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		t := torrentForRequest(r)
		if len(q["path"]) == 0 {
			serveTorrent(w, r, t)
		} else {
			serveFile(w, r, t, q.Get("path"))
		}
	})).ServeHTTP(w, r)
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

func eventHandler(w http.ResponseWriter, r *http.Request) {
	httptoo.RunHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := torrentForRequest(r)
		select {
		case <-t.GotInfo():
		case <-r.Context().Done():
			return
		}
		s := t.SubscribePieceStateChanges()
		defer s.Close()
		websocket.Handler(func(c *websocket.Conn) {
			readClosed := make(chan struct{})
			go func() {
				io.Copy(ioutil.Discard, c)
				close(readClosed)
			}()
			defer c.Close()
			for {
				select {
				case _i := <-s.Values:
					i := _i.(torrent.PieceStateChange).Index
					if err := websocket.JSON.Send(c, confluence.Event{PieceChanged: &i}); err != nil {
						log.Printf("error writing json to websocket: %s", err)
						return
					}
				case <-readClosed:
					return
				}
			}
		}).ServeHTTP(w, r)
	}, w, r, withTorrentContext)
}

func fileStateHandler(w http.ResponseWriter, r *http.Request) {
	httptoo.RunHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path_ := r.URL.Query().Get("path")
		f := torrentFileByPath(torrentForRequest(r), path_)
		json.NewEncoder(w).Encode(f.State())
	}, w, r, withTorrentContext)
}

var metainfoHandler = alice.New(withTorrentContext).ThenFunc(func(w http.ResponseWriter, r *http.Request) {
	var mi metainfo.MetaInfo
	err := bencode.NewDecoder(r.Body).Decode(&mi)
	if err != nil {
		http.Error(w, fmt.Sprintf("error decoding body: %s", err), http.StatusBadRequest)
		return
	}
	t := torrentForRequest(r)
	t.AddTrackers(mi.AnnounceList)
	t.SetInfoBytes(mi.InfoBytes)
	saveTorrentFile(t)
})
