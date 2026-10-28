package rdm

import "errors"

var (
	// ErrInvalidConfig is returned by New for a Config that cannot be used.
	ErrInvalidConfig = errors.New("invalid config")

	// ErrInvalidInput is returned when a request is malformed, either caught
	// locally or rejected by the upstream service with a 400.
	ErrInvalidInput = errors.New("invalid input")

	// ErrNoCredentials is returned when the relevant API key is not configured.
	ErrNoCredentials = errors.New("credentials not configured")

	// ErrUnauthorized is returned when the upstream service rejects the key.
	ErrUnauthorized = errors.New("upstream rejected credentials")

	// ErrNotFound is returned when the upstream service has no such resource.
	ErrNotFound = errors.New("not found")

	// ErrRateLimited is returned when the upstream rate limit is exhausted.
	ErrRateLimited = errors.New("upstream rate limit reached")

	// ErrUnavailable is returned when the upstream service is down or
	// unreachable after retries.
	ErrUnavailable = errors.New("upstream unavailable")

	// ErrBadResponse is returned when the upstream reply cannot be decoded.
	ErrBadResponse = errors.New("unexpected upstream response")
)

// BoardRequest selects a live departure or arrival board.
type BoardRequest struct {
	// CRS is the three-letter code of the station. Required.
	CRS string
	// FilterCRS limits the board to services that call at this station
	// (going to it for departures, coming from it for arrivals). Optional.
	FilterCRS string
	// Rows is the maximum number of services to request. Zero uses the
	// upstream default.
	Rows int
	// TimeOffset shifts the start of the window, in minutes from now. Optional.
	TimeOffset int
	// TimeWindow is the length of the window in minutes. Zero uses the
	// upstream default.
	TimeWindow int
}

// Location is a named station.
type Location struct {
	Name string `json:"name"`
	CRS  string `json:"crs,omitempty"`
}

// CallingPoint is a stop on a service's route.
type CallingPoint struct {
	Location
	// Scheduled is the timetabled time, "HH:MM".
	Scheduled string `json:"scheduled,omitempty"`
	// Expected is a time "HH:MM", or text such as "On time", "Delayed" or
	// "Cancelled". For a stop already made it is the actual time.
	Expected string `json:"expected,omitempty"`
	// Cancelled reports whether the train will not call here.
	Cancelled bool `json:"cancelled,omitempty"`
}

// Service is one train on a board.
type Service struct {
	// ID identifies the service for ServiceDetails.
	ID string `json:"id,omitempty"`
	// Scheduled is the timetabled departure or arrival time, "HH:MM".
	Scheduled string `json:"scheduled"`
	// Expected is a time "HH:MM", or text such as "On time", "Delayed" or
	// "Cancelled".
	Expected string `json:"expected"`
	// Platform is empty when not yet announced.
	Platform string `json:"platform,omitempty"`
	// Operator is the train operating company name.
	Operator string `json:"operator,omitempty"`
	// Origins and Destinations are the ends of the journey.
	Origins      []Location `json:"origins,omitempty"`
	Destinations []Location `json:"destinations,omitempty"`
	// Coaches is the formation length, zero if unknown.
	Coaches      int    `json:"coaches,omitempty"`
	Cancelled    bool   `json:"cancelled,omitempty"`
	CancelReason string `json:"cancelReason,omitempty"`
	DelayReason  string `json:"delayReason,omitempty"`
	// CallingPoints are the stops after this station (departures) or before
	// it (arrivals).
	CallingPoints []CallingPoint `json:"callingPoints,omitempty"`
}

// Board is a live departure or arrival board.
type Board struct {
	Station Location `json:"station"`
	// GeneratedAt is the upstream timestamp as sent.
	GeneratedAt string    `json:"generatedAt,omitempty"`
	Services    []Service `json:"services"`
	// Messages are station-wide notices.
	Messages []string `json:"messages,omitempty"`
}

// ServiceDetail describes one service along its whole route.
type ServiceDetail struct {
	ID       string `json:"id,omitempty"`
	Operator string `json:"operator,omitempty"`
	// Location is the station the detail was generated for, when known.
	Location     Location `json:"location"`
	Platform     string   `json:"platform,omitempty"`
	Cancelled    bool     `json:"cancelled,omitempty"`
	CancelReason string   `json:"cancelReason,omitempty"`
	DelayReason  string   `json:"delayReason,omitempty"`
	// Previous and Subsequent are the stops before and after Location.
	Previous   []CallingPoint `json:"previous,omitempty"`
	Subsequent []CallingPoint `json:"subsequent,omitempty"`
}
