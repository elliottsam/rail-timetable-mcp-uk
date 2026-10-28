package rdm

import (
	"encoding/json"
	"strings"
)

// The wire types mirror the upstream JSON. Field names follow the National
// Rail Live Departure Boards schema; encoding/json matches them
// case-insensitively. The exact gateway shape is unverified until a key is
// available, so every field is optional and decoding is deliberately lenient.

type wireLocation struct {
	LocationName string `json:"locationName"`
	CRS          string `json:"crs"`
}

type wireCallingPoint struct {
	LocationName string `json:"locationName"`
	CRS          string `json:"crs"`
	ST           string `json:"st"`
	ET           string `json:"et"`
	AT           string `json:"at"`
	IsCancelled  bool   `json:"isCancelled"`
}

type wireCallingPointList struct {
	CallingPoint []wireCallingPoint `json:"callingPoint"`
}

type wireService struct {
	ServiceID    string         `json:"serviceID"`
	STD          string         `json:"std"`
	ETD          string         `json:"etd"`
	STA          string         `json:"sta"`
	ETA          string         `json:"eta"`
	Platform     string         `json:"platform"`
	Operator     string         `json:"operator"`
	Length       int            `json:"length"`
	IsCancelled  bool           `json:"isCancelled"`
	CancelReason string         `json:"cancelReason"`
	DelayReason  string         `json:"delayReason"`
	Origin       []wireLocation `json:"origin"`
	Destination  []wireLocation `json:"destination"`

	SubsequentCallingPoints []wireCallingPointList `json:"subsequentCallingPoints"`
	PreviousCallingPoints   []wireCallingPointList `json:"previousCallingPoints"`
}

type wireBoard struct {
	GeneratedAt   string        `json:"generatedAt"`
	LocationName  string        `json:"locationName"`
	CRS           string        `json:"crs"`
	TrainServices []wireService `json:"trainServices"`
	NRCCMessages  []wireMessage `json:"nrccMessages"`
}

type wireDetail struct {
	GeneratedAt  string `json:"generatedAt"`
	RID          string `json:"rid"`
	LocationName string `json:"locationName"`
	CRS          string `json:"crs"`
	Operator     string `json:"operator"`
	Platform     string `json:"platform"`
	IsCancelled  bool   `json:"isCancelled"`
	CancelReason string `json:"cancelReason"`
	DelayReason  string `json:"delayReason"`

	SubsequentCallingPoints []wireCallingPointList `json:"subsequentCallingPoints"`
	PreviousCallingPoints   []wireCallingPointList `json:"previousCallingPoints"`
}

// wireMessage accepts either a bare string or an object with a "value" field,
// and strips the HTML the upstream service embeds in notices.
type wireMessage string

func (m *wireMessage) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		var obj struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(b, &obj); err != nil {
			return err
		}
		s = obj.Value
	}
	*m = wireMessage(stripTags(s))
	return nil
}

func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>' && inTag:
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func (w wireBoard) toBoard() *Board {
	b := &Board{
		Station:     Location{Name: w.LocationName, CRS: w.CRS},
		GeneratedAt: w.GeneratedAt,
		Services:    make([]Service, 0, len(w.TrainServices)),
	}
	for _, s := range w.TrainServices {
		b.Services = append(b.Services, s.toService())
	}
	for _, m := range w.NRCCMessages {
		if m != "" {
			b.Messages = append(b.Messages, string(m))
		}
	}
	return b
}

func (w wireService) toService() Service {
	expected := first(w.ETD, w.ETA)
	return Service{
		ID:            w.ServiceID,
		Scheduled:     first(w.STD, w.STA),
		Expected:      expected,
		Platform:      w.Platform,
		Operator:      w.Operator,
		Origins:       toLocations(w.Origin),
		Destinations:  toLocations(w.Destination),
		Coaches:       max(w.Length, 0),
		Cancelled:     w.IsCancelled || strings.EqualFold(expected, "cancelled"),
		CancelReason:  w.CancelReason,
		DelayReason:   w.DelayReason,
		CallingPoints: toCallingPoints(first2(w.SubsequentCallingPoints, w.PreviousCallingPoints)),
	}
}

func (w wireDetail) toDetail(requestedID string) *ServiceDetail {
	return &ServiceDetail{
		ID:           first(w.RID, requestedID),
		Operator:     w.Operator,
		Location:     Location{Name: w.LocationName, CRS: w.CRS},
		Platform:     w.Platform,
		Cancelled:    w.IsCancelled,
		CancelReason: w.CancelReason,
		DelayReason:  w.DelayReason,
		Previous:     toCallingPoints(w.PreviousCallingPoints),
		Subsequent:   toCallingPoints(w.SubsequentCallingPoints),
	}
}

func toLocations(in []wireLocation) []Location {
	if len(in) == 0 {
		return nil
	}
	out := make([]Location, len(in))
	for i, l := range in {
		out[i] = Location{Name: l.LocationName, CRS: l.CRS}
	}
	return out
}

func toCallingPoints(lists []wireCallingPointList) []CallingPoint {
	var out []CallingPoint
	for _, l := range lists {
		for _, p := range l.CallingPoint {
			out = append(out, CallingPoint{
				Location:  Location{Name: p.LocationName, CRS: p.CRS},
				Scheduled: p.ST,
				Expected:  first(p.AT, p.ET),
				Cancelled: p.IsCancelled || strings.EqualFold(p.ET, "cancelled"),
			})
		}
	}
	return out
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func first2(a, b []wireCallingPointList) []wireCallingPointList {
	if len(a) > 0 {
		return a
	}
	return b
}
