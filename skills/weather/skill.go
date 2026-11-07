package weather

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"yascore/config"
	"yascore/internal/domain"
)

const (
	askPlace   = domain.AskWeatherPlace
	failPhrase = "Не удалось узнать погоду."
)

// Place is the default city stored for later requests.
type Place struct {
	Country   string
	City      string
	Latitude  float64
	Longitude float64
}

// Observation is the current condition at a place.
type Observation struct {
	TemperatureC  float64
	WeatherCode   int
	Precipitation float64
}

// Provider looks up a place and its current weather.
// Another HTTP API can implement this without changing the skill.
type Provider interface {
	Geocode(ctx context.Context, city, country string) (Place, error)
	Current(ctx context.Context, place Place) (Observation, error)
}

// Skill answers the weather tool. It remembers one default place.
type Skill struct {
	provider Provider
	store    *Store
}

// New opens the database and the configured provider.
func New(s Settings) (*Skill, error) {
	s = s.normalized()
	store, err := OpenStore(s.Database)
	if err != nil {
		return nil, err
	}
	return &Skill{provider: newProvider(s), store: store}, nil
}

// Close releases the database.
func (s *Skill) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

// Handle is the protocol callback. Arguments are the tool JSON.
func (s *Skill) Handle(ctx context.Context, _ string, arguments string) (string, error) {
	var args struct {
		City    string `json:"city"`
		Country string `json:"country"`
	}
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return failPhrase, nil
		}
	}
	return s.Ask(ctx, args.City, args.Country)
}

// Ask returns a short Russian sentence.
// An empty city uses the saved place. With nothing saved, it asks and does not call the API.
// City and country together become the new default. City alone is one call and leaves the default.
func (s *Skill) Ask(ctx context.Context, city, country string) (string, error) {
	city = strings.TrimSpace(city)
	country = strings.TrimSpace(country)
	saved, ok, err := s.store.Load(ctx)
	if err != nil {
		config.Error("weather", "err", err)
		return failPhrase, nil
	}
	if city == "" {
		if !ok {
			return askPlace, nil
		}
		return s.report(ctx, saved), nil
	}
	place, err := s.provider.Geocode(ctx, city, country)
	if err != nil {
		return human(err), nil
	}
	place.City = city
	if country != "" {
		place.Country = country
	}
	if country != "" || !ok {
		if err := s.store.Save(ctx, place); err != nil {
			config.Error("weather", "err", err)
			return "Не удалось запомнить город.", nil
		}
	}
	return s.report(ctx, place), nil
}

// report speaks the forecast and, on success, records one ask for the local day.
// The first line is the spoken forecast. The second is a fact for the model.
func (s *Skill) report(ctx context.Context, place Place) string {
	sentence := s.speak(ctx, place)
	if sentence == failPhrase {
		return sentence
	}
	n, err := s.store.noteAsk(ctx, localDay(time.Now()))
	if err != nil {
		config.Error("weather", "err", err)
		return sentence
	}
	return sentence + "\nasks_today: " + strconv.Itoa(n)
}

func localDay(now time.Time) string {
	return now.In(time.Local).Format("2006-01-02")
}

func (s *Skill) speak(ctx context.Context, place Place) string {
	obs, err := s.provider.Current(ctx, place)
	if err != nil {
		config.Error("weather", "err", err)
		return failPhrase
	}
	return speak(place.City, obs)
}

func human(err error) string {
	if errors.Is(err, errNotFound) {
		return "Не нашла такой город."
	}
	if errors.Is(err, errProvider) {
		return "Источник погоды не настроен."
	}
	config.Error("weather", "err", err)
	return failPhrase
}
