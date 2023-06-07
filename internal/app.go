package internal

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/ejuju/boltdb-webgui/pkg/boltutil"
	"github.com/ejuju/boltdb-webgui/pkg/httputils"
	"github.com/ejuju/boltdb-webgui/pkg/logs"
	"github.com/gorilla/mux"
	"go.etcd.io/bbolt"
)

type Server struct {
	db     *bbolt.DB
	logger logs.Logger
}

func NewServer(fpath string) *Server {
	// Init logger
	logger := logs.NewTextLogger(os.Stderr)

	// Open DB file
	db, err := bbolt.Open(fpath, os.ModePerm, &bbolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		panic(fmt.Errorf("open DB file: %w", err))
	}

	return &Server{
		db:     db,
		logger: logger,
	}
}

func (s *Server) NewHTTPHandler() http.Handler {
	// Init static file server
	staticFileServer := http.StripPrefix("/public/", http.FileServer(http.Dir("public")))

	// Init HTTP routes
	router := mux.NewRouter()
	router.PathPrefix("/public/").Handler(staticFileServer)
	router.HandleFunc("/", serveHomePage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/buckets", serveDBBucketsPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/stats", serveDBStatsPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/new-bucket", handleDBNewBucketPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/new-bucket", handleDBNewBucketForm(s)).Methods(http.MethodPost)
	router.HandleFunc("/db/bucket", serveBucketPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/bucket/new-row", handleDBBucketNewRowPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/bucket/new-row", handleDBBucketNewRowForm(s)).Methods(http.MethodPost)
	router.HandleFunc("/db/bucket/edit-row", handleDBBucketEditRowPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/bucket/edit-row", handleDBBucketEditRowForm(s)).Methods(http.MethodPost)
	router.HandleFunc("/db/bucket/delete", handleDBBucketDeleteForm(s)).Methods(http.MethodPost)
	router.HandleFunc("/db/bucket/delete-row", handleDBBucketDeleteRowForm(s)).Methods(http.MethodPost)
	router.NotFoundHandler = handleNotFound(s)

	// Register global middleware
	var routerWithMW http.Handler = router
	// First, log incoming HTTP request
	routerWithMW = httputils.AccessLoggingMiddleware(s.logger)(routerWithMW)
	// Finally, recover from any eventual panic
	routerWithMW = httputils.PanicRecoveryMiddleware(s.logger, onPanicFunc(s))(routerWithMW)

	return routerWithMW
}

func onPanicFunc(s *Server) httputils.PanicHandler {
	return func(w http.ResponseWriter, r *http.Request, err any) {
		s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, fmt.Errorf("%v", err))
	}
}

func handleNotFound(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, fmt.Errorf("%q not found", r.URL))
	}
}

func serveHomePage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "home.gohtml")
	return func(w http.ResponseWriter, r *http.Request) {
		s.respondPageOK(w, r, tmpl, map[string]any{
			"FilePath": s.db.Path(),
		})
	}
}

func serveDBBucketsPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db-buckets.gohtml")
	return func(w http.ResponseWriter, r *http.Request) {
		info, err := boltutil.OpenTxAndGetDBInfo(s.db)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
		s.respondPageOK(w, r, tmpl, map[string]any{
			"Buckets": info.Buckets,
		})
	}
}

func handleDBNewBucketPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db-new-bucket.gohtml")
	return func(w http.ResponseWriter, r *http.Request) {
		s.respondPageOK(w, r, tmpl, map[string]any{
			"Breadcrumbs": Breadcrumbs{
				{Name: "DB"},
				{Name: "Buckets", Path: "/db/buckets"},
				{Name: "Add new bucket"},
			},
		})
	}
}

func serveDBStatsPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db-stats.gohtml")
	return func(w http.ResponseWriter, r *http.Request) {
		info, err := boltutil.OpenTxAndGetDBInfo(s.db)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
		s.respondPageOK(w, r, tmpl, map[string]any{"Info": info})
	}
}

func serveBucketPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db-bucket.gohtml")
	const numRowsPerPage = 10
	return func(w http.ResponseWriter, r *http.Request) {
		urlQuery := r.URL.Query()
		bucketID := urlQuery.Get("id")
		page := urlQuery.Get("page")
		if page == "" {
			page = "0"
		}
		pageNum, err := strconv.Atoi(page)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, err)
			return
		}

		var info *boltutil.BucketInfo
		var rows []*boltutil.Row
		err = s.db.View(func(tx *bbolt.Tx) error {
			b, err := boltutil.FindBucket(tx, []byte(bucketID))
			if err != nil {
				return err
			}
			info, err = boltutil.GetBucketInfo(b)
			if err != nil {
				return err
			}
			rows, err = boltutil.ReadBucketRowPage(b, pageNum, numRowsPerPage)
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}

		s.respondPageOK(w, r, tmpl, map[string]any{
			"Breadcrumbs": Breadcrumbs{
				{Name: "DB"},
				{Name: "Buckets", Path: "/db/buckets"},
				{Name: bucketID},
			},
			"ID":    bucketID,
			"Rows":  rows,
			"Page":  pageNum,
			"Pages": make([]struct{}, 1+info.NumRows/numRowsPerPage),
		})
	}
}

