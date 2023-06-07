package internal

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
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
	router.HandleFunc("/db", serveDBPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/stats", serveDBStatsPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/new-bucket", serveDBNewBucketPage(s)).Methods(http.MethodGet)
	router.HandleFunc("/db/new-bucket", handleDBNewBucketForm(s)).Methods(http.MethodPost)
	router.HandleFunc("/db/search", serveDBSearchPage(s)).Methods(http.MethodGet)
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

func serveDBPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db.gohtml")
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
				{Name: "DB buckets", Path: "/db"},
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
			http.Redirect(w, r, "/db", http.StatusSeeOther)
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

					{Name: "DB buckets", Path: "/db"},
					{Name: id, Path: "/db/search?list=" + url.QueryEscape(id)},
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

func serveDBSearchPage(s *Server) http.HandlerFunc {
	tmpl := mustParseTmpl(layoutTmpls, tmplDirPath, "db-search.gohtml")
	const numRowsPerPage = 10
	return func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
			return
		}
		selectedLists := r.Form["list"]
		query := r.FormValue("query")
		exclude := r.FormValue("exclude")
		if exclude == "" {
			exclude = "false"
		}
		excludeQueryMatches, err := strconv.ParseBool(exclude)
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
			return
		}
		page := r.FormValue("page")
		pageIndex := 1
		if page != "" && page != "1" {
			pageIndex, err = strconv.Atoi(page)
			if err != nil {
				s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
				return
			}
		}

		var regex *regexp.Regexp
		if query != "" {
			regex, err = regexp.Compile(query)
			if err != nil {
				s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
				return
			}
		}

		lists := map[string]bool{}
		err = s.db.ReadEachList(func(name string) error {
			// Set "checked" to true
			// if bucket is selected or if no bucket has been selected at all
			lists[name] = false
			if len(selectedLists) == 0 {
				lists[name] = true
			}
			for _, selectedName := range selectedLists {
				if selectedName == name {
					lists[name] = true
				}
			}
			return nil
		})
		if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}

		result, err := kvstore.Search(s.db, selectedLists, regex, excludeQueryMatches, pageIndex-1, numRowsPerPage)
		if errors.Is(err, kvstore.ErrNotFound) {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusBadRequest, err)
			return
		} else if err != nil {
			s.respondErrorPageHTMLTmpl(w, r, http.StatusInternalServerError, err)
			return
		}

		tmplData := map[string]any{
			"Result":        result,
			"Lists":         lists,
			"SelectedLists": selectedLists,
			"Query":         query,
			"Exclude":       excludeQueryMatches,
			"PageIndex":     pageIndex,
			"Pages":         make([]struct{}, 1+result.TotalResults/numRowsPerPage),
		}
		if len(selectedLists) == 1 {
			tmplData["Breadcrumbs"] = Breadcrumbs{
				{Name: "DB buckets", Path: "/db"},
				{Name: selectedLists[0]},
			}
		}
		s.respondPageOK(w, r, tmpl, tmplData)
	}
}
