module github.com/anacrolix/confluence

require (
	crawshaw.io/sqlite v0.3.2
	github.com/anacrolix/dht/v2 v2.6.1 // indirect
	github.com/anacrolix/envpprof v1.1.0
	github.com/anacrolix/go-libutp v1.0.3
	github.com/anacrolix/missinggo v1.2.1
	github.com/anacrolix/missinggo/v2 v2.4.1-0.20200419051441-747d9d7544c6
	github.com/anacrolix/tagflag v1.1.1-0.20200411025953-9bb5209d56c2
	github.com/anacrolix/torrent v1.18.1-0.20201103041712-96b640065a9f
	github.com/prometheus/client_golang v1.5.1
	golang.org/x/net v0.0.0-20200501053045-e0ff5e5a1de5
)

go 1.13

replace crawshaw.io/sqlite => github.com/zombiezen/sqlite v0.3.3-0.20200630223153-bdd2fdca1601
