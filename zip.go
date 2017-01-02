package main

// func zipWriteFile(z *zip.Writer, f torrent.File, r io.Reader) (err error) {
// 	w, err := z.CreateHeader(&zip.FileHeader{
// 		Name:   f.Path(),
// 		Method: zip.Store,
// 	})
// 	if err != nil {
// 		return
// 	}
// 	n, err := io.Copy(w, r)
// 	if err != nil {
// 		return
// 	}
// 	if n != f.Length() {
// 		panic("short write")
// 	}
// 	return
// }

// func zipTorrent(
// 	zw io.Writer,
// 	t *torrent.Torrent,
// 	getFileReader func(torrent.File) io.Reader,
// ) (
// 	err error,
// ) {
// 	z := zip.NewWriter(zw)
// 	for _, f := range t.Files() {
// 		err = zipWriteFile(z, f, getFileReader(f))
// 		if err != nil {
// 			return
// 		}
// 	}
// 	err = z.Close()
// 	return
// }

// func torrentZipLength(t *torrent.Torrent) int64 {
// 	sw := missinggo.NewStatWriter(ioutil.Discard)
// 	err := zipTorrent(sw, t, func(f torrent.File) io.Reader {
// 		return io.LimitReader(missinggo.ZeroReader, f.Length())
// 	})
// 	if err != nil {
// 		panic(err)
// 	}
// 	return sw.Written
// }

// func zipHandler(rw http.ResponseWriter, r *http.Request) {
// 	t := requestTorrent(r)
// 	rw.Header().Set("Content-Type", "application/zip")
// 	// Nice to wait until we have a proper name for the torrent. I don't think
// 	// there's anything to actually send in the body until the files become
// 	// available anyway.
// 	<-t.GotInfo()
// 	length := torrentZipLength(t.Torrent)
// 	rw.Header().Set("Content-Disposition", contentDispositionValue(t.Info().Name+".zip", false))
// 	rw.Header().Set("Accept-Ranges", "bytes")
// 	pr, pw := io.Pipe()
// 	defer pr.Close()
// 	go func() {
// 		tr := t.NewReader()
// 		defer tr.Close()
// 		err := zipTorrent(pw, t.Torrent, func(f torrent.File) io.Reader {
// 			return missinggo.NewSectionReadSeeker(tr, f.Offset(), f.Length())
// 		})
// 		pw.CloseWithError(err)
// 	}()
// 	cr, partial := httptoo.ParseBytesRange(r.Header.Get("Range"))
// 	if partial {
// 		if cr.First >= length {
// 			rw.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", length))
// 			http.Error(rw, "beyond end of file", http.StatusRequestedRangeNotSatisfiable)
// 			return
// 		}
// 		if cr.Last >= length {
// 			cr.Last = length - 1
// 		}
// 		rw.Header().Set("Content-Length", fmt.Sprintf("%d", cr.Last-cr.First+1))
// 		rw.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", cr.First, cr.Last, length))
// 		rw.WriteHeader(http.StatusPartialContent)
// 		if r.Method == "HEAD" {
// 			return
// 		}
// 		io.CopyN(ioutil.Discard, pr, cr.First)
// 		io.CopyN(rw, pr, cr.Last-cr.First+1)
// 	} else {
// 		rw.Header().Set("Content-Length", strconv.FormatInt(length, 10))
// 		if r.Method == "HEAD" {
// 			return
// 		}
// 		io.Copy(rw, pr)
// 	}
// }
