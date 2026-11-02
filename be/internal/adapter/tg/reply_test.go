package tg

import (
	"encoding/json"
	"testing"
)

func TestMarshalForceReply(t *testing.T) {
	t.Parallel()

	raw, err := marshalForceReply("123456:ABC-defGhIjKlmNoPQRstuVWXyz")
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["force_reply"] != true {
		t.Fatalf("force_reply = %v", got["force_reply"])
	}
	if got["selective"] != true {
		t.Fatalf("selective = %v", got["selective"])
	}
	if got["input_field_placeholder"] != "123456:ABC-defGhIjKlmNoPQRstuVWXyz" {
		t.Fatalf("placeholder = %v", got["input_field_placeholder"])
	}
}
