package confluence

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

func (h *Handler) GetTorrent(ih metainfo.Hash) (t *torrent.Torrent, new bool, release func()) {
	ref := h.torrentRefs.NewRef(ih)
	t, new = h.TC.AddTorrentInfoHash(ih)
	//log.Printf("added ref for %v", ih)
	ref.SetCloser(func() {
		//log.Printf("running torrent ref closer for %v", ih)
		if h.OnTorrentGrace != nil {
			h.OnTorrentGrace(t)
		}
	})
	release = func() {
		//log.Printf("releasing ref on %v", ih)
		time.AfterFunc(h.TorrentGrace, ref.Release)
	}
	return
}

const (
	infohashQueryKey = "ih"
	magnetQueryKey   = "magnet"
)

type request struct {
	torrent *torrent.Torrent
	handler *Handler
	*http.Request
}

func (me *Handler) withTorrentContext(h func(w http.ResponseWriter, r *request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ih, err, afterAdd := func() (ih metainfo.Hash, err error, afterAdd func(t *torrent.Torrent)) {
			q := r.URL.Query()
			ms := q.Get("magnet")
			if ms != "" {
				m, err := metainfo.ParseMagnetUri(ms)
				if err != nil {
					return metainfo.Hash{}, fmt.Errorf("parsing magnet: %w", err), nil
				}
				return m.InfoHash, nil, func(t *torrent.Torrent) {
					ts := [][]string{m.Trackers}
					//log.Printf("adding trackers %v", ts)
					t.AddTrackers(ts)
				}
			}
			if ihqv := q.Get(infohashQueryKey); ihqv != "" {
				err = ih.FromHexString(q.Get(infohashQueryKey))
				return
			}
			err = fmt.Errorf("expected nonempty query parameter %q or %q", magnetQueryKey, infohashQueryKey)
			return
		}()
		if err != nil {
			http.Error(w, fmt.Errorf("error determining requested infohash: %w", err).Error(), http.StatusBadRequest)
			return
		}
		t, new, release := me.GetTorrent(ih)
		defer release()
		if new {
			mi := me.cachedMetaInfo(ih)
			if mi != nil {
				t.SetInfoBytes(mi.InfoBytes)
			}
			if me.OnNewTorrent != nil {
				me.OnNewTorrent(t, mi)
			} else if mi != nil {
				// Retain the old behaviour.
				t.AddTrackers(mi.UpvertedAnnounceList())
			}
			go me.saveTorrentWhenGotInfo(t)
		}
		if afterAdd != nil {
			afterAdd(t)
		}
		me.saveTorrentFile(t)
		h(w, &request{t, me, r})
	})
}

func (h *Handler) saveTorrentWhenGotInfo(t *torrent.Torrent) {
	select {
	case <-t.Closed():
	case <-t.GotInfo():
	}
	err := h.saveTorrentFile(t)
	if err != nil {
		log.Printf("error saving torrent file: %s", err)
	}
}

func (h *Handler) cachedMetaInfo(infoHash metainfo.Hash) *metainfo.MetaInfo {
	p := filepath.Join(h.metainfoCacheDir(), infoHash.HexString()+".torrent")
	mi, err := metainfo.LoadFromFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		log.Printf("error loading metainfo file %q: %s", p, err)
	}
	return mi
}
