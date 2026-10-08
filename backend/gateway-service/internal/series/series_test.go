package series

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakePrometheus(t *testing.T, body string, hits *atomic.Int32, seen *[]string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)

		if seen != nil {
			*seen = append(*seen, r.URL.Query().Get("query")+"|"+r.URL.Query().Get("step"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return server
}

const matrix = `{"status":"success","data":{"resultType":"matrix","result":[
  {"metric":{"trigger":"user"},"values":[[1700000000,"3"],[1700000600,"NaN"],[1700001200,"5"]]},
  {"metric":{"trigger":"batch"},"values":[[1700000000,"1"]]},
  {"metric":{"trigger":"empty"},"values":[[1700000000,"+Inf"]]}
]}}`

func TestQueryMapsLabelsDropsNaNAndSortsLines(t *testing.T) {
	var hits atomic.Int32

	server := fakePrometheus(t, matrix, &hits, nil)
	client := NewClient(server.URL, time.Minute, nil)

	got, err := client.Query(context.Background(), "gradings_by_trigger", false, "24h")
	if err != nil {
		t.Fatal(err)
	}

	if got.Name != "gradings_by_trigger" || got.StepSeconds != 600 || got.Unit != "count" {
		t.Fatalf("result = %+v", got)
	}

	// "empty" had only an infinite value, so it has no curve; the others are sorted by label.
	if len(got.Series) != 2 || got.Series[0].Label != "batch" || got.Series[1].Label != "user" {
		t.Fatalf("series = %+v", got.Series)
	}

	if len(got.Series[1].Points) != 2 || got.Series[1].Points[1] != [2]float64{1700001200, 5} {
		t.Fatalf("user points = %v: NaN must be dropped, not turned into 0", got.Series[1].Points)
	}
}

func TestAnswersAreReusedForTheTTLPerSeriesAndRange(t *testing.T) {
	var hits atomic.Int32

	server := fakePrometheus(t, matrix, &hits, nil)
	client := NewClient(server.URL, time.Minute, nil)

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }

	for range 3 {
		if _, err := client.Query(context.Background(), "llm_requests", false, "24h"); err != nil {
			t.Fatal(err)
		}
	}

	if hits.Load() != 1 {
		t.Fatalf("%d queries for 3 identical requests", hits.Load())
	}

	_, _ = client.Query(context.Background(), "llm_requests", false, "7d")
	_, _ = client.Query(context.Background(), "batch_items", false, "24h")

	if hits.Load() != 3 {
		t.Fatalf("a different range or series must be queried separately, hits = %d", hits.Load())
	}

	now = now.Add(2 * time.Minute)
	_, _ = client.Query(context.Background(), "llm_requests", false, "24h")

	if hits.Load() != 4 {
		t.Fatalf("an expired answer must be fetched again, hits = %d", hits.Load())
	}
}

func TestOnlyPublicSeriesAreReadableByEverybodyAndTheyUseTheirOwnRange(t *testing.T) {
	var (
		hits atomic.Int32
		seen []string
	)

	server := fakePrometheus(t, matrix, &hits, &seen)
	client := NewClient(server.URL, time.Minute, nil)

	if _, err := client.Query(context.Background(), "admin_denied", true, ""); !errors.Is(err, ErrUnknown) {
		t.Fatalf("an admin series asked as public: %v", err)
	}

	if _, err := client.Query(context.Background(), "nope", false, ""); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown series: %v", err)
	}

	if hits.Load() != 0 {
		t.Fatal("a refused series must never reach Prometheus")
	}

	// The caller's range is ignored for a public series: it is always the 14-day, 1-day-step view.
	if _, err := client.Query(context.Background(), "gradings_per_day", true, "1h"); err != nil {
		t.Fatal(err)
	}

	if len(seen) != 1 || !strings.HasSuffix(seen[0], "|86400") || !strings.Contains(seen[0], `visibility="public"`) {
		t.Fatalf("query = %v", seen)
	}
}

func TestEveryPublicQueryIsLimitedToPublicGradings(t *testing.T) {
	for _, def := range Catalog {
		if !def.Public {
			continue
		}

		if !strings.Contains(def.Query, `visibility="public"`) {
			t.Errorf("%s: a public series must filter on visibility=\"public\"", def.Name)
		}

		if strings.Contains(def.Query, "private") {
			t.Errorf("%s mentions private", def.Name)
		}
	}
}

