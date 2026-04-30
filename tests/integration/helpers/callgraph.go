package helpers

type EdgeStats struct {
	Caller string `json:"caller"`
	Callee string `json:"callee"`
	Count  int    `json:"count"`
}

type CallGraph struct {
	Edges     []EdgeStats    `json:"edges"`
	Functions map[string]any `json:"functions"`
}

type FunctionCallGraphStats struct {
	Name                string `json:"name"`
	TotalCalls          int    `json:"total_calls"`
	TotalColdStarts     int    `json:"total_cold_starts"`
	TotalScaleUps       int    `json:"total_scale_ups"`
	TotalPrewarms       int    `json:"total_prewarms"`
	TotalScaleDowns     int    `json:"total_scale_downs"`
	TotalResets         int    `json:"total_resets"`
	LastColdStartAt     string `json:"last_cold_start_at"`
	LastPrewarmAt       string `json:"last_prewarm_at"`
	LastPrewarmDuration int64  `json:"last_prewarm_duration_ns"`
}

func EdgeCount(cg CallGraph, caller string, callee string) int {
	for _, e := range cg.Edges {
		if e.Caller == caller && e.Callee == callee {
			return e.Count
		}
	}
	return 0
}
