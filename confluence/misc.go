package confluence

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/missinggo"
	"github.com/anacrolix/missinggo/httptoo"
	"github.com/anacrolix/torrent"
)

// Path is the given request path.
func torrentFileByPath(t *torrent.Torrent, path_ string) *torrent.File {
	for _, f := range t.Files() {
		if f.DisplayPath() == path_ {
			return f
		}
	}
	return nil
}

func saveTorrentFile(t *torrent.Torrent) (err error) {
	path_ := filepath.Join("torrents", t.InfoHash().HexString()+".torrent")
	os.MkdirAll(filepath.Dir(path_), 0750)
	f, err := os.OpenFile(path_, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0660)
	if err != nil {
		return
	}
	defer f.Close()
	return t.Metainfo().Write(f)
}

func serveTorrent(w http.ResponseWriter, r *http.Request, t *torrent.Torrent) {
	select {
	case <-t.GotInfo():
	case <-r.Context().Done():
		return
	}
	serveReader(w, r, t.NewReader(), t.Name())
}

func serveReader(w http.ResponseWriter, r *http.Request, tr torrent.Reader, name string) {
	defer tr.Close()
	tr.SetReadahead(48 << 20)
	rs := struct {
		io.Reader
		io.Seeker
	}{
		Reader: missinggo.ContextedReader{
			R: tr,
			// From Go 1.8, the Request Context is done when the client goes
			// away.
			Ctx: r.Context(),
		},
		Seeker: tr,
	}
	http.ServeContent(w, r, name, time.Time{}, rs)
}

func serveFile(w http.ResponseWriter, r *http.Request, t *torrent.Torrent, _path string) {
	select {
	case <-r.Context().Done():
		http.Error(w, "request canceled", httptoo.StatusClientCancelledRequest)
		return
	case <-t.GotInfo():
	}
	tf := torrentFileByPath(t, _path)
	if tf == nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	// w.Header().Set("ETag", httptoo.EncodeQuotedString(fmt.Sprintf("%s/%s", t.InfoHash().HexString(), _path)))
	serveReader(w, r, tf.NewReader(), _path)
}
