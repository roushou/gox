package action

import "testing"

func TestValidateInputSuccess(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "integer"},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
	err := ValidateInput(schema, map[string]any{
		"name": "builder",
		"age":  10,
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidateInputRejectsMissingRequired(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
	err := ValidateInput(schema, map[string]any{})
	if err == nil {
		t.Fatal("expected missing required validation error")
	}
	validationErr, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if validationErr.Path != "input.name" || validationErr.Rule != "required" {
		t.Fatalf("unexpected validation error: %#v", validationErr)
	}
}

func TestValidateInputRejectsUnknownField(t *testing.T) {
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
	err := ValidateInput(schema, map[string]any{"x": true})
	if err == nil {
		t.Fatal("expected unknown field validation error")
	}
	validationErr, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if validationErr.Path != "input.x" || validationErr.Rule != "additionalProperties" {
		t.Fatalf("unexpected validation error: %#v", validationErr)
	}
}

func TestValidateInputRejectsWrongType(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
	err := ValidateInput(schema, map[string]any{"name": 123})
	if err == nil {
		t.Fatal("expected type validation error")
	}
	validationErr, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if validationErr.Path != "input.name" || validationErr.Rule != "type" {
		t.Fatalf("unexpected validation error: %#v", validationErr)
	}
}

func TestValidateInputEnum(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scriptType": map[string]any{
				"type": "string",
				"enum": []any{"Script", "LocalScript", "ModuleScript"},
			},
		},
		"required":             []string{"scriptType"},
		"additionalProperties": false,
	}
	err := ValidateInput(schema, map[string]any{"scriptType": "NotValid"})
	if err == nil {
		t.Fatal("expected enum validation error")
	}
	validationErr, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if validationErr.Path != "input.scriptType" || validationErr.Rule != "enum" {
		t.Fatalf("unexpected validation error: %#v", validationErr)
	}
}

func TestValidateInputAllowsAdditionalWhenEnabled(t *testing.T) {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": true,
	}
	if err := ValidateInput(schema, map[string]any{"anything": 1}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
