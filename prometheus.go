package main

import (
	"net/http"

	xprometheus "github.com/anacrolix/missinggo/prometheus"
	"github.com/anacrolix/torrent"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func init() {
	prometheus.MustRegister(xprometheus.NewExpvarCollector())
	http.Handle("/metrics", promhttp.Handler())
}

func registerNumTorrentsMetric(cl *torrent.Client) {
	gauge := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "confluence_num_torrents",
		Help: "Number of torrents loaded into the confluence torrent client",
	}, func() float64 {
		return float64(len(cl.Torrents()))
	})
	prometheus.MustRegister(gauge)
}
