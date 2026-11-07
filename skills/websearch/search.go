package websearch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// searchClient bounds a hung DuckDuckGo response. The request context can still end sooner.
var searchClient = &http.Client{Timeout: 8 * time.Second}

// Search queries the configured HTML endpoint and returns a short text digest.
func Search(ctx context.Context, endpointURL, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "пустой запрос", nil
	}
	form := url.Values{"q": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "yascore")
	resp, err := searchClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("поиск не удался: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("поиск не удался: http %d", resp.StatusCode)
	}
	return ParseResults(string(body)), nil
}

var (
	titleRe = regexp.MustCompile(`(?s)class="result__a"[^>]*>(.*?)</a>`)
	snipRe  = regexp.MustCompile(`(?s)class="result__snippet"[^>]*>(.*?)</(?:a|td)>`)
	tagRe   = regexp.MustCompile(`<[^>]+>`)
)

// ParseResults extracts up to five titles and snippets.
func ParseResults(html string) string {
	titles := titleRe.FindAllStringSubmatch(html, 5)
	snips := snipRe.FindAllStringSubmatch(html, 5)
	if len(titles) == 0 {
		return "ничего не нашлось"
	}
	var rows []string
	for i, title := range titles {
		line := clean(title[1])
		if i < len(snips) {
			if snip := clean(snips[i][1]); snip != "" {
				line += ". " + snip
			}
		}
		if line != "" {
			rows = append(rows, line)
		}
	}
	if len(rows) == 0 {
		return "ничего не нашлось"
	}
	return strings.Join(rows, "\n")
}

func clean(s string) string {
	s = tagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#x27;", "'")
	return strings.Join(strings.Fields(s), " ")
}
