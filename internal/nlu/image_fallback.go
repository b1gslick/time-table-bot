package nlu

import (
	"context"
	"fmt"
	"strings"
)

const defaultImageOCRConfidence = 60

type ConfidenceImageTextRecognizer interface {
	RecognizeTextWithConfidence(ctx context.Context, req ImageTextRequest) (string, float64, error)
}

type FallbackImageTextRecognizer struct {
	primary       ConfidenceImageTextRecognizer
	fallback      ImageTextRecognizer
	minConfidence float64
}

func NewFallbackImageTextRecognizer(primary ConfidenceImageTextRecognizer, fallback ImageTextRecognizer) *FallbackImageTextRecognizer {
	return &FallbackImageTextRecognizer{
		primary:       primary,
		fallback:      fallback,
		minConfidence: defaultImageOCRConfidence,
	}
}

func (r *FallbackImageTextRecognizer) RecognizeText(ctx context.Context, req ImageTextRequest) (string, error) {
	if r == nil || r.primary == nil {
		return "", fmt.Errorf("primary image text recognizer is required")
	}
	primaryText, confidence, primaryErr := r.primary.RecognizeTextWithConfidence(ctx, req)
	if primaryErr == nil && strings.TrimSpace(primaryText) != "" && confidence >= r.minConfidence {
		return primaryText, nil
	}
	if r.fallback != nil {
		fallbackText, fallbackErr := r.fallback.RecognizeText(ctx, req)
		if fallbackErr == nil && strings.TrimSpace(fallbackText) != "" {
			return fallbackText, nil
		}
		if primaryErr != nil {
			return "", fmt.Errorf("local OCR: %v; vision fallback: %w", primaryErr, fallbackErr)
		}
	}
	if primaryErr != nil {
		return "", primaryErr
	}
	if strings.TrimSpace(primaryText) == "" {
		return "", fmt.Errorf("image text is empty")
	}
	return primaryText, nil
}
