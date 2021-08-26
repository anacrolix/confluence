package confluence

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path"
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

func (h *Handler) saveTorrentFile(t *torrent.Torrent) error {
	var miBuf bytes.Buffer
	err := t.Metainfo().Write(&miBuf)
	if err != nil {
		return err
	}
	p := path.Join(h.metainfoCacheDir(), t.InfoHash().HexString()+".torrent")
	if h.MetainfoStorage != nil {
		return h.MetainfoStorage.Put(p, miBuf.Bytes())
	}
	os.MkdirAll(filepath.Dir(p), 0750)
	f, err := os.OpenFile(filepath.FromSlash(p), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0660)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(miBuf.Bytes())
	if err != nil {
		return err
	}
	return f.Close()
}

func ServeTorrent(w http.ResponseWriter, r *http.Request, t *torrent.Torrent) {
	select {
	case <-t.GotInfo():
	case <-r.Context().Done():
		return
	}
	ServeTorrentReader(w, r, t.NewReader(), t.Name())
}

func ServeTorrentReader(w http.ResponseWriter, r *http.Request, tr torrent.Reader, name string) {
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

func ServeFile(w http.ResponseWriter, r *http.Request, t *torrent.Torrent, _path string) {
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
	ServeTorrentReader(w, r, tf.NewReader(), _path)
}
