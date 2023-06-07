package internal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"text/template"
	"time"

	"github.com/ejuju/boltdb_webgui/pkg/httputils"
	"github.com/ejuju/boltdb_webgui/pkg/logs"
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
		// Get top-level buckets and number of rows for each bucket
		buckets := map[string]int{}
		err := s.db.View(func(tx *bbolt.Tx) error {
			return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
				buckets[string(name)] = b.Stats().KeyN
				return nil
			})
		})
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
		s.respondPageOK(w, r, tmpl, map[string]any{
			"Buckets":  buckets,
			"FilePath": s.db.Path(),
		})
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
	}
}

func serveDBBucketsPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db-buckets.gohtml")
	tmpl.Funcs(template.FuncMap{
		"URLQueryEscape": url.QueryEscape,
	})
	return func(w http.ResponseWriter, r *http.Request) {
		bucketsInfo, err := getAllBucketsInfo(s.db)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
		s.respondPageOK(w, r, tmpl, map[string]any{
			"Buckets": bucketsInfo,
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

	type BucketTmplData struct {
		Name         string
		NameSlug     string
		NumRows      int
		TotalRowSize int
		AvgRowSize   int
	}

	type TmplData struct {
		Breadcrumbs Breadcrumbs
		FileSize    float64
		NumBuckets  int
		Buckets     []BucketTmplData
	}

	return func(w http.ResponseWriter, r *http.Request) {
		tmplData := TmplData{}

		// Get stats
		fstats, err := os.Stat(s.db.Path())
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
		tmplData.FileSize = float64(fstats.Size()) / 1_000_000.0 // in GB
		// Get bucket stats for each bucket
		err = s.db.View(func(tx *bbolt.Tx) error {
			return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
				tmplData.NumBuckets++
				totalRowSize := 0
				err := b.ForEach(func(k, v []byte) error { totalRowSize += len(v) + len(k); return nil })
				if err != nil {
					return err
				}
				numRows := b.Stats().KeyN
				avgRowSize := 0
				if numRows != 0 {
					avgRowSize = totalRowSize / numRows
				}
				tmplData.Buckets = append(tmplData.Buckets, BucketTmplData{
					Name:         string(name),
					NameSlug:     url.QueryEscape(string(name)),
					NumRows:      numRows,
					TotalRowSize: totalRowSize,
					AvgRowSize:   avgRowSize,
				})
				return nil
			})
		})
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}

		s.respondPageOK(w, r, tmpl, tmplData)
	}
}

func serveBucketPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db-bucket.gohtml")
	return func(w http.ResponseWriter, r *http.Request) {
		urlQuery := r.URL.Query()

		// Get bucket ID and check if bucket exists
		bucketID := urlQuery.Get("id")
		ok, err := bucketExists(s.db, bucketID)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
		if !ok {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, fmt.Errorf("bucket %q not found", bucketID))
			return
		}

		// Get page number or set default (0)
		page := urlQuery.Get("page")
		if page == "" {
			page = "0"
		}
		pageNum, err := strconv.Atoi(page)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, err)
			return
		}

		// Count number of rows
		numRows, err := countBucketRows(s.db, bucketID)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}

		// Get rows from DB and render template
		numRowsPerPage := 10
		rows, err := getBucketRowPage(s.db, bucketID, pageNum, numRowsPerPage)
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
			"Pages": make([]struct{}, 1+numRows/numRowsPerPage),
		})
	}
}

type row struct {
	Key   string
	Value string
}

func getBucketRowPage(db *bbolt.DB, bucketID string, page, numRowsPerPage int) ([]row, error) {
	rows := []row{}
	return rows, db.View(func(tx *bbolt.Tx) error {
		offset := page * numRowsPerPage
		// Get bucket rows
		c := tx.Bucket([]byte(bucketID)).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			for i := 0; i < offset; i++ {
				k, v = c.Next()
			}
			if len(rows) == numRowsPerPage {
				break
			}
			rows = append(rows, row{Key: string(k), Value: formatRowValue(string(v))})
		}
		return nil
	})
}

func bucketExists(db *bbolt.DB, bucketID string) (bool, error) {
	exists := false
	return exists, db.View(func(tx *bbolt.Tx) error {
		exists = tx.Bucket([]byte(bucketID)) != nil
		return nil
	})
}

func countBucketRows(db *bbolt.DB, bucketID string) (int, error) {
	count := 0
	return count, db.View(func(tx *bbolt.Tx) error {
		count = tx.Bucket([]byte(bucketID)).Stats().KeyN
		return nil
	})
}

var errIdentifierNotFound = errors.New("identifier not found")

func newErrIdentifierNotFound(id string) error {
	return fmt.Errorf("%w: %q", errIdentifierNotFound, id)
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
			bucket := tx.Bucket([]byte(id))
			if bucket == nil {
				return newErrIdentifierNotFound(id)
			}
			return tx.DeleteBucket([]byte(id))
		})
		if errors.Is(err, errIdentifierNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
			return
		}
		if err != nil {
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
			bucket := tx.Bucket([]byte(id))
			if bucket == nil {
				return newErrIdentifierNotFound(id)
			}
			if bucket.Get([]byte(key)) == nil {
				return newErrIdentifierNotFound(key)
			}
			return bucket.Delete([]byte(key))
		})
		if errors.Is(err, errIdentifierNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
			return
		}
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}

		http.Redirect(w, r, "/db/bucket?id="+url.QueryEscape(id), http.StatusSeeOther)
	}
}

func formatRowValue(s string) string {
	buf := &bytes.Buffer{}
	err := json.Indent(buf, []byte(s), "", "\t")
	if err != nil {
		return s
	}
	return buf.String()
}

func handleDBBucketNewRowPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db-bucket-new-row.gohtml")
	return func(w http.ResponseWriter, r *http.Request) {
		s.respondPageOK(w, r, tmpl, map[string]any{
			"BucketID": r.URL.Query().Get("id"),
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
			bucket := tx.Bucket([]byte(bucketID))
			if bucket == nil {
				return newErrIdentifierNotFound(bucketID)
			}
			return bucket.Put([]byte(key), []byte(value))
		})
		if errors.Is(err, errIdentifierNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, err)
			return
		}
		if err != nil {
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
			b := tx.Bucket([]byte(id))
			if b == nil {
				return newErrIdentifierNotFound(id)
			}
			value = b.Get([]byte(key))
			if value == nil {
				return newErrIdentifierNotFound(key)
			}
			return nil
		})
		if errors.Is(err, errIdentifierNotFound) {
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
			return tx.Bucket([]byte(id)).Put([]byte(key), []byte(value))
		})
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
		http.Redirect(w, r, "/db/bucket?id="+url.QueryEscape(id), http.StatusSeeOther)
	}
}
