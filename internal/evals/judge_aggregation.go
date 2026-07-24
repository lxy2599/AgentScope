package evals

import (
	"errors"
	"math"
	"sort"
	"strings"
)

const (
	AggregationMean        = "mean"
	AggregationMedian      = "median"
	AggregationTrimmedMean = "trimmed_mean"
	AggregationMajority    = "majority"
)

// JudgeVote is one provider/model decision. Score and Passed are nil on error.
type JudgeVote struct {
	Score           *float64 `json:"score,omitempty"`
	Passed          *bool    `json:"passed,omitempty"`
	Error           string   `json:"error,omitempty"`
	EssentialFailed bool     `json:"essential_failed,omitempty"`
	VetoTriggered   bool     `json:"veto_triggered,omitempty"`
	VetoReason      string   `json:"veto_reason,omitempty"`
}

// AggregationPolicy is controlled by the server-side metric configuration.
type AggregationPolicy struct {
	Method         string  `json:"method"`
	MinValidVotes  int     `json:"min_valid_votes"`
	PassThreshold  float64 `json:"pass_threshold"`
	TrimProportion float64 `json:"trim_proportion,omitempty"`
}

// JudgeAggregation contains the server-computed result and reliability data.
type JudgeAggregation struct {
	Method          string  `json:"method"`
	Score           float64 `json:"score"`
	Passed          bool    `json:"passed"`
	ValidVoteCount  int     `json:"valid_vote_count"`
	ErrorVoteCount  int     `json:"error_vote_count"`
	RequiredVotes   int     `json:"required_votes"`
	StdDev          float64 `json:"stddev"`
	Range           float64 `json:"range"`
	Disagreement    float64 `json:"disagreement"`
	Confidence      float64 `json:"confidence"`
	LowConfidence   bool    `json:"low_confidence"`
	EssentialFailed bool    `json:"essential_failed"`
	VetoTriggered   bool    `json:"veto_triggered"`
	VetoReason      string  `json:"veto_reason,omitempty"`
}

// AggregateJudgeVotes computes an aggregate from valid persisted votes.
func AggregateJudgeVotes(votes []JudgeVote, policy AggregationPolicy) (JudgeAggregation, error) {
	if len(votes) == 0 {
		return JudgeAggregation{}, errors.New("at least one judge vote is required")
	}
	if policy.Method == "" {
		policy.Method = AggregationMean
	}
	if policy.MinValidVotes <= 0 {
		policy.MinValidVotes = 1
	}
	if policy.PassThreshold <= 0 {
		policy.PassThreshold = 0.7
	}
	if policy.TrimProportion <= 0 {
		policy.TrimProportion = 0.1
	}

	scores := make([]float64, 0, len(votes))
	passes := 0
	result := JudgeAggregation{Method: policy.Method, RequiredVotes: policy.MinValidVotes}
	vetoReasons := make([]string, 0)
	for _, vote := range votes {
		if vote.Error != "" || vote.Score == nil || vote.Passed == nil {
			result.ErrorVoteCount++
			continue
		}
		if *vote.Score < 0 || *vote.Score > 1 {
			result.ErrorVoteCount++
			continue
		}
		scores = append(scores, *vote.Score)
		if *vote.Passed {
			passes++
		}
		result.EssentialFailed = result.EssentialFailed || vote.EssentialFailed
		result.VetoTriggered = result.VetoTriggered || vote.VetoTriggered
		if vote.VetoTriggered && vote.VetoReason != "" {
			vetoReasons = append(vetoReasons, vote.VetoReason)
		}
	}
	result.ValidVoteCount = len(scores)
	result.VetoReason = strings.Join(vetoReasons, "; ")
	if len(scores) == 0 {
		result.LowConfidence = true
		return result, nil
	}

	sorted := append([]float64(nil), scores...)
	sort.Float64s(sorted)
	switch policy.Method {
	case AggregationMean:
		result.Score = mean(sorted)
	case AggregationMedian:
		middle := len(sorted) / 2
		if len(sorted)%2 == 0 {
			result.Score = (sorted[middle-1] + sorted[middle]) / 2
		} else {
			result.Score = sorted[middle]
		}
	case AggregationTrimmedMean:
		trim := int(float64(len(sorted)) * policy.TrimProportion)
		if trim*2 >= len(sorted) {
			trim = 0
		}
		result.Score = mean(sorted[trim : len(sorted)-trim])
	case AggregationMajority:
		result.Score = float64(passes) / float64(len(sorted))
	default:
		return JudgeAggregation{}, errors.New("unsupported judge aggregation method")
	}

	meanScore := mean(sorted)
	var variance float64
	for _, score := range sorted {
		delta := score - meanScore
		variance += delta * delta
	}
	variance /= float64(len(sorted))
	result.StdDev = math.Sqrt(variance)
	result.Range = sorted[len(sorted)-1] - sorted[0]
	result.Disagreement = math.Min(1, result.StdDev/0.5)
	quorumRatio := math.Min(1, float64(len(sorted))/float64(policy.MinValidVotes))
	result.Confidence = math.Max(0, quorumRatio*(1-result.Disagreement))
	result.LowConfidence = len(sorted) < policy.MinValidVotes
	if result.LowConfidence {
		result.Confidence = 0
	}
	result.Passed = !result.LowConfidence &&
		!result.EssentialFailed &&
		!result.VetoTriggered &&
		result.Score >= policy.PassThreshold
	return result, nil
}

func mean(values []float64) float64 {
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}
