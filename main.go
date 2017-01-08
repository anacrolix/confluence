package main

import (
	"log"
	"net"
	"net/http"

	_ "github.com/anacrolix/envpprof"
	"github.com/anacrolix/tagflag"

	"github.com/anacrolix/confluence/confluence"
)

func main() {
	log.SetFlags(log.Flags() | log.Lshortfile)
	flags := struct {
		Addr string
	}{
		Addr: "localhost:8080",
	}
	tagflag.Parse(&flags)
	cl, err := confluence.NewDefaultTorrentClient()
	if err != nil {
		log.Fatalf("error creating torrent client: %s", err)
	}
	defer cl.Close()
	l, err := net.Listen("tcp", flags.Addr)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()
	log.Printf("serving http at %s", l.Addr())
	h := &confluence.Handler{cl}
	err = http.Serve(l, h)
	if err != nil {
		log.Fatal(err)
	}
}
