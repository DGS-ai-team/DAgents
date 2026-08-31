package computeruse

import "testing"

func TestActionValidate(t *testing.T) {
	tests := []struct {
		name    string
		action  Action
		wantErr bool
	}{
		{"click", Action{Name: ActionClick, HasPoint: true}, false},
		{"click missing point", Action{Name: ActionClick}, true},
		{"scroll", Action{Name: ActionScroll, ScrollY: 3}, false},
		{"scroll zero", Action{Name: ActionScroll}, true},
		{"text", Action{Name: ActionTypeText, Text: "hello"}, false},
		{"invalid button", Action{Name: ActionClick, HasPoint: true, Button: "side"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.action.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
