// Package series reads the graphs of the admin and Analytics tabs from Prometheus (docs/metrics.md). The
// browser never sends PromQL: it names one of the fixed series below and one of the fixed ranges, so it
// cannot read other metrics or start an expensive query.
package series

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrOff: no Prometheus is configured (PROMETHEUS_URL is empty).
	ErrOff = errors.New("metrics graphs are off")
	// ErrUnavailable: Prometheus did not answer or answered with an error.
	ErrUnavailable = errors.New("metrics store unavailable")
	// ErrUnknown: no such series, or not one the caller may read.
	ErrUnknown = errors.New("unknown series")
	// ErrBadRange: not one of the fixed ranges.
	ErrBadRange = errors.New("unknown range")
)

// Definition is one named series. Query uses only metrics of this project. Public series are the
// ones the Analytics tab shows to everybody: they count public gradings only.
type Definition struct {
	Name   string
	Public bool
	Unit   string
	Query  string
	// Range, when set, fixes the range of a public series (it ignores the caller's choice).
	Range string
}

// Range is a window and the step between points.
type Range struct {
	Window time.Duration
	Step   time.Duration
}

// Ranges are the windows a caller may choose for an admin series.
var Ranges = map[string]Range{
	"1h":  {Window: time.Hour, Step: 30 * time.Second},
	"6h":  {Window: 6 * time.Hour, Step: 3 * time.Minute},
	"24h": {Window: 24 * time.Hour, Step: 10 * time.Minute},
	"7d":  {Window: 7 * 24 * time.Hour, Step: time.Hour},
	// The public graphs show days; metrics are kept 15 days by default, so 14 days is the longest window.
	"14d": {Window: 14 * 24 * time.Hour, Step: 24 * time.Hour},
}

// DefaultRange is used when the caller names none.
const DefaultRange = "24h"

// Catalog lists every series. Names are fixed words the frontend knows.
var Catalog = []Definition{
	// Public: public gradings only, never a private count.
	{
		Name: "gradings_per_day", Public: true, Unit: "count", Range: "14d",
		Query: `round(sum(increase(aggregator_gradings_completed_total{visibility="public",outcome="ok"}[1d])))`,
	},
	{
		Name: "grading_avg_seconds", Public: true, Unit: "seconds", Range: "14d",
		Query: `sum(increase(aggregator_grading_duration_seconds_sum{visibility="public",outcome="ok"}[1d]))` +
			` / sum(increase(aggregator_grading_duration_seconds_count{visibility="public",outcome="ok"}[1d]))`,
	},

	// Admin only.
	{
		Name: "http_rate", Unit: "per_second",
		Query: `sum by (class) (label_replace(rate(gateway_http_requests_total[5m]), "class", "${1}xx", "status", "(\\d).*"))`,
	},
	{
		Name: "http_latency_p95", Unit: "seconds",
		Query: `histogram_quantile(0.95, sum by (le) (rate(gateway_http_request_duration_seconds_bucket[5m])))`,
	},
	{
		Name: "grpc_errors", Unit: "per_second",
		Query: `sum by (target) (rate(gateway_grpc_client_calls_total{code!="OK"}[5m]))`,
	},
	{
		Name: "gradings_by_trigger", Unit: "count",
		Query: `round(sum by (trigger) (increase(aggregator_gradings_requested_total[1h])))`,
	},
	{
		Name: "grading_outcomes", Unit: "count",
		Query: `round(sum by (outcome) (increase(aggregator_gradings_completed_total[1h])))`,
	},
	{
		Name: "grading_p95_seconds", Unit: "seconds",
		Query: `histogram_quantile(0.95, sum by (le) (rate(aggregator_grading_duration_seconds_bucket[30m])))`,
	},
	{
		Name: "limiter_rejections", Unit: "count",
		Query: `round(sum(increase(aggregator_grade_limiter_rejections_total[1h])))`,
	},
	{
		Name: "llm_requests", Unit: "count",
		Query: `round(sum by (outcome) (increase(aggregator_llm_requests_total[1h])))`,
	},
	{
		Name: "batch_items", Unit: "count",
		Query: `round(sum by (status) (increase(aggregator_regrade_batch_items_total[1h])))`,
	},
	{
		Name: "admin_denied", Unit: "count",
		Query: `round(sum by (reason) (increase(gateway_admin_denied_total[1h])))`,
	},
}

