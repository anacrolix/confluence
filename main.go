package main

import (
	"log"
	"net"
	"net/http"

	"github.com/anacrolix/dht"
	_ "github.com/anacrolix/envpprof"
	"github.com/anacrolix/missinggo/filecache"
	"github.com/anacrolix/tagflag"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"github.com/anacrolix/torrent/storage"

	"github.com/anacrolix/confluence/confluence"
)

var flags = struct {
	Addr          string
	DHTPublicIP   net.IP
	CacheCapacity tagflag.Bytes
}{
	Addr:          "localhost:8080",
	CacheCapacity: 10 << 30,
}

func newTorrentClient() (ret *torrent.Client, err error) {
	blocklist, err := iplist.MMapPacked("packed-blocklist")
	if err != nil {
		log.Print(err)
	}
	fc := filecache.NewCache("filecache")
	fc.SetCapacity(flags.CacheCapacity.Int64())
	storageProvider := fc.AsResourceProvider()
	return torrent.NewClient(&torrent.Config{
		IPBlocklist:    blocklist,
		DefaultStorage: storage.NewResourcePieces(storageProvider),
		DHTConfig: dht.ServerConfig{
			PublicIP: flags.DHTPublicIP,
		},
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
	h := &confluence.Handler{cl}
	err = http.Serve(l, h)
	if err != nil {
		log.Fatal(err)
	}
}
