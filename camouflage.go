package main

import (
	"fmt"
	"sync"

	"crawshaw.io/sqlite"
	"crawshaw.io/sqlite/sqlitex"
	"github.com/anacrolix/torrent"
	pp "github.com/anacrolix/torrent/peer_protocol"
)

// Provides torrent callbacks that can track peer information that would be useful for identifying
// camouflaging opportunities in the network.
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
		PeerConnClosed: func(pc *torrent.PeerConn) {
			me.mu.Lock()
			defer me.mu.Unlock()
			delete(me.peerConnToRowId, pc)
		},
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
