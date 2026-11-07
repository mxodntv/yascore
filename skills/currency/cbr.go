package currency

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

var (
	errUnknown = errors.New("currency is not in the daily list")
	errRates   = errors.New("currency rates are unavailable")
)

const dailyPath = "/scripts/XML_daily.asp"

// Quote is one Central Bank rate: rubles for Nominal units of the currency.
type Quote struct {
	Code    string
	Nominal int
	Name    string
	Rubles  float64
}

// Provider fetches one daily rate. Tests substitute an HTTP server.
type Provider interface {
	Rate(ctx context.Context, code string) (Quote, error)
}

type cbr struct {
	apiBase string
	client  *http.Client
}

func newCBR(s Settings) *cbr {
	return &cbr{
		apiBase: strings.TrimRight(strings.TrimSpace(s.APIBase), "/"),
		client:  &http.Client{Timeout: s.Timeout},
	}
}

func (c *cbr) Rate(ctx context.Context, code string) (Quote, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" || c == nil || c.apiBase == "" {
		return Quote{}, errRates
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase+dailyPath, nil)
	if err != nil {
		return Quote{}, err
	}
	req.Header.Set("User-Agent", "yascore")
	resp, err := c.client.Do(req)
	if err != nil {
		return Quote{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return Quote{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Quote{}, errRates
	}
	quotes, err := parseRates(body)
	if err != nil {
		return Quote{}, err
	}
	q, ok := quotes[code]
	if !ok {
		return Quote{}, errUnknown
	}
	if q.Nominal <= 0 {
		q.Nominal = 1
	}
	return q, nil
}

func parseRates(body []byte) (map[string]Quote, error) {
	body = bytes.TrimSpace(toUTF8(body))
	if len(body) == 0 {
		return nil, errRates
	}
	if body[0] == '{' {
		return parseJSONRates(body)
	}
	return parseXMLRates(body)
}

func parseXMLRates(body []byte) (map[string]Quote, error) {
	var doc struct {
		Valutes []struct {
			CharCode string `xml:"CharCode"`
			Nominal  int    `xml:"Nominal"`
			Name     string `xml:"Name"`
			Value    string `xml:"Value"`
		} `xml:"Valute"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	out := make(map[string]Quote, len(doc.Valutes))
	for _, v := range doc.Valutes {
		code := strings.ToUpper(strings.TrimSpace(v.CharCode))
		if code == "" {
			continue
		}
		rubles, err := parseRubles(v.Value)
		if err != nil {
			continue
		}
		out[code] = Quote{Code: code, Nominal: v.Nominal, Name: strings.TrimSpace(v.Name), Rubles: rubles}
	}
	if len(out) == 0 {
		return nil, errRates
	}
	return out, nil
}

func parseJSONRates(body []byte) (map[string]Quote, error) {
	var doc struct {
		Valute map[string]struct {
			CharCode string  `json:"CharCode"`
			Nominal  int     `json:"Nominal"`
			Name     string  `json:"Name"`
			Value    float64 `json:"Value"`
		} `json:"Valute"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	out := make(map[string]Quote, len(doc.Valute))
	for key, v := range doc.Valute {
		code := strings.ToUpper(strings.TrimSpace(v.CharCode))
		if code == "" {
			code = strings.ToUpper(strings.TrimSpace(key))
		}
		if code == "" {
			continue
		}
		out[code] = Quote{Code: code, Nominal: v.Nominal, Name: strings.TrimSpace(v.Name), Rubles: v.Value}
	}
	if len(out) == 0 {
		return nil, errRates
	}
	return out, nil
}

func parseRubles(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, " ", "")
	raw = strings.ReplaceAll(raw, ",", ".")
	if raw == "" {
		return 0, fmt.Errorf("empty rate")
	}
	var v float64
	if _, err := fmt.Sscan(raw, &v); err != nil {
		return 0, err
	}
	return v, nil
}

func toUTF8(body []byte) []byte {
	if !utf8.Valid(body) {
		body = decode1251(body)
	}
	// encoding/xml refuses a windows-1251 declaration unless a CharsetReader is set.
	// The bytes are UTF-8 by the time we parse them.
	s := string(body)
	for _, old := range []string{"windows-1251", "WINDOWS-1251", "Windows-1251", "cp1251", "CP1251"} {
		s = strings.ReplaceAll(s, old, "UTF-8")
	}
	return []byte(s)
}

func decode1251(body []byte) []byte {
	var buf bytes.Buffer
	buf.Grow(len(body))
	for _, c := range body {
		var r rune
		switch {
		case c < 0x80:
			r = rune(c)
		case c >= 0xC0:
			r = 0x0410 + rune(c-0xC0)
		default:
			r = cp1251[c-0x80]
		}
		buf.WriteRune(r)
	}
	return buf.Bytes()
}

// cp1251 maps bytes 0x80–0xBF. 0xC0–0xFF are the Cyrillic alphabet and are handled above.
var cp1251 = [64]rune{
	0x0402, 0x0403, 0x201A, 0x0453, 0x201E, 0x2026, 0x2020, 0x2021,
	0x20AC, 0x2030, 0x0409, 0x2039, 0x040A, 0x040C, 0x040B, 0x040F,
	0x0452, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x0098, 0x2122, 0x0459, 0x203A, 0x045A, 0x045C, 0x045B, 0x045F,
	0x00A0, 0x040E, 0x045E, 0x0408, 0x00A4, 0x0490, 0x00A6, 0x00A7,
	0x0401, 0x00A9, 0x0404, 0x00AB, 0x00AC, 0x00AD, 0x00AE, 0x0407,
	0x00B0, 0x00B1, 0x0406, 0x0456, 0x0491, 0x00B5, 0x00B6, 0x00B7,
	0x0451, 0x2116, 0x0454, 0x00BB, 0x0458, 0x0405, 0x0455, 0x0457,
}
