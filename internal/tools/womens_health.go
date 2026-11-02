package tools

import (
	"context"
	"encoding/json"

	"github.com/mijndert/garmin-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type menstrualDayViewInput struct {
	Date            string `json:"date,omitempty" jsonschema:"Calendar date, YYYY-MM-DD. Defaults to today."`
	TodayDate       string `json:"today_date,omitempty" jsonschema:"The caller's current date, YYYY-MM-DD, used as the reference point for predictions. Defaults to today."`
	ReducedResponse *bool  `json:"reduced_response,omitempty" jsonschema:"Return a lightweight payload. Default false."`
	SkipBabyMove    *bool  `json:"skip_baby_movement,omitempty" jsonschema:"Exclude baby movement (kick count) data for pregnancy tracking. Default false."`
	IncludeAnalysis *bool  `json:"include_analysis,omitempty" jsonschema:"Include cycle analysis data. Default false."`
}

func registerWomensHealth(s *mcp.Server, d *Deps) {
	addJSON(s, d, "get_menstrual_cycle_summary", "Get menstrual cycle summary",
		"Get the menstrual cycle summary for a date: current cycle day and phase, cycle and period length, predicted period and fertile window dates, and pregnancy tracking status if applicable.",
		func(ctx context.Context, d *Deps, in optionalDateInput) (json.RawMessage, error) {
			date, err := garmin.DateOrToday("date", in.Date)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, "/periodichealth-service/menstrualcycle/summary/"+date, nil)
		})

	addJSON(s, d, "get_menstrual_cycle_day_view", "Get menstrual cycle day view",
		"Get detailed menstrual cycle data for one day: logged symptoms, flow, mood and notes, the cycle summary for that day, and (for pregnancy tracking) weekly weight goal ranges.",
		func(ctx context.Context, d *Deps, in menstrualDayViewInput) (json.RawMessage, error) {
			date, err := garmin.DateOrToday("date", in.Date)
			if err != nil {
				return nil, err
			}
			today, err := garmin.DateOrToday("today_date", in.TodayDate)
			if err != nil {
				return nil, err
			}
			q := garmin.Query{}.
				Set("todayDate", today).
				SetBool("reducedResponse", in.ReducedResponse).
				SetBool("skipbm", in.SkipBabyMove).
				SetBool("includeAnalysis", in.IncludeAnalysis)
			return d.Client.GetJSON(ctx, "/periodichealth-service/menstrualcycle/dayview/"+date, q)
		})
}
