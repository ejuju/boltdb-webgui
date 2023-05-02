package app

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/bmizerany/pat"
	"go.etcd.io/bbolt"
)

func NewHTTPHandler(fpath string) http.Handler {
	// Init logger
	log.SetFlags(log.LUTC | log.Llongfile)

	// Open DB file
	db, err := bbolt.Open(fpath, os.ModePerm, &bbolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		panic(fmt.Errorf("open DB file: %w", err))
	}

	// Init HTTP routes
	router := pat.New()
	router.Add(http.MethodGet, "/", serveHomePage(db))
	router.Add(http.MethodGet, "/bucket", serveBucketPage(db))
	router.Add(http.MethodDelete, "/bucket", handleDeleteRow(db))
	router.Add(http.MethodPost, "/bucket/row", handleAddRowForm(db))
	router.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondErrorPage(w, http.StatusNotFound, "Not found")
	})
	return router
}

//go:embed all:ui
var uiFS embed.FS

// Layout template files used for each page.
var layoutTmpls = []string{
	"ui/_layout.gohtml",
	"ui/_css.gohtml",
	"ui/_header.gohtml",
	"ui/_footer.gohtml",
}

var errPageTmpl = template.Must(template.ParseFS(uiFS, append(layoutTmpls, "ui/_error.gohtml")...))

func respondErrorPage(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	err := errPageTmpl.ExecuteTemplate(w, "layout", map[string]any{
		"Status":       strconv.Itoa(status),
		"StatusText":   http.StatusText(status),
		"ErrorMessage": message,
	})
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func serveHomePage(db *bbolt.DB) http.HandlerFunc {
	tmpl := template.Must(template.ParseFS(uiFS, append(layoutTmpls, "ui/index.gohtml")...))
	return func(w http.ResponseWriter, r *http.Request) {
		buckets, err := getBucketNamesAndNumRows(db)
		if err != nil {
			respondErrorPage(w, http.StatusInternalServerError, err.Error())
			return
		}
		err = tmpl.ExecuteTemplate(w, "layout", map[string]any{
			"Buckets":  buckets,
			"FilePath": db.Path(),
		})
		if err != nil {
			respondErrorPage(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
}

func getBucketNamesAndNumRows(db *bbolt.DB) (map[string]int, error) {
	out := map[string]int{}
	return out, db.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
			out[string(name)] = b.Stats().KeyN
			return nil
		})
	})
}

func serveBucketPage(db *bbolt.DB) http.HandlerFunc {
	tmpl := template.Must(template.ParseFS(uiFS, append(layoutTmpls, "ui/bucket.gohtml")...))
	return func(w http.ResponseWriter, r *http.Request) {
		// Get bucket ID and check if bucket exists
		bucketID := r.URL.Query().Get("id")
		ok, err := bucketExists(db, bucketID)
		if err != nil {
			respondErrorPage(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			respondErrorPage(w, http.StatusNotFound, fmt.Sprintf("bucket %q not found", bucketID))
			return
		}

		// Count number of rows
		numRows, err := countBucketRows(db, bucketID)
		if err != nil {
			respondErrorPage(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Get page number or set default (0)
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "0"
		}
		pageNum, err := strconv.Atoi(page)
		if err != nil {
			respondErrorPage(w, http.StatusBadRequest, err.Error())
			return
		}

		// Get rows from DB and render template
		numRowsPerPage := 10
		rows, err := getBucketRowPage(db, bucketID, pageNum, numRowsPerPage)
		if err != nil {
			respondErrorPage(w, http.StatusInternalServerError, err.Error())
			return
		}
		err = tmpl.ExecuteTemplate(w, "layout", map[string]any{
			"ID":    bucketID,
			"Rows":  rows,
			"Page":  pageNum,
			"Pages": make([]struct{}, 1+numRows/numRowsPerPage),
		})
		if err != nil {
			log.Println(err)
			respondErrorPage(w, http.StatusInternalServerError, err.Error())
			return
		}
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
		k, v := c.First()
		if k == nil {
			return nil
		}
		for i := 0; i < (numRowsPerPage*page)-1; i++ {
			_, _ = c.Next()
		}
		rows = append(rows, row{Key: string(k), Value: formatRowValue(string(v))})
		for i := 1; i < numRowsPerPage; i++ {
			k, v := c.Next()
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

func handleDeleteRow(db *bbolt.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		key := r.URL.Query().Get("key")
		err := db.Update(func(tx *bbolt.Tx) error {
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
		w.WriteHeader(http.StatusOK)
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

func handleAddRowForm(db *bbolt.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			respondErrorPage(w, http.StatusInternalServerError, err.Error())
			return
		}
		key := r.FormValue("key")
		value := r.FormValue("value")
		bucketID := r.FormValue("bucket_id")
		err = db.Update(func(tx *bbolt.Tx) error {
			bucket := tx.Bucket([]byte(bucketID))
			if bucket == nil {
				return fmt.Errorf("%w (%s)", errKeyNotFound, bucketID)
			}
			return bucket.Put([]byte(key), []byte(value))
		})
		if errors.Is(err, errKeyNotFound) {
			respondErrorPage(w, http.StatusNotFound, err.Error())
			return
		}
		if err != nil {
			respondErrorPage(w, http.StatusInternalServerError, err.Error())
			return
		}
		http.Redirect(w, r, "/bucket?id="+bucketID, http.StatusSeeOther)
	}
}
