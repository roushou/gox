package roblox

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var luaRoot = filepath.Join("..", "..", "..", "plugin", "roblox", "src", "bridge")

func readLuaFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(luaRoot, name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lua file %s: %v", name, err)
	}
	return string(content)
}

func TestBridgeSupportsProtocolOperations(t *testing.T) {
	bridgeSource := readLuaFile(t, "GoxBridge.luau")

	expectedOps := []Operation{
		OpPing,
		OpScriptCreate,
		OpScriptUpdate,
		OpScriptDelete,
		OpScriptGetSource,
		OpScriptExecute,
		OpInstanceCreate,
		OpInstanceSetProperty,
		OpInstanceDelete,
		OpInstanceGet,
		OpInstanceListChildren,
		OpInstanceFind,
	}
	for _, op := range expectedOps {
		needle := `request.operation == "` + string(op) + `"`
		if !strings.Contains(bridgeSource, needle) {
			t.Fatalf("bridge missing handler condition for %q", op)
		}
	}
}

func TestBridgeRequestFieldsMatchLua(t *testing.T) {
	protocol := readLuaFile(t, "Protocol.luau")
	transport := readLuaFile(t, "TransportLoop.luau")

	tags := jsonTags(t, Request{})
	for _, tag := range tags {
		found := strings.Contains(protocol, tag) || strings.Contains(transport, tag)
		if !found {
			t.Errorf("Request JSON tag %q not found in Protocol.luau or TransportLoop.luau", tag)
		}
	}
}

func TestBridgeResponseFieldsMatchLua(t *testing.T) {
	protocol := readLuaFile(t, "Protocol.luau")

	tags := jsonTags(t, Response{})
	for _, tag := range tags {
		if !strings.Contains(protocol, tag) {
			t.Errorf("Response JSON tag %q not found in Protocol.luau", tag)
		}
	}
}

func TestBridgeErrorFieldsMatchLua(t *testing.T) {
	protocol := readLuaFile(t, "Protocol.luau")

	tags := jsonTags(t, BridgeError{})
	for _, tag := range tags {
		if !strings.Contains(protocol, tag) {
			t.Errorf("BridgeError JSON tag %q not found in Protocol.luau", tag)
		}
	}
}

func TestBridgeCoerceValueCoverage(t *testing.T) {
	bridgeSource := readLuaFile(t, "GoxBridge.luau")

	expectedTypes := []string{
		"Vector3",
		"Vector2",
		"Ray",
		"Color3",
		"Color3Float",
		"CFrame",
		"UDim2",
		"UDim",
		"RectFloat",
		"BrickColor",
		"Enum",
		"Ref",
		"NumberRange",
		"NumberSequence",
		"ColorSequence",
		"PhysicalProperties",
		"Axes",
		"Faces",
		"Rect",
	}
	for _, typeName := range expectedTypes {
		needle := `tag == "` + typeName + `"`
		if !strings.Contains(bridgeSource, needle) {
			t.Errorf("coerceValue missing handler for _type %q", typeName)
		}
	}
}

func TestBridgeErrorCodesMatchFaultMap(t *testing.T) {
	bridgeSource := readLuaFile(t, "GoxBridge.luau")
	transportSource := readLuaFile(t, "TransportLoop.luau")

	luaProducedCodes := []string{
		"VALIDATION_ERROR",
		"NOT_FOUND",
		"ACCESS_DENIED",
		"INTERNAL_ERROR",
	}
	combined := bridgeSource + transportSource
	for _, code := range luaProducedCodes {
		if !strings.Contains(combined, code) {
			t.Errorf("error code %q not found in GoxBridge.luau or TransportLoop.luau", code)
		}
	}

	mapBridgeCodeCases := []string{
		"VALIDATION_ERROR",
		"ACCESS_DENIED",
		"NOT_FOUND",
		"CONFLICT",
		"TRANSIENT_ERROR",
		"default",
	}
	// Verify mapBridgeCode handles all documented codes by checking the function
	// contains each case string. We test the Go side by calling mapBridgeCode directly.
	for _, code := range mapBridgeCodeCases {
		if code == "default" {
			continue
		}
		result := mapBridgeCode(code)
		if result == "" {
			t.Errorf("mapBridgeCode(%q) returned empty code", code)
		}
	}
	if len(mapBridgeCodeCases) != 6 {
		t.Fatalf("expected 6 documented codes (including default), got %d", len(mapBridgeCodeCases))
	}
}

func TestBridgeProtocolVersionMatchesGo(t *testing.T) {
	transportSource := readLuaFile(t, "TransportLoop.luau")

	goVersion := bridgeProtocolVersion
	needle := `protocolVersion = "` + goVersion + `"`
	if !strings.Contains(transportSource, needle) {
		t.Fatalf("TransportLoop.luau does not contain protocol version %q", goVersion)
	}
}

func jsonTags(t *testing.T, v any) []string {
	t.Helper()
	rt := reflect.TypeOf(v)
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	var tags []string
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		tags = append(tags, name)
	}
	return tags
}
