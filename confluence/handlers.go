package confluence

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/anacrolix/missinggo/v2/httptoo"
	"github.com/anacrolix/missinggo/v2/panicif"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/dht/v2/bep44"
	"github.com/anacrolix/dht/v2/exts/getput"
	"github.com/anacrolix/log"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"golang.org/x/net/websocket"
)

const filePathQueryKey = "path"

func dataQueryHandler(w http.ResponseWriter, r *request) {
	q := r.URL.Query()
	dataHandler(w, r, q.Get(filePathQueryKey),
		// I'm not sure if we can use q.Has for this test, and the behaviour might differ.
		len(q[filePathQueryKey]) != 0)
}

func dataPathHandler(w http.ResponseWriter, r *request) {
	dp := strings.TrimPrefix(r.URL.Path, "/")
	dataHandler(w, r, dp, len(dp) != 0)
}

func setFilenameContentDisposition(w http.ResponseWriter, filename string) {
	w.Header().Set("Content-Disposition", "filename="+strconv.Quote(filename))
}

func dataHandler(w http.ResponseWriter, r *request,
	// TODO: Use a generic Option type.
	filePath string, filePathOk bool,
) {
	q := r.URL.Query()
	t := r.torrent
	const filenameQueryKey = "filename"
	hasFilename := q.Has(filenameQueryKey)
	if hasFilename {
		setFilenameContentDisposition(w, q.Get(filenameQueryKey))
	}
	if !filePathOk {
		ServeTorrent(w, r.Request, t)
		return
	}
	if !hasFilename {
		setFilenameContentDisposition(w, filePath)
	}
	ServeFile(w, r.Request, t, filePath)
}

func (h *Handler) statusHandler(w http.ResponseWriter, r *http.Request) {
	h.TC.WriteStatus(w)
}

func waitForTorrentInfo(w http.ResponseWriter, r *request) bool {
	t := r.torrent
	if nowait, err := strconv.ParseBool(r.URL.Query().Get("nowait")); err == nil && nowait {
		select {
		case <-t.GotInfo():
		default:
			http.Error(w, "info not ready", http.StatusAccepted)
			return false
		}
	} else {
		select {
		case <-t.GotInfo():
		case <-r.Context().Done():
			return false
		}
	}
	return true
}

func infoHandler(w http.ResponseWriter, r *request) {
	if !waitForTorrentInfo(w, r) {
		return
	}
	mi := r.torrent.Metainfo()
	w.Write(mi.InfoBytes)
}

func eventHandler(w http.ResponseWriter, r *request) {
	t := r.torrent
	select {
	case <-t.GotInfo():
	case <-r.Context().Done():
		return
	}
	s := t.SubscribePieceStateChanges()
	defer s.Close()
	websocket.Server{
		Handler: func(c *websocket.Conn) {
			defer c.Close()
			readClosed := make(chan struct{})
			go func() {
				defer close(readClosed)
				c.Read(nil)
			}()
			for {
				select {
				case <-readClosed:
					eventHandlerWebsocketReadClosed.Add(1)
					return
				case <-r.Context().Done():
					eventHandlerContextDone.Add(1)
					return
				case i, ok := <-s.Values:
					if !ok {
						log.Printf("event handler subscription closed for %v; returning", t.InfoHash())
						return
					}
					if err := websocket.JSON.Send(c, Event{PieceChanged: &i.Index}); err != nil {
						if r.Context().Err() == nil {
							log.Printf("error writing json to websocket: %s", err)
						}
						return
					}
				}
			}
		},
	}.ServeHTTP(w, r.Request)
}

func fileStateHandler(w http.ResponseWriter, r *request) {
	path_ := r.URL.Query().Get(filePathQueryKey)
	select {
	case <-r.Context().Done():
		http.Error(w, "request canceled", httptoo.StatusClientCancelledRequest)
		return
	case <-r.torrent.GotInfo():
	}
	f := torrentFileByPath(r.torrent, path_)
	if f == nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	panicif.NotNil(json.NewEncoder(w).Encode(f.State()))
}

func (h *Handler) metainfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		h.metainfoPostHandler(w, r)
		return
	}
	h.withTorrentContextFromQuery(h.contextedMetainfoHandler).ServeHTTP(w, r)
}

