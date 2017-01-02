package main

import "net/http"

var mux = http.DefaultServeMux

func init() {
	mux.HandleFunc("/data", dataHandler)
	mux.HandleFunc("/status", statusHandler)
	mux.HandleFunc("/info", infoHandler)
}
