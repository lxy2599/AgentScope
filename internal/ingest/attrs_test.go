package ingest

import (
	"testing"
)

func TestParseAttributes_simpleString(t *testing.T) {
	m := MustParseAttributes(`{"llm.provider":"openai","agentscope.span.kind":"LLM"}`)
	if AttrString(m, "llm.provider") != "openai" {
		t.Errorf("llm.provider = %q, want openai", AttrString(m, "llm.provider"))
	}
	if AttrString(m, "agentscope.span.kind") != "LLM" {
		t.Errorf("span.kind = %q, want LLM", AttrString(m, "agentscope.span.kind"))
	}
}

func TestParseAttributes_escapedQuotes(t *testing.T) {
	// JSON string with escaped quotes inside a value
	m := MustParseAttributes(`{"tool.arguments_summary":"{\"order_id\":\"123\"}","tool.name":"search"}`)
	v := AttrString(m, "tool.arguments_summary")
	if v != `{"order_id":"123"}` {
		t.Errorf("arguments_summary = %q, want %q", v, `{"order_id":"123"}`)
	}
}

func TestParseAttributes_jsonStringValue(t *testing.T) {
	// structured_output with JSON content
	m := MustParseAttributes(`{"answer.structured_output":"{\"result\":42,\"unit\":\"km\"}"}`)
	v := AttrString(m, "answer.structured_output")
	if v != `{"result":42,"unit":"km"}` {
		t.Errorf("structured_output = %q", v)
	}
}

func TestParseAttributes_numbers(t *testing.T) {
	m := MustParseAttributes(`{"llm.input_tokens":50,"llm.output_tokens":100,"llm.cost":0.002}`)
	if p := AttrInt(m, "llm.input_tokens"); p == nil || *p != 50 {
		t.Errorf("input_tokens = %v, want 50", p)
	}
	if p := AttrInt(m, "llm.output_tokens"); p == nil || *p != 100 {
		t.Errorf("output_tokens = %v, want 100", p)
	}
	if p := AttrFloat(m, "llm.cost"); p == nil || *p != 0.002 {
		t.Errorf("cost = %v, want 0.002", p)
	}
	if AttrInt64(m, "llm.total_tokens") != 0 {
		t.Errorf("total_tokens = %d, want 0 (missing key)", AttrInt64(m, "llm.total_tokens"))
	}
}

func TestParseAttributes_bool(t *testing.T) {
	m := MustParseAttributes(`{"tool.success":true,"agentscope.content_captured":false}`)
	if !AttrBool(m, "tool.success") {
		t.Error("tool.success should be true")
	}
	if AttrBool(m, "agentscope.content_captured") {
		t.Error("content_captured should be false")
	}
}

func TestParseAttributes_stringArray(t *testing.T) {
	m := MustParseAttributes(`{"retrieval.document_ids":["doc-1","doc-2","doc-3"]}`)
	ids := AttrStringArray(m, "retrieval.document_ids")
	if len(ids) != 3 {
		t.Fatalf("got %d ids, want 3", len(ids))
	}
	if ids[0] != "doc-1" || ids[1] != "doc-2" || ids[2] != "doc-3" {
		t.Errorf("ids = %v", ids)
	}
}

func TestParseAttributes_floatArray(t *testing.T) {
	m := MustParseAttributes(`{"retrieval.scores":[0.95,0.87,0.72]}`)
	scores := AttrFloatArray(m, "retrieval.scores")
	if len(scores) != 3 {
		t.Fatalf("got %d scores, want 3", len(scores))
	}
	if scores[0] != 0.95 || scores[1] != 0.87 || scores[2] != 0.72 {
		t.Errorf("scores = %v", scores)
	}
}

func TestParseAttributes_empty(t *testing.T) {
	m := MustParseAttributes("")
	if m == nil {
		t.Fatal("MustParseAttributes of empty should return empty map")
	}
	if AttrString(m, "any.key") != "" {
		t.Error("empty map should return empty string")
	}
}

func TestParseAttributes_thoughtKind(t *testing.T) {
	m := MustParseAttributes(`{"agentscope.span.kind":"THOUGHT","thought.summary":"thinking","thought.stage":"reasoning","thought.confidence":0.85}`)
	if AttrString(m, "agentscope.span.kind") != "THOUGHT" {
		t.Errorf("kind = %q", AttrString(m, "agentscope.span.kind"))
	}
	if AttrString(m, "thought.summary") != "thinking" {
		t.Errorf("summary = %q", AttrString(m, "thought.summary"))
	}
	if AttrString(m, "thought.stage") != "reasoning" {
		t.Errorf("stage = %q", AttrString(m, "thought.stage"))
	}
	if p := AttrFloat(m, "thought.confidence"); p == nil || *p != 0.85 {
		t.Errorf("confidence = %v, want 0.85", p)
	}
}

func TestParseAttributes_finalAnswerKind(t *testing.T) {
	m := MustParseAttributes(`{"agentscope.span.kind":"FINAL_ANSWER","answer.summary":"summary","answer.output":"output text"}`)
	if AttrString(m, "answer.summary") != "summary" {
		t.Errorf("summary = %q", AttrString(m, "answer.summary"))
	}
	if AttrString(m, "answer.output") != "output text" {
		t.Errorf("output = %q", AttrString(m, "answer.output"))
	}
}

func TestParseAttributes_invalidJSON(t *testing.T) {
	_, err := ParseAttributes(`{invalid}`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