func (h *Handler) contextedMetainfoHandler(w http.ResponseWriter, r *request) {

	if !waitForTorrentInfo(w, r) {
		return
	}
	mi := r.torrent.Metainfo()

	switch r.Header.Get("Accept") {
	case "application/json":
		w.Header().Add("Content-Type", "application/json")
		nodes := make([]string, len(mi.Nodes))
		for _, n := range mi.Nodes {
			nodes = append(nodes, string(n))
		}
		enc := json.NewEncoder(w)
		enc.Encode(struct {
			Info         []byte     `json:"info,omitempty"`
			Announce     string     `json:"announce,omitempty"`
			AnnounceList [][]string `json:"announceList,omitempty"`
			Nodes        []string   `json:"nodes,omitempty"`
			CreationDate int64      `json:"creationDate,omitempty"`
			Comment      string     `json:"comment,omitempty"`
			CreatedBy    string     `json:"createdBy,omitempty"`
			Encoding     string     `json:"encoding,omitempty"`
			UrlList      []string   `json:"urlList,omitempty"`
		}{
			Info:         mi.InfoBytes,
			Announce:     mi.Announce,
			AnnounceList: mi.AnnounceList,
			Nodes:        nodes,
			CreationDate: mi.CreationDate,
			Comment:      mi.Comment,
			CreatedBy:    mi.CreatedBy,
			Encoding:     mi.Encoding,
			UrlList:      mi.UrlList,
		})
	default:
		w.Header().Add("Content-Type", "application/x-bittorrent")
		mi.Write(w)
	}
}

func (h *Handler) metainfoPostHandler(w http.ResponseWriter, r *http.Request) {
	var mi metainfo.MetaInfo
	err := bencode.NewDecoder(r.Body).Decode(&mi)
	if err != nil {
		http.Error(w, fmt.Sprintf("error decoding body: %s", err), http.StatusBadRequest)
		return
	}
	h.withTorrentContext(
		func(w http.ResponseWriter, r *request) {
			err := h.PutMetainfo(r.torrent, &mi)
			if err != nil {
				http.Error(w, fmt.Sprintf("error putting metainfo: %s", err), http.StatusInternalServerError)
				return
			}
			fmt.Fprintln(w, r.torrent.InfoHash().HexString())
		},
		func() (ih metainfo.Hash, err error, afterAdd func(t *torrent.Torrent)) {
			ih = mi.HashInfoBytes()
			return
		},
	).ServeHTTP(w, r)
}

// We require the Torrent to be given to ensure we don't infer a torrent from the MetaInfo without
// any release semantics. A torrent is needed to merge in the spec from the metainfo, and then to save the merged
// metainfo.
func (h *Handler) PutMetainfo(t *torrent.Torrent, mi *metainfo.MetaInfo) error {
	spec, _ := torrent.TorrentSpecFromMetaInfoErr(mi)
	err := t.MergeSpec(spec)
	if err != nil {
		return err
	}
	return h.saveTorrentFile(t)
}

