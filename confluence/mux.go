package confluence

import (
	"net/http"

	"github.com/anacrolix/missinggo/httptoo"
	"github.com/justinas/alice"
)

var mux = http.NewServeMux()

func init() {
	mux.Handle("/data", alice.New(withTorrentContext).ThenFunc(dataHandler))
	mux.HandleFunc("/status", statusHandler)
	mux.Handle("/info", alice.New(withTorrentContext).ThenFunc(infoHandler))
	mux.Handle("/events", alice.New(withTorrentContext).ThenFunc(eventHandler))
	mux.Handle("/fileState", alice.New(
		withTorrentContext,
		httptoo.GzipHandler,
	).ThenFunc(fileStateHandler))
	mux.Handle("/metainfo", alice.New(withTorrentContext).ThenFunc(metainfoHandler))
}
