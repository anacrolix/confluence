package confluence

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"golang.org/x/net/websocket"
)

func dataHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	t := torrentForRequest(r)
	if len(q["path"]) == 0 {
		serveTorrent(w, r, t)
	} else {
		serveFile(w, r, t, q.Get("path"))
	}
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	getHandler(r).TC.WriteStatus(w)
}

func infoHandler(w http.ResponseWriter, r *http.Request) {
	t := torrentForRequest(r)
	if nowait, err := strconv.ParseBool(r.URL.Query().Get("nowait")); err == nil && nowait {
		select {
		case <-t.GotInfo():
		default:
			http.Error(w, "info not ready", http.StatusAccepted)
			return
		}
	} else {
		// w.WriteHeader(http.StatusProcessing)
		select {
		case <-t.GotInfo():
		case <-r.Context().Done():
			return
		}
	}
	// w.WriteHeader(http.StatusOK)
	mi := t.Metainfo()
	w.Write(mi.InfoBytes)
}

func eventHandler(w http.ResponseWriter, r *http.Request) {
	t := torrentForRequest(r)
	select {
	case <-t.GotInfo():
	case <-r.Context().Done():
		return
	}
	s := t.SubscribePieceStateChanges()
	defer s.Close()
	websocket.Server{
		Handler: func(c *websocket.Conn) {
			defer c.Close()
			readClosed := make(chan struct{})
			go func() {
				defer close(readClosed)
				c.Read(nil)
			}()
			for {
				select {
				case <-readClosed:
					eventHandlerWebsocketReadClosed.Add(1)
					return
				case <-r.Context().Done():
					eventHandlerContextDone.Add(1)
					return
				case _i := <-s.Values:
					i := _i.(torrent.PieceStateChange).Index
					if err := websocket.JSON.Send(c, Event{PieceChanged: &i}); err != nil {
						if r.Context().Err() == nil {
							log.Printf("error writing json to websocket: %s", err)
						}
						return
					}
				}
			}
		},
	}.ServeHTTP(w, r)
}

func fileStateHandler(w http.ResponseWriter, r *http.Request) {
	path_ := r.URL.Query().Get("path")
	f := torrentFileByPath(torrentForRequest(r), path_)
	if f == nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(f.State())
}

func metainfoHandler(w http.ResponseWriter, r *http.Request) {
	var mi metainfo.MetaInfo
	err := bencode.NewDecoder(r.Body).Decode(&mi)
	if err != nil {
		http.Error(w, fmt.Sprintf("error decoding body: %s", err), http.StatusBadRequest)
		return
	}
	t := torrentForRequest(r)
	t.AddTrackers(mi.UpvertedAnnounceList())
	t.SetInfoBytes(mi.InfoBytes)
	saveTorrentFile(t)
}
