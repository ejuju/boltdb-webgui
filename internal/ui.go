package internal

import (
	"html/template"
	"net/http"
	"net/url"
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
	t := template.New(fname).Funcs(template.FuncMap{
		"QueryEscape": url.QueryEscape,
	})
	return template.Must(t.ParseFiles(append(commonTmpls, filepath.Join(tmplDirPath, fname))...))
}

// Will panic when an error occurs during rendering, make sure you handle panic recovery in a middleware.
func (s *Server) respondHTMLTmpl(
	w http.ResponseWriter,
	r *http.Request,
	statusCode int,
	t *template.Template,
	tname string,
	data any,
) {
	w.WriteHeader(statusCode)
	if data == nil {
		// Required to allow optional fields
		data = map[string]any{}
	}
	err := t.ExecuteTemplate(w, tname, map[string]any{
		"DBPath":  s.db.DiskPath(),
		"Request": r,
		"Local":   data,
	})
	if err != nil {
		w.Write([]byte(err.Error()))
		s.logger.Log(err.Error())
		return
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

func (s *Server) respondPageOK(w http.ResponseWriter, r *http.Request, t *template.Template, data any) {
	s.respondHTMLTmpl(w, r, http.StatusOK, t, tmplLayoutKey, data)
}

type Breadcrumbs []Breadcrumb

type Breadcrumb struct {
	Name string
	Path string
}
