package services

import (
	"testing"

	"github.com/anxi0uz/logiflow/internal/api"
)

func testCounterpartyRequest(name string) api.CounterpartyWrite {
	inn, phone := "7707083893", "+7 900 000-00-00"
	return api.CounterpartyWrite{Kind: "organization", Name: name, Inn: &inn, Address: "Тестовый адрес", ContactName: "Иван Петров", Phone: &phone}
}

func TestCounterpartyRequisitesValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*api.CounterpartyWrite)
		valid  bool
	}{
		{"organization", func(*api.CounterpartyWrite) {}, true},
		{"entrepreneur", func(r *api.CounterpartyWrite) { r.Kind = "entrepreneur"; inn := "500100732259"; r.Inn = &inn }, true},
		{"individual_without_inn", func(r *api.CounterpartyWrite) { r.Kind = "individual"; r.Inn = nil }, true},
		{"bad_checksum", func(r *api.CounterpartyWrite) { inn := "7707083894"; r.Inn = &inn }, false},
		{"all_zero_inn", func(r *api.CounterpartyWrite) { inn := "0000000000"; r.Inn = &inn }, false},
		{"wrong_inn_length", func(r *api.CounterpartyWrite) { inn := "500100732259"; r.Inn = &inn }, false},
		{"blank_name", func(r *api.CounterpartyWrite) { r.Name = "  " }, false},
		{"missing_contact", func(r *api.CounterpartyWrite) { r.Phone = nil }, false},
		{"invalid_email", func(r *api.CounterpartyWrite) { email := "Иван <ivan@example.test>"; r.Email = &email }, false},
		{"kpp_for_individual", func(r *api.CounterpartyWrite) { r.Kind = "individual"; r.Inn = nil; kpp := "770101001"; r.Kpp = &kpp }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testCounterpartyRequest("Контрагент")
			tt.change(&r)
			_, err := normalizeCounterparty(r)
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v, error=%v", tt.valid, err)
			}
		})
	}
}
