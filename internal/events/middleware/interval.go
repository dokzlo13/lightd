package middleware

type IntervalCollector struct{ *bufferedCollector }

func NewIntervalCollector(ms int, fn FlushFunc) *IntervalCollector {
	return &IntervalCollector{newBuffered("interval", ms, fn)}
}
