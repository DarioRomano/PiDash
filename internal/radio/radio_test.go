package radio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchKeepsWhatABrowserCanPlay(t *testing.T) {
	asked := ""
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path + "?" + r.URL.RawQuery
		w.Write([]byte(`[
			{"name":"  Radio  Swiss Jazz ","url":"http://old.example/jazz","url_resolved":"https://stream.example/jazz.mp3","country":"Switzerland","codec":"MP3","bitrate":128,"hls":0},
			{"name":"Jazz HLS","url_resolved":"https://stream.example/jazz.m3u8","codec":"AAC","hls":1},
			{"name":"Jazz Opus","url_resolved":"https://stream.example/jazz.opus","codec":"OPUS","hls":0},
			{"name":"Same stream again","url_resolved":"https://stream.example/jazz.mp3","codec":"MP3","hls":0},
			{"name":"Not a stream","url_resolved":"file:///etc/passwd","codec":"MP3","hls":0},
			{"name":"Jazz 24","url":"http://stream.example/j24","url_resolved":"","country":"","codec":"aac+","bitrate":0,"hls":0}
		]`))
	}))
	defer good.Close()
	// The first server is down; the next one answers.
	Servers = []string{"http://127.0.0.1:1", good.URL}

	got, err := Search(context.Background(), " jazz ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Station{Name: "Radio Swiss Jazz", URL: "https://stream.example/jazz.mp3", Detail: "Switzerland · MP3 128"}) || got[1].Name != "Jazz 24" || got[1].URL != "http://stream.example/j24" {
		t.Errorf("stations = %+v", got)
	}
	if !strings.Contains(asked, "/json/stations/search") || !strings.Contains(asked, "name=jazz") || !strings.Contains(asked, "hidebroken=true") {
		t.Errorf("asked %s", asked)
	}

	if _, err := Search(context.Background(), "j"); err == nil {
		t.Error("a one-letter search was sent")
	}
	Servers = []string{"http://127.0.0.1:1"}
	if _, err := Search(context.Background(), "jazz"); err == nil || !strings.Contains(err.Error(), "could not be reached") {
		t.Errorf("err = %v", err)
	}
}
