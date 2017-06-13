package main

import (
	"log"
	"net"
	"net/http"
	"time"

	"github.com/anacrolix/dht"
	_ "github.com/anacrolix/envpprof"
	"github.com/anacrolix/missinggo/filecache"
	"github.com/anacrolix/missinggo/x"
	"github.com/anacrolix/tagflag"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"github.com/anacrolix/torrent/storage"

	"github.com/anacrolix/confluence/confluence"
)

var flags = struct {
	Addr          string        `help:"HTTP listen address"`
	DHTPublicIP   net.IP        `help:"DHT secure IP"`
	CacheCapacity tagflag.Bytes `help:"Data cache capacity"`
	TorrentGrace  time.Duration `help:"How long to wait to drop a torrent after its last request"`
	FileDir       string        `help:"File-based storage directory, overrides piece storage"`
	Seed          bool          `help:"Seed data"`
	// You'd want this if access to the main HTTP service is trusted, such as
	// used over localhost by other known services.
	DebugOnMain bool `help:"Expose default serve mux /debug/ endpoints over http"`
}{
	Addr:          "localhost:8080",
	CacheCapacity: 10 << 30,
	TorrentGrace:  time.Minute,
}

func newTorrentClient() (ret *torrent.Client, err error) {
	blocklist, err := iplist.MMapPacked("packed-blocklist")
	if err != nil {
		log.Print(err)
	}
	storage := func() storage.ClientImpl {
		if flags.FileDir != "" {
			return storage.NewFile(flags.FileDir)
		}
		fc, err := filecache.NewCache("filecache")
		x.Pie(err)
		fc.SetCapacity(flags.CacheCapacity.Int64())
		storageProvider := fc.AsResourceProvider()
		return storage.NewResourcePieces(storageProvider)
	}()
	return torrent.NewClient(&torrent.Config{
		IPBlocklist:    blocklist,
		DefaultStorage: storage,
		DHTConfig: dht.ServerConfig{
			PublicIP: flags.DHTPublicIP,
		},
		Seed: flags.Seed,
	})
}

func main() {
	log.SetFlags(log.Flags() | log.Lshortfile)
	tagflag.Parse(&flags)
	cl, err := newTorrentClient()
	if err != nil {
		log.Fatalf("error creating torrent client: %s", err)
	}
	defer cl.Close()
	l, err := net.Listen("tcp", flags.Addr)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()
	log.Printf("serving http at %s", l.Addr())
	var h http.Handler = &confluence.Handler{cl, flags.TorrentGrace}
	if flags.DebugOnMain {
		h = func() http.Handler {
			mux := http.NewServeMux()
			mux.Handle("/debug/", http.DefaultServeMux)
			mux.Handle("/", h)
			return mux
		}()
	}
	err = http.Serve(l, h)
	if err != nil {
		log.Fatal(err)
	}
}
