package evals

import (
	"math"
	"testing"
)

func TestAggregateJudgeVotesMethods(t *testing.T) {
	votes := []JudgeVote{
		{Score: ptrFloat(0.2), Passed: ptrBool(false)},
		{Score: ptrFloat(0.8), Passed: ptrBool(true)},
		{Score: ptrFloat(1.0), Passed: ptrBool(true)},
		{Error: "timeout"},
	}
	tests := []struct {
		method string
		want   float64
	}{
		{AggregationMean, 2.0 / 3.0},
		{AggregationMedian, 0.8},
		{AggregationTrimmedMean, 2.0 / 3.0},
		{AggregationMajority, 2.0 / 3.0},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got, err := AggregateJudgeVotes(votes, AggregationPolicy{
				Method: tt.method, MinValidVotes: 2, PassThreshold: 0.6,
			})
			if err != nil {
				t.Fatalf("aggregate: %v", err)
			}
			if math.Abs(got.Score-tt.want) > 0.0001 {
				t.Fatalf("score=%f want=%f", got.Score, tt.want)
			}
			if got.ValidVoteCount != 3 || got.ErrorVoteCount != 1 {
				t.Fatalf("vote counts=%d/%d", got.ValidVoteCount, got.ErrorVoteCount)
			}
			if got.StdDev <= 0 || got.Range != 0.8 || got.Disagreement <= 0 {
				t.Fatalf("statistics not populated: %+v", got)
			}
		})
	}
}

func TestAggregateJudgeVotesQuorumAndVeto(t *testing.T) {
	lowConfidence, err := AggregateJudgeVotes(
		[]JudgeVote{{Score: ptrFloat(0.9), Passed: ptrBool(true)}, {Error: "rate limit"}},
		AggregationPolicy{Method: AggregationMean, MinValidVotes: 2, PassThreshold: 0.7},
	)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if !lowConfidence.LowConfidence || lowConfidence.Passed || lowConfidence.Confidence != 0 {
		t.Fatalf("quorum must prevent pass: %+v", lowConfidence)
	}

	vetoed, err := AggregateJudgeVotes(
		[]JudgeVote{
			{Score: ptrFloat(0.95), Passed: ptrBool(true)},
			{Score: ptrFloat(0.9), Passed: ptrBool(true), VetoTriggered: true, VetoReason: "unsafe"},
		},
		AggregationPolicy{Method: AggregationMean, MinValidVotes: 2, PassThreshold: 0.7},
	)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if vetoed.Passed || !vetoed.VetoTriggered || vetoed.VetoReason != "unsafe" {
		t.Fatalf("veto did not override aggregate: %+v", vetoed)
	}
}

func ptrFloat(v float64) *float64 { return &v }
func ptrBool(v bool) *bool        { return &v }
