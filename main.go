package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/confluence/confluence"
	_ "github.com/anacrolix/envpprof"
	"github.com/anacrolix/missinggo/filecache"
	"github.com/anacrolix/missinggo/x"
	"github.com/anacrolix/tagflag"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"github.com/anacrolix/torrent/storage"
)

var flags = struct {
	Addr               string        `help:"HTTP listen address"`
	PublicIp4          net.IP        `help:"Public IPv4 address"` // TODO: Rename
	PublicIp6          net.IP        `help:"Public IPv6 address"`
	CacheCapacity      tagflag.Bytes `help:"Data cache capacity"`
	TorrentGrace       time.Duration `help:"How long to wait to drop a torrent after its last request"`
	FileDir            string        `help:"File-based storage directory, overrides piece storage"`
	Seed               bool          `help:"Seed data"`
	UPnPPortForwarding bool          `help:"Port forward via UPnP"`
	// You'd want this if access to the main HTTP service is trusted, such as
	// used over localhost by other known services.
	DebugOnMain bool `help:"Expose default serve mux /debug/ endpoints over http"`
	Dht         bool
}{
	Addr:          "localhost:8080",
	CacheCapacity: 10 << 30,
	TorrentGrace:  time.Minute,
	Dht:           true,
}

func newTorrentClient(storage storage.ClientImpl) (ret *torrent.Client, err error) {
	blocklist, err := iplist.MMapPackedFile("packed-blocklist")
	if err != nil {
		log.Print(err)
	} else {
		defer func() {
			if err != nil {
				blocklist.Close()
			} else {
				go func() {
					<-ret.Closed()
					blocklist.Close()
				}()
			}
		}()
	}
	cfg := torrent.NewDefaultClientConfig()
	cfg.IPBlocklist = blocklist
	cfg.DefaultStorage = storage
	cfg.PublicIp4 = flags.PublicIp4
	cfg.PublicIp6 = flags.PublicIp6
	cfg.Seed = flags.Seed
	cfg.NoDefaultPortForwarding = !flags.UPnPPortForwarding
	cfg.NoDHT = !flags.Dht
	cfg.SetListenAddr(":50007")
	http.HandleFunc("/debug/conntrack", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		cfg.ConnTracker.PrintStatus(w)
	})

	// cfg.DisableAcceptRateLimiting = true
	return torrent.NewClient(cfg)
}

func getStorage() (_ storage.ClientImpl, onTorrentGrace func(torrent.InfoHash)) {
	if flags.FileDir != "" {
		return storage.NewFileByInfoHash(flags.FileDir), func(ih torrent.InfoHash) {
			os.RemoveAll(filepath.Join(flags.FileDir, ih.HexString()))
		}
	}
	fc, err := filecache.NewCache("filecache")
	x.Pie(err)

	// Register filecache debug endpoints on the default muxer.
	http.HandleFunc("/debug/filecache/status", func(w http.ResponseWriter, r *http.Request) {
		info := fc.Info()
		fmt.Fprintf(w, "Capacity: %d\n", info.Capacity)
		fmt.Fprintf(w, "Current Size: %d\n", info.Filled)
		fmt.Fprintf(w, "Item Count: %d\n", info.NumItems)
	})
	http.HandleFunc("/debug/filecache/lru", func(w http.ResponseWriter, r *http.Request) {
		fc.WalkItems(func(item filecache.ItemInfo) {
			fmt.Fprintf(w, "%s\t%d\t%s\n", item.Accessed, item.Size, item.Path)
		})
	})

	fc.SetCapacity(flags.CacheCapacity.Int64())
	storageProvider := fc.AsResourceProvider()
	return storage.NewResourcePieces(storageProvider), func(ih torrent.InfoHash) {}
}

func main() {
	log.SetFlags(log.Flags() | log.Lshortfile)
	tagflag.Parse(&flags)
	storage, onTorrentGraceExtra := getStorage()
	cl, err := newTorrentClient(storage)
	if err != nil {
		log.Fatalf("error creating torrent client: %s", err)
	}
	defer cl.Close()
	http.HandleFunc("/debug/dht", func(w http.ResponseWriter, r *http.Request) {
		for _, ds := range cl.DhtServers() {
			ds.WriteStatus(w)
		}
	})
	l, err := net.Listen("tcp", flags.Addr)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()
	log.Printf("serving http at %s", l.Addr())
	var h http.Handler = &confluence.Handler{
		cl,
		flags.TorrentGrace,
		func(t *torrent.Torrent) {
			ih := t.InfoHash()
			t.Drop()
			onTorrentGraceExtra(ih)
		},
	}
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
