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

func dataHandler(w http.ResponseWriter, r *request) {
	q := r.URL.Query()
	t := r.torrent
	if q.Has("filename") {
		w.Header().Set(
			"Content-Disposition",
			fmt.Sprintf(`attachment; filename="%s"`, q.Get("filename")),
		)
	}
	if len(q["path"]) == 0 {
		ServeTorrent(w, r.Request, t)
	} else {
		ServeFile(w, r.Request, t, q.Get("path"))
	}
}

func (h *Handler) statusHandler(w http.ResponseWriter, r *http.Request) {
	h.TC.WriteStatus(w)
}

func waitForTorrentInfo(w http.ResponseWriter, r *request) bool {
	t := r.torrent
	if nowait, err := strconv.ParseBool(r.URL.Query().Get("nowait")); err == nil && nowait {
		select {
		case <-t.GotInfo():
		default:
			http.Error(w, "info not ready", http.StatusAccepted)
			return false
		}
	} else {
		select {
		case <-t.GotInfo():
		case <-r.Context().Done():
			return false
		}
	}
	return true
}

func infoHandler(w http.ResponseWriter, r *request) {
	if !waitForTorrentInfo(w, r) {
		return
	}
	mi := r.torrent.Metainfo()
	w.Write(mi.InfoBytes)
}

func eventHandler(w http.ResponseWriter, r *request) {
	t := r.torrent
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
				case _i, ok := <-s.Values:
					if !ok {
						log.Printf("event handler subscription closed for %v; returning", t.InfoHash())
						return
					}
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
	}.ServeHTTP(w, r.Request)
}

func fileStateHandler(w http.ResponseWriter, r *request) {
	path_ := r.URL.Query().Get("path")
	f := torrentFileByPath(r.torrent, path_)
	if f == nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(f.State())
}

func (h *Handler) metainfoHandler(w http.ResponseWriter, r *request) {
	if r.Method == "POST" {
		h.metainfoPostHandler(w, r)
		return
	}

	if !waitForTorrentInfo(w, r) {
		return
	}
	mi := r.torrent.Metainfo()

	switch r.Header.Get("Accept") {
	case "application/json":
		w.Header().Add("Content-Type", "application/json")
		nodes := make([]string, len(mi.Nodes))
		for _, n := range mi.Nodes {
			nodes = append(nodes, string(n))
		}
		enc := json.NewEncoder(w)
		enc.Encode(struct {
			Info         []byte     `json:"info,omitempty"`
			Announce     string     `json:"announce,omitempty"`
			AnnounceList [][]string `json:"announceList,omitempty"`
			Nodes        []string   `json:"nodes,omitempty"`
			CreationDate int64      `json:"creationDate,omitempty"`
			Comment      string     `json:"comment,omitempty"`
			CreatedBy    string     `json:"createdBy,omitempty"`
			Encoding     string     `json:"encoding,omitempty"`
			UrlList      []string   `json:"urlList,omitempty"`
		}{
			Info:         mi.InfoBytes,
			Announce:     mi.Announce,
			AnnounceList: mi.AnnounceList,
			Nodes:        nodes,
			CreationDate: mi.CreationDate,
			Comment:      mi.Comment,
			CreatedBy:    mi.CreatedBy,
			Encoding:     mi.Encoding,
			UrlList:      mi.UrlList,
		})
	default:
		w.Header().Add("Content-Type", "application/x-bittorrent")
		mi.Write(w)
	}
}

func (h *Handler) metainfoPostHandler(w http.ResponseWriter, r *request) {
	var mi metainfo.MetaInfo
	err := bencode.NewDecoder(r.Body).Decode(&mi)
	if err != nil {
		http.Error(w, fmt.Sprintf("error decoding body: %s", err), http.StatusBadRequest)
		return
	}
	h.PutMetainfo(r.torrent, &mi)
}

// We require the Torrent to be given to ensure we don't infer a torrent from the MetaInfo without
// any release semantics.
func (h *Handler) PutMetainfo(t *torrent.Torrent, mi *metainfo.MetaInfo) error {
	// TODO(anacrolix): Should probably extract merge-style behaviour that Client.AddTorrent
	// contains.
	t.AddTrackers(mi.UpvertedAnnounceList())
	err := t.SetInfoBytes(mi.InfoBytes)
	if err != nil {
		return err
	}
	return h.saveTorrentFile(t)
}
