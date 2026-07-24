package evals

import (
	"math"
	"testing"
)

func TestBuildCalibrationReport(t *testing.T) {
	samples := []CalibrationSample{
		{
			RunID:       "run-1",
			JudgeScores: map[string]float64{"quality": 0.9, "safety": 1},
			GoldScores:  map[string]float64{"quality": 1, "safety": 1},
		},
		{
			RunID:       "run-2",
			JudgeScores: map[string]float64{"quality": 0.2, "safety": 0.8},
			GoldScores:  map[string]float64{"quality": 0, "safety": 0},
		},
		{
			RunID:       "run-3",
			JudgeScores: map[string]float64{"quality": 0.8, "safety": 0.1},
			GoldScores:  map[string]float64{"quality": 1, "safety": 0},
		},
	}

	report, err := BuildCalibrationReport(samples, 0.5)
	if err != nil {
		t.Fatalf("BuildCalibrationReport: %v", err)
	}
	if report.SampleCount != 3 || len(report.Dimensions) != 2 {
		t.Fatalf("unexpected report size: %+v", report)
	}
	if math.Abs(report.MAE-(1.4/6.0)) > 1e-9 {
		t.Fatalf("MAE = %f, want %f", report.MAE, 1.4/6.0)
	}
	if math.Abs(report.AgreementRate-(5.0/6.0)) > 1e-9 {
		t.Fatalf("agreement = %f, want %f", report.AgreementRate, 5.0/6.0)
	}
	if report.CohensKappa == nil {
		t.Fatal("expected Cohen's Kappa")
	}
	if report.Dimensions["safety"].MeanBias <= report.Dimensions["quality"].MeanBias {
		t.Fatalf("expected safety to have larger positive bias: %+v", report.Dimensions)
	}
}

func TestBuildCalibrationReportDoesNotInventKappa(t *testing.T) {
	t.Run("empty samples", func(t *testing.T) {
		_, err := BuildCalibrationReport(nil, 0.5)
		if err == nil {
			t.Fatal("expected empty sample error")
		}
	})

	t.Run("single category", func(t *testing.T) {
		report, err := BuildCalibrationReport([]CalibrationSample{
			{RunID: "r1", JudgeScores: map[string]float64{"quality": 0.9}, GoldScores: map[string]float64{"quality": 1}},
			{RunID: "r2", JudgeScores: map[string]float64{"quality": 0.8}, GoldScores: map[string]float64{"quality": 1}},
		}, 0.5)
		if err != nil {
			t.Fatalf("BuildCalibrationReport: %v", err)
		}
		if report.CohensKappa != nil || report.Dimensions["quality"].CohensKappa != nil {
			t.Fatalf("single-category Kappa must be nil: %+v", report)
		}
		if len(report.Warnings) == 0 {
			t.Fatal("expected reliability warning")
		}
	})
}