func handleDBBucketDeleteForm(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
			return
		}
		id := r.FormValue("id")

		err = s.db.Update(func(tx *bbolt.Tx) error {
			_, err := boltutil.FindBucket(tx, []byte(id))
			if err != nil {
				return err
			}
			return tx.DeleteBucket([]byte(id))
		})
		if errors.Is(err, boltutil.ErrNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, err)
			return
		} else if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}

		http.Redirect(w, r, "/db/buckets", http.StatusSeeOther)
	}
}

func handleDBBucketDeleteRowForm(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
			return
		}
		id := r.FormValue("id")
		key := r.FormValue("key")

		err = s.db.Update(func(tx *bbolt.Tx) error {
			b, err := boltutil.FindBucket(tx, []byte(id))
			if err != nil {
				return err
			}
			return boltutil.DeleteRow(b, []byte(key))
		})
		if errors.Is(err, boltutil.ErrNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, err)
			return
		} else if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}

		http.Redirect(w, r, "/db/bucket?id="+url.QueryEscape(id), http.StatusSeeOther)
	}
}

func handleDBBucketNewRowPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db-bucket-new-row.gohtml")
	return func(w http.ResponseWriter, r *http.Request) {
		bucketName := r.URL.Query().Get("id")
		autoKey := uint64(0)
		err := s.db.Update(func(tx *bbolt.Tx) error {
			b, err := boltutil.FindBucket(tx, []byte(bucketName))
			if err != nil {
				return err
			}
			autoKey, err = b.NextSequence()
			return err
		})
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
		s.respondPageOK(w, r, tmpl, map[string]any{
			"BucketID": bucketName,
			"AutoKey":  autoKey,
		})
	}
}

func handleDBBucketNewRowForm(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get bucket id, key and value from request form
		err := r.ParseForm()
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
		key := r.FormValue("key")
		value := r.FormValue("value")
		bucketID := r.FormValue("id")

		// Write value to bucket key
		err = s.db.Update(func(tx *bbolt.Tx) error {
			b, err := boltutil.FindBucket(tx, []byte(bucketID))
			if err != nil {
				return err
			}
			return boltutil.CreateRow(b, []byte(key), []byte(value))
		})
		if errors.Is(err, boltutil.ErrNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, err)
			return
		} else if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}

		http.Redirect(w, r, "/db/bucket?id="+url.QueryEscape(bucketID), http.StatusSeeOther)
	}
}

func handleDBNewBucketForm(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse form and get bucket name
		err := r.ParseForm()
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
		name := r.FormValue("name")

		// Create bucket in DB
		err = s.db.Update(func(tx *bbolt.Tx) error { _, err := tx.CreateBucket([]byte(name)); return err })
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
			return
		}

		// Redirect to newly created bucket
		http.Redirect(w, r, "/db/bucket?id="+name, http.StatusSeeOther)
	}
}

func handleDBBucketEditRowPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db-bucket-edit-row.gohtml")
	return func(w http.ResponseWriter, r *http.Request) {
		urlQueryParams := r.URL.Query()
		id := urlQueryParams.Get("id")
		key := urlQueryParams.Get("key")
		var value []byte

		err := s.db.View(func(tx *bbolt.Tx) error {
			b, err := boltutil.FindBucket(tx, []byte(id))
			if err != nil {
				return err
			}
			value, err = boltutil.ReadRow(b, []byte(key))
			return err
		})
		if errors.Is(err, boltutil.ErrNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
			return
		}
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}

		s.respondPageOK(w, r, tmpl, map[string]any{
			"Breadcrumbs": Breadcrumbs{
				{Name: "DB"},
				{Name: "Buckets", Path: "/db/buckets"},
				{Name: id, Path: "/db/bucket?id=" + id},
				{Name: key},
			},
			"BucketID": id,
			"Key":      key,
			"Value":    string(value),
		})
	}
}

func handleDBBucketEditRowForm(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
			return
		}
		id := r.FormValue("id")
		key := r.FormValue("key")
		value := r.FormValue("value")

		err = s.db.Update(func(tx *bbolt.Tx) error {
			b, err := boltutil.FindBucket(tx, []byte(id))
			if err != nil {
				return err
			}
			return boltutil.UpdateRow(b, []byte(key), []byte(value))
		})
		if errors.Is(err, boltutil.ErrNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, err)
			return
		} else if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}

		http.Redirect(w, r, "/db/bucket?id="+url.QueryEscape(id), http.StatusSeeOther)
	}
}
