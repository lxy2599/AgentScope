package evals

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

type CalibrationSample struct {
	RunID       string             `json:"run_id"`
	JudgeScores map[string]float64 `json:"judge_scores"`
	GoldScores  map[string]float64 `json:"gold_scores"`
}

type CalibrationDimension struct {
	SampleCount   int      `json:"sample_count"`
	MAE           float64  `json:"mae"`
	MeanBias      float64  `json:"mean_bias"`
	AgreementRate float64  `json:"agreement_rate"`
	CohensKappa   *float64 `json:"cohens_kappa,omitempty"`
}

type CalibrationReport struct {
	SampleCount      int                             `json:"sample_count"`
	ObservationCount int                             `json:"observation_count"`
	MAE              float64                         `json:"mae"`
	AgreementRate    float64                         `json:"agreement_rate"`
	CohensKappa      *float64                        `json:"cohens_kappa,omitempty"`
	Dimensions       map[string]CalibrationDimension `json:"dimensions"`
	Warnings         []string                        `json:"warnings,omitempty"`
}

type calibrationObservation struct {
	judgeScore float64
	goldScore  float64
	judgePass  bool
	goldPass   bool
}

// BuildCalibrationReport compares Judge scores with human Gold scores.
// Only dimensions present in both maps are included.
func BuildCalibrationReport(samples []CalibrationSample, passThreshold float64) (CalibrationReport, error) {
	if len(samples) == 0 {
		return CalibrationReport{}, errors.New("calibration requires at least one sample")
	}
	if passThreshold < 0 || passThreshold > 1 {
		return CalibrationReport{}, errors.New("pass threshold must be between 0 and 1")
	}

	byDimension := make(map[string][]calibrationObservation)
	for _, sample := range samples {
		for dimension, goldScore := range sample.GoldScores {
			judgeScore, ok := sample.JudgeScores[dimension]
			if !ok {
				continue
			}
			if !validUnitScore(judgeScore) || !validUnitScore(goldScore) {
				return CalibrationReport{}, fmt.Errorf("dimension %q contains score outside [0,1]", dimension)
			}
			byDimension[dimension] = append(byDimension[dimension], calibrationObservation{
				judgeScore: judgeScore,
				goldScore:  goldScore,
				judgePass:  judgeScore >= passThreshold,
				goldPass:   goldScore >= passThreshold,
			})
		}
	}
	if len(byDimension) == 0 {
		return CalibrationReport{}, errors.New("calibration has no matching Judge and Gold dimensions")
	}

	report := CalibrationReport{
		SampleCount: len(samples),
		Dimensions:  make(map[string]CalibrationDimension, len(byDimension)),
	}
	var all []calibrationObservation
	names := make([]string, 0, len(byDimension))
	for name := range byDimension {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		observations := byDimension[name]
		all = append(all, observations...)
		dimension := summarizeCalibration(observations)
		report.Dimensions[name] = dimension
		if dimension.CohensKappa == nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s: Gold 或 Judge 仅有单一类别，无法计算 Cohen's Kappa", name))
		}
	}
	overall := summarizeCalibration(all)
	report.ObservationCount = len(all)
	report.MAE = overall.MAE
	report.AgreementRate = overall.AgreementRate
	report.CohensKappa = overall.CohensKappa
	if report.CohensKappa == nil {
		report.Warnings = append(report.Warnings, "总体样本类别不足，无法计算 Cohen's Kappa")
	}
	return report, nil
}

func summarizeCalibration(observations []calibrationObservation) CalibrationDimension {
	var absError, bias float64
	var agreements int
	for _, observation := range observations {
		absError += math.Abs(observation.judgeScore - observation.goldScore)
		bias += observation.judgeScore - observation.goldScore
		if observation.judgePass == observation.goldPass {
			agreements++
		}
	}
	count := len(observations)
	return CalibrationDimension{
		SampleCount:   count,
		MAE:           absError / float64(count),
		MeanBias:      bias / float64(count),
		AgreementRate: float64(agreements) / float64(count),
		CohensKappa:   cohensKappa(observations),
	}
}

func cohensKappa(observations []calibrationObservation) *float64 {
	if len(observations) < 2 {
		return nil
	}
	var bothPass, judgePass, goldPass int
	judgeCategories := map[bool]bool{}
	goldCategories := map[bool]bool{}
	for _, observation := range observations {
		judgeCategories[observation.judgePass] = true
		goldCategories[observation.goldPass] = true
		if observation.judgePass {
			judgePass++
		}
		if observation.goldPass {
			goldPass++
		}
		if observation.judgePass == observation.goldPass {
			bothPass++
		}
	}
	if len(judgeCategories) < 2 || len(goldCategories) < 2 {
		return nil
	}
	n := float64(len(observations))
	observed := float64(bothPass) / n
	judgePassRate := float64(judgePass) / n
	goldPassRate := float64(goldPass) / n
	expected := judgePassRate*goldPassRate + (1-judgePassRate)*(1-goldPassRate)
	if expected == 1 {
		return nil
	}
	kappa := (observed - expected) / (1 - expected)
	return &kappa
}

func validUnitScore(score float64) bool {
	return !math.IsNaN(score) && !math.IsInf(score, 0) && score >= 0 && score <= 1
}
