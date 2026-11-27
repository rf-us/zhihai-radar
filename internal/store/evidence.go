package store

import (
	"context"
	"time"
)

type Evidence struct {
	Source      string     `json:"source"`
	Title       string     `json:"title"`
	Snippet     string     `json:"snippet"`
	URL         string     `json:"url"`
	Votes       int        `json:"votes"`
	Comments    int        `json:"comments"`
	Authority   float64    `json:"authority"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	Rank        int        `json:"rank_no"`
}

func (s *Store) SaveEvidence(ctx context.Context, opportunityID int64, items []Evidence) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // 提交成功后回滚是空操作

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO opportunity_evidence
		(opportunity_id,source,title,snippet,url,votes,comments,authority,published_at,rank_no)
		VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, it := range items {
		var published any
		if it.PublishedAt != nil {
			published = it.PublishedAt.UTC().Format(time.RFC3339)
		}
		if _, err := stmt.ExecContext(ctx, opportunityID, it.Source, it.Title, it.Snippet,
			it.URL, it.Votes, it.Comments, it.Authority, published, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListEvidence(ctx context.Context, opportunityID int64) ([]Evidence, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source,title,snippet,url,votes,comments,authority,published_at,rank_no
		FROM opportunity_evidence WHERE opportunity_id=? ORDER BY rank_no`, opportunityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Evidence, 0, 16)
	for rows.Next() {
		var e Evidence
		var published *string
		if err := rows.Scan(&e.Source, &e.Title, &e.Snippet, &e.URL, &e.Votes, &e.Comments,
			&e.Authority, &published, &e.Rank); err != nil {
			return nil, err
		}
		if published != nil {
			if t, err := time.Parse(time.RFC3339, *published); err == nil {
				e.PublishedAt = &t
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
