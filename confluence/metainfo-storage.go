package confluence

import (
	"bytes"
	"io"
	"path"

	"github.com/anacrolix/missinggo/v2/resource"
	"github.com/anacrolix/torrent/types/infohash"
)

type MetainfoStorage interface {
	Put(ih infohash.T, data []byte) error
	Get(ih infohash.T) (io.ReadCloser, error)
}

type ResourceProviderMetainfoStorage struct {
	Provider resource.Provider
	Dir      string
}

func (m ResourceProviderMetainfoStorage) pathForInfohash(ih infohash.T) string {
	// .torrent in case the provider is a filesystem.
	return path.Join(m.Dir, ih.HexString()+".torrent")
}

func (m ResourceProviderMetainfoStorage) Put(ih infohash.T, data []byte) (err error) {
	p := m.pathForInfohash(ih)
	i, err := m.Provider.NewInstance(p)
	if err != nil {
		return
	}
	return i.Put(bytes.NewReader(data))
}

func (m ResourceProviderMetainfoStorage) Get(ih infohash.T) (_ io.ReadCloser, err error) {
	p := m.pathForInfohash(ih)
	i, err := m.Provider.NewInstance(p)
	if err != nil {
		return
	}
	return i.Get()
}

var _ MetainfoStorage = ResourceProviderMetainfoStorage{}
