package handlers

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/go-chi/chi/v5"
	"github.com/romanzipp/linke-calendar/internal/calendar"
	"github.com/romanzipp/linke-calendar/internal/database"
)

type Scraper interface {
	ScrapeOrganization(orgID int) error
}

type Handler struct {
	db        *database.DB
	scraper   Scraper
	templates *template.Template
	version   string
}

func New(db *database.DB, scraper Scraper, version string) (*Handler, error) {
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"truncate": truncateText,
	}).ParseGlob("web/templates/*.html")
	if err != nil {
		return nil, err
	}

	return &Handler{
		db:        db,
		scraper:   scraper,
		templates: tmpl,
		version:   version,
	}, nil
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (h *Handler) Calendar(w http.ResponseWriter, r *http.Request) {
	orgID, campaignIDs, ok := parseOrgAndCampaign(w, r)
	if !ok {
		return
	}

	hasEvents, err := h.db.HasEventsForOrganization(orgID)
	if err != nil {
		log.Printf("Failed to check events for organization %d: %v", orgID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !hasEvents {
		log.Printf("No events for organization %d, scraping synchronously", orgID)
		if err := h.scraper.ScrapeOrganization(orgID); err != nil {
			log.Printf("Failed to scrape organization %d: %v", orgID, err)
			http.Error(w, "Failed to fetch events", http.StatusInternalServerError)
			return
		}
	}

	yearStr := r.URL.Query().Get("year")
	monthStr := r.URL.Query().Get("month")

	now := time.Now()
	year := now.Year()
	month := now.Month()

	if yearStr != "" {
		if y, err := strconv.Atoi(yearStr); err == nil {
			year = y
		}
	}

	if monthStr != "" {
		if m, err := strconv.Atoi(monthStr); err == nil && m >= 1 && m <= 12 {
			month = time.Month(m)
		}
	}

	startDate := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	endDate := startDate.AddDate(0, 1, 0)

	events, err := h.db.GetEventsByOrganizationInRange(orgID, startDate, endDate, campaignIDs)
	if err != nil {
		log.Printf("Failed to get events: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	org, err := h.db.GetOrganization(orgID)
	if err != nil {
		log.Printf("Failed to get organization %d: %v", orgID, err)
	}

	cal := calendar.Generate(year, month, events)

	data := struct {
		OrganizationID    int
		OrganizationTitle string
		BasePath          string
		Calendar          *calendar.Month
		Year              int
		Month             int
		PrevYear          int
		PrevMonth         int
		NextYear          int
		NextMonth         int
		Version           string
	}{
		OrganizationID:    orgID,
		OrganizationTitle: getOrganizationTitle(org),
		BasePath:          basePath(orgID, campaignIDs),
		Calendar:          cal,
		Year:              year,
		Month:             int(month),
		Version:           h.version,
	}

	prevMonth := month - 1
	prevYear := year
	if prevMonth == 0 {
		prevMonth = 12
		prevYear--
	}
	data.PrevYear = prevYear
	data.PrevMonth = int(prevMonth)

	nextMonth := month + 1
	nextYear := year
	if nextMonth == 13 {
		nextMonth = 1
		nextYear++
	}
	data.NextYear = nextYear
	data.NextMonth = int(nextMonth)

	if r.Header.Get("HX-Request") == "true" {
		if err := h.templates.ExecuteTemplate(w, "calendar-content", data); err != nil {
			log.Printf("Failed to render calendar content: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	if err := h.templates.ExecuteTemplate(w, "calendar.html", data); err != nil {
		log.Printf("Failed to render calendar: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) EventDetail(w http.ResponseWriter, r *http.Request) {
	eventIDStr := chi.URLParam(r, "eventID")
	eventID, err := strconv.Atoi(eventIDStr)
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}

	event, err := h.db.GetEvent(eventID)
	if err != nil {
		log.Printf("Failed to get event %d: %v", eventID, err)
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}

	data := struct {
		Event *database.Event
	}{
		Event: event,
	}

	if err := h.templates.ExecuteTemplate(w, "event-modal", data); err != nil {
		log.Printf("Failed to render event modal: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	orgID, campaignIDs, ok := parseOrgAndCampaign(w, r)
	if !ok {
		return
	}

	hasEvents, err := h.db.HasEventsForOrganization(orgID)
	if err != nil {
		log.Printf("Failed to check events for organization %d: %v", orgID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !hasEvents {
		log.Printf("No events for organization %d, scraping synchronously", orgID)
		if err := h.scraper.ScrapeOrganization(orgID); err != nil {
			log.Printf("Failed to scrape organization %d: %v", orgID, err)
			http.Error(w, "Failed to fetch events", http.StatusInternalServerError)
			return
		}
	}

	org, err := h.db.GetOrganization(orgID)
	if err != nil {
		log.Printf("Failed to get organization %d: %v", orgID, err)
	}

	color := r.URL.Query().Get("color")

	events, err := h.db.GetAllUpcomingEventsByOrganization(orgID, campaignIDs)
	if err != nil {
		log.Printf("Failed to get upcoming events: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := struct {
		OrganizationID    int
		OrganizationTitle string
		Events            []*database.Event
		Color             string
		Version           string
	}{
		OrganizationID:    orgID,
		OrganizationTitle: getOrganizationTitle(org),
		Events:            events,
		Color:             color,
		Version:           h.version,
	}

	if err := h.templates.ExecuteTemplate(w, "list.html", data); err != nil {
		log.Printf("Failed to render list: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) ICalendar(w http.ResponseWriter, r *http.Request) {
	orgID, campaignIDs, ok := parseOrgAndCampaign(w, r)
	if !ok {
		return
	}

	hasEvents, err := h.db.HasEventsForOrganization(orgID)
	if err != nil {
		log.Printf("Failed to check events for organization %d: %v", orgID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !hasEvents {
		log.Printf("No events for organization %d, scraping synchronously", orgID)
		if err := h.scraper.ScrapeOrganization(orgID); err != nil {
			log.Printf("Failed to scrape organization %d: %v", orgID, err)
			http.Error(w, "Failed to fetch events", http.StatusInternalServerError)
			return
		}
	}

	org, err := h.db.GetOrganization(orgID)
	if err != nil {
		log.Printf("Failed to get organization %d: %v", orgID, err)
	}

	title := getOrganizationTitle(org)

	events, err := h.db.GetEventsByOrganization(orgID, campaignIDs)
	if err != nil {
		log.Printf("Failed to get events for organization %d: %v", orgID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		log.Printf("Failed to load Berlin timezone: %v", err)
		berlin = time.UTC
	}

	cal := ics.NewCalendar()
	cal.SetMethod(ics.MethodPublish)
	cal.SetName(title)
	cal.SetDescription(fmt.Sprintf("Events calendar for %s", title))
	cal.SetXPublishedTTL("PT1H")
	cal.SetTimezoneId("Europe/Berlin")

	for _, event := range events {
		icalEvent := cal.AddEvent(fmt.Sprintf("%d-%d@linke-calendar", orgID, event.ID))
		icalEvent.SetCreatedTime(event.CreatedAt)
		icalEvent.SetModifiedAt(event.UpdatedAt)
		icalEvent.SetStartAt(reinterpretTimeInLocation(event.DatetimeStart, berlin))

		if event.DatetimeEnd.Valid {
			duration := event.DatetimeEnd.Time.Sub(event.DatetimeStart)
			if duration > 4*24*time.Hour {
				icalEvent.SetEndAt(reinterpretTimeInLocation(event.DatetimeStart, berlin).Add(1 * time.Hour))
			} else {
				icalEvent.SetEndAt(reinterpretTimeInLocation(event.DatetimeEnd.Time, berlin))
			}
		} else {
			icalEvent.SetEndAt(reinterpretTimeInLocation(event.DatetimeStart, berlin).Add(1 * time.Hour))
		}

		icalEvent.SetSummary(event.Title)

		if event.Description.Valid {
			icalEvent.SetDescription(event.Description.String)
		}

		if event.Location.Valid {
			icalEvent.SetLocation(event.Location.String)
		}

		if event.URL != "" {
			icalEvent.SetURL(event.URL)
		}
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	filename := fmt.Sprintf("%d.ics", orgID)
	if len(campaignIDs) > 0 {
		filename = fmt.Sprintf("%d-%s.ics", orgID, joinCampaignIDs(campaignIDs, "-"))
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))

	if err := cal.SerializeTo(w); err != nil {
		log.Printf("Failed to serialize iCal: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// maxCampaigns limits how many campaigns can be combined in one URL.
const maxCampaigns = 50

// parseOrgAndCampaign reads the organization ID and the optional campaign IDs
// from the URL path. Multiple campaigns are separated by "+", e.g.
// /org/42/campaign/17+18/calendar. An empty slice means "all campaigns".
// On invalid input an error response is written and ok is false.
func parseOrgAndCampaign(w http.ResponseWriter, r *http.Request) (orgID int, campaignIDs []int, ok bool) {
	orgID, err := strconv.Atoi(chi.URLParam(r, "org"))
	if err != nil {
		http.Error(w, "Invalid organization ID", http.StatusBadRequest)
		return 0, nil, false
	}

	campaignStr := chi.URLParam(r, "campaign")
	if campaignStr == "" {
		return orgID, nil, true
	}
	// chi returns the raw segment if the path contains escaped characters
	// such as "%2B", so unescape it before splitting.
	if unescaped, err := url.PathUnescape(campaignStr); err == nil {
		campaignStr = unescaped
	}

	parts := strings.Split(campaignStr, "+")
	if len(parts) > maxCampaigns {
		http.Error(w, fmt.Sprintf("Too many campaigns (max %d)", maxCampaigns), http.StatusBadRequest)
		return 0, nil, false
	}

	seen := make(map[int]bool, len(parts))
	for _, part := range parts {
		id, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || id <= 0 {
			http.Error(w, "Invalid campaign ID", http.StatusBadRequest)
			return 0, nil, false
		}
		if !seen[id] {
			seen[id] = true
			campaignIDs = append(campaignIDs, id)
		}
	}

	return orgID, campaignIDs, true
}

// joinCampaignIDs joins campaign IDs with the given separator.
func joinCampaignIDs(campaignIDs []int, sep string) string {
	parts := make([]string, len(campaignIDs))
	for i, id := range campaignIDs {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, sep)
}

// basePath returns the URL prefix for an organization, including the
// campaign segment if a campaign filter is active.
func basePath(orgID int, campaignIDs []int) string {
	if len(campaignIDs) > 0 {
		return fmt.Sprintf("/org/%d/campaign/%s", orgID, joinCampaignIDs(campaignIDs, "+"))
	}
	return fmt.Sprintf("/org/%d", orgID)
}

// truncateText collapses whitespace and shortens s to at most maxRunes
// characters, cutting at a word boundary where possible and appending "…".
func truncateText(maxRunes int, s string) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}

	cut := runes[:maxRunes]
	if i := strings.LastIndex(string(cut), " "); i > 0 {
		cut = []rune(string(cut)[:i])
	}
	return strings.TrimRight(string(cut), " .,;:-") + "…"
}

func reinterpretTimeInLocation(t time.Time, loc *time.Location) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
}

func getOrganizationTitle(org *database.Organization) string {
	if org != nil && org.Title.Valid {
		return org.Title.String
	}
	if org != nil {
		return fmt.Sprintf("Organization %d", org.ID)
	}
	return "Unknown Organization"
}
