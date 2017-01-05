package main

import "net/http"

var mux = http.DefaultServeMux

func init() {
	mux.HandleFunc("/data", dataHandler)
	mux.HandleFunc("/status", statusHandler)
	mux.HandleFunc("/info", infoHandler)
	mux.HandleFunc("/events", eventHandler)
	mux.HandleFunc("/fileState", fileStateHandler)
	mux.Handle("/metainfo", metainfoHandler)
}
