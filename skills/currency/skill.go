package currency

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"yascore/config"
	"yascore/internal/domain"
)

const (
	askCurrency   = domain.AskCurrency
	failPhrase    = "Не удалось узнать курс."
	unknownPhrase = "Не знаю такую валюту."
)

// Skill answers the currency tool. It remembers one default currency.
// The rate is always in rubles. The base currency is not asked.
type Skill struct {
	provider Provider
	store    *Store
}

// New opens the skill document and the Central Bank client.
func New(s Settings) (*Skill, error) {
	s = s.normalized()
	store, err := OpenStore(s.Database)
	if err != nil {
		return nil, err
	}
	return &Skill{provider: newCBR(s), store: store}, nil
}

// Close releases the store.
func (s *Skill) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

// Handle is the protocol callback. Arguments are the tool JSON.
func (s *Skill) Handle(ctx context.Context, _ string, arguments string) (string, error) {
	var args struct {
		Currency string `json:"currency"`
		Remember bool   `json:"remember"`
	}
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return failPhrase, nil
		}
	}
	return s.Ask(ctx, args.Currency, args.Remember)
}

// Ask returns a short Russian sentence.
// An empty currency uses the saved code. With nothing saved, it asks and does not call the bank.
// The first named currency is saved. A later name is one quote and leaves the default,
// unless remember is set because the user asked to change it.
func (s *Skill) Ask(ctx context.Context, currency string, remember bool) (string, error) {
	saved, ok, err := s.store.Load(ctx)
	if err != nil {
		config.Error("currency", "err", err)
		return failPhrase, nil
	}
	code, form, kind := classify(currency)
	if kind == kindEmpty {
		if !ok {
			return askCurrency, nil
		}
		return s.speak(ctx, saved, forms[saved])
	}
	if kind == kindRUB {
		return rubPhrase, nil
	}
	if kind == kindUnknown {
		return unknownPhrase, nil
	}
	quote, err := s.provider.Rate(ctx, code)
	if err != nil {
		return human(err), nil
	}
	if form.one == "" {
		form = formOrXML(code, quote.Name)
	}
	if remember || !ok {
		if err := s.store.Save(ctx, code); err != nil {
			config.Error("currency", "err", err)
			return "Не удалось запомнить валюту.", nil
		}
	}
	return speak(form, quote.Nominal, quote.Rubles), nil
}

func (s *Skill) speak(ctx context.Context, code string, form nameForm) (string, error) {
	if code == "RUB" || code == "RUR" {
		return rubPhrase, nil
	}
	quote, err := s.provider.Rate(ctx, code)
	if err != nil {
		return human(err), nil
	}
	if form.one == "" {
		form = formOrXML(code, quote.Name)
	}
	return speak(form, quote.Nominal, quote.Rubles), nil
}

func human(err error) string {
	if errors.Is(err, errUnknown) {
		return unknownPhrase
	}
	config.Error("currency", "err", err)
	return failPhrase
}
