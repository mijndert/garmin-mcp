package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mijndert/garmin-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type activityIDInput struct {
	ActivityID int64 `json:"activity_id" jsonschema:"Garmin activity ID, as returned in the activityId field of search_activities results."`
}

type searchActivitiesInput struct {
	Start            *int64   `json:"start,omitempty" jsonschema:"Zero-based offset of the first activity to return, for paging. Default 0."`
	Limit            *int64   `json:"limit,omitempty" jsonschema:"Maximum number of activities to return. Default 99 (the server maximum). Use a small value such as 10 for recent-activity questions."`
	StartDate        string   `json:"start_date,omitempty" jsonschema:"Inclusive earliest local start date, YYYY-MM-DD."`
	EndDate          string   `json:"end_date,omitempty" jsonschema:"Inclusive latest local start date, YYYY-MM-DD."`
	ActivityType     string   `json:"activity_type,omitempty" jsonschema:"Parent activity type key to include, e.g. running, cycling, swimming, walking, hiking, strength_training, fitness_equipment, multi_sport, other. Must be a parent type, not a subtype."`
	ActivitySubType  string   `json:"activity_sub_type,omitempty" jsonschema:"Activity subtype key to include, e.g. trail_running, indoor_cycling, lap_swimming, treadmill_running. If activity_type is also given the subtype must belong to it."`
	EventType        string   `json:"event_type,omitempty" jsonschema:"Event classification: race, training, recreation, fitness, specialEvent or uncategorized."`
	Search           string   `json:"search,omitempty" jsonschema:"Free text matched against activity names and descriptions."`
	SortBy           string   `json:"sort_by,omitempty" jsonschema:"Field to sort by, e.g. name, distance, duration. Default startLocal (local start time)."`
	SortOrder        string   `json:"sort_order,omitempty" jsonschema:"asc or desc. Default desc (newest first)."`
	Favorite         *int64   `json:"favorite,omitempty" jsonschema:"1 = favorites only, 0 = non-favorites only, -1 or omitted = no filter."`
	ExcludeChildren  *bool    `json:"exclude_children,omitempty" jsonschema:"Exclude child activities of multisport events (e.g. the swim leg of a triathlon). Default true."`
	MinDistance      *int64   `json:"min_distance,omitempty" jsonschema:"Inclusive minimum distance in meters."`
	MaxDistance      *int64   `json:"max_distance,omitempty" jsonschema:"Inclusive maximum distance in meters."`
	MinDuration      *int64   `json:"min_duration,omitempty" jsonschema:"Inclusive minimum timer duration in seconds."`
	MaxDuration      *int64   `json:"max_duration,omitempty" jsonschema:"Inclusive maximum timer duration in seconds."`
	MinElapsed       *int64   `json:"min_elapsed_duration,omitempty" jsonschema:"Inclusive minimum elapsed duration including pauses, in seconds."`
	MaxElapsed       *int64   `json:"max_elapsed_duration,omitempty" jsonschema:"Inclusive maximum elapsed duration including pauses, in seconds."`
	MinElevation     *int64   `json:"min_elevation,omitempty" jsonschema:"Inclusive minimum total elevation gain in meters."`
	MaxElevation     *int64   `json:"max_elevation,omitempty" jsonschema:"Inclusive maximum total elevation gain in meters."`
	MinAvgPower      *int64   `json:"min_avg_power,omitempty" jsonschema:"Inclusive minimum average power in watts."`
	UpperLatitude    *float64 `json:"upper_latitude,omitempty" jsonschema:"North edge of a bounding box on start location, decimal degrees. Supply all four edges to apply."`
	LowerLatitude    *float64 `json:"lower_latitude,omitempty" jsonschema:"South edge of the bounding box, decimal degrees."`
	LeftLongitude    *float64 `json:"left_longitude,omitempty" jsonschema:"West edge of the bounding box, decimal degrees."`
	RightLongitude   *float64 `json:"right_longitude,omitempty" jsonschema:"East edge of the bounding box, decimal degrees."`
	LowerMaxDepth    *int64   `json:"lower_max_depth,omitempty" jsonschema:"Dives: inclusive lower bound on maximum depth, in centimeters."`
	UpperMaxDepth    *int64   `json:"upper_max_depth,omitempty" jsonschema:"Dives: inclusive upper bound on maximum depth, in centimeters."`
	MinBottomTime    *int64   `json:"min_bottom_time,omitempty" jsonschema:"Dives: inclusive minimum bottom time in seconds."`
	MaxBottomTime    *int64   `json:"max_bottom_time,omitempty" jsonschema:"Dives: inclusive maximum bottom time in seconds."`
	DecoDive         *bool    `json:"deco_dive,omitempty" jsonschema:"Dives: true for decompression dives only, false for non-decompression only."`
	ExcludedSubTypes []string `json:"excluded_activity_sub_types,omitempty" jsonschema:"Activity subtype keys to exclude."`
	RequiredFields   []string `json:"required_fields,omitempty" jsonschema:"Activity fields that must be non-null for an activity to be included, e.g. averageHR, avgPower."`
}