func TestBadRangeAndOffAndUnavailable(t *testing.T) {
	var hits atomic.Int32

	server := fakePrometheus(t, matrix, &hits, nil)
	client := NewClient(server.URL, time.Minute, nil)

	if _, err := client.Query(context.Background(), "llm_requests", false, "1y"); !errors.Is(err, ErrBadRange) {
		t.Fatalf("bad range: %v", err)
	}

	if _, err := NewClient("", time.Minute, nil).Query(context.Background(), "llm_requests", false, ""); !errors.Is(err, ErrOff) {
		t.Fatalf("off: %v", err)
	}

	var none *Client
	if _, err := none.Query(context.Background(), "llm_requests", false, ""); !errors.Is(err, ErrOff) {
		t.Fatalf("nil client: %v", err)
	}

	down := httptest.NewServer(http.NotFoundHandler())
	url := down.URL
	down.Close()

	if _, err := NewClient(url, time.Minute, nil).Query(context.Background(), "llm_requests", false, ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unreachable: %v", err)
	}

	broken := fakePrometheus(t, `{"status":"error"}`, &hits, nil)
	if _, err := NewClient(broken.URL, time.Minute, nil).Query(context.Background(), "llm_requests", false, ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error answer: %v", err)
	}

	// A failed query is not cached.
	if _, err := NewClient(broken.URL, time.Minute, nil).Query(context.Background(), "llm_requests", false, ""); err == nil {
		t.Fatal("still an error")
	}
}

func TestCatalogNamesAreUniqueAndQueriesAreOurOwnMetrics(t *testing.T) {
	seen := map[string]bool{}

	for _, def := range Catalog {
		if seen[def.Name] {
			t.Errorf("duplicate series %s", def.Name)
		}

		seen[def.Name] = true

		if !strings.Contains(def.Query, "gateway_") && !strings.Contains(def.Query, "aggregator_") &&
			!strings.Contains(def.Query, "rating_") && !strings.Contains(def.Query, "llm_model_") {
			t.Errorf("%s reads no metric of ours: %s", def.Name, def.Query)
		}

		if def.Range != "" {
			if _, ok := Ranges[def.Range]; !ok {
				t.Errorf("%s has unknown range %s", def.Name, def.Range)
			}
		}
	}
}

func TestEveryGraphEndsNowSoTodayIsVisible(t *testing.T) {
	var (
		hits atomic.Int32
		seen []string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		seen = append(seen, r.URL.Query().Get("end")+"|"+r.URL.Query().Get("start"))
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 0, nil)
	client.now = func() time.Time { return time.Date(2026, 10, 8, 18, 26, 10, 0, time.UTC) }

	if _, err := client.Query(context.Background(), "gradings_per_day", true, ""); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 8, 18, 26, 10, 0, time.UTC).Unix()
	want := strconv.FormatInt(now, 10) + "|" + strconv.FormatInt(now-14*86400, 10)

	if len(seen) != 1 || seen[0] != want {
		t.Fatalf("window = %v, want %s: a daily graph must end now, not at midnight", seen, want)
	}
}

func TestCountQueriesAreRoundedBecauseIncreaseExtrapolates(t *testing.T) {
	for _, def := range Catalog {
		if (def.Unit == "count" || def.Unit == "tokens") && !strings.HasPrefix(def.Query, "round(") {
			t.Errorf("%s counts events but its query is not rounded: %s", def.Name, def.Query)
		}
	}
}

func TestLLMSeriesAreAdminOnlyAndNeverCarryPricesOrRepositories(t *testing.T) {
	llm := 0

	for _, def := range Catalog {
		if !strings.HasPrefix(def.Name, "llm_") || !strings.Contains(def.Query, "llm_model_") {
			continue
		}

		llm++

		if def.Public {
			t.Errorf("%s: model usage is for admins only", def.Name)
		}

		// A repository, user or price would have to be a label or a factor in the query.
		for _, banned := range []string{"repo", "user", "slug", "price", " * ", "cost"} {
			if strings.Contains(def.Query, banned) {
				t.Errorf("%s: query mentions %q: %s", def.Name, banned, def.Query)
			}
		}
	}

	if llm != 5 {
		t.Fatalf("expected 5 LLM series, found %d", llm)
	}

	if _, ok := Lookup("llm_tokens_per_day", true); ok {
		t.Fatal("a public caller must not reach an LLM series")
	}

	// The daily token graph has its own fixed 14-day window, whatever range the caller asks for.
	var seen []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query().Get("step"))
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer server.Close()

	if _, err := NewClient(server.URL, 0, nil).Query(context.Background(), "llm_tokens_per_day", false, "1h"); err != nil {
		t.Fatal(err)
	}

	if len(seen) != 1 || seen[0] != "86400" {
		t.Fatalf("step = %v, want one daily step", seen)
	}
}
