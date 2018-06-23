package confluence

import (
	"log"

	"github.com/anacrolix/missinggo/filecache"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"github.com/anacrolix/torrent/storage"
)

func NewDefaultTorrentClient() (ret *torrent.Client, err error) {
	blocklist, err := iplist.MMapPackedFile("packed-blocklist")
	if err != nil {
		log.Print(err)
	}
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
	fileCache, err := filecache.NewCache("filecache")
	if err != nil {
		return
	}
	fileCache.SetCapacity(10 << 30)
	storageProvider := fileCache.AsResourceProvider()
	return torrent.NewClient(&torrent.ClientConfig{
		IPBlocklist:    blocklist,
		DefaultStorage: storage.NewResourcePieces(storageProvider),
	})
}
