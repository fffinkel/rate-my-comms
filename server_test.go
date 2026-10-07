package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) (*Store, http.Handler) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "votes.jsonl")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, newServer(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestVoteCountsOnClick(t *testing.T) {
	store, h := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/vote?key=Weekly+update&value=YES", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	votes := store.ByKey("Weekly update")
	if len(votes) != 1 || votes[0].Value != "yes" {
		t.Fatalf("votes = %+v", votes)
	}
	if !strings.Contains(rec.Body.String(), `name="id" value="`+votes[0].ID+`"`) {
		t.Fatal("vote page missing hidden id")
	}
}

func TestVoteRejectsBadValue(t *testing.T) {
	_, h := newTestServer(t)
	for _, v := range []string{"", "maybe", "0", "11", "3.5"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/vote?key=k&value="+url.QueryEscape(v), nil))
		if rec.Code != 400 {
			t.Errorf("value %q: status %d, want 400", v, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/vote?value=yes", nil))
	if rec.Code != 400 {
		t.Errorf("missing key: status %d, want 400", rec.Code)
	}
}

func TestCommentAttachesToVote(t *testing.T) {
	store, h := newTestServer(t)
	id, err := store.Add("k", "4")
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"id": {id}, "comment": {"  more charts please  "}}
	req := httptest.NewRequest("POST", "/comment", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := store.ByKey("k")[0].Comment; got != "more charts please" {
		t.Fatalf("comment = %q", got)
	}

	form.Set("id", "nope")
	req = httptest.NewRequest("POST", "/comment", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("unknown id: status %d, want 404", rec.Code)
	}
}

func TestIndexShowsSnippets(t *testing.T) {
	_, h := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/?key=Q3+plan&kind=scale&scale=3", nil)
	req.Host = "rate.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{
		"https://rate.example/vote?key=Q3%20plan&amp;value=1",
		"https://rate.example/vote?key=Q3%20plan&amp;value=3",
		"https://rate.example/results?key=Q3%20plan",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
	if strings.Contains(body, "value=4") {
		t.Error("scale 3 should not produce value=4")
	}
}

func TestStoreReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "votes.jsonl")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := s.Add("k", "no")
	s.Add("k", "yes")
	if err := s.SetComment(id, "hi"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	votes := s2.ByKey("k")
	if len(votes) != 2 {
		t.Fatalf("got %d votes after replay, want 2", len(votes))
	}
	if votes[0].Comment != "hi" || votes[1].Comment != "" {
		t.Fatalf("comments after replay: %+v", votes)
	}
	if keys := s2.Keys(); len(keys) != 1 || keys[0].Count != 2 {
		t.Fatalf("keys = %+v", keys)
	}
}
