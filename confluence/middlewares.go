package confluence

import (
	"fmt"
	"log"
	"net/http"
	"os"
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
				m, err := metainfo.ParseMagnetURI(ms)
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
			mi := cachedMetaInfo(ih)
			if mi != nil {
				t.AddTrackers(mi.UpvertedAnnounceList())
				t.SetInfoBytes(mi.InfoBytes)
			}
			go saveTorrentWhenGotInfo(t)
		}
		if afterAdd != nil {
			afterAdd(t)
		}
		saveTorrentFile(t)
		h(w, &request{t, me, r})
	})
}

func saveTorrentWhenGotInfo(t *torrent.Torrent) {
	select {
	case <-t.Closed():
	case <-t.GotInfo():
	}
	err := saveTorrentFile(t)
	if err != nil {
		log.Printf("error saving torrent file: %s", err)
	}
}

func cachedMetaInfo(infoHash metainfo.Hash) *metainfo.MetaInfo {
	p := fmt.Sprintf("torrents/%s.torrent", infoHash.HexString())
	mi, err := metainfo.LoadFromFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		log.Printf("error loading metainfo file %q: %s", p, err)
	}
	return mi
}
