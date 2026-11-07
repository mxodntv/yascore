package domain

import "testing"

func TestReplyZeroTextIsEmpty(t *testing.T) {
	var r Reply
	if r.Text != "" || r.Model != "" {
		t.Fatalf("%+v", r)
	}
}

func TestToolNameRoundTrip(t *testing.T) {
	tool := Tool{Name: "web_search", Description: "поиск"}
	if tool.Name != "web_search" {
		t.Fatal(tool.Name)
	}
}
