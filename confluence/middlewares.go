package confluence

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

// Returns a Torrent for the infohash with a ref that expires when the Request's context closes.
func getTorrentHandle(r *http.Request, ih metainfo.Hash) (t *torrent.Torrent, new bool) {
	h := getHandler(r)
	ref := torrentRefs.NewRef(ih)
	tc := h.TC
	t, new = tc.AddTorrentInfoHash(ih)
	ref.SetCloser(func() {
		if h.OnTorrentGrace != nil {
			h.OnTorrentGrace(t)
		}
	})
	go func() {
		defer time.AfterFunc(h.TorrentGrace, ref.Release)
		<-r.Context().Done()
	}()
	return
}

const (
	infohashQueryKey = "ih"
	magnetQueryKey   = "magnet"
)

func withTorrentContext(h http.Handler) http.Handler {
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
		t, new := getTorrentHandle(r, ih)
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
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), torrentContextKey, t)))
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
