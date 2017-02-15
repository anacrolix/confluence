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
		tc := torrentClientForRequest(r)
		t, new := tc.AddTorrentInfoHash(ih)
		ref.SetCloser(t.Drop)
		defer time.AfterFunc(time.Minute, ref.Release)
		if new {
			mi := cachedMetaInfo(ih)
			if mi != nil {
				t.AddTrackers(mi.AnnounceList)
				t.SetInfoBytes(mi.InfoBytes)
			}
			go saveTorrentWhenGotInfo(t)
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), torrentContextKey, ref)))
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
