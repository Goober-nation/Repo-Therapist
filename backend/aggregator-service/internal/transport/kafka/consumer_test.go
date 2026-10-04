package kafka

import (
	"context"
	"testing"

	"aggregator-service/internal/domain"
)

type recordingSaver struct {
	grade domain.RepoGrade
	mode  string
	code  string
}

func (r *recordingSaver) SaveGradingResult(ctx context.Context, grade domain.RepoGrade, gradeMode string, errorCode string) error {
	r.grade, r.mode, r.code = grade, gradeMode, errorCode
	return nil
}

func TestHandleMessageCarriesConfidence(t *testing.T) {
	saver := &recordingSaver{}
	payload := []byte(`{
		"sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
		"grade_mode": "public", "score": 64, "category_scores": {}, "advice": [], "missing_data": [],
		"graded_at": "2026-09-27T11:59:00Z",
		"confidence": {"low_confidence": true, "reasons": [{"code": "LOW_CODE", "text": "t"}]},
		"error": ""
	}`)

	if err := (&GradeConsumer{service: saver}).handleMessage(context.Background(), payload); err != nil {
		t.Fatal(err)
	}

	got := saver.grade.Confidence
	if got == nil || !got.LowConfidence || string(got.Reasons) != `[{"code": "LOW_CODE", "text": "t"}]` || saver.mode != "public" {
		t.Errorf("confidence = %+v mode = %q", got, saver.mode)
	}
}

func TestHandleMessageWithoutConfidenceIsNil(t *testing.T) {
	for name, field := range map[string]string{"absent": "", "null": `"confidence": null,`} {
		t.Run(name, func(t *testing.T) {
			saver := &recordingSaver{}
			payload := []byte(`{"sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e", "grade_mode": "private", ` + field + ` "graded_at": "2026-09-27T11:59:00Z"}`)

			if err := (&GradeConsumer{service: saver}).handleMessage(context.Background(), payload); err != nil {
				t.Fatal(err)
			}
			if saver.grade.Confidence != nil {
				t.Errorf("confidence = %+v, want nil", saver.grade.Confidence)
			}
		})
	}
}
