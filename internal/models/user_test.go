package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserJSONNeverExposesPasswordHash(t *testing.T) {
	user := User{Email: "driver@example.com", PasswordHash: "secret-hash"}
	encoded, err := json.Marshal(map[string]any{"user": user})
	if err != nil {
		t.Fatalf("marshal user: %v", err)
	}
	if strings.Contains(string(encoded), "PasswordHash") || strings.Contains(string(encoded), "secret-hash") {
		t.Fatalf("password hash leaked in user response: %s", encoded)
	}
	if !strings.Contains(string(encoded), "driver@example.com") {
		t.Fatalf("public user data missing: %s", encoded)
	}
}
