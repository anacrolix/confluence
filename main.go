package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	_ "github.com/anacrolix/envpprof"
	"github.com/anacrolix/missinggo"
	"github.com/anacrolix/missinggo/httptoo"
	"github.com/anacrolix/tagflag"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"golang.org/x/net/context"
)

func getTorrentClientFromRequestContext(r *http.Request) *torrent.Client {
	return r.Context().Value(torrentClientContextKey).(*torrent.Client)
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

func newTorrentClient() *torrent.Client {
	blocklist, err := iplist.MMapPacked("packed-blocklist")
	if err != nil {
		log.Print(err)
	}
	cl, err := torrent.NewClient(&torrent.Config{
		IPBlocklist: blocklist,
	})
	if err != nil {
		log.Fatal(err)
	}
	return cl
}

func main() {
	flags := struct {
		Addr string
	}{
		Addr: "localhost:8080",
	}
	tagflag.Parse(&flags)
	cl := newTorrentClient()
	defer cl.Close()
	l, err := net.Listen("tcp", flags.Addr)
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

func saveTorrentFile(t *torrent.Torrent) (err error) {
	f, err := os.OpenFile(fmt.Sprintf("torrents/%s.torrent", t.InfoHash().HexString()), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0660)
	if err != nil {
		return
	}
	defer f.Close()
	return t.Metainfo().Write(f)
}

// func fileEventHandler(w http.ResponseWriter, r *http.Request) {
// 	q := r.URL.Query()
// 	t := requestTorrent(r).Torrent
// 	f := torrentFileByPath(t, q.Get("path"))
// 	select {
// 	case <-t.GotInfo():
// 	default:
// 		http.Error(w, "missing info", http.StatusServiceUnavailable)
// 		return
// 	}
// 	pl := t.Info().PieceLength
// 	firstPiece := int(f.Offset() / pl)
// 	endPiece := int((f.Offset() + f.Length() + pl - 1) / pl)
// 	s := t.SubscribePieceStateChanges()
// 	defer s.Close()
// 	transcoderEvent, err := getTranscoderEventEvent(r.Context())
// 	if err != nil {
// 		log.Printf("failed to subscribe to transcoder events: %s", err)
// 	}
// 	websocket.Handler(func(c *websocket.Conn) {
// 		incFilePrefetch(f)
// 		defer decFilePrefetch(f)
// 		readClosed := make(chan struct{})
// 		go func() {
// 			io.Copy(ioutil.Discard, c)
// 			close(readClosed)
// 		}()
// 		defer c.Close()
// 		for {
// 			select {
// 			case _i := <-s.Values:
// 				i := _i.(torrent.PieceStateChange).Index
// 				if i < firstPiece || i >= endPiece {
// 					break
// 				}
// 				if err := websocket.JSON.Send(c, fileEvent{PieceChanged: &i}); err != nil {
// 					log.Printf("error writing json to websocket: %s", err)
// 					return
// 				}
// 			case <-transcoderEvent.C():
// 				// log.Printf("file event handler got transcoder event")
// 				transcoderEvent.Clear()
// 				if err := websocket.JSON.Send(c, fileEvent{PreviewProgress: &struct{}{}}); err != nil {
// 					log.Printf("error writing json to websocket: %s", err)
// 					return
// 				}
// 			case <-readClosed:
// 				return
// 			}
// 		}
// 	}).ServeHTTP(w, r)
// }
