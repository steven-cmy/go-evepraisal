package staticdump

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sethgrid/pester"
)

func TestFindLastStaticDumpUsesBuildNumber(t *testing.T) {
	buildNumber := int64(3586130)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "{\"_key\":\"sde\",\"buildNumber\":%d,\"releaseDate\":\"2026-10-07T11:10:05Z\"}\n", buildNumber)
	}))
	defer server.Close()

	client := pester.New()
	url, checksum, err := findLastStaticDump(client, server.URL)
	if err != nil {
		t.Fatalf("findLastStaticDump() error = %v", err)
	}
	expectedURL := "https://developers.eveonline.com/static-data/tranquility/eve-online-static-data-3586130-yaml.zip"
	if url != expectedURL {
		t.Errorf("findLastStaticDump() URL = %q, want %q", url, expectedURL)
	}
	if checksum != "3586130" {
		t.Errorf("findLastStaticDump() checksum = %q, want %q", checksum, "3586130")
	}

	buildNumber++
	nextURL, nextChecksum, err := findLastStaticDump(client, server.URL)
	if err != nil {
		t.Fatalf("findLastStaticDump() for newer build error = %v", err)
	}
	if nextURL == url || nextChecksum == checksum {
		t.Errorf("new build did not change SDE URL/cache key: URL %q, checksum %q", nextURL, nextChecksum)
	}
}
