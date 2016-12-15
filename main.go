package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	_ "github.com/anacrolix/envpprof"
	"github.com/anacrolix/missinggo"
	"github.com/anacrolix/missinggo/httptoo"
	"github.com/anacrolix/missinggo/refclose"
	"github.com/anacrolix/tagflag"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"golang.org/x/net/context"
)

var (
	tcReqCtxKey = new(struct{})
	torrentKey  = new(struct{})
	mux         = http.DefaultServeMux
	torrentRefs refclose.RefPool
)

func init() {
	mux.HandleFunc("/data", dataHandler)
	mux.HandleFunc("/status", statusHandler)
}

type handler struct {
	tc *torrent.Client
}

func getTorrentClientFromRequestContext(r *http.Request) *torrent.Client {
	return r.Context().Value(tcReqCtxKey).(*torrent.Client)
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), torrentKey, h.tc))
	mux.ServeHTTP(w, r)
}

func dataHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var ih metainfo.Hash
	err := ih.FromHexString(q.Get("ih"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ref := torrentRefs.NewRef(ih)
	tc := r.Context().Value(tcReqCtxKey).(*torrent.Client)
	t, _ := tc.AddTorrentInfoHash(ih)
	ref.SetCloser(t.Drop)
	defer time.AfterFunc(time.Minute, ref.Release)
	w.Header().Set("Content-Disposition", "inline")
	if len(q["path"]) == 0 {
		serveTorrent(w, r, t)
	} else {
		serveFile(w, r, t, q.Get("path"))
	}
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	getTorrentClientFromRequestContext(r).WriteStatus(w)
}

func serveTorrent(w http.ResponseWriter, r *http.Request, t *torrent.Torrent) {
	select {
	case <-t.GotInfo():
	case <-r.Context().Done():
		return
	}
	serveTorrentSection(w, r, t, 0, t.Length(), t.Name())
}

func serveTorrentSection(w http.ResponseWriter, r *http.Request, t *torrent.Torrent, offset, length int64, name string) {
	tr := t.NewReader()
	defer tr.Close()
	tr.SetReadahead(48 << 20)
	rs := missinggo.NewSectionReadSeeker(struct {
		io.Reader
		io.Seeker
	}{
		Reader: readContexter{
			r: tr,
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cn := w.(http.CloseNotifier).CloseNotify()
				go func() {
					select {
					case <-cn:
						cancel()
					case <-r.Context().Done():
					}
				}()
				return ctx
			}(),
		},
		Seeker: tr,
	}, offset, length)
	http.ServeContent(w, r, name, time.Time{}, rs)
}

func serveFile(w http.ResponseWriter, r *http.Request, t *torrent.Torrent, _path string) {
	tf := torrentFileByPath(t, _path)
	if tf == nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	w.Header().Set("ETag", httptoo.EncodeQuotedString(fmt.Sprintf("%s/%s", t.InfoHash().HexString(), _path)))
	serveTorrentSection(w, r, t, tf.Offset(), tf.Length(), _path)
}

func main() {
	tagflag.Parse(nil)
	cl, err := torrent.NewClient(nil)
	if err != nil {
		log.Fatal(err)
	}
	defer cl.Close()
	l, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()
	log.Printf("serving http at %s", l.Addr())
	h := &handler{cl}
	err = http.Serve(l, h)
	if err != nil {
		log.Fatal(err)
	}
}

type readContexter struct {
	r interface {
		ReadContext([]byte, context.Context) (int, error)
	}
	ctx context.Context
}

func (me readContexter) Read(b []byte) (int, error) {
	return me.r.ReadContext(b, me.ctx)
}

// Path is the given request path.
func torrentFileByPath(t *torrent.Torrent, path_ string) *torrent.File {
	for _, f := range t.Files() {
		if f.DisplayPath() == path_ {
			return &f
		}
	}
	return nil
}
