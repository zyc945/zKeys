package keystore

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// These public fixtures are independent of Store.Write, so reader and writer
// changes cannot silently agree on a format that breaks older vaults.
func TestV1CompatibilityVectors(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "keystore-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Vectors []struct {
			Name     string
			Password string
			Entries  map[string]string
			Vault    json.RawMessage
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures.Vectors) != 2 {
		t.Fatal("expected both fixed v1 compatibility vectors")
	}
	for _, vector := range fixtures.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			s := testStore(t)
			if err := os.WriteFile(s.Path(), vector.Vault, 0600); err != nil {
				t.Fatal(err)
			}
			entries, err := s.Read([]byte(vector.Password))
			if err != nil || !reflect.DeepEqual(entries, vector.Entries) {
				t.Fatalf("fixed v1 vector could not be read: %v", err)
			}
			if _, err := s.Read([]byte(vector.Password + "-wrong")); !errors.Is(err, InvalidVault) {
				t.Fatalf("wrong password accepted: %v", err)
			}
			for _, field := range []string{"version", "salt", "nonce", "ciphertext"} {
				t.Run("tampered-"+field, func(t *testing.T) {
					var envelope map[string]any
					if err := json.Unmarshal(vector.Vault, &envelope); err != nil {
						t.Fatal(err)
					}
					if field == "version" {
						envelope[field] = 2
					} else {
						value, err := base64.StdEncoding.DecodeString(envelope[field].(string))
						if err != nil || len(value) == 0 {
							t.Fatal("invalid fixture field", field, err)
						}
						value[0] ^= 1
						envelope[field] = base64.StdEncoding.EncodeToString(value)
					}
					changed, err := json.Marshal(envelope)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(s.Path(), changed, 0600); err != nil {
						t.Fatal(err)
					}
					if _, err := s.Read([]byte(vector.Password)); !errors.Is(err, InvalidVault) {
						t.Fatalf("tampered %s accepted: %v", field, err)
					}
				})
			}
		})
	}
}
