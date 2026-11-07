package redact

import "testing"

func BenchmarkRedactLine(b *testing.B) {
	var sample []string
	examples := secretExamples()
	i := 0
	for _, values := range examples {
		sample = append(sample, realisticLine(values[0], i))
		i++
	}
	sample = append(sample, nonSecretCorpus()...)

	r := NewRedactor(DefaultConfig())
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		line := []byte(sample[n%len(sample)])
		r.RedactLine(line)
	}
}
