package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mijndert/garmin-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type optionalDateInput struct {
	Date string `json:"date,omitempty" jsonschema:"Calendar date, YYYY-MM-DD. Defaults to today."`
}

type dateInput struct {
	Date string `json:"date" jsonschema:"Calendar date, YYYY-MM-DD."`
}

type dateRangeInput struct {
	StartDate string `json:"start_date" jsonschema:"Inclusive start date, YYYY-MM-DD."`
	EndDate   string `json:"end_date" jsonschema:"Inclusive end date, YYYY-MM-DD."`
}

type heartRateZonesInput struct {
	GenerateDefaults *bool `json:"generate_defaults,omitempty" jsonschema:"Generate default zones when the user has not configured any. Default true."`
}

type fitnessAgeInput struct {
	Date            string `json:"date,omitempty" jsonschema:"Target date, YYYY-MM-DD; the latest Fitness Age available as of this date is returned. Defaults to today."`
	FullHistoryScan *bool  `json:"full_history_scan,omitempty" jsonschema:"Scan the full history for the latest Fitness Age (default true). When false only the 28 days before the date are considered."`
}

type nutritionInput struct {
	Date             string `json:"date,omitempty" jsonschema:"Calendar date, YYYY-MM-DD, in the user's local time zone. Defaults to today."`
	IncludeGoals     *bool  `json:"include_goals,omitempty" jsonschema:"Include nutrition goals. Default true."`
	IncludeNutrition *bool  `json:"include_nutrition,omitempty" jsonschema:"Include nutrient breakdown. Default true."`
	IncludeMedia     *bool  `json:"include_media,omitempty" jsonschema:"Include food images. Default true; set false for a smaller response."`
	MealID           *int64 `json:"meal_id,omitempty" jsonschema:"Restrict to a single meal by its ID (e.g. breakfast, lunch, dinner, snacks IDs as returned in a full response)."`
}

type weightRangeInput struct {
	StartDate  string `json:"start_date" jsonschema:"Inclusive start date (user's local calendar), YYYY-MM-DD."`
	EndDate    string `json:"end_date" jsonschema:"Inclusive end date (user's local calendar), YYYY-MM-DD."`
	IncludeAll *bool  `json:"include_all,omitempty" jsonschema:"Include every weigh-in per day instead of only the latest. Default false."`
}

type bodyBatteryInput struct {
	StartDate string `json:"start_date" jsonschema:"Inclusive start date, YYYY-MM-DD."`
	EndDate   string `json:"end_date" jsonschema:"Inclusive end date, YYYY-MM-DD. At most 31 days after start_date."`
	Chunks    *int64 `json:"chunks,omitempty" jsonschema:"Number of intervals to divide each day into; the response then includes the lowest and highest Body Battery reading per interval."`
}

func rangePath(format string, in dateRangeInput, maxDays int) (string, error) {
	if err := garmin.ValidateRange("start_date", in.StartDate, "end_date", in.EndDate, maxDays); err != nil {
		return "", err
	}
	return fmt.Sprintf(format, in.StartDate, in.EndDate), nil
}

func rangeQuery(in dateRangeInput, maxDays int) (garmin.Query, error) {
	if err := garmin.ValidateRange("start_date", in.StartDate, "end_date", in.EndDate, maxDays); err != nil {
		return nil, err
	}
	return garmin.Query{}.Set("startDate", in.StartDate).Set("endDate", in.EndDate), nil
}

