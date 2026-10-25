package middleware

type CountCollector struct{ *bufferedCollector }

func NewCountCollector(n int, fn FlushFunc) *CountCollector {
	return &CountCollector{newBuffered("count", n, fn)}
}
