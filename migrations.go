package main

import (
	"database/sql"
	"fmt"
	"time"
)

type Storage struct {
	db *sql.DB
}

type ArchivedItem struct {
	URL       string
	Date      string
	Summary   string
	Title     string
	FeedTitle string
	FeedURL   string
}

func NewStorage(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	s := &Storage{db: db}
	if err := s.applyMigrations(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Storage) applyMigrations() error {
	var err error
	_, err = s.db.Exec("CREATE TABLE IF NOT EXISTS seen (url TEXT, date TEXT, summary TEXT)")
	if err != nil {
		return err
	}

	if err := s.addTextColumnIfNotExists("seen", "summary"); err != nil {
		return err
	}
	if err := s.addTextColumnIfNotExists("seen", "title"); err != nil {
		return err
	}
	if err := s.addTextColumnIfNotExists("seen", "feed_title"); err != nil {
		return err
	}

	if err := s.addNotificationsTableIfNotExists(); err != nil {
		return err
	}
	return nil
}

// Generic helper to add columns
func (s *Storage) addTextColumnIfNotExists(table, colName string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	exists := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == colName {
			exists = true
			break
		}
	}

	if !exists {
		_, err = tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s TEXT", table, colName))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Storage) addNotificationsTableIfNotExists() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS notifications (
			feed TEXT,
			date TEXT,
			notified INTEGER DEFAULT 0,
			PRIMARY KEY (feed, date)
		)
	`)
	return err
}

func (s *Storage) MarkAsSeen(url, summary, title, feedTitle, feedURL string) error {
	today := time.Now().Format("2006-01-02")
	_, err := s.db.Exec("INSERT INTO seen(url, date, summary, title, feed_title) values(?,?,?,?,?)",
		url, today, summary, title, feedTitle)
	return err
}

// IsSeen returns (seen, seen_today, summary)
func (s *Storage) IsSeen(link string) (bool, bool, string) {
	var urlStr, date, summary sql.NullString
	err := s.db.QueryRow("SELECT url, date, summary FROM seen WHERE url=?", link).Scan(&urlStr, &date, &summary)

	if err != nil {
		return false, false, ""
	}

	today := time.Now().Format("2006-01-02")
	isSeen := urlStr.Valid && date.String != today
	isSeenToday := urlStr.Valid && date.String == today

	return isSeen, isSeenToday, summary.String
}

func (s *Storage) GetAllArticles() (map[string][]ArchivedItem, error) {
	// Order by Date DESC, then by Feed Title so they group nicely
	rows, err := s.db.Query("SELECT url, date, summary, IFNULL(title, ''), IFNULL(feed_title, '') FROM seen ORDER BY date DESC, feed_title ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	grouped := make(map[string][]ArchivedItem)

	for rows.Next() {
		var item ArchivedItem
		var date, summary, title, feedTitle sql.NullString
		if err := rows.Scan(&item.URL, &date, &summary, &title, &feedTitle); err != nil {
			return nil, err
		}

		item.Date = date.String
		item.Summary = summary.String
		item.Title = title.String
		item.FeedTitle = feedTitle.String

		// Fallback for old records where title might be empty
		if item.Title == "" {
			item.Title = item.URL
		}
		if item.FeedTitle == "" {
			item.FeedTitle = "Unknown Feed"
		}
		if item.Date == "" {
			item.Date = "Unknown Date"
		}

		grouped[item.Date] = append(grouped[item.Date], item)
	}
	return grouped, nil
}

func (s *Storage) Close() {
	s.db.Close()
}

// MarkFeedNotified marks the feed as notified for today.
func (s *Storage) MarkFeedNotified(feed string) error {
	today := time.Now().Format("2006-01-02")
	// Upsert: insert or replace into notifications
	_, err := s.db.Exec(`
		INSERT INTO notifications(feed, date, notified) VALUES(?, ?, 1)
		ON CONFLICT(feed, date) DO UPDATE SET notified = 1
	`, feed, today)
	return err
}

// WasFeedNotifiedToday returns true if the given feed is marked notified today.
func (s *Storage) WasFeedNotifiedToday(feed string) bool {
	today := time.Now().Format("2006-01-02")
	var notified int
	err := s.db.QueryRow("SELECT IFNULL(notified,0) FROM notifications WHERE feed=? AND date=?", feed, today).Scan(&notified)
	if err != nil {
		// if no row found, QueryRow.Scan returns an error; treat as not notified
		return false
	}
	return notified == 1
}

func (s *Storage) GetAllDays() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT date
		FROM seen
		ORDER BY date ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var days []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err == nil {
			days = append(days, d)
		}
	}
	return days, nil
}

func (s *Storage) GetArticlesForDay(day string) ([]SeenArticle, error) {
	rows, err := s.db.Query(`
		SELECT url, summary
		FROM seen
		WHERE date = ?
	`, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []SeenArticle
	for rows.Next() {
		var a SeenArticle
		rows.Scan(&a.URL, &a.Summary)
		res = append(res, a)
	}
	return res, nil
}

type SeenArticle struct {
	URL     string
	Summary string
}
