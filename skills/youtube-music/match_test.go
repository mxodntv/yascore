package youtubemusic

import "testing"

func TestPickTrackIDPrefersOriginal(t *testing.T) {
	raw := `{"tracks":[
		{"id":"cover","title":"Get Lucky (Instrumental)","artists":"Lounge Band"},
		{"id":"real","title":"Get Lucky","artists":"Daft Punk, Pharrell Williams"}
	]}`
	if got := pickTrackID(raw, "get lucky daft punk"); got != "real" {
		t.Fatalf("got %s", got)
	}
}

func TestSummarizeStatus(t *testing.T) {
	playing := `{"state":{"status":"playing","index":0,"queue":[{"title":"Get Lucky","artists":"Daft Punk"}]}}`
	if got := summarizeStatus(playing); got != "играет Get Lucky — Daft Punk" {
		t.Fatalf("playing %q", got)
	}
	broken := `{"state":{"status":"error","error":"аудиопоток прерван","index":0,"queue":[{"title":"Get Lucky","artists":"Daft Punk"}]}}`
	if got := summarizeStatus(broken); got != "не играет Get Lucky — Daft Punk: аудиопоток прерван" {
		t.Fatalf("error %q", got)
	}
}

func TestVagueMusicResumesStoppedQueue(t *testing.T) {
	if !vagueMusic("музыку", "track") {
		t.Fatal("музыку should be a bare request")
	}
	if vagueMusic("Get Lucky", "track") {
		t.Fatal("named track is not bare")
	}
	stopped := `{"state":{"status":"stopped","queue":[{"title":"Get Lucky"}]}}`
	if got := nextMusicAction(stopped); got != "resume" {
		t.Fatalf("stopped %s", got)
	}
	playing := `{"state":{"status":"playing","queue":[{"title":"Get Lucky"}]}}`
	if got := nextMusicAction(playing); got != "already" {
		t.Fatalf("playing %s", got)
	}
	empty := `{"state":{"status":"stopped","queue":[]}}`
	if got := nextMusicAction(empty); got != "mix" {
		t.Fatalf("empty %s", got)
	}
}

func TestReadyLine(t *testing.T) {
	raw := `{"state":{"status":"buffering","index":0,"queue":[{"title":"Get Lucky","artists":"Daft Punk"}]}}`
	got, ok := readyLine(raw)
	if !ok || got != "играет Get Lucky — Daft Punk" {
		t.Fatalf("%v %q", ok, got)
	}
}

func TestPlaylistPhrase(t *testing.T) {
	if got := playlistPhrase("PLtest"); got != serviceBase+"/playlist?list=PLtest" {
		t.Fatalf("id %q", got)
	}
	if got := playlistPhrase("вечер"); got != "вечер" {
		t.Fatalf("name %q", got)
	}
}

func TestCleanQuery(t *testing.T) {
	if got := cleanQuery("включи песню Get Lucky"); got != "Get Lucky" {
		t.Fatalf("got %q", got)
	}
}
