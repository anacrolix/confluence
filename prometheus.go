package main

import (
	"net/http"

	"github.com/anacrolix/torrent"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func init() {
	prometheus.MustRegister(prometheus.NewExpvarCollector(map[string]*prometheus.Desc{
		"torrent":   prometheus.NewDesc("expvar_torrent", "", []string{"key"}, nil),
		"go-libutp": prometheus.NewDesc("expvar_go_libutp", "", []string{"key"}, nil),
	}))
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
