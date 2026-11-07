package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

var (
	errNotFound = errors.New("place not found")
	errProvider = errors.New("weather provider is not configured")
)

func newProvider(s Settings) Provider {
	switch strings.ToLower(strings.TrimSpace(s.Provider)) {
	case "open-meteo":
		return newOpenMeteo(s)
	default:
		return unknownProvider{name: s.Provider}
	}
}

type unknownProvider struct{ name string }

func (unknownProvider) Geocode(context.Context, string, string) (Place, error) {
	return Place{}, errProvider
}

func (unknownProvider) Current(context.Context, Place) (Observation, error) {
	return Observation{}, errProvider
}

type openMeteo struct {
	apiBase     string
	geocodeBase string
	client      *http.Client
}

func newOpenMeteo(s Settings) *openMeteo {
	return &openMeteo{
		apiBase:     s.APIBase,
		geocodeBase: s.GeocodeBase,
		client:      &http.Client{Timeout: s.Timeout},
	}
}

func (o *openMeteo) Geocode(ctx context.Context, city, country string) (Place, error) {
	q := url.Values{}
	q.Set("name", city)
	q.Set("count", "5")
	q.Set("language", "ru")
	q.Set("format", "json")
	rawURL, err := withQuery(o.geocodeBase, "/v1/search", q)
	if err != nil {
		return Place{}, err
	}
	var body geoResponse
	if err := o.getJSON(ctx, rawURL, &body); err != nil {
		return Place{}, err
	}
	picked, ok := pickPlace(body.Results, country)
	if !ok {
		return Place{}, errNotFound
	}
	return picked, nil
}

func (o *openMeteo) Current(ctx context.Context, place Place) (Observation, error) {
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(place.Latitude, 'f', -1, 64))
	q.Set("longitude", strconv.FormatFloat(place.Longitude, 'f', -1, 64))
	q.Set("current", "temperature_2m,weather_code,precipitation")
	q.Set("temperature_unit", "celsius")
	q.Set("timezone", "auto")
	rawURL, err := withQuery(o.apiBase, "/v1/forecast", q)
	if err != nil {
		return Observation{}, err
	}
	var body forecastResponse
	if err := o.getJSON(ctx, rawURL, &body); err != nil {
		return Observation{}, err
	}
	if body.Current == nil || body.Current.Temperature == nil {
		return Observation{}, fmt.Errorf("empty forecast")
	}
	obs := Observation{TemperatureC: *body.Current.Temperature}
	if body.Current.WeatherCode != nil {
		obs.WeatherCode = int(*body.Current.WeatherCode)
	}
	if body.Current.Precipitation != nil {
		obs.Precipitation = *body.Current.Precipitation
	}
	return obs, nil
}

func (o *openMeteo) getJSON(ctx context.Context, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "yascore")
	resp, err := o.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	if err := json.Unmarshal(buf, dest); err != nil {
		return err
	}
	return nil
}

type geoResult struct {
	Name        string  `json:"name"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

type geoResponse struct {
	Results []geoResult `json:"results"`
}

type forecastResponse struct {
	Current *struct {
		Temperature   *float64 `json:"temperature_2m"`
		WeatherCode   *float64 `json:"weather_code"`
		Precipitation *float64 `json:"precipitation"`
	} `json:"current"`
}

func withQuery(base, path string, q url.Values) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("bad base url")
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = q.Encode()
	u.Fragment = ""
	return u.String(), nil
}

func pickPlace(results []geoResult, country string) (Place, bool) {
	if len(results) == 0 {
		return Place{}, false
	}
	chosen := results[0]
	if strings.TrimSpace(country) != "" {
		found := false
		for _, r := range results {
			if countryMatch(r, country) {
				chosen = r
				found = true
				break
			}
		}
		if !found {
			return Place{}, false
		}
	}
	city := strings.TrimSpace(chosen.Name)
	if city == "" {
		return Place{}, false
	}
	return Place{
		Country:   strings.TrimSpace(chosen.Country),
		City:      city,
		Latitude:  chosen.Latitude,
		Longitude: chosen.Longitude,
	}, true
}

func countryMatch(r geoResult, country string) bool {
	want := fold(country)
	if want == "" {
		return true
	}
	if want == fold(r.Country) || want == fold(r.CountryCode) {
		return true
	}
	if code, ok := countryCodes[want]; ok && fold(r.CountryCode) == fold(code) {
		return true
	}
	got := fold(r.Country)
	if len(want) >= 4 && len(got) >= 4 && (strings.Contains(got, want) || strings.Contains(want, got)) {
		return true
	}
	return false
}

func fold(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.ReplaceAll(s, "ё", "е")
}

var countryCodes = map[string]string{
	"россия":            "RU",
	"рф":                "RU",
	"russia":            "RU",
	"казахстан":         "KZ",
	"беларусь":          "BY",
	"белоруссия":        "BY",
	"украина":           "UA",
	"германия":          "DE",
	"франция":           "FR",
	"сша":               "US",
	"соединенные штаты": "US",
	"великобритания":    "GB",
	"китай":             "CN",
	"япония":            "JP",
	"италия":            "IT",
	"испания":           "ES",
	"турция":            "TR",
	"польша":            "PL",
	"финляндия":         "FI",
}