type dateRangeFromUntilInput struct {
	FromDate  string `json:"from_date" jsonschema:"Inclusive start date, YYYY-MM-DD."`
	UntilDate string `json:"until_date" jsonschema:"Inclusive end date, YYYY-MM-DD."`
}

type downloadActivityInput struct {
	ActivityID int64  `json:"activity_id" jsonschema:"Garmin activity ID from search_activities."`
	OutputDir  string `json:"output_dir,omitempty" jsonschema:"Directory to save into. Defaults to the server's configured download directory. Created if missing."`
	Extract    *bool  `json:"extract,omitempty" jsonschema:"Also unzip the archive next to it so the contained FIT/GPX/TCX file can be read directly. Default false."`
	Overwrite  *bool  `json:"overwrite,omitempty" jsonschema:"Replace files that already exist. Default false, in which case existing files are kept and reported."`
}

type downloadResult struct {
	ActivityID    int64    `json:"activity_id"`
	ZipPath       string   `json:"zip_path"`
	SizeBytes     int      `json:"size_bytes"`
	ContentType   string   `json:"content_type,omitempty"`
	AlreadyExists bool     `json:"already_existed,omitempty"`
	Extracted     []string `json:"extracted_files,omitempty"`
}

func registerActivity(s *mcp.Server, d *Deps) {
	addJSON(s, d, "search_activities", "Search activities",
		"Search and list the user's activities (runs, rides, swims, workouts, etc.) with optional filters, newest first by default. Returns an array of activity summaries including activityId, activityName, activityType, startTimeLocal, distance (m), duration (s), averageHR, calories, elevationGain and more. Use this to find activity IDs for the other activity tools. Paginate with start and limit.",
		func(ctx context.Context, d *Deps, in searchActivitiesInput) (json.RawMessage, error) {
			if in.StartDate != "" {
				if err := garmin.ValidateDate("start_date", in.StartDate); err != nil {
					return nil, err
				}
			}
			if in.EndDate != "" {
				if err := garmin.ValidateDate("end_date", in.EndDate); err != nil {
					return nil, err
				}
			}
			if in.StartDate != "" && in.EndDate != "" {
				if err := garmin.ValidateRange("start_date", in.StartDate, "end_date", in.EndDate, 0); err != nil {
					return nil, err
				}
			}
			q := garmin.Query{}.
				SetInt("start", in.Start).
				SetInt("limit", in.Limit).
				Set("startDate", in.StartDate).
				Set("endDate", in.EndDate).
				Set("activityType", in.ActivityType).
				Set("activitySubType", in.ActivitySubType).
				Set("eventType", in.EventType).
				Set("search", in.Search).
				Set("sortBy", in.SortBy).
				Set("sortOrder", in.SortOrder).
				SetInt("favorite", in.Favorite).
				SetBool("excludeChildren", in.ExcludeChildren).
				SetInt("minDistance", in.MinDistance).
				SetInt("maxDistance", in.MaxDistance).
				SetInt("minDuration", in.MinDuration).
				SetInt("maxDuration", in.MaxDuration).
				SetInt("minElapsedDuration", in.MinElapsed).
				SetInt("maxElapsedDuration", in.MaxElapsed).
				SetInt("minElevation", in.MinElevation).
				SetInt("maxElevation", in.MaxElevation).
				SetInt("minAvgPower", in.MinAvgPower).
				SetFloat("upperLatitude", in.UpperLatitude).
				SetFloat("lowerLatitude", in.LowerLatitude).
				SetFloat("leftLongitude", in.LeftLongitude).
				SetFloat("rightLongitude", in.RightLongitude).
				SetInt("lowerMaxDepth", in.LowerMaxDepth).
				SetInt("upperMaxDepth", in.UpperMaxDepth).
				SetInt("minBottomTime", in.MinBottomTime).
				SetInt("maxBottomTime", in.MaxBottomTime).
				SetBool("decoDive", in.DecoDive).
				Add("excludedActivitySubType", in.ExcludedSubTypes).
				Add("requiredField", in.RequiredFields)
			return d.Client.GetJSON(ctx, "/activitylist-service/activities/search/activities", q)
		})

	addJSON(s, d, "get_activity", "Get activity details",
		"Get the full summary of one activity: metadata, summary metrics (distance, duration, pace/speed, heart rate, power, cadence, elevation, training effect, VO2max estimate), split summaries and device info.",
		func(ctx context.Context, d *Deps, in activityIDInput) (json.RawMessage, error) {
			if in.ActivityID <= 0 {
				return nil, errors.New("activity_id must be a positive integer")
			}
			return d.Client.GetJSON(ctx, fmt.Sprintf("/activity-service/activity/%d", in.ActivityID), nil)
		})

	addJSON(s, d, "get_activity_workouts", "Get activity workouts",
		"Get the structured workout(s) performed during an activity, such as interval steps and targets. Returns an empty array if the activity did not follow a workout.",
		func(ctx context.Context, d *Deps, in activityIDInput) (json.RawMessage, error) {
			if in.ActivityID <= 0 {
				return nil, errors.New("activity_id must be a positive integer")
			}
			return d.Client.GetJSON(ctx, fmt.Sprintf("/activity-service/activity/%d/workouts", in.ActivityID), nil)
		})

	addJSON(s, d, "get_wellness_activity_summaries", "Get wellness activity summaries",
		"List wellness activity summaries (automatically detected or short activities tracked by the device outside of recorded activities) for an inclusive date range.",
		func(ctx context.Context, d *Deps, in dateRangeFromUntilInput) (json.RawMessage, error) {
			if err := garmin.ValidateRange("from_date", in.FromDate, "until_date", in.UntilDate, 0); err != nil {
				return nil, err
			}
			return d.Client.GetJSON(ctx, fmt.Sprintf("/wellnessactivity-service/activity/summary/range/%s/%s", in.FromDate, in.UntilDate), nil)
		})

	f := false
	mcp.AddTool(s, &mcp.Tool{
		Name:        "download_activity_file",
		Description: "Download the original recorded file for an activity (a ZIP archive containing the FIT file, or GPX/TCX for imported activities) and save it to local disk. Returns the saved path; set extract=true to also unzip it. Use this when per-second samples or raw GPS tracks are needed rather than the summary from get_activity.",
		Annotations: &mcp.ToolAnnotations{Title: "Download activity file", ReadOnlyHint: false, DestructiveHint: &f, IdempotentHint: true, OpenWorldHint: &f},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in downloadActivityInput) (*mcp.CallToolResult, any, error) {
		res, err := downloadActivity(ctx, d, in)
		if err != nil {
			return nil, nil, err
		}
		r, err := marshalResult(res)
		return r, nil, err
	})
}

