package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Event struct {
	ID             int
	OrganizationID int
	Title          string
	Description    sql.NullString
	DatetimeStart  time.Time
	DatetimeEnd    sql.NullTime
	URL            string
	Location       sql.NullString
	Scraper        string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Campaign       string
	CampaignID     sql.NullInt64
}

// campaignFilter builds an SQL condition restricting a query to the given
// campaigns, together with its arguments. An empty list disables the filter
// and matches all events of the organization.
func campaignFilter(campaignIDs []int) (string, []interface{}) {
	if len(campaignIDs) == 0 {
		return "1 = 1", nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(campaignIDs)), ", ")
	args := make([]interface{}, len(campaignIDs))
	for i, id := range campaignIDs {
		args[i] = id
	}
	return "campaign_id IN (" + placeholders + ")", args
}

func (db *DB) CreateEvent(event *Event) error {
	query := `
		INSERT INTO events (
			organization_id, title, description, datetime_start, datetime_end,
			url, location, scraper, campaign, campaign_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	result, err := db.Exec(
		query,
		event.OrganizationID,
		event.Title,
		event.Description,
		event.DatetimeStart,
		event.DatetimeEnd,
		event.URL,
		event.Location,
		event.Scraper,
		event.Campaign,
		event.CampaignID,
	)
	if err != nil {
		return fmt.Errorf("failed to create event: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}

	event.ID = int(id)
	return nil
}

func (db *DB) GetEvent(id int) (*Event, error) {
	query := `
		SELECT id, organization_id, title, description, datetime_start, datetime_end,
		       url, location, scraper, created_at, updated_at, COALESCE(campaign, ''), campaign_id
		FROM events WHERE id = ?
	`
	var event Event
	err := db.QueryRow(query, id).Scan(
		&event.ID,
		&event.OrganizationID,
		&event.Title,
		&event.Description,
		&event.DatetimeStart,
		&event.DatetimeEnd,
		&event.URL,
		&event.Location,
		&event.Scraper,
		&event.CreatedAt,
		&event.UpdatedAt,
		&event.Campaign,
		&event.CampaignID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get event: %w", err)
	}
	return &event, nil
}

func (db *DB) GetEventsByOrganization(orgID int, campaignIDs []int) ([]*Event, error) {
	filter, filterArgs := campaignFilter(campaignIDs)
	query := `
		SELECT id, organization_id, title, description, datetime_start, datetime_end,
		       url, location, scraper, created_at, updated_at, COALESCE(campaign, ''), campaign_id
		FROM events
		WHERE organization_id = ? AND ` + filter + `
		ORDER BY datetime_start ASC
	`
	args := append([]interface{}{orgID}, filterArgs...)
	return db.queryEvents(query, args...)
}

func (db *DB) GetEventsByOrganizationInRange(orgID int, start, end time.Time, campaignIDs []int) ([]*Event, error) {
	filter, filterArgs := campaignFilter(campaignIDs)
	query := `
		SELECT id, organization_id, title, description, datetime_start, datetime_end,
		       url, location, scraper, created_at, updated_at, COALESCE(campaign, ''), campaign_id
		FROM events
		WHERE organization_id = ? AND datetime_start >= ? AND datetime_start < ? AND ` + filter + `
		ORDER BY datetime_start ASC
	`
	args := append([]interface{}{orgID, start, end}, filterArgs...)
	return db.queryEvents(query, args...)
}

func (db *DB) GetUpcomingEventsByOrganization(orgID int, campaignIDs []int, limit int) ([]*Event, error) {
	filter, filterArgs := campaignFilter(campaignIDs)
	query := `
		SELECT id, organization_id, title, description, datetime_start, datetime_end,
		       url, location, scraper, created_at, updated_at, COALESCE(campaign, ''), campaign_id
		FROM events
		WHERE organization_id = ? AND datetime_start >= datetime('now') AND ` + filter + `
		ORDER BY datetime_start ASC
		LIMIT ?
	`
	args := append([]interface{}{orgID}, filterArgs...)
	return db.queryEvents(query, append(args, limit)...)
}

func (db *DB) GetAllUpcomingEventsByOrganization(orgID int, campaignIDs []int) ([]*Event, error) {
	filter, filterArgs := campaignFilter(campaignIDs)
	query := `
		SELECT id, organization_id, title, description, datetime_start, datetime_end,
		       url, location, scraper, created_at, updated_at, COALESCE(campaign, ''), campaign_id
		FROM events
		WHERE organization_id = ? AND datetime_start >= datetime('now') AND ` + filter + `
		ORDER BY datetime_start ASC
	`
	args := append([]interface{}{orgID}, filterArgs...)
	return db.queryEvents(query, args...)
}

func (db *DB) UpsertEvent(event *Event) error {
	query := `
		INSERT INTO events (
			organization_id, title, description, datetime_start, datetime_end,
			url, location, scraper, campaign, campaign_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET
			title = excluded.title,
			description = excluded.description,
			datetime_start = excluded.datetime_start,
			datetime_end = excluded.datetime_end,
			location = excluded.location,
			scraper = excluded.scraper,
			updated_at = CURRENT_TIMESTAMP,
			campaign = excluded.campaign,
			campaign_id = excluded.campaign_id
	`
	_, err := db.Exec(
		query,
		event.OrganizationID,
		event.Title,
		event.Description,
		event.DatetimeStart,
		event.DatetimeEnd,
		event.URL,
		event.Location,
		event.Scraper,
		event.Campaign,
		event.CampaignID,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert event: %w", err)
	}
	return nil
}

func (db *DB) DeleteOldEvents(before time.Time) error {
	query := `DELETE FROM events WHERE datetime_start < ?`
	_, err := db.Exec(query, before)
	if err != nil {
		return fmt.Errorf("failed to delete old events: %w", err)
	}
	return nil
}

func (db *DB) queryEvents(query string, args ...interface{}) ([]*Event, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(
			&event.ID,
			&event.OrganizationID,
			&event.Title,
			&event.Description,
			&event.DatetimeStart,
			&event.DatetimeEnd,
			&event.URL,
			&event.Location,
			&event.Scraper,
			&event.CreatedAt,
			&event.UpdatedAt,
			&event.Campaign,
			&event.CampaignID,
		); err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		events = append(events, &event)
	}

	return events, nil
}
