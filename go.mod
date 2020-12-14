module github.com/anacrolix/confluence

require (
	crawshaw.io/sqlite v0.3.3-0.20201116053540-c582b9de4f93
	github.com/anacrolix/envpprof v1.1.0
	github.com/anacrolix/go-libutp v1.0.3
	github.com/anacrolix/missinggo v1.2.1
	github.com/anacrolix/missinggo/v2 v2.5.0
	github.com/anacrolix/tagflag v1.1.1-0.20200411025953-9bb5209d56c2
	github.com/anacrolix/torrent v1.18.1-0.20201210001345-de964db3c2eb
	github.com/prometheus/client_golang v1.5.1
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	golang.org/x/net v0.0.0-20201209123823-ac852fbbde11
)

go 1.13

replace crawshaw.io/sqlite => github.com/getlantern/sqlite v0.3.3-0.20201117072544-3704b1343133
