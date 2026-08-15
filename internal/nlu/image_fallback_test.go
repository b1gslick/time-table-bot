package nlu

import (
	"context"
	"errors"
	"testing"
)

type fakeConfidenceImageRecognizer struct {
	text       string
	confidence float64
	err        error
}

func (r fakeConfidenceImageRecognizer) RecognizeTextWithConfidence(context.Context, ImageTextRequest) (string, float64, error) {
	return r.text, r.confidence, r.err
}

type fakeConfidenceLayoutImageRecognizer struct {
	fakeConfidenceImageRecognizer
	layout string
}

func (r fakeConfidenceLayoutImageRecognizer) RecognizeTextWithLayoutAndConfidence(context.Context, ImageTextRequest) (string, string, float64, error) {
	return r.text, r.layout, r.confidence, r.err
}

type fakeImageRecognizer struct {
	text  string
	calls int
}

func (r *fakeImageRecognizer) RecognizeText(context.Context, ImageTextRequest) (string, error) {
	r.calls++
	return r.text, nil
}

func TestImageFallbackKeepsReliableLocalOCR(t *testing.T) {
	fallback := &fakeImageRecognizer{text: "from qwen"}
	recognizer := NewFallbackImageTextRecognizer(fakeConfidenceImageRecognizer{text: "printed", confidence: 95}, fallback)
	got, err := recognizer.RecognizeText(context.Background(), ImageTextRequest{Image: []byte("image")})
	if err != nil || got != "printed" || fallback.calls != 0 {
		t.Fatalf("result = %q, err=%v, fallback calls=%d", got, err, fallback.calls)
	}
}

func TestImageFallbackPreservesReliableLocalOCRLayout(t *testing.T) {
	recognizer := NewFallbackImageTextRecognizer(fakeConfidenceLayoutImageRecognizer{
		fakeConfidenceImageRecognizer: fakeConfidenceImageRecognizer{text: "printed", confidence: 95},
		layout:                        "[x=10 y=20] printed",
	}, nil)
	text, layout, err := recognizer.RecognizeTextWithLayout(context.Background(), ImageTextRequest{Image: []byte("image")})
	if err != nil || text != "printed" || layout != "[x=10 y=20] printed" {
		t.Fatalf("result = %q, %q, %v", text, layout, err)
	}
}

func TestImageFallbackUsesQwenForLowConfidenceText(t *testing.T) {
	fallback := &fakeImageRecognizer{text: "handwritten"}
	recognizer := NewFallbackImageTextRecognizer(fakeConfidenceImageRecognizer{text: "garbled", confidence: 25}, fallback)
	got, err := recognizer.RecognizeText(context.Background(), ImageTextRequest{Image: []byte("image")})
	if err != nil || got != "handwritten" || fallback.calls != 1 {
		t.Fatalf("result = %q, err=%v, fallback calls=%d", got, err, fallback.calls)
	}
}

func TestImageFallbackUsesQwenWhenLocalOCRFails(t *testing.T) {
	fallback := &fakeImageRecognizer{text: "handwritten"}
	recognizer := NewFallbackImageTextRecognizer(fakeConfidenceImageRecognizer{err: errors.New("ocr failed")}, fallback)
	got, err := recognizer.RecognizeText(context.Background(), ImageTextRequest{Image: []byte("image")})
	if err != nil || got != "handwritten" {
		t.Fatalf("result = %q, err=%v", got, err)
	}
}