// Lookup finds a series; public asks for the ones everybody may read.
func Lookup(name string, public bool) (Definition, bool) {
	for _, def := range Catalog {
		if def.Name == name && (def.Public || !public) {
			return def, true
		}
	}

	return Definition{}, false
}

// Line is one labelled curve; Points are [unix seconds, value].
type Line struct {
	Label  string       `json:"label"`
	Points [][2]float64 `json:"points"`
}

// Result is the answer for one series.
type Result struct {
	Name        string    `json:"name"`
	Unit        string    `json:"unit"`
	StepSeconds int       `json:"step_seconds"`
	GeneratedAt time.Time `json:"generated_at"`
	Series      []Line    `json:"series"`
}

// Client queries Prometheus and reuses an answer for the cache TTL.
type Client struct {
	base  string
	http  *http.Client
	ttl   time.Duration
	now   func() time.Time
	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	result  Result
	expires time.Time
}

// NewClient returns a client for the Prometheus at baseURL; an empty baseURL makes every query ErrOff.
func NewClient(baseURL string, ttl time.Duration, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &Client{
		base:  strings.TrimRight(baseURL, "/"),
		http:  httpClient,
		ttl:   ttl,
		now:   time.Now,
		cache: map[string]cached{},
	}
}

// Query answers one series for a range. A public series always uses its own fixed range.
func (c *Client) Query(ctx context.Context, name string, public bool, rangeName string) (Result, error) {
	if c == nil || c.base == "" {
		return Result{}, ErrOff
	}

	def, ok := Lookup(name, public)
	if !ok {
		return Result{}, ErrUnknown
	}

	if def.Range != "" {
		rangeName = def.Range
	}

	if rangeName == "" {
		rangeName = DefaultRange
	}

	window, ok := Ranges[rangeName]
	if !ok {
		return Result{}, ErrBadRange
	}

	key := name + "|" + rangeName

	c.mu.Lock()
	if hit, found := c.cache[key]; found && c.now().Before(hit.expires) {
		c.mu.Unlock()

		return hit.result, nil
	}
	c.mu.Unlock()

	result, err := c.fetch(ctx, def, window)
	if err != nil {
		return Result{}, err
	}

	c.mu.Lock()
	c.cache[key] = cached{result: result, expires: c.now().Add(c.ttl)}
	c.mu.Unlock()

	return result, nil
}

type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][2]any          `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func (c *Client) fetch(ctx context.Context, def Definition, window Range) (Result, error) {
	// Every graph ends now. A daily graph's points are the 24 hours up to each moment (the last one is
	// "the last 24 hours"), so today's activity shows at once instead of after the next midnight.
	end := c.now()

	query := url.Values{}
	query.Set("query", def.Query)
	query.Set("start", strconv.FormatInt(end.Add(-window.Window).Unix(), 10))
	query.Set("end", strconv.FormatInt(end.Unix(), 10))
	query.Set("step", strconv.Itoa(int(window.Step.Seconds())))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/query_range?"+query.Encode(), nil)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}

	var parsed promResponse
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Status != "success" {
		return Result{}, fmt.Errorf("%w: unreadable answer", ErrUnavailable)
	}

	result := Result{Name: def.Name, Unit: def.Unit, StepSeconds: int(window.Step.Seconds()), GeneratedAt: c.now().UTC(), Series: []Line{}}

	for _, item := range parsed.Data.Result {
		line := Line{Label: labelOf(item.Metric), Points: [][2]float64{}}

		for _, pair := range item.Values {
			at, okAt := pair[0].(float64)
			text, okText := pair[1].(string)

			if !okAt || !okText {
				continue
			}

			value, err := strconv.ParseFloat(text, 64)
			// A division by zero or a gap is NaN: no point, rather than a made-up zero.
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}

			line.Points = append(line.Points, [2]float64{at, value})
		}

		if len(line.Points) > 0 {
			result.Series = append(result.Series, line)
		}
	}

	sort.Slice(result.Series, func(i, j int) bool { return result.Series[i].Label < result.Series[j].Label })

	return result, nil
}

// labelOf names a curve by its label values, or "" when the query grouped by nothing.
func labelOf(metric map[string]string) string {
	keys := make([]string, 0, len(metric))
	for key := range metric {
		if key != "__name__" {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)

	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, metric[key])
	}

	return strings.Join(values, ", ")
}
