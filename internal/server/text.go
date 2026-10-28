package server

import (
	"fmt"
	"strings"

	"github.com/elliottsam/rail-timetable-mcp-uk/internal/rdm"
)

// boardText renders a board as a compact summary for models that read the
// text content rather than the structured output.
func boardText(heading, direction, filterCRS string, b BoardOutput) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s (%s)", heading, b.Station.Name, b.Station.CRS)
	if filterCRS != "" {
		fmt.Fprintf(&sb, ", only trains %s %s", direction, filterCRS)
	}
	fmt.Fprintf(&sb, ": %d %s\n", len(b.Services), plural(len(b.Services), "service", "services"))

	for _, s := range b.Services {
		fmt.Fprintf(&sb, "%s %s %s", s.Scheduled, directionWord(heading), joinLocations(s, heading))
		if s.Platform != "" {
			fmt.Fprintf(&sb, ", platform %s", s.Platform)
		}
		if s.Operator != "" {
			fmt.Fprintf(&sb, ", %s", s.Operator)
		}
		fmt.Fprintf(&sb, " - %s", statusText(s.Expected, s.Scheduled, s.Cancelled))
		if s.CancelReason != "" {
			fmt.Fprintf(&sb, " (%s)", s.CancelReason)
		}
		if s.DelayReason != "" {
			fmt.Fprintf(&sb, " (%s)", s.DelayReason)
		}
		fmt.Fprintf(&sb, " [id %s]\n", s.ID)
	}
	if b.Truncated {
		sb.WriteString("More services are available; raise limit to see them.\n")
	}
	for _, m := range b.Messages {
		fmt.Fprintf(&sb, "Notice: %s\n", m)
	}
	return sb.String()
}

func directionWord(heading string) string {
	if strings.HasPrefix(heading, "Arrivals") {
		return "from"
	}
	return "to"
}

func joinLocations(s rdm.Service, heading string) string {
	locs := s.Destinations
	if strings.HasPrefix(heading, "Arrivals") {
		locs = s.Origins
	}
	names := make([]string, 0, len(locs))
	for _, l := range locs {
		names = append(names, l.Name)
	}
	if len(names) == 0 {
		return "unknown"
	}
	return strings.Join(names, " and ")
}

func statusText(expected, scheduled string, cancelled bool) string {
	switch {
	case cancelled:
		return "CANCELLED"
	case expected == "":
		return "no estimate"
	case expected == scheduled || strings.EqualFold(expected, "on time"):
		return "on time"
	case strings.Contains(expected, ":"):
		return "expected " + expected
	}
	return strings.ToLower(expected)
}

func detailText(d *rdm.ServiceDetail) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Service %s", d.ID)
	if d.Operator != "" {
		fmt.Fprintf(&sb, " (%s)", d.Operator)
	}
	if d.Cancelled {
		sb.WriteString(" - CANCELLED")
		if d.CancelReason != "" {
			fmt.Fprintf(&sb, ": %s", d.CancelReason)
		}
	} else if d.DelayReason != "" {
		fmt.Fprintf(&sb, " - delayed: %s", d.DelayReason)
	}
	sb.WriteString("\n")

	writeStops(&sb, "Previous stops", d.Previous)
	if d.Location.Name != "" {
		fmt.Fprintf(&sb, "Now at: %s (%s)", d.Location.Name, d.Location.CRS)
		if d.Platform != "" {
			fmt.Fprintf(&sb, ", platform %s", d.Platform)
		}
		sb.WriteString("\n")
	}
	writeStops(&sb, "Next stops", d.Subsequent)
	return sb.String()
}

func writeStops(sb *strings.Builder, label string, stops []rdm.CallingPoint) {
	if len(stops) == 0 {
		return
	}
	sb.WriteString(label + ":\n")
	for _, p := range stops {
		fmt.Fprintf(sb, "  %s %s - %s\n", p.Scheduled, p.Name, statusText(p.Expected, p.Scheduled, p.Cancelled))
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
