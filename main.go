package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"crawshaw.io/sqlite"
	"github.com/anacrolix/confluence/confluence"
	_ "github.com/anacrolix/envpprof"
	utp "github.com/anacrolix/go-libutp"
	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/missinggo/v2/resource"
	"github.com/anacrolix/missinggo/x"
	"github.com/anacrolix/tagflag"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	sqliteStorage "github.com/anacrolix/torrent/storage/sqlite"
)

var flags = struct {
	Addr               string        `help:"HTTP listen address"`
	PublicIp4          net.IP        `help:"Public IPv4 address"` // TODO: Rename
	PublicIp6          net.IP        `help:"Public IPv6 address"`
	UnlimitedCache     bool          `help:"Don't limit cache capacity"`
	CacheCapacity      tagflag.Bytes `help:"Data cache capacity"`
	TorrentGrace       time.Duration `help:"How long to wait to drop a torrent after its last request"`
	FileDir            string        `help:"File-based storage directory, overrides piece storage"`
	Seed               bool          `help:"Seed data"`
	UPnPPortForwarding bool          `help:"Port forward via UPnP"`
	// You'd want this if access to the main HTTP service is trusted, such as used over localhost by
	// other known services.
	DebugOnMain      bool `help:"Expose default serve mux /debug/ endpoints over http"`
	Dht              bool
	DisableTrackers  bool     `help:"Disables all trackers"`
	TcpPeers         bool     `help:"Allow TCP peers"`
	UtpPeers         bool     `help:"Allow uTP peers"`
	ImplicitTracker  []string `help:"Trackers to be used for all torrents"`
	OverrideTrackers bool     `help:"Only use implied trackers"`
	Pex              bool

	SqliteStorage           *string
	SqliteStoragePoolSize   int
	InitSqliteStorageSchema bool

	// Attaches the camouflage data collector callbacks.
	CollectCamouflageData bool
}{
	Addr:          "localhost:8080",
	CacheCapacity: 10 << 30,
	TorrentGrace:  time.Minute,
	Dht:           true,
	TcpPeers:      true,
	UtpPeers:      true,
	Pex:           true,

	InitSqliteStorageSchema: true,
}

func newTorrentClient(storage storage.ClientImpl, callbacks torrent.Callbacks) (ret *torrent.Client, err error) {
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
	cfg.DisableTCP = !flags.TcpPeers
	cfg.DisableUTP = !flags.UtpPeers
	cfg.IPBlocklist = blocklist
	cfg.DefaultStorage = storage
	cfg.PublicIp4 = flags.PublicIp4
	cfg.PublicIp6 = flags.PublicIp6
	cfg.Seed = flags.Seed
	cfg.NoDefaultPortForwarding = !flags.UPnPPortForwarding
	cfg.NoDHT = !flags.Dht
	cfg.DisableTrackers = flags.DisableTrackers
	cfg.SetListenAddr(":50007")
	cfg.Callbacks = callbacks
	cfg.DisablePEX = !flags.Pex

	http.HandleFunc("/debug/conntrack", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		cfg.ConnTracker.PrintStatus(w)
	})

	// cfg.DisableAcceptRateLimiting = true
	return torrent.NewClient(cfg)
}

const storageRoot = "filecache"

