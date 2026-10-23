package choicestats

import (
	"errors"
	"sort"
	"strings"
	"time"

	"vozko/domain/shared"
)

var (
	ErrInvalidPeriod = errors.New("choice stats: the period must be whole days, from on or before to, within the counter retention")
	ErrUnavailable   = errors.New("choice stats: the counters could not be read")
)

type CountRow struct {
	NodeID   string
	OptionID string
	Shown    int64
	Clicks   int64
}

type Rate struct {
	Count int64 `json:"count" example:"87"`
	Rate  int   `json:"rate" example:"499"`
}

type OptionStats struct {
	OptionID string `json:"optionId" example:"sim"`
	Shown    int64  `json:"shown" example:"1742"`
	Clicks   int64  `json:"clicks" example:"244"`
	CTR      int    `json:"ctr" example:"1401"`
	Tracked  *bool  `json:"tracked,omitempty"`
}

type PromptStats struct {
	Sends          int64         `json:"sends" example:"1742"`
	ClickedPrompts int64         `json:"clickedPrompts" example:"1394"`
	ClickRate      int           `json:"clickRate" example:"8003"`
	FreeText       Rate          `json:"freeText"`
	Options        []OptionStats `json:"options"`
}

type NodeStats struct {
	NodeID         string        `json:"nodeId" example:"node_ask"`
	Sends          int64         `json:"sends" example:"1742"`
	ClickedPrompts int64         `json:"clickedPrompts" example:"1394"`
	ClickRate      int           `json:"clickRate" example:"8003"`
	FreeText       Rate          `json:"freeText"`
	Options        []OptionStats `json:"options"`
}

type OfferedOption struct {
	ID      string
	Tracked *bool
}

type CanvasNode struct {
	NodeID  string
	Options []OfferedOption
}

func Summarize(rows []CountRow, order []OfferedOption) PromptStats {
	out := PromptStats{Options: []OptionStats{}}
	byOption := map[string]CountRow{}
	for _, row := range rows {
		switch row.OptionID {
		case OptionTotal:
			out.Sends += row.Shown
			out.ClickedPrompts += row.Clicks
		case OptionFreeText:
			out.FreeText.Count += row.Clicks
		default:
			acc := byOption[row.OptionID]
			acc.OptionID = row.OptionID
			acc.Shown += row.Shown
			acc.Clicks += row.Clicks
			byOption[row.OptionID] = acc
		}
	}
	out.ClickRate = shared.BasisPoints(out.ClickedPrompts, out.Sends)
	out.FreeText.Rate = shared.BasisPoints(out.FreeText.Count, out.Sends)
	if order == nil {
		ids := make([]string, 0, len(byOption))
		for id := range byOption {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			order = append(order, OfferedOption{ID: id})
		}
	}
	for _, offered := range order {
		row := byOption[offered.ID]
		out.Options = append(out.Options, OptionStats{
			OptionID: offered.ID,
			Shown:    row.Shown,
			Clicks:   row.Clicks,
			CTR:      shared.BasisPoints(row.Clicks, row.Shown),
			Tracked:  offered.Tracked,
		})
	}
	return out
}

func SummarizeNodes(rows []CountRow, canvas []CanvasNode) []NodeStats {
	byNode := map[string][]CountRow{}
	for _, row := range rows {
		byNode[row.NodeID] = append(byNode[row.NodeID], row)
	}
	out := []NodeStats{}
	for _, node := range canvas {
		nodeRows, ok := byNode[node.NodeID]
		if !ok {
			continue
		}
		summary := Summarize(nodeRows, node.Options)
		if summary.Sends == 0 {
			continue
		}
		out = append(out, NodeStats{
			NodeID:         node.NodeID,
			Sends:          summary.Sends,
			ClickedPrompts: summary.ClickedPrompts,
			ClickRate:      summary.ClickRate,
			FreeText:       summary.FreeText,
			Options:        summary.Options,
		})
	}
	return out
}

type Period struct {
	window shared.DayWindow
}

func NewPeriod(from, to string, loc *time.Location) (Period, error) {
	if loc == nil {
		return Period{}, ErrZoneUnknown
	}
	window, err := shared.NewDayWindowWithin(strings.TrimSpace(from), strings.TrimSpace(to), loc, MaxPeriodDays)
	if err != nil {
		return Period{}, ErrInvalidPeriod
	}
	return Period{window: window}, nil
}

func OptionalPeriod(from, to string, since, now time.Time, loc *time.Location) (Period, error) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from != "" || to != "" {
		return NewPeriod(from, to, loc)
	}
	if loc == nil {
		return Period{}, ErrZoneUnknown
	}
	today := now.In(loc)
	earliest := today.AddDate(0, 0, -(MaxPeriodDays - 1))
	start := since.In(loc)
	if since.IsZero() || start.Before(earliest) {
		start = earliest
	}
	if start.After(today) {
		start = today
	}
	return NewPeriod(start.Format(dayLayout), today.Format(dayLayout), loc)
}

const dayLayout = "2006-01-02"

func (p Period) FirstDay() string { return p.window.FirstDay() }

func (p Period) LastDay() string { return p.window.LastDay() }

func (p Period) Timezone() string {
	if p.window.Location == nil {
		return ""
	}
	return p.window.Location.String()
}

func (p Period) FirstDate() time.Time {
	day, _ := LocalDay(p.window.From, p.window.Location)
	return day
}

func (p Period) LastDate() time.Time {
	day, _ := LocalDay(p.window.To.AddDate(0, 0, -1), p.window.Location)
	return day
}

type WorkflowReport struct {
	From     string      `json:"from" example:"2026-09-23"`
	To       string      `json:"to" example:"2026-10-22"`
	Timezone string      `json:"timezone" example:"America/Sao_Paulo"`
	Version  *int        `json:"version" example:"4"`
	Nodes    []NodeStats `json:"nodes"`
}

type PromptReport struct {
	From     *string     `json:"from" example:"2026-09-23"`
	To       *string     `json:"to" example:"2026-10-22"`
	Timezone string      `json:"timezone" example:"America/Sao_Paulo"`
	Prompt   PromptStats `json:"prompt"`
}

var ErrInvalidVersion = errors.New("choice stats: the version must be a positive number")
