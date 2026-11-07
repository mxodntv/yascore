package weather

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestForecastSpeaksTemperatureWords(t *testing.T) {
	cases := []struct {
		temp float64
		want string
	}{
		{-1, "В городе Киров сейчас минус один и туман"},
		{0, "В городе Киров сейчас ноль и туман"},
		{2, "В городе Киров сейчас два и туман"},
		{21, "В городе Киров сейчас двадцать один и туман"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%g", tc.temp), func(t *testing.T) {
			geo := &recorder{lat: 58.6, lon: 49.66, country: "Россия", name: "Киров"}
			api := &recorder{forecast: true, temp: tc.temp, code: 45}
			geoSrv := httptest.NewServer(geo.handler("search"))
			apiSrv := httptest.NewServer(api.handler("forecast"))
			defer geoSrv.Close()
			defer apiSrv.Close()

			skill := mustSkill(t, filepath.Join(t.TempDir(), "v.db"), geoSrv.URL, apiSrv.URL, 8*time.Second)
			defer skill.Close()

			got, err := skill.Ask(context.Background(), "Киров", "Россия")
			if err != nil {
				t.Fatal(err)
			}
			assertSpoken(t, got, tc.want, 1)
		})
	}
}

func TestNoPlaceAsksAndSkipsForecast(t *testing.T) {
	geo := &recorder{}
	api := &recorder{}
	geoSrv := httptest.NewServer(geo.handler("search"))
	apiSrv := httptest.NewServer(api.handler("forecast"))
	defer geoSrv.Close()
	defer apiSrv.Close()

	dbPath := filepath.Join(t.TempDir(), "nested", "v.db")
	skill := mustSkill(t, dbPath, geoSrv.URL, apiSrv.URL, 8*time.Second)
	defer skill.Close()

	got, err := skill.Handle(context.Background(), "weather", `{"city":"","country":""}`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "В какой стране и в каком городе смотреть погоду?" {
		t.Fatalf("question %q", got)
	}
	if geo.n() != 0 || api.n() != 0 || strings.Contains(api.lastURL, "forecast") {
		t.Fatalf("api called geo=%d forecast=%d url=%s", geo.n(), api.n(), api.lastURL)
	}
	assertStoreFile(t, dbPath)
}

func TestSavePlaceThenEmptyCityUsesCoordinates(t *testing.T) {
	geo := &recorder{lat: 55.79, lon: 49.12, country: "Россия", name: "Казань"}
	api := &recorder{}
	geoSrv := httptest.NewServer(geo.handler("search"))
	apiSrv := httptest.NewServer(api.handler("forecast"))
	defer geoSrv.Close()
	defer apiSrv.Close()

	skill := mustSkill(t, filepath.Join(t.TempDir(), "v.db"), geoSrv.URL, apiSrv.URL, 8*time.Second)
	defer skill.Close()
	ctx := context.Background()

	got, err := skill.Ask(ctx, "Казань", "Россия")
	if err != nil {
		t.Fatal(err)
	}
	assertSpoken(t, got, "В городе Казань сейчас два и пасмурно", 1)
	if geo.n() != 1 || api.n() != 1 {
		t.Fatalf("calls geo=%d forecast=%d", geo.n(), api.n())
	}
	assertCoords(t, api.lastQuery(), 55.79, 49.12)
	if geo.lastQuery().Get("name") != "Казань" {
		t.Fatalf("geocode name %q", geo.lastQuery().Get("name"))
	}
	assertWeatherDoc(t, skill, "Россия", "Казань", 55.79, 49.12)

	got, err = skill.Ask(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "какой стране") {
		t.Fatalf("asked again: %s", got)
	}
	assertSpoken(t, got, "В городе Казань сейчас два и пасмурно", 2)
	if geo.n() != 1 || api.n() != 2 {
		t.Fatalf("second calls geo=%d forecast=%d", geo.n(), api.n())
	}
	assertCoords(t, api.lastQuery(), 55.79, 49.12)
}

func TestSecondCallUsesDatabase(t *testing.T) {
	geo := &recorder{lat: 55.75, lon: 37.62, country: "Россия", name: "Москва"}
	api := &recorder{}
	geoSrv := httptest.NewServer(geo.handler("search"))
	apiSrv := httptest.NewServer(api.handler("forecast"))
	defer geoSrv.Close()
	defer apiSrv.Close()

	db := filepath.Join(t.TempDir(), "nested", "variacore.db")
	first := mustSkill(t, db, geoSrv.URL, apiSrv.URL, 8*time.Second)
	if _, err := first.Ask(context.Background(), "Москва", "Россия"); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	geoBefore := geo.n()

	second := mustSkill(t, db, geoSrv.URL, apiSrv.URL, 8*time.Second)
	defer second.Close()
	got, err := second.Ask(context.Background(), "  ", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "какой стране") {
		t.Fatalf("asked again: %s", got)
	}
	assertSpoken(t, got, "В городе Москва сейчас два и пасмурно", 2)
	if geo.n() != geoBefore {
		t.Fatalf("geocoded again: %d -> %d", geoBefore, geo.n())
	}
	assertCoords(t, api.lastQuery(), 55.75, 37.62)
}

func TestExplicitCityDoesNotReplaceSavedPlace(t *testing.T) {
	geo := &recorder{byName: map[string][2]float64{
		"Москва": {55.75, 37.62},
		"Казань": {55.79, 49.12},
	}, country: "Россия"}
	api := &recorder{}
	geoSrv := httptest.NewServer(geo.handler("search"))
	apiSrv := httptest.NewServer(api.handler("forecast"))
	defer geoSrv.Close()
	defer apiSrv.Close()

	skill := mustSkill(t, filepath.Join(t.TempDir(), "v.db"), geoSrv.URL, apiSrv.URL, 8*time.Second)
	defer skill.Close()
	ctx := context.Background()
	if _, err := skill.Ask(ctx, "Москва", "Россия"); err != nil {
		t.Fatal(err)
	}
	got, err := skill.Ask(ctx, "Казань", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Казань") {
		t.Fatalf("one-off %q", got)
	}
	assertCoords(t, api.lastQuery(), 55.79, 49.12)

	saved, ok, err := skill.store.Load(ctx)
	if err != nil || !ok {
		t.Fatalf("saved %+v %v", saved, err)
	}
	if saved.City != "Москва" || math.Abs(saved.Latitude-55.75) > 0.001 {
		t.Fatalf("default changed: %+v", saved)
	}
	if _, err := skill.Ask(ctx, "", ""); err != nil {
		t.Fatal(err)
	}
	assertCoords(t, api.lastQuery(), 55.75, 37.62)
}

func TestAPIErrorIsRussian(t *testing.T) {
	geo := &recorder{lat: 55.79, lon: 49.12, country: "Россия", name: "Казань"}
	api := &recorder{status: http.StatusBadGateway}
	geoSrv := httptest.NewServer(geo.handler("search"))
	apiSrv := httptest.NewServer(api.handler("forecast"))
	defer geoSrv.Close()
	defer apiSrv.Close()

	skill := mustSkill(t, filepath.Join(t.TempDir(), "v.db"), geoSrv.URL, apiSrv.URL, 8*time.Second)
	defer skill.Close()

	got, err := skill.Ask(context.Background(), "Казань", "Россия")
	if err != nil {
		t.Fatal(err)
	}
	if got != failPhrase {
		t.Fatalf("failure %q", got)
	}
	if strings.Contains(got, "http") || strings.Contains(got, "://") {
		t.Fatalf("leak %q", got)
	}
}

func TestClientUsesConfigTimeoutAndBaseURL(t *testing.T) {
	geo := &recorder{lat: 55.79, lon: 49.12, country: "Россия", name: "Казань"}
	api := &recorder{}
	geoSrv := httptest.NewServer(geo.handler("search"))
	apiSrv := httptest.NewServer(api.handler("forecast"))
	defer geoSrv.Close()
	defer apiSrv.Close()

	timeout := 8 * time.Second
	skill := mustSkill(t, filepath.Join(t.TempDir(), "v.db"), geoSrv.URL, apiSrv.URL, timeout)
	defer skill.Close()
	om, ok := skill.provider.(*openMeteo)
	if !ok {
		t.Fatalf("provider %T", skill.provider)
	}
	if om.client.Timeout != timeout {
		t.Fatalf("timeout %s", om.client.Timeout)
	}
	if om.apiBase != apiSrv.URL || om.geocodeBase != geoSrv.URL {
		t.Fatalf("bases api %s geo %s", om.apiBase, om.geocodeBase)
	}
	if _, err := skill.Ask(context.Background(), "Казань", "Россия"); err != nil {
		t.Fatal(err)
	}
	if geo.n() != 1 || api.n() != 1 {
		t.Fatalf("calls geo=%d forecast=%d", geo.n(), api.n())
	}
	if !strings.HasPrefix(geo.lastURL, geoSrv.URL+"/v1/search") {
		t.Fatalf("geocode url %s", geo.lastURL)
	}
	if !strings.HasPrefix(api.lastURL, apiSrv.URL+"/v1/forecast") {
		t.Fatalf("forecast url %s", api.lastURL)
	}

	slow := &recorder{status: http.StatusOK, delay: 3 * time.Second, lat: 1, lon: 2, country: "Россия", name: "Казань"}
	slowSrv := httptest.NewServer(slow.handler("forecast"))
	defer slowSrv.Close()
	short := mustSkill(t, filepath.Join(t.TempDir(), "slow.db"), geoSrv.URL, slowSrv.URL, 200*time.Millisecond)
	defer short.Close()
	if got := short.provider.(*openMeteo).client.Timeout; got != 200*time.Millisecond {
		t.Fatalf("short timeout %s", got)
	}
	start := time.Now()
	got, err := short.Ask(context.Background(), "Казань", "Россия")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if got != failPhrase {
		t.Fatalf("timeout reply %q", got)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("timeout not applied, took %s", elapsed)
	}
	if slow.n() == 0 {
		t.Fatal("slow forecast was not called")
	}
}

func TestLegacyPlaceCopiedOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "variacore.db")
	db, err := sql.Open("sqlite", "file://"+path+"?mode=rwc&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE place (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		country TEXT NOT NULL,
		city TEXT NOT NULL,
		latitude REAL NOT NULL,
		longitude REAL NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO place (id, country, city, latitude, longitude, updated_at)
		VALUES (1, 'Россия', 'Киров', 58.6, 49.66, '2026-11-06T16:31:28Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	geo := &recorder{}
	api := &recorder{forecast: true, temp: -1, code: 45}
	geoSrv := httptest.NewServer(geo.handler("search"))
	apiSrv := httptest.NewServer(api.handler("forecast"))
	defer geoSrv.Close()
	defer apiSrv.Close()

	skill := mustSkill(t, path, geoSrv.URL, apiSrv.URL, 8*time.Second)
	defer skill.Close()
	got, err := skill.Ask(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	assertSpoken(t, got, "В городе Киров сейчас минус один и туман", 1)
	if geo.n() != 0 {
		t.Fatalf("geocoded legacy place")
	}
	assertCoords(t, api.lastQuery(), 58.6, 49.66)
	assertWeatherDoc(t, skill, "Россия", "Киров", 58.6, 49.66)

	if err := skill.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", "file://"+path+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var city string
	if err := db.QueryRow(`SELECT city FROM place WHERE id = 1`).Scan(&city); err != nil {
		t.Fatal(err)
	}
	if city != "Киров" {
		t.Fatalf("place table changed to %s", city)
	}
}

func mustSkill(t *testing.T, db, geo, api string, timeout time.Duration) *Skill {
	t.Helper()
	skill, err := New(Settings{
		Provider:    "open-meteo",
		APIBase:     api,
		GeocodeBase: geo,
		Database:    db,
		Timeout:     timeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	return skill
}

func assertWeatherDoc(t *testing.T, skill *Skill, country, city string, lat, lon float64) {
	t.Helper()
	raw, ok, err := skill.store.docs.Read(placeFile)
	if err != nil || !ok {
		t.Fatalf("document ok=%v err=%v", ok, err)
	}
	var doc placeDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Country != country || doc.City != city || math.Abs(doc.Latitude-lat) > 0.001 || math.Abs(doc.Longitude-lon) > 0.001 {
		t.Fatalf("document %+v", doc)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for k := range keys {
		switch k {
		case "country", "city", "latitude", "longitude", "asks_day", "asks_count":
		default:
			t.Fatalf("document key %s in %s", k, raw)
		}
	}
	if _, ok := keys["asks_day"]; !ok {
		t.Fatalf("missing asks_day %s", raw)
	}
	if _, ok := keys["asks_count"]; !ok {
		t.Fatalf("missing asks_count %s", raw)
	}
}

func assertSpoken(t *testing.T, got, sentence string, n int) {
	t.Helper()
	want := sentence + "\nasks_today: " + strconv.Itoa(n)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func assertStoreFile(t *testing.T, path string) {
	t.Helper()
	skillDir := path
	if strings.EqualFold(filepath.Ext(path), ".db") {
		skillDir = filepath.Dir(path)
	}
	file := filepath.Join(skillDir, "data", "place.json")
	raw, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		t.Fatalf("store file %s", raw)
	}
}

func assertCoords(t *testing.T, q url.Values, lat, lon float64) {
	t.Helper()
	gotLat, errLat := strconv.ParseFloat(q.Get("latitude"), 64)
	gotLon, errLon := strconv.ParseFloat(q.Get("longitude"), 64)
	if errLat != nil || errLon != nil || math.Abs(gotLat-lat) > 0.001 || math.Abs(gotLon-lon) > 0.001 {
		t.Fatalf("coords lat=%q lon=%q", q.Get("latitude"), q.Get("longitude"))
	}
}

type recorder struct {
	mu       sync.Mutex
	hits     int
	last     url.Values
	lastURL  string
	status   int
	delay    time.Duration
	forecast bool
	temp     float64
	code     int
	lat      float64
	lon      float64
	country  string
	name     string
	byName   map[string][2]float64
}

func (r *recorder) n() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits
}

func (r *recorder) lastQuery() url.Values {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

func (r *recorder) handler(kind string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.hits++
		r.last = req.URL.Query()
		full := *req.URL
		full.Scheme = "http"
		full.Host = req.Host
		r.lastURL = full.String()
		status := r.status
		delay := r.delay
		forecast := r.forecast
		temp := r.temp
		code := r.code
		lat, lon := r.lat, r.lon
		name := r.name
		if name == "" {
			name = req.URL.Query().Get("name")
		}
		if r.byName != nil {
			if pair, ok := r.byName[req.URL.Query().Get("name")]; ok {
				lat, lon = pair[0], pair[1]
			}
		}
		country := r.country
		r.mu.Unlock()

		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-req.Context().Done():
				timer.Stop()
				return
			}
		}

		if !strings.Contains(req.URL.Path, "/v1/"+kind) && kind != "" {
			http.NotFound(w, req)
			return
		}
		if status == 0 {
			status = http.StatusOK
		}
		if status >= 400 {
			http.Error(w, "fail", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if kind == "search" {
			fmt.Fprintf(w, `{"results":[{"name":%q,"country":%q,"country_code":"RU","latitude":%g,"longitude":%g}]}`, name, country, lat, lon)
			return
		}
		if forecast {
			fmt.Fprintf(w, `{"current":{"temperature_2m":%g,"weather_code":%d,"precipitation":0}}`, temp, code)
			return
		}
		_, _ = w.Write([]byte(`{"current":{"temperature_2m":2.4,"weather_code":3,"precipitation":0}}`))
	})
}
