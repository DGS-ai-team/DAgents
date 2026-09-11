package store

import (
	"context"
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Feedback struct {
	ClientFeedbackID string `json:"client_feedback_id"`
	NodeID           string `json:"node_id"`
	Category         string `json:"category"`
	Title            string `json:"title"`
	Body             string `json:"body"`
	Status           string `json:"status"`
	Reply            string `json:"reply"`
	Revision         int    `json:"revision"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	DeliveryStatus   string `json:"delivery_status"`
	LastError        string `json:"last_error,omitempty"`
	Destination      string `json:"destination"`
}
type FeedbackStore struct {
	db *sql.DB
	mu sync.Mutex
}

func (s *FeedbackStore) Create(ctx context.Context, f Feedback) (*Feedback, bool, error) {
	old, err := s.Get(ctx, f.ClientFeedbackID)
	if err != nil {
		return nil, false, err
	}
	if old != nil {
		if old.NodeID != f.NodeID || old.Category != f.Category || old.Title != f.Title || old.Body != f.Body {
			return nil, false, fmt.Errorf("feedback id already exists with different content")
		}
		return old, false, nil
	}
	if f.Status == "" {
		f.Status = "open"
	}
	if f.Revision == 0 {
		f.Revision = 1
	}
	if f.DeliveryStatus == "" {
		f.DeliveryStatus = "pending"
	}
	if f.CreatedAt == "" {
		f.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if f.UpdatedAt == "" {
		f.UpdatedAt = f.CreatedAt
	}
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO feedback(client_feedback_id,node_id,category,title,body,status,reply,revision,created_at,updated_at,delivery_status,last_error,destination) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, f.ClientFeedbackID, f.NodeID, f.Category, f.Title, f.Body, f.Status, f.Reply, f.Revision, f.CreatedAt, f.UpdatedAt, f.DeliveryStatus, f.LastError, f.Destination)
	if err != nil {
		return nil, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		old, err := s.Get(ctx, f.ClientFeedbackID)
		if err != nil {
			return nil, false, err
		}
		if old == nil {
			return nil, false, fmt.Errorf("feedback insert conflict")
		}
		if old.NodeID != f.NodeID || old.Category != f.Category || old.Title != f.Title || old.Body != f.Body {
			return nil, false, fmt.Errorf("feedback id already exists with different content")
		}
		return old, false, nil
	}
	return &f, true, nil
}

func OpenFeedback(path string) (*FeedbackStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if _, e = db.Exec(`CREATE TABLE IF NOT EXISTS feedback (client_feedback_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,category TEXT NOT NULL,title TEXT NOT NULL,body TEXT NOT NULL,status TEXT NOT NULL,reply TEXT NOT NULL,revision INTEGER NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,delivery_status TEXT NOT NULL,last_error TEXT NOT NULL,destination TEXT NOT NULL)`); e != nil {
		db.Close()
		return nil, e
	}
	return &FeedbackStore{db: db}, nil
}
func (s *FeedbackStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
func (s *FeedbackStore) Save(ctx context.Context, f Feedback) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.Status == "" {
		f.Status = "open"
	}
	if f.Revision == 0 {
		f.Revision = 1
	}
	if f.DeliveryStatus == "" {
		f.DeliveryStatus = "pending"
	}
	if f.CreatedAt == "" {
		f.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if f.UpdatedAt == "" {
		f.UpdatedAt = f.CreatedAt
	}
	_, e := s.db.ExecContext(ctx, `INSERT INTO feedback(client_feedback_id,node_id,category,title,body,status,reply,revision,created_at,updated_at,delivery_status,last_error,destination) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(client_feedback_id) DO UPDATE SET node_id=excluded.node_id,category=excluded.category,title=excluded.title,body=excluded.body,status=excluded.status,reply=excluded.reply,revision=excluded.revision,created_at=excluded.created_at,updated_at=excluded.updated_at,delivery_status=excluded.delivery_status,last_error=excluded.last_error,destination=excluded.destination`, f.ClientFeedbackID, f.NodeID, f.Category, f.Title, f.Body, f.Status, f.Reply, f.Revision, f.CreatedAt, f.UpdatedAt, f.DeliveryStatus, f.LastError, f.Destination)
	return e
}
func (s *FeedbackStore) Get(ctx context.Context, id string) (*Feedback, error) {
	row := s.db.QueryRowContext(ctx, `SELECT client_feedback_id,node_id,category,title,body,status,reply,revision,created_at,updated_at,delivery_status,last_error,destination FROM feedback WHERE client_feedback_id=?`, id)
	var f Feedback
	e := row.Scan(&f.ClientFeedbackID, &f.NodeID, &f.Category, &f.Title, &f.Body, &f.Status, &f.Reply, &f.Revision, &f.CreatedAt, &f.UpdatedAt, &f.DeliveryStatus, &f.LastError, &f.Destination)
	if e == sql.ErrNoRows {
		return nil, nil
	}
	return &f, e
}
func (s *FeedbackStore) List(ctx context.Context) ([]Feedback, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT client_feedback_id,node_id,category,title,body,status,reply,revision,created_at,updated_at,delivery_status,last_error,destination FROM feedback ORDER BY updated_at DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Feedback
	for rows.Next() {
		var f Feedback
		if e = rows.Scan(&f.ClientFeedbackID, &f.NodeID, &f.Category, &f.Title, &f.Body, &f.Status, &f.Reply, &f.Revision, &f.CreatedAt, &f.UpdatedAt, &f.DeliveryStatus, &f.LastError, &f.Destination); e != nil {
			return nil, e
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
func (s *FeedbackStore) UpdateDelivery(ctx context.Context, id, status, lastErr string) error {
	_, e := s.db.ExecContext(ctx, `UPDATE feedback SET delivery_status=?,last_error=?,updated_at=? WHERE client_feedback_id=?`, status, lastErr, time.Now().UTC().Format(time.RFC3339Nano), id)
	return e
}
func (s *FeedbackStore) ApplyRemote(ctx context.Context, id, status, reply string, revision int) error {
	_, e := s.db.ExecContext(ctx, `UPDATE feedback SET status=?,reply=?,revision=?,updated_at=? WHERE client_feedback_id=? AND revision <= ?`, status, reply, revision, time.Now().UTC().Format(time.RFC3339Nano), id, revision)
	return e
}
func FeedbackDBPath(runtime string) string {
	return filepath.Join(strings.TrimSpace(runtime), "feedback", "feedback.db")
}
