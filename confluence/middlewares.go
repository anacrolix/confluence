package confluence

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/anacrolix/missinggo/refclose"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

const infohashQueryKey = "ih"

func infohashFromQueryOrServeError(w http.ResponseWriter, q url.Values) (ih metainfo.Hash, ok bool) {
	if err := ih.FromHexString(q.Get(infohashQueryKey)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ok = true
	return
}

// Handles ref counting, close grace, and various torrent client wrapping
// work.
func getTorrentHandle(r *http.Request, ih metainfo.Hash) *torrent.Torrent {
	var ref *refclose.Ref
	grace := torrentCloseGraceForRequest(r)
	if grace >= 0 {
		ref = torrentRefs.NewRef(ih)
	}
	tc := torrentClientForRequest(r)
	t, new := tc.AddTorrentInfoHash(ih)
	if grace >= 0 {
		ref.SetCloser(t.Drop)
		go func() {
			defer time.AfterFunc(grace, ref.Release)
			<-r.Context().Done()
		}()
	}
	if new {
		mi := cachedMetaInfo(ih)
		if mi != nil {
			t.AddTrackers(mi.UpvertedAnnounceList())
			t.SetInfoBytes(mi.InfoBytes)
		}
		go saveTorrentWhenGotInfo(t)
	}
	return t
}

func withTorrentContext(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ih, ok := infohashFromQueryOrServeError(w, r.URL.Query())
		if !ok {
			return
		}
		t := getTorrentHandle(r, ih)
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
