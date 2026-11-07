package currency

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestXMLFixtureSpeaksRubles(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?>
<ValCurs Date="06.11.2026" name="Foreign Currency Market">
  <Valute ID="R01235"><CharCode>USD</CharCode><Nominal>1</Nominal><Name>Доллар США</Name><Value>92,40</Value></Valute>
  <Valute ID="R01239"><CharCode>EUR</CharCode><Nominal>1</Nominal><Name>Евро</Name><Value>100,10</Value></Valute>
  <Valute ID="R01335"><CharCode>KZT</CharCode><Nominal>100</Nominal><Name>Казахстанский тенге</Name><Value>18,42</Value></Valute>
</ValCurs>`
	srv, hits := rateServer(t, []byte(body), http.StatusOK)
	defer srv.Close()
	skill := mustSkill(t, srv.URL)
	defer skill.Close()

	got, err := skill.Ask(context.Background(), "доллар", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Доллар стоит девяносто два рубля" {
		t.Fatalf("got %q", got)
	}
	if hits.n != 1 {
		t.Fatalf("hits %d", hits.n)
	}

	got, err = skill.Ask(context.Background(), "тенге", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Сто тенге стоят восемнадцать рублей" {
		t.Fatalf("tenge %q", got)
	}
}

func TestJSONFixtureSpeaksRubles(t *testing.T) {
	body := []byte(`{"Valute":{"USD":{"CharCode":"USD","Nominal":1,"Name":"Доллар США","Value":92.4}}}`)
	srv, _ := rateServer(t, body, http.StatusOK)
	defer srv.Close()
	skill := mustSkill(t, srv.URL)
	defer skill.Close()

	got, err := skill.Handle(context.Background(), "currency_rate", `{"currency":"USD"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Доллар стоит девяносто два рубля" {
		t.Fatalf("got %q", got)
	}
}

