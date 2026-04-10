package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/agent"
)

// --- lab_open tool tests ---

func TestLabOpen_Success_NewLab(t *testing.T) {
	mock := &mockAgentService{
		labOpenFn: func(_ context.Context, userID, name, problem, domain string, tags []string) (*agent.LabInfo, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "ble-sampling", name)
			require.Equal(t, "Sampling rate drops to 60Hz under load", problem)
			require.Equal(t, "firmware", domain)
			require.Equal(t, []string{"bluetooth"}, tags)
			return &agent.LabInfo{
				SessionName:    "lab/ble-sampling",
				NotebookNoteID: "nb-001",
				Problem:        problem,
				Domain:         domain,
				Status:         "created",
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "lab_open", map[string]any{
		"name":    "ble-sampling",
		"problem": "Sampling rate drops to 60Hz under load",
		"domain":  "firmware",
		"tags":    "bluetooth",
	})

	require.False(t, result.IsError)
	var resp agent.LabInfo
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, "lab/ble-sampling", resp.SessionName)
	require.Equal(t, "nb-001", resp.NotebookNoteID)
	require.Equal(t, "created", resp.Status)
}

func TestLabOpen_MissingName(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "lab_open", map[string]any{
		"problem": "something",
		"domain":  "firmware",
	})
	require.True(t, result.IsError)
}

func TestLabOpen_MissingProblem(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "lab_open", map[string]any{
		"name":   "test",
		"domain": "firmware",
	})
	require.True(t, result.IsError)
}

func TestLabOpen_MissingDomain(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "lab_open", map[string]any{
		"name":    "test",
		"problem": "something",
	})
	require.True(t, result.IsError)
}

