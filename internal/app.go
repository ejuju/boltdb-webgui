package internal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"text/template"
	"time"

	"github.com/gorilla/mux"
	"go.etcd.io/bbolt"
)

type Server struct {
	db *bbolt.DB
}

func NewServer(fpath string) *Server {
	// Init logger
	log.SetFlags(log.LUTC | log.Llongfile)

	// Open DB file
	db, err := bbolt.Open(fpath, os.ModePerm, &bbolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		panic(fmt.Errorf("open DB file: %w", err))
	}

	return &Server{db: db}
}

func (s *Server) NewHTTPHandler() http.Handler {
	// Init static file server
	staticFileServer := http.StripPrefix("/public/", http.FileServer(http.Dir("public")))

	// Init HTTP routes
	router := mux.NewRouter()
	router.Handle("/public/*", staticFileServer)
	router.HandleFunc("/", serveHomePage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/buckets", serveDBBucketsPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/new-bucket", handleDBNewBucketPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/new-bucket", handleDBNewBucketForm(s)).Methods(http.MethodPost)
	router.HandleFunc("/db/bucket", serveBucketPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/bucket/new-row", handleDBBucketNewRowPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/bucket/new-row", handleDBBucketNewRowForm(s)).Methods(http.MethodPost)
	router.HandleFunc("/db/bucket/delete", handleDeleteRow(s)).Methods(http.MethodDelete)
	router.NotFoundHandler = handleNotFound(s)
	return router
}

func handleNotFound(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, fmt.Errorf("%q not found", r.URL))
	}
}

func serveHomePage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "home.gohtml")

	return func(w http.ResponseWriter, r *http.Request) {
		// Get stats
		fstats, err := os.Stat(s.db.Path())
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}

		// Get top-level buckets and number of rows for each bucket
		buckets := map[string]int{}
		err = s.db.View(func(tx *bbolt.Tx) error {
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
			"FileSize": float64(fstats.Size()) / 1_000_000.0, // in GB
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
		s.respondPageOK(w, r, tmpl, nil)
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

		// Count number of rows
		numRows, err := countBucketRows(s.db, bucketID)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
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

		// Get rows from DB and render template
		numRowsPerPage := 10
		rows, err := getBucketRowPage(s.db, bucketID, pageNum, numRowsPerPage)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
		s.respondPageOK(w, r, tmpl, map[string]any{
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
		// Get bucket rows
		c := tx.Bucket([]byte(bucketID)).Cursor()
		k, v := c.Last()
		if k == nil {
			return nil
		}
		for i := 0; i < (numRowsPerPage*page)-1; i++ {
			_, _ = c.Prev()
		}
		rows = append(rows, row{Key: string(k), Value: formatRowValue(string(v))})
		for i := 1; i < numRowsPerPage; i++ {
			k, v := c.Prev()
			if k == nil {
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

var errKeyNotFound = errors.New("key not found")

func handleDeleteRow(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		key := r.URL.Query().Get("key")
		err := s.db.Update(func(tx *bbolt.Tx) error {
			bucket := tx.Bucket([]byte(id))
			if bucket == nil {
				return fmt.Errorf("%w (id=%s)", errKeyNotFound, id)
			}
			if bucket.Get([]byte(key)) == nil {
				return fmt.Errorf("%w (key=%s)", errKeyNotFound, key)
			}
			return bucket.Delete([]byte(key))
		})
		if errors.Is(err, errKeyNotFound) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
				return fmt.Errorf("%w (%s)", errKeyNotFound, bucketID)
			}
			return bucket.Put([]byte(key), []byte(value))
		})
		if errors.Is(err, errKeyNotFound) {
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
		http.Redirect(w, r, "/bucket?id="+name, http.StatusSeeOther)
	}
}
