package assert

import (
	"reflect"
	"testing"
)

func Equal(t *testing.T, expected, actual any) {
	if !reflect.DeepEqual(expected, actual) {
		t.Helper()
		t.Errorf("Not equal: expected %#v, got %#v", expected, actual)
	}
}

func True(t *testing.T, value bool) {
	if !value {
		t.Helper()
		t.Errorf("Expected true but got false")
	}
}
