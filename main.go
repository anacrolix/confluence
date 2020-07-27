package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"crawshaw.io/sqlite"
	"crawshaw.io/sqlite/sqlitex"
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
	pp "github.com/anacrolix/torrent/peer_protocol"
	"github.com/anacrolix/torrent/storage"
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
}{
	Addr:          "localhost:8080",
	CacheCapacity: 10 << 30,
	TorrentGrace:  time.Minute,
	Dht:           true,
	TcpPeers:      true,
	UtpPeers:      true,
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

	http.HandleFunc("/debug/conntrack", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		cfg.ConnTracker.PrintStatus(w)
	})

	// cfg.DisableAcceptRateLimiting = true
	return torrent.NewClient(cfg)
}

const storageRoot = "filecache"

func getStorageProvider() resource.Provider {
	if flags.UnlimitedCache {
		return resource.TranslatedProvider{
			BaseProvider: resource.OSFileProvider{},
			BaseLocation: storageRoot,
			JoinLocations: func(base, rel string) string {
				return filepath.Join(base, rel)
			},
		}
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
	return fc.AsResourceProvider()
}

func getStorage() (_ storage.ClientImpl, onTorrentGrace func(torrent.InfoHash)) {
	if flags.FileDir != "" {
		return storage.NewFileByInfoHash(flags.FileDir), func(ih torrent.InfoHash) {
			os.RemoveAll(filepath.Join(flags.FileDir, ih.HexString()))
		}
	}
	return storage.NewResourcePieces(getStorageProvider()), func(ih torrent.InfoHash) {}
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
	sqliteConn, err := sqlite.OpenConn("file:confluence.db", 0)
	if err != nil {
		return fmt.Errorf("opening confluence sqlite db: %w", err)
	}
	defer sqliteConn.Close()
	cc := camouflageCollector{
		SqliteConn: sqliteConn,
	}
	cc.Init()
	storage, onTorrentGraceExtra := getStorage()
	cl, err := newTorrentClient(storage, cc.TorrentCallbacks())
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
			if !flags.OverrideTrackers {
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

type camouflageCollector struct {
	mu              sync.Mutex
	SqliteConn      *sqlite.Conn
	peerConnToRowId map[*torrent.PeerConn]int64
}

func (me *camouflageCollector) Init() {
	me.peerConnToRowId = make(map[*torrent.PeerConn]int64)
}

func (me *camouflageCollector) TorrentCallbacks() torrent.Callbacks {
	return torrent.Callbacks{
		CompletedHandshake: func(peerConn *torrent.PeerConn, infoHash torrent.InfoHash) {
			me.mu.Lock()
			defer me.mu.Unlock()
			err := sqlitex.Exec(
				me.SqliteConn,
				"insert into peers(infohash, extension_bytes, peer_id, remote_addr) values (?, ?, ?, ?)",
				nil,
				infoHash, peerConn.PeerExtensionBytes, fmt.Sprintf("%q", peerConn.PeerID[:]), peerConn.RemoteAddr.String())
			if err != nil {
				panic(err)
			}
			me.peerConnToRowId[peerConn] = me.SqliteConn.LastInsertRowID()
		},
		ReadMessage: func(conn *torrent.PeerConn, message *pp.Message) {
			if message.Type != pp.Extended || message.ExtendedID != pp.HandshakeExtendedID {
				return
			}
			me.mu.Lock()
			defer me.mu.Unlock()
			err := sqlitex.Exec(
				me.SqliteConn,
				"update peers set extended_handshake=? where rowid=?",
				nil,
				message.ExtendedPayload, me.peerConnToRowId[conn])
			if err != nil {
				panic(err)
			}
		},
		ReadExtendedHandshake: func(conn *torrent.PeerConn, p *pp.ExtendedHandshakeMessage) {
			me.mu.Lock()
			defer me.mu.Unlock()
			err := sqlitex.Exec(
				me.SqliteConn,
				"update peers set extended_v=? where rowid=?",
				nil,
				p.V, me.peerConnToRowId[conn])
			if err != nil {
				panic(err)
			}
		},
	}

}
