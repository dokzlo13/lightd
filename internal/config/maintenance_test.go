package config

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStrictConfigurationDocuments(t *testing.T) {
	for _, body := range []string{"reconciler:\n  enabledd: false\n", "script: x\n---\nscript: y\n"} {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}
func TestEffectiveBounds(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), -1} {
		if len((&ReconcilerConfig{RateLimitRPS: v}).Validate()) == 0 {
			t.Fatalf("invalid rate %v accepted", v)
		}
	}
	if len((&SSEConfig{MinRetryBackoff: Duration(3 * time.Minute)}).Validate()) == 0 {
		t.Fatal("inverted effective backoff accepted")
	}
	if len((&SSEConfig{RetryMultiplier: 0.5}).Validate()) == 0 {
		t.Fatal("shrinking retry accepted")
	}
}
