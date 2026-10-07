package main

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

//go:embed templates/*.html
var templateFS embed.FS

const (
	maxKeyLen     = 200
	maxCommentLen = 4000
	maxScale      = 10
)

type server struct {
	store Store
	log   *slog.Logger
	tmpl  *template.Template
	mux   *http.ServeMux
}

func newServer(store Store, log *slog.Logger) http.Handler {
	s := &server{
		store: store,
		log:   log,
		tmpl:  template.Must(template.New("").Funcs(template.FuncMap{"seq": seq}).ParseFS(templateFS, "templates/*.html")),
		mux:   http.NewServeMux(),
	}
	s.mux.HandleFunc("GET /{$}", s.home)
	s.mux.HandleFunc("GET /links", s.links)
	s.mux.HandleFunc("GET /vote", s.vote)
	s.mux.HandleFunc("POST /comment", s.comment)
	s.mux.HandleFunc("GET /results", s.results)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.mux.ServeHTTP(w, r)
}

// home explains the app and how to use it.
func (s *server) home(w http.ResponseWriter, r *http.Request) {
	s.render(w, "home.html", struct{ Base string }{baseURL(r)})
}

// links shows copy-paste snippets for a key. Query params key, kind and
// scale let the page be bookmarked.
func (s *server) links(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	key := strings.TrimSpace(q.Get("key"))
	kind := q.Get("kind")
	if kind != "scale" {
		kind = "yesno"
	}
	scale, _ := strconv.Atoi(q.Get("scale"))
	if scale < 2 || scale > maxScale {
		scale = 5
	}

	data := struct {
		Key     string
		Kind    string
		Scale   int
		KeyErr  string
		Links   []link
		Slack   string
		Email   string
		Plain   string
		Results string
	}{Key: key, Kind: kind, Scale: scale}

	if key != "" {
		if err := validKey(key); err != nil {
			data.KeyErr = err.Error()
		} else {
			base := baseURL(r)
			if kind == "scale" {
				for i := 1; i <= scale; i++ {
					data.Links = append(data.Links, link{strconv.Itoa(i), voteURL(base, key, strconv.Itoa(i))})
				}
			} else {
				data.Links = []link{{"Yes", voteURL(base, key, "yes")}, {"No", voteURL(base, key, "no")}}
			}
			data.Slack, data.Email, data.Plain = snippets(kind, data.Links)
			data.Results = base + "/results?key=" + escape(key)
		}
	}
	s.render(w, "links.html", data)
}

type link struct {
	Label string
	URL   string
}

func snippets(kind string, links []link) (slack, email, plain string) {
	var sl, em, pl []string
	for _, l := range links {
		sl = append(sl, fmt.Sprintf("<%s|%s>", l.URL, l.Label))
		em = append(em, fmt.Sprintf(`<a href="%s">%s</a>`, l.URL, l.Label))
		pl = append(pl, l.Label+": "+l.URL)
	}
	prompt := "Was this useful?"
	if kind == "scale" {
		prompt = fmt.Sprintf("Rate this (1 = not useful, %d = very useful):", len(links))
	}
	slack = prompt + " " + strings.Join(sl, " · ")
	email = "<p>" + prompt + " " + strings.Join(em, " &middot; ") + "</p>"
	plain = prompt + "\n" + strings.Join(pl, "\n")
	return
}

// baseURL is the public prefix for generated links, taken from the request.
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func voteURL(base, key, value string) string {
	return base + "/vote?key=" + escape(key) + "&value=" + escape(value)
}

// escape query-encodes s using %20 rather than + for spaces, which
// survives email clients and chat tools more reliably.
func escape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// seq returns the integers from a to b inclusive, for template loops.
func seq(a, b int) []int {
	out := make([]int, 0, b-a+1)
	for i := a; i <= b; i++ {
		out = append(out, i)
	}
	return out
}

// vote records the click, then shows an optional comment box. The vote
// counts whether or not the reader types anything.
func (s *server) vote(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	key := strings.TrimSpace(q.Get("key"))
	value := strings.ToLower(strings.TrimSpace(q.Get("value")))
	if err := validKey(key); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validValue(value); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := s.store.Add(r.Context(), key, value)
	if err != nil {
		s.log.Error("add vote", "err", err)
		http.Error(w, "could not save vote", http.StatusInternalServerError)
		return
	}
	s.log.Info("vote", "key", key, "value", value, "id", id)
	s.render(w, "vote.html", struct {
		ID    string
		Key   string
		Value string
	}{id, key, value})
}

func (s *server) comment(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id := r.PostForm.Get("id")
	key := r.PostForm.Get("key")
	comment := strings.TrimSpace(r.PostForm.Get("comment"))
	if utf8.RuneCountInString(comment) > maxCommentLen {
		http.Error(w, fmt.Sprintf("comment longer than %d characters", maxCommentLen), http.StatusBadRequest)
		return
	}
	if comment != "" {
		if err := s.store.SetComment(r.Context(), id, key, comment); err != nil {
			if errors.Is(err, ErrNotFound) {
				http.Error(w, "unknown vote", http.StatusNotFound)
				return
			}
			s.log.Error("set comment", "err", err)
			http.Error(w, "could not save comment", http.StatusInternalServerError)
			return
		}
		s.log.Info("comment", "id", id)
	}
	s.render(w, "done.html", nil)
}

func (s *server) results(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if key == "" {
		keys, err := s.store.Keys(r.Context())
		if err != nil {
			s.log.Error("list keys", "err", err)
			http.Error(w, "could not load results", http.StatusInternalServerError)
			return
		}
		s.render(w, "keys.html", keys)
		return
	}
	votes, err := s.store.ByKey(r.Context(), key)
	if err != nil {
		s.log.Error("load votes", "key", key, "err", err)
		http.Error(w, "could not load results", http.StatusInternalServerError)
		return
	}
	tally := map[string]int{}
	sum, n := 0, 0
	var comments []Vote
	for _, v := range votes {
		tally[v.Value]++
		if i, err := strconv.Atoi(v.Value); err == nil {
			sum += i
			n++
		}
		if v.Comment != "" {
			comments = append(comments, v)
		}
	}
	var avg string
	if n > 0 {
		avg = fmt.Sprintf("%.1f", float64(sum)/float64(n))
	}
	s.render(w, "results.html", struct {
		Key      string
		Total    int
		Tally    map[string]int
		Average  string
		Comments []Vote
	}{key, len(votes), tally, avg, comments})
}

func (s *server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		s.log.Error("render", "template", name, "err", err)
	}
}

func validKey(key string) error {
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if utf8.RuneCountInString(key) > maxKeyLen {
		return fmt.Errorf("key longer than %d characters", maxKeyLen)
	}
	return nil
}

// validValue accepts yes, no, or an integer from 1 to maxScale.
func validValue(v string) error {
	if v == "yes" || v == "no" {
		return nil
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= maxScale {
		return nil
	}
	return fmt.Errorf("value must be yes, no, or a number from 1 to %d", maxScale)
}