func (h *Handler) handleBep44(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	var target bep44.Target
	targetBytes, err := hex.DecodeString(r.FormValue("target"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if copy(target[:], targetBytes) != len(target) {
		http.Error(w, "target has bad length", http.StatusBadRequest)
		return
	}
	if len(h.DhtServers) == 0 {
		http.Error(w, "no dht servers", http.StatusInternalServerError)
		return
	}
	var wg sync.WaitGroup
	resChan := make(chan getput.GetResult, len(h.DhtServers))
	wgDoneChan := make(chan struct{})
	for _, s := range h.DhtServers {
		s := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, _, err := getput.Get(r.Context(), target, s, nil, []byte(r.FormValue("salt")))
			if err != nil {
				h.Logger.Levelf(log.Debug, "error getting %x from %v: %v", target, s, err)
				return
			}
			resChan <- res
		}()
	}
	go func() {
		wg.Wait()
		close(wgDoneChan)
	}()
	select {
	case res := <-resChan:
		bencode.NewEncoder(w).Encode(res.V)
	case <-wgDoneChan:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *Handler) uploadHandler(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		err = fmt.Errorf("parsing multipart form: %w", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	info := metainfo.Info{
		Name: r.MultipartForm.Value["name"][0],
	}
	//spew.Dump(r.MultipartForm)
	files := r.MultipartForm.File["files"]
	// Raw HTML directory file uploads don't support mixing files and directories with a single
	// chooser. So if you need to select everything inside a directory, you have to upload the
	// parent directory itself. This option strips that top-level directory name.
	stripTopDirectory := len(r.MultipartForm.Value["strip-top-directory"]) > 0
	for _, fh := range files {
		_, params, err := mime.ParseMediaType(fh.Header.Get("Content-Disposition"))
		if err != nil {
			err = fmt.Errorf("parsing file content-disposition: %w", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		filename, ok := params["filename"]
		if !ok {
			http.Error(w, "missing filename in Content-Disposition", http.StatusBadRequest)
			return
		}
		// Can't use multipart.FileHeader.Filename because it's stripped of directory components.
		path := strings.Split(filename, "/")
		// If the path only has a single component, it's a file in the top-level directory.
		if len(path) > 1 && stripTopDirectory {
			path = path[1:]
		}
		info.Files = append(info.Files, metainfo.FileInfo{
			Length:   fh.Size,
			Path:     path,
			PathUtf8: path,
		})
	}
	info.PieceLength = metainfo.ChoosePieceLength(info.TotalLength())
	piecesReader, piecesWriter := io.Pipe()
	generatePiecesErrChan := make(chan error, 1)
	go func() {
		var err error
		info.Pieces, err = metainfo.GeneratePieces(piecesReader, info.PieceLength, nil)
		generatePiecesErrChan <- err
	}()
	err = writeMultipartFiles(piecesWriter, files)
	piecesWriter.Close()
	if err != nil {
		err = fmt.Errorf("writing files to piece generator: %w", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	generatePiecesErr := <-generatePiecesErrChan
	if generatePiecesErr != nil {
		panic(generatePiecesErr)
	}
	mi := metainfo.MetaInfo{
		InfoBytes:    bencode.MustMarshal(info),
		CreatedBy:    "anacrolix/confluence upload",
		CreationDate: time.Now().Unix(),
	}
	// Save before running Handler.ModifyUploadMetainfo, because the modifications may be unique to different runs of confluence.
	err = h.saveMetaInfo(mi, mi.HashInfoBytes())
	if err != nil {
		err = fmt.Errorf("saving metainfo: %w", err)
		log.Printf("error uploading: %v", err)
	}
	err = h.storeUploadPieces(&info, mi.HashInfoBytes(), files)
	if err != nil {
		err = fmt.Errorf("storing upload pieces: %w", err)
		log.Printf("error uploading: %v", err)
	}
	if f := h.ModifyUploadMetainfo; f != nil {
		f(&mi)
	}
	mi.Write(w)
}

func writeMultipartFiles(w io.Writer, fhs []*multipart.FileHeader) error {
	for _, fh := range fhs {
		file, err := fh.Open()
		if err != nil {
			err = fmt.Errorf("opening file %q: %w", fh.Filename, err)
			return err
		}
		_, err = io.Copy(w, file)
		file.Close()
		if err != nil {
			err = fmt.Errorf("copying file: %w", err)
			return err
		}
	}
	return nil
}

func (h *Handler) storeUploadPieces(info *metainfo.Info, ih metainfo.Hash, files []*multipart.FileHeader) (err error) {
	torrentStorage, err := h.Storage.OpenTorrent(info, ih)
	if err != nil {
		err = fmt.Errorf("opening storage for torrent: %w", err)
		return
	}
	defer torrentStorage.Close()
	r, w := io.Pipe()
	go func() {
		err := writeMultipartFiles(w, files)
		if err != nil {
			err = fmt.Errorf("writing upload multipart files: %w", err)
		}
		w.CloseWithError(err)
	}()
	defer r.Close()
	buf := make([]byte, info.PieceLength)
pieces:
	for pieceIndex := 0; ; pieceIndex++ {
		numRead, err := io.ReadFull(r, buf)
		switch err {
		default:
			return fmt.Errorf("reading piece %v: %w", pieceIndex, err)
		case io.EOF:
			break pieces
		case nil, io.ErrUnexpectedEOF:
		}
		pieceStorage := torrentStorage.Piece(info.Piece(pieceIndex))
		numWritten, err := pieceStorage.WriteAt(buf[:numRead], 0)
		if numWritten != numRead {
			return fmt.Errorf("writing piece %v: %w", pieceIndex, err)
		}
		err = pieceStorage.MarkComplete()
		if err != nil {
			return fmt.Errorf("marking piece %v complete: %w", pieceIndex, err)
		}
	}
	return nil
}
