package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"
	"time"
)

const benchmarkCorpusSeed int64 = 20261017

// BenchmarkSearchEntries builds the same deterministic mixed-language corpus at
// both target sizes. Setup is outside each timer: the benchmark measures the
// search paths, not migration or fixture creation.
func BenchmarkSearchEntries(b *testing.B) {
	for _, size := range []int{10_000, 100_000} {
		b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) {
			s := benchmarkSearchStore(b, size)
			defer s.Close()

			for _, tc := range []struct {
				name  string
				query string
			}{
				{name: "3-rune-selective", query: "选择性"},
				{name: "3-rune-high-frequency", query: "高频词"},
				{name: "2-rune-like", query: "缓存"},
				{name: "mixed-cjk-english", query: "中文 token"},
			} {
				b.Run(tc.name, func(b *testing.B) {
					ctx := context.Background()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if _, err := s.SearchEntriesPage(ctx, EntrySearch{Query: tc.query, Limit: 20}); err != nil {
							b.Fatal(err)
						}
					}
				})
			}

			b.Run("pagination-second-page", func(b *testing.B) {
				ctx := context.Background()
				first, err := s.SearchEntriesPage(ctx, EntrySearch{Query: "高频词", Limit: 20})
				if err != nil {
					b.Fatal(err)
				}
				if first.NextCursor == "" {
					b.Fatal("benchmark corpus did not produce a second page")
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := s.SearchEntriesPage(ctx, EntrySearch{
						Query: "高频词", Limit: 20, Cursor: first.NextCursor,
					}); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

// benchmarkSearchStore inserts directly in one transaction to keep fixture
// setup bounded at 100,000 rows, then uses the production FTS backfill query.
// The fixed PRNG seed makes the corpus and its selectivity repeatable.
func benchmarkSearchStore(b *testing.B, size int) *Store {
	b.Helper()
	s, err := Open(filepath.Join(b.TempDir(), "sessions.db"))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	if err := s.CreateSession(ctx, Session{ID: "benchmark", Cwd: "/benchmark", CreatedAt: now, UpdatedAt: now}); err != nil {
		s.Close()
		b.Fatal(err)
	}

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		s.Close()
		b.Fatal(err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO entries(session_id, seq, turn_id, type, json, created_at) VALUES (?, ?, '', ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		s.Close()
		b.Fatal(err)
	}
	rng := rand.New(rand.NewSource(benchmarkCorpusSeed))
	vocabulary := []string{"alpha", "beta", "gamma", "delta", "模型", "上下文", "数据库", "检索", "性能", "session"}
	for i := 1; i <= size; i++ {
		content := fmt.Sprintf("记录 %06d %s", i, vocabulary[rng.Intn(len(vocabulary))])
		if i%997 == 0 {
			content += " 选择性"
		}
		if i%2 == 0 {
			content += " 高频词"
		}
		if i%3 == 0 {
			content += " 缓存"
		}
		if i%5 == 0 {
			content += " 中文 token"
		}
		body, err := json.Marshal(struct {
			Content string
		}{Content: content})
		if err != nil {
			stmt.Close()
			tx.Rollback()
			s.Close()
			b.Fatal(err)
		}
		if _, err := stmt.ExecContext(ctx, "benchmark", i, EntryUser, string(body), formatTime(now)); err != nil {
			stmt.Close()
			tx.Rollback()
			s.Close()
			b.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		tx.Rollback()
		s.Close()
		b.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		s.Close()
		b.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO entries_fts(session_id, seq, role, text) `+ftsSourceSelect(` AND session_id = ?`), "benchmark"); err != nil {
		s.Close()
		b.Fatal(err)
	}
	return s
}
