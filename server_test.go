package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) (Store, http.Handler) {
	t.Helper()
	store, err := OpenFileStore(filepath.Join(t.TempDir(), "votes.jsonl"))
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
	votes, _ := store.ByKey(context.Background(), "Weekly update")
	if len(votes) != 1 || votes[0].Value != "yes" {
		t.Fatalf("votes = %+v", votes)
	}
	if !strings.Contains(rec.Body.String(), `name="id" value="`+votes[0].ID+`"`) {
		t.Fatal("vote page missing hidden id")
	}
}

func TestVoteRejectsBadValue(t *testing.T) {
	_, h := newTestServer(t)
	for _, v := range []string{"", "maybe", "0", "6", "3.5"} {
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

func postComment(h http.Handler, id, key, comment string) *httptest.ResponseRecorder {
	form := url.Values{"id": {id}, "key": {key}, "comment": {comment}}
	req := httptest.NewRequest("POST", "/comment", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCommentAttachesToVote(t *testing.T) {
	store, h := newTestServer(t)
	ctx := context.Background()
	id, err := store.Add(ctx, "k", "4")
	if err != nil {
		t.Fatal(err)
	}
	if rec := postComment(h, id, "k", "  more charts please  "); rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	votes, _ := store.ByKey(ctx, "k")
	if got := votes[0].Comment; got != "more charts please" {
		t.Fatalf("comment = %q", got)
	}
	if rec := postComment(h, "nope", "k", "x"); rec.Code != 404 {
		t.Fatalf("unknown id: status %d, want 404", rec.Code)
	}
}

func TestLinksShowsSnippets(t *testing.T) {
	_, h := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/links?key=Q3+plan&kind=scale", nil)
	req.Host = "rate.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{
		"https://rate.example/vote?key=Q3%20plan&amp;value=1",
		"https://rate.example/vote?key=Q3%20plan&amp;value=5",
		"https://rate.example/results?key=Q3%20plan",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("links page missing %q", want)
		}
	}
	if strings.Contains(body, "value=6") {
		t.Error("scale should stop at 5")
	}
}

func TestHomeLinksToPages(t *testing.T) {
	_, h := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{`href="/links"`, `href="/results"`, "/vote?key="} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("home missing %q", want)
		}
	}
}

func TestFileStoreReplay(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "votes.jsonl")
	s, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := s.Add(ctx, "k", "no")
	s.Add(ctx, "k", "yes")
	if err := s.SetComment(ctx, id, "k", "hi"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	votes, _ := s2.ByKey(ctx, "k")
	if len(votes) != 2 {
		t.Fatalf("got %d votes after replay, want 2", len(votes))
	}
	if votes[0].Comment != "hi" || votes[1].Comment != "" {
		t.Fatalf("comments after replay: %+v", votes)
	}
	if keys, _ := s2.Keys(ctx); len(keys) != 1 || keys[0].Count != 2 {
		t.Fatalf("keys = %+v", keys)
	}
}
