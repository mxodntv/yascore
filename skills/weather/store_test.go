package weather

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestSpeakSentenceUnchanged(t *testing.T) {
	got := speak("Киров", Observation{TemperatureC: -1, WeatherCode: 45})
	if got != "В городе Киров сейчас минус один и туман" {
		t.Fatalf("sentence %q", got)
	}
}

func TestNoteAskResetsWhenDayChanges(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Save(ctx, Place{Country: "Россия", City: "Киров", Latitude: 58.6, Longitude: 49.66}); err != nil {
		t.Fatal(err)
	}
	if n, err := store.noteAsk(ctx, "2026-11-06"); err != nil || n != 1 {
		t.Fatalf("first %d %v", n, err)
	}
	if n, err := store.noteAsk(ctx, "2026-11-06"); err != nil || n != 2 {
		t.Fatalf("same day %d %v", n, err)
	}
	if n, err := store.noteAsk(ctx, "2026-11-07"); err != nil || n != 1 {
		t.Fatalf("next day %d %v", n, err)
	}
	saved, ok, err := store.Load(ctx)
	if err != nil || !ok || saved.City != "Киров" || saved.Latitude != 58.6 {
		t.Fatalf("place %+v ok=%v err=%v", saved, ok, err)
	}
	if err := store.Save(ctx, Place{Country: "Россия", City: "Казань", Latitude: 55.79, Longitude: 49.12}); err != nil {
		t.Fatal(err)
	}
	if n, err := store.noteAsk(ctx, "2026-11-07"); err != nil || n != 2 {
		t.Fatalf("save wiped the count: %d %v", n, err)
	}
	saved, ok, err = store.Load(ctx)
	if err != nil || !ok || saved.City != "Казань" {
		t.Fatalf("place after save %+v ok=%v err=%v", saved, ok, err)
	}
}

func TestAskCountFollowsLocalDay(t *testing.T) {
	geo := &recorder{lat: 55.79, lon: 49.12, country: "Россия", name: "Казань"}
	api := &recorder{}
	geoSrv := httptest.NewServer(geo.handler("search"))
	apiSrv := httptest.NewServer(api.handler("forecast"))
	defer geoSrv.Close()
	defer apiSrv.Close()

	skill := mustSkill(t, filepath.Join(t.TempDir(), "v.db"), geoSrv.URL, apiSrv.URL, 8*time.Second)
	defer skill.Close()
	ctx := context.Background()
	sentence := "В городе Казань сейчас два и пасмурно"

	got, err := skill.Ask(ctx, "Казань", "Россия")
	if err != nil {
		t.Fatal(err)
	}
	assertSpoken(t, got, sentence, 1)
	for n := 2; n <= 3; n++ {
		got, err = skill.Ask(ctx, "", "")
		if err != nil {
			t.Fatal(err)
		}
		assertSpoken(t, got, sentence, n)
	}
	if geo.n() != 1 {
		t.Fatalf("geocoded %d times", geo.n())
	}

	tomorrow := time.Now().In(time.Local).Add(48 * time.Hour).Format("2006-01-02")
	n, err := skill.store.noteAsk(ctx, tomorrow)
	if err != nil || n != 1 {
		t.Fatalf("reset %d %v", n, err)
	}
	saved, ok, err := skill.store.Load(ctx)
	if err != nil || !ok || saved.City != "Казань" {
		t.Fatalf("place %+v ok=%v err=%v", saved, ok, err)
	}
}
