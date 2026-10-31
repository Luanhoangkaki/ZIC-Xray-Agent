package strictjson

import (
	"strings"
	"testing"
)

func TestDuplicateAndDeepJSONRejected(t *testing.T) {
	for _, raw := range []string{`{"token":"a","token":"b"}`, `{"users":[{"id":1,"id":2}]}`, `{} {}`, strings.Repeat("[", 40) + "0" + strings.Repeat("]", 40)} {
		if Validate([]byte(raw)) == nil {
			t.Fatal("ambiguous/unbounded JSON accepted")
		}
	}
	if err := Validate([]byte(`{"users":[],"value":9223372036854775807}`)); err != nil {
		t.Fatal(err)
	}
}