func TestLabOpen_InvalidName(t *testing.T) {
	mock := &mockAgentService{
		labOpenFn: func(context.Context, string, string, string, string, []string) (*agent.LabInfo, error) {
			return nil, agent.ErrInvalidLabName
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "lab_open", map[string]any{
		"name":    "bad name!",
		"problem": "something",
		"domain":  "firmware",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "invalid lab name")
}

// --- trial_record tool tests ---

func TestTrialRecord_Success_NewTrial(t *testing.T) {
	mock := &mockAgentService{
		trialRecordFn: func(_ context.Context, userID, lab, title, changes, expected, actual, outcome, notes string) (*agent.TrialSummary, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "ble-sampling", lab)
			require.Equal(t, "Increase connection interval", title)
			require.Equal(t, "ble_config.h: interval=30", changes)
			require.Equal(t, "Stable 100Hz", expected)
			require.Equal(t, "", actual)
			require.Equal(t, "", outcome)
			return &agent.TrialSummary{
				Title:   title,
				NoteID:  "trial-001",
				Outcome: agent.OutcomePending,
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "trial_record", map[string]any{
		"lab":      "ble-sampling",
		"title":    "Increase connection interval",
		"changes":  "ble_config.h: interval=30",
		"expected": "Stable 100Hz",
	})

	require.False(t, result.IsError)
	var resp agent.TrialSummary
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, "trial-001", resp.NoteID)
	require.Equal(t, agent.OutcomePending, resp.Outcome)
}

func TestTrialRecord_Success_WithOutcome(t *testing.T) {
	mock := &mockAgentService{
		trialRecordFn: func(_ context.Context, _, _, _, _, _, actual, outcome, _ string) (*agent.TrialSummary, error) {
			require.Equal(t, "Still drops to 60Hz", actual)
			require.Equal(t, "partial", outcome)
			return &agent.TrialSummary{
				Title:   "Increase connection interval",
				NoteID:  "trial-001",
				Outcome: "partial",
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "trial_record", map[string]any{
		"lab":      "ble-sampling",
		"title":    "Increase connection interval",
		"changes":  "ble_config.h: interval=30",
		"expected": "Stable 100Hz",
		"actual":   "Still drops to 60Hz",
		"outcome":  "partial",
	})

	require.False(t, result.IsError)
	var resp agent.TrialSummary
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, "partial", resp.Outcome)
}

func TestTrialRecord_MissingLab(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "trial_record", map[string]any{
		"title":    "test",
		"changes":  "x",
		"expected": "y",
	})
	require.True(t, result.IsError)
}

func TestTrialRecord_MissingChanges(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "trial_record", map[string]any{
		"lab":      "test",
		"title":    "test",
		"expected": "y",
	})
	require.True(t, result.IsError)
}

func TestTrialRecord_InvalidOutcome(t *testing.T) {
	mock := &mockAgentService{
		trialRecordFn: func(context.Context, string, string, string, string, string, string, string, string) (*agent.TrialSummary, error) {
			return nil, agent.ErrInvalidOutcome
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "trial_record", map[string]any{
		"lab":      "test",
		"title":    "test",
		"changes":  "x",
		"expected": "y",
		"outcome":  "bad",
	})
	require.True(t, result.IsError)
	require.Contains(t, textOf(t, result), "invalid outcome")
}

// --- decision_record tool tests ---

func TestDecisionRecord_Success(t *testing.T) {
	mock := &mockAgentService{
		decisionRecordFn: func(_ context.Context, userID, lab, title, rationale, basedOn, nextSteps string) (*agent.DecisionInfo, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "ble-sampling", lab)
			require.Equal(t, "Ship priority-based coex", title)
			require.Equal(t, "Best tradeoff between stability and WiFi", rationale)
			require.Equal(t, "Trial 2, Trial 3", basedOn)
			require.Equal(t, "Run regression tests", nextSteps)
			return &agent.DecisionInfo{
				Title:  title,
				NoteID: "dec-001",
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "decision_record", map[string]any{
		"lab":        "ble-sampling",
		"title":      "Ship priority-based coex",
		"rationale":  "Best tradeoff between stability and WiFi",
		"based_on":   "Trial 2, Trial 3",
		"next_steps": "Run regression tests",
	})

	require.False(t, result.IsError)
	var resp agent.DecisionInfo
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, "dec-001", resp.NoteID)
	require.Equal(t, "Ship priority-based coex", resp.Title)
}

func TestDecisionRecord_MissingRationale(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "decision_record", map[string]any{
		"lab":   "test",
		"title": "test",
	})
	require.True(t, result.IsError)
}

// --- trial_query tool tests ---

func TestTrialQuery_Success_AllTrials(t *testing.T) {
	mock := &mockAgentService{
		trialQueryFn: func(_ context.Context, userID, lab, query, outcome string, limit int) ([]agent.TrialSummary, error) {
			require.Equal(t, toolTestUser, userID)
			require.Equal(t, "ble-sampling", lab)
			require.Equal(t, "", query)
			require.Equal(t, "", outcome)
			require.Equal(t, 20, limit)
			return []agent.TrialSummary{
				{Title: "Trial 1", NoteID: "t-001", Outcome: "success"},
				{Title: "Trial 2", NoteID: "t-002", Outcome: "failure"},
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "trial_query", map[string]any{
		"lab": "ble-sampling",
	})

	require.False(t, result.IsError)
	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, float64(2), resp["total"])
}

func TestTrialQuery_Success_FilterByOutcome(t *testing.T) {
	mock := &mockAgentService{
		trialQueryFn: func(_ context.Context, _, _, _, outcome string, _ int) ([]agent.TrialSummary, error) {
			require.Equal(t, "success", outcome)
			return []agent.TrialSummary{
				{Title: "Trial 1", NoteID: "t-001", Outcome: "success"},
			}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "trial_query", map[string]any{
		"lab":     "ble-sampling",
		"outcome": "success",
	})

	require.False(t, result.IsError)
	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, float64(1), resp["total"])
}

func TestTrialQuery_EmptyLab(t *testing.T) {
	mock := &mockAgentService{
		trialQueryFn: func(context.Context, string, string, string, string, int) ([]agent.TrialSummary, error) {
			return []agent.TrialSummary{}, nil
		},
	}

	srv := newTestServer(t, mock)
	result := directCall(t, srv, "trial_query", map[string]any{
		"lab": "empty-lab",
	})

	require.False(t, result.IsError)
	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(textOf(t, result)), &resp))
	require.Equal(t, float64(0), resp["total"])
}

func TestTrialQuery_MissingLab(t *testing.T) {
	srv := newTestServer(t, &mockAgentService{})
	result := directCall(t, srv, "trial_query", map[string]any{
		"query": "something",
	})
	require.True(t, result.IsError)
}

