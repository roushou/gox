package action

import (
	"fmt"
	"math"
	"reflect"
)

func ValidateInput(schema map[string]any, input map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	if input == nil {
		input = map[string]any{}
	}
	return validateValue(input, schema, "input")
}

func validateValue(value any, schema map[string]any, path string) error {
	if len(schema) == 0 {
		return nil
	}

	if enumRaw, ok := schema["enum"]; ok {
		enumValues, ok := enumRaw.([]any)
		if !ok {
			return SchemaError{Message: path + ".enum must be an array"}
		}
		if !inEnum(value, enumValues) {
			return ValidationError{
				Path:    path,
				Rule:    "enum",
				Message: "must be one of the allowed values",
			}
		}
	}

	typ, _ := schema["type"].(string)
	switch typ {
	case "":
		return nil
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return ValidationError{
				Path:    path,
				Rule:    "type",
				Message: "must be an object",
			}
		}
		return validateObject(obj, schema, path)
	case "array":
		arr, ok := value.([]any)
		if !ok {
			return ValidationError{
				Path:    path,
				Rule:    "type",
				Message: "must be an array",
			}
		}
		itemSchema, _ := schema["items"].(map[string]any)
		for i, item := range arr {
			if err := validateValue(item, itemSchema, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	case "string":
		if _, ok := value.(string); !ok {
			return ValidationError{
				Path:    path,
				Rule:    "type",
				Message: "must be a string",
			}
		}
		return nil
	case "boolean":
		if _, ok := value.(bool); !ok {
			return ValidationError{
				Path:    path,
				Rule:    "type",
				Message: "must be a boolean",
			}
		}
		return nil
	case "number":
		if !isNumber(value) {
			return ValidationError{
				Path:    path,
				Rule:    "type",
				Message: "must be a number",
			}
		}
		return nil
	case "integer":
		if !isInteger(value) {
			return ValidationError{
				Path:    path,
				Rule:    "type",
				Message: "must be an integer",
			}
		}
		return nil
	default:
		return SchemaError{Message: fmt.Sprintf("unsupported type %q at %s", typ, path)}
	}
}

func validateObject(value map[string]any, schema map[string]any, path string) error {
	requiredKeys, err := requiredKeys(schema["required"])
	if err != nil {
		return SchemaError{Message: err.Error()}
	}
	for _, key := range requiredKeys {
		if _, exists := value[key]; !exists {
			return ValidationError{
				Path:    path + "." + key,
				Rule:    "required",
				Message: "is required",
			}
		}
	}

	properties := map[string]map[string]any{}
	if rawProps, ok := schema["properties"]; ok {
		propsMap, ok := rawProps.(map[string]any)
		if !ok {
			return SchemaError{Message: path + ".properties must be an object"}
		}
		for name, raw := range propsMap {
			propSchema, ok := raw.(map[string]any)
			if !ok {
				return SchemaError{Message: path + ".properties." + name + " must be an object"}
			}
			properties[name] = propSchema
		}
	}

	allowAdditional := false
	if raw, exists := schema["additionalProperties"]; exists {
		typed, ok := raw.(bool)
		if !ok {
			return SchemaError{Message: path + ".additionalProperties must be a boolean"}
		}
		allowAdditional = typed
	}

	for key, fieldValue := range value {
		propSchema, known := properties[key]
		if !known {
			if !allowAdditional {
				return ValidationError{
					Path:    path + "." + key,
					Rule:    "additionalProperties",
					Message: "is not allowed",
				}
			}
			continue
		}
		if err := validateValue(fieldValue, propSchema, path+"."+key); err != nil {
			return err
		}
	}
	return nil
}

func requiredKeys(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	switch typed := raw.(type) {
	case []string:
		return typed, nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("required entries must be strings")
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("required must be an array")
	}
}

func isNumber(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	default:
		return false
	}
}

func isInteger(v any) bool {
	switch typed := v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return math.Trunc(float64(typed)) == float64(typed)
	case float64:
		return math.Trunc(typed) == typed
	default:
		return false
	}
}

func inEnum(value any, allowed []any) bool {
	for _, candidate := range allowed {
		if reflect.DeepEqual(value, candidate) {
			return true
		}
	}
	return false
}
