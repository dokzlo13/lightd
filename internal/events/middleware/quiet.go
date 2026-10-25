package middleware

type QuietCollector struct{ *bufferedCollector }

func NewQuietCollector(ms int, fn FlushFunc) *QuietCollector {
	return &QuietCollector{newBuffered("quiet", ms, fn)}
}
