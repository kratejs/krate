package gohost

import (
	"reflect"
	"testing"
)

func TestRouteMatchesHeadAndAllowedMethods(t *testing.T) {
	routes := []RouteEntry{
		{Method: "GET", Path: "/api/users/{id}"},
		{Method: "POST", Path: "/api/users"},
		{Method: "", Path: "/api/any"},
	}
	if !RouteMatches("HEAD", "/api/users/7", routes) {
		t.Error("HEAD must match a GET route")
	}
	if RouteMatches("DELETE", "/api/users/7", routes) {
		t.Error("DELETE must not match a GET-only route")
	}
	got, ok := AllowedMethods("/api/users/7", routes)
	if !ok || !reflect.DeepEqual(got, []string{"GET", "HEAD", "OPTIONS"}) {
		t.Errorf("AllowedMethods = %v, %v", got, ok)
	}
	if got, ok := AllowedMethods("/api/any", routes); !ok || !reflect.DeepEqual(got, []string{"*"}) {
		t.Errorf("method-less route should allow everything: %v %v", got, ok)
	}
	if _, ok := AllowedMethods("/api/none", routes); ok {
		t.Error("unknown path must not match")
	}
}