func downloadActivity(ctx context.Context, d *Deps, in downloadActivityInput) (*downloadResult, error) {
	if in.ActivityID <= 0 {
		return nil, errors.New("activity_id must be a positive integer")
	}
	dir := in.OutputDir
	if dir == "" {
		dir = d.Config.DownloadDir
	}
	if dir == "" {
		return nil, errors.New("no output_dir given and no default download directory configured")
	}
	if strings.HasPrefix(dir, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(dir, "~"), "/"))
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	overwrite := in.Overwrite != nil && *in.Overwrite
	extract := in.Extract != nil && *in.Extract

	zipPath := filepath.Join(dir, fmt.Sprintf("activity_%d.zip", in.ActivityID))
	res := &downloadResult{ActivityID: in.ActivityID, ZipPath: zipPath}

	var data []byte
	if st, err := os.Stat(zipPath); err == nil && !overwrite {
		res.AlreadyExists = true
		res.SizeBytes = int(st.Size())
		if extract {
			data, err = os.ReadFile(zipPath)
			if err != nil {
				return nil, err
			}
		}
	} else {
		var ct string
		var err error
		data, _, ct, err = d.Client.Download(ctx, fmt.Sprintf("/download-service/files/activity/%d", in.ActivityID))
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			return nil, errors.New("the activity file response was empty")
		}
		if err := os.WriteFile(zipPath, data, 0o644); err != nil {
			return nil, fmt.Errorf("save activity file: %w", err)
		}
		res.SizeBytes = len(data)
		res.ContentType = ct
	}

	if extract {
		files, err := extractZip(data, dir, overwrite)
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", zipPath, err)
		}
		res.Extracted = files
	}
	return res, nil
}

// extractZip unpacks the archive into dir, flattening paths and refusing
// anything that would escape the directory. Returns written (or kept) paths.
func extractZip(data []byte, dir string, overwrite bool) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a ZIP archive: %w", err)
	}
	const maxFile = 512 << 20
	var out []string
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(filepath.Clean("/" + f.Name))
		if name == "" || name == "." || name == "/" {
			continue
		}
		dst := filepath.Join(dir, name)
		if _, err := os.Stat(dst); err == nil && !overwrite {
			out = append(out, dst)
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return out, err
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxFile+1))
		rc.Close()
		if err != nil {
			return out, err
		}
		if len(content) > maxFile {
			return out, fmt.Errorf("%s exceeds the %d byte extraction limit", name, maxFile)
		}
		if err := os.WriteFile(dst, content, 0o644); err != nil {
			return out, err
		}
		out = append(out, dst)
	}
	return out, nil
}
