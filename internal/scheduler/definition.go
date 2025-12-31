package scheduler

// MisfirePolicy defines how to handle missed schedule occurrences on boot
type MisfirePolicy string

const (
	MisfirePolicySkip      MisfirePolicy = "skip"       // Skip missed occurrences on boot
	MisfirePolicyRunLatest MisfirePolicy = "run_latest" // Run the most recent missed occurrence on boot
)
