package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/anacrolix/confluence/confluence"
	debug_writer "github.com/anacrolix/confluence/debug-writer"
	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/int160"
	peer_store "github.com/anacrolix/dht/v2/peer-store"
	_ "github.com/anacrolix/envpprof"
	utp "github.com/anacrolix/go-libutp"
	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/missinggo/v2/resource"
	"github.com/anacrolix/missinggo/x"
	"github.com/anacrolix/publicip"
	"github.com/anacrolix/squirrel"
	"github.com/anacrolix/tagflag"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/analysis"
	"github.com/anacrolix/torrent/iplist"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	sqliteStorage "github.com/anacrolix/torrent/storage/sqlite"
	"github.com/arl/statsviz"
	"zombiezen.com/go/sqlite"
)

var flags = struct {
	Addr           string        `help:"HTTP listen address"`
	TorrentAddr    string        `default:":42069" help:"Torrent client address"`
	PublicIp4      net.IP        `help:"Public IPv4 address"` // TODO: Rename
	PublicIp6      net.IP        `help:"Public IPv6 address"`
	UnlimitedCache bool          `help:"Don't limit cache capacity"`
	CacheCapacity  tagflag.Bytes `help:"Data cache capacity"`

	TorrentGrace   time.Duration `help:"How long to wait to drop a torrent after its last request"`
	ExpireTorrents bool          `help:"Drop torrents after no use for a period"`

	FileDir            string `help:"File-based storage directory, overrides piece storage"`
	Seed               bool   `help:"Seed data"`
	UPnPPortForwarding bool   `help:"Port forward via UPnP"`
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
	InitSqliteStorageSchema bool
	SqliteJournalMode       string

	// Attaches the camouflage data collector callbacks.
	CollectCamouflageData bool

	AnalyzePeerUploadOrder bool `help:"Installs the peer upload order analysis"`
}{
	Addr:           "localhost:8080",
	CacheCapacity:  10 << 30,
	TorrentGrace:   time.Minute,
	ExpireTorrents: true,
	Dht:            true,
	TcpPeers:       true,
	UtpPeers:       true,
	Pex:            true,
	TorrentAddr:    ":42069",

	InitSqliteStorageSchema: true,
}

