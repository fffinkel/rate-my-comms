package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// Vote is one click on a vote link. A later record with the same ID
// carries the optional comment.
type Vote struct {
	ID      string    `json:"id"`
	Key     string    `json:"key"`
	Value   string    `json:"value"`
	Comment string    `json:"comment,omitempty"`
	At      time.Time `json:"at"`
}

// Store keeps votes in memory and appends every change to a JSON Lines
// file. On start it replays the file; records sharing an ID merge, so a
// comment added later overwrites the earlier empty comment.
type Store struct {
	mu    sync.Mutex
	path  string
	f     *os.File
	votes map[string]*Vote
}

var ErrNotFound = errors.New("vote not found")

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, votes: map[string]*Vote{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	s.f = f
	return s, nil
}

func (s *Store) load() error {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		if len(sc.Bytes()) == 0 {
			continue
		}
		var v Vote
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			return fmt.Errorf("%s:%d: %w", s.path, line, err)
		}
		if cur, ok := s.votes[v.ID]; ok {
			cur.Comment = v.Comment
			continue
		}
		s.votes[v.ID] = &v
	}
	return sc.Err()
}

func (s *Store) append(v *Vote) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := s.f.Write(append(b, '\n')); err != nil {
		return err
	}
	return s.f.Sync()
}

func (s *Store) Close() error { return s.f.Close() }

// Add records a vote and returns its ID.
func (s *Store) Add(key, value string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := &Vote{ID: newID(), Key: key, Value: value, At: time.Now().UTC()}
	if err := s.append(v); err != nil {
		return "", err
	}
	s.votes[v.ID] = v
	return v.ID, nil
}

// SetComment attaches a comment to an existing vote.
func (s *Store) SetComment(id, comment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.votes[id]
	if !ok {
		return ErrNotFound
	}
	upd := *v
	upd.Comment = comment
	if err := s.append(&upd); err != nil {
		return err
	}
	v.Comment = comment
	return nil
}

// ByKey returns votes for one key, oldest first.
func (s *Store) ByKey(key string) []Vote {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Vote
	for _, v := range s.votes {
		if v.Key == key {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// Keys returns every key with its vote count, most recent activity first.
func (s *Store) Keys() []KeyCount {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := map[string]*KeyCount{}
	for _, v := range s.votes {
		kc, ok := m[v.Key]
		if !ok {
			kc = &KeyCount{Key: v.Key}
			m[v.Key] = kc
		}
		kc.Count++
		if v.At.After(kc.Last) {
			kc.Last = v.At
		}
	}
	out := make([]KeyCount, 0, len(m))
	for _, kc := range m {
		out = append(out, *kc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out
}

type KeyCount struct {
	Key   string
	Count int
	Last  time.Time
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
