package api

import (
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestQueryKeysDoesNotExposeValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/health?api_key=secret&z=last&a=first", nil)
	if got, want := queryKeys(req), []string{"a", "api_key", "z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("query keys = %#v, want %#v", got, want)
	}
}