func newTorrentClient(
	ctx context.Context, storage storage.ClientImpl, callbacks torrent.Callbacks,
) (
	tc *torrent.Client, err error,
) {
	blocklist, err := iplist.MMapPackedFile("packed-blocklist")
	if err != nil {
		log.Print(err)
	} else {
		defer func() {
			if err != nil {
				blocklist.Close()
			} else {
				go func() {
					<-tc.Closed()
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
	if cfg.PublicIp4 == nil {
		cfg.PublicIp4, err = publicip.Get4(ctx)
		if err != nil {
			log.Printf("error getting public ipv4 address: %v", err)
		}
	}
	cfg.PublicIp6 = flags.PublicIp6
	if cfg.PublicIp6 == nil {
		cfg.PublicIp6, err = publicip.Get6(ctx)
		if err != nil {
			log.Printf("error getting public ipv6 address: %v", err)
		}
	}
	cfg.Seed = flags.Seed
	cfg.NoDefaultPortForwarding = !flags.UPnPPortForwarding
	cfg.NoDHT = !flags.Dht
	cfg.DisableTrackers = flags.DisableTrackers
	// We set this explicitly, even though it may be the default in anacrolix/torrent, as confluence
	// is typically used as a service and an unpredictable port could break things for users.
	cfg.SetListenAddr(flags.TorrentAddr)
	cfg.Callbacks = callbacks
	cfg.DisablePEX = !flags.Pex

	if flags.AnalyzePeerUploadOrder {
		var pieceOrdering analysis.PeerUploadOrder
		pieceOrdering.Init()
		pieceOrdering.Install(&cfg.Callbacks)
	}
	// cfg.DisableAcceptRateLimiting = true

	cfg.ConfigureAnacrolixDhtServer = func(cfg *dht.ServerConfig) {
		cfg.InitNodeId()
		if cfg.PeerStore == nil {
			cfg.PeerStore = &peer_store.InMemory{
				RootId: int160.FromByteArray(cfg.NodeId),
			}
		}
	}

	return torrent.NewClient(cfg)
}

const storageRoot = "filecache"

func newSquirrelCache(path string) *squirrel.Cache {
	if path == "" {
		path = "storage.db"
	}
	cap := flags.CacheCapacity.Int64()
	if flags.UnlimitedCache {
		cap = 0
	}
	var opts squirrel.NewCacheOpts
	opts.Path = path
	opts.DontInitSchema = !flags.InitSqliteStorageSchema
	opts.Capacity = cap
	opts.SetJournalMode = flags.SqliteJournalMode
	ret, err := squirrel.NewCache(opts)
	if err != nil {
		panic(err)
	}
	return ret
}

func getStorageResourceProvider() (_ resource.Provider, close func() error) {
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

func newClientStorage(squirrelCache *squirrel.Cache) (
	_ storage.ClientImpl,
	onTorrentDrop func(torrent.InfoHash), // Storage cleanup for Torrents that are dropped.
	close func() error, // Extra Client-storage-wide cleanup (for ClientImpls that need closing).
) {
	if flags.FileDir != "" {
		return storage.NewFileByInfoHash(flags.FileDir), func(ih torrent.InfoHash) {
			os.RemoveAll(filepath.Join(flags.FileDir, ih.HexString()))
		}, func() error { return nil }
	}
	if path := flags.SqliteStorage; path != nil {
		sci := sqliteStorage.NewWrappingClient(squirrelCache)
		return sci, func(torrent.InfoHash) {}, func() error { return nil }
	}
	prov, close := getStorageResourceProvider()
	return storage.NewResourcePieces(prov), func(torrent.InfoHash) {}, close
}

func main() {
	statsviz.RegisterDefault()
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
	var squirrelCache *squirrel.Cache
	if s := flags.SqliteStorage; s != nil {
		squirrelCache = newSquirrelCache(*s)
		defer squirrelCache.Close()
	}
	clientStorageImpl, onTorrentDrop, closeStorage := newClientStorage(squirrelCache)
	defer closeStorage()
	cl, err := newTorrentClient(context.TODO(), clientStorageImpl, torrentCallbacks)
	if err != nil {
		return fmt.Errorf("creating torrent client: %w", err)
	}
	defer cl.Close()
	http.HandleFunc("/debug/dht", func(w http.ResponseWriter, r *http.Request) {
		for _, ds := range cl.DhtServers() {
			ds.WriteStatus(w)
		}
	})
	http.HandleFunc("/debug/dhtPeerStores", func(w http.ResponseWriter, r *http.Request) {
		for _, ds := range cl.DhtServers() {
			fmt.Fprintf(w, "%v:\n\n", ds)
			func() {
				defer func() {
					r := recover()
					if r == nil {
						return
					}
					fmt.Fprintf(w, "panic: %v\n", r)
				}()
				ds.(torrent.PeerStorer).PeerStore().(debug_writer.Interface).WriteDebug(w)
			}()
			fmt.Fprintln(w)
		}
	})
	http.HandleFunc("/debug/utp", func(w http.ResponseWriter, r *http.Request) {
		utp.WriteStatus(w)
	})
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		cl.WriteStatus(io.Discard)
	})
	l, err := net.Listen("tcp", flags.Addr)
	if err != nil {
		return fmt.Errorf("listening on addr: %w", err)
	}
	defer l.Close()
	log.Printf("serving http at %s", l.Addr())
	ch := confluence.Handler{
		TC:           cl,
		TorrentGrace: flags.TorrentGrace,
		OnNewTorrent: func(t *torrent.Torrent, mi *metainfo.MetaInfo) {
			var spec *torrent.TorrentSpec
			if mi != nil {
				spec, _ = torrent.TorrentSpecFromMetaInfoErr(mi)
			} else {
				spec = new(torrent.TorrentSpec)
			}
			if flags.OverrideTrackers {
				spec.Trackers = nil
			}
			spec.Trackers = append(spec.Trackers, flags.ImplicitTracker)
			t.MergeSpec(spec)
		},
		MetainfoStorage: squirrelCache,
		ModifyUploadMetainfo: func(mi *metainfo.MetaInfo) {
			mi.AnnounceList = append(mi.AnnounceList, flags.ImplicitTracker)
			for _, ip := range cl.PublicIPs() {
				mi.Nodes = append(mi.Nodes, metainfo.Node(net.JoinHostPort(
					ip.String(),
					strconv.FormatInt(int64(cl.LocalPort()), 10))))
			}
		},
		Storage: storage.NewClient(clientStorageImpl),
	}
	if flags.ExpireTorrents {
		ch.OnTorrentGrace = func(t *torrent.Torrent) {
			ih := t.InfoHash()
			t.Drop()
			onTorrentDrop(ih)
		}
	}
	for _, s := range cl.DhtServers() {
		ch.DhtServers = append(ch.DhtServers, s.(torrent.AnacrolixDhtServerWrapper).Server)
	}
	var h http.Handler = &ch
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