func TestWindows1251Fixture(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="windows-1251"?><ValCurs><Valute><CharCode>USD</CharCode><Nominal>1</Nominal><Name>`)
	body = append(body, 0xC4, 0xEE, 0xEB, 0xEB, 0xE0, 0xF0) // Доллар
	body = append(body, []byte(`</Name><Value>92,40</Value></Valute></ValCurs>`)...)
	srv, _ := rateServer(t, body, http.StatusOK)
	defer srv.Close()
	skill := mustSkill(t, srv.URL)
	defer skill.Close()

	got, err := skill.Ask(context.Background(), "USD", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Доллар стоит девяносто два рубля" {
		t.Fatalf("got %q", got)
	}
}

func TestUnknownCurrency(t *testing.T) {
	srv, hits := rateServer(t, []byte(xmlUSD), http.StatusOK)
	defer srv.Close()
	skill := mustSkill(t, srv.URL)
	defer skill.Close()

	got, err := skill.Ask(context.Background(), "абракадабра", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != unknownPhrase {
		t.Fatalf("got %q", got)
	}
	if hits.n != 0 {
		t.Fatalf("bank called for an unknown name: %d", hits.n)
	}
	saved, ok, err := skill.store.Load(context.Background())
	if err != nil || ok || saved != "" {
		t.Fatalf("saved %q ok=%v err=%v", saved, ok, err)
	}

	got, err = skill.Ask(context.Background(), "ZZZ", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != unknownPhrase {
		t.Fatalf("code %q", got)
	}
	if hits.n != 1 {
		t.Fatalf("hits %d", hits.n)
	}
	if strings.Contains(got, "руб") {
		t.Fatalf("invented rate %q", got)
	}
}

func TestHTTPError(t *testing.T) {
	srv, hits := rateServer(t, []byte("nope"), http.StatusBadGateway)
	defer srv.Close()
	skill := mustSkill(t, srv.URL)
	defer skill.Close()

	got, err := skill.Ask(context.Background(), "доллар", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != failPhrase {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "92") || strings.Contains(got, "руб") {
		t.Fatalf("invented rate %q", got)
	}
	if hits.n != 1 {
		t.Fatalf("hits %d", hits.n)
	}
	saved, ok, err := skill.store.Load(context.Background())
	if err != nil || ok || saved != "" {
		t.Fatalf("saved on error %q ok=%v err=%v", saved, ok, err)
	}
}

func TestFirstUseAskThenSavedDefault(t *testing.T) {
	srv, hits := rateServer(t, []byte(xmlUSD), http.StatusOK)
	defer srv.Close()
	skill := mustSkill(t, srv.URL)
	defer skill.Close()
	ctx := context.Background()

	got, err := skill.Handle(ctx, "currency_rate", `{"currency":"","remember":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if got != askCurrency {
		t.Fatalf("ask %q", got)
	}
	if hits.n != 0 {
		t.Fatalf("bank called before a currency: %d", hits.n)
	}

	got, err = skill.Handle(ctx, "currency_rate", `{"currency":"доллар"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Доллар стоит девяносто два рубля" {
		t.Fatalf("first %q", got)
	}
	saved, ok, err := skill.store.Load(ctx)
	if err != nil || !ok || saved != "USD" {
		t.Fatalf("saved %q ok=%v err=%v", saved, ok, err)
	}

	got, err = skill.Handle(ctx, "currency_rate", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Доллар стоит девяносто два рубля" {
		t.Fatalf("default %q", got)
	}
	if hits.n != 2 {
		t.Fatalf("hits %d", hits.n)
	}
	saved, ok, err = skill.store.Load(ctx)
	if err != nil || !ok || saved != "USD" {
		t.Fatalf("default changed %q ok=%v err=%v", saved, ok, err)
	}
}

func TestNamedCurrencyDoesNotReplaceDefault(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?><ValCurs>
  <Valute><CharCode>USD</CharCode><Nominal>1</Nominal><Name>Доллар США</Name><Value>92,40</Value></Valute>
  <Valute><CharCode>EUR</CharCode><Nominal>1</Nominal><Name>Евро</Name><Value>100,10</Value></Valute>
</ValCurs>`
	srv, _ := rateServer(t, []byte(body), http.StatusOK)
	defer srv.Close()
	skill := mustSkill(t, srv.URL)
	defer skill.Close()
	ctx := context.Background()

	if _, err := skill.Ask(ctx, "доллар", false); err != nil {
		t.Fatal(err)
	}
	got, err := skill.Ask(ctx, "евро", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Евро стоит сто рублей" {
		t.Fatalf("one-off %q", got)
	}
	saved, ok, err := skill.store.Load(ctx)
	if err != nil || !ok || saved != "USD" {
		t.Fatalf("default changed to %q ok=%v err=%v", saved, ok, err)
	}
	got, err = skill.Ask(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Доллар стоит девяносто два рубля" {
		t.Fatalf("saved %q", got)
	}
}

func TestRememberChangesDefault(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?><ValCurs>
  <Valute><CharCode>USD</CharCode><Nominal>1</Nominal><Name>Доллар США</Name><Value>92,40</Value></Valute>
  <Valute><CharCode>EUR</CharCode><Nominal>1</Nominal><Name>Евро</Name><Value>100,10</Value></Valute>
</ValCurs>`
	srv, _ := rateServer(t, []byte(body), http.StatusOK)
	defer srv.Close()
	skill := mustSkill(t, srv.URL)
	defer skill.Close()
	ctx := context.Background()

	if _, err := skill.Ask(ctx, "доллар", false); err != nil {
		t.Fatal(err)
	}
	got, err := skill.Handle(ctx, "currency_rate", `{"currency":"евро","remember":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Евро стоит сто рублей" {
		t.Fatalf("remember %q", got)
	}
	saved, ok, err := skill.store.Load(ctx)
	if err != nil || !ok || saved != "EUR" {
		t.Fatalf("saved %q ok=%v err=%v", saved, ok, err)
	}
	got, err = skill.Ask(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Евро стоит сто рублей" {
		t.Fatalf("default question %q", got)
	}
	saved, _, err = skill.store.Load(ctx)
	if err != nil || saved != "EUR" {
		t.Fatalf("default question wrote %q err=%v", saved, err)
	}
}

const xmlUSD = `<?xml version="1.0" encoding="UTF-8"?><ValCurs><Valute><CharCode>USD</CharCode><Nominal>1</Nominal><Name>Доллар США</Name><Value>92,40</Value></Valute></ValCurs>`

func mustSkill(t *testing.T, api string) *Skill {
	t.Helper()
	skill, err := New(Settings{
		APIBase:  api,
		Database: t.TempDir(),
		Timeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return skill
}

type hits struct{ n int }

func rateServer(t *testing.T, body []byte, status int) (*httptest.Server, *hits) {
	t.Helper()
	h := &hits{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.n++
		if r.URL.Path != dailyPath {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	return srv, h
}
