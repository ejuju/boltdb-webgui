package internal

import (
	"html/template"
	"net/http"
	"path/filepath"
)

const tmplDirPath = "gohtml"
const tmplLayoutKey = "layout"

// Layout template files used for each page.
var layoutTmpls = []string{
	filepath.Join(tmplDirPath, "_layout.gohtml"),
	filepath.Join(tmplDirPath, "_css.gohtml"),
	filepath.Join(tmplDirPath, "_header.gohtml"),
	filepath.Join(tmplDirPath, "_footer.gohtml"),
}

func mustParseTmpl(commonTmpls []string, tmplDirPath, fname string) *template.Template {
	return template.Must(template.ParseFiles(append(commonTmpls, filepath.Join(tmplDirPath, fname))...))
}

// Will panic when an error occurs during rendering, make sure you handle panic recovery in a middleware.
func (s *Server) respondHTMLTmpl(
	w http.ResponseWriter,
	r *http.Request,
	statusCode int,
	t *template.Template,
	tname string,
	data map[string]any,
) {
	w.WriteHeader(statusCode)
	err := t.ExecuteTemplate(w, tname, map[string]any{
		"DBPath":  s.db.Path(),
		"Request": r,
		"Local":   data,
	})
	if err != nil {
		panic(err)
	}
}

var errorPageHTMLTmpl = mustParseTmpl(layoutTmpls, tmplDirPath, "_error.gohtml")

func (s *Server) respondErrorPageHTMLTmpl(w http.ResponseWriter, r *http.Request, statusCode int, err error) {
	statusText := http.StatusText(statusCode)
	s.respondHTMLTmpl(w, r, statusCode, errorPageHTMLTmpl, tmplLayoutKey, map[string]any{
		"Error":      err,
		"StatusCode": statusCode,
		"StatusText": statusText,
	})
}

func (s *Server) respondPageOK(w http.ResponseWriter, r *http.Request, t *template.Template, data map[string]any) {
	s.respondHTMLTmpl(w, r, http.StatusOK, t, tmplLayoutKey, data)
}