func registerHealth(s *mcp.Server, d *Deps) {
	// Daily overview --------------------------------------------------------
	addJSON(s, d, "get_daily_summary", "Get daily summary",
		"Get the wellness summary for one day: total steps and step goal, distance, calories (total, active, BMR), floors, intensity minutes, resting/min/max heart rate, average and max stress, Body Battery high/low and charged/drained, sleep seconds, respiration and SpO2 averages. Best single call for 'how was my day'.",
		func(ctx context.Context, d *Deps, in optionalDateInput) (json.RawMessage, error) {
			date, err := garmin.DateOrToday("date", in.Date)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, "/usersummary-service/usersummary/daily", garmin.Query{}.Set("calendarDate", date))
		})

	addJSON(s, d, "get_daily_summaries", "Get daily summaries for a range",
		"Get per-day wellness summaries (steps, calories, heart rate, stress, Body Battery, sleep, intensity minutes, etc.) for an inclusive date range of up to one year. Good for trends; use get_daily_summary for a single day.",
		func(ctx context.Context, d *Deps, in dateRangeInput) (json.RawMessage, error) {
			q, err := rangeQuery(in, 366)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, "/usersummary-service/v2/dailysummaries", q)
		})

	// Steps / stress / hydration -------------------------------------------
	addJSON(s, d, "get_daily_steps", "Get daily steps",
		"Get daily step totals, step goals and distance for an inclusive date range of up to 28 days.",
		func(ctx context.Context, d *Deps, in dateRangeInput) (json.RawMessage, error) {
			p, err := rangePath("/usersummary-service/stats/steps/daily/%s/%s", in, 28)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, p, nil)
		})

	addJSON(s, d, "get_daily_stress", "Get daily stress",
		"Get daily stress scores (overall, rest, low, medium, high stress durations) for an inclusive date range of up to 28 days.",
		func(ctx context.Context, d *Deps, in dateRangeInput) (json.RawMessage, error) {
			p, err := rangePath("/usersummary-service/stats/stress/daily/%s/%s", in, 28)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, p, nil)
		})

	addJSON(s, d, "get_daily_hydration", "Get daily hydration",
		"Get daily hydration intake, goal and sweat loss estimates for an inclusive date range of up to 31 days.",
		func(ctx context.Context, d *Deps, in dateRangeInput) (json.RawMessage, error) {
			p, err := rangePath("/usersummary-service/stats/hydration/daily/%s/%s", in, 31)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, p, nil)
		})

	// Heart ----------------------------------------------------------------
	addJSON(s, d, "get_daily_heart_rate", "Get daily heart rate",
		"Get heart rate for one day: resting, min and max heart rate plus the full time series of readings (timestamp/bpm pairs, typically every 2 minutes). The time series is large; use get_daily_summary if only resting HR is needed.",
		func(ctx context.Context, d *Deps, in optionalDateInput) (json.RawMessage, error) {
			date, err := garmin.DateOrToday("date", in.Date)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, "/wellness-service/wellness/dailyHeartRate", garmin.Query{}.Set("date", date))
		})

	addJSON(s, d, "get_heart_rate_zones", "Get heart rate zones",
		"Get the user's configured heart rate zones (zone boundaries in bpm, max HR, lactate threshold, resting HR) per sport.",
		func(ctx context.Context, d *Deps, in heartRateZonesInput) (json.RawMessage, error) {
			return d.Client.GetJSON(ctx, "/biometric-service/heartRateZones/v2", garmin.Query{}.SetBool("generateDefaults", in.GenerateDefaults))
		})

	addJSON(s, d, "get_hrv", "Get HRV for a day",
		"Get heart rate variability for one night: overnight average, weekly average, baseline range, HRV status (balanced/unbalanced/low), plus the 5-minute HRV readings during sleep.",
		func(ctx context.Context, d *Deps, in optionalDateInput) (json.RawMessage, error) {
			date, err := garmin.DateOrToday("date", in.Date)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, "/hrv-service/hrv/"+date, nil)
		})

	addJSON(s, d, "get_hrv_daily", "Get HRV summaries for a range",
		"Get nightly HRV summaries (overnight average, weekly average, baseline, status) for each day in an inclusive date range. Use for HRV trends.",
		func(ctx context.Context, d *Deps, in dateRangeInput) (json.RawMessage, error) {
			p, err := rangePath("/hrv-service/hrv/daily/%s/%s", in, 0)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, p, nil)
		})

	// Sleep ----------------------------------------------------------------
	addJSON(s, d, "get_daily_sleep", "Get sleep for a night",
		"Get detailed sleep for one night (the night ending on the given date): sleep/wake times, time in deep, light, REM and awake, sleep score and feedback, sleep-stage timeline, overnight respiration, SpO2, HRV and restlessness.",
		func(ctx context.Context, d *Deps, in optionalDateInput) (json.RawMessage, error) {
			date, err := garmin.DateOrToday("date", in.Date)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, "/sleep-service/sleep/dailySleep", garmin.Query{}.Set("date", date))
		})

	addJSON(s, d, "get_sleep_summaries", "Get sleep summaries for a range",
		"Get nightly sleep summaries (duration, stages, score, respiration, SpO2) for an inclusive date range of up to 31 days. Use for sleep trends.",
		func(ctx context.Context, d *Deps, in dateRangeInput) (json.RawMessage, error) {
			q, err := rangeQuery(in, 31)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, "/sleep-service/sleep/dailySleepSummariesByDate", q)
		})

	// Body Battery / SpO2 / Fitness Age ------------------------------------
	addJSON(s, d, "get_body_battery", "Get Body Battery",
		"Get daily Body Battery reports (charged, drained, highest, lowest and the reading timeline) for an inclusive date range of up to 31 days.",
		func(ctx context.Context, d *Deps, in bodyBatteryInput) (json.RawMessage, error) {
			q, err := rangeQuery(dateRangeInput{StartDate: in.StartDate, EndDate: in.EndDate}, 31)
			if err != nil {
				return nil, err
			}
			q.SetInt("chunks", in.Chunks)
			return d.Client.GetJSON(ctx, "/wellness-service/wellness/bodyBattery/reports/daily", q)
		})

	addJSON(s, d, "get_spo2", "Get SpO2 for a day",
		"Get blood oxygen saturation (pulse ox) for one day: average and lowest SpO2, sleep averages, acclimation data and the reading timeline.",
		func(ctx context.Context, d *Deps, in optionalDateInput) (json.RawMessage, error) {
			date, err := garmin.DateOrToday("date", in.Date)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, "/wellness-service/wellness/daily/spo2acclimation/"+date, nil)
		})

	addJSON(s, d, "get_fitness_age", "Get Fitness Age",
		"Get the latest Fitness Age estimate as of a date, with the contributing factors (chronological age, VO2max, BMI/body fat, resting HR, vigorous activity) and what would improve it.",
		func(ctx context.Context, d *Deps, in fitnessAgeInput) (json.RawMessage, error) {
			date, err := garmin.DateOrToday("date", in.Date)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, "/fitnessage-service/fitnessage/v2/"+date, garmin.Query{}.SetBool("fullHistoryScan", in.FullHistoryScan))
		})

	// Weight -----------------------------------------------------------------
	addJSON(s, d, "get_weight_entries", "Get all weight entries",
		"Get every weigh-in recorded in an inclusive date range, with weight (grams), BMI, body fat, body water, bone and muscle mass where the scale provides them, plus the source device.",
		func(ctx context.Context, d *Deps, in dateRangeInput) (json.RawMessage, error) {
			p, err := rangePath("/weight-service/weight/all/%s/%s", in, 0)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, p, nil)
		})

	addJSON(s, d, "get_weight_range", "Get weight summary for a range",
		"Get a weight summary for an inclusive date range: the latest weigh-in per day, the range average, and the nearest weigh-ins just before and after the range. Set include_all to get every entry per day.",
		func(ctx context.Context, d *Deps, in weightRangeInput) (json.RawMessage, error) {
			if err := garmin.ValidateRange("start_date", in.StartDate, "end_date", in.EndDate, 0); err != nil {
				return nil, err
			}
			p := fmt.Sprintf("/weight-service/weight/range/%s/%s", in.StartDate, in.EndDate)
			return d.Client.GetJSON(ctx, p, garmin.Query{}.SetBool("includeAll", in.IncludeAll))
		})

	addJSON(s, d, "get_weight_goals", "Get weight goals",
		"Get the user's effective weight goal(s) and goal ranges for an inclusive date range.",
		func(ctx context.Context, d *Deps, in dateRangeInput) (json.RawMessage, error) {
			p, err := rangePath("/goal-service/goal/user/effective/weightgoal/%s/%s", in, 0)
			if err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, p, nil)
		})

	// Nutrition ----------------------------------------------------------------
	addJSON(s, d, "get_nutrition_logs", "Get nutrition logs",
		"Get the food log for one day: calorie and macro goals, nutrients consumed, and the meal-by-meal breakdown of logged foods.",
		func(ctx context.Context, d *Deps, in nutritionInput) (json.RawMessage, error) {
			date, err := garmin.DateOrToday("date", in.Date)
			if err != nil {
				return nil, err
			}
			q := garmin.Query{}.
				SetBool("includeGoals", in.IncludeGoals).
				SetBool("includeNutrition", in.IncludeNutrition).
				SetBool("includeMedia", in.IncludeMedia).
				SetInt("mealId", in.MealID)
			return d.Client.GetJSON(ctx, "/nutrition-service/food/logs/"+date, q)
		})
}