func getStorageProvider() (_ resource.Provider, close func() error) {
	if path := flags.SqliteStorage; path != nil {
		if *path == "" {
			*path = "storage.db"
		}
		cap := flags.CacheCapacity.Int64()
		if flags.UnlimitedCache {
			cap = 0
		}
		conns, provOpts, err := sqliteStorage.NewPool(sqliteStorage.NewPoolOpts{
			Path:           *path,
			NumConns:       flags.SqliteStoragePoolSize,
			DontInitSchema: !flags.InitSqliteStorageSchema,
			Capacity:       cap,
		})
		if err != nil {
			panic(err)
		}
		if flags.UnlimitedCache {
			conn := conns.Get(context.TODO())
			defer conns.Put(conn)
			err = sqliteStorage.UnlimitCapacity(conn)
			if err != nil {
				panic(err)
			}
		}
		prov, err := sqliteStorage.NewProvider(conns, provOpts)
		if err != nil {
			panic(err)
		}
		return prov, prov.Close
	}
	if flags.UnlimitedCache {
		return resource.TranslatedProvider{
			BaseProvider: resource.OSFileProvider{},
			BaseLocation: storageRoot,
			JoinLocations: func(base, rel string) string {
				return filepath.Join(base, rel)
			},
		}, func() error { return nil }
	}
	fc, err := filecache.NewCache(storageRoot)
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
	return fc.AsResourceProvider(), func() error { return nil }
}

func getStorage() (_ storage.ClientImpl, onTorrentGrace func(torrent.InfoHash), close func() error) {
	if flags.FileDir != "" {
		return storage.NewFileByInfoHash(flags.FileDir),
			func(ih torrent.InfoHash) {
				os.RemoveAll(filepath.Join(flags.FileDir, ih.HexString()))
			},
			func() error { return nil }
	}
	prov, close := getStorageProvider()
	return storage.NewResourcePieces(prov), func(ih torrent.InfoHash) {}, close
}

func main() {
	log.SetFlags(log.Flags() | log.Lshortfile)
	tagflag.Parse(&flags)
	err := mainErr()
	if err != nil {
		log.Printf("error in main: %v", err)
		os.Exit(1)
	}
}

func mainErr() error {
	torrentCallbacks := torrent.Callbacks{}
	if flags.CollectCamouflageData {
		sqliteConn, err := sqlite.OpenConn("file:confluence.db", 0)
		if err != nil {
			return fmt.Errorf("opening confluence sqlite db: %w", err)
		}
		defer sqliteConn.Close()
		cc := camouflageCollector{
			SqliteConn: sqliteConn,
		}
		cc.Init()
		torrentCallbacks = cc.TorrentCallbacks()
	}
	storage, onTorrentGraceExtra, closeStorage := getStorage()
	defer closeStorage()
	cl, err := newTorrentClient(storage, torrentCallbacks)
	if err != nil {
		return fmt.Errorf("creating torrent client: %w", err)
	}
	defer cl.Close()
	http.HandleFunc("/debug/dht", func(w http.ResponseWriter, r *http.Request) {
		for _, ds := range cl.DhtServers() {
			ds.WriteStatus(w)
		}
	})
	http.HandleFunc("/debug/utp", func(w http.ResponseWriter, r *http.Request) {
		utp.WriteStatus(w)
	})
	l, err := net.Listen("tcp", flags.Addr)
	if err != nil {
		return fmt.Errorf("listening on addr: %w", err)
	}
	defer l.Close()
	log.Printf("serving http at %s", l.Addr())
	var h http.Handler = &confluence.Handler{
		TC:           cl,
		TorrentGrace: flags.TorrentGrace,
		OnTorrentGrace: func(t *torrent.Torrent) {
			ih := t.InfoHash()
			t.Drop()
			onTorrentGraceExtra(ih)
		},
		OnNewTorrent: func(t *torrent.Torrent, mi *metainfo.MetaInfo) {
			if !flags.OverrideTrackers && mi != nil {
				t.AddTrackers(mi.UpvertedAnnounceList())
			}
			t.AddTrackers([][]string{flags.ImplicitTracker})
		},
	}
	if flags.DebugOnMain {
		h = func() http.Handler {
			mux := http.NewServeMux()
			mux.Handle("/metrics", http.DefaultServeMux)
			mux.Handle("/debug/", http.DefaultServeMux)
			mux.Handle("/", h)
			return mux
		}()
	}
	registerNumTorrentsMetric(cl)
	return http.Serve(l, h)
}
