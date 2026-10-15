package service

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func normalizeGeminiFinalTurn(body []byte, action string) []byte {
	if action != "generateContent" && action != "streamGenerateContent" {
		return body
	}
	if !gjson.ValidBytes(body) {
		return body
	}
	contentsPath := "contents"
	if gjson.GetBytes(body, "request").Exists() {
		if !strings.HasPrefix(gjson.GetBytes(body, "model").String(), "gemini-") {
			return body
		}
		contentsPath = "request.contents"
	}
	contents := gjson.GetBytes(body, contentsPath)
	if !contents.IsArray() {
		return body
	}
	turns := contents.Array()
	if len(turns) == 0 {
		return body
	}
	last := turns[len(turns)-1]
	if last.Get("role").String() != "model" {
		return body
	}
	parts := last.Get("parts")
	if !parts.IsArray() || len(parts.Array()) == 0 {
		return body
	}
	for _, part := range parts.Array() {
		if part.Get("functionCall").Exists() || part.Get("function_call").Exists() {
			return body
		}
	}
	normalized, err := sjson.SetRawBytes(body, contentsPath+".-1", []byte(`{"role":"user","parts":[{"text":"Please continue."}]}`))
	if err != nil {
		return body
	}
	return normalized
}
