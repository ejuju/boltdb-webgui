package internal

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/ejuju/boltdb-webgui/pkg/boltutil"
	"github.com/ejuju/boltdb-webgui/pkg/httputils"
	"github.com/ejuju/boltdb-webgui/pkg/kvstore"
	"github.com/ejuju/boltdb-webgui/pkg/logs"
	"github.com/gorilla/mux"
)

type Server struct {
	db     kvstore.DB
	logger logs.Logger
}

func NewServer(fpath string) *Server {
	// Init logger
	logger := logs.NewTextLogger(os.Stderr)

	// Open DB file
	db := boltutil.NewKeyValueDB(fpath)

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
	router.HandleFunc("/db/new-bucket", serveDBNewBucketPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/new-bucket", handleDBNewBucketForm(s)).Methods(http.MethodPost)
	router.HandleFunc("/db/bucket", serveBucketPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/bucket/new-row", serveDBBucketNewRowPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/bucket/new-row", handleDBBucketNewRowForm(s)).Methods(http.MethodPost)
	router.HandleFunc("/db/bucket/edit-row", serveDBBucketEditRowPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/bucket/edit-row", handleDBBucketEditRowForm(s)).Methods(http.MethodPost)
	router.HandleFunc("/db/bucket/delete", handleDBBucketDeleteForm(s)).Methods(http.MethodPost)
	router.HandleFunc("/db/bucket/delete-row", handleDBBucketDeleteRowForm(s)).Methods(http.MethodPost)
	router.NotFoundHandler = handleNotFound(s)

	// Register global middleware
	var routerWithMW http.Handler = router
	routerWithMW = httputils.AccessLoggingMiddleware(s.logger)(routerWithMW)
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
		s.respondPageOK(w, r, tmpl, map[string]any{})
	}
}

func serveDBBucketsPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db-buckets.gohtml")
	return func(w http.ResponseWriter, r *http.Request) {
		info, err := kvstore.GetDBInfo(s.db)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
		s.respondPageOK(w, r, tmpl, map[string]any{"Buckets": info.Lists})
	}
}

func serveDBNewBucketPage(s *Server) http.HandlerFunc {
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
		info, err := kvstore.GetDBInfo(s.db)
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
		bucketID, err := url.QueryUnescape(urlQuery.Get("id"))
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
			return
		}
		page := urlQuery.Get("page")
		if page == "" {
			page = "0"
		}
		pageNum, err := strconv.Atoi(page)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, err)
			return
		}

		info, err := kvstore.GetListInfo(s.db, bucketID)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}
		rows, err := s.db.ReadRowPage(bucketID, pageNum, numRowsPerPage)
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

		err = s.db.DeleteList(id)
		if errors.Is(err, kvstore.ErrNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, err)
		} else if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
		} else {
			http.Redirect(w, r, "/db/buckets", http.StatusSeeOther)
		}
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
		err = s.db.DeleteRow(id, r.FormValue("key"))

		if errors.Is(err, kvstore.ErrNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, err)
		} else if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
		} else {
			http.Redirect(w, r, "/db/bucket?id="+url.QueryEscape(id), http.StatusSeeOther)
		}
	}
}

func serveDBBucketNewRowPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db-bucket-new-row.gohtml")
	return func(w http.ResponseWriter, r *http.Request) {
		bucketName := r.URL.Query().Get("id")
		numRows, err := s.db.NumRows(bucketName)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}

		s.respondPageOK(w, r, tmpl, map[string]any{
			"BucketID": bucketName,
			"AutoKey":  numRows + 1,
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

		err = s.db.CreateRow(bucketID, &kvstore.Row{Key: kvstore.RowKey(key), Value: kvstore.RowValue(value)})
		if errors.Is(err, kvstore.ErrNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, err)
		} else if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
		} else {
			http.Redirect(w, r, "/db/bucket?id="+url.QueryEscape(bucketID), http.StatusSeeOther)
		}
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

		// Create bucket in DB and redirect to newly created bucket on success
		err = s.db.CreateList(name)
		if errors.Is(err, kvstore.ErrAlreadyExists) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
		} else if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
		} else {
			http.Redirect(w, r, "/db/bucket?id="+name, http.StatusSeeOther)
		}
	}
}

func serveDBBucketEditRowPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db-bucket-edit-row.gohtml")
	return func(w http.ResponseWriter, r *http.Request) {
		urlQueryParams := r.URL.Query()
		id := urlQueryParams.Get("id")
		key := urlQueryParams.Get("key")

		row, err := s.db.ReadRow(id, key)
		if errors.Is(err, kvstore.ErrNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, err)
		} else if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
		} else {
			s.respondPageOK(w, r, tmpl, map[string]any{
				"Breadcrumbs": Breadcrumbs{
					{Name: "DB"},
					{Name: "Buckets", Path: "/db/buckets"},
					{Name: id, Path: "/db/bucket?id=" + url.QueryEscape(id)},
					{Name: key},
				},
				"BucketID": id,
				"Row":      row,
			})
		}
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

		err = s.db.UpdateRow(id, key, value)
		if errors.Is(err, kvstore.ErrNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusNotFound, err)
		} else if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
		} else {
			http.Redirect(w, r, "/db/bucket?id="+url.QueryEscape(id), http.StatusSeeOther)
		}
	}
}
